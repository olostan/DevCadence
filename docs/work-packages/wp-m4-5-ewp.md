# WP-M4-5 — Multi-Strategy Matrix & Delegation-Floor Campaign Execution

## Identity

- Work Package ID: WP-M4-5 (window 2026-10-E; slice 5 of Milestone M4)
- Revision: 2 (window review: CorpusRegistry parameter in CampaignSpec, composite map keys, RunTelemetrySnapshot status representation, mutex synchronization, cancellation semantics, expanded mutation catalog)
- Task ID: task-m4-5-campaign-execution
- Base commit: `0c3bce20d89010d90bfec93540541effc4591b7c`
- Project state revision: 1
- Contract digest: n/a (Markdown contract authoritative)
- Target implementation endpoint/profile: competent Go implementer
- Status: READY_FOR_IMPLEMENTATION (r2)

## Objective

Deliver the automated multi-strategy and delegation-floor evaluation campaign engine in `internal/benchmark/campaign`. The engine orchestrates the full evaluation matrix:
- 10 Benchmark Tasks (from `internal/benchmark/corpus`)
- 4 Context Strategies (`full_history`, `compacted`, `snippet_pool`, `hybrid_4layer` via `internal/benchmark`)
- 3 Capability Classes (`CapabilityLocalSmall`, `CapabilitySubscriptionCLI`, `CapabilityFrontierAPI` via `internal/benchmark/experiments`)
- 2 Contract Completeness Levels (`ContractBaseline`, `ContractImplementationReady` per ADR-0024)

The campaign engine executes matrix permutations, collects run telemetry snapshots into `internal/benchmark/telemetry`, evaluates hypothesis falsification, and outputs comprehensive run manifests.

## Context Manifest

- role: implementer (Go), independent reviewers
- read-authority envelope: `internal/benchmark/types.go`, `internal/benchmark/runner.go`, `internal/benchmark/experiments/capability_class.go`, `internal/benchmark/experiments/experiment.go`, `internal/benchmark/experiments/falsification.go`, `internal/benchmark/telemetry/metrics.go`, `internal/benchmark/telemetry/collector.go`, `internal/benchmark/corpus/task_registry.go`, `docs/IMPLEMENTATION_PLAN.md` § M4
- semantic write/scope envelope: `internal/benchmark/campaign/` (campaign runner, matrix orchestrator, spec, tests), docs
- risk tags: combinatorial explosion control, concurrency safety, clean failure isolation, deterministic aggregation
- exact normative clauses: DCI-005 (facts vs hypotheses), DCI-054/055 (abstract drivers), DCI-123 (deterministic gates), ADR-0024
- initial evidence handles: `internal/benchmark/runner.go`, `internal/benchmark/experiments/experiment.go`, `internal/benchmark/telemetry/collector.go`
- deferred references: none
- assumptions: A1 Session drivers or mock drivers are injected for each capability class via DriverProvider. A2 TelemetryCollector receives all run snapshots.
- re-resolution triggers: changes to `benchmark.BenchmarkRunResult` or `telemetry.RunTelemetrySnapshot`.

## Semantic scope envelope

### Authorized domains / path patterns

- `internal/benchmark/campaign/campaign_spec.go` (new)
- `internal/benchmark/campaign/campaign_runner.go` (new)
- `internal/benchmark/campaign/campaign_test.go` (new)
- `docs/work-packages/wp-m4-5-ewp.md`

### Explicitly forbidden semantic changes

- No skipping of failed matrix runs without recording failures in telemetry.
- No alteration of verification suites across capability classes (strict tier parity per INV-02).
- No uncoordinated concurrency leaks or unbuffered thread spawning.

### LOCAL_DISCRETION

- Worker concurrency pool size (default bounded to runtime CPU count).
- Matrix permutation order (executed deterministically).
- Temporary directory structure for isolated campaign runs.

## Requirements

