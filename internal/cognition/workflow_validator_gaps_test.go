package cognition_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/protocol"
)

func wfPol(s, r, t int) *cognition.WorkflowPolicy {
	return &cognition.WorkflowPolicy{MaxStages: s, MaxTotalRetries: r, MaxStageTimeoutSeconds: t}
}

// Review gap 1: fallbacks of a later binding of the same role are candidates.
func TestWorkflowValidator_ACC08_LaterBindingFallbacks(t *testing.T) {
	port := makeTestPortfolio()
	port.RoleBindings = append(port.RoleBindings, protocol.RoleBinding{
		Role: "scout", EndpointID: "ep-local-01", ChannelID: "chan-local-01",
		BudgetPoolID: "pool-local", ContextProfileID: "prof-local-01", Priority: 2,
		Fallbacks: []protocol.FallbackBinding{{
			EndpointID: "ep-cli-01", ChannelID: "chan-cli-01", BudgetPoolID: "pool-sub", ContextProfileID: "prof-cli-01",
		}},
	})
	ok := wfCog("s1", 1, "scout", "pool-sub")
	ok.EndpointID, ok.ChannelID, ok.ContextProfileID = ptr("ep-cli-01"), ptr("chan-cli-01"), ptr("prof-cli-01")
	wfExpectValid(t, wfRun(wfSingle(ok), port, nil))
	bad := ok
	bad.ChannelID = ptr("chan-local-01")
	wfExpectInvalid(t, wfRun(wfSingle(bad), port, nil), cognition.CodeWorkflowBindingMismatch+"@stages[0]")
}

// Review gap 2: each policy field at 0 and at -1 is invalid.
func TestWorkflowValidator_ACC03_EachFieldZeroAndNegative(t *testing.T) {
	for _, pol := range []cognition.WorkflowPolicy{
		{0, 6, 3600}, {-1, 6, 3600}, {8, 0, 3600}, {8, -1, 3600}, {8, 6, 0}, {8, 6, -1},
	} {
		p := pol
		r := wfRun(wfSingle(wfCog("s1", 1, "scout", "pool-local")), makeTestPortfolio(), &p)
		wfExpectInvalid(t, r, cognition.CodeWorkflowPolicyInvalid+"@policy")
		wfNoDigests(t, r)
	}
}

// Review gap 3: explicit non-default values are honored per field.
func TestWorkflowValidator_ACC05_ExplicitPolicyHonored(t *testing.T) {
	withRetry := func(id string, order, n, timeout int) protocol.WorkflowStage {
		s := wfCog(id, order, "scout", "pool-local")
		s.RetryLimit, s.TimeoutSeconds = n, timeout
		return s
	}
	wide := wfPol(8, 10, 7200)
	// aggregate 8..10 and timeout up to 7200 are valid under the wide policy.
	wfExpectValid(t, wfRun(wfSingle(withRetry("s1", 1, 4, 3601), withRetry("s2", 2, 4, 7200)), makeTestPortfolio(), wide))
	wfExpectValid(t, wfRun(wfSingle(withRetry("s1", 1, 10, 600)), makeTestPortfolio(), wide)) // per-stage fallback cap = 10
	wfExpectValid(t, wfRun(wfSingle(withRetry("s1", 1, 5, 600), withRetry("s2", 2, 5, 600)), makeTestPortfolio(), wide))
	// above the wide caps.
	wfExpectInvalid(t, wfRun(wfSingle(withRetry("s1", 1, 6, 600), withRetry("s2", 2, 5, 600)), makeTestPortfolio(), wide),
		cognition.CodeWorkflowRetryBound+"@stages")
	wfExpectInvalid(t, wfRun(wfSingle(withRetry("s1", 1, 0, 7201)), makeTestPortfolio(), wide),
		cognition.CodeWorkflowTimeoutBound+"@stages[0]")

	tight := wfPol(3, 2, 100)
	wfExpectInvalid(t, wfRun(wfSingle(withRetry("s1", 1, 0, 101)), makeTestPortfolio(), tight),
		cognition.CodeWorkflowTimeoutBound+"@stages[0]")
	wfExpectInvalid(t, wfRun(wfSingle(withRetry("s1", 1, 2, 100), withRetry("s2", 2, 1, 100)), makeTestPortfolio(), tight),
		cognition.CodeWorkflowRetryBound+"@stages")
	wfExpectInvalid(t, wfRun(wfSingle(withRetry("s1", 1, 3, 100)), makeTestPortfolio(), tight),
		cognition.CodeWorkflowRetryBound+"@stages", cognition.CodeWorkflowRetryBound+"@stages[0]")
	four := wfStages(4)
	for i := range four {
		four[i].TimeoutSeconds = 100
	}
	wfExpectInvalid(t, wfRun(wfSingle(four...), makeTestPortfolio(), tight), cognition.CodeWorkflowUnboundedStages+"@stages")
	wfExpectValid(t, wfRun(wfSingle(withRetry("s1", 1, 1, 100), withRetry("s2", 2, 1, 100)), makeTestPortfolio(), tight))
}

