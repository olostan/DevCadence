package planner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/protocol"
)

var allIntents = []protocol.RecommendationIntent{
	protocol.IntentMinimumSpend,
	protocol.IntentBalanced,
	protocol.IntentMaximumQualityWithinPolicy,
	protocol.IntentPrivacyFirst,
}

var t0 = time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)

type scriptedInvoker struct {
	calls    int
	last     Invocation
	result   InvocationResult
	err      error
	onInvoke func()
}

func (s *scriptedInvoker) Invoke(_ context.Context, inv Invocation) (InvocationResult, error) {
	s.calls++
	s.last = inv
	if s.onInvoke != nil {
		s.onInvoke()
	}
	return s.result, s.err
}

func okResult(content string) InvocationResult {
	return InvocationResult{Content: content, EndpointID: "ep-planner", DriverID: "drv-planner", ModelID: "model-x"}
}

func baseRequest(inv Invoker) Request {
	return Request{
		Inventory:       makeTestInventory(),
		MachineProfile:  makeTestMachineProfile(),
		ContextProfiles: makeTestContextProfiles(),
		Invoker:         inv,
		Clock:           clock.NewFake(t0, time.Second),
	}
}

func alt(intent protocol.RecommendationIntent) map[string]any {
	p, _ := json.Marshal(makeTestPortfolio())
	var portfolio map[string]any
	_ = json.Unmarshal(p, &portfolio)
	return map[string]any{
		"intent":     string(intent),
		"portfolio":  portfolio,
		"rationale":  "because " + string(intent),
		"tradeoffs":  []string{"gives something up"},
		"confidence": "medium",
	}
}

func output(alts ...map[string]any) string {
	b, _ := json.Marshal(map[string]any{"alternatives": alts})
	return string(b)
}

func allAlts(order ...protocol.RecommendationIntent) string {
	alts := make([]map[string]any, 0, len(order))
	for _, i := range order {
		alts = append(alts, alt(i))
	}
	return output(alts...)
}

func mustPlan(t *testing.T, req Request) *Result {
	t.Helper()
	res, err := Plan(context.Background(), req)
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}
	return res
}

