package taskexec

import (
	"log/slog"
	"strings"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/execpolicy"
	"github.com/olostan/DevCadence/internal/execrt"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/observability"
	"github.com/olostan/DevCadence/internal/principal/facade"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/worktrees"
)

// Options configures the TaskExecutor.
type Options struct {
	ProjectID    string
	Lock         *execrt.ProjectLock
	ControlPlane *controlplane.Service
	Worktrees    *worktrees.Manager
	Repositories execrt.RepositoryProvider
	Runner       *process.Runner
	Registry     *facade.OperationRegistry
	Policy       execpolicy.PolicySource
	Resolver     execpolicy.EndpointResolver
	Drivers      execpolicy.DriverFactory
	Compiler     *compiler.Compiler
	Artifacts    execrt.ArtifactSink
	StateDir     string
	Clock        clock.Clock
	IDs          ids.Source
	Logger       *slog.Logger
	Profiles     ProfileSource // defined in validate.go for WP-9
	// ExecutionMode gates run_command; empty means ExecutionStrict.
	ExecutionMode ExecutionMode
}

// Validate checks that all required constructor options are present.
func (o *Options) Validate() error {
	const kind = "taskexec.Options"
	if strings.TrimSpace(o.ProjectID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: ProjectID is required", kind)
	}
	if o.Lock == nil {
		return errs.New(errs.CategoryInvalidArgument, "%s: Lock is required", kind)
	}
	if o.ControlPlane == nil {
		return errs.New(errs.CategoryInvalidArgument, "%s: ControlPlane is required", kind)
	}
	if o.Worktrees == nil {
		return errs.New(errs.CategoryInvalidArgument, "%s: Worktrees is required", kind)
	}
	if o.Repositories == nil {
		return errs.New(errs.CategoryInvalidArgument, "%s: Repositories is required", kind)
	}
	if o.Runner == nil {
		return errs.New(errs.CategoryInvalidArgument, "%s: Runner is required", kind)
	}
	if o.Registry == nil {
		return errs.New(errs.CategoryInvalidArgument, "%s: Registry is required", kind)
	}
	if o.Policy == nil {
		return errs.New(errs.CategoryInvalidArgument, "%s: Policy is required", kind)
	}
	if o.Resolver == nil {
		return errs.New(errs.CategoryInvalidArgument, "%s: Resolver is required", kind)
	}
	if o.Compiler == nil {
		return errs.New(errs.CategoryInvalidArgument, "%s: Compiler is required", kind)
	}
	if o.Artifacts == nil {
		return errs.New(errs.CategoryInvalidArgument, "%s: Artifacts is required", kind)
	}
	if strings.TrimSpace(o.StateDir) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: StateDir is required", kind)
	}
	switch o.ExecutionMode.normalized() {
	case ExecutionStrict, ExecutionUnsafeUnconfinedLocal:
	default:
		return errs.New(errs.CategoryInvalidArgument, "%s: unknown ExecutionMode %q", kind, o.ExecutionMode)
	}
	if o.Clock == nil {
		o.Clock = clock.System()
	}
	if o.IDs == nil {
		o.IDs = ids.NewULIDSource()
	}
	if o.Logger == nil {
		o.Logger = observability.NewLogger(observability.Options{})
	}
	return nil
}

// ExecutionMode selects whether workers may run commands in their candidate
// worktree. The zero value is strict.
type ExecutionMode string

const (
	// ExecutionStrict (default): workers get file tools only; run_command is not
	// declared to the model and any call is refused before a process is created.
	ExecutionStrict ExecutionMode = "strict"
	// ExecutionUnsafeUnconfinedLocal: workers may run argv commands as the
	// invoking OS user with a minimal environment. This is NOT a sandbox; it is
	// an owner-local convenience enabled only by explicit user-level
	// configuration, and every result and audit record carries an
	// unsafe_unconfined marker.
	ExecutionUnsafeUnconfinedLocal ExecutionMode = "unsafe_unconfined_local"
)

func (m ExecutionMode) normalized() ExecutionMode {
	if m == "" {
		return ExecutionStrict
	}
	return m
}
