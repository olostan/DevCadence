# DevCadence System Invariants

These invariants are non-negotiable constraints. They exist to prevent local optimization, model confidence, or implementation convenience from silently changing the character of the system.

Identifiers are stable. If an invariant is superseded, preserve the identifier and record the superseding ADR rather than renumbering history.

## A. Intelligence-boundary invariants

### DCI-001 — High-leverage cognition is allocated deliberately
High-leverage reasoning receives the strongest eligible cognition the active portfolio and policy can justify. No provider/model family is definitionally "the frontier principal."

### DCI-002 — High-volume cognition is resource-aware
High-volume repository work uses the least scarce quality-sufficient eligible resources according to the active portfolio. Local tokens may be effectively unmetered; subscription quota, hosted "cheap" tokens and review cycles are not assumed free.

### DCI-003 — Optimize context volume, not thinking time
The system must not sacrifice design quality merely to reduce wall-clock reasoning time. Compact, high-quality frontier reasoning is preferred over fast implementation begun with weak grounding.

### DCI-004 — Principal self-challenge is mandatory
For systemic and architectural changes, the principal must explicitly challenge its initial solution, verify material assumptions, consider alternatives, and search for counterevidence before implementation.

### DCI-005 — Material assumptions are visible
A material assumption must be represented as an assumption until verified. Inference must not be serialized as fact.

### DCI-006 — Consultants are independent evidence sources
Consultant output is never automatically authoritative. For important independent review, consultants should receive neutral problem statements before seeing the principal’s proposed answer when feasible.

### DCI-007 — First-answer convergence is not a completion criterion
A coherent first design does not by itself establish readiness for systemic or architectural implementation.

### DCI-008 — Product ambiguity is not silently resolved
A frontier principal, consultant, or local agent must not silently turn unresolved human intent into a confirmed requirement or architectural fact.

### DCI-009 — Human product authority is preserved
Goals, acceptable tradeoffs, privacy preferences, user-visible semantics, scope choices and other product-authority decisions belong to the human/product authority. Engineering agents may explain consequences but may not override them for implementation convenience.

## B. Context and evidence invariants

### DCI-010 — The principal does not require whole-repository context
The normal principal interface is ProjectState + targeted EvidencePackets + normative design artifacts. Raw source is progressive escalation, not the default.

### DCI-011 — Raw evidence remains retrievable
Compression must not destroy provenance. Every material evidence claim should point to a retrievable source: file/line/symbol, Git object, command output, test artifact, external source, or consultant result.

### DCI-012 — Model confidence is not evidence
Self-reported confidence may be metadata but cannot replace deterministic checks, provenance, independent agreement/disagreement, or explicit verification.

### DCI-013 — Facts and interpretations remain distinguishable
Structured responses must make it possible to tell deterministic observations from model conclusions, assumptions, recommendations, and unknowns.

### DCI-014 — Evidence depth is progressive
The system should support summary -> symbol/signature -> focused snippet -> diff -> full file -> direct exploration rather than immediately transferring the maximum context.

### DCI-015 — Requirements preserve provenance and epistemic status
A requirement must remain distinguishable as confirmed, evidence-backed, proposed, assumed, deferred, rejected or superseded. Model inference must not be serialized as human-confirmed intent.

### DCI-016 — Architecture follows specification readiness
For substantial greenfield or product-semantic work, architecture must not begin while material ambiguity remains unresolved unless that ambiguity is explicitly accepted as risk or safely deferred behind a documented boundary.

### DCI-017 — Humans are not asked to guess resolvable facts
When a material question can be established through repository evidence, current authoritative research or a bounded experiment, the system should resolve it there rather than forcing the human to provide a technical guess.

### DCI-018 — Durable knowledge is not resident context
Normative authority does not imply default prompt admission. Every substantial context object must be required by the task contract, selected by deterministic role/domain/risk mapping, or retrieved to resolve an explicit question with provenance. Additional evidence remains progressively retrievable; legitimate investigation is bounded by endpoint and policy admission, not forbidden by default context targets.

### DCI-019 — Delegated execution has no hidden requirements
Every execution-critical MUST/MUST-NOT requirement must be present verbatim in the bounded Execution Contract or deterministically admitted as an exact, revision-pinned normative clause before the affected action. A summary, index or reference handle alone is insufficient. Correct execution must not depend on discovering requirements through broad corpus reading; unresolved applicability blocks the affected action and triggers context resolution.

