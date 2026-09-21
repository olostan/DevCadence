# ADR-0001: SQLite control-plane persistence with an append-only journal and derived projections

- **Status:** Accepted
- **Date:** 2026-09-21
- **Decision owner:** Principal
- **Supersedes:** —
- **Superseded by:** —
- **Related invariants:** DCI-032, DCI-052, DCI-053, DCI-093
- **Related tasks:** M1 — Domain core and canonical state

## Context

M1 must provide durable control-plane state: an engineering event journal, a
canonical ProjectState, a task/attempt model and a migration framework.
AGENTS.md §13 and ENGINEERING_STANDARDS.md §10 already fix SQLite as the store
and require explicit migrations, but several decisions below that are still
open and would be expensive to change once durable data exists:

1. which SQLite driver, given that cgo affects how the binary is built and
   distributed;
2. whether the event journal or the queryable tables are authoritative;
3. where large artifacts live;
4. how concurrent access is handled;
5. where the database file lives (docs/SETUP.md §8 explicitly defers this to
   an ADR).

## Verified facts

- `modernc.org/sqlite` v1.59.0 builds and its tests run on this Linux
  toolchain without a C compiler (`go build ./...`, `go test ./...`).
- docs/ARCHITECTURE.md §10 already states that Git is the source of truth for
  code, SQLite for control-plane records, and that large artifacts should be
  content-addressed or otherwise immutable.
- docs/PROJECT_STATE.md §16 describes rebuilding materialised state from the
  event journal as the failure-recovery path.
- NFR-013 targets 48 GB Apple Silicon; NFR-012 targets a single developer
  machine. Neither implies a concurrency requirement beyond one operator.

## Assumptions

| Assumption | Status | Material |
| --- | --- | --- |
| A single local operator drives one control plane at a time | accepted risk | yes |
| Per-project journals stay small enough that full replay on write is acceptable in M1 | accepted risk | yes |
| Pure-Go SQLite performance is adequate for control-plane metadata | accepted risk | no |

A2 is the one to watch. It is measured by the projection-rebuild cost, and the
mitigation (snapshot plus tail replay) is additive; see **Consequences**.

## Decision criteria

- correctness and auditability before throughput (NFR-001);
- local-first operation with no mandatory service (NFR-002);
- single-binary deployment on macOS and Linux;
- reconstructability of state (DCI-053);
- interpretability of historical records (DCI-093);
- simplicity: no distributed-systems machinery that has not been justified.

## Alternatives considered

### Option A — cgo SQLite (`mattn/go-sqlite3`)

**Benefits**
- The reference SQLite implementation, widely deployed.
- Best raw performance.

**Costs / risks**
- Requires a C toolchain, so `CGO_ENABLED=0` builds and cross-compilation stop
  working, which conflicts with "simple local deployment as one binary"
  (ENGINEERING_STANDARDS.md §2).
- Makes CI and contributor setup heavier for a control plane whose workload is
  metadata, not bulk data.

**What would invalidate it**
- Measured pure-Go overhead becoming material for control-plane queries.

### Option B — pure-Go SQLite (`modernc.org/sqlite`) — selected

**Benefits**
- No C toolchain; `go build` and cross-compilation work everywhere.
- Same SQL surface, so the choice is reversible by swapping the driver import.

**Costs / risks**
- Slower than the C implementation.
- A transpiled implementation is a larger trust surface than a thin binding.

**What would invalidate it**
- A correctness divergence from upstream SQLite in a feature we rely on
  (triggers, foreign keys, WAL).

### Option C — embedded key-value store plus hand-rolled indexes

**Benefits**
- Fewer moving parts than SQL for an append-only log.

**Costs / risks**
- Contradicts an existing normative decision (AGENTS.md §13).
- Every query becomes bespoke code; the consistency checks in
  docs/PROJECT_STATE.md §15 become hand-written scans.

**What would invalidate it**
- Nothing currently; it was rejected on consistency with the baseline.

### Authority: journal-first versus table-first

Considered separately: making the relational tables authoritative and treating
events as an audit log. Rejected because DCI-053 requires state to be
rebuildable from durable records, and an audit log that nothing reduces from
decays into prose nobody validates. The inverse — journal authoritative,
tables derived — makes the rebuild path the normal path.

## Independent critique / consultation

Adversarial critique of the selected design raised three objections:

1. *Full replay on every write is O(n) and will not scale.* Accepted as a real
   cost, bounded by the deferred mitigation below. The alternative — a second,
   incremental implementation of the reducer — risks the two disagreeing, and
   a projection that disagrees with the journal is worse than a slow one.
   `TestIncrementalApplicationMatchesWholeStreamReduction` already
   demonstrates that incremental application is equivalent, which is what
   makes the optimisation safe to add later.
