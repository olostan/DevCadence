//go:build !unix

package journal

import "os"

// lockFile reports that single-writer locking is unavailable; the recorder
// then degrades to a no-op.
func lockFile(*os.File) error { return ErrLockUnsupported }

func unlockFile(*os.File) error { return nil }

func lockHeld(*os.File) (bool, error) { return false, ErrLockUnsupported }
