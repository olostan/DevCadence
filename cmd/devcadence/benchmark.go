package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/olostan/DevCadence/internal/benchmark/campaign"
	"github.com/olostan/DevCadence/internal/benchmark/experiments"
	"github.com/olostan/DevCadence/internal/benchmark/gate"
	"github.com/olostan/DevCadence/internal/benchmark/telemetry"
	"github.com/olostan/DevCadence/internal/errs"
)

// gateExitError implements ExitCoder to support discrete exit codes for gate evaluation (REQ-07).
type gateExitError struct {
	code int
	msg  string
}

func (e *gateExitError) Error() string { return e.msg }
func (e *gateExitError) ExitCode() int { return e.code }

func runBenchmark(ctx context.Context, e *env, args []string) error {
	if len(args) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "usage: devcadence benchmark <evaluate-gate|report|gate>")
	}
	switch args[0] {
	case "evaluate-gate":
		return runBenchmarkEvaluateGate(ctx, e, args[1:])
	case "report":
		return runBenchmarkReport(ctx, e, args[1:])
	case "gate":
		return runBenchmarkGate(ctx, e, args[1:])
	default:
		return errs.New(errs.CategoryInvalidArgument, "unknown benchmark subcommand %q", args[0])
	}
}

// loadAggregatedReport loads telemetry snapshots or an aggregated report from a file.
func loadAggregatedReport(path string) (*telemetry.AggregatedReport, map[string]*experiments.FalsificationResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, errs.Wrap(errs.CategoryNotFound, err, "failed to read telemetry input file %q", path)
	}

	// 1. Try parsing as CampaignSummary
	var summary campaign.CampaignSummary
	if err := json.Unmarshal(data, &summary); err == nil && (len(summary.Snapshots) > 0 || summary.AggregatedReport != nil) {
		if summary.AggregatedReport != nil {
			return summary.AggregatedReport, summary.FalsificationResults, nil
		}
		agg, aggErr := telemetry.Aggregate(summary.Snapshots)
		if aggErr != nil {
			return nil, nil, aggErr
		}
		return agg, summary.FalsificationResults, nil
	}

	// 2. Try parsing as []RunTelemetrySnapshot
	var snapshots []telemetry.RunTelemetrySnapshot
	if err := json.Unmarshal(data, &snapshots); err == nil && len(snapshots) > 0 {
		agg, aggErr := telemetry.Aggregate(snapshots)
		if aggErr != nil {
			return nil, nil, aggErr
		}
		return agg, nil, nil
	}

	// 3. Try parsing directly as AggregatedReport
	var report telemetry.AggregatedReport
	if err := json.Unmarshal(data, &report); err == nil && (report.TotalSnapshots > 0 || len(report.Groups) > 0) {
		return &report, nil, nil
	}

	return nil, nil, errs.New(errs.CategoryInvalidArgument, "file %q does not contain valid snapshots, campaign summary, or aggregated report", path)
}

