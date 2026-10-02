package compiler_test

import (
	"testing"

	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/protocol"
)

func TestCapsuleManager_Operations(t *testing.T) {
	mgr := compiler.NewCapsuleManager()

	// 1. Hypotheses
	mgr.AddHypothesis("H1: Issue is caused by missing nil check")
	mgr.AddHypothesis("H2: Issue is caused by timeout")
	snap := mgr.Snapshot()
	if len(snap.Hypotheses) != 2 {
		t.Fatalf("expected 2 hypotheses, got %d", len(snap.Hypotheses))
	}

	mgr.ResolveHypothesis("H1: Issue is caused by missing nil check")
	snap = mgr.Snapshot()
	if len(snap.Hypotheses) != 1 || snap.Hypotheses[0] != "H2: Issue is caused by timeout" {
		t.Fatalf("unexpected hypotheses after resolve: %v", snap.Hypotheses)
	}

	// 2. Active TODOs
	mgr.AddTODO("Implement retry backoff")
	mgr.AddTODO("Add unit tests")
	snap = mgr.Snapshot()
	if len(snap.ActiveTODOs) != 2 {
		t.Fatalf("expected 2 todos, got %d", len(snap.ActiveTODOs))
	}

	mgr.CompleteTODO("Implement retry backoff")
	snap = mgr.Snapshot()
	if len(snap.ActiveTODOs) != 1 || snap.ActiveTODOs[0] != "Add unit tests" {
		t.Fatalf("unexpected active TODOs after complete: %v", snap.ActiveTODOs)
	}

	// 3. Intermediate Decisions
	mgr.RecordDecision("Use exponential backoff with jitter")
	snap = mgr.Snapshot()
	if len(snap.IntermediateDecisions) != 1 || snap.IntermediateDecisions[0] != "Use exponential backoff with jitter" {
		t.Fatalf("unexpected decisions: %v", snap.IntermediateDecisions)
	}

	// 4. Open Questions
	mgr.AddOpenQuestion("What is the maximum backoff interval?")
	snap = mgr.Snapshot()
	if len(snap.OpenQuestions) != 1 {
		t.Fatalf("expected 1 open question, got %d", len(snap.OpenQuestions))
	}
	mgr.AnswerQuestion("What is the maximum backoff interval?")
	snap = mgr.Snapshot()
	if len(snap.OpenQuestions) != 0 {
		t.Fatalf("expected 0 open questions after answer, got %d", len(snap.OpenQuestions))
	}

	// 5. Evidence Dependencies
	mgr.TrackEvidenceDependency("lease-123")
	snap = mgr.Snapshot()
	if len(snap.EvidenceDependencies) != 1 || snap.EvidenceDependencies[0] != "lease-123" {
		t.Fatalf("unexpected evidence dependencies: %v", snap.EvidenceDependencies)
	}
	mgr.RemoveEvidenceDependency("lease-123")
	snap = mgr.Snapshot()
	if len(snap.EvidenceDependencies) != 0 {
		t.Fatalf("expected 0 evidence dependencies after remove, got %d", len(snap.EvidenceDependencies))
	}

	// 6. Restore & Clear
	restoredCapsule := protocol.CognitiveStateCapsule{
		Hypotheses:            []string{"H_restored"},
		ActiveTODOs:           []string{"T_restored"},
		IntermediateDecisions: []string{"D_restored"},
		OpenQuestions:         []string{"Q_restored"},
		EvidenceDependencies:  []string{"lease_restored"},
	}
	mgr.Restore(restoredCapsule)
	snap = mgr.Snapshot()
	if snap.Hypotheses[0] != "H_restored" || snap.ActiveTODOs[0] != "T_restored" {
		t.Fatalf("restore did not restore capsule state: %v", snap)
	}

	mgr.Clear()
	snap = mgr.Snapshot()
	if len(snap.Hypotheses) != 0 || len(snap.ActiveTODOs) != 0 {
		t.Fatalf("clear did not empty capsule state: %v", snap)
	}
}
