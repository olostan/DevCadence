// Package cognition discovers, verifies and routes sources of model cognition.
//
// The package owns three things and deliberately not a fourth:
//
//   - the adapter boundary (this file), through which a runtime, a coding CLI
//     or a remote API is discovered and probed;
//   - the pure evidence and decision logic — acceleration evaluation, capability
//     routing, the ProjectState projection — which are functions of their inputs
//     and consult no clock, host or network;
//   - assembly of a MachineCapabilityProfile from the two.
//
// What it does not own is installation, authentication, credential creation or
// remediation. Those are M3B, and nothing here mutates the machine: the deepest
// action this package can take is to run an already-present model on a
// synthetic prompt when a caller explicitly asks for it.
//
// No provider SDK appears here or in any package that imports it. Adapters live
// in subpackages and speak only in protocol types, so replacing Ollama with
// something else is an adapter change (DCI-055).
package cognition

import (
	"context"
	"time"

	"github.com/olostan/DevCadience/internal/protocol"
)

// SyntheticProbePrompt is the only prompt this package ever sends.
//
// It contains no repository content, no file path, no identifier and nothing
// derived from the project — a capability probe must work identically on a
// machine with no repository registered, and source must never leave the
// machine to answer a health question
// (docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md §12, docs/SECURITY.md §1).
// A test asserts that property against the repository's own vocabulary.
const SyntheticProbePrompt = "Reply with a JSON object containing the key ok set to true. Reply with JSON only."

// SyntheticProbeSchema is the structure a structured-output probe asks for.
//
// It is trivial on purpose. Passing it establishes exactly one thing —
// structured output was produced once, for one tiny schema — and that is
// recorded as FeatureProbePassed. It is not evidence that the endpoint reliably
// emits protocol documents under load, which needs evaluation history.
const SyntheticProbeSchema = `{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"]}`

// DefaultProbeTokens bounds generation during a probe.
//
// Small enough to be cheap on any endpoint, large enough to exercise the
// generation path rather than only the prompt path.
const DefaultProbeTokens = 32

// DefaultProbeTimeout bounds one inference probe.
//
// Loading a model from cold can take a while on a thin machine, so this is
// considerably more generous than a version probe — but still finite.
const DefaultProbeTimeout = 90 * time.Second

// DiscoveryInput is what an adapter is given to discover its endpoints.
//
// Facts are the observed environment: an adapter reads the software inventory
// from them rather than looking for its own binary a second time. Candidates
// are the assessed backends, so a local runtime adapter can say which backend
// it would expect to use without re-deriving the compatibility judgement.
type DiscoveryInput struct {
	Facts      protocol.EnvironmentFacts
	Candidates []protocol.AcceleratorCandidate
	// Depth bounds what the adapter may do. At DepthInventory an adapter must
	// not contact a service; at DepthHealth it may query an already-running
	// one; only at DepthInference may it run a model.
	Depth protocol.ProbeDepth
	// ObservedAt is the injected observation instant, so that two endpoints
	// discovered in one pass agree on when they were seen.
	ObservedAt protocol.Timestamp
}

// ProbeRequest asks an adapter to exercise one endpoint.
type ProbeRequest struct {
	// Prompt defaults to SyntheticProbePrompt. A caller may not supply
	// repository content; the field exists so an adapter test can vary the
	// prompt, not so a workload can be smuggled through a health check.
	Prompt string
	// MaxTokens defaults to DefaultProbeTokens.
	MaxTokens int
	// RequireStructuredOutput asks the endpoint to emit JSON matching
	// SyntheticProbeSchema.
	RequireStructuredOutput bool
	Timeout                 time.Duration
}

// Normalise fills defaults.
func (r ProbeRequest) Normalise() ProbeRequest {
	if r.Prompt == "" {
		r.Prompt = SyntheticProbePrompt
	}
	if r.MaxTokens <= 0 {
		r.MaxTokens = DefaultProbeTokens
	}
	if r.Timeout <= 0 {
		r.Timeout = DefaultProbeTimeout
	}
	return r
}

// ProbeResult is what one probe established.
//
// Backend and Signals are kept separate from Status on purpose: a probe can
// succeed (inference worked) while its signals say the work ran on the CPU.
// Collapsing the two would be the exact confusion DCI-106 forbids.
type ProbeResult struct {
	// Status is whether the probe itself ran and answered.
	Status protocol.FindingStatus
	// Backend is the backend the adapter believes was used, if it can tell.
	Backend protocol.BackendKind
	// Signals are the individual observations bearing on acceleration. An
	// adapter that cannot observe the backend returns none, which yields an
	// unverified result rather than an optimistic one.
	Signals []protocol.AccelerationSignal
	// Measurements are operational observations. An adapter reports a
	// throughput measurement only when the runtime gave it real token counts
	// and durations; it never estimates one from output length.
	Measurements []protocol.Measurement
	// StructuredOutput is what the probe established about JSON generation.
	StructuredOutput protocol.FeatureSupport
	// Auth, when set, revises the endpoint's authentication state.
	//
	// An adapter may only *downgrade* through this field: probe output saying
	// "you are not signed in" is a usable observation, while output claiming a
	// valid session is provider text and not authority (DCI-083). The service
	// enforces that.
	Auth           protocol.AuthStatus
	RuntimeVersion string
	ModelID        string
	DeviceID       string
	// Detail is sanitised, bounded explanation.
	Detail string
	// RawEvidence is the bounded raw exchange for artifact storage. It never
	// contains a credential, because the probe never sends one in-band.
	RawEvidence []byte
	// EvidenceMediaType describes RawEvidence.
	EvidenceMediaType string
}

// Adapter discovers and probes one family of cognition endpoints.
//
// The contract is narrow because everything provider-specific must stay behind
// it: an adapter translates one runtime's or CLI's reality into protocol types
// and nothing else. It holds no policy, makes no routing decision and grades no
// capability beyond what it measured.
type Adapter interface {
	// ID is the adapter's stable identity, used in findings and endpoint ids.
	ID() string
	// Discover returns the endpoints this adapter can see.
	//
	// An adapter that finds nothing returns no endpoints and no error: an
	// absent runtime is a normal state. It returns an error only when it
	// could not carry out discovery at all, and even then the caller records
	// that as a finding against this adapter rather than failing the profile
	// (DCI-104).
	Discover(ctx context.Context, in DiscoveryInput) ([]protocol.CognitionEndpoint, error)
	// Probe exercises one endpoint the adapter previously discovered.
	//
	// It must be side-effect free beyond running inference: no model
	// download, no configuration write, no authentication.
	Probe(ctx context.Context, endpoint protocol.CognitionEndpoint, req ProbeRequest) (ProbeResult, error)
}
