# ADR-0014: Guided bootstrap, setup plans, operational event ledger, and readiness contracts

- **Status:** Accepted
- **Date:** 2026-09-22
- **Related:** ADR-0008 (controlled process execution), ADR-0011 (adaptive environment and host-independent cognition), ADR-0013 (environment intelligence and cognition capability contracts), DCI-033, DCI-081, DCI-084, DCI-104, DCI-105, DCI-106, DCI-108
- **Documents:** docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md, docs/SETUP.md, docs/MODEL_RUNTIME.md, docs/SECURITY.md, docs/INVARIANTS.md

## Context

ADR-0011 and ADR-0013 established passive environment discovery, pure-function candidate assessment, multi-source cognition endpoint abstractions, empirical acceleration verification, and capability-based routing. Crucially, M3A remained completely read-only: it discovered facts and evaluated routing without mutating the host machine, installing packages, downloading weights, or creating credentials.

Milestone M3B transitions from passive discovery to **active guidance and remediation**: diagnostic evaluation (`devcadence doctor`), deployment profile recommendations, and remediation execution (`devcadence setup`).

Implementing M3B requires settling durable operational contracts so that mutation remains auditable, deterministic, crash-safe, and strictly governed by explicit operator consent:

1. **How mutations are represented.** Free-form shell scripts or open-ended parameter dictionaries (`map[string]string`) turn setup into an unverified script runner, violating ADR-0008 and DCI-033.
2. **How operator consent is bound to execution.** An un-hashed plan or a plan that silently adapts to runtime drift violates DCI-108.
3. **How execution state survives interruption.** A process killed mid-step must not leave the machine in an unknown state or blindly re-execute non-idempotent operations.
4. **Where operational state lives.** Machine-wide software installation, hardware profiling, operational ledgers, and locks must not pollute Git project state or project journals.
5. **How readiness is evaluated.** Treating hardware as proof of capability, or computing readiness without an evaluated target profile, produces misleading assertions.
6. **How credentials and authentication are handled.** Storing raw secrets or inferring login status from CLI version output creates critical security vulnerabilities and false positives.

## Decision

### 1. Closed Discriminated-Union Operations and Conditions

Setup operations and conditions are closed, typed protocols, not open-ended key-value bags:
- `TypedOperation` is a discriminated union of supported operations (`ollama_pull_model`, `create_directory`, `write_managed_config`, `remove_stale_cache`, `run_diagnostic_check`).
- Operation parameters are strictly bounded: directory creation is restricted to allowlisted locations (`ManagedDirectoryLocation`), cache removal to allowlisted targets (`CacheTarget`), and configuration mutation to allowlisted keys (`ManagedConfigKey`) and non-secret typed values.
- `Condition` is a discriminated union of closed condition kinds (`command_available`, `executable_verified`, `managed_dir_exists`, `port_listening`, `endpoint_healthy`, `model_digest_present`).
- `CondExecutableVerified` verifies the canonical binary path, expected version, and digest before an action executes, preventing PATH substitution attacks.
- **Intrinsic Policy:** The executor—not the recipe—defines the intrinsic minimum authority and effect categories for each operation kind (`IntrinsicPolicy(op)`). Plan validation rejects any action whose declared authority or effects are weaker than the executor's intrinsic policy.
- **Mutual Exclusion:** Manual actions (`Operation == nil`, non-empty `ManualInstructions`, read-only typed verification conditions) and executable actions (`Operation != nil`, no manual guide) are mutually exclusive.

### 2. Immutable SetupPlan Envelope and Canonical PlanDigest

- A setup plan is an immutable envelope:
  ```go
  type SetupPlan struct {
      SchemaVersion      SchemaVersion      `json:"schema_version"`
      PlanID             string             `json:"plan_id"`
      PlanDigest         string             `json:"plan_digest"`
      RecipeSetVersion   string             `json:"recipe_set_version"`
      MachineFingerprint string             `json:"machine_fingerprint"`
      CreatedAt          Timestamp          `json:"created_at"`
      Target             string             `json:"target"`
      Actions            []SetupAction      `json:"actions"`
      RequiredAuthority  Authority          `json:"required_authority"`
      TotalEffects       []EffectCategory   `json:"total_effects"`
  }
  ```
- **PlanDigest Rule:** `PlanDigest` is the SHA-256 digest of the canonical JSON encoding of the complete saved `SetupPlan`, with only the `plan_digest` field omitted.
- **Two-Step CLI Approval Workflow:**
  ```bash
  devcadence setup plan [target] --output <file>
  devcadence setup apply --plan <file> --approve-plan <sha256:digest> [--yes]
  ```
- Non-interactive execution strictly requires a schema-valid plan file and the exact matching `--approve-plan <digest>`.
- `--yes` authorizes only `user_confirmation` actions and strictly rejects privileged or high-impact actions.
- Preconditions are rechecked before executing each action. Any drift between plan generation and execution invalidates approval, halting execution and demanding a fresh plan.

### 3. Crash-Safe Setup Event Ledger

