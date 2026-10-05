# WP-M5-R1 — Native task executor

## Identity

- Revision: 1; task: task-m5-r1-native-task-executor; window: [2026-10-G](window-2026-10-g-overview.md).
- Base: `71bdaec6d6d81c1b6e52d8b30f0f8485f928925a` (`main` `cebb4f0` plus the PR #83 reconcile merge, 2026-10-05).
- Contract digest: reviewed immutable Git blob. No fictitious runtime state revision; record the actual accepted dependency commits at execution.
- Endpoint: competent Go implementer with Git, process-control and SQLite-transaction skill; complete admission of this contract is mandatory. If it does not fit, split at the Part A / Part B line rather than truncate.
- Status: **DRAFT / NOT_READY / NOT FROZEN. No implementation authority; no endpoint call, credential or spend is authorized.** Part B live use is BLOCKED on OWNER INPUT-2.
- Dependencies: WP-M5-1/2 (merged: `ApplyBatch`, `AuthorizedTask`, `TaskExecutor`, `OperationRegistry`); [M5-R2](wp-m5-r2-independent-review-acceptance-ewp.md) Part A (invocation provenance record and actor derivation; pure, implemented first); [M5-R4](wp-m5-r4-protected-operator-ingress-ewp.md) Part A (policy activation receipt). Production driver clients come from [M5-R3](wp-m5-r3-empirical-verifier-composition-ewp.md) Part A; until then Part B runs only against test drivers and cannot be called live.
- Parts (separately freezable): **A** `ExecutionPolicy` and `EndpointResolver` (pure, offline). **B** worktree, session, candidate, validation and recovery lifecycle.

## Objective

Implement the facade's `TaskExecutor` port so that an approved Work Package becomes a real attempt: a compiler-admitted worker invocation on an eligible, policy-granted endpoint, inside an isolated worktree, producing an immutable candidate commit, with every policy-significant outcome recorded as typed control-plane events. Executor-private state (lock files, the in-memory handle map) is never authority (DCI-159–161): deleting it changes no canonical outcome.

## Context Manifest

Role: bounded Go implementer; independent Contract/Authority and Failure-semantics reviewers. Read envelope: `internal/principal/facade/{ports,operations,service}.go` (signatures only), `internal/controlplane/{batch,service,operations}.go`, `internal/worktrees`, `internal/repository`, `internal/process`, `internal/validation/{record,run,profiles}.go`, `internal/cognition/{routing,portfolio_activation}.go`, `internal/cognition/compiler/compile.go` (`CompileRequest`, `CompileInvocation`), `internal/cognition/drivers/{driver,types,metering,mediation}.go`, `internal/tools`, `internal/events/payloads_task.go`, `internal/tasks`. Write scope: new `internal/taskexec/` and `internal/execpolicy/`, composition in `cmd/devcadence-mcp`, additive docs. No change to facade ports, event payloads, task state machine or invariants.

Exact clauses: AGENTS §§2–9, 12–15, 17; ADR-0024 §§3–5; SECURITY §§3, 5–8, 14–17; DCI-025, 030–033, 040–041, 080–084, 120–124, 129, 133, 159–161; WP-M5-1 §Guards; WP-M5-2 §Exact ports and §Launch. Risks: unauthorized source exposure or spend, crash between intent and effect, executor-private authority, worktree escape, candidate outside scope, double execution. Re-resolution triggers: a facade port or `controlplane` signature change, a missing production driver, a new credential path, a task-state or event change, or an endpoint grant the owner has not made.

## Scope envelope

Authorized: `internal/execpolicy` (policy types, strict decoding, receipt-verified loader); `internal/taskexec` (resolver, executor, worktree/session/candidate/validation/recovery, mediated worker tools); composition of `facade.Options.Tasks` in `cmd/devcadence-mcp`; synchronization of MCP_API (delegate/validate availability), PRINCIPAL_HOSTS, PROJECT_STATE and the schema README.

Forbidden: shell execution for the worker; the worker running tests (validation is DevCadence's controlled runner only, Part B); network or credential access other than through a `DriverFactory` the policy names; merging, advancing the accepted commit or pushing (DCI-031); a hidden endpoint fallback or metered fallback (DCI-122); writing any event not listed here; changing `facade` behavior for acceptance; any model-supplied path, tool, endpoint, limit or policy field affecting authority.

LOCAL_DISCRETION: private helper names, package-local structs, test helpers, the layout of the executor's lock/scratch directory, `git` flag ordering that preserves the exact semantics below.

## Requirements and invariants

| ID | MUST requirement | Invariant |
| --- | --- | --- |
| R1 | Before any effect re-check, in this order: single-owner lock, task/Work Package freshness and attempt budget, current `ExecutionPolicy`, endpoint eligibility, compiler admission. Any failure returns an error with zero events, worktrees, driver calls | I1: absence of policy, endpoint, budget or context fit denies; it never degrades |
| R2 | Record `TaskDelegated` and `AttemptStarted` in one `ApplyBatch` with the WP-M5-1 start-execution guard, before the worktree and model call | I2: intent is durable before any external effect; delegation without an attempt cannot commit |
| R3 | Resolve the endpoint only from the active portfolio filtered by the policy allow-list, with exact locality, source exposure, access channel, health and auth; never fall back at runtime | I3: placement and economics come from granted policy (DCI-120–124), not from the model or availability luck |
| R4 | The worker runs in a fresh isolated worktree at the Work Package base with compiler-admitted context and a mediated tool set whose writes are confined to the declared write scope | I4: scope cannot silently expand (DCI-025, DCI-030) |
| R5 | A candidate is committed by the executor only when every changed path is inside the write scope; the commit's sole parent is the exact base; it records only typed `CandidateProduced` | I5: every candidate has lineage and bounded scope (DCI-032) |
| R6 | Every attempt ends in exactly one durable outcome: `CandidateProduced`, `AttemptFailed` (with closed reason), or `AttemptBlocked`; none is invented by recovery as success | I6: no silent, partial or fabricated outcome |
| R7 | Cancellation and crash are bounded: cancellation records `AttemptFailed{Cancelled:true}` under a fresh bounded context; a restarted owner closes orphaned running attempts as `executor_lost` and never replays model calls | I7: uncertain external effects are recorded, not retried |
| R8 | `Validate` runs the project's deterministic profile in a separate clean worktree at the candidate commit through the existing `validation.ExecuteAndRecord` | I8: deterministic validation is authoritative for deterministic claims (DCI-041); the worker never validates itself |
| R9 | Per-attempt limits (turns, tokens, tool calls, wall time, spend) are required and nonzero; zero is no authority | I9: metering is a pre-call control, not a post-hoc report |
| R10 | The worker's invocation is bound by a canonical provenance record whose actor identity the executor derives from the resolved endpoint binding | I10: independence evidence is never model-named (see R2-A) |

## Verified facts and representability gaps

| Fact / gap | Evidence at pinned base | Implication |
| --- | --- | --- |
| `TaskExecutor{Delegate,Validate}` and `AuthorizedTask` exist; nil denies `MODEL_UNAVAILABLE` | `internal/principal/facade/ports.go`, `service.go` Delegate/Validate | the executor is a plain implementation; the facade pre-checks prefix, approved tuple and lineage |
| Facade does no state transitions of its own for delegate/validate | `service.go` (Delegate calls `Tasks.Delegate` then `checkOperation`) | the executor owns `TaskDelegated`, `AttemptStarted`, `CandidateProduced`, `AttemptFailed`, `ValidationCompleted` |
| `OperationRegistry.Start(project, kind, deadline, fn)` gives `fn` its own context; `Wait` yields after 10 s | `facade/operations.go` | synchronous phase returns errors; asynchronous phase is the registry function |
| `ApplyBatch` with `WorkPackageGuard`, `Preconditions`, `Postconditions`, `Records` per member | `controlplane/batch.go` | delegate + attempt-start batch; candidate commit with a lineage precondition |
| Worktree manager: `Create(Spec{ProjectID,TaskID,AttemptID,BaseCommit})`, `Cleanup`, `Recover`, `LeakedGitWorktrees`; ids are validated path components | `internal/worktrees/worktrees.go` | worktree id is derived from the attempt id; no caller path |
| No repository function creates commits | `internal/repository/*.go` (read/diff/merge-base only) | candidate commit is new executor code through `process.Runner` |
| `process.Runner.Run(Spec{Executable,Args,Dir,Env,Timeout})`: no shell, no implicit env, timeout required | `internal/process/process.go` | all Git calls use it |
| Session drivers exist only as `DirectAPIDriver` (needs a `DirectAPIClient`), `CLIWrapperDriver` (needs a `CommandRunner`/mapper) and a fake; no real provider client | `internal/cognition/drivers/*`, `remoteapi`, `ollama` (probes only) | `DriverFactory` is a port; production implementations are R3-A |
| `MeteredDriver` with `MeterLimits` (zero fields are omitted); loop detector | `drivers/metering.go` | executor rejects any zero required limit before passing it |
| `ScopedToolMediator` registers named handlers; `tools` has read/grep/symbols, no write tool | `drivers/mediation.go`, `internal/tools` | executor adds one confined `write_file` handler |
| `ValidationCompleted` via `validation.ExecuteAndRecord` binds commit, clean tree and HEAD before and after the run; uses `Service.Apply` | `internal/validation/record.go` | reuse unchanged; not CAS-guarded, benign for an immutable commit; reducer rejects it for a task no longer validating |
| `cognition.Route(requirement, policy, endpoints) Decision` and `ActivationManager.GetActivePortfolio` | `internal/cognition/{routing,portfolio_activation}.go` | resolver ranks only endpoints already allowed by policy |
| `compiler.Compiler.CompileInvocation(CompileRequest)` | `compiler/compile.go` | prompt and manifest produced before commit; failure is `CONTEXT_UNFIT`-class, no attempt |
| No `ExecutionPolicy` or source/spend grant record exists | repository search | new, receipt-verified (R4 purpose `execution.policy_activate`) |

Step 0 re-verifies every row; a false row escalates.

## Part A — ExecutionPolicy and resolver (pure)

Part A types (`ExecutionPolicy`, `EndpointGrant`, `ExecutionLimits`, `EndpointRequest`, `ResolvedEndpoint`, `EndpointResolver`, `DriverFactory`) live in `internal/execpolicy`, below the executor, so provider composition ([R3-A](wp-m5-r3-empirical-verifier-composition-ewp.md)) and the review executor depend on them and not on `internal/taskexec`.

~~~go
type ExecutionLimits struct {
    MaxTurns, MaxToolCalls int
    MaxTotalTokens int64
    MaxDurationSeconds int
    MaxAPISpendUSD float64   // 0 = no metered authority
}
type EndpointGrant struct {
    EndpointID, ModelID string
    Roles []string                      // "implementer","reviewer" (R2 uses reviewer)
    Locality protocol.Locality          // must equal observed
    SourceExposure protocol.SourceExposure // ceiling for this endpoint
    AllowedNetworkDomains []string      // empty for local
    ChannelKind protocol.ChannelKind    // must equal the observed channel
    AllowUnknownSubscriptionQuota bool
    Limits ExecutionLimits
}
type ExecutionPolicy struct {
    Version  string // "1.0"
    PolicyID string
    Revision int
    NotBefore, NotAfter string // RFC3339 UTC, validity <= 30 days
    Grants []EndpointGrant        // unique (EndpointID, ModelID); sorted
}
type EndpointRequest struct {
    ProjectID, TaskID, Role string
    ContextNeed protocol.SourceExposure
    PortfolioDigest string
}
type ResolvedEndpoint struct {
    EndpointID, ModelID, ModelRevision, DriverID string
    Kind protocol.EndpointKind
    Locality protocol.Locality
    Channel protocol.AccessChannel
    ContextProfile protocol.ContextProfile
    Limits ExecutionLimits
    PolicyDigest, PortfolioDigest, BindingDigest string // BindingDigest: canonical digest of this struct without secrets
}
type EndpointResolver interface { Resolve(context.Context, EndpointRequest) (ResolvedEndpoint, error) }
type DriverFactory interface { Open(context.Context, ResolvedEndpoint) (drivers.SessionDriver, error) }
~~~

Policy loading: strict decode of `DEVCADENCE_HOME/config/execution-policy.json` under the same protections as the launch binding, plus a `grant` receipt (R4 `execution.policy_activate`, subject = canonical policy digest, project-bound) from `config/execution-policy.receipt.json`. Loaded once per process; pinned for its lifetime (policy change = relaunch). Missing/invalid/expired/unverified policy means resolver returns `MODEL_UNAVAILABLE` with ref `execution-policy-unavailable`.

Resolution algorithm: (1) active portfolio via `GetActivePortfolio`; none → deny. (2) Candidate = every portfolio endpoint with a policy grant matching `EndpointID`, `ModelID`, role. (3) Drop endpoints that are unhealthy, not authenticated (opaque status only; provider text is not authority), whose observed `Locality`/`ChannelKind` differ from the grant, whose `RequiredSourceExposure` exceeds `min(grant.SourceExposure, request.ContextNeed ceiling)` (`ExposureRank`), or whose economics lack the grant's caps: `ChannelKind` metered needs `MaxAPISpendUSD > 0` and independently enforceable per-call bounds (unknown price → drop); subscription needs `MaxDurationSeconds` and call caps, and unknown quota only with `AllowUnknownSubscriptionQuota`; local needs wall-time and token caps. (4) Rank survivors with `cognition.Route` using the portfolio policy. (5) No survivor → `POLICY_DENIED`/no-eligible-endpoint with the dropped reasons as fixed codes. Same inputs give the same output; there is no runtime fallback to a second endpoint.

## Part B — Executor lifecycle

~~~go
type Options struct {
    ControlPlane *controlplane.Service
    Worktrees *worktrees.Manager
    Repositories RepositoryProvider      // project id -> *repository.Repository
    Runner *process.Runner
    Registry *facade.OperationRegistry
    Resolver EndpointResolver
    Drivers DriverFactory                 // nil => MODEL_UNAVAILABLE
    Compiler *compiler.Compiler
    Profiles ProfileSource                // profile id -> validation.Profile
    Artifacts ArtifactSink                // store diff/usage artifacts, return protocol.ArtifactRef
    StateDir string                       // lock + scratch; never authority
    Clock clock.Clock; IDs ids.Source; Logger *slog.Logger
}
func New(Options) (*Executor, error) // takes the project lock; Executor implements facade.TaskExecutor
~~~

Constructor refusals: nil ControlPlane/Worktrees/Runner/Registry/Resolver/Compiler/Profiles. Actor for events: `protocol.Actor{Kind: protocol.ActorControlPlane, ID: "taskexec"}`.

### Algorithm: single owner and recovery

`New` takes an exclusive `flock` on `StateDir/<project>.lock`; failure to acquire denies all `Delegate`/`Validate` with `MODEL_UNAVAILABLE` (`executor-owned-elsewhere`) and performs no recovery. Holding the lock proves no other executor owns running attempts; the lock is liveness, never authority (the OS releases it on crash). On acquisition run `Recover` once: for each task in `running` with an attempt `running`, read the current prefix and `ApplyBatch` `AttemptFailed{Summary:"executor_lost; external effects uncertain", Cancelled:false}` guarded on the attempt still running; never call a model, never recreate the worktree, never retry. A worktree left behind stays registered (`Cleanup` refuses dirty trees); only clean orphan worktrees whose attempt is terminal are cleaned. Non-POSIX platforms: refuse construction.

### Algorithm: Delegate

1. (Sync) Re-read task and the stored Work Package record (digest-verified); require state `ready`, tuple equal to `AuthorizedTask.WorkPackage`, attempts < `MaxAttempts`, no running attempt.
2. `ep := Resolver.Resolve(...)`; role `implementer`; `ContextNeed` from the Work Package's declared exposure (absent → `focused_snippets` ceiling intersected with grant, never wider than the grant).
3. `compiled := Compiler.CompileInvocation(CompileRequest{Role:"implementer", ExecutionContract: <exact stored contract text>, BaseCommit, WriteScope, ReadEnvelope, ContextProfile:&ep.ContextProfile, AccessChannel:&ep.Channel, Tools:<worker tools>, ProjectStateRevision: meta prefix ...})`. A compile error returns before any event.
4. `attemptID := IDs.New`; `worktreeID := attemptID`; build the worker `InvocationProvenance` record (R2-A) from `ep.BindingDigest`, `compiled` manifest digest and `attemptID`.
5. `ApplyBatch{ExpectedStateRevision: meta, WorkPackage: guard, Commands:[TaskDelegated, AttemptStarted{ModelIdentity: ep.EndpointID+"/"+ep.ModelID+"@"+ep.ModelRevision, WorktreeID, WorkerRole:"implementer"}] }` with the provenance record stored on the `AttemptStarted` member. Stale prefix/tuple → the WP-M5-1 conflict error, zero effects.
6. `Registry.Start(project,"delegate",deadline=ep.Limits.MaxDurationSeconds+60s, run)` and return its `OperationRef`. The registry function `run` is below. If `Start` fails (registry full) record `AttemptFailed{Summary:"operation_registry_full"}` immediately.

### Algorithm: run (asynchronous, owns the attempt)

1. `Worktrees.Create(Spec{Project,Task,AttemptID,Base})`; error → `AttemptFailed("worktree_unavailable")`.
2. `d := Drivers.Open(ep)` (nil/err → `AttemptFailed("endpoint_unavailable")`, usage unknown but no call made); `md := drivers.NewMeteredDriver(d, limitsFrom(ep.Limits))`; reject if any required limit is zero before this line.
3. `s := md.StartSession(SessionConfig{SessionID: attemptID+"-s1", ModelID: ep.ModelID, SystemPrompt: compiled prompt, Tools: workerTools, WorktreeScope: scope, Mediator: ScopedToolMediator})`. Worker tools are exactly `read_file`, `grep`, `symbols` (existing `internal/tools`, worktree-scoped) and `write_file`: relative path inside `WriteScope` (via `compiler.IsPathAuthorized`) after `Scope.ResolvePath` (symlink-safe), ≤256 KiB per file, ≤64 files per attempt, UTF-8 text only, never creating a symlink, never touching `.git`. No shell, no network, no test runner. A `PausedReason`, meter suspension or loop detection ends the attempt `AttemptFailed("limit_reached")`.
4. Turn loop: at most `MaxTurns` `ExecuteTurn` calls with the compiled prompt then driver-managed tool results; stop at the first turn with no tool calls and no `PausedReason`. Context cancellation or deadline calls `s.Close` under a fresh 5 s context and ends the attempt (see cancellation).
5. Materialize the candidate (executor-owned, via `Runner`, argv only, env `GIT_CONFIG_GLOBAL`/`GIT_CONFIG_SYSTEM` null, `core.hooksPath` empty, `commit.gpgsign=false`, fixed author/committer `DevCadence executor <executor@devcadence.invalid>`): `git status --porcelain=v2 -z` to list tracked, untracked, renamed, deleted and mode-changed paths; refuse any path outside `WriteScope`, any symlink or submodule change, any `.git` path, any change count/size above 64 files / 2 MiB patch → `AttemptFailed` with reason `scope_violation` or `candidate_too_large` and the offending class (never the content). No change → `AttemptFailed("no_change")`. Otherwise `git add -- <exact paths>`, `git commit` with fixed message `devcadence attempt <attemptID>`, `git rev-parse HEAD`; require one parent equal to the base and re-list `git diff --name-status base..HEAD` against the scope.
6. Store artifacts (unified diff, driver usage JSON with measured/unknown flags, no prompt secrets). `ApplyBatch{ExpectedStateRevision: fresh prefix, Preconditions:[attemptStillRunningGuard], Commands:[CandidateProduced{CandidateCommit, Summary:"candidate produced: n files", Artifacts}]}` where the guard requires attempt `running`, worktree id equal and task `running`.
7. Result handle: `candidate:<attemptID>`. The worktree is retained for review/hygiene; the executor never cleans a worktree holding a candidate.

### Algorithm: failure and cancellation

Every `AttemptFailed` is a typed event with closed `Summary` prefix reasons: `worktree_unavailable`, `endpoint_unavailable`, `limit_reached`, `scope_violation`, `candidate_too_large`, `no_change`, `driver_error`, `cancelled`, `executor_lost`, `operation_registry_full`; `Cancelled:true` only for `cancelled`. Error text from a driver or provider is never copied into the summary or logs (fixed codes and counts only, DCI-083). On cancellation or deadline: close the session within 5 s, kill any child process group started by the executor, append the failure event under a fresh 10 s context (retry once on transient busy), leave the worktree. If the append fails, the attempt stays `running` until the next owner's `Recover`; the operation returns `OPERATION_LOST`-class status, never success. Meter-observed usage is recorded as measured only when the driver reports it; otherwise `unknown`.

### Algorithm: Validate

Candidate lineage and prefix were checked by the facade. The executor re-reads the attempt (must be `candidate_produced`, commit equals `CandidateRef`), resolves `profileID` through `Profiles` (unknown → `NOT_FOUND`), creates a separate clean worktree id `<attemptID>-v<n>` at the candidate commit (`n` = recorded validations + 1), and calls `validation.ExecuteAndRecord` with `Subject` bound to the attempt/candidate, `Commit` = candidate and the controlled runner. A dirty tree, HEAD mismatch or profile with network/shell commands refuses; the validation worktree is cleaned when clean. The operation result handle is the `ValidationCompleted` validation id. Validation never edits the candidate.

## OWNER INPUT-2 — endpoints, source exposure and spend

Decision owner: repository owner. **Safe default: deny** (no `ExecutionPolicy`, so no executor effect and no live use; zero cap means no authority). Compare:

| Criterion | A. Loopback local runtime only (e.g. an already-installed local model) | B. A plus one authenticated subscription CLI endpoint | C. B plus one metered remote API endpoint |
| --- | --- | --- | --- |
| Source leaves the machine | no | yes, mediated by CLI worktree access | yes, windowed context |
| Spend/quota risk | local compute time only | quota unknown unless provider exposes a meter | metered dollars; needs verified per-call upper bound |
| Cap enforceability here | wall time, tokens, tool calls | call count and wall time; not quota | requires price and token bounds the driver can enforce |
| Supports the three M4 tiers | one | two | three |
| New client code | local chat client only | CLI mapper | provider client |

**Recommendation: A first** for R1/R2 integration and smoke; add B and C only by naming each endpoint, model, source exposure ceiling, network domains and caps in the signed policy. Required answers: which endpoints and models, maximum `SourceExposure` per endpoint, any allowed network domains, per-attempt caps, whether unknown subscription quota is permitted. **Remains blocked until answered:** any live task execution, R2 live review, R3 provider composition beyond tests. Offline Part A and all test-driver coverage proceed.

## Authority matrix

| Effect | Authority | Forbidden substitute |
| --- | --- | --- |
| Start an attempt | Facade admission + WP-M5-1 guard + signed `ExecutionPolicy` + eligible endpoint | grant presence alone, model request, cached discovery |
| Use remote endpoint / spend | Named grant with caps in the policy | availability, API key presence, default routing |
| Create a candidate commit | Executor after scope check | worker-run git, model-supplied path |
| Advance accepted state | none (R2 and operator integration only) | executor |

## Missing / unknown / stale and failure semantics

| Input | Missing | Unknown | Stale | Malformed |
| --- | --- | --- | --- | --- |
| Policy/receipt | `MODEL_UNAVAILABLE`, zero effects | n/a | expired: deny | `INTEGRITY`, deny |
| Portfolio / endpoint health | deny | unknown health/auth: not eligible | re-resolve each Delegate | refuse |
| Cap / price | no authority | unknown price: not eligible | policy relaunch | refuse |
| Task/WP/prefix | facade error | n/a | `STALE_*`, zero effects | `INVALID_ARGUMENT` |
| Usage | recorded `unknown` | not zero | n/a | n/a |

| Failure boundary | Required postcondition | Recovery |
| --- | --- | --- |
| Before batch (resolve, compile, policy) | zero events/worktrees/driver calls | principal retries |
| Batch fails | zero events | stale error |
| Worktree create fails after batch | `AttemptFailed(worktree_unavailable)`; no driver call | new Delegate |
| Crash after batch, before candidate | next owner: `AttemptFailed(executor_lost)`; no replay | new Delegate by principal |
| Crash after commit, before `CandidateProduced` | orphan commit in worktree only; attempt closed `executor_lost`; no candidate | new attempt |
| `CandidateProduced` append fails/ambiguous | read state first; if absent the attempt is closed failed by recovery | no duplicate model call |
| Driver call in flight at cancel | usage unknown; failure recorded cancelled | operator reconciliation if spend uncertain |

## Traceability and acceptance scenarios

| ID | Setup → action → expected | Requirement → invariant → representation |
| --- | --- | --- |
| A1 | repo + fake driver editing in scope → delegate → attempt, worktree, one-parent candidate at base, `CandidateProduced`, task `validating` | R1–R6 → I1–I6 → batches, commit algorithm |
| A2 | stale prefix or tuple → delegate → zero events, worktrees, driver calls (spies) | R1/R2 → I1/I2 |
| A3 | no policy / expired / bad receipt / no eligible endpoint → zero effects, `MODEL_UNAVAILABLE`/`POLICY_DENIED` | R1/R3 → I1/I3 |
| A4 | remote or metered endpoint without grant/cap → not eligible, no fallback | R3 → I3 |
| A5 | compile failure (`CONTEXT_UNFIT`) → zero events | R1 → I1 |
| A6 | worker writes outside scope, symlink, `.git`, traversal → no candidate, `scope_violation` | R4/R5 → I4/I5 |
| A7 | no change / oversize → failed with closed reason | R5/R6 → I5/I6 |
| A8 | meter limit or loop → `limit_reached`, no candidate | R9 → I9 |
| A9 | cancel mid-turn → session closed in 5 s, `AttemptFailed{Cancelled}` from a fresh context | R7 → I7 |
| A10 | kill after `AttemptStarted` → restart → `executor_lost`, zero model calls, no replay | R6/R7 → I6/I7 |
| A11 | second executor instance for the project → denied, other instance's attempts untouched | R7 → I7 |
| A12 | validate: separate clean worktree, dirty tree refused, `ValidationCompleted` digest matches | R8 → I8 |
| A13 | delete lock/scratch state → canonical outcomes unchanged; executor cannot append undeclared events (package boundary test) | DCI-160 |
| A14 | worker provenance record present with endpoint-derived actor id; model output cannot alter it | R10 → I10 |
| A15 | canary secret in driver error/env → absent from events, artifacts and logs | DCI-081/083 |

A live run against a real endpoint is a separate operator acceptance step after OWNER INPUT-2; test-driver runs never claim it.

## Validation and Mutation Catalog

`go test -count=1 -race ./internal/taskexec/... ./internal/execpolicy/... ./internal/principal/... ./cmd/devcadence-mcp/... ./tests/...`; `make schemas`; `make docs-check`; `make verify`. Record versions, spy call counts and exit statuses. Tests use real temporary Git repositories and the existing `fake_driver`; no paid or networked test.

| Mutant | Expected failure |
| --- | --- |
| Worktree or driver call before the batch commits | A2 |
| Missing policy treated as permissive; zero cap treated unlimited | A3/A8 |
| Fallback to another endpoint or channel | A4 |
| Skip compile admission | A5 |
| Scope check only on pre-commit, or ignores symlink/.git/rename | A6 |
| Empty diff committed | A7 |
| Cancellation uses the cancelled context to append | A9 |
| Recovery retries the model or marks success | A10 |
| Lock absent: two owners | A11 |
| Validate in the worker worktree or dirty tree accepted | A12 |
| Executor state (lock/handle map) decides an outcome | A13 |
| Actor id read from model output | A14 |
| Provider error copied into summary/log | A15 |

Independent lenses: Contract/Authority; Failure-semantics and Test Adequacy/Mutation. The author does not review.

## Rationale, escalation and readiness

Selected: commit intent first (`TaskDelegated`+`AttemptStarted`), then effects, with a single-owner lock deciding orphan status, versus creating the worktree and calling the model before committing (a stale prefix would then leave an unrecorded paid call) and versus a durable executor job queue (new authority-bearing state, rejected by DCI-160). Cost: a crash leaves a recorded failed attempt rather than a clean slate, which is the honest outcome.

Escalate on: a facade/controlplane signature drift; `ValidationCompleted` not reducing for the task state; driver contract lacking a way to close an in-flight session; no safe way to confine `write_file`; owner wanting concurrent executors or Windows.

Implementation Readiness Report:

~~~text
requirements represented: 10/10
state transitions specified: 6/6 (delegate, start, candidate, fail, cancel, recover) plus validate
failure cases specified: 14/14
authority decisions specified: 4/4
missing/unknown input semantics: 5/5
acceptance scenarios mapped: 15/15
unresolved architecture choices: 1 (OWNER INPUT-2 for live use only)
readiness: NOT_READY pending window review, R2-A and R4-A freezes, and current-base gate
~~~

Weaker-implementer check: yes for the contract as written, assuming step-0 facts; live behavior is blocked on owner policy, not on invention.

## Changelog

- r1: initial draft for window 2026-10-G.