// runBenchmarkEvaluateGate implements `devcadence benchmark evaluate-gate` (REQ-07).
// Options: --snapshots <path>, --output <path>, --json
// Exit codes: 0 for DecisionGo, 1 for DecisionRevise, 2 for DecisionInconclusive.
func runBenchmarkEvaluateGate(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("benchmark evaluate-gate", flag.ContinueOnError)
	snapshotsPath := fs.String("snapshots", "", "path to snapshots, summary, or report JSON file")
	summaryPath := fs.String("summary", "", "alias for --snapshots")
	outputPath := fs.String("output", "", "path to write output markdown/json report (optional, defaults to stdout)")
	criteriaPath := fs.String("criteria", "", "optional path to custom GateCriteria JSON file")
	asJSON := fs.Bool("json", false, "emit evaluation result as JSON instead of Markdown")
	evidenceKind := fs.String("evidence-kind", "", "evidence provenance kind: synthetic_harness_validation or empirical_campaign (unset = unspecified, never rendered as empirical)")
	driverName := fs.String("driver", "", "driver name that produced the evidence (required with --evidence-kind)")
	sourceCommit := fs.String("source-commit", "", "source commit the evidence was generated from")
	regenCmd := fs.String("regen-command", "", "command that regenerates the evidence")

	if err := parseFlags(fs, e, args); err != nil {
		return err
	}

	var provenance *gate.EvidenceProvenance
	if *evidenceKind != "" {
		p, err := gate.NewEvidenceProvenance(*evidenceKind, *driverName, *sourceCommit, *regenCmd)
		if err != nil {
			return err
		}
		provenance = p
	}

	inputPath := *snapshotsPath
	if inputPath == "" {
		inputPath = *summaryPath
	}
	if strings.TrimSpace(inputPath) == "" {
		return errs.New(errs.CategoryInvalidArgument, "--snapshots or --summary is required")
	}

	report, falsifications, err := loadAggregatedReport(inputPath)
	if err != nil {
		return err
	}

	criteria := gate.DefaultM4GateCriteria()
	if *criteriaPath != "" {
		critData, err := os.ReadFile(*criteriaPath)
		if err != nil {
			return errs.Wrap(errs.CategoryNotFound, err, "failed to read criteria file %q", *criteriaPath)
		}
		if err := json.Unmarshal(critData, &criteria); err != nil {
			return errs.Wrap(errs.CategoryInvalidArgument, err, "malformed criteria JSON in %q", *criteriaPath)
		}
	}

	evalRes, err := gate.EvaluateM4Gate(report, falsifications, criteria)
	if err != nil {
		return err
	}
	evalRes.Provenance = provenance

	var outputContent string
	if *asJSON {
		jsonBytes, err := json.MarshalIndent(evalRes, "", "  ")
		if err != nil {
			return errs.Wrap(errs.CategoryInternal, err, "failed to marshal gate evaluation result")
		}
		outputContent = string(jsonBytes)
	} else {
		mdReport, err := gate.SynthesizeEvidenceReport(evalRes)
		if err != nil {
			return errs.Wrap(errs.CategoryInternal, err, "failed to synthesize evidence report")
		}
		outputContent = mdReport
	}

	if *outputPath != "" {
		if err := os.MkdirAll(filepath.Dir(*outputPath), 0o755); err != nil {
			return errs.Wrap(errs.CategoryInternal, err, "failed to create output directory")
		}
		if err := os.WriteFile(*outputPath, []byte(outputContent), 0o644); err != nil {
			return errs.Wrap(errs.CategoryInternal, err, "failed to write output file %q", *outputPath)
		}
		fmt.Fprintf(e.stdout, "evidence_kind=%s\n", provenanceKind(provenance))
		if provenance != nil {
			fmt.Fprintf(e.stdout, "driver=%s\n%s\n", provenance.Driver, provenance.Statement)
		}
	} else {
		fmt.Fprintln(e.stdout, outputContent)
	}

	// Exit codes: 0 for DecisionGo, 1 for DecisionRevise, 2 for DecisionInconclusive (REQ-07)
	switch evalRes.Decision {
	case gate.DecisionGo:
		return nil
	case gate.DecisionRevise:
		return &gateExitError{code: 1, msg: fmt.Sprintf("gate evaluation resulted in %s", evalRes.Decision)}
	case gate.DecisionInconclusive:
		return &gateExitError{code: 2, msg: fmt.Sprintf("gate evaluation resulted in %s", evalRes.Decision)}
	default:
		return &gateExitError{code: 1, msg: fmt.Sprintf("unknown gate decision %s", evalRes.Decision)}
	}
}

// runBenchmarkReport implements `devcadence benchmark report --summary <path> --output <path> [--json]`
func runBenchmarkReport(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("benchmark report", flag.ContinueOnError)
	summaryPath := fs.String("summary", "", "path to summary, snapshots, or aggregated report JSON")
	snapshotsPath := fs.String("snapshots", "", "alias for --summary")
	outputPath := fs.String("output", "", "path to write output report (optional, defaults to stdout)")
	asJSON := fs.Bool("json", false, "emit report as JSON")

	if err := parseFlags(fs, e, args); err != nil {
		return err
	}

	inputPath := *summaryPath
	if inputPath == "" {
		inputPath = *snapshotsPath
	}
	if strings.TrimSpace(inputPath) == "" {
		return errs.New(errs.CategoryInvalidArgument, "--summary or --snapshots is required")
	}

	report, _, err := loadAggregatedReport(inputPath)
	if err != nil {
		return err
	}

	var outputContent string
	if *asJSON {
		bytes, err := telemetry.GenerateJSONReport(report)
		if err != nil {
			return errs.Wrap(errs.CategoryInternal, err, "failed to generate JSON report")
		}
		outputContent = string(bytes)
	} else {
		md, err := telemetry.GenerateMarkdownReport(report)
		if err != nil {
			return errs.Wrap(errs.CategoryInternal, err, "failed to generate markdown report")
		}
		outputContent = md
	}

	if *outputPath != "" {
		if err := os.MkdirAll(filepath.Dir(*outputPath), 0o755); err != nil {
			return errs.Wrap(errs.CategoryInternal, err, "failed to create output directory")
		}
		if err := os.WriteFile(*outputPath, []byte(outputContent), 0o644); err != nil {
			return errs.Wrap(errs.CategoryInternal, err, "failed to write output file %q", *outputPath)
		}
	} else {
		fmt.Fprintln(e.stdout, outputContent)
	}

	return nil
}

// runBenchmarkGate implements `devcadence benchmark gate --summary <path> --criteria <path> --output <path>`
func runBenchmarkGate(ctx context.Context, e *env, args []string) error {
	// Re-uses evaluate-gate logic
	return runBenchmarkEvaluateGate(ctx, e, args)
}

func provenanceKind(p *gate.EvidenceProvenance) string {
	if p == nil {
		return "unspecified"
	}
	return p.Kind
}
