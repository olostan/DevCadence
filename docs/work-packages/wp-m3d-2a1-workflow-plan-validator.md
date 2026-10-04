# WP-M3D-2A1 — Workflow plan validator (structure, bindings, bounds)

## Identity

- Work Package ID: WP-M3D-2A1 (window 2026-10-A; first slice of WP-M3D-2 "Workflow topology planner")
- Revision: 2 (window review probe: READY_WITH_FIXES; behavior-bearing findings 1,2,4,5 and wording findings fixed; contract implemented in a scratch copy, suite green)
- Base commit: current `origin/main` at implementation time (the implementor records the SHA)
- Status: READY_FOR_IMPLEMENTATION (r2)

## Objective

`WorkflowPlan` (protocol) has no consumer and its own `Validate` checks only internal shape. Before any planner (deterministic or AI, M3D-2B/2C) can propose plans, the deterministic authority that decides whether a plan is usable must exist: a pure validator that checks a plan against the active portfolio and explicit bounds. "AI proposes. Deterministic machinery authorizes." (DCI-123/124/125, FR-066, PROTOCOLS §3C "bounded per-task topology"). Budget and metered-pool checks against live state are WP-M3D-2A2, not here.

## Context Manifest

- read-authority: `internal/protocol/portfolio.go` (WorkflowPlan, WorkflowStage, RoleBinding, FallbackBinding, BudgetPool, WorkflowDefaults), `internal/cognition/portfolio_diagnostics.go` and `portfolio_validator.go` (diagnostic type, sorting, digest helpers)
- risk tags: authorization boundary, determinism, fail-closed bounds
- normative clauses: DCI-123, DCI-124, DCI-125, FR-066, PROTOCOLS §3C
- re-resolution triggers: any need to change `protocol` types/schemas, the existing portfolio validator, or to read budget/resource state

## Verified facts (the implementor re-verifies each as step 0 and reports any false row)

| ID | Claim | Evidence |
| --- | --- | --- |
| F-01 | `WorkflowPlan{SchemaVersion, PlanID, TaskID, WorkPackageID, Topology, Stages}` and `WorkflowStage{StageID, Role, Kind, IsReview, Order, DependsOn, BudgetPoolID, TimeoutSeconds, EndpointID, ChannelID, ContextProfileID (pointers), RetryLimit, EscalationTarget (pointer), DeterministicGateID (pointer)}` | `internal/protocol/portfolio.go` |
| F-02 | `WorkflowPlan.Validate` already checks: schema version; non-empty ids; valid topology; at least one stage; per-stage validation; unique stage ids and orders; `DependsOn` references existing stages with strictly lower order; dual-review needs two review stages on distinct endpoints (or distinct roles when an endpoint is nil); deterministic-only needs every stage deterministic | same |
| F-03 | It does NOT check: stage count, retry or timeout bounds, that stage roles/endpoints/pools exist in any portfolio, that `EscalationTarget` resolves, or that review stages are cognition stages | same |
| F-04 | `CognitionPortfolio.RoleBindings` entries carry `Role, EndpointID, ChannelID, BudgetPoolID, ContextProfileID, Priority, Fallbacks`, and each fallback carries `EndpointID, ChannelID, BudgetPoolID, ContextProfileID`; `CognitionPortfolio.WorkflowDefaults` is optional with `DefaultTopology, DefaultTimeoutSeconds, MaxRetries` | same |
| F-05 | `PortfolioDiagnostic{Code, Condition, Target, ViolatedRule, Message, Observed, Required}`, `SortedDiagnostics`, and the `Condition*` constants exist in package `cognition`; digests are `"sha256:" + hex(sha256(protocol.CanonicalJSON(x)))` | `portfolio_diagnostics.go`, `portfolio_validator.go` |
| F-06 | Nothing in the repository consumes `WorkflowPlan` or `WorkflowDefaults` besides their own Validate and tests | scout report |

## Semantic scope envelope

### Authorized domains

