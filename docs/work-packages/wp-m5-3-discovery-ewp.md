# WP-M5-3 — Durable discovery operations and specification readiness

## Identity

- Work Package ID: WP-M5-3
- Revision: 2 (consolidated ARC04/IF03–IF06 repair; planning candidate)
- Task ID: task-m5-3-discovery
- Base: `bd6c424e292815460033b2570dce4d682ef5cb73` (`origin/main` when inspected)
- Project state revision: not supplied; the implementation task must record its actual canonical revision before delegation. This is not a fabricated `ps_` value.
- Contract digest: not calculated; bind the reviewed immutable contract in the delegation manifest.
- Target endpoint/profile: bounded Go implementer with protocol/schema and SQLite integration competence; complete contract admission is mandatory.
- Status: **DRAFT — BLOCKED ON DEPENDENCY CONTRACTS AND INDEPENDENT REVIEW; NOT READY FOR IMPLEMENTATION**.
- Dependencies: WP-M5-1 atomic revision-guarded batch and principal envelope/schema; WP-M5-2 immutable, locally bound CallerContext and deterministic action policy. Dependencies below are proposals, not existing exports at this base.

## Objective

Expose discovery as typed semantic operations, with human product authority, durable provenance, immutable ledger revisions and reconstructable specification state. A fresh Principal session must recover the same intent, questions, requirements, evidence and readiness without a conversation transcript. Preserve the distinction between recording supplied review evidence and actually executing independent review.

## Context Manifest

- Roles: contract author, bounded Go implementer, independent protocol/authority and persistence reviewers. Read `prompts/implementer.md` for implementation; use `prompts/specification-reviewer.md` for actual specification review.
- Task/contract: WP-M5-3 r2 at the base above; write authorization for this planning task is only this Markdown file.
- Implementation read envelope: `internal/controlplane/{service,operations,records,references}.go`, `internal/state/{discovery,projection,reduce}.go`, `internal/events/{payloads_discovery,records}.go`, requirement event definitions, `internal/protocol/{discovery,ambiguity,requirement,specification,specification_review_result,project_state}.go`, `internal/storage/{records,store}.go`, matching schemas, and WP1/WP2 final contracts.
- Applicable clauses: AGENTS.md §§2, 4A, 6–9, 14–15; WORK_PACKAGES.md §§Execution Contract and Context Manifest, Implementation Readiness, Required contract closure; DISCOVERY_AND_SPECIFICATION.md §§8, 11–12, 17–19; DCI-005, 008, 009, 011–016, 046–049, 123–124. Exact admitted invariant clauses are quoted under Requirements; retrieval of the named bounded discovery sections is mandatory.
- Risk tags: human authority, optimistic concurrency, record/event consistency, review provenance, stale readiness, crash/restart.
- Initial evidence: `controlplane.Service.Apply` is one event plus records in one transaction; `state.discoveryState` reduces discovery events; `events.RecordReferencing` verifies durable record claims; ambiguity opened/resolved have no ledger revision/digest binding; `SpecificationReadiness.Validate` checks internal verdict/check consistency but is not an evidence-based readiness service.
- Deferred: model/provider/session execution internals, transport details, architecture/implementation dispatch. Admit them only through a separate EWP.
- Assumptions: WP1 supplies guarded ApplyBatch; WP2 supplies immutable caller policy and project binding; protected human receipts are a separate unresolved security dependency. These are unresolved dependencies, not verified facts.
- Re-resolution/escalation: changed dependency signatures, newly required policy action authorization, altered legacy event interpretation, inability to admit this whole contract, or a readiness proof unavailable from durable input.
- Context budget: no implicit numeric endpoint capacity; compiler/profile admission must prove the complete contract fits. `CONTEXT_UNFIT` is not permission to omit clauses.

## Semantic scope envelope

### Authorized implementation domains / path patterns

- New `internal/principal/discovery/` service and bounded tests, consuming WP2 contracts.
- Additive discovery records/types in `internal/protocol/`, typed events/reference checks in `internal/events/`, and snapshot pointer/equality validation in `internal/state/`.
- Matching additive schemas: `discovery-snapshot.schema.json`, `discovery-authority-evidence.schema.json`, `discovery-review-evidence.schema.json`; versioned principal discovery request/response schemas using WP1 envelope.
- Narrow control-plane referenced-snapshot validation only if needed to verify aggregate references. Batch/CAS infrastructure is WP1 ownership; do not implement another transaction boundary here.
- Affected bounded owning contract sections in DISCOVERY_AND_SPECIFICATION.md, PROJECT_STATE.md, PROTOCOLS.md and schemas/README.md; no historical milestone claims rewritten.

### Explicitly forbidden semantic changes

- No product confirmation from model text, MCP argument `actor_kind`, session label, self-reported authority or tool name.
- No credentials, network/provider setup, metered inference fallback, experiment execution, worker dispatch, architecture approval, independent-review scheduler implementation or host transport.
- No bypass of WP1 atomic guards, WP2 authority checks, existing record validation or legacy event lineage.
- No automatic promotion of a persisted review to executed independent review, or of proposed/evidence-backed requirements to human-confirmed intent.

### LOCAL_DISCRETION

Private helper/file decomposition, deterministic ordering helpers, test fixtures and errors' explanatory text. Public DTOs, authority semantics, event meanings, persistence and verdict rules are not discretionary.

## Requirements

Admitted normative text:

- DCI-005: “A material assumption must be represented as an assumption until verified. Inference must not be serialized as fact.”
- DCI-008: “A frontier principal, consultant, or local agent must not silently turn unresolved human intent into a confirmed requirement or architectural fact.”
- DCI-009: “Goals, acceptable tradeoffs, privacy preferences, user-visible semantics, scope choices and other product-authority decisions belong to the human/product authority.”
- DCI-015: “Model inference must not be serialized as human-confirmed intent.”
- DCI-016: “architecture must not begin while material ambiguity remains unresolved unless that ambiguity is explicitly accepted as risk or safely deferred behind a documented boundary.”
- DCI-014 requires progressive evidence depth; DCI-011 requires retrievable provenance; DCI-123/124 require deterministic authorization and prohibit recommendations expanding authority.

