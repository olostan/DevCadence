# WP-M3D-1C1 — SessionDriver-backed planner Invoker

## Identity

- Work Package ID: WP-M3D-1C1 (window 2026-10-A; first slice of WP-M3D-1C)
- Revision: 2 (window review probe: READY_WITH_FIXES; adapter implemented against the real fake driver, race-clean; behavior-bearing findings fixed)
- Base commit: current `origin/main` at implementation time (the implementor records the SHA)
- Status: READY_FOR_IMPLEMENTATION (r2)

## Objective

Provide one concrete `planner.Invoker` that runs a planner prompt as a single, tool-less, worktree-less turn on a `drivers.SessionDriver`, returning the model text plus Go-known provenance. Endpoint selection, compiler-admitted prompts, retries and activation stay out (later WPs). The adapter must not widen what a planner call can do: no tools, no filesystem scope, no leaked secrets.

## Context Manifest

- read-authority: `internal/cognition/planner/planner.go` (Invoker, Invocation, InvocationResult), `internal/cognition/drivers/{driver,types,fake_driver}.go`, `tests/boundaries_test.go`
- risk tags: tool-less enforcement, secret leakage via error text, driver lifecycle (session close), determinism of tests
- normative clauses: DCI-124 (planner cannot expand authority), DCI-054/055 (adapters replaceable, no provider coupling in core)
- re-resolution triggers: any need to change `drivers`, `planner`, or boundaries tests beyond the listed additions

## Verified facts (the implementor re-verifies each as step 0 and reports any false row)

| ID | Claim | Evidence |
| --- | --- | --- |
| F-01 | `SessionConfig.Validate` requires only non-empty `SessionID` and `ModelID`; `Tools`, `WorktreeScope`, `Mediator`, `SystemPrompt` may be empty/nil in the fake, direct-API and CLI drivers | `drivers/types.go:179-200` |
| F-02 | The fake and direct-API drivers return `CategoryConflict` for a reused `SessionID`, and keep closed sessions in their map | `fake_driver.go` StartSession; `direct_api.go` |
| F-03 | `Session` has no `ModelID()`/`EndpointID()` accessor; `Session.DriverID()` exists; `Session.Config().ModelID` is what the caller supplied | `drivers/driver.go` |
| F-04 | `ExecuteTurn` returns the raw `ctx` error when the context is done; a closed session returns `CategoryInvalidTransition` | drivers |
| F-05 | `TurnResult` has `Content`, `ToolCalls`, `Usage`, `PausedReason`; nothing in the drivers rejects non-empty `ToolCalls` | `drivers/types.go` |
| F-06 | The fake driver stores `SetTurnHandler(turnID, fn)` on the driver, looked up by `TurnInput.TurnID` | `fake_driver.go` |
| F-07 | `drivers` imports `credentials, errs, process, protocol, tools`; it does not import `internal/cognition` or `planner`, so an adapter importing both creates no cycle; no test forbids a package importing both; planner's own dependency test (ACC-18) must keep passing | scout, `tests/boundaries_test.go` |
| F-08 | CLI driver errors can carry subprocess stderr in the error message; a CLI driver also rejects any `ModelID` unless it has a `ModelFlag` or `InvocationMapper` (surfaces as a `start` failure of category Unsupported) | `cli_wrapper.go` run error path, `:157` |
| F-09 | A CLI driver runs its process in `opts.DefaultDir` when `WorktreeScope` is nil, so native tooling/filesystem access of a CLI-wrapped driver is invisible to the adapter; such drivers declare `Capabilities().NativeWorktreeAccess == true` | `cli_wrapper.go:376-379`, `drivers/types.go` |
| F-10 | `errs.CategoryOf(err)` returns the category of an `errs` error and `CategoryInternal` for foreign errors; `context.WithoutCancel` is available (go.mod `go 1.25.0`); `FakeDriver` has no session accessor and `ResumeSession` reopens a closed fake session, so closure and config are observed through a test-local driver wrapper | window probe |

## Semantic scope envelope

### Authorized domains

