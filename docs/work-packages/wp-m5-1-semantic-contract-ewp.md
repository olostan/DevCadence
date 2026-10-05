# WP-M5-1 — Principal identities and transactional freshness boundary

## Identity

- Revision: 2; Task: `task-m5-1-semantic-contract`.
- Base commit: `bd6c424e292815460033b2570dce4d682ef5cb73`.
- State revision: repository authoring has no runtime project; implementation MUST record the fixture/project prefix used.
- Contract identity/digest: immutable Git blob of this document at reviewed candidate; do not insert a self-referential digest.
- Endpoint: competent Go implementer, configured ≥24k context with measured admission for this entire contract, exact clauses and ≥6k implementation/output reserves. Unknown ceiling => CONTEXT_UNFIT; no hidden frontier-only assumption.
- Status: **DRAFT / NOT READY until independent review and dependency/current-base gate**.

## Objective

Add an exact host-neutral principal identity vocabulary and a transactional batch/CAS API. This EWP does not implement MCP, a task executor, host configuration or approvals. It prevents stale or mismatched principal mutations from changing records, events or projections.

## Context Manifest

- Role: Go contract implementer; read: controlplane transaction/query APIs, state task lineage/reducer, storage transaction API, protocol record validation and schemas, events reference validation, tests/boundaries.
- Write envelope: below; domains: principal wire identity, trusted caller binding, transactional guarded persistence; risk tags: authority, TOCTOU, compatibility, crash/rollback, WP freshness.
- Exact normative clauses: AGENTS §§2,6–9,12–15; ADR-0002 Decision §§2–7a, ADR-0003 Decision §§1–7, ADR-0004 Decision §§2/2a/2b, ADR-0005 Decision; ADR-0024 §§1–8; PROJECT_STATE §§7.1/7.3a; MCP_API §§8–10; SECURITY §§3,7,14–17.
- Exact DCI texts to admit from base INVARIANTS: DCI-019,025,031,032,052,053,054,080,081,090,091,092,093,123,124,131,134. Their full clauses, not titles, MUST be resident before affected actions.
- Initial evidence: controlplane `Apply`, `Command`, `Result`, `loadProjection`; `WorkPackageApproved.CheckReferencedRecord`; state `StateRevision`, `applyWorkPackageApproved`, `applyTaskDelegated`, `applyAttemptStarted` (bounded scout at base).
- Assumptions: SQLite transaction replay and current record reference checks remain reusable. Re-verify step 0. Unknown policy grants do not count as authority.
- Deferred: worker scheduling/host/MCP implementation. Re-resolve for a changed storage API, invariant/catalog edit, new credential path, unexpected task lineage representation, accepted base change or context overflow.

## Scope envelope

Authorized: `internal/principal/contract*.go` (pure identities/validation), `internal/controlplane/` guarded batch API and tests, narrowly necessary read-only task lineage accessors in `internal/state/`, `internal/storage/` write-intent acquisition and one additive serialization-row migration (never edit shipped migrations), `schemas/principal-*.schema.json`, matching `fixtures/protocol/principal-*`, schemas registry/README, `tests/boundaries_test.go`, and owning docs MCP_API/PROTOCOLS/PROJECT_STATE/WORK_PACKAGES. New private helper/test files inside these domains are allowed.

Forbidden: change lifecycle edges, old event meanings, immutable-record keys, acceptance/integration authority, global policy, direct storage SQL outside storage, actor/credential grants inferred from requests, executor/job schemas, automatic old-record rewriting, new network dependency or weakening schema/health checks.

LOCAL_DISCRETION: helper names, private structs, test file decomposition, table-driven tests. Public names/shapes and failure semantics below are MUST; request field names use snake_case.

## Requirements and invariants

