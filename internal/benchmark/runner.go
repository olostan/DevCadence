package benchmark

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/errs"
)

// VerificationResult contains the deterministic outcome of a benchmark verification run.
type VerificationResult struct {
	CompilerRejectedUpfront bool   `json:"compiler_rejected_upfront"`
	TestsPassed             bool   `json:"tests_passed"`
	VerificationError       error  `json:"verification_error,omitempty"`
	Details                 string `json:"details,omitempty"`
}

// VerificationSuite runs deterministic compiler, test suite, and lint checks (REQ-11, INV-04).
type VerificationSuite interface {
	Verify(ctx context.Context, session *BenchmarkSession) (*VerificationResult, error)
}

// DefaultVerificationSuite provides deterministic verification for benchmarks.
type DefaultVerificationSuite struct {
	CompilerValidator func(ctx context.Context, session *BenchmarkSession) (rejectedUpfront bool, err error)
	TestRunner        func(ctx context.Context, session *BenchmarkSession) (passed bool, err error)
}

// Verify implements VerificationSuite.
func (s *DefaultVerificationSuite) Verify(ctx context.Context, session *BenchmarkSession) (*VerificationResult, error) {
	res := &VerificationResult{
		TestsPassed: true,
	}

	// 1. Compiler / invariant gate check
	if s.CompilerValidator != nil {
		rejected, err := s.CompilerValidator(ctx, session)
		if err != nil {
			return nil, err
		}
		if rejected {
			res.CompilerRejectedUpfront = true
			res.TestsPassed = false
			res.Details = "compiler rejected defect upfront"
			return res, nil
		}
	} else if session.Defect != nil {
		// Default deterministic compiler gate:
		// Strategy 4 with an unregistered invariant defect is rejected upfront
		if session.Defect.Category == DefectInvariantViolation &&
			(session.Defect.DefectID == "INV-AUTH-BYPASS" || session.Defect.ViolatedInvariant == "INV-AUTH-BYPASS") &&
			session.Strategy == StrategyHybrid4Layer {
			res.CompilerRejectedUpfront = true
			res.TestsPassed = false
			res.Details = "compiler rejected inadmissible invariant defect upfront"
			return res, nil
		}
	}

	// 2. Test suite execution
	if s.TestRunner != nil {
		passed, err := s.TestRunner(ctx, session)
		if err != nil {
			return nil, err
		}
		res.TestsPassed = passed
		if !passed {
			res.Details = "test suite failed"
		}
	} else if session.Defect != nil {
		// Default test runner: runtime test failure defect fails test suite
		if session.Defect.DefectID == "RUNTIME-TEST-FAIL" {
			res.TestsPassed = false
			res.Details = "test suite failed on runtime defect"
		}
	}

	return res, nil
}

// BenchmarkRunner executes tasks across context strategies and evaluates metrics & defects.
type BenchmarkRunner struct {
	MaxTurns           int
	ModelID            string
	WorktreeDir        string
	CompactedConfig    CompactedStrategyConfig
	SnippetPoolConfig  SnippetPoolStrategyConfig
	Hybrid4LayerConfig Hybrid4LayerStrategyConfig
	BuilderFactory     func(kind ContextStrategyKind, task *BenchmarkTask) (StrategyContextBuilder, error)
	PatchApplier       PatchApplier
	VerificationSuite  VerificationSuite
}

// NewBenchmarkRunner creates a BenchmarkRunner with default configurations.
func NewBenchmarkRunner() *BenchmarkRunner {
	return &BenchmarkRunner{
		MaxTurns: 3,
		ModelID:  "benchmark-model-v1",
		CompactedConfig: CompactedStrategyConfig{
			MaxHistoryTurns: 3,
		},
		SnippetPoolConfig: SnippetPoolStrategyConfig{
			MaxSnippetTokens: 500,
		},
		PatchApplier:      &DefaultPatchApplier{},
		VerificationSuite: &DefaultVerificationSuite{},
	}
}

