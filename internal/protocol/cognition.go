package protocol

import (
	"encoding/json"

	"github.com/olostan/DevCadience/internal/errs"
)

// This file holds the durable representation of *cognition capability*: which
// sources of model cognition exist, whether they are actually usable, what is
// known about what they can do, and on what evidence.
//
// The types here are provider-neutral (DCI-054, DCI-055). "Ollama", "Codex CLI"
// and a remote API are values of Runtime/Provider fields discovered by
// adapters; no field, enum or rule in this file is specific to one of them.
//
// Three distinctions are enforced structurally because conflating them is the
// failure mode this milestone exists to prevent:
//
//   - installed != healthy != authenticated != usable (Health, AuthStatus);
//   - inference works != inference is accelerated (AccelerationState);
//   - hardware feasibility != observed capability (CapabilityGrade +
//     CapabilityProvenance).

// EndpointKind is the class of a cognition endpoint.
//
// A cognition endpoint is not a cloud provider. It is any replaceable source
// of model cognition, and the three kinds below differ in where inference runs
// and what the endpoint can be trusted with, not in who sells it
// (docs/MODEL_RUNTIME.md §2).
type EndpointKind string

const (
	// EndpointLocalRuntime is a model runtime executing on this machine.
	EndpointLocalRuntime EndpointKind = "local_runtime"
	// EndpointAuthenticatedCLI is an installed, user-authenticated coding or
	// agent CLI. Inference is remote; tool execution is local.
	EndpointAuthenticatedCLI EndpointKind = "authenticated_cli"
	// EndpointRemoteAPI is a direct remote model API.
	EndpointRemoteAPI EndpointKind = "remote_api"
)

// Valid reports whether the kind is defined by the schema.
//
// Future kinds such as a LAN inference worker are deliberately absent:
// docs/MODEL_RUNTIME.md §21 requires a separate threat model before one
// exists, and publishing the enum value early would invite routing code to
// handle a case nothing can produce.
func (k EndpointKind) Valid() bool {
	switch k {
	case EndpointLocalRuntime, EndpointAuthenticatedCLI, EndpointRemoteAPI:
		return true
	}
	return false
}

// Locality is where inference physically happens relative to this machine.
//
// It is separate from EndpointKind because routing and privacy care about
// locality, while adapters care about kind: an authenticated CLI runs tools
// locally but sends context to a remote model, which is neither "local" nor a
// plain remote API.
type Locality string

const (
	LocalityLocal Locality = "local"
	// LocalityRemoteInferenceLocalTools is the authenticated-CLI shape:
	// inference is remote, but the process reads and writes the local
	// worktree, so source exposure is mediated by a tool rather than by a
	// context window.
	LocalityRemoteInferenceLocalTools Locality = "remote_inference_local_tools"
	LocalityRemote                    Locality = "remote"
)

// Valid reports whether the locality is defined by the schema.
func (l Locality) Valid() bool {
	switch l {
	case LocalityLocal, LocalityRemoteInferenceLocalTools, LocalityRemote:
		return true
	}
	return false
}

// EndpointHealth is how far along the installed-to-usable chain an endpoint is.
//
// The states are ordered but not numerically comparable on purpose: reaching
// "ready" requires a successful probe of *this* endpoint, and no combination
// of weaker facts adds up to it.
type EndpointHealth string

const (
	// EndpointHealthNotInstalled means the endpoint's software is absent.
	EndpointHealthNotInstalled EndpointHealth = "not_installed"
	// EndpointHealthInstalled means the software exists but nothing further was
	// established. This is where a PATH lookup alone leaves an endpoint.
	EndpointHealthInstalled EndpointHealth = "installed"
	// EndpointHealthNotConfigured means the software exists but has no usable
	// configuration — a runtime with no model, a CLI with no project setup.
	EndpointHealthNotConfigured EndpointHealth = "not_configured"
	// EndpointHealthUnhealthy means a probe ran and failed.
	EndpointHealthUnhealthy EndpointHealth = "unhealthy"
	// EndpointHealthUnverified means the endpoint is plausibly usable but no probe
	// has confirmed it. It is not a soft "ready".
	EndpointHealthUnverified EndpointHealth = "unverified"
	// EndpointHealthReady means a probe confirmed the endpoint answered correctly.
	EndpointHealthReady EndpointHealth = "ready"
	// EndpointHealthUnsupported means this build cannot use the endpoint at the
	// observed version.
	EndpointHealthUnsupported EndpointHealth = "unsupported"
	EndpointHealthUnknown     EndpointHealth = "unknown"
)

// Valid reports whether the health state is defined by the schema.
func (h EndpointHealth) Valid() bool {
	switch h {
	case EndpointHealthNotInstalled, EndpointHealthInstalled, EndpointHealthNotConfigured, EndpointHealthUnhealthy,
		EndpointHealthUnverified, EndpointHealthReady, EndpointHealthUnsupported, EndpointHealthUnknown:
		return true
	}
	return false
}

// Usable reports whether routing may consider this endpoint at all.
//
// Only a probed-ready endpoint qualifies. "unverified" deliberately does not:
// routing a real implementation task to an endpoint nobody has called is how a
// capability claim becomes an outage.
func (h EndpointHealth) Usable() bool { return h == EndpointHealthReady }

