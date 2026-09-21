package protocol_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/protocol"
)

func TestUnsupportedSchemaVersionIsRefusedExplicitly(t *testing.T) {
	document := []byte(`{
      "schema_version": "2.0",
      "project_id": "example",
      "state_revision": "ps_000000001",
      "generated_at": "2026-01-02T03:04:05.000000Z",
      "event_high_watermark": "1",
      "git": {"accepted_commit": null},
      "milestone": {"id": "M1", "title": "t", "completed_tasks": 0, "total_tasks": 0},
      "tasks": {"ready": [], "running": [], "blocked": [], "awaiting_principal": []},
      "validation": {"status": "unknown"},
      "risks": [],
      "decisions_required": []
    }`)
	var out protocol.ProjectState
	err := protocol.Unmarshal(document, &out)
	if err == nil {
		t.Fatal("a record with an unsupported schema version was accepted")
	}
	// DCI-092: a reader either supports a version or fails explicitly. A
	// generic parse error would not tell an operator what to do.
	if got := errs.CategoryOf(err); got != errs.CategorySchemaVersionUnsupported {
		t.Fatalf("category = %s, want schema_version_unsupported (%v)", got, err)
	}
}

// TestUnknownFieldsAreRefusedNotDiscarded is the read-path half of DCI-092.
// Dropping the field would leave the caller believing it had the whole
// record; refusing it makes the compatibility problem visible.
func TestUnknownFieldsAreRefusedNotDiscarded(t *testing.T) {
	document := []byte(`{
      "schema_version": "1.0",
      "project_id": "example",
      "state_revision": "ps_000000001",
      "generated_at": "2026-01-02T03:04:05.000000Z",
      "event_high_watermark": "1",
      "git": {"accepted_commit": null},
      "milestone": {"id": "M1", "title": "t", "completed_tasks": 0, "total_tasks": 0},
      "tasks": {"ready": [], "running": [], "blocked": [], "awaiting_principal": []},
      "validation": {"status": "unknown"},
      "risks": [],
      "decisions_required": [],
      "field_from_a_newer_build": {"anything": true}
    }`)
	var out protocol.ProjectState
	if err := protocol.Unmarshal(document, &out); err == nil {
		t.Fatal("an unknown durable field was silently accepted")
	}
}

func TestTrailingContentIsRefused(t *testing.T) {
	document := []byte(`{"schema_version":"1.0"} {"schema_version":"1.0"}`)
	var out protocol.ProjectState
	if err := protocol.Unmarshal(document, &out); err == nil {
		t.Fatal("two concatenated documents were accepted as one record")
	}
}

func TestCanonicalJSONIsStableAndSorted(t *testing.T) {
	value := map[string]any{
		"zulu":  1,
		"alpha": []any{3, 2, 1},
		"mike":  map[string]any{"b": true, "a": false},
	}
	first, err := protocol.CanonicalJSON(value)
	if err != nil {
		t.Fatalf("canonicalise: %v", err)
	}
	for i := 0; i < 32; i++ {
		again, err := protocol.CanonicalJSON(value)
		if err != nil {
			t.Fatalf("canonicalise: %v", err)
		}
		if string(again) != string(first) {
			t.Fatalf("canonical form is not stable:\n%s\n%s", first, again)
		}
	}
	want := `{"alpha":[3,2,1],"mike":{"a":false,"b":true},"zulu":1}`
	if string(first) != want {
		t.Fatalf("canonical form = %s, want %s", first, want)
	}
}

func TestCanonicalJSONDoesNotEscapeHTML(t *testing.T) {
	// Work Package pseudocode routinely contains < and >. Escaping them would
	// change the digest of semantically identical content.
	out, err := protocol.CanonicalJSON(map[string]string{"code": "if a < b && c > d"})
	if err != nil {
		t.Fatalf("canonicalise: %v", err)
	}
	if string(out) != `{"code":"if a < b && c > d"}` {
		t.Fatalf("HTML escaping was applied to canonical JSON: %s", out)
	}
}

func TestDigestIsAlgorithmPrefixedAndContentAddressed(t *testing.T) {
	a, err := protocol.Digest(map[string]any{"x": 1, "y": 2})
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	// Key order must not change the digest.
	b, err := protocol.Digest(map[string]any{"y": 2, "x": 1})
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if a != b {
		t.Fatalf("digest depends on key order: %s vs %s", a, b)
	}
	if len(a) != len("sha256:")+64 || a[:7] != "sha256:" {
		t.Fatalf("digest %q is not algorithm-prefixed", a)
	}
	c, err := protocol.Digest(map[string]any{"x": 1, "y": 3})
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if a == c {
		t.Fatal("different content produced the same digest")
	}
}

func TestTimestampRoundTripsAtMicrosecondResolution(t *testing.T) {
	// Nanoseconds are truncated deliberately: SQLite and RFC3339 text must
	// give back exactly what was written, or a rebuilt projection would not
	// match the original.
	original := protocol.NewTimestamp(time.Date(2026, 9, 20, 19, 58, 9, 123456789, time.UTC))
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded protocol.Timestamp
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !decoded.Time().Equal(original.Time()) {
		t.Fatalf("timestamp round trip changed the value: %s -> %s", original, decoded)
	}
	if decoded.Time().Nanosecond()%1000 != 0 {
		t.Fatalf("sub-microsecond precision survived: %s", decoded)
	}
}

func TestTimestampsSortLexicographically(t *testing.T) {
	earlier := protocol.NewTimestamp(time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC))
	later := protocol.NewTimestamp(time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC))
	if !(earlier.String() < later.String()) {
		t.Fatalf("textual order disagrees with chronological order: %s vs %s", earlier, later)
	}
}