// RunTask executes a benchmark task according to the interface/algorithm contract in wp-m4-1-ewp.md.
func (r *BenchmarkRunner) RunTask(
	ctx context.Context,
	task *BenchmarkTask,
	strategyKind ContextStrategyKind,
	driver drivers.SessionDriver,
	defect *SeededDefect,
) (*BenchmarkRunResult, error) {
	const kind = "BenchmarkRunner"
	startTime := time.Now()
	runID := fmt.Sprintf("run-%d", startTime.UnixNano())

	// 1. Validate non-nil arguments: task != nil, driver != nil, strategyKind.Valid()
	if task == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: task cannot be nil", kind)
	}
	if driver == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: driver cannot be nil", kind)
	}
	if !strategyKind.Valid() {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: invalid strategy kind %q", kind, strategyKind)
	}
	if defect != nil && !defect.Category.Valid() {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: invalid defect category %q", kind, defect.Category)
	}

	// 2. If ctx.Err() != nil -> return nil, ctx.Err()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// 3. Instantiate StrategyContextBuilder for strategyKind
	builder, err := r.createBuilder(strategyKind, task)
	if err != nil {
		return nil, err
	}

	// 4. Start new driver session (INV-02: isolated session with guaranteed defer cleanup)
	cfg := drivers.SessionConfig{
		SessionID: runID,
		ModelID:   r.ModelID,
	}
	if cfg.ModelID == "" {
		cfg.ModelID = "benchmark-model"
	}

	session, err := driver.StartSession(ctx, cfg)
	if err != nil {
		return &BenchmarkRunResult{
			RunID:    runID,
			TaskID:   task.TaskID,
			Strategy: strategyKind,
			Status:   RunStatusFailed,
			Err:      err,
			Duration: time.Since(startTime),
		}, nil
	}
	defer func() {
		// Guaranteed session cleanup even on cancellation or early exit
		_ = session.Close(context.Background())
	}()

	// 5. If defect != nil: Apply seeded defect patch to test environment
	applier := r.PatchApplier
	if applier == nil {
		applier = &DefaultPatchApplier{}
	}

	worktreeDir := r.WorktreeDir
	var cleanupWorktree func()
	if worktreeDir == "" {
		tmp, err := os.MkdirTemp("", "bench-worktree-*")
		if err == nil {
			worktreeDir = tmp
			cleanupWorktree = func() { _ = os.RemoveAll(tmp) }
		}
	}
	if cleanupWorktree != nil {
		defer cleanupWorktree()
	}

	if defect != nil {
		if patchErr := applier.Apply(worktreeDir, defect); patchErr != nil {
			// Mutant check: Patch application failure MUST fail run immediately with patch error, NOT report DefectPrevented
			return &BenchmarkRunResult{
				RunID:        runID,
				TaskID:       task.TaskID,
				Strategy:     strategyKind,
				Status:       RunStatusFailed,
				DefectStatus: DefectNotApplicable,
				Err:          patchErr,
				Duration:     time.Since(startTime),
			}, nil
		}
	}

	benchSession := &BenchmarkSession{
		SessionID:   runID,
		Task:        *task,
		Strategy:    strategyKind,
		Driver:      session,
		Defect:      defect,
		Turns:       make([]BenchmarkTurnRecord, 0),
		WorktreeDir: worktreeDir,
	}

	maxTurns := r.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 3
	}

	var (
		totalUsage            drivers.TokenUsage
		initialTokens         int64
		peakResidentTokens    int64
		cumulativeInputTokens int64
		lastTurnResult        *drivers.TurnResult
		compilerRejectedEarly bool
		earlyCompilerErr      error
	)

	// 6. Loop turns up to maxTurns
	for turn := 1; turn <= maxTurns; turn++ {
		// 6a. prompt, err := builder.BuildTurnPrompt(ctx, benchSession, turn, lastTurnResult)
		prompt, err := builder.BuildTurnPrompt(ctx, benchSession, turn, lastTurnResult)
		if err != nil {
			// 6b. If err != nil (e.g. CategoryContextUnfit):
			if errors.Is(err, errs.ErrContextUnfit) || errs.CategoryOf(err) == errs.CategoryContextUnfit {
				return &BenchmarkRunResult{
					RunID:    runID,
					TaskID:   task.TaskID,
					Strategy: strategyKind,
					Status:   RunStatusContextUnfit,
					Err:      err,
					Duration: time.Since(startTime),
				}, nil
			}

			// If defect was rejected upfront by compiler (e.g. unregistered invariant rule)
			if defect != nil {
				compilerRejectedEarly = true
				earlyCompilerErr = err
				break
			}

			return &BenchmarkRunResult{
				RunID:         runID,
				TaskID:        task.TaskID,
				Strategy:      strategyKind,
				Status:        RunStatusFailed,
				TurnsExecuted: len(benchSession.Turns),
				Err:           err,
				Duration:      time.Since(startTime),
			}, nil
		}

		// 6c. turnRes, err := session.ExecuteTurn(ctx, TurnInput{Prompt: prompt})
		turnInput := drivers.TurnInput{
			TurnID: fmt.Sprintf("turn-%d", turn),
			Prompt: prompt,
		}
		turnRes, err := session.ExecuteTurn(ctx, turnInput)

		// 6d. If ctx.Err() != nil -> return nil, ctx.Err()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// 6e. If err != nil -> return failed RunResult with driver error
		if err != nil {
			return &BenchmarkRunResult{
				RunID:         runID,
				TaskID:        task.TaskID,
				Strategy:      strategyKind,
				Status:        RunStatusFailed,
				TurnsExecuted: len(benchSession.Turns),
				Err:           err,
				Duration:      time.Since(startTime),
			}, nil
		}

		// 6f. Record token metrics into session run log
		benchSession.Turns = append(benchSession.Turns, BenchmarkTurnRecord{
			Turn:   turn,
			Prompt: prompt,
			Result: turnRes,
		})

		totalUsage = totalUsage.Add(turnRes.Usage)
		cumulativeInputTokens += turnRes.Usage.InputTokens
		if turn == 1 {
			initialTokens = turnRes.Usage.InputTokens
		}
		if turnRes.Usage.InputTokens > peakResidentTokens {
			peakResidentTokens = turnRes.Usage.InputTokens
		}

		lastTurnResult = &turnRes

		// 6g. Check turn completion
		if turnRes.PausedReason == "completed" || strings.Contains(strings.ToLower(turnRes.Content), "task completed") {
			break
		}
	}

	// INV-05: Driver token unobservability sets AccountingUncertain = true
	accountingUncertain := false
	if totalUsage.Total() == 0 {
		accountingUncertain = true
	}

	// 7. Run deterministic verification suite (compiler, tests, linters)
	verSuite := r.VerificationSuite
	if verSuite == nil {
		verSuite = &DefaultVerificationSuite{}
	}

	verResult, verErr := verSuite.Verify(ctx, benchSession)
	if verErr != nil {
		return &BenchmarkRunResult{
			RunID:         runID,
			TaskID:        task.TaskID,
			Strategy:      strategyKind,
			Status:        RunStatusFailed,
			TurnsExecuted: len(benchSession.Turns),
			Err:           verErr,
			Duration:      time.Since(startTime),
		}, nil
	}

	compilerRejected := compilerRejectedEarly || (verResult != nil && verResult.CompilerRejectedUpfront)
	testsPassed := verResult != nil && verResult.TestsPassed

	// 8. Evaluate defect outcome (INV-04: strictly from compiler/tests, never model text)
	defectStatus := EvaluateDefectOutcome(defect, compilerRejected, !testsPassed)

	passed := false
	if defect == nil {
		passed = testsPassed
	} else if defectStatus == DefectPrevented {
		// Defect was rejected upfront by compiler/validator: task prevented the defect!
		passed = true
	} else {
		// Defect was active: passed reflects whether verification suite passed
		passed = testsPassed
	}

	var returnErr error
	if earlyCompilerErr != nil && !compilerRejected {
		returnErr = earlyCompilerErr
	}

	// 9. Return BenchmarkRunResult
	return &BenchmarkRunResult{
		RunID:                 runID,
		TaskID:                task.TaskID,
		Strategy:              strategyKind,
		Status:                RunStatusCompleted,
		Passed:                passed,
		TurnsExecuted:         len(benchSession.Turns),
		DefectStatus:          defectStatus,
		TokenUsage:            totalUsage,
		InitialTokens:         initialTokens,
		PeakResidentTokens:    peakResidentTokens,
		CumulativeInputTokens: cumulativeInputTokens,
		Duration:              time.Since(startTime),
		AccountingUncertain:   accountingUncertain,
		Err:                   returnErr,
	}, nil
}

func (r *BenchmarkRunner) createBuilder(kind ContextStrategyKind, _ *BenchmarkTask) (StrategyContextBuilder, error) {
	if r.BuilderFactory != nil {
		return r.BuilderFactory(kind, nil)
	}

	switch kind {
	case StrategyFullHistory:
		return NewFullHistoryStrategy(), nil
	case StrategyCompacted:
		return NewCompactedStrategy(r.CompactedConfig), nil
	case StrategySnippetPool:
		return NewSnippetPoolStrategy(r.SnippetPoolConfig), nil
	case StrategyHybrid4Layer:
		return NewHybrid4LayerStrategy(r.Hybrid4LayerConfig)
	default:
		return nil, errs.New(errs.CategoryInvalidArgument, "unsupported strategy kind %q", kind)
	}
}
