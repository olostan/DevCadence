package events

import (
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/protocol"
	"github.com/olostan/DevCadience/internal/tasks"
)

// Event types that drive the delivery task lifecycle of docs/LIFECYCLE.md §12.
//
// Each of these either creates a task, moves it along exactly one edge of the
// state machine, or records evidence attached to it. The mapping from event to
// edge lives in internal/state so that events stay a description of what
// happened rather than a description of what to do about it.
const (
	TypeTaskCreated                  Type = "TaskCreated"
	TypeTaskScoutingStarted          Type = "TaskScoutingStarted"
	TypeTaskDesignStarted            Type = "TaskDesignStarted"
	TypeWorkPackageApproved          Type = "WorkPackageApproved"
	TypeTaskDelegated                Type = "TaskDelegated"
	TypeAttemptStarted               Type = "AttemptStarted"
	TypeCandidateProduced            Type = "CandidateProduced"
	TypeAttemptBlocked               Type = "AttemptBlocked"
	TypeAttemptFailed                Type = "AttemptFailed"
	TypeValidationCompleted          Type = "ValidationCompleted"
	TypeReviewCompleted              Type = "ReviewCompleted"
	TypeChangeAccepted               Type = "ChangeAccepted"
	TypeChangeRejected               Type = "ChangeRejected"
	TypeEscalationRaised             Type = "EscalationRaised"
	TypeIntegrationStarted           Type = "IntegrationStarted"
	TypeIntegrationValidationStarted Type = "IntegrationValidationStarted"
)

// TaskCreated introduces a task in state PROPOSED.
type TaskCreated struct {
	TaskID      string               `json:"task_id"`
	Alias       string               `json:"alias"`
	Title       string               `json:"title"`
	MilestoneID string               `json:"milestone_id"`
	ChangeClass protocol.ChangeClass `json:"change_class"`
	// DependsOn names tasks that must reach DONE first. M1 records the edges;
	// dependency-aware scheduling is M9 work.
	DependsOn []string `json:"depends_on,omitempty"`
}

// Type implements Payload.
func (p *TaskCreated) Type() Type { return TypeTaskCreated }

// Validate implements Payload.
func (p *TaskCreated) Validate() error {
	if p.TaskID == "" || p.Title == "" {
		return errs.New(errs.CategoryInvalidArgument, "TaskCreated: task_id and title are required")
	}
	if p.Alias == "" {
		return errs.New(errs.CategoryInvalidArgument, "TaskCreated: alias is required")
	}
	if !p.ChangeClass.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"TaskCreated: change_class %q is not a known change class", string(p.ChangeClass))
	}
	return nil
}

// TaskScoutingStarted moves a proposed task into local reconnaissance.
type TaskScoutingStarted struct {
	TaskID string `json:"task_id"`
	// InvestigationID references the InvestigationRequest driving the scout.
	// It is optional in M1 because no scout exists yet.
	InvestigationID string `json:"investigation_id,omitempty"`
}

// Type implements Payload.
func (p *TaskScoutingStarted) Type() Type { return TypeTaskScoutingStarted }

// Validate implements Payload.
func (p *TaskScoutingStarted) Validate() error {
	return requireTaskID("TaskScoutingStarted", p.TaskID)
}

// TaskDesignStarted moves a task into principal design.
//
// It is also the only way out of BLOCKED, so that unblocking is always an
// explicit design act rather than an implicit resume (DCI-024).
type TaskDesignStarted struct {
	TaskID string `json:"task_id"`
	// Reason explains why design is (re)starting, e.g. "initial design" or
	// the block being addressed.
	Reason string `json:"reason"`
	// EvidencePacketRefs ground the design in scout output when present.
	EvidencePacketRefs []string `json:"evidence_packet_refs,omitempty"`
}

// Type implements Payload.
func (p *TaskDesignStarted) Type() Type { return TypeTaskDesignStarted }

// Validate implements Payload.
func (p *TaskDesignStarted) Validate() error {
	if err := requireTaskID("TaskDesignStarted", p.TaskID); err != nil {
		return err
	}
	if p.Reason == "" {
		return errs.New(errs.CategoryInvalidArgument, "TaskDesignStarted: reason is required")
	}
	return nil
}

