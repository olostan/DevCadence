# WP-M3D-2C — Model-Assisted Workflow Topology Planner

## Identity

- Work Package ID: WP-M3D-2C (window 2026-10-C; fourth slice of WP-M3D-2)
- Revision: 2 (window review: topology defaulting contract, DAG dependency remapping during stage ID sanitization, unified input struct signatures, context cancellation preservation)
- Task ID: autonomous-run-1
- Base commit: `a743144af6f4a1c8051628d65766afb331038a4e`
- Project state revision: 1
- Contract digest: n/a (Markdown contract authoritative)
- Target implementation endpoint/profile: competent Go implementer
- Status: READY_FOR_IMPLEMENTATION (r2)

## Objective

Provide model-assisted workflow topology planning in `internal/cognition/workflowplanner`. When an eligible cognition endpoint is available, the planner prompts the model via an injected `planner.Invoker` to propose specialized workflow stage decompositions (e.g. multi-stage scout/implementer/reviewer pipelines). When no invoker is provided, the invoker fails, or the model produces malformed/invalid plans, the system gracefully falls back to the deterministic baseline (`PlanWorkflow` from WP-M3D-2B). Every returned plan is deterministically validated by `cognition.WorkflowValidator` (2A1) and gated by `cognition.AuthorizeWorkflowBudget` (2A2).

## Context Manifest

- role: implementer (Go), independent reviewers
- read-authority envelope: `internal/protocol/portfolio.go` (WorkflowPlan, WorkflowStage, RoleBinding, CognitionPortfolio), `internal/cognition/workflow_validator.go`, `internal/cognition/workflow_budget_authorizer.go`, `internal/cognition/planner/planner.go` (Invoker interface), `internal/cognition/workflowplanner/planner.go`
- semantic write/scope envelope: `internal/cognition/workflowplanner/` (model planner implementation and tests), docs
- risk tags: untrusted model output parsing, graceful degradation, budget authorization, determinism
- exact normative clauses: DCI-123 (AI proposes, deterministic machinery authorizes), DCI-124 (no authority expansion), DCI-104 (capability absence degrades gracefully), PROTOCOLS §3C (bounded per-task topology)
- initial evidence handles: `internal/cognition/workflowplanner/planner.go`, `internal/cognition/planner/planner.go`
- deferred references: none
- assumptions: A1 `planner.Invoker` provides standard one-turn invocation. A2 Deterministic baseline `PlanWorkflow` is available in the same package.
- re-resolution triggers: need to change protocol types or schemas.

## Semantic scope envelope

### Authorized domains / path patterns

- `internal/cognition/workflowplanner/model_planner.go` (new)
- `internal/cognition/workflowplanner/model_planner_test.go` (new)
- `internal/cognition/workflowplanner/planner.go` (minor exported helper if needed)
- `docs/COGNITION_PORTFOLIO.md`, `docs/WORK_PACKAGES.md`

### Explicitly forbidden semantic changes

- No public protocol type or schema changes.
- Package MUST NOT import `internal/cognition/drivers` or concrete adapters (respecting boundaries).
- No unvalidated model output may be returned.
- No failure or panics on malformed model output; must fallback to deterministic baseline.

### LOCAL_DISCRETION

- Private prompt building and decoding helpers.
- Test fixture organization.

## Requirements

