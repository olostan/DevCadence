package protocol

import (
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
)

// ActorBasis is the full six-field bound identity basis from the endpoint
// (EWP WP-M5-R2 Part A).
//
// It captures the endpoint, model, and account identifiers so that any gate
// can re-derive the canonical actor identity under any supported independence
// basis without re-evaluating the model runtime.
type ActorBasis struct {
	EndpointID    string `json:"endpoint_id"`
	ModelID       string `json:"model_id"`
	ModelRevision string `json:"model_revision"`
	Provider      string `json:"provider"`
	ModelFamily   string `json:"model_family"`
	AccountRef    string `json:"account_ref"`
}

// InvocationProvenance captures durable execution provenance binding an
// invocation to its endpoint basis, digests, role, and actor (WP-M5-R2 Part A).
type InvocationProvenance struct {
	SchemaVersion         SchemaVersion   `json:"schema_version"`
	ProvenanceID          string          `json:"provenance_id"`
	ProjectID             string          `json:"project_id"`
	TaskID                string          `json:"task_id"`
	AttemptID             string          `json:"attempt_id"`
	WorkPackageID         string          `json:"work_package_id"`
	Role                  ProvenanceRole  `json:"role"`
	Actor                 ActorProvenance `json:"actor"`
	Basis                 ActorBasis      `json:"basis"`
	IndependenceBasis     string          `json:"independence_basis"`
	EndpointBindingDigest string          `json:"endpoint_binding_digest"`
	ContextManifestDigest string          `json:"context_manifest_digest"`
	PromptDigest          string          `json:"prompt_digest"`
	CandidateCommit       string          `json:"candidate_commit,omitempty"`
	Dimension             ReviewDimension `json:"dimension,omitempty"`
	StartedAt             time.Time       `json:"started_at"`
}

// RecordKind implements Record.
func (p *InvocationProvenance) RecordKind() string { return "InvocationProvenance" }

// RecordID implements Record.
func (p *InvocationProvenance) RecordID() string { return p.ProvenanceID }

// SchemaVer implements Record.
func (p *InvocationProvenance) SchemaVer() SchemaVersion { return p.SchemaVersion }

// ProjectOf implements ProjectScoped.
func (p *InvocationProvenance) ProjectOf() string { return p.ProjectID }

// Validate checks that the invocation provenance record conforms to its contract.
func (p *InvocationProvenance) Validate() error {
	const kind = "InvocationProvenance"
	if err := p.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if err := requireNonEmptyTrimmed(kind, "provenance_id", p.ProvenanceID); err != nil {
		return err
	}
	if err := requireNonEmptyTrimmed(kind, "project_id", p.ProjectID); err != nil {
		return err
	}
	if err := requireNonEmptyTrimmed(kind, "task_id", p.TaskID); err != nil {
		return err
	}
	if err := requireNonEmptyTrimmed(kind, "attempt_id", p.AttemptID); err != nil {
		return err
	}
	if err := requireNonEmptyTrimmed(kind, "work_package_id", p.WorkPackageID); err != nil {
		return err
	}
	if !p.Role.Valid() {
		return enumError(kind, "role", string(p.Role),
			string(ProvenanceRoleReviewer), string(ProvenanceRoleImplementer), string(ProvenanceRoleVerifier))
	}
	if err := p.Actor.Validate(kind+".actor", p.Role); err != nil {
		return err
	}
	if err := requireNonEmptyTrimmed(kind, "independence_basis", p.IndependenceBasis); err != nil {
		return err
	}
	if p.IndependenceBasis != "endpoint_model" && p.IndependenceBasis != "model_family_account" {
		return enumError(kind, "independence_basis", p.IndependenceBasis, "endpoint_model", "model_family_account")
	}
	if err := validateSHA256Digest(kind, "endpoint_binding_digest", p.EndpointBindingDigest); err != nil {
		return err
	}
	if err := validateSHA256Digest(kind, "context_manifest_digest", p.ContextManifestDigest); err != nil {
		return err
	}
	if err := validateSHA256Digest(kind, "prompt_digest", p.PromptDigest); err != nil {
		return err
	}
	if p.StartedAt.IsZero() {
		return errs.New(errs.CategoryInvalidArgument, "%s: started_at is required", kind)
	}

	switch p.Role {
	case ProvenanceRoleReviewer:
		if err := requireNonEmptyTrimmed(kind, "candidate_commit", p.CandidateCommit); err != nil {
			return err
		}
		if !p.Dimension.Valid() {
			return enumError(kind, "dimension", string(p.Dimension),
				string(DimensionCorrectness), string(DimensionArchitecture), string(DimensionInvariants),
				string(DimensionSecurity), string(DimensionTestAdequacy), string(DimensionConcurrency),
				string(DimensionPerformance), string(DimensionMaintainability), string(DimensionOther))
		}
	case ProvenanceRoleVerifier:
		if err := requireNonEmptyTrimmed(kind, "candidate_commit", p.CandidateCommit); err != nil {
			return err
		}
		if p.Dimension != "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: dimension must be empty for verifier", kind)
		}
	case ProvenanceRoleImplementer:
		if p.Dimension != "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: dimension must be empty for implementer", kind)
		}
	}
	return nil
}

