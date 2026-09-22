//go:build !unix

package validation

import (
	"os"
	"os/exec"
)

func setServiceProcAttrs(cmd *exec.Cmd) {}

func killServiceGraceful(p *os.Process) {
	if p != nil {
		_ = p.Kill()
	}
}

func killServiceForced(p *os.Process) {
	if p != nil {
		_ = p.Kill()
	}
}

func isPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	return err == nil && p != nil
}

func terminateProcessGroup(pid int) {
	if pid <= 0 {
		return
	}
	p, err := os.FindProcess(pid)
	if err == nil && p != nil {
		_ = p.Kill()
	}
}
