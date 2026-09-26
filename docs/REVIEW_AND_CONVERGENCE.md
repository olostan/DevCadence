# Review Campaigns, Convergence, and Closure

## Scope

This document defines how DevCadence obtains independent review without falling into unbounded reviewer/implementer/principal ping-pong.

Review is an evidence-gathering process, not a search for perfection.

> **A change is complete when material residual risk is bounded, not when further criticism becomes impossible.**

Repeated intelligent review can almost always produce another locally defensible improvement. DevCadence therefore needs an explicit **termination mechanism for cognition**.

## 1. Core principles

### 1.1 Optimize for bounded residual risk

DevCadence does not require zero conceivable criticism.

A candidate is closable when:
- deterministic validation passes;
- no blocking finding remains open;
- material disagreements are adjudicated or explicitly accepted as risk;
- required review dimensions are satisfied;
- remaining issues are deferred with clear ownership/boundary;
- another review round has low expected material yield.

### 1.2 Parallel review before serial repair

Prefer many independent reviews of one immutable candidate over a chain of review -> repair -> broad review -> repair.

~~~mermaid
flowchart TB
    Candidate["Immutable candidate commit"]
    Candidate --> Correct["Correctness review"]
    Candidate --> Arch["Architecture review"]
    Candidate --> Security["Security review"]
    Candidate --> Tests["Test adequacy review"]
    Candidate --> Simple["Simplicity / maintainability review"]
    Candidate --> Frontier["Independent frontier critic"]
    Correct --> Synth["Principal adjudication"]
    Arch --> Synth
    Security --> Synth
    Tests --> Synth
    Simple --> Synth
    Frontier --> Synth
    Synth --> Repair["ONE Repair Work Package"]
    Repair --> Implement["Repair implementation"]
    Implement --> Focused["Focused revalidation"]
    Focused --> Closure["Closure review"]
~~~

All broad reviewers should normally inspect the same candidate commit and Work Package revision.

### 1.3 Reviewers do not directly control implementation scope

Raw review findings are evidence, not commands.

The flow is:

~~~text
Reviewer findings
    -> Principal adjudication
    -> ACCEPT / REJECT / DEFER / HUMAN_DECISION
    -> consolidated Repair Work Package
    -> Implementer
~~~

The implementer should not independently negotiate every reviewer suggestion.

### 1.4 Adjudicated disagreement is not endlessly relitigated

Once the principal records a disposition with rationale and evidence, equivalent new opinion does not reopen it.

Reopening requires materially new evidence, changed requirements, a failed deterministic check, a newly discovered invariant conflict, or demonstrated correctness/security/integrity failure.

### 1.5 Dual independent review and the "Double-Green" adjudication fast-path

For systemic or high-risk candidates, DevCadence supports an optional **Dual Independent Review** ("2nd Point of View"):
- Two independent reviewer models (ideally from different model families, e.g. Claude and Gemini/OpenAI) evaluate the candidate commit in parallel, each starting from a clean context.
- **The "Double-Green" Adjudication Fast-Path**: If both independent reviewers return `PASS` with zero blocking findings AND all deterministic validation checks pass, the Principal receives an instant green card allowing immediate, frictionless closure. Double-Green is an **adjudication fast-path**, not an unmoderated bypass of human/principal authority (DCI-009) or deterministic closure prerequisites (`closure-decision.schema.json`).
- **Asymmetric Veto**: If any reviewer raises a `BLOCKING` finding in `security` or `invariants`, an Aggregator model **cannot** discard or override it. It can only be dismissed by explicit human disposition or deterministic falsification proof.
- **Aggregator Synthesis**: If findings exist or reviewers disagree, an Aggregator model (or Principal) deduplicates the findings, filters opportunistic nits, adjudicates tensions into standard `FindingDisposition` records, and compiles at most **one single consolidated `RepairWorkPackage`** per round. Implementers never argue directly with reviewers.

### 1.6 Cognitive freedom with silent multi-dimensional metering