// ghostEndpoint rewrites the first channel/binding endpoint so the portfolio is
// internally consistent but names an endpoint absent from the inventory.
func ghostAlt(intent protocol.RecommendationIntent) map[string]any {
	a := alt(intent)
	b, _ := json.Marshal(a)
	s := strings.ReplaceAll(string(b), "ep-local-01", "ep-ghost")
	var out map[string]any
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

func TestPortfolioPlanner_ACC01_RecommendsInCanonicalOrder(t *testing.T) {
	inv := &scriptedInvoker{result: okResult(allAlts(
		protocol.IntentPrivacyFirst, protocol.IntentBalanced, protocol.IntentMaximumQualityWithinPolicy, protocol.IntentMinimumSpend))}
	req := baseRequest(inv)
	clk := req.Clock.(*clock.Fake)
	before := clk.Peek()
	res := mustPlan(t, req)
	after := clk.Peek()
	if after.Sub(before) != time.Second {
		t.Fatalf("clock read %v times, want exactly once", after.Sub(before)/time.Second)
	}
	if res.Outcome != OutcomeRecommended {
		t.Fatalf("outcome %q detail %q rejected %+v", res.Outcome, res.Detail, res.Rejected)
	}
	if len(res.Accepted) != 4 || res.Rejected != nil {
		t.Fatalf("accepted %d rejected %v", len(res.Accepted), res.Rejected)
	}
	invDigest := cognition.InventoryDigest(req.Inventory)
	sum := sha256.Sum256([]byte(invDigest + "|" + res.PromptDigest + "|" + before.UTC().Format(time.RFC3339)))
	wantSet := "set_" + hex.EncodeToString(sum[:])[:16]
	if res.SetID != wantSet || res.InventoryDigest != invDigest {
		t.Fatalf("set %q want %q; inventory digest %q", res.SetID, wantSet, res.InventoryDigest)
	}
	for i, rec := range res.Accepted {
		intent := allIntents[i]
		if rec.Intent != intent {
			t.Fatalf("position %d intent %q want %q", i, rec.Intent, intent)
		}
		if err := rec.Validate(); err != nil {
			t.Fatalf("record invalid: %v", err)
		}
		if rec.SetID != wantSet || rec.RecommendationID != wantSet+"_"+string(intent) ||
			rec.RecommendedPortfolio.PortfolioID != wantSet+"-"+string(intent) ||
			rec.RecommendedPortfolio.Revision != 1 ||
			rec.RecommendedPortfolio.CreatedAt != before.UTC().Format(time.RFC3339) ||
			rec.SynthesizedAt != rec.RecommendedPortfolio.CreatedAt ||
			rec.InventoryDigest != invDigest {
			t.Fatalf("Go-assigned fields wrong: %+v", rec)
		}
		if rec.Planner == nil || rec.Planner.EndpointID != "ep-planner" || rec.Planner.DriverID != "drv-planner" ||
			rec.Planner.ModelID != "model-x" || rec.Planner.InvocationDigest != res.PromptDigest {
			t.Fatalf("provenance wrong: %+v", rec.Planner)
		}
		if rec.ExplanatoryDiagnostics == nil || rec.CapabilityProvenance == nil {
			t.Fatalf("diagnostic slices must be non-nil")
		}
	}
	if inv.calls != 1 || inv.last.PromptDigest != res.PromptDigest {
		t.Fatalf("invoker calls %d", inv.calls)
	}
}

func TestPortfolioPlanner_ACC02_InvalidArguments(t *testing.T) {
	cases := map[string]func(*Request){
		"nil inventory": func(r *Request) { r.Inventory = nil },
		"nil clock":     func(r *Request) { r.Clock = nil },
		"duplicate": func(r *Request) {
			r.Intents = []protocol.RecommendationIntent{protocol.IntentBalanced, protocol.IntentBalanced}
		},
		"unknown": func(r *Request) { r.Intents = []protocol.RecommendationIntent{"bogus"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			inv := &scriptedInvoker{result: okResult(allAlts(allIntents...))}
			req := baseRequest(inv)
			mutate(&req)
			res, err := Plan(context.Background(), req)
			if err == nil || res != nil {
				t.Fatalf("want error, got %+v / %v", res, err)
			}
			if inv.calls != 0 {
				t.Fatalf("invoker called")
			}
		})
	}
}

func TestPortfolioPlanner_ACC03_ContextCancellation(t *testing.T) {
	inv := &scriptedInvoker{result: okResult(allAlts(allIntents...))}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Plan(ctx, baseRequest(inv)); !errors.Is(err, context.Canceled) || inv.calls != 0 {
		t.Fatalf("pre-cancel: err %v calls %d", err, inv.calls)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	inv2 := &scriptedInvoker{result: okResult(allAlts(allIntents...)), onInvoke: cancel2}
	if _, err := Plan(ctx2, baseRequest(inv2)); !errors.Is(err, context.Canceled) || inv2.calls != 1 {
		t.Fatalf("mid-cancel: err %v calls %d", err, inv2.calls)
	}
}

func TestPortfolioPlanner_ACC04_NoPlanner(t *testing.T) {
	req := baseRequest(nil)
	res := mustPlan(t, req)
	if res.Outcome != OutcomeNoPlanner || res.Detail != "no planning endpoint available; deterministic-only" ||
		res.InventoryDigest != cognition.InventoryDigest(req.Inventory) || res.PromptDigest != "" || res.SetID != "" {
		t.Fatalf("%+v", res)
	}
}

func TestPortfolioPlanner_ACC05_InvocationFailedTruncates(t *testing.T) {
	for name, msg := range map[string]string{
		"ascii": strings.Repeat("a", 1000),
		"multi": strings.Repeat("é", 500),
		"odd":   "x" + strings.Repeat("é", 500),
	} {
		t.Run(name, func(t *testing.T) {
			inv := &scriptedInvoker{err: errors.New(msg)}
			res := mustPlan(t, baseRequest(inv))
			if res.Outcome != OutcomeInvocationFailed || len(res.Detail) > 256 || !utf8.ValidString(res.Detail) || inv.calls != 1 {
				t.Fatalf("outcome %q len %d valid %v calls %d", res.Outcome, len(res.Detail), utf8.ValidString(res.Detail), inv.calls)
			}
			if res.PromptDigest == "" || res.Accepted != nil || res.Rejected != nil {
				t.Fatalf("%+v", res)
			}
			wantLen := map[string]int{"ascii": 256, "multi": 256, "odd": 255}[name]
			if len(res.Detail) != wantLen {
				t.Fatalf("detail is %d bytes, want exactly %d", len(res.Detail), wantLen)
			}
			if res.InventoryDigest == "" || res.SetID != "" {
				t.Fatalf("invocation_failed must set InventoryDigest and leave SetID empty: %+v", res)
			}
		})
	}
}

func TestPortfolioPlanner_ACC06_MalformedOutputRules(t *testing.T) {
	valid := alt(protocol.IntentBalanced)
	withExtra := map[string]any{"alternatives": []any{valid}, "extra": 1}
	extraJSON, _ := json.Marshal(withExtra)
	five := output(alt(protocol.IntentMinimumSpend), alt(protocol.IntentBalanced), alt(protocol.IntentMaximumQualityWithinPolicy),
		alt(protocol.IntentPrivacyFirst), alt(protocol.IntentBalanced))
	cases := []struct {
		name    string
		content string
		intents []protocol.RecommendationIntent
		rule    string
	}{
		{"not json", "not json", nil, "rule 3:"},
		{"empty", "", nil, "rule 3:"},
		{"extra field", string(extraJSON), nil, "rule 3:"},
		{"array", "[]", nil, "rule 3:"},
		{"trailing text", output(valid) + " trailing", nil, "rule 4:"},
		{"trailing value", output(valid) + output(valid), nil, "rule 4:"},
		{"oversize", strings.Repeat(" ", 262145), nil, "rule 1:"},
		{"zero alts", `{"alternatives":[]}`, nil, "rule 5:"},
		{"missing alts", `{}`, nil, "rule 5:"},
		{"five alts", five, nil, "rule 5:"},
		{"duplicate intents", output(valid, alt(protocol.IntentBalanced)), nil, "rule 6:"},
		{"unrequested", output(alt(protocol.IntentMinimumSpend)), []protocol.RecommendationIntent{protocol.IntentBalanced}, "rule 6:"},
		{"invalid intent", output(map[string]any{"intent": "nope"}), nil, "rule 6:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv := &scriptedInvoker{result: okResult(tc.content)}
			req := baseRequest(inv)
			req.Intents = tc.intents
			res := mustPlan(t, req)
			if res.Outcome != OutcomeMalformedOutput || !strings.HasPrefix(res.Detail, tc.rule) {
				t.Fatalf("outcome %q detail %q want %q", res.Outcome, res.Detail, tc.rule)
			}
			if res.Accepted != nil || res.Rejected != nil || res.SetID != "" || res.PromptDigest == "" || res.InventoryDigest == "" {
				t.Fatalf("%+v", res)
			}
		})
	}
	// Exactly 262144 bytes passes rule 1 (and fails later, not at rule 1).
	inv := &scriptedInvoker{result: okResult(strings.Repeat(" ", 262144))}
	res := mustPlan(t, baseRequest(inv))
	if !strings.HasPrefix(res.Detail, "rule 3:") {
		t.Fatalf("boundary size detail %q", res.Detail)
	}
}

func TestPortfolioPlanner_ACC07_Fences(t *testing.T) {
	body := allAlts(allIntents...)
	cases := []struct {
		name    string
		content string
		ok      bool
	}{
		{"json label", "```json\n" + body + "\n```", true},
		{"bare", "```\n" + body + "\n```", true},
		{"crlf", "```json\r\n" + body + "\r\n```", true},
		{"surrounding whitespace", "\n  ```json\n" + body + "\n```  \n", true},
		{"unclosed", "```json\n" + body, false},
		{"text before", "Here you go:\n```json\n" + body + "\n```", false},
		{"one line", "```json" + body + "```", false},
		{"wrong label", "```yaml\n" + body + "\n```", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := mustPlan(t, baseRequest(&scriptedInvoker{result: okResult(tc.content)}))
			if tc.ok && res.Outcome != OutcomeRecommended {
				t.Fatalf("want recommended, got %q %q", res.Outcome, res.Detail)
			}
			if !tc.ok && res.Outcome != OutcomeMalformedOutput {
				t.Fatalf("want malformed, got %q", res.Outcome)
			}
		})
	}
	res := mustPlan(t, baseRequest(&scriptedInvoker{result: okResult("```json" + body + "```")}))
	if !strings.HasPrefix(res.Detail, "rule 2:") {
		t.Fatalf("one-line fence detail %q", res.Detail)
	}
	res = mustPlan(t, baseRequest(&scriptedInvoker{result: okResult("```json\n" + body)}))
	if !strings.HasPrefix(res.Detail, "rule 2:") {
		t.Fatalf("unclosed fence detail %q", res.Detail)
	}
	res = mustPlan(t, baseRequest(&scriptedInvoker{result: okResult("prose\n```json\n" + body + "\n```")}))
	if !strings.HasPrefix(res.Detail, "rule 3:") {
		t.Fatalf("text-before detail %q", res.Detail)
	}
}

