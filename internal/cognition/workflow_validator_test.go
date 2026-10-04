package cognition_test

import (
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/protocol"
)

func wfCog(id string, order int, role, pool string) protocol.WorkflowStage {
	return protocol.WorkflowStage{
		StageID: id, Role: role, Kind: protocol.StageKindCognition, Order: order,
		BudgetPoolID: pool, TimeoutSeconds: 600,
	}
}

func wfDet(id string, order int, pool string) protocol.WorkflowStage {
	return protocol.WorkflowStage{
		StageID: id, Role: "gate", Kind: protocol.StageKindDeterministic, Order: order,
		BudgetPoolID: pool, TimeoutSeconds: 60, DeterministicGateID: ptr("gate-01"),
	}
}

func wfPlan(topology protocol.WorkflowTopologyKind, stages ...protocol.WorkflowStage) *protocol.WorkflowPlan {
	return &protocol.WorkflowPlan{
		SchemaVersion: protocol.SchemaVersion1, PlanID: "plan-01", TaskID: "task-01",
		WorkPackageID: "wp-01", Topology: topology, Stages: stages,
	}
}

func wfSingle(stages ...protocol.WorkflowStage) *protocol.WorkflowPlan {
	return wfPlan(protocol.TopologySinglePass, stages...)
}

func wfRun(plan *protocol.WorkflowPlan, port *protocol.CognitionPortfolio, pol *cognition.WorkflowPolicy) cognition.WorkflowValidationResult {
	return cognition.NewWorkflowValidator().Validate(cognition.WorkflowValidationInput{Plan: plan, Portfolio: port, Policy: pol})
}

func wfRunDefault(plan *protocol.WorkflowPlan) cognition.WorkflowValidationResult {
	return wfRun(plan, makeTestPortfolio(), nil)
}

func wfKeys(r cognition.WorkflowValidationResult) []string {
	var out []string
	for _, d := range r.Diagnostics {
		out = append(out, d.Code+"@"+d.Target)
	}
	return out
}

func wfExpectInvalid(t *testing.T, r cognition.WorkflowValidationResult, want ...string) {
	t.Helper()
	if r.Valid {
		t.Fatalf("Valid = true, want false (diagnostics %v)", wfKeys(r))
	}
	if got := wfKeys(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("diagnostics = %v, want %v", got, want)
	}
}

func wfExpectValid(t *testing.T, r cognition.WorkflowValidationResult) {
	t.Helper()
	if !r.Valid || len(r.Diagnostics) != 0 {
		t.Fatalf("Valid = %v diagnostics %v, want valid", r.Valid, wfKeys(r))
	}
	if r.PlanDigest == "" || r.PortfolioDigest == "" || r.PolicyDigest == "" {
		t.Fatalf("digests must be set: %+v", r)
	}
}

func wfNoDigests(t *testing.T, r cognition.WorkflowValidationResult) {
	t.Helper()
	if r.PlanDigest != "" || r.PortfolioDigest != "" || r.PolicyDigest != "" {
		t.Fatalf("digests must be empty on a stop: %+v", r)
	}
}

func TestWorkflowValidator_ACC01_ValidPlans(t *testing.T) {
	t.Run("single_pass", func(t *testing.T) {
		wfExpectValid(t, wfRunDefault(wfSingle(wfCog("s1", 1, "implementer", "pool-local"))))
	})
	t.Run("dual_independent_review", func(t *testing.T) {
		a := wfCog("r1", 1, "scout", "pool-local")
		a.IsReview = true
		a.EndpointID = ptr("ep-local-01")
		b := wfCog("r2", 2, "implementer", "pool-sub")
		b.IsReview = true
		b.EndpointID = ptr("ep-cli-01")
		wfExpectValid(t, wfRunDefault(wfPlan(protocol.TopologyDualIndependentReview, a, b)))
		// Pointers nil, distinct roles.
		a.EndpointID, b.EndpointID = nil, nil
		b.BudgetPoolID = "pool-local"
		wfExpectValid(t, wfRunDefault(wfPlan(protocol.TopologyDualIndependentReview, a, b)))
	})
	t.Run("deterministic_only", func(t *testing.T) {
		wfExpectValid(t, wfRunDefault(wfPlan(protocol.TopologyDeterministicOnly, wfDet("d1", 1, "pool-local"))))
	})
}

