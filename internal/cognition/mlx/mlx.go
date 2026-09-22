// Package mlx adapts the MLX-LM local runtime to the CognitionEndpoint
// contract.
//
// MLX-LM is a Python library rather than a service, so the adapter drives it
// through the controlled process runner. Two things follow from that, and both
// are deliberate:
//
// The Python it runs is a *constant in this file*. It is not assembled from
// configuration, not interpolated from discovered values, and not derived from
// any external output — the only variable that reaches the interpreter is a
// model path, passed as a separate argv element and validated first. There is no
// shell, so there is nothing to escape (DCI-033, docs/SECURITY.md §5).
//
// The introspection program uses getattr fallbacks rather than a pinned API
// surface. MLX moves quickly and the accessor for a peak-memory figure has
// changed names across releases; a program that hard-codes one and crashes on
// another would report a working runtime as broken. Anything the program cannot
// read is simply absent from its output, and absence becomes "unknown" rather
// than a guess.
//
// The adapter is not macOS-only in the domain model. It probes, and on a machine
// without Metal the probe reports a CPU device — which is a valid MLX
// configuration and a correctly unverified acceleration result, not an error.
// What the adapter will never do is create a virtual environment, install a
// package or download weights; those are M3B.
package mlx

import (
	"context"
	"encoding/json"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/environment"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// AdapterID is the stable adapter identity and the prefix of its endpoint ids.
const AdapterID = "mlx"

// MaxEndpoints bounds how many locally cached models become endpoints.
const MaxEndpoints = 4

// introspectProgram reports the runtime's own view of itself as one JSON line.
//
// Every accessor is optional. A field the installed version does not expose is
// omitted, and the Go side treats omission as unknown.
const introspectProgram = `
import json, importlib
out = {}
try:
    mx = importlib.import_module("mlx.core")
except Exception as exc:
    print(json.dumps({"error": "mlx.core import failed: %s" % type(exc).__name__}))
    raise SystemExit(0)
out["mlx_version"] = getattr(mx, "__version__", None)
try:
    out["default_device"] = str(mx.default_device())
except Exception:
    pass
for accessor in ("metal",):
    module = getattr(mx, accessor, None)
    if module is not None and hasattr(module, "is_available"):
        try:
            out["metal_available"] = bool(module.is_available())
        except Exception:
            pass
try:
    info = mx.device_info() if hasattr(mx, "device_info") else None
    if isinstance(info, dict):
        out["device_name"] = str(info.get("device_name", ""))
        for key in ("max_recommended_working_set_size", "memory_size"):
            if key in info:
                out[key] = int(info[key])
except Exception:
    pass
try:
    lm = importlib.import_module("mlx_lm")
    out["mlx_lm_version"] = getattr(lm, "__version__", None)
    if out["mlx_lm_version"] is None:
        metadata = importlib.import_module("importlib.metadata")
        out["mlx_lm_version"] = metadata.version("mlx-lm")
except Exception as exc:
    out["mlx_lm_error"] = "%s" % type(exc).__name__
print(json.dumps(out))
`

// generateProgram runs one synthetic generation and reports what happened.
//
// It reads the model path and prompt from argv rather than having them
// interpolated into the source, and it reports the device *after* generating, in
// the same process, which is what makes the device statement authoritative
// evidence about the inference that just ran.
const generateProgram = `
import json, sys, time, importlib
model_path, prompt, max_tokens = sys.argv[1], sys.argv[2], int(sys.argv[3])
out = {}
try:
    mx = importlib.import_module("mlx.core")
    lm = importlib.import_module("mlx_lm")
except Exception as exc:
    print(json.dumps({"error": "import failed: %s" % type(exc).__name__}))
    raise SystemExit(0)
try:
    started = time.perf_counter()
    model, tokenizer = lm.load(model_path)
    out["load_seconds"] = time.perf_counter() - started
    started = time.perf_counter()
    text = lm.generate(model, tokenizer, prompt=prompt, max_tokens=max_tokens, verbose=False)
    out["generate_seconds"] = time.perf_counter() - started
    out["text"] = text if isinstance(text, str) else str(text)
except Exception as exc:
    out["error"] = "generation failed: %s: %s" % (type(exc).__name__, str(exc)[:200])
    print(json.dumps(out))
    raise SystemExit(0)
try:
    out["default_device"] = str(mx.default_device())
except Exception:
    pass
metal = getattr(mx, "metal", None)
if metal is not None and hasattr(metal, "is_available"):
    try:
        out["metal_available"] = bool(metal.is_available())
    except Exception:
        pass
for holder, name in ((mx, "get_peak_memory"), (metal, "get_peak_memory")):
    if holder is not None and hasattr(holder, name):
        try:
            out["peak_memory_bytes"] = int(getattr(holder, name)())
            break
        except Exception:
            pass
print(json.dumps(out))
`

// Options configures an Adapter.
type Options struct {
	// Commands runs the interpreter. It is the same controlled-runner-backed
	// probe the environment layer uses.
	Commands environment.CommandProbe
	// Sys reads the model cache.
	Sys environment.SysProbe
	// HomeDir is the user's home directory, used to locate the Hugging Face
	// cache. It is passed in rather than read here so a test can describe a
	// cache without touching the real one.
	HomeDir string
	// Interpreter is the Python executable. Empty selects "python3".
	Interpreter string
	// HealthTimeout bounds introspection.
	HealthTimeout time.Duration
}

// Adapter is the MLX-LM CognitionEndpoint adapter.
type Adapter struct {
	commands      environment.CommandProbe
	sys           environment.SysProbe
	homeDir       string
	interpreter   string
	healthTimeout time.Duration
}

// New returns an Adapter.
func New(opts Options) (*Adapter, error) {
	if opts.Commands == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "mlx: a CommandProbe is required")
	}
	interpreter := opts.Interpreter
	if interpreter == "" {
		interpreter = "python3"
	}
	timeout := opts.HealthTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Adapter{
		commands: opts.Commands, sys: opts.Sys, homeDir: opts.HomeDir,
		interpreter: interpreter, healthTimeout: timeout,
	}, nil
}

