// Package repository implements repository registration and deterministic
// Git inspection (docs/IMPLEMENTATION_PLAN.md M2, docs/SECURITY.md §6).
//
// It invokes the installed `git` executable through the controlled process
// runner rather than reimplementing Git, and reads only stable
// machine-oriented output (`--porcelain`, `--format`), never human-oriented
// text, so behaviour does not depend on the user's locale or Git's prose
// output changing between versions (DCI-041).
package repository

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/process"
)

// DefaultGitTimeout bounds an individual Git inspection command. Inspection
// commands are metadata reads, not builds, so a short bound is appropriate;
// callers needing longer (an initial clone, a huge diff) construct their own
// process.Spec through Runner's lower-level helpers.
const DefaultGitTimeout = 30 * time.Second

// Repository is a registered, validated Git working tree.
type Repository struct {
	ProjectID string
	// Path is the canonical (symlink-resolved, absolute) repository root, as
	// returned by `git rev-parse --show-toplevel`. Every Git command this
	// package issues is scoped to this path with `-C`, never inherited from
	// process cwd, so a caller cannot be redirected to a different tree by an
	// ambient working directory.
	Path string
	// DefaultBranch is the branch HEAD pointed at when the repository was
	// registered. It is informational; branches can move.
	DefaultBranch string

	runner *process.Runner
	env    []string
}

// Options configures registration.
type Options struct {
	Runner *process.Runner
	// Env is the environment Git subprocesses run with. BaseEnv() is used
	// when nil.
	Env []string
	// AllowBare permits registering a bare repository. M2 does not implement
	// worktree/candidate operations against a bare repository, so the
	// default is to refuse one with a clear error rather than accept it and
	// fail confusingly three calls later.
	AllowBare bool
}

// Register validates that path is a usable, non-bare Git working tree and
// returns a Repository bound to its canonical root.
//
// Registration deliberately refuses more than it accepts:
//   - the path must exist and be a directory;
//   - symlinks are resolved before any check runs, so a caller cannot be
//     told it registered one path while every subsequent command operates on
//     another (docs/SECURITY.md §6);
//   - the resolved path must be a Git working tree's top level exactly — a
//     subdirectory of a repository is refused rather than silently
//     "corrected" to the enclosing repository, and a linked worktree
//     (docs/SECURITY.md's "nested worktree" case) is refused because its
//     lifecycle belongs to the worktree manager, not to repository
//     registration;
//   - a bare repository is refused unless the caller opts in, because M2 has
//     no working tree to run deterministic checks or produce candidates in.
func Register(ctx context.Context, projectID, path string, opts Options) (*Repository, error) {
	if projectID == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "repository: project id is required")
	}
	if path == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "repository: path is required")
	}
	if !filepath.IsAbs(path) {
		return nil, errs.New(errs.CategoryInvalidArgument, "repository: path %q must be absolute", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryNotFound, err, "repository: path %q does not exist", path)
	}
	if !info.IsDir() {
		return nil, errs.New(errs.CategoryInvalidArgument, "repository: path %q is not a directory", path)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "repository: resolve symlinks for %q", path)
	}

	runner := opts.Runner
	if runner == nil {
		runner = process.NewRunner()
	}
	env := opts.Env
	if env == nil {
		env = process.BaseEnv()
	}
	repo := &Repository{ProjectID: projectID, Path: canonical, runner: runner, env: env}

	isBare, err := repo.git(ctx, "rev-parse", "--is-bare-repository")
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInvalidArgument, err,
			"repository: %q is not a Git repository", canonical)
	}
	if strings.TrimSpace(isBare) == "true" {
		if !opts.AllowBare {
			return nil, errs.New(errs.CategoryInvalidArgument,
				"repository: %q is a bare repository, which M2 does not support", canonical)
		}
	} else {
		inside, err := repo.git(ctx, "rev-parse", "--is-inside-work-tree")
		if err != nil || strings.TrimSpace(inside) != "true" {
			return nil, errs.New(errs.CategoryInvalidArgument,
				"repository: %q is not inside a Git working tree", canonical)
		}
		top, err := repo.git(ctx, "rev-parse", "--show-toplevel")
		if err != nil {
			return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "repository: resolve toplevel of %q", canonical)
		}
		topCanonical, err := filepath.EvalSymlinks(strings.TrimSpace(top))
		if err != nil {
			return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "repository: resolve toplevel symlinks")
		}
		if topCanonical != canonical {
			return nil, errs.New(errs.CategoryInvalidArgument,
				"repository: %q is not a repository root; its toplevel is %q. "+
					"Register the toplevel, not a subdirectory or a nested repository", canonical, topCanonical)
		}
		gitDirCommon, err := repo.git(ctx, "rev-parse", "--git-common-dir")
		if err != nil {
			return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "repository: resolve git-common-dir")
		}
		gitDir, err := repo.git(ctx, "rev-parse", "--git-dir")
		if err != nil {
			return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "repository: resolve git-dir")
		}
		// A linked worktree has a git-dir that is not its own common dir
		// (it lives under the main repository's .git/worktrees/<name>).
		// M2's own worktree manager creates and owns exactly these; a
		// repository registered directly at one would let two lifecycles
		// (worktree manager, repository registration) claim the same path.
		if strings.TrimSpace(gitDir) != strings.TrimSpace(gitDirCommon) {
			return nil, errs.New(errs.CategoryInvalidArgument,
				"repository: %q is a linked Git worktree, not a primary repository; "+
					"register the main repository instead", canonical)
		}
	}

	branch, err := repo.git(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err == nil {
		repo.DefaultBranch = strings.TrimSpace(branch)
	}
	return repo, nil
}

