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

	"github.com/olostan/DevCadence/internal/credentials"
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

// CLIInvocationMapper customizes argument and environment generation for CLI subprocess invocations.
type CLIInvocationMapper interface {
	BuildArgs(cfg SessionConfig, input TurnInput, backendHandle string) ([]string, error)
}

// CLIWrapperOptions configures the CLI wrapper driver.
type CLIWrapperOptions struct {
	Binary                        string
	BaseArgs                      []string
	SafeEnv                       map[string]string // Non-secret environment overrides merged with process.BaseEnv()
	DefaultDir                    string            // Fallback absolute directory if WorktreeScope is absent
	Timeout                       time.Duration     // Wall-clock command timeout
	ResumeFlag                    string            // e.g. "--resume" or "--session-id"
	PromptFlag                    string            // e.g. "-p" or "exec"
	ModelFlag                     string            // e.g. "--model"
	SystemPromptFlag              string            // e.g. "--system"
	ToolsFlag                     string            // e.g. "--tools"
	ToolResultsFlag               string            // e.g. "--tool-results"
	AllowLogicalSessionIDAsHandle bool              // If true, allows using DevCadence SessionID as resume handle
	VerifiedSandbox               bool              // If true, declares NativeWorktreeAccess
	InvocationMapper              CLIInvocationMapper
	Capabilities                  *DriverCapabilities
}

// CLIWrapperDriver normalizes external coding CLIs (e.g. Codex CLI, Claude Code)
// into the unified SessionDriver interface, executing under the controlled process boundary
// of internal/process.Runner (docs/SECURITY.md §5, DCI-033, DCI-055).
//
// Native Filesystem Policy:
// Setting process.Spec.Dir sets execution CWD to Scope.WorktreePath, but does NOT provide
// kernel-level filesystem containment. Unless an explicit verified sandbox provider is configured
// (VerifiedSandbox: true), NativeWorktreeAccess is false, and write mutations must be routed through
// mediated DevCadence tools (ToolMediator) for verified containment.
type CLIWrapperDriver struct {
	id           string
	runner       CommandRunner
	opts         CLIWrapperOptions
	capabilities DriverCapabilities
	mu           sync.RWMutex
	sessions     map[string]*cliSession
}

// NewCLIWrapperDriver constructs a CLI wrapper driver, failing fast if secrets appear in configuration.
func NewCLIWrapperDriver(id string, runner CommandRunner, opts CLIWrapperOptions) (*CLIWrapperDriver, error) {
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

	// Validate BaseArgs and SafeEnv at creation boundary (Directive 6 / DCI-081)
	for _, arg := range opts.BaseArgs {
		if protocol.LooksLikeSecret(arg) {
			return nil, errs.New(errs.CategoryInvalidArgument, "cli_wrapper: base argument %q looks like a secret value", arg)
		}
	}
	for k, v := range opts.SafeEnv {
		if protocol.LooksLikeSecret(k) || protocol.LooksLikeSecret(v) {
			return nil, errs.New(errs.CategoryInvalidArgument, "cli_wrapper: safe env %s contains a secret-looking value", k)
		}
	}

	caps := DriverCapabilities{
		Kind:                  protocol.ChannelCLISubprocess,
		SessionMode:           protocol.SessionResumableHandle,
		ContextControl:        protocol.ContextControlOpaqueSession,
		PrefixCache:           protocol.PrefixCacheNone,
		SupportsStreaming:     true,
		SupportsTools:         true,
		NativeWorktreeAccess:  opts.VerifiedSandbox,
		MaxConcurrentRequests: 1,
	}

	if opts.Capabilities != nil {
		caps = *opts.Capabilities
		if caps.NativeWorktreeAccess && !opts.VerifiedSandbox {
			return nil, errs.New(errs.CategoryInvalidArgument, "cli_wrapper: NativeWorktreeAccess cannot be true without VerifiedSandbox")
		}
	}

	if err := caps.Validate(); err != nil {
		return nil, err
	}

	return &CLIWrapperDriver{
		id:           id,
		runner:       runner,
		opts:         opts,
		capabilities: caps,
		sessions:     make(map[string]*cliSession),
	}, nil
}

