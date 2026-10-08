package benchmark

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// fakeSession implements drivers.Session for deterministic benchmark testing.
type fakeSession struct {
	mu           sync.Mutex
	sessionID    string
	driverID     string
	cfg          drivers.SessionConfig
	status       drivers.SessionStatus
	closed       bool
	closeCount   int
	executeCalls []drivers.TurnInput
	turnResults  []drivers.TurnResult
	turnIndex    int
}

func (s *fakeSession) ID() string                    { return s.sessionID }
func (s *fakeSession) DriverID() string              { return s.driverID }
func (s *fakeSession) Config() drivers.SessionConfig { return s.cfg }
func (s *fakeSession) Status() drivers.SessionStatus { return s.status }

func (s *fakeSession) ExecuteTurn(_ context.Context, input drivers.TurnInput) (drivers.TurnResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.executeCalls = append(s.executeCalls, input)

	if s.turnIndex < len(s.turnResults) {
		res := s.turnResults[s.turnIndex]
		s.turnIndex++
		return res, nil
	}

	return drivers.TurnResult{
		TurnID:  input.TurnID,
		Content: fmt.Sprintf("Executed %s successfully. Task completed.", input.TurnID),
		Usage:   drivers.KnownUsage(100, 0, 25),
	}, nil
}

func (s *fakeSession) StreamTurn(_ context.Context, _ drivers.TurnInput) (drivers.EventStream, error) {
	return nil, errs.New(errs.CategoryUnsupported, "streaming not supported in fakeSession")
}

func (s *fakeSession) Close(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.closeCount++
	s.status = drivers.SessionStatusClosed
	return nil
}

// fakeDriver implements drivers.SessionDriver.
type fakeDriver struct {
	mu          sync.Mutex
	startCalls  []drivers.SessionConfig
	lastSession *fakeSession
	turnResults []drivers.TurnResult
	startErr    error
	sessionHook func(s *fakeSession)
}

func (d *fakeDriver) ID() string { return "fake-benchmark-driver" }
func (d *fakeDriver) Capabilities() drivers.DriverCapabilities {
	return drivers.DriverCapabilities{
		Kind:                  protocol.ChannelDirectHTTPAPI,
		SessionMode:           protocol.SessionStatelessPerCall,
		ContextControl:        protocol.ContextControlExactStateless,
		PrefixCache:           protocol.PrefixCacheNone,
		MaxConcurrentRequests: 1,
	}
}

func (d *fakeDriver) StartSession(_ context.Context, cfg drivers.SessionConfig) (drivers.Session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.startErr != nil {
		return nil, d.startErr
	}
	d.startCalls = append(d.startCalls, cfg)
	sess := &fakeSession{
		sessionID:   cfg.SessionID,
		driverID:    d.ID(),
		cfg:         cfg,
		status:      drivers.SessionStatusActive,
		turnResults: d.turnResults,
	}
	if d.sessionHook != nil {
		d.sessionHook(sess)
	}
	d.lastSession = sess
	return sess, nil
}

func (d *fakeDriver) ResumeSession(_ context.Context, _ string, _ drivers.SessionConfig) (drivers.Session, error) {
	return nil, errs.New(errs.CategoryUnsupported, "resume not supported in fakeDriver")
}

func testCleanTask() *BenchmarkTask {
	return &BenchmarkTask{
		TaskID:            "task-clean-001",
		Name:              "Implement Cache Manager",
		WorkPackageID:     "WP-M4-TEST",
		Contract:          "Implement Cache interface satisfying thread-safety and bounded eviction.",
		ReadFiles:         []string{"internal/cache/cache.go"},
		TargetFiles:       []string{"internal/cache/manager.go"},
		ExpectedMutations: []string{"func NewCache()"},
	}
}

// TestRunner_InvalidArguments verifies fail-closed behavior for missing or invalid inputs.
func TestRunner_InvalidArguments(t *testing.T) {
	runner := NewBenchmarkRunner()
	task := testCleanTask()
	driver := &fakeDriver{}
	ctx := context.Background()

	// 1. Missing task
	if _, err := runner.RunTask(ctx, nil, StrategyFullHistory, driver, nil); err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("expected CategoryInvalidArgument for nil task, got %v", err)
	}

	// 2. Missing driver
	if _, err := runner.RunTask(ctx, task, StrategyFullHistory, nil, nil); err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("expected CategoryInvalidArgument for nil driver, got %v", err)
	}

	// 3. Invalid strategy
	if _, err := runner.RunTask(ctx, task, ContextStrategyKind("invalid_strat"), driver, nil); err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("expected CategoryInvalidArgument for invalid strategy, got %v", err)
	}

	// 4. Invalid defect category
	badDefect := &SeededDefect{
		DefectID: "DEF-BAD",
		Category: DefectCategory("invalid_cat"),
	}
	if _, err := runner.RunTask(ctx, task, StrategyFullHistory, driver, badDefect); err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("expected CategoryInvalidArgument for invalid defect category, got %v", err)
	}
}

