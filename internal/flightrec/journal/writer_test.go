package journal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/flightrec/wire"
)

// A-6: the exact byte layout of header and frames (independent CRC32C).
func TestFramingLayout(t *testing.T) {
	env := newEnv(t)
	w := env.open(t)
	if err := w.AppendCritical(ctxBG(), startBody("a.b")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(env.dir, "segment-000001.pbj"))
	if err != nil {
		t.Fatal(err)
	}
	castagnoli := crc32.MakeTable(crc32.Castagnoli)
	if !bytes.Equal(data[:8], []byte("\x89DCJRNL\n")) {
		t.Fatalf("magic %x", data[:8])
	}
	if binary.LittleEndian.Uint16(data[8:]) != 1 || binary.LittleEndian.Uint16(data[10:]) != 0 {
		t.Fatalf("format_version/flags %x", data[8:12])
	}
	metaLen := int(binary.LittleEndian.Uint32(data[12:]))
	if metaLen < 1 || metaLen > 4096 {
		t.Fatalf("meta_len %d", metaLen)
	}
	var hdr wire.SegmentHeader
	if err := hdr.Unmarshal(data[16 : 16+metaLen]); err != nil {
		t.Fatal(err)
	}
	if hdr.SegmentIndex != 1 || hdr.FirstSequence != 1 || hdr.NodeID != "nod_t" || hdr.RuntimeID != "run_t" ||
		hdr.StreamID != "main" || hdr.WriterVersion != "test" || hdr.MaxRecordBytes != DefaultMaxRecordBytes ||
		hdr.WriterPID != uint32(os.Getpid()) || hdr.SchemaVersion != 1 || hdr.CreatedAt == nil {
		t.Fatalf("header %+v", hdr)
	}
	if got, want := binary.LittleEndian.Uint32(data[16+metaLen:]), crc32.Checksum(data[:16+metaLen], castagnoli); got != want {
		t.Fatalf("header crc %08x want %08x", got, want)
	}
	off := 20 + metaLen
	frames := 0
	for off < len(data) {
		if string(data[off:off+4]) != "FRM1" {
			t.Fatalf("sync at %d: %x", off, data[off:off+4])
		}
		n := int(binary.LittleEndian.Uint32(data[off+4:]))
		payload := data[off+12 : off+12+n]
		want := crc32.Update(crc32.Checksum(data[off+4:off+8], castagnoli), castagnoli, payload)
		if got := binary.LittleEndian.Uint32(data[off+8:]); got != want {
			t.Fatalf("frame crc %08x want %08x (must cover the length)", got, want)
		}
		var rec wire.JournalRecord
		if err := rec.Unmarshal(payload); err != nil || rec.Validate() != nil {
			t.Fatalf("payload does not decode: %v", err)
		}
		off += 12 + n
		frames++
	}
	if frames != 2 || off != len(data) { // STREAM_STARTED health + the critical record
		t.Fatalf("frames=%d off=%d len=%d", frames, off, len(data))
	}
	if w.Stats().Critical != 1 {
		t.Fatal("unexpected stats")
	}

	// Flipping a bit of a length field must be detected (the CRC covers it).
	frameOff := 20 + metaLen
	bad := append([]byte(nil), data...)
	bad[frameOff+4] ^= 0x01
	rep, _ := scanBytes(t, bad, 1, ScanOptions{})
	if rep.CleanEOF || len(rep.Findings) == 0 {
		t.Fatalf("length corruption undetected: %+v", rep)
	}
	switch rep.Findings[0].Code {
	case BadLength, BadCRC, TornTail:
	default:
		t.Fatalf("unexpected finding %v", rep.Findings[0].Code)
	}
}

