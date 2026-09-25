package tests_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/credentials"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/schema"
	"github.com/olostan/DevCadence/internal/setup"
)

// buildCLIBinary builds the devcadence CLI binary once for integration tests.
func buildCLIBinary(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "devcadence")
	cmd := exec.Command("go", "build", "-o", binPath, "github.com/olostan/DevCadence/cmd/devcadence")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build devcadence binary: %v, output: %s", err, string(out))
	}
	return binPath
}

// 1. Blank Machine Verification
// DCI-104, DCI-105: A blank machine produces valid DoctorReport and ResourceInventory
// without panic or error, degrading gracefully to ACTION_REQUIRED or PARTIALLY_READY.
func TestM3BMilestoneClosure_Scenario01_BlankMachine(t *testing.T) {
	schemas, err := schema.Default()
	if err != nil {
		t.Fatalf("schema.Default: %v", err)
	}

	homeDir := t.TempDir()
	fixedTime := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	clk := clock.NewFake(fixedTime, 0)
	seq := ids.NewSequential()

	doc, err := setup.NewDoctor(setup.DoctorOptions{
		Clock:   clk,
		IDs:     seq,
		HomeDir: homeDir,
	})
	if err != nil {
		t.Fatalf("NewDoctor: %v", err)
	}

	blankFacts := protocol.EnvironmentFacts{
		Host: protocol.HostFacts{Family: protocol.OSLinux, Arch: "amd64"},
	}
	scope := protocol.ReadinessEvaluationScope{
		RequiredRoles:  []string{},
		EvidenceStatus: "live",
	}

	report, err := doc.Run(context.Background(), scope, blankFacts)
	if err != nil {
		t.Fatalf("Doctor.Run on blank machine: %v", err)
	}

	// Validate report against schema
	if err := schemas.ValidateRecord(report.RecordKind(), report); err != nil {
		t.Fatalf("DoctorReport fails schema validation: %v", err)
	}

	// Graceful degradation: readiness is ActionRequired or PartiallyReady, never panics
	if report.Readiness != protocol.ReadinessActionRequired && report.Readiness != protocol.ReadinessPartiallyReady {
		t.Errorf("readiness on blank machine = %s, want ACTION_REQUIRED or PARTIALLY_READY", report.Readiness)
	}

	// ResourceInventory on blank machine
	inv, err := doc.BuildResourceInventory(context.Background(), blankFacts, "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildResourceInventory: %v", err)
	}
	if err := schemas.ValidateRecord(inv.RecordKind(), inv); err != nil {
		t.Fatalf("ResourceInventory fails schema validation: %v", err)
	}

	// Only CPU backend should be present
	if len(inv.Hardware.AcceleratorBackends) != 1 || inv.Hardware.AcceleratorBackends[0] != protocol.BackendCPU {
		t.Errorf("accelerator backends on blank machine = %v, want [cpu]", inv.Hardware.AcceleratorBackends)
	}
	if len(inv.CognitionEndpoints) != 0 {
		t.Errorf("endpoints on blank machine = %d, want 0", len(inv.CognitionEndpoints))
	}
}

