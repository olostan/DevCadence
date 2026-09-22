package mlx_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/cognition/mlx"
	"github.com/olostan/DevCadence/internal/environment"
	"github.com/olostan/DevCadence/internal/protocol"
)

// The MLX adapter runs a Python program, so its tests drive the same
// CommandProbe fake the environment layer uses. Nothing here needs Python, MLX,
// a Mac or a GPU.

const homeDir = "/home/dev"

// introspectKey is the argv key for the introspection call. The program source
// is a package constant, so a test cannot reproduce it by hand; the fake matches
// on a prefix instead.
func outcomeFor(fake *environment.FakeCommandProbe, prefix string, outcome environment.ProbeOutcome) {
	fake.Prefixes = append(fake.Prefixes, environment.PrefixOutcome{Prefix: prefix, Outcome: outcome})
}

func appleSiliconFixture(t *testing.T, depth protocol.ProbeDepth) (cognition.DiscoveryInput, *environment.FakeCommandProbe) {
	t.Helper()
	fixture := environment.DarwinAppleSilicon()
	fixture.Commands.Installed["python3"] = "/usr/bin/python3"
	fixture.Commands.Outputs[environment.Key("python3", "--version")] = environment.Observed("Python 3.13.2")
	clk := clock.NewFake(time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC), 0)
	facts, err := fixture.Discover(context.Background(), clk, depth)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	return cognition.DiscoveryInput{
		Facts:      facts,
		Candidates: environment.AssessBackends(facts),
		Depth:      depth,
		ObservedAt: protocol.NewTimestamp(clk.Now()),
	}, fixture.Commands
}

func newAdapter(t *testing.T, commands environment.CommandProbe, sys environment.SysProbe) *mlx.Adapter {
	t.Helper()
	adapter, err := mlx.New(mlx.Options{Commands: commands, Sys: sys, HomeDir: homeDir})
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}
	return adapter
}

// cacheWith builds a Hugging Face cache containing the named repositories.
func cacheWith(repositories ...string) environment.FakeSysProbe {
	sys := environment.FakeSysProbe{Files: map[string]string{}, Dirs: map[string][]string{}}
	hub := homeDir + "/.cache/huggingface/hub"
	var entries []string
	for _, repository := range repositories {
		entry := "models--" + strings.ReplaceAll(repository, "/", "--")
		entries = append(entries, entry)
		sys.Dirs[hub+"/"+entry+"/snapshots"] = []string{"abc123"}
	}
	sys.Dirs[hub] = entries
	return sys
}

func TestAbsentRuntimeYieldsNoEndpoint(t *testing.T) {
	// A machine with neither python nor the console script cannot host MLX.
	fixture := environment.LinuxCPUOnly()
	clk := clock.NewFake(time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC), 0)
	facts, err := fixture.Discover(context.Background(), clk, protocol.DepthHealth)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	adapter := newAdapter(t, fixture.Commands, environment.FakeSysProbe{})
	endpoints, err := adapter.Discover(context.Background(), cognition.DiscoveryInput{
		Facts: facts, Depth: protocol.DepthHealth,
	})
	if err != nil {
		t.Fatalf("an absent optional runtime must not be an error: %v", err)
	}
	if len(endpoints) != 0 {
		t.Errorf("endpoints = %+v", endpoints)
	}
}

// TestPythonPresentButMLXNotImportable is "installed != usable".
func TestPythonPresentButMLXNotImportable(t *testing.T) {
	in, commands := appleSiliconFixture(t, protocol.DepthHealth)
	outcomeFor(commands, "python3 -c", environment.Observed(`{"error": "mlx.core import failed: ModuleNotFoundError"}`))
	adapter := newAdapter(t, commands, environment.FakeSysProbe{})
	endpoints, err := adapter.Discover(context.Background(), in)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(endpoints) != 1 {
		t.Fatalf("endpoints = %+v", endpoints)
	}
	if endpoints[0].Health != protocol.EndpointHealthNotInstalled {
		t.Errorf("health = %q, want not_installed", endpoints[0].Health)
	}
}

