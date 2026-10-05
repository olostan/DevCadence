package campaign

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/benchmark"
	"github.com/olostan/DevCadence/internal/benchmark/corpus"
	"github.com/olostan/DevCadence/internal/benchmark/experiments"
	"github.com/olostan/DevCadence/internal/benchmark/telemetry"
	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// mockSession implements drivers.Session for campaign tests.
type mockSession struct {
	mu        sync.Mutex
	sessionID string
	turnIndex int
	turnHook  func(input drivers.TurnInput, turnIndex int) (drivers.TurnResult, error)
	onClose   func()
}

func (s *mockSession) ID() string       { return s.sessionID }
func (s *mockSession) DriverID() string { return "mock-campaign-driver" }
func (s *mockSession) Config() drivers.SessionConfig {
	return drivers.SessionConfig{SessionID: s.sessionID}
}
func (s *mockSession) Status() drivers.SessionStatus { return drivers.SessionStatusActive }

func (s *mockSession) ExecuteTurn(_ context.Context, input drivers.TurnInput) (drivers.TurnResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.turnIndex
	s.turnIndex++

	if s.turnHook != nil {
		return s.turnHook(input, idx)
	}

	return drivers.TurnResult{
		TurnID:  input.TurnID,
		Content: "Task executed successfully. Task completed.",
		Usage: drivers.TokenUsage{
			InputTokens:  150,
			OutputTokens: 35,
			CachedTokens: 20,
		},
	}, nil
}

func (s *mockSession) StreamTurn(_ context.Context, _ drivers.TurnInput) (drivers.EventStream, error) {
	return nil, errs.New(errs.CategoryUnsupported, "streaming not supported in mockSession")
}

func (s *mockSession) Close(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.onClose != nil {
		s.onClose()
		s.onClose = nil
	}
	return nil
}

// mockDriver implements drivers.SessionDriver.
type mockDriver struct {
	mu           sync.Mutex
	id           string
	startErr     error
	turnHook     func(input drivers.TurnInput, turnIndex int) (drivers.TurnResult, error)
	startHook    func(cfg drivers.SessionConfig) error
	sessionDelay time.Duration
	activeCount  *int64
	peakCount    *int64
}

func newMockDriver(id string) *mockDriver {
	return &mockDriver{id: id}
}

func (d *mockDriver) ID() string {
	if d.id != "" {
		return d.id
	}
	return "mock-driver"
}

func (d *mockDriver) Capabilities() drivers.DriverCapabilities {
	return drivers.DriverCapabilities{
		Kind:                  protocol.ChannelDirectHTTPAPI,
		SessionMode:           protocol.SessionStatelessPerCall,
		ContextControl:        protocol.ContextControlExactStateless,
		PrefixCache:           protocol.PrefixCacheNone,
		MaxConcurrentRequests: 10,
	}
}

func (d *mockDriver) StartSession(_ context.Context, cfg drivers.SessionConfig) (drivers.Session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.startErr != nil {
		return nil, d.startErr
	}
	if d.startHook != nil {
		if err := d.startHook(cfg); err != nil {
			return nil, err
		}
	}

	if d.activeCount != nil {
		curr := atomic.AddInt64(d.activeCount, 1)
		for {
			peak := atomic.LoadInt64(d.peakCount)
			if curr <= peak || atomic.CompareAndSwapInt64(d.peakCount, peak, curr) {
				break
			}
		}
	}

	if d.sessionDelay > 0 {
		time.Sleep(d.sessionDelay)
	}

	sess := &mockSession{
		sessionID: cfg.SessionID,
		turnHook:  d.turnHook,
		onClose: func() {
			if d.activeCount != nil {
				atomic.AddInt64(d.activeCount, -1)
			}
		},
	}
	return sess, nil
}

func (d *mockDriver) ResumeSession(_ context.Context, _ string, _ drivers.SessionConfig) (drivers.Session, error) {
	return nil, errs.New(errs.CategoryUnsupported, "resume not supported")
}

func createTestCorpus(t *testing.T) (*corpus.CorpusRegistry, []benchmark.BenchmarkTask) {
	t.Helper()
	reg := corpus.NewCorpusRegistry()
	t1 := benchmark.BenchmarkTask{
		TaskID:        "task-bench-01",
		Name:          "Task 01",
		WorkPackageID: "WP-M4-5",
		WorkloadKind:  protocol.WorkloadImplementation,
		Contract:      "Fix bug in algorithm contract",
	}
	t2 := benchmark.BenchmarkTask{
		TaskID:        "task-bench-02",
		Name:          "Task 02",
		WorkPackageID: "WP-M4-5",
		WorkloadKind:  protocol.WorkloadReview,
		Contract:      "Implement greenfield feature contract",
	}

	if err := reg.RegisterTask(t1); err != nil {
		t.Fatalf("register task1: %v", err)
	}
	if err := reg.RegisterTask(t2); err != nil {
		t.Fatalf("register task2: %v", err)
	}

	d1 := *benchmark.FixtureAPIMutation()
	if err := reg.AssociateDefect(t1.TaskID, d1); err != nil {
		t.Fatalf("associate defect: %v", err)
	}

	return reg, []benchmark.BenchmarkTask{t1, t2}
}

