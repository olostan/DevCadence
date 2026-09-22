package state_test

import (
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/state"
	"github.com/olostan/DevCadence/internal/tasks"
	"github.com/olostan/DevCadence/internal/testsupport"
)

func TestHappyPathReducesToDone(t *testing.T) {
	stream := testsupport.HappyPathScenario(t, "example").Stream()
	projection, err := state.Reduce(stream)
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	task, err := projection.TaskByAlias("DC-001")
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	if task.State != tasks.StateDone {
		t.Fatalf("state = %s, want done", task.State)
	}
	if task.AcceptedCommit != "cafebabe1234567" {
		t.Fatalf("accepted commit = %q", task.AcceptedCommit)
	}
	// The project baseline advances to the integrated commit, not the
	// candidate: acceptance is not integration.
	if projection.AcceptedCommit != "deadbeef7654321" {
		t.Fatalf("project accepted commit = %q, want the integrated commit", projection.AcceptedCommit)
	}

	attempts := projection.AttemptsForTask(task.ID)
	if len(attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(attempts))
	}
	if attempts[0].Status != tasks.AttemptCandidateProduced {
		t.Fatalf("attempt status = %s", attempts[0].Status)
	}
	if attempts[0].RepairIterations != 2 {
		t.Fatalf("repair iterations = %d, want 2", attempts[0].RepairIterations)
	}

	projectState, err := projection.ProjectState()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if projectState.Milestone.CompletedTasks != 1 || projectState.Milestone.TotalTasks != 1 {
		t.Fatalf("milestone progress = %d/%d, want 1/1",
			projectState.Milestone.CompletedTasks, projectState.Milestone.TotalTasks)
	}
	if len(projectState.RecentSemanticChanges) != 1 {
		t.Fatalf("semantic changes = %d, want 1", len(projectState.RecentSemanticChanges))
	}
	if projectState.RecentSemanticChanges[0].TaskID != "DC-001" {
		t.Fatalf("semantic change is keyed by %q, want the task alias",
			projectState.RecentSemanticChanges[0].TaskID)
	}
}

func TestBlockPreservesReasonAndSurfacesToThePrincipal(t *testing.T) {
	stream := testsupport.BlockedScenario(t, "example").Stream()
	projection, err := state.Reduce(stream)
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	task, err := projection.TaskByAlias("DC-001")
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	if task.State != tasks.StateBlocked {
		t.Fatalf("state = %s, want blocked", task.State)
	}
	if task.Blocked == nil {
		t.Fatal("a blocked task lost its reason")
	}
	if task.Blocked.Trigger != "contradicted_assumption" {
		t.Fatalf("trigger = %q", task.Blocked.Trigger)
	}
	// The state the task blocked from stays recorded even though resuming
	// always routes through DESIGNING.
	if task.Blocked.BlockedFrom != tasks.StateRunning {
		t.Fatalf("blocked_from = %s, want running", task.Blocked.BlockedFrom)
	}

	projectState, err := projection.ProjectState()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(projectState.Tasks.Blocked) != 1 || projectState.Tasks.Blocked[0] != "DC-001" {
		t.Fatalf("blocked bucket = %v", projectState.Tasks.Blocked)
	}
	if len(projectState.Tasks.AwaitingPrincipal) != 1 {
		t.Fatalf("awaiting_principal = %v, want DC-001", projectState.Tasks.AwaitingPrincipal)
	}
}

