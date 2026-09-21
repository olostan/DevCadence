package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/events"
	"github.com/olostan/DevCadience/internal/protocol"
	"github.com/olostan/DevCadience/internal/storage"
	"github.com/olostan/DevCadience/internal/testsupport"
)

func openStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(),
		storage.Config{Path: storage.MemoryPath, Clock: testsupport.NewClock()})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func sampleEvent(id string, payload events.Payload) events.Event {
	return events.Event{
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       id,
		ProjectID:     "example",
		EventType:     payload.Type(),
		OccurredAt:    protocol.NewTimestamp(testsupport.Epoch),
		Actor:         protocol.Actor{Kind: protocol.ActorControlPlane, ID: "devcadience"},
		Correlation:   events.CorrelationFor(payload),
		Payload:       payload,
	}
}

func initPayload() events.Payload {
	return &events.ProjectInitialized{Name: "Example", MilestoneID: "M1", MilestoneTitle: "Domain core"}
}

func TestAppendAssignsAscendingSequences(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)

	var sequences []int64
	if err := store.Write(ctx, func(tx *storage.Tx) error {
		for i, payload := range []events.Payload{
			initPayload(),
			&events.RiskRecorded{RiskID: "R-001", Severity: protocol.SeverityLow, Statement: "a"},
			&events.RiskRecorded{RiskID: "R-002", Severity: protocol.SeverityHigh, Statement: "b"},
		} {
			appended, err := tx.AppendEvent(ctx, sampleEvent(eventID(i+1), payload))
			if err != nil {
				return err
			}
			sequences = append(sequences, appended.Seq)
		}
		return nil
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	for i := 1; i < len(sequences); i++ {
		if sequences[i] <= sequences[i-1] {
			t.Fatalf("sequences are not ascending: %v", sequences)
		}
	}
}

func TestAppendRejectsADuplicateEventID(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)

	if err := store.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.AppendEvent(ctx, sampleEvent(eventID(1), initPayload()))
		return err
	}); err != nil {
		t.Fatalf("first append: %v", err)
	}
	err := store.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.AppendEvent(ctx, sampleEvent(eventID(1),
			&events.RiskRecorded{RiskID: "R-001", Severity: protocol.SeverityLow, Statement: "a"}))
		return err
	})
	if err == nil {
		t.Fatal("a duplicate event id was accepted")
	}
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("error = %v, want a conflict", err)
	}
}

// TestEventsCannotBeUpdatedOrDeleted is the database-level enforcement of
// "events are facts that happened". A future bug or an ad-hoc sqlite3 session
// must not be able to rewrite history.
func TestEventsCannotBeUpdatedOrDeleted(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	if err := store.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.AppendEvent(ctx, sampleEvent(eventID(1), initPayload()))
		return err
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	for _, statement := range []string{
		`UPDATE events SET event_type = 'TaskCreated' WHERE seq = 1`,
		`DELETE FROM events WHERE seq = 1`,
	} {
		err := store.Write(ctx, func(tx *storage.Tx) error {
			return tx.ExecForTest(ctx, statement)
		})
		if err == nil {
			t.Fatalf("statement %q was permitted against the journal", statement)
		}
	}

	// And the event is still there afterwards.
	var stream []events.Event
	if err := store.Read(ctx, func(tx *storage.Tx) error {
		var err error
		stream, err = tx.ReadEvents(ctx, storage.EventQuery{ProjectID: "example"})
		return err
	}); err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(stream) != 1 {
		t.Fatalf("journal holds %d events, want 1", len(stream))
	}
}

