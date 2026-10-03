# ADR-0024: Implementation-Ready Work Packages and Contract Completeness

- **Status:** Proposed
- **Date:** 2026-10-03
- **Owners:** DevCadence architecture / Principal Engineering protocol
- **Related:** AGENTS.md §6–7; PROTOCOLS §7; WORK_PACKAGES.md; PRINCIPAL_ENGINEER.md; LOCAL_AGENTS.md; ADR-0020

## Context

DevCadence intentionally moves expensive architectural reasoning upstream so cheaper/local cognition can perform high-volume implementation safely. The current EWP model already requires a bounded Execution Contract, exact normative clauses, acceptance criteria, validation and escalation. ADR-0020 additionally introduced a manual Contract Completeness Review for systemic protocol work.

PR #27 (WP-M3C-3) exposed the remaining gap: a contract can be architecturally sensible and still leave implementation-critical semantics underspecified. A strong coding model may fill those gaps plausibly but incorrectly; a smaller local model has even less chance of reliably reconstructing missing architecture.

The failure mode is not primarily "weak coding." It is delegated architecture leakage:

- "crash-safe atomic activation" without an explicit commit/recovery model;
- "rollback must revalidate" without forbidding a nil/bypass path;
- missing/unknown policy, capability, auth or context state without declared fail-open/fail-closed semantics;
- a requirement whose intended authorization cannot be represented by the current protocol types/schema;
- acceptance criteria such as "cover failure recovery" without enumerated adversarial scenarios;
- exact file allowlists being treated as semantic authority even when harmless test/helper splits are needed.

DevCadence therefore needs a stronger handoff boundary: the Principal must close implementation-critical semantics before delegation. The target is not to make EWPs longer. The target is to make them denser, mechanically checkable and safe for endpoints with materially weaker architectural reasoning.

## Decision

### 1. Add an Implementation Readiness Gate

A substantial delegated EWP MUST NOT transition from designed/approved to implementation-ready until a Contract Completeness Review produces an explicit readiness result.

The governing question is:

> Could a competent implementation model that knows the language and repository idioms, but has mediocre architectural judgment, implement this contract without inventing any important semantics?

If not, the EWP is not implementation-ready.

This gate applies to all substantial delegated EWPs. Systemic/high-risk work requires the full matrix below; smaller local work may use a reduced form only when omitted dimensions are demonstrably not applicable.

### 2. Zero implementation-critical ambiguity

EWPs have an **ambiguity budget**:

- unresolved architecture choices: **0** before delegation;
- unresolved authority/durability/security/persistence semantics: **0**;
- unresolved missing-input behavior for required inputs: **0**;
- local implementation choices: allowed and explicitly listed.

An implementer may choose names, helper decomposition, private data structures, local idioms and bounded test/helper file layout. It may not choose semantics for public contracts, state transitions, authority, persistence, security, spending, trust boundaries, failure recovery or cross-layer behavior.

### 3. Requirement → invariant → representation → scenario traceability

Each material requirement must be closed through four layers:

1. **Requirement** — externally or architecturally required behavior.
2. **Invariant / state rule** — what must always hold, including before/after/failure states.
3. **Representability binding** — exact type/schema/API/state object that can express and enforce it.
4. **Acceptance scenario** — deterministic or independently reviewable evidence that would fail if the rule were violated.

A requirement with no unambiguous representation is a design defect, not an implementer problem.

### 4. Mandatory contract sections

For substantial EWPs the authoritative Execution Contract includes, as applicable:

#### Invariant table

Stable IDs with concise, testable statements. Example:

| ID | Invariant |
| --- | --- |
| ACT-01 | Every stable state has exactly one authoritative activation. |
| ACT-02 | Active portfolio and lineage identify the same committed activation. |
| ACT-03 | Failure before commit preserves the previous authoritative activation. |
| ACT-04 | Failure after commit yields the new activation or deterministic recovery to it. |

#### Failure matrix

Enumerate meaningful failure points and required postconditions, including crash/restart boundaries where durability is involved.

| Failure | Required behavior | Evidence |
| --- | --- | --- |
| state write fails before commit | previous state remains authoritative | fault-injection test |
| restart with pending intent | deterministic recovery | restart test |
| required policy absent | explicit fail-closed/default behavior | rejection test |

"Handle errors" or "failure recovery" alone is not enough for systemic stateful work.

#### Authority matrix

Identify who/what may decide each material outcome.

| Decision | Authority | Forbidden substitute |
| --- | --- | --- |
| activate portfolio | deterministic validator | model recommendation |
| allow metered fallback | explicit current policy | inferred convenience |
| accept rollback | current validation facts/policy | historical validity |

#### Missing/unknown input semantics

For every required evidence source or control input, define behavior for missing, stale, unknown, malformed or contradictory state.

Default for control-plane/security-sensitive positive requirements: absence of evidence does not satisfy the requirement. Any exception must be explicit.

#### Representability map

Every contract concept maps to the exact current representation:

| Concept | Representation | Status |
| --- | --- | --- |
| metered fallback authority | `BudgetPool.FallbackAllowedToMetered` | represented |
| current inventory freshness | `ExpectedInventoryDigest` | represented |

If representation is absent, contradictory or lossy for the requirement, the Principal MUST amend the protocol/schema, split the work or escalate before delegation.

