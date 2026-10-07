package execpolicy

import (
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// ExecutionLimits defines resource, token, and cost bounds for task execution.
type ExecutionLimits struct {
	MaxTurns               int   `json:"max_turns"`
	MaxToolCalls           int   `json:"max_tool_calls"`
	MaxTotalTokens         int64 `json:"max_total_tokens"`
	MaxDurationSeconds     int   `json:"max_duration_seconds"`
	MaxOutputTokensPerCall int64 `json:"max_output_tokens_per_call"`
	MaxRequestBytes        int   `json:"max_request_bytes"`
	AllowUnknownUsage      bool  `json:"allow_unknown_usage"`
	MaxAPISpendMicroUSD    int64 `json:"max_api_spend_micro_usd"`
}

// Validate checks limits validity under the given locality.
func (l ExecutionLimits) Validate(loc protocol.Locality) error {
	const kind = "ExecutionLimits"
	if !loc.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid locality %q", kind, string(loc))
	}
	if l.MaxOutputTokensPerCall <= 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: max_output_tokens_per_call must be > 0, got %d", kind, l.MaxOutputTokensPerCall)
	}
	if l.MaxRequestBytes <= 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: max_request_bytes must be > 0, got %d", kind, l.MaxRequestBytes)
	}
	if l.MaxTurns < 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: max_turns must be >= 0, got %d", kind, l.MaxTurns)
	}
	if l.MaxToolCalls < 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: max_tool_calls must be >= 0, got %d", kind, l.MaxToolCalls)
	}
	if l.MaxTotalTokens < 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: max_total_tokens must be >= 0, got %d", kind, l.MaxTotalTokens)
	}
	if l.MaxDurationSeconds < 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: max_duration_seconds must be >= 0, got %d", kind, l.MaxDurationSeconds)
	}
	if l.MaxAPISpendMicroUSD < 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: max_api_spend_micro_usd must be >= 0, got %d", kind, l.MaxAPISpendMicroUSD)
	}
	if l.AllowUnknownUsage {
		if loc != protocol.LocalityLocal {
			return errs.New(errs.CategoryInvalidArgument, "%s: allow_unknown_usage is true but locality is %q (valid only for local)", kind, string(loc))
		}
		if l.MaxAPISpendMicroUSD > 0 {
			return errs.New(errs.CategoryInvalidArgument, "%s: allow_unknown_usage cannot be true when max_api_spend_micro_usd > 0", kind)
		}
	}
	return nil
}

// EndpointGrant authorizes a specific endpoint and model combination for execution roles.
type EndpointGrant struct {
	EndpointID                    string                  `json:"endpoint_id"`
	ModelID                       string                  `json:"model_id"`
	Roles                         []string                `json:"roles"`
	Locality                      protocol.Locality       `json:"locality"`
	SourceExposure                protocol.SourceExposure `json:"source_exposure"`
	AllowedNetworkDomains         []string                `json:"allowed_network_domains"`
	ChannelKind                   protocol.ChannelKind    `json:"channel_kind"`
	AllowUnknownSubscriptionQuota bool                    `json:"allow_unknown_subscription_quota"`
	Limits                        ExecutionLimits         `json:"limits"`
}

// Validate checks grant completeness, valid enum values, and limit constraints.
func (g EndpointGrant) Validate() error {
	const kind = "EndpointGrant"
	if strings.TrimSpace(g.EndpointID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: endpoint_id is required", kind)
	}
	if strings.TrimSpace(g.ModelID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: model_id is required", kind)
	}
	if len(g.Roles) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: roles must not be empty", kind)
	}
	for _, r := range g.Roles {
		if strings.TrimSpace(r) == "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: role must not be empty", kind)
		}
	}
	if !g.Locality.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid locality %q", kind, string(g.Locality))
	}
	if !g.SourceExposure.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid source_exposure %q", kind, string(g.SourceExposure))
	}
	if !g.ChannelKind.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "%s: invalid channel_kind %q", kind, string(g.ChannelKind))
	}
	if g.Locality == protocol.LocalityLocal && len(g.AllowedNetworkDomains) > 0 {
		return errs.New(errs.CategoryInvalidArgument, "%s: allowed_network_domains must be empty for local locality", kind)
	}
	for _, d := range g.AllowedNetworkDomains {
		if strings.TrimSpace(d) == "" {
			return errs.New(errs.CategoryInvalidArgument, "%s: allowed_network_domain must not be empty", kind)
		}
	}
	if err := g.Limits.Validate(g.Locality); err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "%s: invalid limits", kind)
	}
	return nil
}

