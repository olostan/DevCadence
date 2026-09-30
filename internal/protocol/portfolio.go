package protocol

import "github.com/olostan/DevCadence/internal/errs"

// RoleBinding maps an engineering role to an endpoint, channel, and budget pool (ADR-0018 §1).
type RoleBinding struct {
	Role             string `json:"role"`
	EndpointID       string `json:"endpoint_id"`
	ChannelID        string `json:"channel_id"`
	BudgetPoolID     string `json:"budget_pool_id"`
	ContextProfileID string `json:"context_profile_id"`
}

// Validate checks RoleBinding fields.
func (r RoleBinding) Validate() error {
	const kind = "RoleBinding"
	if err := requireNonEmpty(kind, "role", r.Role); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "endpoint_id", r.EndpointID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "channel_id", r.ChannelID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "budget_pool_id", r.BudgetPoolID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "context_profile_id", r.ContextProfileID); err != nil {
		return err
	}
	return nil
}

// CognitionPortfolio is the canonical routing configuration (ADR-0018 §7).
type CognitionPortfolio struct {
	SchemaVersion     SchemaVersion   `json:"schema_version"`
	PortfolioID       string          `json:"portfolio_id"`
	Revision          int             `json:"revision"`
	CreatedAt         string          `json:"created_at"`
	Channels          []AccessChannel `json:"channels"`
	RoleBindings      []RoleBinding   `json:"role_bindings"`
	BudgetPools       []BudgetPool    `json:"budget_pools"`
	MaxSourceExposure SourceExposure  `json:"max_source_exposure"`
}

// RecordKind implements Record.
func (c *CognitionPortfolio) RecordKind() string { return "CognitionPortfolio" }

// RecordID implements Record.
func (c *CognitionPortfolio) RecordID() string { return c.PortfolioID }

// SchemaVer implements Record.
func (c *CognitionPortfolio) SchemaVer() SchemaVersion { return c.SchemaVersion }

// Validate enforces CognitionPortfolio constraints per ADR-0018.
func (c *CognitionPortfolio) Validate() error {
	const kind = "CognitionPortfolio"
	if err := c.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "portfolio_id", c.PortfolioID); err != nil {
		return err
	}
	if c.Revision < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: revision must be >= 1, got %d", kind, c.Revision)
	}
	if err := requireNonEmpty(kind, "created_at", c.CreatedAt); err != nil {
		return err
	}
	if !c.MaxSourceExposure.Valid() {
		return enumError(kind, "max_source_exposure", string(c.MaxSourceExposure),
			string(ExposureLocalOnly), string(ExposureSemanticEvidenceOnly),
			string(ExposureFocusedSnippets), string(ExposureSelectedFiles),
			string(ExposureToolMediatedWorktree), string(ExposureUnrestrictedAuthorized))
	}
	for i, ch := range c.Channels {
		if err := ch.Validate(); err != nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: channels[%d]: %v", kind, i, err)
		}
	}
	for i, rb := range c.RoleBindings {
		if err := rb.Validate(); err != nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: role_bindings[%d]: %v", kind, i, err)
		}
	}
	for i, bp := range c.BudgetPools {
		if err := bp.Validate(); err != nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: budget_pools[%d]: %v", kind, i, err)
		}
	}
	return nil
}

// PortfolioRecommendation is an AI-synthesized or heuristic portfolio proposal (ADR-0018 §5, §6).
type PortfolioRecommendation struct {
	SchemaVersion          SchemaVersion      `json:"schema_version"`
	RecommendationID       string             `json:"recommendation_id"`
	InventoryDigest        string             `json:"inventory_digest"`
	SynthesizedAt          string             `json:"synthesized_at"`
	RecommendedPortfolio   CognitionPortfolio `json:"recommended_portfolio"`
	Rationale              string             `json:"rationale"`
	ExplanatoryDiagnostics []string           `json:"explanatory_diagnostics"`
	CapabilityProvenance   []string           `json:"capability_provenance"`
}

// RecordKind implements Record.
func (p *PortfolioRecommendation) RecordKind() string { return "PortfolioRecommendation" }

