package taskexec_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/execpolicy"
	"github.com/olostan/DevCadence/internal/execrt"
	"github.com/olostan/DevCadence/internal/observability"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/principal/facade"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/repository"
	"github.com/olostan/DevCadence/internal/storage"
	"github.com/olostan/DevCadence/internal/taskexec"
	"github.com/olostan/DevCadence/internal/tasks"
	"github.com/olostan/DevCadence/internal/testsupport"
	"github.com/olostan/DevCadence/internal/tools"
	"github.com/olostan/DevCadence/internal/worktrees"
)

var attemptFailedGrammar = regexp.MustCompile(
	`^reason=(worktree_unavailable|endpoint_unavailable|limit_reached|scope_violation|candidate_too_large|no_change|driver_error|cancelled|executor_lost|operation_registry_full)( class=(scope|symlink|submodule|git_dir|count|size))? effects=(none|uncertain)$`,
)

type fakePolicySource struct {
	policy execpolicy.ExecutionPolicy
	digest string
	err    error
}

func (f *fakePolicySource) Current(ctx context.Context) (execpolicy.ExecutionPolicy, string, error) {
	if f.err != nil {
		return execpolicy.ExecutionPolicy{}, "", f.err
	}
	return f.policy, f.digest, nil
}

type fakeDriverFactory struct {
	driver drivers.SessionDriver
	obs    execpolicy.EndpointObservation
	err    error
}

func (f *fakeDriverFactory) Open(ctx context.Context, ep execpolicy.ResolvedEndpoint) (execpolicy.OpenedEndpoint, error) {
	if f.err != nil {
		return execpolicy.OpenedEndpoint{}, f.err
	}
	return execpolicy.OpenedEndpoint{
		Observed: f.obs,
		Driver:   f.driver,
	}, nil
}

func readAllEvents(t *testing.T, store *storage.Store, projectID string) []events.Event {
	t.Helper()
	var evs []events.Event
	err := store.Read(context.Background(), func(tx *storage.Tx) error {
		var err error
		evs, err = tx.ReadEvents(context.Background(), storage.EventQuery{ProjectID: projectID})
		return err
	})
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	return evs
}

type testFixture struct {
	projectID string
	harness   *testsupport.Harness
	gitRepo   *testsupport.GitRepo
	repo      *repository.Repository
	repos     *execrt.SingleRepositoryProvider
	lock      *execrt.ProjectLock
	worktrees *worktrees.Manager
	runner    *process.Runner
	registry  *facade.OperationRegistry
	policy    execpolicy.ExecutionPolicy
	policySrc *fakePolicySource
	resolver  *execpolicy.PortfolioEndpointResolver
	factory   *fakeDriverFactory
	driver    *drivers.FakeDriver
	compiler  *compiler.Compiler
	artifacts *execrt.MemoryArtifactSink
	stateDir  string
	opts      taskexec.Options
}

