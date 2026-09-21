package protocol

import "encoding/json"

// ResolutionAuthority names who or what can settle an ambiguity.
//
// It is distinct from DecisionAuthority: a product ambiguity may be resolvable
// by a repository tool or an experiment, which are not decision-makers.
type ResolutionAuthority string

const (
	ResolveByHuman            ResolutionAuthority = "human"
	ResolveByRepositoryTool   ResolutionAuthority = "repository_tool"
	ResolveByExternalResearch ResolutionAuthority = "external_research"
	ResolveByExperiment       ResolutionAuthority = "experiment"
	ResolveByConsultant       ResolutionAuthority = "consultant"
	ResolveByPrincipal        ResolutionAuthority = "principal"
)

// Valid reports whether the authority is defined by the schema.
func (a ResolutionAuthority) Valid() bool {
	switch a {
	case ResolveByHuman, ResolveByRepositoryTool, ResolveByExternalResearch,
		ResolveByExperiment, ResolveByConsultant, ResolveByPrincipal:
		return true
	}
	return false
}

// ImpactLevel grades architectural impact and the cost of assuming wrongly.
type ImpactLevel string

const (
	ImpactLow      ImpactLevel = "low"
	ImpactMedium   ImpactLevel = "medium"
	ImpactHigh     ImpactLevel = "high"
	ImpactCritical ImpactLevel = "critical"
)

// Valid reports whether the level is one of low/medium/high/critical.
func (l ImpactLevel) Valid() bool {
	switch l {
	case ImpactLow, ImpactMedium, ImpactHigh, ImpactCritical:
		return true
	}
	return false
}

// ValidSecurityImpact additionally admits "none", which the security and
// privacy grade needs and the architectural grade does not.
func (l ImpactLevel) ValidSecurityImpact() bool {
	return l == "none" || l.Valid()
}

// ValidIrreversibility admits only low/medium/high: an irreversible decision
// is not graded "critical", it is graded "high".
func (l ImpactLevel) ValidIrreversibility() bool {
	switch l {
	case ImpactLow, ImpactMedium, ImpactHigh:
		return true
	}
	return false
}

// AmbiguityStatus is the lifecycle of one open question.
type AmbiguityStatus string

const (
	AmbiguityOpen               AmbiguityStatus = "open"
	AmbiguityInvestigating      AmbiguityStatus = "investigating"
	AmbiguityAwaitingHuman      AmbiguityStatus = "awaiting_human"
	AmbiguityResearching        AmbiguityStatus = "researching"
	AmbiguityExperimenting      AmbiguityStatus = "experimenting"
	AmbiguityConsulting         AmbiguityStatus = "consulting"
	AmbiguityResolved           AmbiguityStatus = "resolved"
	AmbiguityExplicitlyDeferred AmbiguityStatus = "explicitly_deferred"
	AmbiguityReopened           AmbiguityStatus = "reopened"
)

// Valid reports whether the status is defined by the schema.
func (s AmbiguityStatus) Valid() bool {
	switch s {
	case AmbiguityOpen, AmbiguityInvestigating, AmbiguityAwaitingHuman, AmbiguityResearching,
		AmbiguityExperimenting, AmbiguityConsulting, AmbiguityResolved,
		AmbiguityExplicitlyDeferred, AmbiguityReopened:
		return true
	}
	return false
}

// AmbiguityEntry is one unresolved question about what is actually wanted.
//
// The impact fields exist so that policy can tell a question worth stopping
// for from one worth deferring: DCI-004 requires the principal to challenge
// its own understanding, and this is where the cost of being wrong is
// recorded rather than felt later.
type AmbiguityEntry struct {
	ID                    string              `json:"id"`
	Question              string              `json:"question"`
	Origin                string              `json:"origin"`
	Category              string              `json:"category"`
	ResolutionAuthority   ResolutionAuthority `json:"resolution_authority"`
	ArchitecturalImpact   ImpactLevel         `json:"architectural_impact"`
	SecurityPrivacyImpact ImpactLevel         `json:"security_privacy_impact,omitempty"`
	Irreversibility       ImpactLevel         `json:"irreversibility,omitempty"`
	CostOfWrongAssumption ImpactLevel         `json:"cost_of_wrong_assumption"`
	WhyItMatters          string              `json:"why_it_matters"`

	PossibleInterpretations []string        `json:"possible_interpretations,omitempty"`
	Status                  AmbiguityStatus `json:"status"`
	Resolution              *string         `json:"resolution,omitempty"`
	// SafeDeferralBoundary states how far work may proceed while this stays
	// open, so that deferring is a bounded decision rather than a hope.
	SafeDeferralBoundary *string  `json:"safe_deferral_boundary,omitempty"`
	EvidenceRefs         []string `json:"evidence_refs,omitempty"`
	ProductDecisionRef   *string  `json:"product_decision_ref,omitempty"`
	RequirementRefs      []string `json:"requirement_refs,omitempty"`
}

