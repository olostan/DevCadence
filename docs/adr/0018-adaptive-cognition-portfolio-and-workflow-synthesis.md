# ADR-0018: Adaptive Cognition Portfolio and Workflow Synthesis

- **Status:** Accepted
- **Date:** 2026-09-23
- **Related:** ADR-0011, ADR-0013, ADR-0014, FR-011, FR-027, FR-028, DCI-054, DCI-055, DCI-104
- **Documents:** docs/COGNITION_PORTFOLIO.md, docs/MODEL_RUNTIME.md, docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md

## Context

Earlier DevCadence documents correctly separated roles from providers and allowed local runtimes, authenticated CLIs and remote APIs. However, the routing/economic model still tended to describe a one-dimensional ladder and M3B proposed selecting one of a small set of deployment profiles with a pure deterministic recommendation function.

That is insufficient. Users differ across hardware, local models, agent CLIs/SDKs, subscriptions, quota pressure, API willingness, enterprise/prepaid allocations, privacy rules, provider preferences, project shape and historical effectiveness. The same model may be economically different through a subscription CLI versus a metered API. A nominally cheap model can consume more total resources through retries; a subscription model can have zero marginal dollars yet scarce quota.

The optimal answer is therefore not a fixed provider ranking, deployment mode, or Principal→Implementer→Reviewer pipeline.

## Decision

### 1. Role, capability, access, economics and workflow are orthogonal
DevCadence separately represents engineering role requirements, endpoint/model identity and capability, access/session channel, economic regime/budget pool, current resource state and task-specific workflow topology.

### 2. Cognition endpoints include the access path
The same provider/model reached through an authenticated subscription CLI and a metered API is represented as distinct endpoints when economics, permissions, tools or session behavior differ.

### 3. Economics use EconomicRegime + BudgetPool
New adaptive behavior uses explicit regimes such as local compute, subscription quota, metered API, prepaid credits, enterprise allocation and unknown/custom. Existing M3A CostClass remains a coarse compatibility field until M3C migration and is not the final ontology.

### 4. Deterministic ResourceInventory precedes AI recommendation
M3B owns safe bootstrap/remediation and produces verified ResourceInventory/readiness without requiring model cognition.

### 5. AI-assisted Cognition Portfolio Planner
When any eligible endpoint satisfies minimum planning capability, DevCadence may use it to synthesize typed PortfolioRecommendations from ResourceInventory, role requirements, project characteristics, user policy, economic/resource state and historical evidence. The planning endpoint has no special authority in the result.

### 6. AI recommendation is not authority
Deterministic validation checks endpoint existence/readiness, capability provenance, required session features, privacy/source exposure, monetary/overage authority, resource constraints and independence claims. The planner cannot create credentials, weaken privacy, enable spending, expand setup/destructive authority or silently rewrite user policy.

### 7. CognitionPortfolio is canonical routing configuration
Deployment labels such as local-heavy, hybrid-thin, cloud-cognition and offline remain useful descriptors/templates but are not closed architectural modes.

### 8. Workflow topology is adaptive
DevCadence routes not only which endpoint fills a role, but which roles/model passes are useful for this task. A Workflow Planner may collapse to one session, use distinct providers, spend abundant local iterations before escalation, or operate deterministically without cognition.

### 9. No silent paid fallback
Loss of subscription/local/prepaid/enterprise availability never authorizes fallback to metered or overage billing. Expanded monetary authority is explicit.

### 10. Hosts and session drivers are independent adapters
Human-facing hosts and machine-invocable cognition/session interfaces are separate contracts even when one product exposes both.

### 11. Adaptation is explicit and reversible
New hardware, subscriptions, APIs, models, policy or evaluated outcomes may produce a PortfolioChangeProposal. Applying it follows validation and normal policy; learned evidence never silently mutates normative configuration.

## Consequences

### Positive
- useful operation across highly heterogeneous machines and access arrangements;
- reuse of subscription quota rather than forced per-token APIs;
- local models valuable but optional;
- user/team provider preferences rather than product doctrine;
- new providers/runtimes/CLIs fit behind adapters;
- workflow complexity can decrease when extra model passes have poor marginal value;
- economics can account for retries and scarce quotas rather than nominal token price.

### Costs
- routing becomes genuinely multidimensional;
- new protocol types are required;
- heterogeneous session drivers need contract tests;
- AI recommendation itself needs evaluation and deterministic validation;
- quota visibility can be partial or unavailable.

### Risks and mitigations
- **Opaque AI optimizer:** typed, explained, validated recommendations.
- **Vendor bias:** no built-in provider-role ranking.
- **Runaway cloud use:** no silent metered fallback and bounded workflow/retry policy.
- **Planner self-preference:** planning endpoint has no special authority.
- **Scope explosion:** M3B remains deterministic bootstrap; M3C owns deterministic cognition/session/economic substrate; M3D owns adaptive synthesis and rich adaptive setup UX.

## Milestone impact
- **M3A:** retained discovery/capability substrate.
- **M3B:** safe deterministic bootstrap, readiness and ResourceInventory with a plain/JSON/basic-terminal surface.
- **M3C:** economics/budgets, session drivers, portfolio protocol shapes and deterministic validation/activation.
- **M3D:** AI-assisted portfolio synthesis, adaptive workflow topology, explicit adaptation/rollback and richer setup/explanation UX.
- **M4:** validates the adaptive heterogeneous-cognition hypothesis early against simpler baselines before broad productization.
- **M5:** semantic principal/host integration consumes the M3 substrate after the evidence gate.
- **M6:** brownfield Project Adoption remains a separate product subsystem and is not required to learn whether adaptive cognition works.
- **M7:** consultants/reviewers become role/independence constraints over the active portfolio.
- **M9:** evaluates endpoint/access/role/task/topology outcomes and governs learned promotion/rollback.
- **M10:** dashboard/long-running scheduler consumes the same protocols.

## Alternatives considered

### Static deployment-profile chooser
Rejected as canonical routing. Labels cannot express subscription pools, API willingness, quota scarcity, role preferences, session features and heterogeneous effectiveness.

### Deterministic weighted scoring only
Rejected as the sole recommender. Hard constraints belong in deterministic code; portfolio synthesis is a reasoning problem and a static score would encode hidden product opinions.

### Let AI fully configure itself
Rejected. AI reasoning is valuable for synthesis but must not control policy/spending/security authority.

### Require local inference
Rejected by ADR-0011 and the goal of useful operation on ordinary/lightweight machines.

### Standardize on one host/provider CLI
Rejected. Hosts and cognition access are replaceable integrations.
