package controlplane_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/observability"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/storage"
	"github.com/olostan/DevCadence/internal/tasks"
	"github.com/olostan/DevCadence/internal/testsupport"

	_ "modernc.org/sqlite" // raw handle used to simulate an out-of-band edit
)

var batchActor = protocol.Actor{Kind: protocol.ActorControlPlane, ID: "devcadence"}

// planned is an approved, READY task and the exact tuple that approved it.
type planned struct {
	taskID string
	wpID   string
	wp     *protocol.EngineeringWorkPackage
	digest string
}

func (p planned) guard() *controlplane.WorkPackageGuard {
	return &controlplane.WorkPackageGuard{
		TaskID: p.taskID, WorkPackageID: p.wpID, Version: p.wp.Version,
		Digest: p.digest, BaseCommit: p.wp.BaseCommit,
	}
}

// readyTask initialises "example" (optionally repository-backed), creates
// DC-001 and approves wp_0001 v1.
func readyTask(t *testing.T, svc *controlplane.Service, init controlplane.InitProjectInput) planned {
	t.Helper()
	ctx := context.Background()
	init.ProjectID, init.MilestoneID, init.MilestoneTitle = "example", "M1", "Domain core"
	if _, err := svc.InitProject(ctx, init); err != nil {
		t.Fatalf("init project: %v", err)
	}
	if _, err := svc.CreateTask(ctx, controlplane.CreateTaskInput{
		ProjectID: "example", Alias: "DC-001", Title: "Bounded reads", ChangeClass: protocol.ChangeSystemic,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	taskID, err := svc.ResolveTaskID(ctx, "example", "DC-001")
	if err != nil {
		t.Fatal(err)
	}
	mustAppend(t, svc, &events.TaskDesignStarted{TaskID: taskID, Reason: "initial design"}, nil)
	return approve(t, svc, taskID, "wp_0001", 1)
}

func approve(t *testing.T, svc *controlplane.Service, taskID, wpID string, version int) planned {
	t.Helper()
	wp := testsupport.WorkPackage("example", taskID, wpID, version)
	digest, err := protocol.Digest(wp)
	if err != nil {
		t.Fatal(err)
	}
	mustAppend(t, svc, &events.WorkPackageApproved{
		TaskID: taskID, WorkPackageID: wpID, WorkPackageVersion: version, RecordDigest: digest,
		ProjectStateRevision: wp.ProjectStateRevision, BaseCommit: wp.BaseCommit, ChangeClass: wp.ChangeClass,
	}, []controlplane.RecordToStore{{Version: version, Record: wp}})
	return planned{taskID: taskID, wpID: wpID, wp: wp, digest: digest}
}

func mustAppend(t *testing.T, svc *controlplane.Service, payload events.Payload, records []controlplane.RecordToStore) {
	t.Helper()
	if _, err := svc.AppendTypedEvent(context.Background(), controlplane.AppendTypedEventInput{
		ProjectID: "example", Payload: payload, Records: records,
	}); err != nil {
		t.Fatalf("append %s: %v", payload.Type(), err)
	}
}

func revisionOf(t *testing.T, svc *controlplane.Service) string {
	t.Helper()
	projectState, err := svc.ProjectState(context.Background(), "example")
	if err != nil {
		t.Fatalf("project state: %v", err)
	}
	return projectState.StateRevision
}

func start(p planned, attemptID string) []controlplane.Command {
	return []controlplane.Command{
		{ProjectID: "example", Actor: batchActor, Payload: &events.TaskDelegated{
			TaskID: p.taskID, WorkPackageID: p.wpID, WorkerRole: "implementer",
		}},
		{ProjectID: "example", Actor: batchActor, Payload: &events.AttemptStarted{
			TaskID: p.taskID, AttemptID: attemptID, WorkPackageID: p.wpID, WorkPackageVersion: p.wp.Version,
			ProjectStateRevision: "ps_000000003", BaseCommit: p.wp.BaseCommit, WorkerRole: "implementer",
		}},
	}
}

func risk(id string) controlplane.Command {
	return controlplane.Command{ProjectID: "example", Actor: batchActor, Payload: &events.RiskRecorded{
		RiskID: id, Severity: protocol.SeverityLow, Statement: "risk " + id,
	}}
}

// durable captures everything a refused batch must leave untouched.
type durable struct {
	events   int
	revision string
	state    string
	records  map[string]bool
}

func capture(t *testing.T, svc *controlplane.Service, probes ...[4]any) durable {
	t.Helper()
	ctx := context.Background()
	stream, err := svc.Events(ctx, storage.EventQuery{ProjectID: "example"})
	if err != nil {
		t.Fatal(err)
	}
	projectState, err := svc.ProjectState(ctx, "example")
	if err != nil {
		t.Fatal(err)
	}
	canonical, _ := protocol.CanonicalJSON(projectState)
	d := durable{events: len(stream), revision: projectState.StateRevision, state: string(canonical), records: map[string]bool{}}
	for _, probe := range probes {
		_, err := svc.Record(ctx, "example", probe[0].(string), probe[1].(string), probe[2].(int))
		d.records[fmt.Sprint(probe[:3])] = err == nil
	}
	return d
}

func requireUnchanged(t *testing.T, before, after durable) {
	t.Helper()
	if before.events != after.events || before.revision != after.revision || before.state != after.state {
		t.Fatalf("a refused batch changed durable state: events %d->%d revision %s->%s",
			before.events, after.events, before.revision, after.revision)
	}
	for key, existed := range before.records {
		if after.records[key] != existed {
			t.Fatalf("a refused batch changed record %s (before %v, after %v)", key, existed, after.records[key])
		}
	}
}

func openService(t *testing.T, path string) *controlplane.Service {
	t.Helper()
	store, err := storage.Open(context.Background(), storage.Config{Path: path, Clock: testsupport.NewClock()})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc, err := controlplane.New(controlplane.Options{
		Store: store, Clock: testsupport.NewClock(), IDs: ids.NewULIDSource(),
		Logger: observability.NewLogger(observability.Options{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

type guardFunc func(context.Context, controlplane.BatchReadView) error

func (g guardFunc) Check(ctx context.Context, v controlplane.BatchReadView) error { return g(ctx, v) }

// A2: two independent services share a database and a prefix; exactly one wins.
func TestA2_ConcurrentBatchesAtOnePrefixCommitExactlyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a2.db")
	one, two := openService(t, path), openService(t, path)
	if _, err := one.InitProject(context.Background(), controlplane.InitProjectInput{
		ProjectID: "example", MilestoneID: "M1", MilestoneTitle: "Domain core",
	}); err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 6; round++ {
		prefix := revisionOf(t, one)
		before := capture(t, one)
		var wg sync.WaitGroup
		barrier := make(chan struct{})
		errsOut := make([]error, 2)
		for i, svc := range []*controlplane.Service{one, two} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-barrier
				_, errsOut[i] = svc.ApplyBatch(context.Background(), controlplane.BatchCommand{
					ProjectID: "example", Actor: batchActor, ExpectedStateRevision: prefix,
					Commands: []controlplane.Command{risk(fmt.Sprintf("R-%d-%d", round, i))},
				})
			}()
		}
		close(barrier)
		wg.Wait()
		wins, stale := 0, 0
		for _, err := range errsOut {
			switch {
			case err == nil:
				wins++
			case errors.Is(err, controlplane.ErrStaleProjectState):
				stale++
			default:
				t.Fatalf("round %d: unexpected error %v", round, err)
			}
		}
		if wins != 1 || stale != 1 {
			t.Fatalf("round %d: %d committed and %d stale; want exactly one of each", round, wins, stale)
		}
		after := capture(t, one)
		if after.events != before.events+1 {
			t.Fatalf("round %d: %d events committed, want exactly 1", round, after.events-before.events)
		}
	}
}

func TestStaleErrorsAreDistinctConflicts(t *testing.T) {
	h := testsupport.NewHarness(t)
	initProject(t, h)
	_, err := h.Service.ApplyBatch(context.Background(), controlplane.BatchCommand{
		ProjectID: "example", Actor: batchActor, ExpectedStateRevision: "ps_000000099",
		Commands: []controlplane.Command{risk("R-1")},
	})
	if !errors.Is(err, controlplane.ErrStaleProjectState) {
		t.Fatalf("err = %v, want ErrStaleProjectState", err)
	}
	if errs.CategoryOf(err) != errs.CategoryConflict {
		t.Fatalf("category = %s, want conflict", errs.CategoryOf(err))
	}
	if errors.Is(err, controlplane.ErrStaleWorkPackage) || errors.Is(err, controlplane.ErrStorageBusy) {
		t.Fatal("a stale prefix must not match the other conflict identities")
	}
	if !errors.Is(controlplane.ErrStaleWorkPackage, errs.ErrConflict) {
		t.Fatal("ErrStaleWorkPackage is not a CategoryConflict")
	}
}

func TestBatchStructuralBounds(t *testing.T) {
	h := testsupport.NewHarness(t)
	initProject(t, h)
	prefix := revisionOf(t, h.Service)
	base := func() controlplane.BatchCommand {
		return controlplane.BatchCommand{
			ProjectID: "example", Actor: batchActor, ExpectedStateRevision: prefix,
			Commands: []controlplane.Command{risk("R-1")},
		}
	}
	tooMany := make([]controlplane.Command, controlplane.MaxBatchCommands+1)
	for i := range tooMany {
		tooMany[i] = risk(fmt.Sprintf("R-%d", i))
	}
	nine := make([]controlplane.BatchGuard, controlplane.MaxBatchGuards+1)
	for i := range nine {
		nine[i] = guardFunc(func(context.Context, controlplane.BatchReadView) error { return nil })
	}
	cases := map[string]func(*controlplane.BatchCommand){
		"no project":         func(c *controlplane.BatchCommand) { c.ProjectID = "" },
		"bad actor":          func(c *controlplane.BatchCommand) { c.Actor = protocol.Actor{} },
		"no expected prefix": func(c *controlplane.BatchCommand) { c.ExpectedStateRevision = "" },
		"malformed prefix":   func(c *controlplane.BatchCommand) { c.ExpectedStateRevision = "ps_12" },
		"no commands":        func(c *controlplane.BatchCommand) { c.Commands = nil },
		"too many commands":  func(c *controlplane.BatchCommand) { c.Commands = tooMany },
		"nil payload": func(c *controlplane.BatchCommand) {
			c.Commands = []controlplane.Command{{ProjectID: "example", Actor: batchActor}}
		},
		"member other project": func(c *controlplane.BatchCommand) { c.Commands[0].ProjectID = "other" },
		"member other actor": func(c *controlplane.BatchCommand) {
			c.Commands[0].Actor = protocol.Actor{Kind: protocol.ActorHuman, ID: "olostan"}
		},
		"member other correlation": func(c *controlplane.BatchCommand) { c.Commands[0].Correlation = events.Correlation{TaskID: "t"} },
		"nil guard":                func(c *controlplane.BatchCommand) { c.Preconditions = []controlplane.BatchGuard{nil} },
		"nine preconditions":       func(c *controlplane.BatchCommand) { c.Preconditions = nine },
		"nine postconditions":      func(c *controlplane.BatchCommand) { c.Postconditions = nine },
		"incomplete wp guard": func(c *controlplane.BatchCommand) {
			c.WorkPackage = &controlplane.WorkPackageGuard{TaskID: "t"}
		},
		"duplicate record identity": func(c *controlplane.BatchCommand) {
			wp := testsupport.WorkPackage("example", "t", "wp_x", 1)
			c.Commands = []controlplane.Command{
				{ProjectID: "example", Actor: batchActor, Payload: c.Commands[0].Payload, Records: []controlplane.RecordToStore{{Version: 1, Record: wp}}},
				{ProjectID: "example", Actor: batchActor, Payload: c.Commands[0].Payload, Records: []controlplane.RecordToStore{{Version: 1, Record: wp}}},
			}
		},
	}
	before := capture(t, h.Service)
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cmd := base()
			mutate(&cmd)
			if _, err := h.Service.ApplyBatch(context.Background(), cmd); err == nil {
				t.Fatal("the malformed batch was accepted")
			} else if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
				t.Fatalf("category = %s, want invalid_argument (%v)", errs.CategoryOf(err), err)
			}
		})
	}
	requireUnchanged(t, before, capture(t, h.Service))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.Service.ApplyBatch(ctx, base()); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled context returned %v", err)
	}
}

