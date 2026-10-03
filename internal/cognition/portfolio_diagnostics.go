package cognition

import (
	"fmt"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
)

// DiagnosticCondition classifies the nature of a validation failure.
type DiagnosticCondition string

const (
	ConditionInvalid      DiagnosticCondition = "invalid"
	ConditionUnsupported  DiagnosticCondition = "unsupported"
	ConditionUnauthorized DiagnosticCondition = "unauthorized"
	ConditionUnknown      DiagnosticCondition = "unknown"
	ConditionOverBudget   DiagnosticCondition = "over_budget"
)

// Valid reports whether the diagnostic condition is recognized.
func (c DiagnosticCondition) Valid() bool {
	switch c {
	case ConditionInvalid, ConditionUnsupported, ConditionUnauthorized, ConditionUnknown, ConditionOverBudget:
		return true
	}
	return false
}

// Stable machine-readable reason codes for portfolio validation rejections.
const (
	CodeEndpointNotFound            = "ENDPOINT_NOT_FOUND"
	CodeEndpointUnhealthy           = "ENDPOINT_UNHEALTHY"
	CodeEndpointUnauthenticated     = "ENDPOINT_UNAUTHENTICATED"
	CodeCapabilityMissing           = "CAPABILITY_MISSING"
	CodeCapabilityGradeInsufficient = "CAPABILITY_GRADE_INSUFFICIENT"
	CodeCapabilityProvenanceInvalid = "CAPABILITY_PROVENANCE_INVALID"
	CodeToolSupportMissing          = "TOOL_SUPPORT_MISSING"
	CodeStructuredOutputMissing     = "STRUCTURED_OUTPUT_MISSING"
	CodeAccelerationUnverified      = "ACCELERATION_UNVERIFIED"
	CodeUnauthorizedSourceExposure  = "UNAUTHORIZED_SOURCE_EXPOSURE"
	CodeUnauthorizedCostClass       = "UNAUTHORIZED_COST_CLASS"
	CodeUnauthorizedEconomicRegime  = "UNAUTHORIZED_ECONOMIC_REGIME"
	CodeUnauthorizedMeteredFallback = "UNAUTHORIZED_METERED_FALLBACK"
	CodeBudgetPoolNotFound          = "BUDGET_POOL_NOT_FOUND"
	CodeBudgetPoolExhausted         = "BUDGET_POOL_EXHAUSTED"
	CodeBudgetReservationExceeded   = "BUDGET_RESERVATION_EXCEEDED"
	CodeContextProfileNotFound      = "CONTEXT_PROFILE_NOT_FOUND"
	CodeContextControlMismatch      = "CONTEXT_CONTROL_MISMATCH"
	CodePrefixCacheMismatch         = "PREFIX_CACHE_MISMATCH"
	CodeContextWindowInsufficient   = "CONTEXT_WINDOW_INSUFFICIENT"
	CodeHardwareBackendUnavailable  = "HARDWARE_BACKEND_UNAVAILABLE"
	CodeResourceCapacityExceeded    = "RESOURCE_CAPACITY_EXCEEDED"
	CodeUnknownResourceState        = "UNKNOWN_RESOURCE_STATE"
	CodePortfolioStructureInvalid   = "PORTFOLIO_STRUCTURE_INVALID"
	CodeDiversityViolation          = "DIVERSITY_VIOLATION"
	CodeEscalationRuleInvalid       = "ESCALATION_RULE_INVALID"
	CodeStaleValidationState        = "STALE_VALIDATION_STATE"
)

// PortfolioDiagnostic describes a single deterministic failure in a candidate portfolio.
type PortfolioDiagnostic struct {
	Code         string              `json:"code"`
	Condition    DiagnosticCondition `json:"condition"`
	Target       string              `json:"target"`
	ViolatedRule string              `json:"violated_rule"`
	Message      string              `json:"message"`
	Observed     string              `json:"observed,omitempty"`
	Required     string              `json:"required,omitempty"`
}

// String formats the diagnostic as a human-readable explanation.
func (d PortfolioDiagnostic) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] (%s) %s: %s", d.Code, d.Condition, d.Target, d.Message)
	if d.ViolatedRule != "" {
		fmt.Fprintf(&b, " (rule: %s)", d.ViolatedRule)
	}
	if d.Observed != "" || d.Required != "" {
		fmt.Fprintf(&b, " [observed: %s, required: %s]", d.Observed, d.Required)
	}
	return b.String()
}

// ValidationResult captures the complete deterministic validation outcome.
type ValidationResult struct {
	Valid           bool                  `json:"valid"`
	Diagnostics     []PortfolioDiagnostic `json:"diagnostics,omitempty"`
	ValidatedAt     string                `json:"validated_at"`
	CandidateDigest string                `json:"candidate_digest"`
	InventoryDigest string                `json:"inventory_digest"`
	PolicyDigest    string                `json:"policy_digest,omitempty"`
}

// Summary returns a concise human-readable summary of the validation result.
func (v ValidationResult) Summary() string {
	if v.Valid {
		return "Portfolio is valid and policy-compliant"
	}
	reasons := make([]string, len(v.Diagnostics))
	for i, d := range v.Diagnostics {
		reasons[i] = d.String()
	}
	return fmt.Sprintf("Portfolio validation failed (%d issues):\n- %s",
		len(v.Diagnostics), strings.Join(reasons, "\n- "))
}

// Err converts an invalid result into a typed error for control-plane routing.
func (v ValidationResult) Err() error {
	if v.Valid {
		return nil
	}
	// Select CategoryPolicyDenied if unauthorized conditions exist, else CategoryValidationFailed
	cat := errs.CategoryValidationFailed
	for _, d := range v.Diagnostics {
		if d.Condition == ConditionUnauthorized {
			cat = errs.CategoryPolicyDenied
			break
		}
	}
	return errs.New(cat, "%s", v.Summary())
}
