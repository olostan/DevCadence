package selfhost_test

// STUB-BASED. The handoff reports the execution mode the attempt ran under,
// recorded at attempt time, not the mode of the executor that inspects it.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/selfhost"
)

func TestStub_HandoffReportsAttemptTimeExecutionMode(t *testing.T) {
	stub := newStubOllama(t, scriptedFix(false))
	f := newFixture(t)
	first := f.build(t, yoloConfig(stub))
	if ref := f.delegate(t, 3*time.Minute); ref.Status != principal.StatusCompleted {
		t.Fatalf("delegate: %s", ref.Status)
	}
	att := f.attempt(t)
	cand := principal.CandidateRef{
		TaskID: f.taskID, AttemptID: att.ID, Commit: att.CandidateCommit,
		WorkPackage: principal.WorkPackageRef{ID: f.wp.WorkPackageID, Version: f.wp.Version, Digest: f.wpDigest, BaseCommit: f.wp.BaseCommit},
	}
	h, err := first.Executor.InspectCandidate(context.Background(), cand)
	if err != nil || h.ExecutionMode != "unsafe_unconfined_local" {
		t.Fatalf("yolo inspect: %+v %v", h, err)
	}

	// The owner later switches the config to strict: a new executor inspects the old attempt.
	_ = first.Close()
	strict := stub.config()
	strict.ExecutionMode = selfhost.ExecutionModeStrict
	second := f.build(t, strict)
	h, err = second.Executor.InspectCandidate(context.Background(), cand)
	if err != nil {
		t.Fatal(err)
	}
	if h.ExecutionMode != "unsafe_unconfined_local" {
		t.Errorf("mode = %q, want the attempt's recorded mode", h.ExecutionMode)
	}
	if !strings.HasPrefix(h.Ref, "refs/devcadence/candidates/") {
		t.Errorf("ref = %q", h.Ref)
	}
}
