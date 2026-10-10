package flightrec

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/flightrec/journal"
	"github.com/olostan/DevCadence/internal/flightrec/wire"
	"github.com/olostan/DevCadence/internal/ids"
)

var t0 = time.Unix(1700000000, 0).UTC()

// memSink is an in-memory Sink for deterministic tests.
type memSink struct {
	mu         sync.Mutex
	recs       []*wire.JournalRecord
	class      []Durability
	critCtx    []context.Context
	ctxErrs    []error // ctx.Err() observed at each critical append
	ctxHasDL   []bool  // whether the ctx had a deadline at each critical append
	critHook   func(*wire.JournalRecord) error
	diagReject bool
	flushes    int
	closes     int
	closeErr   error
}

func (m *memSink) AppendCritical(ctx context.Context, b *wire.JournalRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.critCtx = append(m.critCtx, ctx)
	m.ctxErrs = append(m.ctxErrs, ctx.Err())
	_, hasDL := ctx.Deadline()
	m.ctxHasDL = append(m.ctxHasDL, hasDL)
	if m.critHook != nil {
		if err := m.critHook(b); err != nil {
			return err
		}
	}
	m.recs = append(m.recs, b)
	m.class = append(m.class, Critical)
	return nil
}

func (m *memSink) AppendDiagnostic(b *wire.JournalRecord) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.diagReject {
		return false
	}
	m.recs = append(m.recs, b)
	m.class = append(m.class, Diagnostic)
	return true
}

func (m *memSink) Flush(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flushes++
	return nil
}

func (m *memSink) Close(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closes++
	return m.closeErr
}

func (m *memSink) records() []*wire.JournalRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*wire.JournalRecord(nil), m.recs...)
}

// seqMono is a deterministic monotonic source: 1, 2, 3, ...
func seqMono() func() int64 {
	var n atomic.Int64
	return func() int64 { return n.Add(1) }
}

type testIDs struct {
	*ids.Sequential
	bad map[string]bool
}

func (t testIDs) New(prefix string) string {
	if t.bad[prefix] {
		return "not-an-id"
	}
	return t.Sequential.New(prefix)
}

func newIDs() *ids.Sequential { return ids.NewSequential() }

func nodeID() string { return ids.NewSequential().New("nod") }

func runID() string { return ids.NewSequential().New("run") }

// newRec builds a recorder over sink with the fake clock, sequential ids and a
// sequential monotonic source.
func newRec(t *testing.T, sink Sink) *Recorder {
	t.Helper()
	r, err := New(Config{
		Sink: sink, NodeID: nodeID(), RuntimeID: runID(), StreamID: StreamMain,
		Clock: clock.NewFake(t0, time.Microsecond), IDs: newIDs(), Mono: seqMono(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// journalRec builds a recorder over a real journal writer in a temp dir and
// returns the stream directory. The recorder is closed at cleanup.
func journalRec(t *testing.T) (*Recorder, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "nodes", nodeID(), runID(), StreamMain)
	src := newIDs()
	clk := clock.NewFake(t0, time.Microsecond)
	mono := seqMono()
	w, err := journal.Open(context.Background(), journal.Config{
		Root: root, Dir: dir, NodeID: nodeID(), RuntimeID: runID(), StreamID: StreamMain,
		Clock: clk, IDs: src, Mono: mono,
	})
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(Config{Sink: w, NodeID: nodeID(), RuntimeID: runID(), StreamID: StreamMain, Clock: clk, IDs: src, Mono: mono})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(context.Background()) })
	return r, dir
}

// flushRec drains the async diagnostic queue so the files can be inspected.
func flushRec(t *testing.T, r *Recorder) {
	t.Helper()
	if err := r.sink.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// scanDir returns every record of a stream directory and fails the test on
// any scanner warning.
func scanDir(t *testing.T, dir string) []*wire.JournalRecord {
	t.Helper()
	var out []*wire.JournalRecord
	rep, err := journal.ScanStream(journal.OSFS(), dir, journal.ScanOptions{}, func(s journal.Scanned) error {
		out = append(out, s.Record)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Clean {
		t.Fatalf("journal not clean: %+v", rep.Findings)
	}
	for i, rec := range out {
		if rec.StreamSequence != uint64(i+1) {
			t.Fatalf("sequence gap at %d: %d", i, rec.StreamSequence)
		}
	}
	return out
}

// bodies filters out health records.
func bodies(recs []*wire.JournalRecord, typ wire.RecordType) []*wire.JournalRecord {
	var out []*wire.JournalRecord
	for _, r := range recs {
		if r.BodyType() == typ {
			out = append(out, r)
		}
	}
	return out
}

func onlyRecord(t *testing.T, recs []*wire.JournalRecord, typ wire.RecordType) *wire.JournalRecord {
	t.Helper()
	got := bodies(recs, typ)
	if len(got) != 1 {
		t.Fatalf("want exactly one record of type %d, got %d", typ, len(got))
	}
	return got[0]
}

// ioFile reads a file or fails.
func ioFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// readAllJournalBytes concatenates every segment file of a stream directory.
func readAllJournalBytes(t *testing.T, dir string) []byte {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "segment-*.pbj"))
	if err != nil {
		t.Fatal(err)
	}
	var all []byte
	for _, f := range files {
		all = append(all, ioFile(t, f)...)
	}
	return all
}

func newClockForTest() clock.Clock { return clock.NewFake(t0, time.Microsecond) }
