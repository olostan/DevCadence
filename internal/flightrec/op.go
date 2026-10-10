package flightrec

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/flightrec/wire"
)

// EndTimeout bounds every END append and every post-start Critical append:
// they run under context.WithTimeout(context.WithoutCancel(startCtx),
// EndTimeout) so a cancelled or expired caller context cannot prevent them.
const EndTimeout = 5 * time.Second

// ErrAlreadyEnded is returned by a second End; nothing is written.
var ErrAlreadyEnded = errors.New("flightrec: operation already ended")

// Op is the local handle of one started operation. It is safe for concurrent
// use. It holds the Start context privately and is never stored in a context.
type Op struct {
	r        *Recorder
	startCtx context.Context
	traceID  string
	id       string
	endDur   Durability
	ended    atomic.Bool

	mu        sync.Mutex
	sealed    bool
	results   map[string]any
	artifacts []ArtifactRef
	usage     *wire.Usage
}

func newOp(r *Recorder, startCtx context.Context, traceID, id string, endDur Durability) *Op {
	return &Op{r: r, startCtx: startCtx, traceID: traceID, id: id, endDur: endDur, results: map[string]any{}}
}

// ID is the operation id.
func (o *Op) ID() string { return o.id }

// TraceID is the trace id.
func (o *Op) TraceID() string { return o.traceID }

// SetResult records a result value applied (sanitized) at End. At most 64 keys
// are kept; further keys are dropped and counted in Stats.ResultKeysDropped.
// Calls after End has begun are ignored.
func (o *Op) SetResult(key string, v any) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.sealed {
		return
	}
	if _, exists := o.results[key]; !exists && len(o.results) >= maxResultKeys {
		o.r.stats.resultKeys.Add(1)
		return
	}
	o.results[key] = v
}

// AddArtifact attaches an artifact reference to END. At most 32 are kept; an
// artifact whose non-empty digest is not "sha256:<64 hex>" is dropped. Drops
// are counted in Stats.ArtifactsDropped. Calls after End has begun are ignored.
func (o *Op) AddArtifact(a ArtifactRef) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.sealed {
		return
	}
	if !validDigest(a.Digest) || len(o.artifacts) >= maxOpArtifacts {
		o.r.stats.artifacts.Add(1)
		return
	}
	o.artifacts = append(o.artifacts, a)
}

// SetUsage records resource usage for END (a negative duration is zero).
func (o *Op) SetUsage(in, out uint64, dur time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.sealed {
		return
	}
	dur = max(dur, 0)
	o.usage = &wire.Usage{InputTokens: in, OutputTokens: out, DurationNanos: uint64(dur)}
}

func (o *Op) detached() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(o.startCtx), EndTimeout)
}

// Observe records an observation against this operation. After End it is
// CategoryInvalidArgument, nothing is written and Stats.ObserveAfterEnd counts
// it. A Critical observation uses the detached EndTimeout context.
func (o *Op) Observe(spec ObservationSpec) error {
	if o.ended.Load() {
		o.r.stats.observeAfterEnd.Add(1)
		return invalidArg("observe after end of operation")
	}
	ctx, cancel := o.detached()
	defer cancel()
	return o.r.observe(ctx, o.id, spec)
}

// End records the terminal record exactly once (CAS); the loser gets
// ErrAlreadyEnded, writes nothing and is counted in Stats.DuplicateEnd.
// OutcomePanic, an unspecified outcome and a non-nil cause with
// OutcomeCompleted are CategoryInvalidArgument and do not consume the END. A
// failed Critical END append returns the error (Stats.EndFailed); a dropped
// Diagnostic END returns nil (Stats.EndDropped).
func (o *Op) End(outcome Outcome, cause error) error {
	switch {
	case outcome == OutcomePanic:
		return invalidArg("OutcomePanic is recorded only by Run while unwinding a panic")
	case outcome < OutcomeCompleted || outcome > OutcomeCancelled:
		return invalidArg("invalid outcome")
	case outcome == OutcomeCompleted && cause != nil:
		return invalidArg("a non-nil cause contradicts OutcomeCompleted")
	}
	return o.end(outcome, cause, nil)
}

// endPanic records OUTCOME_PANIC (Critical, best effort). Only Run calls it,
// from its recover path; only the dynamic type of the panic value is recorded.
func (o *Op) endPanic(p any) error { return o.end(OutcomePanic, nil, p) }

func errorCode(cause error) string {
	var e *errs.Error
	switch {
	case errors.As(cause, &e):
		return string(e.Category)
	case errors.Is(cause, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(cause, context.Canceled):
		return "canceled"
	}
	return "error"
}

// safeError renders err even when its Error method panics.
func safeError(err error) (msg string) {
	defer func() {
		if recover() != nil {
			msg = "[error text unavailable]"
		}
	}()
	return err.Error()
}

func errorFields(outcome Outcome, cause error, panicVal any) (code, summary string) {
	if outcome == OutcomePanic {
		return "panic", fmt.Sprintf("%T", panicVal)
	}
	if cause == nil {
		return "", ""
	}
	return errorCode(cause), safeError(cause)
}

func (o *Op) end(outcome Outcome, cause error, panicVal any) error {
	if !o.ended.CompareAndSwap(false, true) {
		o.r.stats.duplicateEnd.Add(1)
		return ErrAlreadyEnded
	}
	o.mu.Lock()
	o.sealed = true
	results, arts, usage := o.results, o.artifacts, o.usage
	o.mu.Unlock()

	r := o.r
	var payload any
	if len(results) > 0 {
		payload = results
	}
	res, t := r.san.json(payload)
	code, summary := errorFields(outcome, cause, panicVal)
	end := &wire.OperationEnd{
		OperationID: o.id, At: r.stamp(), Outcome: outcome,
		ErrorCode: r.san.scalar(&t, ScalarID, code), ErrorSummary: r.san.text(&t, summary),
		JSONResult: res, Usage: usage, Artifacts: r.san.artifacts(&t, arts),
	}
	end.Sanitization = t.ptr()
	d := o.endDur
	if outcome == OutcomePanic {
		d = Critical
	}
	ctx, cancel := o.detached()
	defer cancel()
	accepted, err := r.appendRec(ctx, d, &wire.JournalRecord{End: end})
	if err != nil {
		r.stats.endFailed.Add(1)
		return err
	}
	if !accepted {
		r.stats.endDropped.Add(1)
	}
	return nil
}
