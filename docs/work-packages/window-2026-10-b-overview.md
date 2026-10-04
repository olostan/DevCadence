# Planning Window 2026-10-B: M3D Topology Planning and End-to-End Cognition

## Window Objective & Overview

Window 2026-10-B advances DevCadence through Milestone M3D by connecting pure validators and invokers into complete, bounded runtime components for workflow topology planning and cognition pipeline execution.

Following the success of Window 2026-10-A (which delivered WP-M3C-H4 effective policy, WP-M3D-1C1 driver invoker, and WP-M3D-2A1 pure workflow plan validator), Window 2026-10-B establishes the next coherent set of 3 bounded Work Packages:

1. **WP-M3D-2A2 — Workflow Plan Budget and Metered-Pool Authorizer**
   - Pure validator extending plan verification against live `BudgetState` records.
   - Enforces fail-closed unknown budget state, hard/soft limits, and metered API quota gating.
2. **WP-M3D-2B — Deterministic Baseline Workflow Topology Planner**
   - Pure rule-based planner that compiles a task definition + risk tags + active portfolio into a candidate `WorkflowPlan`.
   - Supports topology collapse (single-pass), dual-review expansion (dual independent review), and deterministic-only execution without AI invocation.
3. **WP-M3D-1C2 — Invoker Pipeline Wiring and Endpoint Selection**
   - Implemented in `internal/cognition/plannerdriver` (avoiding import cycles with `internal/cognition/planner`).
   - Connects endpoint eligibility selection and driver-backed invokers into a complete portfolio planning execution pipeline.

---

## Shared Architecture, Authority, and Interface Invariants

Across all packages in Window 2026-10-B, the following cross-cutting boundaries MUST be preserved:

1. **"AI proposes, deterministic machinery authorizes" (DCI-123/124/125):**
   - Neither the deterministic planner (2B) nor any future model planner (2C) possesses authorization authority; planners produce candidate plan proposals.
   - Every generated `WorkflowPlan` must pass `cognition.WorkflowValidator` (2A1) and budget authorization (2A2) before acceptance.
2. **Deterministic & Pure Execution:**
   - Planners and validators never mutate input arguments.
   - Pure validators (2A1, 2A2) perform no disk I/O, network calls, or clock sampling; timestamps and external observations are caller-supplied.
3. **Fail-Closed Budget & Resource Safety:**
   - Incomplete, stale, or unknown budget state for a metered or capped pool fails closed unless explicitly permitted by policy.
4. **Boundary Isolation & Dependency Direction:**
   - `internal/cognition/planner` defines pure prompt building, output decoding, and validator gating; it NEVER imports `drivers` or `plannerdriver`.
   - Caller-side wiring and endpoint selection live in `internal/cognition/plannerdriver`, which imports `planner` and `drivers` cleanly.
5. **Risk-Based Mutation Testing & Test Strength:**
   - Critical deterministic logic (bounds checking, quota calculation, topology routing) must undergo mutation review and kill all plausible mutants.
