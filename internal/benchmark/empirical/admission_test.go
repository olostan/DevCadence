package empirical

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/benchmark/experiments"
	"github.com/olostan/DevCadence/internal/benchmark/gate"
	"github.com/olostan/DevCadence/internal/protocol"
)

func hasCode(r AdmissionResult, code string) bool {
	for _, c := range r.ReasonCodes {
		if c == code {
			return true
		}
	}
	return false
}

func f64(v float64) *float64 { return &v }

// Production entry point: no operator authority exists, so a perfect fixture is
// still denied and the verifier is never called (A7, A14; never empirical-ready).
func TestProductionAdmissionFailsClosedWithZeroVerifierCalls(t *testing.T) {
	f := newFixture()
	v := &countingVerifier{}
	res, err := ValidateAdmission(context.Background(), f.manifest(), f.store, v)
	if err != nil {
		t.Fatal(err)
	}
	if res.Admitted || !hasCode(res, ReasonOperatorAuthority) || v.calls != 0 {
		t.Fatalf("admitted=%v codes=%v calls=%d", res.Admitted, res.ReasonCodes, v.calls)
	}
	if _, err := ReplayGate(res, gate.DefaultM4GateCriteria()); err == nil {
		t.Fatal("replay of a denied admission must fail")
	}
}

func TestControlFlowAdmitsOnlyThroughVerifierAndAuthority(t *testing.T) {
	f := newFixture()
	v := &countingVerifier{}
	res := f.admit(t, v)
	if !res.Admitted || v.calls != 20 || len(res.CompletedRunIDs) != 20 || res.Counts.Completed != 20 || res.Counts.Accepted != 20 {
		t.Fatalf("%+v calls=%d", res, v.calls)
	}
	if len(res.ComparableResourceMetrics) < 2 {
		t.Fatalf("metrics %v", res.ComparableResourceMetrics)
	}
	rep, err := ReplayGate(res, gate.DefaultM4GateCriteria())
	if err != nil {
		t.Fatal(err)
	}
	if rep.RawGate == nil || rep.RawGate.Decision != gate.DecisionGo || rep.Conclusion != ConclusionGo ||
		rep.RawGate.Provenance == nil || rep.RawGate.Provenance.Kind != gate.EvidenceKindEmpiricalCampaign || len(rep.Verifiers) != 20 {
		t.Fatalf("%+v", rep)
	}
}

func TestNilVerifierIsNotAdmitted(t *testing.T) { // A14
	f := newFixture()
	res := f.admit(t, nil)
	if res.Admitted || !hasCode(res, ReasonUnverifiedOutcome) {
		t.Fatalf("%+v", res)
	}
}

