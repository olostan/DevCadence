package drivers

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/tools"
)

type mockCommandRunner struct {
	delay      time.Duration
	lastSpec   process.Spec
	lastDir    string
	lastArgs   []string
	killedLast bool
	mu         sync.Mutex
}

func (r *mockCommandRunner) Run(ctx context.Context, spec process.Spec) (process.Result, error) {
	if err := ctx.Err(); err != nil {
		r.mu.Lock()
		r.killedLast = true
		r.mu.Unlock()
		return process.Result{Status: process.StatusCancelled}, err
	}

	r.mu.Lock()
	r.lastSpec = spec
	r.lastDir = spec.Dir
	r.lastArgs = spec.Args
	r.mu.Unlock()

	// Drain any input or wait
	if r.delay > 0 {
		select {
		case <-ctx.Done():
			r.mu.Lock()
			r.killedLast = true
			r.mu.Unlock()
			return process.Result{Status: process.StatusCancelled}, ctx.Err()
		case <-time.After(r.delay):
		}
	}

	// Extract prompt from args
	prompt := ""
	for i, arg := range spec.Args {
		if arg == "-p" && i+1 < len(spec.Args) {
			prompt = spec.Args[i+1]
			break
		}
	}

	var stdout strings.Builder
	if strings.Contains(prompt, "CALL_TOOL:") {
		parts := strings.Split(prompt, "CALL_TOOL:")
		toolName := strings.TrimSpace(parts[1])
		stdout.WriteString(fmt.Sprintf(`{"kind":"tool_call","tool_call":{"id":"call-cli-1","name":"%s","arguments":{"key":"val"}}}`+"\n", toolName))
	} else {
		stdout.WriteString(fmt.Sprintf("CLI response to: %s\n", prompt))
	}
	stdout.WriteString(`{"kind":"turn_completed","session_id":"cli-opaque-backend-123","usage":{"input_tokens":15,"output_tokens":30}}` + "\n")

	outBytes := []byte(stdout.String())
	if spec.StdoutSink != nil {
		_, _ = spec.StdoutSink.Write(outBytes)
	}

	// Emit some stderr to test concurrent draining
	if spec.StderrSink != nil {
		_, _ = spec.StderrSink.Write([]byte("diagnostic stderr line\n"))
	}

	return process.Result{
		Status:   process.StatusCompleted,
		ExitCode: 0,
		Stdout:   outBytes,
		Stderr:   []byte("diagnostic stderr line\n"),
	}, nil
}

func TestCLIWrapperDriverContract(t *testing.T) {
	RunDriverContractTestSuite(t, func(t *testing.T) (SessionDriver, func()) {
		runner := &mockCommandRunner{}
		driver := MustNewCLIWrapperDriver("cli-contract-driver", runner, CLIWrapperOptions{
			Binary:           "test-cli",
			PromptFlag:       "-p",
			ModelFlag:        "--model",
			SystemPromptFlag: "--system",
			ToolsFlag:        "--tools",
			ToolResultsFlag:  "--tool-results",
		})
		return driver, func() {}
	})
}

func TestCLIWrapperDriver_WorktreeDirBinding(t *testing.T) {
	runner := &mockCommandRunner{}
	tmpDir, err := os.MkdirTemp("", "cli-worktree-dir-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	driver := MustNewCLIWrapperDriver("cli-worktree-driver", runner, CLIWrapperOptions{
		Binary:     "test-cli",
		PromptFlag: "-p",
		ModelFlag:  "--model",
	})

	scope := &tools.Scope{
		ProjectID:    "proj-1",
		WorktreePath: tmpDir,
	}

	session, err := driver.StartSession(context.Background(), SessionConfig{
		SessionID:     "sess-worktree-dir",
		ModelID:       "test-model",
		WorktreeScope: scope,
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}
	defer session.Close(context.Background())

	_, err = session.ExecuteTurn(context.Background(), TurnInput{
		TurnID: "turn-dir-check",
		Prompt: "check dir",
	})
	if err != nil {
		t.Fatalf("ExecuteTurn failed: %v", err)
	}

	if runner.lastDir != tmpDir {
		t.Errorf("expected working dir %q, got %q", tmpDir, runner.lastDir)
	}
}

