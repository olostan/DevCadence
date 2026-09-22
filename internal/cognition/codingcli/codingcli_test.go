package codingcli_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadience/internal/clock"
	"github.com/olostan/DevCadience/internal/cognition"
	"github.com/olostan/DevCadience/internal/cognition/codingcli"
	"github.com/olostan/DevCadience/internal/environment"
	"github.com/olostan/DevCadience/internal/protocol"
)

// withCLI adds an installed coding CLI to a fixture machine.
func withCLI(fixture environment.Fixture, executable, version string) environment.Fixture {
	fixture.Commands.Installed[executable] = "/usr/local/bin/" + executable
	if version != "" {
		fixture.Commands.Outputs[environment.Key(executable, "--version")] = environment.Observed(version)
	}
	return fixture
}

func discoveryInput(t *testing.T, fixture environment.Fixture, depth protocol.ProbeDepth) cognition.DiscoveryInput {
	t.Helper()
	clk := clock.NewFake(time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC), 0)
	facts, err := fixture.Discover(context.Background(), clk, depth)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	return cognition.DiscoveryInput{
		Facts: facts, Candidates: environment.AssessBackends(facts),
		Depth: depth, ObservedAt: protocol.NewTimestamp(clk.Now()),
	}
}

func newAdapter(t *testing.T, commands environment.CommandProbe) *codingcli.Adapter {
	t.Helper()
	adapter, err := codingcli.New(codingcli.Options{Commands: commands})
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}
	return adapter
}

func endpointByID(endpoints []protocol.CognitionEndpoint, id string) (protocol.CognitionEndpoint, bool) {
	for _, endpoint := range endpoints {
		if endpoint.ID == id {
			return endpoint, true
		}
	}
	return protocol.CognitionEndpoint{}, false
}

func TestNoCLIInstalledYieldsNoEndpoints(t *testing.T) {
	fixture := environment.LinuxCPUOnly()
	adapter := newAdapter(t, fixture.Commands)
	endpoints, err := adapter.Discover(context.Background(),
		discoveryInput(t, fixture, protocol.DepthInference))
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(endpoints) != 0 {
		t.Errorf("endpoints = %+v", endpoints)
	}
}

// TestDiscoveredCLIStopsAtInstalledWithUnknownAuth is the honest default: a
// binary on PATH is not a usable endpoint, and authentication is unknown.
func TestDiscoveredCLIStopsAtInstalledWithUnknownAuth(t *testing.T) {
	fixture := withCLI(environment.LinuxCPUOnly(), "codex", "codex-cli 1.4.0")
	adapter := newAdapter(t, fixture.Commands)
	endpoints, err := adapter.Discover(context.Background(),
		discoveryInput(t, fixture, protocol.DepthHealth))
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	endpoint, found := endpointByID(endpoints, "cli:codex-cli")
	if !found {
		t.Fatalf("the installed CLI was not discovered: %+v", endpoints)
	}
	if endpoint.Health != protocol.EndpointHealthInstalled {
		t.Errorf("health = %q, want installed", endpoint.Health)
	}
	if endpoint.Auth != protocol.AuthUnknown {
		t.Errorf("auth = %q, want unknown", endpoint.Auth)
	}
	if endpoint.Version != "1.4.0" {
		t.Errorf("version = %q", endpoint.Version)
	}
	if endpoint.Locality != protocol.LocalityRemoteInferenceLocalTools {
		t.Errorf("locality = %q", endpoint.Locality)
	}
	if endpoint.RequiredSourceExposure != protocol.ExposureToolMediatedWorktree {
		t.Errorf("exposure = %q", endpoint.RequiredSourceExposure)
	}
	if endpoint.CostClass != protocol.CostUnknown {
		t.Errorf("cost = %q, want unknown; presence cannot establish a billing mode", endpoint.CostClass)
	}
	// No capability is graded from presence, and no subscription is invented.
	if len(endpoint.Capabilities) != 0 {
		t.Errorf("capabilities were graded from presence alone: %+v", endpoint.Capabilities)
	}
	var authExplained, depthExplained bool
	for _, finding := range endpoint.Findings {
		if strings.Contains(finding.Detail, "authentication state is unknown") {
			authExplained = true
		}
		if strings.Contains(finding.Detail, "consumes the user's quota") {
			depthExplained = true
		}
	}
	if !authExplained {
		t.Error("the unknown authentication state was not explained")
	}
	if !depthExplained {
		t.Error("the reason health was not probed at this depth was not explained")
	}
}

