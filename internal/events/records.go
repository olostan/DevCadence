package events

import (
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/protocol"
)

// RecordRef names the durable record a compact event claims exists.
//
// An event carrying a `record_digest` makes a factual claim: "there is an
// immutable document with this identity and these bytes." The reducer cannot
// check it — it is pure and has no store (ADR-0005) — so the claim is
// verified in the control-plane transaction before the event is appended.
// Without that check, the journal can assert that evidence exists when it
// does not, and every lineage check downstream inherits the fiction.
type RecordRef struct {
	// Kind is the protocol record kind, as Record.RecordKind reports it.
	Kind string
	// ID is the record's own identifier within the project.
	ID string
	// Version is the exact record version the event refers to, or 0 when the
	// event names no version and the latest one is meant.
	Version int
	// Digest is the claimed content digest. An empty digest means the payload
	// makes no claim on this occasion, which is legitimate only where the
	// field is optional.
	Digest string
}

// Claimed reports whether the payload actually claims a durable record.
func (r RecordRef) Claimed() bool { return r.Digest != "" }

// RecordReferencing is implemented by every payload carrying a record digest.
//
// It is a typed contract rather than a reflective scan of struct tags so that
// the reference is part of each payload's declared behaviour. A drift test
// (TestEveryRecordDigestPayloadIsVerified) fails the build if a payload grows
// a `record_digest` field without implementing this interface, so a future
// event cannot bypass verification by omission.
type RecordReferencing interface {
	Payload
	// ReferencedRecord names the durable record this payload claims exists.
	ReferencedRecord() RecordRef
	// CheckReferencedRecord compares the compact claims this payload carries
	// against the stored document. The event is a summary of the record; the
	// two disagreeing means one of them is wrong, and the journal must not
	// keep a summary of a document that says something else.
	CheckReferencedRecord(document []byte) error
}

// mismatch reports a compact claim that the durable record contradicts.
func mismatch(event, field string, claimed, stored any) error {
	return errs.New(errs.CategoryIntegrity,
		"%s claims %s %v, but the referenced record says %v",
		event, field, claimed, stored)
}

// --- WorkPackageApproved -----------------------------------------------

// ReferencedRecord implements RecordReferencing.
func (p *WorkPackageApproved) ReferencedRecord() RecordRef {
	return RecordRef{
		Kind: "EngineeringWorkPackage", ID: p.WorkPackageID,
		Version: p.WorkPackageVersion, Digest: p.RecordDigest,
	}
}

// CheckReferencedRecord implements RecordReferencing.
func (p *WorkPackageApproved) CheckReferencedRecord(document []byte) error {
	var wp protocol.EngineeringWorkPackage
	if err := protocol.Unmarshal(document, &wp); err != nil {
		return err
	}
	const event = "WorkPackageApproved"
	if wp.WorkPackageID != p.WorkPackageID {
		return mismatch(event, "work_package_id", p.WorkPackageID, wp.WorkPackageID)
	}
	if wp.Version != p.WorkPackageVersion {
		return mismatch(event, "work_package_version", p.WorkPackageVersion, wp.Version)
	}
	if wp.TaskID != p.TaskID {
		return mismatch(event, "task_id", p.TaskID, wp.TaskID)
	}
	if p.ChangeClass != "" && wp.ChangeClass != p.ChangeClass {
		return mismatch(event, "change_class", p.ChangeClass, wp.ChangeClass)
	}
	return nil
}

// --- ValidationCompleted -----------------------------------------------

// ReferencedRecord implements RecordReferencing.
func (p *ValidationCompleted) ReferencedRecord() RecordRef {
	return RecordRef{Kind: "ValidationResult", ID: p.ValidationID, Digest: p.RecordDigest}
}

// CheckReferencedRecord implements RecordReferencing.
func (p *ValidationCompleted) CheckReferencedRecord(document []byte) error {
	var result protocol.ValidationResult
	if err := protocol.Unmarshal(document, &result); err != nil {
		return err
	}
	const event = "ValidationCompleted"
	if result.ValidationID != p.ValidationID {
		return mismatch(event, "validation_id", p.ValidationID, result.ValidationID)
	}
	if result.Subject.Kind != p.Scope {
		return mismatch(event, "scope", p.Scope, result.Subject.Kind)
	}
	if result.Status != p.Status {
		return mismatch(event, "status", p.Status, result.Status)
	}
	if result.Commit != p.Commit {
		return mismatch(event, "commit", p.Commit, result.Commit)
	}
	if result.Subject.TaskID != p.TaskID {
		return mismatch(event, "task_id", p.TaskID, result.Subject.TaskID)
	}
	if result.Subject.AttemptID != p.AttemptID {
		return mismatch(event, "attempt_id", p.AttemptID, result.Subject.AttemptID)
	}
	return nil
}

