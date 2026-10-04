# WP-M3D-3 — Portfolio Adaptation, Versioning & Atomic Rollback

## Identity

- Work Package ID: WP-M3D-3 (window 2026-10-C)
- Revision: 2 (window review: composite role:priority keys in diffing, zero-base bootstrap semantics, explicit proposal binding in ProposeAndActivate)
- Task ID: autonomous-run-1
- Base commit: `a743144af6f4a1c8051628d65766afb331038a4e`
- Project state revision: 1
- Contract digest: n/a (Markdown contract authoritative)
- Target implementation endpoint/profile: competent Go implementer
- Status: READY_FOR_IMPLEMENTATION (r2)

## Objective

Provide typed, auditable portfolio adaptation mechanics in `internal/cognition`:
1. Calculate semantic deltas (`PortfolioDiff`) comparing an existing portfolio with a proposed candidate across access channels, role bindings, budget pools, source exposure, and policies.
2. Formulate explicit, typed `PortfolioChangeProposal` records explaining why a change is proposed (trigger, rationale, diff).
3. Connect proposal evaluation into `ActivationManager` for atomic activation (`active-portfolio.json`), historical lineage tracking, and deterministic rollback to prior active configurations.
4. Guarantee that portfolio adaptation never silently mutates user policies or spending limits (ADR-0018 §11, DCI-124).

## Context Manifest

- role: implementer (Go), independent reviewers
- read-authority envelope: `internal/protocol/portfolio.go` (CognitionPortfolio, AccessChannel, RoleBinding, BudgetPool), `internal/cognition/portfolio_activation.go`, `internal/cognition/portfolio_validator.go`
- semantic write/scope envelope: `internal/cognition/portfolio_diff.go` (new), `internal/cognition/portfolio_diff_test.go` (new), `internal/cognition/portfolio_adaptation.go` (new), `internal/cognition/portfolio_adaptation_test.go` (new), docs
- risk tags: portfolio state management, atomic rollback, lineage durability, no silent policy mutation
- exact normative clauses: ADR-0018 §11 (explicit and reversible adaptation), DCI-124 (no silent authority expansion), DCI-123 (deterministic authorization)
- initial evidence handles: `internal/cognition/portfolio_activation.go`
- deferred references: none
- assumptions: A1 `ActivationManager` manages filesystem persistence and recovery. A2 Diffing is pure and non-mutating.
- re-resolution triggers: need to change canonical JSON schemas.

## Semantic scope envelope

### Authorized domains / path patterns

- `internal/cognition/portfolio_diff.go`
- `internal/cognition/portfolio_diff_test.go`
- `internal/cognition/portfolio_adaptation.go`
- `internal/cognition/portfolio_adaptation_test.go`
- `docs/COGNITION_PORTFOLIO.md`, `docs/WORK_PACKAGES.md`

### Explicitly forbidden semantic changes

- No bypassing of `ActivationManager` or direct unsynced file writes to `active-portfolio.json`.
- No automatic activation without caller validation.
- No silent modification of `ValidationPolicy`.

### LOCAL_DISCRETION

- Internal diff helper functions and formatting methods.
- Test scenarios and edge case coverage.

## Requirements

