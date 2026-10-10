package facade

import (
	"github.com/olostan/DevCadence/internal/principal/discovery"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/protocol"
)

// Envelope is the part every response shares. Exactly one of the response's
// Result and Error is set. StateRevision is the project-state prefix the call
// observed (a read) or committed (a mutation); it is omitted when the call was
// refused before any state was read, so a response never claims freshness it
// did not observe.
type Envelope struct {
	SchemaVersion string                   `json:"schema_version"`
	StateRevision string                   `json:"state_revision,omitempty"`
	EvidenceRefs  []string                 `json:"evidence_refs"`
	Error         *principal.SemanticError `json:"error,omitempty"`
}

// RepositoryObservation is the live, uncached view of the registered
// repository at the time of the call (detects parallel change made outside
// DevCadence).
type RepositoryObservation struct {
	HeadCommit     string   `json:"head_commit"`
	AcceptedCommit string   `json:"accepted_commit,omitempty"`
	Dirty          bool     `json:"dirty"`
	Drifted        bool     `json:"drifted"`
	ChangedPaths   []string `json:"changed_paths"`
	// RefreshRequired is true when state or evidence captured earlier may no
	// longer describe the repository.
	RefreshRequired bool `json:"refresh_required"`
}

// --- project_state ---

// ProjectStateRequest asks for the canonical state or a focused projection.
type ProjectStateRequest struct {
	Meta       principal.CallMeta `json:"meta"`
	Focus      string             `json:"focus,omitempty"`
	TaskID     string             `json:"task_id,omitempty"`
	AtRevision string             `json:"at_revision,omitempty"`
}

// ProjectStateResult is the focused copy of canonical state.
type ProjectStateResult struct {
	Focus          string                   `json:"focus"`
	SourceRevision string                   `json:"source_revision"`
	ProjectState   *protocol.ProjectState   `json:"project_state,omitempty"`
	Task           *TaskStatus              `json:"task,omitempty"`
	Discovery      *protocol.DiscoveryState `json:"discovery,omitempty"`
	// DiscoveryAnalysis is the read-only reconstruction of discovery from
	// durable records (focus discovery at the current revision only).
	DiscoveryAnalysis *discovery.Analysis    `json:"discovery_analysis,omitempty"`
	Repository        *RepositoryObservation `json:"repository,omitempty"`
}

// ProjectStateResponse is the project_state response.
type ProjectStateResponse struct {
	Envelope
	Result *ProjectStateResult `json:"result,omitempty"`
}

// --- investigate ---

// InvestigateRequest hands a bounded question to the scout runtime.
type InvestigateRequest struct {
	Meta       principal.CallMeta `json:"meta"`
	Question   string             `json:"question"`
	BaseCommit string             `json:"base_commit"`
	ScopePaths []string           `json:"scope_paths"`
	MaxBytes   int                `json:"max_bytes"`
}

// InvestigateResult carries the scout's evidence packet.
type InvestigateResult struct {
	EvidencePacket protocol.EvidencePacket `json:"evidence_packet"`
}

// InvestigateResponse is the investigate response.
type InvestigateResponse struct {
	Envelope
	Result *InvestigateResult `json:"result,omitempty"`
}

// --- create_work_package ---

// CreateWorkPackageRequest proposes an immutable Work Package revision.
type CreateWorkPackageRequest struct {
	Meta        principal.CallMeta              `json:"meta"`
	WorkPackage protocol.EngineeringWorkPackage `json:"work_package"`
}

// CreateWorkPackageResult names the stored proposal. Status is always
// "proposed": a proposal is neither approval nor readiness.
type CreateWorkPackageResult struct {
	TaskID      string                   `json:"task_id"`
	WorkPackage principal.WorkPackageRef `json:"work_package"`
	Status      string                   `json:"status"`
}

// CreateWorkPackageResponse is the create_work_package response.
type CreateWorkPackageResponse struct {
	Envelope
	Result *CreateWorkPackageResult `json:"result,omitempty"`
}

// --- operation-returning tools ---

// OperationResult names a process-scoped operation.
type OperationResult struct {
	Operation principal.OperationRef `json:"operation"`
}

// DelegateRequest asks the accepted runtime to start an approved Work Package.
type DelegateRequest struct {
	Meta        principal.CallMeta       `json:"meta"`
	TaskID      string                   `json:"task_id"`
	WorkPackage principal.WorkPackageRef `json:"work_package"`
}

