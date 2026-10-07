package campaign

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/olostan/DevCadence/internal/benchmark"
	"github.com/olostan/DevCadence/internal/benchmark/experiments"
	"github.com/olostan/DevCadence/internal/benchmark/telemetry"
	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/errs"
)

// CampaignRunner orchestrates multi-strategy evaluation campaigns across capability classes (REQ-04).
type CampaignRunner struct {
	driverProvider   DriverProvider
	collector        *telemetry.TelemetryCollector
	benchmarkRunner  *benchmark.BenchmarkRunner
	experimentRunner *experiments.ExperimentRunner
	mu               sync.Mutex
}

// NewCampaignRunner creates an initialized CampaignRunner (REQ-04).
func NewCampaignRunner(dp DriverProvider, collector *telemetry.TelemetryCollector) *CampaignRunner {
	if collector == nil {
		collector = telemetry.NewTelemetryCollector()
	}
	return &CampaignRunner{
		driverProvider:   dp,
		collector:        collector,
		benchmarkRunner:  benchmark.NewBenchmarkRunner(),
		experimentRunner: experiments.NewExperimentRunner(),
	}
}

// SetBenchmarkRunner overrides the internal BenchmarkRunner (e.g. for testing with custom configs).
func (r *CampaignRunner) SetBenchmarkRunner(br *benchmark.BenchmarkRunner) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.benchmarkRunner = br
}

// SetExperimentRunner overrides the internal ExperimentRunner (e.g. for testing with custom configs).
func (r *CampaignRunner) SetExperimentRunner(er *experiments.ExperimentRunner) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.experimentRunner = er
}

// RunCampaign is an alias for Execute to fulfill campaign execution requirements.
func (r *CampaignRunner) RunCampaign(ctx context.Context, spec CampaignSpec) (*CampaignSummary, error) {
	return r.Execute(ctx, spec)
}