func TestPortfolioPlanner_ACC08_PartialAcceptance(t *testing.T) {
	inv := &scriptedInvoker{result: okResult(output(alt(protocol.IntentBalanced), ghostAlt(protocol.IntentMinimumSpend)))}
	res := mustPlan(t, baseRequest(inv))
	if res.Outcome != OutcomeRecommended || len(res.Accepted) != 1 || res.Accepted[0].Intent != protocol.IntentBalanced {
		t.Fatalf("%+v", res)
	}
	if len(res.Rejected) != 1 || res.Rejected[0].Intent != protocol.IntentMinimumSpend || res.Rejected[0].Reason != "validation_failed" {
		t.Fatalf("%+v", res.Rejected)
	}
	found := false
	for _, d := range res.Rejected[0].Diagnostics {
		if d.Code == cognition.CodeEndpointNotFound {
			found = true
		}
	}
	if !found {
		t.Fatalf("no ENDPOINT_NOT_FOUND in %+v", res.Rejected[0].Diagnostics)
	}
}

func TestPortfolioPlanner_ACC09_AllRejected(t *testing.T) {
	req := baseRequest(&scriptedInvoker{result: okResult(allAlts(allIntents...))})
	req.Policy = &cognition.ValidationPolicy{MaxSourceExposure: protocol.ExposureLocalOnly, MaxCostClass: protocol.CostLocalCompute}
	res := mustPlan(t, req)
	if res.Outcome != OutcomeAllRejected || res.Accepted != nil || len(res.Rejected) != 4 {
		t.Fatalf("%q accepted %d rejected %d", res.Outcome, len(res.Accepted), len(res.Rejected))
	}
	for i, r := range res.Rejected {
		if r.Intent != allIntents[i] || r.Reason != "validation_failed" || len(r.Diagnostics) == 0 {
			t.Fatalf("%+v", r)
		}
	}
}

