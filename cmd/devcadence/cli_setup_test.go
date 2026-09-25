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

	if len(plan.Actions) > 0 {
		if code := exitCode(err); code != ExitCodePlanGenerated {
			t.Errorf("expected exit code %d (plan generated), got %d (err: %v)", ExitCodePlanGenerated, code, err)
		}
		if !strings.Contains(out, "To approve and apply this plan:") {
			t.Errorf("expected apply instructions in output:\n%s", out)
		}
	} else {
		// A nil error exits 0 without going through exitCode: exitCode(nil)
		// classifies an absent error as CategoryInternal (exit 1), which is
		// not a code path main() exercises (see
		// TestDoctorReadinessExitErrorExactExitCodes for the same point).
		if err != nil {
			t.Errorf("expected nil error (no-op), got: %v", err)
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

	if len(plan.Actions) > 0 {
		if code := exitCode(err); code != ExitCodePlanGenerated {
			t.Errorf("expected exit code %d, got %d", ExitCodePlanGenerated, code)
		}
	} else if err != nil {
		t.Errorf("expected nil error (no-op), got: %v", err)
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

// simulateCLIInterruptedAction appends the durable event sequence a real
// `setup apply` writes before crashing mid-action (execution_created,
// plan_approved, action_starting with no matching action terminal event) to
// the ledger at c's home directory, so `setup recover` finds exactly one
// interrupted action for actionID when the test later runs it.
//
// Earlier versions of this helper's call sites built these events inline
// and discarded Append's error (`_, _ = ledger.Append(...)`), which masked
// a real defect: ExecutionCreatedPayload.InitiatedBy and
// ActionStartingPayload.IdempotencyKey are both required, so every such
// Append silently failed validation and the ledger stayed empty — meaning
// setup recover always found zero interrupted actions and the assertions
// that never checked the recovered count (only substring-matching the
// generic "Setup Recovery Reconciled" banner) passed vacuously. Checking
// each Append's error here is what caught it (independent-review follow-up
// on WP-M3B-7, FIX_NOW-4).
func simulateCLIInterruptedAction(t *testing.T, c *cli, plan *protocol.SetupPlan, action protocol.SetupAction) {
	t.Helper()
	ledgerPath := filepath.Join(c.db, "..", "state", "setup-ledger.jsonl")
	if err := os.MkdirAll(filepath.Dir(ledgerPath), 0o700); err != nil {
		t.Fatalf("create ledger dir: %v", err)
	}
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
}

func TestCLISetupRecoverReconcilesInterruptedActions(t *testing.T) {
	c := newCLI(t)
	planFile := filepath.Join(t.TempDir(), "recover-plan.json")

	action := setup.NewCreateDirectoryAction("act_01", protocol.LocationState, "1.0.0")
	fullPlan := buildActionPlan(t, protocol.TargetHardware, action)

	planBytes, _ := json.MarshalIndent(fullPlan, "", "  ")
	_ = os.WriteFile(planFile, planBytes, 0o600)

	simulateCLIInterruptedAction(t, c, fullPlan, action)

	out, stderr, err := c.run("setup", "recover", "--plan", planFile)
	if err != nil {
		t.Fatalf("setup recover failed: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(out, "Setup Recovery Reconciled") {
		t.Errorf("expected recovery message in output:\n%s", out)
	}
	if !strings.Contains(out, "Actions:        1") {
		t.Errorf("expected exactly one reconciled action in output:\n%s", out)
	}
	if !strings.Contains(out, action.ActionID) {
		t.Errorf("expected the reconciled action's ID %q in output:\n%s", action.ActionID, out)
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
		"5  Plan file is malformed, semantically invalid, or fails schema validation",
		"6  Plan generated with pending actions",
	} {
		if !strings.Contains(out, expected) {
			t.Errorf("setup help missing %q:\n%s", expected, out)
		}
	}
}

// TestCLISetupApplyMissingPlanFileReturnsExitCode3 and
// TestCLISetupRecoverMissingPlanFileReturnsExitCode3 are the independent-
// review follow-up on WP-M3B-7, FIX_NOW-1/FIX_NOW-4: a plan file that
// simply does not exist must be exit 3 (CategoryNotFound), distinct from a
// plan file that exists but is invalid (exit 5).
func TestCLISetupApplyMissingPlanFileReturnsExitCode3(t *testing.T) {
	c := newCLI(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist.json")

	_, _, err := c.run("setup", "apply", "--plan", missing, "--approve-plan", "sha256:0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected setup apply against a missing plan file to fail")
	}
	if code := exitCode(err); code != ExitCodeNotFound {
		t.Errorf("expected exit code %d (not found), got %d (err: %v)", ExitCodeNotFound, code, err)
	}
}

func TestCLISetupRecoverMissingPlanFileReturnsExitCode3(t *testing.T) {
	c := newCLI(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist.json")

	_, _, err := c.run("setup", "recover", "--plan", missing)
	if err == nil {
		t.Fatal("expected setup recover against a missing plan file to fail")
	}
	if code := exitCode(err); code != ExitCodeNotFound {
		t.Errorf("expected exit code %d (not found), got %d (err: %v)", ExitCodeNotFound, code, err)
	}
}

// TestCLISetupApplyCorruptPlanFileReturnsExitCode5 and
// TestCLISetupRecoverCorruptPlanFileReturnsExitCode5 prove a plan file that
// exists but is not valid JSON is classified CategoryIntegrity (exit 5),
// not CategoryInvalidArgument (exit 2) — the exact defect the round-3
// reviewer reproduced end to end against `setup apply` and required to be
// fixed for `setup recover` too (independent-review follow-up on
// WP-M3B-7, FIX_NOW-1).
func TestCLISetupApplyCorruptPlanFileReturnsExitCode5(t *testing.T) {
	c := newCLI(t)
	planFile := filepath.Join(t.TempDir(), "corrupt-plan.json")
	if err := os.WriteFile(planFile, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("write corrupt plan fixture: %v", err)
	}

	_, _, err := c.run("setup", "apply", "--plan", planFile, "--approve-plan", "sha256:0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected setup apply against a corrupt plan file to fail")
	}
	if code := exitCode(err); code != ExitCodeIntegrity {
		t.Errorf("expected exit code %d (integrity), got %d (err: %v)", ExitCodeIntegrity, code, err)
	}
}

func TestCLISetupRecoverCorruptPlanFileReturnsExitCode5(t *testing.T) {
	c := newCLI(t)
	planFile := filepath.Join(t.TempDir(), "corrupt-plan.json")
	if err := os.WriteFile(planFile, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("write corrupt plan fixture: %v", err)
	}

	_, _, err := c.run("setup", "recover", "--plan", planFile)
	if err == nil {
		t.Fatal("expected setup recover against a corrupt plan file to fail")
	}
	if code := exitCode(err); code != ExitCodeIntegrity {
		t.Errorf("expected exit code %d (integrity), got %d (err: %v)", ExitCodeIntegrity, code, err)
	}
}

// TestCLISetupApplyInvalidPlanFileReturnsExitCode5 proves a plan file that
// is well-formed JSON but fails SetupPlan's own semantic Validate() (here,
// an unsupported schema_version) is also exit 5, not exit 2.
func TestCLISetupApplyInvalidPlanFileReturnsExitCode5(t *testing.T) {
	c := newCLI(t)
	planFile := filepath.Join(t.TempDir(), "unsupported-version-plan.json")
	action := setup.NewCreateDirectoryAction("act_01", protocol.LocationState, "1.0.0")
	fullPlan := buildActionPlan(t, protocol.TargetHardware, action)
	fullPlan.SchemaVersion = "99.0"

	planBytes, err := json.Marshal(fullPlan)
	if err != nil {
		t.Fatalf("marshal fixture plan: %v", err)
	}
	if err := os.WriteFile(planFile, planBytes, 0o600); err != nil {
		t.Fatalf("write fixture plan: %v", err)
	}

	_, _, err = c.run("setup", "apply", "--plan", planFile, "--approve-plan", fullPlan.PlanDigest)
	if err == nil {
		t.Fatal("expected setup apply against an unsupported schema_version to fail")
	}
	if code := exitCode(err); code != ExitCodeIntegrity {
		t.Errorf("expected exit code %d (integrity), got %d (err: %v)", ExitCodeIntegrity, code, err)
	}
}

// TestCLISetupApplyYesUserConfirmationSucceeds proves the --yes
// non-interactive authorization path exits 0 on a plan whose required
// authority is within scope (user_confirmation), not merely that it is
// rejected when out of scope (TestCLISetupApplyYesScopeRejectsPrivilegedPlan
// already covers the rejection case).
func TestCLISetupApplyYesUserConfirmationSucceeds(t *testing.T) {
	c := newCLI(t)
	c.isTerminal = func() bool { return false }
	planFile := filepath.Join(t.TempDir(), "plan.json")

	// NewCreateDirectoryAction's operation kind intrinsically carries
	// AuthorityUserConfirmation (protocol.IntrinsicPolicy), so this plan is
	// exactly the --yes-eligible case without needing to override Authority.
	action := setup.NewCreateDirectoryAction("act_01", protocol.LocationState, "1.0.0")
	fullPlan := buildActionPlan(t, protocol.TargetHardware, action)
	if fullPlan.RequiredAuthority != protocol.AuthorityUserConfirmation {
		t.Fatalf("fixture precondition: required_authority = %s, want %s", fullPlan.RequiredAuthority, protocol.AuthorityUserConfirmation)
	}

	planBytes, _ := json.MarshalIndent(fullPlan, "", "  ")
	_ = os.WriteFile(planFile, planBytes, 0o600)

	out, stderr, err := c.run("setup", "apply", "--plan", planFile, "--approve-plan", fullPlan.PlanDigest, "--yes")
	if err != nil {
		t.Fatalf("expected --yes on a user_confirmation-authority plan to succeed, got: %v (stderr: %s)", err, stderr)
	}
	if strings.Contains(out, "\x1b") || strings.Contains(stderr, "\x1b") {
		t.Errorf("setup apply --yes output contains ANSI control sequences")
	}
	if !strings.Contains(out, "Setup completed successfully.") {
		t.Errorf("expected success message in output:\n%s", out)
	}
}

// TestCLISetupRecoverOutputHasNoANSI closes the ANSI-absence gap FIX_NOW-4
// noted for setup recover's plain-text output path (setup apply and setup
// plan already carry this assertion elsewhere in this file).
func TestCLISetupRecoverOutputHasNoANSI(t *testing.T) {
	c := newCLI(t)
	planFile := filepath.Join(t.TempDir(), "recover-plan.json")
	action := setup.NewCreateDirectoryAction("act_01", protocol.LocationState, "1.0.0")
	fullPlan := buildActionPlan(t, protocol.TargetHardware, action)
	planBytes, _ := json.MarshalIndent(fullPlan, "", "  ")
	_ = os.WriteFile(planFile, planBytes, 0o600)

	simulateCLIInterruptedAction(t, c, fullPlan, action)

	out, stderr, err := c.run("setup", "recover", "--plan", planFile)
	if err != nil {
		t.Fatalf("setup recover failed: %v (stderr: %s)", err, stderr)
	}
	if strings.Contains(out, "\x1b") || strings.Contains(stderr, "\x1b") {
		t.Errorf("setup recover output contains ANSI control sequences")
	}
	if !strings.Contains(out, "Actions:        1") {
		t.Errorf("expected exactly one reconciled action in output:\n%s", out)
	}
}

// TestCLISetupRecoverJSONValidatesAgainstSchema proves `setup recover
// --json` emits the versioned, schema-governed SetupRecoveryReport rather
// than an unvalidated ad hoc map (independent-review follow-up on
// WP-M3B-7, FIX_NOW-2).
func TestCLISetupRecoverJSONValidatesAgainstSchema(t *testing.T) {
	c := newCLI(t)
	planFile := filepath.Join(t.TempDir(), "recover-plan.json")
	action := setup.NewCreateDirectoryAction("act_01", protocol.LocationState, "1.0.0")
	fullPlan := buildActionPlan(t, protocol.TargetHardware, action)
	planBytes, _ := json.MarshalIndent(fullPlan, "", "  ")
	_ = os.WriteFile(planFile, planBytes, 0o600)

	simulateCLIInterruptedAction(t, c, fullPlan, action)

	out, stderr, err := c.run("setup", "recover", "--plan", planFile, "--json")
	if err != nil {
		t.Fatalf("setup recover --json failed: %v (stderr: %s)", err, stderr)
	}

	set, sErr := schema.Default()
	if sErr != nil {
		t.Fatalf("schema.Default: %v", sErr)
	}
	if vErr := set.ValidateBytes(schema.NameSetupRecoveryReport, []byte(out)); vErr != nil {
		t.Fatalf("emitted SetupRecoveryReport does not satisfy schema: %v\nJSON:\n%s", vErr, out)
	}

	var report protocol.SetupRecoveryReport
	if decErr := protocol.Unmarshal([]byte(out), &report); decErr != nil {
		t.Fatalf("failed to decode SetupRecoveryReport: %v", decErr)
	}
	if report.PlanID != fullPlan.PlanID {
		t.Errorf("plan_id = %q, want %q", report.PlanID, fullPlan.PlanID)
	}
	if len(report.Results) != 1 || report.Results[0].ActionID != action.ActionID {
		t.Errorf("results = %+v, want one result for %q", report.Results, action.ActionID)
	}
}

// TestSetupPlanRejectsConflictingTargetAndExtraPositionals is the
// independent-review follow-up on WP-M3B-7, FIX_NOW-3: `setup plan` must
// not silently accept a positional target that conflicts with --target, or
// extra positional arguments beyond the one target it documents.
func TestCLISetupPlanRejectsConflictingTargetAndExtraPositionals(t *testing.T) {
	c := newCLI(t)

	t.Run("conflicting positional and --target", func(t *testing.T) {
		_, _, err := c.run("setup", "plan", "hardware", "--target", "cognition")
		if err == nil {
			t.Fatal("expected conflicting positional target and --target to fail")
		}
		if code := exitCode(err); code != ExitCodeInvalidArgument {
			t.Errorf("expected exit code %d (invalid argument), got %d (err: %v)", ExitCodeInvalidArgument, code, err)
		}
	})

	t.Run("extra positional arguments", func(t *testing.T) {
		_, _, err := c.run("setup", "plan", "hardware", "cognition")
		if err == nil {
			t.Fatal("expected extra positional arguments to fail")
		}
		if code := exitCode(err); code != ExitCodeInvalidArgument {
			t.Errorf("expected exit code %d (invalid argument), got %d (err: %v)", ExitCodeInvalidArgument, code, err)
		}
	})
}
