# WP-M3C-H4 — Effective-policy consistency and trimmed non-empty fields

## Identity

- Work Package ID: WP-M3C-H4 (window 2026-10-A, with WP-M3D-1C1 and WP-M3D-2A1)
- Revision: 2 (window review probe: READY_WITH_FIXES, wording only; REQ-01 and REQ-02 implemented in a scratch copy: zero existing tests or fixtures break)
- Base commit: current `origin/main` at implementation time (the implementor records the SHA)
- Status: READY_FOR_IMPLEMENTATION (r2)

## Objective

Two mechanical hardenings that remove latent fail-open edges: (1) the one remaining read of `ctx.input.Policy` instead of the effective policy in the portfolio validator; (2) identity and free-text fields in `internal/protocol/portfolio.go` that accept whitespace-only values.

## Context Manifest

- read-authority: `internal/cognition/portfolio_validator*.go`, `internal/protocol/portfolio.go`, `internal/protocol/review_ledger.go` (`requireNonEmptyTrimmed`), protocol fixtures and tests
- risk tags: validator authority, record validation strictness
- normative clauses: DCI-123 (validator authority); the stricter-record rule follows the fail-closed pattern of WP-M3D-1A INV-03
- re-resolution triggers: any need to change a JSON schema, a fixture, `ValidationPolicy`, or `DefaultValidationPolicy()`

## Verified facts about the current code (the implementor re-verifies each as step 0 and reports any false row)

| ID | Claim | Evidence |
| --- | --- | --- |
| F-01 | The only validator read of `ctx.input.Policy` besides the effective-policy builder is `RequireVerifiedAcceleration` in `portfolio_validator_constraints.go` (search `ctx.input.Policy`) | grep |
| F-02 | `DefaultValidationPolicy()` leaves `RequireVerifiedAcceleration` false, so switching that read to `ctx.policy` is behavior-preserving for nil and default policies | `portfolio_validator.go` |
| F-03 | `requireNonEmpty` (protocol.go) is a plain `value == ""` check; `requireNonEmptyTrimmed` (review_ledger.go) trims | source |
| F-04 | In `portfolio.go` (verified by `grep 'requireNonEmpty('`; no extra and no missing sites) the untrimmed checks are in the `Validate` methods of: FallbackBinding (endpoint_id, channel_id, budget_pool_id, context_profile_id), RoleBinding (role, endpoint_id, channel_id, budget_pool_id, context_profile_id), EscalationRule (from_role, to_role, trigger_condition), CognitionPortfolio (portfolio_id, created_at), PortfolioRecommendation (recommendation_id, inventory_digest, synthesized_at, rationale), WorkflowStage (stage_id, role, budget_pool_id; optional pointers endpoint_id, channel_id, context_profile_id, escalation_target, deterministic_gate_id), WorkflowPlan (plan_id, task_id, work_package_id) | scout report, grep `requireNonEmpty(` in portfolio.go |
| F-05 | JSON schemas use `minLength:1`, which accepts whitespace-only strings; Go being stricter than the schema is allowed (a schema-rejected value is still Go-rejected) | WP-M3D-1A INV-03 |

## Semantic scope envelope

### Authorized domains

- `internal/cognition/portfolio_validator_constraints.go` (that one read), new tests under `internal/cognition/` and `internal/protocol/`
- `internal/protocol/portfolio.go` (the `Validate` methods listed in F-04 only)
- `docs/PROTOCOLS.md` / `docs/COGNITION_PORTFOLIO.md`: one sentence each that identity and free-text fields reject whitespace-only values

### Forbidden

- No schema or fixture change; no change to field names or error categories; no other Validate method; no change to `ValidationPolicy`/`DefaultValidationPolicy()`; no change to error message field names (messages keep the JSON key).
- A fixture or existing test that becomes invalid is a STOP-and-report (expected count: zero).

### LOCAL_DISCRETION

Test layout; table-driven structure; helper names.

## Requirements

