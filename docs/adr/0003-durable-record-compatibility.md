# ADR-0003: Strict readers, explicit unknown-field behaviour, and preserved original bytes

- **Status:** Accepted
- **Date:** 2026-09-21
- **Decision owner:** Principal
- **Supersedes:** —
- **Superseded by:** —
- **Related invariants:** DCI-090, DCI-091, DCI-092, DCI-093
- **Related tasks:** M1 — Domain core and canonical state

## Context

DCI-092 states that unknown-field behaviour must be specified per schema and
that silent lossy parsing is forbidden for durable records. docs/PROTOCOLS.md
§18 adds that unknown fields "must not be silently discarded when
round-tripping durable records".

The published schemas set `additionalProperties: false`, which makes a
document carrying an unrecognised field *invalid* rather than merely
unrecognised. Prose and schema therefore point in slightly different
directions: the prose reads as though unknown fields should be preserved, the
schema as though they cannot legitimately occur. Implementing either reading
silently would leave a durable contract ambiguous, so the behaviour is decided
here.

A second, smaller question surfaced while round-tripping fixtures: the schemas
declare many optional fields, and a document may express "nothing is asserted"
as an absent key, an explicit `null`, or a type zero (`false`, `0`, `""`,
`[]`). Whether those are the same statement affects whether a round trip is
lossless.

## Verified facts

- Every schema under `schemas/` declares `additionalProperties: false`
  (inspected directly).
- `schemas/README.md` rule 7 requires unknown-field and migration behaviour to
  be explicit "before the first compatibility-sensitive release".
- No durable DevCadence records exist anywhere yet: M1 is the first
  implementation, and the repository was at Day 0 before it.

## Assumptions

| Assumption | Status | Material |
| --- | --- | --- |
| No deployed data exists at schema version 1.0 yet | verified | yes |
| Future minor versions add optional fields rather than reinterpreting existing ones | accepted risk | yes |

## Decision criteria

- no silent loss of durable information (DCI-092);
- historical records stay interpretable (DCI-093);
- prose and schema agree (DCI-091);
- a reader either supports a version or fails visibly (docs/PROTOCOLS.md §18);
- the rule must be simple enough that every producer follows it without
  thinking about it.

## Alternatives considered

### Option A — Preserve unknown fields in an overflow map on every type

**Benefits**
- A record written by a newer build survives a read/write cycle intact.

**Costs / risks**
- Reintroduces `map[string]any` into durable records, which
  ENGINEERING_STANDARDS.md §4 forbids.
- Contradicts `additionalProperties: false`: the reader would accept documents
  the published schema rejects, so the twin representations would disagree.
- Preserved-but-uninterpreted fields invite code that reads them, which is how
  untyped side channels start.

**What would invalidate it**
- A requirement to route records through an older build unchanged, which does
  not exist in a local-first single-binary system.

### Option B — Strict readers: refuse unknown fields, keep the original bytes — selected

**Benefits**
- Matches `additionalProperties: false` exactly, so Go and JSON Schema accept
  the same set of documents.
- Loss is impossible because nothing is parsed-and-dropped: an unrecognised
  field stops the read.
- The stored original bytes remain byte-verifiable against their digest, so a
  record this build cannot interpret can still be inspected, exported and
  migrated by a build that can.

**Costs / risks**
- An older binary cannot read a record a newer one wrote, even when the new
  field is additive. Mitigated by the migration rule below.

**What would invalidate it**
- A future topology where several DevCadence versions write to one store.

### Option C — Ignore unknown fields silently

Rejected outright: it is precisely what DCI-092 forbids.

## Decision

1. **Reader strictness.** Every durable record is decoded with unknown fields
   disallowed. An unrecognised field is an `invalid_argument` error; an
   unrecognised `schema_version` is a `schema_version_unsupported` error.
   Trailing content after the record is an error. Nothing is parsed and
   dropped, so "silently discarded" cannot occur.
