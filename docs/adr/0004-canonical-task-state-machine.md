# ADR-0004: Canonical task state machine, block semantics and ProjectState task buckets

- **Status:** Accepted
- **Date:** 2026-09-21
- **Decision owner:** Principal
- **Supersedes:** —
- **Superseded by:** —
- **Related invariants:** DCI-024, DCI-025, DCI-032, DCI-044, DCI-045
- **Related tasks:** M1 — Domain core and canonical state

## Context

Two normative documents describe the delivery task lifecycle and they are not
identical:

- **ENGINEERING_STANDARDS.md §12** lists `PROPOSED → SCOUTING → DESIGNING →
  READY → RUNNING → VALIDATING → REVIEWING → ACCEPTED → INTEGRATING → DONE`,
  plus `BLOCKED` from any active state. It introduces the list as "an
  illustrative lifecycle".
- **docs/LIFECYCLE.md §12** draws a state diagram that additionally contains
  `ValidatingIntegration` between `Integrating` and `Done`, and specifies
  particular edges: `Validating → Running` for bounded repair, `Reviewing →
  Running` for a requested local fix, `Reviewing → Blocked` for a
  contradiction or unresolved disagreement, and `Blocked → Designing`.

Independently, `schemas/project-state.schema.json` requires
`tasks.{ready,running,blocked,awaiting_principal}` but no document says which
task state lands in which bucket, and `awaiting_principal` corresponds to no
state at all.

Implementing a state machine without settling these would embed an arbitrary
choice in durable data.

## Verified facts

- ENGINEERING_STANDARDS.md §12 labels its lifecycle "illustrative"; §1 of the
  same document defers to architecture documents on structure.
- docs/README.md §"Normative hierarchy" ranks approved architecture and
  protocol documents above engineering standards.
- docs/LIFECYCLE.md §12 is the only place where the edges are enumerated.
- Neither document draws an edge out of `Blocked` other than to `Designing`.
- Neither document defines `awaiting_principal`.

## Assumptions

| Assumption | Status | Material |
| --- | --- | --- |
| The LIFECYCLE diagram is intended as the detailed form of the standards list, not a competing design | verified | yes |
| A task always has exactly one state; concurrency lives in attempts | verified | yes |

## Decision criteria

- resolve the contradiction rather than pick silently (docs/README.md);
- prefer the interpretation consistent with INVARIANTS.md;
- illegal transitions fail deterministically and mutate nothing;
- a block always records why and who can clear it;
- a retry adds an attempt and never erases one.

## Alternatives considered

### Option A — Adopt the ENGINEERING_STANDARDS list (11 states)

**Benefits**
- Shorter; integration validation collapses into `INTEGRATING`.

**Costs / risks**
- Loses the distinction between "combining the change" and "validating the
  combined result", which is exactly where docs/LIFECYCLE.md §12 places a
  failure edge (`ValidatingIntegration → Blocked: conflict / regression`).
- Discards detail from the higher-ranked document to match a list that calls
  itself illustrative.

**What would invalidate it**
- Nothing; it was rejected as the weaker reading.

### Option B — Adopt the LIFECYCLE diagram (12 states) — selected

**Benefits**
- Consistent with the normative hierarchy.
- Keeps the integration failure edge distinguishable from an implementation
  failure edge, which matters for FR-018 and for M2's integration work.

**Costs / risks**
- One more state to carry before M2 can exercise it.

**What would invalidate it**
- An ADR that redefines integration as a single step.

### Blocked resume: Option B1 (`Blocked → Designing` only) — selected

The alternative was to allow a blocked task to resume into the state it was
blocked from. Rejected: DCI-024 forbids a local agent from silently overriding
a MUST, and a resume that skips design would let a block be cleared without
anyone revising the blueprint that caused it. Routing every resume through
`DESIGNING` makes unblocking an explicit, recorded design act. The state the
task blocked from is preserved on the block reason, so no history is lost.

### `awaiting_principal`: Option B2 (derive it; allow overlap) — selected

Alternatives were (i) make the buckets mutually exclusive, and (ii) add an
explicit `AWAITING_PRINCIPAL` state. (i) loses information: a task blocked on
a principal decision is both "what is stuck" and "what is mine to unstick",
and dropping either answer degrades a different operator question in
docs/OBSERVABILITY.md §2. (ii) duplicates `BLOCKED` and multiplies edges for
no semantic gain.

## Decision

**1. The canonical lifecycle is docs/LIFECYCLE.md §12, with twelve states:**

```
proposed → scouting → designing → ready → running → validating
        → reviewing → accepted → integrating → integration_validating → done
```

`blocked` is reachable from every active state. `done` is terminal; `blocked`
is not.

**2. The complete set of legal edges:**

