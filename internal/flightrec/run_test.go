package flightrec

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/flightrec/wire"
)

type customPanic struct{ secret string }

var errDisk = errors.New("disk exploded")

func failEnds(rec *wire.JournalRecord) error {
	if rec.End != nil {
		return errDisk
	}
	return nil
}

// B-4: outcomes of Run.
func TestRunOutcomes(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		outcome wire.Outcome
		code    string
	}{
		{"success", nil, wire.OutcomeCompleted, ""},
		{"error", errors.New("boom"), wire.OutcomeFailed, "error"},
		{"categorized", errs.New(errs.CategoryNotFound, "gone"), wire.OutcomeFailed, "not_found"},
		{"canceled", fmt.Errorf("wrapped: %w", context.Canceled), wire.OutcomeCancelled, "canceled"},
		{"deadline", fmt.Errorf("wrapped: %w", context.DeadlineExceeded), wire.OutcomeFailed, "deadline_exceeded"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sink := &memSink{}
			r := newRec(t, sink)
			var inner *Op
			err := Run(context.Background(), r, StartSpec{Name: "run.case"}, func(ctx context.Context, op *Op) error {
				inner = op
				if sc, ok := FromContext(ctx); !ok || sc.OperationID != op.ID() {
					t.Errorf("fn context %+v", sc)
				}
				return c.err
			})
			if !errors.Is(err, c.err) && !(c.err == nil && err == nil) {
				t.Fatalf("returned %v", err)
			}
			recs := sink.records()
			if len(recs) != 2 || recs[0].Start == nil || recs[1].End == nil {
				t.Fatalf("records %d", len(recs))
			}
			e := recs[1].End
			if e.OperationID != inner.ID() || e.Outcome != c.outcome || e.ErrorCode != c.code {
				t.Fatalf("end %+v", e)
			}
		})
	}
}

func TestRunStartErrorSkipsFn(t *testing.T) {
	r := newRec(t, &memSink{})
	ran := false
	err := Run(context.Background(), r, StartSpec{Name: "Bad Name"}, func(context.Context, *Op) error {
		ran = true
		return nil
	})
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument || ran {
		t.Fatalf("%v ran=%v", err, ran)
	}
}

func TestRunIgnoresAlreadyEnded(t *testing.T) {
	sink := &memSink{}
	r := newRec(t, sink)
	err := Run(context.Background(), r, StartSpec{Name: "run.self"}, func(_ context.Context, op *Op) error {
		return op.End(OutcomeFailed, errors.New("ended by fn"))
	})
	if err != nil {
		t.Fatalf("%v", err)
	}
	if recs := sink.records(); len(recs) != 2 || recs[1].End.Outcome != wire.OutcomeFailed {
		t.Fatalf("records %d", len(recs))
	}
	if r.Status().Stats.DuplicateEnd != 1 {
		t.Fatalf("%+v", r.Status().Stats)
	}
}

// B-5: panics are re-raised with the original value; END is OUTCOME_PANIC.
func TestRunPanic(t *testing.T) {
	sink := &memSink{}
	r := newRec(t, sink)
	want := customPanic{secret: "s3cret"}
	var got any
	func() {
		defer func() { got = recover() }()
		_ = Run(context.Background(), r, StartSpec{Name: "run.panic"}, func(context.Context, *Op) error { panic(want) })
	}()
	if got != want {
		t.Fatalf("panic value %v", got)
	}
	recs := sink.records()
	if len(recs) != 2 || sink.class[1] != Critical {
		t.Fatalf("records %d", len(recs))
	}
	e := recs[1].End
	if e.Outcome != wire.OutcomePanic || e.ErrorCode != "panic" || e.ErrorSummary != "flightrec.customPanic" {
		t.Fatalf("end %+v", e)
	}

	// The named-return nil-error pattern: the caller swallows the re-panic, the
	// returned error stays nil, and the record is still PANIC, never COMPLETED.
	sink2 := &memSink{}
	r2 := newRec(t, sink2)
	run := func() (err error) {
		defer func() { _ = recover() }()
		return Run(context.Background(), r2, StartSpec{Name: "run.panic", EndDurability: Diagnostic}, func(context.Context, *Op) error { panic("x") })
	}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if recs := sink2.records(); recs[1].End.Outcome != wire.OutcomePanic || recs[1].End.ErrorSummary != "string" {
		t.Fatalf("end %+v", recs[1].End)
	}

	// panic(nil) is a real panic value in Go 1.21+.
	sink3 := &memSink{}
	r3 := newRec(t, sink3)
	func() {
		defer func() { got = recover() }()
		_ = Run(context.Background(), r3, StartSpec{Name: "run.panic"}, func(context.Context, *Op) error { panic(nil) })
	}()
	var pne *runtime.PanicNilError
	if e, ok := got.(error); !ok || !errors.As(e, &pne) || sink3.records()[1].End.Outcome != wire.OutcomePanic {
		t.Fatalf("panic(nil): %v", got)
	}
}

func TestRunPanicWithFailingEnd(t *testing.T) {
	sink := &memSink{critHook: failEnds}
	r := newRec(t, sink)
	var got any
	func() {
		defer func() { got = recover() }()
		_ = Run(context.Background(), r, StartSpec{Name: "run.panic"}, func(context.Context, *Op) error { panic("original") })
	}()
	if got != "original" {
		t.Fatalf("panic value %v", got)
	}
	if st := r.Status().Stats; st.EndFailed != 1 {
		t.Fatalf("%+v", st)
	}
	if len(sink.records()) != 1 {
		t.Fatal("no END may be recorded")
	}
	// Even a degraded recorder re-raises (the critical END cannot be recorded).
	noop := NewNoop(Status{Reason: "test"})
	func() {
		defer func() { got = recover() }()
		_ = Run(context.Background(), noop, StartSpec{Name: "run.panic"}, func(context.Context, *Op) error { panic("noop") })
	}()
	if got != "noop" || noop.Status().Stats.EndFailed != 1 {
		t.Fatalf("%v %+v", got, noop.Status().Stats)
	}
}

// B-6: both the operation error and the END failure are preserved.
func TestRunEndFailure(t *testing.T) {
	sink := &memSink{critHook: failEnds}
	r := newRec(t, sink)
	opErr := errors.New("operation failed")
	err := Run(context.Background(), r, StartSpec{Name: "run.fail", EndDurability: Critical}, func(context.Context, *Op) error { return opErr })
	if !errors.Is(err, opErr) || !errors.Is(err, errDisk) {
		t.Fatalf("%v", err)
	}
	err = Run(context.Background(), r, StartSpec{Name: "run.ok", EndDurability: Critical}, func(context.Context, *Op) error { return nil })
	if err == nil || !errors.Is(err, errDisk) || errs.CategoryOf(err) != errs.CategoryInternal {
		t.Fatalf("%v", err)
	}
}

// B-7: runtime.Goexit leaves the START unmatched.
func TestRunGoexitLeavesStartUnmatched(t *testing.T) {
	sink := &memSink{}
	r := newRec(t, sink)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = Run(context.Background(), r, StartSpec{Name: "run.goexit"}, func(context.Context, *Op) error {
			runtime.Goexit()
			return nil
		})
	}()
	<-done
	recs := sink.records()
	if len(recs) != 1 || recs[0].Start == nil {
		t.Fatalf("want exactly the START, got %d records", len(recs))
	}
	if st := r.Status().Stats; st.EndFailed != 0 || st.EndDropped != 0 {
		t.Fatalf("%+v", st)
	}
}
