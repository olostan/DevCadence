# WP-M4-1 — Empirical Benchmark Harness & Seeded Defect Corpus

## Identity

- Work Package ID: WP-M4-1 (window 2026-10-D; slice 1 of Milestone M4)
- Revision: 2 (window review: complete struct definitions, uniform accounting uncertainty flag, explicit defect categories/statuses, failure matrix additions, unambiguous acceptance scenarios, expanded mutation catalog)
- Task ID: task-m4-1-benchmark-harness
- Base commit: `a457e7d1e28f37c5055d1f51a4da45a4320749a7`
- Project state revision: 1
- Contract digest: n/a (Markdown contract authoritative)
- Target implementation endpoint/profile: competent Go implementer
- Status: READY_FOR_IMPLEMENTATION (r2)

## Objective

Deliver the core empirical benchmark execution harness in `internal/benchmark` and evaluation suites in `tests/benchmark`. The harness executes representative engineering tasks across four distinct context strategies:
1. Strategy 1: Full Conversational History Baseline (traditional agent loop)
2. Strategy 2: Multi-Tier Context Compaction Baseline (ADR-0016)
3. Strategy 3: Static Prefix + Active Snippet Pool
4. Strategy 4: Hybrid 4-Layer Context (Protected Core, Cognitive State Capsule, Evidence Working Set, Ephemeral Tail per ADR-0019/ADR-0020)

The harness measures task completion, token consumption, and ability to detect or prevent a standardized suite of seeded defects spanning architectural invariants, API mutations, and boundary violations.

## Context Manifest

- role: implementer (Go), independent reviewers
- read-authority envelope: `internal/protocol/context.go`, `internal/protocol/portfolio.go`, `internal/cognition/compiler/compile.go`, `internal/cognition/drivers/driver.go`, `internal/cognition/drivers/types.go`, `docs/IMPLEMENTATION_PLAN.md` § M4, `docs/adr/0019-non-conversational-cognition-and-adaptive-review.md`, `docs/adr/0020-cognitive-invocation-compiler-and-review-ledger.md`
- semantic write/scope envelope: `internal/benchmark/` (core types, runner, strategies, defects), `tests/benchmark/` (integration harness tests), docs
- risk tags: benchmark reproducibility, driver session isolation, context strategy fidelity, token accounting precision
- exact normative clauses: DCI-005 (distinguish facts from hypotheses), DCI-014 (evidence is progressively retrievable), DCI-018 (authority does not imply residency), DCI-019 (delegation has no hidden requirements), DCI-054/055 (no provider coupling), DCI-123 (deterministic authorization)
- initial evidence handles: `internal/cognition/compiler/compile.go`, `internal/cognition/drivers/driver.go`
- deferred references: none
- assumptions: A1 Session drivers implement `drivers.SessionDriver` and report token usage in `TurnResult`. A2 Compiler provides deterministic prompt compilation for Strategy 4.
- re-resolution triggers: changes to `protocol.ContextPack` or `drivers.SessionDriver` interface.

## Semantic scope envelope

### Authorized domains / path patterns

- `internal/benchmark/types.go` (new)
- `internal/benchmark/strategy.go` (new)
- `internal/benchmark/defects.go` (new)
- `internal/benchmark/runner.go` (new)
- `internal/benchmark/runner_test.go` (new)
- `internal/benchmark/defects_test.go` (new)
- `tests/benchmark/benchmark_test.go` (new)
- `docs/work-packages/wp-m4-1-ewp.md`

### Explicitly forbidden semantic changes

- No direct import of third-party model vendor SDKs (must use `drivers.SessionDriver`).
- No modification of production compiler behavior to cater to benchmark tests.
- No non-deterministic test assertions or unseeded random state.

### LOCAL_DISCRETION

- Internal prompt construction templates for baseline strategies (Strategies 1–3).
- Harness concurrency workers and channel buffer sizes.
- Unit test helper fixtures and synthetic driver mocks.

## Requirements