func TestVerifierFailureAndMismatchesRefuse(t *testing.T) { // A5, A6, A14
	cases := map[string]struct {
		v    *countingVerifier
		code string
	}{
		"error":           {&countingVerifier{err: errors.New("down")}, ReasonUnverifiedOutcome},
		"wrong candidate": {&countingVerifier{mut: func(o *VerifiedOutcome, _ RunEvidence) { o.CandidateDigest = dg("other") }}, ReasonVerifierRejected},
		"wrong plan":      {&countingVerifier{mut: func(o *VerifiedOutcome, _ RunEvidence) { o.PlanDigest = dg("other") }}, ReasonVerifierRejected},
		"wrong receipt":   {&countingVerifier{mut: func(o *VerifiedOutcome, _ RunEvidence) { o.ReceiptDigest = dg("other") }}, ReasonVerifierRejected},
		"verdict flips":   {&countingVerifier{mut: func(o *VerifiedOutcome, _ RunEvidence) { o.QualityVerdict = QualityRejected }}, ReasonVerifierRejected},
		"bad verdict":     {&countingVerifier{mut: func(o *VerifiedOutcome, _ RunEvidence) { o.QualityVerdict = "great" }}, ReasonVerifierRejected},
		"bad counts":      {&countingVerifier{mut: func(o *VerifiedOutcome, _ RunEvidence) { o.SeededDefectsCaught = 5 }}, ReasonVerifierRejected},
		"no commands":     {&countingVerifier{mut: func(o *VerifiedOutcome, _ RunEvidence) { o.VerifiedCommandArtifactRefs = nil }}, ReasonVerifierRejected},
		"same invocation": {&countingVerifier{mut: func(o *VerifiedOutcome, _ RunEvidence) { o.Verifier.InvocationID = o.Worker.InvocationID }}, ReasonNotIndependent},
		"lineage overlap": {&countingVerifier{mut: func(o *VerifiedOutcome, _ RunEvidence) {
			o.Verifier.LineageActorIDs = []string{o.Worker.ActorID}
		}}, ReasonNotIndependent},
		"actor differs": {&countingVerifier{mut: func(o *VerifiedOutcome, _ RunEvidence) { o.Worker.ActorID = "someone-else" }}, ReasonNotIndependent},
		"wrong role":    {&countingVerifier{mut: func(o *VerifiedOutcome, _ RunEvidence) { o.Verifier.Role = protocol.ProvenanceRoleImplementer }}, ReasonNotIndependent},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			res := newFixture().admit(t, c.v)
			if res.Admitted || !hasCode(res, c.code) {
				t.Fatalf("admitted=%v codes=%v", res.Admitted, res.ReasonCodes)
			}
			if _, err := ReplayGate(res, gate.DefaultM4GateCriteria()); err == nil {
				t.Fatal("replay must refuse")
			}
		})
	}
}

