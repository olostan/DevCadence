# WP-M3D-1A — PortfolioRecommendation explanation and provenance fields

## Identity

- Work Package ID: WP-M3D-1A (first slice of WP-M3D-1 "AI-assisted Portfolio Planner"; WP-M3D-1B is the planner service and depends on this)
- Revision: 2 (independent readiness review: NOT_READY→fixes 1-15 incorporated)
- Task ID: autonomous-run-1 (issue #36)
- Base commit: `1cf7a9103bbef45f6435f1ca1eeff42179ef4830` (main after PR #38)
- Contract digest: n/a (Markdown contract authoritative)
- Target implementation endpoint/profile: competent Go implementer; mechanical protocol work
- Status: READY_FOR_IMPLEMENTATION (r2; reviewer blockers closed and verified against code by the Principal)

## Objective

Make the existing `PortfolioRecommendation` record able to carry what ADR-0018 §5/§6 and COGNITION_PORTFOLIO §9 require of a planner output that M3D-1B will produce: an **intent label** for each materially different alternative, **structured tradeoffs**, a **confidence** level, a **set grouping id**, and **planner provenance**. All additions are optional on the wire (existing records stay valid) and **informational only**: no validator, activation or routing decision may read them.

## Context Manifest

- role: implementer (Go, JSON Schema), then independent reviewers
- read-authority envelope: `internal/protocol/portfolio.go`, `internal/protocol/*` tests, `schemas/portfolio-recommendation.schema.json`, `fixtures/protocol/portfolio-recommendation.*`, `tests/schema_fixtures_test.go` (add the new valid fixture to the explicit round-trip `cases` table, ~line 154-193; invalid fixtures are discovered by the `.invalid-` substring), `docs/PROTOCOLS.md`, `docs/COGNITION_PORTFOLIO.md`, `docs/WORK_PACKAGES.md`
- semantic write/scope envelope: below
- risk tags: protocol/schema change (additive), authority (none granted)
- exact normative clauses: DCI-123 (AI recommends, deterministic policy authorizes), DCI-124 (planner cannot weaken policy), DCI-129 (effectiveness evidence requires provenance), DCI-054 (model-independent protocols)
- assumptions: A1 `protocol.Unmarshal` uses `DisallowUnknownFields` (protocol.go:273-274) and the schema has `additionalProperties:false`, so both change together. A2 the validator is santhosh-tekuri/jsonschema v6 (full Draft 2020-12): `if/then`, `minItems`, `$ref` via `devcadence:///` are available (`schemas/auth-evidence.schema.json:112` already uses `if/then`).
- re-resolution triggers: any need for a new record kind, a required (non-optional) field, a change to `CognitionPortfolio`, validator or activation code

## Semantic scope envelope

### Authorized domains / path patterns

- `internal/protocol/portfolio.go` (+ new tests in `internal/protocol/`); reuse helpers `requireNonEmptyTrimmed` (review_ledger.go:682) and `enumError` (protocol.go:373)
- `schemas/portfolio-recommendation.schema.json`, `schemas/README.md` only if a description line mentions the record
- `fixtures/protocol/portfolio-recommendation.*.json` (new files), `tests/schema_fixtures_test.go`, `tests/twin_fields_test.go`
- `docs/PROTOCOLS.md` (PortfolioRecommendation section), `docs/COGNITION_PORTFOLIO.md` §9, `docs/WORK_PACKAGES.md` (M3D-1 card: note the 1A/1B split)

### Explicitly forbidden semantic changes

- No new record kind; no change to `RecordKind`, `NewRecord`, `RecordKindToSchema`, `AllNames`.
- No change to `CognitionPortfolio`, `internal/cognition/**`, validator, activation, routing.
- No existing field made optional, renamed or retyped; no existing field's validation weakened. `schema_version` stays `"1.0"` (additive, backward compatible; recorded decision D-1).
- No field in this WP may be consumed by any decision logic.

### LOCAL_DISCRETION

Helper names, test layout, comment wording.

## Requirements

| ID | Strength | Requirement | Source |
| --- | --- | --- | --- |
| REQ-01 | MUST | Add optional fields to `PortfolioRecommendation`, all five tagged `omitempty` (needs `import "strings"` in portfolio.go), appended after `CapabilityProvenance` in this order (struct order = marshal key order, normative): `SetID string` `set_id`; `Intent RecommendationIntent` `intent`; `Tradeoffs []string` `tradeoffs`; `Confidence RecommendationConfidence` `confidence`; `Planner *PlannerProvenance` `planner`. | COGNITION_PORTFOLIO §9; WORK_PACKAGES WP-M3D-1; design decision D-1 |
| REQ-02 | MUST | `RecommendationIntent` string type with exactly the values `minimum_spend`, `balanced`, `maximum_quality_within_policy`, `privacy_first`; a `Valid()` method; unknown non-empty value ⇒ `Validate()` error. | COGNITION_PORTFOLIO §9 (candidate kinds) |
| REQ-03 | MUST | `RecommendationConfidence` string type with exactly `high`, `medium`, `low`; `Valid()`; unknown non-empty ⇒ error. A new type (not reuse of `FindingConfidence`). The unprefixed `ConfidenceHigh/Medium/Low` identifiers are already taken (`review_ledger.go:109-116`); the new constants MUST be named exactly `RecommendationConfidenceHigh`, `RecommendationConfidenceMedium`, `RecommendationConfidenceLow`. | WORK_PACKAGES WP-M3D-1 (confidence); DCI-129 |
| REQ-04 | MUST | `PlannerProvenance{EndpointID string `endpoint_id`; DriverID string `driver_id`; ModelID string `model_id,omitempty`; InvocationDigest string `invocation_digest`}`. `endpoint_id`, `driver_id`, `invocation_digest` required non-empty (after `strings.TrimSpace`) when `planner` is present; `model_id` optional and unconstrained. `invocation_digest` has NO format validation (do not use `validateSHA256Digest`). `PlannerProvenance` has its own `Validate()` checking endpoint_id, driver_id, invocation_digest in that order. | design decision D-4; DCI-129 |
| REQ-05 | MUST | Each `tradeoffs` item MUST be non-empty after trim; empty slice and absent are equivalent; duplicates allowed; order preserved. | COGNITION_PORTFOLIO §9 |
| REQ-06 | MUST | `SetID` is a plain string: `""` means absent; a non-empty value that trims to empty (e.g. `" "`) is an error. Trimming is for the emptiness check only; stored values are never normalised. `Planner` absent = nil pointer; JSON `"planner": null` decodes to nil (Go accepts as absent; the schema `type:object` rejects null — allowed by INV-03); `"planner": {}` is present and fails required checks. Enums are not trimmed. | design decision D-1 |
| REQ-07 | MUST | **Consistency rule, in BOTH schema and Go**: when `planner` is present, `intent`, `confidence`, `tradeoffs` (minItems 1) and `set_id` are required; Go `Validate()` returns `CategoryInvalidArgument` otherwise. When `planner` is absent all five fields remain optional. The schema expresses it as top-level `"if": {"required":["planner"]}, "then": {"required":["intent","confidence","tradeoffs","set_id"], "properties":{"tradeoffs":{"minItems":1}}}`. Go additionally rejects whitespace-only strings, which the schema cannot (minLength accepts `" "`; ACC-06 is Go-only). | design decision D-3 (AI output must be explained) |
| REQ-08 | MUST | Schema: add the five properties with `enum` for intent/confidence, `minLength:1` for strings, `items.minLength:1` for tradeoffs, nested `planner` object with `additionalProperties:false`, properties endpoint_id/driver_id/model_id/invocation_digest and `required:[endpoint_id,driver_id,invocation_digest]`; none added to top-level `required` except through the REQ-07 `if/then`. | A1, A2 |
| REQ-09 | MUST | `tests/twin_fields_test.go` passes UNMODIFIED (it checks only top-level properties vs json tags and handles omitempty/pointer). Because nested `planner` fields are not covered by it, add a unit test in `internal/protocol` that reflects over `PlannerProvenance` json tags and compares them with the schema's `properties.planner.properties` keys. | repo convention |
| REQ-10 | MUST NOT | No code outside `internal/protocol` may read the new fields in this WP. | DCI-123/124 |
| REQ-11 | MUST | `docs/PROTOCOLS.md` `### PortfolioRecommendation` (§3C) and `docs/COGNITION_PORTFOLIO.md` `## 9. Portfolio recommendation` add the field names and state: the fields are informational; they never grant, expand or substitute for validation authority; `planner` absent means non-AI/heuristic; Go additionally rejects whitespace-only values. | docs sync |

## Invariants / state rules

| ID | Statement | Reqs |
| --- | --- | --- |
| INV-01 | Every record valid before this WP is still valid and marshals byte-identically (new fields omitted when empty). | REQ-01 |
| INV-02 | Marshal→Unmarshal round-trips are lossless for every new field. | REQ-01, REQ-04 |
| INV-03 | Anything the schema rejects the Go reader rejects (Go may reject more: REQ-07). | REQ-07, REQ-08 |
| INV-04 | New fields carry no authority. | REQ-10 |

## Interface / algorithm contract

```go
type RecommendationIntent string
const (
    IntentMinimumSpend RecommendationIntent = "minimum_spend"
    IntentBalanced RecommendationIntent = "balanced"
    IntentMaximumQualityWithinPolicy RecommendationIntent = "maximum_quality_within_policy"
    IntentPrivacyFirst RecommendationIntent = "privacy_first"
)
func (i RecommendationIntent) Valid() bool  // true only for the four values above
type RecommendationConfidence string  // ConfidenceHigh/Medium/Low
func (c RecommendationConfidence) Valid() bool
type PlannerProvenance struct { ... } // REQ-04
```

`Validate()`: append after the existing `rationale` check and before `return nil` (existing checks and their order are unchanged; existing Validate does not inspect `explanatory_diagnostics`/`capability_provenance`, and this WP does not either): (1) if `Intent != ""` and `!Intent.Valid()` ⇒ `enumError(kind, "intent", ...)`; (2) same for `confidence`; (3) each tradeoff via `requireNonEmptyTrimmed(kind, "tradeoffs[i]", t)`; (4) if `SetID != ""` ⇒ `requireNonEmptyTrimmed(kind, "set_id", ...)`; (5) if `Planner != nil`: `Planner.Validate()` wrapped as `errs.New(errs.CategoryInvalidArgument, "%s: planner: %v", kind, err)`, then REQ-07 presence checks (`requireNonEmpty`-style on intent/confidence/set_id, `len(Tradeoffs) >= 1`).

## Authority matrix

| Decision/effect | Authorized source | Forbidden substitute |
| --- | --- | --- |
| Whether a portfolio is acceptable/active | `cognition.PortfolioValidator` + `ActivationManager` | `intent`, `confidence`, `tradeoffs`, `planner` |

## Missing / unknown / stale input semantics

| Input | Missing | Unknown | Stale | Malformed |
| --- | --- | --- | --- | --- |
| optional new fields | valid (heuristic record) | unknown enum value ⇒ reject | n/a | reject per REQ-02..07 |
| `planner` present without intent/confidence/tradeoff/set_id | n/a | n/a | n/a | Go reject (REQ-07), schema accepts |

## Failure matrix

| Condition | Required postcondition | Evidence |
| --- | --- | --- |
| `confidence: "critical"` | schema and Go both reject | fixture `portfolio-recommendation.invalid-confidence-critical.json` |
| `intent: "cheapest"` | both reject | `…invalid-intent-unknown` |
| tradeoff item `""` | both reject | `…invalid-empty-tradeoff` |
| `planner` missing `endpoint_id` | both reject | `…invalid-planner-missing-endpoint` |
| `planner` present, no `confidence` | both reject (REQ-07 is in the schema) | fixture `…invalid-planner-missing-confidence` |
| extra unknown key inside `planner` | both reject | `…invalid-planner-extra-field` |

## Representability map

| Concept | Representation | Adequacy |
| --- | --- | --- |
| alternative set | `set_id` shared by sibling records | represented; set-level invariants (same `inventory_digest` across siblings) are the planner service's job (M3D-1B), not this record's |
| intent | `RecommendationIntent` | represented |
| tradeoffs | `[]string` | represented; free text by design (D-2) |
| confidence | `RecommendationConfidence` | represented |
| planner provenance | `PlannerProvenance` | represented; `invocation_digest` format not constrained here beyond non-empty |

## Acceptance scenarios

Test and subtest names MUST contain `PortfolioRecommendation` and the `ACC-xx` id.

| ID | Setup | Action | Expected | Maps |
| --- | --- | --- | --- | --- |
| ACC-01 | existing `portfolio-recommendation.valid.json` | existing round-trip tests pass unchanged; plus a unit test marshals the decoded record and asserts none of `set_id`,`intent`,`tradeoffs`,`confidence`,`planner` appear and `Marshal(Unmarshal(Marshal(x))) == Marshal(x)` | still valid, new keys absent (semantic compare, not byte-equality with the pretty-printed file) | INV-01 |
| ACC-02 | new `portfolio-recommendation.planner.valid.json` with all five fields (and `model_id`); added to the round-trip `cases` table | schema-validate, round-trip | valid; all fields preserved | REQ-01..08, INV-02 |
| ACC-03 | the six invalid fixtures named in the Failure matrix, each `portfolio-recommendation.invalid-<slug>.json` (slugs: `confidence-critical`, `intent-unknown`, `empty-tradeoff`, `planner-missing-endpoint`, `planner-missing-confidence`, `planner-extra-field`), each differing from the valid planner fixture by that single defect | schema and Go reader | both reject each | REQ-02..04, 08, INV-03 |
| ACC-04 | programmatic: planner present with each of {no intent, no confidence, empty tradeoffs, no set_id} removed in turn; planner absent with all four absent; planner absent with only intent | `Validate()` | first four reject; last two valid | REQ-07 |
| ACC-05 | `tests/twin_fields_test.go` unmodified plus nested `PlannerProvenance` tag-vs-schema unit test | run | both pass | REQ-09 |
| ACC-06 | whitespace-only `set_id`, whitespace-only `planner.endpoint_id`, whitespace tradeoff | `Validate()` | each rejected | REQ-04..06 |
| ACC-07 | manual evidence in the PR: `grep -rnE '\.(SetID|Tradeoffs|Planner)\b' --include=*.go internal cmd` excluding `internal/protocol/` and `_test.go` (`Intent`/`Confidence` excluded: other types use them) | run and paste output | zero hits | REQ-10 |
| ACC-08 | docs | `PROTOCOLS.md` and `COGNITION_PORTFOLIO.md` describe the fields and REQ-11 | present | REQ-11 |

## Validation

- `go build ./... && go vet ./... && gofmt -l <changed files>` clean
- `go test -count=1 ./...`, `go test -race ./internal/protocol/... ./tests/...`
- `make hooks-check`; no hook bypass
- mutation checks (implementer records): remove REQ-07 block ⇒ ACC-04 fails; drop `enum` from schema confidence ⇒ ACC-03 fixture fails the schema side; drop `omitempty` on `SetID` ⇒ ACC-01 fails
- required independent review lenses: contract/correctness, schema-twin parity

## Escalation triggers

A required (non-optional) field seems necessary; the schema validator lacks a feature the contract needs; any consumer outside `internal/protocol` needs the fields; REQ-07 conflicts with an existing fixture.

## Design / rationale

- D-1 additive optional fields vs new `PortfolioRecommendationSet` kind vs breaking v2: chosen additive optional because it needs no new registration touchpoints, keeps all fixtures valid and defers set semantics to the service that actually builds sets. Cost: set invariants are not type-enforced (accepted; M3D-1B enforces and tests them).
- D-2 tradeoffs as `[]string` vs structured objects: free text is what ADR-0018 asks ("explains tradeoffs"); structured criteria would invent a vocabulary before there is evidence (M4) of what is useful.
- D-3 REQ-07 lives in BOTH schema (`if/then`, available in the Draft 2020-12 validator) and Go, so a planner-produced record cannot be unexplained at either boundary; Go alone adds whitespace checks. An earlier draft claimed the validator lacked conditionals; that was wrong (found by independent review).
- D-4 `invocation_digest` ties a recommendation to the compiled planner invocation (compiler `InvocationDigest`) for DCI-129 provenance without embedding prompts.

## Implementation Readiness Report

```text
requirements represented: 11/11
mandatory clauses resolved: 4/4
state transitions specified: n/a (pure record)
failure cases specified: 6/6 (all schema-expressible; ACC-06 whitespace is Go-only)
authority decisions specified: 1/1
missing/unknown input semantics: 2/2
acceptance scenarios mapped: 8/8
unresolved architecture choices: 0
declared local-discretion choices: 3
readiness: READY_FOR_IMPLEMENTATION (independent review r1 NOT_READY; blockers 1-3 and majors 4-8 fixed)
```
