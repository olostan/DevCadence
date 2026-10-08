package execrt_test

import (
	"bytes"
	"context"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/execrt"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/repository"
)

func TestAcquireProjectLock(t *testing.T) {
	ctx := context.Background()
	stateDir := t.TempDir()
	projectID := "proj-test-1"

	// 1. Initial lock acquisition succeeds
	lock1, err := execrt.AcquireProjectLock(ctx, stateDir, projectID)
	if err != nil {
		t.Fatalf("expected AcquireProjectLock to succeed, got %v", err)
	}
	if lock1 == nil {
		t.Fatal("expected non-nil lock")
	}
	if lock1.Path() == "" {
		t.Error("expected non-empty lock path")
	}

	// 2. Second concurrent lock on same projectID fails with CategoryConflict
	lock2, err := execrt.AcquireProjectLock(ctx, stateDir, projectID)
	if err == nil {
		_ = lock2.Close()
		t.Fatal("expected second lock acquisition to fail, but it succeeded")
	}
	if cat := errs.CategoryOf(err); cat != errs.CategoryConflict {
		t.Fatalf("expected CategoryConflict, got category %q (err: %v)", cat, err)
	}

	// 3. Different projectID can be locked concurrently
	otherProject := "proj-test-2"
	lockOther, err := execrt.AcquireProjectLock(ctx, stateDir, otherProject)
	if err != nil {
		t.Fatalf("expected lock on different project to succeed, got %v", err)
	}
	_ = lockOther.Close()

	// 4. Closing lock1 allows re-acquisition on same projectID
	if err := lock1.Close(); err != nil {
		t.Fatalf("failed to close lock1: %v", err)
	}

	lock3, err := execrt.AcquireProjectLock(ctx, stateDir, projectID)
	if err != nil {
		t.Fatalf("expected re-acquisition after Close to succeed, got %v", err)
	}
	defer lock3.Close()

	// 5. Calling Close repeatedly is idempotent and returns nil
	if err := lock1.Close(); err != nil {
		t.Errorf("second close on lock1 should return nil, got %v", err)
	}
	var nilLock *execrt.ProjectLock
	if nilLock.Path() != "" {
		t.Errorf("path on nil lock should be empty, got %q", nilLock.Path())
	}
	if err := nilLock.Close(); err != nil {
		t.Errorf("close on nil lock should return nil, got %v", err)
	}
}

func TestAcquireProjectLock_Validation(t *testing.T) {
	ctx := context.Background()
	stateDir := t.TempDir()

	testCases := []struct {
		name      string
		stateDir  string
		projectID string
	}{
		{"empty stateDir", "", "proj-1"},
		{"whitespace stateDir", "   ", "proj-1"},
		{"empty projectID", stateDir, ""},
		{"whitespace projectID", stateDir, "   "},
		{"path traversal slash", stateDir, "sub/dir"},
		{"path traversal backslash", stateDir, `sub\dir`},
		{"path traversal dot dot", stateDir, "../escape"},
		{"path traversal relative dot dot", stateDir, ".."},
		{"path traversal single dot", stateDir, "."},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := execrt.AcquireProjectLock(ctx, tc.stateDir, tc.projectID)
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			if cat := errs.CategoryOf(err); cat != errs.CategoryInvalidArgument {
				t.Errorf("expected CategoryInvalidArgument, got %q (err: %v)", cat, err)
			}
		})
	}
}

