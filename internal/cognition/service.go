package cognition

import (
	"context"
	"sort"
	"strings"

	"github.com/olostan/DevCadience/internal/artifacts"
	"github.com/olostan/DevCadience/internal/clock"
	"github.com/olostan/DevCadience/internal/environment"
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/ids"
	"github.com/olostan/DevCadience/internal/protocol"
)

// Profile assembly: environment facts plus adapter discovery plus operator
// declarations, evaluated into one MachineCapabilityProfile.
//
// Persistence boundary, stated once here because it is a real decision rather
// than an omission. M3A computes the profile on demand and does not store it:
//
//   - it is not appended to the engineering event journal, because endpoint
//     health and available memory change by the minute and burying project
//     history under machine telemetry would damage the journal's purpose;
//   - it is not written to an invented state file, because nothing in M3A reads
//     a profile back and a cache nobody consumes is a staleness bug waiting to
//     happen. Caching belongs to M3B, which has a reason to want it, and
//     MachineFingerprint exists so it can be built correctly;
//   - large raw probe output does go to the M2 content-addressed artifact store
//     when one is supplied, and the profile references it by digest, so the
//     evidence behind a verification claim stays retrievable without inflating
//     the record (DCI-011, ADR-0009).
//
// The one durable, project-scoped form is the compact ProjectState projection in
// projection.go.

// Declaration is operator-supplied configuration about one endpoint.
//
// It exists because an operator legitimately knows things DevCadience cannot
// measure — that a particular remote CLI is a strong implementer, that a project
// may only send semantic evidence. Every capability it supplies is recorded with
// provenance "configured", never "measured", so a routing decision can always be
// traced back to the person who declared it (DCI-005).
type Declaration struct {
	EndpointID string
	// Capabilities are graded claims. Their provenance is overwritten with
	// ProvenanceConfigured, so a declaration cannot masquerade as a probe
	// result.
	Capabilities []protocol.GradedCapability
	// CostClass and RequiredSourceExposure override the adapter's defaults,
	// which is how a subscription-backed CLI or a restricted endpoint gets its
	// real class.
	CostClass              protocol.CostClass
	RequiredSourceExposure protocol.SourceExposure
	ContextTokens          *int
	MaxConcurrentSessions  *int
	// CredentialRef is an opaque reference. A raw secret here would be a
	// durable credential leak, so the service rejects anything that looks like
	// one.
	CredentialRef string
	// AccountRef is an opaque non-secret account handle, retained so M6 can
	// later avoid treating two frontends over one account as independent.
	AccountRef string
}

// Options configures a Service.
type Options struct {
	// Adapters are consulted in order. Each is isolated: one adapter failing
	// contributes a finding and leaves the others' endpoints intact (DCI-104).
	Adapters []Adapter
	Clock    clock.Clock
	IDs      ids.Source
	// Artifacts is optional. Without it, raw probe evidence is not retained
	// and the profile says so rather than silently dropping it.
	Artifacts *artifacts.Store
	// ArtifactProject scopes stored probe evidence. The machine profile is not
	// project-scoped, but the artifact store is, so evidence is filed under an
	// explicit owner.
	ArtifactProject string
}

// Service assembles machine capability profiles.
type Service struct {
	adapters        []Adapter
	clock           clock.Clock
	ids             ids.Source
	artifacts       *artifacts.Store
	artifactProject string
}

// NewService returns a Service.
func NewService(opts Options) (*Service, error) {
	if opts.Clock == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "cognition: a Clock is required")
	}
	if opts.IDs == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "cognition: an ID source is required")
	}
	if opts.Artifacts != nil && opts.ArtifactProject == "" {
		return nil, errs.New(errs.CategoryInvalidArgument,
			"cognition: an artifact store requires an ArtifactProject to file evidence under")
	}
	return &Service{
		adapters:        append([]Adapter(nil), opts.Adapters...),
		clock:           opts.Clock,
		ids:             opts.IDs,
		artifacts:       opts.Artifacts,
		artifactProject: opts.ArtifactProject,
	}, nil
}

