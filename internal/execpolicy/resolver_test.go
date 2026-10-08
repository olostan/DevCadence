package execpolicy_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/actors"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/execpolicy"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/protocol"
)

func sampleLimits() execpolicy.ExecutionLimits {
	return execpolicy.ExecutionLimits{
		MaxTurns:               10,
		MaxToolCalls:           20,
		MaxTotalTokens:         100000,
		MaxDurationSeconds:     300,
		MaxOutputTokensPerCall: 4096,
		MaxRequestBytes:        65536,
		MaxAPISpendMicroUSD:    500000,
	}
}

func samplePolicy(grants ...execpolicy.EndpointGrant) execpolicy.ExecutionPolicy {
	now := time.Now().UTC()
	return execpolicy.ExecutionPolicy{
		Version:            "1.0",
		PolicyID:           "test-policy",
		Revision:           1,
		NotBefore:          now.Add(-1 * time.Hour).Format(time.RFC3339),
		NotAfter:           now.Add(24 * time.Hour).Format(time.RFC3339),
		MaxAttemptsPerTask: 3,
		Grants:             grants,
	}
}

func samplePortfolio(endpoints ...string) *protocol.CognitionPortfolio {
	channels := make([]protocol.AccessChannel, 0, len(endpoints))
	roleBindings := make([]protocol.RoleBinding, 0, len(endpoints))
	for i, ep := range endpoints {
		chanID := "chan-" + ep
		profID := "prof-" + ep
		channels = append(channels, protocol.AccessChannel{
			SchemaVersion:         protocol.SchemaVersion1,
			ChannelID:             chanID,
			EndpointID:            ep,
			Kind:                  protocol.ChannelCLISubprocess,
			SessionMode:           protocol.SessionStatelessPerCall,
			ContextControl:        protocol.ContextControlExactStateless,
			PrefixCache:           protocol.PrefixCacheNone,
			MaxConcurrentRequests: 2,
		})
		roleBindings = append(roleBindings, protocol.RoleBinding{
			Role:             "implementer",
			EndpointID:       ep,
			ChannelID:        chanID,
			BudgetPoolID:     "pool-default",
			ContextProfileID: profID,
			Priority:         i + 1,
		})
	}

	return &protocol.CognitionPortfolio{
		SchemaVersion: protocol.SchemaVersion1,
		PortfolioID:   "port-test",
		Revision:      1,
		CreatedAt:     "2026-10-01T00:00:00Z",
		Channels:      channels,
		RoleBindings:  roleBindings,
		BudgetPools: []protocol.BudgetPool{{
			SchemaVersion:  protocol.SchemaVersion1,
			PoolID:         "pool-default",
			Name:           "Default Pool",
			Regime:         protocol.RegimeLocalCompute,
			Unit:           protocol.UnitSeconds,
			HardLimit:      1000,
			SoftAlertLimit: 800,
			Period:         protocol.PeriodPerTask,
		}},
		MaxSourceExposure: protocol.ExposureSelectedFiles,
	}
}

