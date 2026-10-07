package execrt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/olostan/DevCadence/internal/errs"
)

// ProjectLock represents an exclusive POSIX advisory lock on a project.
// Exactly one executor per project holds this lock during execution.
type ProjectLock struct {
	f    *os.File
	path string
}

// AcquireProjectLock acquires an exclusive non-blocking lock on projectID in stateDir.
// It creates stateDir/locks if necessary and locks stateDir/locks/<projectID>.lock.
// Returns errs.CategoryConflict if another process holds the lock.
// Rejects path traversal characters in projectID.
func AcquireProjectLock(ctx context.Context, stateDir, projectID string) (*ProjectLock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	trimmedStateDir := strings.TrimSpace(stateDir)
	if trimmedStateDir == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "stateDir is required")
	}

	trimmedProjectID := strings.TrimSpace(projectID)
	if trimmedProjectID == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "projectID is required")
	}

	if strings.Contains(projectID, "/") || strings.Contains(projectID, "\\") ||
		strings.Contains(projectID, "..") || filepath.Base(projectID) != projectID ||
		projectID == "." {
		return nil, errs.New(errs.CategoryInvalidArgument, "projectID %q contains invalid characters or path traversal", projectID)
	}

	lockDir := filepath.Join(trimmedStateDir, "locks")
	if err := os.MkdirAll(lockDir, 0700); err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "failed to create lock directory %s", lockDir)
	}

	path := filepath.Join(lockDir, trimmedProjectID+".lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "failed to open lock file %s", path)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, errs.New(errs.CategoryConflict, "project %s is currently locked by another process", trimmedProjectID)
		}
		return nil, errs.Wrap(errs.CategoryInternal, err, "failed to acquire lock on %s", path)
	}

	return &ProjectLock{f: f, path: path}, nil
}

// Close releases the lock and closes the lock file.
// If the lock is already closed, it returns nil.
func (l *ProjectLock) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	flockErr := syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	closeErr := l.f.Close()
	l.f = nil
	if flockErr != nil {
		return errs.Wrap(errs.CategoryInternal, flockErr, "failed to unlock %s", l.path)
	}
	if closeErr != nil {
		return errs.Wrap(errs.CategoryInternal, closeErr, "failed to close lock file %s", l.path)
	}
	return nil
}

// Path returns the path of the lock file.
func (l *ProjectLock) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}