| ID | Strength | Requirement | Invariant / source |
| --- | --- | --- | --- |
| R1 | MUST | Define and validate the exact common identities below; strict new wire schemas | I1: an identity never embeds host/provider authority; DCI-054/090–093 |
| R2 | MUST | Check full expected prefix inside the write transaction before any write | I2: a refused stale call commits zero records/events/projection changes; DCI-131 |
| R3 | MUST | Batch stores records, validates references and reduces each event in order, then commits once | I3: no visible partial lifecycle/discovery transition; ADR-0002 |
| R4 | MUST | Optional WP guard checks exact current approved tuple and task-scoped approval freshness | I4: a historical or superseded WP cannot start an attempt; DCI-032 |
| R5 | MUST | Caller actor/grants come from local protected binding; requests carry neither | I5: data cannot mint permission; DCI-080/123/124 |
| R6 | MUST | Keep old Apply and historical durable records readable without changing their semantics | I6: new principal routes cannot bypass guards; DCI-093 |
| R7 | MUST NOT | Treat unknown, absent or malformed current state/record/reference as fresh | I7: integrity/permission failures precede effects; DCI-019/092 |

## Verified facts

| Fact | Evidence at base | Verified by |
| --- | --- | --- |
| Apply writes one event plus arbitrary typed records atomically; it has no expected prefix | `internal/controlplane/service.go`: Command, Apply; guard belongs after loadProjection inside store.Write | bounded scout |
| Revision is formatted from journal high watermark, not wall time | `internal/state/projection.go`: StateRevision; ADR-0005 | scout and normative read |
| Task/WP approval and attempt lineage already reject inconsistent IDs/versions | `internal/state/reduce.go`: named apply methods; events/records.go approval checker | bounded scout |
| TaskDelegated lacks WP version and is separate from AttemptStarted | `internal/events/payloads_task.go` | bounded scout |
| Generic stored record validates schema, digest and project scope | ADR-0002 Decision §§4b–4d; storage/service transaction inspection | bounded scout |

Step 0 re-verifies signatures and reference guards without reading entire packages. Contradiction => stop and amend, not reinterpret.

## Exact interfaces, wire schemas and algorithms

New `internal/principal` vocabulary (types are proposed additions, not existing APIs):

```go
type CallMeta struct {
    SchemaVersion string // exactly "1.0"
    ProjectID string
    ExpectedStateRevision string // required for mutations; forbidden for bootstrap initialize
    CorrelationID string // nonempty <=128 bytes, no controls; not an idempotency promise
}
type WorkPackageRef struct { ID string; Version int; Digest string; BaseCommit string }
type CandidateRef struct { TaskID string; AttemptID string; WorkPackage WorkPackageRef; Commit string }
type CallerContext struct {
    PrincipalID string
    ProjectID string
    AllowedActions []string
    PolicyRef string
    MaxEvidenceBytes int
    MaxSnippetLines int
    SourceDepth string
}
type SemanticError struct { Code string; Message string; EvidenceRefs []string; Retryable bool }
type OperationRef struct { ID string; InstanceID string; Kind string; Status string }
```

JSON schema filenames: `principal-call-meta`, `principal-work-package-ref`, `principal-candidate-ref`, `principal-semantic-error`, `principal-operation-ref` (each `.schema.json`). CallerContext is internal **not a model-supplied wire object**. Every optional wire field explicitly omitted when unasserted; required arrays are `[]`. Digest = `sha256:` plus 64 lower-case hex; commit = 40 or 64 lower-case hex Git object ID, length must match the registered repository's hash format. IDs nonblank ≤128 UTF-8 bytes, controls forbidden; validate, never silently trim. WP Version ≥1. Principal schema family version is independent of durable protocol versions.

CallMeta required fields: schema_version/project_id/correlation_id; expected_state_revision required by mutation wrappers, no arbitrary string coercion. State syntax matches canonical `ps_` followed by at least nine decimal digits; canonical comparison uses generated full string. `ps_000000000` is empty-prefix identity for initialization where permitted. Future/foreign prefix is not fresh. Do not lexically order revisions.

