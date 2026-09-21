package events

import (
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/protocol"
)

// Event types that describe the project as a whole rather than one task.
const (
	TypeProjectInitialized     Type = "ProjectInitialized"
	TypeMilestoneStarted       Type = "MilestoneStarted"
	TypeRequirementRecorded    Type = "RequirementRecorded"
	TypeComponentDeclared      Type = "ComponentDeclared"
	TypeDesignCandidateCreated Type = "DesignCandidateCreated"
	TypeDecisionRequired       Type = "DecisionRequired"
	TypeDecisionRecorded       Type = "DecisionRecorded"
	TypeRiskRecorded           Type = "RiskRecorded"
	TypeRiskResolved           Type = "RiskResolved"
	TypeHealthReportRecorded   Type = "HealthReportRecorded"
)

// ProjectInitialized is the first event of every project. It establishes the
// identity and the initial milestone that ProjectState reports.
type ProjectInitialized struct {
	Name string `json:"name"`
	// AcceptedCommit is empty for a project registered before any change has
	// been accepted, which is every M1 project.
	AcceptedCommit string `json:"accepted_commit,omitempty"`
	Branch         string `json:"branch,omitempty"`
	// RepositoryPath is recorded for provenance only. M1 never reads it;
	// repository access is M2 work.
	RepositoryPath string `json:"repository_path,omitempty"`
	VisionRef      string `json:"vision_ref,omitempty"`
	CurrentOutcome string `json:"current_outcome,omitempty"`
	MilestoneID    string `json:"milestone_id"`
	MilestoneTitle string `json:"milestone_title"`
	// ActiveInvariants are the invariant identifiers the project binds itself
	// to, e.g. "DCI-001". They are identifiers, never copied prose.
	ActiveInvariants []string `json:"active_invariants,omitempty"`
}

// Type implements Payload.
func (p *ProjectInitialized) Type() Type { return TypeProjectInitialized }

// Validate implements Payload.
func (p *ProjectInitialized) Validate() error {
	if p.Name == "" {
		return errs.New(errs.CategoryInvalidArgument, "ProjectInitialized: name is required")
	}
	if p.MilestoneID == "" || p.MilestoneTitle == "" {
		return errs.New(errs.CategoryInvalidArgument, "ProjectInitialized: milestone id and title are required")
	}
	if p.AcceptedCommit != "" && len(p.AcceptedCommit) < 7 {
		return errs.New(errs.CategoryInvalidArgument,
			"ProjectInitialized: accepted_commit must be at least 7 characters when present")
	}
	return nil
}

// MilestoneStarted changes the active milestone.
type MilestoneStarted struct {
	MilestoneID string `json:"milestone_id"`
	Title       string `json:"title"`
	Status      string `json:"status,omitempty"`
}

// Type implements Payload.
func (p *MilestoneStarted) Type() Type { return TypeMilestoneStarted }

// Validate implements Payload.
func (p *MilestoneStarted) Validate() error {
	if p.MilestoneID == "" || p.Title == "" {
		return errs.New(errs.CategoryInvalidArgument, "MilestoneStarted: milestone id and title are required")
	}
	return nil
}

// RequirementRecorded captures a requirement entering or changing in the
// project baseline.
//
// Status and SourceType are durable because DCI-015 requires a requirement to
// stay distinguishable as confirmed, evidence-backed, proposed, assumed,
// deferred, rejected or superseded, and DCI-008 forbids model inference from
// being serialised as human-confirmed intent. ProjectState counts requirements
// by status, so both must be derivable from the journal alone.
//
// Repeated events for the same requirement id replace the previous record, so
// a requirement that is promoted from proposed to confirmed keeps one
// identity while history retains both statements.
type RequirementRecorded struct {
	RequirementID string                       `json:"requirement_id"`
	Statement     string                       `json:"statement"`
	Kind          protocol.RequirementKind     `json:"kind,omitempty"`
	Strength      protocol.RequirementStrength `json:"strength,omitempty"`
	Status        protocol.RequirementStatus   `json:"status"`
	SourceType    protocol.SourceType          `json:"source_type"`
	SourceRef     string                       `json:"source_ref,omitempty"`
	RecordDigest  string                       `json:"record_digest,omitempty"`
}