// TestMLXWithoutMLXLMIsNotConfigured separates the array framework from the
// language-model layer this adapter drives.
func TestMLXWithoutMLXLMIsNotConfigured(t *testing.T) {
	in, commands := appleSiliconFixture(t, protocol.DepthHealth)
	outcomeFor(commands, "python3 -c", environment.Observed(
		`{"mlx_version":"0.32.2","default_device":"Device(gpu, 0)","metal_available":true,"mlx_lm_error":"ModuleNotFoundError"}`))
	adapter := newAdapter(t, commands, environment.FakeSysProbe{})
	endpoints, err := adapter.Discover(context.Background(), in)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if endpoints[0].Health != protocol.EndpointHealthNotConfigured {
		t.Errorf("health = %q, want not_configured", endpoints[0].Health)
	}
}

// TestNoCachedModelIsNotAFailure keeps a working runtime with no weights from
// being reported as broken — and the adapter must not download any.
func TestNoCachedModelIsNotAFailure(t *testing.T) {
	in, commands := appleSiliconFixture(t, protocol.DepthHealth)
	outcomeFor(commands, "python3 -c", environment.Observed(
		`{"mlx_version":"0.32.2","mlx_lm_version":"0.29.0","default_device":"Device(gpu, 0)","metal_available":true}`))
	adapter := newAdapter(t, commands, cacheWith())
	endpoints, err := adapter.Discover(context.Background(), in)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if endpoints[0].Health != protocol.EndpointHealthNotConfigured {
		t.Errorf("health = %q, want not_configured", endpoints[0].Health)
	}
	var explained bool
	for _, finding := range endpoints[0].Findings {
		if strings.Contains(finding.Detail, "will not download weights") {
			explained = true
		}
	}
	if !explained {
		t.Errorf("the absent model was not explained: %+v", endpoints[0].Findings)
	}
}

func TestCachedMLXModelsBecomeUnverifiedEndpoints(t *testing.T) {
	in, commands := appleSiliconFixture(t, protocol.DepthHealth)
	outcomeFor(commands, "python3 -c", environment.Observed(
		`{"mlx_version":"0.32.2","mlx_lm_version":"0.29.0","default_device":"Device(gpu, 0)","metal_available":true}`))
	// A non-MLX cache entry must not become an endpoint: every probe against it
	// would fail.
	adapter := newAdapter(t, commands, cacheWith(
		"mlx-community/Qwen3-4B-4bit", "meta-llama/Llama-3.2-3B"))
	endpoints, err := adapter.Discover(context.Background(), in)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(endpoints) != 1 {
		t.Fatalf("expected only the mlx model, got %+v", ids(endpoints))
	}
	endpoint := endpoints[0]
	if endpoint.ID != "mlx:mlx-community/Qwen3-4B-4bit" {
		t.Errorf("id = %q", endpoint.ID)
	}
	if endpoint.Health != protocol.EndpointHealthUnverified {
		t.Errorf("health = %q, want unverified", endpoint.Health)
	}
	if !strings.HasPrefix(endpoint.ModelID, homeDir) {
		t.Errorf("model reference = %q; it must be a local snapshot path so no download can be triggered",
			endpoint.ModelID)
	}
	if !strings.Contains(endpoint.Version, "mlx-lm 0.29.0") {
		t.Errorf("version = %q", endpoint.Version)
	}
	if err := endpoint.Validate(); err != nil {
		t.Errorf("endpoint violates the contract: %v", err)
	}
}

