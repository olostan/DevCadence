package protocol

import (
	"github.com/olostan/DevCadence/internal/errs"
)

// ReadinessStatus indicates overall readiness against the evaluated scope.
//
// READY does not require EvaluationScope.TargetProfile to be set. An
// earlier revision required it, silently making the informational
// DeploymentProfile label an authority over canonical readiness — exactly
// what ADR-0014 §92 forbids. Canonical readiness is evidence-driven
// (ScopeReadiness / at least one viable cognition path), never gated by
// whether a UX label happened to be chosen (independent-review follow-up
// on WP-M3B-5, finding 1).
type ReadinessStatus string

const (
	ReadinessReady               ReadinessStatus = "READY"
	ReadinessReadyWithReducedCap ReadinessStatus = "READY_WITH_REDUCED_CAPABILITY"
	ReadinessPartiallyReady      ReadinessStatus = "PARTIALLY_READY"
	ReadinessActionRequired      ReadinessStatus = "ACTION_REQUIRED"
)

func (s ReadinessStatus) Valid() bool {
	switch s {
	case ReadinessReady, ReadinessReadyWithReducedCap, ReadinessPartiallyReady, ReadinessActionRequired:
		return true
	}
	return false
}

// DeploymentProfile identifies a standard deployment template.
type DeploymentProfile string

const (
	ProfileLocalHeavy     DeploymentProfile = "local-heavy"
	ProfileHybridThin     DeploymentProfile = "hybrid-thin"
	ProfileCloudCognition DeploymentProfile = "cloud-cognition"
	ProfileOffline        DeploymentProfile = "offline"
	ProfileCustom         DeploymentProfile = "custom"
)

func (p DeploymentProfile) Valid() bool {
	switch p {
	case ProfileLocalHeavy, ProfileHybridThin, ProfileCloudCognition, ProfileOffline, ProfileCustom:
		return true
	}
	return false
}

type ProfileAlternative struct {
	Profile  DeploymentProfile `json:"profile"`
	Eligible bool              `json:"eligible"`
	Reasons  []string          `json:"reasons"`
	Missing  []string          `json:"missing_prerequisites,omitempty"`
}

func (a ProfileAlternative) Validate() error {
	const kind = "ProfileAlternative"
	if !a.Profile.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid profile %q", kind, string(a.Profile))
	}
	if len(a.Reasons) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: reasons cannot be empty", kind)
	}
	return nil
}

type ProfileRecommendation struct {
	SelectedProfile *DeploymentProfile   `json:"selected_profile,omitempty"`
	Rationale       []string             `json:"rationale"`
	Alternatives    []ProfileAlternative `json:"alternatives"`
	Limitations     []string             `json:"limitations,omitempty"`
	Unknowns        []string             `json:"unknowns,omitempty"`
}

func (r ProfileRecommendation) Validate() error {
	const kind = "ProfileRecommendation"
	if r.SelectedProfile != nil && !r.SelectedProfile.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid selected_profile %q", kind, string(*r.SelectedProfile))
	}
	if len(r.Rationale) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: rationale cannot be empty", kind)
	}
	for _, alt := range r.Alternatives {
		if err := alt.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type ReadinessEvaluationScope struct {
	TargetProfile  *DeploymentProfile `json:"target_profile,omitempty"`
	RequiredRoles  []string           `json:"required_roles"`
	EvidenceStatus string             `json:"evidence_status"`
}

func (s ReadinessEvaluationScope) Validate() error {
	const kind = "ReadinessEvaluationScope"
	if s.TargetProfile != nil && !s.TargetProfile.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid target_profile %q", kind, string(*s.TargetProfile))
	}
	switch s.EvidenceStatus {
	case "live", "refreshed_health", "stale_inference_retained":
		// valid
	default:
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid evidence_status %q", kind, s.EvidenceStatus)
	}
	return nil
}

type DiagnosticFinding struct {
	Category    string  `json:"category"`
	Severity    string  `json:"severity"`
	Code        string  `json:"code"`
	Title       string  `json:"title"`
	Detail      string  `json:"detail"`
	Remediation *string `json:"remediation,omitempty"`
}

