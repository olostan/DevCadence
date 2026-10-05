package telemetry

import (
	"encoding/json"
	"time"
)

// LayerBreakdownMetrics breaks down tokens across the architectural context layers (REQ-01).
type LayerBreakdownMetrics struct {
	ProtectedCoreTokens      int64 `json:"protected_core_tokens"`
	StateCapsuleTokens       int64 `json:"state_capsule_tokens"`
	EvidenceWorkingSetTokens int64 `json:"evidence_working_set_tokens"`
	EphemeralTailTokens      int64 `json:"ephemeral_tail_tokens"`
	TotalTokens              int64 `json:"total_tokens"`
}

// RunTelemetrySnapshot captures full empirical metrics for a single benchmark run (REQ-02).
type RunTelemetrySnapshot struct {
	RunID                 string                 `json:"run_id"`
	TaskID                string                 `json:"task_id"`
	Strategy              string                 `json:"strategy"`
	Capability            string                 `json:"capability"`
	InitialTokens         int64                  `json:"initial_tokens"`
	PeakResidentTokens    int64                  `json:"peak_resident_tokens"`
	CachedTokens          int64                  `json:"cached_tokens"`
	OutputTokens          int64                  `json:"output_tokens"`
	CumulativeInputTokens int64                  `json:"cumulative_input_tokens"`
	Duration              time.Duration          `json:"duration"`
	Turns                 int                    `json:"turns"`
	DefectSeeded          bool                   `json:"defect_seeded"`
	DefectStatus          string                 `json:"defect_status"`
	Accepted              bool                   `json:"accepted"`
	ReviewFindingsCount   int                    `json:"review_findings_count"`
	LayerBreakdown        *LayerBreakdownMetrics `json:"layer_breakdown,omitempty"`
	AccountingUncertain   bool                   `json:"accounting_uncertain"`
}

// ResourceEfficiency represents total resource-to-accepted-result ratio (REQ-04).
// If AcceptedCount is 0, IsUndefined is true and Value is 0 to avoid +Inf/NaN in JSON output.
type ResourceEfficiency struct {
	Value       float64 `json:"value"`
	IsUndefined bool    `json:"is_undefined"`
}

// MarshalJSON guarantees clean serialization avoiding NaN/Inf (REQ-04).
func (r ResourceEfficiency) MarshalJSON() ([]byte, error) {
	type alias ResourceEfficiency
	return json.Marshal(alias(r))
}

// AggregatedTelemetry contains grouped summary metrics across runs with the same (Strategy, Capability) (REQ-05).
type AggregatedTelemetry struct {
	Strategy                    string             `json:"strategy"`
	Capability                  string             `json:"capability"`
	RunCount                    int                `json:"run_count"`
	AcceptedCount               int                `json:"accepted_count"`
	FirstPassAcceptanceRate     float64            `json:"first_pass_acceptance_rate"`
	DefectCatchRate             float64            `json:"defect_catch_rate"`
	DefectCatchRateApplicable   bool               `json:"defect_catch_rate_applicable"`
	CacheHitRatio               float64            `json:"cache_hit_ratio"`
	AvgInitialTokens            float64            `json:"avg_initial_tokens"`
	AvgPeakResidentTokens       float64            `json:"avg_peak_resident_tokens"`
	AvgCumulativeInputTokens    float64            `json:"avg_cumulative_input_tokens"`
	AvgDuration                 time.Duration      `json:"avg_duration"`
	ResourcePerAcceptedResult   ResourceEfficiency `json:"resource_per_accepted_result"`
	ContainsUncertainAccounting bool               `json:"contains_uncertain_accounting"`
}

// AggregatedReport synthesizes aggregated metric groups across benchmark runs (REQ-06).
type AggregatedReport struct {
	GeneratedAt    time.Time             `json:"generated_at"`
	TotalSnapshots int                   `json:"total_snapshots"`
	Groups         []AggregatedTelemetry `json:"groups"`
}
