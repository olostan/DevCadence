# ADR-0005: ProjectState is a pure function of the event prefix

- **Status:** Accepted
- **Date:** 2026-09-21
- **Decision owner:** Principal
- **Supersedes:** —
- **Superseded by:** —
- **Related invariants:** DCI-052, DCI-053, DCI-011
- **Related tasks:** M1 — Domain core and canonical state

## Context

docs/PROJECT_STATE.md §7 gives each ProjectState an immutable revision ID, an
accepted Git commit, an event-journal high-water mark, a schema version and a
generation timestamp. §15 asks the system to detect "state revision not
matching journal high-water mark". §16 describes rebuilding materialised state
from the journal when it is lost or suspect.

How the revision ID is allocated is not specified. The obvious implementation —
a per-project counter incremented whenever state is materialised — makes the
revision depend on *when* state happened to be rendered, not on *what history
it summarises*. Two consequences follow: a rebuild cannot reproduce the
revisions it replaces, and the §15 consistency check becomes a real comparison
that can fail rather than a property that holds by construction.

The generation timestamp raises the same question in a different form: a
wall-clock read makes every rendering of the same history produce different
bytes, so "the rebuild produced an equivalent result" can only ever be
asserted field by field, with the timestamp excluded.

## Verified facts

- `schemas/project-state.schema.json` requires `state_revision` (a non-empty
  string) and `generated_at` (a date-time), and allows a nullable
  `event_high_watermark`.
- docs/PROJECT_STATE.md §7 lists the revision, the watermark and the
  generation timestamp as separate fields of one identity.
- `schemas/engineering-work-package.schema.json` requires
  `project_state_revision`, which exists so that a stale plan is detectable.
- The journal assigns a gap-free, never-reused ascending sequence per
  database (`INTEGER PRIMARY KEY AUTOINCREMENT`).

## Assumptions

| Assumption | Status | Material |
| --- | --- | --- |
| Every fact in ProjectState is derivable from events alone | verified | yes |
| Journal sequences are never reused or reordered | verified | yes |
| A revision needs to be comparable for staleness, not merely unique | verified | yes |

The first assumption is the load-bearing one. It is what forced event payloads
to carry the compact facts ProjectState needs (risk severity, decision
question, semantic summary) rather than only pointers to records.

## Decision criteria

- determinism for identical durable inputs (ENGINEERING_STANDARDS.md §17);
- reconstructability (DCI-053);
- staleness of a Work Package must be decidable by comparing revisions;
- the §15 consistency check should hold structurally where it can.

## Alternatives considered

### Option A — Allocated counter (`ps_000184`), timestamp from the clock

**Benefits**
- Matches the illustrative example in docs/PROJECT_STATE.md §3 literally.
- Revisions are dense even if many events do not change visible state.

**Costs / risks**
- A rebuild invents new revision IDs, so a Work Package referencing
  `ps_000184` may point at a revision that no longer exists after recovery.
- Revision and watermark are two independently maintained values that can
  disagree — the §15 check exists precisely because of this.
- Two renderings of one history differ in bytes, so equivalence must be
  asserted by excluding fields, which weakens the test.

**What would invalidate it**
- Nothing technical; it was rejected as the weaker guarantee.

### Option B — Revision derived from the high-watermark; timestamp from the last applied event — selected

**Benefits**
- `state_revision` and `event_high_watermark` cannot disagree: one is a
  rendering of the other, so the §15 check holds by construction.
- A rebuild reproduces every revision exactly, so historical references stay
  valid across recovery.
- Rendering needs no clock and no identifier source, making the whole reducer
  pure and its output byte-comparable.
- Staleness is a numeric comparison: a Work Package planned at `ps_000000004`
  is trivially older than current `ps_000000013`.

**Costs / risks**
- The revision is not dense: events that change no visible field still advance
  it, so consecutive revisions can render identical documents.
- The rendered ID is longer than the example's `ps_000184`.

**What would invalidate it**
- A need for revisions that are stable across unrelated event insertions,
  which would mean content-addressing instead.

### Option C — Content-addressed revision (digest of the rendered state)

**Benefits**
- Identical states get identical revisions; genuinely dense.

**Costs / risks**
- Revisions stop being ordered, so staleness needs a separate comparison.
- A digest is not readable in an escalation or a CLI listing.

**What would invalidate it**
- A requirement to deduplicate identical states, which does not exist.

## Decision

1. **`state_revision = fmt.Sprintf("ps_%09d", highWatermark)`** where the
   high-watermark is the journal sequence of the highest applied event. The
   zero padding keeps revisions lexicographically ordered as well as
   numerically ordered.
2. **`event_high_watermark`** is that same sequence as a decimal string. It is
   nullable in the schema for forward compatibility with states produced
   outside the journal; this build always sets it.