func TestPortfolioPlanner_ACC10_GoOwnedFields(t *testing.T) {
	a := alt(protocol.IntentBalanced)
	portfolio := a["portfolio"].(map[string]any)
	portfolio["portfolio_id"] = "evil"
	portfolio["revision"] = 99
	portfolio["created_at"] = "1999-01-01T00:00:00Z"
	portfolio["schema_version"] = "9.9"
	req := baseRequest(&scriptedInvoker{result: okResult(output(a))})
	res := mustPlan(t, req)
	if res.Outcome != OutcomeRecommended || len(res.Accepted) != 1 {
		t.Fatalf("%+v", res)
	}
	p := res.Accepted[0].RecommendedPortfolio
	if p.PortfolioID != res.SetID+"-balanced" || p.Revision != 1 || p.CreatedAt == "1999-01-01T00:00:00Z" ||
		p.SchemaVersion != protocol.SchemaVersion1 {
		t.Fatalf("model values leaked: %+v", p)
	}

	var root map[string]any
	_ = json.Unmarshal([]byte(allAlts(protocol.IntentBalanced)), &root)
	root["planner"] = map[string]any{"endpoint_id": "forged"}
	b, _ := json.Marshal(root)
	res = mustPlan(t, baseRequest(&scriptedInvoker{result: okResult(string(b))}))
	if res.Outcome != OutcomeMalformedOutput || !strings.HasPrefix(res.Detail, "rule 3:") {
		t.Fatalf("top-level unknown: %q %q", res.Outcome, res.Detail)
	}

	c := alt(protocol.IntentBalanced)
	c["portfolio"].(map[string]any)["surprise"] = true
	res = mustPlan(t, baseRequest(&scriptedInvoker{result: okResult(output(c))}))
	if res.Outcome != OutcomeMalformedOutput || !strings.HasPrefix(res.Detail, "rule 3:") {
		t.Fatalf("nested unknown: %q %q", res.Outcome, res.Detail)
	}
}