func setupFixture(t *testing.T, projectID string) *testFixture {
	t.Helper()
	harness := testsupport.NewHarness(t)
	gitRepo := testsupport.NewGitRepo(t)
	gitRepo.WriteFile("base.txt", "initial base content\n")
	gitRepo.Commit("initial commit")

	runner := process.NewRunner()
	repo, err := repository.Register(context.Background(), projectID, gitRepo.Path, repository.Options{Runner: runner})
	if err != nil {
		t.Fatalf("register repo: %v", err)
	}
	repos := execrt.NewSingleRepositoryProvider(repo)

	stateDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval stateDir: %v", err)
	}

	lock, err := execrt.AcquireProjectLock(context.Background(), stateDir, projectID)
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	t.Cleanup(func() { _ = lock.Close() })

	wtManager, err := worktrees.NewManager(filepath.Join(stateDir, "worktrees"), runner)
	if err != nil {
		t.Fatalf("new worktree manager: %v", err)
	}

	registry, err := facade.NewOperationRegistry("test-inst")
	if err != nil {
		t.Fatalf("new operation registry: %v", err)
	}
	t.Cleanup(func() { registry.Close() })

	now := time.Now().UTC()
	policy := execpolicy.ExecutionPolicy{
		Version:            "1.0",
		PolicyID:           "test-pol",
		Revision:           1,
		NotBefore:          now.Add(-1 * time.Hour).Format(time.RFC3339),
		NotAfter:           now.Add(24 * time.Hour).Format(time.RFC3339),
		MaxAttemptsPerTask: 3,
		Grants: []execpolicy.EndpointGrant{
			{
				EndpointID:     "ep-test",
				ModelID:        "model-test",
				Roles:          []string{"implementer"},
				Locality:       protocol.LocalityLocal,
				SourceExposure: protocol.ExposureToolMediatedWorktree,
				ChannelKind:    protocol.ChannelCLISubprocess,
				Limits: execpolicy.ExecutionLimits{
					MaxTurns:               10,
					MaxToolCalls:           20,
					MaxTotalTokens:         100000,
					MaxDurationSeconds:     300,
					MaxOutputTokensPerCall: 4096,
					MaxRequestBytes:        65536,
					MaxAPISpendMicroUSD:    0,
					AllowUnknownUsage:      true,
				},
			},
		},
	}
	policyDigest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	policySrc := &fakePolicySource{policy: policy, digest: policyDigest}

	portfolio := &protocol.CognitionPortfolio{
		SchemaVersion: protocol.SchemaVersion1,
		PortfolioID:   "port-test",
		Revision:      1,
		CreatedAt:     now.Format(time.RFC3339),
		Channels: []protocol.AccessChannel{
			{
				SchemaVersion:         protocol.SchemaVersion1,
				ChannelID:             "chan-ep-test",
				EndpointID:            "ep-test",
				Kind:                  protocol.ChannelCLISubprocess,
				SessionMode:           protocol.SessionStatelessPerCall,
				ContextControl:        protocol.ContextControlExactStateless,
				PrefixCache:           protocol.PrefixCacheNone,
				MaxConcurrentRequests: 2,
			},
		},
		RoleBindings: []protocol.RoleBinding{
			{
				Role:             "implementer",
				EndpointID:       "ep-test",
				ChannelID:        "chan-ep-test",
				BudgetPoolID:     "pool-default",
				ContextProfileID: "prof-ep-test",
				Priority:         1,
			},
		},
		BudgetPools: []protocol.BudgetPool{
			{
				SchemaVersion:  protocol.SchemaVersion1,
				PoolID:         "pool-default",
				Name:           "Default Pool",
				Regime:         protocol.RegimeLocalCompute,
				Unit:           protocol.UnitSeconds,
				HardLimit:      1000,
				SoftAlertLimit: 800,
				Period:         protocol.PeriodPerTask,
			},
		},
		MaxSourceExposure: protocol.ExposureToolMediatedWorktree,
	}

	prof := compiler.MustDefaultProvisionalProfile("ep-test", "chan-ep-test", "model-test", 200000)
	prof.ProfileID = "prof-ep-test"
	resolver := execpolicy.NewPortfolioEndpointResolver(portfolio).WithContextProfiles(*prof)

	driver := drivers.NewFakeDriver("test-driver-id")
	factory := &fakeDriverFactory{
		driver: driver,
		obs: execpolicy.EndpointObservation{
			ModelRevision:  "v1.0.0",
			RuntimeVersion: "1.0.0",
			DriverID:       "test-driver-id",
		},
	}

	ruleReg, err := compiler.NewCanonicalRuleRegistry()
	if err != nil {
		t.Fatalf("new canonical rule registry: %v", err)
	}
	comp, err := compiler.NewCompiler(ruleReg, nil, nil)
	if err != nil {
		t.Fatalf("new compiler: %v", err)
	}

	artifacts := execrt.NewMemoryArtifactSink()

	opts := taskexec.Options{
		ProjectID:    projectID,
		Lock:         lock,
		ControlPlane: harness.Service,
		Worktrees:    wtManager,
		Repositories: repos,
		Runner:       runner,
		Registry:     registry,
		Policy:       policySrc,
		Resolver:     resolver,
		Drivers:      factory,
		Compiler:     comp,
		Artifacts:    artifacts,
		StateDir:     stateDir,
		Clock:        harness.Clock,
		IDs:          harness.IDs,
		Logger:       observability.NewLogger(observability.Options{}),
	}

	return &testFixture{
		projectID: projectID,
		harness:   harness,
		gitRepo:   gitRepo,
		repo:      repo,
		repos:     repos,
		lock:      lock,
		worktrees: wtManager,
		runner:    runner,
		registry:  registry,
		policy:    policy,
		policySrc: policySrc,
		resolver:  resolver,
		factory:   factory,
		driver:    driver,
		compiler:  comp,
		artifacts: artifacts,
		stateDir:  stateDir,
		opts:      opts,
	}
}

func initProjectAndApproveWP(t *testing.T, f *testFixture, taskAlias, wpID string, inScope []string) (string, *protocol.EngineeringWorkPackage, string) {
	t.Helper()
	ctx := context.Background()
	_, err := f.harness.Service.InitProject(ctx, controlplane.InitProjectInput{
		ProjectID:      f.projectID,
		Name:           "Test Project",
		MilestoneID:    "M1",
		MilestoneTitle: "Milestone 1",
	})
	if err != nil {
		t.Fatalf("init project: %v", err)
	}

	_, err = f.harness.Service.CreateTask(ctx, controlplane.CreateTaskInput{
		ProjectID:   f.projectID,
		Alias:       taskAlias,
		Title:       "Task " + taskAlias,
		ChangeClass: protocol.ChangeLocal,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	taskID, err := f.harness.Service.ResolveTaskID(ctx, f.projectID, taskAlias)
	if err != nil {
		t.Fatalf("resolve task ID: %v", err)
	}

	_, err = f.harness.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: f.projectID,
		Payload: &events.TaskDesignStarted{
			TaskID: taskID,
			Reason: "initial design",
		},
	})
	if err != nil {
		t.Fatalf("design started: %v", err)
	}

	ps, err := f.harness.Service.ProjectState(ctx, f.projectID)
	if err != nil {
		t.Fatalf("project state: %v", err)
	}

	baseCommit := f.gitRepo.Head()
	wp := &protocol.EngineeringWorkPackage{
		SchemaVersion:        protocol.SchemaVersion1,
		WorkPackageID:        wpID,
		TaskID:               taskID,
		Version:              1,
		ProjectID:            f.projectID,
		ProjectStateRevision: ps.StateRevision,
		BaseCommit:           baseCommit,
		ChangeClass:          protocol.ChangeLocal,
		Objective:            "Implement " + taskAlias,
		Rationale:            "Rationale for " + taskAlias,
		ArchitecturalIntent:  "Intent for " + taskAlias,
		Scope: protocol.Scope{
			InScope:    inScope,
			OutOfScope: []string{"unrelated"},
		},
		Guidance: []protocol.Guidance{
			{ID: "G1", Strength: protocol.GuidanceMust, Statement: "Modify only declared in_scope"},
		},
		AcceptanceCriteria:     []string{"Change implemented"},
		ValidationRequirements: []string{"deterministic validation"},
		EscalationConditions:   []string{"Uncertainty"},
	}

	wpDigest, err := protocol.Digest(wp)
	if err != nil {
		t.Fatalf("digest wp: %v", err)
	}

	_, err = f.harness.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: f.projectID,
		Payload: &events.WorkPackageApproved{
			TaskID:               taskID,
			WorkPackageID:        wpID,
			WorkPackageVersion:   1,
			RecordDigest:         wpDigest,
			ProjectStateRevision: ps.StateRevision,
			BaseCommit:           baseCommit,
			ChangeClass:          protocol.ChangeLocal,
		},
		Records: []controlplane.RecordToStore{
			{Version: 1, Record: wp},
		},
	})
	if err != nil {
		t.Fatalf("approve wp: %v", err)
	}

	return taskID, wp, wpDigest
}

