package cognition

import (
	"fmt"
	"sort"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// DeltaKind classifies the nature of change for a portfolio element.
type DeltaKind string

const (
	DeltaAdded    DeltaKind = "added"
	DeltaRemoved  DeltaKind = "removed"
	DeltaModified DeltaKind = "modified"
)

// FieldChange captures the before and after state of a changed property.
type FieldChange struct {
	Field    string `json:"field"`
	OldValue string `json:"old_value"`
	NewValue string `json:"new_value"`
}

// ItemDiff describes changes to an individual channel, role binding, or budget pool.
type ItemDiff struct {
	ID      string        `json:"id"`
	Kind    string        `json:"kind"`
	Delta   DeltaKind     `json:"delta"`
	Details []FieldChange `json:"details,omitempty"`
}

// PortfolioDiff records the structured semantic diff between two portfolios.
type PortfolioDiff struct {
	FromPortfolioID  string        `json:"from_portfolio_id"`
	ToPortfolioID    string        `json:"to_portfolio_id"`
	FromRevision     int           `json:"from_revision"`
	ToRevision       int           `json:"to_revision"`
	ChannelDiffs     []ItemDiff    `json:"channel_diffs,omitempty"`
	RoleBindingDiffs []ItemDiff    `json:"role_binding_diffs,omitempty"`
	BudgetPoolDiffs  []ItemDiff    `json:"budget_pool_diffs,omitempty"`
	PolicyChanges    []FieldChange `json:"policy_changes,omitempty"`
	HasChanges       bool          `json:"has_changes"`
}

// DiffPortfolios computes the semantic difference between base and candidate portfolios (ADR-0018 §11).
// If base is nil, initial bootstrap semantics apply (all candidate items marked DeltaAdded).
func DiffPortfolios(base, candidate *protocol.CognitionPortfolio) (*PortfolioDiff, error) {
	if candidate == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "candidate portfolio is required")
	}

	if base == nil {
		return bootstrapDiff(candidate), nil
	}

	diff := &PortfolioDiff{
		FromPortfolioID: base.PortfolioID,
		ToPortfolioID:   candidate.PortfolioID,
		FromRevision:    base.Revision,
		ToRevision:      candidate.Revision,
	}

	// 1. Compare Channels (Key: ChannelID)
	diff.ChannelDiffs = diffChannels(base.Channels, candidate.Channels)

	// 2. Compare RoleBindings (Key: Role#Priority)
	diff.RoleBindingDiffs = diffRoleBindings(base.RoleBindings, candidate.RoleBindings)

	// 3. Compare BudgetPools (Key: PoolID)
	diff.BudgetPoolDiffs = diffBudgetPools(base.BudgetPools, candidate.BudgetPools)

	// 4. Compare Policy and Metadata fields
	diff.PolicyChanges = diffPolicies(base, candidate)

	diff.HasChanges = len(diff.ChannelDiffs) > 0 ||
		len(diff.RoleBindingDiffs) > 0 ||
		len(diff.BudgetPoolDiffs) > 0 ||
		len(diff.PolicyChanges) > 0

	return diff, nil
}

func bootstrapDiff(candidate *protocol.CognitionPortfolio) *PortfolioDiff {
	diff := &PortfolioDiff{
		FromPortfolioID: "",
		ToPortfolioID:   candidate.PortfolioID,
		FromRevision:    0,
		ToRevision:      candidate.Revision,
		HasChanges:      true,
	}

	for _, c := range candidate.Channels {
		diff.ChannelDiffs = append(diff.ChannelDiffs, ItemDiff{
			ID:    c.ChannelID,
			Kind:  "access_channel",
			Delta: DeltaAdded,
		})
	}
	sortItemDiffs(diff.ChannelDiffs)

	for _, rb := range candidate.RoleBindings {
		key := fmt.Sprintf("%s#%d", rb.Role, rb.Priority)
		diff.RoleBindingDiffs = append(diff.RoleBindingDiffs, ItemDiff{
			ID:    key,
			Kind:  "role_binding",
			Delta: DeltaAdded,
		})
	}
	sortItemDiffs(diff.RoleBindingDiffs)

	for _, bp := range candidate.BudgetPools {
		diff.BudgetPoolDiffs = append(diff.BudgetPoolDiffs, ItemDiff{
			ID:    bp.PoolID,
			Kind:  "budget_pool",
			Delta: DeltaAdded,
		})
	}
	sortItemDiffs(diff.BudgetPoolDiffs)

	if candidate.MaxSourceExposure != "" {
		diff.PolicyChanges = append(diff.PolicyChanges, FieldChange{
			Field:    "max_source_exposure",
			OldValue: "",
			NewValue: string(candidate.MaxSourceExposure),
		})
	}

	return diff
}

