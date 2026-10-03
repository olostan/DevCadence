package cognition_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/protocol"
)

// WP-M3C-H2: fail-closed unknown resource/budget state (KG-1).

func unknownStateInput(p *protocol.CognitionPortfolio, pol *cognition.ValidationPolicy) cognition.ValidationInput {
	return cognition.ValidationInput{
		Portfolio:       p,
		MachineProfile:  makeTestMachineProfile(),
		Inventory:       makeTestInventory(),
		ContextProfiles: makeTestContextProfiles(),
		Policy:          pol,
		Clock:           clock.NewFake(time.Date(2026, 10, 2, 20, 0, 0, 0, time.UTC), 0),
	}
}

func diagsWithCode(res cognition.ValidationResult, code string) []cognition.PortfolioDiagnostic {
	var out []cognition.PortfolioDiagnostic
	for _, d := range res.Diagnostics {
		if d.Code == code {
			out = append(out, d)
		}
	}
	return out
}

func optOutPolicy() *cognition.ValidationPolicy {
	pol := cognition.DefaultValidationPolicy()
	pol.RequireKnownResourceState = false
	pol.RequireKnownBudgetState = false
	return &pol
}

func unknownHost() map[string]*protocol.ResourceState {
	return map[string]*protocol.ResourceState{
		"host-01": {HostID: "host-01", UnknownMetrics: []string{"available_gpu_memory_bytes"}},
	}
}

func unknownSub() map[string]*protocol.BudgetState {
	return map[string]*protocol.BudgetState{
		"pool-sub": {PoolID: "pool-sub", Status: protocol.BudgetStatusUnknown},
	}
}

func TestUnknownState_DefaultPolicyFlags(t *testing.T) {
	pol := cognition.DefaultValidationPolicy()
	if !pol.RequireKnownResourceState || !pol.RequireKnownBudgetState {
		t.Fatalf("REQ-01: default policy must require known resource and budget state: %+v", pol)
	}
}

func TestUnknownState_ACC01_NilPolicyUnknownResource(t *testing.T) {
	in := unknownStateInput(makeTestPortfolio(), nil)
	in.ResourceStates = unknownHost()
	res := cognition.NewPortfolioValidator().Validate(in)
	d := diagsWithCode(res, cognition.CodeUnknownResourceState)
	if res.Valid || len(d) != 1 {
		t.Fatalf("expected invalid with one UNKNOWN_RESOURCE_STATE, got valid=%v %v", res.Valid, res.Diagnostics)
	}
}

func TestUnknownState_ACC02_NilPolicyUnknownBudget(t *testing.T) {
	in := unknownStateInput(makeTestPortfolio(), nil)
	in.BudgetStates = unknownSub()
	res := cognition.NewPortfolioValidator().Validate(in)
	d := diagsWithCode(res, cognition.CodeUnknownBudgetState)
	if res.Valid || len(d) != 1 {
		t.Fatalf("expected invalid with one UNKNOWN_BUDGET_STATE, got valid=%v %v", res.Valid, res.Diagnostics)
	}
	if d[0].Condition != cognition.ConditionUnknown || d[0].ViolatedRule != "DCI-005" ||
		d[0].Observed != "unknown" || d[0].Required != "" || d[0].Target != "budget_pools[pool-sub]" {
		t.Errorf("unexpected diagnostic fields: %+v", d[0])
	}
}

func TestUnknownState_ACC03_ExplicitOptOut(t *testing.T) {
	in := unknownStateInput(makeTestPortfolio(), optOutPolicy())
	in.ResourceStates = unknownHost()
	in.BudgetStates = unknownSub()
	res := cognition.NewPortfolioValidator().Validate(in)
	if !res.Valid || len(diagsWithCode(res, cognition.CodeUnknownResourceState)) != 0 ||
		len(diagsWithCode(res, cognition.CodeUnknownBudgetState)) != 0 {
		t.Fatalf("expected opt-out to accept, got %v", res.Diagnostics)
	}
}

