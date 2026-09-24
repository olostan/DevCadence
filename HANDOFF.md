# Handoff — M3B guided bootstrap (feat/m3b-guided-bootstrap)

Last updated: 2026-09-24T05:10:00Z by Claude Code / Sonnet 5 (cloud session, olostan@gmail.com)

Session takeover HEAD: `a38b293` (origin/main HEAD when this session started — branch did not exist yet)
Expected remote HEAD before next push: `b83507c` (the actual current pushed HEAD — this field always tracks the real remote tip; advance it after every successful push — see "Git safety rules" in AGENT_HANDOFF_PROTOCOL.md).

## Where WP-M3B-3 actually stands

WP-M3B-3 is **implemented, not yet accepted**. Timeline, newest first:

1. A first independent review found 10 blocking issues in the executor — all fixed (see `docs/work-packages/wp-m3b-3-ewp.md` §15).
2. A second review found this WP's design was Ollama-specific, conflicting with `docs/MODEL_RUNTIME.md`/`INVARIANTS.md` DCI-055. The project owner explicitly authorized reopening WP-M3B-1's protocol types on this branch and required MLX-LM to be a fully equal peer to Ollama. A runtime-agnostic `LocalModelRuntimeAdapter` redesign was implemented (`ensure_local_model`/`model_present` protocol types, `ModelRuntimeRegistry`, `OllamaAdapter`/`MLXAdapter`) — see `docs/work-packages/wp-m3b-1-ewp.md` §13 and `wp-m3b-3-ewp.md` §16.
3. **A third, follow-up independent review** ([PR #10 comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5807748158), owner) reviewed that redesign itself and found the *direction* correct but 8 further material gaps, all in the redesign's own implementation quality, not in the earlier 10-finding round (which it confirmed was "largely solid"):
   1. `DefaultMLXRevision` was `"main"` — a mutable ref, defeating the immutable-plan property the redesign's own field documentation claimed.
   2. The MLX adapter didn't enforce `ExpectedSizeBytes`/`AllowedSource` the way Ollama's adapter enforces its equivalents.
   3. The MLX adapter was built against `huggingface-cli`/`--local-files-only`, which the reviewer verified (and this session independently re-verified against current Hugging Face docs) is deprecated/undocumented — `huggingface-cli` was removed in `huggingface_hub` v1.0 in favor of `hf`, and `hf download` has no documented `--local-files-only` flag.
   4. `MLXAdapter.ModelPresent` re-resolved a bare `"huggingface-cli"`/`"hf"` from ambient PATH — used by `Executor.Recover` to decide whether an interrupted mutating action actually succeeded, reintroducing the PATH-substitution class of problem on the verification side.
   5. Ollama-specific configuration (`OllamaBaseURL`) still lived on the generic `ExecutorOptions`/`EvaluatorDeps`/`applierDeps` structs rather than fully behind `OllamaAdapter`.
   6. `ensure_local_model`'s operation identity wasn't structurally bound to its `model_present` postcondition — a plan could approve pulling model A while declaring success against model B.
   7. `state_root_writable`'s mode-bit check claimed unqualified "is writable" wording it cannot actually establish.
   8. This file (`HANDOFF.md`) had stale, contradictory sections describing the pre-redesign state as current.

**All 8 are now fixed in this same session**, in the commit(s) following `5c22d36` (see "What's implemented" below for the file-by-file detail). This is the current, accurate state — do not trust the "Currently in progress" wording from any older revision of this file.

**Verified after all fixes:** `go build ./...`, `go vet ./...`, `gofmt -l internal/setup/*.go internal/protocol/setup*.go`, `go test -count=1 ./...` (all packages, including the `tests` schema-fixture round-trip), `go test -race ./internal/setup/...`, `GOOS=windows GOARCH=amd64 go build ./...` — all clean.

**Not yet done:** a fourth independent review round confirming these 8 fixes. Post a PR comment summarizing them (this session's next job) and keep watching PR #10. Do not flip WP-M3B-3 to `accepted` unilaterally — that is the reviewer's call, per the established pattern for every WP in this milestone.

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
| WP-M3B-3 | implemented, NOT accepted — awaiting review of the 8-finding follow-up fix round | current branch HEAD (see "Expected remote HEAD" above — update after pushing) | `go build ./...`, `go vet ./...`, `go test -count=1 ./...`, `go test -race ./internal/setup/...`, `GOOS=windows GOARCH=amd64 go build ./...` all PASS | first 10-finding round: addressed (EWP §15). Second review (architecture correction): resolved by owner instruction, redesign implemented (EWP §16 amendment). **Third review** ([comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5807748158)): 8 findings against the redesign's implementation quality — all fixed this session, not yet re-reviewed |
| WP-M3B-4 | not started | — | — | blocked on WP-M3B-3 acceptance |
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
- `mlx_adapter.go` — `MLXAdapter{CacheDir string}`: `EnsureModel` runs `hf
  download <ref> --revision <rev>` via the verified executable path (never
  `huggingface-cli`, which is deprecated), then verifies the result by
  reading the local Hugging Face Hub cache directly from disk (never by
  re-invoking the CLI) — checks the resolved-revision snapshot directory
  exists and, when `ExpectedSizeBytes` was approved, that the snapshot's
  measured total size matches exactly. `ModelPresent` is the same
  filesystem-only check with no `ExpectedSizeBytes` requirement — and,
  critically, **never spawns a subprocess at all**, so postcondition/
  recovery evidence cannot depend on ambient PATH resolution. `EnsureModel`
  also rejects any `AllowedSource` other than `"huggingface.co"` — the only
  source this adapter can pull from.
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

Post a PR comment on #10 summarizing the 8-finding fix round (this
session's next job) and continue watching for the owner's response. Once a
fresh review round lands and any further findings are addressed, flip
WP-M3B-3 to `accepted` and proceed to WP-M3B-4/5/6 per the dependency chain
in `docs/WORK_PACKAGES.md` (any order, not concurrently — see
`AGENT_HANDOFF_PROTOCOL.md`'s "Concurrency model"), starting each with the
same pre-check pattern against `internal/setup/{doctor,planner,profiles,
cache}.go`.

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
