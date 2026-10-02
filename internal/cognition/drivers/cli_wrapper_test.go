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

	"github.com/olostan/DevCadence/internal/process"
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
		return process.Result{Status: process.StatusCancelled}, err
	}

	r.mu.Lock()
	r.lastSpec = spec
	r.lastDir = spec.Dir
	r.lastArgs = spec.Args
	r.mu.Unlock()

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
	stdout.WriteString(`{"kind":"turn_completed","usage":{"input_tokens":15,"output_tokens":30}}` + "\n")

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
		driver := NewCLIWrapperDriver("cli-contract-driver", runner, CLIWrapperOptions{
			Binary:     "test-cli",
			PromptFlag: "-p",
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

	driver := NewCLIWrapperDriver("cli-worktree-driver", runner, CLIWrapperOptions{
		Binary:     "test-cli",
		PromptFlag: "-p",
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
	driver := NewCLIWrapperDriver("cli-cancel-driver", runner, CLIWrapperOptions{
		Binary:     "test-cli",
		PromptFlag: "-p",
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

func TestCLIWrapperDriver_ResumeArguments(t *testing.T) {
	runner := &mockCommandRunner{}
	driver := NewCLIWrapperDriver("cli-resume-driver", runner, CLIWrapperOptions{
		Binary:     "test-cli",
		PromptFlag: "-p",
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

	// First turn: not resumed yet
	_, _ = session.ExecuteTurn(context.Background(), TurnInput{
		TurnID: "turn-1",
		Prompt: "hello first",
	})

	// Second turn: should have resume flag
	_, _ = session.ExecuteTurn(context.Background(), TurnInput{
		TurnID: "turn-2",
		Prompt: "hello second",
	})

	foundResume := false
	for i, arg := range runner.lastArgs {
		if arg == "--resume-session" && i+1 < len(runner.lastArgs) && runner.lastArgs[i+1] == "sess-resume-123" {
			foundResume = true
			break
		}
	}
	if !foundResume {
		t.Errorf("expected resume flag in args: %v", runner.lastArgs)
	}
}

func TestCLIWrapperDriver_LargeTokenOutput(t *testing.T) {
	// Tests scanner handling of large output lines exceeding 64KB
	largeOutputRunner := &largeOutputCommandRunner{
		lineSize: 200 * 1024, // 200 KB line
	}
	driver := NewCLIWrapperDriver("cli-large-output", largeOutputRunner, CLIWrapperOptions{
		Binary:     "test-cli",
		PromptFlag: "-p",
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
