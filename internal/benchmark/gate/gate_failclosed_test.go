package gate

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/benchmark"
	"github.com/olostan/DevCadence/internal/benchmark/experiments"
	"github.com/olostan/DevCadence/internal/benchmark/telemetry"
)

const (
	tierLocal    = string(experiments.CapabilityLocalSmall)
	tierFrontier = string(experiments.CapabilityFrontierAPI)
	tierSub      = string(experiments.CapabilitySubscriptionCLI)
)

type grp struct {
	strategy   benchmark.ContextStrategyKind
	tier       string
	catch      float64
	catchAppl  bool
	resource   float64
	undefined  bool
	peakTokens float64
}

func mkGroup(g grp) telemetry.AggregatedTelemetry {
	return telemetry.AggregatedTelemetry{
		Strategy:                  string(g.strategy),
		Capability:                g.tier,
		RunCount:                  10,
		AcceptedCount:             5,
		DefectCatchRate:           g.catch,
		DefectCatchRateApplicable: g.catchAppl,
		AvgPeakResidentTokens:     g.peakTokens,
		ResourcePerAcceptedResult: telemetry.ResourceEfficiency{Value: g.resource, IsUndefined: g.undefined},
	}
}

func s4(tier string, resource float64) grp {
	return grp{strategy: benchmark.StrategyHybrid4Layer, tier: tier, catch: 0.9, catchAppl: true, resource: resource, peakTokens: 500}
}

func s1(tier string, resource float64) grp {
	return grp{strategy: benchmark.StrategyFullHistory, tier: tier, catch: 0.5, catchAppl: true, resource: resource, peakTokens: 1000}
}

func mkReport(groups ...grp) *telemetry.AggregatedReport {
	r := &telemetry.AggregatedReport{GeneratedAt: time.Now().UTC(), TotalSnapshots: 40}
	for _, g := range groups {
		r.Groups = append(r.Groups, mkGroup(g))
	}
	return r
}

