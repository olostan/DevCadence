//go:build unix

package validation

import (
	"os"
	"os/exec"
	"syscall"
)

func setServiceProcAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killServiceGroup(p *os.Process, sig syscall.Signal) {
	if p == nil {
		return
	}
	_ = syscall.Kill(-p.Pid, sig)
	if sig == syscall.SIGKILL {
		_ = p.Kill()
	}
}
