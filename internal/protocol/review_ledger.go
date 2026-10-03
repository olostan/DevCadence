package protocol

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
)

// ProvenanceRole enumerates the per-position roles for ActorProvenance.
type ProvenanceRole string

const (
	ProvenanceRoleReviewer    ProvenanceRole = "reviewer"
	ProvenanceRoleImplementer ProvenanceRole = "implementer"
	ProvenanceRoleVerifier    ProvenanceRole = "verifier"
)

// Valid reports whether the provenance role is known.
func (r ProvenanceRole) Valid() bool {
	switch r {
	case ProvenanceRoleReviewer, ProvenanceRoleImplementer, ProvenanceRoleVerifier:
		return true
	}
	return false
}

// ActorProvenance captures durable actor identity and invocation context
// for review-ledger records (EWP WP-M3C-5 D-1, PROTOCOLS §19).
type ActorProvenance struct {
	ActorID         string         `json:"actor_id"`
	InvocationID    string         `json:"invocation_id"`
	Role            ProvenanceRole `json:"role"`
	LineageActorIDs []string       `json:"lineage_actor_ids,omitempty"`
	EndpointRef     *string        `json:"endpoint_ref,omitempty"`
	SessionRef      *string        `json:"session_ref,omitempty"`
	ModelRef        *string        `json:"model_ref,omitempty"`
}

// Validate checks internal consistency and ensures role matches expected position.
func (a ActorProvenance) Validate(kind string, expectedRole ProvenanceRole) error {
	if err := requireNonEmptyTrimmed(kind, "actor_id", a.ActorID); err != nil {
		return err
	}
	if err := requireNonEmptyTrimmed(kind, "invocation_id", a.InvocationID); err != nil {
		return err
	}
	if !a.Role.Valid() {
		return enumError(kind, "role", string(a.Role), string(ProvenanceRoleReviewer), string(ProvenanceRoleImplementer), string(ProvenanceRoleVerifier))
	}
	if a.Role != expectedRole {
		return errs.New(errs.CategoryInvalidArgument, "%s: role must be %q, got %q", kind, expectedRole, a.Role)
	}
	if len(a.LineageActorIDs) > 0 {
		seen := make(map[string]bool, len(a.LineageActorIDs))
		for _, id := range a.LineageActorIDs {
			if err := requireNonEmptyTrimmed(kind, "lineage_actor_ids item", id); err != nil {
				return err
			}
			if id == a.ActorID {
				return errs.New(errs.CategoryInvalidArgument, "%s: lineage_actor_ids contains own actor_id %q", kind, id)
			}
			if seen[id] {
				return errs.New(errs.CategoryInvalidArgument, "%s: duplicate entry %q in lineage_actor_ids", kind, id)
			}
			seen[id] = true
		}
	}
	return nil
}

// ObservationRef points to a raw ReviewResult finding observation.
type ObservationRef struct {
	ReviewID     string `json:"review_id"`
	FindingIndex int    `json:"finding_index"`
}

// Validate checks the observation reference fields.
func (o ObservationRef) Validate(kind string) error {
	if err := requireNonEmptyTrimmed(kind, "review_id", o.ReviewID); err != nil {
		return err
	}
	if o.FindingIndex < 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: finding_index must be >= 0, got %d", kind, o.FindingIndex)
	}
	return nil
}

// Materiality grades the significance of a finding for the current candidate/campaign.
// It accepts the exact vocabulary from finding-disposition.schema.json (REQ-07).
type Materiality string

const (
	MaterialityBlocking            Materiality = "blocking"
	MaterialityMaterialNonBlocking Materiality = "material_non_blocking"
	MaterialityOpportunistic       Materiality = "opportunistic"
)

// Valid reports whether the materiality is one of the allowed values.
func (m Materiality) Valid() bool {
	switch m {
	case MaterialityBlocking, MaterialityMaterialNonBlocking, MaterialityOpportunistic:
		return true
	}
	return false
}

// FindingConfidence represents the optional epistemic confidence of a finding.
type FindingConfidence string

const (
	ConfidenceHigh   FindingConfidence = "high"
	ConfidenceMedium FindingConfidence = "medium"
	ConfidenceLow    FindingConfidence = "low"
)