| ID | Strength | Requirement |
| --- | --- | --- |
| R1 | MUST | Every successful mutation atomically stores immutable documents, per-entry events where applicable and a final discovery snapshot event under an expected project-state revision. |
| R2 | MUST | Bind caller/project/allowed actions outside serialized arguments; derive journal actor from that binding and human/reviewer provenance from independently verified protected evidence. |
| R3 | MUST | Store exact ProblemModel, AmbiguityLedger, Requirement, ProductDecision, DiscoveryExperiment, SpecificationReviewResult and SpecificationReadiness revisions/digests; never resolve evidence by unconstrained latest lookup. |
| R4 | MUST | Preserve epistemic state; confirmed requirements and product decisions require trusted human confirmation evidence. Evidence-backed technical facts require retrievable evidence, not confidence. |
| R5 | MUST | Reconstruct a full discovery view from immutable snapshots/records plus journal; preserve legacy histories and distinguish unbound legacy records. |
| R6 | MUST | Validate ledger documents against per-entry journal state at batch end, preventing a durable ledger/event disagreement. |
| R7 | MUST | Readiness uses the exact current discovery input set, independent review evidence, current human reflection, unresolved material questions and all fixed checks. A later input mutation invalidates positive readiness. |
| R8 | MUST | Review recording and review execution have different methods/outcomes. Missing execution port returns `model_unavailable`; it creates no completed review event or positive readiness. |
| R9 | MUST | Fail without any record/event/projection change on missing authority, revision conflict, malformed evidence, stale input, failed member or final snapshot mismatch. |
| R10 | MUST | Provide semantic bounded summaries and exact version/digest handles; raw documents are progressively requested under WP2 evidence policy. |

## Invariants / state rules

| ID | Rule | Requirements |
| --- | --- | --- |
| I1 | Snapshot revision starts at 1 and increments exactly once per successful discovery batch; record versions are immutable, monotonic per kind/id, and explicit. | R1,R3 |
| I2 | Batch failure leaves record set, journal high watermark and materialized ProjectState byte-equivalent to their pre-call logical state. | R1,R9 |
| I3 | Snapshot live ledger entries and journal open-question set agree in ID, authority and architectural impact; resolved/deferred entries are absent from that open set. | R6 |
| I4 | Every confirmed requirement traces to trusted human evidence and an active matching product decision or direct human statement; model identity is never human evidence. | R2,R4 |
| I5 | A readiness record assesses current InputDigest and separate ReadinessBasisDigest excluding readiness itself; only exact-current input, receipts, review history and status overlay without unclosed blockers can carry positive verdict. | R7 |
| I6 | Author/reviewer independence is verified by stable trusted identities and immutable assessed input; an unknown identity is not independent. | R7,R8 |
| I8 | Current-input failing/conflicting review invalidates readiness immediately even with unchanged InputDigest; chronology, not last PASS, controls unresolved blocking history. | R7 |
| I7 | Legacy events keep their existing meaning; the additive snapshot validates and indexes state without replaying or double-counting prior discovery effects. | R5,R6 |

## Interface / algorithm contract

### Dependency surface and blockers

WP1 published planning interface (not yet implemented):

```go
type BatchCommand struct {
    ProjectID string
    Actor protocol.Actor
    Correlation events.Correlation
    ExpectedStateRevision string
    WorkPackage *WorkPackageGuard
    Commands []controlplane.Command
    Preconditions, Postconditions []controlplane.BatchGuard
}
type BatchResult struct { Results []controlplane.Result; ProjectState *protocol.ProjectState }
func (s *controlplane.Service) ApplyBatch(ctx context.Context, cmd BatchCommand) (BatchResult, error)
```

One SQLite transaction checks expected revision before writes and applies ordered members. Bounds are 1–256 members and aggregate serialized records/payloads <=1 MiB; duplicate record identities are rejected. WP3 supplies no WorkPackage guard. Ordered Results carry committed events. WP1 requires identical member/outer protocol.Actor and events.Correlation. Derive Actor{Kind: protocol.ActorPrincipal, ID: caller.PrincipalID}; do not impersonate human in the journal. Human authority is carried by verified receipt, not journal Kind. Correlation is events.Correlation{} for these discovery batches; retain meta.CorrelationID in WP2 operation lineage, not as a falsely named task/run identifier. Protocol payload-derived canonical correlation remains existing control-plane behavior. Empty expected revision is invalid. `controlplane.ErrStaleProjectState` maps to semantic `STALE_PROJECT_STATE`; generic conflict is not automatically stale. WP3 supplies one trusted Postcondition for final aggregate validation, never a second transaction. WP1 exact trusted guard contracts:

```go
type BatchReadView interface {
    ProjectState() *protocol.ProjectState
    Record(ctx context.Context, kind, id string, version int) (storage.StoredRecord, error)
}
type BatchGuard interface { Check(ctx context.Context, view BatchReadView) error }
```

Preconditions/Postconditions each have at most eight members and are trusted Go-only, never wire/model arguments. Preconditions run before record writes; Postconditions after all members before commit. The read view is transaction/project scoped, invalid after callback, and guards cannot write, execute tools or perform effects. WP3's guard resolves all exact snapshot pointers through view.Record and compares derived ledger/count/status/readiness facts with view.ProjectState; any error rolls back the whole batch. The single-document RecordReferencing check remains responsible for snapshot identity/digest and compact claims available in its JSON; it must not pretend to resolve other records itself.

WP1 common types are exact:

```go
type CallMeta struct { SchemaVersion, ProjectID, ExpectedStateRevision, CorrelationID string }
type CallerContext struct {
    PrincipalID, ProjectID string
    AllowedActions []string
    PolicyRef string
    MaxEvidenceBytes, MaxSnippetLines int
    SourceDepth string
}
```

Use schema_version "1.0"; validate IDs/revision/digests/bounds as WP1 specifies. Each method takes caller and meta separately; CallerContext is not wire-decodable. Reject caller.ProjectID != meta.ProjectID. PrincipalID/PolicyRef must be present; exact action must occur in immutable AllowedActions, and every method additionally calls the required `principal.PolicyResolver.Check(ctx, caller, meta, action) error`. A binding grant is necessary but not sufficient. Missing PolicyResolver is INVALID_ARGUMENT at construction; denial yields POLICY_DENIED before read, mutation, inference or disclosure. Evidence limits/depth remain enforced. Canonical grants are **exact MCP tool names**, with no discovery.read/discovery.write aliases:

