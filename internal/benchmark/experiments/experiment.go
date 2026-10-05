package experiments

import (
	"context"
	"fmt"
	"time"

	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/errs"
)

// ExperimentSpec defines parameters for a delegation-floor experiment (REQ-03).
type ExperimentSpec struct {
	ExperimentID     string                    `json:"experiment_id"`
	TaskID           string                    `json:"task_id"`
	ContractLevel    ContractCompletenessLevel `json:"contract_level"`
	TargetCapability CapabilityClass           `json:"target_capability"`
	Repetitions      int                       `json:"repetitions"`
	MaxRepairRounds  int                       `json:"max_repair_rounds"`
}

// Validate ensures that the experiment specification conforms to requirements (REQ-03, REQ-10).
func (s ExperimentSpec) Validate() error {
	const kind = "ExperimentSpec"
	if !s.TargetCapability.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid capability class %q", kind, s.TargetCapability)
	}
	if !s.ContractLevel.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid contract completeness level %q", kind, s.ContractLevel)
	}
	if s.Repetitions < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: repetitions must be >= 1, got %d", kind, s.Repetitions)
	}
	if s.MaxRepairRounds < 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: max_repair_rounds must be >= 0, got %d", kind, s.MaxRepairRounds)
	}
	return nil
}

// ExperimentRunResult records the outcome and metrics of a single experiment repetition (REQ-04).
type ExperimentRunResult struct {
	RunID                      string                    `json:"run_id"`
	ExperimentID               string                    `json:"experiment_id"`
	Capability                 CapabilityClass           `json:"capability"`
	ContractLevel              ContractCompletenessLevel `json:"contract_level"`
	PassedFirstPass            bool                      `json:"passed_first_pass"`
	TotalRepairRounds          int                       `json:"total_repair_rounds"`
	ArchitecturalFindingsCount int                       `json:"architectural_findings_count"`
	PrincipalReentryRequired   bool                      `json:"principal_reentry_required"`
	TokensConsumed             int64                     `json:"tokens_consumed"`
	Duration                   time.Duration             `json:"duration"`
	Err                        error                     `json:"err,omitempty"`
}

// ExperimentSummary captures aggregated metrics across all repetitions of an experiment (REQ-05).
type ExperimentSummary struct {
	ExperimentID        string                    `json:"experiment_id"`
	Capability          CapabilityClass           `json:"capability"`
	ContractLevel       ContractCompletenessLevel `json:"contract_level"`
	Repetitions         int                       `json:"repetitions"`
	FirstPassRate       float64                   `json:"first_pass_rate"`
	AvgRepairRounds     float64                   `json:"avg_repair_rounds"`
	AvgArchFindings     float64                   `json:"avg_arch_findings"`
	TotalTokensConsumed int64                     `json:"total_tokens_consumed"`
	Runs                []ExperimentRunResult     `json:"runs"`
}

// VerificationInfo provides context to verification and review passes (REQ-09).
type VerificationInfo struct {
	Spec        ExperimentSpec     `json:"spec"`
	Repetition  int                `json:"repetition"`
	RepairRound int                `json:"repair_round"`
	TurnResult  drivers.TurnResult `json:"turn_result"`
}

// VerificationSuite runs deterministic compiler, test suite, and invariant checks (REQ-09, INV-01).
type VerificationSuite interface {
	Verify(ctx context.Context, session drivers.Session, info VerificationInfo) (passed bool, err error)
}

// VerificationFunc allows using a function as a VerificationSuite.
type VerificationFunc func(ctx context.Context, session drivers.Session, info VerificationInfo) (bool, error)

// Verify implements VerificationSuite.
func (f VerificationFunc) Verify(ctx context.Context, session drivers.Session, info VerificationInfo) (bool, error) {
	return f(ctx, session, info)
}

// ReviewLens evaluates contract adherence and mutations to produce architectural findings (REQ-09).
type ReviewLens interface {
	Review(ctx context.Context, session drivers.Session, info VerificationInfo) (findings int, err error)
}

// ReviewLensFunc allows using a function as a ReviewLens.
type ReviewLensFunc func(ctx context.Context, session drivers.Session, info VerificationInfo) (int, error)

// Review implements ReviewLens.
func (f ReviewLensFunc) Review(ctx context.Context, session drivers.Session, info VerificationInfo) (int, error) {
	return f(ctx, session, info)
}

// EWPLoader loads the EWP contract prompt for the target contract level (REQ-06).
type EWPLoader interface {
	LoadEWP(ctx context.Context, spec ExperimentSpec) (string, error)
}

// EWPLoaderFunc allows using a function as an EWPLoader.
type EWPLoaderFunc func(ctx context.Context, spec ExperimentSpec) (string, error)

// LoadEWP implements EWPLoader.
func (f EWPLoaderFunc) LoadEWP(ctx context.Context, spec ExperimentSpec) (string, error) {
	return f(ctx, spec)
}

// ExperimentRunner executes delegation-floor experiment suites across capability classes (REQ-06, REQ-10).
type ExperimentRunner struct {
	VerificationSuite VerificationSuite
	ContractReviewer  ReviewLens
	MutationReviewer  ReviewLens
	EWPLoader         EWPLoader
	ModelID           string
}

