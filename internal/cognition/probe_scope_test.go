package cognition_test

import (
	"context"
	"strings"
	"testing"

	"github.com/olostan/DevCadience/internal/cognition"
	"github.com/olostan/DevCadience/internal/environment"
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/protocol"
)

// The tests in this file are about one property: an inference probe happens for
// the endpoint the caller named, and for no other endpoint, ever.
//
// It is a cost and privacy property rather than an aesthetic one. An inference
// probe against an authenticated coding CLI spends the user's real subscription
// quota, and against a local runtime it loads a model into a GPU. A machine
// query that fanned inference out across everything it found would charge a user
// three providers to answer "what is installed here", and would do it as a side
// effect of an inventory command. Discovery and health must therefore stay free
// of inference at every depth they support, and inference must be authorised one
// endpoint at a time.

// twoWorldAdapters returns a local-runtime adapter and a coding-CLI adapter,
// each of which records every probe it is asked to perform.
func twoWorldAdapters() (local, cli *cognition.FakeAdapter) {
	local = &cognition.FakeAdapter{
		AdapterID: "ollama",
		Endpoints: []protocol.CognitionEndpoint{cognition.LocalEndpoint("ollama:small", "ollama", "small")},
		Results: map[string]cognition.ProbeResult{
			"ollama:small": {
				Status:  protocol.FindingObserved,
				Backend: protocol.BackendVulkan,
				Signals: []protocol.AccelerationSignal{authoritative(protocol.BackendVulkan, true)},
			},
		},
	}
	cli = &cognition.FakeAdapter{
		AdapterID: "cli",
		Endpoints: []protocol.CognitionEndpoint{
			cognition.CLIEndpoint("cli:claude-code", "anthropic"),
			cognition.CLIEndpoint("cli:codex-cli", "openai"),
		},
		Results: map[string]cognition.ProbeResult{
			"cli:claude-code": {Status: protocol.FindingObserved, Detail: "answered"},
			"cli:codex-cli":   {Status: protocol.FindingObserved, Detail: "answered"},
		},
	}
	return local, cli
}

// TestProbingOneEndpointInvokesOnlyThatEndpoint is the fan-out guarantee stated
// directly: asking about A must not call B.
func TestProbingOneEndpointInvokesOnlyThatEndpoint(t *testing.T) {
	local, cli := twoWorldAdapters()
	service := newService(t, local, cli)
	_, endpoint, err := service.ProbeEndpoint(context.Background(), cognition.ProbeEndpointInput{
		Facts:      facts(t, environment.LinuxAMDIntegrated(), protocol.DepthHealth),
		EndpointID: "cli:codex-cli",
	})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if endpoint.ID != "cli:codex-cli" {
		t.Fatalf("probed endpoint = %q", endpoint.ID)
	}
	if got := strings.Join(cli.Probed, ","); got != "cli:codex-cli" {
		t.Errorf("the cli adapter probed %q, want only cli:codex-cli", got)
	}
	// The sibling endpoint of the *same* adapter is the one most easily swept up
	// by a loop, and it is a second paid provider.
	for _, id := range cli.Probed {
		if id == "cli:claude-code" {
			t.Error("probing one coding CLI spent quota on another")
		}
	}
	if len(local.Probed) != 0 {
		t.Errorf("probing a coding CLI loaded a local model: %v", local.Probed)
	}
	if len(local.Prompts) != 0 {
		t.Errorf("a prompt reached an endpoint nobody asked about: %v", local.Prompts)
	}
}

// TestProbingALocalRuntimeDoesNotInvokeAnInstalledCodingCLI is the same property
// in the direction that costs money.
func TestProbingALocalRuntimeDoesNotInvokeAnInstalledCodingCLI(t *testing.T) {
	local, cli := twoWorldAdapters()
	service := newService(t, local, cli)
	profileOut, endpoint, err := service.ProbeEndpoint(context.Background(), cognition.ProbeEndpointInput{
		Facts:      facts(t, environment.LinuxAMDIntegrated(), protocol.DepthHealth),
		EndpointID: "ollama:small",
	})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if got := strings.Join(local.Probed, ","); got != "ollama:small" {
		t.Errorf("the local adapter probed %q, want only ollama:small", got)
	}
	if len(cli.Probed) != 0 {
		t.Errorf("probing a local runtime invoked paid coding CLIs: %v", cli.Probed)
	}
	if len(cli.Prompts) != 0 {
		t.Errorf("a prompt was sent to a coding CLI nobody asked about: %v", cli.Prompts)
	}
	// The unprobed endpoints are still *reported* — the point is that they were
	// not invoked, not that they vanished.
	if len(profileOut.Endpoints) != 3 {
		t.Errorf("the profile lost endpoints it did not probe: %+v", profileOut.Endpoints)
	}
	for _, other := range profileOut.Endpoints {
		if other.ID == endpoint.ID {
			continue
		}
		if other.AccelerationVerified() {
			t.Errorf("%s claims verified acceleration without having been probed", other.ID)
		}
		if other.VerifiedAt != nil {
			t.Errorf("%s recorded a verification instant without having been probed", other.ID)
		}
	}
}