// Type implements Payload.
func (p *RequirementRecorded) Type() Type { return TypeRequirementRecorded }

// Validate implements Payload.
func (p *RequirementRecorded) Validate() error {
	if p.RequirementID == "" || p.Statement == "" {
		return errs.New(errs.CategoryInvalidArgument, "RequirementRecorded: requirement_id and statement are required")
	}
	if !p.Status.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"RequirementRecorded: status %q is not a known requirement status", string(p.Status))
	}
	if !p.SourceType.ValidRequirementSource() {
		return errs.New(errs.CategoryInvalidArgument,
			"RequirementRecorded: source_type %q is not a known requirement source", string(p.SourceType))
	}
	if p.Kind != "" && !p.Kind.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"RequirementRecorded: kind %q is not a known requirement kind", string(p.Kind))
	}
	if p.Strength != "" && !p.Strength.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"RequirementRecorded: strength %q is not MUST, SHOULD or MAY", string(p.Strength))
	}
	// The same guard the Requirement record carries: a confirmed requirement
	// must trace to a human, so the principal cannot confirm its own
	// inference by way of an event (DCI-008, DCI-015).
	if p.Status == protocol.RequirementConfirmed {
		switch p.SourceType {
		case protocol.SourceProductDecision, protocol.SourceHumanStatement:
		default:
			return errs.New(errs.CategoryInvalidArgument,
				"RequirementRecorded: a confirmed requirement must come from a product decision "+
					"or a human statement, not %q", string(p.SourceType))
		}
	}
	return nil
}

// ComponentDeclared records or updates a component's state and contract.
// Repeated events for the same component id replace the previous record, so
// history keeps every version while ProjectState shows only the current one.
type ComponentDeclared struct {
	ComponentID    string                 `json:"component_id"`
	Responsibility string                 `json:"responsibility,omitempty"`
	Status         string                 `json:"status"`
	ContractState  protocol.ContractState `json:"contract_state,omitempty"`
	EvidenceRefs   []string               `json:"evidence_refs,omitempty"`
}

// Type implements Payload.
func (p *ComponentDeclared) Type() Type { return TypeComponentDeclared }

// Validate implements Payload.
func (p *ComponentDeclared) Validate() error {
	if p.ComponentID == "" || p.Status == "" {
		return errs.New(errs.CategoryInvalidArgument, "ComponentDeclared: component_id and status are required")
	}
	if p.ContractState != "" && !p.ContractState.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"ComponentDeclared: contract_state %q is not a known contract state", string(p.ContractState))
	}
	return nil
}

// DesignCandidateCreated records that an architecture candidate was proposed.
// It references the durable design artifact rather than embedding it.
type DesignCandidateCreated struct {
	CandidateID string `json:"candidate_id"`
	Summary     string `json:"summary"`
	ArtifactRef string `json:"artifact_ref,omitempty"`
}

// Type implements Payload.
func (p *DesignCandidateCreated) Type() Type { return TypeDesignCandidateCreated }

// Validate implements Payload.
func (p *DesignCandidateCreated) Validate() error {
	if p.CandidateID == "" || p.Summary == "" {
		return errs.New(errs.CategoryInvalidArgument, "DesignCandidateCreated: candidate_id and summary are required")
	}
	return nil
}

// DecisionRequired opens a question that stops some work without blocking
// unrelated work (docs/PROJECT_STATE.md §11).
type DecisionRequired struct {
	DecisionRequiredID string                     `json:"decision_required_id"`
	Question           string                     `json:"question"`
	Authority          protocol.DecisionAuthority `json:"authority"`
	EvidenceRefs       []string                   `json:"evidence_refs,omitempty"`
}

// Type implements Payload.
func (p *DecisionRequired) Type() Type { return TypeDecisionRequired }

// Validate implements Payload.
func (p *DecisionRequired) Validate() error {
	if p.DecisionRequiredID == "" || p.Question == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"DecisionRequired: decision_required_id and question are required")
	}
	if !p.Authority.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"DecisionRequired: authority %q is not a known decision authority", string(p.Authority))
	}
	return nil
}

