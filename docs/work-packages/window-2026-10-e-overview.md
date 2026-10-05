# Rolling Planning Window 2026-10-E: Milestone M4 Empirical Evidence Campaign

**Status: COMPLETE (2026-10-05), with a documented caveat.** Delivered via PR #72 (specs), #73 (WP-M4-4), #74 (WP-M4-5) and #75 (WP-M4-6). The campaign evidence is synthetic harness validation (scripted driver), not real-endpoint measurement; the M4 product hypothesis is re-evaluated after M5. See `docs/IMPLEMENTATION_PLAN.md` § M4.

## Window Objective & Overview

Window 2026-10-E conducts the **Empirical Evidence Campaign** for **Milestone M4: Adaptive Cognition Vertical Slice and Evidence Gate** per `docs/IMPLEMENTATION_PLAN.md` § M4.

Building upon the empirical benchmark harness, capability-class experiment framework, and telemetry aggregator established in Window 2026-10-D (WP-M4-1, WP-M4-2, WP-M4-3; commit `0c3bce2`), Window 2026-10-E executes the evaluation campaign necessary to validate or falsify the central product hypothesis:

> *Can adaptive allocation of heterogeneous cognition resources, combined with compressed engineering artifacts and deterministic controls, preserve or improve accepted engineering quality while reducing scarce-resource consumption versus simpler coding-agent workflows?*

Window 2026-10-E structures the empirical campaign across three sequential, coherent work packages:

1. **WP-M4-4 — Seeded Defect Corpus Expansion & Held-Out Benchmark Task Suite** (`internal/benchmark/corpus` / `fixtures/benchmark`):
   - Establishes a standardized, versioned suite of 10 representative engineering tasks across distinct workload classes (navigation, implementation, review, architecture).
   - Seeds calibrated defects across all three canonical categories: Invariant Violations, API/Schema Mutations, and Boundary/Isolation Violations.
   - Pinned fixtures with cryptographic digests guarantee immutable and reproducible benchmark runs (DCI-005, DCI-014).

2. **WP-M4-5 — Multi-Strategy Matrix & Delegation-Floor Campaign Execution** (`internal/benchmark/campaign`):
   - Executes the full evaluation matrix: 10 Tasks x 4 Context Strategies (Full History, Compacted, Snippet Pool, Hybrid 4-Layer via Cognitive Invocation Compiler) x 3 Capability Classes (`CapabilityLocalSmall`, `CapabilitySubscriptionCLI`, `CapabilityFrontierAPI`).
   - Runs comparative baseline vs. implementation-ready experiments under ADR-0024 to measure first-pass acceptance rates, architectural review repair rounds, and Principal re-entry.

3. **WP-M4-6 — Empirical Evidence Report & Milestone M4 Gate Evaluation** (`cmd/devcadence/m4gate` / `docs/evidence/`):
   - Ingests raw campaign run logs into `internal/benchmark/telemetry`.
   - Generates the canonical JSON and Markdown Milestone M4 Evidence Report.
   - Evaluates pre-declared falsification criteria (ADR-0024, PROTOCOLS §10B) and formal M4 exit criteria to produce the binding go/revise gate decision for Milestone M5.

---

## Architectural Boundaries and Governance

1. **"AI Proposes, Deterministic Machinery Authorizes" (DCI-123/124/125):**
   Campaign execution evaluates model proposals against deterministic verification suites and independent review passes. Defect catch rates and acceptance decisions are strictly grounded in deterministic facts, never self-assessment.
2. **Epistemic Integrity & Uncertainty Accounting (DCI-005, AGENTS.md §4):**
   Observed token counts, wall time, and exit codes are recorded as empirical facts. If token metrics from session drivers are unobservable, the `AccountingUncertain` flag is strictly propagated.
3. **Reproducibility & Pinned Ground Truth:**
   Benchmark tasks, patch files, and seeded invariants are content-addressed and immutable. Repeated execution under identical seeds produces byte-identical fixture evaluations.
4. **No Provider SDK Leakage (DCI-054/055):**
   The campaign interacts with models exclusively via abstract `drivers.SessionDriver` and `drivers.Session` substrates.
5. **Zero Coverage Regression & Local Gate Enforcement:**
   All new packages require $\ge 90\%$ statement coverage and an adversarial mutation catalog killing all plausible logic mutants.
