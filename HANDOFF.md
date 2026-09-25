# Handoff — M3B guided bootstrap (feat/m3b-guided-bootstrap)

Last updated: 2026-09-25T04:10:00Z by Claude Code / Sonnet 5 (cloud session, olostan@gmail.com) — syncing after a concurrent session's WP-M3B-5 commits (`5ef449c`, `e7d13f8`) landed fast-forward on top of this session's own WP-M3B-5 implementation (`a26dc5e`)

Session takeover HEAD: `962e67d1a20bb5f5b01f89677fa642d71ceeb614`.
Expected remote HEAD before next push: whatever `git fetch origin feat/m3b-guided-bootstrap` reports as the branch tip — per the follow-up review's bookkeeping note, this field is no longer chased with a dedicated SHA-only commit after every push; the next agent should simply verify the fetched HEAD itself rather than trust a stale value here. As of this revision (the WP-M3B-4 third-round fix, §15), the pushed commit contains the 2 additional independent-review findings fixed on top of `142ff29` (which itself followed `82a0ba4`).

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
substantially amended (the runtime-agnostic redesign, see above).
`doctor.go`, `profiles.go`, and `planner.go` have now been assessed for
WP-M3B-5's scope (`docs/work-packages/wp-m3b-5-ewp.md` §0/§0a) — all three
already substantially satisfy it; only the gaps that EWP identifies remain.
`cache.go` and the fuller WP-M3B-6 "Bounded recipes" scope card are
**still unassessed**.