func TestEndpointRequest_Validate(t *testing.T) {
	validReq := execpolicy.EndpointRequest{
		ProjectID: "proj-1",
		TaskID:    "task-1",
		Role:      "implementer",
	}

	if err := validReq.Validate(); err != nil {
		t.Fatalf("expected valid request, got %v", err)
	}

	t.Run("missing project_id", func(t *testing.T) {
		r := validReq
		r.ProjectID = ""
		if err := r.Validate(); err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got %v", err)
		}
	})

	t.Run("missing task_id", func(t *testing.T) {
		r := validReq
		r.TaskID = ""
		if err := r.Validate(); err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got %v", err)
		}
	})

	t.Run("missing role", func(t *testing.T) {
		r := validReq
		r.Role = ""
		if err := r.Validate(); err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got %v", err)
		}
	})

	t.Run("exclude bases without independence basis", func(t *testing.T) {
		r := validReq
		r.ExcludeBases = []protocol.ActorBasis{{EndpointID: "ep-1", ModelID: "m-1", ModelRevision: "rev-1"}}
		r.IndependenceBasis = ""
		if err := r.Validate(); err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got %v", err)
		}
	})

	t.Run("exclude bases with invalid independence basis", func(t *testing.T) {
		r := validReq
		r.ExcludeBases = []protocol.ActorBasis{{EndpointID: "ep-1", ModelID: "m-1", ModelRevision: "rev-1"}}
		r.IndependenceBasis = "invalid_basis"
		if err := r.Validate(); err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got %v", err)
		}
	})

	t.Run("exclude bases with valid endpoint_model basis", func(t *testing.T) {
		r := validReq
		r.ExcludeBases = []protocol.ActorBasis{{EndpointID: "ep-1", ModelID: "m-1", ModelRevision: "rev-1"}}
		r.IndependenceBasis = actors.BasisEndpointModel
		if err := r.Validate(); err != nil {
			t.Errorf("expected valid, got %v", err)
		}
	})

	t.Run("exclude bases with invalid basis field for endpoint_model", func(t *testing.T) {
		r := validReq
		r.ExcludeBases = []protocol.ActorBasis{{EndpointID: "ep-1", ModelID: "m-1", ModelRevision: ""}} // missing revision
		r.IndependenceBasis = actors.BasisEndpointModel
		if err := r.Validate(); err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got %v", err)
		}
	})

	t.Run("exclude bases with valid model_family_account basis", func(t *testing.T) {
		r := validReq
		r.ExcludeBases = []protocol.ActorBasis{{Provider: "anthropic", ModelFamily: "claude-3-5", AccountRef: "acc-1"}}
		r.IndependenceBasis = actors.BasisModelFamilyAccount
		if err := r.Validate(); err != nil {
			t.Errorf("expected valid, got %v", err)
		}
	})
}

func TestPortfolioEndpointResolver_Resolve_Success(t *testing.T) {
	portfolio := samplePortfolio("ep-1")
	resolver := execpolicy.NewPortfolioEndpointResolver(portfolio)

	policy := samplePolicy(execpolicy.EndpointGrant{
		EndpointID:     "ep-1",
		ModelID:        "claude-3-5-sonnet",
		Roles:          []string{"implementer"},
		Locality:       protocol.LocalityRemote,
		SourceExposure: protocol.ExposureSelectedFiles,
		ChannelKind:    protocol.ChannelCLISubprocess,
		Limits:         sampleLimits(),
	})

	req := execpolicy.EndpointRequest{
		ProjectID:       "proj-1",
		TaskID:          "task-1",
		Role:            "implementer",
		ContextNeed:     protocol.ExposureFocusedSnippets,
		PortfolioDigest: "digest-portfolio-1",
	}

	resolved, err := resolver.Resolve(context.Background(), req, policy, "digest-policy-1")
	if err != nil {
		t.Fatalf("unexpected resolve error: %v", err)
	}

	if resolved.EndpointID != "ep-1" {
		t.Errorf("EndpointID = %q, want ep-1", resolved.EndpointID)
	}
	if resolved.ModelID != "claude-3-5-sonnet" {
		t.Errorf("ModelID = %q, want claude-3-5-sonnet", resolved.ModelID)
	}
	if resolved.PolicyDigest != "digest-policy-1" {
		t.Errorf("PolicyDigest = %q, want digest-policy-1", resolved.PolicyDigest)
	}
	if resolved.PortfolioDigest != "digest-portfolio-1" {
		t.Errorf("PortfolioDigest = %q, want digest-portfolio-1", resolved.PortfolioDigest)
	}
	if resolved.Channel.ChannelID != "chan-ep-1" {
		t.Errorf("ChannelID = %q, want chan-ep-1", resolved.Channel.ChannelID)
	}
	if resolved.Limits.MaxTurns != 10 {
		t.Errorf("Limits.MaxTurns = %d, want 10", resolved.Limits.MaxTurns)
	}

	// Verify unbound fields are empty
	if resolved.ModelRevision != "" || resolved.RuntimeVersion != "" || resolved.DriverID != "" || resolved.BindingDigest != "" {
		t.Errorf("expected unbound fields to be empty, got %+v", resolved)
	}

	// Check ActorBasis
	basis := resolved.ActorBasis()
	if basis.EndpointID != "ep-1" || basis.ModelID != "claude-3-5-sonnet" {
		t.Errorf("ActorBasis unexpected: %+v", basis)
	}
}

