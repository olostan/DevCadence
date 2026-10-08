package execpolicy

import (
	"context"
	"fmt"
	"strings"

	"github.com/olostan/DevCadence/internal/actors"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/protocol"
)

// EndpointRequest specifies requirements for endpoint resolution.
type EndpointRequest struct {
	ProjectID         string
	TaskID            string
	Role              string // e.g. "implementer", "reviewer"
	ContextNeed       protocol.SourceExposure
	PortfolioDigest   string
	IndependenceBasis string                // "endpoint_model" | "model_family_account"; required iff ExcludeBases is non-empty
	ExcludeBases      []protocol.ActorBasis // non-empty for review; empty for implementer
}

// Validate checks EndpointRequest completeness and independence basis consistency.
func (r EndpointRequest) Validate() error {
	const kind = "EndpointRequest"
	if strings.TrimSpace(r.ProjectID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: project_id is required", kind)
	}
	if strings.TrimSpace(r.TaskID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: task_id is required", kind)
	}
	if strings.TrimSpace(r.Role) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: role is required", kind)
	}
	if len(r.ExcludeBases) > 0 {
		if r.IndependenceBasis != actors.BasisEndpointModel && r.IndependenceBasis != actors.BasisModelFamilyAccount {
			return errs.New(errs.CategoryInvalidArgument,
				"%s: independence_basis must be %q or %q when exclude_bases is non-empty, got %q",
				kind, actors.BasisEndpointModel, actors.BasisModelFamilyAccount, r.IndependenceBasis)
		}
		for i, eb := range r.ExcludeBases {
			if _, err := actors.DeriveActorID(r.IndependenceBasis, eb); err != nil {
				return errs.Wrap(errs.CategoryInvalidArgument, err, "%s: exclude_bases[%d] invalid for basis %q", kind, i, r.IndependenceBasis)
			}
		}
	}
	return nil
}

// ResolvedEndpoint represents an endpoint chosen by the resolver.
// It is "unbound" when returned by Resolve (ModelRevision, RuntimeVersion, DriverID, and BindingDigest empty)
// and "bound" after Bind.
type ResolvedEndpoint struct {
	EndpointID      string                  `json:"endpoint_id"`
	ModelID         string                  `json:"model_id"`
	ModelRevision   string                  `json:"model_revision,omitempty"`
	RuntimeVersion  string                  `json:"runtime_version,omitempty"`
	DriverID        string                  `json:"driver_id,omitempty"`
	Provider        string                  `json:"provider"`
	ModelFamily     string                  `json:"model_family"`
	AccountRef      string                  `json:"account_ref"`
	Kind            protocol.EndpointKind   `json:"kind"`
	Locality        protocol.Locality       `json:"locality"`
	Channel         protocol.AccessChannel  `json:"channel"`
	ContextProfile  protocol.ContextProfile `json:"context_profile"`
	Limits          ExecutionLimits         `json:"limits"`
	PolicyDigest    string                  `json:"policy_digest"`
	PortfolioDigest string                  `json:"portfolio_digest"`
	BindingDigest   string                  `json:"binding_digest,omitempty"`
}

// ActorBasis extracts the 6-field actor identity basis from the endpoint.
func (r ResolvedEndpoint) ActorBasis() protocol.ActorBasis {
	return protocol.ActorBasis{
		EndpointID:    r.EndpointID,
		ModelID:       r.ModelID,
		ModelRevision: r.ModelRevision,
		Provider:      r.Provider,
		ModelFamily:   r.ModelFamily,
		AccountRef:    r.AccountRef,
	}
}

// EndpointResolver resolves an eligible endpoint for a task execution request under an execution policy.
type EndpointResolver interface {
	Resolve(ctx context.Context, req EndpointRequest, policy ExecutionPolicy, policyDigest string) (ResolvedEndpoint, error)
}