func TestBatchByteBoundIsEnforced(t *testing.T) {
	h := testsupport.NewHarness(t)
	initProject(t, h)
	big := make([]byte, 0, controlplane.MaxBatchBytes)
	for len(big) < controlplane.MaxBatchBytes {
		big = append(big, 'x')
	}
	cmd := risk("R-big")
	cmd.Payload.(*events.RiskRecorded).Statement = string(big)
	_, err := h.Service.ApplyBatch(context.Background(), controlplane.BatchCommand{
		ProjectID: "example", Actor: batchActor, ExpectedStateRevision: revisionOf(t, h.Service),
		Commands: []controlplane.Command{cmd},
	})
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("an oversized batch returned %v", err)
	}
}

func TestBatchOfIndependentEventsCommitsInOrder(t *testing.T) {
	h := testsupport.NewHarness(t)
	initProject(t, h)
	prefix := revisionOf(t, h.Service)
	result, err := h.Service.ApplyBatch(context.Background(), controlplane.BatchCommand{
		ProjectID: "example", Actor: batchActor, ExpectedStateRevision: prefix,
		Commands: []controlplane.Command{risk("R-1"), risk("R-2"), risk("R-3")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 3 || result.ProjectState == nil {
		t.Fatalf("results = %d", len(result.Results))
	}
	for i := 1; i < 3; i++ {
		if result.Results[i].Event.Seq != result.Results[i-1].Event.Seq+1 {
			t.Fatal("events are not consecutive in declaration order")
		}
	}
	if got := revisionOf(t, h.Service); got != result.ProjectState.StateRevision {
		t.Fatalf("committed revision %s, result revision %s", got, result.ProjectState.StateRevision)
	}
	if len(result.ProjectState.Risks) != 3 {
		t.Fatalf("risks = %d, want 3", len(result.ProjectState.Risks))
	}
	// A bootstrap batch on an empty project is expressed with the empty prefix.
	fresh := testsupport.NewHarness(t)
	_, err = fresh.Service.ApplyBatch(context.Background(), controlplane.BatchCommand{
		ProjectID: "example", Actor: batchActor, ExpectedStateRevision: principal.EmptyStateRevision,
		Commands: []controlplane.Command{risk("R-1")},
	})
	if errs.CategoryOf(err) != errs.CategoryNotFound {
		t.Fatalf("a non-initialising first event returned %v, want not_found", err)
	}
}

// A3: the second lifecycle event is illegal; nothing at all commits.
func TestA3_IllegalSecondMemberRollsEverythingBack(t *testing.T) {
	h := testsupport.NewHarness(t)
	initProject(t, h)
	prefix := revisionOf(t, h.Service)
	before := capture(t, h.Service, [4]any{"EngineeringWorkPackage", "wp_a3", 1, 0})
	wp := testsupport.WorkPackage("example", "task_a3", "wp_a3", 1)
	digest, _ := protocol.Digest(wp)
	_, err := h.Service.ApplyBatch(context.Background(), controlplane.BatchCommand{
		ProjectID: "example", Actor: batchActor, ExpectedStateRevision: prefix,
		Commands: []controlplane.Command{
			{ProjectID: "example", Actor: batchActor, Payload: &events.TaskCreated{
				TaskID: "task_a3", Alias: "DC-A3", Title: "t", ChangeClass: protocol.ChangeLocal,
			}},
			{ProjectID: "example", Actor: batchActor, Payload: &events.TaskDesignStarted{TaskID: "task_a3", Reason: "r"}},
			{ProjectID: "example", Actor: batchActor,
				Payload: &events.WorkPackageApproved{
					TaskID: "task_a3", WorkPackageID: "wp_a3", WorkPackageVersion: 1, RecordDigest: digest,
					ProjectStateRevision: wp.ProjectStateRevision, BaseCommit: wp.BaseCommit, ChangeClass: wp.ChangeClass,
				},
				Records: []controlplane.RecordToStore{{Version: 1, Record: wp}}},
			// READY -> PROPOSED-style illegal edge: AttemptStarted before delegation.
			{ProjectID: "example", Actor: batchActor, Payload: &events.AttemptStarted{
				TaskID: "task_a3", AttemptID: "att_a3", WorkPackageID: "wp_a3", WorkPackageVersion: 1,
				ProjectStateRevision: "ps_000000003", WorkerRole: "implementer",
			}},
		},
	})
	if errs.CategoryOf(err) != errs.CategoryInvalidTransition {
		t.Fatalf("err = %v, want invalid_transition", err)
	}
	requireUnchanged(t, before, capture(t, h.Service, [4]any{"EngineeringWorkPackage", "wp_a3", 1, 0}))
	if _, err := h.Service.TaskDetail(context.Background(), "example", "DC-A3"); err == nil {
		t.Fatal("the task of a rolled-back batch is visible")
	}
}

// A4: same name, wrong version, digest or base: STALE_WORK_PACKAGE and no attempt.
func TestA4_GuardRejectsAWrongWorkPackageTuple(t *testing.T) {
	h := testsupport.NewHarness(t)
	p := readyTask(t, h.Service, controlplane.InitProjectInput{})
	prefix := revisionOf(t, h.Service)
	before := capture(t, h.Service)
	mutations := map[string]func(*controlplane.WorkPackageGuard){
		"version": func(g *controlplane.WorkPackageGuard) { g.Version = 2 },
		"digest": func(g *controlplane.WorkPackageGuard) {
			g.Digest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
		},
		"base":    func(g *controlplane.WorkPackageGuard) { g.BaseCommit = "ffffffffffff" },
		"package": func(g *controlplane.WorkPackageGuard) { g.WorkPackageID = "wp_other" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			guard := *p.guard()
			mutate(&guard)
			_, err := h.Service.ApplyBatch(context.Background(), controlplane.BatchCommand{
				ProjectID: "example", Actor: batchActor, ExpectedStateRevision: prefix,
				WorkPackage: &guard, Commands: start(p, "att_a4"),
			})
			if !errors.Is(err, controlplane.ErrStaleWorkPackage) {
				t.Fatalf("err = %v, want ErrStaleWorkPackage", err)
			}
			requireUnchanged(t, before, capture(t, h.Service))
		})
	}
	// Control: the exact tuple starts execution.
	if _, err := h.Service.ApplyBatch(context.Background(), controlplane.BatchCommand{
		ProjectID: "example", Actor: batchActor, ExpectedStateRevision: prefix,
		WorkPackage: p.guard(), Commands: start(p, "att_a4"),
	}); err != nil {
		t.Fatalf("the exact tuple was refused: %v", err)
	}
	detail, err := h.Service.TaskDetail(context.Background(), "example", "DC-001")
	if err != nil || detail.Task.State != tasks.StateRunning || len(detail.Attempts) != 1 {
		t.Fatalf("task = %+v attempts = %d, %v", detail.Task, len(detail.Attempts), err)
	}
}

func TestGuardOnAnUnknownTaskIsNotFound(t *testing.T) {
	h := testsupport.NewHarness(t)
	p := readyTask(t, h.Service, controlplane.InitProjectInput{})
	guard := *p.guard()
	guard.TaskID = "task_missing"
	_, err := h.Service.ApplyBatch(context.Background(), controlplane.BatchCommand{
		ProjectID: "example", Actor: batchActor, ExpectedStateRevision: revisionOf(t, h.Service),
		WorkPackage: &guard, Commands: start(p, "att_x"),
	})
	if errs.CategoryOf(err) != errs.CategoryNotFound {
		t.Fatalf("err = %v, want not_found", err)
	}
}

// A5: unrelated events advance the prefix but do not stale the plan; the old
// prefix is still refused.
func TestA5_UnrelatedEventsDoNotStaleTheWorkPackageButOldPrefixesAreRefused(t *testing.T) {
	h := testsupport.NewHarness(t)
	p := readyTask(t, h.Service, controlplane.InitProjectInput{})
	oldPrefix := revisionOf(t, h.Service)
	mustAppend(t, h.Service, &events.RiskRecorded{RiskID: "R-9", Severity: protocol.SeverityLow, Statement: "unrelated"}, nil)
	if _, err := h.Service.CreateTask(context.Background(), controlplane.CreateTaskInput{
		ProjectID: "example", Alias: "DC-002", Title: "other task",
	}); err != nil {
		t.Fatal(err)
	}
	freshPrefix := revisionOf(t, h.Service)
	if freshPrefix == oldPrefix {
		t.Fatal("setup did not advance the prefix")
	}

	before := capture(t, h.Service)
	_, err := h.Service.ApplyBatch(context.Background(), controlplane.BatchCommand{
		ProjectID: "example", Actor: batchActor, ExpectedStateRevision: oldPrefix,
		WorkPackage: p.guard(), Commands: start(p, "att_a5"),
	})
	if !errors.Is(err, controlplane.ErrStaleProjectState) {
		t.Fatalf("old prefix: err = %v, want ErrStaleProjectState", err)
	}
	requireUnchanged(t, before, capture(t, h.Service))

	// The plan's own planning revision (ps_000000003) is far behind the global
	// prefix; comparing them would wrongly refuse this valid start.
	if _, err := h.Service.ApplyBatch(context.Background(), controlplane.BatchCommand{
		ProjectID: "example", Actor: batchActor, ExpectedStateRevision: freshPrefix,
		WorkPackage: p.guard(), Commands: start(p, "att_a5"),
	}); err != nil {
		t.Fatalf("a fresh-prefix start of an unchanged approved plan failed: %v", err)
	}
}

// A task-correlated event after approval supersedes the approval even when it
// leaves the task READY.
func TestTaskSignificantEventAfterApprovalStalesTheWorkPackage(t *testing.T) {
	h := testsupport.NewHarness(t)
	p := readyTask(t, h.Service, controlplane.InitProjectInput{})
	if _, err := h.Service.AppendTypedEvent(context.Background(), controlplane.AppendTypedEventInput{
		ProjectID:   "example",
		Payload:     &events.RiskRecorded{RiskID: "R-t", Severity: protocol.SeverityLow, Statement: "about the task"},
		Correlation: events.Correlation{TaskID: p.taskID},
	}); err != nil {
		t.Fatal(err)
	}
	detail, _ := h.Service.TaskDetail(context.Background(), "example", "DC-001")
	if detail.Task.State != tasks.StateReady {
		t.Fatalf("setup left the task %s; the check under test needs READY", detail.Task.State)
	}
	before := capture(t, h.Service)
	_, err := h.Service.ApplyBatch(context.Background(), controlplane.BatchCommand{
		ProjectID: "example", Actor: batchActor, ExpectedStateRevision: revisionOf(t, h.Service),
		WorkPackage: p.guard(), Commands: start(p, "att_ts"),
	})
	if !errors.Is(err, controlplane.ErrStaleWorkPackage) {
		t.Fatalf("err = %v, want ErrStaleWorkPackage", err)
	}
	requireUnchanged(t, before, capture(t, h.Service))
}

// A6: approval -> block -> design -> newer approval; the old tuple is denied.
func TestA6_BlockResumeReapproveDeniesTheOldTuple(t *testing.T) {
	h := testsupport.NewHarness(t)
	old := readyTask(t, h.Service, controlplane.InitProjectInput{})
	mustAppend(t, h.Service, &events.EscalationRaised{
		TaskID: old.taskID, EscalationID: "esc_1",
		Reason: tasks.BlockedReason{
			Trigger: "contradicted_assumption", Statement: "A2 is false",
			Authority: protocol.AuthorityPrincipal, EvidenceRefs: []string{"ev_1"}, BlockedFrom: tasks.StateReady,
		},
	}, nil)
	try := func(p planned, attempt string) error {
		_, err := h.Service.ApplyBatch(context.Background(), controlplane.BatchCommand{
			ProjectID: "example", Actor: batchActor, ExpectedStateRevision: revisionOf(t, h.Service),
			WorkPackage: p.guard(), Commands: start(p, attempt),
		})
		return err
	}
	if err := try(old, "att_blocked"); !errors.Is(err, controlplane.ErrStaleWorkPackage) {
		t.Fatalf("blocked task: err = %v, want ErrStaleWorkPackage", err)
	}
	mustAppend(t, h.Service, &events.TaskDesignStarted{TaskID: old.taskID, Reason: "resume"}, nil)
	if err := try(old, "att_designing"); !errors.Is(err, controlplane.ErrStaleWorkPackage) {
		t.Fatalf("resumed task: err = %v, want ErrStaleWorkPackage", err)
	}
	newer := approve(t, h.Service, old.taskID, "wp_0001", 2)
	before := capture(t, h.Service)
	if err := try(old, "att_old"); !errors.Is(err, controlplane.ErrStaleWorkPackage) {
		t.Fatalf("superseded tuple: err = %v, want ErrStaleWorkPackage", err)
	}
	requireUnchanged(t, before, capture(t, h.Service))
	if err := try(newer, "att_new"); err != nil {
		t.Fatalf("the current tuple was refused: %v", err)
	}
}

func TestRepositoryBackedGuardNeedsTheRegisteredAcceptedBase(t *testing.T) {
	repo := t.TempDir()
	cases := map[string]struct {
		accepted string
		ok       bool
	}{
		"matching accepted base":  {"91acd8273f1", true},
		"different accepted base": {"aaaaaaaaaaaa", false},
		"no accepted base at all": {"", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := testsupport.NewHarness(t)
			p := readyTask(t, h.Service, controlplane.InitProjectInput{RepositoryPath: repo, AcceptedCommit: tc.accepted})
			_, err := h.Service.ApplyBatch(context.Background(), controlplane.BatchCommand{
				ProjectID: "example", Actor: batchActor, ExpectedStateRevision: revisionOf(t, h.Service),
				WorkPackage: p.guard(), Commands: start(p, "att_repo"),
			})
			switch {
			case tc.ok && err != nil:
				t.Fatalf("a matching base was refused: %v", err)
			case !tc.ok && !errors.Is(err, controlplane.ErrStaleWorkPackage):
				t.Fatalf("err = %v, want ErrStaleWorkPackage", err)
			}
		})
	}
}

func TestGuardDetectsATamperedStoredRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tamper.db")
	h := testsupport.NewFileHarness(t, path)
	p := readyTask(t, h.Service, controlplane.InitProjectInput{})
	// An operator edit outside the application: drop the immutability trigger
	// and rewrite the stored document under its recorded digest.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	for _, statement := range []string{
		`DROP TRIGGER records_immutable_update`,
		`UPDATE records SET document = replace(document, 'Bounded journal reads', 'Tampered') WHERE record_kind = 'EngineeringWorkPackage'`,
	} {
		if _, err := raw.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	_, err = h.Service.ApplyBatch(context.Background(), controlplane.BatchCommand{
		ProjectID: "example", Actor: batchActor, ExpectedStateRevision: revisionOf(t, h.Service),
		WorkPackage: p.guard(), Commands: start(p, "att_tamper"),
	})
	if errs.CategoryOf(err) != errs.CategoryIntegrity {
		t.Fatalf("err = %v, want integrity (an unverifiable record is never fresh)", err)
	}
}

