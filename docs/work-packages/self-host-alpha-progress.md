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
2. SH1-2 `apply_patch`/range-replace tool via shared write-path check; `run_command` gated by explicit local-dev execution mode (user-level trust, not repo config).
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

## Next action

Review SH1-1, run `TestLiveOllama` where Ollama is installed, then SH1-2.
