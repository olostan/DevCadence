package reviewexec_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/actors"
	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/execpolicy"
	"github.com/olostan/DevCadence/internal/execrt"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/principal/facade"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/repository"
	"github.com/olostan/DevCadence/internal/reviewexec"
	"github.com/olostan/DevCadence/internal/storage"
	"github.com/olostan/DevCadence/internal/testsupport"
	"github.com/olostan/DevCadence/internal/worktrees"
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

type testFixture struct {
	projectID string
	harness   *testsupport.Harness
	gitRepo   *testsupport.GitRepo
	repo      *repository.Repository
	repos     *execrt.SingleRepositoryProvider
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
	opts      reviewexec.Options
}

func setupFixture(t *testing.T, projectID string) *testFixture {
	t.Helper()
	harness := testsupport.NewHarness(t)
	gitRepo := testsupport.NewGitRepo(t)
	gitRepo.WriteFile("main.go", "package main\n\nfunc main() {}\n")
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
				EndpointID:     "ep-reviewer-1",
				ModelID:        "model-reviewer",
				Roles:          []string{"reviewer"},
				Locality:       protocol.LocalityLocal,
				SourceExposure: protocol.ExposureToolMediatedWorktree,
				ChannelKind:    protocol.ChannelCLISubprocess,
				Limits: execpolicy.ExecutionLimits{
					MaxTurns:               5,
					MaxToolCalls:           10,
					MaxTotalTokens:         100000,
					MaxDurationSeconds:     300,
					MaxOutputTokensPerCall: 4096,
					MaxRequestBytes:        65536,
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
				ChannelID:             "chan-ep-reviewer-1",
				EndpointID:            "ep-reviewer-1",
				Kind:                  protocol.ChannelCLISubprocess,
				SessionMode:           protocol.SessionStatelessPerCall,
				ContextControl:        protocol.ContextControlExactStateless,
				PrefixCache:           protocol.PrefixCacheNone,
				MaxConcurrentRequests: 2,
			},
		},
		RoleBindings: []protocol.RoleBinding{
			{
				Role:             "reviewer",
				EndpointID:       "ep-reviewer-1",
				ChannelID:        "chan-ep-reviewer-1",
				BudgetPoolID:     "pool-default",
				ContextProfileID: "prof-ep-reviewer-1",
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

	prof := compiler.MustDefaultProvisionalProfile("ep-reviewer-1", "chan-ep-reviewer-1", "model-reviewer", 200000)
	prof.ProfileID = "prof-ep-reviewer-1"
	resolver := execpolicy.NewPortfolioEndpointResolver(portfolio).WithContextProfiles(*prof).WithEndpoints(
		protocol.CognitionEndpoint{
			ID:          "ep-reviewer-1",
			ModelID:     "model-reviewer",
			Provider:    "google",
			ModelFamily: "gemini",
			AccountRef:  "acc-test-reviewer",
		},
	)

	driver := drivers.NewFakeDriver("driver-reviewer-1")
	factory := &fakeDriverFactory{
		driver: driver,
		obs: execpolicy.EndpointObservation{
			DriverID:       "driver-reviewer-1",
			ModelRevision:  "rev-1",
			RuntimeVersion: "v1.0.0",
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

	opts := reviewexec.Options{
		ProjectID:         projectID,
		ControlPlane:      harness.Service,
		Worktrees:         wtManager,
		Repositories:      repos,
		Runner:            runner,
		Registry:          registry,
		Policy:            policySrc,
		Resolver:          resolver,
		Drivers:           factory,
		Compiler:          comp,
		Artifacts:         artifacts,
		StateDir:          stateDir,
		IndependenceBasis: actors.BasisEndpointModel,
		Clock:             clock.System(),
		IDs:               ids.NewULIDSource(),
	}

	return &testFixture{
		projectID: projectID,
		harness:   harness,
		gitRepo:   gitRepo,
		repo:      repo,
		repos:     repos,
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

func driveTaskToReviewing(t *testing.T, f *testFixture, taskAlias, wpID, attemptID string, workerBasis protocol.ActorBasis) (string, string) {
	t.Helper()
	ctx := context.Background()

	// 1. Initialize project
	_, err := f.harness.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: f.projectID,
		Payload: &events.ProjectInitialized{
			Name:           "Test Project",
			MilestoneID:    "M1",
			MilestoneTitle: "Milestone 1",
			AcceptedCommit: f.gitRepo.Head(),
		},
	})
	if err != nil {
		t.Fatalf("append ProjectInitialized: %v", err)
	}

	// 2. Create task
	taskID := "tsk_" + taskAlias
	_, err = f.harness.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: f.projectID,
		Payload: &events.TaskCreated{
			TaskID:      taskID,
			Alias:       taskAlias,
			Title:       "Test task",
			ChangeClass: protocol.ChangeLocal,
		},
	})
	if err != nil {
		t.Fatalf("append TaskCreated: %v", err)
	}

	_, err = f.harness.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: f.projectID,
		Payload: &events.TaskDesignStarted{
			TaskID: taskID,
			Reason: "initial design",
		},
	})
	if err != nil {
		t.Fatalf("append TaskDesignStarted: %v", err)
	}

	// 3. Approve Work Package
	ps, err := f.harness.Service.ProjectState(ctx, f.projectID)
	if err != nil {
		t.Fatalf("project state: %v", err)
	}

	wp := &protocol.EngineeringWorkPackage{
		SchemaVersion:        protocol.SchemaVersion1,
		WorkPackageID:        wpID,
		TaskID:               taskID,
		Version:              1,
		ProjectID:            f.projectID,
		ProjectStateRevision: ps.StateRevision,
		BaseCommit:           f.gitRepo.Head(),
		Objective:            "Test objective",
		Rationale:            "Test rationale",
		ArchitecturalIntent:  "Test architectural intent",
		ChangeClass:          protocol.ChangeLocal,
		Scope: protocol.Scope{
			InScope: []string{"main.go"},
		},
		Guidance: []protocol.Guidance{
			{ID: "G1", Strength: protocol.GuidanceMust, Statement: "Modify only declared in_scope"},
		},
		AcceptanceCriteria:     []string{"Change implemented"},
		ValidationRequirements: []string{"deterministic validation"},
		EscalationConditions:   []string{"Uncertainty"},
	}
	wpBytes, _ := protocol.CanonicalJSON(wp)
	wpDigest := protocol.DigestBytes(wpBytes)

	_, err = f.harness.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: f.projectID,
		Payload: &events.WorkPackageApproved{
			TaskID:               taskID,
			WorkPackageID:        wpID,
			WorkPackageVersion:   1,
			RecordDigest:         wpDigest,
			ProjectStateRevision: ps.StateRevision,
			BaseCommit:           f.gitRepo.Head(),
			ChangeClass:          protocol.ChangeLocal,
		},
		Records: []controlplane.RecordToStore{
			{Version: 1, Record: wp},
		},
	})
	if err != nil {
		t.Fatalf("append WorkPackageApproved: %v", err)
	}

	// 4. Delegate task
	_, err = f.harness.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: f.projectID,
		Payload: &events.TaskDelegated{
			TaskID:        taskID,
			WorkPackageID: wpID,
			WorkerRole:    "implementer",
			MaxAttempts:   3,
		},
	})
	if err != nil {
		t.Fatalf("append TaskDelegated: %v", err)
	}

	// 5. Worker attempt started + record worker provenance
	workerActorID, err := actors.DeriveActorID(actors.BasisEndpointModel, workerBasis)
	if err != nil {
		t.Fatalf("derive worker actor id: %v", err)
	}
	workerProv := &protocol.InvocationProvenance{
		SchemaVersion: protocol.SchemaVersion1,
		ProvenanceID:  attemptID + ":implementer",
		ProjectID:     f.projectID,
		TaskID:        taskID,
		AttemptID:     attemptID,
		WorkPackageID: wpID,
		Role:          protocol.ProvenanceRoleImplementer,
		Actor: protocol.ActorProvenance{
			ActorID:      workerActorID,
			InvocationID: attemptID,
			Role:         protocol.ProvenanceRoleImplementer,
		},
		Basis:                 workerBasis,
		IndependenceBasis:     actors.BasisEndpointModel,
		EndpointBindingDigest: "sha256:" + strings.Repeat("2", 64),
		ContextManifestDigest: "sha256:" + strings.Repeat("3", 64),
		PromptDigest:          "sha256:" + strings.Repeat("4", 64),
		StartedAt:             time.Now().UTC(),
	}

	ps, _ = f.harness.Service.ProjectState(ctx, f.projectID)
	_, err = f.harness.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: f.projectID,
		Payload: &events.AttemptStarted{
			TaskID:               taskID,
			AttemptID:            attemptID,
			WorkPackageID:        wpID,
			WorkPackageVersion:   1,
			ProjectStateRevision: ps.StateRevision,
			WorkerRole:           "implementer",
			WorktreeID:           attemptID,
		},
		Records: []controlplane.RecordToStore{
			{Version: 1, Record: workerProv},
		},
	})
	if err != nil {
		t.Fatalf("append AttemptStarted: %v", err)
	}

	// 6. Produce candidate commit
	f.gitRepo.WriteFile("main.go", "package main\n\n// candidate change\nfunc main() {}\n")
	f.gitRepo.Commit("candidate commit")
	candidateCommit := f.gitRepo.Head()

	_, err = f.harness.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: f.projectID,
		Payload: &events.CandidateProduced{
			TaskID:          taskID,
			AttemptID:       attemptID,
			CandidateCommit: candidateCommit,
			Summary:         "candidate produced",
		},
	})
	if err != nil {
		t.Fatalf("append CandidateProduced: %v", err)
	}

	// 7. Transition task to StateReviewing via ValidationCompleted (Pass)
	valResult := &protocol.ValidationResult{
		SchemaVersion: protocol.SchemaVersion1,
		ValidationID:  "val_01",
		ProjectID:     f.projectID,
		Subject: protocol.ValidationSubject{
			Kind:      "attempt",
			TaskID:    taskID,
			AttemptID: attemptID,
		},
		Commit: candidateCommit,
		Status: protocol.ValidationPass,
		Checks: []protocol.CheckResult{
			{ID: "c1", Kind: "test", Status: protocol.CheckPass},
		},
	}
	valBytes, _ := protocol.CanonicalJSON(valResult)
	valDigest := protocol.DigestBytes(valBytes)

	_, err = f.harness.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: f.projectID,
		Payload: &events.ValidationCompleted{
			TaskID:       taskID,
			AttemptID:    attemptID,
			ValidationID: "val_01",
			Scope:        events.ScopeAttempt,
			Commit:       candidateCommit,
			Status:       protocol.ValidationPass,
			RecordDigest: valDigest,
		},
		Records: []controlplane.RecordToStore{
			{Version: 1, Record: valResult},
		},
	})
	if err != nil {
		t.Fatalf("append ValidationCompleted: %v", err)
	}

	return taskID, candidateCommit
}

