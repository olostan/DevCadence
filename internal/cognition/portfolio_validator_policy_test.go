package cognition_test

import (
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

func TestPortfolioDiagnosticsModel(t *testing.T) {
	// 1. DiagnosticCondition.Valid()
	validConditions := []cognition.DiagnosticCondition{
		cognition.ConditionInvalid,
		cognition.ConditionUnsupported,
		cognition.ConditionUnauthorized,
		cognition.ConditionUnknown,
		cognition.ConditionOverBudget,
	}
	for _, c := range validConditions {
		if !c.Valid() {
			t.Errorf("expected condition %q to be valid", c)
		}
	}
	if cognition.DiagnosticCondition("bogus").Valid() {
		t.Errorf("expected bogus condition to be invalid")
	}

	// 2. ValidationResult Summary and Err
	validRes := cognition.ValidationResult{Valid: true}
	if validRes.Err() != nil {
		t.Errorf("expected nil error for valid result, got: %v", validRes.Err())
	}
	if summary := validRes.Summary(); summary != "Portfolio is valid and policy-compliant" {
		t.Errorf("unexpected valid summary: %q", summary)
	}

	unauthDiag := cognition.PortfolioDiagnostic{
		Code:         cognition.CodeUnauthorizedSourceExposure,
		Condition:    cognition.ConditionUnauthorized,
		Target:       "role_bindings[0]",
		ViolatedRule: "DCI-124",
		Message:      "source exposure denied",
	}
	unauthRes := cognition.ValidationResult{
		Valid:       false,
		Diagnostics: []cognition.PortfolioDiagnostic{unauthDiag},
	}
	err := unauthRes.Err()
	if err == nil {
		t.Fatalf("expected error for unauth result")
	}
	if errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Errorf("expected CategoryPolicyDenied for unauthorized diagnostic, got %v", err)
	}

	invalidDiag := cognition.PortfolioDiagnostic{
		Code:         cognition.CodeEndpointNotFound,
		Condition:    cognition.ConditionInvalid,
		Target:       "role_bindings[0]",
		ViolatedRule: "DCI-123",
		Message:      "endpoint not found",
	}
	invalidRes := cognition.ValidationResult{
		Valid:       false,
		Diagnostics: []cognition.PortfolioDiagnostic{invalidDiag},
	}
	err = invalidRes.Err()
	if err == nil {
		t.Fatalf("expected error for invalid result")
	}
	if errs.CategoryOf(err) != errs.CategoryValidationFailed {
		t.Errorf("expected CategoryValidationFailed for invalid diagnostic, got %v", err)
	}

	// 3. SortedDiagnostics
	unsorted := []cognition.PortfolioDiagnostic{
		{Target: "z", Code: "B"},
		{Target: "a", Code: "Z"},
		{Target: "a", Code: "A"},
	}
	sorted := cognition.SortedDiagnostics(unsorted)
	if sorted[0].Target != "a" || sorted[0].Code != "A" {
		t.Errorf("unexpected first sorted diag: %+v", sorted[0])
	}
	if sorted[1].Target != "a" || sorted[1].Code != "Z" {
		t.Errorf("unexpected second sorted diag: %+v", sorted[1])
	}
	if sorted[2].Target != "z" || sorted[2].Code != "B" {
		t.Errorf("unexpected third sorted diag: %+v", sorted[2])
	}
}

func TestPortfolioValidatorAdditionalPolicies(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 10, 2, 20, 0, 0, 0, time.UTC), time.Second)
	validator := cognition.NewPortfolioValidator() // default policy

	t.Run("rejects primary and fallback exceeding policy MaxCostClass", func(t *testing.T) {
		p := makeTestPortfolio()
		mp := makeTestMachineProfile()
		mp.Endpoints[0].CostClass = protocol.CostFrontierExpensive

		policy := cognition.ValidationPolicy{
			MaxCostClass: protocol.CostSubscriptionIncluded,
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
			t.Fatalf("expected rejection when endpoint exceeds MaxCostClass")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeUnauthorizedCostClass {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeUnauthorizedCostClass in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("rejects disallowed economic regimes and forbidden metered API", func(t *testing.T) {
		p := makeTestPortfolio()
		mp := makeTestMachineProfile()

		policy := cognition.ValidationPolicy{
			AllowedRegimes:   []protocol.EconomicRegime{protocol.RegimeLocalCompute},
			ForbidMeteredAPI: true,
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
			t.Fatalf("expected rejection for disallowed regime")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeUnauthorizedEconomicRegime {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeUnauthorizedEconomicRegime in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("rejects budget pool with zero remaining balance and allow_overage false", func(t *testing.T) {
		p := makeTestPortfolio()
		mp := makeTestMachineProfile()
		zeroBal := int64(0)
		budgetStates := map[string]*protocol.BudgetState{
			"pool-local": {
				SchemaVersion:    protocol.SchemaVersion1,
				PoolID:           "pool-local",
				Status:           protocol.BudgetStatusHealthy,
				RemainingBalance: &zeroBal,
				ObservedAt:       "2026-10-02T20:00:00Z",
			},
		}

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       makeTestInventory(),
			ContextProfiles: makeTestContextProfiles(),
			BudgetStates:    budgetStates,
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected rejection when remaining balance is 0 and allow_overage is false")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeBudgetPoolExhausted {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeBudgetPoolExhausted in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("rejects prefix cache mismatch and unknown context control", func(t *testing.T) {
		p := makeTestPortfolio()
		mp := makeTestMachineProfile()
		cp := makeTestContextProfiles()
		// Change observed prefix cache to conflict with channel prefix cache
		prof := cp["prof-local-01"]
		prof.ObservedPrefixCache = protocol.PrefixCacheExplicit
		cp["prof-local-01"] = prof

		policy := cognition.ValidationPolicy{
			RequireKnownContextControl: true,
			MinContractLimitTokens:     200000, // prof-local-01 has 32768
		}

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       makeTestInventory(),
			ContextProfiles: cp,
			Policy:          &policy,
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected rejection for prefix cache mismatch and insufficient tokens")
		}
		foundPrefix := false
		foundLimit := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodePrefixCacheMismatch {
				foundPrefix = true
			}
			if d.Code == cognition.CodeContextWindowInsufficient {
				foundLimit = true
			}
		}
		if !foundPrefix {
			t.Errorf("expected CodePrefixCacheMismatch in diagnostics: %v", res.Diagnostics)
		}
		if !foundLimit {
			t.Errorf("expected CodeContextWindowInsufficient in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("rejects fallback with nonexistent context profile or budget pool", func(t *testing.T) {
		p := makeTestPortfolio()
		mp := makeTestMachineProfile()
		p.RoleBindings[0].Fallbacks[0].ContextProfileID = "prof-nonexistent"
		p.RoleBindings[0].Fallbacks[0].BudgetPoolID = "pool-nonexistent"

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       makeTestInventory(),
			ContextProfiles: makeTestContextProfiles(),
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected rejection for nonexistent fallback context profile and pool")
		}
		foundProf := false
		foundPool := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeContextProfileNotFound {
				foundProf = true
			}
			if d.Code == cognition.CodeBudgetPoolNotFound {
				foundPool = true
			}
		}
		if !foundProf {
			t.Errorf("expected CodeContextProfileNotFound in diagnostics: %v", res.Diagnostics)
		}
		if !foundPool {
			t.Errorf("expected CodeBudgetPoolNotFound in diagnostics: %v", res.Diagnostics)
		}
	})
}
