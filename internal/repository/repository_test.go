package repository_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/repository"
	"github.com/olostan/DevCadence/internal/testsupport"
)

func TestRegisterValidRepository(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	repo, err := repository.Register(context.Background(), "proj-a", fixture.Path, repository.Options{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if repo.Path != fixture.Path {
		t.Fatalf("path = %s, want %s", repo.Path, fixture.Path)
	}
	if repo.DefaultBranch != "main" {
		t.Fatalf("default branch = %s", repo.DefaultBranch)
	}
}

func TestRegisterNonexistentPath(t *testing.T) {
	_, err := repository.Register(context.Background(), "proj-a", "/no/such/path/xyz", repository.Options{})
	if errs.CategoryOf(err) != errs.CategoryNotFound {
		t.Fatalf("category = %v", errs.CategoryOf(err))
	}
}

func TestRegisterNonGitDirectory(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	_, err = repository.Register(context.Background(), "proj-a", dir, repository.Options{})
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category = %v", errs.CategoryOf(err))
	}
}

func TestRegisterRelativePathRejected(t *testing.T) {
	_, err := repository.Register(context.Background(), "proj-a", "relative/path", repository.Options{})
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category = %v", errs.CategoryOf(err))
	}
}

func TestRegisterSubdirectoryOfRepositoryRejected(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	fixture.WriteFile("sub/keep.txt", "x")
	fixture.Commit("add sub")
	sub := filepath.Join(fixture.Path, "sub")
	_, err := repository.Register(context.Background(), "proj-a", sub, repository.Options{})
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category = %v", errs.CategoryOf(err))
	}
}

func TestRegisterBareRepositoryRefusedByDefault(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	fixture := testsupport.NewGitRepo(t)
	fixture.Git("clone", "--quiet", "--bare", fixture.Path, dir)
	_, err = repository.Register(context.Background(), "proj-a", dir, repository.Options{})
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category = %v", errs.CategoryOf(err))
	}
}

