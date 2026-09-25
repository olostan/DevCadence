# DevCadence Agent Operating Directives

This file is normative for every human or AI agent modifying this repository. If a lower-level prompt conflicts with this file, stop and surface the conflict rather than silently overriding these rules.

## 1. Mission

DevCadence is an intelligent software-engineering control plane. Its job is to combine deep frontier-model reasoning with high-volume lower-cost execution cognition through typed protocols, evidence, deterministic verification, independent review, and auditable governance.

Local-first means local project authority, repository execution, canonical state and evidence. It does **not** mean every model inference must run locally.

Do not reduce the project to a generic coding-agent wrapper. Preserve the separation between:

- **Principal cognition:** product reasoning, architecture, alternatives, research, algorithms, pseudocode, detailed work-package design, high-risk decisions.
- **Execution cognition:** repository reconnaissance, implementation, debugging, repeated review and test generation through capability-routed local or policy-authorized remote endpoints.
- **Deterministic machinery:** Git, builds, tests, linters, static analysis, benchmarks, schema validation, policy checks.
- **Control plane:** canonical state, orchestration, task graph, risk classification, evidence, event history, routing, learning and promotion.
- **Consultants:** optional independent reasoning sources used deliberately, never automatically trusted and never tied to one mandatory provider.

## 2. Mandatory reading order

Before substantial changes, read:
1. README.md
2. INVARIANTS.md
3. docs/VISION.md
4. docs/ARCHITECTURE.md
5. docs/PROTOCOLS.md
6. docs/IMPLEMENTATION_PLAN.md
7. the specific domain document relevant to the task.

For work touching agent behavior, additionally read docs/PRINCIPAL_ENGINEER.md and docs/LOCAL_AGENTS.md.

For work touching state or schemas, read docs/PROJECT_STATE.md and all affected files under schemas/.

For security or external execution, read docs/SECURITY.md.

For environment discovery, cognition routing, setup, hardware/runtime detection or authentication, read docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md and docs/MODEL_RUNTIME.md.

For principal-host integration, read docs/PRINCIPAL_HOSTS.md and the relevant host-specific integration document.

For existing/brownfield project onboarding, read docs/PROJECT_ADOPTION.md. A merely registered repository is not automatically ready for normal DevCadence-managed work.

For changes to review, quality, or learning, read docs/VERIFICATION.md, docs/REVIEW_AND_CONVERGENCE.md, docs/REFACTORING_AND_HEALTH.md, and docs/LEARNING.md.

## 3. First principle: do not spend intelligence on repository noise

The system is explicitly designed so that frontier models do not repeatedly ingest large source trees, logs, compiler output, or unchanged context. Preserve this boundary.

Do not expose raw file-system primitives as the primary MCP contract to principals. Expose semantic engineering operations such as:
- project_state
- investigate
- create_work_package / propose_work
- delegate
- validate
- review
- request_evidence
- consult
- accept / reject
- record_decision
- promote_lesson

Raw evidence may be requested progressively when necessary, but should not be the default transport.

## 4. Frontier principal behavior is a correctness mechanism

Never optimize frontier behavior merely for fewer reasoning steps. The desired optimization is **high-value reasoning over compact, relevant context**.

For non-trivial decisions the principal must:
- distinguish facts, assumptions, inferences, and preferences;
- verify material assumptions;
- generate credible alternatives;
- challenge its preferred design;
- identify failure modes and counterexamples;
- request targeted repository evidence from local scouts;
- use external sources when claims depend on current libraries, standards, security guidance, APIs, or performance facts;
- use independent consultants when disagreement would materially increase confidence;
- revise the design after critique;
- document unresolved uncertainty;
- produce a detailed Engineering Work Package before implementation.

The principal must assume it can be confidently wrong.

## 4A. Product discovery and specification

When work begins from a fuzzy idea or changes user-visible/product semantics, read [docs/DISCOVERY_AND_SPECIFICATION.md](docs/DISCOVERY_AND_SPECIFICATION.md) and apply its protocol before architecture.

Agents must:
- preserve human authority for goals, preferences and acceptable tradeoffs;
- expose ambiguity rather than silently invent requirements;
- resolve factual uncertainties with tools/research/experiments when appropriate;
- record ProductDecisions and requirement provenance;
- use the Ambiguity Ledger for material unknowns;
- require Specification Readiness before substantial architecture;
- use independent specification review for substantial greenfield/product-semantic work.

Do not turn a fixed questionnaire into a substitute for adaptive discovery.

## 4B. Brownfield project adoption

When DevCadence is introduced to an existing repository, read [docs/PROJECT_ADOPTION.md](docs/PROJECT_ADOPTION.md).

Agents must not assume existing Markdown is canonical, and must not infer that repository registration means the project is ready for managed engineering.

Before normal managed implementation:
- pin the adoption source commit;
- inventory code/tests/configuration/history and inherited documentation;
- preserve provenance between observed, documented, inferred and human-confirmed statements;
- surface contradictions instead of silently choosing one source;
- materialize the required canonical documentation set in committed repository state;
- require Adoption Readiness.

