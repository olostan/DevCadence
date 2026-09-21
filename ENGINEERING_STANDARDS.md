# DevCadience Engineering Standards

This document defines implementation standards for the DevCadience codebase. Architectural invariants take precedence over convenience.

## 1. Architectural style

The control plane is a modular monolith first. Do not introduce distributed services merely because the conceptual architecture contains multiple roles.

Preferred initial boundaries:

~~~text
cmd/
  devcadience/
  devcadience-mcp/

internal/
  controlplane/
  protocol/
  state/
  events/
  tasks/
  workpackages/
  evidence/
  agents/
  cognition/
  environment/
  onboarding/
  principalhosts/
  adoption/
  consultants/
  repository/
  worktrees/
  validation/
  health/
  learning/
  policy/
  storage/
  observability/

schemas/
docs/
~~~

Separate packages by durable responsibility, not by hypothetical deployment unit.

The boundaries above are the target shape, not a checklist to create up front.
Package names for future M3/M4 subsystems are directional and should still be
challenged against actual implementation boundaries when those milestones
start.

As of M2 the implemented layout is:

~~~text
cmd/devcadience/            CLI adapter (no domain logic)
internal/
  protocol/                 durable typed contracts (twin of schemas/)
  events/                   event envelope, typed payloads, type registry
  tasks/                    task and attempt state machines (pure)
  state/                    event reducer and ProjectState materialisation
  storage/                  SQLite: migrations, journal, records, projections
  controlplane/             application services and transaction boundaries
  schema/                   JSON Schema compilation and validation
  observability/            structured logging and correlation
  clock/ ids/ errs/         time, identifiers, error taxonomy
  testsupport/              deterministic harness and scenarios
  repository/               Git repository registration and inspection (M2)
  worktrees/                isolated Git worktree manager (M2)
  process/                  controlled external-process runner (M2)
  artifacts/                content-addressed artifact store (M2)
  validation/               validation-profile loading and execution (M2)
~~~

Packages for future milestones (`agents`, `cognition`, `environment`,
`onboarding`, `principalhosts`, `adoption`, `consultants`, `health`,
`learning`, `policy`) are created only when the milestone that needs a real
responsibility boundary arrives. An empty package is not a boundary.

## 2. Primary implementation language

Go is the preferred control-plane language because the project needs:
- a long-running daemon;
- explicit concurrency and cancellation;
- strong type contracts;
- reliable subprocess supervision;
- simple local deployment as one binary;
- efficient CLI/MCP servers;
- straightforward SQLite and Git integrations.

Use Python only where a specific model/runtime integration materially benefits from it and cannot be cleanly accessed via subprocess/HTTP. Keep such integration behind a Go adapter.

## 3. Dependency policy

Prefer standard library and small, mature dependencies.

Every new foundational dependency should answer:
- What capability does it replace?
- What is its security/maintenance profile?
- Does it create provider lock-in?
- Is the dependency required in the core domain or only in an adapter?
- Can the behavior be tested without the dependency?

Model-provider SDKs belong in adapters, never in core protocol packages.

## 4. Domain types over generic maps

Protocol data must use explicit types. Avoid map[string]any for durable domain records.

Examples:
- ProjectState
- EngineeringWorkPackage
- EvidencePacket
- ValidationResult
- ReviewResult
- ReviewCampaign
- FindingDisposition
- ClosureDecision
- EscalationRequest
- DecisionRecord
- LessonCandidate
- TrajectoryManifest

Every durable type has:
- schema_version;
- stable identifier;
- creation timestamp;
- provenance or parent identifiers where applicable.

## 5. JSON Schema is normative at integration boundaries

The Go type and JSON Schema are twin representations of the same contract.

CI must eventually check:
- example fixtures validate against schema;
- serialization round-trips;
- required fields are preserved;
- schema version migrations are tested.

Do not edit only one side.

This is enforced at runtime, not only in CI: a durable record is committed
only after both its typed validation and its serialised document against the
published schema pass, and a record kind with no registered schema cannot be
persisted. The two checks are not redundant — constraints such as string
`format` exist only in the schema, so Go validation alone would let a
malformed timestamp become durable evidence.

## 6. Error handling

Errors must preserve context while remaining machine-classifiable.

