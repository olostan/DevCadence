package storage_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/storage"
	"github.com/olostan/DevCadience/internal/testsupport"
)

func TestMigrationsAreOrderedAndUniquelyVersioned(t *testing.T) {
	migrations, err := storage.LoadMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("no migrations are embedded")
	}
	seen := make(map[int]bool, len(migrations))
	previous := 0
	for _, m := range migrations {
		if m.Version <= previous {
			t.Fatalf("migrations are not in ascending order: %d after %d", m.Version, previous)
		}
		if seen[m.Version] {
			t.Fatalf("version %d appears twice", m.Version)
		}
		if m.Checksum == "" || m.SQL == "" {
			t.Fatalf("migration %04d is incomplete", m.Version)
		}
		seen[m.Version] = true
		previous = m.Version
	}
}

func TestMigrationFromAnEmptyDatabase(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, storage.Config{Path: storage.MemoryPath, Clock: testsupport.NewClock()})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = store.Close() }()

	available, err := storage.LoadMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	applied, err := store.AppliedMigrations(ctx)
	if err != nil {
		t.Fatalf("applied: %v", err)
	}
	if len(applied) != len(available) {
		t.Fatalf("applied %d migrations, want %d", len(applied), len(available))
	}
	version, err := store.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if version != available[len(available)-1].Version {
		t.Fatalf("schema version = %d, want %d", version, available[len(available)-1].Version)
	}
}