- NEW files in `internal/cognition/`: `workflow_validator.go`, `workflow_validator_test.go` (and further `workflow_validator_*_test.go` if wanted), plus new diagnostic code constants in a new file `workflow_diagnostics.go`
- `docs/COGNITION_PORTFOLIO.md` (a short subsection describing the workflow validator), `docs/WORK_PACKAGES.md` (M3D-2 split note: 2A1 done; 2A2 budget/metered, 2B deterministic planner, 2C AI planner remain)

### Forbidden

- No change to protocol types, schemas, fixtures, the portfolio validator, activation, planner, or drivers. No clock, I/O, goroutines or global state. No reading of `BudgetStates`/`ResourceStates` (2A2).
- The validator MUST NOT mutate its inputs.

### LOCAL_DISCRETION

Helper names, file splits inside the new files, test layout, message wording.

## Requirements

| ID | Strength | Requirement |
| --- | --- | --- |
| REQ-01 | MUST | API (names exact): `type WorkflowPolicy struct { MaxStages int `json:"max_stages"`; MaxTotalRetries int `json:"max_total_retries"`; MaxStageTimeoutSeconds int `json:"max_stage_timeout_seconds"` }`; `func DefaultWorkflowPolicy() WorkflowPolicy` returning `{MaxStages: 8, MaxTotalRetries: 6, MaxStageTimeoutSeconds: 3600}`; `type WorkflowValidationInput struct { Plan *protocol.WorkflowPlan; Portfolio *protocol.CognitionPortfolio; Policy *WorkflowPolicy }`; `type WorkflowValidationResult struct { Valid bool; Diagnostics []PortfolioDiagnostic; PlanDigest string; PortfolioDigest string; PolicyDigest string }`; `type WorkflowValidator struct{}`; `func NewWorkflowValidator() WorkflowValidator`; `func (WorkflowValidator) Validate(in WorkflowValidationInput) WorkflowValidationResult`. |
| REQ-02 | MUST | New diagnostic code constants (strings exact): `CodeWorkflowInputMissing = "WORKFLOW_INPUT_MISSING"`, `CodeWorkflowPolicyInvalid = "WORKFLOW_POLICY_INVALID"`, `CodeWorkflowPlanInvalid = "WORKFLOW_PLAN_INVALID"`, `CodeWorkflowPortfolioInvalid = "WORKFLOW_PORTFOLIO_INVALID"`, `CodeWorkflowUnboundedStages = "WORKFLOW_UNBOUNDED_STAGES"`, `CodeWorkflowRetryBound = "WORKFLOW_RETRY_BOUND"`, `CodeWorkflowTimeoutBound = "WORKFLOW_TIMEOUT_BOUND"`, `CodeWorkflowRoleUnbound = "WORKFLOW_ROLE_UNBOUND"`, `CodeWorkflowBindingMismatch = "WORKFLOW_BINDING_MISMATCH"`, `CodeWorkflowUnknownBudgetPool = "WORKFLOW_UNKNOWN_BUDGET_POOL"`, `CodeWorkflowEscalationTarget = "WORKFLOW_ESCALATION_TARGET"`, `CodeWorkflowReviewNotCognition = "WORKFLOW_REVIEW_NOT_COGNITION"`. |
| REQ-03 | MUST | Evaluation order and stop rules: (R0) `Plan == nil` or `Portfolio == nil` ⇒ ONE diagnostic `WORKFLOW_INPUT_MISSING` (Target `plan` when the plan is nil — including when BOTH are nil — otherwise `portfolio`; Condition `ConditionInvalid`), `Valid=false`, STOP. (R0b) an explicit `Policy` with any field `<= 0` ⇒ ONE diagnostic `WORKFLOW_POLICY_INVALID` (Target `policy`), STOP. Effective policy = `*Policy` or `DefaultWorkflowPolicy()` (no merging of defaults into an explicit policy). (R1) `Plan.Validate()` error ⇒ ONE `WORKFLOW_PLAN_INVALID` (Message = the error text truncated to 256 bytes at a rune boundary), STOP. (R1b) `Portfolio.Validate()` error ⇒ ONE `WORKFLOW_PORTFOLIO_INVALID` (same truncation), STOP. On every stop (R0, R0b, R1, R1b) all three digests are EMPTY. Only then rules R2-R7 run, all of them, collecting every diagnostic. |
| REQ-04 | MUST | (R2) `len(Stages) > MaxStages` ⇒ one `WORKFLOW_UNBOUNDED_STAGES`, Target `stages`, Observed the count, Required `<= <MaxStages>`. Exactly `MaxStages` stages is valid. |
| REQ-05 | MUST | (R3) per-stage retry cap `c = Portfolio.WorkflowDefaults.MaxRetries` when `WorkflowDefaults != nil` AND `MaxRetries > 0`, else `MaxTotalRetries` (`max_retries` is `omitempty`, so 0 cannot be told apart from unset and means unset; a portfolio therefore cannot express "no retries" through its defaults — recorded limit). `c` may exceed `MaxTotalRetries`; the aggregate check is independent. A stage with `RetryLimit > c` ⇒ one `WORKFLOW_RETRY_BOUND` with Target `stages[<i>]`. If the sum of all `RetryLimit` exceeds `MaxTotalRetries` ⇒ one aggregate `WORKFLOW_RETRY_BOUND` with Target `stages`. (R3b) a stage with `TimeoutSeconds > MaxStageTimeoutSeconds` ⇒ one `WORKFLOW_TIMEOUT_BOUND`, Target `stages[<i>]`. `WorkflowDefaults.DefaultTimeoutSeconds` is a default, not a cap, and is not enforced. |
| REQ-06 | MUST | (R4) for each stage with `Kind == cognition`: the stage `Role` MUST equal the `Role` of at least one `Portfolio.RoleBindings` entry, else `WORKFLOW_ROLE_UNBOUND` (Target `stages[<i>]`) and no binding checks for that stage. Candidates for that role are the binding's primary tuple and each of its fallback tuples, each tuple being `(EndpointID, ChannelID, ContextProfileID, BudgetPoolID)`; with several bindings for one role, all their tuples are candidates. A tuple matches the stage when every PROVIDED (non-nil) stage field among `EndpointID`, `ChannelID`, `ContextProfileID` equals the tuple's field AND the stage `BudgetPoolID` equals the tuple's `BudgetPoolID`. If no candidate matches ⇒ one `WORKFLOW_BINDING_MISMATCH` (Target `stages[<i>]`). Comparison is exact string equality. |
| REQ-07 | MUST | (R5) for EVERY stage (cognition or deterministic): `BudgetPoolID` MUST equal the `PoolID` of some `Portfolio.BudgetPools` entry, else `WORKFLOW_UNKNOWN_BUDGET_POOL` (Target `stages[<i>]`). For a cognition stage whose pool is unknown, REQ-06's match necessarily fails too; emit BOTH diagnostics (they have different codes). |
| REQ-08 | MUST | (R6) a stage with non-nil `EscalationTarget` MUST name the `StageID` of an existing stage whose `Order` is strictly greater than this stage's `Order`, else `WORKFLOW_ESCALATION_TARGET` (Target `stages[<i>]`). (R7) a stage with `IsReview == true` and `Kind != cognition` ⇒ `WORKFLOW_REVIEW_NOT_COGNITION` (Target `stages[<i>]`). |
| REQ-09 | MUST | Diagnostic fields per code (`Message` is free text; for R1/R1b it is the error text truncated to 256 bytes at a rune boundary without ellipsis): INPUT_MISSING, POLICY_INVALID, PLAN_INVALID, PORTFOLIO_INVALID: Condition `ConditionInvalid`, ViolatedRule `DCI-123`, Observed/Required empty. R2 UNBOUNDED_STAGES: `ConditionOverBudget`, `FR-066`, Observed = the stage count, Required = `<= <MaxStages>`. R3 per-stage RETRY_BOUND: `ConditionOverBudget`, `FR-066`, Observed = the stage `RetryLimit`, Required = `<= <c>`; aggregate RETRY_BOUND: Observed = the sum, Required = `<= <MaxTotalRetries>`. R3b TIMEOUT_BOUND: `ConditionOverBudget`, `FR-066`, Observed = the stage `TimeoutSeconds`, Required = `<= <MaxStageTimeoutSeconds>`. R4 ROLE_UNBOUND and BINDING_MISMATCH and R5 UNKNOWN_BUDGET_POOL: `ConditionUnauthorized`, `DCI-123`, Observed = the stage role (ROLE_UNBOUND), `"<endpoint>/<channel>/<profile>/<pool>"` with `-` for nil pointers (BINDING_MISMATCH), the stage pool id (UNKNOWN_BUDGET_POOL); Required empty. R6 ESCALATION_TARGET: `ConditionInvalid`, `DCI-123`, Observed = the target stage id, Required empty. R7 REVIEW_NOT_COGNITION: `ConditionInvalid`, `DCI-123`, Observed = the stage kind. Ordering is `SortedDiagnostics` verbatim (lexical Target, then Code; do NOT re-sort numerically, so `stages[10]` sorts before `stages[2]`). |
| REQ-10 | MUST | `Valid == (len(Diagnostics) == 0)`. Digests: `PlanDigest` and `PortfolioDigest` are `"sha256:" + hex(sha256(protocol.CanonicalJSON(x)))` of the inputs; `PolicyDigest` of the effective policy; set only when evaluation reaches R2 (empty on every R0/R0b/R1/R1b stop). No timestamp field exists (pure function). |
| REQ-11 | MUST | Pure and deterministic: no clock, I/O, goroutines or global state; inputs are not mutated; same input ⇒ identical result. |
| REQ-12 | MUST | Docs per the envelope. |

