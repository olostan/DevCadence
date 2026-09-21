// Package ollama adapts the Ollama local runtime to the CognitionEndpoint
// contract.
//
// The adapter talks to Ollama's documented HTTP API and nothing else: no
// provider SDK, no client library, no shelling out to parse human-readable CLI
// output. The endpoints it uses are:
//
//	GET  /api/version   runtime version
//	GET  /api/tags      locally available models
//	GET  /api/ps        models currently resident, with size and size_vram
//	POST /api/generate  completion, with `format` for structured output and
//	                    total_duration / load_duration / prompt_eval_count /
//	                    prompt_eval_duration / eval_count / eval_duration in
//	                    the final response object
//
// `/api/ps` is what makes acceleration verifiable rather than assumed. It
// reports, for a resident model, both its total size and how much of it is in
// VRAM. A model with size_vram equal to zero is executing on the CPU no matter
// what GPU the machine has, and a model with size_vram greater than zero has
// been offloaded — reported by the runtime that just performed the inference,
// which is the authoritative signal DCI-106 asks for.
//
// What this adapter will not do: install Ollama, start the server, pull a model,
// or change any configuration. "Runtime installed, server healthy, no model
// available" is a valid state it reports rather than a failure it escalates.
package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/olostan/DevCadience/internal/cognition"
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/protocol"
)

// AdapterID is the stable adapter identity and the prefix of its endpoint ids.
const AdapterID = "ollama"

// DefaultBaseURL is Ollama's documented default listen address.
//
// Loopback only. An adapter that silently accepted a remote address would turn
// a local-runtime endpoint into an undeclared network egress path, and the
// locality and source-exposure classes attached to it would then be wrong
// (docs/SECURITY.md §8).
const DefaultBaseURL = "http://127.0.0.1:11434"

// MaxResponseBytes bounds a response body.
//
// Provider output is untrusted input (DCI-083): a runtime that streams
// unexpectedly, or a process that is not Ollama listening on the port, must not
// be able to exhaust memory.
const MaxResponseBytes = 4 << 20 // 4 MiB

// MaxEndpointsPerRuntime bounds how many models become endpoints.
//
// A developer machine can hold dozens of pulled models, and one endpoint each
// would make every profile unreadable and every routing decision noisy. The
// alphabetically first models are taken so the selection is deterministic.
const MaxEndpointsPerRuntime = 8

// Transport performs one HTTP request against the runtime.
//
// It exists so the adapter is testable without a server and without net/http in
// the test path: the ordinary suite must not require an installed Ollama or any
// listening socket.
type Transport interface {
	// Do issues a request and returns the status code and bounded body. It
	// returns an error only when no response was obtained.
	Do(ctx context.Context, method, path string, body []byte) (int, []byte, error)
}

// HTTPTransport is the real Transport.
type HTTPTransport struct {
	BaseURL string
	Client  *http.Client
}

// NewHTTPTransport returns a Transport for a local Ollama server.
//
// baseURL empty selects OLLAMA_HOST when set and DefaultBaseURL otherwise,
// matching the runtime's own convention so that a user who moved the port does
// not have to configure DevCadience twice.
func NewHTTPTransport(baseURL string) (*HTTPTransport, error) {
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("OLLAMA_HOST"))
		if baseURL != "" && !strings.Contains(baseURL, "://") {
			// OLLAMA_HOST is documented as host:port; normalise it.
			baseURL = "http://" + baseURL
		}
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if err := requireLoopback(baseURL); err != nil {
		return nil, err
	}
	return &HTTPTransport{
		BaseURL: strings.TrimSuffix(baseURL, "/"),
		// No timeout on the client: every call carries a context deadline, and
		// a client-level timeout would cut a legitimate cold model load short
		// in a way the caller could not extend.
		Client: &http.Client{},
	}, nil
}

// requireLoopback refuses a non-local runtime address.
func requireLoopback(baseURL string) error {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(baseURL, "http://"), "https://")
	host, _, err := net.SplitHostPort(trimmed)
	if err != nil {
		host = strings.TrimSuffix(trimmed, "/")
	}
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1", "[::1]":
		return nil
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil && ip.IsLoopback() {
		return nil
	}
	return errs.New(errs.CategoryPolicyDenied,
		"ollama: %q is not a loopback address; a remote inference worker needs its own threat model "+
			"(docs/MODEL_RUNTIME.md §21) and is not a local_runtime endpoint", baseURL)
}

