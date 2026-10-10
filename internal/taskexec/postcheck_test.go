package taskexec

import (
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
)

func TestBuildFeedback_BoundedAndParsesFailingTest(t *testing.T) {
	long := strings.Repeat("noise line\n", 2000) + "--- FAIL: TestX (0.00s)\n    x_test.go:9: boom\nFAIL\n"
	// the FAIL header is early in real output; put it first to prove parsing uses the full output
	out := "=== RUN TestX\n--- FAIL: TestX (0.00s)\n" + long
	code := 1
	summary := "exit status 1"
	bad := protocol.CheckResult{ID: "go-test", Command: []string{"go", "test", "./..."}, Status: protocol.CheckFail, ExitCode: &code, Summary: &summary}
	other := protocol.CheckResult{ID: "lint", Status: protocol.CheckError}
	fb := buildFeedback(1, 2, protocol.ValidationFail, []protocol.CheckResult{bad, other},
		&bad, map[string]process.Result{"go-test": {Stdout: []byte(out)}})
	for _, want := range []string{"failing check: go-test", "command: go test ./...", "exit code: 1", "summary: exit status 1", "first failing test: TestX", "other non-passing checks: lint", "do not edit the validation profile"} {
		if !strings.Contains(fb, want) {
			t.Errorf("feedback lacks %q", want)
		}
	}
	if len(fb) > feedbackOutputTail+1024 {
		t.Errorf("feedback length %d is not bounded", len(fb))
	}
	if none := buildFeedback(1, 0, protocol.ValidationError, nil, nil, nil); !strings.Contains(none, "did not complete") {
		t.Errorf("no-check feedback = %q", none)
	}
}

func TestPostCheckReportJSON(t *testing.T) {
	pc := &postCheck{profileID: "default", digest: "sha256:x", maxRepair: 2}
	if _, have, err := pc.reportJSON(ExecutionStrict); have || err != nil {
		t.Fatalf("empty report: have=%v err=%v", have, err)
	}
	pc.rounds = []roundReport{{Round: 1, Passed: true}}
	pc.passed = true
	b, have, err := pc.reportJSON(ExecutionUnsafeUnconfinedLocal)
	if err != nil || !have || !strings.Contains(string(b), `"rounds_used":1`) || !strings.Contains(string(b), `"unsafe_unconfined":true`) {
		t.Fatalf("report = %s have=%v err=%v", b, have, err)
	}
}

func TestOptionsValidate_MaxRepairRoundsBounds(t *testing.T) {
	for _, n := range []int{-1, HardMaxRepairRounds + 1} {
		o := Options{MaxRepairRounds: n}
		if err := o.Validate(); err == nil {
			t.Errorf("MaxRepairRounds %d accepted", n)
		}
	}
}

func TestHasUncertainEffects(t *testing.T) {
	cases := map[string]bool{
		"reason=driver_error effects=uncertain":        true,
		"reason=validation_failed effects=none":        false,
		"reason=x effects=uncertain_not effects=none":  false,
		"note=effects=uncertain":                       false,
		"":                                             false,
		"reason=validation_error  effects=uncertain  ": true,
	}
	for in, want := range cases {
		if got := hasUncertainEffects(in); got != want {
			t.Errorf("hasUncertainEffects(%q) = %v, want %v", in, got, want)
		}
	}
}
