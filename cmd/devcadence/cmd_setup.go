package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/schema"
	"github.com/olostan/DevCadence/internal/setup"
)

func runSetup(ctx context.Context, e *env, args []string) error {
	if len(args) == 0 {
		usageSetup(e.stderr)
		return errs.New(errs.CategoryInvalidArgument, "a setup subcommand (plan, apply, recover) is required")
	}
	switch args[0] {
	case "plan":
		return runSetupPlan(ctx, e, args[1:])
	case "apply":
		return runSetupApply(ctx, e, args[1:])
	case "recover":
		return runSetupRecover(ctx, e, args[1:])
	case "-h", "--help", "help":
		usageSetup(e.stdout)
		return errFlagHelp
	default:
		usageSetup(e.stderr)
		return errs.New(errs.CategoryInvalidArgument, "unknown setup subcommand %q", args[0])
	}
}

func usageSetup(w io.Writer) {
	fmt.Fprintln(w, "usage: devcadence setup <plan|apply|recover> [flags]")
	fmt.Fprintln(w, "\nTwo-Step Approval Workflow:")
	fmt.Fprintln(w, "  1. Generate an immutable plan:")
	fmt.Fprintln(w, "     devcadence setup plan [target] --output <file>")
	fmt.Fprintln(w, "  2. Review and apply the approved plan:")
	fmt.Fprintln(w, "     devcadence setup apply --plan <file> --approve-plan <sha256:digest> [--yes]")
	fmt.Fprintln(w, "\nSubcommands:")
	fmt.Fprintln(w, "  plan     generate an immutable SetupPlan for a target scope")
	fmt.Fprintln(w, "  apply    review and execute an approved SetupPlan")
	fmt.Fprintln(w, "  recover  reconcile interrupted actions from the setup ledger")
	fmt.Fprintln(w, "\nExit codes:")
	fmt.Fprintln(w, "  0  No-op / plan empty / setup succeeded")
	fmt.Fprintln(w, "  1  Execution failure (action failed)")
	fmt.Fprintln(w, "  2  Invalid command-line arguments or digest mismatch")
	fmt.Fprintln(w, "  3  Plan file not found")
	fmt.Fprintln(w, "  4  Precondition drift or interrupted ledger run (re-plan required)")
	fmt.Fprintln(w, "  5  Plan file is malformed, semantically invalid, or fails schema validation")
	fmt.Fprintln(w, "  6  Plan generated with pending actions")
}

