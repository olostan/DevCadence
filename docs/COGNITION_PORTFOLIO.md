# Adaptive Cognition Portfolio

## Scope

This document defines how DevCadence turns discovered cognition resources into an explainable, policy-compliant engineering organization.

It separates five concerns that must not be conflated:

1. engineering **role**;
2. endpoint **capability/effectiveness**;
3. **access/session channel**;
4. **economic/budget regime**;
5. task-specific **workflow topology**.

The normative architecture decision is [ADR-0018](adr/0018-adaptive-cognition-portfolio-and-workflow-synthesis.md).

## 1. Product principle

DevCadence should bring value on substantially any supported configuration:

- no model cognition;
- one authenticated subscription CLI and no useful GPU;
- several subscriptions and no API spending;
- local MLX on Apple Silicon;
- NVIDIA/other local inference;
- a small Linux box plus remote cognition;
- metered APIs only;
- mixed local + subscription + API resources.

Missing resources reduce the available engineering organization; they do not redefine DevCadence around one reference stack.

## 2. Roles are not models

A role declares requirements. It does not name a provider. Principal, Implementer, Reviewer, Consultant, Scout and Summarizer are role abstractions.

A user may prefer Anthropic for architecture, OpenAI for architecture review, Gemini for code review, and Qwen/MLX or hosted Qwen for implementation. Another user may choose the inverse. DevCadence supports preferences and learns empirical outcomes; it ships no vendor doctrine.

## 3. Cognition endpoint identity

A CognitionEndpoint is a usable path to cognition, not merely a model name.

```yaml
id: codex-subscription-primary
identity:
  provider: openai
  model_family: optional
  model_id: optional
access:
  channel: authenticated_agent_cli
  driver: codex
  account_ref: personal-primary
capabilities:
  architecture: strong
  implementation: strong
  review: strong
economics:
  regime: subscription_quota
  budget_pool: openai-personal
policy:
  source_exposure: tool_mediated_worktree
```

The same underlying model through a direct API is a different endpoint when economics, permissions, tools/session behavior or accounting differ.

## 4. Access channels and session drivers

Representative access channels are local runtime, authenticated agent/coding CLI, official agent/runtime SDK, remote API, and future LAN/remote worker.

A session driver advertises capabilities instead of forcing core code to understand every CLI:
- non-interactive invocation;
- model selection;
- structured final output and streaming events;
- resumable/forkable sessions;
- cancellation;
- worktree binding;
- tool/shell/file-edit capability;
- MCP/tool configuration;
- usage and quota/rate-limit reporting.

Unsupported features fail closed when a role or workflow strictly requires them; progressive features (such as live streaming or dynamic quota visibility) degrade gracefully where a non-interactive batch contract satisfies the role. Principal hosts remain separate human-facing adapters even when one product exposes both a host and a session interface.

## 5. Economics

### EconomicRegime

Initial conceptual regimes:
- `local_compute`;
- `subscription_quota`;
- `metered_api`;
- `prepaid_credits`;
- `enterprise_allocation`;
- `unknown` / future custom regime.

This is not a model-quality hierarchy.

### BudgetPool

A BudgetPool represents the scarce resource consumed by one or more endpoints: a subscription usage pool, API-dollar budget, enterprise allocation, credits, or local compute.

A pool may define spending cap, overage policy, reserve percentage, preferred roles, concurrency and reset cadence even when exact remaining quota cannot be queried.

### BudgetState

BudgetState is current evidence, not configuration: quota/credits remaining if observable, reset time, rate/concurrency limit, local resource pressure, availability, or a coarse pressure class. Unknown values remain unknown.

## 6. User policy

Simple intent examples:
- do not spend API money;
- use existing subscriptions;
- prefer local where practical;
- keep source local;
- optimize quality over latency;
- preserve scarce quota for high-leverage work.

Expert policy may additionally specify preferred/excluded endpoints per role, provider/model-family diversity requirements, budget reserves, fallback/escalation order, source exposure, spend caps and task/risk overrides.

Templates are editable starting points, not hidden product beliefs.

## 7. Deterministic ResourceInventory

Before AI recommendation, DevCadence establishes facts deterministically: hardware/accelerators, verified local acceleration, runtimes/models, authenticated CLIs/SDKs, configured APIs through opaque credentials, endpoint health/auth, session-driver features, capability provenance, principal hosts, declared economic/budget bindings, resource observations and user/project policy.