// A7: the final member's record or reference is wrong; everything rolls back
// and a restart equals the pre-state.
func TestA7_FinalMemberReferenceMismatchRollsBackAndSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a7.db")
	h := testsupport.NewFileHarness(t, path)
	initProject(t, h)
	prefix := revisionOf(t, h.Service)
	good := testsupport.WorkPackage("example", "task_a7", "wp_a7", 1)
	goodDigest, _ := protocol.Digest(good)
	probes := [][4]any{{"EngineeringWorkPackage", "wp_a7", 1, 0}, {"EngineeringWorkPackage", "wp_a7b", 1, 0}}
	before := capture(t, h.Service, probes...)
	stateBefore, _ := h.Service.RebuildProjection(context.Background(), "example")
	beforeBytes, _ := protocol.CanonicalJSON(stateBefore)

	bad := testsupport.WorkPackage("example", "task_a7", "wp_a7b", 1)
	_, err := h.Service.ApplyBatch(context.Background(), controlplane.BatchCommand{
		ProjectID: "example", Actor: batchActor, ExpectedStateRevision: prefix,
		Commands: []controlplane.Command{
			{ProjectID: "example", Actor: batchActor, Payload: &events.TaskCreated{
				TaskID: "task_a7", Alias: "DC-A7", Title: "t", ChangeClass: protocol.ChangeSystemic,
			}},
			{ProjectID: "example", Actor: batchActor, Payload: &events.TaskDesignStarted{TaskID: "task_a7", Reason: "r"}},
			{ProjectID: "example", Actor: batchActor,
				Payload: &events.WorkPackageApproved{
					TaskID: "task_a7", WorkPackageID: "wp_a7", WorkPackageVersion: 1, RecordDigest: goodDigest,
					ProjectStateRevision: good.ProjectStateRevision, BaseCommit: good.BaseCommit, ChangeClass: good.ChangeClass,
				},
				Records: []controlplane.RecordToStore{{Version: 1, Record: good}}},
			// Final member: a record is written but the event claims another digest.
			{ProjectID: "example", Actor: batchActor,
				Payload: &events.WorkPackageApproved{
					TaskID: "task_a7", WorkPackageID: "wp_a7b", WorkPackageVersion: 1,
					RecordDigest:         "sha256:0000000000000000000000000000000000000000000000000000000000000000",
					ProjectStateRevision: bad.ProjectStateRevision, BaseCommit: bad.BaseCommit, ChangeClass: bad.ChangeClass,
				},
				Records: []controlplane.RecordToStore{{Version: 1, Record: bad}}},
		},
	})
	if errs.CategoryOf(err) != errs.CategoryIntegrity {
		t.Fatalf("err = %v, want integrity", err)
	}
	requireUnchanged(t, before, capture(t, h.Service, probes...))

	reopened := openService(t, path)
	requireUnchanged(t, before, capture(t, reopened, probes...))
	rebuilt, err := reopened.RebuildProjection(context.Background(), "example")
	if err != nil {
		t.Fatal(err)
	}
	afterBytes, _ := protocol.CanonicalJSON(rebuilt)
	if string(afterBytes) != string(beforeBytes) {
		t.Fatal("a journal rebuild after the refused batch differs from the pre-state")
	}
}