func TestWorkflowValidator_ACC02_MissingInput(t *testing.T) {
	cases := map[string]struct {
		plan   *protocol.WorkflowPlan
		port   *protocol.CognitionPortfolio
		target string
	}{
		"nil plan":      {nil, makeTestPortfolio(), "plan"},
		"nil portfolio": {wfSingle(wfCog("s1", 1, "scout", "pool-local")), nil, "portfolio"},
		"both nil":      {nil, nil, "plan"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := wfRun(c.plan, c.port, nil)
			wfExpectInvalid(t, r, cognition.CodeWorkflowInputMissing+"@"+c.target)
			d := r.Diagnostics[0]
			if d.Condition != cognition.ConditionInvalid || d.ViolatedRule != "DCI-123" || d.Observed != "" || d.Required != "" {
				t.Fatalf("bad fields: %+v", d)
			}
			wfNoDigests(t, r)
		})
	}
}

func TestWorkflowValidator_ACC03_InvalidPolicy(t *testing.T) {
	for _, pol := range []cognition.WorkflowPolicy{{0, 6, 3600}, {8, 0, 3600}, {8, 6, -1}} {
		t.Run(fmt.Sprintf("%+v", pol), func(t *testing.T) {
			p := pol
			r := wfRun(wfSingle(wfCog("s1", 1, "scout", "pool-local")), makeTestPortfolio(), &p)
			wfExpectInvalid(t, r, cognition.CodeWorkflowPolicyInvalid+"@policy")
			d := r.Diagnostics[0]
			if d.Condition != cognition.ConditionInvalid || d.ViolatedRule != "DCI-123" {
				t.Fatalf("bad fields: %+v", d)
			}
			wfNoDigests(t, r)
		})
	}
}

func TestWorkflowValidator_ACC04_StopRules(t *testing.T) {
	t.Run("plan invalid stops", func(t *testing.T) {
		// Duplicate stage ids, plus an unknown pool that R5 would report.
		plan := wfSingle(wfCog("dup", 1, "scout", "pool-local"), wfCog("dup", 2, "scout", "pool-ghost"))
		r := wfRunDefault(plan)
		wfExpectInvalid(t, r, cognition.CodeWorkflowPlanInvalid+"@plan")
		wfNoDigests(t, r)
	})
	t.Run("portfolio invalid stops", func(t *testing.T) {
		port := makeTestPortfolio()
		port.RoleBindings[0].EndpointID = ""
		r := wfRun(wfSingle(wfCog("s1", 1, "ghost-role", "pool-local")), port, nil)
		wfExpectInvalid(t, r, cognition.CodeWorkflowPortfolioInvalid+"@portfolio")
		wfNoDigests(t, r)
	})
	t.Run("message truncated at rune boundary", func(t *testing.T) {
		long := strings.Repeat("é", 200) // 2 bytes per rune
		plan := wfSingle(wfCog(long, 1, "scout", "pool-local"), wfCog(long, 2, "scout", "pool-local"))
		r := wfRunDefault(plan)
		wfExpectInvalid(t, r, cognition.CodeWorkflowPlanInvalid+"@plan")
		m := r.Diagnostics[0].Message
		if len(m) > 256 || len(m) < 254 || !utf8.ValidString(m) {
			t.Fatalf("message len %d: %q", len(m), m)
		}
		if strings.ContainsRune(m, '�') {
			t.Fatalf("message cut inside a rune")
		}
	})
}

func wfStages(n int) []protocol.WorkflowStage {
	var out []protocol.WorkflowStage
	for i := 1; i <= n; i++ {
		out = append(out, wfCog(fmt.Sprintf("s%d", i), i, "scout", "pool-local"))
	}
	return out
}

