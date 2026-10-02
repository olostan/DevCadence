package drivers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/tools"
)

type mockProcessHandle struct {
	stdout  *io.PipeReader
	stderr  *io.PipeReader
	waitErr error
	killed  bool
	mu      sync.Mutex
}

func (h *mockProcessHandle) Stdout() io.ReadCloser { return h.stdout }
func (h *mockProcessHandle) Stderr() io.ReadCloser { return h.stderr }
func (h *mockProcessHandle) Kill() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.killed = true
	h.stdout.Close()
	h.stderr.Close()
	return nil
}
func (h *mockProcessHandle) Wait() (*CommandOutput, error) {
	return &CommandOutput{ExitCode: 0}, h.waitErr
}

type mockCommandRunner struct {
	delay      time.Duration
	lastSpec   CommandSpec
	lastDir    string
	lastArgs   []string
	killedLast bool
	mu         sync.Mutex
}

func (r *mockCommandRunner) Run(ctx context.Context, spec CommandSpec) (*CommandOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.lastSpec = spec
	r.lastDir = spec.Dir
	r.lastArgs = spec.Args
	r.mu.Unlock()

	if r.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(r.delay):
		}
	}

	// Look at args to see prompt
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

	return &CommandOutput{
		Stdout:   []byte(stdout.String()),
		ExitCode: 0,
	}, nil
}

func (r *mockCommandRunner) Start(ctx context.Context, spec CommandSpec) (ProcessHandle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.lastSpec = spec
	r.mu.Unlock()

	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()

	handle := &mockProcessHandle{
		stdout: stdoutR,
		stderr: stderrR,
	}

	go func() {
		defer stdoutW.Close()
		defer stderrW.Close()

		if r.delay > 0 {
			select {
			case <-ctx.Done():
				r.mu.Lock()
				r.killedLast = true
				r.mu.Unlock()
				return
			case <-time.After(r.delay):
			}
		}

		prompt := ""
		for i, arg := range spec.Args {
			if arg == "-p" && i+1 < len(spec.Args) {
				prompt = spec.Args[i+1]
				break
			}
		}

		_, _ = stdoutW.Write([]byte(fmt.Sprintf("Stream line 1: %s\n", prompt)))
		_, _ = stdoutW.Write([]byte(fmt.Sprintf("Stream line 2\n")))
		_, _ = stdoutW.Write([]byte(`{"kind":"turn_completed","usage":{"input_tokens":10,"output_tokens":20}}` + "\n"))
	}()

	return handle, nil
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
	driver := NewCLIWrapperDriver("cli-worktree-driver", runner, CLIWrapperOptions{
		Binary:     "test-cli",
		PromptFlag: "-p",
	})

	scope := &tools.Scope{
		ProjectID:    "proj-1",
		WorktreePath: "/tmp/fake-worktree",
	}

	session, err := driver.StartSession(context.Background(), SessionConfig{
		SessionID:     "sess-worktree-dir",
		ModelID:       "test-model",
		WorktreeScope: scope,
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	_, err = session.ExecuteTurn(context.Background(), TurnInput{
		TurnID: "turn-dir-check",
		Prompt: "check dir",
	})
	if err != nil {
		t.Fatalf("ExecuteTurn failed: %v", err)
	}

	if runner.lastDir != "/tmp/fake-worktree" {
		t.Errorf("expected working dir '/tmp/fake-worktree', got %q", runner.lastDir)
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

func TestOSCommandRunner_Basic(t *testing.T) {
	runner := &OSCommandRunner{}
	ctx := context.Background()

	out, err := runner.Run(ctx, CommandSpec{
		Binary: "echo",
		Args:   []string{"hello-devcadence"},
	})
	if err != nil {
		t.Fatalf("OSCommandRunner failed: %v", err)
	}
	if !bytes.Contains(out.Stdout, []byte("hello-devcadence")) {
		t.Errorf("expected stdout containing 'hello-devcadence', got %s", string(out.Stdout))
	}
}