// TestNoDepthAvailableToListEverInvokesInference covers the inventory command's
// whole depth range: default, inventory and health.
//
// `cognition list` can reach only these, so this is the assertion that an
// ordinary inventory query cannot cost the user anything.
func TestNoDepthAvailableToListEverInvokesInference(t *testing.T) {
	for _, depth := range []protocol.ProbeDepth{"", protocol.DepthInventory, protocol.DepthHealth} {
		local, cli := twoWorldAdapters()
		service := newService(t, local, cli)
		out, err := service.Profile(context.Background(), cognition.ProfileInput{
			Facts: facts(t, environment.LinuxAMDIntegrated(), protocol.DepthHealth),
			Depth: depth,
		})
		if err != nil {
			t.Fatalf("depth %q: %v", depth, err)
		}
		for name, adapter := range map[string]*cognition.FakeAdapter{"ollama": local, "cli": cli} {
			if len(adapter.Probed) != 0 {
				t.Errorf("depth %q invoked inference on the %s adapter: %v", depth, name, adapter.Probed)
			}
			if len(adapter.Prompts) != 0 {
				t.Errorf("depth %q sent a prompt through the %s adapter: %v", depth, name, adapter.Prompts)
			}
		}
		for _, endpoint := range out.Endpoints {
			if endpoint.AccelerationVerified() {
				t.Errorf("depth %q produced verified acceleration with no inference: %s", depth, endpoint.ID)
			}
		}
	}
}

// TestInferenceDepthWithoutATargetIsRefused is the structural half of the fix:
// the fan-out request cannot be constructed, so no caller can make it by
// accident.
func TestInferenceDepthWithoutATargetIsRefused(t *testing.T) {
	local, cli := twoWorldAdapters()
	service := newService(t, local, cli)
	_, err := service.Profile(context.Background(), cognition.ProfileInput{
		Facts: facts(t, environment.LinuxAMDIntegrated(), protocol.DepthHealth),
		Depth: protocol.DepthInference,
	})
	if err == nil {
		t.Fatal("inference depth was accepted with no endpoint named")
	}
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("category = %q, want invalid_argument", errs.CategoryOf(err))
	}
	if len(local.Probed)+len(cli.Probed) != 0 {
		t.Errorf("a refused request still invoked endpoints: %v %v", local.Probed, cli.Probed)
	}
}

// TestInferenceTargetsAreRefusedBelowInferenceDepth catches the opposite
// mistake: a caller that believes it asked for verification and silently did not.
func TestInferenceTargetsAreRefusedBelowInferenceDepth(t *testing.T) {
	service := newService(t, &cognition.FakeAdapter{AdapterID: "ollama"})
	_, err := service.Profile(context.Background(), cognition.ProfileInput{
		Facts:            facts(t, environment.LinuxAMDIntegrated(), protocol.DepthHealth),
		Depth:            protocol.DepthHealth,
		InferenceTargets: []string{"ollama:small"},
	})
	if err == nil {
		t.Fatal("a target was accepted at a depth that cannot honour it")
	}
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("category = %q, want invalid_argument", errs.CategoryOf(err))
	}
}

// TestProbingAnUnknownEndpointNamesTheAvailableOnesAndInvokesNothing keeps a
// typo cheap.
func TestProbingAnUnknownEndpointNamesTheAvailableOnesAndInvokesNothing(t *testing.T) {
	local, cli := twoWorldAdapters()
	service := newService(t, local, cli)
	_, _, err := service.ProbeEndpoint(context.Background(), cognition.ProbeEndpointInput{
		Facts:      facts(t, environment.LinuxAMDIntegrated(), protocol.DepthHealth),
		EndpointID: "ollama:smal",
	})
	if err == nil {
		t.Fatal("a mistyped endpoint id was accepted")
	}
	if errs.CategoryOf(err) != errs.CategoryNotFound {
		t.Errorf("category = %q, want not_found", errs.CategoryOf(err))
	}
	if !strings.Contains(err.Error(), "ollama:small") {
		t.Errorf("the error does not offer the available ids: %v", err)
	}
	if len(local.Probed)+len(cli.Probed) != 0 {
		t.Errorf("a mistyped id still invoked endpoints: %v %v", local.Probed, cli.Probed)
	}
}