## C. Work-package invariants

### DCI-020 — Non-trivial implementation starts from an Engineering Work Package
Systemic and architectural changes must not be delegated to an implementer from an unstructured chat instruction.

### DCI-021 — Work Packages encode how, not only what
Where it reduces ambiguity, the principal must include implementation strategy, algorithms, pseudocode, interface sketches, examples, failure cases and test strategy.

### DCI-022 — Requirement strength is explicit
Guidance is classified as MUST, SHOULD, SUGGESTED, or LOCAL_DISCRETION.

### DCI-023 — Execution workers may challenge assumptions
An implementer must be able to stop and report a contradicted blueprint assumption with evidence.

### DCI-024 — Execution workers may not silently override MUST constraints
If the implementation cannot satisfy a MUST condition, the task is blocked or escalated.

### DCI-025 — Task scope cannot silently expand
Material scope expansion creates a revised Work Package or a separate task.

## D. Source-control and execution invariants

### DCI-030 — Autonomous work is isolated
Once worktree support is implemented, autonomous changes occur in isolated Git worktrees/branches based on an explicit base commit.

### DCI-031 — Execution workers do not merge directly to main
Acceptance and integration are separate control-plane decisions regardless of whether worker inference is local or remote.

### DCI-032 — Every candidate change has lineage
At minimum: project-state revision, task ID, Work Package version, base commit, attempt ID, model/profile, candidate commit, validation results, review results and decision.

### DCI-033 — External processes are controlled
Commands have explicit working directory, environment policy, timeout/cancellation and captured output. Agents do not receive unconstrained shell authority by default.

### DCI-034 — Parallel work cannot share mutable working trees
Parallel implementation uses isolated workspaces and explicit integration.

## E. Verification invariants

### DCI-040 — Passing tests are necessary but not sufficient
Correctness, architecture, security, maintainability and Work Package compliance may require independent checks beyond tests.

### DCI-041 — Deterministic validation is authoritative for deterministic claims
Exit codes, schema validation, compilation, static-analysis output and tests are captured directly from tools, not paraphrased by a model as the source of truth.

### DCI-042 — Reviewers are independent by default
Review agents use clean contexts and do not inherit the implementer’s private reasoning.

### DCI-043 — Review dimensions are explicit
Correctness, architectural compliance, security, test adequacy, performance and complexity are separate review concerns and may use different agents/models.

### DCI-044 — Disagreement is preserved
Independent disagreement is a risk signal. It must not be hidden by majority prose or averaged confidence.

### DCI-045 — Repeated failed attempts trigger escalation
Retries are bounded by policy. Infinite autonomous repair loops are forbidden.

### DCI-046 — Review seeks bounded residual risk, not zero criticism
A candidate is not required to reach a state where no intelligent reviewer can suggest another improvement. Closure depends on material risk and evidence, not exhaustion of possible opinions.

### DCI-047 — Broad reviews converge before repair
Where practical, independent broad reviewers inspect the same immutable candidate and their findings are adjudicated together before implementation changes begin. Serial unrestricted review/repair ping-pong is forbidden by default.

### DCI-048 — Reopening requires threshold-crossing evidence
After adjudication or freeze, another equivalent opinion is not sufficient to reopen work. Reopening requires materially new evidence, changed requirements, deterministic failure, or a newly discovered applicable correctness/security/integrity/durable-contract issue.

### DCI-049 — Review and repair campaigns are bounded
Repair rounds, reviewer output volume, and closure criteria are policy-bounded. Once the closure gate passes, below-threshold findings become explicit future work rather than extending the active campaign.

## F. Architecture and governance invariants

### DCI-050 — Architecture changes are deliberate
Execution workers cannot create, remove or materially redefine architectural boundaries, invariants, security boundaries, or durable public contracts without an authorized design decision.

### DCI-051 — ADRs explain durable choices
Important architectural decisions record context, alternatives, rationale, consequences, evidence and supersession relationships.

### DCI-052 — Canonical project state is external to model conversation
No model’s chat/session history is the sole authoritative memory of the project.

### DCI-053 — Project state is reconstructable
Where practical, state is derived from versioned records/events and durable artifacts so that current state can be audited and rebuilt.

### DCI-054 — Protocols are model-independent
Core contracts must not depend semantically on one current model provider or prompt format.

