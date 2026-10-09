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
	"github.com/olostan/DevCadence/internal/benchmark/empirical"
	"github.com/olostan/DevCadence/internal/benchmark/empirical/verifier"
	"github.com/olostan/DevCadence/internal/benchmark/experiments"
	"github.com/olostan/DevCadence/internal/benchmark/gate"
	"github.com/olostan/DevCadence/internal/benchmark/telemetry"
	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/execrt"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/operator/receipts"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/repository"
	"github.com/olostan/DevCadence/internal/worktrees"
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
		return errs.New(errs.CategoryInvalidArgument, "usage: devcadence benchmark <evaluate-gate|report|gate|replay-empirical>")
	}
	switch args[0] {
	case "evaluate-gate":
		return runBenchmarkEvaluateGate(ctx, e, args[1:])
	case "report":
		return runBenchmarkReport(ctx, e, args[1:])
	case "gate":
		return runBenchmarkGate(ctx, e, args[1:])
	case "replay-empirical":
		return runBenchmarkReplayEmpirical(ctx, e, args[1:])
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

	if *evidenceKind == "empirical_campaign" || *evidenceKind == gate.EvidenceKindEmpiricalCampaign {
		return &gateExitError{
			code: 4,
			msg:  "empirical campaign evidence must be admitted through 'benchmark replay-empirical'",
		}
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

// defaultNewIndependentVerifier constructs the production independent verifier (WP-M5-R3 Part B).
var defaultNewIndependentVerifier = func(ctx context.Context, e *env, resolver empirical.ArtifactResolver, repoDir string) (empirical.IndependentVerifier, error) {
	worktreeRoot := filepath.Join(e.homeDir(), "worktrees")
	wtMgr, err := worktrees.NewManager(worktreeRoot, process.NewRunner())
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "failed to create worktree manager")
	}

	if repoDir == "" {
		repoDir = "."
	}
	repo, err := repository.Register(ctx, "devcadence", repoDir, repository.Options{})
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "invalid verifier repository root %q", repoDir)
	}
	repoProvider := execrt.NewSingleRepositoryProvider(repo)

	opts := verifier.Options{
		Resolver:     resolver,
		Worktrees:    wtMgr,
		Repositories: repoProvider,
		Runner:       process.NewRunner(),
		BuildInfo:    verifier.DefaultBuildInfoSource(),
		Clock:        clock.System(),
		IDs:          ids.NewULIDSource(),
		ScratchDir:   filepath.Join(e.homeDir(), "verifier-scratch"),
	}
	return verifier.New(opts)
}

// newIndependentVerifier seam may be overridden by tests to inject test verification runners.
var newIndependentVerifier = defaultNewIndependentVerifier

// directoryArtifactResolver implements empirical.ArtifactResolver over a local digest-addressed directory.
type directoryArtifactResolver struct {
	dir   string
	extra map[string][]byte
}

func newDirectoryArtifactResolver(dir string, extra map[string][]byte) *directoryArtifactResolver {
	return &directoryArtifactResolver{
		dir:   dir,
		extra: extra,
	}
}

func (r *directoryArtifactResolver) ReadVerified(ctx context.Context, ref, digest string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if r.extra != nil {
		if b, ok := r.extra[ref]; ok {
			if digest == "" || protocol.DigestBytes(b) == digest {
				return b, nil
			}
		}
		if digest != "" {
			if b, ok := r.extra[digest]; ok {
				if protocol.DigestBytes(b) == digest {
					return b, nil
				}
			}
		}
	}

	var candidates []string
	if ref != "" {
		candidates = append(candidates,
			filepath.Join(r.dir, ref),
			filepath.Join(r.dir, filepath.Base(ref)),
			filepath.Join(r.dir, ref+".json"),
			filepath.Join(r.dir, filepath.Base(ref)+".json"),
		)
		if strings.HasPrefix(ref, "sha256:") {
			trimmed := strings.TrimPrefix(ref, "sha256:")
			candidates = append(candidates,
				filepath.Join(r.dir, trimmed),
				filepath.Join(r.dir, trimmed+".json"),
			)
		}
	}
	if digest != "" {
		trimmed := strings.TrimPrefix(digest, "sha256:")
		candidates = append(candidates,
			filepath.Join(r.dir, digest),
			filepath.Join(r.dir, trimmed),
			filepath.Join(r.dir, digest+".json"),
			filepath.Join(r.dir, trimmed+".json"),
		)
	}

	absDir, err := filepath.Abs(r.dir)
	if err != nil {
		absDir = r.dir
	}

	for _, cand := range candidates {
		absCand, err := filepath.Abs(cand)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(absDir, absCand)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		data, err := os.ReadFile(absCand)
		if err != nil {
			continue
		}
		if digest != "" {
			if protocol.DigestBytes(data) == digest {
				return data, nil
			}
		} else {
			return data, nil
		}
	}

	if digest != "" {
		var foundData []byte
		_ = filepath.Walk(absDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return nil
			}
			if info.Size() > 64*1024*1024 {
				return nil
			}
			name := info.Name()
			trimmed := strings.TrimPrefix(digest, "sha256:")
			if name == digest || name == trimmed || name == digest+".json" || name == trimmed+".json" || name == ref || name == filepath.Base(ref) {
				data, err := os.ReadFile(path)
				if err == nil && protocol.DigestBytes(data) == digest {
					foundData = data
					return filepath.SkipAll
				}
			}
			return nil
		})
		if len(foundData) > 0 {
			return foundData, nil
		}
	}

	return nil, errs.New(errs.CategoryNotFound, "artifact %q (%s) not found in %q", ref, digest, r.dir)
}

