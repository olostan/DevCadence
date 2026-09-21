package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/olostan/DevCadience/internal/protocol"
	"github.com/olostan/DevCadience/internal/schema"
)

// These tests run the inspection commands against the machine executing the
// suite, which is deliberate and safe: every one of them is read-only, and the
// assertions are about *shape and honesty* rather than about what happens to be
// installed. They therefore pass identically on a developer's Apple Silicon
// laptop, on a GPU box and in a container with nothing but Git — which is the
// property the whole milestone is about.
//
// Nothing here requires a model runtime, a GPU, credentials or a network, and
// no test invokes an inference probe: doing so would spend a real user's quota
// from a unit test.

// TestEnvironmentInspectDescribesThisMachine checks that discovery answers the
// basic questions on whatever host runs it.
func TestEnvironmentInspectDescribesThisMachine(t *testing.T) {
	c := newCLI(t)
	out := c.mustRun("environment", "inspect")
	for _, section := range []string{
		"Host", "CPU", "Memory", "Accelerators", "Software",
		"Principal hosts", "Backend candidates",
	} {
		if !strings.Contains(out, section) {
			t.Errorf("the inspection has no %q section:\n%s", section, out)
		}
	}
	// The CPU fallback candidate exists on every machine, so its absence would
	// mean assessment did not run at all.
	if !strings.Contains(out, "cpu      support=supported") {
		t.Errorf("no cpu backend candidate was reported:\n%s", out)
	}
	// The command must state the acceleration rule rather than leaving a reader
	// to assume a "supported" backend is in use.
	if !strings.Contains(out, "never reports a backend as verified") {
		t.Errorf("the output does not distinguish assessment from verification:\n%s", out)
	}
}

// TestEnvironmentInspectJSONMatchesTheContract proves the machine-readable form
// is the published type, not an ad-hoc rendering.
func TestEnvironmentInspectJSONMatchesTheContract(t *testing.T) {
	c := newCLI(t)
	out := c.mustRun("environment", "inspect", "--json")
	var facts protocol.EnvironmentFacts
	decoder := json.NewDecoder(strings.NewReader(out))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&facts); err != nil {
		t.Fatalf("the JSON output does not decode into the published type: %v", err)
	}
	if err := facts.Validate(); err != nil {
		t.Fatalf("the emitted facts violate the contract: %v", err)
	}
	if facts.Host.Arch == "" {
		t.Error("the architecture was not reported")
	}
	if len(facts.Software) == 0 {
		t.Error("the software inventory is empty, so nothing was looked for")
	}
}

// TestEnvironmentInspectRefusesInferenceDepth is the performance rule: an
// ordinary environment query must not be able to load a model.
func TestEnvironmentInspectRefusesInferenceDepth(t *testing.T) {
	c := newCLI(t)
	_, _, err := c.run("environment", "inspect", "--depth", "inference")
	if err == nil {
		t.Fatal("environment inspect accepted inference depth")
	}
	if !strings.Contains(err.Error(), "cognition probe") {
		t.Errorf("the refusal does not point at the command that does verify: %v", err)
	}
}

func TestEnvironmentInspectRejectsAnUnknownDepth(t *testing.T) {
	c := newCLI(t)
	if _, _, err := c.run("environment", "inspect", "--depth", "everything"); err == nil {
		t.Fatal("an unknown depth was accepted")
	}
}

// TestCognitionListIsHonestAboutThisMachine checks the profile surface without
// assuming any AI software is present.
func TestCognitionListIsHonestAboutThisMachine(t *testing.T) {
	c := newCLI(t)
	out := c.mustRun("cognition", "list")
	if !strings.Contains(out, "machine      sha256:") {
		t.Errorf("no machine fingerprint was reported:\n%s", out)
	}
	if !strings.Contains(out, "assessment   ") {
		t.Errorf("no readiness assessment was reported:\n%s", out)
	}
	// Whatever this machine has, the answer must be one of the defined states —
	// never a failure — and an absent runtime must read as a state.
	var found bool
	for _, assessment := range []protocol.CognitionAssessment{
		protocol.AssessmentReady, protocol.AssessmentReadyReducedCapability,
		protocol.AssessmentModelCognitionUnavailable, protocol.AssessmentPartiallyReady,
		protocol.AssessmentUnknown,
	} {
		if strings.Contains(out, "assessment   "+string(assessment)) {
			found = true
		}
	}
	if !found {
		t.Errorf("the assessment is not one of the defined states:\n%s", out)
	}
	// No endpoint may claim a capability grade from discovery alone.
	if strings.Contains(out, "implementation         strong (unknown)") {
		t.Errorf("a capability was graded without provenance:\n%s", out)
	}
}

// TestCognitionListJSONSatisfiesItsSchema closes the twin loop through the CLI.
func TestCognitionListJSONSatisfiesItsSchema(t *testing.T) {
	c := newCLI(t)
	out := c.mustRun("cognition", "list", "--json")
	set, err := schema.Default()
	if err != nil {
		t.Fatalf("schemas: %v", err)
	}
	if err := set.ValidateBytes(schema.NameMachineCapabilityProfile, []byte(out)); err != nil {
		t.Fatalf("the emitted profile does not satisfy its schema: %v", err)
	}
	var profile protocol.MachineCapabilityProfile
	if err := protocol.Unmarshal([]byte(out), &profile); err != nil {
		t.Fatalf("the emitted profile does not decode strictly: %v", err)
	}
	if profile.ProbeDepth != protocol.DepthHealth {
		t.Errorf("probe depth = %q, want health by default", profile.ProbeDepth)
	}
	// A profile produced without inference must not contain a verified backend.
	for _, endpoint := range profile.Endpoints {
		if endpoint.AccelerationVerified() {
			t.Errorf("endpoint %s claims verified acceleration at health depth", endpoint.ID)
		}
	}
}

