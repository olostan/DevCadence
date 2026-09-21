package protocol

import "encoding/json"

// ChangeClass is the impact classification from docs/LIFECYCLE.md §7. It
// selects the process depth a change must go through, so it is a durable
// field rather than a scheduling hint.
type ChangeClass string

const (
	ChangeLocal         ChangeClass = "local"
	ChangeSystemic      ChangeClass = "systemic"
	ChangeArchitectural ChangeClass = "architectural"
)

// Valid reports whether the class is defined by the schema.
func (c ChangeClass) Valid() bool {
	switch c {
	case ChangeLocal, ChangeSystemic, ChangeArchitectural:
		return true
	}
	return false
}

// GuidanceStrength expresses authority, not confidence (docs/PROTOCOLS.md §8).
// A SHOULD may be held with high confidence and still be overridable because
// the local worker observes repository reality more accurately.
type GuidanceStrength string

const (
	// GuidanceMust cannot be silently violated (DCI-024).
	GuidanceMust GuidanceStrength = "MUST"
	// GuidanceShould requires a justified, recorded deviation.
	GuidanceShould GuidanceStrength = "SHOULD"
	// GuidanceSuggested is an implementation hint.
	GuidanceSuggested GuidanceStrength = "SUGGESTED"
	// GuidanceLocalDiscretion leaves the decision to the worker.
	GuidanceLocalDiscretion GuidanceStrength = "LOCAL_DISCRETION"
)

// Valid reports whether the strength is defined by the schema.
func (g GuidanceStrength) Valid() bool {
	switch g {
	case GuidanceMust, GuidanceShould, GuidanceSuggested, GuidanceLocalDiscretion:
		return true
	}
	return false
}

// AssumptionStatus records how well grounded an assumption is (DCI-005).
type AssumptionStatus string

const (
	AssumptionVerified     AssumptionStatus = "verified"
	AssumptionAcceptedRisk AssumptionStatus = "accepted_risk"
	AssumptionUnverified   AssumptionStatus = "unverified"
)

// Valid reports whether the status is defined by the schema.
func (s AssumptionStatus) Valid() bool {
	switch s {
	case AssumptionVerified, AssumptionAcceptedRisk, AssumptionUnverified:
		return true
	}
	return false
}

// Assumption is a material or incidental belief the Work Package rests on.
//
// DCI-005 requires that an unverified assumption stay visible as an
// assumption; Material marks the ones whose falsity invalidates the design,
// which is what the contradiction path in docs/LIFECYCLE.md §13 acts on.
type Assumption struct {
	ID           string           `json:"id"`
	Statement    string           `json:"statement"`
	Status       AssumptionStatus `json:"status"`
	Material     bool             `json:"material"`
	EvidenceRefs []string         `json:"evidence_refs,omitempty"`
}

// Guidance is one instruction with explicit authority (DCI-022).
type Guidance struct {
	ID         string           `json:"id"`
	Strength   GuidanceStrength `json:"strength"`
	Statement  string           `json:"statement"`
	SourceRefs []string         `json:"source_refs,omitempty"`
}

// Scope states what the work covers and what it must not touch. Out-of-scope
// is as durable as in-scope because DCI-025 forbids silent scope expansion.
type Scope struct {
	InScope              []string `json:"in_scope"`
	OutOfScope           []string `json:"out_of_scope"`
	ExpectedComponents   []string `json:"expected_components,omitempty"`
	ProhibitedComponents []string `json:"prohibited_components,omitempty"`
}

// EngineeringWorkPackage is the core frontier-to-local contract
// (docs/PROTOCOLS.md §7, schemas/engineering-work-package.schema.json).
//
// It is versioned and immutable once attempts have started: docs/PROTOCOLS.md
// §19 lists in-place mutation after an attempt as an anti-pattern, so a
// revision produces a new Version rather than editing the stored record.
type EngineeringWorkPackage struct {
	SchemaVersion SchemaVersion `json:"schema_version"`
	WorkPackageID string        `json:"work_package_id"`
	TaskID        string        `json:"task_id"`
	Version       int           `json:"version"`
	ProjectID     string        `json:"project_id"`
	// ProjectStateRevision and BaseCommit together make a stale plan
	// detectable (docs/PROJECT_STATE.md §7).
	ProjectStateRevision string      `json:"project_state_revision"`
	BaseCommit           string      `json:"base_commit"`
	ChangeClass          ChangeClass `json:"change_class"`

	Objective           string `json:"objective"`
	Rationale           string `json:"rationale"`
	ArchitecturalIntent string `json:"architectural_intent"`

	Assumptions   []Assumption `json:"assumptions"`
	DecisionRefs  []string     `json:"decision_refs,omitempty"`
	InvariantRefs []string     `json:"invariant_refs,omitempty"`
	EvidenceRefs  []string     `json:"evidence_refs,omitempty"`

	Scope Scope `json:"scope"`

	ImplementationStrategy *string  `json:"implementation_strategy,omitempty"`
	Interfaces             []string `json:"interfaces,omitempty"`
	Pseudocode             []string `json:"pseudocode,omitempty"`
	CodeSnippets           []string `json:"code_snippets,omitempty"`
	RepositoryAnchors      []string `json:"repository_anchors,omitempty"`
	EdgeCases              []string `json:"edge_cases,omitempty"`
	FailureModes           []string `json:"failure_modes,omitempty"`

	Guidance               []Guidance `json:"guidance"`
	AcceptanceCriteria     []string   `json:"acceptance_criteria"`
	ValidationRequirements []string   `json:"validation_requirements"`
	EscalationConditions   []string   `json:"escalation_conditions"`

	ObservabilityRequirements []string `json:"observability_requirements,omitempty"`
	Migration                 *string  `json:"migration,omitempty"`
	Rollback                  *string  `json:"rollback,omitempty"`
}