// NewExperimentRunner creates an initialized ExperimentRunner.
func NewExperimentRunner() *ExperimentRunner {
	return &ExperimentRunner{}
}

// Run executes repetitions, collects results, and calculates aggregated metrics (REQ-06, REQ-10).
func (r *ExperimentRunner) Run(ctx context.Context, spec ExperimentSpec, driver drivers.SessionDriver) (*ExperimentSummary, error) {
	if r == nil {
		r = NewExperimentRunner()
	}
	// 1. Validate spec and driver != nil
	if driver == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "experiment runner: driver cannot be nil")
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	// 2. If ctx.Err() != nil -> return nil, ctx.Err()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// 3. For rep := 0; rep < spec.Repetitions; rep++
	runs := make([]ExperimentRunResult, 0, spec.Repetitions)
	for rep := 0; rep < spec.Repetitions; rep++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		res, err := r.runRepetition(ctx, spec, driver, rep)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		runs = append(runs, res)
	}

	// 4. Aggregate metrics across runs:
	firstPassCount := 0
	totalRepairRounds := 0
	totalArchFindings := 0
	totalTokens := int64(0)

	for _, run := range runs {
		if run.PassedFirstPass {
			firstPassCount++
		}
		totalRepairRounds += run.TotalRepairRounds
		totalArchFindings += run.ArchitecturalFindingsCount
		totalTokens += run.TokensConsumed
	}

	n := float64(spec.Repetitions)
	summary := &ExperimentSummary{
		ExperimentID:        spec.ExperimentID,
		Capability:          spec.TargetCapability,
		ContractLevel:       spec.ContractLevel,
		Repetitions:         spec.Repetitions,
		FirstPassRate:       float64(firstPassCount) / n,
		AvgRepairRounds:     float64(totalRepairRounds) / n,
		AvgArchFindings:     float64(totalArchFindings) / n,
		TotalTokensConsumed: totalTokens,
		Runs:                runs,
	}

	// 5. Return ExperimentSummary
	return summary, nil
}

