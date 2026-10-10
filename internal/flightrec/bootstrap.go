package flightrec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/flightrec/journal"
	"github.com/olostan/DevCadence/internal/flightrec/wire"
	"github.com/olostan/DevCadence/internal/ids"
)

// diagnosticFlushEvery is the diagnostic fsync cadence of a bootstrapped
// journal.
const diagnosticFlushEvery = time.Second

// BootstrapInput is the earliest-startup input of Bootstrap. Its only sources
// are the flag value, process environment path variables, OS home/temp and the
// clock: no DevCadence config, hardware discovery, SQLite, processes or
// network. Nil Clock, IDs and Now take the production defaults.
type BootstrapInput struct {
	Resolve       ResolveInput
	Limits        journal.Limits
	Clock         clock.Clock
	IDs           ids.Source
	Now           func() time.Time // origin and source of the monotonic offset
	WriterVersion string

	// wrapSink is an in-package test seam wrapping the opened writer.
	wrapSink func(Sink) Sink
}

// nodeIDFile reads or creates <root>/node-id (O_EXCL, 0600, "id\n").
// ephemeral is true when the id could not be read or persisted (a corrupt or
// unreadable file is never overwritten; losing a creation race to another
// process also yields an ephemeral id).
func nodeIDFile(root string, src ids.Source) (id string, ephemeral bool) {
	path := filepath.Join(root, "node-id")
	b, err := os.ReadFile(path)
	if err == nil {
		if id = strings.TrimSpace(string(b)); strings.HasPrefix(id, "nod_") && ids.Valid(id) {
			return id, false
		}
	}
	id = src.New("nod")
	if !errors.Is(err, os.ErrNotExist) || !ids.Valid(id) {
		return id, true
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return id, true
	}
	_, werr := f.WriteString(id + "\n")
	serr := f.Sync()
	cerr := f.Close()
	return id, errors.Join(werr, serr, cerr) != nil
}

// Bootstrap builds the process recorder. It NEVER returns an error and NEVER
// panics: any failure (no usable directory, invalid ids, journal open failure,
// a panic) yields a degraded no-op recorder (Mode == ModeDegradedNoop, Reason
// a stable code, Attempts recorded, a DEGRADED_NOOP health ring entry), so a
// degraded recorder is itself visible. In journal mode the first record is a
// critical JournalHealth STREAM_STARTED carrying selected_source and every
// failed path attempt, followed by PATH_FALLBACK (when a set candidate failed)
// and NODE_ID_EPHEMERAL (when the node id could not be persisted).
//
// The run id (runtime_id) is "run_<ULID>": one recorder lifetime. The location
// is fixed for the process lifetime; later configuration affects only the next
// run.
func Bootstrap(ctx context.Context, in BootstrapInput) (rec *Recorder, st Status) {
	var w *journal.Writer
	defer func() {
		if p := recover(); p != nil { // the panic value is never recorded
			if w != nil {
				_ = w.Close(context.WithoutCancel(ctx))
			}
			rec = NewNoop(Status{Reason: ReasonBootstrapPanic})
			st = rec.Status()
		}
	}()
	degrade := func(reason, detail string, attempts []Attempt) (*Recorder, Status) {
		r := newNoop(Status{Reason: reason, Attempts: attempts}, detail)
		return r, r.Status()
	}
	if in.Clock == nil {
		in.Clock = clock.System()
	}
	if in.IDs == nil {
		in.IDs = ids.NewULIDSource()
	}
	if in.Now == nil {
		in.Now = time.Now
	}

	res, err := Resolve(in.Resolve)
	if err != nil {
		return degrade(ReasonNoUsablePath, err.Error(), res.Attempts)
	}
	nodeID, ephemeral := nodeIDFile(res.Root, in.IDs)
	runID := in.IDs.New("run")
	if !ids.Valid(nodeID) || !ids.Valid(runID) {
		return degrade(ReasonInvalidID, "id source returned an invalid id", res.Attempts)
	}
	origin := in.Now()
	mono := func() int64 { return int64(in.Now().Sub(origin)) }
	w, err = journal.Open(ctx, journal.Config{
		Root: res.Root, Dir: filepath.Join(res.Root, "nodes", nodeID, runID, StreamMain),
		NodeID: nodeID, RuntimeID: runID, StreamID: StreamMain, Limits: in.Limits,
		FlushEvery: diagnosticFlushEvery, Clock: in.Clock, IDs: in.IDs, Mono: mono, WriterVersion: in.WriterVersion,
	})
	if err != nil {
		return degrade(ReasonJournalOpenFailed, err.Error(), res.Attempts)
	}
	var sink Sink = w
	if in.wrapSink != nil {
		sink = in.wrapSink(w)
	}
	rec = build(Config{
		Sink: sink, NodeID: nodeID, RuntimeID: runID, StreamID: StreamMain, Clock: in.Clock, IDs: in.IDs, Mono: mono,
		Status: Status{Root: res.Root, Source: res.Source, Fallback: res.Fallback, Attempts: res.Attempts, NodeIDEphemeral: ephemeral},
	})
	if err := rec.recordHealth(ctx, Critical, wire.HealthStreamStarted, "stream_started", "", res.Source, res.Attempts); err != nil {
		_ = w.Close(context.WithoutCancel(ctx))
		return degrade(ReasonStreamStartFailure, err.Error(), res.Attempts)
	}
	if res.Fallback {
		_ = rec.recordHealth(ctx, Diagnostic, wire.HealthPathFallback, "path_fallback", "", res.Source, res.Attempts)
	}
	if ephemeral {
		_ = rec.recordHealth(ctx, Diagnostic, wire.HealthNodeIDEphemeral, "node_id_ephemeral", "", res.Source, nil)
	}
	return rec, rec.Status()
}
