# Engineering Work Package — WP-M3B-7: CLI Surface and Minimal Guided Interaction

- **Author:** Valentyn Shybanov <olostan@gmail.com>
- **Date:** 2026-09-25
- **Base commit:** `bae6d2a86f0a78ba36f36296be04106be0c7fbcc` (`feat/m3b-guided-bootstrap`, includes accepted WP-M3B-1 through WP-M3B-6 checkpoints)
- **Branch:** `feat/m3b-guided-bootstrap`
- **Depends on:** WP-M3B-1 (`SetupPlan`/`SetupAction`/`TypedOperation`, accepted), WP-M3B-2 (ledger/executor sandbox, accepted), WP-M3B-3 (runtime-neutral local model adapters, accepted), WP-M3B-4 (`CredentialRef`/`AuthEvidence`, accepted), WP-M3B-5 (Doctor readiness and ResourceInventory, accepted), WP-M3B-6 (bounded recipes, pre-resolution, and hardware verification, accepted).
- **Status:** Authored and frozen prior to implementation code. Implemented; independent review round 1 found 4 FIX_NOW findings (exit code 5 was unreachable for invalid plan artifacts; `setup recover --json` emitted an ungoverned ad hoc map instead of a typed/versioned public shape; `doctor --scope`/`doctor --target` accepted input with no effect and `setup plan` permitted ambiguous target arguments; the acceptance evidence overclaimed exact deterministic coverage of several verification-matrix rows) — all fixed, see §8.

---

## 1. Objective and Architectural Intent

WP-M3B-7 implements the public CLI surface for `devcadence doctor` and `devcadence setup`, wiring the underlying service APIs from `internal/setup` (and `internal/environment`, `internal/cognition`, `internal/credentials`) to user-facing and machine-readable command interfaces.

Per **AGENTS.md §1, §3, §13, §16** and **ADR-0014 §2, §5, §7**:
1. **Adapter Boundary:** The CLI is strictly an adapter — parsing arguments, invoking service methods, and rendering output. Zero diagnostic, planning, approval, or execution domain logic lives in CLI command files.
2. **Two-Step Approval Workflow:**
   - Step 1: `devcadence setup plan [target] --output <file>` (or `devcadence doctor --fix --output <file>`) discovers facts, runs diagnostics, and generates an immutable, content-addressed `SetupPlan`.
   - Step 2: `devcadence setup apply --plan <file> --approve-plan <sha256:digest> [--yes]` verifies the approval digest, evaluates preconditions against live machine state, and applies actions through the sandboxed executor.
3. **Defined Exit-Code Contract:** Scripts and automation can reliably distinguish:
   - `0`: **No-Op / Clean Success** (system is already ready, plan has 0 pending actions, or execution succeeded).
   - `1`: **Execution Failure** (an action failed execution, doctor encountered an unrecoverable failure, or internal error).
   - `2`: **Invalid Argument** (CLI argument or flag parsing error).
   - `3`: **Not Found** (plan file or referenced target does not exist).
   - `4`: **Drift / Refresh Required** (precondition drift detected between plan generation and application, or interrupted run in ledger requires recovery; `CategoryConflict`).
   - `5`: **Integrity / Unsupported Schema Version** (`CategoryIntegrity`, `CategorySchemaVersionUnsupported`).
   - `6`: **Plan Generated** (a setup plan with 1 or more pending actions was generated and requires operator approval).
4. **Terminal and SSH Usability:**
   - Safe plain-text and structured `--json` outputs.
   - Minimal interactive confirmation prompt with basic terminal / SSH fallback.
   - Non-interactive execution requires explicit `--approve-plan <digest>` and emits **zero control sequences** (ANSI escapes).
   - `--no-tui` is supported as a compatibility/no-op flag (rich TUI is deferred to M3D per ADR-0018).

---

## 2. Scope and Deliverables

