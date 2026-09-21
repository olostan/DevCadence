# Execution Engineering Agents

## Scope

This document defines the bounded roles that perform high-volume engineering cognition and repository work. The historical filename `LOCAL_AGENTS.md` is retained for compatibility, but "local agent" no longer implies that model inference itself must run locally.

A role is a stable responsibility and authority boundary. Its cognition may come from deterministic tooling, a local model runtime, an authenticated coding CLI, or another policy-authorized remote endpoint. Repository mutation, worktrees, deterministic validation, evidence and acceptance remain under the local DevCadience control plane.

## 1. Role graph

```mermaid
flowchart TB
    CP["Control Plane"]

    CP --> Scout["Repository Scout"]
    CP --> Impl["Implementer"]
    CP --> Test["Test Designer"]
    CP --> Correct["Correctness Reviewer"]
    CP --> Arch["Architecture Reviewer"]
    CP --> Sec["Security Reviewer"]
    CP --> Perf["Performance Reviewer"]
    CP --> Failure["Failure Analyst"]
    CP --> Smell["Health / Smell Reviewer"]
    CP --> Post["Postmortem Analyst"]

    Scout --> Evidence["Evidence"]
    Impl --> Candidate["Candidate commit"]
    Test --> Candidate
    Correct --> Reviews["ReviewResults"]
    Arch --> Reviews
    Sec --> Reviews
    Perf --> Reviews
    Failure --> Evidence
    Smell --> Reviews
    Post --> Lessons["LessonCandidates"]
```

Not every task invokes every role.

## 2. Shared execution-agent principles

All execution roles:
- operate under explicit authority;
- distinguish observation from interpretation;
- preserve evidence references;
- may report uncertainty;
- must stop on prohibited scope expansion;
- do not change normative architecture by implication;
- do not mark deterministic facts without tool evidence;
- are replaceable by another model/harness.

## 3. Repository Scout

### Purpose
Spend the cheapest policy-compliant cognition and deterministic repository tooling freely enough to answer focused repository questions without pushing repository noise into frontier principal context.

### Input
- InvestigationRequest;
- project/repository configuration;
- optional scope hints;
- allowed read tools.

### Responsibilities
- search files/symbols/history/tests;
- follow call paths;
- identify existing patterns;
- discover relevant interfaces and invariants;
- detect contradictions to proposed design assumptions;
- return concise EvidencePacket.

### Not allowed
- broad implementation;
- architectural decision;
- altering source;
- converting inference into fact.

### Flow

```mermaid
sequenceDiagram
    participant C as Control Plane
    participant S as Scout
    participant R as Repository
    participant T as Static/Git Tools

    C->>S: InvestigationRequest
    S->>R: search/read relevant source
    S->>T: symbols/history/diff/tests metadata
    T-->>S: evidence
    R-->>S: evidence
    S->>S: synthesize + challenge own conclusion
    S-->>C: EvidencePacket + raw handles
```

## 4. Implementer

### Purpose
Realize an approved Engineering Work Package in an isolated worktree.

### Required input
- immutable Work Package version;
- base commit/worktree;
- relevant evidence;
- repository tools;
- validation commands/policy.

### Execution loop

```mermaid
flowchart TD
    Read["Read Work Package"]
    Inspect["Inspect exact repository context"]
    Check{"Blueprint assumptions hold?"}
    Code["Implement"]
    Fast["Run fast checks"]
    Fix["Repair within scope"]
    Candidate["Commit candidate"]
    Block["Contradiction / escalation"]

    Read --> Inspect
    Inspect --> Check
    Check -->|"no"| Block
    Check -->|"yes"| Code
    Code --> Fast
    Fast -->|"fail, bounded"| Fix
    Fix --> Code
    Fast -->|"pass"| Candidate
```

### Important behavior
The implementer should not blindly translate pseudocode line-for-line. It adapts to actual repository idioms while preserving semantic requirements.

It may deviate from SHOULD/SUGGESTED guidance with explanation.

It may never silently violate MUST.

## 5. Test Designer

### Purpose
Attack the Work Package's acceptance model independently of the implementer.

Useful for:
- boundary cases;
- property tests;
- regression tests;
- fuzz targets;
- state-machine transitions;
- failure injection;
- concurrency tests.

Test Designer should preferably not see the implementer’s explanatory rationale before producing its first test critique.

## 6. Correctness Reviewer

Checks:
- actual behavior against Work Package;
- edge cases;
- error paths;
- state consistency;
- hidden behavior change;
- incomplete implementation;
- unjustified deviation.

It does not primarily review style.

## 7. Architecture / Invariant Reviewer

Checks:
- component responsibility;
- dependency direction;
- public API effects;
- architectural invariant compliance;
- accidental abstractions;
- scope expansion;
- documented design vs implementation.

## 8. Security Reviewer

Checks according to task risk:
- authority expansion;
- command injection;
- prompt injection;
- credentials;
- unsafe file/path handling;
- arbitrary shell/network access;
- sandbox escape assumptions;
- destructive operations;
- secret persistence/logging;
- dependency risk.

## 9. Performance Reviewer

Used when performance is material.

Must distinguish measured concerns from speculative micro-optimization.

May request benchmarks before recommending complexity.

