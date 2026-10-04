package cognition

import (
	"fmt"
	"sort"
	"strings"

	"github.com/olostan/DevCadence/internal/protocol"
)

// validateContextAndConstraints validates dimensions 6, 7, and 8:
// context compatibility, machine/resource constraints, and portfolio structure & diversity.
func (ctx *validatorContext) validateContextAndConstraints(diags *[]PortfolioDiagnostic) {
	p := ctx.p

	// 6. Context Compatibility
	validateContextCompatibility := func(chID, profID, target string) {
		ch, okCh := ctx.channelMap[chID]
		prof, okProf := ctx.input.ContextProfiles[profID]
		if !okCh || !okProf || prof == nil {
			return
		}

		if prof.ObservedContextControl != protocol.ContextControlUnknown && ch.ContextControl != protocol.ContextControlUnknown {
			if prof.ObservedContextControl != ch.ContextControl {
				*diags = append(*diags, PortfolioDiagnostic{
					Code:         CodeContextControlMismatch,
					Condition:    ConditionUnsupported,
					Target:       target,
					ViolatedRule: "DCI-123, ADR-0019 §1",
					Message: fmt.Sprintf("channel context_control (%s) disagrees with context profile observed_context_control (%s)",
						ch.ContextControl, prof.ObservedContextControl),
					Observed: string(ch.ContextControl),
					Required: string(prof.ObservedContextControl),
				})
			}
		}

		if ctx.policy.RequireKnownContextControl {
			if ch.ContextControl == protocol.ContextControlUnknown || prof.ObservedContextControl == protocol.ContextControlUnknown {
				*diags = append(*diags, PortfolioDiagnostic{
					Code:         CodeContextControlMismatch,
					Condition:    ConditionUnknown,
					Target:       target,
					ViolatedRule: "DCI-123",
					Message:      "policy requires verified known context_control, but context control is unknown",
					Observed:     string(protocol.ContextControlUnknown),
				})
			}
		}

		if prof.ObservedPrefixCache != protocol.PrefixCacheUnknown && ch.PrefixCache != protocol.PrefixCacheUnknown {
			if prof.ObservedPrefixCache != ch.PrefixCache {
				*diags = append(*diags, PortfolioDiagnostic{
					Code:         CodePrefixCacheMismatch,
					Condition:    ConditionUnsupported,
					Target:       target,
					ViolatedRule: "DCI-123, ADR-0019 §1",
					Message: fmt.Sprintf("channel prefix_cache (%s) disagrees with context profile observed_prefix_cache (%s)",
						ch.PrefixCache, prof.ObservedPrefixCache),
					Observed: string(ch.PrefixCache),
					Required: string(prof.ObservedPrefixCache),
				})
			}
		}

		if ctx.policy.MinContractLimitTokens > 0 {
			if prof.ContractLimitTokens > 0 && prof.ContractLimitTokens < ctx.policy.MinContractLimitTokens {
				*diags = append(*diags, PortfolioDiagnostic{
					Code:         CodeContextWindowInsufficient,
					Condition:    ConditionUnsupported,
					Target:       target,
					ViolatedRule: "DCI-131, DCI-132",
					Message: fmt.Sprintf("context profile contract_limit_tokens (%d) is below minimum policy requirement (%d)",
						prof.ContractLimitTokens, ctx.policy.MinContractLimitTokens),
					Observed: fmt.Sprintf("%d", prof.ContractLimitTokens),
					Required: fmt.Sprintf("%d", ctx.policy.MinContractLimitTokens),
				})
			}
		}
	}

	// validateContextProfileConsistency reports a profile that does not describe
	// the binding's own endpoint and channel (WP-M3C-H3). A missing or nil
	// profile is reported by the endpoints dimension as CONTEXT_PROFILE_NOT_FOUND.
	validateContextProfileConsistency := func(endpointID, chID, profID, target string) {
		prof := ctx.input.ContextProfiles[profID]
		if prof == nil {
			return
		}
		if prof.EndpointID != endpointID || prof.ChannelID != chID {
			*diags = append(*diags, PortfolioDiagnostic{
				Code:         CodeContextProfileMismatch,
				Condition:    ConditionInvalid,
				Target:       target,
				ViolatedRule: "DCI-123",
				Message:      fmt.Sprintf("context profile %q describes a different endpoint/channel than the binding", profID),
				Observed:     fmt.Sprintf("endpoint_id=%s channel_id=%s", prof.EndpointID, prof.ChannelID),
				Required:     fmt.Sprintf("endpoint_id=%s channel_id=%s", endpointID, chID),
			})
		}
	}

	if ctx.input.ContextProfiles != nil {
		for i, rb := range p.RoleBindings {
			target := fmt.Sprintf("role_bindings[%d]", i)
			validateContextProfileConsistency(rb.EndpointID, rb.ChannelID, rb.ContextProfileID, target)
			validateContextCompatibility(rb.ChannelID, rb.ContextProfileID, target)
			for j, fb := range rb.Fallbacks {
				fbTarget := fmt.Sprintf("role_bindings[%d].fallbacks[%d]", i, j)
				validateContextProfileConsistency(fb.EndpointID, fb.ChannelID, fb.ContextProfileID, fbTarget)
				validateContextCompatibility(fb.ChannelID, fb.ContextProfileID, fbTarget)
			}
		}
	}

	// 7. Machine / Resource Constraints
	validateMachineConstraints := func(epID, target string) {
		ep, inProfile := ctx.profileEndpoints[epID]
		epSum, inInventory := ctx.inventoryEndpoints[epID]

		kind := protocol.EndpointKind("")
		locality := protocol.Locality("")
		var accelBackend *protocol.BackendKind
		accelVerified := false

		if inProfile {
			kind = ep.Kind
			locality = ep.Locality
			if ep.Acceleration != nil {
				accelBackend = &ep.Acceleration.Backend
				accelVerified = (ep.Acceleration.State == protocol.StateVerified)
			}
		} else if inInventory {
			kind = epSum.Kind
			locality = epSum.Locality
			accelBackend = epSum.AccelerationBackend
			accelVerified = epSum.AccelerationVerified
		}

		if kind == protocol.EndpointLocalRuntime || locality == protocol.LocalityLocal {
			if accelBackend != nil && *accelBackend != protocol.BackendCPU && *accelBackend != protocol.BackendUnknown {
				hasBackend := false
				if ctx.input.Inventory != nil {
					for _, b := range ctx.input.Inventory.Hardware.AcceleratorBackends {
						if b == *accelBackend {
							hasBackend = true
							break
						}
					}
				}
				if !hasBackend {
					*diags = append(*diags, PortfolioDiagnostic{
						Code:         CodeHardwareBackendUnavailable,
						Condition:    ConditionUnsupported,
						Target:       target,
						ViolatedRule: "DCI-106",
						Message: fmt.Sprintf("endpoint %q requires accelerator backend %q, but host hardware lacks it",
							epID, string(*accelBackend)),
						Observed: "backend absent",
						Required: string(*accelBackend),
					})
				}
				if ctx.input.Policy != nil && ctx.input.Policy.RequireVerifiedAcceleration && !accelVerified {
					*diags = append(*diags, PortfolioDiagnostic{
						Code:         CodeAccelerationUnverified,
						Condition:    ConditionUnsupported,
						Target:       target,
						ViolatedRule: "DCI-106",
						Message: fmt.Sprintf("endpoint %q acceleration backend %q is unverified",
							epID, string(*accelBackend)),
						Observed: "acceleration_verified: false",
						Required: "acceleration_verified: true",
					})
				}
			}
		}
	}

	for i, rb := range p.RoleBindings {
		validateMachineConstraints(rb.EndpointID, fmt.Sprintf("role_bindings[%d]", i))
		for j, fb := range rb.Fallbacks {
			validateMachineConstraints(fb.EndpointID, fmt.Sprintf("role_bindings[%d].fallbacks[%d]", i, j))
		}
	}

	if ctx.input.ResourceStates != nil {
		hostIDs := make([]string, 0, len(ctx.input.ResourceStates))
		for hostID := range ctx.input.ResourceStates {
			hostIDs = append(hostIDs, hostID)
		}
		sort.Strings(hostIDs)
		for _, hostID := range hostIDs {
			resState := ctx.input.ResourceStates[hostID]
			if resState == nil {
				continue
			}
			if resState.ActiveSlots != nil && resState.MaxConcurrentSlots != nil {
				if *resState.ActiveSlots >= *resState.MaxConcurrentSlots {
					*diags = append(*diags, PortfolioDiagnostic{
						Code:         CodeResourceCapacityExceeded,
						Condition:    ConditionOverBudget,
						Target:       fmt.Sprintf("resource_states[%s]", hostID),
						ViolatedRule: "DCI-127",
						Message: fmt.Sprintf("host %q active slots (%d) have reached or exceeded capacity (%d)",
							hostID, *resState.ActiveSlots, *resState.MaxConcurrentSlots),
						Observed: fmt.Sprintf("%d", *resState.ActiveSlots),
						Required: fmt.Sprintf("< %d", *resState.MaxConcurrentSlots),
					})
				}
			}
			if ctx.policy.RequireKnownResourceState && len(resState.UnknownMetrics) > 0 {
				*diags = append(*diags, PortfolioDiagnostic{
					Code:         CodeUnknownResourceState,
					Condition:    ConditionUnknown,
					Target:       fmt.Sprintf("resource_states[%s]", hostID),
					ViolatedRule: "DCI-005",
					Message: fmt.Sprintf("host %q resource state contains unknown metrics (%s) where certainty is required",
						hostID, strings.Join(resState.UnknownMetrics, ", ")),
				})
			}
		}
	}

	// 8. Diversity Requirements
	if p.DiversityRequirements != nil {
		var reviewBindings []protocol.RoleBinding
		for _, rb := range p.RoleBindings {
			rLower := strings.ToLower(rb.Role)
			if rLower == "reviewer" || rLower == "correctness_reviewer" || rLower == "architecture_reviewer" || strings.Contains(rLower, "review") {
				reviewBindings = append(reviewBindings, rb)
			}
		}

		if len(reviewBindings) >= 2 {
			for i := 0; i < len(reviewBindings); i++ {
				for j := i + 1; j < len(reviewBindings); j++ {
					rb1 := reviewBindings[i]
					rb2 := reviewBindings[j]

					if p.DiversityRequirements.RequireDistinctEndpointsForReview && rb1.EndpointID == rb2.EndpointID {
						*diags = append(*diags, PortfolioDiagnostic{
							Code:         CodeDiversityViolation,
							Condition:    ConditionInvalid,
							Target:       "portfolio.diversity_requirements",
							ViolatedRule: "DCI-123, ADR-0018 §11",
							Message: fmt.Sprintf("distinct endpoints required for review roles, but roles %q and %q bind to the same endpoint %q",
								rb1.Role, rb2.Role, rb1.EndpointID),
							Observed: rb1.EndpointID,
						})
					}

					ep1, ok1 := ctx.profileEndpoints[rb1.EndpointID]
					ep2, ok2 := ctx.profileEndpoints[rb2.EndpointID]
					if ok1 && ok2 {
						if p.DiversityRequirements.RequireDistinctProvidersForReview && ep1.Provider != "" && ep2.Provider != "" && ep1.Provider == ep2.Provider {
							*diags = append(*diags, PortfolioDiagnostic{
								Code:         CodeDiversityViolation,
								Condition:    ConditionInvalid,
								Target:       "portfolio.diversity_requirements",
								ViolatedRule: "DCI-123, ADR-0018 §11",
								Message: fmt.Sprintf("distinct providers required for review roles, but endpoints %q and %q share provider %q",
									ep1.ID, ep2.ID, ep1.Provider),
								Observed: ep1.Provider,
							})
						}
						if p.DiversityRequirements.RequireDistinctModelsForReview {
							model1 := ep1.ModelID
							model2 := ep2.ModelID
							if model1 != "" && model2 != "" && model1 == model2 {
								*diags = append(*diags, PortfolioDiagnostic{
									Code:         CodeDiversityViolation,
									Condition:    ConditionInvalid,
									Target:       "portfolio.diversity_requirements",
									ViolatedRule: "DCI-123, ADR-0018 §11",
									Message: fmt.Sprintf("distinct models required for review roles, but endpoints %q and %q share model %q",
										ep1.ID, ep2.ID, model1),
									Observed: model1,
								})
							}
						}
					}
				}
			}
		}
	}
}
