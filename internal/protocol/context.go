package protocol

import (
	"strings"

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
	Workload        WorkloadKind `json:"workload"`
	EffectiveTokens int          `json:"effective_tokens"`
	CalibrationTask string       `json:"calibration_task"`
	CalibrationDate string       `json:"calibration_date"`
	ConfidenceLevel string       `json:"confidence_level"` // verified | provisional | unknown
}

// Validate checks WorkloadEnvelope constraints.
func (w WorkloadEnvelope) Validate() error {
	if !w.Workload.Valid() {
		return enumError("WorkloadEnvelope", "workload", string(w.Workload),
			string(WorkloadNavigation), string(WorkloadImplementation), string(WorkloadReview), string(WorkloadArchitecture))
	}
	if w.EffectiveTokens < 1 {
		return errs.New(errs.CategoryInvalidArgument, "WorkloadEnvelope: effective_tokens must be >= 1, got %d", w.EffectiveTokens)
	}
	if err := requireNonEmpty("WorkloadEnvelope", "calibration_task", w.CalibrationTask); err != nil {
		return err
	}
	if err := requireNonEmpty("WorkloadEnvelope", "calibration_date", w.CalibrationDate); err != nil {
		return err
	}
	switch w.ConfidenceLevel {
	case "verified", "provisional", "unknown":
		return nil
	default:
		return enumError("WorkloadEnvelope", "confidence_level", w.ConfidenceLevel, "verified", "provisional", "unknown")
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
type ContextProfile struct {
	SchemaVersion             SchemaVersion             `json:"schema_version"`
	ProfileID                 string                    `json:"profile_id"`
	EndpointID                string                    `json:"endpoint_id"`
	ChannelID                 string                    `json:"channel_id"`
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
	if c.Revision < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: revision must be >= 1, got %d", kind, c.Revision)
	}
	if c.DeclaredWindowTokens < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: declared_window_tokens must be >= 1, got %d", kind, c.DeclaredWindowTokens)
	}
	if c.RuntimeWindowTokens < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: runtime_window_tokens must be >= 1, got %d", kind, c.RuntimeWindowTokens)
	}
	if c.HardResidentCeilingTokens < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: hard_resident_ceiling_tokens must be >= 1, got %d", kind, c.HardResidentCeilingTokens)
	}
	if c.TargetResidentTokens < 1 || c.TargetResidentTokens > c.HardResidentCeilingTokens {
		return errs.New(errs.CategoryInvalidArgument, "%s: target_resident_tokens must be between 1 and hard_resident_ceiling_tokens", kind)
	}
	for i, env := range c.WorkloadEnvelopes {
		if err := env.Validate(); err != nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: workload_envelopes[%d]: %v", kind, i, err)
		}
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
	if !strings.HasPrefix(m.ContentDigest, "sha256:") || len(m.ContentDigest) != 71 {
		return errs.New(errs.CategoryInvalidArgument, "%s: content_digest must be sha256 hex prefixed", kind)
	}
	return nil
}

// ContextManifest represents the compiled task intent, read/write scopes, and references.
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
	if err := requireNonEmpty(kind, "role", m.Role); err != nil {
		return err
	}
	if len(m.BaseCommit) < 7 {
		return requireMinItems(kind, "base_commit characters", len(m.BaseCommit), 7)
	}
	if err := requireNonEmpty(kind, "project_state_revision", m.ProjectStateRevision); err != nil {
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

// Validate enforces EvidenceLease constraints.
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
	if err := requireNonEmpty(kind, "file_path", e.FilePath); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "locator", e.Locator); err != nil {
		return err
	}
	if !strings.HasPrefix(e.ContentDigest, "sha256:") || len(e.ContentDigest) != 71 {
		return errs.New(errs.CategoryInvalidArgument, "%s: content_digest must be sha256 hex prefixed", kind)
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

// ContextPack is the ephemeral compiled invocation input reproducible from a manifest.
type ContextPack struct {
	SchemaVersion      SchemaVersion            `json:"schema_version"`
	PackID             string                   `json:"pack_id"`
	ManifestID         string                   `json:"manifest_id"`
	ManifestRevision   int                      `json:"manifest_revision"`
	RoleCore           string                   `json:"role_core"`
	ExecutionContract  string                   `json:"execution_contract"`
	NormativeClauses   []string                 `json:"normative_clauses"`
	CognitiveState     CognitiveStateCapsule    `json:"cognitive_state"`
	EvidenceWorkingSet []EvidenceLease          `json:"evidence_working_set"`
	EphemeralTail      EphemeralTailBlock       `json:"ephemeral_tail"`
	TokenAccounting    TokenAccountingBreakdown `json:"token_accounting"`
	PackDigest         string                   `json:"pack_digest"`
	Status             ContextPackStatus        `json:"status"`
}

// RecordKind implements Record.
func (p *ContextPack) RecordKind() string { return "ContextPack" }

// RecordID implements Record.
func (p *ContextPack) RecordID() string { return p.PackID }

// SchemaVer implements Record.
func (p *ContextPack) SchemaVer() SchemaVersion { return p.SchemaVersion }

// Validate enforces ContextPack constraints.
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
	if err := p.TokenAccounting.Validate(); err != nil {
		return err
	}
	if !p.Status.Valid() {
		return enumError(kind, "status", string(p.Status),
			string(PackStatusReady), string(PackStatusContextUnfit), string(PackStatusRejected))
	}
	if err := requireNonEmpty(kind, "pack_digest", p.PackDigest); err != nil {
		return err
	}
	return nil
}
