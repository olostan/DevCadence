package controlplane_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/olostan/DevCadience/internal/controlplane"
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/events"
	"github.com/olostan/DevCadience/internal/protocol"
	"github.com/olostan/DevCadience/internal/storage"
	"github.com/olostan/DevCadience/internal/tasks"
	"github.com/olostan/DevCadience/internal/testsupport"
)

func initProject(t *testing.T, h *testsupport.Harness) {
	t.Helper()
	if _, err := h.Service.InitProject(context.Background(), controlplane.InitProjectInput{
		ProjectID:      "example",
		Name:           "Example",
		MilestoneID:    "M1",
		MilestoneTitle: "Domain core and canonical state",
	}); err != nil {
		t.Fatalf("init project: %v", err)
	}
}

func TestProjectMustBeInitialisedFirst(t *testing.T) {
	ctx := context.Background()
	h := testsupport.NewHarness(t)

	_, err := h.Service.CreateTask(ctx, controlplane.CreateTaskInput{
		ProjectID: "example", Alias: "DC-001", Title: "x",
	})
	if err == nil {
		t.Fatal("a task was created in an uninitialised project")
	}
	if got := errs.CategoryOf(err); got != errs.CategoryNotFound {
		t.Fatalf("category = %s, want not_found (%v)", got, err)
	}
}

func TestProjectIDIsValidated(t *testing.T) {
	ctx := context.Background()
	h := testsupport.NewHarness(t)
	// Identifiers reach file names and log fields; separators and traversal
	// sequences are refused at the boundary rather than by every consumer.
	for _, id := range []string{"", "Has-Capitals", "has spaces", "../escape", "a/b", "a\x00b"} {
		_, err := h.Service.InitProject(ctx, controlplane.InitProjectInput{
			ProjectID: id, MilestoneID: "M1", MilestoneTitle: "t",
		})
		if err == nil {
			t.Errorf("project id %q was accepted", id)
		}
	}
}

func TestRelativeRepositoryPathIsRefused(t *testing.T) {
	ctx := context.Background()
	h := testsupport.NewHarness(t)
	_, err := h.Service.InitProject(ctx, controlplane.InitProjectInput{
		ProjectID: "example", MilestoneID: "M1", MilestoneTitle: "t",
		RepositoryPath: "relative/path",
	})
	if err == nil {
		t.Fatal("a relative repository path was accepted")
	}
}

// TestIllegalTransitionLeavesPersistenceUntouched is the requirement of
// ENGINEERING_STANDARDS.md §12: a refused transition must not commit the
// event that attempted it.
func TestIllegalTransitionLeavesPersistenceUntouched(t *testing.T) {
	ctx := context.Background()
	h := testsupport.NewHarness(t)
	initProject(t, h)
	result, err := h.Service.CreateTask(ctx, controlplane.CreateTaskInput{
		ProjectID: "example", Alias: "DC-001", Title: "Bounded reads",
		ChangeClass: protocol.ChangeSystemic,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	taskID := result.Event.Payload.(*events.TaskCreated).TaskID
	watermarkBefore := result.Event.Seq
	revisionBefore := result.ProjectState.StateRevision

	// PROPOSED -> RUNNING is not an edge of the lifecycle.
	_, err = h.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: "example",
		Payload: &events.TaskDelegated{
			TaskID: taskID, WorkPackageID: "wp_1", WorkerRole: "implementer",
		},
	})
	if err == nil {
		t.Fatal("an illegal transition was committed")
	}
	if got := errs.CategoryOf(err); got != errs.CategoryInvalidTransition {
		t.Fatalf("category = %s, want invalid_transition (%v)", got, err)
	}

	// Nothing was appended, and the materialised state did not move.
	stream, err := h.Service.Events(ctx, storage.EventQuery{ProjectID: "example"})
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if int64(len(stream)) != watermarkBefore {
		t.Fatalf("journal holds %d events, want %d; the refused event was committed",
			len(stream), watermarkBefore)
	}
	projectState, err := h.Service.ProjectState(ctx, "example")
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if projectState.StateRevision != revisionBefore {
		t.Fatalf("state revision moved to %s after a refused transition", projectState.StateRevision)
	}
	detail, err := h.Service.TaskDetail(ctx, "example", "DC-001")
	if err != nil {
		t.Fatalf("task detail: %v", err)
	}
	if detail.Task.State != tasks.StateProposed {
		t.Fatalf("task state = %s after a refused transition, want proposed", detail.Task.State)
	}
}

