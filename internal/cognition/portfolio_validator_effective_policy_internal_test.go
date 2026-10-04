package cognition

import (
	"testing"

	"github.com/olostan/DevCadence/internal/protocol"
)

// TestEffectivePolicyAndTrimmedFieldsACC04bEffectivePolicyIsRead proves the
// acceleration check reads the effective policy carried by the validator
// context, not ValidationInput.Policy: the input policy is nil while the
// effective policy requires verified acceleration.
func TestEffectivePolicyAndTrimmedFieldsACC04bEffectivePolicyIsRead(t *testing.T) {
	metal := protocol.BackendMetal
	input := ValidationInput{
		Portfolio: &protocol.CognitionPortfolio{
			RoleBindings: []protocol.RoleBinding{
				{Role: "implementer", EndpointID: "ep-local-01", ChannelID: "chan-local-01"},
			},
		},
		Inventory: &protocol.ResourceInventory{
			Hardware: protocol.HardwareSummary{
				AcceleratorBackends: []protocol.BackendKind{protocol.BackendMetal},
			},
			CognitionEndpoints: []protocol.CognitionEndpointSummary{
				{
					ID:                  "ep-local-01",
					Kind:                protocol.EndpointLocalRuntime,
					Locality:            protocol.LocalityLocal,
					AccelerationBackend: &metal,
				},
			},
		},
		Policy: nil,
	}

	effective := DefaultValidationPolicy()
	effective.RequireVerifiedAcceleration = true
	var diags []PortfolioDiagnostic
	newValidatorContext(input, effective).validateContextAndConstraints(&diags)

	found := 0
	for _, d := range diags {
		if d.Code == CodeAccelerationUnverified {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("expected one %s diagnostic from the effective policy, got %d: %v", CodeAccelerationUnverified, found, diags)
	}

	// Control: with the default effective policy the diagnostic is absent.
	diags = nil
	newValidatorContext(input, DefaultValidationPolicy()).validateContextAndConstraints(&diags)
	for _, d := range diags {
		if d.Code == CodeAccelerationUnverified {
			t.Fatalf("default effective policy must not emit %s: %v", CodeAccelerationUnverified, diags)
		}
	}
}