Review prompts must **never** impose artificial turn limits (e.g. "you have 5 turns") on reviewer models. Turn countdowns induce "budget anxiety," causing models to rush, skip crucial caller verification, and hallucinate conclusions when running low on turns.
- Reviewer models are granted full cognitive freedom to inspect whatever files, conventions, or tests they need to reach certainty.
- Context runaway and resource exhaustion are bounded structurally at the runtime level via **Silent Multi-Dimensional Metering**:
  - Cumulative token caps (input, cached, output);
  - Wall-clock execution limits per operation;
  - Cumulative tool-call limits;
  - Semantic loop detection (identifying oscillating edits or repeating identical failed tool calls).
- When an outer budget is exhausted, the control plane does not rush the model; it pauses execution with `PAUSED_BUDGET_EXCEEDED`, checkpoints state, and escalates to the Principal/Human for disposition (DCI-045, DCI-049).

## 2. Review Campaign

A **ReviewCampaign** is the bounded lifecycle around one immutable candidate lineage.

It records:
- candidate commit;
- task / attempt / Work Package;
- review objectives/dimensions;
- reviewers/models;
- reporting threshold;
- findings;
- dispositions;
- repair rounds;
- focused revalidation;
- closure threshold;
- residual risks;
- outcome.

A campaign is not an open-ended conversation.

## 2A. Dynamic cognitive review lenses and active falsification

Code review is a multi-dimensional cognitive process, not a mechanical syntax linter. DevCadence guides reviewers through targeted **Review Lenses / Strategies** (ADR-0019 §4) applied across the stable `ReviewDimension` taxonomy (`correctness`, `architecture`, `invariants`, `security`, `test_adequacy`, `concurrency`, `performance`, `maintainability`, `other`):

1. **Anti-Rabbit Hole Lens (YAGNI & Simplicity)**:
   - Scrutinizes code for defensive bloat, speculative future-proofing, and over-engineering.
   - Replaces paranoid error-handling cascades with simple, clean assertions or fail-fast checks.
2. **Anti-Drift Lens (Scope Discipline)**:
   - Verifies that only authorized files and packages were modified.
   - Flags drive-by refactorings, unsolicited style tweaks in untouched code, and unapproved dependency additions.
3. **Anti-Hallucination Lens (Fact & Grounding Verification)**:
   - Verifies that cited symbols, functions, and CLI flags genuinely exist in the repository.
   - Checks that tests drive real execution paths rather than passing vacuously through tautological mocks.
4. **Architecture & Invariant Lens**:
   - Evaluates cross-layer coupling, security boundaries, and persistence semantics against durable project invariants (DCI compliance).
5. **Active Falsification (`FalsificationProbe` / Bounded Mutation Testing)**:
   - Reviewers can formulate targeted falsification probes (e.g. "temporarily disable this error check, invert this condition, or mutate this return value; verify tests fail").
   - The control plane's deterministic validation machinery executes the probe in an isolated worktree and returns hard evidence, converting reviewer suspicion into empirical proof.

Lenses are selected dynamically based on task risk (e.g. bug fixes emphasize Anti-Drift and Anti-Rabbit Hole; major features invoke Architecture and Anti-Hallucination).

## 2B. Bidirectional Work Package evolution (Living baselines) [Proposed - M3C]

An accepted Work Package is a **stable baseline, not an immutable dogma**.

If a local implementer or reviewer discovers that an upstream interface (e.g. from an earlier Work Package) is clunky, incomplete, or missing a parameter, the implementer is forbidden from building hacky workarounds or shims.

Instead, the worker emits a typed **`RefactoringProposal`** (ADR-0019 §3):
- Cites the upstream package and the specific architectural tension;
- Provides concrete compiler or test evidence;
- Outlines the proposed upstream interface refactor and affected callers.

The Principal adjudicates the proposal. When accepted, an atomic upstream refactor is applied cleanly, regression tests verify all callers, and the codebase stays unified and elegant.

## 3. Finding classes

### BLOCKING

Must be fixed before closure.

Typical examples:
- incorrect externally visible behavior;
- data loss/corruption;
- security boundary violation;
- invariant violation;
- invalid durable schema/event/state semantics;
- unverifiable acceptance/evidence lineage;
- unrecoverable canonical state;
- material requirement violation.

### MATERIAL_NON_BLOCKING

Real and material, but bounded enough to defer deliberately.

Examples:
- known scalability limitation outside current operating envelope;
- maintainability debt with bounded blast radius;
- incomplete observability that does not make current decisions unauditable;
- optimization that measurements do not yet justify.