**Before starting WP-M3B-6 (or any later WP), the next session must first
check whether `internal/setup/{planner,cache}.go` already substantially
satisfies its scope card** — `planner.go` in particular already resolves
immutable digests and license metadata for its model-pull recipes, which
overlaps WP-M3B-6's own acceptance criteria (see the WP-M3B-5 EWP's pre-check
for what was already read). Same discipline as every prior WP in this
milestone: don't assume a blank slate. If it does, write the EWP describing
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
| WP-M3B-4 | **accepted** (§16) | `75e65a7` | `go build ./...`, `go vet ./...`, `go test -count=1 ./...`, `go test -race ./internal/credentials/... ./internal/protocol/... ./internal/cognition/...`, `GOOS=windows GOARCH=amd64 go build ./...` all PASS | 3 independent review rounds, 6+3+2 findings total, all resolved — see EWP §13–§16. Final acceptance: [comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5819033678) |
| WP-M3B-6 | **ACCEPTED** at `62f5254` — round 1 (4 FIX_NOW findings, EWP §10) and round 2 (4 required repairs, EWP §11) fixed; round 3 confirmed GREEN | `62f5254` | `go build ./...`, `go vet ./...`, `go test -count=1 ./...`, `go test -race ./internal/setup/... ./internal/protocol/... ./internal/credentials/... ./internal/schema/... ./internal/environment/... ./tests/...`, `GOOS=windows/linux GOARCH=amd64 go build ./...` all PASS; `gofmt -l` clean on every file the round-2 repair touched (the broad `internal/setup/ internal/protocol/` check reports pre-existing, unrelated `ledger.go`) | round 1 — [comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5834040833), fixed EWP §10. round 2 — [comment id `5836314553`](https://github.com/olostan/DevCadence/pull/10#issuecomment-5836314553), fixed EWP §11. round 3 (acceptance) — [comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5836690733), "GREEN for WP-M3B-6... accepted at this checkpoint" |
| WP-M3B-7 | implemented, NOT accepted — round 1 (4 FIX_NOW findings, EWP §8) fixed, awaiting round-2 review | current branch HEAD (see "Expected remote HEAD" above) | `go build ./...`, `go vet ./...`, `gofmt -l` clean, `go test -count=1 ./...`, `go test -race ./cmd/devcadence/... ./internal/setup/... ./internal/protocol/... ./internal/credentials/... ./internal/schema/... ./internal/environment/... ./tests/...`, `GOOS=windows/linux GOARCH=amd64`/`GOOS=darwin GOARCH=arm64 go build ./...` all PASS | round 1 — [comment id `5837092761`](https://github.com/olostan/DevCadence/pull/10#issuecomment-5837092761), exit code 5 unreachable for invalid plans; `setup recover --json` ungoverned ad hoc map; `--scope`/`--target`/positional-target input silently ignored or ambiguous; acceptance evidence overclaimed exact matrix coverage — all fixed, EWP §8, not yet re-reviewed |
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

## WP-M3B-4 — Credential-reference abstraction (accepted §16)

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
  - All 16 original test scenarios plus new regressions covered in `internal/protocol/credentials_test.go` and `internal/credentials/credentials_test.go` — see EWP §13 for the full list added by the fix round.
- **First independent review** ([comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5811641514), owner, 2026-09-24) found 6 substantive blockers, all fixed this session:
  1. presence-only evidence (env/keychain) was promoted to `authenticated` — now `indeterminate`, structurally forbidden by `AuthEvidence.Validate()`; the related `UnsupportedKeychainChecker` bug (unsupported backend read as "absent") is also fixed (now `unavailable`);
  2. the secret-in-process-spec guard was an unenforced opt-in helper — both CLI adapters now route through a shared `runGuarded` chokepoint;
  3. `AuthEvidence.Validate()` had secret/structural validation gaps on `RefID`/`AdapterID` and no `Kind`↔`ProbeKind` binding — both closed;
  4. Go/JSON-Schema parity was false (keychain `..`, secret-prefix/keyword checks, length ceilings) — unified via a shared `$defs/noSecretLike` schema fragment and a parity regression test;
  5. `CredentialRef`/`AuthEvidence` claimed to be durable Records but didn't actually satisfy the `Record` interface or have `NewRecord`/fixture wiring — completed as real Records (also caught: `schema.AllNames()` was missing both schema names entirely);
  6. `BoundedCLIAuthAdapter` treated every unrecognized nonzero exit as `unauthenticated` — now `indeterminate` unless a provider-specific message matched.
  Full detail and every new test in `docs/work-packages/wp-m3b-4-ewp.md` §13.
- **Follow-up independent review** ([comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5817088046), owner, 2026-09-24) confirmed all 6 findings above were substantively resolved and did not reopen them, but found 3 more blockers, all fixed this session:
  1. real `process.Runner` execution was never demonstrated and the adapter spec (no `Env`) couldn't resolve a bare CLI name against the real runner — `VersionOnlyAdapter`/`BoundedCLIAuthAdapter`'s single `Executable` field is now split into `Handle` (opaque logical identity, matched against `CredentialRef.locator`) and `ExecutablePath` (what actually runs), plus an `Env` field defaulting to `process.BaseEnv()`; new integration test `TestAdaptersExecuteViaRealProcessRunner` uses a real `process.NewRunner()` and a temp-dir fake executable;
  2. `protocol.LooksLikeSecret`'s unconditional ">128 bytes" rule disagreed with `ProbeTarget`'s 256/`Detail`'s 512 declared limits, and was unsafe when reused for process argv/env (a long `PATH` would be treated as a credential) — the length rule is removed from `LooksLikeSecret` entirely (now prefix/keyword-only, matching the schemas exactly); this also uncovered `internal/cognition` and `internal/cognition/remoteapi` silently depending on that same bug for their own `CredentialRef`/`AccountRef` fields, both now given their own explicit 128-byte bound alongside `protocol.LooksLikeSecret`;
  3. `BoundedCLIAuthAdapter` could be configured with version/help-shaped `ProbeArgs` (e.g. `--version`) and still report `authenticated` on exit 0 — `ProbeAuth` now reclassifies such a shape to `probe_kind: cli_version_only` and caps success at `indeterminate`, enforced at evaluation time regardless of construction.
  Full detail and every new test in `docs/work-packages/wp-m3b-4-ewp.md` §14.
- **Third independent review** ([comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5818221753), owner, 2026-09-24) accepted §14 findings 1 and 2 as resolved and did not reopen them, but found 2 more blockers, both fixed this session:
  1. `BoundedCLIAuthAdapter` still lacked *positive* structural proof its command is authoritative — §14's negative version/help blacklist left every other successful command implicitly authoritative (e.g. `{"config","show"}`, `{"status"}`). Fixed by replacing `ProbeArgs []string` with `Probe AuthProbeDefinition`, a closed type whose only field is unexported and producible only by `NewAuthProbeDefinition`/`MustAuthProbeDefinition` (which refuse empty/version-help argv at construction); a bare struct literal leaves `Probe` at its zero value, which reports `unavailable` and never runs a command. §14's runtime `versionOrHelpOnlyArgs` reclassification is superseded (moved into the constructor) and its test removed in favor of `TestNewAuthProbeDefinitionRejectsVersionAndHelpShapes`/`TestBoundedCLIAuthAdapterWithoutDeclaredProbeCannotAuthenticate`.
  2. Go/JSON-Schema length parity diverged for Unicode text: `len(string)` counts UTF-8 bytes, `maxLength` counts Unicode characters — `AuthEvidence.ProbeTarget`/`Detail` now use `utf8.RuneCountInString` instead of `len()`.
  Full detail and every new test in `docs/work-packages/wp-m3b-4-ewp.md` §15.
- **Acceptance:** [comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5819033678), owner, 2026-09-24, at head `75e65a7`. Both §15 findings accepted; a regression sweep over every finding closed across §13/§14/§15 found nothing reopened. **"WP-M3B-4 is accepted. GREEN to proceed to WP-M3B-5."** PR #10 itself remains open/draft for the rest of M3B. EWP §16 records the acceptance.

## WP-M3B-5 — Doctor readiness and ResourceInventory (accepted)

`docs/work-packages/wp-m3b-5-ewp.md` is authored, committed, and fully implemented.

**What's implemented:**
1. **Scope-specific readiness model (`internal/protocol/resource_inventory.go`):**
   - Defined `ScopeKind` (`can_execute_setup_plan`, `has_any_viable_cognition_path`, `can_run_local_inference`, `local_model_available`, `can_use_existing_authenticated_cli`, `principal_host_available`) and `ScopeReadinessStatus` (`ready`, `not_ready`, `unknown`, `unavailable`).
   - Implemented `ScopeReadiness` with deterministic evaluation in `EvaluateScopeReadiness(findings, endpoints, hosts)`.
   - Added `Readiness []ScopeReadiness` to `ResourceInventory` with uniqueness and valid scope validation.
2. **ResourceInventory protocol record & projection (`internal/protocol/resource_inventory.go`, `internal/setup/doctor.go`):**
   - Implemented `ResourceInventory` as a durable protocol `Record` (`schema_version: "1.0"`) capturing hardware summary, local runtimes, installed models, verified readiness, cognition endpoints, credential references, policy summary, principal hosts, and scope readiness.
   - Pure projection `Doctor.BuildResourceInventory` derives `ResourceInventory` deterministically from discovered environment facts and verified endpoint/credential observations.
   - Registered `ResourceInventory` in `internal/schema/schema.go` (`RecordKindToSchema` and `AllNames`).
   - JSON Schema updated at `schemas/resource-inventory.schema.json` with `$defs/scope_readiness` and `readiness` array; fixture updated at `fixtures/protocol/resource-inventory.valid.json`.
3. **Doctor report integration & profile de-authorization (`internal/protocol/doctor.go`, `internal/setup/doctor.go`):**
   - Added `ScopeReadiness []ScopeReadiness` and `ResourceInventory *ResourceInventory` to `DoctorReport` and `schemas/doctor-report.schema.json`.
   - De-authorized static deployment-profile recommendation from canonical readiness and routing authority (retained purely for backward-compatible informational diagnostics and human-readable UX labels).
   - Canonical `READY` evaluated in `Doctor.evaluateReadiness`: requires base dependencies and at least one viable ready cognition path; does not require local models if authenticated CLI exists, and does not require matching `SelectedProfile == TargetProfile`.
4. **Comprehensive 20-Scenario Matrix (`internal/setup/matrix_test.go`):**
   - Scenarios 1–20 covering blank machines, CPU-only, unconfigured runtimes, verified models, authenticated CLIs, unknown auth, unverified env credentials, unsupported keychains, multi-endpoints, zero endpoints, principal hosts, unaccelerated runtimes, stale evidence, capability isolation, deterministic serialization, zero secret leakage, Go/schema parity, Windows compatibility, order independence, and static-profile de-authorization.

**Verified:**
- `go build ./...`: PASS, no diagnostics.
- `go vet ./...`: PASS, no diagnostics.
- `gofmt -l`: clean across all modified files.
- `go test -count=1 ./...`: PASS across all packages.
- `go test -race ./internal/setup/... ./internal/protocol/... ./internal/credentials/... ./internal/schema/... ./tests/...`: PASS, zero race conditions.
- `GOOS=windows GOARCH=amd64 go build ./...`: PASS, clean cross-compilation.
- `git diff --check`: PASS.

**First independent review** ([comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5826591074), owner, 2026-09-25) found 6 substantive blockers, all fixed this session:
1. static deployment profiles were still authoritative in the real `Doctor.Run`/`Planner.Plan` path (the de-authorization the EWP claimed was done had not actually removed the promotion line, nor `Planner`'s mirror of it, nor `DoctorReport.Validate()`'s READY-requires-TargetProfile rule) — all three removed;
2. `ResourceInventory` was too lossy (no link back to `MachineCapabilityProfile`) — new `MachineProfileRef` (profile_id/fingerprint/observed_at/probe_depth) added as `ResourceInventory.Profile`;
3. `BuildResourceInventory` re-probed live state internally (TOCTOU risk) instead of being a pure projection, and stored collections unsorted — now takes `Run`'s already-computed data as parameters and sorts every collection;
4. Go/JSON-Schema parity gaps: a forked local credential_ref/auth_evidence schema copy missing WP-M3B-4's structural rules (now a cross-schema `$ref` onto the canonical schemas), weak Go endpoint validation (now a single canonical `CognitionEndpointSummary.Validate()` reused everywhere), missing accelerator-backend enum check, and `doctor-report.schema.json`'s `resource_inventory` being untyped (now `$ref`'d, plus a report/inventory consistency check);
5. `has_any_viable_cognition_path`/`can_use_existing_authenticated_cli` were auth-blind (health-only) — new shared `protocol.EndpointViable` predicate used by both scope readiness and monolithic readiness;
6. the reauthenticate action's postcondition used `endpoint_healthy`, which doesn't prove authentication — new `endpoint_authenticated` condition kind with its own `EndpointAuthChecker` interface, mirroring the existing `EndpointHealthChecker` pattern.
Full detail and every new test in `docs/work-packages/wp-m3b-5-ewp.md` §14.

**Second independent review** ([comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5827367817), owner, 2026-09-25, at head `7bb3053`) confirmed round 1's six findings genuinely resolved, then found 4 further blockers, fixed in commit `0b2dbe2` (a concurrent session's push, verified and documented in EWP §15 by this session since it landed without its own EWP/HANDOFF/PR-comment update):
1. `MachineProfileRef` was not actually retrievable — `CacheManager` stored one mutable slot keyed by fingerprint, not `ProfileID`, so a later Doctor run silently orphaned an earlier inventory's profile reference — new immutable `profiles/<profile_id>.json` cache keyed by content-derived `ProfileID`, plus a Go+schema rule requiring `Profile` whenever `CognitionEndpoints` is non-empty;
2. auth-aware viability wasn't propagated into `discoverEndpoints`'s diagnostics (a ready+expired-auth endpoint was still reported `ENDPOINT_READY`), and `EndpointViable` had a `locality == local` auth bypass independent of `kind` — both fixed, plus new structural Kind/Locality validation rules preventing a non-local-runtime endpoint from ever claiming local-runtime shape;
3. remaining Go/schema parity holes — duplicate readiness scopes (closed via per-scope `contains`/`maxContains: 1`), remote verified acceleration (schema now matches Go's rejection), DoctorReport's duplicate endpoint/host copies vs. its embedded ResourceInventory (now cross-checked for equality), and same-ID-different-content duplicates (now rejected in Go for endpoints/hosts/credentials, with the residual schema-only-catches-byte-identical-duplicates gap explicitly documented in-schema per the review's own suggested option, added by this session);
4. `endpoint_authenticated` had no production verification path (`NewExecutor` defaulted `EndpointAuth` to `nil`) — new `CredentialsEndpointAuthChecker` delegates to the accepted WP-M3B-4 `credentials.Manager`, wired as `NewExecutor`'s default.
Also addressed: `BuildResourceInventory`'s "pure projection" doc comment wasn't literally true (it still called `CredentialManager.CheckCredential`, live IO) — split into `Doctor.ObserveCredentials` (the IO phase) and package-level `ProjectResourceInventory` (the actual pure projection), with `BuildResourceInventory` kept as a thin wrapper.
Full detail in `docs/work-packages/wp-m3b-5-ewp.md` §15.

**Third independent review** ([comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5829172626), owner, 2026-09-25, at head `0b2dbe2`) confirmed round 2's fixes were mostly genuine, then found 3 further blockers, fixed this session:
1. exact-profile recoverability still wasn't structural — `discoverEndpoints` silently discarded profile-archive write errors (1a), the stale-inference-retained path skipped archiving the active profile entirely (1b), and `WriteProfileWithExpiresAt` unconditionally overwrote an existing ProfileID's content (1c) — fixed by splitting `cache.go`'s writer into an unexported `archiveProfile` (immutable-only, idempotent-if-identical/conflict-if-different) called unconditionally from `discoverEndpoints` with its error now fatal, decoupled from the separate "never refresh stale inference's latest-cache TTL" policy; `ReadProfileByID` also now cross-checks loaded content's own ProfileID against the requested key;
2. production `endpoint_authenticated` collapsed `CognitionEndpoint` identity into `CredentialRef` identity — `CredentialsEndpointAuthChecker` fabricated a `cli_session` `CredentialRef` from the endpoint ID string itself, which is wrong for remote APIs, CLIs whose locator differs from their endpoint ID, and any non-CLI credential kind — fixed with an explicit binding: `EndpointOperand.CredentialRefID`, `CognitionEndpointSummary.CredentialRef` (propagated from discovery), the planner populating it on reauth actions, and the checker resolving it against an explicit configured-`CredentialRef` index (fails closed on empty/unresolved, never guesses); the misleading Runner-only `NewExecutor` fallback (a `credentials.Manager` with zero adapters) is removed;
3. remaining schema/Go parity gaps — added regressions proving the same-ID-different-content Go-only gap is real and intentional (3a), mirrored two Go-only endpoint Kind/Locality/Auth rules into both schemas (3b), and added `Version` to `principalHostsEqual` (3c, previously ignored so a report and its embedded inventory could disagree on a host's version and still validate).
Full detail in `docs/work-packages/wp-m3b-5-ewp.md` §16.

**Fourth independent review** ([comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5830254816), owner, 2026-09-25, at head `b36078e`) confirmed round 3's fixes were genuine, then found 3 further integration issues, fixed this session:
1. the explicit credential binding from round 3 was never actually populated on the normal Doctor → Planner path — the coding-CLI adapter never invents a `CredentialRef` and nothing fed one in, so a real expired CLI endpoint still produced a `credential_ref_id: ""` condition both Go and schema accepted as valid but permanently unverifiable — fixed with new `DoctorOptions.EndpointCredentialRefs` (explicit operator-configured endpoint→RefID binding, applied when Doctor builds summaries), `Condition.Validate()`/schema now requiring non-empty `credential_ref_id`, and Planner skipping the reauth action entirely (same asymmetry as `NoCodingEndpoint`) when no binding is known; new end-to-end test exercises the real Doctor→Planner→Executor chain with an endpoint ID deliberately different from its credential locator;
2. adding `CredentialRef` to `CognitionEndpointSummary` broke the shared `ProjectState` schema twin (three hand-copied endpoint definitions, only two updated) — fixed structurally: `doctor-report.schema.json` and `project-state.schema.json` now both cross-file `$ref` the one canonical `cognition_endpoint_summary` definition in `resource-inventory.schema.json`, eliminating the drift risk rather than copying a fourth time;
3. the "immutable by ProfileID" archive still fell through to overwriting an existing entry it couldn't prove was identical (malformed JSON, invalid content, or a mismatched decoded ProfileID) — fixed: every unprovable case now fails closed with `CategoryConflict` instead of proceeding to write; new `ReadProfileByRef` also cross-checks the full `MachineProfileRef` (fingerprint/observed_at/probe_depth), not just ProfileID.
Full detail in `docs/work-packages/wp-m3b-5-ewp.md` §17.

**Fifth independent review** ([comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5830908798), owner, 2026-09-25, at head `b218af5`) confirmed round 4's fixes were substantially correct, then found 2 narrower integration/integrity gaps, fixed this session:
1. `EndpointCredentialRefs` bindings and adapter-declared `CognitionEndpoint.CredentialRef` values were syntactically valid but not referentially validated against the actually-configured `CredentialRefs` — a typo'd binding still produced a syntactically valid but unresolvable `endpoint_authenticated` condition — fixed: `NewDoctor` now structurally validates every configured `CredentialRef` and rejects any `EndpointCredentialRefs` value absent from that set at construction time; an unrecognized adapter-declared ref is left out of the summary rather than trusted; `EndpointOperand.CredentialRefID` now gets the full bounded opaque-ID/secret-safe contract (Go + schema, reusing the canonical `noSecretLike` schema `$def`); `ResourceInventory.Validate()` now enforces every endpoint `CredentialRef` resolves to a `Credentials` entry when credentials are present;
2. the immutable profile archive could still lose its guarantee via two paths — `ReadProfileByID` deleted a corrupt archive entry as a side effect of reading it (letting a later write recreate the same ProfileID with different bytes), and `archiveProfile` validated only the existing entry's `Data`, not the whole envelope, so a matching `Data` under an invalid envelope (bad schema_version, or a fingerprint mismatch between envelope and Data) reported idempotent success even though a read would then reject that same file — fixed: reads are now fully non-mutating, and `archiveProfile` validates the complete existing envelope before ever declaring a match idempotent.
Full detail in `docs/work-packages/wp-m3b-5-ewp.md` §18.

**Sixth independent review** ([comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5831801920), owner, 2026-09-25, at head `049768d`) confirmed round 5's two fixes were correct, then found one narrow remaining gap, fixed this session: duplicate configured `CredentialRef.RefID` values were still accepted — `NewDoctor` built a bool-set index indifferent to duplicates, and `NewCredentialsEndpointAuthChecker` separately built a "last write wins" map — so two `CredentialRefs` entries sharing a RefID (with the no-`CredentialManager` Doctor path having no other safety net, since `ResourceInventory`'s own duplicate check never runs without one) left an `EndpointCredentialRefs` binding's actual meaning dependent on map iteration order. Fixed with one shared `buildCredentialRefIndex` helper (`internal/setup/conditions.go`) both `NewDoctor` and `NewCredentialsEndpointAuthChecker` now call, rejecting a duplicate RefID (identical or different content) outright; `NewCredentialsEndpointAuthChecker`'s signature changed to return an error, propagated by its only production call site (`NewExecutor`).
Full detail in `docs/work-packages/wp-m3b-5-ewp.md` §19.

**Acceptance:** [comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5832032107), owner, 2026-09-25, at head `75ac877`. The round-6 fix was confirmed correct; a regression sweep over every earlier repair (rounds 1–6) found nothing reopened. **"WP-M3B-5 is accepted. GREEN to proceed to WP-M3B-6."** PR #10 itself remains open/draft for the rest of M3B. Per the acceptance comment's own instruction, this acceptance is recorded here rather than in a separate SHA-only bookkeeping commit.

## WP-M3B-6 — Bounded recipes (ACCEPTED)

- **EWP status:** authored, frozen, and committed at `docs/work-packages/wp-m3b-6-ewp.md`.
- **Base commit this WP started from:** `a8af45608a879e4c4b7f4b8eb61a4101dc4beb5f`
- **What's implemented:**
  1. **Pre-resolution mechanism (`internal/setup/resolver.go`):**
     - Defined `ModelResolver` interface: `ResolveModel(ctx context.Context, runtime, modelRef string) (ResolvedModel, error)`.
     - Defined `ResolvedModel` struct with strict `Validate()` method enforcing non-empty runtime, modelRef, immutable revision (sha256 digest or 40-character hex commit hash for MLX/Hugging Face via `isImmutableHFRevision`), positive size in bytes, allowed source registry, and non-empty license reference.
     - Implemented `CatalogModelResolver` with thread-safe `Register` and `ResolveModel`, pre-populated with verified default models for Ollama (`DefaultOllamaModelTag`) and MLX (`DefaultMLXModelRef`).
  2. **Planner integration & fail-closed generation (`internal/setup/planner.go`):**
     - Added `ModelResolver` and `ModelRefs` to `PlannerOptions` and `Planner` (defaulting to `NewCatalogModelResolver()`).
     - Added `PlanWithContext(ctx context.Context, ...)` while preserving `Plan(...)` backward compatibility.
     - `PlanWithContext` resolves model identities prior to action construction. If a model digest cannot be resolved or is invalid, plan generation **fails closed immediately** with an error rather than planning an under-specified action.
     - When resolved, if local executable identity is untrusted, the planner falls back to a manual pull action binding the exact resolved revision and size in its `model_present` postcondition.
  3. **System / Kernel / Driver manual recipes (`internal/setup/recipes.go`, `internal/setup/planner.go`):**
     - Formalized system/kernel/driver operations as strictly `AuthorityHighImpactManual`, with `Operation == nil` and structured `ManualInstructions`.
     - Deterministic generation in `Planner.PlanWithContext` under `TargetHardware` / `TargetAll` driven by `environment.AssessBackends`:
       - NVIDIA missing driver -> `recipe.manual.install_nvidia_driver`.
       - NVIDIA `/dev/nvidiactl` permission missing -> `recipe.manual.configure_nvidia_device_permissions`.
       - AMD ROCm driver missing -> `recipe.manual.install_rocm_driver`.
       - AMD `/dev/kfd` permission missing -> `recipe.manual.configure_amdgpu_device_permissions`.
  4. **Complete bounded recipe builders (`internal/setup/recipes.go`):**
     - Concrete versioned recipe builders for all 5 WP-M3B-1 operation kinds:
       - `NewCreateDirectoryAction` (`recipe.mkdir.<loc>`)
       - `NewWriteManagedConfigAction` (`recipe.config.<key>`)
       - `NewRemoveStaleCacheAction` (`recipe.cache.remove.<target>`) — also automatically planned on `stale_inference_retained`.
       - `NewRunDiagnosticCheckAction` (`recipe.diagnostic.<check>`)
       - `NewManualGitAction` (`recipe.manual.install_git`)
       - `NewManualReauthenticateAction` (`recipe.manual.reauthenticate`)
       - Driver manual actions above.
     - All executable recipes derive authority and effects from `IntrinsicPolicy(op)` and pass full Go and schema validation.
  5. **Comprehensive 20-Scenario Verification Matrix (`internal/setup/recipes_test.go`):**
     - Full automated coverage of all 20 scenarios specified in EWP §8: pre-resolution of Ollama/MLX models, fail-closed handling for unresolvable models / empty digests / mutable MLX branches / missing licenses, fallback to manual pull on untrusted identity, automated pull on verified identity, NVIDIA and AMD driver and device node manual remediation, cache eviction and diagnostic check recipes, directory creation, config write, manual Git and reauth, IntrinsicPolicy conformance, and JSON Schema round-trip validation.

- **What's verified:**
  - `go build ./...`: PASS, no diagnostics.
  - `go vet ./...`: PASS, no diagnostics.
  - `gofmt -l internal/setup/`: clean.
  - `go test -count=1 ./...`: PASS across all packages.
  - `go test -race -count=1 ./internal/setup/... ./internal/protocol/... ./internal/credentials/... ./internal/schema/... ./tests/...`: PASS, zero race conditions.
  - `GOOS=windows GOARCH=amd64 go build ./...` and `GOOS=linux GOARCH=amd64 go build ./...`: clean cross-compilation.
  - `git diff --check`: clean.

**Independent review round 1** ([comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5834040833), owner, 2026-09-25, base `a8af456`, head `88fee78`) confirmed the recipe/resolver architecture was viable, then found 4 closure-threshold FIX_NOW findings, fixed at commit `ab69189`:
1. Ollama pre-resolution accepted any non-empty revision (including a mutable tag like `"main"`) — `ResolvedModel.Validate()` now dispatches per runtime, with a new `isImmutableOllamaRevision` (`sha256:<64 lowercase hex>`) alongside the existing MLX check, and fails closed for any runtime with no known verification rule;
2. the planner trusted `ModelResolver` output blindly — a resolver could return a valid record for a different runtime/model and the plan would silently target it — new `verifyResolvedIdentity` fails closed on any mismatch;
3. `recipe.manual.install_rocm_driver` was unreachable from the real `AssessBackends -> Planner` path (AMD assessment never distinguished "driver not bound" from "`/dev/kfd` inaccessible," and scenario 11 tested the wrong branch while claiming to cover it) — `amdCandidates` now checks `DriverInUse == "amdgpu"` first, mirroring `nvidiaCandidate`'s existing pattern;
4. the four hardware manual recipes verified only `command_available` (a vendor CLI on PATH proves neither driver binding nor device accessibility) — new closed `device_node_accessible` condition checks the actual device-node state via `os.Stat`/a real open-for-read-write probe.
Full detail in `docs/work-packages/wp-m3b-6-ewp.md` §10.

**Independent review round 2** ([comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5836314553), owner, 2026-09-25, head `ab69189`) confirmed Findings 1–3 were resolved, `managed_dir_exists` restored, and deterministic suite passed cleanly. It noted Finding 4 still permitted false success because `evaluateDeviceNodeAccessible` accepted ordinary regular files, and driver installation did not bind to observed driver state for the intended device. Fixed in round-2 repair:
1. `evaluateDeviceNodeAccessible` now checks `info.Mode()&os.ModeDevice != 0` to reject non-device files (regular files, directories, etc.).
2. Added closed typed condition `kernel_driver_bound` (`CondKindKernelDriverBound`, `protocol.KernelDriverBoundOperand{DeviceID, Driver}`) with `DeviceDriverChecker` interface and production `SysfsDriverChecker` reading `/sys/bus/pci/devices/<slot>/uevent` to confirm the required driver (`nvidia`/`nvidia_drm` or `amdgpu`) is bound to the specific hardware device.
3. Driver install actions verify both `kernel_driver_bound` and `device_node_accessible`.
4. Device permission actions retain distinct access check with `device_node_accessible` (`RequireAccessible: true`).
5. Reconciled EWP §3.2 recipe table with the final typed verification contract.
6. Added negative tests for regular files, missing devices in sysfs, unbound devices, wrong drivers, and corrected state transitions.
Full detail in `docs/work-packages/wp-m3b-6-ewp.md` §11.

**Evidence correction** (raised by the round-3 review itself, see below): the round-2 PR comment's verification list stated `gofmt -l internal/setup/ internal/protocol/` was clean. That broad two-directory check is **not** clean on this checkout — it reports `internal/protocol/ledger.go` — but `ledger.go` was already in that unformatted state at `ab69189`, is unchanged by the round-2 repair, and gofmt is clean on every file the repair actually touched (`internal/protocol/setup.go`, `internal/setup/conditions.go`, `internal/setup/conditions_test.go`, `internal/setup/executor.go`, `internal/setup/recipes.go`, `internal/setup/recipes_test.go`). The narrower, accurate claim is: gofmt is clean on the files this repair changed; the broad two-directory check was not, for a pre-existing, unrelated reason.

**Independent review round 3 — ACCEPTANCE** ([comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5836690733), owner, 2026-09-25, head `62f5254`): confirmed the remaining round-2 finding resolved and all 4 original FIX_NOW findings closed. **"Disposition: GREEN for WP-M3B-6... WP-M3B-6 can be marked accepted at this checkpoint; WP-M3B-7 may proceed under the handoff protocol."** The review also flagged the gofmt evidence-accuracy issue corrected above, and noted Windows/Linux results are cross-builds, not execution tests on those hosts. **WP-M3B-6 is accepted as of `62f5254`.**

**Known blockers / open questions:** none.

## WP-M3B-7 — CLI surface and minimal guided interaction (round 1 fixed, awaiting round-2 review)

- **EWP status:** authored, frozen, and committed at `docs/work-packages/wp-m3b-7-ewp.md`.
- **Base commit this WP started from:** `bae6d2a86f0a78ba36f36296be04106be0c7fbcc`
- **Implementation deliverables:**
  1. `cmd/devcadence/main.go`:
     - Added `ExitCoder` interface and exit code constants (0–6).
     - Mapped `CategoryConflict` to `ExitCodePreconditionDrift` (4) and `CategoryPlanGenerated` to `ExitCodePlanGenerated` (6).
     - Silenced stderr noise for `ExitCodePlanGenerated` (a plan generated is a normal expected outcome).
  2. `cmd/devcadence/run.go`:
     - Added `stdin io.Reader` and `isTerminal func() bool` to `env` struct.
     - Added `homeDir()` resolver fallback to user cache/home directories.
     - Registered `doctor` and `setup` subcommands in `commands()`.
  3. `cmd/devcadence/cmd_doctor.go`:
     - Implemented `devcadence doctor` with plain text summary, `--json` structured output, `--fix` plan generation, and schema validation against `schemas/doctor-report.schema.json` and `schemas/setup-plan.schema.json`.
     - Explicit exit codes: 0 (clean/ready), 1 (remediations needed without `--fix`), 6 (plan generated with `--fix`).
     - Refuses `inference` probe depth per ADR-0014 §2.
  4. `cmd/devcadence/cmd_setup.go`:
     - Implemented `devcadence setup plan [target]` (`--output`, `--json`, `--profile`, `--depth`, `--target`, `--no-tui`), emitting exit code 6 if actions are pending or exit code 0 if already satisfied.
     - Implemented `devcadence setup apply` (`--plan`, `--approve-plan`, `--yes`, `--json`, `--no-tui`):
       - Enforces two-step approval workflow.
       - Validates plan file existence and schema/model integrity (exit code 5 on corrupt/invalid plans, exit code 3 on missing file).
       - Enforces non-interactive fail-closed validation when `approve-plan` is missing on non-TTY.
       - Supports interactive confirmation (`[y/N]`) on TTY.
       - Verifies `--yes` authorization scope against required plan authority.
       - Handles precondition drift gracefully with exit code 4 (`CategoryConflict`).
       - Validates execution report against `schemas/setup-execution-report.schema.json`.
     - Implemented `devcadence setup recover` (`--plan`, `--json`, `--no-tui`) reconciling interrupted actions via sandboxed executor.
     - Added `reorderArgs` flag reordering helper for ergonomic CLI option ordering across subcommands.
  5. `cmd/devcadence/cli_doctor_test.go` and `cmd/devcadence/cli_setup_test.go`:
     - Comprehensive unit and end-to-end tests covering all doctor and setup workflows, schema validation, exit code mappings (0, 1, 2, 3, 4, 5, 6), interactive approval/rejection, non-interactive fail-closed checks, drift detection, and ANSI-free output.

- **Verification evidence (initial implementation):**
  - `go build ./...`: PASS, no diagnostics.
  - `go vet ./...`: PASS, no diagnostics.
  - `gofmt -l cmd/devcadence/`: PASS, all files clean.
  - `go test -count=1 ./...`: PASS across all packages.
  - `go test -race -count=1 ./cmd/devcadence/... ./internal/setup/... ./internal/protocol/... ./internal/credentials/... ./internal/schema/... ./tests/...`: PASS, zero race reports.
  - `GOOS=windows GOARCH=amd64 go build ./...` and `GOOS=linux GOARCH=amd64 go build ./...`: PASS, clean cross-compilation.
  - `git diff --check`: PASS.

**Independent review round 1** ([comment id `5837092761`](https://github.com/olostan/DevCadence/pull/10#issuecomment-5837092761), owner, 2026-09-25, head `c272f35`) found the implementation not yet green, with 4 closure-threshold FIX_NOW findings, fixed this session:
1. Exit code 5 was unreachable for invalid plan artifacts — `setup apply`/`setup recover` classified decode/validate/schema failures as exit 2 (or, for recover, skipped schema validation entirely). Fixed with a single shared `loadSetupPlanArtifact` boundary in `cmd_setup.go`: missing file stays exit 3, everything else wrong about an existing plan file (malformed JSON, failed semantic `Validate()`, unsupported `schema_version`, failed schema validation) is now uniformly exit 5.
2. `setup recover --json` emitted an unvalidated ad hoc `map[string]any`, outside the governed JSON contract, and lost each status's `action_id`. Fixed with a new versioned protocol record `protocol.SetupRecoveryReport` (+ `schemas/setup-recovery-report.schema.json`), and `Executor.Recover`'s return type changed from `[]protocol.ActionStatus` to `[]protocol.RecoveryActionResult` so the CLI has the identity it needs to build a real record.
3. `doctor --scope` was parsed and silently discarded; `doctor --target` was validated only when `--fix` was passed; `setup plan` permitted a positional target to silently conflict with `--target` and dropped extra positionals. Fixed: `--scope` now rejects any value other than `"default"` deterministically; `--target` is validated unconditionally; `setup plan` rejects more than one positional argument and a positional/`--target` conflict.
4. Acceptance evidence overclaimed exact deterministic coverage for several verification-matrix rows (readiness 0/1, `--fix` no-op 0, `--yes` success 0, missing/invalid plan 3/5, ANSI absence). Fixed by extracting `doctorReadinessExitError`/`planActionsExitError` as pure functions with direct unit tests, adding the missing CLI-level exit-code tests, and — in the process of writing exact assertions — catching and fixing a real latent bug in the pre-existing recovery-ledger CLI tests: `Ledger.Append` errors were being discarded, every append was silently failing required-field validation, and `setup recover` was reconciling zero actions in every test that exercised it, undetected because the assertions only substring-matched a generic banner.
Full detail in `docs/work-packages/wp-m3b-7-ewp.md` §8.

**Verification (round-1 revision):** `go build ./...`, `go vet ./...`, `gofmt -l` clean on every file this repair touched, `go test -count=1 ./...` (all 30 packages), `go test -race ./cmd/devcadence/... ./internal/setup/... ./internal/protocol/... ./internal/credentials/... ./internal/schema/... ./internal/environment/... ./tests/...` (clean), `GOOS=windows GOARCH=amd64`/`GOOS=linux GOARCH=amd64`/`GOOS=darwin GOARCH=arm64 go build ./...` (clean), `git diff --check` (clean).

**Known blockers / open questions:** none — awaiting round-2 review to confirm the §8 fixes.

## Next concrete action

Post a PR comment on #10 summarizing the round-1 fix and continue watching for the owner's response. Do not flip WP-M3B-7 to `accepted` unilaterally. Once accepted, proceed to WP-M3B-8 (Milestone Closure & Verification).

## Resume checklist for the next agent

1. `git fetch origin feat/m3b-guided-bootstrap` and check out the branch.
   Record the fetched `HEAD` SHA as your own session's "Expected remote
   HEAD" baseline.
2. Verify test suite with `go test -count=1 ./...`.
3. Check PR #10 comments for independent review findings on WP-M3B-7.
4. If review is GREEN/Accepted, proceed to WP-M3B-8 (Milestone Closure & Verification).
5. If review requests fixes, implement repairs per the EWP and review instructions.