Use typed/sentinel categories for conditions the control plane needs to route:
- ErrContradictedAssumption
- ErrValidationFailed
- ErrPolicyDenied
- ErrNeedsPrincipal
- ErrConsultantUnavailable
- ErrModelUnavailable
- ErrWorktreeConflict
- ErrSchemaVersionUnsupported

The implemented taxonomy in `internal/errs` adds the categories the control
plane routes on today: `ErrInvalidTransition`, `ErrInvalidArgument`,
`ErrNotFound`, `ErrConflict`, `ErrIntegrity` and `ErrInternal`. Errors match
by category, so `errors.Is(err, errs.ErrNotFound)` holds regardless of
message, and an error that did not originate in the taxonomy classifies as
`internal` rather than silently acquiring a routable category.

Human-readable messages supplement, not replace, machine-readable status.

Do not use panics for expected runtime errors. The single deliberate exception
is a `crypto/rand` failure while generating identifiers: a process that cannot
generate identifiers cannot produce durable records at all, and continuing
would risk colliding IDs
([ADR-0006](docs/adr/0006-identifiers-and-time.md)).

## 7. Context and cancellation

Every potentially blocking operation accepts context.Context:
- model inference;
- consultant calls;
- Git operations where wrapper permits;
- process execution;
- tests/builds;
- indexing;
- MCP calls.

Cancellation must propagate down process trees where feasible.

## 8. Subprocess execution

All tool execution uses one controlled runner abstraction.

Required inputs:
- argv without shell interpolation by default;
- working directory;
- sanitized/inherited environment policy;
- timeout;
- output-size policy;
- cancellation context.

Required outputs:
- command identity;
- start/end timestamps;
- exit code/signal;
- stdout/stderr artifact references;
- truncation indicator;
- deterministic digest for stored artifacts.

Shell strings are permitted only when unavoidable and must be explicitly marked.

## 9. Git discipline

Do not implement autonomous editing in the main working tree.

Worktree manager requirements:
- create from explicit base SHA;
- deterministic naming;
- lock ownership;
- cleanup after terminal state;
- preserve failed worktrees according to retention policy;
- record resulting commit SHA;
- detect base divergence before integration.

Integration is separate from implementation.

## 10. Persistence

SQLite is the initial control-plane store.

Use explicit migrations. Never auto-mutably “fix” production schema on open without a versioned migration record.

Suggested separation:
- relational current-index tables for efficient queries;
- append-only engineering event records;
- artifact store references for large logs/diffs/model transcripts;
- Git repository remains source of truth for code itself.

Transactions must preserve state-machine invariants.

Durable records are identified within their project. A semantic identifier
chosen by a human or a principal — `PD-001`, `FR-018`, `wp_1` — will be chosen
again by another project, so `project_id` is part of the record key and of
every lookup. Opaque control-plane ULIDs (event, task, attempt) are globally
unique by construction and are keyed globally. See
[docs/adr/0002-control-plane-persistence.md](docs/adr/0002-control-plane-persistence.md).

Persisted evidence is verified on read: every event payload and stored record
is checked against its digest, and a mismatch is an integrity error rather
than a successfully decoded value. Database-level immutability triggers stop
the application from rewriting history; they do not cover a database modified
outside it, and both have to hold.

## 11. Event model

Important transitions emit durable events, for example:
- ProjectInitialized
- ProblemModelRevised
- AmbiguityOpened
- AmbiguityResolved
- ProductDecisionRecorded
- RequirementRecorded
- DiscoveryExperimentStarted
- DiscoveryExperimentCompleted
- SpecificationReviewCompleted
- SpecificationReadinessRecorded
- DesignCandidateCreated
- DecisionRecorded
- TaskCreated
- WorkPackageApproved
- TaskDelegated
- AttemptStarted
- AttemptBlocked
- CandidateProduced
- ValidationCompleted
- ReviewCompleted
- EscalationRaised
- ChangeAccepted
- ChangeRejected
- LessonCandidateCreated
- LessonPromoted
- RefactoringEpochStarted
- ArchitectureReconciled

Events are facts about transitions, not a dump of arbitrary model prose.

