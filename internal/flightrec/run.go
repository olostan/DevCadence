package flightrec

import (
	"context"
	"errors"
	"fmt"
)

// Starter is what Run needs from a recorder; consumers may define their own.
type Starter interface {
	Start(ctx context.Context, spec StartSpec) (context.Context, *Op, error)
}

func outcomeFor(err error) Outcome {
	switch {
	case err == nil:
		return OutcomeCompleted
	case errors.Is(err, context.Canceled):
		return OutcomeCancelled
	}
	return OutcomeFailed
}

// Run starts an operation, runs fn and records exactly one END.
//
//   - A Start error is returned and fn is NOT run (callers must refuse the
//     protected effect on a Critical Start failure).
//   - fn returning nil is COMPLETED; an error wrapping context.Canceled is
//     CANCELLED; any other error is FAILED. If recording the END fails the
//     failure is joined with fn's error (errors.Is works for both).
//   - If fn panics, END(OUTCOME_PANIC) is recorded as Critical, best effort
//     (failure only counted in Stats.EndFailed), and the ORIGINAL panic value is
//     re-panicked: it is never swallowed or converted to an error and a nil
//     error is never evidence of COMPLETED while unwinding. Re-panicking loses
//     the original stack trace (the new one starts in Run's deferred func); the
//     value is preserved exactly.
//   - runtime.Goexit inside fn records no END: the START stays unmatched, which
//     means unresolved (not failed, not safe to retry).
//
// Never use a named-return deferred End(err) pattern instead of Run.
func Run(ctx context.Context, rec Starter, spec StartSpec, fn func(context.Context, *Op) error) (err error) {
	ctx2, op, serr := rec.Start(ctx, spec)
	if serr != nil {
		return serr
	}
	returned := false
	defer func() {
		if returned {
			return
		}
		p := recover() // must be called directly by the deferred func
		if p == nil {
			return // runtime.Goexit: no END
		}
		_ = op.endPanic(p)
		panic(p)
	}()
	err = fn(ctx2, op)
	returned = true
	if endErr := op.End(outcomeFor(err), err); endErr != nil && !errors.Is(endErr, ErrAlreadyEnded) {
		err = errors.Join(err, fmt.Errorf("flightrec: record end of %q: %w", spec.Name, endErr))
	}
	return err
}
