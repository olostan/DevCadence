package flightrec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/flightrec/journal"
	"github.com/olostan/DevCadence/internal/flightrec/wire"
	"github.com/olostan/DevCadence/internal/ids"
)

// scriptedFS is the real filesystem except that MkdirAll can be denied.
type scriptedFS struct {
	PathFS
	deny func(string) bool
}

func (s scriptedFS) MkdirAll(p string, perm os.FileMode) error {
	if s.deny(p) {
		return errors.New("read-only file system")
	}
	return s.PathFS.MkdirAll(p, perm)
}

type panicFS struct{ PathFS }

func (panicFS) MkdirAll(string, os.FileMode) error { panic("fs exploded") }

type failingSink struct{ Sink }

func (failingSink) AppendCritical(context.Context, *wire.JournalRecord) error { return errDisk }

type panicSink struct{ Sink }

func (panicSink) AppendCritical(context.Context, *wire.JournalRecord) error { panic("sink exploded") }

func noEnv(string) string { return "" }

func noHome() (string, error) { return "", errors.New("HOME is not set") }

// bootInput returns an input whose only usable location is an explicit
// directory below t.TempDir (no host state is consulted).
func bootInput(t *testing.T) (BootstrapInput, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "traces")
	return BootstrapInput{
		Resolve: ResolveInput{ExplicitDir: root, Getenv: noEnv, UserHomeDir: noHome, TempDir: t.TempDir, UID: os.Getuid},
		Clock:   newClockForTest(), IDs: newIDs(), Now: time.Now,
	}, root
}

func streamDir(t *testing.T, root string) string {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join(root, "nodes", "*", "*", StreamMain))
	if err != nil || len(dirs) != 1 {
		t.Fatalf("stream dirs %v %v", dirs, err)
	}
	return dirs[0]
}

func healthOf(recs []*wire.JournalRecord, kind wire.HealthKind) []*wire.JournalHealth {
	var out []*wire.JournalHealth
	for _, r := range recs {
		if r.Health != nil && r.Health.Kind == kind {
			out = append(out, r.Health)
		}
	}
	return out
}

