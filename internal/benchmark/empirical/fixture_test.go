package empirical

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/olostan/DevCadence/internal/benchmark/experiments"
	"github.com/olostan/DevCadence/internal/benchmark/gate"
	"github.com/olostan/DevCadence/internal/benchmark/telemetry"
	"github.com/olostan/DevCadence/internal/protocol"
)

// These fixtures are synthetic: they exercise schema, control flow and
// zero-call denial only. They never establish live (A9) evidence.

type memStore map[string][]byte

func (s memStore) ReadVerified(_ context.Context, ref, _ string) ([]byte, error) {
	b, ok := s[ref]
	if !ok {
		return nil, errors.New("not found")
	}
	return b, nil
}

type allowAuthority struct{}

func (allowAuthority) VerifyAuthorization(context.Context, string, string, []byte) error { return nil }

type countingVerifier struct {
	calls int
	mut   func(*VerifiedOutcome, RunEvidence)
	err   error
}

func (v *countingVerifier) Verify(_ context.Context, plan CampaignPlan, _ PlannedRun, ev RunEvidence, _ ArtifactResolver) (VerifiedOutcome, error) {
	v.calls++
	if v.err != nil {
		return VerifiedOutcome{}, v.err
	}
	pd, _ := PlanDigest(plan)
	o := VerifiedOutcome{
		RunID: ev.RunID, PlanDigest: pd, SessionDigest: dg("session" + ev.RunID),
		CandidateDigest: ev.CandidateArtifactDigest, SnapshotDigest: ev.SnapshotDigest, ReceiptDigest: ev.VerifierReceiptDigest,
		VerifierSourceCommit: "verifiersrc", VerificationProfileDigest: dg("profile"),
		Worker:         protocol.ActorProvenance{ActorID: ev.InvocationProducerID, InvocationID: "inv-w-" + ev.RunID, Role: protocol.ProvenanceRoleImplementer},
		Verifier:       protocol.ActorProvenance{ActorID: ev.VerifierProducerID, InvocationID: "inv-v-" + ev.RunID, Role: protocol.ProvenanceRoleVerifier},
		QualityVerdict: QualityAccepted, SeededDefectTotal: 1, SeededDefectsCaught: 1,
		VerifiedCommandArtifactRefs: []string{"cmd/" + ev.RunID},
	}
	if v.mut != nil {
		v.mut(&o, ev)
	}
	return o, nil
}

func dg(s string) string {
	b, _ := protocol.CanonicalJSON(s)
	return bytesDigest(b)
}

type fixture struct {
	plan               CampaignPlan
	auth               CampaignAuthorization
	runs               []RunEvidence
	snaps              map[string]telemetry.RunTelemetrySnapshot
	fals               map[string]*experiments.FalsificationResult
	store              memStore
	planDigestOverride string
	missing            []TierLimitation
}

func endpoint() EndpointBinding {
	return EndpointBinding{EndpointID: "ep-sub", DriverID: "claude-cli", ModelID: "model-a", ModelRevision: "r1", ChannelID: "ch1",
		CapabilityClass: "subscription_cli", RuntimeVersion: "1.0", ContextProfileDigest: dg("ctx"), PolicyDigest: dg("pol"),
		SubscriptionQuotaUnit: UnknownQuotaUnit}
}

func measurements(cum, peak int64) map[string]Measurement {
	m := map[string]Measurement{}
	for k, u := range measurementUnits {
		m[k] = Measurement{Unit: u, Provenance: ProvenanceUnknown, EvidenceRef: "why/" + k}
	}
	for k, v := range map[string]float64{"cumulative_input_tokens": float64(cum), "cached_input_tokens": 10,
		"output_tokens": 20, "resident_peak_tokens": float64(peak), "wall_seconds": 2.5, "repair_rounds": 1, "principal_reentries": 0} {
		val := v
		m[k] = Measurement{Known: true, Value: &val, Unit: measurementUnits[k], Provenance: ProvenanceMeasured, EvidenceRef: "ev/" + k}
	}
	return m
}

