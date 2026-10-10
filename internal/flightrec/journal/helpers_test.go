package journal

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/flightrec/wire"
	"github.com/olostan/DevCadence/internal/ids"
)

// Helper-process tests (A-9 re-exec, A-14 SIGKILL) run helper mode ONLY when
// the environment variable DEVCADENCE_FLIGHTREC_HELPER is set to a mode name;
// in the normal suite it is unset, only the parent side runs and no helper
// path executes os.Exit. The code paths a helper exercises (Open conflict,
// AppendCritical, lock release) are covered in-process by the other tests.
const (
	helperEnv    = "DEVCADENCE_FLIGHTREC_HELPER"
	helperDirEnv = "DEVCADENCE_FLIGHTREC_HELPER_DIR"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv(helperEnv); mode != "" {
		os.Exit(runHelper(mode, os.Getenv(helperDirEnv)))
	}
	os.Exit(m.Run())
}

func helperConfig(dir string) Config {
	root := filepath.Dir(dir)
	return Config{
		Root: root, Dir: dir, NodeID: "nod_t", RuntimeID: "run_t", StreamID: "main",
		Clock: clock.NewFake(time.Unix(1700000000, 0), time.Microsecond), IDs: ids.NewSequential(),
		Mono: func() int64 { return 1 },
	}
}

func runHelper(mode, dir string) int {
	ctx := context.Background()
	switch mode {
	case "open_expect_conflict":
		_, err := Open(ctx, helperConfig(dir))
		if err == nil {
			return 1
		}
		fmt.Println("conflict:", err)
		return 0
	case "open_ok":
		w, err := Open(ctx, helperConfig(dir))
		if err != nil {
			fmt.Println(err)
			return 1
		}
		if err := w.Close(ctx); err != nil {
			return 1
		}
		return 0
	case "hold_and_exit", "kill_self":
		w, err := Open(ctx, helperConfig(dir))
		if err != nil {
			fmt.Println(err)
			return 1
		}
		if err := w.AppendCritical(ctx, startBody("helper.start")); err != nil {
			return 1
		}
		fmt.Println("ready")
		if mode == "kill_self" {
			_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
			select {} // unreachable: SIGKILL cannot be handled
		}
		return 0 // exits without Close or defers
	}
	return 2
}

func runHelperProcess(t *testing.T, mode, dir string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), helperEnv+"="+mode, helperDirEnv+"="+dir)
	return cmd.CombinedOutput()
}

// ---- configuration and body helpers ----

type testEnv struct {
	root, dir string
	cfg       Config
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	root := filepath.Join(t.TempDir(), "traces")
	dir := filepath.Join(root, "nodes", "nod_t", "run_t", "main")
	var mono atomic.Int64
	return &testEnv{root: root, dir: dir, cfg: Config{
		Root: root, Dir: dir, NodeID: "nod_t", RuntimeID: "run_t", StreamID: "main",
		Clock: clock.NewFake(time.Unix(1700000000, 0), time.Microsecond), IDs: ids.NewSequential(),
		Mono: func() int64 { return mono.Add(1000) }, WriterVersion: "test",
	}}
}

func (e *testEnv) open(t *testing.T) *Writer {
	t.Helper()
	w, err := Open(context.Background(), e.cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = w.Close(context.Background()) })
	return w
}

func startBody(name string) *wire.JournalRecord {
	return &wire.JournalRecord{Start: &wire.OperationStart{TraceID: "trc_1", OperationID: "op_1", OperationName: name}}
}

// obsBody is an Observation whose JSON payload has exactly n bytes.
func obsBody(n int) *wire.JournalRecord {
	return &wire.JournalRecord{Observation: &wire.Observation{
		OperationID: "op_1", Name: "o", JSONPayload: []byte(strings.Repeat("x", n)),
	}}
}

func healthBody(detail string, attempts int) *wire.JournalRecord {
	h := &wire.JournalHealth{Kind: wire.HealthPathFallback, Detail: detail}
	for i := 0; i < attempts; i++ {
		h.Attempts = append(h.Attempts, wire.PathAttempt{Source: "s", Path: strings.Repeat("p", 300), ErrorCode: "e"})
	}
	return &wire.JournalRecord{Health: h}
}

