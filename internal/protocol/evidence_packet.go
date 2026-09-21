package protocol

import "encoding/json"

// ClaimKind separates deterministic facts from model synthesis (DCI-013).
// It is the field that makes an EvidencePacket auditable rather than
// persuasive: a reader can always tell what was observed from what was
// concluded.
type ClaimKind string

const (
	// ClaimDeterministicFact was established by a tool, not a model.
	ClaimDeterministicFact ClaimKind = "deterministic_fact"
	// ClaimRepositoryObservation was read directly from the repository.
	ClaimRepositoryObservation ClaimKind = "repository_observation"
	// ClaimExternalFact came from an external authoritative source.
	ClaimExternalFact ClaimKind = "external_fact"
	// ClaimInterpretation is a model conclusion drawn from evidence.
	ClaimInterpretation ClaimKind = "interpretation"
	// ClaimAssumption is not yet verified (DCI-005).
	ClaimAssumption ClaimKind = "assumption"
	// ClaimRecommendation is a proposed action, not a fact.
	ClaimRecommendation ClaimKind = "recommendation"
)

// Valid reports whether the kind is defined by the schema.
func (k ClaimKind) Valid() bool {
	switch k {
	case ClaimDeterministicFact, ClaimRepositoryObservation, ClaimExternalFact,
		ClaimInterpretation, ClaimAssumption, ClaimRecommendation:
		return true
	}
	return false
}

// RequiresEvidence reports whether a claim of this kind is meaningless
// without provenance.
//
// A deterministic fact or repository observation that cites nothing cannot be
// audited, which is exactly the failure DCI-011 and DCI-012 exist to prevent.
// Interpretations, assumptions and recommendations may legitimately stand on
// reasoning alone, provided they are labelled as such.
func (k ClaimKind) RequiresEvidence() bool {
	switch k {
	case ClaimDeterministicFact, ClaimRepositoryObservation, ClaimExternalFact:
		return true
	}
	return false
}

// Claim is one labelled statement with its provenance.
type Claim struct {
	ID           string    `json:"id"`
	Kind         ClaimKind `json:"kind"`
	Statement    string    `json:"statement"`
	EvidenceRefs []string  `json:"evidence_refs"`
}

// Disagreement preserves a material conflict instead of averaging it away
// (DCI-044).
type Disagreement struct {
	Statement string   `json:"statement"`
	Positions []string `json:"positions"`
}

// EvidencePacket is the principal's boundary object for repository
// understanding (docs/PROTOCOLS.md §5).
//
// Producing one is M3 work. The type exists in M1 because it defines the
// durable semantic language every later milestone depends on (DCI-054).
type EvidencePacket struct {
	SchemaVersion    SchemaVersion `json:"schema_version"`
	EvidencePacketID string        `json:"evidence_packet_id"`
	ProjectID        string        `json:"project_id"`
	BaseCommit       string        `json:"base_commit"`
	Question         string        `json:"question"`
	Conclusion       *string       `json:"conclusion,omitempty"`

	Claims          []Claim        `json:"claims"`
	Counterevidence []string       `json:"counterevidence"`
	Disagreements   []Disagreement `json:"disagreements"`
	Uncertainties   []string       `json:"uncertainties"`
	Recommendations []string       `json:"recommendations,omitempty"`
	RawEvidenceRefs []EvidenceRef  `json:"raw_evidence_refs"`
}

// RecordKind implements Record.
func (p *EvidencePacket) RecordKind() string { return "EvidencePacket" }

// RecordID implements Record.
func (p *EvidencePacket) RecordID() string { return p.EvidencePacketID }

// SchemaVer implements Record.
func (p *EvidencePacket) SchemaVer() SchemaVersion { return p.SchemaVersion }

// Validate enforces schema constraints and the provenance rule that keeps an
// EvidencePacket from disguising synthesis as fact.
func (p *EvidencePacket) Validate() error {
	const kind = "EvidencePacket"
	if err := p.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	for field, value := range map[string]string{
		"evidence_packet_id": p.EvidencePacketID,
		"project_id":         p.ProjectID,
		"base_commit":        p.BaseCommit,
		"question":           p.Question,
	} {
		if err := requireNonEmpty(kind, field, value); err != nil {
			return err
		}
	}
	refIDs := make(map[string]struct{}, len(p.RawEvidenceRefs))
	for _, r := range p.RawEvidenceRefs {
		if err := r.Validate(); err != nil {
			return err
		}
		refIDs[r.ID] = struct{}{}
	}
	for _, c := range p.Claims {
		if err := requireNonEmpty(kind, "claims[].id", c.ID); err != nil {
			return err
		}
		if !c.Kind.Valid() {
			return enumError(kind, "claims[].kind", string(c.Kind),
				"deterministic_fact", "repository_observation", "external_fact",
				"interpretation", "assumption", "recommendation")
		}
		if err := requireNonEmpty(kind, "claims[].statement", c.Statement); err != nil {
			return err
		}
		if c.Kind.RequiresEvidence() && len(c.EvidenceRefs) == 0 {
			return enumError(kind, "claims[].evidence_refs", "(empty)",
				"at least one evidence ref for a "+string(c.Kind)+" claim")
		}
		// A dangling reference breaks the retrievability guarantee of
		// DCI-011, so it is rejected on the write path rather than
		// discovered later by a principal that cannot follow it.
		for _, ref := range c.EvidenceRefs {
			if _, ok := refIDs[ref]; !ok {
				return enumError(kind, "claims[].evidence_refs", ref, "an id present in raw_evidence_refs")
			}
		}
	}
	for _, d := range p.Disagreements {
		if err := requireNonEmpty(kind, "disagreements[].statement", d.Statement); err != nil {
			return err
		}
		if err := requireMinItems(kind, "disagreements[].positions", len(d.Positions), 2); err != nil {
			return err
		}
	}
	return nil
}

// MarshalJSON guarantees the required arrays serialise as `[]`, never `null`.
func (p EvidencePacket) MarshalJSON() ([]byte, error) {
	type alias EvidencePacket
	out := alias(p)
	out.Claims = orEmpty(out.Claims)
	out.Counterevidence = orEmpty(out.Counterevidence)
	out.Disagreements = orEmpty(out.Disagreements)
	out.Uncertainties = orEmpty(out.Uncertainties)
	out.RawEvidenceRefs = orEmpty(out.RawEvidenceRefs)
	return json.Marshal(out)
}
