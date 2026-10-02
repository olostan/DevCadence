package drivers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
