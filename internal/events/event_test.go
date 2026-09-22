package events_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/tasks"
	"github.com/olostan/DevCadence/internal/testsupport"
)

func envelope(payload events.Payload) events.Event {
	return events.Event{
		Seq:           1,
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       "evt_00000000000000000000000001",
		ProjectID:     "example",
		EventType:     payload.Type(),
		OccurredAt:    protocol.NewTimestamp(testsupport.Epoch),
		Actor:         protocol.Actor{Kind: protocol.ActorControlPlane, ID: "devcadence"},
		Correlation:   events.CorrelationFor(payload),
		Payload:       payload,
	}
}

func TestEveryDocumentedEventTypeIsRegistered(t *testing.T) {
	// The list in ENGINEERING_STANDARDS.md §11 plus the types M1 adds. A
	// missing registration would make the journal unable to represent a
	// transition the design already names.
	want := []events.Type{
		events.TypeProjectInitialized, events.TypeRequirementRecorded,
		events.TypeDesignCandidateCreated, events.TypeDecisionRecorded,
		events.TypeTaskCreated, events.TypeWorkPackageApproved, events.TypeTaskDelegated,
		events.TypeAttemptStarted, events.TypeAttemptBlocked, events.TypeCandidateProduced,
		events.TypeValidationCompleted, events.TypeReviewCompleted, events.TypeEscalationRaised,
		events.TypeChangeAccepted, events.TypeChangeRejected,
		events.TypeLessonCandidateCreated, events.TypeLessonPromoted,
		events.TypeRefactoringEpochStarted, events.TypeArchitectureReconciled,
	}
	for _, eventType := range want {
		if !events.Registered(eventType) {
			t.Errorf("event type %s is not registered", eventType)
		}
	}
}

// TestEveryRegisteredTypeRoundTrips checks that the registry and the payload
// types agree: decoding an encoded envelope must give back the same typed
// payload, for every type, without a hand-written table.
func TestEveryRegisteredTypeRoundTrips(t *testing.T) {
	for _, eventType := range events.RegisteredTypes() {
		t.Run(string(eventType), func(t *testing.T) {
			payload, err := events.DecodePayload(eventType, json.RawMessage(`{}`))
			if err != nil {
				t.Fatalf("decode an empty payload: %v", err)
			}
			if payload.Type() != eventType {
				t.Fatalf("payload reports type %s, registered as %s", payload.Type(), eventType)
			}
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			again, err := events.DecodePayload(eventType, encoded)
			if err != nil {
				t.Fatalf("re-decode: %v", err)
			}
			if again.Type() != eventType {
				t.Fatalf("re-decoded payload reports type %s", again.Type())
			}
		})
	}
}

func TestUnknownEventTypeIsRefusedAsACompatibilityProblem(t *testing.T) {
	_, err := events.DecodePayload(events.Type("SomethingFromTheFuture"), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("an unregistered event type was decoded")
	}
	if got := errs.CategoryOf(err); got != errs.CategorySchemaVersionUnsupported {
		t.Fatalf("category = %s, want schema_version_unsupported (%v)", got, err)
	}
}

func TestUnknownPayloadFieldsAreRefused(t *testing.T) {
	_, err := events.DecodePayload(events.TypeTaskCreated,
		json.RawMessage(`{"task_id":"t","alias":"DC-001","title":"x","change_class":"local","surprise":1}`))
	if err == nil {
		t.Fatal("an unknown payload field was silently discarded")
	}
}

func TestEnvelopeRoundTripsThroughJSON(t *testing.T) {
	original := envelope(&events.TaskCreated{
		TaskID: "tsk_1", Alias: "DC-001", Title: "Bounded reads",
		MilestoneID: "M1", ChangeClass: protocol.ChangeSystemic,
		DependsOn: []string{"DC-000"},
	})
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded events.Event
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	payload, ok := decoded.Payload.(*events.TaskCreated)
	if !ok {
		t.Fatalf("payload came back as %T", decoded.Payload)
	}
	if payload.Alias != "DC-001" || payload.ChangeClass != protocol.ChangeSystemic {
		t.Fatalf("payload changed: %+v", payload)
	}
	if decoded.Correlation.TaskID != "tsk_1" {
		t.Fatalf("correlation lost: %+v", decoded.Correlation)
	}
}