- Setup execution history is recorded in an append-only JSONL event ledger at `$DEVCADENCE_HOME/state/setup-ledger.jsonl`.
- Events carry a closed discriminated union payload (`ExecutionCreatedPayload`, `PlanApprovedPayload`, `ActionStartingPayload`, `ActionProcessCompletedPayload`, `PostconditionVerifiedPayload`, `ActionTerminatedPayload`, `ExecutionFinishedPayload`). Arbitrary or raw JSON strings are forbidden.
- **Hash-Chain Integrity:** Each event records `sequence`, `previous_event_digest`, and its own `event_digest` (SHA-256 over canonical JSON with only `event_digest` omitted). Corruption or checksum mismatch prior to the final line fails closed. Only an incomplete final append may be recovered as a torn write.
- **Interruption Recovery:** `EventActionStarting` is flushed and `fsync`'d before `CommandRunner.Run` is called. On restart, any action with `ActionStarting` but no terminal event is marked `ActionStatusInterrupted`. Its postconditions are evaluated:
  - If postconditions pass: action is marked `succeeded`.
  - If postconditions fail: action is marked `blocked`. The action is **never** blindly rerun.
- `SetupExecutionReport` is a derived projection of the event ledger, not the primary ledger record.

### 4. Operational State vs Project Canonical State

- Machine-global operational state lives under `$DEVCADENCE_HOME` (default `~/.devcadence`):
  - `$DEVCADENCE_HOME/state/machine-profile.json`: Cached `MachineCapabilityProfile`.
  - `$DEVCADENCE_HOME/state/setup-ledger.jsonl`: Append-only execution ledger.
  - `$DEVCADENCE_HOME/state/setup.lock`: Exclusive OS `flock` guarding mutating runs.
  - `$DEVCADENCE_HOME/artifacts/setup/`: Output logs (mode `0700` dirs, `0600` files).
  - `$DEVCADENCE_HOME/tmp/`: Private, owner-only temporary directory (mode `0700`).
- None of this operational state enters Git repositories, commits, or project event journals.
- **Cache Freshness:** Cache hit requires an `inventory` probe to compare the live `MachineFingerprint`. `doctor` always refreshes volatile health. Cached acceleration evidence carries a verification timestamp and is treated as stale if expired. Atomic write + fsync + rename protects cache files.

### 5. Scope-Bound Readiness and Pure-Function Recommendations

- `DoctorReport` includes an explicit `ReadinessEvaluationScope` identifying the target deployment profile, required roles, and evidence freshness.
- **Normative Readiness States:**
  - `READY`: Every mandatory capability for the evaluated target profile is currently satisfied by live, verified endpoints.
  - `READY_WITH_REDUCED_CAPABILITY`: Every mandatory capability is satisfied, but optional capabilities (e.g. local coding acceleration or consultants) are unconfigured.
  - `PARTIALLY_READY`: Deterministic control plane is operational, but at least one mandatory capability for the target is absent or no target profile can be selected.
  - `ACTION_REQUIRED`: A mandatory base dependency (Git, write permissions on state root, disk space) prevents operation.
- **M3B boundary:** Doctor computes deterministic readiness and a ResourceInventory from verified facts. Human-readable deployment labels may summarize the environment, but M3B does not solve optimal role/provider/budget allocation with a static pure-function selector.
- **Adaptive portfolio synthesis:** AI-assisted CognitionPortfolio recommendation, economic/budget modeling and task workflow-topology synthesis are governed by ADR-0018/M3C. Any recommendation remains advisory until deterministic policy validation succeeds.

### 6. Opaque Credential References and Secret Isolation

- Configuration references credentials via opaque locators (`CredentialRef` with kinds `env_var`, `cli_session`, `keychain_ref`).
- DevCadence holds no secret custody; managed raw-secret file stores are excluded.
- Environment lookup uses `os.LookupEnv` for presence only; secret values and lengths are never logged.
- CLI version output (`claude --version`) establishes only software installation, never authentication.
- Secrets are strictly forbidden from `process.Spec.Args` and `process.Spec.Env`.

### 7. Bounded Supply-Chain Recipes and Output Artifacts

- System driver/kernel/permission modifications are strictly `AuthorityHighImpactManual`.
- Executable operations are user-level only with declared registries, immutable digests, sizes, and licenses.
- Subprocess execution routes exclusively through `internal/process.Runner` (satisfying the narrow `internal/setup.CommandRunner` interface).
- Output capture is bounded (`4 MiB`), stripped of ANSI control characters, and stored using content-addressed artifacts. Authentication operations capture zero raw output artifacts.
- `doctor --fix` is strictly a convenience shortcut for generating a `SetupPlan` file; it never executes or approves mutations.

## Consequences

### Positive
- Subprocess execution and mutations remain strictly controlled, auditable, and bounded.
- The approval workflow prevents accidental or automated privilege escalation.
- Setup execution is crash-safe and resumes deterministically after unexpected termination.
- Operational machine state remains cleanly separated from Git project history.
- Readiness and ResourceInventory are grounded in verified capability, not hardware heuristics; later AI portfolio recommendations consume that verified substrate.
- Public CLI JSON interfaces are governed by draft 2020-12 schemas with full fixture validation.

### Costs
- Setup actions require explicit Go types, validation functions, and JSON Schema definitions for every new operation and condition.
- Ledger replay and hash-chain verification add operational complexity to the executor.
- Requiring resolved model digests adds pre-planning resolution logic for package/model downloads.
