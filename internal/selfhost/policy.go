package selfhost

import (
	"context"
	"time"

	"github.com/olostan/DevCadence/internal/execpolicy"
	"github.com/olostan/DevCadence/internal/protocol"
)

// PolicyProvenance marks the execution policy source built here. It is NOT the
// receipt-verified activation path of execpolicy.Load: no operator receipt
// exists for it. It is an explicit owner-local-development source, only
// constructible from the user-level Config (DevCadence home / environment),
// never from project-repository content, and it grants exactly one loopback
// local endpoint/model for the implementer role with zero spend and no network
// domains. Strict execpolicy.Load verification is unchanged and unused here.
const PolicyProvenance = "owner_local_unsigned"

// ownerLocalPolicy implements execpolicy.PolicySource for one local grant. The
// policy and its digest are fixed at construction so the digest bound into
// endpoint provenance is stable for the lifetime of the process.
type ownerLocalPolicy struct {
	policy execpolicy.ExecutionPolicy
	digest string
}

var _ execpolicy.PolicySource = (*ownerLocalPolicy)(nil)

func newOwnerLocalPolicy(cfg Config, now time.Time) (*ownerLocalPolicy, error) {
	now = now.UTC()
	policy := execpolicy.ExecutionPolicy{
		Version:            "1.0",
		PolicyID:           PolicyProvenance,
		Revision:           1,
		NotBefore:          now.Add(-time.Hour).Format(time.RFC3339),
		NotAfter:           now.Add(29 * 24 * time.Hour).Format(time.RFC3339),
		MaxAttemptsPerTask: 3,
		Grants: []execpolicy.EndpointGrant{{
			EndpointID:     cfg.EndpointID,
			ModelID:        cfg.Model,
			Roles:          []string{"implementer"},
			Locality:       protocol.LocalityLocal,
			SourceExposure: protocol.ExposureToolMediatedWorktree,
			ChannelKind:    protocol.ChannelDirectHTTPAPI,
			Limits: execpolicy.ExecutionLimits{
				MaxTurns:               24,
				MaxToolCalls:           80,
				MaxTotalTokens:         1_000_000,
				MaxDurationSeconds:     900,
				MaxOutputTokensPerCall: 4096,
				MaxRequestBytes:        262144,
				AllowUnknownUsage:      true, // local only; usage stays unknown unless Ollama reports it
				MaxAPISpendMicroUSD:    0,
			},
		}},
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	digest, err := policy.CanonicalDigest()
	if err != nil {
		return nil, err
	}
	return &ownerLocalPolicy{policy: policy, digest: digest}, nil
}

// Current returns the fixed policy and its canonical digest.
func (p *ownerLocalPolicy) Current(ctx context.Context) (execpolicy.ExecutionPolicy, string, error) {
	if err := ctx.Err(); err != nil {
		return execpolicy.ExecutionPolicy{}, "", err
	}
	return p.policy, p.digest, nil
}
