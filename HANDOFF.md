# Handoff — M3B guided bootstrap (feat/m3b-guided-bootstrap)

Last updated: 2026-09-24T01:30:00Z by Claude Code / Sonnet 5 (cloud session, olostan@gmail.com)

Session takeover HEAD: `a38b293` (origin/main HEAD when this session started — branch did not exist yet)
Expected remote HEAD before next push: `2d0c8a6` (advance this after every successful push — see "Git safety rules" in AGENT_HANDOFF_PROTOCOL.md)

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

**Before starting WP-M3B-3 (or any later WP), the next session must first
check whether `internal/setup/{doctor,planner,profiles,cache}.go` already
substantially satisfies its scope card**, the same way this session and the
WP-M3B-1 session did — rather than assuming a blank slate and writing
duplicate/conflicting code. If it does, the same pattern applies: write the
EWP describing what's actually there, verify it fresh against that WP's own
acceptance criteria, and accept it — or, if there's a real design gap or
architectural mismatch versus what the EWP should specify, use the escalate/
amend path in `AGENT_HANDOFF_PROTOCOL.md` rather than silently implementing
on top of it or silently rewriting it (WP-M3B-2's own EWP §12 is a worked
example of that escalate-not-reopen path, for a different WP's frozen
contract). **WP-M3B-2 confirmed the ledger genuinely didn't exist** (only
the `SetupLedgerEvent` *type* did, correctly WP-M3B-1 scope) and built it
fresh in `internal/setup/ledger.go`, `home.go`, `execlock.go` — see that
WP's row above and `docs/work-packages/wp-m3b-2-ewp.md`.

## Work Package status

| WP | Status | Checkpoint | Validation | Review |
|----|--------|------------|------------|--------|
| WP-M3B-1 | accepted | `6833219` | `go build ./...`, `go vet ./...`, `go test -count=1 ./...` all PASS (see EWP §8) | independent review complete — [PR #10 comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5805285148) (owner), 3 findings, all addressed in EWP §12; verified in [follow-up comment](https://github.com/olostan/DevCadence/pull/10#issuecomment-5805447822) |
| WP-M3B-2 | implemented, pending review | `2d0c8a6` | `go build ./...`, `go vet ./...`, `go test -count=1 ./...`, `go test -race ./internal/setup/...` all PASS (see EWP §13) | not yet independently reviewed |
| WP-M3B-3 | unknown — likely partially pre-existing, unverified | — | — | blocked on WP-M3B-2 review; assess `internal/setup/planner.go` first |
| WP-M3B-4 | not started | — | — | blocked on WP-M3B-3 |
| WP-M3B-5 | unknown — likely partially pre-existing, unverified | — | — | assess `internal/setup/doctor.go`, `profiles.go` first |
| WP-M3B-6 | unknown — likely partially pre-existing, unverified | — | — | assess `internal/setup/planner.go`, `cache.go` first |
| WP-M3B-7 | not started | — | — | blocked on WP-3/4/5/6 |
| WP-M3B-8 | not started | — | — | blocked on WP-7 |
| WP-M3B-9 | not started | — | — | blocked on all prior |

(Never write "merged" for a WP checkpoint — nothing is merged to `main`
until the whole milestone closes.)

## Currently in progress: WP-M3B-2, implemented, awaiting independent review

WP-M3B-1 is fully closed out (accepted, reviewed) — see its own row above
and `docs/work-packages/wp-m3b-1-ewp.md`.

- **EWP status:** committed at `6255e28` (`docs/work-packages/wp-m3b-2-ewp.md`),
  before any implementation code — per the Principal/Implementer sequence.
  Revised in the same file (not yet pushed as of this HANDOFF update — see
  next push) to add §12 (a design decision made during implementation) and
  §13 (deterministic evidence) once the code landed.
- **Base commit this WP started from:** `8cc2378`.
- **What's implemented so far:** `internal/setup/home.go` (`ResolveHome`,
  `EnsureLayout`), `internal/setup/execlock.go` (`AcquireExecutionLock`,
  blocking exclusive `flock` on `state/setup.lock`, reusing the existing
  `lockExclusive`/`unlock` primitives from `lock_unix.go`/`lock_windows.go`),
  `internal/setup/ledger.go` (`OpenLedger`/`Ledger.Append`/`Ledger.Events`
  with hash-chain verification and torn-write recovery; `FindInterrupted`/
  `PostconditionChecker`/`ResolveInterrupted` for crash recovery without
  re-execution; `ProjectExecutionReport` for the derived `SetupExecutionReport`
  projection), plus `home_test.go`/`execlock_test.go`/`ledger_test.go`.
  `cache.go`/`doctor.go`/`planner.go`/`profiles.go` were **not** modified —
  reused as-is per the EWP's §0 provenance check.
- **Design decision made during implementation:** `ProjectExecutionReport`
  takes `machineFingerprint` as an explicit parameter rather than reading it
  from the ledger, because no `SetupLedgerEvent` payload carries it (only
  `SetupPlan.MachineFingerprint` does) and WP-M3B-1's ledger-event type
  contract is an accepted, reviewed checkpoint not to be reopened for a
  projection-layer convenience. Full rationale in EWP §12 — read this before
  touching `ProjectExecutionReport`'s signature.
- **What's verified:** `go build ./...` clean; `go vet ./...` clean;
  `go test -count=1 ./...` — all 26 packages pass, 0 failures; `go test
  -race ./internal/setup/...` clean (the lock/ledger code is exactly where
  a race would show up, so this was run now rather than deferred to
  WP-M3B-9). Full test list in EWP §13.
- **What's left for this WP:** independent per-WP checkpoint review (same
  pattern as WP-M3B-1 — open a PR review comment against the pushed
  commit, address findings, then flip this row and the EWP's disposition
  to `accepted`).
- **Known blockers / open questions:** none for WP-M3B-2's own scope. The
  milestone-level open question from WP-M3B-1's handoff (how much of
  WP-M3B-3/5/6 is already covered by pre-existing `internal/setup/*.go`)
  is still unresolved — this session did not touch it.

## Next concrete action

Push this checkpoint (EWP revision + `home.go`/`execlock.go`/`ledger.go`
+ tests + this HANDOFF.md update as one commit), open/continue PR #10 for
review of WP-M3B-2, and address any findings the same way WP-M3B-1's
review was closed. Once WP-M3B-2 is accepted, start WP-M3B-3
("Executor and approval semantics") — **first** check whether
`internal/setup/planner.go` already substantially covers its scope (per
the pre-check pattern established for WP-M3B-1/2), specifically: does it
already implement the two-step approval workflow, precondition
rechecking, and the `--yes`-equivalent scope restriction, or does it only
generate plans (i.e. is it a WP-M3B-6 "planner"/recipes component wearing
a name that sounds like WP-M3B-3's executor)? Read
`internal/setup/planner.go` in full before assuming either answer.

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
   `docs/work-packages/wp-m3b-2-ewp.md` in full (both short) — they explain
   why every later WP needs the same "check what's already there" pre-check,
   and WP-M3B-2's §12 is a worked example of escalating rather than
   reopening a frozen WP's contract if the next WP hits a similar gap.
4. Read `docs/WORK_PACKAGES.md`'s WP-M3B-3 entry and ADR-0014 §2 (approval
   workflow) — and `internal/setup/planner.go` itself, per "Next concrete
   action" above.
5. Continue from "Next concrete action" above.
6. At the next durable checkpoint, update this file (including advancing
   "Expected remote HEAD") and push it together with that checkpoint's
   commit.
