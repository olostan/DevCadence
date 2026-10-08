package experiments

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// mockSession implements drivers.Session for deterministic experiment testing.
type mockSession struct {
	mu           sync.Mutex
	sessionID    string
	driverID     string
	cfg          drivers.SessionConfig
	status       drivers.SessionStatus
	closed       bool
	closeCount   int
	executeCalls []drivers.TurnInput
	turnHook     func(input drivers.TurnInput, turnIndex int) (drivers.TurnResult, error)
	turnIndex    int
}

func (s *mockSession) ID() string                    { return s.sessionID }
func (s *mockSession) DriverID() string              { return s.driverID }
func (s *mockSession) Config() drivers.SessionConfig { return s.cfg }
func (s *mockSession) Status() drivers.SessionStatus { return s.status }

func (s *mockSession) ExecuteTurn(_ context.Context, input drivers.TurnInput) (drivers.TurnResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.executeCalls = append(s.executeCalls, input)
	idx := s.turnIndex
	s.turnIndex++

	if s.turnHook != nil {
		return s.turnHook(input, idx)
	}

	return drivers.TurnResult{
		TurnID:  input.TurnID,
		Content: "Mock execution successful",
		Usage:   drivers.KnownUsage(100, 0, 20),
	}, nil
}

func (s *mockSession) StreamTurn(_ context.Context, _ drivers.TurnInput) (drivers.EventStream, error) {
	return nil, errs.New(errs.CategoryUnsupported, "streaming not supported in mockSession")
}

func (s *mockSession) Close(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.closeCount++
	s.status = drivers.SessionStatusClosed
	return nil
}

// mockDriver implements drivers.SessionDriver.
type mockDriver struct {
	mu          sync.Mutex
	driverID    string
	startCalls  []drivers.SessionConfig
	sessions    []*mockSession
	turnHook    func(input drivers.TurnInput, turnIndex int) (drivers.TurnResult, error)
	startErr    error
	sessionHook func(s *mockSession)
}

func newMockDriver(id string) *mockDriver {
	return &mockDriver{
		driverID: id,
	}
}

func (d *mockDriver) ID() string {
	if d.driverID != "" {
		return d.driverID
	}
	return "mock-experiment-driver"
}

func (d *mockDriver) Capabilities() drivers.DriverCapabilities {
	return drivers.DriverCapabilities{
		Kind:                  protocol.ChannelDirectHTTPAPI,
		SessionMode:           protocol.SessionStatelessPerCall,
		ContextControl:        protocol.ContextControlExactStateless,
		PrefixCache:           protocol.PrefixCacheNone,
		MaxConcurrentRequests: 1,
	}
}

func (d *mockDriver) StartSession(_ context.Context, cfg drivers.SessionConfig) (drivers.Session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.startErr != nil {
		return nil, d.startErr
	}
	d.startCalls = append(d.startCalls, cfg)
	sess := &mockSession{
		sessionID: cfg.SessionID,
		driverID:  d.ID(),
		cfg:       cfg,
		status:    drivers.SessionStatusActive,
		turnHook:  d.turnHook,
	}
	if d.sessionHook != nil {
		d.sessionHook(sess)
	}
	d.sessions = append(d.sessions, sess)
	return sess, nil
}

func (d *mockDriver) ResumeSession(_ context.Context, _ string, _ drivers.SessionConfig) (drivers.Session, error) {
	return nil, errs.New(errs.CategoryUnsupported, "resume not supported in mockDriver")
}

// TestEnumsValidation verifies CapabilityClass and ContractCompletenessLevel enums (REQ-01, REQ-02).
func TestEnumsValidation(t *testing.T) {
	caps := []CapabilityClass{
		CapabilityLocalSmall,
		CapabilitySubscriptionCLI,
		CapabilityFrontierAPI,
	}
	for _, c := range caps {
		if !c.Valid() {
			t.Errorf("expected capability %q to be valid", c)
		}
	}
	if CapabilityClass("unknown_tier").Valid() {
		t.Errorf("expected unknown capability to be invalid")
	}

	levels := []ContractCompletenessLevel{
		ContractBaseline,
		ContractImplementationReady,
	}
	for _, l := range levels {
		if !l.Valid() {
			t.Errorf("expected contract level %q to be valid", l)
		}
	}
	if ContractCompletenessLevel("custom_level").Valid() {
		t.Errorf("expected custom contract level to be invalid")
	}
}

