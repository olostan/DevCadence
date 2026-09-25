package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/schema"
)

func TestCLIDoctorDescribesMachine(t *testing.T) {
	c := newCLI(t)
	out, stderr, err := c.run("doctor")
	// If the test environment is missing state root dirs or similar, doctor may exit 0 or 1.
	// Either way, it must produce structured text without ANSI escape sequences.
	if strings.Contains(out, "\x1b") || strings.Contains(stderr, "\x1b") {
		t.Errorf("doctor output contains ANSI control sequences:\nstdout:\n%s\nstderr:\n%s", out, stderr)
	}

	for _, section := range []string{
		"DevCadence Doctor Report",
		"Machine Fingerprint:",
		"Readiness:",
		"Observed At:",
	} {
		if !strings.Contains(out, section) {
			t.Errorf("the doctor output has no %q section:\n%s", section, out)
		}
	}

	// Exit code contract: 0 for ready, 1 for unready (ACTION_REQUIRED or PARTIALLY_READY)
	code := exitCode(err)
	if code != ExitCodeSuccess && code != ExitCodeExecutionError {
		t.Errorf("expected exit code 0 or 1, got %d (err: %v)", code, err)
	}
}

func TestCLIDoctorJSONValidatesAgainstSchema(t *testing.T) {
	c := newCLI(t)
	out, stderr, err := c.run("doctor", "--json")
	if out == "" {
		t.Fatalf("expected JSON output from doctor --json, got empty (stderr: %s, err: %v)", stderr, err)
	}

	set, sErr := schema.Default()
	if sErr != nil {
		t.Fatalf("schema.Default: %v", sErr)
	}
	if vErr := set.ValidateBytes(schema.NameDoctorReport, []byte(out)); vErr != nil {
		t.Fatalf("emitted DoctorReport does not validate against schema: %v\nJSON:\n%s", vErr, out)
	}

	var report protocol.DoctorReport
	if decErr := json.Unmarshal([]byte(out), &report); decErr != nil {
		t.Fatalf("failed to decode DoctorReport JSON: %v", decErr)
	}
	if valErr := report.Validate(); valErr != nil {
		t.Fatalf("DoctorReport.Validate failed: %v", valErr)
	}
}

func TestCLIDoctorRefusesInferenceDepth(t *testing.T) {
	c := newCLI(t)
	_, _, err := c.run("doctor", "--depth", "inference")
	if err == nil {
		t.Fatal("doctor accepted inference depth")
	}
	if !strings.Contains(err.Error(), "cognition probe") {
		t.Errorf("refusal does not mention cognition probe: %v", err)
	}
	if code := exitCode(err); code != ExitCodeInvalidArgument {
		t.Errorf("expected exit code %d (invalid argument), got %d", ExitCodeInvalidArgument, code)
	}
}

func TestCLIDoctorFixGeneratesPlan(t *testing.T) {
	c := newCLI(t)
	planFile := filepath.Join(t.TempDir(), "setup-plan.json")
	out, stderr, err := c.run("doctor", "--fix", "--output", planFile)

	if strings.Contains(out, "\x1b") || strings.Contains(stderr, "\x1b") {
		t.Errorf("doctor --fix output contains ANSI control sequences")
	}

	// Verify plan file exists and validates against schema
	planBytes, readErr := os.ReadFile(planFile)
	if readErr != nil {
		t.Fatalf("expected plan file %s to be created: %v", planFile, readErr)
	}

	set, sErr := schema.Default()
	if sErr != nil {
		t.Fatalf("schema.Default: %v", sErr)
	}
	if vErr := set.ValidateBytes(schema.NameSetupPlan, planBytes); vErr != nil {
		t.Fatalf("emitted SetupPlan file does not validate against schema: %v\nJSON:\n%s", vErr, string(planBytes))
	}

	var plan protocol.SetupPlan
	if decErr := protocol.Unmarshal(planBytes, &plan); decErr != nil {
		t.Fatalf("failed to decode SetupPlan: %v", decErr)
	}
	if valErr := plan.Validate(); valErr != nil {
		t.Fatalf("plan.Validate failed: %v", valErr)
	}

	// Exit code contract: if plan has actions, exit 6 (ExitCodePlanGenerated), else nil error (exit 0)
	if len(plan.Actions) > 0 {
		if code := exitCode(err); code != ExitCodePlanGenerated {
			t.Errorf("expected exit code %d (plan generated), got %d (err: %v)", ExitCodePlanGenerated, code, err)
		}
		if !strings.Contains(out, "To approve and apply this plan:") {
			t.Errorf("expected apply instructions in output:\n%s", out)
		}
	} else if err != nil {
		t.Errorf("expected nil error (no-op), got: %v", err)
	}
}

func TestCLIDoctorFixJSONValidatesAgainstSchema(t *testing.T) {
	c := newCLI(t)
	out, stderr, err := c.run("doctor", "--fix", "--json")
	if out == "" {
		t.Fatalf("expected JSON output from doctor --fix --json, got empty (stderr: %s, err: %v)", stderr, err)
	}

	set, sErr := schema.Default()
	if sErr != nil {
		t.Fatalf("schema.Default: %v", sErr)
	}
	if vErr := set.ValidateBytes(schema.NameSetupPlan, []byte(out)); vErr != nil {
		t.Fatalf("emitted SetupPlan does not validate against schema: %v\nJSON:\n%s", vErr, out)
	}

	var plan protocol.SetupPlan
	if decErr := protocol.Unmarshal([]byte(out), &plan); decErr != nil {
		t.Fatalf("failed to decode SetupPlan JSON: %v", decErr)
	}
	if valErr := plan.Validate(); valErr != nil {
		t.Fatalf("plan.Validate failed: %v", valErr)
	}

	if len(plan.Actions) > 0 {
		if code := exitCode(err); code != ExitCodePlanGenerated {
			t.Errorf("expected exit code %d, got %d", ExitCodePlanGenerated, code)
		}
	} else if err != nil {
		t.Errorf("expected nil error (no-op), got: %v", err)
	}
}

