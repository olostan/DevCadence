package facade

import (
	"context"
	"sort"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/repository"
)

// GitObserver is the Git-backed RepositoryObserver. Every call asks Git now;
// nothing is cached, so a file edited or a commit made outside DevCadence is
// visible on the next call.
type GitObserver struct {
	Repo *repository.Repository
}

// NewGitObserver wraps a registered repository.
func NewGitObserver(repo *repository.Repository) (*GitObserver, error) {
	if repo == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "git observer: repository is required")
	}
	return &GitObserver{Repo: repo}, nil
}

// Drift implements RepositoryObserver. The changed set is the union of files
// that differ between base and the current HEAD and tracked files with
// uncommitted modifications. Untracked files are not drift: no recorded
// evidence can have described a file that did not exist in a commit.
func (g *GitObserver) Drift(ctx context.Context, base string, prefixes []string) (Drift, error) {
	facts, err := g.Repo.Inspect(ctx)
	if err != nil {
		return Drift{}, err
	}
	resolved, err := g.Repo.ResolveCommit(ctx, base)
	if err != nil {
		return Drift{}, err
	}
	changed := map[string]bool{}
	if resolved != facts.HeadCommit {
		diff, err := g.Repo.Diff(ctx, resolved, facts.HeadCommit)
		if err != nil {
			return Drift{}, err
		}
		for _, p := range diff.ChangedPaths {
			changed[p] = true
		}
	}
	for _, entry := range facts.Status {
		if strings.HasPrefix(entry.Code, "?") {
			continue
		}
		changed[entry.Path] = true
	}
	out := make([]string, 0, len(changed))
	for p := range changed {
		if matchesPrefix(p, prefixes) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return Drift{HeadCommit: facts.HeadCommit, Dirty: facts.Dirty, ChangedPaths: out}, nil
}

// matchesPrefix reports whether p is at or under any prefix; no prefixes, or
// the root ".", match everything.
func matchesPrefix(p string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, prefix := range prefixes {
		if prefix == "." {
			return true
		}
		prefix = strings.TrimSuffix(prefix, "/")
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}
