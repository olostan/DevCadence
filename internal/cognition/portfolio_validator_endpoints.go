package cognition

import (
	"fmt"

	"github.com/olostan/DevCadence/internal/protocol"
)

// validateEndpointsAndCapabilities validates dimensions 1, 2, and 3:
// endpoint existence, capability compatibility, and capability provenance.
func (ctx *validatorContext) validateEndpointsAndCapabilities(diags *[]PortfolioDiagnostic) {
	p := ctx.p

	// 1. Endpoint existence checks
	checkEndpointExists := func(epID, target string) bool {
		if ctx.excludedMap[epID] {
			*diags = append(*diags, PortfolioDiagnostic{
				Code:         CodePortfolioStructureInvalid,
				Condition:    ConditionInvalid,
				Target:       target,
				ViolatedRule: "DCI-123",
				Message:      fmt.Sprintf("referenced endpoint %q is listed in excluded_endpoint_ids", epID),
				Observed:     epID,
			})
			return false
		}
		_, inProfile := ctx.profileEndpoints[epID]
		_, inInventory := ctx.inventoryEndpoints[epID]
		if !inProfile && !inInventory {
			*diags = append(*diags, PortfolioDiagnostic{
				Code:         CodeEndpointNotFound,
				Condition:    ConditionInvalid,
				Target:       target,
				ViolatedRule: "DCI-123",
				Message:      fmt.Sprintf("endpoint %q does not exist in inventory or machine profile", epID),
				Observed:     epID,
				Required:     "existing endpoint in ResourceInventory or MachineCapabilityProfile",
			})
			return false
		}
		return true
	}

	for i, ch := range p.Channels {
		target := fmt.Sprintf("channels[%d](%s)", i, ch.ChannelID)
		checkEndpointExists(ch.EndpointID, target)
	}

	for i, rb := range p.RoleBindings {
		target := fmt.Sprintf("role_bindings[%d](role=%s)", i, rb.Role)
		checkEndpointExists(rb.EndpointID, target)
		if _, ok := ctx.channelMap[rb.ChannelID]; !ok {
			*diags = append(*diags, PortfolioDiagnostic{
				Code:         CodePortfolioStructureInvalid,
				Condition:    ConditionInvalid,
				Target:       target,
				ViolatedRule: "DCI-123",
				Message:      fmt.Sprintf("role binding references nonexistent channel %q", rb.ChannelID),
				Observed:     rb.ChannelID,
			})
		}
		if _, ok := ctx.input.ContextProfiles[rb.ContextProfileID]; !ok {
			*diags = append(*diags, PortfolioDiagnostic{
				Code:         CodeContextProfileNotFound,
				Condition:    ConditionInvalid,
				Target:       target,
				ViolatedRule: "DCI-123",
				Message:      fmt.Sprintf("role binding references nonexistent context profile %q", rb.ContextProfileID),
				Observed:     rb.ContextProfileID,
			})
		}
		if _, ok := ctx.poolMap[rb.BudgetPoolID]; !ok {
			*diags = append(*diags, PortfolioDiagnostic{
				Code:         CodeBudgetPoolNotFound,
				Condition:    ConditionInvalid,
				Target:       target,
				ViolatedRule: "DCI-123",
				Message:      fmt.Sprintf("role binding references nonexistent budget pool %q", rb.BudgetPoolID),
				Observed:     rb.BudgetPoolID,
			})
		}

		for j, fb := range rb.Fallbacks {
			fbTarget := fmt.Sprintf("role_bindings[%d].fallbacks[%d]", i, j)
			checkEndpointExists(fb.EndpointID, fbTarget)
			if _, ok := ctx.channelMap[fb.ChannelID]; !ok {
				*diags = append(*diags, PortfolioDiagnostic{
					Code:         CodePortfolioStructureInvalid,
					Condition:    ConditionInvalid,
					Target:       fbTarget,
					ViolatedRule: "DCI-123",
					Message:      fmt.Sprintf("fallback binding references nonexistent channel %q", fb.ChannelID),
					Observed:     fb.ChannelID,
				})
			}
			if _, ok := ctx.input.ContextProfiles[fb.ContextProfileID]; !ok {
				*diags = append(*diags, PortfolioDiagnostic{
					Code:         CodeContextProfileNotFound,
					Condition:    ConditionInvalid,
					Target:       fbTarget,
					ViolatedRule: "DCI-123",
					Message:      fmt.Sprintf("fallback binding references nonexistent context profile %q", fb.ContextProfileID),
					Observed:     fb.ContextProfileID,
				})
			}
			if _, ok := ctx.poolMap[fb.BudgetPoolID]; !ok {
				*diags = append(*diags, PortfolioDiagnostic{
					Code:         CodeBudgetPoolNotFound,
					Condition:    ConditionInvalid,
					Target:       fbTarget,
					ViolatedRule: "DCI-123",
					Message:      fmt.Sprintf("fallback binding references nonexistent budget pool %q", fb.BudgetPoolID),
					Observed:     fb.BudgetPoolID,
				})
			}
		}
	}

	// 2 & 3. Capability compatibility and provenance
	for i, rb := range p.RoleBindings {
		target := fmt.Sprintf("role_bindings[%d](role=%s)", i, rb.Role)
		ctx.validateBindingCapability(rb.Role, rb.EndpointID, rb.ChannelID, target, diags)
		for j, fb := range rb.Fallbacks {
			fbTarget := fmt.Sprintf("role_bindings[%d].fallbacks[%d]", i, j)
			ctx.validateBindingCapability(rb.Role, fb.EndpointID, fb.ChannelID, fbTarget, diags)
		}
	}
}

