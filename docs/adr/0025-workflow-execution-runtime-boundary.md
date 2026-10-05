# ADR-0025: Workflow Execution Runtime Boundary and Knowledge-Driven Activation

- **Status:** Accepted
- **Date:** 2026-10-04
- **Decision owner:** Human / Principal
- **Supersedes:** none
- **Superseded by:** none
- **Related:** ADR-0004, ADR-0016, ADR-0018, ADR-0019, ADR-0020, ADR-0024
- **Related invariants:** DCI-123, DCI-124, DCI-125, DCI-131, DCI-159, DCI-160, DCI-161
- **Related tasks:** M4, M7, M9, M10

## Context

DevCadence already separates engineering policy and cognition routing from provider-specific model execution. `WorkflowPlan` is a typed logical workflow; `SessionDriver` normalizes individual cognition sessions across local runtimes, authenticated CLIs and APIs; canonical project state, evidence, review and acceptance live outside model conversations.

A future Cognitive Orchestration System (COS), authored independently from DevCadence, is a materially different kind of runtime. It is not merely another model endpoint or `SessionDriver`. Its useful responsibilities include two mechanisms that sit above an individual inference session:

1. **Activation and suspension:** deciding when a runtime task should execute, sleep, resume or react to a changed condition, rather than requiring a model loop to poll continuously.
2. **Hierarchical knowledge scopes:** maintaining task-local and ancestor-derived knowledge/blackboard state, with references to facts and artifacts, so runtime tasks can share relevant knowledge without flattening all state into one conversational context. A sensory/event mechanism may wake suspended tasks when matching knowledge facts or signals appear.

The native DevCadence implementation does not need those mechanisms today. Designing COS itself into DevCadence now would be speculative coupling. However, allowing the native scheduler/executor to become the *definition* of DevCadence workflow semantics would make a later COS-backed implementation require unnecessary refactoring and would weaken an important future experiment: execute the same DevCadence engineering contracts through the native runtime and through COS, then compare quality, scarce-resource use, latency and human intervention.

This ADR therefore introduces one narrow dependency-inversion boundary without requiring an external runtime, a plugin system, receptors/ligands, hierarchical blackboards, or distributed scheduling in the current implementation.

## Verified facts

- `WorkflowPlan` already describes logical stages, dependencies, role, budget, timeout, retry and optional endpoint/channel/context binding.
- Workflow endpoint/channel bindings are not mandatory for every stage.
- `SessionDriver` is an individual cognition-session abstraction, not a workflow-orchestration abstraction.
- Canonical ProjectState, task state, evidence and review state are already control-plane concepts independent of one model conversation.
- ADR-0018 explicitly allows workflow topology to collapse or expand according to task and resources rather than imposing one fixed agent pipeline.
- ADR-0019/0020 already treat durable state and evidence as external memory and compile bounded task-specific working sets.

## Decision criteria

- preserve current simplicity and avoid speculative implementation;
- keep DevCadence engineering semantics authoritative;
- allow a future external cognitive runtime to execute DevCadence work without rewriting the control plane;
- preserve provider/session neutrality already established by M3C/M3D;
- keep deterministic validation, evidence lineage, policy, budget and acceptance enforceable outside the runtime;
- allow event-driven suspension and hierarchical task knowledge without forcing those concepts into today's native scheduler;
- enable fair native-vs-COS experiments over the same DevCadence contracts.

## Decision

### 1. WorkflowPlan is an engineering contract, not an execution trace

A DevCadence `WorkflowPlan` describes the **logical engineering obligations and ordering constraints** that must be satisfied.

It does not prescribe the complete runtime task graph.

An execution runtime may internally:

- split one logical stage into many runtime tasks;
- create temporary scouts, critics, experiments or local planners;
- recursively decompose work;
- schedule tasks concurrently or serially within allowed constraints;
- suspend and resume runtime tasks;
- retain or reconstruct runtime-private knowledge;
- retry or reroute within the authorized budget/policy envelope.

Those internal operations do not become canonical DevCadence workflow stages merely because the runtime used them.

Conversely, runtime decomposition cannot erase DevCadence obligations. If the logical workflow requires independent review, deterministic validation, a source-exposure bound, a budget limit, or a separate acceptance authority, an execution runtime must preserve those semantics.

