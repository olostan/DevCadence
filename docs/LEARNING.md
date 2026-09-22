# Learning and Continuous Improvement

## Scope

DevCadence learns by improving its engineering system: knowledge, skills, prompts, routing, verification, task design and policies. Learning is evidence-driven and governed. It is not uncontrolled prompt self-editing.

## 1. Learning loop

```mermaid
flowchart LR
    Run["Engineering trajectories"]
    Post["Postmortem / pattern mining"]
    Candidate["LessonCandidate"]
    Eval["Replay / evaluation"]
    Review["Governance review"]
    Promote["Promote versioned change"]
    Observe["Observe future outcomes"]

    Run --> Post
    Post --> Candidate
    Candidate --> Eval
    Eval --> Review
    Review -->|"accepted"| Promote
    Review -->|"rejected/revise"| Candidate
    Promote --> Observe
    Observe --> Run
```

## 2. Trajectory model

A trajectory records the observable engineering process:
- initial ProjectState revision;
- Work Package;
- evidence used;
- models/profiles;
- prompts/skills versions;
- tool calls;
- implementation attempts;
- validation results;
- reviews/disagreements;
- escalations;
- consultant results;
- accepted patch;
- human corrections;
- outcome and later regressions.

It does not require storing hidden chain-of-thought.

## 3. Trajectory relationships

```mermaid
erDiagram
    TASK ||--o{ ATTEMPT : has
    WORK_PACKAGE ||--o{ ATTEMPT : governs
    ATTEMPT ||--o{ VALIDATION : produces
    ATTEMPT ||--o{ REVIEW : receives
    ATTEMPT ||--o{ ESCALATION : may_raise
    TASK ||--o{ CONSULTATION : may_use
    TASK ||--o{ LESSON_CANDIDATE : may_generate
    LESSON_CANDIDATE ||--o{ EVALUATION : evaluated_by
```

## 4. Lesson types

### Project knowledge
Example: “Timestamp normalization belongs in ingestion.”

Can become:
- project invariant;
- ADR clarification;
- component rule.

### Engineering skill
Example: “When a Go interface changes, find/regenerate mocks before validation.”

Can become:
- reusable role skill;
- deterministic preflight check.

### Routing knowledge
Example: “Model A catches API compatibility issues better than Model B.”

Can alter:
- reviewer routing;
- N-version policies.

### Task-design knowledge
Example: “Concurrency tasks fail less when ownership/locking pseudocode is mandatory.”

Can alter:
- Work Package templates;
- readiness policy.

### Verification knowledge
Example: “These parser changes need fuzzing.”

Can alter:
- validation profile selection.

### Prompt/role knowledge
A role prompt consistently misses a class of issue.

Can produce:
- prompt candidate;
- evaluation against frozen trajectories.

### Architecture knowledge
A past ADR caused repeated workarounds.

Can produce:
- architecture lesson;
- reconciliation input.

## 5. Lesson Candidate lifecycle

```mermaid
stateDiagram-v2
    [*] --> Proposed
    Proposed --> EvidenceGathering
    EvidenceGathering --> EvaluationReady
    EvaluationReady --> Evaluating
    Evaluating --> Rejected
    Evaluating --> NeedsRevision
    NeedsRevision --> Proposed
    Evaluating --> Approved
    Approved --> Promoted
    Promoted --> Monitoring
    Monitoring --> RolledBack: regression
    Monitoring --> Stable
    Stable --> [*]
    Rejected --> [*]
```

## 6. Candidate contents

A LessonCandidate should include:
- type;
- scope;
- observed pattern;
- evidence trajectories;
- proposed change;
- expected benefit;
- possible counterexamples;
- conflicts with existing policy/invariants;
- evaluation plan;
- promotion authority;
- rollback plan.

## 7. Evaluation before promotion

Possible evaluation methods:
- replay historical task contexts;
- run a frozen repository fixture;
- A/B role prompts;
- shadow routing;
- compare reviewer true/false positive rates;
- compare retries/escalations;
- human review of representative cases.

Do not promote a general rule solely because it would have fixed one memorable failure.

## 8. Prompt evaluation

Prompt changes are code changes.

Track:
- prompt version;
- evaluation corpus;
- success criteria;
- regressions;
- output schema validity;
- token/context behavior;
- tool-call reliability;
- false escalation rate.

## 9. Routing evaluation

For each model/profile/role, track empirical outcomes.

```mermaid
flowchart TB
    Data["Historical evaluated tasks"]
    Features["Task class / language / risk"]
    Models["Candidate model profiles"]
    Score["Outcome metrics"]
    Policy["Routing policy candidate"]
    Shadow["Shadow / replay evaluation"]
    Promote["Promote"]

    Data --> Features
    Features --> Score
    Models --> Score
    Score --> Policy
    Policy --> Shadow
    Shadow --> Promote
```

Avoid opaque learned routing initially. Start with explicit interpretable policies.

## 10. Useful outcome metrics

- task accepted;
- human correction later;
- regressions found after acceptance;
- retry count;
- principal escalation;
- reviewer defect precision;
- Work Package deviation;
- validation failures;
- wall time;
- local compute;
- frontier quota/API use;
- code-health impact.

Quality dominates raw speed.

## 11. Architecture postmortems

When an implementation is painful, ask whether design was wrong.

Possible causes:
- incorrect architecture;
- ambiguous Work Package;
- missing repository fact;
- bad local model;
- poor routing;
- missing deterministic check;
- insufficient test strategy.

Do not default to blaming the implementer.

## 12. Cross-project vs project-specific learning

A project-specific lesson should not automatically become global.

Promotion scopes:
- task-local;
- project;
- language/framework;
- organization;
- global DevCadence default.

Higher scope requires stronger evidence.

## 13. Knowledge retrieval

Promoted lessons should be retrievable by:
- component;
- language/framework;
- failure type;
- task class;
- architecture concept.

Do not stuff all lessons into every prompt.

## 14. Policy experiments

A PolicyExperiment defines:
- hypothesis;
- baseline;
- candidate behavior;
- evaluation set;
- metrics;
- stop conditions;
- promotion criteria.

Example:
> For systemic Go concurrency changes, adding a clean-context concurrency reviewer after correctness review reduces accepted race defects without increasing false escalation above X.

## 15. Self-development

DevCadence may develop DevCadence, but self-referential work follows identical policy.

```mermaid
flowchart LR
    DC["DevCadence current version"]
    Task["Self-improvement task"]
    EWP["Frontier Work Package"]
    Local["Local implementation"]
    Verify["Independent verification"]
    Decision["Authorized acceptance"]
    Next["DevCadence next version"]

    DC --> Task --> EWP --> Local --> Verify --> Decision --> Next
```

No component gets privileged permission to bypass governance because the target repository is itself.

## 16. Fine-tuning

Fine-tuning is optional and later-stage.

Before considering it, prefer:
- better Work Packages;
- better evidence retrieval;
- role specialization;
- prompts/skills;
- model routing;
- deterministic checks;
- evaluation.

If trajectory data eventually supports fine-tuning, keep train/eval leakage controls and preserve a sealed evaluation corpus.

## 17. Anti-patterns

- agent edits its own normative prompt after one failure;
- “learned rule” with no provenance;
- routing policy based only on vendor benchmark;
- deleting failed trajectories to make metrics look better;
- training on the same tasks used to claim improvement;
- treating human override as noise rather than high-value evidence.
