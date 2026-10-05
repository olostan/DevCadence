# WP-M4-4 — Seeded Defect Corpus Expansion & Held-Out Benchmark Task Suite

## Identity

- Work Package ID: WP-M4-4 (window 2026-10-E; slice 4 of Milestone M4)
- Revision: 2 (window review: WorkloadKind on BenchmarkTask, types.go authorized write scope, package embed FS in fixtures/benchmark, defensive copying in GetTask, expanded mutation catalog)
- Task ID: task-m4-4-defect-corpus-expansion
- Base commit: `0c3bce20d89010d90bfec93540541effc4591b7c`
- Project state revision: 1
- Contract digest: n/a (Markdown contract authoritative)
- Target implementation endpoint/profile: competent Go implementer
- Status: READY_FOR_IMPLEMENTATION (r2)

## Objective

Deliver the expanded, standardized corpus of representative engineering benchmark tasks and seeded defect fixtures in `internal/benchmark/corpus` and `fixtures/benchmark`. The corpus provides 10 pinned, immutable tasks across the four canonical workload dimensions (`navigation`, `implementation`, `review`, `architectural_reasoning` per `protocol.WorkloadKind`), each paired with clean ground-truth verification suites and calibrated seeded defects across all three defect categories (`DefectInvariantViolation`, `DefectAPIMutation`, `DefectBoundaryViolation`).

## Context Manifest

- role: implementer (Go), independent reviewers
- read-authority envelope: `internal/protocol/context.go`, `internal/benchmark/types.go`, `internal/benchmark/defects.go`, `docs/IMPLEMENTATION_PLAN.md` § M4, `docs/adr/0019-non-conversational-cognition-and-adaptive-review.md`, `docs/adr/0024-implementation-ready-work-packages-and-contract-completeness.md`
- semantic write/scope envelope: `internal/benchmark/types.go` (minor addition of WorkloadKind field), `internal/benchmark/corpus/` (corpus loader, registry, task definitions), `fixtures/benchmark/` (pinned tasks, defect patches, package embed FS), docs
- risk tags: fixture immutability, defect calibration realism, determinism, test reproducibility
- exact normative clauses: DCI-005 (facts vs hypotheses), DCI-014 (evidence is progressively retrievable), DCI-123 (deterministic authorization), PROTOCOLS §10B
- initial evidence handles: `internal/benchmark/types.go`, `internal/benchmark/defects.go`
- deferred references: none
- assumptions: A1 BenchmarkTask and SeededDefect types from `internal/benchmark` are used. A2 Fixture files are exported via `embed.FS` from companion package `fixtures/benchmark`.
- re-resolution triggers: changes to `protocol.WorkloadKind` or `benchmark.DefectCategory`.

## Semantic scope envelope

### Authorized domains / path patterns

- `internal/benchmark/types.go` (add `WorkloadKind` to `BenchmarkTask` and `benchmarkTaskDigestView`)
- `internal/benchmark/corpus/task_registry.go` (new)
- `internal/benchmark/corpus/task_corpus.go` (new)
- `internal/benchmark/corpus/corpus_test.go` (new)
- `fixtures/benchmark/fixtures.go` (new package exporting `//go:embed tasks/*.json patches/*.patch; var FS embed.FS`)
- `fixtures/benchmark/tasks/*.json` (new)
- `fixtures/benchmark/patches/*.patch` (new)
- `docs/work-packages/wp-m4-4-ewp.md`

### Explicitly forbidden semantic changes

- No non-deterministic task fixtures (all task digests must be stable sha256).
- No uncontained defect patches (patches must not escape the fixture worktree).
- No hardcoded external filesystem dependencies.

### LOCAL_DISCRETION

- Formatting of fixture JSON files.
- Internal categorization index maps.
- Test assertion helper utilities.

## Requirements

