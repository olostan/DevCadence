package validation

import (
	"context"

	"github.com/olostan/DevCadience/internal/controlplane"
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/events"
	"github.com/olostan/DevCadience/internal/ids"
	"github.com/olostan/DevCadience/internal/protocol"
)

// ExecuteInput describes one profile execution that should become durable
// evidence.
type ExecuteInput struct {
	ProjectID string
	Subject   protocol.ValidationSubject
	// Commit is the tree the run validated (docs/PROTOCOLS.md §10: every
	// scope names the commit it validated).
	Commit  string
	Profile Profile
	Run     RunOptions
	Actor   protocol.Actor
	IDs     ids.Source
}

// ExecuteResult is what a recorded execution produced.
type ExecuteResult struct {
	ValidationResult protocol.ValidationResult
	CommandResult    controlplane.Result
}

// ExecuteAndRecord runs a profile, builds the real M1
// protocol.ValidationResult from its output, and appends a matching
// ValidationCompleted event with that record stored in the same
// control-plane transaction.
//
// The flow is exactly docs/IMPLEMENTATION_PLAN.md M2 §14's contract: execute
// deterministic checks -> produce ValidationResult -> persist record -> emit
// ValidationCompleted with a matching digest -> atomic control-plane
// transaction. Because Service.Apply (internal/controlplane/service.go)
// writes the record and appends the event in one SQLite transaction, and
// ValidationCompleted implements events.RecordReferencing so its digest is
// cross-checked against the stored record before either commits, a process
// that succeeds but whose evidence fails to persist can never leave a
// ValidationCompleted event with no backing record, and an event citing the
// wrong digest is refused by the control plane before anything is written —
// the process outcome and the durable claim about it either land together or
// not at all.
func ExecuteAndRecord(ctx context.Context, svc *controlplane.Service, in ExecuteInput) (ExecuteResult, error) {
	if svc == nil {
		return ExecuteResult{}, errs.New(errs.CategoryInvalidArgument, "validation: control-plane service is required")
	}
	if in.ProjectID == "" {
		return ExecuteResult{}, errs.New(errs.CategoryInvalidArgument, "validation: project id is required")
	}
	if in.Commit == "" {
		return ExecuteResult{}, errs.New(errs.CategoryInvalidArgument, "validation: commit is required")
	}
	if err := in.Subject.Validate("ExecuteAndRecord"); err != nil {
		return ExecuteResult{}, err
	}
	idSource := in.IDs
	if idSource == nil {
		idSource = ids.NewULIDSource()
	}
	in.Run.ProjectID = in.ProjectID

	checks, outcome, err := RunProfile(ctx, in.Profile, in.Run)
	if err != nil {
		return ExecuteResult{}, err
	}

	result := protocol.ValidationResult{
		SchemaVersion: protocol.SchemaVersion1,
		ValidationID:  idSource.New("val"),
		ProjectID:     in.ProjectID,
		Subject:       in.Subject,
		Commit:        in.Commit,
		Status:        outcome,
		Checks:        checks,
	}
	if err := result.Validate(); err != nil {
		return ExecuteResult{}, err
	}
	digest, err := protocol.Digest(&result)
	if err != nil {
		return ExecuteResult{}, err
	}

	var failed []string
	for _, c := range result.Checks {
		if c.Status != protocol.CheckPass && c.Status != protocol.CheckSkipped {
			failed = append(failed, c.ID)
		}
	}

	payload := &events.ValidationCompleted{
		ValidationID: result.ValidationID,
		Scope:        in.Subject.Kind,
		Status:       outcome,
		Commit:       in.Commit,
		RecordDigest: digest,
		FailedChecks: failed,
	}
	if in.Subject.Kind == protocol.ScopeAttempt || in.Subject.Kind == protocol.ScopeIntegration {
		payload.TaskID = in.Subject.TaskID
	}
	if in.Subject.Kind == protocol.ScopeAttempt {
		payload.AttemptID = in.Subject.AttemptID
	}

	cmdResult, err := svc.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: in.ProjectID,
		Payload:   payload,
		Records:   []controlplane.RecordToStore{{Version: 1, Record: &result}},
		Actor:     defaultActor(in.Actor),
	})
	if err != nil {
		return ExecuteResult{}, err
	}
	return ExecuteResult{ValidationResult: result, CommandResult: cmdResult}, nil
}

func defaultActor(a protocol.Actor) protocol.Actor {
	if a.Kind == "" {
		a.Kind = protocol.ActorTool
	}
	if a.ID == "" {
		a.ID = "devcadience-validation"
	}
	return a
}