func TestOptions_Validate(t *testing.T) {
	f := setupFixture(t, "proj-opts")
	opts := f.opts

	if err := opts.Validate(); err != nil {
		t.Fatalf("expected valid options, got: %v", err)
	}

	t.Run("missing project_id", func(t *testing.T) {
		o := opts
		o.ProjectID = ""
		if err := o.Validate(); err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got: %v", err)
		}
	})

	t.Run("missing lock", func(t *testing.T) {
		o := opts
		o.Lock = nil
		if err := o.Validate(); err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got: %v", err)
		}
	})

	t.Run("missing controlplane", func(t *testing.T) {
		o := opts
		o.ControlPlane = nil
		if err := o.Validate(); err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got: %v", err)
		}
	})

	t.Run("missing worktrees", func(t *testing.T) {
		o := opts
		o.Worktrees = nil
		if err := o.Validate(); err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got: %v", err)
		}
	})

	t.Run("missing repositories", func(t *testing.T) {
		o := opts
		o.Repositories = nil
		if err := o.Validate(); err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got: %v", err)
		}
	})

	t.Run("missing runner", func(t *testing.T) {
		o := opts
		o.Runner = nil
		if err := o.Validate(); err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got: %v", err)
		}
	})

	t.Run("missing registry", func(t *testing.T) {
		o := opts
		o.Registry = nil
		if err := o.Validate(); err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got: %v", err)
		}
	})

	t.Run("missing policy", func(t *testing.T) {
		o := opts
		o.Policy = nil
		if err := o.Validate(); err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got: %v", err)
		}
	})

	t.Run("missing resolver", func(t *testing.T) {
		o := opts
		o.Resolver = nil
		if err := o.Validate(); err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got: %v", err)
		}
	})

	t.Run("missing compiler", func(t *testing.T) {
		o := opts
		o.Compiler = nil
		if err := o.Validate(); err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got: %v", err)
		}
	})

	t.Run("missing artifacts", func(t *testing.T) {
		o := opts
		o.Artifacts = nil
		if err := o.Validate(); err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got: %v", err)
		}
	})

	t.Run("missing statedir", func(t *testing.T) {
		o := opts
		o.StateDir = ""
		if err := o.Validate(); err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got: %v", err)
		}
	})

	t.Run("defaults applied for clock, ids, logger", func(t *testing.T) {
		o := opts
		o.Clock = nil
		o.IDs = nil
		o.Logger = nil
		if err := o.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if o.Clock == nil || o.IDs == nil || o.Logger == nil {
			t.Errorf("expected defaults to be set, got clock=%v, ids=%v, logger=%v", o.Clock, o.IDs, o.Logger)
		}
	})
}

