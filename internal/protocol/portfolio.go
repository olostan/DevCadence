package protocol

import "github.com/olostan/DevCadence/internal/errs"

// FallbackBinding defines an explicit, routable fallback path for a role binding (ADR-0018 §1, §9).
type FallbackBinding struct {
	EndpointID       string `json:"endpoint_id"`
	ChannelID        string `json:"channel_id"`
	BudgetPoolID     string `json:"budget_pool_id"`
	ContextProfileID string `json:"context_profile_id"`
}

// Validate checks FallbackBinding fields.
func (f FallbackBinding) Validate() error {
	const kind = "FallbackBinding"
	if err := requireNonEmpty(kind, "endpoint_id", f.EndpointID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "channel_id", f.ChannelID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "budget_pool_id", f.BudgetPoolID); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "context_profile_id", f.ContextProfileID); err != nil {
		return err
	}
	return nil
}

// RoleBinding maps an engineering role to an endpoint, channel, and budget pool (ADR-0018 §1, FR-062).
type RoleBinding struct {
	Role             string            `json:"role"`
	EndpointID       string            `json:"endpoint_id"`
	ChannelID        string            `json:"channel_id"`
	BudgetPoolID     string            `json:"budget_pool_id"`
	ContextProfileID string            `json:"context_profile_id"`
	Priority         int               `json:"priority"`
	Fallbacks        []FallbackBinding `json:"fallbacks,omitempty"`
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
	for i, fb := range r.Fallbacks {
		if err := fb.Validate(); err != nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: fallbacks[%d]: %v", kind, i, err)
		}
		if fb.EndpointID == r.EndpointID {
			return errs.New(errs.CategoryInvalidArgument, "%s: fallbacks[%d] endpoint_id %q matches primary endpoint_id", kind, i, fb.EndpointID)
		}
	}
	return nil
}

// DiversityPolicy specifies provider and model diversity constraints (COGNITION_PORTFOLIO §11, PROTOCOLS §10B).
type DiversityPolicy struct {
	RequireDistinctModelsForReview    bool `json:"require_distinct_models_for_review,omitempty"`
	RequireDistinctProvidersForReview bool `json:"require_distinct_providers_for_review,omitempty"`
	RequireDistinctEndpointsForReview bool `json:"require_distinct_endpoints_for_review,omitempty"`
}

// Validate checks DiversityPolicy fields.
func (d DiversityPolicy) Validate() error {
	return nil
}

// EscalationRule specifies an explicit escalation transition path between roles/endpoints (COGNITION_PORTFOLIO §11).
type EscalationRule struct {
	FromRole         string `json:"from_role"`
	ToRole           string `json:"to_role"`
	TriggerCondition string `json:"trigger_condition"`
	MaxEscalations   int    `json:"max_escalations"`
}

// Validate checks EscalationRule fields.
func (e EscalationRule) Validate() error {
	const kind = "EscalationRule"
	if err := requireNonEmpty(kind, "from_role", e.FromRole); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "to_role", e.ToRole); err != nil {
		return err
	}
	if err := requireNonEmpty(kind, "trigger_condition", e.TriggerCondition); err != nil {
		return err
	}
	if e.MaxEscalations < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: max_escalations must be >= 1, got %d", kind, e.MaxEscalations)
	}
	return nil
}

// WorkflowDefaults specifies default execution constraints for synthesized workflows (COGNITION_PORTFOLIO §11).
type WorkflowDefaults struct {
	DefaultTopology       WorkflowTopologyKind `json:"default_topology,omitempty"`
	DefaultTimeoutSeconds int                  `json:"default_timeout_seconds,omitempty"`
	MaxRetries            int                  `json:"max_retries,omitempty"`
}

