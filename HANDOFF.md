# Handoff — feat/m3c-cognition-substrate

Last updated: 2026-09-30T03:15:00Z by Principal Engineer Session

PR #16 Base: `58869d99635ee0d05b5fe30e3b152dacddc12445`
PR #16 Head: `feat/m3c-cognition-substrate`
Active PR: https://github.com/olostan/DevCadence/pull/16

## Milestone
M3C — Cognition Resource and Session Substrate
See docs/WORK_PACKAGES.md#m3c--cognition-resource-and-session-substrate for the full Work Package breakdown.

## Work Package status

| WP | Status | Checkpoint | Validation | Review |
|----|--------|------------|------------|--------|
| WP-M3C-1 | review repairs completed | PR #16 repair commit pending | `go test -count=1 ./...` PASS | Round-1, Round-2, and portfolio/workflow review findings addressed |
| WP-M3C-2 | not started | — | — | blocked on WP-M3C-1 acceptance |
| WP-M3C-3 | not started | — | — | blocked on WP-M3C-1, WP-M3C-2 |
| WP-M3C-4 | not started | — | — | blocked on WP-M3C-1..3 |

## Currently in progress: WP-M3C-1

- **EWP status:** Amended and verified at `docs/work-packages/wp-m3c-1-ewp.md`.
- **Base commit this WP started from:** `58869d99635ee0d05b5fe30e3b152dacddc12445`
- **What's implemented & repaired:**
  1. Go protocol types and validation in `internal/protocol/`:
     - `access_channel.go` (`AccessChannel`, `ChannelKind`, `SessionMode`, `ContextControl`, `PrefixCache`)
     - `context.go` (`ContextProfile`, `ContextManifest`, `ContextPack`, `EvidenceLease`, `WorkloadEnvelope`, `TokenAccountingBreakdown`, `CognitiveStateCapsule`, `EphemeralTailBlock`). Added PROTOCOLS §10B fields (`Runtime`, `ModelRef`, `Quantization`, `ContextConfiguration`, `MappingVersion`, `SourceRevision`, `AdmissionProvenance`, `AdmittedObjectDigests`, `CoverageSummary`). Enforced verbatim content-addressing check in `EvidenceLease.Validate()` (SHA-256 match).
     - `refactoring_proposal.go` (`RefactoringProposal`, `ReversibilityClass`, `ProposalStatus`, `ProposalAdjudication`). Enforced fail-closed lifecycle: `proposed` forbids adjudication, terminal requires adjudication, `accepted` requires non-empty `ResultingWorkPackageID`.
     - `economics.go` (`EconomicRegime`, `BudgetPool`, `BudgetState`, `ResourceState`, `BudgetUnit`, `BudgetPeriod`). Enforced allowlist guard on `FallbackAllowedToMetered` (only `metered_api`) and `AllowOverage` prohibition on subscription/local/custom regimes. Added pointers for honest unknown/unobserved metrics on `BudgetState` and `ResourceState` (`Record` implementations, `NewRecord` registration).
     - `portfolio.go` (`CognitionPortfolio`, `RoleBinding`, `PortfolioRecommendation`, `WorkflowPlan`, `WorkflowTopologyKind`, `WorkflowStage`). Added FR-062 fields (`Priority`, `FallbackEndpointIDs`, `ExcludedEndpointIDs`, `BudgetReservations`), referential integrity checks against channels and pools, DAG forward-only non-self dependency checks, and topology compatibility rules.
     - Registered all 12 record kinds in `internal/protocol/protocol.go` (`NewRecord`).
  2. Draft 2020-12 JSON Schemas under `schemas/` with `additionalProperties: false`:
     - `access-channel.schema.json`
     - `context-profile.schema.json`
     - `context-manifest.schema.json`
     - `context-pack.schema.json`
     - `evidence-lease.schema.json`
     - `refactoring-proposal.schema.json` (with conditional `allOf` fail-closed adjudication constraints)
     - `budget-pool.schema.json` (with conditional `allOf` allowlist and overage constraints)
     - `budget-state.schema.json` (published)
     - `resource-state.schema.json` (published)
     - `cognition-portfolio.schema.json`
     - `portfolio-recommendation.schema.json`
     - `workflow-plan.schema.json`
  3. Registered schema names and `RecordKindToSchema` mappings in `internal/schema/schema.go`, documented in `schemas/README.md`.
  4. Tested 100% top-level field parity across all 35 schemas in `tests/twin_fields_test.go` (`TestSchemaTopLevelFieldsMatchTheGoTwin`).
  5. Authored valid and invalid test fixtures under `fixtures/protocol/`, wired into `tests/schema_fixtures_test.go` round-trip and negative reader parity tests (`TestTheGoReaderRejectsWhatTheSchemaRejects`).
   6. Added comprehensive domain unit tests in `internal/protocol/` (`access_channel_test.go`, `context_test.go`, `refactoring_proposal_test.go`, `economics_test.go`, `portfolio_test.go`).
   7. Addressed PR #16 Round 2 review comments:
      - Validated `BudgetState.UnknownFields` and `ResourceState.UnknownMetrics` against allowed property names, enforced mutual exclusivity against populated pointer fields, and added consistency rules (`status: exhausted` requires zero/nil balance; `period_end` >= `period_start`). Documented observation versioning conventions in `Tx.PutRecord` and `soft_limit_exceeded`.
      - Enforced SHA-256 digest validation and non-empty key checks for `ContextPack.AdmittedObjectDigests`, validated non-empty `admission_provenance` on `ContextManifest`, and enforced non-empty `calibration_evidence_ref` on `WorkloadEnvelope`.
   8. Addressed PR #16 Round 3 review comments:
      - Enforced channel resolution and pool coverage for `RoleBinding.FallbackEndpointIDs`, prohibiting fallback to metered endpoints from pools that do not permit it (ADR-0018 §9, DCI-104).
      - Enforced role priority uniqueness in `CognitionPortfolio.Validate()`, preventing priority ties across role bindings.
      - Enforced `BudgetReservations` upper bound against `pool.HardLimit`.
      - Introduced explicit `StageKind` (`"cognition" | "deterministic"`) and `is_review: bool` on `WorkflowStage` (and `schemas/workflow-plan.schema.json`), replacing all free-text role substring heuristics for topology validation.
      - Documented in EWP that `ContextPack` resident ceiling check against `ContextProfile` belongs to the Context Compiler / Session Driver (WP-M3C-2/3).
      - Corrected `HANDOFF.md` Base label and refreshed Head reference.
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
  - `TestTheGoReaderRejectsWhatTheSchemaRejects` PASS (including all M3C schemas)
  - ADR-0018 §9 & DCI-104 no-silent-paid-fallback policy strictly enforced and verified.
- **What's left for this WP:**
  - Merge PR #16 when reviewed and approved.
- **Known blockers / open questions:** None.

## Context and evidence capsule

- **Contract:** WP-M3C-1 v1.2, `docs/work-packages/wp-m3c-1-ewp.md`
- **Context Manifest:** `docs/work-packages/wp-m3c-1-ewp.md §1`
- **Derived state:** Round 2 reviews resolved; all PR #16 review findings resolved and deterministically verified.
- **Evidence:** Clean test runs across all packages, base commit `58869d9`.
- **Coverage:** 100% of WP-M3C-1 scope card deliverables implemented, repaired, and verified.
- **Expansion needed:** None.