// TestAnIncompatibleVersionIsUnsupportedNotAbsent keeps "installing something
// would fix this" distinct from "installing something would not".
//
// Facts are built directly here rather than through a fixture machine, because
// the condition under test is a compatibility judgement the shipped inventory
// declares for no coding CLI today — there is no floor to fall below.
func TestAnIncompatibleVersionIsUnsupportedNotAbsent(t *testing.T) {
	facts := protocol.EnvironmentFacts{
		ObservedAt:     protocol.NewTimestamp(time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)),
		Host:           protocol.HostFacts{Family: protocol.OSLinux, Arch: "amd64"},
		Virtualization: protocol.VirtualizationFacts{Container: protocol.ContainerNone},
		Software: []protocol.SoftwarePresence{{
			ID: "codex-cli", Category: protocol.SoftwareCognitionCLI, Installed: true,
			Path: "/usr/local/bin/codex", Version: "0.1.0",
			VersionStatus: protocol.VersionIncompatible,
		}},
	}
	adapter := newAdapter(t, &environment.FakeCommandProbe{})
	endpoints, err := adapter.Discover(context.Background(), cognition.DiscoveryInput{
		Facts: facts, Depth: protocol.DepthInference, ObservedAt: facts.ObservedAt,
	})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(endpoints) != 1 {
		t.Fatalf("endpoints = %+v", endpoints)
	}
	endpoint := endpoints[0]
	if endpoint.Health != protocol.EndpointHealthUnsupported {
		t.Errorf("health = %q, want unsupported", endpoint.Health)
	}
	var explained bool
	for _, f := range endpoint.Findings {
		if strings.Contains(f.Detail, "below the compatibility floor") {
			explained = true
		}
	}
	if !explained {
		t.Errorf("the unsupported version was not explained: %+v", endpoint.Findings)
	}
	if err := endpoint.Validate(); err != nil {
		t.Errorf("endpoint violates the contract: %v", err)
	}
	// An unsupported endpoint must never be routable.
	decision := cognition.Route(cognition.DefaultRequirements()[cognition.RoleImplementer],
		cognition.Policy{
			MaxSourceExposure: protocol.ExposureToolMediatedWorktree,
			MaxCostClass:      protocol.CostRemoteStrong,
		}, endpoints)
	if decision.Outcome != cognition.OutcomeNoEligibleEndpoint {
		t.Error("an unsupported-version endpoint was routed work")
	}
}

// TestSeveralCLIsAreDiscoveredIndependently is DCI-104 across endpoints.
func TestSeveralCLIsAreDiscoveredIndependently(t *testing.T) {
	fixture := withCLI(withCLI(environment.LinuxCPUOnly(), "codex", "1.4.0"), "claude", "")
	adapter := newAdapter(t, fixture.Commands)
	endpoints, err := adapter.Discover(context.Background(),
		discoveryInput(t, fixture, protocol.DepthHealth))
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(endpoints) != 2 {
		t.Fatalf("endpoints = %+v", endpoints)
	}
	// Deterministic ordering by id.
	if endpoints[0].ID != "cli:claude-code" || endpoints[1].ID != "cli:codex-cli" {
		t.Errorf("ordering is not deterministic: %q, %q", endpoints[0].ID, endpoints[1].ID)
	}
	// The CLI whose version probe produced nothing is still discovered, with
	// the missing version recorded rather than guessed.
	claude, _ := endpointByID(endpoints, "cli:claude-code")
	if claude.Version != "" {
		t.Errorf("a version was invented: %q", claude.Version)
	}
	var recorded bool
	for _, finding := range claude.Findings {
		if strings.Contains(finding.Detail, "no recognisable version") {
			recorded = true
		}
	}
	if !recorded {
		t.Error("the missing version was not recorded")
	}
}