// Valid reports whether the confidence is one of the allowed values.
func (c FindingConfidence) Valid() bool {
	switch c {
	case ConfidenceHigh, ConfidenceMedium, ConfidenceLow:
		return true
	}
	return false
}

// ResolutionKind distinguishes a repair attempt from a challenge.
type ResolutionKind string

const (
	ResolutionKindFixAttempted ResolutionKind = "fix_attempted"
	ResolutionKindChallenge    ResolutionKind = "challenge"
)

// Valid reports whether the resolution kind is known.
func (k ResolutionKind) Valid() bool {
	switch k {
	case ResolutionKindFixAttempted, ResolutionKindChallenge:
		return true
	}
	return false
}

// VerificationOutcome captures an independent verifier's judgment over a resolution.
type VerificationOutcome string

const (
	OutcomeVerifiedFixed          VerificationOutcome = "verified_fixed"
	OutcomeVerifiedDismissed      VerificationOutcome = "verified_dismissed"
	OutcomeNotResolved            VerificationOutcome = "not_resolved"
	OutcomeReAdjudicationRequired VerificationOutcome = "re_adjudication_required"
)

// Valid reports whether the verification outcome is known.
func (o VerificationOutcome) Valid() bool {
	switch o {
	case OutcomeVerifiedFixed, OutcomeVerifiedDismissed, OutcomeNotResolved, OutcomeReAdjudicationRequired:
		return true
	}
	return false
}

// FindingResolutionState is the derived lifecycle state of a finding (EWP §5, D-2).
type FindingResolutionState string

const (
	StateUnresolved             FindingResolutionState = "unresolved"
	StateVerificationPending    FindingResolutionState = "verification_pending"
	StateVerifiedFixed          FindingResolutionState = "verified_fixed"
	StateVerifiedDismissed      FindingResolutionState = "verified_dismissed"
	StateReAdjudicationRequired FindingResolutionState = "re_adjudication_required"
)

// Valid reports whether the derived state is known.
func (s FindingResolutionState) Valid() bool {
	switch s {
	case StateUnresolved, StateVerificationPending, StateVerifiedFixed, StateVerifiedDismissed, StateReAdjudicationRequired:
		return true
	}
	return false
}

// ReviewFinding is a normalized, durable material claim (EWP §4B, REQ-01).
// It is immutable and carries no stored status; lifecycle state is derived (D-2).
type ReviewFinding struct {
	SchemaVersion      SchemaVersion      `json:"schema_version"`
	FindingID          string             `json:"finding_id"`
	ProjectID          string             `json:"project_id"`
	CampaignID         string             `json:"campaign_id"`
	CandidateCommit    string             `json:"candidate_commit"`
	WorkPackageID      string             `json:"work_package_id"`
	ContractRevision   int                `json:"contract_revision"`
	Severity           Severity           `json:"severity"`
	Materiality        Materiality        `json:"materiality"`
	Confidence         *FindingConfidence `json:"confidence,omitempty"`
	Claim              string             `json:"claim"`
	Impact             string             `json:"impact"`
	VerificationMethod string             `json:"verification_method"`
	WhyNow             *string            `json:"why_now,omitempty"`
	EvidenceRefs       []string           `json:"evidence_refs"`
	RequirementRefs    []string           `json:"requirement_refs,omitempty"`
	SourceObservations []ObservationRef   `json:"source_observations"`
	Reviewer           ActorProvenance    `json:"reviewer"`
	RecordedAt         string             `json:"recorded_at"`
}

// RecordKind implements Record.
func (f *ReviewFinding) RecordKind() string { return "ReviewFinding" }

// RecordID implements Record.
func (f *ReviewFinding) RecordID() string { return f.FindingID }

// SchemaVer implements Record.
func (f *ReviewFinding) SchemaVer() SchemaVersion { return f.SchemaVersion }

// ProjectOf implements ProjectScoped.
func (f *ReviewFinding) ProjectOf() string { return f.ProjectID }