func TestAcquireProjectLock_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stateDir := t.TempDir()
	_, err := execrt.AcquireProjectLock(ctx, stateDir, "proj-1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestMemoryArtifactSink(t *testing.T) {
	ctx := context.Background()
	sink := execrt.NewMemoryArtifactSink()
	projectID := "proj-artifacts"
	kind := "diff"
	mediaType := "text/x-diff"

	// 1. Content <= 4 MiB succeeds
	content := []byte("diff --git a/foo.go b/foo.go\n+hello world")
	ref, err := sink.Put(ctx, projectID, kind, mediaType, content)
	if err != nil {
		t.Fatalf("expected Put to succeed, got %v", err)
	}

	if err := ref.Validate(); err != nil {
		t.Fatalf("expected ArtifactRef to be valid, got %v", err)
	}
	if ref.Kind != kind {
		t.Errorf("expected kind %q, got %q", kind, ref.Kind)
	}
	if ref.MediaType != mediaType {
		t.Errorf("expected mediaType %q, got %q", mediaType, ref.MediaType)
	}
	if ref.SizeBytes != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), ref.SizeBytes)
	}
	expectedDigest := protocol.DigestBytes(content)
	if ref.Digest != expectedDigest {
		t.Errorf("expected digest %s, got %s", expectedDigest, ref.Digest)
	}

	// Stored content is retrievable
	retrieved, ok := sink.Get(ref.Locator)
	if !ok {
		t.Fatal("expected content to be retrievable by locator")
	}
	if !bytes.Equal(retrieved, content) {
		t.Errorf("retrieved content mismatch: want %q, got %q", content, retrieved)
	}

	// Non-existent locator returns false
	if _, ok := sink.Get("nonexistent"); ok {
		t.Error("expected non-existent locator to return false")
	}

	// 2. Exactly 4 MiB succeeds
	exactContent := make([]byte, execrt.MaxArtifactBytes)
	exactRef, err := sink.Put(ctx, projectID, "blob", "application/octet-stream", exactContent)
	if err != nil {
		t.Fatalf("expected 4 MiB content to succeed, got %v", err)
	}
	if exactRef.SizeBytes != execrt.MaxArtifactBytes {
		t.Errorf("expected size %d, got %d", execrt.MaxArtifactBytes, exactRef.SizeBytes)
	}

	// 3. Content > 4 MiB fails with CategoryInvalidArgument
	oversizedContent := make([]byte, execrt.MaxArtifactBytes+1)
	_, err = sink.Put(ctx, projectID, kind, mediaType, oversizedContent)
	if err == nil {
		t.Fatal("expected error for content > 4 MiB, got nil")
	}
	if cat := errs.CategoryOf(err); cat != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument, got %q (err: %v)", cat, err)
	}

	// 4. Cancelled context fails
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sink.Put(cancelledCtx, projectID, kind, mediaType, content); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestDirectoryArtifactSink(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()
	sink := execrt.NewDirectoryArtifactSink(baseDir)
	projectID := "proj-disk"
	kind := "test-report"
	mediaType := "application/json"

	content := []byte(`{"status":"passed"}`)
	ref, err := sink.Put(ctx, projectID, kind, mediaType, content)
	if err != nil {
		t.Fatalf("expected Put to succeed, got %v", err)
	}

	if err := ref.Validate(); err != nil {
		t.Fatalf("expected valid ArtifactRef, got %v", err)
	}

	data, err := os.ReadFile(ref.Locator)
	if err != nil {
		t.Fatalf("failed to read persisted artifact file: %v", err)
	}
	if !bytes.Equal(data, content) {
		t.Errorf("persisted content mismatch: want %q, got %q", content, data)
	}

	// Oversized fails
	oversized := make([]byte, execrt.MaxArtifactBytes+1)
	_, err = sink.Put(ctx, projectID, kind, mediaType, oversized)
	if err == nil {
		t.Fatal("expected oversized artifact to fail, got nil")
	}
	if cat := errs.CategoryOf(err); cat != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument, got %q", cat)
	}

	// Empty baseDir fails
	emptySink := execrt.NewDirectoryArtifactSink("")
	if _, err := emptySink.Put(ctx, projectID, kind, mediaType, content); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for empty baseDir, got %v", err)
	}

	// Cancelled context fails
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sink.Put(cancelledCtx, projectID, kind, mediaType, content); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestRepositoryProvider(t *testing.T) {
	ctx := context.Background()

	repo1 := &repository.Repository{ProjectID: "proj-1", Path: "/repo/1"}
	repo2 := &repository.Repository{ProjectID: "proj-2", Path: "/repo/2"}

	t.Run("single repository instance", func(t *testing.T) {
		provider := execrt.NewSingleRepositoryProvider(repo1)

		// Found returns repository
		found, err := provider.Repository(ctx, "proj-1")
		if err != nil {
			t.Fatalf("expected repository to be found, got %v", err)
		}
		if found != repo1 {
			t.Errorf("expected repo1, got %+v", found)
		}

		// Unknown returns CategoryNotFound
		_, err = provider.Repository(ctx, "proj-unknown")
		if err == nil {
			t.Fatal("expected error for unknown project, got nil")
		}
		if cat := errs.CategoryOf(err); cat != errs.CategoryNotFound {
			t.Errorf("expected CategoryNotFound, got %q (err: %v)", cat, err)
		}
	})

	t.Run("multi-repository map", func(t *testing.T) {
		provider := execrt.NewMapRepositoryProvider(map[string]*repository.Repository{
			"proj-1": repo1,
			"proj-2": repo2,
		})

		found1, err := provider.Repository(ctx, "proj-1")
		if err != nil || found1 != repo1 {
			t.Fatalf("expected repo1, got %v (err: %v)", found1, err)
		}

		found2, err := provider.Repository(ctx, "proj-2")
		if err != nil || found2 != repo2 {
			t.Fatalf("expected repo2, got %v (err: %v)", found2, err)
		}

		_, err = provider.Repository(ctx, "proj-unknown")
		if err == nil {
			t.Fatal("expected error for unknown project, got nil")
		}
		if cat := errs.CategoryOf(err); cat != errs.CategoryNotFound {
			t.Errorf("expected CategoryNotFound, got %q", cat)
		}
	})

	t.Run("nil provider or empty projectID", func(t *testing.T) {
		var nilProvider *execrt.SingleRepositoryProvider
		_, err := nilProvider.Repository(ctx, "proj-1")
		if cat := errs.CategoryOf(err); cat != errs.CategoryNotFound {
			t.Errorf("expected CategoryNotFound for nil provider, got %q", cat)
		}

		provider := execrt.NewSingleRepositoryProvider(repo1)
		_, err = provider.Repository(ctx, "")
		if cat := errs.CategoryOf(err); cat != errs.CategoryNotFound {
			t.Errorf("expected CategoryNotFound for empty projectID, got %q", cat)
		}

		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := provider.Repository(cancelledCtx, "proj-1"); !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})
}