func TestWorkflowValidator_ACC05_StageBound(t *testing.T) {
	wfExpectValid(t, wfRunDefault(wfSingle(wfStages(8)...)))
	r := wfRunDefault(wfSingle(wfStages(9)...))
	wfExpectInvalid(t, r, cognition.CodeWorkflowUnboundedStages+"@stages")
	d := r.Diagnostics[0]
	if d.Observed != "9" || d.Required != "<= 8" || d.Condition != cognition.ConditionOverBudget || d.ViolatedRule != "FR-066" {
		t.Fatalf("bad fields: %+v", d)
	}
	r = wfRun(wfSingle(wfStages(4)...), makeTestPortfolio(), &cognition.WorkflowPolicy{MaxStages: 3, MaxTotalRetries: 6, MaxStageTimeoutSeconds: 3600})
	wfExpectInvalid(t, r, cognition.CodeWorkflowUnboundedStages+"@stages")
	if r.Diagnostics[0].Observed != "4" || r.Diagnostics[0].Required != "<= 3" {
		t.Fatalf("bad fields: %+v", r.Diagnostics[0])
	}
	// Exactly MaxStages under an explicit policy is valid.
	wfExpectValid(t, wfRun(wfSingle(wfStages(3)...), makeTestPortfolio(), &cognition.WorkflowPolicy{MaxStages: 3, MaxTotalRetries: 6, MaxStageTimeoutSeconds: 3600}))
}

func TestWorkflowValidator_ACC06_RetryAndTimeoutBounds(t *testing.T) {
	withRetry := func(n int) protocol.WorkflowStage {
		s := wfCog("s1", 1, "scout", "pool-local")
		s.RetryLimit = n
		return s
	}
	t.Run("portfolio default caps a stage", func(t *testing.T) {
		port := makeTestPortfolio()
		port.WorkflowDefaults = &protocol.WorkflowDefaults{MaxRetries: 1}
		r := wfRun(wfSingle(withRetry(2)), port, nil)
		wfExpectInvalid(t, r, cognition.CodeWorkflowRetryBound+"@stages[0]")
		d := r.Diagnostics[0]
		if d.Observed != "2" || d.Required != "<= 1" || d.Condition != cognition.ConditionOverBudget || d.ViolatedRule != "FR-066" {
			t.Fatalf("bad fields: %+v", d)
		}
	})
	t.Run("defaults with zero max_retries fall back", func(t *testing.T) {
		port := makeTestPortfolio()
		port.WorkflowDefaults = &protocol.WorkflowDefaults{DefaultTimeoutSeconds: 30}
		wfExpectValid(t, wfRun(wfSingle(withRetry(3)), port, nil))
	})
	t.Run("no defaults, stage over policy cap", func(t *testing.T) {
		r := wfRunDefault(wfSingle(withRetry(7)))
		wfExpectInvalid(t, r, cognition.CodeWorkflowRetryBound+"@stages", cognition.CodeWorkflowRetryBound+"@stages[0]")
	})
	t.Run("portfolio cap above aggregate still aggregates", func(t *testing.T) {
		port := makeTestPortfolio()
		port.WorkflowDefaults = &protocol.WorkflowDefaults{MaxRetries: 10}
		wfExpectInvalid(t, wfRun(wfSingle(withRetry(7)), port, nil), cognition.CodeWorkflowRetryBound+"@stages")
	})
	t.Run("aggregate only", func(t *testing.T) {
		a, b := wfCog("s1", 1, "scout", "pool-local"), wfCog("s2", 2, "scout", "pool-local")
		a.RetryLimit, b.RetryLimit = 4, 3
		r := wfRunDefault(wfSingle(a, b))
		wfExpectInvalid(t, r, cognition.CodeWorkflowRetryBound+"@stages")
		d := r.Diagnostics[0]
		if d.Observed != "7" || d.Required != "<= 6" {
			t.Fatalf("bad fields: %+v", d)
		}
		a.RetryLimit, b.RetryLimit = 3, 3
		wfExpectValid(t, wfRunDefault(wfSingle(a, b)))
	})
	t.Run("timeout", func(t *testing.T) {
		s := wfCog("s1", 1, "scout", "pool-local")
		s.TimeoutSeconds = 3601
		r := wfRunDefault(wfSingle(s))
		wfExpectInvalid(t, r, cognition.CodeWorkflowTimeoutBound+"@stages[0]")
		d := r.Diagnostics[0]
		if d.Observed != "3601" || d.Required != "<= 3600" || d.Condition != cognition.ConditionOverBudget || d.ViolatedRule != "FR-066" {
			t.Fatalf("bad fields: %+v", d)
		}
		s.TimeoutSeconds = 3600
		wfExpectValid(t, wfRunDefault(wfSingle(s)))
	})
}