func findCrit(t *testing.T, res *GateEvaluationResult, name string) CriterionResult {
	t.Helper()
	for _, c := range res.CriteriaEvaluations {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("criterion %q not found in %+v", name, res.CriteriaEvaluations)
	return CriterionResult{}
}

func evalOK(t *testing.T, r *telemetry.AggregatedReport, f map[string]*experiments.FalsificationResult, c GateCriteria) *GateEvaluationResult {
	t.Helper()
	res, err := EvaluateM4Gate(r, f, c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return res
}

// T1: kills mutant "S1 baseline always tiers[0]" (sorted or insertion order).
func TestMutation_S1BaselineNotAlwaysFirstTier(t *testing.T) {
	// Correct pairing passes in both tiers: 4000/5000=0.8 (local), 90/100=0.9 (frontier).
	// Sorted-first mutant (frontier baseline=100) would fail local (40x).
	pass := mkReport(s4(tierLocal, 4000), s1(tierLocal, 5000), s4(tierFrontier, 90), s1(tierFrontier, 100))
	res := evalOK(t, pass, nil, DefaultM4GateCriteria())
	if res.Decision != DecisionGo {
		t.Fatalf("correct pairwise pairing must pass, got %s: %v", res.Decision, res.Recommendations)
	}
	if got := findCrit(t, res, "resource_efficiency_local_small").Observed; math.Abs(got-0.8) > 1e-9 {
		t.Fatalf("local ratio = %v, want 0.8", got)
	}

	// Correct pairing fails in frontier: 3000/100=30. Insertion-first mutant (local baseline=5000)
	// would pass it (0.6).
	fail := mkReport(s4(tierLocal, 4000), s1(tierLocal, 5000), s4(tierFrontier, 3000), s1(tierFrontier, 100))
	res = evalOK(t, fail, nil, DefaultM4GateCriteria())
	if res.Decision != DecisionRevise {
		t.Fatalf("frontier inefficiency must yield revise, got %s", res.Decision)
	}
	if c := findCrit(t, res, "resource_efficiency_frontier_api"); c.Passed || math.Abs(c.Observed-30) > 1e-9 {
		t.Fatalf("frontier criterion = %+v, want failed ratio 30", c)
	}
	if !findCrit(t, res, "resource_efficiency_local_small").Passed {
		t.Fatalf("local tier must still pass")
	}
}

// T2: aggregate falsification rate with MaxFalsificationRate>0 and RequireZeroFalsifications=false.
func TestAggregateFalsificationRate(t *testing.T) {
	report := mkReport(s4(tierLocal, 100), s1(tierLocal, 1000))
	mk := func(falsified int, total int) map[string]*experiments.FalsificationResult {
		m := map[string]*experiments.FalsificationResult{}
		for i := 0; i < total; i++ {
			key := string(rune('a' + i))
			m[key] = &experiments.FalsificationResult{IsApplicable: true, HypothesisFalsified: i < falsified, Reason: "r"}
		}
		return m
	}
	crit := DefaultM4GateCriteria()
	crit.RequireZeroFalsifications = false
	crit.MaxFalsificationRate = 0.25

	// 1/4 = 0.25 <= 0.25 (equality) -> pass overall.
	res := evalOK(t, report, mk(1, 4), crit)
	if res.Decision != DecisionGo {
		t.Fatalf("rate at threshold must pass, got %s: %v", res.Decision, res.Recommendations)
	}
	if c := findCrit(t, res, "aggregate_falsification_rate"); !c.Passed || c.Observed != 0.25 {
		t.Fatalf("aggregate criterion = %+v", c)
	}

	// 2/4 = 0.5 > 0.25 -> fail.
	res = evalOK(t, report, mk(2, 4), crit)
	if res.Decision != DecisionRevise {
		t.Fatalf("rate above threshold must revise, got %s", res.Decision)
	}
	if c := findCrit(t, res, "aggregate_falsification_rate"); c.Passed || c.Observed != 0.5 {
		t.Fatalf("aggregate criterion = %+v", c)
	}

	// RequireZero=true: a single falsification fails regardless of the tolerated rate.
	crit.RequireZeroFalsifications = true
	res = evalOK(t, report, mk(1, 4), crit)
	if res.Decision != DecisionRevise {
		t.Fatalf("RequireZeroFalsifications must fail on 1 falsification, got %s", res.Decision)
	}
}

// T3: equality boundaries.
func TestEqualityBoundaries(t *testing.T) {
	crit := DefaultM4GateCriteria()

	// catch rate exactly at threshold passes; just below fails.
	atCatch := s4(tierLocal, 100)
	atCatch.catch = crit.MinDefectCatchRate
	res := evalOK(t, mkReport(atCatch, s1(tierLocal, 1000)), nil, crit)
	if c := findCrit(t, res, "defect_catch_rate_local_small"); !c.Passed {
		t.Fatalf("catch rate == threshold must pass: %+v", c)
	}
	below := atCatch
	below.catch = crit.MinDefectCatchRate - 0.001
	res = evalOK(t, mkReport(below, s1(tierLocal, 1000)), nil, crit)
	if res.Decision != DecisionRevise || findCrit(t, res, "defect_catch_rate_local_small").Passed {
		t.Fatalf("catch rate below threshold must fail")
	}

	// resource ratio exactly at threshold (1.0) passes; just above fails.
	res = evalOK(t, mkReport(s4(tierLocal, 1000), s1(tierLocal, 1000)), nil, crit)
	if c := findCrit(t, res, "resource_efficiency_local_small"); !c.Passed || c.Observed != 1.0 {
		t.Fatalf("ratio == threshold must pass: %+v", c)
	}
	res = evalOK(t, mkReport(s4(tierLocal, 1001), s1(tierLocal, 1000)), nil, crit)
	if findCrit(t, res, "resource_efficiency_local_small").Passed {
		t.Fatalf("ratio above threshold must fail")
	}

	// peak resident ratio: 1000/1000 == 1.0 passes; 1001/1000 fails.
	equalPeak := s4(tierLocal, 100)
	equalPeak.peakTokens = 1000
	res = evalOK(t, mkReport(equalPeak, s1(tierLocal, 1000)), nil, crit)
	if c := findCrit(t, res, "peak_resident_context_local_small"); !c.Passed || c.Observed != 1.0 {
		t.Fatalf("peak ratio == threshold must pass: %+v", c)
	}
	equalPeak.peakTokens = 1001
	res = evalOK(t, mkReport(equalPeak, s1(tierLocal, 1000)), nil, crit)
	if res.Decision != DecisionRevise || findCrit(t, res, "peak_resident_context_local_small").Passed {
		t.Fatalf("peak ratio above threshold must fail")
	}

	// threshold disabled (0) -> no peak criterion recorded.
	crit.MaxResidentContextRatioBaseline = 0
	res = evalOK(t, mkReport(equalPeak, s1(tierLocal, 1000)), nil, crit)
	for _, c := range res.CriteriaEvaluations {
		if strings.HasPrefix(c.Name, "peak_resident_context") {
			t.Fatalf("peak criterion must be skipped when disabled")
		}
	}
}

// B1/T5: missing or partial criteria can never yield Go.
func TestFailClosed_MissingGroups(t *testing.T) {
	crit := DefaultM4GateCriteria()
	full := func(tier string) []grp { return []grp{s4(tier, 100), s1(tier, 1000)} }

	cases := []struct {
		name   string
		groups []grp
		failed []string
	}{
		{"no S4 groups at all", []grp{s1(tierLocal, 1000), s1(tierFrontier, 1000)},
			[]string{"defect_catch_rate_local_small", "resource_efficiency_local_small", "defect_catch_rate_frontier_api", "resource_efficiency_frontier_api"}},
		{"S4 missing in one tier", append(full(tierLocal), s1(tierFrontier, 1000)),
			[]string{"defect_catch_rate_frontier_api", "resource_efficiency_frontier_api"}},
		{"S1 baseline missing in one tier", append(full(tierLocal), s4(tierFrontier, 100)),
			[]string{"resource_efficiency_frontier_api"}},
		{"tier with only other strategies", append(full(tierLocal), grp{strategy: benchmark.StrategyCompacted, tier: tierSub, resource: 1, peakTokens: 1}),
			[]string{"defect_catch_rate_subscription_cli", "resource_efficiency_subscription_cli"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := evalOK(t, mkReport(tc.groups...), nil, crit)
			if res.Decision == DecisionGo {
				t.Fatalf("must not yield Go")
			}
			if res.Decision != DecisionRevise {
				t.Fatalf("want revise, got %s", res.Decision)
			}
			for _, name := range tc.failed {
				if c := findCrit(t, res, name); c.Passed {
					t.Fatalf("criterion %s must be a recorded failure", name)
				}
			}
			if _, err := json.Marshal(res); err != nil {
				t.Fatalf("result must be JSON-serializable: %v", err)
			}
		})
	}

	// Empty capability names -> no tiers at all.
	r := mkReport(s4("", 100), s1("", 1000))
	res := evalOK(t, r, nil, crit)
	if res.Decision == DecisionGo {
		t.Fatalf("report without capability tiers must not yield Go")
	}
	if c := findCrit(t, res, "capability_tiers_present"); c.Passed {
		t.Fatalf("capability_tiers_present must fail")
	}
}

func TestFailClosed_DefectCatchNotApplicableAndMalformed(t *testing.T) {
	crit := DefaultM4GateCriteria()
	na := s4(tierLocal, 100)
	na.catchAppl = false
	res := evalOK(t, mkReport(na, s1(tierLocal, 1000)), nil, crit)
	if c := findCrit(t, res, "defect_catch_rate_local_small"); c.Passed || !c.ObservedUndefined {
		t.Fatalf("not-applicable catch rate must fail closed: %+v", c)
	}
	if res.Decision != DecisionRevise {
		t.Fatalf("want revise, got %s", res.Decision)
	}

	for _, v := range []float64{math.NaN(), math.Inf(1), -0.1, 1.5} {
		bad := s4(tierLocal, 100)
		bad.catch = v
		res = evalOK(t, mkReport(bad, s1(tierLocal, 1000)), nil, crit)
		if res.Decision != DecisionRevise || findCrit(t, res, "defect_catch_rate_local_small").Passed {
			t.Fatalf("catch rate %v must fail closed", v)
		}
		// The embedded input report itself holds the NaN/Inf, so only the criteria are checked.
		if _, err := json.Marshal(res.CriteriaEvaluations); err != nil {
			t.Fatalf("catch %v: criteria not serializable: %v", v, err)
		}
	}
}

// B2/T5: NaN/Inf/negative resource values, zero baselines, JSON safety.
func TestFailClosed_ResourceValues(t *testing.T) {
	crit := DefaultM4GateCriteria()
	bad := []float64{math.NaN(), math.Inf(1), math.Inf(-1), -5}
	for _, v := range bad {
		for _, side := range []string{"s4", "s1"} {
			a, b := s4(tierLocal, 100), s1(tierLocal, 1000)
			if side == "s4" {
				a.resource = v
			} else {
				b.resource = v
			}
			res := evalOK(t, mkReport(a, b), nil, crit)
			c := findCrit(t, res, "resource_efficiency_local_small")
			if c.Passed || res.Decision != DecisionRevise || !c.ObservedUndefined {
				t.Fatalf("%s=%v must fail closed: %+v", side, v, c)
			}
			if _, err := json.Marshal(res.CriteriaEvaluations); err != nil {
				t.Fatalf("%s=%v: criteria not serializable: %v", side, v, err)
			}
		}
	}

	// Peak resident tokens malformed or zero baseline -> fail closed.
	for _, v := range []float64{math.NaN(), math.Inf(1), -1} {
		a := s4(tierLocal, 100)
		a.peakTokens = v
		res := evalOK(t, mkReport(a, s1(tierLocal, 1000)), nil, crit)
		if res.Decision != DecisionRevise || findCrit(t, res, "peak_resident_context_local_small").Passed {
			t.Fatalf("peak S4=%v must fail closed", v)
		}
	}
	zeroPeak := s1(tierLocal, 1000)
	zeroPeak.peakTokens = 0
	res := evalOK(t, mkReport(s4(tierLocal, 100), zeroPeak), nil, crit)
	if res.Decision != DecisionRevise {
		t.Fatalf("zero S1 peak must fail closed")
	}
}

func TestResourceBaselineFloor(t *testing.T) {
	crit := DefaultM4GateCriteria()
	// EWP formula: ratio = S4 / max(S1, 1.0).
	// S1=0 -> denominator 1.0; S4=0.5 -> 0.5 pass; S4=2 -> 2.0 fail.
	res := evalOK(t, mkReport(s4(tierLocal, 0.5), s1(tierLocal, 0)), nil, crit)
	if c := findCrit(t, res, "resource_efficiency_local_small"); !c.Passed || c.Observed != 0.5 {
		t.Fatalf("zero baseline floor: %+v", c)
	}
	res = evalOK(t, mkReport(s4(tierLocal, 2), s1(tierLocal, 0)), nil, crit)
	if c := findCrit(t, res, "resource_efficiency_local_small"); c.Passed || c.Observed != 2 {
		t.Fatalf("zero baseline must not pass a worse S4: %+v", c)
	}
	// S1=0.25 (<1) is also floored to 1.0 (max(S1, 1.0)): S4=0.5 -> 0.5, not 2.0.
	res = evalOK(t, mkReport(s4(tierLocal, 0.5), s1(tierLocal, 0.25)), nil, crit)
	if c := findCrit(t, res, "resource_efficiency_local_small"); c.Observed != 0.5 {
		t.Fatalf("baseline below 1.0 must floor to 1.0: %+v", c)
	}
}

// B2: undefined S4 must produce a JSON-serializable Revise result, and the Markdown must say so.
func TestUndefinedS4_JSONAndMarkdown(t *testing.T) {
	a := s4(tierLocal, 0)
	a.undefined = true
	res := evalOK(t, mkReport(a, s1(tierLocal, 1000)), nil, DefaultM4GateCriteria())
	if res.Decision != DecisionRevise {
		t.Fatalf("want revise, got %s", res.Decision)
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("json.Marshal must succeed for undefined S4: %v", err)
	}
	if !strings.Contains(string(b), `"observed_undefined":true`) {
		t.Fatalf("expected observed_undefined marker in JSON: %s", b)
	}
	md, err := SynthesizeEvidenceReport(res)
	if err != nil || !strings.Contains(md, "Undefined (0 accepted)") {
		t.Fatalf("markdown must disclose undefined observation: err=%v", err)
	}
}

// N3: the summary counts failed criteria, not recommendations.
func TestSummaryCountsFailedCriteria(t *testing.T) {
	a := s4(tierLocal, 5000) // resource fails
	a.catch = 0.1            // catch fails
	res := evalOK(t, mkReport(a, s1(tierLocal, 1000)), nil, DefaultM4GateCriteria())
	if !strings.Contains(res.Summary, "2 criteria did not meet") {
		t.Fatalf("summary should count 2 failed criteria: %q", res.Summary)
	}
	// Failed criterion without details is still counted.
	if len(res.Recommendations) != 2 {
		t.Fatalf("expected 2 recommendations, got %d", len(res.Recommendations))
	}
}

// N1: a persisted result carries falsification inputs so it can be re-evaluated identically.
func TestResultPreservesFalsificationsForReevaluation(t *testing.T) {
	f := map[string]*experiments.FalsificationResult{"k": {IsApplicable: true, HypothesisFalsified: true, Reason: "x"}}
	res := evalOK(t, mkReport(s4(tierLocal, 100), s1(tierLocal, 1000)), f, DefaultM4GateCriteria())
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		Falsifications map[string]*experiments.FalsificationResult `json:"falsification_results"`
		Report         *telemetry.AggregatedReport                 `json:"aggregated_report"`
	}
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	res2 := evalOK(t, back.Report, back.Falsifications, DefaultM4GateCriteria())
	if res2.Decision != res.Decision || len(res2.CriteriaEvaluations) != len(res.CriteriaEvaluations) {
		t.Fatalf("re-evaluation diverged: %s/%d vs %s/%d", res2.Decision, len(res2.CriteriaEvaluations), res.Decision, len(res.CriteriaEvaluations))
	}
}

func okReport() *telemetry.AggregatedReport {
	return mkReport(s4(tierLocal, 50), s1(tierLocal, 100))
}

func falsifiedApplicable() map[string]*experiments.FalsificationResult {
	return map[string]*experiments.FalsificationResult{
		"task-1": {IsApplicable: true, HypothesisFalsified: true, Reason: "floor broken"},
	}
}

func TestCriteriaValidation_RejectsMalformed(t *testing.T) {
	mut := map[string]func(*GateCriteria){
		"fals_neg":     func(c *GateCriteria) { c.MaxFalsificationRate = -1 },
		"fals_nan":     func(c *GateCriteria) { c.MaxFalsificationRate = math.NaN() },
		"fals_inf":     func(c *GateCriteria) { c.MaxFalsificationRate = math.Inf(1) },
		"fals_gt1":     func(c *GateCriteria) { c.MaxFalsificationRate = 1.5 },
		"resident_neg": func(c *GateCriteria) { c.MaxResidentContextRatioBaseline = -0.1 },
		"resident_nan": func(c *GateCriteria) { c.MaxResidentContextRatioBaseline = math.NaN() },
		"resident_inf": func(c *GateCriteria) { c.MaxResidentContextRatioBaseline = math.Inf(1) },
		"res_zero":     func(c *GateCriteria) { c.MaxResourceRatioVersusBaseline = 0 },
		"res_nan":      func(c *GateCriteria) { c.MaxResourceRatioVersusBaseline = math.NaN() },
		"res_inf":      func(c *GateCriteria) { c.MaxResourceRatioVersusBaseline = math.Inf(1) },
		"catch_nan":    func(c *GateCriteria) { c.MinDefectCatchRate = math.NaN() },
		"catch_gt1":    func(c *GateCriteria) { c.MinDefectCatchRate = 1.1 },
		"runs_zero":    func(c *GateCriteria) { c.MinCompletedRuns = 0 },
	}
	for name, m := range mut {
		c := DefaultM4GateCriteria()
		m(&c)
		res, err := EvaluateM4Gate(okReport(), nil, c)
		if err == nil || res != nil {
			t.Errorf("%s: expected invalid-argument error, got res=%v err=%v", name, res, err)
		}
	}
}

func TestFalsificationProbe_NegativeRateNeverGo(t *testing.T) {
	c := DefaultM4GateCriteria()
	c.RequireZeroFalsifications = false
	c.MaxFalsificationRate = -1
	res, err := EvaluateM4Gate(okReport(), falsifiedApplicable(), c)
	if err == nil {
		t.Fatalf("expected error, got decision %v", res.Decision)
	}
}

func TestFalsification_InformationalStillDecidedByAggregate(t *testing.T) {
	c := DefaultM4GateCriteria()
	c.RequireZeroFalsifications = false
	c.MaxFalsificationRate = 0
	res := evalOK(t, okReport(), falsifiedApplicable(), c)
	if res.Decision != DecisionRevise {
		t.Fatalf("falsified entry with zero tolerated rate must Revise, got %s", res.Decision)
	}
	if findCrit(t, res, "aggregate_falsification_rate").Passed {
		t.Fatal("aggregate criterion must fail")
	}
}

func TestNonApplicableFalsifiedDoesNotFail(t *testing.T) {
	f := map[string]*experiments.FalsificationResult{
		"na": {IsApplicable: false, HypothesisFalsified: true, Reason: "n/a"},
	}
	for _, require := range []bool{true, false} {
		c := DefaultM4GateCriteria()
		c.RequireZeroFalsifications = require
		res := evalOK(t, okReport(), f, c)
		if res.Decision != DecisionGo {
			t.Fatalf("require=%v: non-applicable falsified entry must not fail gate, got %s", require, res.Decision)
		}
	}
}

func TestMinCompletedRunsBoundary(t *testing.T) {
	r := okReport()
	r.TotalSnapshots = 10
	if res := evalOK(t, r, nil, DefaultM4GateCriteria()); res.Decision == DecisionInconclusive {
		t.Fatal("TotalSnapshots == MinCompletedRuns must not be inconclusive")
	}
	r.TotalSnapshots = 9
	if res := evalOK(t, r, nil, DefaultM4GateCriteria()); res.Decision != DecisionInconclusive {
		t.Fatalf("below minimum must be inconclusive, got %s", res.Decision)
	}
}

func TestEnoughSnapshotsZeroGroupsInconclusive(t *testing.T) {
	r := &telemetry.AggregatedReport{GeneratedAt: time.Now().UTC(), TotalSnapshots: 100}
	if res := evalOK(t, r, nil, DefaultM4GateCriteria()); res.Decision != DecisionInconclusive {
		t.Fatalf("zero groups must be inconclusive, got %s", res.Decision)
	}
}

func TestZeroBaselinePeakTokens_FailsClosedAndMarshals(t *testing.T) {
	b := s1(tierLocal, 100)
	b.peakTokens = 0
	res := evalOK(t, mkReport(s4(tierLocal, 50), b), nil, DefaultM4GateCriteria())
	c := findCrit(t, res, "peak_resident_context_local_small")
	if c.Passed || !c.ObservedUndefined || math.IsInf(c.Observed, 0) || math.IsNaN(c.Observed) {
		t.Fatalf("unexpected criterion %+v", c)
	}
	if res.Decision != DecisionRevise {
		t.Fatalf("got %s", res.Decision)
	}
	if _, err := json.Marshal(res); err != nil {
		t.Fatalf("result must marshal: %v", err)
	}
}

func TestProvenance_SyntheticNeverRendersAsProof(t *testing.T) {
	res := evalOK(t, okReport(), nil, DefaultM4GateCriteria())
	if res.Decision != DecisionGo {
		t.Fatalf("setup: %s", res.Decision)
	}
	for _, kind := range []string{"", EvidenceKindSyntheticHarness} {
		if kind != "" {
			p, err := NewEvidenceProvenance(kind, "scripted", "abc", "cmd")
			if err != nil {
				t.Fatal(err)
			}
			res.Provenance = p
		}
		md, err := SynthesizeEvidenceReport(res)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(md), "empirical superiority") || strings.Contains(md, "Empirical Evidence Report") {
			t.Fatalf("kind %q rendered as empirical:\n%s", kind, md)
		}
		if !strings.Contains(md, "NOT") {
			t.Fatalf("kind %q missing non-proof statement", kind)
		}
	}
	p, _ := NewEvidenceProvenance(EvidenceKindEmpiricalCampaign, "real", "abc", "cmd")
	res.Provenance = p
	md, _ := SynthesizeEvidenceReport(res)
	if !strings.Contains(md, "empirical_campaign") || strings.Contains(md, "NON-EMPIRICAL") {
		t.Fatalf("empirical report mislabelled:\n%s", md)
	}
}

func TestProvenance_Validation(t *testing.T) {
	if _, err := NewEvidenceProvenance("bogus", "d", "", ""); err == nil {
		t.Fatal("unknown kind must fail")
	}
	if _, err := NewEvidenceProvenance(EvidenceKindSyntheticHarness, " ", "", ""); err == nil {
		t.Fatal("missing driver must fail")
	}
}
