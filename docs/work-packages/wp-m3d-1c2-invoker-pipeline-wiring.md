# WP-M3D-1C2 — Invoker Pipeline Wiring and Planning Endpoint Selection

## Identity

- Work Package ID: WP-M3D-1C2 (window 2026-10-B; second slice of WP-M3D-1C)
- Revision: 2 (window review: located in internal/cognition/plannerdriver to prevent import cycle, corrected CognitionEndpointSummary fields and EndpointViable check, clarified graceful degradation in ACC-03)
- Base commit: current `origin/main` at implementation time
- Status: READY_FOR_IMPLEMENTATION (r2)

## Objective

WP-M3D-1B provides the pure `planner.Plan` service requiring an injected `planner.Invoker`. WP-M3D-1C1 provides `plannerdriver.NewInvoker` adapting a `drivers.SessionDriver` to `planner.Invoker`.

WP-M3D-1C2 wires these components together into an end-to-end planning executor inside package `internal/cognition/plannerdriver`:
1. Deterministically selects an eligible cognition endpoint for the planner role from the active `ResourceInventory`.
2. Resolves the driver and model ID for that endpoint via an injected resolver.
3. Constructs the `plannerdriver.Invoker` with caller-bound configuration and timeout, verifying driver capabilities (rejecting native worktree access).
4. Invokes `planner.Plan` to produce validated `PortfolioRecommendation` alternatives.
5. If no endpoint is eligible, available, or driver cannot be instantiated, gracefully falls back to `OutcomeNoPlanner` per DCI-104.

## Context Manifest

- read-authority: `internal/cognition/planner/planner.go`, `internal/cognition/plannerdriver/invoker.go`, `internal/cognition/drivers/driver.go`, `internal/protocol/resource_inventory.go`, `internal/protocol/portfolio.go`
- risk tags: endpoint selection, driver resolution, graceful degradation, secret isolation
- normative clauses: DCI-104 (capability absence degrades, not fails), DCI-124 (no authority expansion), DCI-054/055 (adapters replaceable)
- re-resolution triggers: need to change driver interfaces or planner protocol.

## Verified Facts

| ID | Claim | Evidence |
| --- | --- | --- |
| F-01 | `planner.Plan` returns `OutcomeNoPlanner` when `req.Invoker == nil` | `internal/cognition/planner/planner.go:52` |
| F-02 | `plannerdriver.NewInvoker` requires non-empty `EndpointID` and `ModelID`, rejects native worktree access | `internal/cognition/plannerdriver/invoker.go:50-64` |
| F-03 | `ResourceInventory.CognitionEndpoints` provides `ID`, `Kind`, `Locality`, `Health`, `Auth`, `CostClass`, `RequiredSourceExposure` | `internal/protocol/resource_inventory.go:121-137` |
| F-04 | `protocol.EndpointViable(kind, locality, health, auth)` checks whether an endpoint is functionally usable | `internal/protocol/resource_inventory.go:180-205` |

## Semantic Scope Envelope

### Authorized Domains
- NEW file in `internal/cognition/plannerdriver/`: `service.go`, `service_test.go`
- Documentation sync in `docs/COGNITION_PORTFOLIO.md` and `docs/WORK_PACKAGES.md`.

### Forbidden
- No persistent state writes or automatic activation of recommended portfolios.
- No direct credential reading; credentials remain opaque tokens.
- No import from `internal/cognition/planner` into `plannerdriver` (dependency direction is `plannerdriver -> planner`, never cyclic).

## Requirements

| ID | Strength | Requirement |
| --- | --- | --- |
| REQ-01 | MUST | API: `type DriverResolver interface { ResolveDriver(ctx context.Context, endpointID string) (drivers.SessionDriver, string, error) }` (returns driver, modelID, err); `type ExecutionConfig struct { Timeout time.Duration; IncludeErrorText bool }`; `func ExecutePlanning(ctx context.Context, req planner.Request, resolver DriverResolver, cfg ExecutionConfig) (*planner.Result, error)`. |
| REQ-02 | MUST | Stop rules & guards: If `req.Inventory == nil` or `resolver == nil`, return `errs.CategoryInvalidArgument`. |
| REQ-03 | MUST | Endpoint selection: Iterate through `req.Inventory.CognitionEndpoints` in slice order. For each endpoint: verify `protocol.EndpointViable(ep.Kind, ep.Locality, ep.Health, ep.Auth)`. If viable, call `resolver.ResolveDriver(ctx, ep.ID)`. If resolved with no error and `!driver.Capabilities().NativeWorktreeAccess`: select this endpoint and driver; break. |
| REQ-04 | MUST | Graceful degradation: If no candidate endpoint in `req.Inventory.CognitionEndpoints` meets all viability and capability conditions: set `req.Invoker = nil` and call `planner.Plan(ctx, req)`, returning its result (`OutcomeNoPlanner`) with nil error. |
| REQ-05 | MUST | Driver binding: If an endpoint is selected, construct `inv, err := NewInvoker(driver, Config{EndpointID: ep.ID, ModelID: modelID, Timeout: cfg.Timeout, IncludeErrorText: cfg.IncludeErrorText})`. If `NewInvoker` errors, fall back to next viable endpoint or degrade gracefully to `OutcomeNoPlanner`. |
| REQ-06 | MUST | Execution: Call `planner.Plan(ctx, req)` with `req.Invoker = inv` and return the resulting `*planner.Result` and error. |

## Invariants

| ID | Statement |
| --- | --- |
| INV-01 | Lack of an eligible planning endpoint degrades gracefully to `OutcomeNoPlanner` without panics or unexpected errors. |
| INV-02 | Provenance and credentials remain strictly isolated. |
| INV-03 | `internal/cognition/planner` remains an independent pure package without importing `plannerdriver`. |

## Acceptance Scenarios

| ID | Setup | Action | Expected |
| --- | --- | --- | --- |
| ACC-01 | Inventory with valid endpoint and working driver | `ExecutePlanning` | `OutcomeRecommended`, validated recommendations, verified Go-assigned provenance |
| ACC-02 | Inventory with no healthy endpoints | `ExecutePlanning` | `OutcomeNoPlanner`, `Detail: "no planning endpoint available; deterministic-only"` |
| ACC-03 | Inventory with driver declaring `NativeWorktreeAccess == true` and no other endpoints | `ExecutePlanning` | Gracefully degrades to `OutcomeNoPlanner` |
| ACC-04 | Timeout occurs during execution | `ExecutePlanning` | Context deadline honored, session cleanly closed |
| ACC-05 | Nil inventory or nil resolver | `ExecutePlanning` | `errs.CategoryInvalidArgument` returned |

## Validation & Mutation Requirements

- **Mutation testing:** `mutation review sufficient`
- **Mutation catalog:**
  - fail with hard error instead of `OutcomeNoPlanner` when endpoint absent => ACC-02 fails
  - allow `NativeWorktreeAccess` driver => ACC-03 fails
  - omit timeout bounding => ACC-04 fails
  - fail to guard nil inventory/resolver => ACC-05 fails
