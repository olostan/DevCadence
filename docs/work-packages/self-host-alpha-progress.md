# SELF_HOST_ALPHA progress (living handoff)

Authoritative objective: [wp-m5-sh1-self-host-alpha-ewp.md](wp-m5-sh1-self-host-alpha-ewp.md). Keep this file short.

## Status (2026-10-09)

- PR #86 (M5-R3): gofmt fixed on `feat/m5-r3-provider-composition` (f7f2de8). Local hooks bypassed with owner authorization because the sandbox runs as root and `internal/operator/receipts` tests refuse root (`protection_unix.go:18`, `verifier.go:325`; they fail identically on `origin/main`). CI is authoritative.
- SH1-1..SH1-4: not started. No Ollama in the cloud sandbox: live acceptance must run where Ollama is installed.

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

## Next action

Merge #86 when CI is green, then branch from `main` for SH1-1.
