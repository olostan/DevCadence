package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/cognition/planner"
	"github.com/olostan/DevCadence/internal/protocol"
)

// makeValidTestPortfolio creates a self-consistent CognitionPortfolio for CLI tests.
func makeValidTestPortfolio(portfolioID string) *protocol.CognitionPortfolio {
	return &protocol.CognitionPortfolio{
		SchemaVersion:     protocol.SchemaVersion1,
		PortfolioID:       portfolioID,
		Revision:          1,
		CreatedAt:         "2026-10-04T12:00:00Z",
		MaxSourceExposure: protocol.ExposureLocalOnly,
		Channels: []protocol.AccessChannel{
			{
				SchemaVersion:         protocol.SchemaVersion1,
				ChannelID:             "chan-local-01",
				EndpointID:            "ep-local-01",
				Kind:                  protocol.ChannelLocalDaemonSocket,
				SessionMode:           protocol.SessionStatelessPerCall,
				ContextControl:        protocol.ContextControlExactStateless,
				PrefixCache:           protocol.PrefixCacheSessionKV,
				SupportsStreaming:     true,
				SupportsTools:         true,
				MaxConcurrentRequests: 1,
			},
		},
		RoleBindings: []protocol.RoleBinding{
			{
				Role:             "implementer",
				EndpointID:       "ep-local-01",
				ChannelID:        "chan-local-01",
				BudgetPoolID:     "pool-local",
				ContextProfileID: "prof-local-01",
				Priority:         1,
			},
		},
		BudgetPools: []protocol.BudgetPool{
			{
				SchemaVersion:  protocol.SchemaVersion1,
				PoolID:         "pool-local",
				Name:           "Local compute pool",
				Regime:         protocol.RegimeLocalCompute,
				HardLimit:      1000,
				SoftAlertLimit: 800,
				Unit:           protocol.UnitRequests,
				Period:         protocol.PeriodRollingDay,
			},
		},
	}
}

func setupActivePortfolio(t *testing.T, stateDir string, p *protocol.CognitionPortfolio) {
	t.Helper()
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	pBytes, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, cognition.ActivePortfolioFileName), pBytes, 0644); err != nil {
		t.Fatal(err)
	}
	lineage := cognition.PortfolioLineage{
		CurrentActivationID:      "act-20261004T120000Z-0001",
		CurrentSequence:          1,
		CurrentPortfolioID:       p.PortfolioID,
		CurrentPortfolioRevision: p.Revision,
		ActivatedAt:              "2026-10-04T12:00:00Z",
		History: []cognition.ActivationRecord{
			{
				ActivationID:      "act-20261004T120000Z-0001",
				Sequence:          1,
				ActivatedAt:       "2026-10-04T12:00:00Z",
				PortfolioID:       p.PortfolioID,
				PortfolioRevision: p.Revision,
				Portfolio:         *p,
			},
		},
	}
	linBytes, err := json.Marshal(lineage)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, cognition.LineageFileName), linBytes, 0644); err != nil {
		t.Fatal(err)
	}
}

