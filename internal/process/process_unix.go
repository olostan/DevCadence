//go:build unix

package process

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcAttrs places the child in its own process group so the whole tree
// it spawns (a shell wrapper script, a build tool's own children) can be
// killed together on timeout or cancellation (ENGINEERING_STANDARDS.md §7,
// §9: "cancellation must propagate down process trees where feasible").
func setProcAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup sends SIGKILL to the process group led by p, so children
// spawned by the controlled command do not outlive it.
func killProcessGroup(p *os.Process) {
	if p == nil {
		return
	}
	// Negative pid targets the whole process group created by Setpgid.
	_ = syscall.Kill(-p.Pid, syscall.SIGKILL)
	// Also signal the leader directly in case the group signal raced process
	// creation and the group was not yet established.
	_ = p.Kill()
}

// signalName reports the terminating signal name, if the process was killed
// by one, so a timeout/cancellation kill is distinguishable in Result.
func signalName(exitErr *exec.ExitError) string {
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return ""
	}
	return status.Signal().String()
}
