package protocol

import (
	"github.com/olostan/DevCadence/internal/errs"
)

type SetupEventType string

const (
	EventExecutionCreated       SetupEventType = "execution_created"
	EventPlanApproved           SetupEventType = "plan_approved"
	EventActionStarting         SetupEventType = "action_starting"
	EventActionProcessCompleted SetupEventType = "action_process_completed"
	EventPostconditionVerified  SetupEventType = "postcondition_verified"
	EventActionTerminated       SetupEventType = "action_terminated"
	EventExecutionFinished      SetupEventType = "execution_finished"
)

func (t SetupEventType) Valid() bool {
	switch t {
	case EventExecutionCreated, EventPlanApproved, EventActionStarting, EventActionProcessCompleted,
		EventPostconditionVerified, EventActionTerminated, EventExecutionFinished:
		return true
	}
	return false
}

type ExecutionCreatedPayload struct {
	InitiatedBy string      `json:"initiated_by"`
	Target      SetupTarget `json:"target"`
}

func (p ExecutionCreatedPayload) Validate() error {
	const kind = "ExecutionCreatedPayload"
	if p.InitiatedBy == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: initiated_by is required", kind)
	}
	if !p.Target.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid target %q", kind, string(p.Target))
	}
	return nil
}

type PlanApprovedPayload struct {
	ApprovedAuthority Authority `json:"approved_authority"`
	ApprovedBy        string    `json:"approved_by"`
}

func (p PlanApprovedPayload) Validate() error {
	const kind = "PlanApprovedPayload"
	if !p.ApprovedAuthority.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid approved_authority %q", kind, string(p.ApprovedAuthority))
	}
	if p.ApprovedBy == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: approved_by is required", kind)
	}
	return nil
}

type ActionStartingPayload struct {
	ActionID       string         `json:"action_id"`
	RecipeID       string         `json:"recipe_id"`
	RecipeVersion  string         `json:"recipe_version"`
	OperationKind  *OperationKind `json:"operation_kind,omitempty"` // nil for manual actions
	IdempotencyKey string         `json:"idempotency_key"`
}

func (p ActionStartingPayload) Validate() error {
	const kind = "ActionStartingPayload"
	if p.ActionID == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: action_id is required", kind)
	}
	if p.RecipeID == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: recipe_id is required", kind)
	}
	if p.RecipeVersion == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: recipe_version is required", kind)
	}
	if p.OperationKind != nil && !p.OperationKind.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid operation_kind %q", kind, string(*p.OperationKind))
	}
	if p.IdempotencyKey == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: idempotency_key is required", kind)
	}
	return nil
}

type ActionProcessCompletedPayload struct {
	ActionID        string       `json:"action_id"`
	ExitCode        int          `json:"exit_code"`
	Signal          string       `json:"signal,omitempty"`
	OutputTruncated bool         `json:"output_truncated"`
	ArtifactRef     *ArtifactRef `json:"artifact_ref,omitempty"`
}

func (p ActionProcessCompletedPayload) Validate() error {
	const kind = "ActionProcessCompletedPayload"
	if p.ActionID == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: action_id is required", kind)
	}
	if p.ArtifactRef != nil {
		if err := p.ArtifactRef.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type PostconditionVerifiedPayload struct {
	ActionID string `json:"action_id"`
	Passed   bool   `json:"passed"`
	Detail   string `json:"detail"`
}

func (p PostconditionVerifiedPayload) Validate() error {
	const kind = "PostconditionVerifiedPayload"
	if p.ActionID == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: action_id is required", kind)
	}
	if p.Detail == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: detail is required", kind)
	}
	return nil
}

type ActionTerminatedPayload struct {
	ActionID      string       `json:"action_id"`
	Status        ActionStatus `json:"status"`
	FailureReason string       `json:"failure_reason,omitempty"`
	ErrorCategory string       `json:"error_category,omitempty"`
}

func (p ActionTerminatedPayload) Validate() error {
	const kind = "ActionTerminatedPayload"
	if p.ActionID == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: action_id is required", kind)
	}
	if !p.Status.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid status %q", kind, string(p.Status))
	}
	return nil
}

type ExecutionFinishedPayload struct {
	Status      ExecutionStatus `json:"status"`
	FinalDetail string          `json:"final_detail,omitempty"`
}

func (p ExecutionFinishedPayload) Validate() error {
	const kind = "ExecutionFinishedPayload"
	if !p.Status.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid status %q", kind, string(p.Status))
	}
	return nil
}

type EventPayload struct {
	ExecutionCreated        *ExecutionCreatedPayload        `json:"execution_created,omitempty"`
	PlanApproved            *PlanApprovedPayload            `json:"plan_approved,omitempty"`
	ActionStarting          *ActionStartingPayload          `json:"action_starting,omitempty"`
	ActionProcessCompleted  *ActionProcessCompletedPayload  `json:"action_process_completed,omitempty"`
	PostconditionVerified   *PostconditionVerifiedPayload   `json:"postcondition_verified,omitempty"`
	ActionTerminated        *ActionTerminatedPayload        `json:"action_terminated,omitempty"`
	ExecutionFinished       *ExecutionFinishedPayload       `json:"execution_finished,omitempty"`
}