func TestRecover_FailsOrphanedAttempts(t *testing.T) {
	f := setupFixture(t, "proj-recover")
	taskID, _, _ := initProjectAndApproveWP(t, f, "DC-001", "wp-001", []string{"base.txt"})

	ctx := context.Background()
	ps, err := f.harness.Service.ProjectState(ctx, f.projectID)
	if err != nil {
		t.Fatalf("project state: %v", err)
	}

	attemptID := "att_orphan_1"
	// Delegate & Start attempt directly to create a running attempt in the control plane
	_, err = f.harness.Service.ApplyBatch(ctx, controlplane.BatchCommand{
		ProjectID:             f.projectID,
		Actor:                 protocol.Actor{Kind: protocol.ActorControlPlane, ID: "test"},
		ExpectedStateRevision: ps.StateRevision,
		Commands: []controlplane.Command{
			{
				ProjectID: f.projectID,
				Actor:     protocol.Actor{Kind: protocol.ActorControlPlane, ID: "test"},
				Payload: &events.TaskDelegated{
					TaskID:        taskID,
					WorkPackageID: "wp-001",
					WorkerRole:    "implementer",
					MaxAttempts:   3,
				},
			},
			{
				ProjectID: f.projectID,
				Actor:     protocol.Actor{Kind: protocol.ActorControlPlane, ID: "test"},
				Payload: &events.AttemptStarted{
					TaskID:               taskID,
					AttemptID:            attemptID,
					WorkPackageID:        "wp-001",
					WorkPackageVersion:   1,
					ProjectStateRevision: ps.StateRevision,
					BaseCommit:           f.gitRepo.Head(),
					WorkerRole:           "implementer",
					ModelIdentity:        "ep-test/model-test@v1",
					WorktreeID:           attemptID,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("apply running attempt: %v", err)
	}

	// Verify task detail shows running attempt
	detailBefore, err := f.harness.Service.TaskDetail(ctx, f.projectID, "DC-001")
	if err != nil {
		t.Fatalf("task detail: %v", err)
	}
	if len(detailBefore.Attempts) != 1 || detailBefore.Attempts[0].Status != tasks.AttemptRunning {
		t.Fatalf("expected 1 running attempt, got %+v", detailBefore.Attempts)
	}

	// Constructing Executor via New automatically runs Recover
	executor, err := taskexec.New(f.opts)
	if err != nil {
		t.Fatalf("taskexec.New failed: %v", err)
	}
	_ = executor

	// Verify the attempt is now failed with executor_lost
	detailAfter, err := f.harness.Service.TaskDetail(ctx, f.projectID, "DC-001")
	if err != nil {
		t.Fatalf("task detail after recovery: %v", err)
	}
	if len(detailAfter.Attempts) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(detailAfter.Attempts))
	}
	att := detailAfter.Attempts[0]
	if att.Status != tasks.AttemptFailed {
		t.Errorf("expected attempt status Failed, got %s", att.Status)
	}
	if att.FailureSummary != "reason=executor_lost effects=uncertain" {
		t.Errorf("expected failure summary 'reason=executor_lost effects=uncertain', got %q", att.FailureSummary)
	}
	if !attemptFailedGrammar.MatchString(att.FailureSummary) {
		t.Errorf("summary %q does not match closed grammar", att.FailureSummary)
	}
}

func TestDelegate_HappyPath(t *testing.T) {
	f := setupFixture(t, "proj-happy")
	taskID, wp, wpDigest := initProjectAndApproveWP(t, f, "DC-001", "wp-001", []string{"base.txt", "added.txt"})

	// Configure FakeDriver scripted responses
	f.driver.SetTurnHandler("turn-1", func(ctx context.Context, input drivers.TurnInput) (drivers.TurnResult, error) {
		args, _ := json.Marshal(map[string]string{
			"path":    "base.txt",
			"content": "modified base content\n",
		})
		args2, _ := json.Marshal(map[string]string{
			"path":    "added.txt",
			"content": "new file content\n",
		})
		return drivers.TurnResult{
			TurnID:  "turn-1",
			Content: "Modifying base.txt and added.txt",
			ToolCalls: []drivers.ToolCall{
				{ID: "call-1", Name: "write_file", Arguments: args},
				{ID: "call-2", Name: "write_file", Arguments: args2},
			},
			Usage: drivers.KnownUsage(50, 0, 50),
		}, nil
	})

	f.driver.SetTurnHandler("turn-2", func(ctx context.Context, input drivers.TurnInput) (drivers.TurnResult, error) {
		return drivers.TurnResult{
			TurnID:    "turn-2",
			Content:   "Finished implementation cleanly",
			ToolCalls: nil,
			Usage:     drivers.KnownUsage(20, 0, 20),
		}, nil
	})

	exec, err := taskexec.New(f.opts)
	if err != nil {
		t.Fatalf("taskexec.New: %v", err)
	}

	authTask := facade.AuthorizedTask{
		Caller: principal.CallerContext{
			PrincipalID: "test-caller",
			ProjectID:   f.projectID,
			SourceDepth: "all",
		},
		Meta: principal.CallMeta{
			SchemaVersion: principal.SchemaVersion,
			ProjectID:     f.projectID,
			CorrelationID: "corr-1",
		},
		TaskID: taskID,
		WorkPackage: principal.WorkPackageRef{
			ID:         wp.WorkPackageID,
			Version:    wp.Version,
			Digest:     wpDigest,
			BaseCommit: wp.BaseCommit,
		},
	}

	opRef, err := exec.Delegate(context.Background(), authTask)
	if err != nil {
		t.Fatalf("Delegate failed: %v", err)
	}

	// Wait for operation to complete
	completedRef := f.registry.Wait(context.Background(), opRef, 5*time.Second)
	if completedRef.Status != principal.StatusCompleted {
		status, _ := f.registry.Lookup(f.projectID, opRef)
		t.Fatalf("operation status %s, failure: %+v", completedRef.Status, status.Error)
	}

	// Verify task detail & CandidateProduced event
	detail, err := f.harness.Service.TaskDetail(context.Background(), f.projectID, "DC-001")
	if err != nil {
		t.Fatalf("task detail: %v", err)
	}
	if len(detail.Attempts) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(detail.Attempts))
	}
	att := detail.Attempts[0]
	if att.Status != tasks.AttemptCandidateProduced {
		t.Fatalf("expected attempt status %s, got %s", tasks.AttemptCandidateProduced, att.Status)
	}
	if att.CandidateCommit == "" {
		t.Fatalf("expected non-empty candidate commit")
	}

	// Verify candidate commit parent matches base commit
	commitParent := f.gitRepo.Git("rev-parse", att.CandidateCommit+"^")
	if commitParent != wp.BaseCommit {
		t.Errorf("candidate parent = %s, want baseCommit %s", commitParent, wp.BaseCommit)
	}

	// Verify artifacts were stored
	eventsList := readAllEvents(t, f.harness.Store, f.projectID)
	var candidateEv *events.CandidateProduced
	for _, ev := range eventsList {
		if cp, ok := ev.Payload.(*events.CandidateProduced); ok {
			candidateEv = cp
			break
		}
	}
	if candidateEv == nil {
		t.Fatalf("CandidateProduced event not found")
	}
	if len(candidateEv.Artifacts) != 2 {
		t.Errorf("expected 2 artifacts (diff, usage), got %d", len(candidateEv.Artifacts))
	}
}

