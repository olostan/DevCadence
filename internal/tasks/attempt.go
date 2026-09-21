package tasks

import (
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/protocol"
)

// AttemptStatus is the lifecycle of one execution unit.
type AttemptStatus string

const (
	// AttemptRunning is an attempt currently executing.
	AttemptRunning AttemptStatus = "running"
	// AttemptCandidateProduced means the worker produced a candidate commit.
	AttemptCandidateProduced AttemptStatus = "candidate_produced"
	// AttemptBlocked means the worker stopped on a contradicted assumption or
	// an unsatisfiable MUST constraint (DCI-023, DCI-024).
	AttemptBlocked AttemptStatus = "blocked"
	// AttemptFailed means the attempt ended without a candidate and without a
	// reportable contradiction.
	AttemptFailed AttemptStatus = "failed"
	// AttemptCancelled means the attempt was stopped by the control plane.
	AttemptCancelled AttemptStatus = "cancelled"
)

// Valid reports whether the status is known.
func (s AttemptStatus) Valid() bool {
	switch s {
	case AttemptRunning, AttemptCandidateProduced, AttemptBlocked, AttemptFailed, AttemptCancelled:
		return true
	}
	return false
}

// Terminal reports whether the attempt has finished.
func (s AttemptStatus) Terminal() bool { return s.Valid() && s != AttemptRunning }

// attemptTransitions is the attempt lifecycle. An attempt only ever moves
// from running to a terminal status: a finished attempt is historical fact
// and is never reopened, because a retry creates a new Attempt
// (ENGINEERING_STANDARDS.md §12, docs/PROTOCOLS.md §9).
var attemptTransitions = map[AttemptStatus][]AttemptStatus{
	AttemptRunning: {AttemptCandidateProduced, AttemptBlocked, AttemptFailed, AttemptCancelled},
}

// CheckAttemptTransition returns a categorised error when the status change
// is illegal.
func CheckAttemptTransition(attemptID string, from, to AttemptStatus) error {
	if !from.Valid() {
		return errs.New(errs.CategoryIntegrity, "attempt %s: stored status %q is unknown", attemptID, string(from))
	}
	if !to.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "attempt %s: %q is not a known attempt status", attemptID, string(to))
	}
	for _, candidate := range attemptTransitions[from] {
		if candidate == to {
			return nil
		}
	}
	return errs.New(errs.CategoryInvalidTransition,
		"attempt %s: status change %s -> %s is not permitted", attemptID, from, to)
}