// AuthStatus is the authentication state of an endpoint.
//
// AuthUnknown is the correct answer whenever no official, non-mutating,
// non-secret-reading mechanism exposes the answer. DevCadience never inspects
// credential files, copies tokens or launches a login flow to find out
// (docs/SECURITY.md §7).
type AuthStatus string

const (
	// AuthNotApplicable is the local-runtime case: there is no account.
	AuthNotApplicable   AuthStatus = "not_applicable"
	AuthAuthenticated   AuthStatus = "authenticated"
	AuthUnauthenticated AuthStatus = "unauthenticated"
	AuthExpired         AuthStatus = "expired"
	AuthUnknown         AuthStatus = "unknown"
	AuthError           AuthStatus = "error"
)

// Valid reports whether the status is defined by the schema.
func (s AuthStatus) Valid() bool {
	switch s {
	case AuthNotApplicable, AuthAuthenticated, AuthUnauthenticated, AuthExpired, AuthUnknown, AuthError:
		return true
	}
	return false
}

// CostClass is a coarse monetary class.
//
// Classes, not amounts: DevCadience does not know a user's plan, per-token
// price or quota, and a fabricated number would be worse than an honest class
// (docs/MODEL_RUNTIME.md §19). Cost is a property of an *endpoint*, not of a
// provider: one provider commonly exposes several endpoints in different
// classes.
type CostClass string

const (
	CostLocalCompute         CostClass = "local_compute"
	CostSubscriptionIncluded CostClass = "subscription_included"
	CostRemoteEconomy        CostClass = "remote_economy"
	CostRemoteStrong         CostClass = "remote_strong"
	CostFrontierExpensive    CostClass = "frontier_expensive"
	CostUnknown              CostClass = "unknown"
)

// Valid reports whether the class is defined by the schema.
func (c CostClass) Valid() bool {
	switch c {
	case CostLocalCompute, CostSubscriptionIncluded, CostRemoteEconomy,
		CostRemoteStrong, CostFrontierExpensive, CostUnknown:
		return true
	}
	return false
}

// CostRank orders classes from cheapest to most expensive for routing.
//
// CostUnknown ranks *above* every known class rather than below it: choosing an
// endpoint of unknown cost over a known-economical one would spend the user's
// money on an assumption.
func (c CostClass) CostRank() int {
	switch c {
	case CostLocalCompute:
		return 0
	case CostSubscriptionIncluded:
		return 1
	case CostRemoteEconomy:
		return 2
	case CostRemoteStrong:
		return 3
	case CostFrontierExpensive:
		return 4
	default:
		return 5
	}
}

// SourceExposure is how much repository source an endpoint requires to work.
//
// It is a lattice: a project policy names the maximum exposure it permits, an
// endpoint names the minimum exposure it needs, and routing compares them as a
// hard constraint. Remote inference is never an implicit permission for source
// to leave the machine (docs/SECURITY.md §1).
type SourceExposure string

const (
	// ExposureLocalOnly means no source leaves the machine.
	ExposureLocalOnly SourceExposure = "local_only"
	// ExposureSemanticEvidenceOnly permits derived semantic evidence but no
	// verbatim source.
	ExposureSemanticEvidenceOnly SourceExposure = "semantic_evidence_only"
	ExposureFocusedSnippets      SourceExposure = "focused_snippets"
	ExposureSelectedFiles        SourceExposure = "selected_files"
	// ExposureToolMediatedWorktree is the authenticated-CLI shape: an
	// authorized agent reads the worktree through tools rather than receiving
	// a curated context.
	ExposureToolMediatedWorktree   SourceExposure = "tool_mediated_worktree"
	ExposureUnrestrictedAuthorized SourceExposure = "unrestricted_authorized"
)

// Valid reports whether the class is defined by the schema.
func (e SourceExposure) Valid() bool {
	switch e {
	case ExposureLocalOnly, ExposureSemanticEvidenceOnly, ExposureFocusedSnippets,
		ExposureSelectedFiles, ExposureToolMediatedWorktree, ExposureUnrestrictedAuthorized:
		return true
	}
	return false
}

// ExposureRank orders exposure classes from least to most revealing.
func (e SourceExposure) ExposureRank() int {
	switch e {
	case ExposureLocalOnly:
		return 0
	case ExposureSemanticEvidenceOnly:
		return 1
	case ExposureFocusedSnippets:
		return 2
	case ExposureSelectedFiles:
		return 3
	case ExposureToolMediatedWorktree:
		return 4
	case ExposureUnrestrictedAuthorized:
		return 5
	default:
		return 6
	}
}

// CapabilityDimension names one thing an endpoint may be good or bad at.
type CapabilityDimension string

const (
	CapabilityRepositoryReasoning CapabilityDimension = "repository_reasoning"
	CapabilityImplementation      CapabilityDimension = "implementation"
	CapabilityReview              CapabilityDimension = "review"
	CapabilityArchitecture        CapabilityDimension = "architecture"
)

// Valid reports whether the dimension is defined by the schema.
func (d CapabilityDimension) Valid() bool {
	switch d {
	case CapabilityRepositoryReasoning, CapabilityImplementation, CapabilityReview, CapabilityArchitecture:
		return true
	}
	return false
}

// CapabilityGrade is how capable an endpoint is on one dimension.
type CapabilityGrade string

