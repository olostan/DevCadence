package controlplane_test

import (
	"context"
	"testing"

	"github.com/olostan/DevCadience/internal/controlplane"
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/events"
	"github.com/olostan/DevCadience/internal/protocol"
	"github.com/olostan/DevCadience/internal/storage"
	"github.com/olostan/DevCadience/internal/tasks"
	"github.com/olostan/DevCadience/internal/testsupport"
)

// driveLifecycle replays the happy-path scenario through the application
// service, so that the events under test are the ones the control plane
// actually produces rather than hand-built ones.
func driveLifecycle(t *testing.T, h *testsupport.Harness) string {
	t.Helper()
	ctx := context.Background()
	initProject(t, h)
	if _, err := h.Service.CreateTask(ctx, controlplane.CreateTaskInput{
		ProjectID: "example", Alias: "DC-001", Title: "Bounded journal reads",
		ChangeClass: protocol.ChangeSystemic,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	taskID := mustTaskID(t, h, "DC-001")

	const (
		wpID       = "wp_000000000000000000000001"
		attemptID  = "att_000000000000000000000001"
		candidate  = "cafebabe1234567"
		integrated = "deadbeef7654321"
	)
	// Every event that claims a durable record is written together with that
	// record, and the digest is the one the store will compute. This is the
	// shape a real validator or reviewer will use in M2/M3: produce the
	// evidence, emit the event summarising it, one atomic command.
	workPackage := testsupport.WorkPackage("example", taskID, wpID, 1)
	attemptValidation := testsupport.AttemptValidation(
		"example", "val_0001", taskID, attemptID, candidate, protocol.ValidationPass)
	review := testsupport.Review(
		"example", "rev_0001", attemptID, wpID, protocol.DimensionCorrectness, protocol.VerdictPass)
	integrationValidation := testsupport.IntegrationValidation(
		"example", "val_0002", taskID, integrated, protocol.ValidationPass)

	steps := []struct {
		payload events.Payload
		records []controlplane.RecordToStore
	}{
		{payload: &events.TaskDesignStarted{TaskID: taskID, Reason: "initial design"}},
		{
			payload: &events.WorkPackageApproved{
				TaskID: taskID, WorkPackageID: wpID, WorkPackageVersion: 1,
				RecordDigest:         testsupport.Digest(t, workPackage),
				ProjectStateRevision: workPackage.ProjectStateRevision,
				BaseCommit:           workPackage.BaseCommit,
				ChangeClass:          protocol.ChangeSystemic,
			},
			records: []controlplane.RecordToStore{{Version: 1, Record: workPackage}},
		},
		{payload: &events.TaskDelegated{
			TaskID: taskID, WorkPackageID: wpID, WorkerRole: "implementer", MaxAttempts: 3,
		}},
		{payload: &events.AttemptStarted{
			TaskID: taskID, AttemptID: attemptID, WorkPackageID: wpID, WorkPackageVersion: 1,
			ProjectStateRevision: "ps_000000003", BaseCommit: "91acd8273f1", WorkerRole: "implementer",
		}},
		{payload: &events.CandidateProduced{
			TaskID: taskID, AttemptID: attemptID, CandidateCommit: candidate,
			Summary: "Bounded reads implemented.", RepairIterations: 1,
		}},
		{
			payload: &events.ValidationCompleted{
				TaskID: taskID, AttemptID: attemptID, ValidationID: "val_0001",
				Scope: events.ScopeAttempt, Status: protocol.ValidationPass,
				Commit: candidate, RecordDigest: testsupport.Digest(t, attemptValidation),
			},
			records: []controlplane.RecordToStore{{Version: 1, Record: attemptValidation}},
		},
		{
			payload: &events.ReviewCompleted{
				TaskID: taskID, AttemptID: attemptID, ReviewID: "rev_0001",
				Dimension: protocol.DimensionCorrectness, Verdict: protocol.VerdictPass,
				RecordDigest: testsupport.Digest(t, review),
			},
			records: []controlplane.RecordToStore{{Version: 1, Record: review}},
		},
		{payload: &events.ChangeAccepted{
			TaskID: taskID, AttemptID: attemptID, WorkPackageID: wpID, CandidateCommit: candidate,
			SemanticSummary: "Journal reads accept an inclusive upper bound.",
			ValidationIDs:   []string{"val_0001"},
			ReviewIDs:       []string{"rev_0001"},
			DecidedBy:       protocol.AuthorityPrincipal,
		}},
		{payload: &events.IntegrationStarted{TaskID: taskID, IntegrationID: "int_0001"}},
		{payload: &events.IntegrationValidationStarted{
			TaskID: taskID, IntegrationID: "int_0001", IntegratedCommit: integrated,
		}},
		{
			payload: &events.ValidationCompleted{
				TaskID: taskID, ValidationID: "val_0002", Scope: events.ScopeIntegration,
				Status: protocol.ValidationPass, Commit: integrated,
				RecordDigest: testsupport.Digest(t, integrationValidation),
			},
			records: []controlplane.RecordToStore{{Version: 1, Record: integrationValidation}},
		},
	}
	for _, step := range steps {
		if _, err := h.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
			ProjectID:   "example",
			Payload:     step.payload,
			Records:     step.records,
			Correlation: events.CorrelationFor(step.payload),
		}); err != nil {
			t.Fatalf("append %s: %v", step.payload.Type(), err)
		}
	}
	return taskID
}

