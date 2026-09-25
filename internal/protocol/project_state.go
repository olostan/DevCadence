package protocol

import (
	"encoding/json"

	"github.com/olostan/DevCadence/internal/errs"
)

// ProjectState is the canonical compact snapshot described in
// docs/PROJECT_STATE.md and schemas/project-state.schema.json.
//
// It is the principal's normal view of the project (DCI-010). It is not a
// repository index and not a transcript: every field is either a durable
// control-plane fact or a summary that carries evidence references back to
// retrievable provenance (DCI-011).
//
// A ProjectState value is a pure function of the event-journal prefix it was
// reduced from; see internal/state and
// docs/adr/0005-deterministic-project-state-identity.md.
type ProjectState struct {
	SchemaVersion SchemaVersion `json:"schema_version"`
	ProjectID     string        `json:"project_id"`
	// StateRevision is derived from the event high-watermark so that the same
	// journal prefix always yields the same revision identity.
	StateRevision string `json:"state_revision"`
	// GeneratedAt is the occurred_at of the highest applied event, not a
	// wall-clock read, so rebuilding state reproduces it exactly.
	GeneratedAt Timestamp `json:"generated_at"`
	// EventHighWatermark is the decimal journal sequence of the highest
	// applied event. It is nullable only for forward compatibility with
	// states produced outside the journal; this build always sets it.
	EventHighWatermark *string `json:"event_high_watermark"`

	Git        GitState           `json:"git"`
	Product    *ProductState      `json:"product,omitempty"`
	Milestone  MilestoneState     `json:"milestone"`
	Components []ComponentState   `json:"components,omitempty"`
	Modules    []ModuleDefinition `json:"modules,omitempty"`
	Tasks      TaskBuckets        `json:"tasks"`

	ActiveInvariants []string `json:"active_invariants,omitempty"`
	ActiveDecisions  []string `json:"active_decisions,omitempty"`

	Validation ValidationState `json:"validation"`
	Health     *HealthState    `json:"health,omitempty"`

	Risks             []Risk             `json:"risks"`
	DecisionsRequired []DecisionRequired `json:"decisions_required"`

	RecentSemanticChanges []SemanticChange `json:"recent_semantic_changes,omitempty"`
	Capabilities          *Capabilities    `json:"capabilities,omitempty"`
	Discovery             *DiscoveryState  `json:"discovery,omitempty"`
	// Review is the bounded review campaign projection (ADR-0010). Shape
	// only in M1; nothing reduces into it until M6.
	Review *ReviewConvergenceState `json:"review,omitempty"`
}

// DiscoveryState is the compact Day-0 projection of docs/PROJECT_STATE.md §17.
//
// It carries counts and references, never the discovery records themselves:
// ProblemModels, ambiguity ledgers and requirements stay separate durable
// records, and inlining them would defeat the point of a compact state
// (DCI-010).
//
// M1 implements the type and leaves it unset. Deriving it needs the discovery
// event vocabulary, which belongs to the milestone that builds the discovery
// workflow.
type DiscoveryState struct {
	ProblemModelID       *string `json:"problem_model_id,omitempty"`
	ProblemModelRevision *int    `json:"problem_model_revision,omitempty"`
	AmbiguityLedgerID    *string `json:"ambiguity_ledger_id,omitempty"`

	OpenMaterialAmbiguities  int `json:"open_material_ambiguities,omitempty"`
	AwaitingHumanAmbiguities int `json:"awaiting_human_ambiguities,omitempty"`
	ConfirmedRequirements    int `json:"confirmed_requirements,omitempty"`
	ProposedRequirements     int `json:"proposed_requirements,omitempty"`
	// MaterialAssumptionsUnverified is what stops an architecture pass from
	// starting on unverified ground (DCI-005).
	MaterialAssumptionsUnverified int `json:"material_assumptions_unverified,omitempty"`
	ActiveProductDecisions        int `json:"active_product_decisions,omitempty"`

	SpecificationReadinessVerdict *ReadinessVerdict `json:"specification_readiness_verdict,omitempty"`
	SpecificationReadinessRef     *string           `json:"specification_readiness_ref,omitempty"`
	CurrentQuestionRefs           []string          `json:"current_question_refs,omitempty"`
}