func (ctx *validatorContext) validateBindingCapability(role, epID, chID, target string, diags *[]PortfolioDiagnostic) {
	req, hasReq := ctx.roleReqs[Role(role)]

	ep, inProfile := ctx.profileEndpoints[epID]
	epSum, inInventory := ctx.inventoryEndpoints[epID]

	health := protocol.EndpointHealthUnknown
	auth := protocol.AuthUnknown
	var locality protocol.Locality
	var kind protocol.EndpointKind
	accelVerified := false

	if inProfile {
		health = ep.Health
		auth = ep.Auth
		locality = ep.Locality
		kind = ep.Kind
		if ep.Acceleration != nil {
			accelVerified = (ep.Acceleration.State == protocol.StateVerified)
		}
	} else if inInventory {
		health = epSum.Health
		auth = epSum.Auth
		locality = epSum.Locality
		kind = epSum.Kind
		accelVerified = epSum.AccelerationVerified
	}

	// Health check
	if !health.Usable() {
		*diags = append(*diags, PortfolioDiagnostic{
			Code:         CodeEndpointUnhealthy,
			Condition:    ConditionUnsupported,
			Target:       target,
			ViolatedRule: "DCI-104",
			Message:      fmt.Sprintf("endpoint %q health is %q; only probed ready endpoints may be routed to", epID, string(health)),
			Observed:     string(health),
			Required:     string(protocol.EndpointHealthReady),
		})
	}

	// Auth check for non-local endpoints matching protocol.EndpointViable
	if kind != protocol.EndpointLocalRuntime && locality != protocol.LocalityLocal {
		switch auth {
		case protocol.AuthAuthenticated, protocol.AuthNotApplicable:
			// OK
		case protocol.AuthUnauthenticated, protocol.AuthExpired, protocol.AuthError:
			*diags = append(*diags, PortfolioDiagnostic{
				Code:         CodeEndpointUnauthenticated,
				Condition:    ConditionUnauthorized,
				Target:       target,
				ViolatedRule: "DCI-105",
				Message:      fmt.Sprintf("endpoint %q authentication state is %q; non-local endpoints require verified authentication", epID, string(auth)),
				Observed:     string(auth),
				Required:     string(protocol.AuthAuthenticated),
			})
		default:
			*diags = append(*diags, PortfolioDiagnostic{
				Code:         CodeEndpointUnauthenticated,
				Condition:    ConditionUnknown,
				Target:       target,
				ViolatedRule: "DCI-105",
				Message:      fmt.Sprintf("endpoint %q authentication state is %q; non-local endpoints require verified authentication per protocol.EndpointViable", epID, string(auth)),
				Observed:     string(auth),
				Required:     string(protocol.AuthAuthenticated),
			})
		}
	}

	ch, hasCh := ctx.channelMap[chID]

	if !hasReq {
		return
	}

	// Tool use
	if req.RequireToolUse {
		if inProfile {
			if !(ep.ToolUse == protocol.FeatureProbePassed || ep.ToolUse == protocol.FeatureDeclared) {
				*diags = append(*diags, PortfolioDiagnostic{
					Code:         CodeToolSupportMissing,
					Condition:    ConditionUnsupported,
					Target:       target,
					ViolatedRule: "DCI-054",
					Message:      fmt.Sprintf("role %q requires tool support, but endpoint %q tool_use is %q", role, epID, string(ep.ToolUse)),
					Observed:     string(ep.ToolUse),
					Required:     "probe_passed or declared",
				})
			}
		} else {
			*diags = append(*diags, PortfolioDiagnostic{
				Code:         CodeToolSupportMissing,
				Condition:    ConditionUnknown,
				Target:       target,
				ViolatedRule: "DCI-054",
				Message:      fmt.Sprintf("role %q requires tool support, but endpoint %q is not present in MachineCapabilityProfile (inventory summary lacks tool support facts)", role, epID),
				Observed:     string(protocol.GradeUnknown),
				Required:     "probe_passed or declared",
			})
		}
		if hasCh && !ch.SupportsTools {
			*diags = append(*diags, PortfolioDiagnostic{
				Code:         CodeToolSupportMissing,
				Condition:    ConditionUnsupported,
				Target:       target,
				ViolatedRule: "DCI-054",
				Message:      fmt.Sprintf("role %q requires tool support, but channel %q declares supports_tools: false", role, chID),
				Observed:     "false",
				Required:     "true",
			})
		}
	}

	// Structured output
	if req.RequireStructuredOutput {
		if inProfile {
			if !(ep.StructuredOutput == protocol.FeatureProbePassed || ep.StructuredOutput == protocol.FeatureDeclared) {
				*diags = append(*diags, PortfolioDiagnostic{
					Code:         CodeStructuredOutputMissing,
					Condition:    ConditionUnsupported,
					Target:       target,
					ViolatedRule: "DCI-054",
					Message:      fmt.Sprintf("role %q requires structured output, but endpoint %q structured_output is %q", role, epID, string(ep.StructuredOutput)),
					Observed:     string(ep.StructuredOutput),
					Required:     "probe_passed or declared",
				})
			}
		} else {
			*diags = append(*diags, PortfolioDiagnostic{
				Code:         CodeStructuredOutputMissing,
				Condition:    ConditionUnknown,
				Target:       target,
				ViolatedRule: "DCI-054",
				Message:      fmt.Sprintf("role %q requires structured output, but endpoint %q is not present in MachineCapabilityProfile (inventory summary lacks structured output facts)", role, epID),
				Observed:     string(protocol.GradeUnknown),
				Required:     "probe_passed or declared",
			})
		}
	}

	// Verified acceleration
	if req.RequireVerifiedAcceleration && !accelVerified {
		*diags = append(*diags, PortfolioDiagnostic{
			Code:         CodeAccelerationUnverified,
			Condition:    ConditionUnsupported,
			Target:       target,
			ViolatedRule: "DCI-106",
			Message:      fmt.Sprintf("role %q requires verified acceleration, but endpoint %q acceleration is unverified", role, epID),
			Observed:     "false",
			Required:     "true",
		})
	}

	// Capability Grade & Provenance
	if req.MinGrade != protocol.GradeUnknown {
		if inProfile {
			graded := ep.Capability(req.Dimension)
			if graded.Grade == protocol.GradeUnknown {
				*diags = append(*diags, PortfolioDiagnostic{
					Code:         CodeCapabilityMissing,
					Condition:    ConditionUnknown,
					Target:       target,
					ViolatedRule: "DCI-005",
					Message:      fmt.Sprintf("role %q requires %s capability at least %s, but endpoint %q capability is unknown", role, req.Dimension, req.MinGrade, epID),
					Observed:     string(protocol.GradeUnknown),
					Required:     string(req.MinGrade),
				})
			} else if graded.Grade.GradeRank() < req.MinGrade.GradeRank() {
				*diags = append(*diags, PortfolioDiagnostic{
					Code:         CodeCapabilityGradeInsufficient,
					Condition:    ConditionUnsupported,
					Target:       target,
					ViolatedRule: "DCI-005",
					Message:      fmt.Sprintf("role %q requires %s capability %s, but endpoint %q grade is %s", role, req.Dimension, req.MinGrade, epID, graded.Grade),
					Observed:     string(graded.Grade),
					Required:     string(req.MinGrade),
				})
			}

			// Provenance check
			if graded.Provenance == protocol.ProvenanceUnknown || !graded.Provenance.Valid() {
				*diags = append(*diags, PortfolioDiagnostic{
					Code:         CodeCapabilityProvenanceInvalid,
					Condition:    ConditionUnknown,
					Target:       target,
					ViolatedRule: "DCI-005",
					Message:      fmt.Sprintf("endpoint %q %s capability has unknown provenance", epID, req.Dimension),
					Observed:     string(graded.Provenance),
					Required:     "configured, measured, or evaluated",
				})
			} else if ctx.policy.RequireMeasuredProvenance && graded.Provenance != protocol.ProvenanceMeasured && graded.Provenance != protocol.ProvenanceEvaluated {
				*diags = append(*diags, PortfolioDiagnostic{
					Code:         CodeCapabilityProvenanceInvalid,
					Condition:    ConditionUnsupported,
					Target:       target,
					ViolatedRule: "DCI-005",
					Message:      fmt.Sprintf("policy requires measured provenance for role %q, but endpoint %q has provenance %q", role, epID, string(graded.Provenance)),
					Observed:     string(graded.Provenance),
					Required:     "measured or evaluated",
				})
			}
		} else {
			*diags = append(*diags, PortfolioDiagnostic{
				Code:         CodeCapabilityMissing,
				Condition:    ConditionUnknown,
				Target:       target,
				ViolatedRule: "DCI-005, DCI-123",
				Message:      fmt.Sprintf("role %q requires %s capability grade at least %s, but endpoint %q is not present in MachineCapabilityProfile (inventory summary lacks capability facts)", role, req.Dimension, req.MinGrade, epID),
				Observed:     string(protocol.GradeUnknown),
				Required:     string(req.MinGrade),
			})
		}
	}
}