// 2. Dry-Run Mutation Visibility
// DCI-108: Setup plans explicitly enumerate expected filesystem mutations and effects
// prior to any execution attempt.
func TestM3BMilestoneClosure_Scenario02_DryRunMutationVisibility(t *testing.T) {
	schemas, err := schema.Default()
	if err != nil {
		t.Fatalf("schema.Default: %v", err)
	}

	fixedTime := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	clk := clock.NewFake(fixedTime, 0)
	seq := ids.NewSequential()

	planner, err := setup.NewPlanner(setup.PlannerOptions{
		Clock: clk,
		IDs:   seq,
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	// Create a report with remediable findings (missing state directories)
	profile := protocol.ProfileLocalHeavy
	report := &protocol.DoctorReport{
		SchemaVersion:      protocol.SchemaVersion1,
		ReportID:           "rep_test_01",
		MachineFingerprint: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ObservedAt:         protocol.NewTimestamp(fixedTime),
		Readiness:          protocol.ReadinessActionRequired,
		Findings: []protocol.DiagnosticFinding{
			{
				Category: "state",
				Severity: "warning",
				Code:     setup.FindingCodeStateDirsMissing,
				Title:    "Missing state directories",
				Detail:   "State directories are missing",
			},
		},
	}

	plan, err := planner.PlanWithContext(context.Background(), report, protocol.TargetHardware, profile)
	if err != nil {
		t.Fatalf("PlanWithContext: %v", err)
	}

	if err := schemas.ValidateRecord(plan.RecordKind(), plan); err != nil {
		t.Fatalf("SetupPlan fails schema validation: %v", err)
	}

	// Dry-run visibility: every action must declare expected mutations
	if len(plan.Actions) == 0 {
		t.Fatal("expected at least 1 planned action")
	}
	for _, act := range plan.Actions {
		if act.ExpectedMutations == nil {
			t.Errorf("action %s has nil ExpectedMutations", act.ActionID)
		}
		if len(act.ExpectedMutations) == 0 {
			t.Errorf("action %s has 0 ExpectedMutations; dry-run visibility requires explicit mutations", act.ActionID)
		}
	}
}

// 3. Privileged / High-Impact Action Approval Governance
// DCI-108: Privileged or high-impact actions fail closed without explicit matching authority.
func TestM3BMilestoneClosure_Scenario03_PrivilegedApprovalGovernance(t *testing.T) {
	homeDir := t.TempDir()
	fixedTime := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	clk := clock.NewFake(fixedTime, 0)
	seq := ids.NewSequential()

	executor, err := setup.NewExecutor(setup.ExecutorOptions{
		Home:   homeDir,
		Clock:  clk,
		IDs:    seq,
		Runner: &process.Runner{},
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	manual := protocol.SetupAction{
		ActionID:      "act-001",
		RecipeID:      "recipe.manual.install_driver",
		RecipeVersion: "1.0",
		Title:         "Install driver manually",
		Description:   "Requires manual operator intervention",
		Authority:     protocol.AuthorityHighImpactManual,
		Effects:       []protocol.EffectCategory{protocol.EffectPrivilegeElevation},
		ManualInstructions: &protocol.ManualGuide{
			Summary: "Install driver manually",
			Steps:   []string{"Run system installer"},
			VerificationCheck: []protocol.Condition{{
				Kind:             protocol.CondKindCommandAvailable,
				CommandAvailable: &protocol.CommandAvailableOperand{CommandName: "git"},
			}},
		},
		IdempotencyKey: "manual_install_driver",
	}

	plan := &protocol.SetupPlan{
		SchemaVersion:      protocol.SchemaVersion1,
		PlanID:             "plan_privileged",
		RecipeSetVersion:   "1.0",
		MachineFingerprint: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		CreatedAt:          protocol.NewTimestamp(fixedTime),
		Target:             protocol.TargetAll,
		Actions:            []protocol.SetupAction{manual},
		RequiredAuthority:  protocol.AuthorityHighImpactManual,
		TotalEffects:       []protocol.EffectCategory{protocol.EffectPrivilegeElevation},
	}
	digest, err := plan.ComputePlanDigest()
	if err != nil {
		t.Fatalf("ComputePlanDigest: %v", err)
	}
	plan.PlanDigest = digest

	// Applying with approvedPrivileged=false must fail closed against HighImpactManual
	_, err = executor.Apply(context.Background(), plan, plan.PlanDigest, false)
	if err == nil {
		t.Fatal("expected executor to fail closed on high-impact action when approvedPrivileged is false")
	}
}

// 4. Interrupted Setup Recovery via Real CLI
// ADR-0014 §6: Simulated crash / interrupted action in ledger is detected by real CLI,
// blocks subsequent execution with drift/conflict (exit 4), and is reconciled by `setup recover`.
func TestM3BMilestoneClosure_Scenario04_InterruptedSetupRecoveryCLI(t *testing.T) {
	bin := buildCLIBinary(t)
	homeDir := t.TempDir()
	planPath := filepath.Join(homeDir, "plan.json")

	fixedTime := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	action := setup.NewCreateDirectoryAction("act_mkdir", protocol.LocationTmp, "1.0")
	auth := action.Authority
	effects := action.Effects

	plan := &protocol.SetupPlan{
		SchemaVersion:      protocol.SchemaVersion1,
		PlanID:             "plan_recovery_e2e",
		RecipeSetVersion:   "1.0",
		MachineFingerprint: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		CreatedAt:          protocol.NewTimestamp(fixedTime),
		Target:             protocol.TargetHardware,
		Actions:            []protocol.SetupAction{action},
		RequiredAuthority:  auth,
		TotalEffects:       effects,
	}
	digest, err := plan.ComputePlanDigest()
	if err != nil {
		t.Fatalf("ComputePlanDigest: %v", err)
	}
	plan.PlanDigest = digest

	raw, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	if err := os.WriteFile(planPath, raw, 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	// Simulate crash: append action_starting to ledger without action_completed
	stateDir := filepath.Join(homeDir, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	ledgerPath := filepath.Join(stateDir, "setup-ledger.jsonl")
	ledger, err := setup.OpenLedger(ledgerPath)
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}

	const execID = "exec_simulated"
	now := time.Now()
	if _, err := ledger.Append(&protocol.SetupLedgerEvent{
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       "evt_sim_01",
		ExecutionID:   execID,
		PlanID:        plan.PlanID,
		PlanDigest:    plan.PlanDigest,
		Timestamp:     protocol.NewTimestamp(now),
		Type:          protocol.EventExecutionCreated,
		Payload: protocol.EventPayload{
			ExecutionCreated: &protocol.ExecutionCreatedPayload{InitiatedBy: "test", Target: plan.Target},
		},
	}); err != nil {
		t.Fatalf("append execution_created: %v", err)
	}
	if _, err := ledger.Append(&protocol.SetupLedgerEvent{
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       "evt_sim_02",
		ExecutionID:   execID,
		PlanID:        plan.PlanID,
		PlanDigest:    plan.PlanDigest,
		Timestamp:     protocol.NewTimestamp(now),
		Type:          protocol.EventPlanApproved,
		Payload: protocol.EventPayload{
			PlanApproved: &protocol.PlanApprovedPayload{ApprovedAuthority: plan.RequiredAuthority, ApprovedBy: "test"},
		},
	}); err != nil {
		t.Fatalf("append plan_approved: %v", err)
	}
	opKind := action.Operation.Kind
	if _, err := ledger.Append(&protocol.SetupLedgerEvent{
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       "evt_sim_03",
		ExecutionID:   execID,
		PlanID:        plan.PlanID,
		PlanDigest:    plan.PlanDigest,
		ActionID:      action.ActionID,
		Timestamp:     protocol.NewTimestamp(now),
		Type:          protocol.EventActionStarting,
		Payload: protocol.EventPayload{
			ActionStarting: &protocol.ActionStartingPayload{
				ActionID:       action.ActionID,
				RecipeID:       action.RecipeID,
				RecipeVersion:  action.RecipeVersion,
				OperationKind:  &opKind,
				IdempotencyKey: action.IdempotencyKey,
			},
		},
	}); err != nil {
		t.Fatalf("append action_starting: %v", err)
	}

	// 1. `setup apply` must detect interrupted action and exit with code 4 (drift/conflict)
	cmdApply := exec.Command(bin, "setup", "apply", "--plan", planPath, "--approve-plan", plan.PlanDigest, "--yes")
	cmdApply.Env = append(os.Environ(), "DEVCADENCE_HOME="+homeDir)
	var applyStderr bytes.Buffer
	cmdApply.Stderr = &applyStderr
	errApply := cmdApply.Run()
	if errApply == nil {
		t.Fatal("setup apply succeeded despite interrupted run in ledger")
	}
	exitErr, ok := errApply.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 4 {
		t.Fatalf("setup apply exit code = %v (err: %v), want 4 (conflict/drift); stderr: %s", exitErr, errApply, applyStderr.String())
	}

	// Ensure layout so managed tmp directory exists (fulfilling the postcondition)
	if err := setup.EnsureLayout(homeDir); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	// 2. `setup recover --json` reconciles the interrupted action
	cmdRecover := exec.Command(bin, "setup", "recover", "--plan", planPath, "--json")
	cmdRecover.Env = append(os.Environ(), "DEVCADENCE_HOME="+homeDir)
	var recoverStdout, recoverStderr bytes.Buffer
	cmdRecover.Stdout = &recoverStdout
	cmdRecover.Stderr = &recoverStderr
	if err := cmdRecover.Run(); err != nil {
		t.Fatalf("setup recover failed: %v; stderr: %s", err, recoverStderr.String())
	}

	schemas, err := schema.Default()
	if err != nil {
		t.Fatalf("schema.Default: %v", err)
	}
	if err := schemas.ValidateBytes(schema.NameSetupRecoveryReport, recoverStdout.Bytes()); err != nil {
		t.Fatalf("SetupRecoveryReport fails schema validation: %v", err)
	}

	var recoveryReport protocol.SetupRecoveryReport
	if err := json.Unmarshal(recoverStdout.Bytes(), &recoveryReport); err != nil {
		t.Fatalf("unmarshal recovery report: %v", err)
	}
	if len(recoveryReport.Results) != 1 || recoveryReport.Results[0].ActionID != "act_mkdir" || recoveryReport.Results[0].Status != protocol.ActionStatusSucceeded {
		t.Fatalf("recovery report results = %+v, want action act_mkdir succeeded", recoveryReport.Results)
	}
}

// fakeCognitionAdapter is a minimal, deterministic cognition.Adapter for
// milestone-closure scenarios that must drive Doctor.Run through a real
// cognition.Service rather than hand-constructing its output (independent-
// review follow-up on WP-M3B-8, FIX_NOW-1): a hand-built DoctorReport can
// silently drift from what discovery actually produces, and proves nothing
// about discovery itself.
type fakeCognitionAdapter struct {
	adapterID string
	endpoints []protocol.CognitionEndpoint
}

func (a *fakeCognitionAdapter) ID() string { return a.adapterID }
func (a *fakeCognitionAdapter) Discover(ctx context.Context, in cognition.DiscoveryInput) ([]protocol.CognitionEndpoint, error) {
	return a.endpoints, nil
}
func (a *fakeCognitionAdapter) Probe(ctx context.Context, endpoint protocol.CognitionEndpoint, req cognition.ProbeRequest) (cognition.ProbeResult, error) {
	return cognition.ProbeResult{}, nil
}

// findScopeStatus looks up one ScopeReadiness entry by scope kind, failing
// the test if the scope is absent rather than returning a zero value that
// could be mistaken for a real "not ready" status.
func findScopeStatus(t *testing.T, readiness []protocol.ScopeReadiness, scope protocol.ScopeKind) protocol.ScopeReadinessStatus {
	t.Helper()
	for _, r := range readiness {
		if r.Scope == scope {
			return r.Status
		}
	}
	t.Fatalf("scope %q not found in ScopeReadiness", scope)
	return ""
}

// 5. Preference for Existing Usable Tools
// DCI-105, ADR-0014 §7: Satisfied capabilities (e.g. existing authenticated CLI)
// require 0 setup actions, preferring existing tools over redundant installations.
//
// Independent-review follow-up on WP-M3B-8, FIX_NOW-1: the original version
// of this scenario hand-constructed a DoctorReport (with an empty, schema-
// invalid EvaluationScope.EvidenceStatus) claiming the CLI was already
// authenticated, without ever exercising discovery. That proved nothing
// about whether discovery actually recognizes and prefers an existing
// authenticated CLI. This version drives a real cognition.Service with a
// deterministic fake adapter through Doctor.Run, and only then plans from
// the report Run actually produced.
func TestM3BMilestoneClosure_Scenario05_PreferenceForExistingUsableTools(t *testing.T) {
	homeDir := t.TempDir()
	for _, sub := range []string{"state", filepath.Join("artifacts", "setup"), "tmp"} {
		if err := os.MkdirAll(filepath.Join(homeDir, sub), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}

	fixedTime := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	clk := clock.NewFake(fixedTime, 0)
	seq := ids.NewSequential()

	adapter := &fakeCognitionAdapter{
		adapterID: "fake-authenticated-cli",
		endpoints: []protocol.CognitionEndpoint{
			{
				ID:                     "claude-code",
				Kind:                   protocol.EndpointAuthenticatedCLI,
				Locality:               protocol.LocalityRemote,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthAuthenticated,
				CostClass:              protocol.CostSubscriptionIncluded,
				RequiredSourceExposure: protocol.ExposureFocusedSnippets,
				StructuredOutput:       protocol.FeatureDeclared,
				ToolUse:                protocol.FeatureDeclared,
				ObservedAt:             protocol.NewTimestamp(fixedTime),
			},
		},
	}
	cogService, err := cognition.NewService(cognition.Options{
		Adapters: []cognition.Adapter{adapter},
		Clock:    clk,
		IDs:      seq,
	})
	if err != nil {
		t.Fatalf("cognition.NewService: %v", err)
	}

	doc, err := setup.NewDoctor(setup.DoctorOptions{
		Clock:            clk,
		IDs:              seq,
		HomeDir:          homeDir,
		CognitionService: cogService,
	})
	if err != nil {
		t.Fatalf("NewDoctor: %v", err)
	}

	facts := protocol.EnvironmentFacts{
		Host:           protocol.HostFacts{Family: protocol.OSLinux, Arch: "amd64"},
		Virtualization: protocol.VirtualizationFacts{Container: protocol.ContainerNone},
		Software: []protocol.SoftwarePresence{
			{ID: "git", Category: protocol.SoftwareEngineering, Installed: true, Version: "2.45.0"},
		},
	}
	scope := protocol.ReadinessEvaluationScope{
		RequiredRoles:  []string{},
		EvidenceStatus: "live",
	}

	report, err := doc.Run(context.Background(), scope, facts)
	if err != nil {
		t.Fatalf("Doctor.Run: %v", err)
	}
	if err := report.Validate(); err != nil {
		t.Fatalf("DoctorReport.Validate: %v", err)
	}

	// Discovery must have actually surfaced the authenticated CLI, not just
	// have been told about it after the fact.
	discovered := false
	for _, ep := range report.DiscoveredEndpoints {
		if ep.ID == "claude-code" && ep.Auth == protocol.AuthAuthenticated {
			discovered = true
		}
	}
	if !discovered {
		t.Fatalf("doctor did not discover the authenticated CLI endpoint; DiscoveredEndpoints = %+v", report.DiscoveredEndpoints)
	}
	if status := findScopeStatus(t, report.ScopeReadiness, protocol.ScopeCanUseAuthenticatedCLI); status != protocol.ScopeStatusReady {
		t.Errorf("ScopeCanUseAuthenticatedCLI = %s, want ready", status)
	}
	if report.Readiness != protocol.ReadinessReady && report.Readiness != protocol.ReadinessReadyWithReducedCap {
		t.Fatalf("Readiness = %s, want READY or READY_WITH_REDUCED_CAPABILITY (authenticated CLI is a viable path)", report.Readiness)
	}

	planner, err := setup.NewPlanner(setup.PlannerOptions{Clock: clk, IDs: seq})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	plan, err := planner.PlanWithContext(context.Background(), report, protocol.TargetAll, "")
	if err != nil {
		t.Fatalf("PlanWithContext: %v", err)
	}
	if len(plan.Actions) != 0 {
		t.Errorf("planner generated %d actions for an already-satisfied environment (real discovered authenticated CLI), want 0 (no redundant work): %+v", len(plan.Actions), plan.Actions)
	}
}

// 6. Plain / Non-Interactive Operation
// ADR-0014 §2: Non-interactive execution emits zero ANSI control characters and fails
// closed if approval is missing.
//
// Independent-review follow-up on WP-M3B-8, FIX_NOW-1: the original version
// pointed setup apply at a nonexistent plan file, so the CLI stopped at
// "plan not found" (exit 3) and never reached the missing-approval fail-
// closed boundary this scenario claims to prove (documented result: exit
// 2). This version gives setup apply a real, valid, on-disk plan and omits
// --approve-plan, so the exit code and diagnostic it asserts are actually
// produced by the approval boundary, not by a file-not-found short circuit.
func TestM3BMilestoneClosure_Scenario06_PlainNonInteractiveOperation(t *testing.T) {
	bin := buildCLIBinary(t)
	homeDir := t.TempDir()

	// Run doctor on real CLI with stdout/stderr capture
	cmd := exec.Command(bin, "doctor")
	cmd.Env = append(os.Environ(), "DEVCADENCE_HOME="+homeDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run() // exit 0 or 1 depending on host, but output must have no ANSI

	combined := stdout.String() + stderr.String()
	if strings.Contains(combined, "\x1b[") {
		t.Errorf("plain CLI output contained ANSI escape sequences: %q", combined)
	}

	// A real, valid, on-disk plan — so omitting --approve-plan exercises the
	// missing-approval fail-closed boundary itself, not file-not-found.
	planPath := filepath.Join(homeDir, "plan.json")
	action := setup.NewCreateDirectoryAction("act_mkdir", protocol.LocationTmp, "1.0")
	plan := &protocol.SetupPlan{
		SchemaVersion:      protocol.SchemaVersion1,
		PlanID:             "plan_scenario06",
		RecipeSetVersion:   "1.0",
		MachineFingerprint: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		CreatedAt:          protocol.NewTimestamp(time.Now()),
		Target:             protocol.TargetHardware,
		Actions:            []protocol.SetupAction{action},
		RequiredAuthority:  action.Authority,
		TotalEffects:       action.Effects,
	}
	digest, err := plan.ComputePlanDigest()
	if err != nil {
		t.Fatalf("ComputePlanDigest: %v", err)
	}
	plan.PlanDigest = digest
	raw, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	if err := os.WriteFile(planPath, raw, 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	// Non-interactive apply of a real, existing, valid plan without
	// --approve-plan must fail closed with exactly exit code 2.
	//
	// Stdin is explicitly a pipe (via strings.NewReader), not left to
	// exec.Cmd's own default: an unset Cmd.Stdin makes the child read from
	// os.DevNull, and /dev/null itself reports as a character device on
	// Unix (confirmed: os.Open(os.DevNull) has os.ModeCharDevice set) —
	// which would trip the CLI's isTerminal() heuristic (run.go, checking
	// exactly that bit) into treating this as an interactive session, and
	// the test would exercise the interactive-decline path (exit 1) rather
	// than the non-interactive fail-closed boundary this scenario claims
	// to prove (exit 2). A pipe has no such bit set, so it exercises the
	// real non-interactive boundary this scenario names.
	cmdApply := exec.Command(bin, "setup", "apply", "--plan", planPath)
	cmdApply.Env = append(os.Environ(), "DEVCADENCE_HOME="+homeDir)
	cmdApply.Stdin = strings.NewReader("")
	var applyStdout, applyStderr bytes.Buffer
	cmdApply.Stdout = &applyStdout
	cmdApply.Stderr = &applyStderr
	errApply := cmdApply.Run()
	if errApply == nil {
		t.Fatal("expected setup apply on a valid plan without --approve-plan to fail")
	}
	exitErr, ok := errApply.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 2 {
		t.Fatalf("setup apply exit code = %v (err: %v), want exactly 2 (invalid argument); stdout: %s stderr: %s",
			exitErr, errApply, applyStdout.String(), applyStderr.String())
	}
	combinedApply := applyStdout.String() + applyStderr.String()
	if !strings.Contains(combinedApply, "--approve-plan") {
		t.Errorf("expected the approval diagnostic to mention --approve-plan, got: %s", combinedApply)
	}
	if strings.Contains(combinedApply, "\x1b[") {
		t.Errorf("setup apply output contained ANSI escape sequences: %q", combinedApply)
	}
}

// 7. SSH / Basic-Terminal Behavior
// ADR-0018: --no-tui runs deterministically without terminal escape codes.
//
// Independent-review follow-up on WP-M3B-8, FIX_NOW-1: the original version
// discarded the command's error/exit status entirely, so an unknown-flag or
// argument-parsing failure (which also emits no ANSI) would have passed
// while the scenario claimed the flag "is accepted cleanly." This version
// asserts the process actually reached a real doctor readiness outcome (exit
// 0 or 1), never an argument/parser failure (exit 2). Basic-TTY interactive
// approval itself (the other half of ADR-0018's SSH/basic-terminal concern)
// is proven separately and exactly by the existing interactive CLI tests —
// TestCLISetupApplyInteractiveApproval and TestCLISetupApplyInteractiveRejection
// in cmd/devcadence/cli_setup_test.go — so this scenario's scope is narrowed
// to what its name and code actually exercise: the --no-tui compatibility
// flag contract.
func TestM3BMilestoneClosure_Scenario07_SSHBasicTerminalBehavior(t *testing.T) {
	bin := buildCLIBinary(t)
	homeDir := t.TempDir()

	// --no-tui must be accepted cleanly as a compatibility flag: the
	// process must reach a real doctor readiness outcome, never an
	// argument/parser failure.
	cmd := exec.Command(bin, "doctor", "--no-tui")
	cmd.Env = append(os.Environ(), "DEVCADENCE_HOME="+homeDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	combined := stdout.String() + stderr.String()
	if strings.Contains(combined, "\x1b[") {
		t.Errorf("--no-tui output contained ANSI escape sequences: %q", combined)
	}

	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("doctor --no-tui failed to run at all: %v", err)
		}
		code := exitErr.ExitCode()
		if code != 0 && code != 1 {
			t.Fatalf("doctor --no-tui exit code = %d, want 0 or 1 (a real readiness outcome, not an argument/parser failure); stdout: %s stderr: %s",
				code, stdout.String(), stderr.String())
		}
	}
}

// 8. Readiness and Resource-Inventory Degradation
// DCI-104: Unavailable optional hardware or endpoints degrade readiness to
// READY_WITH_REDUCED_CAPABILITY or PARTIALLY_READY rather than causing a fatal error.
//
// Independent-review follow-up on WP-M3B-8, FIX_NOW-1: the original version
// constructed Doctor with no cognition service at all, so the expired
// endpoint was never supplied to Doctor.Run — it only ever reached a
// separate BuildResourceInventory call, meaning the report's non-ready
// state was unrelated to the expired auth it claimed to test. This version
// drives Doctor.Run itself through a real cognition.Service reporting the
// expired-auth endpoint, then builds ResourceInventory from that same
// report's own DiscoveredEndpoints (rather than a hand-built, separately
// constructed list) and validates both records.
func TestM3BMilestoneClosure_Scenario08_GracefulCapabilityDegradation(t *testing.T) {
	schemas, err := schema.Default()
	if err != nil {
		t.Fatalf("schema.Default: %v", err)
	}

	homeDir := t.TempDir()
	fixedTime := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	clk := clock.NewFake(fixedTime, 0)
	seq := ids.NewSequential()

	adapter := &fakeCognitionAdapter{
		adapterID: "fake-expired-cli",
		endpoints: []protocol.CognitionEndpoint{
			{
				ID:                     "expired-cli",
				Kind:                   protocol.EndpointAuthenticatedCLI,
				Locality:               protocol.LocalityRemote,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthExpired,
				CostClass:              protocol.CostSubscriptionIncluded,
				RequiredSourceExposure: protocol.ExposureFocusedSnippets,
				StructuredOutput:       protocol.FeatureDeclared,
				ToolUse:                protocol.FeatureDeclared,
				ObservedAt:             protocol.NewTimestamp(fixedTime),
			},
		},
	}
	cogService, err := cognition.NewService(cognition.Options{
		Adapters: []cognition.Adapter{adapter},
		Clock:    clk,
		IDs:      seq,
	})
	if err != nil {
		t.Fatalf("cognition.NewService: %v", err)
	}

	doc, err := setup.NewDoctor(setup.DoctorOptions{
		Clock:            clk,
		IDs:              seq,
		HomeDir:          homeDir,
		CognitionService: cogService,
	})
	if err != nil {
		t.Fatalf("NewDoctor: %v", err)
	}

	// Simulate machine with degraded endpoint (auth expired)
	facts := protocol.EnvironmentFacts{
		Host:           protocol.HostFacts{Family: protocol.OSLinux, Arch: "amd64"},
		Virtualization: protocol.VirtualizationFacts{Container: protocol.ContainerNone},
		Software: []protocol.SoftwarePresence{
			{ID: "git", Category: protocol.SoftwareEngineering, Installed: true},
		},
	}
	scope := protocol.ReadinessEvaluationScope{
		RequiredRoles:  []string{},
		EvidenceStatus: "live",
	}

	report, err := doc.Run(context.Background(), scope, facts)
	if err != nil {
		t.Fatalf("Doctor.Run with degraded endpoint: %v", err)
	}
	if err := schemas.ValidateRecord(report.RecordKind(), report); err != nil {
		t.Fatalf("DoctorReport fails schema validation: %v", err)
	}

	// The expired endpoint must actually have reached Doctor.Run's own
	// output, not merely a separately-constructed inventory input.
	expiredInReport := false
	for _, ep := range report.DiscoveredEndpoints {
		if ep.ID == "expired-cli" && ep.Auth == protocol.AuthExpired {
			expiredInReport = true
		}
	}
	if !expiredInReport {
		t.Fatalf("doctor report did not discover the expired-auth endpoint; DiscoveredEndpoints = %+v", report.DiscoveredEndpoints)
	}
	if report.Readiness == protocol.ReadinessReady {
		t.Error("report readiness should not be READY when the only endpoint's auth is expired")
	}

	// ResourceInventory is built from the report's own DiscoveredEndpoints —
	// the same evidence Doctor.Run itself produced, not a hand-built list.
	// A profile reference is required whenever cognition endpoints are
	// present (it preserves runtime/model/capability provenance); this is
	// the same minimal-but-valid shape internal/setup's own matrix tests
	// use for this purpose (ProfileID/MachineFingerprint/ObservedAt/
	// ProbeDepth), not a placeholder invented for this test alone.
	cognProfile := &protocol.MachineCapabilityProfile{
		ProfileID:          "mcp-degraded",
		MachineFingerprint: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ObservedAt:         protocol.NewTimestamp(clk.Now()),
		ProbeDepth:         protocol.DepthHealth,
	}
	inv, err := doc.BuildResourceInventory(context.Background(), facts,
		"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		report.DiscoveredEndpoints, nil, cognProfile, report.ScopeReadiness)
	if err != nil {
		t.Fatalf("BuildResourceInventory: %v", err)
	}
	if err := schemas.ValidateRecord(inv.RecordKind(), inv); err != nil {
		t.Fatalf("ResourceInventory fails schema validation: %v", err)
	}
	if len(inv.CognitionEndpoints) != 1 || inv.CognitionEndpoints[0].Auth != protocol.AuthExpired {
		t.Errorf("inventory did not preserve degraded auth state: %+v", inv.CognitionEndpoints)
	}
}

// CredentialRef & AuthEvidence index completeness
func TestM3BMilestoneClosure_CredentialEvidenceIntegrity(t *testing.T) {
	ref := protocol.CredentialRef{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred_anthropic_env",
		Kind:          protocol.CredRefEnvVar,
		Locator:       "ANTHROPIC_API_KEY",
	}
	if err := ref.Validate(); err != nil {
		t.Fatalf("CredentialRef.Validate: %v", err)
	}

	evidence := protocol.AuthEvidence{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred_anthropic_env",
		Kind:          protocol.CredRefEnvVar,
		Status:        protocol.AuthStatusIndeterminate,
		ProbeKind:     protocol.AuthProbeEnvPresence,
		ObservedAt:    protocol.NewTimestamp(time.Now()),
		ProbeTarget:   "ANTHROPIC_API_KEY",
	}
	if err := evidence.Validate(); err != nil {
		t.Fatalf("AuthEvidence.Validate: %v", err)
	}

	mgr, err := credentials.NewManager(credentials.Options{})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	checkedEvidence, err := mgr.CheckCredential(context.Background(), ref)
	if err != nil {
		t.Fatalf("CheckCredential: %v", err)
	}
	if checkedEvidence.RefID != ref.RefID {
		t.Errorf("checkedEvidence.RefID = %q, want %q", checkedEvidence.RefID, ref.RefID)
	}
}
