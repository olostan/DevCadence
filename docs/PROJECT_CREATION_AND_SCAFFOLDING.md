# Greenfield Project Creation

## Scope

Greenfield creation turns a specification-ready product into a trustworthy initial managed repository.

DevCadence does **not** maintain a universal scaffolding/template engine. Ecosystem generators and capable implementation models already create project skeletons well; DevCadence's leverage is requirement-driven architecture, bounded implementation, pack-guided ecosystem knowledge, and deterministic acceptance.

~~~text
ProblemModel / Requirements
        |
Specification Readiness
        |
TechnologyOptionSet + evidence
        |
TechnologyDecision / ADR
        |
ProjectBlueprint
        |
bounded greenfield EWP
        |
pack-guided implementation
  (LLM and/or native generator)
        |
candidate repository/modules
        |
capability/toolchain resolution
        |
ProjectHealthContract + canonical docs
        |
deterministic acceptance
        |
initial managed baseline
~~~

## 1. Technology selection

Discovery establishes constraints, not favorite frameworks.

Material choices compare credible alternatives against target platforms, performance/resources, native API needs, offline/data/security constraints, deployment/operations, interoperability, ecosystem maturity, human/team constraints, maintainability, and reversibility.

DevCadence supportability is a separate reported axis. A less-supported stack may still be the correct architecture.

Material current facts SHOULD be verified through authoritative research or bounded DiscoveryExperiments rather than model memory.

## 2. ProjectBlueprint

The accepted ProjectBlueprint describes intended repository/module topology:

- ADR-0015 modules/paths;
- application archetypes;
- selected technologies/frameworks;
- dependencies/runtime/public boundaries;
- expected capability-pack/toolchain needs;
- canonical docs root;
- initial health expectations.

It does not prescribe every generated file.

## 3. Greenfield materialization is a bounded EWP

After architecture acceptance, the Principal creates a normal bounded EWP for initial materialization.

An implementation model/host may invoke ecosystem-native generators, create files directly, use active pack Skills, configure build/test/lint/run tooling, establish canonical docs, and create the initial ProjectHealthContract.

DevCadence records generator/tool version and arguments actually used when available. It does not require a maintained ScaffoldRecipe/ScaffoldPlan registry.

The result remains a candidate until deterministic acceptance. Generator exit zero is insufficient proof of readiness.

## 4. Packs guide creation

Packs may contribute technology constraints/research pointers, environment/tool mechanics, run/build/test/health mechanics, Agent Skills for common ecosystem tasks, review/testing Skills, and invariant candidates.

If no suitable pack exists, the implementation may proceed through bounded research/EWP work while DevCadence reports reduced support. A candidate project pack may be synthesized and conformance-tested.

## 5. Initial acceptance

A greenfield baseline becomes READY only when:

- repository/module structure matches accepted architecture;
- capability/toolchain composition is recorded;
- required SDK/tool versions are probed;
- ProjectHealthContract exists;
- required health gates pass;
- canonical docs match the materialized architecture;
- support gaps remain explicit;
- baseline is committed.

Once an acceptance contract exists, the base-governed rule applies: a candidate cannot weaken its own judge and immediately use the weaker policy to pass.

## 6. Native generators are execution evidence

Ecosystem-native generators such as `flutter create`, `cargo new`, framework initializers, Xcode/Gradle mechanisms, and package-manager project initializers remain useful.

Packs/Skills may teach models how to use them. DevCadence records relevant versions/arguments/output evidence and validates the result.

This avoids owning every ecosystem template while retaining reproducibility of what actually occurred.

## 7. Unsupported stacks

If the technically preferred stack lacks mature DevCadence support:

- keep the technology decision if still correct;
- report missing capabilities;
- use bounded EWP implementation;
- require explicit native validation or synthesize a pack;
- keep partial/unsupported state visible.

DevCadence MUST NOT silently switch technology because another stack has better pack coverage.

## 8. Hosts

Antigravity, Cursor, VS Code, local coding agents, and other endpoints may perform the greenfield EWP.

They are interchangeable implementation surfaces, not the architecture of project creation.

Durable artifacts are the requirements/decision/blueprint/EWP, pack lineage, repository evidence, health contract, and accepted baseline.

## 9. Verification scenarios

M6 should cover a Go service, Flutter/mobile application, polyglot repository, partially supported preferred stack, native generator use, direct model-created skeleton, synthesized pack, initial health failure/repair, and attempted weakening of the candidate's own gate.

See [PROJECT_CAPABILITY_PACKS.md](PROJECT_CAPABILITY_PACKS.md), [PROJECT_TOOLCHAINS_AND_HEALTH.md](PROJECT_TOOLCHAINS_AND_HEALTH.md), and ADR-0022.