// ID implements cognition.Adapter.
func (a *Adapter) ID() string { return AdapterID }

// introspection is the parsed output of introspectProgram.
type introspection struct {
	MLXVersion     *string `json:"mlx_version"`
	MLXLMVersion   *string `json:"mlx_lm_version"`
	DefaultDevice  string  `json:"default_device"`
	MetalAvailable *bool   `json:"metal_available"`
	DeviceName     string  `json:"device_name"`
	MemorySize     *int64  `json:"memory_size"`
	Error          string  `json:"error"`
	MLXLMError     string  `json:"mlx_lm_error"`
}

// generation is the parsed output of generateProgram.
type generation struct {
	LoadSeconds     *float64 `json:"load_seconds"`
	GenerateSeconds *float64 `json:"generate_seconds"`
	Text            string   `json:"text"`
	DefaultDevice   string   `json:"default_device"`
	MetalAvailable  *bool    `json:"metal_available"`
	PeakMemoryBytes *int64   `json:"peak_memory_bytes"`
	Error           string   `json:"error"`
}

// Discover implements cognition.Adapter.
func (a *Adapter) Discover(ctx context.Context, in cognition.DiscoveryInput) ([]protocol.CognitionEndpoint, error) {
	pythonInstalled, _ := softwarePresence(in.Facts, "python3")
	scriptInstalled, _ := softwarePresence(in.Facts, "mlx-lm")
	if !pythonInstalled && !scriptInstalled {
		// No interpreter and no console script: MLX cannot be present. Absence
		// of an optional runtime is a normal state (DCI-104).
		return nil, nil
	}

	base := cognition.LocalEndpoint(AdapterID+":runtime", AdapterID, "")
	base.Health = protocol.EndpointHealthInstalled
	base.ObservedAt = in.ObservedAt
	base.Provider = "mlx"

	if !in.Depth.AtLeast(protocol.DepthHealth) {
		base.Findings = append(base.Findings, finding(base.ID, "discovery", protocol.FindingUnsupported,
			"probe depth "+string(in.Depth)+" does not permit running the interpreter"))
		return []protocol.CognitionEndpoint{base}, nil
	}

	outcome := a.commands.Run(ctx, environment.ProbeCommand{
		Name:       "mlx-introspect",
		Executable: a.interpreter,
		Args:       []string{"-c", introspectProgram},
		Timeout:    a.healthTimeout,
	})
	if !outcome.Succeeded() {
		// A console script on PATH with no importable package is a stale
		// install: installed, and not usable.
		base.Health = protocol.EndpointHealthNotConfigured
		base.Findings = append(base.Findings, finding(base.ID, "runtime", outcome.Status,
			"the interpreter did not answer the mlx introspection probe: "+outcome.Detail))
		return []protocol.CognitionEndpoint{base}, nil
	}
	var info introspection
	if err := json.Unmarshal([]byte(lastJSONLine(outcome.Stdout)), &info); err != nil {
		base.Health = protocol.EndpointHealthUnhealthy
		base.Findings = append(base.Findings, finding(base.ID, "runtime", protocol.FindingMalformed,
			"the introspection probe did not emit parsable JSON"))
		return []protocol.CognitionEndpoint{base}, nil
	}
	if info.Error != "" {
		base.Health = protocol.EndpointHealthNotInstalled
		base.Findings = append(base.Findings, finding(base.ID, "runtime", protocol.FindingAbsent,
			"mlx is not importable: "+info.Error))
		return []protocol.CognitionEndpoint{base}, nil
	}
	if info.MLXLMVersion == nil {
		// mlx.core without mlx_lm: the array framework is present but the
		// language-model layer this adapter drives is not.
		base.Health = protocol.EndpointHealthNotConfigured
		base.Findings = append(base.Findings, finding(base.ID, "runtime", protocol.FindingAbsent,
			"mlx is importable but mlx_lm is not: "+info.MLXLMError))
		return []protocol.CognitionEndpoint{base}, nil
	}
	base.Version = versionString(info)
	// Native architecture matters: an MLX install driven by a translated
	// interpreter is not the Apple Silicon path it appears to be.
	if native := in.Facts.Virtualization.NativeArchitecture; native != nil && !*native {
		base.Findings = append(base.Findings, finding(base.ID, "architecture", protocol.FindingObserved,
			"the interpreter is running translated, which is not a native apple silicon execution path"))
	}

	models := a.cachedModels(ctx, base.ID, &base)
	if len(models) == 0 {
		base.Health = protocol.EndpointHealthNotConfigured
		base.Findings = append(base.Findings, finding(base.ID, "models", protocol.FindingAbsent,
			"mlx_lm is importable but no locally cached model was found; "+
				"acceleration cannot be verified without one, and this adapter will not download weights"))
		return []protocol.CognitionEndpoint{base}, nil
	}

	endpoints := make([]protocol.CognitionEndpoint, 0, len(models))
	for _, model := range models {
		endpoint := cognition.LocalEndpoint(AdapterID+":"+model.id, AdapterID, model.reference)
		endpoint.Provider = "mlx"
		endpoint.Version = base.Version
		endpoint.ObservedAt = in.ObservedAt
		one := 1
		endpoint.MaxConcurrentSessions = &one
		endpoint.Health = protocol.EndpointHealthUnverified
		endpoint.Findings = append(endpoint.Findings, finding(endpoint.ID, "health", protocol.FindingObserved,
			"mlx_lm is importable and this model is cached locally; generation is unverified until probed"))
		if info.DefaultDevice != "" {
			endpoint.Findings = append(endpoint.Findings, finding(endpoint.ID, "device",
				protocol.FindingObserved, "runtime default device is "+sanitize(info.DefaultDevice, 64)))
		}
		endpoints = append(endpoints, endpoint)
	}
	return endpoints, nil
}