// A-7: happy path with critical and diagnostic records.
func TestHappyPath(t *testing.T) {
	env := newEnv(t)
	w := env.open(t)
	const diag, crit = 7, 5
	for i := 0; i < diag; i++ {
		if !w.AppendDiagnostic(obsBody(10)) {
			t.Fatal("diagnostic refused")
		}
	}
	if err := w.Flush(ctxBG()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < crit; i++ {
		if err := w.AppendCritical(ctxBG(), startBody("x.y")); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
	rep, recs := scanDir(t, env.dir, ScanOptions{})
	if !rep.Clean || rep.Records != 1+diag+crit {
		t.Fatalf("clean=%v records=%d findings=%v", rep.Clean, rep.Records, rep.Findings)
	}
	for i, r := range recs {
		if r.Record.StreamSequence != uint64(i+1) {
			t.Fatalf("record %d has sequence %d", i, r.Record.StreamSequence)
		}
	}
	if recs[0].Record.Health == nil || recs[0].Record.Health.Kind != wire.HealthStreamStarted {
		t.Fatalf("first record %+v", recs[0].Record)
	}
	ev := map[string]bool{}
	for _, r := range recs {
		rec := r.Record
		if ev[rec.EventID] || rec.NodeID != "nod_t" || rec.RuntimeID != "run_t" || rec.StreamID != "main" || rec.SchemaVersion != 1 {
			t.Fatalf("bad envelope %+v", rec)
		}
		ev[rec.EventID] = true
	}
	for _, r := range recs[1+diag:] {
		if r.Record.Durability != wire.DurabilityCritical || r.Record.Type != wire.RecordTypeOperationStart {
			t.Fatalf("critical envelope %+v", r.Record)
		}
	}
	for _, r := range recs[1 : 1+diag] {
		if r.Record.Durability != wire.DurabilityDiagnostic {
			t.Fatalf("diagnostic envelope %+v", r.Record)
		}
	}
	st := w.Stats()
	if st.Appended != diag+crit || st.Critical != crit || st.LastSequence != 1+diag+crit || st.Broken || st.SegmentIndex != 1 {
		t.Fatalf("stats %+v", st)
	}
}

// A-8 and A-16: rotation, first_sequence continuity and re-fill sequencing.
func TestRotation(t *testing.T) {
	env := newEnv(t)
	env.cfg.Limits = Limits{MaxRecordBytes: 4096, MaxSegmentBytes: 24576}
	ffs := newFaultFS(nil)
	env.cfg.FS = ffs
	w := env.open(t)
	for i := 0; i < 60; i++ {
		if err := w.AppendCritical(ctxBG(), obsBody(1000)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
	files := segFiles(t, env.dir)
	if len(files) < 3 {
		t.Fatalf("expected several segments, got %d", len(files))
	}
	for _, f := range files {
		fi, _ := os.Stat(f)
		if fi.Size() > 24576 {
			t.Fatalf("%s is %d bytes, over MaxSegmentBytes", f, fi.Size())
		}
	}
	rep, recs := scanDir(t, env.dir, ScanOptions{})
	if !rep.Clean || len(rep.Segments) != len(files) {
		t.Fatalf("clean=%v findings=%v", rep.Clean, rep.Findings)
	}
	var lastSeq uint64
	firstOf := map[uint32]Scanned{}
	for _, r := range recs {
		if r.Record.StreamSequence != lastSeq+1 {
			t.Fatalf("sequence %d after %d", r.Record.StreamSequence, lastSeq)
		}
		lastSeq = r.Record.StreamSequence
		if _, ok := firstOf[r.Segment]; !ok {
			firstOf[r.Segment] = r
		}
	}
	prevLast := uint64(0)
	for i, sr := range rep.Segments {
		if sr.FirstSequence != prevLast+1 {
			t.Fatalf("segment %d first_sequence %d, previous ended at %d", sr.Index, sr.FirstSequence, prevLast)
		}
		prevLast = sr.LastSequence
		first := firstOf[sr.Index].Record
		want := wire.HealthSegmentRotated
		if i == 0 {
			want = wire.HealthStreamStarted
		}
		if first.Health == nil || first.Health.Kind != want {
			t.Fatalf("segment %d must start with %v health: %+v", sr.Index, want, first)
		}
		if i > 0 {
			// The record that triggered the rotation takes its sequence after the health frame.
			second := recs[firstIndex(recs, sr.Index)+1].Record
			if second.StreamSequence != first.StreamSequence+1 || second.Observation == nil {
				t.Fatalf("segment %d second record %+v", sr.Index, second)
			}
		}
	}
	// Health frames never trigger a rotation: exactly one OpenFile per segment.
	opens := 0
	for _, e := range ffs.logCopy() {
		if strings.HasPrefix(e, "openfile:segment-") {
			opens++
		}
	}
	if opens != len(files) {
		t.Fatalf("%d segment creations for %d segments", opens, len(files))
	}
}

func firstIndex(recs []Scanned, seg uint32) int {
	for i, r := range recs {
		if r.Segment == seg {
			return i
		}
	}
	return -1
}

// A-9: single writer, in-process and across processes.
func TestSingleWriter(t *testing.T) {
	env := newEnv(t)
	w := env.open(t)
	_, err := Open(ctxBG(), env.cfg)
	if errs.CategoryOf(err) != errs.CategoryConflict {
		t.Fatalf("second Open: %v", err)
	}
	if active, err := WriterActive(env.dir); err != nil || !active {
		t.Fatalf("WriterActive = %v, %v", active, err)
	}
	out, err := runHelperProcess(t, "open_expect_conflict", env.dir)
	if err != nil || !bytes.Contains(out, []byte("conflict")) {
		t.Fatalf("helper process must be refused: %v\n%s", err, out)
	}
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
	if active, err := WriterActive(env.dir); err != nil || active {
		t.Fatalf("WriterActive after Close = %v, %v", active, err)
	}
	if out, err := runHelperProcess(t, "open_ok", env.dir); err != nil {
		t.Fatalf("lock must be released by Close: %v\n%s", err, out)
	}
}

// Lock release at process exit, without Close or defers.
func TestLockReleasedOnProcessExit(t *testing.T) {
	env := newEnv(t)
	out, err := runHelperProcess(t, "hold_and_exit", env.dir)
	if err != nil || !bytes.Contains(out, []byte("ready")) {
		t.Fatalf("helper: %v\n%s", err, out)
	}
	w := env.open(t) // reopen succeeds: the lock died with the process
	_, recs := scanDir(t, env.dir, ScanOptions{})
	if len(bodiesOfType(recs, wire.RecordTypeOperationStart)) != 1 {
		t.Fatalf("helper's critical record missing: %v", seqsOf(recs))
	}
	if w.Stats().SegmentIndex != 2 {
		t.Fatalf("reopen must create segment 2, got %d", w.Stats().SegmentIndex)
	}
}

// A-14: a SIGKILLed process leaves its critical record on disk.
func TestSigkillCrash(t *testing.T) {
	env := newEnv(t)
	out, err := runHelperProcess(t, "kill_self", env.dir)
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("helper should die by signal: %v\n%s", err, out)
	}
	if ws, ok := exit.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Fatalf("helper not killed by SIGKILL: %v", err)
	}
	rep, recs := scanDir(t, env.dir, ScanOptions{})
	starts := bodiesOfType(recs, wire.RecordTypeOperationStart)
	if len(starts) != 1 || starts[0].Record.Start.OperationName != "helper.start" || !rep.Clean {
		t.Fatalf("starts=%d clean=%v findings=%v", len(starts), rep.Clean, rep.Findings)
	}
	if active, err := WriterActive(env.dir); err != nil || active {
		t.Fatalf("lock must be gone: %v %v", active, err)
	}
}

var errBoom = errors.New("boom")

// A-10: durability ordering and failure handling through the fault FS.
func TestCriticalDurabilityOrder(t *testing.T) {
	env := newEnv(t)
	ffs := newFaultFS(nil)
	env.cfg.FS = ffs
	w := env.open(t)
	ffs.reset()
	if err := w.AppendCritical(ctxBG(), startBody("x.y")); err != nil {
		t.Fatal(err)
	}
	log := ffs.logCopy()
	if len(log) != 2 || log[0] != "write:segment-000001.pbj" || log[1] != "sync:segment-000001.pbj" {
		t.Fatalf("critical append must be Write then Sync, got %v", log)
	}
}

func TestSyncFailurePoisonsAndRecovers(t *testing.T) {
	env := newEnv(t)
	ffs := newFaultFS(nil)
	env.cfg.FS = ffs
	w := env.open(t)
	ffs.reset()
	ffs.rule = failNth("sync", 1, errBoom)
	err := w.AppendCritical(ctxBG(), startBody("first.one"))
	if err == nil || errs.CategoryOf(err) != errs.CategoryInternal || !errors.Is(err, errBoom) {
		t.Fatalf("sync failure must surface: %v", err)
	}
	st := w.Stats()
	if !st.Broken || !strings.Contains(st.LastError, "boom") {
		t.Fatalf("stats %+v", st)
	}
	ffs.rule = nil
	if err := w.AppendCritical(ctxBG(), startBody("second.one")); err != nil {
		t.Fatal(err)
	}
	if w.Stats().Broken || w.Stats().SegmentIndex != 2 {
		t.Fatalf("stats after recovery %+v", w.Stats())
	}
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
	rep, recs := scanDir(t, env.dir, ScanOptions{})
	if !rep.Clean {
		t.Fatalf("findings %v", rep.Findings)
	}
	kinds := healthKinds(recs)
	want := []wire.HealthKind{wire.HealthStreamStarted, wire.HealthSegmentRotated, wire.HealthWriterError}
	if len(kinds) != 3 || kinds[0] != want[0] || kinds[1] != want[1] || kinds[2] != want[2] {
		t.Fatalf("health kinds %v", kinds)
	}
	// The frame whose fsync failed is on disk (unsynced START without END is "unresolved").
	if len(bodiesOfType(recs, wire.RecordTypeOperationStart)) != 2 {
		t.Fatalf("both records expected: %v", seqsOf(recs))
	}
	var detail string
	for _, r := range recs {
		if r.Record.Health != nil && r.Record.Health.Kind == wire.HealthWriterError {
			detail = r.Record.Health.Detail
		}
	}
	if !strings.Contains(detail, "boom") {
		t.Fatalf("WRITER_ERROR detail %q", detail)
	}
}

func TestShortWritePoisonsAndSequenceNotConsumed(t *testing.T) {
	env := newEnv(t)
	ffs := newFaultFS(nil)
	env.cfg.FS = ffs
	w := env.open(t)
	ffs.reset()
	ffs.rule = func(op, _ string, n int) *fault {
		if op == "write" && n == 1 {
			return &fault{err: errBoom, short: 5, useShort: true}
		}
		return nil
	}
	if err := w.AppendCritical(ctxBG(), startBody("lost.one")); err == nil || !errors.Is(err, errBoom) {
		t.Fatalf("short write must fail: %v", err)
	}
	if w.Stats().LastSequence != 1 {
		t.Fatalf("sequence must not be consumed: %+v", w.Stats())
	}
	ffs.rule = nil
	if err := w.AppendCritical(ctxBG(), startBody("kept.one")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
	rep, recs := scanDir(t, env.dir, ScanOptions{})
	if got := codesOf(rep.Findings); len(got) != 1 || got[0] != TornTail || rep.Findings[0].Segment != 1 {
		t.Fatalf("expected one TORN_TAIL in the poisoned segment, got %v", rep.Findings)
	}
	for i := 1; i < len(recs); i++ {
		if recs[i].Record.StreamSequence != recs[i-1].Record.StreamSequence+1 {
			t.Fatalf("gap or duplicate: %v", seqsOf(recs))
		}
	}
}

// A write reported as failed may still have left a complete frame; the next
// segment then reuses its sequence and the scanner warns (documented, expected).
// With the boundary precedence of the scanner the warning is SEGMENT_ORDER.
func TestFailedWriteLeavingCompleteFrameIsExpectedWarning(t *testing.T) {
	env := newEnv(t)
	ffs := newFaultFS(nil)
	env.cfg.FS = ffs
	w := env.open(t)
	ffs.reset()
	ffs.rule = func(op, _ string, n int) *fault {
		if op == "write" && n == 1 {
			return &fault{err: errBoom, full: true}
		}
		return nil
	}
	if err := w.AppendCritical(ctxBG(), startBody("x.y")); !errors.Is(err, errBoom) {
		t.Fatalf("write failure must surface: %v", err)
	}
	ffs.rule = nil
	if err := w.AppendCritical(ctxBG(), startBody("z.y")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
	rep, _ := scanDir(t, env.dir, ScanOptions{})
	if rep.Clean {
		t.Fatalf("a reused sequence must be reported: %v", rep.Findings)
	}
	order := 0
	for _, f := range rep.Findings {
		if f.Code == SegmentOrder && f.Segment == 2 && f.Expected == 3 && f.Got == 2 {
			order++
		}
	}
	if order != 1 || len(rep.Findings) != 1 {
		t.Fatalf("expected exactly one SEGMENT_ORDER warning: %v", rep.Findings)
	}
}

func TestHeaderAndDirectoryFaultsDuringSegmentCreation(t *testing.T) {
	type tc struct {
		name     string
		rule     func(op, name string, n int) *fault
		leftover string // "none", "partial" (BAD_HEADER) or "complete" (valid header only)
	}
	cases := []tc{
		{"openfile", func(op, name string, n int) *fault {
			if op == "openfile" && name == "segment-000002.pbj" && n >= 1 {
				return &fault{err: errBoom}
			}
			return nil
		}, "none"},
		{"header write short", func(op, name string, n int) *fault {
			if op == "write" && name == "segment-000002.pbj" {
				return &fault{err: errBoom, short: 5, useShort: true}
			}
			return nil
		}, "partial"},
		{"header write empty", func(op, name string, n int) *fault {
			if op == "write" && name == "segment-000002.pbj" {
				return &fault{err: errBoom, short: 0, useShort: true}
			}
			return nil
		}, "partial"},
		{"header sync", func(op, name string, n int) *fault {
			if op == "sync" && name == "segment-000002.pbj" {
				return &fault{err: errBoom}
			}
			return nil
		}, "complete"},
		{"dir sync", func(op, name string, n int) *fault {
			if op == "syncdir" && strings.HasSuffix(name, "main") && n >= 1 {
				return &fault{err: errBoom}
			}
			return nil
		}, "complete"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := newEnv(t)
			ffs := newFaultFS(nil)
			env.cfg.FS = ffs
			w := env.open(t)
			// Poison segment 1 so the next append must create segment 2.
			ffs.reset()
			ffs.rule = failNth("sync", 1, errBoom)
			if err := w.AppendCritical(ctxBG(), startBody("p.q")); err == nil {
				t.Fatal("expected sync failure")
			}
			ffs.reset()
			ffs.rule = c.rule
			if err := w.AppendCritical(ctxBG(), startBody("r.s")); err == nil || !errors.Is(err, errBoom) {
				t.Fatalf("creation failure must surface: %v", err)
			}
			if !w.Stats().Broken || w.Stats().SegmentIndex != 2 {
				t.Fatalf("index must be consumed: %+v", w.Stats())
			}
			seg2 := filepath.Join(env.dir, "segment-000002.pbj")
			var before []byte
			switch c.leftover {
			case "none":
				if _, err := os.Stat(seg2); !os.IsNotExist(err) {
					t.Fatalf("no file expected: %v", err)
				}
			default:
				before, _ = os.ReadFile(seg2)
			}
			ffs.rule = nil
			if err := w.AppendCritical(ctxBG(), startBody("t.u")); err != nil {
				t.Fatalf("retry must use the next index: %v", err)
			}
			if w.Stats().SegmentIndex != 3 || w.Stats().Broken {
				t.Fatalf("stats %+v", w.Stats())
			}
			if err := w.Close(ctxBG()); err != nil {
				t.Fatal(err)
			}
			if c.leftover != "none" {
				after, _ := os.ReadFile(seg2)
				if !bytes.Equal(before, after) {
					t.Fatal("leftover segment was modified")
				}
			}
			rep, recs := scanDir(t, env.dir, ScanOptions{})
			switch c.leftover {
			case "partial":
				found := false
				for _, f := range rep.Findings {
					if f.Code == BadHeader && f.Segment == 2 {
						found = true
					}
				}
				if !found {
					t.Fatalf("BAD_HEADER expected for the leftover: %v", rep.Findings)
				}
			case "complete":
				if !rep.Segments[1].HeaderOK || rep.Segments[1].Records != 0 {
					t.Fatalf("segment 2 should be header-only: %+v", rep.Segments[1])
				}
			}
			// Sequences continue after recovery: no duplicates, no regressions.
			var last uint64
			for _, r := range recs {
				if r.Record.StreamSequence <= last {
					t.Fatalf("sequence regressed: %v", seqsOf(recs))
				}
				last = r.Record.StreamSequence
			}
			for _, f := range rep.Findings {
				missingOK := c.leftover == "none" && f.Code == SegmentMissing && f.Segment == 2
				if !missingOK && (f.Code == SeqGap || f.Code == SeqRegression || f.Code == SegmentOrder || f.Code == SegmentMissing) {
					t.Fatalf("unexpected sequence finding %+v", f)
				}
			}
		})
	}
}

// A-10: the diagnostic path performs no Sync until Flush, tick, rotation or Close.
func TestDiagnosticPathDoesNotSync(t *testing.T) {
	env := newEnv(t)
	ffs := newFaultFS(nil)
	env.cfg.FS = ffs
	batches := make(chan struct{}, 64)
	env.cfg.afterBatch = func() { batches <- struct{}{} }
	tick := make(chan time.Time)
	env.cfg.tickC = tick
	w := env.open(t)
	ffs.reset()
	for i := 0; i < 3; i++ {
		if !w.AppendDiagnostic(obsBody(8)) {
			t.Fatal("refused")
		}
	}
	for done := 0; done < 3; {
		<-batches
		done = int(w.Stats().Appended)
	}
	if ffs.count("sync") != 0 || ffs.count("write") != 3 {
		t.Fatalf("diagnostic appends must write without syncing: sync=%d write=%d", ffs.count("sync"), ffs.count("write"))
	}
	if err := w.Flush(ctxBG()); err != nil {
		t.Fatal(err)
	}
	if ffs.count("sync") != 1 {
		t.Fatalf("Flush must sync once, got %d", ffs.count("sync"))
	}
	// Flush with nothing new does not sync again.
	if err := w.Flush(ctxBG()); err != nil || ffs.count("sync") != 1 {
		t.Fatalf("idle Flush: %v sync=%d", err, ffs.count("sync"))
	}
	// The ticker syncs unsynced diagnostics.
	w.AppendDiagnostic(obsBody(8))
	for int(w.Stats().Appended) < 4 {
		<-batches
	}
	tick <- time.Time{}
	tick <- time.Time{} // returns only after the first tick body finished
	if ffs.count("sync") != 2 {
		t.Fatalf("tick must sync: %d", ffs.count("sync"))
	}
	w.AppendDiagnostic(obsBody(8))
	for int(w.Stats().Appended) < 5 {
		<-batches
	}
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
	if ffs.count("sync") != 3 {
		t.Fatalf("Close must sync: %d", ffs.count("sync"))
	}
}

func TestBackgroundTickerIsCreatedAndStopped(t *testing.T) {
	env := newEnv(t)
	env.cfg.FlushEvery = time.Hour
	w := env.open(t)
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
}

// A-11: drop accounting with a parked drain goroutine.
func TestDropAccounting(t *testing.T) {
	env := newEnv(t)
	env.cfg.QueueSize = 4
	entered := make(chan struct{}, 16)
	release := make(chan struct{})
	env.cfg.afterBatch = func() { entered <- struct{}{}; <-release }
	w := env.open(t)
	if !w.AppendDiagnostic(obsBody(8)) {
		t.Fatal("first refused")
	}
	<-entered // batch 1 written; the drain goroutine is parked between batches
	accepted, refused := 0, 0
	for i := 0; i < 6; i++ {
		if w.AppendDiagnostic(obsBody(8)) {
			accepted++
		} else {
			refused++
		}
	}
	if accepted != 4 || refused != 2 || w.Stats().Dropped != 2 {
		t.Fatalf("accepted=%d refused=%d dropped=%d", accepted, refused, w.Stats().Dropped)
	}
	// A critical append completes while the queue is full and the drain is parked.
	crit := make(chan error, 1)
	go func() { crit <- w.AppendCritical(ctxBG(), startBody("c.d")) }()
	select {
	case err := <-crit:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("critical append blocked behind the diagnostic queue")
	}
	close(release)
	if err := w.Flush(ctxBG()); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
	rep, recs := scanDir(t, env.dir, ScanOptions{})
	if !rep.Clean {
		t.Fatalf("findings %v", rep.Findings)
	}
	var dropped uint64
	for _, r := range recs {
		if r.Record.Health != nil && r.Record.Health.Kind == wire.HealthDroppedDiagnostics {
			dropped += r.Record.Health.DroppedCount
		}
	}
	if dropped != 2 || len(bodiesOfType(recs, wire.RecordTypeObservation)) != 5 {
		t.Fatalf("dropped=%d observations=%d", dropped, len(bodiesOfType(recs, wire.RecordTypeObservation)))
	}
	// DROPPED_DIAGNOSTICS precedes the next record of either class.
	for i, r := range recs {
		if r.Record.Health != nil && r.Record.Health.Kind == wire.HealthDroppedDiagnostics && recs[i+1].Record.Health != nil {
			t.Fatalf("dropped health followed by health: %v", seqsOf(recs))
		}
	}
}

// Flush drains more records than one batch in several mutex holds.
func TestFlushDrainsInBatches(t *testing.T) {
	env := newEnv(t)
	env.cfg.QueueSize = 8
	env.cfg.MaxBatch = 2
	entered := make(chan struct{}, 16)
	release := make(chan struct{})
	env.cfg.afterBatch = func() { entered <- struct{}{}; <-release }
	w := env.open(t)
	w.AppendDiagnostic(obsBody(8))
	<-entered
	for i := 0; i < 5; i++ {
		w.AppendDiagnostic(obsBody(8))
	}
	if err := w.Flush(ctxBG()); err != nil { // drains all 5 queued records itself
		t.Fatal(err)
	}
	if w.Stats().Appended != 6 {
		t.Fatalf("appended %d", w.Stats().Appended)
	}
	close(release)
}

func TestFlushAndAppendContextAndClosed(t *testing.T) {
	env := newEnv(t)
	w := env.open(t)
	ctx, cancel := context.WithCancel(ctxBG())
	cancel()
	if err := w.Flush(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Flush: %v", err)
	}
	if err := w.AppendCritical(ctx, startBody("a.b")); !errors.Is(err, context.Canceled) {
		t.Fatalf("AppendCritical: %v", err)
	}
	if w.Stats().Appended != 0 {
		t.Fatal("cancelled append wrote")
	}
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(ctxBG()); err != nil {
		t.Fatalf("second Close must return nil: %v", err)
	}
	if err := w.AppendCritical(ctxBG(), startBody("a.b")); !errors.Is(err, ErrClosed) {
		t.Fatalf("append after Close: %v", err)
	}
	if w.AppendDiagnostic(obsBody(1)) || w.Stats().Dropped != 1 {
		t.Fatal("diagnostic after Close must be refused and counted")
	}
	if err := w.Flush(ctxBG()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Flush after Close: %v", err)
	}
	// The shut flag also protects an append that raced past the closed check.
	w.mu.Lock()
	err := w.appendLocked(startBody("a.b"), DurabilityCritical)
	w.mu.Unlock()
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("appendLocked after shutdown: %v", err)
	}
}

func TestOpenWithCancelledContext(t *testing.T) {
	env := newEnv(t)
	ctx, cancel := context.WithCancel(ctxBG())
	cancel()
	if _, err := Open(ctx, env.cfg); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open: %v", err)
	}
}

// A-18: Close racing AppendDiagnostic, cancelled contexts, gated writes.
func TestCloseRacesDiagnosticAppends(t *testing.T) {
	env := newEnv(t)
	w := env.open(t)
	const goroutines, per = 8, 200
	var accepted, refused atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < per; i++ {
				if w.AppendDiagnostic(obsBody(4)) {
					accepted.Add(1)
				} else {
					refused.Add(1)
				}
			}
		}()
	}
	close(start)
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if accepted.Load()+refused.Load() != goroutines*per {
		t.Fatal("lost attempts")
	}
	rep, recs := scanDir(t, env.dir, ScanOptions{})
	if !rep.Clean {
		t.Fatalf("findings %v", rep.Findings)
	}
	obs := len(bodiesOfType(recs, wire.RecordTypeObservation))
	if int64(obs) != accepted.Load() {
		t.Fatalf("%d observations on disk, %d accepted", obs, accepted.Load())
	}
	if int64(w.Stats().Dropped) != refused.Load() {
		t.Fatalf("dropped %d, refused %d", w.Stats().Dropped, refused.Load())
	}
}

