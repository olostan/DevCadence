# DevCadence Implementation Plan

## Scope

This roadmap turns the architecture into a sequence of falsifiable milestones. Each milestone has a purpose, deliverables and verification gate.

The project should not proceed merely because code exists. Each milestone proves a capability needed by the next.

## Milestone map

```mermaid
flowchart LR
    M0["M0<br/>Normative baseline"]
    M1["M1<br/>Domain + state core"]
    M2["M2<br/>Repository execution"]
    M3["M3A/B<br/>Environment intelligence<br/>+ deterministic bootstrap"]
    M3C["M3C<br/>Cognition resource<br/>+ session substrate"]
    M3D["M3D<br/>Adaptive portfolio<br/>+ workflow synthesis"]
    M4["M4<br/>Adaptive vertical slice<br/>+ evidence gate"]
    M5["M5<br/>Semantic principal<br/>+ host portability"]
    M6["M6<br/>Project bootstrap<br/>+ adoption"]
    M7["M7<br/>Reviews + consultants"]
    M8["M8<br/>Health/refactoring"]
    M9["M9<br/>Learning/evaluation"]
    M10["M10<br/>Autonomous campaigns"]

    M0 --> M1 --> M2 --> M3 --> M3C --> M3D --> M4 --> M5 --> M6 --> M7 --> M8 --> M9 --> M10
```

M3 is deliberately split into four bounded phases: M3A (read-only
environment/cognition discovery), M3B (safe deterministic bootstrap), M3C
(provider-neutral cognition resource/session substrate), and M3D
(AI-assisted portfolio/workflow synthesis plus the richer adaptive setup
experience).

**M4 is an evidence gate, not merely the next feature milestone.** It tests
the central adaptive-cognition hypothesis before DevCadence invests in broad
principal-host productization, brownfield reconstruction, and later autonomy.
If the experiment shows that simpler workflows consume fewer scarce resources
for equivalent accepted quality, M3D policy/topology should be revised before
M5+ scope proceeds unchanged.

M5 productizes semantic principal integration and host portability. M6 owns
project bootstrap as a product-sized subsystem: greenfield technology selection/
materialization plus brownfield Project Adoption and reconstruction rather than
hiding either inside host integration. M7-M10 then build
review convergence, engineering health, evaluated learning, and long-running
campaigns in that order.

An additional sub-phase, M2.5, sits between M2 and M3: it extends M2's
repository/worktree/process/validation foundation with the module, tool,
service-supervision and context-compaction infrastructure that M3's cognition
runtime and M5's execution-agent roles need. See "M2.5 — Declarative modules,
bounded execution tools, supervised services, and context compaction" below.


## M0 — Normative architecture baseline

### Goal
Make the intended system sufficiently explicit that implementation agents do not invent foundational semantics.

### Deliverables
- vision;
- requirements;
- architecture;
- lifecycle;
- invariants;
- protocols;
- principal/local-agent contracts;
- verification;
- security;
- refactoring/learning;
- implementation plan;
- initial JSON Schemas.

### Verification
- cross-document terminology review;
- all linked documents exist;
- schema examples parse;
- Mermaid diagrams render on GitHub;
- no contradictory invariant/protocol definitions.

### Exit criterion
A fresh capable engineer/model can explain the system and M1 boundaries without relying on chat history.

## M1 — Domain core and canonical state

**Status: complete.**

### Goal
Implement model-independent control-plane primitives.

### Deliverables
- Go module and CLI skeleton — `cmd/devcadence`;
- protocol/domain types, including discovery/specification records —
  `internal/protocol`;
- ProblemModel/AmbiguityLedger/ProductDecision/Requirement/DiscoveryExperiment/
  SpecificationReadiness persistence foundations — the immutable record store
  in `internal/storage` plus their typed representations;
- SQLite migration framework — `internal/storage`;
- engineering event journal — `internal/events`, `internal/storage`;
- ProjectState reducer/materialized view — `internal/state`;
- task/attempt state machine — `internal/tasks`;
- fixture/test framework — `internal/testsupport`, `fixtures/`, `tests/`;
- schema validation tooling — `internal/schema`, `devcadence schema validate`;
- the `ProjectState.review` projection *shape* — `protocol.ReviewConvergenceState`,
  paired with the schema property so the two representations accept the same
  documents. Nothing reduces into it; M3C adds compact durable review-state
  primitives, while full multi-review campaign orchestration, aggregation and
  closure automation remain M7 (ADR-0010 / ADR-0020).

### Verification
- unit tests for legal/illegal transitions — `internal/tasks`, including a
  walk of the full state cross product;
- event replay reconstructs same ProjectState — `internal/state`,
  `internal/controlplane`;
- crash/transaction tests for append + projection — injected failure inside a
  transaction, and a refused transition leaving the journal untouched;
- schema round-trip tests — `tests/schema_fixtures_test.go` over
  `fixtures/protocol/`;
- no LLM required to run test suite — nothing in the tree imports a model
  runtime.

Run with `go test ./... && go vet ./...`, or `make verify`.

### Exit criterion
A synthetic project can be driven through task states deterministically.

Met: `TestSyntheticProjectReachesDoneDeterministically` drives a project from
`PROPOSED` to `DONE` twice and compares canonical ProjectState byte for byte,
and `TestCLIDrivesASyntheticProjectToDone` does the same through the CLI.

### Discovery and specification records

The six Day-0 contracts introduced by
[adr/0001-discovery-specification-subsystem.md](adr/0001-discovery-specification-subsystem.md)
— ProblemModel, AmbiguityLedger, ProductDecision, Requirement,
DiscoveryExperiment and SpecificationReadiness — have typed representations in
`internal/protocol` and persist through the same immutable, versioned,
content-addressed record store as every other protocol record. Their semantic
guards are enforced on the write path, not left to prose:

- a Requirement may be `confirmed` only when it traces to a human, directly or
  through a ProductDecision — the principal cannot confirm its own inference
  (DCI-005);
- a ProductDecision's authority is always `human`;
- a resolved ambiguity must record its resolution;
- a completed experiment must report a result;
- a readiness verdict must agree with its own checks, and an unknown that is
  not safe to defer past architecture forbids any verdict but `not_ready`.

The eight discovery events named in ENGINEERING_STANDARDS.md §11 are
implemented and registered, and `ProjectState.discovery` is reduced from them,
so FR-D-012 holds: a new principal session can reconstruct current product
intent from durable records rather than a conversation transcript. The
derivation rules are in docs/PROJECT_STATE.md §17.1.

Recording these facts is M1; *performing* discovery is not. The adaptive
questioning loop (FR-D-006), human reflection (FR-D-007), external grounding
(FR-D-008), running experiments (FR-D-009) and independent specification
review (FR-D-010) require cognition endpoints from M3 and the semantic principal
surface from M5. M1 guarantees that when those arrive, the state they produce
is already durable, typed and reconstructable.