// WorkPackageApproved marks a blueprint ready for delegation and moves the
// task to READY.
//
// The blueprint body is not in the payload: it is stored as an immutable
// record and referenced here by id, version and digest. The digest is what
// lets a later reader prove the record it retrieves is the one that was
// approved (docs/SECURITY.md §14).
type WorkPackageApproved struct {
	TaskID               string               `json:"task_id"`
	WorkPackageID        string               `json:"work_package_id"`
	WorkPackageVersion   int                  `json:"work_package_version"`
	RecordDigest         string               `json:"record_digest"`
	ProjectStateRevision string               `json:"project_state_revision"`
	BaseCommit           string               `json:"base_commit"`
	ChangeClass          protocol.ChangeClass `json:"change_class"`
}

// Type implements Payload.
func (p *WorkPackageApproved) Type() Type { return TypeWorkPackageApproved }

// Validate implements Payload.
func (p *WorkPackageApproved) Validate() error {
	if err := requireTaskID("WorkPackageApproved", p.TaskID); err != nil {
		return err
	}
	if p.WorkPackageID == "" || p.RecordDigest == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"WorkPackageApproved: work_package_id and record_digest are required")
	}
	if p.WorkPackageVersion < 1 {
		return errs.New(errs.CategoryInvalidArgument, "WorkPackageApproved: work_package_version must be >= 1")
	}
	if p.ProjectStateRevision == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"WorkPackageApproved: project_state_revision is required so that stale plans stay detectable")
	}
	if !p.ChangeClass.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"WorkPackageApproved: change_class %q is not a known change class", string(p.ChangeClass))
	}
	return nil
}

// TaskDelegated moves a READY task to RUNNING by assigning it to a worker
// role and capability profile.
type TaskDelegated struct {
	TaskID        string `json:"task_id"`
	WorkPackageID string `json:"work_package_id"`
	WorkerRole    string `json:"worker_role"`
	WorkerProfile string `json:"worker_profile,omitempty"`
	// MaxAttempts is the retry bound policy assigned to this delegation
	// (FR-016). Enforcing it is control-plane work; recording it here keeps
	// the bound auditable after the fact.
	MaxAttempts int `json:"max_attempts,omitempty"`
}

// Type implements Payload.
func (p *TaskDelegated) Type() Type { return TypeTaskDelegated }

// Validate implements Payload.
func (p *TaskDelegated) Validate() error {
	if err := requireTaskID("TaskDelegated", p.TaskID); err != nil {
		return err
	}
	if p.WorkPackageID == "" || p.WorkerRole == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"TaskDelegated: work_package_id and worker_role are required")
	}
	if p.MaxAttempts < 0 {
		return errs.New(errs.CategoryInvalidArgument, "TaskDelegated: max_attempts must not be negative")
	}
	return nil
}

// AttemptStarted creates an immutable execution unit under a RUNNING task.
// It does not change the task state: the task is already RUNNING, and a task
// may accumulate several attempts over its life.
type AttemptStarted struct {
	TaskID               string `json:"task_id"`
	AttemptID            string `json:"attempt_id"`
	WorkPackageID        string `json:"work_package_id"`
	WorkPackageVersion   int    `json:"work_package_version"`
	ProjectStateRevision string `json:"project_state_revision"`
	BaseCommit           string `json:"base_commit,omitempty"`
	WorkerRole           string `json:"worker_role"`
	WorkerProfile        string `json:"worker_profile,omitempty"`
	ModelIdentity        string `json:"model_identity,omitempty"`
	WorktreeID           string `json:"worktree_id,omitempty"`
}

// Type implements Payload.
func (p *AttemptStarted) Type() Type { return TypeAttemptStarted }

// Validate implements Payload.
func (p *AttemptStarted) Validate() error {
	if err := requireTaskID("AttemptStarted", p.TaskID); err != nil {
		return err
	}
	if p.AttemptID == "" || p.WorkPackageID == "" || p.WorkerRole == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"AttemptStarted: attempt_id, work_package_id and worker_role are required")
	}
	if p.WorkPackageVersion < 1 {
		return errs.New(errs.CategoryInvalidArgument, "AttemptStarted: work_package_version must be >= 1")
	}
	return nil
}

