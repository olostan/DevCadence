package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
)

// WorkloadKind identifies the engineering activity being calibrated (docs/PROTOCOLS.md §10B).
type WorkloadKind string

const (
	WorkloadNavigation     WorkloadKind = "navigation"
	WorkloadImplementation WorkloadKind = "implementation"
	WorkloadReview         WorkloadKind = "review"
	WorkloadArchitecture   WorkloadKind = "architectural_reasoning"
)

// Valid reports whether the workload kind is known.
func (w WorkloadKind) Valid() bool {
	switch w {
	case WorkloadNavigation, WorkloadImplementation, WorkloadReview, WorkloadArchitecture:
		return true
	}
	return false
}

// WorkloadEnvelope specifies empirical effective context bounds for a specific workload.
type WorkloadEnvelope struct {
	Workload               WorkloadKind `json:"workload"`
	EffectiveTokens        int          `json:"effective_tokens"`
	CalibrationTask        string       `json:"calibration_task"`
	CalibrationDate        string       `json:"calibration_date"`
	CalibrationEvidenceRef *string      `json:"calibration_evidence_ref,omitempty"`
	ConfidenceLevel        string       `json:"confidence_level"` // verified | provisional | unknown
}

// Validate checks WorkloadEnvelope constraints.
func (w WorkloadEnvelope) Validate() error {
	const kind = "WorkloadEnvelope"
	if !w.Workload.Valid() {
		return enumError(kind, "workload", string(w.Workload),
			string(WorkloadNavigation), string(WorkloadImplementation), string(WorkloadReview), string(WorkloadArchitecture))
	}
	if w.EffectiveTokens < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: effective_tokens must be >= 1, got %d", kind, w.EffectiveTokens)
	}
	if err := requireNonEmpty(kind, "calibration_task", w.CalibrationTask); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "calibration_date", w.CalibrationDate); err != nil {
		return err
	}
	if w.ConfidenceLevel == "verified" {
		if w.CalibrationEvidenceRef == nil || *w.CalibrationEvidenceRef == "" {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: calibration_evidence_ref is required when confidence_level is %q",
				kind, w.ConfidenceLevel)
		}
	}
	if w.CalibrationEvidenceRef != nil {
		if err := requireNonEmpty(kind, "calibration_evidence_ref", *w.CalibrationEvidenceRef); err != nil {
			return err
		}
	}
	switch w.ConfidenceLevel {
	case "verified", "provisional", "unknown":
		return nil
	default:
		return enumError(kind, "confidence_level", w.ConfidenceLevel, "verified", "provisional", "unknown")
	}
}

// TokenizerAccountingMethod indicates how tokens were counted.
type TokenizerAccountingMethod string

const (
	AccountingExactBPE            TokenizerAccountingMethod = "exact_bpe"
	AccountingProviderAPI         TokenizerAccountingMethod = "provider_api"
	AccountingApproximateEstimate TokenizerAccountingMethod = "approximate_estimate"
)

// Valid reports whether the accounting method is known.
func (m TokenizerAccountingMethod) Valid() bool {
	switch m {
	case AccountingExactBPE, AccountingProviderAPI, AccountingApproximateEstimate:
		return true
	}
	return false
}

// ContextProfile contains endpoint- and access-channel-specific capability and budget evidence.
// It captures hardware, runtime, quantization, and context limits per PROTOCOLS §10B.
type ContextProfile struct {
	SchemaVersion             SchemaVersion             `json:"schema_version"`
	ProfileID                 string                    `json:"profile_id"`
	EndpointID                string                    `json:"endpoint_id"`
	ChannelID                 string                    `json:"channel_id"`
	Runtime                   string                    `json:"runtime"`
	ModelRef                  string                    `json:"model_ref"`
	Quantization              *string                   `json:"quantization,omitempty"`
	ContextConfiguration     map[string]string         `json:"context_configuration,omitempty"`
	Revision                  int                       `json:"revision"`
	DeclaredWindowTokens      int                       `json:"declared_window_tokens"`
	RuntimeWindowTokens       int                       `json:"runtime_window_tokens"`
	WorkloadEnvelopes         []WorkloadEnvelope        `json:"workload_envelopes"`
	TargetResidentTokens      int                       `json:"target_resident_tokens"`
	HardResidentCeilingTokens int                       `json:"hard_resident_ceiling_tokens"`
	ProtectedCoreLimitTokens  int                       `json:"protected_core_limit_tokens"`
	ContractLimitTokens       int                       `json:"contract_limit_tokens"`
	MaxSingleLeaseTokens      int                       `json:"max_single_lease_tokens"`
	OutputReserveTokens       int                       `json:"output_reserve_tokens"`
	ToolTailReserveTokens     int                       `json:"tool_tail_reserve_tokens"`
	AccountingMethod          TokenizerAccountingMethod `json:"accounting_method"`
	EstimateUncertaintyRatio  float64                   `json:"estimate_uncertainty_ratio"`
	ObservedContextControl    ContextControl            `json:"observed_context_control"`
	ObservedPrefixCache       PrefixCache               `json:"observed_prefix_cache"`
}