// PortfolioEndpointResolver implements EndpointResolver by matching against an active CognitionPortfolio and ExecutionPolicy.
type PortfolioEndpointResolver struct {
	portfolio       *protocol.CognitionPortfolio
	endpoints       []protocol.CognitionEndpoint
	contextProfiles map[string]protocol.ContextProfile
}

// NewPortfolioEndpointResolver constructs a new PortfolioEndpointResolver.
func NewPortfolioEndpointResolver(portfolio *protocol.CognitionPortfolio) *PortfolioEndpointResolver {
	return &PortfolioEndpointResolver{
		portfolio:       portfolio,
		contextProfiles: make(map[string]protocol.ContextProfile),
	}
}

// WithEndpoints adds known cognition endpoint records to the resolver.
func (r *PortfolioEndpointResolver) WithEndpoints(endpoints ...protocol.CognitionEndpoint) *PortfolioEndpointResolver {
	r.endpoints = append(r.endpoints, endpoints...)
	return r
}

// WithContextProfiles adds known context profile records to the resolver.
func (r *PortfolioEndpointResolver) WithContextProfiles(profiles ...protocol.ContextProfile) *PortfolioEndpointResolver {
	if r.contextProfiles == nil {
		r.contextProfiles = make(map[string]protocol.ContextProfile)
	}
	for _, cp := range profiles {
		r.contextProfiles[cp.ProfileID] = cp
	}
	return r
}

type candidateEndpoint struct {
	endpointID     string
	modelID        string
	modelRevision  string
	provider       string
	modelFamily    string
	accountRef     string
	kind           protocol.EndpointKind
	locality       protocol.Locality
	channel        protocol.AccessChannel
	contextProfile protocol.ContextProfile
	limits         ExecutionLimits
}