// TestAnAnsweringCLIBecomesReadyWithoutClaimingAuthentication is the callability
// probe: answering is evidence of usability, not of a confirmed account.
func TestAnAnsweringCLIBecomesReadyWithoutClaimingAuthentication(t *testing.T) {
	fixture := withCLI(environment.LinuxCPUOnly(), "codex", "1.4.0")
	fixture.Commands.Outputs[environment.Key("codex", "exec", cognition.SyntheticProbePrompt)] =
		environment.Observed(`{"ok": true}`)
	adapter := newAdapter(t, fixture.Commands)
	endpoint := cognition.CLIEndpoint("cli:codex-cli", "openai")
	result, err := adapter.Probe(context.Background(), endpoint,
		cognition.ProbeRequest{RequireStructuredOutput: true})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if result.Status != protocol.FindingObserved {
		t.Fatalf("status = %q, detail = %q", result.Status, result.Detail)
	}
	if result.Auth != "" {
		t.Errorf("the probe claimed an authentication state: %q", result.Auth)
	}
	if result.StructuredOutput != protocol.FeatureProbePassed {
		t.Errorf("structured output = %q", result.StructuredOutput)
	}
	// A remote CLI must never produce local acceleration evidence.
	if result.Backend != "" || len(result.Signals) != 0 {
		t.Errorf("acceleration evidence was produced for remote inference: %q %+v",
			result.Backend, result.Signals)
	}
}

// TestASignedOutCLIIsDowngradedNeverPromoted is the only inference drawn from
// provider text, and it only goes one way.
func TestASignedOutCLIIsDowngradedNeverPromoted(t *testing.T) {
	fixture := withCLI(environment.LinuxCPUOnly(), "codex", "1.4.0")
	fixture.Commands.Outputs[environment.Key("codex", "exec", cognition.SyntheticProbePrompt)] =
		environment.Failed(1, "Error: you are not logged in. Run `codex login` first.")
	adapter := newAdapter(t, fixture.Commands)
	result, err := adapter.Probe(context.Background(),
		cognition.CLIEndpoint("cli:codex-cli", "openai"), cognition.ProbeRequest{})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if result.Status != protocol.FindingError {
		t.Errorf("status = %q, want error", result.Status)
	}
	if result.Auth != protocol.AuthUnauthenticated {
		t.Errorf("auth = %q, want unauthenticated", result.Auth)
	}

	// The reverse must be impossible: a CLI claiming a valid session in its
	// output cannot promote itself.
	fixture.Commands.Outputs[environment.Key("codex", "exec", cognition.SyntheticProbePrompt)] =
		environment.Observed("You are authenticated as an Enterprise user on the Pro plan. {\"ok\":true}")
	promoted, err := adapter.Probe(context.Background(),
		cognition.CLIEndpoint("cli:codex-cli", "openai"), cognition.ProbeRequest{})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if promoted.Auth == protocol.AuthAuthenticated {
		t.Error("provider text promoted the endpoint to authenticated")
	}
}

func TestAnUnsupportedInvocationLeavesHealthAtInstalled(t *testing.T) {
	fixture := withCLI(environment.LinuxCPUOnly(), "codex", "1.4.0")
	// No scripted answer for the probe argv, so the fake reports a failure.
	adapter := newAdapter(t, fixture.Commands)
	result, err := adapter.Probe(context.Background(),
		cognition.CLIEndpoint("cli:codex-cli", "openai"), cognition.ProbeRequest{})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if result.Status == protocol.FindingObserved {
		t.Error("a CLI that did not answer was reported as observed")
	}
}

