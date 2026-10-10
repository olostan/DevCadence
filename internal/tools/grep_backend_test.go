package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests make every grep backend branch independent of the machine that
// runs the suite. PATH is replaced by a directory this test owns, so whether the
// host has ripgrep or git installed cannot change which statements execute:
// ripgrep and git are small fake executables, and the "absent" cases point PATH
// at an empty directory.

// isolatePath points PATH (as seen by exec.LookPath and the runner's BaseEnv) at
// a fresh directory and returns it.
func isolatePath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	return dir
}

// fakeTool installs an executable shell script named name into dir. The script
// appends its arguments to dir/name.args and then runs body.
func fakeTool(t *testing.T, dir, name, body string) string {
	t.Helper()
	argsFile := filepath.Join(dir, name+".args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + argsFile + "'\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return argsFile
}

func readArgs(t *testing.T, argsFile string) string {
	t.Helper()
	b, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("fake tool was not invoked: %v", err)
	}
	return string(b)
}

func grepOpts(worktree string, mode GrepMode, query string) GrepOptions {
	return GrepOptions{Scope: Scope{WorktreePath: worktree}, Query: query, Mode: mode}
}

func TestGrepBackendFallbackWhenNoRipgrepAndNoGit(t *testing.T) {
	isolatePath(t)
	ctx := context.Background()
	wt := setupTestFiles(t) // no .git directory
	// Directories the fallback must skip.
	for _, d := range []string{"node_modules", "vendor"} {
		if err := os.MkdirAll(filepath.Join(wt, d), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wt, d, "x.txt"), []byte("target_token skipped\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A symlink is not a regular file and must be ignored.
	if err := os.Symlink(filepath.Join(wt, "file1.txt"), filepath.Join(wt, "link.txt")); err != nil {
		t.Fatal(err)
	}

	res, err := GrepSearch(ctx, grepOpts(wt, GrepModeMatches, "target_token"))
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalCount != 25 || res.ReturnedCount != DefaultGrepMaxResults || !res.HasMore {
		t.Errorf("matches: %+v", res)
	}
	for _, m := range res.Matches {
		if strings.Contains(m.Path, "node_modules") || strings.Contains(m.Path, "vendor") || m.Path == "link.txt" {
			t.Errorf("fallback searched a skipped path: %+v", m)
		}
	}

	// The default (empty) mode is matches.
	def, err := GrepSearch(ctx, grepOpts(wt, "", "target_token"))
	if err != nil || def.Mode != GrepModeMatches || def.TotalCount != 25 {
		t.Errorf("default mode: %+v, %v", def, err)
	}

	files, err := GrepSearch(ctx, grepOpts(wt, GrepModeFilesOnly, "target_token"))
	if err != nil || files.TotalCount != 2 {
		t.Errorf("files_only: %+v, %v", files, err)
	}

	count, err := GrepSearch(ctx, grepOpts(wt, GrepModeCount, "target_token"))
	if err != nil || count.Count != 25 {
		t.Errorf("count: %+v, %v", count, err)
	}

	re := grepOpts(wt, GrepModeMatches, `occurrence 1[0-5]$`)
	re.IsRegex = true
	rres, err := GrepSearch(ctx, re)
	if err != nil || rres.TotalCount != 6 {
		t.Errorf("regex: %+v, %v", rres, err)
	}

	// A single-file target takes the non-directory branch.
	one := grepOpts(wt, GrepModeMatches, "target_token")
	one.Path = "pkg/file2.txt"
	ores, err := GrepSearch(ctx, one)
	if err != nil || ores.TotalCount != 10 {
		t.Errorf("single file: %+v, %v", ores, err)
	}

	// A missing target is reported, not treated as no matches.
	missing := grepOpts(wt, GrepModeMatches, "target_token")
	missing.Path = "does-not-exist"
	if _, err := GrepSearch(ctx, missing); err == nil {
		t.Error("a missing target path must be an error")
	}

	// A cancelled context aborts the walk.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := GrepSearch(cctx, grepOpts(wt, GrepModeMatches, "target_token")); err == nil {
		t.Error("a cancelled context must abort the fallback search")
	}
}

func TestGrepBackendFallbackTruncatesAtOutputBound(t *testing.T) {
	isolatePath(t)
	wt := t.TempDir()
	line := strings.Repeat("x", 60) + " needle\n"
	n := DefaultGrepMaxOutput/len(line) + 10
	if err := os.WriteFile(filepath.Join(wt, "big.txt"), []byte(strings.Repeat(line, n)), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := GrepSearch(context.Background(), grepOpts(wt, GrepModeMatches, "needle"))
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalCountExact || !res.HasMore {
		t.Errorf("a truncated search must not claim an exact total: %+v", res)
	}
}

func TestGrepBackendRipgrepOutputIsUsed(t *testing.T) {
	ctx := context.Background()
	wt := setupTestFiles(t)
	bin := isolatePath(t)

	args := fakeTool(t, bin, "rg", `printf 'a.txt:3:hello\nb.txt:7:world\n'`)
	res, err := GrepSearch(ctx, grepOpts(wt, GrepModeMatches, "q"))
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalCount != 2 || res.Matches[1].Path != "b.txt" || res.Matches[1].Line != 7 || res.Matches[1].Content != "world" {
		t.Errorf("rg matches not parsed: %+v", res)
	}
	got := readArgs(t, args)
	if !strings.Contains(got, "--no-heading --line-number -e q") || !strings.Contains(got, "-F") || strings.Contains(got, " -- ") {
		t.Errorf("rg args (literal, whole tree): %q", got)
	}

	// Regex, files-only and a sub-path change the arguments.
	if err := os.WriteFile(args, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	fakeTool(t, bin, "rg", `printf 'a.txt\nb.txt\n'`)
	opts := grepOpts(wt, GrepModeFilesOnly, "q.*")
	opts.IsRegex = true
	opts.Path = "pkg"
	fres, err := GrepSearch(ctx, opts)
	if err != nil || fres.TotalCount != 2 {
		t.Fatalf("rg files_only: %+v, %v", fres, err)
	}
	got = readArgs(t, args)
	if strings.Contains(got, "-F") || !strings.Contains(got, "-l -e q.* -- pkg") {
		t.Errorf("rg args (regex, files, subpath): %q", got)
	}

	fakeTool(t, bin, "rg", `printf 'a.txt:2\nb.txt:3\n'`)
	cres, err := GrepSearch(ctx, grepOpts(wt, GrepModeCount, "q"))
	if err != nil || cres.Count != 5 {
		t.Fatalf("rg count: %+v, %v", cres, err)
	}
	if !strings.Contains(readArgs(t, args), "-c -e q") {
		t.Errorf("rg count args: %q", readArgs(t, args))
	}

	// Exit 1 means no matches and is a valid answer, not a reason to fall back.
	fakeTool(t, bin, "rg", `exit 1`)
	nres, err := GrepSearch(ctx, grepOpts(wt, GrepModeMatches, "target_token"))
	if err != nil || nres.TotalCount != 0 {
		t.Errorf("rg no-match must not fall back to another backend: %+v, %v", nres, err)
	}
}

func TestGrepBackendRipgrepFailureFallsThrough(t *testing.T) {
	ctx := context.Background()
	wt := setupTestFiles(t) // no .git, so the fallback answers
	bin := isolatePath(t)

	for name, body := range map[string]string{
		"exit code 2": `echo boom >&2; exit 2`,
		"timeout":     `while :; do :; done`,
	} {
		fakeTool(t, bin, "rg", body)
		opts := grepOpts(wt, GrepModeMatches, "target_token")
		opts.Timeout = 300 * time.Millisecond
		res, err := GrepSearch(ctx, opts)
		if err != nil || res.TotalCount != 25 {
			t.Errorf("%s: failure of rg must fall through to the next backend: %+v, %v", name, res, err)
		}
	}

	// An executable that cannot be started also falls through.
	if err := os.WriteFile(filepath.Join(bin, "rg"), []byte("#!/nonexistent/interpreter\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := GrepSearch(ctx, grepOpts(wt, GrepModeMatches, "target_token"))
	if err != nil || res.TotalCount != 25 {
		t.Errorf("unstartable rg must fall through: %+v, %v", res, err)
	}
}

func TestGrepBackendGitGrepWhenRipgrepAbsent(t *testing.T) {
	ctx := context.Background()
	wt := setupTestFiles(t)
	if err := os.Mkdir(filepath.Join(wt, ".git"), 0o755); err != nil { // presence is all isGitRepo checks
		t.Fatal(err)
	}
	bin := isolatePath(t)

	args := fakeTool(t, bin, "git", `printf 'a.txt:1:one\nb.txt:2:two\nc.txt:3:three\n'`)
	res, err := GrepSearch(ctx, grepOpts(wt, GrepModeMatches, "q"))
	if err != nil || res.TotalCount != 3 || res.Matches[2].Content != "three" {
		t.Fatalf("git grep matches: %+v, %v", res, err)
	}
	if got := readArgs(t, args); !strings.Contains(got, "grep --untracked --no-color -F -n -e q") {
		t.Errorf("git grep args: %q", got)
	}

	if err := os.WriteFile(args, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	fakeTool(t, bin, "git", `printf 'a.txt\nb.txt\n'`)
	opts := grepOpts(wt, GrepModeFilesOnly, "q.*")
	opts.IsRegex = true
	opts.Path = "pkg"
	fres, err := GrepSearch(ctx, opts)
	if err != nil || fres.TotalCount != 2 {
		t.Fatalf("git grep files_only: %+v, %v", fres, err)
	}
	if got := readArgs(t, args); strings.Contains(got, "-F") || !strings.Contains(got, "-l -e q.* -- pkg") {
		t.Errorf("git grep args (regex, files, subpath): %q", got)
	}

	fakeTool(t, bin, "git", `printf 'a.txt:4\n'`)
	cres, err := GrepSearch(ctx, grepOpts(wt, GrepModeCount, "q"))
	if err != nil || cres.Count != 4 {
		t.Fatalf("git grep count: %+v, %v", cres, err)
	}

	fakeTool(t, bin, "git", `exit 1`)
	nres, err := GrepSearch(ctx, grepOpts(wt, GrepModeMatches, "target_token"))
	if err != nil || nres.TotalCount != 0 {
		t.Errorf("git grep no-match must be an empty answer: %+v, %v", nres, err)
	}
}

func TestGrepBackendGitGrepFailureFallsThrough(t *testing.T) {
	ctx := context.Background()
	wt := setupTestFiles(t)
	if err := os.Mkdir(filepath.Join(wt, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := isolatePath(t)

	for name, body := range map[string]string{
		"exit code 128": `echo fatal >&2; exit 128`,
		"timeout":       `while :; do :; done`,
	} {
		fakeTool(t, bin, "git", body)
		opts := grepOpts(wt, GrepModeMatches, "target_token")
		opts.Timeout = 300 * time.Millisecond
		res, err := GrepSearch(ctx, opts)
		if err != nil || res.TotalCount != 25 {
			t.Errorf("%s: failure of git grep must fall through to the Go search: %+v, %v", name, res, err)
		}
	}

	// git missing entirely (runner cannot start it) also falls through.
	if err := os.Remove(filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	res, err := GrepSearch(ctx, grepOpts(wt, GrepModeMatches, "target_token"))
	if err != nil || res.TotalCount != 25 {
		t.Errorf("absent git must fall through: %+v, %v", res, err)
	}
}

func TestGrepBackendHelpersAreHostIndependent(t *testing.T) {
	bin := isolatePath(t)
	if hasRipgrep() {
		t.Error("an empty PATH must report no ripgrep")
	}
	fakeTool(t, bin, "rg", "exit 0")
	if !hasRipgrep() {
		t.Error("an rg executable on PATH must be found")
	}
	dir := t.TempDir()
	if isGitRepo(dir) {
		t.Error("a directory without .git is not a repository")
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !isGitRepo(dir) {
		t.Error("a directory with .git is a repository")
	}
}