| ID | Strength | Requirement | Normative source |
| --- | --- | --- | --- |
| REQ-01 | MUST | Define `ContextStrategyKind` enum: `StrategyFullHistory` ("full_history"), `StrategyCompacted` ("compacted"), `StrategySnippetPool` ("snippet_pool"), `StrategyHybrid4Layer` ("hybrid_4layer"). Implement `Valid() bool`. | ADR-0019 §1 |
| REQ-02 | MUST | Define `BenchmarkTask` struct: `TaskID string`, `Name string`, `WorkPackageID string`, `Contract string`, `ReadFiles []string`, `TargetFiles []string`, `ExpectedMutations []string`. Implement `Digest() string` computing deterministic sha256 hex of fields. | ADR-0024 |
| REQ-03 | MUST | Define `DefectCategory` enum: `DefectInvariantViolation` ("invariant_violation"), `DefectAPIMutation` ("api_mutation"), `DefectBoundaryViolation` ("boundary_violation"). Implement `Valid() bool`. Define `SeededDefect` struct: `DefectID string`, `Category DefectCategory`, `Description string`, `FileTarget string`, `PatchContent string`, `ViolatedInvariant string`. | docs/IMPLEMENTATION_PLAN.md § M4 |
| REQ-04 | MUST | Define `BenchmarkTurnRecord` struct: `Turn int`, `Prompt string`, `Result drivers.TurnResult`. Define `BenchmarkSession` struct: `SessionID string`, `Task BenchmarkTask`, `Strategy ContextStrategyKind`, `Driver drivers.Session`, `Defect *SeededDefect`, `Turns []BenchmarkTurnRecord`, `WorktreeDir string`. | ADR-0019 / ADR-0020 |
| REQ-05 | MUST | Define `StrategyContextBuilder` interface: `BuildTurnPrompt(ctx context.Context, session *BenchmarkSession, turn int, lastResult *drivers.TurnResult) (string, error)`. Provide concrete implementations for all four strategies with explicit config structs (`CompactedStrategyConfig{MaxHistoryTurns: int}`, `SnippetPoolStrategyConfig{MaxSnippetTokens: int}`). | ADR-0019 / ADR-0020 |
| REQ-06 | MUST | Strategy 4 Implementation: Uses `compiler.Compiler` to compile `CompiledInvocation` on each turn with an active lease set and cognitive state capsule, rejecting unfit contexts fail-closed with `errs.CategoryContextUnfit`. | ADR-0020 §2 |
| REQ-07 | MUST | Strategy 1 Implementation: Monotonically appends user prompts and assistant responses verbatim into a single conversational history. | ADR-0019 § Context |
| REQ-08 | MUST | Strategy 2 Implementation: Applies sliding-window turn compaction when history exceeds `MaxHistoryTurns` threshold, preserving role/contract core prefix. | ADR-0016 |
| REQ-09 | MUST | Strategy 3 Implementation: Retains static system prefix plus dynamically selected active file/symbol snippets bounded by token budget. | docs/IMPLEMENTATION_PLAN.md § M4 |
| REQ-10 | MUST | Define `RunStatus` enum: `RunStatusCompleted` ("completed"), `RunStatusFailed` ("failed"), `RunStatusContextUnfit` ("context_unfit"). Define `BenchmarkRunResult` struct: `RunID string`, `TaskID string`, `Strategy ContextStrategyKind`, `Status RunStatus`, `Passed bool`, `TurnsExecuted int`, `DefectStatus DefectStatus`, `TokenUsage drivers.TokenUsage`, `InitialTokens int64`, `PeakResidentTokens int64`, `CumulativeInputTokens int64`, `Duration time.Duration`, `AccountingUncertain bool`, `Err error`. | docs/IMPLEMENTATION_PLAN.md § M4 |
| REQ-11 | MUST | Define `DefectStatus` enum: `DefectDetected` ("detected"), `DefectPrevented` ("prevented"), `DefectMissed` ("missed"), `DefectIntroduced` ("introduced"), `DefectNotApplicable` ("not_applicable"). Implement `Valid() bool`. Runner evaluates defect outcome: `Prevented` if compiler/validator rejects upfront; `Detected` if test suite fails; `Missed` if tests pass despite active defect; `NotApplicable` if no defect seeded. | docs/IMPLEMENTATION_PLAN.md § M4 |

## Invariants / state rules

