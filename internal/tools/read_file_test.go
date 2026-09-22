package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileLineNumbers(t *testing.T) {
	tempDir := t.TempDir()
	content := "line 1\nline 2\nline 3\n"
	filePath := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	scope := Scope{
		WorktreePath: tempDir,
	}

	// 1. Default: show_line_numbers is false
	resNoNum, err := ReadFile(ReadFileOptions{
		Scope: scope,
		Path:  "test.txt",
	})
	if err != nil {
		t.Fatalf("ReadFile default: %v", err)
	}
	if strings.Contains(resNoNum.Content, "1:") {
		t.Errorf("Expected content without line numbers, got: %q", resNoNum.Content)
	}
	if resNoNum.Content != content {
		t.Errorf("Content mismatch: expected %q, got %q", content, resNoNum.Content)
	}

	// 2. Explicit: show_line_numbers is true
	resNum, err := ReadFile(ReadFileOptions{
		Scope:           scope,
		Path:            "test.txt",
		ShowLineNumbers: true,
	})
	if err != nil {
		t.Fatalf("ReadFile with line numbers: %v", err)
	}
	expectedWithNums := "1: line 1\n2: line 2\n3: line 3\n"
	if resNum.Content != expectedWithNums {
		t.Errorf("Expected %q, got %q", expectedWithNums, resNum.Content)
	}
}

func TestReadFileLineRange(t *testing.T) {
	tempDir := t.TempDir()
	content := "alpha\nbeta\ngamma\ndelta\nepsilon\n"
	filePath := filepath.Join(tempDir, "sample.txt")
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	scope := Scope{
		WorktreePath: tempDir,
	}

	res, err := ReadFile(ReadFileOptions{
		Scope:     scope,
		Path:      "sample.txt",
		StartLine: 2,
		EndLine:   4,
	})
	if err != nil {
		t.Fatalf("ReadFile slice: %v", err)
	}

	expected := "beta\ngamma\ndelta\n"
	if res.Content != expected {
		t.Errorf("Expected slice %q, got %q", expected, res.Content)
	}
	if res.StartLine != 2 || res.EndLine != 4 {
		t.Errorf("Expected line bounds [2, 4], got [%d, %d]", res.StartLine, res.EndLine)
	}
	if res.TotalLines != 5 {
		t.Errorf("Expected total lines 5, got %d", res.TotalLines)
	}
}

func TestReadFileWorktreeContainment(t *testing.T) {
	tempDir := t.TempDir()
	worktreeDir := filepath.Join(tempDir, "worktree")
	outsideDir := filepath.Join(tempDir, "outside")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}

	secretFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("classified"), 0o644); err != nil {
		t.Fatal(err)
	}

	scope := Scope{
		WorktreePath: worktreeDir,
	}

	// Traversal attempt
	_, err := ReadFile(ReadFileOptions{
		Scope: scope,
		Path:  "../outside/secret.txt",
	})
	if err == nil {
		t.Fatal("Expected error for traversal escape outside worktree")
	}

	// Symlink escape attempt
	symlinkPath := filepath.Join(worktreeDir, "leak_link")
	if err := os.Symlink(secretFile, symlinkPath); err != nil {
		t.Fatal(err)
	}

	_, err = ReadFile(ReadFileOptions{
		Scope: scope,
		Path:  "leak_link",
	})
	if err == nil {
		t.Fatal("Expected error for symlink escape outside worktree")
	}
}

func TestReadFileMaxBytesTruncation(t *testing.T) {
	tempDir := t.TempDir()
	content := strings.Repeat("a", 100) + "\n" + strings.Repeat("b", 100) + "\n"
	filePath := filepath.Join(tempDir, "large.txt")
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	scope := Scope{
		WorktreePath: tempDir,
	}

	res, err := ReadFile(ReadFileOptions{
		Scope:    scope,
		Path:     "large.txt",
		MaxBytes: 150,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated {
		t.Errorf("Expected truncated = true, got false")
	}
	if len(res.Content) > 150 {
		t.Errorf("Content length %d exceeded max_bytes 150", len(res.Content))
	}
}
