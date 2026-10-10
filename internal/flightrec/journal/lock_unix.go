//go:build unix

package journal

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"github.com/olostan/DevCadence/internal/errs"
)

func flockNB(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

// classifyLock maps a flock result to the writer's lock error contract.
func classifyLock(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return errs.New(errs.CategoryConflict, "journal: stream already has a writer")
	}
	return fmt.Errorf("journal: flock: %w", err)
}

// lockFile takes the exclusive, non-blocking single-writer lock. flock is
// advisory and per open file description, so it also refuses a second Open of
// the same stream within one process.
func lockFile(f *os.File) error { return classifyLock(flockNB(f)) }

// unlockFile releases the lock (closing the descriptor also releases it).
func unlockFile(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }

// lockHeld reports whether another descriptor holds the lock, releasing it
// again when it was free.
func lockHeld(f *os.File) (bool, error) {
	err := flockNB(f)
	if err == nil {
		return false, unlockFile(f)
	}
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return true, nil
	}
	return false, fmt.Errorf("journal: flock: %w", err)
}
