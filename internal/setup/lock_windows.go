//go:build windows

package setup

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFileEx wraps windows.LockFileEx over the whole (practically unbounded)
// byte range of f. With no LOCKFILE_FAIL_IMMEDIATELY flag, the call blocks
// until the lock is available, matching lock_unix.go's blocking flock
// semantics.
func lockFileEx(f *os.File, flags uint32) error {
	ol := new(windows.Overlapped)
	return windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, ^uint32(0), ^uint32(0), ol)
}

func lockShared(f *os.File) error {
	return lockFileEx(f, 0)
}

func lockExclusive(f *os.File) error {
	return lockFileEx(f, windows.LOCKFILE_EXCLUSIVE_LOCK)
}

func unlock(f *os.File) error {
	ol := new(windows.Overlapped)
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, ^uint32(0), ^uint32(0), ol)
}