const (
	// GradeUnknown is the default and the honest answer for almost every
	// endpoint M3A discovers. Nothing infers a grade from a model name, a
	// parameter count or the machine's memory size.
	GradeUnknown     CapabilityGrade = "unknown"
	GradeUnsupported CapabilityGrade = "unsupported"
	GradeLow         CapabilityGrade = "low"
	GradeMedium      CapabilityGrade = "medium"
	GradeStrong      CapabilityGrade = "strong"
)

// Valid reports whether the grade is defined by the schema.
func (g CapabilityGrade) Valid() bool {
	switch g {
	case GradeUnknown, GradeUnsupported, GradeLow, GradeMedium, GradeStrong:
		return true
	}
	return false
}

// GradeRank orders grades for comparison. Unknown and unsupported share the
// bottom because neither satisfies a requirement, but they are distinct values
// so an explanation can say which one it was.
func (g CapabilityGrade) GradeRank() int {
	switch g {
	case GradeStrong:
		return 4
	case GradeMedium:
		return 3
	case GradeLow:
		return 2
	case GradeUnsupported:
		return 1
	default:
		return 0
	}
}

// CapabilityProvenance says where a grade came from.
//
// This field is what stops a capability claim from becoming folklore
// (DCI-005, DCI-012). A grade with provenance "unknown" is not evidence, and
// routing that requires a grade must say which provenance satisfied it.
type CapabilityProvenance string

const (
	ProvenanceUnknown CapabilityProvenance = "unknown"
	// ProvenanceConfigured means a human or an operator policy asserted it.
	// That is a legitimate source of authority — the operator knows things
	// DevCadience cannot measure — but it is not a measurement, and it is
	// recorded as configuration so a surprising routing choice can be traced
	// to the person who declared it.
	ProvenanceConfigured CapabilityProvenance = "configured"
	// ProvenanceMeasured means an M3A probe observed it. M3A probes establish
	// operational properties (does it answer, does it emit valid JSON), not
	// reasoning quality.
	ProvenanceMeasured CapabilityProvenance = "measured"
	// ProvenanceEvaluated means an evaluation suite with recorded outcomes
	// established it. Nothing in M3A produces this; it is the provenance an
	// implementation-quality claim actually requires.
	ProvenanceEvaluated CapabilityProvenance = "evaluated"
)

// Valid reports whether the provenance is defined by the schema.
func (p CapabilityProvenance) Valid() bool {
	switch p {
	case ProvenanceUnknown, ProvenanceConfigured, ProvenanceMeasured, ProvenanceEvaluated:
		return true
	}
	return false
}

// GradedCapability is one capability grade with its provenance.
type GradedCapability struct {
	Dimension  CapabilityDimension  `json:"dimension"`
	Grade      CapabilityGrade      `json:"grade"`
	Provenance CapabilityProvenance `json:"provenance"`
	// Source names the configuration key, probe or evaluation that produced
	// the grade.
	Source string `json:"source,omitempty"`
}

// Validate checks the graded capability is interpretable.
func (c GradedCapability) Validate() error {
	const kind = "MachineCapabilityProfile"
	if !c.Dimension.Valid() {
		return enumError(kind, "endpoints[].capabilities[].dimension", string(c.Dimension),
			"repository_reasoning", "implementation", "review", "architecture")
	}
	if !c.Grade.Valid() {
		return enumError(kind, "endpoints[].capabilities[].grade", string(c.Grade),
			"unknown", "unsupported", "low", "medium", "strong")
	}
	if !c.Provenance.Valid() {
		return enumError(kind, "endpoints[].capabilities[].provenance", string(c.Provenance),
			"unknown", "configured", "measured", "evaluated")
	}
	// A graded claim with no provenance is exactly the inference DCI-012
	// forbids: it would read as evidence while resting on nothing.
	if c.Grade != GradeUnknown && c.Provenance == ProvenanceUnknown {
		return errs.New(errs.CategoryInvalidArgument,
			"%s: endpoints[].capabilities[%s] grades %q with provenance unknown; a graded capability requires provenance",
			kind, c.Dimension, c.Grade)
	}
	return nil
}

// FeatureSupport is what a probe established about a discrete feature.
type FeatureSupport string

const (
	FeatureUnknown     FeatureSupport = "unknown"
	FeatureUnsupported FeatureSupport = "unsupported"
	// FeatureDeclared means documentation or configuration claims support but
	// no probe confirmed it here.
	FeatureDeclared FeatureSupport = "declared"
	// FeatureProbePassed means a synthetic probe exercised the feature
	// successfully. For structured output this is exactly the claim
	// "structured_output_probe_passed" and nothing more: it does not mean the
	// endpoint reliably emits valid protocol documents under load.
	FeatureProbePassed FeatureSupport = "probe_passed"
	FeatureProbeFailed FeatureSupport = "probe_failed"
)

// Valid reports whether the value is defined by the schema.
func (f FeatureSupport) Valid() bool {
	switch f {
	case FeatureUnknown, FeatureUnsupported, FeatureDeclared, FeatureProbePassed, FeatureProbeFailed:
		return true
	}
	return false
}

// BackendKind is an inference execution backend.
type BackendKind string

