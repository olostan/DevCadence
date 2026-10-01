# Handoff — feat/m3c-cognition-substrate

Last updated: 2026-09-30T18:20:00Z by Frontier Principal Engineer Session

PR #16 Base: `58869d99635ee0d05b5fe30e3b152dacddc12445`
PR #16 Head: `c5e962f` (commit c5e962f; branch `feat/m3c-cognition-substrate`)
Active PR: https://github.com/olostan/DevCadence/pull/16

## Milestone
M3C — Cognition Resource and Session Substrate
See docs/WORK_PACKAGES.md#m3c--cognition-resource-and-session-substrate for the full Work Package breakdown.

## Work Package status

| WP | Status | Checkpoint | Validation | Review |
|----|--------|------------|------------|--------|
| WP-M3C-1 | ready for acceptance | PR #16 review complete & all blockers resolved | `go test -count=1 ./...` PASS | All 5 review blockers and cleanup items resolved & verified |
| WP-M3C-2 | not started | — | — | blocked on WP-M3C-1 acceptance |
| WP-M3C-3 | not started | — | — | blocked on WP-M3C-1, WP-M3C-2 |
| WP-M3C-4 | not started | — | — | blocked on WP-M3C-1..3 |

## Currently in progress: WP-M3C-1

- **EWP status:** Authoritative v1.3 synchronized and verified at `docs/work-packages/wp-m3c-1-ewp.md`.
- **Base commit this WP started from:** `58869d99635ee0d05b5fe30e3b152dacddc12445`
- **What's implemented & repaired:**
  1. Go protocol types and validation in `internal/protocol/`:
     - `access_channel.go` (`AccessChannel`, `ChannelKind`, `SessionMode`, `ContextControl`, `PrefixCache`).
     - `context.go` (`ContextProfile`, `ContextManifest`, `ContextPack`, `EvidenceLease`, `WorkloadEnvelope`, `TokenAccountingBreakdown`, `CognitiveStateCapsule`, `EphemeralTailBlock`). Added PROTOCOLS §10B fields (`Runtime`, `ModelRef`, `Quantization`, `ContextConfiguration`, `MappingVersion`, `SourceRevision`, `AdmissionProvenance`, `AdmittedObjectDigests`, `CoverageSummary`). Enforced verbatim content-addressing check in `EvidenceLease.Validate()` (SHA-256 match). Enforced `CalibrationEvidenceRef` requirement on `WorkloadEnvelope` when `confidence_level == "verified"`. Parsed RFC3339 timestamps for `EvidenceLease.ExpiresAt` comparison against `AcquiredAt`.
     - `refactoring_proposal.go` (`RefactoringProposal`, `ReversibilityClass`, `ProposalStatus`, `ProposalAdjudication`). Enforced fail-closed lifecycle: `proposed` forbids adjudication, terminal requires adjudication, `accepted` requires non-empty `ResultingWorkPackageID`.
     - `economics.go` (`EconomicRegime`, `BudgetPool`, `BudgetState`, `ResourceState`, `BudgetUnit`, `BudgetPeriod`, `BudgetPoolStatus`). Added `BudgetStatusUnknown = "unknown"` and allowed `status` in `BudgetState.UnknownFields` when status is unknown. Enforced allowlist guard on `FallbackAllowedToMetered` (only `metered_api`) and `AllowOverage` prohibition on subscription/local/custom regimes. Added pointers for honest unknown/unobserved metrics on `BudgetState` and `ResourceState`.
     - `portfolio.go` (`CognitionPortfolio`, `RoleBinding`, `FallbackBinding`, `DiversityPolicy`, `EscalationRule`, `WorkflowDefaults`, `PortfolioRecommendation`, `WorkflowPlan`, `WorkflowTopologyKind`, `WorkflowStage`, `StageKind`). Added explicit, routable `FallbackBinding` (`EndpointID`, `ChannelID`, `BudgetPoolID`, `ContextProfileID`) replacing unstructured strings; added explicit `DiversityRequirements`, `EscalationRules`, and `WorkflowDefaults`; added stage execution contracts (`Kind`, `IsReview`, `EndpointID`, `ChannelID`, `ContextProfileID`, `RetryLimit`, `EscalationTarget`, `DeterministicGateID`) with deterministic gate requirements and enforced independence across review stages for `dual_independent_review`.
     - Registered all 12 record kinds in `internal/protocol/protocol.go` (`NewRecord`).
  2. Draft 2020-12 JSON Schemas under `schemas/` with `additionalProperties: false`:
     - `access-channel.schema.json`
     - `context-profile.schema.json` (with conditional requirement for `calibration_evidence_ref` on verified envelopes)
     - `context-manifest.schema.json`
     - `context-pack.schema.json`
     - `evidence-lease.schema.json`
     - `refactoring-proposal.schema.json` (with conditional `allOf` fail-closed adjudication constraints)
     - `budget-pool.schema.json` (with conditional `allOf` allowlist and overage constraints)
     - `budget-state.schema.json` (with `unknown` status and unknown fields support)
     - `resource-state.schema.json`
     - `cognition-portfolio.schema.json` (using `$ref` to `access-channel.schema.json` and `budget-pool.schema.json`, plus typed fallbacks, diversity requirements, escalation rules, and workflow defaults)
     - `portfolio-recommendation.schema.json`
     - `workflow-plan.schema.json` (with stage execution properties and conditional gate requirements)
  3. Registered schema names and `RecordKindToSchema` mappings in `internal/schema/schema.go`, documented in `schemas/README.md`.
  4. Tested 100% top-level field parity across all 35 schemas in `tests/twin_fields_test.go` (`TestSchemaTopLevelFieldsMatchTheGoTwin`).
  5. Authored valid and invalid test fixtures under `fixtures/protocol/`, wired into `tests/schema_fixtures_test.go` round-trip and negative reader parity tests (`TestTheGoReaderRejectsWhatTheSchemaRejects`), including `cognition-portfolio.invalid-nested-pool.json`.
  6. Added comprehensive domain unit tests in `internal/protocol/` (`access_channel_test.go`, `context_test.go`, `refactoring_proposal_test.go`, `economics_test.go`, `portfolio_test.go`).
  7. Addressed all review findings from Review 5370015868:
     - Blocker 1: Added complete FR-062 / COGNITION_PORTFOLIO / PROTOCOLS contracts to `CognitionPortfolio` (`DiversityRequirements`, `EscalationRules`, `WorkflowDefaults`) and `WorkflowStage` (`EndpointID`, `ChannelID`, `ContextProfileID`, `RetryLimit`, `EscalationTarget`, `DeterministicGateID`), with real independence validation in `dual_independent_review`.
     - Blocker 2: Introduced typed, routable `FallbackBinding` on `RoleBinding.Fallbacks` with explicit channel, budget pool, and context profile references and metered fallback policy enforcement.
     - Blocker 3: Reused canonical `$ref` to `devcadence:///budget-pool.schema.json` and `devcadence:///access-channel.schema.json` in `schemas/cognition-portfolio.schema.json`, adding fixture `cognition-portfolio.invalid-nested-pool.json` to verify rejection of invalid nested budget pool configurations.
     - Blocker 4: Added `unknown` to `BudgetPoolStatus`, updated `schemas/budget-state.schema.json`, and permitted `status` in `unknown_fields` when status is unknown.
     - Blocker 5: Reconciled `docs/work-packages/wp-m3c-1-ewp.md` Go twin sketches and context manifest with final v1 types and fields.
     - Cleanups: Enforced non-empty `calibration_evidence_ref` on `confidence_level: "verified"`; used RFC3339 parsed instant comparisons for `EvidenceLease`; synchronized version numbers to v1.3.