// Do implements Transport.
func (t *HTTPTransport) Do(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	var reader io.Reader
	if len(body) > 0 {
		reader = strings.NewReader(string(body))
	}
	request, err := http.NewRequestWithContext(ctx, method, t.BaseURL+path, reader)
	if err != nil {
		return 0, nil, errs.Wrap(errs.CategoryInvalidArgument, err, "ollama: build request for %s", path)
	}
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := t.Client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return 0, nil, errs.Wrap(errs.CategoryProbeTimeout, err, "ollama: %s %s", method, path)
		}
		return 0, nil, errs.Wrap(errs.CategoryModelUnavailable, err,
			"ollama: %s %s (is the server running?)", method, path)
	}
	defer func() { _ = response.Body.Close() }()
	payload, err := io.ReadAll(io.LimitReader(response.Body, MaxResponseBytes))
	if err != nil {
		return response.StatusCode, nil, errs.Wrap(errs.CategoryProbeFailed, err, "ollama: read %s", path)
	}
	return response.StatusCode, payload, nil
}

// Options configures an Adapter.
type Options struct {
	Transport Transport
	// HealthTimeout bounds the cheap version/tags/ps calls.
	HealthTimeout time.Duration
}

// Adapter is the Ollama CognitionEndpoint adapter.
type Adapter struct {
	transport     Transport
	healthTimeout time.Duration
}

// New returns an Adapter.
func New(opts Options) (*Adapter, error) {
	if opts.Transport == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "ollama: a Transport is required")
	}
	timeout := opts.HealthTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Adapter{transport: opts.Transport, healthTimeout: timeout}, nil
}

// ID implements cognition.Adapter.
func (a *Adapter) ID() string { return AdapterID }

// versionResponse is /api/version.
type versionResponse struct {
	Version string `json:"version"`
}

// modelEntry is one entry of /api/tags.
type modelEntry struct {
	Name    string `json:"name"`
	Model   string `json:"model"`
	Size    int64  `json:"size"`
	Details struct {
		Family        string `json:"family"`
		ParameterSize string `json:"parameter_size"`
	} `json:"details"`
}

type tagsResponse struct {
	Models []modelEntry `json:"models"`
}

// residentModel is one entry of /api/ps.
//
// SizeVRAM is the field the whole acceleration verification rests on.
type residentModel struct {
	Name     string `json:"name"`
	Model    string `json:"model"`
	Size     int64  `json:"size"`
	SizeVRAM int64  `json:"size_vram"`
}

type psResponse struct {
	Models []residentModel `json:"models"`
}

// generateResponse is the non-streaming /api/generate response.
//
// Durations are nanoseconds and counts are tokens, both as reported by the
// runtime. They are the only source of throughput figures this adapter will
// use: a rate derived from response length would be a fabrication.
type generateResponse struct {
	Model              string `json:"model"`
	Response           string `json:"response"`
	Done               bool   `json:"done"`
	TotalDuration      int64  `json:"total_duration"`
	LoadDuration       int64  `json:"load_duration"`
	PromptEvalCount    int    `json:"prompt_eval_count"`
	PromptEvalDuration int64  `json:"prompt_eval_duration"`
	EvalCount          int    `json:"eval_count"`
	EvalDuration       int64  `json:"eval_duration"`
}

