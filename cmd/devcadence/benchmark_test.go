package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/benchmark"
	"github.com/olostan/DevCadence/internal/benchmark/experiments"
	"github.com/olostan/DevCadence/internal/benchmark/gate"
	"github.com/olostan/DevCadence/internal/benchmark/telemetry"
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
