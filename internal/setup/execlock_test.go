package setup

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestExecutionLockSerializesConcurrentRuns(t *testing.T) {
	home := t.TempDir()

	first, err := AcquireExecutionLock(home)
	if err != nil {
		t.Fatalf("AcquireExecutionLock (first): %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		second, err := AcquireExecutionLock(home)
		if err != nil {
			t.Errorf("AcquireExecutionLock (second): %v", err)
			return
		}
		close(acquired)
		_ = second.Release()
	}()

	select {
	case <-acquired:
		t.Fatal("second AcquireExecutionLock succeeded while the first lock was still held")
	case <-time.After(200 * time.Millisecond):
		// Expected: the second call is still blocked.
	}

	if err := first.Release(); err != nil {
		t.Fatalf("Release (first): %v", err)
	}

	select {
	case <-acquired:
		// Expected: releasing the first lock lets the second proceed.
	case <-time.After(2 * time.Second):
		t.Fatal("second AcquireExecutionLock did not succeed after the first lock was released")
	}
}

func TestAcquireExecutionLockRejectsRelativeHome(t *testing.T) {
	if _, err := AcquireExecutionLock("relative/home"); err == nil {
		t.Fatal("AcquireExecutionLock accepted a relative home path; expected an error")
	}
}

func TestExecutionLockReleaseIsIdempotent(t *testing.T) {
	home := t.TempDir()
	l, err := AcquireExecutionLock(home)
	if err != nil {
		t.Fatalf("AcquireExecutionLock: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("Release (first): %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("Release (second, must be a no-op): %v", err)
	}
}

func TestExecutionLockFileModeIsHardenedOnExistingPermissiveFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file mode semantics do not apply on Windows")
	}
	home := t.TempDir()
	stateDir := filepath.Join(home, "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("pre-create state dir: %v", err)
	}
	lockPath := filepath.Join(stateDir, "setup.lock")
	if err := os.WriteFile(lockPath, nil, 0644); err != nil {
		t.Fatalf("pre-create permissive lock file: %v", err)
	}

	l, err := AcquireExecutionLock(home)
	if err != nil {
		t.Fatalf("AcquireExecutionLock: %v", err)
	}
	defer l.Release()

	info, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("lock file mode = %o, want 0600 (pre-existing 0644 must be tightened)", perm)
	}
	if info, err := os.Stat(stateDir); err == nil && info.Mode().Perm() != 0700 {
		t.Errorf("state dir mode = %o, want 0700 (pre-existing 0755 must be tightened)", info.Mode().Perm())
	}
}