1. **`devcadence doctor` command:**
   - Flags: `--json`, `--fix`, `--output <path>`, `--depth <inventory|health>`, `--scope <target>`, `--profile <name>`, `--target <scope>`, `--no-tui`.
   - Invokes `setup.NewDoctor` and `Doctor.Run` over observed facts.
   - Without `--fix`: emits plain-text diagnostic summary or JSON `DoctorReport` (validated against `schemas/doctor-report.schema.json`).
     - Returns `0` if readiness is `READY` or `READY_WITH_REDUCED_CAPABILITY`.
     - Returns `1` (`CategoryValidationFailed`) if readiness is `PARTIALLY_READY` or `ACTION_REQUIRED`.
   - With `--fix`: invokes `setup.NewPlanner` and `Planner.PlanWithContext` on the generated report.
     - If plan has 0 actions: prints "no actions needed" and exits `0` (no-op).
     - If plan has >= 1 actions: saves plan if `--output` specified, prints summary / JSON, and exits `6` (plan generated).
2. **`devcadence setup plan [target]` command:**
   - Flags: `--output <path>`, `--json`, `--profile <name>`, `--depth <inventory|health>`, `--no-tui`.
   - Optional positional argument `target` (default: `all`; valid: `all`, `hardware`, `inference`, `cognition`, `auth`).
   - Discovers facts, runs `Doctor.Run`, then `Planner.PlanWithContext`.
   - Validates generated `SetupPlan` via `plan.Validate()` and schema twin `schemas/setup-plan.schema.json`.
   - If plan has 0 actions: prints "no actions needed" and exits `0` (no-op).
   - If plan has >= 1 actions: writes to `--output` (if passed), prints plan summary or JSON to stdout, and exits `6` (plan generated).
3. **`devcadence setup apply` command:**
   - Flags: `--plan <path>` (required), `--approve-plan <digest>`, `--yes`, `--json`, `--no-tui`.
   - Reads and validates plan from `--plan <path>`.
   - Approval enforcement:
     - If `--approve-plan <digest>` is provided: verifies digest matches `plan.PlanDigest`. Mismatch fails closed with exit code `2` (`CategoryInvalidArgument`).
     - If `--approve-plan` is omitted:
       - If non-interactive (stdin not a terminal): fails closed with exit code `2` (`CategoryInvalidArgument`) demanding `--approve-plan`.
       - If interactive: displays plan summary and prompts `Approve plan <digest>? [y/N]: `. User confirmation sets approval; rejection aborts cleanly.
     - `--yes` flag authorizes `user_confirmation` actions non-interactively; rejects plans requiring `privileged` or `high_impact_manual` authority per ADR-0014 §2.
   - Runs `setup.Executor.Apply`.
   - Precondition drift returns `CategoryConflict` (exit code `4`).
   - Execution failure returns error (exit code `1`).
   - Success outputs plain-text summary or JSON `SetupExecutionReport` (validated against `schemas/setup-execution-report.schema.json`) and exits `0`.
4. **`devcadence setup recover` command:**
   - Flags: `--plan <path>` (required), `--json`.
   - Reconciles interrupted actions from ledger via `setup.Executor.Recover(ctx, plan)`.
   - Outputs status of recovered actions.
5. **Help Text and Documentation:**
   - Clear help text documenting the two-step workflow (`setup plan` -> `setup apply`).
   - Documents the defined exit-code contract and flag semantics.

---

## 3. Brownfield Inspection & Classification