// --- ReviewCompleted ---------------------------------------------------

// ReferencedRecord implements RecordReferencing.
func (p *ReviewCompleted) ReferencedRecord() RecordRef {
	return RecordRef{Kind: "ReviewResult", ID: p.ReviewID, Digest: p.RecordDigest}
}

// CheckReferencedRecord implements RecordReferencing.
func (p *ReviewCompleted) CheckReferencedRecord(document []byte) error {
	var review protocol.ReviewResult
	if err := protocol.Unmarshal(document, &review); err != nil {
		return err
	}
	const event = "ReviewCompleted"
	if review.ReviewID != p.ReviewID {
		return mismatch(event, "review_id", p.ReviewID, review.ReviewID)
	}
	if review.AttemptID != p.AttemptID {
		return mismatch(event, "attempt_id", p.AttemptID, review.AttemptID)
	}
	if review.Dimension != p.Dimension {
		return mismatch(event, "dimension", p.Dimension, review.Dimension)
	}
	if review.Verdict != p.Verdict {
		return mismatch(event, "verdict", p.Verdict, review.Verdict)
	}
	if review.PrincipalEscalationRecommended != p.PrincipalEscalationRecommended {
		return mismatch(event, "principal_escalation_recommended",
			p.PrincipalEscalationRecommended, review.PrincipalEscalationRecommended)
	}
	return nil
}

// --- Discovery, decision and lesson payloads ---------------------------
//
// The audit in §12 of the M1 evidence pass found eight further payloads of
// exactly this class. They are covered by the same rule rather than by
// one-off guards, because "which events verify their records" must not be a
// list someone can forget to extend.

// ReferencedRecord implements RecordReferencing.
func (p *DecisionRecorded) ReferencedRecord() RecordRef {
	return RecordRef{Kind: "DecisionRecord", ID: p.DecisionID, Digest: p.RecordDigest}
}

// CheckReferencedRecord implements RecordReferencing.
func (p *DecisionRecorded) CheckReferencedRecord(document []byte) error {
	var record protocol.DecisionRecord
	if err := protocol.Unmarshal(document, &record); err != nil {
		return err
	}
	if record.DecisionID != p.DecisionID {
		return mismatch("DecisionRecorded", "decision_id", p.DecisionID, record.DecisionID)
	}
	return nil
}

// ReferencedRecord implements RecordReferencing.
func (p *ProblemModelRevised) ReferencedRecord() RecordRef {
	return RecordRef{
		Kind: "ProblemModel", ID: p.ProblemModelID,
		Version: p.Revision, Digest: p.RecordDigest,
	}
}

// CheckReferencedRecord implements RecordReferencing.
func (p *ProblemModelRevised) CheckReferencedRecord(document []byte) error {
	var model protocol.ProblemModel
	if err := protocol.Unmarshal(document, &model); err != nil {
		return err
	}
	const event = "ProblemModelRevised"
	if model.ProblemModelID != p.ProblemModelID {
		return mismatch(event, "problem_model_id", p.ProblemModelID, model.ProblemModelID)
	}
	if model.Revision != p.Revision {
		return mismatch(event, "revision", p.Revision, model.Revision)
	}
	return nil
}

// ReferencedRecord implements RecordReferencing.
func (p *ProductDecisionRecorded) ReferencedRecord() RecordRef {
	return RecordRef{Kind: "ProductDecision", ID: p.ProductDecisionID, Digest: p.RecordDigest}
}

// CheckReferencedRecord implements RecordReferencing.
func (p *ProductDecisionRecorded) CheckReferencedRecord(document []byte) error {
	var decision protocol.ProductDecision
	if err := protocol.Unmarshal(document, &decision); err != nil {
		return err
	}
	const event = "ProductDecisionRecorded"
	if decision.DecisionID != p.ProductDecisionID {
		return mismatch(event, "product_decision_id", p.ProductDecisionID, decision.DecisionID)
	}
	// Status is the field the reducer's authority rules turn on, so a journal
	// that disagrees with the record about it is the dangerous case.
	if decision.Status != p.Status {
		return mismatch(event, "status", p.Status, decision.Status)
	}
	return nil
}

// ReferencedRecord implements RecordReferencing.
func (p *DiscoveryExperimentStarted) ReferencedRecord() RecordRef {
	return RecordRef{Kind: "DiscoveryExperiment", ID: p.ExperimentID, Digest: p.RecordDigest}
}

// CheckReferencedRecord implements RecordReferencing.
func (p *DiscoveryExperimentStarted) CheckReferencedRecord(document []byte) error {
	var experiment protocol.DiscoveryExperiment
	if err := protocol.Unmarshal(document, &experiment); err != nil {
		return err
	}
	if experiment.ExperimentID != p.ExperimentID {
		return mismatch("DiscoveryExperimentStarted", "experiment_id",
			p.ExperimentID, experiment.ExperimentID)
	}
	return nil
}