Error Code closed enum: `INVALID_ARGUMENT`, `UNSUPPORTED_SCHEMA_VERSION`, `NOT_FOUND`, `INTEGRITY`, `STALE_PROJECT_STATE`, `STALE_WORK_PACKAGE`, `POLICY_DENIED`, `VALIDATION_FAILED`, `REVIEW_DISAGREEMENT`, `NEEDS_PRINCIPAL`, `NEEDS_HUMAN`, `MODEL_UNAVAILABLE`, `CONTEXT_UNFIT`, `CONTRADICTED_ASSUMPTION`, `OPERATION_LOST`, `CANCELLED`, `INTERNAL`. Safe fixed messages; raw provider/shell/SQL errors forbidden. Error EvidenceRefs contain only authorized project-scoped immutable handles. Errors are not trusted authority statements.

Operation Kind closed enum `investigate|delegate|validate|review|snippet|review_specification`; Status `queued|running|completed|blocked|failed|cancelled|lost`. Process-scoped handles explicitly carry InstanceID. No durability claim in this EWP.

New controlplane API:

```go
type WorkPackageGuard struct {
    TaskID string
    WorkPackageID string
    Version int
    Digest string
    BaseCommit string
}
type BatchCommand struct {
    ProjectID string
    Actor protocol.Actor
    Correlation events.Correlation
    ExpectedStateRevision string
    WorkPackage *WorkPackageGuard
    Preconditions []BatchGuard
    Postconditions []BatchGuard
    Commands []Command
}
type BatchReadView interface {
    ProjectState() *protocol.ProjectState
    Record(context.Context, string, string, int) (storage.StoredRecord, error)
}
type BatchGuard interface { Check(context.Context, BatchReadView) error }
type BatchResult struct { Results []Result; ProjectState *protocol.ProjectState }
func (s *Service) ApplyBatch(ctx context.Context, cmd BatchCommand) (BatchResult, error)
```

Batch guards are trusted in-process application callbacks, **never JSON/wire fields**. Up to eight preconditions and eight postconditions, nonnil entries; unknown condition rejects. ReadView methods resolve only the batch project with digest/schema checks, return copied values/bytes, and cannot write, execute tools, consult networks or mint authority. View lifetime ends when Check returns; retaining/using it afterward fails InvalidTransition. Guards must have no external effects; panic is a rollback/Internal failure, never suppressed. Check failure aborts whole transaction. WP3 final aggregate check uses a postcondition; any future enabled acceptance must use a precondition for its already-stored evidence/policy. This EWP does not itself establish an acceptance policy or producer authority.

Command/Result retain existing shape. All batch members must have same project, Actor and Correlation as parent; reject disagreement, do not silently overwrite. `Commands` length 1–256, each existing typed Payload nonnil, aggregate serialized records/payloads ≤1 MiB. No nested batch. Duplicate record identity in the batch is rejected even if byte-identical; references may name existing immutable records. Validate original typed/schema bytes using existing store policy.

Typed errors: `controlplane.ErrStaleProjectState`, `ErrStaleWorkPackage` (new exported sentinels wrapped as existing `errs.CategoryConflict`); facade checks errors.Is before generic category mapping. Do not globally change error taxonomy or reinterpret every conflict as stale.

Algorithm MUST:

```text
validate batch structural bounds, all member identity and context cancellation
store.Write(transaction):
    acquire SQLite write intent BEFORE any projection/record query
    load current projection under the same write transaction
    compare ExpectedStateRevision exactly with its generated current revision
    mismatch -> ErrStaleProjectState, no writes
    if WP guard present:
        resolve task; find latest approved WP ID/version from canonical lineage
        resolve digest-checked immutable WP record, compare ID/version/digest/base
        locate approval event and task-significant events since approval
        stale if approval absent, task not READY, approved tuple differs,
          task moved/revised/blocked/resumed/candidate produced since approval,
          or registered repository accepted base differs from WP base
        mismatch -> ErrStaleWorkPackage, no writes
    run Preconditions in order on copied/read-only project-bound tx view
    for member in declared order:
        persist all its records using existing strict boundary
        validate every referenced record/digest/compact fact
        append event with server identifiers/time
        apply event against working projection (all previous batch events included)
        rejection -> roll back everything
    run Postconditions in order on final working projection/read-only tx view
    persist final projection and commit once
return Results in event order plus final ProjectState
```

