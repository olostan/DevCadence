package protocol

import (
	"encoding/json"

	"github.com/olostan/DevCadience/internal/errs"
)

// ExperimentStatus is the lifecycle of a discovery experiment.
type ExperimentStatus string

const (
	ExperimentPlanned      ExperimentStatus = "planned"
	ExperimentRunning      ExperimentStatus = "running"
	ExperimentCompleted    ExperimentStatus = "completed"
	ExperimentInconclusive ExperimentStatus = "inconclusive"
	ExperimentCancelled    ExperimentStatus = "cancelled"
)

// Valid reports whether the status is defined by the schema.
func (s ExperimentStatus) Valid() bool {
	switch s {
	case ExperimentPlanned, ExperimentRunning, ExperimentCompleted,
		ExperimentInconclusive, ExperimentCancelled:
		return true
	}
	return false
}

// DiscoveryExperiment is a probe run to settle a question about feasibility
// or behaviour before committing to a design (docs/LIFECYCLE.md §3).
//
// Limitations are required rather than optional: an experiment whose scope is
// not stated invites its result being read as more general than it is, which
// is the failure DCI-012 guards against.
type DiscoveryExperiment struct {
	SchemaVersion SchemaVersion `json:"schema_version"`
	ExperimentID  string        `json:"experiment_id"`
	ProjectID     string        `json:"project_id"`
	Question      string        `json:"question"`
	Hypothesis    string        `json:"hypothesis"`
	Method        string        `json:"method"`
	Environment   string        `json:"environment"`

	AcceptanceThresholds []string         `json:"acceptance_thresholds,omitempty"`
	Status               ExperimentStatus `json:"status"`
	ResultSummary        *string          `json:"result_summary,omitempty"`
	EvidenceRefs         []string         `json:"evidence_refs,omitempty"`
	Limitations          []string         `json:"limitations"`
	RequirementRefs      []string         `json:"requirement_refs,omitempty"`
}

// RecordKind implements Record.
func (e *DiscoveryExperiment) RecordKind() string { return "DiscoveryExperiment" }

// RecordID implements Record.
func (e *DiscoveryExperiment) RecordID() string { return e.ExperimentID }

// SchemaVer implements Record.
func (e *DiscoveryExperiment) SchemaVer() SchemaVersion { return e.SchemaVersion }

// Validate enforces the schema's required fields and enumerations.
func (e *DiscoveryExperiment) Validate() error {
	const kind = "DiscoveryExperiment"
	if err := e.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	for field, value := range map[string]string{
		"experiment_id": e.ExperimentID,
		"project_id":    e.ProjectID,
		"question":      e.Question,
		"hypothesis":    e.Hypothesis,
		"method":        e.Method,
		"environment":   e.Environment,
	} {
		if err := requireNonEmpty(kind, field, value); err != nil {
			return err
		}
	}
	if !e.Status.Valid() {
		return enumError(kind, "status", string(e.Status),
			"planned", "running", "completed", "inconclusive", "cancelled")
	}
	// A completed experiment with no result reports nothing, which would let
	// "we ran it" stand in for "we learned something".
	if e.Status == ExperimentCompleted && (e.ResultSummary == nil || *e.ResultSummary == "") {
		return enumError(kind, "result_summary", "(empty)",
			"a result summary when the experiment is completed")
	}
	return nil
}

// MarshalJSON guarantees the required arrays serialise as `[]`, never `null`.
func (e DiscoveryExperiment) MarshalJSON() ([]byte, error) {
	type alias DiscoveryExperiment
	out := alias(e)
	out.Limitations = orEmpty(out.Limitations)
	return json.Marshal(out)
}

// CheckStatusValue is the outcome of one specification-readiness check.
//
// "accepted_risk" is distinct from "complete": proceeding with a known gap is
// a decision someone owns, not a completed check (docs/LIFECYCLE.md §11).
type CheckStatusValue string

