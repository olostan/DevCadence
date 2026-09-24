# ADR-0012: Mandatory canonical baseline for brownfield project adoption

- **Status:** Accepted
- **Date:** 2026-09-21
- **Related:** DCI-015, DCI-016, DCI-052, DCI-053, DCI-109..DCI-113
- **Documents:** docs/PROJECT_ADOPTION.md, docs/DISCOVERY_AND_SPECIFICATION.md, docs/PROJECT_STATE.md

## Context

DevCadence must be able to take over existing repositories, including projects with no useful documentation, stale documentation, arbitrary documentation formats, undocumented tests/contracts, and architectural rationale preserved only in code or Git history.

Repository registration alone is insufficient. If DevCadence begins normal managed engineering work before reconstructing the project's current engineering contract, later agents can make locally reasonable changes against incorrect assumptions.

Conversely, requiring every existing repository to already contain DevCadence-formatted documentation would prevent brownfield adoption entirely.

## Decision

### 1. Brownfield adoption is a first-class workflow

Existing repositories enter a retrospective reconstruction workflow before normal managed engineering work.

The workflow may inspect code, tests, existing documentation, configuration and Git history; maintain ambiguity/contradiction state; ask targeted human questions; and create candidate canonical documentation.

### 2. Canonical project documentation is mandatory

A repository is not DevCadence-ready until a required canonical documentation baseline exists in Git and passes the Adoption Readiness Gate.

The required semantic slots are:

- VISION.md;
- REQUIREMENTS.md;
- ARCHITECTURE.md;
- INVARIANTS.md;
- SECURITY.md;
- TEST_STRATEGY.md;
- OPERATIONS.md;
- adr/README.md plus the applicable ADR set.

The default canonical root for adopted projects is `docs/devcadence/`. Another committed path may be configured explicitly.

Required artifacts are not optional. A genuinely inapplicable concern is represented explicitly as NOT_APPLICABLE with rationale.

### 3. Existing documentation remains evidence

Existing README/design/RFC/ADR material is harvested and preserved, but does not become canonical merely because it exists.

The canonical baseline may reference strong native documentation, but each required canonical slot still exists as the stable authoritative entry point and must summarize status/provenance sufficiently for a new principal.

### 4. Reconstruction preserves epistemic origin

Material statements distinguish observed behavior, existing documentation, inference, human-confirmed intent, reconstructed-confirmed contracts, unknowns, contradictions and accepted risk.

Retrospective reconstruction must not rewrite uncertainty as historical fact.

### 5. Adoption baseline is version-controlled

Canonical docs are produced in an isolated adoption branch/worktree and committed.

The adoption record pins:
- source commit used for reconstruction;
- candidate/adopted baseline commit;
- evidence and unresolved-risk references;
- readiness decision.

This creates a durable historical boundary between legacy repository state and DevCadence-managed state.

### 6. Normal managed work is gated

Before adoption READY, bounded discovery and adoption work are allowed.

Normal autonomous implementation, acceptance and integration as a DevCadence-managed project are blocked.

### 7. Readiness means bounded uncertainty

Adoption does not require perfect reverse engineering of every file.

The gate requires enough evidence that:
- major architecture/contracts are understood;
- canonical docs exist and are committed;
- material contradictions are resolved or explicitly bounded;
- critical product ambiguity is confirmed/deferred safely;
- remaining uncertainty is visible.

## Consequences

### Positive

- DevCadence can safely adopt poorly documented existing repositories.
- Every managed project presents a predictable canonical contract to future agents.
- Existing documentation is preserved rather than destructively reformatted.
- Code/tests/history can repair stale or missing documentation.
- The point where DevCadence begins managing the project is auditable in Git.
- Reconstructed rationale does not masquerade as original documented intent.

### Costs

- Brownfield onboarding can be a substantial discovery exercise.
- Some projects will require human decisions before becoming READY.
- Canonical docs create an ongoing maintenance obligation.
- Adoption needs explicit provenance and contradiction handling.
- Brownfield adoption is large enough to own M6; the earlier M4 adaptive-cognition evidence gate intentionally does not wait for this subsystem.

## Milestone mapping

- **M2:** repository/worktree/deterministic substrate; unchanged.
- **M3A-M3D:** environment/bootstrap/cognition substrate and adaptive planning; adoption-independent.
- **M4:** adaptive-cognition evidence gate on controlled projects/fixtures; does not claim brownfield readiness.
- **M5:** semantic MCP + principal-host connectivity.
- **M6:** project adoption and retrospective reconstruction, including the brownfield readiness proof.

This ADR does not require M2 or M3 implementation changes.
