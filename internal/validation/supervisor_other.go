//go:build !unix

package validation

import (
	"os"
	"os/exec"
	"syscall"
)

func setServiceProcAttrs(cmd *exec.Cmd) {}

func killServiceGroup(p *os.Process, sig syscall.Signal) {
	if p != nil {
		_ = p.Kill()
	}
}
