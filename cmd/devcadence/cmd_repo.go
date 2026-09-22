// Command support for M2: repository inspection, worktree lifecycle,
// controlled process execution, validation profiles and candidate metadata.
//
// As with every other command in this package, these are thin adapters:
// argument parsing and printing only. The behaviour lives in
// internal/repository, internal/worktrees, internal/process,
// internal/validation and internal/artifacts (docs/ARCHITECTURE.md §3).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/artifacts"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/repository"
	"github.com/olostan/DevCadence/internal/validation"
	"github.com/olostan/DevCadence/internal/worktrees"
)

// defaultUnderHome resolves $DEVCADENCE_HOME/<sub>, mirroring
// defaultDatabasePath in run.go so every piece of DevCadence state lives
// under one root by default.
func defaultUnderHome(sub string) (string, error) {
	home := os.Getenv("DEVCADENCE_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", errs.Wrap(errs.CategoryInvalidArgument, err, "cannot determine home directory")
		}
		home = filepath.Join(userHome, ".devcadence")
	}
	if !filepath.IsAbs(home) {
		return "", errs.New(errs.CategoryInvalidArgument, "DEVCADENCE_HOME must be an absolute path, got %q", home)
	}
	return filepath.Join(home, sub), nil
}

func runRepo(ctx context.Context, e *env, args []string) error {
	if len(args) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "usage: devcadence repo <inspect>")
	}
	switch args[0] {
	case "inspect":
		return runRepoInspect(ctx, e, args[1:])
	default:
		return errs.New(errs.CategoryInvalidArgument, "unknown repo subcommand %q", args[0])
	}
}

func runRepoInspect(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("repo inspect", flag.ContinueOnError)
	projectID := fs.String("project", "", "project identifier (for provenance only)")
	path := fs.String("path", "", "repository path")
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	if *path == "" {
		return errs.New(errs.CategoryInvalidArgument, "-path is required")
	}
	abs, err := filepath.Abs(*path)
	if err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "resolve -path")
	}
	repo, err := repository.Register(ctx, orDefault(*projectID, "unregistered"), abs, repository.Options{})
	if err != nil {
		return err
	}
	facts, err := repo.Inspect(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "path:    %s\n", repo.Path)
	fmt.Fprintf(e.stdout, "branch:  %s (detached=%t)\n", facts.Branch, facts.Detached)
	fmt.Fprintf(e.stdout, "head:    %s\n", facts.HeadCommit)
	fmt.Fprintf(e.stdout, "dirty:   %t\n", facts.Dirty)
	if facts.Dirty {
		for _, s := range facts.Status {
			fmt.Fprintf(e.stdout, "  %s %s\n", s.Code, s.Path)
		}
	}
	return nil
}

func runWorktree(ctx context.Context, e *env, args []string) error {
	if len(args) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "usage: devcadence worktree <create|list|cleanup|recover>")
	}
	switch args[0] {
	case "create":
		return runWorktreeCreate(ctx, e, args[1:])
	case "list":
		return runWorktreeList(ctx, e, args[1:])
	case "cleanup":
		return runWorktreeCleanup(ctx, e, args[1:])
	case "recover":
		return runWorktreeRecover(ctx, e, args[1:])
	default:
		return errs.New(errs.CategoryInvalidArgument, "unknown worktree subcommand %q", args[0])
	}
}

func worktreeManager(root string) (*worktrees.Manager, error) {
	if root == "" {
		resolved, err := defaultUnderHome("worktrees")
		if err != nil {
			return nil, err
		}
		root = resolved
	}
	return worktrees.NewManager(root, process.NewRunner())
}

func registerRepoFlag(ctx context.Context, projectID, path string) (*repository.Repository, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "resolve -repo")
	}
	return repository.Register(ctx, projectID, abs, repository.Options{})
}

func runWorktreeCreate(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("worktree create", flag.ContinueOnError)
	projectID := fs.String("project", "", "project identifier")
	repoPath := fs.String("repo", "", "repository path")
	taskID := fs.String("task", "", "task identifier")
	attemptID := fs.String("attempt", "", "attempt identifier")
	base := fs.String("base", "", "base commit")
	root := fs.String("root", "", "worktree storage root (default $DEVCADENCE_HOME/worktrees)")
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	if *projectID == "" || *repoPath == "" || *taskID == "" || *attemptID == "" || *base == "" {
		return errs.New(errs.CategoryInvalidArgument, "-project, -repo, -task, -attempt and -base are all required")
	}
	repo, err := registerRepoFlag(ctx, *projectID, *repoPath)
	if err != nil {
		return err
	}
	mgr, err := worktreeManager(*root)
	if err != nil {
		return err
	}
	wt, err := mgr.Create(ctx, repo, worktrees.Spec{
		ProjectID: *projectID, TaskID: *taskID, AttemptID: *attemptID, BaseCommit: *base,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "created worktree %s at %s (branch %s)\n", wt.ID, wt.Path, wt.Branch)
	return nil
}

func runWorktreeList(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("worktree list", flag.ContinueOnError)
	projectID := fs.String("project", "", "project identifier")
	root := fs.String("root", "", "worktree storage root (default $DEVCADENCE_HOME/worktrees)")
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	if *projectID == "" {
		return errs.New(errs.CategoryInvalidArgument, "-project is required")
	}
	mgr, err := worktreeManager(*root)
	if err != nil {
		return err
	}
	list, err := mgr.List(*projectID)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(e.stdout, list)
	}
	if len(list) == 0 {
		fmt.Fprintln(e.stdout, "no worktrees")
		return nil
	}
	for _, wt := range list {
		fmt.Fprintf(e.stdout, "%-10s %-8s %-30s %s\n", wt.ID, wt.Status, wt.Branch, wt.Path)
	}
	return nil
}

