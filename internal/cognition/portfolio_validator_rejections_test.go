package cognition_test

import (
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/protocol"
)

func assertHasDiagnostic(t *testing.T, res cognition.ValidationResult, code string) {
	t.Helper()
	for _, d := range res.Diagnostics {
		if d.Code == code {
			return
		}
	}
	t.Errorf("expected %s in diagnostics: %v", code, res.Diagnostics)
}

func TestPortfolioValidator_Rejections(t *testing.T) {
	validator := cognition.NewPortfolioValidator()
	clk := clock.NewFake(time.Date(2026, 10, 2, 20, 0, 0, 0, time.UTC), 0)

	t.Run("endpoint not found", func(t *testing.T) {
		p := makeTestPortfolio()
		p.RoleBindings[0].EndpointID = "ep-nonexistent"
		p.RoleBindings[0].ChannelID = "chan-local-01"

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  makeTestMachineProfile(),
			Inventory:       makeTestInventory(),
			ContextProfiles: makeTestContextProfiles(),
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected validation failure for nonexistent endpoint")
		}
		assertHasDiagnostic(t, res, cognition.CodeEndpointNotFound)
	})

	t.Run("unauthorized source exposure exceeds policy limit", func(t *testing.T) {
		p := makeTestPortfolio()
		// Portfolio allows focused snippets, but policy restricts to local only
		policy := cognition.ValidationPolicy{
			MaxSourceExposure: protocol.ExposureLocalOnly,
			MaxCostClass:      protocol.CostFrontierExpensive,
		}

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  makeTestMachineProfile(),
			Inventory:       makeTestInventory(),
			ContextProfiles: makeTestContextProfiles(),
			Policy:          &policy,
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected rejection for source exposure exceeding policy")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeUnauthorizedSourceExposure {
				found = true
				if d.Condition != cognition.ConditionUnauthorized {
					t.Errorf("expected ConditionUnauthorized, got %s", d.Condition)
				}
				break
			}
		}
		if !found {
			t.Errorf("expected CodeUnauthorizedSourceExposure in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("attempted silent metered fallback without authorization", func(t *testing.T) {
		p := makeTestPortfolio()
		// Add metered pool
		p.BudgetPools = append(p.BudgetPools, protocol.BudgetPool{
			SchemaVersion:            protocol.SchemaVersion1,
			PoolID:                   "pool-metered",
			Name:                     "Metered Pool",
			Regime:                   protocol.RegimeMeteredAPI,
			Unit:                     protocol.UnitUSDCents,
			HardLimit:                1000,
			SoftAlertLimit:           800,
			Period:                   protocol.PeriodRollingDay,
			FallbackAllowedToMetered: true,
		})
		// Primary pool is local compute without FallbackAllowedToMetered
		p.RoleBindings[0].BudgetPoolID = "pool-local"
		p.RoleBindings[0].Fallbacks[0].BudgetPoolID = "pool-metered"

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  makeTestMachineProfile(),
			Inventory:       makeTestInventory(),
			ContextProfiles: makeTestContextProfiles(),
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected rejection for unauthorized metered fallback")
		}
		assertHasDiagnostic(t, res, cognition.CodeUnauthorizedMeteredFallback)
	})

	t.Run("budget pool exhausted in BudgetState", func(t *testing.T) {
		p := makeTestPortfolio()
		budgetStates := map[string]*protocol.BudgetState{
			"pool-local": {
				SchemaVersion:    protocol.SchemaVersion1,
				PoolID:           "pool-local",
				Status:           protocol.BudgetStatusExhausted,
				ObservedAt:       "2026-10-02T20:00:00Z",
				RemainingBalance: ptr(int64(0)),
			},
		}

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  makeTestMachineProfile(),
			Inventory:       makeTestInventory(),
			ContextProfiles: makeTestContextProfiles(),
			BudgetStates:    budgetStates,
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected rejection for exhausted budget pool")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeBudgetPoolExhausted {
				found = true
				if d.Condition != cognition.ConditionOverBudget {
					t.Errorf("expected ConditionOverBudget, got %s", d.Condition)
				}
				break
			}
		}
		if !found {
			t.Errorf("expected CodeBudgetPoolExhausted in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("context control mismatch between channel and context profile", func(t *testing.T) {
		p := makeTestPortfolio()
		cp := makeTestContextProfiles()
		// Force channel context_control to opaque_session while profile observed exact_stateless
		p.Channels[0].ContextControl = protocol.ContextControlOpaqueSession

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  makeTestMachineProfile(),
			Inventory:       makeTestInventory(),
			ContextProfiles: cp,
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected rejection for context control mismatch")
		}
		assertHasDiagnostic(t, res, cognition.CodeContextControlMismatch)
	})

	t.Run("hardware accelerator unavailable on machine", func(t *testing.T) {
		p := makeTestPortfolio()
		mp := makeTestMachineProfile()
		inv := makeTestInventory()
		cp := makeTestContextProfiles()

		// Endpoint claims CUDA backend, but machine inventory only has Metal
		mp.Endpoints[0].Acceleration = &protocol.AccelerationEvidence{
			Backend: protocol.BackendCUDA,
			State:   protocol.StateVerified,
		}

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected rejection for missing CUDA accelerator")
		}
		assertHasDiagnostic(t, res, cognition.CodeHardwareBackendUnavailable)
	})

	t.Run("resource capacity exceeded active slots", func(t *testing.T) {
		p := makeTestPortfolio()
		resStates := map[string]*protocol.ResourceState{
			"host-01": {
				SchemaVersion:      protocol.SchemaVersion1,
				HostID:             "host-01",
				Timestamp:          "2026-10-02T20:00:00Z",
				MaxConcurrentSlots: ptr(4),
				ActiveSlots:        ptr(4), // saturated
			},
		}

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  makeTestMachineProfile(),
			Inventory:       makeTestInventory(),
			ContextProfiles: makeTestContextProfiles(),
			ResourceStates:  resStates,
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected rejection for saturated slots")
		}
		assertHasDiagnostic(t, res, cognition.CodeResourceCapacityExceeded)
	})

	t.Run("diversity requirements violated when review roles share endpoint", func(t *testing.T) {
		p := makeTestPortfolio()
		// Add two review roles bound to the same endpoint
		p.RoleBindings = append(p.RoleBindings,
			protocol.RoleBinding{
				Role:             "correctness_reviewer",
				EndpointID:       "ep-local-01",
				ChannelID:        "chan-local-01",
				BudgetPoolID:     "pool-local",
				ContextProfileID: "prof-local-01",
				Priority:         1,
			},
			protocol.RoleBinding{
				Role:             "architecture_reviewer",
				EndpointID:       "ep-local-01", // duplicate endpoint!
				ChannelID:        "chan-local-01",
				BudgetPoolID:     "pool-local",
				ContextProfileID: "prof-local-01",
				Priority:         1,
			},
		)
		p.DiversityRequirements = &protocol.DiversityPolicy{
			RequireDistinctEndpointsForReview: true,
		}

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  makeTestMachineProfile(),
			Inventory:       makeTestInventory(),
			ContextProfiles: makeTestContextProfiles(),
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected rejection for review diversity violation")
		}
		assertHasDiagnostic(t, res, cognition.CodeDiversityViolation)
	})

	t.Run("capability grade insufficient", func(t *testing.T) {
		p := makeTestPortfolio()
		mp := makeTestMachineProfile()
		// Set implementer capability to low (requires strong)
		mp.Endpoints[0].Capabilities[0].Grade = protocol.GradeLow

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       makeTestInventory(),
			ContextProfiles: makeTestContextProfiles(),
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected rejection for insufficient capability grade")
		}
		assertHasDiagnostic(t, res, cognition.CodeCapabilityGradeInsufficient)
	})

	t.Run("capability provenance invalid when measured required", func(t *testing.T) {
		p := makeTestPortfolio()
		mp := makeTestMachineProfile()
		// Set provenance to configured
		mp.Endpoints[0].Capabilities[0].Provenance = protocol.ProvenanceConfigured

		policy := cognition.ValidationPolicy{
			MaxSourceExposure:         protocol.ExposureFocusedSnippets,
			MaxCostClass:              protocol.CostFrontierExpensive,
			RequireMeasuredProvenance: true,
		}

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       makeTestInventory(),
			ContextProfiles: makeTestContextProfiles(),
			Policy:          &policy,
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected rejection for configured provenance when measured required")
		}
		assertHasDiagnostic(t, res, cognition.CodeCapabilityProvenanceInvalid)
	})
}