func TestContextCancelledWhileWriteGated(t *testing.T) {
	env := newEnv(t)
	ffs := newFaultFS(nil)
	env.cfg.FS = ffs
	w := env.open(t)
	started := make(chan struct{})
	release := make(chan struct{})
	ffs.reset()
	ffs.rule = func(op, _ string, n int) *fault {
		if op == "write" && n == 1 {
			return &fault{gate: func() { close(started); <-release }}
		}
		return nil
	}
	ctx, cancel := context.WithCancel(ctxBG())
	done := make(chan error, 1)
	go func() { done <- w.AppendCritical(ctx, startBody("g.h")) }()
	<-started
	cancel()
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("a started write must complete despite cancellation: %v", err)
	}
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
	_, recs := scanDir(t, env.dir, ScanOptions{})
	if len(bodiesOfType(recs, wire.RecordTypeOperationStart)) != 1 {
		t.Fatal("record missing")
	}
}

func TestCloseWithExpiredContextReleasesLock(t *testing.T) {
	env := newEnv(t)
	env.cfg.QueueSize = 8
	entered := make(chan struct{}, 16)
	release := make(chan struct{})
	env.cfg.afterBatch = func() { entered <- struct{}{}; <-release }
	w := env.open(t)
	w.AppendDiagnostic(obsBody(8))
	<-entered // drain goroutine parked
	w.AppendDiagnostic(obsBody(8))
	w.AppendDiagnostic(obsBody(8))
	ctx, cancel := context.WithCancel(ctxBG())
	cancel()
	err := w.Close(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Close: %v", err)
	}
	if w.Stats().Dropped != 2 {
		t.Fatalf("queued records must be counted as dropped: %d", w.Stats().Dropped)
	}
	if active, err := WriterActive(env.dir); err != nil || active {
		t.Fatalf("lock must be released: %v %v", active, err)
	}
	close(release)
	<-w.exited
}