func diffChannels(baseList, candList []protocol.AccessChannel) []ItemDiff {
	baseMap := make(map[string]protocol.AccessChannel, len(baseList))
	for _, c := range baseList {
		baseMap[c.ChannelID] = c
	}
	candMap := make(map[string]protocol.AccessChannel, len(candList))
	for _, c := range candList {
		candMap[c.ChannelID] = c
	}

	var diffs []ItemDiff

	// Added & Modified
	for id, cand := range candMap {
		base, exists := baseMap[id]
		if !exists {
			diffs = append(diffs, ItemDiff{
				ID:    id,
				Kind:  "access_channel",
				Delta: DeltaAdded,
			})
			continue
		}

		var details []FieldChange
		if cand.EndpointID != base.EndpointID {
			details = append(details, FieldChange{Field: "endpoint_id", OldValue: base.EndpointID, NewValue: cand.EndpointID})
		}
		if cand.Kind != base.Kind {
			details = append(details, FieldChange{Field: "kind", OldValue: string(base.Kind), NewValue: string(cand.Kind)})
		}
		if cand.SessionMode != base.SessionMode {
			details = append(details, FieldChange{Field: "session_mode", OldValue: string(base.SessionMode), NewValue: string(cand.SessionMode)})
		}
		if cand.ContextControl != base.ContextControl {
			details = append(details, FieldChange{Field: "context_control", OldValue: string(base.ContextControl), NewValue: string(cand.ContextControl)})
		}
		if cand.PrefixCache != base.PrefixCache {
			details = append(details, FieldChange{Field: "prefix_cache", OldValue: string(base.PrefixCache), NewValue: string(cand.PrefixCache)})
		}
		if cand.SupportsStreaming != base.SupportsStreaming {
			details = append(details, FieldChange{Field: "supports_streaming", OldValue: fmt.Sprintf("%v", base.SupportsStreaming), NewValue: fmt.Sprintf("%v", cand.SupportsStreaming)})
		}
		if cand.SupportsTools != base.SupportsTools {
			details = append(details, FieldChange{Field: "supports_tools", OldValue: fmt.Sprintf("%v", base.SupportsTools), NewValue: fmt.Sprintf("%v", cand.SupportsTools)})
		}
		if cand.NativeWorktreeAccess != base.NativeWorktreeAccess {
			details = append(details, FieldChange{Field: "native_worktree_access", OldValue: fmt.Sprintf("%v", base.NativeWorktreeAccess), NewValue: fmt.Sprintf("%v", cand.NativeWorktreeAccess)})
		}
		if cand.MaxConcurrentRequests != base.MaxConcurrentRequests {
			details = append(details, FieldChange{Field: "max_concurrent_requests", OldValue: fmt.Sprintf("%d", base.MaxConcurrentRequests), NewValue: fmt.Sprintf("%d", cand.MaxConcurrentRequests)})
		}

		if len(details) > 0 {
			diffs = append(diffs, ItemDiff{
				ID:      id,
				Kind:    "access_channel",
				Delta:   DeltaModified,
				Details: details,
			})
		}
	}

	// Removed
	for id := range baseMap {
		if _, exists := candMap[id]; !exists {
			diffs = append(diffs, ItemDiff{
				ID:    id,
				Kind:  "access_channel",
				Delta: DeltaRemoved,
			})
		}
	}

	sortItemDiffs(diffs)
	return diffs
}

