package taskexec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/tools"
)

const (
	runCommandToolName     = "run_command"
	defaultCommandTimeout  = 120
	maxCommandTimeout      = 600
	commandOutputHead      = 8 * 1024
	commandOutputTail      = 8 * 1024
	maxArgvEntries         = 256
	maxArgvBytes           = 32 * 1024
	commandTraceSchema     = "devcadence.command_trace/v1"
	unsafeUnconfinedMarker = "unsafe_unconfined"
)

const runCommandDescription = "Run ONE program inside the worktree, without a shell (no pipes, redirects, globbing or &&). " +
	"argv is the program followed by its arguments, e.g. [\"go\",\"test\",\"./...\"]. Optional cwd is a relative subdirectory of the worktree; " +
	"timeout_seconds defaults to 120 (max 600). Returns exit_code, timed_out and the combined stdout/stderr (long output keeps head and tail). " +
	"git, gh, ssh, scp, curl, wget, shells and env/xargs/sudo are refused. No credentials or network authority are provided. " +
	"This runs unconfined as the local user: do not run destructive commands."

const runCommandSchema = `{"type":"object","properties":{` +
	`"argv":{"type":"array","items":{"type":"string"},"description":"program and arguments, e.g. [\"go\",\"test\",\"./...\"]"},` +
	`"cwd":{"type":"string","description":"optional relative subdirectory of the worktree"},` +
	`"timeout_seconds":{"type":"integer","description":"default 120, max 600"}},"required":["argv"]}`

// deniedExecutables are refused by basename. This is a best-effort guard
// against the obvious ways to push, fetch, or escape into a shell; it is NOT a
// sandbox (a permitted program such as go or python can still do anything the
// local user can).
var deniedExecutables = map[string]bool{
	"git": true, "gh": true, "ssh": true, "scp": true, "sftp": true, "curl": true, "wget": true,
	"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true, "ksh": true, "csh": true, "tcsh": true,
	"cmd": true, "powershell": true, "pwsh": true,
	"env": true, "xargs": true, "sudo": true, "su": true, "doas": true,
}

// commandRecord is one audited run_command invocation.
type commandRecord struct {
	Seq              int      `json:"seq"`
	Argv             []string `json:"argv"`
	Cwd              string   `json:"cwd"`
	ExitCode         int      `json:"exit_code"`
	DurationMS       int64    `json:"duration_ms"`
	TimedOut         bool     `json:"timed_out"`
	Truncated        bool     `json:"truncated"`
	OutputBytes      int64    `json:"output_bytes"`
	OutputSHA256     string   `json:"output_sha256"`
	Refused          string   `json:"refused,omitempty"`
	UnsafeUnconfined bool     `json:"unsafe_unconfined"`
}

// commandTrace is the persisted audit artifact for an attempt.
type commandTrace struct {
	Schema           string          `json:"schema"`
	Mode             ExecutionMode   `json:"execution_mode"`
	UnsafeUnconfined bool            `json:"unsafe_unconfined"`
	Commands         []commandRecord `json:"commands"`
}

// commandAudit accumulates the attempt's command records.
type commandAudit struct {
	mu      sync.Mutex
	mode    ExecutionMode
	records []commandRecord
}

func newCommandAudit(mode ExecutionMode) *commandAudit { return &commandAudit{mode: mode} }

func (a *commandAudit) add(r commandRecord) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	r.Seq = len(a.records) + 1
	a.records = append(a.records, r)
}

// trace returns the JSON audit document, or nil when no command was attempted.
func (a *commandAudit) trace() ([]byte, error) {
	if a == nil {
		return nil, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.records) == 0 {
		return nil, nil
	}
	return json.Marshal(commandTrace{
		Schema: commandTraceSchema, Mode: a.mode, UnsafeUnconfined: true,
		Commands: append([]commandRecord(nil), a.records...),
	})
}

// commandResult is the JSON returned to the model.
type commandResult struct {
	ExitCode         int    `json:"exit_code"`
	DurationMS       int64  `json:"duration_ms"`
	TimedOut         bool   `json:"timed_out"`
	Truncated        bool   `json:"truncated"`
	OutputBytes      int64  `json:"output_bytes"`
	Output           string `json:"output"`
	UnsafeUnconfined bool   `json:"unsafe_unconfined"`
}

// headTailWriter keeps the first and last N bytes of a stream, counts the
// total and hashes every byte, so output is bounded yet digestible.
type headTailWriter struct {
	mu    sync.Mutex
	head  []byte
	tail  []byte
	total int64
	sum   hash.Hash
}

