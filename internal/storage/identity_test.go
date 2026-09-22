package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/storage"
)

// decisionFor builds a ProductDecision that uses the same semantic id in a
// named project. "PD-001" is exactly the sort of identifier two independent
// projects will both choose.
func decisionFor(projectID, answer string) *protocol.ProductDecision {
	return &protocol.ProductDecision{
		SchemaVersion: protocol.SchemaVersion1,
		DecisionID:    "PD-001",
		ProjectID:     projectID,
		Question:      "Must this work offline?",
		Answer:        answer,
		Authority:     protocol.ProductDecisionAuthorityHuman,
		Status:        protocol.ProductDecisionConfirmed,
		Consequences:  []string{"Everything runs locally."},
	}
}

// TestRecordsAreScopedByProject is the identity property: two projects may
// both use "PD-001" for different decisions without colliding.
func TestRecordsAreScopedByProject(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)

	alpha := decisionFor("alpha", "Yes, fully offline.")
	beta := decisionFor("beta", "No, a package registry is permitted.")

	for _, record := range []*protocol.ProductDecision{alpha, beta} {
		if err := store.Write(ctx, func(tx *storage.Tx) error {
			_, err := tx.PutRecord(ctx, record.ProjectID, 1, record)
			return err
		}); err != nil {
			t.Fatalf("store %s decision: %v", record.ProjectID, err)
		}
	}

	// Each project reads back its own answer, not the other's.
	for _, want := range []*protocol.ProductDecision{alpha, beta} {
		var stored storage.StoredRecord
		if err := store.Read(ctx, func(tx *storage.Tx) error {
			var err error
			stored, err = tx.Record(ctx, want.ProjectID, "ProductDecision", "PD-001", 1)
			return err
		}); err != nil {
			t.Fatalf("read %s decision: %v", want.ProjectID, err)
		}
		var decoded protocol.ProductDecision
		if err := protocol.Unmarshal([]byte(stored.Document), &decoded); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if decoded.Answer != want.Answer {
			t.Fatalf("project %s read answer %q, want %q", want.ProjectID, decoded.Answer, want.Answer)
		}
		if stored.ProjectID != want.ProjectID {
			t.Fatalf("record came back under project %q, want %q", stored.ProjectID, want.ProjectID)
		}
	}
}

// TestRecordLookupCannotReachAnotherProject checks the negative direction:
// a project that never stored the id must not find one.
func TestRecordLookupCannotReachAnotherProject(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)

	if err := store.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.PutRecord(ctx, "alpha", 1, decisionFor("alpha", "Yes."))
		return err
	}); err != nil {
		t.Fatalf("store: %v", err)
	}

	err := store.Read(ctx, func(tx *storage.Tx) error {
		_, err := tx.Record(ctx, "beta", "ProductDecision", "PD-001", 1)
		return err
	})
	if err == nil {
		t.Fatal("project beta retrieved project alpha's record")
	}
	if got := errs.CategoryOf(err); got != errs.CategoryNotFound {
		t.Fatalf("category = %s, want not_found (%v)", got, err)
	}
}

// TestRecordProjectMismatchIsRejected stops a record being filed under a
// project it disclaims.
func TestRecordProjectMismatchIsRejected(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)

	err := store.Write(ctx, func(tx *storage.Tx) error {
		// The record says "alpha"; the write says "beta".
		_, err := tx.PutRecord(ctx, "beta", 1, decisionFor("alpha", "Yes."))
		return err
	})
	if err == nil {
		t.Fatal("a record was stored under a project it does not declare")
	}
	if got := errs.CategoryOf(err); got != errs.CategoryInvalidArgument {
		t.Fatalf("category = %s, want invalid_argument (%v)", got, err)
	}
	if !strings.Contains(err.Error(), "declares project") {
		t.Fatalf("error does not explain the mismatch: %v", err)
	}
}

// TestLatestRecordVersionIsProjectScoped keeps version resolution from
// leaking across projects, which would make a project see a revision it never
// made.
func TestLatestRecordVersionIsProjectScoped(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)

	if err := store.Write(ctx, func(tx *storage.Tx) error {
		for version := 1; version <= 3; version++ {
			workPackage := sampleWorkPackage(version, "alpha objective")
			workPackage.ProjectID = "alpha"
			if _, err := tx.PutRecord(ctx, "alpha", version, workPackage); err != nil {
				return err
			}
		}
		beta := sampleWorkPackage(1, "beta objective")
		beta.ProjectID = "beta"
		_, err := tx.PutRecord(ctx, "beta", 1, beta)
		return err
	}); err != nil {
		t.Fatalf("store: %v", err)
	}

	for project, want := range map[string]int{"alpha": 3, "beta": 1, "gamma": 0} {
		var latest int
		if err := store.Read(ctx, func(tx *storage.Tx) error {
			var err error
			latest, err = tx.LatestRecordVersion(ctx, project, "EngineeringWorkPackage",
				"wp_000000000000000000000001")
			return err
		}); err != nil {
			t.Fatalf("latest version for %s: %v", project, err)
		}
		if latest != want {
			t.Errorf("latest version in project %s = %d, want %d", project, latest, want)
		}
	}
}

// TestRecordLookupRequiresAProject refuses an unscoped lookup outright rather
// than quietly returning whichever project's row comes first.
func TestRecordLookupRequiresAProject(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	err := store.Read(ctx, func(tx *storage.Tx) error {
		_, err := tx.Record(ctx, "", "ProductDecision", "PD-001", 1)
		return err
	})
	if got := errs.CategoryOf(err); got != errs.CategoryInvalidArgument {
		t.Fatalf("category = %s, want invalid_argument (%v)", got, err)
	}
}

// TestProjectScopedRecordSurvivesReopen checks the identity holds across a
// process restart, not just within one connection.
func TestProjectScopedRecordSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/control-plane.db"

	first, err := storage.Open(ctx, storage.Config{Path: path, Clock: testClock()})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := first.Write(ctx, func(tx *storage.Tx) error {
		if _, err := tx.PutRecord(ctx, "alpha", 1, decisionFor("alpha", "Yes.")); err != nil {
			return err
		}
		_, err := tx.PutRecord(ctx, "beta", 1, decisionFor("beta", "No."))
		return err
	}); err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	second, err := storage.Open(ctx, storage.Config{Path: path, Clock: testClock()})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = second.Close() }()

	for project, want := range map[string]string{"alpha": "Yes.", "beta": "No."} {
		var stored storage.StoredRecord
		if err := second.Read(ctx, func(tx *storage.Tx) error {
			var err error
			stored, err = tx.Record(ctx, project, "ProductDecision", "PD-001", 1)
			return err
		}); err != nil {
			t.Fatalf("read %s after reopen: %v", project, err)
		}
		var decoded protocol.ProductDecision
		if err := protocol.Unmarshal([]byte(stored.Document), &decoded); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if decoded.Answer != want {
			t.Fatalf("after reopen, project %s read %q, want %q", project, decoded.Answer, want)
		}
	}
}