func runWorktreeCleanup(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("worktree cleanup", flag.ContinueOnError)
	projectID := fs.String("project", "", "project identifier")
	repoPath := fs.String("repo", "", "repository path")
	id := fs.String("id", "", "worktree id (task/attempt)")
	root := fs.String("root", "", "worktree storage root (default $DEVCADENCE_HOME/worktrees)")
	force := fs.Bool("force", false, "remove even if the worktree has uncommitted changes")
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	if *projectID == "" || *repoPath == "" || *id == "" {
		return errs.New(errs.CategoryInvalidArgument, "-project, -repo and -id are all required")
	}
	repo, err := registerRepoFlag(ctx, *projectID, *repoPath)
	if err != nil {
		return err
	}
	mgr, err := worktreeManager(*root)
	if err != nil {
		return err
	}
	if err := mgr.Cleanup(ctx, repo, *projectID, *id, worktrees.CleanupOptions{Force: *force}); err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "removed worktree %s\n", *id)
	return nil
}

func runWorktreeRecover(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("worktree recover", flag.ContinueOnError)
	projectID := fs.String("project", "", "project identifier")
	repoPath := fs.String("repo", "", "repository path (optional; used to reconcile Git's own bookkeeping)")
	id := fs.String("id", "", "worktree id (task/attempt)")
	root := fs.String("root", "", "worktree storage root (default $DEVCADENCE_HOME/worktrees)")
	confirmGone := fs.Bool("confirm-gone", false, "confirm the worktree directory is gone and should be closed as removed")
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	if *projectID == "" || *id == "" {
		return errs.New(errs.CategoryInvalidArgument, "-project and -id are required")
	}
	var repo *repository.Repository
	if *repoPath != "" {
		r, err := registerRepoFlag(ctx, *projectID, *repoPath)
		if err != nil {
			return err
		}
		repo = r
	}
	mgr, err := worktreeManager(*root)
	if err != nil {
		return err
	}
	if err := mgr.Recover(ctx, repo, *projectID, *id, *confirmGone); err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "recovered worktree %s\n", *id)
	return nil
}

