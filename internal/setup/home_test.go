package setup

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveHomeUsesEnvVarWhenSet(t *testing.T) {
	custom := t.TempDir()
	t.Setenv(homeEnvVar, custom)

	home, err := ResolveHome()
	if err != nil {
		t.Fatalf("ResolveHome: %v", err)
	}
	if home != custom {
		t.Fatalf("ResolveHome() = %q, want %q", home, custom)
	}
}

func TestResolveHomeFallsBackToDotDevcadence(t *testing.T) {
	t.Setenv(homeEnvVar, "")

	home, err := ResolveHome()
	if err != nil {
		t.Fatalf("ResolveHome: %v", err)
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory available in this environment: %v", err)
	}
	want := filepath.Join(userHome, ".devcadence")
	if home != want {
		t.Fatalf("ResolveHome() = %q, want %q", home, want)
	}
}

func TestResolveHomeRejectsRelativeOverride(t *testing.T) {
	t.Setenv(homeEnvVar, "relative/path")

	if _, err := ResolveHome(); err == nil {
		t.Fatal("ResolveHome accepted a relative DEVCADENCE_HOME; expected an error")
	}
}

func TestEnsureLayoutIsIdempotentAndCreatesExpectedDirs(t *testing.T) {
	home := t.TempDir()

	if err := EnsureLayout(home); err != nil {
		t.Fatalf("EnsureLayout (first call): %v", err)
	}
	if err := EnsureLayout(home); err != nil {
		t.Fatalf("EnsureLayout (second call, must be idempotent): %v", err)
	}

	for _, dir := range []string{"state", filepath.Join("artifacts", "setup"), "tmp"} {
		path := filepath.Join(home, dir)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s exists but is not a directory", path)
		}
		if perm := info.Mode().Perm(); perm != 0700 {
			t.Errorf("%s has mode %o, want 0700", path, perm)
		}
	}
}

func TestEnsureLayoutRejectsRelativeHome(t *testing.T) {
	if err := EnsureLayout("relative/home"); err == nil {
		t.Fatal("EnsureLayout accepted a relative home path; expected an error")
	}
}

func TestEnsureLayoutTightensPreExistingPermissiveDirs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file mode semantics do not apply on Windows")
	}
	home := t.TempDir()

	for _, dir := range []string{"state", filepath.Join("artifacts", "setup"), "tmp"} {
		path := filepath.Join(home, dir)
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatalf("pre-create %s: %v", path, err)
		}
	}

	if err := EnsureLayout(home); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	for _, dir := range []string{"state", filepath.Join("artifacts", "setup"), "tmp"} {
		path := filepath.Join(home, dir)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if perm := info.Mode().Perm(); perm != 0700 {
			t.Errorf("%s has mode %o after EnsureLayout, want 0700 (pre-existing 0755 must be tightened)", path, perm)
		}
	}
}