| ID | Statement | Related requirements |
| --- | --- | --- |
| INV-01 | Benchmark tasks and seeded defects are immutable and deterministic. | REQ-02, REQ-03 |
| INV-02 | Each strategy execution runs in an isolated `drivers.Session` with clean state and guaranteed cleanup via defer. | REQ-04, REQ-10 |
| INV-03 | Strategy 4 MUST NOT bypass compiler admission or profile budget checks. | REQ-06 |
| INV-04 | Defect status evaluation must be based on deterministic verification (validation exit code, test pass/fail, or AST/diff check), never self-assessment. | REQ-11, DCI-123 |
| INV-05 | Token accounting unobservability from driver sets `AccountingUncertain = true`. | REQ-10, DCI-005 |

## Interface / algorithm contract

```text
BenchmarkRunner.RunTask(ctx, task, strategyKind, driver, defect):
  1. Validate non-nil arguments: task != nil, driver != nil, strategyKind.Valid().
  2. If ctx.Err() != nil -> return nil, ctx.Err().
  3. Instantiate StrategyContextBuilder for strategyKind.
  4. Start new driver session: session, err := driver.StartSession(ctx, cfg).
     defer session.Close(context.Background())
  5. If defect != nil:
     - Apply seeded defect patch to test environment. If patch fails -> return failed RunResult with patch error.
  6. Loop turns up to maxTurns:
     a. prompt, err := builder.BuildTurnPrompt(ctx, benchSession, turn, lastTurnResult)
     b. If err != nil (e.g. CategoryContextUnfit):
        return BenchmarkRunResult{Status: RunStatusContextUnfit, Err: err}, nil
     c. turnRes, err := session.ExecuteTurn(ctx, TurnInput{Prompt: prompt})
     d. If ctx.Err() != nil -> return nil, ctx.Err()
     e. If err != nil -> return failed RunResult with driver error.
     f. Record token metrics (turnRes.Usage) into session run log.
     g. If turnRes indicates completion or max turns reached: break.
  7. Run deterministic verification suite (compiler, tests, linters).
  8. Evaluate defect outcome:
     - If defect == nil -> DefectStatus = DefectNotApplicable.
     - Else if compiler/validator rejected upfront -> DefectStatus = DefectPrevented.
     - Else if verification suite caught defect -> DefectStatus = DefectDetected.
     - Else -> DefectStatus = DefectMissed.
  9. Return BenchmarkRunResult with token totals, wall time, and defect score.
```

## Authority matrix

| Decision/effect | Authorized source | Forbidden substitute |
| --- | --- | --- |
| Context assembly for Strategy 4 | `compiler.Compiler` | Ad-hoc string concatenation |
| Token measurement | `drivers.TurnResult.Usage` | Model prose claims |
| Defect detection result | Test suite exit code / deterministic assertion | Model reasoning output |

## Missing / unknown / stale input semantics

| Input/fact | Missing | Unknown | Stale | Malformed/contradictory |
| --- | --- | --- | --- | --- |
| StrategyKind | fail closed (`errs.CategoryInvalidArgument`) | fail closed | n/a | fail closed |
| TaskSpec | fail closed (`errs.CategoryInvalidArgument`) | fail closed | n/a | fail closed |
| Driver | fail closed (`errs.CategoryInvalidArgument`) | n/a | n/a | fail closed |
| SeededDefect | permitted (`DefectNotApplicable`) | fail closed | n/a | fail closed |
| Driver Token Usage | recorded as zero with `AccountingUncertain=true` | flag uncertainty | n/a | fail closed |

## Failure matrix

| Failure point / condition | Required postcondition | Recovery / response | Evidence |
| --- | --- | --- | --- |
| Driver session error | Session closed cleanly | Benchmark run marked failed with driver error | `RunResult.Status == RunStatusFailed` |
| Context Unfit (Strategy 4) | Logged as ContextUnfit | Recorded as strategy failure without silent truncation | `RunResult.Status == RunStatusContextUnfit` |
| Verification suite crash | Test environment cleaned up | Fail benchmark run | Non-zero exit code |
| Context cancellation | Session closed cleanly | Return `ctx.Err()` unwrapped | `ctx.Err() != nil` |
| Patch application failure | Clean test environment | Return failed RunResult with patch error | `RunResult.Status == RunStatusFailed` |