const (
	ReadinessComplete      CheckStatusValue = "complete"
	ReadinessIncomplete    CheckStatusValue = "incomplete"
	ReadinessNotApplicable CheckStatusValue = "not_applicable"
	ReadinessAcceptedRisk  CheckStatusValue = "accepted_risk"
)

// Valid reports whether the status is defined by the schema.
func (s CheckStatusValue) Valid() bool {
	switch s {
	case ReadinessComplete, ReadinessIncomplete, ReadinessNotApplicable, ReadinessAcceptedRisk:
		return true
	}
	return false
}

// ReadinessCheck is one gate in the Design Readiness Gate.
type ReadinessCheck struct {
	Status       CheckStatusValue `json:"status"`
	Notes        *string          `json:"notes,omitempty"`
	EvidenceRefs []string         `json:"evidence_refs,omitempty"`
}

// ReadinessChecks is the fixed set of gates. It is a struct rather than a map
// so that a missing gate is a compile-time and schema error rather than an
// absent key nobody notices.
type ReadinessChecks struct {
	ProblemOutcome                 ReadinessCheck `json:"problem_outcome"`
	PrimaryWorkflows               ReadinessCheck `json:"primary_workflows"`
	ScopeBoundaries                ReadinessCheck `json:"scope_boundaries"`
	ArchitectureSensitiveQuestions ReadinessCheck `json:"architecture_sensitive_questions"`
	SecurityPrivacy                ReadinessCheck `json:"security_privacy"`
	MaterialExternalFacts          ReadinessCheck `json:"material_external_facts"`
	FeasibilityAssumptions         ReadinessCheck `json:"feasibility_assumptions"`
	Contradictions                 ReadinessCheck `json:"contradictions"`
	IndependentReviews             ReadinessCheck `json:"independent_reviews"`
	HumanReflection                ReadinessCheck `json:"human_reflection"`
}

// All returns the checks with their field names, for uniform validation and
// reporting. The order is the schema's declaration order, so output is stable.
func (c ReadinessChecks) All() []struct {
	Name  string
	Check ReadinessCheck
} {
	return []struct {
		Name  string
		Check ReadinessCheck
	}{
		{"problem_outcome", c.ProblemOutcome},
		{"primary_workflows", c.PrimaryWorkflows},
		{"scope_boundaries", c.ScopeBoundaries},
		{"architecture_sensitive_questions", c.ArchitectureSensitiveQuestions},
		{"security_privacy", c.SecurityPrivacy},
		{"material_external_facts", c.MaterialExternalFacts},
		{"feasibility_assumptions", c.FeasibilityAssumptions},
		{"contradictions", c.Contradictions},
		{"independent_reviews", c.IndependentReviews},
		{"human_reflection", c.HumanReflection},
	}
}

// RemainingUnknown is an open question the readiness assessment leaves
// unresolved.
//
// ArchitectureSafeToDefer is the field that carries the judgement: an unknown
// that is not safe to defer is what makes a verdict "not_ready", and stating
// it per unknown stops "we still have questions" from being an undifferentiated
// worry.
type RemainingUnknown struct {
	Statement               string  `json:"statement"`
	ArchitectureSafeToDefer bool    `json:"architecture_safe_to_defer"`
	Boundary                *string `json:"boundary,omitempty"`
}

// ReadinessVerdict is the gate's outcome.
type ReadinessVerdict string

const (
	NotReady               ReadinessVerdict = "not_ready"
	ReadyForArchitecture   ReadinessVerdict = "ready_for_architecture"
	ReadyWithExplicitRisks ReadinessVerdict = "ready_with_explicit_risks"
)

// Valid reports whether the verdict is defined by the schema.
func (v ReadinessVerdict) Valid() bool {
	switch v {
	case NotReady, ReadyForArchitecture, ReadyWithExplicitRisks:
		return true
	}
	return false
}