func runSetupPlan(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("setup plan", flag.ContinueOnError)
	outputFile := fs.String("output", "", "path to write the generated SetupPlan JSON file")
	asJSON := fs.Bool("json", false, "emit the generated SetupPlan as JSON to stdout")
	profileFlag := fs.String("profile", "", "target deployment profile (e.g. local-heavy, hybrid-thin, cloud-cognition, offline, custom)")
	depthFlag := fs.String("depth", string(protocol.DepthHealth), "probe depth: inventory or health")
	targetFlag := fs.String("target", "", "setup target: all, hardware, inference, cognition, auth")
	_ = fs.Bool("no-tui", false, "compatibility flag; runs without rich TUI rendering")
	fs.Usage = func() {
		fmt.Fprintf(e.stderr, "usage: devcadence setup plan [target] [flags]\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := parseFlags(fs, e, reorderArgs(args)); err != nil {
		return err
	}

	if fs.NArg() > 1 {
		return errs.New(errs.CategoryInvalidArgument,
			"setup plan: unexpected extra arguments after target: %v", fs.Args()[1:])
	}
	targetStr := *targetFlag
	if fs.NArg() == 1 {
		if targetStr != "" && targetStr != fs.Arg(0) {
			return errs.New(errs.CategoryInvalidArgument,
				"setup plan: conflicting target: positional argument %q conflicts with --target %q", fs.Arg(0), targetStr)
		}
		targetStr = fs.Arg(0)
	}
	if targetStr == "" {
		targetStr = string(protocol.TargetAll)
	}
	target := protocol.SetupTarget(targetStr)
	if !target.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "invalid setup target %q", targetStr)
	}

	depth, err := parseDepth(*depthFlag)
	if err != nil {
		return err
	}
	if depth == protocol.DepthInference {
		return errs.New(errs.CategoryInvalidArgument,
			"setup plan does not run inference; use `cognition probe` to verify an endpoint")
	}

	doc, facts, err := e.buildDoctor(ctx, depth)
	if err != nil {
		return err
	}

	scope := protocol.ReadinessEvaluationScope{
		RequiredRoles:  []string{},
		EvidenceStatus: "live",
	}
	if *profileFlag != "" {
		p := protocol.DeploymentProfile(*profileFlag)
		if !p.Valid() {
			return errs.New(errs.CategoryInvalidArgument, "invalid deployment profile %q", *profileFlag)
		}
		scope.TargetProfile = &p
	}

	report, err := doc.Run(ctx, scope, facts)
	if err != nil {
		return err
	}

	planner, err := setup.NewPlanner(setup.PlannerOptions{
		Clock: clock.System(),
		IDs:   ids.NewULIDSource(),
		Facts: &facts,
	})
	if err != nil {
		return err
	}

	var depProfile protocol.DeploymentProfile
	if *profileFlag != "" {
		depProfile = protocol.DeploymentProfile(*profileFlag)
	}

	plan, err := planner.PlanWithContext(ctx, report, target, depProfile)
	if err != nil {
		return err
	}
	if err := plan.Validate(); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "generated plan failed internal validation")
	}

	planBytes, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "marshal plan")
	}
	planBytes = append(planBytes, '\n')

	set, err := schema.Default()
	if err != nil {
		return err
	}
	if err := set.ValidateBytes(schema.NameSetupPlan, planBytes); err != nil {
		return errs.Wrap(errs.CategoryIntegrity, err, "generated plan failed schema validation")
	}

	if *outputFile != "" {
		if err := os.WriteFile(*outputFile, planBytes, 0o600); err != nil {
			return errs.Wrap(errs.CategoryInvalidArgument, err, "write plan to %s", *outputFile)
		}
	}

	if *asJSON {
		if err := writeJSON(e.stdout, plan); err != nil {
			return err
		}
	} else {
		renderDoctorFixSummary(e.stdout, plan, *outputFile)
	}

	return planActionsExitError(plan)
}

