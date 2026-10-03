# WP-M3C-H2 — Fail-closed unknown resource and budget state

## Identity

- Work Package ID: WP-M3C-H2
- Revision: 2
- Task ID: autonomous-run-1 (issue #36, phase B)
- Base commit: f19d16287531f96ac6837670c486291fed4ab5d5
- Project state revision: main @ f19d162 (M3C-4/M3C-5 merged)
- Contract digest: n/a (Markdown contract authoritative per template)
- Target implementation endpoint/profile: competent Go implementer; no architectural judgment required
- Status: READY_FOR_IMPLEMENTATION (revision 2: independent readiness review findings 1-8 incorporated; reviewer verdict READY_WITH_FIXES, claims verified against code by the Principal)

## Objective

Close known gap KG-1 for the portfolio validator: under the **default** validation policy, a supplied `ResourceState` with `UnknownMetrics`, and a supplied `BudgetState` that is `unknown` or missing for a pool a role binding actually uses (non-local-compute), MUST yield a blocking diagnostic instead of silent acceptance. Unknown stays unknown (DCI-005); it is never treated as healthy.

## Context Manifest

- role: implementer (Go), then independent code reviewers
- read-authority envelope: `internal/cognition/portfolio_*.go`, `internal/protocol/economics.go`, `tests/m3c_substrate_test.go`, `docs/WORK_PACKAGES.md`, `docs/PROTOCOLS.md` §10B, `docs/COGNITION_PORTFOLIO.md`
- semantic write/scope envelope: see below
- risk tags: spending authority (fail-closed), validator semantics
- exact normative clauses: DCI-005 (unknown is not healthy), DCI-126 (budget exhaustion), DCI-127 (capacity), DCI-122/124 (metered/regime authority)
- assumptions: A1 `ValidationInput.BudgetStates`/`ResourceStates` being nil means "no observation supplied" and is out of scope here (stays unchecked; remains a documented residual of KG-1). A2 `local_compute` pools carry no spend and are exempt from budget-state requirements.
- re-resolution triggers: any need to change `protocol` types/schemas, add input fields beyond `RequireKnownBudgetState`, or touch other validators

## Semantic scope envelope

### Authorized domains / path patterns

- `internal/cognition/portfolio_validator.go`, `portfolio_validator_constraints.go`, `portfolio_validator_economics.go`, `portfolio_diagnostics.go`, and new/changed `internal/cognition/*_test.go`
- `tests/m3c_substrate_test.go` (the substrate test's ACC-10 group only, plus doc-string assertions if needed)
- `docs/WORK_PACKAGES.md` (KG-1 bullet), `docs/PROTOCOLS.md` §10B (KG-1 status), `docs/COGNITION_PORTFOLIO.md` (document the two policy fields and the rules, and MUST warn that policies built as struct literals or decoded from JSON default both flags to false, i.e. silent opt-out)

### Explicitly forbidden semantic changes

- Leave `portfolio_validator_constraints.go:141` (`RequireVerifiedAcceleration` reading `input.Policy`, same nil-policy defect) untouched; it is harmless today and is a recorded follow-up.
- No `internal/protocol` type or JSON-schema changes. No other validator rules. No change to KG-2..KG-5 text other than leaving it intact. No persistence changes.
- `ValidationPolicy` MAY gain exactly one field `RequireKnownBudgetState bool` (`json:"require_known_budget_state"`). The field MUST be appended directly after `RequireKnownResourceState`, tagged exactly `json:"require_known_budget_state"` with no `omitempty`. This changes `PolicyDigest` output for every policy; accepted and intended. No test pins a computed digest (`recovery_test.go:316` uses a literal string).

### LOCAL_DISCRETION

Helper names, test helper layout, table-driven structure, comment wording.

## Requirements

| ID | Strength | Requirement | Source |
| --- | --- | --- | --- |
| REQ-01 | MUST | `DefaultValidationPolicy()` returns `RequireKnownResourceState: true` and `RequireKnownBudgetState: true`. | DCI-005 |
| REQ-02 | MUST | The unknown-resource-state check MUST read the **effective** policy (`ctx.policy`), not `ctx.input.Policy`; a nil `input.Policy` therefore enforces the defaults. | DCI-005 |
| REQ-03 | MUST | A caller passing an explicit `Policy` with `RequireKnownResourceState:false` / `RequireKnownBudgetState:false` opts out (explicit-only; Go zero value in an explicit policy is an opt-out, documented). | authority |
| REQ-04 | MUST | New diagnostic code `CodeUnknownBudgetState = "UNKNOWN_BUDGET_STATE"`, Condition `ConditionUnknown`, ViolatedRule `"DCI-005"`. | DCI-005 |
| REQ-05 | MUST | When `RequireKnownBudgetState` and `input.BudgetStates != nil`: for each **used** pool (see algorithm) whose regime is not `local_compute`: emit `CodeUnknownBudgetState` if the pool has no entry in `BudgetStates`, or its entry is nil, or its `Status == BudgetStatusUnknown`. | DCI-005 |
| REQ-06 | MUST | Diagnostics for budget checks are emitted in sorted pool-id order (deterministic output); the existing exhaustion loop MUST also iterate sorted pool ids. | determinism |
| REQ-07 | MUST NOT | A nil, absent, or `status=unknown` entry with `RemainingBalance == nil` MUST NOT emit an exhaustion diagnostic; `local_compute` pools MUST NOT emit any unknown-budget diagnostic. The existing `RemainingBalance <= 0` exhaustion branch is UNCHANGED and still applies when `status=unknown` and a balance is present (both diagnostics may then fire). | DCI-126 |
| REQ-08 | MUST | `BudgetStates`/`ResourceStates` nil-map behavior unchanged (no diagnostics). A nil `ResourceStates[host]` entry is skipped without panic. Documented as residual. | scope |
| REQ-10 | MUST | Diagnostic fields: `Required` empty; `Observed` exactly `"missing"` (entry absent or nil) or `"unknown"`; one diagnostic per pool (used-pool set is de-duplicated, a fallback-only pool counts as used); Message free text. | determinism |
| REQ-09 | MUST | Docs updated: KG-1 described as resolved for supplied-but-unknown state with the nil-input residual; substrings `KG-1`…`KG-5` and `KG-1..KG-5` remain present where ACC-14 asserts them. | docs sync |

## Invariants / state rules

| ID | Statement | Reqs |
| --- | --- | --- |
| INV-01 | Validation is pure and deterministic: same input ⇒ same ordered diagnostics. | REQ-06 |
| INV-02 | Unknown observation of a spend-bearing pool never validates under default policy. | REQ-01, REQ-05 |
| INV-03 | Policy opt-out requires an explicit `Policy` value. | REQ-03 |

## Interface / algorithm contract

```
usedPools(p):  set of rb.BudgetPoolID and fb.BudgetPoolID over all role bindings and fallbacks
if ctx.policy.RequireKnownBudgetState && input.BudgetStates != nil:
  for poolID in sorted(usedPools):
     bp, ok := ctx.poolMap[poolID]; if !ok: continue   // undefined pool already reported elsewhere
     if bp.Regime == local_compute: continue
     st := input.BudgetStates[poolID]   // nil if absent
     if st == nil || st.Status == BudgetStatusUnknown:
        emit {Code: UNKNOWN_BUDGET_STATE, Condition: unknown, Target: "budget_pools[<id>]",
              ViolatedRule: "DCI-005", Message: "...unknown/missing...", Observed: "missing"|"unknown"}
exhaustion loop: iterate sorted(poolMap keys); skip nil entries; body (both branches) otherwise unchanged
resource loop: skip nil ResourceStates[host] entry
resource loop (constraints.go ~186): condition becomes
   ctx.policy.RequireKnownResourceState && len(resState.UnknownMetrics) > 0
```

Add the unknown-budget block immediately before the existing "Live Budget State checks".

## Authority matrix

| Decision | Authorized source | Forbidden substitute |
| --- | --- | --- |
| Accept unknown state | explicit `Policy` with flags false | defaults, nil policy, plausible assumption of healthy |

## Missing / unknown / stale input semantics

| Input | Missing (nil map) | Unknown | Stale | Malformed |
| --- | --- | --- | --- | --- |
| BudgetStates | unchanged: no check (residual) | used non-local pool ⇒ UNKNOWN_BUDGET_STATE | out of scope | out of scope |
| BudgetState entry for used pool | map non-nil but absent/nil ⇒ UNKNOWN_BUDGET_STATE | `status=unknown` ⇒ UNKNOWN_BUDGET_STATE | n/a | n/a |
| ResourceStates | nil: unchanged | `UnknownMetrics` non-empty ⇒ UNKNOWN_RESOURCE_STATE (default) | n/a | n/a |

## Failure matrix

| Condition | Postcondition | Evidence |
| --- | --- | --- |
| Default policy, host has unknown metrics | `Valid=false`, one UNKNOWN_RESOURCE_STATE per host (sorted) | ACC-01 |
| Explicit policy flags false | no unknown diagnostics | ACC-03 |
| Pool unknown/missing, local_compute | no diagnostic | ACC-05 |

## Representability map

| Concept | Representation | Adequacy |
| --- | --- | --- |
| Policy flag | `ValidationPolicy.RequireKnownBudgetState` | represented (new field, authorized) |
| Diagnostic | new const in `portfolio_diagnostics.go` | represented |
| Unknown status | `protocol.BudgetStatusUnknown` | represented |
| Used pool | `RoleBinding.BudgetPoolID`, `Fallback.BudgetPoolID` | represented |

## Acceptance scenarios

| ID | Setup | Expected | Maps |
| --- | --- | --- | --- |
| ACC-01 | nil Policy (required); ResourceState with UnknownMetrics | invalid, UNKNOWN_RESOURCE_STATE | REQ-01/02 |
| ACC-02 | nil Policy; used metered pool, `BudgetStates` non-nil with `status=unknown` | invalid, UNKNOWN_BUDGET_STATE | REQ-01/04/05 |
| ACC-03 | explicit Policy with both flags false, same inputs as 01/02 | no unknown diagnostics | REQ-03 |
| ACC-04 | used non-local pool absent from non-nil `BudgetStates` | UNKNOWN_BUDGET_STATE observed "missing" | REQ-05 |
| ACC-05 | local_compute pool unknown/missing | no diagnostic | REQ-07 |
| ACC-06 | exhausted pool | only exhaustion diagnostic, no unknown one | REQ-07 |
| ACC-07 | nil `BudgetStates`/`ResourceStates` | unchanged (valid if otherwise valid) | REQ-08 |
| ACC-08 | two unknown pools, input map order shuffled across runs | diagnostics identical and sorted | REQ-06 |
| ACC-09 | unused (unreferenced) pool unknown | no diagnostic | REQ-05 |
| ACC-11 | `BudgetStates[pool]` is a nil value for a used metered pool | UNKNOWN_BUDGET_STATE observed "missing", no panic | REQ-05/10 |
| ACC-12 | pool used only as a fallback is unknown | exactly one UNKNOWN_BUDGET_STATE | REQ-05/10 |
| ACC-13 | `status=unknown` with `RemainingBalance` = 0 | both UNKNOWN_BUDGET_STATE and BUDGET_POOL_EXHAUSTED | REQ-07 |
| ACC-14 | digest of nil-policy validation equals digest of explicit `DefaultValidationPolicy()`; nil `ResourceStates[host]` entry | equal; no panic | REQ-02/08 |
| ACC-15 | docs | KG-1..KG-5 substrings still present; KG-1 text updated | REQ-09 |

Substrate test ACC-10 group (`tests/m3c_substrate_test.go` ~2139-2216): subtest (b) `ACC-10_DefaultPolicy_AcceptsUnknownMetrics_KG1` MUST be inverted to assert invalid with UNKNOWN_RESOURCE_STATE and gain an explicit-opt-out variant (flags false ⇒ valid). Subtest (e) `..._AcceptsBudgetStatusUnknown_KG1` uses the `local_compute` pool `pool-local` and MUST remain valid (REQ-07 exemption); rename it off the `_KG1` acceptance-of-gap naming. ADD subtests in that group using a portfolio whose used pool is `metered_api` or `subscription_quota`: unknown ⇒ fail-closed (observed "unknown"), absent ⇒ fail-closed (observed "missing"), plus explicit opt-out. Subtests (c)/(d) (nil maps) are unchanged. Doc-assertion ACC-14 of the substrate test is unaffected as long as `KG-1`..`KG-5` substrings remain.

## Validation

- `go build ./... && go vet ./... && gofmt -l .` clean
- `go test ./internal/cognition/... ./tests/... -count=1`; full `go test ./...`
- `make hooks-check`; hooks installed, no bypass
- mutation checks (implementer records): revert REQ-02 line ⇒ ACC-01 fails; drop local_compute exemption ⇒ ACC-05 fails; drop the `st == nil` branch ⇒ ACC-04/11 fail
- required independent review lenses: correctness/contract, test-adequacy

## Escalation triggers

Any need for protocol/schema changes, a second new policy field, or conflict between ACC-14 doc assertions and docs wording ⇒ stop and return.

## Design / rationale

Alternative considered: treat nil `BudgetStates` as unknown too. Rejected for this WP: many callers legitimately validate structure without live observation, and the change would alter semantics of "no observation" (needs owner decision; recorded as residual). Alternative: tie to budget pool `Status` only; rejected because missing entries in a supplied map are equally unknown.

## Implementation Readiness Report

```text
requirements represented: 9/9
mandatory clauses resolved: 4/4
state transitions specified: n/a (pure validation)
failure cases specified: 3/3
authority decisions specified: 1/1
missing/unknown input semantics: 3/3
acceptance scenarios mapped: 15/15
unresolved architecture choices: 0
declared local-discretion choices: 4
readiness: READY_FOR_IMPLEMENTATION (independent review: READY_WITH_FIXES, fixes applied)
```
