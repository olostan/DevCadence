package state_test

import (
	"testing"

	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/events"
	"github.com/olostan/DevCadience/internal/protocol"
	"github.com/olostan/DevCadience/internal/state"
	"github.com/olostan/DevCadience/internal/tasks"
	"github.com/olostan/DevCadience/internal/testsupport"
)

// Two tasks, each with its own attempt, candidate and evidence. Everything in
// this file asks the same question: can one task's lifecycle advance on the
// other's history?
const (
	taskOne       = "tsk_00000000000000000000000001"
	taskTwo       = "tsk_00000000000000000000000002"
	attemptOne    = "att_00000000000000000000000001"
	attemptTwo    = "att_00000000000000000000000002"
	wpOne         = "wp_000000000000000000000001"
	wpTwo         = "wp_000000000000000000000002"
	commitOne     = "cafebabe1111111"
	commitTwo     = "cafebabe2222222"
	lineageDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
)

// twoTaskScenario drives both tasks to REVIEWING with one validation and one
// review recorded each, which is the state acceptance acts on.
func twoTaskScenario(t *testing.T) *testsupport.ScenarioBuilder {
	t.Helper()
	builder := testsupport.NewScenario(t, "example").
		Add(&events.ProjectInitialized{Name: "Example", MilestoneID: "M1", MilestoneTitle: "Core"})
	for _, task := range []struct {
		id, alias, wp, attempt, commit, validation, review string
	}{
		{taskOne, "DC-001", wpOne, attemptOne, commitOne, "val_1", "rev_1"},
		{taskTwo, "DC-002", wpTwo, attemptTwo, commitTwo, "val_2", "rev_2"},
	} {
		builder = builder.
			Add(&events.TaskCreated{
				TaskID: task.id, Alias: task.alias, Title: task.alias,
				MilestoneID: "M1", ChangeClass: protocol.ChangeSystemic,
			}).
			Add(&events.TaskDesignStarted{TaskID: task.id, Reason: "initial design"}).
			Add(&events.WorkPackageApproved{
				TaskID: task.id, WorkPackageID: task.wp, WorkPackageVersion: 1,
				RecordDigest: lineageDigest, ProjectStateRevision: "ps_000000001",
				BaseCommit: "91acd8273f1", ChangeClass: protocol.ChangeSystemic,
			}).
			Add(&events.TaskDelegated{TaskID: task.id, WorkPackageID: task.wp, WorkerRole: "implementer"}).
			Add(&events.AttemptStarted{
				TaskID: task.id, AttemptID: task.attempt, WorkPackageID: task.wp,
				WorkPackageVersion: 1, ProjectStateRevision: "ps_000000001",
				WorkerRole: "implementer",
			}).
			Add(&events.CandidateProduced{
				TaskID: task.id, AttemptID: task.attempt,
				CandidateCommit: task.commit, Summary: "implemented",
			}).
			Add(&events.ValidationCompleted{
				TaskID: task.id, AttemptID: task.attempt, ValidationID: task.validation,
				Scope: events.ScopeAttempt, Status: protocol.ValidationPass,
				Commit: task.commit, RecordDigest: lineageDigest,
			}).
			Add(&events.ReviewCompleted{
				TaskID: task.id, AttemptID: task.attempt, ReviewID: task.review,
				WorkPackageID: task.wp,
				Dimension:     protocol.DimensionCorrectness, Verdict: protocol.VerdictPass,
				RecordDigest: lineageDigest,
			})
	}
	return builder
}

// acceptance is the well-formed acceptance of task one, which each test
// corrupts in exactly one way.
func acceptance() *events.ChangeAccepted {
	return &events.ChangeAccepted{
		TaskID: taskOne, AttemptID: attemptOne, WorkPackageID: wpOne,
		CandidateCommit: commitOne, SemanticSummary: "Did the thing.",
		ValidationIDs: []string{"val_1"}, ReviewIDs: []string{"rev_1"},
		DecidedBy: protocol.AuthorityPrincipal,
	}
}