// TestEnvelopeAndPayloadTypeMustAgree catches a mislabelled event, which
// would make the journal mean something different from what it says.
func TestEnvelopeAndPayloadTypeMustAgree(t *testing.T) {
	event := envelope(&events.TaskCreated{
		TaskID: "tsk_1", Alias: "DC-001", Title: "x", ChangeClass: protocol.ChangeLocal,
	})
	event.EventType = events.TypeChangeAccepted
	err := event.Validate()
	if err == nil {
		t.Fatal("an envelope that disagrees with its payload was accepted")
	}
	if got := errs.CategoryOf(err); got != errs.CategoryIntegrity {
		t.Fatalf("category = %s, want integrity (%v)", got, err)
	}
}

func TestEnvelopeRequiresAnAttributableActor(t *testing.T) {
	event := envelope(&events.TaskCreated{
		TaskID: "tsk_1", Alias: "DC-001", Title: "x", ChangeClass: protocol.ChangeLocal,
	})
	event.Actor = protocol.Actor{}
	if err := event.Validate(); err == nil {
		t.Fatal("an unattributed event was accepted")
	}
	event.Actor = protocol.Actor{Kind: protocol.ActorKind("wizard"), ID: "gandalf"}
	if err := event.Validate(); err == nil {
		t.Fatal("an unknown actor kind was accepted")
	}
}

func TestPayloadValidationRejectsIncompleteFacts(t *testing.T) {
	cases := []struct {
		name    string
		payload events.Payload
	}{
		{"task without an alias", &events.TaskCreated{TaskID: "t", Title: "x", ChangeClass: protocol.ChangeLocal}},
		{"approval without a state revision", &events.WorkPackageApproved{
			TaskID: "t", WorkPackageID: "wp", WorkPackageVersion: 1,
			RecordDigest: "sha256:x", ChangeClass: protocol.ChangeLocal,
		}},
		{"escalation without a decision owner", &events.EscalationRaised{
			TaskID: "t", EscalationID: "esc",
			Reason: tasksBlockedReasonWithoutAuthority(),
		}},
		{"attempt validation without an attempt", &events.ValidationCompleted{
			TaskID: "t", ValidationID: "val", Scope: events.ScopeAttempt,
			Status: protocol.ValidationPass, RecordDigest: "sha256:x",
		}},
		{"passing validation that lists failed checks", &events.ValidationCompleted{
			TaskID: "t", AttemptID: "att", ValidationID: "val", Scope: events.ScopeAttempt,
			Status: protocol.ValidationPass, RecordDigest: "sha256:x",
			FailedChecks: []string{"chk_test"},
		}},
		{"attempt that records no project state revision", &events.AttemptStarted{
			TaskID: "t", AttemptID: "att", WorkPackageID: "wp", WorkPackageVersion: 1,
			WorkerRole: "implementer",
		}},
		{"acceptance without a decision owner", &events.ChangeAccepted{
			TaskID: "t", AttemptID: "att", CandidateCommit: "c", SemanticSummary: "s",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.payload.Validate(); err == nil {
				t.Fatal("an incomplete payload was accepted")
			}
		})
	}
}

// tasksBlockedReasonWithoutAuthority builds a block that names no decision
// owner, which must be refused: nobody could unblock it.
func tasksBlockedReasonWithoutAuthority() tasks.BlockedReason {
	return tasks.BlockedReason{Trigger: "contradiction", Statement: "A2 is false"}
}

