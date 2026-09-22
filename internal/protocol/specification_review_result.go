package protocol

import (
	"encoding/json"

	"github.com/olostan/DevCadence/internal/errs"
)

// SpecificationReviewDimension names one independent specification-review
// vector, as prompts/specification-reviewer.md defines them.
//
// These are deliberately *not* ReviewDimension. A specification review asks
// whether the intent is well enough understood to build from; an
// implementation review asks whether a candidate satisfies a blueprint. They
// happen at different times, judge different artifacts, and a reviewer
// assigned "ambiguity" is doing nothing an assigned "concurrency" reviewer
// would recognise.
type SpecificationReviewDimension string

const (
	SpecCompleteness              SpecificationReviewDimension = "completeness"
	SpecAmbiguity                 SpecificationReviewDimension = "ambiguity"
	SpecContradiction             SpecificationReviewDimension = "contradiction"
	SpecArchitectureContamination SpecificationReviewDimension = "architecture_contamination"
	SpecSecurityPrivacy           SpecificationReviewDimension = "security_privacy"
	SpecFailureModes              SpecificationReviewDimension = "failure_modes"
	SpecOperations                SpecificationReviewDimension = "operations"
	SpecUXMentalModel             SpecificationReviewDimension = "ux_mental_model"
)

// Valid reports whether the dimension is defined by the schema.
func (d SpecificationReviewDimension) Valid() bool {
	switch d {
	case SpecCompleteness, SpecAmbiguity, SpecContradiction, SpecArchitectureContamination,
		SpecSecurityPrivacy, SpecFailureModes, SpecOperations, SpecUXMentalModel:
		return true
	}
	return false
}

// SpecificationFinding is one specification-review observation.
//
// It carries a resolution authority and a recommended question because the
// point of a specification review is to route an unknown to whoever can
// actually settle it (DCI-008): a finding nobody owns is an observation, not
// a step towards readiness.
type SpecificationFinding struct {
	Severity     Severity `json:"severity"`
	Statement    string   `json:"statement"`
	WhyItMatters string   `json:"why_it_matters"`
	// AffectedRefs cite the requirement, decision or model section that
	// creates the issue, so a finding can be traced to what it is about.
	AffectedRefs        []string            `json:"affected_refs,omitempty"`
	ResolutionAuthority ResolutionAuthority `json:"resolution_authority"`
	RecommendedQuestion string              `json:"recommended_question,omitempty"`
	EvidenceRefs        []string            `json:"evidence_refs,omitempty"`
	// BlocksReadiness is the reviewer's judgement that the Design Readiness
	// Gate must not pass while this stands (DCI-016).
	BlocksReadiness bool `json:"blocks_readiness,omitempty"`
}

// SpecificationReviewResult is independent evidence about a specification,
// produced before implementation exists (docs/DISCOVERY_AND_SPECIFICATION.md
// §"Independent specification review").
//
// It pins the ProblemModel revision it judged, for the same reason
// SpecificationReadiness does: a verdict about revision 7 says nothing about
// revision 8, and a review that could not name what it read could not be
// invalidated when that changed.
type SpecificationReviewResult struct {
	SchemaVersion SchemaVersion `json:"schema_version"`
	ReviewID      string        `json:"review_id"`
	ProjectID     string        `json:"project_id"`

	ProblemModelID       string `json:"problem_model_id"`
	ProblemModelRevision int    `json:"problem_model_revision"`

	Dimension       SpecificationReviewDimension `json:"dimension"`
	ReviewerProfile string                       `json:"reviewer_profile"`
	ModelIdentity   *string                      `json:"model_identity,omitempty"`

	Verdict  ReviewVerdict          `json:"verdict"`
	Findings []SpecificationFinding `json:"findings"`
	// MaterialGapRefs name the ambiguities this review opened, keeping the
	// link from review to reopened question durable.
	MaterialGapRefs []string `json:"material_gap_refs,omitempty"`
	Summary         string   `json:"summary"`
}

// RecordKind implements Record.
func (r *SpecificationReviewResult) RecordKind() string { return "SpecificationReviewResult" }

// RecordID implements Record.
func (r *SpecificationReviewResult) RecordID() string { return r.ReviewID }

// SchemaVer implements Record.
func (r *SpecificationReviewResult) SchemaVer() SchemaVersion { return r.SchemaVersion }

// ProjectOf implements ProjectScoped.
func (r *SpecificationReviewResult) ProjectOf() string { return r.ProjectID }

// Validate enforces schema constraints and the consistency rules that keep a
// verdict from being weaker than its own findings.
func (r *SpecificationReviewResult) Validate() error {
	const kind = "SpecificationReviewResult"
	if err := r.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	for field, value := range map[string]string{
		"review_id":        r.ReviewID,
		"project_id":       r.ProjectID,
		"problem_model_id": r.ProblemModelID,
		"reviewer_profile": r.ReviewerProfile,
		"summary":          r.Summary,
	} {
		if err := requireNonEmpty(kind, field, value); err != nil {
			return err
		}
	}
	if r.ProblemModelRevision < 1 {
		return enumError(kind, "problem_model_revision", "(not set)", "a revision of at least 1")
	}
	if !r.Dimension.Valid() {
		return enumError(kind, "dimension", string(r.Dimension),
			"completeness", "ambiguity", "contradiction", "architecture_contamination",
			"security_privacy", "failure_modes", "operations", "ux_mental_model")
	}
	if !r.Verdict.Valid() {
		return enumError(kind, "verdict", string(r.Verdict), "pass", "concern", "fail", "unable_to_verify")
	}
	blocking := false
	for _, finding := range r.Findings {
		if err := requireNonEmpty(kind, "findings[].statement", finding.Statement); err != nil {
			return err
		}
		if err := requireNonEmpty(kind, "findings[].why_it_matters", finding.WhyItMatters); err != nil {
			return err
		}
		if !finding.Severity.ValidFinding() {
			return enumError(kind, "findings[].severity", string(finding.Severity),
				"info", "low", "medium", "high", "critical")
		}
		if !finding.ResolutionAuthority.Valid() {
			return enumError(kind, "findings[].resolution_authority",
				string(finding.ResolutionAuthority), "human", "repository_tool",
				"external_research", "experiment", "consultant", "principal")
		}
		if finding.BlocksReadiness {
			blocking = true
		}
	}
	// A review that reports a readiness-blocking finding while returning
	// "pass" would let the Gate be passed by the summary rather than by the
	// evidence — the same failure DCI-041 forbids for validation.
	if blocking && r.Verdict == VerdictPass {
		return errs.New(errs.CategoryIntegrity,
			"%s: verdict is pass but a finding blocks readiness", kind)
	}
	return nil
}

// MarshalJSON guarantees the required arrays serialise as `[]`, never `null`.
func (r SpecificationReviewResult) MarshalJSON() ([]byte, error) {
	type alias SpecificationReviewResult
	out := alias(r)
	out.Findings = orEmpty(out.Findings)
	return json.Marshal(out)
}