const (
	BackendCPU    BackendKind = "cpu"
	BackendMetal  BackendKind = "metal"
	BackendCUDA   BackendKind = "cuda"
	BackendROCm   BackendKind = "rocm"
	BackendVulkan BackendKind = "vulkan"
	// BackendUnknown covers a runtime that offloads to a device it does not
	// name. Observed offload with an unnamed backend is still offload.
	BackendUnknown BackendKind = "unknown"
)

// Valid reports whether the backend is defined by the schema.
func (b BackendKind) Valid() bool {
	switch b {
	case BackendCPU, BackendMetal, BackendCUDA, BackendROCm, BackendVulkan, BackendUnknown:
		return true
	}
	return false
}

// AccelerationState is how far the evidence for one backend actually goes.
//
// DCI-106 is the reason this is an eight-state enum rather than a boolean.
// Each state is a distinct claim, and only StateVerified asserts that real
// inference used the backend:
//
//	not_detected     no device for this backend exists
//	candidate        a device exists that this backend could plausibly use
//	runtime_available a runtime that can drive the backend is installed
//	unverified       everything looks possible; no inference has proven it
//	verified         an inference probe produced backend evidence
//	failed           an inference probe ran and did not use the backend
//	unsupported      this device/runtime pair is known not to work
//	unknown          the question could not be evaluated
type AccelerationState string

const (
	StateNotDetected      AccelerationState = "not_detected"
	StateCandidate        AccelerationState = "candidate"
	StateRuntimeAvailable AccelerationState = "runtime_available"
	StateUnverified       AccelerationState = "unverified"
	StateVerified         AccelerationState = "verified"
	StateFailed           AccelerationState = "failed"
	StateUnsupported      AccelerationState = "unsupported"
	StateUnknown          AccelerationState = "unknown"
)

// Valid reports whether the state is defined by the schema.
func (s AccelerationState) Valid() bool {
	switch s {
	case StateNotDetected, StateCandidate, StateRuntimeAvailable, StateUnverified,
		StateVerified, StateFailed, StateUnsupported, StateUnknown:
		return true
	}
	return false
}

// SignalTrust is how much a single acceleration signal establishes on its own.
type SignalTrust string

const (
	// TrustAuthoritative means the runtime executing the inference reported
	// the backend it used. This is the strongest available evidence and is
	// sufficient on its own; requiring corroboration from weaker sources
	// would make the result worse, not safer.
	TrustAuthoritative SignalTrust = "authoritative"
	// TrustCorroborating means an independent observer (OS or vendor
	// telemetry) agrees. It supports an authoritative signal and cannot
	// replace one.
	TrustCorroborating SignalTrust = "corroborating"
	// TrustIndicative means the signal is consistent with acceleration but
	// does not establish it — a driver being loaded, a package being
	// installed. Indicative signals never reach "verified".
	TrustIndicative SignalTrust = "indicative"
)

// Valid reports whether the trust level is defined by the schema.
func (t SignalTrust) Valid() bool {
	switch t {
	case TrustAuthoritative, TrustCorroborating, TrustIndicative:
		return true
	}
	return false
}

// AccelerationSignal is one observation bearing on whether a backend was used.
type AccelerationSignal struct {
	// Source is the observer, e.g. "ollama:/api/ps", "mlx:device_info".
	Source string      `json:"source"`
	Trust  SignalTrust `json:"trust"`
	// Backend is the backend this signal points at.
	Backend BackendKind `json:"backend"`
	// Offloaded is the signal's verdict: true when it indicates the workload
	// ran on the accelerator, false when it indicates it did not. A signal
	// that reports CPU execution is evidence, not a missing observation.
	Offloaded bool `json:"offloaded"`
	// Statement is bounded, sanitised text describing the observation.
	Statement string `json:"statement,omitempty"`
}

// Validate checks the signal is interpretable.
func (s AccelerationSignal) Validate() error {
	const kind = "MachineCapabilityProfile"
	if err := requireNonEmpty(kind, "acceleration.signals[].source", s.Source); err != nil {
		return err
	}
	if !s.Trust.Valid() {
		return enumError(kind, "acceleration.signals[].trust", string(s.Trust),
			"authoritative", "corroborating", "indicative")
	}
	if !s.Backend.Valid() {
		return enumError(kind, "acceleration.signals[].backend", string(s.Backend),
			"cpu", "metal", "cuda", "rocm", "vulkan", "unknown")
	}
	return nil
}

// AccelerationEvidence is the full acceleration claim for one endpoint,
// together with everything the claim rests on.
//
// Conflicts is not decoration. When signals disagree — the runtime says GPU and
// vendor telemetry says no process is resident — the result stays unverified
// and the contradiction is reported. Guessing which observer to believe would
// manufacture exactly the false positive DCI-106 exists to prevent.
type AccelerationEvidence struct {
	Backend BackendKind          `json:"backend"`
	State   AccelerationState    `json:"state"`
	Signals []AccelerationSignal `json:"signals,omitempty"`
	// Conflicts lists contradictions between signals, sorted.
	Conflicts []string `json:"conflicts,omitempty"`
	// VerifiedAt is set only when State is verified.
	VerifiedAt *Timestamp `json:"verified_at,omitempty"`
	// RuntimeVersion and DeviceID pin the software and hardware the
	// verification applies to, so a driver or runtime upgrade can later
	// invalidate it (docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md §5).
	RuntimeVersion string `json:"runtime_version,omitempty"`
	DeviceID       string `json:"device_id,omitempty"`
	// ProbeRefs point at stored probe evidence by artifact reference, so raw
	// output stays retrievable without being inlined here (DCI-011).
	ProbeRefs []string `json:"probe_refs,omitempty"`
}

