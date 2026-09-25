package protocol

import (
	"github.com/olostan/DevCadence/internal/errs"
)

// ScopeKind identifies a concrete operational or cognition capability scope.
type ScopeKind string

const (
	ScopeCanExecuteSetupPlan       ScopeKind = "can_execute_setup_plan"
	ScopeHasAnyViableCognitionPath ScopeKind = "has_any_viable_cognition_path"
	ScopeCanRunLocalInference      ScopeKind = "can_run_local_inference"
	ScopeLocalModelAvailable       ScopeKind = "local_model_available"
	ScopeCanUseAuthenticatedCLI    ScopeKind = "can_use_existing_authenticated_cli"
	ScopePrincipalHostAvailable    ScopeKind = "principal_host_available"
)

// CanonicalScopes defines the stable deterministic ordering of scopes.
var CanonicalScopes = []ScopeKind{
	ScopeCanExecuteSetupPlan,
	ScopeHasAnyViableCognitionPath,
	ScopeCanRunLocalInference,
	ScopeLocalModelAvailable,
	ScopeCanUseAuthenticatedCLI,
	ScopePrincipalHostAvailable,
}

func (s ScopeKind) Valid() bool {
	switch s {
	case ScopeCanExecuteSetupPlan,
		ScopeHasAnyViableCognitionPath,
		ScopeCanRunLocalInference,
		ScopeLocalModelAvailable,
		ScopeCanUseAuthenticatedCLI,
		ScopePrincipalHostAvailable:
		return true
	}
	return false
}

// ScopeReadinessStatus represents the factual readiness of a specific scope.
type ScopeReadinessStatus string

const (
	ScopeStatusReady       ScopeReadinessStatus = "ready"
	ScopeStatusNotReady    ScopeReadinessStatus = "not_ready"
	ScopeStatusUnavailable ScopeReadinessStatus = "unavailable"
	ScopeStatusUnknown     ScopeReadinessStatus = "unknown"
)

func (s ScopeReadinessStatus) Valid() bool {
	switch s {
	case ScopeStatusReady, ScopeStatusNotReady, ScopeStatusUnavailable, ScopeStatusUnknown:
		return true
	}
	return false
}

// ScopeReadiness captures the evidence-backed readiness of a specific capability.
type ScopeReadiness struct {
	Scope  ScopeKind            `json:"scope"`
	Status ScopeReadinessStatus `json:"status"`
	Reason string               `json:"reason"`
}

// Validate checks the scope readiness entry is well-formed.
func (s ScopeReadiness) Validate() error {
	const kind = "ScopeReadiness"
	if !s.Scope.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid scope %q", kind, string(s.Scope))
	}
	if !s.Status.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid status %q", kind, string(s.Status))
	}
	if s.Reason == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: reason is required", kind)
	}
	return nil
}

// EndpointViable is the single authoritative definition of "this endpoint
// can actually be used", not merely healthy — the WP-M3B-4 invariant
// "healthy != authenticated != usable" applies exactly as much to
// aggregate readiness as it does to per-credential evidence. A local
// runtime is usable once ready (auth is not applicable to it); a
// remote/CLI endpoint additionally requires verified authentication.
// Centralized here so scope readiness and monolithic DoctorReport
// readiness cannot silently disagree about what "viable" means
// (independent-review follow-up on WP-M3B-5, finding 5).
func EndpointViable(kind EndpointKind, locality Locality, health EndpointHealth, auth AuthStatus) bool {
	if health != EndpointHealthReady {
		return false
	}
	if kind == EndpointLocalRuntime {
		return true
	}
	return auth == AuthAuthenticated || auth == AuthNotApplicable
}