// TestExperimentSpec_Validate verifies specification validation rules (REQ-03, REQ-10).
func TestExperimentSpec_Validate(t *testing.T) {
	validSpec := ExperimentSpec{
		ExperimentID:     "exp-01",
		TaskID:           "task-wp-m4-2",
		ContractLevel:    ContractImplementationReady,
		TargetCapability: CapabilityLocalSmall,
		Repetitions:      3,
		MaxRepairRounds:  2,
	}
	if err := validSpec.Validate(); err != nil {
		t.Fatalf("expected valid spec to pass, got: %v", err)
	}

	// Invalid Repetitions (< 1)
	invalidRep := validSpec
	invalidRep.Repetitions = 0
	if err := invalidRep.Validate(); err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for repetitions < 1, got %v", err)
	}

	// Invalid MaxRepairRounds (< 0)
	invalidRepair := validSpec
	invalidRepair.MaxRepairRounds = -1
	if err := invalidRepair.Validate(); err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for max_repair_rounds < 0, got %v", err)
	}

	// Invalid TargetCapability
	invalidCap := validSpec
	invalidCap.TargetCapability = "invalid_cap"
	if err := invalidCap.Validate(); err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for invalid capability, got %v", err)
	}

	// Invalid ContractLevel
	invalidLvl := validSpec
	invalidLvl.ContractLevel = "invalid_lvl"
	if err := invalidLvl.Validate(); err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for invalid contract level, got %v", err)
	}
}

// TestExperimentRunner_StopRules verifies stop rules for runner inputs (REQ-10).
func TestExperimentRunner_StopRules(t *testing.T) {
	ctx := context.Background()
	runner := NewExperimentRunner()
	spec := ExperimentSpec{
		ExperimentID:     "exp-stop",
		TaskID:           "task-01",
		ContractLevel:    ContractImplementationReady,
		TargetCapability: CapabilityLocalSmall,
		Repetitions:      2,
		MaxRepairRounds:  1,
	}

	// Driver is nil
	if _, err := runner.Run(ctx, spec, nil); err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for nil driver, got %v", err)
	}

	// Context already canceled
	driver := newMockDriver("drv")
	cancCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := runner.Run(cancCtx, spec, driver); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled when context canceled upfront, got %v", err)
	}
}

