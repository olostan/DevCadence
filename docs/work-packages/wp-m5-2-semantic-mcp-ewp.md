# WP-M5-2 — Semantic facade and no-argument stdio MCP

## Identity

- Revision: 2; task: task-m5-2-semantic-mcp.
- Base: bd6c424e292815460033b2570dce4d682ef5cb73; record accepted WP1 SHA at execution gate.
- Project state: no runtime project in repository authoring; capture implementation fixture prefix.
- Contract digest: immutable reviewed Git blob; not a self-referential inline hash.
- Endpoint: competent Go implementer, measured ≥32k admission for contract/clauses plus ≥8k reserves; unknown ceiling returns CONTEXT_UNFIT.
- Status: **DRAFT / NOT_READY**. This independently reviewable boundary slice does not deliver production task/review executors. M5 exit requires the follow-on runtime window.

## Objective

Provide actual state, proposal persistence and bounded evidence operations through a host-neutral application facade and thin MCP. Missing runtime ports deny without side effects. A lifecycle-record write or mocked executor is not successful task execution. Connection must work without the target repository in the principal workspace.

## Context Manifest

Role: facade/transport implementer. Read: WP1; controlplane query/apply; protocol WorkPackage, ValidationResult, ReviewResult, EvidencePacket; bounded artifacts/tools interfaces; schema and boundary tests. Targeted driver/validation signatures locate future ports, never authorize executor design.

Exact normative clauses: AGENTS §§2–9,12–15; MCP_API §§1–2,5–10; ADR-0016 §1 and **accepted 2026-09-23 evidence-tier amendment in full**; SECURITY §§3–9,14–17; ADR-0002 Decision §§4b–7a; ADR-0003 Decision; ADR-0004 Decision §2b; PROJECT_STATE §§7.1/7.3a; ADR-0024 §§1–8. Admit verbatim base DCI-010,011,013,014,018,019,025,031,032,033,052,054,055,080–084,090–093,107,120,122–124,130–135,159–161 clauses before affected action.

Evidence: base scout identifies ProjectState/Record/TaskDetail, ExecuteAndRecord and in-memory OperationManager; no Delegate/Accept/Reject or review runtime. Risks: authority, wire trust, schema compatibility, evidence exposure, async, secrets. Re-resolution: base/dependency/SDK drift, new provider/action, undeclared durable queue or port semantics. Deferred: live runtime and host tests.

## Semantic scope envelope

Authorized paths: internal/principal/ facade/DTOs/ports; internal/mcpadapter/; cmd/devcadence-mcp/; narrowly required WorkPackageProposed event/reference/reducer; principal schemas/fixtures/registry; MCP SDK go.mod/go.sum; Makefile binary target; tests/boundaries_test.go; MCP_API, PROTOCOLS, PROJECT_STATE and WORK_PACKAGES owning sync. WP3 owns discovery; WP4 host recipes.

Forbidden: raw Apply/event/SQL/filesystem/shell/network/pager/credential exposure; request-supplied actor/grants/policy; integration/merge; acceptance-policy weakening; automatic spend/fallback; fake production executors; host-specific core fields; durable scheduler; provider clients.

LOCAL_DISCRETION: private decomposition/names, test layout, SDK wrappers and fixed safe error text. Tool names, fields, caps and semantics below are MUST.

## Requirements and state rules

| ID | MUST requirement | Invariant |
| --- | --- | --- |
| R1 | Shared facade owns semantics; MCP strictly decodes, binds caller, dispatches and normalizes | I1: same authorized Go/MCP call has identical result |
| R2 | Protected launch binding fixes caller/project/actions/current policy | I2: model/host data cannot mint permission |
| R3 | Mutations use transactional CAS; proposal is distinct from approval | I3: stale/unapproved work starts no effects |
| R4 | Search/symbol compact direct evidence; content always worker-mediated | I4: no open-ended file/pager route |
| R5 | No-argument binary is stdio only; stdout exclusively protocol | I5: source-repository cwd unnecessary |
| R6 | Closed tool allowlist; no resources/prompts/sampling/roots raw backdoor | I6: unauthorized primitive absent and invocation denied |
| R7 | Process operation IDs carry instance identity and honest restart loss | I7: 10s yield is neither deadline nor durability |
| R8 | Acceptance is explicitly disabled in this preparation slice; later activation requires a separate accepted gate/provenance EWP | I8: record existence/confidence alone cannot accept |
| R9 | Missing executor gives MODEL_UNAVAILABLE before any effect | I9: partial facade is not M5 task execution |
| R10 | Owning prose/schema/fixtures/dependencies agree | I10: historical strict readers preserved |

