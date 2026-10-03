# WP-M3D-1A — PortfolioRecommendation explanation and provenance fields

## Identity

- Work Package ID: WP-M3D-1A (first slice of WP-M3D-1 "AI-assisted Portfolio Planner"; WP-M3D-1B is the planner service and depends on this)
- Revision: 1
- Task ID: autonomous-run-1 (issue #36)
- Base commit: `1cf7a9103bbef45f6435f1ca1eeff42179ef4830` (main after PR #38)
- Contract digest: n/a (Markdown contract authoritative)
- Target implementation endpoint/profile: competent Go implementer; mechanical protocol work
- Status: READY_FOR_IMPLEMENTATION pending independent readiness review (see Implementation Readiness Report)

## Objective

Make the existing `PortfolioRecommendation` record able to carry what ADR-0018 §5/§6 and COGNITION_PORTFOLIO §9 require of a planner output that M3D-1B will produce: an **intent label** for each materially different alternative, **structured tradeoffs**, a **confidence** level, a **set grouping id**, and **planner provenance**. All additions are optional on the wire (existing records stay valid) and **informational only**: no validator, activation or routing decision may read them.

## Context Manifest

- role: implementer (Go, JSON Schema), then independent reviewers
- read-authority envelope: `internal/protocol/portfolio.go`, `internal/protocol/*` tests, `schemas/portfolio-recommendation.schema.json`, `fixtures/protocol/portfolio-recommendation.*`, `tests/schema_fixtures_test.go`, `tests/twin_fields_test.go`, `docs/PROTOCOLS.md`, `docs/COGNITION_PORTFOLIO.md`, `docs/WORK_PACKAGES.md`
- semantic write/scope envelope: below
- risk tags: protocol/schema change (additive), authority (none granted)
- exact normative clauses: DCI-123 (AI recommends, deterministic policy authorizes), DCI-124 (planner cannot weaken policy), DCI-129 (effectiveness evidence requires provenance), DCI-054 (model-independent protocols)
- assumptions: A1 `protocol.Unmarshal` rejects unknown fields and the schema has `additionalProperties:false`, so both must change together. A2 the schema validator in this repo supports `enum`, `minLength`, `required`, nested objects and `$ref` (all already used). `if/then` is NOT assumed available.
- re-resolution triggers: any need for a new record kind, a required (non-optional) field, a change to `CognitionPortfolio`, validator or activation code

## Semantic scope envelope

### Authorized domains / path patterns

- `internal/protocol/portfolio.go` (+ new tests in `internal/protocol/`)
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
| REQ-01 | MUST | Add optional fields to `PortfolioRecommendation` (Go field name / JSON key, all `omitempty` except as noted), appended after `CapabilityProvenance` in this order: `SetID string` `set_id`; `Intent RecommendationIntent` `intent`; `Tradeoffs []string` `tradeoffs`; `Confidence RecommendationConfidence` `confidence`; `Planner *PlannerProvenance` `planner`. | ADR-0018 §5 |
| REQ-02 | MUST | `RecommendationIntent` string type with exactly the values `minimum_spend`, `balanced`, `maximum_quality_within_policy`, `privacy_first`; a `Valid()` method; unknown non-empty value ⇒ `Validate()` error. | COGNITION_PORTFOLIO §9 |
| REQ-03 | MUST | `RecommendationConfidence` string type with exactly `high`, `medium`, `low`; `Valid()`; unknown non-empty ⇒ error. A new type (not reuse of the review `FindingConfidence`). | DCI-129 |
| REQ-04 | MUST | `PlannerProvenance{EndpointID string `endpoint_id`; DriverID string `driver_id`; ModelID string `model_id,omitempty`; InvocationDigest string `invocation_digest`}`. `endpoint_id`, `driver_id`, `invocation_digest` required non-empty (after `strings.TrimSpace`) when `planner` is present; `model_id` optional. | DCI-129 |
| REQ-05 | MUST | Each `tradeoffs` item MUST be non-empty after trim; empty slice and absent are equivalent. | explanation |
| REQ-06 | MUST | `set_id`, when present (non-nil in Go means non-empty string), MUST be non-empty after trim. Empty string is "absent". | grouping |
| REQ-07 | MUST | **Go-only consistency rule**: when `Planner != nil`, `Intent`, `Confidence` MUST be non-empty and `Tradeoffs` MUST have at least one item and `SetID` MUST be non-empty; otherwise `Validate()` returns `CategoryInvalidArgument`. When `Planner == nil` all five fields remain optional (heuristic/deterministic records stay valid). This rule is stricter than the schema by design (schema has no conditional). | DCI-123 |
| REQ-08 | MUST | Schema: add the five properties with `enum` for intent/confidence, `minLength:1` for strings, `items.minLength:1` for tradeoffs, nested `planner` object with `additionalProperties:false` and `required:[endpoint_id,driver_id,invocation_digest]`; none added to top-level `required`. | A1 |
| REQ-09 | MUST | The twin-fields test enumerates the new fields on both sides and passes. | repo convention |
| REQ-10 | MUST NOT | No code outside `internal/protocol` may read the new fields in this WP. | DCI-123/124 |
| REQ-11 | MUST | Docs state: the fields are informational; they never grant, expand or substitute for validation authority; `planner` absent means non-AI/heuristic; Go enforces REQ-07 beyond the schema. | docs sync |

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

`Validate()` order (after the existing checks): (1) if `Intent != ""` and `!Intent.Valid()` error; (2) same for Confidence; (3) each trimmed tradeoff non-empty; (4) if `SetID != ""` and trims to empty ⇒ error; (5) if `Planner != nil`: validate its three required fields, then REQ-07 presence rules. Error messages use the existing `errs.New(errs.CategoryInvalidArgument, "%s: ...", kind, ...)` form naming the JSON key.

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
| `confidence: "critical"` | schema and Go both reject | fixture `…invalid-confidence-critical` |
| `intent: "cheapest"` | both reject | `…invalid-intent-unknown` |
| tradeoff item `""` | both reject | `…invalid-empty-tradeoff` |
| `planner` missing `endpoint_id` | both reject | `…invalid-planner-missing-endpoint` |
| `planner` present, no `confidence` | Go rejects (unit test only; no fixture, because the schema accepts) | ACC-04 |
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
| ACC-01 | existing `portfolio-recommendation.valid.json` | schema-validate, Unmarshal, Marshal | still valid; output equals input bytes after canonical marshal; none of the new keys appear | INV-01 |
| ACC-02 | new `portfolio-recommendation.planner.valid.json` with all five fields | schema-validate, round-trip | valid; all fields preserved | REQ-01..08, INV-02 |
| ACC-03 | the five invalid fixtures in the Failure matrix | schema and Go reader | both reject each | REQ-02..04, 08, INV-03 |
| ACC-04 | programmatic: planner present with each of {no intent, no confidence, empty tradeoffs, no set_id} removed in turn; planner absent with all four absent; planner absent with only intent | `Validate()` | first four reject; last two valid | REQ-07 |
| ACC-05 | twin-fields test | run | passes with new fields | REQ-09 |
| ACC-06 | whitespace-only `set_id`, whitespace-only `planner.endpoint_id`, whitespace tradeoff | `Validate()` | each rejected | REQ-04..06 |
| ACC-07 | grep guard (test): no non-test `.go` file outside `internal/protocol` references `.Planner`, `.Intent`, `.Tradeoffs` on a `PortfolioRecommendation`, `.SetID` | static check or documented manual `grep` in PR evidence | none found | REQ-10 |
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
- D-3 Go stricter than schema (REQ-07): the schema lacks conditional logic in this repo's validator; stricter Go is safe (INV-03) and keeps "AI output must be explained" enforceable at the type boundary.
- D-4 `invocation_digest` ties a recommendation to the compiled planner invocation (compiler `InvocationDigest`) for DCI-129 provenance without embedding prompts.

## Implementation Readiness Report

```text
requirements represented: 11/11
mandatory clauses resolved: 4/4
state transitions specified: n/a (pure record)
failure cases specified: 6/6
authority decisions specified: 1/1
missing/unknown input semantics: 2/2
acceptance scenarios mapped: 8/8
unresolved architecture choices: 0
declared local-discretion choices: 3
readiness: pending independent readiness review
```
