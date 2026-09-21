package controlplane_test

import (
	"context"
	"strings"
	"testing"

	"github.com/olostan/DevCadience/internal/controlplane"
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/events"
	"github.com/olostan/DevCadience/internal/protocol"
	"github.com/olostan/DevCadience/internal/storage"
	"github.com/olostan/DevCadience/internal/tasks"
	"github.com/olostan/DevCadience/internal/testsupport"
)

// TestBrokenLineageLeavesPersistenceUntouched is the durable half of the
// lineage guarantee. The reducer refusing an event is only useful if the
// refusal also rolls back the append and the projection update, so that a
// rejected acceptance leaves no trace that could later be read as one.
func TestBrokenLineageLeavesPersistenceUntouched(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name    string
		payload func(taskID, attemptID, wpID, commit string) events.Payload
	}{
		{
			name: "acceptance citing an unknown validation",
			payload: func(taskID, attemptID, wpID, commit string) events.Payload {
				return &events.ChangeAccepted{
					TaskID: taskID, AttemptID: attemptID, WorkPackageID: wpID,
					CandidateCommit: commit, SemanticSummary: "s",
					ValidationIDs: []string{"val_never_recorded"},
					DecidedBy:     protocol.AuthorityPrincipal,
				}
			},
		},
		{
			name: "acceptance citing the wrong commit",
			payload: func(taskID, attemptID, wpID, _ string) events.Payload {
				return &events.ChangeAccepted{
					TaskID: taskID, AttemptID: attemptID, WorkPackageID: wpID,
					CandidateCommit: "0000000deadbee", SemanticSummary: "s",
					ValidationIDs: []string{"val_0001"},
					DecidedBy:     protocol.AuthorityPrincipal,
				}
			},
		},
		{
			name: "acceptance naming the wrong work package",
			payload: func(taskID, attemptID, _, commit string) events.Payload {
				return &events.ChangeAccepted{
					TaskID: taskID, AttemptID: attemptID, WorkPackageID: "wp_somebody_elses",
					CandidateCommit: commit, SemanticSummary: "s",
					ValidationIDs: []string{"val_0001"},
					DecidedBy:     protocol.AuthorityPrincipal,
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := testsupport.NewHarness(t)
			taskID := driveToReviewing(t, h)

			before := snapshot(t, h)
			_, err := h.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
				ProjectID: "example",
				Payload:   tc.payload(taskID, "att_0001", "wp_0001", "cafebabe1234567"),
			})
			if err == nil {
				t.Fatal("an acceptance with broken lineage was committed")
			}
			after := snapshot(t, h)
			if before.watermark != after.watermark {
				t.Fatalf("the journal grew from %d to %d after a refused acceptance",
					before.watermark, after.watermark)
			}
			if before.revision != after.revision {
				t.Fatalf("the state revision moved from %s to %s after a refused acceptance",
					before.revision, after.revision)
			}
			if after.taskState != tasks.StateReviewing {
				t.Fatalf("task state = %s after a refused acceptance, want reviewing", after.taskState)
			}
		})
	}
}

type stateSnapshot struct {
	watermark int
	revision  string
	taskState tasks.State
}

func snapshot(t *testing.T, h *testsupport.Harness) stateSnapshot {
	t.Helper()
	ctx := context.Background()
	stream, err := h.Service.Events(ctx, storage.EventQuery{ProjectID: "example"})
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	projectState, err := h.Service.ProjectState(ctx, "example")
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	detail, err := h.Service.TaskDetail(ctx, "example", "DC-001")
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	return stateSnapshot{
		watermark: len(stream),
		revision:  projectState.StateRevision,
		taskState: detail.Task.State,
	}
}

// driveToReviewing takes a task through to REVIEWING with one validation and
// one review recorded, which is the state acceptance acts on.
func driveToReviewing(t *testing.T, h *testsupport.Harness) string {
	t.Helper()
	ctx := context.Background()
	initProject(t, h)
	if _, err := h.Service.CreateTask(ctx, controlplane.CreateTaskInput{
		ProjectID: "example", Alias: "DC-001", Title: "Bounded reads",
		ChangeClass: protocol.ChangeSystemic,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	taskID := mustTaskID(t, h, "DC-001")
	const (
		wpID      = "wp_0001"
		attemptID = "att_0001"
		candidate = "cafebabe1234567"
	)
	// The evidence these events reference is written with them, so the
	// lineage failures under test are the only thing wrong in each case.
	workPackage := testsupport.WorkPackage("example", taskID, wpID, 1)
	validation := testsupport.AttemptValidation(
		"example", "val_0001", taskID, attemptID, candidate, protocol.ValidationPass)
	review := testsupport.Review(
		"example", "rev_0001", attemptID, wpID, protocol.DimensionCorrectness, protocol.VerdictPass)

	for _, step := range []struct {
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
		{payload: &events.TaskDelegated{TaskID: taskID, WorkPackageID: wpID, WorkerRole: "implementer"}},
		{payload: &events.AttemptStarted{
			TaskID: taskID, AttemptID: attemptID, WorkPackageID: wpID, WorkPackageVersion: 1,
			ProjectStateRevision: "ps_000000003", WorkerRole: "implementer",
		}},
		{payload: &events.CandidateProduced{
			TaskID: taskID, AttemptID: attemptID, CandidateCommit: candidate, Summary: "done",
		}},
		{
			payload: &events.ValidationCompleted{
				TaskID: taskID, AttemptID: attemptID, ValidationID: "val_0001",
				Scope: events.ScopeAttempt, Status: protocol.ValidationPass,
				Commit: candidate, RecordDigest: testsupport.Digest(t, validation),
			},
			records: []controlplane.RecordToStore{{Version: 1, Record: validation}},
		},
		{
			payload: &events.ReviewCompleted{
				TaskID: taskID, AttemptID: attemptID, ReviewID: "rev_0001", WorkPackageID: wpID,
				Dimension: protocol.DimensionCorrectness, Verdict: protocol.VerdictPass,
				RecordDigest: testsupport.Digest(t, review),
			},
			records: []controlplane.RecordToStore{{Version: 1, Record: review}},
		},
	} {
		if _, err := h.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
			ProjectID: "example", Payload: step.payload, Records: step.records,
		}); err != nil {
			t.Fatalf("append %s: %v", step.payload.Type(), err)
		}
	}
	return taskID
}