func wfSha(v any) string {
	b, err := protocol.CanonicalJSON(v)
	if err != nil {
		panic(err)
	}
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

// Review gap 4: digest provenance and independence.
func TestWorkflowValidator_ACC13_DigestProvenance(t *testing.T) {
	plan, port := wfSingle(wfCog("s1", 1, "scout", "pool-local")), makeTestPortfolio()
	pol := wfPol(5, 5, 700)
	r := wfRun(plan, port, pol)
	wfExpectValid(t, r)
	if r.PlanDigest != wfSha(plan) || r.PortfolioDigest != wfSha(port) || r.PolicyDigest != wfSha(*pol) {
		t.Fatalf("digests do not match independently computed canonical hashes: %+v", r)
	}
	if wfRun(plan, port, nil).PolicyDigest != wfSha(cognition.DefaultWorkflowPolicy()) {
		t.Fatal("nil policy digest must be the digest of the default policy")
	}
	// Change only the plan.
	plan2 := wfSingle(wfCog("s1", 1, "scout", "pool-local"))
	plan2.PlanID = "plan-02"
	r2 := wfRun(plan2, port, pol)
	if r2.PlanDigest == r.PlanDigest || r2.PortfolioDigest != r.PortfolioDigest || r2.PolicyDigest != r.PolicyDigest {
		t.Fatal("changing only the plan must change only PlanDigest")
	}
	// Change only the portfolio.
	port2 := makeTestPortfolio()
	port2.Revision = 2
	r3 := wfRun(plan, port2, pol)
	if r3.PortfolioDigest == r.PortfolioDigest || r3.PlanDigest != r.PlanDigest || r3.PolicyDigest != r.PolicyDigest {
		t.Fatal("changing only the portfolio must change only PortfolioDigest")
	}
	// Change only the policy.
	r4 := wfRun(plan, port, wfPol(5, 5, 701))
	if r4.PolicyDigest == r.PolicyDigest || r4.PlanDigest != r.PlanDigest || r4.PortfolioDigest != r.PortfolioDigest {
		t.Fatal("changing only the policy must change only PolicyDigest")
	}
}

// Review gap 5: every bound applies to deterministic stages too.
func TestWorkflowValidator_ACC06_BoundsApplyToDeterministicStages(t *testing.T) {
	topo := protocol.TopologyDeterministicOnly
	d := func(id string, order int) protocol.WorkflowStage { return wfDet(id, order, "pool-local") }
	t.Run("per-stage retry", func(t *testing.T) {
		s := d("d1", 1)
		s.RetryLimit = 7
		wfExpectInvalid(t, wfRunDefault(wfPlan(topo, s)), cognition.CodeWorkflowRetryBound+"@stages", cognition.CodeWorkflowRetryBound+"@stages[0]")
	})
	t.Run("timeout", func(t *testing.T) {
		s := d("d1", 1)
		s.TimeoutSeconds = 3601
		wfExpectInvalid(t, wfRunDefault(wfPlan(topo, s)), cognition.CodeWorkflowTimeoutBound+"@stages[0]")
	})
	t.Run("aggregate only", func(t *testing.T) {
		a, b := d("d1", 1), d("d2", 2)
		a.RetryLimit, b.RetryLimit = 4, 3
		wfExpectInvalid(t, wfRunDefault(wfPlan(topo, a, b)), cognition.CodeWorkflowRetryBound+"@stages")
	})
	t.Run("escalation", func(t *testing.T) {
		a := d("d1", 1)
		a.EscalationTarget = ptr("ghost")
		wfExpectInvalid(t, wfRunDefault(wfPlan(topo, a, d("d2", 2))), cognition.CodeWorkflowEscalationTarget+"@stages[0]")
	})
}

// Review gap 6: stop-rule precedence R0 > R0b > R1 > R1b.
func TestWorkflowValidator_ACC04_StopPrecedence(t *testing.T) {
	badPlan := func() *protocol.WorkflowPlan {
		return wfSingle(wfCog("dup", 1, "scout", "pool-local"), wfCog("dup", 2, "scout", "pool-local"))
	}
	badPort := func() *protocol.CognitionPortfolio {
		p := makeTestPortfolio()
		p.RoleBindings[0].EndpointID = ""
		return p
	}
	good := wfSingle(wfCog("s1", 1, "scout", "pool-local"))
	badPol := wfPol(0, 0, 0)
	cases := []struct {
		name string
		r    cognition.WorkflowValidationResult
		want string
	}{
		{"invalid policy beats invalid plan", wfRun(badPlan(), makeTestPortfolio(), badPol), cognition.CodeWorkflowPolicyInvalid + "@policy"},
		{"invalid policy beats invalid portfolio", wfRun(good, badPort(), badPol), cognition.CodeWorkflowPolicyInvalid + "@policy"},
		{"invalid plan beats invalid portfolio", wfRun(badPlan(), badPort(), nil), cognition.CodeWorkflowPlanInvalid + "@plan"},
		{"nil portfolio beats invalid policy", wfRun(good, nil, badPol), cognition.CodeWorkflowInputMissing + "@portfolio"},
		{"nil plan beats invalid policy", wfRun(nil, makeTestPortfolio(), badPol), cognition.CodeWorkflowInputMissing + "@plan"},
		{"nil portfolio with invalid plan", wfRun(badPlan(), nil, nil), cognition.CodeWorkflowInputMissing + "@portfolio"},
		{"nil plan with invalid portfolio", wfRun(nil, badPort(), nil), cognition.CodeWorkflowInputMissing + "@plan"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wfExpectInvalid(t, c.r, c.want)
			wfNoDigests(t, c.r)
		})
	}
}