// EvaluateScopeReadiness deterministically evaluates the canonical capability scopes
// from verified findings, endpoints, and hosts.
func EvaluateScopeReadiness(
	findings []DiagnosticFinding,
	endpoints []CognitionEndpointSummary,
	hosts []PrincipalHostSummary,
) []ScopeReadiness {
	// 1. can_execute_setup_plan
	setupStatus := ScopeStatusReady
	setupReason := "Git and writable state root are available for setup execution"
	for _, f := range findings {
		if f.Severity == "error" || f.Code == "GIT_NOT_FOUND" || f.Code == "STATE_ROOT_UNWRITABLE" {
			setupStatus = ScopeStatusNotReady
			setupReason = "Base dependencies (Git or state root writability) require remediation"
			break
		}
	}

	// 2. has_any_viable_cognition_path
	cogStatus := ScopeStatusUnavailable
	cogReason := "No cognition endpoints detected on machine"
	hasViableEndpoint := false
	hasUnknownAuthEndpoint := false
	for _, ep := range endpoints {
		if EndpointViable(ep.Kind, ep.Locality, ep.Health, ep.Auth) {
			hasViableEndpoint = true
			break
		}
		if ep.Health == EndpointHealthReady && ep.Auth == AuthUnknown {
			hasUnknownAuthEndpoint = true
		}
	}
	if hasViableEndpoint {
		cogStatus = ScopeStatusReady
		cogReason = "At least one ready and authenticated/usable cognition endpoint is available"
	} else if hasUnknownAuthEndpoint {
		cogStatus = ScopeStatusUnknown
		cogReason = "A ready endpoint exists but its authentication has not yet been verified"
	} else if len(endpoints) > 0 {
		cogStatus = ScopeStatusNotReady
		cogReason = "Cognition endpoints are present but none are ready and authenticated/usable"
	}

	// 3. can_run_local_inference
	localInfStatus := ScopeStatusUnavailable
	localInfReason := "No local inference runtime installed"
	hasLocalRuntime := false
	hasReadyLocal := false
	for _, ep := range endpoints {
		if ep.Kind == EndpointLocalRuntime {
			hasLocalRuntime = true
			if ep.Health == EndpointHealthReady {
				hasReadyLocal = true
				break
			}
		}
	}
	if hasReadyLocal {
		localInfStatus = ScopeStatusReady
		localInfReason = "Local runtime is running with a verified usable model"
	} else if hasLocalRuntime {
		localInfStatus = ScopeStatusNotReady
		localInfReason = "Local runtime is present but not configured or model is not pulled"
	}

	// 4. local_model_available
	modelStatus := ScopeStatusUnavailable
	modelReason := "No local inference runtime installed"
	if hasReadyLocal {
		modelStatus = ScopeStatusReady
		modelReason = "Verified usable local model present in runtime"
	} else if hasLocalRuntime {
		modelStatus = ScopeStatusNotReady
		modelReason = "No verified usable model installed in local runtime"
	}

	// 5. can_use_existing_authenticated_cli
	cliStatus := ScopeStatusUnavailable
	cliReason := "No supported coding CLI installed"
	hasCLI := false
	hasAuthCLI := false
	hasUnknownAuthCLI := false
	for _, ep := range endpoints {
		if ep.Kind == EndpointAuthenticatedCLI {
			hasCLI = true
			if EndpointViable(ep.Kind, ep.Locality, ep.Health, ep.Auth) {
				hasAuthCLI = true
				break
			}
			if ep.Health == EndpointHealthReady && ep.Auth == AuthUnknown {
				hasUnknownAuthCLI = true
			}
		}
	}
	if hasAuthCLI {
		cliStatus = ScopeStatusReady
		cliReason = "Authenticated coding CLI available and operational"
	} else if hasUnknownAuthCLI {
		cliStatus = ScopeStatusUnknown
		cliReason = "Coding CLI is ready but its authentication has not yet been verified"
	} else if hasCLI {
		cliStatus = ScopeStatusNotReady
		cliReason = "Coding CLI detected but authentication is expired or unverified"
	}

	// 6. principal_host_available
	hostStatus := ScopeStatusUnavailable
	hostReason := "No supported principal host installed"
	for _, h := range hosts {
		if h.Installed {
			hostStatus = ScopeStatusReady
			hostReason = "Supported principal host installed"
			break
		}
	}

	return []ScopeReadiness{
		{Scope: ScopeCanExecuteSetupPlan, Status: setupStatus, Reason: setupReason},
		{Scope: ScopeHasAnyViableCognitionPath, Status: cogStatus, Reason: cogReason},
		{Scope: ScopeCanRunLocalInference, Status: localInfStatus, Reason: localInfReason},
		{Scope: ScopeLocalModelAvailable, Status: modelStatus, Reason: modelReason},
		{Scope: ScopeCanUseAuthenticatedCLI, Status: cliStatus, Reason: cliReason},
		{Scope: ScopePrincipalHostAvailable, Status: hostStatus, Reason: hostReason},
	}
}

