# Project Adoption and Retrospective Reconstruction

## Scope

DevCadience supports both greenfield projects and existing repositories.

A greenfield project can establish its canonical engineering baseline prospectively through normal discovery/specification.

An existing repository requires a **Project Adoption and Retrospective Reconstruction** workflow before it becomes a DevCadience-managed project.

The adoption workflow exists because an existing repository may have:

- no engineering documentation;
- partial or stale documentation;
- documentation in arbitrary locations/formats;
- code that contradicts documentation;
- tests that encode undocumented contracts;
- architectural decisions preserved only in Git history;
- current behavior whose original product rationale is no longer known.

> **An existing repository is not DevCadience-ready merely because it can be registered, built, tested, or searched. It becomes DevCadience-ready only after a canonical, evidence-backed project baseline has been materialized in Git and accepted.**

## 1. Two onboarding dimensions

Environment onboarding and project onboarding are independent.

```text
Environment onboarding
  "Can this machine run DevCadience?"
        |
        v
  hardware / runtimes / hosts / auth

Project onboarding
  "Does DevCadience understand this repository well enough
   to manage changes safely?"
        |
        v
  retrospective reconstruction / canonical baseline
```

A machine may be environment-ready while a repository is not project-ready.

## 2. Adoption authority boundary

Before adoption reaches READY, DevCadience may perform bounded read-only or isolated discovery activities needed to understand the repository, including:

- deterministic repository inventory;
- source/index analysis;
- test discovery;
- documentation harvesting;
- Git history inspection;
- bounded experiments in disposable worktrees;
- principal/consultant analysis.

Before READY, DevCadience MUST NOT treat the project as normally managed engineering work.

In particular, normal task delegation, autonomous implementation, acceptance and integration into the managed baseline are blocked until the Adoption Readiness Gate passes.

The adoption workflow itself may create an isolated documentation-baseline branch/worktree.

## 3. Canonical project documentation is mandatory

Every DevCadience-managed project MUST have a committed canonical project documentation set.

The default canonical root for adopted projects is:

```text
docs/devcadience/
```

A project MAY configure another committed canonical root, but the location must be explicit and stable. DevCadience must never infer the canonical location from whichever Markdown file happens to exist.

### Required baseline artifacts

At minimum:

```text
VISION.md
REQUIREMENTS.md
ARCHITECTURE.md
INVARIANTS.md
SECURITY.md
TEST_STRATEGY.md
OPERATIONS.md
adr/
  README.md
```

These are semantic slots, not invitations to produce boilerplate.

A required artifact that is genuinely inapplicable still exists and explains why the topic is not applicable.

Example:

```markdown
## Persistent-state recovery

Status: NOT_APPLICABLE

Reason:
This repository builds a compile-time library and owns no persistent runtime state.
```

### Conditional artifacts

The reconstruction may add canonical artifacts when the system warrants them, for example:

- DATA_MODEL.md;
- API_CONTRACTS.md;
- DEPLOYMENT.md;
- FAILURE_MODEL.md;
- COMPONENTS.md;
- MIGRATION_STRATEGY.md;
- PRIVACY.md;
- domain-specific specifications.

The required baseline is intentionally compact enough to be universal while extensions remain evidence-driven.

## 4. Existing documentation is evidence, not automatic authority

Adoption searches broadly for relevant material rather than expecting DevCadience filenames.

Sources may include:

- README files;
- docs directories;
- Markdown/reStructuredText/text notes;
- ADRs/RFCs/design docs;
- OpenAPI/protobuf/schema files;
- package/module documentation;
- source comments;
- CI configuration;
- deployment configuration;
- database migrations;
- test names/fixtures;
- issue references available through configured integrations;
- Git history.

Existing documentation may be excellent, obsolete, contradictory or incomplete.

Therefore:

> **Existing documentation feeds reconstruction; it does not become canonical merely because it exists.**

If an existing document is current and high quality, the canonical artifact may reference and incorporate it instead of duplicating every paragraph. However, the canonical slot must still exist and must contain enough current summary/status/provenance for a new principal to know what is authoritative without guessing.

## 5. Evidence classes

Retrospective reconstruction must preserve the epistemic origin of material statements.

Useful statuses include:

- **OBSERVED** — established from current code, tests, schemas, configuration or deterministic tooling;
- **DOCUMENTED** — stated by existing project documentation but not independently confirmed;
- **INFERRED** — reasoned from evidence but not directly established;
- **HUMAN_CONFIRMED** — current intent confirmed by product/human authority;
- **RECONSTRUCTED_CONFIRMED** — reconstructed behavior/constraint reviewed and accepted as a current project contract;
- **UNKNOWN** — material question remains unresolved;
- **CONTRADICTED** — material sources disagree;
- **ACCEPTED_RISK** — unresolved uncertainty explicitly accepted within a bounded scope.

Model confidence is not one of these statuses.

## 6. Reconstruction workflow

