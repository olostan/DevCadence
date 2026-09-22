package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
)

// migrationFS holds the ordered SQL migrations.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// Migration is one ordered schema change.
type Migration struct {
	Version  int
	Name     string
	SQL      string
	Checksum string
}

// LoadMigrations reads and orders the embedded migrations.
//
// File names are `NNNN_name.sql`. The numeric prefix is the version and must
// be unique: ENGINEERING_STANDARDS.md §10 requires explicit, ordered
// migrations, and two files claiming one version would make "which schema is
// version 3" unanswerable.
func LoadMigrations() ([]Migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "read embedded migrations")
	}
	out := make([]Migration, 0, len(entries))
	seen := make(map[int]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		base := strings.TrimSuffix(entry.Name(), ".sql")
		prefix, name, found := strings.Cut(base, "_")
		if !found {
			return nil, errs.New(errs.CategoryInternal,
				"migration %q must be named NNNN_name.sql", entry.Name())
		}
		version, err := strconv.Atoi(prefix)
		if err != nil || version < 1 {
			return nil, errs.New(errs.CategoryInternal,
				"migration %q has an invalid version prefix", entry.Name())
		}
		if other, dup := seen[version]; dup {
			return nil, errs.New(errs.CategoryInternal,
				"migrations %q and %q both claim version %d", other, entry.Name(), version)
		}
		seen[version] = entry.Name()
		body, err := migrationFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, errs.Wrap(errs.CategoryInternal, err, "read migration %s", entry.Name())
		}
		sum := sha256.Sum256(body)
		out = append(out, Migration{
			Version:  version,
			Name:     name,
			SQL:      string(body),
			Checksum: "sha256:" + hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// AppliedMigration records one migration that has run against a database.
type AppliedMigration struct {
	Version   int
	Name      string
	Checksum  string
	AppliedAt string
}

// migrate brings db up to the latest schema version.
//
// Each migration runs in its own transaction and records itself in the same
// transaction, so a failure leaves the database at the last fully applied
// version rather than half-migrated. Re-running is a no-op, which is what
// makes opening an existing database safe.
func (s *Store) migrate(ctx context.Context) error {
	migrations, err := LoadMigrations()
	if err != nil {
		return err
	}
	if len(migrations) == 0 {
		return errs.New(errs.CategoryInternal, "no migrations are embedded in this build")
	}
	applied, err := s.AppliedMigrations(ctx)
	if err != nil {
		return err
	}
	byVersion := make(map[int]AppliedMigration, len(applied))
	for _, a := range applied {
		byVersion[a.Version] = a
	}
	if err := verifyAppliedMigrations(migrations, applied); err != nil {
		return err
	}
	for _, m := range migrations {
		if existing, done := byVersion[m.Version]; done {
			// A changed checksum means a migration that already ran has been
			// edited. Silently ignoring it would leave databases with the same
			// recorded version but different schemas, so it is refused.
			if existing.Checksum != m.Checksum {
				return errs.New(errs.CategoryIntegrity,
					"migration %04d_%s was already applied with checksum %s but is now %s; "+
						"add a new migration instead of editing an applied one",
					m.Version, m.Name, existing.Checksum, m.Checksum)
			}
			continue
		}
		if err := s.applyMigration(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

// verifyAppliedMigrations refuses a database whose migration history this
// build cannot account for.
//
// Checking only the migrations this build *has* leaves two histories
// undetected, and both are worse than a checksum mismatch because they are
// silent:
//
//   - a migration recorded as applied that this build does not carry, which
//     means the database was migrated by a newer binary and now holds a schema
//     this one does not understand;
//   - a gap in the applied versions, which means the recorded history is not a
//     prefix of any real migration sequence.
//
// In both cases the database is not at a version this build can reason about,
// and proceeding would read and write a schema it is guessing at. Migrations
// are ordered and contiguous by construction (TestMigrationsAreOrderedAndUniquelyVersioned),
// so the applied set must be a contiguous prefix of them.
func verifyAppliedMigrations(embedded []Migration, applied []AppliedMigration) error {
	known := make(map[int]bool, len(embedded))
	for _, m := range embedded {
		known[m.Version] = true
	}
	highest := 0
	for _, a := range applied {
		if !known[a.Version] {
			return errs.New(errs.CategoryIntegrity,
				"database has migration %04d_%s applied, which this build does not carry; "+
					"it was migrated by a newer DevCadence and must not be used with this one",
				a.Version, a.Name)
		}
		if a.Version > highest {
			highest = a.Version
		}
	}
	// A contiguous prefix has exactly as many entries as its highest version.
	if highest != len(applied) {
		return errs.New(errs.CategoryIntegrity,
			"database migration history is not contiguous: %d migrations applied but the highest is %04d; "+
				"the recorded history is not a prefix of any real migration sequence",
			len(applied), highest)
	}
	return nil
}

// verifySchema checks a database this build will only read: the applied
// migrations must be ones it carries, contiguous, and unedited. A read-only
// command skips migration *application*, but skipping verification too would
// let `state show` report from a schema the build does not understand.
func (s *Store) verifySchema(ctx context.Context) error {
	embedded, err := LoadMigrations()
	if err != nil {
		return err
	}
	applied, err := s.AppliedMigrations(ctx)
	if err != nil {
		return err
	}
	if err := verifyAppliedMigrations(embedded, applied); err != nil {
		return err
	}
	byVersion := make(map[int]Migration, len(embedded))
	for _, m := range embedded {
		byVersion[m.Version] = m
	}
	for _, a := range applied {
		if byVersion[a.Version].Checksum != a.Checksum {
			return errs.New(errs.CategoryIntegrity,
				"migration %04d_%s was applied with checksum %s but this build has %s",
				a.Version, a.Name, a.Checksum, byVersion[a.Version].Checksum)
		}
	}
	return nil
}

// bookkeepingExists reports whether schema_migrations has been created yet.
// A database on which no migration has run has no bookkeeping table, because
// the baseline migration is what creates it.
func (s *Store) bookkeepingExists(ctx context.Context) (bool, error) {
	var name string
	err := s.db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`).Scan(&name)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, errs.Wrap(errs.CategoryInternal, err, "inspect sqlite_master")
	}
	return true, nil
}

func (s *Store) applyMigration(ctx context.Context, m Migration) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "begin migration %04d", m.Version)
	}
	defer func() { _ = tx.Rollback() }()

	// The baseline migration creates schema_migrations itself, so the DDL and
	// the record of having applied it land in one transaction. That is why
	// there is no bootstrap step that creates bookkeeping outside a
	// migration: a fresh database is either fully at version 1 or untouched.
	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "apply migration %04d_%s", m.Version, m.Name)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name, checksum, applied_at) VALUES (?, ?, ?, ?)`,
		m.Version, m.Name, m.Checksum, s.clock.Now().Format(timeLayout),
	); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "record migration %04d_%s", m.Version, m.Name)
	}
	if err := tx.Commit(); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "commit migration %04d_%s", m.Version, m.Name)
	}
	return nil
}

// AppliedMigrations returns the migrations recorded against the database, in
// version order.
func (s *Store) AppliedMigrations(ctx context.Context) ([]AppliedMigration, error) {
	exists, err := s.bookkeepingExists(ctx)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT version, name, checksum, applied_at FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "read schema_migrations")
	}
	defer func() { _ = rows.Close() }()
	var out []AppliedMigration
	for rows.Next() {
		var a AppliedMigration
		if err := rows.Scan(&a.Version, &a.Name, &a.Checksum, &a.AppliedAt); err != nil {
			return nil, errs.Wrap(errs.CategoryInternal, err, "scan schema_migrations")
		}
		out = append(out, a)
	}
	return out, errs.Wrap(errs.CategoryInternal, rows.Err(), "iterate schema_migrations")
}

// SchemaVersion returns the highest applied migration version, or 0 for an
// unmigrated database.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	exists, err := s.bookkeepingExists(ctx)
	if err != nil || !exists {
		return 0, err
	}
	var version sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		return 0, errs.Wrap(errs.CategoryInternal, err, "read schema version")
	}
	if !version.Valid {
		return 0, nil
	}
	return int(version.Int64), nil
}