func diffRoleBindings(baseList, candList []protocol.RoleBinding) []ItemDiff {
	keyOf := func(rb protocol.RoleBinding) string {
		return fmt.Sprintf("%s#%d", rb.Role, rb.Priority)
	}

	baseMap := make(map[string]protocol.RoleBinding, len(baseList))
	for _, rb := range baseList {
		baseMap[keyOf(rb)] = rb
	}
	candMap := make(map[string]protocol.RoleBinding, len(candList))
	for _, rb := range candList {
		candMap[keyOf(rb)] = rb
	}

	var diffs []ItemDiff

	for key, cand := range candMap {
		base, exists := baseMap[key]
		if !exists {
			diffs = append(diffs, ItemDiff{
				ID:    key,
				Kind:  "role_binding",
				Delta: DeltaAdded,
			})
			continue
		}

		var details []FieldChange
		if cand.EndpointID != base.EndpointID {
			details = append(details, FieldChange{Field: "endpoint_id", OldValue: base.EndpointID, NewValue: cand.EndpointID})
		}
		if cand.ChannelID != base.ChannelID {
			details = append(details, FieldChange{Field: "channel_id", OldValue: base.ChannelID, NewValue: cand.ChannelID})
		}
		if cand.BudgetPoolID != base.BudgetPoolID {
			details = append(details, FieldChange{Field: "budget_pool_id", OldValue: base.BudgetPoolID, NewValue: cand.BudgetPoolID})
		}
		if cand.ContextProfileID != base.ContextProfileID {
			details = append(details, FieldChange{Field: "context_profile_id", OldValue: base.ContextProfileID, NewValue: cand.ContextProfileID})
		}

		// Fallbacks summary comparison
		baseFB := formatFallbacks(base.Fallbacks)
		candFB := formatFallbacks(cand.Fallbacks)
		if baseFB != candFB {
			details = append(details, FieldChange{Field: "fallbacks", OldValue: baseFB, NewValue: candFB})
		}

		if len(details) > 0 {
			diffs = append(diffs, ItemDiff{
				ID:      key,
				Kind:    "role_binding",
				Delta:   DeltaModified,
				Details: details,
			})
		}
	}

	for key := range baseMap {
		if _, exists := candMap[key]; !exists {
			diffs = append(diffs, ItemDiff{
				ID:    key,
				Kind:  "role_binding",
				Delta: DeltaRemoved,
			})
		}
	}

	sortItemDiffs(diffs)
	return diffs
}

func formatFallbacks(fbs []protocol.FallbackBinding) string {
	if len(fbs) == 0 {
		return ""
	}
	parts := make([]string, len(fbs))
	for i, fb := range fbs {
		parts[i] = fmt.Sprintf("[%s/%s/%s/%s]", fb.EndpointID, fb.ChannelID, fb.BudgetPoolID, fb.ContextProfileID)
	}
	return strings.Join(parts, ", ")
}

