package events_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/protocol"
)

// TestEveryRecordDigestPayloadIsVerified is the drift guard behind the
// generic rule.
//
// A payload carrying `record_digest` asserts that an immutable document
// exists. The control plane verifies that claim only for payloads that
// implement RecordReferencing, so a new event type growing the field without
// implementing the interface would silently reintroduce exactly the hole the
// rule closes. Reflection is confined to this test: the production path stays
// a typed interface.
func TestEveryRecordDigestPayloadIsVerified(t *testing.T) {
	var unverified []string
	for _, eventType := range events.RegisteredTypes() {
		payload, err := events.NewPayload(eventType)
		if err != nil {
			t.Fatalf("new payload %s: %v", eventType, err)
		}
		if !hasRecordDigestField(reflect.TypeOf(payload).Elem()) {
			continue
		}
		if _, ok := payload.(events.RecordReferencing); !ok {
			unverified = append(unverified, string(eventType))
		}
	}
	if len(unverified) > 0 {
		t.Fatalf("these payloads carry record_digest but do not implement RecordReferencing, "+
			"so the control plane cannot verify the record they claim: %s\n"+
			"Implement ReferencedRecord and CheckReferencedRecord on each (see internal/events/records.go).",
			strings.Join(unverified, ", "))
	}
}

// TestReferencedRecordNamesTheClaim keeps the interface honest in the other
// direction: a payload that implements it must actually report the digest it
// carries, or verification would pass by looking at nothing.
func TestReferencedRecordNamesTheClaim(t *testing.T) {
	for _, eventType := range events.RegisteredTypes() {
		payload, err := events.NewPayload(eventType)
		if err != nil {
			t.Fatalf("new payload %s: %v", eventType, err)
		}
		referencing, ok := payload.(events.RecordReferencing)
		if !ok {
			continue
		}
		// A freshly allocated payload carries no digest, so it must claim
		// nothing; a populated one must claim what it was given.
		if referencing.ReferencedRecord().Claimed() {
			t.Fatalf("%s claims a record before any digest is set", eventType)
		}
		populated := setRecordDigest(t, payload, "sha256:abc")
		ref := populated.ReferencedRecord()
		if !ref.Claimed() || ref.Digest != "sha256:abc" {
			t.Fatalf("%s does not report the digest it carries: %+v", eventType, ref)
		}
		if ref.Kind == "" {
			t.Fatalf("%s names no record kind", eventType)
		}
	}
}

func hasRecordDigestField(t reflect.Type) bool {
	if t.Kind() != reflect.Struct {
		return false
	}
	for i := range t.NumField() {
		tag := t.Field(i).Tag.Get("json")
		if name, _, _ := strings.Cut(tag, ","); name == "record_digest" {
			return true
		}
	}
	return false
}

// setRecordDigest round-trips the payload through JSON so the test does not
// depend on the field's position or on unexported plumbing.
func setRecordDigest(t *testing.T, payload events.Payload, digest string) events.RecordReferencing {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"record_digest": digest})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := json.Unmarshal(raw, payload); err != nil {
		t.Fatalf("set digest on %s: %v", payload.Type(), err)
	}
	return payload.(events.RecordReferencing)
}

func TestReviewInvocationStartedReferencedRecord(t *testing.T) {
	now := time.Now().UTC()
	intent := &protocol.ReviewInvocationIntent{
		SchemaVersion:    protocol.SchemaVersion1,
		ReviewID:         "rev-1",
		ProjectID:        "proj-1",
		TaskID:           "tsk-1",
		AttemptID:        "att-1",
		WorkPackageID:    "wp-1",
		Dimension:        protocol.DimensionCorrectness,
		InvocationID:     "inv-1",
		CandidateCommit:  "c0ffee0000000000000000000000000000000000",
		InvocationNumber: 1,
		ReviewerBasis: protocol.ActorBasis{
			EndpointID:    "ep-1",
			ModelID:       "m-1",
			ModelRevision: "r-1",
		},
		IndependenceBasis:     "endpoint_model",
		EndpointBindingDigest: "sha256:" + strings.Repeat("a", 64),
		StartedAt:             now,
	}
	doc, err := protocol.Marshal(intent)
	if err != nil {
		t.Fatalf("marshal intent: %v", err)
	}

	started := &events.ReviewInvocationStarted{
		TaskID:          "tsk-1",
		AttemptID:       "att-1",
		WorkPackageID:   "wp-1",
		ReviewID:        "rev-1",
		InvocationID:    "inv-1",
		Dimension:       protocol.DimensionCorrectness,
		CandidateCommit: "c0ffee0000000000000000000000000000000000",
		RecordDigest:    "sha256:" + strings.Repeat("b", 64),
	}

	if err := started.CheckReferencedRecord(doc); err != nil {
		t.Fatalf("CheckReferencedRecord: %v", err)
	}

	ref := started.ReferencedRecord()
	if ref.Kind != "ReviewInvocationIntent" {
		t.Fatalf("ref.Kind = %s, want ReviewInvocationIntent", ref.Kind)
	}
	if ref.ID != "intent:att-1:correctness" {
		t.Fatalf("ref.ID = %s, want intent:att-1:correctness", ref.ID)
	}
	if ref.Digest != started.RecordDigest {
		t.Fatalf("ref.Digest = %s, want %s", ref.Digest, started.RecordDigest)
	}

	// Mismatch test
	bad := *started
	bad.CandidateCommit = "different-commit"
	if err := bad.CheckReferencedRecord(doc); err == nil {
		t.Fatal("candidate_commit mismatch was accepted")
	}
}
