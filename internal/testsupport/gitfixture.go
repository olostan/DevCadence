package testsupport

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitEnv is a fixed, hermetic environment for fixture repository setup, so
// commit identity and timestamps never depend on the host machine's Git
// configuration (ENGINEERING_STANDARDS.md §17: deterministic tests).
func gitEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=DevCadence Fixture",
		"GIT_AUTHOR_EMAIL=fixture@devcadence.test",
		"GIT_COMMITTER_NAME=DevCadence Fixture",
		"GIT_COMMITTER_EMAIL=fixture@devcadence.test",
		"GIT_AUTHOR_DATE=2026-01-02T03:04:05Z",
		"GIT_COMMITTER_DATE=2026-01-02T03:04:05Z",
	)
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (in %s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// GitRepo is a synthetic repository built for tests. It is never the
// DevCadence repository itself (docs/IMPLEMENTATION_PLAN.md M2 §25).
type GitRepo struct {
	t    *testing.T
	Path string
}

// NewGitRepo initialises a fresh repository under the test's temp directory
// with one commit on its default branch, so callers always have a known
// HeadCommit to build from.
func NewGitRepo(t *testing.T) *GitRepo {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	runGit(t, dir, "init", "--quiet", "--initial-branch=main")
	repo := &GitRepo{t: t, Path: dir}
	repo.WriteFile("README.md", "# fixture\n")
	repo.Commit("initial commit")
	return repo
}

// WriteFile writes content at a path relative to the repository root,
// creating parent directories as needed. It does not stage or commit.
func (r *GitRepo) WriteFile(relPath, content string) {
	r.t.Helper()
	full := filepath.Join(r.Path, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		r.t.Fatalf("mkdir for %s: %v", relPath, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		r.t.Fatalf("write %s: %v", relPath, err)
	}
}

// Commit stages everything and commits, returning the new commit SHA.
func (r *GitRepo) Commit(message string) string {
	r.t.Helper()
	runGit(r.t, r.Path, "add", "-A")
	runGit(r.t, r.Path, "commit", "--quiet", "-m", message)
	return r.Head()
}

// Head returns the current HEAD commit SHA.
func (r *GitRepo) Head() string {
	r.t.Helper()
	return runGit(r.t, r.Path, "rev-parse", "HEAD")
}

// Branch returns the current branch name.
func (r *GitRepo) Branch() string {
	r.t.Helper()
	return runGit(r.t, r.Path, "rev-parse", "--abbrev-ref", "HEAD")
}

// Git runs an arbitrary Git subcommand against the fixture, for setup steps
// this helper does not name directly (creating a branch to diverge, etc.).
func (r *GitRepo) Git(args ...string) string {
	r.t.Helper()
	return runGit(r.t, r.Path, args...)
}