### DCI-055 — Provider adapters are replaceable
Gemini/Antigravity, Codex, Claude, Ollama, MLX-LM and worker harnesses are integrations, not core-domain dependencies.

## G. Refactoring and health invariants

### DCI-060 — Refactoring is scheduled
Every substantial project must have planned Refactoring Epochs based on milestone or health triggers.

### DCI-061 — Architecture is periodically reconciled with reality
Long-lived projects perform Architecture Reconciliation: compare implemented reality, original design, accumulated ADRs and current requirements; produce explicit migrations where divergence matters.

### DCI-062 — Feature throughput does not erase health debt
A green functional test suite cannot indefinitely defer known structural degradation.

### DCI-063 — Health evidence combines tools and semantic review
Metrics such as cycles/complexity/duplication are useful but insufficient; semantic overlap, layer leakage, conceptual inconsistency and test architecture also require review.

## H. Learning invariants

### DCI-070 — The system learns from trajectories
Implementation attempts, failures, reviews, decisions and human corrections are retained as structured trajectories subject to storage policy.

### DCI-071 — Learning produces candidates, not immediate self-modification
A lesson or policy idea is proposed, evaluated, then promoted through governance.

### DCI-072 — Normative policy changes are versioned and reversible
Prompt/skill/routing/invariant changes resulting from learning require provenance, versioning and rollback.

### DCI-073 — One anecdote is not a universal rule
A single failure can create a candidate but not automatically a cross-project engineering rule.

### DCI-074 — Design mistakes are learnable
Postmortems analyze architecture and Work Package quality, not only implementer performance.

## I. Security and trust invariants

### DCI-080 — Repository and credential access follow least privilege
Each role gets only the tools, paths and secrets it requires.

### DCI-081 — Secrets are not routine model context
Credentials are injected into controlled processes or provider clients and redacted from logs/evidence unless an operation explicitly requires otherwise.

### DCI-082 — Principal source access can be constrained structurally
When operating through an information firewall, permission boundaries must enforce source restrictions; prompts alone are not a security boundary.

### DCI-083 — Tool output is untrusted input
Repository files, build logs, issue text, external web content and consultant text can contain prompt injection or malicious instructions. Agents treat them as data unless policy explicitly grants authority.

### DCI-084 — Destructive operations require stronger policy
Deletion, force pushes, history rewrites, credential changes, production deployment and irreversible migrations cannot be routine autonomous actions.

## J. Documentation and compatibility invariants

### DCI-090 — Protocol schemas are versioned
Every machine-readable contract has a version and explicit compatibility policy.

### DCI-091 — Prose and schema must agree
A protocol change is incomplete until normative docs and JSON Schema definitions are synchronized.

### DCI-092 — Unknown fields are handled deliberately
Forward compatibility behavior must be specified per schema; silent lossy parsing is forbidden for durable records.

### DCI-093 — Historical records remain interpretable
Migration tools or versioned readers must preserve the ability to inspect old trajectories, decisions and Work Packages.

## K. Bootstrap invariants

### DCI-100 — Prove the core hypothesis before broadening scope
The first milestone proves compact principal context + detailed frontier Work Package + local execution + independent verification on real tasks.

### DCI-101 — No dashboard-first development
Operational UI is useful but must not precede a reliable control-plane vertical slice.

### DCI-102 — No training-first development
Fine-tuning local models is not a bootstrap dependency. Start with prompting, retrieval, protocols, routing and evaluation.

### DCI-103 — No uncontrolled recursive self-development
DevCadence may eventually develop DevCadence, but self-changes follow the same isolation, validation, review and governance requirements as any other project.

### DCI-104 — Capability absence degrades; it does not contaminate unrelated capability
Missing optional local models, accelerators, consultant subscriptions, cognition endpoints or principal hosts reduce the available operating profile but do not turn otherwise valid deterministic/control-plane capabilities into failure.

### DCI-105 — Setup derives from observed capability, not assumed products
Bootstrap must discover the machine, software, authentication readiness and supported integrations before recommending configuration. It must not assume a specific GPU, runtime, subscription or principal host exists.

### DCI-106 — Acceleration is verified, not inferred
GPU presence, driver presence or runtime installation is insufficient evidence that inference is accelerated. A backend may be marked ready only after an empirical runtime probe verifies the intended acceleration path.

