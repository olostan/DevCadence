package protocol

import (
	"encoding/json"

	"github.com/olostan/DevCadience/internal/errs"
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

// ValidationResult is the deterministic evidence for one attempt or
// integration (docs/PROTOCOLS.md §10).
type ValidationResult struct {
	SchemaVersion SchemaVersion     `json:"schema_version"`
	ValidationID  string            `json:"validation_id"`
	ProjectID     string            `json:"project_id"`
	AttemptID     string            `json:"attempt_id"`
	Commit        string            `json:"commit"`
	Status        ValidationOutcome `json:"status"`
	Checks        []CheckResult     `json:"checks"`
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
		"attempt_id":    v.AttemptID,
		"commit":        v.Commit,
	} {
		if err := requireNonEmpty(kind, field, value); err != nil {
			return err
		}
	}
	if !v.Status.Valid() {
		return enumError(kind, "status", string(v.Status), "pass", "fail", "error", "cancelled")
	}
	sawFailure := false
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
