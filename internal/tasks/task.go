package tasks

import (
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/protocol"
)

// BlockedReason preserves why a task cannot progress.
//
// ENGINEERING_STANDARDS.md §15 and DCI-045 require that a block name the
// violated or uncertain assumption, cite evidence, and identify a decision
// owner, so that escalation is decision-oriented rather than a failure dump.
type BlockedReason struct {
	// Trigger is the short machine-readable cause, e.g.
	// "contradicted_assumption" or "integration_regression".
	Trigger string `json:"trigger"`
	// Statement is the blocking question or violated assumption in prose.
	Statement string `json:"statement"`
	// Authority names who can unblock the task.
	Authority protocol.DecisionAuthority `json:"authority"`
	// EvidenceRefs point at the evidence supporting the block (DCI-011).
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
	// AttemptID names the attempt that surfaced the block, when there was one.
	AttemptID string `json:"attempt_id,omitempty"`
	// BlockedFrom records the state the task was in when it blocked, so the
	// history remains interpretable even though resume always routes through
	// StateDesigning.
	BlockedFrom State `json:"blocked_from"`
}

// Validate checks that a block carries enough information to act on.
func (r BlockedReason) Validate() error {
	if r.Trigger == "" {
		return errs.New(errs.CategoryInvalidArgument, "blocked reason: trigger is required")
	}
	if r.Statement == "" {
		return errs.New(errs.CategoryInvalidArgument, "blocked reason: statement is required")
	}
	if !r.Authority.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"blocked reason: authority %q is not a known decision authority", string(r.Authority))
	}
	return nil
}

// Task is the projected current view of one delivery task.
//
// It is derived from the event journal, never authored directly: every field
// here is reproduced by replaying events (see internal/state). Storing it is
// a query optimisation, not a second source of truth.
type Task struct {
	ID        string `json:"task_id"`
	ProjectID string `json:"project_id"`
	// Alias is the human-readable handle ("DC-012") layered over the opaque
	// ID, as docs/PROTOCOLS.md §2 prescribes.
	Alias       string               `json:"alias"`
	Title       string               `json:"title"`
	MilestoneID string               `json:"milestone_id"`
	ChangeClass protocol.ChangeClass `json:"change_class"`
	State       State                `json:"state"`

	// WorkPackageID and WorkPackageVersion name the approved blueprint that
	// currently governs the task, if any.
	WorkPackageID      string `json:"work_package_id,omitempty"`
	WorkPackageVersion int    `json:"work_package_version,omitempty"`

	// CurrentAttemptID names the most recently started attempt. Previous
	// attempts are never overwritten; see Attempt.
	CurrentAttemptID string `json:"current_attempt_id,omitempty"`

	// Blocked is set exactly when State == StateBlocked.
	Blocked *BlockedReason `json:"blocked,omitempty"`

	// AcceptedCommit is the candidate commit accepted for this task.
	AcceptedCommit string `json:"accepted_commit,omitempty"`

	// CreatedSeq and UpdatedSeq are event-journal sequences. They give tasks
	// a deterministic ordering that does not depend on wall-clock time.
	CreatedSeq int64 `json:"created_seq"`
	UpdatedSeq int64 `json:"updated_seq"`
}

// Validate checks the task's internal consistency.
func (t *Task) Validate() error {
	if t.ID == "" {
		return errs.New(errs.CategoryInvalidArgument, "task: id is required")
	}
	if t.ProjectID == "" {
		return errs.New(errs.CategoryInvalidArgument, "task %s: project_id is required", t.ID)
	}
	if t.Title == "" {
		return errs.New(errs.CategoryInvalidArgument, "task %s: title is required", t.ID)
	}
	if !t.State.Valid() {
		return errs.New(errs.CategoryIntegrity, "task %s: state %q is not a known task state", t.ID, string(t.State))
	}
	if t.ChangeClass != "" && !t.ChangeClass.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"task %s: change_class %q is not a known change class", t.ID, string(t.ChangeClass))
	}
	// A blocked task without a reason would violate the requirement that a
	// block always preserve why; a reason on a non-blocked task would let a
	// stale block leak into ProjectState.
	if (t.State == StateBlocked) != (t.Blocked != nil) {
		return errs.New(errs.CategoryIntegrity,
			"task %s: state %s and blocked reason presence (%t) disagree", t.ID, t.State, t.Blocked != nil)
	}
	if t.Blocked != nil {
		if err := t.Blocked.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// BlockedAuthority returns the authority that can unblock the task, or the
// empty authority when the task is not blocked.
func (t *Task) BlockedAuthority() protocol.DecisionAuthority {
	if t.Blocked == nil {
		return ""
	}
	return t.Blocked.Authority
}