| ID | Strength | Requirement | Normative source |
| --- | --- | --- | --- |
| REQ-01 | MUST | API: `type ModelPlanRequest struct { Task TaskSpec; Portfolio *protocol.CognitionPortfolio; Policy *cognition.WorkflowPolicy; BudgetStates map[string]*protocol.BudgetState; AllowUnknownLocalCompute bool; Invoker planner.Invoker }`; `type ModelPlanResult struct { Plan *protocol.WorkflowPlan; Diagnostics []cognition.PortfolioDiagnostic; UsedModel bool; FallbackReason string }`; `func PlanWorkflowWithModel(ctx context.Context, req ModelPlanRequest) (*ModelPlanResult, error)`. | design |
| REQ-02 | MUST | Stop rules: If `req.Portfolio == nil` or `strings.TrimSpace(req.Task.TaskID) == ""` or `strings.TrimSpace(req.Task.WorkPackageID) == ""`, return `errs.CategoryInvalidArgument`. If `ctx.Err() != nil` before or after invocation, return `ctx.Err()` UNWRAPPED (never catch as fallback). | DCI-104 |
| REQ-03 | MUST | Invoker absence degradation: If `req.Invoker == nil` or `req.Task.DeterministicOnly`, execute deterministic baseline `PlanWorkflow`, validate budget via `AuthorizeWorkflowBudget`, and return `UsedModel: false, FallbackReason: "no_invoker_or_deterministic_only"`. | DCI-104 |
| REQ-04 | MUST | Prompt generation: `BuildWorkflowPrompt(task TaskSpec, portfolio *protocol.CognitionPortfolio, policy *cognition.WorkflowPolicy)` produces a deterministic prompt containing task metadata, risk tags, role bindings, budget pools, policy limits, and strict JSON output instructions. | DCI-124 |
| REQ-05 | MUST | Strict JSON Decoding: Expects `{"stages": [...]}` or `{"plan": {"stages": [...]}}`. Stages are decoded with bounds checking (`len(stages) <= maxStages`, default 8). Malformed JSON or schema mismatches trigger immediate graceful fallback with `FallbackReason: "malformed_model_output"`. Model may propose optional `topology`; if omitted, invalid, or violates task risk constraints, Go defaults `Topology = selectTopology(req.Task)`. | DCI-123 |
| REQ-06 | MUST | Mandatory Deterministic Gating: Model-proposed candidate plan must be validated with `cognition.NewWorkflowValidator().Validate(cognition.WorkflowValidationInput{Plan: candidatePlan, Portfolio: req.Portfolio, Policy: req.Policy})` AND `cognition.AuthorizeWorkflowBudget(cognition.WorkflowBudgetInput{Plan: candidatePlan, Portfolio: req.Portfolio, BudgetStates: req.BudgetStates, AllowUnknownLocalCompute: req.AllowUnknownLocalCompute})`. If either fails (`!Valid` or `!Authorized`), the planner falls back to deterministic baseline with `FallbackReason: "validator_rejected_model_plan"` or `"budget_unauthorized_model_plan"`. | DCI-123/124 |
| REQ-07 | MUST | Identity and DAG Sanitization: Go overwrites `PlanID = fmt.Sprintf("plan-%s", req.Task.TaskID)`, `TaskID`, `WorkPackageID`, `SchemaVersion = protocol.SchemaVersion1`. When sanitizing stage IDs to `fmt.Sprintf("%s-stage-%d", planID, order)`, Go builds an ID mapping table (`oldToNewMap`) and remaps each stage's `DependsOn` and `EscalationTarget` references so DAG dependencies remain intact. | DCI-129 |
| REQ-08 | MUST | Budget Authorization on Fallback: Baseline fallback plan is also authorized against `BudgetStates`. | DCI-124 |

## Invariants / state rules

| ID | Statement | Related requirements |
| --- | --- | --- |
| INV-01 | Model output is never returned unless verified by both `WorkflowValidator` and `AuthorizeWorkflowBudget`. | REQ-06 |
| INV-02 | Model unavailability or malformed output never aborts planning; deterministic baseline is returned. | REQ-03, REQ-05 |
| INV-03 | Plan identity, task identity, schema version, and stage ID mappings are controlled Go-side. | REQ-07 |

## Interface / algorithm contract

