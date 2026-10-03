# Project Toolchains and Health Contracts

## Scope

ProjectCapabilityPacks provide ecosystem mechanics and Skills. This document defines how those capabilities become explicit project/toolchain composition and deterministic health authority.

> **Evidence is not capability. Capability is not authority.**

- manifests/configuration/executables are evidence;
- conformed/activated pack mechanics are capability;
- ProjectHealthContract/project invariants are authority.

## 1. Compositional toolchains

A module may require multiple independent capability components:

~~~text
web module
  language: TypeScript
  package manager: pnpm
  framework: React
  build: Vite
  checks: tsc + eslint + vitest
~~~

Flutter may compose Dart, Flutter SDK, Android/Gradle, and iOS/Xcode capability.

Avoid combinatorial monolithic toolchain IDs. A resolved **ToolchainProfile** is a versioned, validated composition of capability components for a module. One module may bind 1:N components/packs.

Discovery proposes candidates; explicit activation/resolution establishes capability.

## 2. Native tooling

Brownfield adoption discovers existing build/package config, format/lint tools, tests/coverage, generated-code checks, run/dev commands, and CI/presubmit/merge enforcement.

Good native tooling is mapped rather than replaced for style.

Integration tiers:

1. **structured** — supported standard/result format;
2. **parsed** — pack adapter parses stable native output;
3. **opaque** — exit-code pass/fail only.

Opaque checks provide weaker finding/no-new-debt evidence.

## 3. ProjectHealthContract

Every normally managed project has a versioned ProjectHealthContract containing:

- required check identities;
- module/toolchain ownership;
- lifecycle gates;
- candidate/base identity;
- result/fingerprint semantics;
- regression/baseline rules;
- historical debt;
- bounded exceptions;
- enforcement-adapter provenance.

ValidationProfiles are execution projections. Hooks/CI are enforcement adapters, not independent policy stores.

## 4. Base governs candidate acceptance

A candidate that modifies any part of its accepted judge MUST be evaluated under the accepted **base** judge.

The judge includes:

- ProjectHealthContract;
- inherited-debt baseline;
- resolved ToolchainProfile composition;
- exact activated pack mechanics/result-adapter/fixture digests;
- managed hook/CI/acceptance adapters;
- applicable validation policy.

Candidate policy/capability is only a proposed successor until separately accepted/integrated.

This prevents an agent from "fixing" a red candidate by weakening the judge.

A legitimate successor-policy change follows two steps:

1. code/content changes are evaluated under the old/base judge;
2. the policy/capability delta is separately accepted by the configured authority.

Only then does the successor become the next base.

A permissive solo workflow may use explicit human confirmation instead of team review, but evidence still distinguishes base evaluation from successor-policy acceptance.

If a candidate adds modules/paths/actions not covered by the base judge, those areas are **uncovered/unjudged**, not implicitly passing. Acceptance requires explicit successor coverage for the new scope before it becomes managed.

Activated packs are re-digested when used. A mismatch between active recorded digest and mutable on-disk pack content is PackDrift and MUST be surfaced according to trust policy rather than silently treated as the same judge.

## 5. Candidate-accurate evidence

Health evidence identifies:

- candidate tree/commit/ref;
- base identity;
- ProjectHealthContract revision/digest;
- pack IDs/revisions/content digests;
- tool versions;
- check/profile identity;
- result artifacts/fingerprints;
- validation provenance.

Presubmit validates the intended candidate/staged snapshot; pre-push validates pushed refs; PR validation uses exact base/candidate identity; integration validates the combined candidate.

## 6. No-new-debt

Brownfield adoption may record historical findings instead of requiring immediate perfection.

Prefer stable per-finding fingerprints over raw counts:

~~~text
baseline findings B
candidate findings C

new debt = C - matched(B)
repaired debt = B - matched(C)
~~~

Fingerprints should survive irrelevant line movement where supported.

Normal ratchets prevent new findings and allow accepted debt to shrink.

If a pack/tool/rule revision changes finding semantics, DevCadence explicitly reconciles/rebaselines rather than silently comparing incompatible measurements.

## 7. Health layers

**Mechanical integrity (M6):** format, build/typecheck, lint/static analysis, tests, coverage, generated-code drift, API/schema checks, dependency hygiene, project-specific deterministic checks.

**Quantitative regression:** coverage/benchmark/size/flaky-test/finding signals where stable.

**Semantic/architectural health (M8):** duplication, coupling/layer erosion, architecture/document divergence, maintainability/refactoring.

M6 establishes minimum mechanical health; M8 reasons longitudinally/semantically over it.

## 8. Capability versus authority

Packs expose available checks; project policy chooses what is required.

Capability presence MUST NOT silently create a project invariant.

Pack invariant candidates become operative only through explicit project/adoption authority.

## 9. Conformance strength

A generated/fetched check is stronger when conformance proves command execution, output interpretation, seeded-failure detection, and declared version compatibility.

Health results retain conformance/provenance so permissive operation does not overstate confidence.

## 10. Enforcement adapters and drift

Managed hooks/CI carry enough provenance to identify contract digest, adapter revision, and relevant pack/check revisions.

Drift can be classified as current, native-equivalent, stale-managed adapter, missing surface, or unresolved custom modification.

## 11. Pack/tool upgrades

Because project state retains origin and exact revision, DevCadence can later propose upgrades.

A changed pack/check/tool revision is compatibility-checked and conformance-tested, affected health/Skill trials are rerun, baseline/fingerprint changes are reconciled, and activation remains explicit.

Past evidence stays tied to the historical revision that produced it.

## 12. Trust profiles

Trust profiles affect convenience/approval defaults, not integrity semantics.

Personal/permissive may auto-adopt/warn/record. Team/strict profiles may require approvals/source restrictions.

All preserve:

- candidate cannot rewrite its own judge;
- evidence provenance honesty;
- pack/Skill authority caps;
- controlled execution/isolation;
- explicit pack activation/update.

## 13. M6/M8 boundary

M6 owns PackRegistry integration, capability/toolchain resolution, ProjectHealthContract, native mapping, conformance, brownfield debt/no-new-debt, base-governed acceptance, adapter drift, and readiness.

M8 owns trend history, structural/semantic health, Refactoring Epochs, Architecture Reconciliation, and debt-reduction campaigns.

See [PROJECT_CAPABILITY_PACKS.md](PROJECT_CAPABILITY_PACKS.md) and ADR-0021.