func TestStructuralRefusalsMakeZeroVerifierCalls(t *testing.T) {
	cases := map[string]struct {
		mut  func(*fixture)
		code string
	}{
		"synthetic driver relabeled empirical": {func(f *fixture) {
			for i := range f.runs {
				f.runs[i].Endpoint.DriverID = "scripted-driver"
				f.plan.Runs[i].Endpoint.DriverID = "scripted-driver"
			}
			f.auth.AllowedEndpointBindings[0].DriverID = "scripted-driver"
		}, ReasonPlanInvalid}, // A1 rejected at the plan
		"synthetic producer":   {func(f *fixture) { f.runs[0].InvocationProducerID = "mock-worker" }, ReasonSyntheticEvidence},
		"model changed in run": {func(f *fixture) { f.runs[3].Endpoint.ModelID = "model-b" }, ReasonRunMismatch}, // A2/A13
		"seed changed":         {func(f *fixture) { f.runs[3].Seed = "other" }, ReasonRunMismatch},               // A13
		"unplanned run":        {func(f *fixture) { r := f.runs[0]; r.RunID = "extra"; f.runs = append(f.runs, r) }, ReasonRunMismatch},
		"duplicate run":        {func(f *fixture) { f.runs = append(f.runs, f.runs[0]) }, ReasonRunMismatch},
		"reordered":            {func(f *fixture) { f.runs[0], f.runs[1] = f.runs[1], f.runs[0] }, ReasonRunMismatch},
		"auth wrong plan":      {func(f *fixture) { f.planDigestOverride = dg("other") }, ReasonAuthorizationBad}, // A13
		"zero total runs cap":  {func(f *fixture) { f.auth.MaxTotalRuns = 0 }, ReasonAuthorizationBad},            // A7
		"zero calls cap":       {func(f *fixture) { f.auth.MaxCallsPerRun = 0 }, ReasonAuthorizationBad},
		"cap above plan":       {func(f *fixture) { f.plan.Limits.MaxTotalCalls = 100; f.auth.MaxTotalCalls = 480 }, ReasonAuthorizationBad},
		"truncated matrix":     {func(f *fixture) { f.auth.MaxTotalRuns = 10 }, ReasonAuthorizationBad},
		"unauthorized binding": {func(f *fixture) { f.auth.AllowedEndpointBindings[0].ModelRevision = "r2" }, ReasonAuthorizationBad},
		"unknown quota denied": {func(f *fixture) { f.auth.AllowUnknownSubscriptionQuota = false }, ReasonAuthorizationBad},
		"metered w/o grant": {func(f *fixture) {
			for i := range f.plan.Runs {
				f.plan.Runs[i].Endpoint.CapabilityClass = "frontier_api"
				f.runs[i].Endpoint.CapabilityClass = "frontier_api"
			}
			f.plan.RequestedTiers = []string{"frontier_api"}
			f.auth.AllowedEndpointBindings[0].CapabilityClass = "frontier_api"
		}, ReasonAuthorizationBad}, // A7/A8: unknown API cost never launches
		"unknown spend with positive cap": {func(f *fixture) {
			f.plan.Limits.MaxAPISpendMicroUSD = 10000000
			f.auth.MaxAPISpendMicroUSD = 10000000
			f.runs[0].Measurements["api_spend_usd"] = Measurement{Known: false, Unit: "USD", Provenance: ProvenanceUnknown, EvidenceRef: "e"}
		}, ReasonAuthorizationBad},
		"bad expiry":            {func(f *fixture) { f.auth.Expiry = "tomorrow" }, ReasonAuthorizationBad},
		"completed w/o receipt": {func(f *fixture) { f.runs[0].VerifierReceiptRef = "" }, ReasonRunInvalid}, // A5
		"self verification":     {func(f *fixture) { f.runs[0].VerifierProducerID = f.runs[0].InvocationProducerID }, ReasonNotIndependent},
		"failed but accepted":   {func(f *fixture) { f.runs[0].Status = StatusFailed; f.runs[0].Accepted = true }, ReasonRunInvalid},
		"bad status":            {func(f *fixture) { f.runs[0].Status = "done" }, ReasonRunInvalid},
		"snapshot hides unknown": {func(f *fixture) {
			f.runs[0].Measurements["output_tokens"] = Measurement{Unit: "token", Provenance: ProvenanceUnknown, EvidenceRef: "why"}
		}, ReasonRunInvalid}, // A4: snapshot carries 20 for an unknown value
		"unknown with zero value": {func(f *fixture) {
			f.runs[0].Measurements["output_tokens"] = Measurement{Value: f64(0), Unit: "token", Provenance: ProvenanceUnknown, EvidenceRef: "why"}
		}, ReasonMeasurementInvalid},
		"known estimate": {func(f *fixture) {
			m := f.runs[0].Measurements["output_tokens"]
			m.Provenance = "estimated"
			f.runs[0].Measurements["output_tokens"] = m
		}, ReasonMeasurementInvalid},
		"fractional count": {func(f *fixture) {
			m := f.runs[0].Measurements["output_tokens"]
			m.Value = f64(1.5)
			f.runs[0].Measurements["output_tokens"] = m
		}, ReasonMeasurementInvalid},
		"negative spend": {func(f *fixture) {
			f.runs[0].Measurements["api_spend_usd"] = Measurement{Known: true, Value: f64(-1), Unit: "USD", Provenance: ProvenanceMeasured, EvidenceRef: "e"}
		}, ReasonMeasurementInvalid},
		"missing key": {func(f *fixture) { delete(f.runs[0].Measurements, "wall_seconds") }, ReasonMeasurementInvalid},
		"wrong unit": {func(f *fixture) {
			m := f.runs[0].Measurements["wall_seconds"]
			m.Unit = "ms"
			f.runs[0].Measurements["wall_seconds"] = m
		}, ReasonMeasurementInvalid},
		"quota w/o unit": {func(f *fixture) {
			f.runs[0].Measurements["subscription_quota"] = Measurement{Known: true, Value: f64(3), Unit: "provider_unit", Provenance: ProvenanceMeasured, EvidenceRef: "e"}
		}, ReasonMeasurementInvalid},
		"dropped planned limitation": {func(f *fixture) {
			f.plan.RequestedTiers = []string{"frontier_api", "subscription_cli"}
			f.plan.MissingTiers = []TierLimitation{{Tier: "frontier_api", Cause: CauseNotAuthorized, EvidenceRef: "e"}}
		}, ReasonPlanMismatch},
		"snapshot accepted differs": {func(f *fixture) { s := f.snaps[f.runs[0].RunID]; s.Accepted = false; f.snaps[f.runs[0].RunID] = s }, ReasonRunInvalid},
		"falsification null":        {func(f *fixture) { f.fals["x"] = nil }, ReasonFalsificationBad},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			c.mut(f)
			v := &countingVerifier{}
			var res AdmissionResult
			var err error
			if name == "dropped planned limitation" {
				m := f.manifest()
				res, err = validateAdmission(context.Background(), m, f.store, v, allowAuthority{}, testClock)
			} else {
				res, err = validateAdmission(context.Background(), f.manifest(), f.store, v, allowAuthority{}, testClock)
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.Admitted || !hasCode(res, c.code) {
				t.Fatalf("admitted=%v codes=%v limits=%v", res.Admitted, res.ReasonCodes, res.Limitations)
			}
			if v.calls != 0 && c.code != ReasonNotIndependent {
				t.Fatalf("verifier called %d times before refusal", v.calls)
			}
		})
	}
}