func (f DiagnosticFinding) Validate() error {
	const kind = "DiagnosticFinding"
	switch f.Category {
	case "environment", "hardware", "runtime", "cognition", "auth", "state":
		// valid
	default:
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid category %q", kind, f.Category)
	}
	switch f.Severity {
	case "info", "warning", "error":
		// valid
	default:
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid severity %q", kind, f.Severity)
	}
	if f.Code == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: code is required", kind)
	}
	if f.Title == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: title is required", kind)
	}
	if f.Detail == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: detail is required", kind)
	}
	return nil
}

type PrincipalHostSummary struct {
	HostID    string `json:"host_id"`
	Installed bool   `json:"installed"`
	Path      string `json:"path,omitempty"`
	Version   string `json:"version,omitempty"`
}

func (h PrincipalHostSummary) Validate() error {
	const kind = "PrincipalHostSummary"
	switch h.HostID {
	case "antigravity", "cursor", "vscode":
		// valid
	default:
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid host_id %q", kind, h.HostID)
	}
	return nil
}

type DoctorReport struct {
	SchemaVersion       SchemaVersion              `json:"schema_version"`
	ReportID            string                     `json:"report_id"`
	MachineFingerprint  string                     `json:"machine_fingerprint"`
	ObservedAt          Timestamp                  `json:"observed_at"`
	EvaluationScope     ReadinessEvaluationScope   `json:"evaluation_scope"`
	Readiness           ReadinessStatus            `json:"readiness"`
	Findings            []DiagnosticFinding        `json:"findings"`
	ScopeReadiness      []ScopeReadiness           `json:"scope_readiness,omitempty"`
	ResourceInventory   *ResourceInventory         `json:"resource_inventory,omitempty"`
	RecommendedProfile  *ProfileRecommendation     `json:"recommended_profile,omitempty"`
	DiscoveredEndpoints []CognitionEndpointSummary `json:"discovered_endpoints,omitempty"`
	PrincipalHosts      []PrincipalHostSummary     `json:"principal_hosts,omitempty"`
}

func (r *DoctorReport) RecordKind() string       { return "DoctorReport" }
func (r *DoctorReport) RecordID() string         { return r.ReportID }
func (r *DoctorReport) SchemaVer() SchemaVersion { return r.SchemaVersion }