// Discover implements cognition.Adapter.
//
// It reports what it found and never more. The chain installed -> serving ->
// has a model is walked explicitly, and the endpoint's health names exactly how
// far it got.
func (a *Adapter) Discover(ctx context.Context, in cognition.DiscoveryInput) ([]protocol.CognitionEndpoint, error) {
	installed, presence := softwarePresence(in.Facts, "ollama")
	if !installed {
		// Not an error. An absent optional runtime is a normal machine state,
		// already recorded in the software inventory (DCI-104).
		return nil, nil
	}

	base := cognition.LocalEndpoint(AdapterID+":runtime", AdapterID, "")
	base.Health = protocol.EndpointHealthInstalled
	base.Version = presence.Version
	base.ObservedAt = in.ObservedAt
	// Ollama's HTTP API supports a JSON schema in `format`, so structured output
	// is a declared capability until a probe confirms it here.
	base.StructuredOutput = protocol.FeatureDeclared

	if !in.Depth.AtLeast(protocol.DepthHealth) {
		base.Findings = append(base.Findings, finding(base.ID, "discovery",
			protocol.FindingUnsupported,
			"probe depth "+string(in.Depth)+" does not permit contacting the runtime"))
		return []protocol.CognitionEndpoint{base}, nil
	}

	healthCtx, cancel := context.WithTimeout(ctx, a.healthTimeout)
	defer cancel()

	var version versionResponse
	if err := a.getJSON(healthCtx, "/api/version", &version); err != nil {
		base.Health = protocol.EndpointHealthUnhealthy
		base.Findings = append(base.Findings, finding(base.ID, "server",
			findingFor(err), "the runtime is installed but its server did not answer: "+err.Error()))
		return []protocol.CognitionEndpoint{base}, nil
	}
	if version.Version != "" {
		base.Version = sanitize(version.Version, 64)
	}

	var tags tagsResponse
	if err := a.getJSON(healthCtx, "/api/tags", &tags); err != nil {
		base.Health = protocol.EndpointHealthUnhealthy
		base.Findings = append(base.Findings, finding(base.ID, "models",
			findingFor(err), "the server answered /api/version but not /api/tags: "+err.Error()))
		return []protocol.CognitionEndpoint{base}, nil
	}
	models := usableModelNames(tags)
	if len(models) == 0 {
		// A healthy runtime with no model is a real, common and entirely valid
		// state. It is not a setup failure, and turning it into one would make
		// a freshly installed Ollama look broken.
		base.Health = protocol.EndpointHealthNotConfigured
		base.Findings = append(base.Findings, finding(base.ID, "models",
			protocol.FindingAbsent,
			"the server is healthy but no model is available locally; acceleration cannot be verified without one"))
		return []protocol.CognitionEndpoint{base}, nil
	}

	endpoints := make([]protocol.CognitionEndpoint, 0, len(models))
	for _, model := range models {
		endpoint := cognition.LocalEndpoint(AdapterID+":"+model, AdapterID, model)
		endpoint.Version = base.Version
		endpoint.ObservedAt = in.ObservedAt
		endpoint.StructuredOutput = protocol.FeatureDeclared
		// Serving one model at a time is the runtime's normal mode, and
		// concurrent sessions compete for the same accelerator memory
		// (ENGINEERING_STANDARDS.md §20).
		one := 1
		endpoint.MaxConcurrentSessions = &one
		endpoint.ModelFamily = familyOf(tags, model)
		// Health stops at unverified. The server is serving and the model
		// exists, and neither fact establishes that this model loads and
		// generates — only a probe does.
		endpoint.Health = protocol.EndpointHealthUnverified
		endpoint.Findings = append(endpoint.Findings, finding(endpoint.ID, "health",
			protocol.FindingObserved,
			"the runtime is serving and this model is available locally; generation is unverified until probed"))
		endpoints = append(endpoints, endpoint)
	}
	return endpoints, nil
}

