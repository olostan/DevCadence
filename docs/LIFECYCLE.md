# DevCadience Engineering Lifecycle

## Scope

This document defines how DevCadience handles a project from Day 0 through long-term maintenance. The lifecycle is deliberately broader than code generation.

## 1. Whole-project lifecycle

```mermaid
flowchart TD
    Idea["Idea / Desired Outcome"]
    Discovery["Discovery & Problem Framing"]
    Explore["Design Exploration"]
    Critique["Independent & Adversarial Critique"]
    Baseline["Baseline Engineering Model"]
    Plan["Milestone Planning"]
    Change["Adaptive Change Loop"]
    Refactor["Refactoring Epoch"]
    Reconcile["Architecture Reconciliation"]
    Learn["Learning & Policy Evaluation"]

    Idea --> Discovery
    Discovery --> Explore
    Explore --> Critique
    Critique -->|"not ready"| Explore
    Critique -->|"design ready"| Baseline
    Baseline --> Plan
    Plan --> Change
    Change -->|"more feature work"| Change
    Change -->|"health/milestone trigger"| Refactor
    Refactor --> Reconcile
    Reconcile --> Plan
    Change --> Learn
    Refactor --> Learn
    Reconcile --> Learn
    Learn -. promoted knowledge .-> Discovery
    Learn -. promoted knowledge .-> Explore
    Learn -. promoted knowledge .-> Change
```

## 2. Day 0: idea intake

The system begins by understanding the problem, not inventing architecture.

Required initial artifact: **Problem Model**.

Suggested fields:
- desired outcome;
- target users/operators;
- problem evidence;
- constraints;
- explicit non-goals;
- assumptions;
- unknowns;
- current environment;
- data/security/regulatory concerns;
- expected scale;
- acceptable operational complexity;
- success/failure criteria.

The principal should ask whether the proposed software is actually the correct intervention.

## 3. Discovery

Discovery may use:
- current web/documentation research;
- local environment probes;
- prototypes;
- consultant analyses;
- comparable architecture research;
- performance measurements;
- standards/security references.

The principal distinguishes:
- verified external facts;
- current repository facts;
- assumptions;
- hypotheses.

### Discovery convergence

```mermaid
flowchart LR
    Questions["Unknowns / assumptions"]
    Research["External research"]
    Probe["Local prototype / benchmark"]
    Consultant["Independent consultant"]
    Evidence["Evidence packets"]
    Problem["Refined Problem Model"]

    Questions --> Research
    Questions --> Probe
    Questions --> Consultant
    Research --> Evidence
    Probe --> Evidence
    Consultant --> Evidence
    Evidence --> Problem
```

## 4. Design exploration

Substantial systems should have multiple candidate architectures.

The principal should consider alternatives before becoming attached to one.

For an architectural decision, capture:
- candidate design;
- benefits;
- failure modes;
- operational burden;
- security implications;
- testability;
- migration path;
- reversibility;
- impact on future features;
- assumptions;
- evidence.

## 5. Deliberate critique vectors

```mermaid
flowchart TB
    Candidate["Candidate architecture"]

    Candidate --> Correct["Correctness critique"]
    Candidate --> Simple["Simplicity critique"]
    Candidate --> Scale["Scale/performance critique"]
    Candidate --> Security["Security critique"]
    Candidate --> Operate["Operability critique"]
    Candidate --> Test["Testability critique"]
    Candidate --> Evolve["Evolvability critique"]
    Candidate --> Failure["Failure-mode critique"]

    Correct --> Synthesis["Principal synthesis"]
    Simple --> Synthesis
    Scale --> Synthesis
    Security --> Synthesis
    Operate --> Synthesis
    Test --> Synthesis
    Evolve --> Synthesis
    Failure --> Synthesis

    Synthesis --> Revised["Revised design"]
```

Not every vector needs a frontier consultant. Many can be separate local or frontier passes. The important property is explicit perspective diversity.

## 6. Baseline Engineering Model

Implementation of a new substantial project should begin only after the project has enough durable guidance.

Typical baseline:
- VISION / problem definition;
- functional requirements;
- non-functional requirements;
- architecture;
- component contracts;
- invariants;
- ADRs;
- failure model;
- security model;
- observability strategy;
- testing/verification strategy;
- rollout/migration strategy when relevant;
- milestones.

