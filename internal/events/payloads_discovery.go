package events

import (
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/protocol"
)

// Discovery and specification events.
//
// ENGINEERING_STANDARDS.md §11 names these as durable transitions, and
// FR-D-012 requires a new principal session to reconstruct current product
// intent from durable artifacts rather than from a conversation transcript.
// Both obligations land on the event journal, so the vocabulary lives here
// and ProjectState.discovery is reduced from it (ADR-0005: every
// ProjectState field is derived from events).
//
// This is the discovery *vocabulary*, not the discovery *workflow*. Nothing
// here asks a question, runs an experiment or assesses readiness; these
// events record that such things happened.
const (
	TypeProblemModelRevised            Type = "ProblemModelRevised"
	TypeAmbiguityOpened                Type = "AmbiguityOpened"
	TypeAmbiguityResolved              Type = "AmbiguityResolved"
	TypeProductDecisionRecorded        Type = "ProductDecisionRecorded"
	TypeDiscoveryExperimentStarted     Type = "DiscoveryExperimentStarted"
	TypeDiscoveryExperimentCompleted   Type = "DiscoveryExperimentCompleted"
	TypeSpecificationReviewCompleted   Type = "SpecificationReviewCompleted"
	TypeSpecificationReadinessRecorded Type = "SpecificationReadinessRecorded"
)

// ProblemModelRevised records a new revision of the ProblemModel.
//
// The model is revisioned rather than mutated, so each revision is its own
// event and the full document lives in the record store.
type ProblemModelRevised struct {
	ProblemModelID string `json:"problem_model_id"`
	Revision       int    `json:"revision"`
	RecordDigest   string `json:"record_digest"`
	// Summary is the compact statement of what changed, for the operator
	// timeline. The document itself is retrievable by id and digest.
	Summary string `json:"summary"`
	// MaterialAssumptionsUnverified is what stops architecture starting on
	// unverified ground (DCI-005, DCI-016). It is carried here rather than
	// recomputed from the document so that ProjectState stays derivable from
	// the journal alone.
	MaterialAssumptionsUnverified int    `json:"material_assumptions_unverified"`
	AmbiguityLedgerID             string `json:"ambiguity_ledger_id,omitempty"`
	// HumanReflectionRevision records which revision the human last reviewed.
	// Zero means no reflection has happened yet.
	HumanReflectionRevision int `json:"human_reflection_revision,omitempty"`
}

// Type implements Payload.
func (p *ProblemModelRevised) Type() Type { return TypeProblemModelRevised }

// Validate implements Payload.
func (p *ProblemModelRevised) Validate() error {
	if p.ProblemModelID == "" || p.RecordDigest == "" || p.Summary == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"ProblemModelRevised: problem_model_id, record_digest and summary are required")
	}
	if p.Revision < 1 {
		return errs.New(errs.CategoryInvalidArgument, "ProblemModelRevised: revision must be >= 1")
	}
	if p.MaterialAssumptionsUnverified < 0 {
		return errs.New(errs.CategoryInvalidArgument,
			"ProblemModelRevised: material_assumptions_unverified must not be negative")
	}
	if p.HumanReflectionRevision < 0 || p.HumanReflectionRevision > p.Revision {
		return errs.New(errs.CategoryIntegrity,
			"ProblemModelRevised: human_reflection_revision %d cannot exceed revision %d",
			p.HumanReflectionRevision, p.Revision)
	}
	return nil
}

// AmbiguityOpened records a question about intent entering the ledger
// (DCI-008: ambiguity is exposed, not silently resolved).
type AmbiguityOpened struct {
	AmbiguityID       string `json:"ambiguity_id"`
	AmbiguityLedgerID string `json:"ambiguity_ledger_id"`
	Question          string `json:"question"`
	// ResolutionAuthority decides who can settle it. An open ambiguity whose
	// authority is the human is, by definition, awaiting the human — which is
	// how ProjectState counts it, with no separate status event needed.
	ResolutionAuthority protocol.ResolutionAuthority `json:"resolution_authority"`
	// ArchitecturalImpact is what makes an ambiguity material: docs/
	// DISCOVERY_AND_SPECIFICATION.md §3 defines materiality as the potential
	// to alter architecture, and this is the field that grades it.
	ArchitecturalImpact   protocol.ImpactLevel `json:"architectural_impact"`
	CostOfWrongAssumption protocol.ImpactLevel `json:"cost_of_wrong_assumption"`
	WhyItMatters          string               `json:"why_it_matters"`
}

// Type implements Payload.
func (p *AmbiguityOpened) Type() Type { return TypeAmbiguityOpened }