// RecordKind implements Record.
func (w *EngineeringWorkPackage) RecordKind() string { return "EngineeringWorkPackage" }

// RecordID implements Record.
func (w *EngineeringWorkPackage) RecordID() string { return w.WorkPackageID }

// SchemaVer implements Record. It is distinct from the Work Package's own
// content Version field, which counts blueprint revisions.
func (w *EngineeringWorkPackage) SchemaVer() SchemaVersion { return w.SchemaVersion }

// Validate enforces the schema constraints plus the semantic requirements of
// docs/PROTOCOLS.md §7.2.
func (w *EngineeringWorkPackage) Validate() error {
	const kind = "EngineeringWorkPackage"
	if err := w.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	for field, value := range map[string]string{
		"work_package_id":        w.WorkPackageID,
		"task_id":                w.TaskID,
		"project_id":             w.ProjectID,
		"project_state_revision": w.ProjectStateRevision,
		"objective":              w.Objective,
		"rationale":              w.Rationale,
		"architectural_intent":   w.ArchitecturalIntent,
	} {
		if err := requireNonEmpty(kind, field, value); err != nil {
			return err
		}
	}
	if w.Version < 1 {
		return enumError(kind, "version", "0", ">= 1")
	}
	if len(w.BaseCommit) < 7 {
		return requireMinItems(kind, "base_commit characters", len(w.BaseCommit), 7)
	}
	if !w.ChangeClass.Valid() {
		return enumError(kind, "change_class", string(w.ChangeClass), "local", "systemic", "architectural")
	}
	for _, a := range w.Assumptions {
		if err := requireNonEmpty(kind, "assumptions[].id", a.ID); err != nil {
			return err
		}
		if err := requireNonEmpty(kind, "assumptions[].statement", a.Statement); err != nil {
			return err
		}
		if !a.Status.Valid() {
			return enumError(kind, "assumptions[].status", string(a.Status), "verified", "accepted_risk", "unverified")
		}
	}
	if err := requireMinItems(kind, "guidance", len(w.Guidance), 1); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(w.Guidance))
	for _, g := range w.Guidance {
		if err := requireNonEmpty(kind, "guidance[].id", g.ID); err != nil {
			return err
		}
		// Guidance IDs are cited by ReviewResult.must_compliance, so a
		// duplicate would make a compliance report ambiguous.
		if _, dup := seen[g.ID]; dup {
			return enumError(kind, "guidance[].id", g.ID, "unique guidance ids")
		}
		seen[g.ID] = struct{}{}
		if !g.Strength.Valid() {
			return enumError(kind, "guidance[].strength", string(g.Strength),
				"MUST", "SHOULD", "SUGGESTED", "LOCAL_DISCRETION")
		}
		if err := requireNonEmpty(kind, "guidance[].statement", g.Statement); err != nil {
			return err
		}
	}
	if err := requireMinItems(kind, "acceptance_criteria", len(w.AcceptanceCriteria), 1); err != nil {
		return err
	}
	if err := requireMinItems(kind, "validation_requirements", len(w.ValidationRequirements), 1); err != nil {
		return err
	}
	if err := requireMinItems(kind, "escalation_conditions", len(w.EscalationConditions), 1); err != nil {
		return err
	}
	return nil
}

// MarshalJSON guarantees the required arrays serialise as `[]`, never `null`.
func (w EngineeringWorkPackage) MarshalJSON() ([]byte, error) {
	type alias EngineeringWorkPackage
	out := alias(w)
	out.Assumptions = orEmpty(out.Assumptions)
	out.Scope.InScope = orEmpty(out.Scope.InScope)
	out.Scope.OutOfScope = orEmpty(out.Scope.OutOfScope)
	return json.Marshal(out)
}