### 2. Introduce a workflow-execution runtime boundary above SessionDriver

The durable conceptual boundary is:

```go
type WorkflowExecutor interface {
    Start(ctx context.Context, req ExecutionRequest) (ExecutionHandle, error)
    Observe(ctx context.Context, h ExecutionHandle) (ExecutionEventStream, error)
    Cancel(ctx context.Context, h ExecutionHandle) error
    Result(ctx context.Context, h ExecutionHandle) (ExecutionResult, error)
}
```

The exact Go API is **not** required by this ADR and may change when implementation begins. The durable requirement is the responsibility boundary:

```text
DevCadence policy / EWP / WorkflowPlan / evidence / acceptance
                         |
                         v
                 workflow execution
                    boundary
                  /          \
                 /            \
       native executor      external executor
             |                   |
      SessionDrivers        private runtime graph
      tools / validators    tasks / knowledge / wake logic
```

The existing `SessionDriver` abstraction remains below this boundary. A COS integration is therefore not modeled as a fake single model session.

### 3. ExecutionRequest carries authority and references, not runtime topology

A future `ExecutionRequest` should provide only information required to execute the authorized logical work, such as:

- WorkflowPlan identity/revision/digest;
- EWP / Execution Contract identity;
- project/base/candidate identity as applicable;
- applicable policy and budget envelope;
- references to ContextPacks, evidence and artifacts;
- required output/evidence contracts;
- runtime-independent cancellation/deadline information.

It must not require the caller to describe COS-specific worker trees, receptors, ligands, blackboard layout, planner promotion rules, or equivalent native-scheduler internals.

### 4. Runtime activation is separate from canonical DevCadence task legality

An execution runtime owns the question **"when should this runtime execution unit run?"** inside an authorized logical stage.

A runtime may implement activation using dependencies, timers, external signals, knowledge predicates, event subscriptions, polling-free wait primitives, or another mechanism.

For COS, receptors and ligands plus the Sensory Cortex can implement this responsibility. A native DevCadence executor may initially use ordinary queues, futures, process completion and explicit dependency checks.

DevCadence does not standardize receptor/ligand vocabulary in its canonical protocol now.

Only policy-significant or user-visible lifecycle facts need cross the runtime boundary, for example:

- logical stage started/completed/failed;
- stage is blocked on human/policy/external dependency;
- budget or deadline pause;
- evidence/artifact produced;
- cancellation;
- terminal result.

A runtime-private child sleeping while another child works is not automatically canonical ProjectState.

### 5. Runtime knowledge may be hierarchical, but DevCadence authority stays external

An execution runtime may maintain a hierarchical knowledge substrate for its own tasks.

For COS this may be a task/ancestor blackboard structure where a child sees local knowledge plus inherited or referenced ancestor knowledge, and where fact changes can trigger sensory events. The runtime may store summaries, hypotheses, intermediate results, task relationships and references to durable artifacts.

The boundary distinguishes three classes:

1. **Authoritative DevCadence facts** — project state, accepted decisions, invariants, EWP contract, review state, validated evidence and policy. These remain owned by DevCadence.
2. **Runtime-private knowledge** — hypotheses, temporary task state, blackboard facts, internal subtask outputs and scheduling metadata. These may disappear or be restructured without changing canonical DevCadence state.
3. **Returned candidate evidence/facts** — runtime outputs proposed for DevCadence ingestion. They acquire canonical meaning only through the normal evidence, validation, review or acceptance path.

References/digests are preferred to duplicating large payloads. Existing ContextPack/EvidenceLease/artifact mechanisms remain the source of bounded content delivery; an external runtime may index or mirror them but does not become their authority.

### 6. Knowledge-driven wakeup stays behind the runtime boundary

A runtime may subscribe a sleeping task to a condition over its knowledge/environment and wake it when a matching fact or event appears.

COS may realize this as:

```text
blackboard fact changes
        |
      ligand
        |
  sensory cortex
        |
 matching receptor
        |
   wake runtime task
```

DevCadence needs the semantic effect, not those implementation types.

This permits a future COS adapter to retain its own architecture rather than forcing COS concepts into DevCadence core. It also permits the native executor to remain much simpler.

