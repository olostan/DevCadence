# ADR-0022: Requirement-Driven Greenfield Creation Through Bounded EWPs

- **Status:** Proposed
- **Date:** 2026-10-03
- **Owners:** DevCadence architecture
- **Related:** discovery/specification, ADR-0012, ADR-0015, ADR-0021, ADR-0023, M6 project bootstrap/adoption

## Context

DevCadence discovery already says humans should not be asked to choose frameworks without product reason, but greenfield creation still needs a durable bridge from specification readiness to a real repository.

The initial design proposed a DevCadence-maintained ScaffoldRecipe/ScaffoldPlan subsystem. That would create significant maintenance burden across Flutter, Xcode, Gradle, Python, web frameworks, and future ecosystems, duplicating native generators and capable implementation models.

The durable value is not owning templates. It is preserving architecture intent, bounded implementation, ecosystem guidance, evidence, and deterministic acceptance.

## Decision

Greenfield creation uses:

~~~text
Specification Readiness
 -> TechnologyOptionSet / evidence
 -> TechnologyDecision / ADR
 -> ProjectBlueprint
 -> bounded greenfield EWP
 -> pack-guided implementation
 -> candidate repository
 -> capability/toolchain resolution
 -> ProjectHealthContract
 -> deterministic acceptance
 -> committed managed baseline
~~~

### Technology selection

Material choices compare credible alternatives against product/engineering requirements and current evidence.

DevCadence supportability is a separate criterion from technical/product fitness. A less-supported stack may still be correct.

### ProjectBlueprint

The blueprint records intended module/repository topology, archetypes, technology/framework decisions, boundaries/dependencies, expected capability needs, docs root, and initial health expectations.

It need not specify every generated file.

### Materialization

A normal bounded EWP authorizes a model/host to use ecosystem-native generators, create files, configure tooling, and use active pack Skills.

DevCadence records generator/tool versions and arguments actually used when available. It does not maintain a universal ScaffoldRecipe registry.

### Packs

Capability packs provide mechanics, Agent Skills, research pointers, and invariant candidates. If no pack exists, the task may proceed with reduced support, explicit native validation, or a synthesized candidate pack.

### Acceptance

The initial project becomes READY only when module/toolchain state, canonical docs, ProjectHealthContract, and required deterministic health are established.

A candidate cannot weaken its own acceptance criteria and immediately use the weakened policy to pass.

## Consequences

Positive:

- avoids vertical template maintenance;
- lets native ecosystem generators evolve independently;
- keeps hosts/models interchangeable;
- preserves deterministic acceptance and evidence;
- supports unsupported/bespoke stacks through bounded work and pack synthesis.

Costs:

- some initial materialization remains model/generator-dependent;
- generator output reproducibility is evidence-based rather than template-owned.

## Rejected alternatives

### Ask a host to scaffold with no durable blueprint/EWP
Rejected because repository topology/technology intent would live only in conversation.

### DevCadence-maintained ScaffoldRecipe registry
Rejected because it duplicates fast-moving ecosystem project generators and creates large maintenance surface.

### Choose only well-supported DevCadence stacks
Rejected because tool support is not equivalent to technical suitability.

## Verification

M6 should verify at least a Go project, Flutter/mobile project, polyglot project, partially supported preferred stack, native generator use, model-created skeleton, synthesized pack, and attempted self-weakening of the initial health gate.
