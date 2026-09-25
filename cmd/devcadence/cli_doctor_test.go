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

	// Exit code contract: if plan has actions, exit 6 (ExitCodePlanGenerated), else 0 (ExitCodeSuccess)
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
