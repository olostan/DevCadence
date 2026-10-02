package drivers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
)

// DefaultCLITimeout is the default wall-clock timeout for CLI execution.
const DefaultCLITimeout = 2 * time.Minute

// CommandRunner abstracts execution of controlled processes, matching process.Runner.
type CommandRunner interface {
	Run(ctx context.Context, spec process.Spec) (process.Result, error)
}

// CLIWrapperOptions configures the CLI wrapper driver.
type CLIWrapperOptions struct {
	Binary           string
	BaseArgs         []string
	SafeEnv          map[string]string // Non-secret environment overrides merged with process.BaseEnv()
	DefaultDir       string            // Fallback absolute directory if WorktreeScope is absent
	Timeout          time.Duration     // Wall-clock command timeout
	ResumeFlag       string            // e.g. "--resume" or "--session-id"
	PromptFlag       string            // e.g. "-p" or "exec"
	ModelFlag        string            // e.g. "--model"
	SystemPromptFlag string            // e.g. "--system"
	ToolsFlag        string            // e.g. "--tools"
	Capabilities     *DriverCapabilities
}

// CLIWrapperDriver normalizes external coding CLIs (e.g. Codex CLI, Claude Code)
// into the unified SessionDriver interface, executing under the controlled process boundary
// of internal/process.Runner (docs/SECURITY.md §5, DCI-033, DCI-055).
//
// Native Filesystem Policy:
// When NativeWorktreeAccess is declared, the CLI subprocess is confined by setting
// process.Spec.Dir strictly to Scope.WorktreePath and executing within the non-inherited
// environment (process.BaseEnv()), preventing directory traversal or ambient secret leakage.
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
		runner = process.NewRunner()
	}
	if opts.Binary == "" {
		opts.Binary = "mock-cli"
	}
	if opts.ResumeFlag == "" {
		opts.ResumeFlag = "--resume"
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultCLITimeout
	}
	if opts.DefaultDir == "" {
		opts.DefaultDir = os.TempDir()
	}
	if abs, err := filepath.Abs(opts.DefaultDir); err == nil {
		opts.DefaultDir = abs
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

	// Validate capability constraints: reject unsupported tool declaration
	if len(cfg.Tools) > 0 && !d.capabilities.SupportsTools {
		return nil, errs.New(errs.CategoryUnsupported, "cli driver %q does not support tools", d.id)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.sessions[cfg.SessionID]; exists {
		return nil, errs.New(errs.CategoryConflict, "session %q already exists", cfg.SessionID)
	}

	if cfg.Mediator != nil && len(cfg.Tools) > 0 {
		cfg.Mediator.SetDeclaredTools(cfg.Tools)
	}

	s := &cliSession{
		driver:               d,
		config:               cfg.DeepCopy(),
		status:               SessionStatusActive,
		backendSessionHandle: "", // initially unset; recorded from backend on first turn
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
		if cfg.Mediator != nil && len(cfg.Tools) > 0 {
			cfg.Mediator.SetDeclaredTools(cfg.Tools)
		}
		// If caller provided a backend session handle in options, honor it
		backendHandle := ""
		if cfg.Options != nil {
			backendHandle = cfg.Options["backend_session_handle"]
		}
		if backendHandle == "" {
			backendHandle = sessionID
		}
		s = &cliSession{
			driver:               d,
			config:               cfg.DeepCopy(),
			status:               SessionStatusActive,
			backendSessionHandle: backendHandle,
		}
		d.sessions[sessionID] = s
	} else if s.status == SessionStatusClosed {
		s.status = SessionStatusActive
	}

	return s, nil
}

type cliSession struct {
	driver               *CLIWrapperDriver
	config               SessionConfig
	status               SessionStatus
	backendSessionHandle string
	mu                   sync.Mutex
}

func (s *cliSession) ID() string { return s.config.SessionID }

func (s *cliSession) DriverID() string { return s.driver.ID() }

// Config returns an immutable deep copy of session configuration.
func (s *cliSession) Config() SessionConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config.DeepCopy()
}