// ProfileInput is what one profile pass is given.
type ProfileInput struct {
	Facts protocol.EnvironmentFacts
	// Depth bounds the work. Only DepthInference permits running a model, and
	// only then can a backend become verified.
	Depth protocol.ProbeDepth
	// Declarations are operator configuration, keyed by endpoint id.
	Declarations []Declaration
	// Probe requests, applied at inference depth. Zero values are normalised.
	Probe ProbeRequest
	// InferenceTargets names the endpoints the caller authorises an inference
	// probe against. It is the whole authorisation for spending a model call.
	//
	// Inference is never implicit and never fans out. An inference probe loads a
	// model, occupies a GPU, and on an authenticated coding CLI spends the user's
	// real subscription quota, so "discover the machine" must never be able to
	// turn into "call every provider the user has installed". The field is
	// therefore mandatory at DepthInference and forbidden below it: a caller that
	// wants inference has to name, one id at a time, what it is willing to pay
	// for. An endpoint not named here is discovered and health-checked exactly as
	// it would be at DepthHealth, and never invoked.
	InferenceTargets []string
}

// Profile discovers endpoints and assesses the machine.
//
// Discovery and probing are sequential. Parallelising them would be easy and
// wrong: two inference probes running at once compete for the same GPU memory,
// which can make both fall back to CPU and produce a *worse* acceleration
// answer than running them one at a time. Discovery is fast enough that
// concurrency would buy nothing while costing deterministic ordering.
//
// At DepthInference the caller must name InferenceTargets; see that field. Most
// callers want ProbeEndpoint, which is the single-endpoint form.
func (s *Service) Profile(ctx context.Context, in ProfileInput) (protocol.MachineCapabilityProfile, error) {
	if in.Depth == "" {
		in.Depth = protocol.DepthHealth
	}
	if !in.Depth.Valid() {
		return protocol.MachineCapabilityProfile{},
			errs.New(errs.CategoryInvalidArgument, "cognition: unknown probe depth %q", in.Depth)
	}
	if err := s.validateInferenceTargets(in); err != nil {
		return protocol.MachineCapabilityProfile{}, err
	}
	if err := s.validateDeclarations(in.Declarations); err != nil {
		return protocol.MachineCapabilityProfile{}, err
	}

	observedAt := protocol.NewTimestamp(s.clock.Now())
	candidates := environment.AssessBackends(in.Facts)
	fingerprint, err := environment.Fingerprint(in.Facts)
	if err != nil {
		return protocol.MachineCapabilityProfile{}, err
	}

	facts := in.Facts
	var endpoints []protocol.CognitionEndpoint
	for _, adapter := range s.adapters {
		discovered, err := adapter.Discover(ctx, DiscoveryInput{
			Facts: in.Facts, Candidates: candidates, Depth: in.Depth, ObservedAt: observedAt,
			InferenceTargets: in.InferenceTargets,
		})
		if err != nil {
			// An adapter that could not run is a finding about that adapter.
			// Its neighbours keep their endpoints: MLX being absent must not
			// cost us Ollama, and an unauthenticated CLI must not cost us the
			// local runtime (DCI-104).
			facts.Findings = append(facts.Findings, protocol.DiscoveryFinding{
				Component: "cognition.adapter." + adapter.ID(),
				Status:    findingStatusFor(err),
				Source:    adapter.ID(),
				Detail:    truncate(err.Error(), 1024),
			})
			continue
		}
		endpoints = append(endpoints, discovered...)
	}

	declarations := indexDeclarations(in.Declarations)
	for i := range endpoints {
		s.applyDeclaration(&endpoints[i], declarations[endpoints[i].ID])
	}

	if in.Depth.AtLeast(protocol.DepthInference) {
		endpoints = s.probeEndpoints(ctx, endpoints, candidates, in.Probe, in.InferenceTargets)
	}

	sort.Slice(endpoints, func(i, j int) bool { return endpoints[i].ID < endpoints[j].ID })
	sort.SliceStable(facts.Findings, func(i, j int) bool {
		if facts.Findings[i].Component != facts.Findings[j].Component {
			return facts.Findings[i].Component < facts.Findings[j].Component
		}
		return facts.Findings[i].Status < facts.Findings[j].Status
	})

	// Candidate states are lifted by verified endpoint evidence, so that
	// "which backends work on this machine" and "which endpoint proved it"
	// stay consistent.
	candidates = reconcileCandidates(candidates, endpoints)

	profile := protocol.MachineCapabilityProfile{
		SchemaVersion:         protocol.SchemaVersion1,
		ProfileID:             s.ids.New("mcp"),
		MachineFingerprint:    fingerprint,
		ObservedAt:            observedAt,
		KnowledgeRevision:     environment.KnowledgeRevision,
		ProbeDepth:            in.Depth,
		Environment:           facts,
		AcceleratorCandidates: candidates,
		Endpoints:             endpoints,
	}
	profile.Assessment, profile.Limitations = Assess(profile)
	if err := profile.Validate(); err != nil {
		return protocol.MachineCapabilityProfile{}, errs.Wrap(errs.CategoryInternal, err,
			"cognition: assembled a profile that violates the contract")
	}
	return profile, nil
}

