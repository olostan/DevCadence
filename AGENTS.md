# DevCadence Agent Operating Directives

Normative for every agent modifying this repository. Apply the conflict hierarchy in [docs/README.md](docs/README.md#normative-hierarchy); surface conflicts rather than silently choosing a convenient source.

## 1. Mission

DevCadence combines high-leverage Principal cognition, capability-routed execution cognition, deterministic verification and a local control plane holding canonical state, evidence and authority. Local-first concerns project authority, not mandatory local inference. Preserve these boundaries and optional independent consultants; do not reduce the project to a generic coding-agent wrapper.

## 2. Context admission, not mandatory corpus reading

Before substantial work, obtain or construct a task **Context Manifest**: role, task/EWP revision, base/candidate identity, read and write scope, applicable normative clauses, evidence references, explicit questions and escalation triggers.

Load this file, the applicable role template under `prompts/` (or Principal protocol clauses), the complete bounded EWP **Execution Contract**, and exact applicable normative clauses. Retrieve evidence progressively. An index or summary locates authority; it does not replace exact MUST/MUST-NOT text.

Do not preload README, invariants, architecture, protocols, milestones or whole ADR sets just because they are normative. Full-document reads require a specific question, architectural reconciliation, detected conflict or systemic design work. Record the reason and stay within the selected endpoint's admission envelope. Authority does not imply residency (DCI-018); delegation must have no hidden requirements (DCI-019).

Entering a new domain, discovering a new risk or proposing undeclared paths requires context re-resolution **before modification**. Context admission never grants write, network, credential or spending authority. Unknown mappings are unresolved context, not proof that no constraints apply.

WP-M3C-2B delivers the runtime Cognitive Invocation Compiler (`internal/cognition/compiler`) for deterministic admission of mandatory clauses from embedded INVARIANTS.md, authority-projection catalog authentication, prompt projection with delimiter safety, evidence leases, and state capsules. Until end-to-end runtime integration (WP-M3C-4) is wired into outer hosts, record the manifest in the EWP, PR or handoff and use targeted search/section reads manually. The compiler deterministically admits mandatory clauses and uses lexical/semantic retrieval only for optional discovery; similarity never decides whether a MUST applies. Keep control-plane rules out of prompts when Go can enforce them (DCI-131/132). See [ADR-0020](docs/adr/0020-cognitive-invocation-compiler-and-review-ledger.md), [docs/PROTOCOLS.md §10B](docs/PROTOCOLS.md#10b-adaptive-context-architecture-and-evidence-working-set-implemented---wp-m3c-1--wp-m3c-2b), and [docs/README.md](docs/README.md#context-routing).

## 3. Do not spend intelligence on repository noise

Principal interfaces are semantic: project_state, investigate, propose/create_work_package, delegate, validate, review, request_evidence, consult, accept/reject, record_decision and promote_lesson. Raw filesystem tools are not the primary Principal contract. Exact evidence remains progressively retrievable (DCI-014).

## 4. Principal reasoning is a correctness mechanism

Optimize relevant context, not reasoning effort. Distinguish facts, assumptions, inferences and preferences; verify material assumptions; consider credible alternatives and failure cases; challenge the preferred design; use targeted scouts and current external sources where needed. Independent consultants are optional evidence, not authority. Revise after critique and record uncertainty before producing the EWP. See applicable clauses of [docs/PRINCIPAL_ENGINEER.md](docs/PRINCIPAL_ENGINEER.md).

## 4A. Discovery and specification

For fuzzy ideas or product-semantic changes, apply [docs/DISCOVERY_AND_SPECIFICATION.md](docs/DISCOVERY_AND_SPECIFICATION.md): preserve human product authority, requirement provenance and the Ambiguity Ledger; resolve factual uncertainty with evidence; require Specification Readiness and independent review for substantial work before architecture.

## 4B. Brownfield adoption

Apply [docs/PROJECT_ADOPTION.md](docs/PROJECT_ADOPTION.md) when introducing DevCadence to an existing project. Pin source identity, inventory evidence, distinguish observed/documented/inferred/confirmed claims, surface contradictions, commit the canonical documentation baseline and require Adoption Readiness. Registration is not readiness. Before READY, only bounded investigation and isolated adoption work are authorized.

## 5. First-answer convergence is insufficient

For systemic or architectural work, compare at least two viable approaches under explicit criteria, adversarially critique the preferred choice and verify assumptions that could invalidate it. Consider correctness, simplicity, security, testability, operability and evolvability.

## 6. Engineering Work Packages

A substantial EWP has a bounded authoritative **Execution Contract** plus retrievable design/rationale. The contract includes objective, revision/base identity, allowed paths, MUST/MUST-NOT requirements, interfaces, acceptance, validation, escalation and exact normative references. Include algorithms/pseudocode and edge-case semantics needed for correct execution. No execution-critical constraint may live only in rationale. If the contract cannot fit, split work, choose an authorized capable endpoint or escalate; never silently truncate. See [docs/WORK_PACKAGES.md](docs/WORK_PACKAGES.md#execution-contract-and-context-manifest) and PROTOCOLS §7.

## 7. Challenge without silent redesign

Workers may adapt names, helpers, local data structures and idioms within the EWP. False assumptions require exact contradiction evidence and escalation. Changing public contracts, cross-layer dependencies, invariants, persistence/security boundaries, external services or scope requires explicit authorization and an amended EWP.

## 8. Accepted changes require evidence

Capture exact commands, exit status, relevant versions, base/head identities, counts where available and applicable lint/schema/API results. A model's “tests pass” is not evidence. Model review and deterministic verification are separate signals.

## 8A. Review convergence

Follow DCI-046–049 and DCI-134–135: review evidence is independent, authors do not self-verify, and durable finding/closure state—not conversational claims—controls convergence. The owning process is [docs/REVIEW_AND_CONVERGENCE.md](docs/REVIEW_AND_CONVERGENCE.md); do not duplicate its state machine in agent prompts.

## 9. Reviewer independence

Use clean, lens-specific context: complete acceptance/contract requirements, applicable exact clauses, immutable candidate/diff manifest and validation evidence. Fetch additional hunks, callers and dependants as needed. Do not inherit author reasoning or suppress blockers to meet a context budget. Record disagreements and uncovered areas; incomplete coverage is not PASS.

## 10. Planned structural health

Passing tests do not prove maintainability. Preserve health measurement, Refactoring Epochs, Architecture Reconciliation, duplication/dependency/API analysis and documentation drift detection under [docs/REFACTORING_AND_HEALTH.md](docs/REFACTORING_AND_HEALTH.md). Repository-local enforcement (hooks, CI, coverage-regression and documentation-integrity gates) is owned by [docs/ENGINEERING_HEALTH_POLICY.md](docs/ENGINEERING_HEALTH_POLICY.md); agents must not bypass or weaken those gates merely to land a candidate.

## 11. Governed learning

Trajectories produce LessonCandidates/PolicyExperiments, not direct autonomous changes to normative rules. Promotion requires evidence, conflict checks, evaluation/replay where feasible, policy-level approval, versioned provenance and rollback. Explicit human-authorized design changes are reviewed through the normal change process.

## 12. Git and isolation

Do not merge worker changes directly to protected main. Use isolated branches/worktrees under the applicable runtime manager; development of DevCadence itself follows [AGENT_HANDOFF_PROTOCOL.md](AGENT_HANDOFF_PROTOCOL.md), including its single-writer and no-force-push rules. Preserve task/EWP, base, attempt, validation, review and acceptance lineage.

**Hooks are mandatory setup for every agent, whatever the tool.** In every fresh clone or sandbox, before the first commit, run `make hooks-install` (idempotent; sets `core.hooksPath` to the versioned `.githooks/`) and confirm with `make hooks-check`. Git does not install hooks on clone, and one install per repository covers its worktrees. Do not bypass the hooks (`--no-verify`, `-c core.hooksPath=…`) to land a candidate. The only exception is bootstrapping a fix to the health scripts themselves, which run from `HEAD`: run the fixed `scripts/health/precommit.sh` manually against the exact staged tree and disclose the bypass and that evidence in the commit message and PR. If the install cannot run in your environment, say so in the PR or handoff; CI and the `main` ruleset remain authoritative. See [docs/ENGINEERING_HEALTH_POLICY.md](docs/ENGINEERING_HEALTH_POLICY.md#2-local-gates).

`CLAUDE.md` and `GEMINI.md` exist only to import this file for tools that do not read `AGENTS.md` natively; they must not hold rules of their own.

## 13. Engineering defaults

Go control plane, SQLite canonical storage, typed Go plus versioned JSON Schemas, integrations behind interfaces, thin MCP adapters, CLI first, structured logs, explicit cancellation/timeouts, bounded concurrency, no hidden global mutable state and deterministic tests. Apply relevant [ENGINEERING_STANDARDS.md](ENGINEERING_STANDARDS.md) clauses.

## 14. Documentation synchronization

Update affected owning contracts and references; verify overview, invariants, architecture, protocols, milestone status and schema agreement only where impacted. This is a dependency check, not a requirement to reload the corpus. A behavior change leaving normative prose misleading is incomplete. Future context linting is M3C work, not an existing command.

## 15. Failure behavior

State the uncertain/violated assumption, exact evidence, observation versus interpretation, bounded options and decision owner. Preserve failed trajectories; repeated failure escalates. Context shortage never authorizes guessing, truncating required clauses or relaxing policy.

## 16. Current phase

M0–M3B are complete. M3C adds cognition resources/session drivers, the Cognitive Invocation Compiler, context mediation, and compact durable review-state primitives; M3D adds adaptive portfolio/workflow synthesis; M4 is the empirical evidence gate. M5 covers semantic Principal integration/hosts; M6 adoption; M7 multi-review; M8 health; M9 evaluated learning; M10 autonomous campaigns. [docs/IMPLEMENTATION_PLAN.md](docs/IMPLEMENTATION_PLAN.md) owns detail and status. A local LLM is optional; no provider, subscription or host is mandatory. Prove the core hypothesis before dashboard/distributed-scheduling/training expansion.

## 17. Definition of done

Contract satisfied; required checks and independent review complete (or disagreements explicitly adjudicated); no silent invariant violation; affected docs/schemas synchronized; provenance available; system no more fragile than before. Opening a PR is a candidate for review, not acceptance.