// TestWorkPackageAndItsEventCommitTogether checks the other half of the
// transaction boundary: a rejected event must not leave its record behind.
func TestWorkPackageAndItsEventCommitTogether(t *testing.T) {
	ctx := context.Background()
	h := testsupport.NewHarness(t)
	initProject(t, h)
	if _, err := h.Service.CreateTask(ctx, controlplane.CreateTaskInput{
		ProjectID: "example", Alias: "DC-001", Title: "Bounded reads",
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}

	workPackage := &protocol.EngineeringWorkPackage{
		WorkPackageID:        "wp_000000000000000000000001",
		Version:              1,
		ProjectStateRevision: "ps_000000002",
		BaseCommit:           "91acd8273f1",
		ChangeClass:          protocol.ChangeSystemic,
		Objective:            "Bounded journal reads",
		Rationale:            "The journal grows without bound.",
		ArchitecturalIntent:  "Keep the journal append-only.",
		Scope:                protocol.Scope{InScope: []string{"storage"}, OutOfScope: []string{"envelope"}},
		Guidance: []protocol.Guidance{
			{ID: "G1", Strength: protocol.GuidanceMust, Statement: "The journal stays append-only."},
		},
		AcceptanceCriteria:     []string{"Bounded reads work."},
		ValidationRequirements: []string{"go test ./..."},
		EscalationConditions:   []string{"Assumption A2 is false."},
	}

	// The task is PROPOSED, so approval (which requires DESIGNING) is
	// refused — and the Work Package record must not survive the rollback.
	_, err := h.Service.ApproveWorkPackage(ctx, controlplane.ApproveWorkPackageInput{
		ProjectID: "example", TaskAlias: "DC-001", WorkPackage: workPackage,
	})
	if err == nil {
		t.Fatal("a work package was approved for a task that is not in design")
	}
	if _, err := h.Service.Record(ctx, "EngineeringWorkPackage", workPackage.WorkPackageID, 1); err == nil {
		t.Fatal("the work package record survived the rolled-back approval")
	}

	// Move the task into design and approve again; now both must be durable.
	if _, err := h.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: "example",
		Payload: &events.TaskDesignStarted{
			TaskID: mustTaskID(t, h, "DC-001"), Reason: "initial design",
		},
	}); err != nil {
		t.Fatalf("start design: %v", err)
	}
	if _, err := h.Service.ApproveWorkPackage(ctx, controlplane.ApproveWorkPackageInput{
		ProjectID: "example", TaskAlias: "DC-001", WorkPackage: workPackage,
	}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	stored, err := h.Service.Record(ctx, "EngineeringWorkPackage", workPackage.WorkPackageID, 1)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	var decoded protocol.EngineeringWorkPackage
	if err := protocol.Unmarshal([]byte(stored.Document), &decoded); err != nil {
		t.Fatalf("decode stored work package: %v", err)
	}
	if decoded.Objective != workPackage.Objective {
		t.Fatalf("stored objective = %q", decoded.Objective)
	}
	// The approving event's digest must match the record it points at.
	stream, err := h.Service.Events(ctx, storage.EventQuery{
		ProjectID: "example", Types: []events.Type{events.TypeWorkPackageApproved},
	})
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(stream) != 1 {
		t.Fatalf("approval events = %d, want 1", len(stream))
	}
	approved := stream[0].Payload.(*events.WorkPackageApproved)
	if approved.RecordDigest != stored.Digest {
		t.Fatalf("event digest %s does not match the stored record digest %s",
			approved.RecordDigest, stored.Digest)
	}
	detail, err := h.Service.TaskDetail(ctx, "example", "DC-001")
	if err != nil {
		t.Fatalf("task detail: %v", err)
	}
	if detail.Task.State != tasks.StateReady {
		t.Fatalf("task state = %s, want ready", detail.Task.State)
	}
}