| ID | Strength | Requirement | Normative source |
| --- | --- | --- | --- |
| REQ-01 | MUST | `BenchmarkTask.WorkloadKind`: In `internal/benchmark/types.go`, add `WorkloadKind protocol.WorkloadKind` (with JSON tag `workload_kind`) to `BenchmarkTask` and `benchmarkTaskDigestView` so `Digest()` deterministically includes the workload dimension. | docs/IMPLEMENTATION_PLAN.md § M4 |
| REQ-02 | MUST | Task Corpus Scope: Define a standard registry of at least 10 `benchmark.BenchmarkTask` instances covering all 4 workloads: Navigation (2 tasks), Implementation (4 tasks), Review (2 tasks), Architecture (2 tasks). | docs/IMPLEMENTATION_PLAN.md § M4 |
| REQ-03 | MUST | Pinned Identity & Immutability: Every task must have a unique `TaskID`, valid `WorkloadKind` (`protocol.WorkloadKind.Valid()`), non-empty `Contract`, and verified sha256 digest (`Digest()`). Fixtures must be immutable. | DCI-005, DCI-014 |
| REQ-04 | MUST | Calibrated Defect Suite: Provide at least 6 standard seeded defects (`benchmark.SeededDefect`) spanning: Invariant Violation (e.g. bypass budget authorization, skip validator), API Mutation (e.g. broken schema enum, field type mutation), Boundary Violation (e.g. unmediated worktree write, unauthorized cross-package import). | docs/IMPLEMENTATION_PLAN.md § M4 |
| REQ-05 | MUST | Task Registry API: `type CorpusRegistry struct`; `func NewCorpusRegistry() *CorpusRegistry`; `func (r *CorpusRegistry) RegisterTask(task benchmark.BenchmarkTask) error`; `func (r *CorpusRegistry) GetTask(taskID string) (*benchmark.BenchmarkTask, error)` (returns pointer to defensive copy); `func (r *CorpusRegistry) ListTasks() []benchmark.BenchmarkTask` (sorted by TaskID); `func (r *CorpusRegistry) TasksByWorkload(w protocol.WorkloadKind) []benchmark.BenchmarkTask` (sorted by TaskID). | ADR-0024 |
| REQ-06 | MUST | Defect Association: `func (r *CorpusRegistry) AssociateDefect(taskID string, defect benchmark.SeededDefect) error`; `func (r *CorpusRegistry) GetDefectsForTask(taskID string) []benchmark.SeededDefect` (returns `[]benchmark.SeededDefect{}` if none or unknown task). | docs/IMPLEMENTATION_PLAN.md § M4 |
| REQ-07 | MUST | Embedded Fixture Loader: In `fixtures/benchmark`, declare `package benchmarkfixtures; //go:embed tasks/*.json patches/*.patch; var FS embed.FS`. In `internal/benchmark/corpus`, implement `func LoadDefaultCorpus() (*CorpusRegistry, error)` which consumes `benchmarkfixtures.FS` with zero external filesystem requirements. | DCI-054 |
| REQ-08 | MUST | Stop Rules: `RegisterTask` rejects empty `TaskID`, duplicate `TaskID`, empty `Contract`, or invalid `WorkloadKind` with `errs.CategoryInvalidArgument`. | DCI-104 |

## Invariants / state rules

| ID | Statement | Related requirements |
| --- | --- | --- |
| INV-01 | Corpus tasks and defects are deterministic, immutable, and yield identical digests across calls. | REQ-02, REQ-03, REQ-07 |
| INV-02 | All seeded defects target authorized test domains and pass patch containment verification. | REQ-04, DCI-123 |
| INV-03 | `ListTasks` and `TasksByWorkload` return tasks sorted deterministically by `TaskID` ascending. | REQ-05 |
| INV-04 | `GetTask` returns a defensive copy, preventing caller mutations from altering registry state. | REQ-05 |

## Interface / algorithm contract

```text
CorpusRegistry.RegisterTask(task):
  1. Validate strings.TrimSpace(task.TaskID) != "", strings.TrimSpace(task.Contract) != "", task.WorkloadKind.Valid().
  2. If task already registered -> return errs.CategoryConflict.
  3. Store task in registry map under task.TaskID.
  4. Return nil.

CorpusRegistry.GetTask(taskID):
  1. Look up task in registry.
  2. If not found -> return nil, errs.CategoryNotFound.
  3. Return pointer to defensive copy: copy := *task; return &copy, nil.

LoadDefaultCorpus():
  1. Initialize registry via NewCorpusRegistry().
  2. Read embedded task fixtures from benchmarkfixtures.FS.
  3. Unmarshal and register 10 canonical tasks.
  4. Associate calibrated defects to tasks.
  5. Verify all tasks have valid digests.
  6. Return populated registry.
```

## Authority matrix

| Decision/effect | Authorized source | Forbidden substitute |
| --- | --- | --- |
| Task ground truth definition | `corpus.CorpusRegistry` | Dynamic or randomized task generation |
| Defect association | Static registration in `LoadDefaultCorpus` | Ad-hoc runtime injection |

## Missing / unknown / stale input semantics

