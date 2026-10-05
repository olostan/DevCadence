# WP-M4-3 — Evidence Working Set & Metric Telemetry Aggregator

## Identity

- Work Package ID: WP-M4-3 (window 2026-10-D; slice 3 of Milestone M4)
- Revision: 2 (window review: complete struct definitions, int64 token unification, empty snapshot normalization, zero-defect division-by-zero fix, ResourceEfficiency typed representation, embedded layer breakdown, expanded mutation catalog)
- Task ID: task-m4-3-telemetry-aggregator
- Base commit: `a457e7d1e28f37c5055d1f51a4da45a4320749a7`
- Project state revision: 1
- Contract digest: n/a (Markdown contract authoritative)
- Target implementation endpoint/profile: competent Go implementer
- Status: READY_FOR_IMPLEMENTATION (r2)

## Objective

Deliver the evidence working set measurement and telemetry aggregation engine in `internal/benchmark/telemetry`. The aggregator collects, normalizes, and analyzes empirical metrics across benchmark and experiment runs:
1. Token Dynamics & Working-Set Residency: initial tokens, peak resident tokens, prompt caching efficiency, output tokens, and cumulative submitted tokens (using `int64` throughout).
2. Layered Context Decomposition: breakdown of tokens across Protected Core, Cognitive State Capsule, Evidence Working Set, and Ephemeral Tail.
3. Execution Efficiency & Resource Cost: turn counts, wall time, driver restarts, and total resource-to-accepted-result ratio.
4. Quality Yield & Defect Catch Rate: seeded defect detection rate, false positives, and normalized review findings.
5. Report Generation: deterministic JSON and Markdown evidence synthesis for Milestone M4 gate evaluation.

## Context Manifest

- role: implementer (Go), independent reviewers
- read-authority envelope: `internal/protocol/context.go`, `internal/cognition/compiler/compile.go`, `internal/cognition/drivers/types.go`, `internal/benchmark/types.go`, `docs/adr/0019-non-conversational-cognition-and-adaptive-review.md`, `docs/IMPLEMENTATION_PLAN.md` § M4
- semantic write/scope envelope: `internal/benchmark/telemetry/` (collector, metrics, aggregation, reporting), tests, docs
- risk tags: telemetry precision, unit normalization, division by zero safety, report reproducibility
- exact normative clauses: DCI-005 (epistemic distinction of facts vs estimates), DCI-014 (evidence is progressively retrievable), DCI-123 (deterministic authorization)
- initial evidence handles: `internal/benchmark/types.go`, `internal/protocol/context.go`
- deferred references: none
- assumptions: A1 Run telemetry feeds into aggregator via typed event/snapshot structures. A2 Markdown reports follow clean GitHub-flavored format.
- re-resolution triggers: changes to `protocol.ContextBreakdown` or telemetry schema.

## Semantic scope envelope

### Authorized domains / path patterns

- `internal/benchmark/telemetry/metrics.go` (new)
- `internal/benchmark/telemetry/collector.go` (new)
- `internal/benchmark/telemetry/aggregator.go` (new)
- `internal/benchmark/telemetry/reporter.go` (new)
- `internal/benchmark/telemetry/telemetry_test.go` (new)
- `docs/work-packages/wp-m4-3-ewp.md`

### Explicitly forbidden semantic changes

- No subjective narrative generation without underlying quantitative evidence.
- No dropping of uncertainty flags when driver token accounting is provisional.
- No external network calls or remote reporting services.

### LOCAL_DISCRETION

- Formatting of ASCII summary tables in Markdown reports.
- Internal accumulators and statistical helper functions (mean, median, stddev).
- Test benchmark fixture generators.

## Requirements