// Review gap 7: truncation never cuts a multi-byte rune.
func TestWorkflowValidator_ACC04_TruncationMultibyte(t *testing.T) {
	for pad := 0; pad < 3; pad++ {
		id := strings.Repeat("a", pad) + strings.Repeat("€", 200) // 3-byte runes
		plan := wfSingle(wfCog(id, 1, "scout", "pool-local"), wfCog(id, 2, "scout", "pool-local"))
		r := wfRunDefault(plan)
		wfExpectInvalid(t, r, cognition.CodeWorkflowPlanInvalid+"@plan")
		m := r.Diagnostics[0].Message
		if len(m) > 256 || len(m) < 254 || !utf8.ValidString(m) {
			t.Fatalf("pad %d: bad truncation len=%d valid=%v", pad, len(m), utf8.ValidString(m))
		}
	}
	// Portfolio message too.
	for pad := 0; pad < 3; pad++ {
		p := makeTestPortfolio()
		p.RoleBindings[0].EndpointID = ""
		p.RoleBindings[0].Role = strings.Repeat("a", pad) + strings.Repeat("€", 200)
		r := wfRun(wfSingle(wfCog("s1", 1, "scout", "pool-local")), p, nil)
		wfExpectInvalid(t, r, cognition.CodeWorkflowPortfolioInvalid+"@portfolio")
		m := r.Diagnostics[0].Message
		if len(m) > 256 || !utf8.ValidString(m) {
			t.Fatal(fmt.Sprintf("portfolio pad %d: bad truncation len=%d", pad, len(m)))
		}
	}
}

// Review gap 8: Observed shows a non-nil context profile.
func TestWorkflowValidator_ACC08_ObservedShowsProfile(t *testing.T) {
	s := wfCog("s1", 1, "scout", "pool-local")
	s.ContextProfileID = ptr("prof-bogus")
	r := wfRunDefault(wfSingle(s))
	wfExpectInvalid(t, r, cognition.CodeWorkflowBindingMismatch+"@stages[0]")
	if r.Diagnostics[0].Observed != "-/-/prof-bogus/pool-local" {
		t.Fatalf("observed = %q", r.Diagnostics[0].Observed)
	}
}