- **What's verified:**
  - `go build ./...` clean (exit 0)
  - `go vet ./...` clean (exit 0)
  - `go test -count=1 ./...` across all packages clean (exit 0)
  - `TestSchemaTopLevelFieldsMatchTheGoTwin` PASS (35/35 schemas)
  - `TestEveryRecordKindHasASchema` PASS
  - `TestEverySchemaCompiles` PASS
  - `TestValidFixturesValidate` PASS
  - `TestInvalidFixturesAreRejected` PASS
  - `TestFixturesRoundTripWithoutSemanticLoss` PASS
  - `TestTheGoReaderRejectsWhatTheSchemaRejects` PASS (including all M3C schemas and nested pool fixture)
  - ADR-0018 §9 & DCI-104 no-silent-paid-fallback policy strictly enforced and verified.
- **What's left for this WP:**
  - Merge PR #16 upon reviewer signoff.
- **Known blockers / open questions:** None.

## Context and evidence capsule

- **Contract:** WP-M3C-1 v1.3, `docs/work-packages/wp-m3c-1-ewp.md`
- **Context Manifest:** `docs/work-packages/wp-m3c-1-ewp.md §1` (Revision 3)
- **Derived state:** All 5 review blockers and cleanup items from review 5370015868 resolved and deterministically verified.
- **Evidence:** Clean test runs across all packages, base commit `58869d9`.
- **Coverage:** 100% of WP-M3C-1 scope card deliverables implemented, repaired, and verified.
- **Expansion needed:** None.
