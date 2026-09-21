# DevCadience Protocol Schemas

This directory contains versioned machine-readable contracts for durable DevCadience objects.

## Initial schemas

### Discovery and specification
- `problem-model.schema.json`
- `ambiguity-ledger.schema.json`
- `product-decision.schema.json`
- `requirement.schema.json`
- `discovery-experiment.schema.json`
- `specification-readiness.schema.json`

### Review convergence
- `review-campaign.schema.json`
- `finding-disposition.schema.json`
- `closure-decision.schema.json`

### Engineering and delivery

- `project-state.schema.json`
- `engineering-work-package.schema.json`
- `evidence-packet.schema.json`
- `validation-result.schema.json`
- `review-result.schema.json`
- `decision-record.schema.json`
- `lesson-candidate.schema.json`

The prose semantics are defined in [../docs/PROTOCOLS.md](../docs/PROTOCOLS.md) and [../docs/PROJECT_STATE.md](../docs/PROJECT_STATE.md).

## Rules

1. Schemas use JSON Schema Draft 2020-12.
2. Durable records contain `schema_version`.
3. Breaking interpretation changes require a major schema version.
4. Historical records retain the version under which they were created.
5. Go/domain types and schemas must evolve together.
6. Examples/fixtures added during M1 must validate in CI.
7. Unknown-field and migration behavior must be explicit before the first compatibility-sensitive release. This is settled by [ADR-0003](../docs/adr/0003-durable-record-compatibility.md); see **Compatibility policy** below.

These bootstrap schemas intentionally define the semantic core rather than every future field. Extend through reviewed protocol changes rather than adding arbitrary metadata bags.

## Compatibility policy (schema version 1.0)

Settled by [ADR-0003](../docs/adr/0003-durable-record-compatibility.md).

1. **Readers are strict.** Every schema sets `additionalProperties: false`, and the Go readers match it: an unrecognized field is rejected, not preserved and not discarded. An unrecognized `schema_version` is rejected with a distinct error rather than guessed at.
2. **Nothing is parsed and dropped.** Because an unknown field stops the read, the silent lossy parsing DCI-092 forbids cannot occur.
3. **Original bytes are kept.** Stored records retain the canonical JSON exactly as written, with a `sha256:` digest, so a record this build cannot interpret remains inspectable, exportable and verifiable.
4. **Absent, `null` and zero are the same statement** for an *optional* field: all three mean "nothing is asserted here". Writers emit the shortest form, so `"dirty": false`, `"migration": null` and an omitted key are interchangeable on the wire. Required fields are always emitted, including zero values — `completed_tasks: 0` is an assertion.
5. **Required arrays are `[]`, never `null`.**
6. **Migration produces new records**, never rewrites historical evidence. A durable record keeps the `schema_version` it was created under.

## Canonical form and digests

Digests are computed over canonical JSON: object keys sorted, no HTML escaping, number literals preserved. The digest is algorithm-prefixed (`sha256:...`) so that stored digests stay interpretable if the algorithm changes.

The digest is defined over **the exact stored canonical bytes**, not over a re-encoding of a decoded value. Every read of an event payload or a durable record recomputes it and refuses a mismatch as an integrity error, so persisted evidence that changed after it was written is never returned as if it were intact — including when the change was made outside the application.

## Enforcement at the write boundary

A durable record is committed only after **both** checks pass:

1. the typed Go semantic validation;
2. the serialised document against the schema published here.

The two express different constraints. String `format` exists only in the schema, so a value such as `ProductDecision.recorded_at` — a Go string — is checked for being a real `date-time` only by this second step. Format assertion is opt-in in Draft 2020-12 and is enabled explicitly in the validator; without it the constraint would be an annotation.

A record kind with no schema registered here cannot be persisted at all. That stops a newly added protocol type from bypassing the twin-representation rule by omission.

## Fixtures

Example documents live in [../fixtures/protocol/](../fixtures/protocol/) and are checked by `tests/schema_fixtures_test.go`: valid fixtures must validate and round-trip through their Go types without semantic loss; invalid fixtures must be rejected. `devcadience schema validate <file>...` runs the same check from the command line.

## Changelog

### 1.0 — refined during M1

No durable DevCadience records existed before M1, so these refinements were made in place at version 1.0. From the first tagged release onward, 1.0 is frozen and changes require a version bump.

- `project-state.schema.json`: `git.accepted_commit` is now nullable. A project registered before any change has been accepted — which is every M1 project, since M1 is repository-independent — has no accepted commit, and an explicit `null` keeps that absence visible rather than encoding it as an empty string. The field remains required, so the absence is always stated.
- `project-state.schema.json`: `capabilities` was an unconstrained object, which would have forced a `map[string]any` into a durable record against ENGINEERING_STANDARDS.md §4. It is now a closed object with typed `local_models` and `consultants` arrays. M1 leaves it empty; no model runtime exists yet.

No schema change was needed for the discovery and specification contracts: M1 added their Go types to match the published schemas as they stand, and `project-state.schema.json`'s `discovery` projection is carried by the Go `ProjectState` type and left unset until the discovery workflow exists.
