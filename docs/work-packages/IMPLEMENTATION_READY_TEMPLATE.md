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

## Verified facts about the current code

Record only implementation-relevant claims that the EWP depends on. Each row must say how the claim was verified against the EWP base revision. The implementer re-verifies these facts as step 0; a false or stale row is an escalation trigger, not permission to invent replacement semantics.

| ID | Claim | Evidence (file:line / command / artifact) | Verified by |
| --- | --- | --- | --- |
| F-01 | ... | ... | Principal / reviewer |

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
- mutation testing: [declare one: "mutation testing required: <scope/tool>" | "mutation review sufficient: <adversarial catalog below>" | "not applicable: <reason>"]

### Mutation Catalog (Mandatory)
The Mutation Catalog Table is MANDATORY for all implementation EWPs (unless explicitly declared "not applicable" with architectural justification). For each critical positive requirement or fail-closed invariant, enumerate plausible omissions, condition flips, or bypassed checks and specify the exact test or scenario that must fail.

| Mutant (Plausible Bug / Omission / Boundary Inversion) | Expected Test Failure (Scenario / Invariant Check) |
| --- | --- |
| [mutant 1: e.g. inverted fallback condition or missing error check] | [ACC-XX fails / specific test assertion fails] |
| [mutant 2: e.g. unauthenticated driver admitted or budget uncharged] | [ACC-YY fails / validator rejects with ErrUnauthorized] |
| [mutant 3: e.g. status transition missing or partial rollback] | [ACC-ZZ fails / state verification fails] |

### Required Independent Review Lenses (Dual-Lens Review Pack)
All implementation candidates must be evaluated by two independent review subagents dispatched with clean Reviewer Context Packs (DCI-046, DCI-049):
1. **Contract & Authority Reviewer:**
   - Verifies compliance with `REQ-*` requirements and `INV-*` invariants.
   - Verifies package dependency direction and boundary isolation (`boundaries_test.go`).
   - Verifies authority decisions (fail-closed budget, opaque credentials, rejection of unauthorized drivers/tools).
   - Validates that error reporting does not leak secrets or credentials.
2. **Test Adequacy & Mutation Reviewer:**
   - Verifies scenario coverage (`ACC-*`) and assertional strength (rejects tautological or no-op checks).
   - Verifies test independence and mock fidelity (ensures mocks cannot satisfy tests without exercising real logic).
   - Evaluates code against the **Mutation Catalog**: actively verifies that every cataloged mutant causes existing tests to fail.

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

### Consistency sweep after every material revision

A revision can fix one finding while leaving stale wording, counts, names, or acceptance mappings elsewhere. Before declaring the revised EWP ready:

- [ ] search for superseded wording of every changed decision and remove/qualify stale copies;
- [ ] recompute requirement, invariant, failure-case and acceptance-scenario counts in the Readiness Report;
- [ ] verify interface/type/schema names match the Requirements, Authority Matrix and Acceptance Scenarios;
- [ ] verify Status and Readiness lines reflect the revised document;
- [ ] re-resolve any new/changed domains, risks, paths or normative clauses;
- [ ] if a prior readiness review found material/blocking defects and this revision changes the EWP to resolve them, obtain focused independent verification of those repairs before delegation, consistent with `REVIEW_AND_CONVERGENCE.md` §§6–7.

### Weaker-implementer check

Could a competent implementation model with good language/repository skill but mediocre architectural judgment execute this EWP without inventing important semantics?

- [ ] Yes
- [ ] No — return to Principal design

See ADR-0024.
