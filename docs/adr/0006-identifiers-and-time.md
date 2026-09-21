# ADR-0006: ULID identifiers and injected clocks

- **Status:** Accepted
- **Date:** 2026-09-21
- **Decision owner:** Principal
- **Supersedes:** —
- **Superseded by:** —
- **Related invariants:** DCI-032, DCI-093
- **Related tasks:** M1 — Domain core and canonical state

## Context

docs/PROTOCOLS.md §2 requires opaque stable string identifiers, "e.g.
ULID/UUID", with human-readable task aliases layered on top.
ENGINEERING_STANDARDS.md §17 requires deterministic behaviour for identical
durable inputs, naming injected clocks and deterministic fixture IDs
specifically. Neither settles which identifier scheme, nor how time enters the
system.

This matters more than it looks. Identifiers and timestamps are the two places
where non-determinism most easily leaks into durable records, and once it has
leaked, no test can assert on a whole document again.

## Verified facts

- docs/PROJECT_STATE.md uses `evt_...`, `att_...` and `ps_...` shapes in its
  examples.
- `schemas/project-state.schema.json` requires `generated_at` as a
  `date-time`; several other schemas carry RFC3339 timestamps.
- SQLite has no native timestamp type; timestamps are stored as text here.
- Go's `time.Time` carries nanosecond precision, which RFC3339 text with a
  fixed fractional width does not preserve.

## Assumptions

| Assumption | Status | Material |
| --- | --- | --- |
| Sortable identifiers remove the need for a secondary sort key on durable listings | verified | yes |
| Microsecond resolution is sufficient for engineering events | accepted risk | no |
| A dependency is not warranted for a ~90-line encoder | verified | no |

## Decision criteria

- determinism under test without changing production code paths;
- identifiers readable enough to appear in escalations and CLI output;
- durable values that round-trip unchanged through JSON and SQLite;
- no new dependency without a concrete reason (ENGINEERING_STANDARDS.md §3).

## Alternatives considered

### Option A — UUIDv4

**Benefits**
- Ubiquitous; a standard library exists everywhere.

**Costs / risks**
- Not sortable, so every listing of durable records needs an explicit sort
  key, and journal order and identifier order have nothing to do with each
  other.

**What would invalidate it**
- A requirement for identifiers that reveal nothing about creation time.

### Option B — ULID, implemented in-repo — selected

**Benefits**
- Lexicographically sortable by creation time, so listings are stable without
  a secondary key.
- Crockford base32 excludes `I`, `L`, `O` and `U`, so identifiers survive being
  read aloud or transcribed into an escalation.
- The encoder is about ninety lines; a dependency would be larger than the
  code it replaces.

**Costs / risks**
- An in-repo implementation must be tested rather than trusted.
- Identifiers leak approximate creation time, which is not a concern for
  local-first control-plane records.

**What would invalidate it**
- A need for cross-system ULID interoperability strict enough to demand a
  reference implementation.

### Option C — Monotonic integers

Rejected: identifiers would be meaningful only within one database, which
conflicts with "opaque stable" identifiers that appear in Work Packages,
evidence references and future exports.

## Decision

1. **Identifier form:** `<prefix>_<26-character Crockford base32 ULID>`, where
   the prefix names the record kind (`evt`, `tsk`, `att`, `wp`, `val`, `rev`).
   `ids.Valid` checks the shape.
2. **Identifiers are generated from an injected `ids.Source`.** Production
   uses `ULIDSource`; tests use `Sequential`, which emits identifiers of the
   same length and shape with a per-prefix counter, so a test reading
   `tsk_...0000000001` knows it is the first task regardless of how many
   events were created alongside it.
3. **`NewAt` takes the creation instant explicitly** so that an identifier's
   embedded time and its record's timestamp cannot disagree. The control plane
   uses `NewAt` with the same clock reading it stamps the record with.
4. **Time comes from an injected `clock.Clock`.** Production uses
   `SystemClock`; tests use `clock.Fake`, which starts at a fixed instant and
   advances by a fixed step per reading, so a scenario produces the same
   timestamps on every run without sleeping.
5. **All durable timestamps are UTC and truncated to microseconds.** The
   `protocol.Timestamp` type renders a fixed-width RFC3339 form
   (`2006-01-02T15:04:05.000000Z`), so textual ordering equals chronological
   ordering and a value written to SQLite reads back equal.
6. **Human-readable aliases are layered on top.** Tasks carry an operator-
   chosen alias (`DC-012`), unique per project and enforced by a database
   constraint. ProjectState reports aliases; storage and correlation use the
   opaque identifiers.
7. **Aliases are not identifiers.** Nothing durable keys on an alias; it is a
   lookup handle for humans and for the CLI.
8. **A crypto/rand failure panics.** A process that cannot generate
   identifiers cannot produce durable records at all, and continuing would
   risk colliding IDs. This is the one deliberate exception to "no panics for
   expected runtime errors" (ENGINEERING_STANDARDS.md §6); an exhausted system
   entropy source is not an expected runtime error.

## Rationale

Sortability is the property that earns ULID its place: journal listings, task
listings and attempt listings all become stable without a secondary key, which
in turn keeps rendered output deterministic. Injecting both the clock and the
identifier source is what allows tests to compare whole canonical documents —
the same property ADR-0005 depends on for rebuild equivalence. Microsecond
truncation looks like a detail and is not: nanosecond precision would make a
timestamp written to SQLite differ from the one read back, and a rebuilt
projection would then never match its original.

## Consequences

### Positive
- Durable listings are stable without explicit ordering logic.
- Tests produce byte-identical output across runs and machines.
- No new dependency.
- Identifiers are transcribable into escalations and decision records.

### Negative
- An in-repo ULID encoder is code this project must maintain and test.
- Identifiers disclose approximate creation time.
- Sub-microsecond ordering within one process is not representable; the
  journal sequence is the authority on order, not the timestamp.

### New risks
- **R-M1-06 (low):** the in-repo encoder could diverge from the ULID
  specification in an edge case. Contained by `ids.Valid` and by the shape and
  sort-order tests; nothing outside this repository consumes the encoding yet.

## Implementation guidance

- `internal/ids` and `internal/clock` have no dependencies on other DevCadience
  packages, so any package may use them.
- Domain and storage code never calls `time.Now()`; it takes a `Clock`.
- `controlplane.Service` holds one clock and one ID source and passes the same
  instant to both when creating a record.

## Verification plan

- `TestULIDsAreWellShapedAndUnique`, `TestULIDsSortByCreationTime`,
  `TestValidRejectsMalformedIdentifiers`.
- `TestSequentialSourceIsDeterministic`, `TestSequentialNewAtIgnoresTime`.
- `TestSourcesAreSafeForConcurrentUse`, `TestFakeClockIsSafeForConcurrentUse`.
- `TestFakeClockAdvancesDeterministically`, `TestFrozenFakeClockDoesNotMove`,
  `TestSystemClockIsUTCAndMicrosecondTruncated`.
- `TestTimestampRoundTripsAtMicrosecondResolution`,
  `TestTimestampsSortLexicographically`.
- `TestSyntheticProjectReachesDoneDeterministically` — the end-to-end effect.

## Rollback / supersession strategy

Both are behind interfaces. Switching to UUIDs means replacing `ULIDSource`
and adding explicit ordering to listings; existing identifiers stay valid
because `ids.Valid` is a shape check, not a scheme check.

## Follow-up

- [ ] Reconsider if identifiers ever have to be generated by more than one
      writer (M9).
