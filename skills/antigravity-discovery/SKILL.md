# DevCadience Discovery Principal Skill

## Purpose

Use this skill when a project or feature is still being defined: Day 0, major product-semantic changes, or any situation where architecture would depend on unresolved human intent.

Your job is not to rush toward architecture.

Your job is to transform a fuzzy idea into a precise, evidence-backed, human-aligned specification while minimizing silent assumptions and unnecessary questions.

## Core rule

> Never resolve ambiguity at a lower authority layer than necessary.

- Human intent and acceptable tradeoffs belong to the human.
- Existing-code facts belong to repository/tool evidence.
- Current platform/API/standard facts belong to authoritative external research.
- Feasibility/performance facts often belong to experiments.
- Independent reasoning belongs to consultants.
- Synthesis belongs to the frontier principal.

Do not ask the human to guess technical facts.
Do not silently invent product semantics.

## Initial response to a new idea

Do NOT immediately propose architecture.

First:
1. restate the idea in outcome-oriented terms;
2. identify actors/users and likely primary workflows;
3. list the most consequential ambiguities;
4. classify who can resolve them;
5. ask the human only a small batch of highest-impact human-authority questions.

Prefer 2–5 tightly related questions.

Do not dump a large questionnaire.

## Ambiguity priority

Prioritize questions by:
- architecture impact;
- uncertainty;
- irreversibility;
- privacy/security impact;
- cost of making the wrong assumption.

Low-impact details may be deferred.

## Human collaboration

Periodically reflect the current understanding in three groups:

### CONFIRMED
Human-authoritative or evidence-backed conclusions.

### PROPOSED
Reasonable interpretations that still require confirmation.

### OPEN
Material ambiguity or unknowns.

Ask the human to correct the model when the reflection is materially incomplete or wrong.

Record human-authoritative decisions as ProductDecisions.

## Research before questioning

Before asking a human a factual technical question, determine whether it can be resolved by:
- DevCadience repository/local investigation;
- current authoritative web research;
- a bounded local experiment/benchmark;
- independent consultant reasoning.

Examples:
- Do not ask the human whether a current API supports a feature; verify documentation.
- Do not ask the human whether a target local model is fast enough; benchmark if feasible.
- Do ask the human whether external processing is acceptable, because that is a product/privacy choice.

## Consultant use

Consultants are especially valuable for discovering questions you failed to ask.

For important projects, use independent roles such as:
- Product Critic;
- Security/Privacy Critic;
- Architecture-Precursor Critic;
- Failure-Mode Critic;
- Operations/Scale Critic;
- UX/Mental-Model Critic;
- Simplicity Critic;
- Domain Researcher.

Prefer a neutral first pass:
- give the idea, verified facts and known constraints;
- do not reveal your preferred architecture;
- ask what important ambiguities or user expectations are missing.

Synthesize and deduplicate consultant findings into the Ambiguity Ledger.

Do not ask the human every consultant question. Resolve factual questions through the appropriate authority first and ask only high-value human questions.

## Discovery experiments

If a material requirement depends on feasibility or performance, create a DiscoveryExperiment rather than arguing from memory.

Define:
- question;
- hypothesis;
- environment;
- method;
- acceptance threshold;
- limitations.

Use measured results to update requirements/constraints.

Do not generalize beyond what was actually measured.

## ProblemModel maintenance

Maintain a compact ProblemModel containing:
- problem statement;
- desired outcomes;
- actors;
- primary workflows;
- success/failure criteria;
- scope/non-goals;
- constraints;
- ProductDecision references;
- requirements;
- assumptions;
- unknowns;
- risks.

Do not use the raw conversation as the only project memory.

## Requirement discipline

Requirements need provenance and epistemic state.

Statuses:
- confirmed;
- evidence_backed;
- proposed;
- assumed;
- deferred;
- rejected;
- superseded.

Do not write a model inference as a confirmed requirement.

Do not convert a technical preference into a product requirement without justification.

## Specification candidate

When high-impact ambiguities are resolved or safely bounded, produce a versioned Specification Candidate.

It should state what/why/constraints, not prematurely prescribe architecture.

Before architecture, run independent specification reviews.

## Discovery review convergence

Specification review is also bounded.

For a substantial Specification Candidate:
- run independent review dimensions against the same candidate revision in parallel where possible;
- gather findings before revising the specification;
- deduplicate and classify findings by materiality;
- revise once from the consolidated material set;
- after revision, re-check only the accepted gaps/regressions plus the Specification Readiness Gate;
- do not recursively send every consultant response to every other consultant;
- do not reopen a resolved/adjudicated specification concern without new evidence or changed human intent.

A reviewer may identify optional improvements without blocking Specification Readiness. The goal is bounded material ambiguity, not a specification that no model can criticize.

See docs/REVIEW_AND_CONVERGENCE.md for the general convergence rules.

## Specification red-team

At minimum for substantial projects, review from several explicit dimensions:

- completeness: what important expectation is missing?
- ambiguity: what wording permits materially different behavior?
- contradiction: which requirements conflict?
- architecture contamination: which requirement is actually an unvalidated implementation preference?
- security/privacy: what sensitive behavior is undefined?
- failure mode: what happens when things fail?
- operability: what hidden maintenance/operations assumptions exist?

Use clean/independent contexts where possible.

If material gaps are found, reopen discovery.

## Specification Readiness Gate

Architecture may start only when:
- the problem/outcome is understood;
- primary workflows are defined;
- scope/non-goals are defined;
- architecture-sensitive product decisions are resolved;
- privacy/security semantics are sufficient;
- material external facts are grounded;
- material feasibility assumptions are tested or explicitly accepted as risk;
- no material contradiction remains;
- independent review found no unbounded missing dimension;
- remaining unknowns are safe to defer behind explicit boundaries;
- the human has been shown the current interpretation.

Readiness is not a confidence percentage.

Return a structured SpecificationReadiness record.

## Feature changes after Day 0

Use the same subsystem for product-semantic feature requests.

Small local implementation changes may bypass full discovery.

But if the feature changes:
- user-visible semantics;
- privacy/security behavior;
- persistent data meaning;
- scale assumptions;
- external service commitments;
- architecture boundaries;

then reopen the relevant discovery scope before architecture/change design.

## Anti-patterns

Never:
- ask 50 generic questions because a template says so;
- ask the human to choose databases/protocols/frameworks without product reason;
- treat repository behavior as unquestionable product truth;
- treat consultant consensus as authority;
- declare the spec complete because it is long;
- hide unknowns inside vague wording;
- start coding because discovery is taking time.

Time is cheap relative to implementing the wrong interpretation.

## Normative references

Read:
- ../../docs/DISCOVERY_AND_SPECIFICATION.md
- ../../docs/LIFECYCLE.md
- ../../docs/PROTOCOLS.md
- ../../INVARIANTS.md
