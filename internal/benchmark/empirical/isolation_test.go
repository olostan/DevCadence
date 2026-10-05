package empirical

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// hasLimit reports whether any recorded reason message contains the fragment.
// Reason codes are shared by many checks, so isolation tests also pin the
// message of the one check under test.
func hasLimit(r AdmissionResult, fragment string) bool {
	for _, l := range r.Limitations {
		if strings.Contains(l, fragment) {
			return true
		}
	}
	return false
}

func admitWith(t *testing.T, f *fixture, v IndependentVerifier) AdmissionResult {
	t.Helper()
	return f.admit(t, v)
}

// A plan whose bytes match the manifest digest but are not canonical is refused
// by the canonical-encoding check and nothing else.
func TestNonCanonicalPlanBytesRejected(t *testing.T) {
	f := newFixture()
	m := f.manifest()
	b, err := json.MarshalIndent(f.plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	f.store["plan"] = b
	m.PlanDigest = bytesDigest(b)
	v := &countingVerifier{}
	res, err := validateAdmission(context.Background(), m, f.store, v, allowAuthority{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Admitted || !hasCode(res, ReasonDigestMismatch) || !hasLimit(res, "not the canonical encoding") || v.calls != 0 {
		t.Fatalf("admitted=%v codes=%v limits=%v", res.Admitted, res.ReasonCodes, res.Limitations)
	}
	if len(res.ReasonCodes) != 1 {
		t.Fatalf("only the canonical check may fire: %v", res.ReasonCodes)
	}
}

func TestAdjacentDuplicateRunFlagged(t *testing.T) {
	f := newFixture()
	f.runs = append(f.runs[:1], append([]RunEvidence{f.runs[0]}, f.runs[1:]...)...)
	res := admitWith(t, f, &countingVerifier{})
	if res.Admitted || !hasCode(res, ReasonRunMismatch) || !hasLimit(res, "reported more than once") {
		t.Fatalf("codes=%v limits=%v", res.ReasonCodes, res.Limitations)
	}
}

func TestAuthorizationCapIsolation(t *testing.T) {
	const above = "authorized caps exceed the plan ceilings"
	const cover = "cannot cover the declared run matrix"
	cases := map[string]struct {
		mut      func(*fixture)
		fragment string
	}{
		"max total runs above plan":     {func(f *fixture) { f.plan.Limits.MaxTotalRuns = 20 }, above},
		"max calls per run above plan":  {func(f *fixture) { f.plan.Limits.MaxCallsPerRun = 3 }, above},
		"max total calls above plan":    {func(f *fixture) { f.plan.Limits.MaxTotalCalls = 100 }, above},
		"max run seconds above plan":    {func(f *fixture) { f.plan.Limits.MaxRunSeconds = 900 }, above},
		"max campaign secs above plan":  {func(f *fixture) { f.plan.Limits.MaxCampaignSeconds = 3600 }, above},
		"max api spend above plan":      {func(f *fixture) { f.auth.MaxAPISpendUSD = 5 }, above},
		"max subscription above plan":   {func(f *fixture) { f.plan.Limits.MaxSubscriptionCalls = 100 }, above},
		"max local compute above plan":  {func(f *fixture) { f.auth.MaxLocalComputeSeconds = 10 }, above},
		"allow metered above plan":      {func(f *fixture) { f.auth.AllowMetered = true }, above},
		"unknown quota above plan":      {func(f *fixture) { f.plan.Limits.AllowUnknownSubscriptionQuota = false }, above},
		"max total runs below matrix":   {func(f *fixture) { f.auth.MaxTotalRuns = 19 }, cover},
		"max total calls below matrix":  {func(f *fixture) { f.auth.MaxTotalCalls = 19 }, cover},
		"zero calls per run":            {func(f *fixture) { f.auth.MaxCallsPerRun = 0 }, cover},
		"zero run seconds":              {func(f *fixture) { f.auth.MaxRunSeconds = 0 }, cover},
		"zero campaign seconds":         {func(f *fixture) { f.auth.MaxCampaignSeconds = 0 }, cover},
		"zero subscription call cap":    {func(f *fixture) { f.auth.MaxSubscriptionCalls = 0 }, "positive subscription call cap"},
		"unknown quota without consent": {func(f *fixture) { f.auth.AllowUnknownSubscriptionQuota = false }, "unknown subscription quota without explicit permission"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			c.mut(f)
			v := &countingVerifier{}
			res := admitWith(t, f, v)
			if res.Admitted || !hasCode(res, ReasonAuthorizationBad) || !hasLimit(res, c.fragment) || v.calls != 0 {
				t.Fatalf("admitted=%v codes=%v limits=%v", res.Admitted, res.ReasonCodes, res.Limitations)
			}
		})
	}
}

// Each half of the metered-grant condition is isolated: the grant flag alone and
// the positive spend cap alone.
func TestMeteredGrantHalvesIsolated(t *testing.T) {
	const frag = "needs an explicit metered grant with a positive spend cap"
	setup := func(f *fixture, allow bool, spend float64) {
		for i := range f.plan.Runs {
			f.plan.Runs[i].Endpoint.CapabilityClass = "frontier_api"
			f.runs[i].Endpoint.CapabilityClass = "frontier_api"
		}
		f.plan.RequestedTiers = []string{"frontier_api"}
		f.plan.Limits.AllowMetered = true
		f.plan.Limits.MaxAPISpendUSD = 10
		f.auth.AllowedEndpointBindings[0].CapabilityClass = "frontier_api"
		f.auth.AllowMetered = allow
		f.auth.MaxAPISpendUSD = spend
	}
	for name, c := range map[string]struct {
		allow bool
		spend float64
	}{
		"positive spend without grant": {false, 10},
		"grant with zero spend cap":    {true, 0},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			setup(f, c.allow, c.spend)
			res := admitWith(t, f, &countingVerifier{})
			if res.Admitted || !hasCode(res, ReasonAuthorizationBad) || !hasLimit(res, frag) {
				t.Fatalf("codes=%v limits=%v", res.ReasonCodes, res.Limitations)
			}
		})
	}
	t.Run("full grant has no metered refusal", func(t *testing.T) {
		f := newFixture()
		setup(f, true, 10)
		res := admitWith(t, f, &countingVerifier{})
		if hasLimit(res, frag) {
			t.Fatalf("limits=%v", res.Limitations)
		}
	})
}

// The snapshot's defect fields must agree with the independently verified counts.
func TestSnapshotDefectBindsVerifiedCounts(t *testing.T) {
	cases := map[string]struct {
		mut      func(*VerifiedOutcome, RunEvidence)
		fragment string
	}{
		"seeded flag vs verified total": {func(o *VerifiedOutcome, _ RunEvidence) { o.SeededDefectTotal = 0; o.SeededDefectsCaught = 0 }, "defect_seeded differs"},
		"status vs verified catch":      {func(o *VerifiedOutcome, _ RunEvidence) { o.SeededDefectTotal = 2; o.SeededDefectsCaught = 1 }, "defect status differs"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			res := admitWith(t, newFixture(), &countingVerifier{mut: c.mut})
			if res.Admitted || !hasCode(res, ReasonRunInvalid) || !hasLimit(res, c.fragment) {
				t.Fatalf("codes=%v limits=%v", res.ReasonCodes, res.Limitations)
			}
		})
	}
}