// RecordKind implements Record.
func (c *ContextProfile) RecordKind() string { return "ContextProfile" }

// RecordID implements Record.
func (c *ContextProfile) RecordID() string { return c.ProfileID }

// SchemaVer implements Record.
func (c *ContextProfile) SchemaVer() SchemaVersion { return c.SchemaVersion }

// Validate enforces ContextProfile constraints per PROTOCOLS §10B.
func (c *ContextProfile) Validate() error {
	const kind = "ContextProfile"
	if err := c.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "profile_id", c.ProfileID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "endpoint_id", c.EndpointID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "channel_id", c.ChannelID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "runtime", c.Runtime); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "model_ref", c.ModelRef); err != nil {
		return err
	}
	if c.Revision < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: revision must be >= 1, got %d", kind, c.Revision)
	}
	if c.DeclaredWindowTokens < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: declared_window_tokens must be >= 1, got %d", kind, c.DeclaredWindowTokens)
	}
	if c.RuntimeWindowTokens < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: runtime_window_tokens must be >= 1, got %d", kind, c.RuntimeWindowTokens)
	}
	if c.RuntimeWindowTokens > c.DeclaredWindowTokens {
		return errs.New(errs.CategoryInvalidArgument, "%s: runtime_window_tokens (%d) cannot exceed declared_window_tokens (%d)", kind, c.RuntimeWindowTokens, c.DeclaredWindowTokens)
	}
	if c.HardResidentCeilingTokens < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: hard_resident_ceiling_tokens must be >= 1, got %d", kind, c.HardResidentCeilingTokens)
	}
	if c.OutputReserveTokens < 0 || c.ToolTailReserveTokens < 0 || c.ProtectedCoreLimitTokens < 0 ||
		c.ContractLimitTokens < 0 || c.MaxSingleLeaseTokens < 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: reserve and limit tokens cannot be negative", kind)
	}
	if c.HardResidentCeilingTokens+c.OutputReserveTokens+c.ToolTailReserveTokens > c.RuntimeWindowTokens {
		return errs.New(errs.CategoryInvalidArgument,
			"%s: hard_resident_ceiling_tokens (%d) + output_reserve (%d) + tool_tail_reserve (%d) exceeds runtime_window_tokens (%d)",
			kind, c.HardResidentCeilingTokens, c.OutputReserveTokens, c.ToolTailReserveTokens, c.RuntimeWindowTokens)
	}
	if c.TargetResidentTokens < 1 || c.TargetResidentTokens > c.HardResidentCeilingTokens {
		return errs.New(errs.CategoryInvalidArgument, "%s: target_resident_tokens (%d) must be between 1 and hard_resident_ceiling_tokens (%d)", kind, c.TargetResidentTokens, c.HardResidentCeilingTokens)
	}
	if err := requireMinItems(kind, "workload_envelopes", len(c.WorkloadEnvelopes), 1); err != nil {
		return err
	}
	seenWorkloads := make(map[WorkloadKind]struct{}, len(c.WorkloadEnvelopes))
	for i, env := range c.WorkloadEnvelopes {
		if err := env.Validate(); err != nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: workload_envelopes[%d]: %v", kind, i, err)
		}
		if _, dup := seenWorkloads[env.Workload]; dup {
			return errs.New(errs.CategoryInvalidArgument, "%s: duplicate workload envelope %q", kind, env.Workload)
		}
		seenWorkloads[env.Workload] = struct{}{}
	}
	if !c.AccountingMethod.Valid() {
		return enumError(kind, "accounting_method", string(c.AccountingMethod),
			string(AccountingExactBPE), string(AccountingProviderAPI), string(AccountingApproximateEstimate))
	}
	if c.EstimateUncertaintyRatio < 0.0 || c.EstimateUncertaintyRatio > 1.0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: estimate_uncertainty_ratio must be between 0.0 and 1.0, got %f", kind, c.EstimateUncertaintyRatio)
	}
	if !c.ObservedContextControl.Valid() {
		return enumError(kind, "observed_context_control", string(c.ObservedContextControl),
			string(ContextControlExactStateless), string(ContextControlAppendOnly), string(ContextControlOpaqueSession))
	}
	if !c.ObservedPrefixCache.Valid() {
		return enumError(kind, "observed_prefix_cache", string(c.ObservedPrefixCache),
			string(PrefixCacheExplicit), string(PrefixCacheImplicit), string(PrefixCacheSessionKV), string(PrefixCacheNone))
	}
	return nil
}