// CandidateProduced records that an attempt produced a candidate commit and
// moves the task to VALIDATING.
type CandidateProduced struct {
	TaskID          string `json:"task_id"`
	AttemptID       string `json:"attempt_id"`
	CandidateCommit string `json:"candidate_commit"`
	Summary         string `json:"summary"`
	// RepairIterations is how many internal fix-and-recheck cycles the worker
	// ran inside this attempt. It is distinct from the attempt ordinal, which
	// counts retries across attempts; policy bounds the two separately.
	RepairIterations int                    `json:"repair_iterations,omitempty"`
	Artifacts        []protocol.ArtifactRef `json:"artifacts,omitempty"`
}

// Type implements Payload.
func (p *CandidateProduced) Type() Type { return TypeCandidateProduced }

// Validate implements Payload.
func (p *CandidateProduced) Validate() error {
	if err := requireTaskID("CandidateProduced", p.TaskID); err != nil {
		return err
	}
	if p.AttemptID == "" || p.CandidateCommit == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"CandidateProduced: attempt_id and candidate_commit are required")
	}
	if p.RepairIterations < 0 {
		return errs.New(errs.CategoryInvalidArgument, "CandidateProduced: repair_iterations must not be negative")
	}
	for _, a := range p.Artifacts {
		if err := a.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// AttemptBlocked terminates an attempt on a contradiction or an unsatisfiable
// MUST constraint.
//
// It does not itself block the task: docs/LIFECYCLE.md §13 has the control
// plane verify the contradiction independently before escalating, and that
// escalation is a separate EscalationRaised event.
type AttemptBlocked struct {
	TaskID    string              `json:"task_id"`
	AttemptID string              `json:"attempt_id"`
	Reason    tasks.BlockedReason `json:"reason"`
}

// Type implements Payload.
func (p *AttemptBlocked) Type() Type { return TypeAttemptBlocked }

// Validate implements Payload.
func (p *AttemptBlocked) Validate() error {
	if err := requireTaskID("AttemptBlocked", p.TaskID); err != nil {
		return err
	}
	if p.AttemptID == "" {
		return errs.New(errs.CategoryInvalidArgument, "AttemptBlocked: attempt_id is required")
	}
	return p.Reason.Validate()
}

// AttemptFailed terminates an attempt that produced neither a candidate nor a
// reportable contradiction, for example a crashed or cancelled worker.
type AttemptFailed struct {
	TaskID           string                 `json:"task_id"`
	AttemptID        string                 `json:"attempt_id"`
	Summary          string                 `json:"summary"`
	Cancelled        bool                   `json:"cancelled,omitempty"`
	RepairIterations int                    `json:"repair_iterations,omitempty"`
	Artifacts        []protocol.ArtifactRef `json:"artifacts,omitempty"`
}

// Type implements Payload.
func (p *AttemptFailed) Type() Type { return TypeAttemptFailed }

// Validate implements Payload.
func (p *AttemptFailed) Validate() error {
	if err := requireTaskID("AttemptFailed", p.TaskID); err != nil {
		return err
	}
	if p.AttemptID == "" || p.Summary == "" {
		return errs.New(errs.CategoryInvalidArgument, "AttemptFailed: attempt_id and summary are required")
	}
	return nil
}

// ValidationScope is protocol.ValidationScope. The compact event and the
// durable ValidationResult name the same three scopes, so they share one
// enumeration rather than keeping two that must be kept in step by hand.
type ValidationScope = protocol.ValidationScope

const (
	ScopeAttempt     = protocol.ScopeAttempt
	ScopeIntegration = protocol.ScopeIntegration
	ScopeBaseline    = protocol.ScopeBaseline
)

// ValidationCompleted records deterministic evidence.
//
// Scope ATTEMPT drives VALIDATING -> REVIEWING on pass and VALIDATING ->
// RUNNING on failure, where the repair proceeds as a new attempt rather than
// reopening the terminated one. Scope INTEGRATION drives
// INTEGRATION_VALIDATING -> DONE on pass and, on failure, requires a separate
// EscalationRaised to block the task. Scope BASELINE touches no task.
type ValidationCompleted struct {
	TaskID       string                     `json:"task_id,omitempty"`
	AttemptID    string                     `json:"attempt_id,omitempty"`
	ValidationID string                     `json:"validation_id"`
	Scope        ValidationScope            `json:"scope"`
	Status       protocol.ValidationOutcome `json:"status"`
	Commit       string                     `json:"commit,omitempty"`
	RecordDigest string                     `json:"record_digest"`
	// FailedChecks lists the check ids that did not pass, so that a reader of
	// the journal alone can see what failed without loading the full record.
	FailedChecks []string `json:"failed_checks,omitempty"`
}

// Type implements Payload.
func (p *ValidationCompleted) Type() Type { return TypeValidationCompleted }

// Validate implements Payload.
func (p *ValidationCompleted) Validate() error {
	if p.ValidationID == "" || p.RecordDigest == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"ValidationCompleted: validation_id and record_digest are required")
	}
	if !p.Scope.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"ValidationCompleted: scope %q is not a known validation scope", string(p.Scope))
	}
	if !p.Status.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"ValidationCompleted: status %q is not a known validation outcome", string(p.Status))
	}
	if p.Scope != ScopeBaseline && p.TaskID == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"ValidationCompleted: task_id is required for scope %s", p.Scope)
	}
	if p.Scope == ScopeAttempt && p.AttemptID == "" {
		return errs.New(errs.CategoryInvalidArgument, "ValidationCompleted: attempt_id is required for scope attempt")
	}
	if p.Scope != ScopeAttempt && p.AttemptID != "" {
		return errs.New(errs.CategoryInvalidArgument,
			"ValidationCompleted: attempt_id is not meaningful for scope %s", p.Scope)
	}
	if p.Scope == ScopeBaseline && p.TaskID != "" {
		return errs.New(errs.CategoryInvalidArgument,
			"ValidationCompleted: scope baseline validates the accepted commit outside any task")
	}
	// Every scope names the commit it validated. Evidence that does not say
	// what it is about cannot be read as covering anything in particular, and
	// the durable ValidationResult requires it too.
	if p.Commit == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"ValidationCompleted: commit is required; evidence must name the tree it validated")
	}
	if p.Status == protocol.ValidationPass && len(p.FailedChecks) > 0 {
		return errs.New(errs.CategoryIntegrity,
			"ValidationCompleted: status is pass but %d check(s) are listed as failed", len(p.FailedChecks))
	}
	return nil
}