### 7. The native executor is intentionally dumb

The native DevCadence executor is the reference implementation needed to make DevCadence work, not a second attempt to build a general cognitive operating system.

Its default design target is deliberately boring:

- one DevCadence control-plane instance;
- one host unless a current requirement proves otherwise;
- direct execution of ready logical stages;
- ordinary dependency checks;
- existing SessionDrivers, process runner, worktrees and validators;
- simple bounded concurrency where useful;
- explicit completion/failure/cancellation;
- no distributed task ownership or synchronization;
- no hierarchical blackboard unless a concrete native DevCadence requirement independently needs one;
- no generic sensory/event fabric beyond the events already required by current workflows.

A sophisticated external runtime should add sophistication **behind** the execution boundary rather than forcing the native executor to predict it.

This ADR therefore does **not** require implementing:

- a plugin framework;
- an external executor today;
- hierarchical blackboards;
- receptors or ligands;
- durable runtime-task persistence beyond current requirements;
- distributed workers;
- dynamic recursive planning;
- generic pub/sub infrastructure.

Until there is an actual second execution runtime or the native scheduler needs the abstraction for M4/M10, DevCadence should implement only the smallest seam required by current work.

When substantial workflow execution is first materialized, code should depend inward on the workflow-execution contract rather than on one concrete native scheduler.

### 8. Future task placement and synchronization remain executor concerns

A future execution runtime may distribute its private runtime tasks across machines while preserving the same DevCadence logical workflow.

For example, a future COS Nexus may provide nodes that persist tasks and allow COS Runtime instances to synchronize or acquire those tasks. That can eventually make a COS-backed DevCadence execution distributed without requiring today's DevCadence control plane to implement distributed task scheduling.

The compatibility requirement is intentionally weak:

- canonical DevCadence workflow semantics must not assume that every runtime task executes in the same process or on the same machine;
- logical task/stage identity must not derive authority from executor process/node identity;
- source, artifact, credential, worktree and validation locality remain explicit constraints where they matter;
- an external runtime is responsible for its own task synchronization, ownership, recovery and internal consistency;
- DevCadence still receives policy-significant outcomes/evidence through the workflow-execution boundary.

This does **not** claim that current DevCadence is distributed-ready. A real distributed integration will require concrete designs for authentication, source/artifact availability, worktree placement, failure/retry semantics, secret exposure, evidence provenance and split-brain/duplicate execution. Those are future requirements and must not be pre-implemented speculatively.

The native executor remains free to be single-process and single-host.

### 9. Runtime-private topology must not leak into stable protocol without independent need

Do not add fields to canonical `WorkflowPlan` merely to mirror one executor's machinery.

Examples that remain runtime-private unless a separate DevCadence requirement justifies them:

- parent worker identifiers;
- arbitrary nested subtask depth;
- planner-promotion state;
- blackboard storage layout;
- receptor/ligand representation;
- reflection loop counters;
- model transcript structure.

Protocol additions require an engineering-semantic need, not parity with COS or the native implementation.

## COS mapping (non-normative)

| COS capability | DevCadence boundary interpretation |
| --- | --- |
| persistent/suspendable task | runtime execution unit |
| task state machine / scheduler | executor-private activation machinery |
| receptor + ligand | executor-private wait/wake mechanism |
| Sensory Cortex | executor-private event router |
| hierarchical / holographic blackboard | executor-private knowledge substrate |
| fractal sub-DAG / worker promoted to planner | executor-private decomposition |
| model router | may reuse DevCadence authorized cognition resources beneath the executor |
| task result / evidence | returned through DevCadence evidence/result contracts |
| COS Nexus task storage/synchronization | executor-private placement, persistence and multi-node coordination |

The mapping is deliberately one-way. DevCadence is not required to reimplement COS terminology.

## Consequences

### Positive

- a future COS-backed executor can be introduced without redefining DevCadence engineering policy;
- the existing native executor remains simple;
- native and COS execution can be compared against the same frozen EWP/WorkflowPlan corpus;
- event-driven sleep/wake and hierarchical knowledge become available later without contaminating canonical project state;
- individual model/session adapters remain reusable under either executor;
- COS can evolve independently while DevCadence preserves stable engineering semantics.
- a future COS Nexus can add distributed task placement/synchronization without requiring the native DevCadence executor to become distributed.