// Validate checks semantic and schema constraints.
func (f *ReviewFinding) Validate() error {
	const kind = "ReviewFinding"
	if err := f.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	for field, val := range map[string]string{
		"finding_id":          f.FindingID,
		"project_id":          f.ProjectID,
		"campaign_id":         f.CampaignID,
		"candidate_commit":    f.CandidateCommit,
		"work_package_id":     f.WorkPackageID,
		"claim":               f.Claim,
		"impact":              f.Impact,
		"verification_method": f.VerificationMethod,
	} {
		if err := requireNonEmptyTrimmed(kind, field, val); err != nil {
			return err
		}
	}
	if f.ContractRevision < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: contract_revision must be >= 1, got %d", kind, f.ContractRevision)
	}
	if !f.Severity.ValidFinding() {
		return enumError(kind, "severity", string(f.Severity),
			string(SeverityInfo), string(SeverityLow), string(SeverityMedium), string(SeverityHigh), string(SeverityCritical))
	}
	if !f.Materiality.Valid() {
		return enumError(kind, "materiality", string(f.Materiality),
			string(MaterialityBlocking), string(MaterialityMaterialNonBlocking), string(MaterialityOpportunistic))
	}
	if f.Confidence != nil && !f.Confidence.Valid() {
		return enumError(kind, "confidence", string(*f.Confidence),
			string(ConfidenceHigh), string(ConfidenceMedium), string(ConfidenceLow))
	}
	if f.WhyNow != nil {
		if err := requireNonEmptyTrimmed(kind, "why_now", *f.WhyNow); err != nil {
			return err
		}
	}
	if err := requireMinItems(kind, "evidence_refs", len(f.EvidenceRefs), 1); err != nil {
		return err
	}
	for _, ref := range f.EvidenceRefs {
		if err := requireNonEmptyTrimmed(kind, "evidence_refs item", ref); err != nil {
			return err
		}
	}
	for _, req := range f.RequirementRefs {
		if err := requireNonEmptyTrimmed(kind, "requirement_refs item", req); err != nil {
			return err
		}
	}
	if err := requireMinItems(kind, "source_observations", len(f.SourceObservations), 1); err != nil {
		return err
	}
	for _, obs := range f.SourceObservations {
		if err := obs.Validate(kind); err != nil {
			return err
		}
	}
	if err := f.Reviewer.Validate(kind+".reviewer", ProvenanceRoleReviewer); err != nil {
		return err
	}
	return parseRFC3339(kind, "recorded_at", f.RecordedAt)
}

// MarshalJSON guarantees required arrays serialize as [], never null.
func (f ReviewFinding) MarshalJSON() ([]byte, error) {
	type alias ReviewFinding
	out := alias(f)
	out.EvidenceRefs = orEmpty(out.EvidenceRefs)
	out.SourceObservations = orEmpty(out.SourceObservations)
	return json.Marshal(out)
}

// FindingResolution is an implementer's response to an accepted finding (EWP §4C, REQ-02).
type FindingResolution struct {
	SchemaVersion           SchemaVersion   `json:"schema_version"`
	ResolutionID            string          `json:"resolution_id"`
	ProjectID               string          `json:"project_id"`
	CampaignID              string          `json:"campaign_id"`
	FindingID               string          `json:"finding_id"`
	DispositionID           string          `json:"disposition_id"`
	ContractRevision        int             `json:"contract_revision"`
	AttemptNo               int             `json:"attempt_no"`
	Kind                    ResolutionKind  `json:"kind"`
	TargetCandidateCommit   string          `json:"target_candidate_commit"`
	ResolvedCandidateCommit *string         `json:"resolved_candidate_commit,omitempty"`
	Rationale               string          `json:"rationale"`
	EvidenceRefs            []string        `json:"evidence_refs"`
	Producer                ActorProvenance `json:"producer"`
	RecordedAt              string          `json:"recorded_at"`
}

// RecordKind implements Record.
func (r *FindingResolution) RecordKind() string { return "FindingResolution" }

// RecordID implements Record.
func (r *FindingResolution) RecordID() string { return r.ResolutionID }

// SchemaVer implements Record.
func (r *FindingResolution) SchemaVer() SchemaVersion { return r.SchemaVersion }

// ProjectOf implements ProjectScoped.
func (r *FindingResolution) ProjectOf() string { return r.ProjectID }