// DecisionRecorded closes a durable choice.
//
// The payload carries the compact facts ProjectState needs and references the
// full DecisionRecord by id and digest. The record itself lives in the record
// store; keeping it out of the payload stops the journal from becoming a
// document dump while preserving retrievability (DCI-011).
type DecisionRecorded struct {
	DecisionID   string `json:"decision_id"`
	RecordDigest string `json:"record_digest"`
	Question     string `json:"question"`
	Selected     string `json:"selected"`
	ADRRef       string `json:"adr_ref,omitempty"`
	// ResolvesDecisionRequired names the open question this decision closes,
	// if any. Without it, an answered question would linger in ProjectState.
	ResolvesDecisionRequired string   `json:"resolves_decision_required,omitempty"`
	InvariantChanges         []string `json:"invariant_changes,omitempty"`
}

// Type implements Payload.
func (p *DecisionRecorded) Type() Type { return TypeDecisionRecorded }

// Validate implements Payload.
func (p *DecisionRecorded) Validate() error {
	if p.DecisionID == "" || p.Question == "" || p.Selected == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"DecisionRecorded: decision_id, question and selected are required")
	}
	if p.RecordDigest == "" {
		return errs.New(errs.CategoryInvalidArgument, "DecisionRecorded: record_digest is required")
	}
	return nil
}

// RiskRecorded opens a risk.
type RiskRecorded struct {
	RiskID       string            `json:"risk_id"`
	Severity     protocol.Severity `json:"severity"`
	Statement    string            `json:"statement"`
	EvidenceRefs []string          `json:"evidence_refs,omitempty"`
}

// Type implements Payload.
func (p *RiskRecorded) Type() Type { return TypeRiskRecorded }

// Validate implements Payload.
func (p *RiskRecorded) Validate() error {
	if p.RiskID == "" || p.Statement == "" {
		return errs.New(errs.CategoryInvalidArgument, "RiskRecorded: risk_id and statement are required")
	}
	if !p.Severity.ValidRisk() {
		return errs.New(errs.CategoryInvalidArgument,
			"RiskRecorded: severity %q is not a risk severity", string(p.Severity))
	}
	return nil
}

// RiskResolved closes a risk. The risk leaves current ProjectState but
// remains in history, which is what makes the closure auditable.
type RiskResolved struct {
	RiskID     string `json:"risk_id"`
	Resolution string `json:"resolution"`
}

// Type implements Payload.
func (p *RiskResolved) Type() Type { return TypeRiskResolved }

// Validate implements Payload.
func (p *RiskResolved) Validate() error {
	if p.RiskID == "" || p.Resolution == "" {
		return errs.New(errs.CategoryInvalidArgument, "RiskResolved: risk_id and resolution are required")
	}
	return nil
}

// HealthReportRecorded updates the structural-health summary.
type HealthReportRecorded struct {
	ReportID  string                `json:"report_id"`
	Status    protocol.HealthStatus `json:"status"`
	KnownDebt []string              `json:"known_debt,omitempty"`
}

// Type implements Payload.
func (p *HealthReportRecorded) Type() Type { return TypeHealthReportRecorded }

// Validate implements Payload.
func (p *HealthReportRecorded) Validate() error {
	if p.ReportID == "" {
		return errs.New(errs.CategoryInvalidArgument, "HealthReportRecorded: report_id is required")
	}
	if !p.Status.Valid() {
		return errs.New(errs.CategoryInvalidArgument,
			"HealthReportRecorded: status %q is not a known health status", string(p.Status))
	}
	return nil
}

func init() {
	Register(TypeProjectInitialized, func() Payload { return &ProjectInitialized{} })
	Register(TypeMilestoneStarted, func() Payload { return &MilestoneStarted{} })
	Register(TypeRequirementRecorded, func() Payload { return &RequirementRecorded{} })
	Register(TypeComponentDeclared, func() Payload { return &ComponentDeclared{} })
	Register(TypeDesignCandidateCreated, func() Payload { return &DesignCandidateCreated{} })
	Register(TypeDecisionRequired, func() Payload { return &DecisionRequired{} })
	Register(TypeDecisionRecorded, func() Payload { return &DecisionRecorded{} })
	Register(TypeRiskRecorded, func() Payload { return &RiskRecorded{} })
	Register(TypeRiskResolved, func() Payload { return &RiskResolved{} })
	Register(TypeHealthReportRecorded, func() Payload { return &HealthReportRecorded{} })
}