// DelegateResponse is the delegate response.
type DelegateResponse struct {
	Envelope
	Result *OperationResult `json:"result,omitempty"`
}

// ValidateRequest asks the accepted runtime to validate a candidate.
type ValidateRequest struct {
	Meta      principal.CallMeta     `json:"meta"`
	Candidate principal.CandidateRef `json:"candidate"`
	ProfileID string                 `json:"profile_id"`
}

// ValidateResponse is the validate response.
type ValidateResponse struct {
	Envelope
	Result *OperationResult `json:"result,omitempty"`
}

// ReviewRequest asks the independent review runtime to review a candidate.
type ReviewRequest struct {
	Meta       principal.CallMeta     `json:"meta"`
	Candidate  principal.CandidateRef `json:"candidate"`
	Dimensions []string               `json:"dimensions"`
}

// ReviewResponse is the review response.
type ReviewResponse struct {
	Envelope
	Result *OperationResult `json:"result,omitempty"`
}

// --- task_status ---

// TaskStatusRequest names exactly one of a task or an operation.
type TaskStatusRequest struct {
	Meta      principal.CallMeta      `json:"meta"`
	TaskID    string                  `json:"task_id,omitempty"`
	Operation *principal.OperationRef `json:"operation,omitempty"`
}

// TaskStatus is a bounded copy of a task: no history and no logs.
type TaskStatus struct {
	TaskID          string                    `json:"task_id"`
	Alias           string                    `json:"alias"`
	State           string                    `json:"state"`
	WorkPackage     *principal.WorkPackageRef `json:"work_package,omitempty"`
	Candidate       *principal.CandidateRef   `json:"candidate,omitempty"`
	BlockReason     string                    `json:"block_reason,omitempty"`
	AttemptIDs      []string                  `json:"attempt_ids"`
	HasMoreAttempts bool                      `json:"has_more_attempts"`
}

// OperationStatus reports a process-scoped operation: handles, never logs.
type OperationStatus struct {
	Operation    principal.OperationRef   `json:"operation"`
	ResultHandle string                   `json:"result_handle,omitempty"`
	Error        *principal.SemanticError `json:"error,omitempty"`
}

// TaskStatusResult carries exactly one of Task and Operation. CandidateHandoff
// accompanies Task only when the task has a current candidate and the installed
// task runtime can inspect it (SH1-4C): it is how the owner and the Principal
// see exactly what to review and how to integrate it manually.
type TaskStatusResult struct {
	Task             *TaskStatus       `json:"task,omitempty"`
	Operation        *OperationStatus  `json:"operation,omitempty"`
	CandidateHandoff *CandidateHandoff `json:"candidate_handoff,omitempty"`
}

// TaskStatusResponse is the task_status response.
type TaskStatusResponse struct {
	Envelope
	Result *TaskStatusResult `json:"result,omitempty"`
}

// --- request_evidence ---

// RequestEvidenceRequest is the closed evidence union. Kind selects which
// other fields are required and which are forbidden.
type RequestEvidenceRequest struct {
	Meta          principal.CallMeta      `json:"meta"`
	Kind          string                  `json:"kind"`
	RecordKind    string                  `json:"record_kind,omitempty"`
	RecordID      string                  `json:"record_id,omitempty"`
	RecordVersion int                     `json:"record_version,omitempty"`
	Digest        string                  `json:"digest,omitempty"`
	BaseCommit    string                  `json:"base_commit,omitempty"`
	ScopePaths    []string                `json:"scope_paths,omitempty"`
	Query         string                  `json:"query,omitempty"`
	MaxMatches    int                     `json:"max_matches,omitempty"`
	Path          string                  `json:"path,omitempty"`
	Symbol        string                  `json:"symbol,omitempty"`
	StartLine     int                     `json:"start_line,omitempty"`
	EndLine       int                     `json:"end_line,omitempty"`
	Reason        string                  `json:"reason,omitempty"`
	HitRef        string                  `json:"hit_ref,omitempty"`
	Candidate     *principal.CandidateRef `json:"candidate,omitempty"`
	MaxBytes      int                     `json:"max_bytes"`
}

// EvidenceResult carries a bounded packet or the handle of a mediated read.
type EvidenceResult struct {
	Kind           string                   `json:"kind"`
	EvidencePacket *protocol.EvidencePacket `json:"evidence_packet,omitempty"`
	Operation      *principal.OperationRef  `json:"operation,omitempty"`
}