// GitState records the repository facts ProjectState depends on.
//
// AcceptedCommit is nullable: a project registered before any change has been
// accepted, and every M1 project (which is repository-independent), has no
// accepted commit yet. Representing that as an explicit null keeps the
// absence visible rather than encoding it as an empty string sentinel.
type GitState struct {
	AcceptedCommit *string `json:"accepted_commit"`
	Branch         *string `json:"branch,omitempty"`
	Dirty          bool    `json:"dirty,omitempty"`
}

// ProductState carries the current product intent by reference.
type ProductState struct {
	VisionRef      *string `json:"vision_ref,omitempty"`
	CurrentOutcome *string `json:"current_outcome,omitempty"`
}

// MilestoneState is the active milestone and its derived progress.
//
// CompletedTasks and TotalTasks are counted from the task projection rather
// than stored, so they cannot drift from the task states they summarise.
type MilestoneState struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Status         string `json:"status,omitempty"`
	CompletedTasks int    `json:"completed_tasks"`
	TotalTasks     int    `json:"total_tasks"`
}

// ContractState describes how settled a component's public contract is.
type ContractState string

const (
	ContractStable     ContractState = "stable"
	ContractEvolving   ContractState = "evolving"
	ContractDeprecated ContractState = "deprecated"
	ContractUnknown    ContractState = "unknown"
)

// Valid reports whether the contract state is defined by the schema.
func (c ContractState) Valid() bool {
	switch c {
	case ContractStable, ContractEvolving, ContractDeprecated, ContractUnknown:
		return true
	}
	return false
}

// ComponentState answers the questions docs/PROJECT_STATE.md §9 asks of a
// component. It deliberately omits file listings: those belong to evidence
// packets and repository indexes.
type ComponentState struct {
	ID             string        `json:"id"`
	Responsibility *string       `json:"responsibility,omitempty"`
	Status         string        `json:"status"`
	ContractState  ContractState `json:"contract_state,omitempty"`
	EvidenceRefs   []string      `json:"evidence_refs,omitempty"`
}

// TaskBuckets is the principal-facing summary of the task graph. The mapping
// from task state to bucket is defined in
// docs/adr/0004-canonical-task-state-machine.md and implemented once, in
// internal/state, so the CLI and the MCP layer cannot drift apart.
type TaskBuckets struct {
	Ready             []string `json:"ready"`
	Running           []string `json:"running"`
	Blocked           []string `json:"blocked"`
	AwaitingPrincipal []string `json:"awaiting_principal"`
}

// ValidationStatus summarises the deterministic health of the accepted commit.
type ValidationStatus string

const (
	ValidationGreen   ValidationStatus = "green"
	ValidationYellow  ValidationStatus = "yellow"
	ValidationRed     ValidationStatus = "red"
	ValidationUnknown ValidationStatus = "unknown"
)

// Valid reports whether the status is defined by the schema.
func (s ValidationStatus) Valid() bool {
	switch s {
	case ValidationGreen, ValidationYellow, ValidationRed, ValidationUnknown:
		return true
	}
	return false
}

// ValidationState summarises deterministic verification.
//
// AcceptedCommitVerified distinguishes "the baseline passed" from "the
// baseline passed on the commit we currently call accepted"; without it a
// stale green result could be read as current (DCI-012).
type ValidationState struct {
	Status                 ValidationStatus `json:"status"`
	AcceptedCommitVerified bool             `json:"accepted_commit_verified,omitempty"`
	LastFullValidationID   *string          `json:"last_full_validation_id,omitempty"`
}

// HealthStatus summarises structural health (docs/REFACTORING_AND_HEALTH.md).
type HealthStatus string

const (
	HealthGreen       HealthStatus = "green"
	HealthWatch       HealthStatus = "watch"
	HealthRefactorDue HealthStatus = "refactor_due"
	HealthUnknown     HealthStatus = "unknown"
)