func TestUnknownState_ACC04_AbsentPoolInNonNilMap(t *testing.T) {
	in := unknownStateInput(makeTestPortfolio(), nil)
	in.BudgetStates = map[string]*protocol.BudgetState{}
	res := cognition.NewPortfolioValidator().Validate(in)
	d := diagsWithCode(res, cognition.CodeUnknownBudgetState)
	if res.Valid || len(d) != 1 || d[0].Observed != "missing" {
		t.Fatalf("expected one missing UNKNOWN_BUDGET_STATE, got %v", res.Diagnostics)
	}
}

func TestUnknownState_ACC05_LocalComputeExempt(t *testing.T) {
	// pool-local is local_compute: unknown and absent must not emit.
	for name, states := range map[string]map[string]*protocol.BudgetState{
		"unknown": {"pool-local": {PoolID: "pool-local", Status: protocol.BudgetStatusUnknown}, "pool-sub": {PoolID: "pool-sub", Status: protocol.BudgetStatusHealthy}},
		"absent":  {"pool-sub": {PoolID: "pool-sub", Status: protocol.BudgetStatusHealthy}},
	} {
		t.Run(name, func(t *testing.T) {
			in := unknownStateInput(makeTestPortfolio(), nil)
			in.BudgetStates = states
			res := cognition.NewPortfolioValidator().Validate(in)
			if !res.Valid || len(diagsWithCode(res, cognition.CodeUnknownBudgetState)) != 0 {
				t.Fatalf("expected valid, got %v", res.Diagnostics)
			}
		})
	}
}

func TestUnknownState_ACC06_ExhaustedOnlyExhaustion(t *testing.T) {
	in := unknownStateInput(makeTestPortfolio(), nil)
	in.BudgetStates = map[string]*protocol.BudgetState{
		"pool-sub": {PoolID: "pool-sub", Status: protocol.BudgetStatusExhausted},
	}
	res := cognition.NewPortfolioValidator().Validate(in)
	if res.Valid || len(diagsWithCode(res, cognition.CodeBudgetPoolExhausted)) != 1 ||
		len(diagsWithCode(res, cognition.CodeUnknownBudgetState)) != 0 {
		t.Fatalf("expected only exhaustion, got %v", res.Diagnostics)
	}
}

func TestUnknownState_ACC07_NilMapsUnchanged(t *testing.T) {
	in := unknownStateInput(makeTestPortfolio(), nil)
	res := cognition.NewPortfolioValidator().Validate(in)
	if !res.Valid {
		t.Fatalf("expected valid with nil state maps, got %v", res.Diagnostics)
	}
}

func twoSpendPortfolio() *protocol.CognitionPortfolio {
	p := makeTestPortfolio()
	// scout also uses a second spend-bearing pool whose id sorts before pool-sub.
	second := p.BudgetPools[1]
	second.PoolID = "pool-a-sub"
	p.BudgetPools = append(p.BudgetPools, second)
	p.RoleBindings[1].BudgetPoolID = "pool-a-sub"
	return p
}

func TestUnknownState_ACC08_DeterministicSortedOrder(t *testing.T) {
	var first []cognition.PortfolioDiagnostic
	for i := 0; i < 20; i++ {
		in := unknownStateInput(twoSpendPortfolio(), nil)
		in.BudgetStates = map[string]*protocol.BudgetState{}
		for _, id := range []string{"pool-sub", "pool-a-sub", "pool-local"} {
			in.BudgetStates[id] = &protocol.BudgetState{PoolID: id, Status: protocol.BudgetStatusUnknown}
		}
		res := cognition.NewPortfolioValidator().Validate(in)
		d := diagsWithCode(res, cognition.CodeUnknownBudgetState)
		if len(d) != 2 || d[0].Target != "budget_pools[pool-a-sub]" || d[1].Target != "budget_pools[pool-sub]" {
			t.Fatalf("expected two sorted diagnostics, got %v", d)
		}
		if first == nil {
			first = res.Diagnostics
		} else if !reflect.DeepEqual(first, res.Diagnostics) {
			t.Fatalf("diagnostics not deterministic:\n%v\n%v", first, res.Diagnostics)
		}
	}
}

