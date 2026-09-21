package worktrees_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/repository"
	"github.com/olostan/DevCadience/internal/testsupport"
	"github.com/olostan/DevCadience/internal/worktrees"
)

func newManager(t *testing.T) *worktrees.Manager {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	m, err := worktrees.NewManager(filepath.Join(root, "worktrees"), nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	return m
}

func registerRepo(t *testing.T, fixture *testsupport.GitRepo) *repository.Repository {
	t.Helper()
	repo, err := repository.Register(context.Background(), "proj-a", fixture.Path, repository.Options{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return repo
}

func TestCreateAndCleanup(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	repo := registerRepo(t, fixture)
	m := newManager(t)

	wt, err := m.Create(context.Background(), repo, worktrees.Spec{
		ProjectID: "proj-a", TaskID: "tsk1", AttemptID: "att1", BaseCommit: fixture.Head(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("worktree path missing: %v", err)
	}
	if err := m.Cleanup(context.Background(), repo, "proj-a", wt.ID, worktrees.CleanupOptions{}); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatalf("expected worktree removed, stat err = %v", err)
	}
	got, err := m.Get("proj-a", wt.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != worktrees.StatusRemoved {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestParallelWorktreesAreIsolated(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	repo := registerRepo(t, fixture)
	m := newManager(t)
	base := fixture.Head()

	wt1, err := m.Create(context.Background(), repo, worktrees.Spec{ProjectID: "proj-a", TaskID: "tsk1", AttemptID: "att1", BaseCommit: base})
	if err != nil {
		t.Fatalf("create 1: %v", err)
	}
	wt2, err := m.Create(context.Background(), repo, worktrees.Spec{ProjectID: "proj-a", TaskID: "tsk1", AttemptID: "att2", BaseCommit: base})
	if err != nil {
		t.Fatalf("create 2: %v", err)
	}
	if wt1.Path == wt2.Path || wt1.Branch == wt2.Branch {
		t.Fatalf("worktrees are not isolated: %+v vs %+v", wt1, wt2)
	}

	if err := os.WriteFile(filepath.Join(wt1.Path, "only-in-1.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write in wt1: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt2.Path, "only-in-1.txt")); !os.IsNotExist(err) {
		t.Fatal("write in worktree 1 leaked into worktree 2")
	}
}

func TestConcurrentCreateSameProject(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	repo := registerRepo(t, fixture)
	m := newManager(t)
	base := fixture.Head()

	const n = 5
	var wg sync.WaitGroup
	errsCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := m.Create(context.Background(), repo, worktrees.Spec{
				ProjectID: "proj-a", TaskID: "tsk1", AttemptID: "att" + string(rune('a'+i)), BaseCommit: base,
			})
			errsCh <- err
		}(i)
	}
	wg.Wait()
	close(errsCh)
	for err := range errsCh {
		if err != nil {
			t.Fatalf("concurrent create: %v", err)
		}
	}
	list, err := m.List("proj-a")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != n {
		t.Fatalf("len(list) = %d, want %d", len(list), n)
	}
}

func TestCreateRejectsDuplicateAttempt(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	repo := registerRepo(t, fixture)
	m := newManager(t)
	base := fixture.Head()

	if _, err := m.Create(context.Background(), repo, worktrees.Spec{ProjectID: "proj-a", TaskID: "tsk1", AttemptID: "att1", BaseCommit: base}); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err := m.Create(context.Background(), repo, worktrees.Spec{ProjectID: "proj-a", TaskID: "tsk1", AttemptID: "att1", BaseCommit: base})
	if errs.CategoryOf(err) != errs.CategoryConflict {
		t.Fatalf("category = %v", errs.CategoryOf(err))
	}
}

func TestCreateInvalidBaseCommit(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	repo := registerRepo(t, fixture)
	m := newManager(t)
	_, err := m.Create(context.Background(), repo, worktrees.Spec{
		ProjectID: "proj-a", TaskID: "tsk1", AttemptID: "att1", BaseCommit: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
	})
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category = %v", errs.CategoryOf(err))
	}
}

func TestCreateRejectsPathTraversalIdentifiers(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	repo := registerRepo(t, fixture)
	m := newManager(t)
	base := fixture.Head()
	_, err := m.Create(context.Background(), repo, worktrees.Spec{
		ProjectID: "proj-a", TaskID: "../../etc", AttemptID: "att1", BaseCommit: base,
	})
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category = %v", errs.CategoryOf(err))
	}
}

// TestCreateRejectsPathTraversalProjectID proves ProjectID gets the same
// path-safe validation task/attempt ids already get: an unvalidated
// ProjectID is interpolated straight into the worktree and manifest paths,
// so "../outside" would otherwise escape the manager's root
// (docs/SECURITY.md §6).
func TestCreateRejectsPathTraversalProjectID(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	repo := registerRepo(t, fixture)
	m := newManager(t)
	base := fixture.Head()
	_, err := m.Create(context.Background(), repo, worktrees.Spec{
		ProjectID: "../../etc", TaskID: "tsk1", AttemptID: "att1", BaseCommit: base,
	})
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category = %v", errs.CategoryOf(err))
	}
}

// TestGetListCleanupRecoverRejectPathTraversalProjectID proves the same
// ProjectID validation applies to every entry point that builds a path from
// a raw projectID, not only Create.
func TestGetListCleanupRecoverRejectPathTraversalProjectID(t *testing.T) {
	m := newManager(t)
	const traversal = "../../etc"
	if _, err := m.Get(traversal, "tsk1/att1"); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("Get category = %v", errs.CategoryOf(err))
	}
	if _, err := m.List(traversal); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("List category = %v", errs.CategoryOf(err))
	}
	if err := m.Cleanup(context.Background(), nil, traversal, "tsk1/att1", worktrees.CleanupOptions{}); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("Cleanup category = %v", errs.CategoryOf(err))
	}
	if err := m.Recover(context.Background(), nil, traversal, "tsk1/att1", true); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("Recover category = %v", errs.CategoryOf(err))
	}
}