// ReviewCompleted records one review dimension's result.
//
// It does not change task state. A task leaves REVIEWING only through an
// explicit acceptance or rejection decision, because DCI-044 forbids
// collapsing several reviewers' verdicts into an automatic outcome.
type ReviewCompleted struct {
	TaskID    string `json:"task_id"`
	AttemptID string `json:"attempt_id"`
	ReviewID  string `json:"review_id"`
	// WorkPackageID names the blueprint the candidate was reviewed against.
	// A review is an assessment of a candidate *against the Work Package that
	// governed the attempt*, so a review whose record cites a different
	// blueprint is not evidence about this work at all — and without this
	// field the event could not corroborate the record's own claim.
	//
	// No version is carried: the attempt already pins the exact Work Package
	// version it ran against, and ReviewResult records only the id, so a
	// version here could be checked against nothing.
	WorkPackageID                  string                   `json:"work_package_id"`
	Dimension                      protocol.ReviewDimension `json:"dimension"`
	Verdict                        protocol.ReviewVerdict   `json:"verdict"`
	RecordDigest                   string                   `json:"record_digest"`
	PrincipalEscalationRecommended bool                     `json:"principal_escalation_recommended,omitempty"`
}

// Type implements Payload.
func (p *ReviewCompleted) Type() Type { return TypeReviewCompleted }

// Validate implements Payload.
func (p *ReviewCompleted) Validate() error {
	if err := requireTaskID("ReviewCompleted", p.TaskID); err != nil {
		return err
	}
	if p.AttemptID == "" || p.ReviewID == "" || p.RecordDigest == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"ReviewCompleted: attempt_id, review_id and record_digest are required")
	}
	if p.WorkPackageID == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"ReviewCompleted: work_package_id is required; a review names the blueprint it judged against")
	}
	if !p.Dimension.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"ReviewCompleted: dimension %q is not a known review dimension", string(p.Dimension))
	}
	if !p.Verdict.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"ReviewCompleted: verdict %q is not a known verdict", string(p.Verdict))
	}
	return nil
}