```text
PlanWorkflowWithModel(ctx, req):
  1. Validate non-nil inputs (portfolio, task id, wp id). If ctx.Err() != nil -> return ctx.Err().
  2. If req.Invoker == nil or req.Task.DeterministicOnly:
       return fallbackToBaseline(req, "no_invoker_or_deterministic_only")
  3. prompt, promptDigest := BuildWorkflowPrompt(req.Task, req.Portfolio, req.Policy)
  4. invRes, err := req.Invoker.Invoke(ctx, planner.Invocation{Prompt: prompt, PromptDigest: promptDigest})
     If ctx.Err() != nil -> return ctx.Err()
     If err != nil:
       return fallbackToBaseline(req, "invoker_error: " + err.Error())
  5. stages, proposedTopology, err := decodeModelStages(invRes.Content)
     If err != nil:
       return fallbackToBaseline(req, "malformed_model_output: " + err.Error())
  6. candidatePlan := assemblePlan(req.Task, stages, proposedTopology)
     (assemblePlan assigns PlanID, TaskID, WPID, SchemaVersion, remaps DAG IDs via oldToNewMap, and sets valid Topology)
  7. valResult := cognition.NewWorkflowValidator().Validate(cognition.WorkflowValidationInput{Plan: candidatePlan, Portfolio: req.Portfolio, Policy: req.Policy})
     If !valResult.Valid:
       return fallbackToBaseline(req, "validator_rejected_model_plan")
  8. budgetResult := cognition.AuthorizeWorkflowBudget(cognition.WorkflowBudgetInput{Plan: candidatePlan, Portfolio: req.Portfolio, BudgetStates: req.BudgetStates, AllowUnknownLocalCompute: req.AllowUnknownLocalCompute})
     If !budgetResult.Authorized:
       return fallbackToBaseline(req, "budget_unauthorized_model_plan")
  9. Return ModelPlanResult{Plan: candidatePlan, UsedModel: true}

fallbackToBaseline(req, reason):
  baselineResult, err := PlanWorkflow(PlanRequest{Task: req.Task, Portfolio: req.Portfolio, Policy: req.Policy})
  if err != nil || baselineResult.Plan == nil:
    return &ModelPlanResult{Plan: nil, Diagnostics: baselineResult.Diagnostics, UsedModel: false, FallbackReason: reason}, err
  budgetResult := cognition.AuthorizeWorkflowBudget(cognition.WorkflowBudgetInput{Plan: baselineResult.Plan, Portfolio: req.Portfolio, BudgetStates: req.BudgetStates, AllowUnknownLocalCompute: req.AllowUnknownLocalCompute})
  if !budgetResult.Authorized:
    return &ModelPlanResult{Plan: nil, Diagnostics: budgetResult.Diagnostics, UsedModel: false, FallbackReason: reason}, nil
  return &ModelPlanResult{Plan: baselineResult.Plan, Diagnostics: nil, UsedModel: false, FallbackReason: reason}, nil
```

## Authority matrix

| Decision/effect | Authorized source | Forbidden substitute |
| --- | --- | --- |
| Workflow stage topology | Deterministic Validator + Budget Authorizer | Unchecked model proposal |
| Plan identity & schema version | Go runtime | Model JSON fields |
| Fallback decision | Go runtime control flow | Model prompt instructions |

## Missing / unknown / stale input semantics

| Input/fact | Missing | Unknown | Stale | Malformed/contradictory |
| --- | --- | --- | --- | --- |
| `req.Invoker` | Degrades to baseline (2B) | n/a | n/a | n/a |
| `req.Portfolio` | Fail closed (`InvalidArgument`) | n/a | n/a | Fail closed |
| `req.BudgetStates` | Fail closed for non-local | Fail closed for non-local | Governed by authorizer | Governed by authorizer |
| Model JSON | Degrades to baseline (2B) | Degrades to baseline | Degrades to baseline | Degrades to baseline |

## Failure matrix

| Failure point / condition | Required postcondition | Recovery / response | Evidence |
| --- | --- | --- | --- |
| Invoker returns error (timeout, network) | Plan successfully produced via baseline | Fallback to deterministic baseline | `UsedModel: false, FallbackReason: "invoker_error: ..."` |
| Model emits invalid JSON / markdown noise | Plan successfully produced via baseline | Strip markdown fences or fallback to baseline | `UsedModel: false, FallbackReason: "malformed_model_output: ..."` |
| Model emits stages violating portfolio rules | Plan successfully produced via baseline | Validator rejects -> Fallback to baseline | `UsedModel: false, FallbackReason: "validator_rejected_model_plan"` |
| Model emits stages exceeding budget | Plan successfully produced via baseline | Budget authorizer rejects -> Fallback | `UsedModel: false, FallbackReason: "budget_unauthorized_model_plan"` |
| Context canceled during invocation | No plan produced, error returned | Returns `ctx.Err()` directly | `errors.Is(err, context.Canceled)` |

## Representability map

| Concept / requirement | Exact representation | Adequacy | Action if inadequate |
| --- | --- | --- | --- |
| Model Plan Request | `workflowplanner.ModelPlanRequest` | represented | — |
| Model Plan Result | `workflowplanner.ModelPlanResult` | represented | — |
| Workflow Plan | `protocol.WorkflowPlan` | represented | — |
| Gating results | `cognition.WorkflowValidationResult`, `cognition.WorkflowBudgetResult` | represented | — |

## Acceptance scenarios