| Component | Status | Classification | Rationale |
|---|---|---|---|
| `cmd/devcadence/run.go` | Existing | **ADAPT** | Register `doctor` and `setup` commands in `commands()`. Add stdin to `env`. |
| `cmd/devcadence/main.go` | Existing | **ADAPT** | Update `exitCode(err)` to support exit code `6` for plan generation via `ExitCoder` interface while preserving all existing error category mappings. |
| `cmd/devcadence/cmd_environment.go` | Existing | **KEEP** | Inspection commands remain read-only; helper `newCognitionService` can be shared or mirrored. |
| `cmd/devcadence/cmd_project.go` | Existing | **KEEP** | `writeJSON` reused as canonical machine-readable formatter. |
| `internal/setup/doctor.go` | Existing (accepted WP5) | **KEEP** | Authoritative diagnostic engine. CLI consumes it without modifying domain logic. |
| `internal/setup/planner.go` | Existing (accepted WP6) | **KEEP** | Authoritative plan generator. Pre-resolution and fail-closed logic preserved. |
| `internal/setup/executor.go` | Existing (accepted WP2/3/6) | **KEEP** | Sandboxed execution, lock management, ledger recording, and drift detection. |
| `cmd/devcadence/cmd_doctor.go` | New | **ADD** | CLI handler for `devcadence doctor` and `--fix`. |
| `cmd/devcadence/cmd_setup.go` | New | **ADD** | CLI handler for `devcadence setup plan`, `setup apply`, `setup recover`. |
| `cmd/devcadence/cli_doctor_test.go` | New | **ADD** | End-to-end CLI tests for `doctor` and `doctor --fix`. |
| `cmd/devcadence/cli_setup_test.go` | New | **ADD** | End-to-end CLI tests for `setup plan`, `setup apply`, approval gating, drift, exit codes, and non-interactive mode. |

---

## 4. Exit-Code Contract Design

```go
const (
    ExitCodeSuccess         = 0 // No-op, clean readiness, empty plan, or successful apply
    ExitCodeExecutionError  = 1 // Action failure, doctor unready (without --fix), internal error
    ExitCodeInvalidArgument = 2 // Bad flags, invalid arguments, missing required inputs
    ExitCodeNotFound        = 3 // File or resource not found
    ExitCodeDrift           = 4 // Precondition drift, conflict, or interrupted state (CategoryConflict)
    ExitCodeIntegrity       = 5 // Data integrity or unsupported schema version
    ExitCodePlanGenerated   = 6 // Non-empty setup plan generated and awaiting approval
)
```

In `main.go`, `exitCode(err error) int` is extended:
```go
type ExitCoder interface {
    ExitCode() int
}
```
If an error implements `ExitCoder`, its `ExitCode()` is honored. Otherwise, `errs.CategoryOf(err)` maps:
- `CategoryInvalidArgument` -> 2
- `CategoryNotFound` -> 3
- `CategoryInvalidTransition`, `CategoryConflict` -> 4
- `CategoryIntegrity`, `CategorySchemaVersionUnsupported` -> 5
- Default -> 1

When `doctor --fix` or `setup plan` generates a plan containing $\ge 1$ actions:
It returns an error carrying `ExitCodePlanGenerated` (6), ensuring automation can detect that a plan was generated without parsing stdout.

---

## 5. Non-Interactive and Terminal Interaction Design

1. **Terminal Detection:**
   `env` contains an `isTerminal func() bool` hook. In production, this checks if `stdin` is a character device (`os.Stdin.Stat()`). In unit tests, it is cleanly configurable.
2. **Interactive Confirmation in `setup apply`:**
   - If `--approve-plan` is omitted and `isTerminal()` is true:
     - Prompt: `Approve plan <plan_digest>? [y/N]: `
     - Read line from `e.stdin`. If trimmed lowercase is `"y"` or `"yes"`, adopt `approvedDigest = plan.PlanDigest`.
     - Otherwise, abort with `errs.New(errs.CategoryPolicyDenied, "plan approval declined by operator")`.
   - If `--approve-plan` is omitted and `isTerminal()` is false:
     - Fail closed: `errs.New(errs.CategoryInvalidArgument, "setup apply: non-interactive execution requires --approve-plan <digest>")`.
3. **ANSI Control Sequence Ban:**
   No ANSI escape sequences (e.g. `\x1b[...m`) are used in plain-text output. Non-interactive and script output is completely clean and readable via basic SSH or plain files.
