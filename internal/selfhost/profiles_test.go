package selfhost_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/selfhost"
	"github.com/olostan/DevCadence/internal/taskexec"
)

func TestRepoProfileSource(t *testing.T) {
	repo := t.TempDir()
	src := selfhost.NewRepoProfileSource(repo)
	get := func() error { _, err := src.Profile(t.Context(), "p", selfhost.PostCheckProfileID); return err }

	if err := get(); !errors.Is(err, taskexec.ErrProfileNotConfigured) {
		t.Fatalf("absent file: err = %v, want ErrProfileNotConfigured", err)
	}
	path := filepath.Join(repo, selfhost.ValidationProfileRelPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(body string) {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(validationYAML)
	prof, err := src.Profile(t.Context(), "p", selfhost.PostCheckProfileID)
	if err != nil || len(prof.Checks) != 1 || prof.Checks[0].ID != "go-test" {
		t.Fatalf("profile = %+v err %v", prof, err)
	}
	if _, err := src.Profile(t.Context(), "p", "other"); err == nil || errors.Is(err, taskexec.ErrProfileNotConfigured) {
		t.Errorf("missing profile name: err = %v, want a real error", err)
	}
	write("profiles: [not, a, map")
	if err := get(); err == nil || errors.Is(err, taskexec.ErrProfileNotConfigured) {
		t.Errorf("invalid yaml: err = %v", err)
	}
	write(strings.Repeat("#", 300*1024))
	if err := get(); err == nil {
		t.Errorf("oversized profile accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/hostname", path); err == nil {
		if err := get(); err == nil || errors.Is(err, taskexec.ErrProfileNotConfigured) {
			t.Errorf("symlinked profile: err = %v, want refusal", err)
		}
	}
}
