# Handoff — M3B guided bootstrap (feat/m3b-guided-bootstrap)

Last updated: 2026-09-24T06:40:00Z by Claude Code / Sonnet 5 (cloud session, olostan@gmail.com)

Session takeover HEAD: `a38b293` (origin/main HEAD when this session started — branch did not exist yet)
Expected remote HEAD before next push: `cc799cd` (the actual current pushed HEAD — advance after every successful push; see "Git safety rules" in AGENT_HANDOFF_PROTOCOL.md).

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
WP-M3B-4 through WP-M3B-9 are not started.

## Milestone

M3B — Guided bootstrap and onboarding. See
[docs/WORK_PACKAGES.md#m3b](docs/WORK_PACKAGES.md#m3b--guided-bootstrap-and-onboarding)
for the full Work Package breakdown, and `AGENT_HANDOFF_PROTOCOL.md` for the
branch/commit/handoff discipline.

**Read `docs/work-packages/wp-m3b-1-ewp.md` in full before doing anything
else in this milestone — especially its "Provenance" section and its §13
amendment.** It is not optional background; it explains a load-bearing fact
about this branch's starting state and why WP-M3B-1's protocol types were
amended rather than treated as frozen.

## IMPORTANT — read this before touching internal/setup or internal/protocol/setup.go

`main` (as of `a38b293`, this branch's base) contains a substantial, tested
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
| WP-M3B-4 | not started | — | — | unblocked — WP-M3B-3 accepted |
| WP-M3B-5 | unknown — likely partially pre-existing, unverified | — | — | assess `internal/setup/doctor.go`, `profiles.go` first |
| WP-M3B-6 | unknown — likely partially pre-existing, unverified | — | — | assess `internal/setup/planner.go`, `cache.go` first |
| WP-M3B-7 | not started | — | — | blocked on WP-3/4/5/6 |
| WP-M3B-8 | not started | — | — | blocked on WP-7 |
| WP-M3B-9 | not started | — | — | blocked on all prior |

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
  the cache directory once (`CacheDir`/`HF_HOME`/`~/.cache/huggingface/hub`)
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

## Next concrete action

WP-M3B-3 is accepted. Start WP-M3B-4, or WP-M3B-5/6 (any order, not
concurrently — see `AGENT_HANDOFF_PROTOCOL.md`'s "Concurrency model"), per
the dependency chain in `docs/WORK_PACKAGES.md`. **Before starting
WP-M3B-5 or WP-M3B-6, first check whether
`internal/setup/{doctor,planner,profiles,cache}.go` already substantially
satisfies that WP's scope card** — this has not been assessed yet by any
session (see "IMPORTANT" section above). If it does, write the EWP
describing what's there, verify it fresh, and accept it, rather than
assuming a blank slate. WP-M3B-4 (credential-reference abstraction) has no
such pre-existing-code question — read its scope card in
`docs/WORK_PACKAGES.md` and start its EWP per AGENTS.md §6.

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
