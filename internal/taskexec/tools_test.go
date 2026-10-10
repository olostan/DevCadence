package taskexec

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

func TestSetupWorkerTools_MediatorToolClosures(t *testing.T) {
	tempDir := t.TempDir()
	canonicalDir, err := filepath.EvalSymlinks(tempDir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	// Prepare directories and files
	srcDir := filepath.Join(canonicalDir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("Mkdir src: %v", err)
	}

	mainGo := filepath.Join(srcDir, "main.go")
	mainContent := "package main\n\nfunc Hello() string {\n\treturn \"world\"\n}\n"
	if err := os.WriteFile(mainGo, []byte(mainContent), 0644); err != nil {
		t.Fatalf("WriteFile main.go: %v", err)
	}

	readmeTxt := filepath.Join(srcDir, "readme.txt")
	readmeContent := "Line 1: overview\nLine 2: Hello details\n"
	if err := os.WriteFile(readmeTxt, []byte(readmeContent), 0644); err != nil {
		t.Fatalf("WriteFile readme.txt: %v", err)
	}

	subDir := filepath.Join(canonicalDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("Mkdir subdir: %v", err)
	}

	pyScript := filepath.Join(canonicalDir, "script.py")
	if err := os.WriteFile(pyScript, []byte("print('hello')\n"), 0644); err != nil {
		t.Fatalf("WriteFile script.py: %v", err)
	}

	scope := &tools.Scope{
		ProjectID:    "test-project",
		WorktreePath: canonicalDir,
	}
	runner := process.NewRunner()
	writeScope := []string{"src/**", "pkg/**"}

	mediator := drivers.NewScopedToolMediator(scope)
	toolDefs := setupWorkerTools(mediator, scope, runner, writeScope)
	if len(toolDefs) != 5 {
		t.Fatalf("setupWorkerTools returned %d tool defs, want 5", len(toolDefs))
	}

	ctx := context.Background()

	// --- 1. read_file ---
	t.Run("read_file: valid read", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"path": "src/main.go",
		})
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "c1",
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
		if !strings.Contains(rfRes.Content, "func Hello") {
			t.Errorf("content %q does not contain 'func Hello'", rfRes.Content)
		}
	})

	t.Run("read_file: non-existent file", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"path": "src/nonexistent.go",
		})
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "c2",
			Name:      "read_file",
			Arguments: args,
		})
		if err == nil && !res.IsError {
			t.Fatal("expected error for non-existent file, got success")
		}
	})

	t.Run("read_file: directory read", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"path": "subdir",
		})
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "c3",
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
			ID:        "c4",
			Name:      "read_file",
			Arguments: args,
		})
		if err == nil && !res.IsError {
			t.Fatal("expected error on path traversal, got success")
		}
	})

	t.Run("read_file: invalid arguments", func(t *testing.T) {
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "c5",
			Name:      "read_file",
			Arguments: []byte("invalid-json"),
		})
		if err == nil && !res.IsError {
			t.Fatal("expected error on invalid arguments, got success")
		}
	})

	// --- 2. grep ---
	t.Run("grep: valid search and pattern matching", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"query": "Hello",
			"path":  "src",
		})
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "c6",
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
			t.Errorf("expected grep matches, got 0")
		}
	})

	t.Run("grep: invalid arguments", func(t *testing.T) {
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "c7",
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
			"query": "Hello",
			"path":  "src/main.go",
		})
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "c8",
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
		if symRes.ResolutionLevel != tools.ResolutionSyntactic {
			t.Errorf("ResolutionLevel = %v, want syntactic", symRes.ResolutionLevel)
		}
		if len(symRes.Definitions) == 0 || symRes.Definitions[0].Name != "Hello" {
			t.Errorf("expected definition for Hello, got %+v", symRes.Definitions)
		}
	})

	t.Run("symbols: unsupported file", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"query": "Hello",
			"path":  "script.py",
		})
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "c9",
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
			"query": "Hello",
			"path":  "nonexistent.go",
		})
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "c9b",
			Name:      "symbols",
			Arguments: args,
		})
		if err == nil && !res.IsError {
			t.Fatal("expected error for non-existent file in symbols, got success")
		}
	})

	t.Run("symbols: invalid arguments", func(t *testing.T) {
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "c10",
			Name:      "symbols",
			Arguments: []byte("invalid-json"),
		})
		if err == nil && !res.IsError {
			t.Fatal("expected error on invalid symbols arguments, got success")
		}
	})

	// --- 4. write_file ---
	t.Run("write_file: valid write", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"path":    "src/new_helper.go",
			"content": "package main\n\nfunc Helper() {}\n",
		})
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "c11",
			Name:      "write_file",
			Arguments: args,
		})
		if err != nil || res.IsError {
			t.Fatalf("expected valid write, got err=%v, res=%+v", err, res)
		}
		written, err := os.ReadFile(filepath.Join(canonicalDir, "src/new_helper.go"))
		if err != nil {
			t.Fatalf("ReadFile written file: %v", err)
		}
		if !strings.Contains(string(written), "func Helper") {
			t.Errorf("written content does not contain 'func Helper'")
		}
	})

	t.Run("write_file: protected path outside write scope", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"path":    "unauthorized/file.go",
			"content": "package unauthorized\n",
		})
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "c12",
			Name:      "write_file",
			Arguments: args,
		})
		if err == nil && !res.IsError {
			t.Fatal("expected error writing outside declared write scope, got success")
		}
	})

	t.Run("write_file: protected path .git forbidden", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"path":    ".git/config",
			"content": "forbidden",
		})
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "c13",
			Name:      "write_file",
			Arguments: args,
		})
		if err == nil && !res.IsError {
			t.Fatal("expected error modifying .git directory, got success")
		}
	})

	t.Run("write_file: path traversal relative ..", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"path":    "../outside.go",
			"content": "package outside\n",
		})
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "c14",
			Name:      "write_file",
			Arguments: args,
		})
		if err == nil && !res.IsError {
			t.Fatal("expected error on path traversal, got success")
		}
	})

	t.Run("write_file: absolute path forbidden", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"path":    "/tmp/absolute.go",
			"content": "package tmp\n",
		})
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "c15",
			Name:      "write_file",
			Arguments: args,
		})
		if err == nil && !res.IsError {
			t.Fatal("expected error on absolute path, got success")
		}
	})

	t.Run("write_file: refuse symlink", func(t *testing.T) {
		symlinkPath := filepath.Join(canonicalDir, "src/symlink.go")
		if err := os.Symlink(filepath.Join(canonicalDir, "src/nonexistent_target.go"), symlinkPath); err != nil {
			t.Fatalf("Symlink: %v", err)
		}
		args, _ := json.Marshal(map[string]any{
			"path":    "src/symlink.go",
			"content": "package main\n// overwritten\n",
		})
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "c16",
			Name:      "write_file",
			Arguments: args,
		})
		if err == nil && !res.IsError {
			t.Fatal("expected refusal to write through symlink, got success")
		}
	})

	t.Run("write_file: size exceeds 256 KiB cap", func(t *testing.T) {
		args, _ := json.Marshal(map[string]any{
			"path":    "src/oversized.go",
			"content": strings.Repeat("a", 256*1024+10),
		})
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "c17",
			Name:      "write_file",
			Arguments: args,
		})
		if err == nil && !res.IsError {
			t.Fatal("expected error on oversized file, got success")
		}
	})

	t.Run("write_file: non-UTF8 args", func(t *testing.T) {
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "c18",
			Name:      "write_file",
			Arguments: []byte("{\"path\": \"src/bad.go\", \"content\": \"\xff\xfe\"}"),
		})
		if err == nil && !res.IsError {
			t.Fatal("expected error on non-UTF8 args, got success")
		}
	})

	t.Run("write_file: invalid json args", func(t *testing.T) {
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "c19",
			Name:      "write_file",
			Arguments: []byte("invalid-json"),
		})
		if err == nil && !res.IsError {
			t.Fatal("expected error on invalid write_file arguments, got success")
		}
	})
}
