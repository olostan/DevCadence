package protocol

import "github.com/olostan/DevCadence/internal/errs"

// RoleBinding maps an engineering role to an endpoint, channel, and budget pool (ADR-0018 §1, FR-062).
type RoleBinding struct {
	Role                string   `json:"role"`
	EndpointID          string   `json:"endpoint_id"`
	ChannelID           string   `json:"channel_id"`
	BudgetPoolID        string   `json:"budget_pool_id"`
	ContextProfileID    string   `json:"context_profile_id"`
	Priority            int      `json:"priority"`
	FallbackEndpointIDs []string `json:"fallback_endpoint_ids,omitempty"`
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
	if r.Priority < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: priority must be >= 1, got %d", kind, r.Priority)
	}
	for i, fb := range r.FallbackEndpointIDs {
		if fb == "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: fallback_endpoint_ids[%d] cannot be empty", kind, i)
		}
		if fb == r.EndpointID {
			return errs.New(errs.CategoryInvalidArgument, "%s: fallback_endpoint_ids[%d] %q matches primary endpoint_id", kind, i, fb)
		}
	}
	return nil
}

// CognitionPortfolio is the canonical routing configuration (ADR-0018 §7, FR-062).
type CognitionPortfolio struct {
	SchemaVersion       SchemaVersion    `json:"schema_version"`
	PortfolioID         string           `json:"portfolio_id"`
	Revision            int              `json:"revision"`
	CreatedAt           string           `json:"created_at"`
	Channels            []AccessChannel  `json:"channels"`
	RoleBindings        []RoleBinding    `json:"role_bindings"`
	BudgetPools         []BudgetPool     `json:"budget_pools"`
	MaxSourceExposure   SourceExposure   `json:"max_source_exposure"`
	ExcludedEndpointIDs []string         `json:"excluded_endpoint_ids,omitempty"`
	BudgetReservations  map[string]int64 `json:"budget_reservations,omitempty"`
}

// RecordKind implements Record.
func (c *CognitionPortfolio) RecordKind() string { return "CognitionPortfolio" }

// RecordID implements Record.
func (c *CognitionPortfolio) RecordID() string { return c.PortfolioID }

// SchemaVer implements Record.
func (c *CognitionPortfolio) SchemaVer() SchemaVersion { return c.SchemaVersion }