// ChangeAccepted moves a reviewed task to ACCEPTED.
//
// docs/OBSERVABILITY.md §9 requires that an acceptance be explainable, so the
// payload names the evidence the decision rested on rather than only its
// outcome.
type ChangeAccepted struct {
	TaskID          string `json:"task_id"`
	AttemptID       string `json:"attempt_id"`
	WorkPackageID   string `json:"work_package_id"`
	CandidateCommit string `json:"candidate_commit"`
	// SemanticSummary is the compact delta the principal uses instead of a
	// diff (docs/PROJECT_STATE.md §8).
	SemanticSummary string   `json:"semantic_summary"`
	ValidationIDs   []string `json:"validation_ids,omitempty"`
	ReviewIDs       []string `json:"review_ids,omitempty"`
	// UnresolvedDisagreements are preserved rather than averaged away (DCI-044).
	UnresolvedDisagreements []string                   `json:"unresolved_disagreements,omitempty"`
	DecidedBy               protocol.DecisionAuthority `json:"decided_by"`
}

// Type implements Payload.
func (p *ChangeAccepted) Type() Type { return TypeChangeAccepted }

// Validate implements Payload.
func (p *ChangeAccepted) Validate() error {
	if err := requireTaskID("ChangeAccepted", p.TaskID); err != nil {
		return err
	}
	if p.AttemptID == "" || p.CandidateCommit == "" || p.SemanticSummary == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"ChangeAccepted: attempt_id, candidate_commit and semantic_summary are required")
	}
	// FR-017: acceptance MUST reference the Work Package and the
	// deterministic validation it rests on. Both are required here rather
	// than merely checked when present, because an acceptance that names
	// neither cannot be explained afterwards (docs/OBSERVABILITY.md §13).
	if p.WorkPackageID == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"ChangeAccepted: work_package_id is required; an acceptance must name the blueprint it satisfied")
	}
	if len(p.ValidationIDs) == 0 {
		return errs.New(errs.CategoryInvalidArgument,
			"ChangeAccepted: at least one validation_id is required; "+
				"model review is not a substitute for deterministic evidence (DCI-040)")
	}
	// DCI-032: a candidate change has, at minimum, validation results,
	// review results and a decision. Accepting with no review evidence at
	// all would make REVIEWING ceremonial, so the domain requires at least
	// one review. *Which* reviews and how many remain an M6 policy decision
	// that varies by change class and risk; the lineage of every cited
	// review is checked by the reducer.
	if len(p.ReviewIDs) == 0 {
		return errs.New(errs.CategoryInvalidArgument,
			"ChangeAccepted: at least one review_id is required; "+
				"an implementation candidate may not be accepted without independent review (DCI-032)")
	}
	if !p.DecidedBy.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"ChangeAccepted: decided_by %q is not a known decision authority", string(p.DecidedBy))
	}
	return nil
}

// ChangeRejected sends a reviewed task back for a repair attempt.
//
// Rejection that ends the work is not this event: that is an escalation,
// because abandoning a task is a decision someone must own (DCI-045).
type ChangeRejected struct {
	TaskID    string `json:"task_id"`
	AttemptID string `json:"attempt_id"`
	Reason    string `json:"reason"`
	// RequestedRepairs are the specific changes the next attempt must make.
	RequestedRepairs []string                   `json:"requested_repairs,omitempty"`
	DecidedBy        protocol.DecisionAuthority `json:"decided_by"`
}

// Type implements Payload.
func (p *ChangeRejected) Type() Type { return TypeChangeRejected }

// Validate implements Payload.
func (p *ChangeRejected) Validate() error {
	if err := requireTaskID("ChangeRejected", p.TaskID); err != nil {
		return err
	}
	if p.AttemptID == "" || p.Reason == "" {
		return errs.New(errs.CategoryInvalidArgument, "ChangeRejected: attempt_id and reason are required")
	}
	if !p.DecidedBy.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"ChangeRejected: decided_by %q is not a known decision authority", string(p.DecidedBy))
	}
	return nil
}

