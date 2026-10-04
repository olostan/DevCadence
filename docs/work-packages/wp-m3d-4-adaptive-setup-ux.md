# WP-M3D-4 — Adaptive Setup and Explanation UX

## Identity

- Work Package ID: WP-M3D-4 (window 2026-10-C)
- Revision: 2 (window review: standard exit code alignment with main.go, AdaptationService wiring in apply, DriverResolver construction/fallback, explicit --intent default)
- Task ID: autonomous-run-1
- Base commit: `a743144af6f4a1c8051628d65766afb331038a4e`
- Project state revision: 1
- Contract digest: n/a (Markdown contract authoritative)
- Target implementation endpoint/profile: competent Go implementer
- Status: READY_FOR_IMPLEMENTATION (r2)

## Objective

Surface adaptive portfolio and workflow recommendations in the DevCadence CLI (`devcadence cognition` subcommand group):
1. `devcadence cognition recommend`: Evaluates current discovered resources and environment facts to propose validated `PortfolioRecommendation` candidates (supporting `--intent`, `--json`, and plain terminal formatting).
2. `devcadence cognition explain`: Inspects the active portfolio or a candidate recommendation/proposal and presents a human-readable and structured breakdown of role bindings, budget pools, source exposure, and tradeoffs.
3. `devcadence cognition apply`: Atomically activates a validated recommendation or initiates an auditable rollback (`--rollback`), creating a `PortfolioChangeProposal` and writing through `AdaptationService` and `ActivationManager`.
4. Guarantees that the CLI is a thin presentation layer over canonical internal cognition services and never acts as a second decision engine (ADR-0018 §15). Plain text and JSON remain canonical; exits deterministically using standard exit codes (0, 1, 2, 3, 4, 5) per `cmd/devcadence/main.go`.

## Context Manifest

- role: implementer (Go, CLI), independent reviewers
- read-authority envelope: `cmd/devcadence/run.go`, `cmd/devcadence/main.go`, `cmd/devcadence/cmd_environment.go`, `internal/cognition/portfolio_activation.go`, `internal/cognition/portfolio_diff.go`, `internal/cognition/portfolio_adaptation.go`, `internal/cognition/plannerdriver/service.go`, `internal/setup/`
- semantic write/scope envelope: `cmd/devcadence/cmd_cognition.go` (new), `cmd/devcadence/cmd_cognition_test.go` (new), `cmd/devcadence/run.go` (dispatch update), docs
- risk tags: CLI usability, deterministic exit codes, JSON parity, no secondary decision engine
- exact normative clauses: ADR-0018 §15 (CLI first, plain/JSON canonical, no second decision engine), DCI-123 (deterministic authorization)
- initial evidence handles: `cmd/devcadence/cmd_environment.go`, `cmd/devcadence/main.go`, `cmd/devcadence/run.go`
- deferred references: none
- assumptions: A1 Standard devcadence command dispatch via `env` and `io.Writer`. A2 Subcommands return error to `run()`, which `main.go:exitCode(err)` maps to standard process exit codes.
- re-resolution triggers: need to change root command structures or exit code conventions.

## Semantic scope envelope

### Authorized domains / path patterns

- `cmd/devcadence/cmd_cognition.go`
- `cmd/devcadence/cmd_cognition_test.go`
- `cmd/devcadence/cmd_environment.go` (routing delegation to new cognition command if beneficial)
- `cmd/devcadence/run.go`
- `docs/COGNITION_PORTFOLIO.md`, `docs/WORK_PACKAGES.md`

### Explicitly forbidden semantic changes

- No secondary decision or routing logic embedded in CLI presentation handlers.
- No interactive prompts that block scriptability or fail to respect `--json`.
- No raw panic or uncontrolled stack traces on malformed arguments.
- Do NOT call `os.Exit` inside subcommand handlers; return typed `error` for `exitCode(err)` mapping.

### LOCAL_DISCRETION

- Plain terminal rendering format (tables, headers, text wrapping).
- CLI flag names conforming to devcadence idioms (`--json`, `--intent`, `--rollback`, `--target`).

## Requirements