3. **`generated_at`** is the `occurred_at` of the highest applied event, never
   a wall-clock read. ProjectState therefore has no dependency on when it was
   rendered.
4. **The reducer is pure.** `state.Reduce` and `Projection.ProjectState`
   perform no I/O, read no clock and generate no identifiers. Given the same
   event prefix they return byte-identical canonical JSON.
5. **Every ProjectState field is derived from events**, including risks,
   required decisions, component states, health, validation status and recent
   semantic changes. Event payloads carry the compact facts needed for this
   and reference full protocol records by id and digest for depth (DCI-011).
6. **Historical revisions are reconstructed on demand** by reducing the
   journal prefix up to a sequence, rather than materialised in a snapshot
   table. Only the current revision is stored.
7. **The accepted commit advances on integration, not acceptance.** A change
   becomes the project baseline when `ValidationCompleted{scope: integration}`
   passes, matching docs/ARCHITECTURE.md §11, where an accepted candidate
   still has to survive the integration worktree.
8. **Recent semantic changes are bounded** (currently ten, newest first) so
   that current state stays compact as history grows (docs/PROJECT_STATE.md
   §12). The full history remains in the journal.

## Rationale

Deriving identity from journal position converts two documented consistency
obligations into properties that cannot be violated: a revision cannot drift
from its watermark, and a rebuild cannot invent a different past. The price —
non-dense revisions — costs nothing, because a revision's job is to answer "is
this plan stale?", and a revision that advances without a visible change still
answers that correctly.

Taking `generated_at` from the event stream is the smaller decision with the
larger testing consequence: it is what allows
`TestProjectionCanBeDestroyedAndRebuilt` to compare whole canonical documents
instead of hand-picked fields, which is a far stronger statement of DCI-053.

## Consequences

### Positive
- The §15 check "state revision matches journal high-water mark" holds
  structurally.
- Any historical revision is retrievable (`devcadence state show -at N`).
- Rebuild equivalence is byte equality, not field-by-field similarity.
- No snapshot table to keep consistent.

### Negative
- Reconstructing an old revision costs a replay of that prefix.
- Consecutive revisions may render identical documents.
- Every ProjectState-visible fact must be expressible as an event payload
  field; adding one means adding or extending an event type.

### New risks
- **R-M1-05 (low):** a ProjectState field that cannot be derived from events —
  live repository dirtiness, for instance — would need either a Git-facts
  input to the reducer or an event that records the observation. M2 should
  choose deliberately rather than reading the repository inside the reducer,
  which would destroy purity.

## Implementation guidance

- `state.StateRevision(seq)` is the only place the revision is formatted.
- `Projection.ProjectState()` takes no arguments; if it ever needs a clock or
  an ID source, purity has been lost.
- Repository facts, when M2 introduces them, enter as explicit reducer inputs
  or as recorded events — never as an I/O call inside the reduction.

## Verification plan

- `TestReductionIsAPureFunctionOfTheJournal` — two reductions, byte-identical.
- `TestStateRevisionTracksTheHighWatermark` — every prefix, including
  `generated_at`.
- `TestEveryPrefixRendersAValidProjectState`.
- `TestIncrementalApplicationMatchesWholeStreamReduction`.
- `TestProjectionCanBeDestroyedAndRebuilt` — drop and rebuild, compare whole
  canonical documents.
- `TestHistoricalRevisionsAreReconstructable`.
- `TestSyntheticProjectReachesDoneDeterministically` — two independent runs.
- `TestFutureMilestoneEventsAreRecordedWithoutEffect` — recorded-only events
  advance identity and change nothing else.

## Rollback / supersession strategy

Switching to allocated revisions would require a `project_state_revisions`
table and a migration mapping existing derived revisions into it. Work
Packages referencing old revisions would keep working because the derived form
remains reconstructable from the journal.

## Follow-up

- [x] Document revision and watermark semantics in docs/PROJECT_STATE.md.
- [ ] Decide how Git-derived facts enter the reduction without breaking
      purity (M2).
- [ ] Measure prefix-replay cost for historical revisions (M2).

## Clarification: record resolution belongs to the application transaction

Verifying that an event's `record_digest` resolves to a durable document
requires the record store. That check therefore lives in
`controlplane.Service.Apply`, never in the reducer: giving the reducer store
access would end its purity, make `ProjectState` depend on more than the event
prefix, and break byte-identical rebuild.

The division is exact:

- the **reducer** proves journal-internal consistency — that events refer to
  tasks, attempts and candidates the journal itself established;
- the **control plane** proves that referenced immutable evidence exists and
  matches its digest (ADR-0002 §4d).

Both guarantees are required, and neither implies the other.
