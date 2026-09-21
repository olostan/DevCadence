package validation_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/olostan/DevCadience/internal/artifacts"
	"github.com/olostan/DevCadience/internal/controlplane"
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/events"
	"github.com/olostan/DevCadience/internal/ids"
	"github.com/olostan/DevCadience/internal/protocol"
	"github.com/olostan/DevCadience/internal/storage"
	"github.com/olostan/DevCadience/internal/tasks"
	"github.com/olostan/DevCadience/internal/testsupport"
	"github.com/olostan/DevCadience/internal/validation"
)

func initHarnessProject(t *testing.T, h *testsupport.Harness) {
	t.Helper()
	if _, err := h.Service.InitProject(context.Background(), controlplane.InitProjectInput{
		ProjectID: "example", Name: "Example", MilestoneID: "M2", MilestoneTitle: "Repository execution",
	}); err != nil {
		t.Fatalf("init project: %v", err)
	}
}

func TestExecuteAndRecordBaseline(t *testing.T) {
	h := testsupport.NewHarness(t)
	initHarnessProject(t, h)
	store, err := artifacts.NewStore(filepath.Join(t.TempDir(), "artifacts"), ids.NewSequential())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	fixture := testsupport.NewGitRepo(t)

	res, err := validation.ExecuteAndRecord(context.Background(), h.Service, validation.ExecuteInput{
		ProjectID: "example",
		Subject:   protocol.ValidationSubject{Kind: protocol.ScopeBaseline},
		Commit:    fixture.Head(),
		Profile: validation.Profile{
			Name:   "baseline",
			Checks: []validation.CheckSpec{{ID: "ok", Argv: []string{"true"}, Timeout: 5000000000}},
		},
		Run: validation.RunOptions{Dir: fixture.Path, Artifacts: store},
		IDs: h.IDs,
	})
	if err != nil {
		t.Fatalf("execute and record: %v", err)
	}
	if res.ValidationResult.Status != protocol.ValidationPass {
		t.Fatalf("status = %s", res.ValidationResult.Status)
	}
	if res.CommandResult.Event.EventType != events.TypeValidationCompleted {
		t.Fatalf("event type = %s", res.CommandResult.Event.EventType)
	}
	// The record is durably retrievable by the digest the event cites.
	stored, err := h.Service.Record(context.Background(), "example", "ValidationResult", res.ValidationResult.ValidationID, 1)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if stored.Digest != res.CommandResult.RecordDigests[res.ValidationResult.ValidationID] {
		t.Fatalf("digest mismatch")
	}
}

// driveTaskToValidating takes a fresh task all the way to VALIDATING with a
// candidate commit, which is what attempt-scope validation runs against
// (internal/state/reduce.go's applyAttemptValidation).
func driveTaskToValidating(t *testing.T, h *testsupport.Harness, candidate string) (taskID, attemptID string) {
	t.Helper()
	ctx := context.Background()
	initHarnessProject(t, h)
	if _, err := h.Service.CreateTask(ctx, controlplane.CreateTaskInput{
		ProjectID: "example", Alias: "DC-001", Title: "Repository execution",
		ChangeClass: protocol.ChangeSystemic,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	taskID, err := h.Service.ResolveTaskID(ctx, "example", "DC-001")
	if err != nil {
		t.Fatalf("resolve task: %v", err)
	}
	const wpID = "wp_0001"
	attemptID = "att_0001"
	workPackage := testsupport.WorkPackage("example", taskID, wpID, 1)

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
		{payload: &events.TaskDelegated{TaskID: taskID, WorkPackageID: wpID, WorkerRole: "implementer"}},
		{payload: &events.AttemptStarted{
			TaskID: taskID, AttemptID: attemptID, WorkPackageID: wpID, WorkPackageVersion: 1,
			ProjectStateRevision: "ps_000000003", WorkerRole: "implementer",
		}},
		{payload: &events.CandidateProduced{
			TaskID: taskID, AttemptID: attemptID, CandidateCommit: candidate, Summary: "candidate produced",
		}},
	}
	for _, step := range steps {
		if _, err := h.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
			ProjectID: "example", Payload: step.payload, Records: step.records,
		}); err != nil {
			t.Fatalf("append %s: %v", step.payload.Type(), err)
		}
	}
	return taskID, attemptID
}