// TestGPUGenerationVerifiesMetal is the positive MLX acceleration path: the
// generating process reported its own device.
func TestGPUGenerationVerifiesMetal(t *testing.T) {
	commands := &environment.FakeCommandProbe{Installed: map[string]string{"python3": "/usr/bin/python3"}}
	outcomeFor(commands, "python3 -c", environment.Observed(
		`{"load_seconds":1.25,"generate_seconds":0.8,"text":"{\"ok\": true}",`+
			`"default_device":"Device(gpu, 0)","metal_available":true,"peak_memory_bytes":3221225472}`))
	adapter := newAdapter(t, commands, environment.FakeSysProbe{})
	endpoint := cognition.LocalEndpoint("mlx:model", "mlx", homeDir+"/.cache/huggingface/hub/models--x/snapshots/abc")
	result, err := adapter.Probe(context.Background(), endpoint,
		cognition.ProbeRequest{RequireStructuredOutput: true})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if result.Status != protocol.FindingObserved {
		t.Fatalf("status = %q, detail = %q", result.Status, result.Detail)
	}
	if result.Backend != protocol.BackendMetal {
		t.Errorf("backend = %q, want metal", result.Backend)
	}
	var authoritative, corroborating int
	for _, signal := range result.Signals {
		switch signal.Trust {
		case protocol.TrustAuthoritative:
			authoritative++
			if !signal.Offloaded {
				t.Error("the authoritative signal says the gpu was not used")
			}
		case protocol.TrustCorroborating:
			corroborating++
		}
	}
	if authoritative != 1 || corroborating != 1 {
		t.Errorf("signals = %+v", result.Signals)
	}
	if result.StructuredOutput != protocol.FeatureProbePassed {
		t.Errorf("structured output = %q", result.StructuredOutput)
	}
	byName := map[string]float64{}
	for _, measurement := range result.Measurements {
		byName[measurement.Name] = measurement.Value
	}
	if byName["load_duration_ms"] != 1250 || byName["peak_memory_bytes"] != 3221225472 {
		t.Errorf("measurements = %v", byName)
	}
	// No throughput measurement: MLX-LM did not report token counts, and
	// deriving one from the text would be a fabrication.
	if _, present := byName["generation_tokens_per_second"]; present {
		t.Error("a throughput figure was produced without token counts from the runtime")
	}

	evidence := cognition.EvaluateAcceleration(cognition.AccelerationInput{
		Backend: result.Backend, Signals: result.Signals,
		ProbeRan: true, ProbeSucceeded: true,
		ObservedAt: protocol.NewTimestamp(time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)),
	})
	if evidence.State != protocol.StateVerified {
		t.Errorf("state = %q, want verified", evidence.State)
	}
}

// TestCPUDeviceDoesNotVerifyMetal is the case where Metal exists and was not
// used.
func TestCPUDeviceDoesNotVerifyMetal(t *testing.T) {
	commands := &environment.FakeCommandProbe{Installed: map[string]string{"python3": "/usr/bin/python3"}}
	outcomeFor(commands, "python3 -c", environment.Observed(
		`{"load_seconds":3.1,"generate_seconds":9.4,"text":"ok",`+
			`"default_device":"Device(cpu, 0)","metal_available":true}`))
	adapter := newAdapter(t, commands, environment.FakeSysProbe{})
	result, err := adapter.Probe(context.Background(),
		cognition.LocalEndpoint("mlx:model", "mlx", "/models/x"), cognition.ProbeRequest{})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if result.Backend != protocol.BackendMetal {
		t.Errorf("backend = %q; the intended backend is still metal", result.Backend)
	}
	if len(result.Signals) != 1 || result.Signals[0].Offloaded {
		t.Fatalf("signals = %+v, want one non-offloaded authoritative signal", result.Signals)
	}
	evidence := cognition.EvaluateAcceleration(cognition.AccelerationInput{
		Backend: result.Backend, Signals: result.Signals, ProbeRan: true, ProbeSucceeded: true,
	})
	if evidence.State != protocol.StateFailed {
		t.Errorf("state = %q, want failed; metal was available and unused", evidence.State)
	}
}