func TestRequiredArraysSerialiseAsEmptyNotNull(t *testing.T) {
	// The schemas require these keys to be arrays. A nil Go slice would
	// marshal as null and fail validation, so normalisation happens on the
	// write path rather than at each call site.
	projectState := protocol.ProjectState{
		SchemaVersion: protocol.SchemaVersion1,
		ProjectID:     "example",
		StateRevision: "ps_000000001",
		Milestone:     protocol.MilestoneState{ID: "M1", Title: "t"},
		Validation:    protocol.ValidationState{Status: protocol.ValidationUnknown},
	}
	encoded, err := json.Marshal(projectState)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(encoded, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"risks", "decisions_required"} {
		value, ok := generic[key]
		if !ok {
			t.Fatalf("%s is missing from the document", key)
		}
		if _, isArray := value.([]any); !isArray {
			t.Fatalf("%s serialised as %T, want an array", key, value)
		}
	}
	buckets, ok := generic["tasks"].(map[string]any)
	if !ok {
		t.Fatal("tasks is not an object")
	}
	for _, key := range []string{"ready", "running", "blocked", "awaiting_principal"} {
		if _, isArray := buckets[key].([]any); !isArray {
			t.Fatalf("tasks.%s serialised as %T, want an array", key, buckets[key])
		}
	}
}

func TestMarshalRefusesAnInvalidRecord(t *testing.T) {
	// Validating on the write path is what keeps a record a conforming reader
	// would reject from becoming durable.
	invalid := &protocol.ProjectState{SchemaVersion: protocol.SchemaVersion1}
	if _, err := protocol.Marshal(invalid); err == nil {
		t.Fatal("an invalid record was serialised")
	}
}

// TestValidationPassRequiresSomethingToHavePassed pins the claim
// ValidationOutcome already makes in prose: it omits "skipped" because a run
// in which nothing executed has validated nothing. A run with no checks at
// all is that same claim with less ceremony, and both used to be accepted as
// a pass — letting "validated" mean "we tried nothing and found no problems".
func TestValidationPassRequiresSomethingToHavePassed(t *testing.T) {
	result := func(checks ...protocol.CheckResult) *protocol.ValidationResult {
		return &protocol.ValidationResult{
			SchemaVersion: protocol.SchemaVersion1,
			ValidationID:  "val_1", ProjectID: "example",
			Subject: protocol.ValidationSubject{
				Kind: protocol.ScopeAttempt, TaskID: "tsk_1", AttemptID: "att_1",
			},
			Commit: "cafebabe1234567", Status: protocol.ValidationPass, Checks: checks,
		}
	}
	check := func(status protocol.CheckStatus) protocol.CheckResult {
		return protocol.CheckResult{
			ID: "chk_test", Kind: "test", Status: status,
			StartedAt:  protocol.NewTimestamp(time.Unix(0, 0).UTC()),
			FinishedAt: protocol.NewTimestamp(time.Unix(60, 0).UTC()),
		}
	}

	for _, tc := range []struct {
		name   string
		checks []protocol.CheckResult
	}{
		{"no checks at all", nil},
		{"every check skipped", []protocol.CheckResult{check(protocol.CheckSkipped)}},
		{"every check cancelled", []protocol.CheckResult{check(protocol.CheckCancelled)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := result(tc.checks...).Validate(); err == nil {
				t.Fatal("a pass was accepted although no check executed")
			}
		})
	}

	// Control: one genuine pass alongside a skip is a real pass.
	passing := result(check(protocol.CheckPass), check(protocol.CheckSkipped))
	if err := passing.Validate(); err != nil {
		t.Fatalf("a run with a passing check was refused: %v", err)
	}
}

// TestReviewProjectionRoundTrips pins the twin agreement for the review
// campaign projection: the schema publishes it, so the Go type must accept a
// document carrying it, and must still refuse a phase the campaign model does
// not define (ADR-0010). Nothing populates the block in M1 — this guards the
// shape, not any behaviour.
func TestReviewProjectionRoundTrips(t *testing.T) {
	doc := []byte(`{"schema_version":"1.0","project_id":"x","state_revision":"ps_1",` +
		`"generated_at":"2026-01-02T03:04:05.000000Z","event_high_watermark":null,` +
		`"git":{"accepted_commit":null},"milestone":{"id":"M1","title":"t"},` +
		`"tasks":{},"validation":{"status":"green"},"risks":[],"decisions_required":[],` +
		`"review":{"campaign_id":"rc_1","phase":"focused_revalidation","repair_round":1}}`)
	var ps protocol.ProjectState
	if err := protocol.Unmarshal(doc, &ps); err != nil {
		t.Fatalf("a document carrying a review block was refused: %v", err)
	}
	if ps.Review == nil || ps.Review.CampaignID == nil || *ps.Review.CampaignID != "rc_1" {
		t.Fatalf("review block did not decode: %+v", ps.Review)
	}
	bad := []byte(`{"schema_version":"1.0","project_id":"x","state_revision":"ps_1",` +
		`"generated_at":"2026-01-02T03:04:05.000000Z","event_high_watermark":null,` +
		`"git":{"accepted_commit":null},"milestone":{"id":"M1","title":"t"},` +
		`"tasks":{},"validation":{"status":"green"},"risks":[],"decisions_required":[],` +
		`"review":{"phase":"bikeshedding"}}`)
	var invalid protocol.ProjectState
	if err := protocol.Unmarshal(bad, &invalid); err == nil {
		t.Fatal("an unknown review phase was accepted")
	}
}