// TestACC01_ExperimentRunner_BaselineVsReady tests ACC-01:
// Baseline (incomplete) vs. Ready (complete) specs for CapabilityLocalSmall;
// mock driver configured with defect likelihood on ambiguous contracts.
// Ready contract achieves FirstPassRate > Baseline and AvgRepairRounds < Baseline.
func TestACC01_ExperimentRunner_BaselineVsReady(t *testing.T) {
	ctx := context.Background()
	driver := newMockDriver("drv-acc01")

	// Baseline spec: 3 repetitions, 2 max repairs
	baselineSpec := ExperimentSpec{
		ExperimentID:     "exp-acc01-baseline",
		TaskID:           "task-acc01",
		ContractLevel:    ContractBaseline,
		TargetCapability: CapabilityLocalSmall,
		Repetitions:      3,
		MaxRepairRounds:  2,
	}

	// Ready spec: 3 repetitions, 2 max repairs
	readySpec := ExperimentSpec{
		ExperimentID:     "exp-acc01-ready",
		TaskID:           "task-acc01",
		ContractLevel:    ContractImplementationReady,
		TargetCapability: CapabilityLocalSmall,
		Repetitions:      3,
		MaxRepairRounds:  2,
	}

	// Driver behavior: on baseline, implementation turn (turn 0) has defect.
	// In repair turn (turn 1), it repairs the defect.
	// On ready, implementation turn passes directly.
	driver.turnHook = func(input drivers.TurnInput, turnIndex int) (drivers.TurnResult, error) {
		content := "valid implementation"
		// If input TurnID starts with "impl" and prompt is baseline, simulate defect
		if input.TurnID == "impl-0" || input.TurnID == "impl-1" || input.TurnID == "impl-2" {
			if input.Prompt != "" && input.Prompt[len(input.Prompt)-8:] == "BASELINE" ||
				(input.Prompt != "" && len(input.Prompt) > 10 && input.Prompt[len(input.Prompt)-10:] != "ambiguity.") {
				content = "defect: invariant violation due to ambiguity"
			}
		}
		return drivers.TurnResult{
			TurnID:  input.TurnID,
			Content: content,
			Usage:   drivers.KnownUsage(150, 0, 30),
		}, nil
	}

	runner := NewExperimentRunner()
	// VerificationSuite: fails if content contains "defect:"
	runner.VerificationSuite = VerificationFunc(func(_ context.Context, _ drivers.Session, info VerificationInfo) (bool, error) {
		if info.TurnResult.Content == "defect: invariant violation due to ambiguity" {
			return false, nil
		}
		return true, nil
	})
	// ContractReviewer: reports 1 finding if baseline, 0 if ready
	runner.ContractReviewer = ReviewLensFunc(func(_ context.Context, _ drivers.Session, info VerificationInfo) (int, error) {
		if info.Spec.ContractLevel == ContractBaseline {
			return 1, nil
		}
		return 0, nil
	})

	baselineSummary, err := runner.Run(ctx, baselineSpec, driver)
	if err != nil {
		t.Fatalf("failed baseline run: %v", err)
	}

	readySummary, err := runner.Run(ctx, readySpec, driver)
	if err != nil {
		t.Fatalf("failed ready run: %v", err)
	}

	// Verify ACC-01 outcomes:
	// Ready contract achieves FirstPassRate > Baseline and AvgRepairRounds < Baseline
	if readySummary.FirstPassRate <= baselineSummary.FirstPassRate {
		t.Errorf("ACC-01 failure: expected Ready FirstPassRate (%f) > Baseline (%f)",
			readySummary.FirstPassRate, baselineSummary.FirstPassRate)
	}
	if readySummary.AvgRepairRounds >= baselineSummary.AvgRepairRounds {
		t.Errorf("ACC-01 failure: expected Ready AvgRepairRounds (%f) < Baseline (%f)",
			readySummary.AvgRepairRounds, baselineSummary.AvgRepairRounds)
	}

	// Also verify with EvaluateFalsification: hypothesis should be supported!
	falsResult, err := EvaluateFalsification(baselineSummary, readySummary)
	if err != nil {
		t.Fatalf("EvaluateFalsification failed: %v", err)
	}
	if falsResult.HypothesisFalsified {
		t.Errorf("expected hypothesis supported, got falsified: reason=%s", falsResult.Reason)
	}
	if !falsResult.IsApplicable {
		t.Errorf("expected IsApplicable to be true, got false")
	}
}

// TestACC04_ExperimentRunner_ContextCanceled tests ACC-04:
// Context canceled after 1 repetition in a 5-repetition experiment.
// Cancels promptly, cleans up driver session, returns ctx.Err().
func TestACC04_ExperimentRunner_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	driver := newMockDriver("drv-acc04")

	spec := ExperimentSpec{
		ExperimentID:     "exp-acc04",
		TaskID:           "task-cancel",
		ContractLevel:    ContractImplementationReady,
		TargetCapability: CapabilityLocalSmall,
		Repetitions:      5,
		MaxRepairRounds:  1,
	}

	repCount := 0
	driver.sessionHook = func(s *mockSession) {
		repCount++
		if repCount == 2 {
			// Cancel context as second repetition begins
			cancel()
		}
	}

	runner := NewExperimentRunner()
	_, err := runner.Run(ctx, spec, driver)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled error (ACC-04), got %v", err)
	}

	// Verify all created sessions were cleanly closed
	driver.mu.Lock()
	defer driver.mu.Unlock()
	for i, sess := range driver.sessions {
		sess.mu.Lock()
		closed := sess.closed
		sess.mu.Unlock()
		if !closed {
			t.Errorf("session %d was not cleaned up after cancellation", i)
		}
	}
}

