package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/schema"
	"github.com/olostan/DevCadence/internal/setup"
)

const testMachineFingerprint = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func buildActionPlan(t *testing.T, target protocol.SetupTarget, actions ...protocol.SetupAction) *protocol.SetupPlan {
	t.Helper()
	maxAuth := protocol.AuthorityReadOnly
	effectSet := map[protocol.EffectCategory]bool{}
	for _, a := range actions {
		if a.Authority.Rank() > maxAuth.Rank() {
			maxAuth = a.Authority
		}
		for _, e := range a.Effects {
			effectSet[e] = true
		}
	}
	var totalEffects []protocol.EffectCategory
	for e := range effectSet {
		totalEffects = append(totalEffects, e)
	}

	plan := &protocol.SetupPlan{
		SchemaVersion:      protocol.SchemaVersion1,
		PlanID:             "plan_test_0000000000000001",
		RecipeSetVersion:   "1.0.0",
		MachineFingerprint: testMachineFingerprint,
		CreatedAt:          protocol.NewTimestamp(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)),
		Target:             target,
		Actions:            actions,
		RequiredAuthority:  maxAuth,
		TotalEffects:       totalEffects,
	}
	digest, err := protocol.ComputePlanDigest(plan)
	if err != nil {
		t.Fatalf("ComputePlanDigest: %v", err)
	}
	plan.PlanDigest = digest
	if err := plan.Validate(); err != nil {
		t.Fatalf("plan.Validate: %v", err)
	}
	return plan
}

func TestCLISetupPlanGeneratesValidPlan(t *testing.T) {
	c := newCLI(t)
	planFile := filepath.Join(t.TempDir(), "planned.json")
	out, stderr, err := c.run("setup", "plan", "--output", planFile)

	if strings.Contains(out, "\x1b") || strings.Contains(stderr, "\x1b") {
		t.Errorf("setup plan output contains ANSI control sequences")
	}

	planBytes, readErr := os.ReadFile(planFile)
	if readErr != nil {
		t.Fatalf("expected plan file %s to be created: %v", planFile, readErr)
	}

	set, sErr := schema.Default()
	if sErr != nil {
		t.Fatalf("schema.Default: %v", sErr)
	}
	if vErr := set.ValidateBytes(schema.NameSetupPlan, planBytes); vErr != nil {
		t.Fatalf("emitted SetupPlan does not satisfy schema: %v\nJSON:\n%s", vErr, string(planBytes))
	}

	var plan protocol.SetupPlan
	if decErr := protocol.Unmarshal(planBytes, &plan); decErr != nil {
		t.Fatalf("failed to decode SetupPlan: %v", decErr)
	}
	if valErr := plan.Validate(); valErr != nil {
		t.Fatalf("plan.Validate failed: %v", valErr)
	}

	code := exitCode(err)
	if len(plan.Actions) > 0 {
		if code != ExitCodePlanGenerated {
			t.Errorf("expected exit code %d (plan generated), got %d (err: %v)", ExitCodePlanGenerated, code, err)
		}
		if !strings.Contains(out, "To approve and apply this plan:") {
			t.Errorf("expected apply instructions in output:\n%s", out)
		}
	} else {
		if code != ExitCodeSuccess {
			t.Errorf("expected exit code 0 (no-op), got %d (err: %v)", code, err)
		}
	}
}

func TestCLISetupPlanJSONValidatesAgainstSchema(t *testing.T) {
	c := newCLI(t)
	out, stderr, err := c.run("setup", "plan", "--json")
	if out == "" {
		t.Fatalf("expected JSON output from setup plan --json, got empty (stderr: %s, err: %v)", stderr, err)
	}

	set, sErr := schema.Default()
	if sErr != nil {
		t.Fatalf("schema.Default: %v", sErr)
	}
	if vErr := set.ValidateBytes(schema.NameSetupPlan, []byte(out)); vErr != nil {
		t.Fatalf("emitted SetupPlan does not satisfy schema: %v\nJSON:\n%s", vErr, out)
	}

	var plan protocol.SetupPlan
	if decErr := protocol.Unmarshal([]byte(out), &plan); decErr != nil {
		t.Fatalf("failed to decode SetupPlan JSON: %v", decErr)
	}

	code := exitCode(err)
	if len(plan.Actions) > 0 {
		if code != ExitCodePlanGenerated {
			t.Errorf("expected exit code %d, got %d", ExitCodePlanGenerated, code)
		}
	} else {
		if code != ExitCodeSuccess {
			t.Errorf("expected exit code %d, got %d", ExitCodeSuccess, code)
		}
	}
}