// TestCampaign_ACC01_FullMatrixExecution validates full matrix execution covering all 4 strategies
// and capability classes (ACC-01, REQ-01, REQ-04, REQ-06).
func TestCampaign_ACC01_FullMatrixExecution(t *testing.T) {
	reg, tasks := createTestCorpus(t)
	collector := telemetry.NewTelemetryCollector()

	dp := DriverProviderFunc(func(cap experiments.CapabilityClass) (drivers.SessionDriver, error) {
		return newMockDriver(string(cap)), nil
	})

	runner := NewCampaignRunner(dp, collector)

	spec := CampaignSpec{
		CampaignID: "camp-acc-01",
		Corpus:     reg,
		Tasks:      tasks,
		Strategies: []benchmark.ContextStrategyKind{
			StrategyFullHistory,
			StrategyCompacted,
			StrategySnippetPool,
			StrategyHybrid4Layer,
		},
		Capabilities: []experiments.CapabilityClass{
			CapabilityLocalSmall,
			CapabilitySubscriptionCLI,
			CapabilityFrontierAPI,
		},
		Repetitions:      1,
		MaxRepairRounds:  0,
		ConcurrencyLimit: 4,
	}

	summary, err := runner.Execute(context.Background(), spec)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if summary.Status != CampaignStatusCompleted {
		t.Errorf("expected status %s, got %s", CampaignStatusCompleted, summary.Status)
	}

	// 2 tasks: task1 has 1 defect (+clean = 2), task2 has 0 defects (+clean = 1).
	// Total per cap per strat: (2 + 1) * 4 = 12.
	// For 3 caps: 12 * 3 = 36 runs.
	expectedRuns := 36
	if summary.TotalRuns != expectedRuns {
		t.Errorf("expected TotalRuns=%d, got %d", expectedRuns, summary.TotalRuns)
	}
	if summary.CompletedRuns != expectedRuns {
		t.Errorf("expected CompletedRuns=%d, got %d", expectedRuns, summary.CompletedRuns)
	}
	if summary.FailedRuns != 0 {
		t.Errorf("expected 0 failed runs, got %d", summary.FailedRuns)
	}

	// Snapshots recorded in collector matches summary
	snaps := collector.GetSnapshots()
	if len(snaps) != expectedRuns {
		t.Errorf("expected %d snapshots in collector, got %d", expectedRuns, len(snaps))
	}
	if len(summary.Snapshots) != expectedRuns {
		t.Errorf("expected %d snapshots in summary, got %d", expectedRuns, len(summary.Snapshots))
	}

	// Verify all 4 strategies and 3 capability tiers appear in snapshots
	strategySeen := make(map[string]bool)
	capabilitySeen := make(map[string]bool)
	for _, s := range snaps {
		strategySeen[s.Strategy] = true
		capabilitySeen[s.Capability] = true
		if s.CumulativeInputTokens <= 0 {
			t.Errorf("snapshot %s had 0 cumulative input tokens", s.RunID)
		}
	}
	if len(strategySeen) != 4 {
		t.Errorf("expected 4 strategies seen, got %d", len(strategySeen))
	}
	if len(capabilitySeen) != 3 {
		t.Errorf("expected 3 capabilities seen, got %d", len(capabilitySeen))
	}
}

// TestCampaign_ACC02_ConcurrencyBounds validates that worker concurrency never exceeds
// MaxConcurrency / ConcurrencyLimit (ACC-02, M-01).
func TestCampaign_ACC02_ConcurrencyBounds(t *testing.T) {
	reg, tasks := createTestCorpus(t)
	collector := telemetry.NewTelemetryCollector()

	var activeWorkers int64
	var peakWorkers int64

	dp := DriverProviderFunc(func(cap experiments.CapabilityClass) (drivers.SessionDriver, error) {
		drv := newMockDriver(string(cap))
		drv.activeCount = &activeWorkers
		drv.peakCount = &peakWorkers
		drv.sessionDelay = 15 * time.Millisecond
		return drv, nil
	})

	runner := NewCampaignRunner(dp, collector)

	const limit = 2
	spec := CampaignSpec{
		CampaignID:       "camp-acc-02-concurrency",
		Corpus:           reg,
		Tasks:            tasks,
		Strategies:       []benchmark.ContextStrategyKind{StrategyFullHistory, StrategyCompacted},
		Capabilities:     []experiments.CapabilityClass{CapabilityLocalSmall, CapabilityFrontierAPI},
		Repetitions:      1,
		MaxRepairRounds:  0,
		ConcurrencyLimit: limit,
	}

	summary, err := runner.Execute(context.Background(), spec)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if summary.CompletedRuns == 0 {
		t.Fatalf("no completed runs recorded")
	}

	peak := atomic.LoadInt64(&peakWorkers)
	if peak > limit {
		t.Errorf("concurrency violated: peak workers was %d, exceeding limit %d", peak, limit)
	}
	if peak < 1 {
		t.Errorf("expected at least 1 active worker, got %d", peak)
	}
}

