# Implementation-Ready EWP Template

Use this template for new substantial delegated Engineering Work Packages. Remove sections only when they are genuinely not applicable and record why. This Markdown form is authoritative until typed EWP contract structures supersede it.

## Identity

- Work Package ID:
- Revision:
- Task ID:
- Base commit:
- Project state revision:
- Contract digest:
- Target implementation endpoint/profile:
- Status: DRAFT | READY_FOR_IMPLEMENTATION | BLOCKED

## Objective

State the exact outcome this EWP must produce.

## Context Manifest

- role:
- read-authority envelope:
- semantic write/scope envelope:
- risk tags:
- exact normative clauses:
- initial evidence handles:
- deferred references:
- assumptions:
- re-resolution triggers:

## Semantic scope envelope

### Authorized domains / path patterns

- ...

### Explicitly forbidden semantic changes

- public/API contract changes unless listed below;
- protocol/schema semantic changes unless listed below;
- persistence/crash/recovery semantic changes unless listed below;
- security/trust/privacy/spending authority changes unless listed below;
- unrelated subsystem changes.

### LOCAL_DISCRETION

Examples:
- private helper names;
- package-local data structures;
- splitting/adding tests;
- local test/helper files inside the authorized domain;
- repository-native refactoring that preserves the exact contract.

## Requirements

Use stable IDs.

| ID | Strength | Requirement | Normative source |
| --- | --- | --- | --- |
| REQ-01 | MUST | ... | ... |

## Invariants / state rules

| ID | Statement | Related requirements |
| --- | --- | --- |
| INV-01 | ... | REQ-01 |

For stateful work, include pre-state, committed state and failed/interrupted state semantics.

## Interface / algorithm contract

Provide exact interfaces, state transitions, pseudocode or algorithm semantics needed to prevent architectural invention by the implementer.

## Authority matrix

| Decision/effect | Authorized source | Forbidden substitute |
| --- | --- | --- |
| ... | ... | ... |

## Missing / unknown / stale input semantics

| Input/fact | Missing | Unknown | Stale | Malformed/contradictory |
| --- | --- | --- | --- | --- |
| ... | fail closed / default / ... | ... | ... | ... |

## Failure matrix

| Failure point / condition | Required postcondition | Recovery / response | Evidence |
| --- | --- | --- | --- |
| ... | ... | ... | ... |

For persistence/durability work, enumerate crash/restart boundaries and define the commit point or authoritative-state rule.

## Representability map

| Concept / requirement | Exact representation | Adequacy | Action if inadequate |
| --- | --- | --- | --- |
| ... | Go type / schema / API / record | represented | — |

Any unresolved or contradictory representation makes the EWP NOT READY.

## Acceptance scenarios

| ID | Setup / precondition | Action | Expected result | Maps to |
| --- | --- | --- | --- | --- |
| ACC-01 | ... | ... | ... | REQ-01, INV-01 |

Acceptance scenarios are semantic. Validation commands below provide evidence that the scenarios hold.

## Validation

- command / deterministic check:
- command / deterministic check:
- required independent review lens:
- evidence to capture:

## Escalation triggers

Stop implementation and return to the Principal when:

- a requirement cannot be represented by current types/schema/API/state;
- normative sources conflict;
- an undeclared public/cross-layer/authority/persistence semantic change is required;
- missing/unknown input behavior is not specified;
- a failure state is not covered by the contract;
- the complete contract cannot fit the selected endpoint.

## Design / rationale

Alternatives, research, rejected approaches and extended explanation. Execution-critical semantics must also appear above in the authoritative contract.

## Implementation Readiness Report

```text
requirements represented: __/__
mandatory clauses resolved: __/__
state transitions specified: __/__
failure cases specified: __/__
authority decisions specified: __/__
missing/unknown input semantics: __/__
acceptance scenarios mapped: __/__
unresolved architecture choices: 0
declared local-discretion choices: __
readiness: READY_FOR_IMPLEMENTATION | NOT_READY
```

### Weaker-implementer check

Could a competent implementation model with good language/repository skill but mediocre architectural judgment execute this EWP without inventing important semantics?

- [ ] Yes
- [ ] No — return to Principal design

See ADR-0024.
