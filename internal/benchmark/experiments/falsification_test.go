package experiments

import (
	"reflect"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
)

// TestEvaluateFalsification_NilInputs verifies fail-closed validation on nil summaries (REQ-08, REQ-10).
func TestEvaluateFalsification_NilInputs(t *testing.T) {
	valid := &ExperimentSummary{
		ExperimentID:    "exp-1",
		Capability:      CapabilityLocalSmall,
		ContractLevel:   ContractImplementationReady,
		Repetitions:     3,
		FirstPassRate:   0.8,
		AvgRepairRounds: 0.2,
		AvgArchFindings: 0.0,
	}

	if _, err := EvaluateFalsification(nil, valid); err == nil {
		t.Fatalf("expected error on nil baseline, got nil")
	} else if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("expected CategoryInvalidArgument for nil baseline, got %v", err)
	}

	if _, err := EvaluateFalsification(valid, nil); err == nil {
		t.Fatalf("expected error on nil ready, got nil")
	} else if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("expected CategoryInvalidArgument for nil ready, got %v", err)
	}
}

// TestACC02_EvaluateFalsification_ReadyHigherRepairs tests ACC-02:
// Experiment summaries where ready contract exhibits higher repair rounds than baseline (N >= 3)
// MUST return HypothesisFalsified: true, IsApplicable: true with explicit rationale.
// Kills Mutant 1 (Falsification inverted).
func TestACC02_EvaluateFalsification_ReadyHigherRepairs(t *testing.T) {
	baseline := &ExperimentSummary{
		ExperimentID:    "exp-baseline",
		Capability:      CapabilityLocalSmall,
		ContractLevel:   ContractBaseline,
		Repetitions:     3,
		FirstPassRate:   0.67,
		AvgRepairRounds: 0.33,
		AvgArchFindings: 0.33,
	}
	ready := &ExperimentSummary{
		ExperimentID:    "exp-ready",
		Capability:      CapabilityLocalSmall,
		ContractLevel:   ContractImplementationReady,
		Repetitions:     3,
		FirstPassRate:   0.67,
		AvgRepairRounds: 1.0, // Higher repair rounds than baseline!
		AvgArchFindings: 0.0,
	}

	res, err := EvaluateFalsification(baseline, ready)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !res.IsApplicable {
		t.Errorf("expected IsApplicable to be true, got false")
	}
	if !res.HypothesisFalsified {
		t.Errorf("expected HypothesisFalsified to be true (Mutant 1 alive if false), got false")
	}
	if res.Reason == "" {
		t.Errorf("expected non-empty reason")
	}
	if res.DeltaRepairRounds <= 0 {
		t.Errorf("expected positive DeltaRepairRounds, got %f", res.DeltaRepairRounds)
	}
}

// TestACC03_EvaluateFalsification_SaturatedBaseline tests ACC-03:
// Baseline summary with 0 repair rounds and 100% first pass (saturated task)
// MUST return IsApplicable: false, HypothesisFalsified: false.
// Kills Mutant 2 (Saturated baseline treated as falsifying hypothesis).
func TestACC03_EvaluateFalsification_SaturatedBaseline(t *testing.T) {
	baseline := &ExperimentSummary{
		ExperimentID:    "exp-baseline",
		Capability:      CapabilityLocalSmall,
		ContractLevel:   ContractBaseline,
		Repetitions:     5,
		FirstPassRate:   1.0, // Saturated: 100% first pass
		AvgRepairRounds: 0.0, // Saturated: 0 repairs
		AvgArchFindings: 0.0,
	}
	ready := &ExperimentSummary{
		ExperimentID:    "exp-ready",
		Capability:      CapabilityLocalSmall,
		ContractLevel:   ContractImplementationReady,
		Repetitions:     5,
		FirstPassRate:   1.0,
		AvgRepairRounds: 0.0,
		AvgArchFindings: 0.0,
	}

	res, err := EvaluateFalsification(baseline, ready)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.IsApplicable {
		t.Errorf("expected IsApplicable to be false (Mutant 2 alive if true), got true")
	}
	if res.HypothesisFalsified {
		t.Errorf("expected HypothesisFalsified to be false on saturated baseline, got true")
	}
	if res.Reason != "baseline task exhibited zero defects; cannot test delegation floor" {
		t.Errorf("unexpected reason: %q", res.Reason)
	}
}

// TestEvaluateFalsification_HigherFindings tests falsification when ready contract produces >= findings.
func TestEvaluateFalsification_HigherFindings(t *testing.T) {
	baseline := &ExperimentSummary{
		ExperimentID:    "exp-baseline",
		Capability:      CapabilityLocalSmall,
		ContractLevel:   ContractBaseline,
		Repetitions:     3,
		FirstPassRate:   0.33,
		AvgRepairRounds: 1.0,
		AvgArchFindings: 0.5,
	}
	ready := &ExperimentSummary{
		ExperimentID:    "exp-ready",
		Capability:      CapabilityLocalSmall,
		ContractLevel:   ContractImplementationReady,
		Repetitions:     3,
		FirstPassRate:   0.67,
		AvgRepairRounds: 0.33,
		AvgArchFindings: 0.67, // Higher findings than baseline!
	}

	res, err := EvaluateFalsification(baseline, ready)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !res.IsApplicable {
		t.Errorf("expected IsApplicable to be true, got false")
	}
	if !res.HypothesisFalsified {
		t.Errorf("expected HypothesisFalsified to be true on higher findings, got false")
	}
}