// MandatoryClauseRef identifies an exact normative clause deterministically admitted.
type MandatoryClauseRef struct {
	ClauseID      string `json:"clause_id"`
	SourceDoc     string `json:"source_doc"`
	Revision      string `json:"revision"`
	ContentDigest string `json:"content_digest"`
}

// Validate checks MandatoryClauseRef fields.
func (m MandatoryClauseRef) Validate() error {
	const kind = "MandatoryClauseRef"
	if err := requireNonEmpty(kind, "clause_id", m.ClauseID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "source_doc", m.SourceDoc); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "revision", m.Revision); err != nil {
		return err
	}
	return validateSHA256Digest(kind, "content_digest", m.ContentDigest)
}

// ContextManifest represents the compiled task intent, read/write scopes, and references (PROTOCOLS §10B).
type ContextManifest struct {
	SchemaVersion        SchemaVersion        `json:"schema_version"`
	ManifestID           string               `json:"manifest_id"`
	TaskID               string               `json:"task_id"`
	WorkPackageID        string               `json:"work_package_id"`
	WorkPackageRevision  int                  `json:"work_package_revision"`
	WorkPackageDigest    string               `json:"work_package_digest"`
	Role                 string               `json:"role"`
	BaseCommit           string               `json:"base_commit"`
	CandidateCommit      *string              `json:"candidate_commit,omitempty"`
	ProjectStateRevision string               `json:"project_state_revision"`
	MappingVersion       string               `json:"mapping_version"`
	SourceRevision       string               `json:"source_revision"`
	ReadEnvelope         []string             `json:"read_envelope"`
	WriteScope           []string             `json:"write_scope"`
	Domains              []string             `json:"domains"`
	RiskTags             []string             `json:"risk_tags"`
	MandatoryClauses     []MandatoryClauseRef `json:"mandatory_clauses"`
	InitialEvidenceRefs  []string             `json:"initial_evidence_refs"`
	DeferredEvidenceRefs []string             `json:"deferred_evidence_refs,omitempty"`
	Assumptions          []Assumption         `json:"assumptions"`
	ExplicitQuestions    []string             `json:"explicit_questions"`
	ExpansionTriggers    []string             `json:"expansion_triggers"`
	AdmissionProvenance  []string             `json:"admission_provenance"`
	ContextProfileID     string               `json:"context_profile_id"`
	BudgetPoolID         string               `json:"budget_pool_id"`
}

// RecordKind implements Record.
func (m *ContextManifest) RecordKind() string { return "ContextManifest" }

// RecordID implements Record.
func (m *ContextManifest) RecordID() string { return m.ManifestID }

// SchemaVer implements Record.
func (m *ContextManifest) SchemaVer() SchemaVersion { return m.SchemaVersion }

