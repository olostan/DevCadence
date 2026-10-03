package cognition

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/protocol"
)

// ValidationPolicy specifies project-level constraints and authority bounds
// against which a candidate portfolio is verified (DCI-123, DCI-124).
type ValidationPolicy struct {
	MaxSourceExposure           protocol.SourceExposure   `json:"max_source_exposure"`
	MaxCostClass                protocol.CostClass        `json:"max_cost_class"`
	AllowedRegimes              []protocol.EconomicRegime `json:"allowed_regimes,omitempty"`
	ForbidMeteredAPI            bool                      `json:"forbid_metered_api"`
	RequireMeasuredProvenance   bool                      `json:"require_measured_provenance"`
	RequireVerifiedAcceleration bool                      `json:"require_verified_acceleration"`
	RequireKnownContextControl  bool                      `json:"require_known_context_control"`
	RequireKnownResourceState   bool                      `json:"require_known_resource_state"`
	MinContractLimitTokens      int                       `json:"min_contract_limit_tokens,omitempty"`
	RoleRequirements            map[Role]RoleRequirement  `json:"role_requirements,omitempty"`
}

// DefaultValidationPolicy returns a conservative default validation policy.
func DefaultValidationPolicy() ValidationPolicy {
	return ValidationPolicy{
		MaxSourceExposure: protocol.ExposureFocusedSnippets,
		MaxCostClass:      protocol.CostRemoteEconomy,
		RoleRequirements:  DefaultRequirements(),
	}
}

// ValidationInput contains the complete factual context for deterministic validation.
type ValidationInput struct {
	Portfolio       *protocol.CognitionPortfolio
	Inventory       *protocol.ResourceInventory
	MachineProfile  *protocol.MachineCapabilityProfile
	ContextProfiles map[string]*protocol.ContextProfile
	BudgetStates    map[string]*protocol.BudgetState
	ResourceStates  map[string]*protocol.ResourceState
	Policy          *ValidationPolicy
	Clock           clock.Clock
}

// PortfolioValidator executes pure deterministic validation over candidate portfolios.
type PortfolioValidator struct{}

// NewPortfolioValidator returns a new PortfolioValidator.
func NewPortfolioValidator() *PortfolioValidator {
	return &PortfolioValidator{}
}

// validatorContext bundles precomputed lookups for a validation run.
type validatorContext struct {
	input              ValidationInput
	p                  *protocol.CognitionPortfolio
	channelMap         map[string]protocol.AccessChannel
	poolMap            map[string]protocol.BudgetPool
	excludedMap        map[string]bool
	profileEndpoints   map[string]protocol.CognitionEndpoint
	inventoryEndpoints map[string]protocol.CognitionEndpointSummary
	roleReqs           map[Role]RoleRequirement
}

// Validate executes all 8 deterministic validation dimensions in fixed order.
func (v *PortfolioValidator) Validate(input ValidationInput) ValidationResult {
	clk := input.Clock
	if clk == nil {
		clk = clock.System()
	}
	validatedAt := clk.Now().UTC().Format(time.RFC3339)

	var diagnostics []PortfolioDiagnostic

	if input.Portfolio == nil {
		diagnostics = append(diagnostics, PortfolioDiagnostic{
			Code:         CodePortfolioStructureInvalid,
			Condition:    ConditionInvalid,
			Target:       "portfolio",
			ViolatedRule: "DCI-123",
			Message:      "candidate portfolio cannot be nil",
		})
		return ValidationResult{
			Valid:       false,
			Diagnostics: diagnostics,
			ValidatedAt: validatedAt,
		}
	}

	p := input.Portfolio

	candBytes, _ := protocol.CanonicalJSON(p)
	candDigest := "sha256:" + hashBytes(candBytes)

	invDigest := ""
	if input.Inventory != nil {
		invBytes, _ := protocol.CanonicalJSON(input.Inventory)
		invDigest = "sha256:" + hashBytes(invBytes)
	}

	polDigest := ""
	if input.Policy != nil {
		polBytes, _ := protocol.CanonicalJSON(input.Policy)
		polDigest = "sha256:" + hashBytes(polBytes)
	}

	// Basic Schema Validation
	if err := p.Validate(); err != nil {
		diagnostics = append(diagnostics, PortfolioDiagnostic{
			Code:         CodePortfolioStructureInvalid,
			Condition:    ConditionInvalid,
			Target:       "portfolio",
			ViolatedRule: "DCI-123",
			Message:      fmt.Sprintf("schema validation failed: %v", err),
		})
	}

	ctx := newValidatorContext(input)

	// Dimension 1, 2, 3: Endpoint existence, capability compatibility & provenance
	ctx.validateEndpointsAndCapabilities(&diagnostics)

	// Dimension 4, 5: Source exposure policy, economic & budget bindings
	ctx.validatePolicyAndEconomics(&diagnostics)

	// Dimension 6, 7, 8: Context compatibility, machine constraints, structure & diversity
	ctx.validateContextAndConstraints(&diagnostics)

	return ValidationResult{
		Valid:           len(diagnostics) == 0,
		Diagnostics:     diagnostics,
		ValidatedAt:     validatedAt,
		CandidateDigest: candDigest,
		InventoryDigest: invDigest,
		PolicyDigest:    polDigest,
	}
}

func newValidatorContext(input ValidationInput) *validatorContext {
	p := input.Portfolio

	channelMap := make(map[string]protocol.AccessChannel, len(p.Channels))
	for _, ch := range p.Channels {
		channelMap[ch.ChannelID] = ch
	}

	poolMap := make(map[string]protocol.BudgetPool, len(p.BudgetPools))
	for _, bp := range p.BudgetPools {
		poolMap[bp.PoolID] = bp
	}

	excludedMap := make(map[string]bool, len(p.ExcludedEndpointIDs))
	for _, epID := range p.ExcludedEndpointIDs {
		excludedMap[epID] = true
	}

	profileEndpoints := make(map[string]protocol.CognitionEndpoint)
	if input.MachineProfile != nil {
		for _, ep := range input.MachineProfile.Endpoints {
			profileEndpoints[ep.ID] = ep
		}
	}

	inventoryEndpoints := make(map[string]protocol.CognitionEndpointSummary)
	if input.Inventory != nil {
		for _, ep := range input.Inventory.CognitionEndpoints {
			inventoryEndpoints[ep.ID] = ep
		}
	}

	roleReqs := DefaultRequirements()
	if input.Policy != nil && len(input.Policy.RoleRequirements) > 0 {
		roleReqs = input.Policy.RoleRequirements
	}

	return &validatorContext{
		input:              input,
		p:                  p,
		channelMap:         channelMap,
		poolMap:            poolMap,
		excludedMap:        excludedMap,
		profileEndpoints:   profileEndpoints,
		inventoryEndpoints: inventoryEndpoints,
		roleReqs:           roleReqs,
	}
}

func hashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// SortedDiagnostics sorts diagnostics stably by target and code.
func SortedDiagnostics(diags []PortfolioDiagnostic) []PortfolioDiagnostic {
	out := append([]PortfolioDiagnostic(nil), diags...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Target != out[j].Target {
			return out[i].Target < out[j].Target
		}
		return out[i].Code < out[j].Code
	})
	return out
}