// TestEvaluateFalsification_MarginRequirement tests the >= 0.05 margin when Repetitions >= 3.
func TestEvaluateFalsification_MarginRequirement(t *testing.T) {
	// Baseline and Ready with Repetitions >= 3
	baseline := &ExperimentSummary{
		ExperimentID:    "exp-baseline",
		Capability:      CapabilityLocalSmall,
		ContractLevel:   ContractBaseline,
		Repetitions:     4,
		FirstPassRate:   0.50,
		AvgRepairRounds: 1.0,
		AvgArchFindings: 1.0,
	}
	// Case 1: First-pass improved by only 0.02 (< 0.05 margin) -> Falsified!
	readyNarrow := &ExperimentSummary{
		ExperimentID:    "exp-ready-narrow",
		Capability:      CapabilityLocalSmall,
		ContractLevel:   ContractImplementationReady,
		Repetitions:     4,
		FirstPassRate:   0.52,
		AvgRepairRounds: 0.5,
		AvgArchFindings: 0.5,
	}

	res, err := EvaluateFalsification(baseline, readyNarrow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.HypothesisFalsified {
		t.Errorf("expected narrow margin (< 0.05) with N=4 to falsify hypothesis, got false")
	}

	// Case 2: First-pass improved by 0.10 (>= 0.05 margin) -> Supported!
	readyWide := &ExperimentSummary{
		ExperimentID:    "exp-ready-wide",
		Capability:      CapabilityLocalSmall,
		ContractLevel:   ContractImplementationReady,
		Repetitions:     4,
		FirstPassRate:   0.60,
		AvgRepairRounds: 0.5,
		AvgArchFindings: 0.5,
	}

	res2, err := EvaluateFalsification(baseline, readyWide)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res2.HypothesisFalsified {
		t.Errorf("expected wide margin (>= 0.05) to support hypothesis, got falsified")
	}
	if !res2.IsApplicable {
		t.Errorf("expected IsApplicable to be true, got false")
	}
}

// TestINV02_EvaluateFalsification_PureAndImmutable tests INV-02:
// Falsification evaluation is pure, deterministic, and immutable.
func TestINV02_EvaluateFalsification_PureAndImmutable(t *testing.T) {
	baseline := &ExperimentSummary{
		ExperimentID:    "exp-baseline",
		Capability:      CapabilityLocalSmall,
		ContractLevel:   ContractBaseline,
		Repetitions:     3,
		FirstPassRate:   0.33,
		AvgRepairRounds: 1.0,
		AvgArchFindings: 1.0,
	}
	ready := &ExperimentSummary{
		ExperimentID:    "exp-ready",
		Capability:      CapabilityLocalSmall,
		ContractLevel:   ContractImplementationReady,
		Repetitions:     3,
		FirstPassRate:   1.0,
		AvgRepairRounds: 0.0,
		AvgArchFindings: 0.0,
	}

	// Snapshot values
	origBaselineRate := baseline.FirstPassRate
	origReadyRate := ready.FirstPassRate

	res1, err1 := EvaluateFalsification(baseline, ready)
	res2, err2 := EvaluateFalsification(baseline, ready)

	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected errors: %v, %v", err1, err2)
	}

	if !reflect.DeepEqual(res1, res2) {
		t.Errorf("expected deterministic identical results, got %+v vs %+v", res1, res2)
	}

	if baseline.FirstPassRate != origBaselineRate || ready.FirstPassRate != origReadyRate {
		t.Errorf("mutation detected in input summaries (violates INV-02)")
	}
}

// TestEvaluateFalsification_SmallRepetitions_NonPositiveDeltaFirstPass tests falsification
// when Repetitions < 3 and deltaFirstPass <= 0 (REQ-08).
func TestEvaluateFalsification_SmallRepetitions_NonPositiveDeltaFirstPass(t *testing.T) {
	baseline := &ExperimentSummary{
		ExperimentID:    "exp-base-small",
		Capability:      CapabilityLocalSmall,
		ContractLevel:   ContractBaseline,
		Repetitions:     2,
		FirstPassRate:   0.50,
		AvgRepairRounds: 1.0,
		AvgArchFindings: 1.0,
	}
	ready := &ExperimentSummary{
		ExperimentID:    "exp-ready-small",
		Capability:      CapabilityLocalSmall,
		ContractLevel:   ContractImplementationReady,
		Repetitions:     2,
		FirstPassRate:   0.50, // deltaFirstPass == 0.0 with Repetitions < 3
		AvgRepairRounds: 0.5,
		AvgArchFindings: 0.5,
	}

	res, err := EvaluateFalsification(baseline, ready)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.HypothesisFalsified {
		t.Errorf("expected HypothesisFalsified to be true for deltaFirstPass <= 0 when N < 3, got false")
	}
	if !res.IsApplicable {
		t.Errorf("expected IsApplicable to be true, got false")
	}
}
