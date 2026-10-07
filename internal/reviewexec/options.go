package reviewexec

import (
	"log/slog"
	"strings"

	"github.com/olostan/DevCadence/internal/actors"
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

// Options configures the ReviewExecutor.
type Options struct {
	ProjectID         string
	ControlPlane      *controlplane.Service
	Worktrees         *worktrees.Manager
	Repositories      execrt.RepositoryProvider
	Runner            *process.Runner
	Registry          *facade.OperationRegistry
	Policy            execpolicy.PolicySource
	Resolver          execpolicy.EndpointResolver
	Drivers           execpolicy.DriverFactory
	Compiler          *compiler.Compiler
	Artifacts         execrt.ArtifactSink
	StateDir          string
	IndependenceBasis string // "endpoint_model" or "model_family_account"
	Clock             clock.Clock
	IDs               ids.Source
	Logger            *slog.Logger
}

// Validate checks that all required constructor options are present and valid.
func (o *Options) Validate() error {
	const kind = "reviewexec.Options"
	if strings.TrimSpace(o.ProjectID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: ProjectID is required", kind)
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
	if o.Drivers == nil {
		return errs.New(errs.CategoryInvalidArgument, "%s: Drivers is required", kind)
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
	if o.IndependenceBasis != actors.BasisEndpointModel && o.IndependenceBasis != actors.BasisModelFamilyAccount {
		return errs.New(errs.CategoryInvalidArgument, "%s: IndependenceBasis must be %q or %q, got %q",
			kind, actors.BasisEndpointModel, actors.BasisModelFamilyAccount, o.IndependenceBasis)
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
