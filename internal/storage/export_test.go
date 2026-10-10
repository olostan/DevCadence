package storage

import (
	"context"
	"time"
)

// ExecForTest runs a raw statement inside the transaction.
//
// It exists only so that tests can attack the storage layer the way a bug or
// an operator with a sqlite3 shell would: writing rows the application code
// would never write, and attempting mutations the schema forbids. It is
// defined in a _test file so that it cannot be reached from production code.
func (t *Tx) ExecForTest(ctx context.Context, statement string, args ...any) error {
	_, err := t.tx.ExecContext(ctx, statement, args...)
	return err
}

// ExecWithoutImmutabilityForTest runs a raw statement with the append-only
// triggers temporarily dropped.
//
// It models the one thing those triggers cannot defend against: a database
// edited outside the application. Digest verification is what covers that
// case, and this is how the tests reach it. Like ExecForTest it lives in a
// _test file and is unreachable from production code.
func (t *Tx) ExecWithoutImmutabilityForTest(ctx context.Context, statement string, args ...any) error {
	for _, trigger := range []string{
		"events_immutable_update", "events_immutable_delete",
		"records_immutable_update", "records_immutable_delete",
	} {
		if _, err := t.tx.ExecContext(ctx, "DROP TRIGGER IF EXISTS "+trigger); err != nil {
			return err
		}
	}
	_, err := t.tx.ExecContext(ctx, statement, args...)
	return err
}

// QueryIntForTest runs a statement returning one integer, such as a PRAGMA.
func (t *Tx) QueryIntForTest(ctx context.Context, statement string, args ...any) (int, error) {
	var value int
	err := t.tx.QueryRowContext(ctx, statement, args...).Scan(&value)
	return value, err
}

// IsBusyForTest exposes the busy classifier.
func IsBusyForTest(err error) bool { return isBusy(err) }

// NewCommitErrorForTest wraps cause as the store reports a failed commit.
func NewCommitErrorForTest(cause error) error { return &commitError{cause: cause} }

// ProvokeBusyForTest returns the real driver error produced when the write
// lock is held elsewhere and no busy wait is allowed.
func (s *Store) ProvokeBusyForTest(ctx context.Context) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "PRAGMA busy_timeout = 1"); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, "UPDATE principal_write_serialization SET marker = marker WHERE id = 1")
	return err
}

// AttemptBusyBudgetForTest exposes the per-attempt busy-wait share so that its
// no-deadline and exhausted-deadline branches can be pinned without waiting.
func AttemptBusyBudgetForTest(ctx context.Context, attemptsLeft int) time.Duration {
	return attemptBusyBudget(ctx, attemptsLeft)
}