## 10. Failure Analyst

Invoked when:
- repeated validation failures;
- worker loops;
- unclear compiler/test failures;
- contradiction between blueprint and code;
- integration regression.

The Failure Analyst does not automatically patch. It creates a diagnosis/evidence package that can guide a new Attempt or principal escalation.

## 11. Health / Smell Reviewer

Used during Refactoring Epochs or health triggers.

Looks for semantic smells difficult to reduce to simple metrics:
- duplicated concepts under different names;
- near-identical abstractions;
- layer leakage;
- unnecessary wrappers;
- “AI defensive coding” proliferation;
- fragmented configuration;
- redundant tests;
- accidental public APIs;
- code that technically works but no longer expresses the architecture.

## 12. Postmortem Analyst

Analyzes trajectories after:
- repeated failure;
- principal escalation;
- human correction;
- architectural rollback;
- unusually successful workflow.

It asks whether the cause was:
- Work Package ambiguity;
- wrong principal assumption;
- local model limitation;
- missing deterministic check;
- bad reviewer routing;
- policy threshold;
- architectural weakness.

Its output is a LessonCandidate, never immediate policy mutation.

## 13. Clean-context independence

```mermaid
flowchart LR
    WP["Same Work Package"]
    Diff["Same candidate diff"]

    WP --> Impl["Implementer context"]
    WP --> RevA["Reviewer A clean context"]
    WP --> RevB["Reviewer B clean context"]
    Diff --> RevA
    Diff --> RevB

    Impl -. no reasoning transcript .-> RevA
    Impl -. no reasoning transcript .-> RevB
```

This reduces correlated self-justification.

## 14. Multi-model diversity

When available, critical reviews should sometimes use different model families.

Diversity is useful when models have different failure patterns.

The system should empirically track whether diversity improves defect detection rather than assuming it always does.

## 15. N-version implementation

For high-risk or algorithmically ambiguous tasks:

```mermaid
flowchart TD
    WP["Same Work Package"]
    A["Implementation A<br/>model/profile A"]
    B["Implementation B<br/>model/profile B"]
    VA["Validation A"]
    VB["Validation B"]
    Compare["Comparison reviewer"]
    Agree{"Convergent semantics?"}
    Select["Select/refine candidate"]
    Esc["Principal review"]

    WP --> A
    WP --> B
    A --> VA
    B --> VB
    VA --> Compare
    VB --> Compare
    Compare --> Agree
    Agree -->|"yes"| Select
    Agree -->|"material divergence"| Esc
```

Use selectively; it trades time/compute for independent design evidence.

## 16. Retry policy

Retries are bounded.

A retry should have a reason:
- deterministic failure;
- reviewer-requested repair;
- new evidence;
- revised Work Package.

Repeating the same model/prompt against the same unchanged evidence is not a meaningful retry.

## 17. Model routing

Role profiles may include:
- required context;
- language/framework;
- tool-use reliability;
- structured-output reliability;
- memory footprint;
- expected review strength;
- observed recent success;
- cost class;
- runtime availability.

Model routing is a policy/evaluation problem, not hard-coded identity.

## 18. Cognition endpoint hygiene

For local model runtimes:
- unload models when resource policy requires;
- monitor memory pressure;
- bound concurrent contexts;
- record runtime/model/backend version;
- detect truncation;
- fail explicitly when requested context exceeds configured capability.

For remote/CLI cognition endpoints:
- record endpoint/provider/model identity when available;
- detect auth/rate-limit/provider failures distinctly;
- obey source-exposure and cost policy;
- preserve prompt/output artifacts only according to privacy policy;
- support cancellation and bounded tool/session behavior where the adapter permits it.

Endpoint selection is capability/policy-driven; no role requires a particular inference locality.

## 19. Output discipline

Execution agents return structured outputs. Free-form prose may supplement them.

Critical fields must not be recoverable only by brittle prose parsing.

## 20. Anti-patterns

- one “super agent” with all authorities;
- reviewers inheriting implementer reasoning by default;
- “confidence: 95%” replacing evidence;
- unlimited retry loops;
- worker silently broadening task scope;
- worker rewriting architecture to make tests pass;
- using an expensive/strong cognition endpoint for trivial log compression when smaller/deterministic processing is sufficient.


## Review campaign behavior

Execution reviewers participate in a bounded ReviewCampaign.

### Broad reviewers
- inspect the immutable campaign candidate;
- stay within assigned dimension except critical cross-cutting defects;
- return at most the configured number of most consequential findings;
- provide evidence;
- may return zero findings;
- do not directly request implementation changes.

### Repair implementer
Receives one consolidated Repair Work Package containing adjudicated FIX_NOW findings. It does not receive every reviewer transcript by default.

### Focused revalidator
After repair, checks:
- accepted finding repairs;
- deterministic regressions;
- invariant/contract regressions caused by repair.

It does not restart a general search for lower-severity improvements.

### Closure reviewer
Uses prompts/closure-reviewer.md. It reports only threshold-crossing issues and treats a zero-finding outcome as valid.

Execution reviewers do not reopen adjudicated findings without materially new evidence.

See [REVIEW_AND_CONVERGENCE.md](REVIEW_AND_CONVERGENCE.md).