// TestCampaign_ACC02_DelegationExperiment validates that delegation-floor experiments are executed
// and FalsificationResults / Tasks are populated with canonical composite keys `task:cap` (ACC-02, REQ-03, REQ-07, M-03).
func TestCampaign_ACC02_DelegationExperiment(t *testing.T) {
	reg, tasks := createTestCorpus(t)
	collector := telemetry.NewTelemetryCollector()

	dp := DriverProviderFunc(func(cap experiments.CapabilityClass) (drivers.SessionDriver, error) {
		return newMockDriver(string(cap)), nil
	})

	runner := NewCampaignRunner(dp, collector)

	caps := []experiments.CapabilityClass{
		CapabilityLocalSmall,
		CapabilitySubscriptionCLI,
		CapabilityFrontierAPI,
	}

	spec := CampaignSpec{
		CampaignID:       "camp-acc-02-delegation",
		Corpus:           reg,
		Tasks:            tasks, // 2 tasks
		Strategies:       []benchmark.ContextStrategyKind{StrategyFullHistory},
		Capabilities:     caps, // 3 capabilities
		Repetitions:      1,
		MaxRepairRounds:  0,
		ConcurrencyLimit: 4,
	}

	summary, err := runner.Execute(context.Background(), spec)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	expectedPermutations := len(tasks) * len(caps) // 2 * 3 = 6
	if len(summary.FalsificationResults) != expectedPermutations {
		t.Errorf("expected %d FalsificationResults, got %d", expectedPermutations, len(summary.FalsificationResults))
	}
	if len(summary.DelegationSummaries) != expectedPermutations {
		t.Errorf("expected %d DelegationSummaries, got %d", expectedPermutations, len(summary.DelegationSummaries))
	}
	if len(summary.Tasks) != expectedPermutations {
		t.Errorf("expected %d Tasks summaries, got %d", expectedPermutations, len(summary.Tasks))
	}

	// Verify every composite key `task.TaskID:cap` exists
	for _, task := range tasks {
		for _, cap := range caps {
			key := fmt.Sprintf("%s:%s", task.TaskID, cap)
			if _, ok := summary.FalsificationResults[key]; !ok {
				t.Errorf("missing key %q in FalsificationResults", key)
			}
			if _, ok := summary.DelegationSummaries[key]; !ok {
				t.Errorf("missing key %q in DelegationSummaries", key)
			}
			if _, ok := summary.Tasks[key]; !ok {
				t.Errorf("missing key %q in Tasks", key)
			}
		}
	}
}

// TestCampaign_ACC03_DefectCatchRate validates defect catch rate calculation matching
// seeded defect detection across matrix cells (ACC-03, M-05).
func TestCampaign_ACC03_DefectCatchRate(t *testing.T) {
	reg, tasks := createTestCorpus(t)
	collector := telemetry.NewTelemetryCollector()

	dp := DriverProviderFunc(func(cap experiments.CapabilityClass) (drivers.SessionDriver, error) {
		return newMockDriver(string(cap)), nil
	})

	runner := NewCampaignRunner(dp, collector)

	// Configure benchmark runner with a verification suite that detects seeded defects
	br := benchmark.NewBenchmarkRunner()
	br.VerificationSuite = &benchmark.DefaultVerificationSuite{
		CompilerValidator: func(ctx context.Context, session *benchmark.BenchmarkSession) (bool, error) {
			if session.Defect != nil {
				return true, nil // Compiler rejected defect upfront -> DefectPrevented / DefectDetected
			}
			return false, nil
		},
		TestRunner: func(ctx context.Context, session *benchmark.BenchmarkSession) (bool, error) {
			return true, nil
		},
	}
	runner.SetBenchmarkRunner(br)

	spec := CampaignSpec{
		CampaignID:       "camp-acc-03-catchrate",
		Corpus:           reg,
		Tasks:            tasks, // task1 has 1 defect, task2 has 0 defects
		Strategies:       []benchmark.ContextStrategyKind{StrategyFullHistory},
		Capabilities:     []experiments.CapabilityClass{CapabilityLocalSmall},
		Repetitions:      1,
		MaxRepairRounds:  0,
		ConcurrencyLimit: 1,
	}

	summary, err := runner.Execute(context.Background(), spec)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Total runs:
	// task1: clean + 1 defect = 2 runs (1 seeded)
	// task2: clean = 1 run (0 seeded)
	// Total runs = 3. Total seeded runs = 1.
	// Since compiler rejected defect upfront, 1 defect was detected/prevented out of 1 seeded run.
	// DefectCatchRate MUST be 1.0 (1/1), NOT 1/3 (0.333) (M-05).
	if !summary.DefectCatchRateApplicable {
		t.Errorf("expected DefectCatchRateApplicable = true")
	}
	if math.Abs(summary.DefectCatchRate-1.0) > 1e-6 {
		t.Errorf("expected DefectCatchRate=1.0, got %f", summary.DefectCatchRate)
	}
}