## Verified facts

| Fact | Evidence at base | Verified by |
| --- | --- | --- |
| Typed canonical query and exact-version record lookup exist | controlplane/service.go: ProjectState, TaskDetail, Record | bounded scout |
| ApproveWorkPackage persists approval; not a readiness-review gate | controlplane/operations.go | bounded scout |
| Task/review events record outcomes, no production dispatcher | events/payloads_task.go; protocol review types; bounded inventory | scout |
| Accepted two-tier evidence amendment constrains conceptual MCP depth examples | ADR-0016 amendment; IMPLEMENTATION_PLAN M2.5 principal exposure | normative reconciliation |
| Official Go SDK documents stdio and protocol-version compatibility | official sources below, checked 2026-10-05 | research scout + Principal |

Step 0 re-verifies accepted dependency signatures. Contradiction blocks; no worker invention.

## Interface / algorithm contract

New request schemas use version 1.0, strict additionalProperties=false, closed unions/enums, snake_case JSON and WP1 CallMeta/CallerContext/WorkPackageRef/CandidateRef/SemanticError/OperationRef. Each request has meta. Each response has state_revision (observed or resulting committed prefix), evidence_refs array and exactly one result or SemanticError. Reads never claim freshness beyond observed prefix. Durable record inputs reuse existing strict schemas as nested records, never flatten unknown fields.

Proposed Go facade constructor:

~~~go
type Options struct {
    ControlPlane *controlplane.Service
    Evidence EvidenceReader
    Summaries SummaryReader
    Investigations Investigator
    Snippets SnippetWorker
    Tasks TaskExecutor
    Reviews ReviewExecutor
    Policy PolicyResolver
    Operations *OperationRegistry
}
func NewService(opts Options) (*Service, error)
~~~

Constructor refuses nil ControlPlane/Policy/Operations. Nil Evidence disables that operation; nil Snippets/Tasks/Reviews means MODEL_UNAVAILABLE. Production composition MUST NOT install fake executors/verifiers. NewService is application composition, not a remote configuration API.

Methods use (context.Context, CallerContext, Request) (Response, error), named ProjectState, Investigate, CreateWorkPackage, Delegate, TaskStatus, Validate, Review, RequestEvidence, Accept, Reject, RecordDecision. One method and DTO per tool; no arbitrary event dispatch. Go errors indicate internal/transport failures; ordinary semantic refusals are typed error responses, same in MCP.

Each row defines principal-<tool>-request/response.schema.json and corresponding named Go Request/Response types:

| Tool | Exact additional input fields | Result / effect |
| --- | --- | --- |
| project_state | focus optional enum project/task/discovery; task_id iff task; at_revision optional canonical revision | protocol.ProjectState or focused copied task/discovery projection with source revision; no history/source dump |
| investigate | question nonblank ≤2048 bytes; base_commit; scope_paths 1–16; max_bytes 1–8192 | Investigator receives exact question/scope/base/budget; nil port MODEL_UNAVAILABLE; real scout runtime separately accepted |
| create_work_package | work_package: protocol.EngineeringWorkPackage | immutable proposal ref/digest; existing task must be designing; no approval/ready/delegation |
| delegate | task_id; work_package: WorkPackageRef | accepted runtime port returns OperationRef; nil runtime gives MODEL_UNAVAILABLE, zero writes |
| task_status | exactly one task_id or operation: OperationRef | task id/alias/state/work_package?/candidate?/block_reason?/attempt_ids (newest 20, has_more); operation status + result/error handles, no logs |
| validate | candidate: CandidateRef; profile_id nonblank | accepted executor ValidationResult handle/OperationRef; absent runtime denied |
| review | candidate; unique dimensions 1–8 enum correctness/contract/architecture/security/test_adequacy/maintainability/performance/specification | independent ReviewExecutor handles/operation; absent runtime denied |
| request_evidence | closed evidence union below | bounded structured facts/worker-mediated content operation |
| accept | candidate; unique validation_ids 1–16; unique review_ids 1–16; reason nonblank ≤2048 | always NEEDS_PRINCIPAL with safe acceptance-runtime-unavailable reason; no event/merge; activation is M5-R2 acceptance-gate follow-on |
| reject | candidate; reason nonblank ≤2048; requested_repairs 0–32 strings ≤2048 | checked ChangeRejected; no automatic retry |
| record_decision | decision: protocol.DecisionRecord | checked immutable decision + existing event; no human product confirmation or policy/Git mutation |