// ReviewInvocationIntent is a pre-invocation intent record stored before
// calling a reviewer model, guarding against concurrent duplicate reviews
// (WP-M5-R2 Part A, PROTOCOLS §19).
type ReviewInvocationIntent struct {
	SchemaVersion         SchemaVersion   `json:"schema_version"`
	ReviewID              string          `json:"review_id"`
	ProjectID             string          `json:"project_id"`
	TaskID                string          `json:"task_id"`
	AttemptID             string          `json:"attempt_id"`
	WorkPackageID         string          `json:"work_package_id"`
	Dimension             ReviewDimension `json:"dimension"`
	InvocationID          string          `json:"invocation_id"`
	CandidateCommit       string          `json:"candidate_commit"`
	InvocationNumber      int             `json:"invocation_number"`
	ReviewerBasis         ActorBasis      `json:"reviewer_basis"`
	IndependenceBasis     string          `json:"independence_basis"`
	EndpointBindingDigest string          `json:"endpoint_binding_digest"`
	StartedAt             time.Time       `json:"started_at"`
}

// RecordKind implements Record.
func (i *ReviewInvocationIntent) RecordKind() string { return "ReviewInvocationIntent" }

// RecordID implements Record. Returns deterministic "intent:" + AttemptID + ":" + Dimension.
func (i *ReviewInvocationIntent) RecordID() string {
	return "intent:" + i.AttemptID + ":" + string(i.Dimension)
}

// SchemaVer implements Record.
func (i *ReviewInvocationIntent) SchemaVer() SchemaVersion { return i.SchemaVersion }

// ProjectOf implements ProjectScoped.
func (i *ReviewInvocationIntent) ProjectOf() string { return i.ProjectID }

// Validate checks that the review invocation intent record conforms to its contract.
func (i *ReviewInvocationIntent) Validate() error {
	const kind = "ReviewInvocationIntent"
	if err := i.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if err := requireNonEmptyTrimmed(kind, "review_id", i.ReviewID); err != nil {
		return err
	}
	if err := requireNonEmptyTrimmed(kind, "project_id", i.ProjectID); err != nil {
		return err
	}
	if err := requireNonEmptyTrimmed(kind, "task_id", i.TaskID); err != nil {
		return err
	}
	if err := requireNonEmptyTrimmed(kind, "attempt_id", i.AttemptID); err != nil {
		return err
	}
	if strings.Contains(i.AttemptID, ":") {
		return errs.New(errs.CategoryInvalidArgument, "%s: attempt_id %q must not contain ':'", kind, i.AttemptID)
	}
	if err := requireNonEmptyTrimmed(kind, "work_package_id", i.WorkPackageID); err != nil {
		return err
	}
	if strings.Contains(string(i.Dimension), ":") {
		return errs.New(errs.CategoryInvalidArgument, "%s: dimension %q must not contain ':'", kind, string(i.Dimension))
	}
	if !i.Dimension.Valid() {
		return enumError(kind, "dimension", string(i.Dimension),
			string(DimensionCorrectness), string(DimensionArchitecture), string(DimensionInvariants),
			string(DimensionSecurity), string(DimensionTestAdequacy), string(DimensionConcurrency),
			string(DimensionPerformance), string(DimensionMaintainability), string(DimensionOther))
	}
	if err := requireNonEmptyTrimmed(kind, "invocation_id", i.InvocationID); err != nil {
		return err
	}
	if err := requireNonEmptyTrimmed(kind, "candidate_commit", i.CandidateCommit); err != nil {
		return err
	}
	if i.InvocationNumber != 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: invocation_number must be 1, got %d", kind, i.InvocationNumber)
	}
	if err := requireNonEmptyTrimmed(kind, "independence_basis", i.IndependenceBasis); err != nil {
		return err
	}
	if i.IndependenceBasis != "endpoint_model" && i.IndependenceBasis != "model_family_account" {
		return enumError(kind, "independence_basis", i.IndependenceBasis, "endpoint_model", "model_family_account")
	}
	if err := validateSHA256Digest(kind, "endpoint_binding_digest", i.EndpointBindingDigest); err != nil {
		return err
	}
	if i.StartedAt.IsZero() {
		return errs.New(errs.CategoryInvalidArgument, "%s: started_at is required", kind)
	}

	expectedID := "intent:" + i.AttemptID + ":" + string(i.Dimension)
	if i.RecordID() != expectedID {
		return errs.New(errs.CategoryInvalidArgument, "%s: record id must be %q, got %q", kind, expectedID, i.RecordID())
	}
	return nil
}

// ReviewInvocationOutcome describes the terminal outcome of a review invocation.
type ReviewInvocationOutcome string

