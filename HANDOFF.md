# Handoff — M3B guided bootstrap (feat/m3b-guided-bootstrap)

Last updated: 2026-09-24T10:05:00Z by Claude Code / Sonnet 5 (cloud session, olostan@gmail.com)

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
| WP-M3B-5 | implemented, NOT accepted — awaiting independent review (EWP §11) | current branch HEAD (see "Expected remote HEAD" above) | `go build ./...`, `go vet ./...`, `go test -count=1 ./...`, `go test -race ./internal/setup/... ./internal/protocol/... ./internal/credentials/... ./internal/schema/... ./tests/...`, `GOOS=windows GOARCH=amd64 go build ./...` all PASS | not yet reviewed |
| WP-M3B-6 | unknown — likely partially pre-existing, unverified; `planner.go` already resolves immutable digests + license metadata for its model-pull recipes (found while pre-checking WP-M3B-5) | — | — | assess `internal/setup/planner.go` (already read in full for WP-M3B-5, findings in `wp-m3b-5-ewp.md` §0) and `cache.go` against the full WP-M3B-6 scope card next |
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

## Currently in progress: WP-M3B-5 — Doctor readiness and resource inventory (implemented, awaiting independent review)

`docs/work-packages/wp-m3b-5-ewp.md` is written and implemented (§11). It supersedes the informal pre-check notes this section used to carry — in particular, an earlier version of this section incorrectly claimed "no `doctor --fix` plan-generation path exists." That was wrong: `internal/setup/planner.go` already implements `Planner.Plan(report *DoctorReport, ...)`, converting `DiagnosticFinding`s into real `SetupPlan`/`SetupAction`s — not rebuilt here.

**What's implemented (EWP §11):**
1. `protocol.ResourceInventory` (new `internal/protocol/resource_inventory.go`: `HardwareSummary`, `CredentialInventoryEntry`, `PolicySummary`, `ResourceInventory`) — a real `protocol.Record` (`schema_version`, `Validate()`, `NewRecord` case, registered in both `schema.RecordKindToSchema` **and** `AllNames()` together this time). New `schemas/resource-inventory.schema.json` + `fixtures/protocol/resource-inventory.valid.json`, wired into the generic round-trip suite.
2. `Doctor.BuildResourceInventory` (`internal/setup/doctor.go`) — a pure projection over `Run`'s own facts/fingerprint/endpoints/hosts, plus new `DoctorOptions.CredentialManager`/`CredentialRefs` fields wiring in WP-M3B-4's `credentials.Manager`. A nil `CredentialManager` degrades to an empty `Credentials` section (same pattern `discoverEndpoints` already uses for a nil `CognitionService`); a *malformed configured* `CredentialRef` fails `BuildResourceInventory` outright rather than being papered over with a fabricated evidence entry — see EWP §11.1 for why the original design sketch (degrade to `Unavailable`) was wrong and got corrected during implementation.
3. Two new `Planner.Plan` finding-code blocks (`internal/setup/planner.go`): `FindingCodeAuthExpired` → one `recipe.manual.reauthenticate` `SetupAction` per affected endpoint (sourced from `DiscoveredEndpoints`, not parsed from finding text), gated on `TargetAll`/`TargetAuth`; `FindingCodeNoCodingEndpoint` deliberately produces nothing (EWP §3's MUST-constraint boundary — no generic "install some coding CLI" action, which would mean recommending a provider).

**Verified:** `go build ./...`, `go vet ./...`, `gofmt -l` (every changed file, clean), `go test -count=1 ./...` (all 29 packages), `go test -race ./internal/setup/... ./internal/protocol/... ./internal/credentials/... ./internal/schema/... ./tests/...` (no races), `GOOS=windows GOARCH=amd64`/`GOOS=linux GOARCH=amd64 go build ./...` (clean cross-compilation). New tests listed in EWP §11's verification table.

**Known blockers / open questions:** none — not yet independently reviewed.

## Next concrete action

Post a PR comment on #10 summarizing the WP-M3B-5 implementation and continue watching for the owner's response. Do not flip WP-M3B-5 to `accepted` unilaterally. Once accepted, proceed to WP-M3B-6 — but first assess `internal/setup/planner.go` (already read in full for this WP; `wp-m3b-5-ewp.md` §0 has the findings) and `cache.go` against the full WP-M3B-6 "Bounded recipes" scope card, per the pre-check pattern every WP in this milestone has used.

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