// SpecificationReadiness is the evidence-based gate of docs/LIFECYCLE.md §11.
//
// It pins the ProblemModel revision it judged, so that a later edit to the
// problem cannot inherit a readiness verdict it was never assessed against.
type SpecificationReadiness struct {
	SchemaVersion        SchemaVersion   `json:"schema_version"`
	ReadinessID          string          `json:"readiness_id"`
	ProjectID            string          `json:"project_id"`
	ProblemModelID       string          `json:"problem_model_id"`
	ProblemModelRevision int             `json:"problem_model_revision"`
	Checks               ReadinessChecks `json:"checks"`

	RemainingUnknowns []RemainingUnknown `json:"remaining_unknowns,omitempty"`
	ReviewRefs        []string           `json:"review_refs,omitempty"`
	Verdict           ReadinessVerdict   `json:"verdict"`
	Rationale         *string            `json:"rationale,omitempty"`
}

// RecordKind implements Record.
func (r *SpecificationReadiness) RecordKind() string { return "SpecificationReadiness" }

// RecordID implements Record.
func (r *SpecificationReadiness) RecordID() string { return r.ReadinessID }

// SchemaVer implements Record.
func (r *SpecificationReadiness) SchemaVer() SchemaVersion { return r.SchemaVersion }

// Validate enforces the schema's required fields and enumerations, plus the
// consistency between the individual checks and the overall verdict.
func (r *SpecificationReadiness) Validate() error {
	const kind = "SpecificationReadiness"
	if err := r.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	for field, value := range map[string]string{
		"readiness_id":     r.ReadinessID,
		"project_id":       r.ProjectID,
		"problem_model_id": r.ProblemModelID,
	} {
		if err := requireNonEmpty(kind, field, value); err != nil {
			return err
		}
	}
	if r.ProblemModelRevision < 1 {
		return enumError(kind, "problem_model_revision", "0", ">= 1")
	}
	if !r.Verdict.Valid() {
		return enumError(kind, "verdict", string(r.Verdict),
			"not_ready", "ready_for_architecture", "ready_with_explicit_risks")
	}
	for _, unknown := range r.RemainingUnknowns {
		if err := requireNonEmpty(kind, "remaining_unknowns[].statement", unknown.Statement); err != nil {
			return err
		}
		// An unknown that is not safe to defer contradicts any verdict other
		// than not_ready: the gate exists to stop architecture starting on it.
		if !unknown.ArchitectureSafeToDefer && r.Verdict != NotReady {
			return enumError(kind, "verdict", string(r.Verdict),
				"not_ready while an unknown is not safe to defer past architecture")
		}
		// docs/DISCOVERY_AND_SPECIFICATION.md §5: ambiguity may be deferred
		// only when a safe boundary prevents it from silently becoming
		// architecture. A bare "safe to defer" is an assertion, not the
		// boundary that makes it safe, so the boundary is required.
		if unknown.ArchitectureSafeToDefer && (unknown.Boundary == nil || *unknown.Boundary == "") {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: remaining unknown %q is marked safe to defer but states no boundary; "+
					"an unbounded deferral is a silent architectural decision",
				kind, unknown.Statement)
		}
	}
	sawIncomplete := false
	sawAcceptedRisk := false
	for _, entry := range r.Checks.All() {
		if !entry.Check.Status.Valid() {
			return enumError(kind, "checks."+entry.Name, string(entry.Check.Status),
				"complete", "incomplete", "not_applicable", "accepted_risk")
		}
		switch entry.Check.Status {
		case ReadinessIncomplete:
			sawIncomplete = true
		case ReadinessAcceptedRisk:
			sawAcceptedRisk = true
		}
	}
	// A verdict that contradicts its own checks would let the gate be passed
	// by assertion rather than by evidence, which is exactly what
	// docs/LIFECYCLE.md §11 rules out.
	if r.Verdict != NotReady && sawIncomplete {
		return enumError(kind, "verdict", string(r.Verdict),
			"not_ready while a readiness check is incomplete")
	}
	if r.Verdict == ReadyForArchitecture && sawAcceptedRisk {
		return enumError(kind, "verdict", string(r.Verdict),
			"ready_with_explicit_risks when a check is an accepted risk")
	}
	return nil
}
