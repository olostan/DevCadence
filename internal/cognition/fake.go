package cognition

import (
	"context"
	"time"

	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/protocol"
)

// FakeAdapter is a scripted Adapter.
//
// It exists so that endpoint discovery, acceleration evaluation, profile
// assembly and routing can all be exercised with no runtime installed, no GPU
// and no network — the offline guarantee of ENGINEERING_STANDARDS.md §19. The
// real Ollama and MLX adapters are tested the same way, against transports and
// command probes rather than against software.
type FakeAdapter struct {
	AdapterID string
	// Endpoints is what Discover returns.
	Endpoints []protocol.CognitionEndpoint
	// DiscoverErr makes Discover fail, for testing that one broken adapter
	// leaves its neighbours intact.
	DiscoverErr error
	// Results maps endpoint id to the probe result it produces.
	Results map[string]ProbeResult
	// ProbeErrs maps endpoint id to a probe failure.
	ProbeErrs map[string]error
	// Prompts records every prompt sent, so a test can assert that no
	// repository content was transmitted.
	Prompts []string
	// Probed records the id of every endpoint Probe was called for, in order.
	//
	// It is the evidence for the fan-out guarantee: a test asserts not merely
	// that the right endpoint was verified but that no other endpoint was
	// invoked at all, which is the difference between reporting one result and
	// spending five quotas to report one result.
	Probed []string
	// DiscoveredAtDepth records the depth of every Discover call.
	DiscoveredAtDepth []protocol.ProbeDepth
}

// ID implements Adapter.
func (f *FakeAdapter) ID() string {
	if f.AdapterID == "" {
		return "fake"
	}
	return f.AdapterID
}

// Discover implements Adapter.
func (f *FakeAdapter) Discover(_ context.Context, in DiscoveryInput) ([]protocol.CognitionEndpoint, error) {
	f.DiscoveredAtDepth = append(f.DiscoveredAtDepth, in.Depth)
	if f.DiscoverErr != nil {
		return nil, f.DiscoverErr
	}
	out := make([]protocol.CognitionEndpoint, 0, len(f.Endpoints))
	for _, endpoint := range f.Endpoints {
		endpoint.ObservedAt = in.ObservedAt
		out = append(out, endpoint)
	}
	return out, nil
}

// Probe implements Adapter.
func (f *FakeAdapter) Probe(ctx context.Context, endpoint protocol.CognitionEndpoint, req ProbeRequest) (ProbeResult, error) {
	req = req.Normalise()
	f.Prompts = append(f.Prompts, req.Prompt)
	f.Probed = append(f.Probed, endpoint.ID)
	if err := ctx.Err(); err != nil {
		return ProbeResult{}, errs.Wrap(errs.CategoryProbeTimeout, err, "probe cancelled")
	}
	if err, ok := f.ProbeErrs[endpoint.ID]; ok {
		return ProbeResult{}, err
	}
	result, ok := f.Results[endpoint.ID]
	if !ok {
		return ProbeResult{
			Status: protocol.FindingUnsupported,
			Detail: "the fake adapter has no scripted result for this endpoint",
		}, nil
	}
	return result, nil
}

// fixtureObservedAt is the observation instant the builders below stamp.
//
// They stamp one at all because an endpoint with no observed_at is not a valid
// record — it would serialise as year 1 — so a builder that left it unset would
// hand every test an invalid endpoint. Real discovery overwrites it from
// DiscoveryInput.ObservedAt, and a test that cares about the instant sets it
// explicitly.
var fixtureObservedAt = protocol.NewTimestamp(time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC))

