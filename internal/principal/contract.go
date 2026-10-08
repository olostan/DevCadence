// Package principal defines the host-neutral wire identities a Principal
// interface uses to name project state, Work Packages, candidates, operations
// and failures (WP-M5-1).
//
// The package is deliberately pure: it imports only the standard library and
// internal/errs, performs no I/O and holds no authority. An identity here
// never embeds host or provider authority (I1). Caller identity and grants
// live in CallerContext, which is an internal trusted object that can never be
// decoded from, or encoded to, a wire document (I5).
//
// The principal schema family version is independent of the durable protocol
// versions of internal/protocol.
package principal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/olostan/DevCadence/internal/errs"
)

// SchemaVersion is the only principal wire schema version this build reads.
const SchemaVersion = "1.0"

// MaxIDBytes bounds every identifier, in UTF-8 bytes.
const MaxIDBytes = 128

// MaxMessageBytes bounds a SemanticError message in UTF-8 bytes. Messages are
// fixed per code, so the bound is a defence, not a budget.
const MaxMessageBytes = 512

// EmptyStateRevision is the identity of the empty journal prefix. It is the
// only expected revision an initialisation may name.
const EmptyStateRevision = "ps_000000000"

// CallMeta is the envelope of every principal call.
type CallMeta struct {
	SchemaVersion string `json:"schema_version"`
	ProjectID     string `json:"project_id"`
	// ExpectedStateRevision is required by mutation wrappers and forbidden for
	// bootstrap initialisation. It is omitted from the wire when unasserted.
	ExpectedStateRevision string `json:"expected_state_revision,omitempty"`
	// CorrelationID correlates logs and events. It is not an idempotency promise.
	CorrelationID string `json:"correlation_id"`
}

// WorkPackageRef names one exact Work Package version.
type WorkPackageRef struct {
	ID         string `json:"id"`
	Version    int    `json:"version"`
	Digest     string `json:"digest"`
	BaseCommit string `json:"base_commit"`
}

// CandidateRef names one candidate commit of one attempt.
type CandidateRef struct {
	TaskID      string         `json:"task_id"`
	AttemptID   string         `json:"attempt_id"`
	WorkPackage WorkPackageRef `json:"work_package"`
	Commit      string         `json:"commit"`
}

// SemanticError is the only failure shape a principal caller receives.
//
// Raw provider, shell and SQL errors are never carried: Message is the fixed
// text of Code. A SemanticError is a report, not an authority statement.
type SemanticError struct {
	Code         string   `json:"code"`
	Message      string   `json:"message"`
	EvidenceRefs []string `json:"evidence_refs"`
	Retryable    bool     `json:"retryable"`
}