// TestCampaign_ACC03_DriverFailureIsolation validates that driver unavailability for one tier
// records failed runs in telemetry without aborting or crashing the campaign (ACC-03, REQ-08, M-02).
func TestCampaign_ACC03_DriverFailureIsolation(t *testing.T) {
	reg, tasks := createTestCorpus(t)
	collector := telemetry.NewTelemetryCollector()

	dp := DriverProviderFunc(func(cap experiments.CapabilityClass) (drivers.SessionDriver, error) {
		if cap == CapabilityLocalSmall {
			return nil, errs.New(errs.CategoryModelUnavailable, "local_small endpoint offline")
		}
		return newMockDriver(string(cap)), nil
	})

	runner := NewCampaignRunner(dp, collector)

	spec := CampaignSpec{
		CampaignID:       "camp-acc-03-isolation",
		Corpus:           reg,
		Tasks:            tasks,
		Strategies:       []benchmark.ContextStrategyKind{StrategyFullHistory},
		Capabilities:     []experiments.CapabilityClass{CapabilityLocalSmall, CapabilityFrontierAPI},
		Repetitions:      1,
		MaxRepairRounds:  0,
		ConcurrencyLimit: 2,
	}

	summary, err := runner.Execute(context.Background(), spec)
	if err != nil {
		t.Fatalf("Execute should not abort with error, got: %v", err)
	}

	if summary.FailedRuns == 0 {
		t.Errorf("expected failed runs for unavailable capability, got 0")
	}
	if summary.CompletedRuns == 0 {
		t.Errorf("expected completed runs for available capability, got 0")
	}

	// Verify snapshots recorded for failed runs have Accepted: false and DefectNotApplicable
	snaps := collector.GetSnapshots()
	foundFailed := false
	for _, s := range snaps {
		if s.Capability == string(CapabilityLocalSmall) {
			foundFailed = true
			if s.Accepted {
				t.Errorf("expected failed run to have Accepted=false")
			}
			if s.DefectStatus != string(benchmark.DefectNotApplicable) {
				t.Errorf("expected failed run to have DefectNotApplicable, got %s", s.DefectStatus)
			}
		}
	}
	if !foundFailed {
		t.Errorf("no failure snapshots found for failed capability")
	}
}

// TestCampaign_ACC04_CancellationHandling validates that context cancellation halts execution,
// returns a partial summary with SummaryStatusCancelled, and matches context.Canceled (ACC-04, REQ-08, M-04).
func TestCampaign_ACC04_CancellationHandling(t *testing.T) {
	reg, tasks := createTestCorpus(t)
	collector := telemetry.NewTelemetryCollector()

	ctx, cancel := context.WithCancel(context.Background())

	dp := DriverProviderFunc(func(cap experiments.CapabilityClass) (drivers.SessionDriver, error) {
		drv := newMockDriver(string(cap))
		drv.turnHook = func(input drivers.TurnInput, turnIndex int) (drivers.TurnResult, error) {
			cancel() // cancel context on first turn
			return drivers.TurnResult{
				TurnID:  input.TurnID,
				Content: "First turn executed",
			}, nil
		}
		return drv, nil
	})

	runner := NewCampaignRunner(dp, collector)

	spec := CampaignSpec{
		CampaignID:       "camp-acc-04-cancellation",
		Corpus:           reg,
		Tasks:            tasks,
		Strategies:       []benchmark.ContextStrategyKind{StrategyFullHistory, StrategyCompacted},
		Capabilities:     []experiments.CapabilityClass{CapabilityLocalSmall, CapabilityFrontierAPI},
		Repetitions:      2,
		MaxRepairRounds:  0,
		ConcurrencyLimit: 1,
	}

	summary, err := runner.Execute(ctx, spec)
	if summary == nil {
		t.Fatalf("summary must not be nil on context cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got %v", err)
	}
	if summary.Status != SummaryStatusCancelled {
		t.Errorf("expected status %s, got %s", SummaryStatusCancelled, summary.Status)
	}
}

