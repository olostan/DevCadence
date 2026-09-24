# Handoff — M3B guided bootstrap (feat/m3b-guided-bootstrap)

Last updated: 2026-09-24T03:15:00Z by Claude Code / Sonnet 5 (cloud session, olostan@gmail.com)

Session takeover HEAD: `a38b293` (origin/main HEAD when this session started — branch did not exist yet)
Expected remote HEAD before next push: `82f90a3` (the actual current pushed HEAD — this field always tracks the real remote tip, which is not necessarily the same commit as a WP's own immutable checkpoint SHA in the table below; advance this after every successful push — see "Git safety rules" in AGENT_HANDOFF_PROTOCOL.md)

## STOP — read this before doing anything else on WP-M3B-3

WP-M3B-3 is **not accepted** and has an **unresolved architecture question**
that the next session must not route around. The 10-finding independent
review round is fixed and verified (see the WP table below, EWP §15), but a
**second** review comment
([PR #10](https://github.com/olostan/DevCadence/pull/10#issuecomment-5806753036),
owner) landed while those fixes were being finished, identifying that this
WP's design is Ollama-specific in a way that conflicts with this
repository's own canonical architecture (`docs/MODEL_RUNTIME.md`,
`INVARIANTS.md` DCI-055: model runtimes must be replaceable adapters, not
core-domain dependencies; MLX-LM must be a first-class peer to Ollama, not
a later manual path). Full detail in
`docs/work-packages/wp-m3b-3-ewp.md` §16 — **read it before writing any
more setup/executor code**.

This session did not implement the redesign (a generic
`ensure_local_model`-shaped operation behind a `LocalModelRuntimeAdapter`
boundary, spanning WP-M3B-1's frozen `TypedOperation` union as much as this
WP's executor) — it's a genuine architectural change, not a checkpoint fix,
and this session ran low on budget after the 10-finding round. **The next
session's first job is to get an explicit answer** (from the owner, or by
reading further PR activity if one already arrived) on EWP §16's two
options: (a) implement the runtime-agnostic redesign with MLX-LM as a real
peer adapter before WP-M3B-3 is accepted, or (b) defer it explicitly to a
later WP and accept this checkpoint as an interim Ollama-only state. Do
not silently pick either option.

## Milestone

M3B — Guided bootstrap and onboarding. See
[docs/WORK_PACKAGES.md#m3b](docs/WORK_PACKAGES.md#m3b--guided-bootstrap-and-onboarding)
for the full Work Package breakdown, and `AGENT_HANDOFF_PROTOCOL.md` for the
branch/commit/handoff discipline.

**Read `docs/work-packages/wp-m3b-1-ewp.md` in full before doing anything
else in this milestone — especially its "Provenance" section.** It is not
optional background; it explains a load-bearing fact about this branch's
starting state.

## IMPORTANT — read this before touching internal/setup or internal/protocol/setup.go

`main` (as of `a38b293`, which is this branch's base) already contains a
substantial, tested implementation that predates this protocol and the M3B
work-package breakdown:

- `internal/protocol/setup.go` + `setup_test.go` — `TypedOperation`,
  `Condition`, `SetupPlan`, `PlanDigest`, `IntrinsicPolicy`, managed
  allowlists. This **is** WP-M3B-1, verified and accepted this session (see
  below) — no new code was needed for WP-M3B-1.
- `internal/setup/{doctor,planner,profiles,cache}.go` + tests — this goes
  further, into **WP-M3B-3/5/6 territory** (executor-adjacent planning,
  doctor readiness, profile recommendation, bounded recipes/cache). **This
  session did not assess, verify, or accept this code** — it's out of scope
  for WP-M3B-1 (see the EWP's §9 non-goals). It was authored directly on
  `main` by the project owner on 2026-09-22, before any EWP existed for it.

**Before starting WP-M3B-5 or WP-M3B-6 (or any later WP), the next session
must first check whether `internal/setup/{doctor,planner,profiles,cache}.go`
already substantially satisfies its scope card**, the same way this
session and the WP-M3B-1/2 sessions did — rather than assuming a blank
slate and writing duplicate/conflicting code. If it does, the same pattern
applies: write the EWP describing what's actually there, verify it fresh
against that WP's own acceptance criteria, and accept it — or, if there's a
real design gap or architectural mismatch versus what the EWP should
specify, use the escalate/amend path in `AGENT_HANDOFF_PROTOCOL.md` rather
than silently implementing on top of it or silently rewriting it
(WP-M3B-2's own EWP §12 is a worked example of that escalate-not-reopen
path, for a different WP's frozen contract). **WP-M3B-2 confirmed the
ledger genuinely didn't exist** and built it fresh. **WP-M3B-3 confirmed
`internal/setup/planner.go` is purely plan generation** (no approval,
precondition-rechecking, or execution logic anywhere) and built the actual
executor fresh in `internal/setup/{commandrunner,conditions,operations,
executor}.go` — see that WP's row above and
`docs/work-packages/wp-m3b-3-ewp.md`. `planner.go`/`doctor.go`/`profiles.go`/
`cache.go` remain unassessed for WP-M3B-5/6's own scope — still open.

## Work Package status

| WP | Status | Checkpoint | Validation | Review |
|----|--------|------------|------------|--------|
| WP-M3B-1 | accepted | `6833219` | `go build ./...`, `go vet ./...`, `go test -count=1 ./...` all PASS (see EWP §8) | independent review complete — [PR #10 comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5805285148) (owner), 3 findings, all addressed in EWP §12; verified in [follow-up comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5805447822) |
| WP-M3B-2 | accepted | `bb01bc9` | `go build ./...`, `go vet ./...`, `go test -count=1 ./...`, `go test -race ./internal/setup/...`, `GOOS=windows GOARCH=amd64 go build ./...` all PASS (see EWP §13) | independent review complete — [PR #10 comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5805603916) (owner), 7 findings, all addressed in EWP §14 |
| WP-M3B-3 | implemented, NOT accepted — architecture question open | `<this checkpoint's push>` | `go build ./...`, `go vet ./...`, `go test -count=1 ./...`, `go test -race ./internal/setup/...`, `GOOS=windows GOARCH=amd64 go build ./...` all PASS (see EWP §13) | 10-finding round complete and addressed ([review](https://github.com/olostan/DevCadence/pull/10#issuecomment-5806276805), [fixes in EWP §15](docs/work-packages/wp-m3b-3-ewp.md)); **second review raised an unresolved architecture question** ([comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5806753036), EWP §16) — not yet answered or implemented |
| WP-M3B-4 | not started | — | — | blocked on WP-M3B-3 review |
| WP-M3B-5 | unknown — likely partially pre-existing, unverified | — | — | assess `internal/setup/doctor.go`, `profiles.go` first |
| WP-M3B-6 | unknown — likely partially pre-existing, unverified | — | — | assess `internal/setup/planner.go`, `cache.go` first |
| WP-M3B-7 | not started | — | — | blocked on WP-3/4/5/6 |
| WP-M3B-8 | not started | — | — | blocked on WP-7 |
| WP-M3B-9 | not started | — | — | blocked on all prior |

(Never write "merged" for a WP checkpoint — nothing is merged to `main`
until the whole milestone closes.)

## Currently in progress: WP-M3B-3 — 10-finding round done, architecture question open (see STOP banner above)

WP-M3B-1 and WP-M3B-2 are both fully closed out (accepted, independently
reviewed, findings resolved) — see their rows above.

- **EWP status:** committed at `0fecafd` before any implementation code;
  revised at `23f88b9`-era to add §12/§13; revised again in this session
  to add §14 (interface refinement note renumbered), §15 (10-finding
  review disposition — torn-write-class fixes to lock ownership, crash
  recovery wiring, exported-bypass closure, executable-identity binding,
  supply-chain enforcement, diagnostic truthfulness, a frozen-contract-
  respecting non-mutating rewrite of `state_root_writable`, ledger
  terminality, home-confinement, and ANSI-stripping completeness), and
  §16 (the architecture-correction tracking entry — **read this one**).
- **Base commit this WP started from:** `5bac450`.
- **Pre-check finding:** `internal/setup/planner.go` is confirmed
  **plan-generation only** — no approval, precondition-rechecking, or
  execution logic anywhere in the repo before this WP. Genuine
  from-scratch build, same as WP-M3B-2's ledger.
- **What's implemented:** four new files in `internal/setup`:
  - `commandrunner.go` — the `CommandRunner` interface ADR-0014 §7
    requires; `*process.Runner` satisfies it directly, no adapter.
  - `conditions.go` — `EvaluateCondition`, the live counterpart to
    WP-M3B-1's `Condition` type. Natively implements `command_available`,
    `executable_verified` (digest + optional `--version` check via
    `CommandRunner`), `managed_dir_exists`, `port_listening`, and
    `model_digest_present` (**Ollama-specific only** — loopback HTTP
    `GET /api/tags`; any other `runtime` value fails closed with an
    explicit "unsupported runtime" error). `endpoint_healthy` is an
    **injectable `EndpointHealthChecker` interface with no default
    implementation** — fails closed with no checker configured; no
    current recipe emits this condition kind, so this is a documented,
    currently-inert boundary (EWP §2), not a live gap.
  - `operations.go` — `ApplyOperation`, dispatching all 5
    `TypedOperation` kinds for real: `create_directory` (filesystem),
    `write_managed_config` (new `config.go` managed-config store,
    atomic write), `remove_stale_cache` (delegates to the pre-existing
    `CacheManager.Remove`), `run_diagnostic_check` (4 fixed checks),
    `ollama_pull_model` (real subprocess via `CommandRunner`). Output
    capture reuses `internal/artifacts.Store` rooted at
    `$DEVCADENCE_HOME/artifacts/setup` — see EWP §3 for why that reuse
    is safe.
  - `executor.go` — `Executor.Apply`: digest verification →
    `--yes`-equivalent scope check → refuses to start while any
    interrupted action is unresolved → `ExecutionCreated`/`PlanApproved` →
    walk `plan.Actions` (precondition recheck → `ActionStarting` →
    dispatch or halt on manual → postcondition verify, always reaching
    `ActionTerminated` even on an evaluator error → `ActionTerminated`)
    → `ExecutionFinished` → `ProjectExecutionReport`. Halts the whole
    walk (not just the current action) on precondition drift or a
    manual action. Never persists an output artifact for an action whose
    `Effects` include `EffectAuthentication`. **Now owns the execution
    lock and the ledger's lifecycle itself**: `ExecutorOptions` no longer
    accepts `Ledger`/`Cache`/`Artifacts` — only `Home` (required
    absolute); `NewExecutor` constructs `Cache`/`Artifacts` from `Home`
    internally, and `Apply`/the new `Executor.Recover` each acquire
    `AcquireExecutionLock` before opening the ledger fresh. `Recover(ctx,
    plan)` is the service-level crash-recovery entry point WP-M3B-2's
    `Ledger.ReconcileInterrupted` needed a caller for.
  - `home.go` gained `LocationPath` (the single `ManagedDirectoryLocation`
    → real-path mapping, shared by `EnsureLayout`, the condition
    evaluator, and `create_directory`'s applier) and `ledgerPath` (the
    single `setup-ledger.jsonl` path mapping, shared by `Executor` and
    tests that need to inspect/seed the ledger directly).
  - `cache.go`/`doctor.go`/`planner.go`/`profiles.go` were **not** modified.
  - **Bypass closed:** `applyOperation`/`applierDeps`/`writeManagedConfigKey`
    are now unexported — the executor's approval/lock/ledger gating was
    previously bypassable by calling `ApplyOperation`/`WriteManagedConfigKey`
    directly from anywhere in the module.
- **What's verified:** `go build ./...` clean; `go vet ./...` clean;
  `gofmt -l internal/setup/` clean; `go test -count=1 ./...` — all 26
  packages pass, 0 failures; `go test -race ./internal/setup/...` clean;
  `GOOS=windows GOARCH=amd64 go build ./...` clean. Full test list in
  EWP §13/§15 — every acceptance criterion and every one of the 10
  independent-review findings has a directly-named test.
- **What's left for this WP:** see the STOP banner at the top of this
  file — a second, architectural review comment is open and unanswered
  (EWP §16). Not independent-per-WP-review-pending in the ordinary
  WP-M3B-1/2 sense; this is a real design decision the next session must
  get an explicit answer on before proceeding, not something to silently
  resolve either direction.
- **Known blockers / open questions:** the architecture question in EWP
  §16 (runtime-agnostic `LocalModelRuntimeAdapter` redesign, MLX-LM as a
  first-class peer to Ollama) — see the STOP banner. Separately, the
  milestone-level open question (how much of WP-M3B-5/6 is already
  covered by pre-existing `internal/setup/{doctor,planner,profiles,
  cache}.go`) is still unresolved — no session has touched it yet.

## Next concrete action

**First**, per the STOP banner: get an explicit answer on EWP §16 before
writing more executor/setup code — check for further PR activity on #10
(the owner may have already replied with a decision), and if none has
arrived, treat this as a genuine blocking question, not something to
resolve unilaterally. If the answer is "implement the redesign": that
spans a WP-M3B-1 EWP amendment (a generic `ensure_local_model`-shaped
`TypedOperation`, or equivalent) and a real MLX-LM adapter under
`internal/setup` (or wherever the EWP amendment places it), with
service-level end-to-end coverage on an MLX-capable profile — budget this
as its own substantial session, not a quick follow-up. If the answer is
"defer explicitly": update EWP §16's disposition to record that decision
with attribution, flip WP-M3B-3 to `accepted` at that point, and proceed
to WP-M3B-4/5/6 per the dependency chain in `docs/WORK_PACKAGES.md` (any
order, not concurrently — see `AGENT_HANDOFF_PROTOCOL.md`'s "Concurrency
model"), starting each with the same pre-check pattern against
`internal/setup/{doctor,planner,profiles,cache}.go`.

## Resume checklist for the next agent

1. `git fetch origin feat/m3b-guided-bootstrap` and check out the branch.
   Record the fetched `HEAD` SHA as your own session's "Expected remote
   HEAD" baseline — adopt whatever SHA the fetch actually returns rather
   than assuming it matches this file's "Expected remote HEAD" field
   above; if it doesn't match, reconcile per the protocol's git safety
   rules before doing anything else.
2. Do not trust this file blindly: run `go build ./... && go test -count=1
   ./...` and confirm it matches what's claimed above.
3. Read `docs/work-packages/wp-m3b-1-ewp.md`'s "Provenance" section and
   `docs/work-packages/wp-m3b-2-ewp.md`/`wp-m3b-3-ewp.md` in full (all
   short) — they explain why every later WP needs the same "check what's
   already there" pre-check, and WP-M3B-2's §12 is a worked example of
   escalating rather than reopening a frozen WP's contract if the next WP
   hits a similar gap.
4. Read `docs/WORK_PACKAGES.md`'s entry for whichever WP you're picking up
   next (WP-M3B-4/5/6, per "Next concrete action" above) and its relevant
   ADR-0014 section — and the relevant pre-existing `internal/setup/*.go`
   file(s) for the pre-check.
5. Continue from "Next concrete action" above.
6. At the next durable checkpoint, update this file (including advancing
   "Expected remote HEAD") and push it together with that checkpoint's
   commit.
