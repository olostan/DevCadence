package state

import (
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/tasks"
)

// evidenceRef is the compact lineage of one piece of recorded evidence.
//
// It is reducer-internal and never reaches ProjectState: docs/PROJECT_STATE.md
// §1 keeps the principal's view compact, and the full ValidationResult and
// ReviewResult documents live in the record store behind their ids. What the
// reducer needs is only enough to answer "is this evidence about the thing
// being accepted?", which is the task, the attempt and the candidate.
type evidenceRef struct {
	// scope distinguishes attempt evidence from integration or baseline
	// evidence, so an integration run cannot be cited as evidence for an
	// attempt.
	scope     string
	taskID    string
	attemptID string
	commit    string
}

// requireTaskInState resolves a task and asserts the state a transition
// requires.
//
// The adjacency list alone is not sufficient for this. RUNNING is reachable
// from READY, VALIDATING and REVIEWING, so an event whose only guard is
// "transition to RUNNING is legal" could move a task out of REVIEWING when it
// was only ever meant to act on a task in VALIDATING. Naming the expected
// state closes that.
func (p *Projection) requireTaskInState(taskID string, want tasks.State, what string) (*tasks.Task, error) {
	task, err := p.Task(taskID)
	if err != nil {
		return nil, err
	}
	if task.State != want {
		return nil, errs.New(errs.CategoryInvalidTransition,
			"%s requires task %s to be in %s, but it is in %s", what, task.Alias, want, task.State)
	}
	return task, nil
}

// requireAttemptOf resolves an attempt and asserts it belongs to the task.
//
// This is the check that stops one task's lifecycle advancing on another
// task's execution history (DCI-032: every candidate change has lineage).
func (p *Projection) requireAttemptOf(taskID, attemptID, what string) (*tasks.Attempt, error) {
	if attemptID == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: attempt_id is required", what)
	}
	attempt, err := p.Attempt(attemptID)
	if err != nil {
		return nil, err
	}
	if attempt.TaskID != taskID {
		task, taskErr := p.Task(taskID)
		alias := taskID
		if taskErr == nil {
			alias = task.Alias
		}
		return nil, errs.New(errs.CategoryIntegrity,
			"%s: attempt %s belongs to task %s, not to %s",
			what, attemptID, attempt.TaskID, alias)
	}
	return attempt, nil
}

// requireCandidate asserts the attempt actually produced a candidate.
//
// Validating, reviewing or accepting an attempt that produced nothing would
// be evidence about nothing.
func requireCandidate(attempt *tasks.Attempt, what string) error {
	if attempt.Status != tasks.AttemptCandidateProduced || attempt.CandidateCommit == "" {
		return errs.New(errs.CategoryIntegrity,
			"%s: attempt %s produced no candidate (status %s)", what, attempt.ID, attempt.Status)
	}
	return nil
}

// requireCandidateCommit asserts a claimed commit is the one the attempt
// produced, so evidence cannot be attached to a superseded or unrelated
// candidate.
func requireCandidateCommit(attempt *tasks.Attempt, claimed, what string) error {
	if claimed == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"%s: the candidate commit is required so that the evidence names what it is about", what)
	}
	if claimed != attempt.CandidateCommit {
		return errs.New(errs.CategoryIntegrity,
			"%s: commit %s is not the candidate attempt %s produced (%s)",
			what, claimed, attempt.ID, attempt.CandidateCommit)
	}
	return nil
}

// recordEvidence indexes a validation or review by id.
func (p *Projection) recordEvidence(index map[string]evidenceRef, kind, id string, ref evidenceRef) error {
	if _, exists := index[id]; exists {
		return errs.New(errs.CategoryConflict, "%s %s has already been recorded", kind, id)
	}
	index[id] = ref
	return nil
}

// requireEvidenceFor asserts that every cited evidence id exists and belongs
// to the attempt being accepted.
//
// docs/OBSERVABILITY.md §13: if the system cannot explain why a change was
// accepted, the acceptance mechanism is incomplete. An acceptance citing
// evidence ids that name nothing, or that name another task's evidence, is
// exactly that failure — it reads as justified and is not.
func (p *Projection) requireEvidenceFor(
	index map[string]evidenceRef, kind string, ids []string, task *tasks.Task, attempt *tasks.Attempt,
) error {
	for _, id := range ids {
		ref, known := index[id]
		if !known {
			return errs.New(errs.CategoryIntegrity,
				"acceptance of task %s cites %s %s, which was never recorded",
				task.Alias, kind, id)
		}
		if ref.scope != "" && ref.scope != string(scopeAttemptEvidence) {
			return errs.New(errs.CategoryIntegrity,
				"acceptance of task %s cites %s %s, which is %s evidence, not evidence for an attempt",
				task.Alias, kind, id, ref.scope)
		}
		if ref.taskID != task.ID {
			return errs.New(errs.CategoryIntegrity,
				"acceptance of task %s cites %s %s, which is evidence for another task",
				task.Alias, kind, id)
		}
		if ref.attemptID != attempt.ID {
			return errs.New(errs.CategoryIntegrity,
				"acceptance of task %s cites %s %s, which is evidence for attempt %s, not %s",
				task.Alias, kind, id, ref.attemptID, attempt.ID)
		}
		if ref.commit != "" && ref.commit != attempt.CandidateCommit {
			return errs.New(errs.CategoryIntegrity,
				"acceptance of task %s cites %s %s, which is evidence for candidate %s, not %s",
				task.Alias, kind, id, ref.commit, attempt.CandidateCommit)
		}
	}
	return nil
}

// scopeAttemptEvidence names the evidence scope acceptance may cite.
type evidenceScope string

const scopeAttemptEvidence evidenceScope = "attempt"