func TestArtifactIntegrity(t *testing.T) {
	f := newFixture()
	m := f.manifest()
	for name, mut := range map[string]func(*CampaignManifest, memStore){
		"plan digest wrong":  func(m *CampaignManifest, _ memStore) { m.PlanDigest = dg("x") },
		"plan bytes swapped": func(_ *CampaignManifest, s memStore) { s["plan"] = append([]byte(" "), s["plan"]...) },
		"missing artifact":   func(_ *CampaignManifest, s memStore) { delete(s, "auth") },
		"snapshot tampered":  func(_ *CampaignManifest, s memStore) { s["snap/run-1-t1-full_history"] = []byte(`{}`) },
		"unknown field": func(m *CampaignManifest, s memStore) {
			s["fals"] = []byte(`{"a":{"nope":1}}`)
			m.FalsificationEvidenceDigest = bytesDigest(s["fals"])
		},
		"schema version":   func(m *CampaignManifest, _ memStore) { m.SchemaVersion = "2.0" },
		"no regen command": func(m *CampaignManifest, _ memStore) { m.RegenerationCommand = nil },
	} {
		t.Run(name, func(t *testing.T) {
			mm := m
			mm.Runs = append([]RunEvidence(nil), m.Runs...)
			s := memStore{}
			for k, v := range f.store {
				s[k] = v
			}
			mut(&mm, s)
			res, _ := validateAdmission(context.Background(), mm, s, &countingVerifier{}, allowAuthority{}, testClock)
			if res.Admitted || len(res.ReasonCodes) == 0 {
				t.Fatalf("%v", res.ReasonCodes)
			}
		})
	}
	if res, _ := validateAdmission(context.Background(), m, nil, &countingVerifier{}, allowAuthority{}, testClock); res.Admitted || !hasCode(res, ReasonArtifactUnavailable) {
		t.Fatalf("nil resolver: %v", res.ReasonCodes)
	}
	// A resolver returning bytes that do not match the digest is not trusted.
	lying := lyingResolver{f.store}
	if res, _ := validateAdmission(context.Background(), m, lying, &countingVerifier{}, allowAuthority{}, testClock); res.Admitted || !hasCode(res, ReasonDigestMismatch) {
		t.Fatalf("lying resolver: %v", res.ReasonCodes)
	}
}

type lyingResolver struct{ s memStore }

func (l lyingResolver) ReadVerified(ctx context.Context, ref, d string) ([]byte, error) {
	b, err := l.s.ReadVerified(ctx, ref, d)
	if err == nil && ref == "plan" {
		return append([]byte(nil), append(b, ' ')...), nil
	}
	return b, err
}