// RecordID implements Record.
func (p *PortfolioRecommendation) RecordID() string { return p.RecommendationID }

// SchemaVer implements Record.
func (p *PortfolioRecommendation) SchemaVer() SchemaVersion { return p.SchemaVersion }

// Validate enforces PortfolioRecommendation constraints.
func (p *PortfolioRecommendation) Validate() error {
	const kind = "PortfolioRecommendation"
	if err := p.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "recommendation_id", p.RecommendationID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "inventory_digest", p.InventoryDigest); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "synthesized_at", p.SynthesizedAt); err != nil {
		return err
	}
	if err := p.RecommendedPortfolio.Validate(); err != nil {
		return errs.New(errs.CategoryInvalidArgument, "%s: recommended_portfolio: %v", kind, err)
	}
	if err := requireNonEmpty(kind, "rationale", p.Rationale); err != nil {
		return err
	}
	return nil
}

// WorkflowTopologyKind describes the task-specific execution flow (ADR-0018 §8).
type WorkflowTopologyKind string

const (
	TopologySinglePass            WorkflowTopologyKind = "single_pass"
	TopologyIterativeEscalation   WorkflowTopologyKind = "iterative_escalation"
	TopologyDualIndependentReview WorkflowTopologyKind = "dual_independent_review"
	TopologyDeterministicOnly     WorkflowTopologyKind = "deterministic_only"
)

// Valid reports whether the topology kind is known.
func (t WorkflowTopologyKind) Valid() bool {
	switch t {
	case TopologySinglePass, TopologyIterativeEscalation, TopologyDualIndependentReview, TopologyDeterministicOnly:
		return true
	}
	return false
}

// WorkflowStage defines one cognitive pass in a workflow plan.
type WorkflowStage struct {
	StageID        string   `json:"stage_id"`
	Role           string   `json:"role"`
	Order          int      `json:"order"`
	DependsOn      []string `json:"depends_on,omitempty"`
	BudgetPoolID   string   `json:"budget_pool_id"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

// Validate checks WorkflowStage fields.
func (w WorkflowStage) Validate() error {
	const kind = "WorkflowStage"
	if err := requireNonEmpty(kind, "stage_id", w.StageID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "role", w.Role); err != nil {
		return err
	}
	if w.Order < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: order must be >= 1, got %d", kind, w.Order)
	}
	if err := requireNonEmpty(kind, "budget_pool_id", w.BudgetPoolID); err != nil {
		return err
	}
	if w.TimeoutSeconds < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: timeout_seconds must be >= 1, got %d", kind, w.TimeoutSeconds)
	}
	return nil
}

// WorkflowPlan describes the task-specific workflow topology (ADR-0018 §8).
type WorkflowPlan struct {
	SchemaVersion SchemaVersion        `json:"schema_version"`
	PlanID        string               `json:"plan_id"`
	TaskID        string               `json:"task_id"`
	WorkPackageID string               `json:"work_package_id"`
	Topology      WorkflowTopologyKind `json:"topology"`
	Stages        []WorkflowStage      `json:"stages"`
}

// RecordKind implements Record.
func (w *WorkflowPlan) RecordKind() string { return "WorkflowPlan" }

// RecordID implements Record.
func (w *WorkflowPlan) RecordID() string { return w.PlanID }

// SchemaVer implements Record.
func (w *WorkflowPlan) SchemaVer() SchemaVersion { return w.SchemaVersion }

// Validate enforces WorkflowPlan constraints.
func (w *WorkflowPlan) Validate() error {
	const kind = "WorkflowPlan"
	if err := w.SchemaVersion.Validate(kind); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "plan_id", w.PlanID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "task_id", w.TaskID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "work_package_id", w.WorkPackageID); err != nil {
		return err
	}
	if !w.Topology.Valid() {
		return enumError(kind, "topology", string(w.Topology),
			string(TopologySinglePass), string(TopologyIterativeEscalation),
			string(TopologyDualIndependentReview), string(TopologyDeterministicOnly))
	}
	for i, stage := range w.Stages {
		if err := stage.Validate(); err != nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: stages[%d]: %v", kind, i, err)
		}
	}
	return nil
}