## Representability map

| Concept / requirement | Exact representation | Adequacy | Action if inadequate |
| --- | --- | --- | --- |
| Strategy kind | `type ContextStrategyKind string` | represented | — |
| Seeded defect | `type SeededDefect struct` | represented | — |
| Benchmark session | `type BenchmarkSession struct` | represented | — |
| Benchmark run result | `type BenchmarkRunResult struct` | represented | — |
| Token telemetry | `type TokenUsage struct` from drivers | represented | — |

## Acceptance scenarios

| ID | Setup / precondition | Action | Expected result | Maps to |
| --- | --- | --- | --- | --- |
| ACC-01 | Runner configured with FakeDriver and 4 strategies | Execute clean task across all 4 strategies | All 4 strategies complete; Strategy 4 produces compiler-structured ContextPack; Strategy 1 produces monotonic transcript | REQ-01, REQ-04, REQ-07, REQ-10 |
| ACC-02 | Seeded defect `INV-AUTH-BYPASS` injected (compiler-inadmissible) | Execute task with Strategy 4 | Compiler rejects defect upfront; defect scored strictly as `DefectPrevented` | REQ-03, REQ-11, INV-04 |
| ACC-03 | Seeded defect injected (runtime failure) | Execute task; test suite fails | Verification suite catches bug; defect scored strictly as `DefectDetected` | REQ-11, INV-04 |
| ACC-04 | Seeded defect unhandled by buggy candidate | Execute task; buggy code committed | Verification passes buggy code; defect scored strictly as `DefectMissed` | REQ-11 |
| ACC-05 | Strategy 4 with undersized context profile | Execute task | Compiler returns `errs.CategoryContextUnfit`; runner records `RunStatusContextUnfit` without truncation | REQ-06, INV-03 |

## Validation

- command / deterministic check: `go test -v -race ./internal/benchmark/... ./tests/benchmark/...`
- command / deterministic check: `make verify`
- mutation testing: mutation review sufficient: adversarial catalog below

### Mutation Catalog (Mandatory)

| Mutant (Plausible Bug / Omission / Boundary Inversion) | Expected Test Failure (Scenario / Invariant Check) |
| --- | --- |
| Strategy 4 bypasses compiler and falls back to string concatenation | ACC-01 fails / compiler invocation verification fails |
| Defect status marked `Detected` based on model text rather than deterministic test exit | ACC-04 fails / status assertion mismatch |
| Strategy 1 truncates history instead of accumulating monotonically | ACC-01 fails / transcript length assertion fails |
| Strategy 2 sliding-window compaction removes initial role/contract prompt | Unit test fails / prompt prefix missing assertion |
| Runner fails to close session on context cancellation | Session cleanup test fails / resource leak |
| Driver returns zero tokens and runner leaves `AccountingUncertain=false` | Accounting uncertainty test fails |
| Patch application fails but runner proceeds and reports `DefectPrevented` | Runner patch failure test fails |

### Required Independent Review Lenses (Dual-Lens Review Pack)
1. **Contract & Authority Reviewer:**
   - Verifies compliance with `REQ-*` and `INV-*`.
   - Validates that benchmark runner operates exclusively via `drivers.SessionDriver` and does not import concrete AI SDKs.
   - Verifies fail-closed behavior for invalid strategy types or unseeded defects.
2. **Test Adequacy & Mutation Reviewer:**
   - Verifies coverage of `ACC-01` through `ACC-05`.
   - Checks that all mutations in the Mutation Catalog are killed.
   - Ensures mock driver does not bypass execution logic.

## Implementation Readiness Report

```text
requirements represented: 11/11
mandatory clauses resolved: 6/6
state transitions specified: 4/4
failure cases specified: 5/5
authority decisions specified: 3/3
missing/unknown input semantics: 5/5
acceptance scenarios mapped: 5/5
unresolved architecture choices: 0
declared local-discretion choices: 3
readiness: READY_FOR_IMPLEMENTATION
```

### Weaker-implementer check

Could a competent implementation model with good language/repository skill but mediocre architectural judgment execute this EWP without inventing important semantics?

- [x] Yes
- [ ] No — return to Principal design