### DCI-107 — Principal hosts are replaceable
No core protocol depends on one principal frontend. The initial first-class host scope is Antigravity, Cursor and Visual Studio Code; Antigravity is the reference integration, not an architectural requirement.

### DCI-108 — Setup mutation requires explicit authority
Environment discovery is read-only. Package installation, model download, authentication, service changes, device/group permissions and other setup mutations require a visible plan and the appropriate user approval; privileged/high-impact changes are never silently applied.

### DCI-109 — Managed projects require a canonical documentation baseline
A repository is not DevCadence-ready for normal managed engineering work until the required canonical project documentation set exists in Git and has passed the project Adoption Readiness Gate.

### DCI-110 — Brownfield reconstruction preserves provenance
Retrospective reconstruction must distinguish observed behavior, inherited documentation, model inference, human-confirmed intent, unknowns and contradictions. Reconstructed understanding must not be serialized as historical fact merely because it is plausible.

### DCI-111 — Existing documentation is evidence, not automatic authority
README files, design notes, comments and other inherited documentation inform adoption but do not become canonical solely by existing. Material conflicts with code, tests, schemas or current human intent remain explicit until reconciled.

### DCI-112 — Adoption baseline is version-controlled
The accepted canonical documentation baseline and its adoption decision are tied to explicit Git commits. Normal managed work begins from that accepted baseline, not from an uncommitted reconstruction held only in model or control-plane memory.

### DCI-113 — Pre-adoption autonomy is bounded
Before project adoption reaches READY, DevCadence may perform bounded investigation and isolated adoption work, but it must not treat the repository as ready for normal autonomous implementation, acceptance or integration.

## L. Adaptive cognition portfolio invariants

### DCI-120 — Roles are independent of providers and access channels
Engineering roles are capability requirements, not aliases for vendors, model families, runtimes, CLIs or APIs.

### DCI-121 — Economics belong to access paths
Economic regime, budget/quota pool and scarcity attach to the endpoint/access path, not intrinsically to a model family.

### DCI-122 — No silent metered fallback
DevCadence never converts subscription/local/prepaid usage into metered API or overage spending without explicit policy/approval.

### DCI-123 — AI recommends; deterministic policy authorizes
AI may synthesize portfolio/workflow recommendations. Deterministic machinery validates existence, capability, privacy, spending, permissions, feature compatibility and resource constraints before activation.

### DCI-124 — Recommendation cannot expand authority
A planner cannot weaken source-exposure policy, spending limits, credential rules, setup authority or destructive-operation policy.

### DCI-125 — Workflow topology is adaptive
The number and arrangement of cognition roles is chosen from task risk, portfolio and resource state. More agents, reviews or provider diversity are not intrinsically better.

### DCI-126 — Scarce quota remains scarce when marginal dollars are zero
Subscription-included or enterprise cognition is not treated as unlimited merely because an invocation has no separate API charge.

### DCI-127 — Resource loss degrades, it does not invalidate unrelated capability
When an endpoint, subscription, GPU, runtime or API becomes unavailable, DevCadence preserves deterministic/local-authority capability and derives the best policy-compliant reduced workflow.

### DCI-128 — Portfolio adaptation is explicit and reversible
New resources or learned evidence may produce a recommended configuration delta; they do not silently rewrite normative user policy.

### DCI-129 — Endpoint effectiveness is contextual and empirical
Capability/effectiveness claims are tied to role/task/access context and provenance. Provider reputation, parameter count and marketing tier are not sufficient routing evidence.

### DCI-130 — Hosts and cognition drivers are orthogonal
A human-facing host and a machine-invocable cognition/session interface are separate architectural roles even when one product exposes both.

## M. Cognitive invocation and review-ledger invariants

### DCI-131 — Control-plane complexity does not imply prompt complexity
Lifecycle legality, authority checks, candidate identity, retry/budget enforcement and closure eligibility that can be decided deterministically MUST be enforced by the control plane rather than delegated to model interpretation. A cognition invocation MUST receive only the task-specific semantic obligations/state it needs, not the full DevCadence process model.

### DCI-132 — Mandatory applicability is never similarity-ranked away
Execution-critical MUST/MUST-NOT applicability is decided by deterministic admission class plus task/role/path/domain/risk/action mapping and dependency closure. Every mandatory clause MUST have a deterministic admission path; embeddings, lexical ranking, rerankers or model judgment may improve optional retrieval but MUST NOT remove an applicable mandatory clause.