“Baseline” means implementation agents cannot casually redefine these decisions. It does not mean architecture can never change.

## 7. Change impact classification

Every feature/change is classified before choosing process depth.

```mermaid
flowchart TD
    Request["Change request"]
    Assess{"Semantic impact?"}

    Assess -->|"bounded, no durable contract impact"| Local["LOCAL"]
    Assess -->|"cross-component / meaningful semantics"| Systemic["SYSTEMIC"]
    Assess -->|"durable boundary / invariant / security / persistence / API"| Arch["ARCHITECTURAL"]

    Local --> Fast["Fast path"]
    Systemic --> Normal["Normal path"]
    Arch --> Deep["Deep design path"]

    Risk{"High security / data loss / irreversibility?"}
    Request --> Risk
    Risk -->|"yes"| Deep
```

Risk can promote a small change to the deep path.

## 8. Fast path

Suitable for local changes.

```mermaid
sequenceDiagram
    participant P as Principal
    participant C as Control Plane
    participant W as Local Worker
    participant V as Validator

    P->>C: compact task/work package
    C->>W: implement
    W->>V: validate
    V-->>C: result
    C-->>P: completion summary
```

Fast does not mean unverified.

## 9. Normal path

For systemic changes:

```mermaid
flowchart LR
    Intent["Principal intent"] --> Scout["Local scouting"]
    Scout --> Evidence["EvidencePacket"]
    Evidence --> Design["Principal design + alternatives"]
    Design --> Package["Detailed Work Package"]
    Package --> Implement["Local implementation"]
    Implement --> Validate["Deterministic validation"]
    Validate --> Reviews["Independent reviews"]
    Reviews --> Decide{"Evidence sufficient?"}
    Decide -->|"yes"| Integrate["Integrate"]
    Decide -->|"no"| Escalate["Principal escalation"]
    Escalate --> Design
```

## 10. Deep architectural path

```mermaid
flowchart TD
    Need["Architectural change"]
    Research["Research / repository scouting"]
    A["Principal candidate A/B/C"]
    I1["Independent consultant 1"]
    I2["Independent consultant 2"]
    Critique["Adversarial critique"]
    Synthesis["Principal synthesis"]
    ADR["ADR + invariant/contract updates"]
    Readiness{"Design readiness gate"}
    Decompose["Milestones / Work Packages"]
    Implement["Local execution"]
    Reconcile["Post-change architecture reconciliation"]

    Need --> Research
    Research --> A
    A --> I1
    A --> I2
    A --> Critique
    I1 --> Synthesis
    I2 --> Synthesis
    Critique --> Synthesis
    Synthesis --> ADR
    ADR --> Readiness
    Readiness -->|"not ready"| Research
    Readiness -->|"ready"| Decompose
    Decompose --> Implement
    Implement --> Reconcile
```

Where independence matters, consultants should first receive the problem without the principal's preferred answer.

## 11. Design Readiness Gate

Readiness is evidence-based, not a numeric self-confidence score.

A systemic/architectural design is ready when required policy checks are satisfied, such as:
- material assumptions verified or explicitly accepted as risk;
- alternatives considered;
- current repository constraints grounded;
- current external facts grounded where applicable;
- major risks documented;
- dissent/disagreements resolved or intentionally preserved;
- implementation approach complete;
- pseudocode/algorithms sufficient for non-trivial logic;
- acceptance and test strategy complete;
- rollback/migration understood;
- escalation conditions defined.

```mermaid
stateDiagram-v2
    [*] --> Draft
    Draft --> Investigating
    Investigating --> Critiquing
    Critiquing --> Revising
    Revising --> Investigating: material unknown remains
    Revising --> ReadyCandidate
    ReadyCandidate --> Blocked: unresolved material assumption
    Blocked --> Investigating
    ReadyCandidate --> Approved: readiness policy satisfied
    Approved --> [*]
```

## 12. Delivery task lifecycle