## Invariants

| ID | Statement |
| --- | --- |
| INV-01 | A plan with `Valid == true` references only roles, endpoints, channels, context profiles and budget pools that exist in the given portfolio, within the stated bounds. |
| INV-02 | An explicit policy is never silently completed with defaults. |
| INV-03 | The validator reads no live state (budget or resource), so its verdict depends only on the plan, the portfolio and the policy. |

## Authority matrix

| Decision | Authorized source | Forbidden substitute |
| --- | --- | --- |
| whether a plan is usable | `WorkflowValidator` under the caller's policy | the plan's own topology label, any model confidence |
| the bounds | `WorkflowPolicy` (defaults documented) and the portfolio's `WorkflowDefaults.MaxRetries` | stage-level self-declared limits |

## Missing / unknown input semantics

| Input | Missing | Unknown | Stale | Malformed |
| --- | --- | --- | --- | --- |
| plan / portfolio | `WORKFLOW_INPUT_MISSING`, stop | n/a | staleness vs the activation record is the activation manager's job | plan or portfolio own-Validate failure ⇒ `WORKFLOW_PLAN_INVALID` / `WORKFLOW_PORTFOLIO_INVALID`, stop |
| policy | nil ⇒ defaults | n/a | n/a | any field `<= 0` ⇒ `WORKFLOW_POLICY_INVALID`, stop |
| `Portfolio.WorkflowDefaults` | nil ⇒ per-stage cap is `MaxTotalRetries` | n/a | n/a | n/a |
| stage endpoint/channel/profile pointers | nil ⇒ not compared | n/a | n/a | n/a |