// TestSyntheticProjectReachesDoneDeterministically is the M1 exit criterion:
// a synthetic project can be driven through task states deterministically,
// with no model runtime involved.
func TestSyntheticProjectReachesDoneDeterministically(t *testing.T) {
	ctx := context.Background()

	render := func() string {
		t.Helper()
		h := testsupport.NewHarness(t)
		driveLifecycle(t, h)
		projectState, err := h.Service.ProjectState(ctx, "example")
		if err != nil {
			t.Fatalf("state: %v", err)
		}
		if projectState.Git.AcceptedCommit == nil || *projectState.Git.AcceptedCommit != "deadbeef7654321" {
			t.Fatalf("accepted commit = %v, want the integrated commit", projectState.Git.AcceptedCommit)
		}
		if projectState.Milestone.CompletedTasks != 1 {
			t.Fatalf("completed tasks = %d, want 1", projectState.Milestone.CompletedTasks)
		}
		document, err := protocol.CanonicalJSON(projectState)
		if err != nil {
			t.Fatalf("canonicalise: %v", err)
		}
		return string(document)
	}

	// Two independent runs with the same deterministic clock and identifier
	// source must produce byte-identical canonical state.
	first := render()
	second := render()
	if first != second {
		t.Fatalf("two runs of one scenario produced different state:\n%s\n%s", first, second)
	}
}

// TestAcceptedLifecycleRestsOnRealRecords checks the point of the exercise:
// after a full run to DONE, the evidence the journal cites is actually in the
// record store, not merely named by it.
func TestAcceptedLifecycleRestsOnRealRecords(t *testing.T) {
	ctx := context.Background()
	h := testsupport.NewHarness(t)
	driveLifecycle(t, h)

	for _, want := range []struct{ kind, id string }{
		{"EngineeringWorkPackage", "wp_000000000000000000000001"},
		{"ValidationResult", "val_0001"},
		{"ReviewResult", "rev_0001"},
		{"ValidationResult", "val_0002"},
	} {
		stored, err := h.Service.Record(ctx, "example", want.kind, want.id, 1)
		if err != nil {
			t.Fatalf("%s %s is cited by the journal but not stored: %v", want.kind, want.id, err)
		}
		if stored.Digest == "" {
			t.Fatalf("%s %s has no digest", want.kind, want.id)
		}
	}
}