func TestPackageIsolation(t *testing.T) {
	// Parse all production (.go without _test.go) files in internal/execrt
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go") && strings.HasSuffix(fi.Name(), ".go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("failed to parse internal/execrt files: %v", err)
	}

	allowedRepoImports := map[string]bool{
		"github.com/olostan/DevCadence/internal/repository": true,
		"github.com/olostan/DevCadence/internal/protocol":   true,
		"github.com/olostan/DevCadence/internal/errs":       true,
	}

	fileCount := 0
	for _, pkg := range pkgs {
		for fileName, file := range pkg.Files {
			fileCount++
			for _, imp := range file.Imports {
				importPath := strings.Trim(imp.Path.Value, `"`)
				if strings.HasPrefix(importPath, "github.com/olostan/DevCadence/") {
					if !allowedRepoImports[importPath] {
						t.Errorf("file %s imports disallowed repository package: %s", filepath.Base(fileName), importPath)
					}
				} else {
					// External non-stdlib packages contain a dot before the first slash.
					firstSeg := strings.Split(importPath, "/")[0]
					if strings.Contains(firstSeg, ".") {
						t.Errorf("file %s imports disallowed non-standard library package: %s", filepath.Base(fileName), importPath)
					}
				}
			}
		}
	}

	if fileCount == 0 {
		t.Fatal("no production files were checked for isolation")
	}
}
