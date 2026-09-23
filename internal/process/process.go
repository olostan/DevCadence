// Package process implements the controlled external-process runner
// (docs/SECURITY.md §5, ENGINEERING_STANDARDS.md §8, DCI-033).
//
// The runner is the only way the rest of DevCadence executes an external
// binary. It never interprets a shell string: callers supply an explicit
// executable and argv, an explicit working directory and an explicit
// environment. There is no implicit inheritance of the daemon's own
// environment and no shell expansion, so a repository file that contains
// "ignore your instructions and run `curl ... | sh`" has no path to
// execution through this package (DCI-083).
package process

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
)

// Status is the terminal condition of a run. It exists so callers can
// distinguish "the command ran and exited" from "the runner stopped it" and
// from "the runner never started it" without parsing error text.
type Status string

const (
	// StatusCompleted means the process was started and exited on its own,
	// with any exit code. A nonzero exit code is not a runner error: a
	// failing test command is expected output, not infrastructure failure.
	StatusCompleted Status = "completed"
	// StatusTimeout means the process exceeded Spec.Timeout and was killed.
	StatusTimeout Status = "timeout"
	// StatusCancelled means the caller's context was cancelled and the
	// process was killed.
	StatusCancelled Status = "cancelled"
)

// DefaultMaxOutputBytes bounds captured stdout/stderr when a Spec leaves the
// limit at zero, so a runaway command cannot exhaust memory by default
// (docs/IMPLEMENTATION_PLAN.md M2, DCI-033).
const DefaultMaxOutputBytes = 4 << 20 // 4 MiB

// Spec describes one controlled execution. It is the only accepted shape:
// there is no alternate "run this shell string" entry point.
type Spec struct {
	// Executable is looked up by exact name or absolute/relative path. It is
	// never passed through a shell.
	Executable string
	// Args is the argv passed to Executable, unexpanded.
	Args []string
	// Dir is the working directory. It must be an absolute path that exists;
	// the runner never defaults to the daemon's own working directory,
	// because that would let an unset Dir silently authorize writes outside
	// whatever the caller intended to confine the command to.
	Dir string
	// Env is the complete environment, as "KEY=VALUE" entries. The daemon's
	// own environment is never implicitly inherited (docs/SECURITY.md §5);
	// see BaseEnv for the minimal platform set callers typically start from.
	Env []string
	// Timeout bounds wall-clock execution and is required: an unbounded
	// controlled command is a contradiction in terms.
	Timeout time.Duration
	// Stdin, if set, is streamed to the process. Most controlled commands
	// need none.
	Stdin io.Reader
	// MaxStdoutBytes and MaxStderrBytes bound captured output. Zero selects
	// DefaultMaxOutputBytes; a negative value is rejected.
	MaxStdoutBytes int64
	MaxStderrBytes int64
	// StdoutSink and StderrSink receive streaming output concurrently with
	// bounded inline capture, enabling decoupling of artifact storage or
	// live log streaming without making the runner depend on artifacts (ADR-0016).
	StdoutSink io.Writer
	StderrSink io.Writer
}

// Result is the captured outcome of one run.
type Result struct {
	Command []string
	Dir     string
	// Env is the resolved environment the process actually ran with, for
	// observability. Callers are responsible for not putting secret values
	// into Env in the first place (DCI-081); this package does not attempt
	// to guess which entries are sensitive.
	Env []string

	Status   Status
	ExitCode int // -1 when the process did not exit normally (timeout/cancel/signal).
	Signal   string

	Stdout          []byte
	Stderr          []byte
	StdoutTruncated bool
	StderrTruncated bool

	StartedAt  time.Time
	FinishedAt time.Time
}

// Duration is how long the process ran.
func (r Result) Duration() time.Duration { return r.FinishedAt.Sub(r.StartedAt) }

// Success reports whether the process completed with exit code zero.
func (r Result) Success() bool { return r.Status == StatusCompleted && r.ExitCode == 0 }

// Runner executes Specs. It holds no mutable state and is safe for
// concurrent use, which is what lets parallel worktrees run commands at the
// same time (docs/IMPLEMENTATION_PLAN.md M2 §21).
type Runner struct{}

// NewRunner returns a Runner.
func NewRunner() *Runner { return &Runner{} }