```text
REGISTER
  |
  v
PIN ADOPTION SOURCE COMMIT
  |
  v
REPOSITORY INVENTORY
  |
  v
DOCUMENT HARVEST
  |
  v
CODE / TEST / CONTRACT DISCOVERY
  |
  v
HISTORY ARCHAEOLOGY
  |
  v
GAP + CONTRADICTION ANALYSIS
  |
  v
RETROSPECTIVE SPECIFICATION
  |
  v
PRINCIPAL / HUMAN RECONCILIATION
  |
  v
MATERIALIZE CANONICAL DOC SET
  |
  v
ADOPTION REVIEW
  |
  v
COMMIT ADOPTION BASELINE
  |
  v
ADOPTION READINESS GATE
  |
  v
READY
```

The process is iterative. Material contradiction discovered late can return the project to reconciliation.

## 7. Repository inventory

The first pass should be primarily deterministic.

Useful inventory includes:

- languages and major build systems;
- package/module boundaries;
- entry points;
- executable/services/libraries;
- public APIs;
- persistent stores and migration systems;
- external services;
- CI/build/deployment configuration;
- tests and test frameworks;
- generated code;
- documentation corpus;
- dependency manifests;
- important repository topology;
- current accepted HEAD/branch.

The inventory is evidence for reconstruction, not itself the architecture document.

## 8. Behavior and contract discovery

Current behavior should be reconstructed from multiple evidence classes.

High-value sources include:

- public interfaces and schemas;
- tests;
- error behavior;
- database constraints;
- state transitions;
- security checks;
- compatibility tests;
- configuration defaults;
- migration behavior;
- build/deployment rules.

Tests often encode executable requirements that prose never recorded.

Example:

```yaml
statement: "Deleting a user preserves audit history."
status: OBSERVED
evidence:
  - internal/users/delete.go
  - migrations/042_audit_fk.sql
  - TestDeleteUserPreservesAuditHistory
candidate_destination:
  - REQUIREMENTS.md
  - INVARIANTS.md
```

## 9. Git history archaeology

Current code establishes what exists now. Git history can help reconstruct why.

For material architecture or unusual contracts, adoption may inspect:

- introducing commits;
- reverts;
- related refactors;
- migration sequences;
- blame history;
- commit messages;
- historical tests.

History-derived rationale remains an inference unless the evidence actually establishes the decision.

Example:

```yaml
candidate_adr:
  decision: durable asynchronous order processing
  likely_rationale: synchronous path previously caused deadlock/retry failures
  evidence:
    - commit: ...
    - revert: ...
    - regression_test: ...
  epistemic_status: INFERRED
  human_confirmation_required: true
```

Do not manufacture historical certainty.

## 10. Ambiguity and contradiction ledger

Adoption maintains an explicit ledger for material unresolved questions.

Example:

```text
CONFLICT-017

Existing README:
  PostgreSQL is optional.

Current startup code:
  refuses to start without PostgreSQL.

Deployment:
  always provisions PostgreSQL.

Tests:
  no non-PostgreSQL configuration exists.

Observed conclusion:
  Current implementation requires PostgreSQL.

Unresolved product question:
  Is optional database support still a requirement,
  or is the README obsolete?

Authority:
  human/product
```

A contradiction is not automatically resolved in favor of code or prose. Code establishes current behavior; it does not necessarily establish desired product intent.

## 11. Retrospective canonicalization

Reconstruction produces candidate canonical artifacts.

They should answer, at minimum:

### VISION
- what the system is for;
- users/stakeholders;
- intended outcomes;
- boundaries/non-goals;
- major known constraints.

### REQUIREMENTS
- observed/current required behavior;
- confirmed product intent;
- non-functional requirements;
- compatibility obligations;
- unresolved/accepted-risk requirements with provenance.

### ARCHITECTURE
- current as-is system structure;
- component responsibilities;
- dependencies;
- major flows;
- persistent state;
- external boundaries;
- current architectural debt/divergence.

### INVARIANTS
- rules future work must preserve unless deliberately changed;
- data integrity constraints;
- protocol/security/compatibility invariants;
- source/evidence for reconstructed invariants.

### SECURITY
- trust boundaries;
- credential/data boundaries;
- externally reachable surfaces;
- privilege assumptions;
- known security constraints/unknowns.

### TEST_STRATEGY
- validation layers;
- authoritative test suites;
- integration/e2e expectations;
- known gaps;
- how a candidate establishes evidence of correctness.

### OPERATIONS
- runtime topology;
- deployment/runtime assumptions;
- observability;
- failure/recovery expectations;
- operational dependencies.

### ADR set
- current durable architectural decisions;
- reconstructed decisions where evidence is sufficient;
- explicit "rationale unknown" where history does not support a confident explanation.

## 12. Canonical docs must be committed

The adoption baseline is repository state.

Reconstructed canonical docs MUST be materialized in an isolated adoption worktree/branch and committed.

Conceptually:

```text
legacy source commit A
        |
        v
reconstruction evidence
        |
        v
adoption worktree
        |
        +-- docs/devcadience/...
        |
        v
adoption baseline commit B
```

