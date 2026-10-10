package wire

import (
	"reflect"
)

// codec is the common shape of every wire message.
type codec interface {
	Marshal() ([]byte, error)
	Unmarshal([]byte) error
}

func fixStamp() *Stamp {
	return &Stamp{WallUnixNanos: 1700000000123456789, MonoNanos: 42}
}

func fixArtifact() ArtifactRef {
	return ArtifactRef{
		ID: "art_1", Kind: "stdout", Locator: "file:///x/y", MediaType: "text/plain",
		Digest:    "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		SizeBytes: -7, Truncated: true, DigestVerified: true,
	}
}

func fixLink() Link {
	return Link{Relation: "caused_by", TraceID: "trc_1", OperationID: "op_1", EventID: "evt_1"}
}

func fixSanitization() *Sanitization {
	return &Sanitization{Redactions: 3, Truncations: 2, PayloadReplaced: true}
}

func envelope() JournalRecord {
	return JournalRecord{
		SchemaVersion: 1, EventID: "evt_0001", NodeID: "nod_0001", RuntimeID: "run_0001",
		StreamSequence: 300, Durability: DurabilityCritical, StreamID: "main",
	}
}

func fixStart() *OperationStart {
	return &OperationStart{
		TraceID: "trc_1", OperationID: "op_1", ParentOperationID: "op_0",
		Links: []Link{fixLink(), {Relation: "retry_of", OperationID: "op_9"}}, OperationName: "task.delegate",
		At: fixStamp(), JSONMetadata: []byte(`{"k":"v"}`), ActorID: "act", TaskID: "tsk", AttemptID: "att",
		CanonicalEventID: "evt_c", Artifacts: []ArtifactRef{fixArtifact()}, Sanitization: fixSanitization(),
	}
}

func fixEnd() *OperationEnd {
	return &OperationEnd{
		OperationID: "op_1", At: fixStamp(), Outcome: OutcomeFailed, ErrorCode: "internal", ErrorSummary: "boom",
		JSONResult: []byte(`{"r":1}`), Usage: &Usage{InputTokens: 1, OutputTokens: 2, DurationNanos: 3},
		Artifacts: []ArtifactRef{fixArtifact()}, Sanitization: fixSanitization(),
	}
}

func fixObservation() *Observation {
	return &Observation{
		OperationID: "op_1", At: fixStamp(), Name: "decision.route", Kind: ObservationDecision, ReasonCode: "r1",
		Subject: &Subject{Kind: "capability", ID: "gpu"},
		Provenance: &Provenance{
			SourceKind: "env", SourceRef: "DEVCADENCE_HOME", ObservedBy: "recorder", Method: "probe", ObservedAt: fixStamp(),
		},
		Evidence:  EvidenceConfirmed,
		Artifacts: []ArtifactRef{fixArtifact()}, Links: []Link{fixLink()}, Sanitization: fixSanitization(),
		JSONPayload: []byte(`{"p":true}`),
	}
}

func fixObservationProto() *Observation {
	o := fixObservation()
	o.JSONPayload = nil
	o.ProtoPayload = &Any{TypeURL: "type.googleapis.com/x.Y", Value: []byte{1, 2, 3}}
	return o
}

func fixHealth() *JournalHealth {
	return &JournalHealth{
		Kind: HealthDroppedDiagnostics, At: fixStamp(), DroppedCount: 5, FirstMissingSequence: 6, LastMissingSequence: 9,
		DetailCode: "dropped", Detail: "queue full",
		Attempts:       []PathAttempt{{Source: "explicit_flag", Path: "/tmp/x", ErrorCode: "mkdir_failed"}},
		SelectedSource: "temp", Offset: -3,
	}
}

func fixSegmentHeader() *SegmentHeader {
	return &SegmentHeader{
		SchemaVersion: 1, NodeID: "nod_1", RuntimeID: "run_1", StreamID: "main", SegmentIndex: 2, FirstSequence: 77,
		CreatedAt: fixStamp(), WriterPID: 4242, WriterVersion: "test", MaxRecordBytes: 1 << 20,
		MonoOriginWallUnixNanos: 1700000000000000000,
	}
}

func recWith(f func(*JournalRecord)) *JournalRecord {
	r := envelope()
	f(&r)
	return &r
}

func fixRecordStart() *JournalRecord {
	return recWith(func(r *JournalRecord) { r.Type = RecordTypeOperationStart; r.Start = fixStart() })
}
func fixRecordEnd() *JournalRecord {
	return recWith(func(r *JournalRecord) { r.Type = RecordTypeOperationEnd; r.End = fixEnd() })
}
func fixRecordObservation() *JournalRecord {
	return recWith(func(r *JournalRecord) { r.Type = RecordTypeObservation; r.Observation = fixObservation() })
}
func fixRecordHealth() *JournalRecord {
	return recWith(func(r *JournalRecord) { r.Type = RecordTypeJournalHealth; r.Health = fixHealth() })
}

// fixture is one populated message plus the schema message it exercises.
type fixture struct {
	name    string // schema message name
	variant string
	value   codec
}

func allFixtures() []fixture {
	art := fixArtifact()
	lk := fixLink()
	return []fixture{
		{"Stamp", "", fixStamp()},
		{"ArtifactRef", "", &art},
		{"Link", "", &lk},
		{"Subject", "", &Subject{Kind: "k", ID: "i"}},
		{"Provenance", "", fixObservation().Provenance},
		{"Sanitization", "", fixSanitization()},
		{"Usage", "", &Usage{InputTokens: 1, OutputTokens: 2, DurationNanos: 3}},
		{"PathAttempt", "", &PathAttempt{Source: "s", Path: "/p", ErrorCode: "e"}},
		{"JournalRecord", "start", fixRecordStart()},
		{"JournalRecord", "end", fixRecordEnd()},
		{"JournalRecord", "observation", fixRecordObservation()},
		{"JournalRecord", "health", fixRecordHealth()},
		{"OperationStart", "", fixStart()},
		{"OperationEnd", "", fixEnd()},
		{"Observation", "json", fixObservation()},
		{"Observation", "any", fixObservationProto()},
		{"JournalHealth", "", fixHealth()},
		{"SegmentHeader", "", fixSegmentHeader()},
	}
}

// fresh returns a zero value of the same concrete type as c.
func fresh(c codec) codec {
	return reflect.New(reflect.TypeOf(c).Elem()).Interface().(codec)
}
