# ADR-0007: Repository identity, worktree ownership and integration-check semantics

## Status
Accepted (M2).

## Context

M2 (docs/IMPLEMENTATION_PLAN.md) is the first milestone that touches a real
Git repository and creates isolated worktrees for attempts. DCI-030 requires
autonomous work to be isolated in worktrees based on an explicit base commit;
DCI-034 requires parallel work to never share a mutable working tree;
docs/SECURITY.md §6 requires canonical paths, no symlink escape, and no
writes into another attempt's worktree. None of that is safe by convention
alone — it has to be structural.

Three questions needed a durable answer before implementation:

1. What does it mean for a directory to be "the repository", and what must
   registration refuse?
2. Who owns a worktree's lifecycle metadata, and where does that state live?
3. How does the system learn whether two commits can be integrated without
   mutating the accepted working tree to find out?

## Decision

### Repository identity (`internal/repository`)

Registration resolves the caller's path through `filepath.EvalSymlinks`
*before* any check runs, then requires the canonical path to be exactly a Git
working tree's top level (`git rev-parse --show-toplevel`), matching
`--git-dir` against `--git-common-dir` to refuse a linked worktree, and
refusing a bare repository unless the caller explicitly opts in. A
repository is therefore never "the directory that happened to contain a
`.git`" and never "whatever a relative path or symlink happened to point at
today" — it is one canonical, validated root, checked once, and every
subsequent Git command this package issues is scoped to that root with `-C`,
never to process cwd.

### Worktree ownership (`internal/worktrees`)

A worktree's path and branch are pure functions of validated identifiers —
`<root>/<project>/<task>/<attempt>` and
`devcadence/<task>/<attempt>` — never of caller-supplied path fragments.
Task/attempt identifiers are checked against a restrictive character class
before they ever reach a path, so "../" or an absolute path disguised as an
identifier cannot escape the worktree root. Two calls with the same
identifiers always name the same worktree; different identifiers can never
collide. `Create` never implicitly reuses an existing worktree for an
attempt.

Ownership metadata (project/task/attempt/base commit/branch/path/status) is
tracked in a JSON manifest file per project under the worktree root, written
atomically (temp file + rename), rather than in a new SQLite table.

This was the one real alternative worth naming: a `worktrees` table would let
worktree state participate in the same transactions as task/attempt state.
The manifest was chosen instead because:

- worktrees are fundamentally filesystem state (a directory either exists on
  disk or it does not), and a database row can drift from that fact in a way
  a JSON manifest reconciled against `git worktree list --porcelain` cannot
  hide as easily;
- M2's exit criterion is repository-independent safety, not
  control-plane-integrated scheduling — that integration (which task/attempt
  a worktree serves, surfaced through `Attempt.WorktreeID`) already exists in
  M1's schema and does not require worktree *lifecycle* bookkeeping to live
  in the same store;
- adding a migration and relational schema for worktree lifecycle now would
  be schema surface M3's actual scheduling requirements might not want, and
  ENGINEERING_STANDARDS.md §1 asks for boundaries created when the milestone
  that needs them arrives, not speculatively.

The manifest is per-project and mutated under a per-project in-process mutex,
matching ENGINEERING_STANDARDS.md §21: serialisation is narrow (one
project's worktree operations), not global. This is safe for one daemon
process. It is **not** safe against two separate OS processes racing writes
to the same manifest file — the bootstrap posture (docs/SECURITY.md §17: "a
single-user, local-only daemon") does not require that, and it is recorded
here as deferred debt rather than solved with a cross-process file lock that
nothing yet needs.

`Cleanup` refuses a dirty worktree unless the caller passes `Force`, and
refuses a worktree whose directory has disappeared (crash, external
deletion, manual `git worktree remove`) with an explicit "requires recovery"
error rather than silently treating it as already gone. `Recover` requires
the caller to say what happened — the directory still exists (adopt it back
to active) or is confirmed gone (close the manifest entry) — rather than
guessing or defaulting to `git worktree prune`, which docs/IMPLEMENTATION_PLAN.md
M2 §8 explicitly asks M2 not to reach for as the default recovery mechanism.
`LeakedGitWorktrees` cross-checks the manifest against Git's own worktree
list and reports the difference for inspection; it never prunes on its own.

### Integration checks never mutate the accepted tree (`internal/repository`)

Fast-forward, clean-merge and conflict-detection facts are established using
a disposable, detached worktree created and destroyed within one call
(`Repository.CheckMerge`), never by merging into a tracked branch. The
accepted working tree is asserted clean before and after `CheckMerge` runs in
the test suite specifically because ARCHITECTURE.md §11 requires that
"parallel tasks cannot mutate one shared working directory" — extended here
to mean checking integration feasibility must not mutate the accepted tree
either. The temporary worktree is never registered with the worktree manager
and never appears in `List`: it represents no attempt.

Staleness (`StaleBase` / `Worktree.IsStale`) is a plain equality check
between a candidate's recorded base commit and the currently accepted
commit. It is deliberately not a rebase, a fast-forward attempt, or any
other mutation — M2 answers the question; policy for what to do about a
stale candidate belongs to the caller (eventually M7/M10 orchestration).

## Consequences

- A caller can never obtain a `*repository.Repository` for a path that is
  not a validated, canonical, non-bare Git working tree root.
- A caller can never obtain a worktree path that collides with another
  attempt's, and cannot construct one by hand — only `Manager.Create`
  produces them.
- Recovering from a crashed or externally deleted worktree is an explicit,
  inspectable, two-step operation, never an automatic destructive prune.
- Integration feasibility can be checked repeatedly and concurrently without
  ever risking the accepted branch.
- Cross-process worktree-manifest concurrency is out of scope for M2 and
  must be revisited before DevCadence runs more than one daemon process
  against the same project.

## Alternatives considered

- **A SQLite `worktrees` table.** Rejected for M2 as above; the manifest is
  additive and does not preclude a later migration if M3's scheduler needs
  transactional worktree state.
- **`git merge-tree` for conflict detection**, which needs no scratch
  worktree at all. Rejected because its output format is not a stable
  machine format across the Git versions DevCadence must support, and
  docs/IMPLEMENTATION_PLAN.md M2 explicitly prefers a temporary/integration
  worktree over parsing Git's less stable surfaces.
- **`git worktree prune` as the default recovery path.** Rejected per
  docs/IMPLEMENTATION_PLAN.md M2 §8's explicit instruction not to solve every
  recovery scenario that way.
