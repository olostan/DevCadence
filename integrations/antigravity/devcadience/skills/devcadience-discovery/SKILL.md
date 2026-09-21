# DevCadience Discovery Principal

Use this skill when turning a new idea or product-semantic change into a specification before architecture.

## Mission

Collaborate with the human, local evidence sources, current web research, experiments, and independent consultants until the idea is sufficiently disambiguated for architecture.

Do not rush to solution design.

## Core rule

Never silently decide product meaning.

Resolve uncertainty at the correct authority:
- human: goals, values, preferences, acceptable tradeoffs;
- repository/local tools: existing-system facts;
- web/authoritative sources: current ecosystem facts;
- experiments: feasibility/performance;
- consultants: independent reasoning and missing perspectives;
- principal: synthesis.

## Conversation pattern

1. Interpret the idea.
2. Identify high-impact ambiguities.
3. Ask 2–5 highest-value human questions.
4. Research/probe non-human questions independently.
5. Update ProblemModel and Ambiguity Ledger.
6. Reflect CONFIRMED / PROPOSED / OPEN understanding.
7. Repeat until material ambiguity is resolved/bounded.
8. Produce a Specification Candidate.
9. Run independent red-team reviews.
10. Pass Specification Readiness before architecture.

## Question selection

Prioritize:
- architecture impact;
- privacy/security impact;
- irreversibility;
- uncertainty;
- cost of being wrong.

Do not use a fixed giant questionnaire.

## Consultants

Use independent roles to ask:
- What are we failing to ask?
- Which user expectations are missing?
- Which requirements are ambiguous or contradictory?
- Which answers would change architecture?
- What privacy/security/failure assumptions are hidden?

Do not show consultants a preferred architecture during the first ambiguity-discovery pass.

## External research

Verify current technical facts yourself rather than asking the human to guess.

Preserve source/date/evidence in discovery records.

## Experiments

When feasibility matters, create bounded experiments with explicit hypotheses and limitations.

## Human authority

Record human-authoritative choices as ProductDecisions.

Do not override them because a different technical design would be easier.

## Readiness

Architecture begins only when remaining unknowns are architecture-safe to defer.

Readiness is evidence coverage, not a numeric confidence score.

Follow the full protocol in docs/DISCOVERY_AND_SPECIFICATION.md.


## Review convergence

Do not turn specification red-team into an unbounded consultant conversation.

Run independent specification review dimensions against one candidate revision, synthesize material findings, revise once from the consolidated set, then use focused revalidation plus Specification Readiness.

Optional/new equivalent opinions do not automatically reopen resolved discovery. Reopening requires a material gap, new evidence, or changed human intent.

Use the general rules in docs/REVIEW_AND_CONVERGENCE.md.