// Validate checks the evidence is internally consistent.
func (e AccelerationEvidence) Validate() error {
	const kind = "MachineCapabilityProfile"
	if !e.Backend.Valid() {
		return enumError(kind, "acceleration.backend", string(e.Backend),
			"cpu", "metal", "cuda", "rocm", "vulkan", "unknown")
	}
	if !e.State.Valid() {
		return enumError(kind, "acceleration.state", string(e.State),
			"not_detected", "candidate", "runtime_available", "unverified",
			"verified", "failed", "unsupported", "unknown")
	}
	for _, s := range e.Signals {
		if err := s.Validate(); err != nil {
			return err
		}
	}
	// The durable form of DCI-106: a verified claim must carry the evidence
	// that verified it and the instant it was verified. Without both, a
	// later reader cannot tell a real verification from an assertion.
	if e.State == StateVerified {
		if e.VerifiedAt == nil {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: acceleration state verified requires verified_at", kind)
		}
		authoritative := false
		for _, s := range e.Signals {
			if s.Trust == TrustAuthoritative && s.Offloaded && s.Backend == e.Backend {
				authoritative = true
				break
			}
		}
		if !authoritative {
			return errs.New(errs.CategoryIntegrity,
				"%s: acceleration state verified for backend %s carries no authoritative offload signal (DCI-106)",
				kind, e.Backend)
		}
		if len(e.Conflicts) > 0 {
			return errs.New(errs.CategoryIntegrity,
				"%s: acceleration state verified while %d evidence conflict(s) are recorded; "+
					"contradicted evidence cannot verify a backend (DCI-106)", kind, len(e.Conflicts))
		}
	}
	if e.State != StateVerified && e.VerifiedAt != nil {
		return errs.New(errs.CategoryInvalidArgument,
			"%s: acceleration state %s must not carry verified_at", kind, e.State)
	}
	return nil
}

// SupportLevel is this build's compatibility judgement for a backend on the
// observed hardware.
type SupportLevel string

const (
	SupportSupported SupportLevel = "supported"
	// SupportUncertain is the honest answer for most AMD integrated devices
	// under ROCm: it may work, this build will not claim either way
	// (docs/MODEL_RUNTIME.md §11).
	SupportUncertain   SupportLevel = "uncertain"
	SupportUnsupported SupportLevel = "unsupported"
	SupportUnknown     SupportLevel = "unknown"
)

// Valid reports whether the level is defined by the schema.
func (l SupportLevel) Valid() bool {
	switch l {
	case SupportSupported, SupportUncertain, SupportUnsupported, SupportUnknown:
		return true
	}
	return false
}

// AcceleratorCandidate is an assessed possibility, not a fact and not a
// recommendation.
//
// It answers "could this backend plausibly work on this device, and how far
// does the evidence currently go?" The State field never exceeds what evidence
// supports; only an inference probe can raise it to verified.
type AcceleratorCandidate struct {
	Backend BackendKind `json:"backend"`
	// DeviceID references an AcceleratorDevice in EnvironmentFacts, or is
	// empty for the CPU fallback candidate.
	DeviceID string            `json:"device_id,omitempty"`
	Support  SupportLevel      `json:"support"`
	State    AccelerationState `json:"state"`
	// Reasons explains the assessment in stable, sorted, human-readable
	// terms. This is what makes the compatibility layer auditable instead of
	// a black box.
	Reasons []string `json:"reasons,omitempty"`
	// RequiredSoftware names software this backend needs that was not found.
	RequiredSoftware []string `json:"required_software,omitempty"`
	// KnowledgeRevision identifies the compatibility knowledge that produced
	// this assessment, so a later revision's different answer is explainable
	// rather than mysterious (ADR-0011 §5).
	KnowledgeRevision string `json:"knowledge_revision,omitempty"`
}

// Validate checks the candidate is interpretable.
func (c AcceleratorCandidate) Validate() error {
	const kind = "MachineCapabilityProfile"
	if !c.Backend.Valid() {
		return enumError(kind, "accelerator_candidates[].backend", string(c.Backend),
			"cpu", "metal", "cuda", "rocm", "vulkan", "unknown")
	}
	if !c.Support.Valid() {
		return enumError(kind, "accelerator_candidates[].support", string(c.Support),
			"supported", "uncertain", "unsupported", "unknown")
	}
	if !c.State.Valid() {
		return enumError(kind, "accelerator_candidates[].state", string(c.State),
			"not_detected", "candidate", "runtime_available", "unverified",
			"verified", "failed", "unsupported", "unknown")
	}
	return nil
}

// Measurement is one observed operational property of an endpoint.
//
// Every measurement names its unit and its source. There is no aggregate
// "score": ENGINEERING_STANDARDS.md §14 wants empirical routing inputs, and a
// single number would hide which observation it came from.
type Measurement struct {
	// Name is a stable key, e.g. "load_duration_ms", "request_duration_ms",
	// "generation_tokens_per_second".
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	// Source is the observer that reported it. A throughput measurement is
	// recorded only when the runtime itself reported token counts and
	// durations; nothing here is derived from character counts.
	Source string `json:"source,omitempty"`
}