### DCI-133 — Operative obligations are resident; rationale is retrievable
A model-visible execution-critical obligation must be present as exact revision-pinned content, not only a reference handle. Supporting rationale and large evidence remain progressively retrievable unless required for the current decision.

### DCI-134 — Attempted resolution is not independent verification
An implementer or author may report a fix attempt or challenge with evidence but cannot establish that its own resolution is correct. Findings close only through the configured independent verification/adjudication authority or deterministic proof.

### DCI-135 — Review conversation is not canonical review state
Material findings, dispositions/resolutions, verification and closure state have stable identities outside chat transcripts. Equivalent restatements do not reopen adjudicated findings without materially new evidence, changed contract or a repair regression.

## N. Project bootstrap, capability-pack, and health invariants

### DCI-136 — Discovery is evidence, not authority
Repository manifests, executable presence, CI files, generated configuration, and other observed tooling facts MUST be treated as evidence only; they MUST NOT by themselves create language/framework-specific policy or authority.

### DCI-137 — Ecosystem policy requires explicit activated capability
A toolchain/framework-specific check or invariant MUST become operative only through an explicitly activated/resolved capability or explicitly adopted project-native policy. Unsupported or unknown capability MUST remain explicit rather than receiving model-invented defaults.

### DCI-138 — Managed project health has one canonical contract
Every normally managed project MUST have a versioned ProjectHealthContract defining applicable deterministic checks, lifecycle gates, baseline/regression semantics, debt treatment, and bounded exceptions. ValidationProfiles, hooks, and CI MUST NOT silently become independent health authorities.

### DCI-139 — Historical health debt cannot silently grow
Brownfield health debt MAY be grandfathered through an explicit revision-pinned baseline, but future managed changes MUST NOT introduce unmatched new debt unless an authorized policy change explicitly permits it.

### DCI-140 — Deterministic health precedes cognition-heavy review by default
Unless an EWP or health policy explicitly authorizes a bounded exception, a candidate MUST satisfy its applicable deterministic health gate before scarce semantic-review cognition is spent on acceptance.

### DCI-141 — Health evidence identifies the actual candidate and judge
Presubmit, pre-push, review, and integration health evidence MUST identify the actual candidate/base/ref set plus the governing ProjectHealthContract, pack revisions, and relevant tool versions. Evidence from an unrelated tree or policy revision MUST NOT stand in for the candidate being accepted.

### DCI-142 — Managed enforcement adapters are drift-detectable
When DevCadence manages hooks, CI workflows, or equivalent enforcement adapters, they MUST carry enough contract/pack/renderer provenance for deterministic drift detection.

### DCI-143 — Built-in capability is not universal doctrine
Built-in packs and recipes MUST be treated as reusable capability rather than universal project policy. DevCadence-specific choices such as Make, GitHub Actions, or a particular linter MUST NOT become requirements solely because DevCadence itself uses them.

### DCI-144 — Technology selection is requirement-driven
Greenfield technology/framework selection MUST be justified from product/engineering requirements, constraints, and evidence. DevCadence supportability MUST be represented separately and MUST NOT silently override superior technical/product fit.

### DCI-145 — Blueprint and bounded EWP precede greenfield materialization
A non-trivial greenfield repository/module baseline MUST derive from an accepted ProjectBlueprint or equivalent explicit architecture decision and a bounded EWP. The active host/model MUST NOT become the implicit source of repository topology or technology authority.

### DCI-146 — Greenfield generator/model output is candidate evidence
Files produced by an ecosystem generator or implementation model MUST remain candidate implementation until module/toolchain state, canonical documentation, and health acceptance are established. Generator success MUST NOT itself imply managed readiness.

### DCI-147 — Unsupported capability remains explicit
When no compatible pack/toolchain capability exists, DevCadence MUST report the unsupported/partial state and bounded fallback path; it MUST NOT represent generic model knowledge as deterministic ecosystem support.

### DCI-148 — Initial managed baseline requires deterministic acceptance
A newly created or adopted project MUST NOT enter normal managed feature work until authoritative module/capability bindings, canonical project documentation, and applicable ProjectHealthContract gates are established, or an explicitly authorized readiness exception exists.

### DCI-149 — Ecosystem support grows horizontally
Ordinary support for a new ecosystem MUST be expressible through the ProjectCapabilityPack contract plus fixtures/conformance unless that ecosystem exposes a genuinely new reusable core capability. Ecosystem identity alone MUST NOT require branches throughout core orchestration/state/authority.