func (p EventPayload) Validate(expectedType SetupEventType) error {
	const kind = "EventPayload"
	count := 0
	if p.ExecutionCreated != nil {
		count++
	}
	if p.PlanApproved != nil {
		count++
	}
	if p.ActionStarting != nil {
		count++
	}
	if p.ActionProcessCompleted != nil {
		count++
	}
	if p.PostconditionVerified != nil {
		count++
	}
	if p.ActionTerminated != nil {
		count++
	}
	if p.ExecutionFinished != nil {
		count++
	}
	if count != 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: exactly one payload must be set, got %d", kind, count)
	}

	switch expectedType {
	case EventExecutionCreated:
		if p.ExecutionCreated == nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: execution_created payload required for event type %q", kind, expectedType)
		}
		return p.ExecutionCreated.Validate()
	case EventPlanApproved:
		if p.PlanApproved == nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: plan_approved payload required for event type %q", kind, expectedType)
		}
		return p.PlanApproved.Validate()
	case EventActionStarting:
		if p.ActionStarting == nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: action_starting payload required for event type %q", kind, expectedType)
		}
		return p.ActionStarting.Validate()
	case EventActionProcessCompleted:
		if p.ActionProcessCompleted == nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: action_process_completed payload required for event type %q", kind, expectedType)
		}
		return p.ActionProcessCompleted.Validate()
	case EventPostconditionVerified:
		if p.PostconditionVerified == nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: postcondition_verified payload required for event type %q", kind, expectedType)
		}
		return p.PostconditionVerified.Validate()
	case EventActionTerminated:
		if p.ActionTerminated == nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: action_terminated payload required for event type %q", kind, expectedType)
		}
		return p.ActionTerminated.Validate()
	case EventExecutionFinished:
		if p.ExecutionFinished == nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: execution_finished payload required for event type %q", kind, expectedType)
		}
		return p.ExecutionFinished.Validate()
	default:
		return errs.New(errs.CategoryInvalidArgument, "%s: unhandled event type %q", kind, expectedType)
	}
}

type SetupLedgerEvent struct {
	SchemaVersion       SchemaVersion  `json:"schema_version"`
	Sequence            uint64         `json:"sequence"`
	EventID             string         `json:"event_id"`
	ExecutionID         string         `json:"execution_id"`
	PlanID              string         `json:"plan_id"`
	PlanDigest          string         `json:"plan_digest"`
	ActionID            string         `json:"action_id,omitempty"`
	PreviousEventDigest string         `json:"previous_event_digest"`
	EventDigest         string         `json:"event_digest"`
	Timestamp           Timestamp      `json:"timestamp"`
	Type                SetupEventType `json:"type"`
	Payload             EventPayload   `json:"payload"`
}

func (e *SetupLedgerEvent) RecordKind() string         { return "SetupLedgerEvent" }
func (e *SetupLedgerEvent) RecordID() string           { return e.EventID }
func (e *SetupLedgerEvent) SchemaVer() SchemaVersion   { return e.SchemaVersion }

func (e *SetupLedgerEvent) Validate() error {
	const kind = "SetupLedgerEvent"
	if err := e.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if e.Sequence == 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: sequence must be >= 1", kind)
	}
	if e.EventID == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: event_id is required", kind)
	}
	if e.ExecutionID == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: execution_id is required", kind)
	}
	if e.PlanID == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: plan_id is required", kind)
	}
	if !hexSha256Regex.MatchString(e.PlanDigest) {
		return errs.New(errs.CategoryInvalidArgument, "%s: plan_digest must be sha256 hex, got %q", kind, e.PlanDigest)
	}
	if e.Sequence == 1 {
		if e.PreviousEventDigest != "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: sequence 1 must have empty previous_event_digest, got %q", kind, e.PreviousEventDigest)
		}
	} else {
		if !hexSha256Regex.MatchString(e.PreviousEventDigest) {
			return errs.New(errs.CategoryInvalidArgument, "%s: sequence > 1 must have sha256 previous_event_digest, got %q", kind, e.PreviousEventDigest)
		}
	}
	if e.Timestamp.Time().IsZero() {
		return errs.New(errs.CategoryInvalidArgument, "%s: timestamp is required", kind)
	}
	if !e.Type.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid type %q", kind, string(e.Type))
	}
	if err := e.Payload.Validate(e.Type); err != nil {
		return err
	}

	if e.EventDigest != "" {
		expectedDigest, err := ComputeLedgerEventDigest(e)
		if err != nil {
			return err
		}
		if e.EventDigest != expectedDigest {
			return errs.New(errs.CategoryInvalidArgument, "%s: event_digest mismatch: got %q, expected %q", kind, e.EventDigest, expectedDigest)
		}
	}
	return nil
}

// ComputeLedgerEventDigest calculates the SHA-256 digest of the canonical JSON encoding
// of the complete event with only event_digest omitted (ADR-0014).
func ComputeLedgerEventDigest(e *SetupLedgerEvent) (string, error) {
	clone := *e
	clone.EventDigest = ""
	canonical, err := CanonicalJSON(&clone)
	if err != nil {
		return "", err
	}
	return DigestBytes(canonical), nil
}
