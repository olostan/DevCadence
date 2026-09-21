package process

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadience/internal/errs"
)

func testDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

func TestRunSuccess(t *testing.T) {
	r := NewRunner()
	dir := testDir(t)
	res, err := r.Run(context.Background(), Spec{
		Executable: "echo",
		Args:       []string{"hello"},
		Dir:        dir,
		Env:        BaseEnv(),
		Timeout:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Success() {
		t.Fatalf("expected success, got status=%s exit=%d", res.Status, res.ExitCode)
	}
	if strings.TrimSpace(string(res.Stdout)) != "hello" {
		t.Fatalf("stdout = %q", res.Stdout)
	}
}

func TestRunNonzeroExitIsNotAnError(t *testing.T) {
	r := NewRunner()
	dir := testDir(t)
	res, err := r.Run(context.Background(), Spec{
		Executable: "sh",
		Args:       []string{"-c", "exit 7"},
		Dir:        dir,
		Env:        BaseEnv(),
		Timeout:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusCompleted {
		t.Fatalf("status = %s", res.Status)
	}
	if res.ExitCode != 7 {
		t.Fatalf("exit code = %d", res.ExitCode)
	}
}

func TestRunMissingExecutable(t *testing.T) {
	r := NewRunner()
	dir := testDir(t)
	_, err := r.Run(context.Background(), Spec{
		Executable: "devcadience-does-not-exist-xyz",
		Dir:        dir,
		Env:        BaseEnv(),
		Timeout:    5 * time.Second,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if errs.CategoryOf(err) != errs.CategoryNotFound {
		t.Fatalf("category = %s", errs.CategoryOf(err))
	}
}

func TestRunTimeout(t *testing.T) {
	r := NewRunner()
	dir := testDir(t)
	res, err := r.Run(context.Background(), Spec{
		Executable: "sleep",
		Args:       []string{"5"},
		Dir:        dir,
		Env:        BaseEnv(),
		Timeout:    100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusTimeout {
		t.Fatalf("status = %s, want timeout", res.Status)
	}
}

func TestRunCancellation(t *testing.T) {
	r := NewRunner()
	dir := testDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	res, err := r.Run(ctx, Spec{
		Executable: "sleep",
		Args:       []string{"5"},
		Dir:        dir,
		Env:        BaseEnv(),
		Timeout:    10 * time.Second,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusCancelled {
		t.Fatalf("status = %s, want cancelled", res.Status)
	}
}

func TestRunnerReusableAfterTimeout(t *testing.T) {
	r := NewRunner()
	dir := testDir(t)
	if _, err := r.Run(context.Background(), Spec{
		Executable: "sleep", Args: []string{"5"}, Dir: dir, Env: BaseEnv(), Timeout: 50 * time.Millisecond,
	}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	res, err := r.Run(context.Background(), Spec{
		Executable: "echo", Args: []string{"ok"}, Dir: dir, Env: BaseEnv(), Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !res.Success() {
		t.Fatalf("second run did not succeed: %+v", res)
	}
}

func TestRunStdoutTruncation(t *testing.T) {
	r := NewRunner()
	dir := testDir(t)
	res, err := r.Run(context.Background(), Spec{
		Executable:     "sh",
		Args:           []string{"-c", "yes x | head -c 100000"},
		Dir:            dir,
		Env:            BaseEnv(),
		Timeout:        5 * time.Second,
		MaxStdoutBytes: 1024,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.StdoutTruncated {
		t.Fatal("expected stdout truncated")
	}
	if len(res.Stdout) != 1024 {
		t.Fatalf("stdout len = %d, want 1024", len(res.Stdout))
	}
}

func TestRunStderrCaptured(t *testing.T) {
	r := NewRunner()
	dir := testDir(t)
	res, err := r.Run(context.Background(), Spec{
		Executable: "sh",
		Args:       []string{"-c", "echo oops 1>&2"},
		Dir:        dir,
		Env:        BaseEnv(),
		Timeout:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(string(res.Stderr)) != "oops" {
		t.Fatalf("stderr = %q", res.Stderr)
	}
}

func TestRunInvalidSpecRejected(t *testing.T) {
	r := NewRunner()
	_, err := r.Run(context.Background(), Spec{Executable: "echo", Dir: "relative/path", Env: BaseEnv(), Timeout: time.Second})
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category = %s", errs.CategoryOf(err))
	}
	_, err = r.Run(context.Background(), Spec{Executable: "echo", Dir: os.TempDir(), Env: BaseEnv(), Timeout: 0})
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category (timeout) = %s", errs.CategoryOf(err))
	}
}

func TestRunDirDoesNotExist(t *testing.T) {
	r := NewRunner()
	_, err := r.Run(context.Background(), Spec{
		Executable: "echo", Dir: filepath.Join(os.TempDir(), "devcadience-no-such-dir-xyz"), Env: BaseEnv(), Timeout: time.Second,
	})
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category = %s", errs.CategoryOf(err))
	}
}

func TestRunNoPathCannotResolveByName(t *testing.T) {
	r := NewRunner()
	dir := testDir(t)
	_, err := r.Run(context.Background(), Spec{Executable: "echo", Dir: dir, Env: []string{"HOME=/tmp"}, Timeout: time.Second})
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category = %s", errs.CategoryOf(err))
	}
}

// TestRunRelativePathEntryRejected proves a relative PATH entry is refused
// rather than silently resolved against this daemon's own working
// directory. A relative entry such as "./tools" would let os.Stat here
// find a binary the child process (which runs with Spec.Dir, not this
// process's cwd, as its working directory) would resolve differently or
// not at all, defeating the explicit-directory/controlled-resolution
// guarantee docs/SECURITY.md §6 requires.
func TestRunRelativePathEntryRejected(t *testing.T) {
	r := NewRunner()
	dir := testDir(t)
	_, err := r.Run(context.Background(), Spec{
		Executable: "echo", Dir: dir, Env: []string{"HOME=/tmp", "PATH=./tools"}, Timeout: time.Second,
	})
	if err == nil {
		t.Fatal("expected a relative PATH entry to be refused")
	}
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category = %s, want InvalidArgument", errs.CategoryOf(err))
	}
}

// TestRunRelativePathEntryRejectedBeforeLaterAbsoluteMatch proves a relative
// PATH entry is refused as soon as the lookup reaches it, even when a later
// absolute directory in the same PATH would otherwise have resolved the
// executable: an ambiguous PATH entry must not be silently skipped over in
// search of a match elsewhere.
func TestRunRelativePathEntryRejectedBeforeLaterAbsoluteMatch(t *testing.T) {
	r := NewRunner()
	dir := testDir(t)
	echoPath, err := exeLookup("echo")
	if err != nil {
		t.Skip("echo not found on system PATH")
	}
	_, err = r.Run(context.Background(), Spec{
		Executable: "echo", Dir: dir,
		Env:     []string{"HOME=/tmp", "PATH=tools" + string(os.PathListSeparator) + filepath.Dir(echoPath)},
		Timeout: time.Second,
	})
	if err == nil {
		t.Fatal("expected the leading relative PATH entry to be refused rather than skipped for a later absolute match")
	}
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category = %s, want InvalidArgument", errs.CategoryOf(err))
	}
}

func TestRunAbsolutePathIgnoresEnvPath(t *testing.T) {
	r := NewRunner()
	dir := testDir(t)
	echoPath, err := exeLookup("echo")
	if err != nil {
		t.Skip("echo not found on system PATH")
	}
	res, err := r.Run(context.Background(), Spec{Executable: echoPath, Args: []string{"hi"}, Dir: dir, Env: []string{"HOME=/tmp"}, Timeout: time.Second})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Success() {
		t.Fatalf("expected success: %+v", res)
	}
}

func TestMergeEnv(t *testing.T) {
	base := []string{"PATH=/bin", "HOME=/home/x"}
	merged := MergeEnv(base, map[string]string{"HOME": "/home/y", "FOO": "bar"})
	want := map[string]string{"PATH": "/bin", "HOME": "/home/y", "FOO": "bar"}
	if len(merged) != len(want) {
		t.Fatalf("merged = %v", merged)
	}
	got := map[string]string{}
	for _, e := range merged {
		i := strings.IndexByte(e, '=')
		got[e[:i]] = e[i+1:]
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("key %s = %q, want %q", k, got[k], v)
		}
	}
}

// TestMergeEnvDeduplicatesDuplicateBaseKeys proves the documented last-wins
// rule ("later duplicate keys in either slice win over earlier ones")
// actually holds for duplicate keys within base itself, not only between
// base and overrides. Without deduplication, a duplicated base entry (e.g.
// two PATH= values) would survive into the merged slice twice, letting
// resolveExecutable's PATH lookup (which takes the first match) disagree
// with whichever value the OS treats as authoritative for the child
// process's actual environment.
func TestMergeEnvDeduplicatesDuplicateBaseKeys(t *testing.T) {
	base := []string{"PATH=/first", "HOME=/home/x", "PATH=/second"}
	merged := MergeEnv(base, nil)
	count := 0
	var last string
	for _, e := range merged {
		if strings.HasPrefix(e, "PATH=") {
			count++
			last = e
		}
	}
	if count != 1 {
		t.Fatalf("merged has %d PATH entries, want 1: %v", count, merged)
	}
	if last != "PATH=/second" {
		t.Fatalf("PATH entry = %q, want the later duplicate's value PATH=/second", last)
	}
}

// exeLookup is a tiny helper only for the absolute-path test above; it does
// not exercise resolveExecutable's PATH-in-env behaviour, only where `echo`
// actually lives on this machine so the test can pass an absolute path.
func exeLookup(name string) (string, error) {
	for _, dir := range strings.Split(os.Getenv("PATH"), string(os.PathListSeparator)) {
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", os.ErrNotExist
}