Before READY, bounded investigation and isolated adoption work are allowed; normal autonomous implementation, acceptance and integration are not.

## 5. The first plausible solution is not enough

For systemic or architectural changes, do not accept the first coherent design. At minimum:
- identify two viable approaches;
- state why the selected approach is preferable under explicit criteria;
- perform one adversarial critique of the selected approach;
- verify the assumptions that would invalidate the choice.

Architectural work should often use multiple independent review vectors: simplicity, correctness, scalability, security, testability, operability, evolvability, and consistency with current project invariants.

## 6. Engineering Work Packages are executable design artifacts

Local implementers should not receive vague instructions such as “implement feature X.”

A substantial Work Package must include:
- objective and rationale;
- architectural intent;
- verified assumptions and evidence handles;
- relevant ADRs and invariants;
- MUST / SHOULD / SUGGESTED / LOCAL_DISCRETION constraints;
- implementation strategy;
- interface sketches;
- pseudocode or algorithm details when logic is non-trivial;
- code snippets when they materially reduce ambiguity;
- expected code areas and existing patterns to follow;
- explicit non-goals and forbidden changes;
- edge cases and failure modes;
- acceptance criteria;
- required tests and verification;
- escalation conditions;
- base commit / state revision.

See docs/PROTOCOLS.md.

## 7. Local agents may challenge but may not silently redesign

A local agent that finds a blueprint assumption to be false must return a contradiction or escalation report with exact evidence. It must not quietly reinterpret a MUST-level architectural requirement.

Local discretion is expected for:
- idiomatic decomposition;
- symbol naming;
- helper functions;
- local data structures;
- mechanically necessary adaptations;
- small refactors that do not change contracts or semantics.

Local discretion does not include:
- changing public contracts not authorized by the Work Package;
- adding new cross-layer dependencies;
- changing invariants;
- changing persistence semantics;
- changing security boundaries;
- introducing new external services;
- broad scope expansion.

## 8. Every accepted change requires evidence

A model saying “tests pass” is not sufficient. Capture deterministic evidence:
- exact command;
- exit status;
- relevant tool versions;
- base and head commits;
- test counts when available;
- lint/static-analysis outcomes;
- schema or API diffs where applicable.

Model review and deterministic validation are separate signals.

## 8A. Review convergence and closure

Review is evidence gathering, not a search for perfection.

For substantial candidate review:
- prefer independent reviewers examining the same immutable candidate in parallel;
- do not send raw reviewer suggestions directly to implementers;
- principal/adjudicator deduplicates and classifies findings before repair;
- consolidate all FIX_NOW findings into one Repair Work Package per repair round;
- after repair, run focused revalidation rather than another unrestricted broad review;
- raise the threshold required to reopen code as the campaign converges;
- treat OPPORTUNISTIC findings as future work, not current blockers;
- a frozen decision may be reopened only by materially new evidence or changed requirements;
- reporting zero closure-threshold findings is valid.

Review and repair rounds are bounded by policy. Hitting the bound with unresolved blockers escalates rather than creating an infinite loop.

See docs/REVIEW_AND_CONVERGENCE.md.

## 9. Reviewer independence

Reviewers should normally use a clean context and should not inherit the implementer’s reasoning chain.

A reviewer receives:
- Work Package;
- relevant invariants and ADRs;
- diff / candidate commit;
- focused source context when needed;
- deterministic validation evidence.

Review dimensions should be explicit. “Review the code” is weaker than independent correctness, architecture, security, test-adequacy and complexity reviews.

Disagreement is information and should be recorded, not averaged away.

## 10. Refactoring is planned work

Do not allow repeated feature delivery to indefinitely defer structural health.

Maintain the mechanisms needed for:
- code-health measurement;
- Refactoring Epochs;
- periodic Architecture Reconciliation;
- semantic duplication detection;
- dependency and API growth analysis;
- documentation-to-code drift detection.

Do not treat passing tests as proof of long-term maintainability.

## 11. Learning is proposal-based, never uncontrolled self-modification

Agents may generate LessonCandidates and PolicyExperiments from trajectories.

They may not directly rewrite normative prompts, rules, routing policies, invariants, or architecture based solely on a single run.

Promotion requires:
1. evidence from one or more trajectories;
2. conflict check against current invariants/ADRs;
3. evaluation or replay where feasible;
4. approval according to the policy level;
5. versioned persistence with provenance and rollback.

## 12. Git and isolation

Autonomous implementations must use isolated branches/worktrees once the worktree manager exists.

Local workers never merge directly to the protected main branch.

Every candidate change must be traceable to:
- task/work-package ID;
- base revision;
- implementation attempt;
- validation result;
- review result;
- acceptance decision.

## 13. Code-quality expectations

