# Autonomous run 1 — retrospective

> **Historical snapshot, not current policy.** This records the first manual/autonomous DevCadence-style delivery experiment from 2026-10-03/04. Project status, open items, model names, timings, and process proposals below are preserved as experiment evidence and may now be obsolete. PR #44, which originally carried this retrospective, was never merged; later work absorbed or superseded many of its process ideas. Current normative behavior is defined by today's AGENTS.md, accepted ADRs, invariants, protocols, and implementation plan.


Run ledger: issue #36. Emulation of DevCadence automatic mode over pre-designed plans: Principal session + clean-context implementor and reviewer subagents, GitHub (PRs, comments, CI) as the durable record. Scope per owner: M3C close-out, then as far as possible without limit.

## What was delivered

| Work | PRs | Outcome |
| --- | --- | --- |
| M3C close-out: R1 independent acceptance | #38 | R1 reviewed (`ACCEPT_WITH_CONDITIONS`); acceptance record + corrected health note merged. Model-review signal only; owner confirmation pending. |
| WP-M3C-H2 fail-closed unknown resource/budget state (closes KG-1 for supplied-but-unknown state) | EWP #37, impl #39 | Merged `33525f0` after 2 reviewers and 1 test-only repair round. |
| WP-M3D-1A recommendation explanation/provenance fields | EWP #40, impl #41 | Merged `eb9f218` after 2 reviewers and 1 test-only repair round. |
| WP-M3D-1B validator-gated planner service | EWP #42, impl (in review at time of writing) | EWP merged `5f11254`. |
| This retrospective and process changes | this PR | — |

## What the data says