2. **Unrecognised event types** are `schema_version_unsupported`, not skipped.
   Reading a journal that contains one fails, because skipping it would
   produce a reduction that is wrong in a way nothing downstream could detect.
3. **Original bytes are preserved, and reads verify them.** `records.document`
   stores the canonical JSON exactly as written, with its digest. Reads return
   those bytes rather than a re-serialisation, so a record remains verifiable
   and exportable even when this build cannot interpret it.

   The digest is defined over **the exact stored canonical bytes**, not over a
   re-encoding of the decoded value. That is the stronger and simpler
   invariant: it detects any change to what was written — including one that
   would happen to re-serialise identically — and it stays computable for a
   record this build cannot decode at all, which is precisely the record whose
   integrity matters most. Every read recomputes it and refuses a mismatch
   with an integrity error (`protocol.VerifyDigest`). The same rule applies to
   event payloads.
3a. **Schema-invalid records cannot be written.** Because the Go type and the
   JSON Schema are twin representations of one contract
   (ENGINEERING_STANDARDS.md §5), a document the schema rejects must not
   become durable merely because the Go validation is satisfied. Constraints
   that exist only in the schema — notably string `format` — are therefore
   enforced at the write boundary. Format assertion is opt-in in Draft 2020-12
   and is enabled explicitly; without it `format: "date-time"` would be an
   annotation and a malformed timestamp would validate, leaving the twins
   disagreeing exactly where the Go type is weakest.
4. **Writers validate.** A record is validated before it is serialised, so a
   document a conforming reader would reject never becomes durable.
5. **Zero-value equivalence.** At schema version 1.0, for an *optional* field,
   an absent key, an explicit `null` and the JSON zero value of the field's
   type all mean "nothing is asserted here". Writers emit the shortest form
   (the field is omitted). This is a compatibility statement, not an encoding
   detail: a producer may send any of the three and a consumer must treat them
   alike. Required fields are always emitted, including when their value is a
   zero — `completed_tasks: 0` is an assertion.
6. **Required arrays are never `null`.** A required array field serialises as
   `[]` when empty.
7. **Migration.** A breaking interpretation change is a major schema version.
   A durable record keeps the version it was created under. Migration produces
   new records rather than rewriting historical evidence. Additive minor
   changes remain possible, but because readers are strict, an older binary
   will refuse a newer record rather than misread it — which is the intended
   failure.
8. **Schema refinement at 1.0.** Because no durable data exists yet, M1
   refines the 1.0 schemas in place where the Go types revealed a gap (see
   `schemas/README.md` changelog). From the first tagged release onward, 1.0
   is frozen and changes take a version.

   The same pre-first-release reasoning covers the storage schema: migration
   0001 was refined in place to make `project_id` part of the durable record
   key rather than adding a 0002 that rewrites a table no deployment has. A
   migration that has shipped is never edited — the checksum check refuses
   it — but 0001 has not shipped, and a database created before the change
   fails that checksum check loudly rather than drifting
   (`TestAnEditedMigrationIsRefused`). After the first tagged release this
   option is gone and schema changes are additive migrations.

## Rationale

The two readings of the prose can be reconciled once "not silently discarded"
is read as a prohibition on *loss*, not a mandate to *retain*. Refusing an
unrecognised field loses nothing and makes the compatibility problem visible
to an operator, which is what the invariant is protecting. Retaining unknown
fields would buy cross-version interchange the local-first architecture does
not need, at the cost of the untyped bag ENGINEERING_STANDARDS.md §4 forbids
and of a Go reader that accepts documents its own published schema rejects.

Zero-value equivalence is stated rather than assumed because it is the
difference between a round trip being lossless and being merely similar, and
because the alternative — emitting every optional field explicitly — would
make every record larger and every digest sensitive to a producer's choice of
`null` versus omission.

## Consequences

