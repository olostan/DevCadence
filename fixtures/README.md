# DevCadence Test Fixtures

Fixtures are versioned example documents used by the test suite. They exist so
that the two representations of every contract — the Go type in
`internal/protocol` and the JSON Schema in `schemas/` — are checked against the
same concrete documents rather than against each other's assumptions
(ENGINEERING_STANDARDS.md §5).

## Layout

```
fixtures/protocol/
  <schema-name>.valid.json            documents that MUST validate
  <schema-name>.invalid-<reason>.json documents that MUST NOT validate
```

The file-name prefix names the schema, so `devcadence schema validate` and the
test suite can both infer which schema governs a file without a manifest.

## Rules

1. Every `*.valid.json` fixture must validate against its schema and must
   round-trip through the Go type without loss.
2. Every `*.invalid-*.json` fixture must be rejected, and the reason in its
   name must be the reason it is rejected — a fixture that fails for an
   unrelated reason proves nothing.
3. Fixtures carry `schema_version`. When a schema gains a major version, add
   fixtures for the new version rather than editing the old ones: historical
   records must stay interpretable (DCI-093).
4. Fixtures contain no secrets, no real credentials and no real repository
   paths.
5. A fixture asserting something the contract *forbids* is as valuable as one
   asserting what it permits. The M3A additions are mostly of that kind: the
   `machine-capability-profile.invalid-*` documents cover a `verified` backend
   with no authoritative signal, a remote endpoint claiming local acceleration, a
   graded capability with no provenance and a local runtime reporting itself
   authenticated — each a plausible mistake that would otherwise produce a
   confident, wrong record.

## Machine and cognition fixtures

`machine-capability-profile.valid.json` describes a fully exercised machine —
Apple Silicon, verified Metal offload from an authoritative runtime signal, a
coding CLI whose authentication is honestly `unknown`. `*.valid-blank-machine.json`
is the opposite extreme: a container with no Git, no accelerator and no cognition
endpoint at all, which must still be a valid document with an explicit assessment.
`project-state.valid-cognition.json` carries the compact ProjectState projection.

Fixture *machines* — as opposed to fixture documents — live in
`internal/environment/fixtures.go`, because the discovery tests need probe
behaviour rather than a finished document. Both exist for the same reason: the
test suite must cover a Linux/AMD machine and an Apple Silicon machine without
running on either.