func TestExecuteAndRecordAttemptScopeDrivesTaskToReviewing(t *testing.T) {
	h := testsupport.NewHarness(t)
	fixture := testsupport.NewGitRepo(t)
	candidate := fixture.Head()
	taskID, attemptID := driveTaskToValidating(t, h, candidate)

	store, err := artifacts.NewStore(filepath.Join(t.TempDir(), "artifacts"), ids.NewSequential())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	res, err := validation.ExecuteAndRecord(context.Background(), h.Service, validation.ExecuteInput{
		ProjectID: "example",
		Subject:   protocol.ValidationSubject{Kind: protocol.ScopeAttempt, TaskID: taskID, AttemptID: attemptID},
		Commit:    candidate,
		Profile: validation.Profile{
			Name:   "attempt",
			Checks: []validation.CheckSpec{{ID: "ok", Argv: []string{"true"}, Timeout: 5000000000}},
		},
		Run: validation.RunOptions{Dir: fixture.Path, Artifacts: store},
		IDs: h.IDs,
	})
	if err != nil {
		t.Fatalf("execute and record: %v", err)
	}
	detail, err := h.Service.TaskDetail(context.Background(), "example", "DC-001")
	if err != nil {
		t.Fatalf("task detail: %v", err)
	}
	if detail.Task.State != tasks.StateReviewing {
		t.Fatalf("task state = %s, want reviewing", detail.Task.State)
	}
	if res.ValidationResult.Status != protocol.ValidationPass {
		t.Fatalf("status = %s", res.ValidationResult.Status)
	}
}

func TestExecuteAndRecordAttemptScopeFailureReturnsToRunning(t *testing.T) {
	h := testsupport.NewHarness(t)
	fixture := testsupport.NewGitRepo(t)
	candidate := fixture.Head()
	taskID, attemptID := driveTaskToValidating(t, h, candidate)

	store, err := artifacts.NewStore(filepath.Join(t.TempDir(), "artifacts"), ids.NewSequential())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	if _, err := validation.ExecuteAndRecord(context.Background(), h.Service, validation.ExecuteInput{
		ProjectID: "example",
		Subject:   protocol.ValidationSubject{Kind: protocol.ScopeAttempt, TaskID: taskID, AttemptID: attemptID},
		Commit:    candidate,
		Profile: validation.Profile{
			Name:   "attempt",
			Checks: []validation.CheckSpec{{ID: "fails", Argv: []string{"false"}, Timeout: 5000000000}},
		},
		Run: validation.RunOptions{Dir: fixture.Path, Artifacts: store},
		IDs: h.IDs,
	}); err != nil {
		t.Fatalf("execute and record: %v", err)
	}
	detail, err := h.Service.TaskDetail(context.Background(), "example", "DC-001")
	if err != nil {
		t.Fatalf("task detail: %v", err)
	}
	if detail.Task.State != tasks.StateRunning {
		t.Fatalf("task state = %s, want running", detail.Task.State)
	}
}