func TestPortfolioPlanner_ACC11_ProvenanceIncomplete(t *testing.T) {
	for name, mutate := range map[string]func(*InvocationResult){
		"empty driver":   func(r *InvocationResult) { r.DriverID = "" },
		"blank endpoint": func(r *InvocationResult) { r.EndpointID = "  \t" },
	} {
		t.Run(name, func(t *testing.T) {
			r := okResult(allAlts(allIntents...))
			mutate(&r)
			res := mustPlan(t, baseRequest(&scriptedInvoker{result: r}))
			if res.Outcome != OutcomeAllRejected || len(res.Rejected) != 4 || res.Accepted != nil {
				t.Fatalf("%+v", res)
			}
			for _, rj := range res.Rejected {
				if rj.Reason != "planner_provenance_incomplete" {
					t.Fatalf("reason %q", rj.Reason)
				}
			}
		})
	}
	r := okResult(allAlts(allIntents...))
	r.ModelID = ""
	if res := mustPlan(t, baseRequest(&scriptedInvoker{result: r})); res.Outcome != OutcomeRecommended {
		t.Fatalf("empty ModelID must be allowed: %q", res.Outcome)
	}
}

func TestPortfolioPlanner_ACC12_MissingExplanation(t *testing.T) {
	noTradeoffs := alt(protocol.IntentMinimumSpend)
	noTradeoffs["tradeoffs"] = []string{}
	noConfidence := alt(protocol.IntentPrivacyFirst)
	delete(noConfidence, "confidence")
	res := mustPlan(t, baseRequest(&scriptedInvoker{result: okResult(output(noTradeoffs, alt(protocol.IntentBalanced), noConfidence))}))
	if res.Outcome != OutcomeRecommended || len(res.Accepted) != 1 || len(res.Rejected) != 2 {
		t.Fatalf("%+v", res)
	}
	for _, rj := range res.Rejected {
		if !strings.HasPrefix(rj.Reason, "record_invalid: ") || rj.Diagnostics != nil {
			t.Fatalf("%+v", rj)
		}
	}
	if res.Rejected[0].Intent != protocol.IntentMinimumSpend || res.Rejected[1].Intent != protocol.IntentPrivacyFirst {
		t.Fatalf("order: %+v", res.Rejected)
	}
}