func diffBudgetPools(baseList, candList []protocol.BudgetPool) []ItemDiff {
	baseMap := make(map[string]protocol.BudgetPool, len(baseList))
	for _, bp := range baseList {
		baseMap[bp.PoolID] = bp
	}
	candMap := make(map[string]protocol.BudgetPool, len(candList))
	for _, bp := range candList {
		candMap[bp.PoolID] = bp
	}

	var diffs []ItemDiff

	for id, cand := range candMap {
		base, exists := baseMap[id]
		if !exists {
			diffs = append(diffs, ItemDiff{
				ID:    id,
				Kind:  "budget_pool",
				Delta: DeltaAdded,
			})
			continue
		}

		var details []FieldChange
		if cand.Name != base.Name {
			details = append(details, FieldChange{Field: "name", OldValue: base.Name, NewValue: cand.Name})
		}
		if cand.Regime != base.Regime {
			details = append(details, FieldChange{Field: "regime", OldValue: string(base.Regime), NewValue: string(cand.Regime)})
		}
		if cand.HardLimit != base.HardLimit {
			details = append(details, FieldChange{Field: "hard_limit", OldValue: fmt.Sprintf("%d", base.HardLimit), NewValue: fmt.Sprintf("%d", cand.HardLimit)})
		}
		if cand.SoftAlertLimit != base.SoftAlertLimit {
			details = append(details, FieldChange{Field: "soft_alert_limit", OldValue: fmt.Sprintf("%d", base.SoftAlertLimit), NewValue: fmt.Sprintf("%d", cand.SoftAlertLimit)})
		}
		if cand.Unit != base.Unit {
			details = append(details, FieldChange{Field: "unit", OldValue: string(base.Unit), NewValue: string(cand.Unit)})
		}
		if cand.Period != base.Period {
			details = append(details, FieldChange{Field: "period", OldValue: string(base.Period), NewValue: string(cand.Period)})
		}

		if len(details) > 0 {
			diffs = append(diffs, ItemDiff{
				ID:      id,
				Kind:    "budget_pool",
				Delta:   DeltaModified,
				Details: details,
			})
		}
	}

	for id := range baseMap {
		if _, exists := candMap[id]; !exists {
			diffs = append(diffs, ItemDiff{
				ID:    id,
				Kind:  "budget_pool",
				Delta: DeltaRemoved,
			})
		}
	}

	sortItemDiffs(diffs)
	return diffs
}

func diffPolicies(base, cand *protocol.CognitionPortfolio) []FieldChange {
	var changes []FieldChange

	if cand.MaxSourceExposure != base.MaxSourceExposure {
		changes = append(changes, FieldChange{
			Field:    "max_source_exposure",
			OldValue: string(base.MaxSourceExposure),
			NewValue: string(cand.MaxSourceExposure),
		})
	}

	baseExcluded := strings.Join(base.ExcludedEndpointIDs, ",")
	candExcluded := strings.Join(cand.ExcludedEndpointIDs, ",")
	if baseExcluded != candExcluded {
		changes = append(changes, FieldChange{
			Field:    "excluded_endpoint_ids",
			OldValue: baseExcluded,
			NewValue: candExcluded,
		})
	}

	// Compare diversity requirements
	baseDiv := formatDiversity(base.DiversityRequirements)
	candDiv := formatDiversity(cand.DiversityRequirements)
	if baseDiv != candDiv {
		changes = append(changes, FieldChange{
			Field:    "diversity_requirements",
			OldValue: baseDiv,
			NewValue: candDiv,
		})
	}

	// Compare workflow defaults
	baseWf := formatWorkflowDefaults(base.WorkflowDefaults)
	candWf := formatWorkflowDefaults(cand.WorkflowDefaults)
	if baseWf != candWf {
		changes = append(changes, FieldChange{
			Field:    "workflow_defaults",
			OldValue: baseWf,
			NewValue: candWf,
		})
	}

	return changes
}

func formatDiversity(d *protocol.DiversityPolicy) string {
	if d == nil {
		return ""
	}
	return fmt.Sprintf("models=%v,providers=%v,endpoints=%v",
		d.RequireDistinctModelsForReview,
		d.RequireDistinctProvidersForReview,
		d.RequireDistinctEndpointsForReview)
}

func formatWorkflowDefaults(w *protocol.WorkflowDefaults) string {
	if w == nil {
		return ""
	}
	return fmt.Sprintf("topology=%s,timeout=%d,retries=%d",
		w.DefaultTopology, w.DefaultTimeoutSeconds, w.MaxRetries)
}

func sortItemDiffs(diffs []ItemDiff) {
	sort.Slice(diffs, func(i, j int) bool {
		return diffs[i].ID < diffs[j].ID
	})
}