// AmbiguityLedger is the durable set of open questions about intent.
type AmbiguityLedger struct {
	SchemaVersion     SchemaVersion    `json:"schema_version"`
	AmbiguityLedgerID string           `json:"ambiguity_ledger_id"`
	ProjectID         string           `json:"project_id"`
	Revision          int              `json:"revision"`
	Entries           []AmbiguityEntry `json:"entries"`
}

// RecordKind implements Record.
func (l *AmbiguityLedger) RecordKind() string { return "AmbiguityLedger" }

// RecordID implements Record.
func (l *AmbiguityLedger) RecordID() string { return l.AmbiguityLedgerID }

// SchemaVer implements Record.
func (l *AmbiguityLedger) SchemaVer() SchemaVersion { return l.SchemaVersion }

// Validate enforces the schema's required fields and enumerations.
func (l *AmbiguityLedger) Validate() error {
	const kind = "AmbiguityLedger"
	if err := l.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "ambiguity_ledger_id", l.AmbiguityLedgerID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "project_id", l.ProjectID); err != nil {
		return err
	}
	if l.Revision < 1 {
		return enumError(kind, "revision", "0", ">= 1")
	}
	seen := make(map[string]struct{}, len(l.Entries))
	for _, entry := range l.Entries {
		for field, value := range map[string]string{
			"entries[].id":             entry.ID,
			"entries[].question":       entry.Question,
			"entries[].origin":         entry.Origin,
			"entries[].category":       entry.Category,
			"entries[].why_it_matters": entry.WhyItMatters,
		} {
			if err := requireNonEmpty(kind, field, value); err != nil {
				return err
			}
		}
		// Entry ids are cited by requirements and product decisions, so a
		// duplicate would make those references ambiguous.
		if _, dup := seen[entry.ID]; dup {
			return enumError(kind, "entries[].id", entry.ID, "unique entry ids")
		}
		seen[entry.ID] = struct{}{}
		if !entry.ResolutionAuthority.Valid() {
			return enumError(kind, "entries[].resolution_authority", string(entry.ResolutionAuthority),
				"human", "repository_tool", "external_research", "experiment", "consultant", "principal")
		}
		if !entry.ArchitecturalImpact.Valid() {
			return enumError(kind, "entries[].architectural_impact", string(entry.ArchitecturalImpact),
				"low", "medium", "high", "critical")
		}
		if entry.SecurityPrivacyImpact != "" && !entry.SecurityPrivacyImpact.ValidSecurityImpact() {
			return enumError(kind, "entries[].security_privacy_impact", string(entry.SecurityPrivacyImpact),
				"none", "low", "medium", "high", "critical")
		}
		if entry.Irreversibility != "" && !entry.Irreversibility.ValidIrreversibility() {
			return enumError(kind, "entries[].irreversibility", string(entry.Irreversibility),
				"low", "medium", "high")
		}
		if !entry.CostOfWrongAssumption.Valid() {
			return enumError(kind, "entries[].cost_of_wrong_assumption", string(entry.CostOfWrongAssumption),
				"low", "medium", "high", "critical")
		}
		if !entry.Status.Valid() {
			return enumError(kind, "entries[].status", string(entry.Status),
				"open", "investigating", "awaiting_human", "researching", "experimenting",
				"consulting", "resolved", "explicitly_deferred", "reopened")
		}
		// A resolved ambiguity that records no resolution would let a
		// question be closed without anyone saying what the answer was.
		if entry.Status == AmbiguityResolved && (entry.Resolution == nil || *entry.Resolution == "") {
			return enumError(kind, "entries[].resolution", "(empty)",
				"a resolution when the entry is resolved")
		}
	}
	return nil
}

// MarshalJSON guarantees the required arrays serialise as `[]`, never `null`.
func (l AmbiguityLedger) MarshalJSON() ([]byte, error) {
	type alias AmbiguityLedger
	out := alias(l)
	out.Entries = orEmpty(out.Entries)
	return json.Marshal(out)
}
