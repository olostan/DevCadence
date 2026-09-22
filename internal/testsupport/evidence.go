package testsupport

import (
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/protocol"
)

// Synthetic evidence for tests and walkthroughs.
//
// These build *coherent* documents: the record says the same thing the event
// summarising it says, because that is exactly what the control plane now
// verifies. A placeholder digest pointing at no record is not a shortcut here,
// it is the bug these helpers exist to stop reappearing — so there is
// deliberately no helper that produces one.

// WorkPackage builds a minimal but valid EngineeringWorkPackage.
func WorkPackage(projectID, taskID, workPackageID string, version int) *protocol.EngineeringWorkPackage {
	return &protocol.EngineeringWorkPackage{
		SchemaVersion:        protocol.SchemaVersion1,
		WorkPackageID:        workPackageID,
		TaskID:               taskID,
		Version:              version,
		ProjectID:            projectID,
		ProjectStateRevision: "ps_000000003",
		BaseCommit:           "91acd8273f1",
		ChangeClass:          protocol.ChangeSystemic,
		Objective:            "Bounded journal reads",
		Rationale:            "The journal grows without bound.",
		ArchitecturalIntent:  "Keep the journal append-only.",
		Scope:                protocol.Scope{InScope: []string{"storage"}, OutOfScope: []string{"envelope"}},
		Guidance: []protocol.Guidance{
			{ID: "G1", Strength: protocol.GuidanceMust, Statement: "The journal stays append-only."},
		},
		AcceptanceCriteria:     []string{"Bounded reads work."},
		ValidationRequirements: []string{"go test ./..."},
		EscalationConditions:   []string{"Assumption A2 is false."},
	}
}

// AttemptValidation builds deterministic evidence for one attempt's candidate.
func AttemptValidation(projectID, validationID, taskID, attemptID, commit string,
	status protocol.ValidationOutcome) *protocol.ValidationResult {
	return validationResult(projectID, validationID, commit, status, protocol.ValidationSubject{
		Kind: protocol.ScopeAttempt, TaskID: taskID, AttemptID: attemptID,
	})
}

// IntegrationValidation builds evidence for an integrated result.
func IntegrationValidation(projectID, validationID, taskID, commit string,
	status protocol.ValidationOutcome) *protocol.ValidationResult {
	return validationResult(projectID, validationID, commit, status, protocol.ValidationSubject{
		Kind: protocol.ScopeIntegration, TaskID: taskID,
	})
}

// BaselineValidation builds evidence for the accepted commit, outside any task.
func BaselineValidation(projectID, validationID, commit string,
	status protocol.ValidationOutcome) *protocol.ValidationResult {
	return validationResult(projectID, validationID, commit, status,
		protocol.ValidationSubject{Kind: protocol.ScopeBaseline})
}

func validationResult(projectID, validationID, commit string,
	status protocol.ValidationOutcome, subject protocol.ValidationSubject) *protocol.ValidationResult {
	check := protocol.CheckResult{
		ID: "chk_test", Kind: "test", Command: []string{"go", "test", "./..."},
		Status: protocol.CheckPass, StartedAt: protocol.NewTimestamp(Epoch),
		FinishedAt: protocol.NewTimestamp(Epoch.Add(time.Minute)),
	}
	if status != protocol.ValidationPass {
		check.Status = protocol.CheckFail
	}
	return &protocol.ValidationResult{
		SchemaVersion: protocol.SchemaVersion1,
		ValidationID:  validationID,
		ProjectID:     projectID,
		Subject:       subject,
		Commit:        commit,
		Status:        status,
		Checks:        []protocol.CheckResult{check},
	}
}

// Review builds model-assisted evidence for one review dimension.
func Review(projectID, reviewID, attemptID, workPackageID string,
	dimension protocol.ReviewDimension, verdict protocol.ReviewVerdict) *protocol.ReviewResult {
	return &protocol.ReviewResult{
		SchemaVersion: protocol.SchemaVersion1,
		ReviewID:      reviewID,
		ProjectID:     projectID,
		AttemptID:     attemptID,
		WorkPackageID: workPackageID,
		Dimension:     dimension,
		Verdict:       verdict,
		Findings:      []protocol.Finding{},
		MustCompliance: []protocol.GuidanceCompliance{
			{GuidanceID: "G1", Status: protocol.ComplianceSatisfied},
		},
	}
}

// Digest returns the digest the store will compute for a record, so a test
// can build the event that references it.
func Digest(t *testing.T, record protocol.Record) string {
	t.Helper()
	digest, err := protocol.Digest(record)
	if err != nil {
		t.Fatalf("digest %s: %v", record.RecordKind(), err)
	}
	return digest
}
