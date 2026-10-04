package cognition

// Stable machine-readable reason codes for workflow plan validation (WP-M3D-2A1).
const (
	CodeWorkflowInputMissing       = "WORKFLOW_INPUT_MISSING"
	CodeWorkflowPolicyInvalid      = "WORKFLOW_POLICY_INVALID"
	CodeWorkflowPlanInvalid        = "WORKFLOW_PLAN_INVALID"
	CodeWorkflowPortfolioInvalid   = "WORKFLOW_PORTFOLIO_INVALID"
	CodeWorkflowUnboundedStages    = "WORKFLOW_UNBOUNDED_STAGES"
	CodeWorkflowRetryBound         = "WORKFLOW_RETRY_BOUND"
	CodeWorkflowTimeoutBound       = "WORKFLOW_TIMEOUT_BOUND"
	CodeWorkflowRoleUnbound        = "WORKFLOW_ROLE_UNBOUND"
	CodeWorkflowBindingMismatch    = "WORKFLOW_BINDING_MISMATCH"
	CodeWorkflowUnknownBudgetPool  = "WORKFLOW_UNKNOWN_BUDGET_POOL"
	CodeWorkflowEscalationTarget   = "WORKFLOW_ESCALATION_TARGET"
	CodeWorkflowReviewNotCognition = "WORKFLOW_REVIEW_NOT_COGNITION"
)
