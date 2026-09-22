package setup

import (
	"testing"

	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/protocol"
)

func TestProfileRecommenderLocalHeavy(t *testing.T) {
	rec := NewProfileRecommender()

	facts := protocol.EnvironmentFacts{
		Host: protocol.HostFacts{
			Family: protocol.OSDarwin,
			Arch:   "arm64",
		},
		Memory: protocol.MemoryFacts{
			TotalBytes: int64Ptr(64 * 1024 * 1024 * 1024),
		},
		Accelerators: []protocol.AcceleratorDevice{
			{
				ID:     "apple:gpu",
				Vendor: protocol.VendorApple,
				Class:  protocol.AcceleratorUnified,
			},
		},
	}
	backend := protocol.BackendMetal
	endpoints := []protocol.CognitionEndpointSummary{
		{
			ID:                   "ollama_local",
			Kind:                 protocol.EndpointLocalRuntime,
			Health:               protocol.EndpointHealthReady,
			AccelerationVerified: true,
			AccelerationBackend:  &backend,
		},
	}

	res := rec.Recommend(RecommendationInput{
		Facts:     facts,
		Endpoints: endpoints,
	})
	if res.SelectedProfile == nil || *res.SelectedProfile != protocol.ProfileLocalHeavy {
		t.Fatalf("expected profile %q, got %v", protocol.ProfileLocalHeavy, res.SelectedProfile)
	}

	altMap := make(map[protocol.DeploymentProfile]protocol.ProfileAlternative)
	for _, alt := range res.Alternatives {
		altMap[alt.Profile] = alt
	}

	if !altMap[protocol.ProfileLocalHeavy].Eligible {
		t.Errorf("expected local-heavy to be eligible")
	}
	if !altMap[protocol.ProfileOffline].Eligible {
		t.Errorf("expected offline to be eligible")
	}
}

func TestProfileRecommenderHybridThin(t *testing.T) {
	rec := NewProfileRecommender()

	facts := protocol.EnvironmentFacts{
		Host: protocol.HostFacts{
			Family: protocol.OSDarwin,
			Arch:   "arm64",
		},
		Memory: protocol.MemoryFacts{
			TotalBytes: int64Ptr(16 * 1024 * 1024 * 1024),
		},
		Accelerators: []protocol.AcceleratorDevice{
			{
				ID:     "apple:gpu",
				Vendor: protocol.VendorApple,
				Class:  protocol.AcceleratorUnified,
			},
		},
	}
	backend := protocol.BackendMetal
	endpoints := []protocol.CognitionEndpointSummary{
		{
			ID:                   "ollama_local",
			Kind:                 protocol.EndpointLocalRuntime,
			Health:               protocol.EndpointHealthReady,
			AccelerationVerified: true,
			AccelerationBackend:  &backend,
		},
		{
			ID:     "gemini_api",
			Kind:   protocol.EndpointRemoteAPI,
			Health: protocol.EndpointHealthReady,
			Auth:   protocol.AuthAuthenticated,
		},
	}

	res := rec.Recommend(RecommendationInput{
		Facts:     facts,
		Endpoints: endpoints,
	})
	if res.SelectedProfile == nil || *res.SelectedProfile != protocol.ProfileHybridThin {
		t.Fatalf("expected profile %q, got %v", protocol.ProfileHybridThin, res.SelectedProfile)
	}

	altMap := make(map[protocol.DeploymentProfile]protocol.ProfileAlternative)
	for _, alt := range res.Alternatives {
		altMap[alt.Profile] = alt
	}

	if altMap[protocol.ProfileLocalHeavy].Eligible {
		t.Errorf("expected local-heavy to be ineligible with 16GB memory")
	}
	if !altMap[protocol.ProfileHybridThin].Eligible {
		t.Errorf("expected hybrid-thin to be eligible")
	}
	if !altMap[protocol.ProfileCloudCognition].Eligible {
		t.Errorf("expected cloud-cognition to be eligible")
	}
}

func TestProfileRecommenderCloudCognitionFallback(t *testing.T) {
	rec := NewProfileRecommender()

	// 8GB RAM, no local accelerator, but authenticated coding CLI
	facts := protocol.EnvironmentFacts{
		Memory: protocol.MemoryFacts{
			TotalBytes: int64Ptr(8 * 1024 * 1024 * 1024),
		},
	}
	endpoints := []protocol.CognitionEndpointSummary{
		{
			ID:     "claude_cli",
			Kind:   protocol.EndpointAuthenticatedCLI,
			Health: protocol.EndpointHealthReady,
			Auth:   protocol.AuthAuthenticated,
		},
	}

	res := rec.Recommend(RecommendationInput{
		Facts:     facts,
		Endpoints: endpoints,
	})
	if res.SelectedProfile == nil || *res.SelectedProfile != protocol.ProfileCloudCognition {
		t.Fatalf("expected profile %q, got %v", protocol.ProfileCloudCognition, res.SelectedProfile)
	}

	altMap := make(map[protocol.DeploymentProfile]protocol.ProfileAlternative)
	for _, alt := range res.Alternatives {
		altMap[alt.Profile] = alt
	}

	if altMap[protocol.ProfileLocalHeavy].Eligible {
		t.Errorf("expected local-heavy to be ineligible")
	}
	if altMap[protocol.ProfileHybridThin].Eligible {
		t.Errorf("expected hybrid-thin to be ineligible")
	}
	if !altMap[protocol.ProfileCloudCognition].Eligible {
		t.Errorf("expected cloud-cognition to be eligible")
	}
}

