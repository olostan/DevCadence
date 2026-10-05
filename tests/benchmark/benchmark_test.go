package benchmark_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/olostan/DevCadence/internal/benchmark"
	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// fakeTestDriver provides a deterministic SessionDriver for integration testing.
type fakeTestDriver struct {
	mu           sync.Mutex
	turnResults  []drivers.TurnResult
	defaultTurns int
}

func (d *fakeTestDriver) ID() string { return "test-session-driver" }
func (d *fakeTestDriver) Capabilities() drivers.DriverCapabilities {
	return drivers.DriverCapabilities{
		Kind:                  protocol.ChannelDirectHTTPAPI,
		SessionMode:           protocol.SessionStatelessPerCall,
		ContextControl:        protocol.ContextControlExactStateless,
		PrefixCache:           protocol.PrefixCacheNone,
		MaxConcurrentRequests: 10,
	}
}

func (d *fakeTestDriver) StartSession(_ context.Context, cfg drivers.SessionConfig) (drivers.Session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return &fakeTestSession{
		sessionID:    cfg.SessionID,
		turnResults:  d.turnResults,
		defaultTurns: d.defaultTurns,
	}, nil
}

func (d *fakeTestDriver) ResumeSession(_ context.Context, _ string, _ drivers.SessionConfig) (drivers.Session, error) {
	return nil, errs.New(errs.CategoryUnsupported, "resume not supported")
}

type fakeTestSession struct {
	mu           sync.Mutex
	sessionID    string
	turnResults  []drivers.TurnResult
	defaultTurns int
	currentTurn  int
	closed       bool
}

func (s *fakeTestSession) ID() string       { return s.sessionID }
func (s *fakeTestSession) DriverID() string { return "test-session-driver" }
func (s *fakeTestSession) Config() drivers.SessionConfig {
	return drivers.SessionConfig{SessionID: s.sessionID}
}
func (s *fakeTestSession) Status() drivers.SessionStatus { return drivers.SessionStatusActive }

func (s *fakeTestSession) ExecuteTurn(_ context.Context, input drivers.TurnInput) (drivers.TurnResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.currentTurn++
	if s.currentTurn <= len(s.turnResults) {
		return s.turnResults[s.currentTurn-1], nil
	}

	isFinal := s.currentTurn >= s.defaultTurns
	content := fmt.Sprintf("Turn %d executed successfully.", s.currentTurn)
	reason := ""
	if isFinal {
		content += " Task completed."
		reason = "completed"
	}

	return drivers.TurnResult{
		TurnID:       input.TurnID,
		Content:      content,
		PausedReason: reason,
		Usage: drivers.TokenUsage{
			InputTokens:  120,
			OutputTokens: 30,
		},
	}, nil
}

func (s *fakeTestSession) StreamTurn(_ context.Context, _ drivers.TurnInput) (drivers.EventStream, error) {
	return nil, errs.New(errs.CategoryUnsupported, "streaming not supported")
}

