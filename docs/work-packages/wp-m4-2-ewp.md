# WP-M4-2 — Delegation-Floor & Capability-Class Experiments

## Identity

- Work Package ID: WP-M4-2 (window 2026-10-D; slice 2 of Milestone M4)
- Revision: 2 (window review: complete struct definitions, explicit mathematical falsification criteria with non-zero baseline precondition, Err error field in run result, expanded mutation catalog)
- Task ID: task-m4-2-delegation-experiments
- Base commit: `a457e7d1e28f37c5055d1f51a4da45a4320749a7`
- Project state revision: 1
- Contract digest: n/a (Markdown contract authoritative)
- Target implementation endpoint/profile: competent Go implementer
- Status: READY_FOR_IMPLEMENTATION (r2)

## Objective

Deliver the capability-class and delegation-floor experiment framework in `internal/benchmark/experiments`. The framework tests the central ADR-0024 hypothesis:
*Does complete contract specification (zero implementation-critical ambiguity, fully enumerated invariant/failure/mutation tables) lower the delegation floor, enabling smaller or less expensive cognition capability classes to achieve first-pass acceptance without architectural quality loss?*

The package implements a structured experiment runner executing identical EWP contracts across distinct capability tiers, measuring first-pass acceptance rates, architectural review findings, Principal re-entry, and token expenditure.

## Context Manifest

- role: implementer (Go), independent reviewers
- read-authority envelope: `internal/protocol/portfolio.go`, `internal/cognition/drivers/driver.go`, `internal/cognition/drivers/types.go`, `internal/benchmark/types.go`, `docs/adr/0024-implementation-ready-work-packages-and-contract-completeness.md`, `docs/IMPLEMENTATION_PLAN.md` § M4
- semantic write/scope envelope: `internal/benchmark/experiments/` (experiment harness, capability classes, evaluation metrics), tests, docs
- risk tags: experimental validity, falsification criteria enforcement, capability tier normalization, non-leaky test scaffolding
- exact normative clauses: DCI-005 (distinguish facts from hypotheses), DCI-046/049 (independent review), DCI-123 (deterministic authorization), ADR-0024 §1-3 (implementation readiness & zero ambiguity)
- initial evidence handles: `internal/benchmark/types.go`, `internal/cognition/drivers/types.go`
- deferred references: none
- assumptions: A1 WP-M4-1 benchmark types are available. A2 Drivers or synthetic mocks represent distinct capability classes.
- re-resolution triggers: changes to EWP schema or capability class taxonomy.

## Semantic scope envelope

### Authorized domains / path patterns

- `internal/benchmark/experiments/capability_class.go` (new)
- `internal/benchmark/experiments/experiment.go` (new)
- `internal/benchmark/experiments/experiment_test.go` (new)
- `internal/benchmark/experiments/falsification.go` (new)
- `internal/benchmark/experiments/falsification_test.go` (new)
- `docs/work-packages/wp-m4-2-ewp.md`

### Explicitly forbidden semantic changes

- No subjective or self-reported pass/fail evaluations.
- No relaxation of verification gates for weaker capability tiers.
- No vendor-specific SDK branching or unmediated process execution.

### LOCAL_DISCRETION

- Experiment configuration serialization (JSON/YAML).
- Batch experiment execution concurrency control.
- Internal result accumulation data structures.

## Requirements