A deferred material finding MUST retain risk, rationale, owner/target, and trigger for reconsideration.

### OPPORTUNISTIC

A valid improvement that does not justify reopening the current candidate.

Examples:
- naming/style refinement;
- speculative generalization;
- another reasonable abstraction;
- extra defensive behavior without a plausible current failure mode;
- documentation polish with no semantic ambiguity.

Opportunistic findings may become future backlog items, but MUST NOT extend the active repair campaign.

## 4. Severity and materiality are distinct

Severity describes harm if the issue is real.

Materiality/disposition answers whether it must change the current candidate.

A small patch can be BLOCKING when it changes a durable contract. A large cleanup can be OPPORTUNISTIC.

Important factors:
- current correctness impact;
- security/integrity impact;
- durability/compatibility;
- architecture impact;
- probability;
- blast radius;
- cost of fixing later;
- whether current milestone claims the affected behavior.

## 5. The "why now?" test

After the initial broad review, any finding proposed for current repair MUST answer:

> Why must this be fixed in this milestone/candidate?

Strong reasons:
- current invariant violation;
- current behavior is wrong;
- current evidence/acceptance is unsound;
- security/integrity failure;
- durable protocol would become expensive/incompatible to repair later;
- current milestone exit criterion would be false.

Weak reasons:
- cleaner;
- might be useful later;
- future subsystem may need it;
- another abstraction is aesthetically preferable;
- generalized support could be added now.

Weak reasons normally become deferred/opportunistic work.

## 6. Rising reopen threshold

Review tolerance becomes stricter as the candidate converges.

| Phase | Purpose | Minimum finding that may reopen code |
|---|---|---|
| Broad review | discover material weaknesses | MEDIUM/material or higher |
| Repair revalidation | verify repairs/regressions | HIGH/material or direct repair regression |
| Closure review | decide whether to freeze | BLOCKER / invariant-contract-security-integrity defect |

Projects may tighten thresholds for high-risk changes, but SHOULD NOT lower them merely because more reviewer capacity is available.

~~~mermaid
stateDiagram-v2
    [*] --> BroadReview
    BroadReview --> Adjudication
    Adjudication --> Repair: accepted current-campaign findings
    Adjudication --> ClosureReview: no repair required
    Repair --> FocusedRevalidation
    FocusedRevalidation --> Repair: repair regression / threshold issue
    FocusedRevalidation --> ClosureReview
    ClosureReview --> Repair: blocker discovered
    ClosureReview --> Frozen: closure gate passes
    Frozen --> Reopened: materially new evidence
    Reopened --> BroadReview
~~~

## 7. Repair rounds are bounded

Default:
- one broad review campaign;
- one consolidated repair round;
- one focused revalidation;
- one closure review.

Additional repair rounds require a threshold-crossing finding and explicit principal authorization.

A project may configure a maximum number of repair rounds. Hitting the limit with unresolved blockers escalates; it does not silently accept or continue forever.

## 8. Focused revalidation is not another unrestricted review

After repair, reviewers primarily answer:
- was each accepted finding actually fixed?
- did the repair violate an invariant or introduce a regression?
- do deterministic checks still pass?
- is evidence/lineage still valid?

They SHOULD NOT restart a general search for medium/low improvements.

One optional closure review may inspect broadly, but it reports only findings above the closure threshold.

## 9. Reviewer finding budget

Review prompts SHOULD request only the most consequential findings.

Recommended default:
- at most 5 material findings per reviewer;
- no quota that forces findings;
- lower-value observations omitted or placed in a non-blocking appendix.

The instruction is:

> Report at most the N most consequential findings. Reporting zero findings is valid. Do not invent findings to demonstrate usefulness.

Finding budgets control context size and force prioritization; they do not cap critical safety findings.

## 10. Principal adjudication

For every material finding, the principal chooses exactly one disposition:

- **FIX_NOW** — crosses current threshold and enters the Repair Work Package.
- **REJECT** — false, already covered, outside contract, or unsupported.
- **DEFER** — real but below current repair threshold; retain debt/risk and trigger.
- **HUMAN_DECISION** — changes product intent, risk appetite, or milestone scope.
- **DUPLICATE** — already represented by another finding.

The principal should synthesize related findings before repair so multiple reviewers do not create duplicate implementation churn.

## 11. Reopen Rule

