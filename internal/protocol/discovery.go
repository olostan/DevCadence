package protocol

import (
	"encoding/json"

	"github.com/olostan/DevCadience/internal/errs"
)

// Discovery and specification records.
//
// These are the Day-0 contracts introduced by
// [ADR-0001](../../docs/adr/0001-discovery-specification-subsystem.md): the
// principal must understand the problem before it designs a solution, and
// what it understands has to be durable rather than held in a conversation
// (DCI-052).
//
// M1 provides their typed representations and their persistence — the
// immutable record store handles them like any other protocol record. The
// discovery workflow that produces them is not M1 work; no reducer derives
// state from them yet beyond the compact ProjectState projection, which stays
// unset until that workflow exists.

// SourceType names where a constraint or requirement came from.
//
// It is durable because DCI-005 requires an inference to stay distinguishable
// from a fact: "the human said so" and "the principal inferred it" must not
// read alike once written down.
type SourceType string

const (
	SourceHuman              SourceType = "human"
	SourceExternalFact       SourceType = "external_fact"
	SourceRepositoryFact     SourceType = "repository_fact"
	SourceExperiment         SourceType = "experiment"
	SourcePolicy             SourceType = "policy"
	SourceInference          SourceType = "inference"
	SourceProductDecision    SourceType = "product_decision"
	SourceHumanStatement     SourceType = "human_statement"
	SourcePrincipalInference SourceType = "principal_inference"
)

// ValidConstraintSource reports whether the source type may ground a
// ProblemModel constraint.
func (s SourceType) ValidConstraintSource() bool {
	switch s {
	case SourceHuman, SourceExternalFact, SourceRepositoryFact,
		SourceExperiment, SourcePolicy, SourceInference:
		return true
	}
	return false
}

// ValidRequirementSource reports whether the source type may ground a
// Requirement.
func (s SourceType) ValidRequirementSource() bool {
	switch s {
	case SourceProductDecision, SourceHumanStatement, SourceExternalFact,
		SourceRepositoryFact, SourceExperiment, SourcePrincipalInference, SourcePolicy:
		return true
	}
	return false
}

// Actor describes a user or operator the product serves.
type ProblemActor struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Needs       []string `json:"needs,omitempty"`
}

// Workflow is a primary path an actor takes through the product.
type Workflow struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	ActorRefs   []string `json:"actor_refs,omitempty"`
}

// ProblemScope bounds the problem, including what is deliberately deferred.
type ProblemScope struct {
	InScope        []string `json:"in_scope"`
	OutOfScope     []string `json:"out_of_scope"`
	FuturePossible []string `json:"future_possible,omitempty"`
}

// Constraint is a limit the solution must respect, with its provenance.
type Constraint struct {
	ID         string     `json:"id"`
	Category   string     `json:"category"`
	Statement  string     `json:"statement"`
	SourceType SourceType `json:"source_type"`
	SourceRef  *string    `json:"source_ref,omitempty"`
}

// ProblemModel is the Day-0 artifact of docs/LIFECYCLE.md §2: what problem is
// being solved, for whom, and what would count as success or failure.
//
// It is revisioned rather than mutated, so that a later reader can see how
// the understanding of the problem changed.
type ProblemModel struct {
	SchemaVersion    SchemaVersion  `json:"schema_version"`
	ProblemModelID   string         `json:"problem_model_id"`
	ProjectID        string         `json:"project_id"`
	Revision         int            `json:"revision"`
	ProblemStatement string         `json:"problem_statement"`
	DesiredOutcomes  []string       `json:"desired_outcomes"`
	Actors           []ProblemActor `json:"actors"`
	PrimaryWorkflows []Workflow     `json:"primary_workflows"`
	Scope            ProblemScope   `json:"scope"`
	SuccessCriteria  []string       `json:"success_criteria"`
	FailureCriteria  []string       `json:"failure_criteria"`
	Constraints      []Constraint   `json:"constraints"`

	ProductDecisionRefs []string `json:"product_decision_refs,omitempty"`
	RequirementRefs     []string `json:"requirement_refs,omitempty"`

	Assumptions []Assumption `json:"assumptions"`
	Unknowns    []string     `json:"unknowns"`
	RiskRefs    []string     `json:"risk_refs"`

	AmbiguityLedgerID *string `json:"ambiguity_ledger_id,omitempty"`
	// HumanReflectionRevision records which revision the human last reviewed,
	// so that "the human has seen this" cannot be assumed of later edits.
	HumanReflectionRevision *int `json:"human_reflection_revision,omitempty"`
}