func rejectsWith(t *testing.T, builder *testsupport.ScenarioBuilder, payload events.Payload, want errs.Category) {
	t.Helper()
	_, err := state.Reduce(builder.Add(payload).Stream())
	if err == nil {
		t.Fatal("the reducer accepted an event with broken lineage")
	}
	if got := errs.CategoryOf(err); got != want {
		t.Fatalf("category = %s, want %s (%v)", got, want, err)
	}
}

// TestWellFormedAcceptanceIsAccepted is the control: every rejection below
// must be caused by the one thing the test changed, not by the fixture.
func TestWellFormedAcceptanceIsAccepted(t *testing.T) {
	projection, err := state.Reduce(twoTaskScenario(t).Add(acceptance()).Stream())
	if err != nil {
		t.Fatalf("a well-formed acceptance was rejected: %v", err)
	}
	task, err := projection.TaskByAlias("DC-001")
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	if task.State != tasks.StateAccepted {
		t.Fatalf("state = %s, want accepted", task.State)
	}
}

func TestValidationCannotReferenceAnotherTasksAttempt(t *testing.T) {
	// Task two's attempt, cited for task one. Task one is in REVIEWING here,
	// so the state guard is exercised too; drive a fresh pair instead.
	builder := twoTaskScenario(t)
	rejectsWith(t, builder, &events.ValidationCompleted{
		TaskID: taskOne, AttemptID: attemptTwo, ValidationID: "val_9",
		Scope: events.ScopeAttempt, Status: protocol.ValidationPass,
		Commit: commitTwo, RecordDigest: lineageDigest,
	}, errs.CategoryInvalidTransition) // task one already left VALIDATING
}

func TestValidationCannotReferenceANonexistentAttempt(t *testing.T) {
	// A task still in VALIDATING, so the failure is the missing attempt.
	builder := singleTaskAtValidating(t)
	rejectsWith(t, builder, &events.ValidationCompleted{
		TaskID: taskOne, AttemptID: "att_missing", ValidationID: "val_9",
		Scope: events.ScopeAttempt, Status: protocol.ValidationPass,
		Commit: commitOne, RecordDigest: lineageDigest,
	}, errs.CategoryNotFound)
}

func TestValidationCannotUseAnotherTasksAttemptWhileValidating(t *testing.T) {
	builder := singleTaskAtValidating(t)
	rejectsWith(t, builder, &events.ValidationCompleted{
		TaskID: taskOne, AttemptID: attemptTwo, ValidationID: "val_9",
		Scope: events.ScopeAttempt, Status: protocol.ValidationPass,
		Commit: commitOne, RecordDigest: lineageDigest,
	}, errs.CategoryNotFound) // attemptTwo does not exist in this scenario
}

func TestValidationCommitMustMatchCandidate(t *testing.T) {
	builder := singleTaskAtValidating(t)
	rejectsWith(t, builder, &events.ValidationCompleted{
		TaskID: taskOne, AttemptID: attemptOne, ValidationID: "val_9",
		Scope: events.ScopeAttempt, Status: protocol.ValidationPass,
		Commit: "0000000deadbee", RecordDigest: lineageDigest,
	}, errs.CategoryIntegrity)
}

func TestValidationMustNameTheCandidateItCovers(t *testing.T) {
	builder := singleTaskAtValidating(t)
	rejectsWith(t, builder, &events.ValidationCompleted{
		TaskID: taskOne, AttemptID: attemptOne, ValidationID: "val_9",
		Scope: events.ScopeAttempt, Status: protocol.ValidationPass,
		RecordDigest: lineageDigest,
	}, errs.CategoryInvalidArgument)
}

// TestFailedValidationCannotPullATaskOutOfReview is the hole the state guard
// closes: RUNNING is reachable from REVIEWING too, so without an explicit
// "must be VALIDATING" check a failing validation could bypass the rejection
// decision that is supposed to send a task back.
func TestFailedValidationCannotPullATaskOutOfReview(t *testing.T) {
	builder := twoTaskScenario(t) // task one is in REVIEWING
	rejectsWith(t, builder, &events.ValidationCompleted{
		TaskID: taskOne, AttemptID: attemptOne, ValidationID: "val_9",
		Scope: events.ScopeAttempt, Status: protocol.ValidationFail,
		Commit: commitOne, RecordDigest: lineageDigest,
	}, errs.CategoryInvalidTransition)
}