// loadOperatorVerifier loads the operator receipt verifier with strict filesystem protection checks (B1).
// It does NOT load anchors from artifactsDir.
// Package tests may override this seam to inject in-memory verifiers for testing.
var loadOperatorVerifier = defaultLoadOperatorVerifier

func defaultLoadOperatorVerifier(operatorDir, artifactsDir, homeDir string) (receipts.Verifier, error) {
	opts := receipts.FileOptions{
		OperatorDir:      operatorDir,
		TrustedOwnerUIDs: []uint32{0},
		OperatorUID:      0,
		Clock:            clock.System(),
	}
	if homeDir != "" {
		opts.ReceiptsDir = filepath.Join(homeDir, "receipts")
	}
	return receipts.NewFileVerifier(opts)
}

func formatEmpiricalReport(rep *empirical.Report) (string, error) {
	var sb strings.Builder
	sb.WriteString("# Milestone M4 Empirical Campaign Evidence Report\n\n")
	sb.WriteString(fmt.Sprintf("- **Campaign ID:** `%s`\n", rep.CampaignID))
	sb.WriteString(fmt.Sprintf("- **Plan Digest:** `%s`\n", rep.PlanDigest))
	sb.WriteString(fmt.Sprintf("- **Source Commit:** `%s`\n", rep.SourceCommit))
	sb.WriteString(fmt.Sprintf("- **Evidence Provenance:** `%s`\n", rep.Provenance))
	sb.WriteString(fmt.Sprintf("- **Empirical Conclusion:** %s\n", strings.ToUpper(rep.Conclusion)))

	if len(rep.ConclusionReasons) > 0 {
		sb.WriteString("\n## Conclusion Reasons\n\n")
		for _, r := range rep.ConclusionReasons {
			sb.WriteString(fmt.Sprintf("- %s\n", r))
		}
	}

	sb.WriteString("\n## Admission Result\n\n")
	sb.WriteString(fmt.Sprintf("- **Admitted:** %t\n", rep.Admission.Admitted))
	if len(rep.Admission.ReasonCodes) > 0 {
		sb.WriteString(fmt.Sprintf("- **Reason Codes:** %s\n", strings.Join(rep.Admission.ReasonCodes, ", ")))
	}
	if len(rep.Admission.Limitations) > 0 {
		sb.WriteString("### Limitations\n\n")
		for _, l := range rep.Admission.Limitations {
			sb.WriteString(fmt.Sprintf("- %s\n", l))
		}
	}

	if rep.RawGate != nil {
		sb.WriteString("\n## Raw Gate Evaluation\n\n")
		sb.WriteString(fmt.Sprintf("- **Gate Decision:** %s\n", rep.RawGate.Decision))
		rawMD, err := gate.SynthesizeEvidenceReport(rep.RawGate)
		if err == nil && rawMD != "" {
			sb.WriteString("\n")
			sb.WriteString(rawMD)
		}
	} else if rep.RawGateSkipped != "" {
		sb.WriteString("\n## Raw Gate Evaluation Skipped\n\n")
		sb.WriteString(fmt.Sprintf("- **Reason:** %s\n", rep.RawGateSkipped))
	}

	return sb.String(), nil
}