| ID | Setup / precondition | Action | Expected result | Maps to |
| --- | --- | --- | --- | --- |
| ACC-01 | Eligible invoker returns valid multi-stage JSON; valid portfolio and budget | `PlanWorkflowWithModel` | Returns model plan, `UsedModel: true`, `FallbackReason: ""`, `WorkflowValidator.Valid == true` | REQ-01, REQ-06 |
| ACC-02 | `req.Invoker == nil` | `PlanWorkflowWithModel` | Returns baseline plan (WP-M3D-2B), `UsedModel: false`, `FallbackReason: "no_invoker_or_deterministic_only"` | REQ-03 |
| ACC-03 | Invoker returns raw error | `PlanWorkflowWithModel` | Falls back to baseline plan, `UsedModel: false`, `FallbackReason` contains `"invoker_error"` | REQ-03, REQ-06 |
| ACC-04 | Invoker returns malformed / unparseable JSON | `PlanWorkflowWithModel` | Falls back to baseline plan, `UsedModel: false`, `FallbackReason` contains `"malformed_model_output"` | REQ-05 |
| ACC-05 | Invoker returns stages with unbound role or duplicate orders | `PlanWorkflowWithModel` | Validator catches error, falls back to baseline plan | REQ-06, INV-01 |
| ACC-06 | Invoker returns stages referencing exhausted budget pool | `PlanWorkflowWithModel` | Budget authorizer rejects, falls back to baseline plan | REQ-06, INV-01 |
| ACC-07 | Task has `DeterministicOnly: true` | `PlanWorkflowWithModel` | Skips invoker, returns deterministic baseline directly | REQ-03 |
| ACC-08 | Context canceled before/during invocation | `PlanWorkflowWithModel` | Returns unwrapped `context.Canceled`, no fallback executed | REQ-02 |
| ACC-09 | Model proposes stages with dependencies (`DependsOn`) | `PlanWorkflowWithModel` | Go sanitizes stage IDs and remaps `DependsOn` successfully without validator rejection | REQ-07 |

## Validation

- command / deterministic check: `go test -v -race ./internal/cognition/workflowplanner/...`
- mutation testing: `mutation review sufficient: adversarial catalog below`
- mutation catalog:

| Mutant (Plausible Bug / Omission) | Expected Test Failure (Scenario / Check) |
| --- | --- |
| Accept model stages without running `WorkflowValidator` | ACC-05 fails (returns invalid model plan) |
| Accept model stages without running `AuthorizeWorkflowBudget` | ACC-06 fails (returns over-budget model plan) |
| Return error instead of falling back when invoker errors | ACC-03 fails (returns non-nil error instead of baseline plan) |
| Return error instead of falling back when model JSON is malformed | ACC-04 fails (returns non-nil error instead of baseline plan) |
| Invoke model when `DeterministicOnly: true` | ACC-07 fails (invoker mock records unexpected invocation) |
| Trust model-supplied `PlanID` or `TaskID` without overwriting | Invariant test fails |
| Sanitize stage IDs without remapping `DependsOn` | ACC-09 fails (validator rejects orphaned depends_on) |
| Catch `context.Canceled` as invoker error and trigger fallback | ACC-08 fails (returns plan instead of error) |

- required independent review lenses:
  - Contract & Authority Reviewer: verifies requirements REQ-*, invariants INV-*, boundaries, and fail-closed security.
  - Test Adequacy & Mutation Reviewer: verifies coverage of ACC-*, checks edge cases, and kills all cataloged mutants.
- evidence to capture: test logs, coverage report, git diff.

## Escalation triggers

- Unresolvable schema mismatch in `WorkflowPlan`.
- Need to import `drivers` package into `workflowplanner`.
- Context deadline or cancellation handling conflicts.

## Implementation Readiness Report

```text
requirements represented: 8/8
mandatory clauses resolved: 4/4
state transitions specified: 3/3
failure cases specified: 5/5
authority decisions specified: 3/3
missing/unknown input semantics: 4/4
acceptance scenarios mapped: 9/9
unresolved architecture choices: 0
declared local-discretion choices: 2
readiness: READY_FOR_IMPLEMENTATION
```

### Weaker-implementer check

Could a competent implementation model with good language/repository skill but mediocre architectural judgment execute this EWP without inventing important semantics?

- [x] Yes
- [ ] No — return to Principal design
