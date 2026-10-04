package cognition_test

import (
	"reflect"
	"testing"

	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/protocol"
)

func makeTestBudgetPortfolio() *protocol.CognitionPortfolio {
	p := makeTestPortfolio()
	p.BudgetPools = []protocol.BudgetPool{
		{
			SchemaVersion:  protocol.SchemaVersion1,
			PoolID:         "pool-local",
			Name:           "Local Workstation Compute",
			Regime:         protocol.RegimeLocalCompute,
			HardLimit:      0,
			SoftAlertLimit: 0,
			Unit:           protocol.UnitSeconds,
			Period:         protocol.PeriodRollingDay,
		},
		{
			SchemaVersion:  protocol.SchemaVersion1,
			PoolID:         "pool-metered",
			Name:           "Metered API Pool",
			Regime:         protocol.RegimeMeteredAPI,
			HardLimit:      10000,
			SoftAlertLimit: 8000,
			Unit:           protocol.UnitUSDCents,
			Period:         protocol.PeriodRollingDay,
			AllowOverage:   false,
		},
		{
			SchemaVersion:  protocol.SchemaVersion1,
			PoolID:         "pool-overage-allowed",
			Name:           "Overage Allowed API Pool",
			Regime:         protocol.RegimeMeteredAPI,
			HardLimit:      10000,
			SoftAlertLimit: 8000,
			Unit:           protocol.UnitUSDCents,
			Period:         protocol.PeriodRollingDay,
			AllowOverage:   true,
		},
	}
	p.RoleBindings = []protocol.RoleBinding{
		{
			Role:             "implementer",
			EndpointID:       "ep-local-01",
			ChannelID:        "chan-local-01",
			BudgetPoolID:     "pool-local",
			ContextProfileID: "prof-local-01",
			Priority:         1,
		},
		{
			Role:             "implementer",
			EndpointID:       "ep-local-01",
			ChannelID:        "chan-local-01",
			BudgetPoolID:     "pool-metered",
			ContextProfileID: "prof-local-01",
			Priority:         2,
		},
		{
			Role:             "implementer",
			EndpointID:       "ep-local-01",
			ChannelID:        "chan-local-01",
			BudgetPoolID:     "pool-overage-allowed",
			ContextProfileID: "prof-local-01",
			Priority:         3,
		},
	}
	return p
}

func TestWorkflowBudgetAuthorizer_ACC01_HealthyBudget(t *testing.T) {
	port := makeTestBudgetPortfolio()
	plan := wfSingle(wfCog("s1", 1, "implementer", "pool-metered"))
	states := map[string]*protocol.BudgetState{
		"pool-metered": {
			SchemaVersion:    protocol.SchemaVersion1,
			PoolID:           "pool-metered",
			Status:           protocol.BudgetStatusHealthy,
			RemainingBalance: ptr[int64](5000),
			ObservedAt:       "2026-10-04T00:00:00Z",
		},
	}

	res := cognition.AuthorizeWorkflowBudget(cognition.WorkflowBudgetInput{
		Plan:         plan,
		Portfolio:    port,
		BudgetStates: states,
	})

	if !res.Authorized {
		t.Fatalf("Authorized = false, want true; diags = %+v", res.Diagnostics)
	}
	if len(res.Diagnostics) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d", len(res.Diagnostics))
	}
}

func TestWorkflowBudgetAuthorizer_ACC02_ExhaustedBudget(t *testing.T) {
	port := makeTestBudgetPortfolio()
	plan := wfSingle(wfCog("s1", 1, "implementer", "pool-metered"))
	states := map[string]*protocol.BudgetState{
		"pool-metered": {
			SchemaVersion:    protocol.SchemaVersion1,
			PoolID:           "pool-metered",
			Status:           protocol.BudgetStatusExhausted,
			RemainingBalance: ptr[int64](0),
			ObservedAt:       "2026-10-04T00:00:00Z",
		},
	}

	res := cognition.AuthorizeWorkflowBudget(cognition.WorkflowBudgetInput{
		Plan:         plan,
		Portfolio:    port,
		BudgetStates: states,
	})

	if res.Authorized {
		t.Fatalf("Authorized = true, want false")
	}
	if len(res.Diagnostics) != 1 || res.Diagnostics[0].Code != cognition.CodeWorkflowBudgetExhausted {
		t.Fatalf("unexpected diagnostics: %+v", res.Diagnostics)
	}
	if res.Diagnostics[0].Condition != cognition.ConditionOverBudget {
		t.Fatalf("expected ConditionOverBudget, got %s", res.Diagnostics[0].Condition)
	}
	if res.Diagnostics[0].Target != "stages[0]" {
		t.Fatalf("expected Target stages[0], got %s", res.Diagnostics[0].Target)
	}
}

