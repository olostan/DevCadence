# WP-M3D-2A2 — Workflow Plan Live Budget and Metered-Pool Authorizer

## Identity

- Work Package ID: WP-M3D-2A2 (window 2026-10-B; second slice of WP-M3D-2)
- Revision: 2 (window review: removed unused ResourceStates parameter, standardized diagnostic targets and ConditionOverBudget, clarified non-blocking AllowOverage)
- Base commit: current `origin/main` at implementation time
- Status: READY_FOR_IMPLEMENTATION (r2)

## Objective

`WorkflowValidator` (WP-M3D-2A1) validates structural bounds, role bindings, and budget pool existence against static portfolio definitions. It intentionally does NOT read live budget states.

WP-M3D-2A2 provides the deterministic authorizer that validates a `WorkflowPlan` against live `map[string]*protocol.BudgetState`:
1. Verifies that budget pools referenced by active plan stages have healthy or usable budget states.
2. Fails closed on unknown, unobserved, or exhausted budget states for non-exempt regimes (e.g., metered API, prepaid credits) per ADR-0018 §9 and WP-M3C-H2.
3. Allows local compute regimes to proceed without mandatory live balance accounting when `AllowUnknownLocalCompute` is enabled.
4. Produces structured `PortfolioDiagnostic` errors and deterministic verdicts.

## Context Manifest

- read-authority: `internal/protocol/portfolio.go` (WorkflowPlan, WorkflowStage, BudgetPool), `internal/protocol/economics.go` (BudgetState, BudgetPoolStatus, EconomicRegime), `internal/cognition/workflow_validator.go`, `internal/cognition/workflow_diagnostics.go`
- risk tags: budget authorization, economic boundaries, fail-closed unknown states
- normative clauses: DCI-123 (deterministic authorization), DCI-104 (degrades safely), ADR-0018 §9 (budget gating), WP-M3C-H2 (fail-closed unknown state)
- re-resolution triggers: need to change protocol types or schemas.

## Verified Facts

| ID | Claim | Evidence |
| --- | --- | --- |
| F-01 | `BudgetState` has `PoolID`, `Status`, `CurrentUsage`, `RemainingBalance`, `ObservedAt` | `internal/protocol/economics.go:163-173` |
| F-02 | `BudgetPoolStatus` has constants `BudgetStatusHealthy`, `BudgetStatusSoftLimitExceeded`, `BudgetStatusExhausted`, `BudgetStatusUnknown` | `internal/protocol/economics.go:138-142` |
| F-03 | `BudgetPool` on `CognitionPortfolio` defines `PoolID`, `Regime`, `HardLimit`, `SoftAlertLimit`, `AllowOverage` | `internal/protocol/portfolio.go` |
| F-04 | `WorkflowStage` on `WorkflowPlan` carries `BudgetPoolID` string | `internal/protocol/portfolio.go` |
| F-05 | `RegimeLocalCompute` does not incur monetary overage; `RegimeMeteredAPI` and `RegimePrepaidCredits` require explicit budget availability | ADR-0018 §9 |

## Semantic Scope Envelope

### Authorized Domains
- NEW file in `internal/cognition/`: `workflow_budget_authorizer.go`, `workflow_budget_authorizer_test.go`
- Extend `internal/cognition/workflow_diagnostics.go` with budget authorization diagnostic codes:
  - `CodeWorkflowBudgetExhausted = "WORKFLOW_BUDGET_EXHAUSTED"`
  - `CodeWorkflowBudgetUnknown = "WORKFLOW_BUDGET_UNKNOWN"`
  - `CodeWorkflowBudgetMissingState = "WORKFLOW_BUDGET_MISSING_STATE"`
- Documentation sync in `docs/COGNITION_PORTFOLIO.md` and `docs/WORK_PACKAGES.md`.

### Forbidden
- No mutations to input plans, portfolios, or budget states.
- No direct network or database calls; budget states are passed as caller-supplied in-memory maps.
- No modification of `WorkflowValidator` (2A1) core logic; budget authorization is a clean composition step.
- Host-level hardware/slot telemetry checks (`ResourceState`) are out of scope for pool authorization.

## Requirements