### SQLite serialization / bounded contention

An ordinary deferred BeginTx followed by a read is insufficient across connections. Add a storage-owned write-intent sentinel in a new migration: table `principal_write_serialization(id INTEGER PRIMARY KEY CHECK(id=1), marker INTEGER NOT NULL CHECK(marker=0))` with one immutable-lifetime row `(1,0)`. Before any read inside guarded batch transaction, execute `UPDATE principal_write_serialization SET marker=marker WHERE id=1`; this acquires SQLite write intent without changing project truth. Exactly one row must exist/match; missing/corrupt row is Integrity. Never expose this table as domain state and never delete/recreate it as recovery. Existing read-only commands remain write-free; only guarded batch takes this lock. Legacy writers still serialize through SQLite; guard reloads after lock acquisition.

BUSY/LOCKED before commit rolls back the whole attempt and may retry the **complete transaction** at most five attempts under a total two-second context budget, with deterministic 10/25/50/100ms delays and fresh projection/guard reads each attempt. No retry after uncertain commit; no model invocation or external effect in callbacks. Use storage typed recognition of BUSY/LOCKED, never string matching arbitrary error text. Cancellation/deadline returns existing context error, unchanged; exhausted contention returns new sentinel ErrStorageBusy wrapped CategoryConflict, not falsely STALE_PROJECT_STATE. Derive deadline as min(caller deadline, now+2s); waiting is cancellation-aware. Per-connection SQLite busy waits must obey that deadline; if driver cannot guarantee it, readiness blocks pending exact storage support.

A2's tiny successful competing transactions must finish within the declared contention budget: first obtains lock/commits, second obtains lock afterward/reloads and returns exact stale error. A separate long-held-lock scenario requires bounded ErrStorageBusy/cancel without writes. Do not promise stale on a cancelled or exhausted lock wait. New guards execute only after serialized current reads; postcondition failure proves complete rollback.

WP guard is exclusively the **start-execution** guard; accept/reject/validation/review check current candidate lineage separately, so a running/reviewing task is not mistakenly required READY. No global-prefix equality with the historical WP.ProjectStateRevision at delegation: approval itself advanced the prefix. The planning revision is validated at creation against pre-write state, then stored unchanged. Other-task/discovery events may advance the global prefix but do not make this WP task-significant-stale. New caller state must still pass prefix CAS.

Read task-significant history from the authoritative journal within the transaction; do not make up a wall-clock freshness threshold. No durable schema change for an approval epoch is required. Missing repository readiness/accepted base on repository-backed task blocks execution; Day-0 projects can persist discovery with no repository. WP1 does not introduce or waive M6 Adoption Readiness.

Actor is server binding identity. Binding provisioning is outside this EWP; WP2 must supply it. Direct Go trusted callers still supply Actor as today; model-facing entry points cannot invoke generic Apply/ApplyBatch. A failed batch leaves no partial receipt. IDs reserved/generated before rollback may be skipped; never reused to fabricate success.

Commit response ambiguity: an error before commit is guaranteed unchanged. If driver reports commit error whose completion cannot be established, return existing integrity/internal failure with safe evidence/correlation; caller queries durable events/records before retry. Since this EWP does not add dedup storage, Correlation is only correlation. Repeating old ExpectedStateRevision after a successful commit fails stale. Fresh-prefix replay is a **new mutation** and must be rejected by semantic operation/record identity guards where effects are nonrepeatable.

## Authority matrix

| Effect | Authorized source | Forbidden substitute |
| --- | --- | --- |
| Caller identity/grants | Local protected launch binding, WP2 | Tool arguments, prompt, host label |
| Batch mutation | Typed semantic service plus valid current policy and CAS | Any raw event tool |
| Start execution | Approved WP exact tuple + current task/base + executor authority | Matching name alone, historical approval |
| Acceptance/integration | Existing checked evidence/decision semantics; integration separate | CAS success by itself |