// HardwareSummary is a compact, non-duplicative projection of
// EnvironmentFacts. It deliberately does not re-embed the full facts
// struct — those are looked up by MachineFingerprint when needed, per
// ADR-0013's "machine profiles are computed rather than persisted"
// discipline, which this type follows rather than becoming a second
// source of truth for hardware facts.
type HardwareSummary struct {
	OSFamily            OSFamily      `json:"os_family"`
	Arch                string        `json:"arch"`
	LogicalCores        int           `json:"logical_cores"`
	TotalMemoryBytes    *int64        `json:"total_memory_bytes,omitempty"`
	AcceleratorBackends []BackendKind `json:"accelerator_backends,omitempty"`
}

// Validate checks the hardware summary is well-formed.
func (h HardwareSummary) Validate() error {
	const kind = "HardwareSummary"
	if !h.OSFamily.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid os_family %q", kind, string(h.OSFamily))
	}
	if h.Arch == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: arch is required", kind)
	}
	if h.LogicalCores < 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: logical_cores must not be negative", kind)
	}
	if h.TotalMemoryBytes != nil && *h.TotalMemoryBytes < 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: total_memory_bytes must not be negative", kind)
	}
	for _, b := range h.AcceleratorBackends {
		if !b.Valid() {
			return enumError(kind, "accelerator_backends[]", string(b),
				"cpu", "metal", "cuda", "rocm", "vulkan", "unknown")
		}
	}
	return nil
}

// CredentialInventoryEntry pairs a credential reference with its most
// recent authentication evidence — both exactly the WP-M3B-4 types, so
// this introduces no new secret-adjacent field (DCI-081).
type CredentialInventoryEntry struct {
	Ref      CredentialRef `json:"ref"`
	Evidence AuthEvidence  `json:"evidence"`
}

// Validate checks the entry and that the evidence actually describes the
// paired reference (same RefID/Kind), so an inventory can never pair a
// reference with evidence that was observed for a different credential.
func (e CredentialInventoryEntry) Validate() error {
	const kind = "CredentialInventoryEntry"
	if err := e.Ref.Validate(); err != nil {
		return err
	}
	if err := e.Evidence.Validate(); err != nil {
		return err
	}
	if e.Evidence.RefID != e.Ref.RefID {
		return errs.New(errs.CategoryInvalidArgument,
			"%s: evidence.ref_id %q does not match ref.ref_id %q", kind, e.Evidence.RefID, e.Ref.RefID)
	}
	if e.Evidence.Kind != e.Ref.Kind {
		return errs.New(errs.CategoryInvalidArgument,
			"%s: evidence.kind %q does not match ref.kind %q", kind, e.Evidence.Kind, e.Ref.Kind)
	}
	return nil
}

// PolicySummary is the "available economic/policy metadata" the
// WP-M3B-5 scope card asks for, at the fidelity that actually exists
// today (the WP-M3A cognition.Policy routing policy) — not the M3C
// EconomicRegime/BudgetPool types, which are not yet built. A nil
// PolicySummary on ResourceInventory means no active policy override;
// callers apply the same default-policy fallback evaluateReadiness and
// ProfileRecommender.Recommend already use.
type PolicySummary struct {
	MaxSourceExposure SourceExposure `json:"max_source_exposure"`
	MaxCostClass      CostClass      `json:"max_cost_class"`
}