### Architectural decisions taken during M1
- [adr/0002-control-plane-persistence.md](adr/0002-control-plane-persistence.md)
- [adr/0003-durable-record-compatibility.md](adr/0003-durable-record-compatibility.md)
- [adr/0004-canonical-task-state-machine.md](adr/0004-canonical-task-state-machine.md)
- [adr/0005-deterministic-project-state-identity.md](adr/0005-deterministic-project-state-identity.md)
- [adr/0006-identifiers-and-time.md](adr/0006-identifiers-and-time.md)

### Debt deliberately carried into later milestones
- Appending an event replays the project journal to rebuild the projection
  (O(n) per write). The fix — a snapshot plus tail replay — is additive and
  changes no durable contract (ADR-0002, R-M1-01).
- The SQLite pool is capped at one connection, serialising reads with writes.
- `integrating` and `integration_validating` exist and are reachable but have
  no repository behaviour until M2.
- Git facts are not reducer inputs: `git.accepted_commit` comes from recorded
  events, and `dirty` and `branch` are unset (ADR-0005, R-M1-05).
- `capabilities` is typed but always empty until M3.
- `LessonCandidateCreated`, `LessonPromoted`, `RefactoringEpochStarted` and
  `ArchitectureReconciled` are recorded and derive nothing (M7/M8).
- No artifact store: `ArtifactRef` describes where artifacts will live, and
  nothing writes them yet (M2).
- The discovery workflow is absent: the records, events and projection exist,
  but nothing *performs* discovery — no questioning loop, no human reflection
  round, no experiment execution, no specification review. Those require M3
  cognition capability and the M5 semantic principal surface.
- Experiment and review events are recorded but not projected, because the
  `discovery` object in the schema carries no counts for them.

## M2 — Repository, worktree and process execution

**Status: complete.**

### Goal
Safely operate on real repositories.

### Deliverables
- repository registration — `internal/repository`;
- Git inspection — `internal/repository` (HEAD, branch, status, ancestry,
  merge-base, diff, non-mutating merge/conflict checks);
- isolated worktree manager — `internal/worktrees`;
- controlled process runner — `internal/process`;
- artifact capture — `internal/artifacts`;
- validation profiles — `internal/validation` (profile loading and
  execution against the real M1 `protocol.ValidationResult`/
  `ValidationCompleted`);
- candidate commit/diff metadata — `internal/repository.Repository.Diff`,
  `CheckMerge`, `StaleBase`, surfaced through `devcadence candidate show`.

### Verification
Synthetic fixture repositories only (`internal/testsupport.NewGitRepo`),
never the DevCadence repository itself:
- parallel worktree isolation — `TestParallelWorktreesAreIsolated`,
  `TestConcurrentCreateSameProject`;
- compile/test success/failure — `internal/process`, `internal/validation`
  (`TestRunNonzeroExitIsNotAnError`, `TestRunProfilePassAndFail`);
- timeout/cancellation — `TestRunTimeout`, `TestRunCancellation`,
  `TestRunnerReusableAfterTimeout`, `TestRunProfileTimeout`;
- stale base detection — `TestStaleBase`, `TestIsStale`;
- merge conflict — `TestCheckMergeConflict`, `TestCheckMergeCleanNonFastForward`
  (both assert the accepted working tree is untouched);
- stdout/stderr truncation — `TestRunStdoutTruncation`;
- path/symlink security — `TestRegisterSymlinkedPathCanonicalises`,
  `TestRegisterSubdirectoryOfRepositoryRejected`,
  `TestCreateRejectsPathTraversalIdentifiers`,
  `TestOpenRejectsPathTraversalLocator` (artifacts);
- digest-verified validation lineage — `TestExecuteAndRecordWrongCommitRefused`
  proves a validation citing the wrong commit is refused with the journal
  and task state left untouched, reusing the M1 transaction/digest
  machinery rather than adding new checks.

Run with `go test ./... && go test -race ./... && go vet ./...`, or `make verify`.

### Exit criterion
DevCadence can safely run deterministic engineering work on real
repositories without an LLM.

Met: `internal/validation`'s `TestExecuteAndRecordAttemptScopeDrivesTaskToReviewing`
and its sibling tests drive a task from a delegated Work Package through a
candidate commit, a real `ValidationResult` produced by executing a profile
against a synthetic repository, and a matching `ValidationCompleted` event —
entirely through `internal/repository`, `internal/worktrees`,
`internal/process`, `internal/artifacts` and `internal/validation`, with no
model runtime imported anywhere in the module (`tests.TestNoPackageDependsOnAModelRuntime`
still passes).

### Architectural decisions taken during M2
- [adr/0007-repository-and-worktree-safety-model.md](adr/0007-repository-and-worktree-safety-model.md)
- [adr/0008-controlled-process-execution.md](adr/0008-controlled-process-execution.md)
- [adr/0009-artifact-storage-and-validation-execution.md](adr/0009-artifact-storage-and-validation-execution.md)

### Debt deliberately carried into later milestones
- The worktree manifest is a per-project JSON file guarded by an
  in-process mutex. It is safe for one daemon process and is not
  cross-process safe; the bootstrap posture (docs/SECURITY.md §17) does not
  yet require more than one (ADR-0007).
- `CheckMerge`'s scratch worktree is created outside the worktree manager's
  own accounting; a crash between its creation and its cleanup can leak an
  entry in Git's own `worktree list` for the primary repository. It is
  inspectable (`git worktree list`) and does not touch the accepted branch.
- Integration-scope validation is executable but *integration orchestration*
  (deciding which accepted candidates combine, in what order, into what
  integration commit) remains M6/M9; M2 supplies the deterministic
  mechanics an orchestrator will call.
- The process runner does not sandbox `Spec.Dir`; confinement to a worktree
  is structural (only `internal/worktrees.Manager` hands out worktree
  paths), not OS-enforced. See ADR-0008.
- `devcadence validate`/`run`/`candidate show` are inspection and
  operator/demo commands, not the eventual M3/M4 agent-facing execution
  surface; a future milestone's agent runtime calls
  `internal/validation`/`internal/process` directly, not the CLI.

## M2.5 — Declarative modules, bounded execution tools, supervised services, and context compaction

### Goal

Give execution-cognition roles (`Repository Scout`, `Implementer`) safe, bounded,
monorepo-aware ways to read, search, and validate a repository — and give
long-running execution sessions a way to stay within context budgets — without
enlarging the Principal's own interface. This is infrastructure M3's cognition
runtime and M5's execution-agent roles need; it is not part of M3B (guided
bootstrap), which is unrelated onboarding/setup work.

