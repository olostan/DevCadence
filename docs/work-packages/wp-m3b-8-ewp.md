# Engineering Work Package — WP-M3B-8: Verification Suite and Docs Sync

- **Author:** Valentyn Shybanov <olostan@gmail.com>
- **Date:** 2026-09-25
- **Base commit:** `b1a12f1d8b1662752faa31aa8cc0543023e5db6f` (`feat/m3b-guided-bootstrap`, includes accepted WP-M3B-1 through WP-M3B-7 checkpoints)
- **Branch:** `feat/m3b-guided-bootstrap`
- **Depends on:** WP-M3B-1 through WP-M3B-7 (all accepted).
- **Status:** As-built (process deviation, see §7). This EWP was committed in the same commit (`c18d2f6`) as its implementation, tests, and documentation sync, rather than authored and frozen beforehand as AGENTS.md §6 and `AGENT_HANDOFF_PROTOCOL.md` require. It was written and pushed marked `DRAFT (authoring prior to ...)`, which was factually wrong at the moment of push — no pre-implementation frozen commit of this EWP ever existed. The round-1 independent review ([comment id `5838125371`](https://github.com/olostan/DevCadence/pull/10#issuecomment-5838125371)) caught this (FIX_NOW-3) and required it be named accurately rather than fixed by rewriting history. It is named here as the process deviation it was; the independent review itself is the compensating control, also recorded in `HANDOFF.md`. Implemented; independent review round 1 found 4 FIX_NOW groups (vacuous/incomplete closure scenarios; unsynchronized milestone documentation; this EWP's frozen-before-code claim; an incomplete gofmt Definition-of-Done claim) — all fixed, see §8.

---

## 1. Objective and Architectural Intent

WP-M3B-8 closes the **M3B — Guided deterministic bootstrap and onboarding** milestone.

Per **docs/WORK_PACKAGES.md** and **docs/IMPLEMENTATION_PLAN.md**:
1. **Milestone Exit Criterion:**
   > A user can safely discover/configure at least one viable cognition path when possible and obtain an auditable ResourceInventory/readiness state without needing to understand accelerator/runtime/provider details. The workflow is usable from plain terminals and automation; rich adaptive onboarding is not an M3B gate.
2. **Comprehensive Fixture and Operational Verification:**
   Verify the full matrix across all 8 required operational dimensions:
   - Blank machine (complete absence of accelerators, local runtimes, coding CLIs, and credentials);
   - Dry-run mutation visibility (pre-execution inspection of planned operations and expected filesystem mutations);
   - Privileged and high-impact action governance (fail-closed approval enforcement);
   - Interrupted setup recovery through the real CLI;
   - Preference for existing usable tools over redundant downloads or installations;
   - Plain/non-interactive operation with zero ANSI escapes;
   - SSH and basic-terminal fallback behavior;
   - Degradation of readiness and ResourceInventory rather than generic failure.
3. **Schema and Contract Synchronization:**
   - Verify top-level Go/JSON Schema parity for all M3B records in `tests/twin_fields_test.go`.
   - Update `docs/IMPLEMENTATION_PLAN.md` to mark M3B complete with cited test evidence.
   - Update `docs/SETUP.md` to reflect implemented M3B behavior and clearly defer rich adaptive onboarding to M3D.
   - Spot-check `README.md` and `INVARIANTS.md` for consistency.
4. **Handoff Protocol Compliance:**
   - Retain `HANDOFF.md` through the review and repair cycle.
   - Remove `HANDOFF.md` only when preparing the final frozen closure candidate per `AGENT_HANDOFF_PROTOCOL.md`.

---

## 2. Deliverables and Scope Classification

| Item | Component | Action | Description |
|---|---|---|---|
| 1 | `tests/twin_fields_test.go` | ENHANCE | Add 8 M3B record schemas to top-level property parity checks (`doctor-report`, `setup-plan`, `setup-execution-report`, `setup-recovery-report`, `setup-ledger-event`, `credential-ref`, `auth-evidence`, `resource-inventory`). |
| 2 | `tests/m3b_milestone_closure_test.go` | NEW | Author centralized end-to-end milestone closure suite covering the 8 operational fixture areas. |
| 3 | `docs/IMPLEMENTATION_PLAN.md` | UPDATE | Mark M3B status as complete, document all deliverables, and cite deterministic test evidence. |
| 4 | `docs/SETUP.md` | UPDATE | Describe implemented doctor/setup workflow and explicitly document M3D deferral of rich adaptive onboarding. |
| 5 | `README.md` | UPDATE | Update M3B status, add CLI examples for doctor and setup, and maintain alignment with roadmap. |
| 6 | `INVARIANTS.md` | VERIFY | Spot-check DCI-104 through DCI-108 to verify all invariants are preserved and tested. |
| 7 | `HANDOFF.md` | UPDATE | Record WP-M3B-8 progress and full verification evidence. |

---

## 3. Detailed Verification Matrix for the 8 Closure Dimensions

| # | Operational Dimension | Test Implementation | Key Invariant / Contract |
|---|---|---|---|
| 1 | Blank machine | `TestM3BMilestoneClosure_Scenario01_BlankMachine` | DCI-104, DCI-105: Blank machine facts produce valid `DoctorReport` and `ResourceInventory` without panic or error; degrades gracefully to `ACTION_REQUIRED` / `PARTIALLY_READY`. |
| 2 | Dry-run mutation visibility | `TestM3BMilestoneClosure_Scenario02_DryRunMutationVisibility` | DCI-108: `setup plan` and `doctor --fix` produce an immutable `SetupPlan` with explicit `ExpectedMutations` for every action. |
| 3 | Privileged / high-impact approval | `TestM3BMilestoneClosure_Scenario03_PrivilegedApprovalGovernance` | DCI-108: High-impact manual or privileged actions fail closed unless explicitly approved by matching or exceeding authority. |
| 4 | Interrupted setup recovery via CLI | `TestM3BMilestoneClosure_Scenario04_InterruptedSetupRecoveryCLI` | ADR-0014 §6: Interrupted action in setup ledger is detected; `setup recover` reconciles it and produces a valid `SetupRecoveryReport`. |
| 5 | Preference for existing usable tools | `TestM3BMilestoneClosure_Scenario05_PreferenceForExistingUsableTools` | DCI-105, ADR-0014 §7: Satisfied capabilities (e.g. existing authenticated CLI or verified local model) require 0 setup actions. |
| 6 | Plain / non-interactive operation | `TestM3BMilestoneClosure_Scenario06_PlainNonInteractiveOperation` | DCI-108, ADR-0014 §2: Non-interactive runs require explicit approval digest; output contains zero ANSI control characters. |
| 7 | SSH / basic-terminal behavior | `TestM3BMilestoneClosure_Scenario07_SSHBasicTerminalBehavior` | ADR-0014 §2, ADR-0018: Interactive prompt fallback works on basic TTY; `--no-tui` succeeds without requiring cursor escape codes. |
| 8 | Readiness & inventory degradation | `TestM3BMilestoneClosure_Scenario08_GracefulCapabilityDegradation` | DCI-104: Missing optional hardware or expired auth degrades readiness to `READY_WITH_REDUCED_CAPABILITY` or `PARTIALLY_READY` without aborting unaffected paths. |

---

## 4. Documentation Synchronization Strategy

1. **`docs/IMPLEMENTATION_PLAN.md`:**
   - Change `### M3B — Guided deterministic bootstrap and onboarding` status from `in progress` to `complete`.
   - Update deliverables list to record:
     - `devcadence doctor` and `devcadence setup` CLI surface (`cmd/devcadence`);
     - Safe `SetupPlan` / approval / sandboxed executor and append-only ledger (`internal/setup`, `internal/protocol`);
     - Runtime-agnostic local-model setup boundary with MLX-LM and Ollama peer adapters (`internal/cognition/mlx`, `internal/cognition/ollama`);
     - Credential references, `AuthEvidence`, and reuse of authenticated CLI sessions (`internal/credentials`, `internal/protocol`);
     - Deterministic `ResourceInventory` and doctor readiness projection (`internal/setup/doctor.go`, `internal/protocol`);
     - Plain / `--json` / basic-terminal operation with zero ANSI escapes and exit codes 0–6.
   - Record verification evidence and cite the milestone test suites.
2. **`docs/SETUP.md`:**
   - Update `## Status` to state M1, M2, M3A, and M3B are complete.
   - Document how developers run `devcadence doctor` and `devcadence setup`.
   - Explicitly document that rich adaptive portfolio synthesis and rich TUI onboarding are deferred to M3D.
3. **`README.md`:**
   - Update § Running the control plane with M3B commands (`doctor`, `setup plan`, `setup apply`, `setup recover`).
   - Align the active roadmap summary.

---

## 5. Definition of Done

- All 8 operational closure dimensions verified by deterministic automated tests in `tests/m3b_milestone_closure_test.go`.
- All Go twin property parity checks pass in `tests/twin_fields_test.go`.
- Documentation (`docs/IMPLEMENTATION_PLAN.md`, `docs/SETUP.md`, `README.md`) synchronized and accurate.
- `go build ./...`, `go vet ./...`, `go test -count=1 ./...`, `go test -race ./...`, `GOOS=windows GOARCH=amd64 go build ./...`, `GOOS=linux GOARCH=amd64 go build ./...`, and `git diff --check` pass cleanly.
- `gofmt -l .` is clean on every file this WP's implementation touches (changed-file scope). It is **not** clean repository-wide: 12 pre-existing files predate this WP and are unrelated to it (exact list and rationale in §8, finding 4, and in the round-1 revision's verification evidence). Amended to changed-file scope per the round-1 independent review's explicit instruction, rather than leaving the original repository-wide `gofmt -l .` claim standing while evidence silently narrowed it.
- `HANDOFF.md` updated with WP-M3B-8 evidence.

---

## 6. Constraints, Non-Goals, Failure Modes, and Escalation Conditions

Added per the round-1 independent review (FIX_NOW-3): a substantial closure package needs this structure explicitly, not only a deliverables table.

**MUST**
- Every milestone-closure scenario in `tests/m3b_milestone_closure_test.go` must drive the real production code path it claims to verify (a real `Doctor.Run`/`cognition.Service` call, a real CLI subprocess, a real `Executor`/`Planner`) — never a hand-constructed record standing in for what that path would have produced.
- Every closure scenario's assertion must be exact where the claim it backs is exact (a specific exit code, a specific discovered endpoint, a specific plan-action count) — an assertion that accepts multiple unrelated outcomes as equally valid evidence does not close the milestone.
- Directly affected normative/current-phase documentation (`AGENTS.md` §16, `docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md`, `docs/SETUP.md`, `docs/IMPLEMENTATION_PLAN.md`) must be synchronized to the actual accepted M3B state before this WP is accepted.
- This EWP itself must not claim a pre-implementation frozen checkpoint that did not exist.

**SHOULD**
- New closure-suite helpers (fake adapters, scope-lookup helpers) should follow the same patterns already established in `internal/setup`'s own test suites (e.g. `stubCognitionAdapter`, `doctorE2ETestSetup`) rather than inventing a divergent convention.
- Documentation claims about what a test proves (e.g. "field parity") should name the test and match its actual, stated scope rather than a stronger paraphrase.

**Non-Goals**
- This WP does not re-open or re-litigate any accepted WP-M3B-1 through WP-M3B-7 design decision. A closure-scenario fix that appears to reveal a defect in an already-accepted WP's behavior is reported, not silently patched around, unless the fix is strictly local to this WP's own test/doc code (see Escalation Conditions).
- This WP does not add new CLI flags, new protocol fields, or new schema types. (The one exception applied in round-1 repair — a test-only fix to how `setup apply` is invoked in a subprocess test, not a production code or contract change — is documented in §8 finding 1.)
- This WP does not implement a rich adaptive setup TUI; that remains explicitly deferred to M3D per `AGENTS.md` §16.

**Failure Modes Considered**
- A closure scenario that compiles and passes but does not actually exercise the code path or exit code it names in its own comment — the round-1 review's central finding, addressed by driving scenarios through real `cognition.Service`/CLI-subprocess paths and asserting exact outcomes (see §8, finding 1).
- Milestone documentation claiming M3B complete while a normative current-phase document (`AGENTS.md` §16) still names M3B as "next" — addressed in §8, finding 2.
- A frozen-EWP claim that does not match Git history — addressed in §8, finding 3, and in this file's own status line above.
- An exact Definition-of-Done command (`gofmt -l .`) silently narrowed in evidence reporting to a passing subset — addressed in §8, finding 4.

**Escalation Conditions**
- If a milestone-closure scenario, once made to exercise the real code path, reveals that an accepted WP-M3B-1 through WP-M3B-7 decision does not actually hold (not merely that the *test* was wrong), that is reported to the PR/owner as a contradiction against a frozen decision, per AGENTS.md §7 — it is not silently fixed inside this WP. (No such case was found during the round-1 repair: every scenario's failure, once diagnosed, was attributable to the test's own construction — a missing required field, an invalid synthetic report, or a test harness detail such as `/dev/null` reporting as a character device — not to accepted production behavior being wrong. See §8, finding 1's per-scenario detail.)

---

## 7. Process Deviation and Compensating Control

This EWP was authored and committed in the same commit (`c18d2f6919f255a6b1f65623666f7fd34b1a56b2`) as the implementation, tests, and documentation it describes, rather than authored, frozen, and reviewed before implementation code as AGENTS.md §6 and `AGENT_HANDOFF_PROTOCOL.md`'s explicit sequence require. The original pushed status line ("DRAFT (authoring prior to milestone closure verification code and doc sync)") asserted a frozen-before-code state that Git history does not support — the EWP file and the implementation it describes landed in the same commit.

This is named here honestly, not silently corrected, per the round-1 independent review's explicit instruction ("Pushed history must not be rewritten to fabricate the missing checkpoint... Do not claim that a pre-implementation frozen commit existed").

**Compensating control:** the round-1 independent review itself ([comment id `5838125371`](https://github.com/olostan/DevCadence/pull/10#issuecomment-5838125371)) is the review this EWP should have had before implementation began. It reviewed the as-built candidate against AGENTS.md, the WP-M3B-8 scope card, ADR-0014/0018, the handoff protocol, and this EWP, and found the 4 FIX_NOW groups disposed of in §8. No further compensating control is required beyond that review and its repair having actually happened, and being recorded here and in `HANDOFF.md`, rather than glossed over.

---

## 8. Independent review round 1 disposition: 4 FIX_NOW groups, all fixed

An independent review ([comment id `5838125371`](https://github.com/olostan/DevCadence/pull/10#issuecomment-5838125371), owner, 2026-09-25, head `c18d2f6`) found deterministic verification strong (`go build`, `go vet`, `go test -count=1 ./...`, the exact `go test -race -count=1 ./...`, Windows/Linux cross-builds, `git diff --check` all passed independently) but the candidate **not green for M3B closure**, with 4 FIX_NOW groups, all fixed:

1. **Three closure scenarios were vacuous and one did not assert successful flag handling.**
   - *Scenario 5* (existing usable tools) hand-constructed a `DoctorReport` with an empty `EvaluationScope.EvidenceStatus` (which `report.Validate()` would reject) and no real discovery, proving nothing about whether discovery itself recognizes and prefers an existing authenticated CLI. Fixed: the scenario now drives a real `cognition.Service` with a new deterministic `fakeCognitionAdapter` (mirroring `internal/setup`'s own `stubCognitionAdapter` pattern) through `Doctor.Run`, asserts the authenticated CLI actually appears in `report.DiscoveredEndpoints` and `report.ScopeReadiness`, and only then plans from the report `Run` actually produced, asserting zero actions.
   - *Scenario 6* (missing approval) pointed `setup apply` at a nonexistent plan file and accepted exit 3 as equivalent evidence to the documented exit 2, so it never reached the missing-approval boundary at all. Fixed: the scenario now writes a real, valid, on-disk `SetupPlan` and omits `--approve-plan`, requiring exactly exit 2 and an "--approve-plan" diagnostic. This surfaced a test-harness-only issue, not a production defect: leaving `exec.Cmd.Stdin` unset makes the child read from `os.DevNull`, and `/dev/null` itself reports `os.ModeCharDevice` set on Unix (independently confirmed), which would trip the CLI's `isTerminal()` heuristic into treating the run as interactive. Fixed by giving the subprocess an explicit `strings.NewReader("")` stdin (a pipe, which correctly reports as non-interactive), documented inline at the call site; no CLI/production code changed.
   - *Scenario 8* (degraded auth) constructed `Doctor` with no cognition service at all, so the expired-auth endpoint never reached `Doctor.Run` — it was supplied only to a separate `BuildResourceInventory` call, making the report's non-ready state unrelated to the expired auth the scenario claimed to test. Fixed the same way as scenario 5: a real `cognition.Service`/`fakeCognitionAdapter` reporting the expired-auth endpoint feeds `Doctor.Run` itself; `BuildResourceInventory` is then called with that same report's own `DiscoveredEndpoints` (not a separately hand-built list) and a minimal-but-valid `MachineCapabilityProfile` (the same shape `internal/setup`'s own matrix tests use for this purpose); both the report and the resulting `ResourceInventory` are schema-validated.
   - *Scenario 7* (`--no-tui`) discarded the command's error/exit status entirely, so an unknown-flag or parser failure would have passed silently while the scenario claimed the flag "is accepted cleanly." Fixed: the scenario now asserts the process reaches exactly an allowed doctor readiness outcome (exit 0 or 1), never an argument/parser failure (exit 2). Per the review's own offered alternative, basic-TTY interactive approval itself is not duplicated here — it is already proven exactly by the existing `TestCLISetupApplyInteractiveApproval`/`TestCLISetupApplyInteractiveRejection` in `cmd/devcadence/cli_setup_test.go` — so this scenario's scope and doc comment are narrowed to the `--no-tui` compatibility-flag contract its name and code actually exercise.
2. **Milestone and command documentation was not synchronized.** Fixed: `AGENTS.md` §16 now lists M3B as complete (with the next milestone being M3C, not M3B); `docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md`'s M3B section changed from "in progress" to "complete"; `docs/SETUP.md`'s CLI example changed from the unsupported `doctor --target hardware` (rejected with exit 2 by accepted WP-M3B-7 behavior) to `doctor --fix --target hardware`; `docs/IMPLEMENTATION_PLAN.md`'s dangling reference to a nonexistent `cmd/devcadence/setup_test.go` corrected to the real `cmd/devcadence/cli_doctor_test.go` and `cli_setup_test.go`; the "100% field parity" / "strict twin validation" phrasing in `HANDOFF.md` and `docs/IMPLEMENTATION_PLAN.md` corrected to name `TestSchemaTopLevelFieldsMatchTheGoTwin` and state precisely what it checks (top-level properties only, per that test's own doc comment); the README `setup apply` example now shows the deterministic `--approve-plan` form instead of one that silently depended on interactive approval.
3. **This EWP was not frozen before implementation and remained marked DRAFT.** Addressed in §7 above: the deviation is named honestly rather than fixed by rewriting pushed history, this file's status line and Git history now agree, and the independent review that should have preceded implementation is recorded as the compensating control.
4. **The stated gofmt Definition of Done was not met, and evidence silently narrowed it.** §5 originally stated `gofmt -l .`; the pushed evidence instead reported `gofmt -l tests/`, without saying so. Running the exact originally-stated command reports 12 pre-existing files (`internal/compaction/pruning.go`, `internal/compaction/types.go`, `internal/events/payloads_project.go`, `internal/process/process_test.go`, `internal/protocol/ledger.go`, `internal/state/reduce_test.go`, `internal/tools/fetch.go`, `internal/tools/grep.go`, `internal/tools/symbols.go`, `internal/validation/profiles_test.go`, `internal/validation/run_test.go`, `internal/validation/supervisor_test.go`), none touched by this WP or its round-1 repair (verified against `git diff --name-only b1a12f1..c18d2f6 -- '*.go'` and this repair's own changed files). Fixed per the review's explicitly offered second option: §5's Definition of Done is amended to changed-file-scoped `gofmt -l`, with the repository-wide baseline and its rationale recorded here and in the verification evidence below, rather than silently narrowing the claim without saying so.

**Verification (this revision):** `go build ./...` (clean); `go vet ./...` (clean); `gofmt -l .` reports exactly 12 pre-existing files unrelated to this WP — none touched by `c18d2f6` or this repair: `internal/compaction/pruning.go`, `internal/compaction/types.go`, `internal/events/payloads_project.go`, `internal/process/process_test.go`, `internal/protocol/ledger.go`, `internal/state/reduce_test.go`, `internal/tools/fetch.go`, `internal/tools/grep.go`, `internal/tools/symbols.go`, `internal/validation/profiles_test.go`, `internal/validation/run_test.go`, `internal/validation/supervisor_test.go`; `gofmt -l` on every file this WP's implementation or round-1 repair actually touched (`tests/m3b_milestone_closure_test.go`, `tests/twin_fields_test.go`) is clean; `go test -count=1 ./...` (all 30 packages `ok`); `go test -race -count=1 ./...` (clean, exact command, all packages); `GOOS=windows GOARCH=amd64`/`GOOS=linux GOARCH=amd64 go build ./...` (clean); `git diff --check` (clean).

**Disposition:** all 4 FIX_NOW groups fixed; awaiting a focused re-review of the repaired head per AGENTS.md §8A before WP-M3B-8 (and M3B closure) can be marked `accepted`.