func newHeadTailWriter() *headTailWriter { return &headTailWriter{sum: sha256.New()} }

func (w *headTailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sum.Write(p)
	w.total += int64(len(p))
	rest := p
	if room := commandOutputHead - len(w.head); room > 0 {
		n := min(room, len(rest))
		w.head = append(w.head, rest[:n]...)
		rest = rest[n:]
	}
	if len(rest) > 0 {
		w.tail = append(w.tail, rest...)
		if len(w.tail) > commandOutputTail {
			w.tail = append([]byte(nil), w.tail[len(w.tail)-commandOutputTail:]...)
		}
	}
	return len(p), nil
}

func (w *headTailWriter) render() (out string, truncated bool, digest string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	digest = hex.EncodeToString(w.sum.Sum(nil))
	omitted := w.total - int64(len(w.head)) - int64(len(w.tail))
	if omitted <= 0 {
		return strings.ToValidUTF8(string(w.head)+string(w.tail), "�"), false, digest
	}
	return strings.ToValidUTF8(string(w.head), "�") +
		fmt.Sprintf("\n... [%d bytes omitted; output truncated to head+tail] ...\n", omitted) +
		strings.ToValidUTF8(string(w.tail), "�"), true, digest
}

func cmdErr(cat errs.Category, code, format string, a ...any) error {
	return errs.New(cat, "%s: %s: %s", runCommandToolName, code, fmt.Sprintf(format, a...))
}

