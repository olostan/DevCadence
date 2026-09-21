// Package storage implements the SQLite control-plane store: explicit
// migrations, the append-only event journal, the immutable record store and
// the derived projections.
//
// SQLite is the source of truth for DevCadience control-plane records; Git
// remains the source of truth for code (docs/ARCHITECTURE.md §10). The driver
// is pure Go so that a single static binary runs on macOS and Linux without a
// C toolchain; see docs/adr/0002-control-plane-persistence.md.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/olostan/DevCadience/internal/clock"
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/schema"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// timeLayout is how timestamps are stored. It is fixed-width and UTC so that
// textual ordering equals chronological ordering in SQL.
const timeLayout = "2006-01-02T15:04:05.000000Z"

// MemoryPath opens a private in-memory database. Tests use it so that a suite
// needs no filesystem and no cleanup.
const MemoryPath = ":memory:"

// RecordValidator checks a durable record's serialised document against its
// published JSON Schema before the record is committed.
//
// The interface lives here so that storage can enforce the check while the
// policy — which schema governs which kind — stays in internal/schema. See
// schema.RecordValidator for the implementation.
type RecordValidator interface {
	ValidateDocument(kind string, document []byte) error
}

// Config configures a Store.
type Config struct {
	// Path is the database file, or MemoryPath.
	Path string
	// Clock stamps migration bookkeeping and record creation. Tests inject a
	// deterministic clock here.
	Clock clock.Clock
	// ReadOnly opens the database without applying migrations. A read-only
	// open of an unmigrated database fails rather than silently reporting an
	// empty project.
	ReadOnly bool
	// RecordValidator enforces the published JSON Schema at the durable write
	// boundary. Leaving it nil selects the schemas embedded in this build,
	// so the safe behaviour is the default and disabling the check requires
	// SkipRecordSchemaValidation, which exists only for tests that construct
	// deliberately non-conforming documents.
	RecordValidator RecordValidator
	// SkipRecordSchemaValidation disables schema enforcement on writes. No
	// production path sets it.
	SkipRecordSchemaValidation bool
}

// Store owns the database handle.
//
// The connection pool is capped at one connection. SQLite permits a single
// writer, and a local control plane has no throughput requirement that would
// justify the complexity of juggling reader and writer connections, WAL
// checkpoints and SQLITE_BUSY retries. Serialising at the pool removes a
// whole class of concurrency bugs; if contention ever becomes measurable, the
// cap is one line to revisit (ENGINEERING_STANDARDS.md §25: measure first).
type Store struct {
	db              *sql.DB
	clock           clock.Clock
	path            string
	recordValidator RecordValidator
}

// Open opens (creating if necessary) the control-plane database and brings it
// to the latest schema version.
func Open(ctx context.Context, cfg Config) (*Store, error) {
	if cfg.Path == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "storage: database path is required")
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.System()
	}
	// A read-only open is refused before the driver sees the path, because
	// mode=ro reports a missing file as an opaque driver error and a typo in
	// -db must read as "this project does not exist", not as an internal
	// fault.
	if cfg.ReadOnly && cfg.Path != MemoryPath && !strings.HasPrefix(cfg.Path, "file::memory:") {
		if _, err := os.Stat(cfg.Path); err != nil {
			if os.IsNotExist(err) {
				return nil, errs.New(errs.CategoryNotFound,
					"database %s does not exist; run `devcadience project init` first", cfg.Path)
			}
			return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "open database %s", cfg.Path)
		}
	}
	dsn, err := buildDSN(cfg.Path, cfg.ReadOnly)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "open database %s", cfg.Path)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	// The one connection must never be recycled. An in-memory database lives
	// inside its connection, so dropping it would silently discard the whole
	// store; on disk, recycling would only cost a reopen, but there is no
	// reason to allow it.
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)

	validator := cfg.RecordValidator
	if validator == nil && !cfg.SkipRecordSchemaValidation {
		// Defaulting to the embedded schemas means a caller who thinks about
		// none of this still cannot persist a schema-invalid record.
		defaultValidator, err := schema.DefaultValidator()
		if err != nil {
			_ = db.Close()
			return nil, err
		}
		validator = defaultValidator
	}
	store := &Store{db: db, clock: cfg.Clock, path: cfg.Path, recordValidator: validator}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, errs.Wrap(errs.CategoryInternal, err, "connect to database %s", cfg.Path)
	}
	if cfg.ReadOnly {
		version, err := store.SchemaVersion(ctx)
		if err != nil {
			_ = db.Close()
			return nil, err
		}
		if version == 0 {
			_ = db.Close()
			return nil, errs.New(errs.CategoryNotFound,
				"database %s has no DevCadience schema; run `devcadience project init` first", cfg.Path)
		}
		return store, nil
	}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// buildDSN turns a filesystem path into a driver DSN with the pragmas the