func ctxBG() context.Context { return context.Background() }

// ---- scanning helpers ----

func scanDir(t *testing.T, dir string, opt ScanOptions) (StreamReport, []Scanned) {
	t.Helper()
	var got []Scanned
	rep, err := ScanStream(OSFS(), dir, opt, func(s Scanned) error { got = append(got, s); return nil })
	if err != nil {
		t.Fatalf("ScanStream: %v", err)
	}
	return rep, got
}

func codesOf(fs []Finding) []FindingCode {
	var out []FindingCode
	for _, f := range fs {
		out = append(out, f.Code)
	}
	return out
}

func seqsOf(recs []Scanned) []uint64 {
	var out []uint64
	for _, r := range recs {
		out = append(out, r.Record.StreamSequence)
	}
	return out
}

func healthKinds(recs []Scanned) []wire.HealthKind {
	var out []wire.HealthKind
	for _, r := range recs {
		if r.Record.Health != nil {
			out = append(out, r.Record.Health.Kind)
		}
	}
	return out
}

func bodiesOfType(recs []Scanned, typ wire.RecordType) []Scanned {
	var out []Scanned
	for _, r := range recs {
		if r.Record.BodyType() == typ {
			out = append(out, r)
		}
	}
	return out
}

func segFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if segmentNameRe.MatchString(e.Name()) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// ---- crafted segments ----

func mkRecord(seq uint64) *wire.JournalRecord {
	return &wire.JournalRecord{
		SchemaVersion: 1, EventID: fmt.Sprintf("evt_%d", seq), NodeID: "nod_t", RuntimeID: "run_t", StreamID: "main",
		StreamSequence: seq, Type: wire.RecordTypeObservation, Durability: wire.DurabilityDiagnostic,
		Observation: &wire.Observation{OperationID: "op_1", Name: "n"},
	}
}

func mkFrame(rec *wire.JournalRecord) []byte {
	p, err := rec.Marshal()
	if err != nil {
		panic(err)
	}
	return encodeFrame(p)
}

func mkHeaderBytes(idx uint32, first uint64) []byte {
	meta, err := (&wire.SegmentHeader{
		SchemaVersion: 1, NodeID: "nod_t", RuntimeID: "run_t", StreamID: "main", SegmentIndex: idx, FirstSequence: first,
	}).Marshal()
	if err != nil {
		panic(err)
	}
	return encodeHeader(meta)
}

// segBuilder assembles segment bytes and remembers each frame's offset.
type segBuilder struct {
	buf  []byte
	offs []int
}

func newSeg(idx uint32, first uint64) *segBuilder { return &segBuilder{buf: mkHeaderBytes(idx, first)} }

func (b *segBuilder) frame(f []byte) *segBuilder {
	b.offs = append(b.offs, len(b.buf))
	b.buf = append(b.buf, f...)
	return b
}

func (b *segBuilder) rec(seq uint64) *segBuilder { return b.frame(mkFrame(mkRecord(seq))) }

func (b *segBuilder) raw(p []byte) *segBuilder { b.buf = append(b.buf, p...); return b }

func (b *segBuilder) bytes() []byte { return append([]byte(nil), b.buf...) }

func scanBytes(t *testing.T, data []byte, idx uint32, opt ScanOptions) (SegmentReport, []uint64) {
	t.Helper()
	var seqs []uint64
	rep, err := ScanSegment(bytes.NewReader(data), int64(len(data)), idx, opt, func(s Scanned) error {
		seqs = append(seqs, s.Record.StreamSequence)
		return nil
	})
	if err != nil {
		t.Fatalf("ScanSegment: %v", err)
	}
	return rep, seqs
}

func writeSegFile(t *testing.T, dir string, idx uint32, data []byte) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, segmentName(idx))
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// ---- fault filesystem ----

