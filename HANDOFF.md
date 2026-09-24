# Handoff — M3B guided bootstrap (feat/m3b-guided-bootstrap)

Last updated: 2026-09-24T09:22:00Z by Antigravity (WP-M3B-4 implementation complete)

Session takeover HEAD: `9b8c809615bc2f5c961f2cacf14e0de4110b0ad4`.
Expected remote HEAD before next push: `635fb19c0175b9ca9cba85a62f85e4ea7593c662`
(pre-push baseline for this checkpoint; after a successful push, advance the
session guard to the pushed SHA and record it at the next durable checkpoint).

## Roadmap synchronization after PR #12

New main base: `bc1816549cc4c10b4ec49add7390a58b1ada890c` (merged PR #12).
Merged into the milestone branch at the accepted WP1–WP3 boundary per
`AGENT_HANDOFF_PROTOCOL.md`; no rebase or force-push. The merge had **no
conflicts**. All implementation, tests, schemas, fixtures, accepted EWPs,
and ADR-0014 amendments remain unchanged from the takeover HEAD.

The canonical roadmap from `docs/IMPLEMENTATION_PLAN.md` and
`docs/WORK_PACKAGES.md` is now:

- **M3B: 8 WPs**, deterministic bootstrap, ResourceInventory/readiness and
  plain/JSON/basic-terminal interaction. WP1–WP3 remain accepted; WP4 is next.
- The old rich-TUI WP-M3B-8 is removed from M3B; rich adaptive setup/explain
  UX belongs to M3D. The old WP-M3B-9 closure is now **WP-M3B-8**.
- **M3C:** deterministic cognition resource/session/economic substrate,
  portfolio protocol shapes and deterministic validation/activation.
- **M3D:** AI-assisted portfolio/workflow synthesis and richer setup UX.
- **M4:** adaptive vertical-slice evidence gate before broader productization.

The accepted runtime-neutral `ensure_local_model` / `model_present`,
`LocalModelRuntimeAdapter`, closed `VersionProbeKind`, MLX/Ollama peer
adapters, and ADR-0014 supply-chain/recovery semantics are preserved.
The automatic merge retained the WP1 runtime-neutral amendment in the scope
card. One stale incoming WP5 sentence was corrected from M3C to M3D for
AI-assisted synthesis, consistent with PR #12's canonical split.
Historical EWPs are retained as accepted checkpoint evidence; their old
future milestone/WP numbering is superseded by the current scope cards.

## Post-merge verification

Verified on the merged tree (parents: takeover HEAD above and new main base),
with only Markdown changes relative to the takeover HEAD. Toolchain:
`go version go1.27.1 darwin/arm64`. All commands exited **0**:

| Command | Result |
|---------|--------|
| `go build ./...` | PASS, no diagnostics |
| `go test -count=1 ./...` | PASS, 28 tested packages; 2 packages have no test files |
| `go vet ./...` | PASS, no diagnostics |
| `go test -race -count=1 ./...` | PASS, all 28 tested packages; no race reports |
| `GOOS=windows GOARCH=amd64 go build ./...` | PASS, cross-build only; Windows execution not tested |
| `git diff --check` | PASS |

The full race suite includes setup, MLX cognition and environment packages.
These are local verification results, not GitHub CI results. WP4 has not
been started by this synchronization.

## WP-M3B-3 is accepted

Seven independent review rounds, ending in acceptance
([PR #10 comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5809019161),
owner, 2026-09-24): "WP-M3B-3 is accepted. GREEN to proceed to WP-M3B-4 (or
another allowed next WP per the documented dependency chain)." Full
disposition and the accepted checkpoint's accumulated properties are in
`docs/work-packages/wp-m3b-3-ewp.md` §21. The review history (§15–§21,
25 total findings across all rounds, all resolved):

1. First round: 10 executor findings (lock/ledger ownership, crash
   recovery, exported bypasses, executable-identity binding, supply-chain
   enforcement, diagnostic truthfulness, ledger terminality,
   home-confinement, ANSI stripping) — §15.
2. Architecture correction: this WP's design was Ollama-specific,
   conflicting with `docs/MODEL_RUNTIME.md`/`INVARIANTS.md` DCI-055. The
   project owner explicitly authorized reopening WP-M3B-1's protocol types
   on this branch and required MLX-LM to be a fully equal peer to Ollama.
   A runtime-agnostic `LocalModelRuntimeAdapter` redesign was implemented
   (`ensure_local_model`/`model_present` protocol types,
   `ModelRuntimeRegistry`, `OllamaAdapter`/`MLXAdapter`) — see
   `docs/work-packages/wp-m3b-1-ewp.md` §13 and `wp-m3b-3-ewp.md` §16.
3. Redesign-quality review: 8 implementation-quality findings — §17.
4. 3 material findings (MLX cache-dir binding, crash-recovery size
   verification, `LicenseReference` contract truthfulness) + 4 smaller — §18.
5. 2 integration blockers (`hf` never in default discovery; setup/cognition
   Hugging Face cache-location disagreement) — §19.
6. 1 security/protocol regression (an arbitrary-argv surface the round-5
   fix introduced, closed to a `VersionProbeKind` enum) + canonical-doc
   sync (ADR-0014/`docs/WORK_PACKAGES.md`) — §20.
7. **Final review: accepted, no remaining blocker** — §21.

**Verified at acceptance:** `go build ./...`, `go vet ./...`, `gofmt -l
internal/setup/*.go internal/protocol/setup*.go internal/environment/*.go
internal/cognition/mlx/*.go`, `go test -count=1 ./...` (all packages,
including the `tests` schema-fixture round-trip and the invalid-fixture
regression), `go test -race ./internal/setup/... ./internal/cognition/mlx/...
./internal/environment/...`, `GOOS=windows GOARCH=amd64 go build ./...` —
all clean. No GitHub-attached CI exists on this repository (confirmed
across every review round) — this evidence is session-reported, as it has
been for the whole milestone.

PR #10 itself stays **draft/open** — it tracks the whole M3B milestone, and
WP-M3B-4 through WP-M3B-8 remain unaccepted; see the table below for
pre-existing WP5/6 code that still requires assessment.

## Milestone

M3B — Guided deterministic bootstrap and onboarding (8 WPs). See
[docs/WORK_PACKAGES.md#m3b](docs/WORK_PACKAGES.md#m3b--guided-deterministic-bootstrap-and-onboarding)
for the full Work Package breakdown, and `AGENT_HANDOFF_PROTOCOL.md` for the
branch/commit/handoff discipline.

**Read `docs/work-packages/wp-m3b-1-ewp.md` in full before doing anything
else in this milestone — especially its "Provenance" section and its §13
amendment.** It is not optional background; it explains a load-bearing fact
about this branch's starting state and why WP-M3B-1's protocol types were
amended rather than treated as frozen.

## IMPORTANT — read this before touching internal/setup or internal/protocol/setup.go

The original M3B base at `a38b293` contains a substantial, tested
implementation of `internal/protocol/setup.go`/`internal/setup/{doctor,
planner,profiles,cache}.go` that predates this protocol and the M3B
work-package breakdown — see `docs/work-packages/wp-m3b-1-ewp.md`'s
Provenance section for the full story. WP-M3B-1's types have since been
substantially amended (the runtime-agnostic redesign, see above); the
`planner.go` executor-adjacent scaffolding (`doctor.go`, `profiles.go`,
`cache.go`) has **not** been assessed for WP-M3B-5/6's own scope — still
open, same as before.

**Before starting WP-M3B-5 or WP-M3B-6 (or any later WP), the next session
must first check whether `internal/setup/{doctor,planner,profiles,cache}.go`
already substantially satisfies its scope card**, the same way every prior
session in this milestone did — rather than assuming a blank slate and
writing duplicate/conflicting code. If it does, write the EWP describing
what's actually there, verify it fresh against that WP's own acceptance
criteria, and accept it — or, if there's a real design gap, use the
escalate/amend path in `AGENT_HANDOFF_PROTOCOL.md` rather than silently
implementing on top of it or rewriting it.

## Work Package status

| WP | Status | Checkpoint | Validation | Review |
|----|--------|------------|------------|--------|
| WP-M3B-1 | accepted (amended §13) | see head of `internal/protocol/setup.go` history | `go build ./...`, `go vet ./...`, `go test -count=1 ./...` all PASS | independent review complete — [comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5805285148), findings addressed in EWP §12; §13 amendment (runtime-agnostic types) not yet independently re-reviewed on its own, but covered by the WP-M3B-3 review below since the two ship together |
| WP-M3B-2 | accepted | `bb01bc9` | all PASS (see EWP §13) | independent review complete — [comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5805603916), 7 findings, all addressed in EWP §14 |
| WP-M3B-3 | **accepted** (§21) | `cc799cd` | `go build ./...`, `go vet ./...`, `go test -count=1 ./...`, `go test -race ./internal/setup/... ./internal/cognition/mlx/... ./internal/environment/...`, `GOOS=windows GOARCH=amd64 go build ./...` all PASS | 7 independent review rounds, 25 findings total, all resolved — see EWP §15–§21. Final acceptance: [comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5809019161) |
| WP-M3B-4 | complete (ready for review) | `0afedc2` | `go test ./...`, `go test -race ./...`, windows build all PASS | EWP, threat model, schemas, implementation, and 16 test suites complete; awaiting independent review |
| WP-M3B-5 | unknown — likely partially pre-existing, unverified; scope rebaselined by PR #11 | — | — | assess `internal/setup/doctor.go`, `profiles.go` against deterministic ResourceInventory/readiness scope first |
| WP-M3B-6 | unknown — likely partially pre-existing, unverified | — | — | assess `internal/setup/planner.go`, `cache.go` first |
| WP-M3B-7 | not started | — | — | blocked on WP-3/4/5/6 |
| WP-M3B-8 | not started — verification suite and docs sync (formerly WP9) | — | — | blocked on all prior |

(Never write "merged" for a WP checkpoint — nothing is merged to `main`
until the whole milestone closes.)

## What's implemented (current state of `internal/setup`)

- `commandrunner.go` — the `CommandRunner` interface ADR-0014 §7 requires;
  `*process.Runner` satisfies it directly.
- `conditions.go` — `EvaluateCondition`: `command_available`,
  `executable_verified`, `managed_dir_exists`, `port_listening`,
  `endpoint_healthy` (injectable, fails closed with none configured), and
  `model_present` — dispatched through `ModelRuntimeRegistry.For(runtime)`,
  no runtime name appears in this file at all.
- `operations.go` — `applyOperation`, dispatching all `TypedOperation`
  kinds: `create_directory`, `write_managed_config`, `remove_stale_cache`,
  `run_diagnostic_check` (4 checks — `state_root_writable`'s result wording
  now honestly describes a mode-bit best-effort signal, not unqualified
  writability), `ensure_local_model` — dispatched through
  `ModelRuntimeRegistry.For(runtime)`, no runtime name appears in this file
  either.
- `modelruntime.go` — `LocalModelRuntimeAdapter` interface,
  `ModelRuntimeRegistry` (panics on a duplicate `Runtime()` at construction
  — a misconfiguration, not a runtime condition), `DefaultModelRuntimeAdapters()`
  = `{OllamaAdapter{}, MLXAdapter{}}`.
- `ollama_adapter.go` — `OllamaAdapter{BaseURL string}`: owns its own
  Ollama API base URL configuration (moved off `ExecutorOptions`/
  `EvaluatorDeps`/`applierDeps` — those structs no longer know Ollama
  exists). `EnsureModel`/`ModelPresent` verify exact digest + size against
  the live `/api/tags` response, same as before.
- `mlx_adapter.go` — `MLXAdapter{CacheDir string}`: `EnsureModel` resolves
  the cache directory through the shared Hugging Face resolver (`CacheDir`/
  `HF_HUB_CACHE`/`HF_HOME`/`XDG_CACHE_HOME`/default)
  and runs `hf download <ref> --revision <rev> --cache-dir <that exact
  dir>` via the verified executable path (never `huggingface-cli`, which
  is deprecated) — the explicit `--cache-dir` binds the download to the
  same path verification reads, since `process.BaseEnv()` deliberately
  doesn't pass `HF_HOME`/`HF_HUB_CACHE` to the subprocess. Verification
  reads the local Hugging Face Hub cache directly from disk (never by
  re-invoking the CLI): the resolved-revision snapshot directory must
  exist and, when `ExpectedSizeBytes` was approved (now required by
  `ModelPresentOperand.ExpectedSizeBytes`, structurally bound to match the
  operation's own value — see below), the snapshot's measured total size
  must match exactly, in both `EnsureModel`'s post-download check and
  `ModelPresent` itself (so a crash-recovery postcondition check can tell
  a complete download from a partial one, not just "some directory
  exists"). `ModelPresent` **never spawns a subprocess at all**, so
  postcondition/recovery evidence cannot depend on ambient PATH
  resolution. `EnsureModel` also rejects any `AllowedSource` other than
  `"huggingface.co"` — the only source this adapter can pull from.
  `LicenseReference` is documented (protocol-level) as approval/provenance
  metadata only — no adapter verifies it against fetched metadata.
- `executor.go` — unchanged in shape from the prior round (lock+ledger
  lifecycle owned by `Executor`, `Recover` for crash recovery,
  `verifiedExecutablePath` threading); `ExecutorOptions`/`Executor` no
  longer carry `OllamaBaseURL` — `ModelRuntimes *ModelRuntimeRegistry` is
  the only model-runtime knob, and adapter-specific config (e.g.
  `OllamaAdapter{BaseURL: ...}`, `MLXAdapter{CacheDir: ...}`) is built into
  the adapter instances passed to it.
- `planner.go` — `DefaultMLXRevision` is now a real, immutable Hugging Face
  commit hash (`019cc73c45c770444708a6dd8690c66243cc5c80`) and
  `DefaultMLXSizeBytes` a real measured byte count (`4295890004`), both
  resolved from the live Hugging Face Hub API against
  `DefaultMLXModelRef` (see the constant's doc comment for the exact API
  calls used — not fabricated). `isImmutableHFRevision` rejects mutable
  refs; `localModelRecipe` gained a `revisionIsImmutable` gate so the
  automated `ensure_local_model` path can never be built from a mutable
  ref, for MLX or any future Hugging-Face-backed runtime — Ollama's
  `revisionIsImmutable` stays `nil` because its own sha256-digest format is
  already inherently immutable.
- `internal/protocol/setup.go` — `SetupAction.Validate()` now structurally
  requires an `ensure_local_model` action's `model_present` postcondition
  to carry the identical `(runtime, model_ref, resolved_revision)` — a plan
  can no longer approve pulling model A while declaring success against
  model B.

## Currently in progress: WP-M3B-4 — Credential-reference abstraction (complete, ready for review)

- **EWP status:** expanded and committed at `docs/work-packages/wp-m3b-4-ewp.md` (updated §12 with verification summary).
- **Base commit this WP started from:** `9b8c809615bc2f5c961f2cacf14e0de4110b0ad4`
- **What's implemented:**
  1. Protocol types in `internal/protocol/credentials.go` (`CredentialRefKind`, `CredentialRef`, `AuthEvidenceStatus`, `AuthProbeKind`, `AuthEvidence`, `LooksLikeSecret`). Provider decoupled from `CredentialRef` locator model per architectural invariant `CredentialRef != CognitionEndpoint != AccessChannel != Account`.
  2. JSON Schemas: `schemas/credential-ref.schema.json` and `schemas/auth-evidence.schema.json` (Draft 2020-12, registered in `internal/schema/schema.go`).
  3. `internal/credentials` service package: `EnvReader`/`OsEnvReader` (presence-only, zero value retention), `CLISessionAuthAdapter` registry, `VersionOnlyAdapter` (version output alone proves installation only, never authentication), `BoundedCLIAuthAdapter` (safe bounded auth status probes; raw stdout/stderr discarded immediately, zero raw artifacts), `KeychainChecker`/`UnsupportedKeychainChecker` (pure Go, cross-platform), `ValidateProcessSpecNoSecrets` (guards process args and env against secret injection), and `Manager`.
  4. Deduplicated secret detection across `internal/cognition/service.go` and `internal/cognition/remoteapi/remoteapi.go` by delegating to `protocol.LooksLikeSecret`.
  5. Canonical documentation updated in `docs/PROTOCOLS.md` (§20) and `docs/ARCHITECTURE.md` (§6.7D).
- **What's verified:**
  - `go build ./...`, `go vet ./...`, `go test -count=1 ./...` (all 29 packages PASS).
  - `go test -race ./internal/credentials/... ./internal/protocol/... ./internal/setup/...` (PASS, no races).
  - `GOOS=windows GOARCH=amd64 go build ./...` and `GOOS=linux GOARCH=amd64 go build ./...` (clean cross-compilation).
  - All 16 required test scenarios covered in `internal/protocol/credentials_test.go` and `internal/credentials/credentials_test.go`.
- **Known blockers / open questions:** None.

## Next concrete action

Perform independent review of WP-M3B-4 checkpoint per `AGENT_HANDOFF_PROTOCOL.md` and `docs/REVIEW_AND_CONVERGENCE.md`. Once accepted, proceed to WP-M3B-5.

## Resume checklist for the next agent

1. `git fetch origin feat/m3b-guided-bootstrap` and check out the branch.
   Record the fetched `HEAD` SHA as your own session's "Expected remote
   HEAD" baseline — adopt whatever SHA the fetch actually returns rather
   than assuming it matches this file's "Expected remote HEAD" field
   above; if it doesn't match, reconcile per the protocol's git safety
   rules before doing anything else.
2. Do not trust this file blindly: run `go build ./... && go test -count=1
   ./...` and confirm it matches what's claimed above.
3. Read `docs/work-packages/wp-m3b-1-ewp.md`'s "Provenance" section and §13
   amendment, and `wp-m3b-2-ewp.md`/`wp-m3b-3-ewp.md` in full — they
   explain why every later WP needs the same "check what's already there"
   pre-check, and WP-M3B-2's §12 is a worked example of escalating rather
   than reopening a frozen WP's contract if the next WP hits a similar gap.
4. Read `docs/WORK_PACKAGES.md`'s entry for whichever WP you're picking up
   next and its relevant ADR-0014 section, and the relevant pre-existing
   `internal/setup/*.go` file(s) for the pre-check.
5. Continue from "Next concrete action" above.
6. At the next durable checkpoint, update this file (including advancing
   "Expected remote HEAD" to the SHA an actual `git log`/`git fetch`
   returns, never a value guessed before pushing) and push it together
   with that checkpoint's commit.
