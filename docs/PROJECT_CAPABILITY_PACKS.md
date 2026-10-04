# Project Capability Packs

## Purpose

DevCadence must support new languages, frameworks, build systems, linters, application types, and project conventions without growing a vertical branch of core logic for each ecosystem.

The extension boundary is a **ProjectCapabilityPack**:

> **DevCadence core defines engineering-control semantics. Packs teach DevCadence how a particular ecosystem/project works. Project policy decides what becomes authoritative.**

A pack is not automatically trusted because it is built in, downloaded, human-authored, or model-generated. The producer is separate from the contract. Strictness comes from typed structure, conformance, evidence provenance, candidate isolation, and project authority.

## 1. Horizontal extension

Ordinary support for a new ecosystem should normally require a pack plus fixtures, not ecosystem-specific branches throughout task orchestration, ProjectState, review convergence, or authority logic.

A core change is justified only when an ecosystem reveals a genuinely reusable capability class missing from the generic model.

The conformance question is:

> Can a previously unknown ecosystem be supported by adding a pack and passing conformance, without teaching the control plane the ecosystem's name?

## 2. One PackRegistry, many producers

DevCadence has one logical **PackRegistry**. Packs may come from:

- built-ins shipped with a DevCadence release;
- a committed project-local pack;
- a home-directory pack library;
- an explicitly fetched GitHub repository/ref or HTTPS source;
- a human-authored pack;
- an LLM-synthesized pack produced during brownfield adoption or greenfield work;
- future organization/shared sources.

All producers satisfy the same pack contract.

The initial product does not require a central registry service or signing infrastructure. Explicit installation, source pinning, provenance, conformance, and Git/worktree rollback are sufficient.

## 3. Durable identity, origin, and lineage

A pack activation retains enough lineage to reproduce and later evolve it.

~~~yaml
pack:
  id: flutter
  schema_version: "1"
  revision: "2026.10.1"
  content_digest: "sha256:..."

origin:
  kind: github
  locator: "https://github.com/example/devcadence-flutter-pack"
  requested_revision: "v2026.10.1"
  resolved_revision: "git-sha..."

validation:
  provenance_class: conformance_validated
  conformance_report: ...
  skill_trials: [...]
~~~

Other origins may be built-in release assets, project-local paths, home-library paths, generated-from-project evidence, or HTTPS artifacts pinned by digest.

The project records the exact activated revision/digest, not merely a mutable source URL.

### Update flow

If the origin can produce a newer version, DevCadence may:

1. resolve a candidate successor revision;
2. show the mechanics/Skills/capability diff;
3. run compatibility checks and conformance;
4. trial changed Skills when applicable;
5. produce a PackUpgradeProposal;
6. re-evaluate affected ProjectHealthContract/toolchain bindings;
7. activate only after policy permits;
8. preserve prior revisions in historical evidence.

A pack MUST NOT silently auto-update an active project.

Generated/project-local packs may also evolve; lineage points to their source evidence/path so M9 can propose a new revision later.

## 4. Agent Skills + DevCadence manifest

Pack knowledge uses the open **Agent Skills** layout (`SKILL.md` plus optional references/scripts/resources) for model-facing procedural knowledge, plus a DevCadence manifest for deterministic mechanics and metadata Skills do not express.

Illustrative layout:

~~~text
flutter/
  PACK.yaml
  skills/
    adopt-project/
      SKILL.md
      references/
    implement-feature/
      SKILL.md
    review-change/
      SKILL.md
  mechanics/
    environment.yaml
    health.yaml
    run.yaml
    test.yaml
    install.yaml
  fixtures/
    minimal-app/
~~~

Skills are host-neutral pack content. DevCadence may project them into a host-native skill location, but host discovery is not the authority path for compiler-driven invocations.

## 5. Mechanical manifest

The deterministic manifest can cover the full project lifecycle.

**Environment:** required SDK/runtime/tool versions, probes, supported ranges, optional installation/remediation guidance, platform constraints.

