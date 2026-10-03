# Engineering Work Package: WP-M6-H1 — Extensible Project Bootstrap, Capability Packs, Toolchains, and Health

- **Milestone:** M6 — Project Bootstrap, Toolchains, and Adoption
- **Kind:** architecture / protocol / roadmap
- **Base commit:** `c35b7efe2aee508b196357b43ed7a02f2e815604`
- **Branch:** `architecture/project-health-contracts`
- **Version:** 2.0
- **Status:** Proposed — independent architecture review pending

## Objective

Define the architecture for producer-neutral ProjectCapabilityPacks, requirement-driven greenfield creation, compositional toolchains, and deterministic/base-governed project health without turning ecosystem support into vertical DevCadence-core complexity.

## Required decisions

1. Evidence, capability, and authority remain distinct.
2. ProjectCapabilityPack is the single horizontal ecosystem-extension contract; PackRegistry is the single registry abstraction.
3. A pack may be built-in, human-authored, fetched, project-local, or LLM-synthesized; producer identity does not establish trust.
4. Pack knowledge uses open Agent Skills-format content plus a DevCadence mechanical manifest.
5. Pack mechanics cover environment, build/typecheck, run/dev, tests, lint/static analysis, coverage, generated-code, install/device/deploy, and result adapters where applicable.
6. Conformance includes command/result validation plus core-owned/independently attributable seeded-failure checks for important mechanics; producer-authored-only seeds are weaker evidence.
7. Generic result interchange/fingerprints are preferred; opaque pass/fail is an explicit weaker tier.
8. Pack/Skill provenance is honest and preserved on evidence.
9. Pack origin/lineage records exact revision/digest and source locator; successor versions are explicit upgrade proposals, never silent updates.
10. LLM pack recommendation/synthesis is allowed; compatibility validation and activation remain controlled.
11. Packs may contain bounded authoritative research pointers but M6 does not maintain a broad curated dependency catalog.
12. Pack invariant candidates are proposals until project authority adopts them.
13. ToolchainProfile is a resolved composition of module capabilities, not a monolithic language/package/build/compiler combination.
14. ProjectHealthContract is the canonical health authority.
15. The accepted base judge—contract, baseline, resolved toolchain composition, activated pack-content digests/fixtures/result adapters, and managed adapters—governs a candidate that proposes changes; new uncovered scope requires separately accepted successor coverage.
16. Successor-policy approval authority is capability-separated from the candidate producer; the producing worker cannot possess or invoke the approval credential/channel, even indirectly through its own shell/tools/API surface.
17. Brownfield no-new-debt uses stable finding fingerprints where possible and explicit reconciliation across pack/tool/rule upgrades.
18. Trust profiles vary convenience/approval defaults but never weaken evidence honesty, authority caps, candidate identity, isolation, or base-governed acceptance.
19. Greenfield creation uses TechnologyDecision + ProjectBlueprint + bounded EWP + pack Skills; DevCadence does not own a universal ScaffoldRecipe registry.
20. Minimum mechanical health belongs to M6; semantic/longitudinal health remains M8.
21. M6 implementation is sequenced after the M4 core-hypothesis evidence gate.

## Allowed scope

Architecture/documentation plus required invariant-catalog synchronization:

- `AGENTS.md`
- `README.md`
- `INVARIANTS.md`
- `docs/REQUIREMENTS.md`
- `docs/ARCHITECTURE.md`
- `docs/DISCOVERY_AND_SPECIFICATION.md`
- `docs/PROJECT_ADOPTION.md`
- `docs/PROJECT_TOOLCHAINS_AND_HEALTH.md`
- `docs/PROJECT_CAPABILITY_PACKS.md`
- `docs/PROJECT_CREATION_AND_SCAFFOLDING.md`
- `docs/PROTOCOLS.md`
- `docs/SETUP.md`
- `docs/IMPLEMENTATION_PLAN.md`
- `docs/ENVIRONMENT_INTELLIGENCE_AND_ONBOARDING.md`
- `docs/adr/0018-adaptive-cognition-portfolio-and-workflow-synthesis.md`
- `docs/README.md`
- `docs/WORK_PACKAGES.md`
- ADR-0021..0023
- this EWP
- `internal/cognition/compiler/catalog.go`
- `internal/cognition/compiler/catalog_test.go`
- `internal/cognition/compiler/equivalence_test.go`

## Non-goals

- no runtime pack loader/conformance implementation yet;
- no schema implementation yet;
- no central pack registry/signing infrastructure;
- no universal library/package recommendation database;
- no DevCadence-maintained scaffolding/template registry;
- no claim that any new ecosystem pack is already production-supported.

## Acceptance

Independent review should verify:

- horizontal extension without ecosystem-specific core branches;
- Skills remain advisory and cannot bypass DCI-132/authority;
- generated/fetched packs have meaningful conformance/provenance;
- lineage and upgrade semantics are reproducible;
- base-governed health prevents self-weakened acceptance;
- successor approval is capability-separated/out-of-band from the producing worker, with negative tests proving self-approval attempts fail;
- no-new-debt survives finding movement and pack/tool upgrades;
- toolchain composition avoids combinatorial IDs;
- M6/M8 and M4/M6 sequencing are clear;
- all normative docs and invariant catalog/tests are synchronized.


## M6A follow-up backlog

These are non-blocking architecture/implementation details to resolve while decomposing M6A into executable work packages:

- split pack provenance into orthogonal dimensions (origin × validation level × seed/trial independence) instead of a combinatorial class list;
- define a concrete trust-profile action matrix and state the default profile, including fetch/install/conformance/activation/upgrade/policy-edit actions;
- specify no-new-debt semantics for structured, parsed, and opaque result tiers (opaque checks are pass/fail or explicitly excepted, not silently ratcheted);
- reconcile "auto-adopt where configured" with DCI-153: any automatic adoption/upgrade must be an explicit configured policy that still resolves, diffs, validates, records provenance, and remains reversible;
- define how Agent Skills-declared permissions/tool pre-approvals are ignored or capped by active DevCadence authority, and pin the supported Agent Skills spec revision in the pack schema;
- reconcile DCI-150 normative strength with FR-075 so optional/advisory Skill content is not accidentally made mandatory for every pack.
