package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/benchmark"
	"github.com/olostan/DevCadence/internal/benchmark/campaign"
	"github.com/olostan/DevCadence/internal/benchmark/empirical"
	"github.com/olostan/DevCadence/internal/benchmark/empirical/verifier"
	"github.com/olostan/DevCadence/internal/benchmark/experiments"
	"github.com/olostan/DevCadence/internal/benchmark/gate"
	"github.com/olostan/DevCadence/internal/benchmark/telemetry"
	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/operator/receipts"
	"github.com/olostan/DevCadence/internal/protocol"
)

func writeTestReportFile(t *testing.T, path string, s4CatchRate float64, s4Tokens float64, s1Tokens float64, runs int, uncertain bool) {
	t.Helper()
	capTier := string(experiments.CapabilityLocalSmall)
	s4Group := telemetry.AggregatedTelemetry{
		Strategy:                  string(benchmark.StrategyHybrid4Layer),
		Capability:                capTier,
		RunCount:                  runs / 2,
		AcceptedCount:             5,
		FirstPassAcceptanceRate:   1.0,
		DefectCatchRate:           s4CatchRate,
		DefectCatchRateApplicable: true,
		AvgPeakResidentTokens:     500,
		ResourcePerAcceptedResult: telemetry.ResourceEfficiency{
			Value:       s4Tokens,
			IsUndefined: false,
		},
		ContainsUncertainAccounting: uncertain,
	}
	s1Group := telemetry.AggregatedTelemetry{
		Strategy:                  string(benchmark.StrategyFullHistory),
		Capability:                capTier,
		RunCount:                  runs / 2,
		AcceptedCount:             5,
		FirstPassAcceptanceRate:   1.0,
		DefectCatchRate:           0.70,
		DefectCatchRateApplicable: true,
		AvgPeakResidentTokens:     1000,
		ResourcePerAcceptedResult: telemetry.ResourceEfficiency{
			Value:       s1Tokens,
			IsUndefined: false,
		},
		ContainsUncertainAccounting: false,
	}

	report := telemetry.AggregatedReport{
		GeneratedAt:    time.Now().UTC(),
		TotalSnapshots: runs,
		Groups:         []telemetry.AggregatedTelemetry{s4Group, s1Group},
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal report: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
}

func TestCLIBenchmarkEvaluateGate_DecisionGo(t *testing.T) {
	c := newCLI(t)
	tmpDir := t.TempDir()
	reportPath := filepath.Join(tmpDir, "report.json")
	writeTestReportFile(t, reportPath, 0.95, 400.0, 1000.0, 20, false)

	outPath := filepath.Join(tmpDir, "out.md")
	stdout, stderr, err := c.run("benchmark", "evaluate-gate", "--snapshots", reportPath, "--output", outPath)
	if err != nil {
		t.Fatalf("expected exit code 0 for DecisionGo, got err: %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}

	outBytes, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read output report: %v", err)
	}
	content := string(outBytes)
	if !strings.Contains(content, "GATE DECISION: GO") {
		t.Errorf("expected GATE DECISION: GO in output file, got:\n%s", content)
	}
}

func TestCLIBenchmarkEvaluateGate_DecisionRevise(t *testing.T) {
	c := newCLI(t)
	tmpDir := t.TempDir()
	reportPath := filepath.Join(tmpDir, "report.json")
	// s4Tokens = 1500 > s1Tokens = 1000 -> Revise
	writeTestReportFile(t, reportPath, 0.95, 1500.0, 1000.0, 20, false)

	stdout, stderr, err := c.run("benchmark", "evaluate-gate", "--snapshots", reportPath)
	if err == nil {
		t.Fatalf("expected non-zero exit code for DecisionRevise, got success")
	}
	coder, ok := err.(ExitCoder)
	if !ok || coder.ExitCode() != 1 {
		t.Fatalf("expected exit code 1 for DecisionRevise, got %v (err: %v)", coder, err)
	}
	if !strings.Contains(stdout, "GATE DECISION: REVISE") {
		t.Errorf("expected GATE DECISION: REVISE in stdout, got: %s\nstderr: %s", stdout, stderr)
	}
}

func TestCLIBenchmarkEvaluateGate_DecisionInconclusive(t *testing.T) {
	c := newCLI(t)
	tmpDir := t.TempDir()
	reportPath := filepath.Join(tmpDir, "report.json")
	// 4 runs < 10 required -> Inconclusive (ExitCode 2)
	writeTestReportFile(t, reportPath, 0.95, 400.0, 1000.0, 4, false)

	stdout, stderr, err := c.run("benchmark", "evaluate-gate", "--snapshots", reportPath)
	if err == nil {
		t.Fatalf("expected non-zero exit code for DecisionInconclusive, got success")
	}
	coder, ok := err.(ExitCoder)
	if !ok || coder.ExitCode() != 2 {
		t.Fatalf("expected exit code 2 for DecisionInconclusive, got %v (err: %v)", coder, err)
	}
	if !strings.Contains(stdout, "GATE DECISION: INCONCLUSIVE") {
		t.Errorf("expected GATE DECISION: INCONCLUSIVE in stdout, got: %s\nstderr: %s", stdout, stderr)
	}
}

func TestCLIBenchmarkReport(t *testing.T) {
	c := newCLI(t)
	tmpDir := t.TempDir()
	reportPath := filepath.Join(tmpDir, "report.json")
	writeTestReportFile(t, reportPath, 0.95, 400.0, 1000.0, 20, false)

	outPath := filepath.Join(tmpDir, "report.md")
	stdout, stderr, err := c.run("benchmark", "report", "--summary", reportPath, "--output", outPath)
	if err != nil {
		t.Fatalf("expected report command to succeed, got: %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read generated report: %v", err)
	}
	if !strings.Contains(string(data), "# Empirical Benchmark Telemetry Report") {
		t.Errorf("expected telemetry markdown header, got: %s", string(data))
	}
}

func TestCLIBenchmarkGate_JSON(t *testing.T) {
	c := newCLI(t)
	tmpDir := t.TempDir()
	reportPath := filepath.Join(tmpDir, "report.json")
	writeTestReportFile(t, reportPath, 0.95, 400.0, 1000.0, 20, false)

	stdout, stderr, err := c.run("benchmark", "gate", "--summary", reportPath, "--json")
	if err != nil {
		t.Fatalf("expected gate command to succeed, got: %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}

	var res gate.GateEvaluationResult
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("expected valid JSON evaluation result, got unmarshal error: %v\nstdout: %s", err, stdout)
	}
	if res.Decision != gate.DecisionGo {
		t.Errorf("expected DecisionGo in JSON result, got %v", res.Decision)
	}
}

func TestCLIBenchmark_SnapshotArrayAndCampaignSummary(t *testing.T) {
	c := newCLI(t)
	tmpDir := t.TempDir()

	// 1. Raw snapshots array
	snaps := []telemetry.RunTelemetrySnapshot{
		{
			RunID:                 "snap-1",
			TaskID:                "t1",
			Strategy:              string(benchmark.StrategyHybrid4Layer),
			Capability:            string(experiments.CapabilityLocalSmall),
			Accepted:              true,
			CumulativeInputTokens: 300,
		},
		{
			RunID:                 "snap-2",
			TaskID:                "t1",
			Strategy:              string(benchmark.StrategyFullHistory),
			Capability:            string(experiments.CapabilityLocalSmall),
			Accepted:              true,
			CumulativeInputTokens: 1000,
		},
	}
	snapsPath := filepath.Join(tmpDir, "snapshots.json")
	snapBytes, _ := json.Marshal(snaps)
	_ = os.WriteFile(snapsPath, snapBytes, 0o644)

	// Report JSON format
	reportJSONPath := filepath.Join(tmpDir, "report.json")
	_, _, err := c.run("benchmark", "report", "--summary", snapsPath, "--json", "--output", reportJSONPath)
	if err != nil {
		t.Fatalf("expected report JSON to succeed, got: %v", err)
	}

	// 2. Custom criteria flag
	crit := gate.DefaultM4GateCriteria()
	crit.MinCompletedRuns = 2
	critPath := filepath.Join(tmpDir, "criteria.json")
	critBytes, _ := json.Marshal(crit)
	_ = os.WriteFile(critPath, critBytes, 0o644)

	// Snapshots carry no defect-catch or peak-resident data, so the gate fails closed (revise,
	// exit 1) rather than inconclusive (exit 2): custom MinCompletedRuns=2 was honored.
	_, _, err = c.run("benchmark", "evaluate-gate", "--snapshots", snapsPath, "--criteria", critPath)
	var ge *gateExitError
	if !errors.As(err, &ge) || ge.ExitCode() != 1 {
		t.Fatalf("expected fail-closed revise exit (1) with custom criteria, got: %v", err)
	}

	// 3. Invalid / missing flags error cases
	if _, _, err := c.run("benchmark"); err == nil {
		t.Errorf("expected error when no subcommand provided")
	}
	if _, _, err := c.run("benchmark", "unknown"); err == nil {
		t.Errorf("expected error for unknown subcommand")
	}
	if _, _, err := c.run("benchmark", "evaluate-gate"); err == nil {
		t.Errorf("expected error when missing required flag")
	}
	if _, _, err := c.run("benchmark", "report"); err == nil {
		t.Errorf("expected error when missing required flag")
	}
}

func TestCLIBenchmarkEvaluateGate_PrintsProvenance(t *testing.T) {
	c := newCLI(t)
	tmpDir := t.TempDir()
	reportPath := filepath.Join(tmpDir, "report.json")
	writeTestReportFile(t, reportPath, 0.95, 400.0, 1000.0, 20, false)
	outPath := filepath.Join(tmpDir, "out.md")
	stdout, _, err := c.run("benchmark", "evaluate-gate", "--snapshots", reportPath, "--output", outPath,
		"--evidence-kind", "synthetic_harness_validation", "--driver", "scripted", "--source-commit", "abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "evidence_kind=synthetic_harness_validation") || !strings.Contains(stdout, "NOT empirical proof") {
		t.Errorf("provenance not printed: %q", stdout)
	}
	if _, _, err := c.run("benchmark", "evaluate-gate", "--snapshots", reportPath, "--evidence-kind", "bogus", "--driver", "x"); err == nil {
		t.Error("unknown evidence kind must fail")
	}
}

func TestCLIBenchmarkEvaluateGate_LegacyRefusal(t *testing.T) {
	c := newCLI(t)
	_, _, err := c.run("benchmark", "evaluate-gate", "--evidence-kind", "empirical_campaign")
	if err == nil {
		t.Fatal("expected evaluate-gate with evidence-kind empirical_campaign to fail")
	}
	if code := exitCode(err); code != 4 {
		t.Fatalf("expected exit code 4, got %d (err: %v)", code, err)
	}
	if !strings.Contains(err.Error(), "empirical campaign evidence must be admitted through 'benchmark replay-empirical'") {
		t.Fatalf("expected legacy refusal message, got: %v", err)
	}
}

type testCountingVerifier struct {
	calls int
	mut   func(*empirical.VerifiedOutcome, empirical.RunEvidence)
	err   error
}

func (v *testCountingVerifier) Verify(_ context.Context, plan empirical.CampaignPlan, _ empirical.PlannedRun, ev empirical.RunEvidence, _ empirical.ArtifactResolver) (empirical.VerifiedOutcome, error) {
	v.calls++
	if v.err != nil {
		return empirical.VerifiedOutcome{}, v.err
	}
	pd, _ := empirical.PlanDigest(plan)
	o := empirical.VerifiedOutcome{
		RunID:                       ev.RunID,
		PlanDigest:                  pd,
		SessionDigest:               ev.SessionEvidenceDigest,
		CandidateDigest:             ev.CandidateArtifactDigest,
		SnapshotDigest:              ev.SnapshotDigest,
		ReceiptDigest:               ev.VerifierReceiptDigest,
		VerifierSourceCommit:        plan.VerifierSourceCommit,
		VerificationProfileDigest:   plan.VerificationProfileDigest,
		Worker:                      protocol.ActorProvenance{ActorID: ev.InvocationProducerID, InvocationID: "inv-w-" + ev.RunID, Role: protocol.ProvenanceRoleImplementer},
		Verifier:                    protocol.ActorProvenance{ActorID: ev.VerifierProducerID, InvocationID: "inv-v-" + ev.RunID, Role: protocol.ProvenanceRoleVerifier},
		QualityVerdict:              empirical.QualityAccepted,
		SeededDefectTotal:           1,
		SeededDefectsCaught:         1,
		VerifiedCommandArtifactRefs: []string{"cmd/" + ev.RunID},
	}
	if v.mut != nil {
		v.mut(&o, ev)
	}
	return o, nil
}

type empiricalFixture struct {
	plan    empirical.CampaignPlan
	auth    empirical.CampaignAuthorization
	runs    []empirical.RunEvidence
	snaps   map[string]telemetry.RunTelemetrySnapshot
	fals    map[string]*experiments.FalsificationResult
	store   map[string][]byte
	privKey ed25519.PrivateKey
	pubKey  ed25519.PublicKey
	anchor  receipts.TrustAnchor
}

func dg(s string) string {
	b, _ := protocol.CanonicalJSON(s)
	return protocol.DigestBytes(b)
}

func testMeasurements(cum, peak int64, known bool) map[string]empirical.Measurement {
	units := map[string]string{
		"cumulative_input_tokens": "token",
		"cached_input_tokens":     "token",
		"output_tokens":           "token",
		"resident_peak_tokens":    "token",
		"api_spend_usd":           "USD",
		"subscription_quota":      "provider_unit",
		"local_compute_seconds":   "second",
		"wall_seconds":            "second",
		"repair_rounds":           "count",
		"principal_reentries":     "count",
	}
	m := map[string]empirical.Measurement{}
	for k, u := range units {
		m[k] = empirical.Measurement{Unit: u, Provenance: empirical.ProvenanceUnknown, EvidenceRef: "why/" + k}
	}
	for k, v := range map[string]float64{
		"cumulative_input_tokens": float64(cum),
		"cached_input_tokens":     10,
		"output_tokens":           20,
		"resident_peak_tokens":    float64(peak),
		"wall_seconds":            2.5,
		"repair_rounds":           1,
		"principal_reentries":     0,
	} {
		val := v
		km := known
		prov := empirical.ProvenanceMeasured
		if !km {
			prov = empirical.ProvenanceUnknown
		}
		m[k] = empirical.Measurement{Known: km, Value: &val, Unit: units[k], Provenance: prov, EvidenceRef: "ev/" + k}
	}
	return m
}

func newEmpiricalFixture() *empiricalFixture {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	anchor := receipts.TrustAnchor{
		AnchorID:     "anchor-1",
		HumanActorID: "operator-1",
		PublicKey:    pub,
		NotBefore:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
		Purposes:     []receipts.Purpose{receipts.PurposeEmpiricalCampaignAuthorize},
	}

	f := &empiricalFixture{
		snaps: map[string]telemetry.RunTelemetrySnapshot{},
		store: map[string][]byte{},
		fals: map[string]*experiments.FalsificationResult{
			"t1": {IsApplicable: true, HypothesisFalsified: false, Reason: "ok"},
		},
		privKey: priv,
		pubKey:  pub,
		anchor:  anchor,
	}

	ep := empirical.EndpointBinding{
		EndpointID:            "ep-sub",
		DriverID:              "claude-cli",
		ModelID:               "model-a",
		ModelRevision:         "r1",
		ChannelID:             "ch1",
		CapabilityClass:       "subscription_cli",
		RuntimeVersion:        "1.0",
		ContextProfileDigest:  dg("ctx"),
		PolicyDigest:          dg("pol"),
		SubscriptionQuotaUnit: empirical.UnknownQuotaUnit,
	}
	ewp := empirical.ClosedEWPBinding{
		ID:             "ewp",
		Version:        1,
		RecordDigest:   dg("rec"),
		ContractDigest: dg("con"),
		BaseCommit:     "abc",
	}
	crit, _ := protocol.Digest(gate.DefaultM4GateCriteria())
	f.plan = empirical.CampaignPlan{
		SchemaVersion:             empirical.SchemaVersion,
		CampaignID:                "camp",
		SourceCommit:              "src",
		CorpusDigest:              dg("corpus"),
		CriteriaDigest:            crit,
		RequestedTiers:            []string{"subscription_cli"},
		VerifierSourceCommit:      "verifiersrc",
		VerificationProfileDigest: dg("profile"),
		Limits: empirical.RunLimits{
			MaxTotalRuns:                  120,
			MaxCallsPerRun:                4,
			MaxTotalCalls:                 480,
			MaxRunSeconds:                 1800,
			MaxCampaignSeconds:            21600,
			MaxSubscriptionCalls:          480,
			AllowUnknownSubscriptionQuota: true,
		},
		AllowedSourceClasses:  []string{"public"},
		AllowedNetworkDomains: []string{},
		CredentialRefs:        []string{"cred/ref"},
	}

	ord := 0
	for rep := 1; rep <= 2; rep++ {
		for i := 1; i <= 5; i++ {
			order := []string{empirical.StrategyFullHistory, empirical.StrategyHybrid4}
			if rep == 2 {
				order = []string{empirical.StrategyHybrid4, empirical.StrategyFullHistory}
			}
			for _, st := range order {
				ord++
				id := fmt.Sprintf("run-%d-t%d-%s", rep, i, st)
				f.plan.Runs = append(f.plan.Runs, empirical.PlannedRun{
					Ordinal:    ord,
					RunID:      id,
					TaskID:     fmt.Sprintf("t%d", i),
					TaskDigest: dg(fmt.Sprintf("t%d", i)),
					Seed:       "seed1",
					Strategy:   st,
					Repetition: rep,
					Endpoint:   ep,
					EWP:        ewp,
				})
				cum, peak := int64(1000), int64(500)
				if st == empirical.StrategyHybrid4 {
					cum, peak = 800, 300
				}
				ev := empirical.RunEvidence{
					RunID:                   id,
					TaskID:                  fmt.Sprintf("t%d", i),
					TaskDigest:              dg(fmt.Sprintf("t%d", i)),
					Seed:                    "seed1",
					Strategy:                st,
					Repetition:              rep,
					Endpoint:                ep,
					Status:                  empirical.StatusCompleted,
					SnapshotRef:             "snap/" + id,
					SessionEvidenceRef:      "sess/" + id,
					PromptDigest:            dg("prompt" + id),
					CandidateCommit:         "cand" + id,
					CandidateArtifactRef:    "cand/" + id,
					CandidateArtifactDigest: dg("candbytes" + id),
					VerifierReceiptRef:      "rcpt/" + id,
					VerifierReceiptDigest:   dg("rcptbytes" + id),
					InvocationProducerID:    "worker-actor",
					VerifierProducerID:      "verifier-actor",
					Accepted:                true,
					Measurements:            testMeasurements(cum, peak, true),
				}
				f.runs = append(f.runs, ev)
				f.snaps[id] = telemetry.RunTelemetrySnapshot{
					RunID:                 id,
					TaskID:                ev.TaskID,
					Strategy:              st,
					Capability:            ep.CapabilityClass,
					PeakResidentTokens:    peak,
					CachedTokens:          10,
					OutputTokens:          20,
					CumulativeInputTokens: cum,
					Duration:              2500000000,
					DefectSeeded:          true,
					DefectStatus:          "detected",
					Accepted:              true,
					AccountingUncertain:   true,
				}
			}
		}
	}

	f.auth = empirical.CampaignAuthorization{
		Version:                       empirical.SchemaVersion,
		AuthorizedBy:                  "operator-receipt/1",
		Expiry:                        time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339),
		AllowedEndpointBindings:       []empirical.EndpointBinding{ep},
		AllowedSourceClasses:          []string{"public"},
		AllowedNetworkDomains:         []string{},
		CredentialRefs:                []string{"cred/ref"},
		MaxTotalRuns:                  120,
		MaxCallsPerRun:                4,
		MaxTotalCalls:                 480,
		MaxRunSeconds:                 1800,
		MaxCampaignSeconds:            21600,
		MaxSubscriptionCalls:          480,
		AllowUnknownSubscriptionQuota: true,
		IssuedPolicyDigest:            dg("policy"),
	}
	return f
}

func (f *empiricalFixture) put(ref string, v any) string {
	b, err := protocol.CanonicalJSON(v)
	if err != nil {
		panic(err)
	}
	f.store[ref] = b
	return protocol.DigestBytes(b)
}

func (f *empiricalFixture) putRaw(ref, s string) []byte {
	f.store[ref] = []byte(s)
	return f.store[ref]
}

func (f *empiricalFixture) manifest() (empirical.CampaignManifest, []byte, []byte, string, string) {
	planBytes, err := protocol.CanonicalJSON(f.plan)
	if err != nil {
		panic(err)
	}
	pd := protocol.DigestBytes(planBytes)
	f.store["plan"] = planBytes

	f.auth.PlanDigest = pd
	authBytes, err := protocol.CanonicalJSON(f.auth)
	if err != nil {
		panic(err)
	}
	ad := protocol.DigestBytes(authBytes)
	f.store["auth"] = authBytes

	fd := f.put("fals", f.fals)
	runs := make([]empirical.RunEvidence, len(f.runs))
	copy(runs, f.runs)
	for i := range runs {
		r := &runs[i]
		if s, ok := f.snaps[r.RunID]; ok {
			r.SnapshotDigest = f.put(r.SnapshotRef, s)
		}
		if r.Status == empirical.StatusCompleted {
			r.CandidateArtifactDigest = protocol.DigestBytes(f.putRaw(r.CandidateArtifactRef, "cand:"+r.RunID))
			r.VerifierReceiptDigest = protocol.DigestBytes(f.putRaw(r.VerifierReceiptRef, "rcpt:"+r.RunID))
			sess := empirical.SessionEvidence{
				SchemaVersion:              empirical.SchemaVersion,
				RunID:                      r.RunID,
				CampaignID:                 f.plan.CampaignID,
				AttemptID:                  "att-" + r.RunID,
				TaskDigest:                 r.TaskDigest,
				Endpoint:                   r.Endpoint,
				PromptDigest:               r.PromptDigest,
				ContextManifestDigest:      dg("manifest"),
				InvocationProvenanceDigest: dg("provenance"),
				ExecutionPolicyDigest:      dg("policy"),
				AuthorizationDigest:        ad,
				StartedAt:                  time.Now().UTC().Add(-30 * time.Minute),
				FinishedAt:                 time.Now().UTC().Add(-25 * time.Minute),
				DriverOutcome:              empirical.StatusCompleted,
				Turns:                      1,
				Usage:                      r.Measurements,
			}
			r.SessionEvidenceDigest = f.put(r.SessionEvidenceRef, sess)
		}
	}
	m := empirical.CampaignManifest{
		SchemaVersion:               empirical.SchemaVersion,
		CampaignID:                  f.plan.CampaignID,
		PlanRef:                     "plan",
		PlanDigest:                  pd,
		AuthorizationRef:            "auth",
		AuthorizationDigest:         ad,
		Runs:                        runs,
		FalsificationEvidenceRef:    "fals",
		FalsificationEvidenceDigest: fd,
		RegenerationCommand:         []string{"devcadence", "benchmark", "evaluate-gate"},
	}
	return m, planBytes, authBytes, pd, ad
}

func setupEmpiricalFiles(t *testing.T, f *empiricalFixture) (manifestPath, planPath, authPath, artifactsDir, criteriaPath string) {
	t.Helper()
	tmpDir := t.TempDir()
	artifactsDir = filepath.Join(tmpDir, "artifacts")
	if err := os.MkdirAll(artifactsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll artifacts: %v", err)
	}

	manifest, planBytes, authBytes, pd, ad := f.manifest()

	manifestPath = filepath.Join(tmpDir, "manifest.json")
	mBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, mBytes, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	planPath = filepath.Join(tmpDir, "plan.json")
	if err := os.WriteFile(planPath, planBytes, 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	authPath = filepath.Join(tmpDir, "auth.json")
	if err := os.WriteFile(authPath, authBytes, 0o644); err != nil {
		t.Fatalf("write auth: %v", err)
	}

	crit := gate.DefaultM4GateCriteria()
	critBytes, err := json.MarshalIndent(crit, "", "  ")
	if err != nil {
		t.Fatalf("marshal criteria: %v", err)
	}
	criteriaPath = filepath.Join(tmpDir, "criteria.json")
	if err := os.WriteFile(criteriaPath, critBytes, 0o644); err != nil {
		t.Fatalf("write criteria: %v", err)
	}

	// Write all store files into artifactsDir
	for ref, data := range f.store {
		target := filepath.Join(artifactsDir, ref)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatalf("MkdirAll for store ref: %v", err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			t.Fatalf("write store ref: %v", err)
		}
	}

	// Write trust anchor to artifactsDir/anchors.json
	anchorsDoc := struct {
		Version string                 `json:"version"`
		Anchors []receipts.TrustAnchor `json:"anchors"`
	}{
		Version: "1.0",
		Anchors: []receipts.TrustAnchor{f.anchor},
	}
	anchorsBytes, err := json.MarshalIndent(anchorsDoc, "", "  ")
	if err != nil {
		t.Fatalf("marshal anchors: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactsDir, "anchors.json"), anchorsBytes, 0o644); err != nil {
		t.Fatalf("write anchors.json: %v", err)
	}

	// Sign and write operator receipt to artifactsDir/receipts/auth.json
	now := time.Now().UTC()
	text := "authorization receipt"
	stmt := receipts.Statement{
		Version:      "1.0",
		ReceiptID:    "rcpt_00000000000000000000000001",
		AnchorID:     f.anchor.AnchorID,
		HumanActorID: f.anchor.HumanActorID,
		IssuedAt:     now.Add(-2 * time.Hour),
		NotAfter:     now.Add(48 * time.Hour),
		Purpose:      receipts.PurposeEmpiricalCampaignAuthorize,
		Use:          receipts.UseGrant,
		ProjectID:    "devcadence",
		Subject: receipts.Subject{
			Kind:    "CampaignAuthorization",
			ID:      pd,
			Version: 1,
		},
		SubjectDigest: ad,
		Text:          text,
		TextDigest:    protocol.DigestBytes([]byte(text)),
	}
	rcpt, err := stmt.Sign(f.privKey)
	if err != nil {
		t.Fatalf("sign statement: %v", err)
	}
	rcptBytes, err := json.MarshalIndent(rcpt, "", "  ")
	if err != nil {
		t.Fatalf("marshal receipt: %v", err)
	}
	rcptDir := filepath.Join(artifactsDir, "receipts")
	if err := os.MkdirAll(rcptDir, 0o755); err != nil {
		t.Fatalf("MkdirAll receipts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rcptDir, "auth_rcpt.json"), rcptBytes, 0o644); err != nil {
		t.Fatalf("write receipt: %v", err)
	}

	origLoadOpVerifier := loadOperatorVerifier
	t.Cleanup(func() { loadOperatorVerifier = origLoadOpVerifier })
	inMemVerifier, err := receipts.NewVerifierWithReceipts(
		[]receipts.TrustAnchor{f.anchor},
		[]receipts.Receipt{rcpt},
		receipts.WithClock(clock.System()),
	)
	if err != nil {
		t.Fatalf("NewVerifierWithReceipts: %v", err)
	}
	loadOperatorVerifier = func(operatorDir, artifactsDir, homeDir string) (receipts.Verifier, error) {
		if operatorDir != "" {
			var revList *receipts.RevocationList
			revPath := filepath.Join(operatorDir, "revoked.json")
			if revBytes, err := os.ReadFile(revPath); err == nil {
				var rd struct {
					ReceiptIDs []string `json:"receipt_ids"`
				}
				if json.Unmarshal(revBytes, &rd) == nil && len(rd.ReceiptIDs) > 0 {
					revMap := make(map[string]time.Time, len(rd.ReceiptIDs))
					for _, rid := range rd.ReceiptIDs {
						revMap[rid] = now
					}
					revList = &receipts.RevocationList{
						RevokedIDs: revMap,
						RevokedAt:  now,
					}
				}
			}
			return receipts.NewVerifierWithReceipts(
				[]receipts.TrustAnchor{f.anchor},
				[]receipts.Receipt{rcpt},
				receipts.WithRevocationList(revList),
				receipts.WithClock(clock.System()),
			)
		}
		return inMemVerifier, nil
	}

	return manifestPath, planPath, authPath, artifactsDir, criteriaPath
}

func TestCLIBenchmarkReplayEmpirical_MissingFlags(t *testing.T) {
	c := newCLI(t)
	// Missing --manifest
	_, _, err := c.run("benchmark", "replay-empirical")
	if err == nil {
		t.Fatal("expected error for missing flags")
	}
	if code := exitCode(err); code != 2 {
		t.Fatalf("expected exit code 2, got %d (err: %v)", code, err)
	}

	// Missing --plan
	_, _, err = c.run("benchmark", "replay-empirical", "--manifest", "m.json")
	if err == nil {
		t.Fatal("expected error for missing --plan")
	}
	if code := exitCode(err); code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}

	// Missing --authorization
	_, _, err = c.run("benchmark", "replay-empirical", "--manifest", "m.json", "--plan", "p.json")
	if err == nil {
		t.Fatal("expected error for missing --authorization")
	}
	if code := exitCode(err); code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}

	// Missing --artifacts
	_, _, err = c.run("benchmark", "replay-empirical", "--manifest", "m.json", "--plan", "p.json", "--authorization", "a.json")
	if err == nil {
		t.Fatal("expected error for missing --artifacts")
	}
	if code := exitCode(err); code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}

	// Missing --criteria
	_, _, err = c.run("benchmark", "replay-empirical", "--manifest", "m.json", "--plan", "p.json", "--authorization", "a.json", "--artifacts", "dir")
	if err == nil {
		t.Fatal("expected error for missing --criteria")
	}
	if code := exitCode(err); code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
}

func TestCLIBenchmarkReplayEmpirical_InvalidFiles(t *testing.T) {
	c := newCLI(t)
	tmpDir := t.TempDir()

	validCriteria := filepath.Join(tmpDir, "crit.json")
	_ = os.WriteFile(validCriteria, []byte("{}"), 0o644)
	validArtDir := filepath.Join(tmpDir, "art")
	_ = os.MkdirAll(validArtDir, 0o755)

	// Non-existent manifest
	_, _, err := c.run("benchmark", "replay-empirical",
		"--manifest", filepath.Join(tmpDir, "missing.json"),
		"--plan", filepath.Join(tmpDir, "p.json"),
		"--authorization", filepath.Join(tmpDir, "a.json"),
		"--artifacts", validArtDir,
		"--criteria", validCriteria)
	if err == nil {
		t.Fatal("expected error for missing manifest file")
	}
	if code := exitCode(err); code != 3 {
		t.Fatalf("expected exit code 3 (not found) for missing file, got %d", code)
	}

	// Non-directory artifacts
	badArtFile := filepath.Join(tmpDir, "not-a-dir")
	_ = os.WriteFile(badArtFile, []byte("x"), 0o644)
	existingFile := filepath.Join(tmpDir, "exists.json")
	_ = os.WriteFile(existingFile, []byte(`{"schema_version":"1.0"}`), 0o644)

	_, _, err = c.run("benchmark", "replay-empirical",
		"--manifest", existingFile,
		"--plan", existingFile,
		"--authorization", existingFile,
		"--artifacts", badArtFile,
		"--criteria", validCriteria)
	if err == nil {
		t.Fatal("expected error when artifacts is not a directory")
	}
	if code := exitCode(err); code != 2 {
		t.Fatalf("expected exit code 2 (invalid argument) for non-dir artifacts, got %d", code)
	}

	// Malformed JSON manifest
	badJSON := filepath.Join(tmpDir, "bad.json")
	_ = os.WriteFile(badJSON, []byte("{bad-json"), 0o644)

	_, _, err = c.run("benchmark", "replay-empirical",
		"--manifest", badJSON,
		"--plan", existingFile,
		"--authorization", existingFile,
		"--artifacts", validArtDir,
		"--criteria", validCriteria)
	if err == nil {
		t.Fatal("expected error for malformed json")
	}
	if code := exitCode(err); code != 2 {
		t.Fatalf("expected exit code 2 for malformed json, got %d", code)
	}
}

func TestCLIBenchmarkReplayEmpirical_DefaultVerifier_Refusal_Exit3(t *testing.T) {
	c := newCLI(t)
	f := newEmpiricalFixture()
	mPath, pPath, aPath, artDir, cPath := setupEmpiricalFiles(t, f)

	// In go test, the default verifier cannot determine VCS revision and refuses admission -> exit 3
	_, stderr, err := c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", pPath,
		"--authorization", aPath,
		"--artifacts", artDir,
		"--criteria", cPath)
	if err == nil {
		t.Fatal("expected admission refusal without verified build info")
	}
	if code := exitCode(err); code != 3 {
		t.Fatalf("expected exit code 3 for refused admission, got %d (err: %v)\nstderr: %s", code, err, stderr)
	}
	if !strings.Contains(stderr, "empirical admission refused") {
		t.Fatalf("expected refusal notification in stderr, got: %s", stderr)
	}
}

func TestCLIBenchmarkReplayEmpirical_AdmissionRefused_Expired_Exit3(t *testing.T) {
	c := newCLI(t)
	f := newEmpiricalFixture()
	f.auth.Expiry = "2020-01-01T00:00:00Z" // expired authorization
	mPath, pPath, aPath, artDir, cPath := setupEmpiricalFiles(t, f)

	// Save and restore newIndependentVerifier seam
	origVerifier := newIndependentVerifier
	newIndependentVerifier = func(ctx context.Context, e *env, resolver empirical.ArtifactResolver, repoDir string) (empirical.IndependentVerifier, error) {
		return &testCountingVerifier{}, nil
	}
	defer func() { newIndependentVerifier = origVerifier }()

	_, stderr, err := c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", pPath,
		"--authorization", aPath,
		"--artifacts", artDir,
		"--criteria", cPath)
	if err == nil {
		t.Fatal("expected admission refusal for expired authorization")
	}
	if code := exitCode(err); code != 3 {
		t.Fatalf("expected exit code 3 for refused admission, got %d (err: %v)\nstderr: %s", code, err, stderr)
	}
	if !strings.Contains(stderr, "AUTHORIZATION_INVALID") && !strings.Contains(stderr, "OPERATOR_AUTHORITY_UNAVAILABLE") {
		t.Fatalf("expected authorization failure reason code in stderr, got: %s", stderr)
	}
}

