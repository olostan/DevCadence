package cognition

import (
	"fmt"

	"github.com/olostan/DevCadence/internal/protocol"
)

// WorkflowBudgetInput is the input for deterministic budget and metered-pool authorization (WP-M3D-2A2).
type WorkflowBudgetInput struct {
	Plan                     *protocol.WorkflowPlan
	Portfolio                *protocol.CognitionPortfolio
	BudgetStates             map[string]*protocol.BudgetState
	AllowUnknownLocalCompute bool
}

// WorkflowBudgetResult is the deterministic verdict for budget authorization.
type WorkflowBudgetResult struct {
	Authorized  bool
	Diagnostics []PortfolioDiagnostic
}

// AuthorizeWorkflowBudget validates a WorkflowPlan against live BudgetState records.
// It enforces fail-closed semantics for monetary and capped regimes, while permitting
// local compute regimes to proceed when AllowUnknownLocalCompute is enabled.
func AuthorizeWorkflowBudget(in WorkflowBudgetInput) WorkflowBudgetResult {
	if in.Plan == nil {
		return WorkflowBudgetResult{
			Authorized: false,
			Diagnostics: []PortfolioDiagnostic{{
				Code:         CodeWorkflowInputMissing,
				Condition:    ConditionInvalid,
				Target:       "plan",
				ViolatedRule: workflowRuleAuthority,
				Message:      "workflow plan is required",
			}},
		}
	}
	if in.Portfolio == nil {
		return WorkflowBudgetResult{
			Authorized: false,
			Diagnostics: []PortfolioDiagnostic{{
				Code:         CodeWorkflowInputMissing,
				Condition:    ConditionInvalid,
				Target:       "portfolio",
				ViolatedRule: workflowRuleAuthority,
				Message:      "cognition portfolio is required",
			}},
		}
	}

	if err := in.Plan.Validate(); err != nil {
		return WorkflowBudgetResult{
			Authorized: false,
			Diagnostics: []PortfolioDiagnostic{{
				Code:         CodeWorkflowPlanInvalid,
				Condition:    ConditionInvalid,
				Target:       "plan",
				ViolatedRule: workflowRuleAuthority,
				Message:      truncateRunes(err.Error(), workflowMaxMsgBytes),
			}},
		}
	}

	if err := in.Portfolio.Validate(); err != nil {
		return WorkflowBudgetResult{
			Authorized: false,
			Diagnostics: []PortfolioDiagnostic{{
				Code:         CodeWorkflowPortfolioInvalid,
				Condition:    ConditionInvalid,
				Target:       "portfolio",
				ViolatedRule: workflowRuleAuthority,
				Message:      truncateRunes(err.Error(), workflowMaxMsgBytes),
			}},
		}
	}

	poolMap := make(map[string]protocol.BudgetPool, len(in.Portfolio.BudgetPools))
	for _, p := range in.Portfolio.BudgetPools {
		poolMap[p.PoolID] = p
	}

	var diags []PortfolioDiagnostic

	for i, stage := range in.Plan.Stages {
		target := fmt.Sprintf("stages[%d]", i)
		pool, exists := poolMap[stage.BudgetPoolID]
		if !exists {
			diags = append(diags, PortfolioDiagnostic{
				Code:         CodeWorkflowUnknownBudgetPool,
				Condition:    ConditionInvalid,
				Target:       target,
				ViolatedRule: workflowRuleAuthority,
				Message:      fmt.Sprintf("stage %q references unknown budget pool %q", stage.StageID, stage.BudgetPoolID),
				Observed:     stage.BudgetPoolID,
				Required:     "existing budget pool",
			})
			continue
		}

		bs := in.BudgetStates[stage.BudgetPoolID]
		if bs == nil {
			if pool.Regime == protocol.RegimeLocalCompute && in.AllowUnknownLocalCompute {
				continue
			}
			diags = append(diags, PortfolioDiagnostic{
				Code:         CodeWorkflowBudgetMissingState,
				Condition:    ConditionUnauthorized,
				Target:       target,
				ViolatedRule: workflowRuleAuthority,
				Message:      fmt.Sprintf("budget pool %q has no live budget state", stage.BudgetPoolID),
				Observed:     "missing",
				Required:     "observed budget state",
			})
			continue
		}

		isExhausted := bs.Status == protocol.BudgetStatusExhausted || (bs.RemainingBalance != nil && *bs.RemainingBalance <= 0)
		if isExhausted {
			canOverage := pool.AllowOverage && (pool.Regime == protocol.RegimeMeteredAPI || pool.Regime == protocol.RegimeEnterpriseAllocation)
			if !canOverage {
				diags = append(diags, PortfolioDiagnostic{
					Code:         CodeWorkflowBudgetExhausted,
					Condition:    ConditionOverBudget,
					Target:       target,
					ViolatedRule: workflowRuleBounds,
					Message:      fmt.Sprintf("budget pool %q is exhausted", stage.BudgetPoolID),
					Observed:     string(bs.Status),
					Required:     "available budget",
				})
				continue
			}
		}

		if bs.Status == protocol.BudgetStatusUnknown {
			if pool.Regime == protocol.RegimeLocalCompute && in.AllowUnknownLocalCompute {
				continue
			}
			diags = append(diags, PortfolioDiagnostic{
				Code:         CodeWorkflowBudgetUnknown,
				Condition:    ConditionUnauthorized,
				Target:       target,
				ViolatedRule: workflowRuleAuthority,
				Message:      fmt.Sprintf("budget pool %q status is unknown", stage.BudgetPoolID),
				Observed:     string(protocol.BudgetStatusUnknown),
				Required:     "healthy budget status",
			})
			continue
		}
	}

	sorted := SortedDiagnostics(diags)
	return WorkflowBudgetResult{
		Authorized:  len(sorted) == 0,
		Diagnostics: sorted,
	}
}