func TestRegisterSymlinkedPathCanonicalises(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	linkParent := t.TempDir()
	link := filepath.Join(linkParent, "link")
	if err := os.Symlink(fixture.Path, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	repo, err := repository.Register(context.Background(), "proj-a", link, repository.Options{})
	if err != nil {
		t.Fatalf("register via symlink: %v", err)
	}
	if repo.Path != fixture.Path {
		t.Fatalf("canonical path = %s, want %s", repo.Path, fixture.Path)
	}
}

func TestInspectCleanAndDirty(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	repo, err := repository.Register(context.Background(), "proj-a", fixture.Path, repository.Options{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	facts, err := repo.Inspect(context.Background())
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if facts.Dirty {
		t.Fatal("expected clean tree")
	}
	if facts.HeadCommit != fixture.Head() {
		t.Fatalf("head = %s, want %s", facts.HeadCommit, fixture.Head())
	}
	if facts.Branch != "main" {
		t.Fatalf("branch = %s", facts.Branch)
	}

	fixture.WriteFile("dirty.txt", "uncommitted")
	facts, err = repo.Inspect(context.Background())
	if err != nil {
		t.Fatalf("inspect after write: %v", err)
	}
	if !facts.Dirty {
		t.Fatal("expected dirty tree")
	}
	if len(facts.Status) != 1 || facts.Status[0].Path != "dirty.txt" {
		t.Fatalf("status = %+v", facts.Status)
	}
}

func TestCommitExistsAndAncestry(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	base := fixture.Head()
	fixture.WriteFile("a.txt", "a")
	head := fixture.Commit("add a")

	repo, err := repository.Register(context.Background(), "proj-a", fixture.Path, repository.Options{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	ok, err := repo.CommitExists(context.Background(), head)
	if err != nil || !ok {
		t.Fatalf("commit exists: ok=%v err=%v", ok, err)
	}
	ok, err = repo.CommitExists(context.Background(), "0000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("commit exists (missing): %v", err)
	}
	if ok {
		t.Fatal("expected missing commit to report false")
	}
	isAncestor, err := repo.IsAncestor(context.Background(), base, head)
	if err != nil || !isAncestor {
		t.Fatalf("is ancestor: %v err=%v", isAncestor, err)
	}
}

// TestRevisionArgumentInjectionRejected proves a revision value shaped like
// a Git option (starting with "-") is refused rather than passed straight
// through to `git`, where it could be interpreted as an option instead of a
// revision (docs/SECURITY.md §6).
func TestRevisionArgumentInjectionRejected(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	repo, err := repository.Register(context.Background(), "proj-a", fixture.Path, repository.Options{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	head := fixture.Head()
	malicious := "--output=/tmp/pwned"

	if _, err := repo.CommitExists(context.Background(), malicious); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("CommitExists category = %v", errs.CategoryOf(err))
	}
	if _, err := repo.IsAncestor(context.Background(), malicious, head); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("IsAncestor(ancestor) category = %v", errs.CategoryOf(err))
	}
	if _, err := repo.IsAncestor(context.Background(), head, malicious); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("IsAncestor(descendant) category = %v", errs.CategoryOf(err))
	}
	if _, err := repo.MergeBase(context.Background(), malicious, head); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("MergeBase(a) category = %v", errs.CategoryOf(err))
	}
	if _, err := repo.Diff(context.Background(), malicious, head); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("Diff(base) category = %v", errs.CategoryOf(err))
	}
	if _, err := repo.ResolveCommit(context.Background(), malicious); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("ResolveCommit category = %v", errs.CategoryOf(err))
	}
}

// TestResolveCommitCanonicalisesMovingRef proves ResolveCommit turns a
// moving ref such as a branch name into the full canonical SHA it currently
// names, rather than a caller being able to store the ref text itself as a
// durable base commit (docs/IMPLEMENTATION_PLAN.md M2 §18).
func TestResolveCommitCanonicalisesMovingRef(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	repo, err := repository.Register(context.Background(), "proj-a", fixture.Path, repository.Options{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	head := fixture.Head()

	resolved, err := repo.ResolveCommit(context.Background(), "main")
	if err != nil {
		t.Fatalf("resolve main: %v", err)
	}
	if resolved != head {
		t.Fatalf("resolved = %s, want %s", resolved, head)
	}
	if resolved == "main" {
		t.Fatal("ResolveCommit must not return the ref text itself")
	}

	if _, err := repo.ResolveCommit(context.Background(), "no-such-ref"); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category for unresolvable ref = %v", errs.CategoryOf(err))
	}
}

// TestInspectStatusHandlesPathsWithSpaces proves porcelain v2's NUL-
// delimited (`-z`) form is used to parse status output, so a path
// containing spaces is reported whole rather than truncated to its last
// whitespace-separated word.
func TestInspectStatusHandlesPathsWithSpaces(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	repo, err := repository.Register(context.Background(), "proj-a", fixture.Path, repository.Options{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	fixture.WriteFile("dir with spaces/file name.txt", "original")
	fixture.Commit("add a spaced path")
	fixture.WriteFile("dir with spaces/file name.txt", "modified")
	facts, err := repo.Inspect(context.Background())
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if !facts.Dirty {
		t.Fatal("expected dirty tree")
	}
	found := false
	for _, s := range facts.Status {
		if s.Path == "dir with spaces/file name.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the full spaced path in status entries, got %+v", facts.Status)
	}
}

func TestDiffStat(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	base := fixture.Head()
	fixture.WriteFile("a.txt", "line1\nline2\n")
	head := fixture.Commit("add a")

	repo, err := repository.Register(context.Background(), "proj-a", fixture.Path, repository.Options{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	stat, err := repo.Diff(context.Background(), base, head)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(stat.ChangedPaths) != 1 || stat.ChangedPaths[0] != "a.txt" {
		t.Fatalf("changed paths = %v", stat.ChangedPaths)
	}
	if stat.Additions != 2 || stat.Deletions != 0 {
		t.Fatalf("additions=%d deletions=%d", stat.Additions, stat.Deletions)
	}
}

func TestCheckMergeFastForward(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	base := fixture.Head()
	fixture.WriteFile("a.txt", "a")
	head := fixture.Commit("add a")

	repo, err := repository.Register(context.Background(), "proj-a", fixture.Path, repository.Options{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	check, err := repo.CheckMerge(context.Background(), base, head)
	if err != nil {
		t.Fatalf("check merge: %v", err)
	}
	if !check.FastForward {
		t.Fatal("expected fast-forward")
	}
}

func TestCheckMergeCleanNonFastForward(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	base := fixture.Head()
	fixture.WriteFile("a.txt", "a")
	fixture.Commit("advance main")

	fixture.Git("checkout", "--quiet", "-b", "feature", base)
	fixture.WriteFile("b.txt", "b")
	head := fixture.Commit("add b on feature")
	fixture.Git("checkout", "--quiet", "main")

	repo, err := repository.Register(context.Background(), "proj-a", fixture.Path, repository.Options{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	check, err := repo.CheckMerge(context.Background(), fixture.Head(), head)
	if err != nil {
		t.Fatalf("check merge: %v", err)
	}
	if check.FastForward {
		t.Fatal("did not expect fast-forward")
	}
	if !check.Clean {
		t.Fatalf("expected clean merge, conflicts=%v", check.ConflictingPaths)
	}

	// The accepted working tree must be untouched by the check.
	facts, err := repo.Inspect(context.Background())
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if facts.Dirty {
		t.Fatal("accepted working tree was mutated by CheckMerge")
	}
}

func TestCheckMergeConflict(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	base := fixture.Head()
	fixture.WriteFile("shared.txt", "base\n")
	base = fixture.Commit("add shared")

	fixture.WriteFile("shared.txt", "main change\n")
	mainHead := fixture.Commit("change on main")

	fixture.Git("checkout", "--quiet", "-b", "feature", base)
	fixture.WriteFile("shared.txt", "feature change\n")
	featureHead := fixture.Commit("change on feature")
	fixture.Git("checkout", "--quiet", "main")

	repo, err := repository.Register(context.Background(), "proj-a", fixture.Path, repository.Options{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	check, err := repo.CheckMerge(context.Background(), mainHead, featureHead)
	if err != nil {
		t.Fatalf("check merge: %v", err)
	}
	if check.FastForward || check.Clean {
		t.Fatalf("expected conflict, got %+v", check)
	}
	if len(check.ConflictingPaths) != 1 || check.ConflictingPaths[0] != "shared.txt" {
		t.Fatalf("conflicting paths = %v", check.ConflictingPaths)
	}

	facts, err := repo.Inspect(context.Background())
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if facts.Dirty {
		t.Fatal("accepted working tree was mutated by CheckMerge")
	}
}

// TestIsAncestorNonexistentRevisionErrors proves IsAncestor distinguishes a
// real "not an ancestor" answer (git merge-base --is-ancestor exit 1) from
// an invalid-revision failure (any other nonzero exit, typically 128):
// collapsing both into (false, nil) would let a nonexistent commit silently
// read as an ordinary negative ancestry result.
func TestIsAncestorNonexistentRevisionErrors(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	head := fixture.Head()
	repo, err := repository.Register(context.Background(), "proj-a", fixture.Path, repository.Options{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	const nonexistent = "cafebabe0000000000000000000000000000000"

	if _, err := repo.IsAncestor(context.Background(), nonexistent, head); err == nil {
		t.Fatal("expected an error for a nonexistent ancestor revision, not a false ancestry result")
	}
	if _, err := repo.IsAncestor(context.Background(), head, nonexistent); err == nil {
		t.Fatal("expected an error for a nonexistent descendant revision, not a false ancestry result")
	}

	// A genuinely valid, non-ancestor pair must still report a plain false
	// with no error, so the fix does not turn every negative into an error.
	fixture.WriteFile("a.txt", "a")
	other := fixture.Commit("add a")
	fixture.Git("checkout", "--quiet", "-b", "diverged", head)
	fixture.WriteFile("b.txt", "b")
	diverged := fixture.Commit("diverge")
	isAncestor, err := repo.IsAncestor(context.Background(), other, diverged)
	if err != nil {
		t.Fatalf("is ancestor (diverged, valid): %v", err)
	}
	if isAncestor {
		t.Fatal("diverged commit must not report as an ancestor")
	}
}

// TestCheckMergeInvalidRevisionErrors proves CheckMerge refuses a
// nonexistent base or head with a clear error rather than letting it fall
// through IsAncestor's now-fixed error path into the merge/diff commands,
// where a failing `git worktree add` or an ambiguous downstream failure
// could otherwise be misread as a conflict result.
func TestCheckMergeInvalidRevisionErrors(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	head := fixture.Head()
	repo, err := repository.Register(context.Background(), "proj-a", fixture.Path, repository.Options{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	const nonexistent = "cafebabe0000000000000000000000000000000"

	if _, err := repo.CheckMerge(context.Background(), nonexistent, head); err == nil {
		t.Fatal("expected CheckMerge to error on a nonexistent base, not report a conflict")
	}
	if _, err := repo.CheckMerge(context.Background(), head, nonexistent); err == nil {
		t.Fatal("expected CheckMerge to error on a nonexistent head, not report a conflict")
	}

	// The accepted working tree must still be untouched after a refused
	// check.
	facts, err := repo.Inspect(context.Background())
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if facts.Dirty {
		t.Fatal("accepted working tree was mutated by a refused CheckMerge")
	}
}

func TestStaleBase(t *testing.T) {
	if repository.StaleBase("abc", "abc") {
		t.Fatal("same commit must not be stale")
	}
	if !repository.StaleBase("abc", "def") {
		t.Fatal("different commit must be stale")
	}
}
