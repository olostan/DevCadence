package flightrec

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/flightrec/wire"
	"github.com/olostan/DevCadence/internal/ids"
)

// Sink is the consumer-owned append interface of the recorder. *journal.Writer
// satisfies it directly (the writer fills the record envelope).
type Sink interface {
	AppendCritical(ctx context.Context, body *wire.JournalRecord) error
	AppendDiagnostic(body *wire.JournalRecord) bool
	Flush(ctx context.Context) error
	Close(ctx context.Context) error
}

// Config configures New.
type Config struct {
	Sink                        Sink
	NodeID, RuntimeID, StreamID string
	Clock                       clock.Clock
	IDs                         ids.Source
	Mono                        func() int64
	Sanitizer                   *Sanitizer // nil = DefaultSanitizer
	Status                      Status
}

// Recorder records operations into a Sink. A Recorder without a Sink is the
// degraded no-op recorder. It is safe for concurrent use.
type Recorder struct {
	sink     Sink
	streamID string
	clock    clock.Clock
	ids      ids.Source
	mono     func() int64
	san      *Sanitizer
	status   Status
	stats    counters
	dropNote atomic.Bool

	hmu   sync.Mutex
	ring  []HealthEvent
	hnext int
	hseq  uint64
}

// New builds a journal-mode recorder. It is strict: Sink, Clock, IDs and Mono
// are required and NodeID/RuntimeID must be valid ids, else
// CategoryInvalidArgument.
func New(cfg Config) (*Recorder, error) {
	switch {
	case cfg.Sink == nil || cfg.Clock == nil || cfg.IDs == nil || cfg.Mono == nil:
		return nil, invalidArg("Sink, Clock, IDs and Mono are required")
	case !ids.Valid(cfg.NodeID) || !ids.Valid(cfg.RuntimeID) || !streamIDRE.MatchString(cfg.StreamID):
		return nil, invalidArg("NodeID, RuntimeID and StreamID must be valid")
	}
	return build(cfg), nil
}

// build assembles a recorder from an already validated Config.
func build(cfg Config) *Recorder {
	r := &Recorder{
		sink: cfg.Sink, streamID: cfg.StreamID, clock: cfg.Clock, ids: cfg.IDs, mono: cfg.Mono,
		san: cfg.Sanitizer, status: cfg.Status,
	}
	if r.san == nil {
		r.san = DefaultSanitizer()
	}
	r.status.Mode = ModeJournal
	r.status.NodeID, r.status.RuntimeID = cfg.NodeID, cfg.RuntimeID
	return r
}

// NewNoop returns the degraded recorder: it has no Sink, Start/Observe/End are
// inert (Critical appends fail with CategoryInternal "recorder degraded"),
// and Status/Health remain available. st.Mode is forced to ModeDegradedNoop
// and a DEGRADED_NOOP health event carrying st.Reason is recorded.
func NewNoop(st Status) *Recorder { return newNoop(st, "") }

func newNoop(st Status, detail string) *Recorder {
	r := build(Config{StreamID: StreamMain, Clock: clock.System(), IDs: ids.NewULIDSource(), Mono: func() int64 { return 0 }, Status: st})
	r.status.Mode = ModeDegradedNoop
	r.status.NodeID, r.status.RuntimeID = st.NodeID, st.RuntimeID
	r.noteHealth(wire.HealthDegradedNoop, st.Reason, detail, st.Attempts)
	return r
}

func (r *Recorder) stamp() *wire.Stamp {
	return &wire.Stamp{WallUnixNanos: r.clock.Now().UnixNano(), MonoNanos: r.mono()}
}

func (r *Recorder) newID(prefix string) (string, error) {
	id := r.ids.New(prefix)
	if !ids.Valid(id) {
		return "", errs.New(errs.CategoryInternal, "flightrec: id source returned an invalid %s id", prefix)
	}
	return id, nil
}

// noteDrop counts a dropped diagnostic record and records the first one in
// the health ring.
func (r *Recorder) noteDrop() {
	r.stats.dropped.Add(1)
	if r.dropNote.CompareAndSwap(false, true) {
		r.noteHealth(wire.HealthDroppedDiagnostics, "diagnostic_dropped", "first diagnostic record dropped; see Stats.Dropped", nil)
	}
}

var errDegraded = errs.New(errs.CategoryInternal, "flightrec: recorder degraded")

// appendRec appends rec in class d. Critical failures return an error (a
// degraded recorder always fails them); a dropped diagnostic returns
// accepted=false and a nil error.
func (r *Recorder) appendRec(ctx context.Context, d Durability, rec *wire.JournalRecord) (accepted bool, err error) {
	if d == Critical {
		if r.sink == nil {
			return false, errDegraded
		}
		if err := r.sink.AppendCritical(ctx, rec); err != nil {
			return false, errs.Wrap(errs.CategoryInternal, err, "flightrec: critical append")
		}
		return true, nil
	}
	if r.sink != nil && r.sink.AppendDiagnostic(rec) {
		return true, nil
	}
	r.noteDrop()
	return false, nil
}