The list above is illustrative. The implemented vocabulary additionally
contains `MilestoneStarted`, `ComponentDeclared`, `DecisionRequired`,
`RiskRecorded`, `RiskResolved`, `HealthReportRecorded`, `TaskScoutingStarted`,
`TaskDesignStarted`, `AttemptFailed`, `IntegrationStarted` and
`IntegrationValidationStarted` — each added because a documented ProjectState
field or lifecycle transition had no event to derive it from; see
[docs/adr/0004-canonical-task-state-machine.md](docs/adr/0004-canonical-task-state-machine.md).
`devcadience event types` prints the current vocabulary.

Every event listed above, the discovery ones included, is implemented and
registered, and `tests/doc_drift_test.go` fails if this list ever names one
that is not. Discovery events derive `ProjectState.discovery`; recording one
does not perform discovery, which is a later milestone. `AmbiguityResolved`
carries an outcome distinguishing an answered question from an explicitly
deferred one, because a deferral is valid only behind a safe boundary
(docs/DISCOVERY_AND_SPECIFICATION.md §5).

Every event payload is a registered Go type. An unregistered event type cannot
be appended, and a stored event this build does not recognize is reported
rather than skipped
([ADR-0003](docs/adr/0003-durable-record-compatibility.md)).

## 12. State machines

Task states must be explicit and validated.

The canonical lifecycle is the one drawn in [docs/LIFECYCLE.md](docs/LIFECYCLE.md) §12
and settled by [docs/adr/0004-canonical-task-state-machine.md](docs/adr/0004-canonical-task-state-machine.md):

~~~text
PROPOSED
 -> SCOUTING
 -> DESIGNING
 -> READY
 -> RUNNING
 -> VALIDATING
 -> REVIEWING
 -> ACCEPTED
 -> INTEGRATING
 -> INTEGRATION_VALIDATING
 -> DONE

Any active state may move to BLOCKED.
BLOCKED resumes only through DESIGNING, so unblocking is an explicit design act.
VALIDATING returns to RUNNING for a failed candidate; REVIEWING returns to RUNNING
for a requested repair. Both start a new Attempt.
Retry creates a new Attempt under the same task rather than erasing history.
~~~

The complete edge table, the block semantics and the ProjectState task-bucket
mapping are in ADR-0004. `devcadience task states` prints the implemented
machine.

Illegal transitions return errors and do not partially mutate state. In the
control plane this is enforced by the enclosing SQLite transaction: a refused
transition commits neither the event nor the projection update.

## 13. Agent adapters

A model adapter exposes capabilities, not provider-specific concepts, for example:
- GenerateStructured
- RunAgentSession
- Count/estimate context
- Cancel
- CapabilityProfile

Worker harnesses may be separate adapters:
- raw chat/tool loop;
- Ollama-compatible agent;
- MLX-LM local process;
- OpenHands;
- Goose;
- Aider;
- Codex CLI;
- Claude Code.

The control plane must not assume one harness.

## 14. Capability profiles

Routing should be empirical.

Track per model/profile:
- supported context;
- tool reliability;
- structured-output reliability;
- language/framework strengths;
- latency distribution;
- memory footprint;
- recent task success rate;
- review precision/recall from evaluated history.

Do not encode current model marketing claims as permanent architecture.

## 15. Prompt and skill assets

Prompts are versioned source artifacts.

Each role prompt should specify:
- objective;
- allowed authority;
- forbidden authority;
- expected structured output;
- evidence requirements;
- escalation behavior;
- context budget guidance.

Prompt changes that affect behavior require tests/evals like code changes.

## 16. Observability

Every task/attempt/model call receives correlation identifiers:
- project_id
- task_id
- attempt_id
- work_package_id
- agent_run_id
- evidence_packet_id
- consultation_id where relevant

Use structured logs. Avoid dumping full prompts or secrets by default.

Metrics should distinguish:
- local inference tokens/time;
- frontier quota/API use;
- repository tokens scanned when measurable;
- attempts/retries;
- validation failures;
- reviewer disagreement;
- escalation rate;
- wall-clock duration;
- accepted-without-frontier-review rate.

## 17. Determinism

Control-plane behavior should be deterministic for the same durable inputs wherever model calls are not involved.

Use:
- stable sorting;
- explicit timestamps from injected clock in tests;
- deterministic fixture IDs;
- explicit random seeds for randomized evaluation;
- canonical JSON when hashing artifacts.

## 18. Testing strategy

Required test layers:

### Unit tests
State transitions, policy, schema conversion, routing calculations, command builders, parsers.