### Positive
- The Go types and the JSON Schemas accept exactly the same documents.
- A compatibility problem surfaces as a categorised error naming the field or
  version, not as a subtly wrong record.
- Historical records stay byte-verifiable against their digests.

### Negative
- Downgrading a binary after a minor schema addition fails loudly rather than
  degrading gracefully. This is intended, but it does mean a release note must
  accompany any additive change.
- Producers outside this repository must follow the zero-value rule to get
  digest-stable documents.

### New risks
- **R-M1-03 (low):** a future multi-version topology (remote workers, shared
  store) would make strict readers painful and require revisiting this ADR.

## Implementation guidance

- `protocol.Unmarshal` is the only durable-record decoder; it sets
  `DisallowUnknownFields`, checks `schema_version`, rejects trailing content
  and then runs `Validate`.
- `events.DecodePayload` applies the same strictness to event payloads and
  returns `schema_version_unsupported` for an unregistered type.
- `protocol.CanonicalJSON` and `protocol.Digest` define the canonical form
  used for all digests: sorted keys, no HTML escaping, original number
  literals.
- `MarshalJSON` on each record type normalises required arrays to `[]`.

## Verification plan

- `TestUnsupportedSchemaVersionIsRefusedExplicitly`,
  `TestUnknownFieldsAreRefusedNotDiscarded`, `TestTrailingContentIsRefused`.
- `TestUnknownEventTypeIsRefusedAsACompatibilityProblem`,
  `TestUnknownPayloadFieldsAreRefused`,
  `TestUnknownStoredEventTypeIsReportedNotSkipped`.
- `TestFixturesRoundTripWithoutSemanticLoss` — decodes every valid fixture,
  re-encodes it, revalidates against the schema, and compares informative
  content; it also checks that a second round trip is byte-stable.
- `TestRequiredArraysSerialiseAsEmptyNotNull`,
  `TestCanonicalJSONIsStableAndSorted`,
  `TestDigestIsAlgorithmPrefixedAndContentAddressed`.
- `TestCorruptedEvidenceIsRejectedOnRead` and
  `TestDigestIsOverStoredBytesNotReserialisation` for the read-path integrity
  rule; `TestSchemaInvalidRecordCannotBePersisted` for the write-path schema
  rule, using `ProductDecision.RecordedAt` — a Go string whose schema declares
  `format: date-time` — as the case only the schema can catch.
- `TestPutRecordRefusesToRewriteHistory` — a superseded version is still
  readable.

## Rollback / supersession strategy

Relaxing to lenient readers means changing `additionalProperties` in the
schemas, adding overflow storage to the Go types, and superseding this ADR.
Nothing in the current design blocks that; it would be a protocol change
following CONTRIBUTING.md.

## Follow-up

- [x] Record the unknown-field policy in `schemas/README.md`.
- [x] Align docs/PROTOCOLS.md §18 with this decision.
- [ ] Revisit if a multi-version or multi-machine topology is introduced (M9).

## ValidationResult subject (pre-first-release refinement)

`ValidationResult` originally required `attempt_id` and `commit`, which
modelled attempt-scoped validation only, while `ValidationCompleted` already
carried three scopes (`attempt`, `integration`, `baseline`). A durable record
that cannot express two of the three scopes its own event vocabulary defines
is an inconsistency, not an extension point, and M2 is about to produce real
validation records.

Under §8 (pre-first-release refinement) the record was changed in place rather
than versioned: `attempt_id` is replaced by a `subject` object carrying
`kind` plus the identifiers that scope requires, and `commit` remains required
for every scope. The scope enumeration now lives in `internal/protocol` and is
aliased by `internal/events`, so the record and the event cannot drift apart.

The alternative — making `attempt_id` optional — was rejected: optional fields
cannot state "required here, forbidden there", so an integration result
carrying an attempt id and an attempt result missing one would both have been
accepted, and the control plane could not have cross-checked the event against
the record.