// Execute runs the full evaluation matrix specified in spec (REQ-04, REQ-08).
func (r *CampaignRunner) Execute(ctx context.Context, spec CampaignSpec) (*CampaignSummary, error) {
	const kind = "CampaignRunner.Execute"
	if r.driverProvider == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: driver provider cannot be nil", kind)
	}

	// Default concurrency if unset
	if spec.ConcurrencyLimit == 0 && spec.MaxConcurrency == 0 {
		spec.ConcurrencyLimit = spec.EffectiveConcurrency()
		spec.MaxConcurrency = spec.ConcurrencyLimit
	} else if spec.ConcurrencyLimit <= 0 && spec.MaxConcurrency > 0 {
		spec.ConcurrencyLimit = spec.MaxConcurrency
	} else if spec.MaxConcurrency <= 0 && spec.ConcurrencyLimit > 0 {
		spec.MaxConcurrency = spec.ConcurrencyLimit
	}

	// 1. Validate spec (non-nil Corpus, non-empty fields, positive bounds)
	if err := spec.Validate(); err != nil {
		return nil, err
	}

	startTime := time.Now()

	// 3. Initialize CampaignSummary with CampaignID and start time
	summary := &CampaignSummary{
		CampaignID:           spec.CampaignID,
		Status:               CampaignStatusCompleted,
		Snapshots:            make([]telemetry.RunTelemetrySnapshot, 0),
		DelegationSummaries:  make(map[string]*experiments.ExperimentSummary),
		FalsificationResults: make(map[string]*experiments.FalsificationResult),
		Tasks:                make(map[string]*experiments.ExperimentSummary),
	}

	// 2. Context cancellation fail-closed check
	if ctx.Err() != nil {
		summary.Status = CampaignStatusCancelled
		summary.Duration = time.Since(startTime)
		return summary, ctx.Err()
	}

	// Calculate TotalRuns across all matrix permutations
	totalRuns := 0
	for _, task := range spec.Tasks {
		defects := spec.Corpus.GetDefectsForTask(task.TaskID)
		defectConfigsCount := 1 + len(defects) // clean + seeded defects
		totalRuns += len(spec.Capabilities) * len(spec.Strategies) * defectConfigsCount * spec.Repetitions
	}
	summary.TotalRuns = totalRuns

	type campaignJob func(ctx context.Context)

	jobs := make([]campaignJob, 0)

	// Build matrix jobs and handle tier driver availability
	for _, task := range spec.Tasks {
		taskCopy := task
		defects := spec.Corpus.GetDefectsForTask(taskCopy.TaskID)

		for _, cap := range spec.Capabilities {
			capCopy := cap
			driver, err := r.driverProvider.GetDriver(capCopy)
			if err != nil {
				// Driver unavailable for tier: fail runs for that tier and record failure snapshots (REQ-02, REQ-08)
				for _, strat := range spec.Strategies {
					stratCopy := strat
					defectList := append([]*benchmark.SeededDefect{nil}, defectPtrs(defects)...)
					for _, d := range defectList {
						dCopy := d
						for rep := 0; rep < spec.Repetitions; rep++ {
							repNum := rep
							r.mu.Lock()
							summary.FailedRuns++
							r.mu.Unlock()

							defectID := "clean"
							if dCopy != nil {
								defectID = dCopy.DefectID
							}
							runID := fmt.Sprintf("%s-%s-%s-%s-%s-rep%d-failed", spec.CampaignID, taskCopy.TaskID, capCopy, stratCopy, defectID, repNum)
							snap := telemetry.RunTelemetrySnapshot{
								RunID:        runID,
								TaskID:       taskCopy.TaskID,
								Strategy:     string(stratCopy),
								Capability:   string(capCopy),
								Accepted:     false,
								DefectStatus: string(benchmark.DefectNotApplicable),
								DefectSeeded: dCopy != nil,
								Duration:     0,
							}
							r.collector.RecordSnapshot(snap)
						}
					}
				}
				continue
			}

			// Driver available: schedule clean and seeded defect permutations across spec.Strategies
			defectList := append([]*benchmark.SeededDefect{nil}, defectPtrs(defects)...)
			for _, strat := range spec.Strategies {
				stratCopy := strat
				for _, defect := range defectList {
					defectCopy := defect
					for rep := 0; rep < spec.Repetitions; rep++ {
						repCopy := rep
						jobs = append(jobs, func(ctx context.Context) {
							if ctx.Err() != nil {
								return
							}
							r.executeBenchmarkPermutation(ctx, spec, taskCopy, stratCopy, capCopy, driver, defectCopy, repCopy, summary)
						})
					}
				}
			}

			// Delegation floor experiment for task:cap (REQ-07)
			jobs = append(jobs, func(ctx context.Context) {
				if ctx.Err() != nil {
					return
				}
				r.executeDelegationPermutation(ctx, spec, taskCopy, capCopy, driver, summary)
			})
		}
	}

	// Concurrency control via bounded worker pool (spec.ConcurrencyLimit / spec.MaxConcurrency)
	concurrency := spec.EffectiveConcurrency()
	if concurrency > len(jobs) && len(jobs) > 0 {
		concurrency = len(jobs)
	}
	if concurrency < 1 {
		concurrency = 1
	}

	jobChan := make(chan campaignJob, len(jobs))
	for _, j := range jobs {
		jobChan <- j
	}
	close(jobChan)

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobChan {
				if ctx.Err() != nil {
					return
				}
				job(ctx)
			}
		}()
	}
	wg.Wait()

	summary.Duration = time.Since(startTime)
	summary.Snapshots = r.collector.GetSnapshots()
	r.aggregateSummary(summary)

	// Check cancellation fail-closed postcondition (REQ-08, M-04)
	if ctx.Err() != nil {
		summary.Status = CampaignStatusCancelled
		return summary, ctx.Err()
	}

	if summary.FailedRuns > 0 && summary.CompletedRuns == 0 {
		summary.Status = CampaignStatusFailed
	} else {
		summary.Status = CampaignStatusCompleted
	}

	return summary, nil
}

