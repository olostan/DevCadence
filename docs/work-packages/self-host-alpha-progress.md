# SELF_HOST_ALPHA progress (living handoff)

Authoritative objective: [wp-m5-sh1-self-host-alpha-ewp.md](wp-m5-sh1-self-host-alpha-ewp.md). Keep this file short.

## Status (2026-10-09)

- PR #86 (M5-R3): gofmt fixed on `feat/m5-r3-provider-composition` (f7f2de8). Local hooks bypassed with owner authorization because the sandbox runs as root and `internal/operator/receipts` tests refuse root (`protection_unix.go:18`, `verifier.go:325`; they fail identically on `origin/main`). CI is authoritative.
- SH1-1: implemented on branch `ccr-30e3bc5f-qjnr6g` (draft PR): `internal/selfhost` + composition root in `cmd/devcadence-mcp` (via `mcpadapter.LaunchWith` task-port factory, keeping the adapter's dependency boundary) + `devcadence selfhost check`. Verified with a stub-Ollama deterministic test only; the live test (`TestLiveOllama`) has NOT been run (no Ollama in the sandbox).
- SH1-2..SH1-4: not started. No Ollama in the cloud sandbox: live acceptance must run where Ollama is installed.

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
3. SH1-3 `ProfileSource` over `validation.LoadProfiles`; reviewexec wiring; `review_unavailable`; minimal Accept hand-off; uncertain-retry refusal.
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

## Next action

Review SH1-2, run `TestLiveOllama` where Ollama is installed, then SH1-3 (validation/repair loop, review wiring).