// A-13: reopen over real files never modifies existing bytes.
func TestReopenAfterTornTail(t *testing.T) {
	env := newEnv(t)
	w := env.open(t)
	for i := 0; i < 3; i++ {
		if err := w.AppendCritical(ctxBG(), obsBody(50)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
	seg := filepath.Join(env.dir, "segment-000001.pbj")
	fi, _ := os.Stat(seg)
	if err := os.Truncate(seg, fi.Size()-7); err != nil { // tear the last frame
		t.Fatal(err)
	}
	before, _ := os.ReadFile(seg)
	digest := sha256.Sum256(before)
	repBefore, _ := scanDir(t, env.dir, ScanOptions{})
	tornAt := repBefore.Findings[0].Offset

	w2 := env.open(t)
	after, _ := os.ReadFile(seg)
	if sha256.Sum256(after) != digest {
		t.Fatal("existing segment was modified")
	}
	if w2.Stats().SegmentIndex != 2 {
		t.Fatalf("segment index %d", w2.Stats().SegmentIndex)
	}
	if err := w2.AppendCritical(ctxBG(), obsBody(5)); err != nil {
		t.Fatal(err)
	}
	if err := w2.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
	rep, recs := scanDir(t, env.dir, ScanOptions{})
	if rep.Clean || len(rep.Findings) != 1 || rep.Findings[0].Code != TornTail || rep.Findings[0].Severity != SeverityWarning {
		t.Fatalf("torn tail of the old segment must be a warning: %v", rep.Findings)
	}
	var reopen *wire.JournalHealth
	for _, r := range recs {
		if r.Segment == 2 && r.Record.Health != nil && reopen == nil {
			reopen = r.Record.Health
		}
	}
	if reopen == nil || reopen.Kind != wire.HealthRecoveryReopen || reopen.DetailCode != "TORN_TAIL" || reopen.Offset != tornAt {
		t.Fatalf("RECOVERY_REOPEN %+v (torn at %d)", reopen, tornAt)
	}
	// Sequence continues after the last intact record (health 1 + two intact records = 3).
	seqs := seqsOf(recs)
	if seqs[2] != 3 || seqs[3] != 4 {
		t.Fatalf("sequences %v", seqs)
	}
}

func TestReopenOfCleanStreamAndScanFailures(t *testing.T) {
	env := newEnv(t)
	w := env.open(t)
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
	w2 := env.open(t)
	if err := w2.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
	_, recs := scanDir(t, env.dir, ScanOptions{})
	var codes []string
	for _, r := range recs {
		if r.Record.Health != nil && r.Record.Health.Kind == wire.HealthRecoveryReopen {
			codes = append(codes, r.Record.Health.DetailCode)
		}
	}
	if len(codes) != 1 || codes[0] != "none" {
		t.Fatalf("clean reopen detail codes %v", codes)
	}

	// ReadDir failing on the first (listing) and second (scan) call.
	for nth, want := range map[int]string{1: "list segments", 2: "scan existing segments"} {
		cfg := env.cfg
		cfg.FS = newFaultFS(failNth("readdir", nth, errBoom))
		if _, err := Open(ctxBG(), cfg); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("readdir #%d: %v", nth, err)
		}
	}
	if active, _ := WriterActive(env.dir); active {
		t.Fatal("failed Open must release the lock")
	}
}

func TestReopenSummary(t *testing.T) {
	rep := StreamReport{Findings: []Finding{
		{Code: UnknownRecordType, Severity: SeverityInfo, Offset: 5},
		{Code: BadCRC, Severity: SeverityWarning, Offset: 10},
		{Code: BadCRC, Severity: SeverityWarning, Offset: 20},
		{Code: SeqGap, Severity: SeverityWarning, Offset: 30},
	}}
	if code, off := reopenSummary(rep); code != "BAD_CRC,SEQ_GAP" || off != 10 {
		t.Fatalf("%q %d", code, off)
	}
	if code, off := reopenSummary(StreamReport{}); code != "none" || off != 0 {
		t.Fatalf("%q %d", code, off)
	}
}

func TestSegmentIndexLimit(t *testing.T) {
	env := newEnv(t)
	writeSegFile(t, env.dir, 999999, []byte("junk"))
	if _, err := Open(ctxBG(), env.cfg); !errors.Is(err, ErrSegmentLimit) {
		t.Fatalf("Open: %v", err)
	}
	if active, err := WriterActive(env.dir); err != nil || active {
		t.Fatalf("lock must be released: %v %v", active, err)
	}
}

// A-15 and A-16: the segment budget at the exact minimum limits.
func TestSegmentBudgetAtMinimum(t *testing.T) {
	// payloadOf probes the marshalled size of an Observation with n payload bytes.
	maxPayload := func(w *Writer) int {
		w.mu.Lock()
		defer w.mu.Unlock()
		base, err := w.encodeLocked(obsBody(3000), DurabilityCritical)
		if err != nil {
			t.Fatal(err)
		}
		return 3000 + 4096 - len(base) // payload size with the same overhead reaching exactly 4096
	}
	newW := func(t *testing.T, ffs *faultFS) (*testEnv, *Writer) {
		env := newEnv(t)
		env.cfg.Limits = Limits{MaxRecordBytes: 4096, MaxSegmentBytes: 20548}
		env.cfg.FS = ffs
		return env, env.open(t)
	}

	t.Run("recovery segment holds three health frames and a maximal record", func(t *testing.T) {
		ffs := newFaultFS(nil)
		env, w := newW(t, ffs)
		n := maxPayload(w)
		ffs.reset()
		ffs.rule = failNth("sync", 1, errBoom)
		if err := w.AppendCritical(ctxBG(), obsBody(10)); err == nil {
			t.Fatal("expected sync failure")
		}
		ffs.rule = nil
		w.dropped.Add(3) // a prior full queue: DROPPED_DIAGNOSTICS is pending too
		if err := w.AppendCritical(ctxBG(), obsBody(n)); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(ctxBG()); err != nil {
			t.Fatal(err)
		}
		rep, recs := scanDir(t, env.dir, ScanOptions{Limits: Limits{MaxRecordBytes: 4096}})
		if !rep.Clean {
			t.Fatalf("findings %v", rep.Findings)
		}
		kinds := healthKinds(recs)
		if len(kinds) != 4 || kinds[1] != wire.HealthSegmentRotated || kinds[2] != wire.HealthWriterError || kinds[3] != wire.HealthDroppedDiagnostics {
			t.Fatalf("health kinds %v", kinds)
		}
		fi, _ := os.Stat(filepath.Join(env.dir, "segment-000002.pbj"))
		if fi.Size() > 20548 {
			t.Fatalf("segment is %d bytes", fi.Size())
		}
	})

	t.Run("rotation with pending dropped health", func(t *testing.T) {
		ffs := newFaultFS(nil)
		env, w := newW(t, ffs)
		n := maxPayload(w)
		for i := 0; i < 15; i++ {
			if err := w.AppendCritical(ctxBG(), obsBody(1000)); err != nil {
				t.Fatal(err)
			}
		}
		w.dropped.Add(1)
		if err := w.AppendCritical(ctxBG(), obsBody(n)); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(ctxBG()); err != nil {
			t.Fatal(err)
		}
		for _, f := range segFiles(t, env.dir) {
			fi, _ := os.Stat(f)
			if fi.Size() > 20548 {
				t.Fatalf("%s is %d bytes", f, fi.Size())
			}
		}
		rep, _ := scanDir(t, env.dir, ScanOptions{Limits: Limits{MaxRecordBytes: 4096}})
		if !rep.Clean || len(rep.Segments) < 2 {
			t.Fatalf("clean=%v segments=%d findings=%v", rep.Clean, len(rep.Segments), rep.Findings)
		}
	})
}

// The sequence varint may grow while health frames are written before a
// record; the re-filled record then no longer fits and is refused without
// poisoning the writer or consuming its sequence.
func TestRefillGrowthExceedsMaxRecord(t *testing.T) {
	env := newEnv(t)
	env.cfg.Limits = Limits{MaxRecordBytes: 4096, MaxSegmentBytes: 1 << 20}
	w := env.open(t)
	for w.Stats().LastSequence < 126 {
		if err := w.AppendCritical(ctxBG(), obsBody(10)); err != nil {
			t.Fatal(err)
		}
	}
	// nextSeq == 127 (one-byte varint); size the body to exactly MaxRecordBytes at seq 127.
	w.mu.Lock()
	probe, err := w.encodeLocked(obsBody(3000), DurabilityCritical)
	w.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	n := 3000 + 4096 - len(probe)
	w.dropped.Add(1) // pending health consumes sequence 127, so the record refills at 128
	err = w.AppendCritical(ctxBG(), obsBody(n))
	if !errors.Is(err, ErrRecordTooLarge) {
		t.Fatalf("expected ErrRecordTooLarge after re-fill, got %v", err)
	}
	if st := w.Stats(); st.Broken || st.LastSequence != 127 {
		t.Fatalf("writer must stay healthy and not consume the record's sequence: %+v", st)
	}
	if err := w.AppendCritical(ctxBG(), obsBody(10)); err != nil {
		t.Fatal(err)
	}
}

func TestRecordErrors(t *testing.T) {
	env := newEnv(t)
	env.cfg.Limits = Limits{MaxRecordBytes: 4096}
	w := env.open(t)
	if err := w.AppendCritical(ctxBG(), &wire.JournalRecord{}); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("no body: %v", err)
	}
	bad := &wire.JournalRecord{Start: &wire.OperationStart{OperationName: "\xff"}}
	if err := w.AppendCritical(ctxBG(), bad); errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("bad UTF-8: %v", err)
	}
	if err := w.AppendCritical(ctxBG(), obsBody(5000)); !errors.Is(err, ErrRecordTooLarge) {
		t.Fatalf("oversize: %v", err)
	}
	if st := w.Stats(); st.Broken || st.LastSequence != 1 {
		t.Fatalf("stats %+v", st)
	}
	// An oversize diagnostic record is accepted into the queue, then dropped and counted.
	if !w.AppendDiagnostic(obsBody(5000)) {
		t.Fatal("queue should accept")
	}
	if err := w.Flush(ctxBG()); err != nil {
		t.Fatal(err)
	}
	if w.Stats().Dropped != 1 || w.Stats().Appended != 0 {
		t.Fatalf("stats %+v", w.Stats())
	}
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
	_, recs := scanDir(t, env.dir, ScanOptions{Limits: Limits{MaxRecordBytes: 4096}})
	if hk := healthKinds(recs); len(hk) != 2 || hk[1] != wire.HealthDroppedDiagnostics {
		t.Fatalf("health kinds %v", hk)
	}
}

