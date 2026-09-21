package protocol

import (
	"encoding/json"

	"github.com/olostan/DevCadience/internal/errs"
)

// ProductDecisionStatus is the lifecycle of a human product decision.
type ProductDecisionStatus string

const (
	ProductDecisionConfirmed  ProductDecisionStatus = "confirmed"
	ProductDecisionSuperseded ProductDecisionStatus = "superseded"
	ProductDecisionWithdrawn  ProductDecisionStatus = "withdrawn"
)

// Valid reports whether the status is defined by the schema.
func (s ProductDecisionStatus) Valid() bool {
	switch s {
	case ProductDecisionConfirmed, ProductDecisionSuperseded, ProductDecisionWithdrawn:
		return true
	}
	return false
}

// ProductDecision records an answer only a human can give.
//
// The schema pins `authority` to the constant "human" on purpose: a product
// decision the principal made for itself is an inference, and recording it
// here would erase the distinction DCI-005 exists to preserve. Such a belief
// belongs in a ProblemModel assumption or an AmbiguityLedger entry until a
// human confirms it.
type ProductDecision struct {
	SchemaVersion SchemaVersion `json:"schema_version"`
	DecisionID    string        `json:"decision_id"`
	ProjectID     string        `json:"project_id"`
	Question      string        `json:"question"`
	Answer        string        `json:"answer"`
	// Authority is always "human".
	Authority      string                `json:"authority"`
	AuthorityActor *string               `json:"authority_actor,omitempty"`
	Status         ProductDecisionStatus `json:"status"`
	SourceRef      *string               `json:"source_ref,omitempty"`
	Consequences   []string              `json:"consequences"`
	Supersedes     *string               `json:"supersedes,omitempty"`
	RecordedAt     *string               `json:"recorded_at,omitempty"`
}

// ProductDecisionAuthorityHuman is the only authority a product decision may
// carry.
const ProductDecisionAuthorityHuman = "human"

// RecordKind implements Record.
func (d *ProductDecision) RecordKind() string { return "ProductDecision" }

// RecordID implements Record.
func (d *ProductDecision) RecordID() string { return d.DecisionID }

// SchemaVer implements Record.
func (d *ProductDecision) SchemaVer() SchemaVersion { return d.SchemaVersion }

// Validate enforces the schema's required fields and enumerations.
func (d *ProductDecision) Validate() error {
	const kind = "ProductDecision"
	if err := d.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	for field, value := range map[string]string{
		"decision_id": d.DecisionID,
		"project_id":  d.ProjectID,
		"question":    d.Question,
		"answer":      d.Answer,
	} {
		if err := requireNonEmpty(kind, field, value); err != nil {
			return err
		}
	}
	if d.Authority != ProductDecisionAuthorityHuman {
		return enumError(kind, "authority", d.Authority, ProductDecisionAuthorityHuman)
	}
	if !d.Status.Valid() {
		return enumError(kind, "status", string(d.Status), "confirmed", "superseded", "withdrawn")
	}
	return nil
}

// MarshalJSON guarantees the required arrays serialise as `[]`, never `null`.
func (d ProductDecision) MarshalJSON() ([]byte, error) {
	type alias ProductDecision
	out := alias(d)
	out.Consequences = orEmpty(out.Consequences)
	return json.Marshal(out)
}

// RequirementKind classifies what a requirement constrains.
type RequirementKind string

const (
	RequirementFunctional    RequirementKind = "functional"
	RequirementNonFunctional RequirementKind = "non_functional"
	RequirementConstraint    RequirementKind = "constraint"
	RequirementNonGoal       RequirementKind = "non_goal"
)

// Valid reports whether the kind is defined by the schema.
func (k RequirementKind) Valid() bool {
	switch k {
	case RequirementFunctional, RequirementNonFunctional, RequirementConstraint, RequirementNonGoal:
		return true
	}
	return false
}

// RequirementStrength is RFC 2119 strength, as docs/REQUIREMENTS.md uses it.
//
// It is a separate type from GuidanceStrength: Work Package guidance has a
// fourth level (LOCAL_DISCRETION) that makes no sense for a requirement, and
// conflating them would let a requirement be written as optional.
type RequirementStrength string

const (
	RequirementMust   RequirementStrength = "MUST"
	RequirementShould RequirementStrength = "SHOULD"
	RequirementMay    RequirementStrength = "MAY"
)

// Valid reports whether the strength is defined by the schema.
func (s RequirementStrength) Valid() bool {
	switch s {
	case RequirementMust, RequirementShould, RequirementMay:
		return true
	}
	return false
}

// RequirementStatus is how well established a requirement is.
//
// "assumed" and "proposed" are deliberately distinct from "confirmed": a
// requirement the principal inferred must not be presented as one the human
// asked for (DCI-005).
type RequirementStatus string