- NEW package `internal/cognition/plannerdriver/` (non-test and test files)
- `tests/boundaries_test.go`: add `github.com/olostan/DevCadence/internal/cognition/plannerdriver` to `adapterPackages` (this single line makes the existing tests forbid core packages and the planner from importing it, and forbid it from importing storage, controlplane, state or events); add one test that `internal/cognition` does not depend on `internal/cognition/drivers`, and that the plannerdriver dependency list is non-vacuous (contains both `github.com/olostan/DevCadence/internal/cognition/planner` and `github.com/olostan/DevCadence/internal/cognition/drivers`)
- `docs/COGNITION_PORTFOLIO.md`: edit ONLY the sentence at about line 145 that says the driver-backed Invoker is "Not yet implemented" (now: implemented as a candidate pending review, with the package name); `docs/WORK_PACKAGES.md`: edit ONLY the sentence at about line 556 that says WP-M3D-1C is "not started" (now: 1C1 implemented as a candidate pending review; selection, compiler admission, evidence input and activation link remain). Sibling WPs in this window edit other anchors of the same files; there is no order dependency, keep both on conflict.

### Forbidden

- No change to `drivers`, `planner`, `cognition` (non-test), protocol or schemas. No new third-party dependency. No goroutines, retries, caching or global state. No environment/file access.

### LOCAL_DISCRETION

Internal helper names, test layout, comment wording.

## Requirements