| ID | Strength | Requirement | Normative source |
| --- | --- | --- | --- |
| REQ-01 | MUST | Define `CapabilityClass` enum: `CapabilityLocalSmall` ("local_small"), `CapabilitySubscriptionCLI` ("subscription_cli"), `CapabilityFrontierAPI` ("frontier_api"). Implement `Valid() bool`. | docs/IMPLEMENTATION_PLAN.md § M4 |
| REQ-02 | MUST | Define `ContractCompletenessLevel` enum: `ContractBaseline` ("baseline_incomplete"), `ContractImplementationReady` ("implementation_ready"). Implement `Valid() bool`. | ADR-0024 §1 |
| REQ-03 | MUST | Define `ExperimentSpec`: `ExperimentID string`, `TaskID string`, `ContractLevel ContractCompletenessLevel`, `TargetCapability CapabilityClass`, `Repetitions int`, `MaxRepairRounds int`. Implement `Validate() error` requiring `Repetitions >= 1`, `MaxRepairRounds >= 0`, and valid enums. | docs/IMPLEMENTATION_PLAN.md § M4 |
| REQ-04 | MUST | Define `ExperimentRunResult`: `RunID string`, `ExperimentID string`, `Capability CapabilityClass`, `ContractLevel ContractCompletenessLevel`, `PassedFirstPass bool`, `TotalRepairRounds int`, `ArchitecturalFindingsCount int`, `PrincipalReentryRequired bool`, `TokensConsumed int64`, `Duration time.Duration`, `Err error`. | ADR-0024 § Weaker-implementer check |
| REQ-05 | MUST | Define `ExperimentSummary`: `ExperimentID string`, `Capability CapabilityClass`, `ContractLevel ContractCompletenessLevel`, `Repetitions int`, `FirstPassRate float64`, `AvgRepairRounds float64`, `AvgArchFindings float64`, `TotalTokensConsumed int64`, `Runs []ExperimentRunResult`. | ADR-0024 |
| REQ-06 | MUST | Delegation-Floor Runner: `ExperimentRunner.Run(ctx context.Context, spec ExperimentSpec, driver drivers.SessionDriver) (*ExperimentSummary, error)`. Executes repetitions, collects results, and calculates aggregated metrics. | ADR-0024 |
| REQ-07 | MUST | Define `FalsificationResult`: `HypothesisFalsified bool`, `Reason string`, `BaselineSummary ExperimentSummary`, `ReadySummary ExperimentSummary`, `DeltaFirstPassRate float64`, `DeltaRepairRounds float64`, `DeltaArchFindings float64`, `IsApplicable bool`. | docs/IMPLEMENTATION_PLAN.md § M4 |
| REQ-08 | MUST | Mathematical Falsification Evaluator: `EvaluateFalsification(baseline, ready *ExperimentSummary) (*FalsificationResult, error)`. Validates non-nil summaries. If baseline exhibited no repairs or defects (`baseline.AvgRepairRounds == 0 && baseline.FirstPassRate == 1.0`), returns `IsApplicable: false` (not falsified: task ceiling saturated). Otherwise, calculates deltas. If `ready.AvgRepairRounds >= baseline.AvgRepairRounds` OR `ready.AvgArchFindings >= baseline.AvgArchFindings` OR `ready.FirstPassRate <= baseline.FirstPassRate` (margin >= 0.05 when Repetitions >= 3), returns `HypothesisFalsified: true, IsApplicable: true`. | docs/IMPLEMENTATION_PLAN.md § M4 |
| REQ-09 | MUST | Deterministic Review Emulation: Repair rounds and architectural findings must be computed from deterministic review passes (Contract Reviewer, Mutation Reviewer), not ad-hoc heuristics. | DCI-046, DCI-049 |
| REQ-10 | MUST | Stop Rules: If `spec.Repetitions < 1` or `driver == nil`, return `errs.CategoryInvalidArgument`. If `ctx.Err() != nil`, terminate driver session cleanly and return `ctx.Err()`. | DCI-104 |

## Invariants / state rules

| ID | Statement | Related requirements |
| --- | --- | --- |
| INV-01 | Verification gates must be identical across all capability tiers; no relaxation for weaker models. | REQ-06, DCI-123 |
| INV-02 | Falsification evaluation is pure, deterministic, and immutable. | REQ-07, REQ-08 |
| INV-03 | Experiment results report observable counts and facts; uncertainty is flagged explicitly. | REQ-04, REQ-05, DCI-005 |
| INV-04 | Tasks that pass trivially on baseline with 0 repairs cannot falsify the hypothesis (`IsApplicable = false`). | REQ-08 |