// Validate checks the measurement is interpretable.
func (m Measurement) Validate() error {
	const kind = "MachineCapabilityProfile"
	if err := requireNonEmpty(kind, "endpoints[].measurements[].name", m.Name); err != nil {
		return err
	}
	return requireNonEmpty(kind, "endpoints[].measurements[].unit", m.Unit)
}

// CognitionEndpoint is one discovered source of model cognition.
//
// The record is deliberately dull about identity and strict about evidence:
// Provider, Runtime and ModelID are whatever the adapter observed, while
// Health, AuthStatus, Capabilities and Acceleration may only say what a probe
// established. Nothing in this type may be inferred from another field — in
// particular an authenticated CLI implies nothing about which commercial plan
// backs it, so no subscription field exists to guess into.
type CognitionEndpoint struct {
	// ID is stable across discoveries of the same endpoint on the same
	// machine, so routing decisions and probe evidence can refer to it.
	ID   string       `json:"id"`
	Kind EndpointKind `json:"kind"`
	// Provider is the organisation behind the inference when known,
	// Runtime the software driving it. They are separate because one runtime
	// serves many providers' weights and one provider is reachable through
	// several runtimes.
	Provider string `json:"provider,omitempty"`
	Runtime  string `json:"runtime,omitempty"`
	Version  string `json:"version,omitempty"`
	// ModelID and ModelFamily are the model identity when the endpoint
	// discloses it. M6 needs family to avoid treating two frontends over one
	// model as independent reviewers; M3A only records it.
	ModelID     string `json:"model_id,omitempty"`
	ModelFamily string `json:"model_family,omitempty"`
	// AccountRef is an opaque, non-secret handle for the account or profile
	// in use, for the same independence question. It must never be a token,
	// a key or an email address.
	AccountRef string `json:"account_ref,omitempty"`
	// CredentialRef is an opaque reference to a credential held elsewhere.
	// A raw secret in this field would be a durable credential leak
	// (DCI-081); nothing in this build writes one.
	CredentialRef string `json:"credential_ref,omitempty"`

	Locality Locality       `json:"locality"`
	Health   EndpointHealth `json:"health"`
	Auth     AuthStatus     `json:"auth_status"`

	Capabilities     []GradedCapability `json:"capabilities,omitempty"`
	StructuredOutput FeatureSupport     `json:"structured_output"`
	ToolUse          FeatureSupport     `json:"tool_use"`
	// ContextTokens is the operating context target, not an advertised
	// maximum (docs/MODEL_RUNTIME.md §8).
	ContextTokens *int `json:"context_tokens,omitempty"`
	// MaxConcurrentSessions bounds scheduling against this endpoint.
	MaxConcurrentSessions *int `json:"max_concurrent_sessions,omitempty"`

	CostClass CostClass `json:"cost_class"`
	// RequiredSourceExposure is the minimum exposure class this endpoint
	// needs. Routing rejects it when the project permits less.
	RequiredSourceExposure SourceExposure `json:"required_source_exposure"`

	// Acceleration is present for local endpoints. Its absence on a remote
	// endpoint is correct: acceleration is a property of local execution.
	Acceleration *AccelerationEvidence `json:"acceleration,omitempty"`

	Measurements []Measurement `json:"measurements,omitempty"`

	ObservedAt Timestamp  `json:"observed_at"`
	VerifiedAt *Timestamp `json:"verified_at,omitempty"`
	// ProbeRefs point at stored probe evidence.
	ProbeRefs []string `json:"probe_refs,omitempty"`
	// Findings records what could not be established about this endpoint.
	Findings []DiscoveryFinding `json:"findings,omitempty"`
}