// TestEventCorrelationCannotContradictPayload stops an adapter attributing a
// state change to a task other than the one the payload names.
func TestEventCorrelationCannotContradictPayload(t *testing.T) {
	ctx := context.Background()
	h := testsupport.NewHarness(t)
	initProject(t, h)
	if _, err := h.Service.CreateTask(ctx, controlplane.CreateTaskInput{
		ProjectID: "example", Alias: "DC-001", Title: "Bounded reads",
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	taskID := mustTaskID(t, h, "DC-001")

	_, err := h.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID:   "example",
		Payload:     &events.TaskDesignStarted{TaskID: taskID, Reason: "initial design"},
		Correlation: events.Correlation{TaskID: "tsk_somebody_elses"},
	})
	if err == nil {
		t.Fatal("an event was appended with a correlation contradicting its payload")
	}
	if got := errs.CategoryOf(err); got != errs.CategoryInvalidArgument {
		t.Fatalf("category = %s, want invalid_argument (%v)", got, err)
	}
	if !strings.Contains(err.Error(), "task_id") {
		t.Fatalf("error does not name the contradicting field: %v", err)
	}
}

// TestEmptyAdapterCorrelationStillYieldsCompleteTaskHistory is the other half:
// an adapter that supplies nothing must not produce events that vanish from
// their own task's history, because the journal indexes correlation columns.
func TestEmptyAdapterCorrelationStillYieldsCompleteTaskHistory(t *testing.T) {
	ctx := context.Background()
	h := testsupport.NewHarness(t)
	driveToReviewing(t, h) // every append above supplies no correlation

	detail, err := h.Service.TaskDetail(ctx, "example", "DC-001")
	if err != nil {
		t.Fatalf("task detail: %v", err)
	}
	// TaskCreated plus the seven lifecycle events driveToReviewing appends.
	const wantEvents = 8
	if len(detail.History) != wantEvents {
		t.Fatalf("task history holds %d events, want %d; correlation was not derived for some",
			len(detail.History), wantEvents)
	}
	for _, event := range detail.History {
		if event.Correlation.TaskID != detail.Task.ID {
			t.Fatalf("event %s is in the task's history with correlation task %q",
				event.EventType, event.Correlation.TaskID)
		}
	}
	// Attempt-scoped events must also carry the attempt, which is what
	// attempt-level observability reads.
	for _, event := range detail.History {
		switch event.EventType {
		case events.TypeAttemptStarted, events.TypeCandidateProduced,
			events.TypeValidationCompleted, events.TypeReviewCompleted:
			if event.Correlation.AttemptID == "" {
				t.Errorf("%s carries no attempt correlation", event.EventType)
			}
		}
	}
}

// TestCallerMaySupplyCorrelationThePayloadCannotExpress keeps the merge
// useful: agent_run_id has no payload field, so the adapter is its only
// source and must still be honoured.
func TestCallerMaySupplyCorrelationThePayloadCannotExpress(t *testing.T) {
	ctx := context.Background()
	h := testsupport.NewHarness(t)
	initProject(t, h)
	if _, err := h.Service.CreateTask(ctx, controlplane.CreateTaskInput{
		ProjectID: "example", Alias: "DC-001", Title: "Bounded reads",
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	taskID := mustTaskID(t, h, "DC-001")

	result, err := h.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID:   "example",
		Payload:     &events.TaskDesignStarted{TaskID: taskID, Reason: "initial design"},
		Correlation: events.Correlation{AgentRunID: "run_42"},
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if result.Event.Correlation.AgentRunID != "run_42" {
		t.Fatalf("agent_run_id = %q, want run_42", result.Event.Correlation.AgentRunID)
	}
	if result.Event.Correlation.TaskID != taskID {
		t.Fatalf("task correlation = %q, want the payload's task", result.Event.Correlation.TaskID)
	}
}
