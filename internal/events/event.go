// Package events defines the append-oriented engineering event journal.
//
// Events are facts about transitions that happened (docs/PROJECT_STATE.md §6).
// They are never edited: a correction is a later event, not a rewrite. The
// journal is the logical history from which ProjectState is reduced
// (DCI-053), so everything a projection needs must be derivable from it.
//
// Payloads are typed and registered. docs/PROTOCOLS.md §19 lists parsing
// critical semantics out of prose as an anti-pattern, so there is no
// free-form payload: an unregistered event type cannot be appended, and a
// stored event whose type this build does not know is reported explicitly
// rather than skipped.
package events

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/protocol"
)

// Type names an event. Values are stable strings written to durable storage;
// renaming one would make history uninterpretable (DCI-093).
type Type string

// Payload is the typed body of an event.
type Payload interface {
	// Type returns the event type this payload belongs to.
	Type() Type
	// Validate rejects a payload that could not describe a real transition.
	Validate() error
}

// Correlation carries the identifiers that place an event in the hierarchy of
// docs/OBSERVABILITY.md §1. Every field is optional because different event
// kinds sit at different depths of that hierarchy.
type Correlation struct {
	MilestoneID      string `json:"milestone_id,omitempty"`
	TaskID           string `json:"task_id,omitempty"`
	WorkPackageID    string `json:"work_package_id,omitempty"`
	AttemptID        string `json:"attempt_id,omitempty"`
	AgentRunID       string `json:"agent_run_id,omitempty"`
	EvidencePacketID string `json:"evidence_packet_id,omitempty"`
	ValidationID     string `json:"validation_id,omitempty"`
	ReviewID         string `json:"review_id,omitempty"`
	ConsultationID   string `json:"consultation_id,omitempty"`
	DecisionID       string `json:"decision_id,omitempty"`
}

// Event is one durable journal entry.
//
// Seq is assigned by the journal on append and is the total order of history.
// It is the ordering authority rather than OccurredAt because two events can
// share a timestamp, and rather than EventID because identifier ordering is
// an encoding detail.
type Event struct {
	Seq           int64                  `json:"seq"`
	SchemaVersion protocol.SchemaVersion `json:"schema_version"`
	EventID       string                 `json:"event_id"`
	ProjectID     string                 `json:"project_id"`
	EventType     Type                   `json:"event_type"`
	OccurredAt    protocol.Timestamp     `json:"occurred_at"`
	Actor         protocol.Actor         `json:"actor"`
	Correlation   Correlation            `json:"correlation,omitempty"`
	Payload       Payload                `json:"payload"`
	// PayloadDigest fixes the canonical payload bytes so that a later reader
	// can detect tampering with historical evidence (docs/SECURITY.md §14).
	PayloadDigest string `json:"payload_digest,omitempty"`
}

// Validate checks the envelope and its payload.
func (e *Event) Validate() error {
	if err := e.SchemaVersion.Validate("Event"); err != nil {
		return err
	}
	if e.EventID == "" {
		return errs.New(errs.CategoryInvalidArgument, "event: event_id is required")
	}
	if e.ProjectID == "" {
		return errs.New(errs.CategoryInvalidArgument, "event %s: project_id is required", e.EventID)
	}
	if !Registered(e.EventType) {
		return errs.New(errs.CategoryInvalidArgument,
			"event %s: %q is not a registered event type", e.EventID, string(e.EventType))
	}
	if err := e.Actor.Validate(); err != nil {
		return err
	}
	if e.OccurredAt.Time().IsZero() {
		return errs.New(errs.CategoryInvalidArgument, "event %s: occurred_at is required", e.EventID)
	}
	if e.Payload == nil {
		return errs.New(errs.CategoryInvalidArgument, "event %s: payload is required", e.EventID)
	}
	// A payload that does not match its envelope type would make the journal
	// mean something different from what it says.
	if e.Payload.Type() != e.EventType {
		return errs.New(errs.CategoryIntegrity,
			"event %s: payload is %s but envelope declares %s", e.EventID, e.Payload.Type(), e.EventType)
	}
	return e.Payload.Validate()
}

// ComputeDigest returns the canonical digest of the event's payload.
func (e *Event) ComputeDigest() (string, error) {
	return protocol.Digest(e.Payload)
}

// MarshalJSON renders the event with its payload inline.
func (e Event) MarshalJSON() ([]byte, error) {
	type alias Event
	return json.Marshal(alias(e))
}

// UnmarshalJSON decodes an event, resolving the payload through the registry
// so that the typed body survives the round trip.
func (e *Event) UnmarshalJSON(data []byte) error {
	// wire mirrors Event but keeps the payload as raw JSON until the event
	// type tells us which concrete payload to decode into.
	type wire struct {
		Seq           int64                  `json:"seq"`
		SchemaVersion protocol.SchemaVersion `json:"schema_version"`
		EventID       string                 `json:"event_id"`
		ProjectID     string                 `json:"project_id"`
		EventType     Type                   `json:"event_type"`
		OccurredAt    protocol.Timestamp     `json:"occurred_at"`
		Actor         protocol.Actor         `json:"actor"`
		Correlation   Correlation            `json:"correlation"`
		Payload       json.RawMessage        `json:"payload"`
		PayloadDigest string                 `json:"payload_digest"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var w wire
	if err := dec.Decode(&w); err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "decode event")
	}
	if _, err := dec.Token(); err != io.EOF {
		return errs.New(errs.CategoryInvalidArgument, "decode event: unexpected trailing content")
	}
	payload, err := DecodePayload(w.EventType, w.Payload)
	if err != nil {
		return err
	}
	*e = Event{
		Seq:           w.Seq,
		SchemaVersion: w.SchemaVersion,
		EventID:       w.EventID,
		ProjectID:     w.ProjectID,
		EventType:     w.EventType,
		OccurredAt:    w.OccurredAt,
		Actor:         w.Actor,
		Correlation:   w.Correlation,
		Payload:       payload,
		PayloadDigest: w.PayloadDigest,
	}
	return nil
}