// Validate checks the policy summary is well-formed.
func (p PolicySummary) Validate() error {
	const kind = "PolicySummary"
	if !p.MaxSourceExposure.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid max_source_exposure %q", kind, string(p.MaxSourceExposure))
	}
	if !p.MaxCostClass.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid max_cost_class %q", kind, string(p.MaxCostClass))
	}
	return nil
}

// MachineProfileRef is a typed pointer back to the MachineCapabilityProfile
// observation that produced a ResourceInventory — profile_id, machine
// fingerprint, observation time, and probe depth, without duplicating the
// full profile (its endpoints, capability grades, provenance, and
// runtime/model identity). ADR-0013 already established that machine
// profiles are computed rather than persisted, and are cached/retrievable
// by (MachineFingerprint, ProbeDepth); this reference is what lets a
// future M3C/M3D consumer recover that full factual substrate instead of
// being stuck with only CognitionEndpointSummary's deliberately-reduced
// fields (independent-review follow-up on WP-M3B-5, finding 2: the
// inventory was too lossy to satisfy the canonical "runtimes/models,
// capability provenance" contract without this link).
type MachineProfileRef struct {
	ProfileID          string     `json:"profile_id"`
	MachineFingerprint string     `json:"machine_fingerprint"`
	ObservedAt         Timestamp  `json:"observed_at"`
	ProbeDepth         ProbeDepth `json:"probe_depth"`
}

// Validate checks the profile reference is well-formed.
func (p MachineProfileRef) Validate() error {
	const kind = "MachineProfileRef"
	if err := requireNonEmpty(kind, "profile_id", p.ProfileID); err != nil {
		return err
	}
	if !hexSha256Regex.MatchString(p.MachineFingerprint) {
		return errs.New(errs.CategoryInvalidArgument, "%s: machine_fingerprint must be sha256 hex, got %q", kind, p.MachineFingerprint)
	}
	if p.ObservedAt.Time().IsZero() {
		return errs.New(errs.CategoryInvalidArgument, "%s: observed_at is required", kind)
	}
	if !p.ProbeDepth.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid probe_depth %q", kind, string(p.ProbeDepth))
	}
	return nil
}

// ResourceInventory is a deterministic, point-in-time snapshot of the
// facts M3C/M3D's Portfolio Planner will read — never a decision itself
// (ADR-0014 §92, ADR-0018). It references MachineCapabilityProfile (via
// Profile, a MachineProfileRef) and CognitionEndpointSummary by the same
// provenance-carrying shapes those types already use (ADR-0011) rather
// than re-deriving capability grades, and follows ADR-0013's "computed,
// not persisted" discipline: callers key freshness off
// MachineFingerprint/ObservedAt exactly as DoctorReport already does. It
// has no Recommend/Select/Score method — it decides nothing; a future
// consumer reads it and decides.
type ResourceInventory struct {
	SchemaVersion      SchemaVersion   `json:"schema_version"`
	InventoryID        string          `json:"inventory_id"`
	MachineFingerprint string          `json:"machine_fingerprint"`
	ObservedAt         Timestamp       `json:"observed_at"`
	Hardware           HardwareSummary `json:"hardware"`
	// Profile references the full MachineCapabilityProfile this inventory
	// was projected from — nil only when no cognition discovery ran at all
	// (e.g. Doctor configured without a CognitionService).
	Profile            *MachineProfileRef         `json:"profile,omitempty"`
	CognitionEndpoints []CognitionEndpointSummary `json:"cognition_endpoints,omitempty"`
	PrincipalHosts     []PrincipalHostSummary     `json:"principal_hosts,omitempty"`
	Credentials        []CredentialInventoryEntry `json:"credentials,omitempty"`
	Readiness          []ScopeReadiness           `json:"readiness,omitempty"`
	Policy             *PolicySummary             `json:"policy,omitempty"`
}

// RecordKind implements Record.
func (r *ResourceInventory) RecordKind() string { return "ResourceInventory" }

// RecordID implements Record.
func (r *ResourceInventory) RecordID() string { return r.InventoryID }