// TestCreateResolvesBaseCommitToCanonicalSHA proves a moving ref (a branch
// name) is never stored verbatim as BaseCommit: StaleBase compares
// BaseCommit against an actual SHA, and a stored ref would compare
// incorrectly.
func TestCreateResolvesBaseCommitToCanonicalSHA(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	repo := registerRepo(t, fixture)
	m := newManager(t)

	wt, err := m.Create(context.Background(), repo, worktrees.Spec{
		ProjectID: "proj-a", TaskID: "tsk1", AttemptID: "att1", BaseCommit: "main",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if wt.BaseCommit != fixture.Head() {
		t.Fatalf("stored base commit = %q, want the resolved SHA %q", wt.BaseCommit, fixture.Head())
	}
}

func TestCleanupRefusesDirtyWithoutForce(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	repo := registerRepo(t, fixture)
	m := newManager(t)
	base := fixture.Head()
	wt, err := m.Create(context.Background(), repo, worktrees.Spec{ProjectID: "proj-a", TaskID: "tsk1", AttemptID: "att1", BaseCommit: base})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt.Path, "dirty.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	err = m.Cleanup(context.Background(), repo, "proj-a", wt.ID, worktrees.CleanupOptions{})
	if errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Fatalf("category = %v", errs.CategoryOf(err))
	}
	if err := m.Cleanup(context.Background(), repo, "proj-a", wt.ID, worktrees.CleanupOptions{Force: true}); err != nil {
		t.Fatalf("forced cleanup: %v", err)
	}
}

func TestCleanupMissingWorktreeRequiresRecovery(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	repo := registerRepo(t, fixture)
	m := newManager(t)
	base := fixture.Head()
	wt, err := m.Create(context.Background(), repo, worktrees.Spec{ProjectID: "proj-a", TaskID: "tsk1", AttemptID: "att1", BaseCommit: base})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := os.RemoveAll(wt.Path); err != nil {
		t.Fatalf("simulate leak: %v", err)
	}
	err = m.Cleanup(context.Background(), repo, "proj-a", wt.ID, worktrees.CleanupOptions{})
	if errs.CategoryOf(err) != errs.CategoryIntegrity {
		t.Fatalf("category = %v", errs.CategoryOf(err))
	}
	got, err := m.Get("proj-a", wt.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != worktrees.StatusMissing {
		t.Fatalf("status = %s, want missing", got.Status)
	}

	if err := m.Recover(context.Background(), repo, "proj-a", wt.ID, true); err != nil {
		t.Fatalf("recover: %v", err)
	}
	got, err = m.Get("proj-a", wt.ID)
	if err != nil {
		t.Fatalf("get after recover: %v", err)
	}
	if got.Status != worktrees.StatusRemoved {
		t.Fatalf("status after recover = %s, want removed", got.Status)
	}
}

// TestRecoverRejectsDirectoryNotMatchingGitRegistration proves Recover does
// not adopt a present-but-wrong directory back to active: if the original
// worktree is gone and something else now occupies the recorded path,
// Git's own worktree list will not confirm the expected branch there, and
// Recover must refuse rather than hand back an unrelated directory as a
// valid attempt workspace.
func TestRecoverRejectsDirectoryNotMatchingGitRegistration(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	repo := registerRepo(t, fixture)
	m := newManager(t)
	base := fixture.Head()

	wt, err := m.Create(context.Background(), repo, worktrees.Spec{ProjectID: "proj-a", TaskID: "tsk1", AttemptID: "att1", BaseCommit: base})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Remove the worktree out from under the manager (both Git's
	// registration and the directory), then put an unrelated plain
	// directory back at the same path — simulating "something else now
	// occupies this path".
	fixture.Git("worktree", "remove", "--force", wt.Path)
	if err := os.MkdirAll(wt.Path, 0o700); err != nil {
		t.Fatalf("recreate unrelated dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt.Path, "not-a-worktree.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	err = m.Recover(context.Background(), repo, "proj-a", wt.ID, false)
	if errs.CategoryOf(err) != errs.CategoryIntegrity {
		t.Fatalf("expected Recover to refuse the mismatched directory with CategoryIntegrity, got category = %v (err=%v)",
			errs.CategoryOf(err), err)
	}
}

// TestRecoverAdoptsDirectoryMatchingGitRegistration is the positive
// counterpart: a directory that Git still genuinely registers as this
// worktree's branch is recovered back to active as before.
func TestRecoverAdoptsDirectoryMatchingGitRegistration(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	repo := registerRepo(t, fixture)
	m := newManager(t)
	base := fixture.Head()

	wt, err := m.Create(context.Background(), repo, worktrees.Spec{ProjectID: "proj-a", TaskID: "tsk1", AttemptID: "att1", BaseCommit: base})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := m.Recover(context.Background(), repo, "proj-a", wt.ID, false); err != nil {
		t.Fatalf("recover: %v", err)
	}
	got, err := m.Get("proj-a", wt.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != worktrees.StatusActive {
		t.Fatalf("status = %s, want active", got.Status)
	}
}

// TestRecoverPathExistsButNotDirectoryRejected proves a path that exists
// but is not a directory (something other than the worktree occupies it) is
// rejected outright rather than folded into either "present" or "gone".
func TestRecoverPathExistsButNotDirectoryRejected(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	repo := registerRepo(t, fixture)
	m := newManager(t)
	base := fixture.Head()

	wt, err := m.Create(context.Background(), repo, worktrees.Spec{ProjectID: "proj-a", TaskID: "tsk1", AttemptID: "att1", BaseCommit: base})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	fixture.Git("worktree", "remove", "--force", wt.Path)
	if err := os.WriteFile(wt.Path, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write regular file at worktree path: %v", err)
	}

	err = m.Recover(context.Background(), repo, "proj-a", wt.ID, false)
	if errs.CategoryOf(err) != errs.CategoryIntegrity {
		t.Fatalf("category = %v (err=%v)", errs.CategoryOf(err), err)
	}
}

// TestCreateRollsBackGitWorktreeOnManifestSaveFailure proves that when
// Create's final manifest save fails after Git has already registered the
// worktree and branch, Create rolls that Git-side registration back rather
// than leaving a live, unregistered worktree the manager's normal Cleanup
// path can never find (Cleanup only looks up worktrees the manifest already
// knows about).
//
// The failure is forced deterministically rather than through file
// permissions (which root, as tests run under here, bypasses): the manifest
// path itself is pre-created as a directory, so save()'s final
// os.Rename(tmpFile, manifestPath) fails with "is a directory".
func TestCreateRollsBackGitWorktreeOnManifestSaveFailure(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	repo := registerRepo(t, fixture)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	wtRoot := filepath.Join(root, "worktrees")
	m, err := worktrees.NewManager(wtRoot, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	manifestPath := filepath.Join(wtRoot, "proj-a", "manifest.json")
	if err := os.MkdirAll(manifestPath, 0o700); err != nil {
		t.Fatalf("pre-create manifest path as a directory: %v", err)
	}

	_, err = m.Create(context.Background(), repo, worktrees.Spec{
		ProjectID: "proj-a", TaskID: "tsk1", AttemptID: "att1", BaseCommit: fixture.Head(),
	})
	if err == nil {
		t.Fatal("expected Create to fail once the manifest save fails")
	}

	worktreePath := filepath.Join(wtRoot, "proj-a", "tsk1", "att1")
	entries := fixture.Git("worktree", "list", "--porcelain")
	if strings.Contains(entries, worktreePath) {
		t.Fatalf("git still registers the rolled-back worktree: %s", entries)
	}
	branches := fixture.Git("branch", "--list", "devcadience/tsk1/att1")
	if strings.TrimSpace(branches) != "" {
		t.Fatalf("git still has the rolled-back branch: %q", branches)
	}
}

func TestLeakedGitWorktreesDetected(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	repo := registerRepo(t, fixture)
	m := newManager(t)
	base := fixture.Head()

	// Create one worktree through the manager, and one directly through Git
	// (simulating an out-of-band operation), and confirm only the second is
	// reported as leaked.
	if _, err := m.Create(context.Background(), repo, worktrees.Spec{ProjectID: "proj-a", TaskID: "tsk1", AttemptID: "att1", BaseCommit: base}); err != nil {
		t.Fatalf("create: %v", err)
	}
	leakPath := filepath.Join(filepath.Dir(filepath.Dir(mustWorktreePath(t, m, "proj-a"))), "leaked")
	fixture.Git("worktree", "add", "--detach", "--quiet", leakPath, base)

	leaked, err := m.LeakedGitWorktrees(context.Background(), repo, "proj-a")
	if err != nil {
		t.Fatalf("leaked: %v", err)
	}
	found := false
	for _, p := range leaked {
		if p == leakPath {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected %s in leaked list, got %v", leakPath, leaked)
	}
}

func mustWorktreePath(t *testing.T, m *worktrees.Manager, projectID string) string {
	t.Helper()
	list, err := m.List(projectID)
	if err != nil || len(list) == 0 {
		t.Fatalf("list: %v (len=%d)", err, len(list))
	}
	return list[0].Path
}

func TestIsStale(t *testing.T) {
	wt := worktrees.Worktree{BaseCommit: "aaa"}
	if wt.IsStale("aaa") {
		t.Fatal("same commit must not be stale")
	}
	if !wt.IsStale("bbb") {
		t.Fatal("different commit must be stale")
	}
}