| ID | Strength | Requirement |
| --- | --- | --- |
| REQ-01 | MUST | The `RequireVerifiedAcceleration` check reads `ctx.policy.RequireVerifiedAcceleration` (effective policy), not `ctx.input.Policy`. |
| REQ-02 | MUST | Every field listed in F-04 uses `requireNonEmptyTrimmed(kind, "<json key>", value)`; optional pointer fields that are set use the same trimmed check on the pointed-to value (the existing "if set must be non-empty" semantics are kept, now trimmed). |
| REQ-03 | MUST | A nil optional pointer remains valid. A whitespace-only value is rejected with `errs.CategoryInvalidArgument` and a message containing the JSON key (exact wording is LOCAL_DISCRETION; for optional pointer fields `requireNonEmptyTrimmed`'s wording "is required and must not be empty or whitespace-only" is acceptable). `deterministic_gate_id` has two branches: the one required for deterministic stages MUST be trimmed (test it on a deterministic stage); the one forbidden for cognition stages already rejects any non-nil value. Stored values are never normalized. |
| REQ-04 | MUST | Docs state the stricter rule (one sentence each). |

## Invariants

| ID | Statement |
| --- | --- |
| INV-01 | Every record valid before this WP with non-whitespace values is still valid (no fixture changes). |
| INV-02 | Validator output for nil, default and explicit-default policies is unchanged. |

## Authority matrix

No authority is added, removed or substituted; both changes can only turn a previously accepted ill-formed input into a rejection (fail-closed), except REQ-01 which is behavior-preserving today and prevents a future default flip from being silently ignored.

## Missing / unknown input semantics

| Input | Missing | Whitespace-only | Malformed |
| --- | --- | --- | --- |
| required string field | rejected (unchanged) | rejected (new) | n/a |
| optional pointer field | nil is valid (unchanged) | rejected (new) | n/a |
| `ValidationInput.Policy` | nil ⇒ effective default (unchanged) | n/a | n/a |

## Failure matrix

| Condition | Required postcondition | Evidence |
| --- | --- | --- |
| a listed field is `"   "` | Validate returns InvalidArgument naming the key | ACC-01 |
| an optional pointer is `&"  "` | rejected | ACC-02 |
| explicit policy with `RequireVerifiedAcceleration: true` and unverified acceleration | diagnostic emitted as before | ACC-04 |

## Acceptance scenarios

Test names MUST contain `EffectivePolicyAndTrimmedFields` and the ACC id.

| ID | Setup | Action | Expected |
| --- | --- | --- | --- |
| ACC-01 | table over EVERY required field in F-04, each set to `"   "` on an otherwise valid record | `Validate()` | each rejected; the error names that field's JSON key |
| ACC-02 | table over every optional pointer in F-04 set to pointer-to-`"  "` | `Validate()` | each rejected; nil still valid |
| ACC-03 | all existing protocol fixtures and tests | run unchanged | pass (INV-01) |
| ACC-04 | an input with a role binding whose inventory endpoint has `Kind` local_runtime (or `Locality` local), a non-CPU, non-unknown `AccelerationBackend` (e.g. metal) and `AccelerationVerified` false (the diagnostic fires only for such an endpoint); (a) through public `Validate` with explicit `ValidationPolicy{RequireVerifiedAcceleration: true}` ⇒ the `ACCELERATION_UNVERIFIED` diagnostic as before; nil policy ⇒ unchanged output; the digest of nil-policy validation equals the explicit-default digest; (b) a NEW `package cognition` (internal) test file, because every existing cognition test is `package cognition_test`: build a context with `newValidatorContext(input, effective)` where `input.Policy == nil` and `effective.RequireVerifiedAcceleration == true`, call `validateContextAndConstraints(&diags)`, assert `ACCELERATION_UNVERIFIED` | (a) as stated; (b) the diagnostic is present with the fix and absent under the REQ-01 mutant (`ctx.input.Policy`) |
| ACC-05 | docs | read | the sentences exist |

## Validation

- `go build ./... && go vet ./... && gofmt -l <changed files>`; `go test -count=1 ./...`; `go test -race -count=1 ./internal/cognition/... ./internal/protocol/... ./tests/...`; `make docs-check`; hooks, no bypass
- mutation catalog: for EACH field site in F-04 revert it to the untrimmed check ⇒ the ACC-01/02 table entry for that field fails; revert REQ-01 to `ctx.input.Policy` ⇒ ACC-04(b) fails
- required review lenses: contract/authority; mutation

## Escalation triggers

A fixture or existing test breaks; a listed field is not found where F-04 says; a schema change seems required.

## Design / rationale

D-1 One mechanical sweep instead of per-field WPs: the change is identical at every site and the table test pins each. D-2 No schema change: Go stricter than schema is already the project's accepted pattern (1A INV-03), and schema `pattern` changes would touch every stored-record consumer.

## Implementation Readiness Report

```text
requirements represented: 4/4
mandatory clauses resolved: 2/2
failure cases specified: 3/3
authority decisions specified: 1/1
missing/unknown input semantics: 3/3
acceptance scenarios mapped: 5/5
unresolved architecture choices: 0
readiness: READY_FOR_IMPLEMENTATION (window review: implementability probe passed, findings incorporated)
```