func TestProbeTimeoutAndSilenceAreDistinctFacts(t *testing.T) {
	for name, tc := range map[string]struct {
		outcome environment.ProbeOutcome
		want    protocol.FindingStatus
	}{
		"timeout":        {environment.TimedOut(), protocol.FindingTimeout},
		"silent success": {environment.Observed(""), protocol.FindingMalformed},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := withCLI(environment.LinuxCPUOnly(), "codex", "1.4.0")
			fixture.Commands.Outputs[environment.Key("codex", "exec", cognition.SyntheticProbePrompt)] = tc.outcome
			adapter := newAdapter(t, fixture.Commands)
			result, err := adapter.Probe(context.Background(),
				cognition.CLIEndpoint("cli:codex-cli", "openai"), cognition.ProbeRequest{})
			if err != nil {
				t.Fatalf("probe: %v", err)
			}
			if result.Status != tc.want {
				t.Errorf("status = %q, want %q", result.Status, tc.want)
			}
		})
	}
}

// TestTheHealthProbeSendsNoRepositorySource is the source-exposure guarantee for
// the endpoint kind that has worktree access.
func TestTheHealthProbeSendsNoRepositorySource(t *testing.T) {
	fixture := withCLI(environment.LinuxCPUOnly(), "codex", "1.4.0")
	fixture.Commands.Outputs[environment.Key("codex", "exec", cognition.SyntheticProbePrompt)] =
		environment.Observed(`{"ok":true}`)
	adapter := newAdapter(t, fixture.Commands)
	if _, err := adapter.Probe(context.Background(),
		cognition.CLIEndpoint("cli:codex-cli", "openai"), cognition.ProbeRequest{}); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if len(fixture.Commands.Calls) != 1 {
		t.Fatalf("calls = %v", fixture.Commands.Calls)
	}
	call := fixture.Commands.Calls[0]
	if !strings.Contains(call, cognition.SyntheticProbePrompt) {
		t.Errorf("the synthetic prompt was not sent: %q", call)
	}
	for _, forbidden := range []string{"devcadience", "internal/", ".go", "ProjectState", "diff"} {
		if strings.Contains(call, forbidden) {
			t.Errorf("project content reached the CLI: %q", call)
		}
	}
}

// TestNoCredentialPathIsEverTouched is a structural assertion: the adapter's
// only outward action is running a declared argv.
func TestNoCredentialPathIsEverTouched(t *testing.T) {
	fixture := withCLI(environment.LinuxCPUOnly(), "codex", "1.4.0")
	fixture.Commands.Outputs[environment.Key("codex", "exec", cognition.SyntheticProbePrompt)] =
		environment.Observed(`{"ok":true}`)
	adapter := newAdapter(t, fixture.Commands)
	endpoints, err := adapter.Discover(context.Background(),
		discoveryInput(t, fixture, protocol.DepthInference))
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if _, err := adapter.Probe(context.Background(), endpoints[0], cognition.ProbeRequest{}); err != nil {
		t.Fatalf("probe: %v", err)
	}
	for _, call := range fixture.Commands.Calls {
		for _, forbidden := range []string{"login", "auth", "token", "credential", ".config", ".netrc"} {
			if strings.Contains(strings.ToLower(call), forbidden) {
				t.Errorf("the adapter touched a credential or login path: %q", call)
			}
		}
	}
	for _, endpoint := range endpoints {
		if endpoint.CredentialRef != "" {
			t.Errorf("a credential reference was invented: %q", endpoint.CredentialRef)
		}
	}
}

