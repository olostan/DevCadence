# Engineering Work Package: WP-M3C-R1 — Structural Consolidation Refactoring Epoch

- **Milestone:** M3C — Cognition Resource and Session Substrate
- **Kind:** behavior-preserving Refactoring Epoch
- **Base commit:** `688593d9a562287056a200f236ea7bc834e57508` (origin/main, merge of PR #19 — WP-M3C-2B Cognitive Invocation Compiler)
- **Branch:** `refactor/m3c-structural-consolidation`
- **Work Package ID:** `WP-M3C-R1`
- **Version:** 1.0
- **Status:** Implemented — independent acceptance recorded (ACCEPT_WITH_CONDITIONS, see §Acceptance record); owner confirmation pending

---

## 1. Why this epoch exists

PR #19 completed a large architectural slice of M3C and materially stabilized several concepts that were still evolving during implementation:

- deterministic rule admission;
- effect-capability authority;
- canonical system invariant catalogs;
- frozen catalog provenance and digests;
- prompt projection and invocation identity;
- context profiles;
- evidence leases;
- cognitive state capsules;
- future seams for project-local and organization authority.

The implementation is functionally accepted, but the review history and a repository-wide structural scan show that some files accumulated multiple independently changing responsibilities while these concepts were converging.

This is exactly the trigger described by:

- DCI-060 — Refactoring is scheduled;
- DCI-061 — Architecture is periodically reconciled with reality;
- DCI-062 — Feature throughput does not erase health debt;
- DCI-063 — Health evidence combines tools and semantic review;
- `docs/REFACTORING_AND_HEALTH.md`.

This epoch is therefore **not cleanup for aesthetics** and is not driven by a line-count target. It materializes stable conceptual boundaries before further feature work increases coupling.

---

## 2. Prime directive

**Preserve externally observable behavior.**

This Work Package is a structural refactor, not a feature milestone.

Unless an explicitly documented contradiction is discovered and escalated, the implementation MUST NOT intentionally change:

- protocol or JSON-schema semantics;
- exported type names or field meanings;
- invariant admission behavior;
- rule mappings or canonical invariant content;
- capability normalization or fail-closed tool-authority behavior;
- prompt rendering semantics;
- context-fit behavior;
- digest semantics or provenance identity;
- evidence lease behavior;
- cognition routing decisions;
- setup/remediation decisions;
- event/reducer state-transition semantics;
- error categories relied on by callers;
- CLI behavior.

If preserving behavior is impossible because an architectural contradiction is discovered, stop and raise a separate `RefactoringProposal` / revised EWP rather than hiding the semantic change inside the refactor.

---

## 3. Health snapshot at the base commit

The following are **signals**, not automatic defects.

### Highest-priority responsibility concentration

#### A. `internal/cognition/compiler/admission.go`

Approximate base snapshot:

- ~1,666 lines;
- 43 functions;
- 14 declared domain/support types;
- ~21 `RuleRegistry` methods;
- `CompileInvocation` ~475 lines;
- `ResolveAdmittedRules` ~207 lines.

It currently contains several distinct responsibilities:

1. admission classes and authority-source types;
2. capability vocabulary and normalization;
3. tool capability validation;
4. active-capability derivation;
5. rule registry storage/lifecycle;
6. catalog metadata access;
7. freeze/digest mechanics;
8. reverse coverage;
9. rule applicability and dependency closure;
10. compiler request/output types;
11. compiler orchestration;
12. tool-schema identity extraction;
13. role-core construction;
14. evidence freshness checks;
15. pack and invocation digest construction.

This is the clearest current structural hotspot.

Its companion `admission_test.go` is also ~44 KB and mirrors the production responsibility concentration.

#### B. `internal/setup/doctor.go`

Approximate base snapshot:

- ~1,060 lines;
- `discoverEndpoints` ~282 lines;
- `evaluateReadiness` ~149 lines.

Responsibilities include:

- state-root diagnostics;
- Git checks;
- hardware checks;
- principal-host checks;
- cognition endpoint discovery;
- credential interpretation;
- resource inventory synthesis;
- profile recommendation;
- readiness evaluation.

This is a strong later-epoch candidate, but it is not the first code target of this PR unless the compiler consolidation proves insufficiently separable without it.

#### C. `internal/setup/planner.go`

Approximate base snapshot:

- ~644 lines;
- `PlanWithContext` ~416 lines.

It behaves as a growing procedural remediation rule engine covering directories, Git, drivers, runtimes, caches, authentication, model resolution, dependencies and action identity.

This should eventually become composable planning rules/stages rather than one expanding function.

### Secondary candidates

- `internal/cognition/service.go` — ~887 lines; mixes discovery, declaration overlays, probing, reconciliation, evidence persistence and assessment.
- `internal/protocol/setup.go` — ~1,263 lines / many protocol types. Mostly cohesive as a protocol corpus; likely a navigation/file-layout split, **not** a redesign.
- `internal/protocol/cognition.go` — ~1,104 lines / many protocol types. Same caution: file decomposition may help, but do not create abstractions solely to reduce line count.
- `internal/state/reduce.go` — ~735 lines / many event handlers. Cohesive reducer architecture; possible handler-file split while preserving a single reducer model.
- `internal/worktrees/worktrees.go` — ~765 lines; lifecycle coordination, manifest persistence, Git operations and recovery could be separated internally.
- `internal/cognition/drivers/metering.go` — ~608 lines; meter state, metered session and metered driver are separable layers.
- `internal/setup/profiles.go` — ~489 lines but `Recommend` is ~315 lines; candidate decision-engine decomposition.
- `internal/cognition/drivers/mediation.go` — several mediator layers in one file.
- Large test files in setup/compiler/protocol/credentials should be split by behavior where that improves reviewability, not by arbitrary line thresholds.

### Explicit non-goal

Large cohesive files are not automatically bad.

For example, event payload collections, protocol shape collections and reducers may remain large when the responsibility is singular and navigation/test isolation are acceptable.

No acceptance criterion is based on "every file < N lines".

---

## 4. Epoch structure

This EWP records the whole-repository health findings, but implementation MUST proceed in bounded slices.

### R1A — Cognitive Invocation Compiler decomposition — REQUIRED in this PR

Refactor `internal/cognition/compiler` so stabilized responsibilities are explicit while retaining the same Go package and behavior.

Target conceptual layout (names may change when evidence justifies it):

```
internal/cognition/compiler/
    authority.go          # AdmissionClass, AuthoritySourceKind, Rule, exclusions
    capabilities.go       # capability vocabulary, normalization, tool declarations/binding
    registry.go           # RuleRegistry lifecycle, storage, freeze/provenance
    admission.go          # applicability, reverse coverage, dependency closure
    compile.go            # CompileRequest, Compiler, CompileInvocation orchestration
    digest.go             # canonical digest inputs/calculation
    catalog.go            # DevCadence system invariant extraction/mapping
    profile.go
    renderer.go
    evidence.go
    capsule.go
    retrieval.go
    strategy.go
```

This is a conceptual ownership map, not a mechanical filename mandate.

#### R1A MUST

- preserve package `compiler`;
- preserve exported API unless an export is demonstrably accidental and removal is separately approved;
- keep the canonical system catalog behavior identical;
- preserve exact fail-closed authority semantics established in PR #19;
- preserve catalog / pack / invocation digest behavior byte-for-byte for equivalent inputs;
- preserve deterministic ordering;
- preserve concurrency behavior and registry immutability;
- split tests by behavioral boundary where useful;
- keep each extracted concept understandable without requiring cross-file ping-pong for trivial logic.

#### R1A MUST NOT

- add project-invariant composition;
- implement organization policy;
- implement distributed/multi-user IAM;
- redesign capability semantics;
- change rule mappings;
- redesign renderer formats;
- change context budget policy;
- add generic frameworks merely to reduce duplicated lines;
- introduce interface layers that have only one implementation unless they express an existing architectural boundary.

### R1B — Cognition orchestration decomposition — OPTIONAL only if R1A remains small and independently reviewable

Potential targets:

- `internal/cognition/service.go`;
- `internal/cognition/drivers/metering.go`.

Desired ownership:

- endpoint discovery/normalization;
- probing/evaluation;
- evidence persistence;
- endpoint reconciliation;
- meter state;
- session wrapper;
- driver wrapper.

Do not pull R1B into this PR if doing so materially increases review complexity. A separate follow-up PR is preferred over a large mixed refactor.

### R1C — Setup orchestration — FOLLOW-UP, not implementation scope of this PR

Record for the next refactor slice:

- decompose `Doctor` orchestration from endpoint discovery and readiness synthesis;
- replace `Planner.PlanWithContext`'s monolithic decision procedure with explicit composable planning stages/rules;
- keep the same plan output and deterministic action identity;
- preserve authorization, credential, remediation and recovery semantics.

### R1D — Protocol/navigation and remaining structural debt — FOLLOW-UP

Candidates:

- split large protocol files by concept **within the same package**;
- split reducer handlers by event family while preserving one projection/reducer model;
- separate worktree manifest persistence from Git lifecycle coordination;
- split oversized test files by behavior/scenario.

These are explicitly not required merely to satisfy a file-size metric.

---

## 5. Refactoring method

The implementation agent MUST use the following sequence.

### Step 1 — Freeze behavioral evidence

Before structural edits:

1. run the complete existing test suite;
2. record package-level test status;
3. identify golden/deterministic tests for:
   - catalog digests;
   - mapping digests;
   - invocation digests;
   - prompt projections;
   - rule admission;
   - tool authority;
   - context-fit decisions;
4. add characterization tests **only where behavior is insufficiently locked**.

Do not rewrite tests first in a way that could accidentally bless changed behavior.

### Step 2 — Draw responsibility boundaries

For each extraction, state:

- what concept owns the code;
- why it changes independently;
- what dependencies it should have;
- what dependencies it should not have;
- whether the extraction changes visibility.

Avoid generic utility packages. Prefer domain names.

### Step 3 — Move before redesign

First make behavior-preserving moves/extractions.

Prefer:

- same package;
- same concrete types;
- same function signatures;
- same ordering;
- same errors;
- same serialization.

Only after tests are green should local simplification occur.

### Step 4 — Simplify locally

After structural separation, look for:

- duplicated validation;
- duplicated normalization;
- dead compatibility branches;
- repeated canonicalization;
- comments that describe old review history rather than stable intent;
- names that no longer match responsibility.

Delete accidental complexity where safe.

Do **not** turn several straightforward functions into an abstract framework.

### Step 5 — Verify after every slice

At minimum after each logical extraction:

```
gofmt
go vet ./...
go test -count=1 ./...
```

Run `go test -race -count=1 ./...` before requesting review and whenever concurrency-sensitive code is moved.

### Step 6 — Independent semantic review

Review must assess more than test status:

- are responsibilities actually clearer?
- did coupling decrease?
- did public API grow unnecessarily?
- are new names aligned with DevCadence domain language?
- were behavior and provenance preserved?
- were any "temporary" adapters created that should instead be deleted?
- did tests become more behavior-oriented rather than mirroring file layout?

---

## 6. Stability invariants for R1A

The following properties must be explicitly tested or otherwise evidenced before acceptance.

1. **Catalog equivalence**
   - same canonical invariant set;
   - same source revision;
   - same mapping revision;
   - same normative source digest;
   - same authority projection digest;
   - same catalog digest.

2. **Admission equivalence**
   - same admitted rule IDs and selection rationale for representative fixtures;
   - same dependency closure;
   - same unknown-domain/reverse-coverage failures.

3. **Capability equivalence**
   - aliases normalize identically;
   - empty/unknown authority still fails closed;
   - read-only behavior unchanged;
   - `ToolSchemas` and legacy `Tools` remain mutually exclusive;
   - 1:1 tool/declaration binding unchanged.

4. **Invocation equivalence**
   - same `ContextManifest`;
   - same semantic `ContextPack`;
   - same rendered projection;
   - same `PackDigest`;
   - same `InvocationDigest`.

5. **Context-fit equivalence**
   - same fit/unfit result for identical profile/input;
   - mandatory obligations are never dropped.

6. **Evidence/capsule equivalence**
   - same freshness and revision failures;
   - same lease inclusion;
   - same cognitive-state dependency behavior.

7. **Error compatibility**
   - existing callers observe the same error categories for equivalent invalid inputs.

---

## 7. Testing and verification quality bar

Required before acceptance:

```
gofmt -l .
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...
make verify
git diff --check
```

Additionally:

- run focused compiler tests repeatedly while extracting;
- compare deterministic digests against pre-refactor fixtures;
- inspect `go test ./...` package output for unexpected new skipped/disabled tests;
- no test may be deleted solely because the refactor makes it inconvenient;
- new tests should cover boundaries, not implementation line placement.

If feasible without adding brittle tooling, include a before/after structural snapshot:

- largest production files;
- largest functions;
- public symbols changed;
- package dependency changes.

The goal is not to optimize a score. The snapshot demonstrates whether the intended structural debt actually decreased.

---

## 8. API and compatibility policy

Default policy: **no externally visible API changes**.

If an exported compiler symbol must move between files, that is not an API change because package identity remains the same.

If an exported symbol is proposed for removal/renaming:

1. prove it has no supported callers;
2. document why removal improves architecture;
3. treat it as a semantic/API change requiring explicit approval rather than silently folding it into the refactor.

JSON schemas and protocol types are outside R1A except where a test fixture must be referenced. They must not change semantically.

---

## 9. Comments and documentation cleanup

PR #19 accumulated useful historical review comments in code while the design was converging.

During refactoring:

- preserve comments that explain **why an invariant exists**;
- rewrite comments that refer primarily to "Pass 3", "Finding 5", "round-6 review", etc. into stable architectural explanations when the history is no longer needed to understand the code;
- do not remove provenance that is genuinely required for audit;
- do not copy the same rationale into multiple extracted files.

The code should read as the final architecture, not as a transcript of how the review converged.

---

## 10. Prohibited refactor failure modes

Reject the refactor if it exhibits any of these patterns:

- file splitting without responsibility improvement;
- "utils", "common", "helpers" dumping grounds;
- new generic interfaces with only speculative future consumers;
- circular dependencies hidden through callbacks;
- semantic changes disguised as cleanup;
- changed deterministic hashes without an explicitly approved reason;
- snapshots/golden tests blindly updated to match new output;
- broad renames that obscure review without improving domain language;
- reformatting unrelated packages;
- mixing setup/doctor/planner cleanup into the compiler slice merely because it is nearby;
- weakening tests to make extraction pass;
- increasing public API surface to make moving code easier;
- duplicating policy in multiple extracted components.

---

## 11. Suggested commit structure

Keep commits independently understandable and preferably green:

1. `test(cognition): freeze compiler behavioral equivalence`
2. `refactor(cognition): extract authority and capability model`
3. `refactor(cognition): extract rule registry and admission resolution`
4. `refactor(cognition): isolate compiler orchestration and digests`
5. `test(cognition): reorganize compiler tests by behavioral boundary`
6. `refactor(cognition): simplify duplicated local logic`
7. optional cognition-driver/service slice only if still reviewable

Do not optimize for the exact commit list; optimize for reviewable semantic checkpoints.

---

## 12. Rollback strategy

Because this is behavior-preserving:

- each extraction should be revertible independently;
- avoid migrations;
- avoid persistent-state format changes;
- avoid schema changes;
- avoid generated data rewrites.

If a structural change creates ambiguous behavior, revert that slice rather than patching around it with compatibility glue.

---

## 13. Acceptance criteria

This refactoring slice is accepted only when all are true:

1. Existing functional behavior is preserved.
2. Full verification passes, including race tests.
3. Canonical compiler digests/projections remain equivalent for fixed inputs.
4. No protocol/schema semantic change occurred.
5. No public API expansion occurred without explicit justification.
6. `admission.go` no longer owns unrelated authority, capability, registry, compiler-orchestration and digest responsibilities simultaneously.
7. `CompileInvocation` is materially decomposed into named stages/helpers whose ownership is clear, without hiding behavior in generic frameworks.
8. Tool-authority fail-closed guarantees from PR #19 remain structurally obvious and tested.
9. Test organization reflects behavioral boundaries rather than one monolithic admission test file.
10. New files have clear domain ownership; no generic dumping-ground module is introduced.
11. Unrelated repository files are unchanged.
12. Independent holistic review finds architecture easier to reason about, not merely distributed across more files.
13. A short before/after health note records what structural debt improved and what was deliberately deferred to R1B/R1C/R1D.

---

## 14. Deferred follow-up inventory

Do not lose these findings if R1A is merged first.

### High priority
- setup Doctor responsibility decomposition;
- setup Planner rule/stage decomposition;
- cognition Service discovery/probe/reconciliation decomposition.

### Medium priority
- metering state/session/driver separation;
- worktree lifecycle vs manifest persistence;
- profile recommendation decision-engine decomposition;
- mediator-layer decomposition.

### Navigation/test health
- protocol file splits by stable concept;
- reducer handler splits by event family;
- compiler/setup/protocol/credentials test-file decomposition.

Each should receive its own bounded EWP or explicit R1 sub-slice rather than silently expanding R1A.

---

## 15. Definition of done

The goal is not "smaller files".

The epoch succeeds when the next engineer or agent can answer, with materially less context:

- Where is tool/effect authority defined?
- Where is a rule registry frozen and authenticated?
- Where is applicability resolved?
- Where is an invocation compiled?
- Where are identities/digests computed?
- Which component owns each invariant?
- Which behavior is safe to modify without understanding unrelated compiler mechanics?

A successful refactor leaves **less accidental complexity, fewer mixed responsibilities, stronger local reasoning boundaries, and the same externally observable behavior**.

---

## Acceptance record (autonomous run 1, issue #36)

Independent clean-context review of `688593d..23f2dc1` by a reviewer subagent that did not author R1. Verdict: **ACCEPT_WITH_CONDITIONS**. This is a model-review signal, not owner acceptance; the owner confirms.

Deterministic evidence (reviewer-run, single runs): `go build`, `go vet`, `go test -count=1 ./...` pass at base and head; `go test -race ./...` and `make verify` pass at head; go/ast comparison of non-test declarations: 162 identical, 1 changed (`CompileInvocation`, decomposed into 11 unexported helpers), 0 removed; the head `equivalence_test.go` golden digests pass against the pre-refactor package (genuine pre-refactor constants); no protocol/schema/`go.mod` change; exported API (`go doc -all`) unchanged apart from comment text.

Health note (corrects the PR #20 comment): `admission.go` 1666 → 336 lines; `CompileInvocation` 479 → **106** lines (the earlier "~70" claim was inaccurate); `admission_test.go` 1307 → 401; package total 5892 → 6384 lines (new test files and headers; no behavior added); top-level tests 42 → 47, test names 58 → 67, no test lost.

Observations kept for the record: (1) `totalCatalogInvariants` is now read under the registry read lock (harmless; registry frozen); (2) `ResolveAdmittedRules` remains a 207-line function and `assembleContextPack` takes 10 parameters (placement/size nits, not required by this EWP); (3) a few "Finding/Pass/round" review-history comments remain in `evidence.go`, `profile.go`, `retrieval.go`; (4) repo-wide `gofmt -l .` lists 10 files in other packages at both commits (pre-existing debt, compiler package clean).

Not verified by the reviewer: unchanged files beyond the AST function-body diff (imports/`init` order); assertion strength of moved tests line by line; flakiness (single runs); import-graph direction; inline PR review threads. Independence caveat: PR #20 reviews were posted under the repository owner's account, so author/reviewer identity separation could not be established from that evidence (cf. DCI-134; ActorProvenance in WP-M3C-5 exists to make this explicit).
