// Package codingcli discovers installed coding and agent CLIs and represents
// them as cognition endpoints.
//
// The hard part of this adapter is everything it refuses to do. For each
// supported CLI it establishes only what is safely available:
//
//   - the executable is present, and where;
//   - the version it reports;
//   - whether a synthetic, repository-free prompt gets an answer.
//
// It does not read credential files, copy tokens, inspect unrelated provider
// configuration, launch an interactive login, write any configuration, or send
// repository source to establish health. It does not infer a commercial plan
// from an executable being present: "codex is installed and answered" says
// nothing about which subscription backs it, and there is no field to record a
// guess in.
//
// Authentication is therefore `unknown` by default, and that is the correct
// answer rather than a gap. No supported CLI publishes a non-mutating,
// non-secret-reading way to ask, so the only evidence available is that a probe
// answered — which demonstrates a usable session more directly than a status
// command would. The one inference this adapter draws from probe output is a
// *downgrade*: output that plainly says the user is not signed in sets
// `unauthenticated`. Nothing in this package can promote an endpoint to
// `authenticated`.
//
// The invocation table is versioned compatibility knowledge, not architecture
// (ADR-0011 §5). Coding CLIs change their flags; when one does, its probe fails
// and the endpoint stays `installed` with a finding explaining that the probe
// did not answer. A wrong entry therefore costs a capability, never a false
// claim of one.
package codingcli

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/olostan/DevCadience/internal/cognition"
	"github.com/olostan/DevCadience/internal/environment"
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/protocol"
)

// AdapterID is the stable adapter identity and the prefix of its endpoint ids.
const AdapterID = "cli"

// InvocationRevision versions the invocation table below.
const InvocationRevision = "coding-cli-invocations/2026-09-21"

// Descriptor describes one supported CLI.
//
// Every field is typed Go. Nothing here may be supplied by configuration, a
// repository file or a model response: these are argv values that will be
// executed, and a configuration-driven command table would be an arbitrary
// shell engine by another name.
type Descriptor struct {
	// SoftwareID matches the environment inventory entry.
	SoftwareID string
	// Executable is the binary name.
	Executable string
	// Provider is the organisation behind the inference.
	Provider string
	// PromptArgs is the non-interactive invocation, with PromptPlaceholder
	// marking where the synthetic prompt goes. Nil means this build knows no
	// safe non-interactive invocation, and health stops at "installed".
	PromptArgs []string
	// CostClass is the class this endpoint bills under. An authenticated CLI is
	// normally covered by a subscription the user already pays for, which is
	// why it is cheaper than a metered API without being free.
	CostClass protocol.CostClass
}

// PromptPlaceholder marks the prompt position in PromptArgs.
const PromptPlaceholder = "{{prompt}}"

// DefaultDescriptors are the CLIs this build knows about.
//
// None is mandatory and no unofficial wrapper appears here. The set matches the
// coding-CLI entries of the environment inventory.
func DefaultDescriptors() []Descriptor {
	return []Descriptor{
		{
			SoftwareID: "codex-cli", Executable: "codex", Provider: "openai",
			PromptArgs: []string{"exec", PromptPlaceholder},
			CostClass:  protocol.CostSubscriptionIncluded,
		},
		{
			SoftwareID: "claude-code", Executable: "claude", Provider: "anthropic",
			PromptArgs: []string{"-p", PromptPlaceholder},
			CostClass:  protocol.CostSubscriptionIncluded,
		},
		{
			SoftwareID: "gemini-cli", Executable: "gemini", Provider: "google",
			PromptArgs: []string{"-p", PromptPlaceholder},
			CostClass:  protocol.CostSubscriptionIncluded,
		},
	}
}

// Options configures an Adapter.
type Options struct {
	Commands    environment.CommandProbe
	Descriptors []Descriptor
	// ProbeTimeout bounds one health probe. A coding CLI starting a session is
	// slower than a version command.
	ProbeTimeout time.Duration
}

// Adapter is the coding-CLI adapter.
type Adapter struct {
	commands     environment.CommandProbe
	descriptors  []Descriptor
	probeTimeout time.Duration
}

