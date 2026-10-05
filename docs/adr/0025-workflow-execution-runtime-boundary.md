# ADR-0025: Workflow Execution Runtime Boundary

- **Status:** Proposed
- **Date:** 2026-10-04
- **Decision owner:** Human / Principal
- **Supersedes:** none
- **Superseded by:** none
- **Related:** ADR-0004, ADR-0018, ADR-0019, ADR-0020, ADR-0024
- **Related invariants:** DCI-123, DCI-124, DCI-125, DCI-131, DCI-159, DCI-160, DCI-161
- **Related tasks:** M4, M7, M9, M10

## Context

DevCadence already separates engineering policy and cognition routing from provider-specific model execution. `WorkflowPlan` describes a task-specific workflow, `SessionDriver` normalizes one cognition session, and canonical project state, evidence, review and acceptance live outside model conversations.

M4 is where DevCadence begins to materialize reusable workflow execution. The architectural risk is not that the native executor will be too simple; it is that its scheduling details could accidentally become the meaning of `WorkflowPlan` and ProjectState. If that happens, a later alternative execution runtime would require changes to engineering semantics rather than merely a different execution mechanism.

A future external cognitive runtime such as COS is one motivating example, but this ADR does not design for COS. The durable requirement is narrower: DevCadence engineering contracts must remain independent of one executor's private scheduling, decomposition, memory or placement strategy.

## Decision

### 1. WorkflowPlan is the logical engineering contract, not an execution trace

A `WorkflowPlan` describes policy-significant engineering obligations and ordering constraints that every executor must preserve.

An executor may internally decompose work, run private subtasks, retry internal actions, wait for events, retain private working state, or use another scheduling strategy. Those operations do not become canonical DevCadence stages merely because one executor uses them.

Conversely, private execution strategy cannot erase DevCadence obligations. Deterministic gates, budget/source-exposure limits, review requirements, explicit bindings, and acceptance authority remain enforceable by DevCadence.

### 2. Workflow execution is a responsibility above SessionDriver

The responsibility boundary is:

```text
DevCadence policy / EWP / WorkflowPlan / evidence / acceptance
                         |
                         v
                 workflow execution
                    boundary
                  /          \
                 /            \
       native executor      alternate executor
             |                   |
      SessionDrivers        private execution strategy
      tools / validators
```

`SessionDriver` remains an abstraction for one cognition endpoint/session. It is not the abstraction for an entire workflow.

This ADR intentionally does **not** define a Go `WorkflowExecutor` interface. M4 should extract the smallest concrete seam from demonstrated native execution needs rather than turn an illustrative API into a de facto protocol.

### 3. WorkflowPlan field semantics

Existing `WorkflowPlan` / `WorkflowStage` fields fall into distinct semantic classes. This classification prevents a native scheduler from treating every field as private scheduler state, while also preventing incidental implementation details from becoming workflow authority.

| Field | Classification | Semantics |
| --- | --- | --- |
| `PlanID`, `TaskID`, `WorkPackageID` | binding identity | Identify the authorized logical plan/task/work package revision context. |
| `Topology` | binding workflow obligation | Constrains the logical workflow shape and topology-specific validation requirements. |
| `StageID` | binding logical identity | Stable identity of a logical stage; must not be replaced by process/goroutine/worker identity. |
| `Role`, `Kind` | binding obligation | Define the logical role and cognition-vs-deterministic responsibility of the stage. |
| `IsReview` | binding review marker | Marks a review stage. Independence is established by topology plus deterministic validation/portfolio constraints, not by this flag alone. |
| `DependsOn` | binding precedence | A stage may not logically complete before its declared dependencies. Executors may run independent stages concurrently. |
| `Order` | deterministic plan ordering, not serial scheduling | Provides stable total ordering and constrains dependency references to earlier stages. Absent a dependency or other binding obligation, lower `Order` does **not** require one stage to finish before a higher-`Order` stage starts. |
| `BudgetPoolID` | binding authority/resource constraint | Charges/authorizes the logical stage against the specified budget pool. |
| `TimeoutSeconds` | binding logical-stage bound | Bounds the logical stage execution/attempt as defined by DevCadence; it is not automatically a timeout for every runtime-private subtask. |
| `RetryLimit` | binding logical-stage bound | Caps DevCadence-authorized retries of the logical stage; private runtime operations cannot use it to silently expand authorized retries. |
| `EndpointID`, `ChannelID`, `ContextProfileID` | optional binding when present | If populated, the stage is bound to those authorized resources/profile. If absent, an executor may resolve eligible resources under the validated portfolio/policy. |
| `EscalationTarget` | optional binding when present | Constrains an authorized logical escalation path; it does not prescribe a runtime-private subtask graph. |
| `DeterministicGateID` | binding verification obligation | Identifies the deterministic gate that DevCadence must execute/verify for a deterministic stage. |

This table describes the current protocol; changing a field from binding to advisory (or the reverse) requires an explicit protocol decision rather than an executor-specific interpretation.

### 4. DevCadence authority remains outside the executor

Executor-private state is non-authoritative. It may consume revision-pinned DevCadence facts and artifacts and may return candidate outputs/evidence, but it cannot directly:

