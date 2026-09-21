package protocol

import "encoding/json"

// Alternative is one option considered before a decision was made. Recording
// rejected options is what makes a DecisionRecord auditable later: DCI-051
// requires alternatives, and "what would invalidate it" is what allows a
// future Architecture Reconciliation to notice the decision has expired.
type Alternative struct {
	ID           string   `json:"id"`
	Summary      string   `json:"summary"`
	Benefits     []string `json:"benefits,omitempty"`
	Risks        []string `json:"risks,omitempty"`
	Invalidators []string `json:"invalidators,omitempty"`
}

// DecisionRecord captures a durable choice (docs/PROTOCOLS.md §6).
//
// Dissent and unresolved uncertainty are separate fields from rationale
// because DCI-044 forbids collapsing disagreement into a single confident
// narrative.
type DecisionRecord struct {
	SchemaVersion SchemaVersion `json:"schema_version"`
	DecisionID    string        `json:"decision_id"`
	ProjectID     string        `json:"project_id"`
	Question      string        `json:"question"`
	Alternatives  []Alternative `json:"alternatives"`
	// Selected names the chosen Alternative by its ID.
	Selected     string   `json:"selected"`
	Criteria     []string `json:"criteria"`
	Rationale    string   `json:"rationale"`
	EvidenceRefs []string `json:"evidence_refs"`

	Dissent               []string `json:"dissent,omitempty"`
	UnresolvedUncertainty []string `json:"unresolved_uncertainty,omitempty"`
	Consequences          []string `json:"consequences"`
	InvariantChanges      []string `json:"invariant_changes,omitempty"`
	Supersedes            *string  `json:"supersedes,omitempty"`
	ADRRef                *string  `json:"adr_ref,omitempty"`
}

// RecordKind implements Record.
func (d *DecisionRecord) RecordKind() string { return "DecisionRecord" }

// RecordID implements Record.
func (d *DecisionRecord) RecordID() string { return d.DecisionID }

// SchemaVer implements Record.
func (d *DecisionRecord) SchemaVer() SchemaVersion { return d.SchemaVersion }

// Validate enforces schema constraints and referential consistency between
// the selected option and the alternatives considered.
func (d *DecisionRecord) Validate() error {
	const kind = "DecisionRecord"
	if err := d.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	for field, value := range map[string]string{
		"decision_id": d.DecisionID,
		"project_id":  d.ProjectID,
		"question":    d.Question,
		"selected":    d.Selected,
		"rationale":   d.Rationale,
	} {
		if err := requireNonEmpty(kind, field, value); err != nil {
			return err
		}
	}
	if err := requireMinItems(kind, "alternatives", len(d.Alternatives), 1); err != nil {
		return err
	}
	selectedFound := false
	seen := make(map[string]struct{}, len(d.Alternatives))
	for _, a := range d.Alternatives {
		if err := requireNonEmpty(kind, "alternatives[].id", a.ID); err != nil {
			return err
		}
		if err := requireNonEmpty(kind, "alternatives[].summary", a.Summary); err != nil {
			return err
		}
		if _, dup := seen[a.ID]; dup {
			return enumError(kind, "alternatives[].id", a.ID, "unique alternative ids")
		}
		seen[a.ID] = struct{}{}
		if a.ID == d.Selected {
			selectedFound = true
		}
	}
	// A decision whose selected option is not among the alternatives cannot
	// be replayed or audited, so it is rejected rather than stored.
	if !selectedFound {
		return enumError(kind, "selected", d.Selected, "an id present in alternatives")
	}
	return nil
}

// MarshalJSON guarantees the required arrays serialise as `[]`, never `null`.
func (d DecisionRecord) MarshalJSON() ([]byte, error) {
	type alias DecisionRecord
	out := alias(d)
	out.Alternatives = orEmpty(out.Alternatives)
	out.Criteria = orEmpty(out.Criteria)
	out.EvidenceRefs = orEmpty(out.EvidenceRefs)
	out.Consequences = orEmpty(out.Consequences)
	return json.Marshal(out)
}