// TestCampaign_ACC05_RaceDetectorClean validates thread-safe execution across high concurrency (ACC-05, M-06).
func TestCampaign_ACC05_RaceDetectorClean(t *testing.T) {
	reg, tasks := createTestCorpus(t)
	collector := telemetry.NewTelemetryCollector()

	dp := DriverProviderFunc(func(cap experiments.CapabilityClass) (drivers.SessionDriver, error) {
		return newMockDriver(string(cap)), nil
	})

	runner := NewCampaignRunner(dp, collector)

	spec := CampaignSpec{
		CampaignID: "camp-acc-05-race",
		Corpus:     reg,
		Tasks:      tasks,
		Strategies: []benchmark.ContextStrategyKind{
			StrategyFullHistory,
			StrategyCompacted,
		},
		Capabilities: []experiments.CapabilityClass{
			CapabilityLocalSmall,
			CapabilitySubscriptionCLI,
			CapabilityFrontierAPI,
		},
		Repetitions:      1,
		MaxRepairRounds:  0,
		ConcurrencyLimit: 8, // high concurrency to stress race detector
	}

	summary, err := runner.Execute(context.Background(), spec)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if summary.CompletedRuns == 0 {
		t.Errorf("expected completed runs > 0")
	}
}

// TestCampaign_Mutant_M01_BoundedConcurrency tests M-01.
func TestCampaign_Mutant_M01_BoundedConcurrency(t *testing.T) {
	reg, tasks := createTestCorpus(t)
	collector := telemetry.NewTelemetryCollector()

	var activeCount int64
	var peakCount int64

	dp := DriverProviderFunc(func(cap experiments.CapabilityClass) (drivers.SessionDriver, error) {
		drv := newMockDriver(string(cap))
		drv.activeCount = &activeCount
		drv.peakCount = &peakCount
		drv.sessionDelay = 10 * time.Millisecond
		return drv, nil
	})

	runner := NewCampaignRunner(dp, collector)

	const maxConc = 1
	spec := CampaignSpec{
		CampaignID:       "camp-m01",
		Corpus:           reg,
		Tasks:            tasks,
		Strategies:       []benchmark.ContextStrategyKind{StrategyFullHistory, StrategyCompacted, StrategySnippetPool},
		Capabilities:     []experiments.CapabilityClass{CapabilityLocalSmall},
		Repetitions:      1,
		MaxRepairRounds:  0,
		ConcurrencyLimit: maxConc,
	}

	summary, err := runner.Execute(context.Background(), spec)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if summary.CompletedRuns == 0 {
		t.Fatalf("no runs completed")
	}

	if atomic.LoadInt64(&peakCount) > int64(maxConc) {
		t.Fatalf("M-01 mutant detected: unbounded goroutines spawned! Peak was %d, max allowed %d", peakCount, maxConc)
	}
}

// TestCampaign_Mutant_M02_CellFailureIsolation tests M-02.
func TestCampaign_Mutant_M02_CellFailureIsolation(t *testing.T) {
	reg, tasks := createTestCorpus(t)
	collector := telemetry.NewTelemetryCollector()

	dp := DriverProviderFunc(func(cap experiments.CapabilityClass) (drivers.SessionDriver, error) {
		drv := newMockDriver(string(cap))
		if cap == CapabilityLocalSmall {
			drv.startErr = errors.New("simulated cell failure")
		}
		return drv, nil
	})

	runner := NewCampaignRunner(dp, collector)

	spec := CampaignSpec{
		CampaignID:       "camp-m02",
		Corpus:           reg,
		Tasks:            tasks,
		Strategies:       []benchmark.ContextStrategyKind{StrategyFullHistory},
		Capabilities:     []experiments.CapabilityClass{CapabilityLocalSmall, CapabilityFrontierAPI},
		Repetitions:      1,
		MaxRepairRounds:  0,
		ConcurrencyLimit: 2,
	}

	summary, err := runner.Execute(context.Background(), spec)
	if err != nil {
		t.Fatalf("M-02 mutant detected: cell failure terminated campaign prematurely with error: %v", err)
	}
	if summary.CompletedRuns == 0 {
		t.Fatalf("M-02 mutant detected: zero completed runs due to premature termination")
	}
}

// TestCampaign_Mutant_M03_CompositeKeyNoCollision tests M-03.
func TestCampaign_Mutant_M03_CompositeKeyNoCollision(t *testing.T) {
	reg, tasks := createTestCorpus(t)
	collector := telemetry.NewTelemetryCollector()

	dp := DriverProviderFunc(func(cap experiments.CapabilityClass) (drivers.SessionDriver, error) {
		return newMockDriver(string(cap)), nil
	})

	runner := NewCampaignRunner(dp, collector)

	caps := []experiments.CapabilityClass{
		CapabilityLocalSmall,
		CapabilityFrontierAPI,
	}

	spec := CampaignSpec{
		CampaignID:       "camp-m03",
		Corpus:           reg,
		Tasks:            tasks,
		Strategies:       []benchmark.ContextStrategyKind{StrategyFullHistory},
		Capabilities:     caps,
		Repetitions:      1,
		MaxRepairRounds:  0,
		ConcurrencyLimit: 2,
	}

	summary, err := runner.Execute(context.Background(), spec)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	expectedKeys := len(tasks) * len(caps) // 2 * 2 = 4
	if len(summary.FalsificationResults) != expectedKeys {
		t.Fatalf("M-03 mutant detected: map key collision! Expected %d keys, got %d", expectedKeys, len(summary.FalsificationResults))
	}
}