## Interface / algorithm contract

```text
ExperimentRunner.Run(ctx, spec, driver):
  1. Validate spec (Repetitions >= 1, MaxRepairRounds >= 0, valid enums) and driver != nil.
  2. If ctx.Err() != nil -> return nil, ctx.Err().
  3. For rep := 0; rep < spec.Repetitions; rep++:
     a. If ctx.Err() != nil -> return nil, ctx.Err().
     b. Load EWP according to spec.ContractLevel (Baseline vs. ImplementationReady).
     c. Start driver session: session, err := driver.StartSession(ctx, cfg).
        defer session.Close(context.Background())
     d. Execute implementation turn with driver.
     e. Run deterministic verification suite (ACC scenarios).
     f. Run independent review lenses (Contract & Mutation).
     g. If failures found:
        - While repair rounds < spec.MaxRepairRounds and tests fail:
          - Execute repair turn; increment repair rounds; repeat check.
     h. Record RunResult:
        - PassedFirstPass = (repairRounds == 0 && testPassed)
        - TotalRepairRounds = repairRounds
        - ArchitecturalFindingsCount = findings
     i. session.Close(context.Background())
  4. Aggregate metrics across runs:
     - FirstPassRate = count(PassedFirstPass) / float64(repetitions)
     - AvgRepairRounds = sum(TotalRepairRounds) / float64(repetitions)
     - AvgArchFindings = sum(ArchitecturalFindingsCount) / float64(repetitions)
  5. Return ExperimentSummary.

EvaluateFalsification(baseline, ready):
  1. Validate baseline != nil && ready != nil.
  2. deltaFirstPass = ready.FirstPassRate - baseline.FirstPassRate
  3. deltaRepairs = ready.AvgRepairRounds - baseline.AvgRepairRounds
  4. deltaFindings = ready.AvgArchFindings - baseline.AvgArchFindings
  5. If baseline.AvgRepairRounds == 0 && baseline.FirstPassRate == 1.0:
     return &FalsificationResult{IsApplicable: false, HypothesisFalsified: false, Reason: "baseline task exhibited zero defects; cannot test delegation floor"}
  6. If deltaRepairs >= 0 || deltaFindings >= 0 || deltaFirstPass <= 0:
     return &FalsificationResult{IsApplicable: true, HypothesisFalsified: true, Reason: "implementation-ready contract failed to reduce repair rounds or improve first-pass acceptance"}
  7. Return &FalsificationResult{IsApplicable: true, HypothesisFalsified: false, Reason: "hypothesis supported: implementation-ready contract lowered delegation floor"}
```

## Authority matrix

| Decision/effect | Authorized source | Forbidden substitute |
| --- | --- | --- |
| Falsification determination | Mathematical comparison in `EvaluateFalsification` | Qualitative subjective interpretation |
| Acceptance status | Deterministic test exit code | Driver or model completion claim |

## Missing / unknown / stale input semantics

| Input/fact | Missing | Unknown | Stale | Malformed/contradictory |
| --- | --- | --- | --- | --- |
| CapabilityClass | fail closed (`errs.CategoryInvalidArgument`) | fail closed | n/a | fail closed |
| ContractLevel | fail closed (`errs.CategoryInvalidArgument`) | fail closed | n/a | fail closed |
| Repetitions | fail closed if < 1 | n/a | n/a | fail closed |
| Baseline/Ready in Evaluator | fail closed (`errs.CategoryInvalidArgument`) | n/a | n/a | fail closed |

## Failure matrix

| Failure point / condition | Required postcondition | Recovery / response | Evidence |
| --- | --- | --- | --- |
| Context canceled during run | Ongoing turn canceled, driver closed | Return `ctx.Err()` unwrapped | `ctx.Err() != nil` |
| Driver error during repetition | Record repetition as failed, do not crash runner | Continue or return structured summary | `RunResult.Err != nil` |
| Nil baseline or ready summary | No panic | Return `errs.CategoryInvalidArgument` | `err != nil` |