| From | To |
| --- | --- |
| `proposed` | `scouting`, `designing` |
| `scouting` | `designing` |
| `designing` | `ready` |
| `ready` | `running` |
| `running` | `validating` |
| `validating` | `running` (bounded repair), `reviewing` |
| `reviewing` | `running` (repair requested), `accepted` |
| `accepted` | `integrating` |
| `integrating` | `integration_validating` |
| `integration_validating` | `done` |
| `blocked` | `designing` |
| any active state | `blocked` |

Anything else is an `invalid_transition` error and mutates nothing.

**2a. A legal transition is not sufficient authority.** The adjacency list
says which state changes are possible; it does not say which event may cause
one. Several states are reachable by more than one edge — RUNNING from READY,
VALIDATING and REVIEWING — so an event guarded only by "this transition is
legal" can move a task along an edge it was never meant to act on. A failing
attempt-validation, for instance, could pull a task out of REVIEWING and
bypass the rejection decision that is supposed to send it back. Each
lifecycle event therefore names the state it requires, in addition to the
transition being legal.

**2b. Lineage is checked, not trusted.** An event that references a task, an
attempt, a Work Package, a candidate commit or a piece of evidence must refer
to something that exists and belongs to the same lineage. DCI-032 requires
every candidate change to have lineage, and docs/OBSERVABILITY.md §13 says an
acceptance the system cannot explain means the acceptance mechanism is
incomplete — so an acceptance citing another task's attempt, a commit the
attempt did not produce, or evidence that was never recorded is refused
rather than stored as a decision that merely looks justified. Specifically:

- an **attempt** may only start against the Work Package version the task
  approved, because the attempt record is the durable claim about what the
  work was executed against;
- an **attempt validation** requires its task in VALIDATING, an attempt of
  that task, a candidate actually produced, and a commit matching that
  candidate — the commit is required, not optional, so the evidence names
  what it is about and cannot be read as covering a superseded candidate;
- a **review** requires its task in REVIEWING and an attempt of that task
  that produced a candidate;
- an **acceptance** requires all of the above plus: the candidate commit
  matches the attempt's, the named Work Package is the task's approved one,
  the attempt ran against that same version, at least one validation id and
  at least one review id are cited, and every cited validation and review id
  names evidence recorded for *that* task and *that* attempt.

  The two minimum-one rules come from DCI-032: a candidate change has, at
  minimum, validation results, review results and a decision. Deterministic
  validation and independent review are separate signals (DCI-040), and
  neither substitutes for the other. If review evidence could be empty a
  task could pass through REVIEWING without any review having happened,
  which would make the state ceremonial. *Which* review dimensions and how
  many are required — correctness alone for a local change, correctness plus
  architecture for a systemic one, further vectors for security-sensitive or
  architectural work — is an M6 policy decision keyed on change class and
  risk. The domain floor is one; policy raises it;
- a **rejection** carries the same attempt and candidate requirements minus
  the evidence citations;
- an **escalation** that cites an attempt must cite one of the task's own.

These lineage checks are journal-internal by construction: the reducer is
pure and cannot open the record store. That an evidence id also names a
durable `ValidationResult` or `ReviewResult` that really exists, with the
digest the event claims, is verified in the control-plane transaction before
the event is appended (ADR-0002 §4d). Lineage without that check would let a
fabricated validation id and digest pass every test here and still support an
acceptance resting on nothing.

Validation and review evidence is indexed reducer-internally by id, carrying
only the task, attempt and candidate it belongs to. That index is not part of
ProjectState: docs/PROJECT_STATE.md §1 keeps the principal's view compact, and
the full documents live in the record store behind their ids.

**3. Blocks preserve why.** `BLOCKED` always carries a typed reason with a
trigger, a statement, a decision authority, optional evidence references, the
attempt that surfaced it, and the state the task blocked from. A blocked task
without a reason, or a non-blocked task carrying one, is an integrity error.
`EscalationRaised` is the only event that blocks a task.

**4. Attempts are separate from task state.** `AttemptBlocked` terminates an
attempt without blocking the task, because docs/LIFECYCLE.md §13 has the
control plane verify a contradiction before escalating. A terminal attempt is
never reopened; a retry starts a new attempt with the next ordinal, and
previous attempts, including their block reasons, remain.

**5. Repair versus retry are counted separately.** `RepairIterations` counts
the worker's internal fix-and-recheck cycles *before* it produced its outcome,
and is reported by the worker rather than derived from task transitions.
Attempt ordinal counts retries *across* attempts.

A failed attempt validation returns the task to `RUNNING` and the repair runs
as a **new** attempt; it does not reopen the terminated one. Producing a
candidate terminates the attempt that produced it, and a terminated attempt is
historical fact. The two counters bound different things — how long one worker
may iterate, versus how many times the task may be re-delegated (FR-016) —
which is why neither substitutes for the other.