// TestRunner_SessionCleanupOnCancellation kills mutant: runner fails to close session on context cancellation (INV-02).
func TestRunner_SessionCleanupOnCancellation(t *testing.T) {
	runner := NewBenchmarkRunner()
	task := testCleanTask()
	driver := &fakeDriver{}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context during first turn execution
	driver.sessionHook = func(sess *fakeSession) {
		cancel()
	}

	res, err := runner.RunTask(ctx, task, StrategyFullHistory, driver, nil)
	if err == nil {
		t.Fatal("expected non-nil error on context cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled error, got %v", err)
	}
	if res != nil {
		t.Fatalf("expected nil result on context cancellation, got %+v", res)
	}

	// Verify session cleanup occurred via defer
	if driver.lastSession == nil {
		t.Fatal("expected session to have been started")
	}
	if !driver.lastSession.closed {
		t.Fatal("session leak: expected session.Close() to have been called on context cancellation")
	}
}

// TestStrategy1_MonotonicTranscriptAccumulation kills mutant: Strategy 1 truncates history instead of accumulating monotonically (REQ-07).
func TestStrategy1_MonotonicTranscriptAccumulation(t *testing.T) {
	strat := NewFullHistoryStrategy()
	task := testCleanTask()
	session := &BenchmarkSession{
		SessionID: "sess-strat1",
		Task:      *task,
		Strategy:  StrategyFullHistory,
		Turns:     make([]BenchmarkTurnRecord, 0),
	}

	// Turn 1 prompt
	p1, err := strat.BuildTurnPrompt(context.Background(), session, 1, nil)
	if err != nil {
		t.Fatalf("BuildTurnPrompt turn 1 failed: %v", err)
	}

	// Add Turn 1 to history
	session.Turns = append(session.Turns, BenchmarkTurnRecord{
		Turn:   1,
		Prompt: p1,
		Result: drivers.TurnResult{Content: "Turn 1 completion summary."},
	})

	// Turn 2 prompt
	p2, err := strat.BuildTurnPrompt(context.Background(), session, 2, nil)
	if err != nil {
		t.Fatalf("BuildTurnPrompt turn 2 failed: %v", err)
	}

	// Add Turn 2 to history
	session.Turns = append(session.Turns, BenchmarkTurnRecord{
		Turn:   2,
		Prompt: p2,
		Result: drivers.TurnResult{Content: "Turn 2 completion summary."},
	})

	// Turn 3 prompt
	p3, err := strat.BuildTurnPrompt(context.Background(), session, 3, nil)
	if err != nil {
		t.Fatalf("BuildTurnPrompt turn 3 failed: %v", err)
	}

	// Monotonic growth assertion
	if len(p2) <= len(p1) {
		t.Fatalf("expected monotonic prompt growth: len(p2)=%d must exceed len(p1)=%d", len(p2), len(p1))
	}
	if len(p3) <= len(p2) {
		t.Fatalf("expected monotonic prompt growth: len(p3)=%d must exceed len(p2)=%d", len(p3), len(p2))
	}

	// Verbatim content preservation: earlier responses MUST still be present in later turns
	if !strings.Contains(p3, "Turn 1 completion summary.") {
		t.Fatal("Strategy 1 truncated historical turn 1 response from prompt")
	}
	if !strings.Contains(p3, "Turn 2 completion summary.") {
		t.Fatal("Strategy 1 truncated historical turn 2 response from prompt")
	}
}

// TestStrategy2_CompactionPreservesCorePrefix kills mutant: Strategy 2 compaction removes initial role/contract prompt (REQ-08).
func TestStrategy2_CompactionPreservesCorePrefix(t *testing.T) {
	cfg := CompactedStrategyConfig{MaxHistoryTurns: 2}
	strat := NewCompactedStrategy(cfg)
	task := testCleanTask()
	session := &BenchmarkSession{
		SessionID: "sess-strat2",
		Task:      *task,
		Strategy:  StrategyCompacted,
		Turns:     make([]BenchmarkTurnRecord, 0),
	}

	// Populate 5 turns to trigger compaction
	for turn := 1; turn <= 5; turn++ {
		session.Turns = append(session.Turns, BenchmarkTurnRecord{
			Turn:   turn,
			Prompt: fmt.Sprintf("Turn %d prompt", turn),
			Result: drivers.TurnResult{Content: fmt.Sprintf("Turn %d response", turn)},
		})
	}

	prompt, err := strat.BuildTurnPrompt(context.Background(), session, 6, nil)
	if err != nil {
		t.Fatalf("BuildTurnPrompt failed: %v", err)
	}

	// Assert that initial role and contract core prefix is ALWAYS present
	if !strings.Contains(prompt, "Role: implementer") {
		t.Fatal("Strategy 2 sliding-window compaction removed core role prefix")
	}
	if !strings.Contains(prompt, task.Contract) {
		t.Fatal("Strategy 2 sliding-window compaction removed core execution contract prefix")
	}

	// Assert that older turns are compacted out (sliding window = 2)
	if strings.Contains(prompt, "Turn 1 response") {
		t.Fatal("expected Turn 1 to be compacted out of sliding window")
	}
	if strings.Contains(prompt, "Turn 2 response") {
		t.Fatal("expected Turn 2 to be compacted out of sliding window")
	}
	// Most recent turns (4 and 5) must be present
	if !strings.Contains(prompt, "Turn 4 response") {
		t.Fatal("expected Turn 4 to be preserved in sliding window")
	}
	if !strings.Contains(prompt, "Turn 5 response") {
		t.Fatal("expected Turn 5 to be preserved in sliding window")
	}
}

