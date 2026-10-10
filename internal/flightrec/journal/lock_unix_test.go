//go:build unix

package journal

import (
	"errors"
	"os"
	"syscall"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
)

func TestLockClassification(t *testing.T) {
	if err := classifyLock(nil); err != nil {
		t.Fatal(err)
	}
	if err := classifyLock(syscall.EWOULDBLOCK); errs.CategoryOf(err) != errs.CategoryConflict {
		t.Fatalf("%v", err)
	}
	err := classifyLock(syscall.EBADF)
	if errs.CategoryOf(err) == errs.CategoryConflict || !errors.Is(err, syscall.EBADF) {
		t.Fatalf("%v", err)
	}
	// lockHeld on a closed descriptor reports the flock error.
	f, err := os.CreateTemp(t.TempDir(), "lock")
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if held, err := lockHeld(f); err == nil || held {
		t.Fatalf("%v %v", held, err)
	}
}
