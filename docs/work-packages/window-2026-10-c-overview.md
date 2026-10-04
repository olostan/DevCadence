# Rolling Planning Window 2026-10-C: M3D Topology Planning Completion, Portfolio Adaptation, and Adaptive CLI

## Window Objective & Overview

Window 2026-10-C completes Milestone M3D by establishing the final three capabilities in DevCadence's adaptive cognition architecture:
1. Model-assisted workflow topology synthesis with graceful deterministic fallback (**WP-M3D-2C**).
2. Auditable portfolio adaptation, semantic diffing, versioned activation, and atomic rollback (**WP-M3D-3**).
3. Plain/JSON-first adaptive CLI recommendations, explanation, and application UX (**WP-M3D-4**).

Following Window 2026-10-B (which delivered the deterministic baseline planner 2B, live budget authorizer 2A2, and driver invoker pipeline wiring 1C2), Window 2026-10-C transitions DevCadence from purely manual/heuristic configuration into a complete, governed, adaptive cognition control loop.

---

## Work Packages in Window 2026-10-C

### 1. WP-M3D-2C — Model-Assisted Workflow Topology Planner
- **Package:** `internal/cognition/workflowplanner`
- **Focus:** Enables AI endpoints to propose complex or decomposed workflow stages (e.g. multi-step pipelines, research scouts, specialized reviewers) via the planner invoker pipeline when available.
- **Key Invariants:**
  - "AI proposes, deterministic machinery authorizes" (DCI-123/124).
  - Graceful fallback: If no eligible endpoint exists, invoker fails, or model output is malformed, falls back cleanly to deterministic baseline synthesis (`PlanWorkflow` from 2B) rather than failing the planning request (DCI-104).
  - Strict gating: Model-proposed topologies MUST pass `cognition.WorkflowValidator` (2A1) and `cognition.AuthorizeWorkflowBudget` (2A2).

### 2. WP-M3D-3 — Portfolio Adaptation, Versioning & Atomic Rollback
- **Package:** `internal/cognition`
- **Focus:** Provides typed, explainable transition mechanics for cognition portfolios when hardware, subscriptions, quotas, or policies change.
- **Key Invariants:**
  - Typed semantic diffing: `PortfolioDiff` records added, removed, and modified channels, role bindings, and budget pools between any two portfolios.
  - Explanatory rationale: Portfolio transitions record explicit triggers and reasons without autonomous or silent policy drift (ADR-0018 §11, DCI-124).
  - Atomic versioning & rollback: Leverages `ActivationManager` to guarantee crash-consistent activation of updated portfolios (`active-portfolio.json`), lineage tracking, and deterministic rollback to prior active configurations.

### 3. WP-M3D-4 — Adaptive CLI Setup & Explanation UX
- **Package:** `cmd/devcadence` / `internal/setup`
- **Focus:** Surfaces portfolio and workflow recommendations in the user-facing CLI via `devcadence cognition recommend`, `devcadence cognition apply`, and `devcadence cognition explain`.
- **Key Invariants:**
  - Plain terminal / JSON output first: `--json` emits canonical protocol documents and diff records; human output is clear, clean, and deterministic.
  - Single decision engine: The CLI commands invoke `internal/cognition` and `internal/setup` core services directly; the UX never acts as a second decision engine (ADR-0018 §15).
  - Deterministic exit codes: Standard exit codes (0 for success, 1–6 for categorized failures) without raw stack traces.

---

## Architectural Boundaries and Governance

1. **AI Proposes, Deterministic Machinery Authorizes (DCI-123/124/125):**
   No model proposal (neither portfolio nor workflow topology) bypasses deterministic validation or budget gating.
2. **Fail-Closed Budget & Resource Safety:**
   Missing or exhausted budget state fails closed unless explicitly permitted (e.g. `AllowUnknownLocalCompute` or `AllowOverage`).
3. **No Provider Coupling (DCI-054/055):**
   All planner invokers communicate through abstract `drivers.SessionDriver` / `planner.Invoker` interfaces. Core packages never import provider SDKs or concrete adapters.
4. **Idempotence and Purity:**
   Diffing, validation, and baseline synthesis functions are pure and non-mutating. Durability and file operations are isolated to storage and activation managers.
5. **Mutation Testing Rigor:**
   Every package defines an adversarial mutation catalog table and verifies that all plausible mutants are killed by unit tests.
