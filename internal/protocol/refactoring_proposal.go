package protocol

import "github.com/olostan/DevCadence/internal/errs"

// ReversibilityClass characterizes the blast radius and rollback difficulty of a refactor.
type ReversibilityClass string

const (
	ReversibilityTrivial     ReversibilityClass = "trivial"
	ReversibilityModerate    ReversibilityClass = "moderate"
	ReversibilitySignificant ReversibilityClass = "significant"
)

// Valid reports whether the reversibility class is defined.
func (r ReversibilityClass) Valid() bool {
	switch r {
	case ReversibilityTrivial, ReversibilityModerate, ReversibilitySignificant:
		return true
	}
	return false
}

// ProposalStatus records the adjudication state of a refactoring proposal.
type ProposalStatus string

const (
	ProposalProposed   ProposalStatus = "proposed"
	ProposalAccepted   ProposalStatus = "accepted"
	ProposalRejected   ProposalStatus = "rejected"
	ProposalSuperseded ProposalStatus = "superseded"
)

// Valid reports whether the proposal status is defined.
func (s ProposalStatus) Valid() bool {
	switch s {
	case ProposalProposed, ProposalAccepted, ProposalRejected, ProposalSuperseded:
		return true
	}
	return false
}

// ProposalAdjudication captures the Principal/Human adjudication record.
type ProposalAdjudication struct {
	AdjudicatedBy          Actor   `json:"adjudicated_by"`
	AdjudicatedAt          string  `json:"adjudicated_at"`
	DispositionNotes       string  `json:"disposition_notes"`
	ResultingWorkPackageID *string `json:"resulting_work_package_id,omitempty"`
}

// Validate checks ProposalAdjudication fields.
func (p ProposalAdjudication) Validate() error {
	const kind = "ProposalAdjudication"
	if err := p.AdjudicatedBy.Validate(); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "adjudicated_at", p.AdjudicatedAt); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "disposition_notes", p.DispositionNotes); err != nil {
		return err
	}
	return nil
}

// RefactoringProposal allows workers to challenge upstream contracts without code rot (ADR-0019 §3).
type RefactoringProposal struct {
	SchemaVersion          SchemaVersion         `json:"schema_version"`
	ProposalID             string                `json:"proposal_id"`
	CreatedAt              string                `json:"created_at"`
	SourceWorkPackageID    string                `json:"source_work_package_id"`
	TargetWorkPackageID    string                `json:"target_work_package_id"`
	ArchitecturalTension   string                `json:"architectural_tension"`
	ContradictionEvidence  []EvidenceRef         `json:"contradiction_evidence"`
	ProposedInterface      string                `json:"proposed_interface"`
	AffectedCallers        []string              `json:"affected_callers"`
	Reversibility          ReversibilityClass    `json:"reversibility"`
	ReversibilityRationale string                `json:"reversibility_rationale"`
	Status                 ProposalStatus        `json:"status"`
	Adjudication           *ProposalAdjudication `json:"adjudication,omitempty"`
}

// RecordKind implements Record.
func (r *RefactoringProposal) RecordKind() string { return "RefactoringProposal" }

// RecordID implements Record.
func (r *RefactoringProposal) RecordID() string { return r.ProposalID }

// SchemaVer implements Record.
func (r *RefactoringProposal) SchemaVer() SchemaVersion { return r.SchemaVersion }

// Validate enforces RefactoringProposal constraints per ADR-0019 §3.
func (r *RefactoringProposal) Validate() error {
	const kind = "RefactoringProposal"
	if err := r.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "proposal_id", r.ProposalID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "created_at", r.CreatedAt); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "source_work_package_id", r.SourceWorkPackageID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "target_work_package_id", r.TargetWorkPackageID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "architectural_tension", r.ArchitecturalTension); err != nil {
		return err
	}
	if err := requireMinItems(kind, "contradiction_evidence", len(r.ContradictionEvidence), 1); err != nil {
		return err
	}
	for i, ref := range r.ContradictionEvidence {
		if err := ref.Validate(); err != nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: contradiction_evidence[%d]: %v", kind, i, err)
		}
	}
	if err := requireNonEmpty(kind, "proposed_interface", r.ProposedInterface); err != nil {
		return err
	}
	if !r.Reversibility.Valid() {
		return enumError(kind, "reversibility", string(r.Reversibility),
			string(ReversibilityTrivial), string(ReversibilityModerate), string(ReversibilitySignificant))
	}
	if err := requireNonEmpty(kind, "reversibility_rationale", r.ReversibilityRationale); err != nil {
		return err
	}
	if !r.Status.Valid() {
		return enumError(kind, "status", string(r.Status),
			string(ProposalProposed), string(ProposalAccepted), string(ProposalRejected), string(ProposalSuperseded))
	}
	if r.Adjudication != nil {
		if err := r.Adjudication.Validate(); err != nil {
			return err
		}
	}
	return nil
}