// TestCampaign_Mutant_M04_CancellationReturnsPartialSummary tests M-04.
func TestCampaign_Mutant_M04_CancellationReturnsPartialSummary(t *testing.T) {
	reg, tasks := createTestCorpus(t)
	collector := telemetry.NewTelemetryCollector()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-canceled context

	dp := DriverProviderFunc(func(cap experiments.CapabilityClass) (drivers.SessionDriver, error) {
		return newMockDriver(string(cap)), nil
	})

	runner := NewCampaignRunner(dp, collector)

	spec := CampaignSpec{
		CampaignID:       "camp-m04",
		Corpus:           reg,
		Tasks:            tasks,
		Strategies:       []benchmark.ContextStrategyKind{StrategyFullHistory},
		Capabilities:     []experiments.CapabilityClass{CapabilityLocalSmall},
		Repetitions:      1,
		MaxRepairRounds:  0,
		ConcurrencyLimit: 1,
	}

	summary, err := runner.Execute(ctx, spec)
	if summary == nil {
		t.Fatalf("M-04 mutant detected: context cancellation returned nil summary")
	}
	if summary.Status != SummaryStatusCancelled {
		t.Fatalf("M-04 mutant detected: summary status was %s instead of cancelled", summary.Status)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// TestCampaign_Mutant_M05_DefectCatchRateCalculation tests M-05.
func TestCampaign_Mutant_M05_DefectCatchRateCalculation(t *testing.T) {
	reg, tasks := createTestCorpus(t)
	collector := telemetry.NewTelemetryCollector()

	dp := DriverProviderFunc(func(cap experiments.CapabilityClass) (drivers.SessionDriver, error) {
		return newMockDriver(string(cap)), nil
	})

	runner := NewCampaignRunner(dp, collector)

	// Set verification suite to fail tests when defect is present
	br := benchmark.NewBenchmarkRunner()
	br.VerificationSuite = &benchmark.DefaultVerificationSuite{
		CompilerValidator: func(ctx context.Context, session *benchmark.BenchmarkSession) (bool, error) {
			return false, nil
		},
		TestRunner: func(ctx context.Context, session *benchmark.BenchmarkSession) (bool, error) {
			if session.Defect != nil {
				return false, nil // Test fails when defect is seeded -> DefectDetected
			}
			return true, nil // Test passes for clean code
		},
	}
	runner.SetBenchmarkRunner(br)

	spec := CampaignSpec{
		CampaignID:       "camp-m05",
		Corpus:           reg,
		Tasks:            tasks, // task1 (clean + defect), task2 (clean)
		Strategies:       []benchmark.ContextStrategyKind{StrategyFullHistory},
		Capabilities:     []experiments.CapabilityClass{CapabilityLocalSmall},
		Repetitions:      1,
		MaxRepairRounds:  0,
		ConcurrencyLimit: 1,
	}

	summary, err := runner.Execute(context.Background(), spec)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Total runs: 3 (2 clean, 1 seeded defect).
	// Seeded runs: 1. Detected: 1.
	// Rate should be 1.0 (1/1), not 1/3 (0.333) (M-05).
	if math.Abs(summary.DefectCatchRate-1.0) > 1e-6 {
		t.Fatalf("M-05 mutant detected: defect catch rate divided by total runs instead of seeded runs: got %f", summary.DefectCatchRate)
	}
}

// TestCampaign_Mutant_M06_ThreadSafeUpdates tests M-06.
func TestCampaign_Mutant_M06_ThreadSafeUpdates(t *testing.T) {
	reg, tasks := createTestCorpus(t)
	collector := telemetry.NewTelemetryCollector()

	dp := DriverProviderFunc(func(cap experiments.CapabilityClass) (drivers.SessionDriver, error) {
		return newMockDriver(string(cap)), nil
	})

	runner := NewCampaignRunner(dp, collector)

	spec := CampaignSpec{
		CampaignID: "camp-m06",
		Corpus:     reg,
		Tasks:      tasks,
		Strategies: []benchmark.ContextStrategyKind{
			StrategyFullHistory,
			StrategyCompacted,
			StrategySnippetPool,
			StrategyHybrid4Layer,
		},
		Capabilities: []experiments.CapabilityClass{
			CapabilityLocalSmall,
			CapabilitySubscriptionCLI,
			CapabilityFrontierAPI,
		},
		Repetitions:      2,
		MaxRepairRounds:  0,
		ConcurrencyLimit: 12, // High concurrency
	}

	summary, err := runner.Execute(context.Background(), spec)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if summary.CompletedRuns == 0 {
		t.Fatalf("no completed runs")
	}
}

// TestCampaignSpec_Validate tests all validation boundary conditions (REQ-01).
func TestCampaignSpec_Validate(t *testing.T) {
	reg, tasks := createTestCorpus(t)

	validSpec := CampaignSpec{
		CampaignID:       "spec-01",
		Corpus:           reg,
		Tasks:            tasks,
		Strategies:       []benchmark.ContextStrategyKind{StrategyFullHistory},
		Capabilities:     []experiments.CapabilityClass{CapabilityLocalSmall},
		Repetitions:      1,
		MaxRepairRounds:  0,
		ConcurrencyLimit: 2,
	}

	if err := validSpec.Validate(); err != nil {
		t.Fatalf("expected valid spec to pass, got: %v", err)
	}

	// Empty CampaignID
	s := validSpec
	s.CampaignID = ""
	if err := s.Validate(); err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for empty campaign ID, got: %v", err)
	}

	// Nil Corpus
	s = validSpec
	s.Corpus = nil
	if err := s.Validate(); err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for nil corpus, got: %v", err)
	}

	// Empty Tasks
	s = validSpec
	s.Tasks = nil
	if err := s.Validate(); err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for empty tasks, got: %v", err)
	}

	// Empty TaskID in Tasks
	s = validSpec
	s.Tasks = []benchmark.BenchmarkTask{{TaskID: ""}}
	if err := s.Validate(); err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for empty task ID, got: %v", err)
	}

	// Empty Strategies
	s = validSpec
	s.Strategies = nil
	if err := s.Validate(); err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for empty strategies, got: %v", err)
	}

	// Invalid Strategy
	s = validSpec
	s.Strategies = []benchmark.ContextStrategyKind{"invalid_strat"}
	if err := s.Validate(); err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for invalid strategy, got: %v", err)
	}

	// Empty Capabilities
	s = validSpec
	s.Capabilities = nil
	if err := s.Validate(); err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for empty capabilities, got: %v", err)
	}

	// Invalid Capability
	s = validSpec
	s.Capabilities = []experiments.CapabilityClass{"invalid_cap"}
	if err := s.Validate(); err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for invalid capability, got: %v", err)
	}

	// Repetitions < 1
	s = validSpec
	s.Repetitions = 0
	if err := s.Validate(); err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for repetitions < 1, got: %v", err)
	}

	// MaxRepairRounds < 0
	s = validSpec
	s.MaxRepairRounds = -1
	if err := s.Validate(); err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for max repair rounds < 0, got: %v", err)
	}

	// ConcurrencyLimit < 1 and MaxConcurrency < 1
	s = validSpec
	s.ConcurrencyLimit = 0
	s.MaxConcurrency = 0
	if err := s.Validate(); err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for concurrency limit < 1, got: %v", err)
	}

	// ConcurrencyLimit <= 0 but MaxConcurrency > 0 passes
	s = validSpec
	s.ConcurrencyLimit = 0
	s.MaxConcurrency = 4
	if err := s.Validate(); err != nil {
		t.Errorf("expected MaxConcurrency fallback to pass validate, got: %v", err)
	}
}

