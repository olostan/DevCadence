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
				Role:                "principal",
				EndpointID:          "ep_1",
				ChannelID:           "chan_1",
				BudgetPoolID:        "pool_1",
				ContextProfileID:    "prof_1",
				Priority:            1,
				FallbackEndpointIDs: []string{"ep_fallback_01"},
			},
		},
		BudgetPools:       []protocol.BudgetPool{bp},
		MaxSourceExposure: protocol.ExposureFocusedSnippets,
		BudgetReservations: map[string]int64{
			"pool_1": 100,
		},
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

	t.Run("referential integrity: non-existent channel rejected", func(t *testing.T) {
		p := validPortfolio()
		p.RoleBindings[0].ChannelID = "non_existent_chan"
		if err := p.Validate(); err == nil {
			t.Fatal("expected error when role binding cites non-existent channel, got nil")
		}
	})

	t.Run("referential integrity: endpoint mismatch with channel rejected", func(t *testing.T) {
		p := validPortfolio()
		p.RoleBindings[0].EndpointID = "ep_mismatched"
		if err := p.Validate(); err == nil {
			t.Fatal("expected error when role binding endpoint does not match channel, got nil")
		}
	})

	t.Run("referential integrity: non-existent budget pool rejected", func(t *testing.T) {
		p := validPortfolio()
		p.RoleBindings[0].BudgetPoolID = "non_existent_pool"
		if err := p.Validate(); err == nil {
			t.Fatal("expected error when role binding cites non-existent pool, got nil")
		}
	})

	t.Run("excluded endpoint cannot be primary or fallback", func(t *testing.T) {
		p := validPortfolio()
		p.ExcludedEndpointIDs = []string{"ep_1"}
		if err := p.Validate(); err == nil {
			t.Fatal("expected error when primary endpoint is in excluded list, got nil")
		}

		p = validPortfolio()
		p.ExcludedEndpointIDs = []string{"ep_fallback_01"}
		if err := p.Validate(); err == nil {
			t.Fatal("expected error when fallback endpoint is in excluded list, got nil")
		}
	})

	t.Run("role binding fallback cannot match primary endpoint", func(t *testing.T) {
		p := validPortfolio()
		p.RoleBindings[0].FallbackEndpointIDs = []string{"ep_1"}
		if err := p.Validate(); err == nil {
			t.Fatal("expected error when fallback matches primary endpoint, got nil")
		}
	})

	t.Run("budget reservations integrity", func(t *testing.T) {
		p := validPortfolio()
		p.BudgetReservations = map[string]int64{"non_existent_pool": 50}
		if err := p.Validate(); err == nil {
			t.Fatal("expected error when reservation references non-existent pool, got nil")
		}

		p = validPortfolio()
		p.BudgetReservations = map[string]int64{"pool_1": -10}
		if err := p.Validate(); err == nil {
			t.Fatal("expected error on negative reservation amount, got nil")
		}
	})

	t.Run("duplicate channel or pool IDs rejected", func(t *testing.T) {
		p := validPortfolio()
		p.Channels = append(p.Channels, p.Channels[0])
		if err := p.Validate(); err == nil {
			t.Fatal("expected error on duplicate channel ID, got nil")
		}

		p = validPortfolio()
		p.BudgetPools = append(p.BudgetPools, p.BudgetPools[0])
		if err := p.Validate(); err == nil {
			t.Fatal("expected error on duplicate pool ID, got nil")
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
				{
					StageID:        "stage_2",
					Role:           "reviewer",
					Order:          2,
					DependsOn:      []string{"stage_1"},
					BudgetPoolID:   "pool_1",
					TimeoutSeconds: 300,
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

	t.Run("empty stages rejected", func(t *testing.T) {
		plan := &protocol.WorkflowPlan{
			SchemaVersion: protocol.SchemaVersion1,
			PlanID:        "plan_1",
			TaskID:        "task_1",
			WorkPackageID: "WP-M3C-1",
			Topology:      protocol.TopologySinglePass,
			Stages:        []protocol.WorkflowStage{},
		}
		if err := plan.Validate(); err == nil {
			t.Fatal("expected error on empty stages, got nil")
		}
	})

	t.Run("duplicate stage ID or duplicate order rejected", func(t *testing.T) {
		plan := &protocol.WorkflowPlan{
			SchemaVersion: protocol.SchemaVersion1,
			PlanID:        "plan_1",
			TaskID:        "task_1",
			WorkPackageID: "WP-M3C-1",
			Topology:      protocol.TopologySinglePass,
			Stages: []protocol.WorkflowStage{
				{StageID: "s1", Role: "implementer", Order: 1, BudgetPoolID: "p1", TimeoutSeconds: 100},
				{StageID: "s1", Role: "reviewer", Order: 2, BudgetPoolID: "p1", TimeoutSeconds: 100},
			},
		}
		if err := plan.Validate(); err == nil {
			t.Fatal("expected error on duplicate stage ID, got nil")
		}

		plan.Stages[1].StageID = "s2"
		plan.Stages[1].Order = 1
		if err := plan.Validate(); err == nil {
			t.Fatal("expected error on duplicate stage order, got nil")
		}
	})

	t.Run("DAG dependency checks: self dependency rejected", func(t *testing.T) {
		plan := &protocol.WorkflowPlan{
			SchemaVersion: protocol.SchemaVersion1,
			PlanID:        "plan_1",
			TaskID:        "task_1",
			WorkPackageID: "WP-M3C-1",
			Topology:      protocol.TopologySinglePass,
			Stages: []protocol.WorkflowStage{
				{StageID: "s1", Role: "implementer", Order: 1, DependsOn: []string{"s1"}, BudgetPoolID: "p1", TimeoutSeconds: 100},
			},
		}
		if err := plan.Validate(); err == nil {
			t.Fatal("expected error on self dependency, got nil")
		}
	})

	t.Run("DAG dependency checks: non-existent dependency rejected", func(t *testing.T) {
		plan := &protocol.WorkflowPlan{
			SchemaVersion: protocol.SchemaVersion1,
			PlanID:        "plan_1",
			TaskID:        "task_1",
			WorkPackageID: "WP-M3C-1",
			Topology:      protocol.TopologySinglePass,
			Stages: []protocol.WorkflowStage{
				{StageID: "s1", Role: "implementer", Order: 1, DependsOn: []string{"ghost_stage"}, BudgetPoolID: "p1", TimeoutSeconds: 100},
			},
		}
		if err := plan.Validate(); err == nil {
			t.Fatal("expected error on non-existent dependency, got nil")
		}
	})

	t.Run("DAG dependency checks: out of order dependency rejected", func(t *testing.T) {
		plan := &protocol.WorkflowPlan{
			SchemaVersion: protocol.SchemaVersion1,
			PlanID:        "plan_1",
			TaskID:        "task_1",
			WorkPackageID: "WP-M3C-1",
			Topology:      protocol.TopologyIterativeEscalation,
			Stages: []protocol.WorkflowStage{
				{StageID: "s1", Role: "implementer", Order: 1, DependsOn: []string{"s2"}, BudgetPoolID: "p1", TimeoutSeconds: 100},
				{StageID: "s2", Role: "reviewer", Order: 2, BudgetPoolID: "p1", TimeoutSeconds: 100},
			},
		}
		if err := plan.Validate(); err == nil {
			t.Fatal("expected error on backwards dependency, got nil")
		}
	})

	t.Run("topology compatibility: TopologyDualIndependentReview requires >= 2 review stages", func(t *testing.T) {
		plan := &protocol.WorkflowPlan{
			SchemaVersion: protocol.SchemaVersion1,
			PlanID:        "plan_1",
			TaskID:        "task_1",
			WorkPackageID: "WP-M3C-1",
			Topology:      protocol.TopologyDualIndependentReview,
			Stages: []protocol.WorkflowStage{
				{StageID: "s1", Role: "implementer", Order: 1, BudgetPoolID: "p1", TimeoutSeconds: 100},
				{StageID: "s2", Role: "reviewer_alpha", Order: 2, DependsOn: []string{"s1"}, BudgetPoolID: "p1", TimeoutSeconds: 100},
			},
		}
		if err := plan.Validate(); err == nil {
			t.Fatal("expected error when DualIndependentReview has only 1 reviewer, got nil")
		}

		// Add second review stage
		plan.Stages = append(plan.Stages, protocol.WorkflowStage{
			StageID:        "s3",
			Role:           "reviewer_beta",
			Order:          3,
			DependsOn:      []string{"s1"},
			BudgetPoolID:   "p1",
			TimeoutSeconds: 100,
		})
		if err := plan.Validate(); err != nil {
			t.Fatalf("expected valid DualIndependentReview with 2 reviewers, got: %v", err)
		}
	})

	t.Run("topology compatibility: TopologyDeterministicOnly permits only deterministic stages", func(t *testing.T) {
		plan := &protocol.WorkflowPlan{
			SchemaVersion: protocol.SchemaVersion1,
			PlanID:        "plan_1",
			TaskID:        "task_1",
			WorkPackageID: "WP-M3C-1",
			Topology:      protocol.TopologyDeterministicOnly,
			Stages: []protocol.WorkflowStage{
				{StageID: "s1", Role: "principal_engineer", Order: 1, BudgetPoolID: "p1", TimeoutSeconds: 100},
			},
		}
		if err := plan.Validate(); err == nil {
			t.Fatal("expected error when DeterministicOnly has generative role, got nil")
		}

		plan.Stages[0].Role = "verifier"
		if err := plan.Validate(); err != nil {
			t.Fatalf("expected valid DeterministicOnly with verifier role, got: %v", err)
		}
	})
}
