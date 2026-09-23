# Agent Handoff Protocol

## Scope and why this exists

This document is a **development-process** protocol for building DevCadence
itself. It is not part of DevCadence's own runtime architecture, and it does
not describe how the finished control plane will orchestrate agents —
that is `docs/PROTOCOLS.md`, ADR-0004, and the task/attempt state machine in
`internal/tasks`. Once DevCadence's own task/attempt/worktree machinery is
capable of driving its own development, this protocol should be retired in
favor of that. Until then, we are our own control plane, by hand.

The problem this solves: a milestone-sized piece of work (a full milestone,
or a large Work Package inside one) routinely exceeds what a single agent
session can complete before it runs out of quota — and the person driving
this project holds several different subscriptions/providers that reset on
different schedules. When a session's quota runs out mid-work, whatever
state exists only in that session's memory or in an uncommitted/unpushed
working tree is **gone** until that provider's quota resets, which may be
hours or days. A cloud-hosted session in particular cannot be reached or
resumed at all once its quota is exhausted — the only durable record of
progress is what has been **committed and pushed to GitHub**.

This protocol exists to make that loss impossible: at any point a session
might be cut off, the branch on GitHub should already contain everything
needed for a **different** agent — possibly a different model, a different
provider, with zero memory of this conversation — to pick up exactly where
the previous one stopped.

## The two rules everything else follows from

1. **Commit and push early and often — never batch work waiting for a
   "good stopping point" that might not arrive.** A quota cutoff can happen
   mid-thought. If it would hurt to lose the last hour of work, that work
   should already be on GitHub.
2. **The branch, not the conversation, is the source of truth.** Never
   write anything into a handoff note that assumes the next agent shares
   this session's memory ("as discussed above," "the approach we agreed
   on"). Write it as if a stranger will read it cold, because one will.

## Branch and commit discipline

- One **shared, long-lived branch per milestone** (e.g.
  `feat/m3b-guided-bootstrap`), not one branch per Work Package. All Work
  Packages for that milestone land on this same branch, sequentially, as
  the milestone's own `docs/WORK_PACKAGES.md` entry breaks them down.
- **One PR per milestone**, opened as a **draft** as soon as the first
  commit lands, and kept open/updated across every session and every WP
  until the whole milestone is done. Don't open a fresh PR per WP and
  don't open a fresh PR per session — the continuity is the point.
- **Every meaningful increment is its own commit, pushed immediately** —
  not staged locally and pushed "later." If a session ends between two
  commits, at most one small, easily-redone increment is lost, never a
  whole WP.
- **Never leave the pushed branch in a state that doesn't build.** Prefer
  an incomplete-but-compiling stub (a function that returns
  `ErrNotImplemented`, a test marked `t.Skip("WP-M3B-4")`) over a broken
  tree. A future agent's first move is to build and test what's there;
  that has to work before anything else does.
- Follow `CONTRIBUTING.md`'s existing "one conceptual change per commit"
  guidance — this protocol doesn't change that, it just adds "and push it
  immediately, don't wait to batch."

## The handoff file

