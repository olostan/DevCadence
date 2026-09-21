package repository

import (
	"context"
	"os"
	"strings"

	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/process"
)

// MergeCheck is the deterministic result of testing whether head can be
// combined with base without changing the accepted repository
// (docs/IMPLEMENTATION_PLAN.md M2 §19, ARCHITECTURE.md §11).
type MergeCheck struct {
	// FastForward reports whether base is an ancestor of head, i.e.
	// integrating head requires no merge commit at all.
	FastForward bool
	// Clean reports whether a merge of head into base produces no conflicts.
	// It is meaningless (false) when FastForward is true; callers should
	// check FastForward first.
	Clean bool
	// ConflictingPaths lists paths with unmerged (conflict) status, present
	// only when Clean is false.
	ConflictingPaths []string
}

// CheckMerge determines whether head can be integrated onto base, using a
// disposable detached worktree so the check never touches the accepted
// working tree or any tracked branch (ARCHITECTURE.md §11: "Parallel tasks
// cannot mutate one shared working directory", extended here to "checking
// integration must not mutate the accepted tree either").
//
// The temporary worktree is created and removed within this call; it is
// never registered with the worktree manager and never visible through
// worktree listing, because it represents no attempt.
func (r *Repository) CheckMerge(ctx context.Context, base, head string) (MergeCheck, error) {
	if base == "" || head == "" {
		return MergeCheck{}, errs.New(errs.CategoryInvalidArgument, "repository: base and head commits are required")
	}
	ff, err := r.IsAncestor(ctx, base, head)
	if err != nil {
		return MergeCheck{}, err
	}
	if ff {
		return MergeCheck{FastForward: true}, nil
	}

	tmpDir, err := os.MkdirTemp("", "devcadience-mergecheck-*")
	if err != nil {
		return MergeCheck{}, errs.Wrap(errs.CategoryInternal, err, "repository: create merge-check scratch dir")
	}
	defer os.RemoveAll(tmpDir)

	if _, err := r.git(ctx, "worktree", "add", "--detach", "--quiet", tmpDir, base); err != nil {
		return MergeCheck{}, errs.Wrap(errs.CategoryInternal, err, "repository: create merge-check worktree")
	}
	defer func() {
		_, _ = r.git(ctx, "worktree", "remove", "--force", tmpDir)
	}()

	runInTmp := func(args ...string) (process.Result, error) {
		return r.runner.Run(ctx, process.Spec{
			Executable: "git",
			Args:       args,
			Dir:        tmpDir,
			Env:        r.env,
			Timeout:    DefaultGitTimeout,
		})
	}

	mergeRes, err := runInTmp("merge", "--no-commit", "--no-ff", "--quiet", head)
	if err != nil {
		return MergeCheck{}, err
	}
	if mergeRes.Success() {
		return MergeCheck{Clean: true}, nil
	}

	statusRes, err := runInTmp("diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return MergeCheck{}, err
	}
	var conflicts []string
	for _, line := range strings.Split(strings.TrimSpace(string(statusRes.Stdout)), "\n") {
		if line != "" {
			conflicts = append(conflicts, line)
		}
	}
	// Restore the scratch worktree to a clean state before it is removed;
	// failing to abort is not fatal (the directory is discarded either way)
	// but keeps the operation's intent explicit and its logs unsurprising.
	_, _ = runInTmp("merge", "--abort")

	return MergeCheck{Clean: false, ConflictingPaths: conflicts}, nil
}

// StaleBase reports whether a candidate's recorded base commit is no longer
// the accepted commit, i.e. the accepted history has advanced since the
// candidate's attempt was planned (docs/IMPLEMENTATION_PLAN.md M2 §18,
// docs/PROJECT_STATE.md §7). It never rebases or otherwise mutates
// anything; it only answers the question, so the caller decides what policy
// to apply to a stale candidate.
func StaleBase(candidateBase, currentAccepted string) bool {
	return candidateBase != "" && currentAccepted != "" && candidateBase != currentAccepted
}