const (
	OutcomeCompleted     ReviewInvocationOutcome = "completed"
	OutcomeOutputInvalid ReviewInvocationOutcome = "output_invalid"
	OutcomeLimitReached  ReviewInvocationOutcome = "limit_reached"
	OutcomeDriverError   ReviewInvocationOutcome = "driver_error"
	OutcomeCancelled     ReviewInvocationOutcome = "cancelled"
)

// Valid reports whether the review invocation outcome is known.
func (o ReviewInvocationOutcome) Valid() bool {
	switch o {
	case OutcomeCompleted, OutcomeOutputInvalid, OutcomeLimitReached, OutcomeDriverError, OutcomeCancelled:
		return true
	}
	return false
}

// ReviewInvocation is the terminal protocol record capturing the outcome of a
// review invocation (WP-M5-R2 Part A).
type ReviewInvocation struct {
	SchemaVersion       SchemaVersion           `json:"schema_version"`
	ReviewID            string                  `json:"review_id"`
	ProjectID           string                  `json:"project_id"`
	TaskID              string                  `json:"task_id"`
	AttemptID           string                  `json:"attempt_id"`
	WorkPackageID       string                  `json:"work_package_id"`
	Dimension           ReviewDimension         `json:"dimension"`
	InvocationID        string                  `json:"invocation_id"`
	ProvenanceID        string                  `json:"provenance_id"`
	CandidateCommit     string                  `json:"candidate_commit"`
	InvocationNumber    int                     `json:"invocation_number"`
	Outcome             ReviewInvocationOutcome `json:"outcome"`
	StartedAt           time.Time               `json:"started_at"`
	EndedAt             time.Time               `json:"ended_at"`
	UsageArtifactDigest string                  `json:"usage_artifact_digest,omitempty"`
}

// RecordKind implements Record.
func (r *ReviewInvocation) RecordKind() string { return "ReviewInvocation" }

// RecordID implements Record.
func (r *ReviewInvocation) RecordID() string { return r.ReviewID }

// SchemaVer implements Record.
func (r *ReviewInvocation) SchemaVer() SchemaVersion { return r.SchemaVersion }

// ProjectOf implements ProjectScoped.
func (r *ReviewInvocation) ProjectOf() string { return r.ProjectID }

// Validate checks that the review invocation record conforms to its contract.
func (r *ReviewInvocation) Validate() error {
	const kind = "ReviewInvocation"
	if err := r.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if err := requireNonEmptyTrimmed(kind, "review_id", r.ReviewID); err != nil {
		return err
	}
	if err := requireNonEmptyTrimmed(kind, "project_id", r.ProjectID); err != nil {
		return err
	}
	if err := requireNonEmptyTrimmed(kind, "task_id", r.TaskID); err != nil {
		return err
	}
	if err := requireNonEmptyTrimmed(kind, "attempt_id", r.AttemptID); err != nil {
		return err
	}
	if err := requireNonEmptyTrimmed(kind, "work_package_id", r.WorkPackageID); err != nil {
		return err
	}
	if !r.Dimension.Valid() {
		return enumError(kind, "dimension", string(r.Dimension),
			string(DimensionCorrectness), string(DimensionArchitecture), string(DimensionInvariants),
			string(DimensionSecurity), string(DimensionTestAdequacy), string(DimensionConcurrency),
			string(DimensionPerformance), string(DimensionMaintainability), string(DimensionOther))
	}
	if err := requireNonEmptyTrimmed(kind, "invocation_id", r.InvocationID); err != nil {
		return err
	}
	if err := requireNonEmptyTrimmed(kind, "provenance_id", r.ProvenanceID); err != nil {
		return err
	}
	if r.ProvenanceID != r.ReviewID {
		return errs.New(errs.CategoryInvalidArgument, "%s: provenance_id must equal review_id (got %q != %q)", kind, r.ProvenanceID, r.ReviewID)
	}
	if err := requireNonEmptyTrimmed(kind, "candidate_commit", r.CandidateCommit); err != nil {
		return err
	}
	if r.InvocationNumber != 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: invocation_number must be 1, got %d", kind, r.InvocationNumber)
	}
	if !r.Outcome.Valid() {
		return enumError(kind, "outcome", string(r.Outcome),
			string(OutcomeCompleted), string(OutcomeOutputInvalid), string(OutcomeLimitReached),
			string(OutcomeDriverError), string(OutcomeCancelled))
	}
	if r.StartedAt.IsZero() {
		return errs.New(errs.CategoryInvalidArgument, "%s: started_at is required", kind)
	}
	if r.EndedAt.IsZero() {
		return errs.New(errs.CategoryInvalidArgument, "%s: ended_at is required", kind)
	}
	if r.EndedAt.Before(r.StartedAt) {
		return errs.New(errs.CategoryInvalidArgument, "%s: ended_at must be >= started_at", kind)
	}
	if r.UsageArtifactDigest != "" {
		if err := validateSHA256Digest(kind, "usage_artifact_digest", r.UsageArtifactDigest); err != nil {
			return err
		}
	}
	return nil
}
