package cognition

import (
	"fmt"

	"github.com/olostan/DevCadence/internal/protocol"
)

// validatePolicyAndEconomics validates dimensions 4 and 5:
// source-exposure/privacy policy, and economic/budget bindings.
func (ctx *validatorContext) validatePolicyAndEconomics(diags *[]PortfolioDiagnostic) {
	p := ctx.p

	// 4. Source-exposure / Privacy Policy
	if ctx.input.Policy != nil && ctx.input.Policy.MaxSourceExposure != "" {
		if p.MaxSourceExposure.ExposureRank() > ctx.input.Policy.MaxSourceExposure.ExposureRank() {
			*diags = append(*diags, PortfolioDiagnostic{
				Code:         CodeUnauthorizedSourceExposure,
				Condition:    ConditionUnauthorized,
				Target:       "portfolio.max_source_exposure",
				ViolatedRule: "DCI-080, DCI-124",
				Message: fmt.Sprintf("portfolio max_source_exposure %q exceeds policy limit %q",
					p.MaxSourceExposure, ctx.input.Policy.MaxSourceExposure),
				Observed: string(p.MaxSourceExposure),
				Required: string(ctx.input.Policy.MaxSourceExposure),
			})
		}
	}

	getEndpointExposure := func(epID string) protocol.SourceExposure {
		if ep, ok := ctx.profileEndpoints[epID]; ok {
			return ep.RequiredSourceExposure
		}
		if epSum, ok := ctx.inventoryEndpoints[epID]; ok {
			return epSum.RequiredSourceExposure
		}
		return protocol.ExposureLocalOnly
	}

	getEndpointCostClass := func(epID string) protocol.CostClass {
		if ep, ok := ctx.profileEndpoints[epID]; ok {
			return ep.CostClass
		}
		if epSum, ok := ctx.inventoryEndpoints[epID]; ok {
			return epSum.CostClass
		}
		return protocol.CostLocalCompute
	}

	for i, rb := range p.RoleBindings {
		target := fmt.Sprintf("role_bindings[%d]", i)
		epExposure := getEndpointExposure(rb.EndpointID)
		if epExposure.ExposureRank() > p.MaxSourceExposure.ExposureRank() {
			*diags = append(*diags, PortfolioDiagnostic{
				Code:         CodeUnauthorizedSourceExposure,
				Condition:    ConditionUnauthorized,
				Target:       target,
				ViolatedRule: "DCI-080, DCI-124",
				Message: fmt.Sprintf("endpoint %q requires source exposure %q, which exceeds portfolio limit %q",
					rb.EndpointID, epExposure, p.MaxSourceExposure),
				Observed: string(epExposure),
				Required: string(p.MaxSourceExposure),
			})
		}
		if ctx.input.Policy != nil && ctx.input.Policy.MaxSourceExposure != "" && epExposure.ExposureRank() > ctx.input.Policy.MaxSourceExposure.ExposureRank() {
			*diags = append(*diags, PortfolioDiagnostic{
				Code:         CodeUnauthorizedSourceExposure,
				Condition:    ConditionUnauthorized,
				Target:       target,
				ViolatedRule: "DCI-080, DCI-124",
				Message: fmt.Sprintf("endpoint %q requires source exposure %q, which exceeds project policy limit %q",
					rb.EndpointID, epExposure, ctx.input.Policy.MaxSourceExposure),
				Observed: string(epExposure),
				Required: string(ctx.input.Policy.MaxSourceExposure),
			})
		}

		for j, fb := range rb.Fallbacks {
			fbTarget := fmt.Sprintf("role_bindings[%d].fallbacks[%d]", i, j)
			fbExposure := getEndpointExposure(fb.EndpointID)
			if fbExposure.ExposureRank() > p.MaxSourceExposure.ExposureRank() {
				*diags = append(*diags, PortfolioDiagnostic{
					Code:         CodeUnauthorizedSourceExposure,
					Condition:    ConditionUnauthorized,
					Target:       fbTarget,
					ViolatedRule: "DCI-080, DCI-124",
					Message: fmt.Sprintf("fallback endpoint %q requires source exposure %q, which exceeds portfolio limit %q",
						fb.EndpointID, fbExposure, p.MaxSourceExposure),
					Observed: string(fbExposure),
					Required: string(p.MaxSourceExposure),
				})
			}
			if ctx.input.Policy != nil && ctx.input.Policy.MaxSourceExposure != "" && fbExposure.ExposureRank() > ctx.input.Policy.MaxSourceExposure.ExposureRank() {
				*diags = append(*diags, PortfolioDiagnostic{
					Code:         CodeUnauthorizedSourceExposure,
					Condition:    ConditionUnauthorized,
					Target:       fbTarget,
					ViolatedRule: "DCI-080, DCI-124",
					Message: fmt.Sprintf("fallback endpoint %q requires source exposure %q, which exceeds project policy limit %q",
						fb.EndpointID, fbExposure, ctx.input.Policy.MaxSourceExposure),
					Observed: string(fbExposure),
					Required: string(ctx.input.Policy.MaxSourceExposure),
				})
			}
		}
	}

	// 5. Economic & Budget Bindings
	if ctx.input.Policy != nil {
		if ctx.input.Policy.MaxCostClass != "" {
			for i, rb := range p.RoleBindings {
				cost := getEndpointCostClass(rb.EndpointID)
				if cost.CostRank() > ctx.input.Policy.MaxCostClass.CostRank() {
					*diags = append(*diags, PortfolioDiagnostic{
						Code:         CodeUnauthorizedCostClass,
						Condition:    ConditionUnauthorized,
						Target:       fmt.Sprintf("role_bindings[%d]", i),
						ViolatedRule: "DCI-124",
						Message: fmt.Sprintf("endpoint %q cost class %q exceeds project policy %q",
							rb.EndpointID, cost, ctx.input.Policy.MaxCostClass),
						Observed: string(cost),
						Required: string(ctx.input.Policy.MaxCostClass),
					})
				}
				for j, fb := range rb.Fallbacks {
					fbCost := getEndpointCostClass(fb.EndpointID)
					if fbCost.CostRank() > ctx.input.Policy.MaxCostClass.CostRank() {
						*diags = append(*diags, PortfolioDiagnostic{
							Code:         CodeUnauthorizedCostClass,
							Condition:    ConditionUnauthorized,
							Target:       fmt.Sprintf("role_bindings[%d].fallbacks[%d]", i, j),
							ViolatedRule: "DCI-124",
							Message: fmt.Sprintf("fallback endpoint %q cost class %q exceeds project policy %q",
								fb.EndpointID, fbCost, ctx.input.Policy.MaxCostClass),
							Observed: string(fbCost),
							Required: string(ctx.input.Policy.MaxCostClass),
						})
					}
				}
			}
		}

		if len(ctx.input.Policy.AllowedRegimes) > 0 {
			allowedRegimes := make(map[protocol.EconomicRegime]bool)
			for _, r := range ctx.input.Policy.AllowedRegimes {
				allowedRegimes[r] = true
			}
			for i, bp := range p.BudgetPools {
				if !allowedRegimes[bp.Regime] {
					*diags = append(*diags, PortfolioDiagnostic{
						Code:         CodeUnauthorizedEconomicRegime,
						Condition:    ConditionUnauthorized,
						Target:       fmt.Sprintf("budget_pools[%d](%s)", i, bp.PoolID),
						ViolatedRule: "DCI-124",
						Message:      fmt.Sprintf("economic regime %q is not permitted by policy", bp.Regime),
						Observed:     string(bp.Regime),
					})
				}
			}
		}

		if ctx.input.Policy.ForbidMeteredAPI {
			for i, bp := range p.BudgetPools {
				if bp.Regime == protocol.RegimeMeteredAPI {
					*diags = append(*diags, PortfolioDiagnostic{
						Code:         CodeUnauthorizedEconomicRegime,
						Condition:    ConditionUnauthorized,
						Target:       fmt.Sprintf("budget_pools[%d](%s)", i, bp.PoolID),
						ViolatedRule: "DCI-122, DCI-124",
						Message:      "metered API regime is strictly forbidden by policy",
						Observed:     string(bp.Regime),
					})
				}
			}
		}
	}

	// Invariant DCI-122: No silent metered fallback
	for i, rb := range p.RoleBindings {
		primaryPool, okP := ctx.poolMap[rb.BudgetPoolID]
		if okP {
			for j, fb := range rb.Fallbacks {
				fbPool, okF := ctx.poolMap[fb.BudgetPoolID]
				if okF {
					if fbPool.Regime == protocol.RegimeMeteredAPI && !primaryPool.FallbackAllowedToMetered {
						*diags = append(*diags, PortfolioDiagnostic{
							Code:         CodeUnauthorizedMeteredFallback,
							Condition:    ConditionUnauthorized,
							Target:       fmt.Sprintf("role_bindings[%d].fallbacks[%d]", i, j),
							ViolatedRule: "DCI-122, ADR-0018 §9",
							Message: fmt.Sprintf("fallback to metered pool %q from non-metered pool %q is forbidden without explicit authorization",
								fbPool.PoolID, primaryPool.PoolID),
							Observed: "fallback_allowed_to_metered: false",
							Required: "fallback_allowed_to_metered: true",
						})
					}
				}
			}
		}
	}

	// Live Budget State checks
	if ctx.input.BudgetStates != nil {
		for poolID, bp := range ctx.poolMap {
			if st, ok := ctx.input.BudgetStates[poolID]; ok {
				if st.Status == protocol.BudgetStatusExhausted && !bp.AllowOverage {
					*diags = append(*diags, PortfolioDiagnostic{
						Code:         CodeBudgetPoolExhausted,
						Condition:    ConditionOverBudget,
						Target:       fmt.Sprintf("budget_pools[%s]", poolID),
						ViolatedRule: "DCI-126",
						Message:      fmt.Sprintf("budget pool %q status is exhausted and allow_overage is false", poolID),
						Observed:     string(protocol.BudgetStatusExhausted),
					})
				}
				if st.RemainingBalance != nil && *st.RemainingBalance <= 0 && !bp.AllowOverage {
					*diags = append(*diags, PortfolioDiagnostic{
						Code:         CodeBudgetPoolExhausted,
						Condition:    ConditionOverBudget,
						Target:       fmt.Sprintf("budget_pools[%s]", poolID),
						ViolatedRule: "DCI-126",
						Message:      fmt.Sprintf("budget pool %q has zero remaining balance (%d) and allow_overage is false", poolID, *st.RemainingBalance),
						Observed:     fmt.Sprintf("%d", *st.RemainingBalance),
					})
				}
			}
		}
	}
}
