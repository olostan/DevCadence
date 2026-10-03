# Engineering Work Package: WP-M3C-5 — Durable Review-Ledger Primitives

- **Milestone:** M3C — Cognition Resource and Session Substrate
- **Scope card:** docs/WORK_PACKAGES.md#wp-m3c-5--durable-review-ledger-primitives
- **Work Package ID:** `WP-M3C-5`
- **Task ID:** `task-m3c-5-durable-review-ledger-primitives`
- **EWP revision:** `r2` (r1 was merged in PR #28 and failed independent readiness review)
- **Base commit:** `1491543d00b718e215a32490fee61bbaf5b0fb18` (origin/main, merge of PR #28). If `main` has advanced at delegation, the Principal re-confirms that no file named in §9 or §2 changed, or re-reviews.
- **Project state revision:** n/a (manual self-development; no ProjectState record is produced for this WP)
- **Target implementation endpoint/profile:** unassigned. The delegation manifest records the chosen endpoint; the whole contract (§1–§13) MUST fit its effective context profile, otherwise split or route upward.
- **Contract digest:** `git hash-object docs/work-packages/wp-m3c-5-ewp.md`, recorded in the delegation manifest. Any later edit is a new revision and requires re-delegation.
- **Status:** `BLOCKED` — see §0. Not delegable until PRE-2 (independent re-review) is recorded.
- **Purpose in DevCadence self-development:** manual Principal-authored EWP; no DevCadence self-hosting/runtime enforcement is required.

## 0. Delegation prerequisites (BLOCKED until cleared)

| ID | Prerequisite | Owner |
| --- | --- | --- |
| PRE-1 | **CLEARED** — owner approved on 2026-10-03 the three design decisions recorded in §1A: D-1 (logical actor identity, revised from r1), D-2 (resolution state machine), D-3 (scope: closure wiring, verifier projection and normalization are deferred; the scope card is amended to match). | Principal |
| PRE-2 | Independent readiness re-review of this revision recorded (ADR-0024 gate). r1's self-assessed PASS was contradicted by independent review. | Reviewer |

## 1. Objective

Add the minimal durable, backward-compatible review-ledger primitives needed to carry a normalized finding, an implementer's resolution attempt and an independent verification across clean sessions, and to **mechanically reject logical self-verification**: three new strict Go/schema twin records (`ReviewFinding`, `FindingResolution`, `ResolutionVerification`), one embedded provenance fragment (`ActorProvenance`), and two pure functions over them (`CheckVerification`, `DeriveFindingResolutionState`). It does not implement M7 multi-review orchestration and does not touch `ReviewCampaign`, `FindingDisposition` or `ClosureDecision`.

## 1A. Closed design decisions (the implementer MUST NOT alter these)

**D-1 — Logical actor identity (revised from r1).** Current `main` has no logical producer/verifier identity: `ReviewResult` carries `attempt_id` plus free-text `reviewer_profile` and `model_identity`; `protocol.Actor` is a stable role/profile handle (DCI-081), not a per-assignment identity. Independence is therefore defined **only** over the `ActorProvenance` fragment in §4A, under the rule in §5.

**D-2 — Resolution state machine.**
- Resolution outcomes are four: `verified_fixed`, `verified_dismissed`, `not_resolved`, `re_adjudication_required`. `not_resolved` is added to the three in ADR-0020 §8 so that "not fixed / partially fixed" (fix) and "challenge not upheld" (challenge) have a representation that does not send a routine failed repair to Principal adjudication.
- `ReviewFinding` is immutable and has **no stored status**. Lifecycle state is derived by `DeriveFindingResolutionState` (§5) from the records; nothing in this WP mutates a record.
- Each `FindingResolution` carries a 1-based `attempt_no` per finding. A finding may have several resolutions (repair rounds) but each resolution has **at most one** `ResolutionVerification`; a re-verification requires a new attempt.

**D-3 — Scope.** This WP adds no Go types for `ReviewCampaign`, `FindingDisposition` or `ClosureDecision` (they remain "awaiting implementation (M7)" in `tests/schema_fixtures_test.go`), no closure logic, no verifier projection or identity blinding, no normalization/deduplication algorithm and no ID minting. "Closure-eligible" is defined as: the derived state is `verified_fixed` or `verified_dismissed`; wiring that to `ClosureDecision` counts and to superseding `FindingDisposition=defer` is M7. The WORK_PACKAGES.md card is amended accordingly.

## 2. Semantic scope envelope

### Authorized domains

- `internal/protocol/review_ledger.go` (new) and package-local tests `internal/protocol/review_ledger*_test.go`;
- `internal/protocol/protocol.go`: only the three `NewRecord` cases (§9);
- `internal/schema/schema.go`: only the three `Name…` constants, three `RecordKindToSchema` entries and three `AllNames` entries (§9);
- `schemas/review-finding.schema.json`, `schemas/finding-resolution.schema.json`, `schemas/resolution-verification.schema.json` (new), `schemas/README.md` listing;
- `fixtures/protocol/review-finding.*`, `finding-resolution.*`, `resolution-verification.*` (new);
- `tests/schema_fixtures_test.go` and `tests/twin_fields_test.go`: only the table entries named in §9;
- documentation synchronization listed in §9.

### Explicitly forbidden (require Principal amendment)

- modifying `schemas/review-campaign.schema.json`, `finding-disposition.schema.json`, `closure-decision.schema.json`, `review-result.schema.json`, their fixtures, `ReviewResult`/`Finding` Go types, or the `awaitingImplementation` entries;
- Go types for `ReviewCampaign`, `FindingDisposition`, `ClosureDecision`; closure-eligibility logic against `ClosureDecision`;
- giving any new record closure, disposition or deferral authority;
- changing any identity, event, reducer, store, control-plane or `internal/ids` code (D-3);
- new severity, materiality or confidence vocabularies;
- M7 orchestration (fan-out, lenses, aggregation, campaign automation);
- changing D-1/D-2/D-3.

### LOCAL_DISCRETION

- private helper names and the file split inside the authorized files;
- table-driven versus subtest organization in package-local tests;
- the precise error message text (categories in §5 and §8 are fixed).

## 3. Material requirements

| ID | Requirement |
| --- | --- |
| REQ-01 | Add `ReviewFinding` exactly as specified in §4B. |
| REQ-02 | Add `FindingResolution` exactly as specified in §4C. |
| REQ-03 | Add `ResolutionVerification` exactly as specified in §4D. |
| REQ-04 | Add `ActorProvenance` (§4A) and enforce the per-position role. |
| REQ-05 | Add `CheckVerification` implementing the independence rule, link equalities, candidate rules and kind×outcome matrix of §5, failing closed. |
| REQ-06 | Add `DeriveFindingResolutionState` implementing §5. A `fix_attempted` record never yields a verified state by itself. |
| REQ-07 | `severity` and `materiality` accept exactly the vocabularies already in `schemas/finding-disposition.schema.json`; `confidence` is `high | medium | low`; the three vocabularies are not interchangeable. |
| REQ-08 | Existing review artifacts remain byte-identical and valid (§2 forbidden list). |
| REQ-09 | New kinds are registered everywhere in §9 so persistence accepts them and rejects project mismatch (`ProjectScoped`). |
| REQ-10 | State derivation needs only the decoded durable records, never a transcript (clean-session reconstruction). |
| REQ-11 | Documentation is synchronized as in §9. |

## 4. Interface contract

All three records: strict (`additionalProperties:false`; unknown fields rejected by `protocol.Unmarshal`), `schema_version` const `"1.0"`, `project_id` required, implement `Record` and `ProjectScoped` (`ProjectOf()`), Go and schema twins in parity (`tests/twin_fields_test.go`). Absent, null and zero are equivalent for optional fields (ADR-0003 convention used by `finding-disposition.schema.json`): optional strings are `type: ["string","null"]` in schema and `*string` with `omitempty` in Go. "Non-empty" means `strings.TrimSpace(x) != ""` in Go and `minLength: 1` plus `pattern: "\\S"` in schema. Timestamps are RFC3339 strings (`format: date-time`; Go parses `time.RFC3339Nano` then `time.RFC3339`). ID formats are control-plane convention and are **not** validated beyond non-empty (like existing `disposition_id`/`finding_id`): proposed prefixes `rf`, `rsl`, `rvf`, `act`, `ivk` (not `inv`, which `internal/setup/doctor.go` already uses for ResourceInventory).

### 4A. `ActorProvenance` (embedded; not a record)

| JSON | Go | Type | Rule |
| --- | --- | --- | --- |
| `actor_id` | `ActorID` | string | required, non-empty. Opaque; minted by the control plane when it assigns a role; never derived from account, provider, model, endpoint or session strings. |
| `invocation_id` | `InvocationID` | string | required, non-empty. Opaque; one cognitive invocation. |
| `role` | `Role` | enum `reviewer \| implementer \| verifier` | required. Must equal the position: `ReviewFinding.reviewer` is `reviewer`, `FindingResolution.producer` is `implementer`, `ResolutionVerification.verifier` is `verifier`; a mismatch is invalid. |
| `lineage_actor_ids` | `LineageActorIDs` | []string | optional; items non-empty, unique; MUST NOT contain this record's own `actor_id`. The control plane populates the **transitive closure** of actors whose output this actor consumed as a basis for its work; the validators check direct membership only. |
| `endpoint_ref` / `session_ref` / `model_ref` | `*string` each | optional | informational by-reference provenance (PROTOCOLS §FindingResolution/§ResolutionVerification); MUST NOT participate in any independence decision. |

### 4B. `ReviewFinding` (REQ-01)

| JSON | Go | Type | Rule |
| --- | --- | --- | --- |
| `finding_id` | `FindingID` | string | required, non-empty; the stable identity referenced by `ReviewCampaign.finding_refs` and `FindingDisposition.finding_id` (those remain free strings; no referential check). |
| `project_id`, `campaign_id` | `ProjectID`, `CampaignID` | string | required, non-empty. |
| `candidate_commit` | `CandidateCommit` | string | required, non-empty; immutable target candidate. |
| `work_package_id` | `WorkPackageID` | string | required, non-empty. |
| `contract_revision` | `ContractRevision` | int | required, `>= 1` (same meaning as `ReviewCampaign.work_package_version`). |
| `severity` | `Severity` (`protocol.Severity`) | enum `info\|low\|medium\|high\|critical` | required; Go uses `Severity.ValidFinding()`. |
| `materiality` | `Materiality` (new type) | enum `blocking\|material_non_blocking\|opportunistic` | required. |
| `confidence` | `Confidence` | optional enum `high\|medium\|low` | absent means unknown. |
| `claim`, `impact`, `verification_method` | strings | | required, non-empty. |
| `why_now` | `*string` | optional | |
| `evidence_refs` | []string | | required, at least 1 item, each non-empty. |
| `requirement_refs` | []string | | optional; items non-empty when present. |
| `source_observations` | []`ObservationRef{ReviewID string, FindingIndex int}` | | required, at least 1; `review_id` non-empty, `finding_index >= 0`. Points to `ReviewResult.review_id` and an index in that record's `findings`. No existence check; a superseded `ReviewResult` stays a valid historical link. |
| `reviewer` | `Reviewer` (`ActorProvenance`) | | required; role `reviewer`. |
| `recorded_at` | `RecordedAt` | string | required, RFC3339. |

### 4C. `FindingResolution` (REQ-02)

| JSON | Go | Type | Rule |
| --- | --- | --- | --- |
| `resolution_id`, `project_id`, `campaign_id`, `finding_id` | strings | | required, non-empty. |
| `disposition_id` | `DispositionID` | string | required, non-empty; the `fix_now` `FindingDisposition` this answers. Not resolved or checked against any record in this WP. |
| `contract_revision` | int | | required, `>= 1`. |
| `attempt_no` | `AttemptNo` | int | required, `>= 1`. |
| `kind` | `Kind` (`ResolutionKind`) | enum `fix_attempted \| challenge` | required. |
| `target_candidate_commit` | string | | required, non-empty; the finding's `candidate_commit`. |
| `resolved_candidate_commit` | `*string` | | required non-empty when `kind == fix_attempted`; MUST be absent or null when `kind == challenge`. Schema expresses this with `allOf` `if/then/else`. |
| `rationale` | string | | required, non-empty (fix: what changed; challenge: the contradiction argument). |
| `evidence_refs` | []string | | required, at least 1, each non-empty. |
| `producer` | `ActorProvenance` | | required; role `implementer`. |
| `recorded_at` | string | | required, RFC3339. |

### 4D. `ResolutionVerification` (REQ-03)

| JSON | Go | Type | Rule |
| --- | --- | --- | --- |
| `verification_id`, `project_id`, `campaign_id`, `finding_id`, `resolution_id` | strings | | required, non-empty. |
| `contract_revision` | int | | required, `>= 1`. |
| `verified_candidate_commit` | string | | required, non-empty; the candidate the verifier examined. |
| `outcome` | `Outcome` (`VerificationOutcome`) | enum `verified_fixed \| verified_dismissed \| not_resolved \| re_adjudication_required` | required. |
| `rationale` | string | | required, non-empty. |
| `evidence_refs` | []string | | required, at least 1, each non-empty. |
| `verifier` | `ActorProvenance` | | required; role `verifier`. |
| `recorded_at` | string | | required, RFC3339. |

Nothing in any record expresses deferral or risk acceptance: `re_adjudication_required` is evidence returned to the authorized Principal/Human, never an authorization.

## 5. Algorithms (exact semantics)

All failures below return `errs.CategoryValidationFailed` unless stated.

**independent(a, b ActorProvenance) bool** = `a.ActorID != b.ActorID` ∧ `a.ActorID ∉ b.LineageActorIDs` ∧ `b.ActorID ∉ a.LineageActorIDs` ∧ `a.InvocationID != b.InvocationID`. Both `ActorID` and `InvocationID` on both sides MUST be non-empty, otherwise the pair is **not** independent (fail closed). Lineage is compared directly (no transitive computation).

**`CheckVerification(f *ReviewFinding, r *FindingResolution, v *ResolutionVerification) error`** checks, in order, and returns on the first failure:

1. `f`, `r`, `v` non-nil and each passes its own `Validate()`; a `Validate()` failure is returned wrapped (`errs.Wrap(errs.CategoryValidationFailed, err, …)`) so that every `CheckVerification` failure has category `CategoryValidationFailed`.
2. Links: `r.ProjectID == f.ProjectID == v.ProjectID`; `r.CampaignID == f.CampaignID == v.CampaignID`; `r.FindingID == f.FindingID == v.FindingID`; `v.ResolutionID == r.ResolutionID`; `r.ContractRevision == f.ContractRevision == v.ContractRevision`; `r.TargetCandidateCommit == f.CandidateCommit`.
3. Candidate rule: if `r.Kind == fix_attempted` then `v.VerifiedCandidateCommit == *r.ResolvedCandidateCommit`; if `r.Kind == challenge` then `v.VerifiedCandidateCommit == f.CandidateCommit`.
4. Kind×outcome matrix: `fix_attempted` allows `verified_fixed`, `not_resolved`, `re_adjudication_required`; `challenge` allows `verified_dismissed`, `not_resolved`, `re_adjudication_required`. Any other combination fails.
5. Independence: `independent(v.Verifier, r.Producer)` MUST hold. If `r.Kind == challenge`, `independent(v.Verifier, f.Reviewer)` MUST also hold (a reviewer cannot adjudicate a challenge to their own claim). For `fix_attempted`, the original reviewer MAY verify.

**`DeriveFindingResolutionState(f *ReviewFinding, rs []FindingResolution, vs []ResolutionVerification) (FindingResolutionState, error)`**; `FindingResolutionState` values: `unresolved`, `verification_pending`, `verified_fixed`, `verified_dismissed`, `re_adjudication_required`.

1. `f` non-nil and valid; every element of `rs`/`vs` valid; every `r` links to `f` (project, campaign, finding_id, contract_revision) else error.
2. If `len(rs) == 0`: return `unresolved` (and `len(vs)` MUST be 0, else error).
3. `attempt_no` values MUST be exactly `1..len(rs)` with no gaps or duplicates, else error. Order of input slices is irrelevant.
4. Each `v` MUST reference exactly one existing `r.ResolutionID`; at most one `v` per `r`, else error. Each `(r, v)` pair MUST pass `CheckVerification`, else return that error (fail closed; an invalid verification never degrades to "ignored").
5. Every resolution except the one with the highest `attempt_no` MUST have a verification with outcome `not_resolved`, else error (an attempt superseded without a failed verification).
6. Let `R` be the highest-`attempt_no` resolution. No verification for `R`: return `verification_pending`. Otherwise map `v.Outcome`: `verified_fixed` → `verified_fixed`; `verified_dismissed` → `verified_dismissed`; `re_adjudication_required` → `re_adjudication_required`; `not_resolved` → `unresolved`.

Closure-eligible (definition only): the derived state is `verified_fixed` or `verified_dismissed`. This WP does not consume it.

## 6. Invariants / state rules

| ID | Invariant | Requirements |
| --- | --- | --- |
| INV-01 | Finding observation, disposition, resolution, verification and closure remain distinct authority layers; this WP writes none of the disposition or closure layers. | REQ-01..03 |
| INV-02 | A verification is independently valid only under §5 step 5; absent or empty identifiers fail closed. | REQ-04, REQ-05 |
| INV-03 | Account, provider, model, endpoint and session strings never decide independence. | REQ-04, REQ-05 |
| INV-04 | A `fix_attempted` resolution never yields a verified state without a verification. | REQ-06 |
| INV-05 | No outcome or field expresses deferral or accepted risk. | REQ-03 |
| INV-06 | Severity is harm, materiality is current-candidate effect, confidence is evidence strength; their vocabularies are disjoint and not interchangeable. | REQ-07 |
| INV-07 | Existing review schemas, fixtures and Go types are byte-identical after this WP. | REQ-08 |
| INV-08 | State is derived from durable records alone. | REQ-10 |

## 7. Authority matrix

| Decision / effect | Authorized source | Forbidden substitute |
| --- | --- | --- |
| mint `actor_id`, `invocation_id`, `lineage_actor_ids` | control plane | model output, implementer, verifier, account/provider strings |
| normalize and deduplicate raw findings, mint `finding_id` | control plane / Principal process (out of scope here) | the implementer |
| disposition (`fix_now`, `defer`, …) | existing Principal/Human `FindingDisposition` | any new record |
| attempt a fix or challenge | implementer-role actor | the verifier |
| decide whether a resolution is verified | independent verifier-role actor | the producer, or a verifier failing §5 |
| accept deferral / risk | Principal/Human via a superseding `FindingDisposition=defer` (M7) | a verifier outcome |
| closure | existing `ClosureDecision` process (M7) | `DeriveFindingResolutionState` |

## 8. Missing / unknown / stale / malformed input semantics

| Input | Missing | Unknown | Stale | Malformed / contradictory |
| --- | --- | --- | --- | --- |
| `producer` / `verifier` / `reviewer` provenance | record invalid (`errs.CategoryInvalidArgument`) | n/a | n/a | empty or whitespace id, bad role, self in lineage, duplicate lineage entry: invalid |
| independence comparison | not independent: fail closed | n/a | n/a | n/a |
| `confidence` | allowed (unknown) | allowed | n/a | out-of-vocabulary: invalid |
| `severity` / `materiality` | invalid (both required) | n/a | n/a | out-of-vocabulary or cross-vocabulary value: invalid |
| candidate or contract identity | invalid | n/a | mismatch between records: `CheckVerification` failure | n/a |
| `resolved_candidate_commit` | required for `fix_attempted`, forbidden for `challenge` | n/a | n/a | violation: invalid |
| `evidence_refs`, `source_observations` | invalid (minimum 1) | n/a | n/a | empty item: invalid |
| verification for a resolution | `verification_pending` | n/a | n/a | two verifications for one resolution: error |
| disposition for closure | not evaluated by this WP | n/a | n/a | n/a |
| legacy review records | unchanged, no migration, no rewrite | n/a | n/a | n/a |

## 9. Representability map and registration touchpoints

| Concept | Exact representation |
| --- | --- |
| normalized finding / resolution / verification | `protocol.ReviewFinding`, `protocol.FindingResolution`, `protocol.ResolutionVerification` in `internal/protocol/review_ledger.go`, schemas `review-finding`, `finding-resolution`, `resolution-verification` |
| provenance and independence | `protocol.ActorProvenance`, `ProvenanceRole`, §5 `independent` |
| severity vocabulary | existing `protocol.Severity` and `ValidFinding()` |
| materiality / confidence / kind / outcome | new string types `Materiality`, `FindingConfidence`, `ResolutionKind`, `VerificationOutcome` with `Valid()` |
| raw observation link | `ObservationRef{ReviewID, FindingIndex}` |
| derived state | `FindingResolutionState` and `DeriveFindingResolutionState` |
| existing disposition / campaign / closure | schemas only; no Go types (D-3) |

Registration checklist (every item MUST be done; nothing else is wired):
1. `internal/protocol/review_ledger.go`: types, `Validate`, `RecordKind`, `RecordID`, `SchemaVer`, `ProjectOf` for each record.
2. `internal/protocol/protocol.go` `NewRecord`: cases `"ReviewFinding"`, `"FindingResolution"`, `"ResolutionVerification"`.
3. `internal/schema/schema.go`: constants `NameReviewFinding`, `NameFindingResolution`, `NameResolutionVerification`; `RecordKindToSchema` entries; `AllNames()` entries; update the doc comment above the review-convergence constants.
4. Three schema files in `schemas/`; list them under "Review convergence" in `schemas/README.md`.
5. Fixtures in `fixtures/protocol/`: for each record one `*.valid.json` and the invalid fixtures in ACC-02.
6. `tests/schema_fixtures_test.go`: add the three `*.valid.json` rows to the round-trip table (next to `refactoring-proposal.valid.json`), the three `*.invalid-` prefixes to `TestTheGoReaderRejectsWhatTheSchemaRejects`, and three cases to `recordKindFor`.
7. `tests/twin_fields_test.go`: three rows.
8. No change to `awaitingImplementation`, `internal/storage`, `internal/controlplane`, events or reducers; `internal/storage/schema_boundary_test.go` must pass because the kinds are registered.

Documentation synchronization: `docs/PROTOCOLS.md` (the ReviewFinding/FindingResolution/ResolutionVerification sections: state the D-1 representation and the four outcomes, remove `status` from the ReviewFinding field list because state is derived, and remove "do not yet have committed Go/schema twins"), `docs/REVIEW_AND_CONVERGENCE.md` (§3 no stored status, §6 provenance representation, §7 outcomes), `docs/adr/0020-…` §8 (add `not_resolved` and a pointer to D-1/D-2), `schemas/README.md`, `docs/IMPLEMENTATION_PLAN.md` and `docs/WORK_PACKAGES.md` status.

## 10. Acceptance scenarios

Test and subtest names MUST contain `ReviewLedger` and their `ACC-xx` id.

| ID | Setup | Action | Expected | Maps to |
| --- | --- | --- | --- | --- |
| ACC-01 | A `*.valid.json` fixture per record | schema-validate; `protocol.Unmarshal`; `protocol.Marshal`; re-validate; twin-fields test | schema accepts; Go round-trips byte-equivalent after canonical marshal; published and declared field sets equal | REQ-01..04, REQ-09 |
| ACC-02 | Invalid fixtures, each rejected by BOTH the schema and the Go reader: `review-finding.invalid-` {`no-project-id`, `severity-blocking` (materiality value in severity), `materiality-high`, `confidence-critical`, `empty-evidence`, `empty-observations`, `reviewer-role-verifier`, `whitespace-actor-id`}; `finding-resolution.invalid-` {`fix-without-resolved-commit`, `challenge-with-resolved-commit`, `missing-invocation-id`, `producer-role-verifier`, `lineage-contains-self`, `duplicate-lineage`, `attempt-zero`}; `resolution-verification.invalid-` {`unknown-outcome`, `outcome-deferred`, `missing-verifier`, `empty-rationale`, plus one with an extra unknown field}; one wrong `schema_version` per record | schema validation and `protocol.Unmarshal` | every invalid fixture is rejected by both | REQ-01..04, REQ-07, INV-06 |
| ACC-03 | Programmatic comparison of schema enum sets | compare `severity` and `materiality` enums in `review-finding.schema.json` with `finding-disposition.schema.json` | identical sets; `confidence` enum is exactly `high, medium, low` | REQ-07 |
| ACC-04 | Baseline valid chain `f`, `r` (`fix_attempted`), `v` (`verified_fixed`) with independent provenance | `CheckVerification(f, r, v)` | passes | REQ-05 |
| ACC-05 | Baseline chain, then one mutation per row | `CheckVerification` | each row returns `CategoryValidationFailed` except the rows marked PASS | REQ-05, INV-02, INV-03 |

ACC-05 mutation table (each is a separate table-driven case):

| # | Mutation | Expected |
| --- | --- | --- |
| a | `v.verifier.actor_id == r.producer.actor_id` | fail |
| b | `v.verifier.invocation_id == r.producer.invocation_id` | fail |
| c | `r.producer.actor_id ∈ v.verifier.lineage_actor_ids` | fail |
| d | `v.verifier.actor_id ∈ r.producer.lineage_actor_ids` | fail |
| e | empty `v.verifier.actor_id` (record invalid) | fail |
| f | empty `r.producer.invocation_id` | fail |
| g | verifier equals the original reviewer, `kind == fix_attempted` | PASS |
| h | verifier `actor_id` equals `f.reviewer.actor_id`, `kind == challenge`, outcome `verified_dismissed` | fail |
| i | `f.reviewer.actor_id ∈ v.verifier.lineage_actor_ids`, `kind == challenge` | fail |
| j | same `endpoint_ref`, `model_ref`, `session_ref` on both sides, fresh distinct ids | PASS (informational fields are ignored) |
| k | `v.project_id` differs | fail |
| l | `v.campaign_id` differs | fail |
| m | `v.finding_id` differs | fail |
| n | `v.resolution_id` differs | fail |
| o | `v.contract_revision` differs | fail |
| p | `fix_attempted` and `v.verified_candidate_commit != r.resolved_candidate_commit` | fail |
| q | `challenge` and `v.verified_candidate_commit != f.candidate_commit` | fail |
| r | `fix_attempted` with outcome `verified_dismissed` | fail |
| s | `challenge` with outcome `verified_fixed` | fail |
| t | `r.target_candidate_commit != f.candidate_commit` | fail |
| u | verifier role `implementer` on `v.verifier` | fail |

| ID | Setup | Action | Expected | Maps to |
| --- | --- | --- | --- | --- |
| ACC-06 | Chains built from valid records | `DeriveFindingResolutionState`, one case per row of the table below | exact states/errors as listed | REQ-06, INV-04, INV-05 |
| ACC-07 | A valid chain in memory | marshal each record to JSON bytes, decode with `protocol.Unmarshal` into fresh values, derive state from only the decoded values (no shared pointers, no transcript input) | state equals the state derived in memory | REQ-10, INV-08 |
| ACC-08 | git base..head | `git diff --name-only <base>..HEAD` | no path matches the forbidden list in §2; `awaitingImplementation` unchanged; all existing review fixtures and schemas byte-identical; existing tests pass | REQ-08, INV-07 |
| ACC-09 | Registered kinds | `NewRecord` for each kind; `TestEveryRecordKindHasASchema`; `internal/storage` schema-boundary tests; persist a record with a mismatching project | `NewRecord` returns the type; both tests pass; persistence refuses a project mismatch via `ProjectScoped` | REQ-09 |
| ACC-10 | Docs edited per §9 | run documentation tests | `go test -count=1 ./tests -run 'Doc'` passes; PROTOCOLS no longer says the three records lack twins | REQ-11 |

ACC-06 derivation table:

| # | Input | Expected |
| --- | --- | --- |
| a | no resolutions, no verifications | `unresolved` |
| b | one `fix_attempted`, no verification | `verification_pending` |
| c | `fix_attempted` + `verified_fixed` | `verified_fixed` |
| d | `fix_attempted` + `not_resolved` | `unresolved` |
| e | attempt 1 `not_resolved`, attempt 2 `fix_attempted` + `verified_fixed` | `verified_fixed` |
| f | attempt 1 with no verification and attempt 2 present | error |
| g | attempt numbers 1 and 3 | error |
| h | duplicate `attempt_no` | error |
| i | two verifications for one resolution | error |
| j | `challenge` + `verified_dismissed` | `verified_dismissed` |
| k | `challenge` + `not_resolved` | `unresolved` |
| l | `fix_attempted` + `re_adjudication_required` | `re_adjudication_required` (not closure-eligible) |
| m | a verification that fails `CheckVerification` | error (never ignored) |
| n | verification referencing an unknown resolution | error |

## 11. Validation

Run from the repository root; record exit status, counts, base and head SHAs and the Go version:

- `make hooks-install` once, then `make hooks-check` (AGENTS.md §12);
- `go build ./...`, `go vet ./...`;
- `go test -count=1 ./...` and `go test -race -count=1 ./...`;
- `go test -count=1 -v -run 'ReviewLedger' ./internal/protocol ./tests`;
- `make verify`;
- `bash scripts/health/precommit.sh` on the staged tree.

Required independent review lens: mutation-style review that every ACC-05 and ACC-06 row can fail when the corresponding rule is removed, plus a schema-versus-Go parity review.

## 12. Escalation triggers

Return to the Principal, with the exact record, row id and observed behavior, when:

- any identifier named in §9 is missing or behaves differently at delegation (for example `NewRecord`, `RecordKindToSchema`, `Severity.ValidFinding`, `ProjectScoped`);
- implementing a record would require modifying a path in the §2 forbidden list, or Go types for `ReviewCampaign`/`FindingDisposition`/`ClosureDecision`;
- a rule in §5 is internally inconsistent with the fixtures or with an existing test;
- schema and Go cannot express the same constraint (for example the `kind`/`resolved_candidate_commit` conditional);
- a requirement appears to need M7 behavior (closure wiring, projection/blinding, normalization).

A failing scenario is reported BLOCKED with evidence; it is never skipped, deleted or weakened.

## 13. Implementation Readiness Report

```text
requirements represented: 11/11
mandatory clauses resolved: DCI-134, DCI-135, DCI-081, DCI-092 cited; ADR-0020 §7/§8, PROTOCOLS review records, REVIEW_AND_CONVERGENCE §3/§6/§7
state transitions specified: derivation steps 1-6 and the kind x outcome matrix (§5)
failure cases specified: record-level (§8) + 21 independence mutations + 14 derivation rows
authority decisions specified: 7/7
missing/unknown input semantics: 10/10 rows (§8)
acceptance scenarios mapped: 10/10
closed design decisions: 3 (D-1, D-2, D-3), approved by the owner (PRE-1 cleared)
declared local-discretion choices: 3
known limitations stated: lineage is control-plane supplied and not authenticated; lineage compared directly (transitive closure supplied by the control plane); invocation_id uniqueness is checked only between a verification and its resolution; provenance authenticity is not verified here
readiness: NOT_READY (BLOCKED on PRE-2 independent re-review)
```

Self-assessment only. This revision has not yet had an independent readiness review (PRE-2); the weaker-implementer check is therefore **not** claimed.
