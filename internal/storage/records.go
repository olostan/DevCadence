package storage

import (
	"context"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// StoredRecord is a durable protocol document as it sits in the store.
type StoredRecord struct {
	ProjectID     string
	Kind          string
	ID            string
	Version       int
	SchemaVersion protocol.SchemaVersion
	// Document is the canonical JSON exactly as it was written. Keeping the
	// original bytes, rather than re-serialising the Go struct on read, is
	// what lets a record written by a different build remain byte-verifiable
	// against its digest (DCI-093).
	Document  string
	Digest    string
	CreatedAt string
}

// PutRecord stores an immutable protocol record and returns its digest.
//
// Records are content-addressed and never updated: docs/PROTOCOLS.md §19
// lists mutating a Work Package after attempts have started as an
// anti-pattern, and the database enforces it with a trigger. Writing the same
// (project, kind, id, version) twice succeeds only if the bytes are
// identical, which makes the operation idempotent without being lossy.
//
// Three checks stand between a caller and a durable record, and all three are
// here rather than in the caller so that there is one write path that cannot
// be bypassed:
//
//  1. the record's own declared project must match the project it is written
//     to, so a record cannot be filed under a project it disclaims;
//  2. the typed Go semantic validation must pass;
//  3. the serialised document must satisfy the published JSON Schema.
func (t *Tx) PutRecord(ctx context.Context, projectID string, version int, record protocol.Record) (string, error) {
	if projectID == "" {
		return "", errs.New(errs.CategoryInvalidArgument, "record: project_id is required")
	}
	if version < 1 {
		return "", errs.New(errs.CategoryInvalidArgument, "record: version must be >= 1")
	}
	// A record that names its own project must agree with the project it is
	// being written to. Disagreement means one of the two is wrong, and
	// guessing which would file evidence under the wrong project.
	if scoped, ok := record.(protocol.ProjectScoped); ok {
		if declared := scoped.ProjectOf(); declared != projectID {
			return "", errs.New(errs.CategoryInvalidArgument,
				"%s %s declares project %q but is being stored under project %q",
				record.RecordKind(), record.RecordID(), declared, projectID)
		}
	}
	// Validate before serialising, so an invalid record never becomes
	// durable evidence.
	if err := record.Validate(); err != nil {
		return "", err
	}
	canonical, err := protocol.CanonicalJSON(record)
	if err != nil {
		return "", err
	}
	// The JSON Schema is normative at integration boundaries
	// (ENGINEERING_STANDARDS.md §5), so a document the schema would reject
	// must not become durable even when the Go validation is satisfied. The
	// two representations express different constraints — string formats, for
	// instance, live only in the schema.
	if t.recordValidator != nil {
		if err := t.recordValidator.ValidateDocument(record.RecordKind(), canonical); err != nil {
			return "", errs.Wrap(errs.CategoryInvalidArgument, err,
				"%s %s does not satisfy its published schema", record.RecordKind(), record.RecordID())
		}
	}
	digest := protocol.DigestBytes(canonical)

	existing, err := t.record(ctx, projectID, record.RecordKind(), record.RecordID(), version)
	switch {
	case err == nil:
		if existing.Digest != digest {
			return "", errs.New(errs.CategoryConflict,
				"%s %s version %d already exists in project %s with different content (stored %s, offered %s)",
				record.RecordKind(), record.RecordID(), version, projectID, existing.Digest, digest)
		}
		return digest, nil
	case errs.CategoryOf(err) != errs.CategoryNotFound:
		return "", err
	}

	if _, err := t.tx.ExecContext(ctx,
		`INSERT INTO records (project_id, record_kind, record_id, record_version,
                              schema_version, document, digest, created_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		projectID, record.RecordKind(), record.RecordID(), version,
		string(record.SchemaVer()), string(canonical), digest, t.clock.Now().Format(timeLayout),
	); err != nil {
		return "", errs.Wrap(errs.CategoryInternal, err,
			"store %s %s version %d in project %s", record.RecordKind(), record.RecordID(), version, projectID)
	}
	return digest, nil
}

// Record returns one stored record, scoped to its project.
//
// The read verifies the stored document against its digest, so a caller can
// never act on evidence that changed after it was written.
func (t *Tx) Record(ctx context.Context, projectID, kind, id string, version int) (StoredRecord, error) {
	return t.record(ctx, projectID, kind, id, version)
}

func (t *Tx) record(ctx context.Context, projectID, kind, id string, version int) (StoredRecord, error) {
	if projectID == "" {
		return StoredRecord{}, errs.New(errs.CategoryInvalidArgument,
			"record lookup: project_id is required; records are identified within a project")
	}
	var (
		r         StoredRecord
		schemaVer string
	)
	err := t.tx.QueryRowContext(ctx,
		`SELECT project_id, record_kind, record_id, record_version, schema_version,
                document, digest, created_at
         FROM records
         WHERE project_id = ? AND record_kind = ? AND record_id = ? AND record_version = ?`,
		projectID, kind, id, version).
		Scan(&r.ProjectID, &r.Kind, &r.ID, &r.Version, &schemaVer, &r.Document, &r.Digest, &r.CreatedAt)
	if isNoRows(err) {
		return StoredRecord{}, notFound(kind, id)
	}
	if err != nil {
		return StoredRecord{}, errs.Wrap(errs.CategoryInternal, err, "read %s %s", kind, id)
	}
	r.SchemaVersion = protocol.SchemaVersion(schemaVer)
	// Evidence integrity is not the immutability triggers' job alone: those
	// stop the application from rewriting a row, and do nothing about a file
	// edited outside it. Verifying here means the caller cannot forget to
	// (docs/SECURITY.md §14).
	if err := protocol.VerifyDigest(kind, id, r.Digest, []byte(r.Document)); err != nil {
		return StoredRecord{}, err
	}
	return r, nil
}

// LatestRecordVersion returns the highest stored version for a record id
// within a project, or 0 when none exists.
func (t *Tx) LatestRecordVersion(ctx context.Context, projectID, kind, id string) (int, error) {
	if projectID == "" {
		return 0, errs.New(errs.CategoryInvalidArgument,
			"record lookup: project_id is required; records are identified within a project")
	}
	var version int
	err := t.tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(record_version), 0) FROM records
         WHERE project_id = ? AND record_kind = ? AND record_id = ?`,
		projectID, kind, id).Scan(&version)
	if err != nil {
		return 0, errs.Wrap(errs.CategoryInternal, err, "read latest version of %s %s", kind, id)
	}
	return version, nil
}

// RecordExists reports whether a record of the kind and id exists in the
// project at any version.
//
// It supports the high-value referential checks the control plane performs
// between product-authority records, without becoming a general referential
// engine.
//
// Existence is decided by reading the latest version through the same
// digest-verifying path a caller would use, not by a bare MAX(record_version)
// query. A row whose bytes changed outside the application must not be able to
// satisfy a reference check that a read of the same row would refuse: an
// authority check that accepts what the reader rejects is the weaker of the
// two signals, and the strict one has to win (ADR-0002 §4b).
func (t *Tx) RecordExists(ctx context.Context, projectID, kind, id string) (bool, error) {
	stored, err := t.LatestRecord(ctx, projectID, kind, id)
	if err != nil {
		return false, err
	}
	return stored != nil, nil
}

// LatestRecord returns the highest-versioned record of the kind and id in the
// project, or nil when the project has none. The returned record has been
// digest-verified.
func (t *Tx) LatestRecord(ctx context.Context, projectID, kind, id string) (*StoredRecord, error) {
	version, err := t.LatestRecordVersion(ctx, projectID, kind, id)
	if err != nil {
		return nil, err
	}
	if version == 0 {
		return nil, nil
	}
	stored, err := t.record(ctx, projectID, kind, id, version)
	if err != nil {
		return nil, err
	}
	return &stored, nil
}