| ID | Strength | Requirement | Normative source |
| --- | --- | --- | --- |
| REQ-01 | MUST | CLI verbs added under `cognition`: `recommend`, `explain`, `apply` (joining existing `list`, `probe`, `route`). Dispatched via `cmd_cognition.go`. | ADR-0018 §15 |
| REQ-02 | MUST | `devcadence cognition recommend`: Flags: `--json`, `--intent` (default: all 4 canonical intents if omitted, or specific intent if given). Gathers local environment facts. If session drivers are available, uses `plannerdriver.ExecutePlanning`; otherwise gracefully falls back to deterministic planning (`req.Invoker = nil`). With `--json`, outputs `planner.Result` JSON. In plain mode, prints clean summary table of accepted alternatives and rejections. | ADR-0018 §15 |
| REQ-03 | MUST | `devcadence cognition explain`: Flags: `--json`, `--file <path>`, `--active`. If `--active` (default when no file given), inspects active portfolio via `ActivationManager`. Prints role bindings, budget pools, fallbacks, and max source exposure. With `--json`, prints the target JSON document. | ADR-0018 §15 |
| REQ-04 | MUST | `devcadence cognition apply`: Flags: `--file <path>`, `--rollback`, `--target <activation_id>`, `--reason <text>`, `--json`. Instantiates `AdaptationService`. If `--rollback`, performs rollback via `adaptationService.Rollback`. Otherwise, reads candidate from `--file`, computes diff from current active, builds `PortfolioChangeProposal` with `--reason` (default: `"applied via CLI"` if omitted), and activates via `adaptationService.ProposeAndActivate`. | ADR-0018 §11, §15 |
| REQ-05 | MUST | Standard Exit Codes: Subcommand handlers return errors whose categories map via `main.go:exitCode(err)` to authoritative process exit codes: Exit 0 (`ExitCodeSuccess`), Exit 1 (`ExitCodeExecutionError`), Exit 2 (`ExitCodeInvalidArgument` for bad flags, missing file), Exit 3 (`ExitCodeNotFound` for missing active/target activation), Exit 4 (`ExitCodeDrift` for stale base portfolio conflicts), Exit 5 (`ExitCodeIntegrity` for corrupted JSON). | `cmd/devcadence/main.go` |
| REQ-06 | MUST | Strict JSON Fidelity: When `--json` is supplied, stdout contains ONLY valid parseable JSON. Diagnostic logs and warnings go to stderr. | repo standards |
| REQ-07 | MUST | Single Decision Engine: CLI commands instantiate and execute `cognition.AdaptationService`, `cognition.ActivationManager`, and `cognition.PortfolioValidator`. No business logic or heuristic routing lives in `cmd/devcadence`. | ADR-0018 §15 |

## Invariants / state rules

| ID | Statement | Related requirements |
| --- | --- | --- |
| INV-01 | CLI outputs with `--json` are parseable as valid JSON matching canonical protocol schemas. | REQ-02, REQ-03, REQ-06 |
| INV-02 | Applying or rolling back a portfolio via CLI always produces an immutable `ActivationRecord` in lineage via `AdaptationService`. | REQ-04 |
| INV-03 | CLI presentation code never alters or overrides validation verdicts from the core engine. | REQ-07 |

## Interface / algorithm contract

```text
runCognition(ctx, e, args):
  switch args[0]:
    case "list": return runCognitionList(ctx, e, args[1:])
    case "probe": return runCognitionProbe(ctx, e, args[1:])
    case "route": return runCognitionRoute(ctx, e, args[1:])
    case "recommend": return runCognitionRecommend(ctx, e, args[1:])
    case "explain": return runCognitionExplain(ctx, e, args[1:])
    case "apply": return runCognitionApply(ctx, e, args[1:])
    default: return usage error

runCognitionRecommend(ctx, e, args):
  1. Parse flags (--json, --intent, --depth)
  2. Discover inventory from local environment
  3. Execute planner via plannerdriver.ExecutePlanning (or fallback with req.Invoker = nil)
  4. If --json: output result as JSON to e.stdout
  5. Else: render formatted table of recommendations, confidence, tradeoffs to e.stdout

runCognitionApply(ctx, e, args):
  1. Parse flags (--file, --rollback, --target, --reason, --json)
  2. Init ActivationManager and AdaptationService with local devcadence dir
  3. If --rollback:
       actRecord, err := adaptSvc.Rollback(ctx, *target, revalInput)
       (handle error returning errs.CategoryNotFound or ExecutionError)
  4. Else:
       candidate := readPortfolioFromFile(filePath)
       proposal, err := cognition.CreateChangeProposal(TriggerManualProposal, reason, base, candidate, now)
       actRecord, err := adaptSvc.ProposeAndActivate(ctx, proposal, valInput)
  5. If err != nil: return err (main.go maps category to exit code)
  6. If --json: render actRecord JSON; Else: render success message
```

## Authority matrix

| Decision/effect | Authorized source | Forbidden substitute |
| --- | --- | --- |
| Portfolio activation | `cognition.AdaptationService` + `ActivationManager` | Direct file manipulation by CLI |
| Recommendation synthesis | `plannerdriver.ExecutePlanning` / `planner.Plan` | Ad-hoc CLI heuristics |
| Validation verdict | `cognition.PortfolioValidator` | CLI-side override |