func TestCLIBenchmarkReplayEmpirical_Pass_Exit0(t *testing.T) {
	c := newCLI(t)
	f := newEmpiricalFixture()
	mPath, pPath, aPath, artDir, cPath := setupEmpiricalFiles(t, f)

	origVerifier := newIndependentVerifier
	cv := &testCountingVerifier{}
	newIndependentVerifier = func(ctx context.Context, e *env, resolver empirical.ArtifactResolver, repoDir string) (empirical.IndependentVerifier, error) {
		return cv, nil
	}
	defer func() { newIndependentVerifier = origVerifier }()

	// 1. Stdout output
	stdout, stderr, err := c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", pPath,
		"--authorization", aPath,
		"--artifacts", artDir,
		"--criteria", cPath)
	if err != nil {
		t.Fatalf("expected exit code 0 for DecisionGo, got err: %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	if !strings.Contains(stdout, "**Empirical Conclusion:** GO") {
		t.Errorf("expected **Empirical Conclusion:** GO in stdout, got:\n%s", stdout)
	}
	if cv.calls != 20 {
		t.Errorf("expected 20 verifier calls, got %d", cv.calls)
	}

	// 2. Output file
	tmpDir := t.TempDir()
	outMD := filepath.Join(tmpDir, "report.md")
	_, _, err = c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", pPath,
		"--authorization", aPath,
		"--artifacts", artDir,
		"--criteria", cPath,
		"--output", outMD)
	if err != nil {
		t.Fatalf("expected success with --output, got: %v", err)
	}
	mdContent, err := os.ReadFile(outMD)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if !strings.Contains(string(mdContent), "**Empirical Conclusion:** GO") {
		t.Errorf("expected **Empirical Conclusion:** GO in report.md, got:\n%s", string(mdContent))
	}

	// 3. JSON output
	outJSON := filepath.Join(tmpDir, "report.json")
	stdoutJSON, _, err := c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", pPath,
		"--authorization", aPath,
		"--artifacts", artDir,
		"--criteria", cPath,
		"--json",
		"--output", outJSON)
	if err != nil {
		t.Fatalf("expected success with --json, got: %v", err)
	}
	jsonBytes, err := os.ReadFile(outJSON)
	if err != nil {
		t.Fatalf("read json file: %v", err)
	}
	var rep empirical.Report
	if err := json.Unmarshal(jsonBytes, &rep); err != nil {
		t.Fatalf("unmarshal json report: %v\ncontent: %s", err, string(jsonBytes))
	}
	if rep.Conclusion != empirical.ConclusionGo {
		t.Errorf("expected conclusion go, got %q", rep.Conclusion)
	}
	_ = stdoutJSON
}