// Validate checks WorkflowDefaults fields.
func (w WorkflowDefaults) Validate() error {
	const kind = "WorkflowDefaults"
	if w.DefaultTopology != "" && !w.DefaultTopology.Valid() {
		return enumError(kind, "default_topology", string(w.DefaultTopology),
			string(TopologySinglePass), string(TopologyIterativeEscalation),
			string(TopologyDualIndependentReview), string(TopologyDeterministicOnly))
	}
	if w.DefaultTimeoutSeconds < 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: default_timeout_seconds cannot be negative, got %d", kind, w.DefaultTimeoutSeconds)
	}
	if w.MaxRetries < 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: max_retries cannot be negative, got %d", kind, w.MaxRetries)
	}
	return nil
}

// CognitionPortfolio is the canonical routing configuration (ADR-0018 §7, FR-062).
type CognitionPortfolio struct {
	SchemaVersion         SchemaVersion     `json:"schema_version"`
	PortfolioID           string            `json:"portfolio_id"`
	Revision              int               `json:"revision"`
	CreatedAt             string            `json:"created_at"`
	Channels              []AccessChannel   `json:"channels"`
	RoleBindings          []RoleBinding     `json:"role_bindings"`
	BudgetPools           []BudgetPool      `json:"budget_pools"`
	MaxSourceExposure     SourceExposure    `json:"max_source_exposure"`
	ExcludedEndpointIDs   []string          `json:"excluded_endpoint_ids,omitempty"`
	BudgetReservations    map[string]int64  `json:"budget_reservations,omitempty"`
	DiversityRequirements *DiversityPolicy  `json:"diversity_requirements,omitempty"`
	EscalationRules       []EscalationRule  `json:"escalation_rules,omitempty"`
	WorkflowDefaults      *WorkflowDefaults `json:"workflow_defaults,omitempty"`
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

	roleNames := make(map[string]struct{}, len(c.RoleBindings))
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
		roleNames[rb.Role] = struct{}{}
		rpKey := rolePriorityKey{role: rb.Role, priority: rb.Priority}
		if firstIndex, dup := seenRolePriority[rpKey]; dup {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: role_bindings[%d] duplicate priority %d for role %q (already assigned in role_bindings[%d])",
				kind, i, rb.Priority, rb.Role, firstIndex)
		}
		seenRolePriority[rpKey] = i

		for j, fb := range rb.Fallbacks {
			if excludedMap[fb.EndpointID] {
				return errs.New(errs.CategoryInvalidArgument, "%s: role_bindings[%d].fallbacks[%d] endpoint_id %q is excluded", kind, i, j, fb.EndpointID)
			}
			fbCh, ok := channelMap[fb.ChannelID]
			if !ok {
				return errs.New(errs.CategoryInvalidArgument, "%s: role_bindings[%d].fallbacks[%d] references non-existent channel_id %q", kind, i, j, fb.ChannelID)
			}
			if fbCh.EndpointID != fb.EndpointID {
				return errs.New(errs.CategoryInvalidArgument, "%s: role_bindings[%d].fallbacks[%d] endpoint_id %q does not match channel endpoint_id %q", kind, i, j, fb.EndpointID, fbCh.EndpointID)
			}
			fbPool, ok := poolMap[fb.BudgetPoolID]
			if !ok {
				return errs.New(errs.CategoryInvalidArgument, "%s: role_bindings[%d].fallbacks[%d] references non-existent budget_pool_id %q", kind, i, j, fb.BudgetPoolID)
			}
			if fbPool.Regime == RegimeMeteredAPI && !primaryPool.FallbackAllowedToMetered {
				return errs.New(errs.CategoryInvalidArgument,
					"%s: role_bindings[%d].fallbacks[%d] endpoint_id %q is bound to metered budget pool %q, but primary budget pool %q forbids fallback to metered (ADR-0018 §9, DCI-104)",
					kind, i, j, fb.EndpointID, fbPool.PoolID, primaryPool.PoolID)
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

	if c.DiversityRequirements != nil {
		if err := c.DiversityRequirements.Validate(); err != nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: diversity_requirements: %v", kind, err)
		}
	}

	for i, er := range c.EscalationRules {
		if err := er.Validate(); err != nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: escalation_rules[%d]: %v", kind, i, err)
		}
		if _, ok := roleNames[er.FromRole]; !ok {
			return errs.New(errs.CategoryInvalidArgument, "%s: escalation_rules[%d] from_role %q is not defined in role_bindings", kind, i, er.FromRole)
		}
		if _, ok := roleNames[er.ToRole]; !ok {
			return errs.New(errs.CategoryInvalidArgument, "%s: escalation_rules[%d] to_role %q is not defined in role_bindings", kind, i, er.ToRole)
		}
	}

	if c.WorkflowDefaults != nil {
		if err := c.WorkflowDefaults.Validate(); err != nil {
			return errs.New(errs.CategoryInvalidArgument, "%s: workflow_defaults: %v", kind, err)
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
	StageID             string    `json:"stage_id"`
	Role                string    `json:"role"`
	Kind                StageKind `json:"kind"`
	IsReview            bool      `json:"is_review,omitempty"`
	Order               int       `json:"order"`
	DependsOn           []string  `json:"depends_on,omitempty"`
	BudgetPoolID        string    `json:"budget_pool_id"`
	TimeoutSeconds      int       `json:"timeout_seconds"`
	EndpointID          *string   `json:"endpoint_id,omitempty"`
	ChannelID           *string   `json:"channel_id,omitempty"`
	ContextProfileID    *string   `json:"context_profile_id,omitempty"`
	RetryLimit          int       `json:"retry_limit,omitempty"`
	EscalationTarget    *string   `json:"escalation_target,omitempty"`
	DeterministicGateID *string   `json:"deterministic_gate_id,omitempty"`
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
	if w.Kind == StageKindDeterministic {
		if w.DeterministicGateID == nil || *w.DeterministicGateID == "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: deterministic_gate_id is required when kind is %q", kind, StageKindDeterministic)
		}
	} else if w.DeterministicGateID != nil && *w.DeterministicGateID != "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: deterministic_gate_id is forbidden when kind is %q", kind, w.Kind)
	}
	if w.RetryLimit < 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: retry_limit cannot be negative, got %d", kind, w.RetryLimit)
	}
	if w.EndpointID != nil && *w.EndpointID == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: endpoint_id cannot be empty if specified", kind)
	}
	if w.ChannelID != nil && *w.ChannelID == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: channel_id cannot be empty if specified", kind)
	}
	if w.ContextProfileID != nil && *w.ContextProfileID == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: context_profile_id cannot be empty if specified", kind)
	}
	if w.EscalationTarget != nil && *w.EscalationTarget == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: escalation_target cannot be empty if specified", kind)
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
		// Enforce independence between review stages (ADR-0019 §2, PROTOCOLS §10B)
		var reviewStages []WorkflowStage
		for _, s := range w.Stages {
			if s.IsReview {
				reviewStages = append(reviewStages, s)
			}
		}
		for i := 0; i < len(reviewStages); i++ {
			for j := i + 1; j < len(reviewStages); j++ {
				s1, s2 := reviewStages[i], reviewStages[j]
				if s1.EndpointID != nil && s2.EndpointID != nil && *s1.EndpointID == *s2.EndpointID {
					return errs.New(errs.CategoryInvalidArgument,
						"%s: topology %q requires independent review stages, but stages %q and %q bind to the same endpoint %q",
						kind, w.Topology, s1.StageID, s2.StageID, *s1.EndpointID)
				}
				if (s1.EndpointID == nil || s2.EndpointID == nil) && s1.Role == s2.Role {
					return errs.New(errs.CategoryInvalidArgument,
						"%s: topology %q requires independent review stages, but stages %q and %q share the same role %q without distinct endpoint assignments",
						kind, w.Topology, s1.StageID, s2.StageID, s1.Role)
				}
			}
		}
	case TopologyDeterministicOnly:
		if deterministicStageCount != len(w.Stages) {
			return errs.New(errs.CategoryInvalidArgument, "%s: topology %q permits only deterministic stages (kind: %q), but found %d non-deterministic stages", kind, w.Topology, StageKindDeterministic, len(w.Stages)-deterministicStageCount)
		}
	}

	return nil
}