func TestWorkflowValidator_ACC07_RoleUnbound(t *testing.T) {
	r := wfRunDefault(wfSingle(wfCog("s1", 1, "scout", "pool-local"), wfCog("s2", 2, "ghost", "pool-ghost")))
	// ROLE_UNBOUND suppresses the binding check; the unknown pool is a separate rule.
	wfExpectInvalid(t, r, cognition.CodeWorkflowRoleUnbound+"@stages[1]", cognition.CodeWorkflowUnknownBudgetPool+"@stages[1]")
	for _, d := range r.Diagnostics {
		if d.Code == cognition.CodeWorkflowRoleUnbound && (d.Observed != "ghost" || d.Condition != cognition.ConditionUnauthorized || d.ViolatedRule != "DCI-123") {
			t.Fatalf("bad fields: %+v", d)
		}
	}
	wfExpectInvalid(t, wfRunDefault(wfSingle(wfCog("s1", 1, "ghost", "pool-local"))), cognition.CodeWorkflowRoleUnbound+"@stages[0]")
}

func TestWorkflowValidator_ACC08_BindingMatch(t *testing.T) {
	impl := func(ep, ch, prof *string, pool string) protocol.WorkflowStage {
		s := wfCog("s1", 1, "implementer", pool)
		s.EndpointID, s.ChannelID, s.ContextProfileID = ep, ch, prof
		return s
	}
	t.Run("fallback tuple matches", func(t *testing.T) {
		wfExpectValid(t, wfRunDefault(wfSingle(impl(ptr("ep-cli-01"), ptr("chan-cli-01"), ptr("prof-cli-01"), "pool-sub"))))
		wfExpectValid(t, wfRunDefault(wfSingle(impl(ptr("ep-cli-01"), nil, nil, "pool-sub"))))
	})
	t.Run("mixed tuples mismatch", func(t *testing.T) {
		r := wfRunDefault(wfSingle(impl(ptr("ep-local-01"), ptr("chan-cli-01"), nil, "pool-local")))
		wfExpectInvalid(t, r, cognition.CodeWorkflowBindingMismatch+"@stages[0]")
		d := r.Diagnostics[0]
		if d.Observed != "ep-local-01/chan-cli-01/-/pool-local" || d.Condition != cognition.ConditionUnauthorized || d.ViolatedRule != "DCI-123" || d.Required != "" {
			t.Fatalf("bad fields: %+v", d)
		}
	})
	t.Run("mixed tuples across profile", func(t *testing.T) {
		wfExpectInvalid(t, wfRunDefault(wfSingle(impl(nil, ptr("chan-local-01"), ptr("prof-cli-01"), "pool-local"))),
			cognition.CodeWorkflowBindingMismatch+"@stages[0]")
	})
	t.Run("pool differs from matched tuple", func(t *testing.T) {
		wfExpectInvalid(t, wfRunDefault(wfSingle(impl(ptr("ep-cli-01"), nil, nil, "pool-local"))),
			cognition.CodeWorkflowBindingMismatch+"@stages[0]")
	})
	t.Run("pool alone must match some tuple", func(t *testing.T) {
		wfExpectValid(t, wfRunDefault(wfSingle(impl(nil, nil, nil, "pool-sub"))))
	})
	t.Run("several bindings per role", func(t *testing.T) {
		port := makeTestPortfolio()
		port.RoleBindings = append(port.RoleBindings, protocol.RoleBinding{
			Role: "scout", EndpointID: "ep-cli-01", ChannelID: "chan-cli-01", BudgetPoolID: "pool-sub", ContextProfileID: "prof-cli-01", Priority: 2,
		})
		s := wfCog("s1", 1, "scout", "pool-sub")
		s.EndpointID = ptr("ep-cli-01")
		wfExpectValid(t, wfRun(wfSingle(s), port, nil))
	})
}

func TestWorkflowValidator_ACC09_UnknownPool(t *testing.T) {
	r := wfRunDefault(wfSingle(wfCog("s1", 1, "scout", "pool-ghost"), wfDet("d1", 2, "pool-ghost")))
	wfExpectInvalid(t, r,
		cognition.CodeWorkflowBindingMismatch+"@stages[0]",
		cognition.CodeWorkflowUnknownBudgetPool+"@stages[0]",
		cognition.CodeWorkflowUnknownBudgetPool+"@stages[1]")
	for _, d := range r.Diagnostics {
		if d.Code == cognition.CodeWorkflowUnknownBudgetPool && (d.Observed != "pool-ghost" || d.Condition != cognition.ConditionUnauthorized || d.ViolatedRule != "DCI-123") {
			t.Fatalf("bad fields: %+v", d)
		}
	}
}