1. **The readiness gate earned its cost every time.** Every EWP I authored (3 of 3) had defects the independent readiness review found, and each of those would have forced a weaker implementer to invent semantics or fail an acceptance step. Examples: a constant name colliding with an existing one; a validator capability I asserted from memory that was false (and that also invalidated a design rationale); an acceptance criterion that could not hold (byte-equality); a design hole where the prompt omitted the identifiers the validator requires, so the model could never emit an acceptable portfolio; ownership of nested `schema_version` fields undefined. All three were found on paper, not after implementation.
2. **Revisions create new defects.** After fixing review findings, the re-review of the revised EWP found fresh contradictions (stale sentences contradicting the new decision, stale counts). A revision without a second independent review is not a reviewed EWP.
3. **A contract-correctness reviewer alone is insufficient.** H2 (#39) and 1A (#41) were accepted by the contract lens and then accepted *with conditions* by the mutation lens, which found real test gaps (surviving mutants: unpinned sort order, vacuous de-duplication assertion, half of a schema conditional untested, enum values not pinned). Their repairs were test-only, and the Principal re-ran sample survivors. No production defect surfaced on H2 or 1A after implementation. On 1B (#43) a third, robustness/fuzz lens found three production issues that neither the contract nor the mutation lens caught (decode amplification of ~500x memory from a 262 KB input, uncapped model-controlled rejection text, whitespace-only rationale accepted), and the mutation lens found a vacuous assertion in the prompt test. A well-closed EWP kept implementation clean, but input-parsing WPs need the third lens.
4. **Reviewer claims need Principal verification, and so do reviewer limits.** One reviewer reported ACCEPT while its own mutation runs had failed to execute (sandbox refused compound commands; a sed left an unused variable). Its "not verified" list was the honest part. Acceptance is scoped by that list.
5. **Implementors disclose well when the contract tells them to.** Deviations reported unprompted: an unneeded import swap, a reformatted schema file (confirmed semantically identical by parsed comparison), a cache deletion to pass the coverage gate.
6. **Cost shape** (from the Principal's session notes, not reproducible from GitHub). About 16 subagent runs of roughly 80-125k tokens each (~1.5M) delivered three WPs, plus the Principal's own work. Wall-clock was dominated not by model work but by the pre-push hook (5-8 minutes per push, duplicating CI) and the slowest mutation review (~20 minutes).

## Friction found (not model-quality problems)

Items marked (notes) come from the Principal's session notes and subagent reports, not from GitHub.

- The coverage gate is not deterministic: a cached base measurement exceeded a fresh measurement of identical code by 2 statements (13516 vs 13514 covered, about 0.0116 pp), failing the gate on the test-only repair commit of #39 (disclosed in that PR); the same effect appeared on a later docs-only amend (notes). Workaround used (and disclosed): delete the stale cache entry for the base SHA so both sides re-measure. Needs an owner decision (tolerance, fixed measurement, or excluding nondeterministic paths).
- The stop hook reports "unpushed commits" while the real push is still inside the multi-minute pre-push hook.
- The designated session branch still held already-merged history, so each cycle needed a force-with-lease; `.claude/worktrees/` (subagent worktrees) shows as untracked and trips the stop hook.
- Reviewers confused two-dot and three-dot diffs after `main` moved; every review prompt should state the base SHA and the three-dot form.
- The classifier correctly refused to record unverifiable acceptances and to bypass hooks; the run worked within that, and acceptance claims in docs were scoped as model-review signals.

## Autonomous decisions taken (with the options considered)

Authority note: all merges in this run were executed by the Principal session through the owner's GitHub account under the owner's explicit instruction to proceed autonomously without limit (issue #36 and the run request). Nothing here establishes a standing right to merge to `main`; AGENTS.md §12/§17 are unchanged. The GitHub record cannot distinguish Principal-executed from owner-executed merges, so #36 is the ledger. Readiness-review results for #37, #40 and #42 were not posted on those PRs at the time (they live in session notes, commit messages and the #36 comments); and #38 (docs recording the R1 review) was merged on green CI without a review of its own, the R1 review it records being a separate artifact. Both are practice gaps the protocol text now addresses.

| Decision | Chosen | Alternatives rejected |
| --- | --- | --- |
| Merging PRs (delegated run) | Principal, under the run's owner delegation, after CI green on the exact head and independent review closed | wait for the owner on each PR (defeats the exercise); self-merge without review |
| Split WP-M3D-1 | 1A protocol fields, 1B service, 1C (driver adapter, endpoint selection, compiler admission, historical evidence, activation link) deferred | one large WP (exceeds one EWP's closable scope) |
| Recommendation shape | additive optional fields on the existing record + `set_id` grouping | new `PortfolioRecommendationSet` kind (more registration, set invariants still need the service); breaking v2 |
| Planner invocation | one-method injected `Invoker`; endpoint binding by the caller | direct `SessionDriver` use (couples the planner to session lifecycle and forces undefined "planning capability" decisions) |
| Identity/provenance | all Go-assigned; model output cannot set ids, revision, timestamps, provenance | trust-and-validate (validation cannot prove truthful provenance) |
| Malformed output | whole-output rejection for envelope violations; per-alternative salvage for validator/record failures | salvage everything (a model that breaks the envelope is untrustworthy); reject everything on any failure |
| Credential exposure in the planner prompt | none (ids only for context profiles and budget pools, no state values) | include inventory wholesale (maximum exposure) |

## Process changes proposed by the original PR #44 (historical)

- The abandoned PR proposed an `AGENT_HANDOFF_PROTOCOL.md` "Autonomous delivery loop" section (merge authority only under explicit owner delegation, roles and isolation, per-WP sequence, mandatory mutation lens plus a robustness lens for untrusted-input parsers, second review after NOT_READY or any major, Principal verification of reviewer claims that supplements but does not replace independent verification, repair per REVIEW_AND_CONVERGENCE §1 with the run's ceiling stated as local policy, durable posting of readiness reviews, merge gate, evidence conventions, operational notes). These are proposals for owner review; this PR was itself reviewed by an independent accuracy/consistency reviewer, whose findings on overclaims, DCI-049 attribution, merge authority and force-push wording are incorporated.
- The abandoned PR proposed `IMPLEMENTATION_READY_TEMPLATE.md` additions: a "Verified facts about the current code" table, mutation catalog in Validation, and a consistency-sweep checklist after revisions. Mutation/review requirements later evolved independently; this salvage PR only re-evaluates the still-useful verified-facts and consistency-sweep ideas against the current template.

## Process v2 used/proposed during the run (historical)

Observation by the owner mid-run: reviewing individual EWPs took longer than implementing them, and a lot of time went into syncing EWPs. The data agrees: implementation lenses found no design flaw in H2, 1A or H3; the costly defects were seam defects between WPs (1B's prompt omitting identifiers the validator requires; the profile-to-binding gap) and wording drift introduced by revisions. Proposed process, with three refinements from this run:

1. Keep a rolling lookahead of 3-5 WPs and architecture-review the whole window together, at contract level only: interfaces, authority, failure and missing-input semantics, representability. Code-level facts (target strings, line numbers, fixture setups) are bound late, at implementation time against merged code, so lookahead EWPs do not go stale as earlier WPs land.
2. Make EWPs narrower and more mechanical. The implementor verifies the EWP's "verified facts" table as step 0 and reports any false row; the Principal no longer pre-verifies every code claim.
3. Reopen upstream architecture only on genuinely new evidence from implementation (DCI-048/049; escalation triggers).
4. Replace the second textual readiness review with an *implementability probe*: a reviewer implements the contract in a scratch copy and runs the suite. Both re-reviews that did this (1B r3, H3 r3) confirmed feasibility empirically and also found real defects (for example the nil profile entry panic).
5. Triage review findings: behavior-bearing (blocker/major) findings are fixed and probed again; wording-only findings are batched into one commit without a re-review. Do not spend further rounds on wording that cannot change implementation behavior.

Not changed here (owner decision): pushes through the pre-push hook took 5-8 minutes each and duplicate CI; in this run they cost more wall-clock than any review round.

Other lessons added late in the run: a finished subagent with a lingering background process re-sends its hand-back repeatedly (about a dozen duplicates from one implementor), so implementors are told to send exactly one final report and leave no watchers, and a finished agent is stopped; `make docs-check` should be run before committing docs because the link checker reads raw text (a bracketed index immediately followed by a parenthesis, as in some diagnostic target strings, parses as a link); the Principal's own consistency sweep missed a stale "readiness: pending" line that the re-review caught, so the sweep checklist must name the Status and Readiness lines explicitly.

## Open items for the owner (not decided autonomously)

1. Coverage gate nondeterminism and the pre-push/CI duplication (policy change to a protected gate).
2. Whether R1 acceptance and the model-review signals recorded in this run count toward milestone acceptance.
3. Remaining known gaps from WP-M3C-4: KG-2 (opaque-usage uncertainty), KG-3 (WriteScope enforcement), KG-4 (lease invalidation wiring); the nil-map residual of KG-1; the same nil-policy defect at `portfolio_validator_constraints.go:141` (`RequireVerifiedAcceleration`).
4. Reported by the 1B re-reviewer, not yet verified by the Principal: the validator does not appear to check that a context profile referenced by a role binding belongs to that binding's endpoint/channel, so a mismatched profile id can pass and its `MinContractLimitTokens` is not enforced. Candidate follow-up WP (validator consistency).
5. WP-M3D-1C scope decisions: "minimum planning capability" definition and endpoint selection, driver adapter, compiler-backed invocation digest, historical-evidence input, activation-record link to the recommendation.
6. A deterministic mutation helper (a `make` target that applies a declared mutant list and reports survivors) would remove the dependence on reviewer sandbox permissions and make the mutation lens reproducible.
7. Review records: all roles posted under one GitHub account; recording role, lens and head SHA in each post (what `ActorProvenance` models) was done by convention here and should be mechanical in the product.
