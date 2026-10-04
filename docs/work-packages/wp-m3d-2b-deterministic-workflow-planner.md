# WP-M3D-2B — Deterministic Baseline Workflow Topology Planner

## Identity

- Work Package ID: WP-M3D-2B (window 2026-10-B; third slice of WP-M3D-2)
- Revision: 2 (window review: specified stage synthesis for deterministic-only, disambiguation algorithm for dual-review, pinned stage IDs and timeouts)
- Base commit: current `origin/main` at implementation time
- Status: READY_FOR_IMPLEMENTATION (r2)

## Objective

Provide a pure deterministic baseline planner in `internal/cognition/workflowplanner` that compiles a task definition, task risk tags, and active `CognitionPortfolio` into a candidate `WorkflowPlan` proposal.
It serves as the baseline decision engine before AI workflow planning (2C) and ensures that even without an active AI planning model, DevCadence can:
1. Synthesize minimal single-pass plans for low-risk tasks (topology collapse).
2. Synthesize dual-review or multi-stage plans for high-risk or security-sensitive tasks (topology expansion).
3. Synthesize deterministic-only plans with gate IDs for tool-only or deterministic tasks.
4. Automatically bind eligible roles, primary/fallback endpoints, and budget pools from the active portfolio.
5. Guarantee that every synthesized plan strictly passes `cognition.WorkflowValidator` (2A1).

## Context Manifest

- read-authority: `internal/protocol/portfolio.go` (WorkflowPlan, WorkflowStage, RoleBinding, CognitionPortfolio), `internal/cognition/workflow_validator.go`, `internal/cognition/workflow_diagnostics.go`
- risk tags: deterministic topology synthesis, role binding, topology collapse/expansion
- normative clauses: DCI-123 (deterministic authorization), DCI-124 (no authority expansion), PROTOCOLS §3C (bounded per-task topology)
- re-resolution triggers: need to change protocol types or schemas.

## Verified Facts

| ID | Claim | Evidence |
| --- | --- | --- |
| F-01 | `WorkflowTopology` has `TopologySinglePass`, `TopologyDualIndependentReview`, `TopologyDeterministicOnly` | `internal/protocol/portfolio.go` |
| F-02 | `WorkflowValidator` checks that dual-review topology requires two review stages on distinct endpoints or roles | `internal/protocol/portfolio.go`, `internal/cognition/workflow_validator.go` |
| F-03 | `CognitionPortfolio` defines available roles (`scout`, `implementer`, `reviewer`, `verifier`, etc.) and bindings | `internal/protocol/portfolio.go` |
| F-04 | High-risk tasks (e.g. risk tags `security`, `spending`, `schema`, `auth`, `authority`) require independent review under DevCadence directives | `docs/PROTOCOLS.md`, `docs/SECURITY.md` |

## Semantic Scope Envelope

### Authorized Domains
- NEW package `internal/cognition/workflowplanner/`:
  - `planner.go`: pure deterministic synthesis functions
  - `planner_test.go`: acceptance suite verifying plan validity across topologies
- Documentation sync in `docs/COGNITION_PORTFOLIO.md` and `docs/WORK_PACKAGES.md`.

### Forbidden
- No LLM invocation or external process calls (AI planning is 2C).
- No direct storage or state writes.
- Planner MUST NOT return plans that fail `cognition.WorkflowValidator`.

## Requirements