**Build/typecheck:** build commands, typecheck/static compilation, generated-artifact checks, artifact locations.

**Health:** formatting, lint/static analysis, unit/integration tests, coverage, generated-code drift, API/schema checks, dependency hygiene.

**Mutation testing:** whether ecosystem-native mutation testing is supported (`supported`, `unsupported`, `unavailable/unknown`), tool provider and version/provenance, invocation argv/flags, scope mechanism (e.g. package, path, changed-files diff), timeout expectations, result adapter/parser, and known limitations.
Pack manifests declare a `mutation_testing` mechanic section when available:
```json
{
  "support": "supported",
  "tool": "go-mutesting",
  "command": ["go-mutesting", "--timeout", "30s"],
  "scope_mode": "package_or_diff",
  "output_format": "text_or_json",
  "min_mutation_score": 0.80
}
```
Core orchestration implements a **dual execution path**:
1. *Mechanical Path:* If the active capability pack advertises `supported` and the tool passes environment probes, the mutation mechanic executes against scoped packages or diffs, reporting the empirical mutation score.
2. *Adversarial Model Path:* If mutation tooling is unsupported, unavailable, or too resource-intensive for rapid iteration, the system uses the EWP's explicit **Mutation Catalog** where an independent Test Adequacy & Mutation Reviewer verifies that all cataloged plausible mutations fail existing tests.
Neither path is allowed to silently pass if the contract mandates mutation verification for a high-risk package.

**Run/dev:** local run command, dev server, emulator/simulator lifecycle, readiness probes.

**Install/deploy:** artifact install, simulator/device install, optional deployment and post-deploy probes.

Commands are explicit and module-scoped. Pack mechanics never grant themselves filesystem/process/network authority; active trust policy and the process substrate bound execution.

## 6. Generic result interchange

Where practical, packs project tool-specific results into stable formats:

- SARIF for static-analysis findings;
- JUnit for tests;
- LCOV/Cobertura or another supported structured coverage representation;
- typed DevCadence finding/result records.

The core should know result semantics, fingerprints, candidate identity, baseline, and provenance—not every tool's syntax.

Exit-code-only commands remain a valid but explicitly weaker integration tier.

## 7. Conformance harness

A pack can be produced by anyone; confidence comes from validation.

Conformance checks, as applicable:

- schema/manifest validity;
- declared tool/version probes;
- commands run in the advertised fixture/project shape;
- outputs parse into declared result formats;
- module/path scoping is correct;
- declared health checks detect representative seeded failures;
- required commands fail honestly rather than silently no-op;
- mechanics remain inside declared capability boundaries.

### Seeded failures

For important checks, conformance MUST NOT rely solely on failure fixtures/seeds authored by the same pack producer.

DevCadence core owns a small **generic seed catalog by check class**, such as:

- format violation;
- lint/static-analysis finding;
- type/compile error;
- failing assertion/test;
- generated-code drift.

The pack may supply an ecosystem-specific **seed realization adapter** describing how to materialize a core seed safely in a disposable worktree/fixture.

Seed realization is declarative by default (bounded patch/append/replace operations applied by the harness). Any executable realization helper is subject to the same sandbox/trust-profile gating and authority caps as other unvalidated pack commands.

The harness verifies that:

1. the mutation is bounded/minimal (single file where practical and within a configured diff-size cap);
2. the target check class transitions from clean/pass to finding/fail;
3. unrelated check classes remain unaffected where the fixture permits discrimination;
4. removing/reverting the seed restores the prior result where applicable.

This discrimination requirement provides a tool-independent oracle for the intended failure class: a format seed should not need to break tests, and a failing-test seed should not need to break formatting.

Additional pack-authored or independently authored seeds may supplement the core catalog, but provenance MUST record who supplied the seed.

Validation evidence records seed independence separately from pack origin. A pack validated only by its own seeds is weaker evidence than one validated by core-owned or independently authored seeds.