func TestPortfolioEndpointResolver_Resolve_NoGrantMatch(t *testing.T) {
	portfolio := samplePortfolio("ep-1")
	resolver := execpolicy.NewPortfolioEndpointResolver(portfolio)

	policy := samplePolicy(execpolicy.EndpointGrant{
		EndpointID:     "ep-1",
		ModelID:        "claude-3-5-sonnet",
		Roles:          []string{"implementer"},
		Locality:       protocol.LocalityRemote,
		SourceExposure: protocol.ExposureFocusedSnippets, // rank 2
		ChannelKind:    protocol.ChannelCLISubprocess,
		Limits:         sampleLimits(),
	})

	t.Run("role mismatch", func(t *testing.T) {
		req := execpolicy.EndpointRequest{
			ProjectID:   "proj-1",
			TaskID:      "task-1",
			Role:        "reviewer", // grant only has "implementer"
			ContextNeed: protocol.ExposureFocusedSnippets,
		}
		_, err := resolver.Resolve(context.Background(), req, policy, "policy-digest")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		coded, ok := err.(*principal.CodedError)
		if !ok {
			t.Fatalf("expected *principal.CodedError, got %T: %v", err, err)
		}
		if coded.Code() != principal.CodeModelUnavailable {
			t.Errorf("Code = %q, want %q", coded.Code(), principal.CodeModelUnavailable)
		}
		if len(coded.EvidenceRefs()) == 0 || coded.EvidenceRefs()[0] != "no-eligible-endpoint" {
			t.Errorf("EvidenceRefs = %v, want [no-eligible-endpoint]", coded.EvidenceRefs())
		}
	})

	t.Run("exposure insufficient", func(t *testing.T) {
		req := execpolicy.EndpointRequest{
			ProjectID:   "proj-1",
			TaskID:      "task-1",
			Role:        "implementer",
			ContextNeed: protocol.ExposureSelectedFiles, // rank 3 > rank 2 of grant
		}
		_, err := resolver.Resolve(context.Background(), req, policy, "policy-digest")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		coded, ok := err.(*principal.CodedError)
		if !ok {
			t.Fatalf("expected *principal.CodedError, got %T: %v", err, err)
		}
		if coded.Code() != principal.CodeModelUnavailable {
			t.Errorf("Code = %q, want %q", coded.Code(), principal.CodeModelUnavailable)
		}
	})
}