// Validate checks semantic and schema constraints.
func (r *FindingResolution) Validate() error {
	const kind = "FindingResolution"
	if err := r.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	for field, val := range map[string]string{
		"resolution_id":           r.ResolutionID,
		"project_id":              r.ProjectID,
		"campaign_id":             r.CampaignID,
		"finding_id":              r.FindingID,
		"disposition_id":          r.DispositionID,
		"target_candidate_commit": r.TargetCandidateCommit,
		"rationale":               r.Rationale,
	} {
		if err := requireNonEmptyTrimmed(kind, field, val); err != nil {
			return err
		}
	}
	if r.ContractRevision < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: contract_revision must be >= 1, got %d", kind, r.ContractRevision)
	}
	if r.AttemptNo < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: attempt_no must be >= 1, got %d", kind, r.AttemptNo)
	}
	if !r.Kind.Valid() {
		return enumError(kind, "kind", string(r.Kind), string(ResolutionKindFixAttempted), string(ResolutionKindChallenge))
	}
	switch r.Kind {
	case ResolutionKindFixAttempted:
		if r.ResolvedCandidateCommit == nil || strings.TrimSpace(*r.ResolvedCandidateCommit) == "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: resolved_candidate_commit is required for fix_attempted", kind)
		}
	case ResolutionKindChallenge:
		if r.ResolvedCandidateCommit != nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: resolved_candidate_commit must be absent or null for challenge", kind)
		}
	}
	if err := requireMinItems(kind, "evidence_refs", len(r.EvidenceRefs), 1); err != nil {
		return err
	}
	for _, ref := range r.EvidenceRefs {
		if err := requireNonEmptyTrimmed(kind, "evidence_refs item", ref); err != nil {
			return err
		}
	}
	if err := r.Producer.Validate(kind+".producer", ProvenanceRoleImplementer); err != nil {
		return err
	}
	return parseRFC3339(kind, "recorded_at", r.RecordedAt)
}

// MarshalJSON guarantees required arrays serialize as [], never null.
func (r FindingResolution) MarshalJSON() ([]byte, error) {
	type alias FindingResolution
	out := alias(r)
	out.EvidenceRefs = orEmpty(out.EvidenceRefs)
	return json.Marshal(out)
}

// ResolutionVerification records an independent verifier's judgment (EWP §4D, REQ-03).
type ResolutionVerification struct {
	SchemaVersion           SchemaVersion       `json:"schema_version"`
	VerificationID          string              `json:"verification_id"`
	ProjectID               string              `json:"project_id"`
	CampaignID              string              `json:"campaign_id"`
	FindingID               string              `json:"finding_id"`
	ResolutionID            string              `json:"resolution_id"`
	ContractRevision        int                 `json:"contract_revision"`
	VerifiedCandidateCommit string              `json:"verified_candidate_commit"`
	Outcome                 VerificationOutcome `json:"outcome"`
	Rationale               string              `json:"rationale"`
	EvidenceRefs            []string            `json:"evidence_refs"`
	Verifier                ActorProvenance     `json:"verifier"`
	RecordedAt              string              `json:"recorded_at"`
}

// RecordKind implements Record.
func (v *ResolutionVerification) RecordKind() string { return "ResolutionVerification" }

// RecordID implements Record.
func (v *ResolutionVerification) RecordID() string { return v.VerificationID }

// SchemaVer implements Record.
func (v *ResolutionVerification) SchemaVer() SchemaVersion { return v.SchemaVersion }

// ProjectOf implements ProjectScoped.
func (v *ResolutionVerification) ProjectOf() string { return v.ProjectID }