// TestProjectionCanBeDestroyedAndRebuilt is DCI-053 end to end: the
// materialised view holds no information the journal does not.
func TestProjectionCanBeDestroyedAndRebuilt(t *testing.T) {
	ctx := context.Background()
	h := testsupport.NewHarness(t)
	taskID := driveLifecycle(t, h)

	before, err := h.Service.ProjectState(ctx, "example")
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	beforeDetail, err := h.Service.TaskDetail(ctx, "example", "DC-001")
	if err != nil {
		t.Fatalf("task detail: %v", err)
	}

	// Destroy the derived rows entirely, as docs/PROJECT_STATE.md §16
	// contemplates when the materialised state is lost or suspect.
	if err := h.Store.Write(ctx, func(tx *storage.Tx) error {
		return tx.DropProjection(ctx, "example")
	}); err != nil {
		t.Fatalf("drop projection: %v", err)
	}
	if _, err := h.Service.ProjectState(ctx, "example"); err == nil {
		t.Fatal("state was still available after the projection was dropped")
	}

	rebuilt, err := h.Service.RebuildProjection(ctx, "example")
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	beforeJSON, _ := protocol.CanonicalJSON(before)
	rebuiltJSON, _ := protocol.CanonicalJSON(rebuilt)
	if string(beforeJSON) != string(rebuiltJSON) {
		t.Fatalf("rebuilt state differs from the original:\n%s\n%s", rebuiltJSON, beforeJSON)
	}

	afterDetail, err := h.Service.TaskDetail(ctx, "example", "DC-001")
	if err != nil {
		t.Fatalf("task detail after rebuild: %v", err)
	}
	if afterDetail.Task.ID != taskID || afterDetail.Task.State != tasks.StateDone {
		t.Fatalf("rebuilt task = %+v", afterDetail.Task)
	}
	if len(afterDetail.Attempts) != len(beforeDetail.Attempts) {
		t.Fatalf("rebuilt attempts = %d, want %d", len(afterDetail.Attempts), len(beforeDetail.Attempts))
	}
	for i := range afterDetail.Attempts {
		a, _ := protocol.CanonicalJSON(afterDetail.Attempts[i])
		b, _ := protocol.CanonicalJSON(beforeDetail.Attempts[i])
		if string(a) != string(b) {
			t.Fatalf("rebuilt attempt %d differs:\n%s\n%s", i, a, b)
		}
	}
}

// TestHistoricalRevisionsAreReconstructable shows that any past ProjectState
// revision can be recovered from the journal prefix, which is what makes a
// Work Package's project_state_revision meaningful after the fact.
func TestHistoricalRevisionsAreReconstructable(t *testing.T) {
	ctx := context.Background()
	h := testsupport.NewHarness(t)
	driveLifecycle(t, h)

	stream, err := h.Service.Events(ctx, storage.EventQuery{ProjectID: "example"})
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	var snapshots []string
	for _, event := range stream {
		projectState, err := h.Service.ProjectStateAt(ctx, "example", event.Seq)
		if err != nil {
			t.Fatalf("state at %d: %v", event.Seq, err)
		}
		if projectState.EventHighWatermark == nil {
			t.Fatalf("state at %d has no high-watermark", event.Seq)
		}
		document, err := protocol.CanonicalJSON(projectState)
		if err != nil {
			t.Fatalf("canonicalise: %v", err)
		}
		snapshots = append(snapshots, string(document))
	}
	// Reconstructing the same revisions again must give the same documents.
	for i, event := range stream {
		projectState, err := h.Service.ProjectStateAt(ctx, "example", event.Seq)
		if err != nil {
			t.Fatalf("state at %d: %v", event.Seq, err)
		}
		document, _ := protocol.CanonicalJSON(projectState)
		if string(document) != snapshots[i] {
			t.Fatalf("revision at seq %d is not reproducible", event.Seq)
		}
	}
	// The final reconstruction must equal the materialised current state.
	current, err := h.Service.ProjectState(ctx, "example")
	if err != nil {
		t.Fatalf("current state: %v", err)
	}
	currentJSON, _ := protocol.CanonicalJSON(current)
	if string(currentJSON) != snapshots[len(snapshots)-1] {
		t.Fatal("the materialised state differs from the reduction of the whole journal")
	}
}

func TestRebuildRefusesAnUnknownProject(t *testing.T) {
	ctx := context.Background()
	h := testsupport.NewHarness(t)
	_, err := h.Service.RebuildProjection(ctx, "never-existed")
	if got := errs.CategoryOf(err); got != errs.CategoryNotFound {
		t.Fatalf("category = %s, want not_found (%v)", got, err)
	}
}