func TestDuplicateTaskAliasIsRefused(t *testing.T) {
	ctx := context.Background()
	h := testsupport.NewHarness(t)
	initProject(t, h)
	if _, err := h.Service.CreateTask(ctx, controlplane.CreateTaskInput{
		ProjectID: "example", Alias: "DC-001", Title: "first",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err := h.Service.CreateTask(ctx, controlplane.CreateTaskInput{
		ProjectID: "example", Alias: "DC-001", Title: "second",
	})
	if err == nil {
		t.Fatal("a duplicate task alias was accepted; principal references would be ambiguous")
	}
	if got := errs.CategoryOf(err); got != errs.CategoryConflict {
		t.Fatalf("category = %s, want conflict (%v)", got, err)
	}
}

// TestPersistedDatabaseReopensWithItsState is the durability guarantee: the
// control plane must be able to stop and resume.
func TestPersistedDatabaseReopensWithItsState(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "control-plane.db")

	func() {
		h := testsupport.NewFileHarness(t, path)
		initProject(t, h)
		if _, err := h.Service.CreateTask(ctx, controlplane.CreateTaskInput{
			ProjectID: "example", Alias: "DC-001", Title: "Bounded reads",
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}()

	reopened := testsupport.NewFileHarness(t, path)
	projectState, err := reopened.Service.ProjectState(ctx, "example")
	if err != nil {
		t.Fatalf("state after reopen: %v", err)
	}
	if projectState.Milestone.TotalTasks != 1 {
		t.Fatalf("reopened state reports %d tasks, want 1", projectState.Milestone.TotalTasks)
	}
	detail, err := reopened.Service.TaskDetail(ctx, "example", "DC-001")
	if err != nil {
		t.Fatalf("task after reopen: %v", err)
	}
	if detail.Task.Title != "Bounded reads" {
		t.Fatalf("task title = %q after reopen", detail.Task.Title)
	}
}

func mustTaskID(t *testing.T, h *testsupport.Harness, alias string) string {
	t.Helper()
	id, err := h.Service.ResolveTaskID(context.Background(), "example", alias)
	if err != nil {
		t.Fatalf("resolve %s: %v", alias, err)
	}
	return id
}

// TestConcurrentAppendsSerialiseConsistently checks that the transaction
// boundary holds under concurrency.
//
// The control plane is single-operator by design (ADR-0002), but the CLI, a
// future MCP adapter and a future scheduler can all call Apply, so two
// callers reaching it at once must not interleave. Each append must see the
// journal the previous one committed, or a projection could be built from a
// stale reduction.
func TestConcurrentAppendsSerialiseConsistently(t *testing.T) {
	ctx := context.Background()
	h := testsupport.NewHarness(t)
	initProject(t, h)

	const workers = 8
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := h.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
				ProjectID: "example",
				Payload: &events.RiskRecorded{
					RiskID:    fmt.Sprintf("R-%03d", n),
					Severity:  protocol.SeverityLow,
					Statement: fmt.Sprintf("risk %d", n),
				},
			})
			errCh <- err
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent append: %v", err)
		}
	}

	// Every risk is present exactly once, sequences are gap-free, and the
	// materialised state agrees with a fresh reduction of the journal.
	projectState, err := h.Service.ProjectState(ctx, "example")
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if len(projectState.Risks) != workers {
		t.Fatalf("risks = %d, want %d", len(projectState.Risks), workers)
	}
	stream, err := h.Service.Events(ctx, storage.EventQuery{ProjectID: "example"})
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(stream) != workers+1 {
		t.Fatalf("journal holds %d events, want %d", len(stream), workers+1)
	}
	for i, event := range stream {
		if event.Seq != int64(i+1) {
			t.Fatalf("journal sequences have a gap at position %d: %d", i, event.Seq)
		}
	}
	rebuilt, err := h.Service.ProjectStateAt(ctx, "example", stream[len(stream)-1].Seq)
	if err != nil {
		t.Fatalf("reduce journal: %v", err)
	}
	current, _ := protocol.CanonicalJSON(projectState)
	replayed, _ := protocol.CanonicalJSON(rebuilt)
	if string(current) != string(replayed) {
		t.Fatalf("materialised state disagrees with the journal after concurrent appends:\n%s\n%s",
			current, replayed)
	}
}