// Validate checks semantic and schema constraints.
func (v *ResolutionVerification) Validate() error {
	const kind = "ResolutionVerification"
	if err := v.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	for field, val := range map[string]string{
		"verification_id":           v.VerificationID,
		"project_id":                v.ProjectID,
		"campaign_id":               v.CampaignID,
		"finding_id":                v.FindingID,
		"resolution_id":             v.ResolutionID,
		"verified_candidate_commit": v.VerifiedCandidateCommit,
		"rationale":                 v.Rationale,
	} {
		if err := requireNonEmptyTrimmed(kind, field, val); err != nil {
			return err
		}
	}
	if v.ContractRevision < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: contract_revision must be >= 1, got %d", kind, v.ContractRevision)
	}
	if !v.Outcome.Valid() {
		return enumError(kind, "outcome", string(v.Outcome),
			string(OutcomeVerifiedFixed), string(OutcomeVerifiedDismissed), string(OutcomeNotResolved), string(OutcomeReAdjudicationRequired))
	}
	if err := requireMinItems(kind, "evidence_refs", len(v.EvidenceRefs), 1); err != nil {
		return err
	}
	for _, ref := range v.EvidenceRefs {
		if err := requireNonEmptyTrimmed(kind, "evidence_refs item", ref); err != nil {
			return err
		}
	}
	if err := v.Verifier.Validate(kind+".verifier", ProvenanceRoleVerifier); err != nil {
		return err
	}
	return parseRFC3339(kind, "recorded_at", v.RecordedAt)
}

// MarshalJSON guarantees required arrays serialize as [], never null.
func (v ResolutionVerification) MarshalJSON() ([]byte, error) {
	type alias ResolutionVerification
	out := alias(v)
	out.EvidenceRefs = orEmpty(out.EvidenceRefs)
	return json.Marshal(out)
}

// Independent reports whether two actor provenances are independent per EWP §5.
//
// independent(a, b) = a.ActorID != b.ActorID ∧ a.ActorID ∉ b.LineageActorIDs ∧
//
//	b.ActorID ∉ a.LineageActorIDs ∧ a.InvocationID != b.InvocationID.
//
// Fails closed if either ActorID or InvocationID is empty or whitespace-only on either side.
func Independent(a, b ActorProvenance) bool {
	if strings.TrimSpace(a.ActorID) == "" || strings.TrimSpace(a.InvocationID) == "" ||
		strings.TrimSpace(b.ActorID) == "" || strings.TrimSpace(b.InvocationID) == "" {
		return false
	}
	if a.ActorID == b.ActorID {
		return false
	}
	if a.InvocationID == b.InvocationID {
		return false
	}
	for _, id := range b.LineageActorIDs {
		if id == a.ActorID {
			return false
		}
	}
	for _, id := range a.LineageActorIDs {
		if id == b.ActorID {
			return false
		}
	}
	return true
}

func independent(a, b ActorProvenance) bool {
	return Independent(a, b)
}

