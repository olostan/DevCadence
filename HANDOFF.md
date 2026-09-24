# Handoff — M3B guided bootstrap (feat/m3b-guided-bootstrap)

Last updated: 2026-09-24T02:10:00Z by Claude Code / Sonnet 5 (cloud session, olostan@gmail.com)

Session takeover HEAD: `a38b293` (origin/main HEAD when this session started — branch did not exist yet)
Expected remote HEAD before next push: `23f88b9` (the actual current pushed HEAD — this field always tracks the real remote tip, which is not necessarily the same commit as a WP's own immutable checkpoint SHA in the table below; advance this after every successful push — see "Git safety rules" in AGENT_HANDOFF_PROTOCOL.md)

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
| WP-M3B-3 | implemented, pending review | `23f88b9` | `go build ./...`, `go vet ./...`, `go test -count=1 ./...`, `go test -race ./internal/setup/...`, `GOOS=windows GOARCH=amd64 go build ./...` all PASS (see EWP §13) | not yet independently reviewed |
| WP-M3B-4 | not started | — | — | blocked on WP-M3B-3 review |
| WP-M3B-5 | unknown — likely partially pre-existing, unverified | — | — | assess `internal/setup/doctor.go`, `profiles.go` first |
| WP-M3B-6 | unknown — likely partially pre-existing, unverified | — | — | assess `internal/setup/planner.go`, `cache.go` first |
| WP-M3B-7 | not started | — | — | blocked on WP-3/4/5/6 |
| WP-M3B-8 | not started | — | — | blocked on WP-7 |
| WP-M3B-9 | not started | — | — | blocked on all prior |

(Never write "merged" for a WP checkpoint — nothing is merged to `main`
until the whole milestone closes.)

## Currently in progress: WP-M3B-3, implemented, awaiting independent review

WP-M3B-1 and WP-M3B-2 are both fully closed out (accepted, independently
reviewed, findings resolved) — see their rows above.

- **EWP status:** committed at `0fecafd` (`docs/work-packages/wp-m3b-3-ewp.md`)
  before any implementation code; revised in the same file (not yet pushed
  as of this HANDOFF update) to add §12 (one interface refinement made
  during implementation) and §13 (deterministic evidence).
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
    `--yes`-equivalent scope check → `ExecutionCreated`/`PlanApproved` →
    walk `plan.Actions` (precondition recheck → `ActionStarting` →
    dispatch or halt on manual → postcondition verify → `ActionTerminated`)
    → `ExecutionFinished` → `ProjectExecutionReport`. Halts the whole
    walk (not just the current action) on precondition drift or a
    manual action. Never persists an output artifact for an action whose
    `Effects` include `EffectAuthentication`.
  - `home.go` gained `LocationPath` (the single `ManagedDirectoryLocation`
    → real-path mapping, now shared by `EnsureLayout`, the condition
    evaluator, and `create_directory`'s applier — previously that mapping
    only existed inline inside `EnsureLayout`).
  - `cache.go`/`doctor.go`/`planner.go`/`profiles.go` were **not** modified.
- **One interface refinement made during implementation:** `ApplyOperation`
  gained a `procResult *process.Result` return (non-nil only when a
  subprocess actually ran), so the executor knows when to emit
  `ActionProcessCompleted`. Local, non-breaking, recorded in EWP §12 — not
  an architectural change requiring escalation.
- **What's verified:** `go build ./...` clean; `go vet ./...` clean;
  `gofmt -l internal/setup/` clean; `go test -count=1 ./...` — all 26
  packages pass, 0 failures; `go test -race ./internal/setup/...` clean;
  `GOOS=windows GOARCH=amd64 go build ./...` clean. Full test list in
  EWP §13 — every §8 acceptance criterion has a directly-named test.
- **What's left for this WP:** independent per-WP checkpoint review (same
  pattern as WP-M3B-1/2).
- **Known blockers / open questions:** none for WP-M3B-3's own scope. The
  milestone-level open question (how much of WP-M3B-5/6 is already
  covered by pre-existing `internal/setup/{doctor,planner,profiles,
  cache}.go`) is still unresolved — this session did not touch it.

## Next concrete action

Push this checkpoint (EWP revision + the 5 new/changed `internal/setup`
files + 3 new test files + this HANDOFF.md update as one commit), open a
PR review comment for WP-M3B-3 the same way WP-M3B-1/2's were opened, and
address any findings the same way. Once WP-M3B-3 is accepted, start
WP-M3B-4 ("Credential-reference abstraction") — this one is genuinely
security-sensitive per its own scope card (needs the `CONTRIBUTING.md`
threat-model review independent of the rest of the milestone) — or,
per the dependency chain in `docs/WORK_PACKAGES.md`, WP-M3B-4/5/6 may be
done in any order once WP-M3B-3 is accepted (not concurrently — see
`AGENT_HANDOFF_PROTOCOL.md`'s "Concurrency model"). Whichever is picked
next, start with the same pre-check pattern against
`internal/setup/{doctor,planner,profiles,cache}.go` before assuming a
blank slate.

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
