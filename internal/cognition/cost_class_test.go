package cognition_test

import (
	"context"
	"testing"

	"github.com/olostan/DevCadience/internal/cognition"
	"github.com/olostan/DevCadience/internal/environment"
	"github.com/olostan/DevCadience/internal/protocol"
)

// Cost class is a commercial fact, and DevCadience can observe almost none of it.
//
// It can see that a binary exists and that it answered a prompt. It cannot see
// which of a subscription, a metered API key, an enterprise agreement or a credit
// balance is being drawn down, and the only way to find out would be to read the
// credentials it is forbidden to touch. So discovery produces `unknown`, routing
// treats `unknown` as dearer than every known class, and an operator declaration
// is the single path to a real one.

func cliAdapterWithOneEndpoint() *cognition.FakeAdapter {
	return &cognition.FakeAdapter{
		AdapterID: "cli",
		Endpoints: []protocol.CognitionEndpoint{cognition.CLIEndpoint("cli:codex-cli", "openai")},
		Results: map[string]cognition.ProbeResult{
			"cli:codex-cli": {Status: protocol.FindingObserved, Detail: "answered"},
		},
	}
}

// TestADiscoveredCLIHasAnUnknownCostClass is the default through the whole
// profile path, not just the adapter.
func TestADiscoveredCLIHasAnUnknownCostClass(t *testing.T) {
	service := newService(t, cliAdapterWithOneEndpoint())
	out := profile(t, service, cognition.ProfileInput{
		Facts: facts(t, environment.LinuxCPUOnly(), protocol.DepthHealth),
		Depth: protocol.DepthHealth,
	})
	endpoint, found := endpointByID(out, "cli:codex-cli")
	if !found {
		t.Fatal("the cli endpoint is missing")
	}
	if endpoint.CostClass != protocol.CostUnknown {
		t.Errorf("cost class = %q, want unknown with no declaration supplied", endpoint.CostClass)
	}
}

// TestAnOperatorDeclarationIsTheOnlyPathToAKnownCostClass exercises the existing
// declaration mechanism rather than a new bypass.
func TestAnOperatorDeclarationIsTheOnlyPathToAKnownCostClass(t *testing.T) {
	service := newService(t, cliAdapterWithOneEndpoint())
	out := profile(t, service, cognition.ProfileInput{
		Facts: facts(t, environment.LinuxCPUOnly(), protocol.DepthHealth),
		Depth: protocol.DepthHealth,
		Declarations: []cognition.Declaration{{
			EndpointID: "cli:codex-cli",
			CostClass:  protocol.CostSubscriptionIncluded,
		}},
	})
	endpoint, _ := endpointByID(out, "cli:codex-cli")
	if endpoint.CostClass != protocol.CostSubscriptionIncluded {
		t.Errorf("cost class = %q, want the declared subscription_included", endpoint.CostClass)
	}
	// A declaration about cost must not quietly grade capability or claim
	// authentication on the way through.
	if len(endpoint.Capabilities) != 0 {
		t.Errorf("declaring a cost class graded a capability: %+v", endpoint.Capabilities)
	}
	if endpoint.Auth != protocol.AuthUnknown {
		t.Errorf("auth = %q, want unknown; a cost declaration is not an authentication claim", endpoint.Auth)
	}
	if endpoint.RequiredSourceExposure != protocol.ExposureToolMediatedWorktree {
		t.Errorf("exposure = %q; a cost declaration must not relax privacy", endpoint.RequiredSourceExposure)
	}
}

// TestADeclaredCostClassSurvivesATargetedProbe checks the two fixes together: an
// operator's declared class must not be reset by the probe path.
func TestADeclaredCostClassSurvivesATargetedProbe(t *testing.T) {
	service := newService(t, cliAdapterWithOneEndpoint())
	_, endpoint, err := service.ProbeEndpoint(context.Background(), cognition.ProbeEndpointInput{
		Facts:      facts(t, environment.LinuxCPUOnly(), protocol.DepthHealth),
		EndpointID: "cli:codex-cli",
		Declarations: []cognition.Declaration{{
			EndpointID: "cli:codex-cli",
			CostClass:  protocol.CostSubscriptionIncluded,
		}},
	})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if endpoint.CostClass != protocol.CostSubscriptionIncluded {
		t.Errorf("cost class = %q after a probe, want the declared class", endpoint.CostClass)
	}
}

// TestUnknownCostIsNeverPreferredOverAKnownCheapEndpoint is the routing half.
//
// Ordering `unknown` as cheap would be the same bug as inferring a subscription:
// it would spend a user's money on an assumption. The fail-closed direction is to
// treat an unpriced endpoint as the most expensive thing on the machine.
func TestUnknownCostIsNeverPreferredOverAKnownCheapEndpoint(t *testing.T) {
	local := cognition.Ready(cognition.LocalEndpoint("ollama:local-strong", "ollama", "big-coder"))
	local = cognition.WithCapability(local, protocol.CapabilityImplementation,
		protocol.GradeStrong, protocol.ProvenanceConfigured)

	unpriced := cognition.Ready(cognition.CLIEndpoint("cli:codex-cli", "openai"))
	unpriced = cognition.WithCapability(unpriced, protocol.CapabilityImplementation,
		protocol.GradeStrong, protocol.ProvenanceConfigured)
	if unpriced.CostClass != protocol.CostUnknown {
		t.Fatalf("fixture cost class = %q, want unknown", unpriced.CostClass)
	}

	// Both are eligible and equally graded, so the choice is made on cost.
	decision := cognition.Route(requirement(cognition.RoleImplementer), permissivePolicy(),
		[]protocol.CognitionEndpoint{local, unpriced})
	if decision.SelectedID != "ollama:local-strong" {
		t.Fatalf("selected %q; an unpriced endpoint beat a known-cheap one: %+v",
			decision.SelectedID, decision)
	}

	// And an unknown class is not quietly admitted under a cheap policy ceiling.
	cheapOnly := cognition.Policy{
		MaxSourceExposure: protocol.ExposureToolMediatedWorktree,
		MaxCostClass:      protocol.CostSubscriptionIncluded,
	}
	restricted := cognition.Route(requirement(cognition.RoleImplementer), cheapOnly,
		[]protocol.CognitionEndpoint{unpriced})
	if restricted.Outcome != cognition.OutcomeNoEligibleEndpoint {
		t.Errorf("an unknown cost class passed a subscription_included ceiling: %+v", restricted)
	}
	rejection, found := rejectionFor(restricted, "cli:codex-cli")
	if !found || !reasonsContain(rejection.Reasons, "cost class unknown exceeds") {
		t.Errorf("the cost rejection was not explained: %+v", restricted)
	}
}
