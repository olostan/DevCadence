package validation

import (
	"context"
	"strings"
	"time"

	"github.com/olostan/DevCadience/internal/controlplane"
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/events"
	"github.com/olostan/DevCadience/internal/ids"
	"github.com/olostan/DevCadience/internal/process"
	"github.com/olostan/DevCadience/internal/protocol"
)

// headCommitTimeout bounds the `git rev-parse HEAD` check ExecuteAndRecord
// runs before trusting a caller-claimed commit.
const headCommitTimeout = 30 * time.Second

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
	if in.Run.Dir == "" {
		return ExecuteResult{}, errs.New(errs.CategoryInvalidArgument, "validation: dir is required")
	}
	in.Run.ProjectID = in.ProjectID

	// A caller-supplied Commit is a claim, not a fact: nothing upstream of
	// this call verifies that in.Run.Dir's actual Git state matches it.
	// Without checking, a ValidationResult could be persisted claiming
	// validation of a commit the checks never actually ran against (a stale
	// worktree, a caller's typo, or a deliberately mismatched claim), which
	// is exactly the kind of unverified "tests pass" claim
	// docs/IMPLEMENTATION_PLAN.md M2 and AGENTS.md §8 rule out. Binding
	// execution to the real repository state means resolving in.Run.Dir's
	// actual HEAD before checks run and refusing to proceed if it disagrees
	// with in.Commit.
	actualHead, err := headCommit(ctx, in.Run)
	if err != nil {
		return ExecuteResult{}, err
	}
	if actualHead != in.Commit {
		return ExecuteResult{}, errs.New(errs.CategoryIntegrity,
			"validation: claimed commit %s does not match %s's actual HEAD %s",
			in.Commit, in.Run.Dir, actualHead)
	}

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

// headCommit resolves the actual Git HEAD of run.Dir, using the same runner
// and environment RunProfile would use for that RunOptions.
func headCommit(ctx context.Context, run RunOptions) (string, error) {
	runner := run.Runner
	if runner == nil {
		runner = process.NewRunner()
	}
	env := run.Env
	if env == nil {
		env = process.BaseEnv()
	}
	res, err := runner.Run(ctx, process.Spec{
		Executable: "git",
		Args:       []string{"-C", run.Dir, "rev-parse", "HEAD"},
		Dir:        run.Dir,
		Env:        env,
		Timeout:    headCommitTimeout,
	})
	if err != nil {
		return "", errs.Wrap(errs.CategoryInternal, err, "validation: resolve HEAD in %s", run.Dir)
	}
	if !res.Success() {
		return "", errs.New(errs.CategoryInvalidArgument,
			"validation: resolve HEAD in %s: %s", run.Dir, strings.TrimSpace(string(res.Stderr)))
	}
	return strings.TrimSpace(string(res.Stdout)), nil
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
