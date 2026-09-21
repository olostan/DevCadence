package storage

import "context"

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
