# Review Campaigns, Durable Findings, and Convergence

## Scope

This document defines DevCadence review behavior. ADR-0020 owns the architectural decision that review state is durable control-plane state rather than conversational history.

The goal is independent criticism without endless reviewer/implementer ping-pong.

> **Review gathers evidence. The control plane decides whether the candidate has satisfied closure policy.**

A candidate is complete when material residual risk is bounded, not when no further criticism can be imagined.

## 1. Default campaign shape

Prefer parallel review of one immutable candidate, followed by one consolidated repair and focused verification.

~~~text
immutable candidate + contract revision
          |
          +--> independent review A
          +--> independent review B
          +--> additional required lenses
          |
          v
 finding normalization / deduplication
          |
          v
     Review Ledger
          |
          v
 one consolidated Repair Packet
          |
          v
  attempted fixes / challenges
          |
          v
 independent per-finding verification
          |
          v
    one closure review
          |
          v
         freeze
~~~

The normal campaign is:

1. broad independent review;
2. normalization/adjudication;
3. at most one consolidated repair packet;
4. focused verification;
5. closure decision.

Additional repair rounds require a threshold-crossing material finding, repair regression, changed contract, or materially new evidence.

## 2. ReviewCampaign and compatibility

The existing durable `ReviewCampaign`, `FindingDisposition`, and `ClosureDecision` records remain canonical compatibility surfaces. Raw reviewer observations continue to originate in `ReviewResult.findings`. This design adds stable normalized finding identity and resolution-verification evidence between those existing stages; it does not create a parallel campaign system.

A ReviewCampaign is anchored to:

- immutable base/candidate identity;
- task/attempt/Work Package ID and contract revision/digest;
- required review dimensions/lenses;
- reviewer independence requirements;
- deterministic validation evidence;
- stable finding IDs;
- current campaign/closure state.

The campaign is not a chat thread. Comments and model transcripts are evidence attached to canonical state.

## 3. ReviewFinding

A material finding must be specific enough to verify independently. A normalized ReviewFinding gives stable identity to one or more raw `ReviewResult.findings` observations and is the object referenced by the existing `ReviewCampaign.finding_refs` / `FindingDisposition.finding_id` lineage.

Minimum semantic fields:

~~~yaml
finding_id: RF-...
candidate_commit: ...
contract_revision: ...
severity: info | low | medium | high | critical
materiality: blocking | material_non_blocking | opportunistic
confidence: high | medium | low
claim: ...
evidence:
  - ...
requirement_refs:
  - DCI-...
impact: ...
why_now: ...
verification_method: ...
source:
  reviewer: ...
  lens: ...
status: open
~~~

The serialization is illustrative. The typed protocol may use different field names.

### Severity, materiality, and confidence

Canonical severity and materiality remain compatible with the existing FindingDisposition schema. **Severity** estimates harm if the finding is true; **materiality** determines current-campaign significance (`blocking | material_non_blocking | opportunistic`); optional **confidence** records evidence strength. FindingDisposition preserves the normalized finding's severity/materiality rather than inventing another classification vocabulary.

**blocking**

The current candidate cannot close if the claim is true. Typical reasons:

- correctness/security/integrity failure;
- MUST/invariant violation;
- invalid durable schema/state semantics;
- acceptance evidence is unsound;
- current milestone contract cannot be represented or satisfied.

**material_non_blocking**

A real issue with bounded current risk. It may be fixed now or explicitly deferred with owner/trigger.

**opportunistic**

An improvement, alternative, or future idea that does not justify extending the current campaign.

Severity and materiality are not identical. A stylistically large change can be opportunistic; a one-line durable-contract defect can be blocking.

## 4. Finding admission

Reviewer prose is not automatically implementation scope.

A material finding must provide:

1. concrete claim;
2. affected artifact/behavior;
3. evidence;
4. applicable requirement/invariant or engineering rationale;
5. consequence if unfixed;
6. materiality;
7. independent verification method.

Findings that cannot satisfy this shape remain observations or opportunistic suggestions until clarified.