func TestCLISetupPlanTargetScoping(t *testing.T) {
	c := newCLI(t)
	out, stderr, err := c.run("setup", "plan", "hardware", "--json")
	if out == "" {
		t.Fatalf("expected JSON output from setup plan hardware --json (stderr: %s, err: %v)", stderr, err)
	}

	var plan protocol.SetupPlan
	if decErr := protocol.Unmarshal([]byte(out), &plan); decErr != nil {
		t.Fatalf("failed to decode SetupPlan JSON: %v", decErr)
	}
	if plan.Target != protocol.TargetHardware {
		t.Errorf("expected target %q, got %q", protocol.TargetHardware, plan.Target)
	}
	_ = err
}

func TestCLISetupApplyNonInteractiveRequiresApprovePlan(t *testing.T) {
	c := newCLI(t)
	// Non-terminal mode
	c.isTerminal = func() bool { return false }

	planFile := filepath.Join(t.TempDir(), "dummy-plan.json")
	dummyAction := setup.NewCreateDirectoryAction("act_01", protocol.LocationState, "1.0.0")
	fullPlan := buildActionPlan(t, protocol.TargetHardware, dummyAction)

	planBytes, _ := json.MarshalIndent(fullPlan, "", "  ")
	_ = os.WriteFile(planFile, planBytes, 0o600)

	_, stderr, err := c.run("setup", "apply", "--plan", planFile)
	if err == nil {
		t.Fatal("expected non-interactive setup apply without --approve-plan to fail")
	}
	if code := exitCode(err); code != ExitCodeInvalidArgument {
		t.Errorf("expected exit code %d (invalid argument), got %d (err: %v)", ExitCodeInvalidArgument, code, err)
	}
	if !strings.Contains(err.Error(), "--approve-plan") && !strings.Contains(stderr, "--approve-plan") {
		t.Errorf("expected error to demand --approve-plan, got: %v", err)
	}
}

func TestCLISetupApplyDigestMismatchFailsClosed(t *testing.T) {
	c := newCLI(t)
	planFile := filepath.Join(t.TempDir(), "plan.json")
	action := setup.NewCreateDirectoryAction("act_01", protocol.LocationState, "1.0.0")
	fullPlan := buildActionPlan(t, protocol.TargetHardware, action)

	planBytes, _ := json.MarshalIndent(fullPlan, "", "  ")
	_ = os.WriteFile(planFile, planBytes, 0o600)

	_, _, err := c.run("setup", "apply", "--plan", planFile, "--approve-plan", "sha256:0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected digest mismatch to fail")
	}
	if code := exitCode(err); code != ExitCodeInvalidArgument {
		t.Errorf("expected exit code %d (invalid argument), got %d (err: %v)", ExitCodeInvalidArgument, code, err)
	}
	if !strings.Contains(err.Error(), "does not match plan digest") {
		t.Errorf("expected error message to mention digest mismatch, got: %v", err)
	}
}

