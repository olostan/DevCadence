package controlplane_test

import (
	"context"
	"testing"

	"github.com/olostan/DevCadience/internal/controlplane"
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/events"
	"github.com/olostan/DevCadience/internal/protocol"
	"github.com/olostan/DevCadience/internal/testsupport"
)

func decision(status protocol.ProductDecisionStatus) *protocol.ProductDecision {
	return &protocol.ProductDecision{
		SchemaVersion: protocol.SchemaVersion1,
		DecisionID:    "PD-001",
		ProjectID:     "example",
		Question:      "Must this work offline?",
		Answer:        "Yes, fully offline.",
		Authority:     protocol.ProductDecisionAuthorityHuman,
		Status:        status,
		Consequences:  []string{"Everything runs locally."},
	}
}

func confirmedRequirement() *protocol.Requirement {
	ref := "PD-001"
	return &protocol.Requirement{
		SchemaVersion: protocol.SchemaVersion1,
		RequirementID: "FR-001",
		ProjectID:     "example",
		Statement:     "The control plane MUST run without network access.",
		Kind:          protocol.RequirementConstraint,
		Strength:      protocol.RequirementMust,
		Status:        protocol.RequirementConfirmed,
		Source:        protocol.RequirementSource{Type: protocol.SourceProductDecision, Ref: &ref},
	}
}

// storeDecisionAndRequirement writes the decision, then persists a confirmed
// requirement resting on it.
//
// The requirement record deliberately rides an *unrelated* payload. Attaching
// it to RequirementRecorded would prove nothing about persistence: the reducer
// refuses that event on its own, so the command would fail even with no
// persistence guard at all. The gap under test is that a caller can put a
// durable record into the store without ever emitting the event that would be
// checked — so the record write path has to carry the same rule.
func storeDecisionAndRequirement(
	t *testing.T, h *testsupport.Harness, status protocol.ProductDecisionStatus,
) error {
	t.Helper()
	ctx := context.Background()
	if _, err := h.Service.Apply(ctx, controlplane.Command{
		ProjectID: "example",
		Actor:     protocol.Actor{Kind: protocol.ActorHuman, ID: "operator"},
		Payload: &events.ProductDecisionRecorded{
			ProductDecisionID: "PD-001", Question: "Must this work offline?",
			Answer:       "Yes, fully offline.",
			RecordDigest: testsupport.Digest(t, decision(status)), Status: status,
		},
		Records: []controlplane.RecordToStore{{Version: 1, Record: decision(status)}},
	}); err != nil {
		t.Fatalf("store decision: %v", err)
	}
	unrelated := decision(protocol.ProductDecisionConfirmed)
	unrelated.DecisionID = "PD-002"
	unrelated.Question = "Anything else?"
	unrelated.Answer = "No."
	_, err := h.Service.Apply(ctx, controlplane.Command{
		ProjectID: "example",
		Actor:     protocol.Actor{Kind: protocol.ActorHuman, ID: "operator"},
		Payload: &events.ProductDecisionRecorded{
			ProductDecisionID: "PD-002", Question: "Anything else?", Answer: "No.",
			RecordDigest: testsupport.Digest(t, unrelated), Status: protocol.ProductDecisionConfirmed,
		},
		// The unrelated decision is what the payload is about; the
		// requirement simply rides along, which is the whole point — a
		// durable record can reach the store without its own event.
		Records: []controlplane.RecordToStore{
			{Version: 1, Record: unrelated},
			{Version: 1, Record: confirmedRequirement()},
		},
	})
	return err
}

// TestPersistedRequirementCannotRestOnAWithdrawnDecision closes the asymmetry
// between the two write paths. The reducer already refused the equivalent
// journal event; persistence proved only that *some* decision row existed, so
// a confirmed requirement could be written against authority the human had
// taken back. A rule enforced on one path only is a rule a caller can avoid.
func TestPersistedRequirementCannotRestOnAWithdrawnDecision(t *testing.T) {
	h := testsupport.NewHarness(t)
	initProject(t, h)

	// Control: a confirmed decision carries authority, so the requirement
	// must be accepted. Without this the test could pass for the wrong reason.
	if err := storeDecisionAndRequirement(t, h, protocol.ProductDecisionConfirmed); err != nil {
		t.Fatalf("a requirement resting on a confirmed decision was refused: %v", err)
	}

	withdrawn := testsupport.NewHarness(t)
	initProject(t, withdrawn)
	err := storeDecisionAndRequirement(t, withdrawn, protocol.ProductDecisionWithdrawn)
	if err == nil {
		t.Fatal("a requirement was persisted against a withdrawn decision")
	}
	if got := errs.CategoryOf(err); got != errs.CategoryIntegrity {
		t.Fatalf("category = %s, want integrity (%v)", got, err)
	}
}