| MCP tool / exact principal action | Go method | Semantics |
| --- | --- | --- |
| initialize_project | Initialize | bootstrap registration only |
| discovery_state | View | bounded discovery pointers/summary |
| record_problem_model | ReviseProblem | one new model revision |
| record_ambiguities | ReviseLedger | one complete ledger revision |
| record_product_decision | RecordProductDecision | protected human receipt mandatory |
| record_requirements | RecordRequirement | RequirementsInput batch, 1–64 records, one snapshot/commit |
| record_discovery_experiment | RecordExperiment | record supplied experiment outcome |
| review_specification | ExecuteReview | actual independent port invocation, not supplied review completion |
| record_specification_readiness | RecordReadiness | computed exact-basis verdict |

RecordReview and RecordReflection are trusted operator-only Go methods; **not extra MCP tools**. Their internal nonwire policy actions are `operator.record_specification_review` and `operator.record_human_reflection`; these names must not enter the principal binding enum/AllowedActions. Their caller comes from protected operator ingress, and PolicyResolver must independently validate that ingress and internal action. A principal CallerContext cannot construct the operator ingress. The secure ingress is a disabled dependency until approved; no production accepting fake. Internal persistence of ExecuteReview's verified result retains the public action review_specification and does not grant operator action. A declared action never proves human authority.

For human confirmation the required additional protected local receipt verifier must authenticate a human-issued receipt independently of the model process. A marker/JSON file readable or writable under the same OS identity, payload actor flag, AllowedActions value or PolicyRef string is insufficient isolation. WP2 does not implement receipt minting. Missing, unverified or unavailable trustworthy receipt returns semantic `NEEDS_HUMAN` with no write. The protected receipt issuance/verification architecture is **BLOCKED and must be separately approved before human-changing operations can be implemented as enabled paths**. Non-human discovery operations can be planned independently, but the whole WP is not ready while this security boundary is unresolved.

### Service signatures and explicit inputs

New package `internal/principal/discovery`, using the published common types:

```go
type Service struct { /* immutable injected ports; no hidden global state */ }
func New(opts Options) (*Service, error)
func (s *Service) Initialize(ctx context.Context, caller principal.CallerContext, meta principal.CallMeta, in InitializeInput) (controlplane.Result, error)
func (s *Service) View(ctx context.Context, caller principal.CallerContext, meta principal.CallMeta) (DiscoveryView, error)
func (s *Service) ReviseProblem(ctx context.Context, caller principal.CallerContext, meta principal.CallMeta, in ProblemInput) (MutationResult, error)
func (s *Service) ReviseLedger(ctx context.Context, caller principal.CallerContext, meta principal.CallMeta, in LedgerInput) (MutationResult, error)
func (s *Service) RecordProductDecision(ctx context.Context, caller principal.CallerContext, meta principal.CallMeta, in DecisionInput) (MutationResult, error)
func (s *Service) RecordRequirement(ctx context.Context, caller principal.CallerContext, meta principal.CallMeta, in RequirementsInput) (MutationResult, error)
func (s *Service) RecordExperiment(ctx context.Context, caller principal.CallerContext, meta principal.CallMeta, in ExperimentInput) (MutationResult, error)
func (s *Service) RecordReview(ctx context.Context, caller principal.CallerContext, meta principal.CallMeta, in ReviewInput) (MutationResult, error)
func (s *Service) ExecuteReview(ctx context.Context, caller principal.CallerContext, meta principal.CallMeta, in ReviewRequest) (MutationResult, error)
func (s *Service) RecordReflection(ctx context.Context, caller principal.CallerContext, meta principal.CallMeta, in ReflectionInput) (MutationResult, error)
func (s *Service) RecordReadiness(ctx context.Context, caller principal.CallerContext, meta principal.CallMeta, in ReadinessInput) (MutationResult, error)
```

All input structs use explicit named fields, not arbitrary events/raw protocol.Record:

- InitializeInput `{ProjectID, Name, MilestoneID, MilestoneTitle, VisionRef, CurrentOutcome, RepositoryPath, AcceptedCommit, Branch string; ActiveInvariants []string}`. This is existing controlplane.InitProjectInput's exact data shape excluding its Actor. Policy checks initialize_project and caller/meta/input project equality first. Require meta.ExpectedStateRevision absent for bootstrap; call InitProject(ctx, InitProjectInput{... Actor: protocol.Actor{Kind: protocol.ActorPrincipal, ID: caller.PrincipalID}}) explicitly. Never inherit InitProject's human/operator default. Existing absolute-path and milestone validation apply; missing Name follows existing ProjectID fallback. This registers a project only; repository/source adoption/readiness is not inferred or required for registration. No discovery snapshot exists until first discovery mutation. Already initialized project returns existing transition/conflict classification with no append; no update/reinitialization bypass.