// cachedModel is one locally cached model.
type cachedModel struct {
	// id is the endpoint-safe handle.
	id string
	// reference is what mlx_lm.load accepts.
	reference string
}

// cachedModels lists MLX models already in the Hugging Face cache.
//
// Only already-present models are considered. Passing a bare repository name to
// mlx_lm.load would make it *download* the weights, which is a multi-gigabyte
// mutation requiring explicit approval (DCI-108) — so the adapter resolves a
// local snapshot directory and probes that, or probes nothing.
func (a *Adapter) cachedModels(_ context.Context, endpointID string, base *protocol.CognitionEndpoint) []cachedModel {
	if a.sys == nil || a.homeDir == "" {
		base.Findings = append(base.Findings, finding(endpointID, "models", protocol.FindingUnsupported,
			"no filesystem probe or home directory was configured, so the model cache was not inspected"))
		return nil
	}
	hub := path.Join(a.homeDir, ".cache", "huggingface", "hub")
	entries, err := a.sys.ReadDir(hub)
	if err != nil {
		base.Findings = append(base.Findings, finding(endpointID, "models", classify(err),
			"the hugging face cache at "+hub+" could not be listed"))
		return nil
	}
	var out []cachedModel
	for _, entry := range entries {
		if !strings.HasPrefix(entry, "models--") {
			continue
		}
		// An MLX model directory is only usable if a snapshot was actually
		// materialised; a cache entry with no snapshot is an interrupted
		// download, not a model.
		snapshotRoot := path.Join(hub, entry, "snapshots")
		snapshots, err := a.sys.ReadDir(snapshotRoot)
		if err != nil || len(snapshots) == 0 {
			continue
		}
		reference := strings.ReplaceAll(strings.TrimPrefix(entry, "models--"), "--", "/")
		if !looksLikeMLXModel(reference) {
			continue
		}
		out = append(out, cachedModel{
			id:        sanitize(reference, 96),
			reference: path.Join(snapshotRoot, snapshots[len(snapshots)-1]),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	if len(out) > MaxEndpoints {
		out = out[:MaxEndpoints]
	}
	return out
}

// looksLikeMLXModel keeps non-MLX cache entries out of the endpoint list.
//
// The cache holds every Hugging Face artifact the user has ever pulled,
// including models MLX cannot load. The naming convention MLX conversions use is
// the available signal; a false negative costs an endpoint that could have been
// probed, which is better than an endpoint whose every probe fails.
func looksLikeMLXModel(reference string) bool {
	lowered := strings.ToLower(reference)
	return strings.Contains(lowered, "mlx")
}

// Probe implements cognition.Adapter.
func (a *Adapter) Probe(
	ctx context.Context,
	endpoint protocol.CognitionEndpoint,
	req cognition.ProbeRequest,
) (cognition.ProbeResult, error) {
	req = req.Normalise()
	result := cognition.ProbeResult{Status: protocol.FindingError, EvidenceMediaType: "application/json"}
	if endpoint.ModelID == "" {
		result.Status = protocol.FindingAbsent
		result.Detail = "this endpoint has no locally cached model to probe"
		return result, nil
	}
	// The model reference reaches the interpreter as its own argv element, so
	// it cannot be misread as code. It is still validated: a path-shaped value
	// is the only thing this adapter is willing to load, because a bare
	// repository name would trigger a download.
	if !strings.HasPrefix(endpoint.ModelID, "/") {
		return result, errs.New(errs.CategoryInvalidArgument,
			"mlx: endpoint %s names a non-path model reference; only an already-downloaded snapshot may be probed",
			endpoint.ID)
	}

	outcome := a.commands.Run(ctx, environment.ProbeCommand{
		Name:       "mlx-generate",
		Executable: a.interpreter,
		Args: []string{
			"-c", generateProgram,
			endpoint.ModelID, req.Prompt, itoa(req.MaxTokens),
		},
		Timeout: req.Timeout,
	})
	result.RawEvidence = []byte(outcome.Stdout)
	switch outcome.Status {
	case protocol.FindingTimeout:
		result.Status = protocol.FindingTimeout
		result.Detail = "inference probe exceeded its time bound"
		return result, nil
	case protocol.FindingAbsent:
		result.Status = protocol.FindingAbsent
		result.Detail = "the interpreter could not be resolved"
		return result, nil
	}
	if !outcome.Succeeded() {
		result.Status = protocol.FindingError
		result.Detail = "the generation probe failed: " + outcome.Detail
		return result, nil
	}
	var generated generation
	if err := json.Unmarshal([]byte(lastJSONLine(outcome.Stdout)), &generated); err != nil {
		result.Status = protocol.FindingMalformed
		result.Detail = "the generation probe did not emit parsable JSON"
		return result, nil
	}
	if generated.Error != "" {
		result.Status = protocol.FindingError
		result.Detail = sanitize(generated.Error, 512)
		return result, nil
	}

	result.Status = protocol.FindingObserved
	result.RuntimeVersion = endpoint.Version
	result.ModelID = endpoint.ModelID
	result.Measurements = measurements(generated)
	if req.RequireStructuredOutput {
		result.StructuredOutput = structuredOutcome(generated.Text)
	}
	result.Backend, result.Signals = accelerationSignals(generated)
	if info := sanitize(generated.DefaultDevice, 64); info != "" {
		result.DeviceID = info
	}
	return result, nil
}

// accelerationSignals derives acceleration evidence from the generating process.
//
// The device statement is authoritative because it was read in the same process,
// after the same generation: the runtime is reporting where it just ran. Metal
// availability is only corroborating — it says the backend exists, not that it
// was used — which is why a machine with Metal available but a CPU default
// device yields a failed rather than a verified result.
func accelerationSignals(generated generation) (protocol.BackendKind, []protocol.AccelerationSignal) {
	device := strings.ToLower(generated.DefaultDevice)
	var signals []protocol.AccelerationSignal
	switch {
	case device == "":
		// Generation worked and the device could not be read. No signal, and
		// therefore an unverified result.
		return protocol.BackendUnknown, nil
	case strings.Contains(device, "gpu"):
		signals = append(signals, protocol.AccelerationSignal{
			Source: "mlx:default_device", Trust: protocol.TrustAuthoritative,
			Backend: protocol.BackendMetal, Offloaded: true,
			Statement: "the generating process reported default device " + sanitize(generated.DefaultDevice, 64),
		})
		if generated.MetalAvailable != nil {
			signals = append(signals, protocol.AccelerationSignal{
				Source: "mlx:metal.is_available", Trust: protocol.TrustCorroborating,
				Backend: protocol.BackendMetal, Offloaded: *generated.MetalAvailable,
				Statement: "metal availability reported as " + boolString(*generated.MetalAvailable),
			})
		}
		return protocol.BackendMetal, signals
	default:
		signals = append(signals, protocol.AccelerationSignal{
			Source: "mlx:default_device", Trust: protocol.TrustAuthoritative,
			Backend: protocol.BackendCPU, Offloaded: false,
			Statement: "the generating process reported default device " + sanitize(generated.DefaultDevice, 64),
		})
		return protocol.BackendMetal, signals
	}
}

// measurements records only what the probe actually timed.
//
// MLX-LM's returned value is text; it carries no token counts this adapter can
// rely on across versions. Durations and peak memory are therefore reported and
// throughput is not — deriving a token rate from character counts would be a
// fabricated measurement.
func measurements(generated generation) []protocol.Measurement {
	var out []protocol.Measurement
	if generated.LoadSeconds != nil {
		out = append(out, protocol.Measurement{
			Name: "load_duration_ms", Value: *generated.LoadSeconds * 1000,
			Unit: "ms", Source: "mlx:load",
		})
	}
	if generated.GenerateSeconds != nil {
		out = append(out, protocol.Measurement{
			Name: "request_duration_ms", Value: *generated.GenerateSeconds * 1000,
			Unit: "ms", Source: "mlx:generate",
		})
	}
	if generated.PeakMemoryBytes != nil {
		out = append(out, protocol.Measurement{
			Name: "peak_memory_bytes", Value: float64(*generated.PeakMemoryBytes),
			Unit: "bytes", Source: "mlx:get_peak_memory",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func structuredOutcome(text string) protocol.FeatureSupport {
	var decoded struct {
		OK *bool `json:"ok"`
	}
	trimmed := strings.TrimSpace(text)
	// A model often wraps JSON in prose; the first balanced object is what the
	// probe judges, because that is what a repair wrapper would extract.
	if start := strings.Index(trimmed, "{"); start >= 0 {
		if end := strings.LastIndex(trimmed, "}"); end > start {
			trimmed = trimmed[start : end+1]
		}
	}
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil || decoded.OK == nil {
		return protocol.FeatureProbeFailed
	}
	return protocol.FeatureProbePassed
}

func versionString(info introspection) string {
	parts := make([]string, 0, 2)
	if info.MLXLMVersion != nil && *info.MLXLMVersion != "" {
		parts = append(parts, "mlx-lm "+sanitize(*info.MLXLMVersion, 32))
	}
	if info.MLXVersion != nil && *info.MLXVersion != "" {
		parts = append(parts, "mlx "+sanitize(*info.MLXVersion, 32))
	}
	return strings.Join(parts, ", ")
}

// lastJSONLine returns the final non-empty line of output.
//
// A Python process can emit warnings on stdout before the program's own output;
// taking the last line is how the adapter reads its own result rather than a
// deprecation notice.
func lastJSONLine(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "{") {
			return line
		}
	}
	return ""
}

func softwarePresence(facts protocol.EnvironmentFacts, id string) (bool, protocol.SoftwarePresence) {
	for _, entry := range facts.Software {
		if entry.ID == id {
			return entry.Installed, entry
		}
	}
	return false, protocol.SoftwarePresence{}
}

func finding(endpointID, aspect string, status protocol.FindingStatus, detail string) protocol.DiscoveryFinding {
	return protocol.DiscoveryFinding{
		Component: "endpoint." + endpointID + "." + aspect,
		Status:    status,
		Source:    AdapterID,
		Detail:    sanitize(detail, 1024),
	}
}

func classify(err error) protocol.FindingStatus {
	switch errs.CategoryOf(err) {
	case errs.CategoryNotFound:
		return protocol.FindingAbsent
	case errs.CategoryUnsupported:
		return protocol.FindingUnsupported
	default:
		return protocol.FindingError
	}
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// sanitize bounds and de-fangs text taken from the interpreter.
func sanitize(value string, max int) string {
	var b strings.Builder
	for _, r := range value {
		if b.Len() >= max {
			break
		}
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}