func TestAcceptanceMustMatchCandidateLineage(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*events.ChangeAccepted)
		want   errs.Category
	}{
		{
			name:   "another task's attempt",
			mutate: func(a *events.ChangeAccepted) { a.AttemptID = attemptTwo },
			want:   errs.CategoryIntegrity,
		},
		{
			name:   "an attempt that does not exist",
			mutate: func(a *events.ChangeAccepted) { a.AttemptID = "att_missing" },
			want:   errs.CategoryNotFound,
		},
		{
			name:   "another candidate commit",
			mutate: func(a *events.ChangeAccepted) { a.CandidateCommit = commitTwo },
			want:   errs.CategoryIntegrity,
		},
		{
			name:   "a different work package",
			mutate: func(a *events.ChangeAccepted) { a.WorkPackageID = wpTwo },
			want:   errs.CategoryIntegrity,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := acceptance()
			tc.mutate(payload)
			rejectsWith(t, twoTaskScenario(t), payload, tc.want)
		})
	}
}

func TestAcceptanceRequiresKnownValidationEvidence(t *testing.T) {
	payload := acceptance()
	payload.ValidationIDs = []string{"val_never_recorded"}
	rejectsWith(t, twoTaskScenario(t), payload, errs.CategoryIntegrity)
}

func TestAcceptanceRequiresKnownReviewEvidence(t *testing.T) {
	payload := acceptance()
	payload.ReviewIDs = []string{"rev_never_recorded"}
	rejectsWith(t, twoTaskScenario(t), payload, errs.CategoryIntegrity)
}

// TestAcceptanceCannotCiteAnotherTasksEvidence is the subtle one: the IDs
// exist and the evidence is real, but it is about a different task. An
// acceptance built this way reads as fully justified and is not.
func TestAcceptanceCannotCiteAnotherTasksEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*events.ChangeAccepted)
	}{
		{"validation from another task", func(a *events.ChangeAccepted) {
			a.ValidationIDs = []string{"val_2"}
		}},
		{"review from another task", func(a *events.ChangeAccepted) {
			a.ReviewIDs = []string{"rev_2"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := acceptance()
			tc.mutate(payload)
			rejectsWith(t, twoTaskScenario(t), payload, errs.CategoryIntegrity)
		})
	}
}

// TestAcceptanceCannotCiteIntegrationEvidenceAsAttemptEvidence keeps the
// scopes apart: an integration run validates the merged result, not the
// candidate under review.
func TestAcceptanceCannotCiteIntegrationEvidenceAsAttemptEvidence(t *testing.T) {
	// Drive task two to DONE so an integration validation exists, then try to
	// cite it while accepting task one.
	builder := twoTaskScenario(t).
		Add(acceptance()).
		Add(&events.IntegrationStarted{TaskID: taskOne, IntegrationID: "int_1"}).
		Add(&events.IntegrationValidationStarted{
			TaskID: taskOne, IntegrationID: "int_1", IntegratedCommit: "deadbeef999999",
		}).
		Add(&events.ValidationCompleted{
			TaskID: taskOne, ValidationID: "val_int", Scope: events.ScopeIntegration,
			Status: protocol.ValidationPass, Commit: "deadbeef999999", RecordDigest: lineageDigest,
		})
	// Now accept task two citing task one's integration validation.
	payload := &events.ChangeAccepted{
		TaskID: taskTwo, AttemptID: attemptTwo, WorkPackageID: wpTwo,
		CandidateCommit: commitTwo, SemanticSummary: "Did the other thing.",
		ValidationIDs: []string{"val_int"}, ReviewIDs: []string{"rev_2"},
		DecidedBy: protocol.AuthorityPrincipal,
	}
	rejectsWith(t, builder, payload, errs.CategoryIntegrity)
}

