# ADR-0001: First-Class Discovery and Specification Subsystem

- **Status:** Accepted
- **Date:** 2026-09-20
- **Decision owner:** Human + Principal
- **Supersedes:** none
- **Superseded by:** none
- **Related invariants:** DCI-004, DCI-005, DCI-052
- **Related tasks:** future bootstrap milestones

## Context

DevCadence originally specified a strong architecture/delivery pipeline once a project had enough product definition to design.

That left a dangerous gap at Day 0: a frontier principal could receive a fuzzy human idea and silently resolve product ambiguity through plausible assumptions before architecture even began.

Because DevCadence explicitly relies on frontier models for high-leverage reasoning, early misunderstood intent would be compressed into durable architecture and Work Packages, amplifying rather than correcting the error.

## Verified facts

- Human ideas are commonly incomplete and ambiguous at first articulation.
- Many seemingly small product ambiguities materially change architecture, privacy, persistence, latency, operational burden, or scope.
- Current DevCadence invariants already require material assumptions to be visible and challenged.
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

DevCadence will treat Discovery & Specification as a first-class lifecycle phase before architecture for greenfield projects and product-semantic changes.

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

DevCadence's central thesis is to use frontier intelligence where it has leverage. Day-0 problem disambiguation is one of the highest-leverage places to spend frontier reasoning, consultant diversity, current research, and human collaboration.

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

---

## Implementation note (M1)

The M1 control plane implements the durable half of this decision: the six
record types, the eight discovery events, and the `ProjectState.discovery`
projection. The discovery *workflow* — questioning, human reflection,
experiments, specification review — is not implemented and needs a model
runtime (M3) and the MCP surface (M4).

Where this ADR's principles could be enforced mechanically rather than left
to agent behaviour, they are:

- **DCI-008 / DCI-015, requirement provenance.** A requirement may be
  `confirmed` only when its source type is `product_decision` or
  `human_statement` *and* it carries a non-empty `source.ref`. Naming a
  human-originating source type is a claim about provenance, not provenance;
  a confirmed requirement pointing at nothing cannot be traced back to the
  human it invokes. Where the ref names a ProductDecision, the control plane
  additionally verifies that decision exists in the same project and has not
  been withdrawn, so a requirement cannot claim authority from a decision
  nobody recorded or from one the human took back. Both the journal path and
  the record-persistence path enforce this: a rule enforced on one write path
  only is a rule a caller can choose to avoid, since a durable record can be
  stored alongside an unrelated event. For
  `human_statement`, M1 has no durable transcript record, so the rule is that
  *some* retrievable handle is required; tightening it to a specific record
  kind waits for the milestone that introduces one.
- **DCI-009, human product authority.** `ProductDecision.authority` is pinned
  to `human`, and `ProblemModel.human_reflection_revision` must fall between 1
  and the current revision — the human cannot be recorded as having reviewed a
  revision that did not exist when they looked.
- **DCI-016 and §5, safe deferral.** An ambiguity may be deferred only behind
  a boundary: `AmbiguityResolved` with outcome `explicitly_deferred` requires
  `safe_deferral_boundary`, and a `SpecificationReadiness` unknown marked
  `architecture_safe_to_defer` requires an explicit `boundary`. A bare "safe
  to defer" is an assertion, not the boundary that makes it safe. A readiness
  verdict also does not survive the ProblemModel revision it judged.

These are write-path refusals, not advisory checks, so an agent cannot record
the weaker claim at all.

## Durable specification-review evidence (M1 addendum)

`SpecificationReviewCompleted` originally referenced a `ReviewResult` by
digest. That was a semantic fiction: `ReviewResult` is implementation evidence
requiring `attempt_id` and `work_package_id` and using implementation review
dimensions, while a specification review happens before either exists and uses
the discovery dimensions this ADR's protocol defines. The optional digest hid
the mismatch, because callers could simply omit it — but when present, it
claimed a document that could not represent the event.

M1 therefore introduces `SpecificationReviewResult` as its own protocol record
and JSON Schema, and repoints the event at it. The alternative — reusing
`ReviewResult` with empty attempt and work-package fields — was rejected: it
would make the two kinds of evidence structurally interchangeable, which is
exactly the distinction this ADR exists to preserve, and optional-everything
records cannot be validated meaningfully.

The *workflow* that produces these records still does not exist; that remains
M3/M4 (FR-D-006…010). What exists now is the durable contract, so the workflow
arrives into a protocol that already says what its evidence must look like.