// Validate enforces CognitionPortfolio constraints per ADR-0018 and FR-062.
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

	channelMap := make(map[string]AccessChannel, len(c.Channels))
	for i, ch := range c.Channels {
		if err := ch.Validate(); err != nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: channels[%d]: %v", kind, i, err)
		}
		if _, exists := channelMap[ch.ChannelID]; exists {
			return errs.New(errs.CategoryInvalidArgument, "%s: duplicate channel_id %q", kind, ch.ChannelID)
		}
		channelMap[ch.ChannelID] = ch
	}

	poolMap := make(map[string]BudgetPool, len(c.BudgetPools))
	for i, bp := range c.BudgetPools {
		if err := bp.Validate(); err != nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: budget_pools[%d]: %v", kind, i, err)
		}
		if _, exists := poolMap[bp.PoolID]; exists {
			return errs.New(errs.CategoryInvalidArgument, "%s: duplicate budget_pool_id %q", kind, bp.PoolID)
		}
		poolMap[bp.PoolID] = bp
	}

	excludedMap := make(map[string]bool, len(c.ExcludedEndpointIDs))
	for _, epID := range c.ExcludedEndpointIDs {
		excludedMap[epID] = true
	}

	endpointChannels := make(map[string][]AccessChannel)
	for _, ch := range c.Channels {
		endpointChannels[ch.EndpointID] = append(endpointChannels[ch.EndpointID], ch)
	}

	endpointPools := make(map[string][]BudgetPool)
	for _, b := range c.RoleBindings {
		if pool, ok := poolMap[b.BudgetPoolID]; ok {
			endpointPools[b.EndpointID] = append(endpointPools[b.EndpointID], pool)
		}
	}

	type rolePriorityKey struct {
		role     string
		priority int
	}
	seenRolePriority := make(map[rolePriorityKey]int, len(c.RoleBindings))

	for i, rb := range c.RoleBindings {
		if err := rb.Validate(); err != nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: role_bindings[%d]: %v", kind, i, err)
		}
		ch, ok := channelMap[rb.ChannelID]
		if !ok {
			return errs.New(errs.CategoryInvalidArgument, "%s: role_bindings[%d] references non-existent channel_id %q", kind, i, rb.ChannelID)
		}
		if rb.EndpointID != ch.EndpointID {
			return errs.New(errs.CategoryInvalidArgument, "%s: role_bindings[%d] endpoint_id %q does not match channel endpoint_id %q", kind, i, rb.EndpointID, ch.EndpointID)
		}
		primaryPool, ok := poolMap[rb.BudgetPoolID]
		if !ok {
			return errs.New(errs.CategoryInvalidArgument, "%s: role_bindings[%d] references non-existent budget_pool_id %q", kind, i, rb.BudgetPoolID)
		}
		if excludedMap[rb.EndpointID] {
			return errs.New(errs.CategoryInvalidArgument, "%s: role_bindings[%d] uses excluded endpoint_id %q", kind, i, rb.EndpointID)
		}
		rpKey := rolePriorityKey{role: rb.Role, priority: rb.Priority}
		if firstIndex, dup := seenRolePriority[rpKey]; dup {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: role_bindings[%d] duplicate priority %d for role %q (already assigned in role_bindings[%d])",
				kind, i, rb.Priority, rb.Role, firstIndex)
		}
		seenRolePriority[rpKey] = i

		for _, fb := range rb.FallbackEndpointIDs {
			if excludedMap[fb] {
				return errs.New(errs.CategoryInvalidArgument, "%s: role_bindings[%d] fallback_endpoint_id %q is excluded", kind, i, fb)
			}
			if len(endpointChannels[fb]) == 0 {
				return errs.New(errs.CategoryInvalidArgument, "%s: role_bindings[%d] fallback_endpoint_id %q has no access channel in portfolio channels", kind, i, fb)
			}
			fbPools, covered := endpointPools[fb]
			if !covered || len(fbPools) == 0 {
				return errs.New(errs.CategoryInvalidArgument, "%s: role_bindings[%d] fallback_endpoint_id %q is not covered by any budget pool in the portfolio", kind, i, fb)
			}
			for _, fbPool := range fbPools {
				if fbPool.Regime == RegimeMeteredAPI && !primaryPool.FallbackAllowedToMetered {
					return errs.New(errs.CategoryInvalidArgument,
						"%s: role_bindings[%d] fallback_endpoint_id %q is bound to metered budget pool %q, but primary budget pool %q forbids fallback to metered (ADR-0018 §9, DCI-104)",
						kind, i, fb, fbPool.PoolID, primaryPool.PoolID)
				}
			}
		}
	}

	for poolID, amount := range c.BudgetReservations {
		pool, ok := poolMap[poolID]
		if !ok {
			return errs.New(errs.CategoryInvalidArgument, "%s: budget_reservations references non-existent budget_pool_id %q", kind, poolID)
		}
		if amount < 0 {
			return errs.New(errs.CategoryInvalidArgument, "%s: budget_reservation for %q cannot be negative (%d)", kind, poolID, amount)
		}
		if amount > pool.HardLimit {
			return errs.New(errs.CategoryInvalidArgument, "%s: budget_reservation for %q (%d) exceeds pool hard_limit (%d)", kind, poolID, amount, pool.HardLimit)
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

// StageKind distinguishes cognitive from deterministic stages in a workflow (ADR-0018 §8).
type StageKind string

const (
	StageKindCognition     StageKind = "cognition"
	StageKindDeterministic StageKind = "deterministic"
)

// Valid reports whether the stage kind is known.
func (k StageKind) Valid() bool {
	switch k {
	case StageKindCognition, StageKindDeterministic:
		return true
	}
	return false
}

// WorkflowStage defines one cognitive or deterministic pass in a workflow plan.
type WorkflowStage struct {
	StageID        string    `json:"stage_id"`
	Role           string    `json:"role"`
	Kind           StageKind `json:"kind"`
	IsReview       bool      `json:"is_review,omitempty"`
	Order          int       `json:"order"`
	DependsOn      []string  `json:"depends_on,omitempty"`
	BudgetPoolID   string    `json:"budget_pool_id"`
	TimeoutSeconds int       `json:"timeout_seconds"`
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
	if !w.Kind.Valid() {
		return enumError(kind, "kind", string(w.Kind),
			string(StageKindCognition), string(StageKindDeterministic))
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
	if err := requireMinItems(kind, "stages", len(w.Stages), 1); err != nil {
		return err
	}

	stageIDs := make(map[string]WorkflowStage, len(w.Stages))
	orders := make(map[int]string, len(w.Stages))
	reviewStageCount := 0
	deterministicStageCount := 0

	for i, stage := range w.Stages {
		if err := stage.Validate(); err != nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: stages[%d]: %v", kind, i, err)
		}
		if _, exists := stageIDs[stage.StageID]; exists {
			return errs.New(errs.CategoryInvalidArgument, "%s: duplicate stage_id %q", kind, stage.StageID)
		}
		stageIDs[stage.StageID] = stage

		if existingID, exists := orders[stage.Order]; exists {
			return errs.New(errs.CategoryInvalidArgument, "%s: duplicate stage order %d (stages %q and %q)", kind, stage.Order, existingID, stage.StageID)
		}
		orders[stage.Order] = stage.StageID

		if stage.IsReview {
			reviewStageCount++
		}
		if stage.Kind == StageKindDeterministic {
			deterministicStageCount++
		}
	}

	// Validate DAG dependencies: forward-only, no self-deps, references valid stages
	for _, stage := range w.Stages {
		for _, depID := range stage.DependsOn {
			if depID == stage.StageID {
				return errs.New(errs.CategoryInvalidArgument, "%s: stage %q cannot depend on itself", kind, stage.StageID)
			}
			depStage, exists := stageIDs[depID]
			if !exists {
				return errs.New(errs.CategoryInvalidArgument, "%s: stage %q depends on non-existent stage %q", kind, stage.StageID, depID)
			}
			if depStage.Order >= stage.Order {
				return errs.New(errs.CategoryInvalidArgument, "%s: stage %q (order %d) cannot depend on later or equal stage %q (order %d)", kind, stage.StageID, stage.Order, depID, depStage.Order)
			}
		}
	}

	// Topology compatibility checks
	switch w.Topology {
	case TopologyDualIndependentReview:
		if reviewStageCount < 2 {
			return errs.New(errs.CategoryInvalidArgument, "%s: topology %q requires at least 2 review stages (with is_review: true), got %d", kind, w.Topology, reviewStageCount)
		}
	case TopologyDeterministicOnly:
		if deterministicStageCount != len(w.Stages) {
			return errs.New(errs.CategoryInvalidArgument, "%s: topology %q permits only deterministic stages (kind: %q), but found %d non-deterministic stages", kind, w.Topology, StageKindDeterministic, len(w.Stages)-deterministicStageCount)
		}
	}

	return nil
}