// SchemaVer implements Record.
func (r *ResourceInventory) SchemaVer() SchemaVersion { return r.SchemaVersion }

// Validate checks the inventory is well-formed and internally consistent.
func (r *ResourceInventory) Validate() error {
	const kind = "ResourceInventory"
	if err := r.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "inventory_id", r.InventoryID); err != nil {
		return err
	}
	if !hexSha256Regex.MatchString(r.MachineFingerprint) {
		return errs.New(errs.CategoryInvalidArgument, "%s: machine_fingerprint must be sha256 hex, got %q", kind, r.MachineFingerprint)
	}
	if r.ObservedAt.Time().IsZero() {
		return errs.New(errs.CategoryInvalidArgument, "%s: observed_at is required", kind)
	}
	if err := r.Hardware.Validate(); err != nil {
		return err
	}
	if len(r.CognitionEndpoints) > 0 && r.Profile == nil {
		return errs.New(errs.CategoryInvalidArgument,
			"%s: profile reference is required when cognition endpoints are present to preserve runtime/model and capability provenance", kind)
	}
	if r.Profile != nil {
		if err := r.Profile.Validate(); err != nil {
			return err
		}
		if r.Profile.MachineFingerprint != r.MachineFingerprint {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: profile.machine_fingerprint %q does not match inventory machine_fingerprint %q",
				kind, r.Profile.MachineFingerprint, r.MachineFingerprint)
		}
	}
	seenEndpoints := make(map[string]bool, len(r.CognitionEndpoints))
	for _, ep := range r.CognitionEndpoints {
		if err := ep.Validate(); err != nil {
			return err
		}
		if seenEndpoints[ep.ID] {
			return errs.New(errs.CategoryInvalidArgument, "%s: duplicate endpoint %q in cognition_endpoints", kind, ep.ID)
		}
		seenEndpoints[ep.ID] = true
	}
	seenHosts := make(map[string]bool, len(r.PrincipalHosts))
	for _, h := range r.PrincipalHosts {
		if err := h.Validate(); err != nil {
			return err
		}
		if seenHosts[h.HostID] {
			return errs.New(errs.CategoryInvalidArgument, "%s: duplicate host %q in principal_hosts", kind, h.HostID)
		}
		seenHosts[h.HostID] = true
	}
	seenCreds := make(map[string]bool, len(r.Credentials))
	for _, c := range r.Credentials {
		if err := c.Validate(); err != nil {
			return err
		}
		if seenCreds[c.Ref.RefID] {
			return errs.New(errs.CategoryInvalidArgument, "%s: duplicate credential ref_id %q in credentials", kind, c.Ref.RefID)
		}
		seenCreds[c.Ref.RefID] = true
	}
	// When Credentials is populated, it is the authoritative binding
	// substrate for this inventory: an endpoint claiming a CredentialRef
	// that names no entry there would let the durable inventory contradict
	// itself (a binding the inventory itself cannot back). Credentials
	// being empty is not itself an error — an inventory can validly omit
	// credential observation entirely — so this check only activates once
	// there is something to be inconsistent with (independent-review
	// follow-up on WP-M3B-5, round-5 finding 1).
	if len(r.Credentials) > 0 {
		for _, ep := range r.CognitionEndpoints {
			if ep.CredentialRef != "" && !seenCreds[ep.CredentialRef] {
				return errs.New(errs.CategoryInvalidArgument,
					"%s: cognition_endpoints[%s].credential_ref %q does not match any entry in credentials", kind, ep.ID, ep.CredentialRef)
			}
		}
	}
	seenScopes := make(map[ScopeKind]bool, len(r.Readiness))
	for _, rd := range r.Readiness {
		if err := rd.Validate(); err != nil {
			return err
		}
		if seenScopes[rd.Scope] {
			return errs.New(errs.CategoryInvalidArgument, "%s: duplicate scope %q in readiness", kind, rd.Scope)
		}
		seenScopes[rd.Scope] = true
	}
	if r.Policy != nil {
		if err := r.Policy.Validate(); err != nil {
			return err
		}
	}
	return nil
}
