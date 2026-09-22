package protocol

import (
	"encoding/json"

	"github.com/olostan/DevCadence/internal/errs"
)

// CheckStatus is the outcome of one deterministic check.
type CheckStatus string

const (
	CheckPass      CheckStatus = "pass"
	CheckFail      CheckStatus = "fail"
	CheckError     CheckStatus = "error"
	CheckCancelled CheckStatus = "cancelled"
	CheckSkipped   CheckStatus = "skipped"
)

// Valid reports whether the status is defined by the schema.
func (s CheckStatus) Valid() bool {
	switch s {
	case CheckPass, CheckFail, CheckError, CheckCancelled, CheckSkipped:
		return true
	}
	return false
}

// ValidationOutcome is the rolled-up outcome of a validation run. It omits
// "skipped": a run in which every check was skipped has not validated
// anything and must not be reported as a pass.
type ValidationOutcome string

const (
	ValidationPass      ValidationOutcome = "pass"
	ValidationFail      ValidationOutcome = "fail"
	ValidationError     ValidationOutcome = "error"
	ValidationCancelled ValidationOutcome = "cancelled"
)

// Valid reports whether the outcome is defined by the schema.
func (s ValidationOutcome) Valid() bool {
	switch s {
	case ValidationPass, ValidationFail, ValidationError, ValidationCancelled:
		return true
	}
	return false
}

// CheckResult captures one deterministic check exactly as the tool reported
// it (DCI-041). The command, exit code and artifact references are the
// authoritative record; Summary is a convenience, never the source of truth.
type CheckResult struct {
	ID               string      `json:"id"`
	Kind             string      `json:"kind"`
	Command          []string    `json:"command,omitempty"`
	ToolVersion      *string     `json:"tool_version,omitempty"`
	WorkingDirectory *string     `json:"working_directory,omitempty"`
	Status           CheckStatus `json:"status"`
	ExitCode         *int        `json:"exit_code,omitempty"`
	StartedAt        Timestamp   `json:"started_at"`
	FinishedAt       Timestamp   `json:"finished_at"`
	Summary          *string     `json:"summary,omitempty"`
	StdoutArtifact   *string     `json:"stdout_artifact,omitempty"`
	StderrArtifact   *string     `json:"stderr_artifact,omitempty"`
	OutputTruncated  bool        `json:"output_truncated,omitempty"`
}

// ValidationScope distinguishes validating one attempt's candidate from
// validating an integrated result or the accepted baseline. The three drive
// different task transitions, so the distinction is durable rather than
// inferred.
//
// It lives here, not in internal/events, because the durable ValidationResult
// and the compact ValidationCompleted event must name the same three scopes.
// Two enumerations that must agree are one enumeration written twice.
type ValidationScope string

const (
	// ScopeAttempt validates a candidate produced by one attempt.
	ScopeAttempt ValidationScope = "attempt"
	// ScopeIntegration validates the combined, integrated result.
	ScopeIntegration ValidationScope = "integration"
	// ScopeBaseline validates the accepted commit outside any task. It is the
	// source of ProjectState.validation.
	ScopeBaseline ValidationScope = "baseline"
)

// Valid reports whether the scope is known.
func (s ValidationScope) Valid() bool {
	switch s {
	case ScopeAttempt, ScopeIntegration, ScopeBaseline:
		return true
	}
	return false
}

// ValidationSubject states what a validation run validated.
//
// The subject is an explicit object rather than a set of optional top-level
// fields because the three scopes need different identifiers, and optional
// fields cannot express "required here, forbidden there". A reader does not
// have to infer the scope from which identifiers happen to be present: the
// record says what it is, and the combination is checked.
//
// It deliberately carries no integration identifier. The ValidationCompleted
// event has none either, and a field the event cannot corroborate could not
// be verified when the event and the record are cross-checked.
type ValidationSubject struct {
	Kind      ValidationScope `json:"kind"`
	TaskID    string          `json:"task_id,omitempty"`
	AttemptID string          `json:"attempt_id,omitempty"`
}

