package reviewexec

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/tools"
)

func TestSetupReviewerTools_MediatorToolClosures(t *testing.T) {
	tempDir := t.TempDir()
	canonicalDir, err := filepath.EvalSymlinks(tempDir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	srcDir := filepath.Join(canonicalDir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("Mkdir src: %v", err)
	}

	mainGo := filepath.Join(srcDir, "main.go")
	mainContent := "package main\n\nfunc ReviewTarget() string {\n\treturn \"reviewed\"\n}\n"
	if err := os.WriteFile(mainGo, []byte(mainContent), 0644); err != nil {
		t.Fatalf("WriteFile main.go: %v", err)
	}

	subDir := filepath.Join(canonicalDir, "emptydir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("Mkdir emptydir: %v", err)
	}

	txtFile := filepath.Join(canonicalDir, "doc.txt")
	if err := os.WriteFile(txtFile, []byte("Documentation notes\nReviewTarget reference\n"), 0644); err != nil {
		t.Fatalf("WriteFile doc.txt: %v", err)
	}

	scope := &tools.Scope{
		ProjectID:    "test-review-proj",
		WorktreePath: canonicalDir,
	}
	runner := process.NewRunner()

	mediator := drivers.NewScopedToolMediator(scope)
	toolDefs := setupReviewerTools(mediator, scope, runner)
	if len(toolDefs) != 3 {
		t.Fatalf("setupReviewerTools returned %d tool defs, want 3", len(toolDefs))
	}

	ctx := context.Background()

	// --- 1. read_file ---
	t.Run("read_file: valid read", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"path": "src/main.go",
		})
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "r1",
			Name:      "read_file",
			Arguments: args,
		})
		if err != nil || res.IsError {
			t.Fatalf("expected valid read, got err=%v, res=%+v", err, res)
		}
		var rfRes tools.ReadFileResult
		if err := json.Unmarshal([]byte(res.Content), &rfRes); err != nil {
			t.Fatalf("unmarshal ReadFileResult: %v", err)
		}
		if !strings.Contains(rfRes.Content, "func ReviewTarget") {
			t.Errorf("content %q does not contain 'func ReviewTarget'", rfRes.Content)
		}
	})

	t.Run("read_file: non-existent file", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"path": "src/missing.go",
		})
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "r2",
			Name:      "read_file",
			Arguments: args,
		})
		if err == nil && !res.IsError {
			t.Fatal("expected error reading missing file, got success")
		}
	})

	t.Run("read_file: directory read", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"path": "emptydir",
		})
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "r3",
			Name:      "read_file",
			Arguments: args,
		})
		if err == nil && !res.IsError {
			t.Fatal("expected error reading directory, got success")
		}
	})

	t.Run("read_file: path traversal", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"path": "../../etc/passwd",
		})
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "r4",
			Name:      "read_file",
			Arguments: args,
		})
		if err == nil && !res.IsError {
			t.Fatal("expected error on path traversal, got success")
		}
	})

	t.Run("read_file: invalid arguments", func(t *testing.T) {
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "r5",
			Name:      "read_file",
			Arguments: []byte("invalid-json"),
		})
		if err == nil && !res.IsError {
			t.Fatal("expected error on invalid arguments, got success")
		}
	})

	// --- 2. grep ---
	t.Run("grep: valid search", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"query": "ReviewTarget",
			"path":  "src",
		})
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "r6",
			Name:      "grep",
			Arguments: args,
		})
		if err != nil || res.IsError {
			t.Fatalf("expected valid grep, got err=%v, res=%+v", err, res)
		}
		var grepRes tools.GrepResult
		if err := json.Unmarshal([]byte(res.Content), &grepRes); err != nil {
			t.Fatalf("unmarshal GrepResult: %v", err)
		}
		if len(grepRes.Matches) == 0 {
			t.Errorf("expected grep matches for ReviewTarget, got 0")
		}
	})

	t.Run("grep: invalid arguments", func(t *testing.T) {
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "r7",
			Name:      "grep",
			Arguments: []byte("invalid-json"),
		})
		if err == nil && !res.IsError {
			t.Fatal("expected error on invalid grep arguments, got success")
		}
	})

	// --- 3. symbols ---
	t.Run("symbols: valid parse", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"query": "ReviewTarget",
			"path":  "src/main.go",
		})
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "r8",
			Name:      "symbols",
			Arguments: args,
		})
		if err != nil || res.IsError {
			t.Fatalf("expected valid symbols, got err=%v, res=%+v", err, res)
		}
		var symRes tools.SymbolResult
		if err := json.Unmarshal([]byte(res.Content), &symRes); err != nil {
			t.Fatalf("unmarshal SymbolResult: %v", err)
		}
		if len(symRes.Definitions) == 0 || symRes.Definitions[0].Name != "ReviewTarget" {
			t.Errorf("expected definition for ReviewTarget, got %+v", symRes.Definitions)
		}
	})

	t.Run("symbols: unsupported file", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"query": "ReviewTarget",
			"path":  "doc.txt",
		})
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "r9",
			Name:      "symbols",
			Arguments: args,
		})
		if err != nil || res.IsError {
			t.Fatalf("expected unsupported file to return without error, got err=%v, res=%+v", err, res)
		}
		var symRes tools.SymbolResult
		if err := json.Unmarshal([]byte(res.Content), &symRes); err != nil {
			t.Fatalf("unmarshal SymbolResult: %v", err)
		}
		if len(symRes.Definitions) != 0 {
			t.Errorf("expected 0 definitions for unsupported file, got %d", len(symRes.Definitions))
		}
	})

	t.Run("symbols: non-existent file", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"query": "ReviewTarget",
			"path":  "missing.go",
		})
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "r10",
			Name:      "symbols",
			Arguments: args,
		})
		if err == nil && !res.IsError {
			t.Fatal("expected error on non-existent file in symbols, got success")
		}
	})

	t.Run("symbols: invalid arguments", func(t *testing.T) {
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "r11",
			Name:      "symbols",
			Arguments: []byte("invalid-json"),
		})
		if err == nil && !res.IsError {
			t.Fatal("expected error on invalid symbols arguments, got success")
		}
	})
}
