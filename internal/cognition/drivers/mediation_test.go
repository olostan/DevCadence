package drivers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/tools"
)

func TestWorktreeMediation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "worktree-mediation-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	subDir := filepath.Join(tempDir, "src")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	testFile := filepath.Join(subDir, "main.go")
	if err := os.WriteFile(testFile, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	scope := &tools.Scope{
		ProjectID:    "test-project",
		WorktreePath: tempDir,
	}

	mediator := NewScopedToolMediator(scope)

	// Valid path inside worktree
	resolved, err := mediator.ValidatePath("src/main.go")
	if err != nil {
		t.Fatalf("expected path to validate successfully: %v", err)
	}
	if resolved != testFile {
		t.Errorf("expected resolved path %q, got %q", testFile, resolved)
	}

	// Path escape outside worktree
	_, err = mediator.ValidatePath("../../etc/passwd")
	if err == nil {
		t.Fatalf("expected error for path escaping worktree root, got nil")
	}
	if errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Errorf("expected CategoryPolicyDenied, got %v", err)
	}
}

func TestToolMediation_Dispatch(t *testing.T) {
	mediator := NewScopedToolMediator(nil)

	mediator.RegisterHandler("echo_tool", func(ctx context.Context, args json.RawMessage) (string, error) {
		var input struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			return "", err
		}
		return "echo: " + input.Text, nil
	})

	ctx := context.Background()

	// Successful dispatch
	call := ToolCall{
		ID:        "call-1",
		Name:      "echo_tool",
		Arguments: []byte(`{"text":"hello"}`),
	}
	res, err := mediator.ExecuteTool(ctx, call)
	if err != nil {
		t.Fatalf("ExecuteTool failed: %v", err)
	}
	if res.IsError {
		t.Errorf("expected IsError=false, got true: %s", res.Content)
	}
	if res.Content != "echo: hello" {
		t.Errorf("expected 'echo: hello', got %q", res.Content)
	}

	// Unknown tool
	unknownCall := ToolCall{
		ID:        "call-2",
		Name:      "nonexistent_tool",
		Arguments: []byte(`{}`),
	}
	res2, err := mediator.ExecuteTool(ctx, unknownCall)
	if err != nil {
		t.Fatalf("ExecuteTool failed: %v", err)
	}
	if !res2.IsError {
		t.Errorf("expected IsError=true for unknown tool")
	}
}

func TestToolMediation_ContextCancelled(t *testing.T) {
	mediator := NewScopedToolMediator(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	call := ToolCall{
		ID:        "call-cancel",
		Name:      "any_tool",
		Arguments: []byte(`{}`),
	}
	_, err := mediator.ExecuteTool(ctx, call)
	if err == nil {
		t.Errorf("expected error with cancelled context, got nil")
	}
}

func TestSessionScopedMediator_ConcurrentForSessionAndExecuteTool(t *testing.T) {
	mediator := NewScopedToolMediator(nil)
	mediator.RegisterToolDefinition(ToolDefinition{
		Name:        "ping",
		Description: "ping tool",
	})
	mediator.RegisterHandler("ping", func(ctx context.Context, args json.RawMessage) (string, error) {
		return "pong", nil
	})

	const sessionID = "sess-concurrent-test"
	declaredTools := []ToolDefinition{{Name: "ping"}}
	sessionMediator := mediator.ForSession(sessionID, declaredTools)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	const workers = 20
	const iterations = 100

	// 20 workers concurrently calling ForSession on the same session
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				select {
				case <-ctx.Done():
					return
				default:
				}
				s := mediator.ForSession(sessionID, declaredTools)
				if s == nil {
					t.Errorf("ForSession returned nil")
					return
				}
			}
		}()
	}

	// 20 workers concurrently calling ExecuteTool on the session mediator
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				select {
				case <-ctx.Done():
					return
				default:
				}
				res, err := sessionMediator.ExecuteTool(ctx, ToolCall{
					ID:        fmt.Sprintf("call-%d-%d", workerID, j),
					Name:      "ping",
					Arguments: []byte(`{}`),
				})
				if err != nil {
					t.Errorf("ExecuteTool returned error: %v", err)
					return
				}
				if res.Content != "pong" {
					t.Errorf("expected 'pong', got %q", res.Content)
					return
				}
			}
		}(i)
	}

	// 5 workers concurrently calling SetDeclaredTools
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				select {
				case <-ctx.Done():
					return
				default:
				}
				sessionMediator.SetDeclaredTools(declaredTools)
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-ctx.Done():
		t.Fatalf("test deadlocked or timed out under concurrent ForSession and ExecuteTool: %v", ctx.Err())
	}
}