// A8: the facade seam: the actor is the binding, and a wire document cannot
// supply grants (the decode refusal is covered in internal/principal).
func TestA8_BatchActorMustBeAValidServerBindingIdentity(t *testing.T) {
	h := testsupport.NewHarness(t)
	initProject(t, h)
	_, err := h.Service.ApplyBatch(context.Background(), controlplane.BatchCommand{
		ProjectID: "example", Actor: protocol.Actor{Kind: "root", ID: "x"},
		ExpectedStateRevision: revisionOf(t, h.Service), Commands: []controlplane.Command{risk("R-1")},
	})
	if err == nil {
		t.Fatal("an actor outside the closed kinds was accepted")
	}
}

// A9: old Apply callers keep their behaviour: no expected prefix, per-event
// projection, same results.
func TestA9_ApplyIsUnchangedByTheBatchAPI(t *testing.T) {
	h := testsupport.NewHarness(t)
	initProject(t, h)
	r1, err := h.Service.Apply(context.Background(), risk("R-1"))
	if err != nil {
		t.Fatal(err)
	}
	r2, err := h.Service.Apply(context.Background(), risk("R-2"))
	if err != nil {
		t.Fatal(err)
	}
	if r2.Event.Seq != r1.Event.Seq+1 || len(r2.ProjectState.Risks) != 2 {
		t.Fatalf("Apply behaviour changed: %+v", r2.Event.Seq)
	}
	if got := revisionOf(t, h.Service); got != r2.ProjectState.StateRevision {
		t.Fatalf("materialised revision %s, result %s", got, r2.ProjectState.StateRevision)
	}
	if _, err := h.Service.Apply(context.Background(), controlplane.Command{ProjectID: "example", Actor: batchActor}); err == nil {
		t.Fatal("Apply accepted a command without a payload")
	}
}

