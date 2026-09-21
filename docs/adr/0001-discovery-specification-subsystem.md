# ADR-0001: First-Class Discovery and Specification Subsystem

- **Status:** Accepted
- **Date:** 2026-09-20
- **Decision owner:** Human + Principal
- **Supersedes:** none
- **Superseded by:** none
- **Related invariants:** DCI-004, DCI-005, DCI-052
- **Related tasks:** future bootstrap milestones

## Context

DevCadience originally specified a strong architecture/delivery pipeline once a project had enough product definition to design.

That left a dangerous gap at Day 0: a frontier principal could receive a fuzzy human idea and silently resolve product ambiguity through plausible assumptions before architecture even began.

Because DevCadience explicitly relies on frontier models for high-leverage reasoning, early misunderstood intent would be compressed into durable architecture and Work Packages, amplifying rather than correcting the error.

## Verified facts

- Human ideas are commonly incomplete and ambiguous at first articulation.
- Many seemingly small product ambiguities materially change architecture, privacy, persistence, latency, operational burden, or scope.
- Current DevCadience invariants already require material assumptions to be visible and challenged.
- Consultants, external research, local tools, and experiments can resolve different classes of uncertainty more appropriately than repeatedly asking the human.

## Assumptions

- A structured discovery phase can remain conversational rather than becoming a rigid questionnaire.
- A compact durable ProblemModel and Ambiguity Ledger can replace dependence on long chat history.
- Specification readiness can be assessed by resolved/bounded ambiguity rather than model confidence.

## Decision criteria

- preserve human product authority;
- reduce silent assumptions;
- avoid unnecessary human questions;
- keep discovery adaptive;
- maintain model/provider independence;
- produce compact durable specification artifacts;
- integrate cleanly with the existing Architecture Loop.

## Alternatives considered

### Option A — Rely on principal prompt quality only

**Benefits**
- no new protocol surface;
- simple.

**Costs / risks**
- important ambiguity remains ephemeral in conversation;
- no readiness gate;
- no provenance;
- difficult to audit why a requirement exists;
- easy for a new principal session to reinterpret intent.

**What would invalidate it**
- any need for persistent multi-session discovery or human-authoritative product decisions.

### Option B — Fixed onboarding questionnaire

**Benefits**
- predictable;
- easy to implement.

**Costs / risks**
- asks irrelevant questions;
- misses project-specific ambiguity;
- poor collaboration experience;
- prioritizes coverage over impact.

**What would invalidate it**
- projects with highly heterogeneous risk/architecture drivers.

### Option C — First-class adaptive Discovery & Specification subsystem

**Benefits**
- explicit ambiguity;
- dynamic question prioritization;
- human/tool/research/experiment/consultant authority classification;
- durable ProblemModel and ProductDecisions;
- independent spec red-team;
- readiness gate.

**Costs / risks**
- additional protocol/state complexity;
- discovery can become over-engineered if policy is too rigid.

**What would invalidate it**
- evidence that the added process materially slows low-risk work without improving specification quality.

## Decision

Adopt Option C.

DevCadience will treat Discovery & Specification as a first-class lifecycle phase before architecture for greenfield projects and product-semantic changes.

New durable protocol objects:
- ProblemModel;
- AmbiguityLedger;
- ProductDecision;
- Requirement;
- DiscoveryExperiment;
- SpecificationReadiness.

The Antigravity integration will provide a dedicated Discovery Principal skill.

## Rationale

The most expensive architecture error is often a correct implementation of the wrong product interpretation.

DevCadience's central thesis is to use frontier intelligence where it has leverage. Day-0 problem disambiguation is one of the highest-leverage places to spend frontier reasoning, consultant diversity, current research, and human collaboration.

## Consequences

### Positive
- product intent becomes durable and auditable;
- hidden assumptions become visible;
- consultants can help discover missing questions;
- humans are asked only questions that require human authority;
- architecture begins from a stronger baseline.

### Negative
- more protocol objects;
- additional state/readiness logic;
- requires careful UX so discovery remains conversational.

### New risks
- the Discovery Principal could over-question;
- readiness policy could become bureaucratic;
- low-impact ambiguity could block unnecessarily.

These risks are controlled through impact-based questioning and explicit safe deferral.

## Implementation guidance

Implement discovery objects in the domain/state layer before relying on them operationally.

Question priority should be policy-driven by impact/uncertainty, not a fixed form.

The Discovery Principal should work in small conversational batches and periodically reflect its current understanding.

## Verification plan

Evaluate on several fuzzy project ideas:
- seed known ambiguous requirements;
- measure whether the principal surfaces them;
- measure unnecessary questions;
- run independent red-team reviews;
- check whether a fresh principal can reconstruct intent from durable artifacts without full transcript.

## Rollback / supersession strategy

If the protocol proves too heavy, retain ProductDecision/Requirement provenance while simplifying the active Ambiguity Ledger/readiness machinery through a superseding ADR.
