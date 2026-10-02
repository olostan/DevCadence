package drivers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// CommandSpec defines the parameters for executing an external CLI subprocess.
type CommandSpec struct {
	Binary string
	Args   []string
	Dir    string
	Env    []string
	Stdin  io.Reader
}

// CommandOutput captures the exit output of a CLI command.
type CommandOutput struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// ProcessHandle provides control over a running subprocess.
type ProcessHandle interface {
	Stdout() io.ReadCloser
	Stderr() io.ReadCloser
	Wait() (*CommandOutput, error)
	Kill() error
}

// CommandRunner abstracts subprocess execution for deterministic testing and OS isolation.
type CommandRunner interface {
	Run(ctx context.Context, spec CommandSpec) (*CommandOutput, error)
	Start(ctx context.Context, spec CommandSpec) (ProcessHandle, error)
}

// OSCommandRunner executes real OS commands via os/exec.
type OSCommandRunner struct{}

func (r *OSCommandRunner) Run(ctx context.Context, spec CommandSpec) (*CommandOutput, error) {
	cmd := exec.CommandContext(ctx, spec.Binary, spec.Args...)
	if spec.Dir != "" {
		cmd.Dir = spec.Dir
	}
	if len(spec.Env) > 0 {
		cmd.Env = spec.Env
	}
	if spec.Stdin != nil {
		cmd.Stdin = spec.Stdin
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, err
		}
	}

	return &CommandOutput{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: exitCode,
	}, nil
}

type osProcessHandle struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr io.ReadCloser
}

func (h *osProcessHandle) Stdout() io.ReadCloser { return h.stdout }
func (h *osProcessHandle) Stderr() io.ReadCloser { return h.stderr }
func (h *osProcessHandle) Kill() error {
	if h.cmd.Process != nil {
		return h.cmd.Process.Kill()
	}
	return nil
}
func (h *osProcessHandle) Wait() (*CommandOutput, error) {
	err := h.cmd.Wait()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, err
		}
	}
	return &CommandOutput{
		ExitCode: exitCode,
	}, nil
}

func (r *OSCommandRunner) Start(ctx context.Context, spec CommandSpec) (ProcessHandle, error) {
	cmd := exec.CommandContext(ctx, spec.Binary, spec.Args...)
	if spec.Dir != "" {
		cmd.Dir = spec.Dir
	}
	if len(spec.Env) > 0 {
		cmd.Env = spec.Env
	}
	if spec.Stdin != nil {
		cmd.Stdin = spec.Stdin
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdout.Close()
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		stdout.Close()
		stderr.Close()
		return nil, err
	}

	return &osProcessHandle{
		cmd:    cmd,
		stdout: stdout,
		stderr: stderr,
	}, nil
}

// CLIWrapperOptions configures the CLI wrapper driver.
type CLIWrapperOptions struct {
	Binary       string
	BaseArgs     []string
	Env          []string
	ResumeFlag   string // e.g. "--resume" or "--session-id"
	PromptFlag   string // e.g. "-p" or "exec"
	Capabilities *DriverCapabilities
}

// CLIWrapperDriver normalizes external coding CLIs (e.g. Codex CLI, Claude Code)
// into the unified SessionDriver interface.
type CLIWrapperDriver struct {
	id           string
	runner       CommandRunner
	opts         CLIWrapperOptions
	capabilities DriverCapabilities
	mu           sync.RWMutex
	sessions     map[string]*cliSession
}

// NewCLIWrapperDriver constructs a CLI wrapper driver.
func NewCLIWrapperDriver(id string, runner CommandRunner, opts CLIWrapperOptions) *CLIWrapperDriver {
	if runner == nil {
		runner = &OSCommandRunner{}
	}
	if opts.Binary == "" {
		opts.Binary = "mock-cli"
	}
	if opts.ResumeFlag == "" {
		opts.ResumeFlag = "--resume"
	}

	caps := DriverCapabilities{
		Kind:                  protocol.ChannelCLISubprocess,
		SessionMode:           protocol.SessionResumableHandle,
		ContextControl:        protocol.ContextControlOpaqueSession,
		PrefixCache:           protocol.PrefixCacheNone,
		SupportsStreaming:     true,
		SupportsTools:         true,
		NativeWorktreeAccess:  true,
		MaxConcurrentRequests: 1,
	}

	if opts.Capabilities != nil {
		caps = *opts.Capabilities
	}

	return &CLIWrapperDriver{
		id:           id,
		runner:       runner,
		opts:         opts,
		capabilities: caps,
		sessions:     make(map[string]*cliSession),
	}
}