A closed/frozen candidate may be reopened only by materially new information:
- deterministic validation failure;
- production/reproduction evidence;
- new or changed requirement;
- newly discovered applicable invariant;
- security/integrity/correctness defect;
- evidence showing an adjudicated factual premise was false;
- compatibility/durable-contract issue not previously represented.

The following alone do NOT reopen:
- another model prefers a different design;
- a consultant proposes a cleaner abstraction;
- restating an already adjudicated finding;
- a new reviewer assigns higher subjective severity without new evidence.

## 12. Freeze semantics

When ClosureDecision = FROZEN:
- current candidate review campaign terminates;
- opportunistic/material-deferred findings move to backlog/risk;
- ordinary new findings create new work rather than extending the campaign;
- only the Reopen Rule can reactivate the campaign.

Freeze is a governance state, not a claim of perfection.

## 13. Closure Gate

A closure decision should inspect structured evidence rather than ask "does everyone agree?"

~~~yaml
closure:
  deterministic_validation: pass
  required_review_dimensions: complete
  blocking_findings:
    open: 0
  material_findings:
    unresolved_without_disposition: 0
  reviewer_disagreements:
    unresolved_material: 0
  contract_changes:
    reviewed: true
  repair_regressions:
    open: 0
  residual_risks:
    bounded: true
  reopen_threshold: blocker
  outcome: frozen
~~~

**No blockers** is a valid completion criterion.

**No findings** is not required.

## 14. Context and token discipline

Rules:
- reviewers inspect an immutable candidate, not evolving chat narratives;
- reviewer outputs are structured and bounded;
- the principal receives deduplicated findings, not every raw transcript by default;
- implementers receive the Repair Work Package, not all reviewer conversations;
- focused revalidation gets finding IDs + changed evidence, not the entire campaign history;
- closure review gets compact campaign state plus candidate evidence;
- raw transcripts remain retrievable by reference when needed.

~~~mermaid
flowchart LR
    Raw["Many reviewer transcripts"]
    Normalize["Normalized findings"]
    Adjudicate["Disposition records"]
    EWP["Repair Work Package"]
    Repair["Repair agent"]
    Closure["Compact closure packet"]
    Raw --> Normalize --> Adjudicate --> EWP --> Repair --> Closure
~~~

## 15. Review yield and convergence telemetry

Track:
- new material findings per round;
- duplicate/rejected/opportunistic findings;
- repair regressions;
- context/tokens consumed;
- reviewer disagreement;
- reopened frozen campaigns;
- time/tokens per material finding.

Conceptually:

~~~text
Review Yield = new material findings / review effort
~~~

This is telemetry, not a hard mathematical stopping function.

Falling review yield plus a passing Closure Gate is evidence to stop.

## 16. Relationship to consultants

Consultants participate as independent reviewers/evidence sources and SHOULD run in parallel when possible.

After principal adjudication, a consultant's alternative opinion does not reopen a decision without new evidence.

For unresolved material disagreement, the principal may request targeted evidence, run an experiment, invoke a neutral tie-break consultation, or escalate to human authority.

Do not recursively show every consultant every other consultant response.

## 17. Human role

Human involvement is reserved for product/risk authority, scope changes, unusual residual-risk acceptance, policy exceptions, and unresolved material tradeoffs where the system lacks authority.

Humans SHOULD NOT arbitrate routine stylistic reviewer disagreement.

## 18. Anti-patterns

- sequential unrestricted broad reviews after every fix;
- forwarding every reviewer comment directly to implementers;
- "fix everything reviewers mention";
- requiring all reviewers to agree;
- reopening an adjudicated issue because another model expresses the same opinion;
- treating stylistic cleanup as a blocker;
- lowering the review threshold because context is still available;
- asking closure reviewers to "find more issues";
- allowing repair rounds without a maximum/escalation path;
- carrying the entire review transcript into every subsequent model context.

## 19. Completion condition

A ReviewCampaign terminates when:
1. required deterministic validation passes;
2. required review dimensions completed;
3. all threshold-crossing findings have dispositions;
4. accepted repairs are revalidated;
5. no closure-threshold finding remains;
6. residual risk is explicitly bounded;
7. ClosureDecision freezes the candidate.

At that point the correct next action is progress, not another unrestricted review.
