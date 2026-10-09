package selfhost

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/cognition/sessionclients"
	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/execpolicy"
	"github.com/olostan/DevCadence/internal/execrt"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/principal/facade"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/repository"
	"github.com/olostan/DevCadence/internal/taskexec"
	"github.com/olostan/DevCadence/internal/worktrees"
)

// Deps are the process-level collaborators Build composes around the Config.
type Deps struct {
	ProjectID    string
	RepoPath     string // registered source repository (from project state, not the config)
	StateDir     string // user-level state dir (locks, worktrees, artifacts)
	ControlPlane *controlplane.Service
	Registry     *facade.OperationRegistry
	Clock        clock.Clock // optional
	IDs          ids.Source  // optional
	Logger       *slog.Logger
}

// Built is the composed executor plus what was observed at preflight.
type Built struct {
	Executor *taskexec.Executor
	Identity Identity
	// Close releases the project execution lock.
	Close func() error
}

// Build probes the configured Ollama, then composes the native task executor
// around the real sessionclients composition. It performs no model call.
func Build(ctx context.Context, cfg Config, deps Deps) (*Built, error) {
	if err := cfg.normalize(); err != nil {
		return nil, err
	}
	if deps.ProjectID == "" || deps.RepoPath == "" || deps.StateDir == "" ||
		deps.ControlPlane == nil || deps.Registry == nil || deps.Logger == nil {
		return nil, errs.New(errs.CategoryInvalidArgument,
			"selfhost: ProjectID, RepoPath, StateDir, ControlPlane, Registry and Logger are required")
	}
	identity, err := Probe(ctx, cfg)
	if err != nil {
		return nil, err
	}
	clk := deps.Clock
	if clk == nil {
		clk = clock.System()
	}
	policy, err := newOwnerLocalPolicy(cfg, time.Now())
	if err != nil {
		return nil, err
	}

	chanID := "chan-" + cfg.EndpointID
	profID := "prof-" + cfg.EndpointID
	portfolio := &protocol.CognitionPortfolio{
		SchemaVersion: protocol.SchemaVersion1,
		PortfolioID:   "port-" + cfg.EndpointID,
		Revision:      1,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		Channels: []protocol.AccessChannel{{
			SchemaVersion:         protocol.SchemaVersion1,
			ChannelID:             chanID,
			EndpointID:            cfg.EndpointID,
			Kind:                  protocol.ChannelDirectHTTPAPI,
			SessionMode:           protocol.SessionStatelessPerCall,
			ContextControl:        protocol.ContextControlExactStateless,
			PrefixCache:           protocol.PrefixCacheExplicit,
			MaxConcurrentRequests: 1,
		}},
		RoleBindings: []protocol.RoleBinding{{
			Role: "implementer", EndpointID: cfg.EndpointID, ChannelID: chanID,
			BudgetPoolID: "pool-default", ContextProfileID: profID, Priority: 1,
		}},
		BudgetPools: []protocol.BudgetPool{{
			SchemaVersion: protocol.SchemaVersion1, PoolID: "pool-default", Name: "Local compute",
			Regime: protocol.RegimeLocalCompute, Unit: protocol.UnitSeconds,
			HardLimit: 1000000, SoftAlertLimit: 800000, Period: protocol.PeriodPerTask,
		}},
		MaxSourceExposure: protocol.ExposureToolMediatedWorktree,
	}
	prof := compiler.MustDefaultProvisionalProfile(cfg.EndpointID, chanID, cfg.Model, cfg.ContextTokens)
	prof.ProfileID = profID
	endpoint := protocol.CognitionEndpoint{
		ID: cfg.EndpointID, Kind: protocol.EndpointLocalRuntime, Provider: "ollama", Runtime: "ollama",
		ModelID: cfg.Model, ModelFamily: modelFamily(cfg.Model), AccountRef: "local",
		Locality: protocol.LocalityLocal,
	}
	resolver := execpolicy.NewPortfolioEndpointResolver(portfolio).
		WithEndpoints(endpoint).WithContextProfiles(*prof)

	drivers, err := sessionclients.New(sessionclients.Options{
		LoopbackBaseURLs: map[string]string{cfg.EndpointID: cfg.OllamaURL},
		Clock:            clk,
	})
	if err != nil {
		return nil, err
	}
	rules, err := compiler.NewCanonicalRuleRegistry()
	if err != nil {
		return nil, err
	}
	comp, err := compiler.NewCompiler(rules, nil, nil)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(deps.StateDir, 0o700); err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "selfhost: cannot create state dir %s", deps.StateDir)
	}
	runner := process.NewRunner()
	repo, err := repository.Register(ctx, deps.ProjectID, deps.RepoPath, repository.Options{Runner: runner})
	if err != nil {
		return nil, err
	}
	lock, err := execrt.AcquireProjectLock(ctx, deps.StateDir, deps.ProjectID)
	if err != nil {
		return nil, err
	}
	wt, err := worktrees.NewManager(filepath.Join(deps.StateDir, "worktrees"), runner)
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	exec, err := taskexec.New(taskexec.Options{
		ProjectID:    deps.ProjectID,
		Lock:         lock,
		ControlPlane: deps.ControlPlane,
		Worktrees:    wt,
		Repositories: execrt.NewSingleRepositoryProvider(repo),
		Runner:       runner,
		Registry:     deps.Registry,
		Policy:       policy,
		Resolver:     resolver,
		Drivers:      drivers,
		Compiler:     comp,
		Artifacts:    execrt.NewDirectoryArtifactSink(filepath.Join(deps.StateDir, "artifacts")),
		StateDir:     deps.StateDir,
		Clock:        clk,
		IDs:          deps.IDs,
		Logger:       deps.Logger,
	})
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	deps.Logger.Warn("selfhost executor enabled with owner-local unsigned execution policy (no activation receipt)",
		slog.String("policy_provenance", PolicyProvenance),
		slog.String("endpoint_id", cfg.EndpointID),
		slog.String("model", cfg.Model),
		slog.String("model_digest", identity.Digest),
		slog.String("ollama_version", identity.Version))
	return &Built{Executor: exec, Identity: identity, Close: lock.Close}, nil
}

// modelFamily is the model name without its tag ("qwen2.5-coder:7b" -> "qwen2.5-coder").
func modelFamily(model string) string {
	for i := 0; i < len(model); i++ {
		if model[i] == ':' {
			return model[:i]
		}
	}
	return model
}