Commit B establishes the first revision from which DevCadience may regard the repository as managed.

The baseline records/references the source commit A used for reconstruction so later changes during adoption cannot be silently mistaken for evidence from the original snapshot.

## 13. Existing native docs and canonical docs

DevCadience should preserve useful existing documentation.

It must not rewrite a repository merely to impose stylistic uniformity.

A canonical artifact can deliberately reference a strong native document, for example:

```markdown
# Architecture

Canonical status: CURRENT
Adoption source commit: abc123

Primary native architecture source:
- ../architecture.md

DevCadience summary:
- service A owns ...
- service B owns ...
- invariant ...

Adoption verification:
- compared against packages ...
- compared against tests ...
```

The canonical document remains the stable entry point.

If the native document later changes in a way that alters the contract, normal DevCadience change governance applies.

## 14. Adoption state machine

Conceptual states:

```text
UNREGISTERED
  -> REGISTERED
  -> RECONSTRUCTING
  -> RECONCILING
  -> BASELINE_CANDIDATE
  -> ADOPTION_REVIEW
  -> READY
```

Material new evidence may move a pre-READY project backward.

READY is a gate outcome, not a model confidence score.

## 15. Adoption Readiness Gate

A project may become READY only when all required conditions are satisfied.

Minimum gate:

- repository identity and adoption source commit are pinned;
- required canonical documentation artifacts exist;
- canonical artifacts are committed;
- major components and external interfaces are mapped;
- build/test strategy is established;
- material invariants are recorded;
- material documentation/code contradictions are resolved or explicitly bounded as accepted risk;
- critical product ambiguities are human-confirmed or explicitly deferred behind a safe boundary;
- security/trust boundaries are documented to the level needed for managed execution;
- canonical docs do not knowingly contradict current deterministic evidence without recording the discrepancy;
- the principal has reviewed the candidate baseline;
- remaining uncertainty is bounded and visible;
- an AdoptionDecision records READY and references the baseline commit/evidence.

The gate optimizes for **bounded uncertainty**, not exhaustive understanding of every file.

## 16. No undocumented managed project

Once READY, normal DevCadience work depends on the canonical baseline.

Normal task planning and Work Packages may cite:

- canonical requirements;
- canonical invariants;
- canonical architecture;
- ADRs;
- ProjectState;
- targeted EvidencePackets.

If required canonical documentation is missing, corrupt, or materially stale such that the project contract cannot be trusted, DevCadience must surface that as a project-readiness problem rather than silently continuing as if the baseline were valid.

## 17. Evolution after adoption

Retrospective reconstruction is a transition, not a permanent special mode.

After READY:

```text
Adoption Baseline
      |
      v
normal DevCadience lifecycle
      |
      +-- requirements changes
      +-- ADRs
      +-- Work Packages
      +-- validation/review
      +-- refactoring epochs
      +-- architecture reconciliation
```

Future documentation changes are prospective and deliberate.

The fact that an invariant or ADR originated through reconstruction remains provenance, but does not make it second-class after it has been accepted.

## 18. CLI direction

Conceptual surface:

```text
devcadience project adopt <repo>
devcadience project adoption status
devcadience project adoption investigate
devcadience project adoption review
devcadience project adoption finalize
```

The exact CLI belongs to its implementation milestone.

Interactive onboarding should explain:

- what was discovered;
- what docs already exist;
- what is missing;
- what is contradictory;
- what decisions require the user;
- what canonical artifacts DevCadience proposes to create;
- why the project is or is not READY.

## 19. Milestone ownership

This workflow depends on capabilities delivered across earlier milestones:

- **M2** provides repository inspection, worktrees and deterministic execution;
- **M3** provides environment/cognition capability routing;
- **M4A** provides semantic principal-host integration;
- **M4B** introduces Project Adoption and Retrospective Reconstruction;
- **M5** should prove both greenfield and brownfield end-to-end operation.

M4B should not delay basic M4A principal connectivity, but M5 should not claim the product hypothesis is proven only on a repository that was already perfectly documented.

## 20. Test strategy

Important adoption cases include:

- repository with no documentation;
- repository with only a README;
- repository with comprehensive but stale docs;
- docs that contradict current code/tests;
- tests that reveal undocumented invariant behavior;
- important rationale recoverable from Git history;
- rationale that cannot be recovered;
- a material product question requiring human authority;
- required canonical file omitted;
- required file present but explicitly NOT_APPLICABLE;
- baseline docs generated but not committed;
- adoption source commit changes during reconstruction;
- accepted-risk unknowns;
- attempt to delegate normal implementation before READY;
- project becoming READY after all gate blockers close.

Synthetic brownfield fixtures should deliberately include misleading/stale documentation so the workflow proves it distinguishes documentation from evidence.

## 21. Core principle

The goal is not to generate impressive documentation from an old repository.

The goal is:

> **reconstruct a trustworthy, explicitly evidenced engineering contract, materialize that contract in the repository, and establish a versioned boundary from which DevCadience can safely manage future work.**