// RecordKind implements Record.
func (m *ProblemModel) RecordKind() string { return "ProblemModel" }

// RecordID implements Record.
func (m *ProblemModel) RecordID() string { return m.ProblemModelID }

// SchemaVer implements Record.
func (m *ProblemModel) SchemaVer() SchemaVersion { return m.SchemaVersion }

// Validate enforces the schema's required fields and enumerations.
func (m *ProblemModel) Validate() error {
	const kind = "ProblemModel"
	if err := m.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	for field, value := range map[string]string{
		"problem_model_id":  m.ProblemModelID,
		"project_id":        m.ProjectID,
		"problem_statement": m.ProblemStatement,
	} {
		if err := requireNonEmpty(kind, field, value); err != nil {
			return err
		}
	}
	if m.Revision < 1 {
		return enumError(kind, "revision", "0", ">= 1")
	}
	if err := requireMinItems(kind, "desired_outcomes", len(m.DesiredOutcomes), 1); err != nil {
		return err
	}
	for _, actor := range m.Actors {
		if err := requireNonEmpty(kind, "actors[].id", actor.ID); err != nil {
			return err
		}
		if err := requireNonEmpty(kind, "actors[].description", actor.Description); err != nil {
			return err
		}
	}
	for _, workflow := range m.PrimaryWorkflows {
		if err := requireNonEmpty(kind, "primary_workflows[].id", workflow.ID); err != nil {
			return err
		}
		if err := requireNonEmpty(kind, "primary_workflows[].description", workflow.Description); err != nil {
			return err
		}
	}
	for _, constraint := range m.Constraints {
		if err := requireNonEmpty(kind, "constraints[].id", constraint.ID); err != nil {
			return err
		}
		if err := requireNonEmpty(kind, "constraints[].category", constraint.Category); err != nil {
			return err
		}
		if err := requireNonEmpty(kind, "constraints[].statement", constraint.Statement); err != nil {
			return err
		}
		if !constraint.SourceType.ValidConstraintSource() {
			return enumError(kind, "constraints[].source_type", string(constraint.SourceType),
				"human", "external_fact", "repository_fact", "experiment", "policy", "inference")
		}
	}
	for _, assumption := range m.Assumptions {
		if err := requireNonEmpty(kind, "assumptions[].id", assumption.ID); err != nil {
			return err
		}
		if err := requireNonEmpty(kind, "assumptions[].statement", assumption.Statement); err != nil {
			return err
		}
		if !assumption.Status.Valid() {
			return enumError(kind, "assumptions[].status", string(assumption.Status),
				"verified", "accepted_risk", "unverified")
		}
	}
	// The human cannot have reviewed a revision that does not exist yet.
	// Allowing it would let "the human has seen this" be claimed for edits
	// made after they looked (DCI-009).
	if m.HumanReflectionRevision != nil {
		if *m.HumanReflectionRevision < 1 || *m.HumanReflectionRevision > m.Revision {
			return errs.New(errs.CategoryIntegrity,
				"%s: human_reflection_revision %d must be between 1 and the current revision %d",
				kind, *m.HumanReflectionRevision, m.Revision)
		}
	}
	return nil
}

// MarshalJSON guarantees the required arrays serialise as `[]`, never `null`.
func (m ProblemModel) MarshalJSON() ([]byte, error) {
	type alias ProblemModel
	out := alias(m)
	out.DesiredOutcomes = orEmpty(out.DesiredOutcomes)
	out.Actors = orEmpty(out.Actors)
	out.PrimaryWorkflows = orEmpty(out.PrimaryWorkflows)
	out.Scope.InScope = orEmpty(out.Scope.InScope)
	out.Scope.OutOfScope = orEmpty(out.Scope.OutOfScope)
	out.SuccessCriteria = orEmpty(out.SuccessCriteria)
	out.FailureCriteria = orEmpty(out.FailureCriteria)
	out.Constraints = orEmpty(out.Constraints)
	out.Assumptions = orEmpty(out.Assumptions)
	out.Unknowns = orEmpty(out.Unknowns)
	out.RiskRefs = orEmpty(out.RiskRefs)
	return json.Marshal(out)
}
