# ADR-0008: Controlled process execution and environment policy

## Status
Accepted (M2).

## Context

M2 is the first milestone that executes external binaries on the operator's
behalf. DCI-033 requires explicit working directory, environment policy,
timeout/cancellation and captured output, and forbids unconstrained shell
authority by default. docs/SECURITY.md §5 requires structured argv, not a
shell string. ENGINEERING_STANDARDS.md §8 additionally requires the runner to
report command identity, timestamps, exit code/signal, captured output with
truncation indication, and a deterministic digest for stored artifacts — and
to distinguish infrastructure failure from an ordinary nonzero exit.

## Decision

### One runner, one entry point, no shell

`internal/process.Runner.Run` accepts a `Spec{Executable, Args, Dir, Env,
Timeout, ...}` and never a shell string. There is no second, more permissive
entry point anywhere in the M2 packages; every command DevCadience issues —
Git inspection, worktree operations, validation checks, the ad hoc `devcadience
run` CLI command — goes through this one function.

### Environment is never inherited implicitly

`Spec.Env` is the *complete* environment; `os.Environ()` is never copied in.
`process.BaseEnv()` returns a minimal, conservative starting point (`PATH`,
`HOME`, a fixed locale) that a caller layers project- or command-specific
values onto with `process.MergeEnv`. Executable resolution by bare name
(`git`, `go`) is looked up only in the `PATH` entry of `Spec.Env`, never in
the daemon's own `PATH` — so a caller that wants `go` found must say where
`go` lives. This is what makes "controlled environment" a real property
rather than a convention: the runner cannot silently reuse ambient state a
caller did not name. A relative path executable (e.g. `./tool`) is refused
outright, because it would resolve against the daemon's own working
directory rather than `Spec.Dir`, contradicting the explicit-`Dir` guarantee.

### Outcome categories are distinguished, not conflated

`Run` returns `(Result, error)`. The error is non-nil only when the command
never meaningfully ran under the caller's control: invalid configuration
(`CategoryInvalidArgument`), an executable the runner could not resolve
(`CategoryNotFound`), or an internal runner failure
(`CategoryInternal`). A command that started and then exited — with any exit
code, including nonzero — is reported through `Result.Status =
StatusCompleted` and `Result.ExitCode`, with a **nil** error: a failing test
command is expected output, not infrastructure failure (DCI-041, restated in
docs/IMPLEMENTATION_PLAN.md M2 §9). A command stopped by its own `Spec.Timeout`
is `StatusTimeout`; a command stopped because the caller's `context.Context`
was cancelled is `StatusCancelled`. `internal/validation` maps these onto
`protocol.CheckStatus`: `StatusCompleted` with exit 0 is `pass`, nonzero is
`fail`, `StatusTimeout` is `error` (it establishes no fact about the code
under test), `StatusCancelled` is `cancelled`, and a runner error that never
started the process is also `error`.

### Process-tree cancellation

On timeout or cancellation the runner kills the process's whole group
(`Setpgid` at start, `SIGKILL` to the negative pid) on POSIX platforms, not
only the immediate child, so a controlled command that spawns further
children (a shell wrapper, a build tool's own workers) does not outlive the
kill (ENGINEERING_STANDARDS.md §7 and §9). The Runner holds no per-call
mutable state, so it is reusable and safe for concurrent use after a timeout
or cancellation — proven by `TestRunnerReusableAfterTimeout` and by the
worktree package's concurrent-creation test, both of which share one Runner
across goroutines.

### Output is bounded, never silently

`Spec.MaxStdoutBytes` / `MaxStderrBytes` bound captured output (default 4
MiB via `DefaultMaxOutputBytes`); once reached, further bytes are dropped and
`Result.Stdout/StderrTruncated` is set. The source is still drained (not
blocked) so a live process is never stalled on a full internal buffer. This
mirrors the ValidationResult schema's `output_truncated` field exactly, so
truncation is never invisible.

### cwd authorization is a caller responsibility, not a runner feature

The runner validates that `Spec.Dir` is an absolute, existing directory, but
it does not itself restrict *which* directory a caller may name — there is
no chroot or sandbox. Confinement to an attempt's worktree is achieved by
construction: `internal/worktrees.Manager` is the only source of worktree
paths, and every M2 caller that runs commands against an attempt passes the
path it received from the manager, never an operator-supplied string. A
future milestone that hands command authority to an LLM-driven agent must
enforce this at whichever layer constructs the `Spec.Dir`, exactly as
`internal/validation` and the worktree-scoped CLI commands do today; adding
OS-level sandboxing to the runner itself is not justified by the bootstrap
threat model in docs/SECURITY.md §17 and is left for a future ADR if a
concrete need appears.

## Consequences

- No package in this build can execute a shell string; `git grep -- '"sh",
  "-c"'` outside test fixtures finds nothing in the M2 packages themselves.
- A failing `go test` inside a validation profile is indistinguishable in
  shape from a passing one except for `ExitCode`/`Status` — both are ordinary
  evidence, not an error path.
- Killing a runaway command cannot leave the Runner unusable for the next
  call.
- Command authority is bounded by what constructs `Spec`, which is exactly
  the minimum command-authority boundary docs/IMPLEMENTATION_PLAN.md M2 §10
  asks for without inventing a policy language beyond validation profiles
  (`internal/validation.Profile`).

## Alternatives considered

- **`exec.CommandContext` with `os/exec`'s own environment inheritance.**
  Rejected: the default behaviour inherits the parent's environment, which
  is exactly the "blindly inherit every parent environment variable" M2 §11
  forbids.
- **A generic `RunShell(cmd string)` convenience for callers that "just need
  one command".** Rejected outright; every M2 caller that needs `sh -c`
  semantics (there are none in production code) would have to say so
  explicitly by passing `sh`/`-c` as argv, which is visibly different from a
  shell-string API and cannot be reached by accident.