func TestProfileRecommenderNoneEligibleReturnsNil(t *testing.T) {
	rec := NewProfileRecommender()

	// 8GB RAM, no local accelerator, no authenticated remote endpoints
	facts := protocol.EnvironmentFacts{
		Memory: protocol.MemoryFacts{
			TotalBytes: int64Ptr(8 * 1024 * 1024 * 1024),
		},
	}
	endpoints := []protocol.CognitionEndpointSummary{
		{
			ID:     "claude_cli",
			Kind:   protocol.EndpointAuthenticatedCLI,
			Health: protocol.EndpointHealthUnhealthy,
			Auth:   protocol.AuthUnauthenticated,
		},
	}

	res := rec.Recommend(RecommendationInput{
		Facts:     facts,
		Endpoints: endpoints,
	})
	if res.SelectedProfile != nil {
		t.Fatalf("expected SelectedProfile to be nil when no profile is eligible, got %v", *res.SelectedProfile)
	}

	if len(res.Rationale) == 0 {
		t.Errorf("expected non-empty rationale explaining why nothing is eligible")
	}
}

func TestProfileRecommenderPolicyRestriction(t *testing.T) {
	rec := NewProfileRecommender()

	// Machine with remote endpoint that requires full files, but project policy permits only local
	facts := protocol.EnvironmentFacts{
		Memory: protocol.MemoryFacts{
			TotalBytes: int64Ptr(8 * 1024 * 1024 * 1024),
		},
	}
	endpoints := []protocol.CognitionEndpointSummary{
		{
			ID:                     "remote_frontier",
			Kind:                   protocol.EndpointRemoteAPI,
			Health:                 protocol.EndpointHealthReady,
			Auth:                   protocol.AuthAuthenticated,
			RequiredSourceExposure: protocol.ExposureSelectedFiles,
			CostClass:              protocol.CostFrontierExpensive,
		},
	}
	policy := &cognition.Policy{
		MaxSourceExposure: protocol.ExposureLocalOnly,
		MaxCostClass:      protocol.CostLocalCompute,
	}

	res := rec.Recommend(RecommendationInput{
		Facts:     facts,
		Endpoints: endpoints,
		Policy:    policy,
	})

	if res.SelectedProfile != nil {
		t.Fatalf("expected nil SelectedProfile when remote endpoint violates policy, got %v", *res.SelectedProfile)
	}
}

func TestProfileRecommenderUserPreferences(t *testing.T) {
	rec := NewProfileRecommender()

	facts := protocol.EnvironmentFacts{
		Host: protocol.HostFacts{
			Family: protocol.OSDarwin,
			Arch:   "arm64",
		},
		Memory: protocol.MemoryFacts{
			TotalBytes: int64Ptr(64 * 1024 * 1024 * 1024),
		},
		Accelerators: []protocol.AcceleratorDevice{
			{
				ID:     "apple:gpu",
				Vendor: protocol.VendorApple,
				Class:  protocol.AcceleratorUnified,
			},
		},
	}
	backend := protocol.BackendMetal
	endpoints := []protocol.CognitionEndpointSummary{
		{
			ID:                   "ollama_local",
			Kind:                 protocol.EndpointLocalRuntime,
			Health:               protocol.EndpointHealthReady,
			AccelerationVerified: true,
			AccelerationBackend:  &backend,
		},
		{
			ID:     "claude_cli",
			Kind:   protocol.EndpointAuthenticatedCLI,
			Health: protocol.EndpointHealthReady,
			Auth:   protocol.AuthAuthenticated,
		},
	}

	// Operator explicitly prefers hybrid-thin over local-heavy
	pref := protocol.ProfileHybridThin
	res := rec.Recommend(RecommendationInput{
		Facts:           facts,
		Endpoints:       endpoints,
		UserPreferences: &UserPreferences{PreferredProfile: &pref},
	})

	if res.SelectedProfile == nil || *res.SelectedProfile != protocol.ProfileHybridThin {
		t.Fatalf("expected preferred profile %q, got %v", protocol.ProfileHybridThin, res.SelectedProfile)
	}
}
