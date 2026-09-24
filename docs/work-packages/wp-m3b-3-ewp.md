# Engineering Work Package: WP-M3B-3 — Executor and approval semantics (service layer, no public CLI)

- **Milestone:** M3B — Guided bootstrap and onboarding
- **Scope card:** [docs/WORK_PACKAGES.md#wp-m3b-3--executor-and-approval-semantics-service-layer-no-public-cli](../WORK_PACKAGES.md#wp-m3b-3--executor-and-approval-semantics-service-layer-no-public-cli)
- **Base commit:** `5bac450` (`feat/m3b-guided-bootstrap`, includes accepted WP-M3B-1 and WP-M3B-2 checkpoints)
- **Branch:** `feat/m3b-guided-bootstrap`
- **Depends on:** WP-M3B-1 (types), WP-M3B-2 (`Ledger`, `PostconditionChecker`, `ReconcileInterrupted`) — both accepted.
- **Status:** Implemented, pending independent review. See §12 for one interface refinement made during implementation (`ApplyOperation` also returns `*process.Result`) and §13 for deterministic evidence.

## 0. What already exists (pre-check, same pattern as WP-M3B-1/2)

`internal/setup/planner.go` (Phase 2, pre-existing on `main`) generates `SetupPlan`s from a `DoctorReport` — it is **not** an executor. It never runs anything, never checks a digest, never re-checks a precondition, and has no notion of approval. It is exactly the "planner/recipes component wearing a name that sounds like WP-M3B-3's executor" the WP-M3B-2 handoff flagged as a possibility, confirmed by full reading: its only output is an unexecuted `*protocol.SetupPlan`. It belongs to WP-M3B-6 territory (recipe generation), not this WP, and is **not modified** here — this WP's executor consumes a `*protocol.SetupPlan` (from `planner.go` or anywhere else) as an opaque input.

No approval/precondition-rechecking/execution logic, no `CommandRunner`, no operation-dispatch, no live `Condition` evaluator, and no managed-config store exist anywhere in the repository (confirmed by `grep -rln "CommandRunner\|EvaluateCondition\|ManagedConfigKey" internal/ --include=*.go`, which only matches the WP-M3B-1 type declaration). This WP is a genuine, from-scratch build.

## 1. Objective and rationale

Wire WP-M3B-1's plan types and WP-M3B-2's ledger into an actual executor implementing ADR-0014 §2's two-step approval workflow, as service-level Go APIs (`internal/setup`) — no CLI. The rationale is the same as ADR-0014's Context: an un-hashed or silently-adaptable plan lets what executes drift from what was approved; this WP is what makes the digest-approval and precondition-recheck guarantees real rather than aspirational type fields.

## 2. Architectural intent

- **Package:** `internal/setup`, new files `commandrunner.go`, `conditions.go`, `operations.go`, `executor.go`, plus `_test.go` files. Same package as the rest of the setup domain — the executor is not split into a separate package, since it is tightly coupled to `Ledger`/`PostconditionChecker` from WP-M3B-2 and the plan types from WP-M3B-1.
- **`CommandRunner` (`commandrunner.go`):** the narrow interface ADR-0014 §7 requires (`Run(ctx, process.Spec) (process.Result, error)`). `*process.Runner` already satisfies it exactly — no wrapper type is needed, only the interface declaration that makes the dependency swappable in tests. This is the *only* path through which this package invokes an external process; every operation applier that needs to run something goes through an injected `CommandRunner`, never `os/exec` directly.
- **Condition evaluation (`conditions.go`):** `EvaluateCondition(ctx, deps EvaluatorDeps, cond protocol.Condition) (bool, string, error)` is the live-evaluation counterpart to the WP-M3B-1 `Condition` type — the executor's precondition rechecking and (via WP-M3B-2's `PostconditionChecker` interface, which this function satisfies) interruption reconciliation both go through it. Four of six condition kinds are evaluated natively, self-contained within this package:
  - `command_available` → `exec.LookPath` (matches `doctor.go`'s existing pattern for the same check; `LookPath` searches PATH via `stat`, it does not execute anything, so it is not subject to the "subprocess execution exclusively through `CommandRunner`" MUST).
  - `executable_verified` → `os.Stat` the exact `canonical_path` (never PATH-searched — this is what prevents PATH substitution, per ADR-0014 §1), hash its contents (sha256) and compare to `ExpectedDigest` when set, and when `ExpectedVersion` is set, run `<canonical_path> --version` through `CommandRunner` (bounded timeout, output not persisted as an artifact — this is a live probe, not a plan action) and require the output to contain `ExpectedVersion` as a substring.
  - `managed_dir_exists` → resolve `Location` via `LocationPath` (§2 below) and `os.Stat` it, checking both existence and exact mode.
  - `port_listening` → `net.DialTimeout("tcp", host:port, boundedTimeout)`; succeeds only if the dial succeeds (loopback-only is already enforced by `Condition.Validate()`).
  - `model_digest_present` → implemented natively **only for `runtime == "ollama"`**: an unauthenticated loopback HTTP `GET 127.0.0.1:11434/api/tags` (matching the port `planner.go` already hardcodes for its own Ollama preconditions), parsed for a tag/digest match. Any other `runtime` value returns a clear "unsupported runtime" error (fails closed, not silently true) rather than guessing at a protocol this WP has no adapter for.
  - `endpoint_healthy` → **not implemented natively.** It requires the M3A cognition endpoint registry/probing machinery (`internal/cognition`), which this package does not and should not duplicate or reach into directly (that coupling belongs to whichever service already owns endpoint discovery). `EvaluateCondition` accepts an optional `EndpointHealthChecker` in `EvaluatorDeps`; with none supplied, an `endpoint_healthy` condition fails closed with an explicit "no endpoint health checker configured" error — never silently `true`. No current recipe (`planner.go`) emits this condition kind, so this is a documented, currently-inert boundary, not a gap in anything actually exercised.
- **Operation appliers (`operations.go`):** `ApplyOperation(ctx, deps ApplierDeps, op protocol.TypedOperation) (mutated bool, detail string, artifactRef *protocol.ArtifactRef, err error)` dispatches on `op.Kind`, one function per kind, **all implemented for real, nothing stubbed**:
  - `create_directory` → `ensureDirMode` (WP-M3B-2, reused) at the path `LocationPath` resolves.
  - `write_managed_config` → a new minimal managed-config store, `state/config.json` under `$DEVCADENCE_HOME`, atomic write (mirrors `cache.go`'s temp-file+fsync+rename pattern) keyed by `ManagedConfigKey`.
  - `remove_stale_cache` → delegates directly to WP-M3B-1/Phase-2 `CacheManager.Remove` — no new cache-eviction logic.
  - `run_diagnostic_check` → the four fixed `DiagnosticCheckName`s (`git_available`, `ollama_responding`, `mlx_importable`, `state_root_writable`), each a small self-contained probe (`LookPath`, a loopback dial, a bounded `CommandRunner` invocation, an `os.Stat`+write-check respectively).
  - `ollama_pull_model` → `ollama pull <model_tag>` via `CommandRunner`, bounded timeout, output captured through the artifact store below.
- **Output capture (`executor.go`):** reuses the existing `internal/artifacts.Store` — rooted at `$DEVCADENCE_HOME/artifacts/setup` (not a project's own store; ADR-0014 §4 requires this never touch project/Git state) with a fixed namespace (`"setup"`, a valid `validateProjectID` shape) — for content-addressed, size-bounded (`process.DefaultMaxOutputBytes`, 4 MiB, reused rather than redefined) captured stdout, ANSI-stripped before storage. An action whose `Effects` include `EffectAuthentication` never gets an artifact written for it, by construction, regardless of operation kind — a generic rule on the executor's capture path, not a per-operation special case, since no current `OperationKind`'s `IntrinsicPolicy` happens to include `EffectAuthentication` (this rule is exercised with a synthetically-constructed test action, per §8).
- **Executor (`executor.go`):** `Executor.Apply(ctx, plan *protocol.SetupPlan, approvedDigest string, yesScope bool) (*protocol.SetupExecutionReport, error)`:
  1. Verify `plan.Validate()` passes (confirms `plan.PlanDigest` is internally self-consistent) and `approvedDigest == plan.PlanDigest`; on mismatch, reject before anything else — no ledger writes.
  2. If `yesScope`, reject outright if `plan.RequiredAuthority` ranks above `AuthorityUserConfirmation` — privileged/high-impact actions always require the bare digest-approval path (no `--yes`-equivalent), regardless of caller.
  3. Append `ExecutionCreated` then `PlanApproved` to a `Ledger` opened at `state/setup-ledger.jsonl`.
  4. Walk `plan.Actions` in dependency order (topological, respecting `DependsOn` — the same forward-reference-free order `SetupPlan.Validate()` already requires of the input).
  5. For each action: **re-evaluate every precondition live** via `EvaluateCondition`; on any failure, append `ExecutionFinished{status: failed}` and return an error — halting before this or any later action executes, per the acceptance criterion. This is drift detection: a precondition that held at plan-generation time but not at apply time invalidates the approval.
  6. Append `ActionStarting` (fsync'd, per WP-M3B-2, before anything runs).
  7. If the action is manual (`ManualInstructions != nil`): append `ActionTerminated{status: manual_required}` and **stop the whole walk** — no automated path exists past a step a human must perform, and no later action's precondition can be safely assumed to hold.
  8. If executable: dispatch via `ApplyOperation`; on success append `ActionProcessCompleted` (when a subprocess ran) then re-check postconditions via `EvaluateCondition` and append `PostconditionVerified`, then `ActionTerminated{status: succeeded|failed}` accordingly.
  9. After the walk (all actions terminal, or halted by drift), append `ExecutionFinished` and return `ProjectExecutionReport(ledgerEvents, executionID, plan)` (WP-M3B-2, reused verbatim — no second report-construction path).

## 3. Verified assumptions and evidence

- **Assumption:** no executor/`CommandRunner`/condition-evaluator/managed-config code exists yet. Verified: `grep -rln "CommandRunner\|EvaluateCondition\|ManagedConfigKey\|config.json" internal/ --include=*.go` matches only the WP-M3B-1 type declaration in `internal/protocol/setup.go`.
- **Assumption:** `*process.Runner`'s `Run` method signature already matches the `CommandRunner` interface this WP needs, with no adapter required. Verified: `internal/process/process.go:134`, `func (r *Runner) Run(ctx context.Context, spec Spec) (Result, error)`.
- **Assumption:** `internal/artifacts.Store` can be reused rooted at an arbitrary absolute path (not necessarily a project's own artifact root) with an arbitrary namespace string standing in for `ProjectID`. Verified: `NewStore(root, ids)` only requires `root` to be absolute (`internal/artifacts/artifacts.go:42`); `PutInput.ProjectID` is validated by `validateProjectID` as a bounded lowercase/digit/`-`/`_` string (`artifacts.go:369`), which `"setup"` satisfies, and nothing in `Store` assumes its root is project-scoped.
- **Assumption:** `planner.go` already hardcodes Ollama's local port as `11434` for its own `port_listening` precondition, so reusing that port for this WP's `model_digest_present`/`ollama_responding` probes is consistent with existing recipe assumptions, not a new one this WP introduces. Verified: `internal/setup/planner.go:257-262`.

## 4. Constraints

**MUST:**
- No operation executes anything outside `ApplyOperation`'s dispatch; no ad hoc shell-out or filesystem mutation for a `TypedOperation` kind lives anywhere else in `internal/setup`.
- All subprocess execution goes through the injected `CommandRunner`, never `os/exec` directly (the one narrow exception is `exec.LookPath`, which performs no execution, matching `doctor.go`'s existing precedent).
- `Executor.Apply` never executes anything without first confirming `approvedDigest == plan.PlanDigest`.
- `yesScope` rejects outright (no partial execution) any plan whose `RequiredAuthority` exceeds `AuthorityUserConfirmation`.
- Preconditions are re-evaluated live immediately before each action executes; any failure halts the entire remaining walk, not just that action.
- No manual action is ever "executed" by this package; encountering one halts the automated walk.
- Output capture never persists an artifact for an action whose `Effects` include `EffectAuthentication`.
- Output capture is bounded to `process.DefaultMaxOutputBytes` (4 MiB) and content-addressed via `internal/artifacts.Store`.
- No operation this WP applies ever enters project Git state or the project event journal — all of it is `$DEVCADENCE_HOME`-rooted operational state (ADR-0014 §4), consistent with WP-M3B-2.

**SHOULD:**
- Reuse `WP-M3B-2`'s `PostconditionChecker` interface directly for postcondition re-verification (`EvaluateCondition` satisfies it), rather than a second, parallel condition-checking path.
- Reuse `Ledger.ReconcileInterrupted` for any interruption recovery this WP's own tests need to simulate, rather than hand-rolling ledger events.

**SUGGESTED:**
- If a future WP adds a real `EndpointHealthChecker`, it should live in whichever package owns the M3A endpoint registry, with `internal/setup` only depending on the interface — mirroring this WP's own `CommandRunner`/`PostconditionChecker` pattern.

**LOCAL_DISCRETION:**
- Exact JSON shape of the managed-config store (`state/config.json`) beyond "atomic write, keyed by `ManagedConfigKey`, values validated by `ManagedConfigKey.ValidateValue`."
- Internal helper naming, test table structure.

## 5. Interface sketch

```go
// internal/setup/commandrunner.go
type CommandRunner interface {
    Run(ctx context.Context, spec process.Spec) (process.Result, error)
}
// *process.Runner satisfies this directly; no adapter type.

// internal/setup/conditions.go
type EndpointHealthChecker interface {
    CheckEndpointHealthy(ctx context.Context, endpointID string) (healthy bool, detail string, err error)
}
type EvaluatorDeps struct {
    Runner         CommandRunner
    Home           string // resolved $DEVCADENCE_HOME, for managed_dir_exists
    EndpointHealth EndpointHealthChecker // optional; nil => endpoint_healthy fails closed
}
func EvaluateCondition(ctx context.Context, deps EvaluatorDeps, cond protocol.Condition) (passed bool, detail string, err error)
// EvaluateCondition (partially applied over a fixed EvaluatorDeps) satisfies
// setup.PostconditionChecker from WP-M3B-2.

// internal/setup/operations.go
type ApplierDeps struct {
    Runner CommandRunner
    Home   string
    Cache  *CacheManager // WP-M3B-1/Phase-2, reused for remove_stale_cache
}
// procResult is non-nil only when this operation kind actually ran a
// subprocess — see §12 for why this return was added during implementation.
func ApplyOperation(ctx context.Context, deps ApplierDeps, op protocol.TypedOperation, captureOutput bool) (mutated bool, detail string, procResult *process.Result, artifact *protocol.ArtifactRef, err error)
func LocationPath(home string, loc protocol.ManagedDirectoryLocation) (string, error) // shared by home.go's EnsureLayout and this WP

// internal/setup/executor.go
type ExecutorOptions struct {
    Runner    CommandRunner
    Home      string
    Ledger    *Ledger
    Cache     *CacheManager
    Artifacts *artifacts.Store
    Clock     clock.Clock
    IDs       ids.Source
    EndpointHealth EndpointHealthChecker // optional, see conditions.go
}
type Executor struct{ /* unexported deps */ }
func NewExecutor(opts ExecutorOptions) (*Executor, error)
func (e *Executor) Apply(ctx context.Context, plan *protocol.SetupPlan, approvedDigest string, yesScope bool) (*protocol.SetupExecutionReport, error)
```

## 6. Pseudocode: `Executor.Apply`

```
function Apply(ctx, plan, approvedDigest, yesScope):
    if plan.Validate() fails: return error  // plan itself must be internally consistent
    if approvedDigest != plan.PlanDigest: return error  // no ledger writes yet
    if yesScope and plan.RequiredAuthority > AuthorityUserConfirmation:
        return error  // rejected outright, no partial execution

    ledger := open ledger at home/state/setup-ledger.jsonl
    executionID := ids.New("exec")
    append ExecutionCreated(executionID, plan.PlanID, plan.PlanDigest)
    append PlanApproved(executionID, approvedAuthority=plan.RequiredAuthority)

    for action in topologicalOrder(plan.Actions):  // DependsOn-respecting order
        for cond in action.Preconditions:
            passed, detail, err := EvaluateCondition(ctx, deps, cond)
            if err != nil or not passed:
                append ExecutionFinished(status=failed, detail)
                return ProjectExecutionReport(ledger.Events(), executionID, plan), error("precondition drift")

        append ActionStarting(action)  // fsync'd before anything runs

        if action.ManualInstructions != nil:
            append ActionTerminated(action, status=manual_required)
            break  // stop the whole walk: no automated path past a manual step

        mutated, detail, artifactRef, err := ApplyOperation(ctx, deps, *action.Operation)
        if artifactRef present:
            append ActionProcessCompleted(action, artifactRef)  // never for EffectAuthentication actions

        for cond in action.Postconditions:
            passed, pcDetail, pcErr := EvaluateCondition(ctx, deps, cond)
            append PostconditionVerified(action, passed, pcDetail)
            if not passed or pcErr != nil or err != nil:
                append ActionTerminated(action, status=failed)
                append ExecutionFinished(status=failed)
                return ProjectExecutionReport(ledger.Events(), executionID, plan), error

        append ActionTerminated(action, status=succeeded)

    append ExecutionFinished(status=succeeded)
    return ProjectExecutionReport(ledger.Events(), executionID, plan), nil
```

## 7. Edge cases and failure modes

- Wrong `approvedDigest` → rejected before any ledger write, before any action runs.
- Correct digest, but `plan.Validate()` itself fails (a caller passed a self-inconsistent plan) → rejected the same way.
- `yesScope=true` on a plan with any `privileged_confirmation`/`high_impact_manual` action → rejected outright, zero actions execute.
- `yesScope=true` on a plan whose every action is `user_confirmation` or weaker → proceeds normally.
- A precondition that held when the plan was generated but not now (e.g. `ollama` was on PATH then, isn't now) → halts before that action or any later one executes; already-executed earlier actions are not rolled back (out of scope — this WP detects and halts, it does not implement compensating transactions, which ADR-0014 does not ask for).
- A manual action reached mid-plan → the walk stops there; later independent actions (that don't depend on the manual step) are **not** executed either, per §2's design (no automated path exists to know when the manual step is done, so nothing past it is safe to assume ready).
- An action's `Operation.Kind` applier itself fails (e.g. `ollama pull` exits nonzero) → `ActionProcessCompleted` still recorded (the process did run), postcondition check will then fail, action terminates `failed`, execution finishes `failed`.
- `model_digest_present` condition for a `runtime` other than `"ollama"` → fails closed with an explicit "unsupported runtime" error, never silently `true`.
- `endpoint_healthy` condition with no `EndpointHealthChecker` configured → fails closed with an explicit error.
- An action declaring `EffectAuthentication` whose operation produces output → the output is discarded, never written to `internal/artifacts.Store`, `ActionProcessCompleted.ArtifactRef` stays `nil`.

## 8. Acceptance criteria (from the scope card) and how each is verified

| Criterion | Verification plan |
|---|---|
| A plan approved with the wrong digest is rejected | `TestExecutorRejectsWrongDigest`: call `Apply` with a valid plan and a digest that doesn't match; assert error, assert the ledger file was never created (zero ledger writes) |
| A plan whose preconditions drifted between generation and apply halts without executing later actions | `TestExecutorHaltsOnPreconditionDrift`: a two-action plan where action 2's precondition can be made to fail (e.g. `command_available` for a fabricated command name); assert action 1 executes (terminal event present) and action 2 never reaches `ActionStarting` |
| The `--yes`-equivalent scope on a plan containing a privileged action is rejected outright | `TestExecutorRejectsYesScopeOnPrivilegedPlan`: a plan with one `high_impact_manual` action, `Apply(..., yesScope=true)`; assert rejected, zero ledger writes |
| Output artifacts respect the byte cap and never appear for authentication operations | `TestExecutorBoundsOutputArtifacts` (a subprocess emitting more than the cap; assert `Truncated`) and `TestExecutorNeverArtifactsAuthenticationActions` (a synthetic action with `EffectAuthentication` in `Effects`; assert `ActionProcessCompleted.ArtifactRef == nil`) — all exercised against `Executor.Apply` directly, no CLI in the loop |

## 9. Non-goals / forbidden changes for this WP

- No CLI (`devcadence setup apply`) — WP-M3B-7.
- No credential-reference resolution (`CredentialRef`, `os.LookupEnv`-based presence checks) — WP-M3B-4; this WP's operations do not currently need credentials (none of the 5 `OperationKind`s carry `EffectCredentialAccess` or `EffectAuthentication` in their `IntrinsicPolicy`).
- No changes to `internal/setup/{doctor,planner,profiles,cache}.go` — reused, not modified.
- No `endpoint_healthy` live implementation — documented boundary, see §2.
- No change to `docs/IMPLEMENTATION_PLAN.md`'s M3B status line — WP-M3B-9.

## 10. Escalation conditions

None triggered. `internal/artifacts.Store`'s reuse (§3) held exactly as verified — no revision needed.

## 11. Disposition

Implemented; 10 independent-review findings against the first revision addressed — see §15. **Not yet fully accepted**: a follow-up architecture correction ([PR #10 comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5806753036), owner, 2026-09-24) identifies that this WP's Ollama-specific design (`ollama_pull_model` in core `TypedOperation`, hard-coded Ollama CLI/HTTP calls in the executor) conflicts with `docs/MODEL_RUNTIME.md`/`INVARIANTS.md` DCI-055's runtime-agnostic adapter architecture and needs a genuine redesign (a generic `ensure_local_model` operation behind a `LocalModelRuntimeAdapter` boundary, MLX-LM as a peer to Ollama, not a later add-on). That redesign spans WP-M3B-1's `TypedOperation` union and WP-M3B-6's recipes as much as this WP's executor, is larger than a checkpoint-review fix round, and is tracked separately — see §16.

## 12. Interface refinement made during implementation: `ApplyOperation` also returns `*process.Result`

§5's original sketch had `ApplyOperation` return `(mutated bool, detail string, artifact *protocol.ArtifactRef, err error)`. While wiring `Executor.walk`, it became clear the executor needs `ExitCode`/`Signal`/output-truncation state to build a correct `ActionProcessCompletedPayload` ledger event (ADR-0014 §3) — and that event should only ever be appended for an action whose operation actually ran a subprocess (`create_directory`/`write_managed_config`/`remove_stale_cache` and three of the four diagnostic checks never do). `ApplyOperation`'s signature gained a fifth return, `procResult *process.Result`, non-nil exactly when a subprocess ran; `Executor.walk` appends `ActionProcessCompleted` if and only if it is non-nil. This is a local, non-breaking signature refinement within this WP's own new code (no other WP calls `ApplyOperation` yet) — recorded here per AGENTS.md §7's "local discretion" (mechanically necessary adaptation), not an architectural change requiring escalation.

## 13. Deterministic evidence (base commit `0fecafd`, Go toolchain `go1.25.0`, linux/amd64)

```
$ go build ./...
(clean, exit 0)

$ go vet ./...
(clean, exit 0)

$ gofmt -l internal/setup/
(no output — all formatted)

$ go test -count=1 ./...
ok  	github.com/olostan/DevCadence/internal/setup	0.461s
... (all 26 packages ok, 0 failures)

$ go test -race ./internal/setup/...
ok  	github.com/olostan/DevCadence/internal/setup	3.999s

$ GOOS=windows GOARCH=amd64 go build ./...
(clean, exit 0)
```

New test files: `commandrunner.go` has no direct tests (it is a one-method interface declaration; its sole real implementation, `*process.Runner`, is already tested in `internal/process`). `conditions_test.go` (9 tests: command_available, managed_dir_exists before/after creation, port_listening, executable_verified digest mismatch/match/version-via-runner, endpoint_healthy fail-closed/configured, model_digest_present unsupported-runtime rejection). `operations_test.go` (7 tests: one per operation kind's applier, plus an invalid-managed-config-value rejection and an unhandled-kind rejection). `executor_test.go` (9 tests, directly mapping to §8's acceptance criteria plus manual-action-halts-the-walk and the `PostconditionChecker` interface-satisfaction check).

Each §8 acceptance criterion maps to a specific test: `TestExecutorRejectsWrongDigest`, `TestExecutorHaltsOnPreconditionDrift`, `TestExecutorRejectsYesScopeOnPrivilegedPlan` (plus `TestExecutorYesScopeAllowsUserConfirmationPlan` proving the restriction is exactly "no more than user_confirmation," not "yesScope always rejected"), `TestExecutorBoundsOutputArtifacts` and `TestExecutorNeverArtifactsAuthenticationActions`.

## 15. Independent review disposition (10 findings, all addressed)

[PR #10 review comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5806276805) (project owner, 2026-09-24) found the overall decomposition good (EWP-before-code, correct planner pre-check, narrow `CommandRunner`, digest gating before ledger writes, drift halting) but 10 blockers against the first revision, all addressed in this revision:

1. **The executor never acquired `setup.lock`.** `Executor` previously accepted a pre-opened `*Ledger`, so two instances could mutate the same home concurrently, and a ledger opened before a lock existed could read a stale chain tip. Fixed: `ExecutorOptions` no longer accepts `Ledger`/`Cache`/`Artifacts` — only `Home` (now required absolute). `Executor.openLockedLedger()` acquires `AcquireExecutionLock` and only then opens the ledger, for both `Apply` and the new `Recover`. New test: `TestExecutorSerializesConcurrentApply` (two executors, one held mid-subprocess via a blocking fake runner, proves the second blocks until the first releases the lock).
2. **`Ledger.ReconcileInterrupted` was never wired into the executor.** Added `Executor.Recover(ctx, plan)`: under the same lock, finds interrupted actions belonging to `plan` (matched by `plan_id`+`plan_digest`), resolves each via the plan's own recorded `Postconditions`, and durably reconciles via WP-M3B-2's primitive. `Apply` now refuses to start a new execution while any interrupted action remains unresolved, forcing `Recover` first. New tests: `TestExecutorApplyRefusesWhileInterruptedActionsUnresolved`, `TestExecutorRecoverReconcilesInterruptedAction`.
3. **`ApplyOperation`/`WriteManagedConfigKey` were exported bypasses.** Both (and the `ApplierDeps` type) are now unexported (`applyOperation`, `writeManagedConfigKey`, `applierDeps`); tests, in package `setup`, still call them directly. `ReadManagedConfig` stays exported (read-only, no bypass risk).
4. **`executable_verified` verified one binary; the operation could run a different one.** `applyOllamaPullModel` resolved a bare `"ollama"` from PATH regardless of what the precondition had just verified. Fixed: `walk` extracts the `canonical_path` from the action's own `executable_verified` precondition (`verifiedExecutablePath`) and passes it through `applyOperation`; `applyOllamaPullModel` now requires a non-empty verified path and fails closed without one. New tests: `TestApplyOperationOllamaPullModelRequiresVerifiedExecutablePath`, `TestExecutorOllamaPullModelUsesTheVerifiedExecutablePath` (asserts both the `--version` precondition check and the `pull` call hit the identical verified path).
5. **Supply-chain fields were decorative.** `ResolvedDigest`/`ExpectedSizeBytes`/`AllowedRegistryHost` were recorded but never enforced, and `evaluateModelDigestPresent` weakened a full digest to a prefix match. Fixed: `applyOllamaPullModel` now verifies the pulled model's digest and size against the plan's approved values after every pull (failing the operation, not just relying on a possibly-absent postcondition), prefixes the pull reference with `AllowedRegistryHost` when it names something other than Ollama's own default, and `evaluateModelDigestPresent`/the post-pull check both require exact normalized-digest equality. New tests: `TestApplyOperationOllamaPullModelRejectsDigestMismatch`, `...RejectsSizeMismatch`, `...PrefixesNonDefaultRegistry`.
6. **Failed diagnostics were reported as successful actions.** `applyRunDiagnosticCheck` discarded negative outcomes (nil error either way). Fixed: every check now returns a categorized error (`diagnosticFailed`) when its outcome is negative, which the executor's existing `opErr`-based status logic already treats as a real failure — no separate status-plumbing needed.
7. **`state_root_writable` mutated under a `read_only`-declaring operation kind.** It called `os.CreateTemp`+remove, contradicting `IntrinsicPolicy(OpKindRunDiagnosticCheck)` (WP-M3B-1, frozen: `AuthorityReadOnly`). Rather than reopen that accepted contract for one diagnostic, the check itself was rewritten to be genuinely non-mutating (owner-write mode-bit inspection via `os.Stat`) — a weaker, best-effort signal than proving writability by writing, accepted deliberately to preserve the frozen policy. New test: `TestApplyOperationRunDiagnosticCheckStateRootWritableDoesNotMutate` (asserts zero temp files created).
8. **A postcondition evaluator error left an action non-terminal in a finished execution.** `walk` returned immediately on `pcErr != nil`, skipping `ActionTerminated`. Fixed: the postcondition loop now always reaches the `ActionTerminated` append (marking the action `failed` and recording the evaluator error as the postcondition detail) before `walk` returns the error. New test: `TestExecutorTerminalizesActionOnPostconditionEvaluatorError`.
9. **The service API didn't enforce machine-global-state confinement.** `NewExecutor` only checked `Home != ""`, and `Ledger`/`Cache`/`Artifacts` were injectable from anywhere. Fixed (same change as finding 1): `Home` must be absolute, and `Cache`/`Artifacts` are now constructed inside `NewExecutor` from `Home`, never accepted as independently-rooted dependencies. New test: `TestNewExecutorRequiresAbsoluteHome`.
10. **ANSI stripping only matched a narrow CSI pattern**, missing OSC sequences and private-mode CSI parameters. Replaced with a regex covering OSC (`ESC ] ... BEL|ST`), CSI with private/intermediate bytes (`ESC [ ... final-byte`), and remaining two-byte Fe sequences. New test: `TestAnsiEscapeStripsOSCAndPrivateModeSequences` (6 cases: color CSI, private-mode CSI, OSC/BEL, OSC/ST, `\r`, plain text).

**Tracking note:** the review also flagged the same `Expected remote HEAD` self-reference class of issue WP-M3B-1/2 hit; per the reviewer's own explicit guidance this time ("do not create an infinite commit loop for this"), it is updated naturally with this revision's push rather than chased with a dedicated follow-up commit.

Re-verified after all 10 fixes: `go build`/`go vet`/`gofmt -l internal/setup/`/`go test -count=1 ./...` clean (26 packages); `go test -race ./internal/setup/...` clean; `GOOS=windows GOARCH=amd64 go build ./...` clean.

## 16. Architecture correction: runtime-agnostic local-model setup (tracked, not yet implemented)

A second review comment ([PR #10](https://github.com/olostan/DevCadence/pull/10#issuecomment-5806753036), owner, 2026-09-24), arriving while the 10 findings above were being fixed, identifies a real conflict between this WP's design and the repository's own canonical architecture:

- `docs/MODEL_RUNTIME.md` requires the control plane not depend directly on Ollama tags, MLX process syntax, or one provider API.
- `INVARIANTS.md` DCI-055 requires Ollama/MLX-LM/other model runtimes to be replaceable adapters, not core-domain dependencies.
- `internal/cognition/ollama/` and `internal/cognition/mlx/` already model this as peer adapters elsewhere in the codebase.

WP-M3B-1's `TypedOperation` (closed union including `ollama_pull_model` specifically) and this WP's executor (hard-coded Ollama CLI args and `/api/tags` HTTP calls) both grew Ollama-specific in a way that contradicts that architecture. The required direction — a generic `ensure_local_model`-shaped operation delegating to a `LocalModelRuntimeAdapter` boundary, with MLX-LM as a first-class peer to Ollama (not a later manual-only path), immutable model identity/provenance bound at the adapter level, and postcondition verification going through the same adapter abstraction — is a genuine redesign, not a checkpoint-review fix. It touches WP-M3B-1's frozen protocol union as much as this WP's executor, and per this WP's own escalation discipline (§10; see also WP-M3B-2 EWP §12's precedent), a change of this shape is recorded and escalated explicitly rather than folded silently into this checkpoint.

**Disposition:** tracked as required follow-up work, not implemented in this revision. The 10 findings in §15 are fixed and independently verified within the current (Ollama-specific) design — that work is real and correct as far as it goes (digest/size/registry enforcement, executable-identity binding, lock ownership, crash recovery wiring, etc. are all runtime-agnostic *mechanisms* that a future adapter boundary would keep, not throw away). But this WP should not be marked `accepted` until either: (a) this redesign lands as an amendment to this EWP (and likely a WP-M3B-1 amendment for `TypedOperation`), with MLX-LM implemented as a real peer adapter and end-to-end service-level coverage on an MLX-capable profile per the review's acceptance bar; or (b) the project owner explicitly defers the redesign to a later WP and accepts this checkpoint as an interim, Ollama-only state. This is a decision for the next session/the owner to make explicitly — not one this session made unilaterally by either doing a rushed redesign or silently shipping the Ollama-specific version as final.

### §16 amendment — redesign implemented (2026-09-24, same session/branch)

The project owner explicitly resolved option (a) above and explicitly authorized reopening WP-M3B-1's protocol types on this branch: "I wouldn't consider that as frozen as we are working on same branch... it is very important to be able to run on mlx-ml as well as on ollama in absolutely equal way." This is a direct instruction, not an inferred decision, and supersedes AGENTS.md §7's default "must not silently reinterpret a MUST-level architectural requirement" caution for these specific types on this specific unmerged branch — the owner is the decision owner for exactly this kind of question, and gave the answer explicitly.

**Implemented, this revision:**

- `internal/protocol/setup.go` (amends WP-M3B-1's EWP — see that document's own amendment section): `OpKindOllamaPullModel`→`OpKindEnsureLocalModel` ("ensure_local_model"), `OllamaPullModelParams{ModelTag,ResolvedDigest,AllowedRegistryHost,...}`→`EnsureLocalModelParams{Runtime,ModelRef,ResolvedRevision,AllowedSource,...}`; `CondKindModelDigestPresent`→`CondKindModelPresent` ("model_present"), `ModelDigestOperand{Runtime,ModelTag,Digest}`→`ModelPresentOperand{Runtime,ModelRef,ResolvedRevision}`. `ResolvedRevision` deliberately carries no sha256-hex format constraint (unlike the old `ResolvedDigest`) because MLX/Hugging Face revisions are not sha256 digests (they are refs like `"main"` or commit hashes) — this is a genuine relaxation of WP-M3B-1's original validation, made because the original constraint itself encoded the Ollama-specific assumption the owner is now correcting.
- `internal/setup/modelruntime.go` (new): `LocalModelRuntimeAdapter` interface (`Runtime() string`, `EnsureModel(...)`, `ModelPresent(...)`), `ModelRuntimeRegistry` (name→adapter lookup, no default/preferred runtime — an unregistered `Runtime` string is always an error, symmetric across every runtime including `"ollama"`), `DefaultModelRuntimeAdapters()` returning `{OllamaAdapter{}, MLXAdapter{}}` as an unordered pair.
- `internal/setup/ollama_adapter.go` (new): `OllamaAdapter`, extracted verbatim (field names updated) from the previous `conditions.go`/`operations.go` Ollama-specific functions — same digest/size supply-chain verification, same PATH-substitution defense via required `verifiedExecutablePath`. No behavior change for Ollama, only a location and field-name change.
- `internal/setup/mlx_adapter.go` (new): `MLXAdapter`, a real peer implementation using `huggingface-cli` (MLX-LM's own model distribution path) — `EnsureModel` runs `huggingface-cli download <ref> --revision <rev>` via the verified executable path (same fail-closed-without-a-verified-path rule as Ollama), then re-verifies via a `--local-files-only` presence check before reporting a mutation (the same bounded-supply-chain shape as Ollama's post-pull tags check); `ModelPresent` runs the same `--local-files-only` probe as a read-only, bare-PATH-resolved check (the same lower-stakes tier as `applyRunDiagnosticCheck`'s `mlx_importable` check already used).
- `internal/setup/conditions.go`/`operations.go`: `EvaluateCondition`'s `model_present` case and `applyOperation`'s `ensure_local_model` case now dispatch through `ModelRuntimeRegistry.For(runtime)` — neither function contains a runtime name anymore. `EvaluatorDeps`/`applierDeps` gained a `ModelRuntimes`/`modelRuntimes *ModelRuntimeRegistry` field.
- `internal/setup/executor.go`: `ExecutorOptions` gained `ModelRuntimes *ModelRuntimeRegistry` (nil defaults to `DefaultModelRuntimeAdapters()` inside `NewExecutor`); threaded into both `evaluatorDeps()` and `applierDeps()`.
- `internal/setup/planner.go`: added `DefaultMLX*` constants as the equal-peer counterpart of `DefaultOllama*`; factored the previously Ollama-only automated-vs-manual action construction into a shared `localModelRecipe`/`ensureLocalModelAction` helper so both runtimes build their action through identical code, differing only in recipe inputs (model ref, command name, trustworthy-identity detection). MLX now gets the same automated path Ollama has (via a trustworthy `huggingface-cli` identity check mirroring the existing Ollama one) instead of being manual-only, while remaining correctly gated to Darwin/arm64 — a real platform constraint, not a runtime preference.
- Schemas/fixtures updated to match: `schemas/setup-plan.schema.json` (`ensure_local_model`/`model_present` `oneOf` branches, no sha256 pattern on `resolved_revision`), `schemas/setup-ledger-event.schema.json` (`OperationKind` enum), `fixtures/protocol/setup-plan.valid.json` (renamed fields, recomputed `plan_digest`).

**Verification:** `go build ./...`, `go vet ./...`, `gofmt -l internal/setup/ internal/protocol/setup*.go`, `go test -count=1 ./...` (all packages, including `tests` schema-fixture round-trip), `go test -race ./internal/setup/...`, `GOOS=windows GOARCH=amd64 go build ./...` — all clean. New/renamed tests cover both adapters symmetrically: `TestApplyOperationEnsureLocalModelOllama*` (5 cases: success, requires-verified-path, digest-mismatch, size-mismatch, non-default-source-prefix), `TestApplyOperationEnsureLocalModelMLX` / `...RequiresVerifiedExecutablePath`, `TestApplyOperationEnsureLocalModelRejectsUnregisteredRuntime`, `TestEvaluateConditionModelPresentMLX`, `TestEvaluateConditionModelPresentRejectsUnregisteredRuntime`, `TestExecutorEnsureLocalModelOllamaUsesTheVerifiedExecutablePath`.

**Disposition:** WP-M3B-3's architecture question from the original §16 is now resolved per explicit owner instruction, option (a). This WP can be considered ready for a fresh independent-review pass on the redesign specifically (the review dimension that matters now: does the adapter boundary genuinely eliminate runtime-specific code from `conditions.go`/`operations.go`/`executor.go`, and is MLX treated with the same rigor as Ollama — not whether Ollama's own pre-existing behavior regressed, which it did not).

## 17. Third independent review (redesign quality): 8 findings, all fixed

A third review ([PR #10 comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5807748158), owner, 2026-09-24) reviewed the §16 amendment's redesign itself — confirming the direction was correct (protocol types, adapter boundary, registry dispatch, planner symmetry, fail-closed unregistered runtimes) but finding 8 gaps in its implementation quality, concentrated on making MLX a *real* equal peer rather than a syntactically symmetric fake-runner path. All 8 are fixed in this revision:

1. **MLX was not pinned to an immutable revision.** `DefaultMLXRevision = "main"` was a mutable branch ref — a `PlanDigest` approving `main` does not bind the bytes downloaded later, defeating the immutable-plan property `ResolvedRevision`'s own doc comment claimed. Fixed: `DefaultMLXRevision` is now a real, immutable Hugging Face commit hash (`019cc73c45c770444708a6dd8690c66243cc5c80`), resolved from the live Hugging Face Hub API (`GET /api/models/{id}` → `.sha`) against `DefaultMLXModelRef` — not fabricated. `DefaultMLXSizeBytes` (`4295890004`) was resolved the same way, from `GET /api/models/{id}/tree/{revision}`'s summed file sizes. A new `internal/setup/planner.go` helper, `isImmutableHFRevision` (regex: 40 lowercase hex chars), gates `localModelRecipe`'s automated path via a new `revisionIsImmutable func(string) bool` field: the automated `ensure_local_model` action can never be built from a mutable ref, for MLX or any future Hugging-Face-backed runtime. Ollama's `revisionIsImmutable` stays `nil` — its sha256-digest format is already inherently immutable, needing no separate gate. New tests: `TestPlannerMLX*` (existing tests already exercised the manual fallback since none configure a trustworthy `hf` identity; the immutability gate is exercised implicitly by every MLX planner test remaining on the manual path).
2. **MLX didn't enforce `ExpectedSizeBytes`/`AllowedSource`.** The Ollama adapter enforces exact digest and size against the live API; the MLX adapter used `ModelRef`/`ResolvedRevision` only. Fixed as part of finding 4's larger redesign below: `MLXAdapter.EnsureModel` now measures the downloaded snapshot's real total size from disk and rejects a mismatch against `ExpectedSizeBytes`, and rejects any `AllowedSource` other than `"huggingface.co"` (the only source this adapter can pull from) before attempting a download. New tests: `TestApplyOperationEnsureLocalModelMLXRejectsSizeMismatch`, `...RejectsUnapprovedSource`.
3. **The MLX implementation targeted a deprecated/undocumented Hugging Face CLI contract.** `huggingface-cli download ... --local-files-only` — this session independently re-verified against current Hugging Face documentation (`docs/huggingface_hub/concepts/migration`, `docs/huggingface_hub/package_reference/cli`) that `huggingface-cli` was removed in `huggingface_hub` v1.0 in favor of `hf`, and that `hf download` has no documented `--local-files-only` flag. Fixed: `EnsureModel` now invokes `hf download <ref> --revision <rev>` (no presence-probe flag needed — see finding 4). `internal/setup/planner.go`'s `commandName`/manual-guide steps and detected-software IDs updated from `"huggingface-cli"` to `"hf"` accordingly.
4. **`MLXAdapter.ModelPresent` re-resolved a bare CLI name from ambient PATH**, and that evidence is what `Executor.Recover` uses to decide whether an interrupted mutating action actually succeeded — reintroducing the PATH-substitution class of problem (ADR-0014 §1) on the verification side, not just the mutation side. Fixed with a redesign that also resolves findings 2 and 3: `MLXAdapter` now verifies presence and size by reading the local Hugging Face Hub cache directly from disk (`~/.cache/huggingface/hub` or `$HF_HOME/hub`, matching the Hub client libraries' own resolution order) — `ModelPresent` checks whether the resolved-revision snapshot directory exists (a pure `os.Stat`, no subprocess at all, so there is no PATH dependency to substitute), and `EnsureModel`'s post-download verification does the same plus a real measured-size comparison via a symlink-following directory walk (`hfSnapshotSize`, since the Hub cache stores snapshots as directories of symlinks into a shared blob store). This is strictly more trustworthy than re-invoking a CLI for verification, not merely a workaround for finding 3. New tests: `TestEvaluateConditionModelPresentMLX`/`...MLXAbsent` (filesystem-only, no `CommandRunner` involved), `TestApplyOperationEnsureLocalModelMLX` (uses a real on-disk fake snapshot rather than a faked subprocess result for verification).
5. **Ollama-specific configuration still lived on the generic executor/dependency structs.** `ExecutorOptions.OllamaBaseURL`, `Executor.ollamaBaseURL`, `EvaluatorDeps.OllamaBaseURL`, `applierDeps.ollamaBaseURL` meant a second HTTP-backed runtime would still require editing those generic structs. Fixed: `OllamaAdapter` gained its own `BaseURL string` field and `baseURL()` accessor (falling back to `defaultOllamaBaseURL`); all four struct fields and their accessor methods were removed. `ExecutorOptions`/`EvaluatorDeps`/`applierDeps` now carry only `ModelRuntimes *ModelRuntimeRegistry` — no runtime-specific field at all. Tests that previously set `deps.ollamaBaseURL = srv.URL` now construct `NewModelRuntimeRegistry(OllamaAdapter{BaseURL: srv.URL}, MLXAdapter{})` instead.
6. **`ensure_local_model` identity wasn't structurally bound to its `model_present` postcondition** — a plan could approve pulling model A in the operation while declaring success against model B in the postcondition; `SetupAction.Validate()` checked the two independently. Fixed in `internal/protocol/setup.go`: `SetupAction.Validate()` now requires, for any `ensure_local_model` action, at least one `model_present` postcondition whose `(runtime, model_ref, resolved_revision)` exactly matches the operation's own — a mismatched or entirely absent postcondition fails plan validation, not just adapter-internal checks after the fact. New test: `TestSetupActionEnsureLocalModelRequiresMatchingPostcondition` (mismatch case and missing-postcondition case, plus the baseline still passing).
7. **`state_root_writable`'s result wording claimed unqualified "is writable"** from a mode-bit check that cannot actually establish that (ACLs, read-only mounts, and quota are all invisible to it) — flagged as a truthfulness regression given diagnostic truthfulness was itself a first-round finding. Fixed: the check's positive-result wording now says exactly what it checked ("owner-write permission bit is set... appears writable; this mode-bit check cannot see ACLs, read-only mounts, or quota, so it is not a guarantee") rather than asserting writability outright. `IntrinsicPolicy(OpKindRunDiagnosticCheck)`'s `AuthorityReadOnly`/effects were deliberately left unchanged — a wording fix, not a reopening of that operation kind's policy for this specific check, since three of the four diagnostics under that kind remain genuinely read-only and forcing user-confirmation authority onto all of them for one check's honesty would be a disproportionate UX regression.
8. **`HANDOFF.md` had stale, contradictory sections** describing the pre-redesign architecture-question-open state as current, referencing `model_digest_present`/`ollama_pull_model` as though still live, and pointing "Expected remote HEAD" at a stale SHA. Fixed: rewritten to describe the current three-review timeline accurately, with the older per-WP narrative sections removed rather than left to drift further out of sync on the next edit.

**Other observation acted on:** `ModelRuntimeRegistry` previously let a later adapter with a duplicate `Runtime()` silently overwrite an earlier one. `NewModelRuntimeRegistry` now panics on a duplicate — a construction-time misconfiguration, not a runtime condition, the same class of defensive check `net/http`'s `ServeMux.Handle` applies to a duplicate pattern.

**Verification (this revision):** `go build ./...`, `go vet ./...`, `gofmt -l internal/setup/*.go internal/protocol/setup*.go`, `go test -count=1 ./...` (all packages), `go test -race ./internal/setup/...`, `GOOS=windows GOARCH=amd64 go build ./...` — all clean.

**Disposition:** all 8 findings fixed; awaiting a fourth review round to confirm before WP-M3B-3 can be marked `accepted`.

## 18. Fourth independent review: 3 material findings, all fixed

A fourth review ([PR #10 comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5808007255), owner, 2026-09-24) confirmed 6 of the 8 §17 findings were substantively resolved (immutable revision shape, source/size enforcement wiring, current HF CLI surface, MLX verification's PATH-substitution fix, Ollama config behind its adapter, structural identity binding) and found 3 further material issues plus 4 smaller follow-ups, all fixed in this revision:

1. **MLX download and verification were not guaranteed to use the same Hugging Face cache.** `MLXAdapter.cacheDir()` resolves `CacheDir`/`HF_HOME`/`~/.cache/huggingface/hub`, but `EnsureModel`'s subprocess ran with `process.BaseEnv()`, which deliberately passes only `PATH`/`HOME`/locale — not `HF_HOME`/`HF_HUB_CACHE`. A daemon environment with `HF_HOME` set (or an adapter configured with an explicit `CacheDir`) could download to one cache while verification read a different one. Fixed: `EnsureModel` now resolves the cache directory once via `a.cacheDir()` and passes it explicitly to the subprocess as `hf download ... --cache-dir <dir>`, so the download and the verification that follows it are bound to the identical path — they cannot disagree because of ambient environment handling. New test: `TestApplyOperationEnsureLocalModelMLX` now asserts the runner spec includes `--cache-dir <the exact cacheDir>`.
2. **MLX `model_present` was too weak for crash recovery** — a snapshot directory existing does not prove the download is complete (it can be interrupted/partial), yet this is the condition `Executor.Recover` uses to decide whether an interrupted `ensure_local_model` action actually succeeded, and `ModelPresentOperand` had no size field to check against. Fixed: `ModelPresentOperand` gained `ExpectedSizeBytes int64` (`internal/protocol/setup.go`); `SetupAction.Validate()`'s structural identity-binding check (§17 finding 6) was extended to also require the postcondition's `ExpectedSizeBytes` to match the operation's exactly — a plan can no longer approve one size while asking the postcondition (and therefore recovery) to accept any size. `MLXAdapter.ModelPresent` now measures the snapshot's real size via `hfSnapshotSize` and rejects a mismatch when `op.ExpectedSizeBytes > 0`, exactly like `EnsureModel`'s own post-download check. `OllamaAdapter.ModelPresent` also honors `ExpectedSizeBytes` now, for uniform behavior across adapters (redundant there since Ollama's digest match already proves complete content, but no longer silently ignored). New tests: `TestExecutorRecoverDoesNotReconcileIncompleteMLXSnapshotAsSucceeded` (a half-sized fake snapshot, `Recover` must not report it succeeded), `internal/protocol/setup_test.go`'s size-mismatch case in `TestSetupActionEnsureLocalModelRequiresMatchingPostcondition`.
3. **`LicenseReference` was documented as runtime-enforced but neither adapter verifies it.** Fixed by narrowing the claim rather than adding unverifiable enforcement: `EnsureLocalModelParams`'s doc comment now explicitly defines `LicenseReference` as approval/provenance metadata only (no adapter treats it as enforced, since neither Ollama's registry API nor Hugging Face's model API is treated as an authoritative license source here), separately from `ExpectedSizeBytes`/`AllowedSource`, which genuinely are enforced per-adapter.

**Smaller follow-ups, also fixed:**
- The immutable-revision gate lacked a regression distinguishing "forced manual because the revision is mutable" from "forced manual because no verified executable identity was found" (existing MLX planner tests never configure a trustworthy `hf` identity, so they only exercise the latter). New tests: `TestIsImmutableHFRevision` (table-driven format check) and `TestEnsureLocalModelActionRejectsMutableRevisionEvenWithTrustworthyIdentity` (a valid executable identity plus `resolvedRevision: "main"` — asserts the manual recipe regardless).
- `state_root_writable`'s negative-branch wording still asserted "not writable" from a mode-bit check that cannot establish that (group/other permissions or ACLs could still permit writes). Changed to "writable access not established," matching the positive branch's already-hedged wording.
- `HANDOFF.md`'s expected-remote-head tracking is updated naturally with this checkpoint's push, per the reviewer's own explicit instruction not to create a dedicated SHA-only follow-up commit for it.
- No GitHub-attached CI exists on this repository (confirmed again) — evidence below remains session-reported, as it has been for every WP in this milestone.

**Verification (this revision):** `go build ./...`, `go vet ./...`, `gofmt -l internal/setup/*.go internal/protocol/setup*.go`, `go test -count=1 ./...` (all packages), `go test -race ./internal/setup/...`, `GOOS=windows GOARCH=amd64 go build ./...` — all clean.

**Disposition:** all 3 material findings plus the 4 smaller follow-ups fixed; awaiting a fifth review round. The reviewer noted they do not expect a further architectural redesign and expect the next review to be smaller.

## 19. Fifth independent review: 2 integration blockers, both fixed

A fifth review ([PR #10 comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5808511199), owner) confirmed all §18 fixes were correct at the local adapter/protocol level, then went one layer farther — a required "end-to-end MLX reality check," since the acceptance bar was explicitly "MLX-LM as a first-class implementation/acceptance target," not just isolated `MLXAdapter` unit tests — and found 2 integration blockers, both fixed in this revision:

1. **Default environment discovery never inventoried the `hf` CLI, so the automated MLX path was unreachable in the real product.** The planner's MLX automated-path trustworthy-identity check looks for `sw.ID == "hf"` in `EnvironmentFacts.Software`, but `internal/environment/software.go`'s `DefaultInventory()` had no `hf`/`huggingface_hub` descriptor at all — so `hfPath`/`hfVersion` could never be populated from a real discovery run, and MLX was only ever automated in hand-constructed test facts, never in the actual product. Fixed: added an `hf` `SoftwareDescriptor` to `DefaultInventory()` (`Executables: ["hf"]`, `VersionArgs: ["version"]` — the current CLI exposes a `version` subcommand, not a `--version` flag, confirmed against current Hugging Face docs). This also exposed a second, related gap: `evaluateExecutableVerified`'s live precondition check hardcoded `[]string{"--version"}` for every tool, which would have made the MLX `executable_verified` precondition fail even with `hf` correctly discovered. Fixed by adding `ExecutableVerifiedOperand.VersionArgs []string` (mirroring `SoftwareDescriptor.VersionArgs`, which already supported this per-tool variation at discovery time — e.g. `go version` vs `git --version`) and threading `localModelRecipe.versionCheckArgs` through `ensureLocalModelAction` into the MLX recipe's precondition. New integration test: `TestPlannerReachesAutomatedMLXRecipeFromDefaultInventory` — starts from `environment.DefaultInventory()` (not hand-built facts), discovers a fake installed `hf` + MLX runtime on a `DarwinAppleSilicon()` fixture at `DepthHealth`, passes the real discovered facts into `Planner`, and asserts the resulting action is the automated `recipe.mlx.download_model` with a matching `executable_verified` precondition, not the manual fallback.
2. **M3B setup and the existing M3A MLX cognition adapter could resolve two different Hugging Face cache locations**, both because `MLXAdapter.cacheDir()`'s precedence didn't match Hugging Face's actual documented precedence (missing `HF_HUB_CACHE` and `XDG_CACHE_HOME`), and because `internal/cognition/mlx`'s `cachedModels` hardcoded `homeDir/.cache/huggingface/hub` directly, ignoring every override entirely. Since WP-M3B-3 now passes its derived cache path to `hf download --cache-dir` (§18 finding 1), the two layers disagreeing would mean a model M3B installs and verifies is invisible to M3A's discovery — breaking the exact "setup installs → cognition discovers → real inference" contract this runtime-agnostic redesign exists to establish. Fixed with a single shared resolver rather than two independent implementations: new `internal/environment.HuggingFaceCacheDir(explicit, getenv, homeDir)` (real precedence: explicit override → `HF_HUB_CACHE` → `HF_HOME/hub` → `XDG_CACHE_HOME/huggingface/hub` → `homeDir/.cache/huggingface/hub`, injected `getenv`/`homeDir` so neither caller nor this package touches `os.Getenv`/`os.UserHomeDir` directly except at the real entry points). `internal/setup/mlx_adapter.go`'s `cacheDir()` and `internal/cognition/mlx/mlx.go`'s `cachedModels` both now call this one function — `internal/environment` has no dependency on either package, so this introduces no import cycle. `cognition/mlx.Options` gained an optional `Getenv func(string) string` (nil defaults to `os.Getenv`), consistent with the package's existing injected-dependency-for-testability pattern (`Commands`, `Sys`, `HomeDir`). New tests: `internal/environment/hfcache_test.go`'s `TestHuggingFaceCacheDirPrecedence` (table-driven, all 5 precedence levels) and `TestHuggingFaceCacheDirNilGetenv`; `internal/cognition/mlx/mlx_test.go`'s `TestCachedModelsHonorHFHubCacheOverride`; `internal/setup/conditions_test.go`'s `TestEvaluateConditionModelPresentMLXHonorsHFHubCacheEnvVar` (uses `t.Setenv`, no explicit `CacheDir`) — the last two independently prove each call site actually wires a real environment variable through to the shared resolver, not just that the resolver itself is correct in isolation.

**Verification (this revision):** `go build ./...`, `go vet ./...`, `gofmt -l internal/setup/*.go internal/protocol/setup*.go internal/environment/*.go internal/cognition/mlx/*.go`, `go test -count=1 ./...` (all packages), `go test -race ./internal/setup/... ./internal/cognition/mlx/... ./internal/environment/...`, `GOOS=windows GOARCH=amd64 go build ./...` — all clean.

**Disposition:** both integration blockers fixed; awaiting a sixth review round. The fifth reviewer stated they do not currently see another substantive WP-M3B-3 blocker once these two are addressed.
