# Engineering Work Package: WP-M3B-5 — Doctor readiness and ResourceInventory (service layer, no public CLI)

- **Milestone:** M3B — Guided bootstrap and onboarding
- **Scope card:** [docs/WORK_PACKAGES.md#wp-m3b-5--doctor-readiness-and-resource-inventory-service-layer-no-public-cli](../WORK_PACKAGES.md#wp-m3b-5--doctor-readiness-and-resource-inventory-service-layer-no-public-cli)
- **Base commit:** `e2e0849` (`feat/m3b-guided-bootstrap`, includes accepted WP-M3B-1 through WP-M3B-4 checkpoints)
- **Branch:** `feat/m3b-guided-bootstrap`
- **Depends on:** WP-M3B-1 (`SetupPlan`/`SetupAction`/`TypedOperation`, accepted), WP-M3B-2 (ledger/home layout, accepted), WP-M3B-3 (executor, accepted), WP-M3B-4 (`CredentialRef`/`AuthEvidence`/`Manager`, accepted).
- **Status:** Expanded & Authoritative; implementation complete; verification in progress.

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