func (r *CampaignRunner) executeBenchmarkPermutation(
	ctx context.Context,
	spec CampaignSpec,
	task benchmark.BenchmarkTask,
	strat benchmark.ContextStrategyKind,
	cap experiments.CapabilityClass,
	driver drivers.SessionDriver,
	defect *benchmark.SeededDefect,
	rep int,
	summary *CampaignSummary,
) {
	runner := r.benchmarkRunner
	if runner == nil {
		runner = benchmark.NewBenchmarkRunner()
	}

	res, err := runner.RunTask(ctx, &task, strat, driver, defect)
	if ctx.Err() != nil {
		return
	}

	isFailed := err != nil || res == nil || res.Status == benchmark.RunStatusFailed

	r.mu.Lock()
	if isFailed {
		summary.FailedRuns++
	} else {
		summary.CompletedRuns++
	}
	r.mu.Unlock()

	snap := r.convertToSnapshot(spec, res, task, strat, cap, defect, rep, err)
	r.collector.RecordSnapshot(snap)
}

func (r *CampaignRunner) executeDelegationPermutation(
	ctx context.Context,
	spec CampaignSpec,
	task benchmark.BenchmarkTask,
	cap experiments.CapabilityClass,
	driver drivers.SessionDriver,
	summary *CampaignSummary,
) {
	delKey := fmt.Sprintf("%s:%s", task.TaskID, cap)

	baseSpec := experiments.ExperimentSpec{
		ExperimentID:     fmt.Sprintf("%s-%s-baseline", task.TaskID, cap),
		TaskID:           task.TaskID,
		ContractLevel:    experiments.ContractBaseline,
		TargetCapability: cap,
		Repetitions:      spec.Repetitions,
		MaxRepairRounds:  spec.MaxRepairRounds,
	}
	readySpec := experiments.ExperimentSpec{
		ExperimentID:     fmt.Sprintf("%s-%s-ready", task.TaskID, cap),
		TaskID:           task.TaskID,
		ContractLevel:    experiments.ContractImplementationReady,
		TargetCapability: cap,
		Repetitions:      spec.Repetitions,
		MaxRepairRounds:  spec.MaxRepairRounds,
	}

	expRunner := r.experimentRunner
	if expRunner == nil {
		expRunner = experiments.NewExperimentRunner()
	}

	baseSum, err := expRunner.Run(ctx, baseSpec, driver)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		// Cell error isolation
		r.mu.Lock()
		summary.DelegationSummaries[delKey] = &experiments.ExperimentSummary{
			ExperimentID:  readySpec.ExperimentID,
			Capability:    cap,
			ContractLevel: experiments.ContractImplementationReady,
		}
		summary.Tasks[delKey] = summary.DelegationSummaries[delKey]
		r.mu.Unlock()
		return
	}

	readySum, err := expRunner.Run(ctx, readySpec, driver)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		// Cell error isolation
		r.mu.Lock()
		summary.DelegationSummaries[delKey] = &experiments.ExperimentSummary{
			ExperimentID:  readySpec.ExperimentID,
			Capability:    cap,
			ContractLevel: experiments.ContractImplementationReady,
		}
		summary.Tasks[delKey] = summary.DelegationSummaries[delKey]
		r.mu.Unlock()
		return
	}

	falsResult, _ := experiments.EvaluateFalsification(baseSum, readySum)

	r.mu.Lock()
	summary.DelegationSummaries[delKey] = readySum
	summary.Tasks[delKey] = readySum
	if falsResult != nil {
		summary.FalsificationResults[delKey] = falsResult
	}
	r.mu.Unlock()
}