// TestCognitionRouteExplainsEveryRole is the explainability requirement, checked
// through the surface an operator actually uses.
func TestCognitionRouteExplainsEveryRole(t *testing.T) {
	c := newCLI(t)
	out := c.mustRun("cognition", "route")
	for _, role := range []string{
		"scout:", "classifier:", "implementer:", "correctness_reviewer:", "architecture_reviewer:",
	} {
		if !strings.Contains(out, role) {
			t.Errorf("role %s was not routed:\n%s", role, out)
		}
	}
	if !strings.Contains(out, "policy: source_exposure<=") {
		t.Errorf("the policy in force was not stated:\n%s", out)
	}
	// Every decision states either a selection or a reason there is none.
	if !strings.Contains(out, "selected:") {
		t.Errorf("no selection line was emitted:\n%s", out)
	}
	if !strings.Contains(out, "reasons:") {
		t.Errorf("no reasons were given:\n%s", out)
	}
	// No numeric score may appear in an explanation.
	for _, forbidden := range []string{"score", "weight=", "confidence"} {
		if strings.Contains(strings.ToLower(out), forbidden) {
			t.Errorf("the explanation contains %q:\n%s", forbidden, out)
		}
	}
}

// TestCognitionRouteRespectsAPrivacyPolicyFromTheCommandLine exercises the
// hard privacy constraint end to end.
func TestCognitionRouteRespectsAPrivacyPolicyFromTheCommandLine(t *testing.T) {
	c := newCLI(t)
	out := c.mustRun("cognition", "route", "--role", "implementer", "--source-exposure", "local_only")
	if !strings.Contains(out, "source_exposure<=local_only") {
		t.Errorf("the policy was not applied:\n%s", out)
	}
	// Any endpoint needing more than local_only must be rejected with the
	// policy named. On a machine with no such endpoint there is nothing to
	// check, which is itself a valid outcome.
	if strings.Contains(out, "tool_mediated_worktree") &&
		!strings.Contains(out, "exceeds the project policy of local_only") {
		t.Errorf("a remote endpoint was not rejected by the privacy policy:\n%s", out)
	}
}

func TestCognitionRouteRejectsUnknownPolicyValues(t *testing.T) {
	c := newCLI(t)
	for _, args := range [][]string{
		{"cognition", "route", "--source-exposure", "everything"},
		{"cognition", "route", "--max-cost", "free"},
		{"cognition", "route", "--role", "oracle"},
	} {
		if _, _, err := c.run(args...); err == nil {
			t.Errorf("%v was accepted", args)
		}
	}
}

// TestCognitionRouteJSONIsMachineReadable covers the --json contract for the
// decision surface.
func TestCognitionRouteJSONIsMachineReadable(t *testing.T) {
	c := newCLI(t)
	out := c.mustRun("cognition", "route", "--role", "scout", "--json")
	var decisions []struct {
		Role       string `json:"Role"`
		Outcome    string `json:"Outcome"`
		SelectedID string `json:"SelectedID"`
		Reasons    []string
		Rejected   []struct {
			EndpointID string
			Eligible   bool
			Reasons    []string
		}
	}
	if err := json.Unmarshal([]byte(out), &decisions); err != nil {
		t.Fatalf("the routing output is not valid JSON: %v\n%s", err, out)
	}
	if len(decisions) != 1 {
		t.Fatalf("expected one decision, got %d", len(decisions))
	}
	if decisions[0].Role != "scout" {
		t.Errorf("role = %q", decisions[0].Role)
	}
	if decisions[0].Outcome != "selected" && decisions[0].Outcome != "no_eligible_endpoint" {
		t.Errorf("outcome = %q", decisions[0].Outcome)
	}
	if len(decisions[0].Reasons) == 0 {
		t.Error("a decision was emitted with no reasons")
	}
}

// TestCognitionProbeRequiresAnEndpointID checks argument handling without
// invoking a probe: running one would spend a real user's quota from a test.
func TestCognitionProbeRequiresAnEndpointID(t *testing.T) {
	c := newCLI(t)
	if _, _, err := c.run("cognition", "probe"); err == nil {
		t.Fatal("cognition probe ran with no endpoint")
	}
}

// TestUnknownSubcommandsAreRejected keeps the surface from silently accepting
// something that does not exist — notably `setup` and `doctor`, which are M3B.
func TestUnknownSubcommandsAreRejected(t *testing.T) {
	c := newCLI(t)
	for _, args := range [][]string{
		{"environment", "configure"},
		{"cognition", "install"},
		{"setup"},
		{"doctor"},
	} {
		if _, _, err := c.run(args...); err == nil {
			t.Errorf("%v was accepted; %v is not implemented in M3A", args, args[0])
		}
	}
}

// TestInspectionCommandsNeedNoDatabase is a small but real property: reading the
// machine has nothing to do with project state, so a missing control-plane
// database must not stop it.
func TestInspectionCommandsNeedNoDatabase(t *testing.T) {
	c := newCLI(t)
	// c.db points at a path inside a fresh temp dir that no test has created.
	if _, _, err := c.run("environment", "inspect"); err != nil {
		t.Errorf("environment inspect required a database: %v", err)
	}
	if _, _, err := c.run("cognition", "list"); err != nil {
		t.Errorf("cognition list required a database: %v", err)
	}
}