All IDs/base/digests follow WP1. Task creation and approval remain trusted local operator services. A pre-created designing task is a valid demo setup. Do not invent a create-task MCP tool.

### Proposal persistence

New events.WorkPackageProposed fields: TaskID, WorkPackageID, Version, ProjectStateRevision, BaseCommit, RecordDigest, with strict reference checker matching existing EWP record and project. Validate task in designing, monotonic proposed versions, exact immutable identity. Does not replace approved tuple or modify lifecycle. Reducer may retain proposal refs privately; journal remains authoritative. New versioned event schema/registry/fixtures/docs sync required. Historical readers encountering new event refuse, never skip.

EWP.ProjectID matches caller/meta; planning ProjectStateRevision equals transaction-entry ExpectedStateRevision; task exists; accepted repository/base matches; record version new. No generated design or normalization. Duplicate immutable revision rejected. Separate approval reuses exact stored proposal digest and preserves its historical planning revision; it does not rewrite the record prefix to current state.

### Evidence union and caps

kind is required and closed. All requests return project/base/digest-bound citation, explicit resolution level/backend, facts versus interpretations and uncertainty. No raw URL/request command flags.

- summary: record_kind (closed allowed EvidencePacket/ValidationResult/ReviewResult/DecisionRecord; WP3 discovery types after its gate), record_id, record_version, digest, max_bytes. Return semantic summary and exact ref, never arbitrary raw durable JSON.
- search: base_commit, scope_paths (1–16 relative prefixes), query (literal ≤256 bytes), max_matches (1–20), max_bytes. Structured path/line/symbol matches only.
- symbol: base_commit, path, symbol, max_bytes. Syntactic backend honestly labeled.
- snippet: base_commit, path, start_line/end_line (1-based inclusive, ≤200 lines), reason nonblank ≤1024, optional hit_ref, max_bytes. hit_ref if supplied must match project/base/path; absence permitted only with explicit path+reason. SnippetWorker alone reads in authorized worktree. Hard serialized content cap 8192 UTF-8 bytes, further reduced by caller caps. Oversize gives CONTEXT_UNFIT and narrower-request advice; never silently truncate success.
- diff: candidate, path, reason, start_line/end_line, max_bytes. Worker-mediated bounded hunk with the same caps and immutable candidate/base.

max_bytes is 1–8192 and within binding cap. Path rejects absolute/drive/UNC/dotdot/control/NUL/symlink escape after canonical worker resolution. Root "." is allowed only as an explicitly permitted bounded search prefix, never a file read. Branch/HEAD strings cannot replace commit IDs. Cross-project/wrong digest handles denied before lookup/release. Check source policy before invocation and again before serialization. Untrusted content is data, never instructions.

No principal raw file/artifact depth despite conceptual MCP_API §5: accepted ADR-0016 governs. Authorized workers/local operator retain complete source evidence; principal can obtain relevant mediated snippets.

### Exact ports

