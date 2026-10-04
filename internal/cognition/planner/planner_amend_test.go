package planner

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/protocol"
)

// ACC-20 (REQ-06 amendment A1): rule 5 is evaluated before any element is
// decoded, so memory is bounded by the content limit.
func TestPortfolioPlanner_ACC20_Rule5BeforeElementDecode(t *testing.T) {
	content := `{"alternatives":[` + strings.Repeat("{},", 80000) + `{}]}`
	if len(content) > maxContentBytes {
		t.Fatalf("fixture too large: %d", len(content))
	}
	req := baseRequest(&scriptedInvoker{result: okResult(content)})
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	res := mustPlan(t, req)
	runtime.ReadMemStats(&after)
	if res.Outcome != OutcomeMalformedOutput || !strings.HasPrefix(res.Detail, "rule 5:") {
		t.Fatalf("outcome %q detail %q", res.Outcome, res.Detail)
	}
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 32<<20 {
		t.Fatalf("allocated %d bytes decoding a rule 5 failure; want well under 32 MiB", grown)
	}

	// An unknown field inside an element with a valid count is rule 3 with the index.
	bad := alt(protocol.IntentBalanced)
	bad["surprise"] = 1
	res = mustPlan(t, baseRequest(&scriptedInvoker{result: okResult(output(alt(protocol.IntentMinimumSpend), bad))}))
	if res.Outcome != OutcomeMalformedOutput || !strings.HasPrefix(res.Detail, "rule 3: alternatives[1]:") {
		t.Fatalf("outcome %q detail %q", res.Outcome, res.Detail)
	}
	// A wrongly typed element is also rule 3 with its index.
	res = mustPlan(t, baseRequest(&scriptedInvoker{result: okResult(`{"alternatives":[1]}`)}))
	if !strings.HasPrefix(res.Detail, "rule 3: alternatives[0]:") {
		t.Fatalf("detail %q", res.Detail)
	}
	// Rule 5 precedes element errors: too many elements, each invalid, is rule 5.
	res = mustPlan(t, baseRequest(&scriptedInvoker{result: okResult(`{"alternatives":[1,1,1,1,1]}`)}))
	if !strings.HasPrefix(res.Detail, "rule 5:") {
		t.Fatalf("detail %q", res.Detail)
	}
}

// ACC-21 (REQ-08 amendment A3): blank rationale / tradeoff is rejected by the planner.
func TestPortfolioPlanner_ACC21_BlankExplanationRejected(t *testing.T) {
	blankRationale := alt(protocol.IntentMinimumSpend)
	blankRationale["rationale"] = " \t\n"
	blankTradeoff := alt(protocol.IntentPrivacyFirst)
	blankTradeoff["tradeoffs"] = []string{"fine", "   "}
	res := mustPlan(t, baseRequest(&scriptedInvoker{result: okResult(output(blankRationale, alt(protocol.IntentBalanced), blankTradeoff))}))
	if res.Outcome != OutcomeRecommended || len(res.Accepted) != 1 || res.Accepted[0].Intent != protocol.IntentBalanced {
		t.Fatalf("%+v", res)
	}
	if len(res.Rejected) != 2 {
		t.Fatalf("%+v", res.Rejected)
	}
	if res.Rejected[0].Intent != protocol.IntentMinimumSpend || res.Rejected[0].Reason != "record_invalid: rationale is blank" {
		t.Fatalf("%+v", res.Rejected[0])
	}
	if res.Rejected[1].Intent != protocol.IntentPrivacyFirst || res.Rejected[1].Reason != "record_invalid: tradeoffs[1] is blank" {
		t.Fatalf("%+v", res.Rejected[1])
	}
	for _, r := range res.Rejected {
		if r.Diagnostics != nil {
			t.Fatalf("record failures carry no diagnostics: %+v", r)
		}
	}
}

// ACC-22 (REQ-07/08 amendment A2): record_invalid reasons are bounded.
func TestPortfolioPlanner_ACC22_ReasonBounded(t *testing.T) {
	for name, fill := range map[string]string{
		"ascii": strings.Repeat("x", 200000),
		"multi": strings.Repeat("é", 100000),
		"wide":  "a" + strings.Repeat("世", 66000),
	} {
		t.Run(name, func(t *testing.T) {
			a := alt(protocol.IntentBalanced)
			a["confidence"] = fill
			res := mustPlan(t, baseRequest(&scriptedInvoker{result: okResult(output(a))}))
			if res.Outcome != OutcomeAllRejected || len(res.Rejected) != 1 {
				t.Fatalf("%+v", res)
			}
			const prefix = "record_invalid: "
			reason := res.Rejected[0].Reason
			if !strings.HasPrefix(reason, prefix) || len(reason) > len(prefix)+512 || !utf8.ValidString(reason) {
				t.Fatalf("reason len %d valid %v", len(reason), utf8.ValidString(reason))
			}
			if len(reason) < len(prefix)+509 {
				t.Fatalf("reason unexpectedly short (%d); the fixture must exceed the bound", len(reason))
			}
		})
	}
}

