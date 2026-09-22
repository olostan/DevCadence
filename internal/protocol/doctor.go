package protocol

import (
	"github.com/olostan/DevCadence/internal/errs"
)

// ReadinessStatus indicates overall readiness against the evaluated scope.
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
	SchemaVersion       SchemaVersion            `json:"schema_version"`
	ReportID            string                   `json:"report_id"`
	MachineFingerprint  string                   `json:"machine_fingerprint"`
	ObservedAt          Timestamp                `json:"observed_at"`
	EvaluationScope     ReadinessEvaluationScope `json:"evaluation_scope"`
	Readiness           ReadinessStatus          `json:"readiness"`
	Findings            []DiagnosticFinding      `json:"findings"`
	RecommendedProfile  *ProfileRecommendation   `json:"recommended_profile,omitempty"`
	DiscoveredEndpoints []CognitionEndpointSummary `json:"discovered_endpoints,omitempty"`
	PrincipalHosts      []PrincipalHostSummary   `json:"principal_hosts,omitempty"`
}

func (r *DoctorReport) RecordKind() string         { return "DoctorReport" }
func (r *DoctorReport) RecordID() string           { return r.ReportID }
func (r *DoctorReport) SchemaVer() SchemaVersion   { return r.SchemaVersion }

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
	// Profile-level READY requires an evaluated target profile (ADR-0014)
	if r.Readiness == ReadinessReady && r.EvaluationScope.TargetProfile == nil {
		return errs.New(errs.CategoryInvalidArgument, "%s: cannot claim READY without an explicit evaluated target_profile", kind)
	}

	for _, f := range r.Findings {
		if err := f.Validate(); err != nil {
			return err
		}
	}
	if r.RecommendedProfile != nil {
		if err := r.RecommendedProfile.Validate(); err != nil {
			return err
		}
	}
	for _, ep := range r.DiscoveredEndpoints {
		if ep.ID == "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: endpoint id cannot be empty", kind)
		}
		if !ep.Kind.Valid() {
			return errs.New(errs.CategoryInvalidArgument, "%s: invalid endpoint kind %q", kind, ep.Kind)
		}
	}
	for _, h := range r.PrincipalHosts {
		if err := h.Validate(); err != nil {
			return err
		}
	}
	return nil
}
