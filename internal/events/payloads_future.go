package events

import (
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// Event types that later milestones act on. They are registered in M1 so that
// the journal can already represent them and so that a build which encounters
// one does not report an unknown type.
//
// M1 records these facts and nothing else: no ProjectState field is derived
// from them, and no behaviour is attached. That is deliberate — speculative
// behaviour for subsystems that do not exist would be architecture invented
// ahead of evidence.
const (
	TypeLessonCandidateCreated  Type = "LessonCandidateCreated"
	TypeLessonPromoted          Type = "LessonPromoted"
	TypeRefactoringEpochStarted Type = "RefactoringEpochStarted"
	TypeArchitectureReconciled  Type = "ArchitectureReconciled"
)

// LessonCandidateCreated records a proposal derived from trajectories (M8).
type LessonCandidateCreated struct {
	LessonCandidateID string `json:"lesson_candidate_id"`
	// Scope is the protocol enum, not a free string: the event and the
	// LessonCandidate it references must agree on how widely a lesson may
	// apply, and DCI-073 makes that bound the whole point of the field.
	Scope        protocol.LessonScope `json:"scope"`
	Observation  string               `json:"observation"`
	RecordDigest string               `json:"record_digest"`
}

// Type implements Payload.
func (p *LessonCandidateCreated) Type() Type { return TypeLessonCandidateCreated }

// Validate implements Payload.
func (p *LessonCandidateCreated) Validate() error {
	if p.LessonCandidateID == "" || p.Observation == "" || p.RecordDigest == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"LessonCandidateCreated: lesson_candidate_id, observation and record_digest are required")
	}
	if !p.Scope.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"LessonCandidateCreated: scope %q is not a known lesson scope", string(p.Scope))
	}
	return nil
}

// LessonPromoted records governed promotion of a lesson (M8). The approver
// and version are durable because DCI-072 requires promotion to be versioned
// and reversible.
type LessonPromoted struct {
	LessonCandidateID string `json:"lesson_candidate_id"`
	PromotedBy        string `json:"promoted_by"`
	TargetRef         string `json:"target_ref"`
	Version           string `json:"version"`
}

// Type implements Payload.
func (p *LessonPromoted) Type() Type { return TypeLessonPromoted }

// Validate implements Payload.
func (p *LessonPromoted) Validate() error {
	if p.LessonCandidateID == "" || p.PromotedBy == "" || p.TargetRef == "" || p.Version == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"LessonPromoted: lesson_candidate_id, promoted_by, target_ref and version are required")
	}
	return nil
}

// RefactoringEpochStarted records the start of a planned refactoring epoch (M7).
type RefactoringEpochStarted struct {
	EpochID string `json:"epoch_id"`
	Trigger string `json:"trigger"`
	Scope   string `json:"scope,omitempty"`
}

// Type implements Payload.
func (p *RefactoringEpochStarted) Type() Type { return TypeRefactoringEpochStarted }

// Validate implements Payload.
func (p *RefactoringEpochStarted) Validate() error {
	if p.EpochID == "" || p.Trigger == "" {
		return errs.New(errs.CategoryInvalidArgument, "RefactoringEpochStarted: epoch_id and trigger are required")
	}
	return nil
}

// ArchitectureReconciled records the outcome of comparing implemented reality
// with intended architecture (M7, DCI-061).
type ArchitectureReconciled struct {
	ReconciliationID string   `json:"reconciliation_id"`
	Outcome          string   `json:"outcome"`
	SupersededADRs   []string `json:"superseded_adrs,omitempty"`
	MigrationRef     string   `json:"migration_ref,omitempty"`
}

// Type implements Payload.
func (p *ArchitectureReconciled) Type() Type { return TypeArchitectureReconciled }

// Validate implements Payload.
func (p *ArchitectureReconciled) Validate() error {
	if p.ReconciliationID == "" || p.Outcome == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"ArchitectureReconciled: reconciliation_id and outcome are required")
	}
	return nil
}

func init() {
	Register(TypeLessonCandidateCreated, func() Payload { return &LessonCandidateCreated{} })
	Register(TypeLessonPromoted, func() Payload { return &LessonPromoted{} })
	Register(TypeRefactoringEpochStarted, func() Payload { return &RefactoringEpochStarted{} })
	Register(TypeArchitectureReconciled, func() Payload { return &ArchitectureReconciled{} })
}
