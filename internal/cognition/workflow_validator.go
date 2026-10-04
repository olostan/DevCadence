package cognition

import (
	"fmt"
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/olostan/DevCadence/internal/protocol"
)

// WorkflowPolicy carries the owner-supplied bounds a workflow plan must respect.
// It is supplied by the owner or Go code, never by a plan or a model.
type WorkflowPolicy struct {
	MaxStages              int `json:"max_stages"`
	MaxTotalRetries        int `json:"max_total_retries"`
	MaxStageTimeoutSeconds int `json:"max_stage_timeout_seconds"`
}

// DefaultWorkflowPolicy returns the documented conservative default bounds.
func DefaultWorkflowPolicy() WorkflowPolicy {
	return WorkflowPolicy{MaxStages: 8, MaxTotalRetries: 6, MaxStageTimeoutSeconds: 3600}
}

// WorkflowValidationInput is the complete input of the pure workflow validator.
type WorkflowValidationInput struct {
	Plan      *protocol.WorkflowPlan
	Portfolio *protocol.CognitionPortfolio
	Policy    *WorkflowPolicy
}

// WorkflowValidationResult is the deterministic verdict. It carries no timestamp.
type WorkflowValidationResult struct {
	Valid           bool
	Diagnostics     []PortfolioDiagnostic
	PlanDigest      string
	PortfolioDigest string
	PolicyDigest    string
}

// WorkflowValidator deterministically decides whether a plan is usable against a portfolio and policy.
type WorkflowValidator struct{}

// NewWorkflowValidator returns a WorkflowValidator.
func NewWorkflowValidator() WorkflowValidator { return WorkflowValidator{} }

const (
	workflowRuleAuthority = "DCI-123"
	workflowRuleBounds    = "FR-066"
	workflowMaxMsgBytes   = 256
)

// truncateRunes cuts s to at most max bytes at a rune boundary, without ellipsis.
func truncateRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func workflowStop(code, target, msg string) WorkflowValidationResult {
	return WorkflowValidationResult{Diagnostics: []PortfolioDiagnostic{{
		Code: code, Condition: ConditionInvalid, Target: target,
		ViolatedRule: workflowRuleAuthority, Message: msg,
	}}}
}

func workflowDigest(v any) string {
	b, err := protocol.CanonicalJSON(v)
	if err != nil {
		return ""
	}
	return "sha256:" + hashBytes(b)
}

func stageTarget(i int) string { return fmt.Sprintf("stages[%d]", i) }

func dashOr(p *string) string {
	if p == nil {
		return "-"
	}
	return *p
}

type bindingTuple struct{ endpoint, channel, profile, pool string }

func (c bindingTuple) matches(s protocol.WorkflowStage) bool {
	return (s.EndpointID == nil || *s.EndpointID == c.endpoint) &&
		(s.ChannelID == nil || *s.ChannelID == c.channel) &&
		(s.ContextProfileID == nil || *s.ContextProfileID == c.profile) &&
		s.BudgetPoolID == c.pool
}