// ProbeEndpointInput asks for one endpoint to be verified by inference.
type ProbeEndpointInput struct {
	Facts protocol.EnvironmentFacts
	// EndpointID is the single endpoint that may be invoked. It is required.
	EndpointID string
	// Declarations are operator configuration, keyed by endpoint id.
	Declarations []Declaration
	// Probe requests. Zero values are normalised.
	Probe ProbeRequest
}

// ProbeEndpoint discovers the machine cheaply and runs an inference probe
// against exactly one endpoint.
//
// This is the only path in M3A that spends a model call, and it exists as its
// own operation rather than as a depth flag because the two halves of the work
// have completely different costs. Discovery and health are cheap, local and
// safe to run on everything; inference is expensive, occupies a device and can
// spend a paid quota, so it happens once, against an endpoint the caller named.
//
// Every other endpoint is still discovered and health-checked — that is what
// makes the returned profile a usable answer and lets a mistyped id be reported
// with the available ones — but none of them is invoked.
func (s *Service) ProbeEndpoint(
	ctx context.Context,
	in ProbeEndpointInput,
) (protocol.MachineCapabilityProfile, protocol.CognitionEndpoint, error) {
	if in.EndpointID == "" {
		return protocol.MachineCapabilityProfile{}, protocol.CognitionEndpoint{},
			errs.New(errs.CategoryInvalidArgument, "cognition: an endpoint id is required to probe")
	}
	profile, err := s.Profile(ctx, ProfileInput{
		Facts:            in.Facts,
		Depth:            protocol.DepthInference,
		Declarations:     in.Declarations,
		Probe:            in.Probe,
		InferenceTargets: []string{in.EndpointID},
	})
	if err != nil {
		return protocol.MachineCapabilityProfile{}, protocol.CognitionEndpoint{}, err
	}
	available := make([]string, 0, len(profile.Endpoints))
	for _, endpoint := range profile.Endpoints {
		if endpoint.ID == in.EndpointID {
			return profile, endpoint, nil
		}
		available = append(available, endpoint.ID)
	}
	detail := "none were discovered"
	if len(available) > 0 {
		detail = "available: " + strings.Join(available, ", ")
	}
	return protocol.MachineCapabilityProfile{}, protocol.CognitionEndpoint{},
		errs.New(errs.CategoryNotFound, "cognition: no endpoint %q was discovered; %s", in.EndpointID, detail)
}

// validateInferenceTargets keeps inference authorisation explicit in both
// directions.
//
// A missing target at inference depth is the fan-out bug: it would mean "probe
// everything". A target named below inference depth is the opposite mistake — a
// caller that believes it asked for verification and silently did not get it. Both
// are refused rather than interpreted.
func (s *Service) validateInferenceTargets(in ProfileInput) error {
	if in.Depth.AtLeast(protocol.DepthInference) {
		if len(in.InferenceTargets) == 0 {
			return errs.New(errs.CategoryInvalidArgument,
				"cognition: probe depth %q requires naming the endpoints it may invoke; "+
					"inference is never run across every discovered endpoint", in.Depth)
		}
		for _, target := range in.InferenceTargets {
			if target == "" {
				return errs.New(errs.CategoryInvalidArgument,
					"cognition: an empty endpoint id cannot authorise an inference probe")
			}
		}
		return nil
	}
	if len(in.InferenceTargets) > 0 {
		return errs.New(errs.CategoryInvalidArgument,
			"cognition: probe depth %q does not run inference, so inference targets cannot be honoured",
			in.Depth)
	}
	return nil
}