- ProblemInput `{Model protocol.ProblemModel; Summary string}`. Revision is prior +1, initial 1. Derive assumption count from model assumptions; client cannot submit count. A claimed HumanReflectionRevision is rejected; reflection is a separate authorized operation.
- LedgerInput `{Ledger protocol.AmbiguityLedger; HumanReceiptRefs map[string]string}`. Map keys are affected ambiguity IDs and values protected receipt handles; unrecognized/unused keys are invalid. Desired complete immutable ledger, revision prior +1, unchanged ledger ID. It may add/reopen questions, update nonterminal investigation metadata, resolve/defer existing questions; deletion, terminal rewriting and changes to existing question/authority/impact require a new question ID. A reopen preserves prior resolution in old revision and clears terminal fields in new revision.
- DecisionInput `{Decision protocol.ProductDecision; Version int; ResolvesAmbiguityID string; SourceRef string; HumanReceiptRef string}`. SourceRef must identify the statement confirmed by a verified protected receipt. Service binds authority actor; no caller-supplied actor accepted.
- RequirementsInput `{Requirements []RequirementInput}` requires 1–64 items, unique RequirementID and deterministic ID ordering. Each member is RequirementInput `{Requirement protocol.Requirement; Version int; HumanEvidenceRef *RecordPointer; HumanReceiptRef string}`. Pointer mandatory for confirmed; direct human statement source must match trusted evidence. A proposed principal inference with NeedsHumanConfirmation=false is rejected, not silently confirmed. All items validate first; then one ApplyBatch emits ordered RequirementRecorded members and exactly one final snapshot. One failed item rolls back all 1–64 records; no per-item partial success.
- ExperimentInput `{Experiment protocol.DiscoveryExperiment; Version int}`. Only running, completed or inconclusive statuses are supported for event-backed recording in this slice. Planned/cancelled return unsupported with no write; a future lifecycle EWP may add events. Recording never runs the method.
- ReviewInput `{Review protocol.SpecificationReviewResult; Version int; Evidence protocol.DiscoveryReviewEvidence}`. Evidence is verified through trusted review-record action authorization/provenance, not accepted because JSON names a reviewer.
- ReviewRequest `{InputDigest string; Dimension protocol.SpecificationReviewDimension}`. Exact current input; execution port handles approved resource/privacy/spend policy separately.
- ReflectionInput `{ProblemModelID string; ProblemModelRevision int; InputDigest string; SourceRef string; HumanReceiptRef string}`. A verified protected human receipt confirms the exact current interpretation; record protected-receipt-verified authority evidence and snapshot reflection pointer. Do not mutate the old model record.
- ReadinessInput `{Assessment protocol.SpecificationReadiness; Version int; InputDigest, ReadinessBasisDigest string; RiskEvidenceRefs []RecordPointer; HumanReceiptRefs []string}`. Qualitative checks are proposed assessments requiring evidence; service computes final verdict under rules below, never trusts supplied Verdict.

Options has exact fields `ControlPlane *controlplane.Service`, `Policy principal.PolicyResolver`, `ReviewPort IndependentReviewPort`, `HumanReceipts HumanReceiptVerifier`, `Evidence EvidenceResolver`. Missing ControlPlane/Policy/Evidence is INVALID_ARGUMENT; ReviewPort/HumanReceipts may be nil, producing MODEL_UNAVAILABLE/NEEDS_HUMAN for their respective operation. Every method uses exact action-grant admission plus Policy.Check; do not add mutable alias grants or treat PolicyRef as authorization by itself.

```go
type ReceiptSubject struct { Kind, ID string; Version int }
type HumanReceipt struct { HumanActorID, SourceRef, Purpose, InputDigest string; Subject ReceiptSubject }
type PolicyResolver interface {
    Check(ctx context.Context, caller principal.CallerContext, meta principal.CallMeta, action string) error
} // defined in internal/principal, not duplicated by discovery
type HumanReceiptVerifier interface {
    Verify(ctx context.Context, caller principal.CallerContext,
        receiptRef, purpose, inputDigest string, subject ReceiptSubject) (HumanReceipt, error)
}
type EvidenceResolver interface {
    Resolve(ctx context.Context, caller principal.CallerContext,
        meta principal.CallMeta, ref string) error
}
```

Verify must validate authentic issuance, subject/input/purpose binding and caller/project eligibility, and return an independently established human identity. HumanReceipt is not wire input. This defines the consumer contract only; secure issuer/isolation/protected ingress remains BLOCKED, not delegated invention. A nil verifier cannot be substituted with an accepting fake in production. EvidenceResolver proves authorized retrievability; it does not prove a claim true merely because a source exists. Implement resolver against WP2 progressive evidence policy only after its exact port is reconciled. Local SourceRef attestation for external independent review has the same provenance isolation blocker and cannot be accepted from model JSON alone.

MutationResult `{ProjectState *protocol.ProjectState; Snapshot RecordPointer; Events []events.Event}`; DiscoveryView `{ProjectID, StateRevision string; Snapshot *RecordPointer; Problem *RecordPointer; Ledger *RecordPointer; Requirements, Decisions, Experiments, Reviews []RecordPointer; Readiness, Reflection *RecordPointer; InputDigest, ReadinessBasisDigest string; DecisionStatuses []DecisionStatusEntry; LegacyUnbound bool}`. Empty initialized project returns empty arrays and nil pointers, not false readiness. Summary fields follow WP2 response envelope and bounded evidence policy; no document bodies in View.

### Exact additive durable representation

Define `protocol.RecordPointer{Kind string; ID string; Version int; Digest string}`. Digest uses existing protocol.Digest format; version >=1 and nonempty recognized kind/id. Ordered reference arrays sort by kind, ID, version; duplicate kind/id entries are invalid except Reviews, which retains distinct exact historical versions keyed by (kind,id,version); duplicate exact tuples are invalid. An updated review must not remove a prior current-input blocking result/evidence. All new records implement RecordKind/RecordID/SchemaVer/Validate and use schema_version `1.0` following existing protocol encoding, with matching schemas and registry integration.

`protocol.DiscoverySnapshot`:

```text
SchemaVersion; SnapshotID string; ProjectID string; Revision int;
PreviousSnapshot *RecordPointer; Problem *RecordPointer; Ledger *RecordPointer;
Requirements []RecordPointer; Decisions []RecordPointer; Experiments []RecordPointer;
Reviews []RecordPointer; AuthorityEvidence []RecordPointer;
Reflection *RecordPointer; Readiness *RecordPointer; InputDigest string;
DecisionStatuses []DecisionStatusEntry; ReadinessBasisDigest string; ReadinessAssessedBasisDigest string;
```

Snapshot ID is service allocated stable per project on first snapshot, retained thereafter; record version = Revision. Arrays are complete current index, not an update delta. References resolve exact project/kind/id/version/digest inside the batch transaction. No latest substitution. InputDigest is digest of canonical JSON of `{project_id,problem,ledger,requirements,decisions,experiments}` only, with ordered arrays; review/reflection/readiness/authority receipts are deliberately excluded to avoid circular hashes. No timestamps or actor names enter this input digest. Snapshot record digest covers every field.

`DecisionStatusEntry{DecisionID string; Status protocol.ProductDecisionStatus}` is sorted by DecisionID. Fold the journal strictly by increasing sequence: for each ProductDecisionRecorded, set the status of its Supersedes ID to superseded when present, then set its own ID to payload.Status. Later explicit reconfirmation of an old ID sets it confirmed again. Count active confirmed statuses from this overlay, never by walking static document Supersedes links or trusting old document Status. The overlay is validated against canonical journal projection; exact records remain immutable. Superseded provenance may still explain historical confirmation under existing reducer semantics; new service confirmation requires currently active authority. Withdrawn is not active.