// TestRunner_TokenUnobservabilityAccounting kills mutant: Driver returns zero tokens and runner leaves AccountingUncertain=false (INV-05).
func TestRunner_TokenUnobservabilityAccounting(t *testing.T) {
	runner := NewBenchmarkRunner()
	task := testCleanTask()
	driver := &fakeDriver{
		turnResults: []drivers.TurnResult{
			{
				TurnID:       "turn-1",
				Content:      "Work done without telemetry. Task completed.",
				PausedReason: "completed",
				Usage:        drivers.TokenUsage{}, // 0 tokens unobservable
			},
		},
	}

	res, err := runner.RunTask(context.Background(), task, StrategyFullHistory, driver, nil)
	if err != nil {
		t.Fatalf("RunTask failed: %v", err)
	}

	if !res.AccountingUncertain {
		t.Fatal("INV-05 violated: expected AccountingUncertain=true when driver reports 0 tokens")
	}

	// Test positive tokens sets AccountingUncertain=false
	driverPositive := &fakeDriver{
		turnResults: []drivers.TurnResult{
			{
				TurnID:       "turn-1",
				Content:      "Work done with telemetry. Task completed.",
				PausedReason: "completed",
				Usage:        drivers.KnownUsage(150, 0, 30),
			},
		},
	}
	resPos, err := runner.RunTask(context.Background(), task, StrategyFullHistory, driverPositive, nil)
	if err != nil {
		t.Fatalf("RunTask failed: %v", err)
	}
	if resPos.AccountingUncertain {
		t.Fatal("expected AccountingUncertain=false when tokens are observable")
	}
	if resPos.TokenUsage.Input.Value != 150 || resPos.TokenUsage.Output.Value != 30 {
		t.Fatalf("token accounting mismatch: expected 150/30, got %d/%d",
			resPos.TokenUsage.Input.Value, resPos.TokenUsage.Output.Value)
	}
}

// TestRunner_PatchApplicationFailure kills mutant: Patch application fails but runner proceeds and reports DefectPrevented.
func TestRunner_PatchApplicationFailure(t *testing.T) {
	runner := NewBenchmarkRunner()
	task := testCleanTask()
	driver := &fakeDriver{}

	// Boundary-escaping defect causes patch failure
	defect := FixtureBoundaryViolation()

	res, err := runner.RunTask(context.Background(), task, StrategyFullHistory, driver, defect)
	if err != nil {
		t.Fatalf("expected nil outer error, got %v", err)
	}

	if res.Status != RunStatusFailed {
		t.Fatalf("expected RunStatusFailed on patch failure, got %s", res.Status)
	}
	if res.DefectStatus == DefectPrevented {
		t.Fatal("mutant detected: runner reported DefectPrevented on patch application failure")
	}
	if res.Err == nil {
		t.Fatal("expected patch error recorded in result.Err")
	}
}

// TestRunner_Strategy4_ContextUnfit verifies ACC-05: undersized profile returns CategoryContextUnfit without silent truncation (REQ-06, INV-03).
func TestRunner_Strategy4_ContextUnfit(t *testing.T) {
	runner := NewBenchmarkRunner()
	task := testCleanTask()
	driver := &fakeDriver{}

	// Undersized context profile where contract token ceiling is artificially exceeded
	prof, err := compiler.DefaultProvisionalProfile("bench-ep", "bench-ch", "bench-model", 4096)
	if err != nil {
		t.Fatalf("DefaultProvisionalProfile failed: %v", err)
	}
	// Set contract limit to 1 token, which cannot fit the execution contract
	prof.ContractLimitTokens = 1
	runner.Hybrid4LayerConfig.Profile = prof

	res, err := runner.RunTask(context.Background(), task, StrategyHybrid4Layer, driver, nil)
	if err != nil {
		t.Fatalf("expected nil outer error, got %v", err)
	}

	if res.Status != RunStatusContextUnfit {
		t.Fatalf("ACC-05 failed: expected RunStatusContextUnfit, got %s", res.Status)
	}
	if res.Err == nil || !errors.Is(res.Err, errs.ErrContextUnfit) {
		t.Fatalf("expected ErrContextUnfit in res.Err, got %v", res.Err)
	}
}