// TestMutationCatalog_Mutant3_FirstPassOffByOne tests Mutant 3:
// "First-pass check off-by-one: marks true if repairRounds <= 1"
// Test fails on run with 1 repair round asserting PassedFirstPass == false.
func TestMutationCatalog_Mutant3_FirstPassOffByOne(t *testing.T) {
	ctx := context.Background()
	driver := newMockDriver("drv-m3")

	spec := ExperimentSpec{
		ExperimentID:     "exp-m3",
		TaskID:           "task-m3",
		ContractLevel:    ContractImplementationReady,
		TargetCapability: CapabilityLocalSmall,
		Repetitions:      1,
		MaxRepairRounds:  2,
	}

	runner := NewExperimentRunner()
	// Turn 0 fails, repair round 1 passes
	runner.VerificationSuite = VerificationFunc(func(_ context.Context, _ drivers.Session, info VerificationInfo) (bool, error) {
		if info.RepairRound == 0 {
			return false, nil // Initial turn fails!
		}
		return true, nil // Repair round 1 passes
	})

	summary, err := runner.Run(ctx, spec, driver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(summary.Runs) != 1 {
		t.Fatalf("expected 1 run result, got %d", len(summary.Runs))
	}

	run := summary.Runs[0]
	if run.TotalRepairRounds != 1 {
		t.Errorf("expected TotalRepairRounds == 1, got %d", run.TotalRepairRounds)
	}
	// Kills Mutant 3: PassedFirstPass MUST be false when repairRounds == 1
	if run.PassedFirstPass {
		t.Fatalf("Mutant 3 alive! PassedFirstPass marked true despite repairRounds == 1")
	}
	if summary.FirstPassRate != 0.0 {
		t.Errorf("expected FirstPassRate == 0.0, got %f", summary.FirstPassRate)
	}
}

// TestMutationCatalog_Mutant4_VerificationGateParity tests Mutant 4 & INV-01:
// "Verification gate lowered for CapabilityLocalSmall"
// Parity of acceptance criteria across tiers.
func TestMutationCatalog_Mutant4_VerificationGateParity(t *testing.T) {
	tiers := []CapabilityClass{
		CapabilityLocalSmall,
		CapabilitySubscriptionCLI,
		CapabilityFrontierAPI,
	}

	for _, tier := range tiers {
		t.Run(string(tier), func(t *testing.T) {
			ctx := context.Background()
			driver := newMockDriver("drv-m4")
			spec := ExperimentSpec{
				ExperimentID:     fmt.Sprintf("exp-m4-%s", tier),
				TaskID:           "task-parity",
				ContractLevel:    ContractImplementationReady,
				TargetCapability: tier,
				Repetitions:      1,
				MaxRepairRounds:  0,
			}

			verifiedCapability := CapabilityClass("")
			runner := NewExperimentRunner()
			runner.VerificationSuite = VerificationFunc(func(_ context.Context, _ drivers.Session, info VerificationInfo) (bool, error) {
				verifiedCapability = info.Spec.TargetCapability
				// Identical verification check: always fails
				return false, nil
			})

			summary, err := runner.Run(ctx, spec, driver)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if verifiedCapability != tier {
				t.Errorf("expected verified capability %s, got %s", tier, verifiedCapability)
			}
			// Gate must not be relaxed: all tiers fail identically
			if summary.Runs[0].PassedFirstPass {
				t.Fatalf("INV-01 violated: gate relaxed for capability tier %s", tier)
			}
			if summary.Runs[0].PrincipalReentryRequired != true {
				t.Fatalf("INV-01 violated: principal reentry not required on failure for tier %s", tier)
			}
		})
	}
}

// TestMutationCatalog_Mutant5_MaxRepairRoundsBounds tests Mutant 5:
// "Max repair rounds not enforced, looping indefinitely"
// Runner loop bounds test fails on max rounds exceeded.
func TestMutationCatalog_Mutant5_MaxRepairRoundsBounds(t *testing.T) {
	ctx := context.Background()
	driver := newMockDriver("drv-m5")

	// Case 1: MaxRepairRounds = 2
	spec := ExperimentSpec{
		ExperimentID:     "exp-m5-rounds",
		TaskID:           "task-m5",
		ContractLevel:    ContractImplementationReady,
		TargetCapability: CapabilityLocalSmall,
		Repetitions:      1,
		MaxRepairRounds:  2,
	}

	runner := NewExperimentRunner()
	// Verification always fails
	repairRoundsSeen := 0
	runner.VerificationSuite = VerificationFunc(func(_ context.Context, _ drivers.Session, info VerificationInfo) (bool, error) {
		if info.RepairRound > 0 {
			repairRoundsSeen++
		}
		return false, nil
	})

	summary, err := runner.Run(ctx, spec, driver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Kills Mutant 5: repair rounds must equal exactly MaxRepairRounds
	if summary.Runs[0].TotalRepairRounds != 2 {
		t.Fatalf("Mutant 5 alive! TotalRepairRounds %d != MaxRepairRounds 2", summary.Runs[0].TotalRepairRounds)
	}
	if repairRoundsSeen != 2 {
		t.Fatalf("Mutant 5 alive! repairRoundsSeen %d != 2", repairRoundsSeen)
	}

	// Case 2: MaxRepairRounds = 0
	specZero := spec
	specZero.MaxRepairRounds = 0
	repairRoundsSeen = 0

	summaryZero, err := runner.Run(ctx, specZero, driver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summaryZero.Runs[0].TotalRepairRounds != 0 {
		t.Fatalf("Mutant 5 alive! TotalRepairRounds %d != 0 when MaxRepairRounds == 0", summaryZero.Runs[0].TotalRepairRounds)
	}
	if repairRoundsSeen != 0 {
		t.Fatalf("Mutant 5 alive! repair rounds executed when MaxRepairRounds == 0")
	}
}

// TestMutationCatalog_Mutant6_SessionCleanupBetweenRepetitions tests Mutant 6:
// "Runner fails to close session between repetitions"
// Verifies that session.Close() is called for each repetition.
func TestMutationCatalog_Mutant6_SessionCleanupBetweenRepetitions(t *testing.T) {
	ctx := context.Background()
	driver := newMockDriver("drv-m6")

	spec := ExperimentSpec{
		ExperimentID:     "exp-m6-cleanup",
		TaskID:           "task-m6",
		ContractLevel:    ContractImplementationReady,
		TargetCapability: CapabilityLocalSmall,
		Repetitions:      3,
		MaxRepairRounds:  1,
	}

	runner := NewExperimentRunner()
	_, err := runner.Run(ctx, spec, driver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	driver.mu.Lock()
	defer driver.mu.Unlock()

	if len(driver.sessions) != 3 {
		t.Fatalf("expected 3 sessions started, got %d", len(driver.sessions))
	}

	// Kills Mutant 6: every session must have been closed
	for i, sess := range driver.sessions {
		sess.mu.Lock()
		closed := sess.closed
		closeCount := sess.closeCount
		sess.mu.Unlock()

		if !closed {
			t.Fatalf("Mutant 6 alive! Session %d was never closed (resource leak)", i)
		}
		if closeCount != 1 {
			t.Errorf("session %d closed %d times, want 1", i, closeCount)
		}
	}
}

// TestExperimentRunner_DriverErrorNonCancellation tests that non-cancellation driver errors
// do not crash the runner and are recorded with Err != nil in the run result (Failure Matrix).
func TestExperimentRunner_DriverErrorNonCancellation(t *testing.T) {
	ctx := context.Background()
	driver := newMockDriver("drv-err")

	spec := ExperimentSpec{
		ExperimentID:     "exp-driver-err",
		TaskID:           "task-err",
		ContractLevel:    ContractImplementationReady,
		TargetCapability: CapabilityLocalSmall,
		Repetitions:      2,
		MaxRepairRounds:  1,
	}

	// Repetition 0 fails with driver error, Repetition 1 succeeds
	driverError := errors.New("simulated model rate limit")
	driver.turnHook = func(input drivers.TurnInput, turnIndex int) (drivers.TurnResult, error) {
		if input.TurnID == "impl-0" {
			return drivers.TurnResult{}, driverError
		}
		return drivers.TurnResult{
			TurnID:  input.TurnID,
			Content: "ok",
			Usage:   drivers.KnownUsage(100, 0, 20),
		}, nil
	}

	runner := NewExperimentRunner()
	summary, err := runner.Run(ctx, spec, driver)
	if err != nil {
		t.Fatalf("runner crashed on non-cancellation driver error: %v", err)
	}

	if len(summary.Runs) != 2 {
		t.Fatalf("expected 2 run results, got %d", len(summary.Runs))
	}

	// Repetition 0 had error
	if !errors.Is(summary.Runs[0].Err, driverError) {
		t.Errorf("expected driver error recorded in run 0, got %v", summary.Runs[0].Err)
	}
	if summary.Runs[0].PassedFirstPass {
		t.Errorf("expected PassedFirstPass to be false on error")
	}
	if !summary.Runs[0].PrincipalReentryRequired {
		t.Errorf("expected PrincipalReentryRequired to be true on error")
	}

	// Repetition 1 succeeded
	if summary.Runs[1].Err != nil {
		t.Errorf("expected nil error on run 1, got %v", summary.Runs[1].Err)
	}
	if !summary.Runs[1].PassedFirstPass {
		t.Errorf("expected PassedFirstPass to be true on run 1")
	}
}

// TestEWPLoader_CustomLoaderAndError tests EWPLoader interface, custom EWPLoaderFunc, and error handling.
func TestEWPLoader_CustomLoaderAndError(t *testing.T) {
	ctx := context.Background()
	spec := ExperimentSpec{
		ExperimentID:     "exp-ewp",
		TaskID:           "task-ewp",
		ContractLevel:    ContractImplementationReady,
		TargetCapability: CapabilityLocalSmall,
		Repetitions:      1,
		MaxRepairRounds:  0,
	}

	// 1. Direct call on EWPLoaderFunc
	customFunc := EWPLoaderFunc(func(_ context.Context, s ExperimentSpec) (string, error) {
		return "custom prompt for " + s.TaskID, nil
	})
	prompt, err := customFunc.LoadEWP(ctx, spec)
	if err != nil || prompt != "custom prompt for task-ewp" {
		t.Fatalf("unexpected prompt or error: prompt=%q, err=%v", prompt, err)
	}

	// 2. Custom loader used by runner
	driver := newMockDriver("drv-ewp")
	runner := NewExperimentRunner()
	runner.EWPLoader = customFunc

	summary, err := runner.Run(ctx, spec, driver)
	if err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}
	if len(driver.sessions) != 1 || len(driver.sessions[0].executeCalls) != 1 {
		t.Fatalf("expected 1 execute call")
	}
	if driver.sessions[0].executeCalls[0].Prompt != "custom prompt for task-ewp" {
		t.Errorf("expected custom prompt passed to turn, got %q", driver.sessions[0].executeCalls[0].Prompt)
	}
	if !summary.Runs[0].PassedFirstPass {
		t.Errorf("expected PassedFirstPass to be true")
	}

	// 3. EWPLoader returns error
	loadErr := errors.New("contract file not found")
	errLoader := EWPLoaderFunc(func(_ context.Context, _ ExperimentSpec) (string, error) {
		return "", loadErr
	})
	runnerErr := NewExperimentRunner()
	runnerErr.EWPLoader = errLoader

	summaryErr, err := runnerErr.Run(ctx, spec, driver)
	if err != nil {
		t.Fatalf("unexpected runner error when EWPLoader fails: %v", err)
	}
	if !errors.Is(summaryErr.Runs[0].Err, loadErr) {
		t.Errorf("expected loadErr recorded in run result, got %v", summaryErr.Runs[0].Err)
	}
	if !summaryErr.Runs[0].PrincipalReentryRequired {
		t.Errorf("expected PrincipalReentryRequired to be true when EWPLoader fails")
	}
}

// TestReviewLenses_MutationReviewerAndErrors tests MutationReviewer findings accumulation and error branches.
func TestReviewLenses_MutationReviewerAndErrors(t *testing.T) {
	ctx := context.Background()
	driver := newMockDriver("drv-lenses")
	spec := ExperimentSpec{
		ExperimentID:     "exp-lenses",
		TaskID:           "task-lenses",
		ContractLevel:    ContractImplementationReady,
		TargetCapability: CapabilityLocalSmall,
		Repetitions:      1,
		MaxRepairRounds:  0,
	}

	// 1. Both ContractReviewer and MutationReviewer contribute findings
	runner := NewExperimentRunner()
	runner.ContractReviewer = ReviewLensFunc(func(_ context.Context, _ drivers.Session, _ VerificationInfo) (int, error) {
		return 2, nil
	})
	runner.MutationReviewer = ReviewLensFunc(func(_ context.Context, _ drivers.Session, _ VerificationInfo) (int, error) {
		return 3, nil
	})

	summary, err := runner.Run(ctx, spec, driver)
	if err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}
	if summary.Runs[0].ArchitecturalFindingsCount != 5 {
		t.Errorf("expected 5 total findings, got %d", summary.Runs[0].ArchitecturalFindingsCount)
	}
	if !summary.Runs[0].PrincipalReentryRequired {
		t.Errorf("expected PrincipalReentryRequired when findings > 0")
	}

	// 2. ContractReviewer returns error
	contractErr := errors.New("contract review failed")
	runnerContractErr := NewExperimentRunner()
	runnerContractErr.ContractReviewer = ReviewLensFunc(func(_ context.Context, _ drivers.Session, _ VerificationInfo) (int, error) {
		return 0, contractErr
	})
	summaryCErr, err := runnerContractErr.Run(ctx, spec, driver)
	if err != nil {
		t.Fatalf("unexpected runner error: %v", err)
	}
	if !errors.Is(summaryCErr.Runs[0].Err, contractErr) {
		t.Errorf("expected contractErr recorded in run result, got %v", summaryCErr.Runs[0].Err)
	}
	if !summaryCErr.Runs[0].PrincipalReentryRequired {
		t.Errorf("expected PrincipalReentryRequired on contract review error")
	}

	// 3. MutationReviewer returns error
	mutationErr := errors.New("mutation review failed")
	runnerMutationErr := NewExperimentRunner()
	runnerMutationErr.MutationReviewer = ReviewLensFunc(func(_ context.Context, _ drivers.Session, _ VerificationInfo) (int, error) {
		return 0, mutationErr
	})
	summaryMErr, err := runnerMutationErr.Run(ctx, spec, driver)
	if err != nil {
		t.Fatalf("unexpected runner error: %v", err)
	}
	if !errors.Is(summaryMErr.Runs[0].Err, mutationErr) {
		t.Errorf("expected mutationErr recorded in run result, got %v", summaryMErr.Runs[0].Err)
	}
	if !summaryMErr.Runs[0].PrincipalReentryRequired {
		t.Errorf("expected PrincipalReentryRequired on mutation review error")
	}
}

// TestRunRepetition_VerificationSuiteErrors tests verification suite errors on initial turn and repair turns.
func TestRunRepetition_VerificationSuiteErrors(t *testing.T) {
	ctx := context.Background()
	driver := newMockDriver("drv-verr")
	spec := ExperimentSpec{
		ExperimentID:     "exp-verr",
		TaskID:           "task-verr",
		ContractLevel:    ContractImplementationReady,
		TargetCapability: CapabilityLocalSmall,
		Repetitions:      1,
		MaxRepairRounds:  2,
	}

	// 1. Initial turn verification returns error
	initErr := errors.New("initial verification failed with system error")
	runnerInit := NewExperimentRunner()
	runnerInit.VerificationSuite = VerificationFunc(func(_ context.Context, _ drivers.Session, info VerificationInfo) (bool, error) {
		if info.RepairRound == 0 {
			return false, initErr
		}
		return true, nil
	})

	summaryInit, err := runnerInit.Run(ctx, spec, driver)
	if err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}
	if !errors.Is(summaryInit.Runs[0].Err, initErr) {
		t.Errorf("expected initErr recorded in run result, got %v", summaryInit.Runs[0].Err)
	}
	if !summaryInit.Runs[0].PrincipalReentryRequired {
		t.Errorf("expected PrincipalReentryRequired to be true")
	}

	// 2. Repair turn verification returns error
	repairVErr := errors.New("repair verification test harness crashed")
	runnerRepair := NewExperimentRunner()
	runnerRepair.VerificationSuite = VerificationFunc(func(_ context.Context, _ drivers.Session, info VerificationInfo) (bool, error) {
		if info.RepairRound == 0 {
			return false, nil // Triggers repair round 1
		}
		return false, repairVErr
	})

	summaryRepair, err := runnerRepair.Run(ctx, spec, driver)
	if err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}
	if !errors.Is(summaryRepair.Runs[0].Err, repairVErr) {
		t.Errorf("expected repairVErr recorded in run result, got %v", summaryRepair.Runs[0].Err)
	}
	if summaryRepair.Runs[0].TotalRepairRounds != 1 {
		t.Errorf("expected TotalRepairRounds == 1, got %d", summaryRepair.Runs[0].TotalRepairRounds)
	}
}