func (r *DoctorReport) Validate() error {
	const kind = "DoctorReport"
	if err := r.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "report_id", r.ReportID); err != nil {
		return err
	}
	if !hexSha256Regex.MatchString(r.MachineFingerprint) {
		return errs.New(errs.CategoryInvalidArgument, "%s: machine_fingerprint must be sha256 hex, got %q", kind, r.MachineFingerprint)
	}
	if r.ObservedAt.Time().IsZero() {
		return errs.New(errs.CategoryInvalidArgument, "%s: observed_at is required", kind)
	}
	if err := r.EvaluationScope.Validate(); err != nil {
		return err
	}
	if !r.Readiness.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid readiness %q", kind, string(r.Readiness))
	}
	for _, f := range r.Findings {
		if err := f.Validate(); err != nil {
			return err
		}
	}
	seenScopes := make(map[ScopeKind]bool, len(r.ScopeReadiness))
	for _, sr := range r.ScopeReadiness {
		if err := sr.Validate(); err != nil {
			return err
		}
		if seenScopes[sr.Scope] {
			return errs.New(errs.CategoryInvalidArgument, "%s: duplicate scope %q in scope_readiness", kind, sr.Scope)
		}
		seenScopes[sr.Scope] = true
	}
	seenEndpoints := make(map[string]bool, len(r.DiscoveredEndpoints))
	for _, ep := range r.DiscoveredEndpoints {
		if err := ep.Validate(); err != nil {
			return err
		}
		if seenEndpoints[ep.ID] {
			return errs.New(errs.CategoryInvalidArgument, "%s: duplicate endpoint %q in discovered_endpoints", kind, ep.ID)
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
	if r.ResourceInventory != nil {
		if err := r.ResourceInventory.Validate(); err != nil {
			return err
		}
		// The report and its embedded inventory each carry a
		// machine_fingerprint/readiness/endpoint/host snapshot; they must not
		// silently contradict each other (independent-review follow-up on
		// WP-M3B-5, findings 3c and 4d).
		if r.ResourceInventory.MachineFingerprint != r.MachineFingerprint {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: resource_inventory.machine_fingerprint %q does not match report machine_fingerprint %q",
				kind, r.ResourceInventory.MachineFingerprint, r.MachineFingerprint)
		}
		if len(r.ScopeReadiness) > 0 && !scopeReadinessSetsEqual(r.ScopeReadiness, r.ResourceInventory.Readiness) {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: scope_readiness does not match resource_inventory.readiness", kind)
		}
		if len(r.DiscoveredEndpoints) > 0 || len(r.ResourceInventory.CognitionEndpoints) > 0 {
			if !cognitionEndpointsEqual(r.DiscoveredEndpoints, r.ResourceInventory.CognitionEndpoints) {
				return errs.New(errs.CategoryInvalidArgument,
					"%s: discovered_endpoints does not match resource_inventory.cognition_endpoints", kind)
			}
		}
		if len(r.PrincipalHosts) > 0 || len(r.ResourceInventory.PrincipalHosts) > 0 {
			if !principalHostsEqual(r.PrincipalHosts, r.ResourceInventory.PrincipalHosts) {
				return errs.New(errs.CategoryInvalidArgument,
					"%s: principal_hosts does not match resource_inventory.principal_hosts", kind)
			}
		}
	}
	if r.RecommendedProfile != nil {
		if err := r.RecommendedProfile.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// scopeReadinessSetsEqual reports whether two ScopeReadiness slices agree
// on every scope's status/reason, ignoring order — used to keep
// DoctorReport.ScopeReadiness and its embedded ResourceInventory.Readiness
// from silently contradicting each other (finding 4d above). Both slices
// are already individually validated (no duplicate scopes) by the time
// this runs.
func scopeReadinessSetsEqual(a, b []ScopeReadiness) bool {
	if len(a) != len(b) {
		return false
	}
	byScope := make(map[ScopeKind]ScopeReadiness, len(a))
	for _, sr := range a {
		byScope[sr.Scope] = sr
	}
	for _, sr := range b {
		other, ok := byScope[sr.Scope]
		if !ok || other.Status != sr.Status || other.Reason != sr.Reason {
			return false
		}
	}
	return true
}

func cognitionEndpointsEqual(a, b []CognitionEndpointSummary) bool {
	if len(a) != len(b) {
		return false
	}
	byID := make(map[string]CognitionEndpointSummary, len(a))
	for _, ep := range a {
		byID[ep.ID] = ep
	}
	for _, ep := range b {
		other, ok := byID[ep.ID]
		if !ok {
			return false
		}
		if other.Kind != ep.Kind ||
			other.Locality != ep.Locality ||
			other.Health != ep.Health ||
			other.Auth != ep.Auth ||
			other.CostClass != ep.CostClass ||
			other.RequiredSourceExposure != ep.RequiredSourceExposure ||
			other.AccelerationVerified != ep.AccelerationVerified {
			return false
		}
		if (other.AccelerationBackend == nil) != (ep.AccelerationBackend == nil) {
			return false
		}
		if other.AccelerationBackend != nil && *other.AccelerationBackend != *ep.AccelerationBackend {
			return false
		}
	}
	return true
}

func principalHostsEqual(a, b []PrincipalHostSummary) bool {
	if len(a) != len(b) {
		return false
	}
	byID := make(map[string]PrincipalHostSummary, len(a))
	for _, h := range a {
		byID[h.HostID] = h
	}
	for _, h := range b {
		other, ok := byID[h.HostID]
		if !ok || other.Installed != h.Installed || other.Path != h.Path {
			return false
		}
	}
	return true
}