func TestPortfolioPlanner_ACC13_Determinism(t *testing.T) {
	build := func() Request {
		inv := &scriptedInvoker{result: okResult(allAlts(allIntents...))}
		req := baseRequest(inv)
		req.Clock = clock.NewFake(t0, 0)
		req.BudgetStates = map[string]*protocol.BudgetState{}
		for _, id := range []string{"z-pool", "a-pool", "m-pool", "pool-local", "pool-sub"} {
			req.BudgetStates[id] = &protocol.BudgetState{PoolID: id}
		}
		req.Project = ProjectCharacteristics{Languages: []string{"go", "c", "go"}, RiskTags: nil}
		return req
	}
	r1, r2 := build(), build()
	p1, d1, err1 := BuildPrompt(r1)
	p2, d2, err2 := BuildPrompt(r2)
	if err1 != nil || err2 != nil || p1 != p2 || d1 != d2 {
		t.Fatalf("prompt differs")
	}
	for i := 0; i < 20; i++ {
		p, d, _ := BuildPrompt(build())
		if p != p1 || d != d1 {
			t.Fatalf("map-order dependent prompt")
		}
	}
	a, _ := Plan(context.Background(), r1)
	b, _ := Plan(context.Background(), r2)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("results differ")
	}
}

func TestPortfolioPlanner_ACC14_PromptContent(t *testing.T) {
	req := baseRequest(nil)
	req.Inventory.Credentials = []protocol.CredentialInventoryEntry{{
		Ref: protocol.CredentialRef{SchemaVersion: protocol.SchemaVersion1, RefID: "SENTINEL_CRED_ENTRY", Kind: protocol.CredRefEnvVar, Locator: "SENTINEL_LOCATOR"},
	}}
	req.Inventory.CognitionEndpoints[1].CredentialRef = "SENTINEL_CREDREF"
	req.Inventory.PrincipalHosts = []protocol.PrincipalHostSummary{{HostID: "SENTINEL_HOST", Installed: true}}
	req.BudgetStates = map[string]*protocol.BudgetState{"pool-sub": {PoolID: "pool-sub", ObservedAt: "SENTINEL_BUDGET_VALUE", UnknownFields: []string{"SENTINEL_UF"}}}
	req.ResourceStates = map[string]*protocol.ResourceState{"host-01": {HostID: "host-01", Timestamp: "SENTINEL_RES_VALUE", UnknownMetrics: []string{"SENTINEL_UM"}}}
	req.MachineProfile.ProfileID = "SENTINEL_RAW_PROFILE"
	req.Intents = []protocol.RecommendationIntent{protocol.IntentPrivacyFirst, protocol.IntentBalanced}
	req.Project = ProjectCharacteristics{Languages: []string{"go", "c", "go"}}
	before, _ := protocol.CanonicalJSON(req.Inventory)

	prompt, digest, err := BuildPrompt(req)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := protocol.CanonicalJSON(req.Inventory)
	if !bytes.Equal(before, after) || req.Inventory.CognitionEndpoints[1].CredentialRef != "SENTINEL_CREDREF" {
		t.Fatalf("BuildPrompt mutated the inventory")
	}
	for _, s := range []string{"SENTINEL_CRED_ENTRY", "SENTINEL_LOCATOR", "SENTINEL_CREDREF", "SENTINEL_HOST", "SENTINEL_BUDGET_VALUE",
		"SENTINEL_UF", "SENTINEL_RES_VALUE", "SENTINEL_UM", "SENTINEL_RAW_PROFILE", "host-01"} {
		if strings.Contains(prompt, s) {
			t.Fatalf("prompt leaks %q", s)
		}
	}
	sum := sha256.Sum256([]byte(prompt))
	if digest != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatalf("digest")
	}
	if strings.SplitN(prompt, "\n", 2)[0] != "devcadence-planner-prompt/1" {
		t.Fatalf("first line")
	}
	canon := func(v any) string { b, _ := protocol.CanonicalJSON(v); return string(b) }
	if canon(req.Inventory.Readiness) == "null" || canon(req.Inventory.Policy) == "null" {
		t.Fatalf("fixture must carry non-empty Readiness and Policy")
	}
	effective := cognition.DefaultValidationPolicy()
	want := []string{
		`["balanced","privacy_first"]`,
		"ep-local-01", "ep-cli-01",
		canon(req.Inventory.Hardware), canon(req.Inventory.Readiness), canon(req.Inventory.Policy),
		"READINESS_FIXTURE_REASON", `"max_cost_class":"subscription_included"`,
		"valid context_profile_id values", "budget pool ids that have live state",
		`"profile_id":"prof-local-01"`, `"endpoint_id":"ep-local-01"`, `"channel_id":"chan-local-01"`,
		`"observed_context_control":"exact_stateless"`, `"observed_context_control":"append_only"`,
		`["pool-sub"]`,
		canon(effective),
		`{"languages":["c","go"],"risk_tags":[]}`,
		plannerExampleAlternative,
		"IDs must come from the lists above",
		"Your output is advisory. Deterministic validation decides whether any alternative is usable.",
	}
	for _, w := range want {
		if !strings.Contains(prompt, w) {
			t.Fatalf("prompt missing %q", w)
		}
	}
	// Policy back-fill: an explicit policy without role requirements gets defaults.
	req.Policy = &cognition.ValidationPolicy{MaxSourceExposure: protocol.ExposureFocusedSnippets}
	p2, _, _ := BuildPrompt(req)
	filled := *req.Policy
	filled.RoleRequirements = cognition.DefaultRequirements()
	if !strings.Contains(p2, canon(filled)) || req.Policy.RoleRequirements != nil {
		t.Fatalf("policy back-fill missing or caller policy mutated")
	}
	// BuildPrompt needs neither Clock nor Invoker, but validates Inventory/Intents.
	req.Clock = nil
	if _, _, err := BuildPrompt(req); err != nil {
		t.Fatalf("clockless BuildPrompt: %v", err)
	}
	req.Intents = []protocol.RecommendationIntent{"bogus"}
	if _, _, err := BuildPrompt(req); err == nil {
		t.Fatalf("want intent error")
	}
	req.Intents = nil
	req.Inventory = nil
	if _, _, err := BuildPrompt(req); err == nil {
		t.Fatalf("want inventory error")
	}

	// The embedded example decodes strictly and validates after Go stamping.
	dec := json.NewDecoder(strings.NewReader(plannerExampleAlternative))
	dec.DisallowUnknownFields()
	var a decodedAlternative
	if err := dec.Decode(&a); err != nil {
		t.Fatalf("example does not decode strictly: %v", err)
	}
	rec := buildRecommendation(a, a.Intent, "set_x", "sha256:i", "sha256:p", "2026-10-03T09:00:00Z", okResult(""))
	if err := rec.RecommendedPortfolio.Validate(); err != nil {
		t.Fatalf("example portfolio invalid after stamping: %v", err)
	}
	if err := rec.Validate(); err != nil {
		t.Fatalf("example record invalid: %v", err)
	}
}