| ID | Strength | Requirement | Normative source |
| --- | --- | --- | --- |
| REQ-01 | MUST | `PortfolioDiff` API: `type DeltaKind string` (`DeltaAdded="added"`, `DeltaRemoved="removed"`, `DeltaModified="modified"`); `type FieldChange struct { Field string; OldValue string; NewValue string }`; `type ItemDiff struct { ID string; Kind string; Delta DeltaKind; Details []FieldChange }`; `type PortfolioDiff struct { FromPortfolioID string; ToPortfolioID string; FromRevision int; ToRevision int; ChannelDiffs []ItemDiff; RoleBindingDiffs []ItemDiff; BudgetPoolDiffs []ItemDiff; PolicyChanges []FieldChange; HasChanges bool }`; `func DiffPortfolios(base, candidate *protocol.CognitionPortfolio) (*PortfolioDiff, error)`. | ADR-0018 §11 |
| REQ-02 | MUST | Stop rules and Bootstrap Semantics: If `candidate == nil`, return `errs.CategoryInvalidArgument`. If `base == nil` (initial bootstrap on fresh repo), `DiffPortfolios` succeeds and treats all candidate items as `DeltaAdded`, with `FromPortfolioID: ""`, `FromRevision: 0`, `ToPortfolioID: candidate.PortfolioID`, `ToRevision: candidate.Revision`, and `HasChanges: true`. | DCI-104 |
| REQ-03 | MUST | Semantic Diffing & Disambiguation: Detect added, removed, and modified items by their natural keys: `ChannelID` for channels, composite `fmt.Sprintf("%s#%d", rb.Role, rb.Priority)` for role bindings (preventing collisions between distinct priorities for the same role), and `PoolID` for budget pools. Changes in endpoints, fallback bindings, regimes, or limits are recorded in `Details`. Diffs are sorted deterministically by key. | ADR-0018 §11 |
| REQ-04 | MUST | `PortfolioChangeProposal` API: `type ChangeTrigger string` (`TriggerResourceChange="resource_change"`, `TriggerPolicyUpdate="policy_update"`, `TriggerManualProposal="manual_proposal"`, `TriggerEvaluatedOutcome="evaluated_outcome"`); `type PortfolioChangeProposal struct { ProposalID string; Trigger ChangeTrigger; Rationale string; BasePortfolioID string; CandidatePortfolio protocol.CognitionPortfolio; Diff PortfolioDiff; ProposedAt string }`; `func CreateChangeProposal(trigger ChangeTrigger, rationale string, base *protocol.CognitionPortfolio, candidate protocol.CognitionPortfolio, now time.Time) (*PortfolioChangeProposal, error)`. | ADR-0018 §11 |
| REQ-05 | MUST | Proposal Validation & Non-Empty Rationale: `CreateChangeProposal` fails if `strings.TrimSpace(rationale) == ""` or `!trigger.Valid()` or `candidate.Validate() != nil`. | DCI-124 |
| REQ-06 | MUST | Adaptation Service API: `type AdaptationService struct { mgr *ActivationManager }`; `func NewAdaptationService(mgr *ActivationManager) *AdaptationService`; `func (s *AdaptationService) ProposeAndActivate(ctx context.Context, proposal *PortfolioChangeProposal, valInput ValidationInput) (*ActivationRecord, error)`; `func (s *AdaptationService) Rollback(ctx context.Context, targetActivationID string, revalInput *ValidationInput) (*ActivationRecord, error)`. | ADR-0018 §11 |
| REQ-07 | MUST | Atomic Activation via Lineage: `ProposeAndActivate` enforces candidate binding: Go forces `valInput.Portfolio = &proposal.CandidatePortfolio`. It verifies that `proposal.BasePortfolioID` matches current active portfolio ID (if active exists), and invokes `mgr.Activate(ctx, valInput)` atomically. | ADR-0018 §11 |
| REQ-08 | MUST | Rollback Guarantees: `Rollback` requires non-nil `revalInput`, re-validates the historical portfolio against current environment/policy, and atomically activates the target record with `IsRollback: true`. | ADR-0018 §11 |

## Invariants / state rules

| ID | Statement | Related requirements |
| --- | --- | --- |
| INV-01 | Diffs are deterministic, pure, and sorted by identifier. | REQ-01, REQ-03 |
| INV-02 | Every portfolio transition records an explicit trigger, non-blank rationale, and verifiable diff. | REQ-04, REQ-05 |
| INV-03 | Rollback re-validates the target portfolio against current environment facts before activation. | REQ-08 |
| INV-04 | Activation and rollback are atomic and crash-consistent via `ActivationManager`. | REQ-06, REQ-07 |

## Interface / algorithm contract

```text
DiffPortfolios(base, candidate):
  1. Validate candidate != nil. If base == nil: return bootstrapDiff(candidate).
  2. Compare channels (key: ChannelID). Record Added, Removed, Modified.
  3. Compare role bindings (key: role#priority). Record Added, Removed, Modified.
  4. Compare budget pools (key: PoolID). Record Added, Removed, Modified.
  5. Compare MaxSourceExposure, ExcludedEndpointIDs, DiversityRequirements.
  6. Return sorted PortfolioDiff with HasChanges = (len(all diffs) > 0).

ProposeAndActivate(ctx, proposal, valInput):
  1. Validate proposal != nil.
  2. Enforce candidate binding: valInput.Portfolio = &proposal.CandidatePortfolio.
  3. If active lineage exists: verify proposal.BasePortfolioID == lineage.CurrentPortfolioID.
     (Prevent stale race transitions; return errs.CategoryConflict on mismatch).
  4. Invoke mgr.Activate(ctx, valInput). Return ActivationRecord.

Rollback(ctx, targetActivationID, revalInput):
  1. If targetActivationID == "": return mgr.RollbackToPrevious(ctx, revalInput)
  2. Else: return mgr.RollbackToActivation(ctx, targetActivationID, revalInput)
```

## Authority matrix

| Decision/effect | Authorized source | Forbidden substitute |
| --- | --- | --- |
| Portfolio activation | Deterministic Validator + `ActivationManager` | Direct file copy to `active-portfolio.json` |
| Policy changes | Explicit user configuration | AI planner inferred policy drift |
| Rollback admissibility | Deterministic Revalidation | Blind restoration of stale historical state |

## Missing / unknown / stale input semantics

