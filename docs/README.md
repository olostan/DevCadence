# DevCadience Documentation Map

The documentation is intentionally split by durable concern so humans and agents can load focused context instead of a single enormous specification.

```mermaid
flowchart TB
    Vision["VISION<br/>why this exists"]
    Req["REQUIREMENTS<br/>what it must do"]
    Discovery["DISCOVERY & SPECIFICATION<br/>idea → grounded spec"]
    Inv["INVARIANTS<br/>what must never drift"]
    Arch["ARCHITECTURE<br/>durable boundaries"]
    Life["LIFECYCLE<br/>idea → design → delivery → health"]
    Principal["PRINCIPAL ENGINEER<br/>frontier cognition"]
    Protocol["PROTOCOLS<br/>semantic language"]
    State["PROJECT STATE<br/>canonical memory"]
    Local["LOCAL AGENTS<br/>abundant cognition"]
    MCP["MCP API<br/>principal interface"]
    AG["ANTIGRAVITY INTEGRATION<br/>concrete frontend setup"]
    Runtime["MODEL RUNTIME<br/>local compute"]
    Verify["VERIFICATION<br/>evidence + review"]
    Consult["CONSULTANTS<br/>cognitive diversity"]
    Security["SECURITY<br/>authority boundaries"]
    Observe["OBSERVABILITY<br/>auditability"]
    Health["REFACTORING & HEALTH"]
    Learn["LEARNING"]
    Plan["IMPLEMENTATION PLAN"]
    Setup["SETUP"]

    Vision --> Discovery
    Discovery --> Req
    Req --> Inv
    Inv --> Arch
    Arch --> Life
    Life --> Principal
    Life --> Protocol
    Protocol --> State
    Protocol --> MCP
    MCP --> AG
    AG --> Principal
    MCP --> Local
    Local --> Runtime
    Local --> Verify
    Principal --> Consult
    Arch --> Security
    Verify --> Observe
    Life --> Health
    Health --> Learn
    Learn --> Principal
    Arch --> Plan
    Plan --> Setup
```

## Reading paths

### New contributor
1. [../README.md](../README.md)
2. [VISION.md](VISION.md)
3. [REQUIREMENTS.md](REQUIREMENTS.md)
4. [../INVARIANTS.md](../INVARIANTS.md)
5. [ARCHITECTURE.md](ARCHITECTURE.md)
6. [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md)
7. [../AGENTS.md](../AGENTS.md)

### Working on Day-0 discovery/specification
1. [DISCOVERY_AND_SPECIFICATION.md](DISCOVERY_AND_SPECIFICATION.md)
2. [../skills/antigravity-discovery/SKILL.md](../skills/antigravity-discovery/SKILL.md)
3. [CONSULTANTS.md](CONSULTANTS.md)
4. [PROTOCOLS.md](PROTOCOLS.md)
5. [../prompts/specification-reviewer.md](../prompts/specification-reviewer.md)
6. [adr/0001-discovery-specification-subsystem.md](adr/0001-discovery-specification-subsystem.md)

### Working on frontier/principal behavior
1. [PRINCIPAL_ENGINEER.md](PRINCIPAL_ENGINEER.md)
2. [LIFECYCLE.md](LIFECYCLE.md)
3. [PROTOCOLS.md](PROTOCOLS.md)
4. [ANTIGRAVITY_INTEGRATION.md](ANTIGRAVITY_INTEGRATION.md)
5. [CONSULTANTS.md](CONSULTANTS.md)
6. [../integrations/antigravity/devcadience/skills/devcadience-principal/SKILL.md](../integrations/antigravity/devcadience/skills/devcadience-principal/SKILL.md)