// Start begins an operation. The returned context carries only the new
// SpanContext (the derived context keeps ctx's cancellation and deadline).
//
// Errors: an invalid spec is CategoryInvalidArgument (also for Diagnostic). A
// Critical Start with an already done ctx fails before any write; a Critical
// append failure returns CategoryInternal. In both cases the returned context
// is ctx unchanged and the Op is nil: the caller MUST refuse the protected
// effect. A Diagnostic Start never fails because of I/O.
func (r *Recorder) Start(ctx context.Context, spec StartSpec) (context.Context, *Op, error) {
	if err := validateCommon(spec.Name, spec.Links, spec.Artifacts, spec.Durability, spec.EndDurability); err != nil {
		return ctx, nil, err
	}
	meta, t, err := r.san.checkedJSON(spec.Metadata)
	if err != nil {
		return ctx, nil, err
	}
	sc, _ := FromContext(ctx)
	opID, err := r.newID("op")
	if err != nil {
		return ctx, nil, err
	}
	traceID := sc.TraceID
	if traceID == "" {
		if traceID, err = r.newID("trc"); err != nil {
			return ctx, nil, err
		}
	}
	start := &wire.OperationStart{
		TraceID: traceID, OperationID: opID, ParentOperationID: sc.OperationID,
		Links: r.san.links(spec.Links), OperationName: spec.Name, At: r.stamp(), JSONMetadata: meta,
		ActorID: r.san.scalar(&t, ScalarID, spec.ActorID), TaskID: r.san.scalar(&t, ScalarID, spec.TaskID),
		AttemptID:        r.san.scalar(&t, ScalarID, spec.AttemptID),
		CanonicalEventID: r.san.scalar(&t, ScalarID, spec.CanonicalEventID),
		Artifacts:        r.san.artifacts(&t, spec.Artifacts),
	}
	start.Sanitization = t.ptr()
	if spec.Durability == Critical {
		if cerr := ctx.Err(); cerr != nil {
			return ctx, nil, errs.Wrap(errs.CategoryInvalidArgument, cerr, "flightrec: start %q: context done", spec.Name)
		}
	}
	if _, err := r.appendRec(ctx, spec.Durability, &wire.JournalRecord{Start: start}); err != nil {
		return ctx, nil, err
	}
	nctx := withSpanContext(ctx, SpanContext{
		TraceID: traceID, OperationID: opID, ParentOperationID: sc.OperationID,
		NodeID: r.status.NodeID, RuntimeID: r.status.RuntimeID, StreamID: r.streamID,
	})
	return nctx, newOp(r, ctx, traceID, opID, spec.EndDurability), nil
}

// Observe records an observation against the current operation carried by
// ctx; with no current operation it is CategoryInvalidArgument.
func (r *Recorder) Observe(ctx context.Context, spec ObservationSpec) error {
	sc, ok := FromContext(ctx)
	if !ok {
		return invalidArg("observe without a current operation")
	}
	return r.observe(ctx, sc.OperationID, spec)
}

func (r *Recorder) observe(appendCtx context.Context, opID string, spec ObservationSpec) error {
	if err := validateCommon(spec.Name, spec.Links, spec.Artifacts, spec.Durability); err != nil {
		return err
	}
	payload, t, err := r.san.checkedJSON(spec.Payload)
	if err != nil {
		return err
	}
	obs := &wire.Observation{
		OperationID: opID, At: r.stamp(), Name: spec.Name, Kind: spec.Kind,
		ReasonCode: r.san.scalar(&t, ScalarID, spec.ReasonCode), Evidence: spec.Evidence,
		Artifacts: r.san.artifacts(&t, spec.Artifacts), Links: r.san.links(spec.Links), JSONPayload: payload,
	}
	if s := spec.Subject; s != nil {
		obs.Subject = &wire.Subject{Kind: r.san.scalar(&t, ScalarID, s.Kind), ID: r.san.scalar(&t, ScalarID, s.ID)}
	}
	if p := spec.Provenance; p != nil {
		obs.Provenance = &wire.Provenance{
			SourceKind: r.san.scalar(&t, ScalarID, p.SourceKind), SourceRef: r.san.scalar(&t, ScalarLocator, p.SourceRef),
			ObservedBy: r.san.scalar(&t, ScalarID, p.ObservedBy), Method: r.san.scalar(&t, ScalarID, p.Method),
		}
		if !p.ObservedAt.IsZero() {
			obs.Provenance.ObservedAt = &wire.Stamp{WallUnixNanos: p.ObservedAt.UnixNano()}
		}
	}
	obs.Sanitization = t.ptr()
	_, err = r.appendRec(appendCtx, spec.Durability, &wire.JournalRecord{Observation: obs})
	return err
}

// recordHealth writes (and notes in the health ring) a JournalHealth record
// with every scalar sanitized; attempts beyond wire.MaxAttempts are dropped and
// counted in the detail.
func (r *Recorder) recordHealth(ctx context.Context, d Durability, kind wire.HealthKind, code, detail, source string, attempts []Attempt) error {
	r.noteHealth(kind, code, detail, attempts)
	var t tally
	clean := r.san.text(&t, detail)
	if len(attempts) > maxHealthAttemptsOut {
		clean += fmt.Sprintf(" [%d attempts dropped]", len(attempts)-maxHealthAttemptsOut)
		attempts = attempts[:maxHealthAttemptsOut]
	}
	h := &wire.JournalHealth{
		Kind: kind, At: r.stamp(), DetailCode: r.san.scalar(&t, ScalarID, code),
		Detail: clean, SelectedSource: r.san.scalar(&t, ScalarID, source),
	}
	for _, a := range attempts {
		h.Attempts = append(h.Attempts, wire.PathAttempt{
			Source: r.san.scalar(&t, ScalarID, a.Source), Path: r.san.scalar(&t, ScalarLocator, a.Path),
			ErrorCode: r.san.scalar(&t, ScalarID, a.ErrCode),
		})
	}
	_, err := r.appendRec(ctx, d, &wire.JournalRecord{Health: h})
	return err
}

// Close closes the Sink (draining accepted diagnostics) and is idempotent;
// a degraded recorder has nothing to close. Appends after Close fail
// (Critical) or are dropped and counted (Diagnostic).
func (r *Recorder) Close(ctx context.Context) error {
	if r.sink == nil {
		return nil
	}
	return r.sink.Close(ctx)
}