func TestWorkflowBudgetAuthorizer_ACC03_MissingStateOnMeteredPool(t *testing.T) {
	port := makeTestBudgetPortfolio()
	plan := wfSingle(wfCog("s1", 1, "implementer", "pool-metered"))

	res := cognition.AuthorizeWorkflowBudget(cognition.WorkflowBudgetInput{
		Plan:         plan,
		Portfolio:    port,
		BudgetStates: map[string]*protocol.BudgetState{},
	})

	if res.Authorized {
		t.Fatalf("Authorized = true, want false")
	}
	if len(res.Diagnostics) != 1 || res.Diagnostics[0].Code != cognition.CodeWorkflowBudgetMissingState {
		t.Fatalf("unexpected diagnostics: %+v", res.Diagnostics)
	}
	if res.Diagnostics[0].Condition != cognition.ConditionUnauthorized {
		t.Fatalf("expected ConditionUnauthorized, got %s", res.Diagnostics[0].Condition)
	}
}

func TestWorkflowBudgetAuthorizer_ACC04_LocalComputeExemption(t *testing.T) {
	port := makeTestBudgetPortfolio()
	plan := wfSingle(wfCog("s1", 1, "implementer", "pool-local"))

	// Missing state with exemption allowed
	res := cognition.AuthorizeWorkflowBudget(cognition.WorkflowBudgetInput{
		Plan:                     plan,
		Portfolio:                port,
		BudgetStates:             map[string]*protocol.BudgetState{},
		AllowUnknownLocalCompute: true,
	})
	if !res.Authorized {
		t.Fatalf("Authorized = false, want true with local compute exemption; diags: %+v", res.Diagnostics)
	}

	// Unknown state with exemption allowed
	res2 := cognition.AuthorizeWorkflowBudget(cognition.WorkflowBudgetInput{
		Plan:      plan,
		Portfolio: port,
		BudgetStates: map[string]*protocol.BudgetState{
			"pool-local": {
				SchemaVersion: protocol.SchemaVersion1,
				PoolID:        "pool-local",
				Status:        protocol.BudgetStatusUnknown,
				ObservedAt:    "2026-10-04T00:00:00Z",
			},
		},
		AllowUnknownLocalCompute: true,
	})
	if !res2.Authorized {
		t.Fatalf("Authorized = false, want true with local compute unknown state; diags: %+v", res2.Diagnostics)
	}

	// Missing state WITHOUT exemption fails
	res3 := cognition.AuthorizeWorkflowBudget(cognition.WorkflowBudgetInput{
		Plan:                     plan,
		Portfolio:                port,
		BudgetStates:             map[string]*protocol.BudgetState{},
		AllowUnknownLocalCompute: false,
	})
	if res3.Authorized {
		t.Fatalf("Authorized = true, want false without exemption")
	}
	if len(res3.Diagnostics) != 1 || res3.Diagnostics[0].Code != cognition.CodeWorkflowBudgetMissingState {
		t.Fatalf("unexpected diagnostics: %+v", res3.Diagnostics)
	}
}