Until superseded by an ADR:
- control plane: Go;
- state store: SQLite;
- external and model integrations: adapters behind interfaces;
- protocol structures: strongly typed Go types plus versioned JSON Schema;
- MCP transport: thin adapter over application services, not business logic;
- CLI first; web dashboard later;
- structured logging, no printf-style operational state;
- context-aware cancellation for long-running processes;
- bounded concurrency;
- explicit process timeouts;
- no hidden global mutable state;
- deterministic tests wherever possible.

See ENGINEERING_STANDARDS.md.

## 14. Documentation synchronization

Architecture and protocol changes are incomplete until affected documentation is updated.

At minimum verify:
- README overview remains accurate;
- INVARIANTS.md is respected;
- docs/ARCHITECTURE.md reflects component boundaries;
- docs/PROTOCOLS.md reflects schema/state-machine changes;
- docs/IMPLEMENTATION_PLAN.md accurately reflects milestone status if changed;
- relevant JSON Schemas match prose contracts.

An implementation that changes behavior but leaves normative docs misleading is not done.

## 15. Failure behavior

Never hide uncertainty or force progress through an architectural contradiction.

When blocked:
- state the violated or uncertain assumption;
- cite evidence;
- separate observed fact from interpretation;
- provide bounded options;
- identify the decision owner;
- preserve the failed trajectory for later learning.

Repeated failure should escalate rather than produce infinite retries.

## 16. Current phase

M0 (normative baseline), M1 (domain core and canonical state), M2
(repository/worktree/process execution), M3A (environment intelligence and
cognition runtime) and M3B (guided deterministic bootstrap) are complete.

The control plane now has typed protocol records, an append-only engineering
event journal, deterministic ProjectState reduction, task/attempt state
machines, SQLite persistence, schema validation, real Git/repository
inspection, isolated worktrees, controlled external processes,
content-addressed artifacts, validation profiles, candidate metadata and
non-mutating integration checks. None of that requires a model runtime.

**M3A** adds environment intelligence and the cognition runtime. DevCadence
discovers hardware and software from operating-system facts, assesses accelerator
backend candidates as a pure function of those facts, discovers cognition
endpoints across local runtimes / authenticated coding CLIs / remote APIs,
verifies local acceleration only from an authoritative runtime signal produced by
real inference, and capability-routes roles deterministically under privacy and
cost policy. Everything in M3A is read-only; nothing mutates the machine.

When working on this area, note the contracts settled by
[ADR-0013](docs/adr/0013-environment-intelligence-and-cognition-contracts.md):
facts are separate from assessment, a capability grade requires provenance,
`verified` acceleration requires authoritative evidence and is refused by record
validation without it, machine profiles are computed rather than persisted, and
routing is a filter plus an explicit ordering with no score.

**M3B — guided deterministic bootstrap** is complete: `setup`/`doctor`,
remediation planning, installation recipes, credential references,
deterministic `ResourceInventory`, readiness projection, and a
plain/JSON/basic-terminal surface (WP-M3B-1 through WP-M3B-8, all accepted).
M3B computes factual readiness and resource inventory without embedding a
static deployment-profile chooser. A rich adaptive setup TUI was deliberately
not an M3B exit criterion.

The next milestone is:
- **M3C — cognition resource and session substrate** implements provider-neutral
  session drivers, access-channel capability contracts, `EconomicRegime`,
  `BudgetPool` / dynamic resource state, portfolio protocol shapes, and the
  deterministic portfolio validator/activation boundary.
- **M3D — adaptive portfolio and workflow synthesis** implements the
  AI-assisted Portfolio Planner, adaptive `WorkflowPlan` topology,
  re-recommendation/versioning/rollback, and the richer explain/setup UX.

Do not reintroduce the old assumption that M3 means "install one local model."
A strong local model—and even a local LLM at all—is optional capability.

After M3:
- **M4** is the adaptive-cognition vertical-slice **evidence gate**. It compares
  DevCadence allocation/workflow decisions against simpler baselines before
  broad productization; if the evidence is weak, revise M3D rather than
  preserving orchestration for its own sake.
- **M5** productizes the semantic MCP principal interface and first-class host
  portability for Antigravity, Cursor and Visual Studio Code.
- **M6** owns Project Adoption and Retrospective Reconstruction as a full
  brownfield milestone.
- **M7** implements bounded multi-review and consultant cognition.
- **M8** implements engineering health/refactoring epochs.
- **M9** implements evaluated learning and promotion/rollback.
- **M10** implements long-running autonomous campaigns.

ADRs 0011, 0012, 0013, 0014, and 0018 are accepted and normative for this
direction; `docs/IMPLEMENTATION_PLAN.md` is the canonical milestone sequence.

The overall implementation objective is still to prove the central hypothesis
without prematurely adding dashboard complexity, generalized distributed
scheduling, fine-tuning, or autonomous policy mutation.

## 17. Definition of done

A change is done when:
- its contract is satisfied;
- required deterministic checks pass;
- required review passes or disagreements are explicitly resolved;
- no invariant is silently violated;
- documentation and schemas are synchronized;
- provenance and decision records are available;
- the resulting system is simpler or at least no more fragile than before.

