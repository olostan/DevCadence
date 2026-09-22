package setup

import (
	"testing"

	"github.com/olostan/DevCadence/internal/protocol"
)


func TestProfileRecommenderLocalHeavy(t *testing.T) {
	rec := NewProfileRecommender()

	facts := protocol.EnvironmentFacts{
		Memory: protocol.MemoryFacts{
			TotalBytes: int64Ptr(64 * 1024 * 1024 * 1024),
		},
		Accelerators: []protocol.AcceleratorDevice{
			{
				ID:    "apple:gpu",
				Class: protocol.AcceleratorUnified,
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

	res := rec.Recommend(facts, endpoints)
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
		Memory: protocol.MemoryFacts{
			TotalBytes: int64Ptr(16 * 1024 * 1024 * 1024),
		},
		Accelerators: []protocol.AcceleratorDevice{
			{
				ID:    "apple:gpu",
				Class: protocol.AcceleratorUnified,
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

	res := rec.Recommend(facts, endpoints)
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

	res := rec.Recommend(facts, endpoints)
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