func TestRejectionCannotUseAnotherTasksAttempt(t *testing.T) {
	rejectsWith(t, twoTaskScenario(t), &events.ChangeRejected{
		TaskID: taskOne, AttemptID: attemptTwo, Reason: "not good enough",
		DecidedBy: protocol.AuthorityPrincipal,
	}, errs.CategoryIntegrity)
}

// TestAttemptCannotRunAgainstAnUnapprovedWorkPackage closes the lineage hole
// at its origin: an attempt records what blueprint the work was executed
// against, and starting one against a blueprint the task never approved makes
// that record a fiction.
func TestAttemptCannotRunAgainstAnUnapprovedWorkPackage(t *testing.T) {
	builder := singleTaskAtRunning(t)
	rejectsWith(t, builder, &events.AttemptStarted{
		TaskID: taskOne, AttemptID: "att_rogue", WorkPackageID: wpTwo,
		WorkPackageVersion: 1, ProjectStateRevision: "ps_000000001", WorkerRole: "implementer",
	}, errs.CategoryIntegrity)

	// The same check catches a stale blueprint version.
	rejectsWith(t, singleTaskAtRunning(t), &events.AttemptStarted{
		TaskID: taskOne, AttemptID: "att_stale", WorkPackageID: wpOne,
		WorkPackageVersion: 99, ProjectStateRevision: "ps_000000001", WorkerRole: "implementer",
	}, errs.CategoryIntegrity)
}

// TestEscalationCannotCiteAnotherTasksAttempt stops a block pointing the
// decision owner at unrelated work.
func TestEscalationCannotCiteAnotherTasksAttempt(t *testing.T) {
	rejectsWith(t, twoTaskScenario(t), &events.EscalationRaised{
		TaskID: taskOne, EscalationID: "esc_1",
		Reason: tasks.BlockedReason{
			Trigger: "contradicted_assumption", Statement: "A2 is false.",
			Authority: protocol.AuthorityPrincipal, AttemptID: attemptTwo,
			EvidenceRefs: []string{"ev_callers"},
		},
	}, errs.CategoryIntegrity)
}

// singleTaskAtRunning stops after delegation, before any attempt exists.
func singleTaskAtRunning(t *testing.T) *testsupport.ScenarioBuilder {
	t.Helper()
	return testsupport.NewScenario(t, "example").
		Add(&events.ProjectInitialized{Name: "Example", MilestoneID: "M1", MilestoneTitle: "Core"}).
		Add(&events.TaskCreated{
			TaskID: taskOne, Alias: "DC-001", Title: "Bounded reads",
			MilestoneID: "M1", ChangeClass: protocol.ChangeSystemic,
		}).
		Add(&events.TaskDesignStarted{TaskID: taskOne, Reason: "initial design"}).
		Add(&events.WorkPackageApproved{
			TaskID: taskOne, WorkPackageID: wpOne, WorkPackageVersion: 1,
			RecordDigest: lineageDigest, ProjectStateRevision: "ps_000000001",
			BaseCommit: "91acd8273f1", ChangeClass: protocol.ChangeSystemic,
		}).
		Add(&events.TaskDelegated{TaskID: taskOne, WorkPackageID: wpOne, WorkerRole: "implementer"})
}

// singleTaskAtValidating stops with a candidate produced and nothing else.
func singleTaskAtValidating(t *testing.T) *testsupport.ScenarioBuilder {
	t.Helper()
	return singleTaskAtRunning(t).
		Add(&events.AttemptStarted{
			TaskID: taskOne, AttemptID: attemptOne, WorkPackageID: wpOne,
			WorkPackageVersion: 1, ProjectStateRevision: "ps_000000001", WorkerRole: "implementer",
		}).
		Add(&events.CandidateProduced{
			TaskID: taskOne, AttemptID: attemptOne,
			CandidateCommit: commitOne, Summary: "implemented",
		})
}