// Validate enforces ContextManifest constraints.
func (m *ContextManifest) Validate() error {
	const kind = "ContextManifest"
	if err := m.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "manifest_id", m.ManifestID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "task_id", m.TaskID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "work_package_id", m.WorkPackageID); err != nil {
		return err
	}
	if m.WorkPackageRevision < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: work_package_revision must be >= 1, got %d", kind, m.WorkPackageRevision)
	}
	if err := validateSHA256Digest(kind, "work_package_digest", m.WorkPackageDigest); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "role", m.Role); err != nil {
		return err
	}
	if len(m.BaseCommit) < 7 {
		return requireMinItems(kind, "base_commit characters", len(m.BaseCommit), 7)
	}
	if err := requireNonEmpty(kind, "project_state_revision", m.ProjectStateRevision); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "mapping_version", m.MappingVersion); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "source_revision", m.SourceRevision); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "context_profile_id", m.ContextProfileID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "budget_pool_id", m.BudgetPoolID); err != nil {
		return err
	}
	for i, clause := range m.MandatoryClauses {
		if err := clause.Validate(); err != nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: mandatory_clauses[%d]: %v", kind, i, err)
		}
	}
	for i, asm := range m.Assumptions {
		if err := asm.Validate(); err != nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: assumptions[%d]: %v", kind, i, err)
		}
	}
	if err := requireMinItems(kind, "admission_provenance", len(m.AdmissionProvenance), 1); err != nil {
		return err
	}
	for i, prov := range m.AdmissionProvenance {
		if strings.TrimSpace(prov) == "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: admission_provenance[%d] cannot be empty", kind, i)
		}
	}
	return nil
}

// EvidenceLeaseKind enumerates the leased evidence categories.
type EvidenceLeaseKind string

const (
	LeaseKindSourceSnippet   EvidenceLeaseKind = "source_snippet"
	LeaseKindDiffHunk        EvidenceLeaseKind = "diff_hunk"
	LeaseKindLogExcerpt      EvidenceLeaseKind = "log_excerpt"
	LeaseKindSymbolSignature EvidenceLeaseKind = "symbol_signature"
	LeaseKindNormativeClause EvidenceLeaseKind = "normative_clause"
)

// Valid reports whether the lease kind is known.
func (k EvidenceLeaseKind) Valid() bool {
	switch k {
	case LeaseKindSourceSnippet, LeaseKindDiffHunk, LeaseKindLogExcerpt, LeaseKindSymbolSignature, LeaseKindNormativeClause:
		return true
	}
	return false
}

// EvidenceLeaseStatus records the lifecycle state of leased working-set evidence.
type EvidenceLeaseStatus string

const (
	LeaseStatusActive      EvidenceLeaseStatus = "active"
	LeaseStatusReleased    EvidenceLeaseStatus = "released"
	LeaseStatusInvalidated EvidenceLeaseStatus = "invalidated"
)

// Valid reports whether the lease status is known.
func (s EvidenceLeaseStatus) Valid() bool {
	switch s {
	case LeaseStatusActive, LeaseStatusReleased, LeaseStatusInvalidated:
		return true
	}
	return false
}

// EvidenceLease is a verbatim content-addressed slice of working memory.
type EvidenceLease struct {
	SchemaVersion       SchemaVersion             `json:"schema_version"`
	LeaseID             string                    `json:"lease_id"`
	EvidenceKind        EvidenceLeaseKind         `json:"evidence_kind"`
	SourceRevision      string                    `json:"source_revision"`
	WorktreeID          string                    `json:"worktree_id"`
	FilePath            string                    `json:"file_path"`
	Locator             string                    `json:"locator"`
	ContentDigest       string                    `json:"content_digest"`
	AcquisitionQuestion string                    `json:"acquisition_question"`
	AcquisitionReason   string                    `json:"acquisition_reason"`
	Content             string                    `json:"content"`
	TokenCount          int                       `json:"token_count"`
	AccountingMethod    TokenizerAccountingMethod `json:"accounting_method"`
	Status              EvidenceLeaseStatus       `json:"status"`
	AcquiredAt          string                    `json:"acquired_at"`
	ExpiresAt           *string                   `json:"expires_at,omitempty"`
}

// RecordKind implements Record.
func (e *EvidenceLease) RecordKind() string { return "EvidenceLease" }

// RecordID implements Record.
func (e *EvidenceLease) RecordID() string { return e.LeaseID }

// SchemaVer implements Record.
func (e *EvidenceLease) SchemaVer() SchemaVersion { return e.SchemaVersion }