// Validate enforces the identifiers each scope requires and forbids.
func (s ValidationSubject) Validate(kind string) error {
	if !s.Kind.Valid() {
		return enumError(kind, "subject.kind", string(s.Kind), "attempt", "integration", "baseline")
	}
	switch s.Kind {
	case ScopeAttempt:
		if s.TaskID == "" || s.AttemptID == "" {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: subject.task_id and subject.attempt_id are required for scope attempt", kind)
		}
	case ScopeIntegration:
		if s.TaskID == "" {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: subject.task_id is required for scope integration", kind)
		}
		if s.AttemptID != "" {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: subject.attempt_id is not meaningful for scope integration; "+
					"an integration validates the merged result, not one attempt's candidate", kind)
		}
	case ScopeBaseline:
		if s.TaskID != "" || s.AttemptID != "" {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: scope baseline validates the accepted commit outside any task, "+
					"so subject.task_id and subject.attempt_id must be empty", kind)
		}
	}
	return nil
}

// ValidationResult is the deterministic evidence for one attempt, integration
// or baseline run (docs/PROTOCOLS.md §10).
type ValidationResult struct {
	SchemaVersion SchemaVersion     `json:"schema_version"`
	ValidationID  string            `json:"validation_id"`
	ProjectID     string            `json:"project_id"`
	Subject       ValidationSubject `json:"subject"`
	// Commit is required for every scope: a validation run always validates
	// some tree, and evidence that does not name what it is about cannot be
	// read as covering anything in particular.
	Commit string            `json:"commit"`
	Status ValidationOutcome `json:"status"`
	Checks []CheckResult     `json:"checks"`
}

// RecordKind implements Record.
func (v *ValidationResult) RecordKind() string { return "ValidationResult" }

// RecordID implements Record.
func (v *ValidationResult) RecordID() string { return v.ValidationID }

// SchemaVer implements Record.
func (v *ValidationResult) SchemaVer() SchemaVersion { return v.SchemaVersion }

// Validate enforces schema constraints plus the consistency rules that make
// the rolled-up status trustworthy.
func (v *ValidationResult) Validate() error {
	const kind = "ValidationResult"
	if err := v.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	for field, value := range map[string]string{
		"validation_id": v.ValidationID,
		"project_id":    v.ProjectID,
		"commit":        v.Commit,
	} {
		if err := requireNonEmpty(kind, field, value); err != nil {
			return err
		}
	}
	if err := v.Subject.Validate(kind); err != nil {
		return err
	}
	if !v.Status.Valid() {
		return enumError(kind, "status", string(v.Status), "pass", "fail", "error", "cancelled")
	}
	sawFailure := false
	sawPass := false
	for _, c := range v.Checks {
		if err := requireNonEmpty(kind, "checks[].id", c.ID); err != nil {
			return err
		}
		if err := requireNonEmpty(kind, "checks[].kind", c.Kind); err != nil {
			return err
		}
		if !c.Status.Valid() {
			return enumError(kind, "checks[].status", string(c.Status),
				"pass", "fail", "error", "cancelled", "skipped")
		}
		if c.FinishedAt.Time().Before(c.StartedAt.Time()) {
			return errs.New(errs.CategoryIntegrity,
				"%s: check %s finished before it started", kind, c.ID)
		}
		if c.Status == CheckFail || c.Status == CheckError {
			sawFailure = true
		}
		if c.Status == CheckPass {
			sawPass = true
		}
	}
	// A pass must rest on something that actually ran. ValidationOutcome
	// omits "skipped" precisely because a run in which nothing executed has
	// validated nothing — and a run with no checks at all is the same claim
	// with less ceremony. Accepting either would let "validated" mean "we
	// tried nothing and found no problems" (DCI-040: deterministic evidence
	// is what acceptance rests on).
	if v.Status == ValidationPass && !sawPass {
		return errs.New(errs.CategoryIntegrity,
			"%s: status is pass but no check passed; a run that executed nothing has validated nothing", kind)
	}
	// A run that reports "pass" while containing a failed check would let a
	// model-authored summary override tool output, which DCI-041 forbids.
	if v.Status == ValidationPass && sawFailure {
		return errs.New(errs.CategoryIntegrity,
			"%s: status is pass but at least one check failed", kind)
	}
	return nil
}

// MarshalJSON guarantees the required arrays serialise as `[]`, never `null`.
func (v ValidationResult) MarshalJSON() ([]byte, error) {
	type alias ValidationResult
	out := alias(v)
	out.Checks = orEmpty(out.Checks)
	return json.Marshal(out)
}
