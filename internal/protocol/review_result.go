package protocol

import "encoding/json"

// ReviewDimension names one independent review vector (DCI-043). Dimensions
// are separate records rather than sections of one review so that different
// models can hold different verdicts without being averaged together.
type ReviewDimension string

const (
	DimensionCorrectness     ReviewDimension = "correctness"
	DimensionArchitecture    ReviewDimension = "architecture"
	DimensionInvariants      ReviewDimension = "invariants"
	DimensionSecurity        ReviewDimension = "security"
	DimensionTestAdequacy    ReviewDimension = "test_adequacy"
	DimensionConcurrency     ReviewDimension = "concurrency"
	DimensionPerformance     ReviewDimension = "performance"
	DimensionMaintainability ReviewDimension = "maintainability"
	DimensionOther           ReviewDimension = "other"
)

// Valid reports whether the dimension is defined by the schema.
func (d ReviewDimension) Valid() bool {
	switch d {
	case DimensionCorrectness, DimensionArchitecture, DimensionInvariants, DimensionSecurity,
		DimensionTestAdequacy, DimensionConcurrency, DimensionPerformance,
		DimensionMaintainability, DimensionOther:
		return true
	}
	return false
}

// ReviewVerdict is a reviewer's overall judgement.
type ReviewVerdict string

const (
	VerdictPass           ReviewVerdict = "pass"
	VerdictConcern        ReviewVerdict = "concern"
	VerdictFail           ReviewVerdict = "fail"
	VerdictUnableToVerify ReviewVerdict = "unable_to_verify"
)

// Valid reports whether the verdict is defined by the schema.
func (v ReviewVerdict) Valid() bool {
	switch v {
	case VerdictPass, VerdictConcern, VerdictFail, VerdictUnableToVerify:
		return true
	}
	return false
}

// ComplianceStatus reports whether one piece of Work Package guidance was
// honoured. "unable_to_verify" is distinct from "satisfied" on purpose: an
// unverified MUST is not a satisfied MUST (DCI-012).
type ComplianceStatus string

const (
	ComplianceSatisfied      ComplianceStatus = "satisfied"
	ComplianceViolated       ComplianceStatus = "violated"
	ComplianceNotApplicable  ComplianceStatus = "not_applicable"
	ComplianceUnableToVerify ComplianceStatus = "unable_to_verify"
)

// Valid reports whether the status is defined by the schema.
func (c ComplianceStatus) Valid() bool {
	switch c {
	case ComplianceSatisfied, ComplianceViolated, ComplianceNotApplicable, ComplianceUnableToVerify:
		return true
	}
	return false
}

// Finding is one reviewer observation.
type Finding struct {
	Severity        Severity `json:"severity"`
	Statement       string   `json:"statement"`
	EvidenceRefs    []string `json:"evidence_refs,omitempty"`
	RepairRequested bool     `json:"repair_requested,omitempty"`
}

// GuidanceCompliance reports the fate of one Work Package guidance item.
type GuidanceCompliance struct {
	GuidanceID string           `json:"guidance_id"`
	Status     ComplianceStatus `json:"status"`
	Note       *string          `json:"note,omitempty"`
}

// ReviewResult is model-assisted evidence about a candidate change
// (docs/PROTOCOLS.md §11). It is never a substitute for deterministic
// validation (DCI-040); the control plane treats the two as separate signals.
type ReviewResult struct {
	SchemaVersion SchemaVersion   `json:"schema_version"`
	ReviewID      string          `json:"review_id"`
	ProjectID     string          `json:"project_id"`
	AttemptID     string          `json:"attempt_id"`
	WorkPackageID string          `json:"work_package_id"`
	Dimension     ReviewDimension `json:"dimension"`

	ReviewerProfile *string `json:"reviewer_profile,omitempty"`
	ModelIdentity   *string `json:"model_identity,omitempty"`

	Verdict        ReviewVerdict        `json:"verdict"`
	Findings       []Finding            `json:"findings"`
	MustCompliance []GuidanceCompliance `json:"must_compliance"`
	Deviations     []string             `json:"deviations,omitempty"`
	Uncertainties  []string             `json:"uncertainties,omitempty"`

	PrincipalEscalationRecommended bool `json:"principal_escalation_recommended"`
}

// RecordKind implements Record.
func (r *ReviewResult) RecordKind() string { return "ReviewResult" }

// RecordID implements Record.
func (r *ReviewResult) RecordID() string { return r.ReviewID }

// SchemaVer implements Record.
func (r *ReviewResult) SchemaVer() SchemaVersion { return r.SchemaVersion }

// Validate enforces schema constraints and enumerations.
func (r *ReviewResult) Validate() error {
	const kind = "ReviewResult"
	if err := r.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	for field, value := range map[string]string{
		"review_id":       r.ReviewID,
		"project_id":      r.ProjectID,
		"attempt_id":      r.AttemptID,
		"work_package_id": r.WorkPackageID,
	} {
		if err := requireNonEmpty(kind, field, value); err != nil {
			return err
		}
	}
	if !r.Dimension.Valid() {
		return enumError(kind, "dimension", string(r.Dimension), "correctness", "architecture",
			"invariants", "security", "test_adequacy", "concurrency", "performance",
			"maintainability", "other")
	}
	if !r.Verdict.Valid() {
		return enumError(kind, "verdict", string(r.Verdict), "pass", "concern", "fail", "unable_to_verify")
	}
	for _, f := range r.Findings {
		if !f.Severity.ValidFinding() {
			return enumError(kind, "findings[].severity", string(f.Severity),
				"info", "low", "medium", "high", "critical")
		}
		if err := requireNonEmpty(kind, "findings[].statement", f.Statement); err != nil {
			return err
		}
	}
	for _, c := range r.MustCompliance {
		if err := requireNonEmpty(kind, "must_compliance[].guidance_id", c.GuidanceID); err != nil {
			return err
		}
		if !c.Status.Valid() {
			return enumError(kind, "must_compliance[].status", string(c.Status),
				"satisfied", "violated", "not_applicable", "unable_to_verify")
		}
	}
	return nil
}

// MarshalJSON guarantees the required arrays serialise as `[]`, never `null`.
func (r ReviewResult) MarshalJSON() ([]byte, error) {
	type alias ReviewResult
	out := alias(r)
	out.Findings = orEmpty(out.Findings)
	out.MustCompliance = orEmpty(out.MustCompliance)
	return json.Marshal(out)
}
