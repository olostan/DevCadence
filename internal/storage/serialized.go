package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/olostan/DevCadence/internal/errs"

	"modernc.org/sqlite"
)

// Contention policy of WriteSerialized (WP-M5-1). The whole attempt is retried,
// never part of it, and never after a commit whose outcome is uncertain.
const (
	// ContentionBudget is the total time one WriteSerialized call may spend,
	// bounded further by the caller's own deadline.
	ContentionBudget = 2 * time.Second
	// MaxContentionAttempts bounds complete-transaction attempts.
	MaxContentionAttempts = 5

	defaultBusyTimeoutMillis = 5000
)

// contentionDelays are the deterministic waits between attempts.
var contentionDelays = [...]time.Duration{
	10 * time.Millisecond, 25 * time.Millisecond, 50 * time.Millisecond, 100 * time.Millisecond,
}

// busySentinel is the identity of ErrBusy. It unwraps to errs.ErrConflict so
// that the error is a CategoryConflict, while errors.Is(err, ErrBusy) matches
// only contention exhaustion and not every conflict.
type busySentinel struct{}

func (busySentinel) Error() string { return "conflict: storage write lock is busy" }
func (busySentinel) Unwrap() error { return errs.ErrConflict }

// ErrBusy reports that the SQLite write lock could not be obtained within the
// contention budget. Nothing was written. It is not a staleness verdict.
var ErrBusy error = busySentinel{}

// isBusy reports a SQLITE_BUSY or SQLITE_LOCKED error, by typed driver code
// and never by message text.
func isBusy(err error) bool {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	switch sqliteErr.Code() & 0xff {
	case 5, 6: // SQLITE_BUSY, SQLITE_LOCKED
		return true
	}
	return false
}

// WriteSerialized runs fn in a read-write transaction that has already taken
// the SQLite write lock through the principal_write_serialization row, so every
// read fn performs observes the state it will write against (I2).
//
// BUSY or LOCKED before commit rolls the whole attempt back and retries the
// complete transaction, at most MaxContentionAttempts times under
// ContentionBudget, with fresh reads each attempt. An exhausted budget returns
// ErrBusy; a cancelled or expired caller context returns that context's error
// unchanged. A commit error is never retried: its outcome is uncertain and is
// reported as an internal failure for the caller to resolve from durable state.
func (s *Store) WriteSerialized(ctx context.Context, fn func(*Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	budgetCtx, cancel := context.WithTimeout(ctx, ContentionBudget)
	defer cancel()

	var last error
	for attempt := 0; attempt < MaxContentionAttempts; attempt++ {
		err := s.serializedAttempt(ctx, budgetCtx, attemptBusyBudget(budgetCtx, MaxContentionAttempts-attempt), fn)
		if err == nil {
			return nil
		}
		if parentErr := ctx.Err(); parentErr != nil {
			return parentErr
		}
		budgetSpent := budgetCtx.Err() != nil
		if !budgetSpent && !isBusy(err) {
			return err
		}
		last = err
		if budgetSpent || attempt == MaxContentionAttempts-1 {
			break
		}
		timer := time.NewTimer(contentionDelays[attempt])
		select {
		case <-budgetCtx.Done():
			timer.Stop()
		case <-timer.C:
		}
		if budgetCtx.Err() != nil {
			break
		}
	}
	if parentErr := ctx.Err(); parentErr != nil {
		return parentErr
	}
	if last != nil && !isBusy(last) && !errors.Is(last, context.DeadlineExceeded) {
		return last
	}
	return fmt.Errorf("%w: gave up after the %s contention budget", ErrBusy, ContentionBudget)
}

// attemptBusyBudget shares the remaining budget across the attempts left, so
// that per-connection busy waits never outlive the declared deadline.
func attemptBusyBudget(ctx context.Context, attemptsLeft int) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return ContentionBudget / MaxContentionAttempts
	}
	share := time.Until(deadline) / time.Duration(attemptsLeft)
	if share < time.Millisecond {
		share = time.Millisecond
	}
	return share
}

// serializedAttempt runs one complete transaction attempt. The budget context
// bounds only the wait for the connection and for the write lock; the
// transaction body and its commit run under the caller's own context, so the
// contention budget never cuts off legitimate work once the lock is held.
func (s *Store) serializedAttempt(ctx, budgetCtx context.Context, busyWait time.Duration, fn func(*Tx) error) error {
	conn, err := s.db.Conn(budgetCtx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	// The pool holds one connection, so the busy timeout set here applies to
	// this attempt only and is restored before the connection is released.
	if _, err := conn.ExecContext(budgetCtx, fmt.Sprintf("PRAGMA busy_timeout = %d", busyWait.Milliseconds()+1)); err != nil {
		return err
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(),
			fmt.Sprintf("PRAGMA busy_timeout = %d", defaultBusyTimeoutMillis))
	}()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	scope := &Tx{tx: tx, clock: s.clock, recordValidator: s.recordValidator}
	if err := scope.acquireWriteIntent(budgetCtx); err != nil {
		return err
	}
	if err := fn(scope); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		committed = true // the transaction is finished either way
		return &commitError{cause: err}
	}
	committed = true
	return nil
}

// commitError marks a commit failure so it is reported, never retried.
type commitError struct{ cause error }

func (e *commitError) Error() string {
	return "internal: commit transaction: outcome must be resolved from durable state: " + e.cause.Error()
}
func (e *commitError) Unwrap() error {
	return errs.Wrap(errs.CategoryInternal, e.cause, "commit transaction")
}

// acquireWriteIntent takes the SQLite write lock through the serialization row
// before any other read in the transaction. Exactly one row must exist.
func (t *Tx) acquireWriteIntent(ctx context.Context) error {
	res, err := t.tx.ExecContext(ctx,
		`UPDATE principal_write_serialization SET marker = marker WHERE id = 1`)
	if err != nil {
		if isBusy(err) || ctx.Err() != nil {
			return err
		}
		return errs.Wrap(errs.CategoryIntegrity, err, "acquire write intent")
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "acquire write intent")
	}
	if affected != 1 {
		return errs.New(errs.CategoryIntegrity,
			"principal_write_serialization row is missing or duplicated; it is never recreated as recovery")
	}
	var rows, zero int
	if err := t.tx.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(marker = 0 AND id = 1), 0) FROM principal_write_serialization`).
		Scan(&rows, &zero); err != nil {
		return errs.Wrap(errs.CategoryIntegrity, err, "verify write intent row")
	}
	if rows != 1 || zero != 1 {
		return errs.New(errs.CategoryIntegrity, "principal_write_serialization row is corrupt")
	}
	return nil
}