// RequestEvidenceResponse is the request_evidence response.
type RequestEvidenceResponse struct {
	Envelope
	Result *EvidenceResult `json:"result,omitempty"`
}

// --- accept / reject / record_decision ---

// AcceptRequest names the candidate and the evidence a future gate would use.
type AcceptRequest struct {
	Meta          principal.CallMeta     `json:"meta"`
	Candidate     principal.CandidateRef `json:"candidate"`
	ValidationIDs []string               `json:"validation_ids"`
	ReviewIDs     []string               `json:"review_ids"`
	Reason        string                 `json:"reason"`
}

// AcceptResult carries the candidate handoff packet when the installed task
// runtime can inspect the candidate. Acceptance itself is never performed: the
// response still carries the NEEDS_PRINCIPAL error, and integration is a manual
// owner action described by the packet. Without a candidate inspector there is
// no result at all.
type AcceptResult struct {
	Handoff *CandidateHandoff `json:"handoff,omitempty"`
}

// AcceptResponse is the accept response; it always carries an error.
type AcceptResponse struct {
	Envelope
	Result *AcceptResult `json:"result,omitempty"`
}

// RejectRequest sends a reviewed candidate back for repair.
type RejectRequest struct {
	Meta             principal.CallMeta     `json:"meta"`
	Candidate        principal.CandidateRef `json:"candidate"`
	Reason           string                 `json:"reason"`
	RequestedRepairs []string               `json:"requested_repairs"`
}

// RejectResult reports the committed rejection.
type RejectResult struct {
	TaskID    string `json:"task_id"`
	AttemptID string `json:"attempt_id"`
	State     string `json:"state"`
}

// RejectResponse is the reject response.
type RejectResponse struct {
	Envelope
	Result *RejectResult `json:"result,omitempty"`
}

// RecordDecisionRequest records an immutable decision.
type RecordDecisionRequest struct {
	Meta     principal.CallMeta      `json:"meta"`
	Decision protocol.DecisionRecord `json:"decision"`
}

// RecordDecisionResult names the stored decision.
type RecordDecisionResult struct {
	DecisionID string `json:"decision_id"`
	Digest     string `json:"digest"`
}

// RecordDecisionResponse is the record_decision response.
type RecordDecisionResponse struct {
	Envelope
	Result *RecordDecisionResult `json:"result,omitempty"`
}

// --- decoding ---

func decodeReq[T any](document []byte, validate func(*T) error) (T, error) {
	var out T
	if len(document) > MaxRequestBytes {
		return out, errs.New(errs.CategoryInvalidArgument, "request exceeds %d bytes", MaxRequestBytes)
	}
	if err := principal.DecodeStrict(document, &out); err != nil {
		return out, err
	}
	return out, validate(&out)
}

// DecodeProjectStateRequest strictly decodes and validates a request.
func DecodeProjectStateRequest(doc []byte) (ProjectStateRequest, error) {
	return decodeReq(doc, func(r *ProjectStateRequest) error { return r.Validate() })
}

// DecodeInvestigateRequest strictly decodes and validates a request.
func DecodeInvestigateRequest(doc []byte) (InvestigateRequest, error) {
	return decodeReq(doc, func(r *InvestigateRequest) error { return r.Validate() })
}

// DecodeCreateWorkPackageRequest strictly decodes and validates a request.
func DecodeCreateWorkPackageRequest(doc []byte) (CreateWorkPackageRequest, error) {
	return decodeReq(doc, func(r *CreateWorkPackageRequest) error { return r.Validate() })
}

// DecodeDelegateRequest strictly decodes and validates a request.
func DecodeDelegateRequest(doc []byte) (DelegateRequest, error) {
	return decodeReq(doc, func(r *DelegateRequest) error { return r.Validate() })
}

// DecodeTaskStatusRequest strictly decodes and validates a request.
func DecodeTaskStatusRequest(doc []byte) (TaskStatusRequest, error) {
	return decodeReq(doc, func(r *TaskStatusRequest) error { return r.Validate() })
}

// DecodeValidateRequest strictly decodes and validates a request.
func DecodeValidateRequest(doc []byte) (ValidateRequest, error) {
	return decodeReq(doc, func(r *ValidateRequest) error { return r.Validate() })
}