// TestReopeningAMigratedDatabaseIsIdempotent is what makes `devcadience`
// safe to run repeatedly: opening must not re-apply or mutate the schema.
func TestReopeningAMigratedDatabaseIsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "control-plane.db")

	first, err := storage.Open(ctx, storage.Config{Path: path, Clock: testsupport.NewClock()})
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	firstApplied, err := first.AppliedMigrations(ctx)
	if err != nil {
		t.Fatalf("applied: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	second, err := storage.Open(ctx, storage.Config{Path: path, Clock: testsupport.NewClock()})
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer func() { _ = second.Close() }()
	secondApplied, err := second.AppliedMigrations(ctx)
	if err != nil {
		t.Fatalf("applied: %v", err)
	}
	if len(firstApplied) != len(secondApplied) {
		t.Fatalf("reopening changed the migration count: %d then %d", len(firstApplied), len(secondApplied))
	}
	for i := range firstApplied {
		if firstApplied[i] != secondApplied[i] {
			t.Fatalf("reopening changed migration %d: %+v then %+v", i, firstApplied[i], secondApplied[i])
		}
	}
}

// TestAnEditedMigrationIsRefused protects against two databases reporting the
// same schema version while holding different schemas.
func TestAnEditedMigrationIsRefused(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "control-plane.db")
	store, err := storage.Open(ctx, storage.Config{Path: path, Clock: testsupport.NewClock()})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Simulate an edited migration by corrupting the recorded checksum.
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE schema_migrations SET checksum = 'sha256:tampered' WHERE version = 1`); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	_, err = storage.Open(ctx, storage.Config{Path: path, Clock: testsupport.NewClock()})
	if err == nil {
		t.Fatal("opening a database whose applied migration was edited succeeded")
	}
	if got := errs.CategoryOf(err); got != errs.CategoryIntegrity {
		t.Fatalf("category = %s, want integrity (%v)", got, err)
	}
}

func TestReadOnlyOpenRefusesAnUnmigratedDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "never-initialised.db")
	_, err := storage.Open(ctx, storage.Config{Path: path, Clock: testsupport.NewClock(), ReadOnly: true})
	if err == nil {
		t.Fatal("a read-only open of an unmigrated database succeeded")
	}
	// A typo in -db must report a missing project, not an empty one.
	if got := errs.CategoryOf(err); got != errs.CategoryNotFound {
		t.Fatalf("category = %s, want not_found (%v)", got, err)
	}
}

func TestOpenRequiresAPath(t *testing.T) {
	if _, err := storage.Open(context.Background(), storage.Config{}); err == nil {
		t.Fatal("opening with no path succeeded")
	}
}

// TestReadOnlyOpenCreatesNothingOnDisk pins the read-only contract at the
// filesystem, not merely at the migration step. Refusing migrations was never
// enough: SQLite creates the database file when it opens it, so a typo in -db
// left an empty database (and its -wal/-shm companions) behind while
// reporting the project missing. A command that promises not to write must
// leave the directory exactly as it found it.
func TestReadOnlyOpenCreatesNothingOnDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "never-created.db")

	_, err := storage.Open(context.Background(),
		storage.Config{Path: path, Clock: testsupport.NewClock(), ReadOnly: true})
	if err == nil {
		t.Fatal("a read-only open succeeded against a database that does not exist")
	}
	if got := errs.CategoryOf(err); got != errs.CategoryNotFound {
		t.Fatalf("category = %s, want not_found (%v)", got, err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("a read-only open created %v", names)
	}
}

// TestMigrationHistoryThisBuildCannotAccountForIsRefused covers the two silent
// histories a checksum check alone misses.
//
// Both mean the recorded version is not one this build can reason about, so
// reading or writing against it would be guessing at the schema.
func TestMigrationHistoryThisBuildCannotAccountForIsRefused(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		corrupt string
		what    string
	}{
		{
			name: "applied migration this build does not carry",
			corrupt: `INSERT INTO schema_migrations (version, name, checksum, applied_at)
                      VALUES (99, 'from_the_future', 'sha256:ffff', '2026-01-02T03:04:05.000000Z')`,
			what: "a database migrated by a newer build",
		},
		{
			name: "gap in the applied history",
			corrupt: `INSERT INTO schema_migrations (version, name, checksum, applied_at)
                      VALUES (3, 'orphan', 'sha256:eeee', '2026-01-02T03:04:05.000000Z')`,
			what: "a non-contiguous migration history",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "control-plane.db")
			store, err := storage.Open(ctx, storage.Config{Path: path, Clock: testsupport.NewClock()})
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if err := store.Write(ctx, func(tx *storage.Tx) error {
				return tx.ExecForTest(ctx, tc.corrupt)
			}); err != nil {
				t.Fatalf("seed history: %v", err)
			}
			if err := store.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}

			// Both the migrating path and the read-only path must refuse it:
			// a read-only command reporting from a schema it does not
			// understand is the quieter of the two failures.
			for _, readOnly := range []bool{false, true} {
				_, err := storage.Open(ctx, storage.Config{
					Path: path, Clock: testsupport.NewClock(), ReadOnly: readOnly,
				})
				if err == nil {
					t.Fatalf("%s was opened (read_only=%v)", tc.what, readOnly)
				}
				if got := errs.CategoryOf(err); got != errs.CategoryIntegrity {
					t.Fatalf("read_only=%v: category = %s, want integrity (%v)", readOnly, got, err)
				}
			}
		})
	}
}

// TestReadOnlyOpenVerifiesMigrationChecksums closes the other half: read-only
// skipped applying migrations and verifying them.
func TestReadOnlyOpenVerifiesMigrationChecksums(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "control-plane.db")
	store, err := storage.Open(ctx, storage.Config{Path: path, Clock: testsupport.NewClock()})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := store.Write(ctx, func(tx *storage.Tx) error {
		return tx.ExecForTest(ctx, `UPDATE schema_migrations SET checksum = 'sha256:tampered'`)
	}); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	_, err = storage.Open(ctx, storage.Config{
		Path: path, Clock: testsupport.NewClock(), ReadOnly: true,
	})
	if err == nil {
		t.Fatal("a read-only open accepted an edited migration history")
	}
	if got := errs.CategoryOf(err); got != errs.CategoryIntegrity {
		t.Fatalf("category = %s, want integrity (%v)", got, err)
	}
}
