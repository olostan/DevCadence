package events

import (
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
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

// sameOptional compares a repeated field the event may legitimately omit. A
// compact event that says nothing about an optional field is not contradicting
// the record; one that names a different value is.
func sameOptional(claimed string, stored *string) bool {
	if claimed == "" {
		return true
	}
	return stored != nil && *stored == claimed
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
	// The baseline metadata is what makes a stale plan detectable
	// (docs/PROJECT_STATE.md §7). A blueprint stored against one baseline
	// while the journal records another would make staleness undetectable in
	// exactly the case it exists to catch.
	if wp.ProjectStateRevision != p.ProjectStateRevision {
		return mismatch(event, "project_state_revision",
			p.ProjectStateRevision, wp.ProjectStateRevision)
	}
	if wp.BaseCommit != p.BaseCommit {
		return mismatch(event, "base_commit", p.BaseCommit, wp.BaseCommit)
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
	if review.WorkPackageID != p.WorkPackageID {
		return mismatch(event, "work_package_id", p.WorkPackageID, review.WorkPackageID)
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
	if decision.Question != p.Question {
		return mismatch(event, "question", p.Question, decision.Question)
	}
	if decision.Answer != p.Answer {
		return mismatch(event, "answer", p.Answer, decision.Answer)
	}
	// The human who decided and the decision superseded are the provenance a
	// later reader follows; a journal that names a different actor than the
	// record would misattribute human authority (DCI-009).
	if !sameOptional(p.AuthorityActor, decision.AuthorityActor) {
		return mismatch(event, "authority_actor", p.AuthorityActor, decision.AuthorityActor)
	}
	if !sameOptional(p.Supersedes, decision.Supersedes) {
		return mismatch(event, "supersedes", p.Supersedes, decision.Supersedes)
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
	const event = "DiscoveryExperimentStarted"
	if experiment.ExperimentID != p.ExperimentID {
		return mismatch(event, "experiment_id", p.ExperimentID, experiment.ExperimentID)
	}
	if experiment.Question != p.Question {
		return mismatch(event, "question", p.Question, experiment.Question)
	}
	if experiment.Hypothesis != p.Hypothesis {
		return mismatch(event, "hypothesis", p.Hypothesis, experiment.Hypothesis)
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
	if !sameOptional(p.ResultSummary, experiment.ResultSummary) {
		return mismatch(event, "result_summary", p.ResultSummary, experiment.ResultSummary)
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
	const event = "LessonCandidateCreated"
	if candidate.LessonCandidateID != p.LessonCandidateID {
		return mismatch(event, "lesson_candidate_id",
			p.LessonCandidateID, candidate.LessonCandidateID)
	}
	if candidate.Scope != p.Scope {
		return mismatch(event, "scope", p.Scope, candidate.Scope)
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
// The record is a SpecificationReviewResult, not a ReviewResult. The two are
// different kinds of evidence: a specification review judges whether intent is
// understood well enough to build from, before any Engineering Work Package or
// Attempt exists, so it cannot carry the attempt and work-package identifiers a
// ReviewResult requires. Pointing this event at a ReviewResult would have been
// a claim about a document that cannot represent it.
//
// The digest stays optional: a specification review may be journalled during
// discovery before its full document is written.
func (p *SpecificationReviewCompleted) ReferencedRecord() RecordRef {
	return RecordRef{Kind: "SpecificationReviewResult", ID: p.ReviewID, Digest: p.RecordDigest}
}

// CheckReferencedRecord implements RecordReferencing.
func (p *SpecificationReviewCompleted) CheckReferencedRecord(document []byte) error {
	var review protocol.SpecificationReviewResult
	if err := protocol.Unmarshal(document, &review); err != nil {
		return err
	}
	const event = "SpecificationReviewCompleted"
	if review.ReviewID != p.ReviewID {
		return mismatch(event, "review_id", p.ReviewID, review.ReviewID)
	}
	if review.ReviewerProfile != p.ReviewerProfile {
		return mismatch(event, "reviewer_profile", p.ReviewerProfile, review.ReviewerProfile)
	}
	if review.Summary != p.Summary {
		return mismatch(event, "summary", p.Summary, review.Summary)
	}
	// The gaps the journal reports are the gaps the record found: a review
	// that opened an ambiguity the compact event omits would leave that
	// question invisible to anything reading the journal alone.
	if !sameStrings(p.MaterialGapsFound, review.MaterialGapRefs) {
		return mismatch(event, "material_gaps_found", p.MaterialGapsFound, review.MaterialGapRefs)
	}
	return nil
}

// sameStrings compares two reference lists as sets: order carries no meaning
// in either the event or the record, but membership does.
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, v := range a {
		seen[v]++
	}
	for _, v := range b {
		seen[v]--
		if seen[v] < 0 {
			return false
		}
	}
	return true
}