func TestPortfolioPlanner_ACC15_UnknownResourceStateFailsClosed(t *testing.T) {
	req := baseRequest(&scriptedInvoker{result: okResult(output(alt(protocol.IntentBalanced)))})
	req.ResourceStates = map[string]*protocol.ResourceState{"host-01": {HostID: "host-01", UnknownMetrics: []string{"available_ram_bytes"}}}
	res := mustPlan(t, req)
	if res.Outcome != OutcomeAllRejected || len(res.Rejected) != 1 || res.Rejected[0].Reason != "validation_failed" {
		t.Fatalf("%+v", res)
	}
	found := false
	for _, d := range res.Rejected[0].Diagnostics {
		if d.Code == cognition.CodeUnknownResourceState {
			found = true
		}
	}
	if !found {
		t.Fatalf("no UNKNOWN_RESOURCE_STATE: %+v", res.Rejected[0].Diagnostics)
	}
}

func TestPortfolioPlanner_ACC16_ExplicitPolicyHonored(t *testing.T) {
	req := baseRequest(&scriptedInvoker{result: okResult(output(alt(protocol.IntentBalanced)))})
	req.ResourceStates = map[string]*protocol.ResourceState{"host-01": {HostID: "host-01", UnknownMetrics: []string{"available_ram_bytes"}}}
	policy := cognition.DefaultValidationPolicy()
	policy.RequireKnownResourceState = false
	policy.RequireKnownBudgetState = false
	req.Policy = &policy
	res := mustPlan(t, req)
	if res.Outcome != OutcomeRecommended || len(res.Accepted) != 1 {
		t.Fatalf("%+v", res)
	}
	if policy.RequireKnownResourceState || policy.RequireKnownBudgetState {
		t.Fatalf("explicit policy was altered")
	}
}