func setupCognitionFixture(t *testing.T, tempHome string) {
	t.Helper()
	totalMem := int64(32 * 1024 * 1024 * 1024)
	ts := protocol.NewTimestamp(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC))
	fixture := cognition.ValidationInput{
		MachineProfile: &protocol.MachineCapabilityProfile{
			SchemaVersion:      protocol.SchemaVersion1,
			ProfileID:          "prof-test-machine",
			MachineFingerprint: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			ObservedAt:         ts,
			Endpoints: []protocol.CognitionEndpoint{
				{
					ID:                     "ep-local-01",
					Kind:                   protocol.EndpointLocalRuntime,
					Locality:               protocol.LocalityLocal,
					Health:                 protocol.EndpointHealthReady,
					Auth:                   protocol.AuthNotApplicable,
					CostClass:              protocol.CostLocalCompute,
					RequiredSourceExposure: protocol.ExposureLocalOnly,
					StructuredOutput:       protocol.FeatureProbePassed,
					ToolUse:                protocol.FeatureProbePassed,
					ObservedAt:             ts,
					Capabilities: []protocol.GradedCapability{
						{
							Dimension:  protocol.CapabilityImplementation,
							Grade:      protocol.GradeStrong,
							Provenance: protocol.ProvenanceMeasured,
						},
						{
							Dimension:  protocol.CapabilityRepositoryReasoning,
							Grade:      protocol.GradeStrong,
							Provenance: protocol.ProvenanceMeasured,
						},
					},
				},
			},
		},
		Inventory: &protocol.ResourceInventory{
			SchemaVersion:      protocol.SchemaVersion1,
			InventoryID:        "inv-test",
			MachineFingerprint: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			ObservedAt:         ts,
			Hardware: protocol.HardwareSummary{
				OSFamily:         protocol.OSLinux,
				Arch:             "amd64",
				LogicalCores:     8,
				TotalMemoryBytes: &totalMem,
			},
			CognitionEndpoints: []protocol.CognitionEndpointSummary{
				{
					ID:                     "ep-local-01",
					Kind:                   protocol.EndpointLocalRuntime,
					Locality:               protocol.LocalityLocal,
					Health:                 protocol.EndpointHealthReady,
					Auth:                   protocol.AuthNotApplicable,
					CostClass:              protocol.CostLocalCompute,
					RequiredSourceExposure: protocol.ExposureLocalOnly,
				},
			},
		},
	}
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("setupCognitionFixture: marshal failed: %v", err)
	}
	fixturePath := filepath.Join(tempHome, "cognition-fixture.json")
	if err := os.WriteFile(fixturePath, data, 0644); err != nil {
		t.Fatalf("setupCognitionFixture: write failed: %v", err)
	}
}

// TestCognitionRecommendACC01 verifies ACC-01: `devcadence cognition recommend --json`
// emits valid planner.Result JSON with deterministic exit 0.
func TestCognitionRecommendACC01(t *testing.T) {
	c := newCLI(t)
	out, stderr, err := c.run("cognition", "recommend", "--json")
	if err != nil {
		t.Fatalf("cognition recommend --json failed: %v\nstderr: %s", err, stderr)
	}

	var res planner.Result
	dec := json.NewDecoder(strings.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&res); err != nil {
		t.Fatalf("failed to decode planner.Result JSON: %v\noutput: %s", err, out)
	}
	if res.Outcome == "" {
		t.Errorf("expected non-empty Outcome in planner.Result")
	}
	if res.InventoryDigest == "" {
		t.Errorf("expected non-empty InventoryDigest in planner.Result")
	}
}

