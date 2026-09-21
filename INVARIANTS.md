# DevCadience System Invariants

These invariants are non-negotiable constraints. They exist to prevent local optimization, model confidence, or implementation convenience from silently changing the character of the system.

Identifiers are stable. If an invariant is superseded, preserve the identifier and record the superseding ADR rather than renumbering history.

## A. Intelligence-boundary invariants

### DCI-001 — Frontier intelligence is used for high-leverage cognition
Frontier models own product reasoning, architectural decisions, alternative analysis, implementation strategy for substantial work, algorithms, pseudocode, interface design, and high-risk adjudication. They are not reduced to task routers.

### DCI-002 — Local intelligence absorbs high-volume repository cognition
Repository search, repeated source inspection, compiler/test loops, log interpretation, routine implementation, broad local review, and repeated verification should be local by default.

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

## C. Work-package invariants

### DCI-020 — Non-trivial implementation starts from an Engineering Work Package
Systemic and architectural changes must not be delegated to an implementer from an unstructured chat instruction.

### DCI-021 — Work Packages encode how, not only what
Where it reduces ambiguity, the principal must include implementation strategy, algorithms, pseudocode, interface sketches, examples, failure cases and test strategy.

### DCI-022 — Requirement strength is explicit
Guidance is classified as MUST, SHOULD, SUGGESTED, or LOCAL_DISCRETION.

### DCI-023 — Local agents may challenge assumptions
An implementer must be able to stop and report a contradicted blueprint assumption with evidence.

### DCI-024 — Local agents may not silently override MUST constraints
If the implementation cannot satisfy a MUST condition, the task is blocked or escalated.

### DCI-025 — Task scope cannot silently expand
Material scope expansion creates a revised Work Package or a separate task.

## D. Source-control and execution invariants

### DCI-030 — Autonomous work is isolated
Once worktree support is implemented, autonomous changes occur in isolated Git worktrees/branches based on an explicit base commit.

### DCI-031 — Local workers do not merge directly to main
Acceptance and integration are separate control-plane decisions.

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

## F. Architecture and governance invariants

### DCI-050 — Architecture changes are deliberate
Local workers cannot create, remove or materially redefine architectural boundaries, invariants, security boundaries, or durable public contracts without an authorized design decision.

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
DevCadience may eventually develop DevCadience, but self-changes follow the same isolation, validation, review and governance requirements as any other project.