func TestWorkflowValidator_ACC09_UnknownPoolDeterministicOnly(t *testing.T) {
	r := wfRunDefault(wfPlan(protocol.TopologyDeterministicOnly, wfDet("d1", 1, "pool-ghost")))
	wfExpectInvalid(t, r, cognition.CodeWorkflowUnknownBudgetPool+"@stages[0]")
}

func TestWorkflowValidator_ACC10_EscalationTarget(t *testing.T) {
	cases := map[string]struct {
		target string
		valid  bool
	}{
		"unknown":      {"ghost", false},
		"self":         {"s2", false},
		"lower order":  {"s1", false},
		"higher order": {"s3", true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s2 := wfCog("s2", 2, "scout", "pool-local")
			s2.EscalationTarget = ptr(c.target)
			r := wfRunDefault(wfSingle(wfCog("s1", 1, "scout", "pool-local"), s2, wfCog("s3", 3, "scout", "pool-local")))
			if c.valid {
				wfExpectValid(t, r)
				return
			}
			wfExpectInvalid(t, r, cognition.CodeWorkflowEscalationTarget+"@stages[1]")
			d := r.Diagnostics[0]
			if d.Observed != c.target || d.Condition != cognition.ConditionInvalid || d.ViolatedRule != "DCI-123" || d.Required != "" {
				t.Fatalf("bad fields: %+v", d)
			}
		})
	}
}

func TestWorkflowValidator_ACC11_ReviewNotCognition(t *testing.T) {
	d := wfDet("d1", 1, "pool-local")
	d.IsReview = true
	r := wfRunDefault(wfSingle(d))
	wfExpectInvalid(t, r, cognition.CodeWorkflowReviewNotCognition+"@stages[0]")
	got := r.Diagnostics[0]
	if got.Observed != "deterministic" || got.Condition != cognition.ConditionInvalid || got.ViolatedRule != "DCI-123" {
		t.Fatalf("bad fields: %+v", got)
	}
}

func TestWorkflowValidator_ACC12_OrderingAndDeterminism(t *testing.T) {
	stages := wfStages(11)
	stages[0].RetryLimit, stages[1].RetryLimit = 4, 3 // aggregate 7 > 6
	stages[2].TimeoutSeconds = 3601                   // stages[2]
	stages[10].TimeoutSeconds = 3601                  // stages[10]
	pol := &cognition.WorkflowPolicy{MaxStages: 12, MaxTotalRetries: 6, MaxStageTimeoutSeconds: 3600}
	want := []string{
		cognition.CodeWorkflowRetryBound + "@stages",
		cognition.CodeWorkflowTimeoutBound + "@stages[10]",
		cognition.CodeWorkflowTimeoutBound + "@stages[2]",
	}
	first := wfRun(wfSingle(stages...), makeTestPortfolio(), pol)
	wfExpectInvalid(t, first, want...)
	for i := 0; i < 20; i++ {
		again := wfRun(wfSingle(stages...), makeTestPortfolio(), pol)
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("run %d differs", i)
		}
	}
	if !reflect.DeepEqual(first.Diagnostics, cognition.SortedDiagnostics(first.Diagnostics)) {
		t.Fatal("diagnostics are not in SortedDiagnostics order")
	}
}

func wfUnorderedPlan() *protocol.WorkflowPlan {
	a := wfCog("sa", 3, "scout", "pool-local")
	a.EndpointID = ptr("ep-local-01")
	b := wfCog("sb", 1, "implementer", "pool-sub")
	b.DependsOn = nil
	c := wfCog("sc", 2, "scout", "pool-local")
	c.EscalationTarget = ptr("sa")
	return wfSingle(a, b, c)
}