ReadinessBasisDigest is the digest of canonical JSON `{input_digest,reviews,review_evidence,reflection,authority_evidence,decision_statuses}` with ordered exact pointers and chronological overlay above. Exclude readiness and its own authority/assessment records to avoid circular hashes; authority_evidence here includes protected human input/risk receipts established before readiness, not a receipt referring to the readiness document itself. Add `ReviewEvidence []RecordPointer` to DiscoverySnapshot; retain all evidence for current-input reviews, including blocking history. Positive readiness requires both InputDigest and ReadinessAssessedBasisDigest == current ReadinessBasisDigest. A later current-input review changes basis even when InputDigest does not. A failing/conflicting review blocks immediately; a later PASS cannot silently erase it. Clearing such a blocker requires separately authorized adjudication or rereview closure explicitly referencing the blocker, with independently verified evidence. That closure runtime/representation is not implemented in this slice: pending its approved contract, keep the block and not_ready. No worker may invent a last-PASS-wins rule.

`protocol.DiscoveryAuthorityEvidence`:

```text
SchemaVersion; EvidenceID string; ProjectID string;
Purpose string (product_decision | requirement_confirmation | reflection | accepted_risk);
HumanActorID string; SourceRef string; InputDigest string;
SubjectKind string; SubjectID string; SubjectVersion int;
```

Version is 1, immutable; generated only from independently verified protected human receipt. Subject names exact confirmed decision/requirement/model or readiness risk subject. Product decision/requirement receipt binds the exact prospective resulting InputDigest computed before verification; reflection and accepted-risk evidence bind the assessed current digest. IDs and actor are service supplied. SourceRef is a non-secret retrievable statement handle; this is an authority receipt, not proof supplied by a model.

`protocol.DiscoveryReviewEvidence`:

```text
SchemaVersion; EvidenceID string; ProjectID string; Review RecordPointer;
Problem RecordPointer; InputDigest string; ReviewerActorID string;
AuthorActorIDs []string; SourceRef string;
Origin string (trusted_external_review | executed_independent_review);
ExecutionID *string;
```

Version 1, immutable. ReviewerActorID must be known and distinct from every AuthorActorID; authors derive from trusted input-change journal actors for the assessed input, not caller-provided arrays. SourceRef must retrieve actual completed review output. Origin executed requires nonempty execution ID emitted by the execution port; external origin requires a trusted local review-record attestation. RecordReview cannot manufacture executed origin. Review pointer must match SpecificationReviewResult and exact problem revision. Receipt existence is evidence provenance, not inference dispatch.

New payload `events.DiscoverySnapshotRecorded`:

```text
SnapshotID string; Revision int; RecordDigest string; InputDigest string;
ProblemModelID string; ProblemModelRevision int;
AmbiguityLedgerID string; AmbiguityLedgerRevision int;
OpenQuestions []DiscoveryOpenQuestion;
MaterialAssumptionsUnverified int;
ConfirmedRequirements int; ProposedRequirements int; ActiveProductDecisions int;
DecisionStatuses []DecisionStatusEntry;
ReadinessRef *RecordPointer; ReflectionRef *RecordPointer;
ReadinessBasisDigest string; ReadinessAssessedBasisDigest string;
```

`DiscoveryOpenQuestion{ID string; ResolutionAuthority protocol.ResolutionAuthority; ArchitecturalImpact protocol.ImpactLevel}` is sorted by ID. Optional absent problem/ledger use empty ID and revision 0 together, never one missing half. It implements RecordReferencing for exact DiscoverySnapshot version/digest. CheckReferencedRecord checks identity/digest/repeated claims available in snapshot JSON; the trusted Batch Postcondition resolves exact referenced documents and validates all derived counts/overlay/basis against transaction state. No alternate transaction or general-purpose referential engine is required.

Reducer behavior: validate snapshot revision progression and OpenQuestions against the per-entry open-state map; compare counts/problem identity against current discovery projection; then only store snapshot ID/revision/digest/input digest/ledger revision/readiness assessment digest pointers. Do not increment question/requirement/decision counts a second time. Add optional ProjectState.discovery fields `snapshot_ref` (RecordPointer), `ambiguity_ledger_revision` (int), `input_digest` (string), `readiness_input_digest` (string), `readiness_basis_digest` (string), `readiness_assessed_basis_digest` (string), `decision_statuses` ([]DecisionStatusEntry). Positive readiness is visible only when assessed input digest and assessed readiness-basis digest equal their current values and no unclosed blocking/conflicting current-input review exists. A snapshot with changed inputs clears exposed old readiness even if the problem revision stayed unchanged. Legacy state without these fields retains historical projection but is not sufficient evidence for new positive readiness.

### Existing DTO/event mapping and order

| Operation | Durable record(s) | Ordered state events before final snapshot |
| --- | --- | --- |
| ReviseProblem | ProblemModel at Revision | ProblemModelRevised, compact fields derived from record |
| ReviseLedger | AmbiguityLedger at Revision | AmbiguityResolved for changed terminal entries, then AmbiguityOpened for new/reopened entries, each sorted by ID |
| RecordProductDecision | ProductDecision at Version; authority receipt; changed ledger if resolving a question | ProductDecisionRecorded; it alone closes ResolvesAmbiguity (do not also emit AmbiguityResolved) |
| RecordRequirement | 1–64 Requirement versions; human receipts when confirmed | ordered RequirementRecorded members, one snapshot |
| RecordExperiment | DiscoveryExperiment at Version | DiscoveryExperimentStarted for running; DiscoveryExperimentCompleted for completed/inconclusive |
| RecordReview | SpecificationReviewResult; review evidence; changed ledger for gaps | AmbiguityOpened for supplied new material gaps, then SpecificationReviewCompleted with required digest |
| RecordReflection | authority receipt | final snapshot only; reflection never implies a model revision occurred |
| RecordReadiness | SpecificationReadiness at Version; risk evidence references | SpecificationReadinessRecorded, then final snapshot |