// EscalationRaised blocks a task and records why (docs/PROTOCOLS.md §13).
// It is the only event that moves a task to BLOCKED.
type EscalationRaised struct {
	TaskID       string              `json:"task_id"`
	EscalationID string              `json:"escalation_id"`
	Reason       tasks.BlockedReason `json:"reason"`
	// Options are the bounded choices offered to the decision owner.
	Options []string `json:"options,omitempty"`
	// Tried records what the local layer already attempted, so the decision
	// owner is not asked to re-suggest it.
	Tried []string `json:"tried,omitempty"`
}

// Type implements Payload.
func (p *EscalationRaised) Type() Type { return TypeEscalationRaised }

// Validate implements Payload.
func (p *EscalationRaised) Validate() error {
	if err := requireTaskID("EscalationRaised", p.TaskID); err != nil {
		return err
	}
	if p.EscalationID == "" {
		return errs.New(errs.CategoryInvalidArgument, "EscalationRaised: escalation_id is required")
	}
	return p.Reason.Validate()
}

// IntegrationStarted moves an accepted task to INTEGRATING.
type IntegrationStarted struct {
	TaskID string `json:"task_id"`
	// IntegrationID groups tasks integrated together.
	IntegrationID string `json:"integration_id"`
	BaseCommit    string `json:"base_commit,omitempty"`
}

// Type implements Payload.
func (p *IntegrationStarted) Type() Type { return TypeIntegrationStarted }

// Validate implements Payload.
func (p *IntegrationStarted) Validate() error {
	if err := requireTaskID("IntegrationStarted", p.TaskID); err != nil {
		return err
	}
	if p.IntegrationID == "" {
		return errs.New(errs.CategoryInvalidArgument, "IntegrationStarted: integration_id is required")
	}
	return nil
}

// IntegrationValidationStarted moves an integrating task to
// INTEGRATION_VALIDATING.
type IntegrationValidationStarted struct {
	TaskID           string `json:"task_id"`
	IntegrationID    string `json:"integration_id"`
	IntegratedCommit string `json:"integrated_commit,omitempty"`
}

// Type implements Payload.
func (p *IntegrationValidationStarted) Type() Type { return TypeIntegrationValidationStarted }

// Validate implements Payload.
func (p *IntegrationValidationStarted) Validate() error {
	if err := requireTaskID("IntegrationValidationStarted", p.TaskID); err != nil {
		return err
	}
	if p.IntegrationID == "" {
		return errs.New(errs.CategoryInvalidArgument, "IntegrationValidationStarted: integration_id is required")
	}
	return nil
}

func requireTaskID(kind, taskID string) error {
	if taskID == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: task_id is required", kind)
	}
	return nil
}

func init() {
	Register(TypeTaskCreated, func() Payload { return &TaskCreated{} })
	Register(TypeTaskScoutingStarted, func() Payload { return &TaskScoutingStarted{} })
	Register(TypeTaskDesignStarted, func() Payload { return &TaskDesignStarted{} })
	Register(TypeWorkPackageApproved, func() Payload { return &WorkPackageApproved{} })
	Register(TypeTaskDelegated, func() Payload { return &TaskDelegated{} })
	Register(TypeAttemptStarted, func() Payload { return &AttemptStarted{} })
	Register(TypeCandidateProduced, func() Payload { return &CandidateProduced{} })
	Register(TypeAttemptBlocked, func() Payload { return &AttemptBlocked{} })
	Register(TypeAttemptFailed, func() Payload { return &AttemptFailed{} })
	Register(TypeValidationCompleted, func() Payload { return &ValidationCompleted{} })
	Register(TypeReviewCompleted, func() Payload { return &ReviewCompleted{} })
	Register(TypeChangeAccepted, func() Payload { return &ChangeAccepted{} })
	Register(TypeChangeRejected, func() Payload { return &ChangeRejected{} })
	Register(TypeEscalationRaised, func() Payload { return &EscalationRaised{} })
	Register(TypeIntegrationStarted, func() Payload { return &IntegrationStarted{} })
	Register(TypeIntegrationValidationStarted, func() Payload { return &IntegrationValidationStarted{} })
}