2. *Capping the pool at one connection serialises reads.* Accepted; the
   workload is one operator, and SQLITE_BUSY handling is a cost with no
   current benefit.
3. *Triggers are an unusual way to enforce immutability.* Kept deliberately:
   a rule enforced only in Go is a rule a future bug or an ad-hoc `sqlite3`
   session can break, and the journal's immutability is load-bearing for
   auditability.

No external consultant was involved; M6 is where consultant adapters arrive.

## Decision

1. **Driver:** `modernc.org/sqlite` (pure Go). No cgo.
2. **Authority:** the `events` table is the durable source of truth for
   control-plane history. `projection_*` tables are derived and may be dropped
   and rebuilt at any time. `records` holds immutable protocol documents that
   events reference by id and digest.
3. **Immutability:** `events` and `records` carry `BEFORE UPDATE` and
   `BEFORE DELETE` triggers that abort. Corrections are later events; record
   revisions are new versions.
4. **Ordering:** `events.seq` (`INTEGER PRIMARY KEY AUTOINCREMENT`) is the
   total order of history and the value ProjectState reports as its
   high-watermark. Sequence numbers are never reused.
5. **Migrations:** ordered `NNNN_name.sql` files embedded in the binary, each
   applied in its own transaction together with its bookkeeping row. The
   baseline migration creates `schema_migrations` itself, so a database is
   either fully at a version or untouched. An applied migration whose checksum
   no longer matches is refused.
6. **Transactions:** appending an event and updating the projection happen in
   one transaction. A refused transition commits neither.
7. **Concurrency:** the pool is capped at one connection; WAL and a busy
   timeout are set for on-disk databases.
8. **Artifacts:** large logs, diffs and model transcripts are referenced by
   `protocol.ArtifactRef` (locator plus digest) and never stored in a column.
9. **Location:** the database is `$DEVCADIENCE_HOME/state/control-plane.db`,
   with `DEVCADIENCE_HOME` defaulting to `~/.devcadience`. The path must be
   absolute; `-db` overrides it. This settles the question docs/SETUP.md §8
   deferred. A full XDG layout is not adopted: the macOS-first target and the
   single `DEVCADIENCE_HOME` indirection cover the need with one variable.

## Rationale

The journal-first arrangement is what makes DCI-053 testable rather than
aspirational: because the projection is a pure reduction, a test can destroy
it and prove the rebuild is byte-identical. Database-enforced immutability
turns "events are facts" from a convention into a property. The pure-Go driver
costs throughput the control plane does not need and buys deployment
simplicity it does.

## Consequences

### Positive
- `go build` produces a static binary on macOS and Linux with no C toolchain.
- Materialised state can be destroyed and rebuilt, and tests prove it.
- History cannot be rewritten, including by code outside this repository.
- Artifact growth does not enter the relational store.

### Negative
- Each write replays the project's journal: O(n) per append.
- Reads and writes serialise on one connection.
- The pure-Go driver is a larger trust surface than a thin C binding.

### New risks
- **R-M1-01 (medium):** projection rebuild cost grows with journal length. It
  becomes material somewhere in the tens of thousands of events per project.
- **R-M1-02 (low):** a driver-specific divergence from upstream SQLite in
  triggers, foreign keys or WAL would be discovered late.

## Implementation guidance

- `internal/storage` owns all SQL. No other package constructs statements.
- `Store.Write` and `Store.Read` are the only transaction entry points.
- A projection save rewrites the project's derived rows rather than patching
  them, so there is exactly one implementation of what an event means.
- The deferred optimisation is a `projection_snapshots` table plus tail replay
  from the snapshot's watermark; it does not change any durable contract.

## Verification plan

- `TestMigrationFromAnEmptyDatabase`, `TestReopeningAMigratedDatabaseIsIdempotent`,
  `TestAnEditedMigrationIsRefused`.
- `TestEventsCannotBeUpdatedOrDeleted`, `TestRecordsCannotBeUpdatedOrDeleted`.
- `TestFailedTransactionRollsBackTheAppend`,
  `TestIllegalTransitionLeavesPersistenceUntouched`,
  `TestWorkPackageAndItsEventCommitTogether`.
- `TestProjectionCanBeDestroyedAndRebuilt`,
  `TestPersistedDatabaseReopensWithItsState`.
- `TestForeignKeysAreEnforced`.

## Rollback / supersession strategy

The driver is one import; swapping it requires no schema change. Making the
relational tables authoritative would be a new ADR superseding this one and
would require a migration that stops treating `projection_*` as disposable.

## Follow-up

- [ ] Measure replay cost and, if warranted, add snapshotting (M2 or later).
- [ ] Define artifact-store layout and retention policy (M2).
- [ ] Revisit the connection cap if a second writer ever exists (M9).