### Status

Implemented on branch `feat/modules-tools-services` (PR #7), reviewed and
repaired through several rounds; not yet merged to `main`.

### Deliverables
- **Declarative modules & scoped worktrees** (ADR-0015): `ModuleDefinition`/
  `ModuleCatalogRecorded` in `internal/protocol`/`internal/state`; module
  inheritance and precedence across profile/check/service, with unknown
  module IDs rejected before process startup; directory containment checks
  against parent traversal and symlink escapes, including ancestor-directory
  symlinks — `internal/tools/scope.go`, `internal/validation`.
- **Bounded execution tools** (ADR-0016 §1, §3): `read_file` (opt-in line
  numbers, byte caps), `fetch_content` (universal paginator over immutable
  content-addressed artifacts, `lines`/`bytes` units, contiguous cursors),
  `grep_search` (ripgrep/git-grep/pure-Go fallback, 20-result cutoff,
  automatic pagination) — `internal/tools`.
- **Supervised validation services** (ADR-0016 §2): `ServiceSupervisor` in
  `internal/validation` with port allocation modes (`env_var`, `cli_flag`,
  `socket_inheritance`), isolated temp dirs, readiness probing, `MaxLifetime`
  bounds, process-group reaping, and start-time-based restart reconciliation
  against PID recycling.
- **Asynchronous operations** (ADR-0016 §2): `OperationManager` in
  `internal/process` — 10s response-yield threshold, background continuation
  under original timeouts, idempotent cancellation.
- **Multi-tier context compaction** (ADR-0016 §4): admission-safe budgeting
  against an endpoint's `MaxRequestTokens`; Tier 1 deterministic tool-result
  pruning (soft watermark); Tier 2 episodic trajectory summarization (hard
  watermark) with atomic tool-call/result groups preserved across the split
  and a final admission guard on every path — `internal/compaction`.
- **Syntactic symbol inspection** (ADR-0016, WP6): `find_symbol` over native
  Go AST, and regex-based syntactic matching for TypeScript/JavaScript with
  honest `backend` reporting (`go/ast` vs `syntactic-regex`, never
  `tree-sitter`) and comment/string-literal masking — `internal/tools`.

### Verification

No structured `ValidationResult`/evidence-bundle tooling is expected here —
this is pre-M1-adoption infra work on DevCadence's own repository, verified
the ordinary way:
- `go test -count=1 ./...` and `go test -race ./...` — pass;
- `go vet ./...` — pass;
- `GOOS=windows GOARCH=amd64 go build ./...` and `go vet ./internal/validation/...`
  — pass (the latter required isolating `syscall` usage, including in test
  code, behind `//go:build unix` / `!unix` files);
- targeted regression tests per finding, e.g. `TestReadFileWorktreeContainment`,
  `TestServicePrematureExit`, `TestRunProfileModuleInheritanceAndOverride`,
  `TestClosureKeepsProtectedToolGroupWhole`, `TestTier2DigestModelVerifiedFlagIsUntrusted`,
  `TestClosureBlankLinesPreservedInTotals`.

### Explicitly deferred (see ADR-0016's delivered-vs-deferred table)
- artifact-backed live validation streaming with 4 KiB previews (sinks are
  decoupled now; the daemon/task-runner milestone wires them to
  `internal/artifacts`);
- durable, SQLite-backed asynchronous-operation events across daemon
  restarts (current `OperationManager` is in-memory, same-process only);
- BPE tokenizers and independent summarizer-input admission for compaction
  (current admission uses a selected-field/byte heuristic against the
  endpoint ceiling);
- Cgo Tree-sitter grammar parsing for symbol inspection (TypeScript/JavaScript
  stays on the honestly-labeled regex backend);
- full cross-platform executable-path verification for service/process
  restart ownership (current check is start-time matching, which is
  fail-closed against PID recycling but not identity-verified).

### Architectural decisions taken during M2.5
- [adr/0015-declarative-modules-and-scoped-worktrees.md](adr/0015-declarative-modules-and-scoped-worktrees.md)
- [adr/0016-validation-services-bounded-tools-and-context-compaction.md](adr/0016-validation-services-bounded-tools-and-context-compaction.md)

### Note on principal exposure

Per ADR-0016 §1, `read_file`, `grep_search`, `find_symbol` and `run_command`
are execution-agent capabilities scoped to isolated worktrees for the
`Repository Scout`/`Implementer` roles, not part of the Principal's own
interface — the Principal still only sees the semantic operations listed in
AGENTS.md §3 and M5's deliverables. No MCP server exists yet to expose any
tool externally (that is M5 work), so this boundary is currently structural
(nothing outside `internal/tools`'s own tests calls these functions) rather
than enforced by a wire-level contract.

ADR-0016's 2026-09-23 amendment (see the ADR) splits this further into two
evidence tiers M5 must carry forward: `grep_search`/`find_symbol` results are
compact, citable evidence `request_evidence` may fetch for the Principal
directly; `read_file`/`fetch_content` remain execution-agent-only, reachable
by the Principal only through a bounded, execution-agent-mediated snippet
request — never as an open-ended file-reading tool.

## M3 — Environment intelligence, cognition runtime, and guided bootstrap

### Goal

Make DevCadence adaptive to the machine and AI tooling it actually finds,
rather than assuming strong local hardware, Ollama/MLX, one subscription, or a
preinstalled principal host.

Local-first means local control-plane/repository authority. Model inference may
be local or remote according to capability, privacy, cost and policy.

### M3A — Environment intelligence + cognition runtime

**Status: implemented.** See
[adr/0013-environment-intelligence-and-cognition-contracts.md](adr/0013-environment-intelligence-and-cognition-contracts.md)
for the durable contracts it settled, and `internal/environment`,
`internal/cognition`, `internal/principalhosts` for the implementation.

Two deliverables below are narrower than the heading suggests, deliberately:

- The **remote-API** kind ships as the adapter boundary plus a deterministic
  client rather than a provider implementation. The domain question — can a
  remote API be discovered, described, health-checked, cost-classed,
  privacy-constrained and routed like any other endpoint? — is answered by the
  boundary, and implementing one vendor's HTTP surface would have added a
  dependency and a credential path without proving anything further.
- **Measured capability profiles** cover *operational* properties: does the
  endpoint answer, does it emit valid JSON once, how long did it take, what did
  the runtime report about tokens and memory. Reasoning and coding quality
  require evaluation history, which no milestone has built yet, so those grades
  come from explicit operator declaration or stay `unknown`.

#### Deliverables
- hardware/environment discovery:
  - OS/distribution/architecture;
  - CPU/RAM/storage;
  - Apple/NVIDIA/AMD/Intel accelerator candidates;
  - relevant device/runtime/permission facts;
- local runtime discovery/adapters, initially including practical Ollama and
  MLX-LM paths where supported;
- actual acceleration verification through a real inference probe rather than
  "GPU/runtime present" inference;
- backend-candidate assessment (Metal/MLX, CUDA, ROCm, Vulkan, CPU fallback as
  applicable);
- CognitionEndpoint abstraction across:
  - local runtimes;
  - authenticated coding/agent CLIs;
  - remote APIs;
- endpoint health/auth/capability discovery;
- measured capability profiles and lightweight microbenchmarks;
- capability-based role routing;
- privacy/source-exposure and cost-class inputs to routing;
- support for no-local-model operation.

#### Verification
- blank machine fixture;
- runtime absent;
- runtime installed but CPU-only fallback;
- supported acceleration verified empirically;
- unsupported/uncertain ROCm with viable Vulkan candidate;
- Apple Silicon native/accelerated path;
- authenticated coding CLI discovered;
- endpoint unhealthy/auth expired;
- local-small usable while strong-local unavailable;
- remote implementation selected only when policy permits;
- no-local-model profile remains operational for deterministic/local control
  plane capabilities.

#### Exit criterion
DevCadence can describe the machine and available cognition endpoints from
observed evidence, verify local acceleration where configured, and route roles
without assuming a strong local coder exists.

**Met.** Every verification case above is covered by a deterministic test against
fixture machines in `internal/environment/fixtures.go`; the suite needs no GPU, no
runtime, no Python, no credentials and no network. The read-only proof surface is
`devcadence environment inspect`, `cognition list`, `cognition probe` and
`cognition route`.

### M3B — Guided deterministic bootstrap and onboarding

**Status: complete.** M3B owns safe discovery/cache/readiness, setup
mutation, credential references, bounded runtime/model/tool recipes and the
minimal CLI interaction needed to use those capabilities safely. It does not
hard-code a final cognition-organization strategy.

M3B's canonical output for later recommendation is deterministic
**ResourceInventory** plus readiness evidence. Deployment labels may be shown
as descriptors but are not routing configuration. A polished/rich interactive
setup UI is intentionally deferred until M3D, when the portfolio semantics it
must explain are known.

#### Deliverables
- `devcadence doctor` and `devcadence setup` CLI surfaces (`plan`, `apply`, `recover`);
- safe `SetupPlan` / approval / append-only ledger semantics (`internal/protocol/setup.go`, `setup-plan.schema.json`, `internal/setup/ledger.go`);
- modular discovery/remediation for hardware, local inference, cognition
  interfaces, principal hosts and auth;
- runtime-agnostic local-model setup boundary with MLX-LM and Ollama as peer
  implementations (`internal/setup/ollama_adapter.go`, `internal/setup/mlx_adapter.go`);
- opaque secret-safe credential references and reuse of existing authenticated sessions (`internal/credentials/`, `credential-ref.schema.json`, `auth-evidence.schema.json`);
- deterministic ResourceInventory + readiness projection (`doctor-report.schema.json`, `resource-inventory.schema.json`);
- plain / `--json` / basic-terminal operation with minimal confirmations and
  SSH-safe fallbacks; no rich TUI is required for M3B completion.

#### Verification
- 8-dimension milestone closure suite in `tests/m3b_milestone_closure_test.go`:
  1. Blank machine fixture producing valid DoctorReport and ResourceInventory without panic (DCI-104, DCI-105);
  2. Dry-run mutation visibility with explicit filesystem mutations (DCI-108);
  3. Privileged/high-impact actions failing closed without explicit authority (DCI-108);
  4. Interrupted setup recovery via real CLI (`setup recover`) reconciling ledger conflicts (ADR-0014 §6);
  5. Preference for existing usable tools requiring 0 actions for satisfied environments (DCI-105, ADR-0014 §7);
  6. Plain/non-interactive operation emitting zero ANSI escape sequences and failing closed without approval;
  7. SSH/basic-terminal `--no-tui` deterministic compatibility (ADR-0018);
  8. Honest degradation of readiness and ResourceInventory when optional endpoints/hardware are degraded.
- Credential evidence integrity and schema-to-Go twin parity across all 8 M3B record schemas (`tests/twin_fields_test.go`);
- Comprehensive 23-dimension matrix tests in `internal/setup/matrix_test.go` and full CLI tests in `cmd/devcadence/cli_doctor_test.go` and `cmd/devcadence/cli_setup_test.go`.

#### Exit criterion
A user can safely discover/configure at least one viable cognition path when
possible and obtain an auditable ResourceInventory/readiness state without
needing to understand accelerator/runtime/provider details. The workflow is
usable from plain terminals and automation; rich adaptive onboarding is not an
M3B gate.

**Met.** Verified across unit, matrix, CLI end-to-end, and milestone closure suites without requiring GPU, cloud credentials, or interactive TUI. All 8 M3B schemas pass `TestSchemaTopLevelFieldsMatchTheGoTwin` (top-level schema properties matched against their canonical Go structs — not a deep/nested field-by-field check).

### M3C — Cognition resource and session substrate

#### Goal
Create the provider-neutral deterministic substrate that represents how
cognition can be accessed, constrained, budgeted and invoked, without yet
asking AI to choose the portfolio.

#### Deliverables
- provider-neutral AccessChannel and session-driver capability contracts;
- ContextProfile / ContextManifest / ContextPack / EvidenceLease shapes, versioned schemas and EWP companion compatibility;
- Cognitive Invocation Compiler with deterministic task/role/action/path/domain/risk applicability mapping, dependency-closed exact mandatory clause admission, projection freshness and complete-contract admission/reserves (`CONTEXT_UNFIT`);
- optional-context retrieval baseline: exact/lexical + dependency graph traversal; dense embeddings/reranking are not required M3C implementation and are evaluated experimentally in M4;
- endpoint-specific prompt-renderer interface so canonical protocol/state stays format-neutral while model-facing task/contract/evidence/action projections can be calibrated;
- context budget/reference linting for role cores, contracts and compiled packs, plus honest measured/estimated/unknown telemetry and expansion/restart handling;
- provider-neutral context-control capabilities: `ContextControl = ExactStateless | AppendOnly | OpaqueSession` and `PrefixCache = Explicit | Implicit | SessionKV | None` (ADR-0019 §1);
- `Evidence Working Set` lease manager with content-addressed provenance `(file_path, content_digest, start_line, end_line)`, automatic freshness invalidation on file mutation, and server-side path authorization;
- `RefactoringProposal` protocol definition in Go and JSON Schema (`internal/protocol/`, `schemas/refactoring-proposal.schema.json`) enabling bottom-up upstream challenge (ADR-0019 §3);
- EconomicRegime, BudgetPool and optional dynamic BudgetState / ResourceState;
- explicit privacy/spending/source-exposure/reserve/preference/diversity policy;
- versioned CognitionPortfolio / PortfolioRecommendation / WorkflowPlan
  protocol shapes and activation/versioning primitives;
- WP-M3C-2A session execution substrate (**medium** planning size): authenticated CLI/SDK/API/local drivers, two materially different real driver shapes plus a fake adapter contract, structured/streaming events, resume/cancellation/tool capabilities and silent resource metering;
- WP-M3C-2B Cognitive Invocation Compiler (**large** planning size): deterministic admission/mappings/dependency closure, renderers, ContextPack enforcement, state capsule, evidence leases, expansion/restart handling and lexical/graph retrieval. 2A/2B are independently acceptable; M4 compiler/context experiments require 2B, and either slice must be split again rather than silently expanding beyond its EWP boundary;
- deterministic portfolio validator covering endpoint identity/capability
  provenance, source exposure, spending/overage, budget bindings, driver
  features and resource constraints;
- separate WP-M3C-5 durable review-ledger foundation [Implemented - WP-M3C-5]: stable ReviewFinding identity, author fix-attempt/challenge response, independent resolution verification with record-level self-verification rejection and a derived resolution state, decoupled from WP-M3C-2 session/compiler delivery; Go twins for the existing campaign/disposition/closure records, ClosureDecision wiring, verifier projection/blinding, normalization and rich fan-out/aggregation remain M7;
- no silent subscription/local → metered API fallback.

M3C is deliberately deterministic infrastructure. It may validate an explicit
operator-authored or fixture portfolio, but AI portfolio synthesis belongs to
M3D.

#### Verification
Exercise at least two materially different real driver shapes plus a fake third
provider/driver contract; local and remote access; subscription and metered
economic regimes; missing/unknown quota; endpoint loss; source-exposure and
spending denial; resume/cancellation capability differences; and a machine
with no cognition endpoint. Prove that a mandatory clause with low semantic
similarity is still admitted, mandatory admission classes have reverse coverage,
optional retrieval cannot displace required authority, provisional profiles do
not masquerade as calibrated evidence, hostile evidence delimiters cannot escape
their PromptProjection container, and an oversized mandatory pack returns
`CONTEXT_UNFIT`. Substrate integration verification across heterogeneous portfolios and known gaps (KG-1..KG-5) is verified in WP-M3C-4 (`tests/m3c_substrate_test.go`). Separately, WP-M3C-5 proves review finding/resolution state
survives a clean-session handoff without permitting author self-verification.

#### Exit criterion
Given ResourceInventory + policy + an explicit candidate portfolio, DevCadence
can represent sessions/economics faithfully, deterministically validate or
reject that portfolio, and explain why. The companion WP-M3C-5 review ledger
can also preserve normalized finding -> resolution -> independent verification
state across clean sessions without author self-verification. No provider/model
family or billing channel is structurally privileged.

### M3D — Adaptive portfolio and workflow synthesis

**Status: complete.** M3D owns AI-assisted portfolio planning, workflow topology planning, adaptation diffing/rollback, plain/JSON CLI UX, and cross-portfolio verification. Verified across Apple Silicon/MLX, NVIDIA CUDA local-only, single-subscription, multi-subscription, paid-API allowed/forbidden, mixed portfolios, budget exhaustion, resource additions, and future driver extensibility in `tests/m3d_cross_portfolio_test.go`. Milestone M3D exit criteria are satisfied.

#### Goal
Use eligible cognition to recommend how the discovered resources should be
organized, while keeping activation authority deterministic and adapting the
workflow topology rather than assuming more agents are always better.

#### Deliverables
- AI-assisted Portfolio Planner usable through any sufficiently capable
  eligible endpoint;
- typed portfolio alternatives with rationale, tradeoffs, confidence and
  provenance;
- deterministic validation before any recommendation can activate;
- task-specific Workflow Planner that may collapse to one capable session or
  expand into multiple independent roles based on task/risk/resources;
- incremental re-recommendation when resources, policy, budget pressure or
  evidence change;
- explicit diff/apply/rollback/versioning for portfolio changes;
- adaptive setup/explain UX, including optional rich terminal rendering only
  after the portfolio semantics are stable;
- cross-portfolio verification across local, subscription, metered and mixed
  configurations.

#### Verification
Exercise strong Apple Silicon + MLX, NVIDIA/local runtime, one subscription
CLI/no local model, multiple subscriptions/no API spending, explicit metered
API budget, mixed portfolios, no cognition endpoint, resource removal/quota
constraint, and adding a new subscription. Verify both topology expansion and
topology collapse. A fake new provider/session driver must participate without
core role/workflow changes.

#### Exit criterion
Given supported ResourceInventory and policy, DevCadence can recommend,
validate and activate a useful CognitionPortfolio or explain why cognition is
unavailable; derive a bounded task WorkflowPlan; and present the choice without
silently expanding spending, privacy exposure or execution authority.

## M4 — Adaptive cognition vertical slice and evidence gate

### Status: CLOSED WITH DOCUMENTED CAVEATS (2026-10-05)
The M4 harness, corpus, campaign matrix and fail-closed gate are implemented
(WP-M4-4 PR #73, WP-M4-5 PR #74, WP-M4-6 PR #75). The gate evaluates to
`DecisionGo` on **synthetic harness evidence only**: the committed
report is [`docs/evidence/m4-evidence-report.md`](evidence/m4-evidence-report.md),
produced by a scripted deterministic driver (kind
`synthetic_harness_validation`). It validates the measurement and decision
machinery. It is **not** empirical proof of the M4 product hypothesis, and no
real local, subscription-CLI or frontier-API endpoint was measured.

**Mandatory re-evaluation after M5.** Once M5 allows real API/CLI/local-LLM
operation, run a bounded real-endpoint campaign (Strategy 1 vs Strategy 4
within each capability tier, unobservable resources recorded as unknown)
through the same gate with provenance kind `empirical_campaign`. If that run
returns Revise or Inconclusive, or contradicts the synthetic result, revise
admission/profile/routing/topology policy or gate criteria through the normal
EWP change process. The synthetic `DecisionGo` MUST NOT be cited to justify
current policy against real evidence. This re-evaluation is M5 exit work and
may not be silently dropped.

### Goal
Test the central product hypothesis **before** broad host/product/adoption
investment: can adaptive allocation of heterogeneous cognition resources,
combined with compressed engineering artifacts and deterministic controls,
preserve or improve accepted engineering quality while reducing scarce-resource
consumption versus simpler coding-agent workflows?

M4 is intentionally allowed to use a controlled/reference orchestration
harness. Full semantic-host portability is M5, and full brownfield adoption is
M6; neither should block learning whether the adaptive cognition thesis is
worth productizing.

The M4 harness MUST preserve the workflow-execution boundary from ADR-0025:
`WorkflowPlan` is the logical engineering contract, while native scheduling
and runtime-private decomposition remain execution-mechanism concerns. M4 does
not need a generic executor plugin system, but reusable orchestration code must
not make the native scheduler the definition of WorkflowPlan semantics.

The reference/native executor should be the simplest implementation sufficient
for the M4 experiments: single-node/process-local, direct scheduling of ready
stages, and reuse of existing session/process/worktree/validation primitives.
Independent stages may execute in any order consistent with binding dependencies
and constraints; `WorkflowStage.Order` is deterministic plan ordering, not an
implicit serial-scheduling edge. Do not add distributed task stores, leases,
generic event fabrics or other speculative runtime machinery solely for future
compatibility. Execution placement remains non-semantic (DCI-161).

### Experiment
Compare direct strong coding-agent and other simple baselines with
DevCadence-generated WorkflowPlans under strong-local,
one-subscription/no-local, multiple-subscriptions/no-paid-API, metered-remote
and mixed local/subscription/API portfolios. The role graph need not be
identical across scenarios, and "one capable session + deterministic checks"
is a valid DevCadence outcome.

Benchmark the **Adaptive Context Architecture + Cognitive Invocation Compiler**
(ADR-0019 / ADR-0020) against standard conversational agent baselines across
seeded defect suites:
- Strategy 1: Full conversational history (traditional agent loop);
- Strategy 2: Multi-tier context compaction (ADR-0016);
- Strategy 3: Static prefix + active snippet pool;
- Strategy 4: Hybrid 4-layer context (Protected Core, Cognitive State Capsule, Evidence Working Set, Ephemeral Tail).

Empirically validate working hypotheses:
- Workload-specific effective context and instruction/constraint adherence across endpoint/runtime/quantization/context configurations, without assuming a universal degradation boundary;
- Provider cognitive diversity across language idioms, concurrency, schemas, and invariants;
- Working-memory budget sweeps (e.g. 6k, 12k, 24k) with full-reserve accounting and complete-contract admission, including contracts too large to fit;
- Working-set versus full-corpus/compaction baselines on representative implementation and documentation/code review tasks across a small local coder, larger local model, subscription CLI and frontier API where available. Missing endpoint classes are explicit limits of the evidence;
- **Delegation-floor experiments (ADR-0024):** execute the same implementation-ready EWP against materially different implementation-capability classes (for example small local coder, Qwen-class ~30B local coder where available, subscription coding CLI, frontier coder). Measure first-pass acceptance, architectural repair findings, Principal re-entry and repair rounds. Distinguish tasks that intrinsically require stronger cognition from contracts that leak unresolved architecture. Pre-declare falsification criteria (no significant reduction in repair rounds/architectural findings despite full semantic closure falsifies the hypothesis that contract completeness alone can lower the delegation floor for that workload class). The goal is to lower the minimum endpoint capability that can safely reach acceptance without lowering quality;
- rule-selection strategies: deterministic admission classes/mapping plus lexical/graph retrieval, compared with optional dense embeddings/reranking experiments; measure mandatory-rule false negatives separately from optional-context relevance;
- mandatory-admission ground truth independent of the compiler under test: seeded omitted/mis-mapped mandatory rules plus held-out task applicability sets established by an independent full-corpus Principal/human audit; report unmapped and confidently mis-mapped false negatives separately;
- prompt renderers (for example compact tagged Markdown/XML-like sections versus JSON/YAML where supported) by endpoint/configuration rather than assuming a universal best serialization;
- effective cognitive load in addition to tokens: active normative-clause count, evidence-object count, dependency depth and structured-output adherence;
- Held-out tasks and repeated runs with predeclared quality/defect-yield criteria; do not tune and validate on the same tasks or treat token savings alone as success. Separate endpoint/layout and task-decomposition effects, report configuration and opaque-session accounting limitations.

### Measurements
- accepted correctness, regressions and human corrections;
- subscription/quota consumption;
- initial/peak resident tokens and protected/contract/normative/state/leased-evidence components, reserves, cumulative input/cached/output, reloaded evidence and counting provenance/unknowns;
- expansion/eviction/restart counts, coverage gaps, false positives and missed seeded defects;
- metered API spend and token volume where observable;
- local inference/compute use and working-memory footprint;
- cognition invocation/session count;
- defect catch rate across review vectors;
- stale-evidence error rates and duplicate reads/rediscovery;
- review findings before/after normalization, duplicates, fix attempts, challenge outcomes, verification failures, repair rounds, newly discovered material closure findings and Principal re-entry;
- source exposure and wall time;
- whether a simpler topology would have produced the same accepted result;
- total resource-to-accepted-result.

### Exit criterion
DevCadence demonstrates useful outcomes across materially different portfolios
without dependence on local inference, one provider, one subscription model or
one fixed role topology. If adaptive orchestration routinely consumes more
scarce resources than a simpler baseline without quality benefit, revise M3D
routing/topology policy before broadening product scope. M4 is a real
go/revise gate, not a ceremonial demo. Working-set context must preserve predeclared accepted-quality and defect-yield criteria while improving useful resource efficiency; otherwise revise admission/profile/decomposition policy. No claimed percentage reduction is an exit requirement or an established result.

## M5 — Semantic principal integration and host portability

**Preparation status (2026-10-05):** the [2026-10-F window](work-packages/window-2026-10-f-overview.md) is a draft design candidate. It explicitly records missing production task/review execution and empirical candidate verification as follow-on work; transport connectivity and synthetic benchmark results do not satisfy this milestone's exit. No M5 implementation or live campaign is claimed by the planning documents.

### Goal
Productize the semantic principal boundary after the adaptive cognition
hypothesis has passed or been revised through M4. Allow a capable principal to
operate through compact semantic operations without requiring direct
repository browsing.

Antigravity is the reference integration. Cursor and Visual Studio Code are
the other initial first-class principal hosts. No one host is a core-domain
dependency.

### Deliverables
- stdio MCP adapter with no-argument `devcadence-mcp` launch contract;
- host-neutral semantic principal contract;
- PrincipalHost adapter boundary;
- first-class integration support for Antigravity plus at least one of Cursor
  or Visual Studio Code, with all three remaining intended first-class hosts;
- host discovery/compatibility/configuration consumed from M3;
- principal instructions/skills/rules;
- semantic operations for project_state, investigate, create_work_package,
  delegate, task_status, validate, review, request_evidence and accept/reject;
- Discovery Principal semantic operations;
- Day-0 persistence operations for product decisions, requirements and
  readiness.

- **M4 real-evidence re-evaluation** (carried over from M4 closure): run a bounded real-endpoint campaign through the M4 gate with provenance `empirical_campaign` and revise policy per the M4 status section if it does not hold.

### Verification
- principal initializes from compact ProjectState;
- repository is not required in strict principal workspace mode;
- targeted source evidence retrieval works;
- unauthorized raw operations are not exposed;
- stale state/work package rejected;
- Antigravity integration smoke test;
- Cursor or VS Code portability proof through the same semantic contract;
- blank-host setup can guide/configure a selected supported host.

### Exit criterion
A principal can plan and drive one task through the semantic interface without
directly browsing the repository, and the core contract is demonstrably not
Antigravity-specific.

See [PRINCIPAL_HOSTS.md](PRINCIPAL_HOSTS.md),
[ANTIGRAVITY_INTEGRATION.md](ANTIGRAVITY_INTEGRATION.md), and
[MCP_API.md](MCP_API.md).

## M6 — Project Bootstrap, Capability Packs, and Adoption

### Goal

Create or adopt a repository into a trustworthy DevCadence-managed project using a horizontal ecosystem-capability model rather than language/framework-specific core branches.

M6 begins **after the M4 core-hypothesis/evidence gate**. M6 must not be pulled forward in a way that delays proving the compact-principal + bounded-execution + deterministic-verification hypothesis.

M6 has three implementation slices so the platform contract lands before broad adoption/scaffolding behavior.

### M6A — Pack, mechanics, and health core

Deliverables:

- one PackRegistry abstraction;
- ProjectCapabilityPack manifest/schema;
- Agent Skills-compatible pack knowledge;
- built-in, home-library, project-local, and explicit GitHub/HTTPS pack loading through one contract;
- durable pack identity/origin/revision/content-digest/provenance;
- explicit PackUpgradeProposal/revalidation flow;
- compatibility/dependency/conflict resolution;
- compositional ToolchainProfile capabilities per ADR-0015 module;
- mechanical manifest for environment, build/typecheck, run/dev, test, lint/static-analysis, coverage, generated-code, install/device/deploy, and result adapters;
- generic structured result/finding interchange and explicit opaque-pass/fail tier;
- pack conformance harness;
- seeded-failure checks for important health mechanics;
- ProjectHealthContract;
- base-governed candidate acceptance ("candidate cannot rewrite its own judge");
- capability-separated successor-policy approval channel outside the worker tool/shell/API envelope;
- candidate-accurate evidence;
- finding-fingerprint no-new-debt baseline/ratchet;
- explicit baseline reconciliation across pack/tool/rule upgrades;
- managed hook/CI adapter provenance/drift detection;
- initial trust-profile presets while preserving always-on integrity;
- Cognitive Invocation Compiler admission of pack Skills without double injection or authority expansion;
- at least two genuinely different reference packs (for example Go and Flutter).

### M6B — Brownfield adoption and pack synthesis

Deliverables:

- adoption source-commit pinning;
- deterministic repository/document/tool inventory;
- existing-document harvest/classification;
- source/test/schema/configuration/history reconstruction;
- ambiguity/contradiction ledger;
- canonical documentation baseline under docs/devcadence/ by default;
- native build/test/lint/coverage/CI mapping;
- pack recommendation from observed stack;
- LLM-synthesized candidate pack for unsupported/bespoke stacks;
- conformance/seeded-failure/Skill-trial validation of synthesized packs;
- inherited debt/finding baseline;
- project invariant candidate reconciliation;
- AdoptionDecision and readiness gate;
- guard preventing normal managed implementation/acceptance/integration before READY.

Existing good native tooling/documentation is preserved/mapped rather than rewritten for stylistic consistency.

### M6C — Greenfield project bootstrap

Deliverables:

- TechnologyOptionSet comparison for material stack choices;
- explicit separation of technical fit from DevCadence supportability;
- TechnologyDecision/ADR evidence;
- versioned ProjectBlueprint;
- bounded greenfield EWP for initial materialization;
- pack-guided Agent Skills and ecosystem-native generator/model execution;
- observed generator/tool version/argument provenance when available;
- explicit partial/unsupported capability handling;
- optional candidate-pack synthesis during bootstrap;
- post-materialization module/capability resolution;
- initial ProjectHealthContract and canonical documentation;
- deterministic readiness gate before normal feature work.

DevCadence does **not** maintain a universal ScaffoldRecipe/template registry. The durable contract is architecture + EWP + pack knowledge + observed execution evidence + deterministic acceptance.

### Verification

M6A:
- Go and Flutter reference packs;
- structured and opaque result tiers;
- seeded lint/test failure detection;
- pack source pinning and successor upgrade;
- compositional module capabilities;
- candidate attempting to weaken its own health contract;
- worker attempts to self-approve the successor through CLI, writable state, daemon/control-plane API, subprocesses, reused credentials/tokens, or trust-profile mutation; all attempts MUST fail until an out-of-band authority approves;
- finding-fingerprint debt ratchet;
- permissive and stricter trust presets;
- Skill projection without double injection.

M6B:
- no-docs and stale-doc brownfield fixtures;
- native-tool preservation;
- unsupported stack;
- LLM-synthesized unfamiliar "Zeta" pack;
- conformance catching a hallucinated/no-op command;
- generated Skill trial;
- accepted-risk debt/readiness.

M6C:
- backend service;
- Flutter/mobile app;
- polyglot project;
- technically preferred but partially supported stack;
- native generator use;
- direct model-created baseline;
- post-bootstrap health failure/repair.

### Exit criterion

DevCadence can:

1. load/synthesize ecosystem capability packs through one producer-neutral contract;
2. preserve exact pack lineage and safely propose later upgrades;
3. map a project/module to compositional capabilities and a canonical health contract;
4. prevent a candidate from weakening its own acceptance judge;
5. adopt an imperfect brownfield repository with bounded debt;
6. turn a specification-ready greenfield product into a bounded EWP-created, health-verified managed baseline;
7. do all of this without adding ecosystem-specific branches to core orchestration/state/authority for an ordinary new stack.

See PROJECT_CAPABILITY_PACKS.md, PROJECT_CREATION_AND_SCAFFOLDING.md,
PROJECT_ADOPTION.md, PROJECT_TOOLCHAINS_AND_HEALTH.md, ADR-0012,
ADR-0015, and ADR-0021..0023.

## M7 — Multi-review and consultant cognition

### Goal
Add cognitive diversity where it has leverage, with bounded convergence rather
than an AI committee that can reopen work forever.

### Deliverables
- multiple review dimensions;
- dual independent review fan-out on clean starting contexts across model families (ADR-0019 §5);
- dynamic review lenses (`anti_rabbit_hole`, `anti_drift`, `anti_hallucination`) guiding reviewer attention without schema churn (ADR-0019 §4);
- active falsification probes (`FalsificationProbe` / mutation testing) executed by deterministic validation runners in isolated worktrees (ADR-0019 §4);
- bounded ReviewCampaign orchestration over the durable Review Ledger foundation from M3C;
- Aggregator normalization/deduplication and synthesis into accepted repair work, preserving stable finding identity and independent ResolutionVerification;
- "Double-Green" adjudication fast-path for rapid Principal approval when dual independent reviews and deterministic validation pass;
- rising reopen thresholds and repair-round limits, including enforcement of
  `TaskDelegated.max_attempts`;
- focused revalidation and ClosureDecision freeze semantics;
- reviewer finding budgets and compact review-state handoff;
- disagreement reports and risk-based review policy;
- consultant abstraction;
- at least one real external consultant adapter under
  `ConsultationRequest`/`ConsultationResult`, distinct from evidence
  acquisition;
- optional external research/evidence-acquisition service per ADR-0017,
  distinct from the consultant abstraction and not required for M7's other
  deliverables;
- anti-anchoring independent-consultation mode;
- Design Readiness Gate;
- independent specification-review dimensions and consultant-assisted
  ambiguity discovery;
- Specification Readiness evaluation using local/consultant review evidence.

### Verification
Seeded defect suite; parallel reviewers on the same immutable candidate;
deduplication before repair; one consolidated repair package per round;
focused revalidation without broad-review restart; thresholded closure;
frozen-campaign reopening only for materially new evidence; repair-round
escalation; disagreement routing; blind consultant request; consultant
unavailable behavior; and security/redaction policy.

## M8 — Engineering health and refactoring

### Goal
Prevent feature throughput from degrading architecture.

### Deliverables
- deterministic longitudinal health metrics over the M6 ProjectHealthContract
  baseline;
- structural metrics and trend snapshots;
- semantic health reviews;
- Refactoring Epoch state/process;
- Architecture Reconciliation workflow;
- refactoring Work Package templates;
- health/debt reduction campaigns and policy-evolution proposals.

M8 does **not** introduce the first build/test/lint/coverage policy for a
managed project. Minimum deterministic mechanical health is established during
M6 project bootstrap/adoption; M8 reasons about trends, structural debt and
architecture over that trusted substrate.

### Verification
Seed a fixture project with intentional smells and verify detection, epoch
planning, behavior-preserving refactor, full regression and before/after health
comparison.

## M9 — Learning and evaluation

### Goal
Improve the engineering system from evidence without hidden policy mutation.

### Deliverables
- trajectory manifests;
- LessonCandidate lifecycle;
- frozen evaluation corpus;
- prompt/portfolio/workflow routing experiments;
- promotion/rollback;
- endpoint-access-role-task outcome metrics;
- resource-to-accepted-result metrics across subscription, API and local
  regimes;
- evaluated recommendations for portfolio changes without silent policy
  mutation.

### Verification
Demonstrate one evaluated improvement: candidate derived from real failure,
replay/evaluation, versioned promotion, future task uses promoted knowledge,
and rollback works.

## M10 — Long-running autonomous campaigns

### Goal
Allow milestone-scale execution with principal/human intervention only when
valuable.

### Deliverables
- dependency-aware scheduler;
- overnight/background task queue;
- integration planning;
- principal decision queue;
- human decision queue;
- optional dashboard;
- resumable daemon.

### Verification
Run a multi-task milestone with parallel independent tasks, dependency blocks,
local retry, principal escalation, integration conflict, refactoring trigger
and daily summary.

## Suggested bootstrap repository structure

```text
cmd/
  devcadence/
  devcadence-mcp/
internal/
  protocol/
  state/
  events/
  storage/
  tasks/
  policy/
  repository/
  process/
  worktrees/
  agents/
  models/
  validation/
  evidence/
  consultants/
  health/
  learning/
  observability/
schemas/
prompts/
skills/
docs/
tests/
fixtures/
```

## Implementation ordering inside M1

1. project/config type;
2. IDs/time abstraction;
3. schema types;
4. SQLite store/migrations;
5. event append/read;
6. task state machine;
7. ProjectState reducer;
8. CLI inspect commands;
9. deterministic test fixtures.

Do not start model integration before these basics are trustworthy.

This ordering was followed. The one deviation: a small immutable record store
was added alongside the event journal, because `WorkPackageApproved` would
otherwise reference a blueprint nothing had stored. Events carry the compact
facts ProjectState needs and reference full protocol documents by id and
digest.

### M2 assumptions validated by M1

- Attempt lineage (state revision, Work Package version, base commit, worker
  profile, worktree, candidate commit, artifacts) is representable and
  persisted before any repository machinery exists, so M2 does not have to
  retrofit it.
- Task state, attempts and blocks are derived from events, so M2 can add
  repository behaviour without inventing a second source of truth.
- `ArtifactRef` (locator plus digest) is the agreed boundary for logs, diffs
  and transcripts, so the artifact store can be built without touching the
  relational schema.

## Path toward distributed and multi-user DevCadence

While current milestones focus on a single-developer, local-first control plane, DevCadence preserves explicit architectural seams for future distributed and multi-user operation:

- **Multiple Collaborative Actors**: Multiple developers, local workers, remote/cloud cognition runners, CI automation agents, and delegated independent reviewers working on the same repository.
- **Explicit Provenance and Authority Layers**: The architecture preserves a path for every invariant, contract, and finding to carry explicit `AuthoritySource` provenance (`system | organization | project | task`). Organization-level policies can be composed without altering engine invariants or target project baselines.
- **Control-Plane Effect Authority**: Capabilities represent execution effect authority (`write`, `exec`, `credentials`, `network`, `spending`, `state`) verified and attached by the deterministic control plane, preventing models from self-granting or negotiating privileges.
- **Actor/Identity Decoupling**: The `ContextManifest` and context pack admission architecture naturally accommodate future human identity, actor signatures, and RBAC tiers without requiring structural redesign or premature IAM/PKI complexity in early milestones.

## Definition of milestone done

Every milestone completion must include:
- passing required tests;
- documentation synchronized;
- no known invariant violations;
- explicit deferred debt;
- demo/repro steps;
- verification report;
- bounded review/repair campaign for substantial changes;
- no open closure-threshold findings;
- explicit residual-risk disposition;
- ClosureDecision/freeze once convergence machinery is implemented;
- next milestone assumptions validated.

Milestone completion does not require that no reviewer can imagine another improvement. See docs/REVIEW_AND_CONVERGENCE.md.