### Working on local execution
1. [LOCAL_AGENTS.md](LOCAL_AGENTS.md)
2. [MODEL_RUNTIME.md](MODEL_RUNTIME.md)
3. [VERIFICATION.md](VERIFICATION.md)
4. [SECURITY.md](SECURITY.md)
5. [../prompts/scout.md](../prompts/scout.md)
6. [../prompts/implementer.md](../prompts/implementer.md)
7. [../prompts/reviewer.md](../prompts/reviewer.md)

### Working on control-plane data/contracts
1. [PROTOCOLS.md](PROTOCOLS.md)
2. [PROJECT_STATE.md](PROJECT_STATE.md)
3. [MCP_API.md](MCP_API.md)
4. [../schemas/README.md](../schemas/README.md)
5. [adr/0003-durable-record-compatibility.md](adr/0003-durable-record-compatibility.md)
6. [adr/0004-canonical-task-state-machine.md](adr/0004-canonical-task-state-machine.md)
7. [adr/0005-deterministic-project-state-identity.md](adr/0005-deterministic-project-state-identity.md)

### Working on persistence
1. [ARCHITECTURE.md](ARCHITECTURE.md) §10
2. [adr/0002-control-plane-persistence.md](adr/0002-control-plane-persistence.md)
3. [adr/0006-identifiers-and-time.md](adr/0006-identifiers-and-time.md)
4. [../ENGINEERING_STANDARDS.md](../ENGINEERING_STANDARDS.md) §10–§12

### Working on long-term quality
1. [REFACTORING_AND_HEALTH.md](REFACTORING_AND_HEALTH.md)
2. [LEARNING.md](LEARNING.md)
3. [OBSERVABILITY.md](OBSERVABILITY.md)
4. [../prompts/postmortem.md](../prompts/postmortem.md)

## Accepted ADRs

Accepted ADRs are normative and rank above the engineering standards in the
hierarchy below.

| ADR | Decision |
| --- | --- |
| [0001](adr/0001-discovery-specification-subsystem.md) | First-class discovery and specification subsystem |
| [0002](adr/0002-control-plane-persistence.md) | SQLite persistence: pure-Go driver, append-only journal, derived projections |
| [0003](adr/0003-durable-record-compatibility.md) | Strict readers, unknown-field behaviour, preserved original bytes |
| [0004](adr/0004-canonical-task-state-machine.md) | Canonical task lifecycle, block semantics, ProjectState task buckets |
| [0005](adr/0005-deterministic-project-state-identity.md) | ProjectState is a pure function of the event prefix |
| [0006](adr/0006-identifiers-and-time.md) | ULID identifiers and injected clocks |
| [0007](adr/0007-repository-and-worktree-safety-model.md) | Repository identity, worktree ownership (per-project manifest), non-mutating integration checks |
| [0008](adr/0008-controlled-process-execution.md) | Controlled process execution: no shell, no implicit environment inheritance, distinguished outcome categories |
| [0009](adr/0009-artifact-storage-and-validation-execution.md) | Content-addressed artifact store; validation-profile execution produces the real M1 ValidationResult |

## Normative hierarchy

If documents appear to conflict, use this order and surface the inconsistency:

1. explicit human-authorized project decision / accepted ADR;
2. [../INVARIANTS.md](../INVARIANTS.md);
3. approved architecture and protocol documents;
4. [../AGENTS.md](../AGENTS.md) and engineering standards;
5. current Work Package;
6. role prompts/skills;
7. implementation suggestion.

A conflict is evidence to resolve, not permission to silently choose whichever text is convenient.


### Working on review, repair, or milestone closure
1. [REVIEW_AND_CONVERGENCE.md](REVIEW_AND_CONVERGENCE.md)
2. [VERIFICATION.md](VERIFICATION.md)
3. [PRINCIPAL_ENGINEER.md](PRINCIPAL_ENGINEER.md)
4. [../prompts/reviewer.md](../prompts/reviewer.md)
5. [../prompts/review-synthesizer.md](../prompts/review-synthesizer.md)
6. [../prompts/closure-reviewer.md](../prompts/closure-reviewer.md)