This is especially important for LLM-synthesized mechanics because it prevents the same producer from designing both the check and the only test it knows how to pass.

## 8. Provenance classes

Every pack/result preserves honest provenance. Initial classes can include:

- `builtin`;
- `human_authored`;
- `fetched_unvalidated`;
- `generated_unvalidated`;
- `conformance_validated`;
- `generated_conformance_validated`;
- `generated_trial_validated`.

A permissive profile may use an unvalidated pack, but DevCadence MUST NOT report it as equivalent to a validated one.

Health evidence records pack revision/digest, contract revision, tool versions, candidate/base identity, and validation provenance.

## 9. Skills and prompt knowledge

Pack Skills are advisory procedures: adoption/reconstruction, implementation conventions, testing, debugging, migration, and review guidance.

Skills may declare role/module/toolchain/action/archetype applicability and token budgets.

For compiler-driven cognition, the Cognitive Invocation Compiler selects applicable Skills deterministically. Model similarity may improve optional retrieval but never decides mandatory obligations.

### No double injection

- Compiler-driven invocations receive compiler-selected pack Skills.
- Host-driven sessions may receive a deterministic provenance-tagged projection.
- The same guidance is not independently injected through both paths.

### Authority

Pack Skills MUST NOT weaken system/project invariants, override an EWP, expand tool authority, turn advisory guidance into mandatory authority, or bypass context budgets.

Operative obligations remain typed project/system authority.

### Skill trials

A generated/changed Skill may be trialed in a scratch worktree against a representative task and the real ProjectHealthContract.

A passing trial demonstrates only that **this bounded task completed without violating the available acceptance/health contract**. It does not prove general Skill quality, completeness, or superiority, and it is only as strong as the contract and evidence used by the trial.

Skill validation records the task/evidence/health-contract revision and whether the trial task was authored independently of the Skill producer. Trial results can later feed M9 governed learning without being overclaimed as universal correctness.

## 10. Brownfield synthesis

Brownfield adoption may discover a stack with no suitable installed pack.

A Principal/LLM may synthesize a candidate pack from repository reality: manifests, CI, build scripts, format/lint tools, tests/coverage, run/dev instructions, generated-code rules, framework conventions, and docs.

The candidate pack then goes through deterministic conformance and, where valuable, seeded failures and Skill trials.

This supports bespoke/internal stacks without core changes or a centrally maintained ecosystem matrix.

## 11. Recommendation and unsupported stacks

LLMs may recommend packs from greenfield needs, a ProjectBlueprint/EWP, discovered brownfield evidence, or missing capabilities.

A PackRecommendation is advisory and records candidate pack/source/revision, rationale, expected capabilities, compatibility assumptions, validation provenance, and whether activation changes project policy.

Existence, compatibility, installation, and activation remain deterministic/policy decisions.

When no compatible pack exists, DevCadence reports unsupported/partial capability and bounded fallbacks rather than pretending model knowledge equals supported tooling.

## 12. Research pointers, not a stale package catalog

Packs may contain bounded pointers to authoritative upstream docs, package registries, migration/version docs, and ecosystem tool docs.

M6 SHOULD NOT require a large curated library/package recommendation catalog. Dependency choice remains architecture/research work; packs may point to authoritative sources without creating hidden recommendations.

## 13. Invariant candidates

A pack may propose framework/toolchain invariant candidates. These become operative only through project/adoption governance and deterministic compiler admission.

A pack MUST NOT self-promote an invariant.

## 14. Compositional capabilities

Avoid monolithic combinatorial IDs such as `typescript-pnpm-vite-eslint`.

A module may compose:

~~~text
language: typescript
package_manager: pnpm
framework: react
build_system: vite
checks: tsc + eslint + vitest
~~~

Flutter may compose Dart, Flutter SDK, Android/Gradle, and iOS/Xcode capabilities.

A resolved ToolchainProfile is a validated composition for a module. Dependencies/conflicts are explicit and MUST NOT be resolved by incidental load order.

