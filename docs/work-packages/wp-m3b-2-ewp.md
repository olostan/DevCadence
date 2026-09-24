# Engineering Work Package: WP-M3B-2 — Setup event ledger and operational state layout

- **Milestone:** M3B — Guided bootstrap and onboarding
- **Scope card:** [docs/WORK_PACKAGES.md#wp-m3b-2--setup-event-ledger-and-operational-state-layout](../WORK_PACKAGES.md#wp-m3b-2--setup-event-ledger-and-operational-state-layout)
- **Base commit:** `8cc2378` (`feat/m3b-guided-bootstrap`, includes accepted WP-M3B-1 checkpoint)
- **Branch:** `feat/m3b-guided-bootstrap`
- **Depends on:** WP-M3B-1 (accepted at `6833219`) — reuses its `protocol.SetupLedgerEvent`, `protocol.EventPayload`, `protocol.SetupExecutionReport`, `protocol.Condition` types verbatim; this WP adds no new protocol types.
- **Status:** Draft — implementation not yet written as of this commit.

## 0. What already exists (read before implementing anything)

Following the pattern that closed out WP-M3B-1 (see `docs/work-packages/wp-m3b-1-ewp.md` §1 "Provenance"), this WP's scope was checked against `internal/setup/*.go` (the pre-existing Phase 2 code on `main`, predating this protocol) before writing anything new. Two of this WP's deliverables are **already implemented and tested** there; the rest are genuinely absent.

**Already done — reuse, do not reimplement:**
- `internal/setup/cache.go`'s `CacheManager` already implements `state/machine-profile.json` caching with `MachineFingerprint`-keyed freshness checks, per-target `flock`-based locking (`lockShared`/`lockExclusive`/`unlock` from `internal/setup/lock_unix.go`/`lock_windows.go`), and atomic write+fsync+rename — exactly ADR-0014 §4's cache-freshness bullet. `internal/setup/cache_test.go` covers it. This WP's ledger and execution lock reuse the same `lockShared`/`lockExclusive`/`unlock` primitives for consistency, but do **not** touch `cache.go` itself.
- `internal/setup/doctor.go`'s `checkStateRoot()` already *diagnoses* (read-only) whether `state/`, `artifacts/setup/`, and `tmp/` exist under `$DEVCADENCE_HOME` — but it does not create them, and it is out of this WP's scope to modify (`internal/setup/doctor.go` belongs to WP-M3B-5's own EWP). This WP adds the actual directory-creation/layout-bootstrap logic doctor's check is diagnosing the absence of.

**Genuinely absent — this WP's real scope:**
- No JSONL ledger writer/reader anywhere in the repository. Only the `protocol.SetupLedgerEvent` *type* (WP-M3B-1) exists — its `Validate()` checks a single event's own internal self-consistency (its own `event_digest`, and that sequence-1 events have an empty `previous_event_digest`), but nothing verifies chain continuity *across* a sequence of events in a file, and nothing reads or writes the file itself.
- No `$DEVCADENCE_HOME/state/setup.lock` execution-wide lock. `cache.go`'s locks are per-cache-file, not a whole-setup-run lock.
- No directory-creation/bootstrap logic for the `$DEVCADENCE_HOME` layout — only doctor's read-only check of it.
- No crash/interruption recovery logic (torn-write recovery, `ActionStatusInterrupted` reconciliation).
- No `SetupExecutionReport` projection function — only the type.
- **Observation, not this WP's to fix:** `doctor.go`'s `HomeDir` option resolution (`internal/setup/doctor.go:75-80`) defaults via `os.UserHomeDir()` + `.devcadence` and never consults a `DEVCADENCE_HOME` environment variable, while `cmd/devcadence/cmd_repo.go`'s `defaultUnderHome` and `cmd/devcadence/run.go`'s `defaultDatabasePath` both do. This WP's own home-resolution helper (`internal/setup.ResolveHome`, §5 below) follows the `cmd/devcadence` convention (env var, must be absolute, default `~/.devcadence`) so every *new* piece of operational state agrees with the CLI layer. Reconciling `doctor.go`'s existing divergence is flagged here for whoever expands WP-M3B-5's EWP, not fixed in this WP — `doctor.go` is out of scope per WP-M3B-1's own non-goals, and this WP's objective explicitly keeps the ledger "independent of the executor," which extends to not reaching into `doctor.go`.

## 1. Objective and rationale

Build the crash-safe, hash-chained, append-only setup event ledger and the `$DEVCADENCE_HOME` operational-state directory layout, as a self-contained layer with no dependency on the executor that will eventually write to it (WP-M3B-3). ADR-0014 §3's rationale: a process killed mid-action must never leave the machine in an unknown state or cause a non-idempotent operation to be blindly rerun — the ledger is what makes "what actually happened" reconstructable after an arbitrary kill, and the hash chain is what makes a tampered or corrupted ledger detectable rather than silently trusted.

## 2. Architectural intent

- **Package:** `internal/setup` (same package as the existing Phase 2 code), new files `home.go`, `execlock.go`, `ledger.go`, plus matching `_test.go` files. No new package — this is operational-state machinery alongside the cache/doctor/planner/profiles code already there, not a separate domain.
- **Home resolution (`home.go`):** `ResolveHome() (string, error)` reads `DEVCADENCE_HOME`, falls back to `$HOME/.devcadence`, and rejects a non-absolute override — mirroring `cmd/devcadence/cmd_repo.go`'s `defaultUnderHome` exactly, so CLI and service layers never disagree about where state lives. `EnsureLayout(home string) error` idempotently creates `state/` (`0700`), `artifacts/setup/` (`0700`), `tmp/` (`0700`) — the same three directories `doctor.checkStateRoot()` already checks for, using the same names.
- **Execution lock (`execlock.go`):** `AcquireExecutionLock(home string) (*ExecutionLock, error)` opens (creating if absent, mode `0600`) `state/setup.lock` and takes a **blocking** exclusive `flock` via the existing `lockExclusive` primitive from `lock_unix.go`/`lock_windows.go` — "serialized," per the scope card's acceptance criterion, means a second concurrent run waits rather than fails. `(*ExecutionLock).Release()` unlocks and closes.
- **Ledger (`ledger.go`):** `Ledger` wraps a single JSONL file path (`state/setup-ledger.jsonl`). It is not itself lock-holding — callers acquire `ExecutionLock` first (this WP's tests construct a `Ledger` directly without a lock, since lock acquisition is orthogonal to ledger correctness; only whole-process-level serialization depends on the lock). `OpenLedger(path string) (*Ledger, error)` loads and validates the existing file (recovering a torn final write, failing closed on any earlier corruption — see §6), establishing the in-memory chain tip. `(*Ledger).Append(event *protocol.SetupLedgerEvent) (*protocol.SetupLedgerEvent, error)` takes a caller-populated event (`ExecutionID`, `PlanID`, `PlanDigest`, `ActionID`, `Timestamp`, `Type`, `Payload`, `EventID` already set by the caller via its own `ids.Source`/`clock.Clock`, matching the `planner.go` convention), overwrites `Sequence`/`PreviousEventDigest`/`EventDigest` from the current chain tip, appends one canonical-JSON line, `fsync`s, and returns the finalized event. ADR-0014 §3 requires `EventActionStarting` specifically to be flushed and `fsync`'d *before* the caller runs the command it describes — that ordering is the caller's (WP-M3B-3's) responsibility to sequence correctly; this WP's `Append` guarantees the fsync happens synchronously within the call, which is what makes that ordering possible.
- **Recovery (`ledger.go`):** `FindInterrupted(events []*protocol.SetupLedgerEvent) []InterruptedAction` is a pure function over already-loaded events: for each `execution_id` whose most recent relevant event is an `ActionStarting` with no later `ActionTerminated` for that `action_id` and no `ExecutionFinished` for that execution, it reports an `InterruptedAction`. `PostconditionChecker` is a one-method interface (`CheckPostconditions(ctx, []protocol.Condition) (bool, string, error)`) this package depends on but does not implement — WP-M3B-3 supplies the real implementation backed by live condition evaluation; this WP's own tests use a stub. `ResolveInterrupted(ctx, checker PostconditionChecker, postconditions []protocol.Condition) (protocol.ActionStatus, string, error)` checks postconditions once and returns `ActionStatusSucceeded` or `ActionStatusBlocked` — **never** re-executes anything, per ADR-0014 §3's explicit "never blindly rerun."
- **Projection:** `ProjectExecutionReport(events []*protocol.SetupLedgerEvent, executionID string) (*protocol.SetupExecutionReport, error)` derives a `SetupExecutionReport` purely from ledger events for one execution — no separate report state is ever written independently of the ledger (ADR-0014 §3: "not the primary ledger record").

## 3. Verified assumptions and evidence

- **Assumption:** no ledger writer already exists. Verified by `grep -rn "setup-ledger\|DEVCADENCE_HOME\|setup\.lock" internal/` (matches only `internal/protocol/setup.go`/`setup_test.go` [the type], `internal/schema/schema.go` [schema registration], `internal/setup/planner.go` [a doc-string mention], `internal/setup/cache.go` [unrelated per-cache-file locks]) and `find . -iname "*ledger*"` (schema/fixture/type files only).
- **Assumption:** `lockShared`/`lockExclusive`/`unlock` in `lock_unix.go`/`lock_windows.go` are generic OS-level primitives, not cache-specific, and safe to reuse for the execution lock. Verified by reading `internal/setup/lock_unix.go` (11 lines, operates on any `*os.File` via `syscall.Flock`) and its one existing caller, `cache.go`, which already uses it on an unrelated file path.
- **Assumption:** `protocol.SetupLedgerEvent.Validate()` checks only single-event self-consistency, not cross-event chain continuity. Verified by reading `internal/protocol/ledger.go`'s `Validate()` (checks own `event_digest`, and that `sequence == 1` implies empty `previous_event_digest` — nothing compares against a prior event, because a single `Validate()` call has no access to one).
- **Assumption:** `internal/setup/doctor.go`'s `checkStateRoot()` only reads, never creates. Verified by reading `internal/setup/doctor.go:178-270` — it `os.Stat`s each directory and reports missing ones as a finding; nothing calls `os.MkdirAll`.

## 4. Constraints

**MUST:**
- The ledger file and `$DEVCADENCE_HOME` layout are machine-global operational state — never written into a Git repository, commit, or project event journal (ADR-0014 §4; `internal/state`'s project-scoped reducer is untouched by this WP).
- `EventActionStarting` must be `fsync`'d before the action it describes runs — this WP's `Append` synchronously fsyncs within the call so the caller (WP-M3B-3) can rely on "returned means durable."
- An event `sequence == 1` must have `previous_event_digest == ""`; every later event's `previous_event_digest` must equal the immediately preceding event's `event_digest`. A load that finds this false on any *non-final* line fails closed (returns an error, does not silently drop or skip).
- An interrupted action is never blindly rerun — `ResolveInterrupted` only ever reads postcondition state and reports `succeeded`/`blocked`.
- `setup.lock` uses a blocking exclusive `flock`, so concurrent runs serialize rather than corrupt each other or silently proceed in parallel.

**SHOULD:**
- `EnsureLayout` is idempotent — calling it against an already-correct layout is a no-op, not an error.
- Directory/file permissions match ADR-0014 §4 exactly (`0700` dirs under `$DEVCADENCE_HOME`, `0600` for `setup.lock` and ledger file itself).

**SUGGESTED:**
- Keep `Ledger.Append`'s digest computation delegating to `protocol.ComputeLedgerEventDigest` rather than duplicating canonical-JSON logic in `internal/setup`.

**LOCAL_DISCRETION:**
- Internal buffering/scanning strategy for `OpenLedger`'s line-by-line read (bufio.Scanner vs. bufio.Reader) — bounded by the same reasoning `internal/process`'s output capture already uses elsewhere in this codebase, not a new pattern.
- Exact wording of `InterruptedAction`'s fields beyond `ExecutionID`/`ActionID` (e.g., whether to carry `RecipeID` for logging convenience).

## 5. Interface sketch

```go
// internal/setup/home.go
func ResolveHome() (string, error)
func EnsureLayout(home string) error

// internal/setup/execlock.go
type ExecutionLock struct{ /* unexported *os.File */ }
func AcquireExecutionLock(home string) (*ExecutionLock, error)
func (l *ExecutionLock) Release() error

// internal/setup/ledger.go
type Ledger struct{ /* unexported path, tip state */ }
func OpenLedger(path string) (*Ledger, error)
func (l *Ledger) Events() []*protocol.SetupLedgerEvent // defensive copy of loaded/appended events
func (l *Ledger) Append(event *protocol.SetupLedgerEvent) (*protocol.SetupLedgerEvent, error)

type InterruptedAction struct {
    ExecutionID string
    ActionID    string
    RecipeID    string
}
func FindInterrupted(events []*protocol.SetupLedgerEvent) []InterruptedAction

type PostconditionChecker interface {
    CheckPostconditions(ctx context.Context, conditions []protocol.Condition) (passed bool, detail string, err error)
}
func ResolveInterrupted(ctx context.Context, checker PostconditionChecker, postconditions []protocol.Condition) (protocol.ActionStatus, string, error)

func ProjectExecutionReport(events []*protocol.SetupLedgerEvent, executionID string) (*protocol.SetupExecutionReport, error)
```

## 6. Pseudocode: `OpenLedger` recovery algorithm

```
function OpenLedger(path):
    if file does not exist: return &Ledger{path: path, tip: nil}, nil
    open file, scan line by line, tracking byte offset before each line
    events := []
    prevDigest := ""
    for each line, with index i (0-based):
        isLast := (this is the final line in the file)
        event, err := unmarshal line into SetupLedgerEvent
        if err == nil:
            err = event.Validate()  // self-consistency: own event_digest
        chainOK := err == nil &&
                   event.Sequence == uint64(i+1) &&
                   event.PreviousEventDigest == prevDigest
        if chainOK:
            events = append(events, event)
            prevDigest = event.EventDigest
            continue
        // failure: either malformed JSON, failed self-Validate, or chain mismatch
        if isLast:
            // torn write or corrupted-but-recoverable final append: truncate
            // the file back to the last known-good byte offset and stop.
            truncate file to offset-before-this-line
            break
        // non-final failure: fail closed
        return nil, errs.New(CategoryIntegrity, "setup ledger: corrupt event at sequence %d (line %d): %v", i+1, i+1, err)
    return &Ledger{path: path, tip: last(events)}, nil
```

## 7. Edge cases and failure modes

- Empty/nonexistent ledger file → `OpenLedger` succeeds with zero events, next `Append` starts at `sequence = 1`.
- Non-final line is malformed JSON → fail closed (`CategoryIntegrity` error), ledger unusable until manually inspected — this WP does not add an auto-repair path, matching ADR-0014's "fails closed."
- Non-final line has a valid event but wrong `sequence`/`previous_event_digest` (e.g. two ledgers concatenated) → same fail-closed treatment (chain mismatch is a corruption class, not a special case).
- Final line is truncated mid-write (e.g. process killed during `Append`'s write, before `fsync`) → recovered: file truncated back to the last complete, valid event; next `Append` continues from that tip's sequence + 1.
- Final line is a complete but otherwise-corrupt/invalid event (bad digest, wrong sequence) → also recovered the same way — the scope card's acceptance criterion groups "corrupted" and "truncated" final-line cases together ("a corrupted/truncated final line recovers as a torn write"), so both truncate-and-continue rather than only the strictly-truncated-bytes case.
- `ActionStarting` recorded, process killed, restarted: `FindInterrupted` reports it; `ResolveInterrupted` checks postconditions via the injected `PostconditionChecker` and reports `succeeded` (postconditions already true — e.g. the download actually finished before the kill) or `blocked` (postconditions still false) — the action is never rerun to find out.
- Two `devcadence setup apply` invocations racing: the second blocks on `AcquireExecutionLock` until the first calls `Release()`.
- `ResolveHome` given a relative `DEVCADENCE_HOME` → rejected with `CategoryInvalidArgument`, matching `cmd/devcadence`'s existing behavior exactly.

## 8. Acceptance criteria (from the scope card) and how each is verified

| Criterion | Verification plan |
|---|---|
| Simulated crash mid-action recovers to `interrupted` then resolves to `succeeded`/`blocked` on restart | `TestLedgerRecoversInterruptedAction`: append `ExecutionCreated`→`PlanApproved`→`ActionStarting` for one action, stop (simulating a kill — no terminal event written); reopen the ledger; `FindInterrupted` must report exactly that action; `ResolveInterrupted` with a stub `PostconditionChecker` returning `true`/`false` must yield `Succeeded`/`Blocked` respectively, and the action must not have been "rerun" (no re-execution hook exists in this WP for it to call) |
| Corrupted non-final ledger line fails closed | `TestLedgerFailsClosedOnNonFinalCorruption`: write 3 valid lines, corrupt line 2's bytes (not the last line), `OpenLedger` must return a `CategoryIntegrity` error |
| Corrupted/truncated final line recovers as a torn write | `TestLedgerRecoversTornFinalWrite`: write 2 valid lines, append a truncated/incomplete final line (and, separately, a complete-but-digest-mismatched final line), `OpenLedger` must succeed, return exactly the 2 valid events, and a subsequent `Append` must continue the chain correctly (`sequence = 3`) |
| Concurrent setup runs are serialized by `setup.lock` | `TestExecutionLockSerializesConcurrentRuns`: acquire the lock in the test goroutine, launch a second goroutine that calls `AcquireExecutionLock` on the same home and signals a channel the instant it succeeds; assert the second goroutine has *not* signaled while the first still holds the lock, then `Release()` the first and assert the second acquires it promptly afterward |

## 9. Non-goals / forbidden changes for this WP

- No process execution, no `CommandRunner` — WP-M3B-3.
- No real `PostconditionChecker` implementation (live condition evaluation against the OS) — WP-M3B-3 supplies it; this WP only defines and depends on the interface.
- No CLI wiring — WP-M3B-7.
- No changes to `internal/setup/{doctor,planner,profiles,cache}.go` — reused, not modified. If `doctor.go`'s home-resolution divergence (§0) needs fixing, that's WP-M3B-5's EWP to decide and do.
- No change to `docs/IMPLEMENTATION_PLAN.md`'s M3B status line — WP-M3B-9.

## 10. Escalation conditions

None anticipated. If implementation reveals `protocol.SetupLedgerEvent`'s fields are insufficient to reconstruct `SetupExecutionReport` correctly (e.g. a needed timestamp or artifact reference is missing from a payload), that is a WP-M3B-1 type-contract gap, not something this WP silently works around — it would be escalated as a proposed amendment to the WP-M3B-1 EWP/ADR-0014, not patched ad hoc.

## 11. Disposition

Draft. To be updated to `implemented, pending review` once the code lands, and `accepted` once independently reviewed, matching the WP-M3B-1 checkpoint pattern.