No model is required for this stage.

## 8. Bootstrap and portfolio synthesis

```mermaid
flowchart TD
    Discover["Deterministic discovery"]
    Inventory["ResourceInventory"]
    Minimal["Suitable cognition endpoint?"]
    Bootstrap["Bounded setup/remediation"]
    Planner["AI Cognition Portfolio Planner"]
    Candidate["PortfolioRecommendation(s)"]
    Validate["Deterministic policy validator"]
    Portfolio["Active CognitionPortfolio"]
    Discover --> Inventory --> Minimal
    Minimal -->|"no"| Bootstrap --> Discover
    Minimal -->|"yes"| Planner --> Candidate --> Validate --> Portfolio
```

The endpoint used to plan the portfolio need not be the endpoint ultimately preferred for Principal, implementation or review.

**Implementation status (WP-M3D-1B).** The `Planner` step above exists as a pure service, `internal/cognition/planner`: given caller-supplied facts and an injected one-method `Invoker`, it builds a deterministic prompt, calls the invoker once (no retry), strictly decodes the output, assigns every identity, timestamp and provenance field in Go, and gates each alternative through the deterministic validator. Alternatives that fail are returned as structured rejections. It never activates or persists anything. Not yet implemented (WP-M3D-1C and later): planning-endpoint selection, the driver-backed `Invoker` adapter, compiler admission of the planner role, historical-evidence input and the link from a recommendation to activation.

## 9. Portfolio recommendation

The planner reasons over ResourceInventory, DevCadence role requirements, project language/shape/risk/privacy, user policy/preferences, budget/resource state and evaluated historical outcomes.

Where useful, it presents materially different candidates such as minimum monetary spend, balanced, maximum quality within policy, or privacy-first. A recommendation explains tradeoffs and does not grant authority.

The planner service (WP-M3D-1B) is the first producer of these records; it consumes the inventory, project languages/risk tags, policy and budget/resource state as caller-supplied facts. Historical evidence is not yet an input. Rejection reasons are bounded by the planner, but diagnostics are validator output and are not truncated: consumers that log or persist a planner `Result` must bound diagnostics.

A recommendation record carries this explanation in optional fields: `set_id` (sibling alternatives), `intent` (`minimum_spend`, `balanced`, `maximum_quality_within_policy`, `privacy_first`), `tradeoffs`, `confidence` (`high`, `medium`, `low`) and `planner` provenance (`endpoint_id`, `driver_id`, optional `model_id`, `invocation_digest`). When `planner` is present the other four are required; `planner` absent means a non-AI/heuristic recommendation. These fields are informational only: they never grant, expand or substitute for deterministic validation or activation authority. The Go reader additionally rejects whitespace-only values (the schema cannot). See PROTOCOLS §3C.

## 10. Deterministic validation

Before activation, validate that endpoints/budget pools exist, health/auth is sufficient, capability provenance is acceptable, required session features exist, source exposure and spending are authorized, credential handling stays opaque, resource claims are feasible, diversity claims are truthful, and setup/destructive authority is not expanded.

Unknown observed state fails closed (DCI-005). `DefaultValidationPolicy()` sets `RequireKnownResourceState` and `RequireKnownBudgetState` to true, and a nil `ValidationInput.Policy` enforces these defaults:

- A supplied `ResourceState` with non-empty `UnknownMetrics` yields `UNKNOWN_RESOURCE_STATE`.
- When `BudgetStates` is supplied (non-nil), each budget pool used by a role binding or fallback whose regime is not `local_compute` yields `UNKNOWN_BUDGET_STATE` if its entry is absent or nil (observed `missing`) or has `status=unknown` (observed `unknown`). Diagnostics are emitted in sorted pool-id order; `local_compute` pools and unused pools are exempt.
- A nil `BudgetStates` or `ResourceStates` map means no observation was supplied and is not checked (documented residual).

Context profile consistency (WP-M3C-H3, always on, no policy flag): a context profile referenced by a role or fallback binding must describe that binding's own endpoint and channel (`ContextProfile.EndpointID`/`ChannelID`, exact string equality). Otherwise one `CONTEXT_PROFILE_MISMATCH` diagnostic is emitted per offending binding or fallback, even when the binding's channel is missing. A nonexistent or nil profile entry yields only `CONTEXT_PROFILE_NOT_FOUND`.