// Attempt is one immutable historical execution unit attached to a task and a
// Work Package version (docs/PROTOCOLS.md §9).
//
// "Immutable" means historical: the record accumulates the outcome of the run
// it describes and then never changes again, and a retry adds a new Attempt
// rather than rewriting this one. The lineage fields are what DCI-032
// requires of every candidate change.
//
// Fields that only M2 and M3 can populate (worktree, candidate commit, model
// identity) exist now because they are lineage, and lineage recorded late is
// lineage lost. M1 leaves them empty.
type Attempt struct {
	ID        string `json:"attempt_id"`
	ProjectID string `json:"project_id"`
	TaskID    string `json:"task_id"`

	// Ordinal counts attempts within the task, starting at 1. It makes
	// attempt history readable without comparing identifiers.
	Ordinal int `json:"ordinal"`

	WorkPackageID      string `json:"work_package_id"`
	WorkPackageVersion int    `json:"work_package_version"`
	// ProjectStateRevision and BaseCommit pin what the attempt was planned
	// against, which is what makes a stale plan detectable.
	ProjectStateRevision string `json:"project_state_revision"`
	BaseCommit           string `json:"base_commit,omitempty"`

	// WorkerRole is the engineering role (implementer, reviewer); WorkerProfile
	// is the capability profile it was routed to. Neither is a provider name:
	// models are not roles (docs/ARCHITECTURE.md §8).
	WorkerRole    string `json:"worker_role"`
	WorkerProfile string `json:"worker_profile,omitempty"`
	// ModelIdentity records which runtime actually ran, for replay and
	// capability accounting. Populated from M3 onwards.
	ModelIdentity string `json:"model_identity,omitempty"`
	// WorktreeID names the isolated workspace. Populated from M2 onwards
	// (DCI-030).
	WorktreeID string `json:"worktree_id,omitempty"`

	Status     AttemptStatus       `json:"status"`
	StartedAt  protocol.Timestamp  `json:"started_at"`
	FinishedAt *protocol.Timestamp `json:"finished_at,omitempty"`

	// CandidateCommit is set when the attempt produced a candidate.
	CandidateCommit string `json:"candidate_commit,omitempty"`
	// RepairIterations counts the worker's internal fix-and-recheck cycles
	// before this attempt produced its outcome. It is reported by the worker,
	// not derived from task transitions.
	//
	// Retries across attempts are counted by Ordinal instead. The two are
	// separate because they bound different things: RepairIterations bounds
	// how long one worker may iterate, Ordinal bounds how many times the task
	// may be re-delegated (FR-016). A failed validation returns the task to
	// RUNNING and the repair begins a new attempt; it does not reopen this one.
	RepairIterations int `json:"repair_iterations"`

	// BlockReason records a contradiction or unsatisfiable MUST. It is set
	// exactly when Status is AttemptBlocked.
	BlockReason *BlockedReason `json:"block_reason,omitempty"`
	// FailureSummary explains a non-contradiction failure.
	FailureSummary string `json:"failure_summary,omitempty"`

	// Artifacts reference logs, diffs and transcripts stored outside SQLite.
	Artifacts []protocol.ArtifactRef `json:"artifacts,omitempty"`

	CreatedSeq int64 `json:"created_seq"`
	UpdatedSeq int64 `json:"updated_seq"`
}

// Validate checks the attempt's internal consistency.
func (a *Attempt) Validate() error {
	if a.ID == "" {
		return errs.New(errs.CategoryInvalidArgument, "attempt: id is required")
	}
	if a.TaskID == "" {
		return errs.New(errs.CategoryInvalidArgument, "attempt %s: task_id is required", a.ID)
	}
	if a.ProjectID == "" {
		return errs.New(errs.CategoryInvalidArgument, "attempt %s: project_id is required", a.ID)
	}
	if a.Ordinal < 1 {
		return errs.New(errs.CategoryInvalidArgument, "attempt %s: ordinal must be >= 1", a.ID)
	}
	if a.WorkerRole == "" {
		return errs.New(errs.CategoryInvalidArgument, "attempt %s: worker_role is required", a.ID)
	}
	if !a.Status.Valid() {
		return errs.New(errs.CategoryIntegrity, "attempt %s: status %q is unknown", a.ID, string(a.Status))
	}
	if a.RepairIterations < 0 {
		return errs.New(errs.CategoryInvalidArgument, "attempt %s: repair_iterations must not be negative", a.ID)
	}
	// A terminal attempt must say when it ended, and a running one must not,
	// otherwise duration accounting and replay both become unreliable.
	if a.Status.Terminal() != (a.FinishedAt != nil) {
		return errs.New(errs.CategoryIntegrity,
			"attempt %s: status %s and finished_at presence (%t) disagree", a.ID, a.Status, a.FinishedAt != nil)
	}
	if a.FinishedAt != nil && a.FinishedAt.Time().Before(a.StartedAt.Time()) {
		return errs.New(errs.CategoryIntegrity, "attempt %s: finished before it started", a.ID)
	}
	if (a.Status == AttemptBlocked) != (a.BlockReason != nil) {
		return errs.New(errs.CategoryIntegrity,
			"attempt %s: status %s and block reason presence (%t) disagree", a.ID, a.Status, a.BlockReason != nil)
	}
	if a.BlockReason != nil {
		if err := a.BlockReason.Validate(); err != nil {
			return err
		}
	}
	if a.Status == AttemptCandidateProduced && a.CandidateCommit == "" {
		return errs.New(errs.CategoryIntegrity,
			"attempt %s: status %s requires a candidate commit", a.ID, a.Status)
	}
	for _, artifact := range a.Artifacts {
		if err := artifact.Validate(); err != nil {
			return err
		}
	}
	return nil
}