// TestFailedTransactionRollsBackTheAppend is the transactional guarantee the
// control plane depends on: an operation that fails after appending must
// leave no event behind.
func TestFailedTransactionRollsBackTheAppend(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)

	injected := errors.New("simulated failure after the append")
	err := store.Write(ctx, func(tx *storage.Tx) error {
		if _, err := tx.AppendEvent(ctx, sampleEvent(eventID(1), initPayload())); err != nil {
			return err
		}
		return injected
	})
	if !errors.Is(err, injected) {
		t.Fatalf("error = %v, want the injected failure", err)
	}

	var watermark int64
	if err := store.Read(ctx, func(tx *storage.Tx) error {
		var err error
		watermark, err = tx.HighWatermark(ctx, "example")
		return err
	}); err != nil {
		t.Fatalf("watermark: %v", err)
	}
	if watermark != 0 {
		t.Fatalf("high-watermark = %d after a rolled-back append, want 0", watermark)
	}
}

func TestReadEventsFiltersAndOrders(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	payloads := []events.Payload{
		initPayload(),
		&events.RiskRecorded{RiskID: "R-001", Severity: protocol.SeverityLow, Statement: "a"},
		&events.RiskRecorded{RiskID: "R-002", Severity: protocol.SeverityHigh, Statement: "b"},
		&events.RiskResolved{RiskID: "R-001", Resolution: "done"},
	}
	if err := store.Write(ctx, func(tx *storage.Tx) error {
		for i, payload := range payloads {
			if _, err := tx.AppendEvent(ctx, sampleEvent(eventID(i+1), payload)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	read := func(q storage.EventQuery) []events.Event {
		t.Helper()
		var out []events.Event
		if err := store.Read(ctx, func(tx *storage.Tx) error {
			var err error
			out, err = tx.ReadEvents(ctx, q)
			return err
		}); err != nil {
			t.Fatalf("read: %v", err)
		}
		return out
	}

	all := read(storage.EventQuery{ProjectID: "example"})
	if len(all) != len(payloads) {
		t.Fatalf("read %d events, want %d", len(all), len(payloads))
	}
	for i := 1; i < len(all); i++ {
		if all[i].Seq <= all[i-1].Seq {
			t.Fatal("events were not returned in journal order")
		}
	}

	byType := read(storage.EventQuery{ProjectID: "example", Types: []events.Type{events.TypeRiskRecorded}})
	if len(byType) != 2 {
		t.Fatalf("type filter returned %d events, want 2", len(byType))
	}

	bounded := read(storage.EventQuery{ProjectID: "example", AfterSeq: 1, UpToSeq: 3})
	if len(bounded) != 2 || bounded[0].Seq != 2 || bounded[1].Seq != 3 {
		t.Fatalf("bounded read returned %v", sequencesOf(bounded))
	}

	newest := read(storage.EventQuery{ProjectID: "example", Newest: true, Limit: 2})
	if len(newest) != 2 || newest[0].Seq != 4 || newest[1].Seq != 3 {
		t.Fatalf("newest-first read returned %v", sequencesOf(newest))
	}

	other := read(storage.EventQuery{ProjectID: "another-project"})
	if len(other) != 0 {
		t.Fatalf("another project's query returned %d events", len(other))
	}
}

// TestPayloadsSurviveTheRoundTrip checks that a typed payload written to
// SQLite comes back as the same typed value, not as a generic map.
func TestPayloadsSurviveTheRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	original := &events.RiskRecorded{
		RiskID: "R-001", Severity: protocol.SeverityCritical,
		Statement:    "Unmeasured structured-output reliability.",
		EvidenceRefs: []string{"ev_a", "ev_b"},
	}
	if err := store.Write(ctx, func(tx *storage.Tx) error {
		if _, err := tx.AppendEvent(ctx, sampleEvent(eventID(1), initPayload())); err != nil {
			return err
		}
		_, err := tx.AppendEvent(ctx, sampleEvent(eventID(2), original))
		return err
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	var stream []events.Event
	if err := store.Read(ctx, func(tx *storage.Tx) error {
		var err error
		stream, err = tx.ReadEvents(ctx, storage.EventQuery{
			ProjectID: "example", Types: []events.Type{events.TypeRiskRecorded},
		})
		return err
	}); err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(stream) != 1 {
		t.Fatalf("read %d events, want 1", len(stream))
	}
	decoded, ok := stream[0].Payload.(*events.RiskRecorded)
	if !ok {
		t.Fatalf("payload came back as %T, want *events.RiskRecorded", stream[0].Payload)
	}
	if decoded.RiskID != original.RiskID || decoded.Severity != original.Severity ||
		decoded.Statement != original.Statement || len(decoded.EvidenceRefs) != 2 {
		t.Fatalf("payload changed across the round trip: %+v", decoded)
	}
	if stream[0].PayloadDigest == "" {
		t.Fatal("the stored event has no payload digest")
	}
	// The digest must describe the payload that came back.
	recomputed, err := stream[0].ComputeDigest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if recomputed != stream[0].PayloadDigest {
		t.Fatalf("digest mismatch: stored %s, recomputed %s", stream[0].PayloadDigest, recomputed)
	}
}

// TestUnknownStoredEventTypeIsReportedNotSkipped is DCI-092 at the storage
// layer: a record this build cannot interpret must stop the read, because
// skipping it would silently produce a wrong reduction.
func TestUnknownStoredEventTypeIsReportedNotSkipped(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	if err := store.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.AppendEvent(ctx, sampleEvent(eventID(1), initPayload()))
		return err
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	// Write a row directly, as a newer build would have.
	if err := store.Write(ctx, func(tx *storage.Tx) error {
		return tx.ExecForTest(ctx,
			`INSERT INTO events (event_id, project_id, event_type, schema_version, occurred_at,
                actor_kind, actor_id, correlation, payload, payload_digest)
             VALUES ('evt_future', 'example', 'SomethingFromTheFuture', '1.0',
                     '2026-01-02T03:04:05.000000Z', 'control_plane', 'devcadience',
                     '{}', '{"anything":true}', 'sha256:0')`)
	}); err != nil {
		t.Fatalf("insert future event: %v", err)
	}

	err := store.Read(ctx, func(tx *storage.Tx) error {
		_, err := tx.ReadEvents(ctx, storage.EventQuery{ProjectID: "example"})
		return err
	})
	if err == nil {
		t.Fatal("an unknown event type was read without complaint")
	}
	if got := errs.CategoryOf(err); got != errs.CategorySchemaVersionUnsupported {
		t.Fatalf("category = %s, want schema_version_unsupported (%v)", got, err)
	}
}

func TestForeignKeysAreEnforced(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	// projection_tasks references projection_projects; inserting an orphan
	// must fail, which is how the projection stays internally consistent.
	err := store.Write(ctx, func(tx *storage.Tx) error {
		return tx.ExecForTest(ctx,
			`INSERT INTO projection_tasks
                (task_id, project_id, alias, title, milestone_id, change_class, state,
                 work_package_version, created_seq, updated_seq)
             VALUES ('tsk_1', 'no-such-project', 'DC-001', 't', 'M1', 'local', 'proposed', 0, 1, 1)`)
	})
	if err == nil {
		t.Fatal("an orphan projection row was accepted; foreign keys are not enforced")
	}
}

func TestEventQueryRequiresAProject(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	err := store.Read(ctx, func(tx *storage.Tx) error {
		_, err := tx.ReadEvents(ctx, storage.EventQuery{})
		return err
	})
	if err == nil {
		t.Fatal("a project-less event query was accepted")
	}
}

func sequencesOf(stream []events.Event) []int64 {
	out := make([]int64, 0, len(stream))
	for _, e := range stream {
		out = append(out, e.Seq)
	}
	return out
}

func eventID(n int) string {
	const width = 26
	body := make([]byte, width)
	for i := range body {
		body[i] = '0'
	}
	for i := width - 1; i >= 0 && n > 0; i-- {
		body[i] = byte('0' + n%10)
		n /= 10
	}
	return "evt_" + string(body)
}