## Input and failure semantics

| Input | Missing/unknown | Stale | Malformed/contradictory |
| --- | --- | --- | --- |
| Current projection | NOT_FOUND or INTEGRITY, unchanged | reconstruct current prefix; never cached comparison | integrity error, no effects |
| Expected prefix | INVALID_ARGUMENT, no effects | STALE_PROJECT_STATE | INVALID_ARGUMENT |
| WP tuple/base/approval | STALE_WORK_PACKAGE or NOT_FOUND, no effects | STALE_WORK_PACKAGE | INVALID_ARGUMENT/INTEGRITY |
| Policy/actor/grant | POLICY_DENIED at facade; batch requires valid typed actor | policy recheck before effect | POLICY_DENIED |
| Member record/reference | existing strict refusal | exact version/digest only | unchanged batch rollback |

| Failure | Postcondition | Recovery / evidence |
| --- | --- | --- |
| Before transaction / cancelled context | no writes | typed failure |
| After member records, before event | all batch writes rolled back | fault-injection transaction test |
| After earlier event, later member rejects | previous records/events/projection unchanged | compare prefix and store keys |
| Concurrent calls at same prefix | exactly one commits; other stale | barrier test on two services/connections |
| Contention deadline exhausted / cancelled | no logical project write | bounded ErrStorageBusy / context error, no implicit success |
| Commit succeeds, response lost | committed prefix retained, no inferred failure rollback | lookup by records/event correlation; old-prefix replay denied |
| Restart | full journal rebuild equals committed final projection | byte-identical projection test |

## Traceability and acceptance scenarios

| Scenario | Setup → action → expected observation | Requirement → invariant → representation |
| --- | --- | --- |
| A1 | Invalid enum, trailing JSON, unknown key, schema version, bad digest → decode → reject before callback | R1 → I1/I7 → strict principal schemas |
| A2 | Two independent services share DB/prefix → concurrent batches → one success, one exact stale error; one event set | R2 → I2 → transactional ExpectedStateRevision |
| A3 | Two lifecycle events; second illegal → batch → no records/events/projection committed | R3 → I3 → ApplyBatch |
| A4 | WP same name wrong version/digest/base → guarded start → STALE_WORK_PACKAGE; no attempt | R4 → I4 → WorkPackageGuard + stored record |
| A5 | Unrelated task event advances state → fresh-prefix start with unchanged approved WP → succeeds; old-prefix call denied | R2/R4 → I2/I4 → task-significant journal check |
| A6 | Approval → block/resume/reapprove newer WP → old tuple start → denied | R4 → I4 → approval lineage |
| A7 | Record write/reference mismatch on final member → batch → rollback all; restart equals pre-state | R3/R7 → I3/I7 → reference validation/reducer |
| A8 | Model supplies actor/grants fields → wire decode → unknown-field refusal; trusted binding remains unchanged | R5 → I5 → internal CallerContext |
| A9 | Existing fixtures/old Apply callers → schema/decode/queries → historical behavior preserved | R6 → I6 → unchanged old types/readers |
| A10 | Success response dropped → same-prefix repeat → stale; observed committed records retrievable | R2/R3 → I2/I3 → batch prefix/correlation |
| A11 | Trusted precondition/postcondition rejects after valid members → batch → all writes rolled back; retained view unusable | R3/R7 → I3/I7 → BatchGuard/BatchReadView |
| A12 | Write lock held beyond budget or caller cancels → batch → bounded busy/cancel, no project changes; read-only command takes no lock | R2/R7 → I2/I7 → sentinel/deadline/retry |

## Validation and mutation catalog

Required implementation checks: `go test -count=1 -race ./internal/controlplane/... ./internal/state/... ./internal/principal/... ./tests/...`; `make schemas`; `make docs-check`; `make verify`. Capture actual commands/status, Go version, base/head, fixture counts and fault-injection results. Current preparation PR runs only repository checks; these future package commands must be re-resolved after packages exist.

