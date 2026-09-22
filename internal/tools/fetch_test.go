package tools

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/artifacts"
)

func TestFetchContentPagination(t *testing.T) {
	tempDir := t.TempDir()
	store, err := artifacts.NewStore(filepath.Join(tempDir, "artifacts"), nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	lines := []string{
		"line 1", "line 2", "line 3", "line 4", "line 5",
		"line 6", "line 7", "line 8", "line 9", "line 10",
	}
	content := strings.Join(lines, "\n")
	putRes, err := store.PutBytes(t.Context(), "test_proj", "evidence", "text/plain", []byte(content), 0)
	if err != nil {
		t.Fatalf("store.PutBytes: %v", err)
	}
	ref := putRes.Ref.Locator

	// Page 1: lines 1-4
	p1, err := FetchContent(FetchContentOptions{
		Artifacts:  store,
		ProjectID:  "test_proj",
		ContentRef: ref,
		Offset:     1,
		Limit:      4,
		Unit:       "lines",
	})
	if err != nil {
		t.Fatalf("FetchContent p1: %v", err)
	}
	if p1.ReturnedCount != 4 || p1.TotalCount != 10 || !p1.HasMore || p1.NextOffset != 5 {
		t.Errorf("Unexpected p1 metadata: %+v", p1)
	}
	if p1.Content != strings.Join(lines[0:4], "\n") {
		t.Errorf("p1 content mismatch: %q", p1.Content)
	}

	// Page 2: lines 5-8
	p2, err := FetchContent(FetchContentOptions{
		Artifacts:  store,
		ProjectID:  "test_proj",
		ContentRef: ref,
		Offset:     p1.NextOffset,
		Limit:      4,
		Unit:       "lines",
	})
	if err != nil {
		t.Fatalf("FetchContent p2: %v", err)
	}
	if p2.ReturnedCount != 4 || p2.TotalCount != 10 || !p2.HasMore || p2.NextOffset != 9 {
		t.Errorf("Unexpected p2 metadata: %+v", p2)
	}
	if p2.Content != strings.Join(lines[4:8], "\n") {
		t.Errorf("p2 content mismatch: %q", p2.Content)
	}

	// Page 3: lines 9-10 (final page)
	p3, err := FetchContent(FetchContentOptions{
		Artifacts:  store,
		ProjectID:  "test_proj",
		ContentRef: ref,
		Offset:     p2.NextOffset,
		Limit:      4,
		Unit:       "lines",
	})
	if err != nil {
		t.Fatalf("FetchContent p3: %v", err)
	}
	if p3.ReturnedCount != 2 || p3.TotalCount != 10 || p3.HasMore || p3.NextOffset != 0 {
		t.Errorf("Unexpected p3 metadata: %+v", p3)
	}
	if p3.Content != strings.Join(lines[8:10], "\n") {
		t.Errorf("p3 content mismatch: %q", p3.Content)
	}
}

func TestFetchContentBytes(t *testing.T) {
	tempDir := t.TempDir()
	store, err := artifacts.NewStore(filepath.Join(tempDir, "artifacts"), nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	raw := []byte("0123456789abcdef")
	putRes, err := store.PutBytes(t.Context(), "test_proj", "evidence", "text/plain", raw, 0)
	if err != nil {
		t.Fatalf("store.PutBytes: %v", err)
	}
	ref := putRes.Ref.Locator

	res, err := FetchContent(FetchContentOptions{
		Artifacts:  store,
		ProjectID:  "test_proj",
		ContentRef: ref,
		Offset:     4,
		Limit:      6,
		Unit:       "bytes",
	})
	if err != nil {
		t.Fatalf("FetchContent bytes: %v", err)
	}
	if res.ReturnedCount != 6 || res.TotalCount != 16 || !res.HasMore || res.NextOffset != 10 {
		t.Errorf("Unexpected byte paging result: %+v", res)
	}
	if res.Content != "456789" {
		t.Errorf("Content mismatch: expected '456789', got %q", res.Content)
	}
}

func TestFetchContentMissingRef(t *testing.T) {
	tempDir := t.TempDir()
	store, err := artifacts.NewStore(filepath.Join(tempDir, "artifacts"), nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	_, err = FetchContent(FetchContentOptions{
		Artifacts:  store,
		ProjectID:  "test_proj",
		ContentRef: "artifact:test_proj:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	})
	if err == nil {
		t.Fatal("Expected error for nonexistent content_ref")
	}
}

func TestFetchContentAuthorization(t *testing.T) {
	tempDir := t.TempDir()
	store, err := artifacts.NewStore(filepath.Join(tempDir, "artifacts"), nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	putRes, err := store.PutBytes(t.Context(), "secret_proj", "evidence", "text/plain", []byte("confidential"), 0)
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	// Caller from attacker_proj attempts to read secret_proj artifact
	_, err = FetchContent(FetchContentOptions{
		Artifacts:  store,
		ProjectID:  "attacker_proj",
		ContentRef: putRes.Ref.Locator,
	})
	if err == nil {
		t.Fatal("Expected authorization error when ProjectID does not match locator project")
	}
}

func TestFetchContentLongLines(t *testing.T) {
	tempDir := t.TempDir()
	store, err := artifacts.NewStore(filepath.Join(tempDir, "artifacts"), nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Line of 80,000 bytes (exceeds default bufio.Scanner 64KB buffer)
	longLine := strings.Repeat("A", 80000)
	putRes, err := store.PutBytes(t.Context(), "test_proj", "evidence", "text/plain", []byte(longLine+"\nsecond line\n"), 0)
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	res, err := FetchContent(FetchContentOptions{
		Artifacts:  store,
		ProjectID:  "test_proj",
		ContentRef: putRes.Ref.Locator,
		Offset:     1,
		Limit:      2,
		MaxBytes:   100000,
	})
	if err != nil {
		t.Fatalf("FetchContent with long line: %v", err)
	}
	if res.TotalCount != 2 {
		t.Errorf("Expected TotalCount 2, got %d", res.TotalCount)
	}
	if res.ReturnedCount != 2 {
		t.Errorf("Expected ReturnedCount 2, got %d", res.ReturnedCount)
	}
	if !strings.HasPrefix(res.Content, "AAAA") {
		t.Errorf("Expected content to start with AAAA, got %q", res.Content[:min(len(res.Content), 50)])
	}
}

func TestClosureLinePageRespectsByteCap(t *testing.T) {
	tempDir := t.TempDir()
	store, err := artifacts.NewStore(filepath.Join(tempDir, "artifacts"), nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	longLine := strings.Repeat("A", 70000)
	putRes, err := store.PutBytes(t.Context(), "test_proj", "evidence", "text/plain", []byte(longLine+"\n"), 0)
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	res, err := FetchContent(FetchContentOptions{
		Artifacts:  store,
		ProjectID:  "test_proj",
		ContentRef: putRes.Ref.Locator,
		Offset:     1,
		Limit:      10,
		MaxBytes:   65536,
	})
	if err != nil {
		t.Fatalf("FetchContent: %v", err)
	}
	if int64(len(res.Content)) > 65536 {
		t.Errorf("Content length %d exceeds MaxBytes 65536", len(res.Content))
	}
	if !res.Truncated {
		t.Errorf("Expected Truncated to be true for oversized first line")
	}
}

func TestClosureLinePageHasNoHoles(t *testing.T) {
	tempDir := t.TempDir()
	store, err := artifacts.NewStore(filepath.Join(tempDir, "artifacts"), nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// a\n, 100 b's\n, z\n
	content := "a\n" + strings.Repeat("b", 100) + "\nz\n"
	putRes, err := store.PutBytes(t.Context(), "test_proj", "evidence", "text/plain", []byte(content), 0)
	if err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	res, err := FetchContent(FetchContentOptions{
		Artifacts:  store,
		ProjectID:  "test_proj",
		ContentRef: putRes.Ref.Locator,
		Offset:     1,
		Limit:      10,
		MaxBytes:   4,
	})
	if err != nil {
		t.Fatalf("FetchContent: %v", err)
	}

	if res.Content != "a" {
		t.Errorf("Expected Content 'a', got %q (possible hole or skipped line)", res.Content)
	}
	if res.ReturnedCount != 1 {
		t.Errorf("Expected ReturnedCount 1, got %d", res.ReturnedCount)
	}
	if res.NextOffset != 2 {
		t.Errorf("Expected NextOffset 2 (pointing to first omitted line), got %d", res.NextOffset)
	}
	if !res.Truncated {
		t.Errorf("Expected Truncated true")
	}
	if !res.HasMore {
		t.Errorf("Expected HasMore true")
	}
}
