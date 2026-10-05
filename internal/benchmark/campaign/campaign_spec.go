package campaign

import (
	"runtime"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/benchmark"
	"github.com/olostan/DevCadence/internal/benchmark/corpus"
	"github.com/olostan/DevCadence/internal/benchmark/experiments"
	"github.com/olostan/DevCadence/internal/benchmark/telemetry"
	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/errs"
)

// Re-exported strategy constants for campaign callers.
const (
	StrategyFullHistory  = benchmark.StrategyFullHistory
	StrategyCompacted    = benchmark.StrategyCompacted
	StrategyCompaction   = benchmark.StrategyCompacted
	StrategySnippetPool  = benchmark.StrategySnippetPool
	StrategyStaticActive = benchmark.StrategySnippetPool
	StrategyHybrid4Layer = benchmark.StrategyHybrid4Layer
)

// Re-exported capability constants for campaign callers.
const (
	CapabilityLocalSmall      = experiments.CapabilityLocalSmall
	CapabilitySubscriptionCLI = experiments.CapabilitySubscriptionCLI
	CapabilityFrontierAPI     = experiments.CapabilityFrontierAPI
)

// CampaignStatus identifies the lifecycle and execution outcome of a campaign.
type CampaignStatus string

const (
	CampaignStatusCompleted CampaignStatus = "completed"
	CampaignStatusFailed    CampaignStatus = "failed"
	CampaignStatusCancelled CampaignStatus = "cancelled"

	SummaryStatusCompleted = CampaignStatusCompleted
	SummaryStatusFailed    = CampaignStatusFailed
	SummaryStatusCancelled = CampaignStatusCancelled
)

// TaskSummary aliases experiments.ExperimentSummary for per-task summaries.
type TaskSummary = experiments.ExperimentSummary

// CampaignSpec defines the execution parameters for a multi-strategy campaign (REQ-01).
type CampaignSpec struct {
	CampaignID       string                          `json:"campaign_id"`
	Corpus           *corpus.CorpusRegistry          `json:"corpus"`
	Tasks            []benchmark.BenchmarkTask       `json:"tasks"`
	Strategies       []benchmark.ContextStrategyKind `json:"strategies"`
	Capabilities     []experiments.CapabilityClass   `json:"capabilities"`
	Repetitions      int                             `json:"repetitions"`
	MaxRepairRounds  int                             `json:"max_repair_rounds"`
	ConcurrencyLimit int                             `json:"concurrency_limit"`
	MaxConcurrency   int                             `json:"max_concurrency,omitempty"`
}

// Validate ensures that the campaign specification conforms to requirements (REQ-01).
func (s CampaignSpec) Validate() error {
	const kind = "CampaignSpec"
	if strings.TrimSpace(s.CampaignID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: campaign_id cannot be empty", kind)
	}
	if s.Corpus == nil {
		return errs.New(errs.CategoryInvalidArgument, "%s: corpus cannot be nil", kind)
	}
	if len(s.Tasks) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: tasks cannot be empty", kind)
	}
	for i, t := range s.Tasks {
		if strings.TrimSpace(t.TaskID) == "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: task[%d] task_id cannot be empty", kind, i)
		}
	}
	if len(s.Strategies) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: strategies cannot be empty", kind)
	}
	for _, strat := range s.Strategies {
		if !strat.Valid() {
			return errs.New(errs.CategoryInvalidArgument, "%s: invalid strategy %q", kind, strat)
		}
	}
	if len(s.Capabilities) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: capabilities cannot be empty", kind)
	}
	for _, cap := range s.Capabilities {
		if !cap.Valid() {
			return errs.New(errs.CategoryInvalidArgument, "%s: invalid capability %q", kind, cap)
		}
	}
	if s.Repetitions < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: repetitions must be >= 1, got %d", kind, s.Repetitions)
	}
	if s.MaxRepairRounds < 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: max_repair_rounds must be >= 0, got %d", kind, s.MaxRepairRounds)
	}

	limit := s.ConcurrencyLimit
	if limit <= 0 && s.MaxConcurrency > 0 {
		limit = s.MaxConcurrency
	}
	if limit < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: concurrency limit must be >= 1, got %d", kind, limit)
	}
	return nil
}

// EffectiveConcurrency returns the configured concurrency limit, defaulting to runtime.NumCPU() if <= 0.
func (s CampaignSpec) EffectiveConcurrency() int {
	if s.ConcurrencyLimit > 0 {
		return s.ConcurrencyLimit
	}
	if s.MaxConcurrency > 0 {
		return s.MaxConcurrency
	}
	n := runtime.NumCPU()
	if n < 1 {
		return 1
	}
	return n
}

// DriverProvider provides session drivers for capability tiers (REQ-02).
type DriverProvider interface {
	GetDriver(cap experiments.CapabilityClass) (drivers.SessionDriver, error)
}

// DriverProviderFunc allows using a function as a DriverProvider.
type DriverProviderFunc func(cap experiments.CapabilityClass) (drivers.SessionDriver, error)

// GetDriver implements DriverProvider.
func (f DriverProviderFunc) GetDriver(cap experiments.CapabilityClass) (drivers.SessionDriver, error) {
	return f(cap)
}

// CampaignSummary captures aggregated metrics, run snapshots, and delegation results across matrix permutations (REQ-03).
type CampaignSummary struct {
	CampaignID                string                                      `json:"campaign_id"`
	Status                    CampaignStatus                              `json:"status"`
	TotalRuns                 int                                         `json:"total_runs"`
	CompletedRuns             int                                         `json:"completed_runs"`
	FailedRuns                int                                         `json:"failed_runs"`
	Snapshots                 []telemetry.RunTelemetrySnapshot            `json:"snapshots"`
	DelegationSummaries       map[string]*experiments.ExperimentSummary   `json:"delegation_summaries"`
	FalsificationResults      map[string]*experiments.FalsificationResult `json:"falsification_results"`
	Tasks                     map[string]*experiments.ExperimentSummary   `json:"tasks,omitempty"`
	AggregatedReport          *telemetry.AggregatedReport                 `json:"aggregated_report,omitempty"`
	DefectCatchRate           float64                                     `json:"defect_catch_rate"`
	DefectCatchRateApplicable bool                                        `json:"defect_catch_rate_applicable"`
	ResourcePerAcceptedResult telemetry.ResourceEfficiency                `json:"resource_per_accepted_result"`
	Duration                  time.Duration                               `json:"duration"`
}
