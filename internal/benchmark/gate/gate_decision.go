package gate

import (
	"math"
	"time"

	"github.com/olostan/DevCadence/internal/benchmark/experiments"
	"github.com/olostan/DevCadence/internal/benchmark/telemetry"
	"github.com/olostan/DevCadence/internal/errs"
)

// GateDecision represents the formal Milestone M4 evidence gate outcome (REQ-01).
type GateDecision string

const (
	// DecisionGo indicates all quality, efficiency, and floor criteria passed.
	DecisionGo GateDecision = "go"
	// DecisionRevise indicates one or more criteria failed; recommendations produced.
	DecisionRevise GateDecision = "revise"
	// DecisionInconclusive indicates insufficient completed runs to evaluate criteria.
	DecisionInconclusive GateDecision = "inconclusive"

	// Alias for prompt compatibility:
	DecisionPivot = DecisionRevise
)

// Valid reports whether the gate decision is known (REQ-01).
func (d GateDecision) Valid() bool {
	switch d {
	case DecisionGo, DecisionRevise, DecisionInconclusive:
		return true
	default:
		return false
	}
}

// GateCriteria specifies the quantitative thresholds for Milestone M4 gate evaluation (REQ-02).
type GateCriteria struct {
	MinCompletedRuns                int     `json:"min_completed_runs"`
	MinDefectCatchRate              float64 `json:"min_defect_catch_rate"`
	MaxResourceRatioVersusBaseline  float64 `json:"max_resource_ratio_versus_baseline"`
	RequireZeroFalsifications       bool    `json:"require_zero_falsifications"`
	MaxFalsificationRate            float64 `json:"max_falsification_rate,omitempty"`
	MaxResidentContextRatioBaseline float64 `json:"max_resident_context_ratio_versus_baseline,omitempty"`
}

// DefaultM4GateCriteria returns the normative thresholds defined in docs/IMPLEMENTATION_PLAN.md § M4 (REQ-02).
func DefaultM4GateCriteria() GateCriteria {
	return GateCriteria{
		MinCompletedRuns:                10,
		MinDefectCatchRate:              0.80,
		MaxResourceRatioVersusBaseline:  1.00,
		RequireZeroFalsifications:       true,
		MaxFalsificationRate:            0.00,
		MaxResidentContextRatioBaseline: 1.00,
	}
}

// CriterionResult records the outcome of an individual gate criterion check (REQ-03).
type CriterionResult struct {
	Name      string  `json:"name"`
	Passed    bool    `json:"passed"`
	Threshold float64 `json:"threshold"`
	Observed  float64 `json:"observed"`
	// ObservedUndefined marks criteria whose observed value is undefined, missing or malformed;
	// Observed is then 0 (never +Inf, which is not JSON-serializable).
	ObservedUndefined bool   `json:"observed_undefined,omitempty"`
	Details           string `json:"details,omitempty"`
}

// GateEvaluationResult encapsulates the overall decision, criterion breakdown, and recommendations (REQ-03).
type GateEvaluationResult struct {
	Decision            GateDecision      `json:"decision"`
	CriteriaEvaluations []CriterionResult `json:"criteria_evaluations"`
	// FalsificationResults preserves the delegation-floor inputs so a persisted result can be
	// re-evaluated (reproducibility); the key matches campaign.CampaignSummary.
	FalsificationResults map[string]*experiments.FalsificationResult `json:"falsification_results,omitempty"`
	Summary              string                                      `json:"summary"`
	Recommendations      []string                                    `json:"recommendations,omitempty"`
	AggregatedReport     *telemetry.AggregatedReport                 `json:"aggregated_report,omitempty"`
	ReportDigest         string                                      `json:"report_digest,omitempty"`
	// Provenance states what kind of evidence was evaluated; nil means unspecified and is
	// never rendered as empirical proof.
	Provenance  *EvidenceProvenance `json:"provenance,omitempty"`
	EvaluatedAt time.Time           `json:"evaluated_at"`
}

func badFloat(v float64) bool { return math.IsNaN(v) || math.IsInf(v, 0) }

// Validate fails closed on criteria that would silently disable a check (REQ-08).
func (c GateCriteria) Validate() error {
	if c.MinCompletedRuns < 1 {
		return errs.New(errs.CategoryInvalidArgument, "min_completed_runs must be >= 1, got %d", c.MinCompletedRuns)
	}
	if badFloat(c.MinDefectCatchRate) || c.MinDefectCatchRate < 0 || c.MinDefectCatchRate > 1 {
		return errs.New(errs.CategoryInvalidArgument, "min_defect_catch_rate must be between 0.0 and 1.0, got %v", c.MinDefectCatchRate)
	}
	if badFloat(c.MaxResourceRatioVersusBaseline) || c.MaxResourceRatioVersusBaseline <= 0 {
		return errs.New(errs.CategoryInvalidArgument, "max_resource_ratio_versus_baseline must be finite and > 0.0, got %v", c.MaxResourceRatioVersusBaseline)
	}
	if badFloat(c.MaxFalsificationRate) || c.MaxFalsificationRate < 0 || c.MaxFalsificationRate > 1 {
		return errs.New(errs.CategoryInvalidArgument, "max_falsification_rate must be between 0.0 and 1.0, got %v", c.MaxFalsificationRate)
	}
	if badFloat(c.MaxResidentContextRatioBaseline) || c.MaxResidentContextRatioBaseline < 0 {
		return errs.New(errs.CategoryInvalidArgument, "max_resident_context_ratio_versus_baseline must be finite and >= 0.0, got %v", c.MaxResidentContextRatioBaseline)
	}
	return nil
}
