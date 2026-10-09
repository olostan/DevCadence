# WP-M5-SH1 — First DevCadence self-hosted coding loop

- Status: **PROPOSED / implementation-ready sequencing target; independent review required**.
- Priority: **P0 product viability**. This is the next useful-capability gate, not an additional formal milestone required to complete all M5 research.
- Scope: a single developer, one trusted local repository, one bounded task, explicit opt-in experimental permissions, and human-controlled integration. Default policies remain strict.
- Existing dependencies: M5-R1 native executor, M5-R2 independent reviewer/acceptance, M5-R3-A real loopback client, M5-R4 minimum protected authority. Use actual merged features instead of duplicating them.
- Non-goals: empirical corpus, multi-tier campaign comparison, distributed workers, unattended PR merging, generic host portability beyond the first Antigravity path.

## Outcome

**DevCadence modifies DevCadence itself** using a real local LLM and its own executable workflow. An Antigravity principal delegates a bounded EWP through the semantic MCP interface, a worker modifies a worktree, deterministic checks run, an independent review/repair cycle executes (or fails explicitly for missing reviewers), and a candidate is presented to the owner for integration.

The first successful outcome must be backed by: requested EWP id/digest, exact model/runner identity, input source revision, bounded command trace, changed-file manifest and commit, test results, reviewer identity/findings, final outcome and owner disposition. A mock model, synthesized patch injected by the harness, or an Antigravity agent performing the edits instead of DevCadence **does not count**.

## P0 ordered implementation work packages (bounded)

### SH1-1 — Compose actual execution (required)

- Wire the existing host-neutral `TaskExecutor` to the R1 executor, R3-A loopback provider and persisted task/attempt state; missing provider returns an actionable error.
- Run one real Ollama model request, collecting truthful endpoint/provenance and costs/usage (unknown remains unknown).
- Prove request -> model -> task-attempt -> committed candidate with one small disposable Go project; no fake driver in the success-path test.
- Do not wait for R3-B empirical replay, R3-C campaigns, or R3-D profiles.

### SH1-2 — Make bounded editing practical (required)

- Implement/finish R1-D `apply_patch` or exact-range replacement in worker worktrees (not principal workspace), with explicit paths, write-scope checks, symlink rejection, deterministic errors and dry-run tests.
- The first model session must successfully modify a Go source file and add or change a test. Do not substitute manual edits.
- Allow explicitly authorized local shell/test commands in a trusted project for YOLO; do not expose host credentials implicitly.

### SH1-3 — Close the feedback loop (required)

- Run project-specific `go test`/format/health checks in the candidate worktree, preserve failures, and permit bounded model repairs.
- Call an independent reviewer where available (different actor/model if configured); if no independent reviewer is available, label the result `review_unavailable` and require manual owner approval instead of fabricating independence.
- Implement the minimum acceptance/hand-off decision: evidence snapshot, no acceptance-by-label, no automatic merge or destructive integration.
- Demonstrate retry without duplicate model calls after uncertain interruption, or explicitly refuse retry with the durable reason.

### SH1-4 — Antigravity dogfood (required)

- Connect Antigravity to the actual `devcadence-mcp` semantic commands; verify project state, EWP creation, delegation, progress, validation, review and candidate hand-off.
- Start with a disposable Go project; then choose a small, testable DevCadence issue (documentation/test/leaf package) with a hard write scope.
- Collect a reproducible dogfood record including runner/model versions, task inputs, git before/after, diffs, health output and exact manual intervention.
- Owner reviews and manually integrates the result. This achieves **SELF_HOST_ALPHA**, not general autonomous production readiness.

## P0 safety/YOLO policy

Self-host alpha is **opt-in for a developer-owned, trusted checkout**. A project preference may authorize unconfined host execution persistently, but the UI/CLI must conspicuously display `unsafe_unconfined` and preserve audit traces. A strict CLI override must always work. On initial enablement, warn that tests and model-generated binaries may read/write user files, run subprocesses and reach the network.

YOLO only relaxes containment/approval ergonomics for owner-authorized local operations. It **does not**:
- grant paid API spending or remote source disclosure;
- bypass evidence hashes, task identity or immutable model binding;
- treat a reviewer as independent when the actors are the same;
- permit automatic merging, deleting the source repository, or pushing to remote by default;
- allow untrusted third-party projects to silently opt a user into unsafe execution.

For early usability, manual owner consent to an unconfined local execution profile is sufficient; OS sandbox development is **not** a prerequisite to SELF_HOST_ALPHA. Distinguish `self_host_alpha_unsafe` from an isolated/production-grade run in every report.

## Acceptance scenarios

1. **Real success**: on a clean local Go fixture, a real Ollama invocation receives a bounded EWP, uses a DevCadence-managed edit tool, creates a candidate commit, passes tests, gets a distinct review (or explicitly reports human-only review), and awaits manual integration.
2. **Self-host**: same path fixes one approved, bounded DevCadence issue and produces an auditable PR-ready diff.
3. **Strict refusal**: without YOLO consent and without a sandbox, unconfined verification/edit process refuses before subprocess creation.
4. **YOLO explicit**: trusted project opt-in enables subprocesses with a visible unsafe provenance marker; strict invocation override refuses again.
5. **No fabricated claims**: absent model, missing reviewer, unknown spend, failed tests, interrupted attempt or wrong base commit can never be reported as completed/independently accepted.
6. **No silent privileges**: remote model, credentials, network source upload, branch merge and GitHub push require their existing separate authority.

## Gate boundaries and postponements

- **SELF_HOST_ALPHA**: SH1-1 through SH1-4 pass in a trusted local checkout with one actual model, even with explicit unsafe mode, one host and manual merge.
- **M5_PRODUCT_COMPLETE**: the original broader M5 obligations (portability, protected human ingress, independent policies and M4 empirical re-evaluation) remain tracked, but do not gate SELF_HOST_ALPHA.
- **EMPIRICAL_VALIDATED**: R3-D corpus, R3-C runner and real multi-tier M4 re-evaluation after the alpha loop. Synthetic M4 results remain synthetic, never marketed as validated gains.
- **Future / optional before alpha**: M6 pack catalog and project adoption breadth, M7 parallel review campaigns, M8 structural refactoring analytics, M9 optimization/learning, M10 long-running autonomy; all may be revisited after the first useful loop.
- Any new requirement added to SH1 must show a concrete, reproducible failure blocking the alpha scenario; otherwise put it in post-alpha backlog.

## Execution discipline

Use small EWP batches with precise acceptance commands. Prefer functional end-to-end tests over new schemas and governance for hypothetical environments. Preserve existing security defaults and protocols, but create a bounded local unsafe profile for the developer-controlled alpha case rather than expanding the global trust model. Record escaped limitations and temporary manual steps so they can be retired later.