// CheckVerification checks link equalities, candidate rules, kind×outcome matrix,
// and actor independence between a finding, resolution, and verification (EWP §5, REQ-05).
// All errors return CategoryValidationFailed.
func CheckVerification(f *ReviewFinding, r *FindingResolution, v *ResolutionVerification) error {
	// Step 1: non-nil and each passes its own Validate().
	if f == nil {
		return errs.New(errs.CategoryValidationFailed, "finding is nil")
	}
	if r == nil {
		return errs.New(errs.CategoryValidationFailed, "resolution is nil")
	}
	if v == nil {
		return errs.New(errs.CategoryValidationFailed, "verification is nil")
	}
	if err := f.Validate(); err != nil {
		return errs.Wrap(errs.CategoryValidationFailed, err, "invalid finding")
	}
	if err := r.Validate(); err != nil {
		return errs.Wrap(errs.CategoryValidationFailed, err, "invalid resolution")
	}
	if err := v.Validate(); err != nil {
		return errs.Wrap(errs.CategoryValidationFailed, err, "invalid verification")
	}

	// Step 2: Links.
	if r.ProjectID != f.ProjectID || v.ProjectID != f.ProjectID {
		return errs.New(errs.CategoryValidationFailed, "project_id mismatch: finding=%q, resolution=%q, verification=%q", f.ProjectID, r.ProjectID, v.ProjectID)
	}
	if r.CampaignID != f.CampaignID || v.CampaignID != f.CampaignID {
		return errs.New(errs.CategoryValidationFailed, "campaign_id mismatch: finding=%q, resolution=%q, verification=%q", f.CampaignID, r.CampaignID, v.CampaignID)
	}
	if r.FindingID != f.FindingID || v.FindingID != f.FindingID {
		return errs.New(errs.CategoryValidationFailed, "finding_id mismatch: finding=%q, resolution=%q, verification=%q", f.FindingID, r.FindingID, v.FindingID)
	}
	if v.ResolutionID != r.ResolutionID {
		return errs.New(errs.CategoryValidationFailed, "resolution_id mismatch: resolution=%q, verification=%q", r.ResolutionID, v.ResolutionID)
	}
	if r.ContractRevision != f.ContractRevision || v.ContractRevision != f.ContractRevision {
		return errs.New(errs.CategoryValidationFailed, "contract_revision mismatch: finding=%d, resolution=%d, verification=%d", f.ContractRevision, r.ContractRevision, v.ContractRevision)
	}
	if r.TargetCandidateCommit != f.CandidateCommit {
		return errs.New(errs.CategoryValidationFailed, "target_candidate_commit mismatch: finding candidate=%q, resolution target=%q", f.CandidateCommit, r.TargetCandidateCommit)
	}

	// Step 3: Candidate rule.
	switch r.Kind {
	case ResolutionKindFixAttempted:
		if r.ResolvedCandidateCommit == nil || v.VerifiedCandidateCommit != *r.ResolvedCandidateCommit {
			return errs.New(errs.CategoryValidationFailed, "verified candidate commit %q does not match resolved candidate commit %v", v.VerifiedCandidateCommit, r.ResolvedCandidateCommit)
		}
	case ResolutionKindChallenge:
		if v.VerifiedCandidateCommit != f.CandidateCommit {
			return errs.New(errs.CategoryValidationFailed, "verified candidate commit %q does not match finding candidate commit %q", v.VerifiedCandidateCommit, f.CandidateCommit)
		}
	default:
		return errs.New(errs.CategoryValidationFailed, "unknown resolution kind %q", r.Kind)
	}

	// Step 4: Kind×outcome matrix.
	switch r.Kind {
	case ResolutionKindFixAttempted:
		switch v.Outcome {
		case OutcomeVerifiedFixed, OutcomeNotResolved, OutcomeReAdjudicationRequired:
			// allowed
		default:
			return errs.New(errs.CategoryValidationFailed, "outcome %q not allowed for kind fix_attempted", v.Outcome)
		}
	case ResolutionKindChallenge:
		switch v.Outcome {
		case OutcomeVerifiedDismissed, OutcomeNotResolved, OutcomeReAdjudicationRequired:
			// allowed
		default:
			return errs.New(errs.CategoryValidationFailed, "outcome %q not allowed for kind challenge", v.Outcome)
		}
	}

	// Step 5: Independence.
	if !independent(v.Verifier, r.Producer) {
		return errs.New(errs.CategoryValidationFailed, "verifier %q is not independent of producer %q", v.Verifier.ActorID, r.Producer.ActorID)
	}
	if r.Kind == ResolutionKindChallenge {
		if !independent(v.Verifier, f.Reviewer) {
			return errs.New(errs.CategoryValidationFailed, "verifier %q is not independent of reviewer %q for challenge resolution", v.Verifier.ActorID, f.Reviewer.ActorID)
		}
	}

	return nil
}