// TestDriverProvider_FailsClosed tests REQ-02.
func TestDriverProvider_FailsClosed(t *testing.T) {
	runner := NewCampaignRunner(nil, nil)
	reg, tasks := createTestCorpus(t)
	spec := CampaignSpec{
		CampaignID:       "spec-nil-driver",
		Corpus:           reg,
		Tasks:            tasks,
		Strategies:       []benchmark.ContextStrategyKind{StrategyFullHistory},
		Capabilities:     []experiments.CapabilityClass{CapabilityLocalSmall},
		Repetitions:      1,
		ConcurrencyLimit: 1,
	}

	_, err := runner.Execute(context.Background(), spec)
	if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument when driver provider is nil, got: %v", err)
	}
}

// TestSnapshotConversion_Fidelity verifies that token usage metrics and all fields
// are converted to RunTelemetrySnapshot without dropping or truncating (REQ-06).
func TestSnapshotConversion_Fidelity(t *testing.T) {
	runner := NewCampaignRunner(DriverProviderFunc(func(cap experiments.CapabilityClass) (drivers.SessionDriver, error) {
		return newMockDriver(string(cap)), nil
	}), nil)

	spec := CampaignSpec{CampaignID: "test-camp"}
	task := benchmark.BenchmarkTask{TaskID: "task-01"}
	res := &benchmark.BenchmarkRunResult{
		RunID:                 "run-fidelity",
		TaskID:                "task-01",
		Strategy:              StrategyHybrid4Layer,
		Status:                benchmark.RunStatusCompleted,
		Passed:                true,
		TurnsExecuted:         2,
		DefectStatus:          benchmark.DefectDetected,
		InitialTokens:         12345,
		PeakResidentTokens:    67890,
		CumulativeInputTokens: 99999,
		Duration:              250 * time.Millisecond,
		AccountingUncertain:   false,
		TokenUsage: drivers.TokenUsage{
			InputTokens:  99999,
			OutputTokens: 5555,
			CachedTokens: 4444,
		},
	}

	snap := runner.convertToSnapshot(spec, res, task, StrategyHybrid4Layer, CapabilityFrontierAPI, nil, 0, nil)

	if snap.InitialTokens != 12345 {
		t.Errorf("expected InitialTokens=12345, got %d", snap.InitialTokens)
	}
	if snap.PeakResidentTokens != 67890 {
		t.Errorf("expected PeakResidentTokens=67890, got %d", snap.PeakResidentTokens)
	}
	if snap.CumulativeInputTokens != 99999 {
		t.Errorf("expected CumulativeInputTokens=99999, got %d", snap.CumulativeInputTokens)
	}
	if snap.OutputTokens != 5555 {
		t.Errorf("expected OutputTokens=5555, got %d", snap.OutputTokens)
	}
	if snap.CachedTokens != 4444 {
		t.Errorf("expected CachedTokens=4444, got %d", snap.CachedTokens)
	}
	if snap.Turns != 2 {
		t.Errorf("expected Turns=2, got %d", snap.Turns)
	}
	if snap.Duration != 250*time.Millisecond {
		t.Errorf("expected Duration=250ms, got %v", snap.Duration)
	}
	if !snap.Accepted {
		t.Errorf("expected Accepted=true")
	}
}