// TestRunRepetition_RepairTurnExecutionError tests session turn execution errors during repair rounds.
func TestRunRepetition_RepairTurnExecutionError(t *testing.T) {
	ctx := context.Background()
	driver := newMockDriver("drv-reperr")
	spec := ExperimentSpec{
		ExperimentID:     "exp-reperr",
		TaskID:           "task-reperr",
		ContractLevel:    ContractImplementationReady,
		TargetCapability: CapabilityLocalSmall,
		Repetitions:      1,
		MaxRepairRounds:  2,
	}

	repairDriverErr := errors.New("model timeout on repair turn")
	driver.turnHook = func(input drivers.TurnInput, turnIndex int) (drivers.TurnResult, error) {
		if turnIndex == 0 {
			return drivers.TurnResult{
				TurnID:  input.TurnID,
				Content: "initial attempt",
			}, nil
		}
		return drivers.TurnResult{}, repairDriverErr
	}

	runner := NewExperimentRunner()
	runner.VerificationSuite = VerificationFunc(func(_ context.Context, _ drivers.Session, info VerificationInfo) (bool, error) {
		return false, nil // Always fails to trigger repair
	})

	summary, err := runner.Run(ctx, spec, driver)
	if err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}
	if !errors.Is(summary.Runs[0].Err, repairDriverErr) {
		t.Errorf("expected repairDriverErr recorded in run result, got %v", summary.Runs[0].Err)
	}
	if summary.Runs[0].TotalRepairRounds != 1 {
		t.Errorf("expected TotalRepairRounds == 1, got %d", summary.Runs[0].TotalRepairRounds)
	}
}