// DeriveFindingResolutionState computes the current lifecycle state from durable records (EWP §5, REQ-06).
func DeriveFindingResolutionState(f *ReviewFinding, rs []FindingResolution, vs []ResolutionVerification) (FindingResolutionState, error) {
	// Step 1: f non-nil and valid; every element of rs/vs valid; every r links to f.
	if f == nil {
		return "", errs.New(errs.CategoryValidationFailed, "finding is nil")
	}
	if err := f.Validate(); err != nil {
		return "", errs.Wrap(errs.CategoryValidationFailed, err, "invalid finding")
	}
	for _, r := range rs {
		if err := r.Validate(); err != nil {
			return "", errs.Wrap(errs.CategoryValidationFailed, err, "invalid resolution %s", r.ResolutionID)
		}
		if r.ProjectID != f.ProjectID || r.CampaignID != f.CampaignID || r.FindingID != f.FindingID || r.ContractRevision != f.ContractRevision {
			return "", errs.New(errs.CategoryValidationFailed, "resolution %s does not link to finding %s", r.ResolutionID, f.FindingID)
		}
	}
	for _, v := range vs {
		if err := v.Validate(); err != nil {
			return "", errs.Wrap(errs.CategoryValidationFailed, err, "invalid verification %s", v.VerificationID)
		}
	}

	// Step 2: if len(rs) == 0, return unresolved (len(vs) must be 0).
	if len(rs) == 0 {
		if len(vs) != 0 {
			return "", errs.New(errs.CategoryValidationFailed, "verifications present without resolutions")
		}
		return StateUnresolved, nil
	}

	// Step 3: attempt_no values MUST be exactly 1..len(rs) with no gaps or duplicates.
	seenAttempts := make(map[int]FindingResolution, len(rs))
	byID := make(map[string]FindingResolution, len(rs))
	for _, r := range rs {
		if r.AttemptNo < 1 || r.AttemptNo > len(rs) {
			return "", errs.New(errs.CategoryValidationFailed, "attempt_no %d out of range 1..%d", r.AttemptNo, len(rs))
		}
		if _, exists := seenAttempts[r.AttemptNo]; exists {
			return "", errs.New(errs.CategoryValidationFailed, "duplicate attempt_no %d", r.AttemptNo)
		}
		seenAttempts[r.AttemptNo] = r

		if _, exists := byID[r.ResolutionID]; exists {
			return "", errs.New(errs.CategoryValidationFailed, "duplicate resolution_id %s", r.ResolutionID)
		}
		byID[r.ResolutionID] = r
	}

	// Step 4: Each v MUST reference exactly one existing r.ResolutionID; at most one v per r.
	// Each (r, v) pair MUST pass CheckVerification (fail closed).
	verificationsByResID := make(map[string]*ResolutionVerification, len(vs))
	for i := range vs {
		v := &vs[i]
		r, exists := byID[v.ResolutionID]
		if !exists {
			return "", errs.New(errs.CategoryValidationFailed, "verification %s references unknown resolution %s", v.VerificationID, v.ResolutionID)
		}
		if _, already := verificationsByResID[v.ResolutionID]; already {
			return "", errs.New(errs.CategoryValidationFailed, "multiple verifications for resolution %s", v.ResolutionID)
		}
		verificationsByResID[v.ResolutionID] = v
		if err := CheckVerification(f, &r, v); err != nil {
			return "", err
		}
	}

	// Step 5: Every resolution except highest attempt_no MUST have verification with outcome not_resolved.
	for attempt := 1; attempt < len(rs); attempt++ {
		r := seenAttempts[attempt]
		v, ok := verificationsByResID[r.ResolutionID]
		if !ok {
			return "", errs.New(errs.CategoryValidationFailed, "superseded attempt %d (resolution %s) has no verification", attempt, r.ResolutionID)
		}
		if v.Outcome != OutcomeNotResolved {
			return "", errs.New(errs.CategoryValidationFailed, "superseded attempt %d (resolution %s) has outcome %q, must be not_resolved", attempt, r.ResolutionID, v.Outcome)
		}
	}

	// Step 6: Map outcome for the highest attempt_no resolution.
	highestRes := seenAttempts[len(rs)]
	v, ok := verificationsByResID[highestRes.ResolutionID]
	if !ok {
		return StateVerificationPending, nil
	}
	switch v.Outcome {
	case OutcomeVerifiedFixed:
		return StateVerifiedFixed, nil
	case OutcomeVerifiedDismissed:
		return StateVerifiedDismissed, nil
	case OutcomeReAdjudicationRequired:
		return StateReAdjudicationRequired, nil
	case OutcomeNotResolved:
		return StateUnresolved, nil
	default:
		return "", errs.New(errs.CategoryValidationFailed, "unexpected outcome %q", v.Outcome)
	}
}

func requireNonEmptyTrimmed(kind, field, value string) error {
	if strings.TrimSpace(value) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: %s is required and must not be empty or whitespace-only", kind, field)
	}
	return nil
}

func parseRFC3339(kind, field, value string) error {
	if strings.TrimSpace(value) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: %s is required", kind, field)
	}
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		if _, err := time.Parse(time.RFC3339, value); err != nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: %s %q is not valid RFC3339", kind, field, value)
		}
	}
	return nil
}
