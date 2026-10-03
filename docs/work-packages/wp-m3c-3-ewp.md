# Engineering Work Package: WP-M3C-3 — Deterministic Portfolio Validator and Activation

- **Milestone:** M3C — Cognition Resource and Session Substrate
- **Scope card:** [docs/WORK_PACKAGES.md#wp-m3c-3--deterministic-portfolio-validator-and-activation](../WORK_PACKAGES.md#wp-m3c-3--deterministic-portfolio-validator-and-activation)
- **Base commit:** `1debfeb0366db024fb250482623366d6a754b308` (origin/main, merge of PR #22 — Project Health Contracts)
- **Branch:** `feat/m3c-3-portfolio-validator`
- **Task ID:** `task-m3c-3-portfolio-validator-and-activation`
- **Work Package ID:** `WP-M3C-3`
- **Version:** 1.0
- **Status:** Approved for Implementation

---

## 1. Context Manifest (AGENTS.md §2, docs/PROTOCOLS.md §10B)

```json
{
  "manifest_id": "manifest-wp-m3c-3-v1",
  "task_id": "task-m3c-3-portfolio-validator-and-activation",
  "work_package_id": "WP-M3C-3",
  "work_package_revision": 1,
  "role": "principal_engineer",
  "base_commit": "1debfeb0366db024fb250482623366d6a754b308",
  "project_state_revision": "bootstrap-m3c-2b-closed",
  "read_envelope": [
    "AGENTS.md",
    "INVARIANTS.md",
    "docs/README.md",
    "docs/WORK_PACKAGES.md",
    "docs/PROTOCOLS.md",
    "docs/IMPLEMENTATION_PLAN.md",
    "docs/COGNITION_PORTFOLIO.md",
    "docs/adr/0018-adaptive-cognition-portfolio-and-workflow-synthesis.md",
    "docs/adr/0019-non-conversational-cognition-and-adaptive-review.md",
    "docs/adr/0020-cognitive-invocation-compiler-and-review-ledger.md",
    "docs/work-packages/wp-m3c-1-ewp.md",
    "docs/work-packages/wp-m3c-2a-ewp.md",
    "docs/work-packages/wp-m3c-2b-ewp.md",
    "internal/errs/errs.go",
    "internal/protocol/access_channel.go",
    "internal/protocol/cognition.go",
    "internal/protocol/context.go",
    "internal/protocol/economics.go",
    "internal/protocol/portfolio.go",
    "internal/protocol/resource_inventory.go"
  ],
  "write_scope": [
    "docs/work-packages/wp-m3c-3-ewp.md",
    "docs/WORK_PACKAGES.md",
    "docs/IMPLEMENTATION_PLAN.md",
    "internal/cognition/portfolio_diagnostics.go",
    "internal/cognition/portfolio_validator.go",
    "internal/cognition/portfolio_validator_endpoints.go",
    "internal/cognition/portfolio_validator_economics.go",
    "internal/cognition/portfolio_validator_constraints.go",
    "internal/cognition/portfolio_activation.go",
    "internal/cognition/portfolio_validator_test.go",
    "internal/cognition/portfolio_activation_test.go"
  ],
  "domains": [
    "cognition_portfolio",
    "deterministic_validation",
    "capability_provenance",
    "source_exposure_policy",
    "economic_regimes",
    "silent_metered_fallback",
    "atomic_activation",
    "rollback_primitives"
  ],
  "risk_tags": [
    "silent_spending_expansion",
    "unauthorized_source_exposure",
    "missing_capability_blindness",
    "stale_validation_activation",
    "partial_activation_state"
  ],
  "mandatory_clauses": [
    {
      "clause_id": "DCI-005",
      "source_doc": "INVARIANTS.md",
      "summary": "Provenanced claims: facts require verifiable evidence; unknown capability must not be assumed supported."
    },
    {
      "clause_id": "DCI-080",
      "source_doc": "INVARIANTS.md",
      "summary": "Least privilege: source exposure must never exceed project policy limits."
    },
    {
      "clause_id": "DCI-104",
      "source_doc": "INVARIANTS.md",
      "summary": "Capability absence degrades; unhealthy or unready endpoints are rejected outright from routing."
    },
    {
      "clause_id": "DCI-106",
      "source_doc": "INVARIANTS.md",
      "summary": "Acceleration is verified, not inferred: unverified backends cannot satisfy verified acceleration demands."
    },
    {
      "clause_id": "DCI-120",
      "source_doc": "INVARIANTS.md",
      "summary": "Roles are independent of providers and access channels: capability requirements, not vendor aliases."
    },
    {
      "clause_id": "DCI-121",
      "source_doc": "INVARIANTS.md",
      "summary": "Economics belong to access paths: regimes, budget pools, and overage attach to access channels."
    },
    {
      "clause_id": "DCI-122",
      "source_doc": "INVARIANTS.md",
      "summary": "No silent metered fallback: subscription or local usage never converts to metered API spending without explicit policy."
    },
    {
      "clause_id": "DCI-123",
      "source_doc": "INVARIANTS.md",
      "summary": "AI recommends; deterministic policy authorizes: validator is an unbypassable gate."
    },
    {
      "clause_id": "DCI-124",
      "source_doc": "INVARIANTS.md",
      "summary": "Recommendation cannot expand authority: planner cannot weaken source exposure or spending policy."
    },
    {
      "clause_id": "DCI-126",
      "source_doc": "INVARIANTS.md",
      "summary": "Scarce quota remains scarce when marginal dollars are zero: quota exhaustion enforces overage policy."
    },
    {
      "clause_id": "DCI-128",
      "source_doc": "INVARIANTS.md",
      "summary": "Portfolio adaptation is explicit and reversible: activation is atomic, versioned, and supports clean rollback."
    }
  ]
}
```

---

## 2. Authoritative Execution Contract

### Objective
Implement the deterministic control-plane gate that validates candidate cognition portfolios across 8 required dimensions and supports crash-safe, versioned atomic activation and rollback.

### Allowed Paths
- `docs/work-packages/wp-m3c-3-ewp.md`
- `docs/WORK_PACKAGES.md`
- `docs/IMPLEMENTATION_PLAN.md`
- `internal/cognition/portfolio_diagnostics.go`
- `internal/cognition/portfolio_validator.go`
- `internal/cognition/portfolio_validator_endpoints.go`
- `internal/cognition/portfolio_validator_economics.go`
- `internal/cognition/portfolio_validator_constraints.go`
- `internal/cognition/portfolio_activation.go`
- `internal/cognition/portfolio_validator_test.go`
- `internal/cognition/portfolio_activation_test.go`

### Requirements
1. **Deterministic Portfolio Validator:**
   - Evaluates 8 required dimensions:
     1. Endpoint existence (channels, bindings, fallbacks, budget pools, context profiles in inventory/state).
     2. Capability compatibility (usable health, valid auth, tool support, structured output, capability grades).
     3. Capability provenance (explicit non-unknown provenance; enforcement of measured/evaluated when required).
     4. Source-exposure / privacy policy (fails closed if required exposure exceeds portfolio or project policy).
     5. Economic & budget bindings (authorized regimes, spend caps, overage rules, DCI-122 no silent metered fallback, live budget state).
     6. Context compatibility (channel context control and prefix cache agree with observed context profile, token limits).
     7. Machine & resource constraints (hardware accelerator backends, verified acceleration, active slot capacity).
     8. Portfolio structure & diversity (schema validation, exclusions, distinct endpoints/providers/models for review roles).
   - Zero model inference or probabilistic heuristics in the validation gate.
2. **Structured Explanatory Diagnostics:**
   - Emits structured `PortfolioDiagnostic` items containing `Code`, `Condition` (`invalid`, `unsupported`, `unauthorized`, `unknown`, `over_budget`), `Target`, `ViolatedRule`, `Message`, `Observed`, and `Required`.
3. **Atomic Versioned Activation and Rollback:**
   - Active portfolio persisted as pure, valid `protocol.CognitionPortfolio` at `active-portfolio.json`.
   - Immutable versioned snapshots in `portfolio-history/activation-XXXXXX-<actID>.json`.
   - Explicit lineage tracking in `active-portfolio.lineage.json`.
   - Crash-safe atomic file writes using sibling temp files and rename.
   - Clean rollback to previous activation and rollback to specific historical activation ID.
   - Freshness verification: rejection on stale inventory state.

---

## 3. Verification & Evidence
- Pure unit tests in `internal/cognition/portfolio_validator_test.go` covering all valid and invalid matrices.
- Activation tests in `internal/cognition/portfolio_activation_test.go` covering atomic activation, rollback, failure recovery, and freshness.
- Full repository race and schema verification (`make verify`, `go test -race -count=1 ./...`).
