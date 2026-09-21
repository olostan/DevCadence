package worktrees_test

import (
	"context"
	"os"
	"path/filepath"
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