func defaultWorkerBasis() protocol.ActorBasis {
	return protocol.ActorBasis{
		EndpointID:    "ep-worker-1",
		ModelID:       "model-worker",
		ModelRevision: "rev-worker",
		Provider:      "anthropic",
		ModelFamily:   "claude",
		AccountRef:    "acc-worker",
	}
}

// 1. Test New validates options and rejects unknown independence basis.
func TestNew_ValidatesOptionsAndIndependenceBasis(t *testing.T) {
	f := setupFixture(t, "proj-opts-test")

	t.Run("rejects empty ProjectID", func(t *testing.T) {
		opts := f.opts
		opts.ProjectID = ""
		if _, err := reviewexec.New(opts); err == nil {
			t.Fatal("expected error for empty ProjectID, got nil")
		}
	})

	t.Run("rejects nil ControlPlane", func(t *testing.T) {
		opts := f.opts
		opts.ControlPlane = nil
		if _, err := reviewexec.New(opts); err == nil {
			t.Fatal("expected error for nil ControlPlane, got nil")
		}
	})

	t.Run("rejects nil Worktrees", func(t *testing.T) {
		opts := f.opts
		opts.Worktrees = nil
		if _, err := reviewexec.New(opts); err == nil {
			t.Fatal("expected error for nil Worktrees, got nil")
		}
	})

	t.Run("rejects nil Repositories", func(t *testing.T) {
		opts := f.opts
		opts.Repositories = nil
		if _, err := reviewexec.New(opts); err == nil {
			t.Fatal("expected error for nil Repositories, got nil")
		}
	})

	t.Run("rejects nil Runner", func(t *testing.T) {
		opts := f.opts
		opts.Runner = nil
		if _, err := reviewexec.New(opts); err == nil {
			t.Fatal("expected error for nil Runner, got nil")
		}
	})

	t.Run("rejects nil Registry", func(t *testing.T) {
		opts := f.opts
		opts.Registry = nil
		if _, err := reviewexec.New(opts); err == nil {
			t.Fatal("expected error for nil Registry, got nil")
		}
	})

	t.Run("rejects nil Policy", func(t *testing.T) {
		opts := f.opts
		opts.Policy = nil
		if _, err := reviewexec.New(opts); err == nil {
			t.Fatal("expected error for nil Policy, got nil")
		}
	})

	t.Run("rejects nil Resolver", func(t *testing.T) {
		opts := f.opts
		opts.Resolver = nil
		if _, err := reviewexec.New(opts); err == nil {
			t.Fatal("expected error for nil Resolver, got nil")
		}
	})

	t.Run("rejects nil Drivers", func(t *testing.T) {
		opts := f.opts
		opts.Drivers = nil
		if _, err := reviewexec.New(opts); err == nil {
			t.Fatal("expected error for nil Drivers, got nil")
		}
	})

	t.Run("rejects nil Compiler", func(t *testing.T) {
		opts := f.opts
		opts.Compiler = nil
		if _, err := reviewexec.New(opts); err == nil {
			t.Fatal("expected error for nil Compiler, got nil")
		}
	})

	t.Run("rejects nil Artifacts", func(t *testing.T) {
		opts := f.opts
		opts.Artifacts = nil
		if _, err := reviewexec.New(opts); err == nil {
			t.Fatal("expected error for nil Artifacts, got nil")
		}
	})

	t.Run("rejects empty StateDir", func(t *testing.T) {
		opts := f.opts
		opts.StateDir = ""
		if _, err := reviewexec.New(opts); err == nil {
			t.Fatal("expected error for empty StateDir, got nil")
		}
	})

	t.Run("rejects unknown IndependenceBasis", func(t *testing.T) {
		opts := f.opts
		opts.IndependenceBasis = "invalid_basis"
		if _, err := reviewexec.New(opts); err == nil {
			t.Fatal("expected error for unknown IndependenceBasis, got nil")
		}
	})

	t.Run("accepts valid endpoint_model basis", func(t *testing.T) {
		opts := f.opts
		opts.IndependenceBasis = actors.BasisEndpointModel
		exec, err := reviewexec.New(opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if exec == nil {
			t.Fatal("expected non-nil executor")
		}
	})

	t.Run("accepts valid model_family_account basis", func(t *testing.T) {
		opts := f.opts
		opts.IndependenceBasis = actors.BasisModelFamilyAccount
		exec, err := reviewexec.New(opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if exec == nil {
			t.Fatal("expected non-nil executor")
		}
	})
}

// 2. Test synchronous rejection on empty dimensions or unknown dimensions.
func TestReview_SynchronousRejection_DimensionsAndValidation(t *testing.T) {
	f := setupFixture(t, "proj-sync-rejections")
	exec, err := reviewexec.New(f.opts)
	if err != nil {
		t.Fatalf("reviewexec.New: %v", err)
	}

	taskID, commit := driveTaskToReviewing(t, f, "T-01", "wp-01", "att-01", defaultWorkerBasis())
	ctx := context.Background()
	caller := principal.CallerContext{PrincipalID: "princ_1", ProjectID: f.projectID}
	meta := principal.CallMeta{ProjectID: f.projectID}
	cand := principal.CandidateRef{TaskID: taskID, AttemptID: "att-01", Commit: commit}

	t.Run("rejects empty dimensions with review-dimensions-required", func(t *testing.T) {
		_, err := exec.Review(ctx, caller, meta, cand, []string{})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var coded *principal.CodedError
		if !errors.As(err, &coded) {
			t.Fatalf("expected *principal.CodedError, got %T (%v)", err, err)
		}
		if coded.Code() != principal.CodeInvalidArgument {
			t.Fatalf("expected CodeInvalidArgument, got %s", coded.Code())
		}
		if len(coded.EvidenceRefs()) == 0 || coded.EvidenceRefs()[0] != "review-dimensions-required" {
			t.Fatalf("expected ref review-dimensions-required, got %v", coded.EvidenceRefs())
		}
	})

	t.Run("rejects unknown dimensions with invalid-dimension", func(t *testing.T) {
		_, err := exec.Review(ctx, caller, meta, cand, []string{"not_a_valid_dimension"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var coded *principal.CodedError
		if !errors.As(err, &coded) {
			t.Fatalf("expected *principal.CodedError, got %T (%v)", err, err)
		}
		if coded.Code() != principal.CodeInvalidArgument {
			t.Fatalf("expected CodeInvalidArgument, got %s", coded.Code())
		}
		if len(coded.EvidenceRefs()) == 0 || coded.EvidenceRefs()[0] != "invalid-dimension" {
			t.Fatalf("expected ref invalid-dimension, got %v", coded.EvidenceRefs())
		}
	})

	t.Run("rejects duplicate dimensions with duplicate-dimension", func(t *testing.T) {
		_, err := exec.Review(ctx, caller, meta, cand, []string{"correctness", "correctness"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var coded *principal.CodedError
		if !errors.As(err, &coded) {
			t.Fatalf("expected *principal.CodedError, got %T (%v)", err, err)
		}
		if coded.Code() != principal.CodeInvalidArgument {
			t.Fatalf("expected CodeInvalidArgument, got %s", coded.Code())
		}
		if len(coded.EvidenceRefs()) == 0 || coded.EvidenceRefs()[0] != "duplicate-dimension" {
			t.Fatalf("expected ref duplicate-dimension, got %v", coded.EvidenceRefs())
		}
	})

	t.Run("rejects project mismatch caller", func(t *testing.T) {
		badCaller := principal.CallerContext{PrincipalID: "princ_1", ProjectID: "other-project"}
		_, err := exec.Review(ctx, badCaller, meta, cand, []string{"correctness"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var coded *principal.CodedError
		if !errors.As(err, &coded) || coded.Code() != principal.CodePolicyDenied {
			t.Fatalf("expected CodePolicyDenied, got %v", err)
		}
	})

	t.Run("rejects missing candidate commit", func(t *testing.T) {
		badCand := cand
		badCand.Commit = ""
		_, err := exec.Review(ctx, caller, meta, badCand, []string{"correctness"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var coded *principal.CodedError
		if !errors.As(err, &coded) || coded.Code() != principal.CodeInvalidArgument {
			t.Fatalf("expected CodeInvalidArgument, got %v", err)
		}
	})
}

// 3. Test fast-fail rejection when intent already exists (review-already-recorded).
func TestReview_FastFail_AlreadyRecorded(t *testing.T) {
	f := setupFixture(t, "proj-fastfail-intent")
	exec, err := reviewexec.New(f.opts)
	if err != nil {
		t.Fatalf("reviewexec.New: %v", err)
	}

	taskID, commit := driveTaskToReviewing(t, f, "T-02", "wp-02", "att-02", defaultWorkerBasis())
	ctx := context.Background()
	caller := principal.CallerContext{PrincipalID: "princ_1", ProjectID: f.projectID}
	meta := principal.CallMeta{ProjectID: f.projectID}
	cand := principal.CandidateRef{TaskID: taskID, AttemptID: "att-02", Commit: commit}

	// Manually inject ReviewInvocationIntent record
	existingIntent := &protocol.ReviewInvocationIntent{
		SchemaVersion:    protocol.SchemaVersion1,
		ReviewID:         "rev-existing",
		ProjectID:        f.projectID,
		TaskID:           taskID,
		AttemptID:        "att-02",
		WorkPackageID:    "wp-02",
		Dimension:        protocol.DimensionCorrectness,
		InvocationID:     "inv-existing",
		CandidateCommit:  commit,
		InvocationNumber: 1,
		ReviewerBasis: protocol.ActorBasis{
			EndpointID:    "ep-reviewer-1",
			ModelID:       "model-reviewer",
			ModelRevision: "rev-1",
		},
		IndependenceBasis:     actors.BasisEndpointModel,
		EndpointBindingDigest: "sha256:" + strings.Repeat("e", 64),
		StartedAt:             time.Now().UTC(),
	}

	intentBytes, _ := protocol.CanonicalJSON(existingIntent)
	intentDigest := protocol.DigestBytes(intentBytes)

	ps, _ := f.harness.Service.ProjectState(ctx, f.projectID)
	_, err = f.harness.Service.ApplyBatch(ctx, controlplane.BatchCommand{
		ProjectID:             f.projectID,
		Actor:                 protocol.Actor{Kind: protocol.ActorControlPlane, ID: "test"},
		ExpectedStateRevision: ps.StateRevision,
		Commands: []controlplane.Command{
			{
				ProjectID: f.projectID,
				Actor:     protocol.Actor{Kind: protocol.ActorControlPlane, ID: "test"},
				Payload: &events.ReviewInvocationStarted{
					TaskID:          taskID,
					AttemptID:       "att-02",
					WorkPackageID:   "wp-02",
					ReviewID:        "rev-existing",
					InvocationID:    "inv-existing",
					Dimension:       protocol.DimensionCorrectness,
					CandidateCommit: commit,
					RecordDigest:    intentDigest,
				},
				Records: []controlplane.RecordToStore{
					{Version: 1, Record: existingIntent},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("failed injecting intent: %v", err)
	}

	// Now call Review: should fast-fail synchronously with review-already-recorded
	_, err = exec.Review(ctx, caller, meta, cand, []string{"correctness"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var coded *principal.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("expected *principal.CodedError, got %T (%v)", err, err)
	}
	if coded.Code() != principal.CodePolicyDenied {
		t.Fatalf("expected CodePolicyDenied, got %s", coded.Code())
	}
	if len(coded.EvidenceRefs()) == 0 || coded.EvidenceRefs()[0] != "review-already-recorded" {
		t.Fatalf("expected ref review-already-recorded, got %v", coded.EvidenceRefs())
	}
}

// Helper to script a passing review result from the fake driver
func scriptPassingReview(driver *drivers.FakeDriver) {
	reviewJSON := `{
  "schema_version": "1.0",
  "review_id": "placeholder",
  "project_id": "placeholder",
  "attempt_id": "placeholder",
  "work_package_id": "placeholder",
  "dimension": "correctness",
  "verdict": "pass",
  "findings": [],
  "must_compliance": [],
  "principal_escalation_recommended": false
}`
	driver.SetTurnHandler("turn-1", func(ctx context.Context, input drivers.TurnInput) (drivers.TurnResult, error) {
		return drivers.TurnResult{
			TurnID:  "turn-1",
			Content: reviewJSON,
			Usage:   drivers.KnownUsage(50, 0, 50),
		}, nil
	})
}

// 4. Test pre-model commit: ReviewInvocationStarted and ReviewInvocationIntent exist in store.
func TestReview_PreModelCommit(t *testing.T) {
	f := setupFixture(t, "proj-premodel-commit")
	exec, err := reviewexec.New(f.opts)
	if err != nil {
		t.Fatalf("reviewexec.New: %v", err)
	}

	taskID, commit := driveTaskToReviewing(t, f, "T-03", "wp-03", "att-03", defaultWorkerBasis())
	ctx := context.Background()
	caller := principal.CallerContext{PrincipalID: "princ_1", ProjectID: f.projectID}
	meta := principal.CallMeta{ProjectID: f.projectID}
	cand := principal.CandidateRef{TaskID: taskID, AttemptID: "att-03", Commit: commit}

	scriptPassingReview(f.driver)

	opRef, err := exec.Review(ctx, caller, meta, cand, []string{"correctness"})
	if err != nil {
		t.Fatalf("Review failed: %v", err)
	}

	completedRef := f.registry.Wait(ctx, opRef, 5*time.Second)
	if completedRef.Status != principal.StatusCompleted {
		status, _ := f.registry.Lookup(f.projectID, opRef)
		t.Fatalf("operation status %s, error: %+v", completedRef.Status, status.Error)
	}

	// Verify ReviewInvocationIntent record is stored
	intentID := "intent:att-03:correctness"
	stored, err := f.harness.Service.Record(ctx, f.projectID, "ReviewInvocationIntent", intentID, 1)
	if err != nil {
		t.Fatalf("ReviewInvocationIntent record not found: %v", err)
	}
	var intent protocol.ReviewInvocationIntent
	if err := protocol.Unmarshal([]byte(stored.Document), &intent); err != nil {
		t.Fatalf("unmarshal intent: %v", err)
	}
	if intent.TaskID != taskID || intent.AttemptID != "att-03" || intent.Dimension != protocol.DimensionCorrectness {
		t.Fatalf("intent fields mismatch: %+v", intent)
	}

	// Verify ReviewInvocationStarted event was recorded
	var foundStarted bool
	err = f.harness.Store.Read(ctx, func(tx *storage.Tx) error {
		evs, err := tx.ReadEvents(ctx, storage.EventQuery{ProjectID: f.projectID})
		if err != nil {
			return err
		}
		for _, e := range evs {
			if e.EventType == events.TypeReviewInvocationStarted {
				foundStarted = true
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if !foundStarted {
		t.Fatal("ReviewInvocationStarted event not found in event log")
	}
}

// 5. Test pass-shopping prevention: crash/orphan intent permanently blocks re-invocation.
func TestReview_PassShoppingPrevention(t *testing.T) {
	f := setupFixture(t, "proj-pass-shopping")
	exec, err := reviewexec.New(f.opts)
	if err != nil {
		t.Fatalf("reviewexec.New: %v", err)
	}

	taskID, commit := driveTaskToReviewing(t, f, "T-04", "wp-04", "att-04", defaultWorkerBasis())
	ctx := context.Background()
	caller := principal.CallerContext{PrincipalID: "princ_1", ProjectID: f.projectID}
	meta := principal.CallMeta{ProjectID: f.projectID}
	cand := principal.CandidateRef{TaskID: taskID, AttemptID: "att-04", Commit: commit}

	// First invocation: driver fails (simulating crash or failure after intent committed)
	f.driver.SetTurnHandler("turn-1", func(ctx context.Context, input drivers.TurnInput) (drivers.TurnResult, error) {
		return drivers.TurnResult{}, errors.New("simulated model crash")
	})

	opRef, err := exec.Review(ctx, caller, meta, cand, []string{"correctness"})
	if err != nil {
		t.Fatalf("initial Review call failed: %v", err)
	}

	completedRef := f.registry.Wait(ctx, opRef, 5*time.Second)
	if completedRef.Status != principal.StatusFailed {
		t.Fatalf("expected initial review to fail, got %s", completedRef.Status)
	}

	// Second invocation (retry attempt): must be rejected permanently because intent is durable!
	_, err = exec.Review(ctx, caller, meta, cand, []string{"correctness"})
	if err == nil {
		t.Fatal("expected pass-shopping retry to be rejected, got nil")
	}
	var coded *principal.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("expected *principal.CodedError, got %T (%v)", err, err)
	}
	if coded.Code() != principal.CodePolicyDenied {
		t.Fatalf("expected CodePolicyDenied, got %s", coded.Code())
	}
	if len(coded.EvidenceRefs()) == 0 || coded.EvidenceRefs()[0] != "review-already-recorded" {
		t.Fatalf("expected ref review-already-recorded, got %v", coded.EvidenceRefs())
	}
}

// 6. Test independent actor collision: if reviewer basis resolves to same actor ID as worker, fails with independent-actor-collision.
func TestReview_IndependentActorCollision(t *testing.T) {
	f := setupFixture(t, "proj-actor-collision")

	// Set worker basis to match the reviewer endpoint's actor basis!
	collidingWorkerBasis := protocol.ActorBasis{
		EndpointID:    "ep-reviewer-1",
		ModelID:       "model-reviewer",
		ModelRevision: "rev-1",
		Provider:      "google",
		ModelFamily:   "gemini",
		AccountRef:    "acc-test-reviewer",
	}

	exec, err := reviewexec.New(f.opts)
	if err != nil {
		t.Fatalf("reviewexec.New: %v", err)
	}

	taskID, commit := driveTaskToReviewing(t, f, "T-05", "wp-05", "att-05", collidingWorkerBasis)
	ctx := context.Background()
	caller := principal.CallerContext{PrincipalID: "princ_1", ProjectID: f.projectID}
	meta := principal.CallMeta{ProjectID: f.projectID}
	cand := principal.CandidateRef{TaskID: taskID, AttemptID: "att-05", Commit: commit}

	opRef, err := exec.Review(ctx, caller, meta, cand, []string{"correctness"})
	if err != nil {
		// If rejected synchronously
		var coded *principal.CodedError
		if errors.As(err, &coded) {
			found := false
			for _, r := range coded.EvidenceRefs() {
				if r == "independent-actor-collision" || r == "no-independent-reviewer" {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected independent-actor-collision or no-independent-reviewer, got %v", coded.EvidenceRefs())
			}
			return
		}
		t.Fatalf("unexpected error shape: %v", err)
	}

	// If resolved in async run, verify operation failed with independent-actor-collision
	completedRef := f.registry.Wait(ctx, opRef, 5*time.Second)
	if completedRef.Status != principal.StatusFailed {
		t.Fatalf("expected operation to fail, got %s", completedRef.Status)
	}

	status, err := f.registry.Lookup(f.projectID, opRef)
	if err != nil {
		t.Fatalf("lookup operation: %v", err)
	}
	if status.Error == nil {
		t.Fatal("expected status.Error to be non-nil")
	}
	found := false
	for _, r := range status.Error.EvidenceRefs {
		if r == "independent-actor-collision" || r == "no-independent-reviewer" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected independent-actor-collision, got %v", status.Error.EvidenceRefs)
	}
}

// 7. Test terminal batch persistence: ReviewCompleted, ReviewResult, InvocationProvenance, and ReviewInvocation stored.
func TestReview_TerminalBatchPersistence(t *testing.T) {
	f := setupFixture(t, "proj-terminal-persistence")
	exec, err := reviewexec.New(f.opts)
	if err != nil {
		t.Fatalf("reviewexec.New: %v", err)
	}

	taskID, commit := driveTaskToReviewing(t, f, "T-06", "wp-06", "att-06", defaultWorkerBasis())
	ctx := context.Background()
	caller := principal.CallerContext{PrincipalID: "princ_1", ProjectID: f.projectID}
	meta := principal.CallMeta{ProjectID: f.projectID}
	cand := principal.CandidateRef{TaskID: taskID, AttemptID: "att-06", Commit: commit}

	scriptPassingReview(f.driver)

	opRef, err := exec.Review(ctx, caller, meta, cand, []string{"correctness"})
	if err != nil {
		t.Fatalf("Review failed: %v", err)
	}

	completedRef := f.registry.Wait(ctx, opRef, 5*time.Second)
	if completedRef.Status != principal.StatusCompleted {
		status, _ := f.registry.Lookup(f.projectID, opRef)
		t.Fatalf("operation status %s, error: %+v", completedRef.Status, status.Error)
	}

	// 1. Verify ReviewCompleted event exists in journal
	var reviewCompleted *events.ReviewCompleted
	err = f.harness.Store.Read(ctx, func(tx *storage.Tx) error {
		evs, err := tx.ReadEvents(ctx, storage.EventQuery{ProjectID: f.projectID})
		if err != nil {
			return err
		}
		for _, e := range evs {
			if e.EventType == events.TypeReviewCompleted {
				if p, ok := e.Payload.(*events.ReviewCompleted); ok {
					reviewCompleted = p
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if reviewCompleted == nil {
		t.Fatal("ReviewCompleted event not found")
	}
	if reviewCompleted.Verdict != protocol.VerdictPass {
		t.Fatalf("expected verdict pass, got %s", reviewCompleted.Verdict)
	}

	reviewID := reviewCompleted.ReviewID

	// 2. Verify ReviewResult stored record
	resRecord, err := f.harness.Service.Record(ctx, f.projectID, "ReviewResult", reviewID, 1)
	if err != nil {
		t.Fatalf("ReviewResult record %s not found: %v", reviewID, err)
	}
	var res protocol.ReviewResult
	if err := protocol.Unmarshal([]byte(resRecord.Document), &res); err != nil {
		t.Fatalf("unmarshal ReviewResult: %v", err)
	}
	if res.ReviewID != reviewID || res.Verdict != protocol.VerdictPass {
		t.Fatalf("ReviewResult mismatch: %+v", res)
	}

	// 3. Verify InvocationProvenance stored record
	provRecord, err := f.harness.Service.Record(ctx, f.projectID, "InvocationProvenance", reviewID, 1)
	if err != nil {
		t.Fatalf("InvocationProvenance record %s not found: %v", reviewID, err)
	}
	var prov protocol.InvocationProvenance
	if err := protocol.Unmarshal([]byte(provRecord.Document), &prov); err != nil {
		t.Fatalf("unmarshal InvocationProvenance: %v", err)
	}
	if prov.ProvenanceID != reviewID || prov.Role != protocol.ProvenanceRoleReviewer {
		t.Fatalf("InvocationProvenance mismatch: %+v", prov)
	}

	// 4. Verify ReviewInvocation stored record
	invRecord, err := f.harness.Service.Record(ctx, f.projectID, "ReviewInvocation", reviewID, 1)
	if err != nil {
		t.Fatalf("ReviewInvocation record %s not found: %v", reviewID, err)
	}
	var inv protocol.ReviewInvocation
	if err := protocol.Unmarshal([]byte(invRecord.Document), &inv); err != nil {
		t.Fatalf("unmarshal ReviewInvocation: %v", err)
	}
	if inv.ReviewID != reviewID || inv.Outcome != protocol.OutcomeCompleted {
		t.Fatalf("ReviewInvocation mismatch: %+v", inv)
	}
}

// 8. Test cleanup of rv-* worktrees.
func TestReview_CleanupWorktrees(t *testing.T) {
	f := setupFixture(t, "proj-cleanup-worktrees")

	t.Run("cleans up active review worktree upon completion", func(t *testing.T) {
		exec, err := reviewexec.New(f.opts)
		if err != nil {
			t.Fatalf("reviewexec.New: %v", err)
		}

		taskID, commit := driveTaskToReviewing(t, f, "T-07", "wp-07", "att-07", defaultWorkerBasis())
		ctx := context.Background()
		caller := principal.CallerContext{PrincipalID: "princ_1", ProjectID: f.projectID}
		meta := principal.CallMeta{ProjectID: f.projectID}
		cand := principal.CandidateRef{TaskID: taskID, AttemptID: "att-07", Commit: commit}

		scriptPassingReview(f.driver)

		opRef, err := exec.Review(ctx, caller, meta, cand, []string{"correctness"})
		if err != nil {
			t.Fatalf("Review failed: %v", err)
		}

		completedRef := f.registry.Wait(ctx, opRef, 5*time.Second)
		if completedRef.Status != principal.StatusCompleted {
			t.Fatalf("expected StatusCompleted, got %s", completedRef.Status)
		}

		// Verify worktree list has no active rv-* worktrees
		wts, err := f.worktrees.List(f.projectID)
		if err != nil {
			t.Fatalf("list worktrees: %v", err)
		}
		for _, wt := range wts {
			if strings.HasPrefix(wt.AttemptID, "rv-") && wt.Status == worktrees.StatusActive {
				t.Fatalf("found active review worktree that should have been cleaned up: %+v", wt)
			}
		}
	})

	t.Run("startup removes clean orphan rv-* trees and retains dirty ones", func(t *testing.T) {
		ctx := context.Background()
		repo, err := f.repos.Repository(ctx, f.projectID)
		if err != nil {
			t.Fatalf("get repo: %v", err)
		}

		// Create a clean orphan rv-* worktree
		wtClean, err := f.worktrees.Create(ctx, repo, worktrees.Spec{
			ProjectID:  f.projectID,
			TaskID:     "T-orphan",
			AttemptID:  "rv-clean-orphan",
			BaseCommit: f.gitRepo.Head(),
		})
		if err != nil {
			t.Fatalf("create clean orphan worktree: %v", err)
		}

		// Create a dirty orphan rv-* worktree
		wtDirty, err := f.worktrees.Create(ctx, repo, worktrees.Spec{
			ProjectID:  f.projectID,
			TaskID:     "T-orphan",
			AttemptID:  "rv-dirty-orphan",
			BaseCommit: f.gitRepo.Head(),
		})
		if err != nil {
			t.Fatalf("create dirty orphan worktree: %v", err)
		}

		// Make wtDirty dirty by creating an uncommitted file
		dirtyFile := filepath.Join(wtDirty.Path, "dirty.txt")
		if err := os.WriteFile(dirtyFile, []byte("dirty content"), 0o644); err != nil {
			t.Fatalf("make dirty: %v", err)
		}

		// Start a new Executor: New should clean clean orphan and retain dirty orphan!
		_, err = reviewexec.New(f.opts)
		if err != nil {
			t.Fatalf("reviewexec.New: %v", err)
		}

		// Verify wtClean is removed
		wts, err := f.worktrees.List(f.projectID)
		if err != nil {
			t.Fatalf("list worktrees: %v", err)
		}

		for _, wt := range wts {
			if wt.ID == wtClean.ID && wt.Status == worktrees.StatusActive {
				t.Fatalf("expected wtClean to be removed, got active")
			}
			if wt.ID == wtDirty.ID && wt.Status != worktrees.StatusActive {
				t.Fatalf("expected wtDirty to remain active, got %s", wt.Status)
			}
		}
	})
}

type fakeGuardView struct {
	ps        *protocol.ProjectState
	recordErr error
}

func (v fakeGuardView) ProjectState() *protocol.ProjectState { return v.ps }
func (v fakeGuardView) Record(ctx context.Context, kind, id string, version int) (storage.StoredRecord, error) {
	if v.recordErr != nil {
		return storage.StoredRecord{}, v.recordErr
	}
	return storage.StoredRecord{Document: "{}"}, nil
}

func TestReview_IntentAbsentGuard_TaskNotRunning(t *testing.T) {
	guard := reviewexec.NewIntentAbsentGuardForTesting(
		"tsk_nonexistent",
		"c123",
		"intent_1",
	)
	ps := &protocol.ProjectState{
		Tasks: protocol.TaskBuckets{
			Running: []string{"task-running-1"},
		},
	}
	err := guard.Check(context.Background(), fakeGuardView{
		ps:        ps,
		recordErr: errs.New(errs.CategoryNotFound, "not found"),
	})
	if err == nil {
		t.Fatal("expected error for task not running, got nil")
	}
	if !strings.Contains(err.Error(), "task tsk_nonexistent is not running") {
		t.Fatalf("unexpected error: %v", err)
	}

	// nil ProjectState test
	errNil := guard.Check(context.Background(), fakeGuardView{
		ps:        nil,
		recordErr: errs.New(errs.CategoryNotFound, "not found"),
	})
	if errNil == nil {
		t.Fatal("expected error for nil project state, got nil")
	}

	// task running but intent already recorded
	guardRunning := reviewexec.NewIntentAbsentGuardForTesting(
		"task-running-1",
		"c123",
		"intent_1",
	)
	errExists := guardRunning.Check(context.Background(), fakeGuardView{
		ps:        ps,
		recordErr: nil, // found!
	})
	if errExists == nil {
		t.Fatal("expected error for existing intent, got nil")
	}
	var coded *principal.CodedError
	if !errors.As(errExists, &coded) || coded.Code() != principal.CodePolicyDenied {
		t.Fatalf("expected CodePolicyDenied, got %v", errExists)
	}
}

func TestReview_MaxRequestBytesExceeded(t *testing.T) {
	f := setupFixture(t, "proj-reqbytes")
	f.policy.Grants[0].Limits.MaxRequestBytes = 10

	exec, err := reviewexec.New(f.opts)
	if err != nil {
		t.Fatalf("reviewexec.New: %v", err)
	}

	taskID, commit := driveTaskToReviewing(t, f, "T-reqbytes", "wp-reqbytes", "att-reqbytes", defaultWorkerBasis())
	ctx := context.Background()
	caller := principal.CallerContext{PrincipalID: "princ_1", ProjectID: f.projectID}
	meta := principal.CallMeta{ProjectID: f.projectID}
	cand := principal.CandidateRef{TaskID: taskID, AttemptID: "att-reqbytes", Commit: commit}

	opRef, err := exec.Review(ctx, caller, meta, cand, []string{"correctness"})
	if err != nil {
		t.Fatalf("Review call failed: %v", err)
	}

	completedRef := f.registry.Wait(ctx, opRef, 5*time.Second)
	if completedRef.Status != principal.StatusFailed {
		t.Fatalf("expected review to fail, got %s", completedRef.Status)
	}
}

func TestReview_InvalidExecutionLimits(t *testing.T) {
	f := setupFixture(t, "proj-invlimits")
	f.policy.Grants[0].Limits.MaxTurns = 0

	exec, err := reviewexec.New(f.opts)
	if err != nil {
		t.Fatalf("reviewexec.New: %v", err)
	}

	taskID, commit := driveTaskToReviewing(t, f, "T-invlimits", "wp-invlimits", "att-invlimits", defaultWorkerBasis())
	ctx := context.Background()
	caller := principal.CallerContext{PrincipalID: "princ_1", ProjectID: f.projectID}
	meta := principal.CallMeta{ProjectID: f.projectID}
	cand := principal.CandidateRef{TaskID: taskID, AttemptID: "att-invlimits", Commit: commit}

	opRef, err := exec.Review(ctx, caller, meta, cand, []string{"correctness"})
	if err != nil {
		t.Fatalf("Review call failed: %v", err)
	}

	completedRef := f.registry.Wait(ctx, opRef, 5*time.Second)
	if completedRef.Status != principal.StatusFailed {
		t.Fatalf("expected review to fail, got %s", completedRef.Status)
	}
}
