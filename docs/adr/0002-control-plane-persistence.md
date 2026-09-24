# ADR-0002: SQLite control-plane persistence with an append-only journal and derived projections

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
4a. **Record identity begins with the project.** The durable key of a stored
   protocol record is `(project_id, record_kind, record_id, record_version)`.
   This is the line between the two kinds of identifier the system uses:

   - *Semantic identifiers* are chosen by a human or a principal — `PD-001`,
     `FR-018`, `wp_1`, `pm_1`. Two independent projects will both pick them.
     Records keyed on such an identifier must be scoped by project, or two
     projects fight over one row and every lookup is ambiguous.
   - *Opaque identifiers* are generated by the control plane as ULIDs —
     event, task and attempt ids. They are globally unique by construction,
     carry no project-chosen meaning, and are keyed globally.

   A record that declares its own `project_id` must agree with the project it
   is written to; disagreement is refused rather than resolved, because
   guessing which of the two is wrong would file evidence under the wrong
   project. LessonCandidate carries no `project_id` (DCI-073 allows a lesson
   to outlive the project that produced it), so nothing is cross-checked for
   it; the check is expressed as an optional `protocol.ProjectScoped`
   interface rather than a mandatory method on every record.
4b. **Reads verify digests.** Every read of an event payload or a stored
   record recomputes the digest over the bytes the database returned and
   refuses a mismatch with an integrity error. The immutability triggers stop
   the *application* from rewriting history; they do nothing about a database
   file edited outside it, and evidence integrity has to hold in both cases
   (docs/SECURITY.md §14). Verification happens inside the store so a caller
   cannot forget it.

   This extends to *existence* checks and to the envelope, not only to
   whole-document reads. `RecordExists` resolves the record through the same
   digest-verifying path a reader uses rather than answering from a bare
   `MAX(record_version)` query: it backs the product-authority guard, and a
   check that accepts what a read of the same row refuses is the weaker of two
   signals about the same bytes, so the strict one wins. A journal read
   likewise validates the reconstructed envelope before returning it, so an
   event whose `schema_version` was changed outside the application is refused
   by every read rather than only by the reads that happen to feed the
   reducer.
4c. **Schema is enforced at the durable write boundary.** A record is
   committed only after both its typed Go validation and its serialised
   document against the published JSON Schema pass. The two express different
   constraints — string formats such as `date-time` live only in the schema —
   so checking one is not checking the contract. The check is injected into
   the store as a `RecordValidator` so that storage enforces it while
   `internal/schema` keeps the policy, and it defaults to the embedded
   schemas so the safe behaviour needs no opt-in. A record kind with no
   registered schema is refused, which stops a newly added protocol type
   bypassing the rule by omission.
4d. **An event claiming a durable record is verified before it is appended.**
   Every payload carrying `record_digest` names a durable document. The
   control-plane transaction resolves that reference before the append: the
   record must exist in the project — already stored, or written from
   `Command.Records` in this same transaction — its kind, id and version must
   match what the event names, its stored digest must equal the claimed
   digest, and the compact facts the event repeats (status, commit, verdict,
   dimension, scope, subject) must agree with the document they summarise.

   The two integrity guarantees are separate and both are required:

   > Reducer lineage proves journal-internal consistency.
   > Control-plane reference validation proves that referenced immutable
   > evidence actually exists and matches the digest.

   Lineage cannot see the record store; reference validation cannot see the
   task graph. Without the second, a caller could append a validation and a
   review naming records that were never written, and an acceptance citing
   them would pass every lineage check while resting on nothing.

   The rule is generic, carried by the typed `events.RecordReferencing`
   interface rather than by per-event guards, and a drift test fails the
   build if a payload grows a `record_digest` field without implementing it.
   A failure returns from inside the write transaction, so the record write,
   the journal append and the projection update roll back together.
5. **Migrations:** ordered `NNNN_name.sql` files embedded in the binary, each
   applied in its own transaction together with its bookkeeping row. The
   baseline migration creates `schema_migrations` itself, so a database is
   either fully at a version or untouched. An applied migration whose checksum
   no longer matches is refused.
6. **Transactions:** appending an event and updating the projection happen in
   one transaction. A refused transition commits neither.
7. **Concurrency:** the pool is capped at one connection; WAL and a busy
   timeout are set for on-disk databases.
7a. **A read-only open cannot write.** Read-only commands are opened with a
   `mode=ro` DSN and without the `journal_mode`/`synchronous` pragmas, which
   are themselves writes to the database header, and a missing path is refused
   before the driver sees it so that the failure reads as "this project does
   not exist" rather than as a driver fault. Declining to run migrations was
   not enough: SQLite creates the database file when it opens it, so a typo in
   `-db` left an empty database behind while reporting the project missing.
8. **Artifacts:** large logs, diffs and model transcripts are referenced by
   `protocol.ArtifactRef` (locator plus digest) and never stored in a column.
9. **Location:** the database is `$DEVCADENCE_HOME/state/control-plane.db`,
   with `DEVCADENCE_HOME` defaulting to `~/.devcadence`. The path must be
   absolute; `-db` overrides it. This settles the question docs/SETUP.md §8
   deferred. A full XDG layout is not adopted: the macOS-first target and the
   single `DEVCADENCE_HOME` indirection cover the need with one variable.

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
- Every record read pays a SHA-256 over the stored document, and every record
  write pays a schema validation. Both are bounded by document size, which is
  small for control-plane records; neither has been measured as material.
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
- `TestRecordsAreScopedByProject`, `TestRecordLookupCannotReachAnotherProject`,
  `TestRecordProjectMismatchIsRejected`, `TestLatestRecordVersionIsProjectScoped`,
  `TestProjectScopedRecordSurvivesReopen`.
- `TestCorruptedEvidenceIsRejectedOnRead` (four corruption shapes),
  `TestDigestIsOverStoredBytesNotReserialisation`.
- `TestSchemaInvalidRecordCannotBePersisted`,
  `TestSchemaEnforcementCoversEveryRegisteredRecordKind`,
  `TestUnregisteredRecordKindCannotBePersisted`.
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
- [ ] Revisit the connection cap if a second writer ever exists (M10).