// runBenchmarkReplayEmpirical implements `devcadence benchmark replay-empirical` (WP-M5-R3 Part B).
// Flags:
//
//	--manifest <path> (required)
//	--plan <path> (required)
//	--authorization <path> (required)
//	--artifacts <dir> (required)
//	--criteria <path> (required)
//	--output <path> (optional, defaults to stdout)
//	--json (optional boolean)
//
// Exit codes:
//
//	0: GATE_PASS (ConclusionGo)
//	1: GATE_FAIL (ConclusionRevise)
//	2: GATE_INDETERMINATE (ConclusionInconclusive)
//	3: Refused admission
func runBenchmarkReplayEmpirical(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("benchmark replay-empirical", flag.ContinueOnError)
	manifestPath := fs.String("manifest", "", "path to CampaignManifest JSON file (required)")
	planPath := fs.String("plan", "", "path to CampaignPlan JSON file (required)")
	authPath := fs.String("authorization", "", "path to CampaignAuthorization JSON file (required)")
	artifactsDir := fs.String("artifacts", "", "directory containing digest-addressed artifacts (required)")
	criteriaPath := fs.String("criteria", "", "path to GateCriteria JSON file (required)")
	outputPath := fs.String("output", "", "path to write output report (optional, defaults to stdout)")
	asJSON := fs.Bool("json", false, "emit evaluation report as JSON")
	operatorDir := fs.String("operator-dir", "", "optional directory containing operator anchors.json")
	repoPath := fs.String("repo", "", "path to git repository root (optional, defaults to current working directory)")

	if err := parseFlags(fs, e, args); err != nil {
		return err
	}

	if strings.TrimSpace(*manifestPath) == "" {
		return errs.New(errs.CategoryInvalidArgument, "--manifest is required")
	}
	if strings.TrimSpace(*planPath) == "" {
		return errs.New(errs.CategoryInvalidArgument, "--plan is required")
	}
	if strings.TrimSpace(*authPath) == "" {
		return errs.New(errs.CategoryInvalidArgument, "--authorization is required")
	}
	if strings.TrimSpace(*artifactsDir) == "" {
		return errs.New(errs.CategoryInvalidArgument, "--artifacts is required")
	}
	if strings.TrimSpace(*criteriaPath) == "" {
		return errs.New(errs.CategoryInvalidArgument, "--criteria is required")
	}

	// 1. Parse flags and load files.
	manifestData, err := os.ReadFile(*manifestPath)
	if err != nil {
		return errs.Wrap(errs.CategoryNotFound, err, "failed to read manifest file %q", *manifestPath)
	}
	var manifest empirical.CampaignManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "malformed manifest JSON in %q", *manifestPath)
	}

	planData, err := os.ReadFile(*planPath)
	if err != nil {
		return errs.Wrap(errs.CategoryNotFound, err, "failed to read plan file %q", *planPath)
	}
	var plan empirical.CampaignPlan
	if err := json.Unmarshal(planData, &plan); err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "malformed plan JSON in %q", *planPath)
	}

	authData, err := os.ReadFile(*authPath)
	if err != nil {
		return errs.Wrap(errs.CategoryNotFound, err, "failed to read authorization file %q", *authPath)
	}
	var authz empirical.CampaignAuthorization
	if err := json.Unmarshal(authData, &authz); err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "malformed authorization JSON in %q", *authPath)
	}

	artInfo, err := os.Stat(*artifactsDir)
	if err != nil {
		return errs.Wrap(errs.CategoryNotFound, err, "artifacts directory %q not found", *artifactsDir)
	}
	if !artInfo.IsDir() {
		return errs.New(errs.CategoryInvalidArgument, "artifacts path %q is not a directory", *artifactsDir)
	}

	criteriaData, err := os.ReadFile(*criteriaPath)
	if err != nil {
		return errs.Wrap(errs.CategoryNotFound, err, "failed to read criteria file %q", *criteriaPath)
	}
	var criteria gate.GateCriteria
	if err := json.Unmarshal(criteriaData, &criteria); err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "malformed criteria JSON in %q", *criteriaPath)
	}

	if err := ctx.Err(); err != nil {
		fmt.Fprintf(e.stderr, "devcadence: empirical admission error: %v\n", err)
		return &gateExitError{
			code: 3,
			msg:  fmt.Sprintf("empirical admission error: %v", err),
		}
	}

	validatedRepoRoot := "."
	if strings.TrimSpace(*repoPath) != "" {
		absRepo, err := filepath.Abs(*repoPath)
		if err != nil {
			return errs.Wrap(errs.CategoryInvalidArgument, err, "failed to resolve --repo %q", *repoPath)
		}
		if _, err := repository.Register(ctx, "devcadence", absRepo, repository.Options{}); err != nil {
			return errs.Wrap(errs.CategoryInvalidArgument, err, "invalid repository root %q", absRepo)
		}
		validatedRepoRoot = absRepo
	}

	// 2. Initialize receipt verifier (receipts.NewVerifier / operator store).
	rcptVerifier, err := loadOperatorVerifier(*operatorDir, *artifactsDir, e.homeDir())
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "failed to initialize receipt verifier")
	}

	// 3. Instantiate verifier.NewCampaignAuthority(...).
	campaignAuth, err := verifier.NewCampaignAuthority(rcptVerifier, "devcadence", clock.System())
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "failed to instantiate campaign authority")
	}

	// 4 & 5. Instantiate artifact resolver reading from --artifacts directory.
	extra := map[string][]byte{
		*planPath:                      planData,
		manifest.PlanRef:               planData,
		manifest.PlanDigest:            planData,
		protocol.DigestBytes(planData): planData,
		*authPath:                      authData,
		manifest.AuthorizationRef:      authData,
		manifest.AuthorizationDigest:   authData,
		protocol.DigestBytes(authData): authData,
	}
	resolver := newDirectoryArtifactResolver(*artifactsDir, extra)

	// 4. Instantiate verifier.New(...) (empirical.IndependentVerifier).
	indepVerifier, err := newIndependentVerifier(ctx, e, resolver, validatedRepoRoot)
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "failed to instantiate independent verifier")
	}

	// 6. Instantiate empirical.NewAdmitter(...).
	admitter, err := empirical.NewAdmitter(empirical.AdmitterOptions{
		Authority: campaignAuth,
		Verifier:  indepVerifier,
		Resolver:  resolver,
		Clock:     clock.System(),
	})
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "failed to instantiate admitter")
	}

	// 7. Execute admitter.Admit(ctx, manifest).
	admissionResult, err := admitter.Admit(ctx, manifest)
	if err != nil {
		fmt.Fprintf(e.stderr, "devcadence: empirical admission error: %v\n", err)
		return &gateExitError{
			code: 3,
			msg:  fmt.Sprintf("empirical admission failed: %v", err),
		}
	}

	// 8. If admission refused/invalid: report refusal reason and exit with code 3.
	if !admissionResult.Admitted {
		reasons := strings.Join(admissionResult.ReasonCodes, ", ")
		if reasons == "" {
			reasons = "admission refused without reason codes"
		}
		fmt.Fprintf(e.stderr, "devcadence: empirical admission refused: %s\n", reasons)
		for _, code := range admissionResult.ReasonCodes {
			fmt.Fprintf(e.stderr, " - reason: %s\n", code)
		}
		return &gateExitError{
			code: 3,
			msg:  fmt.Sprintf("empirical admission refused: %s", reasons),
		}
	}

	// 9. If admitted: evaluate empirical.ReplayGate(criteria, admissionResult).
	rep, err := empirical.ReplayGate(admissionResult, criteria)
	if err != nil {
		return err
	}

	// 10. Write gate report to --output (formatted or JSON).
	var outputContent string
	if *asJSON {
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return errs.Wrap(errs.CategoryInternal, err, "failed to marshal empirical report")
		}
		outputContent = string(b)
	} else {
		md, err := formatEmpiricalReport(rep)
		if err != nil {
			return errs.Wrap(errs.CategoryInternal, err, "failed to format empirical report")
		}
		outputContent = md
	}

	if *outputPath != "" && *outputPath != "-" {
		if err := os.MkdirAll(filepath.Dir(*outputPath), 0o755); err != nil {
			return errs.Wrap(errs.CategoryInternal, err, "failed to create output directory")
		}
		if err := os.WriteFile(*outputPath, []byte(outputContent), 0o644); err != nil {
			return errs.Wrap(errs.CategoryInternal, err, "failed to write output file %q", *outputPath)
		}
	} else {
		fmt.Fprintln(e.stdout, outputContent)
	}

	// 11. Exit code:
	//     0: GATE_PASS
	//     1: GATE_FAIL
	//     2: GATE_INDETERMINATE
	//     3: Refused admission (handled in step 8)
	switch rep.Conclusion {
	case empirical.ConclusionGo:
		return nil
	case empirical.ConclusionRevise:
		return &gateExitError{code: 1, msg: fmt.Sprintf("empirical gate conclusion: %s", rep.Conclusion)}
	case empirical.ConclusionInconclusive:
		return &gateExitError{code: 2, msg: fmt.Sprintf("empirical gate conclusion: %s", rep.Conclusion)}
	default:
		return &gateExitError{code: 1, msg: fmt.Sprintf("unknown empirical conclusion %s", rep.Conclusion)}
	}
}