| ID | Strength | Requirement | Normative source |
| --- | --- | --- | --- |
| REQ-01 | MUST | Define `LayerBreakdownMetrics`: `ProtectedCoreTokens int64`, `StateCapsuleTokens int64`, `EvidenceWorkingSetTokens int64`, `EphemeralTailTokens int64`, `TotalTokens int64`. | ADR-0019 §1 |
| REQ-02 | MUST | Define `RunTelemetrySnapshot`: `RunID string`, `TaskID string`, `Strategy string`, `Capability string`, `InitialTokens int64`, `PeakResidentTokens int64`, `CachedTokens int64`, `OutputTokens int64`, `CumulativeInputTokens int64`, `Duration time.Duration`, `Turns int`, `DefectSeeded bool`, `DefectStatus string`, `Accepted bool`, `ReviewFindingsCount int`, `LayerBreakdown *LayerBreakdownMetrics`, `AccountingUncertain bool`. | docs/IMPLEMENTATION_PLAN.md § M4 |
| REQ-03 | MUST | Define `TelemetryCollector`: Thread-safe collector recording snapshots: `RecordSnapshot(s RunTelemetrySnapshot)`, `RecordLayerBreakdown(runID string, b LayerBreakdownMetrics)`, `GetSnapshots() []RunTelemetrySnapshot`. | DCI-014 |
| REQ-04 | MUST | Define `ResourceEfficiency`: `Value float64`, `IsUndefined bool` (true if AcceptedCount == 0, serialized in JSON as `{"value": 0, "is_undefined": true}`, avoiding `+Inf`/`NaN`). | docs/IMPLEMENTATION_PLAN.md § Measurements |
| REQ-05 | MUST | Define `AggregatedTelemetry`: `Strategy string`, `Capability string`, `RunCount int`, `AcceptedCount int`, `FirstPassAcceptanceRate float64`, `DefectCatchRate float64`, `DefectCatchRateApplicable bool`, `CacheHitRatio float64`, `AvgInitialTokens float64`, `AvgPeakResidentTokens float64`, `AvgCumulativeInputTokens float64`, `AvgDuration time.Duration`, `ResourcePerAcceptedResult ResourceEfficiency`, `ContainsUncertainAccounting bool`. Grouped by `(Strategy, Capability)`. | docs/IMPLEMENTATION_PLAN.md § M4 |
| REQ-06 | MUST | Define `AggregatedReport`: `GeneratedAt time.Time`, `TotalSnapshots int`, `Groups []AggregatedTelemetry`. Define `Aggregate(snapshots []RunTelemetrySnapshot) (*AggregatedReport, error)`. If `len(snapshots) == 0`, returns `&AggregatedReport{GeneratedAt: time.Now(), TotalSnapshots: 0, Groups: nil}, nil` safely without error or division-by-zero. | DCI-005 |
| REQ-07 | MUST | Defect Catch Rate Calculation: If `count(DefectSeeded) == 0`, sets `DefectCatchRate = 1.0` and `DefectCatchRateApplicable = false`. Else computes `float64(count(DefectDetected|Prevented)) / float64(count(DefectSeeded))`. | docs/IMPLEMENTATION_PLAN.md § M4 |
| REQ-08 | MUST | Report Synthesis: `GenerateJSONReport(report *AggregatedReport) ([]byte, error)` and `GenerateMarkdownReport(report *AggregatedReport) (string, error)`. Markdown formats undefined resource efficiency as `"N/A (no accepted runs)"` and flags accounting uncertainty explicitly. | docs/IMPLEMENTATION_PLAN.md § Exit criterion |
| REQ-09 | MUST | Uncertainty Propagation: If any snapshot in an aggregate group has `AccountingUncertain: true`, the aggregate group sets `ContainsUncertainAccounting: true`. | DCI-005 |

## Invariants / state rules

| ID | Statement | Related requirements |
| --- | --- | --- |
| INV-01 | Telemetry collection and aggregation are thread-safe and deterministic. | REQ-03, REQ-06 |
| INV-02 | Metric calculations never divide by zero or emit NaN/Inf in JSON output. | REQ-04, REQ-06, REQ-07 |
| INV-03 | Reports are strictly grounded in collected snapshots; no invented metrics. | REQ-08, DCI-005 |

## Interface / algorithm contract

```text
TelemetryAggregator.Aggregate(snapshots):
  1. If len(snapshots) == 0:
     return &AggregatedReport{GeneratedAt: time.Now(), TotalSnapshots: 0, Groups: nil}, nil
  2. Group snapshots by GroupKey{Strategy: s.Strategy, Capability: s.Capability}.
  3. For each group:
     a. Compute sums and means for token metrics (using int64 sums converted to float64 averages).
     b. CacheHitRatio = sum(CachedTokens) / max(sum(CumulativeInputTokens), 1).
     c. AcceptanceRate = float64(count(Accepted)) / float64(len(groupSnapshots)).
     d. If count(DefectSeeded) == 0:
        DefectCatchRate = 1.0; DefectCatchRateApplicable = false
        Else:
        DefectCatchRate = float64(count(DefectDetected|Prevented)) / float64(count(DefectSeeded))
        DefectCatchRateApplicable = true
     e. If count(Accepted) == 0:
        ResourcePerAcceptedResult = ResourceEfficiency{Value: 0, IsUndefined: true}
        Else:
        ResourcePerAcceptedResult = ResourceEfficiency{Value: float64(sum(CumulativeInputTokens)) / float64(count(Accepted)), IsUndefined: false}
     f. Propagate ContainsUncertainAccounting = any(s.AccountingUncertain).
  4. Sort group results deterministically by Strategy ascending, then Capability ascending.
  5. Return AggregatedReport{GeneratedAt: time.Now(), TotalSnapshots: len(snapshots), Groups: sortedGroups}.
```

## Authority matrix

| Decision/effect | Authorized source | Forbidden substitute |
| --- | --- | --- |
| Metric aggregation | `TelemetryAggregator.Aggregate` | Ad-hoc spreadsheet / external scripts |
| Markdown report synthesis | `reporter.GenerateMarkdownReport` | Manual unverified author editing |