func TestDelegate_PreEffectPolicyDeniedOrBudgetExhausted(t *testing.T) {
	f := setupFixture(t, "proj-budget")
	taskID, wp, wpDigest := initProjectAndApproveWP(t, f, "DC-001", "wp-001", []string{"base.txt"})

	ctx := context.Background()
	ps, err := f.harness.Service.ProjectState(ctx, f.projectID)
	if err != nil {
		t.Fatalf("project state: %v", err)
	}

	// Exhaust budget: add 3 failed attempts
	for i := 1; i <= 3; i++ {
		attID := fmt.Sprintf("att_budget_%d", i)
		var cmds []controlplane.Command
		if i == 1 {
			cmds = append(cmds, controlplane.Command{
				ProjectID: f.projectID,
				Actor:     protocol.Actor{Kind: protocol.ActorControlPlane, ID: "test"},
				Payload: &events.TaskDelegated{
					TaskID:        taskID,
					WorkPackageID: wp.WorkPackageID,
					WorkerRole:    "implementer",
					MaxAttempts:   3,
				},
			})
		}
		cmds = append(cmds,
			controlplane.Command{
				ProjectID: f.projectID,
				Actor:     protocol.Actor{Kind: protocol.ActorControlPlane, ID: "test"},
				Payload: &events.AttemptStarted{
					TaskID:               taskID,
					AttemptID:            attID,
					WorkPackageID:        wp.WorkPackageID,
					WorkPackageVersion:   wp.Version,
					ProjectStateRevision: ps.StateRevision,
					BaseCommit:           wp.BaseCommit,
					WorkerRole:           "implementer",
					ModelIdentity:        "ep-test/model-test@v1",
					WorktreeID:           attID,
				},
			},
			controlplane.Command{
				ProjectID: f.projectID,
				Actor:     protocol.Actor{Kind: protocol.ActorControlPlane, ID: "test"},
				Payload: &events.AttemptFailed{
					TaskID:    taskID,
					AttemptID: attID,
					Summary:   "reason=driver_error effects=uncertain",
					Cancelled: false,
				},
			},
		)

		_, err = f.harness.Service.ApplyBatch(ctx, controlplane.BatchCommand{
			ProjectID:             f.projectID,
			Actor:                 protocol.Actor{Kind: protocol.ActorControlPlane, ID: "test"},
			ExpectedStateRevision: ps.StateRevision,
			Commands:              cmds,
		})
		if err != nil {
			t.Fatalf("apply attempt %d: %v", i, err)
		}
		ps, _ = f.harness.Service.ProjectState(ctx, f.projectID)
	}

	exec, err := taskexec.New(f.opts)
	if err != nil {
		t.Fatalf("taskexec.New: %v", err)
	}

	eventsBefore := readAllEvents(t, f.harness.Store, f.projectID)

	authTask := facade.AuthorizedTask{
		Caller: principal.CallerContext{
			PrincipalID: "test-caller",
			ProjectID:   f.projectID,
			SourceDepth: "all",
		},
		Meta: principal.CallMeta{
			SchemaVersion: principal.SchemaVersion,
			ProjectID:     f.projectID,
			CorrelationID: "corr-1",
		},
		TaskID: taskID,
		WorkPackage: principal.WorkPackageRef{
			ID:         wp.WorkPackageID,
			Version:    wp.Version,
			Digest:     wpDigest,
			BaseCommit: wp.BaseCommit,
		},
	}

	_, err = exec.Delegate(ctx, authTask)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var codedErr *principal.CodedError
	if !errors.As(err, &codedErr) {
		t.Fatalf("expected CodedError, got %T: %v", err, err)
	}
	if codedErr.Code() != principal.CodePolicyDenied {
		t.Errorf("code = %q, want %s", codedErr.Code(), principal.CodePolicyDenied)
	}

	// Prove ZERO events emitted
	eventsAfter := readAllEvents(t, f.harness.Store, f.projectID)
	if len(eventsAfter) != len(eventsBefore) {
		t.Errorf("expected 0 events emitted on denial, got %d -> %d", len(eventsBefore), len(eventsAfter))
	}
}

func TestDelegate_PreEffectProjectMismatch(t *testing.T) {
	f := setupFixture(t, "proj-mismatch")
	taskID, wp, wpDigest := initProjectAndApproveWP(t, f, "DC-001", "wp-001", []string{"base.txt"})

	exec, err := taskexec.New(f.opts)
	if err != nil {
		t.Fatalf("taskexec.New: %v", err)
	}

	authTask := facade.AuthorizedTask{
		Caller: principal.CallerContext{
			PrincipalID: "test-caller",
			ProjectID:   "other-project",
			SourceDepth: "all",
		},
		Meta: principal.CallMeta{
			SchemaVersion: principal.SchemaVersion,
			ProjectID:     "other-project",
			CorrelationID: "corr-1",
		},
		TaskID: taskID,
		WorkPackage: principal.WorkPackageRef{
			ID:         wp.WorkPackageID,
			Version:    wp.Version,
			Digest:     wpDigest,
			BaseCommit: wp.BaseCommit,
		},
	}

	eventsBefore := readAllEvents(t, f.harness.Store, f.projectID)

	_, err = exec.Delegate(context.Background(), authTask)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var codedErr *principal.CodedError
	if !errors.As(err, &codedErr) || codedErr.Code() != principal.CodePolicyDenied {
		t.Fatalf("expected CodePolicyDenied, got %v", err)
	}

	eventsAfter := readAllEvents(t, f.harness.Store, f.projectID)
	if len(eventsAfter) != len(eventsBefore) {
		t.Errorf("expected 0 events, got %d -> %d", len(eventsBefore), len(eventsAfter))
	}
}

