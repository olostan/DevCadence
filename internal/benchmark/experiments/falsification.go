package experiments

import (
	"github.com/olostan/DevCadence/internal/errs"
)

// FalsificationResult represents the mathematical evaluation of the contract-completeness hypothesis (REQ-07).
type FalsificationResult struct {
	HypothesisFalsified bool              `json:"hypothesis_falsified"`
	Reason              string            `json:"reason"`
	BaselineSummary     ExperimentSummary `json:"baseline_summary"`
	ReadySummary        ExperimentSummary `json:"ready_summary"`
	DeltaFirstPassRate  float64           `json:"delta_first_pass_rate"`
	DeltaRepairRounds   float64           `json:"delta_repair_rounds"`
	DeltaArchFindings   float64           `json:"delta_arch_findings"`
	IsApplicable        bool              `json:"is_applicable"`
}

// EvaluateFalsification computes mathematical falsification criteria comparing baseline and ready experiment summaries (REQ-08, INV-02, INV-04).
//
// Invariants enforced:
// - Non-nil summaries required (returns errs.CategoryInvalidArgument).
// - INV-04: Tasks that pass trivially on baseline with 0 repairs cannot falsify the hypothesis (IsApplicable = false).
// - INV-02: Pure, deterministic, and immutable evaluation.
func EvaluateFalsification(baseline, ready *ExperimentSummary) (*FalsificationResult, error) {
	if baseline == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "falsification: baseline summary cannot be nil")
	}
	if ready == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "falsification: ready summary cannot be nil")
	}

	deltaFirstPass := ready.FirstPassRate - baseline.FirstPassRate
	deltaRepairs := ready.AvgRepairRounds - baseline.AvgRepairRounds
	deltaFindings := ready.AvgArchFindings - baseline.AvgArchFindings

	// Saturated baseline check (REQ-08, INV-04):
	// If baseline exhibited no repairs or defects (0 repairs and 100% first pass rate),
	// the task is ceiling-saturated and cannot test whether contract completeness lowers the delegation floor.
	if baseline.AvgRepairRounds == 0 && baseline.FirstPassRate == 1.0 {
		return &FalsificationResult{
			HypothesisFalsified: false,
			Reason:              "baseline task exhibited zero defects; cannot test delegation floor",
			BaselineSummary:     *baseline,
			ReadySummary:        *ready,
			DeltaFirstPassRate:  deltaFirstPass,
			DeltaRepairRounds:   deltaRepairs,
			DeltaArchFindings:   deltaFindings,
			IsApplicable:        false,
		}, nil
	}

	// Mathematical falsification criteria (REQ-08):
	// If ready repair rounds >= baseline OR ready findings >= baseline OR ready first-pass <= baseline
	// (with margin >= 0.05 when Repetitions >= 3), hypothesis is falsified.
	falsified := false
	if deltaRepairs >= 0 || deltaFindings >= 0 {
		falsified = true
	} else if (baseline.Repetitions >= 3 || ready.Repetitions >= 3) && deltaFirstPass < 0.05-1e-9 {
		falsified = true
	} else if deltaFirstPass <= 0 {
		falsified = true
	}

	if falsified {
		return &FalsificationResult{
			HypothesisFalsified: true,
			Reason:              "implementation-ready contract failed to reduce repair rounds or improve first-pass acceptance",
			BaselineSummary:     *baseline,
			ReadySummary:        *ready,
			DeltaFirstPassRate:  deltaFirstPass,
			DeltaRepairRounds:   deltaRepairs,
			DeltaArchFindings:   deltaFindings,
			IsApplicable:        true,
		}, nil
	}

	return &FalsificationResult{
		HypothesisFalsified: false,
		Reason:              "hypothesis supported: implementation-ready contract lowered delegation floor",
		BaselineSummary:     *baseline,
		ReadySummary:        *ready,
		DeltaFirstPassRate:  deltaFirstPass,
		DeltaRepairRounds:   deltaRepairs,
		DeltaArchFindings:   deltaFindings,
		IsApplicable:        true,
	}, nil
}