// Valid reports whether the status is defined by the schema.
func (s HealthStatus) Valid() bool {
	switch s {
	case HealthGreen, HealthWatch, HealthRefactorDue, HealthUnknown:
		return true
	}
	return false
}

// HealthState summarises code and architecture health.
type HealthState struct {
	Status       HealthStatus `json:"status,omitempty"`
	LastReportID *string      `json:"last_report_id,omitempty"`
	KnownDebt    []string     `json:"known_debt,omitempty"`
}

// Severity grades risks and review findings.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// ValidRisk reports whether the severity may grade a risk. The risk schema
// excludes "info": an informational risk is not a risk.
func (s Severity) ValidRisk() bool {
	switch s {
	case SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical:
		return true
	}
	return false
}

// ValidFinding reports whether the severity may grade a review finding.
func (s Severity) ValidFinding() bool {
	return s == SeverityInfo || s.ValidRisk()
}

// Risk is an open, unresolved threat to the project (docs/PROJECT_STATE.md §10).
type Risk struct {
	ID           string   `json:"id"`
	Severity     Severity `json:"severity"`
	Statement    string   `json:"statement"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
}

// DecisionAuthority names who may resolve an open question.
type DecisionAuthority string

const (
	AuthorityPolicy     DecisionAuthority = "policy"
	AuthorityPrincipal  DecisionAuthority = "principal"
	AuthorityConsultant DecisionAuthority = "consultant"
	AuthorityHuman      DecisionAuthority = "human"
)

// Valid reports whether the authority is defined by the schema.
func (a DecisionAuthority) Valid() bool {
	switch a {
	case AuthorityPolicy, AuthorityPrincipal, AuthorityConsultant, AuthorityHuman:
		return true
	}
	return false
}

// DecisionRequired is an open question that blocks some work but should not
// block unrelated work (docs/PROJECT_STATE.md §11).
type DecisionRequired struct {
	ID           string            `json:"id"`
	Question     string            `json:"question"`
	Authority    DecisionAuthority `json:"authority"`
	EvidenceRefs []string          `json:"evidence_refs,omitempty"`
}

// SemanticChange is the compact delta the principal uses to update its mental
// model instead of reading a diff (docs/PROJECT_STATE.md §8).
type SemanticChange struct {
	TaskID  string  `json:"task_id"`
	Commit  *string `json:"commit,omitempty"`
	Summary string  `json:"summary"`
}

// Capabilities describes the cognition currently available to the project.
//
// It is an explicit type rather than a free-form object because durable
// records may not use untyped maps (ENGINEERING_STANDARDS.md §4).
//
// The shape has two generations, which is deliberate rather than untidy. M1
// reserved `local_models` and `consultants` when the architecture assumed a
// local model plus a set of consultant subscriptions. ADR-0011 replaced that
// model: cognition is now a set of capability-routed endpoints that may be
// local runtimes, authenticated CLIs or remote APIs, and "consultant" became a
// role played by an endpoint rather than a separate universe. The `cognition`
// field below is the current projection.
//
// The M1 fields are retained, deprecated and never written by this build.
// Deleting them would make every historical ProjectState document
// unreadable under strict decoding (DCI-092, DCI-093), which the compatibility
// policy forbids; keeping them as read-only legacy shape costs two fields and
// preserves the ability to inspect old trajectories. See
// docs/adr/0013-environment-intelligence-and-cognition-contracts.md §4.
type Capabilities struct {
	// Cognition is the compact projection of discovered cognition capability.
	Cognition *CognitionCapabilities `json:"cognition,omitempty"`

	// LocalModels is the deprecated M1 representation. Readable for
	// historical documents; never written.
	LocalModels []ModelCapability `json:"local_models,omitempty"`
	// Consultants is the deprecated M1 representation. Readable for
	// historical documents; never written.
	Consultants []ConsultantCapability `json:"consultants,omitempty"`
}

// ModelCapability reports a configured local model profile.
//
// Deprecated: superseded by CognitionEndpointSummary (ADR-0011, ADR-0013). It
// remains part of the contract so documents written before M3A stay readable.
type ModelCapability struct {
	Profile   string `json:"profile"`
	Runtime   string `json:"runtime,omitempty"`
	Available bool   `json:"available"`
}

// ConsultantCapability reports a configured frontier consultant adapter.
//
// Deprecated: superseded by CognitionEndpointSummary (ADR-0011, ADR-0013).
// Consultant selection is an M6 policy over discovered endpoints, not a
// separate capability list.
type ConsultantCapability struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
}

// CognitionCapabilities is the compact, project-facing projection of the
// machine's cognition capability.
//
// It is a projection, not a copy. The full MachineCapabilityProfile carries
// CPU features, device nodes, probe signals and measurements; none of that
// belongs in the principal's normal view of a project (DCI-010), and putting
// it here would make ProjectState grow with every probe. What a project-level
// reader needs is: can cognition happen at all, through which endpoints, at
// what cost and exposure, and how stale is that answer.
//
// Freshness is explicit for a reason. Machine facts are "runtime current"
// (docs/PROJECT_STATE.md §13): a durable project record that repeats a
// month-old endpoint health as though it were current project truth would be
// worse than carrying nothing. MachineFingerprint plus ObservedAt let a reader
// tell whether the projection still describes the machine in front of it.
type CognitionCapabilities struct {
	Assessment CognitionAssessment `json:"assessment"`
	// ObservedAt is when the underlying profile was observed, not when this
	// state was reduced.
	ObservedAt *Timestamp `json:"observed_at,omitempty"`
	// MachineFingerprint identifies the machine the projection describes.
	MachineFingerprint *string `json:"machine_fingerprint,omitempty"`
	// ProfileRef points at the full MachineCapabilityProfile when one was
	// retained, so detail stays retrievable without being inlined (DCI-011).
	ProfileRef *string `json:"profile_ref,omitempty"`

	Endpoints []CognitionEndpointSummary `json:"endpoints,omitempty"`
	// Limitations states what this configuration cannot do, sorted.
	Limitations []string `json:"limitations,omitempty"`
}

// CognitionEndpointSummary is one endpoint reduced to what a project-level
// decision needs.
//
// Capability grades are deliberately absent: a grade without its provenance
// invites exactly the unevidenced claim DCI-012 forbids, and the provenance
// belongs with the full profile. A reader that needs grades reads the profile.
type CognitionEndpointSummary struct {
	ID        string         `json:"id"`
	Kind      EndpointKind   `json:"kind"`
	Locality  Locality       `json:"locality"`
	Health    EndpointHealth `json:"health"`
	Auth      AuthStatus     `json:"auth_status"`
	CostClass CostClass      `json:"cost_class"`
	// RequiredSourceExposure is what the endpoint needs, so a privacy
	// question can be answered from the projection alone.
	RequiredSourceExposure SourceExposure `json:"required_source_exposure"`
	// AccelerationVerified is true only for a local endpoint whose non-CPU
	// backend was empirically verified (DCI-106).
	AccelerationVerified bool `json:"acceleration_verified,omitempty"`
	// AccelerationBackend names the backend the state refers to, so
	// "verified: false" can be distinguished from "no backend considered".
	AccelerationBackend *BackendKind `json:"acceleration_backend,omitempty"`
}

// Validate checks the projection's enumerations.
func (c *CognitionCapabilities) Validate() error {
	const kind = "ProjectState"
	if !c.Assessment.Valid() {
		return enumError(kind, "capabilities.cognition.assessment", string(c.Assessment),
			"ready", "ready_with_reduced_capability", "model_cognition_unavailable",
			"partially_ready", "unknown")
	}
	seen := make(map[string]bool, len(c.Endpoints))
	for _, e := range c.Endpoints {
		if err := e.Validate(); err != nil {
			return err
		}
		if seen[e.ID] {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: capabilities.cognition.endpoints[] lists %q twice", kind, e.ID)
		}
		seen[e.ID] = true
	}
	return nil
}

// Validate checks the endpoint summary's enumerations and internal
// consistency. It is the single canonical validator every caller
// (ProjectState, DoctorReport, ResourceInventory) uses, so a summary
// cannot be valid in one context and silently malformed in another
// (independent-review follow-up on WP-M3B-5, finding 4b: Go endpoint
// validation was previously much weaker than the JSON Schema twin).
func (e CognitionEndpointSummary) Validate() error {
	const kind = "CognitionEndpointSummary"
	if err := requireNonEmpty(kind, "id", e.ID); err != nil {
		return err
	}
	if !e.Kind.Valid() {
		return enumError(kind, "kind", string(e.Kind),
			"local_runtime", "authenticated_cli", "remote_api")
	}
	if !e.Locality.Valid() {
		return enumError(kind, "locality", string(e.Locality),
			"local", "remote_inference_local_tools", "remote")
	}
	if !e.Health.Valid() {
		return enumError(kind, "health", string(e.Health),
			"not_installed", "installed", "not_configured", "unhealthy",
			"unverified", "ready", "unsupported", "unknown")
	}
	if !e.Auth.Valid() {
		return enumError(kind, "auth_status", string(e.Auth),
			"not_applicable", "authenticated", "unauthenticated", "expired", "unknown", "error")
	}
	if !e.CostClass.Valid() {
		return enumError(kind, "cost_class", string(e.CostClass),
			"local_compute", "subscription_included", "remote_economy",
			"remote_strong", "frontier_expensive", "unknown")
	}
	if !e.RequiredSourceExposure.Valid() {
		return enumError(kind, "required_source_exposure",
			string(e.RequiredSourceExposure),
			"local_only", "semantic_evidence_only", "focused_snippets",
			"selected_files", "tool_mediated_worktree", "unrestricted_authorized")
	}
	if e.AccelerationBackend != nil && !e.AccelerationBackend.Valid() {
		return enumError(kind, "acceleration_backend",
			string(*e.AccelerationBackend), "cpu", "metal", "cuda", "rocm", "vulkan", "unknown")
	}
	// A remote endpoint cannot have verified local acceleration; the
	// inference is not happening here.
	if e.AccelerationVerified && e.Locality != LocalityLocal {
		return errs.New(errs.CategoryInvalidArgument,
			"%s: endpoint %q is %s and cannot report verified local acceleration",
			kind, e.ID, e.Locality)
	}
	return nil
}

// RecordKind implements Record.
func (s *ProjectState) RecordKind() string { return "ProjectState" }

// RecordID implements Record.
func (s *ProjectState) RecordID() string { return s.StateRevision }

// SchemaVer implements Record.
func (s *ProjectState) SchemaVer() SchemaVersion { return s.SchemaVersion }

// Validate enforces the schema's required fields and enumerations plus the
// semantic constraints JSON Schema cannot express.
func (s *ProjectState) Validate() error {
	const kind = "ProjectState"
	if err := s.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "project_id", s.ProjectID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "state_revision", s.StateRevision); err != nil {
		return err
	}
	if s.Git.AcceptedCommit != nil && len(*s.Git.AcceptedCommit) < 7 {
		return errs.New(errs.CategoryInvalidArgument,
			"%s: git.accepted_commit must be at least 7 characters when present", kind)
	}
	if err := requireNonEmpty(kind, "milestone.id", s.Milestone.ID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "milestone.title", s.Milestone.Title); err != nil {
		return err
	}
	if s.Milestone.CompletedTasks < 0 || s.Milestone.TotalTasks < 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: milestone task counts must not be negative", kind)
	}
	if s.Milestone.CompletedTasks > s.Milestone.TotalTasks {
		return errs.New(errs.CategoryIntegrity,
			"%s: milestone reports %d completed of %d total tasks",
			kind, s.Milestone.CompletedTasks, s.Milestone.TotalTasks)
	}
	for _, c := range s.Components {
		if err := requireNonEmpty(kind, "components[].id", c.ID); err != nil {
			return err
		}
		if err := requireNonEmpty(kind, "components[].status", c.Status); err != nil {
			return err
		}
		if c.ContractState != "" && !c.ContractState.Valid() {
			return enumError(kind, "components[].contract_state", string(c.ContractState),
				"stable", "evolving", "deprecated", "unknown")
		}
	}
	if !s.Validation.Status.Valid() {
		return enumError(kind, "validation.status", string(s.Validation.Status), "green", "yellow", "red", "unknown")
	}
	if s.Health != nil && s.Health.Status != "" && !s.Health.Status.Valid() {
		return enumError(kind, "health.status", string(s.Health.Status), "green", "watch", "refactor_due", "unknown")
	}
	for _, r := range s.Risks {
		if err := requireNonEmpty(kind, "risks[].id", r.ID); err != nil {
			return err
		}
		if !r.Severity.ValidRisk() {
			return enumError(kind, "risks[].severity", string(r.Severity), "low", "medium", "high", "critical")
		}
		if err := requireNonEmpty(kind, "risks[].statement", r.Statement); err != nil {
			return err
		}
	}
	for _, d := range s.DecisionsRequired {
		if err := requireNonEmpty(kind, "decisions_required[].id", d.ID); err != nil {
			return err
		}
		if err := requireNonEmpty(kind, "decisions_required[].question", d.Question); err != nil {
			return err
		}
		if !d.Authority.Valid() {
			return enumError(kind, "decisions_required[].authority", string(d.Authority),
				"policy", "principal", "consultant", "human")
		}
	}
	if s.Discovery != nil {
		if s.Discovery.ProblemModelRevision != nil && *s.Discovery.ProblemModelRevision < 1 {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: discovery.problem_model_revision must be >= 1 when present", kind)
		}
		if s.Discovery.SpecificationReadinessVerdict != nil && !s.Discovery.SpecificationReadinessVerdict.Valid() {
			return enumError(kind, "discovery.specification_readiness_verdict",
				string(*s.Discovery.SpecificationReadinessVerdict),
				"not_ready", "ready_for_architecture", "ready_with_explicit_risks")
		}
		for name, count := range map[string]int{
			"open_material_ambiguities":       s.Discovery.OpenMaterialAmbiguities,
			"awaiting_human_ambiguities":      s.Discovery.AwaitingHumanAmbiguities,
			"confirmed_requirements":          s.Discovery.ConfirmedRequirements,
			"proposed_requirements":           s.Discovery.ProposedRequirements,
			"material_assumptions_unverified": s.Discovery.MaterialAssumptionsUnverified,
			"active_product_decisions":        s.Discovery.ActiveProductDecisions,
		} {
			if count < 0 {
				return errs.New(errs.CategoryInvalidArgument,
					"%s: discovery.%s must not be negative", kind, name)
			}
		}
	}
	if s.Review != nil {
		if err := s.Review.Validate(); err != nil {
			return err
		}
	}
	if s.Capabilities != nil && s.Capabilities.Cognition != nil {
		if err := s.Capabilities.Cognition.Validate(); err != nil {
			return err
		}
	}
	for _, c := range s.RecentSemanticChanges {
		if err := requireNonEmpty(kind, "recent_semantic_changes[].task_id", c.TaskID); err != nil {
			return err
		}
		if err := requireNonEmpty(kind, "recent_semantic_changes[].summary", c.Summary); err != nil {
			return err
		}
	}
	return nil
}

// MarshalJSON guarantees that the arrays the schema marks required are
// emitted as `[]` rather than `null`.
//
// A nil Go slice and an empty JSON array mean the same thing here ("no open
// risks"), but `null` violates the schema. Normalising on the write path
// keeps every producer of a ProjectState — reducer, CLI, fixtures — from
// having to remember this.
func (s ProjectState) MarshalJSON() ([]byte, error) {
	type alias ProjectState // avoids recursing into this method
	out := alias(s)
	out.Tasks.Ready = orEmpty(out.Tasks.Ready)
	out.Tasks.Running = orEmpty(out.Tasks.Running)
	out.Tasks.Blocked = orEmpty(out.Tasks.Blocked)
	out.Tasks.AwaitingPrincipal = orEmpty(out.Tasks.AwaitingPrincipal)
	if out.Risks == nil {
		out.Risks = []Risk{}
	}
	if out.DecisionsRequired == nil {
		out.DecisionsRequired = []DecisionRequired{}
	}
	return json.Marshal(out)
}

// orEmpty replaces a nil slice with an empty one.
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// ReviewPhase is where a bounded review campaign currently stands
// (ADR-0010, DCI-047).
type ReviewPhase string

const (
	ReviewPhaseBroadReview         ReviewPhase = "broad_review"
	ReviewPhaseAdjudication        ReviewPhase = "adjudication"
	ReviewPhaseRepair              ReviewPhase = "repair"
	ReviewPhaseFocusedRevalidation ReviewPhase = "focused_revalidation"
	ReviewPhaseClosureReview       ReviewPhase = "closure_review"
	ReviewPhaseFrozen              ReviewPhase = "frozen"
	ReviewPhaseReopened            ReviewPhase = "reopened"
	ReviewPhaseEscalated           ReviewPhase = "escalated"
)

// Valid reports whether the phase is defined by the schema.
func (p ReviewPhase) Valid() bool {
	switch p {
	case ReviewPhaseBroadReview, ReviewPhaseAdjudication, ReviewPhaseRepair,
		ReviewPhaseFocusedRevalidation, ReviewPhaseClosureReview,
		ReviewPhaseFrozen, ReviewPhaseReopened, ReviewPhaseEscalated:
		return true
	}
	return false
}

// ReviewConvergenceState is the compact projection of the active review
// campaign (docs/REVIEW_AND_CONVERGENCE.md, ADR-0010).
//
// M1 defines the shape only. No event reduces into it and the control plane
// never populates it: campaigns, finding dispositions and closure decisions
// are M6. The type exists here because `project-state.schema.json` publishes
// the field, and a schema property with no counterpart on its Go twin would
// mean strict decoding refuses a document the schema calls valid (DCI-092).
//
// Only counts and references belong here. Reviewer transcripts, consultant
// conversations and repair histories stay evidence artifacts retrievable by
// reference — the compact-state rule of docs/PROJECT_STATE.md §1 applies to a
// campaign exactly as it does to a task.
type ReviewConvergenceState struct {
	CampaignID      *string      `json:"campaign_id,omitempty"`
	CandidateCommit *string      `json:"candidate_commit,omitempty"`
	Phase           *ReviewPhase `json:"phase,omitempty"`

	RepairRound     int `json:"repair_round,omitempty"`
	MaxRepairRounds int `json:"max_repair_rounds,omitempty"`

	RequiredDimensions  []string `json:"required_dimensions,omitempty"`
	CompletedDimensions []string `json:"completed_dimensions,omitempty"`

	BlockingFindingsOpen          int `json:"blocking_findings_open,omitempty"`
	MaterialFindingsUnadjudicated int `json:"material_findings_unadjudicated,omitempty"`
	FixNowFindings                int `json:"fix_now_findings,omitempty"`
	DeferredFindings              int `json:"deferred_findings,omitempty"`
	RejectedFindings              int `json:"rejected_findings,omitempty"`
	OpportunisticFindings         int `json:"opportunistic_findings,omitempty"`

	ClosureThreshold   *string  `json:"closure_threshold,omitempty"`
	ResidualRiskRefs   []string `json:"residual_risk_refs,omitempty"`
	ClosureDecisionRef *string  `json:"closure_decision_ref,omitempty"`
}

// Validate checks the enumerations the schema constrains. It is called from
// ProjectState.Validate so that a hand-authored state carrying a review block
// cannot name a phase the campaign model does not define.
func (r *ReviewConvergenceState) Validate() error {
	const kind = "ProjectState"
	if r.Phase != nil && !(*r.Phase).Valid() {
		return enumError(kind, "review.phase", string(*r.Phase),
			"broad_review", "adjudication", "repair", "focused_revalidation",
			"closure_review", "frozen", "reopened", "escalated")
	}
	if r.ClosureThreshold != nil && *r.ClosureThreshold != "high" && *r.ClosureThreshold != "critical" {
		return enumError(kind, "review.closure_threshold", *r.ClosureThreshold, "high", "critical")
	}
	return nil
}