func TestCLIBenchmarkReplayEmpirical_Fail_Exit1(t *testing.T) {
	c := newCLI(t)
	f := newEmpiricalFixture()
	// Mutate Hybrid4 to use more tokens than FullHistory (1500 > 1000) -> DecisionRevise
	for i := range f.runs {
		if f.runs[i].Strategy == empirical.StrategyHybrid4 {
			f.runs[i].Measurements = testMeasurements(1500, 1200, true)
			id := f.runs[i].RunID
			s := f.snaps[id]
			s.CumulativeInputTokens = 1500
			s.PeakResidentTokens = 1200
			f.snaps[id] = s
		}
	}
	mPath, pPath, aPath, artDir, cPath := setupEmpiricalFiles(t, f)

	origVerifier := newIndependentVerifier
	newIndependentVerifier = func(ctx context.Context, e *env, resolver empirical.ArtifactResolver, repoDir string) (empirical.IndependentVerifier, error) {
		return &testCountingVerifier{}, nil
	}
	defer func() { newIndependentVerifier = origVerifier }()

	stdout, stderr, err := c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", pPath,
		"--authorization", aPath,
		"--artifacts", artDir,
		"--criteria", cPath)
	if err == nil {
		t.Fatal("expected exit code 1 for DecisionRevise, got success")
	}
	if code := exitCode(err); code != 1 {
		t.Fatalf("expected exit code 1 for DecisionRevise, got %d (err: %v)\nstderr: %s\nstdout: %s", code, err, stderr, stdout)
	}
	if !strings.Contains(stdout, "**Empirical Conclusion:** REVISE") {
		t.Errorf("expected REVISE in stdout, got:\n%s", stdout)
	}
}