// TestResumeCreatesANewAttemptAndKeepsTheOld is the retry guarantee of
// ENGINEERING_STANDARDS.md §12: history accumulates, it is never rewritten.
func TestResumeCreatesANewAttemptAndKeepsTheOld(t *testing.T) {
	const (
		taskID = "tsk_00000000000000000000000001"
		wpID   = "wp_000000000000000000000001"
	)
	builder := testsupport.BlockedScenario(t, "example").
		AddAs(protocol.Actor{Kind: protocol.ActorPrincipal, ID: "principal"}, &events.TaskDesignStarted{
			TaskID: taskID, Reason: "assumption A2 revised after the contradiction",
		}).
		AddAs(protocol.Actor{Kind: protocol.ActorPrincipal, ID: "principal"}, &events.WorkPackageApproved{
			TaskID: taskID, WorkPackageID: wpID, WorkPackageVersion: 2,
			RecordDigest: "sha256:" + testZeros(64), ProjectStateRevision: "ps_000000009",
			BaseCommit: "91acd8273f1", ChangeClass: protocol.ChangeSystemic,
		}).
		Add(&events.TaskDelegated{TaskID: taskID, WorkPackageID: wpID, WorkerRole: "implementer"}).
		Add(&events.AttemptStarted{
			TaskID: taskID, AttemptID: "att_00000000000000000000000002",
			WorkPackageID: wpID, WorkPackageVersion: 2,
			ProjectStateRevision: "ps_000000009", WorkerRole: "implementer",
		})

	projection, err := state.Reduce(builder.Stream())
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	task, err := projection.TaskByAlias("DC-001")
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	if task.State != tasks.StateRunning {
		t.Fatalf("state = %s, want running", task.State)
	}
	if task.Blocked != nil {
		t.Fatal("the block was not cleared when the task resumed")
	}
	if task.WorkPackageVersion != 2 {
		t.Fatalf("work package version = %d, want 2", task.WorkPackageVersion)
	}
	attempts := projection.AttemptsForTask(task.ID)
	if len(attempts) != 2 {
		t.Fatalf("attempts = %d, want 2; the retry must not erase the blocked attempt", len(attempts))
	}
	if attempts[0].Status != tasks.AttemptBlocked || attempts[0].BlockReason == nil {
		t.Fatal("the first attempt lost its blocked status or reason")
	}
	if attempts[1].Ordinal != 2 {
		t.Fatalf("second attempt ordinal = %d, want 2", attempts[1].Ordinal)
	}
}

func TestInconsistentHistoriesAreRejected(t *testing.T) {
	cases := []struct {
		name     string
		build    func(t *testing.T) []events.Event
		category errs.Category
	}{
		{
			name: "event before project initialisation",
			build: func(t *testing.T) []events.Event {
				return testsupport.NewScenario(t, "example").
					Add(&events.TaskCreated{
						TaskID: "tsk_1", Alias: "DC-001", Title: "x",
						MilestoneID: "M1", ChangeClass: protocol.ChangeLocal,
					}).Stream()
			},
			category: errs.CategoryIntegrity,
		},
		{
			name: "project initialised twice",
			build: func(t *testing.T) []events.Event {
				return testsupport.NewScenario(t, "example").
					Add(&events.ProjectInitialized{Name: "a", MilestoneID: "M1", MilestoneTitle: "t"}).
					Add(&events.ProjectInitialized{Name: "b", MilestoneID: "M1", MilestoneTitle: "t"}).
					Stream()
			},
			category: errs.CategoryIntegrity,
		},
		{
			name: "duplicate task alias",
			build: func(t *testing.T) []events.Event {
				return testsupport.NewScenario(t, "example").
					Add(&events.ProjectInitialized{Name: "a", MilestoneID: "M1", MilestoneTitle: "t"}).
					Add(&events.TaskCreated{TaskID: "tsk_1", Alias: "DC-001", Title: "x",
						MilestoneID: "M1", ChangeClass: protocol.ChangeLocal}).
					Add(&events.TaskCreated{TaskID: "tsk_2", Alias: "DC-001", Title: "y",
						MilestoneID: "M1", ChangeClass: protocol.ChangeLocal}).
					Stream()
			},
			category: errs.CategoryConflict,
		},
		{
			name: "delegation without an approved work package",
			build: func(t *testing.T) []events.Event {
				return testsupport.NewScenario(t, "example").
					Add(&events.ProjectInitialized{Name: "a", MilestoneID: "M1", MilestoneTitle: "t"}).
					Add(&events.TaskCreated{TaskID: "tsk_1", Alias: "DC-001", Title: "x",
						MilestoneID: "M1", ChangeClass: protocol.ChangeLocal}).
					Add(&events.TaskDelegated{TaskID: "tsk_1", WorkPackageID: "wp_1", WorkerRole: "implementer"}).
					Stream()
			},
			category: errs.CategoryInvalidTransition,
		},
		{
			name: "attempt for an unknown task",
			build: func(t *testing.T) []events.Event {
				return testsupport.NewScenario(t, "example").
					Add(&events.ProjectInitialized{Name: "a", MilestoneID: "M1", MilestoneTitle: "t"}).
					Add(&events.AttemptStarted{TaskID: "tsk_missing", AttemptID: "att_1",
						WorkPackageID: "wp_1", WorkPackageVersion: 1,
						ProjectStateRevision: "ps_000000001", WorkerRole: "implementer"}).
					Stream()
			},
			category: errs.CategoryNotFound,
		},
		{
			name: "resolving a question that was never asked",
			build: func(t *testing.T) []events.Event {
				return testsupport.NewScenario(t, "example").
					Add(&events.ProjectInitialized{Name: "a", MilestoneID: "M1", MilestoneTitle: "t"}).
					Add(&events.DecisionRecorded{
						DecisionID: "dec_1", RecordDigest: "sha256:x", Question: "q",
						Selected: "a", ResolvesDecisionRequired: "DR-404",
					}).Stream()
			},
			category: errs.CategoryIntegrity,
		},
		{
			name: "resolving a risk that is not open",
			build: func(t *testing.T) []events.Event {
				return testsupport.NewScenario(t, "example").
					Add(&events.ProjectInitialized{Name: "a", MilestoneID: "M1", MilestoneTitle: "t"}).
					Add(&events.RiskResolved{RiskID: "R-404", Resolution: "gone"}).
					Stream()
			},
			category: errs.CategoryIntegrity,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := state.Reduce(tc.build(t))
			if err == nil {
				t.Fatal("reduce accepted an inconsistent history")
			}
			if got := errs.CategoryOf(err); got != tc.category {
				t.Fatalf("category = %s, want %s (%v)", got, tc.category, err)
			}
		})
	}
}