Mutation review sufficient with deliberate local mutants, no new mutation dependency required. Independent Test Adequacy reviewer must observe each failing scenario, not merely read the table.

| Mutant | Required failure |
| --- | --- |
| Compare prefix before entering store.Write | A2 admits two writers |
| Skip final member rollback / persist projection early | A3/A7 detects partial state |
| Ignore WP digest/version/base | A4 fails |
| Compare WP planning prefix to global prefix after approval | A5 valid start fails |
| Treat block/resume as unrelated | A6 old start accepted |
| Trust actor/grants in JSON | A8 fails |
| Suppress strict record/reference checks | A7/A9 fails |
| Omit write-intent before reads / retry only append | A2/A12 exposes contention misclassification or partial state |
| Ignore or retain transaction guard read view | A11 fails |
| Treat Correlation as dedup without durable evidence | A10 cannot resolve durable success correctly |

Dual implementation lenses: Contract/Authority and Test Adequacy/Mutation, clean immutable candidate packs. No author self-verification.

## Escalation, rationale and readiness

Stop for an unrepresented task freshness rule, missing store transaction query seam, unsafe commit ambiguity, state/protocol/invariant conflict, new dependency, unreconciled base or contract overflow. Guards MUST remain application/transaction semantics, never MCP-only checks.

Compared approach A (selected): transactional expected-prefix batch, versus B: read state then lock in facade and invoke Apply twice. B is viable only with a single writer and no crashes; it cannot enforce two connections or rollback the two events together. Selected design is broader in the trusted service but has explicit limits and tests.

Readiness: **NOT_READY (independent review/current-base gate pending)**. Requirements represented 7/7; scenarios 12/12; architectural choices inside this slice proposed and explicit. The unanswered live endpoint/provider/spend choices are outside this slice, not permission defaults. Freeze only after independent reviewer verdict and exact admission check. No implementation authorized by this draft.

## Changelog

- r1: initial contract and batch/freshness design; preserved historical WP planning revision and separated start guard from candidate operations.

- r2: consolidated transaction-guard and SQLite contention repair: bounded read-only transaction guards, early SQLite write-intent serialization and cancellation-aware contention semantics.

## Implementation record

Implemented from base `2c0a3d4` (the EWP base `bd6c424` plus the documentation-only M5 preparation commits) in the working tree, **without commit**. Authorization: the repository owner explicitly authorized implementation as a disclosed gate exception while the header status remained DRAFT/NOT_READY; no independent Contract/Authority or Test Adequacy review had run. This record does not change the contract above.

Interpretations and deviations, each needing reviewer confirmation:

1. Prefix comparison for an uninitialised project uses the empty prefix `ps_000000000`; the first event must still be `ProjectInitialized`.
2. "Task-significant events since approval" is implemented as any journal event correlated with the task after its latest `WorkPackageApproved` event (strictest reading; no state-specific allowlist). Consequently any task-correlated event after approval makes the guard stale: delegating and starting in separate calls is stale, so delegation and start must be in one batch. Whether to add a state-specific allowlist is an owner decision, pending.
3. "Repository-backed" means the projection has a non-empty `RepositoryPath`; the accepted-base comparison is skipped for projects with none. The registered repository's hash format cannot be consulted from the control plane, so commit equality is by exact string; the wire `ValidateCommit` accepts 40 or 64 hex and callers must match the repository's format.
4. The contention budget bounds only the wait for the connection and the write lock; the transaction body and commit run under the caller's context so a long journal replay is never cut off by the 2 s budget. Per-attempt busy waits are set with `PRAGMA busy_timeout` on the dedicated connection and restored to 5000 ms before release.
5. The `CONSULTANT_UNAVAILABLE` code of the earlier informal MCP_API list is not in the EWP's closed enum and is not defined in v1.
6. Existing durable Work Packages and events may carry short commit identifiers; the 40/64 hex rule applies to principal wire objects only, and the guard compares the stored string exactly.
7. The mutant "treat Correlation as dedup" has no code to mutate (no dedup exists); A10 asserts the opposite behaviour.
