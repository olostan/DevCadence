package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/schema"
	"github.com/olostan/DevCadence/internal/setup"
)

// planGeneratedError signals that a non-empty SetupPlan was produced and is awaiting approval.
type planGeneratedError struct {
	plan *protocol.SetupPlan
}

func (e *planGeneratedError) Error() string {
	return fmt.Sprintf("plan %s generated with %d action(s)", e.plan.PlanID, len(e.plan.Actions))
}

func (e *planGeneratedError) ExitCode() int {
	return ExitCodePlanGenerated
}

func runDoctor(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the DoctorReport or SetupPlan as JSON")
	fix := fs.Bool("fix", false, "generate a SetupPlan addressing remediable findings")
	outputFile := fs.String("output", "", "file path to write the generated SetupPlan (with --fix) or report to")
	depthFlag := fs.String("depth", string(protocol.DepthHealth), "probe depth: inventory or health")
	scopeFlag := fs.String("scope", "default", "readiness evaluation scope identifier")
	profileFlag := fs.String("profile", "", "target deployment profile (e.g. local-heavy, hybrid-thin, cloud-cognition, offline, custom)")
	targetFlag := fs.String("target", string(protocol.TargetAll), "setup target when --fix is specified (all, hardware, inference, cognition, auth)")
	_ = fs.Bool("no-tui", false, "compatibility flag; runs without rich TUI rendering")
	fs.Usage = func() {
		fmt.Fprintf(e.stderr, "usage: devcadence doctor [flags]\n\nFlags:\n")
		fs.PrintDefaults()
		fmt.Fprintln(e.stderr, "\nExit codes:")
		fmt.Fprintln(e.stderr, "  0  Ready / no remediation needed")
		fmt.Fprintln(e.stderr, "  1  Action required / doctor diagnostics unready (without --fix)")
		fmt.Fprintln(e.stderr, "  2  Invalid command-line arguments")
		fmt.Fprintln(e.stderr, "  6  Plan generated (--fix produced pending actions requiring approval)")
	}
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}

	// No readiness-scope identifier other than "default" is implemented yet
	// (ReadinessEvaluationScope carries no scope-identifier field at all —
	// see internal/protocol/doctor.go). Rather than silently accepting and
	// discarding an operator-supplied value that has no effect, an explicit
	// non-default value is rejected deterministically (independent-review
	// follow-up on WP-M3B-7, FIX_NOW-3).
	if *scopeFlag != "default" {
		return errs.New(errs.CategoryInvalidArgument,
			"doctor: unsupported readiness scope %q (only \"default\" is currently supported)", *scopeFlag)
	}

	depth, err := parseDepth(*depthFlag)
	if err != nil {
		return err
	}
	if depth == protocol.DepthInference {
		return errs.New(errs.CategoryInvalidArgument,
			"doctor does not run inference; use `cognition probe` to verify an endpoint")
	}

	// --target is meaningful only when --fix is specified. An explicitly
	// supplied target without --fix has no effect on diagnosis or
	// readiness, so it is rejected deterministically as invalid CLI input
	// (independent-review follow-up on WP-M3B-7, FIX_NOW-3 residual).
	var targetSupplied bool
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "target" {
			targetSupplied = true
		}
	})
	if targetSupplied && !*fix {
		return errs.New(errs.CategoryInvalidArgument, "doctor: --target requires --fix")
	}

	target := protocol.SetupTarget(*targetFlag)
	if !target.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "invalid setup target %q", *targetFlag)
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

	if !*fix {
		if *outputFile != "" {
			reportBytes, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				return errs.Wrap(errs.CategoryInternal, err, "marshal doctor report")
			}
			reportBytes = append(reportBytes, '\n')
			if err := os.WriteFile(*outputFile, reportBytes, 0o600); err != nil {
				return errs.Wrap(errs.CategoryInvalidArgument, err, "write report to %s", *outputFile)
			}
		}

		if *asJSON {
			set, err := schema.Default()
			if err != nil {
				return err
			}
			reportBytes, err := json.Marshal(report)
			if err != nil {
				return errs.Wrap(errs.CategoryInternal, err, "marshal report for schema check")
			}
			if err := set.ValidateBytes(schema.NameDoctorReport, reportBytes); err != nil {
				return errs.Wrap(errs.CategoryIntegrity, err, "doctor report failed schema validation")
			}
			if err := writeJSON(e.stdout, report); err != nil {
				return err
			}
		} else {
			renderDoctorReport(e.stdout, report)
		}

		return doctorReadinessExitError(report)
	}

	// --fix: generate a SetupPlan
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