4. **Compatibility Flags:**
   `--no-tui` is accepted on all `doctor` and `setup` commands as a no-op flag so that existing caller scripts remain forward-compatible when M3D introduces optional rich TUI rendering.

---

## 6. Verification Matrix

| ID | Scenario Description | Tested Command | Expected Result | Exit Code |
|---|---|---|---|---|
| 1 | `doctor` on clean machine (ready) | `devcadence doctor` | Reports readiness summary, plain text | 0 |
| 2 | `doctor --json` schema compliance | `devcadence doctor --json` | Validates against `schemas/doctor-report.schema.json` | 0 |
| 3 | `doctor` on unready machine without fix | `devcadence doctor` (state dirs missing) | Reports `ACTION_REQUIRED` / findings | 1 |
| 4 | `doctor --fix` generates plan | `devcadence doctor --fix --output <file>` | Writes `SetupPlan` to file, prints plan summary | 6 |
| 5 | `doctor --fix --json` schema compliance | `devcadence doctor --fix --json` | Emits `SetupPlan` validating against `schemas/setup-plan.schema.json` | 6 |
| 6 | `doctor --fix` on already ready machine | `devcadence doctor --fix` | Reports no actions required (no-op) | 0 |
| 7 | `setup plan` generates plan | `devcadence setup plan --output <file>` | Writes valid `SetupPlan`, lists actions | 6 |
| 8 | `setup plan --json` schema compliance | `devcadence setup plan --json` | Validates against `schemas/setup-plan.schema.json` | 6 |
| 9 | `setup plan` target scoping | `devcadence setup plan hardware` | Plans only hardware-scoped actions | 6 |
| 10 | `setup apply` with matching digest | `devcadence setup apply --plan <file> --approve-plan <digest>` | Executes actions, records ledger events, reports success | 0 |
| 11 | `setup apply --json` schema compliance | `devcadence setup apply --plan <file> --approve-plan <digest> --json` | Validates against `schemas/setup-execution-report.schema.json` | 0 |
| 12 | `setup apply` digest mismatch fails closed | `devcadence setup apply --plan <file> --approve-plan sha256:wrong` | Rejects execution before acquiring lock or writing ledger | 2 |
| 13 | `setup apply` non-interactive missing digest | `devcadence setup apply --plan <file>` (non-terminal) | Rejects with argument error demanding `--approve-plan` | 2 |
| 14 | `setup apply` interactive approval | `devcadence setup apply --plan <file>` (terminal stdin `y\n`) | Prompts, user approves, executes | 0 |
| 15 | `setup apply` interactive rejection | `devcadence setup apply --plan <file>` (terminal stdin `n\n`) | Prompts, user rejects, aborts without execution | 1 |
| 16 | `setup apply` `--yes` on user_confirmation plan | `devcadence setup apply --plan <file> --approve-plan <digest> --yes` | Executes user-confirmation plan non-interactively | 0 |
| 17 | `setup apply` `--yes` on privileged plan fails | `devcadence setup apply --plan <file> --approve-plan <digest> --yes` | PolicyDenied: `--yes` cannot authorize privileged authority | 1 |
| 18 | `setup apply` halts on precondition drift | `devcadence setup apply --plan <file> --approve-plan <digest>` | Precondition drift detected -> halts execution | 4 |
| 19 | `setup recover` recovers interrupted action | `devcadence setup recover --plan <file>` | Reconciles interrupted action from ledger | 0 |
| 20 | Non-interactive output contains no ANSI | All commands with plain output | String inspection proves zero `\x1b` bytes emitted | 0/6 |
| 21 | Help text documents two-step workflow & exit codes | `devcadence doctor --help`, `devcadence setup --help` | Documents workflow and exit codes | 0 |

---

## 7. Definition of Done