~~~go
type AuthorizedTask struct {
    Caller CallerContext
    Meta CallMeta
    TaskID string
    WorkPackage WorkPackageRef
}
type TaskExecutor interface {
    Delegate(context.Context, AuthorizedTask) (OperationRef, error)
    Validate(context.Context, CallerContext, CallMeta, CandidateRef, string) (OperationRef, error)
}
type ReviewExecutor interface {
    Review(context.Context, CallerContext, CallMeta, CandidateRef, []string) (OperationRef, error)
}
type SnippetRequest struct {
    Meta CallMeta
    BaseCommit, Path, Reason, HitRef string
    StartLine, EndLine, MaxBytes int
}
type DiffRequest struct {
    Meta CallMeta
    Candidate CandidateRef
    Path, Reason string
    StartLine, EndLine, MaxBytes int
}
type SnippetWorker interface {
    Request(context.Context, CallerContext, SnippetRequest) (OperationRef, error)
    Diff(context.Context, CallerContext, DiffRequest) (OperationRef, error)
}
type EvidenceRecordRef struct { Kind, ID, Digest string; Version int }
type SummaryRequest struct { Meta CallMeta; Record EvidenceRecordRef; MaxBytes int }
type SummaryReader interface {
    ReadSummary(context.Context, CallerContext, SummaryRequest) (protocol.EvidencePacket, error)
}
type InvestigationRequest struct {
    Meta CallMeta
    Question, BaseCommit string
    ScopePaths []string
    MaxBytes int
}
type Investigator interface {
    Investigate(context.Context, CallerContext, InvestigationRequest) (protocol.EvidencePacket, error)
}
type EvidenceQuery struct {
    Meta CallMeta
    Kind, BaseCommit, Path, Query, Symbol string
    ScopePaths []string
    MaxMatches, MaxBytes int
}
type EvidenceReader interface {
    Read(context.Context, CallerContext, EvidenceQuery) (protocol.EvidencePacket, error)
}
type GateInput struct {
    Caller CallerContext
    Meta CallMeta
    Candidate CandidateRef
    ValidationIDs, ReviewIDs []string
}
// Reserved seam for a separately reviewed enabled acceptance contract, not activated here.
type CandidateGate interface {
    Check(context.Context, controlplane.BatchReadView, GateInput) error
}
type PolicyResolver interface {
    Check(context.Context, CallerContext, CallMeta, string) error
}
~~~

Dispatch is closed and exact: search/symbol → EvidenceReader.Read; summary → SummaryReader.ReadSummary; snippet → SnippetWorker.Request; diff → SnippetWorker.Diff; investigate → Investigator.Investigate. Each preserves all request fields and rechecks source/grants/output limits. SummaryReader resolves exact digest-verified record in batch project and constructs an allowed bounded semantic projection; unsupported record kind returns INVALID_ARGUMENT, never raw JSON. Snippet/diff results are process operations; a completed result retains exact source/candidate identity, never substitutes HEAD. Investigator must receive Question verbatim as untrusted data; no guessed extraction into a search query. Its production adapter is a follow-on scout runtime; nil denies rather than ignore Question. EvidenceReader is search/symbol only, no implicit model invocation.

SnippetWorker, Investigator, TaskExecutor and ReviewExecutor production implementations belong to accepted follow-on runtime EWPs, not LOCAL_DISCRETION. Nil ports are production default until accepted. Spies/fakes prove dispatch/denial only. The runtime owns current policy/CAS before effects, lifecycle/worktree/session/compiler/cancellation and durable outcomes.

### Enabled versus deferred acceptance

**Accept is hard-disabled in this EWP's production service and transport**, irrespective of grant, injected test gate or evidence supplied. After structural/project/action checks it returns NEEDS_PRINCIPAL with acceptance-runtime-unavailable reason, zero journal/storage/Git effects. No constructor flag, nonnil test fake or owner-edited JSON enables it. R8/A9/A10 verify this reduced slice. This is a disclosed temporary unavailable operation, not M5 acceptance delivery.