func TestCLISetupApplyInteractiveApproval(t *testing.T) {
	c := newCLI(t)
	c.isTerminal = func() bool { return true }
	c.stdin = strings.NewReader("y\n")

	planFile := filepath.Join(t.TempDir(), "plan.json")
	action := setup.NewCreateDirectoryAction("act_01", protocol.LocationState, "1.0.0")
	fullPlan := buildActionPlan(t, protocol.TargetHardware, action)

	planBytes, _ := json.MarshalIndent(fullPlan, "", "  ")
	_ = os.WriteFile(planFile, planBytes, 0o600)

	out, stderr, err := c.run("setup", "apply", "--plan", planFile)
	if err != nil {
		t.Fatalf("expected interactive approval with 'y' to succeed, got error: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(out, "Approve plan") {
		t.Errorf("expected confirmation prompt in output:\n%s", out)
	}
	if !strings.Contains(out, "Setup completed successfully.") {
		t.Errorf("expected success message in output:\n%s", out)
	}
}

func TestCLISetupApplyInteractiveRejection(t *testing.T) {
	c := newCLI(t)
	c.isTerminal = func() bool { return true }
	c.stdin = strings.NewReader("n\n")

	planFile := filepath.Join(t.TempDir(), "plan.json")
	action := setup.NewCreateDirectoryAction("act_01", protocol.LocationState, "1.0.0")
	fullPlan := buildActionPlan(t, protocol.TargetHardware, action)

	planBytes, _ := json.MarshalIndent(fullPlan, "", "  ")
	_ = os.WriteFile(planFile, planBytes, 0o600)

	_, _, err := c.run("setup", "apply", "--plan", planFile)
	if err == nil {
		t.Fatal("expected interactive rejection with 'n' to fail")
	}
	if code := exitCode(err); code != ExitCodeExecutionError {
		t.Errorf("expected exit code %d (policy denied/execution error), got %d (err: %v)", ExitCodeExecutionError, code, err)
	}
}

func TestCLISetupApplyMatchingDigestAndJSONOutput(t *testing.T) {
	c := newCLI(t)
	planFile := filepath.Join(t.TempDir(), "plan.json")
	action := setup.NewCreateDirectoryAction("act_01", protocol.LocationState, "1.0.0")
	fullPlan := buildActionPlan(t, protocol.TargetHardware, action)

	planBytes, _ := json.MarshalIndent(fullPlan, "", "  ")
	_ = os.WriteFile(planFile, planBytes, 0o600)

	out, stderr, err := c.run("setup", "apply", "--plan", planFile, "--approve-plan", fullPlan.PlanDigest, "--json")
	if err != nil {
		t.Fatalf("setup apply with approved digest failed: %v (stderr: %s)", err, stderr)
	}

	set, sErr := schema.Default()
	if sErr != nil {
		t.Fatalf("schema.Default: %v", sErr)
	}
	if vErr := set.ValidateBytes(schema.NameSetupExecutionReport, []byte(out)); vErr != nil {
		t.Fatalf("emitted execution report does not satisfy schema: %v\nJSON:\n%s", vErr, out)
	}

	var report protocol.SetupExecutionReport
	if decErr := protocol.Unmarshal([]byte(out), &report); decErr != nil {
		t.Fatalf("failed to decode SetupExecutionReport: %v", decErr)
	}
	if report.Status != protocol.ExecutionStatusSucceeded {
		t.Errorf("expected execution status %s, got %s", protocol.ExecutionStatusSucceeded, report.Status)
	}
}

func TestCLISetupApplyPreconditionDriftReturnsExitCode4(t *testing.T) {
	c := newCLI(t)
	planFile := filepath.Join(t.TempDir(), "drift-plan.json")

	action := setup.NewCreateDirectoryAction("act_drift", protocol.LocationState, "1.0.0")
	action.Preconditions = []protocol.Condition{
		{
			Kind:             protocol.CondKindCommandAvailable,
			CommandAvailable: &protocol.CommandAvailableOperand{CommandName: "non_existent_command_xyz_123"},
		},
	}

	fullPlan := buildActionPlan(t, protocol.TargetHardware, action)
	planBytes, _ := json.MarshalIndent(fullPlan, "", "  ")
	_ = os.WriteFile(planFile, planBytes, 0o600)

	_, _, err := c.run("setup", "apply", "--plan", planFile, "--approve-plan", fullPlan.PlanDigest)
	if err == nil {
		t.Fatal("expected precondition drift to fail")
	}

	// Exit code contract: 4 for drift/conflict
	code := exitCode(err)
	if code != ExitCodeDrift {
		t.Errorf("expected exit code %d (ExitCodeDrift), got %d (err: %v)", ExitCodeDrift, code, err)
	}
}

func TestCLISetupApplyYesScopeRejectsPrivilegedPlan(t *testing.T) {
	c := newCLI(t)
	planFile := filepath.Join(t.TempDir(), "privileged-plan.json")

	action := setup.NewCreateDirectoryAction("act_priv", protocol.LocationState, "1.0.0")
	action.Authority = protocol.AuthorityPrivilegedConfirmation

	fullPlan := buildActionPlan(t, protocol.TargetHardware, action)
	planBytes, _ := json.MarshalIndent(fullPlan, "", "  ")
	_ = os.WriteFile(planFile, planBytes, 0o600)

	_, _, err := c.run("setup", "apply", "--plan", planFile, "--approve-plan", fullPlan.PlanDigest, "--yes")
	if err == nil {
		t.Fatal("expected --yes on privileged plan to fail")
	}
	if code := exitCode(err); code != ExitCodeExecutionError {
		t.Errorf("expected exit code %d (policy denied/execution error), got %d (err: %v)", ExitCodeExecutionError, code, err)
	}
}

func TestCLISetupRecoverReconcilesInterruptedActions(t *testing.T) {
	c := newCLI(t)
	planFile := filepath.Join(t.TempDir(), "recover-plan.json")

	action := setup.NewCreateDirectoryAction("act_01", protocol.LocationState, "1.0.0")
	fullPlan := buildActionPlan(t, protocol.TargetHardware, action)

	planBytes, _ := json.MarshalIndent(fullPlan, "", "  ")
	_ = os.WriteFile(planFile, planBytes, 0o600)

	// Simulate an interrupted run in the ledger: append ActionStarting without ActionTerminated
	ledgerPath := filepath.Join(c.db, "..", "state", "setup-ledger.jsonl")
	_ = os.MkdirAll(filepath.Dir(ledgerPath), 0o700)
	ledger, lErr := setup.OpenLedger(ledgerPath)
	if lErr != nil {
		t.Fatalf("OpenLedger: %v", lErr)
	}

	execID := "exec_simulated"
	_, _ = ledger.Append(&protocol.SetupLedgerEvent{
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       "evt_01",
		ExecutionID:   execID,
		PlanID:        fullPlan.PlanID,
		PlanDigest:    fullPlan.PlanDigest,
		Timestamp:     protocol.NewTimestamp(time.Now()),
		Type:          protocol.EventExecutionCreated,
		Payload: protocol.EventPayload{
			ExecutionCreated: &protocol.ExecutionCreatedPayload{Target: protocol.TargetHardware},
		},
	})
	opKind := action.Operation.Kind
	_, _ = ledger.Append(&protocol.SetupLedgerEvent{
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       "evt_02",
		ExecutionID:   execID,
		PlanID:        fullPlan.PlanID,
		PlanDigest:    fullPlan.PlanDigest,
		ActionID:      action.ActionID,
		Timestamp:     protocol.NewTimestamp(time.Now()),
		Type:          protocol.EventActionStarting,
		Payload: protocol.EventPayload{
			ActionStarting: &protocol.ActionStartingPayload{
				ActionID:      action.ActionID,
				RecipeID:      action.RecipeID,
				RecipeVersion: action.RecipeVersion,
				OperationKind: &opKind,
			},
		},
	})

	out, stderr, err := c.run("setup", "recover", "--plan", planFile)
	if err != nil {
		t.Fatalf("setup recover failed: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(out, "Setup Recovery Reconciled") {
		t.Errorf("expected recovery message in output:\n%s", out)
	}
}

func TestCLISetupHelpDocumentsWorkflowAndExitCodes(t *testing.T) {
	c := newCLI(t)
	out, _, err := c.run("setup", "--help")
	if err != nil && exitCode(err) != ExitCodeSuccess {
		t.Errorf("unexpected error from setup --help: %v", err)
	}

	for _, expected := range []string{
		"usage: devcadence setup",
		"Two-Step Approval Workflow:",
		"1. Generate an immutable plan:",
		"devcadence setup plan [target] --output <file>",
		"2. Review and apply the approved plan:",
		"devcadence setup apply --plan <file> --approve-plan <sha256:digest> [--yes]",
		"Exit codes:",
		"0  No-op / plan empty / setup succeeded",
		"1  Execution failure",
		"2  Invalid command-line arguments",
		"3  Plan file not found",
		"4  Precondition drift or interrupted ledger run",
		"6  Plan generated with pending actions",
	} {
		if !strings.Contains(out, expected) {
			t.Errorf("setup help missing %q:\n%s", expected, out)
		}
	}
}