// ID returns the driver identifier.
func (d *CLIWrapperDriver) ID() string { return d.id }

// Capabilities returns driver capabilities.
func (d *CLIWrapperDriver) Capabilities() DriverCapabilities { return d.capabilities }

// StartSession initializes a new CLI session.
func (d *CLIWrapperDriver) StartSession(ctx context.Context, cfg SessionConfig) (Session, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.sessions[cfg.SessionID]; exists {
		return nil, errs.New(errs.CategoryConflict, "session %q already exists", cfg.SessionID)
	}

	s := &cliSession{
		driver:    d,
		config:    cfg,
		status:    SessionStatusActive,
		isResumed: false,
	}
	d.sessions[cfg.SessionID] = s
	return s, nil
}

// ResumeSession resumes an existing CLI session by ID.
func (d *CLIWrapperDriver) ResumeSession(ctx context.Context, sessionID string, cfg SessionConfig) (Session, error) {
	if sessionID == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "sessionID cannot be empty")
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	s, exists := d.sessions[sessionID]
	if !exists {
		if cfg.SessionID == "" {
			cfg.SessionID = sessionID
		}
		if err := cfg.Validate(); err != nil {
			return nil, err
		}
		s = &cliSession{
			driver:    d,
			config:    cfg,
			status:    SessionStatusActive,
			isResumed: true,
		}
		d.sessions[sessionID] = s
	} else {
		s.isResumed = true
		if s.status == SessionStatusClosed {
			s.status = SessionStatusActive
		}
	}

	return s, nil
}

type cliSession struct {
	driver    *CLIWrapperDriver
	config    SessionConfig
	status    SessionStatus
	isResumed bool
	mu        sync.Mutex
}

func (s *cliSession) ID() string { return s.config.SessionID }

func (s *cliSession) DriverID() string { return s.driver.ID() }

func (s *cliSession) Config() SessionConfig { return s.config }

func (s *cliSession) Status() SessionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *cliSession) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = SessionStatusClosed
	return nil
}

func (s *cliSession) buildArgs(prompt string) []string {
	args := append([]string(nil), s.driver.opts.BaseArgs...)
	if s.isResumed {
		args = append(args, s.driver.opts.ResumeFlag, s.config.SessionID)
	}
	if s.driver.opts.PromptFlag != "" {
		args = append(args, s.driver.opts.PromptFlag)
	}
	if prompt != "" {
		args = append(args, prompt)
	}
	return args
}

func (s *cliSession) workingDir() string {
	if s.config.WorktreeScope != nil && s.config.WorktreeScope.WorktreePath != "" {
		return s.config.WorktreeScope.WorktreePath
	}
	return ""
}

func (s *cliSession) ExecuteTurn(ctx context.Context, input TurnInput) (TurnResult, error) {
	if err := ctx.Err(); err != nil {
		return TurnResult{}, err
	}

	s.mu.Lock()
	if s.status == SessionStatusClosed {
		s.mu.Unlock()
		return TurnResult{}, errs.New(errs.CategoryInvalidTransition, "session is closed")
	}
	s.mu.Unlock()

	spec := CommandSpec{
		Binary: s.driver.opts.Binary,
		Args:   s.buildArgs(input.Prompt),
		Dir:    s.workingDir(),
		Env:    s.driver.opts.Env,
	}

	start := time.Now()
	out, err := s.driver.runner.Run(ctx, spec)
	duration := time.Since(start)
	if err != nil {
		return TurnResult{}, err
	}
	if out.ExitCode != 0 {
		return TurnResult{}, errs.New(errs.CategoryInternal, "cli process exited with code %d: %s", out.ExitCode, string(out.Stderr))
	}

	// Session is now in resumed state for subsequent turns
	s.mu.Lock()
	s.isResumed = true
	s.mu.Unlock()

	// Parse stdout: could be JSON event stream or raw text
	content, toolCalls, usage := parseCLIStdout(out.Stdout)

	// If tools were called and mediator is available, execute them
	if len(toolCalls) > 0 && s.config.Mediator != nil {
		for _, tc := range toolCalls {
			_, _ = s.config.Mediator.ExecuteTool(ctx, tc)
		}
	}

	return TurnResult{
		TurnID:    input.TurnID,
		Content:   content,
		ToolCalls: toolCalls,
		Usage:     usage,
		Duration:  duration,
	}, nil
}