// fault describes an injected behaviour. gate (if set) runs first and may
// block; then short bytes (when useShort) are written before err is returned.
type fault struct {
	err      error
	short    int
	useShort bool
	full     bool // write the whole buffer, then report err
	silent   bool // with useShort: report success despite the short write
	gate     func()
}

type faultFS struct {
	FS
	mu     sync.Mutex
	log    []string
	counts map[string]int
	rule   func(op, name string, n int) *fault
}

func newFaultFS(rule func(op, name string, n int) *fault) *faultFS {
	return &faultFS{FS: OSFS(), counts: map[string]int{}, rule: rule}
}

func (f *faultFS) hit(op, name string) *fault {
	f.mu.Lock()
	f.counts[op]++
	n := f.counts[op]
	f.log = append(f.log, op+":"+name)
	rule := f.rule
	f.mu.Unlock()
	if rule == nil {
		return nil
	}
	return rule(op, name, n)
}

func (f *faultFS) check(op, name string) error {
	ft := f.hit(op, name)
	if ft == nil {
		return nil
	}
	if ft.gate != nil {
		ft.gate()
	}
	return ft.err
}

func (f *faultFS) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts = map[string]int{}
	f.log = nil
}

func (f *faultFS) count(op string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[op]
}

func (f *faultFS) logCopy() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.log...)
}

func (f *faultFS) MkdirAll(p string, perm os.FileMode) error {
	if err := f.check("mkdirall", p); err != nil {
		return err
	}
	return f.FS.MkdirAll(p, perm)
}

func (f *faultFS) OpenFile(name string, flag int, perm os.FileMode) (File, error) {
	if err := f.check("openfile", filepath.Base(name)); err != nil {
		return nil, err
	}
	fl, err := f.FS.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	return &faultFile{File: fl, ff: f, name: filepath.Base(name)}, nil
}

func (f *faultFS) SyncDir(p string) error {
	if err := f.check("syncdir", p); err != nil {
		return err
	}
	return f.FS.SyncDir(p)
}

func (f *faultFS) ReadDir(p string) ([]os.DirEntry, error) {
	if err := f.check("readdir", p); err != nil {
		return nil, err
	}
	return f.FS.ReadDir(p)
}

func (f *faultFS) Lstat(p string) (os.FileInfo, error) {
	if err := f.check("lstat", p); err != nil {
		return nil, err
	}
	return f.FS.Lstat(p)
}

func (f *faultFS) Open(name string) (ReadFile, error) {
	if err := f.check("open", filepath.Base(name)); err != nil {
		return nil, err
	}
	rf, err := f.FS.Open(name)
	if err != nil {
		return nil, err
	}
	return &faultRead{ReadFile: rf, ff: f, name: filepath.Base(name)}, nil
}

type faultFile struct {
	File
	ff   *faultFS
	name string
}

func (f *faultFile) Write(b []byte) (int, error) {
	ft := f.ff.hit("write", f.name)
	if ft == nil {
		return f.File.Write(b)
	}
	if ft.gate != nil {
		ft.gate()
	}
	if ft.useShort {
		n, _ := f.File.Write(b[:ft.short])
		if ft.silent {
			return n, nil
		}
		return n, ft.err
	}
	if ft.full {
		n, _ := f.File.Write(b)
		return n, ft.err
	}
	if ft.err != nil {
		return 0, ft.err
	}
	return f.File.Write(b)
}

func (f *faultFile) Sync() error {
	if err := f.ff.check("sync", f.name); err != nil {
		return err
	}
	return f.File.Sync()
}

type faultRead struct {
	ReadFile
	ff   *faultFS
	name string
}

func (f *faultRead) Stat() (os.FileInfo, error) {
	if err := f.ff.check("stat", f.name); err != nil {
		return nil, err
	}
	return f.ReadFile.Stat()
}

// failNth returns a rule failing the nth call (1-based) of op with err.
func failNth(op string, nth int, err error) func(string, string, int) *fault {
	return func(o, _ string, n int) *fault {
		if o == op && n == nth {
			return &fault{err: err}
		}
		return nil
	}
}
