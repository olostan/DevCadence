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
7. Unknown-field and migration behavior must be explicit before the first compatibility-sensitive release.

These bootstrap schemas intentionally define the semantic core rather than every future field. Extend through reviewed protocol changes rather than adding arbitrary metadata bags.