**6. Reviews do not move the task.** `ReviewCompleted` records evidence. A
task leaves `REVIEWING` only through `ChangeAccepted` or `ChangeRejected`,
because DCI-044 forbids collapsing several reviewers' verdicts into an
automatic outcome.

**7. ProjectState bucket mapping:**

| State | Buckets |
| --- | --- |
| `proposed` | — (untriaged; not yet work) |
| `scouting`, `running`, `validating`, `reviewing`, `integrating`, `integration_validating` | `running` |
| `designing` | `awaiting_principal` |
| `ready` | `ready` |
| `accepted` | — (transient control-plane step) |
| `done` | — (reported through milestone progress and recent semantic changes) |
| `blocked`, authority `principal` | `blocked` **and** `awaiting_principal` |
| `blocked`, any other authority | `blocked` |

Buckets list task **aliases** (`DC-012`), not opaque identifiers: the
principal reasons about aliases.

**8. Event-to-transition mapping.** Each transition is caused by exactly one
event type. Where docs/LIFECYCLE.md named a transition but the illustrative
event list in ENGINEERING_STANDARDS.md §11 had no event for it, M1 adds one:
`TaskScoutingStarted`, `TaskDesignStarted`, `IntegrationStarted`,
`IntegrationValidationStarted`, and `AttemptFailed` (for an attempt that ends
with neither a candidate nor a reportable contradiction). §11's list is
explicitly open-ended ("for example"); these additions fill named gaps rather
than introducing new concepts.

## Rationale

The contradiction is resolved in favour of the more detailed document because
the normative hierarchy says so and because the extra state carries real
information the shorter list drops. The remaining choices — single resume
path, overlapping buckets, reviews as evidence — each follow from an invariant
rather than from convenience.

## Consequences

### Positive
- One adjacency list is the single source of truth; no other package encodes
  an edge.
- A blocked task always names a decision owner, so it cannot be silently
  abandoned.
- Attempt history accumulates, satisfying DCI-032 lineage.
- Buckets are derived in one place, so CLI and (later) MCP cannot drift.

### Negative
- `integration_validating` has no behaviour until M2 and is currently only
  reachable by appending its event explicitly.
- A task blocked on a principal decision appears in two buckets, which a
  consumer that sums bucket sizes must account for.

### New risks
- **R-M1-04 (low):** the added event types are M1 inventions filling gaps in
  an illustrative list; if M2 finds a better decomposition of integration,
  they would need superseding while historical events stay readable.

## Implementation guidance

- `internal/tasks` holds the states, the adjacency list, `CheckTransition`,
  the attempt status machine and `Buckets`. It is pure — no storage, no
  events — so legality is testable in isolation.
- `internal/state` maps events to transitions in one `switch`; events stay a
  vocabulary of facts.
- Transition legality is checked before any mutation; the enclosing SQLite
  transaction makes the "mutates nothing" guarantee durable.

## Verification plan

- `TestEveryLegalTransitionIsPermitted` and
  `TestOnlyDeclaredTransitionsArePermitted` — the second walks the full state
  cross product, so a widened adjacency list fails.
- `TestIllegalTransitionsAreCategorised`, `TestDoneIsTheOnlyTerminalState`.
- `TestBucketMapping`.
- `TestBlockPreservesReasonAndSurfacesToThePrincipal`,
  `TestResumeCreatesANewAttemptAndKeepsTheOld`.
- `TestAttemptStatusTransitions`, `TestBlockedReasonRequiresADecisionOwner`,
  `TestTaskBlockedStateAndReasonMustAgree`.
- `TestIllegalTransitionLeavesPersistenceUntouched` — the durable half.
- `internal/state/lineage_test.go` — the adversarial set: validation or
  acceptance citing another task's attempt, a nonexistent attempt, a different
  candidate commit, a different Work Package, unrecorded evidence ids,
  another task's evidence, and integration evidence cited as attempt
  evidence; plus `TestFailedValidationCannotPullATaskOutOfReview` for the
  state-guard hole and `TestAttemptCannotRunAgainstAnUnapprovedWorkPackage`
  for the origin of the Work Package lineage.
- `TestBrokenLineageLeavesPersistenceUntouched` — the durable half of the
  lineage rules: a refused acceptance advances neither the journal nor the
  projection.

## Rollback / supersession strategy

Changing the lifecycle means a new ADR plus a reducer change. Because task
state is derived from events rather than stored as the source of truth, a
redefinition can be applied by re-reducing existing journals, provided the
event vocabulary is preserved or migrated.

## Follow-up

- [x] Record the canonical lifecycle in ENGINEERING_STANDARDS.md §12.
- [x] Record the bucket mapping in docs/PROJECT_STATE.md.
- [ ] Exercise `integrating` and `integration_validating` against real
      worktrees (M2).