func (s *cliSession) StreamTurn(ctx context.Context, input TurnInput) (EventStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	if s.status == SessionStatusClosed {
		s.mu.Unlock()
		return nil, errs.New(errs.CategoryInvalidTransition, "session is closed")
	}
	s.mu.Unlock()

	spec := CommandSpec{
		Binary: s.driver.opts.Binary,
		Args:   s.buildArgs(input.Prompt),
		Dir:    s.workingDir(),
		Env:    s.driver.opts.Env,
	}

	handle, err := s.driver.runner.Start(ctx, spec)
	if err != nil {
		return nil, err
	}

	outStream := NewChannelEventStream(16)

	go func() {
		defer handle.Stdout().Close()
		defer handle.Stderr().Close()

		// Goroutine to monitor context cancellation and kill process
		stopKill := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				_ = handle.Kill()
			case <-stopKill:
			}
		}()
		defer close(stopKill)

		scanner := bufio.NewScanner(handle.Stdout())
		for scanner.Scan() {
			line := scanner.Text()
			// Attempt to parse as structured DriverEvent JSON
			var ev DriverEvent
			if jsonErr := json.Unmarshal([]byte(line), &ev); jsonErr == nil && ev.Kind != "" {
				ev.SessionID = s.ID()
				ev.TurnID = input.TurnID
				if ev.Timestamp.IsZero() {
					ev.Timestamp = time.Now()
				}
				if !outStream.Send(ev) {
					return
				}
			} else {
				// Treat as plain text delta
				if !outStream.Send(DriverEvent{
					Kind:      EventContentDelta,
					SessionID: s.ID(),
					TurnID:    input.TurnID,
					Delta:     line + "\n",
					Timestamp: time.Now(),
				}) {
					return
				}
			}
		}

		out, waitErr := handle.Wait()
		if waitErr != nil {
			outStream.CloseWithError(waitErr)
			return
		}
		if out != nil && out.ExitCode != 0 {
			outStream.CloseWithError(errs.New(errs.CategoryInternal, "cli process exited with code %d", out.ExitCode))
			return
		}

		s.mu.Lock()
		s.isResumed = true
		s.mu.Unlock()

		outStream.CloseWithError(nil)
	}()

	return outStream, nil
}

// parseCLIStdout parses process output into content, tool calls, and usage.
func parseCLIStdout(output []byte) (string, []ToolCall, TokenUsage) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	var contentBuilder strings.Builder
	var toolCalls []ToolCall
	var usage TokenUsage

	for scanner.Scan() {
		line := scanner.Text()
		var ev DriverEvent
		if err := json.Unmarshal([]byte(line), &ev); err == nil && ev.Kind != "" {
			if ev.Delta != "" {
				contentBuilder.WriteString(ev.Delta)
			}
			if ev.ToolCall != nil {
				toolCalls = append(toolCalls, *ev.ToolCall)
			}
			if ev.Usage != nil {
				usage = usage.Add(*ev.Usage)
			}
		} else {
			contentBuilder.WriteString(line)
			contentBuilder.WriteString("\n")
		}
	}

	content := strings.TrimRight(contentBuilder.String(), "\n")
	if usage.Total() == 0 && len(content) > 0 {
		// Provide basic token estimation if CLI does not report usage
		usage.OutputTokens = int64(len(strings.Fields(content)))
	}

	return content, toolCalls, usage
}
