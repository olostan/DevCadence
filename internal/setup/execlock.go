package setup

import (
	"os"
	"path/filepath"

	"github.com/olostan/DevCadence/internal/errs"
)

// ExecutionLock guards $DEVCADENCE_HOME/state/setup.lock so that at most
// one setup/doctor run mutates operational state at a time (ADR-0014 §4).
type ExecutionLock struct {
	file *os.File
}

// AcquireExecutionLock opens (creating if absent) state/setup.lock under
// home and blocks until it holds an exclusive flock on it. A concurrent
// caller's AcquireExecutionLock blocks until this lock is Released,
// serializing rather than racing or rejecting concurrent runs.
func AcquireExecutionLock(home string) (*ExecutionLock, error) {
	if !filepath.IsAbs(home) {
		return nil, errs.New(errs.CategoryInvalidArgument, "AcquireExecutionLock: home must be an absolute path, got %q", home)
	}
	stateDir := filepath.Join(home, "state")
	if err := ensureDirMode(stateDir, 0700); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(stateDir, "setup.lock")
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "open %s", lockPath)
	}
	if err := ensureFileMode(lockPath, 0600); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := lockExclusive(f); err != nil {
		_ = f.Close()
		return nil, errs.Wrap(errs.CategoryInternal, err, "acquire exclusive lock on %s", lockPath)
	}
	return &ExecutionLock{file: f}, nil
}

// Release unlocks and closes the execution lock file.
func (l *ExecutionLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := unlock(l.file)
	closeErr := l.file.Close()
	l.file = nil
	if unlockErr != nil {
		return errs.Wrap(errs.CategoryInternal, unlockErr, "release execution lock")
	}
	if closeErr != nil {
		return errs.Wrap(errs.CategoryInternal, closeErr, "close execution lock file")
	}
	return nil
}