func TestDelegate_PreEffectStaleStateOrWP(t *testing.T) {
	f := setupFixture(t, "proj-stale")
	taskID, wp, wpDigest := initProjectAndApproveWP(t, f, "DC-001", "wp-001", []string{"base.txt"})

	exec, err := taskexec.New(f.opts)
	if err != nil {
		t.Fatalf("taskexec.New: %v", err)
	}

	t.Run("stale project state revision", func(t *testing.T) {
		authTask := facade.AuthorizedTask{
			Caller: principal.CallerContext{ProjectID: f.projectID},
			Meta: principal.CallMeta{
				ProjectID:             f.projectID,
				ExpectedStateRevision: "ps_wrong_revision",
			},
			TaskID: taskID,
			WorkPackage: principal.WorkPackageRef{
				ID:         wp.WorkPackageID,
				Version:    wp.Version,
				Digest:     wpDigest,
				BaseCommit: wp.BaseCommit,
			},
		}
		_, err := exec.Delegate(context.Background(), authTask)
		if !errors.Is(err, controlplane.ErrStaleProjectState) {
			t.Fatalf("expected ErrStaleProjectState, got %v", err)
		}
	})

	t.Run("stale work package digest", func(t *testing.T) {
		authTask := facade.AuthorizedTask{
			Caller: principal.CallerContext{ProjectID: f.projectID},
			Meta:   principal.CallMeta{ProjectID: f.projectID},
			TaskID: taskID,
			WorkPackage: principal.WorkPackageRef{
				ID:         wp.WorkPackageID,
				Version:    wp.Version,
				Digest:     "sha256:0000000000000000000000000000000000000000000000000000000000000000",
				BaseCommit: wp.BaseCommit,
			},
		}
		_, err := exec.Delegate(context.Background(), authTask)
		if !errors.Is(err, controlplane.ErrStaleWorkPackage) {
			t.Fatalf("expected ErrStaleWorkPackage, got %v", err)
		}
	})
}

func TestDelegate_DriverIDMismatch(t *testing.T) {
	f := setupFixture(t, "proj-driver-mismatch")
	taskID, wp, wpDigest := initProjectAndApproveWP(t, f, "DC-001", "wp-001", []string{"base.txt"})

	// Set observation DriverID to differ from driver.ID()
	f.factory.obs.DriverID = "completely-different-driver-id"

	exec, err := taskexec.New(f.opts)
	if err != nil {
		t.Fatalf("taskexec.New: %v", err)
	}

	authTask := facade.AuthorizedTask{
		Caller: principal.CallerContext{ProjectID: f.projectID},
		Meta:   principal.CallMeta{ProjectID: f.projectID},
		TaskID: taskID,
		WorkPackage: principal.WorkPackageRef{
			ID:         wp.WorkPackageID,
			Version:    wp.Version,
			Digest:     wpDigest,
			BaseCommit: wp.BaseCommit,
		},
	}

	eventsBefore := readAllEvents(t, f.harness.Store, f.projectID)

	_, err = exec.Delegate(context.Background(), authTask)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var codedErr *principal.CodedError
	if !errors.As(err, &codedErr) {
		t.Fatalf("expected CodedError, got %v", err)
	}
	if codedErr.Code() != principal.CodeModelUnavailable {
		t.Errorf("code = %q, want %s", codedErr.Code(), principal.CodeModelUnavailable)
	}
	if len(codedErr.EvidenceRefs()) == 0 || codedErr.EvidenceRefs()[0] != "driver-id-mismatch" {
		t.Errorf("evidence refs = %v, want ['driver-id-mismatch']", codedErr.EvidenceRefs())
	}

	eventsAfter := readAllEvents(t, f.harness.Store, f.projectID)
	if len(eventsAfter) != len(eventsBefore) {
		t.Errorf("expected 0 events, got %d -> %d", len(eventsBefore), len(eventsAfter))
	}
}

func TestDelegate_DriverError(t *testing.T) {
	f := setupFixture(t, "proj-driver-err")
	taskID, wp, wpDigest := initProjectAndApproveWP(t, f, "DC-001", "wp-001", []string{"base.txt"})

	f.driver.SetTurnHandler("turn-1", func(ctx context.Context, input drivers.TurnInput) (drivers.TurnResult, error) {
		return drivers.TurnResult{}, errors.New("network connection reset by peer")
	})

	exec, err := taskexec.New(f.opts)
	if err != nil {
		t.Fatalf("taskexec.New: %v", err)
	}

	authTask := facade.AuthorizedTask{
		Caller: principal.CallerContext{ProjectID: f.projectID},
		Meta:   principal.CallMeta{ProjectID: f.projectID},
		TaskID: taskID,
		WorkPackage: principal.WorkPackageRef{
			ID:         wp.WorkPackageID,
			Version:    wp.Version,
			Digest:     wpDigest,
			BaseCommit: wp.BaseCommit,
		},
	}

	opRef, err := exec.Delegate(context.Background(), authTask)
	if err != nil {
		t.Fatalf("Delegate: %v", err)
	}

	completedRef := f.registry.Wait(context.Background(), opRef, 5*time.Second)
	if completedRef.Status != principal.StatusFailed {
		t.Errorf("status = %s, want failed", completedRef.Status)
	}

	detail, err := f.harness.Service.TaskDetail(context.Background(), f.projectID, "DC-001")
	if err != nil {
		t.Fatalf("task detail: %v", err)
	}
	if len(detail.Attempts) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(detail.Attempts))
	}
	att := detail.Attempts[0]
	if att.Status != tasks.AttemptFailed {
		t.Errorf("attempt status = %s, want failed", att.Status)
	}
	if att.FailureSummary != "reason=driver_error effects=uncertain" {
		t.Errorf("summary = %q, want 'reason=driver_error effects=uncertain'", att.FailureSummary)
	}
	if !attemptFailedGrammar.MatchString(att.FailureSummary) {
		t.Errorf("summary %q violates closed grammar", att.FailureSummary)
	}
}

