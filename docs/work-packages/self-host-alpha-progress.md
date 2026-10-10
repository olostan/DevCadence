# SELF_HOST_ALPHA progress (living handoff)

Authoritative objective: [wp-m5-sh1-self-host-alpha-ewp.md](wp-m5-sh1-self-host-alpha-ewp.md). Keep this file short.

## Status (2026-10-09)

- PR #86 (M5-R3): gofmt fixed on `feat/m5-r3-provider-composition` (f7f2de8). Local hooks bypassed with owner authorization because the sandbox runs as root and `internal/operator/receipts` tests refuse root (`protection_unix.go:18`, `verifier.go:325`; they fail identically on `origin/main`). CI is authoritative.
- SH1-1: implemented on branch `ccr-30e3bc5f-qjnr6g` (draft PR): `internal/selfhost` + composition root in `cmd/devcadence-mcp` (via `mcpadapter.LaunchWith` task-port factory, keeping the adapter's dependency boundary) + `devcadence selfhost check`. Verified with a stub-Ollama deterministic test only; the live test (`TestLiveOllama`) has NOT been run (no Ollama in the sandbox).
- SH1-2: merged (PR #90; see section below).
- SH1-3 (minimal): implemented on `ccr-30e3bc5f-qjnr6g` (draft PR): post-check validation, bounded repair loop, `validation-report` and `review_unavailable` evidence, uncertain-retry refusal. Stub-tested only; `TestLiveOllamaRepair` has NOT been run (no Ollama in the sandbox).
- SH1-4A/C/D (this PR, draft): MCP -> native runtime verified at the MCP transport, candidate handoff packet, `accept` handoff, deterministic stub-based rehearsal (section below). Real-Ollama acceptance is still NOT run (no Ollama in the cloud sandbox): the owner runs it later on a Mac.

## Implementation Surface Map (verified by scout, not yet by tests)

- No production code constructs `taskexec.Executor`, `reviewexec.Executor` or `sessionclients.Composition`. Only composition root: `internal/mcpadapter/launch.go` (`facade.Options` without `Tasks`/`Reviews`), so `Delegate`/`Validate`/`Review` refuse via `nilPort`.
- `taskexec.New(Options)`: needs ProjectID, Lock, ControlPlane, Worktrees, Repositories, Runner, Registry, Policy, Resolver, Drivers, Compiler, Artifacts, StateDir, Clock, IDs, Logger, Profiles. `Options.Profiles` has no implementation (`Validate` -> `validate-not-implemented`).
- `sessionclients.New(Options{LoopbackBaseURLs})` is an `execpolicy.DriverFactory`; tests are httptest only.
- Worker tools (`taskexec/tools.go setupWorkerTools`): `read_file`, `grep`, `symbols`, `write_file` (whole file; scope, symlink, size checks). No `apply_patch`, range replace or `run_command`. Declared tools hardcoded at `delegate.go:~212`.
- YOLO exists only for benchmark verifier (`cmd/devcadence/benchmark.go`, `.devcadence/benchmark.json`). No `unsafe_unconfined` anywhere.
- `facade.Service.Accept` is a stub (service.go:819). No `review_unavailable` outcome. No retry-after-uncertain guard (new attempt = new model call).

## Smallest changes, in order

1. SH1-1 composition root (`internal/selfhost`) + `mcpadapter` wiring + no-fake-driver success test.
2. SH1-2 (done, see below) `apply_patch`/range-replace tool via shared write-path check; `run_command` gated by explicit local-dev execution mode (user-level trust, not repo config).
3. SH1-3 (minimal, done, see below) `ProfileSource` over `validation.LoadProfiles`; `review_unavailable`; uncertain-retry refusal. Still open: reviewexec wiring (the minimal `accept` hand-off is done in SH1-4C).
4. SH1-4 dogfood scripts/records.

## SH1-1 usage (owner-local, user-level only)

Config (never read from the project repo): only the existence of `$DEVCADENCE_HOME/config/selfhost.json` enables the executor. `DEVCADENCE_OLLAMA_URL` / `DEVCADENCE_OLLAMA_MODEL` may only override fields of that file and can never enable self-host on their own. Assumption: `DEVCADENCE_HOME` is user-controlled and trusted.

- `model` (required in the file; env override `DEVCADENCE_OLLAMA_MODEL`; `:latest` is implied when no tag), `ollama_url` (loopback only, default `http://127.0.0.1:11434`; `DEVCADENCE_OLLAMA_URL`), `endpoint_id` (default `ollama-local`), `context_tokens` (default 32768).
- Present config makes `devcadence-mcp` build the executor (`facade.Options.Tasks`); absent config leaves behavior unchanged. With config present, Ollama down or model missing fails launch with an actionable error: this fail-closed behavior is intentional, and the MCP server must be restarted after `ollama serve` / `ollama pull`. A registered-repository read error only warns and leaves execution unavailable (same degradation as the drift observer). Models are never pulled. The model digest is probed at launch only: a later `ollama pull` may change what the model name resolves to until the server is restarted. The Ollama URL must be a bare loopback origin (no path, query or fragment).
- Policy: no operator receipt exists for a user-level setup, so `internal/selfhost/policy.go` supplies an explicit `owner_local_unsigned` PolicySource (one loopback local grant, implementer role, zero spend, unknown usage allowed, 3 attempts). `execpolicy.Load` receipt verification is unchanged and not used here; a warning is logged at launch.
- Check: `devcadence selfhost check [-json]` prints Ollama version, selected model digest and installed models.
- Live test (needs a running Ollama and an installed model): `DEVCADENCE_LIVE_OLLAMA=1 DEVCADENCE_OLLAMA_MODEL=<model> go test ./internal/selfhost -run TestLiveOllama -v -timeout 30m`.
- Not yet: Ollama `num_ctx` is not set by the client; `context_tokens` only budgets the prompt.

## SH1-2 worker tools (implemented; stub-tested only, no real-model run)

- `apply_patch` (`internal/taskexec/tool_patch.go`): args `{path, edits:[{old_text,new_text}]}`; exact-text replace, every `old_text` must match exactly once in the ORIGINAL content. All-or-nothing; atomic temp-file + rename preserving mode. Stable error codes in the message: `stale_context`, `ambiguous_context`, `empty_old_text`, `overlapping_edits`, `no_edits`, `not_regular_file`, `binary_or_non_utf8`, `too_large`, `file_not_found`. Result JSON: path, hunks_applied, bytes_before/after, compact diff (8 KiB cap). Shares one guard with `write_file` (relative path, no `..`/`.git`, write scope, containment, symlinked path components refused, 256 KiB cap, 64 distinct files per attempt across both tools).
- `run_command` (`internal/taskexec/tool_command.go`): `{argv, cwd?, timeout_seconds?}`; no shell, direct exec through `process.Runner`; env is `process.BaseEnv()` with HOME replaced by a per-attempt scratch dir (`<state>/attempts/<attempt>/home`), `GIT_CONFIG_NOSYSTEM=1`, `GOTOOLCHAIN=local` and the host's GOCACHE/GOMODCACHE/GOPATH (from `go env`, so offline builds from warm caches work; GOPROXY untouched). No credential variables, SSH agent or GOFLAGS are forwarded, cwd confined to the worktree (symlinks refused), default 120 s / max 600 s, combined output head+tail 16 KiB. Basename denylist (git, gh, ssh, scp, sftp, curl, wget, shells, env/xargs/sudo/su/doas) and relative-path executables refused. Executables whose real path is inside the worktree (scripts/binaries the model wrote) are refused, and the denylist also applies to the symlink-resolved basename. Best effort, NOT a sandbox: an unconfined process can still read any user-readable file by absolute path, a renamed copy of a denied binary outside the worktree is not detected, and `go run`, `go test -exec`, `make` and interpreters are not blocked; `go`, `python` etc. can still do anything the local user can, and files they create outside the write scope fail candidate materialization.
- Mode: `execution_mode` in `$DEVCADENCE_HOME/config/selfhost.json`, `"strict"` (default) or `"yolo"` (maps to `taskexec.ExecutionUnsafeUnconfinedLocal`). `DEVCADENCE_EXECUTION_MODE=strict` may only downgrade; no env value enables yolo; never read from the repo. Strict: `run_command` is not offered and the handler refuses before any process exists (a scripted call to an undeclared tool fails the attempt at the driver integrity check). Yolo: warning at launch and per attempt, no per-command approvals, every result and audit record carries `unsafe_unconfined`.
- Audit: the attempt's command trace (argv, cwd, exit code, duration, timed_out, truncated, output sha256, refusals) is stored via the existing `ArtifactSink` as kind `command-trace` and attached to `CandidateProduced.Artifacts` and to `AttemptFailed.Artifacts` on every failure path (via `failAttempt`) when commands were attempted. If the trace cannot be stored after commands ran, the attempt fails closed (`reason=audit_unavailable`, no candidate). No schema or event change. Processes are killed as a group after every run; leftover `.apply_patch-*.tmp` files are swept before candidate materialization.
- Fixed on the way: `runDelegate` executed every tool call a second time after the driver had already executed it via the mediator; it now skips, per call ID, the calls the driver executed.
- Tests: `internal/taskexec/tool_patch_test.go`, `tool_command_test.go`, `internal/selfhost/tools_scenario_test.go` (stub-Ollama scenario: apply_patch, write_file, `go test ./...`), config tests in `selfhost_test.go`. Stub-based: these prove wiring and policy, not real model behavior.

## SH1-3 post-check and repair loop (implemented; stub-tested only, no real-model run)

- Flow (`internal/taskexec/postcheck.go`, `delegate.go`): when the model replies without tool calls, the project's validation profile runs in the candidate worktree through `validation.RunProfile` BEFORE any candidate commit. On failure the same model session gets one bounded user message (failing check, argv, exit code, first failing `go test` name, last 4 KiB of output; full logs stay in artifacts) and may repair with its tools. Turns and request bytes count against the existing limits. Still failing after `max_repair_rounds` -> attempt fails `reason=validation_failed effects=none`, no candidate, `validation-report` attached to the failure.
- Config (user-level `selfhost.json`): `execution_mode` `strict`|`yolo`, `max_repair_rounds` (default 2, hard max 5, 0 = check once without repair).
- Project profile: `.devcadence/validation.yaml` in the registered repo, profile `default` (format of `validation.LoadProfiles`, e.g. `profiles: {default: {checks: [{id: go-test, argv: [go, test, ./...], timeout: 5m}]}}`). Absent file = no post-check. It is read from the registered checkout when a delegation starts (not from the model-edited worktree). Its commands are repository-controlled, so they run ONLY in `yolo`; in `strict` Delegate refuses before any subprocess or model call with ref `postcheck_requires_yolo`. Same env scrub as `run_command` (scratch HOME, no credentials); `validateProfileSafety` still rejects shells/network arguments.
- Evidence (via `ArtifactSink`, attached to `CandidateProduced.Artifacts` / `AttemptFailed.Artifacts`, no schema change): `validation-report` (profile id and digest, per-round per-check status, exit code, duration, rounds used, passed, `unsafe_unconfined`), `command-trace`, and on candidates `review-status` = `review_unavailable` ("no independent reviewer configured; owner manual acceptance required"). `independent` is false: nobody independent reviewed the candidate.
- Retry guard: a new Delegate for a task with a prior attempt whose summary contains `effects=uncertain` is refused (ref `uncertain_prior_attempt`) before any model call. There is no owner acknowledgement mechanism yet.
- Live acceptance (owner machine, real Ollama and an installed model; never pulls): `DEVCADENCE_OLLAMA_MODEL=<installed-model> scripts/selfhost-live.sh` (preflights `devcadence selfhost check`, then `TestLiveOllamaRepair`, which builds a disposable Go project with a real defect and prints model identity and digest, model commands, validation rounds, candidate commit, manifest and review label).
- Limitations: post-check validation EXECUTES MODEL-WRITTEN CODE (tests, Makefiles, scripts, `go generate`-style hooks) unconfined as the local user in `yolo`; treat the candidate worktree as untrusted. Strict mode CANNOT delegate in any repo containing `.devcadence/validation.yaml` (Delegate refuses with `postcheck_requires_yolo`). The uncertain-retry refusal has no override: create a new task or work package. Tests are stub-based and prove wiring and failure semantics, not real model behavior. `yolo` is unconfined as the local user, not a sandbox, and the post-check runs the repository's commands in that mode. Candidates carry `review_unavailable`: reviewexec is not wired, manual owner acceptance is required. Post-check time counts against the operation deadline but not the model meter. A passing check that leaves untracked out-of-scope files (for example a coverage profile) still fails candidate scope checks. Captured check output is bounded to 4 MiB (head kept).

## SH1-4 MCP delegation path, candidate handoff, rehearsal (stub-based plumbing only; NOT a real-model result)

**MCP surface** (exactly 11 tools, all reach the real facade; with selfhost config present `delegate`, `validate` and candidate inspection reach the real `taskexec.Executor`): `project_state`, `create_work_package`, `delegate` (returns an operation handle at once), `task_status` (poll the operation handle until `completed|failed|cancelled`; `task_id` form returns the task, its candidate and, new, `candidate_handoff`), `validate`, `accept` (still refused; new: carries `result.handoff`), `reject`, `record_decision`, `request_evidence`, `investigate` (MODEL_UNAVAILABLE: no scout runtime), `review` (MODEL_UNAVAILABLE: reviewexec not wired, honest `review_unavailable`). Absent `selfhost.json`: `delegate`/`validate` refuse with `MODEL_UNAVAILABLE` and evidence ref `runtime-not-installed:task-execution|validation`, and launch logs the remedy. Strict mode in a repo with `.devcadence/validation.yaml`: `delegate` refuses synchronously with `POLICY_DENIED` + ref `postcheck_requires_yolo`, no model call, no subprocess.

**Handoff packet** (`facade.CandidateHandoff`; schema `candidateHandoff` in `principal-task-status-response`, fixtures `principal-*-response.valid-*handoff.json`): task/attempt, base and candidate commit, durable ref `refs/devcadence/candidates/<task>-<attempt>` (created with `git update-ref` in the attempt materialization, create-only; moves no branch and not HEAD; nothing pushed or merged; failing to create it fails the attempt closed), worktree branch/path, changed-file manifest, model endpoint/model/digest, execution mode, `review` (`review_unavailable`, `independent=false`), post-check rounds (repair history), `validate` outcomes, command-trace counts, and exact `git -C <repo> diff|log|merge --no-ff|cherry-pick` text. Built on demand from durable records by `Executor.InspectCandidate` (optional `facade.CandidateInspector`, found by type assertion).

**Owner steps outside MCP** (no MCP tool exists for them): create the task and start design (`devcadence task create ...`, `devcadence event append -type TaskDesignStarted`), approve the proposed Work Package after `create_work_package` (`devcadence event append -type WorkPackageApproved -task <alias> -payload '{...}'`; the record is already stored), and integrate the candidate. `accept` requires at least one `review_ids` entry: pass the sentinel `review_unavailable` while no reviewer runs.

**Antigravity MCP config** (`mcp_config.json`; build with `make build-mcp`, binary `bin/devcadence-mcp`):

```json
{"mcpServers": {"devcadence": {
  "command": "/ABS/PATH/DevCadence/bin/devcadence-mcp",
  "env": {"DEVCADENCE_HOME": "/Users/you/.devcadence", "DEVCADENCE_PROJECT_ID": "myproject"}}}}
```

`$DEVCADENCE_HOME/config/principal-binding.json` (0600, dir 0700) must grant the tools; `$DEVCADENCE_HOME/config/selfhost.json` (owner-local, never in the repo):

```json
{"model": "qwen2.5-coder:7b", "ollama_url": "http://127.0.0.1:11434", "execution_mode": "yolo", "max_repair_rounds": 2}
```

**Principal call sequence:** `project_state` -> `project_state(focus=task)` -> `create_work_package` (planning revision = current `state_revision`) -> owner approves -> `project_state` -> `delegate` -> poll `task_status(operation)` -> `task_status(task_id)` (read `candidate_handoff`) -> `validate(profile_id=default)` + poll -> `task_status` again -> `accept` (returns the handoff and refuses) -> owner integrates. Antigravity never edits files: all edits are made by the executor's tools in an isolated worktree.

**Owner inspect/merge:** run the packet's `inspect.diff` and `inspect.log`; integrate manually from your target branch with `inspect.merge` or `inspect.cherry_pick` (or `git merge refs/devcadence/candidates/<task>-<attempt>`). Delete the ref and the `devcadence/<task>/<attempt>` branch/worktree when done.

**Rehearsal** (`cmd/devcadence-mcp/rehearsal_test.go`, MCP in-memory transport -> real composition root -> real executor -> scripted httptest Ollama; disposable repo with `internal/calc`, committed `.devcadence/validation.yaml` gofmt-check + targeted `go test`): happy path with a failing first validation, repair and pass; asserts edits by DevCadence tools, repair history, commits/manifest/ref with primary HEAD unmoved, provenance, `review_unavailable`, that the printed `git diff`/`log`/`cherry-pick` commands work; plus strict-mode refusal, scope violation and absent-config refusal. Limitations: stub only; `validate` runs repository commands outside `yolo` gating (existing behavior); handoff worktree path may be gone after cleanup (the ref stays); `ref` creation requires a repository whose refs dir is writable; the 32 KiB response cap bounds very large manifests (capped at 256 entries).

## Next action

Run `scripts/selfhost-live.sh` where Ollama is installed (SH1-2/SH1-3 live acceptance), then follow the Antigravity sequence above against a real model; reviewexec wiring and an MCP approval tool remain open.