// TestCampaignRunner_RunCampaignAndSetters tests RunCampaign alias and Setters.
func TestCampaignRunner_RunCampaignAndSetters(t *testing.T) {
	reg, tasks := createTestCorpus(t)
	dp := DriverProviderFunc(func(cap experiments.CapabilityClass) (drivers.SessionDriver, error) {
		return newMockDriver(string(cap)), nil
	})

	runner := NewCampaignRunner(dp, nil)
	runner.SetBenchmarkRunner(benchmark.NewBenchmarkRunner())
	runner.SetExperimentRunner(experiments.NewExperimentRunner())

	spec := CampaignSpec{
		CampaignID:       "camp-runcampaign",
		Corpus:           reg,
		Tasks:            tasks,
		Strategies:       []benchmark.ContextStrategyKind{StrategyFullHistory},
		Capabilities:     []experiments.CapabilityClass{CapabilityLocalSmall},
		Repetitions:      1,
		ConcurrencyLimit: 1,
	}

	summary, err := runner.RunCampaign(context.Background(), spec)
	if err != nil {
		t.Fatalf("RunCampaign failed: %v", err)
	}
	if summary.CompletedRuns == 0 {
		t.Errorf("expected completed runs > 0")
	}
}

// TestEffectiveConcurrency_Branches tests EffectiveConcurrency fallback logic.
func TestEffectiveConcurrency_Branches(t *testing.T) {
	s1 := CampaignSpec{ConcurrencyLimit: 5}
	if s1.EffectiveConcurrency() != 5 {
		t.Errorf("expected 5, got %d", s1.EffectiveConcurrency())
	}

	s2 := CampaignSpec{MaxConcurrency: 7}
	if s2.EffectiveConcurrency() != 7 {
		t.Errorf("expected 7, got %d", s2.EffectiveConcurrency())
	}

	s3 := CampaignSpec{}
	if s3.EffectiveConcurrency() < 1 {
		t.Errorf("expected >= 1, got %d", s3.EffectiveConcurrency())
	}
}

// TestDelegationExperiment_ErrorIsolation tests cell error isolation in delegation experiments.
func TestDelegationExperiment_ErrorIsolation(t *testing.T) {
	reg, tasks := createTestCorpus(t)
	dp := DriverProviderFunc(func(cap experiments.CapabilityClass) (drivers.SessionDriver, error) {
		return newMockDriver(string(cap)), nil
	})

	runner := NewCampaignRunner(dp, nil)

	// Experiment runner with an EWPLoader that fails
	expRunner := experiments.NewExperimentRunner()
	expRunner.EWPLoader = experiments.EWPLoaderFunc(func(ctx context.Context, spec experiments.ExperimentSpec) (string, error) {
		return "", errors.New("simulated EWP load error")
	})
	runner.SetExperimentRunner(expRunner)

	spec := CampaignSpec{
		CampaignID:       "camp-del-error",
		Corpus:           reg,
		Tasks:            tasks,
		Strategies:       []benchmark.ContextStrategyKind{StrategyFullHistory},
		Capabilities:     []experiments.CapabilityClass{CapabilityLocalSmall},
		Repetitions:      1,
		ConcurrencyLimit: 1,
	}

	summary, err := runner.Execute(context.Background(), spec)
	if err != nil {
		t.Fatalf("Execute should not return error on cell failure, got: %v", err)
	}
	if summary.CompletedRuns == 0 {
		t.Errorf("expected completed runs for strategy runs")
	}
}