### Contract tests
Each adapter against a fake provider/runtime plus optional integration tests against a real local runtime.

### Repository fixture tests
Small synthetic Git repos that exercise:
- worktree isolation;
- merge conflicts;
- compile/test failure;
- diff/evidence extraction;
- stale base detection.

### End-to-end bootstrap tests
A tiny target repository where the full scout -> Work Package -> implement -> validate -> review path can be replayed.

### Evaluation suites
Frozen trajectories used to compare prompts, cognition endpoints, reviewers and routing policies.

## 19. Test doubles

Do not require a running LLM for ordinary unit tests.

Create deterministic fake agents that:
- return fixture EvidencePackets;
- deliberately contradict assumptions;
- simulate malformed output;
- timeout;
- generate known review disagreements.

The hard control-plane logic must be testable offline.

## 20. Concurrency

Bound concurrency explicitly.

The scheduler considers:
- unified-memory/VRAM/RAM pressure for local inference;
- local model load/unload cost;
- worktree exclusivity;
- CPU/GPU contention;
- test-suite resource conflicts;
- remote-provider quota/rate limits;
- source-exposure policy;
- monetary cost class.

Correctness beats throughput.

## 21. Cognition endpoint resource management

Local runtime adapters should expose, where supported:
- loaded model;
- verified acceleration/backend;
- estimated/observed memory;
- context configuration;
- active sessions;
- unload/load operations.

Remote/API/CLI cognition adapters should expose, where supported:
- endpoint/model identity;
- readiness/auth state;
- rate-limit/quota information;
- context/tool capability;
- cancellation/session state;
- usage/cost metadata.

A 48 GB Apple Silicon machine is a reference strong-local profile, not a
bootstrap prerequisite. Local resource policy should preserve OS/IDE/build
headroom instead of filling all available memory with weights.

Cognition scheduling is capability/policy configuration, not a hard-coded
assumption about one machine, runtime, provider or inference locality.

## 22. Security

Never pass repository-originated instructions into higher-authority prompts as trusted directives.

Mark external data sections clearly.

Credentials:
- use OS/keychain/environment indirection;
- redact logs;
- do not persist secrets in trajectories;
- separate provider auth from prompt content.

See docs/SECURITY.md.

## 23. Documentation standards

Normative documents use “MUST”, “SHOULD”, “MAY” consistently.

Major docs should begin with scope and authority.

Examples should be realistic and versioned when machine-consumed.

Do not copy generated documentation into multiple mirrors that can drift. Link to one canonical source.

## 24. Change-size discipline

Prefer small coherent changes, but do not mechanically optimize line count.

A change should be decomposed when:
- it has independent acceptance criteria;
- it crosses unrelated architectural concerns;
- it makes review evidence difficult to interpret;
- rollback would need independent control.

## 25. Performance

The project does not initially optimize for minimum wall-clock latency.

Performance priorities:
1. correctness;
2. reproducibility;
3. bounded resource use;
4. reliability;
5. operator clarity;
6. throughput;
7. latency.

Measure before optimizing.

## 26. Backward compatibility

Protocol versioning must define:
- reader compatibility;
- writer compatibility;
- migration behavior;
- unknown-field behavior.

Stored historical trajectories cannot become unreadable after routine releases.



## 27. Review convergence discipline

Substantial review uses a bounded ReviewCampaign.

Standards:
- broad reviewers inspect one immutable candidate before repair where practical;
- reviewer findings are normalized evidence, not direct implementation commands;
- the principal/adjudicator deduplicates findings and records FindingDisposition;
- all FIX_NOW findings for a round are consolidated into one Repair Work Package;
- focused revalidation checks accepted repairs and regressions rather than restarting broad review;
- the reopen threshold must not decrease as the campaign converges without explicit policy exception;
- closure review reports only threshold-crossing issues;
- a frozen campaign reopens only on materially new evidence or changed requirements;
- reviewer finding count and repair rounds are policy-bounded;
- hitting the repair-round bound with unresolved blockers escalates rather than continuing autonomously;
- reviewer/consultant transcripts are stored by reference and are not copied into every subsequent model context.

A zero-finding closure review is valid. Reviewers MUST NOT manufacture findings to demonstrate activity.

See docs/REVIEW_AND_CONVERGENCE.md.