// registerRunCommand registers the run_command handler and, only in the
// unsafe-unconfined mode, returns its definition for declaration to the model.
// The handler is registered in every mode and re-checks the mode itself, so a
// strict-mode call is refused before any subprocess exists.
func registerRunCommand(mediator *drivers.ScopedToolMediator, scope *tools.Scope, runner *process.Runner, cfg workerToolConfig) (drivers.ToolDefinition, bool) {
	mode := cfg.Mode.normalized()
	def := drivers.ToolDefinition{
		Name:           runCommandToolName,
		Description:    runCommandDescription,
		Parameters:     json.RawMessage(runCommandSchema),
		PathParameters: []string{"cwd"},
	}
	mediator.RegisterToolDefinition(def)
	mediator.RegisterHandler(runCommandToolName, func(ctx context.Context, args json.RawMessage) (string, error) {
		if mode != ExecutionUnsafeUnconfinedLocal {
			return "", cmdErr(errs.CategoryPolicyDenied, "execution_disabled",
				"command execution is disabled (execution_mode=strict); the machine owner can enable it with \"execution_mode\":\"yolo\" in $DEVCADENCE_HOME/config/selfhost.json")
		}
		var p struct {
			Argv           []string `json:"argv"`
			Cwd            string   `json:"cwd"`
			TimeoutSeconds int      `json:"timeout_seconds"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return "", errs.Wrap(errs.CategoryInvalidArgument, err, "invalid run_command arguments")
		}
		rec := commandRecord{Argv: p.Argv, Cwd: p.Cwd, ExitCode: -1, UnsafeUnconfined: true}
		refuse := func(code string, err error) (string, error) {
			rec.Refused = code
			cfg.Audit.add(rec)
			return "", err
		}
		if err := validateArgv(p.Argv); err != nil {
			return refuse("invalid_argv", err)
		}
		if err := checkExecutable(p.Argv[0]); err != nil {
			return refuse("denied_executable", err)
		}
		dir, err := resolveCommandDir(scope, p.Cwd)
		if err != nil {
			return refuse("invalid_cwd", err)
		}
		timeout := p.TimeoutSeconds
		switch {
		case timeout == 0:
			timeout = defaultCommandTimeout
		case timeout < 0 || timeout > maxCommandTimeout:
			return refuse("invalid_timeout", cmdErr(errs.CategoryInvalidArgument, "invalid_timeout",
				"timeout_seconds must be between 1 and %d, got %d", maxCommandTimeout, timeout))
		}

		out := newHeadTailWriter()
		res, runErr := runner.Run(ctx, process.Spec{
			Executable: p.Argv[0],
			Args:       p.Argv[1:],
			Dir:        dir,
			Env:        process.BaseEnv(),
			Timeout:    time.Duration(timeout) * time.Second,
			// Inline capture is irrelevant (the sinks see the full stream); keep it tiny.
			MaxStdoutBytes: 1,
			MaxStderrBytes: 1,
			StdoutSink:     out,
			StderrSink:     out,
		})
		text, truncated, digest := out.render()
		rec.Truncated, rec.OutputBytes, rec.OutputSHA256 = truncated, out.total, digest
		if runErr != nil {
			rec.Refused = "not_started"
			cfg.Audit.add(rec)
			return "", cmdErr(errs.CategoryInvalidArgument, "not_started", "%v", runErr)
		}
		rec.ExitCode = res.ExitCode
		rec.DurationMS = res.Duration().Milliseconds()
		rec.TimedOut = res.Status == process.StatusTimeout
		cfg.Audit.add(rec)
		if res.Status == process.StatusCancelled {
			return "", ctx.Err()
		}
		b, err := json.Marshal(commandResult{
			ExitCode: rec.ExitCode, DurationMS: rec.DurationMS, TimedOut: rec.TimedOut,
			Truncated: truncated, OutputBytes: rec.OutputBytes, Output: text, UnsafeUnconfined: true,
		})
		if err != nil {
			return "", err
		}
		return string(b), nil
	})
	if mode != ExecutionUnsafeUnconfinedLocal {
		return drivers.ToolDefinition{}, false
	}
	if cfg.Logger != nil {
		cfg.Logger.Warn("worker run_command enabled in unsafe_unconfined_local mode: commands run unconfined as the local user",
			slog.String("marker", unsafeUnconfinedMarker))
	}
	return def, true
}

func validateArgv(argv []string) error {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return cmdErr(errs.CategoryInvalidArgument, "invalid_argv", "argv must be a non-empty list whose first element is the program")
	}
	if len(argv) > maxArgvEntries {
		return cmdErr(errs.CategoryInvalidArgument, "invalid_argv", "argv has %d entries, maximum is %d", len(argv), maxArgvEntries)
	}
	total := 0
	for _, a := range argv {
		if strings.IndexByte(a, 0) >= 0 {
			return cmdErr(errs.CategoryInvalidArgument, "invalid_argv", "argv entries must not contain NUL")
		}
		total += len(a)
	}
	if total > maxArgvBytes {
		return cmdErr(errs.CategoryInvalidArgument, "invalid_argv", "argv is %d bytes, maximum is %d", total, maxArgvBytes)
	}
	return nil
}

// checkExecutable applies the best-effort basename denylist and refuses
// relative-path executables (bare names are resolved on PATH by the Runner).
func checkExecutable(exe string) error {
	if strings.ContainsAny(exe, `/\`) && !filepath.IsAbs(exe) {
		return cmdErr(errs.CategoryPolicyDenied, "denied_executable", "relative-path executable %q is refused; use a bare program name on PATH", exe)
	}
	base := strings.ToLower(filepath.Base(exe))
	base = strings.TrimSuffix(base, ".exe")
	if deniedExecutables[base] {
		return cmdErr(errs.CategoryPolicyDenied, "denied_executable",
			"%q is not allowed (no git/gh/ssh/scp/curl/wget, shells, or privilege/exec wrappers); run the program directly with argv", base)
	}
	return nil
}

// resolveCommandDir returns the absolute, symlink-free directory for cwd,
// confined to the worktree and never inside .git.
func resolveCommandDir(scope *tools.Scope, cwd string) (string, error) {
	root, err := filepath.EvalSymlinks(scope.WorktreePath)
	if err != nil {
		return "", errs.Wrap(errs.CategoryInternal, err, "run_command: resolve worktree")
	}
	if cwd == "" || cwd == "." {
		return root, nil
	}
	if filepath.IsAbs(cwd) {
		return "", cmdErr(errs.CategoryPolicyDenied, "invalid_cwd", "cwd must be relative to the worktree, got absolute %q", cwd)
	}
	clean := filepath.Clean(cwd)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", cmdErr(errs.CategoryPolicyDenied, "invalid_cwd", "cwd %q escapes the worktree", cwd)
	}
	if isGitPath(clean) {
		return "", cmdErr(errs.CategoryPolicyDenied, "invalid_cwd", "cwd inside .git is forbidden")
	}
	resolved, err := scope.ResolvePath(clean)
	if err != nil {
		return "", errs.Wrap(errs.CategoryPolicyDenied, err, "run_command: cwd containment violation")
	}
	if err := refuseSymlinkedPath(scope, clean, resolved); err != nil {
		return "", errs.Wrap(errs.CategoryPolicyDenied, err, "run_command: refusing symlinked cwd %q", cwd)
	}
	fi, err := os.Stat(resolved)
	if err != nil || !fi.IsDir() {
		return "", cmdErr(errs.CategoryInvalidArgument, "invalid_cwd", "cwd %q is not an existing directory", cwd)
	}
	return resolved, nil
}