ReviewInput must include newly opened gap entries in an amended LedgerInput if Review.MaterialGapRefs names unknown questions; lacking them is invalid_argument. Add `Ledger *protocol.AmbiguityLedger` to ReviewInput for this purpose; review may only introduce new live gap entries, not close human questions. Decision resolution derives the new ledger from exact prior ledger and resolution; a missing prior ledger when a question is named is integrity. This avoids asking workers to choose between contradictory closure events.

A ReviseLedger operation needs verified protected human receipt for a HUMAN-authority terminal resolution; an AllowedActions entry cannot settle it. Resolved HUMAN questions require an active ProductDecision reference with matching human evidence, or direct human evidence explicitly tied to the entry; no engineering evidence alone. Reopening is allowed as new evidence, never as silent product override. Deferred entries require nonnil/nonempty Resolution (the reason for deferring) and a nonempty safe boundary; AmbiguityResolved.Resolution must equal the ledger Resolution exactly. Whitespace-only reasons/boundaries are invalid. A reason without a boundary or boundary without a reason fails before writes. Deferred entries additionally require matching protected-human-receipt-backed accepted-risk evidence for product tradeoffs. Non-human factual resolutions require nonempty retrievable EvidenceRefs.

Ledger live statuses are open/investigating/awaiting_human/researching/experimenting/consulting/reopened; terminal statuses resolved/explicitly_deferred. Initial ledger revision may contain only live questions; historical terminal entries cannot be invented without journal history. Metadata-only live changes need no opened event, but snapshot equality still validates live identity/authority/impact.

### Mutation algorithm

1. Verify immutable caller/project/exact-tool grant and Policy.Check before querying, writing or executing; operator-only methods require protected ingress and their internal nonwire action. Resolve exact prior state/snapshot using meta.ExpectedStateRevision, validate input DTOs and project equality; do not overwrite a contradictory project ID.
2. Reconstruct desired full current index from prior snapshot. On legacy-only histories, construct the first snapshot by reducing discovery events and resolving their exact record references; do not import unrelated/latest records. Missing ambiguity ledger may be repaired by explicitly supplied ledger whose live fields match the journal. Unresolvable legacy input yields integrity and no write.
3. Compute ordered entry diff and exact next immutable versions. Check referenced active product decisions and all authority evidence. Verify protected human receipts before constructing authority records; no policy action is a substitute. Derive per-entry events and all compact fields from documents.
4. Compute desired InputDigest, chronological DecisionStatuses and ReadinessBasisDigest. Preserve review/reflection receipts and all blocking review history; positive readiness carries forward only if assessed input/basis equal current input/basis and no unclosed current-input review block exists. Changed input must expose no positive current readiness.
5. Build final snapshot and snapshot event, validate documents/refs/counts. Call one ApplyBatch with expected revision and ordered members. Store each referenced document no later than the member referencing it. The final snapshot and any receipts may be stored on the final member; forward reference validation must not require receipts earlier than that member. Add the exact aggregate BatchGuard to Postconditions; WP1 invokes it after all members before commit using the scoped read view.
6. Return only committed BatchResult. No side effect before commit, no automatic retry on conflict. On success View immediately returns committed final state and exact snapshot. Cancellation before commit rolls back; cancellation after commit may mean response lost, never fabricate rollback.
7. Ambiguous response-loss retry is not automatically idempotent: caller rereads state/snapshot/history and compares exact submitted record digests. Reusing an old expected revision returns conflict. Request-key/idempotency behavior belongs to WP1; this slice must not invent it.

### Readiness and independent-review truth

`IndependentReviewPort.Review(ctx, BoundReviewRequest) (CompletedIndependentReview,error)` accepts immutable project/input digest/problem pointer/dimension and trusted author identities, and returns result plus trusted reviewer actor, source handle and execution ID. Port implementation must enforce independent context and privacy/spending policy; this WP defines/injects the port and tests fake ports only. A nil port or unavailable authorized endpoint is model_unavailable. No runtime is claimed delivered here. A completed port result is verified and persisted with RecordReview; if input changed during inference ApplyBatch conflict prevents stale completion becoming current. The external computation remains evidence outside the journal until recorded successfully.

BoundReviewRequest/CompletedIndependentReview exact fields are the values named above plus `Result protocol.SpecificationReviewResult` on completion. No user-supplied execution ID can enter ExecuteReview. Stable author/reviewer identity resolution and independent-port approval are **BLOCKED on WP2 and a later execution runtime contract**; trusted external review recording is separately allowed only with explicit local binding.

RecordReadiness validates the proposed fixed ReadinessChecks and their EvidenceRefs, then computes:

- Any incomplete check, open architecture-sensitive material question without exact-current protected accepted-risk evidence and safe boundary, unverified material assumption, missing current reflection or absent verified independent review coverage => not_ready.
- Any failing/conflicting current-input review without separately authorized closure forces not_ready immediately, even if another current-input PASS exists or the input digest stayed constant. A stale basis request conflicts with no write.
- Required independent specification review dimensions are completeness, ambiguity, contradiction, architecture_contamination, security_privacy, failure_modes, operations and ux_mental_model. Each must have exact-current-input review evidence, known independent identity and PASS verdict; no merely persisted result, unknown identity or unresolved material findings counts. Extra reviews/disagreements remain retrievable.
- Every complete check requires at least one resolvable evidence handle; not_applicable requires nonempty rationale and must not exempt independent_reviews or human_reflection. A qualitative assertion without evidence yields not_ready. External evidence resolution uses WP2 evidence policy; unknown/unresolvable is incomplete, not success.
- Each accepted_risk check requires protected-receipt-verified human risk evidence bound to current input and a nonempty boundary/rationale. Material product ambiguity may proceed only through that explicit accepted risk or safe deferral; it must not become a confirmed requirement. RemainingUnknowns with unsafe deferral or missing boundary force not_ready.
- If all required checks are supported, and at least one authorized accepted risk exists, ready_with_explicit_risks; otherwise ready_for_architecture. Store the computed verdict, never use caller Verdict to select behavior.
- Contradictory requested positive verdict returns invalid_argument rather than silently relabeling an assertion; callers may submit not_ready or the exactly computed verdict. Recording readiness is not permission to execute architecture; later lifecycle gating must inspect this exact current readiness evidence.

