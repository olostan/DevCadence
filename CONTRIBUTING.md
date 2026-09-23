# Contributing to DevCadence

DevCadence welcomes changes that strengthen the central idea: deep, grounded frontier reasoning should create durable engineering guidance, while local agents perform high-volume repository work under explicit evidence and verification contracts.

## Before contributing

Read README.md, INVARIANTS.md, AGENTS.md and the relevant architecture/protocol documentation.

If your change contradicts an invariant, do not code around it. Propose an ADR explaining why the invariant should be changed or superseded.

## Types of contribution

Typical contributions include:
- control-plane implementation;
- protocol/schema evolution;
- model/runtime adapters;
- MCP integration;
- repository/worktree machinery;
- deterministic validators;
- local agent role prompts;
- evaluation fixtures;
- code-health analyzers;
- consultant adapters;
- security hardening;
- documentation and ADRs.

## Change classification

Classify the change before implementation:

**Local:** isolated behavior, no durable contract or architecture impact.

**Systemic:** crosses components, changes protocol behavior, scheduling, persistence, evidence generation, or a public internal contract.

**Architectural:** changes durable boundaries, trust assumptions, core schemas, invariants, lifecycle, persistence model, or intelligence hierarchy.

Local changes may use a fast review path. Systemic and architectural changes require deeper design evidence. Architectural changes require an ADR.

## Pull request expectations

A substantial PR should state:
- problem and user/system impact;
- change class;
- design/ADR link;
- invariants considered;
- implementation summary;
- protocol/schema compatibility impact;
- deterministic validation commands and results;
- independent review findings when required;
- migration/rollback plan;
- risks and unresolved questions.

The preferred PR is easy to audit, not merely easy to merge.

## Protocol changes

When changing a protocol:
1. update the Go/domain type;
2. update JSON Schema;
3. update examples/fixtures;
4. add compatibility tests;
5. update docs/PROTOCOLS.md or docs/PROJECT_STATE.md;
6. describe migration or backward-compatibility behavior.

## Model/prompt changes

Treat prompts and skills as executable behavior.

Provide:
- reason for change;
- target failure mode;
- evaluation trajectories or fixtures;
- before/after behavior;
- known regressions;
- rollback path.

A prompt that “sounds better” is not sufficient evidence.

## New providers or runtimes

Provider adapters must not leak provider-specific semantics into the control plane.

Document:
- authentication mechanism;
- capabilities;
- structured-output behavior;
- cancellation;
- quota/rate-limit handling;
- privacy implications;
- test strategy;
- failure modes.

## Security-sensitive contributions

Changes involving commands, credentials, repository write authority, remote MCP, consultant invocation or network access require a threat-model review against docs/SECURITY.md.

## Commits

Use descriptive commits. Prefer one conceptual change per commit where practical.

Never rewrite shared history to hide failed experiments that are relevant to a design decision. Experiments may live on branches; durable lessons belong in ADRs/evaluation artifacts.

## Multi-session / multi-provider handoff

Milestone-sized work commonly outlasts one agent session's quota. See
[AGENT_HANDOFF_PROTOCOL.md](AGENT_HANDOFF_PROTOCOL.md) for the branch,
commit, and handoff-file discipline this requires, and
[docs/WORK_PACKAGES.md](docs/WORK_PACKAGES.md) for how a milestone gets
split into checkpoints small enough that a quota cutoff costs little.

## Documentation

If behavior changes, documentation changes in the same PR.

Normative docs should explain why constraints exist, not merely list implementation details.

## Review culture

Reviewers are encouraged to challenge:
- hidden assumptions;
- unnecessary complexity;
- provider lock-in;
- architectural leakage;
- insufficient deterministic evidence;
- missing negative tests;
- overly broad model authority;
- unbounded retries/concurrency;
- optimistic failure handling.

Disagreement should be resolved through evidence and explicit decisions rather than rhetorical confidence.