// validateDeclarations refuses configuration that would corrupt the record.
func (s *Service) validateDeclarations(declarations []Declaration) error {
	for _, declaration := range declarations {
		if declaration.EndpointID == "" {
			return errs.New(errs.CategoryInvalidArgument, "cognition: a declaration requires an endpoint id")
		}
		// A credential reference is a handle. Anything long or secret-shaped is
		// refused outright rather than redacted, because a redaction that ran
		// after the value had already been copied into a record would be too
		// late (DCI-081).
		if looksLikeSecret(declaration.CredentialRef) {
			return errs.New(errs.CategoryInvalidArgument,
				"cognition: credential_ref for %s looks like a secret value; it must be an opaque reference",
				declaration.EndpointID)
		}
		if looksLikeSecret(declaration.AccountRef) {
			return errs.New(errs.CategoryInvalidArgument,
				"cognition: account_ref for %s looks like a secret value; it must be an opaque reference",
				declaration.EndpointID)
		}
		for _, capability := range declaration.Capabilities {
			if !capability.Dimension.Valid() {
				return errs.New(errs.CategoryInvalidArgument,
					"cognition: declaration for %s names unknown capability dimension %q",
					declaration.EndpointID, capability.Dimension)
			}
			if !capability.Grade.Valid() {
				return errs.New(errs.CategoryInvalidArgument,
					"cognition: declaration for %s names unknown capability grade %q",
					declaration.EndpointID, capability.Grade)
			}
		}
	}
	return nil
}

// applyDeclaration layers operator configuration onto a discovered endpoint.
func (s *Service) applyDeclaration(endpoint *protocol.CognitionEndpoint, declaration *Declaration) {
	if declaration == nil {
		return
	}
	for _, capability := range declaration.Capabilities {
		capability.Provenance = protocol.ProvenanceConfigured
		if capability.Source == "" {
			capability.Source = "operator declaration"
		}
		setCapability(endpoint, capability)
	}
	if declaration.CostClass != "" {
		endpoint.CostClass = declaration.CostClass
	}
	if declaration.RequiredSourceExposure != "" {
		endpoint.RequiredSourceExposure = declaration.RequiredSourceExposure
	}
	if declaration.ContextTokens != nil {
		tokens := *declaration.ContextTokens
		endpoint.ContextTokens = &tokens
	}
	if declaration.MaxConcurrentSessions != nil {
		sessions := *declaration.MaxConcurrentSessions
		endpoint.MaxConcurrentSessions = &sessions
	}
	if declaration.CredentialRef != "" {
		endpoint.CredentialRef = declaration.CredentialRef
	}
	if declaration.AccountRef != "" {
		endpoint.AccountRef = declaration.AccountRef
	}
}

// setCapability replaces or appends a graded capability.
//
// A measured grade is never overwritten by a configured one: measurement is the
// stronger provenance, and an operator's optimistic declaration must not erase
// a probe that disagreed.
func setCapability(endpoint *protocol.CognitionEndpoint, capability protocol.GradedCapability) {
	for i, existing := range endpoint.Capabilities {
		if existing.Dimension != capability.Dimension {
			continue
		}
		if provenanceRank(existing.Provenance) > provenanceRank(capability.Provenance) {
			return
		}
		endpoint.Capabilities[i] = capability
		return
	}
	endpoint.Capabilities = append(endpoint.Capabilities, capability)
	sort.Slice(endpoint.Capabilities, func(i, j int) bool {
		return endpoint.Capabilities[i].Dimension < endpoint.Capabilities[j].Dimension
	})
}

func provenanceRank(provenance protocol.CapabilityProvenance) int {
	switch provenance {
	case protocol.ProvenanceEvaluated:
		return 3
	case protocol.ProvenanceMeasured:
		return 2
	case protocol.ProvenanceConfigured:
		return 1
	default:
		return 0
	}
}

