//go:build unix

package flightrec

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// R2-NB(f): a FIFO at <root>/node-id is never opened (Lstat says it is not a
// regular file), so the read can neither block nor consume it; the id is
// ephemeral. A blocking open would hang this test, which has no sleeps.
func TestNodeIDFileFIFO(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "node-id"), 0o600); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}
	id, eph := nodeIDFile(dir, newIDs())
	if !eph || !strings.HasPrefix(id, "nod_") {
		t.Fatalf("%q %v", id, eph)
	}
}