func TestCLIWrapperDriver_CancellationKillsProcess(t *testing.T) {
	runner := &mockCommandRunner{delay: 200 * time.Millisecond}
	driver := MustNewCLIWrapperDriver("cli-cancel-driver", runner, CLIWrapperOptions{
		Binary:     "test-cli",
		PromptFlag: "-p",
		ModelFlag:  "--model",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	session, err := driver.StartSession(context.Background(), SessionConfig{
		SessionID: "sess-cancel-kill",
		ModelID:   "test-model",
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}
	defer session.Close(context.Background())

	_, err = session.ExecuteTurn(ctx, TurnInput{
		TurnID: "turn-cancel-cli",
		Prompt: "slow operation",
	})
	if err == nil {
		t.Errorf("expected cancellation error, got nil")
	}
}

func TestCLIWrapperDriver_StreamEarlyCloseKillsProcess(t *testing.T) {
	runner := &mockCommandRunner{delay: 3 * time.Second}
	driver := MustNewCLIWrapperDriver("cli-early-close-driver", runner, CLIWrapperOptions{
		Binary:     "test-cli",
		PromptFlag: "-p",
		ModelFlag:  "--model",
	})

	session, err := driver.StartSession(context.Background(), SessionConfig{
		SessionID: "sess-early-close",
		ModelID:   "test-model",
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}
	defer session.Close(context.Background())

	stream, err := session.StreamTurn(context.Background(), TurnInput{
		TurnID: "turn-early-close",
		Prompt: "long running stream",
	})
	if err != nil {
		t.Fatalf("StreamTurn failed: %v", err)
	}

	// Close the stream early without waiting for it to finish
	startClose := time.Now()
	if err := stream.Close(); err != nil {
		t.Fatalf("stream.Close failed: %v", err)
	}
	closeDuration := time.Since(startClose)

	// Closing must be prompt and trigger cancellation of the runner
	if closeDuration > 500*time.Millisecond {
		t.Errorf("stream.Close took too long (%v), expected prompt cancellation", closeDuration)
	}

	// Verify runner was killed
	runner.mu.Lock()
	killed := runner.killedLast
	runner.mu.Unlock()

	// Wait up to 100ms for runner cancellation to propagate
	if !killed {
		time.Sleep(50 * time.Millisecond)
		runner.mu.Lock()
		killed = runner.killedLast
		runner.mu.Unlock()
	}
	if !killed {
		t.Errorf("expected runner to be cancelled/killed upon stream early close")
	}
}

func TestCLIWrapperDriver_ResumeArguments(t *testing.T) {
	runner := &mockCommandRunner{}
	driver := MustNewCLIWrapperDriver("cli-resume-driver", runner, CLIWrapperOptions{
		Binary:     "test-cli",
		PromptFlag: "-p",
		ModelFlag:  "--model",
		ResumeFlag: "--resume-session",
	})

	session, err := driver.StartSession(context.Background(), SessionConfig{
		SessionID: "sess-resume-123",
		ModelID:   "test-model",
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}
	defer session.Close(context.Background())

	// First turn: backend emits session_id="cli-opaque-backend-123"
	_, _ = session.ExecuteTurn(context.Background(), TurnInput{
		TurnID: "turn-1",
		Prompt: "hello first",
	})

	// Second turn: should have resume flag with backend session handle
	_, _ = session.ExecuteTurn(context.Background(), TurnInput{
		TurnID: "turn-2",
		Prompt: "hello second",
	})

	foundResume := false
	for i, arg := range runner.lastArgs {
		if arg == "--resume-session" && i+1 < len(runner.lastArgs) && runner.lastArgs[i+1] == "cli-opaque-backend-123" {
			foundResume = true
			break
		}
	}
	if !foundResume {
		t.Errorf("expected resume flag with backend handle in args: %v", runner.lastArgs)
	}
}

func TestCLIWrapperDriver_SecretValidationAtCreation(t *testing.T) {
	runner := &mockCommandRunner{}

	// Secret in BaseArgs
	_, err := NewCLIWrapperDriver("cli-secret-arg", runner, CLIWrapperOptions{
		Binary:   "test-cli",
		BaseArgs: []string{"--key", "ghp_123456789012345678901234567890123456"},
	})
	if err == nil {
		t.Fatalf("expected error for secret in BaseArgs, got nil")
	}
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument, got %v", err)
	}

	// Secret in SafeEnv
	_, err = NewCLIWrapperDriver("cli-secret-env", runner, CLIWrapperOptions{
		Binary: "test-cli",
		SafeEnv: map[string]string{
			"API_TOKEN": "ghp_123456789012345678901234567890123456",
		},
	})
	if err == nil {
		t.Fatalf("expected error for secret in SafeEnv, got nil")
	}
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument, got %v", err)
	}
}

func TestCLIWrapperDriver_SecretValidationAtExecution(t *testing.T) {
	runner := &mockCommandRunner{}
	driver := MustNewCLIWrapperDriver("cli-exec-secret", runner, CLIWrapperOptions{
		Binary:     "test-cli",
		PromptFlag: "-p",
		ModelFlag:  "--model",
	})

	session, err := driver.StartSession(context.Background(), SessionConfig{
		SessionID: "sess-exec-secret",
		ModelID:   "test-model",
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}
	defer session.Close(context.Background())

	// Attempt passing raw secret in prompt argument
	_, err = session.ExecuteTurn(context.Background(), TurnInput{
		TurnID: "turn-sec",
		Prompt: "ghp_123456789012345678901234567890123456",
	})
	if err == nil {
		t.Fatalf("expected secret validation failure, got nil")
	}
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument, got %v", err)
	}
}

func TestCLIWrapperDriver_UnsupportedFlagsFailClosed(t *testing.T) {
	runner := &mockCommandRunner{}
	driver := MustNewCLIWrapperDriver("cli-unsupported", runner, CLIWrapperOptions{
		Binary:     "test-cli",
		PromptFlag: "-p",
		// ModelFlag, SystemPromptFlag, ToolsFlag, ToolResultsFlag intentionally empty
	})

	ctx := context.Background()

	// ModelID without ModelFlag fails closed
	_, err := driver.StartSession(ctx, SessionConfig{
		SessionID: "sess-no-modelflag",
		ModelID:   "custom-model",
	})
	if err == nil {
		t.Errorf("expected error when model_id cannot be mapped, got nil")
	}
	if errs.CategoryOf(err) != errs.CategoryUnsupported {
		t.Errorf("expected CategoryUnsupported, got %v", err)
	}

	// Tool declaration without ToolsFlag fails closed
	driverWithModel := MustNewCLIWrapperDriver("cli-with-model", runner, CLIWrapperOptions{
		Binary:     "test-cli",
		PromptFlag: "-p",
		ModelFlag:  "--model",
	})
	_, err = driverWithModel.StartSession(ctx, SessionConfig{
		SessionID: "sess-no-toolsflag",
		ModelID:   "custom-model",
		Tools:     []ToolDefinition{{Name: "t1", Description: "d"}},
	})
	if err == nil {
		t.Errorf("expected error when tools cannot be mapped, got nil")
	}
	if errs.CategoryOf(err) != errs.CategoryUnsupported {
		t.Errorf("expected CategoryUnsupported, got %v", err)
	}

	// ToolResults without ToolResultsFlag fails closed
	sess, err := driverWithModel.StartSession(ctx, SessionConfig{
		SessionID: "sess-tool-res",
		ModelID:   "custom-model",
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}
	_, err = sess.ExecuteTurn(ctx, TurnInput{
		TurnID: "t-res",
		ToolResults: []ToolResult{
			{ToolCallID: "c1", Name: "t1", Content: "res"},
		},
	})
	if err == nil {
		t.Errorf("expected error when tool_results cannot be mapped, got nil")
	}
	if errs.CategoryOf(err) != errs.CategoryUnsupported {
		t.Errorf("expected CategoryUnsupported, got %v", err)
	}
}

func TestCLIWrapperDriver_OpaqueSessionResumeWithoutHandleRejection(t *testing.T) {
	runner := &mockCommandRunner{}
	driver := MustNewCLIWrapperDriver("cli-opaque-resume", runner, CLIWrapperOptions{
		Binary:     "test-cli",
		PromptFlag: "-p",
		ModelFlag:  "--model",
		ResumeFlag: "--resume",
		// AllowLogicalSessionIDAsHandle is false by default
	})

	ctx := context.Background()

	// Resume without prior session or backend handle must fail closed with CategoryInvalidTransition
	_, err := driver.ResumeSession(ctx, "non-existent-session", SessionConfig{
		SessionID: "non-existent-session",
		ModelID:   "test-model",
	})
	if err == nil {
		t.Fatalf("expected error resuming opaque session without handle, got nil")
	}
	if errs.CategoryOf(err) != errs.CategoryInvalidTransition {
		t.Errorf("expected CategoryInvalidTransition, got %v", err)
	}

	// Providing backend handle in Options works
	resumed, err := driver.ResumeSession(ctx, "resumed-sess", SessionConfig{
		SessionID: "resumed-sess",
		ModelID:   "test-model",
		Options: map[string]string{
			"backend_session_handle": "handle-abc-123",
		},
	})
	if err != nil {
		t.Fatalf("ResumeSession with handle failed: %v", err)
	}
	if resumed == nil {
		t.Errorf("expected resumed session, got nil")
	}
}

func TestChannelEventStream_BoundedBackpressure(t *testing.T) {
	stream := NewChannelEventStream(2)

	// Send 2 items without blocking
	if !stream.Send(DriverEvent{Kind: EventContentDelta, Delta: "1"}) {
		t.Fatalf("failed sending 1")
	}
	if !stream.Send(DriverEvent{Kind: EventContentDelta, Delta: "2"}) {
		t.Fatalf("failed sending 2")
	}

	// 3rd item should block until 1 item is read
	sendDone := make(chan bool, 1)
	go func() {
		ok := stream.Send(DriverEvent{Kind: EventContentDelta, Delta: "3"})
		sendDone <- ok
	}()

	select {
	case <-sendDone:
		t.Fatalf("expected Send to block on full buffer")
	case <-time.After(30 * time.Millisecond):
		// Blocked as expected
	}

	// Read 1 item
	ev1, err := stream.Recv()
	if err != nil || ev1.Delta != "1" {
		t.Fatalf("expected delta '1', got %v, err %v", ev1.Delta, err)
	}

	// Now Send should unblock
	select {
	case ok := <-sendDone:
		if !ok {
			t.Fatalf("Send unblocked with false")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("Send failed to unblock after Recv drained buffer")
	}

	// Test SetOnClose
	closedHookRan := false
	stream.SetOnClose(func() {
		closedHookRan = true
	})
	_ = stream.Close()
	if !closedHookRan {
		t.Errorf("expected SetOnClose hook to run on Close()")
	}
}

func TestCLIWrapperDriver_LargeTokenOutput(t *testing.T) {
	largeOutputRunner := &largeOutputCommandRunner{
		lineSize: 200 * 1024, // 200 KB line
	}
	driver := MustNewCLIWrapperDriver("cli-large-output", largeOutputRunner, CLIWrapperOptions{
		Binary:     "test-cli",
		PromptFlag: "-p",
		ModelFlag:  "--model",
	})

	session, err := driver.StartSession(context.Background(), SessionConfig{
		SessionID: "sess-large-output",
		ModelID:   "test-model",
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}
	defer session.Close(context.Background())

	stream, err := session.StreamTurn(context.Background(), TurnInput{
		TurnID: "turn-large",
		Prompt: "generate large",
	})
	if err != nil {
		t.Fatalf("StreamTurn failed: %v", err)
	}
	defer stream.Close()

	recvd := 0
	for {
		ev, err := stream.Recv()
		if err != nil {
			break
		}
		recvd += len(ev.Delta)
	}
	if recvd < 200*1024 {
		t.Errorf("expected at least 200KB streamed, got %d", recvd)
	}
}

type largeOutputCommandRunner struct {
	lineSize int
}

func (r *largeOutputCommandRunner) Run(ctx context.Context, spec process.Spec) (process.Result, error) {
	largeLine := strings.Repeat("A", r.lineSize) + "\n"
	if spec.StdoutSink != nil {
		_, _ = spec.StdoutSink.Write([]byte(largeLine))
	}
	return process.Result{
		Status:   process.StatusCompleted,
		ExitCode: 0,
		Stdout:   []byte(largeLine),
	}, nil
}

func TestProcessRunner_Direct(t *testing.T) {
	runner := process.NewRunner()
	ctx := context.Background()

	tmpDir := os.TempDir()
	spec := process.Spec{
		Executable: "echo",
		Args:       []string{"hello-devcadence"},
		Dir:        tmpDir,
		Env:        process.BaseEnv(),
		Timeout:    5 * time.Second,
	}

	res, err := runner.Run(ctx, spec)
	if err != nil {
		t.Fatalf("process.Runner failed: %v", err)
	}
	if !bytes.Contains(res.Stdout, []byte("hello-devcadence")) {
		t.Errorf("expected stdout containing 'hello-devcadence', got %s", string(res.Stdout))
	}
}

func TestCLIWrapperDriver_StreamTurnCapturesBackendHandle(t *testing.T) {
	runner := &mockCommandRunner{}
	driver := MustNewCLIWrapperDriver("cli-stream-handle-driver", runner, CLIWrapperOptions{
		Binary:     "test-cli",
		PromptFlag: "-p",
		ModelFlag:  "--model",
		ResumeFlag: "--resume",
	})

	ctx := context.Background()
	session, err := driver.StartSession(ctx, SessionConfig{
		SessionID: "sess-stream-backend-test",
		ModelID:   "test-model",
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	stream, err := session.StreamTurn(ctx, TurnInput{
		TurnID: "turn-1",
		Prompt: "hello stream",
	})
	if err != nil {
		t.Fatalf("StreamTurn failed: %v", err)
	}

	// Drain the stream
	for {
		_, recvErr := stream.Recv()
		if recvErr != nil {
			break
		}
	}

	cliSess, ok := session.(*cliSession)
	if !ok {
		t.Fatalf("expected session to be *cliSession")
	}

	// Assert backend session handle was captured from the streaming turn
	backendHandle := cliSess.BackendSessionHandle()
	if backendHandle != "cli-opaque-backend-123" {
		t.Errorf("expected backendSessionHandle to be 'cli-opaque-backend-123', got %q", backendHandle)
	}

	// Assert config options contains the captured handle
	cfg := session.Config()
	if cfg.Options["backend_session_handle"] != "cli-opaque-backend-123" {
		t.Errorf("expected config.Options['backend_session_handle'] to be 'cli-opaque-backend-123', got %q", cfg.Options["backend_session_handle"])
	}

	// Now run a second turn and verify runner received --resume cli-opaque-backend-123
	_, err = session.ExecuteTurn(ctx, TurnInput{
		TurnID: "turn-2",
		Prompt: "second turn",
	})
	if err != nil {
		t.Fatalf("ExecuteTurn failed: %v", err)
	}

	runner.mu.Lock()
	lastArgs := runner.lastArgs
	runner.mu.Unlock()

	foundResume := false
	for i, arg := range lastArgs {
		if arg == "--resume" && i+1 < len(lastArgs) && lastArgs[i+1] == "cli-opaque-backend-123" {
			foundResume = true
			break
		}
	}
	if !foundResume {
		t.Errorf("expected --resume cli-opaque-backend-123 in args, got %v", lastArgs)
	}
}

func TestCLIWrapperDriver_StreamTurnRejectsWhenStreamingUnsupported(t *testing.T) {
	runner := &mockCommandRunner{}
	noStreamCaps := DriverCapabilities{
		Kind:                  protocol.ChannelCLISubprocess,
		SessionMode:           protocol.SessionResumableHandle,
		ContextControl:        protocol.ContextControlOpaqueSession,
		PrefixCache:           protocol.PrefixCacheNone,
		SupportsStreaming:     false,
		SupportsTools:         true,
		NativeWorktreeAccess:  false,
		MaxConcurrentRequests: 1,
	}

	driver := MustNewCLIWrapperDriver("cli-no-stream-driver", runner, CLIWrapperOptions{
		Binary:       "test-cli",
		PromptFlag:   "-p",
		ModelFlag:    "--model",
		Capabilities: &noStreamCaps,
	})

	ctx := context.Background()
	session, err := driver.StartSession(ctx, SessionConfig{
		SessionID: "sess-no-stream-test",
		ModelID:   "test-model",
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	_, err = session.StreamTurn(ctx, TurnInput{
		TurnID: "turn-1",
		Prompt: "hello",
	})
	if err == nil {
		t.Fatalf("expected error when streaming is unsupported, got nil")
	}
	if errs.CategoryOf(err) != errs.CategoryUnsupported {
		t.Errorf("expected CategoryUnsupported, got %v", err)
	}
}
