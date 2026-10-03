# ADR-0023: Open Project Capability Packs

- **Status:** Proposed
- **Date:** 2026-10-03
- **Owners:** DevCadence architecture
- **Related:** ADR-0020, ADR-0021, ADR-0022; M6; M9

## Context

DevCadence needs horizontal ecosystem extensibility without either hard-coding every stack into core or trusting arbitrary plugin/prompt content.

Pack producers may be built-in, human, fetched, project-local, or LLM-generated. Producer identity therefore cannot be the primary trust boundary.

## Decision

DevCadence defines one strict **ProjectCapabilityPack contract** and one PackRegistry.

A pack combines:

1. an open Agent Skills-compatible layout for model-facing procedures;
2. a DevCadence manifest for deterministic mechanics, applicability, compatibility, origin/lineage, and result semantics;
3. fixtures/conformance evidence.

### Producer-neutral validation

Any producer may create a pack. Confidence derives from schema validation, deterministic compatibility, conformance, seeded-failure checks, optional Skill trials, provenance, and active trust policy.

For important health classes, conformance MUST NOT depend only on seeds/fixtures authored by the pack producer. DevCadence owns a generic seed catalog by check class; packs provide ecosystem-specific realization adapters. Validation records seed provenance/independence so self-authored-only validation remains a weaker evidence class. Seed realization is declarative/bounded by default; executable helpers are sandbox/policy gated. Conformance SHOULD discriminate the intended failure class rather than accepting any mutation that merely makes the target command fail.

### Mechanics scope

The manifest may describe environment/tool probes, build/typecheck, run/dev, tests, lint/static analysis, coverage, generated-code checks, install/device/deploy mechanics, and result adapters.

### Generic results

Core prefers stable result semantics such as SARIF, JUnit, LCOV/Cobertura, and typed finding fingerprints. Exit-code-only commands are an explicit weaker tier.

### Skills

Agent Skills-format procedures are advisory. Compiler-driven invocations select applicable pack Skills deterministically; host-driven sessions receive a provenance-tagged projection, avoiding double injection.

Skills cannot weaken higher authority or expand tool capability.

### Synthesis

Brownfield adoption may synthesize a candidate pack from repository reality. The same conformance harness applies. Generated packs/Skills preserve honest provenance and may be trial-validated.

### Trust profiles

Initial presets range from personal/permissive to team/strict. Profiles change confirmation/source defaults, not always-on integrity:

- candidate cannot rewrite the base judge used to accept itself, including exact activated pack-content digests and resolved toolchain composition;
- evidence records pack/contract/tool provenance;
- pack/Skill authority is capped;
- execution uses controlled worktrees/processes.

### Judge succession and uncovered scope

A proposed health/pack/toolchain-policy successor is accepted separately from the implementation candidate it judges. The successor-acceptance authority MUST be independent of the candidate producer and MUST NOT be invocable by that worker. The implementation is evaluated under the prior accepted judge; only after configured human/review/policy acceptance does the successor become the new base.

New files under an already-covered module/path remain covered. New modules, capability/toolchain components, check classes, or executable/action surfaces absent from base coverage are recorded as uncovered and require explicit successor coverage before managed readiness. Greenfield/adoption bootstrap establishes the first judge as part of initial readiness.

Mutable pack content is re-digested at use so project-local/home/fetched PackDrift cannot silently alter the active judge.

### Origin and lineage

Activated packs record logical ID, exact revision/content digest, origin kind/locator, resolved source revision, acquisition provenance, and validation provenance.

A successor may produce a PackUpgradeProposal. Upgrades are explicit, revalidated, reversible, and preserve historical evidence under prior revisions. Active projects never silently follow mutable branches/tags/URLs.

### Distribution

Initial sources may be embedded built-ins, project-local directories, a home pack library, and explicit GitHub/HTTPS fetches.

A central registry/signing infrastructure is deferred. The same loader/contract applies regardless of source.

### Composition

Capabilities compose per module; dependencies/conflicts are explicit. Incidental load order never resolves semantic conflicts.

### Research pointers

Packs may contain authoritative research/documentation pointers. M6 does not require a large curated package/library recommendation catalog.

### Learning

M9 may propose new pack/Skill revisions from evaluated trajectories. Promotion remains explicit, versioned, evaluated, and reversible.

## Consequences

Positive:

- ecosystem support scales horizontally;
- arbitrary producers can contribute without being inherently trusted;
- bespoke brownfield stacks can synthesize support;
- lineage supports later upgrades;
- Skills reuse an open format while DevCadence retains deterministic authority.

Costs:

- conformance harness;
- result normalization/fingerprinting;
- lineage/compatibility state;
- trust-profile policy;
- Skill projection/admission rules.

## Rejected alternatives

### Core language/framework branches
Rejected as vertically scaling complexity.

### Trusted built-ins only
Rejected as too restrictive for bespoke stacks.

### Arbitrary executable plugins first
Rejected; data/Skills/command mechanics plus narrow controlled helpers cover the initial need with less security/versioning complexity.

### Host Skills as sole authority
Rejected. The open Skills format is useful for advisory procedures; DevCadence manifest/compiler/project policy still own capability and authority.

### Silent auto-upgrade
Rejected because it breaks reproducibility and can mutate policy/evidence semantics without review.

## Verification direction

M6 should prove Go and Flutter reference packs, an LLM-synthesized unfamiliar Zeta pack, schema/compatibility/conformance, seeded failure, Skill trial, source pinning and upgrade, provenance reporting, compositional capability, and permissive/strict trust presets.