| Input/fact | Missing | Unknown | Stale | Malformed/contradictory |
| --- | --- | --- | --- | --- |
| Base portfolio for diff | Bootstrap (all added) | n/a | n/a | Return `InvalidArgument` |
| Proposal rationale | Return `InvalidArgument` | n/a | n/a | Return `InvalidArgument` |
| Target rollback ID | Defaults to immediate previous | Fail closed (`NotFound`) | Fail closed | Fail closed |
| Environment on rollback | Re-validation fails closed | Re-validation fails closed | Fails validation | Fails validation |

## Failure matrix

| Failure point / condition | Required postcondition | Recovery / response | Evidence |
| --- | --- | --- | --- |
| Base portfolio ID mismatch | Current active portfolio untouched | Return `CategoryConflict` | Error returned |
| Candidate fails validation | Current active portfolio untouched | Return validation error | Error returned |
| Rollback target no longer viable on current hardware | Active portfolio untouched | Return validation error | Error returned |
| Process crash mid-activation | Clean startup recovery on next run | `ActivationManager.recoverStartupLocked` cleans pending | Lineage integrity verified |

## Representability map

| Concept / requirement | Exact representation | Adequacy | Action if inadequate |
| --- | --- | --- | --- |
| Portfolio Delta | `cognition.PortfolioDiff` | represented | — |
| Change Proposal | `cognition.PortfolioChangeProposal` | represented | — |
| Adaptation Service | `cognition.AdaptationService` | represented | — |
| Activation Result | `cognition.ActivationRecord` | represented | — |

## Acceptance scenarios

| ID | Setup / precondition | Action | Expected result | Maps to |
| --- | --- | --- | --- | --- |
| ACC-01 | Two distinct portfolios (added channel, changed role endpoint) | `DiffPortfolios(p1, p2)` | `HasChanges: true`, accurate item diffs, sorted deterministically | REQ-01, REQ-03 |
| ACC-02 | Identical portfolios | `DiffPortfolios(p1, p1)` | `HasChanges: false`, empty diff slices | REQ-01 |
| ACC-03 | Valid proposal and validation input | `ProposeAndActivate` | New activation record created, lineage updated, `active-portfolio.json` updated | REQ-06, REQ-07 |
| ACC-04 | Proposal referencing obsolete base portfolio ID | `ProposeAndActivate` | Returns `CategoryConflict`, activation rejected | REQ-07, INV-04 |
| ACC-05 | Active portfolio running, previous exists | `Rollback("", reval)` | Restores previous activation, marks `IsRollback: true`, lineage updated | REQ-08, INV-03 |
| ACC-06 | Rollback target references endpoint missing in current inventory | `Rollback(targetID, reval)` | Revalidation fails, rollback aborted, current active untouched | REQ-08, INV-03 |
| ACC-07 | Blank rationale in proposal | `CreateChangeProposal` | Returns `CategoryInvalidArgument` | REQ-05 |
| ACC-08 | Initial bootstrap with `base == nil` | `DiffPortfolios(nil, p1)` | `HasChanges: true`, all items `DeltaAdded`, `FromPortfolioID: ""` | REQ-02 |

## Validation

- command / deterministic check: `go test -v -race ./internal/cognition/... -run "TestPortfolio(Diff|Adaptation)"`
- mutation testing: `mutation review sufficient: adversarial catalog below`
- mutation catalog:

| Mutant (Plausible Bug / Omission) | Expected Test Failure (Scenario / Check) |
| --- | --- |
| Ignore base portfolio ID mismatch in `ProposeAndActivate` | ACC-04 fails |
| Skip re-validation during `Rollback` | ACC-06 fails |
| Allow empty or whitespace-only proposal rationale | ACC-07 fails |
| Fail `DiffPortfolios` when `base == nil` | ACC-08 fails |
| Collide role bindings with same role but different priorities | Multi-priority diff test fails |
| Invert diff additions and removals | ACC-01 fails |

- required independent review lenses:
  - Contract & Authority Reviewer: verifies requirements REQ-*, invariants INV-*, boundaries, and fail-closed security.
  - Test Adequacy & Mutation Reviewer: verifies coverage of ACC-*, checks edge cases, and kills all cataloged mutants.
- evidence to capture: test logs, coverage report, git diff.

## Escalation triggers

- File lock conflicts on activation storage.
- Schema conflicts with `CognitionPortfolio`.

## Implementation Readiness Report

```text
requirements represented: 8/8
mandatory clauses resolved: 3/3
state transitions specified: 3/3
failure cases specified: 4/4
authority decisions specified: 3/3
missing/unknown input semantics: 4/4
acceptance scenarios mapped: 8/8
unresolved architecture choices: 0
declared local-discretion choices: 2
readiness: READY_FOR_IMPLEMENTATION
```

### Weaker-implementer check

Could a competent implementation model with good language/repository skill but mediocre architectural judgment execute this EWP without inventing important semantics?

- [x] Yes
- [ ] No — return to Principal design