func TestDelegate_NoChange(t *testing.T) {
	f := setupFixture(t, "proj-nochange")
	taskID, wp, wpDigest := initProjectAndApproveWP(t, f, "DC-001", "wp-001", []string{"base.txt"})

	// Driver stops immediately without calling any tools
	f.driver.SetTurnHandler("turn-1", func(ctx context.Context, input drivers.TurnInput) (drivers.TurnResult, error) {
		return drivers.TurnResult{
			TurnID:    "turn-1",
			Content:   "I decided not to touch anything.",
			ToolCalls: nil,
			Usage:     drivers.KnownUsage(10, 0, 10),
		}, nil
	})

	exec, err := taskexec.New(f.opts)
	if err != nil {
		t.Fatalf("taskexec.New: %v", err)
	}

	authTask := facade.AuthorizedTask{
		Caller: principal.CallerContext{ProjectID: f.projectID},
		Meta:   principal.CallMeta{ProjectID: f.projectID},
		TaskID: taskID,
		WorkPackage: principal.WorkPackageRef{
			ID:         wp.WorkPackageID,
			Version:    wp.Version,
			Digest:     wpDigest,
			BaseCommit: wp.BaseCommit,
		},
	}

	opRef, err := exec.Delegate(context.Background(), authTask)
	if err != nil {
		t.Fatalf("Delegate: %v", err)
	}

	completedRef := f.registry.Wait(context.Background(), opRef, 5*time.Second)
	if completedRef.Status != principal.StatusFailed {
		t.Errorf("status = %s, want failed", completedRef.Status)
	}

	detail, err := f.harness.Service.TaskDetail(context.Background(), f.projectID, "DC-001")
	if err != nil {
		t.Fatalf("task detail: %v", err)
	}
	att := detail.Attempts[0]
	if att.FailureSummary != "reason=no_change effects=none" {
		t.Errorf("summary = %q, want 'reason=no_change effects=none'", att.FailureSummary)
	}
	if !attemptFailedGrammar.MatchString(att.FailureSummary) {
		t.Errorf("summary %q violates closed grammar", att.FailureSummary)
	}
}

func TestDelegate_ScopeViolationDuringExecution(t *testing.T) {
	f := setupFixture(t, "proj-scope-viol")
	taskID, wp, wpDigest := initProjectAndApproveWP(t, f, "DC-001", "wp-001", []string{"base.txt"})

	f.driver.SetTurnHandler("turn-1", func(ctx context.Context, input drivers.TurnInput) (drivers.TurnResult, error) {
		args, _ := json.Marshal(map[string]string{
			"path":    "unauthorized.txt",
			"content": "illegal content\n",
		})
		return drivers.TurnResult{
			TurnID:  "turn-1",
			Content: "Writing to unauthorized file",
			ToolCalls: []drivers.ToolCall{
				{ID: "call-1", Name: "write_file", Arguments: args},
			},
			Usage: drivers.KnownUsage(10, 0, 10),
		}, nil
	})

	f.driver.SetTurnHandler("turn-2", func(ctx context.Context, input drivers.TurnInput) (drivers.TurnResult, error) {
		// Tool failed, model acknowledges and exits without further writes
		return drivers.TurnResult{
			TurnID:    "turn-2",
			Content:   "I could not write the file.",
			ToolCalls: nil,
			Usage:     drivers.KnownUsage(10, 0, 10),
		}, nil
	})

	exec, err := taskexec.New(f.opts)
	if err != nil {
		t.Fatalf("taskexec.New: %v", err)
	}

	authTask := facade.AuthorizedTask{
		Caller: principal.CallerContext{ProjectID: f.projectID},
		Meta:   principal.CallMeta{ProjectID: f.projectID},
		TaskID: taskID,
		WorkPackage: principal.WorkPackageRef{
			ID:         wp.WorkPackageID,
			Version:    wp.Version,
			Digest:     wpDigest,
			BaseCommit: wp.BaseCommit,
		},
	}

	opRef, err := exec.Delegate(context.Background(), authTask)
	if err != nil {
		t.Fatalf("Delegate: %v", err)
	}

	completedRef := f.registry.Wait(context.Background(), opRef, 5*time.Second)
	if completedRef.Status != principal.StatusFailed {
		t.Errorf("status = %s, want failed", completedRef.Status)
	}

	// Because no files were successfully modified, attempt ends with no_change
	detail, err := f.harness.Service.TaskDetail(context.Background(), f.projectID, "DC-001")
	if err != nil {
		t.Fatalf("task detail: %v", err)
	}
	att := detail.Attempts[0]
	if att.FailureSummary != "reason=no_change effects=none" {
		t.Errorf("summary = %q, want 'reason=no_change effects=none'", att.FailureSummary)
	}
}