### Why-now test

A current-campaign repair should normally be justified by one of:

- current invariant/requirement violation;
- current behavior is wrong;
- current evidence or acceptance is unsound;
- security/integrity risk;
- durable protocol would become materially harder to repair after freeze;
- milestone exit claim would otherwise be false.

"Cleaner", "more generic", or "might be useful later" is normally opportunistic/deferred.

## 5. Normalize before repair

Multiple reviewers may describe the same underlying defect differently.

The Principal/Aggregator should create one canonical finding and retain reviewer observations as supporting evidence rather than giving the implementer duplicate tasks.

Example:

~~~text
reviewer A: "fallback has no billing semantics"
reviewer B: "fallback endpoint does not identify access/economic path"

          ↓ normalize

RF-021: fallback is not a complete routable/economic binding
supported_by: [A-7, B-12]
~~~

The implementation packet consumes the canonical finding, not every transcript.

## 6. Resolution is not verification

After the existing FindingDisposition selects a current-campaign repair (`fix_now`), an implementer responds with exactly one semantic outcome:

- **fix_attempted** — candidate changed; provide commit/files/evidence;
- **challenge** — claim is false, inapplicable, outside the current contract, or belongs at another boundary; provide argument/evidence.

An implementer MUST NOT mark its own resolution verified (DCI-134).

The runtime verifies that every required finding received a response; the implementer does not need the full lifecycle rules in its prompt.

## 7. Independent verification

### Fix verification

A clean verifier receives:

- finding;
- relevant contract/mandatory clauses;
- old/new candidate identity and focused diff/evidence;
- deterministic validation evidence relevant to the finding.

It answers whether the original claim is:

- verified fixed;
- not fixed;
- partially fixed;
- invalidated by stronger deterministic evidence.

Focused verification is not another unrestricted broad review.

### Challenge verification

A challenge is evaluated in an unbiased context containing:

- original finding claim/evidence;
- challenge argument/evidence;
- exact applicable normative clauses;
- only the additional evidence needed to decide.

The verifier-facing projection is blinded by default to reviewer/challenger identity and model/provider. The control plane separately enforces independence and reveals identity only when it is materially relevant evidence. Do not prime the verifier with "the author says reviewer X was wrong" or load the conversational transcript.

Conceptual terminal outcomes:

- `verified_fixed`;
- `verified_dismissed`;
- `verified_deferred`.

A deferment names a destination owner/WP, why deferral is safe now, and a reconsideration trigger. "Future work" alone is not closure.

## 8. Review independence

Reviewers start from clean role/lens-specific Context Packs and do not inherit author reasoning.

For systemic/high-risk candidates, independence may require:

- different endpoint/model family;
- different provider/access path where policy requires;
- different review method/lens;
- clean starting context.

Independence is a routing/control-plane policy. The reviewer should see only the independence-related information needed for its assignment.

Security/invariant blockers cannot be waived merely by an Aggregator preference. They must be fixed, proven false/inapplicable with evidence, or explicitly escalated to the authorized decision owner.

## 9. Closure review and reopen threshold

Closure review exists to detect:

- repair regressions;
- blockers missed by the initial campaign;
- contract changes introduced during repair;
- materially new evidence.

A new closure finding should record origin:

- `repair_regression`;
- `previously_missed_material_defect`;
- `contract_change`;
- `new_evidence`.

Equivalent restatements of already adjudicated findings do not reopen a campaign.

A frozen candidate may reopen only for materially new information such as:

- deterministic validation failure;
- reproduction/production evidence;
- changed requirement;
- newly applicable invariant;
- security/integrity/correctness defect;
- evidence that an adjudicated factual premise was false;
- previously unrepresented durable compatibility problem.

Another model preferring a different abstraction is not new evidence.

## 10. Closure gate

The control plane evaluates structured state rather than asking agents whether "everything was addressed."

Conceptually:

~~~yaml
closure:
  candidate_identity: immutable
  deterministic_validation: pass
  required_review_dimensions: complete
  blocking_findings:
    unresolved: 0
  material_findings:
    without_verified_disposition: 0
  repair_regressions:
    unresolved: 0
  residual_risk:
    bounded: true
  outcome: frozen