### DCI-150 — Pack extension is declarative/Skills-first
ProjectCapabilityPacks MUST use versioned manifest data, Agent Skills-format knowledge, command/result mechanics, references, and fixtures as the default extension surface. Arbitrary in-process executable plugins MUST NOT be the default extension mechanism.

### DCI-151 — Pack Skills are typed, bounded, and subordinate to authority
Pack-provided Skills/prompt guidance MUST have explicit identity/revision, provenance, applicability, and context bounds. They MUST NOT expand authority, weaken system/project invariants or EWP constraints, or grant themselves mandatory status.

### DCI-152 — Pack composition is explicit
Capability-pack dependencies, compatibility, and conflicts MUST be resolved deterministically. Semantic conflicts MUST NOT be resolved by incidental load order.

### DCI-153 — Pack identity, origin, and upgrades are durable
An activated pack MUST retain logical identity, exact revision/content digest, source/origin locator when available, and validation provenance. A successor revision MUST be an explicit revalidated upgrade; active projects MUST NOT silently follow mutable pack sources.

### DCI-154 — Pack extensibility is proven by conformance
The extension model MUST be evaluated with materially different reference ecosystems and at least one unfamiliar synthesized ecosystem demonstrating that discovery/mechanics/Skills/health support can be added through a pack and conformance without ecosystem-specific core branches.

### DCI-155 — Pack producer is not the trust boundary
Built-in, human-authored, fetched, project-local, and model-generated packs MUST satisfy the same structural/compatibility contract. Evidence MUST preserve provenance/conformance class rather than treating producer identity as proof of correctness.

### DCI-156 — A candidate cannot rewrite or silently extend its own judge
A candidate that changes its ProjectHealthContract, debt baseline, resolved toolchain composition, activated pack content/digest, conformance fixtures/result adapters, or managed acceptance adapter MUST be evaluated under the accepted base judge. Proposed successor policy/capability MUST NOT govern acceptance of the same candidate that introduces it. New scope not covered by the base judge MUST be represented as uncovered and MUST NOT be treated as passing until successor coverage is separately accepted.

### DCI-157 — Pack/Skill authority is capped by active policy
A pack, Skill, manifest, or generated pack content MUST NOT grant itself filesystem/process/network/spending/tool authority beyond the active DevCadence policy/trust profile.

### DCI-158 — Successor-policy acceptance is capability-separated
A proposed successor health/pack/toolchain policy MUST be accepted by an authority structurally independent of the candidate producer. The producing worker MUST NOT possess, derive, invoke, or mutate the credential, API, IPC endpoint, UI action, state record, or other capability that records/promotes successor approval. Role labels, prompts, or an approval command reachable from the worker's own execution environment are insufficient separation.

## O. Workflow execution runtime invariants

### DCI-159 — WorkflowPlan is a logical contract, not a runtime trace
A WorkflowPlan MUST describe engineering obligations, ordering, role/independence requirements, deterministic gates, budgets and other policy-significant constraints rather than mirror one executor's private execution trace. An executor MAY use its own internal scheduling, decomposition, retry or waiting strategy, but MUST preserve every applicable DevCadence obligation and MUST NOT require executor-private topology to become canonical ProjectState solely for execution convenience.

### DCI-160 — Executor-private state cannot create authority
Executor-private state, memory, hypotheses, scheduling metadata, wait conditions, signals or equivalent mechanisms are non-authoritative. They MAY consume revision-pinned DevCadence facts/evidence by reference and MAY return candidate facts/evidence, but MUST NOT directly mutate accepted project truth, weaken policy, close independent review, expand source/spending/tool authority, replace deterministic validation, or establish acceptance. Policy-significant lifecycle and evidence outcomes cross the execution boundary through DevCadence-governed records.

### DCI-161 — Execution placement is not workflow semantics
A logical DevCadence task or WorkflowPlan stage MUST NOT derive engineering meaning or authority from the process, host, node, scheduler instance or executor that happens to perform it, unless an explicit locality/security/tool constraint is part of the task contract. The native executor MAY remain single-process and single-host. A future executor MAY use different placement internally without changing DevCadence workflow semantics; ownership, duplicate-execution, synchronization, recovery and consistency mechanisms remain executor concerns until a concrete DevCadence requirement makes them cross-boundary semantics.
