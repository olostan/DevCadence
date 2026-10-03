# ADR-0021: Compositional Project Toolchains and Base-Governed Health Contracts

- **Status:** Proposed
- **Date:** 2026-10-03
- **Owners:** DevCadence architecture
- **Related:** ADR-0012, ADR-0015, ADR-0016, ADR-0023; M2 validation; M6 project bootstrap/adoption; M8 engineering health

## Context

DevCadence already has deterministic ValidationProfiles and ADR-0015 module boundaries, but a managed project still needs an explicit answer to:

- which toolchain capabilities actually apply;
- how polyglot/module-specific tooling composes;
- what health checks are authoritative;
- how inherited debt is represented;
- which policy revision governs a candidate that proposes to change the policy itself.

A single monolithic toolchain ID is insufficient for modern projects. A module may combine language/runtime, package manager, build system, framework, compiler, and check providers.

Self-hosting health enforcement also showed that candidate identity, pushed refs, finding semantics, baseline/tool versions, and policy drift are precise control-plane concerns.

## Decision

### Compositional toolchains

ToolchainProfile becomes a resolved, versioned composition of capabilities supplied by one or more active ProjectCapabilityPacks and/or explicitly adopted native project tooling.

A module may bind multiple capability components. Discovery is evidence; activation/resolution is capability; ProjectHealthContract/project invariants are authority.

### ProjectHealthContract

Every normally managed project has one canonical versioned health contract defining:

- required checks;
- lifecycle gates;
- candidate/base semantics;
- result/fingerprint semantics;
- regression/debt baseline;
- bounded exceptions;
- enforcement-adapter provenance.

ValidationProfiles remain execution projections.

### Base governs

A candidate that changes the health contract, baseline, pack/toolchain binding, or managed enforcement adapter is evaluated under the accepted base policy. Candidate policy is a proposed successor until accepted.

This prevents a worker from weakening the judge used to accept its own change.

### No-new-debt

Brownfield inherited debt may be retained as a revision-pinned baseline. Where possible it is represented by stable finding fingerprints rather than raw counts.

A candidate may repair historical findings but must not introduce unmatched new debt unless explicit authority permits it.

If a pack/tool/rule upgrade changes finding semantics, baseline reconciliation is explicit.

### Generic result tiers

Preferred integrations use structured formats such as SARIF, JUnit, LCOV/Cobertura, or typed findings. Parsed native output is acceptable; exit-code-only checks are a weaker explicit tier.

### Enforcement adapters

Hooks/CI/merge checks are projections/adapters of the health contract rather than independent policy stores. Managed adapters carry enough provenance for deterministic drift detection.

### Trust profiles

Trust profiles may vary confirmation/source defaults, but always preserve evidence honesty, candidate/base identity, authority caps, controlled execution, and the base-governs rule.

### M6/M8 boundary

M6 establishes minimum mechanical health and debt baseline. M8 owns longitudinal/semantic health, Refactoring Epochs, and Architecture Reconciliation.

## Consequences

Positive:

- polyglot/toolchain composition avoids combinatorial profile explosion;
- deterministic health authority is reproducible;
- agents cannot self-weaken acceptance policy;
- brownfield debt can ratchet down safely;
- pack/tool upgrades preserve measurement honesty.

Costs:

- finding fingerprint/reconciliation semantics;
- capability composition/resolution;
- more explicit evidence metadata.

## Rejected alternatives

### One monolithic toolchain ID per combination
Rejected as combinatorial and poor for Flutter/native/polyglot modules.

### ValidationProfile alone
Rejected because it does not own authority, debt, baseline, candidate/base semantics, or drift.

### Candidate policy governs its own acceptance
Rejected because it lets an implementation rewrite its own judge.

### Raw debt counts only
Rejected because fixing one violation and adding another can preserve the count while moving debt.

## Follow-up

M6 implementation should define typed ToolchainProfile composition, ProjectHealthContract/baseline/finding protocols, result tiers, adapter provenance, and base-governed candidate evaluation.