// doctorReadinessExitError maps a DoctorReport's Readiness to the doctor
// exit-code contract (0 ready, 1 action required), as a pure function of
// the report so it can be exercised deterministically without depending on
// real hardware discovery (independent-review follow-up on WP-M3B-7,
// FIX_NOW-4).
func doctorReadinessExitError(report *protocol.DoctorReport) error {
	if report.Readiness == protocol.ReadinessActionRequired || report.Readiness == protocol.ReadinessPartiallyReady {
		return errs.New(errs.CategoryValidationFailed,
			"doctor: readiness is %s; run 'devcadence doctor --fix' to plan remediation", report.Readiness)
	}
	return nil
}

// planActionsExitError maps a generated SetupPlan to the shared "plan
// generated" exit-code contract (0 no-op, 6 pending actions) used by both
// `doctor --fix` and `setup plan`, as a pure function of the plan so it can
// be exercised deterministically without depending on real hardware
// discovery (independent-review follow-up on WP-M3B-7, FIX_NOW-4).
func planActionsExitError(plan *protocol.SetupPlan) error {
	if len(plan.Actions) == 0 {
		return nil
	}
	return &planGeneratedError{plan: plan}
}

func (e *env) buildDoctor(ctx context.Context, depth protocol.ProbeDepth) (*setup.Doctor, protocol.EnvironmentFacts, error) {
	facts, err := discoverFacts(ctx, depth)
	if err != nil {
		return nil, protocol.EnvironmentFacts{}, err
	}
	cognitionService, err := newCognitionService()
	if err != nil {
		return nil, protocol.EnvironmentFacts{}, err
	}
	home := e.homeDir()
	cacheMgr, err := setup.NewCacheManager(filepath.Join(home, "state"), clock.System(), setup.DefaultCacheTTL)
	if err != nil {
		return nil, protocol.EnvironmentFacts{}, err
	}
	doc, err := setup.NewDoctor(setup.DoctorOptions{
		Clock:            clock.System(),
		IDs:              ids.NewULIDSource(),
		HomeDir:          home,
		CognitionService: cognitionService,
		Cache:            cacheMgr,
	})
	if err != nil {
		return nil, protocol.EnvironmentFacts{}, err
	}
	return doc, facts, nil
}

