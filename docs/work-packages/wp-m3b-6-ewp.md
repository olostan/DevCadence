# Engineering Work Package: WP-M3B-6 — Bounded recipes

- **Milestone:** M3B — Guided bootstrap and onboarding
- **Scope card:** [docs/WORK_PACKAGES.md#wp-m3b-6--bounded-recipes](../WORK_PACKAGES.md#wp-m3b-6--bounded-recipes)
- **Base commit:** `a8af45608a879e4c4b7f4b8eb61a4101dc4beb5f` (`feat/m3b-guided-bootstrap`, includes accepted WP-M3B-1 through WP-M3B-5 checkpoints)
- **Branch:** `feat/m3b-guided-bootstrap`
- **Depends on:** WP-M3B-1 (`SetupPlan`/`SetupAction`/`TypedOperation`, accepted), WP-M3B-2 (ledger/executor sandbox, accepted), WP-M3B-3 (runtime-neutral local model adapters, accepted), WP-M3B-4 (`CredentialRef`/`AuthEvidence`, accepted), WP-M3B-5 (Doctor readiness and ResourceInventory, accepted).
- **Status:** Authored and frozen prior to implementation code. Implemented; independent review round 1 found 4 FIX_NOW findings (Ollama pre-resolution didn't enforce an immutable digest; planner didn't bind resolver output to the requested runtime/model identity; `recipe.manual.install_rocm_driver` was unreachable from the real planning path; driver/device-permission manual recipes verified only `command_available`, not actual remediation state) — all fixed, see §10. Awaiting round-2 review.

---

## 1. Central Design Question & Answer

> **«How does DevCadence ensure that every executable setup mutation is bound to an immutable, reproducible, supply-chain-verified recipe with pre-resolved digests and explicit licenses, while strictly segregating privileged system/driver operations into non-auto-executable manual guides?»**

### The Answer

1. **Pre-Resolution Before Plan Generation:**
   `Planner` never embeds mutable tags, branch names, or unresolvable references into a plan. A dedicated `ModelResolver` interface pre-resolves a model reference (`model_ref`) into an immutable snapshot (`ResolvedModel`) carrying its exact cryptographic digest or commit hash (`resolved_revision`), exact measured byte count (`expected_size_bytes`), authorized registry source (`allowed_source`), and declared license identifier (`license_reference`).
   **Fail-Closed Invariant:** If a digest or revision cannot be resolved, plan generation fails immediately (`errs.CategoryNotFound` or `errs.CategoryInvalidArgument`). The system never plans an under-specified or unpinned action.

2. **Strict Intrinsic Policy & Authority Enforcement:**
   Every executable recipe derives its authority and effects directly from `protocol.IntrinsicPolicy(op)`. A plan validator enforces that `SetupAction.Authority` meets or exceeds `IntrinsicPolicy(op)`. Furthermore, manual actions and executable operations are strictly mutually exclusive: an executable action must have a non-nil `Operation` and nil `ManualInstructions`, while a manual action must have nil `Operation`, non-empty `ManualInstructions`, and `Authority == AuthorityHighImpactManual`.

3. **System / Kernel / Driver Operations Classified Strictly High-Impact Manual:**
   Operations modifying system drivers, kernel device permissions (e.g. `/dev/nvidiactl`, `/dev/kfd`), or host packages are strictly classified as `AuthorityHighImpactManual`. They are never auto-executable by DevCadence. When `DoctorReport` and `EnvironmentFacts` indicate missing drivers or unconfigured hardware device nodes, the planner emits structured `ManualGuide` recipes with clear verification checks (e.g., verifying `command_available: nvidia-smi` or device node availability), ensuring human operator oversight and zero privilege escalation.

4. **Complete Bounded Recipe Set:**
   A versioned recipe catalogue is materialized across all five closed operation kinds from WP-M3B-1:
   - `ensure_local_model`: `recipe.ollama.pull_model`, `recipe.mlx.download_model` (executable); `recipe.manual.pull_ollama_model`, `recipe.manual.pull_mlx_model` (manual fallback when CLI identity is untrusted but digest is pinned).
   - `create_directory`: `recipe.mkdir.state`, `recipe.mkdir.artifacts_setup`, `recipe.mkdir.tmp`.
   - `write_managed_config`: `recipe.config.default_profile`, `recipe.config.max_concurrent_sessions`, `recipe.config.log_verbosity`.
   - `remove_stale_cache`: `recipe.cache.remove.machine_profile`, `recipe.cache.remove.endpoint_probes`.
   - `run_diagnostic_check`: `recipe.diagnostic.git_available`, `recipe.diagnostic.state_root_writable`, `recipe.diagnostic.ollama_responding`, `recipe.diagnostic.mlx_importable`.
   - Driver & System Manual Recipes: `recipe.manual.install_nvidia_driver`, `recipe.manual.configure_nvidia_device_permissions`, `recipe.manual.install_rocm_driver`, `recipe.manual.configure_amdgpu_device_permissions`, `recipe.manual.install_git`, `recipe.manual.reauthenticate`.

---

## 2. Brownfield Realignment: Classification of Pre-existing Code

The pre-check recorded in `HANDOFF.md` at commit `a8af456` identified existing recipe generation in `internal/setup/planner.go`. In accordance with AGENTS.md §6, pre-existing code is classified as follows:

| Component / Symbol | Classification | Rationale & Architectural Disposition |
|---|---|---|
| `DefaultRecipeSetVersion` | **KEEP** | Standard version string for generated recipes (`"1.0.0"`). |
| `DefaultOllama*` / `DefaultMLX*` constants | **KEEP & ADAPT** | Move into `CatalogModelResolver` as standard catalog entries for default verified models; no longer direct inline dependencies of `Plan`. |
| `isImmutableHFRevision` / `hfCommitHashPattern` | **KEEP** | Retained and enforced by pre-resolution and action generation to reject mutable Hugging Face branches/tags. |
| `Planner.Plan` directory actions (`recipe.mkdir.*`) | **KEEP** | Properly derives policy from `IntrinsicPolicy(op)` and verifies `managed_dir_exists`. |
| `Planner.Plan` git manual action (`recipe.manual.install_git`) | **KEEP** | Strictly manual with `AuthorityHighImpactManual`. |
| `Planner.Plan` reauthenticate manual action (`recipe.manual.reauthenticate`) | **KEEP** | Strictly manual with `AuthorityHighImpactManual` and explicit credential binding. |
| `Planner.Plan` config action (`recipe.config.default_profile`) | **KEEP** | Correctly derives policy from `IntrinsicPolicy(op)`. |
| `localModelRecipe` / `ensureLocalModelAction` | **ADAPT** | Adapt to consume `ResolvedModel` emitted by `ModelResolver` instead of static constants. Enforce that missing or unresolvable digest fails plan generation rather than emitting an unpinned action. |
| `PlannerOptions` | **ADAPT** | Add `ModelResolver` interface (defaulting to `NewCatalogModelResolver()`). |
| `Planner.Plan` model pull generation | **ADAPT** | Execute pre-resolution step via `ModelResolver.ResolveModel` prior to action construction. If resolution fails, halt plan generation with an error. |
| System/Driver operations | **ADD** | Implement deterministic generation of `recipe.manual.install_nvidia_driver`, `recipe.manual.configure_nvidia_device_permissions`, `recipe.manual.install_rocm_driver`, `recipe.manual.configure_amdgpu_device_permissions` based on `environment.AssessBackends`. |
| Cache eviction operations | **ADD** | Implement `recipe.cache.remove.machine_profile` and `recipe.cache.remove.endpoint_probes`. |
| Diagnostic check operations | **ADD** | Implement `recipe.diagnostic.*` action builders with `AuthorityReadOnly`. |

---

## 3. Bounded Recipe Catalogue

Every recipe in the catalogue is versioned (`recipe_version`), carries an immutable `recipe_id`, and declares minimum authority matching or exceeding its intrinsic policy.

### 3.1 Executable Recipes

| Recipe ID | Operation Kind | Minimum Authority | Effects | Preconditions | Postconditions | Supply-Chain Metadata |
|---|---|---|---|---|---|---|
| `recipe.ollama.pull_model` | `ensure_local_model` | `user_confirmation` | `network_access`, `model_download`, `filesystem_write` | `command_available: ollama`, `executable_verified`, `port_listening: 11434` | `model_present` (exact digest & size) | `AllowedSource`: `registry.ollama.ai`<br>`ResolvedRevision`: `sha256:...`<br>`ExpectedSizeBytes`: exact bytes<br>`LicenseReference`: e.g. `Apache-2.0` |
| `recipe.mlx.download_model` | `ensure_local_model` | `user_confirmation` | `network_access`, `model_download`, `filesystem_write` | `command_available: hf`, `executable_verified` (version subcommand) | `model_present` (exact commit & size) | `AllowedSource`: `huggingface.co`<br>`ResolvedRevision`: 40-hex commit hash<br>`ExpectedSizeBytes`: exact bytes<br>`LicenseReference`: e.g. `Apache-2.0` |
| `recipe.mkdir.<location>` | `create_directory` | `user_confirmation` | `filesystem_write` | none | `managed_dir_exists` (mode `0700`) | N/A (local filesystem) |
| `recipe.config.<key>` | `write_managed_config` | `user_confirmation` | `filesystem_write` | `managed_dir_exists: state` | `managed_dir_exists: state` | N/A (local configuration) |
| `recipe.cache.remove.<target>` | `remove_stale_cache` | `user_confirmation` | `filesystem_write` | `managed_dir_exists: state` | none | N/A (local cache eviction) |
| `recipe.diagnostic.<check>` | `run_diagnostic_check` | `read_only` | `network_access` (or none) | none | none | N/A (read-only diagnostic) |

### 3.2 High-Impact Manual Recipes (Strictly Non-Executable)

| Recipe ID | Domain | Minimum Authority | Effects | Manual Guide Summary | Verification Conditions |
|---|---|---|---|---|---|
| `recipe.manual.install_nvidia_driver` | GPU Driver | `high_impact_manual` | `privilege_elevation`, `device_permission_change`, `package_download` | Instructions to install vendor NVIDIA proprietary driver package | `command_available: nvidia-smi` |
| `recipe.manual.configure_nvidia_device_permissions` | Kernel Device Access | `high_impact_manual` | `privilege_elevation`, `device_permission_change` | Instructions to add user to `video`/`render` group and configure udev | `command_available: nvidia-smi` |
| `recipe.manual.install_rocm_driver` | GPU Driver | `high_impact_manual` | `privilege_elevation`, `device_permission_change`, `package_download` | Instructions to install AMD ROCm driver and compute stack | `command_available: rocminfo` |
| `recipe.manual.configure_amdgpu_device_permissions` | Kernel Device Access | `high_impact_manual` | `privilege_elevation`, `device_permission_change` | Instructions to configure `/dev/kfd` permissions | `command_available: rocminfo` |
| `recipe.manual.install_git` | Core Dependency | `high_impact_manual` | `package_download`, `filesystem_write` | System package manager instructions to install Git | `command_available: git` |
| `recipe.manual.reauthenticate` | Authentication | `high_impact_manual` | `authentication` | CLI login / API key configuration instructions | `endpoint_authenticated` |
| `recipe.manual.pull_ollama_model` | Local Model | `high_impact_manual` | `package_download`, `filesystem_write` | Manual `ollama pull` instructions when local binary identity is unverified | `model_present` (exact digest & size) |
| `recipe.manual.pull_mlx_model` | Local Model | `high_impact_manual` | `package_download`, `filesystem_write` | Manual `hf download` instructions when Python/hf environment is unverified | `model_present` (exact commit & size) |

---

## 4. Model Pre-Resolution Architecture

### 4.1 Interface Contract

```go
// ModelResolver resolves a runtime model reference into an immutable supply-chain record
// prior to plan generation.
type ModelResolver interface {
    ResolveModel(ctx context.Context, runtime, modelRef string) (ResolvedModel, error)
}

// ResolvedModel contains verified supply-chain metadata for an install-class model operation.
type ResolvedModel struct {
    Runtime           string `json:"runtime"`
    ModelRef          string `json:"model_ref"`
    ResolvedRevision  string `json:"resolved_revision"`
    ExpectedSizeBytes int64  `json:"expected_size_bytes"`
    AllowedSource     string `json:"allowed_source"`
    LicenseReference  string `json:"license_reference"`
}
```

### 4.2 Invariant Validation Rules for `ResolvedModel`
- `Runtime`: non-empty string identifying registered runtime.
- `ModelRef`: non-empty string.
- `ResolvedRevision`: non-empty immutable revision:
  - For Hugging Face / MLX: MUST match `^[0-9a-f]{40}$` (full 40-character commit hash). Mutable branches (e.g. `main`) or tags are rejected.
  - For Ollama: MUST match `^sha256:[a-f0-9]{64}$` or adapter-verified immutable manifest digest.
- `ExpectedSizeBytes`: strictly positive (`> 0`).
- `AllowedSource`: non-empty domain or registry host (e.g. `registry.ollama.ai`, `huggingface.co`).
- `LicenseReference`: non-empty license identifier (e.g. `Apache-2.0`).

### 4.3 Fail-Closed Plan Generation
When `Planner.Plan` evaluates an `ensure_local_model` action:
1. `p.modelResolver.ResolveModel(ctx, runtime, modelRef)` is invoked.
2. If resolution returns an error, empty digest, invalid size, unpinned revision, or empty license:
   **The planner halts immediately and returns an error** (`errs.CategoryNotFound` or `errs.CategoryInvalidArgument`).
3. An action is **NEVER** created without a valid `ResolvedModel`.
4. Only once `ResolvedModel` is valid does the planner check whether local CLI identity is trustworthy:
   - If trustworthy (`executablePath != ""` and `executableVersion != ""` and machine fingerprint matches): emits executable action (`recipe.ollama.pull_model` or `recipe.mlx.download_model`).
   - If untrustworthy: emits manual action (`recipe.manual.pull_ollama_model` or `recipe.manual.pull_mlx_model`), binding the exact pre-resolved revision and size in the `model_present` postcondition.

---

## 5. System, Kernel, and Driver Operations Governance

DevCadence strictly adheres to the principle of least privilege and controlled process execution (ADR-0008, ADR-0014 §7).

1. **Zero Automated Kernel / Driver Mutation:**
   DevCadence never executes `sudo`, `modprobe`, `apt-get install nvidia-driver`, `usermod -aG render`, or equivalent commands. Any operation touching system drivers, kernel modules, device node permissions, or root-owned package installations is classified `AuthorityHighImpactManual`.
2. **Deterministic Triggering via `environment.AssessBackends`:**
   During planning (`TargetHardware` or `TargetAll`), `Planner` inspects accelerator candidates from `environment.AssessBackends(*p.facts)`:
   - If `candidate.Backend == protocol.BackendCUDA`:
     - If `RequiredSoftware` contains `"nvidia-driver"`: plans `recipe.manual.install_nvidia_driver`.
     - If `RequiredSoftware` contains `"nvidia-driver-device-access"`: plans `recipe.manual.configure_nvidia_device_permissions`.
   - If `candidate.Backend == protocol.BackendROCm`:
     - If `RequiredSoftware` contains `"amdgpu-driver"`: plans `recipe.manual.install_rocm_driver`.
     - If `RequiredSoftware` contains `"amdkfd-device-access"`: plans `recipe.manual.configure_amdgpu_device_permissions`.
3. **Execution Guard:**
   `SetupAction.Validate()` structurally forbids any `AuthorityHighImpactManual` action from having `Operation != nil`. The executor will refuse to execute any manual action.

---

## 6. IntrinsicPolicy and Authority Enforcement

In accordance with WP-M3B-1 and ADR-0014 §1:
- `IntrinsicPolicy(op)` defines the lower bound for authority and required effect categories:
  - `ensure_local_model`: `AuthorityUserConfirmation`, `[filesystem_write, model_download, network_access]`
  - `create_directory`: `AuthorityUserConfirmation`, `[filesystem_write]`
  - `write_managed_config`: `AuthorityUserConfirmation`, `[filesystem_write]`
  - `remove_stale_cache`: `AuthorityUserConfirmation`, `[filesystem_write]`
  - `run_diagnostic_check`: `AuthorityReadOnly`, `[network_access]`
- In `SetupAction.Validate()`:
  - If `a.Authority.Rank() < minAuth.Rank()`: fails validation with `CategoryInvalidArgument`.
  - If required effects are missing from `a.Effects`: fails validation with `CategoryInvalidArgument`.
- In `SetupPlan.Validate()`:
  - `RequiredAuthority` must equal the maximum rank across all actions.
  - `TotalEffects` must contain the union of all action effects, deterministically sorted.

---

## 7. Security & Threat Model

| Threat Vector | Mitigation |
|---|---|
| 1. Under-specified or mutable model pull | Pre-resolution requires immutable commit hash or sha256 digest before planning. Plans with unresolvable digests fail generation closed. |
| 2. Unlicensed model ingestion | Every install-class operation requires non-empty `LicenseReference` at pre-resolution and plan validation time. |
| 3. Automated privilege escalation via drivers | Driver/kernel operations are classified strictly `AuthorityHighImpactManual`. `SetupAction.Validate` rejects any non-nil `Operation` on manual actions. |
| 4. Supply-chain registry confusion | `AllowedSource` is validated and enforced by runtime adapters during both planning and execution. |
| 5. Postcondition mismatch / bait-and-switch | `SetupAction.Validate()` structurally requires `model_present` postcondition to match `EnsureLocalModelParams` in runtime, model_ref, resolved_revision, and expected_size_bytes. |

---

## 8. Verification Matrix (20 Scenarios)

1. **Pre-resolution of Ollama model tag:** Resolves default model tag to immutable `sha256:...` digest, exact byte size, source, and license.
2. **Pre-resolution of MLX model ref:** Resolves MLX repo to 40-character commit hash, exact byte size, source, and license.
3. **Fail-closed on unresolvable model tag:** Planner fails plan generation when resolver returns error for unknown model.
4. **Fail-closed on empty digest/revision:** Planner fails plan generation when model has empty revision.
5. **Fail-closed on mutable MLX revision:** Planner fails plan generation when MLX model resolver returns mutable branch (e.g., `main`).
6. **Fail-closed on missing license metadata:** Planner fails plan generation when resolved model lacks license metadata.
7. **Fallback to manual model pull on untrusted CLI identity:** Emits `recipe.manual.pull_ollama_model` or `recipe.manual.pull_mlx_model` with bound resolved revision in postcondition.
8. **Automated model pull on verified CLI identity:** Emits `recipe.ollama.pull_model` or `recipe.mlx.download_model` with `Operation != nil`.
9. **Hardware driver remediation (NVIDIA missing driver):** Emits `recipe.manual.install_nvidia_driver` with `AuthorityHighImpactManual`.
10. **Hardware device node remediation (NVIDIA device access):** Emits `recipe.manual.configure_nvidia_device_permissions` with `AuthorityHighImpactManual`.
11. **Hardware driver remediation (AMD ROCm missing driver):** Emits `recipe.manual.install_rocm_driver` with `AuthorityHighImpactManual`.
12. **Hardware device node remediation (AMD device access):** Emits `recipe.manual.configure_amdgpu_device_permissions` with `AuthorityHighImpactManual`.
13. **Cache removal recipe generation:** Emits `recipe.cache.remove.machine_profile` with `AuthorityUserConfirmation` and `MutationCacheRemoved`.
14. **Diagnostic check recipe generation:** Emits `recipe.diagnostic.state_root_writable` with `AuthorityReadOnly`.
15. **Directory creation recipe generation:** Emits `recipe.mkdir.*` with `AuthorityUserConfirmation` and `managed_dir_exists` postcondition.
16. **Config write recipe generation:** Emits `recipe.config.default_profile` with `AuthorityUserConfirmation`.
17. **Manual Git install recipe generation:** Emits `recipe.manual.install_git` with `AuthorityHighImpactManual`.
18. **Manual re-authenticate recipe generation:** Emits `recipe.manual.reauthenticate` with `AuthorityHighImpactManual`.
19. **Executable declared authority matches IntrinsicPolicy:** Proves every executable recipe passes `SetupAction.Validate()` intrinsic policy checks.
20. **Deterministic plan digest and serialization:** Generated plans round-trip through JSON schema and cross-compile on Linux and Windows amd64.

---

## 9. Acceptance Criteria

- Pre-resolution interface `ModelResolver` implemented with default catalog and fail-closed planning.
- All five `TypedOperation` kinds have concrete versioned recipes.
- System/kernel/driver operations formalized as strictly `AuthorityHighImpactManual`.
- Every executable recipe matches or exceeds `IntrinsicPolicy`.
- Unresolvable digests fail plan generation.
- License metadata present on all install-class operations.
- Full verification suite clean: `go test -count=1 ./...`, race tests, Windows cross-build, `git diff --check`.

## 10. Independent review round 1 disposition: 4 FIX_NOW findings, all fixed

An independent review ([PR #10 comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5834040833), owner, 2026-09-25, base `a8af456`, head `88fee78`) confirmed the overall recipe/resolver architecture was viable (no unrestricted redesign requested) but found 4 closure-threshold FIX_NOW contract/invariant failures, all fixed:

1. **Ollama pre-resolution did not enforce an immutable digest.** `ResolvedModel.Validate()` only applied an immutable-revision format check for `Runtime == "mlx"`; Ollama accepted any non-empty `ResolvedRevision`, including a mutable tag like `"main"`. Fixed: `Validate()` now dispatches per-runtime (`switch m.Runtime`) — MLX still requires `isImmutableHFRevision` (a 40-hex commit hash), Ollama now requires new `isImmutableOllamaRevision` (`sha256:<64 lowercase hex>`, matching `DefaultOllamaDigest`'s own shape), and any runtime this package doesn't yet know how to verify fails closed rather than being accepted as "non-empty is enough." `localModelRecipe`'s Ollama branch also now sets `revisionIsImmutable: isImmutableOllamaRevision` as defense in depth alongside the resolution-time check. New test: `TestRecipe_FailClosedOnMutableOllamaRevision`.
2. **The planner did not bind resolver output to the requested runtime/model identity.** After `resolved.Validate()`, the planner built the action straight from `resolved.Runtime`/`resolved.ModelRef` with no check they matched the query — a buggy or malicious `ModelResolver` implementation could return a valid, immutable record for a *different* runtime/model and the plan would silently target it. Fixed: new `verifyResolvedIdentity(resolved, wantRuntime, wantModelRef) error` in `resolver.go`, called at both the Ollama and MLX resolution sites immediately after `resolved.Validate()`; a mismatch fails plan generation. New tests: `TestRecipe_FailClosedOnResolverRuntimeMismatch`, `TestRecipe_FailClosedOnResolverModelRefMismatch`.
3. **`recipe.manual.install_rocm_driver` was unreachable from the real deterministic planning path, and scenario 11 gave false coverage.** `environment.AssessBackends`'s AMD/ROCm assessment never emitted `"amdgpu-driver"` (the string `planner.go` requires to route to `install_rocm_driver`) — it only distinguished "architecture unknown" from "`/dev/kfd` inaccessible," with no signal for "no amdgpu kernel driver bound at all." Scenario 11 was labeled "AMD ROCm missing driver" but its fixture (no `DriverInUse` set) actually exercised the device-access path and asserted `configure_amdgpu_device_permissions`, masking the gap. Fixed: `amdCandidates` now checks `device.DriverInUse == "amdgpu"` first (mirroring `nvidiaCandidate`'s own `driverBound` pattern exactly) — driver not bound emits `"amdgpu-driver"`; driver bound but `/dev/kfd` unusable emits `"amdkfd-device-access"`, the same distinction NVIDIA's path already made. Scenario 11 now actually asserts `install_rocm_driver` (its fixture, driver unbound, now correctly routes there); new `TestRecipe_Scenario11b_HardwareDriverRemediationAmdgpuDeviceAccessViaAssessBackends` exercises the device-access case through the real `AssessBackends -> Planner` path (not `NewManualAmdgpuDevicePermissionsAction` called directly, which scenario 12 already covered without testing routing).
4. **Driver/device-permission manual recipes verified only `command_available`, which proves neither driver binding nor device accessibility.** All four hardware manual recipes (`install_nvidia_driver`, `configure_nvidia_device_permissions`, `install_rocm_driver`, `configure_amdgpu_device_permissions`) used `command_available: nvidia-smi`/`rocminfo` as their verification/postcondition — a vendor CLI binary being on PATH says nothing about whether a kernel driver is bound or a device node is user-accessible. Fixed with a new closed, read-only, typed condition: `CondKindDeviceNodeAccessible` (`protocol.DeviceNodeOperand{Path, RequireAccessible}`) checks a device special file's actual state — `os.Stat` for existence, and (when `RequireAccessible`) an immediately-closed `os.OpenFile(path, O_RDWR, 0)` probe for real current-user read/write access, never trusting raw permission-bit inspection alone. The two "install driver" recipes now verify node *existence* at the canonical path (`/dev/nvidiactl`, `/dev/kfd` — the same paths `environment/compatibility.go` already checks); the two "configure permissions" recipes now verify the same path with `RequireAccessible: true`. New `TestEvaluateDeviceNodeAccessible` covers missing/existing-but-inaccessible/existing-and-accessible, including the required pre-remediation-fails/corrected-state-passes transition (root-aware: skips the permission-denial subtest when running as root, since permission bits cannot deny access to that user).

**Contract-drift cleanup also addressed** (non-blocking, but the review asked to either restore the frozen contract or explicitly reconcile): `NewWriteManagedConfigAction`/`NewRemoveStaleCacheAction` now carry the `managed_dir_exists` precondition the frozen EWP §3.1 always described; this is safe because `planner.go`'s `dirActionIDs` threading already orders these actions after state-directory creation via `DependsOn`, so the precondition is always satisfiable by the time either action runs.

**Verification (this revision):** `go build ./...`, `go vet ./...`, `go test -count=1 ./...` (all 30 packages `ok`), `go test -race ./internal/setup/... ./internal/protocol/... ./internal/credentials/... ./internal/schema/... ./internal/environment/... ./tests/...` (clean), `GOOS=windows GOARCH=amd64`/`GOOS=linux GOARCH=amd64 go build ./...` (clean), `git diff --check` (clean).

**Disposition:** all 4 FIX_NOW findings fixed, plus the contract-drift cleanup; awaiting a focused re-review of the repaired head per AGENTS.md §8A before WP-M3B-6 can be marked `accepted`.