// T3 (D-8): Go stamps empty nested schema versions and never overwrites a wrong one.
func TestPortfolioPlanner_ACC10_NestedSchemaVersionStamping(t *testing.T) {
	mutate := func(f func(portfolio map[string]any)) string {
		a := alt(protocol.IntentBalanced)
		f(a["portfolio"].(map[string]any))
		return output(a)
	}
	each := func(portfolio map[string]any, key string, f func(m map[string]any)) {
		for _, e := range portfolio[key].([]any) {
			f(e.(map[string]any))
		}
	}
	cases := []struct {
		name string
		body string
		ok   bool
	}{
		{"channels omit version", mutate(func(p map[string]any) { each(p, "channels", func(m map[string]any) { delete(m, "schema_version") }) }), true},
		{"pools omit version", mutate(func(p map[string]any) {
			each(p, "budget_pools", func(m map[string]any) { delete(m, "schema_version") })
		}), true},
		{"channels empty version", mutate(func(p map[string]any) { each(p, "channels", func(m map[string]any) { m["schema_version"] = "" }) }), true},
		{"pools empty version", mutate(func(p map[string]any) { each(p, "budget_pools", func(m map[string]any) { m["schema_version"] = "" }) }), true},
		{"wrong channel version", mutate(func(p map[string]any) { p["channels"].([]any)[0].(map[string]any)["schema_version"] = "9.9" }), false},
		{"wrong pool version", mutate(func(p map[string]any) { p["budget_pools"].([]any)[0].(map[string]any)["schema_version"] = "9.9" }), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := mustPlan(t, baseRequest(&scriptedInvoker{result: okResult(tc.body)}))
			if tc.ok {
				if res.Outcome != OutcomeRecommended || len(res.Accepted) != 1 {
					t.Fatalf("%+v", res)
				}
				p := res.Accepted[0].RecommendedPortfolio
				for _, c := range p.Channels {
					if c.SchemaVersion != protocol.SchemaVersion1 {
						t.Fatalf("channel not stamped: %q", c.SchemaVersion)
					}
				}
				for _, b := range p.BudgetPools {
					if b.SchemaVersion != protocol.SchemaVersion1 {
						t.Fatalf("pool not stamped: %q", b.SchemaVersion)
					}
				}
				return
			}
			if res.Outcome != OutcomeAllRejected || len(res.Rejected) != 1 ||
				(!strings.HasPrefix(res.Rejected[0].Reason, "record_invalid: ") && res.Rejected[0].Reason != "validation_failed") {
				t.Fatalf("%+v", res)
			}
		})
	}
}

// T6: SynthesizedAt is RFC3339 (seconds), not RFC3339Nano.
func TestPortfolioPlanner_ACC01_SynthesizedAtIsRFC3339(t *testing.T) {
	req := baseRequest(&scriptedInvoker{result: okResult(output(alt(protocol.IntentBalanced)))})
	req.Clock = clock.NewFake(time.Date(2026, 10, 3, 9, 0, 0, 123456000, time.UTC), 0)
	res := mustPlan(t, req)
	if len(res.Accepted) != 1 {
		t.Fatalf("%+v", res)
	}
	rec := res.Accepted[0]
	if rec.SynthesizedAt != "2026-10-03T09:00:00Z" || rec.RecommendedPortfolio.CreatedAt != "2026-10-03T09:00:00Z" {
		t.Fatalf("synthesized_at %q created_at %q", rec.SynthesizedAt, rec.RecommendedPortfolio.CreatedAt)
	}
	// A non-UTC clock reading is normalized to UTC.
	loc := time.FixedZone("x", 3*3600)
	req.Clock = clock.NewFake(time.Date(2026, 10, 3, 12, 0, 0, 0, loc), 0)
	req.Invoker = &scriptedInvoker{result: okResult(output(alt(protocol.IntentBalanced)))}
	res = mustPlan(t, req)
	if res.Accepted[0].SynthesizedAt != "2026-10-03T09:00:00Z" {
		t.Fatalf("not UTC: %q", res.Accepted[0].SynthesizedAt)
	}
}

// T7: fence label matching is case-sensitive.
func TestPortfolioPlanner_ACC07_FenceLabelCaseSensitive(t *testing.T) {
	res := mustPlan(t, baseRequest(&scriptedInvoker{result: okResult("```JSON\n" + allAlts(allIntents...) + "\n```")}))
	if res.Outcome != OutcomeMalformedOutput || !strings.HasPrefix(res.Detail, "rule 2:") {
		t.Fatalf("%q %q", res.Outcome, res.Detail)
	}
}

// T5: rule 3 detail copies at most 160 bytes of decoder text.
func TestPortfolioPlanner_ACC06_Rule3DetailBounded(t *testing.T) {
	long := strings.Repeat("k", 1000)
	cases := map[string]string{
		"envelope": `{"` + long + `":1}`,
		"element":  `{"alternatives":[{"` + long + `":1}]}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			res := mustPlan(t, baseRequest(&scriptedInvoker{result: okResult(content)}))
			prefix := "rule 3: "
			if name == "element" {
				prefix = "rule 3: alternatives[0]: "
			}
			if !strings.HasPrefix(res.Detail, prefix) || len(res.Detail) != len(prefix)+160 {
				t.Fatalf("detail length %d: %q", len(res.Detail), res.Detail)
			}
		})
	}
	// The result JSON-encodes cleanly (no stray bytes).
	if _, err := json.Marshal(Result{Detail: "rule 3: x"}); err != nil {
		t.Fatal(err)
	}
}
