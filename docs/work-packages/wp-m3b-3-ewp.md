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

Implemented; pending independent review, matching the WP-M3B-1/2 checkpoint pattern (this checkpoint is only marked `accepted` once that review's findings, if any, are closed).

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
