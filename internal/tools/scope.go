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

	// Verify symlink resolution across the entire target path and its ancestors
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err == nil {
		relTarget, err := filepath.Rel(resolvedWorktree, resolvedTarget)
		if err != nil || strings.HasPrefix(relTarget, "..") {
			return "", errs.New(errs.CategoryPolicyDenied, "scope: path %q resolves to %q which escapes worktree %q", relPath, resolvedTarget, s.WorktreePath)
		}
		return resolvedTarget, nil
	} else if !os.IsNotExist(err) {
		return "", errs.Wrap(errs.CategoryInvalidArgument, err, "scope: eval symlinks %q", target)
	}

	// Target does not exist yet: verify existing ancestor directory does not escape
	curr := filepath.Dir(target)
	for {
		if resolvedAncestor, err := filepath.EvalSymlinks(curr); err == nil {
			relAncestor, err := filepath.Rel(resolvedWorktree, resolvedAncestor)
			if err != nil || strings.HasPrefix(relAncestor, "..") {
				return "", errs.New(errs.CategoryPolicyDenied, "scope: ancestor %q of %q escapes worktree %q", curr, relPath, s.WorktreePath)
			}
			break
		}
		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}
		curr = parent
	}

	return target, nil
}
