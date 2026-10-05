# Rolling Planning Window 2026-10-D: Milestone M4 Adaptive Cognition Vertical Slice and Evidence Gate

## Window Objective & Overview

Window 2026-10-D initiates **Milestone M4: "Adaptive Cognition Vertical Slice and Evidence Gate"** per `docs/IMPLEMENTATION_PLAN.md` § M4.

M4 tests the central product hypothesis of DevCadence before broader host integration (M5) or brownfield adoption (M6):
> *Can adaptive allocation of heterogeneous cognition resources, combined with compressed engineering artifacts and deterministic controls, preserve or improve accepted engineering quality while reducing scarce-resource consumption versus simpler coding-agent workflows?*

Following the completion of Milestone M3D (commit `a457e7d`), which established adaptive cognition portfolios, deterministic/model topology synthesis, atomic adaptation rollback, and unified CLI UX, Window 2026-10-D builds the empirical evaluation and evidence engine across three tightly coupled, buildable work packages:

1. **WP-M4-1 — Empirical Benchmark Harness & Seeded Defect Corpus** (`internal/benchmark` / `tests/benchmark`)
2. **WP-M4-2 — Delegation-Floor & Capability-Class Experiments** (`internal/benchmark/experiments`)
3. **WP-M4-3 — Evidence Working Set & Metric Telemetry Aggregator** (`internal/benchmark/telemetry`)

---

## Work Packages in Window 2026-10-D

### 1. WP-M4-1 — Empirical Benchmark Harness & Seeded Defect Corpus
- **Package:** `internal/benchmark` / `tests/benchmark`
- **Focus:** Controlled orchestration harness evaluating four distinct context strategies against representative engineering tasks and a multi-dimensional seeded defect corpus.
- **Context Strategies Under Test (ADR-0019 / ADR-0020):**
  1. *Strategy 1 (Full History Baseline):* Monotonic conversational transcript accumulation (traditional agent loop).
  2. *Strategy 2 (Multi-tier Compaction Baseline):* Conversational transcript compaction across turn thresholds per ADR-0016.
  3. *Strategy 3 (Static Prefix + Active Snippets):* Static role prefix with a dynamic pool of active file/test snippets.
  4. *Strategy 4 (Hybrid 4-Layer Context):* Protected Core, Cognitive State Capsule, Evidence Working Set, and Ephemeral Tail compiled by `internal/cognition/compiler`.
- **Seeded Defect Corpus:**
  - Invariant Violations (e.g., bypassing fail-closed budget authorization, skipping validator gating).
  - API & Protocol Mutations (e.g., breaking schema types, field name drift, omitted mandatory validation).
  - Boundary & Isolation Violations (e.g., forbidden cross-package imports, unmediated tool execution).
- **Key Invariants:**
  - Benchmark execution is deterministic and replayable (DCI-005, DCI-014).
  - Test tasks and seeded defects are immutable fixtures with cryptographic digests.
  - Harness operates via abstract `drivers.SessionDriver` / `drivers.Session` substrates; zero vendor SDK coupling (DCI-054/055).

### 2. WP-M4-2 — Delegation-Floor & Capability-Class Experiments
- **Package:** `internal/benchmark/experiments`
- **Focus:** Executes identical implementation-ready EWPs against materially different cognition capability classes (e.g. small local coder, subscription coding CLI, frontier coder) to empirically test the delegation floor hypothesis (ADR-0024).
- **Hypothesis & Falsification:**
  - *Hypothesis:* Complete contract specification (zero implementation-critical ambiguity) lowers the minimum capability tier required to achieve first-pass acceptance without quality degradation.
  - *Falsification Criteria:* If repair rounds and architectural violations do not decrease despite full semantic closure, the contract-completeness delegation floor hypothesis is falsified for that workload class.
- **Key Measurements:**
  - First-pass acceptance rate (`ACC-*` scenarios passing on attempt 1).
  - Architectural repair findings caught by independent review lenses.
  - Principal re-entry rate and total repair iterations.

### 3. WP-M4-3 — Evidence Working Set & Metric Telemetry Aggregator
- **Package:** `internal/benchmark/telemetry`
- **Focus:** Captures, normalizes, and aggregates multi-dimensional resource and quality metrics across benchmark runs into canonical JSON/Markdown evidence reports.
- **Telemetry Dimensions (ADR-0019 §1, docs/IMPLEMENTATION_PLAN.md § M4):**
  - *Token Dynamics:* Initial resident tokens, peak resident tokens, cached prompt tokens, output tokens, cumulative submitted tokens.
  - *Context Composition:* Protected Core vs. State Capsule vs. Leased Evidence vs. Ephemeral Tail token volume.
  - *Execution Efficiency:* Wall time, turn counts, driver session restarts, evidence cache hit rates.
  - *Quality Yield:* Seeded defects detected vs. missed, false positive rate, review findings before/after normalization.
  - *Resource-to-Accepted-Result:* Total compute/token expenditure per accepted engineering change.

---

## Architectural Boundaries and Governance

1. **"AI Proposes, Deterministic Machinery Authorizes" (DCI-123/124/125):**
   Benchmark harness execution and candidate evaluations rely on deterministic validators (`WorkflowValidator`, `AuthorizeWorkflowBudget`, test suites). Model self-assessment is never treated as verification evidence.
2. **Epistemic Boundaries (DCI-005, AGENTS.md §4):**
   Observed token counts and exit codes are recorded as facts. Model reasoning quality and architectural adherence are evaluated through structured review lenses, not subjective claims.
3. **No Provider Coupling (DCI-054/055):**
   The benchmark harness and experiment runner interact exclusively with `internal/cognition/drivers.SessionDriver`, `compiler.Compiler`, and standard repository tools.
4. **Clean-Context Independent Review (DCI-046, DCI-049):**
   All evaluation lenses (Contract & Authority Reviewer, Test Adequacy & Mutation Reviewer) evaluate candidates in clean, unpolluted contexts with isolated review packets.
5. **Mutation Testing Rigor:**
   Every package defines an adversarial mutation catalog table and verifies that all plausible mutants in harness logic, defect scoring, and metric aggregation are killed by deterministic tests.