### Negative

- there is one more architectural boundary to preserve;
- some lifecycle facts will need careful classification as canonical versus runtime-private when the executor is implemented;
- an external runtime may need adapters for DevCadence evidence/artifact/context references.

### New risks

- **Leaky abstraction:** native or COS implementation details creep into WorkflowPlan. Mitigation: DCI-159 and contract-focused review.
- **Shadow authority:** runtime blackboard facts are accidentally treated as accepted project truth. Mitigation: DCI-160 and normal evidence/acceptance ingestion.
- **Double scheduling:** DevCadence and an external runtime both try to own the same internal scheduling decision. Mitigation: DevCadence owns logical legality/obligations; executor owns runtime activation inside an authorized stage.
- **Lost observability:** runtime-private tasks hide material failure. Mitigation: require policy-significant lifecycle/evidence/resource outcomes at the boundary without mirroring every internal event.
- **Premature distribution tax:** future multi-node possibilities distort the simple native executor. Mitigation: DCI-161; keep placement/runtime ownership non-semantic while explicitly deferring distributed correctness mechanisms until a concrete runtime needs them.

## Implementation guidance

### Now

- document the boundary and invariants;
- keep `WorkflowPlan` logical;
- do not introduce COS-specific types;
- do not implement an executor plugin system solely for this future possibility.
- keep the native executor single-node and straightforward unless a current DevCadence requirement proves a more complex mechanism necessary;
- do not implement Nexus-like synchronization, consensus, leases, distributed queues or replicated task stores in DevCadence merely for future compatibility.

### When M4 materializes a reusable orchestration harness

Prefer a narrow internal workflow-execution seam so the reference/native executor is one implementation rather than the semantic definition of DevCadence.

The first implementation can be intentionally small. An interface should be extracted from demonstrated native needs, not guessed from COS.

### When COS is available as an executable/package

Add a COS adapter at the workflow-execution boundary. Map DevCadence logical stages and references into COS mission/tasks; allow COS to create its own private hierarchical graph/blackboards/wake conditions; map only semantic outcomes and evidence back. If COS Nexus is present, COS may also place/synchronize those private tasks across Nexus nodes; that remains an executor implementation detail unless a concrete DevCadence policy needs to constrain placement.

Do not bypass DevCadence validators, policy, independent-review rules or acceptance authority.

### Experimental use

Run the same revision-pinned workload corpus through:

```text
WorkflowPlan -> native executor
WorkflowPlan -> COS executor
```

Measure at least:

- accepted correctness / regressions;
- scarce frontier or subscription usage;
- total local inference/compute;
- context/input volume where measurable;
- retries and repair rounds;
- wall time;
- human interventions;
- policy violations/refusals;
- evidence completeness;
- recovery from waits/failures.

A later DevCadence rewrite using COS should preserve the frozen specification and compare against a control implementation so that gains are attributable rather than anecdotal.

## Verification plan

1. Architecture review can identify a single ownership answer for each concern: DevCadence logical policy or execution-runtime mechanism.
2. A future fake second executor can run representative WorkflowPlans without changes to task/EWP/policy/acceptance semantics.
3. Native and external executors can consume the same logical plan and produce comparable semantic results/evidence.
4. Runtime-private knowledge cannot directly mutate accepted ProjectState or close review/acceptance obligations.
5. Independent-review requirements remain enforceable even when one runtime internally uses many workers.
6. A task can remain dormant in an external runtime without DevCadence requiring repeated model polling.

## Rollback / supersession strategy

If experience shows that no meaningful second runtime exists and the boundary adds unjustified complexity, the interface may remain internal or collapse into the native executor while retaining the semantic distinction that WorkflowPlan is not an execution trace.

If COS or another runtime proves that additional cross-runtime semantics are truly required, amend this ADR from demonstrated integration requirements rather than preemptively modeling the full external runtime.

## Follow-up

- [ ] Preserve this boundary when M4 introduces reusable orchestration code.
- [ ] Add executable interface/types only when a concrete native implementation needs them.
- [ ] Add a COS adapter only when COS has a stable callable runtime/package boundary.
- [ ] Define the native-vs-COS benchmark corpus before claiming improvement.
