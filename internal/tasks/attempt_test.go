package tasks_test

import (
	"testing"
	"time"

	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/protocol"
	"github.com/olostan/DevCadience/internal/tasks"
)

func baseAttempt() tasks.Attempt {
	started := protocol.NewTimestamp(time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC))
	return tasks.Attempt{
		ID:                   "att_0001",
		ProjectID:            "example",
		TaskID:               "tsk_0001",
		Ordinal:              1,
		WorkPackageID:        "wp_0001",
		WorkPackageVersion:   1,
		ProjectStateRevision: "ps_000000004",
		WorkerRole:           "implementer",
		Status:               tasks.AttemptRunning,
		StartedAt:            started,
	}
}

func TestAttemptTerminalStatusesRequireAFinishTime(t *testing.T) {
	for _, status := range []tasks.AttemptStatus{
		tasks.AttemptCandidateProduced, tasks.AttemptBlocked,
		tasks.AttemptFailed, tasks.AttemptCancelled,
	} {
		attempt := baseAttempt()
		attempt.Status = status
		if err := attempt.Validate(); err == nil {
			t.Errorf("status %s without finished_at was accepted", status)
		} else if errs.CategoryOf(err) != errs.CategoryIntegrity {
			t.Errorf("status %s: category = %s, want integrity", status, errs.CategoryOf(err))
		}
	}
}

func TestRunningAttemptMustNotHaveAFinishTime(t *testing.T) {
	attempt := baseAttempt()
	finished := protocol.NewTimestamp(time.Date(2026, 9, 20, 13, 0, 0, 0, time.UTC))
	attempt.FinishedAt = &finished
	if err := attempt.Validate(); err == nil {
		t.Fatal("a running attempt with finished_at was accepted")
	}
}

func TestCandidateProducedRequiresACommit(t *testing.T) {
	attempt := baseAttempt()
	finished := protocol.NewTimestamp(time.Date(2026, 9, 20, 13, 0, 0, 0, time.UTC))
	attempt.Status = tasks.AttemptCandidateProduced
	attempt.FinishedAt = &finished
	if err := attempt.Validate(); err == nil {
		t.Fatal("candidate_produced without a commit was accepted")
	}
	attempt.CandidateCommit = "cafebabe123"
	if err := attempt.Validate(); err != nil {
		t.Fatalf("valid candidate attempt rejected: %v", err)
	}
}

func TestBlockedAttemptMustCarryItsReason(t *testing.T) {
	attempt := baseAttempt()
	finished := protocol.NewTimestamp(time.Date(2026, 9, 20, 13, 0, 0, 0, time.UTC))
	attempt.Status = tasks.AttemptBlocked
	attempt.FinishedAt = &finished
	if err := attempt.Validate(); err == nil {
		t.Fatal("a blocked attempt without a reason was accepted")
	}
	attempt.BlockReason = &tasks.BlockedReason{
		Trigger:   "contradicted_assumption",
		Statement: "Assumption A2 is false: three callers build the query positionally.",
		Authority: protocol.AuthorityPrincipal,
	}
	if err := attempt.Validate(); err != nil {
		t.Fatalf("valid blocked attempt rejected: %v", err)
	}
}

func TestAttemptStatusTransitions(t *testing.T) {
	terminal := []tasks.AttemptStatus{
		tasks.AttemptCandidateProduced, tasks.AttemptBlocked,
		tasks.AttemptFailed, tasks.AttemptCancelled,
	}
	for _, to := range terminal {
		if err := tasks.CheckAttemptTransition("att_0001", tasks.AttemptRunning, to); err != nil {
			t.Errorf("running -> %s rejected: %v", to, err)
		}
	}
	// A terminal attempt is historical fact. Reopening one would let a retry
	// overwrite history instead of adding an attempt.
	for _, from := range terminal {
		for _, to := range append(terminal, tasks.AttemptRunning) {
			err := tasks.CheckAttemptTransition("att_0001", from, to)
			if err == nil {
				t.Errorf("%s -> %s was accepted; terminal attempts must be immutable", from, to)
			}
		}
	}
}

func TestBlockedReasonRequiresADecisionOwner(t *testing.T) {
	reason := tasks.BlockedReason{Trigger: "contradiction", Statement: "A2 is false"}
	if err := reason.Validate(); err == nil {
		t.Fatal("a block with no authority was accepted; nobody could unblock it")
	}
	reason.Authority = protocol.AuthorityPrincipal
	if err := reason.Validate(); err != nil {
		t.Fatalf("valid reason rejected: %v", err)
	}
}

func TestTaskBlockedStateAndReasonMustAgree(t *testing.T) {
	task := tasks.Task{
		ID: "tsk_0001", ProjectID: "example", Alias: "DC-001",
		Title: "example", State: tasks.StateBlocked,
	}
	if err := task.Validate(); err == nil {
		t.Fatal("a blocked task with no reason was accepted")
	}
	task.Blocked = &tasks.BlockedReason{
		Trigger: "contradiction", Statement: "A2 is false", Authority: protocol.AuthorityPrincipal,
	}
	if err := task.Validate(); err != nil {
		t.Fatalf("valid blocked task rejected: %v", err)
	}
	// The inverse: a stale reason on a running task would leak a resolved
	// block into ProjectState.
	task.State = tasks.StateRunning
	if err := task.Validate(); err == nil {
		t.Fatal("a running task carrying a block reason was accepted")
	}
}