## Failure matrix

| Condition | Required postcondition | Evidence |
| --- | --- | --- |
| nil plan / nil portfolio | one INPUT_MISSING, stop | ACC-02 |
| explicit policy with a zero field | one POLICY_INVALID, stop | ACC-03 |
| plan fails its own Validate | one PLAN_INVALID, stop, no further diagnostics | ACC-04 |
| portfolio fails its own Validate | one PORTFOLIO_INVALID, stop | ACC-04 |
| 9 stages (default policy) | UNBOUNDED_STAGES; 8 stages valid | ACC-05 |
| retry/timeout over caps | per-stage and aggregate diagnostics | ACC-06 |
| role without a binding | ROLE_UNBOUND, no binding check | ACC-07 |
| provided endpoint/channel/profile or pool matching no candidate | BINDING_MISMATCH | ACC-08 |
| unknown pool id | UNKNOWN_BUDGET_POOL (and BINDING_MISMATCH for cognition stages) | ACC-09 |
| escalation target missing, self, or lower order | ESCALATION_TARGET | ACC-10 |
| review stage that is deterministic | REVIEW_NOT_COGNITION | ACC-11 |

## Representability map

| Concept | Representation | Adequacy |
| --- | --- | --- |
| plan-to-portfolio binding | stage endpoint/channel/profile/pool vs role binding tuples | represented |
| bounds | `WorkflowPolicy` + `WorkflowDefaults.MaxRetries` | represented; numeric defaults are documented policy values (owner may change) |
| plan provenance / rationale / source portfolio id | NOT represented on `WorkflowPlan` | not needed by this validator; recorded gap for M3D-2B/2C |
| live budget and quota | not read | WP-M3D-2A2 |