// DecodeReviewRequest strictly decodes and validates a request.
func DecodeReviewRequest(doc []byte) (ReviewRequest, error) {
	return decodeReq(doc, func(r *ReviewRequest) error { return r.Validate() })
}

// DecodeRequestEvidenceRequest strictly decodes and validates a request.
func DecodeRequestEvidenceRequest(doc []byte) (RequestEvidenceRequest, error) {
	return decodeReq(doc, func(r *RequestEvidenceRequest) error { return r.Validate() })
}

// DecodeAcceptRequest strictly decodes and validates a request.
func DecodeAcceptRequest(doc []byte) (AcceptRequest, error) {
	return decodeReq(doc, func(r *AcceptRequest) error { return r.Validate() })
}

// DecodeRejectRequest strictly decodes and validates a request.
func DecodeRejectRequest(doc []byte) (RejectRequest, error) {
	return decodeReq(doc, func(r *RejectRequest) error { return r.Validate() })
}

// DecodeRecordDecisionRequest strictly decodes and validates a request.
func DecodeRecordDecisionRequest(doc []byte) (RecordDecisionRequest, error) {
	return decodeReq(doc, func(r *RecordDecisionRequest) error { return r.Validate() })
}

// --- validation ---

func invalid(format string, args ...any) error {
	return errs.New(errs.CategoryInvalidArgument, format, args...)
}

// validateText checks nonblank prose of at most max bytes without NUL or
// control characters other than line breaks and tabs.
func validateText(field, value string, max int) error {
	if strings.TrimSpace(value) == "" {
		return invalid("%s is required", field)
	}
	if len(value) > max {
		return invalid("%s exceeds %d bytes", field, max)
	}
	if !utf8.ValidString(value) {
		return invalid("%s is not valid UTF-8", field)
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return invalid("%s contains a control character", field)
		}
	}
	return nil
}