**Warning:** a `ValidationPolicy` built as a struct literal or decoded from JSON defaults both flags to false, which is a silent opt-out of these checks. Start from `DefaultValidationPolicy()` and clear the flags only as an explicit, deliberate opt-out.

**AI proposes. Deterministic machinery authorizes.**

## 11. CognitionPortfolio

The active CognitionPortfolio is versioned configuration containing role eligibility/preferences/fallbacks, endpoint exclusions, budget policy/reserves, source-exposure constraints, escalation rules, diversity requirements, retry/review constraints and workflow defaults.

It is distinct from transient BudgetState and human-readable deployment labels. Identity and free-text fields of portfolio, recommendation and workflow records reject whitespace-only values in Go, and the portfolio validator reads only the effective policy (WP-M3C-H4).

## 12. Adaptive workflow topology

For each task, a Workflow Planner combines task/risk requirements, CognitionPortfolio, current resource/budget state, deterministic gates and historical effectiveness.

It may choose one capable session plus deterministic validation; distinct Principal/Implementer/Reviewer endpoints; many local repair loops with scarce remote escalation; provider-diverse review for high-risk work; or deterministic/manual operation.

The system is explicitly allowed to choose **less orchestration**.

### Workflow plan validator

`internal/cognition.WorkflowValidator` (WP-M3D-2A1) is the pure deterministic authority that decides whether a `WorkflowPlan` is usable against a given `CognitionPortfolio` and `WorkflowPolicy`; plans, planners and models propose, only this validator authorizes. It reads no clock, I/O or live budget/resource state and never mutates its inputs.

Evaluation stops with one diagnostic when an input is missing (`WORKFLOW_INPUT_MISSING`), an explicit policy has a field `<= 0` (`WORKFLOW_POLICY_INVALID`), or the plan or portfolio fails its own `Validate` (`WORKFLOW_PLAN_INVALID`, `WORKFLOW_PORTFOLIO_INVALID`). Otherwise every rule is evaluated and all diagnostics are collected in `SortedDiagnostics` order:

- Bounds: stage count (`max_stages`, default 8), per-stage retries (the portfolio `WorkflowDefaults.MaxRetries` when greater than zero, else `max_total_retries`), aggregate retries (`max_total_retries`, default 6, saturating `int64` sum) and per-stage timeout (`max_stage_timeout_seconds`, default 3600). `DefaultTimeoutSeconds` is a default, not a cap. An explicit policy is never completed with defaults.
- Bindings: a cognition stage's role must have a role binding, and its provided endpoint, channel and context profile plus its budget pool must all match one single binding tuple (primary or fallback) of that role. Every stage's budget pool must exist. A deterministic stage must carry no endpoint, channel or context profile pointer.
- Structure: an escalation target must name an existing stage with a strictly greater order; review stages must be cognition stages.

A valid verdict is relative to the given portfolio and policy only; binding the portfolio to the active one is the caller's job. Live budget and metered-pool authorization is WP-M3D-2A2; the deterministic planner is WP-M3D-2B; the AI planner is WP-M3D-2C. Contract: [wp-m3d-2a1-workflow-plan-validator.md](work-packages/wp-m3d-2a1-workflow-plan-validator.md).

## 13. Adaptation

A material resource change can trigger a PortfolioChangeProposal: a new or expired subscription, new CLI/SDK, API spending change, new GPU/runtime/model, changed policy, or meaningful evaluated outcome evidence.

The proposal is a diff with rationale. It is validated, auditable and reversible. DevCadence does not silently rewrite user policy.

## 14. Learning

Track effectiveness against the complete context:

```text
(task class, role, endpoint, access channel, economic regime,
 workflow topology, prompt/skill revision) -> outcomes/resources
```

Learned routing remains governed through LessonCandidate/evaluation/promotion, not opaque self-modification.

## 15. UI and configuration

The CLI is the first user experience. A later self-hosted UI/dashboard should visualize and edit the same canonical contracts: resources, endpoint health, budget state, role assignments, workflow history, effectiveness evidence and recommended configuration deltas.

The dashboard is not a second source of truth and is deliberately not built before the protocols are proven.

## 16. Success criterion

A new user should eventually be able to run one guided setup command and receive a safe, useful, explainable configuration that makes the best use of what they already have—without needing to understand model routing, accelerator stacks, subscription economics or provider integration details.

Advanced users retain full control over policy and role preferences.
