# WP-M3C-H3 — Context profile ↔ binding consistency in the portfolio validator

## Identity

- Work Package ID: WP-M3C-H3
- Revision: 1
- Task ID: autonomous-run-1 (issue #36)
- Base commit: `596019451e4cd55f4bc12e34ddc0c5bd97fffd43` (main after PR #43)
- Contract digest: n/a (Markdown contract authoritative)
- Target implementation endpoint/profile: competent Go implementer
- Status: DRAFT pending independent readiness review

## Objective

A role binding or fallback binding references a context profile by id. Today the validator checks only that the profile exists and then uses its observed context control, prefix cache and contract limit to judge the binding. It never checks that the profile *describes the binding's own endpoint and channel* (`ContextProfile.EndpointID`/`ChannelID`). A binding can therefore be judged against another endpoint's profile, so a compatibility or `MinContractLimitTokens` decision may rest on the wrong facts. Add a fail-closed consistency diagnostic. (Found by the WP-M3D-1B re-review; verified by the Principal: no reference to `prof.EndpointID`/`prof.ChannelID` exists in `internal/cognition/portfolio_validator*.go`.)

## Context Manifest

- role: implementer (Go), independent reviewers
- read-authority envelope: `internal/cognition/portfolio_validator*.go`, `portfolio_diagnostics.go`, `internal/protocol/context.go`, `internal/protocol/portfolio.go` (RoleBinding, FallbackBinding), existing cognition tests and `tests/m3c_substrate_test.go`
- semantic write/scope envelope: below
- risk tags: authorization boundary (validator), fail-closed behavior change
- exact normative clauses: DCI-123 (AI recommends; deterministic policy authorizes: validates existence, capability, feature compatibility, resource constraints BEFORE activation), ADR-0019 §1 (context control/prefix cache must reflect observation)
- assumptions: A1 `ContextProfile` has required non-empty `EndpointID` and `ChannelID` (`internal/protocol/context.go:100-101,141-146`). A2 `RoleBinding` and `FallbackBinding` each carry `EndpointID`, `ChannelID`, `ContextProfileID`. A3 the context checks live in `validateContextCompatibility` called at `portfolio_validator_constraints.go:86` (primary) and `:89` (fallbacks). The implementer re-verifies A1-A3 and escalates if false.
- re-resolution triggers: any need to change protocol types, schemas, `ValidationPolicy`, or other validator dimensions

## Semantic scope envelope

### Authorized domains / path patterns

- `internal/cognition/portfolio_validator_constraints.go` (call sites / helper), `internal/cognition/portfolio_diagnostics.go` (one new code), new tests under `internal/cognition/`
- `internal/cognition/planner/planner_test.go` or a new planner test file: ONLY ACC-11
- Existing tests/fixtures that currently reference a profile belonging to a different endpoint/channel MAY be corrected minimally so they stay valid (each correction listed in the PR description), in `internal/cognition/*_test.go` and `tests/m3c_substrate_test.go`
- `docs/COGNITION_PORTFOLIO.md` §10 validation list, `docs/WORK_PACKAGES.md` (short note)

### Explicitly forbidden semantic changes

- No change to protocol types or schemas; no new `ValidationPolicy` field (PolicyDigest MUST NOT change); no change to any other validator dimension's rules; no change to activation, routing, planner code.
- Do not weaken or remove any existing assertion to make a test pass: a fixture that is genuinely inconsistent is corrected; an assertion is never loosened.

### LOCAL_DISCRETION

Helper names, test layout, message wording.

## Requirements

| ID | Strength | Requirement | Source |
| --- | --- | --- | --- |
| REQ-01 | MUST | New diagnostic code `CodeContextProfileMismatch = "CONTEXT_PROFILE_MISMATCH"` in `portfolio_diagnostics.go`, next to the other context codes. | DCI-123 |
| REQ-02 | MUST | For every role binding and every fallback binding whose `ContextProfileID` EXISTS in `ctx.input.ContextProfiles` (nil entries skipped), compare `prof.EndpointID` with the binding's `EndpointID` and `prof.ChannelID` with the binding's `ChannelID` (exact string equality, no trimming/normalization). If either differs, emit ONE diagnostic for that binding (not one per field). Evaluated even if the channel itself is missing from the portfolio. | DCI-123 |
| REQ-03 | MUST | Diagnostic fields: `Code = CodeContextProfileMismatch`, `Condition = ConditionInvalid`, `ViolatedRule = "DCI-123"`, `Target` = the same target string the existing context checks use for that binding (primary: the string built at the `:86` call site, of the form `role_bindings[<i>]` followed by the role suffix; fallback: the string built at the `:89` call site), `Observed = "endpoint_id=<profile.EndpointID> channel_id=<profile.ChannelID>"`, `Required = "endpoint_id=<binding.EndpointID> channel_id=<binding.ChannelID>"`, `Message` free text. | determinism |
| REQ-04 | MUST | Always evaluated; NOT gated by any policy flag; no new field in `ValidationPolicy` (PolicyDigest unchanged). | fail-closed |
| REQ-05 | MUST | A nonexistent profile id yields ONLY the existing `CONTEXT_PROFILE_NOT_FOUND` (no mismatch diagnostic). | no double report |
| REQ-06 | MUST | Existing context checks (context-control mismatch, prefix-cache mismatch, contract limit, known-context-control policy) are UNCHANGED and still run against the referenced profile; a mismatched binding may therefore also produce them. | scope |
| REQ-07 | MUST | One diagnostic per mismatched binding or fallback occurrence, even if the same profile id is referenced by several bindings. Emission order follows binding order, primary before its fallbacks; final order is the existing `SortedDiagnostics`. | determinism |
| REQ-08 | MUST | `docs/COGNITION_PORTFOLIO.md` §10 lists the consistency check; the WORK_PACKAGES note records WP-M3C-H3 and that the planner (WP-M3D-1B) gets this check through the validator without code change. | docs sync |

## Invariants / state rules

| ID | Statement | Reqs |
| --- | --- | --- |
| INV-01 | No binding is judged on context facts from a profile that describes a different endpoint or channel without a diagnostic. | REQ-02 |
| INV-02 | Validation remains pure and deterministic. | REQ-07 |
| INV-03 | Valid, consistent portfolios (all existing valid fixtures after minimal correction) validate exactly as before. | REQ-06 |

## Interface / algorithm contract

```
at each existing call site of validateContextCompatibility(chID, profID, target)
  (primary: rb.ChannelID, rb.ContextProfileID, the primary target string;
   fallbacks: fb.ChannelID, fb.ContextProfileID, the fallback target string)
add, with endpointID = rb.EndpointID / fb.EndpointID:
  prof := ctx.input.ContextProfiles[profID]
  if prof == nil: skip                       // not-found is reported by the endpoints dimension
  if prof.EndpointID != endpointID || prof.ChannelID != chID:
      emit CONTEXT_PROFILE_MISMATCH{...REQ-03}
```

The check may be a separate helper invoked from the same loop so existing early returns in `validateContextCompatibility` (missing channel or profile) do NOT suppress it.

## Authority matrix

| Decision/effect | Authorized source | Forbidden substitute |
| --- | --- | --- |
| Whether a binding's context facts are the right ones | validator, exact id equality | caller/planner assurance |

## Missing / unknown / stale input semantics

| Input | Missing | Unknown | Stale | Malformed/contradictory |
| --- | --- | --- | --- | --- |
| profile id not in `ContextProfiles` | existing `CONTEXT_PROFILE_NOT_FOUND` only | n/a | n/a | n/a |
| nil profile entry | skipped (no mismatch; not-found rules unchanged) | n/a | n/a | n/a |
| profile with empty EndpointID/ChannelID | n/a | n/a | n/a | differs from the binding ⇒ mismatch (fail closed) |
| `ContextProfiles` nil | unchanged (every reference already not-found) | n/a | n/a | n/a |

## Failure matrix

| Condition | Required postcondition | Evidence |
| --- | --- | --- |
| profile of another endpoint | invalid, one mismatch diagnostic | ACC-02 |
| profile of another channel (same endpoint) | invalid, one mismatch diagnostic | ACC-03 |
| both differ | exactly one diagnostic | ACC-04 |
| fallback mismatch | diagnostic with the fallback target | ACC-05 |
| profile id absent | only CONTEXT_PROFILE_NOT_FOUND | ACC-06 |

## Representability map

| Concept | Representation | Adequacy |
| --- | --- | --- |
| profile identity | `ContextProfile.EndpointID`, `ChannelID` | represented |
| diagnostic | new code constant + existing `PortfolioDiagnostic` | represented |

## Acceptance scenarios

Test names MUST contain `ContextProfileConsistency` and the `ACC-xx` id.

| ID | Setup | Action | Expected | Maps |
| --- | --- | --- | --- | --- |
| ACC-01 | portfolio whose bindings and fallbacks reference profiles with matching endpoint and channel | `Validate` | no `CONTEXT_PROFILE_MISMATCH` | INV-03 |
| ACC-02 | primary binding references a profile whose `EndpointID` differs | `Validate` | invalid; exactly one mismatch diagnostic; Observed/Required strings per REQ-03 | REQ-02, 03 |
| ACC-03 | profile `ChannelID` differs, endpoint equal | `Validate` | exactly one mismatch diagnostic | REQ-02 |
| ACC-04 | both differ | `Validate` | exactly one diagnostic (not two) | REQ-02 |
| ACC-05 | a fallback references a mismatched profile | `Validate` | one diagnostic whose Target is the fallback target | REQ-02, 03 |
| ACC-06 | binding references a nonexistent profile id | `Validate` | `CONTEXT_PROFILE_NOT_FOUND` present, no mismatch diagnostic | REQ-05 |
| ACC-07 | binding's channel is missing from the portfolio and the profile's endpoint differs | `Validate` | mismatch diagnostic still emitted | REQ-02 |
| ACC-08 | one mismatched profile id referenced by two bindings | `Validate` | two diagnostics (one per binding), sorted deterministically | REQ-07 |
| ACC-09 | `PolicyDigest` of a default-policy validation | compare with the literal captured on the base commit | unchanged | REQ-04 |
| ACC-10 | same invalid input, 20 runs, ContextProfiles map built in different insertion orders | `Validate` | identical ordered diagnostics | INV-02 |
| ACC-11 | planner (WP-M3D-1B) alternative referencing another endpoint's profile | `planner.Plan` with a fake invoker | alternative in `Rejected` with Reason `validation_failed` and the diagnostic code present | REQ-02 |
| ACC-12 | existing context-control mismatch / contract-limit tests | run unchanged | still pass (no assertion weakened) | REQ-06 |
| ACC-13 | docs | `COGNITION_PORTFOLIO.md` §10 and WORK_PACKAGES | describe the check | REQ-08 |

## Validation

- `go build ./... && go vet ./... && gofmt -l <changed files>` clean
- `go test -count=1 ./...`; `go test -race -count=1 ./internal/cognition/... ./tests/...`
- `make hooks-check`; hooks, no bypass
- mutation catalog (mutant → ACC that must fail): drop the endpoint comparison ⇒ ACC-02; drop the channel comparison ⇒ ACC-03; skip fallbacks ⇒ ACC-05; emit per field (two diagnostics when both differ) ⇒ ACC-04; gate the check on channel existence (put it behind the existing early return) ⇒ ACC-07; emit also for nonexistent profile ⇒ ACC-06; compare the profile to the wrong binding field (e.g. fallback uses the primary's endpoint) ⇒ ACC-05; downgrade Condition or change the code string ⇒ ACC-02; dedupe by profile id ⇒ ACC-08; add a policy flag (changes PolicyDigest) ⇒ ACC-09; trim/case-fold ids before comparing ⇒ add a test with a case-different id expecting a mismatch (part of ACC-03)
- required review lenses: contract/authority, mutation. (No untrusted-input parsing here.)
- every existing test fixture corrected for consistency is listed in the PR description with before/after ids

## Escalation triggers

A needed change to protocol types, schemas or `ValidationPolicy`; an existing test that can only pass by loosening an assertion; a legitimate use case where one profile must describe several channels or endpoints (A1 says it cannot).

## Design / rationale

- D-1 Always-on vs policy-flagged: a binding judged on another endpoint's facts is a structural authorization error, like referencing a nonexistent channel; a flag would default to the unsafe behavior and change `PolicyDigest` for every stored policy.
- D-2 One diagnostic per binding, not per field: the remedy is the same (bind the right profile) and counts stay stable.
- D-3 Exact equality: ids are opaque identifiers; normalization would create aliasing.
- D-4 Not-found and mismatch are mutually exclusive to avoid double reporting of one root cause.
- Known limits: the check does not verify that the profile's `ModelRef`/runtime matches the endpoint's declared model (separate concern); it does not verify that observed facts are fresh.

## Implementation Readiness Report

```text
requirements represented: 8/8
mandatory clauses resolved: 2/2
state transitions specified: n/a (pure validation)
failure cases specified: 5/5
authority decisions specified: 1/1
missing/unknown input semantics: 4/4
acceptance scenarios mapped: 13/13
unresolved architecture choices: 0
declared local-discretion choices: 3
readiness: pending independent readiness review
```