// Probe implements cognition.Adapter.
//
// The sequence is: generate, then read residency. Reading /api/ps *after*
// generation is deliberate — the model is loaded at that point, so its VRAM
// residency describes the inference that just happened rather than whatever was
// resident beforehand.
func (a *Adapter) Probe(
	ctx context.Context,
	endpoint protocol.CognitionEndpoint,
	req cognition.ProbeRequest,
) (cognition.ProbeResult, error) {
	req = req.Normalise()
	result := cognition.ProbeResult{Status: protocol.FindingError, EvidenceMediaType: "application/json"}
	if endpoint.ModelID == "" {
		// Nothing to run. This is the healthy-runtime-no-model case, and it is
		// a fact rather than an error.
		result.Status = protocol.FindingAbsent
		result.Detail = "this endpoint has no model to probe"
		return result, nil
	}

	probeCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()

	body := map[string]any{
		"model":  endpoint.ModelID,
		"prompt": req.Prompt,
		"stream": false,
		// keep_alive is short so the probe does not leave a multi-gigabyte
		// model resident in memory the user did not ask for. It must be long
		// enough that /api/ps still sees it immediately afterwards.
		"keep_alive": "30s",
		"options":    map[string]any{"num_predict": req.MaxTokens, "temperature": 0},
	}
	if req.RequireStructuredOutput {
		// `format` accepts a JSON schema, which is the documented way to ask
		// for structured output rather than hoping prose compliance works.
		var schema any
		if err := json.Unmarshal([]byte(cognition.SyntheticProbeSchema), &schema); err == nil {
			body["format"] = schema
		}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return result, errs.Wrap(errs.CategoryInternal, err, "ollama: encode probe request")
	}

	status, raw, err := a.transport.Do(probeCtx, http.MethodPost, "/api/generate", payload)
	if err != nil {
		if errs.CategoryOf(err) == errs.CategoryProbeTimeout {
			result.Status = protocol.FindingTimeout
			result.Detail = "inference probe exceeded its time bound"
			return result, nil
		}
		return result, err
	}
	result.RawEvidence = raw
	if status != http.StatusOK {
		result.Status = protocol.FindingError
		result.Detail = fmt.Sprintf("/api/generate returned status %d", status)
		return result, nil
	}
	var generated generateResponse
	if err := json.Unmarshal(raw, &generated); err != nil {
		// Malformed provider output is a fact about the runtime, not a crash.
		result.Status = protocol.FindingMalformed
		result.Detail = "the runtime's generate response was not valid JSON"
		return result, nil
	}
	if !generated.Done && generated.Response == "" {
		result.Status = protocol.FindingMalformed
		result.Detail = "the runtime returned neither output nor a completion marker"
		return result, nil
	}

	result.Status = protocol.FindingObserved
	result.RuntimeVersion = endpoint.Version
	result.ModelID = sanitize(generated.Model, 128)
	result.Measurements = measurements(generated)
	if req.RequireStructuredOutput {
		result.StructuredOutput = structuredOutcome(generated.Response)
	}

	// Residency, and therefore the acceleration evidence.
	var resident psResponse
	psCtx, psCancel := context.WithTimeout(ctx, a.healthTimeout)
	defer psCancel()
	if err := a.getJSON(psCtx, "/api/ps", &resident); err != nil {
		// Generation worked but the backend could not be observed. That yields
		// an unverified acceleration result, never an optimistic one.
		result.Detail = appendDetail(result.Detail,
			"inference succeeded but /api/ps did not answer, so the backend could not be observed")
		return result, nil
	}
	entry, found := residencyFor(resident, endpoint.ModelID)
	if !found {
		result.Detail = appendDetail(result.Detail,
			"inference succeeded but the model was no longer resident, so the backend could not be observed")
		return result, nil
	}
	result.Backend, result.Signals = accelerationSignals(entry, endpoint.ModelID)
	return result, nil
}

// accelerationSignals turns a residency entry into acceleration evidence.
//
// The runtime reports how much of the model is in VRAM. That is an authoritative
// statement about where the weights were: a fully CPU-resident model was not
// accelerated, and a VRAM-resident model was offloaded.
//
// The one thing Ollama does not tell us is *which* backend did the offloading.
// The backend name therefore comes from the machine's assessed candidates, and
// when that is ambiguous the signal reports BackendUnknown — observed offload to
// an unnamed device, which is honest, rather than a guessed backend name that
// would look like evidence.
func accelerationSignals(entry residentModel, model string) (protocol.BackendKind, []protocol.AccelerationSignal) {
	if entry.SizeVRAM <= 0 {
		return protocol.BackendCPU, []protocol.AccelerationSignal{{
			Source:    "ollama:/api/ps",
			Trust:     protocol.TrustAuthoritative,
			Backend:   protocol.BackendCPU,
			Offloaded: false,
			Statement: fmt.Sprintf("resident model %s reports size_vram=0 of size=%d, so the weights are in system memory",
				sanitize(model, 64), entry.Size),
		}}
	}
	// Partial offload is still offload, and it is reported as such with the
	// numbers, so an operator can see that only part of the model fitted.
	return protocol.BackendUnknown, []protocol.AccelerationSignal{{
		Source:    "ollama:/api/ps",
		Trust:     protocol.TrustAuthoritative,
		Backend:   protocol.BackendUnknown,
		Offloaded: true,
		Statement: fmt.Sprintf("resident model %s reports size_vram=%d of size=%d",
			sanitize(model, 64), entry.SizeVRAM, entry.Size),
	}}
}

