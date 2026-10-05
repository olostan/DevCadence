# WP-M5-R2 — Independent review executor and acceptance gate

## Identity

- Revision: 1; task: task-m5-r2-review-acceptance; window: [2026-10-G](window-2026-10-g-overview.md).
- Base: `71bdaec6d6d81c1b6e52d8b30f0f8485f928925a` (`main` `cebb4f0` plus the PR #83 reconcile merge, 2026-10-05).
- Contract digest: reviewed immutable Git blob. No fictitious runtime state revision; record the actual accepted dependency commits at execution.
- Endpoint: competent Go implementer with protocol/record, SQLite-transaction and review-process skill; complete admission is mandatory. Each Part is sized for one endpoint session.
- Status: **DRAFT / NOT_READY / NOT FROZEN. No implementation authority.** Acceptance remains hard-disabled until Part C and its policy receipt are accepted; Part C policy content is OWNER INPUT-3.
- Dependencies: WP-M5-1/2 (merged); [M5-R1](wp-m5-r1-native-task-executor-ewp.md) Part A (`EndpointResolver`, `ExecutionPolicy`) and Part B primitives (Registry, Drivers, Compiler, Worktrees) for Part B; [M5-R4](wp-m5-r4-protected-operator-ingress-ewp.md) Part A for Part C (policy receipt).
- Parts (separately freezable, in order): **A** invocation provenance and actor derivation (pure; first, because R1 uses it). **B** independent review executor. **C** versioned acceptance policy and transaction-guarded acceptance activation. Full multi-review campaign automation, finding closure ledger workflow and repair campaigns remain M7.

## Objective

Make "independent review" and "accepted" evidence-backed rather than conversational. (A) Bind every worker and reviewer invocation to a canonical `protocol.ActorProvenance` derived by DevCadence from the resolved endpoint, not named by a model. (B) Run each required review dimension as a separate, clean-context invocation on an actor independent of the worker, producing durable `ReviewResult` evidence. (C) Replace the hard-disabled `accept` with a gate that, inside the same transaction as `ChangeAccepted`, re-verifies the active versioned acceptance policy, complete validation and review evidence, actor independence and candidate lineage. Absent or unknown evidence denies (DCI-040–044, 134, 135).

## Context Manifest

Role: protocol/record implementer; independent Contract/Authority and Test Adequacy/Mutation reviewers. Read envelope: `internal/protocol/{review_ledger,review_result,validation_result,project_state,work_package}.go`, `internal/events/{payloads_task,records}.go`, `internal/controlplane/{batch,service,records}.go`, `internal/principal/facade/{ports,service}.go` (Accept, `CandidateGate`, `GateInput`), `internal/state` task reduction, `prompts/reviewer.md`, `prompts/closure-reviewer.md`, R1 resolver and executor primitives. Write scope: additive records in `internal/protocol` with schemas and fixtures; `internal/actors` (derivation), `internal/reviewexec`, `internal/acceptance`; one additive method on `controlplane.BatchReadView` (Part C, see G-C1); `facade.Accept` enablement; composition in `cmd/devcadence-mcp`; synchronization of PROTOCOLS §§9–11/19, PROJECT_STATE, MCP_API, REVIEW_AND_CONVERGENCE cross-references, schema README. No invariant or catalog change.

Exact clauses: AGENTS §§2–9, 8A, 12–15, 17; ADR-0024 §§3–5; ADR-0010; REVIEW_AND_CONVERGENCE §§1, 6–7, 14; SECURITY §§3, 14–17; DCI-025, 032, 040–049, 080–084, 090, 120–124, 133–135, 159–161. Risks: self-verification, laundered independence, cherry-picked evidence, TOCTOU, authority from conversational claims, unbounded review cost. Re-resolution triggers: a new record kind, a `BatchReadView` change, an `ActorProvenance` change, owner selecting another independence basis, or any request to enable acceptance without a verified policy.

## Scope envelope

Authorized: as in Context Manifest. Forbidden: enabling `accept` by constructor flag, test double, config toggle or owner-edited JSON; averaging or suppressing disagreement; in-place finding closure by the author or worker (DCI-134); a reviewer receiving worker transcripts or reasoning; automatic merge, accepted-commit advancement or integration (acceptance remains separate from integration); a change to `ReviewResult`, `ValidationCompleted`, `ChangeAccepted` payload schemas; M7 campaign orchestration; new MCP tools or grants.

LOCAL_DISCRETION: helper layout, review-prompt wording inside the compiler-admitted lens templates, test helpers, record-id formatting that preserves the exact ids below.

## Requirements and invariants

| ID | MUST requirement | Invariant |
| --- | --- | --- |
| R1 | Every worker, reviewer and verifier invocation has one immutable `InvocationProvenance` record whose `ActorProvenance` is derived from endpoint basis fields by `internal/actors`; model output never supplies identity | I1: independence evidence is canonical and not model-named |
| R2 | The gate recomputes `ActorID` from the stored basis fields and applies `protocol.ActorsIndependent` between the worker and every accepting reviewer; unknown basis field denies | I2: independence is derived, not trusted |
| R3 | Each required dimension is reviewed by its own invocation on an endpoint whose actor differs from the worker's under the policy basis; no independent eligible endpoint denies `MODEL_UNAVAILABLE` | I3: no self-review and no silent downgrade |
| R4 | A reviewer receives only the exact contract, an immutable candidate/diff manifest, validation evidence and its lens template; it never receives worker session text | I4: lineage is empty by construction (DCI-042, DCI-135) |
| R5 | A review result is persisted only from strictly decoded, executor-completed output together with its provenance record, atomically, and only for the current reviewing candidate | I5: model output is evidence, never a state change |
| R6 | `accept` succeeds only through a process-pinned, receipt-verified `AcceptancePolicy` and an in-transaction re-evaluation of complete evidence | I6: acceptance is a deterministic consequence of recorded evidence |
| R7 | Evidence is the complete set for the attempt: the latest validation per required scope and every review per required dimension; any non-pass, missing, errored or unverifiable item blocks; disagreement is never averaged | I7: no cherry-picking; DCI-044 preserved |
| R8 | The gate writes a deterministic `AcceptanceEvidence` record in the same transaction as `ChangeAccepted` and re-verifies it as a precondition | I8: no TOCTOU between check and commit |
| R9 | A finding closes only by a new candidate that passes fresh independent review in M5; the author, worker and acceptance gate never resolve a finding | I9: attempted resolution is not verification (DCI-134) |
| R10 | Acceptance neither merges nor advances accepted source; absence of an active policy, unknown change class or expired policy leaves `accept` denied | I10: acceptance is separate from integration; unknown denies |

## Verified facts and representability gaps

| Fact / gap | Evidence at pinned base | Implication |
| --- | --- | --- |
| `accept` is hard-disabled: after admission it returns `NEEDS_PRINCIPAL` with ref `acceptance-runtime-unavailable`; no `ACCEPTANCE_NOT_IMPLEMENTED` code exists | `facade/service.go` Accept; `TestA10_AcceptIsHardDisabled` | enablement must preserve this denial when no gate is installed |
| `CandidateGate{Check(ctx, BatchReadView, GateInput)}` and `GateInput{Caller,Meta,Candidate,ValidationIDs,ReviewIDs}` exist as an unused reserved seam | `facade/ports.go` | reuse as the transactional guard shape |
| `ReviewResult` has only `ReviewerProfile`/`ModelIdentity` strings; `Attempt` has `WorkerRole/Profile/ModelIdentity` | `protocol/review_result.go`, `tasks/attempt.go` | not stable independent actors; provenance record required |
| `ActorProvenance{ActorID,InvocationID,Role,LineageActorIDs,EndpointRef,SessionRef,ModelRef}`, `Validate(kind, role)`, `ActorsIndependent` (distinct actor, distinct invocation, no lineage overlap) exist | `protocol/review_ledger.go` | reuse unchanged; no schema change |
| `ChangeAccepted` carries `ValidationIDs`, `ReviewIDs`, `UnresolvedDisagreements`, `DecidedBy`; `DecisionAuthority` has `policy` | `events/payloads_task.go`, `protocol/project_state.go` | v1 uses `DecidedBy: policy` and empty disagreements; no payload change |
| `BatchReadView` exposes `ProjectState()` and `Record(kind,id,version)` only; no per-attempt evidence enumeration | `controlplane/batch.go` | **G-C1:** completeness (latest per scope, all reviews) cannot be proven transactionally; additive read method required (decision below) |
| `Command.Records` can attach a record independent of payload references | `controlplane/service.go` | provenance and acceptance evidence attach without payload edits |
| Review ledger records (`ReviewFinding`, resolution, verification) are keyed by `CampaignID`; no durable ledger projection or events | `protocol/review_ledger.go`; `internal/events` | not used for the M5 minimal path; M7 owns it |
| `prompts/reviewer.md`, `closure-reviewer.md` exist | `prompts/` | lens templates; compiler role admission re-verified at step 0 |
| `Severity` is an unordered string enum | `protocol/project_state.go` | policy lists blocking severities explicitly |
| No `InvocationProvenance`, `AcceptancePolicy`, `AcceptanceEvidence` types | repository search | new additive records |

Step 0 re-verifies each row; a false row escalates.

## Part A — Invocation provenance and actor derivation (pure)

~~~go
type ActorBasis struct {                 // non-secret; account_ref is an opaque handle, never an email or key
    EndpointID, ModelID, ModelRevision string
    Provider, ModelFamily, AccountRef  string
}
type InvocationProvenance struct {       // protocol record kind "InvocationProvenance", schema 1.0, version 1
    SchemaVersion, ProvenanceID string   // implementer: "<attempt_id>:implementer"; reviewer: "<review_id>"; verifier: "<run_id>:verifier"
    ProjectID, TaskID, AttemptID, WorkPackageID string
    Role  protocol.ProvenanceRole
    Actor protocol.ActorProvenance        // Validate(kind, Role) must pass
    Basis ActorBasis
    IndependenceBasis string              // "endpoint_model" | "model_family_account"
    EndpointBindingDigest, ContextManifestDigest, PromptDigest string
    CandidateCommit string                // "" for implementer at start; required for reviewer/verifier
    Dimension string                      // reviewer only
    StartedAt string
}
func DeriveActorID(basis string, b ActorBasis) (string, error) // "actor:" + first 24 hex of sha256(canonical{basis, selected fields})
~~~

`endpoint_model` selects `{endpoint_id, model_id, model_revision}`; `model_family_account` selects `{provider, model_family, account_ref}`. Any selected field empty returns an error (unknown denies; never a fallback to the other basis). `InvocationID` is a server-generated unique `inv_` id per invocation. `LineageActorIDs` contains the ActorIDs of every prior invocation whose output *text* the invocation received; the review executor passes none, so reviewer lineage is empty. The record is stored by the invoking component in the same transaction as its first durable event (`AttemptStarted` for workers per R1; `ReviewCompleted` for reviewers). `ActorProvenance.EndpointRef/SessionRef/ModelRef` are filled from the resolved binding, never from model text.

## Part B — Independent review executor

~~~go
type Options struct { /* ControlPlane, Resolver, Drivers, Compiler, Worktrees, Runner, Registry, Profiles(artifacts), Policy ExecutionPolicy source, Clock, IDs, Logger */ }
func New(Options) (*Executor, error) // implements facade.ReviewExecutor; shares R1 primitives and the project lock
~~~

Algorithm `Review(ctx, caller, meta, candidate, dimensions)`:

1. (Sync) Facade already checked admission, prefix, lineage and WP freshness. Re-read the attempt: status `candidate_produced`, task `reviewing`, commit equals `candidate`. Dimensions must be unique values of the closed `protocol.ReviewDimension` set; an empty request uses the active acceptance policy's required set for the task's change class when installed, otherwise the explicitly requested set (non-empty required).
2. Load the worker `InvocationProvenance` (`<attempt_id>:implementer`); derive the worker actor under the active independence basis (the basis comes from the acceptance policy when installed, else `endpoint_model`).
3. For each dimension: resolve a reviewer endpoint with role `reviewer` through the R1 resolver with `ExcludeActorIDs = {worker actor}` (resolver extension: drop endpoints whose derived actor equals an excluded id; unknown basis fields drop the endpoint). None eligible → deny the whole call `MODEL_UNAVAILABLE`, ref `no-independent-reviewer`, zero effects; never reuse the worker's actor.
4. Compile a clean-context pack with the compiler (role reviewer, `CandidateCommit`, `CandidateDiffManifest`, `ValidationSummaries` by digest, exact Work Package contract, the lens template for the dimension). The pack contains no worker prompt, transcript, reasoning or session artifact. Create a read-only worktree id `<attemptID>-r<n>` at the candidate commit; tools are `read_file`, `grep`, `symbols` only; no write handler, no shell, no network.
5. Start the operation via `Registry.Start` (process-scoped). Run one metered invocation per dimension under the same limits/cancellation rules as R1 (limits from the reviewer grant). Output must be a single JSON `ReviewResult` body: strict decode into `protocol.ReviewResult`; the executor then overwrites `ReviewID`, `ProjectID`, `AttemptID`, `WorkPackageID`, `Dimension`, `ReviewerProfile`, `ModelIdentity` from its own trusted values (model-supplied values ignored), requires `Validate()` to pass, and caps findings (≤64, statements ≤2 KiB). Invalid output fails the operation with fixed code `review_output_invalid`; no repair loop, no durable record, no retry (DCI-049).
6. Persist per dimension: `ApplyBatch{ExpectedStateRevision: fresh prefix, Preconditions:[candidateStillReviewingGuard], Commands:[ReviewCompleted{...RecordDigest}], Records:[ReviewResult, InvocationProvenance(reviewer, id = ReviewID)]}`. The guard requires task `reviewing`, attempt and commit unchanged. A concurrent change returns a conflict; the result is dropped, not retried against a new candidate.
7. Dimensions run sequentially by default (bounded concurrency one) and each completed review is durable even if a later one fails. Reviews never edit the candidate, never write findings elsewhere and never resolve earlier findings. Dimension verdicts that disagree remain separate records.

Interpretation recorded for review: a review that fails before `ReviewCompleted` leaves only a process-scoped failed operation (no durable event), because a review has no state transition of its own; a crash mid-review loses that process-scoped result and any uncertain spend is reported `unknown` in the operation, not fabricated.

## Part C — Acceptance policy and transaction-guarded activation

~~~go
type ClassRule struct {
    ChangeClass protocol.ChangeClass
    ValidationScopes []protocol.ValidationScope  // each must have a passing latest validation
    ReviewDimensions []protocol.ReviewDimension  // each must have >=1 review, all verdict pass
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
type ReviewEvidenceRef struct { ReviewID, Digest, Dimension, Verdict, ActorID, InvocationID string; Independent bool }
type ValidationEvidenceRef struct { ValidationID, Digest, Scope, Outcome string; Seq int64 }
type AcceptanceEvidence struct {         // protocol record kind "AcceptanceEvidence", version 1, id "acc:<attempt_id>:<candidate12>"
    SchemaVersion, EvidenceID, ProjectID, TaskID, AttemptID, WorkPackageID string
    WorkPackageVersion int
    CandidateCommit, BaseCommit string
    Policy AcceptancePolicy; PolicyDigest, PolicyReceiptID string
    Worker protocol.ActorProvenance
    Validations []ValidationEvidenceRef  // ordered by scope
    Reviews []ReviewEvidenceRef          // ordered by dimension, ReviewID
    StateRevision string                 // prefix the evaluation read
    Decision string                      // "accept"
}
type Evaluator interface { // pure; same code serves pre-build and in-transaction use
    Evaluate(ctx context.Context, view controlplane.BatchReadView, in facade.GateInput) (AcceptanceEvidence, error)
}
~~~

**G-C1 decision (explicit amendment of WP-M5-1).** Add one read method to `controlplane.BatchReadView`: `AttemptEvidence(ctx, attemptID string) (AttemptEvidenceIDs, error)` returning, from the transaction's journal, the ordered `ValidationCompleted` (id, scope, commit, status, seq, digest) and `ReviewCompleted` (id, dimension, verdict, digest, seq) events correlated with that attempt. Selected over alternatives: (a) trust the principal-supplied `ValidationIDs`/`ReviewIDs` (cherry-picking a stale pass: rejected); (b) read the journal outside the transaction (TOCTOU: rejected). The interface has one in-repo implementation; the change is additive. This amendment needs window-level review before Part C freezes.

Activation: `acceptance.Load(ctx, verifier receipts.Verifier, files)` reads `DEVCADENCE_HOME/config/acceptance-policy.json` and `acceptance-policy.receipt.json` under launch-binding protections, requires a verified `grant` receipt (R4 `acceptance.policy_activate`, subject = canonical policy digest, project-bound), strict-validates the policy, and returns an `Evaluator` pinned for the process lifetime. The `Evaluator` re-verifies the receipt (not revoked/expired) on every `Evaluate`. No policy, invalid policy, unverified or expired receipt means no installed gate: `accept` keeps today's denial (`acceptance-runtime-unavailable`).

`Evaluate` algorithm (every failure is a fixed denial ref; none writes):

1. Receipt/policy valid and `NotBefore <= now <= NotAfter`; `view.ProjectState()` task exists in state `reviewing`; the attempt equals `GateInput.Candidate.Attempt`, status `candidate_produced`, commit equal; Work Package id/version/digest equal the task's approved tuple; when `Git.AcceptedCommit` is set it equals the Work Package base (candidate not stale).
2. `ClassRule` for the task's change class, else `acceptance-unknown-change-class`.
3. `AttemptEvidence` for the attempt. For each required scope: the highest-`Seq` validation for that scope on that commit must exist and have outcome `pass`, and its `ValidationResult` record (via `view.Record`, digest-verified) must name the same commit; any later non-pass supersedes an earlier pass.
4. For each required dimension: at least one `ReviewCompleted` for the attempt; **every** such review must have verdict `pass`; each review's `ReviewResult` record is loaded, digest-verified, bound to the attempt/WP/dimension; no finding in it has a severity in `BlockingSeverities`; its reviewer `InvocationProvenance` (id = review id) exists, role `reviewer`, `CandidateCommit` equals the candidate, `DeriveActorID(policy.basis, stored basis)` equals `Actor.ActorID`, and `protocol.ActorsIndependent(worker, reviewer)` holds. Any `concern`, `fail`, `unable_to_verify`, missing record or failed derivation blocks.
5. The accepted evidence is exactly the supplied `ValidationIDs`/`ReviewIDs` set: each supplied id must be in the enumerated evidence and every enumerated latest/required item must be supplied; a mismatch denies (the principal cannot omit an inconvenient review).
6. Build `AcceptanceEvidence` in canonical order with the policy copy and digests.

`facade.Accept` with an installed gate: admission, prefix and lineage checks as today; `Evaluate` outside the transaction; then one `ApplyBatch{ExpectedStateRevision: meta, Preconditions:[guard{Evaluate again and require canonical-digest equality with the pre-built evidence}], Commands:[ChangeAccepted{... DecidedBy: policy, UnresolvedDisagreements: empty} + Records:[AcceptanceEvidence]]}`. `Reject` is unchanged. Acceptance does not touch Git, integration state or `Git.AcceptedCommit`.

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
| Review output invalid / endpoint fails | no durable record; operation failed | fixed code |
| Review commit conflicts | result dropped | conflict error |
| Ambiguous accept commit | read state/record before retry | `AcceptanceEvidence` id |

## Traceability and acceptance scenarios

| ID | Setup → action → expected | Requirement → invariant → representation |
| --- | --- | --- |
| A1 | endpoint basis → derive twice → identical id; different model revision → different id | R1 → I1 → `DeriveActorID` |
| A2 | missing family/account under `model_family_account` → derivation error, no fallback | R1/R2 → I1/I2 |
| A3 | model output claims an actor/profile → stored identity unchanged | R1 → I1 |
| A4 | provenance record fails `Validate(kind, role)` or is a duplicate id → refused | R1 → I1 → record |
| B1 | only the worker's actor available → `MODEL_UNAVAILABLE`, zero calls/effects | R3 → I3 |
| B2 | reviewer context pack inspected → no worker prompt/transcript; lineage empty | R4 → I4 |
| B3 | malformed or extra-field model output → no `ReviewCompleted`, fixed code, no retry | R5 → I5 |
| B4 | valid output → `ReviewResult` + provenance + `ReviewCompleted` atomically, executor-set identity fields | R5 → I5 |
| B5 | candidate changes mid-review → conflict, nothing persisted | R5 → I5 |
| B6 | review limit exceeded or cancel → process-scoped failure, no durable record, session closed | R5 → I5 |
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
| C11 | no new tool/grant; `reject` behavior unchanged | scope |

Live review and live acceptance are operator acceptance steps after OWNER INPUT-2/3; test-driver runs never claim them.

## Validation and Mutation Catalog

`go test -count=1 -race ./internal/actors/... ./internal/reviewexec/... ./internal/acceptance/... ./internal/controlplane/... ./internal/principal/... ./tests/...`; `make schemas`; `make docs-check`; `make verify`. Record versions, fixtures, spy counts and exit statuses.

| Mutant | Expected failure |
| --- | --- |
| Actor id from model output or from the stored field without recomputation | A3/C6 |
| Fallback between independence bases | A2 |
| Reviewer resolution not excluding the worker actor | B1 |
| Worker transcript included in reviewer pack | B2 |
| Persist invalid or partially decoded output; keep model identity fields | B3/B4 |
| Persist review for a changed candidate | B5 |
| `accept` enabled when policy missing or by a test fake | C1 |
| Evaluate outside the transaction only | C9 |
| Latest-validation rule uses first pass | C4 |
| Single passing review suffices when another dimension review failed | C5 |
| Supplied ids not compared to enumerated set | C7 |
| Skip independence or lineage check | C6 |
| Accept advances accepted commit or merges | C2 |
| Unknown class or expired policy allowed | C8 |

Independent lenses: Contract/Authority; Test Adequacy/Mutation. Part B authors do not review Part C.

## Rationale, escalation and readiness

Selected: re-evaluate pure evidence inside the acceptance transaction with an additive enumeration read, versus a persistent "review state" projection (larger state surface, duplicates the M7 ledger). Provenance is a record, not a payload field, to avoid event schema changes. Rejected: trusting reviewer-profile strings as independence; letting the principal's id list define completeness; configuring acceptance through an environment toggle.

Escalate on: the owner needs acceptance with one actor before C is specified; the `BatchReadView` amendment is not authorized; an `ActorProvenance` or `ReviewResult` change is needed; a record kind registry cannot accept the new kinds additively; any request to review with worker lineage.

Implementation Readiness Report:

~~~text
requirements represented: 10/10
state transitions specified: 4/4 (provenance write, review persist, evaluate, accept)
failure cases specified: 9/9
authority decisions specified: 5/5
missing/unknown input semantics: 4/4
acceptance scenarios mapped: 4 (A) + 6 (B) + 11 (C) = 21/21
unresolved architecture choices: 2 (OWNER INPUT-3; G-C1 amendment authorization)
readiness: NOT_READY pending window review, owner inputs, R1 freeze for Part B and R4 freeze for Part C
~~~

Weaker-implementer check: Part A yes; Part B yes once R1 primitives are frozen; Part C no until G-C1 and OWNER INPUT-3 are resolved.

## Changelog

- r1: initial draft for window 2026-10-G.