// New returns an Adapter.
func New(opts Options) (*Adapter, error) {
	if opts.Commands == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "codingcli: a CommandProbe is required")
	}
	descriptors := opts.Descriptors
	if len(descriptors) == 0 {
		descriptors = DefaultDescriptors()
	}
	timeout := opts.ProbeTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Adapter{commands: opts.Commands, descriptors: descriptors, probeTimeout: timeout}, nil
}

// ID implements cognition.Adapter.
func (a *Adapter) ID() string { return AdapterID }

// Discover implements cognition.Adapter.
//
// Discovery is inventory-driven: it reports an endpoint for each CLI the
// environment layer found installed, and nothing for the others. It runs no
// inference — a CLI's callability is established by Probe, at inference depth,
// because invoking a coding agent consumes the user's quota and an ordinary
// environment query must not do that silently.
func (a *Adapter) Discover(_ context.Context, in cognition.DiscoveryInput) ([]protocol.CognitionEndpoint, error) {
	var endpoints []protocol.CognitionEndpoint
	for _, descriptor := range a.descriptors {
		presence, found := softwarePresence(in.Facts, descriptor.SoftwareID)
		if !found || !presence.Installed {
			continue
		}
		endpoint := cognition.CLIEndpoint(AdapterID+":"+descriptor.SoftwareID, descriptor.Provider)
		endpoint.Runtime = descriptor.SoftwareID
		endpoint.Version = presence.Version
		endpoint.CostClass = descriptor.CostClass
		endpoint.ObservedAt = in.ObservedAt
		// Installed is where discovery stops. A binary on PATH is not a usable
		// endpoint (docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md §7).
		endpoint.Health = protocol.EndpointHealthInstalled
		endpoint.Auth = protocol.AuthUnknown
		endpoint.Findings = append(endpoint.Findings, finding(endpoint.ID, "auth",
			protocol.FindingUnsupported,
			"no non-mutating, non-secret-reading way to read this CLI's authentication state is published, "+
				"so the authentication state is unknown"))
		if presence.Version == "" {
			endpoint.Findings = append(endpoint.Findings, finding(endpoint.ID, "version",
				protocol.FindingAbsent, "the CLI reported no recognisable version"))
		}
		if presence.VersionStatus == protocol.VersionIncompatible {
			// Below the declared compatibility floor. This is distinct from
			// absence: installing something does not fix it, and probing it
			// would produce a confusing failure rather than a clear state. No
			// coding CLI in the shipped inventory declares a floor today, so
			// this path is reachable only when one is configured — which is the
			// point of having it rather than discovering the need later.
			endpoint.Health = protocol.EndpointHealthUnsupported
			endpoint.Findings = append(endpoint.Findings, finding(endpoint.ID, "version",
				protocol.FindingUnsupported,
				"version "+presence.Version+" is below the compatibility floor this build declares"))
			endpoints = append(endpoints, endpoint)
			continue
		}
		if descriptor.PromptArgs == nil {
			endpoint.Findings = append(endpoint.Findings, finding(endpoint.ID, "health",
				protocol.FindingUnsupported,
				"this build knows no safe non-interactive invocation for this CLI, so callability cannot be probed"))
		} else if !in.Depth.AtLeast(protocol.DepthInference) {
			endpoint.Findings = append(endpoint.Findings, finding(endpoint.ID, "health",
				protocol.FindingUnsupported,
				"probe depth "+string(in.Depth)+" does not permit invoking the CLI; "+
					"calling a coding agent consumes the user's quota and is never implicit"))
		}
		endpoints = append(endpoints, endpoint)
	}
	sort.Slice(endpoints, func(i, j int) bool { return endpoints[i].ID < endpoints[j].ID })
	return endpoints, nil
}