func TestWorkerTools_WriteFileSafety(t *testing.T) {
	tempDir := t.TempDir()
	scope := &tools.Scope{
		ProjectID:    "proj-test",
		WorktreeID:   "wt-1",
		WorktreePath: tempDir,
	}
	mediator := drivers.NewScopedToolMediator(scope)
	writeScope := []string{"pkg/valid.go", "docs/*"}
	toolDefs := taskexec.SetupWorkerToolsForTesting(mediator, scope, process.NewRunner(), writeScope)
	if len(toolDefs) != 5 {
		t.Fatalf("expected 5 tool definitions, got %d", len(toolDefs))
	}

	callWrite := func(path, content string) error {
		args, _ := json.Marshal(map[string]string{"path": path, "content": content})
		res, err := mediator.ExecuteTool(context.Background(), drivers.ToolCall{
			ID:        "tc-1",
			Name:      "write_file",
			Arguments: args,
		})
		if err != nil {
			return err
		}
		if res.IsError {
			return errors.New(res.Content)
		}
		return nil
	}

	t.Run("valid file write within scope", func(t *testing.T) {
		err := callWrite("pkg/valid.go", "package pkg\n")
		if err != nil {
			t.Errorf("expected success, got %v", err)
		}
		data, readErr := os.ReadFile(filepath.Join(tempDir, "pkg/valid.go"))
		if readErr != nil || string(data) != "package pkg\n" {
			t.Errorf("file content mismatch: %v, %q", readErr, string(data))
		}
	})

	t.Run("out of scope path rejected", func(t *testing.T) {
		err := callWrite("other/file.go", "package other\n")
		if err == nil {
			t.Error("expected error for path outside write scope, got nil")
		}
	})

	t.Run("absolute path rejected", func(t *testing.T) {
		err := callWrite("/etc/passwd", "root\n")
		if err == nil {
			t.Error("expected error for absolute path, got nil")
		}
	})

	t.Run("path traversal rejected", func(t *testing.T) {
		err := callWrite("../outside.txt", "outside\n")
		if err == nil {
			t.Error("expected error for path traversal, got nil")
		}
	})

	t.Run(".git modification forbidden", func(t *testing.T) {
		err := callWrite(".git/config", "config\n")
		if err == nil {
			t.Error("expected error for .git directory, got nil")
		}
	})

	t.Run("payload exceeding 256 KiB rejected", func(t *testing.T) {
		huge := strings.Repeat("A", 256*1024+1)
		err := callWrite("docs/huge.txt", huge)
		if err == nil {
			t.Error("expected error for file exceeding 256 KiB, got nil")
		}
	})

	t.Run("invalid UTF-8 rejected", func(t *testing.T) {
		rawArgs := append([]byte(`{"path":"docs/bad.txt","content":"`), 0xff, 0xfe, 0xfd, '"', '}')
		res, err := mediator.ExecuteTool(context.Background(), drivers.ToolCall{
			ID:        "tc-bad-utf8",
			Name:      "write_file",
			Arguments: rawArgs,
		})
		if err == nil && !res.IsError {
			t.Error("expected error for invalid UTF-8, got nil")
		}
	})
}

func TestValidate_WP9Stub(t *testing.T) {
	f := setupFixture(t, "proj-val")
	exec, err := taskexec.New(f.opts)
	if err != nil {
		t.Fatalf("taskexec.New: %v", err)
	}

	candidate := principal.CandidateRef{
		TaskID:    "tsk_1",
		AttemptID: "att_1",
		Commit:    "abc1234",
	}

	_, err = exec.Validate(context.Background(), principal.CallerContext{}, principal.CallMeta{}, candidate, "prof-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var codedErr *principal.CodedError
	if !errors.As(err, &codedErr) {
		t.Fatalf("expected CodedError, got %v", err)
	}
	if codedErr.Code() != principal.CodeModelUnavailable {
		t.Errorf("code = %q, want %s", codedErr.Code(), principal.CodeModelUnavailable)
	}
}

func TestDelegate_MaxRequestBytesExceeded(t *testing.T) {
	f := setupFixture(t, "proj-reqbytes")
	f.policy.Grants[0].Limits.MaxRequestBytes = 10
	taskID, wp, wpDigest := initProjectAndApproveWP(t, f, "DC-001", "wp-001", []string{"base.txt"})

	exec, err := taskexec.New(f.opts)
	if err != nil {
		t.Fatalf("taskexec.New: %v", err)
	}

	authTask := facade.AuthorizedTask{
		Caller: principal.CallerContext{ProjectID: f.projectID},
		Meta:   principal.CallMeta{ProjectID: f.projectID},
		TaskID: taskID,
		WorkPackage: principal.WorkPackageRef{
			ID:         wp.WorkPackageID,
			Version:    wp.Version,
			Digest:     wpDigest,
			BaseCommit: wp.BaseCommit,
		},
	}

	opRef, err := exec.Delegate(context.Background(), authTask)
	if err != nil {
		t.Fatalf("Delegate: %v", err)
	}

	completedRef := f.registry.Wait(context.Background(), opRef, 5*time.Second)
	if completedRef.Status != principal.StatusFailed {
		t.Errorf("status = %s, want failed", completedRef.Status)
	}

	detail, err := f.harness.Service.TaskDetail(context.Background(), f.projectID, "DC-001")
	if err != nil {
		t.Fatalf("TaskDetail: %v", err)
	}
	if len(detail.Attempts) == 0 {
		t.Fatal("expected at least 1 attempt")
	}
	att := detail.Attempts[len(detail.Attempts)-1]
	if att.FailureSummary != "reason=limit_reached effects=none" {
		t.Errorf("summary = %q, want 'reason=limit_reached effects=none'", att.FailureSummary)
	}
	if !attemptFailedGrammar.MatchString(att.FailureSummary) {
		t.Errorf("summary %q violates closed grammar", att.FailureSummary)
	}
}

func TestDelegate_NonPositiveMaxOutputTokensPerCallRefusal(t *testing.T) {
	f := setupFixture(t, "proj-maxoutput")
	f.policy.Grants[0].Limits.MaxOutputTokensPerCall = 0
	taskID, wp, wpDigest := initProjectAndApproveWP(t, f, "DC-002", "wp-002", []string{"base.txt"})

	exec, err := taskexec.New(f.opts)
	if err != nil {
		t.Fatalf("taskexec.New: %v", err)
	}

	authTask := facade.AuthorizedTask{
		Caller: principal.CallerContext{ProjectID: f.projectID},
		Meta:   principal.CallMeta{ProjectID: f.projectID},
		TaskID: taskID,
		WorkPackage: principal.WorkPackageRef{
			ID:         wp.WorkPackageID,
			Version:    wp.Version,
			Digest:     wpDigest,
			BaseCommit: wp.BaseCommit,
		},
	}

	_, err = exec.Delegate(context.Background(), authTask)
	if err == nil {
		t.Fatal("expected Delegate to fail synchronously with invalid_argument, got nil")
	}
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("expected CategoryInvalidArgument, got %v", err)
	}
}
