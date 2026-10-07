package protocol_test

import (
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/protocol"
)

func validBasis() protocol.ActorBasis {
	return protocol.ActorBasis{
		EndpointID:    "ep-claude-3-5",
		ModelID:       "claude-3-5-sonnet",
		ModelRevision: "20241022",
		Provider:      "anthropic",
		ModelFamily:   "claude-3-5",
		AccountRef:    "acc-default",
	}
}

func TestInvocationProvenanceValidation(t *testing.T) {
	now := time.Now().UTC()
	validReviewer := &protocol.InvocationProvenance{
		SchemaVersion: protocol.SchemaVersion1,
		ProvenanceID:  "rev-123",
		ProjectID:     "proj-1",
		TaskID:        "tsk-1",
		AttemptID:     "att-1",
		WorkPackageID: "wp-1",
		Role:          protocol.ProvenanceRoleReviewer,
		Actor: protocol.ActorProvenance{
			ActorID:      "act-reviewer",
			InvocationID: "inv-1",
			Role:         protocol.ProvenanceRoleReviewer,
		},
		Basis:                 validBasis(),
		IndependenceBasis:     "endpoint_model",
		EndpointBindingDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ContextManifestDigest: "sha256:123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0",
		PromptDigest:          "sha256:23456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef01",
		CandidateCommit:       "c0ffee0000000000000000000000000000000000",
		Dimension:             protocol.DimensionCorrectness,
		StartedAt:             now,
	}

	if err := validReviewer.Validate(); err != nil {
		t.Fatalf("valid reviewer provenance rejected: %v", err)
	}
	if validReviewer.RecordKind() != "InvocationProvenance" {
		t.Fatalf("record kind = %s, want InvocationProvenance", validReviewer.RecordKind())
	}
	if validReviewer.RecordID() != "rev-123" {
		t.Fatalf("record id = %s, want rev-123", validReviewer.RecordID())
	}
	if validReviewer.ProjectOf() != "proj-1" {
		t.Fatalf("project of = %s, want proj-1", validReviewer.ProjectOf())
	}

	// Round-trip test
	bytes, err := protocol.Marshal(validReviewer)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var roundTripped protocol.InvocationProvenance
	if err := protocol.Unmarshal(bytes, &roundTripped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if roundTripped.ProvenanceID != validReviewer.ProvenanceID {
		t.Fatalf("roundtripped provenance id = %s, want %s", roundTripped.ProvenanceID, validReviewer.ProvenanceID)
	}

	// Reviewer missing candidate commit
	badReviewer := *validReviewer
	badReviewer.CandidateCommit = ""
	if err := badReviewer.Validate(); err == nil {
		t.Fatal("reviewer without candidate_commit was accepted")
	}

	// Reviewer with invalid dimension
	badDimension := *validReviewer
	badDimension.Dimension = "invalid_dim"
	if err := badDimension.Validate(); err == nil {
		t.Fatal("reviewer with invalid dimension was accepted")
	}

	// Implementer role valid case
	validImplementer := &protocol.InvocationProvenance{
		SchemaVersion: protocol.SchemaVersion1,
		ProvenanceID:  "att-1:implementer",
		ProjectID:     "proj-1",
		TaskID:        "tsk-1",
		AttemptID:     "att-1",
		WorkPackageID: "wp-1",
		Role:          protocol.ProvenanceRoleImplementer,
		Actor: protocol.ActorProvenance{
			ActorID:      "act-implementer",
			InvocationID: "inv-2",
			Role:         protocol.ProvenanceRoleImplementer,
		},
		Basis:                 validBasis(),
		IndependenceBasis:     "endpoint_model",
		EndpointBindingDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ContextManifestDigest: "sha256:123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0",
		PromptDigest:          "sha256:23456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef01",
		StartedAt:             now,
	}
	if err := validImplementer.Validate(); err != nil {
		t.Fatalf("valid implementer provenance rejected: %v", err)
	}

	// Implementer with non-empty dimension must fail
	badImplementer := *validImplementer
	badImplementer.Dimension = protocol.DimensionCorrectness
	if err := badImplementer.Validate(); err == nil {
		t.Fatal("implementer with dimension was accepted")
	}

	// Verifier role valid case
	validVerifier := &protocol.InvocationProvenance{
		SchemaVersion: protocol.SchemaVersion1,
		ProvenanceID:  "run-1:verifier",
		ProjectID:     "proj-1",
		TaskID:        "tsk-1",
		AttemptID:     "att-1",
		WorkPackageID: "wp-1",
		Role:          protocol.ProvenanceRoleVerifier,
		Actor: protocol.ActorProvenance{
			ActorID:      "act-verifier",
			InvocationID: "inv-3",
			Role:         protocol.ProvenanceRoleVerifier,
		},
		Basis:                 validBasis(),
		IndependenceBasis:     "endpoint_model",
		EndpointBindingDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ContextManifestDigest: "sha256:123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0",
		PromptDigest:          "sha256:23456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef01",
		CandidateCommit:       "c0ffee0000000000000000000000000000000000",
		StartedAt:             now,
	}
	if err := validVerifier.Validate(); err != nil {
		t.Fatalf("valid verifier provenance rejected: %v", err)
	}

	// Verifier with dimension must fail
	badVerifier := *validVerifier
	badVerifier.Dimension = protocol.DimensionCorrectness
	if err := badVerifier.Validate(); err == nil {
		t.Fatal("verifier with dimension was accepted")
	}

	// Actor role mismatch
	roleMismatch := *validReviewer
	roleMismatch.Actor.Role = protocol.ProvenanceRoleImplementer
	if err := roleMismatch.Validate(); err == nil {
		t.Fatal("role mismatch between provenance and actor was accepted")
	}

	// Invalid independence basis
	badIndependence := *validReviewer
	badIndependence.IndependenceBasis = "unsupported_basis"
	if err := badIndependence.Validate(); err == nil {
		t.Fatal("unsupported independence_basis was accepted")
	}

	// Invalid digests
	badDigest := *validReviewer
	badDigest.EndpointBindingDigest = "not-a-digest"
	if err := badDigest.Validate(); err == nil {
		t.Fatal("invalid endpoint_binding_digest was accepted")
	}
}

func TestReviewInvocationIntentValidation(t *testing.T) {
	now := time.Now().UTC()
	intent := &protocol.ReviewInvocationIntent{
		SchemaVersion:         protocol.SchemaVersion1,
		ReviewID:              "rev-1",
		ProjectID:             "proj-1",
		TaskID:                "tsk-1",
		AttemptID:             "att-1",
		WorkPackageID:         "wp-1",
		Dimension:             protocol.DimensionCorrectness,
		InvocationID:          "inv-1",
		CandidateCommit:       "c0ffee0000000000000000000000000000000000",
		InvocationNumber:      1,
		ReviewerBasis:         validBasis(),
		IndependenceBasis:     "endpoint_model",
		EndpointBindingDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		StartedAt:             now,
	}

	if err := intent.Validate(); err != nil {
		t.Fatalf("valid intent rejected: %v", err)
	}
	if intent.RecordKind() != "ReviewInvocationIntent" {
		t.Fatalf("record kind = %s, want ReviewInvocationIntent", intent.RecordKind())
	}
	expectedID := "intent:att-1:correctness"
	if intent.RecordID() != expectedID {
		t.Fatalf("record id = %s, want %s", intent.RecordID(), expectedID)
	}
	if intent.ProjectOf() != "proj-1" {
		t.Fatalf("project of = %s, want proj-1", intent.ProjectOf())
	}

	// Round-trip
	bytes, err := protocol.Marshal(intent)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var roundTripped protocol.ReviewInvocationIntent
	if err := protocol.Unmarshal(bytes, &roundTripped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if roundTripped.RecordID() != expectedID {
		t.Fatalf("roundtripped id = %s, want %s", roundTripped.RecordID(), expectedID)
	}

	// AttemptID containing ':'
	badColonAttempt := *intent
	badColonAttempt.AttemptID = "att:1"
	if err := badColonAttempt.Validate(); err == nil {
		t.Fatal("attempt_id with colon was accepted")
	}

	// Dimension containing ':' (if casted)
	badColonDim := *intent
	badColonDim.Dimension = "correct:ness"
	if err := badColonDim.Validate(); err == nil {
		t.Fatal("dimension with colon was accepted")
	}

	// InvocationNumber != 1
	badInvNum := *intent
	badInvNum.InvocationNumber = 2
	if err := badInvNum.Validate(); err == nil {
		t.Fatal("invocation_number != 1 was accepted")
	}
}

func TestReviewInvocationValidation(t *testing.T) {
	now := time.Now().UTC()
	ended := now.Add(2 * time.Minute)
	inv := &protocol.ReviewInvocation{
		SchemaVersion:       protocol.SchemaVersion1,
		ReviewID:            "rev-1",
		ProjectID:           "proj-1",
		TaskID:              "tsk-1",
		AttemptID:           "att-1",
		WorkPackageID:       "wp-1",
		Dimension:           protocol.DimensionCorrectness,
		InvocationID:        "inv-1",
		ProvenanceID:        "rev-1",
		CandidateCommit:     "c0ffee0000000000000000000000000000000000",
		InvocationNumber:    1,
		Outcome:             protocol.OutcomeCompleted,
		StartedAt:           now,
		EndedAt:             ended,
		UsageArtifactDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}

	if err := inv.Validate(); err != nil {
		t.Fatalf("valid review invocation rejected: %v", err)
	}
	if inv.RecordKind() != "ReviewInvocation" {
		t.Fatalf("record kind = %s, want ReviewInvocation", inv.RecordKind())
	}
	if inv.RecordID() != "rev-1" {
		t.Fatalf("record id = %s, want rev-1", inv.RecordID())
	}
	if inv.ProjectOf() != "proj-1" {
		t.Fatalf("project of = %s, want proj-1", inv.ProjectOf())
	}

	// Round-trip
	bytes, err := protocol.Marshal(inv)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var roundTripped protocol.ReviewInvocation
	if err := protocol.Unmarshal(bytes, &roundTripped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if roundTripped.ReviewID != "rev-1" {
		t.Fatalf("roundtripped review id = %s, want rev-1", roundTripped.ReviewID)
	}

	// All outcomes in enum
	for _, outcome := range []protocol.ReviewInvocationOutcome{
		protocol.OutcomeCompleted,
		protocol.OutcomeOutputInvalid,
		protocol.OutcomeLimitReached,
		protocol.OutcomeDriverError,
		protocol.OutcomeCancelled,
	} {
		testInv := *inv
		testInv.Outcome = outcome
		if err := testInv.Validate(); err != nil {
			t.Fatalf("outcome %s rejected: %v", outcome, err)
		}
	}

	// Invalid outcome
	badOutcome := *inv
	badOutcome.Outcome = "unknown_outcome"
	if err := badOutcome.Validate(); err == nil {
		t.Fatal("invalid outcome was accepted")
	}

	// ProvenanceID != ReviewID
	badProv := *inv
	badProv.ProvenanceID = "different-rev"
	if err := badProv.Validate(); err == nil {
		t.Fatal("provenance_id != review_id was accepted")
	}

	// EndedAt before StartedAt
	badTimes := *inv
	badTimes.EndedAt = now.Add(-1 * time.Minute)
	if err := badTimes.Validate(); err == nil {
		t.Fatal("ended_at before started_at was accepted")
	}
}

func TestNewRecordAllocatesInvocationRecords(t *testing.T) {
	for _, kind := range []string{"InvocationProvenance", "ReviewInvocationIntent", "ReviewInvocation"} {
		rec, err := protocol.NewRecord(kind)
		if err != nil {
			t.Fatalf("NewRecord(%q): %v", kind, err)
		}
		if rec.RecordKind() != kind {
			t.Fatalf("rec.RecordKind() = %q, want %q", rec.RecordKind(), kind)
		}
		if _, ok := rec.(protocol.ProjectScoped); !ok {
			t.Fatalf("%s does not implement ProjectScoped", kind)
		}
	}
}
