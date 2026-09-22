package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/olostan/DevCadence/internal/artifacts"
)

func setupTestFiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	subDir := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// file1 has 15 matching lines
	var f1Content string
	for i := 1; i <= 15; i++ {
		f1Content += fmt.Sprintf("target_token occurrence %d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "file1.txt"), []byte(f1Content), 0o644); err != nil {
		t.Fatal(err)
	}

	// file2 has 10 matching lines
	var f2Content string
	for i := 1; i <= 10; i++ {
		f2Content += fmt.Sprintf("pkg target_token item %d\n", i)
	}
	if err := os.WriteFile(filepath.Join(subDir, "file2.txt"), []byte(f2Content), 0o644); err != nil {
		t.Fatal(err)
	}

	// file3 has non-matching lines
	if err := os.WriteFile(filepath.Join(dir, "file3.txt"), []byte("other lines\nno match here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestGrepSearchCutoffAndArtifact(t *testing.T) {
	ctx := context.Background()
	worktree := setupTestFiles(t)

	store, err := artifacts.NewStore(filepath.Join(t.TempDir(), "artifacts"), nil)
	if err != nil {
		t.Fatal(err)
	}

	scope := Scope{
		WorktreePath: worktree,
	}

	// 25 total matches, limit to 5
	res, err := GrepSearch(ctx, GrepOptions{
		Artifacts:  store,
		Scope:      scope,
		Query:      "target_token",
		MaxResults: 5,
		Mode:       GrepModeMatches,
	})
	if err != nil {
		t.Fatalf("GrepSearch: %v", err)
	}

	if res.ReturnedCount != 5 {
		t.Errorf("Expected ReturnedCount 5, got %d", res.ReturnedCount)
	}
	if len(res.Matches) != 5 {
		t.Errorf("Expected 5 matches in result, got %d", len(res.Matches))
	}
	if res.TotalCount != 25 {
		t.Errorf("Expected TotalCount 25, got %d", res.TotalCount)
	}
	if !res.HasMore {
		t.Errorf("Expected HasMore = true")
	}
	if res.ContentRef == "" {
		t.Errorf("Expected ContentRef to be populated when results exceed MaxResults")
	}

	// Verify pagination over the generated artifact
	paged, err := FetchContent(FetchContentOptions{
		Artifacts:  store,
		ContentRef: res.ContentRef,
		Offset:     1,
		Limit:      10,
		Unit:       "lines",
	})
	if err != nil {
		t.Fatalf("FetchContent on grep artifact: %v", err)
	}
	if paged.ReturnedCount != 10 || paged.TotalCount != 25 || !paged.HasMore {
		t.Errorf("Unexpected paged artifact result: %+v", paged)
	}
}

func TestGrepSearchFilesOnly(t *testing.T) {
	ctx := context.Background()
	worktree := setupTestFiles(t)

	scope := Scope{
		WorktreePath: worktree,
	}

	res, err := GrepSearch(ctx, GrepOptions{
		Scope: scope,
		Query: "target_token",
		Mode:  GrepModeFilesOnly,
	})
	if err != nil {
		t.Fatalf("GrepSearch files_only: %v", err)
	}

	if res.ReturnedCount != 2 {
		t.Errorf("Expected 2 files with matches, got %d", res.ReturnedCount)
	}
	if len(res.Files) != 2 {
		t.Errorf("Expected 2 files in list, got %v", res.Files)
	}
	if res.HasMore {
		t.Errorf("Expected HasMore = false")
	}
}

func TestGrepSearchCount(t *testing.T) {
	ctx := context.Background()
	worktree := setupTestFiles(t)

	scope := Scope{
		WorktreePath: worktree,
	}

	res, err := GrepSearch(ctx, GrepOptions{
		Scope: scope,
		Query: "target_token",
		Mode:  GrepModeCount,
	})
	if err != nil {
		t.Fatalf("GrepSearch count: %v", err)
	}

	if res.Count != 25 {
		t.Errorf("Expected count 25, got %d", res.Count)
	}
}

func TestGrepSearchRegexVsLiteral(t *testing.T) {
	ctx := context.Background()
	worktree := setupTestFiles(t)

	scope := Scope{
		WorktreePath: worktree,
	}

	// Regex matching "item [0-9]+"
	resRegex, err := GrepSearch(ctx, GrepOptions{
		Scope:   scope,
		Query:   `item [0-9]+`,
		IsRegex: true,
	})
	if err != nil {
		t.Fatalf("GrepSearch regex: %v", err)
	}
	if resRegex.TotalCount != 10 {
		t.Errorf("Expected 10 regex matches, got %d", resRegex.TotalCount)
	}

	// Literal search for "item [0-9]+" should find 0
	resLiteral, err := GrepSearch(ctx, GrepOptions{
		Scope:   scope,
		Query:   `item [0-9]+`,
		IsRegex: false,
	})
	if err != nil {
		t.Fatalf("GrepSearch literal: %v", err)
	}
	if resLiteral.TotalCount != 0 {
		t.Errorf("Expected 0 literal matches, got %d", resLiteral.TotalCount)
	}
}

func TestGrepSearchScopeContainment(t *testing.T) {
	ctx := context.Background()
	worktree := setupTestFiles(t)

	scope := Scope{
		WorktreePath: worktree,
	}

	// Escape attempt via path
	_, err := GrepSearch(ctx, GrepOptions{
		Scope: scope,
		Query: "anything",
		Path:  "../outside",
	})
	if err == nil {
		t.Fatal("Expected error for search path escaping worktree")
	}
}