// TestExecuteAndRecordWrongCommitRefused proves the adversarial boundary
// from docs/IMPLEMENTATION_PLAN.md M2 §26: a validation citing a commit that
// does not match the attempt's actual candidate must not silently advance
// the task, and nothing partial is left behind.
func TestExecuteAndRecordWrongCommitRefused(t *testing.T) {
	h := testsupport.NewHarness(t)
	fixture := testsupport.NewGitRepo(t)
	candidate := fixture.Head()
	taskID, attemptID := driveTaskToValidating(t, h, candidate)

	// Advance the fixture's real HEAD past the recorded candidate, so the
	// claimed commit below is a genuine, verifiable HEAD (passing the new
	// dir-matches-claim check in ExecuteAndRecord) that nonetheless disagrees
	// with the attempt's actual recorded candidate (tripping the existing
	// candidate cross-check further downstream).
	fixture.WriteFile("advance.txt", "advance past the candidate\n")
	wrongButReal := fixture.Commit("advance past the candidate")

	store, err := artifacts.NewStore(filepath.Join(t.TempDir(), "artifacts"), ids.NewSequential())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	before, err := h.Service.Events(context.Background(), storage.EventQuery{ProjectID: "example"})
	if err != nil {
		t.Fatalf("events: %v", err)
	}

	_, err = validation.ExecuteAndRecord(context.Background(), h.Service, validation.ExecuteInput{
		ProjectID: "example",
		Subject:   protocol.ValidationSubject{Kind: protocol.ScopeAttempt, TaskID: taskID, AttemptID: attemptID},
		Commit:    wrongButReal, // not the recorded candidate
		Profile: validation.Profile{
			Name:   "attempt",
			Checks: []validation.CheckSpec{{ID: "ok", Argv: []string{"true"}, Timeout: 5000000000}},
		},
		Run: validation.RunOptions{Dir: fixture.Path, Artifacts: store},
		IDs: h.IDs,
	})
	if err == nil {
		t.Fatal("expected the wrong-commit validation to be refused")
	}
	if errs.CategoryOf(err) != errs.CategoryIntegrity {
		t.Fatalf("category = %v", errs.CategoryOf(err))
	}
	after, err := h.Service.Events(context.Background(), storage.EventQuery{ProjectID: "example"})
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(before) != len(after) {
		t.Fatalf("journal grew from %d to %d after a refused validation", len(before), len(after))
	}
	detail, err := h.Service.TaskDetail(context.Background(), "example", "DC-001")
	if err != nil {
		t.Fatalf("task detail: %v", err)
	}
	if detail.Task.State != tasks.StateValidating {
		t.Fatalf("task state = %s after refused validation, want validating unchanged", detail.Task.State)
	}
}

// TestExecuteAndRecordCommitNotDirHeadRefused proves ExecuteAndRecord binds
// execution to the actual repository state: a caller-claimed Commit that
// does not match Run.Dir's real Git HEAD must be refused before anything is
// run or persisted, rather than trusting the claim at face value.
func TestExecuteAndRecordCommitNotDirHeadRefused(t *testing.T) {
	h := testsupport.NewHarness(t)
	initHarnessProject(t, h)
	fixture := testsupport.NewGitRepo(t)
	store, err := artifacts.NewStore(filepath.Join(t.TempDir(), "artifacts"), ids.NewSequential())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	_, err = validation.ExecuteAndRecord(context.Background(), h.Service, validation.ExecuteInput{
		ProjectID: "example",
		Subject:   protocol.ValidationSubject{Kind: protocol.ScopeBaseline},
		Commit:    "0000000000000000000000000000000000000000", // not fixture's real HEAD
		Profile: validation.Profile{
			Name:   "baseline",
			Checks: []validation.CheckSpec{{ID: "ok", Argv: []string{"true"}, Timeout: 5000000000}},
		},
		Run: validation.RunOptions{Dir: fixture.Path, Artifacts: store},
		IDs: h.IDs,
	})
	if err == nil {
		t.Fatal("expected a claimed commit that is not the dir's real HEAD to be refused")
	}
	if errs.CategoryOf(err) != errs.CategoryIntegrity {
		t.Fatalf("category = %v", errs.CategoryOf(err))
	}
}

// TestExecuteAndRecordDirNotAGitRepoRefused proves a validation run against a
// directory that is not a Git repository at all is refused rather than
// silently accepted as satisfying an arbitrary claimed commit.
func TestExecuteAndRecordDirNotAGitRepoRefused(t *testing.T) {
	h := testsupport.NewHarness(t)
	initHarnessProject(t, h)
	store, err := artifacts.NewStore(filepath.Join(t.TempDir(), "artifacts"), ids.NewSequential())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	_, err = validation.ExecuteAndRecord(context.Background(), h.Service, validation.ExecuteInput{
		ProjectID: "example",
		Subject:   protocol.ValidationSubject{Kind: protocol.ScopeBaseline},
		Commit:    "cafebabe0000000000000000000000000000000",
		Profile: validation.Profile{
			Name:   "baseline",
			Checks: []validation.CheckSpec{{ID: "ok", Argv: []string{"true"}, Timeout: 5000000000}},
		},
		Run: validation.RunOptions{Dir: t.TempDir(), Artifacts: store},
		IDs: h.IDs,
	})
	if err == nil {
		t.Fatal("expected validation against a non-git dir to be refused")
	}
}