| ID | Strength | Requirement |
| --- | --- | --- |
| REQ-01 | MUST | API: `type TaskSpec struct { TaskID string; WorkPackageID string; RiskTags []string; RequiresDualReview bool; DeterministicOnly bool }`; `type PlanRequest struct { Task TaskSpec; Portfolio *protocol.CognitionPortfolio; Policy *cognition.WorkflowPolicy }`; `type PlanResult struct { Plan *protocol.WorkflowPlan; Diagnostics []cognition.PortfolioDiagnostic }`; `func PlanWorkflow(req PlanRequest) (*PlanResult, error)`. |
| REQ-02 | MUST | Stop rules: If `req.Portfolio == nil` or strings.TrimSpace(req.Task.TaskID) == "" or strings.TrimSpace(req.Task.WorkPackageID) == "", return `errs.CategoryInvalidArgument`. |
| REQ-03 | MUST | Topology selection: If `req.Task.DeterministicOnly` is true => synthesize `TopologyDeterministicOnly`. Else if `req.Task.RequiresDualReview` or any `req.Task.RiskTags` equals `"security"`, `"spending"`, `"schema"`, `"auth"`, or `"authority"` => synthesize `TopologyDualIndependentReview`. Else => synthesize `TopologySinglePass` (topology collapse). |
| REQ-04 | MUST | Plan Identity & Defaults: `PlanID = fmt.Sprintf("plan-%s", req.Task.TaskID)`. Stage IDs use format `fmt.Sprintf("%s-stage-%d", PlanID, order)`. Stage timeouts use `req.Portfolio.WorkflowDefaults.DefaultTimeoutSeconds` if > 0, else default to 1800 (capped at `effectivePolicy.MaxStageTimeoutSeconds`). `RetryLimit` uses `req.Portfolio.WorkflowDefaults.MaxRetries` if > 0, else 0. |
| REQ-05 | MUST | Stage synthesis for `TopologySinglePass`: 1 stage: `Order: 1`, Role: `"implementer"`, `Kind: protocol.StageKindCognition`, `IsReview: false`. Bound to primary tuple of role `"implementer"` in `req.Portfolio.RoleBindings`. |
| REQ-06 | MUST | Stage synthesis for `TopologyDualIndependentReview`: 3 stages: (1) `Order: 1`, Role: `"implementer"`, primary tuple of `"implementer"`. (2) `Order: 2`, Role: `"reviewer"`, `IsReview: true`, `DependsOn: [stage1.StageID]`, bound to primary tuple of `"reviewer"`. (3) `Order: 3`, `IsReview: true`, `DependsOn: [stage1.StageID]`. Review binding disambiguation: Check if role `"verifier"` exists in bindings with an endpoint distinct from Stage 2's endpoint; if so, bind `"verifier"` (primary). Else check `reviewer.Fallbacks` for first fallback tuple with an endpoint distinct from Stage 2's endpoint; if found, bind `"reviewer"` with that fallback tuple. Else bind `"verifier"` primary (allowing validator gate to catch any unbound/duplicate endpoint condition). |
| REQ-07 | MUST | Stage synthesis for `TopologyDeterministicOnly`: 1 stage: `Order: 1`, Role: `"verifier"`, `Kind: protocol.StageKindDeterministic`, `IsReview: false`. `DeterministicGateID = ptr(fmt.Sprintf("gate-%s", req.Task.TaskID))`. `EndpointID: nil, ChannelID: nil, ContextProfileID: nil`. `BudgetPoolID` set to first pool in `req.Portfolio.BudgetPools` matching `RegimeLocalCompute` (or first pool in `BudgetPools`). |
| REQ-08 | MUST | Validator Gating: Before returning, the synthesized `Plan` is run through `cognition.NewWorkflowValidator().Validate(cognition.WorkflowValidationInput{Plan: plan, Portfolio: req.Portfolio, Policy: req.Policy})`. If `!Valid`, `PlanResult` returns `Plan: nil` and the resulting `Diagnostics`. |
| REQ-09 | MUST | Pure & deterministic: Given identical `PlanRequest`, returns identical `WorkflowPlan` byte-for-byte. Never samples clock or random sources. |

## Invariants

| ID | Statement |
| --- | --- |
| INV-01 | Every successfully returned plan passes `cognition.WorkflowValidator` with `Valid == true`. |
| INV-02 | High-risk tasks are never collapsed to a single pass without independent review. |
| INV-03 | Deterministic stages carry non-nil `DeterministicGateID` and nil cognition pointers. |

## Acceptance Scenarios

| ID | Setup | Action | Expected |
| --- | --- | --- | --- |
| ACC-01 | Low-risk task with bound implementer | `PlanWorkflow` | `TopologySinglePass`, 1 stage, `WorkflowValidator.Valid == true` |
| ACC-02 | High-risk task (`RiskTags: ["security"]`) | `PlanWorkflow` | `TopologyDualIndependentReview`, review stages on distinct endpoints, validator `Valid == true` |
| ACC-03 | `DeterministicOnly: true` | `PlanWorkflow` | `TopologyDeterministicOnly`, 1 deterministic stage, valid gate ID, validator `Valid == true` |
| ACC-04 | Portfolio lacking required role bindings | `PlanWorkflow` | Returns `Plan: nil` and validator diagnostics (`WORKFLOW_ROLE_UNBOUND`) |
| ACC-05 | Idempotent / deterministic | `PlanWorkflow` called 10 times | Identical JSON output |

## Validation & Mutation Requirements

- **Mutation testing:** `mutation review sufficient`
- **Mutation catalog:**
  - collapse high-risk task to single-pass => ACC-02 fails
  - produce dual review with shared endpoint/role => ACC-02 validator check fails
  - produce deterministic stage with cognition pointers => ACC-03 validator check fails
  - skip validation gate before returning => ACC-04 fails
  - non-deterministic stage ID generation => ACC-05 fails
