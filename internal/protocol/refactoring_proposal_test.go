package protocol_test

import (
	"testing"

	"github.com/olostan/DevCadence/internal/protocol"
)

func validRefactoringProposal() *protocol.RefactoringProposal {
	return &protocol.RefactoringProposal{
		SchemaVersion:        protocol.SchemaVersion1,
		ProposalID:           "prop_1",
		CreatedAt:            "2026-09-30T00:00:00Z",
		SourceWorkPackageID:  "WP-M3C-2",
		TargetWorkPackageID:  "WP-M3C-1",
		ArchitecturalTension: "Adapter controllability tension",
		ContradictionEvidence: []protocol.EvidenceRef{
			{
				ID:      "ev_1",
				Type:    protocol.EvidenceRefSource,
				Locator: "internal/protocol/access_channel.go#L35",
			},
		},
		ProposedInterface:      "type ExtendedChannel interface{}",
		AffectedCallers:        []string{"internal/cognition/drivers/api.go"},
		Reversibility:          protocol.ReversibilityModerate,
		ReversibilityRationale: "Straightforward interface expansion",
		Status:                 protocol.ProposalProposed,
	}
}

func TestRefactoringProposalValidation(t *testing.T) {
	t.Run("valid proposal passes", func(t *testing.T) {
		prop := validRefactoringProposal()
		if err := prop.Validate(); err != nil {
			t.Fatalf("expected valid, got: %v", err)
		}
		if prop.RecordKind() != "RefactoringProposal" {
			t.Errorf("record kind: got %q, want RefactoringProposal", prop.RecordKind())
		}
		if prop.RecordID() != "prop_1" {
			t.Errorf("record ID: got %q, want prop_1", prop.RecordID())
		}
	})

	t.Run("missing contradiction evidence rejected", func(t *testing.T) {
		prop := validRefactoringProposal()
		prop.ContradictionEvidence = nil
		if err := prop.Validate(); err == nil {
			t.Fatal("expected error on empty contradiction evidence, got nil")
		}
	})

	t.Run("invalid reversibility rejected", func(t *testing.T) {
		prop := validRefactoringProposal()
		prop.Reversibility = "unbounded"
		if err := prop.Validate(); err == nil {
			t.Fatal("expected error on invalid reversibility, got nil")
		}
	})

	t.Run("invalid status rejected", func(t *testing.T) {
		prop := validRefactoringProposal()
		prop.Status = "auto_merged"
		if err := prop.Validate(); err == nil {
			t.Fatal("expected error on invalid status, got nil")
		}
	})

	t.Run("fail-closed adjudication lifecycle", func(t *testing.T) {
		wpID := "WP-M3C-3"

		// 1. Proposed status must NOT have adjudication
		prop := validRefactoringProposal()
		prop.Status = protocol.ProposalProposed
		prop.Adjudication = &protocol.ProposalAdjudication{
			AdjudicatedBy: protocol.Actor{
				Kind: protocol.ActorPrincipal,
				ID:   "principal_session_01",
			},
			AdjudicatedAt:    "2026-09-30T00:10:00Z",
			DispositionNotes: "Early notes",
		}
		if err := prop.Validate(); err == nil {
			t.Fatal("expected error: proposed status must not have adjudication")
		}

		// 2. Terminal accepted without adjudication must fail
		prop = validRefactoringProposal()
		prop.Status = protocol.ProposalAccepted
		prop.Adjudication = nil
		if err := prop.Validate(); err == nil {
			t.Fatal("expected error: accepted status requires adjudication")
		}

		// 3. Terminal accepted without resulting_work_package_id must fail
		prop = validRefactoringProposal()
		prop.Status = protocol.ProposalAccepted
		prop.Adjudication = &protocol.ProposalAdjudication{
			AdjudicatedBy: protocol.Actor{
				Kind: protocol.ActorPrincipal,
				ID:   "principal_session_01",
			},
			AdjudicatedAt:    "2026-09-30T00:10:00Z",
			DispositionNotes: "Accepted",
		}
		if err := prop.Validate(); err == nil {
			t.Fatal("expected error: accepted status requires resulting_work_package_id")
		}

		// 4. Terminal accepted with resulting_work_package_id passes
		prop.Adjudication.ResultingWorkPackageID = &wpID
		if err := prop.Validate(); err != nil {
			t.Fatalf("expected valid accepted proposal, got: %v", err)
		}

		// 5. Terminal rejected with adjudication passes even without resulting_work_package_id
		prop = validRefactoringProposal()
		prop.Status = protocol.ProposalRejected
		prop.Adjudication = &protocol.ProposalAdjudication{
			AdjudicatedBy: protocol.Actor{
				Kind: protocol.ActorHuman,
				ID:   "owner_olostan",
			},
			AdjudicatedAt:    "2026-09-30T00:10:00Z",
			DispositionNotes: "Not aligned with architectural boundaries",
		}
		if err := prop.Validate(); err != nil {
			t.Fatalf("expected valid rejected proposal, got: %v", err)
		}

		// 6. Invalid actor in adjudication fails
		prop.Adjudication.AdjudicatedBy.Kind = "invalid_actor"
		if err := prop.Validate(); err == nil {
			t.Fatal("expected error on invalid actor kind in adjudication, got nil")
		}
	})
}
