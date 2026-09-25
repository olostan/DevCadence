# Engineering Work Package: WP-M3B-5 — Doctor readiness and ResourceInventory (service layer, no public CLI)

- **Milestone:** M3B — Guided bootstrap and onboarding
- **Scope card:** [docs/WORK_PACKAGES.md#wp-m3b-5--doctor-readiness-and-resource-inventory-service-layer-no-public-cli](../WORK_PACKAGES.md#wp-m3b-5--doctor-readiness-and-resource-inventory-service-layer-no-public-cli)
- **Base commit:** `e2e0849` (`feat/m3b-guided-bootstrap`, includes accepted WP-M3B-1 through WP-M3B-4 checkpoints)
- **Branch:** `feat/m3b-guided-bootstrap`
- **Depends on:** WP-M3B-1 (`SetupPlan`/`SetupAction`/`TypedOperation`, accepted), WP-M3B-2 (ledger/home layout, accepted), WP-M3B-3 (executor, accepted), WP-M3B-4 (`CredentialRef`/`AuthEvidence`/`Manager`, accepted).
- **Status:** Implementation complete; independent review round 1 found 6 substantive blockers (static profiles still authoritative in the real Doctor/Planner path, a too-lossy ResourceInventory with no machine-profile provenance link, BuildResourceInventory re-probing live state (TOCTOU) plus no ordering normalization, Go/schema parity gaps including a forked weaker nested credential schema, an auth-blind viability check, and a re-auth action postcondition that endpoint health could satisfy without real authentication) — all fixed, see §14. Independent review round 2 found 4 further blockers (MachineProfileRef not durably retrievable, auth-aware viability not propagated into discovery diagnostics plus a locality-based auth bypass, remaining Go/schema/report parity holes, `endpoint_authenticated` not operationally wired) — all fixed, see §15. Awaiting round-3 review.

---

## 1. Central Design Question & Answer

> **«What is the smallest deterministic ResourceInventory/readiness contract that captures enough factual machine/cognition state for future M3C/M3D planning without prematurely encoding portfolio, economic, routing, or workflow decisions?»**

### The Answer
The smallest contract consists of two distinct protocol shapes:
1. **`ResourceInventory` (Machine-oriented factual substrate):** A top-level durable protocol `Record` (`schema_version: "1.0"`) that aggregates discovered, verified observations without prescribing their use:
   - `hardware`: OS, architecture, logical cores, total memory bytes, and supported accelerator backends (`HardwareSummary`).
   - `cognition_endpoints`: Discovered endpoints with factual health, authentication status, coarse cost class, source exposure, and acceleration verification (`[]CognitionEndpointSummary`).
   - `principal_hosts`: Host environments verified on the machine (`[]PrincipalHostSummary`).
   - `credentials`: Configured references paired with verified authentication evidence (`[]CredentialInventoryEntry`).
   - `readiness`: Scope-specific readiness projections (`[]ScopeReadiness`) across concrete operational and cognition capabilities (`ready`, `not_ready`, `unavailable`, `unknown`).
   - `policy`: Available routing constraints (`*PolicySummary`).
   `ResourceInventory` contains no scoring, weighting, model preference, role assignment, or budget optimization.

2. **`DoctorReport` (Operator-oriented diagnostic & readiness projection):** A top-level durable protocol `Record` providing actionable human and operational evaluation:
   - `readiness`: Composite operational health (`READY`, `READY_WITH_REDUCED_CAPABILITY`, `PARTIALLY_READY`, `ACTION_REQUIRED`) derived strictly from base dependencies and viable cognition availability—**completely decoupled from any mandatory deployment profile**.
   - `scope_readiness`: Granular capability evaluations matching `ResourceInventory`.
   - `findings`: Concrete diagnostic issues (`DiagnosticFinding`) with remediations.
   - `resource_inventory`: The full machine inventory projection embedded for operator visibility.
   - `recommended_profile`: Retained solely as an optional, descriptive UX label summarizing hardware facts; **it exercises zero authority over canonical readiness or planning**.

---

## 2. Brownfield Realignment: Classification of Pre-existing Code

Per the WP-M3B-5 mandate, pre-existing code from before the PR #12 roadmap rebaseline is explicitly classified:

| Component / Symbol | Classification | Rationale & Architectural Disposition |
|---|---|---|
| `Doctor.checkStateRoot` | **KEEP** | Pure deterministic inspection of `$DEVCADENCE_HOME` writability and directories. |
| `Doctor.checkGit` | **KEEP** | Verifies Git presence and version facts without heuristics. |
| `Doctor.checkHardware` | **KEEP** | Evaluates CPU, memory, and accelerator facts via `environment.AssessBackends`. |
| `Doctor.checkPrincipalHosts` | **KEEP** | Inspects supported host installations (`antigravity`, `cursor`, `vscode`). |
| `Doctor.discoverEndpoints` | **KEEP & ADAPT** | Discovers local runtimes, models, and endpoints; adapted to feed `ResourceInventory`. |
| `Doctor.evaluateReadiness` | **ADAPT** | Removed mandatory dependency on `TargetProfile`/`SelectedProfile`. Readiness is now evaluated from verified base dependencies, viable cognition presence, and scope-specific projections. |
| `DoctorReport.Validate` | **ADAPT** | Removed requirement that `READY` demands a non-nil `TargetProfile`. |
| `Doctor.BuildResourceInventory` | **KEEP & EXPAND** | Pure projection building `ResourceInventory`, expanded with `ScopeReadiness` and credential integration. |
| `ProfileRecommender.Recommend` | **DEPRECATE** | De-authorized from all canonical readiness, routing, and portfolio authority. Retained strictly as an informational UX label generator. |
| `DeploymentProfile` / `SelectedProfile` | **DEPRECATE** | Deprecated as canonical configuration or routing gate. No longer required for `DoctorReport.Readiness`. |
| `ProfileAlternative` | **DEPRECATE** | Informational UX description only; not a portfolio decision engine. |
| `Planner.Plan` finding handlers | **KEEP** | Handles `FindingCodeStateDirsMissing`, `FindingCodeGitNotFound`, and `FindingCodeAuthExpired`. |
| `Planner.Plan` profile model pulling | **ADAPT** | Model-pull actions triggered by explicit target scope or concrete missing local model, without requiring static profile authority. |
| `EconomicRegime` / `BudgetPool` / `BudgetState` | **DEFER TO M3C** | M3C owns the deterministic economic and session substrate. |
| `CognitionPortfolio` / `PortfolioPlanner` / `WorkflowPlan` | **DEFER TO M3D** | M3D owns AI-assisted portfolio and workflow synthesis. |

---

## 3. Canonical Architecture Boundary (M3B vs M3C vs M3D)

- **M3B (This Milestone):** Deterministic facts, bootstrap, scope-specific readiness, and `ResourceInventory`. Pure function of environment facts and verified probes. Zero AI cognition used. Zero static profile dominance.
- **M3C:** Deterministic session-driver abstractions, access-channel capability contracts, `EconomicRegime`, `BudgetPool`, dynamic `BudgetState`, and deterministic portfolio validation/activation gates.
- **M3D:** AI-assisted Portfolio Planner synthesizing `PortfolioRecommendation` from `ResourceInventory` + project requirements + policy + history, and compiling `WorkflowPlan` topologies.

---

## 4. Scope-Specific Readiness Model

Rather than relying solely on a single global boolean or profile-scoped status, readiness is evaluated across 6 orthogonal capabilities:

```go
type ScopeKind string

const (
    ScopeCanExecuteSetupPlan       ScopeKind = "can_execute_setup_plan"
    ScopeHasAnyViableCognitionPath ScopeKind = "has_any_viable_cognition_path"
    ScopeCanRunLocalInference      ScopeKind = "can_run_local_inference"
    ScopeLocalModelAvailable       ScopeKind = "local_model_available"
    ScopeCanUseAuthenticatedCLI    ScopeKind = "can_use_existing_authenticated_cli"
    ScopePrincipalHostAvailable    ScopeKind = "principal_host_available"
)

type ScopeReadinessStatus string

const (
    ScopeStatusReady       ScopeReadinessStatus = "ready"
    ScopeStatusNotReady    ScopeReadinessStatus = "not_ready"
    ScopeStatusUnavailable ScopeReadinessStatus = "unavailable"
    ScopeStatusUnknown     ScopeReadinessStatus = "unknown"
)

type ScopeReadiness struct {
    Scope  ScopeKind            `json:"scope"`
    Status ScopeReadinessStatus `json:"status"`
    Reason string               `json:"reason"`
}
```

### Deterministic Evaluation Rules:
1. `can_execute_setup_plan`:
   - `ready`: Git installed, state root exists and is writable.
   - `not_ready`: State root missing or unwritable, or Git not found (remediable by setup).
   - `unavailable`: OS or base environment fundamentally unsupported.
2. `has_any_viable_cognition_path`:
   - `ready`: At least one endpoint (local runtime, authenticated CLI, or remote API) has `Health == ready`.
   - `not_ready`: Endpoints are detected but require authentication or configuration.
   - `unavailable`: No cognition endpoints exist on the machine.
3. `can_run_local_inference`:
   - `ready`: At least one local runtime endpoint has `Health == ready` (runtime running + usable model verified).
   - `not_ready`: Local runtime installed or port listening, but model not pulled or unconfigured.
   - `unavailable`: No local runtime is installed.
4. `local_model_available`:
   - `ready`: Verified local model present on an installed runtime.
   - `not_ready`: Local runtime installed, but no verified model present.
   - `unavailable`: No local runtime installed.
5. `can_use_existing_authenticated_cli`:
   - `ready`: Coding CLI endpoint has `Health == ready` and `Auth == authenticated`.
   - `not_ready`: Coding CLI installed, but auth is expired, unauthenticated, or unknown.
   - `unavailable`: No coding CLI installed.
6. `principal_host_available`:
   - `ready`: At least one supported host (Antigravity, Cursor, VSCode) has `Installed == true`.
   - `unavailable`: No supported host installed.

---

## 5. ResourceInventory Protocol Record

Defined in `internal/protocol/resource_inventory.go`:
```go
type ResourceInventory struct {
    SchemaVersion      SchemaVersion              `json:"schema_version"`
    InventoryID        string                     `json:"inventory_id"`
    MachineFingerprint string                     `json:"machine_fingerprint"`
    ObservedAt         Timestamp                  `json:"observed_at"`
    Hardware           HardwareSummary            `json:"hardware"`
    CognitionEndpoints []CognitionEndpointSummary `json:"cognition_endpoints,omitempty"`
    PrincipalHosts     []PrincipalHostSummary     `json:"principal_hosts,omitempty"`
    Credentials        []CredentialInventoryEntry `json:"credentials,omitempty"`
    Readiness          []ScopeReadiness           `json:"readiness,omitempty"`
    Policy             *PolicySummary             `json:"policy,omitempty"`
}
```

- Implements `protocol.Record` (`RecordKind() == "ResourceInventory"`).
- Validated by Draft 2020-12 schema `schemas/resource-inventory.schema.json`.
- Stable deterministic serialization: scopes always sorted in canonical order.
- Secret isolation: inherits `$defs/noSecretLike` pattern for all credential references and evidence.

---

## 6. Relationship with DoctorReport

- `DoctorReport` is the diagnostic evaluation artifact. It now carries `ScopeReadiness []ScopeReadiness` and an optional `ResourceInventory *ResourceInventory`.
- `ResourceInventory` is the machine-oriented factual substrate consumed by M3C/M3D.
- Both are produced by `Doctor` without manufacturing competing facts or re-probing the machine.

---

## 7. De-authorizing Old Static Deployment Profiles

- In `Doctor.Run`, `scope.TargetProfile` is no longer defaulted to `recommendation.SelectedProfile`.
- Canonical readiness does not require a `TargetProfile`.
- If an operator explicitly provides a `TargetProfile`, profile constraints are checked as an advisory filter, but omitting it yields a valid, evidence-backed `READY` whenever base dependencies and at least one viable cognition path are operational.
- The static profile chooser is purely an advisory UX label and has no authority over autonomous routing or portfolio synthesis.

---

## 8. Invariants & ADR Alignment

- **ADR-0011 / ADR-0013:** Machine profiles are computed, not persisted. Facts are separated from assessment.
- **ADR-0014 (§92):** Doctor computes deterministic readiness and ResourceInventory from verified facts. M3B does not solve optimal role/provider allocation with a static pure-function selector.
- **ADR-0018:** Deterministic ResourceInventory precedes AI recommendation.
- **DCI-081:** Zero raw secrets in durable records or logs.
- **DCI-055:** Local model runtimes (Ollama, MLX) treated as peer adapters.

---

## 9. Security & Threat Model

| Threat Vector | Mitigation |
|---|---|
| 1. Secret leakage via inventory or report | Inventory uses opaque `CredentialRef` and `AuthEvidence`. All fields validate against `LooksLikeSecret` and `$defs/noSecretLike`. |
| 2. Stale inventory accepted as current | Inventory carries `machine_fingerprint` and `observed_at`. Callers verify fingerprint freshness. |
| 3. Auth probe privilege escalation | Manual re-authenticate actions are strictly `AuthorityHighImpactManual`. |
| 4. Old profile recommender quietly acts as authority | Canonical readiness decoupled from `SelectedProfile`. Validation allows `READY` without a target profile. |

---

## 10. Failure & Degradation Semantics

- Missing GPU/acceleration: Degrades to CPU backend; `can_run_local_inference` remains ready if CPU is viable.
- Missing local runtime: `can_run_local_inference` is `unavailable`; does not invalidate authenticated CLIs.
- Missing credentials: Empty credential section; does not fail doctor execution.
- Malformed configured reference: Fails closed with `InvalidArgument` rather than fabricating evidence.

---

## 11. Schema & Protocol Discipline

- `schemas/resource-inventory.schema.json` updated with `$defs/scope_readiness` and `readiness` array.
- `schemas/doctor-report.schema.json` updated with optional `scope_readiness` and `resource_inventory`.
- `fixtures/protocol/resource-inventory.valid.json` updated with canonical `readiness`.
- `internal/schema/schema.go` registered in `RecordKindToSchema` and `AllNames()`.
- Full round-trip tests in `tests/schema_fixtures_test.go`.

---

## 12. Required 20-Scenario Verification Matrix

1. **Blank machine:** No Git, no runtimes, no CLIs, no hosts -> `can_execute_setup_plan: not_ready`, all cognition scopes `unavailable`, report `ACTION_REQUIRED`.
2. **CPU-only machine:** Supported CPU backend, no GPU -> `HardwareSummary` reports CPU backend, graceful operation.
3. **Local runtime installed but no model:** Ollama running, no model pulled -> `can_run_local_inference: not_ready`, `local_model_available: not_ready`.
4. **Local runtime + verified usable model:** Ready model -> `can_run_local_inference: ready`, `local_model_available: ready`.
5. **Authenticated CLI with no local inference:** Ready Claude/Codex CLI -> `can_use_existing_authenticated_cli: ready`, `can_run_local_inference: unavailable`, `has_any_viable_cognition_path: ready`.
6. **Installed CLI but authentication unknown:** Unverified probe -> `can_use_existing_authenticated_cli: not_ready` or `unknown`.
7. **Credential env var present but unverified:** Indeterminate status -> valid inventory entry, CLI auth not assumed.
8. **Unsupported keychain backend:** `UnsupportedKeychainChecker` returns `unavailable` -> inventory captures evidence without failing doctor.
9. **Multiple cognition endpoints:** Mix of local and remote -> all summarized in inventory.
10. **No cognition endpoint:** Valid deterministic state -> `has_any_viable_cognition_path: unavailable`, report `PARTIALLY_READY`.
11. **Principal host present vs absent:** Antigravity/Cursor/VSCode installed -> `principal_host_available: ready` vs `unavailable`.
12. **Runtime present but acceleration unavailable:** Unverified acceleration -> warning finding, graceful degradation.
13. **Partially stale evidence:** `stale_inference_retained` -> `READY_WITH_REDUCED_CAPABILITY`.
14. **One capability unavailable without invalidating others:** Missing local runtime does not block ready remote CLI.
15. **Deterministic ordering:** Scopes always sorted canonically; stable JSON serialization.
16. **No secret material in serialized outputs:** Inspected with `LooksLikeSecret`.
17. **Go/schema parity:** Round-trip and negative parity tests pass.
18. **Windows cross-compilation:** `GOOS=windows GOARCH=amd64 go build ./...` passes clean.
19. **Order independence:** ResourceInventory produced identically regardless of map or endpoint ordering.
20. **Static profile de-authorization:** `SelectedProfile` nil or non-matching does not block `READY` when capabilities exist; target profile nil produces `READY` without setting `SelectedProfile`.

---

## 13. Acceptance Criteria

- All 20 scenarios verified with automated deterministic unit and integration tests.
- Zero reliance on static deployment profile chooser for canonical readiness.
- No secrets in ResourceInventory or DoctorReport.
- Clean Go build, vet, test, race, and Windows cross-compilation.

## 14. Independent review disposition: 6 findings, all fixed

An independent review ([PR #10 comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5826591074), owner, 2026-09-25) found the overall direction good — EWP-before-code followed, `ResourceInventory` wired as a real Record, credential ref/evidence pairing integrity-checked, clean degradation on missing credentials/keychain support, the scope-specific readiness model a useful direction, the matrix covering many real heterogeneous-host cases, `FindingCodeNoCodingEndpoint` correctly not inventing a preferred provider, broad verification evidence — but 6 substantive blockers, all fixed in this revision:

1. **Static deployment profiles were still authoritative in the real `Doctor`/`Planner` path.** `Doctor.Run` still silently promoted `recommendation.SelectedProfile` into `scope.TargetProfile` (this line survived the earlier de-authorization work in this EWP's §7 — a real gap between documented intent and actual code, not just a missing test), which `evaluateReadiness` then used to enforce local-heavy/hybrid-thin/cloud-cognition constraints; `Planner.Plan` had the identical pattern deciding whether Ollama/MLX model-pull actions were planned; `DoctorReport.Validate()` still rejected `READY` with a nil `TargetProfile`. Fixed: the promotion line is removed from `Run` (a nil `TargetProfile` is used exactly as the caller passed it); `Planner.Plan` no longer defaults `profile` from `report.RecommendedProfile`, and its local-model gating now depends on `target` plus concrete facts (a discovered runtime-named endpoint) or explicit `PlannerOptions.SelectedRuntimes`, never the profile label; `DoctorReport.Validate()`'s READY-requires-TargetProfile rule is removed (READY is evidence-driven, via `ScopeReadiness`/viable-path checks, never gated by an unset UX label). New/rewritten tests: `TestMatrix_Scenario20_StaticProfileNoLongerGatesReadiness` (rewritten to exercise `Doctor.Run` end-to-end with a real `cognition.Service`/fake adapter, not the private `evaluateReadiness` helper — the prior version could not have caught this bug), `TestPlannerRecommendationLabelAloneCannotChangeSetupPlan`, `TestDoctorReportValidation` updated to assert READY-with-no-TargetProfile is now accepted.
2. **`ResourceInventory` was too lossy to satisfy the canonical "runtimes/models, capability provenance" contract.** It carried only `[]CognitionEndpointSummary` (deliberately reduced — no provider, runtime identity, model ID, capability grades/provenance by its own design) with no link back to the full `MachineCapabilityProfile` that has all of that. Fixed: new `protocol.MachineProfileRef` (`profile_id`, `machine_fingerprint`, `observed_at`, `probe_depth` — no duplication of the full profile) and a new `ResourceInventory.Profile *MachineProfileRef` field, populated from `Doctor.Run`'s own `cognProfile`; `ResourceInventory.Validate()` cross-checks `Profile.MachineFingerprint` against the inventory's own, so the two can never silently disagree. A consumer follows `Profile.ProfileID`/`MachineFingerprint` back to the cached full profile (ADR-0013's "computed, not persisted" — profiles are already cached by fingerprint) rather than the inventory re-deriving capability data itself.
3. **`BuildResourceInventory` was not the pure projection its own doc comment claimed.** It re-ran `checkStateRoot()`/`checkGit()`/`checkHardware()` internally — live filesystem/PATH probes, capable of disagreeing with `Run`'s own already-computed findings via TOCTOU, and (via `checkGit`'s `exec.LookPath` fallback) capable of leaking the *actual test-running host's* PATH into a supposedly-synthetic cross-platform facts set. It also stored caller-supplied collections in whatever order they arrived, with no normalization — scenario 19's claimed order-independence test never actually built two inventories to compare. Fixed: `BuildResourceInventory` no longer calls any `check*` method; it now takes the endpoints/hosts/`cognProfile`/`scopeReadiness` `Run` already computed as direct parameters (no re-derivation), and `Run` computes `scopeReadiness` exactly once, passing the same slice to both the report and the inventory (closing finding 4d's contradiction risk as a side effect — literally the same data, not two computations that could drift). `CognitionEndpoints`, `PrincipalHosts`, `Credentials` (by `RefID`), and `Hardware.AcceleratorBackends` are all sorted into a stable order before being stored. New tests: `TestMatrix_Scenario19b_ResourceInventoryOrderIndependence` (builds two real inventories from differently-ordered input and diffs their JSON), `TestBuildResourceInventory*` behavior otherwise unchanged/re-verified.
4. **Go/JSON-Schema twins were not actually equivalent**, in four concrete ways:
   - **4a.** `resource-inventory.schema.json` forked local `$defs/credential_ref`/`$defs/auth_evidence` copies that didn't carry the full WP-M3B-4 structural rules (e.g. accepted a lowercase `env_var` locator; accepted a `kind`/`probe_kind` mismatch Go rejects). Fixed by replacing both local `$defs` with cross-schema `$ref`s onto the canonical `credential-ref.schema.json`/`auth-evidence.schema.json` (the schema loader already registers every schema before compiling any, specifically so cross-file `$ref`s resolve) — one source of truth, not a copy that can silently drift. New tests: `TestResourceInventorySchemaParity` gained the review's exact two examples (nested lowercase `env_var` locator; nested `kind`/`probe_kind` mismatch), both now correctly rejected by Go and schema alike.
   - **4b.** `ResourceInventory`/`DoctorReport`'s Go endpoint validation checked only ID and Kind, far weaker than the schema's full `locality`/`health`/`auth_status`/`cost_class`/`required_source_exposure`/`acceleration_backend` requirements. Fixed by extracting a single canonical `CognitionEndpointSummary.Validate()` (moved from `ProjectState`'s own already-complete inline validation, the "existing ProjectState endpoint validation" the review pointed at) and using it in all three callers (`ProjectState`, `DoctorReport`, `ResourceInventory`) — a summary cannot be valid in one context and malformed in another. This immediately caught two of the matrix's own test fixtures (scenarios 18, 20) constructing incomplete endpoints that had been silently passing the old weak check.
   - **4c.** `HardwareSummary.Validate()` never validated `AcceleratorBackends` enum entries. Fixed: added the same enum check the schema already had.
   - **4d.** `doctor-report.schema.json`'s `resource_inventory` property was `{"type": "object"}` — effectively untyped, while Go recursively validates the real `ResourceInventory` contract. Fixed: `"resource_inventory": {"$ref": "devcadence:///resource-inventory.schema.json"}`. The report/inventory duplicate-fields concern (finding 3 above already made `ScopeReadiness` literally the same slice as `ResourceInventory.Readiness`) is additionally guarded by an explicit `DoctorReport.Validate()` check that `ResourceInventory.MachineFingerprint` matches the report's own and that the two `ScopeReadiness` sets agree (order-independent set comparison), so a future divergence fails validation rather than silently shipping.
5. **`has_any_viable_cognition_path` (and `can_use_existing_authenticated_cli`) could report `ready` for an unusable endpoint.** Both scopes checked only `Health == Ready`, ignoring `Auth` entirely — the matrix's own scenario 6 constructed a `ready`+`expired` CLI endpoint and only checked the narrower `can_use_existing_authenticated_cli` scope, missing that `has_any_viable_cognition_path` was *also* wrongly `ready` for the same endpoint. Fixed: new `protocol.EndpointViable(kind, locality, health, auth)` — the single authoritative "usable, not merely healthy" predicate (a local runtime is usable once ready; a remote/CLI endpoint additionally needs `authenticated`/`not_applicable`) — used by both `EvaluateScopeReadiness` and `Doctor.evaluateReadiness`'s own local/remote endpoint classification, so scope-specific and monolithic readiness can no longer disagree about what "viable" means. Presence of a `ready`-but-`unknown`-auth endpoint now reports `unknown` (not silently `not_ready`), distinguishing "evidence says no" from "no evidence yet." New test: `TestEvaluateScopeReadinessRespectsAuthentication` (5 cases: expired-auth CLI → not viable at all; unknown-auth CLI → `unknown`; local runtime → viable without auth; one bad remote + one good local → still viable; a genuinely authenticated CLI → ready).
6. **The re-authenticate `SetupAction`'s postcondition (`endpoint_healthy`) did not prove authentication** — exactly the WP-M3B-4 "healthy != authenticated != usable" invariant this WP's own scope-readiness model exists to enforce, violated by its own remediation action. Fixed: new `protocol.ConditionKind` `endpoint_authenticated` (own operand reusing `EndpointOperand`, own mutual-exclusion/validation branch, own `setup-plan.schema.json` `oneOf` case) with a parallel `internal/setup.EndpointAuthChecker` interface and `EvaluatorDeps.EndpointAuth`/`Executor.endpointAuth` wiring, mirroring `EndpointHealthChecker` exactly (also declared-but-not-yet-production-wired, the same state `EndpointHealthChecker` itself is in — no concrete checker exists in production code yet for either). The reauthenticate action's `Postconditions`/`VerificationCheck` now use `endpoint_authenticated`, not `endpoint_healthy`. New tests: `TestEvaluateConditionEndpointAuthenticatedFailsClosedWithoutChecker`, `TestEndpointHealthyAndEndpointAuthenticatedAreIndependent` (the review's exact required regression: a stub reporting healthy=true/authenticated=false proves `endpoint_healthy` passing does not make `endpoint_authenticated` pass), `TestEvaluateConditionEndpointAuthenticatedUsesConfiguredChecker`; `TestPlannerGeneratesReauthenticateActionForExpiredEndpoint` updated to assert the new condition kind.

**Matrix scenario naming corrected** (not a separate finding — the review's own note): scenarios 15, 17, and 19's doc comments overclaimed what they exercised (ScopeReadiness-only ordering/parity, not the full ResourceInventory). Comments corrected to state their actual narrower scope and point to the new tests (finding 3/4a's tests, and 19b) that cover the fuller claim.

**Verification (this revision):** `go build ./...`, `go vet ./...`, `gofmt -l` (every changed file, clean), `go test -count=1 ./...` (all 29 packages, including every new/updated test above), `go test -race ./internal/setup/... ./internal/protocol/... ./internal/credentials/... ./internal/schema/... ./tests/...` (no races), `GOOS=windows GOARCH=amd64`/`GOOS=linux GOARCH=amd64 go build ./...` (clean), `git diff --check` (clean whitespace).

**Disposition:** all 6 findings fixed; awaiting a follow-up review round before WP-M3B-5 can be marked `accepted`.

## 15. Independent review round 2 disposition: 4 findings, all fixed

A follow-up review ([PR #10 comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5827367817), owner, 2026-09-25, at head `7bb3053`) confirmed round 1's six findings were genuinely resolved, then found four further substantive blockers, fixed in commit `0b2dbe2` (verified and documented here, since that commit landed without an EWP/HANDOFF/PR-comment update of its own):

1. **`MachineProfileRef` was not actually retrievable.** `CacheManager` stored exactly one mutable `machine-profile.json` slot keyed by machine fingerprint, not by `ProfileID`; a later Doctor run silently overwrote the profile an earlier `ResourceInventory.Profile` referenced, leaving the reference dangling. Fixed: new `CacheManager.WriteProfile`/`WriteProfileWithExpiresAt`/`ReadProfileByID` (`internal/setup/cache.go`) persist each observed profile immutably under `profiles/<profile_id>.json` (file-locked, atomic tmp+rename+fsync, never overwritten by a later run since a later run has a different content-derived `ProfileID`), alongside the existing mutable "latest" cache slot for unkeyed lookups. `discoverEndpoints` now writes through `WriteProfile`/`WriteProfileWithExpiresAt` instead of the old fingerprint-keyed `Write`. `ResourceInventory.Validate()` now requires `Profile != nil` whenever `CognitionEndpoints` is non-empty (an inventory with endpoints can no longer silently omit the provenance reference), enforced in both Go and `resource-inventory.schema.json` (a new `allOf`/`if`/`then` requiring `profile` when `cognition_endpoints` has `minItems: 1`). New tests in `TestResourceInventorySchemaParity`/`cache_test.go` cover writing two distinct profiles for the same fingerprint and reading each back by its own `ProfileID` after the second write.
2. **Auth-aware viability was not propagated into discovery diagnostics, and `EndpointViable` had a locality-based auth bypass.** `discoverEndpoints` still branched on `ep.Health == EndpointHealthReady` alone, so a health-ready+auth-expired endpoint was reported `ENDPOINT_READY` and folded into `hasCoding`, contradicting the now-correct scope readiness for the same endpoint; separately, `EndpointViable`'s `kind == EndpointLocalRuntime || locality == LocalityLocal` let a malformed/future non-local-runtime endpoint claiming `LocalityLocal` bypass auth entirely. Fixed: `EndpointViable` now exempts only `kind == EndpointLocalRuntime` from the auth check (`internal/protocol/resource_inventory.go`); `discoverEndpoints` now branches on `protocol.EndpointViable(...)` first, then on `ep.Auth` (`AuthExpired`/`AuthUnauthenticated`/`AuthUnknown`/other) to choose between new `FindingCodeAuthUnauthenticated`/`FindingCodeAuthUnknown` and the existing `FindingCodeAuthExpired`, so a ready-but-unauthenticated endpoint is never silently counted as usable; `NO_CODING_ENDPOINT`'s title/detail now say "viable," not merely "healthy." The locality bypass is additionally closed structurally: `CognitionEndpoint.Validate()` and `CognitionEndpointSummary.Validate()` now reject `Kind == EndpointLocalRuntime` with `Locality != LocalityLocal`, and `Kind == EndpointRemoteAPI` with `Locality == LocalityLocal` — an authenticated CLI or remote API can no longer structurally claim the local-runtime shape in the first place. New/updated tests in `doctor_test.go`/`resource_inventory_test.go` cover health-ready+expired-auth, health-ready+unknown-auth, local-runtime-ready, and mixed unusable-remote+usable-local cases end-to-end through `Doctor.Run`.
3. **Remaining Go/JSON-Schema and report/inventory parity holes**, closed as follows:
   - **3a (duplicate readiness scopes).** Go already rejected duplicate `ScopeReadiness.Scope`; the schemas only had `uniqueItems` (which does not catch two same-scope entries with differing status/reason). Fixed: both `resource-inventory.schema.json` and `doctor-report.schema.json`'s `readiness`/`scope_readiness` arrays now carry one `contains`/`minContains: 0`/`maxContains: 1` constraint per canonical `ScopeKind`, expressing "at most one entry per scope" structurally in Draft 2020-12.
   - **3b (remote verified acceleration).** `CognitionEndpointSummary.Validate()` already rejected `acceleration_verified = true` with `locality != local`; the schemas accepted it. Fixed: both schemas' `cognition_endpoint_summary` definitions gained the matching `if acceleration_verified==true then locality==local` constraint (plus the same `if kind==local_runtime then locality==local` constraint from finding 2 above).
   - **3c (DoctorReport duplicate-projection drift).** `DoctorReport` carried both `DiscoveredEndpoints`/`PrincipalHosts` and an embedded `ResourceInventory` with its own copies, with no check they agreed — a valid report could show endpoint A at the top level and endpoint B inside its inventory. Fixed: `DoctorReport.Validate()` now calls new `cognitionEndpointsEqual`/`principalHostsEqual` helpers (ID-keyed, field-by-field comparison, order-independent) whenever either side is non-empty.
   - **3d (resource identity uniqueness).** Neither Go nor schema rejected two entries sharing an ID/RefID/HostID but differing in content. Fixed in Go: `ResourceInventory.Validate()` and `DoctorReport.Validate()` both now track seen IDs (`CognitionEndpoints` by `ID`, `PrincipalHosts` by `HostID`, `Credentials` by `Ref.RefID`) and reject a repeat. Schema-side, `uniqueItems: true` was added to every affected array, which catches byte-identical duplicates but — as documented in an explicit `description` added to each field in this revision — cannot by itself express "unique by id, permissive to differ in every other field is a bug" without restructuring the array into a keyed object; that residual Go-only strictness (Go rejects what schema alone would still accept: same ID, different content) is called out in-schema rather than silently left inconsistent, per the review's own third suggested option for finding 3a ("explicitly document why this is a Go-only semantic constraint"). New tests: `TestResourceInventoryValidation_DuplicateIDs`, `TestDoctorReportValidation_DuplicatesAndInventoryParity`, `TestDoctorReportSchemaParity`.
4. **`endpoint_authenticated` had no production verification path.** The condition kind, its `EndpointAuthChecker` interface, and `EvaluatorDeps`/`Executor` wiring existed, but `NewExecutor` defaulted `EndpointAuth` to `nil`, so a WP-M3B-5-generated reauthenticate action's postcondition would fail closed with "no `EndpointAuthChecker` configured" in production — a verification condition with no service-layer implementation capable of evaluating it. Fixed: new `internal/setup.CredentialsEndpointAuthChecker` (`conditions.go`) delegates to the accepted WP-M3B-4 `credentials.Manager` — it builds a `CredentialRef{Kind: CredRefCLISession, Locator: endpointID}` (also trying the `cli:`-stripped locator), calls `CheckCredential`, and reports `authenticated` iff the returned `AuthEvidence.Status == AuthStatusAuthenticated`. `ExecutorOptions` gained an optional `CredentialManager *credentials.Manager`; `NewExecutor` now defaults `EndpointAuth` to a `CredentialsEndpointAuthChecker` built from `CredentialManager` (or, failing that, a fresh `credentials.NewManager` built from the executor's own `Runner`) whenever the caller doesn't supply an explicit `EndpointAuth` — closing the "acknowledged not production-wired" gap from round 1 without inventing new auth-evidence semantics outside WP-M3B-4's boundary.

**Smaller accuracy note addressed:** the round-1 doc comment claiming `BuildResourceInventory` was "a pure, deterministic projection" was not literally true — it still called `CredentialManager.CheckCredential`, which can perform live env/keychain/CLI probes. Fixed by splitting the method: new `Doctor.ObserveCredentials` is the explicit observation phase (the only IO-performing step, gathering `[]CredentialInventoryEntry` for the configured `CredentialRefs`), and new package-level `ProjectResourceInventory` (taking an explicit `inventoryID`/`observedAt` plus all previously-observed data, including the credential entries and policy) is now the actual pure projection with no `Doctor` receiver and no IO. `Doctor.BuildResourceInventory` is kept as a thin convenience wrapper (`ObserveCredentials` then `ProjectResourceInventory`) for existing callers; its doc comment on the pure half no longer overclaims what the coordinating wrapper does.

**Verification (this revision):** `go build ./...`, `go vet ./...`, `go test -count=1 ./...` (all packages `ok`), `go test -count=1 ./internal/schema/... ./internal/protocol/... ./internal/setup/... ./tests/...` re-run after this session's additional schema `description` annotations — clean.

**Disposition:** all 4 round-2 findings fixed, plus the smaller BuildResourceInventory-purity accuracy note; awaiting a round-3 review before WP-M3B-5 can be marked `accepted`.