func (s *fakeTestSession) Close(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func cleanBenchmarkTask() *benchmark.BenchmarkTask {
	return &benchmark.BenchmarkTask{
		TaskID:            "task-integration-001",
		Name:              "Implement Cognitive Cache",
		WorkPackageID:     "WP-M4-INT",
		Contract:          "Implement CognitiveCache satisfying LRU eviction and concurrency bounds.",
		ReadFiles:         []string{"internal/cache/cache.go"},
		TargetFiles:       []string{"internal/cache/cognitive.go"},
		ExpectedMutations: []string{"func NewCognitiveCache()"},
	}
}

// ACC-01: Runner executes clean task across all 4 strategies (REQ-01, REQ-04, REQ-07, REQ-10).
// Strategy 4 produces compiler-structured ContextPack; Strategy 1 produces monotonic transcript.
func TestACC01_CleanTaskAcrossAllStrategies(t *testing.T) {
	ctx := context.Background()
	task := cleanBenchmarkTask()
	strategies := []benchmark.ContextStrategyKind{
		benchmark.StrategyFullHistory,
		benchmark.StrategyCompacted,
		benchmark.StrategySnippetPool,
		benchmark.StrategyHybrid4Layer,
	}

	for _, strat := range strategies {
		t.Run(string(strat), func(t *testing.T) {
			runner := benchmark.NewBenchmarkRunner()
			runner.MaxTurns = 2
			driver := &fakeTestDriver{defaultTurns: 2}

			res, err := runner.RunTask(ctx, task, strat, driver, nil)
			if err != nil {
				t.Fatalf("RunTask failed for strategy %s: %v", strat, err)
			}

			if res.Status != benchmark.RunStatusCompleted {
				t.Fatalf("expected RunStatusCompleted, got %s", res.Status)
			}
			if !res.Passed {
				t.Fatalf("expected Passed=true for clean task, got false")
			}
			if res.DefectStatus != benchmark.DefectNotApplicable {
				t.Fatalf("expected DefectNotApplicable for unseeded task, got %s", res.DefectStatus)
			}
			if res.TurnsExecuted != 2 {
				t.Fatalf("expected 2 turns executed, got %d", res.TurnsExecuted)
			}
			if res.TokenUsage.Total() <= 0 {
				t.Fatalf("expected positive token usage, got %d", res.TokenUsage.Total())
			}
			if res.InitialTokens <= 0 {
				t.Fatalf("expected positive InitialTokens, got %d", res.InitialTokens)
			}
			if res.PeakResidentTokens <= 0 {
				t.Fatalf("expected positive PeakResidentTokens, got %d", res.PeakResidentTokens)
			}
			if res.CumulativeInputTokens <= 0 {
				t.Fatalf("expected positive CumulativeInputTokens, got %d", res.CumulativeInputTokens)
			}
			if res.AccountingUncertain {
				t.Fatalf("expected AccountingUncertain=false for observed driver tokens")
			}
		})
	}

	// Strategy 4 invariant check: produces compiler-structured ContextPack
	t.Run("Strategy4_ProducesCompilerContextPack", func(t *testing.T) {
		strat4, err := benchmark.NewHybrid4LayerStrategy(benchmark.Hybrid4LayerStrategyConfig{})
		if err != nil {
			t.Fatalf("NewHybrid4LayerStrategy failed: %v", err)
		}

		session := &benchmark.BenchmarkSession{
			SessionID: "sess-acc01-s4",
			Task:      *task,
			Strategy:  benchmark.StrategyHybrid4Layer,
			Turns:     nil,
		}

		prompt, err := strat4.BuildTurnPrompt(ctx, session, 1, nil)
		if err != nil {
			t.Fatalf("BuildTurnPrompt failed: %v", err)
		}
		if prompt == "" {
			t.Fatal("expected non-empty prompt from Strategy 4")
		}

		// Verify compiler-structured ContextPack was attached
		if session.CompiledPack == nil {
			t.Fatal("Strategy 4 mutant: failed to produce compiler-structured ContextPack")
		}
		if session.CompiledPack.RoleCore == "" {
			t.Fatal("compiled pack has empty RoleCore")
		}
		if session.CompiledPack.ExecutionContract == "" {
			t.Fatal("compiled pack has empty ExecutionContract")
		}
		if session.CompiledPack.PackDigest == "" {
			t.Fatal("compiled pack has empty PackDigest")
		}
	})

	// Strategy 1 invariant check: produces monotonic transcript
	t.Run("Strategy1_ProducesMonotonicTranscript", func(t *testing.T) {
		strat1 := benchmark.NewFullHistoryStrategy()
		session := &benchmark.BenchmarkSession{
			SessionID: "sess-acc01-s1",
			Task:      *task,
			Strategy:  benchmark.StrategyFullHistory,
			Turns:     nil,
		}

		p1, err := strat1.BuildTurnPrompt(ctx, session, 1, nil)
		if err != nil {
			t.Fatalf("BuildTurnPrompt turn 1 failed: %v", err)
		}
		session.Turns = append(session.Turns, benchmark.BenchmarkTurnRecord{
			Turn:   1,
			Prompt: p1,
			Result: drivers.TurnResult{Content: "Assistant response 1 with detailed findings."},
		})

		p2, err := strat1.BuildTurnPrompt(ctx, session, 2, nil)
		if err != nil {
			t.Fatalf("BuildTurnPrompt turn 2 failed: %v", err)
		}
		session.Turns = append(session.Turns, benchmark.BenchmarkTurnRecord{
			Turn:   2,
			Prompt: p2,
			Result: drivers.TurnResult{Content: "Assistant response 2 with further implementation."},
		})

		p3, err := strat1.BuildTurnPrompt(ctx, session, 3, nil)
		if err != nil {
			t.Fatalf("BuildTurnPrompt turn 3 failed: %v", err)
		}

		if len(p2) <= len(p1) {
			t.Fatalf("Strategy 1 monotonic assertion failed: len(p2)=%d must exceed len(p1)=%d", len(p2), len(p1))
		}
		if len(p3) <= len(p2) {
			t.Fatalf("Strategy 1 monotonic assertion failed: len(p3)=%d must exceed len(p2)=%d", len(p3), len(p2))
		}
		if !strings.Contains(p3, "Assistant response 1") || !strings.Contains(p3, "Assistant response 2") {
			t.Fatal("Strategy 1 truncated historical conversational context")
		}
	})
}

// ACC-02: Seeded defect INV-AUTH-BYPASS injected (compiler-inadmissible) (REQ-03, REQ-11, INV-04).
// Strategy 4 rejects defect upfront; defect scored strictly as DefectPrevented.
func TestACC02_DefectPreventedByCompilerUpfront(t *testing.T) {
	ctx := context.Background()
	task := cleanBenchmarkTask()
	runner := benchmark.NewBenchmarkRunner()
	driver := &fakeTestDriver{defaultTurns: 1}

	defect := benchmark.FixtureAuthBypass()

	res, err := runner.RunTask(ctx, task, benchmark.StrategyHybrid4Layer, driver, defect)
	if err != nil {
		t.Fatalf("RunTask returned unexpected outer error: %v", err)
	}

	if res.DefectStatus != benchmark.DefectPrevented {
		t.Fatalf("ACC-02 failed: expected DefectPrevented, got %s", res.DefectStatus)
	}
	if !res.Passed {
		t.Fatalf("expected Passed=true when candidate prevents defect, got false")
	}
}

// ACC-03: Seeded defect injected (runtime failure) (REQ-11, INV-04).
// Verification suite catches bug; defect scored strictly as DefectDetected.
func TestACC03_DefectDetectedByVerificationSuite(t *testing.T) {
	ctx := context.Background()
	task := cleanBenchmarkTask()
	runner := benchmark.NewBenchmarkRunner()
	driver := &fakeTestDriver{defaultTurns: 1}

	defect := benchmark.FixtureRuntimeFailure()

	res, err := runner.RunTask(ctx, task, benchmark.StrategyFullHistory, driver, defect)
	if err != nil {
		t.Fatalf("RunTask returned unexpected outer error: %v", err)
	}

	if res.DefectStatus != benchmark.DefectDetected {
		t.Fatalf("ACC-03 failed: expected DefectDetected, got %s", res.DefectStatus)
	}
	if res.Passed {
		t.Fatalf("expected Passed=false when test suite fails, got true")
	}
}

// ACC-04: Seeded defect unhandled by buggy candidate (REQ-11, INV-04).
// Model prose claims defect was resolved, but verification suite passes buggy code.
// Defect scored strictly as DefectMissed, NEVER DefectDetected based on model prose.
func TestACC04_DefectMissedEvenWithDeceptiveModelProse(t *testing.T) {
	ctx := context.Background()
	task := cleanBenchmarkTask()
	runner := benchmark.NewBenchmarkRunner()

	// Deceptive driver output claiming the defect was caught and eliminated
	driver := &fakeTestDriver{
		turnResults: []drivers.TurnResult{
			{
				TurnID:       "turn-1",
				Content:      "DEFECT DETECTED AND RESOLVED: I discovered the defect and verified with 100% confidence that the fix is applied. All invariants pass.",
				PausedReason: "completed",
				Usage: drivers.TokenUsage{
					InputTokens:  100,
					OutputTokens: 50,
				},
			},
		},
	}

	defect := benchmark.FixtureSilentBug()

	// Verification suite passes the buggy candidate
	runner.VerificationSuite = &benchmark.DefaultVerificationSuite{
		CompilerValidator: func(_ context.Context, _ *benchmark.BenchmarkSession) (bool, error) {
			return false, nil // Did not reject upfront
		},
		TestRunner: func(_ context.Context, _ *benchmark.BenchmarkSession) (bool, error) {
			return true, nil // Tests pass buggy code!
		},
	}

	res, err := runner.RunTask(ctx, task, benchmark.StrategyFullHistory, driver, defect)
	if err != nil {
		t.Fatalf("RunTask returned unexpected error: %v", err)
	}

	// Mutant check: model prose claim MUST NOT influence defect status!
	if res.DefectStatus == benchmark.DefectDetected {
		t.Fatal("Mutant detected: defect scored as DefectDetected based on model text rather than deterministic test exit!")
	}
	if res.DefectStatus != benchmark.DefectMissed {
		t.Fatalf("ACC-04 failed: expected DefectMissed when tests pass buggy code, got %s", res.DefectStatus)
	}
	if !res.Passed {
		t.Fatalf("expected Passed=true because verification suite passed buggy code, got false")
	}
}

// ACC-05: Strategy 4 with undersized context profile (REQ-06, INV-03).
// Compiler returns errs.CategoryContextUnfit; runner records RunStatusContextUnfit without truncation.
func TestACC05_Strategy4_ContextUnfitEnforcement(t *testing.T) {
	ctx := context.Background()
	task := cleanBenchmarkTask()
	runner := benchmark.NewBenchmarkRunner()
	driver := &fakeTestDriver{defaultTurns: 1}

	// Construct undersized context profile
	prof, err := compiler.DefaultProvisionalProfile("bench-ep-acc05", "bench-ch-acc05", "bench-model", 4096)
	if err != nil {
		t.Fatalf("DefaultProvisionalProfile failed: %v", err)
	}
	// Artificially restrict contract tokens to 1 so contract cannot fit
	prof.ContractLimitTokens = 1
	runner.Hybrid4LayerConfig.Profile = prof

	res, err := runner.RunTask(ctx, task, benchmark.StrategyHybrid4Layer, driver, nil)
	if err != nil {
		t.Fatalf("RunTask returned unexpected outer error: %v", err)
	}

	if res.Status != benchmark.RunStatusContextUnfit {
		t.Fatalf("ACC-05 failed: expected RunStatusContextUnfit, got %s", res.Status)
	}
	if res.Err == nil || !errors.Is(res.Err, errs.ErrContextUnfit) {
		t.Fatalf("expected ErrContextUnfit in res.Err, got %v", res.Err)
	}
}