// probeEndpoints exercises the authorised endpoints in turn.
//
// Only an endpoint named in targets is invoked. Every other endpoint is left
// exactly as discovery and health left it: not probed is not a failure, and an
// unprobed endpoint keeps its honest unverified state rather than acquiring a
// finding about work nobody asked for.
//
// Execution stays sequential for the reason given on Profile: concurrent probes
// contend for the same device and can each report a CPU fallback that neither
// would report alone.
func (s *Service) probeEndpoints(
	ctx context.Context,
	endpoints []protocol.CognitionEndpoint,
	candidates []protocol.AcceleratorCandidate,
	request ProbeRequest,
	targets []string,
) []protocol.CognitionEndpoint {
	request = request.Normalise()
	authorised := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		authorised[target] = struct{}{}
	}
	byAdapter := map[string]Adapter{}
	for _, adapter := range s.adapters {
		byAdapter[adapter.ID()] = adapter
	}
	for i := range endpoints {
		endpoint := &endpoints[i]
		if _, ok := authorised[endpoint.ID]; !ok {
			continue
		}
		adapter, ok := byAdapter[adapterIDOf(endpoint.ID)]
		if !ok {
			endpoint.Findings = append(endpoint.Findings, protocol.DiscoveryFinding{
				Component: "endpoint." + endpoint.ID + ".probe",
				Status:    protocol.FindingUnsupported,
				Detail:    "no adapter claims this endpoint, so it cannot be probed",
			})
			continue
		}
		// An endpoint that is not even installed is not worth a probe, and
		// probing it would turn a clear absence into a confusing failure.
		if endpoint.Health == protocol.EndpointHealthNotInstalled ||
			endpoint.Health == protocol.EndpointHealthUnsupported {
			continue
		}
		result, err := adapter.Probe(ctx, *endpoint, request)
		if err != nil {
			endpoint.Health = protocol.EndpointHealthUnhealthy
			endpoint.Findings = append(endpoint.Findings, protocol.DiscoveryFinding{
				Component: "endpoint." + endpoint.ID + ".probe",
				Status:    findingStatusFor(err),
				Source:    adapter.ID(),
				Detail:    truncate(err.Error(), 1024),
			})
			continue
		}
		s.applyProbeResult(ctx, endpoint, result, candidates)
	}
	return endpoints
}

// applyProbeResult folds one probe's findings into the endpoint record.
func (s *Service) applyProbeResult(
	ctx context.Context,
	endpoint *protocol.CognitionEndpoint,
	result ProbeResult,
	candidates []protocol.AcceleratorCandidate,
) {
	succeeded := result.Status == protocol.FindingObserved
	if succeeded {
		endpoint.Health = protocol.EndpointHealthReady
		verifiedAt := protocol.NewTimestamp(s.clock.Now())
		endpoint.VerifiedAt = &verifiedAt
	} else if endpoint.Health == protocol.EndpointHealthReady ||
		endpoint.Health == protocol.EndpointHealthUnverified {
		// A probe that ran and did not answer downgrades a previously
		// optimistic health state. Leaving it at "ready" would let a failing
		// endpoint keep receiving work.
		endpoint.Health = protocol.EndpointHealthUnhealthy
	}
	if result.Detail != "" || result.Status != protocol.FindingObserved {
		endpoint.Findings = append(endpoint.Findings, protocol.DiscoveryFinding{
			Component: "endpoint." + endpoint.ID + ".probe",
			Status:    result.Status,
			Source:    endpoint.Runtime,
			Detail:    truncate(result.Detail, 1024),
		})
	}
	if result.RuntimeVersion != "" {
		endpoint.Version = result.RuntimeVersion
	}
	if result.ModelID != "" {
		endpoint.ModelID = result.ModelID
	}
	if result.StructuredOutput != "" {
		endpoint.StructuredOutput = result.StructuredOutput
	}
	// An adapter may report an authentication downgrade but never a promotion:
	// a provider claiming its own session is valid is text, not authority
	// (DCI-083). Only AuthUnauthenticated, AuthExpired and AuthError may pass.
	switch result.Auth {
	case protocol.AuthUnauthenticated, protocol.AuthExpired, protocol.AuthError:
		endpoint.Auth = result.Auth
	}
	endpoint.Measurements = mergeMeasurements(endpoint.Measurements, result.Measurements)

	probeRefs := s.storeEvidence(ctx, endpoint, result)
	endpoint.ProbeRefs = append(endpoint.ProbeRefs, probeRefs...)
	sort.Strings(endpoint.ProbeRefs)

	// Acceleration is a local-execution property. Evaluating it for a remote
	// endpoint would let a remote provider's claim satisfy a local
	// acceleration requirement.
	if endpoint.Locality != protocol.LocalityLocal {
		return
	}
	backend := result.Backend
	if backend == "" {
		backend = expectedBackend(candidates)
	}
	signals := result.Signals
	if backend == protocol.BackendUnknown {
		backend, signals = resolveUnnamedBackend(candidates, signals)
	}
	evidence := EvaluateAcceleration(AccelerationInput{
		Backend:        backend,
		Candidate:      candidateForBackend(candidates, backend),
		Signals:        signals,
		ProbeRan:       true,
		ProbeSucceeded: succeeded,
		ObservedAt:     protocol.NewTimestamp(s.clock.Now()),
		RuntimeVersion: endpoint.Version,
		DeviceID:       result.DeviceID,
		ProbeRefs:      probeRefs,
	})
	endpoint.Acceleration = &evidence
}