// ReferencedRecord implements RecordReferencing.
func (p *DiscoveryExperimentCompleted) ReferencedRecord() RecordRef {
	return RecordRef{Kind: "DiscoveryExperiment", ID: p.ExperimentID, Digest: p.RecordDigest}
}

// CheckReferencedRecord implements RecordReferencing.
func (p *DiscoveryExperimentCompleted) CheckReferencedRecord(document []byte) error {
	var experiment protocol.DiscoveryExperiment
	if err := protocol.Unmarshal(document, &experiment); err != nil {
		return err
	}
	const event = "DiscoveryExperimentCompleted"
	if experiment.ExperimentID != p.ExperimentID {
		return mismatch(event, "experiment_id", p.ExperimentID, experiment.ExperimentID)
	}
	if experiment.Status != p.Status {
		return mismatch(event, "status", p.Status, experiment.Status)
	}
	return nil
}

// ReferencedRecord implements RecordReferencing.
func (p *SpecificationReadinessRecorded) ReferencedRecord() RecordRef {
	return RecordRef{Kind: "SpecificationReadiness", ID: p.ReadinessID, Digest: p.RecordDigest}
}

// CheckReferencedRecord implements RecordReferencing.
func (p *SpecificationReadinessRecorded) CheckReferencedRecord(document []byte) error {
	var readiness protocol.SpecificationReadiness
	if err := protocol.Unmarshal(document, &readiness); err != nil {
		return err
	}
	const event = "SpecificationReadinessRecorded"
	if readiness.ReadinessID != p.ReadinessID {
		return mismatch(event, "readiness_id", p.ReadinessID, readiness.ReadinessID)
	}
	if readiness.ProblemModelID != p.ProblemModelID {
		return mismatch(event, "problem_model_id", p.ProblemModelID, readiness.ProblemModelID)
	}
	if readiness.ProblemModelRevision != p.ProblemModelRevision {
		return mismatch(event, "problem_model_revision",
			p.ProblemModelRevision, readiness.ProblemModelRevision)
	}
	if readiness.Verdict != p.Verdict {
		return mismatch(event, "verdict", p.Verdict, readiness.Verdict)
	}
	return nil
}

// ReferencedRecord implements RecordReferencing.
func (p *LessonCandidateCreated) ReferencedRecord() RecordRef {
	return RecordRef{Kind: "LessonCandidate", ID: p.LessonCandidateID, Digest: p.RecordDigest}
}

// CheckReferencedRecord implements RecordReferencing.
func (p *LessonCandidateCreated) CheckReferencedRecord(document []byte) error {
	var candidate protocol.LessonCandidate
	if err := protocol.Unmarshal(document, &candidate); err != nil {
		return err
	}
	if candidate.LessonCandidateID != p.LessonCandidateID {
		return mismatch("LessonCandidateCreated", "lesson_candidate_id",
			p.LessonCandidateID, candidate.LessonCandidateID)
	}
	return nil
}

// ReferencedRecord implements RecordReferencing.
//
// RequirementRecorded's digest is optional: a requirement may be journalled
// during discovery before any durable document exists. When the digest is
// present the claim is real and is verified like any other.
func (p *RequirementRecorded) ReferencedRecord() RecordRef {
	return RecordRef{Kind: "Requirement", ID: p.RequirementID, Digest: p.RecordDigest}
}

// CheckReferencedRecord implements RecordReferencing.
func (p *RequirementRecorded) CheckReferencedRecord(document []byte) error {
	var requirement protocol.Requirement
	if err := protocol.Unmarshal(document, &requirement); err != nil {
		return err
	}
	const event = "RequirementRecorded"
	if requirement.RequirementID != p.RequirementID {
		return mismatch(event, "requirement_id", p.RequirementID, requirement.RequirementID)
	}
	if requirement.Status != p.Status {
		return mismatch(event, "status", p.Status, requirement.Status)
	}
	return nil
}

// ReferencedRecord implements RecordReferencing.
//
// SpecificationReviewCompleted's digest is optional in the same way.
func (p *SpecificationReviewCompleted) ReferencedRecord() RecordRef {
	return RecordRef{Kind: "ReviewResult", ID: p.ReviewID, Digest: p.RecordDigest}
}

// CheckReferencedRecord implements RecordReferencing.
func (p *SpecificationReviewCompleted) CheckReferencedRecord(document []byte) error {
	var review protocol.ReviewResult
	if err := protocol.Unmarshal(document, &review); err != nil {
		return err
	}
	if review.ReviewID != p.ReviewID {
		return mismatch("SpecificationReviewCompleted", "review_id", p.ReviewID, review.ReviewID)
	}
	return nil
}
