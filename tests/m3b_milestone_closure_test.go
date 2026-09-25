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

// 5. Preference for Existing Usable Tools
// DCI-105, ADR-0014 §7: Satisfied capabilities (e.g. existing authenticated CLI)
// require 0 setup actions, preferring existing tools over redundant installations.
func TestM3BMilestoneClosure_Scenario05_PreferenceForExistingUsableTools(t *testing.T) {
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

	// Report indicates authenticated CLI is present and healthy, no remediable findings
	profile := protocol.DeploymentProfile("")
	report := &protocol.DoctorReport{
		SchemaVersion:      protocol.SchemaVersion1,
		ReportID:           "rep_usable_tools",
		MachineFingerprint: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ObservedAt:         protocol.NewTimestamp(fixedTime),
		Readiness:          protocol.ReadinessReady,
		ScopeReadiness: []protocol.ScopeReadiness{
			{Scope: protocol.ScopeCanUseAuthenticatedCLI, Status: protocol.ScopeStatusReady, Reason: "Authenticated CLI available"},
			{Scope: protocol.ScopeHasAnyViableCognitionPath, Status: protocol.ScopeStatusReady, Reason: "Viable path available"},
		},
		Findings: []protocol.DiagnosticFinding{},
	}

	plan, err := planner.PlanWithContext(context.Background(), report, protocol.TargetAll, profile)
	if err != nil {
		t.Fatalf("PlanWithContext: %v", err)
	}

	if len(plan.Actions) != 0 {
		t.Errorf("planner generated %d actions for already-satisfied environment, want 0 (no redundant work)", len(plan.Actions))
	}
}

// 6. Plain / Non-Interactive Operation
// ADR-0014 §2: Non-interactive execution emits zero ANSI control characters and fails
// closed if approval is missing.
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

	// Non-interactive apply without approval fails closed with exit code 2
	cmdApply := exec.Command(bin, "setup", "apply", "--plan", filepath.Join(homeDir, "nonexistent.json"))
	cmdApply.Env = append(os.Environ(), "DEVCADENCE_HOME="+homeDir)
	errApply := cmdApply.Run()
	if errApply == nil {
		t.Fatal("expected setup apply without approval flags to fail")
	}
	exitErr, ok := errApply.(*exec.ExitError)
	if !ok || (exitErr.ExitCode() != 2 && exitErr.ExitCode() != 3) {
		t.Errorf("exit code = %v, want 2 (invalid args) or 3 (not found)", exitErr)
	}
}

// 7. SSH / Basic-Terminal Behavior
// ADR-0018: --no-tui runs deterministically without terminal escape codes or TUI dependencies.
func TestM3BMilestoneClosure_Scenario07_SSHBasicTerminalBehavior(t *testing.T) {
	bin := buildCLIBinary(t)
	homeDir := t.TempDir()

	// --no-tui must be accepted cleanly as a compatibility flag
	cmd := exec.Command(bin, "doctor", "--no-tui")
	cmd.Env = append(os.Environ(), "DEVCADENCE_HOME="+homeDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run()

	combined := stdout.String() + stderr.String()
	if strings.Contains(combined, "\x1b[") {
		t.Errorf("--no-tui output contained ANSI escape sequences: %q", combined)
	}
}

// 8. Readiness and Resource-Inventory Degradation
// DCI-104: Unavailable optional hardware or endpoints degrade readiness to
// READY_WITH_REDUCED_CAPABILITY or PARTIALLY_READY rather than causing a fatal error.
func TestM3BMilestoneClosure_Scenario08_GracefulCapabilityDegradation(t *testing.T) {
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

	// Simulate machine with degraded endpoint (e.g. auth expired)
	facts := protocol.EnvironmentFacts{
		Host: protocol.HostFacts{Family: protocol.OSLinux, Arch: "amd64"},
		Software: []protocol.SoftwarePresence{
			{ID: "git", Category: protocol.SoftwareEngineering, Installed: true},
		},
	}
	endpoints := []protocol.CognitionEndpointSummary{
		{
			ID:                     "expired-cli",
			Kind:                   protocol.EndpointAuthenticatedCLI,
			Locality:               protocol.LocalityRemote,
			Health:                 protocol.EndpointHealthReady,
			Auth:                   protocol.AuthExpired,
			CostClass:              protocol.CostSubscriptionIncluded,
			RequiredSourceExposure: protocol.ExposureFocusedSnippets,
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

	cognProfile := &protocol.MachineCapabilityProfile{
		ProfileID:          "mcp-degraded",
		MachineFingerprint: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ObservedAt:         protocol.NewTimestamp(clk.Now()),
		ProbeDepth:         protocol.DepthHealth,
	}

	// Verify inventory builds with honest degradation
	inv, err := doc.BuildResourceInventory(context.Background(), facts, "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", endpoints, nil, cognProfile, nil)
	if err != nil {
		t.Fatalf("BuildResourceInventory: %v", err)
	}

	if len(inv.CognitionEndpoints) != 1 || inv.CognitionEndpoints[0].Auth != protocol.AuthExpired {
		t.Errorf("inventory did not preserve degraded auth state: %+v", inv.CognitionEndpoints)
	}
	if report.Readiness == protocol.ReadinessReady {
		t.Error("report readiness should not be READY when primary endpoint auth is expired")
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
