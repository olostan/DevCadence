package storage_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/storage"
	"github.com/olostan/DevCadence/internal/testsupport"
)

func openFileStore(t *testing.T, path string) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), storage.Config{Path: path, Clock: testsupport.NewClock()})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// holdLock takes the write lock on store and holds it until release is closed.
func holdLock(t *testing.T, store *storage.Store) (release func()) {
	t.Helper()
	held, stop, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- store.WriteSerialized(context.Background(), func(*storage.Tx) error {
			close(held)
			<-stop
			return nil
		})
	}()
	select {
	case <-held:
	case <-time.After(5 * time.Second):
		t.Fatal("the lock holder never obtained the write lock")
	}
	var once bool
	return func() {
		if once {
			return
		}
		once = true
		close(stop)
		if err := <-done; err != nil {
			t.Errorf("lock holder: %v", err)
		}
	}
}

func TestSerializationRowIsPermanent(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	err := store.Write(ctx, func(tx *storage.Tx) error {
		return tx.ExecForTest(ctx, `DELETE FROM principal_write_serialization`)
	})
	if err == nil {
		t.Fatal("the serialization row was deleted")
	}
	err = store.Write(ctx, func(tx *storage.Tx) error {
		return tx.ExecForTest(ctx, `UPDATE principal_write_serialization SET id = 2`)
	})
	if err == nil {
		t.Fatal("the serialization row id was changed")
	}
	err = store.Write(ctx, func(tx *storage.Tx) error {
		return tx.ExecForTest(ctx, `UPDATE principal_write_serialization SET marker = 1`)
	})
	if err == nil {
		t.Fatal("the serialization marker was changed")
	}
}

func TestMissingSerializationRowIsAnIntegrityFailure(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	if err := store.Write(ctx, func(tx *storage.Tx) error {
		if err := tx.ExecForTest(ctx, `DROP TRIGGER principal_write_serialization_no_delete`); err != nil {
			return err
		}
		return tx.ExecForTest(ctx, `DELETE FROM principal_write_serialization`)
	}); err != nil {
		t.Fatalf("simulate an operator deleting the row: %v", err)
	}
	called := false
	err := store.WriteSerialized(ctx, func(*storage.Tx) error { called = true; return nil })
	if errs.CategoryOf(err) != errs.CategoryIntegrity {
		t.Fatalf("err = %v, want an integrity failure", err)
	}
	if called {
		t.Fatal("the transaction body ran without write intent")
	}
}

func TestWriteSerializedCommitsAndRollsBack(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	if err := store.WriteSerialized(ctx, func(tx *storage.Tx) error {
		_, err := tx.AppendEvent(ctx, sampleEvent("evt_1", initPayload()))
		return err
	}); err != nil {
		t.Fatalf("serialized write: %v", err)
	}
	sentinel := errors.New("domain refusal")
	calls := 0
	err := store.WriteSerialized(ctx, func(tx *storage.Tx) error {
		calls++
		if _, err := tx.AppendEvent(ctx, sampleEvent("evt_2", initPayload())); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) || calls != 1 {
		t.Fatalf("err = %v after %d calls; a domain error is returned unchanged and never retried", err, calls)
	}
	var watermark int64
	if err := store.Read(ctx, func(tx *storage.Tx) error {
		var err error
		watermark, err = tx.HighWatermark(ctx, "example")
		return err
	}); err != nil || watermark != 1 {
		t.Fatalf("watermark = %d (%v), want 1: the refused write must not persist", watermark, err)
	}
}

func TestWriteSerializedHonoursACancelledContext(t *testing.T) {
	store := openStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := store.WriteSerialized(ctx, func(*storage.Tx) error { called = true; return nil })
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("err = %v, called = %v; want context.Canceled and no body", err, called)
	}
}