| ID | Strength | Requirement |
| --- | --- | --- |
| REQ-01 | MUST | API (names exact): `type Config struct { EndpointID string; ModelID string; SessionIDPrefix string; Timeout time.Duration; IncludeErrorText bool }`; `func NewInvoker(driver drivers.SessionDriver, cfg Config) (*Invoker, error)`; `func (i *Invoker) Invoke(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error)` (implements `planner.Invoker`; add a compile-time assertion `var _ planner.Invoker = (*Invoker)(nil)`). |
| REQ-02 | MUST | `NewInvoker` returns `errs.CategoryInvalidArgument` when `driver == nil` or `strings.TrimSpace(EndpointID)`/`ModelID` is empty or `Timeout < 0`. `SessionIDPrefix` empty defaults to `"planner"`. `driver.Capabilities().NativeWorktreeAccess == true` ⇒ `errs.CategoryInvalidArgument` (a driver with native filesystem tooling cannot be made tool-less by configuration; see F-09). `ModelID` and `EndpointID` are passed verbatim (never normalized). |
| REQ-03 | MUST | Each `Invoke` uses a fresh session id `<prefix>-<n>` where `n` is a per-`Invoker` atomic counter starting at 1 (so a reused driver never sees a duplicate id, and tests can predict the id). The turn id is the constant `"planner-turn-1"`, passed as `TurnInput.TurnID`. The counter is incremented AFTER the entry `ctx.Err()` check, so a call refused by an already-done context consumes no id; a failed `StartSession` does consume one. |
| REQ-04 | MUST | Session configuration is tool-less and worktree-less: `SessionConfig{SessionID, ModelID: cfg.ModelID}` with `SystemPrompt` empty, `Tools` nil, `WorktreeScope` nil, `Mediator` nil, `Options` nil. The prompt text is passed verbatim as `TurnInput.Prompt` (no `ToolResults`). |
| REQ-05 | MUST | Flow: if `ctx.Err() != nil` return it raw; if `cfg.Timeout > 0` derive `context.WithTimeout`; `StartSession`; `defer` `session.Close` on a context that is NOT already cancelled (use `context.WithoutCancel(ctx)` bounded by the same Timeout when set, else as is) and ignore its error; `ExecuteTurn`; then, whether it returned success or error, if the derived call context has `Err() != nil` return that error raw; build the result. |
| REQ-06 | MUST | Fail closed on anything the planner contract does not allow: a non-empty `TurnResult.ToolCalls` ⇒ error (category `errs.CategoryInvalidArgument`, message `planner session returned tool calls`); non-empty `PausedReason` ⇒ error with message `planner session paused` (the driver-supplied `PausedReason` text is appended as `: <text>` only when `IncludeErrorText` is true, truncated to at most 64 bytes at a rune boundary by the adapter's own helper); `session.Status()` after the turn not equal to `SessionStatusActive` (paused, error or closed) ⇒ the same error class (`errs.CategoryInvalidTransition`, message `planner session not active`). An empty `Content` is returned as-is (the planner reports it as a malformed-output rule). |
| REQ-07 | MUST | Result: `InvocationResult{Content: TurnResult.Content, EndpointID: cfg.EndpointID, DriverID: session.DriverID(), ModelID: cfg.ModelID}`. Provenance is exactly what the caller bound and the driver reports; nothing from model output. |
| REQ-08 | MUST | Error text hygiene (F-08): driver/session errors are returned wrapped as `errs.New(category, "planner driver call failed: <stage>")` where `<stage>` is `start` or `execute`, and category is `errs.CategoryOf(err)` (it already yields `CategoryInternal` for foreign errors; do not hand-roll it). The original error text is included ONLY when `cfg.IncludeErrorText` is true. Context cancellation/deadline errors (`errors.Is(err, context.Canceled/DeadlineExceeded)` after the call) are returned raw so `planner.Plan` can classify them. |
| REQ-09 | MUST | `Invoke` holds no state beyond the counter; concurrent `Invoke` calls on one `Invoker` are safe (race-clean). |
| REQ-10 | MUST | Docs updated per the envelope. |

## Invariants

| ID | Statement |
| --- | --- |
| INV-01 | The adapter configures no tools, worktree scope, mediator or system prompt, rejects returned tool calls, and rejects drivers that declare native worktree access. Native tooling of a driver that does NOT declare it is the driver-selection WP's responsibility. |
| INV-02 | Provenance is Go/driver-assigned, never model-derived. |
| INV-03 | With `IncludeErrorText` false, no driver-provided text (errors from `StartSession`/`ExecuteTurn`, `PausedReason`) reaches the returned error. |
| INV-04 | Every started session is closed, including on error and cancellation. |

## Authority matrix

| Decision | Authorized source | Forbidden substitute |
| --- | --- | --- |
| which endpoint/model is called | the caller via `Config` | the model, the prompt |
| whether a tool call is honored | nobody (always rejected) | the driver's capability flags |
| what error detail is exposed | `Config.IncludeErrorText` (default false) | driver defaults |

## Missing / unknown input semantics

| Input | Missing | Unknown | Malformed |
| --- | --- | --- | --- |
| driver / endpoint id / model id | `NewInvoker` InvalidArgument | n/a | whitespace-only ⇒ InvalidArgument |
| `PausedReason`, `ToolCalls` | empty/nil ⇒ proceed | non-empty ⇒ error (REQ-06) | n/a |
| `Content` | empty ⇒ returned as-is | n/a | n/a |

## Failure matrix

| Condition | Required postcondition | Evidence |
| --- | --- | --- |
| StartSession fails | error stage `start`, no session to close, counter consumed | ACC-04 |
| ExecuteTurn fails (non-ctx) | error stage `execute`, session closed | ACC-05 |
| ctx cancelled before/during | raw ctx error; session closed when started | ACC-06 |
| driver returns tool calls | error, session closed | ACC-07 |
| driver paused | error, session closed | ACC-08 |
| Timeout elapses (even if the driver ignores ctx and succeeds, or returns a non-ctx error after the deadline) | `DeadlineExceeded` raw (via the post-turn call-context check) | ACC-09 |
| session not active after the turn (paused/error/closed) | error, session closed | ACC-14 |
| driver declares native worktree access | `NewInvoker` InvalidArgument | ACC-10 |

## Acceptance scenarios

Test names MUST contain `PlannerDriverInvoker` and the ACC id. Use the existing fake driver (`NewFakeDriver`, `SetTurnHandler("planner-turn-1", ...)`).

| ID | Setup | Action | Expected |
| --- | --- | --- | --- |
| ACC-01 | a test-local `drivers.SessionDriver` wrapper around the fake driver that records the `Session` returned by `StartSession` (the fake has no session accessor, and `ResumeSession` would reopen a closed session, so do not use it to check closure); handler returning `{Content: "OK"}`; Config with endpoint `ep-1`, model `m-1` | `Invoke` | result Content "OK", EndpointID `ep-1`, ModelID `m-1`, DriverID = fake driver id; the recorded session reports `SessionStatusClosed` |
| ACC-02 | handler asserts it received the exact prompt, `TurnID` `planner-turn-1` and no ToolResults; the recorded session's `Config()` has nil `Tools`, `WorktreeScope`, `Mediator`, `Options` and an empty `SystemPrompt` | `Invoke` | assertions hold (INV-01) |
| ACC-03 | `Invoke` twice on one Invoker and one driver (ids observed through the recording wrapper; a call with an already-cancelled context consumes no id) | | session ids `planner-1`, `planner-2`; no Conflict |
| ACC-04..09 | the six failure-matrix rows (StartSession failure via pre-existing session id `planner-1`; handler error with a secret-looking message; cancelled ctx; handler returning ToolCalls; handler returning PausedReason (message `planner session paused` without the flag; with `IncludeErrorText` the reason text appended and truncated at a rune boundary); handler delay > Timeout, and a handler that ignores ctx and returns success after the deadline) | `Invoke` | per matrix; for the handler-error case the returned error text does NOT contain the secret-looking string when `IncludeErrorText` is false and DOES when true (INV-03); errors keep the original category |
| ACC-10 | `NewInvoker` with nil driver, blank endpoint, blank model, negative timeout, and a driver whose `Capabilities().NativeWorktreeAccess` is true (fake driver built with that capability) | | InvalidArgument each |
| ACC-11 | 16 goroutines calling `Invoke` | `-race` | no race, 16 distinct session ids |
| ACC-12 | `planner.Plan` with this Invoker and a handler returning a valid planner JSON (the probe found the needed builders are NOT minimal: copy `internal/cognition/planner/fixtures_test.go` (about 277 lines: portfolio, machine profile, inventory, context profiles, ptr) into this package's tests plus about 20 lines for the alternative and output; do not export test helpers from `planner`) | `Plan` | `OutcomeRecommended`; Planner provenance endpoint/driver/model equal the Config/driver values |
| ACC-14 | a test-local `Session` wrapper whose `Status()` returns `SessionStatusPausedBudgetExceeded`, then `SessionStatusError`, then `SessionStatusClosed` after the turn (the fake never produces these itself) | `Invoke` | error (`planner session not active`) each, session closed | REQ-06 |
| ACC-13 | boundary tests | `go test ./tests/...` | planner and cognition still do not depend on drivers; the new package does not depend on storage/controlplane/state/events (non-vacuous) |

## Validation

- build, vet, gofmt on changed files, `go test -count=1 ./...`, `go test -race -count=1 ./internal/cognition/... ./tests/...`, `make docs-check`; hooks, no bypass
- mutation catalog (mutant ⇒ ACC that must fail): enable a tool/mediator/worktree in the session config ⇒ ACC-02; reuse one session id ⇒ ACC-03; skip Close on the error path ⇒ ACC-05/ACC-07/ACC-08 (closed-session assertion); ignore ToolCalls ⇒ ACC-07; ignore PausedReason ⇒ ACC-08; include PausedReason text without the flag ⇒ ACC-08; skip the post-turn status check ⇒ ACC-14; skip the post-turn call-context check ⇒ ACC-09 (success-after-deadline case); allow NativeWorktreeAccess drivers ⇒ ACC-10; always include the error text ⇒ ACC-05 (secret absent); never include it even when the flag is true ⇒ ACC-05; wrap ctx errors (non-raw) ⇒ ACC-06/09; ModelID taken from anywhere but Config ⇒ ACC-01; DriverID hard-coded ⇒ ACC-01; counter not atomic ⇒ ACC-11 under `-race`; Timeout ignored ⇒ ACC-09
- required review lenses: contract/authority; mutation. A robustness lens is optional (no untrusted parsing; error-text handling is covered by ACC-05)

## Escalation triggers

A driver requiring a non-nil Mediator or WorktreeScope; the fake driver cannot report a closed session for ACC-01; any need to change `drivers` or `planner`.

## Design / rationale

D-1 Separate package, not inside `planner`: the planner package's dependency test forbids drivers, which keeps the planner provider-free (DCI-054/055). D-2 One session per call: simplest lifecycle, no reuse hazards, deterministic ids. D-3 Reject tool calls instead of mediating: the planner is advisory text only; supporting tools would add authority. D-4 Error text off by default: CLI driver errors can embed stderr (F-08) and `Plan` copies invoker error text into `Result.Detail`; category-only is safe by default and opt-in is explicit. D-5 `EndpointID` is caller-bound because drivers do not expose it (F-03); endpoint selection is a later WP.

## Implementation Readiness Report

```text
requirements represented: 10/10
mandatory clauses resolved: 3/3
failure cases specified: 8/8
authority decisions specified: 3/3
missing/unknown input semantics: 3/3
acceptance scenarios mapped: 14/14
unresolved architecture choices: 0
readiness: READY_FOR_IMPLEMENTATION (window review: implementability probe passed, findings incorporated)
```
