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

This protocol bounds that loss and makes recovery deterministic: it does
not make loss of *any* work impossible (an uncommitted increment in
progress at the exact moment of a cutoff is still lost — see "The two rules"
below), but it keeps that loss small and keeps the branch on GitHub always
self-describing enough for a **different** agent — possibly a different
model, a different provider, with zero memory of this conversation — to
verify what's actually there and continue from it correctly.

## Revision history

This is the second revision. The first (reviewed on PR #9) proposed a
model that turned out to conflict with itself and with `AGENTS.md`: it
allowed Work Packages to be "parallelized across sessions" while also
specifying one shared branch and one root `HANDOFF.md` — which cannot
represent multiple concurrent writers safely — and it let the same
execution agent both author and implement a Work Package's detailed design,
collapsing the Principal/Implementer authority split `AGENTS.md` §4/§6/§7
establishes. An independent review
([PR #9 comment](https://github.com/olostan/DevCadence/pull/9#issuecomment-5804998531))
found these plus eight further material/non-blocking issues, all
incorporated into the second revision.

A follow-up closure review of that revision
([PR #9 comment](https://github.com/olostan/DevCadence/pull/9#issuecomment-5805061005))
confirmed all twelve prior findings resolved and found two further issues,
incorporated into this (third) revision: the `HANDOFF.md` deletion step was
still ordered *after* closure freeze, which would make the merged commit
different from the exact commit closure reviewed and froze; and the
remote-`HEAD` guard compared against a fixed session-start SHA rather than
an SHA that advances after each push, which — since every session pushes
through the same GitHub account regardless of which agent is driving —
would make a session's own later push look like a foreign write on its
next push attempt.

## The rules everything else follows from

1. **Commit and push early and often — never batch work waiting for a
   "good stopping point" that might not arrive.** A quota cutoff can happen
   mid-thought. If it would hurt to lose the last hour of work, that work
   should already be on GitHub. This bounds loss to "whatever's uncommitted
   right now," not "however long since the last push."
2. **The branch, not the conversation, is the source of truth.** Never
   write anything into a handoff note that assumes the next agent shares
   this session's memory ("as discussed above," "the approach we agreed
   on"). Write it as if a stranger will read it cold, because one will.
3. **One writer at a time on a given milestone's implementation.** See
   "Concurrency model" below — this is what makes rule 1 and the handoff
   file actually work, rather than racing.
4. **A scope card is not implementation authority.** See "Principal/
   Implementer separation" below — an execution agent does not get to
   invent the design it then implements.

## Concurrency model: single-writer implementation per milestone

Multiple Work Packages inside a milestone may have no dependency on each
other (`docs/WORK_PACKAGES.md` notes this where it's true) and could in
principle be implemented in parallel. **This protocol does not attempt
that.** A shared branch with one `HANDOFF.md` cannot safely represent
multiple concurrent writers — two sessions starting from the same `HEAD`
can race, and one agent's push silently makes another's working tree stale
mid-edit. Real parallel implementation would need per-WP branches/worktrees
under a milestone integration branch, with a human or Principal-level
session integrating completed WP branches; that is real additional
machinery this protocol does not build, because the actual problem it
exists to solve (a solo developer's sessions handing off to each other
across provider quota resets) doesn't need it.

So: **independent-of-each-other Work Packages are still implemented one at
a time**, in the order recorded in `docs/WORK_PACKAGES.md`, by whichever
single session currently holds the branch. Independent **review** (multiple
reviewers examining the same immutable checkpoint) is a different thing and
is encouraged in parallel, same as `docs/REVIEW_AND_CONVERGENCE.md` §1.2
already prefers.

### Git safety rules for the shared milestone branch

Because it's explicitly shared, mutable-until-closure history across
sessions:

- **Never force-push the milestone branch.**
- **Never rebase already-pushed milestone-branch history.**
- Track an **expected remote `HEAD`** — not a fixed "session-start" SHA.
  Since every push in this project goes through the same GitHub account
  regardless of which agent/provider is driving, commit author identity
  cannot distinguish "my own previous push, earlier this session" from "a
  different session wrote here" — only the exact SHA can. So: record the
  remote `HEAD` at takeover into `HANDOFF.md`; **before every push**, fetch
  and require `origin/<branch>` to still equal the expected SHA; **after
  every successful push, advance the expected SHA to the one just
  pushed** (update `HANDOFF.md` with it at the next durable checkpoint). A
  session comparing against a stale session-start SHA instead of its own
  latest push would misidentify its own prior work as a foreign write.
- If the fetched `HEAD` is not the expected SHA, **stop and reconcile**
  (read what changed, merge it in explicitly) rather than force-pushing or
  rebasing over it. Git's non-fast-forward rejection is the actual
  concurrency guard here; the handoff file's timestamp (below) is only
  advisory metadata on top of it,
  never a substitute for it.
- A recent `Last updated` timestamp in `HANDOFF.md` means *likely* still
  active — a session can work for hours between handoff updates, so an
  older timestamp means only *possibly* abandoned, never proof. If there's
  any doubt whether another session is still active, ask the human/owner
  before taking over, rather than assuming abandonment.

### Keeping the milestone branch current with `main`

A milestone branch can live for a while. At WP boundaries (not mid-WP):
fetch `main`; if it advanced materially, merge it into the milestone branch
(never rebase); resolve conflicts explicitly; rerun the relevant
deterministic baseline (`go build ./... && go test ./...` at minimum); and
record the new base in `HANDOFF.md`/the WP checkpoint. This is the same
"merge the base branch into the PR head" pattern this project's own
PR-babysitting rules already use elsewhere — nothing new, just applied to a
long-lived branch instead of a short-lived PR.

## Principal/Implementer separation

`AGENTS.md` §4 makes producing a detailed Engineering Work Package a
Principal-cognition responsibility, before implementation; §6 says a
substantial Work Package must include objective, architectural intent,
MUST/SHOULD/SUGGESTED/LOCAL_DISCRETION constraints, interface sketches,
pseudocode where non-trivial, edge cases, and acceptance criteria — not
"implement feature X"; §7 says local agents may challenge but must not
silently redesign a MUST-level requirement. A Work Package entry in
`docs/WORK_PACKAGES.md` is deliberately a lighter-weight *scope card*, not
that full artifact — so treating "expand the scope card" as something the
same agent that will then implement it can just do on its own collapses
exactly the boundary those sections exist to keep.

**The correct sequence:**

```text
scope card (docs/WORK_PACKAGES.md)
        ↓
Principal-capable session expands it into a full
Engineering Work Package per AGENTS.md §6
        ↓
EWP is committed to the milestone branch, with its base SHA,
as its own commit — this is a checkpoint, not a draft
        ↓
(systemic/architectural/security-sensitive WPs: review/approval
of the EWP itself before implementation starts — see
"Per-WP checkpoints and review" below)
        ↓
implementation session(s) implement against the committed,
frozen EWP — not against the scope card directly
        ↓
if the implementer finds the EWP's assumption false or its
design contradicted by something real, it escalates/amends the
EWP explicitly (a new committed revision) — it does not
silently redesign and implement something else
```

The same underlying agent/session may fill both the Principal-expansion
role and the implementation role back to back — nothing here requires two
different providers — but the **role transition must be explicit**: the
EWP gets committed and frozen as its own artifact before implementation
code is written, so a handoff mid-WP always has a real design document to
resume against, not just a scope card and whatever the previous session
happened to be thinking.

## The handoff file

A single file, **`HANDOFF.md`, at the repository root of the working
branch** (not `docs/` — it's temporary, not normative documentation), is
the resumption point for the next agent. It is:

- **Committed to the branch** alongside the code it describes, so it
  travels with `git fetch`/`git checkout`, not left in any one session's
  memory.
- **Updated, not appended to forever** — it describes current state, not a
  chronological log. (Git history is the log; this file is a snapshot.)
- **Updated at every durable checkpoint, not deferred to end-of-session.**
  The failure mode this protocol exists for is a session becoming
  inaccessible *without* a graceful end-of-session phase — a rule that
  only updates the handoff file "near the end" doesn't survive the exact
  event it's meant to survive. Whenever a commit materially changes WP
  status, verified commands, the next action, a known blocker, or an
  interface decision, update `HANDOFF.md` in that same checkpoint or
  immediately after, and push both together. It does not need updating for
  every single edit — just every point where losing "since the last
  update" would actually hurt.
- **Never contains secrets, tokens, raw credential values, private
  provider-session data, or large raw logs** — it is intentionally
  committed and pushed. Reference artifacts/commits/ledger entries
  instead, never paste sensitive content into it.
- **Has no authority over `AGENTS.md`, accepted ADRs, invariants, or a
  committed EWP's acceptance criteria.** It is status/evidence metadata,
  not a decision record. If a previous session wrote something like "we
  decided to change X, continue this way" without a corresponding
  committed EWP/ADR change backing it, the next agent treats that as a
  *claim* to verify or escalate — never as standing authority to act on.
- **Kept through review and repair; removed *before* forming the final
  closure candidate, never after freeze.** Review and repair
  (`docs/REVIEW_AND_CONVERGENCE.md`) can itself span multiple
  quota-limited sessions — a milestone candidate reaching "ready for
  review" is not the end of multi-session work, closure/freeze is. But a
  `ReviewCampaign` freezes an *exact* candidate commit, and the commit
  merged must be the exact commit closure reviewed and froze — so the
  deletion has to happen *before* that final candidate is formed, not as
  cleanup afterward (which would create a new, unreviewed commit and break
  that identity). The lifecycle is:

  ```text
  implementation → per-WP checkpoints → milestone candidate
    → broad review → repairs / focused revalidation (HANDOFF.md present
      throughout — it may need to come back if closure reopens the
      candidate and another repair round starts)
    → prepare the final closure candidate:
        - delete HANDOFF.md
        - finalize any EWP keep/archive decisions (see "Work Package
          boundaries" below)
    → deterministic validation on that candidate
    → closure review
    → FROZEN at that exact SHA
    → merge that exact SHA — no cleanup commit occurs after freeze
  ```

  It must never reach `main` either way.

### Handoff file template

```markdown
# Handoff — <milestone/branch name>

Last updated: <UTC timestamp> by <session/provider identifier, e.g.
"Claude Code / Sonnet 5" or "session run on <provider>">

Session takeover HEAD: `<sha>` (remote HEAD when this session started)
Expected remote HEAD before next push: `<sha>` (advances after every
successful push this session makes — see "Git safety rules")

## Milestone
<Milestone ID and one-line goal, e.g. "M3B — guided bootstrap and onboarding">
See docs/WORK_PACKAGES.md#<milestone> for the full Work Package breakdown.

## Work Package status

| WP | Status | Checkpoint | Validation | Review |
|----|--------|------------|------------|--------|
| WP-M3B-1 | accepted | `<sha>` | `go test ./... ` PASS | correctness review complete |
| WP-M3B-2 | in progress | — | — | — |
| WP-M3B-3 | not started | — | — | blocked on WP-M3B-2 |
| ... | | | | |

(Never write "merged" for a WP checkpoint — nothing is merged to `main`
until the whole milestone closes. "accepted at checkpoint `<sha>`" is the
correct phrasing.)

## Currently in progress: <WP ID>

- **EWP status:** <not yet expanded / expanded and committed at `<sha>` /
  amended at `<sha>` because ...>
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

1. `git fetch origin <branch>` and check out the branch. Record the fetched
   `HEAD` SHA as this session's own "Session takeover HEAD" / initial
   "Expected remote HEAD" — this replaces whatever the previous session
   left in those fields, since it's now *this* session's baseline to
   compare future fetches against.
2. **Do not trust this file blindly** — run `go build ./... && go test
   ./...` (or the narrower scope relevant to the in-progress WP) and
   confirm the actual state matches what's claimed above before doing
   anything else. If it doesn't match, fix the discrepancy in this file
   first.
3. Check whether `Last updated` is recent enough that another session
   might still be active; if in doubt, ask the human before proceeding.
4. Read this WP's entry in `docs/WORK_PACKAGES.md`, and its committed EWP
   if one exists, in full before writing code — this handoff file is a
   status snapshot, not a substitute for either.
5. Continue from "Next concrete action" above.
6. At the next durable checkpoint (not just "before the session ends"),
   update this file again — including advancing "Expected remote HEAD" to
   match, per "Git safety rules" — and push it together with that checkpoint's
   commit.
```

## Per-WP checkpoints and review

Deferring all review to one giant end-of-milestone diff lets a bad early
interface decision propagate through every later WP before anyone catches
it. Instead, when a WP is marked done, record an immutable checkpoint (the
table above) with: the EWP revision it implemented, base SHA, completion
SHA, exact deterministic validation run, acceptance-criteria result, and
required review disposition. Later WPs that depend on it start from that
accepted checkpoint, not from an unreviewed one.

Security-sensitive WPs in particular (e.g. WP-M3B-4's credential-reference
abstraction) get their `CONTRIBUTING.md`-required threat-model review at
their own checkpoint, before later WPs build on that contract — not
deferred to the final milestone review, by which point the cost of a
finding is much higher.

The milestone PR still gets a final integration/closure review once every
WP is accepted, per `docs/REVIEW_AND_CONVERGENCE.md`'s normal campaign —
per-WP checkpoints reduce what that final review has to catch, they don't
replace it.

## Work Package boundaries

Work Packages exist to make quota cutoffs and review both cheap, not just
to organize the milestone conceptually. When splitting a milestone
(`docs/WORK_PACKAGES.md` is where this is recorded per-milestone), prefer
boundaries where:

- each WP is independently buildable and testable at its own final commit,
  even if later WPs aren't started yet;
- each WP has exactly one owner for any given public surface (a service/
  domain layer and the CLI wiring that exposes it are different WPs if
  that avoids two WPs both claiming to deliver the same command);
- a WP is small enough that losing "the rest of this WP" to a quota
  cutoff is an acceptable, cheaply-redone loss — if a WP feels like it
  would take one agent session's entire budget, split it further; the
  actual per-WP size still needs case-by-case judgment against the budget
  available;
- dependencies between WPs are explicit and linear, so a new agent doesn't
  have to reconstruct which WPs block which from scratch, and "no
  dependency between these WPs" is recorded as "may be done in either
  order" — never as "may be done concurrently" (see "Concurrency model"
  above).

A WP entry in `docs/WORK_PACKAGES.md` stays a compact scope card
permanently — it is not replaced by the full EWP. The full EWP a Principal-
capable session expands it into (per "Principal/Implementer separation"
above) lives as its own linked file per WP (e.g.
`docs/work-packages/wp-m3b-4-ewp.md`), committed once frozen. At milestone
closure, decide deliberately whether each WP's detailed EWP is kept
permanently for engineering provenance or archived — don't let
`docs/WORK_PACKAGES.md` itself silently grow into an implementation
archive; Git history is available for archaeology either way.

## What does not change

Everything else in `AGENTS.md`, `CONTRIBUTING.md`, and
`docs/REVIEW_AND_CONVERGENCE.md` still applies unmodified: deterministic
evidence over claims, documentation staying synchronized with behavior,
security-sensitive changes needing a threat-model review, and so on. This
protocol only adds the discipline needed to survive a mid-milestone quota
cutoff — it does not relax anything else, and where an earlier version of
this document read as if it did (see "Revision history"), that was a
mistake this revision corrects.
