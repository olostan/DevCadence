package protocol_test

import (
	"testing"

	"github.com/olostan/DevCadence/internal/protocol"
)

func validPortfolio() *protocol.CognitionPortfolio {
	ch := *validAccessChannel()
	bp := *validBudgetPool()
	return &protocol.CognitionPortfolio{
		SchemaVersion: protocol.SchemaVersion1,
		PortfolioID:   "port_1",
		Revision:      1,
		CreatedAt:     "2026-09-30T00:00:00Z",
		Channels:      []protocol.AccessChannel{ch},
		RoleBindings: []protocol.RoleBinding{
			{
				Role:             "principal",
				EndpointID:       "ep_1",
				ChannelID:        "chan_1",
				BudgetPoolID:     "pool_1",
				ContextProfileID: "prof_1",
			},
		},
		BudgetPools:       []protocol.BudgetPool{bp},
		MaxSourceExposure: protocol.ExposureFocusedSnippets,
	}
}

func TestPortfolioValidation(t *testing.T) {
	t.Run("valid portfolio passes", func(t *testing.T) {
		p := validPortfolio()
		if err := p.Validate(); err != nil {
			t.Fatalf("expected valid, got: %v", err)
		}
		if p.RecordKind() != "CognitionPortfolio" {
			t.Errorf("record kind: got %q, want CognitionPortfolio", p.RecordKind())
		}
	})

	t.Run("invalid max_source_exposure rejected", func(t *testing.T) {
		p := validPortfolio()
		p.MaxSourceExposure = "broadcast_to_world"
		if err := p.Validate(); err == nil {
			t.Fatal("expected error on invalid source exposure, got nil")
		}
	})
}

func TestPortfolioRecommendationValidation(t *testing.T) {
	t.Run("valid recommendation passes", func(t *testing.T) {
		p := *validPortfolio()
		rec := &protocol.PortfolioRecommendation{
			SchemaVersion:          protocol.SchemaVersion1,
			RecommendationID:       "rec_1",
			InventoryDigest:        "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			SynthesizedAt:          "2026-09-30T00:00:00Z",
			RecommendedPortfolio:   p,
			Rationale:              "Optimal configuration for machine",
			ExplanatoryDiagnostics: []string{"Diagnosed healthy endpoints"},
			CapabilityProvenance:   []string{"prov_1"},
		}
		if err := rec.Validate(); err != nil {
			t.Fatalf("expected valid recommendation, got: %v", err)
		}
		if rec.RecordKind() != "PortfolioRecommendation" {
			t.Errorf("record kind: got %q, want PortfolioRecommendation", rec.RecordKind())
		}
	})
}

func TestWorkflowPlanValidation(t *testing.T) {
	t.Run("valid workflow plan passes", func(t *testing.T) {
		plan := &protocol.WorkflowPlan{
			SchemaVersion: protocol.SchemaVersion1,
			PlanID:        "plan_1",
			TaskID:        "task_1",
			WorkPackageID: "WP-M3C-1",
			Topology:      protocol.TopologyIterativeEscalation,
			Stages: []protocol.WorkflowStage{
				{
					StageID:        "stage_1",
					Role:           "implementer",
					Order:          1,
					BudgetPoolID:   "pool_1",
					TimeoutSeconds: 600,
				},
			},
		}
		if err := plan.Validate(); err != nil {
			t.Fatalf("expected valid WorkflowPlan, got: %v", err)
		}
		if plan.RecordKind() != "WorkflowPlan" {
			t.Errorf("record kind: got %q, want WorkflowPlan", plan.RecordKind())
		}
	})

	t.Run("invalid topology rejected", func(t *testing.T) {
		plan := &protocol.WorkflowPlan{
			SchemaVersion: protocol.SchemaVersion1,
			PlanID:        "plan_1",
			TaskID:        "task_1",
			WorkPackageID: "WP-M3C-1",
			Topology:      "random_walk",
			Stages:        []protocol.WorkflowStage{},
		}
		if err := plan.Validate(); err == nil {
			t.Fatal("expected error on invalid topology, got nil")
		}
	})
}
