# Engineering Work Package: WP-M3C-5 — Durable Review-Ledger Primitives

- **Milestone:** M3C — Cognition Resource and Session Substrate
- **Scope card:** docs/WORK_PACKAGES.md#wp-m3c-5--durable-review-ledger-primitives
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
| REQ-04 | Structurally reject logical self-verification, including across clean sessions under the same visible GitHub/user account. |
| REQ-05 | Preserve existing FindingDisposition severity/materiality vocabulary and closure authority. |
| REQ-06 | A blocking fix attempt remains unresolved until independent verification succeeds. |
| REQ-07 | Verification may request re-adjudication but cannot authorize deferral/accepted risk. |
| REQ-08 | Challenge verification is identity-blinded by default; identity is exposed only when materially relevant evidence requires it. |
| REQ-09 | Existing accepted ReviewCampaign / FindingDisposition / ClosureDecision records remain backward-compatible. |
| REQ-10 | Clean-session reconstruction requires durable state/evidence, not prior chat transcript. |

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
| producer/verifier independence | durable logical producer + invocation provenance fields |

If the existing protocol lacks a stable logical identity representation sufficient to reject self-verification, stop and amend the protocol explicitly rather than inferring independence from GitHub account/model/provider strings.

## 10. Acceptance scenarios

| ID | Scenario | Expected result |
| --- | --- | --- |
| ACC-01 | ReviewFinding round-trip through Go/schema | exact strict parity |
| ACC-02 | FindingResolution produced and later verified in clean session | reconstruction succeeds from durable state |
| ACC-03 | same logical producer attempts ResolutionVerification | deterministic rejection |
| ACC-04 | different verifier verifies fix | verified-fixed state can contribute to existing closure flow |
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

- independent logical identity cannot be represented by existing protocol without a broader identity design;
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
unresolved architecture choices: 0
local-discretion classes: 5
readiness: READY_FOR_IMPLEMENTATION
```

Weaker-implementer check: **PASS**, subject to the explicit escalation trigger if the current producer/verifier identity representation proves insufficient.
