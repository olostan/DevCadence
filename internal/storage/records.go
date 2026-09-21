package storage

import (
	"context"

	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/protocol"
)

// StoredRecord is a durable protocol document as it sits in the store.
type StoredRecord struct {
	Kind          string
	ID            string
	Version       int
	ProjectID     string
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
// (kind, id, version) twice succeeds only if the bytes are identical, which
// makes the operation idempotent without being lossy.
func (t *Tx) PutRecord(ctx context.Context, projectID string, version int, record protocol.Record) (string, error) {
	if projectID == "" {
		return "", errs.New(errs.CategoryInvalidArgument, "record: project_id is required")
	}
	if version < 1 {
		return "", errs.New(errs.CategoryInvalidArgument, "record: version must be >= 1")
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
	digest, err := protocol.Digest(record)
	if err != nil {
		return "", err
	}

	existing, err := t.record(ctx, record.RecordKind(), record.RecordID(), version)
	switch {
	case err == nil:
		if existing.Digest != digest {
			return "", errs.New(errs.CategoryConflict,
				"%s %s version %d already exists with different content (stored %s, offered %s)",
				record.RecordKind(), record.RecordID(), version, existing.Digest, digest)
		}
		return digest, nil
	case errs.CategoryOf(err) != errs.CategoryNotFound:
		return "", err
	}

	if _, err := t.tx.ExecContext(ctx,
		`INSERT INTO records (record_kind, record_id, record_version, project_id,
                              schema_version, document, digest, created_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		record.RecordKind(), record.RecordID(), version, projectID,
		string(record.SchemaVer()), string(canonical), digest, t.clock.Now().Format(timeLayout),
	); err != nil {
		return "", errs.Wrap(errs.CategoryInternal, err,
			"store %s %s version %d", record.RecordKind(), record.RecordID(), version)
	}
	return digest, nil
}

// Record returns one stored record.
func (t *Tx) Record(ctx context.Context, kind, id string, version int) (StoredRecord, error) {
	return t.record(ctx, kind, id, version)
}

func (t *Tx) record(ctx context.Context, kind, id string, version int) (StoredRecord, error) {
	var (
		r         StoredRecord
		schemaVer string
	)
	err := t.tx.QueryRowContext(ctx,
		`SELECT record_kind, record_id, record_version, project_id, schema_version,
                document, digest, created_at
         FROM records WHERE record_kind = ? AND record_id = ? AND record_version = ?`,
		kind, id, version).
		Scan(&r.Kind, &r.ID, &r.Version, &r.ProjectID, &schemaVer, &r.Document, &r.Digest, &r.CreatedAt)
	if isNoRows(err) {
		return StoredRecord{}, notFound(kind, id)
	}
	if err != nil {
		return StoredRecord{}, errs.Wrap(errs.CategoryInternal, err, "read %s %s", kind, id)
	}
	r.SchemaVersion = protocol.SchemaVersion(schemaVer)
	return r, nil
}

// LatestRecordVersion returns the highest stored version for a record id, or
// 0 when none exists.
func (t *Tx) LatestRecordVersion(ctx context.Context, kind, id string) (int, error) {
	var version int
	err := t.tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(record_version), 0) FROM records WHERE record_kind = ? AND record_id = ?`,
		kind, id).Scan(&version)
	if err != nil {
		return 0, errs.Wrap(errs.CategoryInternal, err, "read latest version of %s %s", kind, id)
	}
	return version, nil
}
