package reviewexec

import (
	"context"
	"strings"

	"github.com/olostan/DevCadence/internal/principal/facade"
	"github.com/olostan/DevCadence/internal/worktrees"
)

// Ensure Executor implements facade.ReviewExecutor.
var _ facade.ReviewExecutor = (*Executor)(nil)

// Executor coordinates bounded independent review execution.
type Executor struct {
	opts Options
}

// New constructs and initializes a new ReviewExecutor.
// It validates required options, ensures independence basis is supported,
// and cleans up clean orphan rv-* worktrees on startup (retaining dirty ones).
func New(opts Options) (*Executor, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	e := &Executor{opts: opts}
	e.cleanupOrphanWorktrees(context.Background())
	return e, nil
}

// cleanupOrphanWorktrees removes clean orphan rv-* worktrees and retains dirty ones.
// It does not touch durable intent records in the control plane.
func (e *Executor) cleanupOrphanWorktrees(ctx context.Context) {
	repo, err := e.opts.Repositories.Repository(ctx, e.opts.ProjectID)
	if err != nil {
		return
	}
	wts, err := e.opts.Worktrees.List(e.opts.ProjectID)
	if err != nil {
		return
	}
	for _, wt := range wts {
		if isReviewWorktree(wt) {
			if wt.Status == worktrees.StatusActive {
				_ = e.opts.Worktrees.Cleanup(ctx, repo, e.opts.ProjectID, wt.ID, worktrees.CleanupOptions{Force: false})
			}
		}
	}
}

func isReviewWorktree(wt *worktrees.Worktree) bool {
	return strings.HasPrefix(wt.AttemptID, "rv-") ||
		strings.HasPrefix(wt.TaskID, "rv-") ||
		strings.HasPrefix(wt.ID, "rv-") ||
		strings.Contains(wt.ID, "/rv-")
}