## Acceptance scenarios

Test names MUST contain `WorkflowValidator` and the ACC id (one top-level test per ACC; subtests may group cases). Build fixtures from the portfolio test builders pattern (copy the minimal builders needed into the new test file; roles `scout` and `implementer`, endpoints `ep-local-01` and `ep-cli-01`, with a fallback on one binding).

| ID | Setup | Action | Expected |
| --- | --- | --- | --- |
| ACC-01 | valid `single_pass` plan on a bound role; a valid `dual_independent_review` plan with two `IsReview` cognition stages (roles `scout` and `implementer`; endpoints nil, or distinct: scout on `ep-local-01`/`pool-local`, implementer on its fallback `ep-cli-01`/`pool-sub`); a `deterministic_only` plan | `Validate` | `Valid == true`, no diagnostics, all three digests set |
| ACC-02 | nil plan; nil portfolio | `Validate` | one INPUT_MISSING each; no panic |
| ACC-03 | explicit policy `{0,6,3600}`, `{8,0,3600}`, `{8,6,-1}` | `Validate` | one POLICY_INVALID each |
| ACC-04 | plan with duplicate stage ids (fails own Validate) AND at least one extra violation R2-R7 would report if evaluation continued (e.g. an unknown budget pool); a portfolio with an invalid role binding AND a plan that would also violate R4 | `Validate` | one PLAN_INVALID / one PORTFOLIO_INVALID; nothing else; all three digests empty; `Valid == false` |
| ACC-05 | 8 stages, then 9 stages (default policy); explicit `MaxStages: 3` with 4 stages | `Validate` | 8 valid; 9 and 4 give UNBOUNDED_STAGES with Observed/Required |
| ACC-06 | `WorkflowDefaults.MaxRetries = 1` and a stage with `RetryLimit 2`; `WorkflowDefaults` present with `MaxRetries` 0 (cap falls back to `MaxTotalRetries`, so `RetryLimit 3` is valid); no defaults and per-stage 7 with policy default; sum of retries 7 over cap 6; stage timeout 3601 | `Validate` | per-stage RETRY_BOUND (Target `stages[<i>]`), aggregate RETRY_BOUND (Target `stages`), TIMEOUT_BOUND |
| ACC-07 | cognition stage with a role absent from all bindings | `Validate` | ROLE_UNBOUND only for that stage |
| ACC-08 | stage with provided endpoint matching a fallback tuple (valid); with endpoint of one tuple and channel of another (mismatch); stage pool differing from the matched tuple's pool | `Validate` | match valid; the others BINDING_MISMATCH |
| ACC-09 | stage pool id not in `BudgetPools`, cognition and deterministic variants | `Validate` | UNKNOWN_BUDGET_POOL for both; BINDING_MISMATCH additionally for the cognition one |
| ACC-10 | escalation target unknown; equal to own stage id; lower order; higher order | `Validate` | first three ESCALATION_TARGET; last valid |
| ACC-11 | `IsReview` stage with `Kind` deterministic (otherwise valid for the topology) | `Validate` | REVIEW_NOT_COGNITION |
| ACC-12 | one input with several violations that include an aggregate target (`stages`) and per-stage targets (including a two-digit index such as `stages[10]`, which needs 11 stages and a policy `MaxStages` of 12), produced in a non-sorted generation order | `Validate` | all collected and ordered by `SortedDiagnostics`; repeated 20 times ⇒ identical; every ACC-02..11 scenario asserts `Valid == false` and the passing scenarios `Valid == true` |
| ACC-13 | deep-copy inputs before (the plan's stages given in NON-ascending `Order` in the slice), compare after; digests of two equal inputs | `Validate` | inputs unchanged (stage slice order intact); digests equal; digest of an explicit-default policy equals the nil-policy digest |
| ACC-14 | a stage list containing a nil-pointer `EndpointID`, `ChannelID`, `ContextProfileID` | `Validate` | not compared (valid when role and pool match) |
| ACC-15 | docs | read | describe the validator, defaults and the 2A2/2B/2C split |

## Interface / algorithm contract

REQ-01 through REQ-11 are the contract. Evaluation order: R0, R0b, R1, R1b (each a STOP), then R2, R3, R3b, R4, R5, R6, R7 over every stage in slice order, collecting all diagnostics; then `SortedDiagnostics`; then `Valid` and digests. Stage kind constants are `protocol.StageKindCognition` and `protocol.StageKindDeterministic`; digests use the package-private `hashBytes` helper (in-package, accessible).

### Weaker-implementer check

A reviewer implemented REQ-01..11 in a scratch copy with only existing builders (`makeTestPortfolio`: roles `implementer` with primary `ep-local-01`/`chan-local-01`/`pool-local`/`prof-local-01` and fallback `ep-cli-01`/`chan-cli-01`/`pool-sub`/`prof-cli-01`, and `scout` on `ep-local-01`) and the suite was green.

## Validation

- build, vet, gofmt on changed files; `go test -count=1 ./...`; `go test -race -count=1 ./internal/cognition/... ./tests/...`; `make docs-check`; hooks, no bypass
- mutation catalog (mutant ⇒ ACC that must fail): stage cap `>=` instead of `>` ⇒ ACC-05; cap checked against the wrong policy field ⇒ ACC-05/06; ignore `WorkflowDefaults.MaxRetries` ⇒ ACC-06; drop the aggregate retry check ⇒ ACC-06; drop timeout check ⇒ ACC-06; role check skipped ⇒ ACC-07; binding match ignoring pool ⇒ ACC-08; match only primary tuples (skip fallbacks) ⇒ ACC-08; match fields independently across different tuples ⇒ ACC-08 (mixed tuple case); compare provided pointers as required (nil treated as mismatch) ⇒ ACC-14; unknown-pool check only for cognition stages ⇒ ACC-09; escalation order `>=` ⇒ ACC-10; self target accepted ⇒ ACC-10; review check dropped ⇒ ACC-11; do not stop after PLAN_INVALID ⇒ ACC-04; merge defaults into an explicit policy ⇒ ACC-03/13; `Valid` computed while ignoring one diagnostic code ⇒ the per-scenario `Valid` assertions of ACC-02..11; mutate input (e.g. sort stages in place) ⇒ ACC-13; nondeterministic order (unsorted) ⇒ ACC-12
- required review lenses: contract/authority; mutation

## Escalation triggers

A required semantic cannot be expressed with the current protocol types; the existing `PortfolioDiagnostic`/`SortedDiagnostics` cannot be reused as is; any need to read budget/resource state.

## Design / rationale

D-1 Validator before planner: the authority boundary must exist and be tested before anything proposes plans; it also makes the later deterministic baseline and AI planner testable against one oracle. D-2 Reuse `PortfolioDiagnostic` so tooling and explanations stay uniform. D-3 Stop rules: a structurally invalid plan or portfolio makes binding diagnostics meaningless, so evaluation stops with one diagnostic. D-4 `WorkflowDefaults.MaxRetries` is treated as a cap (the only sensible reading of "max"), `DefaultTimeoutSeconds` is not (a default); numeric policy defaults (8 stages, 6 retries, 3600 s) are conservative documented choices an owner may change. D-5 Slice split: live budget/metered authorization is a separate WP (2A2) because it needs the H2 unknown-state rules and metered-fallback semantics to be restated for stages.

Known limits: provenance, rationale and source-portfolio linkage are not representable on `WorkflowPlan` today; per-endpoint quota state does not exist (host-level `ResourceState` only).

## Implementation Readiness Report

```text
requirements represented: 12/12
mandatory clauses resolved: 5/5
failure cases specified: 11/11
authority decisions specified: 2/2
missing/unknown input semantics: 5/5
acceptance scenarios mapped: 15/15
unresolved architecture choices: 0
readiness: READY_FOR_IMPLEMENTATION (window review r1: implementability probe passed, findings incorporated)
```