// Validate evaluates R0..R7 of WP-M3D-2A1. It never mutates its input.
func (WorkflowValidator) Validate(in WorkflowValidationInput) WorkflowValidationResult {
	if in.Plan == nil {
		return workflowStop(CodeWorkflowInputMissing, "plan", "workflow plan is required")
	}
	if in.Portfolio == nil {
		return workflowStop(CodeWorkflowInputMissing, "portfolio", "cognition portfolio is required")
	}
	policy := DefaultWorkflowPolicy()
	if in.Policy != nil {
		p := *in.Policy
		if p.MaxStages <= 0 || p.MaxTotalRetries <= 0 || p.MaxStageTimeoutSeconds <= 0 {
			return workflowStop(CodeWorkflowPolicyInvalid, "policy",
				"every workflow policy field must be greater than zero")
		}
		policy = p
	}
	if err := in.Plan.Validate(); err != nil {
		return workflowStop(CodeWorkflowPlanInvalid, "plan", truncateRunes(err.Error(), workflowMaxMsgBytes))
	}
	if err := in.Portfolio.Validate(); err != nil {
		return workflowStop(CodeWorkflowPortfolioInvalid, "portfolio", truncateRunes(err.Error(), workflowMaxMsgBytes))
	}

	plan, port := in.Plan, in.Portfolio
	var diags []PortfolioDiagnostic
	add := func(d PortfolioDiagnostic) { diags = append(diags, d) }

	// R2
	if len(plan.Stages) > policy.MaxStages {
		add(PortfolioDiagnostic{Code: CodeWorkflowUnboundedStages, Condition: ConditionOverBudget, Target: "stages",
			ViolatedRule: workflowRuleBounds, Message: "workflow has more stages than the policy allows",
			Observed: strconv.Itoa(len(plan.Stages)), Required: "<= " + strconv.Itoa(policy.MaxStages)})
	}

	// R3 per-stage cap: the portfolio default when set (> 0), else the policy aggregate cap.
	retryCap := policy.MaxTotalRetries
	if port.WorkflowDefaults != nil && port.WorkflowDefaults.MaxRetries > 0 {
		retryCap = port.WorkflowDefaults.MaxRetries
	}

	roleTuples := map[string][]bindingTuple{}
	for _, rb := range port.RoleBindings {
		roleTuples[rb.Role] = append(roleTuples[rb.Role],
			bindingTuple{rb.EndpointID, rb.ChannelID, rb.ContextProfileID, rb.BudgetPoolID})
		for _, fb := range rb.Fallbacks {
			roleTuples[rb.Role] = append(roleTuples[rb.Role],
				bindingTuple{fb.EndpointID, fb.ChannelID, fb.ContextProfileID, fb.BudgetPoolID})
		}
	}
	pools := make(map[string]bool, len(port.BudgetPools))
	for _, bp := range port.BudgetPools {
		pools[bp.PoolID] = true
	}
	orderByID := make(map[string]int, len(plan.Stages))
	for _, s := range plan.Stages {
		orderByID[s.StageID] = s.Order
	}

	var retrySum int64
	for i, s := range plan.Stages {
		t := stageTarget(i)
		rl := int64(s.RetryLimit)
		if rl > int64(retryCap) {
			add(PortfolioDiagnostic{Code: CodeWorkflowRetryBound, Condition: ConditionOverBudget, Target: t,
				ViolatedRule: workflowRuleBounds, Message: "stage retry limit exceeds the per-stage cap",
				Observed: strconv.FormatInt(rl, 10), Required: "<= " + strconv.Itoa(retryCap)})
		}
		if retrySum > math.MaxInt64-rl {
			retrySum = math.MaxInt64
		} else {
			retrySum += rl
		}
		// R3b
		if s.TimeoutSeconds > policy.MaxStageTimeoutSeconds {
			add(PortfolioDiagnostic{Code: CodeWorkflowTimeoutBound, Condition: ConditionOverBudget, Target: t,
				ViolatedRule: workflowRuleBounds, Message: "stage timeout exceeds the policy cap",
				Observed: strconv.Itoa(s.TimeoutSeconds), Required: "<= " + strconv.Itoa(policy.MaxStageTimeoutSeconds)})
		}

		switch s.Kind {
		case protocol.StageKindCognition: // R4
			cands, bound := roleTuples[s.Role]
			if !bound {
				add(PortfolioDiagnostic{Code: CodeWorkflowRoleUnbound, Condition: ConditionUnauthorized, Target: t,
					ViolatedRule: workflowRuleAuthority, Message: "stage role has no role binding in the portfolio",
					Observed: s.Role})
				break
			}
			matched := false
			for _, c := range cands {
				if c.matches(s) {
					matched = true
					break
				}
			}
			if !matched {
				add(PortfolioDiagnostic{Code: CodeWorkflowBindingMismatch, Condition: ConditionUnauthorized, Target: t,
					ViolatedRule: workflowRuleAuthority, Message: "stage routing matches no binding tuple of its role",
					Observed: dashOr(s.EndpointID) + "/" + dashOr(s.ChannelID) + "/" + dashOr(s.ContextProfileID) + "/" + s.BudgetPoolID})
			}
		case protocol.StageKindDeterministic: // R4b
			if s.EndpointID != nil || s.ChannelID != nil || s.ContextProfileID != nil {
				add(PortfolioDiagnostic{Code: CodeWorkflowBindingMismatch, Condition: ConditionUnauthorized, Target: t,
					ViolatedRule: workflowRuleAuthority, Message: "deterministic stages route through no endpoint",
					Observed: "deterministic stage carries a cognition binding"})
			}
		}

		// R5
		if !pools[s.BudgetPoolID] {
			add(PortfolioDiagnostic{Code: CodeWorkflowUnknownBudgetPool, Condition: ConditionUnauthorized, Target: t,
				ViolatedRule: workflowRuleAuthority, Message: "stage budget pool does not exist in the portfolio",
				Observed: s.BudgetPoolID})
		}

		// R6
		if s.EscalationTarget != nil {
			ord, ok := orderByID[*s.EscalationTarget]
			if !ok || ord <= s.Order {
				add(PortfolioDiagnostic{Code: CodeWorkflowEscalationTarget, Condition: ConditionInvalid, Target: t,
					ViolatedRule: workflowRuleAuthority, Message: "escalation target must be an existing stage with a strictly greater order",
					Observed: *s.EscalationTarget})
			}
		}

		// R7
		if s.IsReview && s.Kind != protocol.StageKindCognition {
			add(PortfolioDiagnostic{Code: CodeWorkflowReviewNotCognition, Condition: ConditionInvalid, Target: t,
				ViolatedRule: workflowRuleAuthority, Message: "review stages must be cognition stages",
				Observed: string(s.Kind)})
		}
	}
	if retrySum > int64(policy.MaxTotalRetries) {
		add(PortfolioDiagnostic{Code: CodeWorkflowRetryBound, Condition: ConditionOverBudget, Target: "stages",
			ViolatedRule: workflowRuleBounds, Message: "total retries across stages exceed the policy cap",
			Observed: strconv.FormatInt(retrySum, 10), Required: "<= " + strconv.Itoa(policy.MaxTotalRetries)})
	}

	diags = SortedDiagnostics(diags)
	return WorkflowValidationResult{
		Valid:           len(diags) == 0,
		Diagnostics:     diags,
		PlanDigest:      workflowDigest(plan),
		PortfolioDigest: workflowDigest(port),
		PolicyDigest:    workflowDigest(policy),
	}
}
