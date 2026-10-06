# WP-M5-R1 — Native task executor

## Identity

- Revision: 4 (focused contract repair after independent review of r3 head `0da5013`; verdicts pending clean re-verification); task: task-m5-r1-native-task-executor; window: [2026-10-G](window-2026-10-g-overview.md).
- Base: `71bdaec6d6d81c1b6e52d8b30f0f8485f928925a` (`main` `cebb4f0` plus the PR #83 reconcile merge, 2026-10-05).
- Contract digest: reviewed immutable Git blob. No fictitious runtime state revision; record the actual accepted dependency commits at execution.
- Endpoint: competent Go implementer with Git, process-control and SQLite-transaction skill; complete admission of the Part being executed is mandatory. The contract is split into three separately freezable Parts (A, B, C) so that each fits one endpoint session; never truncate a Part.
- Status: **DRAFT / NOT_READY / NOT FROZEN. No implementation authority; no endpoint call, credential or spend is authorized.** Parts B and C live use is BLOCKED on OWNER INPUT-2.
- Dependencies: WP-M5-1/2 (merged: `ApplyBatch`, `AuthorizedTask`, `TaskExecutor`, `OperationRegistry`); [M5-R2](wp-m5-r2-independent-review-acceptance-ewp.md) Part A (`internal/actors`: `ActorBasis`, `DeriveActorID`; **Part A of this EWP imports it**, because the resolver compares excluded `ActorBasis` values and the executor derives actor ids); [M5-R4](wp-m5-r4-protected-operator-ingress-ewp.md) Part A (`receipts.Verifier`; **Part A of this EWP imports it** for the policy loader). Production driver clients come from [M5-R3](wp-m5-r3-empirical-verifier-composition-ewp.md) Part A; until then Parts B/C run only against test drivers and cannot be called live. [M5-R3](wp-m5-r3-empirical-verifier-composition-ewp.md) **Part 0** (driver usage-knownness amendment) must be frozen before Part B (the meter and the usage artifact use `TokenMeasurement`).
- Freeze order: R4-A, R2-A and R3-0 (mutually independent) -> R1-A -> R1-B -> R1-C. R1-A is **not** independent of R4-A/R2-A; R1-B needs R3-0.
- Parts (separately freezable): **A** `ExecutionPolicy` and `EndpointResolver` (pure, offline; needs R2-A, R4-A). **B** shared leaf `internal/execrt` (project lock, repository provider, artifact sink), worktree, compiler-admitted session, candidate commit, durable outcomes, cancellation and startup recovery (`Delegate`). **C** `Validate` (clean validation worktree and `validation.ExecuteAndRecord`). Recovery stays in B (not C) because `Delegate` must not run before recovery has completed; `Validate` is separable because it needs none of the model, lock-recovery or compiler machinery.

## Objective

Implement the facade's `TaskExecutor` port so that an approved Work Package becomes a real attempt: a compiler-admitted worker invocation on an eligible, policy-granted endpoint, inside an isolated worktree, producing an immutable candidate commit, with every policy-significant outcome recorded as typed control-plane events. Executor-private state (lock files, the in-memory handle map) is never authority (DCI-159–161): deleting it changes no canonical outcome.

## Context Manifest

Role: bounded Go implementer; independent Contract/Authority and Failure-semantics reviewers. Read envelope: `internal/principal/facade/{ports,operations,service}.go` (signatures only), `internal/controlplane/{batch,service,operations}.go`, `internal/worktrees`, `internal/repository`, `internal/process`, `internal/validation/{record,run,profiles}.go`, `internal/cognition/{routing,portfolio_activation}.go`, `internal/cognition/compiler/compile.go` (`CompileRequest`, `CompileInvocation`), `internal/cognition/drivers/{driver,types,metering,mediation}.go`, `internal/tools`, `internal/events/payloads_task.go`, `internal/tasks`. Write scope: new `internal/taskexec/`, `internal/execrt/` (leaf: `ProjectLock`, `RepositoryProvider`, `ArtifactSink`; imports only `repository`, `protocol`, `errs`, `ids`) and `internal/execpolicy/`, composition in `cmd/devcadence-mcp`, additive docs. No change to facade ports, event payloads, task state machine or invariants; the only shared amendment this EWP consumes is `principal.CodedError` (window overview). **Disclosed cross-EWP amendments:** `EndpointRequest` carries `ExcludeBases []actors.ActorBasis`/`IndependenceBasis` (added here in Part A, for R2-B's independent-reviewer resolution; `ExcludeActorIDs` is **not** used because a hashed actor id cannot be compared to an endpoint/model basis before the revision is known); `ResolvedEndpoint` carries `Provider`, `ModelFamily`, `AccountRef`, `RuntimeVersion` (the actor basis and the observed runtime; the resolver is the sole owner of basis derivation); `OpenedEndpoint` carries an `EndpointObservation` (including the opened driver's trusted `DriverID`) and has no cleanup obligation; `EndpointResolver.Resolve` consumes the caller's already-verified policy snapshot/digest instead of re-reading policy authority; `ExecutionLimits` gains per-call bounds (`MaxOutputTokensPerCall`, `MaxRequestBytes`, `AllowUnknownUsage`). Exported coded errors come from the shared `principal.CodedError` of the [window overview](window-2026-10-g-overview.md#shared-contracts-resolved-in-r3-phase-1) (no facade-private `coded`). R2-B consumes these types; it does not amend this Part's frozen types.

Exact clauses: AGENTS §§2–9, 12–15, 17; ADR-0024 §§3–5; SECURITY §§3, 5–8, 14–17; DCI-025, 030–033, 040–041, 080–084, 120–124, 129, 133, 159–161; WP-M5-1 §Guards; WP-M5-2 §Exact ports and §Launch. Risks: unauthorized source exposure or spend, crash between intent and effect, executor-private authority, worktree escape, candidate outside scope, double execution. Re-resolution triggers: a facade port or `controlplane` signature change, a missing production driver, a new credential path, a task-state or event change, or an endpoint grant the owner has not made.

## Scope envelope

Authorized: `internal/execpolicy` (policy types, strict decoding, receipt-verified loader); `internal/taskexec` (resolver, executor, worktree/session/candidate/validation/recovery, mediated worker tools); composition of `facade.Options.Tasks` in `cmd/devcadence-mcp`; synchronization of MCP_API (delegate/validate availability), PRINCIPAL_HOSTS, PROJECT_STATE and the schema README.

Forbidden: shell execution for the worker; the worker running tests (validation is DevCadence's controlled runner only, Part B); network or credential access other than through a `DriverFactory` the policy names; merging, advancing the accepted commit or pushing (DCI-031); a hidden endpoint fallback or metered fallback (DCI-122); writing any event not listed here; changing `facade` behavior for acceptance; any model-supplied path, tool, endpoint, limit or policy field affecting authority.

LOCAL_DISCRETION: private helper names, package-local structs, test helpers, the layout of the executor's lock/scratch directory, `git` flag ordering that preserves the exact semantics below.

## Requirements and invariants

| ID | MUST requirement | Invariant |
| --- | --- | --- |
| R1 | Before any effect re-check, in this order: single-owner lock (held for the process), task/Work Package tuple and prefix freshness, **current `ExecutionPolicy`**, attempt budget **derived from that policy**, endpoint eligibility, metadata observation, compiler admission. Any failure returns an error with zero events, zero worktrees and **no prompt, source, tool content, model generation, session, credential-bearing request or spend-producing effect**; the only external interaction permitted before the batch is the bounded metadata-only endpoint identity probe inside `DriverFactory.Open` | I1: absence of policy, endpoint, budget or context fit denies; it never degrades |
| R2 | Record `TaskDelegated` and `AttemptStarted` in one `ApplyBatch` with the WP-M5-1 start-execution guard, before the worktree, any session and any model invocation | I2: intent is durable before any prompt, source exposure, generation, session or spend; delegation without an attempt cannot commit |
| R3 | Resolve the endpoint only from the active portfolio filtered by the policy allow-list, with exact locality, source exposure, access channel, health and auth; never fall back at runtime | I3: placement and economics come from granted policy (DCI-120–124), not from the model or availability luck |
| R4 | The worker runs in a fresh isolated worktree at the Work Package base with compiler-admitted context and a mediated tool set whose writes are confined to the declared write scope | I4: scope cannot silently expand (DCI-025, DCI-030) |
| R5 | A candidate is committed by the executor only when every changed path is inside the write scope; the commit's sole parent is the exact base; it records only typed `CandidateProduced` | I5: every candidate has lineage and bounded scope (DCI-032) |
| R6 | Every attempt ends in exactly one durable outcome: `CandidateProduced` or `AttemptFailed` (closed `Summary` grammar below). v1 never emits `AttemptBlocked`: the worker has no tool to report a contradicted assumption, so no producing algorithm exists; a future EWP that adds one owns the trigger and `BlockedReason`. Recovery never invents success | I6: no silent, partial or fabricated outcome |
| R7 | Cancellation and crash are bounded: cancellation records `AttemptFailed{Cancelled:true}` under a fresh bounded context; a restarted owner closes orphaned running attempts as `reason=executor_lost` and never replays model calls | I7: uncertain external effects are recorded, not retried |
| R8 (Part C) | `Validate` runs the project's deterministic profile in a separate clean worktree at the candidate commit through the existing `validation.ExecuteAndRecord` | I8: deterministic validation is authoritative for deterministic claims (DCI-041); the worker never validates itself |
| R9 | Per-attempt limits (turns, tool calls, wall time, total tokens) **and per-call bounds** (`MaxOutputTokensPerCall`, `MaxRequestBytes`) are required and nonzero; spend is integer micro-USD and zero means no metered authority; zero is never unlimited. The meter **observes** usage after each call; pre-call safety comes only from the independently enforceable per-call bounds (output cap sent to the runtime, request-size and context ceilings, wall time per call). Unknown usage never proves a token or spend budget: it may continue only for a local-locality grant with `AllowUnknownUsage` explicitly true while turns, wall time and the per-call bounds still cap the attempt; for a metered or remote channel unknown usage ends the attempt (`limit_reached`) and such endpoints are ineligible until a client with known billable usage exists | I9: budgets are claimed only from known measurements; pre-call safety is a hard per-call bound, not the meter |
| R11 | Policy bytes are pinned at launch; the policy's activation receipt, expiry and revocation are re-verified at every `Delegate` (and, for R2-B, every `Review` start; for R2-C every `Decide`); one rule for the whole window: a revoked/expired grant stops the *next* start, while an already-started attempt ends under its own caps and wall time | I11: standing authority is revocable without relaunch, yet the loaded content is immutable |
| R10 | The worker's invocation is bound by a canonical provenance record whose actor identity the executor derives from the **bound** endpoint under the fixed record-time basis `endpoint_model`, storing the full `ActorBasis` so any later gate re-derives under the policy basis | I10: independence evidence is never model-named (see R2-A) |

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
| `TaskDelegated` is `ready -> running` only; `AttemptStarted` requires task `running`, no running attempt and the approved WP id/version; `applyAttemptFailed` finishes the attempt (`failed`, or `cancelled` when `Cancelled:true`) and **does not change the task state** (stays `running`); `running -> running` is not a transition | `internal/state/reduce.go` `applyTaskDelegated/applyAttemptStarted/applyAttemptFailed`; `internal/tasks/state.go` | a retry after a failed attempt is a batch of `AttemptStarted` alone (no `TaskDelegated`); task states `ready` and `running`-without-a-running-attempt are both delegable |
| `TaskDelegated.MaxAttempts` is recorded but not projected into task state | `events/payloads_task.go`; no field in `tasks.Task` | the attempt budget is `ExecutionPolicy.MaxAttemptsPerTask`, counted from projected attempts, and also recorded in `TaskDelegated.MaxAttempts` on the first delegation |
| `AttemptBlocked{TaskID, AttemptID, Reason tasks.BlockedReason}` exists but nothing in M5 produces a contradiction report | `events/payloads_task.go` | removed from R1 outcomes (R6) |
| `OperationFunc func(ctx) (resultHandle string, err error)`; `finish`: `err == nil` -> `completed`; operation context cancelled (or registry closed) -> `cancelled` with fixed code `CANCELLED` and no refs; any other error -> `failed` with `semanticFor(err)`; `lost` exists only for an unknown/stale `OperationRef` (another instance or evicted), never from a function return | `facade/operations.go` `finish`/`Wait`, `principal/contract.go` | restated map below: R1 never expects `lost` from a run; no registry amendment is needed |
| `facade.coded`/`codedError` are unexported; `semanticFor` maps non-coded errors by `errs` category only; `principal` imports only `errs` | `facade/errors.go`, `principal/contract.go` | refs such as `execution-policy-unavailable` need the shared `principal.CodedError` (window overview, Shared contracts SC-2); `facade.coded` becomes a thin wrapper and `semanticFor` honours `errors.As(*principal.CodedError)` |
| `drivers.SessionDriver` has no `Close`; only `drivers.Session` does; `drivers.TokenUsage` is three `int64` counts (a missing usage block equals measured zero) | `cognition/drivers/driver.go`, `types.go` | `OpenedEndpoint` allocates nothing that needs cleanup (below); usage knownness comes from the R3 Part 0 amendment (`TokenMeasurement`) |
| `validation.ExecuteInput{ProjectID, Subject protocol.ValidationSubject, Commit, Profile validation.Profile, Run RunOptions, Actor, IDs ids.Source}`; `ExecuteAndRecord` blocks until the run completes | `internal/validation/record.go` | `Validate` is sync-checked then asynchronous through `Registry.Start` |
| `portfolio` endpoints (`protocol.CognitionEndpoint`) carry `Provider`, `ModelFamily`, `AccountRef`, `ModelID`, `Locality`, `Health`, `Auth`, `RequiredSourceExposure`; none carries a model revision or runtime version; `protocol.AccessChannel` carries `ChannelID` | `internal/protocol/{cognition,access_channel}.go` | `ResolvedEndpoint` copies the basis fields and the channel (so `Channel.ChannelID` is its source); revision and runtime version are observed by `DriverFactory.Open` (see Binding) |

Step 0 re-verifies every row; a false row escalates.

## Part A — ExecutionPolicy and resolver (pure)

Part A types (`ExecutionPolicy`, `EndpointGrant`, `ExecutionLimits`, `EndpointRequest`, `ResolvedEndpoint`, `EndpointObservation`, `OpenedEndpoint`, `EndpointResolver`, `DriverFactory`, `PolicySource`) live in `internal/execpolicy`, below the executor, so provider composition ([R3-A](wp-m5-r3-empirical-verifier-composition-ewp.md)) and the review executor depend on them and not on `internal/taskexec`. `internal/execpolicy` imports `internal/actors` (R2-A), `internal/operator/receipts` (R4-A) and `internal/principal` (for `CodedError`); all are frozen first. `ActorBasis` is defined in `internal/protocol` (it is a field of additive records and `actors` imports `protocol`, never the reverse); `actors.ActorBasis` is a type alias of it, so both spellings are the same type.

~~~go
type ExecutionLimits struct {
    MaxTurns, MaxToolCalls int
    MaxTotalTokens int64
    MaxDurationSeconds int
    MaxOutputTokensPerCall int64   // required nonzero: sent to the runtime as the per-call output cap (Ollama num_predict)
    MaxRequestBytes int            // required nonzero: ceiling on one request body (prompt + tools + context window the client may send)
    AllowUnknownUsage bool         // default false; true is valid ONLY for Locality local grants (policy load refuses it otherwise)
    MaxAPISpendMicroUSD int64      // integer micro-dollars (digests must not depend on float formatting); 0 = no metered authority
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
    MaxAttemptsPerTask int      // 1..5, required; recorded in TaskDelegated.MaxAttempts
    Grants []EndpointGrant        // unique (EndpointID, ModelID); sorted
}
type EndpointRequest struct {
    ProjectID, TaskID, Role string
    ContextNeed protocol.SourceExposure
    PortfolioDigest string
    IndependenceBasis string            // "endpoint_model" | "model_family_account"; required iff ExcludeBases is non-empty, else must be ""
    ExcludeBases []actors.ActorBasis    // R2-B: the worker's STORED basis (full record); empty for implementer
}
// ResolvedEndpoint is "unbound" when returned by Resolve (ModelRevision == "", RuntimeVersion == "", DriverID == "" and BindingDigest == "") and "bound" after Bind.
type ResolvedEndpoint struct {
    EndpointID, ModelID, ModelRevision, RuntimeVersion, DriverID string
    Provider, ModelFamily, AccountRef string   // copied from the portfolio endpoint; "" = unknown; the resolver is the ONLY place basis fields are derived
    Kind protocol.EndpointKind
    Locality protocol.Locality
    Channel protocol.AccessChannel
    ContextProfile protocol.ContextProfile
    Limits ExecutionLimits
    PolicyDigest, PortfolioDigest, BindingDigest string // BindingDigest: sha256 of protocol canonical JSON of this struct with BindingDigest == "" (no secrets are in it)
}
// EndpointObservation is trusted metadata produced by DriverFactory.Open. ModelRevision and RuntimeVersion are runtime observations; DriverID is copied from the actual opened SessionDriver.ID(). Empty = unavailable.
type EndpointObservation struct { ModelRevision, RuntimeVersion, DriverID string }
// OpenedEndpoint has NO cleanup obligation: Open is metadata-only (below), so the Driver is a lazily-connecting handle that owns no goroutine, connection, session, file or credential needing release.
// Sessions are created later by Driver.StartSession and released by drivers.Session.Close. An implementation that cannot meet this contract is non-compliant and needs an amendment adding an explicit io.Closer; no caller closes an OpenedEndpoint.
type OpenedEndpoint struct {
    Driver   drivers.SessionDriver
    Observed EndpointObservation
}
type EndpointResolver interface { Resolve(context.Context, EndpointRequest, ExecutionPolicy, string /*canonical policy digest*/) (ResolvedEndpoint, error) }
// Open MAY perform only bounded metadata requests (at most two, e.g. GET /api/version and GET /api/tags for the loopback composition) to the granted endpoint.
// It MUST NOT send prompt, source, tool content or credentials, MUST NOT start a session or generation, MUST NOT spend, MUST be cancellable, and MUST allocate nothing that needs Close.
type DriverFactory interface { Open(context.Context, ResolvedEndpoint) (OpenedEndpoint, error) }
// Bind is pure: it requires obs.ModelRevision != "" (else MODEL_UNAVAILABLE, ref model-revision-unknown), obs.RuntimeVersion != "" (else ref runtime-version-unknown), and obs.DriverID != "" (else ref driver-id-unknown); it sets all three and computes BindingDigest.
func Bind(ep ResolvedEndpoint, obs EndpointObservation) (ResolvedEndpoint, error)
type PolicySource interface { Current(ctx context.Context) (ExecutionPolicy, string /*canonical policy digest*/, error) }
~~~

All refusals in this Part are `principal.NewCodedError(code, retryable, refs, detail)` values with the fixed refs named in this EWP.

**Binding order (closes the digest/revision/driver circularity).** `Resolve` returns an unbound endpoint (`BindingDigest`/`DriverID` empty); `Open` observes revision/runtime version and binds `Observed.DriverID = opened.Driver.ID()` from the actual driver handle; `Bind` fills `ModelRevision`, `RuntimeVersion`, `DriverID` and then computes `BindingDigest`. Only a bound endpoint is used for `AttemptStarted.ModelIdentity`, `ActorBasis`, provenance and the compiler request. `Open` is metadata-only, so it is permitted before the intent batch (it is not a prompt, source, session, generation or spend effect) and nothing needs cleanup on later failures. The pre-batch invariant is the one in R1/R2 above, not "zero driver calls".

**Policy loading and revocation (one rule; also R2/R3/R4).** `execpolicy.Load(ctx, verifier receipts.Verifier, home)` strict-decodes `DEVCADENCE_HOME/config/execution-policy.json` under the launch-binding protections, computes the canonical policy digest and returns a `PolicySource`. The *bytes* are pinned: the file digest is compared at every `Current`, and a changed file denies until relaunch. The *authority* is not pinned: every `Current` call (one per `Delegate`, per `Review` start, per R3 run) calls `verifier.Verify(Request{ReceiptID: "", ProjectID, Purpose: execution.policy_activate, Subject{ExecutionPolicy, PolicyID, Revision}, SubjectDigest: policyDigest})` (R4 lookup by subject, so no receipt path and no reference lives in the policy file), checks `NotBefore <= now <= NotAfter` and `Verified.IsValid()`. Any failure (missing, invalid, expired, revoked, anchor unprotected, changed file) means `MODEL_UNAVAILABLE` with ref `execution-policy-unavailable` and zero effects. Policy change = new file + new receipt + relaunch; revocation or expiry needs no relaunch and takes effect at the next start. An already-started attempt ends under its own caps and wall time; this is disclosed, not a gap. Policy validation additionally refuses: any required limit field zero; `AllowUnknownUsage` on a non-local grant; `AllowUnknownUsage` together with `MaxAPISpendMicroUSD > 0`.

Resolution algorithm: the caller passes the **already verified** `ExecutionPolicy` and canonical `policyDigest` returned by its one `PolicySource.Current(ctx)` call for this start. `Resolve` validates the policy/digest pair structurally and never re-reads policy/receipt authority; the same snapshot therefore governs attempt budget and endpoint eligibility. (1) Active portfolio via `GetActivePortfolio`; none → deny. (2) Candidate = every portfolio endpoint with a policy grant matching `EndpointID`, `ModelID`, role; fill `Provider/ModelFamily/AccountRef` from the portfolio endpoint and set `PolicyDigest = policyDigest`. (3) Drop endpoints that are unhealthy, not authenticated (opaque status only; provider text is not authority), whose observed `Locality`/`ChannelKind` differ from the grant, whose `RequiredSourceExposure` exceeds `min(grant.SourceExposure, request.ContextNeed ceiling)` (`ExposureRank`), or whose economics lack the grant's caps: `ChannelKind` metered is **not eligible in this window** (no client reports known billable usage; reason `driver-not-implemented` until an owner-input amendment adds one) and any metered grant additionally needs `MaxAPISpendMicroUSD > 0` and known price; subscription needs `MaxDurationSeconds` and call caps, unknown quota only with `AllowUnknownSubscriptionQuota`, and unknown usage is never allowed; local needs wall-time, token, per-call output and request-size caps. (4) **Exclusion** (only when `ExcludeBases` is non-empty; `IndependenceBasis` must then be set and every excluded basis must have all fields selected by that basis non-empty, else `INVALID_ARGUMENT`). The pre-bind comparison is a direct **tuple comparison**, never a hash comparison: under `endpoint_model` drop every candidate whose `(EndpointID, ModelID)` equals the `(EndpointID, ModelID)` of any excluded basis (conservative: the revision is unknown before `Open`, so this may drop an endpoint whose revision differs; it never admits a same-actor one); under `model_family_account` drop every candidate with an empty `Provider`, `ModelFamily` or `AccountRef` (unknown never independent) and every candidate whose `(Provider, ModelFamily, AccountRef)` equals that of any excluded basis. The caller re-derives actor ids with `actors.DeriveActorID` from the **bound** endpoint after `Bind` and denies on equality (`independent-actor-collision`). (5) Rank survivors with `cognition.Route` using the portfolio policy. (6) No survivor → `POLICY_DENIED`/no-eligible-endpoint with the dropped reasons as fixed codes. Same inputs give the same output; there is no runtime fallback to a second endpoint.

## Part B — Executor lifecycle (lock, delegate, run, failure, recovery)

The shared infrastructure types below live in the leaf package `internal/execrt` (imports only `repository`, `protocol`, `errs`), **not** in `taskexec`, so `reviewexec` (R2-B) and `verifier` (R3-B) import a leaf and never R1's implementation package; `taskexec` re-exports nothing. R1-B freezes `execrt` with its first use.

~~~go
// package execrt
type RepositoryProvider interface { Repository(ctx context.Context, projectID string) (*repository.Repository, error) } // unknown project -> NOT_FOUND
type ArtifactSink interface {                      // content-addressed, immutable; same digest = same ref
    Put(ctx context.Context, projectID, kind, mediaType string, content []byte) (protocol.ArtifactRef, error) // <= 4 MiB; larger -> INVALID_ARGUMENT; ref.Digest = sha256 of content
}
type ProjectLock struct{ /* flock handle; liveness only */ }
// AcquireProjectLock is called ONCE per process by composition (cmd/devcadence-mcp) and the result is passed to every executor
// (taskexec here, reviewexec in R2-B; the verifier does not need it). Executors never acquire it themselves, so there is exactly one lock owner.
func AcquireProjectLock(stateDir, projectID string) (*ProjectLock, error)   // non-POSIX or contended -> error
func (l *ProjectLock) Close() error

// package taskexec
type Options struct {
    ProjectID string                        // one project per executor process; every method refuses another project id
    Lock *execrt.ProjectLock
    ControlPlane *controlplane.Service
    Worktrees *worktrees.Manager
    Repositories execrt.RepositoryProvider
    Runner *process.Runner
    Registry *facade.OperationRegistry
    Policy execpolicy.PolicySource
    Resolver execpolicy.EndpointResolver
    Drivers execpolicy.DriverFactory       // nil => MODEL_UNAVAILABLE
    Compiler *compiler.Compiler
    Artifacts execrt.ArtifactSink           // diff and usage artifacts
    StateDir string                         // scratch only; never authority
    Clock clock.Clock; IDs ids.Source; Logger *slog.Logger
}
func New(Options) (*Executor, error)        // implements facade.TaskExecutor; runs Recover before returning
~~~

Constructor refusals: nil Lock/ControlPlane/Worktrees/Runner/Registry/Policy/Resolver/Compiler/Repositories/Artifacts, empty ProjectID/StateDir. Actor for events: `protocol.Actor{Kind: protocol.ActorControlPlane, ID: "taskexec"}`.

### Algorithm: single owner and recovery

Holding `Options.Lock` proves no other executor process owns running attempts; the lock is liveness, never authority (the OS releases it on crash). A process that cannot acquire the lock does not construct an executor, so `Facade.Options.Tasks` stays nil and every `Delegate`/`Validate` denies `MODEL_UNAVAILABLE` (the composition logs `executor-owned-elsewhere`); no recovery runs there. `New` runs `Recover` exactly once before returning: for each task of `ProjectID` in `running` that has an attempt in `running`, read the current prefix and `ApplyBatch` `AttemptFailed{Summary:"reason=executor_lost effects=uncertain", Cancelled:false}` guarded on that attempt still running; never call a model, never recreate the worktree, never retry. A recovery batch that fails leaves the attempt `running` and `New` returns an error (the executor is not installed; the next start retries recovery). A worktree left behind stays registered (`Cleanup` refuses dirty trees); only clean orphan worktrees whose attempt is terminal are cleaned. Non-POSIX platforms: `AcquireProjectLock` refuses.

### AttemptFailed summary grammar (closed; tests parse it with this exact expression)

`^reason=(worktree_unavailable|endpoint_unavailable|limit_reached|scope_violation|candidate_too_large|no_change|driver_error|cancelled|executor_lost|operation_registry_full)( class=(scope|symlink|submodule|git_dir|count|size))? effects=(none|uncertain)$`. `class` is present only for `scope_violation` (`scope|symlink|submodule|git_dir`) and `candidate_too_large` (`count|size`). `effects=uncertain` for `executor_lost`, `cancelled` and `driver_error` (a driver call may have been in flight or its usage unknown); `effects=none` for the rest. `Cancelled:true` iff `reason=cancelled`. Recovery's summary is therefore exactly `reason=executor_lost effects=uncertain`. Provider or driver error text, paths and file names never enter the summary (DCI-083).

### Task state after a terminal attempt (reducer fact)

`AttemptFailed`/cancel finishes the attempt and leaves the task `running` with no running attempt. That state is delegable (retry = `AttemptStarted` alone). The task leaves it only through a new attempt that yields a candidate (`running -> validating`), or an escalation outside this EWP. When `attempts >= MaxAttemptsPerTask` further `Delegate` denies `POLICY_DENIED` with ref `attempt-budget-exhausted` and no events; this EWP does not block or escalate the task itself.

### Algorithm: Delegate

**Order is normative: nothing policy-derived is used before the policy is loaded, and nothing before the batch may produce a prompt, source exposure, session, generation or spend.**

1. (Sync) Reject any `ProjectID` other than `Options.ProjectID`. Re-read the task and the stored Work Package record (digest-verified); require the tuple equal to `AuthorizedTask.WorkPackage`, the task state either `ready` (**first delegation**) or `running` with no running attempt (**retry**), and the current state prefix `P` equal to `meta` (the facade already checked; a mismatch here is the WP-M5-1 stale conflict, zero effects). Read the Work Package `BaseCommit` and the declared exposure from that stored record.
2. `policy, policyDigest, err := Policy.Current(ctx)` (R11 re-verification; failure → `MODEL_UNAVAILABLE` ref `execution-policy-unavailable`).
3. Attempt budget **from that policy**: projected attempts of the task must be `< policy.MaxAttemptsPerTask`, else `POLICY_DENIED` ref `attempt-budget-exhausted`, zero events.
4. `ep := Resolver.Resolve(ctx, EndpointRequest{...role implementer, empty ExcludeBases...}, policy, policyDigest)`; the resolver MUST NOT call `Policy.Current` again. `ContextNeed` comes from the Work Package's declared exposure (absent → `focused_snippets` ceiling intersected with grant, never wider than the grant). Then `opened := Drivers.Open(ctx, ep)` (nil factory/error → `MODEL_UNAVAILABLE` `endpoint-unavailable`, zero events; **metadata only, no cleanup**), `ep = execpolicy.Bind(ep, opened.Observed)` (unknown revision or runtime version → deny with the `*-unknown` ref).
5. `compiled := Compiler.CompileInvocation(CompileRequest{Role:"implementer", ExecutionContract: <exact stored contract text>, BaseCommit, WriteScope, ReadEnvelope, ContextProfile:&ep.ContextProfile, AccessChannel:&ep.Channel, Tools:<worker tools>, ProjectStateRevision: P ...})`. A compile error returns before any event. The compile result is bound to prefix `P`.
6. `attemptID := IDs.New("att")`; `worktreeID := attemptID`; build the worker `InvocationProvenance` (R2-A): `Role` implementer, `Basis` = the stored `ActorBasis{EndpointID, ModelID, ModelRevision, Provider, ModelFamily, AccountRef}` from the **bound** `ep`, `IndependenceBasis` = the fixed record-time value `"endpoint_model"`, `Actor.ActorID = actors.DeriveActorID("endpoint_model", Basis)` (a derivation error — an empty `EndpointID/ModelID/ModelRevision` — cannot occur after `Bind` and denies if it does), `EndpointBindingDigest = ep.BindingDigest`, manifest/prompt digests from `compiled`, `CandidateCommit` empty. No acceptance policy is consulted: the gate (R2-C) and the reviewer resolver (R2-B) re-derive the worker actor under their own basis from the stored `Basis`, which therefore must keep **all** six fields even when some are empty.
7. **Prefix is not refreshed.** Immediately before the batch re-read the prefix; if it differs from `P` the call fails with the WP-M5-1 stale conflict, zero effects (the compiled pack is bound to `P`; the principal re-calls `delegate`). Then `ApplyBatch{ExpectedStateRevision: P, WorkPackage: guard, Commands: first-delegation ? [TaskDelegated{TaskID, WorkPackageID, WorkerRole:"implementer", MaxAttempts: policy.MaxAttemptsPerTask}, AttemptStarted{...}] : [AttemptStarted{...}]}` with the provenance record stored on the `AttemptStarted` member. **`AttemptStarted` field sources:** `TaskID` from the task; `AttemptID = attemptID`; `WorkPackageID`/`WorkPackageVersion` from the re-read approved tuple (equal to `AuthorizedTask.WorkPackage`); `ProjectStateRevision = P` (the meta prefix the batch expects, which is also the compiler's `ProjectStateRevision`); `BaseCommit` = the stored Work Package `BaseCommit`; `WorkerRole` `"implementer"`; `WorkerProfile` omitted; `ModelIdentity = ep.EndpointID+"/"+ep.ModelID+"@"+ep.ModelRevision`; `WorktreeID = attemptID`. Stale prefix/tuple → the conflict error, zero effects.
8. `Registry.Start(project,"delegate",deadline=ep.Limits.MaxDurationSeconds+60s, run)` and return its `OperationRef`. If `Start` fails (registry full) record `AttemptFailed{Summary:"reason=operation_registry_full effects=none"}` immediately under a fresh context (nothing needs closing: no session exists yet).

### Algorithm: run (asynchronous, owns the attempt)

1. `Worktrees.Create(ctx, repo, Spec{Project,Task,AttemptID,Base})` (`repo` from `Repositories`); error → `reason=worktree_unavailable`.
2. `md := drivers.NewMeteredDriver(opened.Driver, limitsFrom(ep.Limits))` (`MeterLimits` carries the turn, tool-call, wall-time and token limits plus `AllowUnknownUsage` per the R3 Part 0 amendment; the per-call bounds are passed to the driver client through `SessionConfig`); reject if any required limit or per-call bound is zero before this line (`reason=endpoint_unavailable`, no session started, no call made).
3. `s := md.StartSession(SessionConfig{SessionID: attemptID+"-s1", ModelID: ep.ModelID, SystemPrompt: compiled prompt, Tools: workerTools, WorktreeScope: scope, Mediator: ScopedToolMediator})`. Worker tools are exactly `read_file`, `grep`, `symbols` (existing `internal/tools`, worktree-scoped) and `write_file`: relative path inside `WriteScope` (via `compiler.IsPathAuthorized`) after `Scope.ResolvePath` (symlink-safe), ≤256 KiB per file, ≤64 files per attempt, UTF-8 text only, never creating a symlink, never touching `.git`. No shell, no network, no test runner. A `PausedReason`, meter suspension, loop detection, **unknown usage reported where `AllowUnknownUsage` is not true**, or a metered/remote channel reporting any unknown usage ends the attempt `reason=limit_reached` (the meter observes after the call; the per-call bounds are what limited that call).
4. Turn loop: at most `MaxTurns` `ExecuteTurn` calls with the compiled prompt then driver-managed tool results; stop at the first turn with no tool calls and no `PausedReason`. Context cancellation or deadline calls `s.Close` under a fresh 5 s context and ends the attempt (see cancellation); the session is the only closable resource of the attempt (`OpenedEndpoint` owns none).
5. Materialize the candidate (executor-owned, via `Runner`, argv only, env `GIT_CONFIG_GLOBAL`/`GIT_CONFIG_SYSTEM` null, `core.hooksPath` empty, `commit.gpgsign=false`, fixed author/committer `DevCadence executor <executor@devcadence.invalid>`): `git status --porcelain=v2 -z` to list tracked, untracked, renamed, deleted and mode-changed paths; refuse any path outside `WriteScope` (`scope_violation class=scope`), any symlink or submodule change (`class=symlink|submodule`), any `.git` path (`class=git_dir`), more than 64 files (`candidate_too_large class=count`) or a patch above 2 MiB (`class=size`). No change → `reason=no_change`. Otherwise `git add -- <exact paths>`, `git commit` with fixed message `devcadence attempt <attemptID>`, `git rev-parse HEAD`; require one parent equal to the base and re-list `git diff --name-status base..HEAD` against the scope.
6. Store artifacts via `Artifacts.Put` (unified diff, driver usage JSON that records each `TokenMeasurement` with its `Known` flag, no prompt secrets). `ApplyBatch{ExpectedStateRevision: fresh prefix, Preconditions:[attemptStillRunningGuard], Commands:[CandidateProduced{CandidateCommit, Summary:"candidate produced: n files", Artifacts}]}` where the guard requires attempt `running`, worktree id equal and task `running`.
7. Result handle: `candidate:<attemptID>`. The worktree is retained for review/hygiene; the executor never cleans a worktree holding a candidate.

### Algorithm: failure and cancellation

Every `AttemptFailed` is a typed event whose `Summary` matches the grammar above. On cancellation or deadline: close the session within 5 s, kill any child process group started by the executor, append the failure event under a fresh 10 s context (retry once on transient busy), leave the worktree.

**Operation-registry mapping (restated to the registry's actual outputs, `OperationRegistry.finish`).** `run` returns `(resultHandle, error)`; the registry yields `completed` for a nil error, `cancelled` (fixed code `CANCELLED`, no refs) whenever the operation context was cancelled or the registry closed, and `failed` with `semanticFor(err)` for any other error. `lost` is **never** produced by `run`; it appears only to a caller holding an unknown or stale handle. Therefore: after a recorded cancellation `run` returns `("", ctx error)` and the operation is `cancelled`; for any other recorded `AttemptFailed`, `run` returns `("", principal.NewCodedError(code, false, ["attempt-failed"], ...))` with code `MODEL_UNAVAILABLE` for `worktree_unavailable|endpoint_unavailable|driver_error` and `POLICY_DENIED` for `limit_reached|scope_violation|candidate_too_large|no_change` (status `failed`); if the append itself fails (or its outcome is ambiguous and a state lookup cannot confirm it) the attempt stays `running` until the next owner's `Recover` and `run` returns `principal.NewCodedError(principal.CodeInternal, true, ["attempt-outcome-unrecorded"], ...)` (status `failed`, never `completed`). **Disclosed residual:** if the operation context was also cancelled, the registry reports `cancelled` without the ref; the caller must then read project state, where an attempt still `running` means the outcome is unrecorded. The synchronous caller (`delegate`) therefore only ever sees `Delegate`'s own refusals plus the `OperationRef`; every later failure is observed through the operation status and the journal. Meter-observed usage is recorded as measured only when the driver reports it as known (`TokenMeasurement.Known`); otherwise `unknown`.

## Part C — Validate (separately freezable; depends on Part B's `Options`)

`Validate(ctx, caller, meta, candidate, profileID)`: **synchronous phase** (returns errors, no events): reject another project; re-read the attempt (must be `candidate_produced`, commit equals `CandidateRef`); `profile, err := Options.Profiles.Profile(ctx, projectID, profileID)` where

~~~go
type ProfileSource interface { Profile(ctx context.Context, projectID, profileID string) (validation.Profile, error) } // unknown -> NOT_FOUND; the profile is project-owned configuration, never model input
~~~

(`Options.Profiles ProfileSource` is added by this Part; nil denies `MODEL_UNAVAILABLE`); refuse a profile containing a shell executable, a network-looking argument or a service spec the executor cannot start. **Asynchronous phase:** `Registry.Start(project,"validate",deadline=sum(check timeouts)+60s, run)` (the registry, not a new goroutine mechanism, is the only async path; `Wait` yields after 10 s as for delegate). `run` creates a separate clean worktree id `<attemptID>-v<n>` (`n` = recorded `ValidationCompleted` for the attempt + 1, from the projected state) at the candidate commit and calls `validation.ExecuteAndRecord(ctx, ControlPlane, ExecuteInput{ProjectID, Subject: bound to the attempt/candidate, Commit: candidate, Profile, Run, Actor: control-plane taskexec, IDs})`; a dirty tree or HEAD mismatch (as `ExecuteAndRecord` already checks) refuses; the validation worktree is cleaned when clean. The result handle is the `ValidationCompleted` validation id. Cancellation kills the child process group and `run` returns the context error (operation status `cancelled`, fixed code `CANCELLED`, no event; validation is re-runnable); an append failure returns `principal.NewCodedError(principal.CodeInternal, true, ["validation-outcome-unrecorded"], ...)` (status `failed`; `lost` is never produced by a run). Validation never edits the candidate and never uses `Registry` results as authority.

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
| Policy/receipt | `MODEL_UNAVAILABLE`, zero effects | n/a | expired or revoked: deny at the next `Delegate` (R11); changed policy file: deny until relaunch | `INTEGRITY`, deny |
| Portfolio / endpoint health | deny | unknown health/auth: not eligible | re-resolve each Delegate | refuse |
| Cap / price | no authority | unknown price: not eligible | policy relaunch | refuse |
| Task/WP/prefix | facade error | n/a | `STALE_*`, zero effects | `INVALID_ARGUMENT` |
| Usage | recorded `unknown` | unknown is never zero and never proves a budget; local + `AllowUnknownUsage` may continue under the other hard bounds, any other case ends `limit_reached` | n/a | n/a |
| Runtime version / model revision | `Open` reports none: deny (`runtime-version-unknown` / `model-revision-unknown`) | same | n/a | refuse |

| Failure boundary | Required postcondition | Recovery |
| --- | --- | --- |
| Before batch (policy, budget, resolve, open/bind, compile, stale prefix) | zero events, zero worktrees, no session start, no prompt/source/tool content, no generation, no credential-bearing request, no spend; at most the bounded metadata-only probe(s) of `Open` occurred | principal retries |
| Batch fails | zero events | stale error |
| Worktree create fails after batch | `AttemptFailed(reason=worktree_unavailable effects=none)`; no session started | new Delegate |
| Crash after batch, before candidate | next owner: `AttemptFailed(reason=executor_lost effects=uncertain)`; no replay; task stays `running` | new Delegate by principal (`AttemptStarted` only) while budget remains |
| Crash after commit, before `CandidateProduced` | orphan commit in worktree only; attempt closed `executor_lost`; no candidate | new attempt |
| `CandidateProduced` append fails/ambiguous | read state first; if absent the attempt is closed failed by recovery | no duplicate model call |
| Driver call in flight at cancel | usage unknown; `reason=cancelled effects=uncertain`, `Cancelled:true` | operator reconciliation if spend uncertain |
| Failure append fails or is ambiguous | attempt stays `running`; operation `failed` with ref `attempt-outcome-unrecorded` (`cancelled` without ref if the context was cancelled) | state lookup first; next owner's `Recover` closes it |

## Traceability and acceptance scenarios

| ID | Setup → action → expected | Requirement → invariant → representation |
| --- | --- | --- |
| A1 | repo + fake driver editing in scope → delegate → attempt, worktree, one-parent candidate at base, `CandidateProduced`, task `validating` | R1–R6 → I1–I6 → batches, commit algorithm |
| A2 | stale prefix or tuple (race after `Open`/compile) → delegate → zero events and worktrees; spy `SessionDriver` wrapper records **no** `StartSession`/`ResumeSession`/`ExecuteTurn`; `DriverFactory.Open` call count ≤ 1 and the spy `Open` issues no request other than the declared metadata probes (no prompt/source bytes); prefix is not refreshed | R1/R2 → I1/I2 |
| A3 | no policy / expired / bad receipt / no eligible endpoint → zero effects, `MODEL_UNAVAILABLE`/`POLICY_DENIED` | R1/R3 → I1/I3 |
| A4 | remote or metered endpoint without grant/cap → not eligible, no fallback | R3 → I3 |
| A5 | compile failure (`CONTEXT_UNFIT`) after a successful `Open` → zero events, zero worktrees, no session/model invocation (same spies as A2) | R1 → I1 |
| A6 | worker writes outside scope, symlink, `.git`, traversal → no candidate, `scope_violation` | R4/R5 → I4/I5 |
| A7 | no change / oversize → failed with closed reason | R5/R6 → I5/I6 |
| A8 | meter limit or loop → `limit_reached`, no candidate; unknown usage with `AllowUnknownUsage=false` → `limit_reached`; unknown usage on a local grant with `AllowUnknownUsage=true` continues only while turns, wall time and per-call bounds hold (a request over `MaxRequestBytes` or output cap request above `MaxOutputTokensPerCall` is refused before the call); unknown usage never satisfies a token budget assertion | R9 → I9 |
| A9 | cancel mid-turn → session closed in 5 s, `AttemptFailed{Cancelled:true, Summary:"reason=cancelled effects=uncertain"}` from a fresh context; operation status `cancelled` (code `CANCELLED`); append made to fail without cancellation → status `failed`, ref `attempt-outcome-unrecorded`, attempt still `running`, never `completed`; append failing under cancellation → status `cancelled` and the attempt visibly `running` in state; no scenario expects `lost` (registry never produces it from a run) | R7 → I7 |
| A10 | kill after `AttemptStarted` → restart → exactly `reason=executor_lost effects=uncertain`, zero model calls, no replay; task `running`, retry batch is `AttemptStarted` only | R6/R7 → I6/I7 |
| A11 | second process cannot `AcquireProjectLock` → no executor installed, other instance's attempts untouched; one lock handle is shared by taskexec and reviewexec in one process | R7 → I7 |
| A12 (Part C) | validate: sync refusals (unknown profile, wrong commit) leave zero events; async run in a separate clean worktree via `Registry.Start`; dirty tree refused; `ValidationCompleted` digest matches | R8 → I8 |
| A13 | delete lock/scratch state → canonical outcomes unchanged; executor cannot append undeclared events (package boundary test) | DCI-160 |
| A14 | worker provenance record present at `AttemptStarted` with the full stored `ActorBasis`, `IndependenceBasis="endpoint_model"` and an actor id equal to `DeriveActorID("endpoint_model", Basis)` from the **bound** endpoint; model output cannot alter it; re-derivation of the same stored basis under `model_family_account` is possible when those fields are non-empty | R10 → I10 |
| A15 | canary secret in driver error/env → absent from events, artifacts and logs | DCI-081/083 |
| A16 | first delegation (task `ready`) commits `[TaskDelegated, AttemptStarted]`; after a failed attempt the next batch is `[AttemptStarted]` only; at `MaxAttemptsPerTask` → `attempt-budget-exhausted`, zero events; the budget check observably happens after `Policy.Current` (a spy `PolicySource` call precedes the attempt-count read, and a policy with a different `MaxAttemptsPerTask` changes the outcome); `AttemptStarted` fields match the sources in Delegate step 7 | R1/R6 → I1/I6 → reducer facts |
| A17 | policy receipt revoked/expired between two `Delegate` calls (no relaunch) → second denied, zero effects; policy file edited → denied until relaunch; an attempt already running finishes under its caps | R11 → I11 |
| A18 | `Open` observes no model revision, runtime version **or driver id** → deny with `model-revision-unknown` / `runtime-version-unknown` / `driver-id-unknown`, zero events; `Observed.DriverID` must equal `opened.Driver.ID()` and the bound endpoint changes `BindingDigest` when revision/runtime/driver identity changes; nothing is closed because `OpenedEndpoint` owns no resource | Binding → I1/I10 |
| A19 | every `AttemptFailed` produced by any path matches the summary grammar; a provider error string never appears | R6 → I6 |
| A20 | no step of the resolver derives a basis field other than from the portfolio endpoint; `ExcludeBases` (full stored basis) drops a same-`(EndpointID, ModelID)` endpoint under `endpoint_model` (even when the revision differs: conservative) and any endpoint with equal or empty `(Provider, ModelFamily, AccountRef)` under `model_family_account`; empty `IndependenceBasis` with a non-empty list, or an excluded basis with a selected field empty, is `INVALID_ARGUMENT`; `independent-actor-collision` fires after `Bind` | Part A → R2-B I3 |
| A21 | exported coded errors: every refusal of this EWP surfaces through `semanticFor` with the exact code and ref (`execution-policy-unavailable`, `attempt-budget-exhausted`, `attempt-failed`, `attempt-outcome-unrecorded`, `model-revision-unknown`, `runtime-version-unknown`, `endpoint-unavailable`); a non-coded error never leaks text | SC-2 → I1 |
| A22 | policy validation refuses a zero per-call bound, `AllowUnknownUsage` on a non-local grant, or together with spend; metered kind is not eligible (no fallback) | R9 → I9 |

A live run against a real endpoint is a separate operator acceptance step after OWNER INPUT-2; test-driver runs never claim it.

## Validation and Mutation Catalog

`go test -count=1 -race ./internal/taskexec/... ./internal/execpolicy/... ./internal/principal/... ./cmd/devcadence-mcp/... ./tests/...`; `make schemas`; `make docs-check`; `make verify`. Record versions, spy call counts and exit statuses. Tests use real temporary Git repositories and the existing `fake_driver`; no paid or networked test.

Test seams (explicit, no production-reachable hook): spies are wrappers around the `Options` interfaces (`DriverFactory`, `Runner` via a recording executable resolver, `Worktrees` via a spy manager, `Artifacts`, `Clock`, `IDs`); a failing append (A9 lost) is a `controlplane.Service` over a store wrapper that fails the Nth write, constructed only in `_test.go`; a crash (A10) is simulated by abandoning executor #1 without `Close` while its fake driver blocks on a barrier, then constructing executor #2 over the same SQLite file and a new `ProjectLock` after releasing the abandoned handle; the package-boundary test (A13) is a `tests/` test using `go list -deps -json` asserting `internal/taskexec` imports no event-appending path other than `controlplane` and declares only the events listed in this EWP (a static scan of composite literals of `events.*` types).

| Mutant | Expected failure |
| --- | --- |
| Worktree, session start, generation, or any prompt/source-bearing request before the batch commits; or `Open` retained as a closable resource | A2/A18 |
| Attempt budget read before `Policy.Current`, or prefix silently refreshed after compile | A16/A2 |
| Unknown usage treated as zero or as proof of budget; `AllowUnknownUsage` accepted for remote/metered | A8/A22 |
| Registry `lost` expected from a run, or coded error text built outside `principal.CodedError` | A9/A21 |
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
| Retry batch repeats `TaskDelegated`, or budget unchecked | A16 |
| Receipt/expiry checked once at launch only | A17 |
| Revision or runtime version defaulted to a literal instead of denying | A18 |
| Free-form failure summary or provider text in summary | A19 |
| Exclusion skipped, applied only before `Bind`, or compared by actor-id hash | A20 |
| Actor id read from model output | A14 |
| Provider error copied into summary/log | A15 |

Independent lenses: Contract/Authority; Failure-semantics and Test Adequacy/Mutation. The author does not review.

## Rationale, escalation and readiness

Selected: commit intent first (`TaskDelegated`+`AttemptStarted`), then effects, with a single-owner lock deciding orphan status, versus creating the worktree and calling the model before committing (a stale prefix would then leave an unrecorded paid call) and versus a durable executor job queue (new authority-bearing state, rejected by DCI-160). Cost: a crash leaves a recorded failed attempt rather than a clean slate, which is the honest outcome.

Escalate on: a facade/controlplane signature drift; `ValidationCompleted` not reducing for the task state; driver contract lacking a way to close an in-flight session; no safe way to confine `write_file`; owner wanting concurrent executors or Windows.

Implementation Readiness Report:

~~~text
author tally after repair round 2 (r3) (a self-count, not evidence; independent re-verification PENDING):
requirements represented: 11 (R1-R11; R8 in Part C)
acceptance scenarios mapped: 22 (A1-A22); mutation rows 24
unresolved architecture choices: 1 (OWNER INPUT-2 for live use only); 0 known implementation-critical holes after r2, unverified
readiness: NOT_READY pending independent re-verification, R2-A and R4-A freezes, step-0 fact checks and the current-base gate
~~~

Weaker-implementer check: author expectation only; the independent Implementability reviewer must re-test it per Part. Live behavior is blocked on owner policy, not on invention.

## Follow-up card (not authored; not required to freeze R1)

**R1-D bounded edit tool.** Whole-file `write_file` is the only safe edit in this contract but is token-expensive for weaker/local workers, which cuts against the project's token-minimization goal. Recommended before practical self-hosting experiments: an `apply_patch` or exact-range `replace_range` worker tool with the identical checks (`compiler.IsPathAuthorized`, symlink-safe `Scope.ResolvePath`, size/file caps, never `.git`, no symlink creation). It is a new worker tool (a contract amendment of Part B's tool list and A6), so it is a separate card with its own freeze.

## Changelog

- r4: focused repair after review of `0da5013`: `EndpointObservation` now carries the actual opened `Driver.ID()` and `Bind` requires/binds it before computing `BindingDigest`; `EndpointResolver.Resolve` consumes the caller's single already-verified policy snapshot/digest so attempt budgeting and routing cannot observe different policy evaluations.
- r3: owner review of head 106dafd: `ExcludeBases []actors.ActorBasis` replaces `ExcludeActorIDs` with a tuple comparison rule (item 1); exported `principal.CodedError` carrier instead of facade-private `coded` (2); registry mapping restated to actual outputs, no `lost` from a run (3); `OpenedEndpoint` is metadata-only with no cleanup, `EndpointObservation{ModelRevision, RuntimeVersion}`, `Channel.ChannelID` source (4/8); worker provenance at `AttemptStarted` with fixed `endpoint_model` record basis and full stored `Basis` (5); R1 invariant restated from "zero driver calls" to "no prompt/source/tool/generation/session/credential/spend before the batch; one bounded metadata probe" (6); Delegate order policy -> budget -> resolve/open/bind/compile -> batch, prefix not refreshed, `AttemptStarted` field sources, "first delegation = task `ready`" (7); per-call bounds and unknown-usage rules (11); leaf `internal/execrt` for `ProjectLock`/`RepositoryProvider`/`ArtifactSink`; R1-D follow-up card.
- r1: initial draft for window 2026-10-G.
- r2: repair round 1: split into Parts A/B/C; collaborators `RepositoryProvider`/`ArtifactSink`/`ProfileSource` and `ProjectLock` given exact signatures; single lock owner; `Options.ProjectID`; closed `AttemptFailed` summary grammar and reducer-grounded task-state/retry batch; `AttemptBlocked` removed from v1; semantic-code map and cancellation shape; policy bytes pinned but authority re-verified per `Delegate` (R11); `ResolvedEndpoint` basis fields, `Open`/`Bind` ordering, `ExcludeActorIDs` amendment disclosed; integer micro-USD; attempt budget from policy; dependency/freeze-order corrected; test seams; honest readiness tally.