// ExecutionPolicy defines the complete execution governance policy.
type ExecutionPolicy struct {
	Version            string          `json:"version"`
	PolicyID           string          `json:"policy_id"`
	Revision           int             `json:"revision"`
	NotBefore          string          `json:"not_before"`
	NotAfter           string          `json:"not_after"`
	MaxAttemptsPerTask int             `json:"max_attempts_per_task"`
	Grants             []EndpointGrant `json:"grants"`
}

// Validate checks policy structure, validity window, attempt bounds, and grant ordering.
func (p ExecutionPolicy) Validate() error {
	const kind = "ExecutionPolicy"
	if p.Version != "1.0" {
		return errs.New(errs.CategoryInvalidArgument, "%s: version must be 1.0, got %q", kind, p.Version)
	}
	if strings.TrimSpace(p.PolicyID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: policy_id is required", kind)
	}
	if p.Revision < 1 {
		return errs.New(errs.CategoryInvalidArgument, "%s: revision must be >= 1, got %d", kind, p.Revision)
	}
	if strings.TrimSpace(p.NotBefore) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: not_before is required", kind)
	}
	if strings.TrimSpace(p.NotAfter) == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s: not_after is required", kind)
	}
	nb, err := time.Parse(time.RFC3339, p.NotBefore)
	if err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "%s: invalid not_before timestamp", kind)
	}
	if _, offset := nb.Zone(); offset != 0 || !strings.HasSuffix(p.NotBefore, "Z") {
		return errs.New(errs.CategoryInvalidArgument, "%s: not_before must be RFC3339 UTC ('Z' suffix)", kind)
	}
	na, err := time.Parse(time.RFC3339, p.NotAfter)
	if err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "%s: invalid not_after timestamp", kind)
	}
	if _, offset := na.Zone(); offset != 0 || !strings.HasSuffix(p.NotAfter, "Z") {
		return errs.New(errs.CategoryInvalidArgument, "%s: not_after must be RFC3339 UTC ('Z' suffix)", kind)
	}
	if !na.After(nb) {
		return errs.New(errs.CategoryInvalidArgument, "%s: not_after (%s) must be after not_before (%s)", kind, p.NotAfter, p.NotBefore)
	}
	if na.Sub(nb) > 30*24*time.Hour {
		return errs.New(errs.CategoryInvalidArgument, "%s: validity duration (%s) exceeds 30 days", kind, na.Sub(nb))
	}
	if p.MaxAttemptsPerTask < 1 || p.MaxAttemptsPerTask > 5 {
		return errs.New(errs.CategoryInvalidArgument, "%s: max_attempts_per_task must be between 1 and 5, got %d", kind, p.MaxAttemptsPerTask)
	}
	for i, g := range p.Grants {
		if err := g.Validate(); err != nil {
			return errs.Wrap(errs.CategoryInvalidArgument, err, "%s: grant[%d] invalid", kind, i)
		}
		if i > 0 {
			prev := p.Grants[i-1]
			if prev.EndpointID == g.EndpointID && prev.ModelID == g.ModelID {
				return errs.New(errs.CategoryInvalidArgument, "%s: duplicate grant for (endpoint_id=%q, model_id=%q)", kind, g.EndpointID, g.ModelID)
			}
			if g.EndpointID < prev.EndpointID || (g.EndpointID == prev.EndpointID && g.ModelID < prev.ModelID) {
				return errs.New(errs.CategoryInvalidArgument, "%s: grants must be sorted by (endpoint_id, model_id), got (%q, %q) after (%q, %q)",
					kind, g.EndpointID, g.ModelID, prev.EndpointID, prev.ModelID)
			}
		}
	}
	return nil
}

// CanonicalDigest computes the protocol canonical digest for the execution policy.
func (p ExecutionPolicy) CanonicalDigest() (string, error) {
	canonical, err := protocol.CanonicalJSON(p)
	if err != nil {
		return "", err
	}
	return protocol.DigestBytes(canonical), nil
}