// Resolve selects the first matching eligible endpoint for the given request and policy.
func (r *PortfolioEndpointResolver) Resolve(ctx context.Context, req EndpointRequest, policy ExecutionPolicy, policyDigest string) (ResolvedEndpoint, error) {
	if err := req.Validate(); err != nil {
		return ResolvedEndpoint{}, err
	}
	if err := policy.Validate(); err != nil {
		return ResolvedEndpoint{}, err
	}
	if strings.TrimSpace(policyDigest) == "" {
		return ResolvedEndpoint{}, errs.New(errs.CategoryInvalidArgument, "policy digest is required")
	}
	if r.portfolio == nil {
		return ResolvedEndpoint{}, principal.NewCodedError(
			principal.CodeModelUnavailable,
			false,
			[]string{"no-eligible-endpoint"},
			"portfolio is nil",
		)
	}

	// Index portfolio channels
	channelMap := make(map[string]protocol.AccessChannel, len(r.portfolio.Channels))
	for _, ch := range r.portfolio.Channels {
		channelMap[ch.ChannelID] = ch
	}

	// Index role bindings for the requested role
	type roleBindingMeta struct {
		channelID        string
		contextProfileID string
	}
	roleBindings := make(map[string]roleBindingMeta)
	for _, rb := range r.portfolio.RoleBindings {
		if rb.Role == req.Role {
			roleBindings[rb.EndpointID] = roleBindingMeta{
				channelID:        rb.ChannelID,
				contextProfileID: rb.ContextProfileID,
			}
			for _, fb := range rb.Fallbacks {
				if _, exists := roleBindings[fb.EndpointID]; !exists {
					roleBindings[fb.EndpointID] = roleBindingMeta{
						channelID:        fb.ChannelID,
						contextProfileID: fb.ContextProfileID,
					}
				}
			}
		}
	}

	// Index excluded endpoint IDs from portfolio
	excludedEndpointMap := make(map[string]bool, len(r.portfolio.ExcludedEndpointIDs))
	for _, id := range r.portfolio.ExcludedEndpointIDs {
		excludedEndpointMap[id] = true
	}

	// Pre-derive excluded actor IDs if exclude_bases is non-empty
	var excludedActorIDs map[string]bool
	if len(req.ExcludeBases) > 0 {
		excludedActorIDs = make(map[string]bool, len(req.ExcludeBases))
		for _, eb := range req.ExcludeBases {
			id, err := actors.DeriveActorID(req.IndependenceBasis, eb)
			if err != nil {
				return ResolvedEndpoint{}, errs.Wrap(errs.CategoryInvalidArgument, err, "derive excluded actor id")
			}
			excludedActorIDs[id] = true
		}
	}

	// Build candidate list
	var candidates []candidateEndpoint
	if len(r.endpoints) > 0 {
		for _, ep := range r.endpoints {
			if excludedEndpointMap[ep.ID] {
				continue
			}
			for _, grant := range policy.Grants {
				if grant.EndpointID != ep.ID {
					continue
				}
				if ep.ModelID != "" && grant.ModelID != ep.ModelID {
					continue
				}
				if !roleAllowed(grant.Roles, req.Role) {
					continue
				}
				if grant.SourceExposure.ExposureRank() < req.ContextNeed.ExposureRank() {
					continue
				}

				// Find channel
				var ch protocol.AccessChannel
				var cpID string
				if rbMeta, ok := roleBindings[ep.ID]; ok {
					if c, found := channelMap[rbMeta.channelID]; found {
						ch = c
					}
					cpID = rbMeta.contextProfileID
				}
				if ch.ChannelID == "" {
					for _, c := range r.portfolio.Channels {
						if c.EndpointID == ep.ID {
							ch = c
							break
						}
					}
				}
				if ch.ChannelID == "" {
					continue
				}

				// Find or synthesize context profile
				var cp protocol.ContextProfile
				if cpID != "" {
					if p, found := r.contextProfiles[cpID]; found {
						cp = p
					}
				}
				if cp.ProfileID == "" {
					for _, p := range r.contextProfiles {
						if p.EndpointID == ep.ID {
							cp = p
							break
						}
					}
				}
				if cp.ProfileID == "" {
					cp = protocol.ContextProfile{
						ProfileID:  cpID,
						EndpointID: ep.ID,
						ChannelID:  ch.ChannelID,
						ModelRef:   grant.ModelID,
					}
				}

				kind := ep.Kind
				if kind == "" {
					kind = channelToEndpointKind(ch.Kind)
				}

				candidates = append(candidates, candidateEndpoint{
					endpointID:     ep.ID,
					modelID:        grant.ModelID,
					modelRevision:  ep.Version,
					provider:       ep.Provider,
					modelFamily:    ep.ModelFamily,
					accountRef:     ep.AccountRef,
					kind:           kind,
					locality:       grant.Locality,
					channel:        ch,
					contextProfile: cp,
					limits:         grant.Limits,
				})
			}
		}
	} else {
		for _, grant := range policy.Grants {
			if excludedEndpointMap[grant.EndpointID] {
				continue
			}
			if !roleAllowed(grant.Roles, req.Role) {
				continue
			}
			if grant.SourceExposure.ExposureRank() < req.ContextNeed.ExposureRank() {
				continue
			}

			// Find channel
			var ch protocol.AccessChannel
			var cpID string
			if rbMeta, ok := roleBindings[grant.EndpointID]; ok {
				if c, found := channelMap[rbMeta.channelID]; found {
					ch = c
				}
				cpID = rbMeta.contextProfileID
			}
			if ch.ChannelID == "" {
				for _, c := range r.portfolio.Channels {
					if c.EndpointID == grant.EndpointID {
						ch = c
						break
					}
				}
			}
			if ch.ChannelID == "" {
				continue
			}

			var cp protocol.ContextProfile
			if cpID != "" {
				if p, found := r.contextProfiles[cpID]; found {
					cp = p
				}
			}
			if cp.ProfileID == "" {
				cp = protocol.ContextProfile{
					ProfileID:  cpID,
					EndpointID: grant.EndpointID,
					ChannelID:  ch.ChannelID,
					ModelRef:   grant.ModelID,
				}
			}

			candidates = append(candidates, candidateEndpoint{
				endpointID:     grant.EndpointID,
				modelID:        grant.ModelID,
				kind:           channelToEndpointKind(ch.Kind),
				locality:       grant.Locality,
				channel:        ch,
				contextProfile: cp,
				limits:         grant.Limits,
			})
		}
	}

	// Filter by independence exclusion and select the first eligible candidate
	portfolioDigest := req.PortfolioDigest
	if portfolioDigest == "" {
		portfolioDigest = r.portfolio.PortfolioID
	}

	for _, cand := range candidates {
		candidateBasis := protocol.ActorBasis{
			EndpointID:    cand.endpointID,
			ModelID:       cand.modelID,
			ModelRevision: cand.modelRevision,
			Provider:      cand.provider,
			ModelFamily:   cand.modelFamily,
			AccountRef:    cand.accountRef,
		}

		if len(req.ExcludeBases) > 0 {
			var candidateActorID string
			switch req.IndependenceBasis {
			case actors.BasisEndpointModel:
				if candidateBasis.ModelRevision != "" {
					id, err := actors.DeriveActorID(req.IndependenceBasis, candidateBasis)
					if err == nil {
						candidateActorID = id
					}
				}
			case actors.BasisModelFamilyAccount:
				id, err := actors.DeriveActorID(req.IndependenceBasis, candidateBasis)
				if err != nil {
					// Cannot prove independence if provider, model_family, or account_ref is missing
					continue
				}
				candidateActorID = id
			}

			isExcluded := false
			for _, eb := range req.ExcludeBases {
				exclID, err := actors.DeriveActorID(req.IndependenceBasis, eb)
				if err != nil {
					return ResolvedEndpoint{}, errs.Wrap(errs.CategoryInvalidArgument, err, "derive excluded actor id")
				}
				if candidateActorID != "" && candidateActorID == exclID {
					isExcluded = true
					break
				}
				if req.IndependenceBasis == actors.BasisEndpointModel {
					if candidateBasis.EndpointID == eb.EndpointID && candidateBasis.ModelID == eb.ModelID {
						isExcluded = true
						break
					}
				}
			}
			if isExcluded {
				continue
			}
		}

		return ResolvedEndpoint{
			EndpointID:      cand.endpointID,
			ModelID:         cand.modelID,
			ModelRevision:   "",
			RuntimeVersion:  "",
			DriverID:        "",
			Provider:        cand.provider,
			ModelFamily:     cand.modelFamily,
			AccountRef:      cand.accountRef,
			Kind:            cand.kind,
			Locality:        cand.locality,
			Channel:         cand.channel,
			ContextProfile:  cand.contextProfile,
			Limits:          cand.limits,
			PolicyDigest:    policyDigest,
			PortfolioDigest: portfolioDigest,
			BindingDigest:   "",
		}, nil
	}

	return ResolvedEndpoint{}, principal.NewCodedError(
		principal.CodeModelUnavailable,
		false,
		[]string{"no-eligible-endpoint"},
		fmt.Sprintf("no eligible endpoint for role %q", req.Role),
	)
}

func roleAllowed(roles []string, role string) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}

func channelToEndpointKind(kind protocol.ChannelKind) protocol.EndpointKind {
	switch kind {
	case protocol.ChannelLocalDaemonSocket:
		return protocol.EndpointLocalRuntime
	case protocol.ChannelCLISubprocess:
		return protocol.EndpointAuthenticatedCLI
	case protocol.ChannelDirectHTTPAPI, protocol.ChannelRemoteAgentProxy:
		return protocol.EndpointRemoteAPI
	default:
		return protocol.EndpointLocalRuntime
	}
}