// A10: a dropped response leaves the commit in place; repeating the same
// prefix is stale and the committed records remain retrievable.
func TestA10_RepeatingASuccessfulBatchAtItsOldPrefixIsStale(t *testing.T) {
	h := testsupport.NewHarness(t)
	p := readyTask(t, h.Service, controlplane.InitProjectInput{})
	prefix := revisionOf(t, h.Service)
	cmd := controlplane.BatchCommand{
		ProjectID: "example", Actor: batchActor, ExpectedStateRevision: prefix,
		Correlation: events.Correlation{TaskID: p.taskID},
		WorkPackage: p.guard(), Commands: start(p, "att_a10"),
	}
	for i := range cmd.Commands {
		cmd.Commands[i].Correlation = cmd.Correlation
	}
	if _, err := h.Service.ApplyBatch(context.Background(), cmd); err != nil {
		t.Fatal(err)
	} // the response is "dropped": the caller never reads it
	before := capture(t, h.Service)
	_, err := h.Service.ApplyBatch(context.Background(), cmd)
	if !errors.Is(err, controlplane.ErrStaleProjectState) {
		t.Fatalf("repeat at the old prefix: err = %v, want ErrStaleProjectState", err)
	}
	requireUnchanged(t, before, capture(t, h.Service))
	// The committed facts are retrievable by correlation and by record identity.
	history, err := h.Service.Events(context.Background(), storage.EventQuery{ProjectID: "example", TaskID: p.taskID, AfterSeq: 0})
	if err != nil || len(history) < 4 {
		t.Fatalf("history = %d, %v", len(history), err)
	}
	if _, err := h.Service.Record(context.Background(), "example", "EngineeringWorkPackage", p.wpID, 1); err != nil {
		t.Fatalf("committed record is not retrievable: %v", err)
	}
	// A fresh-prefix replay is a new mutation; the reducer refuses the
	// nonrepeatable attempt id.
	cmd.ExpectedStateRevision = revisionOf(t, h.Service)
	cmd.WorkPackage = nil
	if _, err := h.Service.ApplyBatch(context.Background(), cmd); err == nil {
		t.Fatal("a fresh-prefix replay of a nonrepeatable start was accepted")
	}
}