// Probe implements cognition.Adapter.
//
// The prompt is SyntheticProbePrompt and the working directory is the probe
// directory, not a repository: a health check must not send source anywhere
// (docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md §12).
func (a *Adapter) Probe(
	ctx context.Context,
	endpoint protocol.CognitionEndpoint,
	req cognition.ProbeRequest,
) (cognition.ProbeResult, error) {
	req = req.Normalise()
	result := cognition.ProbeResult{Status: protocol.FindingUnsupported}
	descriptor, found := a.descriptorFor(endpoint)
	if !found {
		result.Detail = "no descriptor matches this endpoint"
		return result, nil
	}
	if descriptor.PromptArgs == nil {
		result.Detail = "this build knows no safe non-interactive invocation for this CLI"
		return result, nil
	}

	args := make([]string, 0, len(descriptor.PromptArgs))
	for _, arg := range descriptor.PromptArgs {
		if arg == PromptPlaceholder {
			args = append(args, req.Prompt)
			continue
		}
		args = append(args, arg)
	}
	timeout := req.Timeout
	if timeout > a.probeTimeout {
		timeout = a.probeTimeout
	}
	outcome := a.commands.Run(ctx, environment.ProbeCommand{
		Name:       descriptor.SoftwareID + "-health",
		Executable: descriptor.Executable,
		Args:       args,
		Timeout:    timeout,
	})
	result.RawEvidence = []byte(outcome.Stdout)
	result.EvidenceMediaType = "text/plain"

	switch {
	case outcome.Status == protocol.FindingTimeout:
		result.Status = protocol.FindingTimeout
		result.Detail = "the CLI did not answer within its time bound"
		return result, nil
	case outcome.Status == protocol.FindingAbsent:
		result.Status = protocol.FindingAbsent
		result.Detail = "the CLI executable could not be resolved"
		return result, nil
	case !outcome.Succeeded():
		result.Status = protocol.FindingError
		result.Detail = "the CLI exited without answering: " + outcome.Detail
		if mentionsSignIn(outcome.Stdout + " " + outcome.Stderr) {
			// A downgrade only. Nothing here can claim authentication, and the
			// service refuses a promotion even if an adapter attempted one.
			result.Detail = "the CLI reports that it is not signed in"
			result.Auth = protocol.AuthUnauthenticated
		}
		return result, nil
	case strings.TrimSpace(outcome.Stdout) == "":
		result.Status = protocol.FindingMalformed
		result.Detail = "the CLI exited successfully but produced no output"
		return result, nil
	}

	result.Status = protocol.FindingObserved
	result.Detail = "the CLI answered a synthetic prompt containing no repository content"
	result.Measurements = []protocol.Measurement{{
		Name: "request_duration_ms", Value: float64(outcome.Duration.Milliseconds()),
		Unit: "ms", Source: descriptor.SoftwareID,
	}}
	if req.RequireStructuredOutput {
		result.StructuredOutput = structuredOutcome(outcome.Stdout)
	}
	// Deliberately no Backend and no Signals: inference happened remotely, so
	// there is no local acceleration to describe, and the service refuses to
	// attach acceleration evidence to a non-local endpoint anyway.
	return result, nil
}

// signInMarkers are the phrases a CLI uses to say the user is not signed in.
//
// Matching is deliberately narrow and can only downgrade an endpoint. Treating
// provider text as authority in the other direction would let output decide it
// was trustworthy (DCI-083).
var signInMarkers = []string{
	"not logged in", "not signed in", "please log in", "please sign in",
	"login required", "authentication required", "unauthenticated",
	"run `login`", "no credentials",
}

func mentionsSignIn(output string) bool {
	lowered := strings.ToLower(output)
	for _, marker := range signInMarkers {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}

func structuredOutcome(output string) protocol.FeatureSupport {
	trimmed := strings.TrimSpace(output)
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start < 0 || end <= start {
		return protocol.FeatureProbeFailed
	}
	if !strings.Contains(trimmed[start:end+1], "\"ok\"") {
		return protocol.FeatureProbeFailed
	}
	return protocol.FeatureProbePassed
}

func (a *Adapter) descriptorFor(endpoint protocol.CognitionEndpoint) (Descriptor, bool) {
	for _, descriptor := range a.descriptors {
		if AdapterID+":"+descriptor.SoftwareID == endpoint.ID {
			return descriptor, true
		}
	}
	return Descriptor{}, false
}

func softwarePresence(facts protocol.EnvironmentFacts, id string) (protocol.SoftwarePresence, bool) {
	for _, entry := range facts.Software {
		if entry.ID == id {
			return entry, true
		}
	}
	return protocol.SoftwarePresence{}, false
}

func finding(endpointID, aspect string, status protocol.FindingStatus, detail string) protocol.DiscoveryFinding {
	return protocol.DiscoveryFinding{
		Component: "endpoint." + endpointID + "." + aspect,
		Status:    status,
		Source:    AdapterID,
		Detail:    detail,
	}
}