Open material question predicate matches existing projection: architectural impact medium/high/critical. Security/privacy critical issues must also make the corresponding check incomplete; absence from the architectural counter does not imply security readiness. Readiness qualitative truth/evidence-resolution semantics beyond these deterministic gates remain subject to independent contract review; do not implement unsupported fact inference. RecordReadiness must preserve limitations and accepted-risk evidence in assessment notes/refs. Tests must demonstrate the correspondence.

## Authority matrix

| Effect | Required trusted source/action authorization | Forbidden substitute |
| --- | --- | --- |
| Read discovery | project binding + discovery_state | unbound project argument |
| Propose/change investigation facts | exact mapped public tool grant plus Policy.Check + exact evidence | confirmed human status |
| Product decision/confirmed requirement/HUMAN resolution/reflection/risk | exact mapped tool/internal operator policy action plus verified protected human receipt and retrievable source | principal/MCP actor flags, LLM content |
| Record external review evidence | operator.record_specification_review and locally attested reviewer provenance | caller-provided independent=true |
| Execute review | review_specification plus independent port's resource/privacy/spending policy | record method or metered fallback |
| Persist computed readiness | record_specification_readiness + exact current evidence | confidence score or caller verdict |

Caller identity is not authority: the immutable binding decides allowed actions. Any unknown binding fails; human actor is never defaulted to operator.

## Missing / unknown / stale input semantics

| Input | Missing/unknown | Stale/contradictory |
| --- | --- | --- |
| Caller or action authorization | policy_denied, no write | policy_denied, no write |
| Expected state revision | invalid_argument | conflict, no auto retry |
| Required document/pointer/digest/version | invalid_argument; missing stored ref not_found/integrity as existing taxonomy dictates | integrity or conflict; never latest fallback |
| Legacy snapshot | View returns LegacyUnbound=true and available historical handles | positive readiness blocked until exact snapshot reconciled |
| Human receipt/source/identity | NEEDS_HUMAN; no write | NEEDS_HUMAN for unverifiable receipt; INTEGRITY for verified contradictory source |
| Review runtime | model_unavailable | conflict on changed assessed input |
| Review evidence/identity/coverage | not_ready; recording malformed evidence invalid_argument | stale evidence remains historical, does not satisfy current readiness |
| Experiment result/evidence | invalid_argument for completion without result; no inferred finding | exact versions required |
| Readiness proof | compute not_ready | requested unsupported positive verdict invalid_argument |

## Failure matrix

| Failure point | Required postcondition | Evidence |
| --- | --- | --- |
| Authorization/admission/input validation | no records/events/projection changes | exact classified error; before/after snapshots |
| Prior state changes between read/build and ApplyBatch | conflict and no partial write | concurrent-call deterministic test |
| Record or per-entry event validation | entire batch rollback, including earlier members | store counts/high watermark and reopened DB |
| Final snapshot aggregate/reference mismatch | entire batch rollback | deliberately divergent ledger/count/digest fixture |
| Process crash before commit | previous state reconstructs; no half-ledger revision | SQLite transactional fixture/reopen |
| Crash/response loss after commit | all records/events visible and replay agrees; stale retry conflict | reopen/reduce equality |
| Review-port error/cancellation | no ReviewCompleted/ready claim | port call/result and event count |
| Review completes but persistence conflicts/fails | no durable completion claim; preserve caller-returned failure | error + journal absence |
| Evidence-policy denies source retrieval | no disclosure; check incomplete/not_ready | denial category and bounded response |

## Representability map

| Requirement | State rule | Representation | Acceptance |
| --- | --- | --- | --- |
| R1,R9 | I1,I2 | WP1 ApplyBatch expected revision + final snapshot | A1,A2,A3 |
| R2,R4 | I4 | WP2 immutable caller + DiscoveryAuthorityEvidence | A4,A5 |
| R3,R5,R10 | I1,I7 | RecordPointer and DiscoverySnapshot; exact View refs | A6,A7 |
| R6 | I3,I7 | per-entry events + ledger record + snapshot aggregate checker | A2,A8 |
| R7 | I5,I6 | InputDigest, review/authority receipts, computed SpecificationReadiness | A9,A10,A11 |
| R8 | I6 | separate RecordReview/ExecuteReview and injected port | A10,A12 |

## Acceptance scenarios

| ID | Setup/action | Expected result and required evidence |
| --- | --- | --- |
| A1 | Current revision; create model, then ledger through separate guarded service mutations | one batch commit per mutation; exact record versions/digests; replay equals returned state (R1,I1) |
| A2 | Inject final ledger/event disagreement after valid earlier members | integrity; no records/events/high-watermark change, including after reopen (R6,I2,I3) |
| A3 | Two callers mutate same expected state revision | exactly one success, one conflict; no mixed snapshot (R1,R9,I2) |
| A4 | MCP arguments claim actor=human without protected receipt | NEEDS_HUMAN (or strict unknown-field INVALID_ARGUMENT); no confirmation/decision/receipt (R2,R4,I4) |
| A5 | Protected human receipt channel confirms proposed requirement and closes question | active decision/human receipt/requirement match; one closure effect and durable prior versions (R4,I4) |
| A6 | Fresh session/reopened DB reads snapshot | identical intent/index/questions/requirements and exact evidence handles without transcript (R3,R5,R10) |
| A7 | Legacy event-only project with optional-digest review | history still readable; LegacyUnbound; no positive readiness from unbound review; explicit reconciliation failure has no writes (R5,I7) |
| A8 | Reopen resolved entry then defer with bounded authorized risk | reopened event is replayable; immutable old ledger retained; deferred entry terminal and boundary retrievable; no double count (R6,I3) |
| A9 | All supported checks/current reflection/current independent coverage, then requirement or ledger mutation without model revision | positive readiness first; new digest invalidates it; historical assessment remains retrievable (R7,I5) |
| A10 | Self-authored/unknown identity review, stale review, missing dimension or merely recorded unattested result | no independent coverage; not_ready; executed origin cannot be spoofed (R7,R8,I6) |
| A11 | Proposed positive verdict with unsupported check or unbounded unknown; supported accepted-risk alternative | first invalid_argument/no write; authorized bounded risk produces ready_with_explicit_risks (R7,I5) |
| A12 | Nil execution port; then fake completed independent port while concurrent mutation occurs | first model_unavailable/no completed event; second conflict/no stale review completion claim (R8,R9) |