func runSetupApply(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("setup apply", flag.ContinueOnError)
	planFile := fs.String("plan", "", "path to the SetupPlan JSON file to apply (required)")
	approvePlan := fs.String("approve-plan", "", "SHA-256 digest of the approved plan")
	yesFlag := fs.Bool("yes", false, "authorize user_confirmation actions non-interactively")
	asJSON := fs.Bool("json", false, "emit SetupExecutionReport as JSON to stdout")
	_ = fs.Bool("no-tui", false, "compatibility flag; runs without rich TUI rendering")
	fs.Usage = func() {
		fmt.Fprintf(e.stderr, "usage: devcadence setup apply --plan <file> [flags]\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := parseFlags(fs, e, reorderArgs(args)); err != nil {
		return err
	}

	if *planFile == "" {
		return errs.New(errs.CategoryInvalidArgument, "setup apply: --plan <file> is required")
	}

	plan, set, err := loadSetupPlanArtifact(*planFile)
	if err != nil {
		return err
	}

	approvedDigest := *approvePlan
	if approvedDigest != "" {
		if approvedDigest != plan.PlanDigest {
			return errs.New(errs.CategoryInvalidArgument,
				"setup apply: approved digest %q does not match plan digest %q", approvedDigest, plan.PlanDigest)
		}
	} else {
		if !e.isTerminal() {
			return errs.New(errs.CategoryInvalidArgument,
				"setup apply: --approve-plan <digest> is required in non-interactive mode")
		}
		renderPlanSummaryForApproval(e.stdout, plan)
		fmt.Fprintf(e.stdout, "\nApprove plan %s? [y/N]: ", plan.PlanDigest)
		var input string
		if e.stdin != nil {
			reader := bufio.NewReader(e.stdin)
			line, _ := reader.ReadString('\n')
			input = strings.TrimSpace(line)
		}
		if !strings.EqualFold(input, "y") && !strings.EqualFold(input, "yes") {
			return errs.New(errs.CategoryPolicyDenied, "plan approval declined by operator")
		}
		approvedDigest = plan.PlanDigest
	}

	home := e.homeDir()
	executor, err := setup.NewExecutor(setup.ExecutorOptions{
		Runner: process.NewRunner(),
		Home:   home,
		Clock:  clock.System(),
		IDs:    ids.NewULIDSource(),
	})
	if err != nil {
		return err
	}

	report, applyErr := executor.Apply(ctx, plan, approvedDigest, *yesFlag)

	if *asJSON {
		if report != nil {
			reportBytes, err := json.Marshal(report)
			if err != nil {
				return errs.Wrap(errs.CategoryInternal, err, "marshal execution report")
			}
			if err := set.ValidateBytes(schema.NameSetupExecutionReport, reportBytes); err != nil {
				return errs.Wrap(errs.CategoryIntegrity, err, "execution report failed schema validation")
			}
			if err := writeJSON(e.stdout, report); err != nil {
				return err
			}
		}
	} else {
		if report != nil {
			renderSetupExecutionReport(e.stdout, report)
		}
	}

	return applyErr
}

func runSetupRecover(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("setup recover", flag.ContinueOnError)
	planFile := fs.String("plan", "", "path to the SetupPlan JSON file to recover (required)")
	asJSON := fs.Bool("json", false, "emit recovery statuses as JSON")
	_ = fs.Bool("no-tui", false, "compatibility flag; runs without rich TUI rendering")
	if err := parseFlags(fs, e, reorderArgs(args)); err != nil {
		return err
	}

	if *planFile == "" {
		return errs.New(errs.CategoryInvalidArgument, "setup recover: --plan <file> is required")
	}

	plan, set, err := loadSetupPlanArtifact(*planFile)
	if err != nil {
		return err
	}

	home := e.homeDir()
	executor, err := setup.NewExecutor(setup.ExecutorOptions{
		Runner: process.NewRunner(),
		Home:   home,
		Clock:  clock.System(),
		IDs:    ids.NewULIDSource(),
	})
	if err != nil {
		return err
	}

	results, err := executor.Recover(ctx, plan)
	if err != nil {
		return err
	}
	if results == nil {
		results = []protocol.RecoveryActionResult{}
	}

	if *asJSON {
		report := &protocol.SetupRecoveryReport{
			SchemaVersion: protocol.SchemaVersion1,
			RecoveryID:    ids.NewULIDSource().New("recovery"),
			PlanID:        plan.PlanID,
			PlanDigest:    plan.PlanDigest,
			RecoveredAt:   protocol.NewTimestamp(clock.System().Now()),
			Results:       results,
		}
		if err := report.Validate(); err != nil {
			return errs.Wrap(errs.CategoryInternal, err, "generated recovery report failed internal validation")
		}
		reportBytes, err := json.Marshal(report)
		if err != nil {
			return errs.Wrap(errs.CategoryInternal, err, "marshal recovery report")
		}
		if err := set.ValidateBytes(schema.NameSetupRecoveryReport, reportBytes); err != nil {
			return errs.Wrap(errs.CategoryIntegrity, err, "recovery report failed schema validation")
		}
		return writeJSON(e.stdout, report)
	}

	fmt.Fprintf(e.stdout, "Setup Recovery Reconciled\n")
	fmt.Fprintf(e.stdout, "Plan ID:        %s\n", plan.PlanID)
	fmt.Fprintf(e.stdout, "Plan Digest:    %s\n", plan.PlanDigest)
	fmt.Fprintf(e.stdout, "Actions:        %d\n", len(results))
	for i, res := range results {
		fmt.Fprintf(e.stdout, "  %d. %s: %s\n", i+1, res.ActionID, res.Status)
	}
	return nil
}

// loadSetupPlanArtifact reads, decodes, semantically validates, and schema-
// validates a SetupPlan file from disk. It is the single boundary both
// `setup apply` and `setup recover` load a plan artifact through, so the
// two commands cannot drift on how a bad artifact is classified.
//
// A missing file is CategoryNotFound (exit 3): the operator simply pointed
// at the wrong path. Everything else that is wrong about a file that does
// exist — malformed JSON, a schema_version this build cannot interpret, a
// document that fails SetupPlan's semantic Validate(), or one that fails
// setup-plan.schema.json — is CategoryIntegrity (exit 5): the artifact
// itself is not a plan this build can trust, which is a different failure
// than a bad CLI argument (exit 2) and must not be reported as one
// (independent-review follow-up on WP-M3B-7, FIX_NOW-1).
func loadSetupPlanArtifact(path string) (*protocol.SetupPlan, *schema.Set, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, errs.New(errs.CategoryNotFound, "plan file %q not found", path)
		}
		return nil, nil, errs.Wrap(errs.CategoryInvalidArgument, err, "read plan file %s", path)
	}

	var plan protocol.SetupPlan
	if err := protocol.Unmarshal(data, &plan); err != nil {
		return nil, nil, errs.Wrap(errs.CategoryIntegrity, err, "plan file %s is not a valid SetupPlan", path)
	}

	set, err := schema.Default()
	if err != nil {
		return nil, nil, err
	}
	if err := set.ValidateBytes(schema.NameSetupPlan, data); err != nil {
		return nil, nil, errs.Wrap(errs.CategoryIntegrity, err, "plan file %s failed schema validation", path)
	}

	return &plan, set, nil
}