// OperationRef names a process-scoped operation handle. It makes no durability
// claim: InstanceID scopes the handle to the process that issued it.
type OperationRef struct {
	ID         string `json:"id"`
	InstanceID string `json:"instance_id"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
}

// Semantic error codes. The set is closed.
const (
	CodeInvalidArgument          = "INVALID_ARGUMENT"
	CodeUnsupportedSchemaVersion = "UNSUPPORTED_SCHEMA_VERSION"
	CodeNotFound                 = "NOT_FOUND"
	CodeIntegrity                = "INTEGRITY"
	CodeStaleProjectState        = "STALE_PROJECT_STATE"
	CodeStaleWorkPackage         = "STALE_WORK_PACKAGE"
	CodePolicyDenied             = "POLICY_DENIED"
	CodeValidationFailed         = "VALIDATION_FAILED"
	CodeReviewDisagreement       = "REVIEW_DISAGREEMENT"
	CodeNeedsPrincipal           = "NEEDS_PRINCIPAL"
	CodeNeedsHuman               = "NEEDS_HUMAN"
	CodeModelUnavailable         = "MODEL_UNAVAILABLE"
	CodeContextUnfit             = "CONTEXT_UNFIT"
	CodeContradictedAssumption   = "CONTRADICTED_ASSUMPTION"
	CodeOperationLost            = "OPERATION_LOST"
	CodeCancelled                = "CANCELLED"
	CodeInternal                 = "INTERNAL"
	CodeConflict                 = "CONFLICT"
)

// Operation kinds and statuses. Both sets are closed.
const (
	KindInvestigate         = "investigate"
	KindDelegate            = "delegate"
	KindValidate            = "validate"
	KindReview              = "review"
	KindSnippet             = "snippet"
	KindReviewSpecification = "review_specification"

	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusBlocked   = "blocked"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
	StatusLost      = "lost"
)

// fixedMessages maps each error code to its single safe message.
var fixedMessages = map[string]string{
	CodeInvalidArgument:          "The request is malformed or contradictory.",
	CodeUnsupportedSchemaVersion: "The schema version is not supported.",
	CodeNotFound:                 "The requested item does not exist.",
	CodeIntegrity:                "Stored data failed an integrity check.",
	CodeStaleProjectState:        "The project state changed since the expected revision.",
	CodeStaleWorkPackage:         "The Work Package is no longer the current approved plan.",
	CodePolicyDenied:             "Policy does not permit this action.",
	CodeValidationFailed:         "Deterministic validation failed.",
	CodeReviewDisagreement:       "Independent reviews disagree.",
	CodeNeedsPrincipal:           "A principal decision is required.",
	CodeNeedsHuman:               "A human decision is required.",
	CodeModelUnavailable:         "No model is available for this work.",
	CodeContextUnfit:             "The required context does not fit the selected endpoint.",
	CodeContradictedAssumption:   "A recorded assumption was contradicted by evidence.",
	CodeOperationLost:            "The operation handle is no longer available.",
	CodeCancelled:                "The operation was cancelled.",
	CodeInternal:                 "An internal error occurred.",
	CodeConflict:                 "A conflict occurred.",
}

// ErrorCodes returns the closed error-code set in specification order.
func ErrorCodes() []string {
	return []string{
		CodeInvalidArgument, CodeUnsupportedSchemaVersion, CodeNotFound, CodeIntegrity,
		CodeStaleProjectState, CodeStaleWorkPackage, CodePolicyDenied, CodeValidationFailed,
		CodeReviewDisagreement, CodeNeedsPrincipal, CodeNeedsHuman, CodeModelUnavailable,
		CodeContextUnfit, CodeContradictedAssumption, CodeOperationLost, CodeCancelled,
		CodeInternal,
	}
}

// FixedMessage returns the safe message of an error code, and false for a code
// outside the closed set.
func FixedMessage(code string) (string, bool) {
	message, ok := fixedMessages[code]
	return message, ok
}

// OperationKinds returns the closed operation-kind set.
func OperationKinds() []string {
	return []string{KindInvestigate, KindDelegate, KindValidate, KindReview, KindSnippet, KindReviewSpecification}
}

// OperationStatuses returns the closed operation-status set.
func OperationStatuses() []string {
	return []string{StatusQueued, StatusRunning, StatusCompleted, StatusBlocked, StatusFailed, StatusCancelled, StatusLost}
}

// NewSemanticError builds a SemanticError with the fixed message of code. An
// unknown code is an Internal-programming error and yields CodeInternal, so a
// caller can never smuggle free text through the constructor.
func NewSemanticError(code string, evidenceRefs []string, retryable bool) SemanticError {
	if _, ok := fixedMessages[code]; !ok {
		code = CodeInternal
	}
	refs := append([]string{}, evidenceRefs...)
	return SemanticError{Code: code, Message: fixedMessages[code], EvidenceRefs: refs, Retryable: retryable}
}

// Error implements error with the code and fixed message only.
func (e SemanticError) Error() string { return e.Code + ": " + e.Message }

// MarshalJSON emits required arrays as [] rather than null.
func (e SemanticError) MarshalJSON() ([]byte, error) {
	type plain SemanticError
	if e.EvidenceRefs == nil {
		e.EvidenceRefs = []string{}
	}
	return json.Marshal(plain(e))
}

// ValidateID checks an identifier: nonblank, at most MaxIDBytes of valid UTF-8
// and free of control characters. Values are validated, never trimmed.
func ValidateID(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s is required", field)
	}
	if len(value) > MaxIDBytes {
		return errs.New(errs.CategoryInvalidArgument, "%s exceeds %d bytes", field, MaxIDBytes)
	}
	if !utf8.ValidString(value) {
		return errs.New(errs.CategoryInvalidArgument, "%s is not valid UTF-8", field)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return errs.New(errs.CategoryInvalidArgument, "%s contains a control character", field)
		}
	}
	return nil
}

// ValidateDigest checks `sha256:` followed by 64 lower-case hex digits.
func ValidateDigest(field, value string) error {
	hexPart, ok := strings.CutPrefix(value, "sha256:")
	if !ok || len(hexPart) != 64 || !isLowerHex(hexPart) {
		return errs.New(errs.CategoryInvalidArgument,
			"%s must be sha256: followed by 64 lower-case hex digits", field)
	}
	return nil
}

// MinCommitHexDigits and MaxCommitHexDigits bound a wire commit identifier.
// Short abbreviated ids are accepted because the control plane stores base
// commits in short form; resolving an abbreviation against a repository is the
// caller's check, and equality between stored and wire ids is by exact string.
const (
	MinCommitHexDigits = 7
	MaxCommitHexDigits = 64
)

// ValidateCommit checks a lower-case hex Git object id of 7 to 64 digits.
func ValidateCommit(field, value string) error {
	if len(value) < MinCommitHexDigits || len(value) > MaxCommitHexDigits || !isLowerHex(value) {
		return errs.New(errs.CategoryInvalidArgument,
			"%s must be a %d to %d digit lower-case hex Git object id", field, MinCommitHexDigits, MaxCommitHexDigits)
	}
	return nil
}

func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// ParseStateRevision validates the canonical project-state revision syntax,
// `ps_` followed by the journal high-watermark in at least nine decimal
// digits, and returns the high-watermark. A non-canonical spelling (for
// example surplus leading zeros) is refused: comparison is by the generated
// string and revisions are never ordered lexically.
func ParseStateRevision(value string) (int64, error) {
	digits, ok := strings.CutPrefix(value, "ps_")
	if !ok || len(digits) < 9 {
		return 0, errs.New(errs.CategoryInvalidArgument,
			"state revision must be ps_ followed by at least nine decimal digits")
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return 0, errs.New(errs.CategoryInvalidArgument,
				"state revision must be ps_ followed by at least nine decimal digits")
		}
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || FormatStateRevision(n) != value {
		return 0, errs.New(errs.CategoryInvalidArgument, "state revision is not in canonical form")
	}
	return n, nil
}

// FormatStateRevision renders the canonical revision of a journal
// high-watermark. internal/state.StateRevision must agree (tested).
func FormatStateRevision(highWatermark int64) string {
	return fmt.Sprintf("ps_%09d", highWatermark)
}

// Validate checks the envelope shared by every call. It does not decide
// whether an expected revision is required; see ValidateMutation and
// ValidateBootstrap.
func (m CallMeta) Validate() error {
	if m.SchemaVersion != SchemaVersion {
		return errs.New(errs.CategorySchemaVersionUnsupported,
			"schema_version %q is not supported; this build reads %q", m.SchemaVersion, SchemaVersion)
	}
	if err := ValidateID("project_id", m.ProjectID); err != nil {
		return err
	}
	if err := ValidateID("correlation_id", m.CorrelationID); err != nil {
		return err
	}
	if m.ExpectedStateRevision != "" {
		if _, err := ParseStateRevision(m.ExpectedStateRevision); err != nil {
			return err
		}
	}
	return nil
}

// ValidateMutation additionally requires a canonical expected revision.
func (m CallMeta) ValidateMutation() error {
	if err := m.Validate(); err != nil {
		return err
	}
	if m.ExpectedStateRevision == "" {
		return errs.New(errs.CategoryInvalidArgument, "expected_state_revision is required for a mutation")
	}
	return nil
}

// ValidateBootstrap additionally forbids an expected revision.
func (m CallMeta) ValidateBootstrap() error {
	if err := m.Validate(); err != nil {
		return err
	}
	if m.ExpectedStateRevision != "" {
		return errs.New(errs.CategoryInvalidArgument,
			"expected_state_revision is forbidden when initialising a project")
	}
	return nil
}

// Validate checks the reference.
func (r WorkPackageRef) Validate() error {
	if err := ValidateID("work_package.id", r.ID); err != nil {
		return err
	}
	if r.Version < 1 {
		return errs.New(errs.CategoryInvalidArgument, "work_package.version must be >= 1")
	}
	if err := ValidateDigest("work_package.digest", r.Digest); err != nil {
		return err
	}
	return ValidateCommit("work_package.base_commit", r.BaseCommit)
}

// Validate checks the reference.
func (r CandidateRef) Validate() error {
	if err := ValidateID("task_id", r.TaskID); err != nil {
		return err
	}
	if err := ValidateID("attempt_id", r.AttemptID); err != nil {
		return err
	}
	if err := r.WorkPackage.Validate(); err != nil {
		return err
	}
	return ValidateCommit("commit", r.Commit)
}

// Validate checks the error: a known code carrying exactly its fixed message
// and only well-formed evidence handles.
func (e SemanticError) Validate() error {
	fixed, ok := fixedMessages[e.Code]
	if !ok {
		return errs.New(errs.CategoryInvalidArgument, "error code %q is not in the closed set", e.Code)
	}
	if len(e.Message) > MaxMessageBytes || e.Message != fixed {
		return errs.New(errs.CategoryInvalidArgument,
			"error message must be the fixed message of code %s", e.Code)
	}
	if e.EvidenceRefs == nil {
		return errs.New(errs.CategoryInvalidArgument, "evidence_refs is required; use an empty array")
	}
	for _, ref := range e.EvidenceRefs {
		if err := ValidateID("evidence_refs entry", ref); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks the handle.
func (r OperationRef) Validate() error {
	if err := ValidateID("operation.id", r.ID); err != nil {
		return err
	}
	if err := ValidateID("operation.instance_id", r.InstanceID); err != nil {
		return err
	}
	if !contains(OperationKinds(), r.Kind) {
		return errs.New(errs.CategoryInvalidArgument, "operation.kind %q is not in the closed set", r.Kind)
	}
	if !contains(OperationStatuses(), r.Status) {
		return errs.New(errs.CategoryInvalidArgument, "operation.status %q is not in the closed set", r.Status)
	}
	return nil
}

func contains(set []string, value string) bool {
	for _, item := range set {
		if item == value {
			return true
		}
	}
	return false
}

// decodeStrict decodes exactly one JSON document into out: unknown keys,
// explicit nulls, trailing data and type mismatches are all refused before any
// caller callback can see a value.
func decodeStrict(document []byte, out any) error {
	var generic any
	probe := json.NewDecoder(bytes.NewReader(document))
	probe.UseNumber()
	if err := probe.Decode(&generic); err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "document is not valid JSON")
	}
	if err := requireEOF(probe); err != nil {
		return err
	}
	if err := rejectNull(generic); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "document does not match the schema")
	}
	return requireEOF(decoder)
}

// DecodeStrict decodes exactly one JSON document into out, refusing unknown
// keys, explicit nulls, trailing data and type mismatches. Principal request
// documents use it so that every wire decoder shares one definition of strict.
func DecodeStrict(document []byte, out any) error { return decodeStrict(document, out) }

func requireEOF(d *json.Decoder) error {
	if _, err := d.Token(); err != io.EOF {
		return errs.New(errs.CategoryInvalidArgument, "document has trailing data after the JSON value")
	}
	return nil
}

func rejectNull(value any) error {
	switch typed := value.(type) {
	case nil:
		return errs.New(errs.CategoryInvalidArgument,
			"null is not allowed; omit an unasserted optional field")
	case map[string]any:
		for _, member := range typed {
			if err := rejectNull(member); err != nil {
				return err
			}
		}
	case []any:
		for _, member := range typed {
			if err := rejectNull(member); err != nil {
				return err
			}
		}
	}
	return nil
}

// DecodeCallMeta strictly decodes and validates an envelope (without deciding
// mutation versus bootstrap).
func DecodeCallMeta(document []byte) (CallMeta, error) {
	var out CallMeta
	if err := decodeStrict(document, &out); err != nil {
		return CallMeta{}, err
	}
	return out, out.Validate()
}

// DecodeWorkPackageRef strictly decodes and validates a reference.
func DecodeWorkPackageRef(document []byte) (WorkPackageRef, error) {
	var out WorkPackageRef
	if err := decodeStrict(document, &out); err != nil {
		return WorkPackageRef{}, err
	}
	return out, out.Validate()
}

// DecodeCandidateRef strictly decodes and validates a reference.
func DecodeCandidateRef(document []byte) (CandidateRef, error) {
	var out CandidateRef
	if err := decodeStrict(document, &out); err != nil {
		return CandidateRef{}, err
	}
	return out, out.Validate()
}

// DecodeSemanticError strictly decodes and validates an error.
func DecodeSemanticError(document []byte) (SemanticError, error) {
	var out SemanticError
	if err := decodeStrict(document, &out); err != nil {
		return SemanticError{}, err
	}
	return out, out.Validate()
}

// DecodeOperationRef strictly decodes and validates a handle.
func DecodeOperationRef(document []byte) (OperationRef, error) {
	var out OperationRef
	if err := decodeStrict(document, &out); err != nil {
		return OperationRef{}, err
	}
	return out, out.Validate()
}