~~~

Agents report evidence and attempted work. DevCadence computes closure.

"No blockers" is a valid completion condition. "No possible findings" is not.

## 11. Contract completeness review

For systemic/durable protocol work, the Principal/contract author performs Contract Completeness Review before implementation as an effective-now manual evidence step; M7 may automate it later.

The purpose is representability, not code correctness:

- enumerate the owning requirements/clauses;
- map each required concept to protocol/schema representation;
- identify missing semantics before implementation;
- challenge accidental provider/model coupling;
- identify validation that is intentionally deferred to a later cross-record/runtime boundary.

Prefer a compact matrix:

| Requirement | Required concept | Representation | Boundary |
| --- | --- | --- | --- |
| FR-X | fallback economic path | `FallbackBinding` | record-local |
| PROTOCOL-Y | session requirement | `WorkflowStage` | protocol |
| DCI-Z | activation authority | validator | later runtime |

An unresolved empty cell on a MUST is a blocker to implementation/freeze.

## 12. Context and token discipline

Review Context Packs follow ADR-0020 and ADR-0019:

- complete bounded Execution Contract;
- exact applicable mandatory clauses;
- immutable candidate/diff identity;
- assigned dimensions/lenses;
- validation summaries/handles;
- focused initial evidence;
- progressive EvidenceLeases for explicit questions.

Do not preload the repository or previous review conversations merely because they exist.

Do not impose artificial "you have N turns" countdowns. Runtime meters tokens/tool calls/wall clock silently and checkpoints/escalates on exhaustion.

A finding-output budget may prioritize presentation but never suppress a blocker or convert incomplete coverage into PASS.

## 13. Dynamic review lenses

Lenses are selected from task risk rather than made resident in every review prompt.

Examples:

- correctness/failure-first;
- architecture/invariants;
- security;
- test adequacy/falsification;
- concurrency/performance;
- anti-drift/scope discipline;
- anti-rabbit-hole/simplicity;
- grounding/anti-hallucination.

M7 may automate lens selection, parallel fan-out, mutation/falsification probes, and aggregation. The Review Ledger semantics apply before M7; M7 expands orchestration rather than replacing them.

## 14. Bounded cognition and campaign health

DevCadence should record campaign metrics without turning them into model instructions:

- initial findings;
- normalized findings/duplicates;
- fix attempts;
- challenges;
- verification failures;
- repair rounds;
- new material closure findings;
- review/repair input and output tokens;
- wall clock and economic regime usage where available.

A high rate of previously-missed blockers or repeated fix verification failures is evidence that the review process, contract, or context compiler needs improvement.

It is not a reason to add more prose rules to every prompt.

## 15. Effective-now vs implementation roadmap

### Effective immediately as process guidance

- immutable candidate identity;
- stable finding IDs in handoff/PR artifacts where practical;
- fix/challenge distinction;
- author does not self-verify;
- focused revalidation;
- no equivalent reopening without new evidence;
- contract completeness review for systemic protocol WPs.

### WP-M3C-5

Implement compact typed finding/resolution/verification records sufficient to preserve state across clean sessions as a companion work package independent of WP-M3C-2's session/compiler implementation.

### M4

Measure token/cost/defect yield and whether structured review reduces duplicate findings and repair rounds.

### M7

Add full multi-review campaign automation, dynamic lenses, active falsification, aggregation and closure orchestration.

## 16. Relationship to other documents

- **ADR-0020** owns cognitive invocation compilation and the durable review-state authority split.
- **ADR-0019** owns adaptive context layers and non-conversational cognition.
- **PROTOCOLS.md** owns wire/record semantics.
- **WORK_PACKAGES.md / IMPLEMENTATION_PLAN.md** own milestone placement.
- **AGENTS.md** contains only compact operating directives and points here for review behavior.

Avoid duplicating these rules into prompts or role docs. Runtime projections should select only what the current model action requires.