// Validate implements Payload.
func (p *AmbiguityOpened) Validate() error {
	if p.AmbiguityID == "" || p.AmbiguityLedgerID == "" || p.Question == "" || p.WhyItMatters == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"AmbiguityOpened: ambiguity_id, ambiguity_ledger_id, question and why_it_matters are required")
	}
	if !p.ResolutionAuthority.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"AmbiguityOpened: resolution_authority %q is not a known resolution authority",
			string(p.ResolutionAuthority))
	}
	if !p.ArchitecturalImpact.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"AmbiguityOpened: architectural_impact %q is not low, medium, high or critical",
			string(p.ArchitecturalImpact))
	}
	if !p.CostOfWrongAssumption.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"AmbiguityOpened: cost_of_wrong_assumption %q is not low, medium, high or critical",
			string(p.CostOfWrongAssumption))
	}
	return nil
}

// AmbiguityOutcome distinguishes a question that was answered from one that
// was deliberately deferred.
//
// docs/DISCOVERY_AND_SPECIFICATION.md §5 permits deferral only behind a safe
// boundary, so the two outcomes cannot be the same event field.
type AmbiguityOutcome string

const (
	// AmbiguityOutcomeResolved means the question was answered.
	AmbiguityOutcomeResolved AmbiguityOutcome = "resolved"
	// AmbiguityOutcomeDeferred means it stays open in principle but cannot
	// silently become an architectural decision, because a boundary bounds it.
	AmbiguityOutcomeDeferred AmbiguityOutcome = "explicitly_deferred"
)

// Valid reports whether the outcome is known.
func (o AmbiguityOutcome) Valid() bool {
	return o == AmbiguityOutcomeResolved || o == AmbiguityOutcomeDeferred
}

// AmbiguityResolved closes an ambiguity, by answer or by bounded deferral.
type AmbiguityResolved struct {
	AmbiguityID string           `json:"ambiguity_id"`
	Outcome     AmbiguityOutcome `json:"outcome"`
	// Resolution is the answer, or the reason for deferring.
	Resolution string `json:"resolution"`
	// ResolvedBy names who or what actually settled it, which may differ from
	// the authority the question was opened against.
	ResolvedBy protocol.ResolutionAuthority `json:"resolved_by"`
	// SafeDeferralBoundary is required for a deferral: an unbounded deferral
	// is a silent architectural decision.
	SafeDeferralBoundary string   `json:"safe_deferral_boundary,omitempty"`
	ProductDecisionRef   string   `json:"product_decision_ref,omitempty"`
	EvidenceRefs         []string `json:"evidence_refs,omitempty"`
}

// Type implements Payload.
func (p *AmbiguityResolved) Type() Type { return TypeAmbiguityResolved }

// Validate implements Payload.
func (p *AmbiguityResolved) Validate() error {
	if p.AmbiguityID == "" || p.Resolution == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"AmbiguityResolved: ambiguity_id and resolution are required")
	}
	if !p.Outcome.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"AmbiguityResolved: outcome %q is not resolved or explicitly_deferred", string(p.Outcome))
	}
	if !p.ResolvedBy.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"AmbiguityResolved: resolved_by %q is not a known resolution authority", string(p.ResolvedBy))
	}
	if p.Outcome == AmbiguityOutcomeDeferred && p.SafeDeferralBoundary == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"AmbiguityResolved: a deferral requires a safe_deferral_boundary, "+
				"otherwise it is a silent architectural decision")
	}
	return nil
}

// ProductDecisionRecorded records an answer only a human can give (DCI-009).
type ProductDecisionRecorded struct {
	ProductDecisionID string                         `json:"product_decision_id"`
	Question          string                         `json:"question"`
	Answer            string                         `json:"answer"`
	RecordDigest      string                         `json:"record_digest"`
	Status            protocol.ProductDecisionStatus `json:"status"`
	// AuthorityActor names the human who decided. It is an identity handle,
	// never a credential (DCI-081).
	AuthorityActor    string `json:"authority_actor,omitempty"`
	Supersedes        string `json:"supersedes,omitempty"`
	ResolvesAmbiguity string `json:"resolves_ambiguity,omitempty"`
}

// Type implements Payload.
func (p *ProductDecisionRecorded) Type() Type { return TypeProductDecisionRecorded }

// Validate implements Payload.
func (p *ProductDecisionRecorded) Validate() error {
	if p.ProductDecisionID == "" || p.Question == "" || p.Answer == "" || p.RecordDigest == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"ProductDecisionRecorded: product_decision_id, question, answer and record_digest are required")
	}
	if !p.Status.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"ProductDecisionRecorded: status %q is not confirmed, superseded or withdrawn", string(p.Status))
	}
	return nil
}

// DiscoveryExperimentStarted records a bounded probe beginning.
type DiscoveryExperimentStarted struct {
	ExperimentID string `json:"experiment_id"`
	Question     string `json:"question"`
	Hypothesis   string `json:"hypothesis"`
	RecordDigest string `json:"record_digest"`
	// ResolvesAmbiguity names the question the experiment is meant to settle.
	ResolvesAmbiguity string `json:"resolves_ambiguity,omitempty"`
}

// Type implements Payload.
func (p *DiscoveryExperimentStarted) Type() Type { return TypeDiscoveryExperimentStarted }