// Validate enforces EvidenceLease constraints, including content-addressing verification.
func (e *EvidenceLease) Validate() error {
	const kind = "EvidenceLease"
	if err := e.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "lease_id", e.LeaseID); err != nil {
		return err
	}
	if !e.EvidenceKind.Valid() {
		return enumError(kind, "evidence_kind", string(e.EvidenceKind),
			string(LeaseKindSourceSnippet), string(LeaseKindDiffHunk), string(LeaseKindLogExcerpt),
			string(LeaseKindSymbolSignature), string(LeaseKindNormativeClause))
	}
	if err := requireNonEmpty(kind, "source_revision", e.SourceRevision); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "worktree_id", e.WorktreeID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "file_path", e.FilePath); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "locator", e.Locator); err != nil {
		return err
	}
	if err := validateSHA256Digest(kind, "content_digest", e.ContentDigest); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "acquisition_question", e.AcquisitionQuestion); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "acquisition_reason", e.AcquisitionReason); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "content", e.Content); err != nil {
		return err
	}
	// Verbatim content-addressing check: content_digest must match SHA-256 of content
	hasher := sha256.New()
	hasher.Write([]byte(e.Content))
	expectedDigest := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if e.ContentDigest != expectedDigest {
		return errs.New(errs.CategoryInvalidArgument, "%s: content_digest (%q) does not match sha256 of content (%q)", kind, e.ContentDigest, expectedDigest)
	}
	if e.TokenCount < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: token_count must be >= 1, got %d", kind, e.TokenCount)
	}
	if !e.AccountingMethod.Valid() {
		return enumError(kind, "accounting_method", string(e.AccountingMethod),
			string(AccountingExactBPE), string(AccountingProviderAPI), string(AccountingApproximateEstimate))
	}
	if !e.Status.Valid() {
		return enumError(kind, "status", string(e.Status),
			string(LeaseStatusActive), string(LeaseStatusReleased), string(LeaseStatusInvalidated))
	}
	if err := requireNonEmpty(kind, "acquired_at", e.AcquiredAt); err != nil {
		return err
	}
	if e.ExpiresAt != nil {
		if err := requireNonEmpty(kind, "expires_at", *e.ExpiresAt); err != nil {
			return err
		}
		acqTime, errAcq := time.Parse(time.RFC3339Nano, e.AcquiredAt)
		expTime, errExp := time.Parse(time.RFC3339Nano, *e.ExpiresAt)
		if errAcq == nil && errExp == nil {
			if expTime.Before(acqTime) {
				return errs.New(errs.CategoryInvalidArgument, "%s: expires_at (%q) cannot be earlier than acquired_at (%q)", kind, *e.ExpiresAt, e.AcquiredAt)
			}
		} else if *e.ExpiresAt < e.AcquiredAt {
			return errs.New(errs.CategoryInvalidArgument, "%s: expires_at (%q) cannot be earlier than acquired_at (%q)", kind, *e.ExpiresAt, e.AcquiredAt)
		}
	}
	return nil
}

// CognitiveStateCapsule maintains compact intermediate hypotheses and TODOs across turns.
type CognitiveStateCapsule struct {
	Hypotheses            []string `json:"hypotheses"`
	ActiveTODOs           []string `json:"active_todos"`
	IntermediateDecisions []string `json:"intermediate_decisions"`
	OpenQuestions         []string `json:"open_questions"`
	EvidenceDependencies  []string `json:"evidence_dependencies"`
}

// EphemeralTailBlock encapsulates the transient tool results and current prompt tail.
type EphemeralTailBlock struct {
	RecentToolExchanges   []string `json:"recent_tool_exchanges"`
	CandidateDiffManifest *string  `json:"candidate_diff_manifest,omitempty"`
	ValidationSummaries   []string `json:"validation_summaries,omitempty"`
	CurrentAction         string   `json:"current_action"`
}

// TokenAccountingBreakdown details the resident token breakdown across layers.
type TokenAccountingBreakdown struct {
	RoleTokens          int                       `json:"role_tokens"`
	ContractTokens      int                       `json:"contract_tokens"`
	NormativeTokens     int                       `json:"normative_tokens"`
	StateTokens         int                       `json:"state_tokens"`
	EvidenceTokens      int                       `json:"evidence_tokens"`
	TailTokens          int                       `json:"tail_tokens"`
	OutputReserveTokens int                       `json:"output_reserve_tokens"`
	TotalResidentTokens int                       `json:"total_resident_tokens"`
	AccountingMethod    TokenizerAccountingMethod `json:"accounting_method"`
}

