# Engineering Work Package: WP-M3C-5 — Durable Review-Ledger Primitives

- **Milestone:** M3C — Cognition Resource and Session Substrate
- **Scope card:** docs/WORK_PACKAGES.md#wp-m3c-5--durable-review-ledger-primitives
- **Work Package ID:** `WP-M3C-5`
- **Task ID:** `task-m3c-5-durable-review-ledger-primitives`
- **EWP revision:** `r1`
- **Base commit:** `4e4a24b63b544f0bc3e7d0ff87ec359a0f59c2da` (origin/main, merge of PR #29 — post-M3C-3 hardening)
- **Contract digest:** computed over this file's bytes at delegation (`git hash-object docs/work-packages/wp-m3c-5-ewp.md`) and recorded in the delegation manifest; any later edit is a new revision and requires re-delegation.
- **Status:** READY_FOR_IMPLEMENTATION
- **Purpose in DevCadence self-development:** manual Principal-authored EWP; no DevCadence self-hosting/runtime enforcement is required.

## 1. Objective

Implement the minimal backward-compatible durable state needed to carry normalized review findings, repair/challenge attempts and independent verification across clean sessions, while preserving the existing ReviewCampaign / FindingDisposition / ClosureDecision authority model.

This WP does not implement M7 multi-review orchestration.

## 2. Semantic scope envelope

### Authorized domains

- review-ledger protocol Go types and strict JSON schemas;
- validation/parity logic and fixtures;
- persistence/reducer wiring only as required to store/reconstruct the new records through existing canonical mechanisms;
- focused tests for producer/verifier provenance and closure compatibility;
- documentation synchronization for the new records.

### Explicitly forbidden semantic expansion

Requires Principal amendment:

- replacing ReviewCampaign, FindingDisposition or ClosureDecision;
- giving FindingResolution or ResolutionVerification independent closure authority;
- allowing an implementer to verify its own resolution;
- introducing M7 fan-out/lens/aggregation orchestration;
- adding new severity/materiality vocabularies incompatible with existing durable records;
- allowing a verifier to authorize risk deferral.

### LOCAL_DISCRETION

- exact source/test file split;
- helper constructors;
- validation helper organization;
- package-local normalization utilities;
- fixture organization.

## 3. Material requirements

| ID | Requirement |
| --- | --- |
| REQ-01 | Add durable `ReviewFinding` with stable identity, immutable candidate/contract identity, severity, materiality, optional confidence, claim/evidence/requirement refs, verification method and reviewer provenance. |
| REQ-02 | Add durable `FindingResolution` with `fix_attempted | challenge`, candidate/evidence refs and mandatory producer/invocation provenance. |
| REQ-03 | Add durable `ResolutionVerification` with verifier/invocation provenance and outcomes sufficient for fixed/dismissed/re-adjudication-required semantics. |
| REQ-04 | Structurally reject logical self-verification, including across clean sessions under the same visible GitHub/user account, using exactly the identity model closed in §4A (decision ID-1). |
| REQ-05 | Preserve existing FindingDisposition severity/materiality vocabulary and closure authority. |
| REQ-06 | A blocking fix attempt remains unresolved until independent verification succeeds. |
| REQ-07 | Verification may request re-adjudication but cannot authorize deferral/accepted risk. |
| REQ-08 | Challenge verification is identity-blinded by default; identity is exposed only when materially relevant evidence requires it. |
| REQ-09 | Existing accepted ReviewCampaign / FindingDisposition / ClosureDecision records remain backward-compatible. |
| REQ-10 | Clean-session reconstruction requires durable state/evidence, not prior chat transcript. |

## 4A. Closed design decision ID-1 — logical actor identity

Current `main` has no logical producer/verifier identity: `ReviewResult` carries only `attempt_id` plus the free-text `reviewer_profile` and `model_identity` strings, which are not an independence basis. The Principal closes the design here; the implementer MUST NOT alter it and MUST NOT substitute account, provider or model strings.

**`ActorProvenance`** (new record fragment, embedded as `producer` in `FindingResolution` and `verifier` in `ResolutionVerification`; strict Go/schema twin):

| Field | Type | Rule |
| --- | --- | --- |
| `actor_id` | string, required | Opaque logical-actor identifier minted by the control plane (`internal/ids`, prefix `actor`) when it assigns a role to work. One `actor_id` is minted per role assignment, never derived from account, provider, model, endpoint or session strings. |
| `invocation_id` | string, required | Control-plane-minted identifier (`internal/ids`, prefix `inv`) of the single cognitive invocation that produced the record. Unique per invocation. |
| `role` | enum, required | `implementer` for `FindingResolution.producer`, `verifier` for `ResolutionVerification.verifier`. |
| `lineage_actor_ids` | []string, optional | `actor_id`s of every earlier actor whose output this actor consumed as a basis for the attempt (for example the implementer whose fix is being repaired). Empty means none. |
| `endpoint_ref` | string, optional | Informational only. MUST NOT participate in any independence decision. |

**Independence rule (the only definition of "independent"):** a `ResolutionVerification` is independently valid if and only if, with `R` the `FindingResolution` it verifies:

1. `verifier.actor_id != R.producer.actor_id`;
2. `verifier.actor_id` is not in `R.producer.lineage_actor_ids`;
3. `R.producer.actor_id` is not in `verifier.lineage_actor_ids`;
4. `verifier.invocation_id != R.producer.invocation_id`;
5. all four identifier fields are present and non-empty on both sides.

Any violation, including an absent field, is a deterministic rejection with `errs.CategoryValidationFailed`. A verification that cannot be compared is invalid, never "assumed independent".

**Authority:** only the control plane mints `actor_id` and `invocation_id`. Values in model output are never trusted; the validator checks only structure and the rule above. Verifying that an id was really minted by the control plane is out of scope for this WP (no minting registry is added) and remains a stated limitation: this WP enforces the independence rule over recorded provenance, not provenance authenticity.

**Same-model clean session:** a new session of the same model/provider, scheduled by the control plane with a fresh `actor_id` and no lineage link, is independent by this rule. Reuse of the producer's `actor_id` for verification is rejected. This is the intended behavior of ADR-0020 §8 (rejecting logical self-verification across clean sessions).

## 4. Invariants / state rules

| ID | Invariant |
| --- | --- |
| INV-01 | Finding observation, disposition, resolution, verification and closure remain distinct authority layers. |
| INV-02 | Producer and verifier logical identities must differ for independent verification. |
| INV-03 | Same visible account/provider session metadata cannot substitute for logical producer/verifier provenance. |
| INV-04 | A `fix_attempted` record never means “fixed” by itself. |
| INV-05 | A verifier cannot create or imply authorized deferral/accepted risk. |
| INV-06 | Severity = harm, materiality = effect on current candidate, confidence = evidence strength; they are not interchangeable. |
| INV-07 | Existing durable records retain their prior meaning and remain readable/valid. |
| INV-08 | Review state required for continuation survives clean-session handoff without transcript dependence. |

## 5. State transition contract

Allowed conceptual progression:

```text
raw ReviewResult observation
  -> normalized ReviewFinding
  -> existing FindingDisposition
  -> FindingResolution(fix_attempted | challenge)
  -> ResolutionVerification(verified_fixed | verified_dismissed | re_adjudication_required)
  -> existing ClosureDecision when owning closure requirements are satisfied
```

Forbidden shortcuts:

- FindingResolution -> ClosureDecision without required verification;
- ResolutionVerification -> authorized defer/accepted-risk disposition;
- producer verifies its own FindingResolution;
- new record silently mutates historical FindingDisposition.

## 6. Authority matrix

| Decision | Authority |
| --- | --- |
| raw finding observation | reviewer |
| finding normalization/dedup identity | normalization/control-plane process |
| disposition (fix/defer/reject/etc.) | existing authorized adjudication/Principal/Human process |
| repair/challenge attempt | implementer/author |
| whether repair/challenge is independently verified | independent verifier |
| whether risk may be deferred/accepted | existing authorized disposition owner, not verifier |
| final closure | existing ClosureDecision process |

## 7. Missing / unknown input semantics

- missing producer provenance on FindingResolution: invalid;
- missing verifier provenance on ResolutionVerification: invalid;
- producer/verifier identity cannot be compared: verification is not independently valid; fail closed;
- missing candidate/contract identity needed to anchor a finding: invalid;
- unknown confidence: confidence may be omitted if protocol permits; severity/materiality may not silently become unknown if owning records require them;
- challenge lacks evidence/rationale: invalid or non-verifiable per exact schema contract;
- historical disposition absent when required for closure: no closure.

## 8. Failure matrix

| Failure / condition | Required behavior |
| --- | --- |
| same logical producer attempts verification | deterministic rejection |
| clean-session verifier has no chat history | verification reconstructs from durable records/evidence |
| verifier believes risk should be deferred | emit re-adjudication-required; no closure authority |
| challenge verification succeeds | verified dismissal/re-adjudication path without rewriting historical evidence |
| schema/Go representations diverge | parity test fails |
| legacy record loaded | retains prior meaning; no mandatory migration rewrite |
| duplicate raw findings normalize together | stable links preserve source observations; no evidence loss |
| candidate/contract identity mismatch | reject as stale/wrong-target verification |

## 9. Representability map

| Concept | Representation |
| --- | --- |
| normalized finding | new `ReviewFinding` Go/schema twin |
| repair/challenge attempt | new `FindingResolution` Go/schema twin |
| independent verification | new `ResolutionVerification` Go/schema twin |
| disposition authority | existing `FindingDisposition` |
| campaign identity | existing `ReviewCampaign` |
| final closure authority | existing `ClosureDecision` |
| producer/verifier independence | `ActorProvenance` per §4A (`actor_id`, `invocation_id`, `role`, `lineage_actor_ids`) and the five-clause independence rule |

The identity representation is closed by §4A; no further identity design is delegated to the implementer.

## 10. Acceptance scenarios

| ID | Scenario | Expected result |
| --- | --- | --- |
| ACC-01 | ReviewFinding round-trip through Go/schema | exact strict parity |
| ACC-02 | FindingResolution produced and later verified in clean session | reconstruction succeeds from durable state |
| ACC-03 | verifier reuses the producer's `actor_id`, or its `invocation_id`, or appears in lineage in either direction, or any of the four fields is empty | deterministic rejection for each variant (five table cases) |
| ACC-04 | verifier has a fresh `actor_id`/`invocation_id`, no lineage overlap, same model string and same visible account as the producer | accepted as independent; verified-fixed state can contribute to existing closure flow |
| ACC-05 | verifier recommends defer | re-adjudication required; closure still blocked until authorized disposition changes |
| ACC-06 | challenge verified | dismissal/re-adjudication outcome recorded without hidden mutation |
| ACC-07 | legacy campaign/disposition/closure fixtures | remain valid/backward-compatible |
| ACC-08 | severity/materiality/confidence round-trip | vocabularies preserve their distinct semantics |
| ACC-09 | candidate/contract mismatch | verification rejected |
| ACC-10 | restart/clean process/session | no transcript required to continue review state |

## 11. Validation

Minimum evidence:

- strict schema fixture validation;
- Go/schema parity tests;
- `go test -count=1 ./...`
- `go test -race ./...`
- `go vet ./...`
- targeted self-verification rejection and backward-compatibility tests.

## 12. Escalation triggers

Stop implementation when:

- `internal/ids` cannot mint the `actor`/`inv` identifiers without changing its public API;
- accepted FindingDisposition/ClosureDecision meaning would need to change;
- a verifier would need direct closure/deferral authority;
- M7 orchestration is required to complete the primitive;
- backward compatibility cannot be preserved without a versioned migration decision.

## 13. Implementation Readiness Report

```text
requirements represented: 10/10
invariants specified: 8/8
state transition shortcuts forbidden: 4
failure classes specified: 8/8
authority decisions specified: 7/7
missing/unknown input classes specified: 7/7
acceptance scenarios mapped: 10/10
unresolved architecture choices: 0 (identity model closed as ID-1)
local-discretion classes: 5
readiness: READY_FOR_IMPLEMENTATION
```

Weaker-implementer check: **PASS** after closure of decision ID-1 (§4A). Self-assessment only; independent readiness review is required before delegation (WORK_PACKAGES.md readiness gate).
