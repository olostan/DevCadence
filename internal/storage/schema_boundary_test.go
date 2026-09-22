package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/schema"
	"github.com/olostan/DevCadence/internal/storage"
)

// TestSchemaInvalidRecordCannotBePersisted covers the gap between the two
// representations of a contract.
//
// ProductDecision.RecordedAt is a Go string whose schema declares
// `format: date-time`. The Go type cannot express that, so semantic
// validation passes and only the schema catches it. If the write boundary
// checked Go validation alone, a record the published contract rejects would
// become durable evidence.
func TestSchemaInvalidRecordCannotBePersisted(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)

	decision := decisionFor("example", "Yes, fully offline.")
	notATimestamp := "last Tuesday"
	decision.RecordedAt = &notATimestamp

	// The Go type is satisfied: this is exactly the blind spot.
	if err := decision.Validate(); err != nil {
		t.Fatalf("precondition: Go validation should accept this record, got %v", err)
	}

	err := store.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.PutRecord(ctx, "example", 1, decision)
		return err
	})
	if err == nil {
		t.Fatal("a record violating its published schema became durable")
	}
	if got := errs.CategoryOf(err); got != errs.CategoryInvalidArgument {
		t.Fatalf("category = %s, want invalid_argument (%v)", got, err)
	}
	if !strings.Contains(err.Error(), "schema") {
		t.Fatalf("error does not attribute the refusal to the schema: %v", err)
	}

	// Nothing was written.
	var latest int
	if err := store.Read(ctx, func(tx *storage.Tx) error {
		var err error
		latest, err = tx.LatestRecordVersion(ctx, "example", "ProductDecision", "PD-001")
		return err
	}); err != nil {
		t.Fatalf("latest version: %v", err)
	}
	if latest != 0 {
		t.Fatalf("the refused record was persisted at version %d", latest)
	}

	// A well-formed timestamp persists.
	valid := "2026-09-20T11:15:00.000000Z"
	decision.RecordedAt = &valid
	if err := store.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.PutRecord(ctx, "example", 1, decision)
		return err
	}); err != nil {
		t.Fatalf("a valid record was refused: %v", err)
	}
}

// TestSchemaEnforcementCoversEveryRegisteredRecordKind proves the boundary is
// not selective: every kind the protocol layer defines is checked, discovery
// records included.
func TestSchemaEnforcementCoversEveryRegisteredRecordKind(t *testing.T) {
	validator, err := schema.DefaultValidator()
	if err != nil {
		t.Fatalf("validator: %v", err)
	}
	// An obviously invalid document for each kind must be refused. If a kind
	// were unregistered, ValidateDocument would report that instead — which
	// is also a refusal, and also correct.
	for kind := range schema.RecordKindToSchema {
		if err := validator.ValidateDocument(kind, []byte(`{"schema_version":"1.0"}`)); err == nil {
			t.Errorf("record kind %s accepted a document missing every required field", kind)
		}
	}
}

// TestUnregisteredRecordKindCannotBePersisted stops a newly added protocol
// type from silently bypassing the twin-representation rule: without a
// registered schema there is nothing to check it against, so it is refused
// rather than waved through.
func TestUnregisteredRecordKindCannotBePersisted(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)

	err := store.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.PutRecord(ctx, "example", 1, &unregisteredRecord{})
		return err
	})
	if err == nil {
		t.Fatal("a record kind with no published schema became durable")
	}
	if !strings.Contains(err.Error(), "no registered JSON Schema") {
		t.Fatalf("error does not explain the missing registration: %v", err)
	}
}

// unregisteredRecord stands in for a protocol type added without a schema.
type unregisteredRecord struct{}

func (r *unregisteredRecord) RecordKind() string                { return "SomethingNewAndUnregistered" }
func (r *unregisteredRecord) RecordID() string                  { return "new_1" }
func (r *unregisteredRecord) SchemaVer() protocol.SchemaVersion { return protocol.SchemaVersion1 }
func (r *unregisteredRecord) Validate() error                   { return nil }