func (r *ExperimentRunner) runRepetition(ctx context.Context, spec ExperimentSpec, driver drivers.SessionDriver, rep int) (ExperimentRunResult, error) {
	repStart := time.Now()
	runID := fmt.Sprintf("%s-rep-%d", spec.ExperimentID, rep)
	if spec.ExperimentID == "" {
		runID = fmt.Sprintf("run-%s-%d", spec.TargetCapability, rep)
	}

	// b. Load EWP according to spec.ContractLevel (Baseline vs. ImplementationReady)
	ewpPrompt, err := r.loadEWP(ctx, spec)
	if err != nil {
		if ctx.Err() != nil {
			return ExperimentRunResult{}, ctx.Err()
		}
		return ExperimentRunResult{
			RunID:                    runID,
			ExperimentID:             spec.ExperimentID,
			Capability:               spec.TargetCapability,
			ContractLevel:            spec.ContractLevel,
			PrincipalReentryRequired: true,
			Duration:                 time.Since(repStart),
			Err:                      err,
		}, nil
	}

	// c. Start driver session: session, err := driver.StartSession(ctx, cfg)
	modelID := r.ModelID
	if modelID == "" {
		modelID = string(spec.TargetCapability)
	}
	cfg := drivers.SessionConfig{
		SessionID: runID,
		ModelID:   modelID,
	}

	session, err := driver.StartSession(ctx, cfg)
	if err != nil {
		if ctx.Err() != nil {
			return ExperimentRunResult{}, ctx.Err()
		}
		return ExperimentRunResult{
			RunID:                    runID,
			ExperimentID:             spec.ExperimentID,
			Capability:               spec.TargetCapability,
			ContractLevel:            spec.ContractLevel,
			PrincipalReentryRequired: true,
			Duration:                 time.Since(repStart),
			Err:                      err,
		}, nil
	}
	// Deferred session cleanup ensures resources are cleanly released between repetitions (REQ-10)
	defer session.Close(context.Background())

	// d. Execute implementation turn with driver
	turnInput := drivers.TurnInput{
		TurnID: fmt.Sprintf("impl-%d", rep),
		Prompt: ewpPrompt,
	}
	turnRes, err := session.ExecuteTurn(ctx, turnInput)
	tokensConsumed := turnRes.Usage.Total()
	if err != nil {
		if ctx.Err() != nil {
			return ExperimentRunResult{}, ctx.Err()
		}
		return ExperimentRunResult{
			RunID:                    runID,
			ExperimentID:             spec.ExperimentID,
			Capability:               spec.TargetCapability,
			ContractLevel:            spec.ContractLevel,
			PassedFirstPass:          false,
			PrincipalReentryRequired: true,
			TokensConsumed:           tokensConsumed,
			Duration:                 time.Since(repStart),
			Err:                      err,
		}, nil
	}

	vInfo := VerificationInfo{
		Spec:        spec,
		Repetition:  rep,
		RepairRound: 0,
		TurnResult:  turnRes,
	}

	// e. Run deterministic verification suite (ACC scenarios)
	testsPassed, vErr := r.runVerification(ctx, session, vInfo)
	if vErr != nil {
		if ctx.Err() != nil {
			return ExperimentRunResult{}, ctx.Err()
		}
		return ExperimentRunResult{
			RunID:                    runID,
			ExperimentID:             spec.ExperimentID,
			Capability:               spec.TargetCapability,
			ContractLevel:            spec.ContractLevel,
			PassedFirstPass:          false,
			PrincipalReentryRequired: true,
			TokensConsumed:           tokensConsumed,
			Duration:                 time.Since(repStart),
			Err:                      vErr,
		}, nil
	}

	// f. Run independent review lenses (Contract & Mutation)
	findings, rErr := r.runReviewLenses(ctx, session, vInfo)
	if rErr != nil {
		if ctx.Err() != nil {
			return ExperimentRunResult{}, ctx.Err()
		}
		return ExperimentRunResult{
			RunID:                    runID,
			ExperimentID:             spec.ExperimentID,
			Capability:               spec.TargetCapability,
			ContractLevel:            spec.ContractLevel,
			PassedFirstPass:          false,
			PrincipalReentryRequired: true,
			TokensConsumed:           tokensConsumed,
			Duration:                 time.Since(repStart),
			Err:                      rErr,
		}, nil
	}

	// g. If failures found:
	// While repair rounds < spec.MaxRepairRounds and tests fail:
	repairRounds := 0
	var lastErr error
	for repairRounds < spec.MaxRepairRounds && !testsPassed {
		if ctx.Err() != nil {
			return ExperimentRunResult{}, ctx.Err()
		}
		repairRounds++
		repairInput := drivers.TurnInput{
			TurnID: fmt.Sprintf("repair-%d-%d", rep, repairRounds),
			Prompt: fmt.Sprintf("Verification failed on repair round %d/%d for task %s. Repair the defect to satisfy contract requirements.", repairRounds, spec.MaxRepairRounds, spec.TaskID),
		}
		repairRes, repErr := session.ExecuteTurn(ctx, repairInput)
		tokensConsumed += repairRes.Usage.Total()
		if repErr != nil {
			if ctx.Err() != nil {
				return ExperimentRunResult{}, ctx.Err()
			}
			lastErr = repErr
			break
		}

		repairVInfo := VerificationInfo{
			Spec:        spec,
			Repetition:  rep,
			RepairRound: repairRounds,
			TurnResult:  repairRes,
		}

		// repeat check
		repPassed, repVErr := r.runVerification(ctx, session, repairVInfo)
		if repVErr != nil {
			if ctx.Err() != nil {
				return ExperimentRunResult{}, ctx.Err()
			}
			lastErr = repVErr
			break
		}
		testsPassed = repPassed
	}

	// h. Record RunResult:
	// - PassedFirstPass = (repairRounds == 0 && testPassed)
	// - TotalRepairRounds = repairRounds
	// - ArchitecturalFindingsCount = findings
	passedFirstPass := repairRounds == 0 && testsPassed
	principalReentry := !testsPassed || findings > 0

	return ExperimentRunResult{
		RunID:                      runID,
		ExperimentID:               spec.ExperimentID,
		Capability:                 spec.TargetCapability,
		ContractLevel:              spec.ContractLevel,
		PassedFirstPass:            passedFirstPass,
		TotalRepairRounds:          repairRounds,
		ArchitecturalFindingsCount: findings,
		PrincipalReentryRequired:   principalReentry,
		TokensConsumed:             tokensConsumed,
		Duration:                   time.Since(repStart),
		Err:                        lastErr,
	}, nil
}

func (r *ExperimentRunner) loadEWP(ctx context.Context, spec ExperimentSpec) (string, error) {
	if r.EWPLoader != nil {
		return r.EWPLoader.LoadEWP(ctx, spec)
	}
	if spec.ContractLevel == ContractImplementationReady {
		return fmt.Sprintf("Task %s: Implementation-ready execution contract with zero ambiguity.", spec.TaskID), nil
	}
	return fmt.Sprintf("Task %s: Baseline incomplete execution contract.", spec.TaskID), nil
}

func (r *ExperimentRunner) runVerification(ctx context.Context, session drivers.Session, info VerificationInfo) (bool, error) {
	// INV-01: Verification gates must be identical across all capability tiers; no relaxation for weaker models.
	if r.VerificationSuite != nil {
		return r.VerificationSuite.Verify(ctx, session, info)
	}
	return info.TurnResult.PausedReason == "", nil
}

func (r *ExperimentRunner) runReviewLenses(ctx context.Context, session drivers.Session, info VerificationInfo) (int, error) {
	findings := 0
	if r.ContractReviewer != nil {
		f, err := r.ContractReviewer.Review(ctx, session, info)
		if err != nil {
			return 0, err
		}
		findings += f
	}
	if r.MutationReviewer != nil {
		f, err := r.MutationReviewer.Review(ctx, session, info)
		if err != nil {
			return 0, err
		}
		findings += f
	}
	return findings, nil
}
