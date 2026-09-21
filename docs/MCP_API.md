# Semantic MCP API

## Scope

This document defines the intended principal-facing MCP surface. It is deliberately semantic. The principal should think in engineering operations, not raw repository primitives.

Exact MCP transport/configuration is adapter-level and may evolve without changing these semantics.

### Bootstrap executable contract

The `devcadience-mcp` binary MUST start the stdio MCP server when invoked with no arguments. The no-argument stdio behavior is a host-neutral compatibility contract intended to make integration straightforward from supported principal hosts.

Antigravity is the reference host; Cursor and Visual Studio Code are also initial first-class host targets. Host-specific configuration remains adapter-level.

See [PRINCIPAL_HOSTS.md](PRINCIPAL_HOSTS.md) and [ANTIGRAVITY_INTEGRATION.md](ANTIGRAVITY_INTEGRATION.md).

## 1. API design principle

```mermaid
flowchart LR
    Bad["Raw API<br/>read_file / shell / ask_qwen"]
    Good["Semantic API<br/>investigate / delegate / review"]
    Principal["Principal engineer"]
    Control["Control plane"]

    Principal -. avoid .-> Bad
    Principal --> Good --> Control
```

The control plane chooses how to satisfy semantic operations.

## 2. Bootstrap tool set

### `project_state`
Returns compact canonical state.

Input:
- project ID;
- optional focus (milestone/component/task);
- optional state revision.

Output:
- ProjectState or focused projection.

### `investigate`
Requests repository-local investigation.

Input:
- question;
- evidence requested;
- scope hints;
- base commit/state revision;
- semantic response budget.

Output:
- EvidencePacket.

### `create_work_package`
Persists a principal-authored Engineering Work Package after validating references and base revision.

This tool does not generate the package; the principal supplies the design artifact.

### `delegate`
Starts an Attempt for an approved Work Package.

Input:
- work package ID/version;
- worker profile;
- optional execution policy override allowed by project policy.

Output:
- attempt ID and status.

### `task_status`
Returns current task/attempt status without flooding the principal with logs.

### `validate`
Runs or retrieves a validation profile for a candidate.

### `review`
Schedules independent review dimensions.

### `request_evidence`
Retrieves progressively deeper raw evidence behind an EvidencePacket/ReviewResult.

### `accept`
Records an authorized candidate acceptance subject to policy gates.

### `reject`
Rejects candidate with reason and optional revision direction.

### `record_decision`
Persists a DecisionRecord and links ADR/invariant updates.

## 2A. Discovery tool set

For Day-0 and product-semantic discovery, the semantic MCP surface should support:

### `initialize_project`
Creates a project before source implementation necessarily exists.

### `discovery_state`
Returns the current ProblemModel reference/summary, Ambiguity Ledger summary, requirements status, ProductDecisions, active experiments and latest SpecificationReadiness.

### `record_problem_model`
Persists a new immutable/versioned ProblemModel revision.

### `record_ambiguities`
Adds/updates ambiguity entries while preserving resolution history.

### `record_product_decision`
Persists a human-authoritative product decision and its consequences.

### `record_requirements`
Persists versioned requirements with provenance and epistemic status.

### `record_discovery_experiment`
Creates/updates a bounded discovery experiment and evidence references.

### `review_specification`
Schedules independent specification review dimensions through available local/consultant reviewers.

### `record_specification_readiness`
Persists the evidence-based readiness gate result.

These operations are semantic persistence/orchestration tools. Antigravity remains responsible for the human conversation and synthesis.

See [DISCOVERY_AND_SPECIFICATION.md](DISCOVERY_AND_SPECIFICATION.md).

## 3. Extended tools