func TestCLIBenchmarkReplayEmpirical_Indeterminate_Exit2(t *testing.T) {
	c := newCLI(t)
	f := newEmpiricalFixture()
	// Mark falsification as not applicable -> ConclusionInconclusive
	f.fals["t1"] = &experiments.FalsificationResult{IsApplicable: false}
	mPath, pPath, aPath, artDir, cPath := setupEmpiricalFiles(t, f)

	origVerifier := newIndependentVerifier
	newIndependentVerifier = func(ctx context.Context, e *env, resolver empirical.ArtifactResolver, repoDir string) (empirical.IndependentVerifier, error) {
		return &testCountingVerifier{}, nil
	}
	defer func() { newIndependentVerifier = origVerifier }()

	stdout, stderr, err := c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", pPath,
		"--authorization", aPath,
		"--artifacts", artDir,
		"--criteria", cPath)
	if err == nil {
		t.Fatal("expected exit code 2 for DecisionInconclusive, got success")
	}
	if code := exitCode(err); code != 2 {
		t.Fatalf("expected exit code 2 for DecisionInconclusive, got %d (err: %v)\nstderr: %s\nstdout: %s", code, err, stderr, stdout)
	}
	if !strings.Contains(stdout, "**Empirical Conclusion:** INCONCLUSIVE") {
		t.Errorf("expected INCONCLUSIVE in stdout, got:\n%s", stdout)
	}
}