| ID | Strength | Requirement |
| --- | --- | --- |
| REQ-01 | MUST | API: `type WorkflowBudgetInput struct { Plan *protocol.WorkflowPlan; Portfolio *protocol.CognitionPortfolio; BudgetStates map[string]*protocol.BudgetState; AllowUnknownLocalCompute bool }`; `type WorkflowBudgetResult struct { Authorized bool; Diagnostics []PortfolioDiagnostic }`; `func AuthorizeWorkflowBudget(in WorkflowBudgetInput) WorkflowBudgetResult`. |
| REQ-02 | MUST | Stop rules: If `in.Plan == nil` or `in.Portfolio == nil`, returns `Authorized: false` with ONE diagnostic `CodeWorkflowInputMissing` (Target: `"plan"` if plan is nil else `"portfolio"`). If `in.Plan.Validate() != nil`, returns `Authorized: false` with `CodeWorkflowPlanInvalid`. If `in.Portfolio.Validate() != nil`, returns `Authorized: false` with `CodeWorkflowPortfolioInvalid`. |
| REQ-03 | MUST | For each stage in `in.Plan.Stages`: look up `stage.BudgetPoolID` in `in.Portfolio.BudgetPools`. If the pool is missing, emit `CodeWorkflowUnknownBudgetPool` with `Target: fmt.Sprintf("stages[%d]", i)`, `Condition: ConditionInvalid`, and skip further checks for that stage. |
| REQ-04 | MUST | For each referenced pool: inspect `in.BudgetStates[poolID]`. If state is missing (`nil`): if `pool.Regime == RegimeLocalCompute` and `in.AllowUnknownLocalCompute` is true, proceed; otherwise emit `CodeWorkflowBudgetMissingState` with `Target: fmt.Sprintf("stages[%d]", i)`, `Condition: ConditionUnauthorized`, `Authorized: false`. |
| REQ-05 | MUST | If `BudgetState.Status == BudgetStatusExhausted` or (`RemainingBalance != nil` and `*RemainingBalance <= 0`): if `pool.AllowOverage` is true on `RegimeMeteredAPI` or `RegimeEnterpriseAllocation`, proceed without blocking; otherwise emit `CodeWorkflowBudgetExhausted` with `Target: fmt.Sprintf("stages[%d]", i)`, `Condition: ConditionOverBudget`, `Authorized: false`. |
| REQ-06 | MUST | If `BudgetState.Status == BudgetStatusUnknown`: if `pool.Regime == RegimeLocalCompute` and `in.AllowUnknownLocalCompute` is true, proceed; otherwise emit `CodeWorkflowBudgetUnknown` with `Target: fmt.Sprintf("stages[%d]", i)`, `Condition: ConditionUnauthorized`, `Authorized: false`. |
| REQ-07 | MUST | Diagnostics are sorted deterministically via `SortedDiagnostics`. |

## Invariants

| ID | Statement |
| --- | --- |
| INV-01 | A plan referencing an exhausted metered pool is NEVER authorized unless `AllowOverage` is explicitly true for that pool. |
| INV-02 | Missing or unknown budget state fails closed by default for monetary regimes. |
| INV-03 | Authorizer is pure and non-mutating. |

## Acceptance Scenarios

| ID | Setup | Action | Expected |
| --- | --- | --- | --- |
| ACC-01 | Plan with healthy budget state for all pools | `AuthorizeWorkflowBudget` | `Authorized: true`, 0 diagnostics |
| ACC-02 | Plan referencing pool with `BudgetStatusExhausted` without overage | `AuthorizeWorkflowBudget` | `Authorized: false`, `WORKFLOW_BUDGET_EXHAUSTED` |
| ACC-03 | Plan referencing metered pool with missing state | `AuthorizeWorkflowBudget` | `Authorized: false`, `WORKFLOW_BUDGET_MISSING_STATE` |
| ACC-04 | Plan referencing local compute pool with missing/unknown state and `AllowUnknownLocalCompute=true` | `AuthorizeWorkflowBudget` | `Authorized: true`, 0 diagnostics |
| ACC-05 | Exhausted pool with `AllowOverage=true` on metered regime | `AuthorizeWorkflowBudget` | `Authorized: true`, 0 diagnostics |
| ACC-06 | Plan with invalid structure or nil inputs | `AuthorizeWorkflowBudget` | `Authorized: false`, expected stop rule diagnostic |

## Validation & Mutation Requirements

- **Mutation testing:** `mutation review sufficient`
- **Mutation catalog:**
  - skip `BudgetStatusExhausted` check => ACC-02 fails
  - allow missing state on metered regime => ACC-03 fails
  - fail local compute when `AllowUnknownLocalCompute=true` => ACC-04 fails
  - ignore `AllowOverage` => ACC-05 fails
  - mutate input plan or state => immutability check fails