- mutate accepted project truth;
- weaken policy, budgets, source exposure or tool authority;
- satisfy or close independent review by assertion;
- replace deterministic validation results;
- establish acceptance.

Policy-significant lifecycle and evidence outcomes cross the boundary through DevCadence-governed records and checks.

An alternate executor is not required to expose every internal subtask or model call as a DevCadence stage. It must, however, stay within the authority envelope granted for the logical work and return the evidence/provenance/usage that DevCadence requires to evaluate the contract.

### 5. Execution placement is not workflow authority

A logical DevCadence task or stage does not gain engineering meaning or authority from the process, host, node, scheduler instance or worker that happens to execute it unless an explicit locality/security/tool constraint is part of the task contract.

The native executor may remain single-process and single-host. A future executor may use different placement internally without changing `WorkflowPlan` semantics. Any concrete distributed integration must separately solve its own ownership, duplicate-execution, source/artifact locality, credential, recovery and consistency requirements; this ADR does not pre-design those mechanisms.

### 6. The native executor stays deliberately simple

The native DevCadence executor is a reference implementation needed to make DevCadence work, not a general cognitive runtime.

Its default design target is:

- one DevCadence control-plane instance;
- single-node/process-local execution unless a current requirement proves otherwise;
- direct execution of ready logical stages;
- ordinary dependency checks;
- reuse of existing `SessionDriver`, process, worktree and validator machinery;
- simple bounded concurrency where useful;
- explicit completion, failure and cancellation.

Do not add a plugin framework, generic event fabric, hierarchical blackboard, distributed task store, leases, consensus, recursive planner machinery or other speculative runtime infrastructure solely to preserve future substitutability.

### 7. Extract the concrete seam from M4 evidence

M4 should keep workflow execution behind one narrow internal entry point, but the exact executable interface must be derived from real native orchestration requirements.

A useful architectural test is that a second/fake executor can satisfy the same logical plan while scheduling independent stages differently (for example asynchronously or out of total-order execution) without requiring changes to EWP, policy, ProjectState, evidence or acceptance semantics.

If that test exposes native-scheduler assumptions, fix the semantic leak rather than expanding the stable protocol to mirror the native executor.

## Consequences

### Positive

- native scheduling details do not become stable workflow semantics;
- an alternate runtime can later execute the same engineering contracts;
- `SessionDriver` remains reusable and correctly scoped to individual cognition sessions;
- DevCadence retains deterministic authority over policy, evidence, review and acceptance;
- the native implementation can remain intentionally small.

### Negative

- the architecture must maintain a real distinction between logical lifecycle and executor-private lifecycle;
- some future M4 lifecycle records may need explicit classification as canonical versus executor-private;
- alternate executors may need adapters for DevCadence context, artifact, evidence and authority envelopes.

### Risks and mitigations

- **Leaky abstraction:** executor details creep into `WorkflowPlan`. Mitigation: DCI-159 and field classification above.
- **Shadow authority:** executor-private state is treated as accepted project truth. Mitigation: DCI-160 and DevCadence-owned evidence/acceptance.
- **Double scheduling:** DevCadence and an alternate runtime both attempt to own the same private scheduling decision. Mitigation: DevCadence owns logical legality/obligations; the executor owns private execution strategy inside that envelope.
- **Premature distribution tax:** future placement possibilities distort the native executor. Mitigation: DCI-161 and the deliberately simple native target.

## Implementation guidance

### Now

- preserve DCI-159–161;
- keep `WorkflowPlan` logical according to the field classification above;
- keep `SessionDriver` below workflow execution;
- do not introduce a generalized executor/plugin framework solely for future compatibility.

### When M4 materializes execution

- build the smallest native executor sufficient for the evidence-gate workload;
- use stable logical stage/attempt identity rather than process/goroutine identity;
- keep DevCadence-owned deterministic gates, evidence and acceptance outside executor assertions;
- allow independent logical stages to execute in any order consistent with binding dependencies/constraints;
- add a cheap alternate/fake-executor test if it helps expose native-scheduler assumptions;
- extract an interface only when the native implementation demonstrates the required operations and failure semantics.

### When a real alternate runtime exists

Define an adapter from the demonstrated execution boundary. Add only cross-runtime semantics proven necessary by that integration. Runtime-specific task graphs, memory systems, wake mechanisms, worker kinds or distributed placement remain private unless a separate DevCadence requirement makes them policy-significant.

## Verification

Review should be able to answer each concern with one owner:

- engineering legality, policy, evidence and acceptance -> DevCadence;
- private scheduling/decomposition/working state -> executor;
- individual model/session access -> `SessionDriver` for the native path;
- explicit plan bindings/limits -> preserved by every executor.

A representative `WorkflowPlan` should be executable by a native reference executor and by a deliberately different fake executor without changing the plan's engineering meaning.

## Rollback / supersession

If no meaningful alternate runtime emerges and the boundary adds unjustified implementation complexity, the executable seam may remain internal or collapse into the native executor. The semantic distinction that `WorkflowPlan` is not an execution trace remains useful independently.

If a real integration proves that additional cross-runtime semantics are necessary, amend this ADR from demonstrated requirements rather than preemptively modeling the external runtime.