func TestDirectoryArtifactResolver(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte("hello artifact world")
	digest := protocol.DigestBytes(content)

	extra := map[string][]byte{
		"extra-ref": content,
	}
	r := newDirectoryArtifactResolver(tmpDir, extra)

	ctx := context.Background()

	// 1. Extra map hit
	b, err := r.ReadVerified(ctx, "extra-ref", digest)
	if err != nil || string(b) != string(content) {
		t.Fatalf("expected extra hit: %v, %s", err, string(b))
	}

	// 2. Direct file
	artPath := filepath.Join(tmpDir, "file1.txt")
	_ = os.WriteFile(artPath, content, 0o644)
	b, err = r.ReadVerified(ctx, "file1.txt", digest)
	if err != nil || string(b) != string(content) {
		t.Fatalf("expected direct file read: %v, %s", err, string(b))
	}

	// 3. File named by digest
	digestPath := filepath.Join(tmpDir, strings.TrimPrefix(digest, "sha256:"))
	_ = os.WriteFile(digestPath, content, 0o644)
	b, err = r.ReadVerified(ctx, "arbitrary-ref", digest)
	if err != nil || string(b) != string(content) {
		t.Fatalf("expected digest lookup: %v, %s", err, string(b))
	}

	// 4. Traversal prevented
	_, err = r.ReadVerified(ctx, "../../../etc/passwd", "")
	if err == nil {
		t.Fatal("expected path traversal to fail")
	}

	// 5. Not found
	_, err = r.ReadVerified(ctx, "nonexistent", dg("missing"))
	if err == nil {
		t.Fatal("expected nonexistent artifact to fail")
	}

	// 6. Context cancellation
	cancCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = r.ReadVerified(cancCtx, "extra-ref", digest)
	if err == nil {
		t.Fatal("expected error on canceled context")
	}

	// 7. Extra map digest hit
	extra2 := map[string][]byte{
		digest: content,
	}
	r2 := newDirectoryArtifactResolver(tmpDir, extra2)
	b, err = r2.ReadVerified(ctx, "unmatched-ref", digest)
	if err != nil || string(b) != string(content) {
		t.Fatalf("expected extra digest hit: %v, %s", err, string(b))
	}

	// 8. Ref with sha256: prefix
	shaContent := []byte("sha prefix content")
	shaDigest := protocol.DigestBytes(shaContent)
	trimmedSha := strings.TrimPrefix(shaDigest, "sha256:")
	_ = os.WriteFile(filepath.Join(tmpDir, trimmedSha), shaContent, 0o644)
	b, err = r.ReadVerified(ctx, shaDigest, shaDigest)
	if err != nil || string(b) != string(shaContent) {
		t.Fatalf("expected sha prefix match: %v, %s", err, string(b))
	}

	// 9. Digest is empty
	plainContent := []byte("plain file without digest")
	_ = os.WriteFile(filepath.Join(tmpDir, "plain.txt"), plainContent, 0o644)
	b, err = r.ReadVerified(ctx, "plain.txt", "")
	if err != nil || string(b) != string(plainContent) {
		t.Fatalf("expected empty digest match: %v, %s", err, string(b))
	}

	// 10. Walk nested directory lookup
	nestedDir := filepath.Join(tmpDir, "nested", "sub")
	_ = os.MkdirAll(nestedDir, 0o755)
	walkContent := []byte("walk search content")
	walkDigest := protocol.DigestBytes(walkContent)
	walkPath := filepath.Join(nestedDir, walkDigest+".json")
	_ = os.WriteFile(walkPath, walkContent, 0o644)
	b, err = r.ReadVerified(ctx, "unmatched-ref", walkDigest)
	if err != nil || string(b) != string(walkContent) {
		t.Fatalf("expected walk match: %v, %s", err, string(b))
	}
}

func TestFormatEmpiricalReport(t *testing.T) {
	rep := &empirical.Report{
		CampaignID:        "camp-1",
		PlanDigest:        "sha256:111",
		SourceCommit:      "commit-1",
		Provenance:        "test",
		Conclusion:        empirical.ConclusionGo,
		ConclusionReasons: []string{"reason 1", "reason 2"},
		Admission: empirical.AdmissionResult{
			Admitted:    true,
			ReasonCodes: []string{"RC1", "RC2"},
			Limitations: []string{"limitation 1"},
		},
		RawGateSkipped: "eval skipped for testing",
	}
	out, err := formatEmpiricalReport(rep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "**Reason Codes:** RC1, RC2") {
		t.Errorf("missing reason codes in output: %s", out)
	}
	if !strings.Contains(out, "limitation 1") {
		t.Errorf("missing limitation in output: %s", out)
	}
	if !strings.Contains(out, "eval skipped for testing") {
		t.Errorf("missing raw gate skipped in output: %s", out)
	}

	repWithRawGate := &empirical.Report{
		CampaignID: "camp-2",
		Conclusion: empirical.ConclusionGo,
		RawGate: &gate.GateEvaluationResult{
			Decision: gate.DecisionGo,
		},
	}
	out2, err := formatEmpiricalReport(repWithRawGate)
	if err != nil || !strings.Contains(out2, "**Gate Decision:** go") {
		t.Errorf("expected RawGate formatted in output: %v, %s", err, out2)
	}
}