// control plane relies on.
//
// The path is cleaned and, for on-disk databases, made absolute, so that a
// relative path cannot resolve differently depending on the process working
// directory (docs/SECURITY.md §6: canonical paths).
// readOnly opens the database without any possibility of writing to it: the
// driver is given mode=ro, so a command that promises not to write cannot
// create a missing database file, apply pragmas or alter a byte. Refusing
// migrations was never enough — SQLite creates the file when it opens it, so
// a typo in -db left an empty database behind before the schema check
// reported the project missing.
func buildDSN(path string, readOnly bool) (string, error) {
	pragmas := url.Values{}
	// Referential integrity between projections is enforced by the database,
	// not by hope.
	pragmas.Add("_pragma", "foreign_keys(1)")
	// A blocked write should surface as a timeout, not spin.
	pragmas.Add("_pragma", "busy_timeout(5000)")

	if path == MemoryPath || strings.HasPrefix(path, "file::memory:") {
		// An in-memory database has nothing to protect and cannot pre-exist,
		// so mode=ro would only make it unopenable.
		return "file::memory:?" + pragmas.Encode(), nil
	}
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", errs.Wrap(errs.CategoryInvalidArgument, err, "resolve database path %s", path)
	}
	if readOnly {
		// journal_mode and synchronous are writes to the database header, so
		// they are deliberately not set here.
		pragmas.Set("mode", "ro")
		return "file:" + absolute + "?" + pragmas.Encode(), nil
	}
	// WAL keeps a reader from blocking the writer and survives process crashes
	// without losing committed transactions.
	pragmas.Add("_pragma", "journal_mode(WAL)")
	pragmas.Add("_pragma", "synchronous(NORMAL)")
	return "file:" + absolute + "?" + pragmas.Encode(), nil
}

// Path returns the database path the store was opened with.
func (s *Store) Path() string { return s.path }

// Clock returns the store's clock, so that callers stamp records with the
// same time source the store uses.
func (s *Store) Clock() clock.Clock { return s.clock }

// Close releases the database handle.
func (s *Store) Close() error {
	return errs.Wrap(errs.CategoryInternal, s.db.Close(), "close database")
}

// Tx is a transaction scope. Every method that reads or writes control-plane
// state takes one, so that a caller cannot accidentally split an atomic
// operation across two transactions.
type Tx struct {
	tx              *sql.Tx
	clock           clock.Clock
	recordValidator RecordValidator
}

// Write runs fn inside a read-write transaction, committing on success and
// rolling back on any error or panic.
//
// This is the boundary that makes "append an event and update the projection"
// atomic: either both are durable or neither is (ENGINEERING_STANDARDS.md
// §10, §12).
func (s *Store) Write(ctx context.Context, fn func(*Tx) error) (err error) {
	tx, beginErr := s.db.BeginTx(ctx, nil)
	if beginErr != nil {
		return errs.Wrap(errs.CategoryInternal, beginErr, "begin transaction")
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		// Rollback errors are not reported over the original failure: the
		// caller needs to know why the operation failed, and an unmigrated
		// rollback of an already-failed transaction adds nothing.
		_ = tx.Rollback()
	}()

	scope := &Tx{tx: tx, clock: s.clock, recordValidator: s.recordValidator}
	if err := fn(scope); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "commit transaction")
	}
	committed = true
	return nil
}

// Read runs fn inside a read-only transaction. It uses a transaction rather
// than bare queries so that a multi-statement read sees one consistent
// snapshot.
func (s *Store) Read(ctx context.Context, fn func(*Tx) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "begin read transaction")
	}
	defer func() { _ = tx.Rollback() }()
	return fn(&Tx{tx: tx, clock: s.clock, recordValidator: s.recordValidator})
}

// isConstraintViolation reports whether err is a SQLite constraint failure.
//
// The driver does not export a typed constraint error, so the message is
// inspected. The check is deliberately narrow and only used to turn an
// expected uniqueness clash into a categorised conflict; every other error
// keeps its original classification.
func isConstraintViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "constraint failed") ||
		strings.Contains(err.Error(), "SQLITE_CONSTRAINT")
}

// notFound builds the standard not-found error for a missing row.
func notFound(kind, id string) error {
	return errs.New(errs.CategoryNotFound, "%s %s does not exist", kind, id)
}

// isNoRows reports whether err is sql.ErrNoRows.
func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