func TestUnknownState_ACC09_UnusedPoolIgnored(t *testing.T) {
	p := makeTestPortfolio()
	extra := p.BudgetPools[1]
	extra.PoolID = "pool-unused"
	p.BudgetPools = append(p.BudgetPools, extra)
	in := unknownStateInput(p, nil)
	in.BudgetStates = map[string]*protocol.BudgetState{
		"pool-sub":    {PoolID: "pool-sub", Status: protocol.BudgetStatusHealthy},
		"pool-unused": {PoolID: "pool-unused", Status: protocol.BudgetStatusUnknown},
	}
	res := cognition.NewPortfolioValidator().Validate(in)
	if len(diagsWithCode(res, cognition.CodeUnknownBudgetState)) != 0 {
		t.Fatalf("unused pool must not emit, got %v", res.Diagnostics)
	}
}

func TestUnknownState_ACC11_NilEntryNoPanic(t *testing.T) {
	in := unknownStateInput(makeTestPortfolio(), nil)
	in.BudgetStates = map[string]*protocol.BudgetState{"pool-sub": nil}
	res := cognition.NewPortfolioValidator().Validate(in)
	d := diagsWithCode(res, cognition.CodeUnknownBudgetState)
	if res.Valid || len(d) != 1 || d[0].Observed != "missing" {
		t.Fatalf("expected one missing diagnostic, got %v", res.Diagnostics)
	}
	if len(diagsWithCode(res, cognition.CodeBudgetPoolExhausted)) != 0 {
		t.Errorf("nil entry must not emit exhaustion: %v", res.Diagnostics)
	}
}

func TestUnknownState_ACC12_FallbackOnlyPoolOnce(t *testing.T) {
	// pool-sub is referenced only by the implementer fallback in the base portfolio.
	in := unknownStateInput(makeTestPortfolio(), nil)
	in.BudgetStates = unknownSub()
	res := cognition.NewPortfolioValidator().Validate(in)
	if d := diagsWithCode(res, cognition.CodeUnknownBudgetState); len(d) != 1 {
		t.Fatalf("expected exactly one diagnostic, got %v", d)
	}
	// Also used by a second binding: still one diagnostic (de-duplicated).
	p := makeTestPortfolio()
	p.RoleBindings[1].BudgetPoolID = "pool-sub"
	in = unknownStateInput(p, nil)
	in.BudgetStates = unknownSub()
	res = cognition.NewPortfolioValidator().Validate(in)
	if d := diagsWithCode(res, cognition.CodeUnknownBudgetState); len(d) != 1 {
		t.Fatalf("expected de-duplicated single diagnostic, got %v", d)
	}
}

func TestUnknownState_ACC13_UnknownWithZeroBalance(t *testing.T) {
	zero := int64(0)
	in := unknownStateInput(makeTestPortfolio(), nil)
	in.BudgetStates = map[string]*protocol.BudgetState{
		"pool-sub": {PoolID: "pool-sub", Status: protocol.BudgetStatusUnknown, RemainingBalance: &zero},
	}
	res := cognition.NewPortfolioValidator().Validate(in)
	if len(diagsWithCode(res, cognition.CodeUnknownBudgetState)) != 1 || len(diagsWithCode(res, cognition.CodeBudgetPoolExhausted)) != 1 {
		t.Fatalf("expected both diagnostics, got %v", res.Diagnostics)
	}
}

func TestUnknownState_ACC14_NilPolicyDigestAndNilResourceEntry(t *testing.T) {
	def := cognition.DefaultValidationPolicy()
	v := cognition.NewPortfolioValidator()
	a := v.Validate(unknownStateInput(makeTestPortfolio(), nil))
	b := v.Validate(unknownStateInput(makeTestPortfolio(), &def))
	if a.PolicyDigest == "" || a.PolicyDigest != b.PolicyDigest {
		t.Fatalf("policy digests differ: %q vs %q", a.PolicyDigest, b.PolicyDigest)
	}
	in := unknownStateInput(makeTestPortfolio(), nil)
	in.ResourceStates = map[string]*protocol.ResourceState{"host-01": nil}
	res := v.Validate(in) // must not panic
	if !res.Valid {
		t.Fatalf("nil resource entry should be skipped, got %v", res.Diagnostics)
	}
}