// TestCognitionRecommendTableFormat tests human-readable table output.
func TestCognitionRecommendTableFormat(t *testing.T) {
	c := newCLI(t)
	out, stderr, err := c.run("cognition", "recommend")
	if err != nil {
		t.Fatalf("cognition recommend failed: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(out, "Cognition Portfolio Recommendations") {
		t.Errorf("expected header in output:\n%s", out)
	}
	if !strings.Contains(out, "Outcome:") {
		t.Errorf("expected Outcome line in output:\n%s", out)
	}
}

// TestCognitionExplainACC02 verifies ACC-02: `devcadence cognition explain --active`
// explains the active portfolio. When active exists, prints roles and pools.
// When active does not exist, returns NotFound (exit code 3).
func TestCognitionExplainACC02(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("DEVCADENCE_HOME", tempHome)

	c := newCLI(t)

	// 1. Initial check: no active portfolio -> returns NotFound (exit code 3)
	_, _, err := c.run("cognition", "explain", "--active")
	if err == nil {
		t.Fatalf("expected error when no active portfolio exists")
	}
	if code := exitCode(err); code != ExitCodeNotFound {
		t.Errorf("expected exit code %d (ExitCodeNotFound), got %d (err: %v)", ExitCodeNotFound, code, err)
	}

	// 2. Setup an active portfolio via helper
	stateDir := filepath.Join(tempHome, "state")
	p := makeValidTestPortfolio("port-active-01")
	setupActivePortfolio(t, stateDir, p)

	// 3. Re-run explain --active: exits 0 and prints summary
	out, stderr, err := c.run("cognition", "explain", "--active")
	if err != nil {
		t.Fatalf("cognition explain --active failed: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(out, "Cognition Portfolio: port-active-01") {
		t.Errorf("expected portfolio header in output:\n%s", out)
	}
	if !strings.Contains(out, "Role Bindings (1):") {
		t.Errorf("expected Role Bindings section in output:\n%s", out)
	}
	if !strings.Contains(out, "Budget Pools (1):") {
		t.Errorf("expected Budget Pools section in output:\n%s", out)
	}

	// 4. Test explain --json on active portfolio
	jsonOut, jsonStderr, err := c.run("cognition", "explain", "--active", "--json")
	if err != nil {
		t.Fatalf("cognition explain --active --json failed: %v\nstderr: %s", err, jsonStderr)
	}
	var decoded protocol.CognitionPortfolio
	if err := json.Unmarshal([]byte(jsonOut), &decoded); err != nil {
		t.Fatalf("failed to unmarshal JSON portfolio: %v\njson: %s", err, jsonOut)
	}
	if decoded.PortfolioID != "port-active-01" {
		t.Errorf("expected portfolio ID 'port-active-01', got %s", decoded.PortfolioID)
	}
}

// TestCognitionExplainFile tests inspecting an explicit file with `devcadence cognition explain --file <path>`.
func TestCognitionExplainFile(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("DEVCADENCE_HOME", tempHome)

	c := newCLI(t)
	p := makeValidTestPortfolio("port-file-01")
	pBytes, _ := json.Marshal(p)
	pFile := filepath.Join(tempHome, "portfolio.json")
	if err := os.WriteFile(pFile, pBytes, 0644); err != nil {
		t.Fatalf("failed to write portfolio file: %v", err)
	}

	out, stderr, err := c.run("cognition", "explain", "--file", pFile)
	if err != nil {
		t.Fatalf("cognition explain --file failed: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(out, "Cognition Portfolio: port-file-01") {
		t.Errorf("expected portfolio header in output:\n%s", out)
	}

	// Nonexistent file returns ExitCodeInvalidArgument (exit 2)
	_, _, err = c.run("cognition", "explain", "--file", filepath.Join(tempHome, "nonexistent.json"))
	if err == nil {
		t.Fatalf("expected error for nonexistent file")
	}
	if code := exitCode(err); code != ExitCodeInvalidArgument {
		t.Errorf("expected ExitCodeInvalidArgument (2), got %d (err: %v)", code, err)
	}
}

// TestCognitionApplyACC03_ACC04_ACC05_ACC06_ACC07 comprehensively covers:
// - ACC-03: Valid candidate file -> activates portfolio, prints activation ID (exit 0).
// - ACC-04: Invalid candidate file -> fails deterministic validation (exit 1, ExecutionError).
// - ACC-05: Rollback to previous activation -> restores previous portfolio (exit 0).
// - ACC-06: Malformed flag or missing file -> fails closed with exit 2 (InvalidArgument).
// - ACC-07: Targeted rollback by activation ID -> restores targeted activation (exit 0).
func TestCognitionApplyACC03_ACC04_ACC05_ACC06_ACC07(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("DEVCADENCE_HOME", tempHome)

	c := newCLI(t)
	setupCognitionFixture(t, tempHome)

	// Rollback with no active portfolio -> exit 3 (NotFound)
	_, _, err := c.run("cognition", "apply", "--rollback")
	if err == nil {
		t.Fatalf("expected error when rolling back with no active portfolio")
	}
	if code := exitCode(err); code != ExitCodeNotFound {
		t.Errorf("expected ExitCodeNotFound (3), got %d (err: %v)", code, err)
	}

	// ACC-06: Missing --file when not rolling back -> exit 2 (InvalidArgument)
	_, _, err = c.run("cognition", "apply")
	if err == nil {
		t.Fatalf("expected error when neither --file nor --rollback is provided")
	}
	if code := exitCode(err); code != ExitCodeInvalidArgument {
		t.Errorf("expected ExitCodeInvalidArgument (2), got %d (err: %v)", code, err)
	}

	// ACC-06: Nonexistent candidate file -> exit 2 (InvalidArgument)
	_, _, err = c.run("cognition", "apply", "--file", filepath.Join(tempHome, "nonexistent.json"))
	if err == nil {
		t.Fatalf("expected error for nonexistent file")
	}
	if code := exitCode(err); code != ExitCodeInvalidArgument {
		t.Errorf("expected ExitCodeInvalidArgument (2), got %d (err: %v)", code, err)
	}

	// ACC-04: Invalid candidate portfolio file (passes schema, fails validation against inventory)
	invalidPort := makeValidTestPortfolio("port-invalid")
	invalidPort.Channels[0].EndpointID = "ep-nonexistent-123"
	invalidPort.RoleBindings[0].EndpointID = "ep-nonexistent-123"
	invBytes, _ := json.Marshal(invalidPort)
	invFile := filepath.Join(tempHome, "invalid.json")
	if err := os.WriteFile(invFile, invBytes, 0644); err != nil {
		t.Fatalf("failed to write invalid portfolio: %v", err)
	}

	_, _, err = c.run("cognition", "apply", "--file", invFile)
	if err == nil {
		t.Fatalf("expected error applying invalid portfolio")
	}
	if code := exitCode(err); code != ExitCodeExecutionError {
		t.Errorf("expected ExitCodeExecutionError (1) for invalid portfolio validation, got %d (err: %v)", code, err)
	}

	// ACC-03: Valid candidate portfolio file -> activates portfolio (exit 0)
	p1 := makeValidTestPortfolio("port-v1")
	p1Bytes, _ := json.Marshal(p1)
	p1File := filepath.Join(tempHome, "p1.json")
	if err := os.WriteFile(p1File, p1Bytes, 0644); err != nil {
		t.Fatalf("failed to write p1: %v", err)
	}

	out1, stderr1, err := c.run("cognition", "apply", "--file", p1File, "--reason", "Initial activation")
	if err != nil {
		t.Fatalf("cognition apply p1 failed: %v\nstderr: %s", err, stderr1)
	}
	if !strings.Contains(out1, "Successfully activated portfolio port-v1") {
		t.Errorf("expected success activation message in output:\n%s", out1)
	}

	// Apply candidate 2 with --json flag
	p2 := makeValidTestPortfolio("port-v2")
	p2Bytes, _ := json.Marshal(p2)
	p2File := filepath.Join(tempHome, "p2.json")
	if err := os.WriteFile(p2File, p2Bytes, 0644); err != nil {
		t.Fatalf("failed to write p2: %v", err)
	}

	out2, stderr2, err := c.run("cognition", "apply", "--file", p2File, "--reason", "Upgrade to v2", "--json")
	if err != nil {
		t.Fatalf("cognition apply p2 --json failed: %v\nstderr: %s", err, stderr2)
	}
	var act2 cognition.ActivationRecord
	if err := json.Unmarshal([]byte(out2), &act2); err != nil {
		t.Fatalf("failed to unmarshal JSON activation record: %v\noutput: %s", err, out2)
	}
	if act2.PortfolioID != "port-v2" {
		t.Errorf("expected activated portfolio 'port-v2', got %s", act2.PortfolioID)
	}
	if act2.Sequence != 2 {
		t.Errorf("expected sequence 2, got %d", act2.Sequence)
	}

	// ACC-05: Rollback to previous activation -> restores port-v1
	out3, stderr3, err := c.run("cognition", "apply", "--rollback")
	if err != nil {
		t.Fatalf("cognition apply --rollback failed: %v\nstderr: %s", err, stderr3)
	}
	if !strings.Contains(out3, "Successfully rolled back portfolio to port-v1") {
		t.Errorf("expected rollback to port-v1 in output:\n%s", out3)
	}

	// ACC-07: Targeted rollback by activation ID -> rollback to act2
	out4, stderr4, err := c.run("cognition", "apply", "--rollback", "--target", act2.ActivationID, "--json")
	if err != nil {
		t.Fatalf("cognition apply --rollback --target failed: %v\nstderr: %s", err, stderr4)
	}
	var act4 cognition.ActivationRecord
	if err := json.Unmarshal([]byte(out4), &act4); err != nil {
		t.Fatalf("failed to unmarshal JSON rollback record: %v\noutput: %s", err, out4)
	}
	if act4.PortfolioID != "port-v2" {
		t.Errorf("expected targeted rollback to restore 'port-v2', got %s", act4.PortfolioID)
	}
	if !act4.IsRollback {
		t.Errorf("expected IsRollback true on rollback record")
	}
}

type mockDriver struct{}

func (m *mockDriver) ID() string { return "mock" }
func (m *mockDriver) Capabilities() drivers.DriverCapabilities {
	return drivers.DriverCapabilities{}
}
func (m *mockDriver) StartSession(ctx context.Context, cfg drivers.SessionConfig) (drivers.Session, error) {
	return nil, nil
}
func (m *mockDriver) ResumeSession(ctx context.Context, sessionID string, cfg drivers.SessionConfig) (drivers.Session, error) {
	return nil, nil
}

func TestSimpleDriverResolverCoverage(t *testing.T) {
	ctx := context.Background()
	r := &simpleDriverResolver{}
	drv, m, err := r.ResolveDriver(ctx, "ep1")
	if drv != nil || m != "" || err != nil {
		t.Fatalf("expected nil driver, empty model, nil error")
	}

	r.drivers = map[string]drivers.SessionDriver{"ep1": nil}
	drv, m, err = r.ResolveDriver(ctx, "ep1")
	if drv != nil || m != "" || err != nil {
		t.Fatalf("expected nil driver")
	}

	drv, m, err = r.ResolveDriver(ctx, "ep-missing")
	if drv != nil || m != "" || err != nil {
		t.Fatalf("expected nil driver for missing endpoint")
	}

	md := &mockDriver{}
	r.drivers["ep1"] = md
	drv, m, err = r.ResolveDriver(ctx, "ep1")
	if drv != md || m != "" || err != nil {
		t.Fatalf("expected mock driver and empty model")
	}

	r.models = map[string]string{"ep1": "model-xyz"}
	drv, m, err = r.ResolveDriver(ctx, "ep1")
	if drv != md || m != "model-xyz" || err != nil {
		t.Fatalf("expected mock driver and model-xyz")
	}
}

func TestRenderRecommendationsCoverage(t *testing.T) {
	res := &planner.Result{
		Outcome:         "accepted",
		InventoryDigest: "sha256:1234",
		Accepted: []*protocol.PortfolioRecommendation{
			{
				Intent:     protocol.IntentBalanced,
				Confidence: protocol.RecommendationConfidenceHigh,
				Tradeoffs:  []string{"moderate latency", "local compute"},
				RecommendedPortfolio: protocol.CognitionPortfolio{
					PortfolioID: "port-rec-1",
					RoleBindings: []protocol.RoleBinding{
						{
							Role:         "implementer",
							EndpointID:   "ep-local-01",
							ChannelID:    "chan-01",
							BudgetPoolID: "pool-01",
						},
					},
				},
			},
		},
		Rejected: []planner.Rejected{
			{
				Intent: protocol.IntentPrivacyFirst,
				Reason: "no local reasoning model available",
				Diagnostics: []cognition.PortfolioDiagnostic{
					{
						Code: "NO_LOCAL_ENDPOINT",
					},
				},
			},
		},
	}
	var buf bytes.Buffer
	renderRecommendations(&buf, res)
	out := buf.String()
	if !strings.Contains(out, "Accepted Recommendations:") {
		t.Errorf("expected Accepted Recommendations in output: %s", out)
	}
	if !strings.Contains(out, "Tradeoffs: moderate latency, local compute") {
		t.Errorf("expected Tradeoffs in output: %s", out)
	}
	if !strings.Contains(out, "Rejections:") {
		t.Errorf("expected Rejections in output: %s", out)
	}
	if !strings.Contains(out, "Reason: no local reasoning model available") {
		t.Errorf("expected Rejection reason in output: %s", out)
	}
}

func TestRenderPortfolioExplanationCoverage(t *testing.T) {
	p := makeValidTestPortfolio("port-explain-detail")
	p.RoleBindings[0].Fallbacks = []protocol.FallbackBinding{
		{
			EndpointID:   "ep-fallback-01",
			ChannelID:    "chan-local-01",
			BudgetPoolID: "pool-local",
		},
	}
	rec := &cognition.ActivationRecord{
		ActivationID: "act-123",
		Sequence:     5,
		ActivatedAt:  "2026-10-04T15:00:00Z",
	}
	var buf bytes.Buffer
	renderPortfolioExplanation(&buf, p, rec)
	out := buf.String()
	if !strings.Contains(out, "Active Sequence:     5") {
		t.Errorf("expected active sequence in output: %s", out)
	}
	if !strings.Contains(out, "fallback -> endpoint=ep-fallback-01") {
		t.Errorf("expected fallback in output: %s", out)
	}
}

func TestSynthesizeContextProfilesCoverage(t *testing.T) {
	synthesizeContextProfiles(nil, nil)
	profiles := make(map[string]*protocol.ContextProfile)
	synthesizeContextProfiles(nil, profiles)

	p := makeValidTestPortfolio("port-synth")
	p.RoleBindings[0].Fallbacks = []protocol.FallbackBinding{
		{
			EndpointID:       "ep-local-01",
			ChannelID:        "chan-local-01",
			ContextProfileID: "prof-fallback-01",
		},
	}
	existingProf := &protocol.ContextProfile{ProfileID: "prof-local-01"}
	profiles["prof-local-01"] = existingProf

	synthesizeContextProfiles(p, profiles)

	if profiles["prof-local-01"] != existingProf {
		t.Errorf("pre-existing profile was overwritten")
	}
	if profiles["prof-fallback-01"] == nil {
		t.Errorf("fallback profile was not synthesized")
	}
	if profiles["prof-fallback-01"].ProfileID != "prof-fallback-01" {
		t.Errorf("fallback profile ID mismatch: %s", profiles["prof-fallback-01"].ProfileID)
	}
}

func TestBuildCognitionValidationInputCoverage(t *testing.T) {
	tempHome := t.TempDir()
	e := &env{}

	t.Setenv("DEVCADENCE_COGNITION_FIXTURE", filepath.Join(tempHome, "missing.json"))
	_, err := buildCognitionValidationInput(context.Background(), e)
	if err == nil {
		t.Errorf("expected error for missing fixture file")
	}

	corruptFile := filepath.Join(tempHome, "corrupt.json")
	_ = os.WriteFile(corruptFile, []byte("{not json"), 0644)
	t.Setenv("DEVCADENCE_COGNITION_FIXTURE", corruptFile)
	_, err = buildCognitionValidationInput(context.Background(), e)
	if err == nil {
		t.Errorf("expected error for malformed fixture file")
	}
}

func TestCognitionRecommendBranchCoverage(t *testing.T) {
	c := newCLI(t)

	_, _, err := c.run("cognition", "recommend", "--depth", "inference")
	if err == nil {
		t.Errorf("expected error for inference depth")
	}
	if code := exitCode(err); code != ExitCodeInvalidArgument {
		t.Errorf("expected ExitCodeInvalidArgument (2), got %d", code)
	}

	_, _, err = c.run("cognition", "recommend", "--depth", "invalid_depth")
	if err == nil {
		t.Errorf("expected error for invalid depth")
	}

	_, _, err = c.run("cognition", "recommend", "--intent", "invalid_intent")
	if err == nil {
		t.Errorf("expected error for invalid intent")
	}
	if code := exitCode(err); code != ExitCodeInvalidArgument {
		t.Errorf("expected ExitCodeInvalidArgument (2), got %d", code)
	}

	out, stderr, err := c.run("cognition", "recommend", "--intent", "balanced")
	if err != nil {
		t.Fatalf("expected success with --intent balanced: %v, stderr: %s", err, stderr)
	}
	if !strings.Contains(out, "Cognition Portfolio Recommendations") {
		t.Errorf("expected header in output: %s", out)
	}
}

func TestCognitionExplainMalformedFile(t *testing.T) {
	tempHome := t.TempDir()
	c := newCLI(t)
	badFile := filepath.Join(tempHome, "bad.json")
	_ = os.WriteFile(badFile, []byte("{not valid json"), 0644)

	_, _, err := c.run("cognition", "explain", "--file", badFile)
	if err == nil {
		t.Fatalf("expected error for malformed file")
	}
	if code := exitCode(err); code != ExitCodeIntegrity {
		t.Errorf("expected ExitCodeIntegrity (5), got %d (err: %v)", code, err)
	}
}

func TestCognitionApplyMalformedCandidateFile(t *testing.T) {
	tempHome := t.TempDir()
	c := newCLI(t)
	badFile := filepath.Join(tempHome, "bad.json")
	_ = os.WriteFile(badFile, []byte("{not valid json"), 0644)

	_, _, err := c.run("cognition", "apply", "--file", badFile)
	if err == nil {
		t.Fatalf("expected error for malformed file")
	}
	if code := exitCode(err); code != ExitCodeIntegrity {
		t.Errorf("expected ExitCodeIntegrity (5), got %d (err: %v)", code, err)
	}
}