// git runs one Git subcommand scoped to this repository via `-C` and returns
// trimmed stdout. It is unexported: every fact this package exposes goes
// through a named method so callers cannot pass arbitrary Git flags (M2 is
// deterministic infrastructure, not a general Git command channel — see
// docs/IMPLEMENTATION_PLAN.md M2 §10).
func (r *Repository) git(ctx context.Context, args ...string) (string, error) {
	full := append([]string{"-C", r.Path}, args...)
	res, err := r.runner.Run(ctx, process.Spec{
		Executable: "git",
		Args:       full,
		Dir:        r.Path,
		Env:        r.env,
		Timeout:    DefaultGitTimeout,
	})
	if err != nil {
		return "", err
	}
	if !res.Success() {
		return "", errs.New(errs.CategoryInvalidArgument,
			"git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(res.Stderr)))
	}
	return string(res.Stdout), nil
}

// Facts is the deterministic Git state of a repository at inspection time.
type Facts struct {
	HeadCommit string
	Branch     string
	// Detached reports whether HEAD is not on a branch.
	Detached bool
	Dirty    bool
	Status   []StatusEntry
}

// StatusEntry is one line of `git status --porcelain=v2`, parsed enough to
// tell a caller what changed without re-parsing porcelain text themselves.
type StatusEntry struct {
	// Code is the raw two-letter XY status code from porcelain v2 ordinary
	// entries ("changed" lines), or a synthetic marker ("??" untracked, "UU"
	// style for unmerged) for the other record types.
	Code string
	Path string
}

// Inspect returns the repository's current deterministic facts.
func (r *Repository) Inspect(ctx context.Context) (Facts, error) {
	head, err := r.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return Facts{}, errs.Wrap(errs.CategoryInvalidArgument, err, "repository: resolve HEAD")
	}
	branchOut, err := r.git(ctx, "symbolic-ref", "--short", "-q", "HEAD")
	detached := err != nil
	branch := strings.TrimSpace(branchOut)

	statusOut, err := r.git(ctx, "status", "--porcelain=v2")
	if err != nil {
		return Facts{}, errs.Wrap(errs.CategoryInternal, err, "repository: status")
	}
	entries := parsePorcelainV2(statusOut)
	return Facts{
		HeadCommit: strings.TrimSpace(head),
		Branch:     branch,
		Detached:   detached,
		Dirty:      len(entries) > 0,
		Status:     entries,
	}, nil
}

func parsePorcelainV2(out string) []StatusEntry {
	var entries []StatusEntry
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "1", "2": // ordinary / renamed-or-copied change entries
			if len(fields) < 9 {
				continue
			}
			path := fields[len(fields)-1]
			entries = append(entries, StatusEntry{Code: fields[1], Path: path})
		case "u": // unmerged
			if len(fields) < 11 {
				continue
			}
			entries = append(entries, StatusEntry{Code: "UU", Path: fields[len(fields)-1]})
		case "?": // untracked
			entries = append(entries, StatusEntry{Code: "??", Path: strings.Join(fields[1:], " ")})
		case "!": // ignored
			// Ignored files do not make a tree dirty.
		}
	}
	return entries
}

// CommitExists reports whether sha names a reachable commit object.
func (r *Repository) CommitExists(ctx context.Context, sha string) (bool, error) {
	if sha == "" {
		return false, errs.New(errs.CategoryInvalidArgument, "repository: commit is required")
	}
	res, err := r.runner.Run(ctx, process.Spec{
		Executable: "git",
		Args:       []string{"-C", r.Path, "cat-file", "-e", sha + "^{commit}"},
		Dir:        r.Path,
		Env:        r.env,
		Timeout:    DefaultGitTimeout,
	})
	if err != nil {
		return false, err
	}
	return res.Success(), nil
}

// IsAncestor reports whether ancestor is an ancestor of (or equal to)
// descendant.
func (r *Repository) IsAncestor(ctx context.Context, ancestor, descendant string) (bool, error) {
	res, err := r.runner.Run(ctx, process.Spec{
		Executable: "git",
		Args:       []string{"-C", r.Path, "merge-base", "--is-ancestor", ancestor, descendant},
		Dir:        r.Path,
		Env:        r.env,
		Timeout:    DefaultGitTimeout,
	})
	if err != nil {
		return false, err
	}
	return res.Success(), nil
}

// MergeBase returns the merge base of a and b.
func (r *Repository) MergeBase(ctx context.Context, a, b string) (string, error) {
	out, err := r.git(ctx, "merge-base", a, b)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// DiffStat is the deterministic summary of a base..head comparison.
type DiffStat struct {
	ChangedPaths []string
	Additions    int
	Deletions    int
}

// Diff returns the changed-path/line-count summary between base and head.
// Binary files report additions/deletions of 0 with the path still listed
// (`git diff --numstat` reports "-" for binaries).
func (r *Repository) Diff(ctx context.Context, base, head string) (DiffStat, error) {
	out, err := r.git(ctx, "diff", "--numstat", base, head)
	if err != nil {
		return DiffStat{}, err
	}
	var stat DiffStat
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		stat.ChangedPaths = append(stat.ChangedPaths, fields[2])
		if a, err := strconv.Atoi(fields[0]); err == nil {
			stat.Additions += a
		}
		if d, err := strconv.Atoi(fields[1]); err == nil {
			stat.Deletions += d
		}
	}
	return stat, nil
}
