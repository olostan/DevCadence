# Engineering Work Package: WP-M3C-1 — Portfolio protocol, economics, context capabilities and refactoring proposals

- **Milestone:** M3C — Cognition Resource and Session Substrate
- **Scope card:** [docs/WORK_PACKAGES.md#wp-m3c-1--portfolio-protocol-economics-context-capabilities-and-refactoring-proposals](../WORK_PACKAGES.md#wp-m3c-1--portfolio-protocol-economics-context-capabilities-and-refactoring-proposals)
- **Base commit:** `58869d99635ee0d05b5fe30e3b152dacddc12445` (origin/main, merge of PR #14 — Context Working-Set Architecture)
- **Branch:** `feat/m3c-cognition-substrate`
- **Task ID:** `task-m3c-1-portfolio-protocol-economics-context-refactoring`
- **Work Package ID:** `WP-M3C-1`
- **Version:** 1.3
- **Status:** Approved by Frontier Principal Engineer for Implementation (Post-Review Contract Resolution)

---

## 1. Context Manifest (AGENTS.md §2, docs/PROTOCOLS.md §10B)

```json
{
  "manifest_id": "manifest-wp-m3c-1-v3",
  "task_id": "task-m3c-1-portfolio-protocol-economics-context-refactoring",
  "work_package_id": "WP-M3C-1",
  "work_package_revision": 3,
  "role": "principal_engineer",
  "base_commit": "58869d99635ee0d05b5fe30e3b152dacddc12445",
  "project_state_revision": "bootstrap-m3b-closed",
  "read_envelope": [
    "docs/WORK_PACKAGES.md",
    "docs/PROTOCOLS.md",
    "docs/adr/0018-adaptive-cognition-portfolio-and-workflow-synthesis.md",
    "docs/adr/0019-non-conversational-cognition-and-adaptive-review.md",
    "internal/protocol/*",
    "schemas/*",
    "tests/twin_fields_test.go",
    "tests/schema_fixtures_test.go",
    "fixtures/protocol/*"
  ],
  "write_scope": [
    "internal/protocol/access_channel.go",
    "internal/protocol/context.go",
    "internal/protocol/refactoring_proposal.go",
    "internal/protocol/economics.go",
    "internal/protocol/portfolio.go",
    "schemas/access-channel.schema.json",
    "schemas/context-profile.schema.json",
    "schemas/context-manifest.schema.json",
    "schemas/context-pack.schema.json",
    "schemas/evidence-lease.schema.json",
    "schemas/refactoring-proposal.schema.json",
    "schemas/budget-pool.schema.json",
    "schemas/cognition-portfolio.schema.json",
    "schemas/portfolio-recommendation.schema.json",
    "schemas/workflow-plan.schema.json",
    "schemas/README.md",
    "internal/schema/schema.go",
    "tests/twin_fields_test.go",
    "tests/schema_fixtures_test.go",
    "fixtures/protocol/*",
    "docs/work-packages/wp-m3c-1-ewp.md",
    "HANDOFF.md"
  ],
  "domains": [
    "cognition_protocol",
    "adaptive_context",
    "economic_regimes",
    "living_work_packages"
  ],
  "risk_tags": [
    "protocol_drift",
    "schema_twin_mismatch",
    "silent_billing_fallback",
    "credential_leak"
  ],
  "mandatory_clauses": [
    {
      "clause_id": "DCI-018",
      "source_doc": "docs/INVARIANTS.md",
      "summary": "Authority does not imply residency: normative rules must not be preloaded wholesale."
    },
    {
      "clause_id": "DCI-019",
      "source_doc": "docs/INVARIANTS.md",
      "summary": "Delegation must have no hidden requirements: all required constraints must be admitted."
    },
    {
      "clause_id": "DCI-054",
      "source_doc": "docs/INVARIANTS.md",
      "summary": "Provider neutrality: semantics must not bake specific provider idioms into protocol types."
    },
    {
      "clause_id": "DCI-055",
      "source_doc": "docs/INVARIANTS.md",
      "summary": "Equal treatment: local runtimes, authenticated CLIs, and direct APIs share one abstraction."
    },
    {
      "clause_id": "DCI-081",
      "source_doc": "docs/INVARIANTS.md",
      "summary": "Zero plaintext credentials in protocol records: use opaque CredentialRef only."
    },
    {
      "clause_id": "DCI-092",
      "source_doc": "docs/INVARIANTS.md",
      "summary": "No silent lossy parsing: strict JSON schema decoding with additionalProperties: false."
    },
    {
      "clause_id": "DCI-104",
      "source_doc": "docs/INVARIANTS.md",
      "summary": "Explicit monetary authority: loss of local/subscription quota never falls back to metered billing."
    },
    {
      "clause_id": "ADR-0018-S9",
      "source_doc": "docs/adr/0018-adaptive-cognition-portfolio-and-workflow-synthesis.md",
      "summary": "No silent paid fallback: metered API fallback is forbidden without explicit authorization."
    },
    {
      "clause_id": "ADR-0019-S1",
      "source_doc": "docs/adr/0019-non-conversational-cognition-and-adaptive-review.md",
      "summary": "ContextControl = ExactStateless | AppendOnly | OpaqueSession; PrefixCache = Explicit | Implicit | SessionKV | None."
    },
    {
      "clause_id": "ADR-0019-S3",
      "source_doc": "docs/adr/0019-non-conversational-cognition-and-adaptive-review.md",
      "summary": "Living work packages and RefactoringProposal bottom-up challenge protocol."
    }
  ],
  "assumptions": [
    {
      "id": "asm-m3c-1-pure-protocol",
      "statement": "WP-M3C-1 is strictly protocol, schema, and validator types. No runtime subprocesses or network calls are made.",
      "status": "verified",
      "material": true
    },
    {
      "id": "asm-m3c-1-parity-enforcement",
      "statement": "All published schemas must be twin-tested with TestSchemaTopLevelFieldsMatchTheGoTwin and compiled in internal/schema.",
      "status": "verified",
      "material": true
    }
  ],
  "expansion_triggers": [
    "Mutation to existing M0-M3B schemas requires explicit Principal escalation.",
    "Introduction of provider-specific fields into protocol records requires escalation."
  ]
}
```

---

## 2. Execution Contract (Authoritative & Bounded)

### 2.1 Objective

Establish the provider-neutral deterministic protocol and schema types for:
1. `AccessChannel` and session capabilities, distinguishing access pathways from model identity;
2. `ContextProfile`, `ContextManifest`, `ContextPack`, and `EvidenceLease` per PROTOCOLS §10B and ADR-0019;
3. `ContextControl` (`exact_stateless`, `append_only`, `opaque_session`) and `PrefixCache` (`explicit`, `implicit`, `session_kv`, `none`) capabilities;
4. `RefactoringProposal` enabling bottom-up upstream challenge without architectural rot (ADR-0019 §3);
5. `EconomicRegime`, `BudgetPool`, `BudgetState`, and `ResourceState` (ADR-0018 §3);
6. `CognitionPortfolio`, `PortfolioRecommendation`, and `WorkflowPlan` without conflating model identity with billing semantics (ADR-0018 §1, §7, §8);
7. Full schema compilation, Go twin field parity, and fixture validation test suites.

### 2.2 Verbatim MUST & MUST-NOT Constraints

- **MUST NOT allow silent paid fallback (ADR-0018 §9, DCI-104):** `BudgetPool.FallbackAllowedToMetered` must validate to `false` by default and fail closed. Loss of subscription quota or local inference must never trigger fallback to a metered API without explicit human policy override.
- **MUST NOT infer context control capabilities from local/remote labels (ADR-0019 §1, PROTOCOLS §10B):** `ContextControl` and `PrefixCache` must represent observed adapter facts. A local runtime is not automatically stateless, and a remote API is not automatically opaque.
- **MUST NOT report hidden opaque session tokens as zero (PROTOCOLS §10B):** Opaque session drivers that cannot inspect resident prompt state must report non-zero uncertainty and cannot satisfy strict-bound policies.
- **MUST NOT permit lossy code summaries in EvidenceLease (ADR-0019 §1, PROTOCOLS §10B):** Code snippets and normative text must be verbatim, content-addressed with SHA-256 digests, and locate exact ranges.
- **MUST NOT impose artificial turn limits in prompts (ADR-0019 §2):** Prompts must never inject turn countdowns; outer limits are strictly enforced by the control plane.
- **MUST enforce structural orthogonality (ADR-0018, DCI-054, DCI-081):**
  `CredentialRef != CognitionEndpoint != AccessChannel != Session != Account != EconomicRegime != CognitionPortfolio`.
  No secrets or credentials may appear in any protocol record or schema.
- **MUST enforce strict schema decoding (DCI-092):** Every published JSON Schema must set `additionalProperties: false` at all object levels.
- **MUST enforce bidirectional twin parity:** Every top-level schema property must exist on the Go twin struct and vice-versa, verified by `TestSchemaTopLevelFieldsMatchTheGoTwin`.

---

## 3. Detailed Go Twin Specifications

All types live in `package protocol` (`internal/protocol/`) and implement `protocol.Record` where durable records are published.

### 3.1 Access Channel & Session Capabilities (`internal/protocol/access_channel.go`)

```go
package protocol

// ChannelKind distinguishes the operational pathway to model cognition.
type ChannelKind string

const (
    ChannelDirectHTTPAPI    ChannelKind = "direct_http_api"
    ChannelCLISubprocess    ChannelKind = "cli_subprocess"
    ChannelLocalDaemonSocket ChannelKind = "local_daemon_socket"
    ChannelRemoteAgentProxy ChannelKind = "remote_agent_proxy"
)

// SessionMode describes how session state is persisted across requests.
type SessionMode string

const (
    SessionStatelessPerCall SessionMode = "stateless_per_call"
    SessionPersistentState  SessionMode = "persistent_session"
    SessionResumableHandle  SessionMode = "resumable_session"
)

// ContextControl describes the adapter's prompt controllability (ADR-0019 §1).
type ContextControl string

const (
    ContextControlExactStateless ContextControl = "exact_stateless"
    ContextControlAppendOnly     ContextControl = "append_only"
    ContextControlOpaqueSession  ContextControl = "opaque_session"
)

// PrefixCache describes observable KV/prefix caching capabilities (ADR-0019 §1).
type PrefixCache string

const (
    PrefixCacheExplicit  PrefixCache = "explicit"
    PrefixCacheImplicit  PrefixCache = "implicit"
    PrefixCacheSessionKV PrefixCache = "session_kv"
    PrefixCacheNone      PrefixCache = "none"
)

// AccessChannel represents a concrete access path to an endpoint.
type AccessChannel struct {
    SchemaVersion          SchemaVersion   `json:"schema_version"`
    ChannelID              string          `json:"channel_id"`
    EndpointID             string          `json:"endpoint_id"`
    Kind                   ChannelKind     `json:"kind"`
    SessionMode            SessionMode     `json:"session_mode"`
    ContextControl         ContextControl  `json:"context_control"`
    PrefixCache            PrefixCache     `json:"prefix_cache"`
    SupportsStreaming      bool            `json:"supports_streaming"`
    SupportsTools          bool            `json:"supports_tools"`
    NativeWorktreeAccess   bool            `json:"native_worktree_access"`
    CredentialRefID        *string         `json:"credential_ref_id,omitempty"`
    MaxConcurrentRequests  int             `json:"max_concurrent_requests"`
}
```

### 3.2 Adaptive Context Working-Set Types (`internal/protocol/context.go`)

Per `docs/PROTOCOLS.md §10B` and `docs/adr/0019-non-conversational-cognition-and-adaptive-review.md`:

```go
package protocol

// WorkloadKind identifies the engineering activity being calibrated.
type WorkloadKind string

const (
    WorkloadNavigation     WorkloadKind = "navigation"
    WorkloadImplementation WorkloadKind = "implementation"
    WorkloadReview         WorkloadKind = "review"
    WorkloadArchitecture   WorkloadKind = "architectural_reasoning"
)

// WorkloadEnvelope specifies empirical effective context bounds.
type WorkloadEnvelope struct {
    Workload               WorkloadKind `json:"workload"`
    EffectiveTokens        int          `json:"effective_tokens"`
    CalibrationTask        string       `json:"calibration_task"`
    CalibrationDate        string       `json:"calibration_date"`
    CalibrationEvidenceRef *string      `json:"calibration_evidence_ref,omitempty"`
    ConfidenceLevel        string       `json:"confidence_level"` // verified | provisional | unknown
}

// TokenizerAccountingMethod indicates how tokens were counted.
type TokenizerAccountingMethod string

const (
    AccountingExactBPE            TokenizerAccountingMethod = "exact_bpe"
    AccountingProviderAPI         TokenizerAccountingMethod = "provider_api"
    AccountingApproximateEstimate TokenizerAccountingMethod = "approximate_estimate"
)

// ContextProfile contains endpoint- and workload-specific capability/budget evidence.
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

// MandatoryClauseRef identifies an exact normative clause deterministically admitted.
type MandatoryClauseRef struct {
    ClauseID      string `json:"clause_id"`
    SourceDoc     string `json:"source_doc"`
    Revision      string `json:"revision"`
    ContentDigest string `json:"content_digest"`
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
    ContextProfileID     string               `json:"context_profile_id"`
    BudgetPoolID         string               `json:"budget_pool_id"`
    AdmissionProvenance  []string             `json:"admission_provenance"`
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

// EvidenceLeaseStatus records the lifecycle state of leased working-set evidence.
type EvidenceLeaseStatus string

const (
    LeaseStatusActive      EvidenceLeaseStatus = "active"
    LeaseStatusReleased    EvidenceLeaseStatus = "released"
    LeaseStatusInvalidated EvidenceLeaseStatus = "invalidated"
)

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

// CognitiveStateCapsule maintains compact intermediate hypotheses and TODOs across turns.
type CognitiveStateCapsule struct {
    Hypotheses             []string `json:"hypotheses"`
    ActiveTODOs            []string `json:"active_todos"`
    IntermediateDecisions  []string `json:"intermediate_decisions"`
    OpenQuestions          []string `json:"open_questions"`
    EvidenceDependencies   []string `json:"evidence_dependencies"`
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
    RoleTokens           int                       `json:"role_tokens"`
    ContractTokens       int                       `json:"contract_tokens"`
    NormativeTokens      int                       `json:"normative_tokens"`
    StateTokens          int                       `json:"state_tokens"`
    EvidenceTokens       int                       `json:"evidence_tokens"`
    TailTokens           int                       `json:"tail_tokens"`
    OutputReserveTokens  int                       `json:"output_reserve_tokens"`
    TotalResidentTokens  int                       `json:"total_resident_tokens"`
    AccountingMethod     TokenizerAccountingMethod `json:"accounting_method"`
}

// ContextPackStatus indicates whether the pack is ready for model invocation.
type ContextPackStatus string

const (
    PackStatusReady         ContextPackStatus = "ready"
    PackStatusContextUnfit  ContextPackStatus = "context_unfit"
    PackStatusRejected      ContextPackStatus = "rejected"
)

// ContextPack is the ephemeral compiled invocation input reproducible from a manifest.
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
```

> [!NOTE]
> **Compilation Boundary & Resident Ceiling Invariant:** In WP-M3C-1, `ContextPack` records are validated for structural integrity, layer accounting sums, and content-addressed SHA-256 digests. Cross-record verification between a compiled `ContextPack` with `status: ready` and its parent `ContextProfile.HardResidentCeilingTokens` is an operational invariant of the M3C Context Compiler / Session Driver (WP-M3C-2/3), where both records are loaded in-context during compilation before setting `status: ready`.

### 3.3 Living Work Packages & Refactoring Proposals (`internal/protocol/refactoring_proposal.go`)

```go
package protocol

// ReversibilityClass characterizes the blast radius and rollback difficulty of a refactor.
type ReversibilityClass string

const (
    ReversibilityTrivial     ReversibilityClass = "trivial"
    ReversibilityModerate    ReversibilityClass = "moderate"
    ReversibilitySignificant ReversibilityClass = "significant"
)

// ProposalStatus records the adjudication state of a refactoring proposal.
type ProposalStatus string

const (
    ProposalProposed   ProposalStatus = "proposed"
    ProposalAccepted   ProposalStatus = "accepted"
    ProposalRejected   ProposalStatus = "rejected"
    ProposalSuperseded ProposalStatus = "superseded"
)

// ProposalAdjudication captures the Principal/Human adjudication record.
type ProposalAdjudication struct {
    AdjudicatedBy         Actor   `json:"adjudicated_by"`
    AdjudicatedAt         string  `json:"adjudicated_at"`
    DispositionNotes      string  `json:"disposition_notes"`
    ResultingWorkPackageID *string `json:"resulting_work_package_id,omitempty"`
}

// RefactoringProposal allows workers to challenge upstream contracts without code rot (ADR-0019 §3).
type RefactoringProposal struct {
    SchemaVersion            SchemaVersion         `json:"schema_version"`
    ProposalID               string                `json:"proposal_id"`
    CreatedAt                string                `json:"created_at"`
    SourceWorkPackageID      string                `json:"source_work_package_id"`
    TargetWorkPackageID      string                `json:"target_work_package_id"`
    ArchitecturalTension     string                `json:"architectural_tension"`
    ContradictionEvidence    []EvidenceRef         `json:"contradiction_evidence"`
    ProposedInterface        string                `json:"proposed_interface"`
    AffectedCallers          []string              `json:"affected_callers"`
    Reversibility            ReversibilityClass    `json:"reversibility"`
    ReversibilityRationale   string                `json:"reversibility_rationale"`
    Status                   ProposalStatus        `json:"status"`
    Adjudication             *ProposalAdjudication `json:"adjudication,omitempty"`
}
```

### 3.4 Economic Regimes & Budget Pools (`internal/protocol/economics.go`)

```go
package protocol

// EconomicRegime defines billing/consumption semantics (ADR-0018 §3).
type EconomicRegime string

const (
    RegimeLocalCompute         EconomicRegime = "local_compute"
    RegimeSubscriptionQuota    EconomicRegime = "subscription_quota"
    RegimeMeteredAPI           EconomicRegime = "metered_api"
    RegimePrepaidCredits       EconomicRegime = "prepaid_credits"
    RegimeEnterpriseAllocation EconomicRegime = "enterprise_allocation"
    RegimeUnknownCustom        EconomicRegime = "unknown_custom"
)

// BudgetUnit is the dimensional unit of metering.
type BudgetUnit string

const (
    UnitUSDCents     BudgetUnit = "usd_cents"
    UnitTokenCredits BudgetUnit = "token_credits"
    UnitTokens       BudgetUnit = "tokens"
    UnitRequests     BudgetUnit = "requests"
    UnitSeconds      BudgetUnit = "seconds"
)

// BudgetPeriod is the window over which a budget applies.
type BudgetPeriod string

const (
    PeriodRollingHour  BudgetPeriod = "rolling_hour"
    PeriodRollingDay   BudgetPeriod = "rolling_day"
    PeriodBillingCycle BudgetPeriod = "billing_cycle"
    PeriodPerAttempt   BudgetPeriod = "per_attempt"
    PeriodPerTask      BudgetPeriod = "per_task"
)

// BudgetPool tracks resource allocations without conflating them with model identity.
type BudgetPool struct {
    SchemaVersion            SchemaVersion  `json:"schema_version"`
    PoolID                   string         `json:"pool_id"`
    Name                     string         `json:"name"`
    Regime                   EconomicRegime `json:"regime"`
    Unit                     BudgetUnit     `json:"unit"`
    HardLimit                int64          `json:"hard_limit"`
    SoftAlertLimit           int64          `json:"soft_alert_limit"`
    Period                   BudgetPeriod   `json:"period"`
    AllowOverage             bool           `json:"allow_overage"`
    FallbackAllowedToMetered bool           `json:"fallback_allowed_to_metered"`
}

// BudgetPoolStatus expresses current pool capacity.
type BudgetPoolStatus string

const (
    BudgetStatusHealthy           BudgetPoolStatus = "healthy"
    BudgetStatusSoftLimitExceeded BudgetPoolStatus = "soft_limit_exceeded"
    BudgetStatusExhausted         BudgetPoolStatus = "exhausted"
    BudgetStatusUnknown           BudgetPoolStatus = "unknown"
)

// BudgetState captures live pool balance and status with honest unknown representation (PROTOCOLS §10B).
type BudgetState struct {
    SchemaVersion    SchemaVersion    `json:"schema_version"`
    PoolID           string           `json:"pool_id"`
    CurrentUsage     *int64           `json:"current_usage,omitempty"`
    RemainingBalance *int64           `json:"remaining_balance,omitempty"`
    PeriodStart      *string          `json:"period_start,omitempty"`
    PeriodEnd        *string          `json:"period_end,omitempty"`
    Status           BudgetPoolStatus `json:"status"`
    ObservedAt       string           `json:"observed_at"`
    UnknownFields    []string         `json:"unknown_fields,omitempty"`
}

// ResourceState captures machine-level compute availability with honest unknown metrics (PROTOCOLS §10B).
type ResourceState struct {
    SchemaVersion           SchemaVersion `json:"schema_version"`
    HostID                  string        `json:"host_id"`
    Timestamp               string        `json:"timestamp"`
    AvailableGPUMemoryBytes *int64        `json:"available_gpu_memory_bytes,omitempty"`
    AvailableRAMBytes       *int64        `json:"available_ram_bytes,omitempty"`
    MaxConcurrentSlots      *int          `json:"max_concurrent_slots,omitempty"`
    ActiveSlots             *int          `json:"active_slots,omitempty"`
    UnknownMetrics          []string      `json:"unknown_metrics,omitempty"`
}
```

### 3.5 Cognition Portfolio & Workflow Topology (`internal/protocol/portfolio.go`)

```go
package protocol

// FallbackBinding defines an explicit, routable fallback path for a role binding (ADR-0018 §1, §9).
type FallbackBinding struct {
    EndpointID       string `json:"endpoint_id"`
    ChannelID        string `json:"channel_id"`
    BudgetPoolID     string `json:"budget_pool_id"`
    ContextProfileID string `json:"context_profile_id"`
}

// RoleBinding maps an engineering role to an endpoint, channel, and budget pool (ADR-0018 §1, FR-062).
type RoleBinding struct {
    Role             string            `json:"role"`
    EndpointID       string            `json:"endpoint_id"`
    ChannelID        string            `json:"channel_id"`
    BudgetPoolID     string            `json:"budget_pool_id"`
    ContextProfileID string            `json:"context_profile_id"`
    Priority         int               `json:"priority"`
    Fallbacks        []FallbackBinding `json:"fallbacks,omitempty"`
}

// DiversityPolicy specifies provider and model diversity constraints (COGNITION_PORTFOLIO §11, PROTOCOLS §10B).
type DiversityPolicy struct {
    RequireDistinctModelsForReview    bool `json:"require_distinct_models_for_review,omitempty"`
    RequireDistinctProvidersForReview bool `json:"require_distinct_providers_for_review,omitempty"`
    RequireDistinctEndpointsForReview bool `json:"require_distinct_endpoints_for_review,omitempty"`
}

// EscalationRule specifies an explicit escalation transition path between roles/endpoints (COGNITION_PORTFOLIO §11).
type EscalationRule struct {
    FromRole         string `json:"from_role"`
    ToRole           string `json:"to_role"`
    TriggerCondition string `json:"trigger_condition"`
    MaxEscalations   int    `json:"max_escalations"`
}

// WorkflowDefaults specifies default execution constraints for synthesized workflows (COGNITION_PORTFOLIO §11).
type WorkflowDefaults struct {
    DefaultTopology       WorkflowTopologyKind `json:"default_topology,omitempty"`
    DefaultTimeoutSeconds int                  `json:"default_timeout_seconds,omitempty"`
    MaxRetries            int                  `json:"max_retries,omitempty"`
}

// CognitionPortfolio is the canonical routing configuration (ADR-0018 §7, FR-062).
type CognitionPortfolio struct {
    SchemaVersion         SchemaVersion     `json:"schema_version"`
    PortfolioID           string            `json:"portfolio_id"`
    Revision              int               `json:"revision"`
    CreatedAt             string            `json:"created_at"`
    Channels              []AccessChannel   `json:"channels"`
    RoleBindings          []RoleBinding     `json:"role_bindings"`
    BudgetPools           []BudgetPool      `json:"budget_pools"`
    MaxSourceExposure     SourceExposure    `json:"max_source_exposure"`
    ExcludedEndpointIDs   []string          `json:"excluded_endpoint_ids,omitempty"`
    BudgetReservations    map[string]int64  `json:"budget_reservations,omitempty"`
    DiversityRequirements *DiversityPolicy  `json:"diversity_requirements,omitempty"`
    EscalationRules       []EscalationRule  `json:"escalation_rules,omitempty"`
    WorkflowDefaults      *WorkflowDefaults `json:"workflow_defaults,omitempty"`
}

// PortfolioRecommendation is an AI-suggested or heuristic portfolio proposal.
type PortfolioRecommendation struct {
    SchemaVersion          SchemaVersion      `json:"schema_version"`
    RecommendationID       string             `json:"recommendation_id"`
    InventoryDigest        string             `json:"inventory_digest"`
    SynthesizedAt          string             `json:"synthesized_at"`
    RecommendedPortfolio   CognitionPortfolio `json:"recommended_portfolio"`
    Rationale              string             `json:"rationale"`
    ExplanatoryDiagnostics []string           `json:"explanatory_diagnostics"`
    CapabilityProvenance   []string           `json:"capability_provenance"`
}

// WorkflowTopologyKind describes the task-specific execution flow.
type WorkflowTopologyKind string

const (
    TopologySinglePass            WorkflowTopologyKind = "single_pass"
    TopologyIterativeEscalation   WorkflowTopologyKind = "iterative_escalation"
    TopologyDualIndependentReview WorkflowTopologyKind = "dual_independent_review"
    TopologyDeterministicOnly     WorkflowTopologyKind = "deterministic_only"
)

// StageKind distinguishes cognitive from deterministic stages in a workflow (ADR-0018 §8).
type StageKind string

const (
    StageKindCognition     StageKind = "cognition"
    StageKindDeterministic StageKind = "deterministic"
)

// WorkflowStage defines one cognitive or deterministic pass in a workflow plan.
type WorkflowStage struct {
    StageID             string    `json:"stage_id"`
    Role                string    `json:"role"`
    Kind                StageKind `json:"kind"`
    IsReview            bool      `json:"is_review,omitempty"`
    Order               int       `json:"order"`
    DependsOn           []string  `json:"depends_on,omitempty"`
    BudgetPoolID        string    `json:"budget_pool_id"`
    TimeoutSeconds      int       `json:"timeout_seconds"`
    EndpointID          *string   `json:"endpoint_id,omitempty"`
    ChannelID           *string   `json:"channel_id,omitempty"`
    ContextProfileID    *string   `json:"context_profile_id,omitempty"`
    RetryLimit          int       `json:"retry_limit,omitempty"`
    EscalationTarget    *string   `json:"escalation_target,omitempty"`
    DeterministicGateID *string   `json:"deterministic_gate_id,omitempty"`
}

// WorkflowPlan describes the task-specific workflow topology (ADR-0018 §8).
type WorkflowPlan struct {
    SchemaVersion SchemaVersion        `json:"schema_version"`
    PlanID        string               `json:"plan_id"`
    TaskID        string               `json:"task_id"`
    WorkPackageID string               `json:"work_package_id"`
    Topology      WorkflowTopologyKind `json:"topology"`
    Stages        []WorkflowStage      `json:"stages"`
}
```

---

## 4. Published JSON Schemas

The following Draft 2020-12 JSON Schemas must be published under `schemas/` with `additionalProperties: false` enforced across every object definition:

1. `schemas/access-channel.schema.json`
2. `schemas/context-profile.schema.json`
3. `schemas/context-manifest.schema.json`
4. `schemas/context-pack.schema.json`
5. `schemas/evidence-lease.schema.json`
6. `schemas/refactoring-proposal.schema.json`
7. `schemas/budget-pool.schema.json`
8. `schemas/budget-state.schema.json`
9. `schemas/resource-state.schema.json`
10. `schemas/cognition-portfolio.schema.json`
11. `schemas/portfolio-recommendation.schema.json`
12. `schemas/workflow-plan.schema.json`

All schema names must be registered in `internal/schema/schema.go`, documented in `schemas/README.md`, and wired into `RecordKindToSchema`.

---

## 5. Acceptance Criteria & Deterministic Verification

| Deliverable / Requirement | Verification Command / Suite | Pass Criteria |
|---|---|---|
| Schema Compilation | `go test ./internal/schema/...` | All 12 new schemas compile successfully in `TestEverySchemaCompiles`. |
| Top-Level Field Parity | `go test ./tests -run TestSchemaTopLevelFieldsMatchTheGoTwin` | All new protocol records match their schema twin properties bidirectionally. |
| Record Kind Mapping | `go test ./tests -run TestEveryRecordKindHasASchema` | Every new schema has its Go twin mapped in `RecordKindToSchema`. |
| Fixture Validation | `go test ./tests -run "TestValidFixturesValidate\|TestInvalidFixturesAreRejected"` | Valid and invalid fixtures for each schema pass validation. |
| Semantic Validation Tests | `go test ./internal/protocol/...` | Validation logic rejects empty IDs, unknown enum constants, negative tokens/limits, and unvalidated fallback. |
| Non-Fallback Guarantee | `go test ./internal/protocol/... -run TestBudgetPoolValidation` | `BudgetPool` rejects `FallbackAllowedToMetered = true` without explicit manual regime. |
| Zero Credential Leakage | Code inspection & fixture check | No schema includes secret fields (tokens, passwords, api_keys). |

---

## 6. Non-Goals / Forbidden Changes for WP-M3C-1

- **No driver subprocess execution:** WP-M3C-1 creates data contracts and schemas only. Subprocess runners and direct API clients belong to WP-M3C-2.
- **No dynamic portfolio activation:** `active-portfolio.json` runtime swapping and validation belong to WP-M3C-3.
- **No modification of M0–M3B schemas:** Existing schemas remain immutable.
- **No external network dependencies:** All tests must run offline deterministically.

---

## 7. Escalation Triggers

- If an existing invariant or schema from M0–M3B must be modified to support M3C types, halt and escalate to the Principal Engineer.
- If a provider-specific requirement arises that cannot be expressed neutrally, halt and escalate.