// TestDoctorReadinessExitErrorExactExitCodes is the independent-review
// follow-up on WP-M3B-7, FIX_NOW-4: the doctor "ready = 0, action required =
// 1" exit-code contract needs a deterministic, exact assertion, not only a
// live CLI run whose readiness depends on the test host's real hardware and
// software state. doctorReadinessExitError is exercised directly against
// synthetic reports so every readiness value's mapped exit code is proven.
func TestDoctorReadinessExitErrorExactExitCodes(t *testing.T) {
	cases := []struct {
		readiness protocol.ReadinessStatus
		wantCode  int
	}{
		{protocol.ReadinessReady, ExitCodeSuccess},
		{protocol.ReadinessReadyWithReducedCap, ExitCodeSuccess},
		{protocol.ReadinessPartiallyReady, ExitCodeExecutionError},
		{protocol.ReadinessActionRequired, ExitCodeExecutionError},
	}
	for _, tc := range cases {
		t.Run(string(tc.readiness), func(t *testing.T) {
			err := doctorReadinessExitError(&protocol.DoctorReport{Readiness: tc.readiness})
			// main() only calls exitCode on a non-nil error (a nil error
			// exits 0 implicitly without going through exitCode at all), so
			// a nil result here is asserted directly rather than through
			// exitCode(nil), which classifies an absent error as
			// CategoryInternal and is not a code path main() exercises.
			if err == nil {
				if tc.wantCode != ExitCodeSuccess {
					t.Errorf("readiness %s: got nil error, want exit %d", tc.readiness, tc.wantCode)
				}
				return
			}
			if code := exitCode(err); code != tc.wantCode {
				t.Errorf("readiness %s: exitCode = %d, want %d (err: %v)", tc.readiness, code, tc.wantCode, err)
			}
		})
	}
}

// TestPlanActionsExitErrorExactExitCodes is the independent-review
// follow-up on WP-M3B-7, FIX_NOW-4: "doctor --fix already-ready/no-action
// 0" versus "plan generated 6" needs a deterministic, exact assertion
// rather than depending on whether the test host happens to need
// remediation. planActionsExitError backs both `doctor --fix` and `setup
// plan`, so this single pure-function test covers the exit-code contract
// for both commands' plan-generation outcome.
func TestPlanActionsExitErrorExactExitCodes(t *testing.T) {
	t.Run("no actions is a no-op success", func(t *testing.T) {
		// A nil error exits 0 without going through exitCode at all (see
		// doctorReadinessExitError's test above for why exitCode(nil) is
		// not the right assertion here).
		if err := planActionsExitError(&protocol.SetupPlan{}); err != nil {
			t.Errorf("expected nil error for a plan with no actions, got: %v", err)
		}
	})
	t.Run("pending actions require approval", func(t *testing.T) {
		plan := &protocol.SetupPlan{
			PlanID:  "plan_test",
			Actions: []protocol.SetupAction{{ActionID: "act_01"}},
		}
		err := planActionsExitError(plan)
		if code := exitCode(err); code != ExitCodePlanGenerated {
			t.Errorf("exitCode = %d, want %d (err: %v)", code, ExitCodePlanGenerated, err)
		}
	})
}

func TestCLIDoctorRejectsUnsupportedScope(t *testing.T) {
	c := newCLI(t)
	_, _, err := c.run("doctor", "--scope", "custom-scope-xyz")
	if err == nil {
		t.Fatal("doctor accepted an unsupported --scope value")
	}
	if code := exitCode(err); code != ExitCodeInvalidArgument {
		t.Errorf("expected exit code %d (invalid argument), got %d (err: %v)", ExitCodeInvalidArgument, code, err)
	}
}

func TestCLIDoctorRejectsInvalidTargetEvenWithoutFix(t *testing.T) {
	c := newCLI(t)
	_, _, err := c.run("doctor", "--target", "not-a-real-target")
	if err == nil {
		t.Fatal("doctor accepted an invalid --target value without --fix")
	}
	if code := exitCode(err); code != ExitCodeInvalidArgument {
		t.Errorf("expected exit code %d (invalid argument), got %d (err: %v)", ExitCodeInvalidArgument, code, err)
	}
}

func TestCLIDoctorHelpDocumentsExitCodes(t *testing.T) {
	c := newCLI(t)
	_, stderr, err := c.run("doctor", "-h")
	if err != nil && exitCode(err) != ExitCodeSuccess {
		t.Errorf("unexpected error from doctor -h: %v", err)
	}
	for _, expected := range []string{
		"usage: devcadence doctor",
		"Exit codes:",
		"0  Ready",
		"1  Action required",
		"2  Invalid command-line arguments",
		"6  Plan generated",
	} {
		if !strings.Contains(stderr, expected) {
			t.Errorf("doctor help missing %q:\n%s", expected, stderr)
		}
	}
}