func runRun(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	dir := fs.String("dir", "", "working directory (required, must be absolute)")
	timeout := fs.Duration("timeout", 5*time.Minute, "command timeout")
	envOverrides := fs.String("env", "", "comma-separated KEY=VALUE environment overrides")
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	rest := fs.Args()
	dashDash := indexOf(args, "--")
	if dashDash >= 0 {
		rest = args[dashDash+1:]
	}
	if len(rest) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "usage: devcadence run -dir <path> -- <executable> [args...]")
	}
	if *dir == "" {
		return errs.New(errs.CategoryInvalidArgument, "-dir is required")
	}
	abs, err := filepath.Abs(*dir)
	if err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "resolve -dir")
	}
	env := process.MergeEnv(process.BaseEnv(), overridesToMap(*envOverrides))
	res, err := process.NewRunner().Run(ctx, process.Spec{
		Executable: rest[0], Args: rest[1:], Dir: abs, Env: env, Timeout: *timeout,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(e.stderr, "status=%s exit=%d duration=%s\n", res.Status, res.ExitCode, res.Duration())
	e.stdout.Write(res.Stdout)
	e.stderr.Write(res.Stderr)
	if res.Status != process.StatusCompleted || res.ExitCode != 0 {
		return errs.New(errs.CategoryValidationFailed, "command exited with status %s (exit %d)", res.Status, res.ExitCode)
	}
	return nil
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

func overridesToMap(s string) map[string]string {
	out := map[string]string{}
	for _, kv := range splitList(s) {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			out[kv[:i]] = kv[i+1:]
		}
	}
	return out
}

func runValidate(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	projectID := fs.String("project", "", "project identifier")
	profileFile := fs.String("profile-file", "", "path to a validation profiles document")
	profileName := fs.String("profile", "", "profile name within the document")
	dir := fs.String("dir", "", "working directory to run checks in")
	commit := fs.String("commit", "", "commit the run validates")
	scope := fs.String("scope", "", "attempt, integration or baseline")
	taskID := fs.String("task", "", "task id (attempt/integration scope)")
	attemptID := fs.String("attempt", "", "attempt id (attempt scope)")
	artifactRoot := fs.String("artifacts", "", "artifact storage root (default $DEVCADENCE_HOME/artifacts)")
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	for name, v := range map[string]string{
		"project": *projectID, "profile-file": *profileFile, "profile": *profileName,
		"dir": *dir, "commit": *commit, "scope": *scope,
	} {
		if v == "" {
			return errs.New(errs.CategoryInvalidArgument, "-%s is required", name)
		}
	}
	data, err := os.ReadFile(*profileFile)
	if err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "read profile file")
	}
	profiles, err := validation.LoadProfiles(data)
	if err != nil {
		return err
	}
	profile, ok := profiles[*profileName]
	if !ok {
		return errs.New(errs.CategoryInvalidArgument, "profile %q not found in %s", *profileName, *profileFile)
	}
	absDir, err := filepath.Abs(*dir)
	if err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "resolve -dir")
	}
	root := *artifactRoot
	if root == "" {
		resolved, err := defaultUnderHome("artifacts")
		if err != nil {
			return err
		}
		root = resolved
	}
	store, err := artifacts.NewStore(root, ids.NewULIDSource())
	if err != nil {
		return err
	}

	subject := protocol.ValidationSubject{Kind: protocol.ValidationScope(*scope), TaskID: *taskID, AttemptID: *attemptID}

	service, dbStore, err := e.openService(ctx, false)
	if err != nil {
		return err
	}
	defer func() { _ = dbStore.Close() }()

	result, err := validation.ExecuteAndRecord(ctx, service, validation.ExecuteInput{
		ProjectID: *projectID,
		Subject:   subject,
		Commit:    *commit,
		Profile:   profile,
		Run:       validation.RunOptions{Dir: absDir, Artifacts: store},
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "validation %s: %s (%d checks)\n",
		result.ValidationResult.ValidationID, result.ValidationResult.Status, len(result.ValidationResult.Checks))
	for _, c := range result.ValidationResult.Checks {
		fmt.Fprintf(e.stdout, "  %-20s %s\n", c.ID, c.Status)
	}
	if result.ValidationResult.Status != protocol.ValidationPass {
		return errs.New(errs.CategoryValidationFailed, "validation %s did not pass", result.ValidationResult.ValidationID)
	}
	return nil
}

func runCandidate(ctx context.Context, e *env, args []string) error {
	if len(args) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "usage: devcadence candidate <show>")
	}
	switch args[0] {
	case "show":
		return runCandidateShow(ctx, e, args[1:])
	default:
		return errs.New(errs.CategoryInvalidArgument, "unknown candidate subcommand %q", args[0])
	}
}

func runCandidateShow(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("candidate show", flag.ContinueOnError)
	projectID := fs.String("project", "", "project identifier")
	repoPath := fs.String("repo", "", "repository path")
	base := fs.String("base", "", "base commit")
	head := fs.String("head", "", "candidate/head commit")
	checkIntegration := fs.Bool("check-integration", true, "also report fast-forward/merge/conflict feasibility against base")
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	if *projectID == "" || *repoPath == "" || *base == "" || *head == "" {
		return errs.New(errs.CategoryInvalidArgument, "-project, -repo, -base and -head are all required")
	}
	repo, err := registerRepoFlag(ctx, *projectID, *repoPath)
	if err != nil {
		return err
	}
	exists, err := repo.CommitExists(ctx, *head)
	if err != nil {
		return err
	}
	if !exists {
		return errs.New(errs.CategoryInvalidArgument, "candidate commit %q does not exist in %s", *head, repo.Path)
	}
	diff, err := repo.Diff(ctx, *base, *head)
	if err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "base:  %s\n", *base)
	fmt.Fprintf(e.stdout, "head:  %s\n", *head)
	if service, dbStore, err := e.openService(ctx, true); err == nil {
		if projectState, err := service.ProjectState(ctx, *projectID); err == nil && projectState.Git.AcceptedCommit != nil {
			accepted := *projectState.Git.AcceptedCommit
			stale := repository.StaleBase(*base, accepted)
			fmt.Fprintf(e.stdout, "stale: %t (accepted=%s)\n", stale, accepted)
		}
		_ = dbStore.Close()
	}
	fmt.Fprintf(e.stdout, "changed files: %d (+%d/-%d)\n", len(diff.ChangedPaths), diff.Additions, diff.Deletions)
	for _, p := range diff.ChangedPaths {
		fmt.Fprintf(e.stdout, "  %s\n", p)
	}
	if *checkIntegration {
		check, err := repo.CheckMerge(ctx, *base, *head)
		if err != nil {
			return err
		}
		switch {
		case check.FastForward:
			fmt.Fprintln(e.stdout, "integration: fast-forward")
		case check.Clean:
			fmt.Fprintln(e.stdout, "integration: clean merge")
		default:
			fmt.Fprintf(e.stdout, "integration: conflict in %d path(s)\n", len(check.ConflictingPaths))
			for _, p := range check.ConflictingPaths {
				fmt.Fprintf(e.stdout, "  %s\n", p)
			}
		}
	}
	return nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