## Representability map

| Concept / requirement | Exact representation | Adequacy | Action if inadequate |
| --- | --- | --- | --- |
| Capability class | `type CapabilityClass string` | represented | — |
| Contract completeness | `type ContractCompletenessLevel string` | represented | — |
| Experiment summary | `type ExperimentSummary struct` | represented | — |
| Falsification result | `type FalsificationResult struct` | represented | — |

## Acceptance scenarios

| ID | Setup / precondition | Action | Expected result | Maps to |
| --- | --- | --- | --- | --- |
| ACC-01 | Baseline (incomplete) vs. Ready (complete) specs for `CapabilityLocalSmall`; mock driver configured with defect likelihood on ambiguous contracts | Run experiment comparison | Ready contract achieves `FirstPassRate > Baseline` and `AvgRepairRounds < Baseline` | REQ-01, REQ-04, REQ-05, REQ-06 |
| ACC-02 | Experiment summaries where ready contract exhibits higher repair rounds than baseline ($N \ge 3$) | Run `EvaluateFalsification` | Returns `HypothesisFalsified: true, IsApplicable: true` with explicit rationale | REQ-07, REQ-08, INV-02 |
| ACC-03 | Baseline summary with 0 repair rounds and 100% first pass (saturated task) | Run `EvaluateFalsification` | Returns `IsApplicable: false, HypothesisFalsified: false` | REQ-08, INV-04 |
| ACC-04 | Context canceled after 1 repetition | Run experiment with 5 repetitions | Cancels promptly, cleans up driver session, returns `ctx.Err()` | REQ-10 |

## Validation

- command / deterministic check: `go test -v -race ./internal/benchmark/experiments/...`
- command / deterministic check: `make verify`
- mutation testing: mutation review sufficient: adversarial catalog below

### Mutation Catalog (Mandatory)

| Mutant (Plausible Bug / Omission / Boundary Inversion) | Expected Test Failure (Scenario / Invariant Check) |
| --- | --- |
| Falsification inverted (returns false when ready repairs >= baseline) | ACC-02 fails / assertion on HypothesisFalsified |
| Saturated baseline (0 repairs) treated as falsifying hypothesis ($0 \ge 0$) | ACC-03 fails / assertion on IsApplicable |
| First-pass check off-by-one: marks true if `repairRounds <= 1` | Test fails on run with 1 repair round asserting `PassedFirstPass == false` |
| Verification gate lowered for `CapabilityLocalSmall` | Test checks parity of acceptance criteria across tiers |
| Max repair rounds not enforced, looping indefinitely | Runner loop bounds test fails on max rounds exceeded |
| Runner fails to close session between repetitions | Session cleanup test fails / resource leak |

### Required Independent Review Lenses (Dual-Lens Review Pack)
1. **Contract & Authority Reviewer:**
   - Verifies compliance with `REQ-*` and `INV-*`.
   - Ensures no special relaxation or bypass for smaller model tiers.
   - Verifies strict error handling on invalid specifications.
2. **Test Adequacy & Mutation Reviewer:**
   - Verifies that `ACC-01` through `ACC-04` are exercised.
   - Validates that `EvaluateFalsification` edge cases are killed by unit tests.

## Implementation Readiness Report

```text
requirements represented: 10/10
mandatory clauses resolved: 5/5
state transitions specified: 4/4
failure cases specified: 3/3
authority decisions specified: 2/2
missing/unknown input semantics: 4/4
acceptance scenarios mapped: 4/4
unresolved architecture choices: 0
declared local-discretion choices: 3
readiness: READY_FOR_IMPLEMENTATION
```

### Weaker-implementer check

Could a competent implementation model with good language/repository skill but mediocre architectural judgment execute this EWP without inventing important semantics?

- [x] Yes
- [ ] No — return to Principal design
