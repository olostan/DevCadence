# ADR-0010: Bounded Review Campaigns and Explicit Closure

- **Status:** Accepted
- **Date:** 2026-09-21
- **Decision owner:** Principal + Human
- **Related invariants:** DCI-040–DCI-049
- **Related milestone:** M6, with normative rules effective immediately

## Context

Independent review improves quality, but repeated serial review can fail to terminate.

A powerful reviewer can almost always find another locally defensible improvement after each repair. If completion means "no reviewer can find anything else", review becomes an unbounded consumption of context, tokens, wall time, and code churn.

## Decision

DevCadence adopts bounded **ReviewCampaigns**.

Key rules:
- broad independent reviewers inspect the same immutable candidate in parallel where possible;
- reviewer findings are evidence, not direct implementation commands;
- the principal deduplicates/adjudicates findings before repair;
- accepted findings are consolidated into one Repair Work Package per round;
- revalidation after repair is focused on repairs/regressions;
- the threshold required to reopen code rises as the campaign converges;
- closure requires bounded residual risk, not zero findings;
- frozen decisions reopen only on materially new evidence or changed requirements;
- review/repair rounds are bounded and escalate when the bound is reached.

## Alternatives

### Endless broad re-review
Rejected: maximizes search coverage but has no convergence criterion and repeatedly mutates the candidate.

### Single reviewer only
Rejected: converges cheaply but sacrifices independent cognitive diversity.

### Fixed number of reviews with no materiality model
Rejected: bounds cost but can stop with real blockers or waste rounds on trivialities.

### Bounded campaign with evidence-based closure
Selected: preserves independent review while giving the system an explicit termination rule.

## Consequences

Positive:
- bounded serial mutation;
- lower context/token consumption;
- explicit residual risk;
- fewer duplicate repairs;
- reviewer disagreement remains auditable;
- campaign completion becomes machine-checkable.

Risks:
- an overly aggressive threshold could defer a real issue;
- severity classification itself can be wrong;
- closure policy may need stricter variants for high-risk projects.

Mitigation:
- deterministic failures/invariant/security/integrity defects remain blocking;
- durable-contract change-later cost is part of materiality;
- human escalation remains available;
- post-freeze new evidence can reopen.

## Implementation

Normative semantics: docs/REVIEW_AND_CONVERGENCE.md.

Durable contracts:
- ReviewCampaign;
- FindingDisposition;
- ClosureDecision.

M6 implements orchestration/policy enforcement; M1–M5 agents follow the normative stopping/reopen rules immediately.
