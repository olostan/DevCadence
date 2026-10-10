//go:build unix

package process

import (
	"context"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A leader that exits while a backgrounded child still holds the output pipe
// must neither hang Run nor leave the child alive.
func TestRunKillsBackgroundedChildAfterNormalExit(t *testing.T) {
	start := time.Now()
	res, err := NewRunner().Run(context.Background(), Spec{
		Executable: "sh",
		Args:       []string{"-c", "sleep 60 & echo $!"},
		Dir:        testDir(t),
		Env:        BaseEnv(),
		Timeout:    30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 15*time.Second {
		t.Fatalf("Run blocked %v on the inherited pipe", took)
	}
	if res.Status != StatusCompleted || res.ExitCode != 0 {
		t.Fatalf("result = %+v", res)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(res.Stdout)))
	if err != nil {
		t.Fatalf("stdout %q: %v", res.Stdout, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("background child %d still alive after Run returned", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