func closeRec(t *testing.T, r *Recorder) {
	t.Helper()
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// B-13: no config, no HOME, empty env, read-only home: the journal lands at
// the temp fallback and records every attempted path.
func TestBootstrapFallsBackToTemp(t *testing.T) {
	tmp := t.TempDir()
	in := BootstrapInput{
		Resolve: ResolveInput{
			Getenv: noEnv, UserHomeDir: func() (string, error) { return "/ro-home", nil }, TempDir: func() string { return tmp }, UID: os.Getuid,
			FS: scriptedFS{PathFS: OSPathFS(), deny: func(p string) bool { return strings.HasPrefix(p, "/ro-home") }},
		},
		Clock: newClockForTest(), IDs: newIDs(), Now: time.Now, WriterVersion: "test-1",
	}
	rec, st := Bootstrap(context.Background(), in)
	root := filepath.Join(tmp, "devcadence-trace-"+strconv.Itoa(os.Getuid()))
	if st.Mode != ModeJournal || st.Source != SourceTemp || !st.Fallback || st.Root != root || st.NodeIDEphemeral || st.Reason != "" {
		t.Fatalf("status %+v", st)
	}
	wantAttempts := []Attempt{
		{SourceExplicitFlag, "", ErrCodeUnset}, {SourceEnvTraceDir, "", ErrCodeUnset}, {SourceEnvHome, "", ErrCodeUnset},
		{SourceXDGState, "", ErrCodeUnset}, {SourceUserHome, "/ro-home/.devcadence/traces", ErrCodeMkdirFailed},
	}
	if len(st.Attempts) != len(wantAttempts) {
		t.Fatalf("attempts %+v", st.Attempts)
	}
	for i, a := range wantAttempts {
		if st.Attempts[i] != a {
			t.Fatalf("attempt %d: %+v", i, st.Attempts[i])
		}
	}
	if !ids.Valid(st.NodeID) || !ids.Valid(st.RuntimeID) || !strings.HasPrefix(st.NodeID, "nod_") || !strings.HasPrefix(st.RuntimeID, "run_") {
		t.Fatalf("ids %+v", st)
	}
	node := ioFile(t, filepath.Join(root, "node-id"))
	if string(node) != st.NodeID+"\n" {
		t.Fatalf("node-id %q", node)
	}
	if fi, err := os.Stat(filepath.Join(root, "node-id")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("node-id mode %v %v", fi, err)
	}
	closeRec(t, rec)
	recs := scanDir(t, streamDir(t, root))
	started := healthOf(recs, wire.HealthStreamStarted)
	var ours *wire.JournalHealth
	for _, h := range started {
		if h.SelectedSource != "" {
			ours = h
		}
	}
	if ours == nil || ours.SelectedSource != SourceTemp || ours.DetailCode != "stream_started" || len(ours.Attempts) != 5 {
		t.Fatalf("STREAM_STARTED %+v", started)
	}
	if got, want := ours.Attempts[4].Path, rec.san.Scalar(ScalarLocator, "/ro-home/.devcadence/traces"); got != want || ours.Attempts[4].ErrorCode != ErrCodeMkdirFailed || ours.Attempts[4].Source != SourceUserHome {
		t.Fatalf("attempt %+v", ours.Attempts[4])
	}
	fb := healthOf(recs, wire.HealthPathFallback)
	if len(fb) != 1 || len(fb[0].Attempts) != 5 || fb[0].SelectedSource != SourceTemp {
		t.Fatalf("PATH_FALLBACK %+v", fb)
	}
	for _, r := range recs {
		if r.Health != nil && r.Health.Kind == wire.HealthStreamStarted && r.Health.SelectedSource != "" && r.Durability != wire.DurabilityCritical {
			t.Fatalf("STREAM_STARTED must be critical: %v", r.Durability)
		}
	}
	if len(healthOf(recs, wire.HealthNodeIDEphemeral)) != 0 {
		t.Fatal("node id was persisted")
	}
	// The segment header carries the writer version and the mono origin.
	if got := string(readAllJournalBytes(t, streamDir(t, root))); !strings.Contains(got, "test-1") {
		t.Fatal("writer version missing from the segment header")
	}
}

// B-13: the node id is persisted and reused across two Bootstraps.
func TestBootstrapReusesNodeID(t *testing.T) {
	in, root := bootInput(t)
	rec1, st1 := Bootstrap(context.Background(), in)
	closeRec(t, rec1)
	rec2, st2 := Bootstrap(context.Background(), in)
	defer closeRec(t, rec2)
	if st1.Mode != ModeJournal || st2.Mode != ModeJournal || st1.NodeID != st2.NodeID || st1.RuntimeID == st2.RuntimeID || st1.NodeIDEphemeral || st2.NodeIDEphemeral {
		t.Fatalf("%+v %+v", st1, st2)
	}
	if st1.Fallback || st1.Source != SourceExplicitFlag || st1.Root != root {
		t.Fatalf("%+v", st1)
	}
	if dirs, _ := filepath.Glob(filepath.Join(root, "nodes", st1.NodeID, "*", StreamMain)); len(dirs) != 2 {
		t.Fatalf("run dirs %v", dirs)
	}
}

// B-13: an unwritable or unusable node-id file yields an ephemeral id and a
// NODE_ID_EPHEMERAL health record; existing content is never overwritten.
func TestBootstrapEphemeralNodeID(t *testing.T) {
	setups := map[string]func(t *testing.T, path string){
		"dangling symlink (create fails)": func(t *testing.T, path string) {
			if err := os.Symlink(filepath.Join(filepath.Dir(path), "missing", "target"), path); err != nil {
				t.Fatal(err)
			}
		},
		"directory (read fails)": func(t *testing.T, path string) {
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		},
		"corrupt content": func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("garbage\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"wrong prefix": func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte(ids.NewSequential().New("run")+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, setup := range setups {
		t.Run(name, func(t *testing.T) {
			in, root := bootInput(t)
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "node-id")
			setup(t, path)
			before, _ := os.Lstat(path)
			rec, st := Bootstrap(context.Background(), in)
			if st.Mode != ModeJournal || !st.NodeIDEphemeral || !ids.Valid(st.NodeID) {
				t.Fatalf("%+v", st)
			}
			var ring int
			for _, ev := range rec.Health() {
				if ev.Kind == wire.HealthNodeIDEphemeral {
					ring++
				}
			}
			closeRec(t, rec)
			recs := scanDir(t, streamDir(t, root))
			if len(healthOf(recs, wire.HealthNodeIDEphemeral)) != 1 || ring != 1 {
				t.Fatalf("NODE_ID_EPHEMERAL records %d ring %d", len(healthOf(recs, wire.HealthNodeIDEphemeral)), ring)
			}
			after, _ := os.Lstat(path)
			if !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatal("the node-id entry must not be replaced")
			}
		})
	}
}