## Missing / unknown / stale input semantics

| Input/fact | Missing | Unknown | Stale | Malformed/contradictory |
| --- | --- | --- | --- | --- |
| `--file` on apply | Return `InvalidArgument` (exit 2) | n/a | n/a | Fail closed (exit 2) |
| Active portfolio on explain | Return `NotFound` error (exit 3) | n/a | n/a | Fail closed |
| Reason on apply | Defaults to `"applied via CLI"` | n/a | n/a | Fail closed |

## Failure matrix

| Failure point / condition | Required postcondition | Recovery / response | Evidence |
| --- | --- | --- | --- |
| Candidate file unparseable | Active portfolio untouched | Return `CategoryInvalidArgument` (exit 2) | Error output |
| Candidate fails validation | Active portfolio untouched | Return `CategoryExecutionError` (exit 1) | Diagnostics rendered |
| Rollback target not found | Active portfolio untouched | Return `CategoryNotFound` (exit 3) | Error output |
| Stale base portfolio conflict | Active portfolio untouched | Return `CategoryConflict` (exit 4) | Error output |

## Representability map

| Concept / requirement | Exact representation | Adequacy | Action if inadequate |
| --- | --- | --- | --- |
| Command Run | `cmd/devcadence/cmd_cognition.go` | represented | — |
| CLI Output | JSON or Plain Text via `env.stdout` | represented | — |
| Activation | `cognition.ActivationRecord` | represented | — |

## Acceptance scenarios

| ID | Setup / precondition | Action | Expected result | Maps to |
| --- | --- | --- | --- | --- |
| ACC-01 | Environment with discovered endpoints | `devcadence cognition recommend --json` | Exit 0, stdout is valid `planner.Result` JSON | REQ-02, REQ-06 |
| ACC-02 | Active portfolio exists | `devcadence cognition explain --active` | Exit 0, prints summary of roles and pools | REQ-03 |
| ACC-03 | Valid candidate portfolio file | `devcadence cognition apply --file p.json --reason "test"` | Exit 0, activates portfolio, prints activation ID | REQ-04 |
| ACC-04 | Invalid candidate portfolio file (e.g. unbound role) | `devcadence cognition apply --file invalid.json` | Exit 1 (`ExitCodeExecutionError`), prints validator diagnostics | REQ-04, REQ-05 |
| ACC-05 | Active portfolio activated twice | `devcadence cognition apply --rollback` | Exit 0, rolls back to first activation | REQ-04 |
| ACC-06 | Malformed flag or missing file | `devcadence cognition apply --file nonexistent.json` | Exit 2 (`ExitCodeInvalidArgument`), prints file not found error | REQ-05 |
| ACC-07 | Targeted rollback by activation ID | `devcadence cognition apply --rollback --target act-1` | Exit 0, activates target historical record | REQ-04 |

## Validation

- command / deterministic check: `go test -v -race ./cmd/devcadence/... -run "TestCognition"`
- mutation testing: `mutation review sufficient: adversarial catalog below`
- mutation catalog:

| Mutant (Plausible Bug / Omission) | Expected Test Failure (Scenario / Check) |
| --- | --- |
| Emit non-JSON stdout when `--json` flag is provided | ACC-01 fails JSON parsing check |
| Bypass validation when applying candidate portfolio | ACC-04 fails (accepts invalid portfolio) |
| Map invalid flags or missing files to exit 1 instead of exit 2 | ACC-06 fails |
| Fail to update lineage on rollback | ACC-05 fails |
| Bypass `AdaptationService` and write direct files | Lineage audit test fails |

- required independent review lenses:
  - Contract & Authority Reviewer: verifies requirements REQ-*, invariants INV-*, boundaries, and fail-closed security.
  - Test Adequacy & Mutation Reviewer: verifies coverage of ACC-*, checks edge cases, and kills all cataloged mutants.
- evidence to capture: test logs, CLI output fixtures, git diff.

## Escalation triggers

- Terminal formatting library conflicts or dependencies.
- Changes to `run.go` command dispatch table structure.

## Implementation Readiness Report

```text
requirements represented: 7/7
mandatory clauses resolved: 2/2
state transitions specified: 3/3
failure cases specified: 4/4
authority decisions specified: 3/3
missing/unknown input semantics: 3/3
acceptance scenarios mapped: 7/7
unresolved architecture choices: 0
declared local-discretion choices: 2
readiness: READY_FOR_IMPLEMENTATION
```

### Weaker-implementer check

Could a competent implementation model with good language/repository skill but mediocre architectural judgment execute this EWP without inventing important semantics?

- [x] Yes
- [ ] No — return to Principal design
