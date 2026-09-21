package artifacts

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/ids"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(filepath.Join(t.TempDir(), "artifacts"), ids.NewSequential())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return s
}

func TestPutAndOpenRoundTrip(t *testing.T) {
	s := newStore(t)
	res, err := s.PutBytes(context.Background(), "proj-a", "stdout", "text/plain", []byte("hello world"), 0)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if res.Ref.Digest == "" || res.Ref.SizeBytes != 11 {
		t.Fatalf("ref = %+v", res.Ref)
	}
	if res.Truncated {
		t.Fatal("unexpected truncation")
	}
	rc, err := s.Open(res.Ref)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "hello world" {
		t.Fatalf("data = %q", data)
	}
	if err := s.Verify(res.Ref); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestPutTruncation(t *testing.T) {
	s := newStore(t)
	res, err := s.PutBytes(context.Background(), "proj-a", "stdout", "", bytes.Repeat([]byte("x"), 1000), 100)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if !res.Truncated || !res.Ref.Truncated {
		t.Fatal("expected truncation")
	}
	if res.Ref.SizeBytes != 100 {
		t.Fatalf("size = %d", res.Ref.SizeBytes)
	}
}

func TestPutDeduplicatesIdenticalContent(t *testing.T) {
	s := newStore(t)
	r1, err := s.PutBytes(context.Background(), "proj-a", "stdout", "", []byte("same"), 0)
	if err != nil {
		t.Fatalf("put 1: %v", err)
	}
	r2, err := s.PutBytes(context.Background(), "proj-a", "stdout", "", []byte("same"), 0)
	if err != nil {
		t.Fatalf("put 2: %v", err)
	}
	if r1.Ref.Digest != r2.Ref.Digest || r1.Ref.Locator != r2.Ref.Locator {
		t.Fatalf("expected same object: %+v vs %+v", r1.Ref, r2.Ref)
	}
}

func TestVerifyDetectsCorruption(t *testing.T) {
	s := newStore(t)
	res, err := s.PutBytes(context.Background(), "proj-a", "stdout", "", []byte("original"), 0)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	res.Ref.Digest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	if err := s.Verify(res.Ref); errs.CategoryOf(err) != errs.CategoryIntegrity {
		t.Fatalf("expected integrity error, got %v", err)
	}
}

func TestOpenRejectsPathTraversalLocator(t *testing.T) {
	s := newStore(t)
	_, err := s.resolveLocator("artifact:../../../etc:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err == nil {
		t.Fatal("expected error for traversal-shaped project id")
	}
}

func TestOpenRejectsMalformedLocator(t *testing.T) {
	s := newStore(t)
	if _, err := s.resolveLocator("not-a-locator"); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category = %v", errs.CategoryOf(err))
	}
	if _, err := s.resolveLocator("artifact:proj:short"); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category = %v", errs.CategoryOf(err))
	}
}

func TestPutRejectsInvalidProjectID(t *testing.T) {
	s := newStore(t)
	_, err := s.PutBytes(context.Background(), "../escape", "stdout", "", []byte("x"), 0)
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category = %v", errs.CategoryOf(err))
	}
}

func TestPutIsAtomicNoPartialObjectOnCancel(t *testing.T) {
	s := newStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := strings.NewReader(strings.Repeat("y", 1<<20))
	_, err := s.Put(ctx, PutInput{ProjectID: "proj-a", Kind: "stdout", Reader: r})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	entries, _ := os.ReadDir(filepath.Join(s.root, "proj-a", "objects"))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "tmp-") {
			t.Fatalf("leaked temp file: %s", e.Name())
		}
	}
}