func TestOutOfOrderSequencesAreRejected(t *testing.T) {
	stream := testsupport.HappyPathScenario(t, "example").Stream()
	stream[2], stream[3] = stream[3], stream[2]
	if _, err := state.Reduce(stream); err == nil {
		t.Fatal("reduce accepted events out of sequence order")
	} else if errs.CategoryOf(err) != errs.CategoryIntegrity {
		t.Fatalf("category = %s, want integrity (%v)", errs.CategoryOf(err), err)
	}
}

func TestEventsFromAnotherProjectAreRejected(t *testing.T) {
	stream := testsupport.HappyPathScenario(t, "example").Stream()
	stream[1].ProjectID = "someone-else"
	if _, err := state.Reduce(stream); err == nil {
		t.Fatal("reduce accepted an event from a different project")
	}
}

func TestRisksAndDecisionsAppearAndClear(t *testing.T) {
	stream := testsupport.NewScenario(t, "example").
		Add(&events.ProjectInitialized{Name: "a", MilestoneID: "M1", MilestoneTitle: "Domain core"}).
		Add(&events.RiskRecorded{RiskID: "R-001", Severity: protocol.SeverityHigh,
			Statement: "Structured output reliability is unmeasured."}).
		Add(&events.RiskRecorded{RiskID: "R-002", Severity: protocol.SeverityLow,
			Statement: "Artifact retention policy is undecided."}).
		Add(&events.DecisionRequired{DecisionRequiredID: "DR-001",
			Question: "Blobs or artifact store?", Authority: protocol.AuthorityPrincipal}).
		Add(&events.RiskResolved{RiskID: "R-001", Resolution: "Measured in DC-004."}).
		Add(&events.DecisionRecorded{
			DecisionID: "dec_1", RecordDigest: "sha256:x", Question: "Blobs or artifact store?",
			Selected: "artifact_store", ResolvesDecisionRequired: "DR-001",
			ADRRef: "docs/adr/0002-control-plane-persistence.md",
		}).
		Add(&events.HealthReportRecorded{ReportID: "hr_1", Status: protocol.HealthWatch,
			KnownDebt: []string{"Projection rebuild replays the whole journal."}}).
		Stream()

	projection, err := state.Reduce(stream)
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	projectState, err := projection.ProjectState()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(projectState.Risks) != 1 || projectState.Risks[0].ID != "R-002" {
		t.Fatalf("risks = %v, want only R-002 open", projectState.Risks)
	}
	if len(projectState.DecisionsRequired) != 0 {
		t.Fatalf("decisions_required = %v, want empty after the decision", projectState.DecisionsRequired)
	}
	if len(projectState.ActiveDecisions) != 1 || projectState.ActiveDecisions[0] != "dec_1" {
		t.Fatalf("active_decisions = %v", projectState.ActiveDecisions)
	}
	if projectState.Health == nil || projectState.Health.Status != protocol.HealthWatch {
		t.Fatalf("health = %+v, want watch", projectState.Health)
	}
}

