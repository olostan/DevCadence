// Package tools implements bounded, deterministic execution-agent capabilities
// (docs/LOCAL_AGENTS.md, ADR-0015, ADR-0016).
//
// These tools are execution primitives for Repository Scout and Implementer
// roles in isolated worktrees. They are not the Principal's primary interface
// (DCI-001, DCI-010).
package tools

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
)

// Scope defines the boundary within which an execution tool operates.
type Scope struct {
	ProjectID    string
	WorktreeID   string
	WorktreePath string // Canonical absolute path to worktree root
	ModuleID     string // Optional module ID within monorepo
	ModulePath   string // Optional repository-relative module path
}

// ResolvePath ensures that relPath is strictly contained within the active scope,
// evaluating symlinks and rejecting parent traversal escapes (ADR-0015).
func (s Scope) ResolvePath(relPath string) (string, error) {
	if s.WorktreePath == "" {
		return "", errs.New(errs.CategoryInvalidArgument, "scope: worktree path is required")
	}
	resolvedWorktree, err := filepath.EvalSymlinks(s.WorktreePath)
	if err != nil {
		return "", errs.Wrap(errs.CategoryInvalidArgument, err, "scope: resolve worktree %q", s.WorktreePath)
	}

	baseDir := resolvedWorktree
	if s.ModulePath != "" {
		baseDir = filepath.Join(resolvedWorktree, s.ModulePath)
	}

	target := filepath.Clean(filepath.Join(baseDir, relPath))
	relFromWorktree, err := filepath.Rel(resolvedWorktree, target)
	if err != nil || strings.HasPrefix(relFromWorktree, "..") {
		return "", errs.New(errs.CategoryPolicyDenied, "scope: path %q escapes worktree %q", relPath, s.WorktreePath)
	}

	// If file or dir exists, verify symlink resolution does not escape either
	if info, err := os.Lstat(target); err == nil {
		var resolvedTarget string
		if info.Mode()&os.ModeSymlink != 0 {
			resolvedTarget, err = filepath.EvalSymlinks(target)
			if err != nil {
				return "", errs.Wrap(errs.CategoryInvalidArgument, err, "scope: resolve symlink %q", target)
			}
			relTarget, err := filepath.Rel(resolvedWorktree, resolvedTarget)
			if err != nil || strings.HasPrefix(relTarget, "..") {
				return "", errs.New(errs.CategoryPolicyDenied, "scope: symlink %q escapes worktree %q", relPath, s.WorktreePath)
			}
			return resolvedTarget, nil
		}
	}

	return target, nil
}