// A11: trusted guards reject after valid members; all writes roll back; a
// retained view is unusable.
func TestA11_GuardsRollBackEverythingAndRetiredViewsAreUnusable(t *testing.T) {
	h := testsupport.NewHarness(t)
	initProject(t, h)
	prefix := revisionOf(t, h.Service)
	probe := [4]any{"EngineeringWorkPackage", "wp_a11", 1, 0}
	wp := testsupport.WorkPackage("example", "task_a11", "wp_a11", 1)
	digest, _ := protocol.Digest(wp)
	members := []controlplane.Command{
		{ProjectID: "example", Actor: batchActor, Payload: &events.TaskCreated{
			TaskID: "task_a11", Alias: "DC-A11", Title: "t", ChangeClass: protocol.ChangeSystemic,
		}},
		{ProjectID: "example", Actor: batchActor, Payload: &events.TaskDesignStarted{TaskID: "task_a11", Reason: "r"}},
		{ProjectID: "example", Actor: batchActor,
			Payload: &events.WorkPackageApproved{
				TaskID: "task_a11", WorkPackageID: "wp_a11", WorkPackageVersion: 1, RecordDigest: digest,
				ProjectStateRevision: wp.ProjectStateRevision, BaseCommit: wp.BaseCommit, ChangeClass: wp.ChangeClass,
			},
			Records: []controlplane.RecordToStore{{Version: 1, Record: wp}}},
	}
	before := capture(t, h.Service, probe)
	deny := errors.New("guard refuses")

	run := func(pre, post []controlplane.BatchGuard) error {
		_, err := h.Service.ApplyBatch(context.Background(), controlplane.BatchCommand{
			ProjectID: "example", Actor: batchActor, ExpectedStateRevision: prefix,
			Preconditions: pre, Postconditions: post, Commands: members,
		})
		return err
	}
	t.Run("precondition", func(t *testing.T) {
		err := run([]controlplane.BatchGuard{guardFunc(func(context.Context, controlplane.BatchReadView) error { return deny })}, nil)
		if !errors.Is(err, deny) {
			t.Fatalf("err = %v", err)
		}
		requireUnchanged(t, before, capture(t, h.Service, probe))
	})
	t.Run("postcondition sees the final working state and rejects", func(t *testing.T) {
		sawFinal := false
		err := run(nil, []controlplane.BatchGuard{guardFunc(func(ctx context.Context, v controlplane.BatchReadView) error {
			// Members' records and events are visible to the postcondition...
			if _, err := v.Record(ctx, "EngineeringWorkPackage", "wp_a11", 1); err != nil {
				return fmt.Errorf("member record not visible: %w", err)
			}
			sawFinal = v.ProjectState().Tasks.Ready != nil && len(v.ProjectState().Tasks.Ready) == 1
			return deny // ...and rejecting it still rolls everything back.
		})})
		if !errors.Is(err, deny) || !sawFinal {
			t.Fatalf("err = %v, postcondition saw final state = %v", err, sawFinal)
		}
		requireUnchanged(t, before, capture(t, h.Service, probe))
	})
	t.Run("panic is an internal rollback", func(t *testing.T) {
		err := run(nil, []controlplane.BatchGuard{guardFunc(func(context.Context, controlplane.BatchReadView) error { panic("boom") })})
		if errs.CategoryOf(err) != errs.CategoryInternal {
			t.Fatalf("err = %v, want internal", err)
		}
		requireUnchanged(t, before, capture(t, h.Service, probe))
	})
	t.Run("precondition sees only the pre-batch state", func(t *testing.T) {
		err := run([]controlplane.BatchGuard{guardFunc(func(ctx context.Context, v controlplane.BatchReadView) error {
			if v.ProjectState().StateRevision != prefix {
				return errors.New("precondition view is not the pre-batch prefix")
			}
			if _, err := v.Record(ctx, "EngineeringWorkPackage", "wp_a11", 1); err == nil {
				return errors.New("precondition view sees a record of its own batch")
			}
			return nil
		})}, nil)
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestA11_RetainedViewIsUnusableAfterCheckReturns(t *testing.T) {
	h := testsupport.NewHarness(t)
	initProject(t, h)
	var retained controlplane.BatchReadView
	_, err := h.Service.ApplyBatch(context.Background(), controlplane.BatchCommand{
		ProjectID: "example", Actor: batchActor, ExpectedStateRevision: revisionOf(t, h.Service),
		Preconditions: []controlplane.BatchGuard{guardFunc(func(_ context.Context, v controlplane.BatchReadView) error {
			if v.ProjectState() == nil {
				return errors.New("live view returned no state")
			}
			retained = v
			return nil
		})},
		Commands: []controlplane.Command{risk("R-1")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if retained.ProjectState() != nil {
		t.Fatal("a retired view still renders project state")
	}
	if _, err := retained.Record(context.Background(), "EngineeringWorkPackage", "wp", 1); errs.CategoryOf(err) != errs.CategoryInvalidTransition {
		t.Fatalf("retired view Record: err = %v, want invalid_transition", err)
	}
}

func TestBatchViewIsProjectBoundAndValidatesLookups(t *testing.T) {
	h := testsupport.NewHarness(t)
	initProject(t, h)
	_, err := h.Service.ApplyBatch(context.Background(), controlplane.BatchCommand{
		ProjectID: "example", Actor: batchActor, ExpectedStateRevision: revisionOf(t, h.Service),
		Preconditions: []controlplane.BatchGuard{guardFunc(func(ctx context.Context, v controlplane.BatchReadView) error {
			if _, err := v.Record(ctx, "", "id", 1); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
				return fmt.Errorf("empty kind: %v", err)
			}
			if _, err := v.Record(ctx, "EngineeringWorkPackage", "id", 0); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
				return fmt.Errorf("zero version: %v", err)
			}
			if _, err := v.Record(ctx, "EngineeringWorkPackage", "absent", 1); errs.CategoryOf(err) != errs.CategoryNotFound {
				return fmt.Errorf("absent record: %v", err)
			}
			return nil
		})},
		Commands: []controlplane.Command{risk("R-1")},
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A12: a lock held beyond the budget gives bounded ErrStorageBusy, a caller
// deadline gives the context error, read-only commands take no lock.
func TestA12_ContentionIsBoundedAndReadsStayFree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a12.db")
	svc := openService(t, path)
	if _, err := svc.InitProject(context.Background(), controlplane.InitProjectInput{
		ProjectID: "example", MilestoneID: "M1", MilestoneTitle: "Domain core",
	}); err != nil {
		t.Fatal(err)
	}
	holderStore, err := storage.Open(context.Background(), storage.Config{Path: path, Clock: testsupport.NewClock()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holderStore.Close() }()
	held, stop, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- holderStore.WriteSerialized(context.Background(), func(*storage.Tx) error {
			close(held)
			<-stop
			return nil
		})
	}()
	<-held

	prefix := revisionOf(t, svc) // read-only: must not block on the held lock
	before := capture(t, svc)
	cmd := controlplane.BatchCommand{
		ProjectID: "example", Actor: batchActor, ExpectedStateRevision: prefix,
		Commands: []controlplane.Command{risk("R-1")},
	}

	started := time.Now()
	_, err = svc.ApplyBatch(context.Background(), cmd)
	if !errors.Is(err, controlplane.ErrStorageBusy) {
		t.Fatalf("err = %v, want ErrStorageBusy", err)
	}
	if errs.CategoryOf(err) != errs.CategoryConflict || errors.Is(err, controlplane.ErrStaleProjectState) {
		t.Fatalf("busy must be a conflict that is not a staleness verdict: %v", err)
	}
	if elapsed := time.Since(started); elapsed > storage.ContentionBudget+1500*time.Millisecond {
		t.Fatalf("busy took %s", elapsed)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := svc.ApplyBatch(ctx, cmd); !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, controlplane.ErrStorageBusy) {
		t.Fatalf("caller deadline: err = %v, want the context error unchanged", err)
	}
	cancelled, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	if _, err := svc.ApplyBatch(cancelled, cmd); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: err = %v", err)
	}
	requireUnchanged(t, before, capture(t, svc))

	close(stop)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// With the lock released the same call, still at the same prefix, commits.
	if _, err := svc.ApplyBatch(context.Background(), cmd); err != nil {
		t.Fatalf("after the lock was released: %v", err)
	}
}