func TestWriteSerializedRestoresTheBusyTimeout(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	if err := store.WriteSerialized(ctx, func(*storage.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	var timeout int
	if err := store.Read(ctx, func(tx *storage.Tx) error {
		var err error
		timeout, err = tx.QueryIntForTest(ctx, `PRAGMA busy_timeout`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if timeout != 5000 {
		t.Fatalf("busy_timeout = %d after a serialized write, want the default 5000", timeout)
	}
}

// TestWriteSerializedGivesUpWithinTheContentionBudget is the long-held-lock
// half of A12: bounded ErrBusy, no body run, no implicit success, and not a
// context error because the caller never cancelled.
func TestWriteSerializedGivesUpWithinTheContentionBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "busy.db")
	holder, waiter := openFileStore(t, path), openFileStore(t, path)
	release := holdLock(t, holder)
	defer release()

	called := false
	started := time.Now()
	err := waiter.WriteSerialized(context.Background(), func(*storage.Tx) error { called = true; return nil })
	elapsed := time.Since(started)

	if !errors.Is(err, storage.ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	if errs.CategoryOf(err) != errs.CategoryConflict {
		t.Fatalf("category = %s, want conflict", errs.CategoryOf(err))
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		t.Fatalf("exhausted contention was reported as a context error: %v", err)
	}
	if called {
		t.Fatal("the transaction body ran although the lock was never obtained")
	}
	if elapsed > storage.ContentionBudget+1500*time.Millisecond {
		t.Fatalf("gave up after %s, far beyond the %s budget", elapsed, storage.ContentionBudget)
	}
}

func TestWriteSerializedCancellationDuringContention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cancel.db")
	holder, waiter := openFileStore(t, path), openFileStore(t, path)
	release := holdLock(t, holder)
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	called := false
	started := time.Now()
	err := waiter.WriteSerialized(ctx, func(*storage.Tx) error { called = true; return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the caller's deadline error unchanged", err)
	}
	if errors.Is(err, storage.ErrBusy) || called {
		t.Fatalf("a caller deadline must not be reported as busy (called=%v): %v", called, err)
	}
	if time.Since(started) > 1500*time.Millisecond {
		t.Fatalf("cancellation took %s", time.Since(started))
	}
}

func TestWriteSerializedWaitsOutBriefContention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "brief.db")
	holder, waiter := openFileStore(t, path), openFileStore(t, path)
	release := holdLock(t, holder)
	go func() {
		time.Sleep(200 * time.Millisecond)
		release()
	}()
	calls := 0
	if err := waiter.WriteSerialized(context.Background(), func(*storage.Tx) error { calls++; return nil }); err != nil {
		t.Fatalf("a brief contention must not fail the write: %v", err)
	}
	if calls != 1 {
		t.Fatalf("body ran %d times; it must run only once the lock is held", calls)
	}
}

func TestCommitErrorIsNeverClassifiedBusy(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "busy.db")
	holder := openFileStore(t, path)
	other := openFileStore(t, path)
	release := holdLock(t, holder)
	busy := other.ProvokeBusyForTest(ctx)
	release()
	if !storage.IsBusyForTest(busy) {
		t.Fatalf("precondition: %v must classify as busy", busy)
	}
	if storage.IsBusyForTest(storage.NewCommitErrorForTest(busy)) {
		t.Fatal("a busy-shaped commit failure must not classify as busy")
	}
}

func TestWriteSerializedNeverRetriesAFailedCommit(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	attempts := 0
	err := store.WriteSerialized(ctx, func(tx *storage.Tx) error {
		attempts++
		// Defer the foreign-key check to commit, then violate it.
		if err := tx.ExecForTest(ctx, "PRAGMA defer_foreign_keys = ON"); err != nil {
			return err
		}
		return tx.ExecForTest(ctx, `INSERT INTO projection_attempts
			(attempt_id, project_id, task_id, ordinal, status, document, created_seq, updated_seq)
			VALUES ('a1', 'p', 'missing_task', 1, 's', '{}', 1, 1)`)
	})
	if err == nil {
		t.Fatal("commit with a deferred foreign-key violation must fail")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want exactly 1: a commit failure is never retried", attempts)
	}
	if errors.Is(err, storage.ErrBusy) {
		t.Fatalf("err = %v must not be ErrBusy", err)
	}
	if errs.CategoryOf(err) != errs.CategoryInternal && errs.CategoryOf(err) != errs.CategoryIntegrity {
		t.Fatalf("err = %v, want an internal/integrity failure", err)
	}
}

// TestWriteSerializedReportsParentCancellationAfterAFailedAttempt covers the
// caller-cancelled-during-attempt branch deterministically; it otherwise ran
// only when a deadline happened to expire inside a busy wait.
func TestWriteSerializedReportsParentCancellationAfterAFailedAttempt(t *testing.T) {
	store := openStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	err := store.WriteSerialized(ctx, func(*storage.Tx) error {
		calls++
		cancel()
		return errors.New("body failure after cancellation")
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("err = %v after %d calls; the caller's cancellation is reported unchanged and never retried", err, calls)
	}
}

func TestAttemptBusyBudgetBranches(t *testing.T) {
	if got, want := storage.AttemptBusyBudgetForTest(context.Background(), 1),
		storage.ContentionBudget/storage.MaxContentionAttempts; got != want {
		t.Fatalf("no deadline: share = %s, want the fixed %s", got, want)
	}
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
	defer cancel()
	if got := storage.AttemptBusyBudgetForTest(expired, 3); got != time.Millisecond {
		t.Fatalf("expired deadline: share = %s, want the 1ms floor", got)
	}
}

// scriptedErrContext reports the scripted Err results in order, then the real
// ones. It lets a test place a caller cancellation between two exact checks of
// WriteSerialized without sleeping or racing a timer.
type scriptedErrContext struct {
	context.Context
	script []error
}

func (c *scriptedErrContext) Err() error {
	if len(c.script) > 0 {
		err := c.script[0]
		c.script = c.script[1:]
		return err
	}
	return c.Context.Err()
}

// TestWriteSerializedPostLoopOutcomes covers the two ways the retry loop ends
// after the contention budget is spent by a non-busy attempt failure: the
// caller's cancellation, if it appeared after the in-loop check, wins; without
// one the attempt's own error is returned.
func TestWriteSerializedPostLoopOutcomes(t *testing.T) {
	bodyErr := errors.New("body failure once the budget is spent")
	for _, tc := range []struct {
		name   string
		postLp error
		check  func(error) bool
	}{
		{"caller cancelled after the in-loop check", context.Canceled, func(err error) bool { return errors.Is(err, context.Canceled) }},
		{"attempt error survives", nil, func(err error) bool { return errors.Is(err, bodyErr) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := openStore(t)
			inner, cancel := context.WithCancel(context.Background())
			defer cancel()
			// Err checks, in order: entry, in-loop after the attempt, post-loop.
			ctx := &scriptedErrContext{Context: inner, script: []error{nil, nil, tc.postLp}}
			calls := 0
			err := store.WriteSerialized(ctx, func(*storage.Tx) error {
				calls++
				cancel() // spends the budget context, which derives from the caller's
				return bodyErr
			})
			if calls != 1 || !tc.check(err) {
				t.Fatalf("err = %v after %d calls", err, calls)
			}
		})
	}
}