func renderPlanSummaryForApproval(w interface{ Write([]byte) (int, error) }, plan *protocol.SetupPlan) {
	fmt.Fprintf(w, "Setup Plan Details:\n")
	fmt.Fprintf(w, "  Plan ID:            %s\n", plan.PlanID)
	fmt.Fprintf(w, "  Plan Digest:        %s\n", plan.PlanDigest)
	fmt.Fprintf(w, "  Target:             %s\n", plan.Target)
	fmt.Fprintf(w, "  Required Authority: %s\n", plan.RequiredAuthority)
	if len(plan.TotalEffects) > 0 {
		effects := make([]string, len(plan.TotalEffects))
		for i, eff := range plan.TotalEffects {
			effects[i] = string(eff)
		}
		fmt.Fprintf(w, "  Total Effects:      %s\n", strings.Join(effects, ", "))
	}
	fmt.Fprintf(w, "  Actions (%d):\n", len(plan.Actions))
	for i, a := range plan.Actions {
		kind := "manual"
		if a.Operation != nil {
			kind = string(a.Operation.Kind)
		}
		fmt.Fprintf(w, "    %d. [%-15s] %-25s (%s)\n", i+1, a.Authority, a.RecipeID, kind)
		if a.Description != "" {
			fmt.Fprintf(w, "       %s\n", a.Description)
		}
	}
}

func renderSetupExecutionReport(w interface{ Write([]byte) (int, error) }, report *protocol.SetupExecutionReport) {
	fmt.Fprintf(w, "Setup Execution Report\n")
	fmt.Fprintf(w, "Execution ID: %s\n", report.ExecutionID)
	fmt.Fprintf(w, "Plan ID:      %s\n", report.PlanID)
	fmt.Fprintf(w, "Plan Digest:  %s\n", report.PlanDigest)
	fmt.Fprintf(w, "Status:       %s\n", report.Status)
	fmt.Fprintf(w, "Started At:   %s\n", report.StartedAt.String())
	if report.CompletedAt != nil {
		fmt.Fprintf(w, "Completed At: %s\n", report.CompletedAt.String())
	}
	fmt.Fprintf(w, "\nAction Results (%d):\n", len(report.Results))
	for i, r := range report.Results {
		fmt.Fprintf(w, "  %d. [%-10s] %s\n", i+1, r.Status, r.ActionID)
		if r.ExitCode != nil {
			fmt.Fprintf(w, "       Exit Code: %d\n", *r.ExitCode)
		}
		if r.FailureDetail != "" {
			fmt.Fprintf(w, "       Detail:    %s\n", r.FailureDetail)
		}
		if r.ArtifactRef != nil {
			fmt.Fprintf(w, "       Artifact:  %s (digest: %s)\n", r.ArtifactRef.ID, r.ArtifactRef.Digest)
		}
	}
	if report.Status == protocol.ExecutionStatusSucceeded {
		fmt.Fprintf(w, "\nSetup completed successfully.\n")
	} else {
		fmt.Fprintf(w, "\nSetup execution finished with status: %s\n", report.Status)
	}
}

func reorderArgs(args []string) []string {
	var flags, positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			if !strings.Contains(arg, "=") && (arg == "-output" || arg == "--output" ||
				arg == "-target" || arg == "--target" ||
				arg == "-profile" || arg == "--profile" ||
				arg == "-depth" || arg == "--depth" ||
				arg == "-scope" || arg == "--scope" ||
				arg == "-plan" || arg == "--plan" ||
				arg == "-approve-plan" || arg == "--approve-plan") {
				if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
					i++
					flags = append(flags, args[i])
				}
			}
		} else {
			positionals = append(positionals, arg)
		}
	}
	return append(flags, positionals...)
}