// TestRunRepetition_StartSessionError tests driver.StartSession returning error.
func TestRunRepetition_StartSessionError(t *testing.T) {
	ctx := context.Background()
	driver := newMockDriver("drv-starterr")
	startErr := errors.New("cannot allocate driver session")
	driver.startErr = startErr

	spec := ExperimentSpec{
		ExperimentID:     "exp-start",
		TaskID:           "task-start",
		ContractLevel:    ContractImplementationReady,
		TargetCapability: CapabilityLocalSmall,
		Repetitions:      1,
		MaxRepairRounds:  0,
	}

	runner := NewExperimentRunner()
	summary, err := runner.Run(ctx, spec, driver)
	if err != nil {
		t.Fatalf("unexpected runner error: %v", err)
	}
	if !errors.Is(summary.Runs[0].Err, startErr) {
		t.Errorf("expected startErr recorded, got %v", summary.Runs[0].Err)
	}
	if !summary.Runs[0].PrincipalReentryRequired {
		t.Errorf("expected PrincipalReentryRequired to be true")
	}
}

// TestRunRepetition_EmptyExperimentID verifies fallback RunID format when ExperimentID is empty.
func TestRunRepetition_EmptyExperimentID(t *testing.T) {
	ctx := context.Background()
	driver := newMockDriver("drv-emptyid")
	spec := ExperimentSpec{
		ExperimentID:     "",
		TaskID:           "task-fallback",
		ContractLevel:    ContractImplementationReady,
		TargetCapability: CapabilityLocalSmall,
		Repetitions:      1,
		MaxRepairRounds:  0,
	}

	runner := NewExperimentRunner()
	summary, err := runner.Run(ctx, spec, driver)
	if err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}
	expectedID := "run-local_small-0"
	if summary.Runs[0].RunID != expectedID {
		t.Errorf("expected RunID %q, got %q", expectedID, summary.Runs[0].RunID)
	}
}

// TestRunRepetition_ContextCanceledDuringTurn verifies prompt return of context.Canceled
// when context is canceled during turn execution or verification.
func TestRunRepetition_ContextCanceledDuringTurn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	driver := newMockDriver("drv-canc-turn")
	spec := ExperimentSpec{
		ExperimentID:     "exp-canc-turn",
		TaskID:           "task-canc-turn",
		ContractLevel:    ContractImplementationReady,
		TargetCapability: CapabilityLocalSmall,
		Repetitions:      1,
		MaxRepairRounds:  1,
	}

	driver.turnHook = func(input drivers.TurnInput, turnIndex int) (drivers.TurnResult, error) {
		cancel() // Cancel while turn is executing
		return drivers.TurnResult{}, context.Canceled
	}

	runner := NewExperimentRunner()
	_, err := runner.Run(ctx, spec, driver)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