// B-14: all candidates fail -> degraded no-op with a visible status.
func TestBootstrapAllCandidatesFail(t *testing.T) {
	in := BootstrapInput{
		Resolve: ResolveInput{
			ExplicitDir: "/x", Getenv: noEnv, UserHomeDir: noHome, UID: func() int { return -1 },
			FS: scriptedFS{PathFS: OSPathFS(), deny: func(string) bool { return true }},
		},
	}
	rec, st := Bootstrap(context.Background(), in)
	if st.Mode != ModeDegradedNoop || st.Reason != ReasonNoUsablePath || len(st.Attempts) != 6 || st.Attempts[0].ErrCode != ErrCodeMkdirFailed || st.Attempts[5].ErrCode != ErrCodeUIDUnknown {
		t.Fatalf("%+v", st)
	}
	if rec.Status().Mode != ModeDegradedNoop {
		t.Fatal("status must persist on the recorder")
	}
	h := rec.Health()
	if len(h) != 1 || h[0].Kind != wire.HealthDegradedNoop || h[0].Code != ReasonNoUsablePath || len(h[0].Attempts) != 6 {
		t.Fatalf("health %+v", h)
	}
	checkNoopInert(t, rec)
}

func checkNoopInert(t *testing.T, rec *Recorder) {
	t.Helper()
	ctx, op, err := rec.Start(context.Background(), StartSpec{Name: "noop.op"})
	if err != nil || op == nil {
		t.Fatalf("%v", err)
	}
	if sc, ok := FromContext(ctx); !ok || sc.OperationID != op.ID() || sc.StreamID != StreamMain {
		t.Fatalf("span %+v", sc)
	}
	if err := rec.Observe(ctx, ObservationSpec{Name: "noop.obs"}); err != nil {
		t.Fatal(err)
	}
	if err := op.Observe(ObservationSpec{Name: "noop.obs"}); err != nil {
		t.Fatal(err)
	}
	if err := op.End(OutcomeCompleted, nil); err != nil {
		t.Fatal(err)
	}
	if st := rec.Status().Stats; st.Dropped != 4 || st.EndDropped != 1 {
		t.Fatalf("%+v", st)
	}
	// Critical appends cannot be honored by a degraded recorder.
	cctx := context.Background()
	nctx, nop, err := rec.Start(cctx, StartSpec{Name: "noop.crit", Durability: Critical})
	if err == nil || errs.CategoryOf(err) != errs.CategoryInternal || !strings.Contains(err.Error(), "recorder degraded") || nop != nil || nctx != cctx {
		t.Fatalf("%v", err)
	}
	if err := rec.Observe(ctx, ObservationSpec{Name: "noop.crit", Durability: Critical}); errs.CategoryOf(err) != errs.CategoryInternal {
		t.Fatalf("%v", err)
	}
	if err := rec.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNewNoopForcesDegradedMode(t *testing.T) {
	rec := NewNoop(Status{Mode: ModeJournal, Reason: "custom", NodeID: id("nod", 4), RuntimeID: id("run", 4), Attempts: []Attempt{{Source: "s"}}})
	st := rec.Status()
	if st.Mode != ModeDegradedNoop || st.Reason != "custom" || st.NodeID != id("nod", 4) || len(st.Attempts) != 1 {
		t.Fatalf("%+v", st)
	}
	ctx, _, err := rec.Start(context.Background(), StartSpec{Name: "a.b"})
	if sc, _ := FromContext(ctx); err != nil || sc.NodeID != id("nod", 4) || sc.RuntimeID != id("run", 4) {
		t.Fatalf("%+v %v", sc, err)
	}
	st.Attempts[0].Source = "mutated"
	if rec.Status().Attempts[0].Source != "s" {
		t.Fatal("Status must return a copy")
	}
}

func TestBootstrapDegradationReasons(t *testing.T) {
	t.Run("invalid ids", func(t *testing.T) {
		in, _ := bootInput(t)
		in.IDs = testIDs{Sequential: newIDs(), bad: map[string]bool{"run": true}}
		rec, st := Bootstrap(context.Background(), in)
		if st.Mode != ModeDegradedNoop || st.Reason != ReasonInvalidID || rec.Health()[0].Code != ReasonInvalidID {
			t.Fatalf("%+v", st)
		}
	})
	t.Run("journal open failure", func(t *testing.T) {
		in, _ := bootInput(t)
		in.Limits = journal.Limits{MaxRecordBytes: 100}
		rec, st := Bootstrap(context.Background(), in)
		if st.Mode != ModeDegradedNoop || st.Reason != ReasonJournalOpenFailed || !strings.Contains(rec.Health()[0].Detail, "MaxRecordBytes") {
			t.Fatalf("%+v %+v", st, rec.Health())
		}
	})
	t.Run("stream started append fails", func(t *testing.T) {
		in, root := bootInput(t)
		in.wrapSink = func(s Sink) Sink { return failingSink{s} }
		_, st := Bootstrap(context.Background(), in)
		if st.Mode != ModeDegradedNoop || st.Reason != ReasonStreamStartFailure {
			t.Fatalf("%+v", st)
		}
		active, err := journal.WriterActive(streamDir(t, root))
		if err != nil || active {
			t.Fatalf("writer still open: %v %v", active, err)
		}
	})
}

// B-18: nil Clock/IDs/Now take the defaults; panics never escape.
func TestBootstrapDefaultsAndMono(t *testing.T) {
	in, root := bootInput(t)
	in.Clock, in.IDs, in.Now = nil, nil, nil
	rec, st := Bootstrap(context.Background(), in)
	if st.Mode != ModeJournal || !ids.Valid(st.RuntimeID) || !strings.HasPrefix(st.RuntimeID, "run_") || st.Root != root {
		t.Fatalf("%+v", st)
	}
	ctx, op, err := rec.Start(context.Background(), StartSpec{Name: "real.clock"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Observe(ctx, ObservationSpec{Name: "real.note"}); err != nil {
		t.Fatal(err)
	}
	if err := op.End(OutcomeCompleted, nil); err != nil {
		t.Fatal(err)
	}
	closeRec(t, rec)
	if recs := scanDir(t, streamDir(t, root)); len(bodies(recs, wire.RecordTypeOperationEnd)) != 1 {
		t.Fatalf("records %d", len(recs))
	}

	// Mono is monotonic and non-decreasing from the origin.
	in2, _ := bootInput(t)
	var n int64
	base := time.Unix(1700000000, 0)
	in2.Now = func() time.Time {
		n++
		return base.Add(time.Duration(n) * time.Millisecond)
	}
	rec2, st2 := Bootstrap(context.Background(), in2)
	if st2.Mode != ModeJournal {
		t.Fatalf("%+v", st2)
	}
	defer closeRec(t, rec2)
	prev := int64(0)
	for i := 0; i < 5; i++ {
		m := rec2.mono()
		if m <= 0 || m < prev {
			t.Fatalf("mono %d after %d", m, prev)
		}
		prev = m
	}
}

func TestBootstrapNeverPanics(t *testing.T) {
	t.Run("panicking PathFS", func(t *testing.T) {
		in, _ := bootInput(t)
		hermeticEnv(t) // the last-resort sidecar uses the real HOME and TMPDIR
		in.Resolve.FS = panicFS{OSPathFS()}
		rec, st := Bootstrap(context.Background(), in)
		if st.Mode != ModeDegradedNoop || st.Reason != ReasonBootstrapPanic || rec.Health()[0].Code != ReasonBootstrapPanic {
			t.Fatalf("%+v", st)
		}
		checkNoopInert(t, rec)
	})
	t.Run("panic after the journal is open closes the writer", func(t *testing.T) {
		in, root := bootInput(t)
		in.wrapSink = func(s Sink) Sink { return panicSink{s} }
		_, st := Bootstrap(context.Background(), in)
		if st.Mode != ModeDegradedNoop || st.Reason != ReasonBootstrapPanic {
			t.Fatalf("%+v", st)
		}
		active, err := journal.WriterActive(streamDir(t, root))
		if err != nil || active {
			t.Fatalf("writer still open: %v %v", active, err)
		}
	})
}