func (s *cliSession) Status() SessionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *cliSession) Close(ctx context.Context) error {
	s.mu.Lock()
	s.status = SessionStatusClosed
	sessID := s.config.SessionID
	s.mu.Unlock()

	// Clean up from driver registry
	s.driver.mu.Lock()
	delete(s.driver.sessions, sessID)
	s.driver.mu.Unlock()

	return nil
}

func (s *cliSession) buildArgs(prompt string) []string {
	args := append([]string(nil), s.driver.opts.BaseArgs...)

	// Convey model ID if flag configured
	if s.driver.opts.ModelFlag != "" && s.config.ModelID != "" {
		args = append(args, s.driver.opts.ModelFlag, s.config.ModelID)
	}

	// Convey system prompt if flag configured
	if s.driver.opts.SystemPromptFlag != "" && s.config.SystemPrompt != "" {
		args = append(args, s.driver.opts.SystemPromptFlag, s.config.SystemPrompt)
	}

	// Convey declared tools if flag configured
	if s.driver.opts.ToolsFlag != "" && len(s.config.Tools) > 0 {
		if toolsJSON, err := json.Marshal(s.config.Tools); err == nil {
			args = append(args, s.driver.opts.ToolsFlag, string(toolsJSON))
		}
	}

	// Resume using backend opaque session handle if available
	if s.backendSessionHandle != "" && s.driver.opts.ResumeFlag != "" {
		args = append(args, s.driver.opts.ResumeFlag, s.backendSessionHandle)
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
	return s.driver.opts.DefaultDir
}

func (s *cliSession) buildSpec(args []string, timeout time.Duration) process.Spec {
	if timeout <= 0 {
		timeout = s.driver.opts.Timeout
	}
	return process.Spec{
		Executable: s.driver.opts.Binary,
		Args:       args,
		Dir:        s.workingDir(),
		Env:        process.MergeEnv(process.BaseEnv(), s.driver.opts.SafeEnv),
		Timeout:    timeout,
	}
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
	args := s.buildArgs(input.Prompt)
	s.mu.Unlock()

	spec := s.buildSpec(args, s.driver.opts.Timeout)

	start := time.Now()
	res, err := s.driver.runner.Run(ctx, spec)
	duration := time.Since(start)

	if err != nil {
		return TurnResult{}, err
	}
	if res.Status == process.StatusCancelled {
		return TurnResult{}, ctx.Err()
	}
	if res.Status == process.StatusTimeout {
		return TurnResult{}, errs.New(errs.CategoryProbeTimeout, "cli process timed out after %v", spec.Timeout)
	}
	if res.ExitCode != 0 {
		return TurnResult{}, errs.New(errs.CategoryInternal, "cli process exited with code %d: %s", res.ExitCode, string(res.Stderr))
	}

	// Parse stdout
	content, toolCalls, usage, backendHandle := parseCLIStdout(res.Stdout)

	s.mu.Lock()
	if backendHandle != "" {
		s.backendSessionHandle = backendHandle
	} else if s.backendSessionHandle == "" {
		// Default to session ID if backend does not emit a distinct handle
		s.backendSessionHandle = s.config.SessionID
	}
	s.mu.Unlock()

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

// safeBuffer is a concurrency-safe bytes buffer.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (s *cliSession) StreamTurn(ctx context.Context, input TurnInput) (EventStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if !s.driver.capabilities.SupportsStreaming {
		return nil, errs.New(errs.CategoryUnsupported, "cli driver %q does not support streaming", s.driver.id)
	}

	s.mu.Lock()
	if s.status == SessionStatusClosed {
		s.mu.Unlock()
		return nil, errs.New(errs.CategoryInvalidTransition, "session is closed")
	}
	args := s.buildArgs(input.Prompt)
	s.mu.Unlock()

	spec := s.buildSpec(args, s.driver.opts.Timeout)

	stdoutR, stdoutW := io.Pipe()
	stderrBuf := &safeBuffer{}

	spec.StdoutSink = stdoutW
	spec.StderrSink = stderrBuf

	outStream := NewChannelEventStream(32)

	// runCtx is cancelled when stream is closed or parent context cancels,
	// triggering process group killing and tree reaping in process.Runner
	runCtx, cancelRun := context.WithCancel(ctx)

	var wg sync.WaitGroup
	wg.Add(1)

	// Goroutine executing controlled process
	go func() {
		defer wg.Done()
		res, err := s.driver.runner.Run(runCtx, spec)
		if err != nil {
			_ = stdoutW.CloseWithError(err)
			return
		}
		if res.Status == process.StatusCancelled {
			_ = stdoutW.CloseWithError(runCtx.Err())
			return
		}
		if res.Status == process.StatusTimeout {
			_ = stdoutW.CloseWithError(errs.New(errs.CategoryProbeTimeout, "cli process timed out after %v", spec.Timeout))
			return
		}
		if res.ExitCode != 0 {
			_ = stdoutW.CloseWithError(errs.New(errs.CategoryInternal, "cli process exited with code %d: %s", res.ExitCode, stderrBuf.String()))
			return
		}
		_ = stdoutW.Close()
	}()

	// Scanner goroutine consuming stdout with large buffer (up to 10MB tokens)
	go func() {
		defer stdoutR.Close()
		defer cancelRun()

		scanner := bufio.NewScanner(stdoutR)
		// Support lines up to 10 MiB to prevent scanner buffer overflow on large tool responses
		const maxLineSize = 10 * 1024 * 1024
		scanner.Buffer(make([]byte, 64*1024), maxLineSize)

		for scanner.Scan() {
			line := scanner.Text()
			var ev DriverEvent
			if jsonErr := json.Unmarshal([]byte(line), &ev); jsonErr == nil && ev.Kind != "" {
				ev.SessionID = s.ID()
				ev.TurnID = input.TurnID
				if ev.Timestamp.IsZero() {
					ev.Timestamp = time.Now()
				}
				if !outStream.Send(ev) {
					break
				}
			} else {
				if !outStream.Send(DriverEvent{
					Kind:      EventContentDelta,
					SessionID: s.ID(),
					TurnID:    input.TurnID,
					Delta:     line + "\n",
					Timestamp: time.Now(),
				}) {
					break
				}
			}
		}

		if scanErr := scanner.Err(); scanErr != nil {
			outStream.CloseWithError(scanErr)
		} else {
			outStream.CloseWithError(nil)
		}

		// Ensure runner process has terminated and reaped
		wg.Wait()

		s.mu.Lock()
		if s.backendSessionHandle == "" {
			s.backendSessionHandle = s.config.SessionID
		}
		s.mu.Unlock()
	}()

	return outStream, nil
}

// parseCLIStdout parses process output into content, tool calls, usage, and backend session handle.
func parseCLIStdout(output []byte) (string, []ToolCall, TokenUsage, string) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	// Allow scanning up to 10MB lines
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	var contentBuilder strings.Builder
	var toolCalls []ToolCall
	var usage TokenUsage
	var backendHandle string

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
			// Check for backend session handle announcement
			var rawObj map[string]any
			if json.Unmarshal([]byte(line), &rawObj) == nil {
				if val, ok := rawObj["backend_session_id"].(string); ok && val != "" {
					backendHandle = val
				} else if val, ok := rawObj["session_id"].(string); ok && val != "" {
					backendHandle = val
				}
			}
			contentBuilder.WriteString(line)
			contentBuilder.WriteString("\n")
		}
	}

	content := strings.TrimRight(contentBuilder.String(), "\n")
	if usage.Total() == 0 && len(content) > 0 {
		usage.OutputTokens = int64(len(strings.Fields(content)))
	}

	return content, toolCalls, usage, backendHandle
}