func TestLoadAggregatedReport(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Not found
	_, _, err := loadAggregatedReport(filepath.Join(tmpDir, "missing.json"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}

	// 2. CampaignSummary with AggregatedReport
	summaryPath := filepath.Join(tmpDir, "summary_agg.json")
	summaryData, _ := json.Marshal(campaign.CampaignSummary{
		AggregatedReport: &telemetry.AggregatedReport{
			TotalSnapshots: 10,
		},
	})
	_ = os.WriteFile(summaryPath, summaryData, 0o644)
	rep, _, err := loadAggregatedReport(summaryPath)
	if err != nil || rep == nil || rep.TotalSnapshots != 10 {
		t.Fatalf("expected aggregated report from summary, got %v, %v", rep, err)
	}

	// 3. CampaignSummary with Snapshots (no AggregatedReport)
	summarySnapPath := filepath.Join(tmpDir, "summary_snaps.json")
	summarySnapData, _ := json.Marshal(campaign.CampaignSummary{
		Snapshots: []telemetry.RunTelemetrySnapshot{
			{RunID: "r1", Strategy: "s4", Capability: "c1", Accepted: true},
		},
	})
	_ = os.WriteFile(summarySnapPath, summarySnapData, 0o644)
	rep, _, err = loadAggregatedReport(summarySnapPath)
	if err != nil || rep == nil || rep.TotalSnapshots != 1 {
		t.Fatalf("expected aggregated report from snapshots in summary, got %v, %v", rep, err)
	}

	// 4. Invalid content
	invalidPath := filepath.Join(tmpDir, "invalid.json")
	_ = os.WriteFile(invalidPath, []byte(`{"not_recognized": true}`), 0o644)
	_, _, err = loadAggregatedReport(invalidPath)
	if err == nil {
		t.Fatal("expected error for unrecognized content")
	}
}

func TestCLIBenchmarkEvaluateGate_AdditionalBranches(t *testing.T) {
	c := newCLI(t)
	tmpDir := t.TempDir()
	reportPath := filepath.Join(tmpDir, "report.json")
	writeTestReportFile(t, reportPath, 0.95, 400.0, 1000.0, 20, false)

	// 1. Unknown flag
	_, _, err := c.run("benchmark", "evaluate-gate", "--unknown-flag")
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}

	// 2. Missing snapshots
	_, _, err = c.run("benchmark", "evaluate-gate", "--snapshots", filepath.Join(tmpDir, "missing.json"))
	if err == nil {
		t.Fatal("expected error for missing snapshots file")
	}

	// 3. Missing criteria
	_, _, err = c.run("benchmark", "evaluate-gate", "--snapshots", reportPath, "--criteria", filepath.Join(tmpDir, "missing.json"))
	if err == nil {
		t.Fatal("expected error for missing criteria file")
	}

	// 4. Malformed criteria
	badCrit := filepath.Join(tmpDir, "bad_criteria.json")
	_ = os.WriteFile(badCrit, []byte("{invalid"), 0o644)
	_, _, err = c.run("benchmark", "evaluate-gate", "--snapshots", reportPath, "--criteria", badCrit)
	if err == nil {
		t.Fatal("expected error for malformed criteria")
	}

	// 5. JSON output to stdout
	stdout, _, err := c.run("benchmark", "evaluate-gate", "--snapshots", reportPath, "--json")
	if err != nil {
		t.Fatalf("unexpected error for json stdout: %v", err)
	}
	if !strings.Contains(stdout, `"decision"`) {
		t.Fatalf("expected JSON output in stdout: %s", stdout)
	}

	// 6. JSON output to file
	outJSON := filepath.Join(tmpDir, "out.json")
	_, _, err = c.run("benchmark", "evaluate-gate", "--snapshots", reportPath, "--json", "--output", outJSON)
	if err != nil {
		t.Fatalf("unexpected error for json output file: %v", err)
	}

	// 7. Output directory creation failure
	badFile := filepath.Join(tmpDir, "file_blocking_dir")
	_ = os.WriteFile(badFile, []byte("x"), 0o644)
	_, _, err = c.run("benchmark", "evaluate-gate", "--snapshots", reportPath, "--output", filepath.Join(badFile, "sub", "out.md"))
	if err == nil {
		t.Fatal("expected error when output dir cannot be created")
	}

	// 8. Output file write failure (output is a directory)
	_, _, err = c.run("benchmark", "evaluate-gate", "--snapshots", reportPath, "--output", tmpDir)
	if err == nil {
		t.Fatal("expected error when output path is a directory")
	}
}

func TestCLIBenchmarkReport_AdditionalBranches(t *testing.T) {
	c := newCLI(t)
	tmpDir := t.TempDir()
	reportPath := filepath.Join(tmpDir, "report.json")
	writeTestReportFile(t, reportPath, 0.95, 400.0, 1000.0, 20, false)

	// 1. Unknown flag
	_, _, err := c.run("benchmark", "report", "--unknown-flag")
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}

	// 2. Missing summary file
	_, _, err = c.run("benchmark", "report", "--summary", filepath.Join(tmpDir, "missing.json"))
	if err == nil {
		t.Fatal("expected error for missing summary file")
	}

	// 3. Output to stdout markdown
	stdout, _, err := c.run("benchmark", "report", "--summary", reportPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "# Empirical Benchmark Telemetry Report") {
		t.Fatalf("expected markdown report in stdout: %s", stdout)
	}

	// 4. Output to stdout json
	stdout, _, err = c.run("benchmark", "report", "--summary", reportPath, "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, `"total_snapshots"`) {
		t.Fatalf("expected json report in stdout: %s", stdout)
	}

	// 5. Output dir creation failure
	badFile := filepath.Join(tmpDir, "blocking_file")
	_ = os.WriteFile(badFile, []byte("x"), 0o644)
	_, _, err = c.run("benchmark", "report", "--summary", reportPath, "--output", filepath.Join(badFile, "sub", "rep.md"))
	if err == nil {
		t.Fatal("expected error when output dir cannot be created")
	}

	// 6. Output file write failure
	_, _, err = c.run("benchmark", "report", "--summary", reportPath, "--output", tmpDir)
	if err == nil {
		t.Fatal("expected error when output path is a directory")
	}
}