// storeEvidence files raw probe output in the artifact store.
//
// Raw output can be large and is of no interest until something disagrees, so it
// goes to the content-addressed store and the profile keeps a digest reference
// (ADR-0009). Without a store configured, the evidence is dropped and the
// endpoint records that it was — a silently missing reference would later read
// as "no evidence existed".
func (s *Service) storeEvidence(ctx context.Context, endpoint *protocol.CognitionEndpoint, result ProbeResult) []string {
	if len(result.RawEvidence) == 0 {
		return nil
	}
	if s.artifacts == nil {
		endpoint.Findings = append(endpoint.Findings, protocol.DiscoveryFinding{
			Component: "endpoint." + endpoint.ID + ".probe_evidence",
			Status:    protocol.FindingUnsupported,
			Detail:    "no artifact store is configured, so raw probe evidence was not retained",
		})
		return nil
	}
	mediaType := result.EvidenceMediaType
	if mediaType == "" {
		mediaType = "application/json"
	}
	stored, err := s.artifacts.PutBytes(ctx, s.artifactProject, "cognition-probe", mediaType,
		result.RawEvidence, int64(len(result.RawEvidence)))
	if err != nil {
		endpoint.Findings = append(endpoint.Findings, protocol.DiscoveryFinding{
			Component: "endpoint." + endpoint.ID + ".probe_evidence",
			Status:    protocol.FindingError,
			Detail:    truncate(err.Error(), 512),
		})
		return nil
	}
	return []string{stored.Ref.Locator}
}

