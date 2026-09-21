//go:build !unix

package process

import (
	"os"
	"os/exec"
)

// setProcAttrs is a no-op on platforms without POSIX process groups. Child
// trees are best-effort on these platforms; docs/SECURITY.md scopes the
// bootstrap posture to local-only daemons and does not require perfect
// cross-platform child-tree cleanup.
func setProcAttrs(cmd *exec.Cmd) {}

// killProcessGroup kills only the direct child process.
func killProcessGroup(p *os.Process) {
	if p == nil {
		return
	}
	_ = p.Kill()
}

// signalName has no signal information on this platform.
func signalName(exitErr *exec.ExitError) string { return "" }