func renderDoctorReport(w interface{ Write([]byte) (int, error) }, report *protocol.DoctorReport) {
	fmt.Fprintf(w, "DevCadence Doctor Report\n")
	fmt.Fprintf(w, "Machine Fingerprint: %s\n", report.MachineFingerprint)
	fmt.Fprintf(w, "Readiness:           %s\n", report.Readiness)
	if report.EvaluationScope.TargetProfile != nil {
		fmt.Fprintf(w, "Target Profile:      %s\n", *report.EvaluationScope.TargetProfile)
	} else {
		fmt.Fprintf(w, "Target Profile:      (none)\n")
	}
	fmt.Fprintf(w, "Observed At:         %s\n", report.ObservedAt.String())

	if len(report.Findings) > 0 {
		fmt.Fprintf(w, "\nFindings (%d):\n", len(report.Findings))
		for _, f := range report.Findings {
			fmt.Fprintf(w, "  [%-7s] %s: %s\n", strings.ToUpper(string(f.Severity)), f.Code, f.Title)
			if f.Detail != "" {
				fmt.Fprintf(w, "            Detail: %s\n", f.Detail)
			}
			if f.Remediation != nil && *f.Remediation != "" {
				fmt.Fprintf(w, "            Remediation: %s\n", *f.Remediation)
			}
		}
	} else {
		fmt.Fprintf(w, "\nFindings: none\n")
	}

	if len(report.DiscoveredEndpoints) > 0 {
		fmt.Fprintf(w, "\nDiscovered Endpoints (%d):\n", len(report.DiscoveredEndpoints))
		for _, ep := range report.DiscoveredEndpoints {
			fmt.Fprintf(w, "  - %-20s kind=%-12s locality=%-6s cost=%-12s\n",
				ep.ID, ep.Kind, ep.Locality, ep.CostClass)
		}
	}

	if len(report.PrincipalHosts) > 0 {
		fmt.Fprintf(w, "\nPrincipal Hosts (%d):\n", len(report.PrincipalHosts))
		for _, h := range report.PrincipalHosts {
			status := "absent"
			if h.Installed {
				status = "installed"
			}
			fmt.Fprintf(w, "  - %-15s %s\n", h.HostID, status)
		}
	}

	if report.ResourceInventory != nil {
		fmt.Fprintf(w, "\nResource Inventory:\n")
		fmt.Fprintf(w, "  OS Family:         %s\n", report.ResourceInventory.Hardware.OSFamily)
		fmt.Fprintf(w, "  Architecture:      %s\n", report.ResourceInventory.Hardware.Arch)
		fmt.Fprintf(w, "  Logical Cores:     %d\n", report.ResourceInventory.Hardware.LogicalCores)
		if report.ResourceInventory.Hardware.TotalMemoryBytes != nil {
			fmt.Fprintf(w, "  RAM:               %.1f GB\n", float64(*report.ResourceInventory.Hardware.TotalMemoryBytes)/(1024*1024*1024))
		}
		if len(report.ResourceInventory.Hardware.AcceleratorBackends) > 0 {
			backends := make([]string, len(report.ResourceInventory.Hardware.AcceleratorBackends))
			for i, b := range report.ResourceInventory.Hardware.AcceleratorBackends {
				backends[i] = string(b)
			}
			fmt.Fprintf(w, "  Accelerators:      %s\n", strings.Join(backends, ", "))
		}
	}
}

func renderDoctorFixSummary(w interface{ Write([]byte) (int, error) }, plan *protocol.SetupPlan, outputPath string) {
	fmt.Fprintf(w, "Setup Plan Generated\n")
	fmt.Fprintf(w, "Plan ID:            %s\n", plan.PlanID)
	fmt.Fprintf(w, "Plan Digest:        %s\n", plan.PlanDigest)
	fmt.Fprintf(w, "Target:             %s\n", plan.Target)
	fmt.Fprintf(w, "Required Authority: %s\n", plan.RequiredAuthority)
	if len(plan.TotalEffects) > 0 {
		effects := make([]string, len(plan.TotalEffects))
		for i, eff := range plan.TotalEffects {
			effects[i] = string(eff)
		}
		fmt.Fprintf(w, "Total Effects:      %s\n", strings.Join(effects, ", "))
	}
	fmt.Fprintf(w, "Planned Actions:    %d\n", len(plan.Actions))

	if len(plan.Actions) == 0 {
		fmt.Fprintln(w, "\nNo remediation actions required; system already satisfies readiness.")
		return
	}

	fmt.Fprintln(w, "\nActions:")
	for i, a := range plan.Actions {
		kind := "manual"
		if a.Operation != nil {
			kind = string(a.Operation.Kind)
		}
		fmt.Fprintf(w, "  %d. [%-15s] %-30s (%s)\n", i+1, a.Authority, a.RecipeID, kind)
		if a.Description != "" {
			fmt.Fprintf(w, "     %s\n", a.Description)
		}
		if a.ManualInstructions != nil {
			fmt.Fprintf(w, "     Manual steps: %s\n", a.ManualInstructions.Summary)
		}
	}

	planRef := "<plan-file>"
	if outputPath != "" {
		planRef = outputPath
	}
	fmt.Fprintf(w, "\nTo approve and apply this plan:\n")
	fmt.Fprintf(w, "  devcadence setup apply --plan %s --approve-plan %s", planRef, plan.PlanDigest)
	if plan.RequiredAuthority == protocol.AuthorityUserConfirmation {
		fmt.Fprintf(w, " [--yes]")
	}
	fmt.Fprintln(w)
}