// TestAcceptanceRequiresReviewEvidence pins the DCI-032 floor: an accepted
// implementation candidate must cite deterministic validation *and*
// independent review. Without the review requirement a task could pass
// through REVIEWING without any review having happened, making the state
// ceremonial. How many reviews, and along which dimensions, is M6 policy;
// zero is not a policy choice the domain permits.
func TestAcceptanceRequiresReviewEvidence(t *testing.T) {
	complete := func() *events.ChangeAccepted {
		return &events.ChangeAccepted{
			TaskID: "task_1", AttemptID: "att_1", WorkPackageID: "wp_1",
			CandidateCommit: "c0ffee", SemanticSummary: "Did the thing.",
			ValidationIDs: []string{"val_1"}, ReviewIDs: []string{"rev_1"},
			DecidedBy: protocol.AuthorityPrincipal,
		}
	}
	// Control: the fixture must be accepted, so the refusals below are
	// caused by the one field each case empties.
	if err := complete().Validate(); err != nil {
		t.Fatalf("a complete acceptance was refused: %v", err)
	}
	for _, tc := range []struct {
		name  string
		empty func(*events.ChangeAccepted)
	}{
		{"no review evidence", func(a *events.ChangeAccepted) { a.ReviewIDs = nil }},
		{"empty review evidence", func(a *events.ChangeAccepted) { a.ReviewIDs = []string{} }},
		{"no validation evidence", func(a *events.ChangeAccepted) { a.ValidationIDs = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := complete()
			tc.empty(payload)
			err := payload.Validate()
			if err == nil {
				t.Fatal("an acceptance with no evidence of its own kind was accepted")
			}
			if got := errs.CategoryOf(err); got != errs.CategoryInvalidArgument {
				t.Fatalf("category = %s, want %s (%v)", got, errs.CategoryInvalidArgument, err)
			}
		})
	}
}

// TestDecodingAnEventValidatesTheEnvelope closes the last route by which an
// unsupported event could reach a caller as a usable value.
//
// The storage read path validates, but decoding is the boundary where an
// event arrives from outside this process, and a half-valid Event should
// never exist for a caller to act on.
func TestDecodingAnEventValidatesTheEnvelope(t *testing.T) {
	valid := envelope(&events.RiskRecorded{
		RiskID: "R-001", Severity: protocol.SeverityLow, Statement: "noted",
	})
	document, err := json.Marshal(valid)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Control: the well-formed envelope round-trips.
	var round events.Event
	if err := json.Unmarshal(document, &round); err != nil {
		t.Fatalf("a valid event did not decode: %v", err)
	}

	tampered := strings.Replace(string(document), `"schema_version":"1.0"`, `"schema_version":"99.0"`, 1)
	if tampered == string(document) {
		t.Fatal("test did not modify the schema version")
	}
	var decoded events.Event
	if err := json.Unmarshal([]byte(tampered), &decoded); err == nil {
		t.Fatal("an event with an unsupported schema version decoded successfully")
	}
}

// TestLessonScopeIsTheClosedEnum keeps the event and the LessonCandidate it
// references from disagreeing about how widely a lesson may apply, which
// DCI-073 makes the whole point of the field.
func TestLessonScopeIsTheClosedEnum(t *testing.T) {
	payload := &events.LessonCandidateCreated{
		LessonCandidateID: "lc_1", Observation: "Bounded reads need a test.",
		RecordDigest: "sha256:0", Scope: protocol.LessonScope("everywhere"),
	}
	if err := payload.Validate(); err == nil {
		t.Fatal("an unknown lesson scope was accepted")
	}
	payload.Scope = protocol.LessonScopeProject
	if err := payload.Validate(); err != nil {
		t.Fatalf("a known lesson scope was refused: %v", err)
	}
}

// TestReviewCorrelationCarriesTheWorkPackage keeps observability able to join
// a review to the blueprint it judged without reopening the record.
func TestReviewCorrelationCarriesTheWorkPackage(t *testing.T) {
	correlation := events.CorrelationFor(&events.ReviewCompleted{
		TaskID: "tsk_1", AttemptID: "att_1", ReviewID: "rev_1", WorkPackageID: "wp_1",
		Dimension: protocol.DimensionCorrectness, Verdict: protocol.VerdictPass,
		RecordDigest: "sha256:0",
	})
	if correlation.WorkPackageID != "wp_1" {
		t.Fatalf("work package correlation = %q, want wp_1", correlation.WorkPackageID)
	}
}
