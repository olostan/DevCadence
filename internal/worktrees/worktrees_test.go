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

// TestCleanupRecordedMissingRefusesEvenIfPathReappears proves that once a
// worktree is recorded StatusMissing, a later Cleanup call cannot fall
// through to ordinary removal just because something now occupies the
// recorded path again: the manifest's stored status must gate Cleanup, not
// only the current filesystem state, otherwise a replacement directory (or
// an unrelated worktree that happens to land at the same path) could be
// force-removed by an operator who only meant to clean up the originally
// missing one.
func TestCleanupRecordedMissingRefusesEvenIfPathReappears(t *testing.T) {
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
	// First Cleanup call records it as missing (existing behaviour).
	if err := m.Cleanup(context.Background(), repo, "proj-a", wt.ID, worktrees.CleanupOptions{}); errs.CategoryOf(err) != errs.CategoryIntegrity {
		t.Fatalf("first cleanup category = %v", errs.CategoryOf(err))
	}

	// Something reoccupies the path — a plain directory standing in for a
	// replacement worktree or unrelated content.
	if err := os.MkdirAll(wt.Path, 0o700); err != nil {
		t.Fatalf("recreate dir at path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt.Path, "replacement.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write into replacement dir: %v", err)
	}

	err = m.Cleanup(context.Background(), repo, "proj-a", wt.ID, worktrees.CleanupOptions{})
	if errs.CategoryOf(err) != errs.CategoryIntegrity {
		t.Fatalf("second cleanup (path reoccupied) category = %v, want CategoryIntegrity requiring Recover (err=%v)",
			errs.CategoryOf(err), err)
	}
	// The reoccupying directory must survive untouched: Cleanup must not
	// have reached the Git-removal path at all.
	if _, statErr := os.Stat(filepath.Join(wt.Path, "replacement.txt")); statErr != nil {
		t.Fatalf("replacement content was removed by Cleanup: %v", statErr)
	}
	got, err := m.Get("proj-a", wt.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != worktrees.StatusMissing {
		t.Fatalf("status = %s, want still missing (Cleanup must not have advanced it)", got.Status)
	}
}

// TestRecoverRejectsConfirmGoneContradictedByExistingDirectory proves
// Recover refuses confirmGone=true when the directory it is meant to
// confirm as absent is, in fact, still present: silently treating that
// contradiction as destructive permission would let a stale or mistaken
// confirmGone=true force-delete a worktree (and any uncommitted evidence
// in it) that never actually went missing.
func TestRecoverRejectsConfirmGoneContradictedByExistingDirectory(t *testing.T) {
	fixture := testsupport.NewGitRepo(t)
	repo := registerRepo(t, fixture)
	m := newManager(t)
	base := fixture.Head()
	wt, err := m.Create(context.Background(), repo, worktrees.Spec{ProjectID: "proj-a", TaskID: "tsk1", AttemptID: "att1", BaseCommit: base})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	err = m.Recover(context.Background(), repo, "proj-a", wt.ID, true)
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category = %v, want CategoryInvalidArgument for a contradicted confirmGone (err=%v)",
			errs.CategoryOf(err), err)
	}
	// The live worktree must be untouched: no destructive removal happened.
	if _, statErr := os.Stat(wt.Path); statErr != nil {
		t.Fatalf("worktree directory was removed despite the contradiction being refused: %v", statErr)
	}
	got, err := m.Get("proj-a", wt.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != worktrees.StatusActive {
		t.Fatalf("status = %s, want unchanged active", got.Status)
	}
}

// TestLoadRejectsCorruptManifestEntries proves the manifest loader
// validates every decoded entry rather than trusting a syntactically valid
// but semantically corrupt manifest.json (as a hand edit, or a bug
// elsewhere writing outside save's atomic path, could produce). A nil
// entry, an entry addressed to a different project, an entry whose task id
// is not a safe path component, and an entry whose ID or manifest key does
// not match its own task/attempt ids must all be refused with an integrity
// error before Cleanup or Recover can ever act on them.
func TestLoadRejectsCorruptManifestEntries(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{name: "nil entry", json: `{"worktrees":{"tsk1/att1":null}}`},
		{
			name: "project id mismatch",
			json: `{"worktrees":{"tsk1/att1":{"id":"tsk1/att1","project_id":"proj-b","task_id":"tsk1","attempt_id":"att1","base_commit":"deadbeef","branch":"b","path":"/ignored","status":"active","created_at":"x","updated_at":"x"}}}`,
		},
		{
			name: "task id is not a safe path component",
			json: `{"worktrees":{"tsk1/att1":{"id":"../../../etc/att1","project_id":"proj-a","task_id":"../../../etc","attempt_id":"att1","base_commit":"deadbeef","branch":"b","path":"/ignored","status":"active","created_at":"x","updated_at":"x"}}}`,
		},
		{
			name: "id does not match task/attempt ids",
			json: `{"worktrees":{"tsk1/att1":{"id":"other/id","project_id":"proj-a","task_id":"tsk1","attempt_id":"att1","base_commit":"deadbeef","branch":"b","path":"/ignored","status":"active","created_at":"x","updated_at":"x"}}}`,
		},
		{
			name: "manifest key does not match its own id",
			json: `{"worktrees":{"tsk1/wrong-key":{"id":"tsk1/att1","project_id":"proj-a","task_id":"tsk1","attempt_id":"att1","base_commit":"deadbeef","branch":"b","path":"/ignored","status":"active","created_at":"x","updated_at":"x"}}}`,
		},
		{
			name: "unrecognized status",
			json: `{"worktrees":{"tsk1/att1":{"id":"tsk1/att1","project_id":"proj-a","task_id":"tsk1","attempt_id":"att1","base_commit":"deadbeef","branch":"b","path":"/ignored","status":"quantum","created_at":"x","updated_at":"x"}}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatalf("eval symlinks: %v", err)
			}
			wtRoot := filepath.Join(root, "worktrees")
			m, err := worktrees.NewManager(wtRoot, nil)
			if err != nil {
				t.Fatalf("new manager: %v", err)
			}
			manifestDir := filepath.Join(wtRoot, "proj-a")
			if err := os.MkdirAll(manifestDir, 0o700); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(filepath.Join(manifestDir, "manifest.json"), []byte(tc.json), 0o600); err != nil {
				t.Fatalf("write manifest: %v", err)
			}
			if _, err := m.Get("proj-a", "tsk1/att1"); errs.CategoryOf(err) != errs.CategoryIntegrity {
				t.Fatalf("category = %v, want CategoryIntegrity (err=%v)", errs.CategoryOf(err), err)
			}
		})
	}
}

// TestLoadRecomputesPathFromValidatedIdentifiersNotStoredValue proves a
// valid manifest entry's Path is always recomputed from its validated
// project/task/attempt identifiers rather than trusted verbatim: even a
// syntactically fine entry could carry a stale or hand-edited Path field
// pointing somewhere this manager does not own, and Cleanup/Recover must
// never be handed that untrusted value.
func TestLoadRecomputesPathFromValidatedIdentifiersNotStoredValue(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	wtRoot := filepath.Join(root, "worktrees")
	m, err := worktrees.NewManager(wtRoot, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	manifestDir := filepath.Join(wtRoot, "proj-a")
	if err := os.MkdirAll(manifestDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifestJSON := `{"worktrees":{"tsk1/att1":{"id":"tsk1/att1","project_id":"proj-a","task_id":"tsk1","attempt_id":"att1","base_commit":"deadbeef","branch":"b","path":"/somewhere/an/attacker/or/a/stale/edit/put/here","status":"active","created_at":"x","updated_at":"x"}}}`
	if err := os.WriteFile(filepath.Join(manifestDir, "manifest.json"), []byte(manifestJSON), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	got, err := m.Get("proj-a", "tsk1/att1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	want := filepath.Join(wtRoot, "proj-a", "tsk1", "att1")
	if got.Path != want {
		t.Fatalf("path = %q, want the recomputed path %q (stored value must never be trusted verbatim)", got.Path, want)
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