// newFixture builds 5 tasks x 2 repetitions x 2 strategies = 20 completed runs.
func newFixture() *fixture {
	f := &fixture{snaps: map[string]telemetry.RunTelemetrySnapshot{}, store: memStore{}, fals: map[string]*experiments.FalsificationResult{
		"t1": {IsApplicable: true, HypothesisFalsified: false, Reason: "ok"}}}
	ep := endpoint()
	ewp := ClosedEWPBinding{ID: "ewp", Version: 1, RecordDigest: dg("rec"), ContractDigest: dg("con"), BaseCommit: "abc"}
	crit, _ := protocol.Digest(gate.DefaultM4GateCriteria())
	f.plan = CampaignPlan{SchemaVersion: SchemaVersion, CampaignID: "camp", SourceCommit: "src", CorpusDigest: dg("corpus"),
		CriteriaDigest: crit, RequestedTiers: []string{"subscription_cli"},
		Limits: RunLimits{MaxTotalRuns: 120, MaxCallsPerRun: 4, MaxTotalCalls: 480, MaxRunSeconds: 1800, MaxCampaignSeconds: 21600,
			MaxSubscriptionCalls: 480, AllowUnknownSubscriptionQuota: true},
		AllowedSourceClasses: []string{"public"}, AllowedNetworkDomains: []string{}, CredentialRefs: []string{"cred/ref"}}
	ord := 0
	for rep := 1; rep <= 2; rep++ {
		for i := 1; i <= 5; i++ {
			order := []string{StrategyFullHistory, StrategyHybrid4}
			if rep == 2 {
				order = []string{StrategyHybrid4, StrategyFullHistory}
			}
			for _, st := range order {
				ord++
				id := fmt.Sprintf("run-%d-t%d-%s", rep, i, st)
				f.plan.Runs = append(f.plan.Runs, PlannedRun{Ordinal: ord, RunID: id, TaskID: fmt.Sprintf("t%d", i), TaskDigest: dg(fmt.Sprintf("t%d", i)),
					Seed: "seed1", Strategy: st, Repetition: rep, Endpoint: ep, EWP: ewp})
				cum, peak := int64(1000), int64(500)
				if st == StrategyHybrid4 {
					cum, peak = 800, 300
				}
				ev := RunEvidence{RunID: id, TaskID: fmt.Sprintf("t%d", i), TaskDigest: dg(fmt.Sprintf("t%d", i)), Seed: "seed1", Strategy: st,
					Repetition: rep, Endpoint: ep, Status: StatusCompleted, SnapshotRef: "snap/" + id, SessionEvidenceRef: "sess/" + id,
					PromptDigest: dg("prompt" + id), CandidateCommit: "cand" + id, CandidateArtifactRef: "cand/" + id,
					CandidateArtifactDigest: dg("candbytes" + id), VerifierReceiptRef: "rcpt/" + id, VerifierReceiptDigest: dg("rcptbytes" + id),
					InvocationProducerID: "worker-actor", VerifierProducerID: "verifier-actor", Accepted: true, Measurements: measurements(cum, peak)}
				f.runs = append(f.runs, ev)
				f.snaps[id] = telemetry.RunTelemetrySnapshot{RunID: id, TaskID: ev.TaskID, Strategy: st, Capability: ep.CapabilityClass,
					PeakResidentTokens: peak, CachedTokens: 10, OutputTokens: 20, CumulativeInputTokens: cum, Duration: 2500000000,
					DefectSeeded: true, DefectStatus: "detected", Accepted: true, AccountingUncertain: true}
			}
		}
	}
	f.auth = CampaignAuthorization{Version: SchemaVersion, AuthorizedBy: "operator-receipt/1", Expiry: "2030-01-01T00:00:00Z",
		AllowedEndpointBindings: []EndpointBinding{ep}, AllowedSourceClasses: []string{"public"}, AllowedNetworkDomains: []string{},
		CredentialRefs: []string{"cred/ref"}, MaxTotalRuns: 120, MaxCallsPerRun: 4, MaxTotalCalls: 480, MaxRunSeconds: 1800,
		MaxCampaignSeconds: 21600, MaxSubscriptionCalls: 480, AllowUnknownSubscriptionQuota: true, IssuedPolicyDigest: dg("policy")}
	return f
}

func (f *fixture) put(ref string, v any) string {
	b, err := protocol.CanonicalJSON(v)
	if err != nil {
		panic(err)
	}
	f.store[ref] = b
	return bytesDigest(b)
}

// manifest publishes every artifact and returns the post-run manifest.
func (f *fixture) manifest() CampaignManifest {
	pd := f.put("plan", f.plan)
	f.auth.PlanDigest = pd
	if f.planDigestOverride != "" {
		f.auth.PlanDigest = f.planDigestOverride
	}
	ad := f.put("auth", f.auth)
	fd := f.put("fals", f.fals)
	runs := make([]RunEvidence, len(f.runs))
	copy(runs, f.runs)
	for i := range runs {
		r := &runs[i]
		if s, ok := f.snaps[r.RunID]; ok {
			r.SnapshotDigest = f.put(r.SnapshotRef, s)
		}
		if r.Status == StatusCompleted {
			r.CandidateArtifactDigest = bytesDigest(f.putRaw(r.CandidateArtifactRef, "cand:"+r.RunID))
			r.VerifierReceiptDigest = bytesDigest(f.putRaw(r.VerifierReceiptRef, "rcpt:"+r.RunID))
		}
	}
	return CampaignManifest{SchemaVersion: SchemaVersion, CampaignID: f.plan.CampaignID, PlanRef: "plan", PlanDigest: pd,
		AuthorizationRef: "auth", AuthorizationDigest: ad, MissingTiers: f.missing, Runs: runs,
		FalsificationEvidenceRef: "fals", FalsificationEvidenceDigest: fd, RegenerationCommand: []string{"devcadence", "benchmark", "evaluate-gate"}}
}

func (f *fixture) putRaw(ref, s string) []byte {
	f.store[ref] = []byte(s)
	return f.store[ref]
}

// admit runs the internal path with a test authority that grants nothing real.
func (f *fixture) admit(t *testing.T, v IndependentVerifier) AdmissionResult {
	t.Helper()
	res, err := validateAdmission(context.Background(), f.manifest(), f.store, v, allowAuthority{})
	if err != nil {
		t.Fatal(err)
	}
	return res
}