func TestCLIBenchmarkReplayEmpirical_MoreCases(t *testing.T) {
	c := newCLI(t)
	f := newEmpiricalFixture()
	mPath, pPath, aPath, artDir, cPath := setupEmpiricalFiles(t, f)
	tmpDir := t.TempDir()

	origVerifier := newIndependentVerifier
	newIndependentVerifier = func(ctx context.Context, e *env, resolver empirical.ArtifactResolver, repoDir string) (empirical.IndependentVerifier, error) {
		return &testCountingVerifier{}, nil
	}
	defer func() { newIndependentVerifier = origVerifier }()

	// 1. Unknown flag
	_, _, err := c.run("benchmark", "replay-empirical", "--unknown-flag")
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}

	// 2. Missing plan file
	_, _, err = c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", filepath.Join(tmpDir, "missing_plan.json"),
		"--authorization", aPath,
		"--artifacts", artDir,
		"--criteria", cPath)
	if code := exitCode(err); code != 3 {
		t.Fatalf("expected exit code 3 for missing plan, got %d", code)
	}

	// 3. Malformed plan file
	badPlan := filepath.Join(tmpDir, "bad_plan.json")
	_ = os.WriteFile(badPlan, []byte("{invalid"), 0o644)
	_, _, err = c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", badPlan,
		"--authorization", aPath,
		"--artifacts", artDir,
		"--criteria", cPath)
	if code := exitCode(err); code != 2 {
		t.Fatalf("expected exit code 2 for malformed plan, got %d", code)
	}

	// 4. Missing auth file
	_, _, err = c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", pPath,
		"--authorization", filepath.Join(tmpDir, "missing_auth.json"),
		"--artifacts", artDir,
		"--criteria", cPath)
	if code := exitCode(err); code != 3 {
		t.Fatalf("expected exit code 3 for missing auth, got %d", code)
	}

	// 5. Malformed auth file
	badAuth := filepath.Join(tmpDir, "bad_auth.json")
	_ = os.WriteFile(badAuth, []byte("{invalid"), 0o644)
	_, _, err = c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", pPath,
		"--authorization", badAuth,
		"--artifacts", artDir,
		"--criteria", cPath)
	if code := exitCode(err); code != 2 {
		t.Fatalf("expected exit code 2 for malformed auth, got %d", code)
	}

	// 6. Missing criteria file
	_, _, err = c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", pPath,
		"--authorization", aPath,
		"--artifacts", artDir,
		"--criteria", filepath.Join(tmpDir, "missing_crit.json"))
	if code := exitCode(err); code != 3 {
		t.Fatalf("expected exit code 3 for missing criteria, got %d", code)
	}

	// 7. Malformed criteria file
	badCrit := filepath.Join(tmpDir, "bad_crit.json")
	_ = os.WriteFile(badCrit, []byte("{invalid"), 0o644)
	_, _, err = c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", pPath,
		"--authorization", aPath,
		"--artifacts", artDir,
		"--criteria", badCrit)
	if code := exitCode(err); code != 2 {
		t.Fatalf("expected exit code 2 for malformed criteria, got %d", code)
	}

	// 8. Missing artifacts dir
	_, _, err = c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", pPath,
		"--authorization", aPath,
		"--artifacts", filepath.Join(tmpDir, "nonexistent_artifacts"),
		"--criteria", cPath)
	if code := exitCode(err); code != 3 {
		t.Fatalf("expected exit code 3 for missing artifacts dir, got %d", code)
	}

	// 9. Custom operator-dir with anchors and revocations (receipt revoked -> exit 3)
	customOpDir := filepath.Join(tmpDir, "custom_op")
	_ = os.MkdirAll(customOpDir, 0o755)
	anchorsDoc := struct {
		Version string                 `json:"version"`
		Anchors []receipts.TrustAnchor `json:"anchors"`
	}{
		Version: "1.0",
		Anchors: []receipts.TrustAnchor{f.anchor},
	}
	anchorsBytes, _ := json.Marshal(anchorsDoc)
	_ = os.WriteFile(filepath.Join(customOpDir, "anchors.json"), anchorsBytes, 0o644)
	revDoc := struct {
		Version    string   `json:"version"`
		ReceiptIDs []string `json:"receipt_ids"`
	}{
		Version:    "1.0",
		ReceiptIDs: []string{"rcpt_00000000000000000000000001"},
	}
	revBytes, _ := json.Marshal(revDoc)
	_ = os.WriteFile(filepath.Join(customOpDir, "revoked.json"), revBytes, 0o644)

	_, stderr, err := c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", pPath,
		"--authorization", aPath,
		"--artifacts", artDir,
		"--criteria", cPath,
		"--operator-dir", customOpDir)
	if code := exitCode(err); code != 3 {
		t.Fatalf("expected exit code 3 for revoked receipt in custom operator-dir, got %d (err: %v)", code, err)
	}
	if !strings.Contains(stderr, "empirical admission refused") {
		t.Fatalf("expected refusal notification, got: %s", stderr)
	}

	// 9b. Custom operator-dir with anchors and no revocations -> success
	customOpPassDir := filepath.Join(tmpDir, "custom_op_pass")
	_ = os.MkdirAll(customOpPassDir, 0o755)
	_ = os.WriteFile(filepath.Join(customOpPassDir, "anchors.json"), anchorsBytes, 0o644)
	_, _, err = c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", pPath,
		"--authorization", aPath,
		"--artifacts", artDir,
		"--criteria", cPath,
		"--operator-dir", customOpPassDir)
	if err != nil {
		t.Fatalf("expected success with valid operator-dir, got: %v", err)
	}

	// 10. Output report write failure (dir creation error)
	badFile := filepath.Join(tmpDir, "block_file")
	_ = os.WriteFile(badFile, []byte("x"), 0o644)
	_, _, err = c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", pPath,
		"--authorization", aPath,
		"--artifacts", artDir,
		"--criteria", cPath,
		"--output", filepath.Join(badFile, "sub", "rep.md"))
	if err == nil {
		t.Fatal("expected error on output dir creation failure")
	}

	// 11. Output report write failure (path is directory)
	_, _, err = c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", pPath,
		"--authorization", aPath,
		"--artifacts", artDir,
		"--criteria", cPath,
		"--output", tmpDir)
	if err == nil {
		t.Fatal("expected error on output write failure when path is directory")
	}

	// 11b. Output to stdout using "-"
	stdout, _, err := c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", pPath,
		"--authorization", aPath,
		"--artifacts", artDir,
		"--criteria", cPath,
		"--output", "-")
	if err != nil {
		t.Fatalf("expected success with --output -, got: %v", err)
	}
	if !strings.Contains(stdout, "**Empirical Conclusion:** GO") {
		t.Errorf("expected GO conclusion in stdout, got: %s", stdout)
	}

	// 12. newIndependentVerifier returns error
	newIndependentVerifier = func(ctx context.Context, e *env, resolver empirical.ArtifactResolver, repoDir string) (empirical.IndependentVerifier, error) {
		return nil, errors.New("simulated verifier creation error")
	}
	_, _, err = c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", pPath,
		"--authorization", aPath,
		"--artifacts", artDir,
		"--criteria", cPath)
	if err == nil {
		t.Fatal("expected error when independent verifier fails to instantiate")
	}
	newIndependentVerifier = func(ctx context.Context, e *env, resolver empirical.ArtifactResolver, repoDir string) (empirical.IndependentVerifier, error) {
		return &testCountingVerifier{}, nil
	}

	// 13. Context cancellation causing admitter.Admit error
	cancCtx, cancel := context.WithCancel(context.Background())
	cancel()
	var outBuf, errBuf bytes.Buffer
	testEnv := &env{stdout: &outBuf, stderr: &errBuf}
	err = runBenchmarkReplayEmpirical(cancCtx, testEnv, []string{
		"--manifest", mPath,
		"--plan", pPath,
		"--authorization", aPath,
		"--artifacts", artDir,
		"--criteria", cPath,
	})
	if code := exitCode(err); code != 3 {
		t.Fatalf("expected exit code 3 on canceled context, got %d (err: %v)", code, err)
	}
	if !strings.Contains(errBuf.String(), "empirical admission error") {
		t.Fatalf("expected admission error message in stderr, got: %s", errBuf.String())
	}
}

func TestLoadOperatorVerifier_UnprotectedOrAbsentDirRejected(t *testing.T) {
	// Calling defaultLoadOperatorVerifier with a user-owned temp dir fails because
	// receipts.NewFileVerifier enforces that operator dir must not be owned by verifier euid.
	tmpDir := t.TempDir()
	_, err := defaultLoadOperatorVerifier(tmpDir, "", "")
	if err == nil {
		t.Fatal("expected error for unprotected operator dir")
	}
	if cat := errs.CategoryOf(err); cat != errs.CategoryPolicyDenied {
		t.Errorf("expected CategoryPolicyDenied, got %v: %v", cat, err)
	}

	// Calling with non-existent dir also fails
	_, err = defaultLoadOperatorVerifier(filepath.Join(tmpDir, "nonexistent"), "", "")
	if err == nil {
		t.Fatal("expected error for non-existent operator dir")
	}
}

func TestLoadOperatorVerifier_ForgedAnchorInArtifactsRejected(t *testing.T) {
	// A forged anchor placed in artifactsDir must NOT be used as a trust root (B1).
	tmpDir := t.TempDir()
	artDir := filepath.Join(tmpDir, "artifacts")
	_ = os.MkdirAll(artDir, 0o755)

	f := newEmpiricalFixture()
	ad := struct {
		Version string                 `json:"version"`
		Anchors []receipts.TrustAnchor `json:"anchors"`
	}{
		Version: "1.0",
		Anchors: []receipts.TrustAnchor{f.anchor},
	}
	adBytes, _ := json.Marshal(ad)
	_ = os.WriteFile(filepath.Join(artDir, "anchors.json"), adBytes, 0o644)
	_ = os.WriteFile(filepath.Join(artDir, "revoked.json"), []byte(`{"version":"1.0","receipt_ids":[],"anchor_ids":[]}`), 0o644)

	// Calling defaultLoadOperatorVerifier with empty operatorDir and forged artifactsDir
	// must not load anchors from artDir; it fails because the system operator root is absent/unprotected.
	_, err := defaultLoadOperatorVerifier("", artDir, "")
	if err == nil {
		t.Fatal("expected defaultLoadOperatorVerifier to reject and not load forged anchor from artifactsDir")
	}
}

func TestCLIReplayEmpirical_ForgedAnchorInArtifactsRejected(t *testing.T) {
	c := newCLI(t)
	f := newEmpiricalFixture()
	mPath, pPath, aPath, artDir, cPath := setupEmpiricalFiles(t, f)

	// Reset loadOperatorVerifier to defaultLoadOperatorVerifier to test real protection
	loadOperatorVerifier = defaultLoadOperatorVerifier

	// With real verifier, forged anchor in artDir is not trusted -> operator verification fails
	_, stderr, err := c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", pPath,
		"--authorization", aPath,
		"--artifacts", artDir,
		"--criteria", cPath)
	if err == nil {
		t.Fatal("expected failure when operator trust root is missing or forged in --artifacts")
	}
	errStr := err.Error() + " " + stderr
	if !strings.Contains(errStr, "failed to initialize receipt verifier") && !strings.Contains(errStr, "OPERATOR_AUTHORITY") {
		t.Fatalf("unexpected error/stderr: %s", errStr)
	}
}