#### Acceptance scenarios

Acceptance is scenario-oriented, not merely command-oriented. Each scenario names setup, action, expected result and mapped requirement/invariant. Commands such as `go test ./...` remain validation evidence but do not replace semantic scenarios.

### 5. Readiness report

The Principal records a compact readiness report before implementation, for example:

```text
requirements represented: 18/18
mandatory clauses resolved: 11/11
state transitions specified: 7/7
failure cases specified: 14/14
authority decisions specified: 9/9
missing/unknown input semantics: 12/12
acceptance scenarios mapped: 21/21
unresolved architecture choices: 0
declared local-discretion choices: 6
readiness: READY_FOR_IMPLEMENTATION
```

Counts are evidence of coverage, not a substitute for semantic review. A false claim of completeness is still a defect.

### 6. Scope envelopes are semantic, not brittle filename allowlists

An EWP SHOULD authorize bounded **domains/path patterns** plus explicit forbidden semantic changes.

Routine local expansion is allowed when it stays within the declared implementation domain and does not alter semantics, for example:

- splitting or adding tests;
- private helpers;
- package-local support files;
- repository-native refactoring required to realize the exact contract.

Explicit amendment/escalation is required for material semantic expansion, including:

- public/API contract changes;
- protocol/schema meaning;
- persistence or crash semantics;
- security/trust/privacy/spending authority;
- external services/dependencies;
- cross-layer dependency direction;
- invariant changes;
- unrelated subsystem work.

This prevents both unsafe scope creep and pointless bureaucracy.

### 7. The Principal owns semantic closure

The Principal must not hand an implementer a problem that still requires Principal-level reasoning to discover the intended semantics.

For a weaker/local endpoint, the contract should resemble a locally complete programming problem:

- exact obligations resident;
- architecture choices already made;
- edge/failure behavior explicit;
- authority and missing-state behavior explicit;
- repository anchors/evidence retrievable;
- local discretion clearly bounded.

If the Principal cannot close the semantics within the endpoint envelope, it must split, route to a more capable endpoint or escalate. It must not compensate with vague prose or a very large context dump.

### 8. Implementers stop on semantic holes

When implementation reveals any of the following, the correct action is escalation, not plausible invention:

- two normative sources imply incompatible representations;
- satisfying a requirement requires changing an undeclared authority boundary;
- required failure semantics are absent;
- a required concept cannot be expressed by current types/schema;
- a missing/unknown input has no specified behavior;
- the implementation would need a cross-layer or public-contract change not explicitly authorized.

### 9. Typed future contract

The Markdown form is effective immediately. Future typed EWP/contract work SHOULD add versioned structures equivalent to:

- `ContractInvariant`
- `FailureCase`
- `AuthorityRule`
- `InputSemantic`
- `RepresentabilityBinding`
- `AcceptanceScenario`
- `ScopeEnvelope`
- `ImplementationReadiness`

The Cognitive Invocation Compiler should eventually admit the exact execution-ready subset mechanically rather than asking the worker to reconstruct the contract from prose.

### 10. Measure the delegation floor

M4 should evaluate the same implementation-ready contract across endpoints of different capability classes.

A useful metric is the **delegation floor**: the minimum endpoint capability that can implement a contract to acceptance without architectural repair.

If a contract repeatedly succeeds only with frontier coding models but fails with otherwise competent smaller implementers, that is evidence that either:

- the task truly requires higher cognition and routing is correct; or
- the contract compiler/EWP still leaks architecture into implementation.

The goal is to lower the delegation floor without lowering accepted quality.

## Consequences

### Positive

- More defects are discovered before code instead of during review.
- Smaller/local models receive less architectural freedom and more executable semantics.
- Review becomes verification of a closed contract rather than architecture recovery.
- Protocol/type contradictions surface during design.
- Acceptance tests become a second representation of the specification.
- Harmless local code/test organization remains flexible.
- Frontier tokens are spent on architecture once instead of repeated coding repair loops.

### Costs

- Principal authoring becomes more disciplined.
- Stateful/high-risk WPs require explicit matrices and scenarios.
- Some WPs will be split earlier.
- Readiness review can expose protocol defects that expand design work before implementation.

These costs are intentional: discovering the same problem after implementation is more expensive and less reliable.

## Non-goals

- Making every tiny change use every matrix.
- Requiring 50k-token EWPs.
- Removing implementer judgment about local code quality or repository idioms.
- Pretending completeness can be proven by a checklist alone.
- Replacing independent implementation review.

## Example: why this matters

A phrase such as "crash-safe atomic activation" is not implementation-ready by itself.

An implementation-ready contract additionally specifies:

- authoritative state and commit point;
- pre/post invariants;
- each relevant write/failure boundary;
- restart recovery behavior;
- what an error return means if visibility already changed;
- fault-injection acceptance scenarios.

Likewise, "no silent metered fallback" is incomplete until the contract identifies the exact authorization representation and proves that the schema/type model can encode both allowed and forbidden cases.

## Supersession / relationship

This ADR strengthens ADR-0020 §10. ADR-0020 remains authoritative for the Cognitive Invocation Compiler and context admission. This ADR owns implementation-readiness and semantic-closure requirements for delegated EWPs.
