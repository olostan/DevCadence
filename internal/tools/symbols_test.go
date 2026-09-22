package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFindSymbolGo(t *testing.T) {
	tempDir := t.TempDir()

	goSource := `package main

type Engine struct {
	power int
}

func (e *Engine) Start() bool {
	return true
}

func Start() {
	println("global start")
}

func RunEngine(e *Engine) {
	e.Start()
	Start()
}
`
	if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte(goSource), 0o644); err != nil {
		t.Fatal(err)
	}

	scope := Scope{
		WorktreePath: tempDir,
	}

	res, err := FindSymbol(context.Background(), FindSymbolOptions{
		Scope:  scope,
		Symbol: "Start",
	})
	if err != nil {
		t.Fatalf("FindSymbol Go: %v", err)
	}

	if res.ResolutionLevel != ResolutionSyntactic {
		t.Errorf("Expected resolution_level syntactic, got %v", res.ResolutionLevel)
	}
	if res.Backend != "tree-sitter" {
		t.Errorf("Expected backend tree-sitter, got %v", res.Backend)
	}
	if res.Language != "go" {
		t.Errorf("Expected language go, got %v", res.Language)
	}

	// Should have 2 definitions: method (Engine.Start) and function (Start)
	if len(res.Definitions) != 2 {
		t.Fatalf("Expected 2 definitions for Start, got %d: %+v", len(res.Definitions), res.Definitions)
	}
	// Should have 2 candidate calls: e.Start() and Start()
	if len(res.CandidateCalls) != 2 {
		t.Fatalf("Expected 2 candidate calls for Start, got %d: %+v", len(res.CandidateCalls), res.CandidateCalls)
	}
}

func TestFindSymbolTypeScript(t *testing.T) {
	tempDir := t.TempDir()

	tsSource := `export interface UserProfile {
	id: string;
}

export class UserManager {
	async authenticate(token: string): Promise<boolean> {
		return true;
	}
}

export async function authenticate(token: string) {
	const mgr = new UserManager();
	return mgr.authenticate(token);
}
`
	if err := os.WriteFile(filepath.Join(tempDir, "service.ts"), []byte(tsSource), 0o644); err != nil {
		t.Fatal(err)
	}

	scope := Scope{
		WorktreePath: tempDir,
	}

	res, err := FindSymbol(context.Background(), FindSymbolOptions{
		Scope:  scope,
		Symbol: "authenticate",
	})
	if err != nil {
		t.Fatalf("FindSymbol TS: %v", err)
	}

	if res.ResolutionLevel != ResolutionSyntactic {
		t.Errorf("Expected resolution_level syntactic, got %v", res.ResolutionLevel)
	}
	if res.Language != "typescript" {
		t.Errorf("Expected language typescript, got %v", res.Language)
	}

	// Should have method and function definitions
	if len(res.Definitions) != 2 {
		t.Fatalf("Expected 2 definitions, got %d: %+v", len(res.Definitions), res.Definitions)
	}
	// Should have candidate call
	if len(res.CandidateCalls) == 0 {
		t.Errorf("Expected candidate calls for authenticate, got 0")
	}
}

func TestFindSymbolUnsupportedLanguage(t *testing.T) {
	tempDir := t.TempDir()
	pyFile := filepath.Join(tempDir, "script.py")
	if err := os.WriteFile(pyFile, []byte("def calculate(): pass"), 0o644); err != nil {
		t.Fatal(err)
	}

	scope := Scope{
		WorktreePath: tempDir,
	}

	res, err := FindSymbol(context.Background(), FindSymbolOptions{
		Scope:    scope,
		Symbol:   "calculate",
		Language: "python",
	})
	if err != nil {
		t.Fatalf("FindSymbol unsupported: %v", err)
	}

	if res.ResolutionLevel != ResolutionUnsupported {
		t.Errorf("Expected resolution_level unsupported, got %v", res.ResolutionLevel)
	}
	if res.FallbackQuery != "calculate" {
		t.Errorf("Expected fallback query 'calculate', got: %s", res.FallbackQuery)
	}
}