func (r *CampaignRunner) convertToSnapshot(
	spec CampaignSpec,
	res *benchmark.BenchmarkRunResult,
	task benchmark.BenchmarkTask,
	strat benchmark.ContextStrategyKind,
	cap experiments.CapabilityClass,
	defect *benchmark.SeededDefect,
	rep int,
	execErr error,
) telemetry.RunTelemetrySnapshot {
	defectID := "clean"
	if defect != nil {
		defectID = defect.DefectID
	}
	runID := fmt.Sprintf("%s-%s-%s-%s-%s-rep%d", spec.CampaignID, task.TaskID, cap, strat, defectID, rep)

	if res == nil || execErr != nil || res.Status == benchmark.RunStatusFailed {
		return telemetry.RunTelemetrySnapshot{
			RunID:        runID,
			TaskID:       task.TaskID,
			Strategy:     string(strat),
			Capability:   string(cap),
			Accepted:     false,
			DefectStatus: string(benchmark.DefectNotApplicable),
			DefectSeeded: defect != nil,
			Duration:     0,
		}
	}

	accepted := res.Passed && res.Status == benchmark.RunStatusCompleted
	defectStatus := string(res.DefectStatus)
	if defectStatus == "" {
		defectStatus = string(benchmark.DefectNotApplicable)
	}

	cumulativeInput := res.CumulativeInputTokens
	if cumulativeInput == 0 && res.TokenUsage.Input.Value > 0 {
		cumulativeInput = res.TokenUsage.Input.Value
	}

	return telemetry.RunTelemetrySnapshot{
		RunID:                 runID,
		TaskID:                res.TaskID,
		Strategy:              string(res.Strategy),
		Capability:            string(cap),
		InitialTokens:         res.InitialTokens,
		PeakResidentTokens:    res.PeakResidentTokens,
		CachedTokens:          res.TokenUsage.Cached.Value,
		OutputTokens:          res.TokenUsage.Output.Value,
		CumulativeInputTokens: cumulativeInput,
		Duration:              res.Duration,
		Turns:                 res.TurnsExecuted,
		DefectSeeded:          defect != nil,
		DefectStatus:          defectStatus,
		Accepted:              accepted,
		ReviewFindingsCount:   0,
		AccountingUncertain:   res.AccountingUncertain,
	}
}

func (r *CampaignRunner) aggregateSummary(summary *CampaignSummary) {
	report, err := telemetry.Aggregate(summary.Snapshots)
	if err == nil {
		summary.AggregatedReport = report
	}

	var (
		acceptedCount       int
		seededCount         int
		detectedCount       int
		sumCumulativeTokens int64
	)

	for _, s := range summary.Snapshots {
		if s.Accepted {
			acceptedCount++
		}
		if s.DefectSeeded {
			seededCount++
			if s.DefectStatus == "detected" || s.DefectStatus == "prevented" {
				detectedCount++
			}
		}
		sumCumulativeTokens += s.CumulativeInputTokens
	}

	if seededCount == 0 {
		summary.DefectCatchRate = 1.0
		summary.DefectCatchRateApplicable = false
	} else {
		// M-05 killed: divide by seededCount, NOT total runs / runCount
		summary.DefectCatchRate = float64(detectedCount) / float64(seededCount)
		summary.DefectCatchRateApplicable = true
	}

	if acceptedCount == 0 {
		summary.ResourcePerAcceptedResult = telemetry.ResourceEfficiency{
			Value:       0,
			IsUndefined: true,
		}
	} else {
		summary.ResourcePerAcceptedResult = telemetry.ResourceEfficiency{
			Value:       float64(sumCumulativeTokens) / float64(acceptedCount),
			IsUndefined: false,
		}
	}
}

func defectPtrs(defects []benchmark.SeededDefect) []*benchmark.SeededDefect {
	ptrs := make([]*benchmark.SeededDefect, len(defects))
	for i := range defects {
		d := defects[i]
		ptrs[i] = &d
	}
	return ptrs
}