Later milestones may add:
- `start_review_campaign`;
- `review_campaign_state`;
- `adjudicate_findings`;
- `create_repair_work_package`;
- `focused_revalidate`;
- `close_review_campaign`;
- `reopen_review_campaign`;
- `consult`;
- `classify_change`;
- `plan_refactoring_epoch`;
- `health_report`;
- `architecture_reconcile`;
- `lesson_candidates`;
- `promote_lesson`;
- `integration_plan`;
- `cancel_attempt`.

## 4. Typical principal session

```mermaid
sequenceDiagram
    participant P as Principal
    participant M as MCP
    participant C as Control Plane

    P->>M: project_state(focus=active_milestone)
    M->>C: query
    C-->>M: ProjectState
    M-->>P: compact state

    P->>M: investigate(question)
    M->>C: create/run Investigation
    C-->>M: EvidencePacket
    M-->>P: evidence

    P->>P: deep design + research + consultants
    P->>M: create_work_package(package)
    M->>C: validate/persist
    C-->>M: work_package_id
    M-->>P: id

    P->>M: delegate(id)
    M->>C: start Attempt
    C-->>M: attempt status
    M-->>P: status

    P->>M: task_status()
    M-->>P: compact completion / escalation

    P->>M: accept(candidate)
    M->>C: policy gate + record
    C-->>P: accepted state revision
```

## 5. Information depth

`request_evidence` supports explicit depth:

- `summary` — semantic finding;
- `symbol` — signatures/types/call relationships;
- `snippet` — focused source excerpts;
- `diff` — relevant patch;
- `file` — selected complete file;
- `artifact` — raw log/test/consultation artifact.

Direct unrestricted repository traversal is intentionally not a public bootstrap MCP primitive.

## 6. Asynchronous behavior

Long operations return IDs/status rather than holding a giant MCP response.

```mermaid
stateDiagram-v2
    [*] --> Queued
    Queued --> Running
    Running --> Completed
    Running --> Blocked
    Running --> Failed
    Running --> Cancelled
    Blocked --> Running: decision/revision
    Completed --> [*]
    Failed --> [*]
    Cancelled --> [*]
```

The principal can poll status or use future notification/event mechanisms.

## 7. Error model

Semantic errors include:
- `STALE_PROJECT_STATE`;
- `POLICY_DENIED`;
- `CONTRADICTED_ASSUMPTION`;
- `VALIDATION_FAILED`;
- `REVIEW_DISAGREEMENT`;
- `NEEDS_PRINCIPAL`;
- `NEEDS_HUMAN`;
- `MODEL_UNAVAILABLE`;
- `CONSULTANT_UNAVAILABLE`;
- `UNSUPPORTED_SCHEMA_VERSION`.

Errors should provide actionable evidence handles.

## 8. Project-state staleness

Creating a Work Package against stale state must be detected.

Policy options:
- accept if changed files/components are disjoint;
- require targeted re-scout;
- require principal revalidation;
- reject.

Do not silently execute a plan whose assumptions may no longer hold.

## 9. Authority

Each MCP tool has an authority level.

Example:
- read semantic state: low;
- investigate: low;
- start isolated attempt: medium;
- accept/integrate: higher;
- promote lesson: higher;
- destructive Git/deployment: outside normal principal MCP bootstrap surface.

## 10. MCP adapter rule

The MCP server is thin:
- validate input schema;
- authenticate/identify caller if needed;
- invoke application service;
- normalize errors;
- stream/return result.

No important routing or acceptance policy belongs only in the MCP adapter.


### Review-campaign API rule

Review tools must preserve campaign convergence semantics.

The API must not expose "run another unrestricted review" as the default post-repair operation. Broad review, focused revalidation, closure review, and reopen are distinct semantic actions with different thresholds.

A reopen request for an adjudicated/frozen campaign must carry materially new evidence or a changed requirement/policy basis. Equivalent new opinion is rejected by policy.

Implementers receive consolidated Repair Work Packages rather than raw reviewer transcripts by default.

See [REVIEW_AND_CONVERGENCE.md](REVIEW_AND_CONVERGENCE.md).