The next M5-R2 acceptance-gate EWP must close exact immutable acceptance policy and trusted producer evidence; current ReviewResult has only ReviewerProfile/ModelIdentity and current Attempt has only WorkerRole/Profile/ModelIdentity, so those are not stable independent ActorIDs. It must bind reusable protocol.ActorProvenance (ActorID/InvocationID/Role/LineageActorIDs) to candidate/attempt/review via versioned canonical evidence, not model-supplied names, and define unresolved finding/disagreement policy. The reserved CandidateGate seam above can use WP1 BatchReadView and Preconditions in the same transaction as ChangeAccepted; no public enabled API is promised until that follow-on contract is independently ready. Unknown producer, policy or closure evidence must deny. Full M7 campaign orchestration remains M7.

Reject remains an explicitly authorized checked ChangeRejected wrapper matching current reviewing candidate. Other states deny without a transition. Reject cannot start another attempt or increase its budget.

### Launch and trusted caller binding

No args starts stdio. Unexpected args: safe stderr, exit 2 before launch. Require nonempty DEVCADENCE_PROJECT_ID and absolute DEVCADENCE_HOME; optional absolute DEVCADENCE_PRINCIPAL_BINDING, otherwise fixed DEVCADENCE_HOME/config/principal-binding.json. Never discover config/database in cwd/source repo. Resolve ownership/canonical path, reject symlink and group/world writable file/parents; mode 0600 in private 0700 POSIX directory. Unknown ownership/ACL support blocks launch. This protects against unrelated users, **not same-OS-user agent processes**; human receipts need separately enforced authority.

Binding schema: binding_version exactly 1.0; CallerContext fields project_id/principal_id/allowed_actions/policy_ref/max_evidence_bytes/max_snippet_lines/source_depth; server-owned opaque DB/artifact/repository registration IDs. Actions closed base/discovery tool enum; empty grants allowed, unknown grants rejected. SourceDepth summary/symbol/snippet; caps 1–8192 bytes/1–200 lines. No raw credentials, model-provided override, policy/binding mutation tools or automatic fallback. The launch binding and its digest-pinned policy are immutable for that process lifetime. A changed file does not silently grant new authority; drift rejects subsequent actions and requires operator shutdown/relaunch. Grant revocation is enforced by terminating that bound process, not a false promise of cross-file/SQLite atomic revocation. Each operation checks PolicyResolver against the same immutable binding/policy identity; live resource/budget freshness belongs to the accepted executor. Policy file mutation cannot enable acceptance or human receipt authority.

MCP receives a copied immutable CallerContext from local launch composition. Journal actor uses protocol.Actor{Kind: protocol.ActorPrincipal, ID: PrincipalID}, never request actor. Existing events.Correlation has no generic CorrelationID. Preserve CallMeta.CorrelationID as facade operation lineage; do not map it to AgentRunID or assert unsupported journal field. New durable correlation representation is outside this slice unless explicitly amended.

Proposed SDK pin: github.com/modelcontextprotocol/go-sdk v1.7.0. Re-verify release/API/Go compatibility at readiness gate; unavailable => reviewed amendment, no silently chosen latest. Its documented support: 2026-07-28, 2025-11-25, 2025-06-18, 2025-03-26, 2024-11-05. Use SDK negotiation/backward initialize behavior; do not prescribe universal initialize for the newest revision.

No HTTP/OAuth/roots/client sampling/raw resources or prompts. Exact base tool set is eleven names in table. Canonical grants are the exact MCP tool names, not aliases. WP3 adds exactly initialize_project/discovery_state/record_problem_model/record_ambiguities/record_product_decision/record_requirements/record_discovery_experiment/review_specification/record_specification_readiness. Its internal trusted review/reflection record methods are not MCPtools or principal grants. PolicyResolver.Check(ctx, caller, meta, exactToolName) is required before all facade/discovery effects; grant presence alone is insufficient. Unknown tool/action rejects, never falls back to a broader discovery.write permission. Descriptions disclose unavailable runtime; successful connectivity never implies delegate works. Unauthorized raw names never registered regardless of grants.

Request body ≤1 MiB; complete response ≤32 KiB and caller evidence cap; oversize returns complete CONTEXT_UNFIT error, not malformed/truncated JSON. Semantic errors: isError=true + safe SemanticError in structuredContent and bounded JSON text compatibility result. Fixed messages; never provider/SQL/shell errors, environment values or secret paths. stdout valid MCP only; logs stderr.