// Validate implements Payload.
func (p *DiscoveryExperimentStarted) Validate() error {
	if p.ExperimentID == "" || p.Question == "" || p.Hypothesis == "" || p.RecordDigest == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"DiscoveryExperimentStarted: experiment_id, question, hypothesis and record_digest are required")
	}
	return nil
}

// DiscoveryExperimentCompleted records a probe ending.
//
// An inconclusive experiment is a first-class outcome: reporting it as
// completed would let "we ran it" stand in for "we learned something".
type DiscoveryExperimentCompleted struct {
	ExperimentID  string                    `json:"experiment_id"`
	Status        protocol.ExperimentStatus `json:"status"`
	ResultSummary string                    `json:"result_summary"`
	RecordDigest  string                    `json:"record_digest"`
	Limitations   []string                  `json:"limitations,omitempty"`
}

// Type implements Payload.
func (p *DiscoveryExperimentCompleted) Type() Type { return TypeDiscoveryExperimentCompleted }

// Validate implements Payload.
func (p *DiscoveryExperimentCompleted) Validate() error {
	if p.ExperimentID == "" || p.RecordDigest == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"DiscoveryExperimentCompleted: experiment_id and record_digest are required")
	}
	if !p.Status.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"DiscoveryExperimentCompleted: status %q is not a known experiment status", string(p.Status))
	}
	if p.Status == protocol.ExperimentPlanned || p.Status == protocol.ExperimentRunning {
		return errs.New(errs.CategoryInvalidArgument,
			"DiscoveryExperimentCompleted: status %q is not a terminal experiment status", string(p.Status))
	}
	if p.Status == protocol.ExperimentCompleted && p.ResultSummary == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"DiscoveryExperimentCompleted: a completed experiment must report a result")
	}
	return nil
}

// SpecificationReviewCompleted records an independent review of the
// specification (FR-D-010).
//
// Reviews are evidence, not a gate: a material gap they find becomes an
// AmbiguityOpened event, and readiness is assessed separately.
type SpecificationReviewCompleted struct {
	ReviewID        string `json:"review_id"`
	ReviewerProfile string `json:"reviewer_profile"`
	// MaterialGapsFound names the ambiguities the review opened, so that the
	// link from review to reopened question stays in the journal.
	MaterialGapsFound []string `json:"material_gaps_found,omitempty"`
	Summary           string   `json:"summary"`
	RecordDigest      string   `json:"record_digest,omitempty"`
}

// Type implements Payload.
func (p *SpecificationReviewCompleted) Type() Type { return TypeSpecificationReviewCompleted }

// Validate implements Payload.
func (p *SpecificationReviewCompleted) Validate() error {
	if p.ReviewID == "" || p.ReviewerProfile == "" || p.Summary == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"SpecificationReviewCompleted: review_id, reviewer_profile and summary are required")
	}
	return nil
}

// SpecificationReadinessRecorded records a Design Readiness Gate assessment
// (DCI-016, FR-D-011).
type SpecificationReadinessRecorded struct {
	ReadinessID string `json:"readiness_id"`
	// ProblemModelID and ProblemModelRevision pin what was assessed, so a
	// later revision cannot inherit a verdict it was never judged against.
	ProblemModelID       string                    `json:"problem_model_id"`
	ProblemModelRevision int                       `json:"problem_model_revision"`
	Verdict              protocol.ReadinessVerdict `json:"verdict"`
	RecordDigest         string                    `json:"record_digest"`
}

// Type implements Payload.
func (p *SpecificationReadinessRecorded) Type() Type { return TypeSpecificationReadinessRecorded }

// Validate implements Payload.
func (p *SpecificationReadinessRecorded) Validate() error {
	if p.ReadinessID == "" || p.ProblemModelID == "" || p.RecordDigest == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"SpecificationReadinessRecorded: readiness_id, problem_model_id and record_digest are required")
	}
	if p.ProblemModelRevision < 1 {
		return errs.New(errs.CategoryInvalidArgument,
			"SpecificationReadinessRecorded: problem_model_revision must be >= 1")
	}
	if !p.Verdict.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"SpecificationReadinessRecorded: verdict %q is not a known readiness verdict", string(p.Verdict))
	}
	return nil
}

func init() {
	Register(TypeProblemModelRevised, func() Payload { return &ProblemModelRevised{} })
	Register(TypeAmbiguityOpened, func() Payload { return &AmbiguityOpened{} })
	Register(TypeAmbiguityResolved, func() Payload { return &AmbiguityResolved{} })
	Register(TypeProductDecisionRecorded, func() Payload { return &ProductDecisionRecorded{} })
	Register(TypeDiscoveryExperimentStarted, func() Payload { return &DiscoveryExperimentStarted{} })
	Register(TypeDiscoveryExperimentCompleted, func() Payload { return &DiscoveryExperimentCompleted{} })
	Register(TypeSpecificationReviewCompleted, func() Payload { return &SpecificationReviewCompleted{} })
	Register(TypeSpecificationReadinessRecorded, func() Payload { return &SpecificationReadinessRecorded{} })
}