// mergeMeasurements combines measurement lists, keeping the newest value per
// name and a deterministic order.
func mergeMeasurements(existing, incoming []protocol.Measurement) []protocol.Measurement {
	index := map[string]int{}
	out := append([]protocol.Measurement(nil), existing...)
	for i, measurement := range out {
		index[measurement.Name] = i
	}
	for _, measurement := range incoming {
		if i, ok := index[measurement.Name]; ok {
			out[i] = measurement
			continue
		}
		index[measurement.Name] = len(out)
		out = append(out, measurement)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// reconcileCandidates lifts a candidate to verified when an endpoint proved it.
//
// This is the only way a candidate reaches verified, and it happens after the
// fact rather than during assessment, which keeps the assessment layer pure and
// keeps the evidence attached to the endpoint that produced it.
func reconcileCandidates(candidates []protocol.AcceleratorCandidate, endpoints []protocol.CognitionEndpoint) []protocol.AcceleratorCandidate {
	out := append([]protocol.AcceleratorCandidate(nil), candidates...)
	for _, endpoint := range endpoints {
		if endpoint.Acceleration == nil {
			continue
		}
		for i := range out {
			if out[i].Backend != endpoint.Acceleration.Backend {
				continue
			}
			switch endpoint.Acceleration.State {
			case protocol.StateVerified:
				out[i].State = protocol.StateVerified
				out[i].Reasons = append(out[i].Reasons,
					"verified by an inference probe on endpoint "+endpoint.ID)
			case protocol.StateFailed:
				// Recorded, but it does not downgrade the candidate: another
				// endpoint or another model may still use the backend.
				out[i].Reasons = append(out[i].Reasons,
					"an inference probe on endpoint "+endpoint.ID+" did not use this backend")
			}
			sort.Strings(out[i].Reasons)
		}
	}
	return out
}

// resolveUnnamedBackend names the backend behind an observed-but-unnamed offload.
//
// Some runtimes report *that* they offloaded without saying through what: Ollama
// exposes a resident model's VRAM residency, which authoritatively establishes
// offload, and never names CUDA, ROCm, Vulkan or Metal. Leaving the backend as
// `unknown` would be honest but would also make the verified endpoint and the
// machine's candidate list disagree — the endpoint reporting verified offload
// while the Vulkan candidate it actually used still reads `runtime_available`.
//
// So the *name* is taken from the machine's assessment, and only when the
// assessment is unambiguous: exactly one accelerated backend assessed as
// supported. The inference is strictly about naming; whether offload happened is
// still decided solely by the runtime's own signal. With zero or several
// supported accelerated candidates the backend stays `unknown`, which keeps a
// verified-but-unnamed offload truthful rather than guessing between two devices.
func resolveUnnamedBackend(
	candidates []protocol.AcceleratorCandidate,
	signals []protocol.AccelerationSignal,
) (protocol.BackendKind, []protocol.AccelerationSignal) {
	var named protocol.BackendKind
	matches := 0
	for _, candidate := range candidates {
		if candidate.Backend == protocol.BackendCPU || candidate.Backend == protocol.BackendUnknown {
			continue
		}
		if candidate.Support != protocol.SupportSupported {
			continue
		}
		named = candidate.Backend
		matches++
	}
	if matches != 1 {
		return protocol.BackendUnknown, signals
	}
	// Re-label only the unnamed signals. A signal that named a backend itself is
	// left exactly as the adapter reported it.
	out := make([]protocol.AccelerationSignal, 0, len(signals))
	for _, signal := range signals {
		if signal.Backend == protocol.BackendUnknown {
			signal.Backend = named
			signal.Statement = appendStatement(signal.Statement,
				"the runtime did not name the backend; identified as "+string(named)+
					" from the machine's only supported accelerated candidate")
		}
		out = append(out, signal)
	}
	return named, out
}

func appendStatement(existing, addition string) string {
	if existing == "" {
		return addition
	}
	return existing + "; " + addition
}

func candidateForBackend(candidates []protocol.AcceleratorCandidate, backend protocol.BackendKind) *protocol.AcceleratorCandidate {
	for i := range candidates {
		if candidates[i].Backend == backend {
			return &candidates[i]
		}
	}
	return nil
}

// expectedBackend picks the accelerated backend the machine is most likely to
// use, for the case where an adapter could not name one.
//
// It is only a label for the acceleration record; it cannot make a claim true.
// With no accelerated candidate it returns CPU, which is the honest default.
func expectedBackend(candidates []protocol.AcceleratorCandidate) protocol.BackendKind {
	best := protocol.BackendCPU
	bestRank := -1
	for _, candidate := range candidates {
		if candidate.Backend == protocol.BackendCPU {
			continue
		}
		rank := supportRank(candidate.Support)
		if rank > bestRank {
			bestRank, best = rank, candidate.Backend
		}
	}
	return best
}

func supportRank(level protocol.SupportLevel) int {
	switch level {
	case protocol.SupportSupported:
		return 3
	case protocol.SupportUncertain:
		return 2
	case protocol.SupportUnknown:
		return 1
	default:
		return 0
	}
}

// adapterIDOf extracts the adapter identity from an endpoint id.
//
// Endpoint ids are "<adapter>:<instance>" by convention, which keeps the
// mapping from endpoint back to adapter explicit without a second registry to
// keep in sync.
func adapterIDOf(endpointID string) string {
	adapter, _, _ := strings.Cut(endpointID, ":")
	return adapter
}

func indexDeclarations(declarations []Declaration) map[string]*Declaration {
	out := make(map[string]*Declaration, len(declarations))
	for i := range declarations {
		out[declarations[i].EndpointID] = &declarations[i]
	}
	return out
}

// findingStatusFor maps an adapter error to the finding that describes it.
func findingStatusFor(err error) protocol.FindingStatus {
	switch errs.CategoryOf(err) {
	case errs.CategoryNotFound:
		return protocol.FindingAbsent
	case errs.CategoryUnsupported:
		return protocol.FindingUnsupported
	case errs.CategoryProbeTimeout:
		return protocol.FindingTimeout
	case errs.CategoryProbeFailed, errs.CategoryModelUnavailable, errs.CategoryUnauthenticated:
		return protocol.FindingError
	default:
		return protocol.FindingError
	}
}

// looksLikeSecret is a conservative guard against a credential reaching a
// durable record.
//
// It is not a secret scanner and does not pretend to be. It rejects the shapes
// an operator most plausibly pastes by mistake — a long opaque token, an
// explicit key prefix — so the mistake fails loudly at configuration time
// instead of becoming a stored credential.
func looksLikeSecret(value string) bool {
	if value == "" {
		return false
	}
	if len(value) > 128 {
		return true
	}
	lowered := strings.ToLower(value)
	for _, prefix := range []string{"sk-", "sk_", "pat_", "ghp_", "github_pat_", "bearer ", "aws_"} {
		if strings.HasPrefix(lowered, prefix) {
			return true
		}
	}
	return strings.Contains(lowered, "secret=") || strings.Contains(lowered, "token=")
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}

// Assess derives the readiness verdict and the limitation list.
//
// It is a pure function of the profile so that the verdict cannot disagree with
// the endpoints it summarises. The precedence matters: a missing deterministic
// prerequisite outranks missing cognition, because DevCadience without Git
// cannot do its local-authority job at all, whereas DevCadience without a model
// is a documented operating profile (docs/MODEL_RUNTIME.md §7).
func Assess(profile protocol.MachineCapabilityProfile) (protocol.CognitionAssessment, []string) {
	var limitations []string

	gitInstalled := false
	for _, entry := range profile.Environment.Software {
		if entry.ID == "git" && entry.Installed {
			gitInstalled = true
			if entry.VersionStatus == protocol.VersionIncompatible {
				limitations = append(limitations,
					"the installed git version is below the supported floor")
			}
		}
	}
	if !gitInstalled {
		limitations = append(limitations,
			"git is not installed, so deterministic repository capability is unavailable")
	}

	usable := 0
	graded := 0
	acceleratedLocal := 0
	localEndpoints := 0
	for _, endpoint := range profile.Endpoints {
		// Only a local endpoint that actually exists counts towards the
		// acceleration limitation. An endpoint reported at not_installed is
		// there to explain *why* a runtime is unavailable; complaining that it
		// has no verified acceleration would be noise about software that is not
		// there.
		if endpoint.Locality == protocol.LocalityLocal &&
			endpoint.Health != protocol.EndpointHealthNotInstalled &&
			endpoint.Health != protocol.EndpointHealthUnsupported {
			localEndpoints++
			if endpoint.AccelerationVerified() {
				acceleratedLocal++
			}
		}
		if !endpoint.Health.Usable() {
			continue
		}
		usable++
		if endpoint.Capability(protocol.CapabilityImplementation).Grade.GradeRank() >=
			protocol.GradeStrong.GradeRank() {
			graded++
		}
	}
	if usable == 0 {
		limitations = append(limitations, "no cognition endpoint is usable")
	}
	if graded == 0 && usable > 0 {
		limitations = append(limitations,
			"no usable endpoint has a graded strong implementation capability, so implementation cannot be routed")
	}
	if localEndpoints > 0 && acceleratedLocal == 0 {
		limitations = append(limitations,
			"no local endpoint has verified non-cpu acceleration")
	}
	for _, candidate := range profile.AcceleratorCandidates {
		if candidate.Support == protocol.SupportUncertain {
			limitations = append(limitations,
				"backend "+string(candidate.Backend)+" support is uncertain on this hardware")
		}
	}
	sort.Strings(limitations)
	limitations = dedupe(limitations)

	switch {
	case !gitInstalled:
		return protocol.AssessmentPartiallyReady, limitations
	case usable == 0:
		return protocol.AssessmentModelCognitionUnavailable, limitations
	case graded > 0:
		return protocol.AssessmentReady, limitations
	default:
		return protocol.AssessmentReadyReducedCapability, limitations
	}
}