| Input/fact | Missing | Unknown | Stale | Malformed/contradictory |
| --- | --- | --- | --- | --- |
| TaskID in GetTask | return `errs.CategoryNotFound` | return `errs.CategoryNotFound` | n/a | fail closed |
| Task in Register | fail closed (`errs.CategoryInvalidArgument`) | fail closed | n/a | fail closed |
| WorkloadKind in Task | fail closed (`errs.CategoryInvalidArgument`) | fail closed | n/a | fail closed |
| Defect in Associate | fail closed (`errs.CategoryInvalidArgument`) | fail closed | n/a | fail closed |

## Failure matrix

| Failure point / condition | Required postcondition | Recovery / response | Evidence |
| --- | --- | --- | --- |
| Duplicate task registration | Existing task unmodified | Return `errs.CategoryConflict` | `err != nil` |
| Task not found in registry | Nil task returned | Return `errs.CategoryNotFound` | `err != nil` |
| Defect associated to non-existent task | Registry state clean | Return `errs.CategoryNotFound` | `err != nil` |

## Representability map

| Concept / requirement | Exact representation | Adequacy | Action if inadequate |
| --- | --- | --- | --- |
| Corpus registry | `type CorpusRegistry struct` | represented | — |
| Canonical task suite | `[]benchmark.BenchmarkTask` | represented | — |
| Calibrated defect suite | `[]benchmark.SeededDefect` | represented | — |
| Fixture embed | `benchmarkfixtures.FS` | represented | — |

## Acceptance scenarios

| ID | Setup / precondition | Action | Expected result | Maps to |
| --- | --- | --- | --- | --- |
| ACC-01 | Default corpus loader invoked | Call `LoadDefaultCorpus()` | Returns registry with exactly 10 tasks spanning all 4 workloads; all tasks have non-empty contracts, valid WorkloadKind, and sha256 digests | REQ-01, REQ-02, REQ-03, REQ-07 |
| ACC-02 | Registered tasks queried by workload | Call `TasksByWorkload()` for Navigation, Implementation, Review, Architecture | Returns exact expected counts (2, 4, 2, 2); results sorted deterministically by TaskID | REQ-02, REQ-05, INV-03 |
| ACC-03 | Calibrated defects queried for tasks | Call `GetDefectsForTask()` | Returns associated defects spanning all 3 defect categories | REQ-04, REQ-06 |
| ACC-04 | Register task with empty or invalid WorkloadKind | Call `RegisterTask()` | Fails closed with `errs.CategoryInvalidArgument` | REQ-01, REQ-08 |
| ACC-05 | Modify task returned by `GetTask()` | Mutate returned struct pointer | Registry copy remains unmodified | REQ-05, INV-04 |

## Validation

- command / deterministic check: `go test -v -race ./internal/benchmark/corpus/... ./fixtures/benchmark/...`
- command / deterministic check: `make verify`
- mutation testing: mutation review sufficient: adversarial catalog below

### Mutation Catalog (Mandatory)

| Mutant (Plausible Bug / Omission / Boundary Inversion) | Expected Test Failure (Scenario / Invariant Check) |
| --- | --- |
| Duplicate task ID overwrites existing task silently | Collision error test fails |
| `TasksByWorkload` returns tasks unsorted | Determinism test fails on sort order check |
| Default corpus missing one of the 4 required workloads | ACC-01 fails / workload coverage assertion |
| Defect associated to non-existent task accepted silently | Defect association test fails with CategoryNotFound |
| `RegisterTask` accepts invalid `WorkloadKind` without error | ACC-04 fails / argument validation check |
| `GetTask` returns direct pointer allowing mutation | ACC-05 fails / defensive copy test fails |

### Required Independent Review Lenses (Dual-Lens Review Pack)
1. **Contract & Authority Reviewer:**
   - Verifies compliance with REQ-01 through REQ-08 and INV-01 through INV-04.
   - Verifies fixture immutability and patch containment.
2. **Test Adequacy & Mutation Reviewer:**
   - Verifies scenario coverage (`ACC-01` through `ACC-05`).
   - Checks that all mutations in the Mutation Catalog are killed.

## Implementation Readiness Report

```text
requirements represented: 8/8
mandatory clauses resolved: 5/5
state transitions specified: 3/3
failure cases specified: 3/3
authority decisions specified: 2/2
missing/unknown input semantics: 4/4
acceptance scenarios mapped: 5/5
unresolved architecture choices: 0
declared local-discretion choices: 3
readiness: READY_FOR_IMPLEMENTATION
```

### Weaker-implementer check

Could a competent implementation model with good language/repository skill but mediocre architectural judgment execute this EWP without inventing important semantics?

- [x] Yes
- [ ] No — return to Principal design
