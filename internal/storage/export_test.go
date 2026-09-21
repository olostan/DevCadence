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