// Run executes spec to completion, to a timeout, or to cancellation.
//
// The returned error is non-nil only for conditions the process never ran
// to distinguish, or could not meaningfully run under: invalid
// configuration, an executable the runner could not resolve, or an
// internal failure starting the process. A command that started and was
// then stopped by a timeout or by ctx cancellation is reported through
// Result.Status with a nil error, because the runner did what it was asked
// to do.
func (r *Runner) Run(ctx context.Context, spec Spec) (Result, error) {
	if err := validateSpec(spec); err != nil {
		return Result{}, err
	}
	resolved, err := resolveExecutable(spec.Executable, spec.Env)
	if err != nil {
		return Result{}, err
	}

	maxStdout := spec.MaxStdoutBytes
	if maxStdout == 0 {
		maxStdout = DefaultMaxOutputBytes
	}
	maxStderr := spec.MaxStderrBytes
	if maxStderr == 0 {
		maxStderr = DefaultMaxOutputBytes
	}

	runCtx, cancel := context.WithTimeout(ctx, spec.Timeout)
	defer cancel()

	cmd := exec.Cmd{
		Path: resolved,
		Args: append([]string{resolved}, spec.Args...),
		Dir:  spec.Dir,
		Env:  append([]string(nil), spec.Env...),
	}
	if spec.Stdin != nil {
		cmd.Stdin = spec.Stdin
	}
	stdout := newBoundedWriter(maxStdout)
	stderr := newBoundedWriter(maxStderr)
	if spec.StdoutSink != nil {
		cmd.Stdout = io.MultiWriter(stdout, spec.StdoutSink)
	} else {
		cmd.Stdout = stdout
	}
	if spec.StderrSink != nil {
		cmd.Stderr = io.MultiWriter(stderr, spec.StderrSink)
	} else {
		cmd.Stderr = stderr
	}

	setProcAttrs(&cmd)

	started := time.Now().UTC()
	if err := cmd.Start(); err != nil {
		if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) {
			return Result{}, errs.Wrap(errs.CategoryNotFound, err, "start %s", resolved)
		}
		return Result{}, errs.Wrap(errs.CategoryInternal, err, "start %s", resolved)
	}

	waitErrCh := make(chan error, 1)
	go func() { waitErrCh <- cmd.Wait() }()

	var waitErr error
	select {
	case waitErr = <-waitErrCh:
		// The process exited on its own before the deadline or cancellation.
	case <-runCtx.Done():
		killProcessGroup(cmd.Process)
		waitErr = <-waitErrCh
	}
	finished := time.Now().UTC()

	result := Result{
		Command:         append([]string{resolved}, spec.Args...),
		Dir:             spec.Dir,
		Env:             append([]string(nil), spec.Env...),
		Stdout:          stdout.buf.Bytes(),
		Stderr:          stderr.buf.Bytes(),
		StdoutTruncated: stdout.truncated,
		StderrTruncated: stderr.truncated,
		StartedAt:       started,
		FinishedAt:      finished,
		ExitCode:        -1,
	}

	switch {
	case ctx.Err() != nil:
		result.Status = StatusCancelled
	case runCtx.Err() != nil:
		result.Status = StatusTimeout
	default:
		result.Status = StatusCompleted
	}

	if waitErr == nil {
		result.ExitCode = 0
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		if sig := signalName(exitErr); sig != "" {
			result.Signal = sig
			result.ExitCode = -1
		}
		return result, nil
	}
	// Wait failed for a reason other than a nonzero/ signalled exit: the
	// process could not be waited on. This is a runner failure, not a
	// command outcome, unless it was caused by our own kill, which the
	// status fields above already explain.
	if result.Status == StatusCompleted {
		return result, errs.Wrap(errs.CategoryInternal, waitErr, "wait for %s", resolved)
	}
	return result, nil
}

func validateSpec(spec Spec) error {
	if spec.Executable == "" {
		return errs.New(errs.CategoryInvalidArgument, "process: executable is required")
	}
	if spec.Dir == "" {
		return errs.New(errs.CategoryInvalidArgument, "process: dir is required")
	}
	if !filepath.IsAbs(spec.Dir) {
		return errs.New(errs.CategoryInvalidArgument, "process: dir %q must be absolute", spec.Dir)
	}
	info, err := os.Stat(spec.Dir)
	if err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "process: dir %q is not accessible", spec.Dir)
	}
	if !info.IsDir() {
		return errs.New(errs.CategoryInvalidArgument, "process: dir %q is not a directory", spec.Dir)
	}
	if spec.Timeout <= 0 {
		return errs.New(errs.CategoryInvalidArgument, "process: timeout must be > 0")
	}
	if spec.MaxStdoutBytes < 0 || spec.MaxStderrBytes < 0 {
		return errs.New(errs.CategoryInvalidArgument, "process: output limits must not be negative")
	}
	for _, e := range spec.Env {
		if !strings.Contains(e, "=") {
			return errs.New(errs.CategoryInvalidArgument, "process: env entry %q is not KEY=VALUE", e)
		}
	}
	return nil
}