// measurements records only what the runtime actually reported.
//
// Throughput is computed solely from the runtime's own token counts and
// durations. If a count is missing the measurement is omitted rather than
// estimated: a fabricated tokens-per-second figure would be worse than no
// figure, because it would be used.
func measurements(response generateResponse) []protocol.Measurement {
	var out []protocol.Measurement
	if response.TotalDuration > 0 {
		out = append(out, protocol.Measurement{
			Name: "request_duration_ms", Value: nanosToMillis(response.TotalDuration),
			Unit: "ms", Source: "ollama:/api/generate",
		})
	}
	if response.LoadDuration > 0 {
		out = append(out, protocol.Measurement{
			Name: "load_duration_ms", Value: nanosToMillis(response.LoadDuration),
			Unit: "ms", Source: "ollama:/api/generate",
		})
	}
	if response.PromptEvalCount > 0 {
		out = append(out, protocol.Measurement{
			Name: "prompt_tokens", Value: float64(response.PromptEvalCount),
			Unit: "tokens", Source: "ollama:/api/generate",
		})
	}
	if response.EvalCount > 0 {
		out = append(out, protocol.Measurement{
			Name: "generation_tokens", Value: float64(response.EvalCount),
			Unit: "tokens", Source: "ollama:/api/generate",
		})
	}
	if response.PromptEvalCount > 0 && response.PromptEvalDuration > 0 {
		out = append(out, protocol.Measurement{
			Name:  "prompt_tokens_per_second",
			Value: perSecond(response.PromptEvalCount, response.PromptEvalDuration),
			Unit:  "tokens/s", Source: "ollama:/api/generate",
		})
	}
	if response.EvalCount > 0 && response.EvalDuration > 0 {
		out = append(out, protocol.Measurement{
			Name:  "generation_tokens_per_second",
			Value: perSecond(response.EvalCount, response.EvalDuration),
			Unit:  "tokens/s", Source: "ollama:/api/generate",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func nanosToMillis(nanos int64) float64 { return float64(nanos) / 1e6 }

func perSecond(count int, durationNanos int64) float64 {
	return float64(count) / (float64(durationNanos) / 1e9)
}

// structuredOutcome judges whether the structured-output probe passed.
//
// The bar is exactly "valid JSON object carrying the requested key". Passing
// establishes FeatureProbePassed and nothing about reliability under load.
func structuredOutcome(response string) protocol.FeatureSupport {
	var decoded struct {
		OK *bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(response)), &decoded); err != nil {
		return protocol.FeatureProbeFailed
	}
	if decoded.OK == nil {
		return protocol.FeatureProbeFailed
	}
	return protocol.FeatureProbePassed
}

func (a *Adapter) getJSON(ctx context.Context, path string, out any) error {
	status, raw, err := a.transport.Do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return errs.New(errs.CategoryProbeFailed, "ollama: %s returned status %d", path, status)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return errs.Wrap(errs.CategoryProbeFailed, err, "ollama: %s returned malformed JSON", path)
	}
	return nil
}

// usableModelNames returns a bounded, deterministic model list.
func usableModelNames(tags tagsResponse) []string {
	seen := map[string]bool{}
	var names []string
	for _, model := range tags.Models {
		name := model.Name
		if name == "" {
			name = model.Model
		}
		name = sanitize(name, 128)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > MaxEndpointsPerRuntime {
		names = names[:MaxEndpointsPerRuntime]
	}
	return names
}

func familyOf(tags tagsResponse, model string) string {
	for _, entry := range tags.Models {
		if entry.Name == model || entry.Model == model {
			return sanitize(entry.Details.Family, 64)
		}
	}
	return ""
}

// residencyFor finds the resident entry for a model, tolerating the runtime's
// two naming fields.
func residencyFor(resident psResponse, model string) (residentModel, bool) {
	for _, entry := range resident.Models {
		if entry.Name == model || entry.Model == model {
			return entry, true
		}
	}
	// A tag-less name ("qwen3" against "qwen3:latest") is the common mismatch.
	bare, _, _ := strings.Cut(model, ":")
	for _, entry := range resident.Models {
		if strings.HasPrefix(entry.Name, bare+":") || strings.HasPrefix(entry.Model, bare+":") {
			return entry, true
		}
	}
	return residentModel{}, false
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

func findingFor(err error) protocol.FindingStatus {
	switch errs.CategoryOf(err) {
	case errs.CategoryProbeTimeout:
		return protocol.FindingTimeout
	case errs.CategoryModelUnavailable:
		return protocol.FindingAbsent
	default:
		return protocol.FindingError
	}
}

func appendDetail(existing, addition string) string {
	if existing == "" {
		return addition
	}
	return existing + "; " + addition
}

// sanitize bounds and de-fangs text taken from the runtime.
//
// Model names, version strings and error text all originate outside
// DevCadience and end up in durable records and operator terminals, so control
// characters are dropped and length is bounded (DCI-083).
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