## Missing / unknown / stale input semantics

| Input/fact | Missing | Unknown | Stale | Malformed/contradictory |
| --- | --- | --- | --- | --- |
| Snapshots list | return empty report (`&AggregatedReport{}`) | n/a | n/a | fail closed |
| Token Usage | record 0, mark uncertain (`AccountingUncertain=true`) | mark uncertain | n/a | fail closed |

## Failure matrix

| Failure point / condition | Required postcondition | Recovery / response | Evidence |
| --- | --- | --- | --- |
| Zero accepted runs in group | `ResourcePerAcceptedResult` flagged `IsUndefined: true` | Return structured report without NaN | `Report.Groups[k].ResourcePerAcceptedResult.IsUndefined == true` |
| JSON serialization error | Return wrapped error | Clean error return | `err != nil` |
| Empty snapshots slice | Return valid empty report with 0 groups | Clean non-nil report | `Report.TotalSnapshots == 0` |

## Representability map

| Concept / requirement | Exact representation | Adequacy | Action if inadequate |
| --- | --- | --- | --- |
| Telemetry snapshot | `type RunTelemetrySnapshot struct` | represented | — |
| Aggregated report | `type AggregatedReport struct` | represented | — |
| Layer breakdown | `type LayerBreakdownMetrics struct` | represented | — |
| Resource efficiency | `type ResourceEfficiency struct` | represented | — |

## Acceptance scenarios

| ID | Setup / precondition | Action | Expected result | Maps to |
| --- | --- | --- | --- | --- |
| ACC-01 | Snapshots recorded across 4 strategies | Call `Aggregate()` | Generates cleanly grouped metrics; Strategy 4 shows higher cache efficiency and lower peak residency | REQ-01, REQ-02, REQ-05, REQ-06 |
| ACC-02 | Snapshots with 0 accepted runs | Call `Aggregate()` | Calculates safely without panic or NaN; flags `ResourcePerAcceptedResult.IsUndefined = true` | REQ-04, REQ-06, INV-02 |
| ACC-03 | Snapshots containing uncertain accounting | Call `Aggregate()` | Aggregated report flags `ContainsUncertainAccounting: true` | REQ-05, REQ-09, INV-03 |
| ACC-04 | Completed aggregate report with undefined resource efficiency | Call `GenerateMarkdownReport()` | Produces valid Markdown table formatting undefined metric as `"N/A (no accepted runs)"` | REQ-08 |
| ACC-05 | Empty snapshots slice | Call `Aggregate(nil)` | Returns empty report with `TotalSnapshots: 0` without error | REQ-06 |

## Validation

- command / deterministic check: `go test -v -race ./internal/benchmark/telemetry/...`
- command / deterministic check: `make verify`
- mutation testing: mutation review sufficient: adversarial catalog below

### Mutation Catalog (Mandatory)

| Mutant (Plausible Bug / Omission / Boundary Inversion) | Expected Test Failure (Scenario / Invariant Check) |
| --- | --- |
| Division by zero when accepted count is 0 | ACC-02 fails / panic in aggregation test |
| Division by zero when `count(DefectSeeded) == 0` | ACC-05 fails / NaN in defect catch rate test |
| Uncertainty flag ignored during group aggregation | ACC-03 fails / uncertainty flag assertion |
| Token counts sum cached tokens twice | ACC-01 fails / cumulative input token verification |
| Reports emit unsorted group rows causing non-deterministic output | Ordering assertion test fails across repeated runs |
| TotalTokens does not equal sum of individual layer tokens | Layer breakdown integrity test fails |

### Required Independent Review Lenses (Dual-Lens Review Pack)
1. **Contract & Authority Reviewer:**
   - Verifies compliance with `REQ-*` and `INV-*`.
   - Validates proper propagation of epistemic uncertainty flags (`AccountingUncertain`).
   - Ensures thread-safety of `TelemetryCollector`.
2. **Test Adequacy & Mutation Reviewer:**
   - Verifies scenario coverage (`ACC-01` through `ACC-05`).
   - Checks that division-by-zero mutants (0 accepted, 0 seeded defects) are thoroughly tested.

## Implementation Readiness Report

```text
requirements represented: 9/9
mandatory clauses resolved: 5/5
state transitions specified: 3/3
failure cases specified: 3/3
authority decisions specified: 2/2
missing/unknown input semantics: 2/2
acceptance scenarios mapped: 5/5
unresolved architecture choices: 0
declared local-discretion choices: 3
readiness: READY_FOR_IMPLEMENTATION
```

### Weaker-implementer check

Could a competent implementation model with good language/repository skill but mediocre architectural judgment execute this EWP without inventing important semantics?

- [x] Yes
- [ ] No — return to Principal design
