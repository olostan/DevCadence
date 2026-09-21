# ADR-0009: Artifact storage semantics and validation-profile execution

## Status
Accepted (M2).

## Context

M1 defined `protocol.ArtifactRef` (locator + digest) as the boundary for
large evidence but wrote nothing to it (docs/IMPLEMENTATION_PLAN.md M1: "No
artifact store"). M1 also defined the full `protocol.ValidationResult`
contract and `events.ValidationCompleted`, including the digest
cross-check machinery in `checkReferencedRecord`
(internal/controlplane/records.go), but nothing produced a real
`ValidationResult` — M2 is required to use that real model, "not
reintroduce a placeholder validation digest" (docs/IMPLEMENTATION_PLAN.md M2
§14).

Two questions needed a durable answer:

1. How are large artifacts (stdout, stderr, diffs) stored so a durable
   record can reference them without ever embedding them?
2. How does executing a validation profile become a real, digest-verified
   `ValidationResult` plus a matching `ValidationCompleted` event, atomically?

## Decision

### Content-addressed artifact store (`internal/artifacts`)

Artifacts are stored at `<root>/<project>/objects/<digest[:2]>/<digest>`,
written to a temp file in the same directory while a streaming SHA-256 is
computed, then published with a single `rename` once the digest is known.
This makes writes atomic (a reader can never observe a partial object) and
naturally deduplicating (two writes of identical bytes converge on the same
path; the second is a no-op after comparing the digest, matching
`storage.PutRecord`'s existing idempotent-write pattern for protocol
records). A read (`Open`/`Verify`) resolves only through a locator this
package generated (`artifact:<project>:<digest>`), and `resolveLocator`
additionally checks the resulting path stays under the store root — an
artifact reference can never be used to read outside the store even if one
were ever constructed by hand instead of returned by `Put`. Writes respect
`context.Context` cancellation and a caller-supplied byte limit with an
explicit `Truncated` flag on the result, matching the same "never silently
truncate" rule the M1 `ValidationResult.CheckResult.output_truncated` field
already established at the schema level.

This is deliberately a plain filesystem store, not a generic object-storage
abstraction with pluggable backends: docs/ARCHITECTURE.md §10 says large
artifacts should be content-addressed "where practical," and a local
single-daemon bootstrap has no requirement yet that would justify a backend
abstraction (ENGINEERING_STANDARDS.md §1: boundaries are created when the
milestone that needs them arrives).

### Validation-profile execution produces the real M1 model

`internal/validation.Profile` is an ordered list of `CheckSpec{ID, Kind,
Argv, Timeout, Env}` — the same shape `docs/IMPLEMENTATION_PLAN.md M2 §10`'s
example YAML shows and `config/project.example.yaml`'s `validation.profiles`
block already documents. `RunProfile` executes every check through
`internal/process`, in order, capturing each check's stdout/stderr as
artifacts and building `[]protocol.CheckResult` plus a rolled-up
`protocol.ValidationOutcome` using exactly the rules the M1 type already
enforces (`ValidationPass` requires at least one passed check and no failed
one; `CheckError`/`CheckCancelled` never round up to `pass`).

`validation.ExecuteAndRecord` is the only path that turns a profile run into
a durable fact: it builds the `protocol.ValidationResult`, computes its
digest with `protocol.Digest` (the same function every other M1 record
uses), and calls `controlplane.Service.AppendTypedEvent` with the record as
a `RecordToStore` alongside a `ValidationCompleted` event carrying that
digest. Because `Service.Apply` (internal/controlplane/service.go) writes
the record and appends the event inside one SQLite transaction, and
`ValidationCompleted` already implements `events.RecordReferencing` — its
digest is cross-checked against the stored record before either commits —
this milestone adds no new digest-verification machinery. It reuses M1's:
a `ValidationCompleted` whose digest does not match its record is refused
before anything is written, and a validation whose commit or attempt does
not match the task's actual candidate is refused by the M1 reducer
(`applyAttemptValidation`) with the whole transaction rolled back —
proven directly by `TestExecuteAndRecordWrongCommitRefused`.

Every check runs even after an earlier one fails or errors. Partial evidence
(stopping at the first failure) would under-inform the reader relative to
DCI-040, and profiles are expected to stay small; a stop-on-failure mode is
left out because M2 §14 asks for it only "if clearly needed," and nothing
yet needs it.

### Scopes are M1's three scopes, used as-is

`ExecuteInput.Subject` is `protocol.ValidationSubject`, unmodified from M1.
M2 makes attempt-scope validation fully real end to end (a task moves
VALIDATING → REVIEWING or back to RUNNING exactly as the M1 reducer already
specifies). Baseline-scope validation is fully executable today (it names no
task) and integration-scope validation is executable wherever a caller can
supply an integrated commit and a task id — M2 does not add integration
*orchestration* (which candidates get combined, in what order); that is
M6/M9 territory, and this ADR does not distort the M1 schema to simulate it
early.

## Consequences

- A `ValidationResult` this build produces is never distinguishable from one
  a future milestone produces through a richer orchestration path — both
  satisfy the same M1 contract and the same digest checks.
- Large check output never reaches a SQLite row; only artifact IDs do.
- A storage hiccup while persisting stdout/stderr does not discard a
  deterministic outcome the tool already produced — the check's
  pass/fail/error status is authoritative regardless of whether its output
  artifact happened to be stored, and this is a deliberate, narrow exception
  to "no partial evidence": the *evidence that matters most* (did the check
  pass) is never lost, only the optional stdout/stderr attachment can be.

## Alternatives considered

- **SQLite blobs for artifact bytes**, as docs/PROJECT_STATE.md's example
  `decisions_required` shows was an open M1 question ("content-addressed
  filesystem artifacts or SQLite blobs during bootstrap?"). Rejected per
  docs/ARCHITECTURE.md §10's explicit preference and
  ENGINEERING_STANDARDS.md's ban on putting large output in SQLite rows.
- **A model-authored validation summary standing in for the record.**
  Rejected outright; DCI-041 and DCI-012 forbid it, and the M1 type already
  structurally prevents a `pass` status from resting on anything but an
  actual passed check.
