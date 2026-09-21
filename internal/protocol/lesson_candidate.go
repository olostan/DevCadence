package protocol

import "encoding/json"

// LessonScope bounds how widely a lesson may apply. DCI-073 forbids turning a
// single anecdote into a cross-project rule, so scope is a durable field that
// promotion policy reads, not a narrative aside.
type LessonScope string

const (
	LessonScopeTask              LessonScope = "task"
	LessonScopeProject           LessonScope = "project"
	LessonScopeLanguageFramework LessonScope = "language_framework"
	LessonScopeOrganization      LessonScope = "organization"
	LessonScopeGlobal            LessonScope = "global"
)

// Valid reports whether the scope is defined by the schema.
func (s LessonScope) Valid() bool {
	switch s {
	case LessonScopeTask, LessonScopeProject, LessonScopeLanguageFramework,
		LessonScopeOrganization, LessonScopeGlobal:
		return true
	}
	return false
}

// LessonType names what kind of normative artifact a lesson would change.
type LessonType string

const (
	LessonTypeProjectInvariant  LessonType = "project_invariant"
	LessonTypeEngineeringSkill  LessonType = "engineering_skill"
	LessonTypeRouting           LessonType = "routing"
	LessonTypePrompt            LessonType = "prompt"
	LessonTypeTestHeuristic     LessonType = "test_heuristic"
	LessonTypeReviewRule        LessonType = "review_rule"
	LessonTypeEscalationRule    LessonType = "escalation_rule"
	LessonTypeTaskDecomposition LessonType = "task_decomposition"
	LessonTypeArchitecture      LessonType = "architecture"
	LessonTypeOther             LessonType = "other"
)

// Valid reports whether the type is defined by the schema.
func (t LessonType) Valid() bool {
	switch t {
	case LessonTypeProjectInvariant, LessonTypeEngineeringSkill, LessonTypeRouting,
		LessonTypePrompt, LessonTypeTestHeuristic, LessonTypeReviewRule,
		LessonTypeEscalationRule, LessonTypeTaskDecomposition, LessonTypeArchitecture,
		LessonTypeOther:
		return true
	}
	return false
}

// LessonStatus is the governed lifecycle of a candidate (docs/LEARNING.md).
// A candidate never reaches "promoted" without passing through evaluation,
// which is how DCI-071 keeps learning from becoming self-modification.
type LessonStatus string

const (
	LessonProposed          LessonStatus = "proposed"
	LessonEvidenceGathering LessonStatus = "evidence_gathering"
	LessonEvaluationReady   LessonStatus = "evaluation_ready"
	LessonEvaluating        LessonStatus = "evaluating"
	LessonNeedsRevision     LessonStatus = "needs_revision"
	LessonApproved          LessonStatus = "approved"
	LessonPromoted          LessonStatus = "promoted"
	LessonMonitoring        LessonStatus = "monitoring"
	LessonStable            LessonStatus = "stable"
	LessonRejected          LessonStatus = "rejected"
	LessonRolledBack        LessonStatus = "rolled_back"
)

// Valid reports whether the status is defined by the schema.
func (s LessonStatus) Valid() bool {
	switch s {
	case LessonProposed, LessonEvidenceGathering, LessonEvaluationReady, LessonEvaluating,
		LessonNeedsRevision, LessonApproved, LessonPromoted, LessonMonitoring,
		LessonStable, LessonRejected, LessonRolledBack:
		return true
	}
	return false
}

// PromotionAuthority names who may promote a lesson into normative policy.
type PromotionAuthority string

const (
	PromotionPolicy    PromotionAuthority = "policy"
	PromotionPrincipal PromotionAuthority = "principal"
	PromotionHuman     PromotionAuthority = "human"
)

// Valid reports whether the authority is defined by the schema.
func (a PromotionAuthority) Valid() bool {
	switch a {
	case PromotionPolicy, PromotionPrincipal, PromotionHuman:
		return true
	}
	return false
}

// LessonCandidate is a proposal derived from trajectories, never an applied
// change (DCI-071, docs/PROTOCOLS.md §16).
type LessonCandidate struct {
	SchemaVersion     SchemaVersion `json:"schema_version"`
	LessonCandidateID string        `json:"lesson_candidate_id"`
	Scope             LessonScope   `json:"scope"`
	Type              LessonType    `json:"type"`
	Observation       string        `json:"observation"`
	// TrajectoryRefs must be non-empty: a lesson with no evidence is an
	// opinion, and DCI-070 grounds learning in retained trajectories.
	TrajectoryRefs []string `json:"trajectory_refs"`
	ProposedChange string   `json:"proposed_change"`

	ExpectedBenefit         *string            `json:"expected_benefit,omitempty"`
	PossibleCounterexamples []string           `json:"possible_counterexamples,omitempty"`
	ConflictRefs            []string           `json:"conflict_refs,omitempty"`
	EvaluationPlan          string             `json:"evaluation_plan"`
	PromotionAuthority      PromotionAuthority `json:"promotion_authority,omitempty"`
	RollbackPlan            *string            `json:"rollback_plan,omitempty"`
	Status                  LessonStatus       `json:"status"`
}

// RecordKind implements Record.
func (l *LessonCandidate) RecordKind() string { return "LessonCandidate" }

// RecordID implements Record.
func (l *LessonCandidate) RecordID() string { return l.LessonCandidateID }

// SchemaVer implements Record.
func (l *LessonCandidate) SchemaVer() SchemaVersion { return l.SchemaVersion }

// Validate enforces schema constraints and the evidence requirement.
func (l *LessonCandidate) Validate() error {
	const kind = "LessonCandidate"
	if err := l.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	for field, value := range map[string]string{
		"lesson_candidate_id": l.LessonCandidateID,
		"observation":         l.Observation,
		"proposed_change":     l.ProposedChange,
		"evaluation_plan":     l.EvaluationPlan,
	} {
		if err := requireNonEmpty(kind, field, value); err != nil {
			return err
		}
	}
	if !l.Scope.Valid() {
		return enumError(kind, "scope", string(l.Scope), "task", "project",
			"language_framework", "organization", "global")
	}
	if !l.Type.Valid() {
		return enumError(kind, "type", string(l.Type), "project_invariant", "engineering_skill",
			"routing", "prompt", "test_heuristic", "review_rule", "escalation_rule",
			"task_decomposition", "architecture", "other")
	}
	if !l.Status.Valid() {
		return enumError(kind, "status", string(l.Status), "proposed", "evidence_gathering",
			"evaluation_ready", "evaluating", "needs_revision", "approved", "promoted",
			"monitoring", "stable", "rejected", "rolled_back")
	}
	if l.PromotionAuthority != "" && !l.PromotionAuthority.Valid() {
		return enumError(kind, "promotion_authority", string(l.PromotionAuthority),
			"policy", "principal", "human")
	}
	if err := requireMinItems(kind, "trajectory_refs", len(l.TrajectoryRefs), 1); err != nil {
		return err
	}
	return nil
}

// MarshalJSON guarantees the required arrays serialise as `[]`, never `null`.
func (l LessonCandidate) MarshalJSON() ([]byte, error) {
	type alias LessonCandidate
	out := alias(l)
	out.TrajectoryRefs = orEmpty(out.TrajectoryRefs)
	return json.Marshal(out)
}
