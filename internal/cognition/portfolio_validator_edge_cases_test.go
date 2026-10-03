package cognition_test

import (
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/protocol"
)

func TestPortfolioValidatorEdgeCasesAndFallbackAuthorization(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 10, 2, 22, 0, 0, 0, time.UTC), time.Second)
	mp := makeTestMachineProfile()
	inv := makeTestInventory()
	cp := makeTestContextProfiles()

	t.Run("freshness check rejects stale ExpectedInventoryDigest", func(t *testing.T) {
		p := makeTestPortfolio()
		validator := cognition.NewPortfolioValidator()

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:               p,
			MachineProfile:          mp,
			Inventory:               inv,
			ContextProfiles:         cp,
			ExpectedInventoryDigest: "sha256:stale-digest-does-not-match",
			Clock:                   clk,
		})

		if res.Valid {
			t.Fatalf("expected validation failure for mismatched ExpectedInventoryDigest")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeStaleValidationState {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeStaleValidationState in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("inventory-only endpoint fails closed for role capability and features", func(t *testing.T) {
		p := makeTestPortfolio()
		mpWithoutCLI := makeTestMachineProfile()
		mpWithoutCLI.Endpoints = []protocol.CognitionEndpoint{mp.Endpoints[0]} // only ep-local-01

		// Bind role 'reviewer' directly to ep-cli-01 (only in inventory)
		p.RoleBindings[0].EndpointID = "ep-cli-01"
		p.RoleBindings[0].ChannelID = "chan-cli-01"

		validator := cognition.NewPortfolioValidator()
		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mpWithoutCLI,
			Inventory:       inv,
			ContextProfiles: cp,
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected validation failure for inventory-only endpoint")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeCapabilityMissing {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeCapabilityMissing in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("AuthUnknown for non-local endpoint fails closed", func(t *testing.T) {
		p := makeTestPortfolio()
		mpAuthUnknown := makeTestMachineProfile()
		mpAuthUnknown.Endpoints[1].Auth = protocol.AuthUnknown // ep-cli-01 auth is unknown

		p.RoleBindings[0].EndpointID = "ep-cli-01"
		p.RoleBindings[0].ChannelID = "chan-cli-01"

		validator := cognition.NewPortfolioValidator()
		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mpAuthUnknown,
			Inventory:       inv,
			ContextProfiles: cp,
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected failure when non-local endpoint has AuthUnknown")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeEndpointUnauthenticated {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeEndpointUnauthenticated in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("missing ContextProfiles fails closed", func(t *testing.T) {
		p := makeTestPortfolio()
		validator := cognition.NewPortfolioValidator()

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: nil, // unavailable
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected failure when ContextProfiles is nil")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeContextProfileNotFound {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeContextProfileNotFound in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("nil policy falls back to DefaultValidationPolicy and enforces limits", func(t *testing.T) {
		p := makeTestPortfolio()
		p.MaxSourceExposure = protocol.ExposureUnrestrictedAuthorized

		validator := cognition.NewPortfolioValidator()
		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Policy:          nil, // Should fall back to DefaultValidationPolicy
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected failure under DefaultValidationPolicy when exposure exceeds focused_snippets")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeUnauthorizedSourceExposure {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeUnauthorizedSourceExposure in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("non-metered to metered fallback requires explicit authorization on primary pool", func(t *testing.T) {
		p := makeTestPortfolio()
		meteredPool := protocol.BudgetPool{
			SchemaVersion:            protocol.SchemaVersion1,
			PoolID:                   "pool-metered",
			Name:                     "Metered API Pool",
			Regime:                   protocol.RegimeMeteredAPI,
			Unit:                     protocol.UnitUSDCents,
			HardLimit:                5000,
			SoftAlertLimit:           4000,
			Period:                   protocol.PeriodBillingCycle,
			AllowOverage:             true,
			FallbackAllowedToMetered: true,
		}
		p.BudgetPools = append(p.BudgetPools, meteredPool)

		// Set fallback to metered pool
		p.RoleBindings[0].Fallbacks[0].BudgetPoolID = "pool-metered"

		validator := cognition.NewPortfolioValidator()

		// 1. Without explicit authorization on primary pool: validation MUST fail
		resFail := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Clock:           clk,
		})
		if resFail.Valid {
			t.Fatalf("expected validation failure when primary pool forbids metered fallback")
		}
		foundUnauthorized := false
		for _, d := range resFail.Diagnostics {
			if d.Code == cognition.CodeUnauthorizedMeteredFallback {
				foundUnauthorized = true
				if d.Condition != cognition.ConditionUnauthorized {
					t.Errorf("expected ConditionUnauthorized, got: %s", d.Condition)
				}
				break
			}
		}
		if !foundUnauthorized {
			t.Errorf("expected CodeUnauthorizedMeteredFallback in diagnostics, got: %v", resFail.Diagnostics)
		}

		// 2. With explicit authorization on primary pool: entire portfolio validation MUST succeed
		p.BudgetPools[0].FallbackAllowedToMetered = true
		resPass := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Clock:           clk,
		})
		if !resPass.Valid {
			t.Fatalf("expected entire portfolio to validate successfully with explicit authorization, got diagnostics: %v", resPass.Diagnostics)
		}
	})

	t.Run("role requires tool support and structured output but endpoint does not support them", func(t *testing.T) {
		p := makeTestPortfolio()
		mpNoTools := makeTestMachineProfile()
		mpNoTools.Endpoints[0].ToolUse = protocol.FeatureUnsupported
		mpNoTools.Endpoints[0].StructuredOutput = protocol.FeatureUnsupported

		policy := cognition.ValidationPolicy{
			RoleRequirements: map[cognition.Role]cognition.RoleRequirement{
				cognition.RoleImplementer: {
					Role:                    cognition.RoleImplementer,
					RequireToolUse:          true,
					RequireStructuredOutput: true,
				},
			},
		}

		validator := cognition.NewPortfolioValidator()
		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mpNoTools,
			Inventory:       inv,
			ContextProfiles: cp,
			Policy:          &policy,
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected failure when endpoint lacks required tool use and structured output")
		}
		var foundTool, foundStructured bool
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeToolSupportMissing {
				foundTool = true
			}
			if d.Code == cognition.CodeStructuredOutputMissing {
				foundStructured = true
			}
		}
		if !foundTool || !foundStructured {
			t.Errorf("expected tool and structured output diagnostics, got: %v", res.Diagnostics)
		}
	})

	t.Run("role requires tool support but channel declares supports_tools false", func(t *testing.T) {
		p := makeTestPortfolio()
		p.Channels[0].SupportsTools = false

		policy := cognition.ValidationPolicy{
			RoleRequirements: map[cognition.Role]cognition.RoleRequirement{
				cognition.RoleImplementer: {
					Role:           cognition.RoleImplementer,
					RequireToolUse: true,
				},
			},
		}

		validator := cognition.NewPortfolioValidator()
		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Policy:          &policy,
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected failure when channel declares supports_tools: false")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeToolSupportMissing {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeToolSupportMissing, got: %v", res.Diagnostics)
		}
	})
}