| ID | Strength | Requirement | Normative source |
| --- | --- | --- | --- |
| REQ-01 | MUST | Campaign Specification API: `type CampaignSpec struct { CampaignID string; Corpus *corpus.CorpusRegistry; Tasks []benchmark.BenchmarkTask; Strategies []benchmark.ContextStrategyKind; Capabilities []experiments.CapabilityClass; Repetitions int; MaxRepairRounds int; ConcurrencyLimit int }`. Implement `Validate() error` (requires `CampaignID` non-empty, non-nil `Corpus`, non-empty tasks/strategies/capabilities, `Repetitions >= 1`, `MaxRepairRounds >= 0`, `ConcurrencyLimit >= 1`). | docs/IMPLEMENTATION_PLAN.md § M4 |
| REQ-02 | MUST | Driver Provider API: `type DriverProvider interface { GetDriver(cap experiments.CapabilityClass) (drivers.SessionDriver, error) }`. Fails closed if capability is unsupported or driver is unavailable. | DCI-054/055 |
| REQ-03 | MUST | Campaign Result API: `type CampaignSummary struct { CampaignID string; TotalRuns int; CompletedRuns int; FailedRuns int; Snapshots []telemetry.RunTelemetrySnapshot; DelegationSummaries map[string]*experiments.ExperimentSummary; FalsificationResults map[string]*experiments.FalsificationResult; Duration time.Duration }`. Map keys use canonical composite key: `fmt.Sprintf("%s:%s", task.TaskID, cap)`. | ADR-0024 |
| REQ-04 | MUST | Campaign Orchestrator: `type CampaignRunner struct`; `func NewCampaignRunner(dp DriverProvider, collector *telemetry.TelemetryCollector) *CampaignRunner`; `func (r *CampaignRunner) Execute(ctx context.Context, spec CampaignSpec) (*CampaignSummary, error)`. Uses internal mutex synchronization for thread-safe accumulation when `ConcurrencyLimit > 1`. | docs/IMPLEMENTATION_PLAN.md § M4 |
| REQ-05 | MUST | Seeded Defect Injection: For each task in `spec.Tasks`, queries associated defects via `spec.Corpus.GetDefectsForTask(task.TaskID)`. Runs clean baseline (defect = nil) and defect runs for each seeded defect. | docs/IMPLEMENTATION_PLAN.md § M4 |
| REQ-06 | MUST | Snapshot Conversion: Runner converts `benchmark.BenchmarkRunResult` into `telemetry.RunTelemetrySnapshot` faithfully, mapping all `int64` token counts, defect statuses, uncertainty flags, and durations without data loss. If a run fails, records `Accepted: false`, `DefectStatus: string(benchmark.DefectNotApplicable)`. | DCI-005 |
| REQ-07 | MUST | Delegation Floor Evaluation: For each task and capability class, runner executes both `ContractBaseline` and `ContractImplementationReady`, computes `experiments.EvaluateFalsification`, and records the result in `FalsificationResults[fmt.Sprintf("%s:%s", task.TaskID, cap)]`. | ADR-0024 § Falsification |
| REQ-08 | MUST | Failure Resilience & Cancellation: A failed run on one permutation records failure into telemetry and continues remaining runs. If `ctx.Done()` triggers mid-campaign, runner ceases spawning new runs, awaits in-flight workers, and returns partial summary with `ctx.Err()`. | DCI-104 |

## Invariants / state rules

| ID | Statement | Related requirements |
| --- | --- | --- |
| INV-01 | Matrix permutations execute in isolated sandboxes with zero state leakage across runs. | REQ-04, REQ-08 |
| INV-02 | Verification gates remain strictly identical across all capability tiers. | REQ-07, DCI-123 |
| INV-03 | Campaign execution is deterministic for a given spec, task corpus, and driver configuration. | REQ-01, REQ-04 |
| INV-04 | `CampaignSummary` fields and maps are thread-safe under concurrent execution. | REQ-03, REQ-04 |

## Interface / algorithm contract

```text
CampaignRunner.Execute(ctx, spec):
  1. Validate spec (non-nil Corpus, non-empty fields, positive bounds).
  2. If ctx.Err() != nil -> return nil, ctx.Err().
  3. Initialize CampaignSummary with CampaignID and start time.
  4. For each task in spec.Tasks:
     For each cap in spec.Capabilities:
       driver, err := driverProvider.GetDriver(cap)
       If err != nil -> record failed run in telemetry; continue.
       defects := spec.Corpus.GetDefectsForTask(task.TaskID)
       // Run clean and seeded defect permutations across spec.Strategies
       For each strategy in spec.Strategies:
         For each defect in append([]*SeededDefect{nil}, defects...):
           For rep := 0; rep < spec.Repetitions; rep++:
             If ctx.Err() != nil -> return partialSummary, ctx.Err()
             res := runTask(ctx, task, strategy, driver, defect)
             snap := convertToSnapshot(res, cap)
             collector.RecordSnapshot(snap)
             mutex.Lock(); summary.CompletedRuns++; mutex.Unlock()
       // Run delegation experiment
       delKey := fmt.Sprintf("%s:%s", task.TaskID, cap)
       baseSum, readySum := runDelegationExperiment(ctx, task, cap, driver)
       falsResult, _ := experiments.EvaluateFalsification(baseSum, readySum)
       mutex.Lock()
       summary.DelegationSummaries[delKey] = readySum
       summary.FalsificationResults[delKey] = falsResult
       mutex.Unlock()
  5. Aggregate final CampaignSummary with snapshots from collector.
  6. Return CampaignSummary, nil.
```

