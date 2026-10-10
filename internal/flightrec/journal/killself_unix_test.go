//go:build unix

package journal

import (
	"os"
	"syscall"
)

// killSelf terminates the helper process without running defers.
func killSelf() { _ = syscall.Kill(os.Getpid(), syscall.SIGKILL) }