func TestFailedBlockedAndUnpairedRunsAreCountedNotCompleted(t *testing.T) { // A3, A8, A11
	f := newFixture()
	// Run 0 fails (zero metrics, unknown) so its partner is excluded from comparison.
	f.runs[0].Status = StatusFailed
	f.runs[0].Accepted = false
	f.runs[0].Measurements = measurements(0, 0)
	for k, m := range f.runs[0].Measurements {
		f.runs[0].Measurements[k] = Measurement{Unit: m.Unit, Provenance: ProvenanceUnknown, EvidenceRef: "why"}
	}
	delete(f.snaps, f.runs[0].RunID)
	f.runs[0].SnapshotRef = ""
	f.runs[2].Status = StatusBlocked
	f.runs[2].Accepted = false
	f.runs = f.runs[:len(f.runs)-1] // last planned run is missing, not zero
	res := f.admit(t, &countingVerifier{})
	if !res.Admitted {
		t.Fatalf("%v", res.ReasonCodes)
	}
	c := res.Counts
	if c.Failed != 1 || c.Blocked != 1 || c.Missing != 1 || c.Planned != 20 || c.Reported != 19 || c.Attempted != 19 || c.Completed != 17 {
		t.Fatalf("%+v", c)
	}
	// 20 planned: 3 pairs broken (run0/1, run2/3, run19 missing partner run18) -> 14 admitted.
	if len(res.CompletedRunIDs) != 14 || len(res.ExcludedRunIDs) != 6 {
		t.Fatalf("completed=%d excluded=%d", len(res.CompletedRunIDs), len(res.ExcludedRunIDs))
	}
	joined := strings.Join(res.Limitations, "|")
	if !strings.Contains(joined, "INCOMPLETE_PAIR") || !strings.Contains(joined, "INCOMPLETE_COVERAGE") {
		t.Fatal(joined)
	}
	rep, err := ReplayGate(res, gate.DefaultM4GateCriteria())
	if err != nil || rep.Conclusion == ConclusionGo {
		t.Fatalf("incomplete coverage must not be go: %v %v", err, rep)
	}
}

func TestNoVerifiedCompletedRunIsNotAdmitted(t *testing.T) {
	f := newFixture()
	for i := range f.runs {
		f.runs[i].Status = StatusFailed
		f.runs[i].Accepted = false
		f.runs[i].SnapshotRef = ""
		delete(f.snaps, f.runs[i].RunID)
	}
	res := f.admit(t, &countingVerifier{})
	if res.Admitted || res.Counts.Failed != 20 || res.Counts.Completed != 0 {
		t.Fatalf("%+v", res)
	}
}

func TestUnknownTokenAccountingNeverFeedsZerosToGate(t *testing.T) { // A4, A10
	f := newFixture()
	for i := range f.runs {
		f.runs[i].Measurements["cumulative_input_tokens"] = Measurement{Unit: "token", Provenance: ProvenanceUnknown, EvidenceRef: "why"}
		s := f.snaps[f.runs[i].RunID]
		s.CumulativeInputTokens = 0
		s.AccountingUncertain = true
		f.snaps[f.runs[i].RunID] = s
	}
	res := f.admit(t, &countingVerifier{})
	if !res.Admitted {
		t.Fatalf("%v %v", res.ReasonCodes, res.Limitations)
	}
	for _, m := range res.ComparableResourceMetrics {
		if m == "cumulative_input_tokens" {
			t.Fatal("unknown metric listed as comparable")
		}
	}
	rep, err := ReplayGate(res, gate.DefaultM4GateCriteria())
	if err != nil || rep.RawGate != nil || rep.RawGateSkipped == "" || rep.Conclusion != ConclusionInconclusive {
		t.Fatalf("%v %+v", err, rep)
	}
}

func replayWith(t *testing.T, mut func(*fixture)) *Report {
	t.Helper()
	return replayVerified(t, mut, &countingVerifier{})
}