// Validate checks TokenAccountingBreakdown values.
func (t TokenAccountingBreakdown) Validate() error {
	const kind = "TokenAccountingBreakdown"
	if t.RoleTokens < 0 || t.ContractTokens < 0 || t.NormativeTokens < 0 ||
		t.StateTokens < 0 || t.EvidenceTokens < 0 || t.TailTokens < 0 ||
		t.OutputReserveTokens < 0 || t.TotalResidentTokens < 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: token counts cannot be negative", kind)
	}
	expectedResident := t.RoleTokens + t.ContractTokens + t.NormativeTokens + t.StateTokens + t.EvidenceTokens + t.TailTokens
	if t.TotalResidentTokens != expectedResident {
		return errs.New(errs.CategoryInvalidArgument,
			"%s: total_resident_tokens (%d) must equal sum of constituent layers (%d)", kind, t.TotalResidentTokens, expectedResident)
	}
	if !t.AccountingMethod.Valid() {
		return enumError(kind, "accounting_method", string(t.AccountingMethod),
			string(AccountingExactBPE), string(AccountingProviderAPI), string(AccountingApproximateEstimate))
	}
	return nil
}

// ContextPackStatus indicates whether the pack is ready for model invocation.
type ContextPackStatus string

const (
	PackStatusReady        ContextPackStatus = "ready"
	PackStatusContextUnfit ContextPackStatus = "context_unfit"
	PackStatusRejected     ContextPackStatus = "rejected"
)

// Valid reports whether the context pack status is known.
func (s ContextPackStatus) Valid() bool {
	switch s {
	case PackStatusReady, PackStatusContextUnfit, PackStatusRejected:
		return true
	}
	return false
}

// ContextPack is the ephemeral compiled invocation input reproducible from a manifest (PROTOCOLS §10B).
type ContextPack struct {
	SchemaVersion         SchemaVersion            `json:"schema_version"`
	PackID                string                   `json:"pack_id"`
	ManifestID            string                   `json:"manifest_id"`
	ManifestRevision      int                      `json:"manifest_revision"`
	RoleCore              string                   `json:"role_core"`
	ExecutionContract     string                   `json:"execution_contract"`
	NormativeClauses      []string                 `json:"normative_clauses"`
	CognitiveState        CognitiveStateCapsule    `json:"cognitive_state"`
	EvidenceWorkingSet    []EvidenceLease          `json:"evidence_working_set"`
	EphemeralTail         EphemeralTailBlock       `json:"ephemeral_tail"`
	TokenAccounting       TokenAccountingBreakdown `json:"token_accounting"`
	AdmittedObjectDigests map[string]string        `json:"admitted_object_digests"`
	PackDigest            string                   `json:"pack_digest"`
	CoverageSummary       string                   `json:"coverage_summary"`
	Status                ContextPackStatus        `json:"status"`
}

// RecordKind implements Record.
func (p *ContextPack) RecordKind() string { return "ContextPack" }

// RecordID implements Record.
func (p *ContextPack) RecordID() string { return p.PackID }

// SchemaVer implements Record.
func (p *ContextPack) SchemaVer() SchemaVersion { return p.SchemaVersion }

// Validate enforces ContextPack constraints per PROTOCOLS §10B.
func (p *ContextPack) Validate() error {
	const kind = "ContextPack"
	if err := p.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "pack_id", p.PackID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "manifest_id", p.ManifestID); err != nil {
		return err
	}
	if p.ManifestRevision < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: manifest_revision must be >= 1, got %d", kind, p.ManifestRevision)
	}
	if err := requireNonEmpty(kind, "role_core", p.RoleCore); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "execution_contract", p.ExecutionContract); err != nil {
		return err
	}
	for i, lease := range p.EvidenceWorkingSet {
		if err := lease.Validate(); err != nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: evidence_working_set[%d]: %v", kind, i, err)
		}
	}
	if err := p.TokenAccounting.Validate(); err != nil {
		return err
	}
	for k, v := range p.AdmittedObjectDigests {
		if strings.TrimSpace(k) == "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: admitted_object_digests key cannot be empty", kind)
		}
		if err := validateSHA256Digest(kind, "admitted_object_digests["+k+"]", v); err != nil {
			return err
		}
	}
	if err := validateSHA256Digest(kind, "pack_digest", p.PackDigest); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "coverage_summary", p.CoverageSummary); err != nil {
		return err
	}
	if !p.Status.Valid() {
		return enumError(kind, "status", string(p.Status),
			string(PackStatusReady), string(PackStatusContextUnfit), string(PackStatusRejected))
	}
	return nil
}
