# WP-M5-R2 — Independent review executor and acceptance gate

## Identity

- Revision: 3 (owner review of r2 head 106dafd repaired; verdicts pending re-verification); task: task-m5-r2-review-acceptance; window: [2026-10-G](window-2026-10-g-overview.md).
- Base: `71bdaec6d6d81c1b6e52d8b30f0f8485f928925a` (`main` `cebb4f0` plus the PR #83 reconcile merge, 2026-10-05).
- Contract digest: reviewed immutable Git blob. No fictitious runtime state revision; record the actual accepted dependency commits at execution.
- Endpoint: competent Go implementer with protocol/record, SQLite-transaction and review-process skill; complete admission is mandatory. Each Part is sized for one endpoint session.
- Status: **DRAFT / NOT_READY / NOT FROZEN. No implementation authority.** Acceptance remains hard-disabled until Part C and its policy receipt are accepted; Part C policy content is OWNER INPUT-3.
- Dependencies: WP-M5-1/2 (merged); Part A none (pure; **R1-A imports it**, so R2-A freezes before R1-A); [M5-R1](wp-m5-r1-native-task-executor-ewp.md) Part A (`EndpointResolver` with `ExcludeBases`, `ResolvedEndpoint` basis fields, `PolicySource`, `Bind`) and Part B (Registry, Drivers, Compiler, Worktrees, the leaf `internal/execrt` with the single `ProjectLock`) for Part B; [M5-R4](wp-m5-r4-protected-operator-ingress-ewp.md) Part A (`receipts.Verifier` with subject lookup) for Part C; **Part C also waits for R1-C** (the validation producer) and for R2-B (the review producer). **Part B has no dependency on Part C or on any acceptance policy** (explicit configuration contract, below). Part C additionally needs the disclosed `controlplane`, `facade` and `principal` amendments (G-C1, G-C2, SC-2).
- Parts (separately freezable, in order): **A** invocation provenance and actor derivation (pure; first, because R1 uses it). **B** independent review executor (policy-independent; durable bounded review invocations). **C** versioned acceptance policy and transaction-guarded acceptance activation. Full multi-review campaign automation, finding closure ledger workflow and repair campaigns remain M7.

## Objective

Make "independent review" and "accepted" evidence-backed rather than conversational. (A) Bind every worker and reviewer invocation to a canonical `protocol.ActorProvenance` derived by DevCadence from the resolved endpoint, not named by a model. (B) Run each explicitly requested review dimension as a separate, clean-context invocation on an actor independent of the worker, producing durable evidence for **every** invocation outcome, one invocation per `(attempt, dimension)`. (C) Replace the hard-disabled `accept` with a gate that, inside the same transaction as `ChangeAccepted`, re-verifies the active versioned acceptance policy, complete validation and review evidence, actor independence and candidate lineage. Absent or unknown evidence denies (DCI-040–044, 134, 135).

## Context Manifest

Role: protocol/record implementer; independent Contract/Authority and Test Adequacy/Mutation reviewers. Read envelope: `internal/protocol/{review_ledger,review_result,validation_result,project_state,work_package}.go`, `internal/events/{payloads_task,records}.go`, `internal/controlplane/{batch,service,records}.go`, `internal/principal/facade/{ports,service}.go` (Accept, `CandidateGate`, `GateInput`), `internal/state` task reduction, `prompts/reviewer.md`, `prompts/closure-reviewer.md`, R1 resolver and executor primitives. Write scope: additive records (`InvocationProvenance`, `ReviewInvocation`, `AcceptanceEvidence`) and the `ActorBasis` type in `internal/protocol` with schemas and fixtures; `internal/actors` (derivation; `actors.ActorBasis` is an alias of `protocol.ActorBasis`), `internal/reviewexec`, `internal/acceptance`; additive `controlplane` read methods and one facade port replacement (Part C, see G-C1/G-C2); `facade.Accept` enablement; composition in `cmd/devcadence-mcp`; synchronization of PROTOCOLS §§9–11/19, PROJECT_STATE, MCP_API, REVIEW_AND_CONVERGENCE cross-references, schema README. No invariant or catalog change.

Exact clauses: AGENTS §§2–9, 8A, 12–15, 17; ADR-0024 §§3–5; ADR-0010; REVIEW_AND_CONVERGENCE §§1, 6–7, 14; SECURITY §§3, 14–17; DCI-025, 032, 040–049, 080–084, 090, 120–124, 133–135, 159–161. Risks: self-verification, laundered independence, cherry-picked evidence, TOCTOU, authority from conversational claims, unbounded review cost. Re-resolution triggers: a new record kind beyond the three above, a `BatchReadView`/facade port change beyond G-C1/G-C2, an `ActorProvenance` change, owner selecting another independence basis, or any request to enable acceptance without a verified policy.

## Scope envelope

Authorized: as in Context Manifest. Forbidden: enabling `accept` by constructor flag, test double, config toggle or owner-edited JSON; averaging or suppressing disagreement; in-place finding closure by the author or worker (DCI-134); a reviewer receiving worker transcripts or reasoning; automatic merge, accepted-commit advancement or integration (acceptance remains separate from integration); a change to `ReviewResult`, `ValidationCompleted`, `ChangeAccepted` payload schemas; M7 campaign orchestration; new MCP tools or grants.

LOCAL_DISCRETION: helper layout, review-prompt wording inside the compiler-admitted lens templates, test helpers, record-id formatting that preserves the exact ids below.

## Requirements and invariants

| ID | MUST requirement | Invariant |
| --- | --- | --- |
| R1 | Every worker, reviewer and verifier invocation has one immutable `InvocationProvenance` record whose `ActorProvenance` is derived from endpoint basis fields by `internal/actors`; model output never supplies identity | I1: independence evidence is canonical and not model-named |
| R2 | The gate recomputes `ActorID` from the stored basis fields and applies `protocol.ActorsIndependent` between the worker and every accepting reviewer; unknown basis field denies | I2: independence is derived, not trusted |
| R3 | Each **explicitly requested** dimension (non-empty list; no policy default) is reviewed by its own invocation on an endpoint whose actor differs from the worker's under the **configured review-execution basis** (`Options.IndependenceBasis`, closed set; fixed `endpoint_model` in the first slice); no independent eligible endpoint denies `MODEL_UNAVAILABLE`. Whether the collected dimensions and basis satisfy the `AcceptancePolicy` is decided by Part C alone | I3: no self-review and no silent downgrade; Part B has no policy dependency |
| R4 | A reviewer receives only the exact contract, an immutable candidate/diff manifest, validation evidence and its lens template; it never receives worker session text | I4: lineage is empty by construction (DCI-042, DCI-135) |
| R5 | A passing/concern/failing `ReviewResult` is persisted only from strictly decoded, executor-completed output together with its provenance record and a `ReviewInvocation` record (`outcome=completed`), atomically, and only for the current reviewing candidate. Every other terminal invocation outcome (invalid output, limit, driver failure, cancellation) is persisted as an **executor-authored non-passing review** (`verdict=unable_to_verify`, no findings, `ReviewInvocation.outcome` naming the cause) in the same shape; model output is never copied into it | I5: model output is evidence, never a state change; no invocation outcome is invisible to acceptance |
| R6 | `accept` succeeds only through an `AcceptancePolicy` whose bytes are process-pinned and whose activation receipt is re-verified on every `Decide`, and an in-transaction re-evaluation of complete evidence | I6: acceptance is a deterministic consequence of recorded evidence |
| R7 | Evidence is the complete set for the attempt: the latest validation per required scope and **every review invocation of the attempt in any dimension** (required or not, including executor-authored non-passing outcomes); any non-pass, blocking-severity, missing, errored or unverifiable item blocks; disagreement is never averaged. **Invocation bound:** at most **one** durable review invocation per `(attempt, dimension)` (v1 constant `MaxReviewInvocationsPerDimension = 1`); a durable non-pass of any cause is final for the attempt and the only repair is `reject` plus a new attempt with fresh reviews. There is no retry-until-pass path | I7: no cherry-picking and no pass-shopping by repeating failed invocations; DCI-044 preserved |
| R8 | The gate writes a deterministic `AcceptanceEvidence` record in the same transaction as `ChangeAccepted` and re-verifies it as a precondition | I8: no TOCTOU between check and commit |
| R9 | A finding closes only by a new candidate that passes fresh independent review in M5; the author, worker and acceptance gate never resolve a finding | I9: attempted resolution is not verification (DCI-134) |
| R10 | Acceptance neither merges nor advances accepted source; absence of an active policy, unknown change class or expired policy leaves `accept` denied | I10: acceptance is separate from integration; unknown denies |
| R11 | Review worktrees are named from the **invocation id** (never from a persisted-review count) and are removed by the executor after the invocation ends; startup removes clean orphans | I11: a failed or unpersisted review cannot make a later invocation collide |

## Verified facts and representability gaps

| Fact / gap | Evidence at pinned base | Implication |
| --- | --- | --- |
| `accept` is hard-disabled: after admission it returns `NEEDS_PRINCIPAL` with ref `acceptance-runtime-unavailable`; no `ACCEPTANCE_NOT_IMPLEMENTED` code exists | `facade/service.go` Accept; `TestA10_AcceptIsHardDisabled` | enablement must preserve this denial when no gate is installed |
| `CandidateGate{Check(ctx, BatchReadView, GateInput)}` and `GateInput{Caller,Meta,Candidate,ValidationIDs,ReviewIDs}` exist as an unused reserved seam | `facade/ports.go` | reuse as the transactional guard shape |
| `ReviewResult` has only `ReviewerProfile`/`ModelIdentity` strings; `Attempt` has `WorkerRole/Profile/ModelIdentity` | `protocol/review_result.go`, `tasks/attempt.go` | not stable independent actors; provenance record required |
| `ActorProvenance{ActorID,InvocationID,Role,LineageActorIDs,EndpointRef,SessionRef,ModelRef}`, `Validate(kind, role)`, `ActorsIndependent` (distinct actor, distinct invocation, no lineage overlap) exist | `protocol/review_ledger.go` | reuse unchanged; no schema change |
| `ChangeAccepted` carries `ValidationIDs`, `ReviewIDs`, `UnresolvedDisagreements`, `DecidedBy`; `DecisionAuthority` has `policy` | `events/payloads_task.go`, `protocol/project_state.go` | v1 uses `DecidedBy: policy` and empty disagreements; no payload change |
| `BatchReadView` exposes `ProjectState()` and `Record(kind,id,version)` only; no per-attempt evidence enumeration; no way to obtain a view outside a batch | `controlplane/batch.go` | **G-C1:** completeness (latest per scope, all reviews) cannot be proven transactionally; additive read methods required (decision below) |
| `CandidateGate.Check(ctx, view, GateInput) error` is an unused seam that returns only an error; `Accept` today returns `NEEDS_PRINCIPAL`/`acceptance-runtime-unavailable` after `admit` and `Validate` and nothing else; `AcceptRequest` has `Meta, Candidate, ValidationIDs, ReviewIDs, Reason` | `facade/ports.go`, `dto.go`, `service.go` | **G-C2:** the gate must also hand back the event payload and the record to store, so the seam is replaced (disclosed public Go contract change; nothing calls `CandidateGate` today) |
| `ChangeAccepted.Validate` requires `TaskID, AttemptID, CandidateCommit, SemanticSummary, WorkPackageID`, **at least one** `ValidationID`, **at least one** `ReviewID` and a valid `DecidedBy`; `ReviewCompleted` carries `ReviewID, Dimension, Verdict, RecordDigest, WorkPackageID`; `ValidationCompleted` carries `ValidationID, Scope, Status, Commit, RecordDigest` | `events/payloads_task.go` | exact field sources are specified under Part C |
| `protocol.DecisionAuthority` has `policy`; `ActorControlPlane` exists | `protocol/project_state.go`, `protocol.go` | `DecidedBy: protocol.AuthorityPolicy`; event actor `{ActorControlPlane, "acceptance"}` |
| `Command.Records` can attach a record independent of payload references | `controlplane/service.go` | provenance and acceptance evidence attach without payload edits |
| Review ledger records (`ReviewFinding`, resolution, verification) are keyed by `CampaignID`; no durable ledger projection or events | `protocol/review_ledger.go`; `internal/events` | not used for the M5 minimal path; M7 owns it |
| `prompts/reviewer.md`, `closure-reviewer.md` exist | `prompts/` | lens templates; compiler role admission re-verified at step 0 |
| `Severity` is an unordered string enum | `protocol/project_state.go` | policy lists blocking severities explicitly |
| No `InvocationProvenance`, `ReviewInvocation`, `AcceptancePolicy`, `AcceptanceEvidence` types; `ReviewResult.Validate` accepts verdict `unable_to_verify` with empty findings; `ReviewCompleted.Verdict` carries all four verdicts; records are stored only with an event (`Command.Records`), so a review invocation cannot be recorded without a `ReviewCompleted` | repository search; `protocol/review_result.go`; `controlplane/service.go` | new additive records; non-passing invocation outcomes reuse `ReviewCompleted` (no new event) |
| `facade.coded` is unexported (see R1 facts) | `facade/errors.go` | R2 refusals use `principal.CodedError` (SC-2); the review-partial counts travel as evidence refs |

Step 0 re-verifies each row; a false row escalates.

## Part A — Invocation provenance and actor derivation (pure)

~~~go
// package protocol (serialized field of additive records); package actors declares: type ActorBasis = protocol.ActorBasis
type ActorBasis struct {                 // non-secret; account_ref is an opaque handle, never an email or key
    EndpointID, ModelID, ModelRevision string
    Provider, ModelFamily, AccountRef  string
}
type InvocationProvenance struct {       // protocol record kind "InvocationProvenance", schema 1.0, version 1
    SchemaVersion, ProvenanceID string   // implementer: "<attempt_id>:implementer"; reviewer: "<review_id>"; verifier: "<run_id>:verifier"
    ProjectID, TaskID, AttemptID, WorkPackageID string
    Role  protocol.ProvenanceRole
    Actor protocol.ActorProvenance        // Validate(kind, Role) must pass
    Basis ActorBasis                      // ALWAYS the full six-field basis from the bound endpoint (fields may be ""), so any gate can re-derive under any basis
    IndependenceBasis string              // basis used to compute Actor.ActorID at record time: implementer "endpoint_model" (fixed); reviewer Options.IndependenceBasis; verifier "endpoint_model"
    EndpointBindingDigest, ContextManifestDigest, PromptDigest string
    CandidateCommit string                // "" for implementer at start; required for reviewer/verifier
    Dimension string                      // reviewer only
    StartedAt string
}
type ReviewInvocation struct {           // protocol record kind "ReviewInvocation", schema 1.0, version 1, id = ReviewID (1:1 with the ReviewCompleted event)
    SchemaVersion, ReviewID, ProjectID, TaskID, AttemptID, WorkPackageID string
    Dimension protocol.ReviewDimension
    InvocationID, ProvenanceID, CandidateCommit string // ProvenanceID == ReviewID of the stored InvocationProvenance
    InvocationNumber int                  // always 1 in v1 (== MaxReviewInvocationsPerDimension)
    Outcome string                       // closed: "completed" | "output_invalid" | "limit_reached" | "driver_error" | "cancelled"
    StartedAt, EndedAt string
    UsageArtifactDigest string            // "" when not recorded; usage uses the knownness-aware measurement
}
func DeriveActorID(basis string, b ActorBasis) (string, error) // "actor:" + first 24 hex of sha256(canonical{basis, selected fields})
~~~

`endpoint_model` selects `{endpoint_id, model_id, model_revision}`; `model_family_account` selects `{provider, model_family, account_ref}`. Non-model actors (the R3 verifier) are derived through the same function under `endpoint_model` with `ActorBasis{EndpointID:"devcadence-verifier", ModelID:<source commit>, ModelRevision:<profile digest>}` so there is exactly one derivation path. Any selected field empty returns an error (unknown denies; never a fallback to the other basis). `InvocationID` is a server-generated unique `inv_` id per invocation. `LineageActorIDs` contains the ActorIDs of every prior invocation whose output *text* the invocation received; the review executor passes none, so reviewer lineage is empty. The worker's record is stored in the same transaction as `AttemptStarted` (R1 Delegate step 7; the worker record is derived under the fixed `endpoint_model` and carries the full `Basis`, which is what makes later re-derivation under another basis possible without any acceptance policy existing at start time). A reviewer's `InvocationProvenance` and `ReviewInvocation` are stored in the same transaction as its `ReviewCompleted` (Part B step 6). `ActorProvenance.EndpointRef/SessionRef/ModelRef` are filled from the resolved binding, never from model text.

**Re-derivation rule (used by Part B step 2 and Part C `Decide` step 4, stated once).** For any stored `InvocationProvenance` and any selected basis `B`, the actor under `B` is `actors.DeriveActorID(B, record.Basis)`; the stored `Actor.ActorID` is only a claim and is accepted only when it equals the derivation under the record's own `IndependenceBasis`. Independence is then evaluated on provenance copies whose `Actor.ActorID` is replaced by the derivation under `B` (an error on any selected field blocks).

## Part B — Independent review executor

Part B is **policy-independent**: it neither reads nor imports any acceptance policy or `internal/acceptance`. Everything it needs beyond the request comes from an explicit configuration contract.

~~~go
type Options struct { /* ProjectID, Lock *execrt.ProjectLock (the SAME handle composition passed to taskexec; never acquired here), ControlPlane, Policy execpolicy.PolicySource, Resolver, Drivers, Compiler, Worktrees, Repositories execrt.RepositoryProvider, Runner, Registry, Artifacts execrt.ArtifactSink, Clock, IDs, Logger,
    IndependenceBasis string // REQUIRED, closed set {"endpoint_model","model_family_account"}; composition passes "endpoint_model" in this slice; anything else refuses construction
*/ }
const MaxReviewInvocationsPerDimension = 1   // v1: not configurable
func New(Options) (*Executor, error) // implements facade.ReviewExecutor; no recovery of results, but it removes clean orphan review worktrees (step 4)
~~~

Constructor refusals: nil Lock/ControlPlane/Policy/Resolver/Compiler/Worktrees/Repositories/Runner/Registry/Artifacts, empty ProjectID, `IndependenceBasis` outside the closed set. There is no "default dimensions" behaviour: the dimension list is always the request's.

Algorithm `Review(ctx, caller, meta, candidate, dimensions)`:

1. (Sync) Facade already checked admission, prefix, lineage and WP freshness. Re-read the attempt: status `candidate_produced`, task `reviewing`, commit equals `candidate`. `dimensions` must be **non-empty** (else `INVALID_ARGUMENT`, ref `review-dimensions-required`), unique, and values of the closed `protocol.ReviewDimension` set. **At most one durable invocation per dimension per attempt:** any requested dimension that already has a `ReviewCompleted` for this attempt (read from the projected evidence; this includes executor-authored non-pass outcomes) denies the whole call `POLICY_DENIED`, ref `review-already-recorded`, zero effects. A recorded non-pass review of any cause is final for the attempt; the repair path is `reject` and a new attempt (R7).
2. `policy, _ := Policy.Current(ctx)` (R11 rule: re-verified at every `Review` start). Load the worker `InvocationProvenance` (`<attempt_id>:implementer`) via the stored record (digest-verified); derive the worker actor under `Options.IndependenceBasis` from the **stored `Basis`** with the re-derivation rule (Part A); any error denies `MODEL_UNAVAILABLE`, ref `worker-basis-unknown`. Keep the worker's stored `Basis` for exclusion.
3. For each requested dimension, in sorted order: `Resolver.Resolve(EndpointRequest{Role:"reviewer", IndependenceBasis: Options.IndependenceBasis, ExcludeBases: {worker stored Basis}})` (R1-A owns exclusion and the tuple comparison; this Part does not extend the resolver), then `Drivers.Open` (metadata only; nothing to close) and `execpolicy.Bind`, then re-derive the reviewer actor from the **bound** endpoint and deny `independent-actor-collision` on equality with the worker actor. None eligible → deny the whole call `MODEL_UNAVAILABLE`, ref `no-independent-reviewer`, zero effects (all dimensions are resolved and opened/bound before any invocation starts, so an unavailable reviewer for a later dimension denies before earlier dimensions run); never reuse the worker's actor.
4. Per dimension generate `invocationID := IDs.New("inv")` and compile a clean-context pack with the compiler (role reviewer, `CandidateCommit`, `CandidateDiffManifest`, `ValidationSummaries` by digest, exact Work Package contract, the lens template for the dimension). The pack contains no worker prompt, transcript, reasoning or session artifact. Create a read-only worktree with id **`rv-<invocationID>`** at the candidate commit (derived from the invocation id, never from a persisted-review count, so an unpersisted or failed earlier invocation cannot cause a `worktrees.Create` collision); tools are `read_file`, `grep`, `symbols` only; no write handler, no shell, no network. **Cleanup:** the `run` function removes its worktree with `Worktrees.Cleanup` after the invocation ends on every path (success, failure, cancellation; a refused cleanup of a dirty tree is logged and left); `New` removes clean orphan worktrees whose id has the prefix `rv-` using the same `Cleanup` path (review worktrees are read-only, so a dirty one is anomalous and is retained and logged).
5. Start the operation via `Registry.Start(project,"review",deadline=sum(limits)+60s, run)` (process-scoped). `run` executes dimensions sequentially (bounded concurrency one), one metered invocation per dimension under the same limits, per-call bounds, unknown-usage and cancellation rules as R1 (limits from the reviewer grant). The invocation ends in exactly one `Outcome`: `completed` (output is a single JSON `ReviewResult` body, strict-decoded into `protocol.ReviewResult`; the executor then overwrites `ReviewID` (= `IDs.New("rev")`), `ProjectID`, `AttemptID`, `WorkPackageID`, `Dimension`, `ReviewerProfile`, `ModelIdentity` from its own trusted values (model-supplied values ignored), requires `Validate()` to pass, and caps findings (≤64, statements ≤2 KiB)); `output_invalid` (decode/validate/cap failure; no repair loop); `limit_reached` (meter, turn, wall-time or unknown-usage rule); `driver_error`; `cancelled`. There is **no retry** inside or outside the operation (DCI-049).
6. Persist **per dimension, for every outcome**: `ApplyBatch{ExpectedStateRevision: fresh prefix, Preconditions:[candidateStillReviewingGuard], Commands:[ReviewCompleted{...RecordDigest}], Records:[ReviewResult, InvocationProvenance(reviewer, ProvenanceID = ReviewID, InvocationID = invocationID), ReviewInvocation{ReviewID, InvocationNumber:1, Outcome, ...}]}`. For `completed` the `ReviewResult` is the validated model result. For any other outcome the executor authors the `ReviewResult` itself: fresh `ReviewID`, trusted fields as above, `Verdict: unable_to_verify`, empty findings and must-compliance, no model text; it is persisted exactly like a completed review (cancellation persists under a fresh 10 s context). The guard requires task `reviewing`, attempt and commit unchanged and no existing `ReviewCompleted` for that dimension. A concurrent change returns a conflict; the result is dropped, not retried against a new candidate.
7. **Partial-result semantics.** Each dimension's outcome is durable at its own commit. If dimension *i* ends in any non-`completed` outcome, or its persistence conflicts, dimensions already persisted stay, dimensions not yet run are not run, and the operation fails (status `failed`; `cancelled` with no refs when the registry reports cancellation, in which case the caller reads state) with `principal.NewCodedError(code, retryable, refs, ...)` where `code` is `MODEL_UNAVAILABLE` for `driver_error`/`limit_reached`, `INTERNAL` for `output_invalid`, `POLICY_DENIED` for a persistence conflict, carrying evidence refs `review-partial`, `completed:<n>`, `requested:<m>` (the counts travel as refs, not as free text; `n` counts dimensions with a durable `ReviewCompleted` from this call, including executor-authored non-pass outcomes, `m` is the request size). The principal may call `review` again only for dimensions with no durable invocation (step 1 permits exactly those); a dimension that ended in a durable non-pass is **never re-run**: the gate (R7) will deny and `reject` starts a new attempt. If persisting an outcome itself fails, no durable invocation exists for that dimension and the operation fails with ref `review-outcome-unrecorded`; that is the only way a dimension can be re-requested after an invocation started (disclosed below). Reviews never edit the candidate, never write findings elsewhere and never resolve earlier findings; disagreeing dimension verdicts remain separate records.

**Disclosed limitation: crash accounting (loopback-only slice).** Unlike R1, a review has no durable *intent* before the model call: the first durable fact is the terminal outcome. A crash mid-invocation, or a persistence failure after the call, loses that process-scoped result, leaves no `ReviewCompleted`, and permits one new invocation for that dimension (bounded only by the principal and by cost). That is acceptable for the first loopback-only slice, where the cost is local compute. **Before any subscription or metered-API reviewer is enabled**, a durable review-invocation intent (a new additive event written before the call, with its own contract) is required; until then INPUT-2 option A (loopback) is the only permitted reviewer class and step 3 of this Part denies `MODEL_UNAVAILABLE`, ref `reviewer-grant-not-local`, whenever the resolved reviewer endpoint's `Locality` is not local. Uncertain spend is reported `unknown`, never fabricated.

## Part C — Acceptance policy and transaction-guarded activation

~~~go
type ClassRule struct {
    ChangeClass protocol.ChangeClass
    ValidationScopes []protocol.ValidationScope  // >=1 required (policy validation refuses an empty list); each must have a passing latest validation
    ReviewDimensions []protocol.ReviewDimension  // >=1 required (ChangeAccepted needs a review id); each must have >=1 review; ALL reviews of the attempt must pass
}
type AcceptancePolicy struct {
    Version string // "1.0"
    PolicyID string; Revision int
    NotBefore, NotAfter string // RFC3339 UTC, validity <= 30 days
    IndependenceBasis string   // "endpoint_model" | "model_family_account"
    Classes []ClassRule        // unique ChangeClass; unknown class at use denies
    BlockingSeverities []protocol.Severity // any finding with one of these in a required review blocks
    // Fixed in v1, not configurable: accepting verdict is "pass" only; unresolved disagreements must be zero.
}
type ReviewEvidenceRef struct { ReviewID, Digest, Dimension, Verdict, InvocationOutcome, ActorID, InvocationID string; Independent bool } // ActorID = the RE-DERIVED reviewer actor under the policy basis
type ValidationEvidenceRef struct { ValidationID, Digest, Scope, Outcome string; Seq int64 }
type AcceptanceEvidence struct {         // protocol record kind "AcceptanceEvidence", version 1, id "acc:<attempt_id>:<candidate12>"
    SchemaVersion, EvidenceID, ProjectID, TaskID, AttemptID, WorkPackageID string
    WorkPackageVersion int
    CandidateCommit, BaseCommit string
    Policy AcceptancePolicy; PolicyDigest, PolicyReceiptID string
    Worker protocol.ActorProvenance      // actor id re-derived under the policy basis from the stored worker Basis (not the stored claim)
    Validations []ValidationEvidenceRef  // ordered by scope
    Reviews []ReviewEvidenceRef          // ordered by dimension, ReviewID
    StateRevision string                 // prefix the evaluation read
    Decision string                      // "accept"
}
type Evaluator interface { // pure; `AcceptanceGate.Decide` wraps it, and the same code serves pre-build and in-transaction use
    Evaluate(ctx context.Context, view controlplane.BatchReadView, in facade.GateInput) (AcceptanceEvidence, error)
}
~~~

**G-C1 decision (explicit amendment of WP-M5-1, disclosed).** Two additive `controlplane` read methods, one in-repo implementation each:

~~~go
// on BatchReadView (in-transaction view)
AttemptEvidence(ctx context.Context, attemptID string) (AttemptEvidenceIDs, error)
// on *controlplane.Service: a read-only snapshot view outside any batch, same implementation as the in-transaction one
ReadView(ctx context.Context, projectID string) (BatchReadView, func(), error)   // the func releases the read transaction

type ValidationEvidence struct {          // from ValidationCompleted events of the attempt, ascending Seq
    ValidationID, Scope, Commit, Status, RecordDigest string
    Seq int64                              // events.Event.Seq: the project journal sequence of the event
}
type ReviewEvidence struct {              // from ReviewCompleted events of the attempt, ascending Seq
    ReviewID, Dimension, Verdict, RecordDigest string
    Seq int64
}
type AttemptEvidenceIDs struct { Validations []ValidationEvidence; Reviews []ReviewEvidence }
~~~

`AttemptEvidence` reads the journal inside the same SQLite transaction; ordering is by `Seq`; an unknown attempt returns `NOT_FOUND`. Selected over alternatives: (a) trust the principal-supplied `ValidationIDs`/`ReviewIDs` (cherry-picking a stale pass: rejected); (b) read the journal outside the transaction (TOCTOU: rejected). `ReadView` exists only so the pre-build evaluation can run the same `Evaluator` code before the batch (the in-transaction guard re-runs it). This amendment needs window-level review before Part C freezes.

**G-C2 decision (explicit public Go contract change in `facade`, disclosed).** The unused `CandidateGate` seam is replaced:

~~~go
type AcceptanceDecision struct {
    Payload *events.ChangeAccepted           // fully built; the facade does not edit it
    Record  controlplane.RecordToStore        // the AcceptanceEvidence record (Version 1)
    EvidenceDigest string                    // protocol.Digest of the AcceptanceEvidence; the in-transaction guard compares it
}
type AcceptanceGate interface {
    // Decide is pure (no writes, no network, no model). Every denial is a coded error with a fixed ref.
    Decide(context.Context, controlplane.BatchReadView, GateInput) (AcceptanceDecision, error)
}
// facade.Options gains: Acceptance AcceptanceGate   // nil => today's NEEDS_PRINCIPAL / acceptance-runtime-unavailable, byte-for-byte
~~~

`GateInput` (unchanged) gains no field; `AcceptRequest.Reason` is **never** read by the gate and never persisted in any event or record (the payload has no field for it); the facade may log its length and digest only. It is untrusted display text.

Activation: `acceptance.Load(ctx, verifier receipts.Verifier, home) (AcceptanceGate, error)` reads `DEVCADENCE_HOME/config/acceptance-policy.json` under launch-binding protections, strict-validates it, computes the canonical policy digest and pins the **bytes** (changed file → deny until relaunch). Authority is re-verified in every `Decide` through `verifier.Verify(Request{ReceiptID:"", Purpose: acceptance.policy_activate, Subject{AcceptancePolicy, PolicyID, Revision}, SubjectDigest: policyDigest})` (R4 lookup by subject; same rule as R1 R11), requiring `Verified.IsValid()` and the validity window. No policy, invalid policy, unverified, expired or revoked grant means `Decide` returns the denial `acceptance-runtime-unavailable` (the same ref as today, so the facade response is unchanged).

`Decide` algorithm (every failure is a fixed denial ref; none writes):

1. Receipt/policy valid and `NotBefore <= now <= NotAfter`; `view.ProjectState()` task exists in state `reviewing`; the attempt equals `GateInput.Candidate.Attempt`, status `candidate_produced`, commit equal; Work Package id/version/digest equal the task's approved tuple; when `Git.AcceptedCommit` is set it equals the Work Package base (candidate not stale).
2. `ClassRule` for the task's change class, else `acceptance-unknown-change-class`.
3. `ev := view.AttemptEvidence(attemptID)`. For each required scope: the highest-`Seq` validation for that scope **on that commit** must exist and have status `pass`, and its `ValidationResult` record (via `view.Record`, digest-verified) must name the same commit; any later non-pass supersedes an earlier pass.
4. Worker actor: load the worker `InvocationProvenance` (`<attempt_id>:implementer`) via `view.Record` (digest-verified; role implementer; attempt/WP ids equal; `EndpointBindingDigest` non-empty). **Re-derive** `workerActor := DeriveActorID(policy.IndependenceBasis, record.Basis)` from the stored `Basis` under the policy basis (error on any selected empty field blocks); the stored `Actor.ActorID` must equal the derivation under the record's own `IndependenceBasis`, else `INTEGRITY`. Build `workerProv` = the stored provenance with `Actor.ActorID = workerActor`.
5. Reviews: **every** `ReviewCompleted` of the attempt in `ev.Reviews`, in any dimension (required or not), must have verdict `pass` and no finding with a severity in `BlockingSeverities`; each is loaded via `view.Record`, digest-verified and bound to the attempt/WP/dimension. Each must have exactly one `ReviewInvocation` record (id = review id, `InvocationNumber == 1`, same attempt, dimension and candidate commit) with `Outcome == completed` (missing record: `acceptance-review-invocation-missing`; any other outcome blocks), which makes the invocation set exactly the review set: **there is no invocation evidence outside `ev.Reviews`** (every durable invocation produced a `ReviewCompleted`, Part B step 6; a count of two reviews for one dimension is `INTEGRITY`). Its reviewer `InvocationProvenance` (id = review id) must exist, role `reviewer`, `CandidateCommit` equals the candidate, its stored `Actor.ActorID` equals `DeriveActorID(record.IndependenceBasis, record.Basis)`, and with `reviewerProv` = the stored provenance whose `Actor.ActorID` is re-derived under `policy.IndependenceBasis`, `protocol.ActorsIndependent(workerProv, reviewerProv)` holds. Each required dimension must have at least one review. A failed or non-completed review in an optional dimension therefore blocks acceptance (closes the cherry-pick vector of running extra dimensions and omitting the failing one). Any `concern`, `fail`, `unable_to_verify`, missing record or failed derivation blocks. Part C alone decides whether the dimensions present and the review-execution basis recorded in each provenance satisfy the `ClassRule`/`IndependenceBasis`: an execution basis different from the policy basis is not an error by itself, because independence is recomputed under the policy basis from stored `Basis` fields (unknown fields block).
6. The accepted evidence is exactly the supplied sets: `GateInput.ValidationIDs` must equal, as a set, the ids of the selected latest-per-required-scope validations; `GateInput.ReviewIDs` must equal, as a set, **all** review ids in `ev.Reviews`. A mismatch denies (the principal cannot omit an inconvenient review or pad with a stale validation).
7. Build `AcceptanceEvidence` in canonical order with the policy copy and digests, and the payload:
   - `ChangeAccepted.TaskID, AttemptID, WorkPackageID, CandidateCommit` from the verified attempt (never from the request);
   - `ValidationIDs` = the selected validation ids sorted by `Scope`; non-empty because every `ClassRule` must list at least one required scope (policy validation refuses a class with none); `ReviewIDs` = all review ids sorted by `(Dimension, ReviewID)`; non-empty because every `ClassRule` must list at least one required dimension;
   - `UnresolvedDisagreements` = empty (any disagreement already blocked);
   - `DecidedBy` = `protocol.AuthorityPolicy`;
   - `SemanticSummary` = the deterministic template `"accepted candidate <candidate_commit[:12]> of <work_package_id> v<version> under policy <policy_id> r<revision>; evidence <EvidenceDigest>"` (no request text, no model text);
   - the record id `acc:<attempt_id>:<candidate12>` and `AcceptanceEvidence.Decision = "accept"`.

`facade.Accept` with an installed gate: admission, `Validate()`, prefix and lineage checks as today; `view, release := ControlPlane.ReadView(ctx, project)`; `dec := Acceptance.Decide(ctx, view, input)`, `release()`; then one `ApplyBatch{ExpectedStateRevision: meta, Preconditions:[guard{Decide again inside the transaction and require EvidenceDigest equality with dec.EvidenceDigest}], Commands:[{Actor: {ActorControlPlane,"acceptance"}, Payload: dec.Payload, Records: [dec.Record]}]}`. A denial from `Decide` is returned through `refuse` as a `NEEDS_PRINCIPAL`/`POLICY_DENIED`/`STALE_*` coded error with the fixed ref (the existing `AcceptResponse` stays "error or success": `AcceptResult` gains `{TaskID, AttemptID, EvidenceID}`, additive). `Reject` is unchanged. Denials and refs are produced with `principal.NewCodedError` (SC-2; the facade's private `coded` is not used by `acceptance`). Acceptance does not touch Git, integration state or `Git.AcceptedCommit`.

## OWNER INPUT-3 — acceptance policy content and independence basis

Decision owner: repository owner, expressed as the signed `AcceptancePolicy`. **Safe default: deny** (no installed policy; `accept` stays disabled).

| Criterion | A. `model_family_account` (distinct provider/family/account) | B. `endpoint_model` (distinct endpoint and model id/revision) | C. Human-adjudicated acceptance when only one actor exists |
| --- | --- | --- | --- |
| Independence strength | highest; same-family reviewers do not count | medium; two quantizations of one family count as distinct | by a human receipt, not by model independence |
| Feasible on one local model | no | no (needs a second model/endpoint) | yes |
| Feasible with two local models | only if families differ | yes | yes |
| Honesty about weakness | strong claim | evidence records the basis | explicit human decision |
| Cost here | needs provider/family metadata on endpoints | derivation from existing fields | new decision purpose and gate path (not specified; own EWP) |

**Recommendation: B** as the policy default, recorded in every `AcceptanceEvidence`; A when the owner has two distinct families; C only as a separate later EWP if the owner wants to accept with one model. Proposed (not decided) required dimensions: `local`: correctness, test_adequacy; `systemic`: correctness, invariants, test_adequacy, security; `architectural`: correctness, architecture, invariants, security, test_adequacy. Required answers: basis, per-class validation scopes and dimensions, blocking severities (proposal: `high`, `critical`, and `medium` for architectural), validity window. **Remains blocked until answered:** Part C activation and therefore the M5 closure item "authorized acceptance". Parts A and B and all offline tests proceed. With a single actor available, acceptance is impossible by design and is reported as such, not worked around.

## Authority matrix

| Decision/effect | Authority | Forbidden substitute |
| --- | --- | --- |
| Actor identity | `internal/actors` from resolved endpoint basis | model-stated name/profile, request field |
| Independent verdict | Review executed by Part B on an independent actor; durable `ReviewResult` + provenance | principal-supplied verdict, worker self-report |
| Accept | Verified policy + complete evidence re-checked in the transaction; `DecidedBy: policy` | grant presence alone, test fake, JSON toggle |
| Activate/replace policy | R4 `acceptance.policy_activate` grant receipt and relaunch | any tool, same-identity file |
| Integrate/merge | not here | accept |

## Missing / unknown / stale and failure semantics

| Input | Missing | Unknown | Stale | Malformed/contradictory |
| --- | --- | --- | --- | --- |
| Policy / receipt | accept denied (`acceptance-runtime-unavailable`) | unknown class: denied | expired/revoked: denied | `INTEGRITY`, denied |
| Validation/review evidence | blocked (`acceptance-validation-missing`/`-review-missing`) | `unable_to_verify`: blocked | later non-pass supersedes | digest/binding mismatch: `INTEGRITY` |
| Provenance / basis fields | blocked | unknown family/account under basis: blocked | n/a | derivation mismatch: blocked |
| Candidate / base | stale conflict | n/a | `STALE_PROJECT_STATE`/`STALE_WORK_PACKAGE` | `INVALID_ARGUMENT` |

| Failure boundary | Required postcondition | Evidence |
| --- | --- | --- |
| Evaluation denies | zero events/records | fixed ref |
| Evidence changes between evaluation and commit | batch precondition fails, zero effects | guard digest mismatch |
| Review output invalid / limit / driver failure / cancellation | durable executor-authored `unable_to_verify` review + `ReviewInvocation` (outcome names the cause); operation failed; the dimension is final for the attempt | fixed code, `review-partial` refs |
| Persisting that outcome fails, or crash mid-invocation (loopback-only limitation) | no durable invocation; operation failed `review-outcome-unrecorded`; one new invocation may be requested | state lookup first; disclosed in Part B |
| Review commit conflicts | result dropped; earlier dimensions stay durable | conflict error, `review-partial` + `completed:<n>`/`requested:<m>` refs |
| Ambiguous accept commit | read state/record before retry | `AcceptanceEvidence` id |

## Traceability and acceptance scenarios

| ID | Setup → action → expected | Requirement → invariant → representation |
| --- | --- | --- |
| A1 | endpoint basis → derive twice → identical id; different model revision → different id | R1 → I1 → `DeriveActorID` |
| A2 | missing family/account under `model_family_account` → derivation error, no fallback | R1/R2 → I1/I2 |
| A3 | model output claims an actor/profile → stored identity unchanged | R1 → I1 |
| A4 | provenance record fails `Validate(kind, role)` or is a duplicate id → refused | R1 → I1 → record |
| A5 | a stored provenance with all six `Basis` fields: `DeriveActorID` under both bases is computable; a record whose stored `Actor.ActorID` disagrees with the derivation under its own `IndependenceBasis` is `INTEGRITY`; `actors.ActorBasis` and `protocol.ActorBasis` are the same type | R1/R2 → I1/I2 |
| B1 | only the worker's actor available → `MODEL_UNAVAILABLE`, zero calls/effects | R3 → I3 |
| B2 | reviewer context pack inspected → no worker prompt/transcript; lineage empty | R4 → I4 |
| B3 | malformed or extra-field model output → durable executor-authored review (`unable_to_verify`, no findings) with `ReviewInvocation.outcome=output_invalid`, no model text copied, no retry; the same dimension then denies `review-already-recorded` | R5/R7 → I5/I7 |
| B4 | valid output → `ReviewResult` + provenance + `ReviewCompleted` atomically, executor-set identity fields | R5 → I5 |
| B5 | candidate changes mid-review → conflict, nothing persisted | R5 → I5 |
| B6 | review limit exceeded, driver error, unknown usage without permission, or cancel → durable non-pass review with the matching `outcome` (cancel persisted under a fresh context), session closed, operation failed/cancelled; no second invocation of that dimension is possible in the attempt | R5/R7 → I5/I7 |
| C1 | no policy / bad receipt / revoked → accept still denied with today's ref, zero effects | R6/R10 → I6/I10 |
| C2 | complete passing evidence, independent reviewers → accepted; `AcceptanceEvidence` present; accepted commit unchanged | R6/R8/R10 → I6/I8/I10 |
| C3 | required validation scope missing or error → denied | R7 → I7 |
| C4 | earlier validation pass, later fail → denied (no cherry-picking) | R7 → I7 |
| C5 | a required-dimension review is `concern`/`fail`/`unable_to_verify` or a blocking-severity finding → denied | R7/R9 → I7/I9 |
| C6 | reviewer actor equals worker actor, or lineage overlaps → denied | R2 → I2 |
| C7 | principal omits an inconvenient review id → denied (set mismatch) | R7 → I7 |
| C8 | unknown change class, expired policy, stale candidate/base → denied | R6/R10 → I6/I10 |
| C9 | evidence/record changes between evaluate and commit → precondition fails, zero effects | R8 → I8 |
| C10 | finding present → only a new candidate with fresh independent reviews can lead to accept | R9 → I9 |
| C11 | no new tool/grant; `reject` behavior unchanged; with `Options.Acceptance == nil` the `accept` response is byte-identical to today's | scope |
| C12 | a non-required-dimension review is `fail`/`concern`/executor-authored `unable_to_verify` (or has a blocking finding) while all required reviews pass → denied; supplied `ReviewIDs` that omit it → denied (set mismatch) | R7 → I7 |
| C13 | a dimension with any durable invocation (pass, concern, fail, or a failed/invalid/limited/cancelled outcome) → `review` of it denies `review-already-recorded`; a dimension with none can still be requested; a persisted non-pass review (of any cause) → `accept` denied, `reject` then a new attempt with fresh reviews is the only path; a `ReviewCompleted` without its `ReviewInvocation(outcome=completed)` → `acceptance-review-invocation-missing`; two reviews for one dimension → `INTEGRITY` | R7/R9 → I7/I9 |
| C14 | `ChangeAccepted` payload built by the gate has non-empty sorted `ValidationIDs`/`ReviewIDs`, `DecidedBy: policy`, `SemanticSummary` equal to the template (no `AcceptRequest.Reason` text anywhere in events/records/logs) | R6/R8 → I6/I8 |
| C16 | worker provenance stored under `endpoint_model` with full `Basis`; policy basis `model_family_account` → worker and reviewer actors are **re-derived** from stored `Basis` (not read from the stored claim); empty family/account on either record → blocked; a stored worker `Actor.ActorID` forged to differ from reviewer's while bases are equal → still denied (collision after re-derivation) | R2 → I2 |
| C15 | policy receipt revoked between two `Decide` calls (no relaunch) → second denied `acceptance-runtime-unavailable`; zero-value `Verified` or JSON-decoded one is refused | R6 → I6; R4 R6 |
| B7 | `review` of [a,b,c]: a persisted `pass`, b ends `output_invalid` → a stays durable, b is durable non-pass, c is not run; operation `failed` with refs `review-partial`, `completed:2`, `requested:3`; `review` of [b] denies `review-already-recorded`, `review` of [c] is permitted; reviewer for b unavailable at resolution → whole call denied before a runs; persisting b's outcome fails → refs `review-outcome-unrecorded`, b has no durable record | R5/R7 → I5/I7 |
| B8 | reviewer endpoint whose bound actor equals the worker's (the pre-filter used the worker's stored `Basis`; a differing revision passes the conservative filter only if the `(EndpointID, ModelID)` differs, a derivation collision after `Bind` still denies) → `independent-actor-collision`, zero effects | R3 → I3 |
| B9 | empty `dimensions` → `review-dimensions-required`; `Options.IndependenceBasis` outside the closed set → constructor refuses; `reviewexec` has no import of `internal/acceptance`, `execpolicy` acceptance types or any acceptance-policy reader (package-boundary test) | R3 → I3 |
| B10 | review worktree id is `rv-<invocationID>`: a failed, unpersisted invocation followed by a new request never collides; the worktree is removed after the invocation on every path; `New` removes a clean orphan `rv-*` worktree and retains a dirty one | R11 → I11 |
| B11 | non-local reviewer endpoint → `reviewer-grant-not-local`, zero effects; a provenance for each persisted review carries the full six-field `Basis` and `IndependenceBasis` equal to `Options.IndependenceBasis` | R3 → I3 |

Live review and live acceptance are operator acceptance steps after OWNER INPUT-2/3; test-driver runs never claim them.

## Validation and Mutation Catalog

`go test -count=1 -race ./internal/actors/... ./internal/reviewexec/... ./internal/acceptance/... ./internal/controlplane/... ./internal/principal/... ./tests/...`; `make schemas`; `make docs-check`; `make verify`. Record versions, fixtures, spy counts and exit statuses.

Test seams (explicit): as in R1 (wrappers around `Options` interfaces; a store wrapper that fails the Nth write for mid-persist faults; evidence/record mutation between `Decide` and commit for C9 is a test `controlplane.Service` hook constructed only in `_test.go` that applies a competing `ApplyBatch` between the pre-build and the guard); the `ReadView`/`AttemptEvidence` implementations are tested with real SQLite fixtures including cross-attempt events that must not be returned.

| Mutant | Expected failure |
| --- | --- |
| Actor id from model output or from the stored field without recomputation | A3/C6 |
| Fallback between independence bases | A2 |
| Reviewer resolution not excluding the worker actor | B1 |
| Worker transcript included in reviewer pack | B2 |
| Worker actor read from the stored claim instead of re-derived under the policy basis; reviewer pre-filter by actor-id hash | C16/B8 |
| Failed/invalid/limited/cancelled invocation leaves no durable record (retry until pass); second invocation allowed for a dimension; worktree id from review count | B3/B6/B7/C13/B10 |
| Review executor reads an acceptance policy or defaults dimensions from it | B9 |
| Acceptance inspects only `ReviewCompleted` verdicts and ignores `ReviewInvocation` outcome | C13 |
| Persist invalid or partially decoded output; keep model identity fields | B3/B4 |
| Persist review for a changed candidate | B5 |
| `accept` enabled when policy missing or by a test fake | C1 |
| Evaluate outside the transaction only | C9 |
| Latest-validation rule uses first pass | C4 |
| Single passing review suffices when another dimension review failed | C5 |
| Supplied ids not compared to enumerated set | C7 |
| Skip independence or lineage check | C6 |
| Only required-dimension reviews inspected; supplied ids compared to the required set only | C12 |
| Same dimension reviewed twice, or a failed review re-run until pass | C13 |
| Summary built from request `Reason`, or free text | C14 |
| Policy authority cached from launch | C15 |
| Whole call continues after a later reviewer is unavailable; partial persistence not reported | B7 |
| Collision checked only before `Bind` | B8 |
| Accept advances accepted commit or merges | C2 |
| Unknown class or expired policy allowed | C8 |

Independent lenses: Contract/Authority; Test Adequacy/Mutation. Part B authors do not review Part C.

## Rationale, escalation and readiness

Selected: re-evaluate pure evidence inside the acceptance transaction with an additive enumeration read, versus a persistent "review state" projection (larger state surface, duplicates the M7 ledger). Provenance is a record, not a payload field, to avoid event schema changes. Rejected: trusting reviewer-profile strings as independence; letting the principal's id list define completeness; configuring acceptance through an environment toggle.

Escalate on: the owner needs acceptance with one actor before C is specified; the `BatchReadView` amendment is not authorized; an `ActorProvenance` or `ReviewResult` change is needed; a record kind registry cannot accept the new kinds additively; any request to review with worker lineage.

Implementation Readiness Report:

~~~text
author tally after repair round 2 (r3) (a self-count, not evidence; independent re-verification PENDING):
requirements represented: 11 (R1-R11; R3/R5/R7 tightened in r3)
acceptance scenarios mapped: 5 (A) + 11 (B) + 16 (C) = 32
unresolved architecture choices: OWNER INPUT-3; authorization of the disclosed amendments (`BatchReadView.AttemptEvidence`, `Service.ReadView`, `facade.AcceptanceGate`, additive `AcceptResult`, `principal.CodedError`, additive record kind `ReviewInvocation`); durable review intent before any non-loopback reviewer (disclosed limitation)
readiness: NOT_READY pending independent re-verification, owner inputs, R1 freeze for Part B and R4 freeze for Part C
~~~

Weaker-implementer check: author expectation only, to be re-tested by the independent Implementability reviewer. Part C stays blocked until G-C1/G-C2 are authorized and OWNER INPUT-3 is answered.

## Changelog

- r3: owner review of head 106dafd: reviewer exclusion by stored `Basis` (`ExcludeBases`) (item 1); `principal.CodedError` refs, `review-partial` counts as evidence refs (2); worker actor re-derived from stored `Basis` under the selected basis in R2-B step 2 and Decide step 4, implementer record basis specified at start (5); R2-B made policy-independent: explicit non-empty dimensions and a closed `Options.IndependenceBasis`, Part C alone judges sufficiency, dependency/freeze order updated, R2-C waits for R1-C (9 and note 2); review invocations vs verdicts: every outcome is durable, `ReviewInvocation` additive record, `MaxReviewInvocationsPerDimension = 1`, contradictory "no retry"/"re-requestable" text removed (10); worktree id `rv-<invocationID>` with cleanup (note 2); crash-accounting limitation and loopback-only restriction disclosed (note 1); leaf `execrt` imports.
- r1: initial draft for window 2026-10-G.
- r2: repair round 1: G-C2 (`AcceptanceGate` replaces unused `CandidateGate`; `Service.ReadView`) disclosed; exact `AttemptEvidenceIDs` (Seq = journal sequence); every review of the attempt must pass (optional-dimension cherry-pick closed) and one review per dimension per attempt with partial-result semantics; exact `ChangeAccepted` field sources and deterministic `SemanticSummary` (`Reason` is never persisted); `ReviewID`/`InvocationID` generation; exclusion/basis derivation moved to R1-A with post-`Bind` collision check; authority re-verified per `Review`/`Decide` via subject lookup; single shared `ProjectLock`; verifier actor derived through `DeriveActorID`; test seams; honest readiness tally.