// TestProbeEndpointRequiresAnEndpointID leaves no empty-string path to fan-out.
func TestProbeEndpointRequiresAnEndpointID(t *testing.T) {
	service := newService(t, &cognition.FakeAdapter{AdapterID: "ollama"})
	if _, _, err := service.ProbeEndpoint(context.Background(), cognition.ProbeEndpointInput{
		Facts: facts(t, environment.LinuxAMDIntegrated(), protocol.DepthHealth),
	}); err == nil {
		t.Fatal("an empty endpoint id was accepted")
	}
}

// TestTargetedProbeProducesTheSameEvidenceAsAWholeMachineProbe is the
// no-regression half of the fix: removing the fan-out must not change what a
// probe establishes about the endpoint that was asked about.
//
// The comparison is between the target endpoint's record on a machine where it is
// the only endpoint and on a machine crowded with others. Byte-identical records
// mean the probe path, the acceleration evaluation and the evidence recording are
// untouched, and only the set of endpoints invoked has changed.
func TestTargetedProbeProducesTheSameEvidenceAsAWholeMachineProbe(t *testing.T) {
	machineFacts := facts(t, environment.LinuxAMDIntegrated(), protocol.DepthHealth)

	alone, _ := twoWorldAdapters()
	_, solo, err := newService(t, alone).ProbeEndpoint(context.Background(),
		cognition.ProbeEndpointInput{Facts: machineFacts, EndpointID: "ollama:small"})
	if err != nil {
		t.Fatalf("probe on a bare machine: %v", err)
	}

	local, cli := twoWorldAdapters()
	crowdedProfile, crowded, err := newService(t, local, cli).ProbeEndpoint(context.Background(),
		cognition.ProbeEndpointInput{Facts: machineFacts, EndpointID: "ollama:small"})
	if err != nil {
		t.Fatalf("probe on a crowded machine: %v", err)
	}

	soloDigest, err := protocol.Digest(&solo)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	crowdedDigest, err := protocol.Digest(&crowded)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if soloDigest != crowdedDigest {
		t.Errorf("the probed endpoint's evidence depends on what else is installed:\n%+v\n%+v", solo, crowded)
	}

	// And the evidence is the real thing, not two identically empty records.
	if !crowded.AccelerationVerified() {
		t.Fatalf("acceleration was not verified: %+v", crowded.Acceleration)
	}
	if crowded.Acceleration.Backend != protocol.BackendVulkan {
		t.Errorf("backend = %q, want vulkan", crowded.Acceleration.Backend)
	}
	if crowded.Health != protocol.EndpointHealthReady || crowded.VerifiedAt == nil {
		t.Errorf("health = %q, verified_at = %v", crowded.Health, crowded.VerifiedAt)
	}
	// The machine's candidate list is still lifted by the endpoint that proved it.
	for _, candidate := range crowdedProfile.AcceleratorCandidates {
		if candidate.Backend == protocol.BackendVulkan && candidate.State != protocol.StateVerified {
			t.Errorf("the verified backend did not reach the candidate list: %+v", candidate)
		}
	}
	if crowdedProfile.ProbeDepth != protocol.DepthInference {
		t.Errorf("probe depth = %q, want inference", crowdedProfile.ProbeDepth)
	}
}

// TestATargetedProbeStillSendsOnlyTheSyntheticPrompt is the privacy regression
// check over the changed code path.
func TestATargetedProbeStillSendsOnlyTheSyntheticPrompt(t *testing.T) {
	local, cli := twoWorldAdapters()
	service := newService(t, local, cli)
	if _, _, err := service.ProbeEndpoint(context.Background(), cognition.ProbeEndpointInput{
		Facts:      facts(t, environment.LinuxAMDIntegrated(), protocol.DepthHealth),
		EndpointID: "cli:codex-cli",
	}); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if len(cli.Prompts) != 1 {
		t.Fatalf("prompts = %v, want exactly one", cli.Prompts)
	}
	if cli.Prompts[0] != cognition.SyntheticProbePrompt {
		t.Errorf("a probe sent something other than the synthetic prompt: %q", cli.Prompts[0])
	}
}