A single file, **`HANDOFF.md`, at the repository root of the working
branch** (not `docs/` — it's temporary, not normative documentation), is
the resumoption point for the next agent. It is:

- **Committed to the branch** alongside the code it describes, so it
  travels with `git fetch`/`git checkout`, not left in any one session's
  memory.
- **Updated, not appended to forever** — it describes current state, not a
  chronological log. (Git history is the log; this file is a snapshot.)
- **Rewritten as close to the end of every session as possible** — the
  last thing a session does before it might run out of quota is bring this
  file up to date and push it.
- **Deleted in the milestone's final commit**, once every WP is done and
  the PR is ready for review — it should never merge into `main`.

### Handoff file template

```markdown
# Handoff — <milestone/branch name>

Last updated: <UTC timestamp> by <session/provider identifier, e.g.
"Claude Code / Sonnet 5" or "session run on <provider>">

## Milestone
<Milestone ID and one-line goal, e.g. "M3B — guided bootstrap and onboarding">
See docs/WORK_PACKAGES.md#<milestone> for the full Work Package breakdown.

## Work Package status

| WP | Status | Notes |
|----|--------|-------|
| WP-M3B-1 | done | merged in commits abc123..def456 |
| WP-M3B-2 | in progress | see below |
| WP-M3B-3 | not started | blocked on WP-M3B-2 |
| ... | | |

## Currently in progress: <WP ID>

- **Base commit this WP started from:** `<sha>`
- **What's implemented so far:** <concrete, specific — file paths, function
  names, what they do>
- **What's verified:** <exact commands run and their result — e.g.
  `go test ./internal/setup/... -run TestX` passed; `go build ./...`
  passed; anything NOT yet verified should say so explicitly>
- **What's left for this WP:** <concrete remaining steps, in order>
- **Known blockers / open questions:** <anything needing a human decision,
  or anything the current agent is uncertain about — do not paper over
  uncertainty here>

## Next concrete action

<The single next thing to do. Not "continue implementing WP-M3B-2" —
literally the next file to open / function to write / test to run.>

## Resume checklist for the next agent

1. `git fetch origin <branch>` and check out the branch.
2. **Do not trust this file blindly** — run `go build ./... && go test
   ./...` (or the narrower scope relevant to the in-progress WP) and
   confirm the actual state matches what's claimed above before doing
   anything else. If it doesn't match, fix the discrepancy in this file
   first.
3. Read this WP's entry in `docs/WORK_PACKAGES.md` in full before writing
   code — this handoff file is a status snapshot, not a substitute for the
   WP's actual scope/constraints/acceptance criteria.
4. Continue from "Next concrete action" above.
5. Before this session ends (quota running low, or the WP/milestone is
   done), update this file again and push.
```

### Advisory claim marker (not a lock — this can't be enforced across
independent sessions)

The `Last updated` timestamp doubles as a soft claim marker. If a new agent
picks up the branch and finds `Last updated` very recent (say, under the
last couple of hours), another session may still be actively working on it
— check with the human before proceeding, rather than risk two agents
editing the same WP concurrently and producing conflicting commits. If the
timestamp is old, treat the branch as abandoned (quota exhausted) and
proceed normally.

## Work Package boundaries

Work Packages exist to make quota cutoffs cheap, not just to organize the
milestone conceptually. When splitting a milestone (`docs/WORK_PACKAGES.md`
is where this is recorded per-milestone), prefer boundaries where:

- each WP is independently buildable and testable at its own final commit,
  even if later WPs aren't started yet;
- a WP is small enough that losing "the rest of this WP" to a quota
  cutoff is an acceptable, cheaply-redone loss — if a WP feels like it
  would take one agent session's entire budget, split it further;
  the actual per-WP size still needs case-by-case judgment against the
  budget available, the same way the M3B sizing discussion in this
  project's own history did;
- dependencies between WPs are explicit and linear where possible, so a
  new agent doesn't have to reconstruct which WPs block which from
  scratch.

Per AGENTS.md §6, a WP entry in `docs/WORK_PACKAGES.md` is a *scope card*
(objective, deliverables, constraints, acceptance criteria, dependencies,
non-goals) — not necessarily the full detailed Engineering Work Package
AGENTS.md describes (interface sketches, pseudocode, edge cases). **The
first action of whichever agent starts a given WP is to expand that scope
card into a full Engineering Work Package per AGENTS.md §6** before writing
implementation code, and to record that expansion (as a section in this
WP's part of `docs/WORK_PACKAGES.md`, or linked from it) so the next agent
inherits the detailed plan, not just the scope card.

## What does not change

Everything else in `AGENTS.md`, `CONTRIBUTING.md`, and
`docs/REVIEW_AND_CONVERGENCE.md` still applies unmodified: deterministic
evidence over claims, documentation staying synchronized with behavior,
security-sensitive changes needing a threat-model review, and so on. This
protocol only adds the discipline needed to survive a mid-milestone quota
cutoff — it does not relax anything else.