func TestCLIReplayEmpirical_InvalidRepoRootRejected(t *testing.T) {
	c := newCLI(t)
	f := newEmpiricalFixture()
	mPath, pPath, aPath, artDir, cPath := setupEmpiricalFiles(t, f)

	// Pass a nonexistent directory to --repo (I3)
	_, stderr, err := c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", pPath,
		"--authorization", aPath,
		"--artifacts", artDir,
		"--criteria", cPath,
		"--repo", filepath.Join(t.TempDir(), "nonexistent-repo"))
	if err == nil {
		t.Fatal("expected error for invalid --repo")
	}
	errStr := err.Error() + " " + stderr
	if !strings.Contains(errStr, "invalid repository root") {
		t.Fatalf("expected 'invalid repository root' error, got: %s", errStr)
	}
}

func TestCLIReplayEmpirical_ValidRepoRootAccepted(t *testing.T) {
	c := newCLI(t)
	f := newEmpiricalFixture()
	mPath, pPath, aPath, artDir, cPath := setupEmpiricalFiles(t, f)

	// Initialize a minimal git repository in temp directory
	repoDir := t.TempDir()
	cmd := exec.Command("git", "init", repoDir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	origVerifier := newIndependentVerifier
	var repoRootsSeen []string
	newIndependentVerifier = func(ctx context.Context, e *env, resolver empirical.ArtifactResolver, repoDir string) (empirical.IndependentVerifier, error) {
		repoRootsSeen = append(repoRootsSeen, repoDir)
		return &testCountingVerifier{}, nil
	}
	defer func() { newIndependentVerifier = origVerifier }()

	stdout, stderr, err := c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", pPath,
		"--authorization", aPath,
		"--artifacts", artDir,
		"--criteria", cPath,
		"--repo", repoDir)
	if err != nil {
		t.Fatalf("expected success with valid --repo, got: %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	if !strings.Contains(stdout, "**Empirical Conclusion:** GO") {
		t.Errorf("expected GO conclusion in stdout, got: %s", stdout)
	}

	if len(repoRootsSeen) != 1 || repoRootsSeen[0] != repoDir {
		t.Fatalf("expected explicit repository %q, observed %v", repoDir, repoRootsSeen)
	}

	// An independent replay must not inherit --repo from an earlier invocation.
	_, stderr, err = c.run("benchmark", "replay-empirical",
		"--manifest", mPath,
		"--plan", pPath,
		"--authorization", aPath,
		"--artifacts", artDir,
		"--criteria", cPath)
	if err != nil {
		t.Fatalf("expected independent replay to work: %v, stderr: %s", err, stderr)
	}
	if len(repoRootsSeen) != 2 || repoRootsSeen[1] != "." {
		t.Fatalf("repository selection leaked across replays: %v", repoRootsSeen)
	}
}

func TestLoadOperatorVerifier_RevocationHonored(t *testing.T) {
	f := newEmpiricalFixture()
	manifest, _, authBytes, pd, ad := f.manifest()

	now := time.Now().UTC()
	text := "authorization receipt"
	stmt := receipts.Statement{
		Version:      "1.0",
		ReceiptID:    "rcpt_00000000000000000000000001",
		AnchorID:     f.anchor.AnchorID,
		HumanActorID: f.anchor.HumanActorID,
		IssuedAt:     now.Add(-2 * time.Hour),
		NotAfter:     now.Add(48 * time.Hour),
		Purpose:      receipts.PurposeEmpiricalCampaignAuthorize,
		Use:          receipts.UseGrant,
		ProjectID:    "devcadence",
		Subject: receipts.Subject{
			Kind:    "CampaignAuthorization",
			ID:      pd,
			Version: 1,
		},
		SubjectDigest: ad,
		Text:          text,
		TextDigest:    protocol.DigestBytes([]byte(text)),
	}
	signedRcpt, err := stmt.Sign(f.privKey)
	if err != nil {
		t.Fatal(err)
	}

	revMap := map[string]time.Time{
		"rcpt_00000000000000000000000001": now,
	}
	revList := &receipts.RevocationList{
		RevokedIDs: revMap,
		RevokedAt:  now,
	}

	inMemVerifier, err := receipts.NewVerifierWithReceipts(
		[]receipts.TrustAnchor{f.anchor},
		[]receipts.Receipt{signedRcpt},
		receipts.WithRevocationList(revList),
		receipts.WithClock(clock.System()),
	)
	if err != nil {
		t.Fatal(err)
	}

	ca, err := verifier.NewCampaignAuthority(inMemVerifier, "devcadence", clock.System())
	if err != nil {
		t.Fatal(err)
	}

	_, err = ca.VerifyAuthorization(context.Background(), manifest.PlanDigest, manifest.AuthorizationDigest, authBytes)
	if err == nil {
		t.Fatal("expected error for revoked receipt")
	}
	if cat := errs.CategoryOf(err); cat != errs.CategoryPolicyDenied {
		t.Errorf("expected CategoryPolicyDenied, got %v: %v", cat, err)
	}
}

func TestDefaultNewIndependentVerifier(t *testing.T) {
	v, err := defaultNewIndependentVerifier(context.Background(), &env{}, nil, ".")
	if err != nil {
		t.Fatalf("unexpected error creating independent verifier: %v", err)
	}
	if v == nil {
		t.Fatal("expected non-nil verifier")
	}
}

func TestCLIReplayEmpirical_YoloRequiresExplicitConsent(t *testing.T) {
	c := newCLI(t)
	f := newEmpiricalFixture()
	mPath, pPath, aPath, artDir, cPath := setupEmpiricalFiles(t, f)
	base := []string{"benchmark", "replay-empirical",
		"--manifest", mPath, "--plan", pPath, "--authorization", aPath,
		"--artifacts", artDir, "--criteria", cPath}
	for _, tc := range []struct {
		name  string
		extra []string
	}{
		{name: "yolo without consent", extra: []string{"--execution-mode", "yolo"}},
		{name: "consent in strict mode", extra: []string{"--allow-unconfined-verifier"}},
		{name: "unknown mode", extra: []string{"--execution-mode", "anything"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := c.run(append(append([]string(nil), base...), tc.extra...)...)
			if err == nil {
				t.Fatal("expected unsafe flag combination to fail closed")
			}
		})
	}
}

func TestCLIReplayEmpirical_YoloConsentIsInvocationScoped(t *testing.T) {
	c := newCLI(t)
	f := newEmpiricalFixture()
	mPath, pPath, aPath, artDir, cPath := setupEmpiricalFiles(t, f)
	original := newIndependentVerifier
	defer func() { newIndependentVerifier = original }()
	var decisions []bool
	newIndependentVerifier = func(ctx context.Context, e *env, resolver empirical.ArtifactResolver, root string) (empirical.IndependentVerifier, error) {
		decisions = append(decisions, ctx.Value(verifierExecutionConsentKey{}) == true)
		return &testCountingVerifier{}, nil
	}
	args := []string{"benchmark", "replay-empirical", "--manifest", mPath,
		"--plan", pPath, "--authorization", aPath, "--artifacts", artDir,
		"--criteria", cPath}
	for _, flags := range [][]string{
		{"--execution-mode", "yolo", "--allow-unconfined-verifier"},
		nil,
	} {
		if _, _, err := c.run(append(append([]string(nil), args...), flags...)...); err != nil {
			t.Fatalf("replay with flags %v failed: %v", flags, err)
		}
	}
	if len(decisions) != 2 || !decisions[0] || decisions[1] {
		t.Fatalf("unsafe consent leaked across invocations: %v", decisions)
	}
}

func TestCLIReplayEmpirical_ProjectYOLOConfiguration(t *testing.T) {
	c := newCLI(t)
	f := newEmpiricalFixture()
	mPath, pPath, aPath, artDir, cPath := setupEmpiricalFiles(t, f)
	repoDir := t.TempDir()
	if err := exec.Command("git", "init", repoDir).Run(); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(repoDir, ".devcadence")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(configDir, "benchmark.json")
	if err := os.WriteFile(configFile, []byte(`{"execution_mode":"yolo","allow_unconfined_verifier":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	original := newIndependentVerifier
	defer func() { newIndependentVerifier = original }()
	var consent []bool
	newIndependentVerifier = func(ctx context.Context, e *env, resolver empirical.ArtifactResolver, root string) (empirical.IndependentVerifier, error) {
		consent = append(consent, ctx.Value(verifierExecutionConsentKey{}) == true)
		return &testCountingVerifier{}, nil
	}
	base := []string{"benchmark", "replay-empirical", "--repo", repoDir,
		"--manifest", mPath, "--plan", pPath, "--authorization", aPath,
		"--artifacts", artDir, "--criteria", cPath}
	for _, flags := range [][]string{nil, {"--execution-mode", "strict"}} {
		_, _, err := c.run(append(append([]string{}, base...), flags...)...)
		if err != nil {
			t.Fatalf("configured replay flags %v: %v", flags, err)
		}
	}
	if len(consent) != 2 || !consent[0] || consent[1] {
		t.Fatalf("project yolo should be enabled and explicit strict override should disable it: %v", consent)
	}
	if err := os.WriteFile(configFile, []byte(`{"execution_mode":"yolo","allow_unconfined_verifier":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.run(base...); err == nil {
		t.Fatal("project yolo without explicit consent must be refused")
	}
}
