# Engineering Work Package: WP-M3B-2 — Setup event ledger and operational state layout

- **Milestone:** M3B — Guided bootstrap and onboarding
- **Scope card:** [docs/WORK_PACKAGES.md#wp-m3b-2--setup-event-ledger-and-operational-state-layout](../WORK_PACKAGES.md#wp-m3b-2--setup-event-ledger-and-operational-state-layout)
- **Base commit:** `8cc2378` (`feat/m3b-guided-bootstrap`, includes accepted WP-M3B-1 checkpoint)
- **Branch:** `feat/m3b-guided-bootstrap`
- **Depends on:** WP-M3B-1 (accepted at `6833219`) — reuses its `protocol.SetupLedgerEvent`, `protocol.EventPayload`, `protocol.SetupExecutionReport`, `protocol.Condition` types verbatim; this WP adds no new protocol types.
- **Status:** Accepted. Independent review ([PR #10 comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5805603916), owner, 2026-09-24) found 7 blocking findings against the first implementation revision, all addressed — see §14. §12 records the `machineFingerprint`→verified-`plan` design decision (finding 4 sharpened this from a parameter choice into a provenance requirement); §13 has deterministic evidence for the accepted revision.

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
- **Projection:** `ProjectExecutionReport(events []*protocol.SetupLedgerEvent, executionID, machineFingerprint string) (*protocol.SetupExecutionReport, error)` derives a `SetupExecutionReport` from ledger events for one execution plus the machine fingerprint of the plan being executed — no separate report *state* is ever written independently of the ledger (ADR-0014 §3: "not the primary ledger record"); see §12 for why `machineFingerprint` is a parameter rather than something read back out of the ledger itself.

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

As accepted, after the review revision in §14 (superseding the first-draft signatures this section originally sketched):

```go
// internal/setup/home.go
func ResolveHome() (string, error)
func EnsureLayout(home string) error // tightens pre-existing permissive dirs, not just newly-created ones

// internal/setup/execlock.go
type ExecutionLock struct{ /* unexported *os.File */ }
func AcquireExecutionLock(home string) (*ExecutionLock, error)
func (l *ExecutionLock) Release() error

// internal/setup/ledger.go
type Ledger struct{ /* unexported path, tip state */ }
func OpenLedger(path string) (*Ledger, error)
func (l *Ledger) Events() ([]*protocol.SetupLedgerEvent, error) // fails closed on corruption, does not return nil silently
func (l *Ledger) Append(event *protocol.SetupLedgerEvent) (*protocol.SetupLedgerEvent, error)

type InterruptedAction struct {
    ExecutionID string
    ActionID    string
    RecipeID    string
    PlanID      string
    PlanDigest  string
}
func FindInterrupted(events []*protocol.SetupLedgerEvent) []InterruptedAction // never suppressed by a later ExecutionFinished

type PostconditionChecker interface {
    CheckPostconditions(ctx context.Context, conditions []protocol.Condition) (passed bool, detail string, err error)
}
func ResolveInterrupted(ctx context.Context, checker PostconditionChecker, postconditions []protocol.Condition) (protocol.ActionStatus, string, error)

// ReconcileInterrupted durably appends PostconditionVerified + ActionTerminated
// to the ledger — resolution is never left in-memory only.
func (l *Ledger) ReconcileInterrupted(ctx context.Context, checker PostconditionChecker, action InterruptedAction, postconditions []protocol.Condition, postconditionEventID, terminatedEventID string, now protocol.Timestamp) (protocol.ActionStatus, error)

// plan is verified against the ledger's recorded plan_id/plan_digest before
// its MachineFingerprint is used — not an unverified bare string.
func ProjectExecutionReport(events []*protocol.SetupLedgerEvent, executionID string, plan *protocol.SetupPlan) (*protocol.SetupExecutionReport, error)
```

## 6. Pseudocode: `OpenLedger` recovery algorithm

**As accepted** (revised in review — see §14 finding 1; the original draft's rule keyed recoverability off *position* — "is this the last line" — which a concurrent short-write-then-append sequence could defeat, since position alone can't tell a complete-but-unflushed record apart from a genuinely committed one). The accepted rule keys recoverability off **termination**, not position or parseability:

```
function readLedgerLines(path):
    // Splits the file into chunks on '\n', recording each chunk's start
    // offset and whether it was itself followed by '\n' (i.e. genuinely
    // committed) or the read hit EOF first (i.e. unterminated — only
    // possible for the very last chunk). A non-EOF read error is returned
    // immediately, never treated as a torn write.

function loadLedgerFile(path):
    if file does not exist: return [], nil
    lines := readLedgerLines(path)
    events := []
    prevDigest := ""
    for each line, with index i (0-based):
        if not line.terminated:
            // Only possible for the last chunk. Whether or not its bytes
            // happen to parse as valid JSON, it was never durably
            // committed — truncate the file back to this chunk's start
            // and stop. This is the ONLY case ever recovered.
            truncate file to line.offsetBefore
            break
        event, err := unmarshal line.content into SetupLedgerEvent
        if err == nil:
            err = event.Validate()  // self-consistency: own event_digest
        chainOK := err == nil &&
                   event.Sequence == uint64(i+1) &&
                   event.PreviousEventDigest == prevDigest
        if not chainOK:
            // Newline-terminated (committed) but invalid — fails closed
            // REGARDLESS of position, including the final line.
            return nil, errs.New(CategoryIntegrity, "setup ledger: corrupt event at sequence %d (line %d): %v", i+1, i+1, err)
        events = append(events, event)
        prevDigest = event.EventDigest
    return events, nil

function OpenLedger(path):
    events := loadLedgerFile(path)  // propagates any CategoryIntegrity error
    return &Ledger{path: path, tip: last(events)}, nil
```

## 7. Edge cases and failure modes

- Empty/nonexistent ledger file → `OpenLedger` succeeds with zero events, next `Append` starts at `sequence = 1`.
- Any newline-terminated (committed) line is malformed JSON, fails self-validation, or breaks the hash chain → fail closed (`CategoryIntegrity` error), **regardless of position, including the final line** — ledger unusable until manually inspected; this WP does not add an auto-repair path, matching ADR-0014's "fails closed." (Revised in review — see §14 finding 1: position was never the right test, termination is.)
- A line has a valid event but wrong `sequence`/`previous_event_digest` (e.g. two ledgers concatenated) → same fail-closed treatment (chain mismatch is a corruption class, not a special case), whatever its position.
- The file does not end in `\n` (the last chunk is unterminated) → the one and only recoverable case: that chunk is dropped and the file truncated back to its start, **whether or not its bytes happen to parse as complete, valid JSON** — termination, not parseability, is what proves a write committed. Next `Append` continues from the last committed event's sequence + 1.
- `ActionStarting` recorded, process killed, restarted: `FindInterrupted` reports it (even if a later, inconsistent `ExecutionFinished` exists); `ProjectExecutionReport` shows it `interrupted`, not `running`; `Ledger.ReconcileInterrupted` checks postconditions via the injected `PostconditionChecker` and durably appends `PostconditionVerified` + `ActionTerminated` (`succeeded` if postconditions already held — e.g. the download actually finished before the kill — otherwise `blocked`) — the action is never rerun to find out, and the resolution survives a crash immediately after `ReconcileInterrupted` returns because it is on disk before the call returns.
- Two `devcadence setup apply` invocations racing: the second blocks on `AcquireExecutionLock` until the first calls `Release()`, on Unix (`flock`) and Windows (`LockFileEx`) alike.
- `ResolveHome` given a relative `DEVCADENCE_HOME` → rejected with `CategoryInvalidArgument`, matching `cmd/devcadence`'s existing behavior exactly.
- A pre-existing `state`/`tmp`/`artifacts/setup` directory, or `setup.lock`/ledger file, at a looser mode than required → tightened to `0700`/`0600` the next time it's touched by `EnsureLayout`/`AcquireExecutionLock`/`Ledger.Append`, not left permissive.
- `ProjectExecutionReport` called with a plan whose `plan_id` or computed digest doesn't match what the ledger recorded for that execution → rejected (`CategoryInvalidArgument`); a `nil` plan is likewise rejected.

## 8. Acceptance criteria (from the scope card) and how each is verified

| Criterion | Verification plan |
|---|---|
| Simulated crash mid-action recovers to `interrupted` then resolves to `succeeded`/`blocked` on restart, durably | `TestLedgerReconcilesInterruptedActionDurably`/`...AsBlockedWhenPostconditionsFail`: append `ExecutionCreated`→`PlanApproved`→`ActionStarting` for one action, stop (simulating a kill); reopen; `FindInterrupted` must report exactly that action; the pre-reconcile `ProjectExecutionReport` must show it `interrupted`; `Ledger.ReconcileInterrupted` with a stub `PostconditionChecker` must durably resolve it (verified by a **fresh** `OpenLedger`, not the in-memory `Ledger`) to `succeeded`/`blocked` |
| Corrupted ledger line fails closed | `TestLedgerFailsClosedOnNonFinalCorruption` (non-final) and `TestLedgerFailsClosedOnCorruptNewlineTerminatedFinalLine` (final, newline-terminated, invalid) — both must return a `CategoryIntegrity` error; `TestEventsFailsClosedOnCorruption` proves `Ledger.Events()` propagates it too, rather than returning an empty slice |
| Corrupted/truncated final line recovers as a torn write | `TestLedgerRecoversTornFinalWrite` (bytes truncated mid-write, unterminated) and `TestLedgerAcceptsCompleteUnterminatedFinalRecordAsTorn` (complete, valid-looking JSON but still unterminated) — both recover by truncation; a subsequent `Append` must continue the chain correctly (`sequence` picks up where the last committed event left off) |
| Concurrent setup runs are serialized by `setup.lock` | `TestExecutionLockSerializesConcurrentRuns` (Unix `flock`, this environment) plus `GOOS=windows GOARCH=amd64 go build ./...` (Windows `LockFileEx`, cross-compiled — this environment cannot run Windows tests) |

## 9. Non-goals / forbidden changes for this WP

- No process execution, no `CommandRunner` — WP-M3B-3.
- No real `PostconditionChecker` implementation (live condition evaluation against the OS) — WP-M3B-3 supplies it; this WP only defines and depends on the interface.
- No CLI wiring — WP-M3B-7.
- No changes to `internal/setup/{doctor,planner,profiles,cache}.go` — reused, not modified. If `doctor.go`'s home-resolution divergence (§0) needs fixing, that's WP-M3B-5's EWP to decide and do.
- No change to `docs/IMPLEMENTATION_PLAN.md`'s M3B status line — WP-M3B-9.

## 10. Escalation conditions

Triggered once, resolved without amending WP-M3B-1's frozen contract — see §12.

## 11. Disposition

Accepted. Independent review ([PR #10 comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5805603916)) found 7 blocking findings against the first revision; all addressed in the revision this EWP now describes — see §14.

## 12. Design decision made during implementation: `ProjectExecutionReport`'s plan provenance

§10 anticipated exactly this class of gap. While implementing `ProjectExecutionReport`, `protocol.SetupExecutionReport.Validate()` requires a non-empty, sha256-hex `MachineFingerprint` — but no `SetupLedgerEvent` payload (checked: `ExecutionCreatedPayload`, `PlanApprovedPayload`, `ActionStartingPayload`, `ActionProcessCompletedPayload`, `PostconditionVerifiedPayload`, `ActionTerminatedPayload`, `ExecutionFinishedPayload`, all in `internal/protocol/ledger.go`) carries a machine fingerprint. Only `protocol.SetupPlan.MachineFingerprint` (WP-M3B-1) does.

Two options were available:
1. **Amend WP-M3B-1's `ExecutionCreatedPayload`** to add a `machine_fingerprint` field, threading it through the ledger event stream so `ProjectExecutionReport` could read it back purely from `events`.
2. **Take the plan as an input** to `ProjectExecutionReport`, sourced by the caller from the `SetupPlan` it is executing (which it necessarily already holds — a plan is what's being executed).

Option 2 was chosen, on the same rationale as before: WP-M3B-1's `SetupLedgerEvent`/`EventPayload` types are an accepted, independently-reviewed checkpoint, and reopening that type contract for a projection-layer convenience would be exactly the scope creep `AGENT_HANDOFF_PROTOCOL.md`'s "a frozen decision may be reopened only by materially new evidence" guards against — the ledger's own correctness (hash-chaining, corruption detection, interruption recovery) needs nothing from the machine fingerprint.

**Revised during review** (finding 4, §14): the first implementation took `machineFingerprint` as a bare `string` parameter. Review correctly identified this as insufficient, not merely inconvenient — an unverified string means two callers could project the *same immutable ledger* into two different valid `SetupExecutionReport` documents just by passing different strings, which is precisely what "derived projection, not a second source of truth" forbids. The accepted signature instead takes the full `*protocol.SetupPlan` and verifies it against what the ledger actually recorded — `plan.PlanID` must equal the execution's ledger `plan_id`, and `protocol.ComputePlanDigest(plan)` must equal the ledger's recorded `plan_digest` — before `MachineFingerprint` is read from it. This keeps the same "no protocol type changed, no WP-M3B-1 checkpoint reopened" property while closing the provenance gap review found.

## 13. Deterministic evidence (accepted revision, base commit `8cc2378`, Go toolchain `go1.25.0`, linux/amd64)

```
$ go build ./...
(clean, exit 0)

$ go vet ./...
(clean, exit 0)

$ go test -count=1 ./...
ok  	github.com/olostan/DevCadence/internal/setup	0.249s
... (all 26 packages ok, 0 failures)

$ go test -race ./internal/setup/...
ok  	github.com/olostan/DevCadence/internal/setup	1.347s

$ GOOS=windows GOARCH=amd64 go build ./...
(clean, exit 0)
```

Tests (all PASS), including the review-driven additions in §14: `TestResolveHomeUsesEnvVarWhenSet`, `TestResolveHomeFallsBackToDotDevcadence`, `TestResolveHomeRejectsRelativeOverride`, `TestEnsureLayoutIsIdempotentAndCreatesExpectedDirs`, `TestEnsureLayoutRejectsRelativeHome`, `TestEnsureLayoutTightensPreExistingPermissiveDirs`, `TestExecutionLockSerializesConcurrentRuns`, `TestAcquireExecutionLockRejectsRelativeHome`, `TestExecutionLockReleaseIsIdempotent`, `TestExecutionLockFileModeIsHardenedOnExistingPermissiveFile`, `TestLedgerAppendAndReload`, `TestLedgerFailsClosedOnNonFinalCorruption`, `TestLedgerFailsClosedOnCorruptNewlineTerminatedFinalLine`, `TestLedgerRecoversTornFinalWrite`, `TestLedgerAcceptsCompleteUnterminatedFinalRecordAsTorn`, `TestLedgerReconcilesInterruptedActionDurably`, `TestLedgerReconcilesInterruptedActionAsBlockedWhenPostconditionsFail`, `TestFindInterruptedSurfacesInconsistencyEvenAfterExecutionFinished`, `TestProjectExecutionReport`, `TestProjectExecutionReportRejectsPlanNotMatchingTheLedger` (3 subtests), `TestEventsFailsClosedOnCorruption`, `TestLedgerFileModeIsHardenedOnExistingPermissiveFile`.

Each acceptance criterion from §8 maps directly to one of these — `TestLedgerReconcilesInterruptedActionDurably` for the crash-recovery criterion (now durable, not just in-memory), `TestLedgerFailsClosedOnNonFinalCorruption`/`TestLedgerFailsClosedOnCorruptNewlineTerminatedFinalLine`/`TestLedgerRecoversTornFinalWrite`/`TestLedgerAcceptsCompleteUnterminatedFinalRecordAsTorn` for the corruption-handling criteria (now termination-based, not parseability-based), `TestExecutionLockSerializesConcurrentRuns` plus the Windows cross-compile for the lock-serialization criterion.

## 14. Independent review disposition

[PR #10 review comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5805603916) (project owner, 2026-09-24) is this WP's required independent per-WP checkpoint review. It found the process discipline good (EWP-before-code, the `cache.go`/`doctor.go` overlap check, synchronous fsync, running `-race` early) but 7 blocking findings against the first implementation revision, all addressed in this revision:

1. **Torn-write recovery was too permissive, with a concrete data-loss bug.** The original `loadLedgerFile` treated any invalid *final logical line* as recoverable, including a complete, newline-terminated corrupt record — and did not track whether the final record was actually newline-terminated, so a crash that left a complete-but-unflushed JSON record without its trailing `\n` would be silently *accepted*, and the next `Append` would then concatenate onto it, corrupting two events on the following reopen. Fixed: `readLedgerLines` now tracks termination per chunk; `loadLedgerFile`'s rule is strictly termination-based, not parseability-based — a newline-terminated chunk that fails to parse, fails self-validation, or breaks the hash chain fails closed *regardless of position*, and only a genuinely unterminated final chunk (the file does not end in `\n`) is ever treated as torn and truncated, whether or not its bytes happen to parse as valid JSON. `readLedgerLines` also now distinguishes a real I/O error (`err != io.EOF`) from ordinary EOF, surfacing the former rather than converting it into a torn-write repair. New tests: `TestLedgerFailsClosedOnCorruptNewlineTerminatedFinalLine`, `TestLedgerAcceptsCompleteUnterminatedFinalRecordAsTorn`.
2. **Interruption recovery did not durably mark or reconcile anything.** `ResolveInterrupted` only returned an in-memory status; nothing appended `PostconditionVerified`/`ActionTerminated` evidence, so a crash immediately after it returned left the ledger exactly where it started, and `ProjectExecutionReport` still showed the action `running`, never `interrupted`. Fixed: added `Ledger.ReconcileInterrupted`, which checks postconditions (still never re-executing the action) and durably appends both events via `Ledger.Append`; `ProjectExecutionReport` now derives `ActionStatusInterrupted`/`ExecutionStatusInterrupted` for any action that started with no terminal event and no later `ExecutionFinished` override. Also, `FindInterrupted` no longer suppresses an interrupted action merely because an `ExecutionFinished` event exists for its execution — that combination is now surfaced as the inconsistency it is, per the review's explicit instruction. New/changed tests: `TestLedgerReconcilesInterruptedActionDurably` (reopens the ledger from a fresh `OpenLedger` after reconciliation, with no hand-written test append, and checks the pre-reconcile report shows `interrupted`), `TestLedgerReconcilesInterruptedActionAsBlockedWhenPostconditionsFail`, `TestFindInterruptedSurfacesInconsistencyEvenAfterExecutionFinished`.
3. **`Ledger.Events()` failed open on integrity/read errors.** It silently returned `nil` on any `loadLedgerFile` error, making corruption indistinguishable from "no events" to a `FindInterrupted(l.Events())` caller. Fixed: `Events()` now returns `([]*protocol.SetupLedgerEvent, error)` and propagates the error. New test: `TestEventsFailsClosedOnCorruption`.
4. **`ProjectExecutionReport`'s bare `machineFingerprint string` was unanchored.** See §12's revised design-decision writeup — the function now takes the verified `*protocol.SetupPlan` instead. New tests: `TestProjectExecutionReportRejectsPlanNotMatchingTheLedger` (wrong `plan_id`, digest-mismatched same-`plan_id`, and `nil` plan, as three subtests).
5. **The execution lock was a silent no-op on Windows.** `lock_windows.go` (pre-existing, from the Phase 2 commit) returned `nil` from all three lock functions without taking any OS lock, so `AcquireExecutionLock`'s serialization guarantee was simply false on Windows despite the repo modeling Windows as a supported OS. Fixed: implemented `lock_windows.go` for real using `golang.org/x/sys/windows`'s `LockFileEx`/`UnlockFileEx` over the file's full byte range, with no `LOCKFILE_FAIL_IMMEDIATELY` flag so the call blocks exactly like Unix's blocking `flock`, matching `lock_unix.go`'s semantics. `golang.org/x/sys` moved from an indirect to a direct `go.mod` dependency (`go mod tidy`). Verified with `GOOS=windows GOARCH=amd64 go build ./...` (this environment cannot run Windows tests, but the cross-compile confirms the code is syntactically and type-correct against the real `golang.org/x/sys/windows` API, not just plausible-looking).
6. **Directory/file modes were only enforced at creation time.** `os.MkdirAll`/`os.OpenFile`'s mode argument is ignored for a path that already exists, so a pre-existing `state/`/`tmp/`/`artifacts/setup/` at `0755` or `setup.lock`/the ledger file at `0644` stayed permissive. Fixed: added `ensureDirMode`/`ensureFileMode` helpers (`home.go`) that `Chmod` after create-or-open, used by `EnsureLayout`, `AcquireExecutionLock`, and `Ledger.Append`. New tests: `TestEnsureLayoutTightensPreExistingPermissiveDirs`, `TestExecutionLockFileModeIsHardenedOnExistingPermissiveFile`, `TestLedgerFileModeIsHardenedOnExistingPermissiveFile`.
7. **The report projection dropped `ActionProcessCompleted` data it had fields for.** `ProjectExecutionReport` ignored `exit_code`/`artifact_ref` from `EventActionProcessCompleted`, even though `ActionResult` has fields for both. Fixed: the projection now maps them. `TestProjectExecutionReport` extended with an `ActionProcessCompleted` event and an `ExitCode` assertion.

**Tracking cleanup:** the review also flagged that `HANDOFF.md`'s WP-M3B-2 checkpoint/expected-remote-HEAD pointed at `2d0c8a6`, not the actual pushed PR-head commit `a33daa1` — the same pre-push-SHA class of issue WP-M3B-1 hit. Fixed alongside this revision's push; see `HANDOFF.md`.