OperationRegistry: ≤64 active/256 completed, process instance ID, 10s response-yield threshold independent from current policy deadline. Unknown deadline denies scheduling. Full registry denies new work, never evicts active operation. Old completed handle may expire => OPERATION_LOST. Different InstanceID/restart => OPERATION_LOST, no replay. EOF/cancel stops owned process-scoped operations; canonical task/attempt records are not invented or marked done. Follow-on recovery is separately specified.

## Authority and input semantics

| Effect | Authorized source | Forbidden substitute |
| --- | --- | --- |
| State/evidence | Binding grant + current source policy | Host icon, cwd, prompt |
| WP proposal | Grant + current prefix/task/base + schema | Automatic approval |
| Execution/review | Accepted runtime adapter + current deterministic authority | Non-nil fake, lifecycle event |
| Accept | Disabled until separately accepted M5-R2 gate/provenance contract | Grant, host approval, record existence, nonnil fake |
| Snippet | Explicit depth/locality + bounded worker | Direct read/pager |

| Input | Missing/unknown | Stale | Malformed/contradictory |
| --- | --- | --- | --- |
| Binding/action/policy | launch failure/POLICY_DENIED | re-resolve, deny until current | zero callbacks |
| Prefix/base/WP | INVALID_ARGUMENT/NOT_FOUND | WP1 stale error | strict reject |
| Runtime/scout/snippet port | MODEL_UNAVAILABLE, zero effects | invalidate | unsafe native access denied |
| Evidence/producers | NOT_FOUND/INTEGRITY/POLICY_DENIED | no acceptance/release | lineage mismatch denied |
| Operation instance | OPERATION_LOST | OPERATION_LOST | invalid argument |
| Deadline/capacity | POLICY_DENIED/CONTEXT_UNFIT | re-admit | no limitless default |

## Failure matrix

| Failure | Required postcondition | Evidence |
| --- | --- | --- |
| Decode/binding denied | no callback/write/network | spy count zero |
| CAS/gate refusal | unchanged records/events/prefix | transactional fixture comparison |
| Missing runtime | no attempt/worktree/session | unchanged task and registry |
| Worker wrong identity/size | no result release/acceptance | rejection + private failure artifact |
| Response cap exceeded | complete typed refusal | independent JSON parser |
| Restart/EOF | operations lost/cancelled, no fabricated durable completion | old handle denied |
| Commit response lost | inspect durable refs before retry, no external replay | stale same-prefix repeat |

## Representability and acceptance scenarios

| ID | Setup → action → expected | Requirement → invariant → representation |
| --- | --- | --- |
| A1 | source-free cwd + binding → no-arg launch/project_state → works, stdout protocol only | R1/R5 → I1/I5 → facade/SDK |
| A2 | wrong project/actor/grants/unknown key → call → refused, zero callbacks | R2 → I2 → binding/schema |
| A3 | stale prefix/base → WP proposal → no record/event | R3 → I3 → ApplyBatch |
| A4 | valid proposal → task query → designing, no approved tuple | R3 → I3 → WorkPackageProposed |
| A5 | raw file/pager/shell/SQL/resources → list/call → absent/denied | R6 → I6 → registry |
| A6 | scoped search then snippet → direct compact facts, bounded worker-mediated content | R4 → I4 → ports |
| A7 | escape/cross-project/wrong hit/201 lines/8193 bytes/full file → deny before release | R4 → I4 → unions/caps |
| A8 | nil Tasks/Reviews → delegate/validate/review → MODEL_UNAVAILABLE, unchanged task | R9 → I9 → ports |
| A9 | failed/missing/self review/wrong lineage → accept → denied, no accepted event | R8 → I8 → hard-disabled acceptance |
| A10 | apparently passing records/accept grant/non-nil test gate → accept → still NEEDS_PRINCIPAL, zero storage/Git effects; no hidden enable path | R8 → I8 → disabled constructor/dispatch |
| A11 | operation yields at 10s → poll same ID; restart → lost, no replay | R7 → I7 → instance registry |
| A12 | bad frame/revision/cancel/EOF → bounded protocol failure/shutdown, no stdout logs | R5/R7 → I5/I7 → SDK |
| A13 | hostile raw provider error contains secret/source → normalization → no leaked text | R2/R6 → I2/I6 → SemanticError |
| A14 | same Go/MCP call + historical fixtures → semantic equality/readability | R1/R10 → I1/I10 → facade/strict schemas |