```mermaid
stateDiagram-v2
    [*] --> Proposed
    Proposed --> Scouting
    Scouting --> Designing
    Designing --> Ready
    Ready --> Running
    Running --> Validating
    Validating --> Running: bounded repair
    Validating --> Reviewing: deterministic checks pass
    Reviewing --> Running: local fix requested
    Reviewing --> Blocked: contradiction / unresolved disagreement
    Blocked --> Designing: principal revises package
    Reviewing --> Accepted
    Accepted --> Integrating
    Integrating --> ValidatingIntegration
    ValidatingIntegration --> Done: pass
    ValidatingIntegration --> Blocked: conflict / regression
    Done --> [*]
```

Retries create new Attempt records and do not erase failed history.

## 13. Contradiction path

A local agent is expected to challenge a false blueprint assumption.

```mermaid
sequenceDiagram
    participant P as Principal
    participant W as Local Worker
    participant C as Control Plane
    participant S as Scout

    P->>C: Work Package with assumption A
    C->>W: execute
    W->>C: CONTRADICTION: A false + evidence
    C->>S: verify contradiction independently
    S-->>C: corroborating/conflicting evidence
    C-->>P: EscalationRequest
    P->>P: reconsider design
    P->>C: revised Work Package / decision
    C->>W: continue as new attempt
```

## 14. Refactoring Epoch triggers

A Refactoring Epoch may be triggered by:
- milestone completion;
- N systemic tasks since previous epoch;
- rising complexity or duplication;
- API surface growth;
- repeated reviewer concerns;
- architectural smell clusters;
- significant dependency changes;
- pre-release gate.

Feature pressure cannot permanently suppress the epoch.

## 15. Architecture Reconciliation

Refactoring focuses on code quality within the intended architecture.

Architecture Reconciliation asks whether the intended architecture itself is still right.

Inputs:
- current implementation map;
- current requirements;
- original architecture;
- ADR history;
- accumulated workarounds;
- code-health reports;
- production/operational evidence;
- repeated failure patterns.

Outputs:
- confirmation of current baseline; or
- new architecture version;
- superseded ADRs/invariants;
- migration plan;
- new milestones/work packages.

## 16. Learning loop integration

```mermaid
flowchart LR
    Traj["Completed trajectories"]
    Post["Postmortem analysis"]
    Candidate["Lesson / policy candidate"]
    Replay["Replay / evaluation"]
    Review["Governance review"]
    Promote["Promoted skill/rule/policy"]
    Future["Future lifecycle"]

    Traj --> Post
    Post --> Candidate
    Candidate --> Replay
    Replay --> Review
    Review -->|"accepted"| Promote
    Review -->|"rejected"| Candidate
    Promote --> Future
```

## 17. Human escalation

The human should see decisions with durable product or risk significance, not every implementation failure.

Examples:
- product semantics genuinely ambiguous;
- irreversible data migration;
- security boundary change;
- legal/compliance implication;
- cost/operational commitment;
- multiple frontier consultants remain materially divided;
- project goals conflict.

The system should present the human with evidence, alternatives and consequences rather than an unstructured transcript.


## 18. Discovery and Specification Loop

The Day-0 intake described above is governed by the full protocol in [DISCOVERY_AND_SPECIFICATION.md](DISCOVERY_AND_SPECIFICATION.md).

Before entering Design Exploration, a substantial greenfield project or product-semantic change must pass Specification Readiness.

```mermaid
flowchart TD
    Idea["Idea / feature intent"]
    PM["ProblemModel"]
    AL["Ambiguity Ledger"]
    Resolve["Human / research / experiment / consultants"]
    Reflect["Human reflection"]
    Candidate["Specification Candidate"]
    RedTeam["Independent spec red-team"]
    Gate{"Specification Ready?"}
    Design["Design Exploration"]

    Idea --> PM --> AL --> Resolve --> PM
    PM --> Reflect --> AL
    AL -->|"material ambiguity bounded"| Candidate
    Candidate --> RedTeam
    RedTeam -->|"gaps"| AL
    RedTeam --> Gate
    Gate -->|"no"| AL
    Gate -->|"yes"| Design
```

A fixed questionnaire is not the lifecycle. The principal asks a small number of high-impact questions, updates durable state, and repeats.

When requirements change after architecture has begun, only the affected discovery scope must be reopened unless the change invalidates foundational product assumptions.
