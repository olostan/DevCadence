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
		ContentRef: "artifact:test_proj:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	})
	if err == nil {
		t.Fatal("Expected error for nonexistent content_ref")
	}
}