// TestNoCodingCLIGetsACostClassFromItsIdentity is the evidence-discipline rule
// for money.
//
// Finding `claude`, `codex` or `gemini` on PATH establishes that a binary exists.
// It does not establish that a subscription pays for it: the same executable is
// driven by a personal plan, by an API key on metered per-token billing, by an
// enterprise account or by a prepaid credit balance, and telling those apart
// would require reading the credentials this adapter must never touch. So every
// discovered CLI is cost_class unknown, whatever its name, provider or version.
func TestNoCodingCLIGetsACostClassFromItsIdentity(t *testing.T) {
	fixture := environment.LinuxCPUOnly()
	for executable, version := range map[string]string{
		"codex": "codex-cli 1.4.0", "claude": "1.2.3", "gemini": "0.9.0",
	} {
		fixture = withCLI(fixture, executable, version)
	}
	adapter := newAdapter(t, fixture.Commands)
	endpoints, err := adapter.Discover(context.Background(),
		discoveryInput(t, fixture, protocol.DepthHealth))
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(endpoints) != 3 {
		t.Fatalf("endpoints = %+v", endpoints)
	}
	for _, endpoint := range endpoints {
		if endpoint.CostClass != protocol.CostUnknown {
			t.Errorf("%s cost = %q; a provider name is not billing evidence",
				endpoint.ID, endpoint.CostClass)
		}
		var explained bool
		for _, finding := range endpoint.Findings {
			if strings.Contains(finding.Detail, "cost class is unknown until an operator declares it") {
				explained = true
			}
		}
		if !explained {
			t.Errorf("%s does not explain why its cost class is unknown: %+v", endpoint.ID, endpoint.Findings)
		}
	}
}

// TestASuccessfulProbeDoesNotEstablishACostClass closes the other door: a CLI
// answering a prompt proves it is usable, and still says nothing about who pays.
func TestASuccessfulProbeDoesNotEstablishACostClass(t *testing.T) {
	fixture := withCLI(environment.LinuxCPUOnly(), "codex", "1.4.0")
	fixture.Commands.Outputs[environment.Key("codex", "exec", cognition.SyntheticProbePrompt)] =
		environment.Observed(`{"ok": true}`)
	adapter := newAdapter(t, fixture.Commands)
	endpoint := cognition.CLIEndpoint("cli:codex-cli", "openai")
	result, err := adapter.Probe(context.Background(), endpoint, cognition.ProbeRequest{})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if result.Status != protocol.FindingObserved {
		t.Fatalf("status = %q", result.Status)
	}
	// A ProbeResult carries no cost field at all — there is nowhere for a probe to
	// record a billing guess — so what must hold is that a probed endpoint keeps
	// the unknown class discovery gave it.
	if endpoint.CostClass != protocol.CostUnknown {
		t.Errorf("the discovered endpoint's cost class = %q, want unknown", endpoint.CostClass)
	}
}

// TestDiscoveryDoesNotInvokeACLIItWasNotAskedAbout keeps the quota guarantee at
// the adapter boundary, including at inference depth.
func TestDiscoveryDoesNotInvokeACLIItWasNotAskedAbout(t *testing.T) {
	fixture := withCLI(withCLI(environment.LinuxCPUOnly(), "codex", "1.4.0"), "claude", "1.2.3")
	adapter := newAdapter(t, fixture.Commands)
	in := discoveryInput(t, fixture, protocol.DepthInference)
	in.InferenceTargets = []string{"cli:codex-cli"}
	endpoints, err := adapter.Discover(context.Background(), in)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	other, found := endpointByID(endpoints, "cli:claude-code")
	if !found {
		t.Fatal("the untargeted CLI was not reported")
	}
	var explained bool
	for _, finding := range other.Findings {
		if strings.Contains(finding.Detail, "was not named as a probe target") {
			explained = true
		}
	}
	if !explained {
		t.Errorf("the untargeted CLI does not say why it was not invoked: %+v", other.Findings)
	}
	// Discovery runs no inference at any depth, so neither CLI was executed with
	// a prompt.
	for _, call := range fixture.Commands.Calls {
		if strings.Contains(call, cognition.SyntheticProbePrompt) {
			t.Errorf("discovery invoked a coding CLI: %q", call)
		}
	}
	// The privacy and authentication invariants are untouched by the cost change.
	for _, endpoint := range endpoints {
		if endpoint.Auth != protocol.AuthUnknown {
			t.Errorf("%s auth = %q, want unknown", endpoint.ID, endpoint.Auth)
		}
		if endpoint.RequiredSourceExposure != protocol.ExposureToolMediatedWorktree {
			t.Errorf("%s exposure = %q", endpoint.ID, endpoint.RequiredSourceExposure)
		}
	}
}