| A13 | Grant allowed but Policy.Check denies each public tool and operator-only operation | POLICY_DENIED before read/write/inference; no invented alias grants; protected operator ingress required (R2,R9) |
| A14 | Positive readiness, then failing/conflicting independent review of same InputDigest, followed by PASS | basis changes; positive verdict suppressed immediately; new PASS does not close old blocker; stale basis write rolls back (R7,I5,I8) |
| A15 | A confirmed; B supersedes A; later A explicitly reconfirmed in chronological journal | active counts 1, then 1, then 2; immutable A document never rewritten by B; snapshot overlay/replay match (R3,R6) |
| A16 | Deferred ledger entry has boundary but missing/blank Resolution, or mismatched AmbiguityResolved reason | INVALID_ARGUMENT/INTEGRITY; no writes; valid reason+boundary exactly retained (R6,I2,I3) |
| A17 | 64 requirement batch, last item invalid; then valid 64; 65-item request | failed batch writes none; valid batch commits one snapshot; 65 rejected before writes (R1,R9) |
| A18 | initialize_project on source-free project with explicit Principal actor, then repeated initialize | initial registration succeeds without adoption claim; repeat rejected; no human/operator default actor (R2) |

## Validation

Implementation evidence must record base/head, exact commands, exit status, schema results and scenario counts. Proposed commands (confirm repository targets before delegation): `go test ./internal/principal/discovery/... ./internal/controlplane/... ./internal/state/... ./internal/events/... ./internal/protocol/...`, race tests on concurrency scenarios, matching schema/API checks, and required repository health/documentation gates. Do not claim these executed in this planning task.

### Mutation Catalog (Mandatory)

| ID | Mutation to kill | Scenario |
| --- | --- | --- |
| M01 | Remove expected revision | A3 |
| M02 | Trust payload human actor | A4 |
| M03 | Use latest rather than exact record version | A6,A7 |
| M04 | Omit final aggregate Postcondition | A2 |
| M05 | Double-count snapshot questions | A8 |
| M06 | Treat optional-digest legacy review as verified | A7 |
| M07 | Allow self-review/unknown identity | A10 |
| M08 | Ignore ledger-only InputDigest change | A9 |
| M09 | Accept missing human reflection | A11 |
| M10 | Allow fabricated executed origin | A10 |
| M11 | Return completed review without execution port | A12 |
| M12 | Accept deferral without boundary | A8,A16 |
| M13 | Skip Policy.Check when binding grant exists | A13 |
| M14 | Use discovery.write alias instead of exact tool action | A13 |
| M15 | Keep readiness on same-input failing review | A14 |
| M16 | Let later PASS overwrite blocking review | A14 |
| M17 | Count active decisions from static Supersedes documents | A15 |
| M18 | Accept blank/mismatched deferral reason | A16 |
| M19 | Commit requirements separately/partially | A17 |
| M20 | Inherit initialize human/operator default | A18 |

### Required Independent Review Lenses (Dual-Lens Review Pack)

1. Protocol/authority: human confirmation, caller binding, epistemic states, independent provenance, readiness completeness, legacy interpretation and exact dependency contracts.
2. Persistence/state: transaction/member ordering, aggregate references, rollback/restart, concurrent CAS, replay determinism, input digest invalidation and version/schema compatibility.

Review immutable contract/candidate and deterministic evidence separately; author reasoning is not reviewer authority. Findings/closures must be durably recorded under the normal review process.

## Implementation Readiness Report

- Planning coverage: 10 requirements, 8 state rules, 18 acceptance scenarios, 20 numbered mutations; R1–R10 mapped; full DTO/algorithm and failure rules specified as planning proposals.
- Existing reusable facts: event/record vocabulary and reducers; new runtime/facade/snapshot/CAS interfaces are not claimed present.
- Blockers: final WP2 evidence/identity port reconciliation, protected operator ingress and blocker-closure contract, protected human-receipt issuance/isolation contract, authorized independent-review provenance contract, and independent review of these additive protocol/state changes.
- Runtime boundary: trusted review evidence can be persisted after policy/provenance closure; actual inference/scheduling remains absent unless separately supplied by an approved execution port. This is not an end-to-end discovery agent.
- State revision/digest binding for delegation remains to be recorded.
- Readiness: **NOT READY FOR IMPLEMENTATION**. Do not delegate code until blockers are closed in a revised complete contract; no placeholder TODOs authorize semantic invention.

### Weaker-implementer check

A competent Go implementer can follow the proposed ordering, representations and tests, but cannot invent protected receipt security boundary or dependency action/identity/evidence-resolution exports or independent runtime trust semantics. Those are Principal-owned blockers. Helpers/order implementation are local discretion; action authorization, authority, durability and review truth are not.

## Design and rationale

Considered (1) extending every legacy ambiguity event with ledger digest/version and migrating all historical meanings, and (2) keeping entry events and adding an immutable aggregate snapshot with exact references. Choose (2) because replay compatibility and one final consistency gate are explicit; it adds a reference index without changing old claims. Counter-risk: duplicated compact fields can disagree. The final snapshot record checker plus reducer equality check is mandatory, and the mutation catalog must prove their removal is detected. A snapshot is not permission to bypass entry lineage or treat raw record storage as completed workflow.

Escalate to the Principal for any dependency mismatch, unverifiable human/reviewer identity, unsupported legacy reconstruction, schema-version conflict or required readiness proof unavailable from existing durable evidence. Preserve exact contradiction evidence; do not choose a convenient fallback.

## Revision 2 closure record

ARC04/IF03: nine exact public tool/action/method mappings, required PolicyResolver, plural requirement atomic bound and operator-only nonwire actions. IF04: separate readiness basis and unresolved same-input review blocking. IF05: chronological decision overlay and reconfirmation scenario. IF06: required exact nonblank deferral reason plus boundary. These are contract amendments; independent re-review remains pending. Protected human/operator issuer, live review port and authorized blocker closure remain disabled prerequisites, not implemented features.
