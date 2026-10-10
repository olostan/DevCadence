package flightrec

import (
	"context"
	"errors"
	"io"
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
	// sidecarCreate is an in-package test seam for sidecar file creation.
	sidecarCreate sidecarCreate
}

// maxNodeIDFileBytes bounds the node-id read: a valid file is "nod_<ulid>\n".
const maxNodeIDFileBytes = 256

// nodeIDFile reads or creates <root>/node-id (O_EXCL, 0600, "id\n").
// ephemeral is true when the id could not be read or persisted (a corrupt,
// oversized, non-regular (symlink, directory, FIFO) or unreadable file is never
// overwritten or followed; losing a creation race to another process also
// yields an ephemeral id). The file is never opened unless Lstat says it is a
// regular file, and at most maxNodeIDFileBytes are read, so a FIFO or a huge
// file can neither block nor exhaust the process.
func nodeIDFile(root string, src ids.Source) (id string, ephemeral bool) {
	path := filepath.Join(root, "node-id")
	fi, err := os.Lstat(path)
	if err == nil {
		if fi.Mode().IsRegular() {
			if f, oerr := os.Open(path); oerr == nil {
				b, _ := io.ReadAll(io.LimitReader(f, maxNodeIDFileBytes))
				_ = f.Close()
				if id = strings.TrimSpace(string(b)); strings.HasPrefix(id, "nod_") && ids.Valid(id) {
					return id, false
				}
			}
		}
		return src.New("nod"), true
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
	var (
		w          *journal.Writer
		res        Resolution
		nodeID     string
		nodeSource string
	)
	// degrade builds the degraded no-op recorder and writes the last-resort
	// bootstrap-failure sidecar (it never panics: writeSidecar recovers).
	degrade := func(reason, detail string, journalFailed bool) (*Recorder, Status) {
		root := ""
		if journalFailed {
			root = res.Root
		}
		body := buildSidecar(DefaultSanitizer(), in.Clock.Now(), reason, in.WriterVersion, nodeID, nodeSource, res.Attempts, Stats{})
		create := in.sidecarCreate
		if create == nil {
			create = createSidecarFile
		}
		path, code := writeSidecar(in.Resolve, create, root, body)
		r := newNoop(Status{Reason: reason, Attempts: res.Attempts, SidecarPath: path, SidecarError: code}, detail)
		return r, r.Status()
	}
	defer func() {
		if p := recover(); p != nil { // the panic value is never recorded
			if w != nil {
				_ = w.Close(context.WithoutCancel(ctx))
			}
			rec, st = degrade(ReasonBootstrapPanic, "", res.Root != "")
		}
	}()
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
		return degrade(ReasonNoUsablePath, err.Error(), false)
	}
	nodeID, ephemeral := nodeIDFile(res.Root, in.IDs)
	nodeSource = nodeIDSourceFile
	if ephemeral {
		nodeSource = nodeIDSourceEphemeral
	}
	runID := in.IDs.New("run")
	if !ids.Valid(nodeID) || !ids.Valid(runID) {
		return degrade(ReasonInvalidID, "id source returned an invalid id", true)
	}
	origin := in.Now()
	mono := func() int64 { return int64(in.Now().Sub(origin)) }
	w, err = journal.Open(ctx, journal.Config{
		Root: res.Root, Dir: filepath.Join(res.Root, "nodes", nodeID, runID, StreamMain),
		NodeID: nodeID, RuntimeID: runID, StreamID: StreamMain, Limits: in.Limits,
		FlushEvery: diagnosticFlushEvery, Clock: in.Clock, IDs: in.IDs, Mono: mono, WriterVersion: in.WriterVersion,
	})
	if err != nil {
		return degrade(ReasonJournalOpenFailed, err.Error(), true)
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
		return degrade(ReasonStreamStartFailure, err.Error(), true)
	}
	if res.Fallback {
		_ = rec.recordHealth(ctx, Diagnostic, wire.HealthPathFallback, "path_fallback", "", res.Source, res.Attempts)
	}
	if ephemeral {
		_ = rec.recordHealth(ctx, Diagnostic, wire.HealthNodeIDEphemeral, "node_id_ephemeral", "", res.Source, nil)
	}
	return rec, rec.Status()
}
