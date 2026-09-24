# Handoff — M3B guided bootstrap (feat/m3b-guided-bootstrap)

Last updated: 2026-09-24T00:00:00Z by Claude Code / Sonnet 5 (cloud session, olostan@gmail.com)

Session takeover HEAD: `a38b293` (origin/main HEAD when this session started — branch did not exist yet)
Expected remote HEAD before next push: `4b006d3` (this session's EWP-commit push — advance this after every successful push)

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

**Before starting WP-M3B-2 or WP-M3B-3, the next session must first check
whether `internal/setup/{doctor,planner,profiles,cache}.go` already
substantially satisfies those WPs' scope cards**, the same way this session
checked for WP-M3B-1 — rather than assuming a blank slate and writing
duplicate/conflicting code. If it does, the same pattern applies: write the
EWP describing what's actually there, verify it fresh against that WP's own
acceptance criteria, and accept it — or, if there's a real design gap or
architectural mismatch versus what the EWP should specify, use the escalate/
amend path in `AGENT_HANDOFF_PROTOCOL.md` rather than silently
implementing on top of it or silently rewriting it. Note WP-M3B-2 (the
ledger) is the more likely genuinely-not-yet-started piece — I did not find
a JSONL setup ledger, `$DEVCADENCE_HOME/state/setup-ledger.jsonl`, or
`setup.lock` implementation anywhere in the repo; only the `SetupLedgerEvent`
*type* (`internal/protocol/setup.go`) and its schema/fixture exist, which is
correctly WP-M3B-1 scope (the type), not WP-M3B-2 scope (the crash-safe
file-backed ledger that writes it).

## Work Package status

| WP | Status | Checkpoint | Validation | Review |
|----|--------|------------|------------|--------|
| WP-M3B-1 | accepted | `4b006d3` | `go build ./...`, `go vet ./...`, `go test -count=1 ./...` all PASS (see EWP §8) | not yet independently reviewed — EWP + fresh verification only |
| WP-M3B-2 | not started | — | — | — |
| WP-M3B-3 | unknown — likely partially pre-existing, unverified | — | — | blocked on WP-M3B-2; assess `internal/setup/planner.go` first |
| WP-M3B-4 | not started | — | — | blocked on WP-M3B-3 |
| WP-M3B-5 | unknown — likely partially pre-existing, unverified | — | — | assess `internal/setup/doctor.go`, `profiles.go` first |
| WP-M3B-6 | unknown — likely partially pre-existing, unverified | — | — | assess `internal/setup/planner.go`, `cache.go` first |
| WP-M3B-7 | not started | — | — | blocked on WP-3/4/5/6 |
| WP-M3B-8 | not started | — | — | blocked on WP-7 |
| WP-M3B-9 | not started | — | — | blocked on all prior |

(Never write "merged" for a WP checkpoint — nothing is merged to `main`
until the whole milestone closes.)

## Currently in progress: none — WP-M3B-1 is closed out for this session

- **EWP status:** committed at `4b006d3` (`docs/work-packages/wp-m3b-1-ewp.md`).
- **Base commit this WP started from:** `a38b293` (origin/main).
- **What's implemented so far:** nothing new — WP-M3B-1's scope was already
  satisfied by pre-existing `main` code (see EWP §1 "Provenance"). The EWP
  itself is the deliverable of this session's work on WP-M3B-1.
- **What's verified:** `go build ./...` clean; `go vet ./...` clean;
  `go test -count=1 ./...` — all 26 packages pass, 0 failures, including
  every WP-M3B-1 acceptance-criteria test named in the EWP §7 table. Not
  yet run: `go test -race ./...` (WP-M3B-9's deliverable per its scope
  card, but worth a spot-check by whoever opens WP-M3B-2 against a mutable
  ledger/lock, since races are exactly what that WP needs to get right).
- **What's left for this WP:** nothing — WP-M3B-1 is done. A dedicated
  independent correctness/security review of the EWP (this branch's own
  per-WP checkpoint review, per `AGENT_HANDOFF_PROTOCOL.md`'s "Per-WP
  checkpoints and review") has not happened yet; it can happen any time
  before milestone closure, in parallel with later WPs starting.
- **Known blockers / open questions:** none for WP-M3B-1 itself. The open
  question for the *milestone* is the one flagged above: how much of
  WP-M3B-3/5/6 is already done in `internal/setup/*.go` and just needs the
  same EWP-retrofit treatment versus a genuine gap. This session did not
  resolve that question for WP-3/5/6 — only for WP-1.

## Next concrete action

Start WP-M3B-2 ("Setup event ledger and operational state layout"). Concrete
first step: `grep -rn "setup-ledger\|DEVCADENCE_HOME\|setup.lock" internal/
` and `find . -iname "*ledger*"` to confirm the ledger writer genuinely
doesn't exist yet (this session's search found only the `SetupLedgerEvent`
protocol type, not a ledger implementation — but verify fresh rather than
trusting this note, per the protocol's own resume checklist). If confirmed
absent, read `docs/WORK_PACKAGES.md`'s WP-M3B-2 entry and ADR-0014 §3-4 in
full, then write `docs/work-packages/wp-m3b-2-ewp.md` per AGENTS.md §6
before writing any ledger code, following the same Principal/Implementer
sequence this session used for WP-M3B-1.

## Resume checklist for the next agent

1. `git fetch origin feat/m3b-guided-bootstrap` and check out the branch.
   Record the fetched `HEAD` SHA as your own session's "Expected remote
   HEAD" baseline (should be `4b006d3` unless someone else pushed after
   this was written — if so, reconcile per the protocol's git safety
   rules before doing anything else).
2. Do not trust this file blindly: run `go build ./... && go test -count=1
   ./...` and confirm it matches what's claimed above.
3. Read `docs/work-packages/wp-m3b-1-ewp.md` in full — it's short, and its
   "Provenance" section is why WP-M3B-2 needs the same pre-check this
   session did for WP-M3B-1.
4. Read `docs/WORK_PACKAGES.md`'s WP-M3B-2 entry and ADR-0014 §3-4.
5. Continue from "Next concrete action" above.
6. At the next durable checkpoint, update this file (including advancing
   "Expected remote HEAD") and push it together with that checkpoint's
   commit.