- All 21 verification matrix scenarios pass in automated unit/CLI tests.
- Zero ANSI control characters emitted in plain-text output.
- All JSON outputs strictly validated by Draft 2020-12 schemas.
- `go build ./...`, `go vet ./...`, `gofmt -l <changed_files>`, `go test -count=1 ./...`, `go test -race ./...`, `GOOS=windows GOARCH=amd64 go build ./...`, `GOOS=linux GOARCH=amd64 go build ./...`, and `git diff --check` pass cleanly.
- `HANDOFF.md` updated with WP-M3B-7 status and evidence.

## 8. Independent review round 1 disposition: 4 FIX_NOW findings, all fixed

An independent review ([comment id `5837092761`](https://github.com/olostan/DevCadence/pull/10#issuecomment-5837092761), owner, 2026-09-25, head `c272f35`) found the implementation not yet green for WP-M3B-8, with 4 closure-threshold FIX_NOW findings, all fixed:

1. **Documented exit code 5 was unreachable for invalid plan artifacts.** `setup apply` classified plan decode, semantic-validation, and schema-validation failures as `CategoryInvalidArgument` (exit 2), and `setup recover` did the same while not even schema-validating its input at all — reproduced end to end against the checked-in invalid-target fixture, which exited 2 instead of the documented 5. Fixed: both commands now load their plan artifact through a single new `loadSetupPlanArtifact(path string) (*protocol.SetupPlan, *schema.Set, error)` boundary in `cmd_setup.go`. A missing file is `CategoryNotFound` (exit 3, unchanged); a file that exists but is malformed JSON, fails `SetupPlan.Validate()`, carries an unsupported `schema_version`, or fails `setup-plan.schema.json` is uniformly `CategoryIntegrity` (exit 5) — no longer `CategoryInvalidArgument`. `setup recover` now schema-validates its plan artifact for the first time. The setup help text documents exit 5. New tests: `TestCLISetupApplyMissingPlanFileReturnsExitCode3`, `TestCLISetupRecoverMissingPlanFileReturnsExitCode3`, `TestCLISetupApplyCorruptPlanFileReturnsExitCode5`, `TestCLISetupRecoverCorruptPlanFileReturnsExitCode5`, `TestCLISetupApplyInvalidPlanFileReturnsExitCode5` (unsupported `schema_version`).
2. **`setup recover --json` emitted an ungoverned ad hoc map, outside the public JSON contract.** The recovery JSON response was built from a bare `map[string]any{"plan_id": ..., "statuses": ...}` — never a typed protocol record, never schema-validated before emission, and its `statuses` array was bare `ActionStatus` strings with no `action_id` to associate each status with (a pre-existing loss of information `Executor.Recover`'s own return type, `[]protocol.ActionStatus`, already carried no way to fix from the CLI alone). Fixed: new versioned protocol record `protocol.SetupRecoveryReport` (`schema_version`, `recovery_id`, `plan_id`, `plan_digest`, `recovered_at`, `results: []RecoveryActionResult{action_id, status}`), its own `schemas/setup-recovery-report.schema.json`, registered in `internal/schema/schema.go` (`RecordKindToSchema`, `AllNames`) and `protocol.NewRecord`. `Executor.Recover`'s return type changed from `[]protocol.ActionStatus` to `[]protocol.RecoveryActionResult` (action ID now travels with its status — the two `internal/setup/executor_test.go` call sites updated accordingly) so the CLI has what it needs to build a real record. `setup recover --json` now builds, validates, and emits `SetupRecoveryReport` the same way `setup apply --json` already validates `SetupExecutionReport`. New fixture `fixtures/protocol/setup-recovery-report.valid.json` and round-trip case in `tests/schema_fixtures_test.go`. New test: `TestCLISetupRecoverJSONValidatesAgainstSchema`.
3. **Accepted CLI input was silently ignored or ambiguous.** `doctor --scope` was parsed and unconditionally discarded (`_ = scopeFlag`); `doctor --target` was validated only when `--fix` was also passed, so an invalid value was silently accepted without `--fix`; `setup plan`'s positional-target handling permitted a positional argument to silently conflict with `--target`, and any positional arguments after the first were silently dropped. Fixed: `doctor --scope` now rejects any value other than `"default"` deterministically (`ReadinessEvaluationScope` carries no scope-identifier field to give any other value real meaning — implementing one was out of this repair's scope, so the honest fix is to stop accepting a value that does nothing); `doctor --target` is now validated unconditionally, not gated on `--fix`; `setup plan` now rejects more than one positional argument, and rejects a positional target that disagrees with an explicitly-passed `--target`. New tests: `TestCLIDoctorRejectsUnsupportedScope`, `TestCLIDoctorRejectsInvalidTargetEvenWithoutFix`, `TestCLISetupPlanRejectsConflictingTargetAndExtraPositionals`.
4. **Acceptance evidence overclaimed the verification matrix's exact coverage.** Several matrix rows (readiness `0` vs `1`, `doctor --fix` already-ready/no-op `0`, `--yes` user-confirmation success `0`, missing plan `3`, invalid/corrupt plan `5`, ANSI absence across every plain-output path) were only exercised by CLI-level tests whose outcome depended on the test host's real hardware/software state, so a pass proved nothing exact. Fixed with two complementary approaches:
   - The readiness-to-exit-code and plan-actions-to-exit-code decisions were extracted into pure functions — `doctorReadinessExitError(report *protocol.DoctorReport) error` and `planActionsExitError(plan *protocol.SetupPlan) error` (the latter shared by both `doctor --fix` and `setup plan`, which is why fixing it once fixes both matrix rows) — and are now exercised directly against synthetic `DoctorReport`/`SetupPlan` values in `TestDoctorReadinessExitErrorExactExitCodes` and `TestPlanActionsExitErrorExactExitCodes`, independent of any real hardware state.
   - New deterministic CLI-level tests fill the remaining gaps: `TestCLISetupApplyYesUserConfirmationSucceeds` (the success counterpart to the pre-existing `TestCLISetupApplyYesScopeRejectsPrivilegedPlan`, which only covered the rejection path), the exit-3/exit-5 tests from finding 1 above, and `TestCLISetupRecoverOutputHasNoANSI` for `setup recover`'s previously-unchecked plain-output path.
   - Writing these exact-assertion tests surfaced a genuine, previously-masked defect in the *existing* recovery-ledger test fixtures: `TestCLISetupRecoverReconcilesInterruptedActions` (and this round's own first draft of the new ANSI/JSON tests) discarded `Ledger.Append`'s error (`_, _ = ledger.Append(...)`), and every such append was silently failing validation (`ExecutionCreatedPayload.InitiatedBy` and `ActionStartingPayload.IdempotencyKey` are both required but were never set) — so the ledger stayed empty and `setup recover` always reconciled zero actions, which passed unnoticed because the existing assertion only substring-matched the generic "Setup Recovery Reconciled" banner, never the actual recovered count. Fixed by checking every `Append` error and adding the required fields, factored into a shared `simulateCLIInterruptedAction` test helper (mirroring `internal/setup/executor_test.go`'s own `simulateCrash` helper, including the `plan_approved` event it appends that the CLI tests had been missing) now used by all three CLI recovery tests, each of which now asserts the exact reconciled-action count/ID rather than a generic banner substring.

**Verification (this revision):** `go build ./...`, `go vet ./...`, `gofmt -l` clean on every file this repair touched, `go test -count=1 ./...` (all 30 packages `ok`), `go test -race ./cmd/devcadence/... ./internal/setup/... ./internal/protocol/... ./internal/credentials/... ./internal/schema/... ./internal/environment/... ./tests/...` (clean), `GOOS=windows GOARCH=amd64`/`GOOS=linux GOARCH=amd64`/`GOOS=darwin GOARCH=arm64 go build ./...` (clean), `git diff --check` (clean).

**Disposition:** all 4 FIX_NOW findings fixed; awaiting a focused re-review of the repaired head per AGENTS.md §8A before WP-M3B-7 can be marked `accepted`.