A9/A10 test the preparation slice's denial contract. Future successful acceptance is a new follow-on EWP scenario, not counted as this slice coverage. Likewise mock task/review calls do not count as live execution.

| ID | Scenario | Traceability |
| --- | --- | --- |
| A15 | summary identity/digest, diff candidate, and investigation Question differ → spies observe exact distinct inputs; missing corresponding port denies | R1/R4 → I1/I4 → complete separate DTOs/ports |
| A16 | each of nine discovery action grants in isolation → only mapped tool reaches service; internal reflection/review-record names never callable by MCP | R2/R6 → I2/I6 → canonical action table |
| A17 | policy identity drifts during accepted transaction precondition or binding file changes → effect denied; stale bound process never gains grants | R2/R3 → I2/I3 → immutable policy binding/BatchGuard |

## Validation and Mutation Catalog

Future implementation commands: go test -count=1 -race ./internal/principal/... ./internal/mcpadapter/... ./cmd/devcadence-mcp/... ./tests/... ; make schemas ; make docs-check ; make verify. Re-resolve package paths after creation. Official SDK client runs no-arg server in a temporary source-free directory; capture negotiated versions/tool list, Go/SDK/base/head and exact outcome. Real host smoke belongs WP4.

Mutation review sufficient; independent reviewer must observe failing deliberate mutants:

| Mutant | Scenario that must fail |
| --- | --- |
| Trust supplied grants/actor | A2 |
| Proposal approves | A4 |
| Raw pager/resource fallback | A5 |
| Principal reads snippet directly | A6 worker spy |
| Ignore base/symlink/depth | A7 |
| Append delegation on nil runtime | A8 |
| Any acceptance success hidden behind supplied grant/fake | A9/A10 |
| Accept integrates / ignores transaction | A10 |
| Drop InstanceID/replay on restart | A11 |
| stdout diagnostic / universal latest initialize | A1/A12 |
| Leak provider errors | A13 |
| MCP-only policy enforcement | A14 |
| Drop summary digest/diff candidate/investigation question | A15 |
| Alias broad discovery.write to all tools | A16 |
| Mutable policy file silently grants / transaction guard skipped | A17 |

Clean dual implementation reviewers: Contract/Authority and Test Adequacy/Mutation. Focused independent verification after material repair; no author self-verification.

## Escalation, rationale and readiness

Stop for proposal-version gaps, SDK bound inability, unsupported port enforcement, a hidden acceptance enable path or any attempt to implement deferred runtime within LOCAL_DISCRETION. Selected approach: thin official SDK + application facade. Viable alternative: per-tool JSON CLI bridge; cheaper reuse but generic CLI events and subprocess/stdout/caller binding make it harder to enforce common authority and CAS.

**NOT_READY:** dependency signature/current-base/independent review and contract admission gates remain pending. Proposed requirements 10/10 map to 17 scenarios; acceptance/runtime/producer-policy enabled implementation is explicitly outside this reduced slice and remains a M5-R2 prerequisite. No full execution readiness claimed.

Official sources verified 2026-10-05: https://github.com/modelcontextprotocol/go-sdk ; https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio . SDK compatibility is not installed-host compatibility.

## Changelog

- r1: preparation facade/stdio boundary, explicit absent-runtime denial, proposal/approval distinction and accepted mediated-content rule.


- r2: consolidated ARC-01/03/04 and IF01–03 repair; complete evidence dispatch, exact discovery grants, transaction read-guard seam, and explicitly disabled acceptance until canonical producer/policy contract exists.
