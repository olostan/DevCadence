package cognition_test

import (
	"testing"

	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/protocol"
)

// unverifiedAccelerationInput returns a valid standard input whose local
// endpoint reports a non-CPU backend with acceleration_verified false, resolved
// through the inventory (no machine profile), the only shape on which the
// ACCELERATION_UNVERIFIED diagnostic can fire.
func unverifiedAccelerationInput(pol *cognition.ValidationPolicy) cognition.ValidationInput {
	in := unknownStateInput(makeTestPortfolio(), pol)
	in.MachineProfile = nil
	metal := protocol.BackendMetal
	in.Inventory.CognitionEndpoints[0].AccelerationVerified = false
	in.Inventory.CognitionEndpoints[0].AccelerationBackend = &metal
	return in
}

// TestEffectivePolicyAndTrimmedFieldsACC04PublicValidate pins the public
// behavior of the acceleration policy: explicit true emits the diagnostic, nil
// and explicit-default policies do not, and their digests are identical.
func TestEffectivePolicyAndTrimmedFieldsACC04PublicValidate(t *testing.T) {
	v := cognition.NewPortfolioValidator()

	strict := cognition.DefaultValidationPolicy()
	strict.RequireVerifiedAcceleration = true
	res := v.Validate(unverifiedAccelerationInput(&strict))
	got := diagsWithCode(res, cognition.CodeAccelerationUnverified)
	// Both standard role bindings route to the single local endpoint.
	if res.Valid || len(got) != 2 || got[0].Target != "role_bindings[0]" || got[1].Target != "role_bindings[1]" ||
		got[0].Observed != "acceleration_verified: false" || got[0].Required != "acceleration_verified: true" {
		t.Fatalf("explicit policy: valid=%v diags=%v", res.Valid, res.Diagnostics)
	}

	def := cognition.DefaultValidationPolicy()
	nilRes := v.Validate(unverifiedAccelerationInput(nil))
	defRes := v.Validate(unverifiedAccelerationInput(&def))
	if len(diagsWithCode(nilRes, cognition.CodeAccelerationUnverified)) != 0 {
		t.Fatalf("nil policy must not require verified acceleration: %v", nilRes.Diagnostics)
	}
	if len(diagsWithCode(defRes, cognition.CodeAccelerationUnverified)) != 0 {
		t.Fatalf("default policy must not require verified acceleration: %v", defRes.Diagnostics)
	}
	if nilRes.PolicyDigest == "" || nilRes.PolicyDigest != defRes.PolicyDigest {
		t.Fatalf("nil-policy digest %q != explicit-default digest %q", nilRes.PolicyDigest, defRes.PolicyDigest)
	}
	if nilRes.Valid != defRes.Valid || len(nilRes.Diagnostics) != len(defRes.Diagnostics) {
		t.Fatalf("nil and default outputs differ: %v vs %v", nilRes.Diagnostics, defRes.Diagnostics)
	}
}