// LocalEndpoint builds a local-runtime endpoint for tests.
//
// Health starts at unverified, which is where discovery of a live-but-unprobed
// runtime honestly leaves it, and auth is not_applicable because a local runtime
// has no account.
func LocalEndpoint(id, runtime, model string) protocol.CognitionEndpoint {
	return protocol.CognitionEndpoint{
		ObservedAt:             fixtureObservedAt,
		ID:                     id,
		Kind:                   protocol.EndpointLocalRuntime,
		Provider:               runtime,
		Runtime:                runtime,
		ModelID:                model,
		Locality:               protocol.LocalityLocal,
		Health:                 protocol.EndpointHealthUnverified,
		Auth:                   protocol.AuthNotApplicable,
		StructuredOutput:       protocol.FeatureUnknown,
		ToolUse:                protocol.FeatureUnknown,
		CostClass:              protocol.CostLocalCompute,
		RequiredSourceExposure: protocol.ExposureLocalOnly,
	}
}

// CLIEndpoint builds an authenticated-CLI endpoint.
//
// Auth is unknown by default because that is the honest default: no supported
// CLI publishes a safe, non-mutating way to ask, and DevCadience will not read
// credential files to find out. Cost class is unknown for the same reason — the
// same binary may be billed by subscription, by metered API key or by an
// enterprise account, and discovery cannot tell. Only an operator Declaration
// may name a class.
func CLIEndpoint(id, provider string) protocol.CognitionEndpoint {
	return protocol.CognitionEndpoint{
		ObservedAt:             fixtureObservedAt,
		ID:                     id,
		Kind:                   protocol.EndpointAuthenticatedCLI,
		Provider:               provider,
		Runtime:                provider,
		Locality:               protocol.LocalityRemoteInferenceLocalTools,
		Health:                 protocol.EndpointHealthInstalled,
		Auth:                   protocol.AuthUnknown,
		StructuredOutput:       protocol.FeatureUnknown,
		ToolUse:                protocol.FeatureDeclared,
		CostClass:              protocol.CostUnknown,
		RequiredSourceExposure: protocol.ExposureToolMediatedWorktree,
	}
}

// RemoteEndpoint builds a remote-API endpoint for tests.
func RemoteEndpoint(id, provider string, cost protocol.CostClass) protocol.CognitionEndpoint {
	return protocol.CognitionEndpoint{
		ObservedAt:             fixtureObservedAt,
		ID:                     id,
		Kind:                   protocol.EndpointRemoteAPI,
		Provider:               provider,
		Locality:               protocol.LocalityRemote,
		Health:                 protocol.EndpointHealthUnverified,
		Auth:                   protocol.AuthUnknown,
		StructuredOutput:       protocol.FeatureUnknown,
		ToolUse:                protocol.FeatureUnknown,
		CostClass:              cost,
		RequiredSourceExposure: protocol.ExposureFocusedSnippets,
	}
}

// Ready marks an endpoint probed-ready, as a successful probe would.
func Ready(endpoint protocol.CognitionEndpoint) protocol.CognitionEndpoint {
	endpoint.Health = protocol.EndpointHealthReady
	return endpoint
}

// WithCapability declares a graded capability with explicit provenance.
//
// Tests must state provenance, because a graded capability without it is exactly
// the unevidenced claim the contract refuses (DCI-012).
func WithCapability(
	endpoint protocol.CognitionEndpoint,
	dimension protocol.CapabilityDimension,
	grade protocol.CapabilityGrade,
	provenance protocol.CapabilityProvenance,
) protocol.CognitionEndpoint {
	setCapability(&endpoint, protocol.GradedCapability{
		Dimension: dimension, Grade: grade, Provenance: provenance, Source: "test",
	})
	return endpoint
}

// WithVerifiedAcceleration attaches verified acceleration evidence.
func WithVerifiedAcceleration(
	endpoint protocol.CognitionEndpoint,
	backend protocol.BackendKind,
	at protocol.Timestamp,
) protocol.CognitionEndpoint {
	evidence := EvaluateAcceleration(AccelerationInput{
		Backend: backend,
		Signals: []protocol.AccelerationSignal{{
			Source: "test:runtime", Trust: protocol.TrustAuthoritative,
			Backend: backend, Offloaded: true, Statement: "runtime reported offload",
		}},
		ProbeRan:       true,
		ProbeSucceeded: true,
		ObservedAt:     at,
	})
	endpoint.Acceleration = &evidence
	return endpoint
}