// Validate checks the endpoint record is interpretable and self-consistent.
func (e CognitionEndpoint) Validate() error {
	const kind = "MachineCapabilityProfile"
	if err := requireNonEmpty(kind, "endpoints[].id", e.ID); err != nil {
		return err
	}
	if !e.Kind.Valid() {
		return enumError(kind, "endpoints[].kind", string(e.Kind),
			"local_runtime", "authenticated_cli", "remote_api")
	}
	if !e.Locality.Valid() {
		return enumError(kind, "endpoints[].locality", string(e.Locality),
			"local", "remote_inference_local_tools", "remote")
	}
	if !e.Health.Valid() {
		return enumError(kind, "endpoints[].health", string(e.Health),
			"not_installed", "installed", "not_configured", "unhealthy",
			"unverified", "ready", "unsupported", "unknown")
	}
	if !e.Auth.Valid() {
		return enumError(kind, "endpoints[].auth_status", string(e.Auth),
			"not_applicable", "authenticated", "unauthenticated", "expired", "unknown", "error")
	}
	if !e.StructuredOutput.Valid() {
		return enumError(kind, "endpoints[].structured_output", string(e.StructuredOutput),
			"unknown", "unsupported", "declared", "probe_passed", "probe_failed")
	}
	if !e.ToolUse.Valid() {
		return enumError(kind, "endpoints[].tool_use", string(e.ToolUse),
			"unknown", "unsupported", "declared", "probe_passed", "probe_failed")
	}
	if !e.CostClass.Valid() {
		return enumError(kind, "endpoints[].cost_class", string(e.CostClass),
			"local_compute", "subscription_included", "remote_economy",
			"remote_strong", "frontier_expensive", "unknown")
	}
	if !e.RequiredSourceExposure.Valid() {
		return enumError(kind, "endpoints[].required_source_exposure", string(e.RequiredSourceExposure),
			"local_only", "semantic_evidence_only", "focused_snippets",
			"selected_files", "tool_mediated_worktree", "unrestricted_authorized")
	}
	seen := make(map[CapabilityDimension]bool, len(e.Capabilities))
	for _, c := range e.Capabilities {
		if err := c.Validate(); err != nil {
			return err
		}
		if seen[c.Dimension] {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: endpoints[%s] grades capability %s twice", kind, e.ID, c.Dimension)
		}
		seen[c.Dimension] = true
	}
	for _, m := range e.Measurements {
		if err := m.Validate(); err != nil {
			return err
		}
	}
	if e.Acceleration != nil {
		if err := e.Acceleration.Validate(); err != nil {
			return err
		}
		// Acceleration describes local execution. Recording it on a remote
		// endpoint would let a routing policy that requires verified
		// acceleration be satisfied by a machine that is not running the
		// model at all.
		if e.Locality != LocalityLocal {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: endpoints[%s] is %s but carries local acceleration evidence", kind, e.ID, e.Locality)
		}
	}
	if e.Kind == EndpointLocalRuntime && e.Auth != AuthNotApplicable && e.Auth != AuthUnknown {
		return errs.New(errs.CategoryInvalidArgument,
			"%s: endpoints[%s] is a local runtime with auth_status %s; local runtimes have no account",
			kind, e.ID, e.Auth)
	}
	if e.ContextTokens != nil && *e.ContextTokens < 1 {
		return errs.New(errs.CategoryInvalidArgument,
			"%s: endpoints[%s] context_tokens must be >= 1 when present", kind, e.ID)
	}
	if e.MaxConcurrentSessions != nil && *e.MaxConcurrentSessions < 1 {
		return errs.New(errs.CategoryInvalidArgument,
			"%s: endpoints[%s] max_concurrent_sessions must be >= 1 when present", kind, e.ID)
	}
	for _, f := range e.Findings {
		if err := f.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Capability returns the graded capability for a dimension, defaulting to
// unknown/unknown when the endpoint says nothing about it.
//
// Defaulting to unknown rather than to a neutral middle grade is the point: an
// endpoint that has never been measured must not satisfy a requirement.
func (e CognitionEndpoint) Capability(d CapabilityDimension) GradedCapability {
	for _, c := range e.Capabilities {
		if c.Dimension == d {
			return c
		}
	}
	return GradedCapability{Dimension: d, Grade: GradeUnknown, Provenance: ProvenanceUnknown}
}

// AccelerationVerified reports whether this endpoint has a verified
// non-CPU backend.
func (e CognitionEndpoint) AccelerationVerified() bool {
	return e.Acceleration != nil &&
		e.Acceleration.State == StateVerified &&
		e.Acceleration.Backend != BackendCPU
}

// CognitionAssessment is the overall readiness verdict for the machine.
//
// It exists so that a machine with no optional AI software reports a state
// rather than a failure (DCI-104,
// docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md §8).
type CognitionAssessment string

const (
	// AssessmentReady means deterministic capability plus at least one ready
	// cognition endpoint with graded capability.
	AssessmentReady CognitionAssessment = "ready"
	// AssessmentReadyReducedCapability means usable cognition exists but some
	// capability is absent or ungraded.
	AssessmentReadyReducedCapability CognitionAssessment = "ready_with_reduced_capability"
	// AssessmentModelCognitionUnavailable means no cognition endpoint is
	// usable. Deterministic control-plane capability is unaffected, which is
	// why this is a normal state and not an error.
	AssessmentModelCognitionUnavailable CognitionAssessment = "model_cognition_unavailable"
	// AssessmentPartiallyReady means cognition exists but a deterministic
	// prerequisite (such as Git) does not.
	AssessmentPartiallyReady CognitionAssessment = "partially_ready"
	AssessmentUnknown        CognitionAssessment = "unknown"
)

// Valid reports whether the assessment is defined by the schema.
func (a CognitionAssessment) Valid() bool {
	switch a {
	case AssessmentReady, AssessmentReadyReducedCapability, AssessmentModelCognitionUnavailable,
		AssessmentPartiallyReady, AssessmentUnknown:
		return true
	}
	return false
}

// MachineCapabilityProfile is the durable, machine-scoped assessment of what
// this machine and its cognition endpoints can do.
//
// It is machine-scoped, not project-scoped: the hardware and the installed
// runtimes are the same for every project on the host, so it carries no
// project_id and does not implement ProjectScoped. What differs per project is
// *policy* — which endpoints a project's privacy and cost rules permit — and
// that lives in the routing policy, not here.
//
// It is also not written into the engineering event journal by this milestone.
// Available memory, loaded models and endpoint health change minute to minute;
// appending that churn to an append-only project history would bury the record
// of what the project actually did. See
// docs/adr/0013-environment-intelligence-and-cognition-contracts.md for the
// persistence boundary and ProjectState.capabilities for the compact
// project-facing projection.
type MachineCapabilityProfile struct {
	SchemaVersion SchemaVersion `json:"schema_version"`
	ProfileID     string        `json:"profile_id"`
	// MachineFingerprint is a digest over the *stable* facts of the machine
	// (OS, architecture, CPU model, total memory, accelerator identity,
	// software versions). Volatile observations such as available memory are
	// excluded on purpose: the fingerprint exists so a later milestone can
	// tell "the same machine, observed again" from "the machine changed and
	// prior verification is stale".
	MachineFingerprint string    `json:"machine_fingerprint"`
	ObservedAt         Timestamp `json:"observed_at"`
	// KnowledgeRevision identifies the compatibility knowledge used.
	KnowledgeRevision string `json:"knowledge_revision"`
	// ProbeDepth records how deep the discovery that produced this profile
	// was allowed to go, so a profile cannot be read as having tried an
	// inference probe it never ran.
	ProbeDepth ProbeDepth `json:"probe_depth"`

	Environment           EnvironmentFacts       `json:"environment"`
	AcceleratorCandidates []AcceleratorCandidate `json:"accelerator_candidates"`
	Endpoints             []CognitionEndpoint    `json:"endpoints"`

	Assessment CognitionAssessment `json:"assessment"`
	// Limitations states, in stable sorted order, what this machine cannot
	// currently do. It is the honest counterpart to Assessment.
	Limitations []string `json:"limitations,omitempty"`
}

// ProbeDepth is how much work discovery was authorised to do.
//
// Progressive depth keeps an ordinary environment query cheap: `environment
// inspect` must never load a model as a side effect, so an expensive inference
// probe happens only when a caller explicitly asks for it.
type ProbeDepth string

const (
	// DepthInventory reads OS facts and resolves executables. No service is
	// contacted.
	DepthInventory ProbeDepth = "inventory"
	// DepthHealth additionally queries already-running local services and
	// runs bounded, non-mutating version/health commands.
	DepthHealth ProbeDepth = "health"
	// DepthInference additionally runs a small synthetic inference probe
	// against models that already exist locally. It never downloads a model.
	DepthInference ProbeDepth = "inference"
)

// Valid reports whether the depth is defined by the schema.
func (d ProbeDepth) Valid() bool {
	switch d {
	case DepthInventory, DepthHealth, DepthInference:
		return true
	}
	return false
}

// AtLeast reports whether d permits the work required by want.
func (d ProbeDepth) AtLeast(want ProbeDepth) bool { return d.rank() >= want.rank() }

func (d ProbeDepth) rank() int {
	switch d {
	case DepthHealth:
		return 1
	case DepthInference:
		return 2
	default:
		return 0
	}
}

// RecordKind implements Record.
func (p *MachineCapabilityProfile) RecordKind() string { return "MachineCapabilityProfile" }

// RecordID implements Record.
func (p *MachineCapabilityProfile) RecordID() string { return p.ProfileID }

// SchemaVer implements Record.
func (p *MachineCapabilityProfile) SchemaVer() SchemaVersion { return p.SchemaVersion }

// Validate enforces the schema's constraints plus the semantic rules JSON
// Schema cannot express.
func (p *MachineCapabilityProfile) Validate() error {
	const kind = "MachineCapabilityProfile"
	if err := p.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "profile_id", p.ProfileID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "machine_fingerprint", p.MachineFingerprint); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "knowledge_revision", p.KnowledgeRevision); err != nil {
		return err
	}
	if !p.ProbeDepth.Valid() {
		return enumError(kind, "probe_depth", string(p.ProbeDepth), "inventory", "health", "inference")
	}
	if err := p.Environment.Validate(); err != nil {
		return err
	}
	for _, c := range p.AcceleratorCandidates {
		if err := c.Validate(); err != nil {
			return err
		}
	}
	ids := make(map[string]bool, len(p.Endpoints))
	for _, e := range p.Endpoints {
		if err := e.Validate(); err != nil {
			return err
		}
		if ids[e.ID] {
			return errs.New(errs.CategoryInvalidArgument, "%s: endpoint id %q appears twice", kind, e.ID)
		}
		ids[e.ID] = true
	}
	if !p.Assessment.Valid() {
		return enumError(kind, "assessment", string(p.Assessment),
			"ready", "ready_with_reduced_capability", "model_cognition_unavailable",
			"partially_ready", "unknown")
	}
	// A profile produced without inference authority cannot contain a
	// verified backend: verification requires an inference probe, so a
	// verified claim at a shallower depth means the two disagree about what
	// actually happened.
	if !p.ProbeDepth.AtLeast(DepthInference) {
		for _, e := range p.Endpoints {
			if e.Acceleration != nil && e.Acceleration.State == StateVerified {
				return errs.New(errs.CategoryIntegrity,
					"%s: endpoint %s reports verified acceleration but probe_depth is %s; "+
						"verification requires an inference probe (DCI-106)", kind, e.ID, p.ProbeDepth)
			}
		}
	}
	return nil
}

// MarshalJSON guarantees the arrays the schema marks required are emitted as
// `[]` rather than `null`.
func (p MachineCapabilityProfile) MarshalJSON() ([]byte, error) {
	type alias MachineCapabilityProfile
	out := alias(p)
	if out.AcceleratorCandidates == nil {
		out.AcceleratorCandidates = []AcceleratorCandidate{}
	}
	if out.Endpoints == nil {
		out.Endpoints = []CognitionEndpoint{}
	}
	return json.Marshal(out)
}