func TestMalformedAndFailingGenerationAreFacts(t *testing.T) {
	for name, tc := range map[string]struct {
		outcome environment.ProbeOutcome
		want    protocol.FindingStatus
	}{
		"not json":         {environment.Observed("Traceback (most recent call last):"), protocol.FindingMalformed},
		"reported failure": {environment.Observed(`{"error":"generation failed: ValueError: bad shape"}`), protocol.FindingError},
		"nonzero exit":     {environment.Failed(1, "killed"), protocol.FindingError},
		"timeout":          {environment.TimedOut(), protocol.FindingTimeout},
	} {
		t.Run(name, func(t *testing.T) {
			commands := &environment.FakeCommandProbe{Installed: map[string]string{"python3": "/usr/bin/python3"}}
			outcomeFor(commands, "python3 -c", tc.outcome)
			adapter := newAdapter(t, commands, environment.FakeSysProbe{})
			result, err := adapter.Probe(context.Background(),
				cognition.LocalEndpoint("mlx:model", "mlx", "/models/x"), cognition.ProbeRequest{})
			if err != nil {
				t.Fatalf("a failing probe must be a fact: %v", err)
			}
			if result.Status != tc.want {
				t.Errorf("status = %q, want %q", result.Status, tc.want)
			}
		})
	}
}

// TestANonPathModelReferenceIsRefused is what stops a probe from triggering a
// multi-gigabyte download.
func TestANonPathModelReferenceIsRefused(t *testing.T) {
	commands := &environment.FakeCommandProbe{Installed: map[string]string{"python3": "/usr/bin/python3"}}
	adapter := newAdapter(t, commands, environment.FakeSysProbe{})
	_, err := adapter.Probe(context.Background(),
		cognition.LocalEndpoint("mlx:model", "mlx", "mlx-community/Qwen3-4B-4bit"), cognition.ProbeRequest{})
	if err == nil {
		t.Fatal("a repository reference was accepted, which would download weights")
	}
	if len(commands.Calls) != 0 {
		t.Errorf("the interpreter was invoked anyway: %v", commands.Calls)
	}
}

// TestTheProbeSendsOnlySyntheticContentAsArgv checks both the no-source rule and
// the no-shell-interpolation rule.
func TestTheProbeSendsOnlySyntheticContentAsArgv(t *testing.T) {
	commands := &environment.FakeCommandProbe{Installed: map[string]string{"python3": "/usr/bin/python3"}}
	outcomeFor(commands, "python3 -c", environment.Observed(
		`{"text":"{\"ok\":true}","default_device":"Device(gpu, 0)"}`))
	adapter := newAdapter(t, commands, environment.FakeSysProbe{})
	if _, err := adapter.Probe(context.Background(),
		cognition.LocalEndpoint("mlx:model", "mlx", "/models/x"), cognition.ProbeRequest{}); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if len(commands.Calls) != 1 {
		t.Fatalf("calls = %v", commands.Calls)
	}
	call := commands.Calls[0]
	if !strings.Contains(call, cognition.SyntheticProbePrompt) {
		t.Errorf("the synthetic prompt was not passed: %q", call)
	}
	// No shell metacharacter handling exists because there is no shell: the
	// program and the prompt are separate argv elements.
	if strings.Contains(call, "&&") || strings.Contains(call, "|") || strings.Contains(call, ";") {
		t.Errorf("the invocation looks like a shell string: %q", call)
	}
	for _, forbidden := range []string{"devcadence", "internal/", "ProjectState"} {
		if strings.Contains(call, forbidden) {
			t.Errorf("project content reached the interpreter: %q", call)
		}
	}
}

// TestInventoryDepthRunsNoInterpreter is the progressive-depth guarantee.
func TestInventoryDepthRunsNoInterpreter(t *testing.T) {
	in, commands := appleSiliconFixture(t, protocol.DepthInventory)
	before := len(commands.Calls)
	adapter := newAdapter(t, commands, environment.FakeSysProbe{})
	endpoints, err := adapter.Discover(context.Background(), in)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(commands.Calls) != before {
		t.Errorf("inventory depth ran commands: %v", commands.Calls[before:])
	}
	if len(endpoints) != 1 || endpoints[0].Health != protocol.EndpointHealthInstalled {
		t.Errorf("endpoints = %+v", endpoints)
	}
}

func ids(endpoints []protocol.CognitionEndpoint) []string {
	out := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		out = append(out, endpoint.ID)
	}
	return out
}
