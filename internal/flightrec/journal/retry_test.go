package journal

import (
	"errors"
	"testing"

	"github.com/olostan/DevCadence/internal/flightrec/wire"
)

// Pending segment-start health survives a failed flush: Flush and Close retry.
func TestPendingSegmentStartHealthIsRetried(t *testing.T) {
	env := newEnv(t)
	ffs := newFaultFS(nil)
	env.cfg.FS = ffs
	w := env.open(t)
	ffs.reset()
	ffs.rule = failNth("sync", 1, errBoom)
	if err := w.AppendCritical(ctxBG(), obsBody(5)); err == nil {
		t.Fatal("expected sync failure")
	}
	// The recovery segment's first health frame (write #2 after the header) fails.
	ffs.reset()
	ffs.rule = failNth("write", 2, errBoom)
	if err := w.AppendCritical(ctxBG(), obsBody(5)); !errors.Is(err, errBoom) {
		t.Fatalf("%v", err)
	}
	// Flush cannot open a segment either: the error surfaces.
	ffs.reset()
	ffs.rule = failNth("openfile", 1, errBoom)
	if err := w.Flush(ctxBG()); !errors.Is(err, errBoom) {
		t.Fatalf("Flush: %v", err)
	}
	// With a healthy FS, Close persists the pending health into a fresh segment.
	ffs.rule = nil
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
	_, recs := scanDir(t, env.dir, ScanOptions{})
	hk := healthKinds(recs)
	if hk[len(hk)-1] != wire.HealthWriterError {
		t.Fatalf("health kinds %v", hk)
	}
}