// MustNewCLIWrapperDriver constructs a CLI wrapper driver or panics on invalid configuration.
func MustNewCLIWrapperDriver(id string, runner CommandRunner, opts CLIWrapperOptions) *CLIWrapperDriver {
	d, err := NewCLIWrapperDriver(id, runner, opts)
	if err != nil {
		panic(err)
	}
	return d
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

	// Validate capability and flag mappings: fail closed rather than silently ignoring
	if cfg.ModelID != "" && d.opts.ModelFlag == "" && d.opts.InvocationMapper == nil {
		return nil, errs.New(errs.CategoryUnsupported, "cli driver %q: model_id specified but ModelFlag is not configured", d.id)
	}
	if cfg.SystemPrompt != "" && d.opts.SystemPromptFlag == "" && d.opts.InvocationMapper == nil {
		return nil, errs.New(errs.CategoryUnsupported, "cli driver %q: system_prompt specified but SystemPromptFlag is not configured", d.id)
	}
	if len(cfg.Tools) > 0 && d.opts.ToolsFlag == "" && d.opts.InvocationMapper == nil {
		return nil, errs.New(errs.CategoryUnsupported, "cli driver %q: tools specified but ToolsFlag is not configured", d.id)
	}
	if len(cfg.Tools) > 0 && !d.capabilities.SupportsTools {
		return nil, errs.New(errs.CategoryUnsupported, "cli driver %q does not support tools", d.id)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.sessions[cfg.SessionID]; exists {
		return nil, errs.New(errs.CategoryConflict, "session %q already exists", cfg.SessionID)
	}

	if cfg.Mediator != nil {
		if scoped, ok := cfg.Mediator.(*ScopedToolMediator); ok {
			cfg.Mediator = scoped.ForSession(cfg.SessionID, cfg.Tools)
		} else {
			cfg.Mediator.SetDeclaredTools(cfg.Tools)
		}
	}

	var backendHandle string
	if cfg.Options != nil {
		backendHandle = cfg.Options["backend_session_handle"]
	}

	s := &cliSession{
		driver:               d,
		config:               cfg.DeepCopy(),
		status:               SessionStatusActive,
		backendSessionHandle: backendHandle,
	}
	d.sessions[cfg.SessionID] = s
	return s, nil
}

// ResumeSession restores or connects to an existing CLI session.
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

		if cfg.ModelID != "" && d.opts.ModelFlag == "" && d.opts.InvocationMapper == nil {
			return nil, errs.New(errs.CategoryUnsupported, "cli driver %q: model_id specified but ModelFlag is not configured", d.id)
		}
		if cfg.SystemPrompt != "" && d.opts.SystemPromptFlag == "" && d.opts.InvocationMapper == nil {
			return nil, errs.New(errs.CategoryUnsupported, "cli driver %q: system_prompt specified but SystemPromptFlag is not configured", d.id)
		}
		if len(cfg.Tools) > 0 && d.opts.ToolsFlag == "" && d.opts.InvocationMapper == nil {
			return nil, errs.New(errs.CategoryUnsupported, "cli driver %q: tools specified but ToolsFlag is not configured", d.id)
		}
		if len(cfg.Tools) > 0 && !d.capabilities.SupportsTools {
			return nil, errs.New(errs.CategoryUnsupported, "cli driver %q does not support tools", d.id)
		}

		backendHandle := ""
		if cfg.Options != nil {
			backendHandle = cfg.Options["backend_session_handle"]
		}
		if backendHandle == "" && d.opts.AllowLogicalSessionIDAsHandle {
			backendHandle = sessionID
		}
		if backendHandle == "" {
			return nil, errs.New(errs.CategoryInvalidTransition, "cli driver %q: cannot resume opaque session %q without backend session handle", d.id, sessionID)
		}

		if cfg.Mediator != nil {
			if scoped, ok := cfg.Mediator.(*ScopedToolMediator); ok {
				cfg.Mediator = scoped.ForSession(cfg.SessionID, cfg.Tools)
			} else {
				cfg.Mediator.SetDeclaredTools(cfg.Tools)
			}
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

// BackendSessionHandle returns the opaque backend session handle, if captured.
func (s *cliSession) BackendSessionHandle() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.backendSessionHandle
}

func (s *cliSession) setBackendSessionHandle(handle string) {
	if handle == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.backendSessionHandle = handle
	if s.config.Options == nil {
		s.config.Options = make(map[string]string)
	}
	s.config.Options["backend_session_handle"] = handle
}

func (s *cliSession) buildArgs(input TurnInput) ([]string, error) {
	if s.driver.opts.InvocationMapper != nil {
		return s.driver.opts.InvocationMapper.BuildArgs(s.config, input, s.backendSessionHandle)
	}

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
		toolsJSON, err := json.Marshal(s.config.Tools)
		if err != nil {
			return nil, errs.Wrap(errs.CategoryInternal, err, "failed marshaling tools")
		}
		args = append(args, s.driver.opts.ToolsFlag, string(toolsJSON))
	}

	// Convey tool results if provided
	if len(input.ToolResults) > 0 {
		if s.driver.opts.ToolResultsFlag == "" {
			return nil, errs.New(errs.CategoryUnsupported, "cli driver %q: tool_results provided but ToolResultsFlag is not configured", s.driver.id)
		}
		resultsJSON, err := json.Marshal(input.ToolResults)
		if err != nil {
			return nil, errs.Wrap(errs.CategoryInternal, err, "failed marshaling tool results")
		}
		args = append(args, s.driver.opts.ToolResultsFlag, string(resultsJSON))
	}

	// Resume using backend opaque session handle if available
	if s.backendSessionHandle != "" && s.driver.opts.ResumeFlag != "" {
		args = append(args, s.driver.opts.ResumeFlag, s.backendSessionHandle)
	}

	if s.driver.opts.PromptFlag != "" {
		args = append(args, s.driver.opts.PromptFlag)
	}
	if input.Prompt != "" {
		args = append(args, input.Prompt)
	}
	return args, nil
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

	if len(input.ToolResults) > 0 && s.driver.opts.ToolResultsFlag == "" && s.driver.opts.InvocationMapper == nil {
		return TurnResult{}, errs.New(errs.CategoryUnsupported, "cli driver %q: tool_results provided but ToolResultsFlag is not configured", s.driver.id)
	}

	s.mu.Lock()
	if s.status == SessionStatusClosed {
		s.mu.Unlock()
		return TurnResult{}, errs.New(errs.CategoryInvalidTransition, "session is closed")
	}
	args, argErr := s.buildArgs(input)
	s.mu.Unlock()
	if argErr != nil {
		return TurnResult{}, argErr
	}

	spec := s.buildSpec(args, s.driver.opts.Timeout)

	// Enforce process secret boundary at execution time (Directive 6 / DCI-081)
	if err := credentials.ValidateProcessSpecNoSecrets(spec); err != nil {
		return TurnResult{}, err
	}

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

	if backendHandle != "" && backendHandle != s.ID() {
		s.setBackendSessionHandle(backendHandle)
	}

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

	if len(input.ToolResults) > 0 && s.driver.opts.ToolResultsFlag == "" && s.driver.opts.InvocationMapper == nil {
		return nil, errs.New(errs.CategoryUnsupported, "cli driver %q: tool_results provided but ToolResultsFlag is not configured", s.driver.id)
	}

	s.mu.Lock()
	if s.status == SessionStatusClosed {
		s.mu.Unlock()
		return nil, errs.New(errs.CategoryInvalidTransition, "session is closed")
	}
	args, argErr := s.buildArgs(input)
	s.mu.Unlock()
	if argErr != nil {
		return nil, argErr
	}

	spec := s.buildSpec(args, s.driver.opts.Timeout)

	// Enforce process secret boundary at execution time (Directive 6 / DCI-081)
	if err := credentials.ValidateProcessSpecNoSecrets(spec); err != nil {
		return nil, err
	}

	stdoutR, stdoutW := io.Pipe()
	spec.StdoutSink = stdoutW

	// Concurrently drain stderr to avoid pipe buffer deadlocks
	var stderrBuf safeBuffer
	spec.StderrSink = &stderrBuf

	outStream := NewChannelEventStream(32)

	// runCtx is cancelled when stream is closed or parent context cancels,
	// triggering process group killing and tree reaping in process.Runner
	runCtx, cancelRun := context.WithCancel(ctx)

	// Set synchronous cancellation hook so closing outStream immediately halts subprocess
	outStream.SetOnClose(func() {
		cancelRun()
		_ = stdoutR.Close()
	})

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

	// Scanner goroutine consuming stdout with bounded buffer
	go func() {
		defer func() {
			cancelRun() // Ensure cancelled before waiting for process termination
			wg.Wait()
			_ = stdoutR.Close()
		}()

		scanner := bufio.NewScanner(stdoutR)
		const maxLineSize = 10 * 1024 * 1024
		scanner.Buffer(make([]byte, 64*1024), maxLineSize)

		for scanner.Scan() {
			line := scanner.Text()
			var ev DriverEvent
			if jsonErr := json.Unmarshal([]byte(line), &ev); jsonErr == nil && ev.Kind != "" {
				if ev.SessionID != "" && ev.SessionID != s.ID() {
					s.setBackendSessionHandle(ev.SessionID)
				}
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
	}()

	return outStream, nil
}

// parseCLIStdout parses process output into content, tool calls, usage, and backend session handle.
func parseCLIStdout(output []byte) (string, []ToolCall, TokenUsage, string) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	const maxLineSize = 10 * 1024 * 1024
	scanner.Buffer(make([]byte, 64*1024), maxLineSize)

	var contentBuilder strings.Builder
	var toolCalls []ToolCall
	usage := KnownZeroUsage()
	var sawUsage bool
	var backendHandle string

	for scanner.Scan() {
		line := scanner.Text()
		var ev DriverEvent
		if err := json.Unmarshal([]byte(line), &ev); err == nil && ev.Kind != "" {
			switch ev.Kind {
			case EventContentDelta:
				contentBuilder.WriteString(ev.Delta)
			case EventToolCall:
				if ev.ToolCall != nil {
					toolCalls = append(toolCalls, *ev.ToolCall)
				}
			case EventTurnCompleted:
				if ev.Usage != nil {
					usage = usage.Add(*ev.Usage)
					sawUsage = true
				}
				if ev.SessionID != "" {
					backendHandle = ev.SessionID
				}
			}
		} else {
			contentBuilder.WriteString(line)
			contentBuilder.WriteString("\n")
		}
	}

	if !sawUsage {
		usage = TokenUsage{}
	}

	return contentBuilder.String(), toolCalls, usage, backendHandle
}
