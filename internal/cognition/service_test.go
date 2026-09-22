package cognition_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadience/internal/artifacts"
	"github.com/olostan/DevCadience/internal/clock"
	"github.com/olostan/DevCadience/internal/cognition"
	"github.com/olostan/DevCadience/internal/environment"
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/ids"
	"github.com/olostan/DevCadience/internal/protocol"
	"github.com/olostan/DevCadience/internal/schema"
)

func testClock() clock.Clock {
	return clock.NewFake(time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC), time.Second)
}

// deterministicIDs produces predictable profile ids so profiles can be compared.
type deterministicIDs struct{ n int }

func (d *deterministicIDs) New(prefix string) string {
	d.n++
	return prefix + "_0000000000000000000000000" + string(rune('0'+d.n%10))
}

func facts(t *testing.T, fixture environment.Fixture, depth protocol.ProbeDepth) protocol.EnvironmentFacts {
	t.Helper()
	observed, err := fixture.Discover(context.Background(), testClock(), depth)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	return observed
}

func newService(t *testing.T, adapters ...cognition.Adapter) *cognition.Service {
	t.Helper()
	service, err := cognition.NewService(cognition.Options{
		Adapters: adapters, Clock: testClock(), IDs: &deterministicIDs{},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return service
}

func profile(t *testing.T, service *cognition.Service, in cognition.ProfileInput) protocol.MachineCapabilityProfile {
	t.Helper()
	out, err := service.Profile(context.Background(), in)
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	if err := out.Validate(); err != nil {
		t.Fatalf("profile violates the contract: %v", err)
	}
	// Every profile must also satisfy the published schema, not merely the Go
	// validation: the two are twins and a divergence must fail here rather than
	// at an integration boundary.
	set, err := schema.Default()
	if err != nil {
		t.Fatalf("schemas: %v", err)
	}
	if err := set.ValidateRecord(out.RecordKind(), &out); err != nil {
		t.Fatalf("profile does not satisfy machine-capability-profile.schema.json: %v", err)
	}
	return out
}

func endpointByID(p protocol.MachineCapabilityProfile, id string) (protocol.CognitionEndpoint, bool) {
	for _, endpoint := range p.Endpoints {
		if endpoint.ID == id {
			return endpoint, true
		}
	}
	return protocol.CognitionEndpoint{}, false
}

// TestBlankMachineProducesAValidProfileWithNoEndpoints is the degradation floor:
// nothing installed, nothing measurable, and still a valid answer.
func TestBlankMachineProducesAValidProfileWithNoEndpoints(t *testing.T) {
	service := newService(t)
	out := profile(t, service, cognition.ProfileInput{
		Facts: facts(t, environment.BlankLinux(), protocol.DepthHealth),
		Depth: protocol.DepthHealth,
	})
	if len(out.Endpoints) != 0 {
		t.Errorf("endpoints were invented on a blank machine: %+v", out.Endpoints)
	}
	if out.Assessment != protocol.AssessmentPartiallyReady {
		t.Errorf("assessment = %q, want partially_ready (git is missing)", out.Assessment)
	}
	if !containsSubstring(out.Limitations, "git is not installed") {
		t.Errorf("limitations do not name the missing prerequisite: %v", out.Limitations)
	}
	if !containsSubstring(out.Limitations, "no cognition endpoint is usable") {
		t.Errorf("limitations do not name the absent cognition: %v", out.Limitations)
	}
	if out.MachineFingerprint == "" {
		t.Error("a blank machine produced no fingerprint")
	}
}

// TestNoLocalModelButRemoteAvailableIsReady covers the zero-local-model profile.
func TestNoLocalModelButRemoteAvailableIsReady(t *testing.T) {
	remote := &cognition.FakeAdapter{
		AdapterID: "cli",
		Endpoints: []protocol.CognitionEndpoint{cognition.CLIEndpoint("cli:codex-cli", "openai")},
		Results: map[string]cognition.ProbeResult{
			"cli:codex-cli": {Status: protocol.FindingObserved, Detail: "answered"},
		},
	}
	service := newService(t, remote)
	out := profile(t, service, cognition.ProfileInput{
		Facts:            facts(t, environment.LinuxCPUOnly(), protocol.DepthInference),
		Depth:            protocol.DepthInference,
		InferenceTargets: []string{"cli:codex-cli"},
		Declarations: []cognition.Declaration{{
			EndpointID: "cli:codex-cli",
			Capabilities: []protocol.GradedCapability{{
				Dimension: protocol.CapabilityImplementation, Grade: protocol.GradeStrong,
			}},
		}},
	})
	endpoint, found := endpointByID(out, "cli:codex-cli")
	if !found {
		t.Fatal("the remote endpoint is missing")
	}
	if endpoint.Health != protocol.EndpointHealthReady {
		t.Errorf("health = %q, want ready", endpoint.Health)
	}
	if endpoint.Acceleration != nil {
		t.Error("local acceleration evidence was attached to a remote endpoint")
	}
	// A declared grade must be recorded as configured, never as measured.
	capability := endpoint.Capability(protocol.CapabilityImplementation)
	if capability.Grade != protocol.GradeStrong {
		t.Errorf("grade = %q, want strong", capability.Grade)
	}
	if capability.Provenance != protocol.ProvenanceConfigured {
		t.Errorf("provenance = %q, want configured; a declaration is not a measurement", capability.Provenance)
	}
	if out.Assessment != protocol.AssessmentReady {
		t.Errorf("assessment = %q, want ready", out.Assessment)
	}
}

// TestOneBrokenAdapterDoesNotAffectAnother is DCI-104 at the adapter boundary.
func TestOneBrokenAdapterDoesNotAffectAnother(t *testing.T) {
	broken := &cognition.FakeAdapter{
		AdapterID:   "mlx",
		DiscoverErr: errs.New(errs.CategoryProbeFailed, "the interpreter exploded"),
	}
	working := &cognition.FakeAdapter{
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
	service := newService(t, broken, working)
	out := profile(t, service, cognition.ProfileInput{
		Facts:            facts(t, environment.LinuxAMDIntegrated(), protocol.DepthInference),
		Depth:            protocol.DepthInference,
		InferenceTargets: []string{"ollama:small"},
	})
	if _, found := endpointByID(out, "ollama:small"); !found {
		t.Fatal("a broken adapter cost an unrelated adapter its endpoint")
	}
	var recorded bool
	for _, finding := range out.Environment.Findings {
		if finding.Component == "cognition.adapter.mlx" {
			recorded = true
			if finding.Status == protocol.FindingObserved {
				t.Error("a failed adapter was recorded as observed")
			}
		}
	}
	if !recorded {
		t.Error("the failing adapter's failure was not recorded as a finding")
	}
}

// TestVerifiedAccelerationFlowsFromProbeToCandidateAndProjection is the
// end-to-end acceleration path.
func TestVerifiedAccelerationFlowsFromProbeToCandidateAndProjection(t *testing.T) {
	adapter := &cognition.FakeAdapter{
		AdapterID: "ollama",
		Endpoints: []protocol.CognitionEndpoint{cognition.LocalEndpoint("ollama:small", "ollama", "small")},
		Results: map[string]cognition.ProbeResult{
			"ollama:small": {
				Status:  protocol.FindingObserved,
				Backend: protocol.BackendVulkan,
				Signals: []protocol.AccelerationSignal{authoritative(protocol.BackendVulkan, true)},
				Measurements: []protocol.Measurement{{
					Name: "generation_tokens_per_second", Value: 42.5, Unit: "tokens/s", Source: "runtime",
				}},
				StructuredOutput: protocol.FeatureProbePassed,
			},
		},
	}
	service := newService(t, adapter)
	out := profile(t, service, cognition.ProfileInput{
		Facts:            facts(t, environment.LinuxAMDIntegrated(), protocol.DepthInference),
		Depth:            protocol.DepthInference,
		InferenceTargets: []string{"ollama:small"},
		Probe:            cognition.ProbeRequest{RequireStructuredOutput: true},
	})

	endpoint, _ := endpointByID(out, "ollama:small")
	if !endpoint.AccelerationVerified() {
		t.Fatalf("acceleration was not verified: %+v", endpoint.Acceleration)
	}
	if endpoint.VerifiedAt == nil {
		t.Error("a ready endpoint recorded no verification instant")
	}
	if endpoint.StructuredOutput != protocol.FeatureProbePassed {
		t.Errorf("structured output = %q", endpoint.StructuredOutput)
	}
	// The candidate list must agree with the endpoint that proved it.
	var vulkan protocol.AcceleratorCandidate
	for _, candidate := range out.AcceleratorCandidates {
		if candidate.Backend == protocol.BackendVulkan {
			vulkan = candidate
		}
	}
	if vulkan.State != protocol.StateVerified {
		t.Errorf("candidate state = %q, want verified", vulkan.State)
	}
	if !containsSubstring(vulkan.Reasons, "verified by an inference probe on endpoint ollama:small") {
		t.Errorf("the candidate does not cite the endpoint that verified it: %v", vulkan.Reasons)
	}

	// The compact projection carries the claim without the evidence.
	projection := cognition.Project(out, "artifact:profile:sha256:abc")
	if len(projection.Endpoints) != 1 {
		t.Fatalf("projection endpoints = %+v", projection.Endpoints)
	}
	summary := projection.Endpoints[0]
	if !summary.AccelerationVerified {
		t.Error("the projection lost the verified acceleration")
	}
	if summary.AccelerationBackend == nil || *summary.AccelerationBackend != protocol.BackendVulkan {
		t.Error("the projection lost the backend identity")
	}
	if projection.MachineFingerprint == nil || *projection.MachineFingerprint != out.MachineFingerprint {
		t.Error("the projection cannot be checked for staleness against the machine")
	}
	state := protocol.ProjectState{
		SchemaVersion: protocol.SchemaVersion1, ProjectID: "p", StateRevision: "r1",
		Milestone:    protocol.MilestoneState{ID: "M3A", Title: "t"},
		Validation:   protocol.ValidationState{Status: protocol.ValidationUnknown},
		Capabilities: &protocol.Capabilities{Cognition: &projection},
	}
	if err := state.Validate(); err != nil {
		t.Fatalf("the projection is not a valid ProjectState capability block: %v", err)
	}
	set, err := schema.Default()
	if err != nil {
		t.Fatalf("schemas: %v", err)
	}
	if err := set.ValidateRecord(state.RecordKind(), &state); err != nil {
		t.Fatalf("the projected ProjectState does not satisfy its schema: %v", err)
	}
}

// TestAnUnnamedOffloadIsNamedOnlyWhenUnambiguous covers the runtime that proves
// offload without saying through what — Ollama's VRAM residency.
func TestAnUnnamedOffloadIsNamedOnlyWhenUnambiguous(t *testing.T) {
	// The machine's assessment has exactly one supported accelerated backend
	// (Vulkan; ROCm is uncertain for gfx90c), so the offload can be named.
	unnamed := protocol.AccelerationSignal{
		Source: "ollama:/api/ps", Trust: protocol.TrustAuthoritative,
		Backend: protocol.BackendUnknown, Offloaded: true,
		Statement: "resident model reports size_vram=3220000000 of size=3220000000",
	}
	adapter := &cognition.FakeAdapter{
		AdapterID: "ollama",
		Endpoints: []protocol.CognitionEndpoint{cognition.LocalEndpoint("ollama:small", "ollama", "small")},
		Results: map[string]cognition.ProbeResult{
			"ollama:small": {
				Status:  protocol.FindingObserved,
				Backend: protocol.BackendUnknown,
				Signals: []protocol.AccelerationSignal{unnamed},
			},
		},
	}
	service := newService(t, adapter)
	out := profile(t, service, cognition.ProfileInput{
		Facts:            facts(t, environment.LinuxAMDIntegrated(), protocol.DepthInference),
		Depth:            protocol.DepthInference,
		InferenceTargets: []string{"ollama:small"},
	})
	endpoint, _ := endpointByID(out, "ollama:small")
	if endpoint.Acceleration.Backend != protocol.BackendVulkan {
		t.Fatalf("backend = %q, want vulkan (the only supported accelerated candidate)",
			endpoint.Acceleration.Backend)
	}
	if !endpoint.AccelerationVerified() {
		t.Fatalf("the offload was not verified: %+v", endpoint.Acceleration)
	}
	// The inference must be visible in the evidence, not silent.
	if !strings.Contains(endpoint.Acceleration.Signals[0].Statement, "did not name the backend") {
		t.Errorf("the naming inference was not recorded: %q", endpoint.Acceleration.Signals[0].Statement)
	}
	// The candidate list must now agree with the endpoint that proved it.
	for _, candidate := range out.AcceleratorCandidates {
		if candidate.Backend == protocol.BackendVulkan && candidate.State != protocol.StateVerified {
			t.Errorf("the vulkan candidate was not lifted to verified: %+v", candidate)
		}
	}

	// With two supported accelerated backends the name stays unknown rather than
	// being guessed between them. The discrete-AMD fixture has both: a
	// ROCm-supported architecture with a writable /dev/kfd, and a writable render
	// node with a Vulkan loader installed.
	out = profile(t, service, cognition.ProfileInput{
		Facts:            facts(t, environment.LinuxAMDDiscreteROCm(), protocol.DepthInference),
		Depth:            protocol.DepthInference,
		InferenceTargets: []string{"ollama:small"},
	})
	endpoint, _ = endpointByID(out, "ollama:small")
	if endpoint.Acceleration.Backend != protocol.BackendUnknown {
		t.Errorf("backend = %q, want unknown with two supported accelerated candidates",
			endpoint.Acceleration.Backend)
	}
	// Offload was still observed, so it is still verified — just not attributed.
	if !endpoint.AccelerationVerified() {
		t.Error("an observed offload stopped being verified merely because it could not be named")
	}
}

// TestCPUFallbackProfileReportsFailedNotVerified is the "runtime installed, GPU
// present, work ran on the CPU" case.
func TestCPUFallbackProfileReportsFailedNotVerified(t *testing.T) {
	adapter := &cognition.FakeAdapter{
		AdapterID: "ollama",
		Endpoints: []protocol.CognitionEndpoint{cognition.LocalEndpoint("ollama:small", "ollama", "small")},
		Results: map[string]cognition.ProbeResult{
			"ollama:small": {
				Status:  protocol.FindingObserved,
				Backend: protocol.BackendCUDA,
				Signals: []protocol.AccelerationSignal{authoritative(protocol.BackendCPU, false)},
			},
		},
	}
	service := newService(t, adapter)
	out := profile(t, service, cognition.ProfileInput{
		Facts:            facts(t, environment.LinuxNVIDIAReady(), protocol.DepthInference),
		Depth:            protocol.DepthInference,
		InferenceTargets: []string{"ollama:small"},
	})
	endpoint, _ := endpointByID(out, "ollama:small")
	if endpoint.Acceleration.State != protocol.StateFailed {
		t.Errorf("state = %q, want failed", endpoint.Acceleration.State)
	}
	if endpoint.AccelerationVerified() {
		t.Error("a cpu-fallback endpoint reported verified acceleration")
	}
	if endpoint.Health != protocol.EndpointHealthReady {
		t.Errorf("health = %q; inference worked, so the endpoint is usable even unaccelerated", endpoint.Health)
	}
	if !containsSubstring(out.Limitations, "no local endpoint has verified non-cpu acceleration") {
		t.Errorf("the limitation was not reported: %v", out.Limitations)
	}
}

// TestShallowDepthNeverProbes is the progressive-depth and performance
// guarantee: an inventory query must not load a model.
func TestShallowDepthNeverProbes(t *testing.T) {
	adapter := &cognition.FakeAdapter{
		AdapterID: "ollama",
		Endpoints: []protocol.CognitionEndpoint{cognition.LocalEndpoint("ollama:small", "ollama", "small")},
		Results: map[string]cognition.ProbeResult{
			"ollama:small": {Status: protocol.FindingObserved,
				Signals: []protocol.AccelerationSignal{authoritative(protocol.BackendVulkan, true)}},
		},
	}
	service := newService(t, adapter)
	for _, depth := range []protocol.ProbeDepth{protocol.DepthInventory, protocol.DepthHealth} {
		out := profile(t, service, cognition.ProfileInput{
			Facts: facts(t, environment.LinuxAMDIntegrated(), depth), Depth: depth,
		})
		if len(adapter.Prompts) != 0 {
			t.Fatalf("depth %s sent a prompt: %v", depth, adapter.Prompts)
		}
		endpoint, _ := endpointByID(out, "ollama:small")
		if endpoint.AccelerationVerified() {
			t.Errorf("depth %s produced verified acceleration without inference", depth)
		}
	}
}

// TestProbeEvidenceGoesToTheArtifactStore keeps large raw output out of the
// compact record while staying retrievable.
func TestProbeEvidenceGoesToTheArtifactStore(t *testing.T) {
	store, err := artifacts.NewStore(t.TempDir(), ids.NewULIDSource())
	if err != nil {
		t.Fatalf("artifact store: %v", err)
	}
	raw := strings.Repeat(`{"response":"ok"}`, 500)
	adapter := &cognition.FakeAdapter{
		AdapterID: "ollama",
		Endpoints: []protocol.CognitionEndpoint{cognition.LocalEndpoint("ollama:small", "ollama", "small")},
		Results: map[string]cognition.ProbeResult{
			"ollama:small": {
				Status:      protocol.FindingObserved,
				Signals:     []protocol.AccelerationSignal{authoritative(protocol.BackendVulkan, true)},
				RawEvidence: []byte(raw),
			},
		},
	}
	service, err := cognition.NewService(cognition.Options{
		Adapters: []cognition.Adapter{adapter}, Clock: testClock(), IDs: &deterministicIDs{},
		Artifacts: store, ArtifactProject: "demo",
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	out := profile(t, service, cognition.ProfileInput{
		Facts:            facts(t, environment.LinuxAMDIntegrated(), protocol.DepthInference),
		Depth:            protocol.DepthInference,
		InferenceTargets: []string{"ollama:small"},
	})
	endpoint, _ := endpointByID(out, "ollama:small")
	if len(endpoint.ProbeRefs) != 1 {
		t.Fatalf("probe refs = %v", endpoint.ProbeRefs)
	}
	if endpoint.Acceleration == nil || len(endpoint.Acceleration.ProbeRefs) != 1 {
		t.Error("the acceleration claim does not reference its evidence")
	}
	// The raw text must not have been inlined into the record.
	encoded, err := protocol.Marshal(&out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), raw) {
		t.Error("raw probe output was inlined into the compact record")
	}
	if len(encoded) > 64<<10 {
		t.Errorf("the profile is %d bytes; raw evidence is leaking into it", len(encoded))
	}
}

// TestWithoutAnArtifactStoreTheOmissionIsRecorded keeps a missing reference from
// later reading as "there was no evidence".
func TestWithoutAnArtifactStoreTheOmissionIsRecorded(t *testing.T) {
	adapter := &cognition.FakeAdapter{
		AdapterID: "ollama",
		Endpoints: []protocol.CognitionEndpoint{cognition.LocalEndpoint("ollama:small", "ollama", "small")},
		Results: map[string]cognition.ProbeResult{
			"ollama:small": {Status: protocol.FindingObserved, RawEvidence: []byte("{}")},
		},
	}
	service := newService(t, adapter)
	out := profile(t, service, cognition.ProfileInput{
		Facts:            facts(t, environment.LinuxAMDIntegrated(), protocol.DepthInference),
		Depth:            protocol.DepthInference,
		InferenceTargets: []string{"ollama:small"},
	})
	endpoint, _ := endpointByID(out, "ollama:small")
	var recorded bool
	for _, finding := range endpoint.Findings {
		if strings.HasSuffix(finding.Component, ".probe_evidence") {
			recorded = true
		}
	}
	if !recorded {
		t.Error("dropping probe evidence was not recorded")
	}
}

// TestSecretShapedDeclarationsAreRefused keeps a credential out of a durable
// record, at configuration time rather than after the fact.
func TestSecretShapedDeclarationsAreRefused(t *testing.T) {
	service := newService(t, &cognition.FakeAdapter{AdapterID: "cli"})
	for name, declaration := range map[string]cognition.Declaration{
		"api key as credential ref": {EndpointID: "cli:x", CredentialRef: "sk-abcdefghijklmnop"},
		"long opaque token":         {EndpointID: "cli:x", CredentialRef: strings.Repeat("z", 200)},
		"token in account ref":      {EndpointID: "cli:x", AccountRef: "token=abc123"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := service.Profile(context.Background(), cognition.ProfileInput{
				Facts:        facts(t, environment.LinuxCPUOnly(), protocol.DepthHealth),
				Depth:        protocol.DepthHealth,
				Declarations: []cognition.Declaration{declaration},
			})
			if err == nil {
				t.Fatal("a secret-shaped reference was accepted")
			}
			if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
				t.Errorf("category = %q, want invalid_argument", errs.CategoryOf(err))
			}
		})
	}
}

// TestNoSecretAppearsInAProfile is the credential-leak check over the whole
// serialised record.
func TestNoSecretAppearsInAProfile(t *testing.T) {
	adapter := &cognition.FakeAdapter{
		AdapterID: "cli",
		Endpoints: []protocol.CognitionEndpoint{cognition.CLIEndpoint("cli:codex-cli", "openai")},
		Results: map[string]cognition.ProbeResult{
			"cli:codex-cli": {Status: protocol.FindingObserved, Detail: "answered"},
		},
	}
	service := newService(t, adapter)
	out := profile(t, service, cognition.ProfileInput{
		Facts:            facts(t, environment.LinuxCPUOnly(), protocol.DepthInference),
		Depth:            protocol.DepthInference,
		InferenceTargets: []string{"cli:codex-cli"},
		Declarations: []cognition.Declaration{{
			EndpointID: "cli:codex-cli", CredentialRef: "cred_openai_default", AccountRef: "acct_ref_7f3a",
		}},
	})
	encoded, err := protocol.Marshal(&out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	lowered := strings.ToLower(string(encoded))
	for _, forbidden := range []string{"sk-", "bearer ", "password", "api_key", "apikey", "secret"} {
		if strings.Contains(lowered, forbidden) {
			t.Errorf("the profile contains %q", forbidden)
		}
	}
	// The opaque references themselves are fine, and are what the record should
	// carry instead.
	if !strings.Contains(string(encoded), "cred_openai_default") {
		t.Error("the opaque credential reference was dropped")
	}
}

// TestTheProbePromptCarriesNoRepositoryContent is the source-exposure guarantee
// for health checks.
func TestTheProbePromptCarriesNoRepositoryContent(t *testing.T) {
	adapter := &cognition.FakeAdapter{
		AdapterID: "cli",
		Endpoints: []protocol.CognitionEndpoint{cognition.CLIEndpoint("cli:codex-cli", "openai")},
		Results: map[string]cognition.ProbeResult{
			"cli:codex-cli": {Status: protocol.FindingObserved},
		},
	}
	service := newService(t, adapter)
	profile(t, service, cognition.ProfileInput{
		Facts:            facts(t, environment.LinuxCPUOnly(), protocol.DepthInference),
		Depth:            protocol.DepthInference,
		InferenceTargets: []string{"cli:codex-cli"},
	})
	if len(adapter.Prompts) == 0 {
		t.Fatal("no prompt was sent, so nothing was verified")
	}
	for _, prompt := range adapter.Prompts {
		if prompt != cognition.SyntheticProbePrompt {
			t.Errorf("a probe sent something other than the synthetic prompt: %q", prompt)
		}
		lowered := strings.ToLower(prompt)
		// The probe must not mention this repository, its packages, its paths
		// or any project vocabulary.
		for _, forbidden := range []string{
			"devcadience", "projectstate", "internal/", ".go", "package ", "func ",
			"repository", "worktree", "commit",
		} {
			if strings.Contains(lowered, forbidden) {
				t.Errorf("the synthetic prompt contains project vocabulary %q: %q", forbidden, prompt)
			}
		}
	}
}

// TestProfileAssemblyIsDeterministic protects the fingerprint and digest.
func TestProfileAssemblyIsDeterministic(t *testing.T) {
	build := func() protocol.MachineCapabilityProfile {
		adapter := &cognition.FakeAdapter{
			AdapterID: "ollama",
			Endpoints: []protocol.CognitionEndpoint{
				cognition.LocalEndpoint("ollama:zeta", "ollama", "zeta"),
				cognition.LocalEndpoint("ollama:alpha", "ollama", "alpha"),
			},
			Results: map[string]cognition.ProbeResult{
				"ollama:alpha": {Status: protocol.FindingObserved,
					Signals: []protocol.AccelerationSignal{authoritative(protocol.BackendVulkan, true)}},
				"ollama:zeta": {Status: protocol.FindingObserved},
			},
		}
		service := newService(t, adapter)
		return profile(t, service, cognition.ProfileInput{
			Facts:            facts(t, environment.LinuxAMDIntegrated(), protocol.DepthInference),
			Depth:            protocol.DepthInference,
			InferenceTargets: []string{"ollama:alpha", "ollama:zeta"},
		})
	}
	first, second := build(), build()
	// Profile ids and observation instants come from injected sources, so the
	// comparison is over everything else.
	first.ProfileID, second.ProfileID = "", ""
	firstDigest, err := protocol.Digest(first)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	secondDigest, err := protocol.Digest(second)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if firstDigest != secondDigest {
		t.Error("two identical passes produced different profiles")
	}
	if len(first.Endpoints) != 2 || first.Endpoints[0].ID != "ollama:alpha" {
		t.Errorf("endpoints are not in stable order: %+v", first.Endpoints)
	}
}

// TestCancellationDuringProbingIsRecorded checks context propagation.
func TestCancellationDuringProbingIsRecorded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	adapter := &cognition.FakeAdapter{
		AdapterID: "ollama",
		Endpoints: []protocol.CognitionEndpoint{cognition.LocalEndpoint("ollama:small", "ollama", "small")},
		Results: map[string]cognition.ProbeResult{
			"ollama:small": {Status: protocol.FindingObserved,
				Signals: []protocol.AccelerationSignal{authoritative(protocol.BackendVulkan, true)}},
		},
	}
	service := newService(t, adapter)
	out, err := service.Profile(ctx, cognition.ProfileInput{
		Facts:            facts(t, environment.LinuxAMDIntegrated(), protocol.DepthInference),
		Depth:            protocol.DepthInference,
		InferenceTargets: []string{"ollama:small"},
	})
	if err != nil {
		t.Fatalf("cancellation must not fail profile assembly: %v", err)
	}
	endpoint, _ := endpointByID(out, "ollama:small")
	if endpoint.AccelerationVerified() {
		t.Error("a cancelled probe produced verified acceleration")
	}
	if endpoint.Health != protocol.EndpointHealthUnhealthy {
		t.Errorf("health after a cancelled probe = %q, want unhealthy", endpoint.Health)
	}
}

// TestAProfileCannotClaimVerificationAtAShallowDepth is the contract-level guard
// against a depth/claim mismatch.
func TestAProfileCannotClaimVerificationAtAShallowDepth(t *testing.T) {
	verified := cognition.WithVerifiedAcceleration(
		cognition.Ready(cognition.LocalEndpoint("ollama:small", "ollama", "small")),
		protocol.BackendVulkan, at())
	out := protocol.MachineCapabilityProfile{
		SchemaVersion: protocol.SchemaVersion1, ProfileID: "mcp_1",
		MachineFingerprint: "sha256:x", KnowledgeRevision: "r",
		ProbeDepth: protocol.DepthHealth,
		Environment: protocol.EnvironmentFacts{
			Host:           protocol.HostFacts{Family: protocol.OSLinux, Arch: "amd64"},
			Virtualization: protocol.VirtualizationFacts{Container: protocol.ContainerNone},
		},
		Endpoints:  []protocol.CognitionEndpoint{verified},
		Assessment: protocol.AssessmentReady,
	}
	err := out.Validate()
	if err == nil {
		t.Fatal("a health-depth profile was allowed to claim verified acceleration")
	}
	if errs.CategoryOf(err) != errs.CategoryIntegrity {
		t.Errorf("category = %q, want integrity", errs.CategoryOf(err))
	}
}

// TestMeasuredCapabilityIsNotOverwrittenByADeclaration keeps an optimistic
// operator claim from erasing a probe that disagreed.
func TestMeasuredCapabilityIsNotOverwrittenByADeclaration(t *testing.T) {
	measured := cognition.WithCapability(
		cognition.LocalEndpoint("ollama:small", "ollama", "small"),
		protocol.CapabilityImplementation, protocol.GradeLow, protocol.ProvenanceEvaluated)
	adapter := &cognition.FakeAdapter{
		AdapterID: "ollama",
		Endpoints: []protocol.CognitionEndpoint{measured},
		Results: map[string]cognition.ProbeResult{
			"ollama:small": {Status: protocol.FindingObserved},
		},
	}
	service := newService(t, adapter)
	out := profile(t, service, cognition.ProfileInput{
		Facts: facts(t, environment.LinuxAMDIntegrated(), protocol.DepthHealth),
		Depth: protocol.DepthHealth,
		Declarations: []cognition.Declaration{{
			EndpointID: "ollama:small",
			Capabilities: []protocol.GradedCapability{{
				Dimension: protocol.CapabilityImplementation, Grade: protocol.GradeStrong,
			}},
		}},
	})
	endpoint, _ := endpointByID(out, "ollama:small")
	capability := endpoint.Capability(protocol.CapabilityImplementation)
	if capability.Grade != protocol.GradeLow || capability.Provenance != protocol.ProvenanceEvaluated {
		t.Errorf("an evaluated grade was overwritten by a declaration: %+v", capability)
	}
}

func containsSubstring(values []string, needle string) bool {
	for _, value := range values {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