const (
	RequirementConfirmed      RequirementStatus = "confirmed"
	RequirementEvidenceBacked RequirementStatus = "evidence_backed"
	RequirementProposed       RequirementStatus = "proposed"
	RequirementAssumed        RequirementStatus = "assumed"
	RequirementDeferred       RequirementStatus = "deferred"
	RequirementRejected       RequirementStatus = "rejected"
	RequirementSuperseded     RequirementStatus = "superseded"
)

// Valid reports whether the status is defined by the schema.
func (s RequirementStatus) Valid() bool {
	switch s {
	case RequirementConfirmed, RequirementEvidenceBacked, RequirementProposed,
		RequirementAssumed, RequirementDeferred, RequirementRejected, RequirementSuperseded:
		return true
	}
	return false
}

// RequirementSource records where a requirement came from.
type RequirementSource struct {
	Type SourceType `json:"type"`
	Ref  *string    `json:"ref,omitempty"`
}

// Requirement is one durable statement of what the product must do.
type Requirement struct {
	SchemaVersion SchemaVersion       `json:"schema_version"`
	RequirementID string              `json:"requirement_id"`
	ProjectID     string              `json:"project_id"`
	Kind          RequirementKind     `json:"kind"`
	Statement     string              `json:"statement"`
	Strength      RequirementStrength `json:"strength"`
	Status        RequirementStatus   `json:"status"`
	Source        RequirementSource   `json:"source"`

	NeedsHumanConfirmation bool     `json:"needs_human_confirmation,omitempty"`
	EvidenceRefs           []string `json:"evidence_refs,omitempty"`
	Rationale              *string  `json:"rationale,omitempty"`
	AcceptanceHint         *string  `json:"acceptance_hint,omitempty"`
	Supersedes             *string  `json:"supersedes,omitempty"`
}

// RecordKind implements Record.
func (r *Requirement) RecordKind() string { return "Requirement" }

// RecordID implements Record.
func (r *Requirement) RecordID() string { return r.RequirementID }

// SchemaVer implements Record.
func (r *Requirement) SchemaVer() SchemaVersion { return r.SchemaVersion }

// Validate enforces the schema's required fields and enumerations.
func (r *Requirement) Validate() error {
	const kind = "Requirement"
	if err := r.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	for field, value := range map[string]string{
		"requirement_id": r.RequirementID,
		"project_id":     r.ProjectID,
		"statement":      r.Statement,
	} {
		if err := requireNonEmpty(kind, field, value); err != nil {
			return err
		}
	}
	if !r.Kind.Valid() {
		return enumError(kind, "kind", string(r.Kind),
			"functional", "non_functional", "constraint", "non_goal")
	}
	if !r.Strength.Valid() {
		return enumError(kind, "strength", string(r.Strength), "MUST", "SHOULD", "MAY")
	}
	if !r.Status.Valid() {
		return enumError(kind, "status", string(r.Status), "confirmed", "evidence_backed",
			"proposed", "assumed", "deferred", "rejected", "superseded")
	}
	if !r.Source.Type.ValidRequirementSource() {
		return enumError(kind, "source.type", string(r.Source.Type),
			"product_decision", "human_statement", "external_fact", "repository_fact",
			"experiment", "principal_inference", "policy")
	}
	// A confirmed requirement must trace to a human, directly or through a
	// product decision. Anything else is the principal confirming its own
	// inference, which DCI-005 and ADR-0001 both forbid.
	if r.Status == RequirementConfirmed {
		switch r.Source.Type {
		case SourceProductDecision, SourceHumanStatement:
		default:
			return enumError(kind, "source.type", string(r.Source.Type),
				"product_decision or human_statement for a confirmed requirement")
		}
		// Naming a human-originating source type is not provenance; it is a
		// claim about provenance. "Confirmed because a human said so" with
		// nothing to point at is exactly the unverifiable assertion DCI-015
		// exists to prevent, so the reference is required.
		//
		// For source_product_decision the ref is a ProductDecision id, which
		// the control plane additionally checks exists in the same project.
		// For human_statement M1 has no durable transcript record, so the ref
		// is whatever durable artifact carries the statement (an evidence ref
		// or an ambiguity entry id). Requiring *some* retrievable handle is
		// the M1-safe rule; tightening it to a specific record kind waits for
		// the milestone that introduces one.
		if r.Source.Ref == nil || *r.Source.Ref == "" {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: a confirmed requirement sourced from %s must carry source.ref; "+
					"a claim of human provenance with nothing to trace to is not provenance",
				kind, r.Source.Type)
		}
	}
	return nil
}