## Authority matrix

| Decision/effect | Authorized source | Forbidden substitute |
| --- | --- | --- |
| Driver selection | `DriverProvider.GetDriver` | Hardcoded driver instantiation |
| Falsification evaluation | `experiments.EvaluateFalsification` | Qualitative subjective claims |

## Missing / unknown / stale input semantics

| Input/fact | Missing | Unknown | Stale | Malformed/contradictory |
| --- | --- | --- | --- | --- |
| CampaignSpec | fail closed (`errs.CategoryInvalidArgument`) | fail closed | n/a | fail closed |
| DriverProvider | fail closed (`errs.CategoryInvalidArgument`) | fail closed | n/a | fail closed |
| Driver for Capability | fail run, record error in snapshot | fail run | n/a | fail closed |

## Failure matrix

| Failure point / condition | Required postcondition | Recovery / response | Evidence |
| --- | --- | --- | --- |
| Context canceled during campaign | In-flight turns finished, partial summary returned with ctx.Err() | Return `(summary, ctx.Err())` | `ctx.Err() != nil` |
| Driver unavailable for tier | Runs for that tier marked failed | Continue remaining tiers | `Summary.FailedRuns > 0` |
| Individual run error | Error snapshot recorded | Continue matrix execution | `Snapshot.Accepted == false` |

## Representability map

| Concept / requirement | Exact representation | Adequacy | Action if inadequate |
| --- | --- | --- | --- |
| Campaign spec | `type CampaignSpec struct` | represented | — |
| Campaign summary | `type CampaignSummary struct` | represented | — |
| Driver provider | `type DriverProvider interface` | represented | — |

## Acceptance scenarios

| ID | Setup / precondition | Action | Expected result | Maps to |
| --- | --- | --- | --- | --- |
| ACC-01 | CampaignSpec configured with 2 tasks, 4 strategies, 2 capabilities | Call `Execute()` with mock driver provider | Executes permutations; all snapshots recorded in collector; `CompletedRuns > 0` | REQ-01, REQ-04, REQ-06 |
| ACC-02 | Campaign includes delegation experiment | Call `Execute()` | Computes baseline vs ready and populates `FalsificationResults` with composite key `task:cap` | REQ-03, REQ-07, INV-02 |
| ACC-03 | Mock driver returns error for one capability tier | Call `Execute()` | Continues execution; records failed runs in `FailedRuns`; does not panic | REQ-08 |
| ACC-04 | Context canceled after first permutation | Call `Execute()` | Stops prompt execution; returns partial summary and `ctx.Err()` | REQ-08 |

## Validation

- command / deterministic check: `go test -v -race ./internal/benchmark/campaign/...`
- command / deterministic check: `make verify`
- mutation testing: mutation review sufficient: adversarial catalog below

### Mutation Catalog (Mandatory)

| Mutant (Plausible Bug / Omission / Boundary Inversion) | Expected Test Failure (Scenario / Invariant Check) |
| --- | --- |
| Failed run aborts entire campaign without recording snapshot | ACC-03 fails / snapshot count assertion |
| Token metrics dropped or truncated during snapshot conversion | ACC-01 fails / snapshot token verification |
| Map key collision: overwrites FalsificationResults across capabilities | ACC-02 fails / length equals len(tasks)*len(caps) |
| Concurrent writes to CampaignSummary race under ConcurrencyLimit > 1 | Race detector fails under -race test |
| Runner ignores context cancellation and finishes all permutations | ACC-04 fails / prompt return of ctx.Err() |

### Required Independent Review Lenses (Dual-Lens Review Pack)
1. **Contract & Authority Reviewer:**
   - Verifies compliance with REQ-01 through REQ-08.
   - Verifies tier parity and failure isolation.
2. **Test Adequacy & Mutation Reviewer:**
   - Verifies scenario coverage (`ACC-01` through `ACC-04`).
   - Checks that all mutations in the Mutation Catalog are killed.

## Implementation Readiness Report

```text
requirements represented: 8/8
mandatory clauses resolved: 4/4
state transitions specified: 3/3
failure cases specified: 3/3
authority decisions specified: 2/2
missing/unknown input semantics: 3/3
acceptance scenarios mapped: 4/4
unresolved architecture choices: 0
declared local-discretion choices: 3
readiness: READY_FOR_IMPLEMENTATION
```

### Weaker-implementer check

Could a competent implementation model with good language/repository skill but mediocre architectural judgment execute this EWP without inventing important semantics?

- [x] Yes
- [ ] No — return to Principal design