func replayVerified(t *testing.T, mut func(*fixture), v IndependentVerifier) *Report {
	t.Helper()
	f := newFixture()
	mut(f)
	res := f.admit(t, v)
	if !res.Admitted {
		t.Fatalf("%v %v", res.ReasonCodes, res.Limitations)
	}
	rep, err := ReplayGate(res, gate.DefaultM4GateCriteria())
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestObservedFailuresReviseAndAbsentEvidenceIsInconclusive(t *testing.T) { // A10
	if r := replayWith(t, func(f *fixture) {
		f.fals["t1"] = &experiments.FalsificationResult{IsApplicable: true, HypothesisFalsified: true, Reason: "bad"}
	}); r.Conclusion != ConclusionRevise || r.RawGate.Decision != gate.DecisionRevise {
		t.Fatalf("%+v", r.Conclusion)
	}
	if r := replayWith(t, func(f *fixture) { f.fals = nil }); r.Conclusion != ConclusionInconclusive ||
		r.RawGate.Decision != gate.DecisionGo || !strings.Contains(strings.Join(r.ConclusionReasons, "|"), "falsification") {
		t.Fatalf("raw go must not become empirical go without falsification: %+v", r)
	}
	if r := replayWith(t, func(f *fixture) {
		f.fals = map[string]*experiments.FalsificationResult{"t1": {IsApplicable: false, Reason: "saturated"}}
	}); r.Conclusion != ConclusionInconclusive {
		t.Fatal("inapplicable falsification cannot support go")
	}
	if r := replayWith(t, func(f *fixture) { // missing tier => no all-tier claim
		f.plan.RequestedTiers = []string{"frontier_api", "subscription_cli"}
		f.plan.MissingTiers = []TierLimitation{{Tier: "frontier_api", Cause: CauseNotAuthorized, EvidenceRef: "e"}}
		f.missing = f.plan.MissingTiers
	}); r.Conclusion != ConclusionInconclusive || r.RawGate.Decision != gate.DecisionGo || len(r.MissingTiers) != 1 {
		t.Fatalf("%+v", r)
	}
	// A completed-but-rejected Strategy 4 outcome is a legitimate quality failure kept in the denominator.
	rejecting := &countingVerifier{mut: func(o *VerifiedOutcome, ev RunEvidence) {
		if ev.Strategy == StrategyHybrid4 {
			o.QualityVerdict = QualityRejected
			o.SeededDefectsCaught = 0
		}
	}}
	if r := replayVerified(t, func(f *fixture) {
		for i := range f.runs {
			if f.runs[i].Strategy == StrategyHybrid4 {
				f.runs[i].Accepted = false
				s := f.snaps[f.runs[i].RunID]
				s.Accepted = false
				s.DefectStatus = "missed"
				f.snaps[f.runs[i].RunID] = s
			}
		}
	}, rejecting); r.Conclusion != ConclusionRevise || r.RawGate.Decision != gate.DecisionRevise {
		t.Fatalf("%+v", r.Conclusion)
	}
}

func TestReplayRefusesForgedResultsAndChangedCriteria(t *testing.T) {
	crit := gate.DefaultM4GateCriteria()
	if _, err := ReplayGate(AdmissionResult{Admitted: true}, crit); err == nil {
		t.Fatal("hand-built admitted result must be refused")
	}
	f := newFixture()
	res := f.admit(t, &countingVerifier{})
	weak := crit
	weak.MaxResidentContextRatioBaseline = 0
	if _, err := ReplayGate(res, weak); err == nil {
		t.Fatal("weakened criteria must be refused")
	}
	weak = crit
	weak.MinCompletedRuns = 1
	if _, err := ReplayGate(res, weak); err == nil {
		t.Fatal("changed criteria must be refused")
	}
	f2 := newFixture()
	f2.plan.CriteriaDigest = dg("other")
	res2 := f2.admit(t, &countingVerifier{})
	if _, err := ReplayGate(res2, crit); err == nil {
		t.Fatal("criteria digest mismatch must be refused")
	}
}

func TestValidatePlan(t *testing.T) {
	if p := ValidatePlan(newFixture().plan); len(p) != 0 {
		t.Fatal(p)
	}
	cases := map[string]func(*CampaignPlan){
		"version":       func(p *CampaignPlan) { p.SchemaVersion = "0" },
		"ordinal gap":   func(p *CampaignPlan) { p.Runs[3].Ordinal = 9 },
		"duplicate id":  func(p *CampaignPlan) { p.Runs[1].RunID = p.Runs[0].RunID },
		"strategy enum": func(p *CampaignPlan) { p.Runs[0].Strategy = "compacted" },
		"repetition":    func(p *CampaignPlan) { p.Runs[0].Repetition = 3 },
		"order rep1": func(p *CampaignPlan) {
			p.Runs[0], p.Runs[1] = p.Runs[1], p.Runs[0]
			p.Runs[0].Ordinal, p.Runs[1].Ordinal = 1, 2
		},
		"unpaired":                          func(p *CampaignPlan) { p.Runs = p.Runs[:19] },
		"ceiling":                           func(p *CampaignPlan) { p.Limits.MaxTotalCalls = 481 },
		"negative cap":                      func(p *CampaignPlan) { p.Limits.MaxAPISpendMicroUSD = -1 },
		"missing verifier source commit":    func(p *CampaignPlan) { p.VerifierSourceCommit = "" },
		"missing verification profile hash": func(p *CampaignPlan) { p.VerificationProfileDigest = "invalid" },
		"tier unknown":                      func(p *CampaignPlan) { p.RequestedTiers = []string{"bogus"} },
		"tier unsorted":                     func(p *CampaignPlan) { p.RequestedTiers = []string{"subscription_cli", "local_small"} },
		"tier no runs":                      func(p *CampaignPlan) { p.RequestedTiers = []string{"local_small", "subscription_cli"} },
		"limit cause": func(p *CampaignPlan) {
			p.MissingTiers = []TierLimitation{{Tier: "subscription_cli", Cause: "x", EvidenceRef: "e"}}
		},
		"limit no ref":   func(p *CampaignPlan) { p.MissingTiers = []TierLimitation{{Tier: "local_small", Cause: CauseAbsent}} },
		"ewp differs":    func(p *CampaignPlan) { p.Runs[1].EWP.Version = 2 },
		"bad digest":     func(p *CampaignPlan) { p.CorpusDigest = "x" },
		"endpoint blank": func(p *CampaignPlan) { p.Runs[0].Endpoint.ModelID = "" },
		"bad class":      func(p *CampaignPlan) { p.Runs[0].Endpoint.CapabilityClass = "gpu" },
		"too many runs":  func(p *CampaignPlan) { p.Limits.MaxTotalRuns = 19 },
		"duplicate pair": func(p *CampaignPlan) { p.Runs[1].Strategy = p.Runs[0].Strategy },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			p := newFixture().plan
			mut(&p)
			if len(ValidatePlan(p)) == 0 {
				t.Fatal("expected plan problems")
			}
		})
	}
}

func TestPlanDigestCoversSeedOrderAndEWPButNotAuthorization(t *testing.T) { // A13
	base := newFixture().plan
	d0, err := PlanDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(*CampaignPlan){
		"seed":  func(p *CampaignPlan) { p.Runs[0].Seed = "s2" },
		"order": func(p *CampaignPlan) { p.Runs[0], p.Runs[1] = p.Runs[1], p.Runs[0] },
		"ewp":   func(p *CampaignPlan) { p.Runs[0].EWP.BaseCommit = "zzz" },
		"cap":   func(p *CampaignPlan) { p.Limits.MaxRunSeconds = 1 },
	} {
		p := newFixture().plan
		mut(&p)
		if d, _ := PlanDigest(p); d == d0 {
			t.Fatalf("%s not covered by the plan digest", name)
		}
	}
}
