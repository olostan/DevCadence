// Package worktrees implements the isolated Git worktree manager
// (docs/IMPLEMENTATION_PLAN.md M2, DCI-030, DCI-034).
//
// A worktree is the safety boundary between an attempt and every other
// attempt, and between an attempt and the accepted working tree. Ownership
// (project/task/attempt/base commit/branch/path) is tracked in a manifest
// file per project rather than in SQLite: worktrees are filesystem state
// that can be inspected and recovered independently of the control-plane
// database, and M2 deliberately keeps that recovery path simple rather than
// wiring it through a new relational schema (see docs/adr/0007 for the
// rationale).
package worktrees

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/process"
	"github.com/olostan/DevCadience/internal/repository"
)

// Status is a worktree's lifecycle state.
type Status string

const (
	// StatusActive is a worktree created and not yet cleaned up.
	StatusActive Status = "active"
	// StatusRemoved was cleaned up normally.
	StatusRemoved Status = "removed"
	// StatusMissing means the manifest still lists it but its directory is
	// gone (crash, manual deletion, external `git worktree` operation). It
	// requires explicit Recover rather than being silently pruned.
	StatusMissing Status = "missing"
)

// idComponent restricts task/attempt identifiers used as path components.
// DCI-030/docs/SECURITY.md §6: a worktree path is derived only from
// validated identifiers, never from caller-supplied path fragments, so
// "../" or an absolute path masquerading as an id cannot escape the root.
var idComponent = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// Worktree is one tracked isolated working tree.
type Worktree struct {
	ID         string `json:"id"`
	ProjectID  string `json:"project_id"`
	TaskID     string `json:"task_id"`
	AttemptID  string `json:"attempt_id"`
	BaseCommit string `json:"base_commit"`
	Branch     string `json:"branch"`
	Path       string `json:"path"`
	Status     Status `json:"status"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// IsStale reports whether the worktree's base commit differs from the
// currently accepted commit (docs/IMPLEMENTATION_PLAN.md M2 §18).
func (w Worktree) IsStale(currentAccepted string) bool {
	return repository.StaleBase(w.BaseCommit, currentAccepted)
}

// Spec describes a worktree to create.
type Spec struct {
	ProjectID  string
	TaskID     string
	AttemptID  string
	BaseCommit string
}

// Manager creates, lists and removes isolated worktrees under one root
// directory, tracked in a per-project JSON manifest.
//
// One Manager may serve many repositories/projects concurrently. A per-
// project mutex serialises manifest reads/writes and the Git worktree
// operations for that project — the narrow serialisation
// ENGINEERING_STANDARDS.md §21 asks for, not a global lock: worktree
// creation for project A never waits on project B.
type Manager struct {
	root   string
	runner *process.Runner

	mu       sync.Mutex
	projects map[string]*sync.Mutex
}

// NewManager returns a Manager rooted at root, an absolute directory that
// worktrees are created under. It never creates the directory itself.
func NewManager(root string, runner *process.Runner) (*Manager, error) {
	if root == "" || !filepath.IsAbs(root) {
		return nil, errs.New(errs.CategoryInvalidArgument, "worktrees: root must be an absolute path")
	}
	if runner == nil {
		runner = process.NewRunner()
	}
	return &Manager{root: filepath.Clean(root), runner: runner, projects: map[string]*sync.Mutex{}}, nil
}

func (m *Manager) lockFor(projectID string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.projects[projectID]
	if !ok {
		l = &sync.Mutex{}
		m.projects[projectID] = l
	}
	return l
}

// Create adds a new isolated Git worktree for one attempt.
//
// Path and branch are deterministic functions of the identifiers
// (`<root>/<project>/<task>/<attempt>`,
// `devcadience/<task>/<attempt>`), never caller-supplied strings, which is
// what makes "no writing into another attempt's worktree" a structural
// property rather than a convention: two calls with the same identifiers
// always name the same worktree, and different identifiers can never
// collide.
//
// Create does not implicitly reuse an existing worktree for the same
// attempt (docs/IMPLEMENTATION_PLAN.md M2 §8: "prefer no implicit reuse
// initially"); a second Create for the same attempt is refused.
func (m *Manager) Create(ctx context.Context, repo *repository.Repository, spec Spec) (*Worktree, error) {
	if err := validateSpec(spec); err != nil {
		return nil, err
	}
	if repo == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "worktrees: repository is required")
	}
	exists, err := repo.CommitExists(ctx, spec.BaseCommit)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errs.New(errs.CategoryInvalidArgument,
			"worktrees: base commit %q does not exist in the repository", spec.BaseCommit)
	}

	lock := m.lockFor(spec.ProjectID)
	lock.Lock()
	defer lock.Unlock()

	manifest, err := m.load(spec.ProjectID)
	if err != nil {
		return nil, err
	}
	id := worktreeID(spec.TaskID, spec.AttemptID)
	if existing, ok := manifest.byID(id); ok && existing.Status == StatusActive {
		return nil, errs.New(errs.CategoryConflict,
			"worktrees: attempt %s already has an active worktree at %s; retries use a new attempt id",
			spec.AttemptID, existing.Path)
	}

	path := filepath.Join(m.root, spec.ProjectID, spec.TaskID, spec.AttemptID)
	branch := fmt.Sprintf("devcadience/%s/%s", spec.TaskID, spec.AttemptID)

	if _, err := os.Stat(path); err == nil {
		return nil, errs.New(errs.CategoryConflict,
			"worktrees: path %s already exists on disk but is not tracked as active; "+
				"run recovery before creating a worktree at this location", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "worktrees: create parent directory")
	}

	res, err := m.runner.Run(ctx, process.Spec{
		Executable: "git",
		Args:       []string{"-C", repo.Path, "worktree", "add", "-b", branch, path, spec.BaseCommit},
		Dir:        repo.Path,
		Env:        process.BaseEnv(),
		Timeout:    2 * time.Minute,
	})
	if err != nil {
		return nil, err
	}
	if !res.Success() {
		msg := strings.TrimSpace(string(res.Stderr))
		if strings.Contains(msg, "already exists") {
			return nil, errs.New(errs.CategoryConflict, "worktrees: branch %s already exists: %s", branch, msg)
		}
		return nil, errs.New(errs.CategoryInternal, "worktrees: git worktree add failed: %s", msg)
	}

	now := nowRFC3339()
	wt := &Worktree{
		ID: id, ProjectID: spec.ProjectID, TaskID: spec.TaskID, AttemptID: spec.AttemptID,
		BaseCommit: spec.BaseCommit, Branch: branch, Path: path,
		Status: StatusActive, CreatedAt: now, UpdatedAt: now,
	}
	manifest.put(wt)
	if err := m.save(spec.ProjectID, manifest); err != nil {
		return nil, err
	}
	return wt, nil
}

// CleanupOptions configures worktree removal.
type CleanupOptions struct {
	// Force removes a worktree even if it has uncommitted changes. Without
	// it, Cleanup refuses a dirty worktree so an operator does not silently
	// lose in-progress evidence.
	Force bool
}

// Cleanup removes a worktree's directory and Git registration and marks it
// removed in the manifest.
func (m *Manager) Cleanup(ctx context.Context, repo *repository.Repository, projectID, id string, opts CleanupOptions) error {
	lock := m.lockFor(projectID)
	lock.Lock()
	defer lock.Unlock()

	manifest, err := m.load(projectID)
	if err != nil {
		return err
	}
	wt, ok := manifest.byID(id)
	if !ok {
		return errs.New(errs.CategoryNotFound, "worktrees: %s not found in project %s", id, projectID)
	}
	if wt.Status == StatusRemoved {
		return nil
	}
	if _, statErr := os.Stat(wt.Path); os.IsNotExist(statErr) {
		wt.Status = StatusMissing
		wt.UpdatedAt = nowRFC3339()
		manifest.put(wt)
		if err := m.save(projectID, manifest); err != nil {
			return err
		}
		return errs.New(errs.CategoryIntegrity,
			"worktrees: %s's directory %s is missing; its state is unknown and it must be handled with Recover, "+
				"not an ordinary cleanup", id, wt.Path)
	}

	if !opts.Force {
		dirty, err := isDirty(ctx, m.runner, wt.Path)
		if err != nil {
			return err
		}
		if dirty {
			return errs.New(errs.CategoryPolicyDenied,
				"worktrees: %s has uncommitted changes; pass Force to discard them", id)
		}
	}

	args := []string{"-C", repo.Path, "worktree", "remove"}
	if opts.Force {
		args = append(args, "--force")
	}
	args = append(args, wt.Path)
	res, err := m.runner.Run(ctx, process.Spec{
		Executable: "git", Args: args, Dir: repo.Path, Env: process.BaseEnv(), Timeout: 2 * time.Minute,
	})
	if err != nil {
		return err
	}
	if !res.Success() {
		return errs.New(errs.CategoryInternal, "worktrees: git worktree remove failed: %s", strings.TrimSpace(string(res.Stderr)))
	}
	// The branch is no longer needed once its worktree is gone; deleting it
	// is best-effort and not fatal (a branch left behind is inspectable,
	// never destructive).
	_, _ = m.runner.Run(ctx, process.Spec{
		Executable: "git", Args: []string{"-C", repo.Path, "branch", "-D", wt.Branch},
		Dir: repo.Path, Env: process.BaseEnv(), Timeout: 30 * time.Second,
	})

	wt.Status = StatusRemoved
	wt.UpdatedAt = nowRFC3339()
	manifest.put(wt)
	return m.save(projectID, manifest)
}

// Recover reconciles a worktree the manifest marked (or would mark) missing.
// It never force-deletes on its own initiative: the caller must say what
// happened, either that the directory truly is gone (so the manifest entry
// is closed as removed) or that it exists and should be adopted back to
// active. This mirrors docs/IMPLEMENTATION_PLAN.md M2 §8's instruction not
// to default every recovery scenario to `git worktree prune` or a forced
// delete.
func (m *Manager) Recover(ctx context.Context, repo *repository.Repository, projectID, id string, confirmGone bool) error {
	lock := m.lockFor(projectID)
	lock.Lock()
	defer lock.Unlock()

	manifest, err := m.load(projectID)
	if err != nil {
		return err
	}
	wt, ok := manifest.byID(id)
	if !ok {
		return errs.New(errs.CategoryNotFound, "worktrees: %s not found in project %s", id, projectID)
	}
	_, statErr := os.Stat(wt.Path)
	dirExists := statErr == nil

	if dirExists && !confirmGone {
		// The directory is actually there; recover it back to a normal,
		// inspectable active worktree rather than guessing further.
		wt.Status = StatusActive
		wt.UpdatedAt = nowRFC3339()
		manifest.put(wt)
		return m.save(projectID, manifest)
	}
	if !dirExists && !confirmGone {
		return errs.New(errs.CategoryInvalidArgument,
			"worktrees: %s's directory is gone; pass confirmGone=true to close it as removed", id)
	}
	// confirmGone: the operator has confirmed the directory should be (or
	// already is) gone. Ask Git to drop its own bookkeeping for the path if
	// it still thinks the worktree exists; a failure here is not fatal
	// because there may be nothing left for Git to know about.
	if repo != nil {
		_, _ = m.runner.Run(ctx, process.Spec{
			Executable: "git", Args: []string{"-C", repo.Path, "worktree", "remove", "--force", wt.Path},
			Dir: repo.Path, Env: process.BaseEnv(), Timeout: 30 * time.Second,
		})
	}
	wt.Status = StatusRemoved
	wt.UpdatedAt = nowRFC3339()
	manifest.put(wt)
	return m.save(projectID, manifest)
}

// Get returns one tracked worktree.
func (m *Manager) Get(projectID, id string) (*Worktree, error) {
	lock := m.lockFor(projectID)
	lock.Lock()
	defer lock.Unlock()
	manifest, err := m.load(projectID)
	if err != nil {
		return nil, err
	}
	wt, ok := manifest.byID(id)
	if !ok {
		return nil, errs.New(errs.CategoryNotFound, "worktrees: %s not found in project %s", id, projectID)
	}
	cp := *wt
	return &cp, nil
}

// List returns every tracked worktree for a project, oldest first.
func (m *Manager) List(projectID string) ([]*Worktree, error) {
	lock := m.lockFor(projectID)
	lock.Lock()
	defer lock.Unlock()
	manifest, err := m.load(projectID)
	if err != nil {
		return nil, err
	}
	out := make([]*Worktree, 0, len(manifest.Worktrees))
	for _, wt := range manifest.Worktrees {
		cp := *wt
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out, nil
}

// LeakedGitWorktrees compares the manifest against what Git itself believes
// exists for repo and reports worktree paths Git knows about that this
// manifest has no active record of — a leaked or externally created
// worktree, surfaced for inspection rather than silently pruned
// (docs/IMPLEMENTATION_PLAN.md M2 §8).
func (m *Manager) LeakedGitWorktrees(ctx context.Context, repo *repository.Repository, projectID string) ([]string, error) {
	tracked, err := m.List(projectID)
	if err != nil {
		return nil, err
	}
	trackedPaths := map[string]bool{}
	for _, wt := range tracked {
		if wt.Status == StatusActive {
			trackedPaths[wt.Path] = true
		}
	}
	res, err := m.runner.Run(ctx, process.Spec{
		Executable: "git", Args: []string{"-C", repo.Path, "worktree", "list", "--porcelain"},
		Dir: repo.Path, Env: process.BaseEnv(), Timeout: 30 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	if !res.Success() {
		return nil, errs.New(errs.CategoryInternal, "worktrees: git worktree list failed: %s", string(res.Stderr))
	}
	var leaked []string
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		if !strings.HasPrefix(line, "worktree ") {
			continue
		}
		path := strings.TrimPrefix(line, "worktree ")
		if path == repo.Path {
			continue
		}
		if !strings.HasPrefix(path, m.root) {
			// A linked worktree Git knows about but that lives outside this
			// manager's root is not this manager's concern.
			continue
		}
		if !trackedPaths[path] {
			leaked = append(leaked, path)
		}
	}
	return leaked, nil
}

func isDirty(ctx context.Context, runner *process.Runner, path string) (bool, error) {
	res, err := runner.Run(ctx, process.Spec{
		Executable: "git", Args: []string{"-C", path, "status", "--porcelain=v2"},
		Dir: path, Env: process.BaseEnv(), Timeout: 30 * time.Second,
	})
	if err != nil {
		return false, err
	}
	if !res.Success() {
		return false, errs.New(errs.CategoryInternal, "worktrees: git status failed: %s", string(res.Stderr))
	}
	return len(strings.TrimSpace(string(res.Stdout))) > 0, nil
}

func validateSpec(spec Spec) error {
	if spec.ProjectID == "" {
		return errs.New(errs.CategoryInvalidArgument, "worktrees: project id is required")
	}
	for name, v := range map[string]string{"task_id": spec.TaskID, "attempt_id": spec.AttemptID} {
		if v == "" {
			return errs.New(errs.CategoryInvalidArgument, "worktrees: %s is required", name)
		}
		if !idComponent.MatchString(v) {
			return errs.New(errs.CategoryInvalidArgument,
				"worktrees: %s %q contains characters that are not safe as a path component", name, v)
		}
	}
	if spec.BaseCommit == "" {
		return errs.New(errs.CategoryInvalidArgument, "worktrees: base commit is required")
	}
	return nil
}

func worktreeID(taskID, attemptID string) string { return taskID + "/" + attemptID }

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// manifest is the durable on-disk record of a project's worktrees.
type manifest struct {
	Worktrees map[string]*Worktree `json:"worktrees"`
}

func (m *manifest) byID(id string) (*Worktree, bool) {
	wt, ok := m.Worktrees[id]
	return wt, ok
}

func (m *manifest) put(wt *Worktree) {
	if m.Worktrees == nil {
		m.Worktrees = map[string]*Worktree{}
	}
	m.Worktrees[wt.ID] = wt
}

func (m *Manager) manifestPath(projectID string) string {
	return filepath.Join(m.root, projectID, "manifest.json")
}

// load reads the manifest, returning an empty one if it does not exist yet.
func (m *Manager) load(projectID string) (*manifest, error) {
	path := m.manifestPath(projectID)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &manifest{Worktrees: map[string]*Worktree{}}, nil
	}
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "worktrees: read manifest")
	}
	var m2 manifest
	if err := json.Unmarshal(data, &m2); err != nil {
		return nil, errs.Wrap(errs.CategoryIntegrity, err, "worktrees: manifest %s is corrupt", path)
	}
	if m2.Worktrees == nil {
		m2.Worktrees = map[string]*Worktree{}
	}
	return &m2, nil
}

// save writes the manifest atomically (temp file + rename), so a crash mid
// write never leaves a torn manifest that a later load would fail to parse.
func (m *Manager) save(projectID string, mf *manifest) error {
	path := m.manifestPath(projectID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "worktrees: create manifest directory")
	}
	data, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "worktrees: encode manifest")
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "manifest-*.json.tmp")
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "worktrees: create temp manifest")
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return errs.Wrap(errs.CategoryInternal, err, "worktrees: write temp manifest")
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return errs.Wrap(errs.CategoryInternal, err, "worktrees: sync temp manifest")
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return errs.Wrap(errs.CategoryInternal, err, "worktrees: close temp manifest")
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return errs.Wrap(errs.CategoryInternal, err, "worktrees: publish manifest")
	}
	return nil
}
