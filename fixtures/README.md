# DevCadience Test Fixtures

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

The file-name prefix names the schema, so `devcadience schema validate` and the
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