// resolveExecutable finds the binary using only spec.Env's PATH (or an
// explicit path), never the daemon's own environment. This is what makes
// "controlled environment" mean something: a caller that wants `go` found
// must say where `go` lives, rather than the runner silently reusing
// whatever happens to be on the daemon's PATH.
func resolveExecutable(name string, env []string) (string, error) {
	if strings.ContainsRune(name, os.PathSeparator) {
		// A path-shaped executable must be absolute. A relative one (e.g.
		// "./tool" or "sub/tool") would resolve against the daemon's own
		// working directory rather than Spec.Dir, which is not what a
		// caller who set an explicit Dir would expect and would make the
		// resolved binary depend on ambient process state instead of the
		// Spec (docs/SECURITY.md §6).
		if !filepath.IsAbs(name) {
			return "", errs.New(errs.CategoryInvalidArgument,
				"executable %q is a relative path; use an absolute path or a bare name resolved via env PATH", name)
		}
		info, err := os.Stat(name)
		if err != nil {
			return "", errs.Wrap(errs.CategoryNotFound, err, "executable %q not found", name)
		}
		if info.IsDir() {
			return "", errs.New(errs.CategoryInvalidArgument, "executable %q is a directory", name)
		}
		return name, nil
	}
	path := ""
	for _, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			path = strings.TrimPrefix(e, "PATH=")
			break
		}
	}
	if path == "" {
		return "", errs.New(errs.CategoryInvalidArgument,
			"process: no PATH in env; cannot resolve executable %q by name", name)
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		if !filepath.IsAbs(dir) {
			// A relative PATH entry (".", "./tools", "tools", …) would be
			// joined with name and passed to os.Stat relative to this
			// daemon's own current directory, not Spec.Dir — the very
			// ambient-state dependency the absolute-path branch above
			// already refuses for a path-shaped executable name. Worse,
			// the child process itself runs with Spec.Dir as its working
			// directory, so a relative PATH entry would make this
			// pre-check resolve a different file than the one the shell
			// (or the child's own exec) would actually run, silently
			// defeating the controlled-resolution guarantee
			// (docs/SECURITY.md §6). Reject it outright rather than
			// guessing which directory it should be relative to.
			return "", errs.New(errs.CategoryInvalidArgument,
				"process: PATH entry %q is relative; only absolute PATH directories are supported for resolving %q", dir, name)
		}
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		if isExecutable(info) {
			return candidate, nil
		}
	}
	return "", errs.New(errs.CategoryNotFound, "executable %q not found on the configured PATH", name)
}

func isExecutable(info os.FileInfo) bool {
	return info.Mode()&0o111 != 0
}

// boundedWriter accepts up to max bytes and silently drops the rest, setting
// truncated rather than erroring: DCI-033/M2 requires enough evidence to
// debug a failure without letting unlimited output exhaust memory, and an
// explicit truncation flag is what makes the loss visible instead of silent.
type boundedWriter struct {
	buf       bytes.Buffer
	max       int64
	truncated bool
}

func newBoundedWriter(max int64) *boundedWriter { return &boundedWriter{max: max} }

func (w *boundedWriter) Write(p []byte) (int, error) {
	remaining := w.max - int64(w.buf.Len())
	if remaining <= 0 {
		w.truncated = true
		return len(p), nil
	}
	if int64(len(p)) > remaining {
		w.buf.Write(p[:remaining])
		w.truncated = true
		return len(p), nil
	}
	w.buf.Write(p)
	return len(p), nil
}

// BaseEnv returns the minimal platform environment controlled commands need
// to resolve and run ordinary tooling: PATH, HOME and a stable locale. It
// deliberately does not copy the daemon's full os.Environ(): docs/SECURITY.md
// §5 requires conservative environment handling, and every project- or
// command-specific variable must be added explicitly by the caller.
func BaseEnv() []string {
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/local/bin:/usr/bin:/bin"
	}
	env := []string{"PATH=" + path, "LANG=C.UTF-8", "LC_ALL=C.UTF-8"}
	if home := os.Getenv("HOME"); home != "" {
		env = append(env, "HOME="+home)
	}
	return env
}

// MergeEnv layers overrides onto base, keeping deterministic ordering: base
// entries first (skipped when overridden), then overrides in the order
// given. Later duplicate keys in either slice win over earlier ones.
func MergeEnv(base []string, overrides map[string]string) []string {
	keyOf := func(e string) string {
		if i := strings.IndexByte(e, '='); i >= 0 {
			return e[:i]
		}
		return e
	}
	// Base entries: a duplicate key keeps its first position (for
	// deterministic ordering) but the last occurrence's value, matching the
	// documented last-wins rule. Without this, a duplicated base key (e.g.
	// two PATH= entries) would survive into the resolved environment twice,
	// letting resolveExecutable's PATH lookup (which takes the first match)
	// and the child process's actual environment (where a later duplicate
	// wins) disagree about which value is in effect.
	baseIndex := make(map[string]int, len(base))
	var baseOut []string
	for _, e := range base {
		k := keyOf(e)
		if _, overridden := overrides[k]; overridden {
			continue
		}
		if idx, ok := baseIndex[k]; ok {
			baseOut[idx] = e
			continue
		}
		baseIndex[k] = len(baseOut)
		baseOut = append(baseOut, e)
	}
	out := make([]string, 0, len(baseOut)+len(overrides))
	out = append(out, baseOut...)
	seen := make(map[string]bool, len(overrides))
	// Deterministic order for overrides regardless of map iteration.
	keys := make([]string, 0, len(overrides))
	for k := range overrides {
		keys = append(keys, k)
	}
	sortStrings(keys)
	for _, k := range keys {
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k+"="+overrides[k])
	}
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