// Health records written through the journal are capped deterministically.
func TestHealthCapThroughWriter(t *testing.T) {
	env := newEnv(t)
	w := env.open(t)
	if err := w.AppendCritical(ctxBG(), healthBody(strings.Repeat("d", 4096), 20)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
	_, recs := scanDir(t, env.dir, ScanOptions{})
	h := recs[len(recs)-1].Record.Health
	if h == nil || h.Kind != wire.HealthPathFallback || len(h.Attempts) != 8 || len(h.Detail) > 1024 ||
		!strings.Contains(h.Detail, "[truncated") || !strings.Contains(h.Detail, "[+12 attempts dropped]") {
		t.Fatalf("health %+v", h)
	}
	for _, a := range h.Attempts {
		if len(a.Path) != 256 {
			t.Fatalf("path not capped: %d", len(a.Path))
		}
	}
}

func TestCapHealthUnits(t *testing.T) {
	// Worst-case field sizes force the attempts to be emptied.
	in := &wire.JournalHealth{
		DetailCode: strings.Repeat("c", 500), SelectedSource: strings.Repeat("s", 500), Detail: strings.Repeat("d", 5000),
		Unknown: []byte{1},
	}
	for i := 0; i < 8; i++ {
		in.Attempts = append(in.Attempts, wire.PathAttempt{
			Source: strings.Repeat("a", 500), Path: strings.Repeat("p", 500), ErrorCode: strings.Repeat("e", 500),
		})
	}
	out := capHealth(in)
	b, err := out.Marshal()
	if err != nil || len(b) > MaxHealthBytes {
		t.Fatalf("capped health is %d bytes: %v", len(b), err)
	}
	if len(out.Attempts) != 0 || len(out.DetailCode) != 128 || len(out.SelectedSource) != 128 || out.Unknown != nil {
		t.Fatalf("cap result %+v", out)
	}
	if len(in.Attempts) != 8 || len(in.Detail) != 5000 {
		t.Fatal("input must not be mutated")
	}
	// UTF-8 boundary truncation and invalid UTF-8 replacement.
	multi := strings.Repeat("é", 700) // 1400 bytes
	got := truncDetail(multi, 1024)
	if len(got) > 1024 || !strings.Contains(got, "[truncated") || strings.ToValidUTF8(got, "") != got {
		t.Fatalf("detail %d bytes valid=%v", len(got), strings.ToValidUTF8(got, "") == got)
	}
	if got := truncUTF8("ab\xffcd", 100); got != "ab�cd" {
		t.Fatalf("%q", got)
	}
	if got := truncUTF8(strings.Repeat("é", 10), 5); got != "éé" {
		t.Fatalf("%q", got)
	}
	short := capHealth(&wire.JournalHealth{Detail: "ok", Attempts: []wire.PathAttempt{{Path: "/p"}}})
	if short.Detail != "ok" || len(short.Attempts) != 1 {
		t.Fatalf("%+v", short)
	}
}

// Failures while flushing health into a fresh segment, and while rotating.
func TestHealthWriteFailures(t *testing.T) {
	// Writes on a recovery segment: 1 header, 2 segment-start, 3 WRITER_ERROR, 4 DROPPED, 5 record.
	for nth := 2; nth <= 5; nth++ {
		env := newEnv(t)
		ffs := newFaultFS(nil)
		env.cfg.FS = ffs
		w := env.open(t)
		ffs.reset()
		ffs.rule = failNth("sync", 1, errBoom)
		if err := w.AppendCritical(ctxBG(), obsBody(5)); err == nil {
			t.Fatal("expected sync failure")
		}
		w.dropped.Add(1)
		ffs.reset()
		ffs.rule = failNth("write", nth, errBoom)
		if err := w.AppendCritical(ctxBG(), obsBody(5)); err == nil || !errors.Is(err, errBoom) {
			t.Fatalf("write #%d: %v", nth, err)
		}
		ffs.rule = nil
		if err := w.AppendCritical(ctxBG(), obsBody(5)); err != nil {
			t.Fatalf("write #%d recovery: %v", nth, err)
		}
		if err := w.Close(ctxBG()); err != nil {
			t.Fatal(err)
		}
	}

	// A dropped-health write failing in the non-rotating flush of appendLocked.
	env := newEnv(t)
	ffs := newFaultFS(nil)
	env.cfg.FS = ffs
	w := env.open(t)
	w.dropped.Add(1)
	ffs.reset()
	ffs.rule = failNth("write", 1, errBoom)
	if err := w.AppendCritical(ctxBG(), obsBody(5)); !errors.Is(err, errBoom) {
		t.Fatalf("health flush failure: %v", err)
	}

	// Rotation failing on the fsync of the finished segment.
	env2 := newEnv(t)
	env2.cfg.Limits = Limits{MaxRecordBytes: 4096, MaxSegmentBytes: 24576}
	ffs2 := newFaultFS(nil)
	env2.cfg.FS = ffs2
	w2 := env2.open(t)
	for w2.Stats().SegmentIndex == 1 && w2.size < 22000 {
		if !w2.AppendDiagnostic(obsBody(1000)) {
			t.Fatal("refused")
		}
		if err := w2.Flush(ctxBG()); err != nil {
			t.Fatal(err)
		}
	}
	w2.mu.Lock()
	w2.unsynced = true
	w2.mu.Unlock()
	ffs2.reset()
	ffs2.rule = failNth("sync", 1, errBoom)
	if err := w2.AppendCritical(ctxBG(), obsBody(3000)); !errors.Is(err, errBoom) {
		t.Fatalf("rotation sync failure: %v", err)
	}
}

func TestTickAfterBrokenWriterIsHarmless(t *testing.T) {
	env := newEnv(t)
	ffs := newFaultFS(nil)
	env.cfg.FS = ffs
	w := env.open(t)
	w.AppendDiagnostic(obsBody(5))
	if err := w.Flush(ctxBG()); err != nil {
		t.Fatal(err)
	}
	// No further async append here: the queue is empty, so nothing can reopen a
	// segment between tick() and the assertions.
	w.mu.Lock()
	w.unsynced = true
	w.mu.Unlock()
	ffs.reset()
	ffs.rule = failNth("sync", 1, errBoom)
	w.tick()
	if ffs.count("sync") != 1 || !w.Stats().Broken {
		t.Fatal("failed tick sync must poison the segment")
	}
}

func segmentCount(t *testing.T, dir string) int {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return len(ents)
}

// B2: nothing may open a segment once the writer is shut (single-writer).
func TestNoSegmentOpenedAfterClose(t *testing.T) {
	env := newEnv(t)
	ffs := newFaultFS(nil)
	env.cfg.FS = ffs
	w := env.open(t)
	ffs.reset()
	ffs.rule = failNth("sync", 1, errBoom)
	_ = w.AppendCritical(ctxBG(), obsBody(5)) // poisons: active == nil, WRITER_ERROR pending
	ffs.rule = nil
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
	before := segmentCount(t, env.dir)
	w.dropped.Add(1) // pending health again, as if produced concurrently with Close
	w.mu.Lock()
	err1 := w.flushHealthLocked()
	err2 := w.ensureActiveLocked()
	w.mu.Unlock()
	if !errors.Is(err1, ErrClosed) || !errors.Is(err2, ErrClosed) {
		t.Fatalf("shut writer must refuse: %v %v", err1, err2)
	}
	if err := w.Flush(ctxBG()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Flush: %v", err)
	}
	// Interleaving: Flush passed the closed check, then Close completed.
	w.cmu.Lock()
	w.closed = false
	w.cmu.Unlock()
	if err := w.Flush(ctxBG()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Flush after shut: %v", err)
	}
	w.cmu.Lock()
	w.closed = true // restore: the cleanup Close must stay idempotent
	w.cmu.Unlock()
	if after := segmentCount(t, env.dir); after != before {
		t.Fatalf("segment created after Close: %d -> %d", before, after)
	}
}

// N8: a nil body is an invalid argument, not a panic.
func TestNilBodyIsRejected(t *testing.T) {
	env := newEnv(t)
	w := env.open(t)
	if err := w.AppendCritical(ctxBG(), nil); !errors.Is(err, ErrNilBody) {
		t.Fatalf("AppendCritical(nil): %v", err)
	}
	if w.AppendDiagnostic(nil) || w.Stats().Dropped != 1 {
		t.Fatalf("AppendDiagnostic(nil) must be dropped: %+v", w.Stats())
	}
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
}

func TestCloseErrorsAreJoined(t *testing.T) {
	// Sync failure at Close.
	env := newEnv(t)
	ffs := newFaultFS(nil)
	env.cfg.FS = ffs
	w := env.open(t)
	w.AppendDiagnostic(obsBody(5))
	if err := w.Flush(ctxBG()); err != nil {
		t.Fatal(err)
	}
	w.mu.Lock()
	w.unsynced = true
	w.mu.Unlock()
	ffs.reset()
	ffs.rule = failNth("sync", 1, errBoom)
	if err := w.Close(ctxBG()); !errors.Is(err, errBoom) {
		t.Fatalf("Close: %v", err)
	}

	// Broken writer at Close: the fresh segment for the pending WRITER_ERROR fails to open.
	env2 := newEnv(t)
	ffs2 := newFaultFS(nil)
	env2.cfg.FS = ffs2
	w2 := env2.open(t)
	ffs2.reset()
	ffs2.rule = failNth("sync", 1, errBoom)
	if err := w2.AppendCritical(ctxBG(), obsBody(5)); err == nil {
		t.Fatal("expected failure")
	}
	ffs2.rule = failNth("openfile", 1, errBoom)
	if err := w2.Close(ctxBG()); !errors.Is(err, errBoom) {
		t.Fatalf("Close of broken writer: %v", err)
	}

	// Broken writer at Close with a healthy FS: WRITER_ERROR is still persisted.
	env3 := newEnv(t)
	ffs3 := newFaultFS(nil)
	env3.cfg.FS = ffs3
	w3 := env3.open(t)
	ffs3.reset()
	ffs3.rule = failNth("sync", 1, errBoom)
	_ = w3.AppendCritical(ctxBG(), obsBody(5))
	ffs3.rule = nil
	if err := w3.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
	_, recs := scanDir(t, env3.dir, ScanOptions{})
	if hk := healthKinds(recs); hk[len(hk)-1] != wire.HealthWriterError {
		t.Fatalf("health kinds %v", hk)
	}
}

// A Write that reports fewer bytes than requested without an error is a failure.
func TestSilentShortWriteIsAnError(t *testing.T) {
	env := newEnv(t)
	ffs := newFaultFS(nil)
	env.cfg.FS = ffs
	w := env.open(t)
	ffs.reset()
	ffs.rule = func(op, _ string, n int) *fault {
		if op == "write" && n == 1 {
			return &fault{short: 3, useShort: true, silent: true}
		}
		return nil
	}
	err := w.AppendCritical(ctxBG(), startBody("a.b"))
	if !errors.Is(err, io.ErrShortWrite) || !w.Stats().Broken {
		t.Fatalf("%v %+v", err, w.Stats())
	}
}

// N7: worst-case ids and writer version at the exact minimum MaxSegmentBytes:
// header + three health frames + one maximal record still fit one segment.
func TestMinimumSegmentBudgetHoldsWithMaximalIDs(t *testing.T) {
	env := newEnv(t)
	env.cfg.NodeID = strings.Repeat("n", maxIDBytes)
	env.cfg.RuntimeID = strings.Repeat("r", maxIDBytes)
	env.cfg.StreamID = strings.Repeat("s", maxIDBytes)
	env.cfg.WriterVersion = strings.Repeat("v", maxVersionByte)
	env.cfg.Limits = Limits{MaxRecordBytes: 4096, MaxSegmentBytes: minSegmentBytes(4096)}
	ffs := newFaultFS(nil)
	env.cfg.FS = ffs
	w := env.open(t)
	if w.size > headerFixed+MaxHeaderMeta+4+frameOverhead+MaxHealthBytes {
		t.Fatalf("header plus start health exceeds the budget: %d", w.size)
	}
	// Largest observation whose marshalled record is within MaxRecordBytes.
	n := 4096
	w.mu.Lock()
	for ; n > 0; n-- {
		if _, err := w.encodeLocked(obsBody(n), DurabilityCritical); err == nil {
			break
		}
	}
	w.mu.Unlock()
	// Poison, then queue a drop so the fresh segment owes three health records.
	ffs.reset()
	ffs.rule = failNth("sync", 1, errBoom)
	_ = w.AppendCritical(ctxBG(), obsBody(5))
	ffs.rule = nil
	w.dropped.Add(1)
	if err := w.AppendCritical(ctxBG(), obsBody(n)); err != nil {
		t.Fatalf("maximal record with three pending health frames: %v", err)
	}
	if err := w.Close(ctxBG()); err != nil {
		t.Fatal(err)
	}
	if got := len(segFiles(t, env.dir)); got != 2 {
		t.Fatalf("want the poisoned segment plus one fresh segment, got %d", got)
	}
	for _, p := range segFiles(t, env.dir) {
		fi, err := os.Stat(p)
		if err != nil || fi.Size() > int64(env.cfg.Limits.MaxSegmentBytes) {
			t.Fatalf("%s: size %v exceeds MaxSegmentBytes %d (%v)", p, fi, env.cfg.Limits.MaxSegmentBytes, err)
		}
	}
	rep, _ := scanDir(t, env.dir, ScanOptions{Limits: env.cfg.Limits})
	for _, f := range rep.Findings {
		if f.Severity == SeverityWarning && f.Code != SegmentOrder {
			t.Fatalf("unexpected finding %+v", f)
		}
	}
}