func TestPortfolioEndpointResolver_Exclusion_EndpointModel(t *testing.T) {
	portfolio := samplePortfolio("ep-1", "ep-2")
	resolver := execpolicy.NewPortfolioEndpointResolver(portfolio)

	policy := samplePolicy(
		execpolicy.EndpointGrant{
			EndpointID:     "ep-1",
			ModelID:        "model-A",
			Roles:          []string{"reviewer"},
			Locality:       protocol.LocalityRemote,
			SourceExposure: protocol.ExposureSelectedFiles,
			ChannelKind:    protocol.ChannelCLISubprocess,
			Limits:         sampleLimits(),
		},
		execpolicy.EndpointGrant{
			EndpointID:     "ep-2",
			ModelID:        "model-B",
			Roles:          []string{"reviewer"},
			Locality:       protocol.LocalityRemote,
			SourceExposure: protocol.ExposureSelectedFiles,
			ChannelKind:    protocol.ChannelCLISubprocess,
			Limits:         sampleLimits(),
		},
	)

	// Exclude ep-1 / model-A
	req := execpolicy.EndpointRequest{
		ProjectID:         "proj-1",
		TaskID:            "task-1",
		Role:              "reviewer",
		ContextNeed:       protocol.ExposureFocusedSnippets,
		IndependenceBasis: actors.BasisEndpointModel,
		ExcludeBases: []protocol.ActorBasis{
			{
				EndpointID:    "ep-1",
				ModelID:       "model-A",
				ModelRevision: "rev-1",
			},
		},
	}

	resolved, err := resolver.Resolve(context.Background(), req, policy, "policy-digest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ep-1 is excluded, should resolve to ep-2
	if resolved.EndpointID != "ep-2" {
		t.Errorf("EndpointID = %q, want ep-2", resolved.EndpointID)
	}
	if resolved.ModelID != "model-B" {
		t.Errorf("ModelID = %q, want model-B", resolved.ModelID)
	}

	// Now exclude both ep-1 and ep-2
	req.ExcludeBases = append(req.ExcludeBases, protocol.ActorBasis{
		EndpointID:    "ep-2",
		ModelID:       "model-B",
		ModelRevision: "rev-2",
	})
	_, err = resolver.Resolve(context.Background(), req, policy, "policy-digest")
	if err == nil {
		t.Fatal("expected no eligible endpoint error, got nil")
	}
	coded, ok := err.(*principal.CodedError)
	if !ok || coded.Code() != principal.CodeModelUnavailable {
		t.Errorf("expected CodeModelUnavailable, got %v", err)
	}
}

func TestPortfolioEndpointResolver_Exclusion_ModelFamilyAccount(t *testing.T) {
	portfolio := samplePortfolio("ep-1", "ep-2")
	resolver := execpolicy.NewPortfolioEndpointResolver(portfolio)

	// Add endpoints with explicit provider, model family, account ref
	resolver.WithEndpoints(
		protocol.CognitionEndpoint{
			ID:          "ep-1",
			ModelID:     "claude-3-5-sonnet",
			Provider:    "anthropic",
			ModelFamily: "claude-3-5",
			AccountRef:  "account-prod",
			Kind:        protocol.EndpointAuthenticatedCLI,
			Locality:    protocol.LocalityRemote,
		},
		protocol.CognitionEndpoint{
			ID:          "ep-2",
			ModelID:     "gpt-4o",
			Provider:    "openai",
			ModelFamily: "gpt-4",
			AccountRef:  "account-openai",
			Kind:        protocol.EndpointAuthenticatedCLI,
			Locality:    protocol.LocalityRemote,
		},
	)

	policy := samplePolicy(
		execpolicy.EndpointGrant{
			EndpointID:     "ep-1",
			ModelID:        "claude-3-5-sonnet",
			Roles:          []string{"reviewer"},
			Locality:       protocol.LocalityRemote,
			SourceExposure: protocol.ExposureSelectedFiles,
			ChannelKind:    protocol.ChannelCLISubprocess,
			Limits:         sampleLimits(),
		},
		execpolicy.EndpointGrant{
			EndpointID:     "ep-2",
			ModelID:        "gpt-4o",
			Roles:          []string{"reviewer"},
			Locality:       protocol.LocalityRemote,
			SourceExposure: protocol.ExposureSelectedFiles,
			ChannelKind:    protocol.ChannelCLISubprocess,
			Limits:         sampleLimits(),
		},
	)

	// Exclude ep-1 by model_family_account basis (same provider, model_family, account_ref)
	req := execpolicy.EndpointRequest{
		ProjectID:         "proj-1",
		TaskID:            "task-1",
		Role:              "reviewer",
		ContextNeed:       protocol.ExposureFocusedSnippets,
		IndependenceBasis: actors.BasisModelFamilyAccount,
		ExcludeBases: []protocol.ActorBasis{
			{
				Provider:    "anthropic",
				ModelFamily: "claude-3-5",
				AccountRef:  "account-prod",
			},
		},
	}

	resolved, err := resolver.Resolve(context.Background(), req, policy, "policy-digest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ep-1 should be excluded by model_family_account collision; ep-2 chosen
	if resolved.EndpointID != "ep-2" {
		t.Errorf("EndpointID = %q, want ep-2", resolved.EndpointID)
	}
	if resolved.ModelID != "gpt-4o" {
		t.Errorf("ModelID = %q, want gpt-4o", resolved.ModelID)
	}
	if resolved.Provider != "openai" {
		t.Errorf("Provider = %q, want openai", resolved.Provider)
	}
}