func TestBaselineValidationDrivesValidationState(t *testing.T) {
	const commit = "91acd8273f1"
	build := func(status protocol.ValidationOutcome, atCommit string) *protocol.ProjectState {
		t.Helper()
		stream := testsupport.NewScenario(t, "example").
			Add(&events.ProjectInitialized{
				Name: "a", MilestoneID: "M1", MilestoneTitle: "Domain core", AcceptedCommit: commit,
			}).
			Add(&events.ValidationCompleted{
				ValidationID: "val_1", Scope: events.ScopeBaseline, Status: status,
				Commit: atCommit, RecordDigest: "sha256:x",
			}).Stream()
		projection, err := state.Reduce(stream)
		if err != nil {
			t.Fatalf("reduce: %v", err)
		}
		projectState, err := projection.ProjectState()
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		return projectState
	}

	if got := build(protocol.ValidationPass, commit); got.Validation.Status != protocol.ValidationGreen ||
		!got.Validation.AcceptedCommitVerified {
		t.Fatalf("passing baseline on the accepted commit = %+v, want green and verified", got.Validation)
	}
	// A green run on a different commit says nothing about the accepted one.
	if got := build(protocol.ValidationPass, "0000000aaaa"); got.Validation.AcceptedCommitVerified {
		t.Fatal("a baseline run on another commit was reported as verifying the accepted commit")
	}
	if got := build(protocol.ValidationFail, commit); got.Validation.Status != protocol.ValidationRed {
		t.Fatalf("failing baseline = %s, want red", got.Validation.Status)
	}
	// A run that did not complete is not evidence either way.
	if got := build(protocol.ValidationCancelled, commit); got.Validation.Status != protocol.ValidationUnknown {
		t.Fatalf("cancelled baseline = %s, want unknown", got.Validation.Status)
	}
}

func testZeros(n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = '0'
	}
	return string(out)
}

func TestFutureMilestoneEventsAreRecordedWithoutEffect(t *testing.T) {
	base := testsupport.NewScenario(t, "example").
		Add(&events.ProjectInitialized{Name: "a", MilestoneID: "M1", MilestoneTitle: "Domain core"})
	before, err := state.Reduce(base.Stream())
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	beforeState, err := before.ProjectState()
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	withFuture := base.
		Add(&events.LessonCandidateCreated{
			LessonCandidateID: "lc_1", Scope: "project",
			Observation: "Reviewers skipped MUST compliance.", RecordDigest: "sha256:x",
		}).
		Add(&events.RefactoringEpochStarted{EpochID: "ep_1", Trigger: "milestone_complete"}).
		Add(&events.ArchitectureReconciled{ReconciliationID: "ar_1", Outcome: "baseline confirmed"})
	after, err := state.Reduce(withFuture.Stream())
	if err != nil {
		t.Fatalf("reduce with future events: %v", err)
	}
	afterState, err := after.ProjectState()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// Only the identity fields that track journal position may differ.
	beforeState.StateRevision = afterState.StateRevision
	beforeState.EventHighWatermark = afterState.EventHighWatermark
	beforeState.GeneratedAt = afterState.GeneratedAt
	beforeJSON, _ := protocol.CanonicalJSON(beforeState)
	afterJSON, _ := protocol.CanonicalJSON(afterState)
	if string(beforeJSON) != string(afterJSON) {
		t.Fatalf("future-milestone events changed ProjectState:\n%s\n%s", beforeJSON, afterJSON)
	}
}