## 15. Trust profiles and always-on integrity

Trust is a policy dial.

**Personal/permissive:** auto-adopt where configured, warn/record more often than block, allow local/fetched/generated packs, rely on isolation/conformance/rollback.

**Team/strict:** confirmation/source controls, review for pack/health changes, stricter provenance requirements.

Start with presets rather than dozens of toggles.

Every profile MUST preserve:

1. **A candidate cannot rewrite its own judge.** The accepted base judge includes ProjectHealthContract, debt baseline, resolved ToolchainProfile composition, exact activated pack mechanics/result-adapter/fixture digests, and managed acceptance adapters. A candidate proposing any of these changes is still evaluated under the base judge.
2. **Evidence honesty.** Pack/contract/tool revisions and provenance are recorded.
3. **Authority caps.** A pack/Skill cannot grant itself tools or authority.
4. **Isolation/rollback.** Trials and agent changes use controlled worktrees/processes.

Permissive means fewer prompts, not weaker evidence semantics.

### Judge drift and newly uncovered scope

Activated pack content is re-digested at use. If project-local/home/fetched pack content no longer matches the activated digest, DevCadence reports **PackDrift** and applies the active profile; drift MUST NOT be silently accepted as the same capability revision.

A legitimate policy/pack/contract change has two acceptance dimensions: the implementation candidate is judged under the accepted base judge, while the proposed successor policy/capability delta is accepted separately by the configured authority.

The successor-acceptance authority MUST be capability-separated from the candidate producer. The worker that authored the candidate MUST NOT possess, derive, invoke, or mutate the credential/API/IPC/UI/state capability that records or promotes approval. An approval prompt or CLI command reachable from the worker's own shell/tool envelope does **not** satisfy this rule.

A personal profile may use explicit human confirmation through a parent/control-plane UI or another channel outside the worker capability envelope; a team profile may require independent review/approval. Only after integration/acceptance does the successor become the new base judge.

"Uncovered scope" is defined at the managed-policy boundary, not per new file. New files under an already covered module/path remain governed by that module's existing checks. A new module, new toolchain/capability component, new check class, or new executable/action surface (for example deploy/install) that lacks base coverage is **uncovered**, not green. Greenfield/adoption bootstrap is the special initial case: the first accepted contract establishes the initial judge rather than violating a nonexistent base.

## 16. Health lineage and no-new-debt

Health evidence binds to base/candidate identity, contract digest, pack digest, concrete tool versions, and finding fingerprints.

Historical debt SHOULD use stable finding fingerprints rather than raw counts where possible.

If a pack/tool/rule revision changes finding semantics, DevCadence performs explicit baseline reconciliation rather than silently comparing incompatible measurements.

## 17. Pack upgrade and learning lifecycle

~~~text
active pack revision A
        |
candidate successor B
        |
diff + compatibility + conformance
        |
skill trials / health replay
        |
PackUpgradeProposal
        |
project policy/review
        |
activate B
~~~

Historical evidence remains tied to A. M9 may propose pack/Skill improvements from trajectories; promotion remains evaluated, versioned, reversible, and explicit.

## 18. Initial distribution

Built-ins may be repository assets embedded with `go:embed` for offline bootstrap.

The same loader/contract applies to embedded, home-library, project-local, and fetched packs. Built-in packaging is convenience, not a separate semantic type.

Manual CLI installation from a GitHub ref/HTTPS source or by placing a folder into the pack library is sufficient initially.

## 19. Verification

M6 should prove:

- at least two genuinely different reference ecosystems (for example Go and Flutter);
- one LLM-synthesized unfamiliar "Zeta" pack;
- seeded-failure detection;
- Skill trial;
- pack source pinning and successor upgrade;
- compositional capabilities;
- unsupported-stack diagnostics;
- permissive and stricter trust presets.

## 20. Principle

> **Do not trust the pack producer; validate the pack contract, preserve provenance, bound authority, and let project policy decide activation.**