func TestWorkflowValidator_ACC13_PurityAndDigests(t *testing.T) {
	plan, port := wfUnorderedPlan(), makeTestPortfolio()
	port.WorkflowDefaults = &protocol.WorkflowDefaults{MaxRetries: 2}
	pol := cognition.DefaultWorkflowPolicy()
	wantPlan, wantPort := wfUnorderedPlan(), makeTestPortfolio()
	wantPort.WorkflowDefaults = &protocol.WorkflowDefaults{MaxRetries: 2}
	wantPol := pol
	r1 := wfRun(plan, port, &pol)
	if !reflect.DeepEqual(plan, wantPlan) || !reflect.DeepEqual(port, wantPort) || !reflect.DeepEqual(pol, wantPol) {
		t.Fatal("inputs were mutated")
	}
	if plan.Stages[0].StageID != "sa" || plan.Stages[1].StageID != "sb" || plan.Stages[2].StageID != "sc" {
		t.Fatal("stage slice order changed")
	}
	wfExpectValid(t, r1)
	r2 := wfRun(wfUnorderedPlan(), port, nil)
	if r1.PlanDigest != r2.PlanDigest || r1.PortfolioDigest != r2.PortfolioDigest {
		t.Fatal("digests of equal inputs differ")
	}
	if r1.PolicyDigest != r2.PolicyDigest {
		t.Fatal("explicit default policy digest must equal the nil-policy digest")
	}
	if !strings.HasPrefix(r1.PlanDigest, "sha256:") || len(r1.PlanDigest) != len("sha256:")+64 {
		t.Fatalf("bad digest %q", r1.PlanDigest)
	}
	other := cognition.WorkflowPolicy{MaxStages: 9, MaxTotalRetries: 6, MaxStageTimeoutSeconds: 3600}
	if wfRun(plan, port, &other).PolicyDigest == r1.PolicyDigest {
		t.Fatal("different policy must give a different digest")
	}
}

func TestWorkflowValidator_ACC14_NilPointersNotCompared(t *testing.T) {
	s := wfCog("s1", 1, "implementer", "pool-local")
	if s.EndpointID != nil || s.ChannelID != nil || s.ContextProfileID != nil {
		t.Fatal("fixture must carry nil pointers")
	}
	wfExpectValid(t, wfRunDefault(wfSingle(s)))
}

func TestWorkflowValidator_ACC15_Docs(t *testing.T) {
	b, err := os.ReadFile("../../docs/COGNITION_PORTFOLIO.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	for _, want := range []string{"Workflow plan validator", "WP-M3D-2A2", "WP-M3D-2B", "WP-M3D-2C", "max_stages"} {
		if !strings.Contains(doc, want) {
			t.Errorf("COGNITION_PORTFOLIO.md missing %q", want)
		}
	}
}

func TestWorkflowValidator_ACC16_DeterministicPointers(t *testing.T) {
	mk := func(f func(*protocol.WorkflowStage)) cognition.WorkflowValidationResult {
		d := wfDet("d1", 1, "pool-local")
		f(&d)
		return wfRunDefault(wfPlan(protocol.TopologyDeterministicOnly, d))
	}
	for name, f := range map[string]func(*protocol.WorkflowStage){
		"endpoint": func(s *protocol.WorkflowStage) { s.EndpointID = ptr("ep-local-01") },
		"channel":  func(s *protocol.WorkflowStage) { s.ChannelID = ptr("chan-local-01") },
		"profile":  func(s *protocol.WorkflowStage) { s.ContextProfileID = ptr("prof-local-01") },
	} {
		t.Run(name, func(t *testing.T) {
			r := mk(f)
			wfExpectInvalid(t, r, cognition.CodeWorkflowBindingMismatch+"@stages[0]")
			d := r.Diagnostics[0]
			if d.Condition != cognition.ConditionUnauthorized || d.ViolatedRule != "DCI-123" || d.Observed != "deterministic stage carries a cognition binding" {
				t.Fatalf("bad fields: %+v", d)
			}
		})
	}
	wfExpectValid(t, mk(func(*protocol.WorkflowStage) {}))
}

func TestWorkflowValidator_ACC17_RetrySumSaturates(t *testing.T) {
	a, b := wfCog("s1", 1, "scout", "pool-local"), wfCog("s2", 2, "scout", "pool-local")
	a.RetryLimit, b.RetryLimit = math.MaxInt, math.MaxInt
	port := makeTestPortfolio()
	port.WorkflowDefaults = &protocol.WorkflowDefaults{MaxRetries: math.MaxInt}
	r := wfRun(wfSingle(a, b), port, nil)
	wfExpectInvalid(t, r, cognition.CodeWorkflowRetryBound+"@stages")
	if r.Diagnostics[0].Observed != fmt.Sprint(int64(math.MaxInt64)) {
		t.Fatalf("sum must saturate, got %s", r.Diagnostics[0].Observed)
	}
}