// TestRejectedEventLeavesTheProjectionUntouched is the in-memory half of the
// "illegal transitions do not partially mutate state" requirement.
//
// The enclosing SQLite transaction protects durable state, but the reducer
// must hold the same contract on its own: a half-applied event would produce
// a projection that is wrong in a way no rollback would catch, because
// nothing failed at the storage layer.
func TestRejectedEventLeavesTheProjectionUntouched(t *testing.T) {
	const (
		taskID    = "tsk_00000000000000000000000001"
		attemptID = "att_00000000000000000000000001"
		wpID      = "wp_000000000000000000000001"
	)

	cases := []struct {
		name     string
		prefix   func(*testing.T) *testsupport.ScenarioBuilder
		rejected events.Payload
	}{
		{
			// The attempt is still RUNNING while the task has been blocked by
			// an escalation. Terminating the attempt is legal on its own, but
			// the task cannot move BLOCKED -> VALIDATING, so the whole event
			// must be rejected with the attempt left running.
			name: "candidate produced while the task is blocked",
			prefix: func(t *testing.T) *testsupport.ScenarioBuilder {
				return testsupport.NewScenario(t, "example").
					Add(&events.ProjectInitialized{Name: "a", MilestoneID: "M1", MilestoneTitle: "t"}).
					Add(&events.TaskCreated{
						TaskID: taskID, Alias: "DC-001", Title: "x",
						MilestoneID: "M1", ChangeClass: protocol.ChangeSystemic,
					}).
					Add(&events.TaskDesignStarted{TaskID: taskID, Reason: "initial design"}).
					Add(&events.WorkPackageApproved{
						TaskID: taskID, WorkPackageID: wpID, WorkPackageVersion: 1,
						RecordDigest: "sha256:" + testZeros(64), ProjectStateRevision: "ps_000000004",
						BaseCommit: "91acd8273f1", ChangeClass: protocol.ChangeSystemic,
					}).
					Add(&events.TaskDelegated{
						TaskID: taskID, WorkPackageID: wpID, WorkerRole: "implementer",
					}).
					Add(&events.AttemptStarted{
						TaskID: taskID, AttemptID: attemptID, WorkPackageID: wpID,
						WorkPackageVersion: 1, ProjectStateRevision: "ps_000000004",
						WorkerRole: "implementer",
					}).
					Add(&events.EscalationRaised{
						TaskID: taskID, EscalationID: "esc_0001",
						Reason: tasks.BlockedReason{
							Trigger:      "contradicted_assumption",
							Statement:    "Assumption A2 is false.",
							Authority:    protocol.AuthorityPrincipal,
							EvidenceRefs: []string{"ev_callers"},
						},
					})
			},
			rejected: &events.CandidateProduced{
				TaskID: taskID, AttemptID: attemptID,
				CandidateCommit: "cafebabe1234567", Summary: "late candidate",
			},
		},
		{
			// A duplicate decision must not close the open question first.
			name: "duplicate decision that also resolves a question",
			prefix: func(t *testing.T) *testsupport.ScenarioBuilder {
				return testsupport.NewScenario(t, "example").
					Add(&events.ProjectInitialized{Name: "a", MilestoneID: "M1", MilestoneTitle: "t"}).
					Add(&events.DecisionRequired{
						DecisionRequiredID: "DR-001", Question: "q", Authority: protocol.AuthorityPrincipal,
					}).
					Add(&events.DecisionRecorded{
						DecisionID: "dec_1", RecordDigest: "sha256:x", Question: "q", Selected: "a",
					}).
					Add(&events.DecisionRequired{
						DecisionRequiredID: "DR-002", Question: "q2", Authority: protocol.AuthorityPrincipal,
					})
			},
			rejected: &events.DecisionRecorded{
				DecisionID: "dec_1", RecordDigest: "sha256:x", Question: "q2",
				Selected: "b", ResolvesDecisionRequired: "DR-002",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			builder := tc.prefix(t)
			projection, err := state.Reduce(builder.Stream())
			if err != nil {
				t.Fatalf("reduce prefix: %v", err)
			}
			before, err := projection.ProjectState()
			if err != nil {
				t.Fatalf("render before: %v", err)
			}
			beforeJSON, _ := protocol.CanonicalJSON(before)
			watermarkBefore := projection.HighWatermark

			// The rejected event is the one immediately after the prefix.
			full := builder.Add(tc.rejected).Stream()
			rejected := full[len(full)-1]
			if err := projection.Apply(&rejected); err == nil {
				t.Fatal("the event was accepted")
			}

			if projection.HighWatermark != watermarkBefore {
				t.Fatalf("high-watermark moved to %d after a rejected event", projection.HighWatermark)
			}
			after, err := projection.ProjectState()
			if err != nil {
				t.Fatalf("render after: %v", err)
			}
			afterJSON, _ := protocol.CanonicalJSON(after)
			if string(afterJSON) != string(beforeJSON) {
				t.Fatalf("a rejected event changed the projection:\n%s\n%s", afterJSON, beforeJSON)
			}
			// ProjectState does not surface attempts, so check them directly:
			// a half-applied CandidateProduced would show here and nowhere
			// else.
			for _, attempt := range projection.AttemptsForTask(taskID) {
				if attempt.Status != tasks.AttemptRunning && attempt.CandidateCommit != "" {
					t.Fatalf("a rejected event terminated attempt %s with a candidate", attempt.ID)
				}
			}
		})
	}
}
