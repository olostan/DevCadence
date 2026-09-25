# Engineering Work Package — WP-M3B-8: Verification Suite and Docs Sync

- **Author:** Valentyn Shybanov <olostan@gmail.com>
- **Date:** 2026-09-25
- **Base commit:** `b1a12f1d8b1662752faa31aa8cc0543023e5db6f` (`feat/m3b-guided-bootstrap`, includes accepted WP-M3B-1 through WP-M3B-7 checkpoints)
- **Branch:** `feat/m3b-guided-bootstrap`
- **Depends on:** WP-M3B-1 through WP-M3B-7 (all accepted).
- **Status:** DRAFT (authoring prior to milestone closure verification code and doc sync).

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
- `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test -count=1 ./...`, `go test -race ./...`, `GOOS=windows GOARCH=amd64 go build ./...`, `GOOS=linux GOARCH=amd64 go build ./...`, and `git diff --check` pass cleanly.
- `HANDOFF.md` updated with WP-M3B-8 evidence.