func TestWorkflowBudgetAuthorizer_ACC05_AllowOverageMeteredRegime(t *testing.T) {
	port := makeTestBudgetPortfolio()
	plan := wfSingle(wfCog("s1", 1, "implementer", "pool-overage-allowed"))
	states := map[string]*protocol.BudgetState{
		"pool-overage-allowed": {
			SchemaVersion:    protocol.SchemaVersion1,
			PoolID:           "pool-overage-allowed",
			Status:           protocol.BudgetStatusExhausted,
			RemainingBalance: ptr[int64](0),
			ObservedAt:       "2026-10-04T00:00:00Z",
		},
	}

	res := cognition.AuthorizeWorkflowBudget(cognition.WorkflowBudgetInput{
		Plan:         plan,
		Portfolio:    port,
		BudgetStates: states,
	})

	if !res.Authorized {
		t.Fatalf("Authorized = false, want true when AllowOverage is enabled; diags = %+v", res.Diagnostics)
	}
	if len(res.Diagnostics) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d", len(res.Diagnostics))
	}
}

func TestWorkflowBudgetAuthorizer_ACC06_StopRules(t *testing.T) {
	port := makeTestBudgetPortfolio()
	plan := wfSingle(wfCog("s1", 1, "implementer", "pool-metered"))

	// Nil plan
	resNilPlan := cognition.AuthorizeWorkflowBudget(cognition.WorkflowBudgetInput{
		Plan:      nil,
		Portfolio: port,
	})
	if resNilPlan.Authorized || len(resNilPlan.Diagnostics) != 1 || resNilPlan.Diagnostics[0].Code != cognition.CodeWorkflowInputMissing {
		t.Fatalf("nil plan failed stop rule: %+v", resNilPlan)
	}

	// Nil portfolio
	resNilPort := cognition.AuthorizeWorkflowBudget(cognition.WorkflowBudgetInput{
		Plan:      plan,
		Portfolio: nil,
	})
	if resNilPort.Authorized || len(resNilPort.Diagnostics) != 1 || resNilPort.Diagnostics[0].Code != cognition.CodeWorkflowInputMissing {
		t.Fatalf("nil portfolio failed stop rule: %+v", resNilPort)
	}

	// Invalid plan
	invalidPlan := wfPlan(protocol.TopologySinglePass) // 0 stages
	resInvPlan := cognition.AuthorizeWorkflowBudget(cognition.WorkflowBudgetInput{
		Plan:      invalidPlan,
		Portfolio: port,
	})
	if resInvPlan.Authorized || len(resInvPlan.Diagnostics) != 1 || resInvPlan.Diagnostics[0].Code != cognition.CodeWorkflowPlanInvalid {
		t.Fatalf("invalid plan failed stop rule: %+v", resInvPlan)
	}
}

func TestWorkflowBudgetAuthorizer_UnknownBudgetPool(t *testing.T) {
	port := makeTestBudgetPortfolio()
	plan := wfSingle(wfCog("s1", 1, "implementer", "nonexistent-pool"))

	res := cognition.AuthorizeWorkflowBudget(cognition.WorkflowBudgetInput{
		Plan:         plan,
		Portfolio:    port,
		BudgetStates: map[string]*protocol.BudgetState{},
	})
	if res.Authorized || len(res.Diagnostics) != 1 || res.Diagnostics[0].Code != cognition.CodeWorkflowUnknownBudgetPool {
		t.Fatalf("unknown budget pool failed: %+v", res)
	}
}

func TestWorkflowBudgetAuthorizer_Immutability(t *testing.T) {
	port := makeTestBudgetPortfolio()
	plan := wfSingle(wfCog("s1", 1, "implementer", "pool-metered"))
	states := map[string]*protocol.BudgetState{
		"pool-metered": {
			SchemaVersion:    protocol.SchemaVersion1,
			PoolID:           "pool-metered",
			Status:           protocol.BudgetStatusHealthy,
			RemainingBalance: ptr[int64](5000),
			ObservedAt:       "2026-10-04T00:00:00Z",
		},
	}

	planCopy := *plan
	portCopy := *port

	_ = cognition.AuthorizeWorkflowBudget(cognition.WorkflowBudgetInput{
		Plan:         plan,
		Portfolio:    port,
		BudgetStates: states,
	})

	if !reflect.DeepEqual(*plan, planCopy) {
		t.Fatalf("plan was mutated during authorization")
	}
	if !reflect.DeepEqual(*port, portCopy) {
		t.Fatalf("portfolio was mutated during authorization")
	}
}