func TestPortfolioPlanner_ACC17_InventoryDigest(t *testing.T) {
	if cognition.InventoryDigest(nil) != "" {
		t.Fatalf("nil digest must be empty")
	}
	inv := makeTestInventory()
	p := makeTestPortfolio()
	vr := cognition.NewPortfolioValidator().Validate(cognition.ValidationInput{Portfolio: p, Inventory: inv})
	if vr.InventoryDigest == "" || vr.InventoryDigest != cognition.InventoryDigest(inv) {
		t.Fatalf("digest %q vs %q", vr.InventoryDigest, cognition.InventoryDigest(inv))
	}
}

func TestPortfolioPlanner_ACC19_InputsNotMutated(t *testing.T) {
	inv := &scriptedInvoker{result: okResult(output(alt(protocol.IntentBalanced), alt(protocol.IntentPrivacyFirst)))}
	req := baseRequest(inv)
	req.Intents = []protocol.RecommendationIntent{protocol.IntentPrivacyFirst, protocol.IntentBalanced}
	req.Project = ProjectCharacteristics{Languages: []string{"go", "c", "go"}, RiskTags: []string{"z", "a"}}
	wantIntents := append([]protocol.RecommendationIntent(nil), req.Intents...)
	wantProject := ProjectCharacteristics{Languages: []string{"go", "c", "go"}, RiskTags: []string{"z", "a"}}
	invokerBefore := req.Invoker
	req.BudgetStates = map[string]*protocol.BudgetState{"pool-sub": {PoolID: "pool-sub"}}
	req.ResourceStates = map[string]*protocol.ResourceState{"host-01": {HostID: "host-01"}}
	policy := cognition.ValidationPolicy{MaxSourceExposure: protocol.ExposureFocusedSnippets}
	req.Policy = &policy
	req.Inventory.CognitionEndpoints[0].CredentialRef = "ref-1"
	snap := func() string {
		var sb strings.Builder
		for _, v := range []any{req.Inventory, req.MachineProfile, req.ContextProfiles, req.BudgetStates, req.ResourceStates, req.Policy} {
			b, err := protocol.CanonicalJSON(v)
			if err != nil {
				t.Fatal(err)
			}
			sb.Write(b)
		}
		return sb.String()
	}
	before := snap()
	_ = mustPlan(t, req)
	if snap() != before {
		t.Fatalf("Plan mutated its inputs")
	}
	if !reflect.DeepEqual(req.Intents, wantIntents) || !reflect.DeepEqual(req.Project, wantProject) {
		t.Fatalf("Plan mutated Intents or Project: %v %+v", req.Intents, req.Project)
	}
	if req.Invoker != invokerBefore {
		t.Fatalf("Plan replaced the Invoker")
	}
}

func TestPortfolioPlanner_IntentCanonicalizationAndSubset(t *testing.T) {
	got, err := canonicalizeIntents([]protocol.RecommendationIntent{protocol.IntentPrivacyFirst, protocol.IntentMinimumSpend})
	if err != nil || !reflect.DeepEqual(got, []protocol.RecommendationIntent{protocol.IntentMinimumSpend, protocol.IntentPrivacyFirst}) {
		t.Fatalf("%v %v", got, err)
	}
	req := baseRequest(&scriptedInvoker{result: okResult(output(alt(protocol.IntentBalanced)))})
	req.Intents = []protocol.RecommendationIntent{protocol.IntentBalanced}
	if res := mustPlan(t, req); res.Outcome != OutcomeRecommended || len(res.Accepted) != 1 {
		t.Fatalf("%+v", res)
	}
}