// validateRelPath rejects absolute, drive, UNC, backslash, dot-dot, control and
// non-clean paths. Symlink escape needs the filesystem and is the worker's
// canonical-resolution check. "." is a bounded search prefix only.
func validateRelPath(field, p string, allowRoot bool) error {
	if p == "" || len(p) > 1024 || !utf8.ValidString(p) {
		return invalid("%s must be a nonempty relative path of at most 1024 bytes", field)
	}
	for _, r := range p {
		if unicode.IsControl(r) {
			return invalid("%s contains a control character", field)
		}
	}
	if strings.HasPrefix(p, "/") || strings.Contains(p, `\`) || (len(p) >= 2 && p[1] == ':') {
		return invalid("%s must be a relative slash-separated path", field)
	}
	if p == "." {
		if allowRoot {
			return nil
		}
		return invalid("%s must name a file, not the repository root", field)
	}
	trimmed := strings.TrimSuffix(p, "/")
	for _, seg := range strings.Split(trimmed, "/") {
		if seg == ".." {
			return invalid("%s must not contain '..'", field)
		}
	}
	if trimmed == "" || path.Clean(trimmed) != trimmed {
		return invalid("%s must be a clean relative path", field)
	}
	return nil
}

func validateScopePaths(field string, paths []string) error {
	if len(paths) < 1 || len(paths) > MaxScopePaths {
		return invalid("%s needs 1 to %d entries", field, MaxScopePaths)
	}
	seen := map[string]bool{}
	for _, p := range paths {
		if err := validateRelPath(field+" entry", p, true); err != nil {
			return err
		}
		if seen[p] {
			return invalid("%s entry %q is repeated", field, p)
		}
		seen[p] = true
	}
	return nil
}

func validateMaxBytes(field string, v int) error {
	if v < 1 || v > MaxEvidenceBytes {
		return invalid("%s must be 1 to %d", field, MaxEvidenceBytes)
	}
	return nil
}

func validateUniqueIDs(field string, ids []string, min, max int) error {
	if len(ids) < min || len(ids) > max {
		return invalid("%s needs %d to %d entries", field, min, max)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if err := principal.ValidateID(field+" entry", id); err != nil {
			return err
		}
		if seen[id] {
			return invalid("%s entry %q is repeated", field, id)
		}
		seen[id] = true
	}
	return nil
}

// Validate checks the request. Authorization is separate.
func (r ProjectStateRequest) Validate() error {
	if err := r.Meta.Validate(); err != nil {
		return err
	}
	switch r.Focus {
	case "", FocusProject, FocusDiscovery:
		if r.TaskID != "" {
			return invalid("task_id is allowed only with focus task")
		}
	case FocusTask:
		if err := principal.ValidateID("task_id", r.TaskID); err != nil {
			return err
		}
	default:
		return invalid("focus %q is not one of project, task, discovery", r.Focus)
	}
	if r.AtRevision != "" {
		if _, err := principal.ParseStateRevision(r.AtRevision); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks the request.
func (r InvestigateRequest) Validate() error {
	if err := r.Meta.Validate(); err != nil {
		return err
	}
	if err := validateText("question", r.Question, MaxQuestionBytes); err != nil {
		return err
	}
	if err := principal.ValidateCommit("base_commit", r.BaseCommit); err != nil {
		return err
	}
	if err := validateScopePaths("scope_paths", r.ScopePaths); err != nil {
		return err
	}
	return validateMaxBytes("max_bytes", r.MaxBytes)
}

// Validate checks the request: the Work Package must be a valid record; its
// identity, project and planning revision are checked against the call and the
// stored state by the facade.
func (r CreateWorkPackageRequest) Validate() error {
	if err := r.Meta.ValidateMutation(); err != nil {
		return err
	}
	if err := r.WorkPackage.Validate(); err != nil {
		return err
	}
	return principal.ValidateCommit("work_package.base_commit", r.WorkPackage.BaseCommit)
}

// Validate checks the request.
func (r DelegateRequest) Validate() error {
	if err := r.Meta.ValidateMutation(); err != nil {
		return err
	}
	if err := principal.ValidateID("task_id", r.TaskID); err != nil {
		return err
	}
	return r.WorkPackage.Validate()
}

// Validate checks the request: exactly one of task_id and operation.
func (r TaskStatusRequest) Validate() error {
	if err := r.Meta.Validate(); err != nil {
		return err
	}
	if (r.TaskID == "") == (r.Operation == nil) {
		return invalid("exactly one of task_id and operation is required")
	}
	if r.Operation != nil {
		return r.Operation.Validate()
	}
	return principal.ValidateID("task_id", r.TaskID)
}

// Validate checks the request.
func (r ValidateRequest) Validate() error {
	if err := r.Meta.Validate(); err != nil {
		return err
	}
	if err := r.Candidate.Validate(); err != nil {
		return err
	}
	return principal.ValidateID("profile_id", r.ProfileID)
}

// Validate checks the request.
func (r ReviewRequest) Validate() error {
	if err := r.Meta.Validate(); err != nil {
		return err
	}
	if err := r.Candidate.Validate(); err != nil {
		return err
	}
	if len(r.Dimensions) < 1 || len(r.Dimensions) > 8 {
		return invalid("dimensions needs 1 to 8 entries")
	}
	seen := map[string]bool{}
	for _, d := range r.Dimensions {
		known := false
		for _, k := range reviewDimensions() {
			known = known || k == d
		}
		if !known {
			return invalid("dimension %q is not in the closed set", d)
		}
		if seen[d] {
			return invalid("dimension %q is repeated", d)
		}
		seen[d] = true
	}
	return nil
}

// presentFields lists the kind-specific fields that carry a value. Every such
// field has a nonzero minimum, so a zero value is "absent".
func (r RequestEvidenceRequest) presentFields() map[string]bool {
	set := map[string]bool{}
	mark := func(name string, present bool) {
		if present {
			set[name] = true
		}
	}
	mark("record_kind", r.RecordKind != "")
	mark("record_id", r.RecordID != "")
	mark("record_version", r.RecordVersion != 0)
	mark("digest", r.Digest != "")
	mark("base_commit", r.BaseCommit != "")
	mark("scope_paths", r.ScopePaths != nil)
	mark("query", r.Query != "")
	mark("max_matches", r.MaxMatches != 0)
	mark("path", r.Path != "")
	mark("symbol", r.Symbol != "")
	mark("start_line", r.StartLine != 0)
	mark("end_line", r.EndLine != 0)
	mark("reason", r.Reason != "")
	mark("hit_ref", r.HitRef != "")
	mark("candidate", r.Candidate != nil)
	return set
}

var evidenceAllowed = map[string][]string{
	EvidenceSummary: {"record_kind", "record_id", "record_version", "digest"},
	EvidenceSearch:  {"base_commit", "scope_paths", "query", "max_matches"},
	EvidenceSymbol:  {"base_commit", "path", "symbol"},
	EvidenceSnippet: {"base_commit", "path", "start_line", "end_line", "reason", "hit_ref"},
	EvidenceDiff:    {"candidate", "path", "start_line", "end_line", "reason"},
}

// SummaryRecordKinds are the record kinds a summary may name.
func SummaryRecordKinds() []string {
	return []string{"EvidencePacket", "ValidationResult", "ReviewResult", "DecisionRecord"}
}

// Validate checks the request against its kind.
func (r RequestEvidenceRequest) Validate() error {
	if err := r.Meta.Validate(); err != nil {
		return err
	}
	allowed, ok := evidenceAllowed[r.Kind]
	if !ok {
		return invalid("kind %q is not in the closed evidence union", r.Kind)
	}
	permitted := map[string]bool{}
	for _, f := range allowed {
		permitted[f] = true
	}
	for f := range r.presentFields() {
		if !permitted[f] {
			return invalid("field %s is not allowed for kind %s", f, r.Kind)
		}
	}
	if err := validateMaxBytes("max_bytes", r.MaxBytes); err != nil {
		return err
	}
	switch r.Kind {
	case EvidenceSummary:
		known := false
		for _, k := range SummaryRecordKinds() {
			known = known || k == r.RecordKind
		}
		if !known {
			return invalid("record_kind %q cannot be summarised", r.RecordKind)
		}
		if err := principal.ValidateID("record_id", r.RecordID); err != nil {
			return err
		}
		if r.RecordVersion < 1 {
			return invalid("record_version must be >= 1")
		}
		return principal.ValidateDigest("digest", r.Digest)
	case EvidenceSearch:
		if err := principal.ValidateCommit("base_commit", r.BaseCommit); err != nil {
			return err
		}
		if err := validateScopePaths("scope_paths", r.ScopePaths); err != nil {
			return err
		}
		if err := validateText("query", r.Query, MaxQueryBytes); err != nil {
			return err
		}
		if r.MaxMatches < 1 || r.MaxMatches > MaxMatches {
			return invalid("max_matches must be 1 to %d", MaxMatches)
		}
	case EvidenceSymbol:
		if err := principal.ValidateCommit("base_commit", r.BaseCommit); err != nil {
			return err
		}
		if err := validateRelPath("path", r.Path, false); err != nil {
			return err
		}
		return validateText("symbol", r.Symbol, MaxQueryBytes)
	case EvidenceSnippet:
		if err := principal.ValidateCommit("base_commit", r.BaseCommit); err != nil {
			return err
		}
		if err := validateRelPath("path", r.Path, false); err != nil {
			return err
		}
		if err := validateLines(r.StartLine, r.EndLine); err != nil {
			return err
		}
		if err := validateText("reason", r.Reason, MaxHitReasonBytes); err != nil {
			return err
		}
		if r.HitRef != "" {
			return principal.ValidateID("hit_ref", r.HitRef)
		}
	case EvidenceDiff:
		if r.Candidate == nil {
			return invalid("candidate is required for kind diff")
		}
		if err := r.Candidate.Validate(); err != nil {
			return err
		}
		if err := validateRelPath("path", r.Path, false); err != nil {
			return err
		}
		if err := validateLines(r.StartLine, r.EndLine); err != nil {
			return err
		}
		return validateText("reason", r.Reason, MaxHitReasonBytes)
	}
	return nil
}

func validateLines(start, end int) error {
	if start < 1 || end < start || end-start+1 > MaxSnippetLines {
		return invalid("start_line and end_line must be 1-based, ordered and span at most %d lines", MaxSnippetLines)
	}
	return nil
}

// Validate checks the request.
func (r AcceptRequest) Validate() error {
	if err := r.Meta.Validate(); err != nil {
		return err
	}
	if err := r.Candidate.Validate(); err != nil {
		return err
	}
	if err := validateUniqueIDs("validation_ids", r.ValidationIDs, 1, 16); err != nil {
		return err
	}
	if err := validateUniqueIDs("review_ids", r.ReviewIDs, 1, 16); err != nil {
		return err
	}
	return validateText("reason", r.Reason, MaxReasonBytes)
}

// Validate checks the request.
func (r RejectRequest) Validate() error {
	if err := r.Meta.ValidateMutation(); err != nil {
		return err
	}
	if err := r.Candidate.Validate(); err != nil {
		return err
	}
	if err := validateText("reason", r.Reason, MaxReasonBytes); err != nil {
		return err
	}
	if len(r.RequestedRepairs) > MaxRepairs {
		return invalid("requested_repairs has at most %d entries", MaxRepairs)
	}
	for _, repair := range r.RequestedRepairs {
		if err := validateText("requested_repairs entry", repair, MaxReasonBytes); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks the request: the decision must be a valid record.
func (r RecordDecisionRequest) Validate() error {
	if err := r.Meta.ValidateMutation(); err != nil {
		return err
	}
	return r.Decision.Validate()
}

// --- candidate handoff (SH1-4C) ---

// CandidateHandoff is the bounded, reproducible description of one candidate
// that a human (or the Principal) needs to inspect and integrate it manually.
// It is evidence, never authority: nothing here accepts, merges or pushes.
type CandidateHandoff struct {
	TaskID          string `json:"task_id"`
	AttemptID       string `json:"attempt_id"`
	BaseCommit      string `json:"base_commit"`
	CandidateCommit string `json:"candidate_commit"`
	// Ref is a durable ref in the primary repository (refs/devcadence/candidates/...)
	// pinning the candidate commit; creating it moves no branch and not HEAD.
	Ref string `json:"ref"`
	// Branch and WorktreePath locate the attempt's isolated worktree (may be removed later).
	Branch       string        `json:"branch,omitempty"`
	WorktreePath string        `json:"worktree_path,omitempty"`
	ChangedFiles []ChangedFile `json:"changed_files"`
	Model        HandoffModel  `json:"model"`
	// ExecutionMode is "strict" or "unsafe_unconfined_local".
	ExecutionMode string         `json:"execution_mode,omitempty"`
	Review        HandoffReview  `json:"review"`
	PostCheck     *HandoffChecks `json:"post_check,omitempty"`
	// Validations are the results of separate validate calls on this attempt.
	Validations  []HandoffValidation `json:"validations"`
	CommandTrace *HandoffCommands    `json:"command_trace,omitempty"`
	Inspect      HandoffInspect      `json:"inspect"`
	// Acceptance states plainly who may accept and that nothing was accepted.
	Acceptance string `json:"acceptance"`
}

// ChangedFile is one entry of the candidate's changed-file manifest.
type ChangedFile struct {
	Status string `json:"status"`
	Path   string `json:"path"`
}

// HandoffModel is the model identity recorded for the attempt.
type HandoffModel struct {
	EndpointID string `json:"endpoint_id,omitempty"`
	Model      string `json:"model,omitempty"`
	Digest     string `json:"digest,omitempty"`
}

// HandoffReview is the honest review label. Independent is false when no
// independent reviewer examined the candidate.
type HandoffReview struct {
	Status      string `json:"status"`
	Independent bool   `json:"independent"`
	Reason      string `json:"reason,omitempty"`
}

// HandoffChecks summarizes the post-check (validation-report artifact).
type HandoffChecks struct {
	ProfileID       string         `json:"profile_id"`
	Passed          bool           `json:"passed"`
	RoundsUsed      int            `json:"rounds_used"`
	RepairRounds    int            `json:"repair_rounds"`
	MaxRepairRounds int            `json:"max_repair_rounds"`
	Rounds          []HandoffRound `json:"rounds"`
}

// HandoffRound is one validation round of the repair history.
type HandoffRound struct {
	Round  int            `json:"round"`
	Passed bool           `json:"passed"`
	Checks []HandoffCheck `json:"checks"`
}

// HandoffCheck is one check of a round; output stays in the artifact.
type HandoffCheck struct {
	ID       string   `json:"id"`
	Argv     []string `json:"argv,omitempty"`
	Status   string   `json:"status"`
	ExitCode *int     `json:"exit_code,omitempty"`
}

// HandoffValidation is the outcome of a validate call on the attempt.
type HandoffValidation struct {
	ValidationID string `json:"validation_id"`
	Outcome      string `json:"outcome"`
}

// HandoffCommands summarizes the run_command audit trace.
type HandoffCommands struct {
	Commands         int  `json:"commands"`
	Refused          int  `json:"refused"`
	UnsafeUnconfined bool `json:"unsafe_unconfined"`
}

// HandoffInspect holds the exact commands to reproduce the inspection and
// to integrate manually. They are text for the owner to run; DevCadence runs none.
type HandoffInspect struct {
	Diff       string `json:"diff"`
	Log        string `json:"log"`
	Merge      string `json:"merge"`
	CherryPick string `json:"cherry_pick"`
}
