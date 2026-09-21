package storage_test

import (
	"context"
	"testing"

	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/protocol"
	"github.com/olostan/DevCadience/internal/storage"
)

func sampleWorkPackage(version int, objective string) *protocol.EngineeringWorkPackage {
	return &protocol.EngineeringWorkPackage{
		SchemaVersion:        protocol.SchemaVersion1,
		WorkPackageID:        "wp_000000000000000000000001",
		TaskID:               "tsk_000000000000000000000001",
		Version:              version,
		ProjectID:            "example",
		ProjectStateRevision: "ps_000000004",
		BaseCommit:           "91acd8273f1",
		ChangeClass:          protocol.ChangeSystemic,
		Objective:            objective,
		Rationale:            "Because the journal grows without bound.",
		ArchitecturalIntent:  "Keep the journal append-only.",
		Scope:                protocol.Scope{InScope: []string{"storage"}, OutOfScope: []string{"envelope"}},
		Guidance: []protocol.Guidance{
			{ID: "G1", Strength: protocol.GuidanceMust, Statement: "The journal stays append-only."},
		},
		AcceptanceCriteria:     []string{"Bounded reads work."},
		ValidationRequirements: []string{"go test ./..."},
		EscalationConditions:   []string{"Assumption A2 is false."},
	}
}

func TestPutRecordIsContentAddressedAndIdempotent(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)

	var firstDigest string
	if err := store.Write(ctx, func(tx *storage.Tx) error {
		digest, err := tx.PutRecord(ctx, "example", 1, sampleWorkPackage(1, "Bounded journal reads"))
		firstDigest = digest
		return err
	}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if firstDigest == "" {
		t.Fatal("no digest was returned")
	}

	// Writing identical content again is a no-op, not a conflict: a retried
	// approval must not fail merely because it is a retry.
	if err := store.Write(ctx, func(tx *storage.Tx) error {
		digest, err := tx.PutRecord(ctx, "example", 1, sampleWorkPackage(1, "Bounded journal reads"))
		if err != nil {
			return err
		}
		if digest != firstDigest {
			t.Fatalf("digest changed for identical content: %s then %s", firstDigest, digest)
		}
		return nil
	}); err != nil {
		t.Fatalf("idempotent put: %v", err)
	}
}

// TestPutRecordRefusesToRewriteHistory is the anti-pattern in
// docs/PROTOCOLS.md §19: a Work Package must not be mutated in place.
func TestPutRecordRefusesToRewriteHistory(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	if err := store.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.PutRecord(ctx, "example", 1, sampleWorkPackage(1, "Bounded journal reads"))
		return err
	}); err != nil {
		t.Fatalf("put: %v", err)
	}

	err := store.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.PutRecord(ctx, "example", 1, sampleWorkPackage(1, "A different objective entirely"))
		return err
	})
	if err == nil {
		t.Fatal("a stored record was overwritten with different content")
	}
	if got := errs.CategoryOf(err); got != errs.CategoryConflict {
		t.Fatalf("category = %s, want conflict (%v)", got, err)
	}

	// A revision is a new version, which is permitted.
	if err := store.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.PutRecord(ctx, "example", 2, sampleWorkPackage(2, "A revised objective"))
		return err
	}); err != nil {
		t.Fatalf("storing a new version failed: %v", err)
	}

	var latest int
	if err := store.Read(ctx, func(tx *storage.Tx) error {
		var err error
		latest, err = tx.LatestRecordVersion(ctx, "EngineeringWorkPackage", "wp_000000000000000000000001")
		return err
	}); err != nil {
		t.Fatalf("latest version: %v", err)
	}
	if latest != 2 {
		t.Fatalf("latest version = %d, want 2", latest)
	}

	// The superseded version is still retrievable, which is what keeps
	// historical Work Packages interpretable (DCI-093).
	var original storage.StoredRecord
	if err := store.Read(ctx, func(tx *storage.Tx) error {
		var err error
		original, err = tx.Record(ctx, "EngineeringWorkPackage", "wp_000000000000000000000001", 1)
		return err
	}); err != nil {
		t.Fatalf("read version 1: %v", err)
	}
	var decoded protocol.EngineeringWorkPackage
	if err := protocol.Unmarshal([]byte(original.Document), &decoded); err != nil {
		t.Fatalf("decode version 1: %v", err)
	}
	if decoded.Objective != "Bounded journal reads" {
		t.Fatalf("version 1 objective = %q; it was overwritten", decoded.Objective)
	}
}

func TestRecordsCannotBeUpdatedOrDeleted(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	if err := store.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.PutRecord(ctx, "example", 1, sampleWorkPackage(1, "Bounded journal reads"))
		return err
	}); err != nil {
		t.Fatalf("put: %v", err)
	}
	for _, statement := range []string{
		`UPDATE records SET document = '{}' WHERE record_id = 'wp_000000000000000000000001'`,
		`DELETE FROM records WHERE record_id = 'wp_000000000000000000000001'`,
	} {
		if err := store.Write(ctx, func(tx *storage.Tx) error {
			return tx.ExecForTest(ctx, statement)
		}); err == nil {
			t.Fatalf("statement %q was permitted against immutable evidence", statement)
		}
	}
}

func TestPutRecordRefusesAnInvalidDocument(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	invalid := sampleWorkPackage(1, "")
	err := store.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.PutRecord(ctx, "example", 1, invalid)
		return err
	})
	if err == nil {
		t.Fatal("an invalid record became durable evidence")
	}
	if got := errs.CategoryOf(err); got != errs.CategoryInvalidArgument {
		t.Fatalf("category = %s, want invalid_argument (%v)", got, err)
	}
}

func TestMissingRecordIsNotFound(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	err := store.Read(ctx, func(tx *storage.Tx) error {
		_, err := tx.Record(ctx, "EngineeringWorkPackage", "wp_missing", 1)
		return err
	})
	if got := errs.CategoryOf(err); got != errs.CategoryNotFound {
		t.Fatalf("category = %s, want not_found (%v)", got, err)
	}
}
