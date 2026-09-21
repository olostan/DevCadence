package storage_test

import (
	"context"
	"testing"

	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/events"
	"github.com/olostan/DevCadience/internal/protocol"
	"github.com/olostan/DevCadience/internal/state"
	"github.com/olostan/DevCadience/internal/storage"
)

// corruptionCases are the four ways persisted evidence can stop matching its
// digest. Each is applied with raw SQL, standing in for a database edited
// outside the application — which is precisely the case the append-only
// triggers do not cover.
func TestCorruptedEvidenceIsRejectedOnRead(t *testing.T) {
	cases := []struct {
		name    string
		corrupt string
		read    func(ctx context.Context, tx *storage.Tx) error
	}{
		{
			name: "event payload changed, digest left alone",
			corrupt: `UPDATE events SET payload = '{"name":"Tampered","milestone_id":"M1",` +
				`"milestone_title":"Domain core"}' WHERE seq = 1`,
			read: readFirstEvent,
		},
		{
			name:    "event digest changed",
			corrupt: `UPDATE events SET payload_digest = 'sha256:deadbeef' WHERE seq = 1`,
			read:    readFirstEvent,
		},
		{
			name:    "record document changed",
			corrupt: `UPDATE records SET document = '{"schema_version":"1.0"}' WHERE record_id = 'PD-001'`,
			read:    readDecision,
		},
		{
			name:    "record digest changed",
			corrupt: `UPDATE records SET digest = 'sha256:deadbeef' WHERE record_id = 'PD-001'`,
			read:    readDecision,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := openStore(t)
			seedEvidence(t, store)

			// Sanity: the read succeeds before corruption, so a failure
			// afterwards is attributable to the corruption and not to setup.
			if err := store.Read(ctx, func(tx *storage.Tx) error { return tc.read(ctx, tx) }); err != nil {
				t.Fatalf("read before corruption: %v", err)
			}

			// The immutability triggers guard events and records, so the
			// corruption is applied with them disabled — exactly the
			// authority an attacker or a bug outside the application has.
			if err := store.Write(ctx, func(tx *storage.Tx) error {
				return tx.ExecWithoutImmutabilityForTest(ctx, tc.corrupt)
			}); err != nil {
				t.Fatalf("corrupt: %v", err)
			}

			err := store.Read(ctx, func(tx *storage.Tx) error { return tc.read(ctx, tx) })
			if err == nil {
				t.Fatal("corrupted evidence was returned as if it were intact")
			}
			if got := errs.CategoryOf(err); got != errs.CategoryIntegrity {
				t.Fatalf("category = %s, want integrity (%v)", got, err)
			}
		})
	}
}

func readFirstEvent(ctx context.Context, tx *storage.Tx) error {
	_, err := tx.ReadEvents(ctx, storage.EventQuery{ProjectID: "example"})
	return err
}

func readDecision(ctx context.Context, tx *storage.Tx) error {
	_, err := tx.Record(ctx, "example", "ProductDecision", "PD-001", 1)
	return err
}

// seedEvidence writes one event and one durable record to corrupt.
func seedEvidence(t *testing.T, store *storage.Store) {
	t.Helper()
	ctx := context.Background()
	if err := store.Write(ctx, func(tx *storage.Tx) error {
		if _, err := tx.AppendEvent(ctx, sampleEvent(eventID(1), initPayload())); err != nil {
			return err
		}
		_, err := tx.PutRecord(ctx, "example", 1, decisionFor("example", "Yes, fully offline."))
		return err
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// TestDigestIsOverStoredBytesNotReserialisation pins the digest contract of
// ADR-0003: the digest describes what was written, so a read verifies the
// bytes the database returned rather than a re-encoding of the decoded value.
//
// Re-encoding would hide a change that happens to round-trip, and would be
// impossible for a record this build cannot decode.
func TestDigestIsOverStoredBytesNotReserialisation(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	seedEvidence(t, store)

	var stored storage.StoredRecord
	if err := store.Read(ctx, func(tx *storage.Tx) error {
		var err error
		stored, err = tx.Record(ctx, "example", "ProductDecision", "PD-001", 1)
		return err
	}); err != nil {
		t.Fatalf("read: %v", err)
	}
	if want := protocol.DigestBytes([]byte(stored.Document)); stored.Digest != want {
		t.Fatalf("stored digest %s does not describe the stored bytes (%s)", stored.Digest, want)
	}

	var stream []events.Event
	if err := store.Read(ctx, func(tx *storage.Tx) error {
		var err error
		stream, err = tx.ReadEvents(ctx, storage.EventQuery{ProjectID: "example"})
		return err
	}); err != nil {
		t.Fatalf("read events: %v", err)
	}
	canonical, err := protocol.CanonicalJSON(stream[0].Payload)
	if err != nil {
		t.Fatalf("canonicalise: %v", err)
	}
	if want := protocol.DigestBytes(canonical); stream[0].PayloadDigest != want {
		t.Fatalf("event digest %s does not describe its canonical payload (%s)",
			stream[0].PayloadDigest, want)
	}
}

// TestTamperedProjectionIsRejectedOnRead closes the last read path. The
// projection is derived and rebuildable, so this is not evidence integrity in
// the sense the journal needs — but `state show` reads this row and nothing
// else, so an unverified projection could report state the journal never
// produced.
func TestTamperedProjectionIsRejectedOnRead(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	seedProjection(t, store)

	if err := store.Read(ctx, func(tx *storage.Tx) error {
		_, err := tx.MaterialisedProjectState(ctx, "example")
		return err
	}); err != nil {
		t.Fatalf("read before tampering: %v", err)
	}

	// projection_projects has no immutability trigger: it is rewritten on
	// every append, so a raw edit is the only way it changes unexpectedly.
	if err := store.Write(ctx, func(tx *storage.Tx) error {
		return tx.ExecForTest(ctx,
			`UPDATE projection_projects SET project_state =
                replace(project_state, '"milestone"', '"tampered_milestone"')
             WHERE project_id = 'example'`)
	}); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	err := store.Read(ctx, func(tx *storage.Tx) error {
		_, err := tx.MaterialisedProjectState(ctx, "example")
		return err
	})
	if err == nil {
		t.Fatal("a tampered projection was reported as canonical state")
	}
	if got := errs.CategoryOf(err); got != errs.CategoryIntegrity {
		t.Fatalf("category = %s, want integrity (%v)", got, err)
	}
}

// seedProjection materialises one project's state.
func seedProjection(t *testing.T, store *storage.Store) {
	t.Helper()
	ctx := context.Background()
	if err := store.Write(ctx, func(tx *storage.Tx) error {
		appended, err := tx.AppendEvent(ctx, sampleEvent(eventID(1), initPayload()))
		if err != nil {
			return err
		}
		projection := state.New()
		if err := projection.Apply(&appended); err != nil {
			return err
		}
		return tx.SaveProjection(ctx, projection)
	}); err != nil {
		t.Fatalf("seed projection: %v", err)
	}
}
