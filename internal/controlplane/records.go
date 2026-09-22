package controlplane

import (
	"context"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/storage"
)

// checkReferencedRecord verifies that a payload claiming a durable record is
// telling the truth, before the event is appended.
//
// The two guarantees are deliberately separate and both are required:
//
//	reducer lineage        proves journal-internal consistency — that the
//	                       events refer to tasks, attempts and candidates
//	                       that the journal itself established;
//	reference validation   proves that the referenced immutable evidence
//	                       actually exists and matches the digest.
//
// Lineage alone cannot see the record store, and this check alone cannot see
// the task graph. An acceptance that passes only one of them is justified by
// something that is not there.
//
// The record may have been written earlier or in this same transaction:
// Command.Records is persisted before this runs, so both resolve through one
// lookup. Any failure returns an error from inside the write transaction, so
// the record writes, the journal append and the projection update all roll
// back together — no partial evidence survives.
func checkReferencedRecord(
	ctx context.Context, tx *storage.Tx, projectID string, payload events.Payload,
) error {
	referencing, ok := payload.(events.RecordReferencing)
	if !ok {
		return nil
	}
	ref := referencing.ReferencedRecord()
	if !ref.Claimed() {
		// The payload carries an optional digest and did not use it, so it
		// asserts nothing about a durable document.
		return nil
	}
	if ref.ID == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"%s claims a record digest but names no record", payload.Type())
	}

	stored, err := resolveRecord(ctx, tx, projectID, ref)
	if err != nil {
		return err
	}
	if stored == nil {
		return errs.New(errs.CategoryIntegrity,
			"%s references %s %s, which does not exist in project %s; "+
				"the referenced record must already be stored or be written in the same command, "+
				"otherwise the journal claims evidence that is not there",
			payload.Type(), ref.Kind, ref.ID, projectID)
	}
	if stored.Digest != ref.Digest {
		return errs.New(errs.CategoryIntegrity,
			"%s references %s %s with digest %s, but the stored record has digest %s",
			payload.Type(), ref.Kind, ref.ID, ref.Digest, stored.Digest)
	}
	// Identity and digest agree; now the compact claims must agree with the
	// document they summarise.
	return referencing.CheckReferencedRecord([]byte(stored.Document))
}

// resolveRecord loads the referenced version, or the latest when the event
// names none. A missing record is (nil, nil) so the caller can report it with
// the payload's own vocabulary.
func resolveRecord(
	ctx context.Context, tx *storage.Tx, projectID string, ref events.RecordRef,
) (*storage.StoredRecord, error) {
	if ref.Version <= 0 {
		return tx.LatestRecord(ctx, projectID, ref.Kind, ref.ID)
	}
	stored, err := tx.Record(ctx, projectID, ref.Kind, ref.ID, ref.Version)
	if err != nil {
		if errs.CategoryOf(err) == errs.CategoryNotFound {
			// An event naming a version the store does not have is the
			// "wrong version" case, reported by the caller as a missing
			// reference rather than as an internal failure.
			return nil, nil
		}
		return nil, err
	}
	return &stored, nil
}
