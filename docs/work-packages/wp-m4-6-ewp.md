# WP-M4-6 — Empirical Evidence Report & Milestone M4 Gate Evaluation

## Identity

- Work Package ID: WP-M4-6 (window 2026-10-E; slice 6 of Milestone M4)
- Revision: 3 (r3 amendment after PR #75 review; see Changelog). r2 (window review: CriterionResult struct definition, authorized CLI write domain in cmd/devcadence, pairwise tier matching semantics, IsUndefined fail-closed handling, clean Inconclusive nil-error semantics, expanded mutation catalog)
- Task ID: task-m4-6-evidence-report-gate
- Base commit: `0c3bce20d89010d90bfec93540541effc4591b7c`
- Project state revision: 1
- Contract digest: n/a (Markdown contract authoritative)
- Target implementation endpoint/profile: competent Go implementer
- Status: AMENDED_R3 (implemented; under review, PR #75)

## Objective

Deliver the evidence synthesizer and formal gate evaluator for Milestone M4 in `internal/benchmark/gate` and CLI command `devcadence benchmark evaluate-gate` in `cmd/devcadence/benchmark.go`. The package:
1. Ingests raw campaign run logs and aggregates telemetry via `internal/benchmark/telemetry`.
2. Computes the formal Milestone M4 Evidence Gate Decision (`Go`, `Revise`, or `Inconclusive`) against pre-declared falsification criteria and efficiency metrics (ADR-0019, ADR-0024, docs/IMPLEMENTATION_PLAN.md § M4).
3. Produces the canonical, publication-ready Milestone M4 Evidence Report in Markdown (`docs/evidence/m4-evidence-report.md`) and JSON format.

## Context Manifest

- role: implementer (Go), independent reviewers
- read-authority envelope: `internal/benchmark/telemetry/metrics.go`, `internal/benchmark/telemetry/aggregator.go`, `internal/benchmark/telemetry/reporter.go`, `internal/benchmark/campaign/campaign_spec.go`, `docs/IMPLEMENTATION_PLAN.md` § M4, `docs/adr/0019-non-conversational-cognition-and-adaptive-review.md`, `docs/adr/0024-implementation-ready-work-packages-and-contract-completeness.md`
- semantic write/scope envelope: `internal/benchmark/gate/` (gate evaluator, criteria, report synthesis), `cmd/devcadence/benchmark.go`, `cmd/devcadence/benchmark_test.go`, `docs/evidence/`, docs
- risk tags: objective gate evaluation, falsification enforcement, decision durability, reporting clarity
- exact normative clauses: DCI-005 (facts vs hypotheses), DCI-014 (retrievable evidence), DCI-123 (deterministic authorization), docs/IMPLEMENTATION_PLAN.md § M4 Exit criterion
- initial evidence handles: `internal/benchmark/telemetry/aggregator.go`, `internal/benchmark/campaign/campaign_spec.go`
- deferred references: none
- assumptions: A1 AggregatedReport from telemetry is used as input. A2 Falsification results from campaign are available.
- re-resolution triggers: changes to Milestone M4 exit criteria in `docs/IMPLEMENTATION_PLAN.md`.

## Semantic scope envelope

### Authorized domains / path patterns

- `internal/benchmark/gate/gate_evaluator.go` (new)
- `internal/benchmark/gate/gate_decision.go` (new)
- `internal/benchmark/gate/gate_test.go` (new)
- `cmd/devcadence/benchmark.go` (new CLI command handler for `devcadence benchmark evaluate-gate`)
- `cmd/devcadence/benchmark_test.go` (new)
- `docs/evidence/m4-evidence-report.md` (generated report)
- `docs/evidence/m4-evidence-report.json` (generated report, JSON)
- `internal/benchmark/gate/gate_failclosed_test.go` (fail-closed and mutation tests)
- `docs/work-packages/wp-m4-6-ewp.md`

### Explicitly forbidden semantic changes

- No subjective overrides of mathematical gate criteria (if hypothesis is falsified, gate must not return `DecisionGo`).
- No suppression of accounting uncertainty flags in the final report.
- No network calls or non-local dependencies.

### LOCAL_DISCRETION

- Formatting of executive summary callout banners in the evidence report.
- Helper structs for criteria breakdown.
- CLI output styling.

## Requirements

| ID | Strength | Requirement | Normative source |
| --- | --- | --- | --- |
| REQ-01 | MUST | Gate Decision Enum: `type GateDecision string` (`DecisionGo="go"`, `DecisionRevise="revise"`, `DecisionInconclusive="inconclusive"`). Implement `Valid() bool`. | docs/IMPLEMENTATION_PLAN.md § M4 Exit criterion |
| REQ-02 | MUST | Gate Criteria Specification: Define `GateCriteria struct`: `MinCompletedRuns int` (default >= 10), `MinDefectCatchRate float64` (default >= 0.80), `MaxResourceRatioVersusBaseline float64` (default <= 1.0; Strategy 4 resource per accepted result must not exceed baseline Strategy 1), `RequireZeroFalsifications bool` (true: if delegation floor hypothesis is falsified on applicable tasks, gate fails). | docs/IMPLEMENTATION_PLAN.md § M4 |
| REQ-03 | MUST | Criterion Result: Define `type CriterionResult struct { Name string; Passed bool; Threshold float64; Observed float64; Details string }`. Define `type GateEvaluationResult struct { Decision GateDecision; CriteriaEvaluations []CriterionResult; Summary string; AggregatedReport telemetry.AggregatedReport; EvaluatedAt time.Time }`. | ADR-0024 |
| REQ-04 | MUST | Pure Gate Evaluator: `func EvaluateM4Gate(report *telemetry.AggregatedReport, falsifications map[string]*experiments.FalsificationResult, criteria GateCriteria) (*GateEvaluationResult, error)`. Pure function, validates non-nil inputs. Pairwise evaluates capability tiers: for each capability tier $C$ present, compares Strategy 4 vs Strategy 1. | DCI-123 |
| REQ-05 | MUST | Exit Criterion Enforcement: Returns `DecisionRevise` if Strategy 4 defect catch rate < 0.80 OR Strategy 4 resource-per-accepted > Strategy 1 baseline OR Strategy 4 has `ResourcePerAcceptedResult.IsUndefined == true` (0 accepted runs) OR any applicable delegation hypothesis is falsified. If baseline Strategy 1 has `IsUndefined == true` and Strategy 4 has accepted runs, resource efficiency PASSES. Returns `DecisionInconclusive` with `nil` error if completed runs < `MinCompletedRuns`. Returns `DecisionGo` if and only if all criteria PASS. | docs/IMPLEMENTATION_PLAN.md § Exit criterion |
| REQ-06 | MUST | Evidence Document Synthesizer: `func SynthesizeEvidenceReport(eval *GateEvaluationResult) (string, error)` produces a comprehensive GitHub-flavored Markdown document with executive summary, decision callout, criteria table, and recommendations for Milestone M5. Clearly discloses provisional token accounting. | docs/IMPLEMENTATION_PLAN.md § Measurements |
| REQ-07 | MUST | CLI Command Integration: In `cmd/devcadence/benchmark.go`, implement `devcadence benchmark evaluate-gate --snapshots <path> --output <path> [--json]`. Exit codes: 0 for DecisionGo, 1 for DecisionRevise, 2 for DecisionInconclusive. | docs/IMPLEMENTATION_PLAN.md § Exit criterion |
| REQ-08 | MUST | Stop Rules: Fails closed with `errs.CategoryInvalidArgument` on nil report or invalid criteria bounds. | DCI-104 |

## Invariants / state rules

| ID | Statement | Related requirements |
| --- | --- | --- |
| INV-01 | Gate evaluation is pure, deterministic, and strictly grounded in empirical evidence. | REQ-04, DCI-005 |
| INV-02 | A falsified delegation hypothesis, failing defect catch rate, or negative resource efficiency strictly precludes `DecisionGo`. | REQ-05 |
| INV-03 | Report output clearly discloses any provisional token accounting or uncalibrated endpoints. | REQ-06, DCI-005 |
| INV-04 | DecisionInconclusive returns `(*GateEvaluationResult, nil)` cleanly without error. | REQ-05 |

## Interface / algorithm contract

```text
EvaluateM4Gate(report, falsifications, criteria):
  1. Validate report != nil. If criteria.MinCompletedRuns < 1 -> return nil, errs.CategoryInvalidArgument.
  2. If len(report.Groups) == 0 || report.TotalSnapshots < criteria.MinCompletedRuns:
     return &GateEvaluationResult{Decision: DecisionInconclusive, Summary: "insufficient completed runs"}, nil
  3. Initialize CriteriaEvaluations slice.
  4. Pairwise Tier Evaluation:
     For each capability tier C in distinct tiers:
       s4Group := findGroup(report, StrategyHybrid4Layer, C)
       s1Group := findGroup(report, StrategyFullHistory, C)

       // a. Quality (Defect Catch Rate)
       if s4Group != nil && s4Group.DefectCatchRateApplicable:
         pass := s4Group.DefectCatchRate >= criteria.MinDefectCatchRate
         recordCriterion("defect_catch_rate_"+C, pass, criteria.MinDefectCatchRate, s4Group.DefectCatchRate)

       // b. Resource Efficiency
       if s4Group != nil && s1Group != nil:
         if s4Group.ResourcePerAcceptedResult.IsUndefined:
           recordCriterion("resource_efficiency_"+C, false, 1.0, 0, "Strategy 4 has 0 accepted runs")
         else if s1Group.ResourcePerAcceptedResult.IsUndefined:
           recordCriterion("resource_efficiency_"+C, true, 1.0, 0, "Strategy 4 accepted runs while baseline has 0")
         else:
           ratio := s4Group.ResourcePerAcceptedResult.Value / max(s1Group.ResourcePerAcceptedResult.Value, 1.0)
           pass := ratio <= criteria.MaxResourceRatioVersusBaseline
           recordCriterion("resource_efficiency_"+C, pass, criteria.MaxResourceRatioVersusBaseline, ratio)
  5. Delegation Floor Evaluation:
     For each key, fals := range falsifications:
       if fals.IsApplicable && fals.HypothesisFalsified:
         recordCriterion("delegation_floor_"+key, false, 0, 1, fals.Reason)
  6. Final Decision:
     If any criterion failed -> return DecisionRevise.
     Else -> return DecisionGo.
```

## Authority matrix

| Decision/effect | Authorized source | Forbidden substitute |
| --- | --- | --- |
| Gate decision | `EvaluateM4Gate` mathematical evaluation | Subjective product team consensus |
| Evidence report text | `SynthesizeEvidenceReport` | Hand-edited unverified markdown |

## Missing / unknown / stale input semantics

| Input/fact | Missing | Unknown | Stale | Malformed/contradictory |
| --- | --- | --- | --- | --- |
| AggregatedReport | fail closed (`errs.CategoryInvalidArgument`) | n/a | n/a | fail closed |
| Falsifications map | permitted (evaluated as empty) | n/a | n/a | fail closed |
| Baseline Resource Undefined | Pass if S4 accepted; fail if S4 undefined | n/a | n/a | fail closed |

## Failure matrix

| Failure point / condition | Required postcondition | Recovery / response | Evidence |
| --- | --- | --- | --- |
| Insufficient completed runs | Return `DecisionInconclusive, nil` | Clean structured result | `Result.Decision == DecisionInconclusive` |
| Hypotheses falsified | Return `DecisionRevise, nil` | Detailed failure reasons logged | `Result.Decision == DecisionRevise` |
| Invalid criteria | Return `nil, err` | CategoryInvalidArgument | `err != nil` |

## Representability map

| Concept / requirement | Exact representation | Adequacy | Action if inadequate |
| --- | --- | --- | --- |
| Gate decision | `type GateDecision string` | represented | — |
| Gate criteria | `type GateCriteria struct` | represented | — |
| Criterion result | `type CriterionResult struct` | represented | — |
| Evaluation result | `type GateEvaluationResult struct` | represented | — |

## Acceptance scenarios

| ID | Setup / precondition | Action | Expected result | Maps to |
| --- | --- | --- | --- | --- |
| ACC-01 | Campaign report where Strategy 4 achieves >= 80% catch rate, lower token ratio than baseline, and 0 falsifications | Call `EvaluateM4Gate()` | Returns `DecisionGo, nil` with all criteria marked passed | REQ-01, REQ-04, REQ-05 |
| ACC-02 | Campaign report where Strategy 4 consumes more resources than baseline Strategy 1 in a tier | Call `EvaluateM4Gate()` | Returns `DecisionRevise, nil` with resource efficiency failure | REQ-05, INV-02 |
| ACC-03 | Campaign report with an applicable falsified delegation hypothesis | Call `EvaluateM4Gate()` | Returns `DecisionRevise, nil` with delegation floor failure | REQ-05, INV-02 |
| ACC-04 | Report with fewer completed runs than `MinCompletedRuns` | Call `EvaluateM4Gate()` | Returns `DecisionInconclusive, nil` without error | REQ-05, INV-04 |
| ACC-05 | Completed evaluation result | Call `SynthesizeEvidenceReport()` | Emits valid Markdown containing decision badge, criteria table, and telemetry breakdown | REQ-06, INV-03 |
| ACC-06 | CLI invoked via `devcadence benchmark evaluate-gate` on DecisionGo report | Run CLI command | Exits 0, outputs synthesized report | REQ-07 |

## Validation

- command / deterministic check: `go test -v -race ./internal/benchmark/gate/... ./cmd/devcadence/...`
- command / deterministic check: `make verify`
- mutation testing: mutation review sufficient: adversarial catalog below

### Mutation Catalog (Mandatory)

| Mutant (Plausible Bug / Omission / Boundary Inversion) | Expected Test Failure (Scenario / Invariant Check) |
| --- | --- |
| Gate returns `DecisionGo` despite resource inefficiency | ACC-02 fails / decision assertion |
| Gate returns `DecisionGo` despite applicable falsification | ACC-03 fails / decision assertion |
| Gate returns `err != nil` instead of `DecisionInconclusive` on few runs | ACC-04 fails / err == nil assertion |
| Undefined resource efficiency silently evaluated as 0.0 allowing DecisionGo | Test fails on zero accepted S4 runs |
| Synthesizer omits uncertainty disclosure when report contains uncertain accounting | ACC-05 fails / uncertainty disclosure string check |

### Required Independent Review Lenses (Dual-Lens Review Pack)
1. **Contract & Authority Reviewer:**
   - Verifies compliance with REQ-01 through REQ-08.
   - Verifies that `DecisionGo` cannot be reached when invariants or falsification rules fail.
2. **Test Adequacy & Mutation Reviewer:**
   - Verifies scenario coverage (`ACC-01` through `ACC-06`).
   - Checks that all mutations in the Mutation Catalog are killed.

## Implementation Readiness Report

```text
requirements represented: 8/8
mandatory clauses resolved: 5/5
state transitions specified: 3/3
failure cases specified: 3/3
authority decisions specified: 2/2
missing/unknown input semantics: 3/3
acceptance scenarios mapped: 6/6
unresolved architecture choices: 0
declared local-discretion choices: 3
readiness: READY_FOR_IMPLEMENTATION
```

### Weaker-implementer check

Could a competent implementation model with good language/repository skill but mediocre architectural judgment execute this EWP without inventing important semantics?

- [x] Yes
- [ ] No — return to Principal design

## Amendment r3 (PR #75 review)

### Changelog

- r3: Authorized write domain extended to `docs/evidence/*.json`, `internal/benchmark/gate/gate_failclosed_test.go`. Added fields and semantics below (previously implemented but undeclared). Added evidence provenance and the synthetic-vs-empirical distinction. Added criteria validation (B3).

### Added representation

- `CriterionResult.ObservedUndefined bool` (`observed_undefined`): set when the observed value is undefined, missing or malformed; `Observed` is then 0, never +Inf/NaN (JSON-serializable).
- `GateEvaluationResult` gains `FalsificationResults` (`falsification_results`, preserves delegation-floor inputs for re-evaluation), `ReportDigest` (`sha256:` of the aggregated report JSON), `Recommendations`, `Provenance`, and `AggregatedReport` is a pointer (`*telemetry.AggregatedReport`), superseding REQ-03's value type.
- `GateCriteria` gains `MaxFalsificationRate` (default 0.0) and `MaxResidentContextRatioBaseline` (default 1.0; 0 disables the peak-resident check). `DecisionPivot` is an alias of `DecisionRevise`.

### Criteria validation (fail closed, `errs.CategoryInvalidArgument`, `nil` result)

MinCompletedRuns >= 1; MinDefectCatchRate finite in [0,1]; MaxResourceRatioVersusBaseline finite and > 0; MaxFalsificationRate finite in [0,1]; MaxResidentContextRatioBaseline finite and >= 0. A malformed bound must never silently disable a check (probe: Require=false, MaxFalsificationRate=-1, one falsified applicable entry => error, never Go).

### Fail-closed missing-input semantics

- Per tier present: missing Strategy 4 group, non-applicable or malformed Strategy 4 defect catch rate, missing Strategy 1 group, undefined/malformed resource values => FAILED criterion with `ObservedUndefined`, so `DecisionRevise`. Exception (unchanged): Strategy 1 undefined while Strategy 4 has accepted runs passes.
- Peak resident context: baseline `AvgPeakResidentTokens <= 0` or malformed => FAILED undefined criterion (never +Inf).
- No capability tiers, or tiers containing only Strategy 2/3 groups => `DecisionRevise` (Strategy 4 vs Strategy 1 comparison impossible).
- `len(Groups)==0 || TotalSnapshots < MinCompletedRuns` => `DecisionInconclusive`; `TotalSnapshots == MinCompletedRuns` is sufficient.
- Falsification semantics: only applicable entries count. With `RequireZeroFalsifications=true` each falsified applicable entry is a failed `delegation_floor_<key>` criterion. With false, per-task entries are informational (recorded as passed) and the aggregate `aggregate_falsification_rate` criterion (falsified/applicable <= MaxFalsificationRate) decides; criteria validation guarantees this aggregate check runs whenever totalApplicable > 0.

### Evidence provenance: synthetic versus empirical

`GateEvaluationResult.Provenance` (`evidence_kind`, `driver`, `source_commit`, `regeneration_command`, `statement`). Kinds: `synthetic_harness_validation` (scripted drivers; validates gate machinery only) and `empirical_campaign` (real endpoints). Unspecified provenance is treated as non-empirical. Only `empirical_campaign` may render empirical-evidence language; synthetic or unspecified evidence MUST render a non-proof statement and MUST NOT be presented as proof of the M4 product hypothesis. The CLI accepts `--evidence-kind/--driver/--source-commit/--regen-command` and prints the provenance. The committed `docs/evidence/m4-evidence-report.{md,json}` is `synthetic_harness_validation` (driver `canonicalTestDriver`): a GO on it validates the gate machinery. Completing the M4 product claim requires an `empirical_campaign` run on real endpoints through the same gate.
