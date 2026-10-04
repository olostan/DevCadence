package workflowplanner_test

import (
	"bytes"
	"testing"

	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/cognition/workflowplanner"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

func ptr[T any](v T) *T { return &v }

func makeTestPlannerPortfolio() *protocol.CognitionPortfolio {
	return &protocol.CognitionPortfolio{
		SchemaVersion:     protocol.SchemaVersion1,
		PortfolioID:       "port-planner-01",
		Revision:          1,
		CreatedAt:         "2026-10-04T00:00:00Z",
		MaxSourceExposure: protocol.ExposureFocusedSnippets,
		Channels: []protocol.AccessChannel{
			{
				SchemaVersion:         protocol.SchemaVersion1,
				ChannelID:             "chan-local-01",
				EndpointID:            "ep-local-01",
				Kind:                  protocol.ChannelLocalDaemonSocket,
				SessionMode:           protocol.SessionStatelessPerCall,
				ContextControl:        protocol.ContextControlExactStateless,
				PrefixCache:           protocol.PrefixCacheSessionKV,
				SupportsStreaming:     true,
				SupportsTools:         true,
				NativeWorktreeAccess:  false,
				MaxConcurrentRequests: 2,
			},
			{
				SchemaVersion:         protocol.SchemaVersion1,
				ChannelID:             "chan-cli-01",
				EndpointID:            "ep-cli-01",
				Kind:                  protocol.ChannelCLISubprocess,
				SessionMode:           protocol.SessionResumableHandle,
				ContextControl:        protocol.ContextControlAppendOnly,
				PrefixCache:           protocol.PrefixCacheImplicit,
				SupportsStreaming:     true,
				SupportsTools:         true,
				NativeWorktreeAccess:  false,
				MaxConcurrentRequests: 1,
			},
			{
				SchemaVersion:         protocol.SchemaVersion1,
				ChannelID:             "chan-cloud-01",
				EndpointID:            "ep-cloud-01",
				Kind:                  protocol.ChannelDirectHTTPAPI,
				SessionMode:           protocol.SessionStatelessPerCall,
				ContextControl:        protocol.ContextControlExactStateless,
				PrefixCache:           protocol.PrefixCacheSessionKV,
				SupportsStreaming:     true,
				SupportsTools:         true,
				NativeWorktreeAccess:  false,
				MaxConcurrentRequests: 4,
			},
		},
		BudgetPools: []protocol.BudgetPool{
			{
				SchemaVersion:  protocol.SchemaVersion1,
				PoolID:         "pool-local",
				Name:           "Local Workstation Compute",
				Regime:         protocol.RegimeLocalCompute,
				HardLimit:      36000,
				SoftAlertLimit: 28800,
				Unit:           protocol.UnitSeconds,
				Period:         protocol.PeriodRollingDay,
			},
			{
				SchemaVersion:  protocol.SchemaVersion1,
				PoolID:         "pool-sub",
				Name:           "Subscription Pool",
				Regime:         protocol.RegimeSubscriptionQuota,
				HardLimit:      500,
				SoftAlertLimit: 400,
				Unit:           protocol.UnitRequests,
				Period:         protocol.PeriodBillingCycle,
			},
		},
		RoleBindings: []protocol.RoleBinding{
			{
				Role:             "implementer",
				EndpointID:       "ep-local-01",
				ChannelID:        "chan-local-01",
				BudgetPoolID:     "pool-local",
				ContextProfileID: "prof-local-01",
				Priority:         1,
			},
			{
				Role:             "reviewer",
				EndpointID:       "ep-cli-01",
				ChannelID:        "chan-cli-01",
				BudgetPoolID:     "pool-sub",
				ContextProfileID: "prof-cli-01",
				Priority:         1,
				Fallbacks: []protocol.FallbackBinding{
					{
						EndpointID:       "ep-cloud-01",
						ChannelID:        "chan-cloud-01",
						BudgetPoolID:     "pool-sub",
						ContextProfileID: "prof-cloud-01",
					},
				},
			},
			{
				Role:             "verifier",
				EndpointID:       "ep-cloud-01",
				ChannelID:        "chan-cloud-01",
				BudgetPoolID:     "pool-local",
				ContextProfileID: "prof-cloud-01",
				Priority:         1,
			},
		},
	}
}

func TestWorkflowPlanner_ACC01_LowRiskSinglePass(t *testing.T) {
	port := makeTestPlannerPortfolio()
	req := workflowplanner.PlanRequest{
		Task: workflowplanner.TaskSpec{
			TaskID:        "task-low-01",
			WorkPackageID: "wp-low-01",
			RiskTags:      []string{"docs", "cleanup"},
		},
		Portfolio: port,
	}

	res, err := workflowplanner.PlanWorkflow(req)
	if err != nil {
		t.Fatalf("PlanWorkflow failed: %v", err)
	}
	if res.Plan == nil {
		t.Fatalf("expected plan, got nil with diags: %+v", res.Diagnostics)
	}
	if res.Plan.Topology != protocol.TopologySinglePass {
		t.Fatalf("expected TopologySinglePass, got %s", res.Plan.Topology)
	}
	if len(res.Plan.Stages) != 1 {
		t.Fatalf("expected 1 stage, got %d", len(res.Plan.Stages))
	}
	stage := res.Plan.Stages[0]
	if stage.Role != "implementer" || stage.Kind != protocol.StageKindCognition || stage.IsReview {
		t.Fatalf("unexpected stage 1 attributes: %+v", stage)
	}
	if stage.EndpointID == nil || *stage.EndpointID != "ep-local-01" {
		t.Fatalf("expected endpoint ep-local-01, got %v", stage.EndpointID)
	}
}

func TestWorkflowPlanner_ACC02_HighRiskDualReview(t *testing.T) {
	highRiskTags := [][]string{
		{"security"},
		{"spending"},
		{"schema"},
		{"auth"},
		{"authority"},
		{"other", "SECURITY"},
	}

	for _, tags := range highRiskTags {
		port := makeTestPlannerPortfolio()
		req := workflowplanner.PlanRequest{
			Task: workflowplanner.TaskSpec{
				TaskID:        "task-risk-01",
				WorkPackageID: "wp-risk-01",
				RiskTags:      tags,
			},
			Portfolio: port,
		}

		res, err := workflowplanner.PlanWorkflow(req)
		if err != nil {
			t.Fatalf("PlanWorkflow failed for tags %v: %v", tags, err)
		}
		if res.Plan == nil {
			t.Fatalf("expected plan for tags %v, got nil; diags: %+v", tags, res.Diagnostics)
		}
		if res.Plan.Topology != protocol.TopologyDualIndependentReview {
			t.Fatalf("expected TopologyDualIndependentReview, got %s", res.Plan.Topology)
		}
		if len(res.Plan.Stages) != 3 {
			t.Fatalf("expected 3 stages, got %d", len(res.Plan.Stages))
		}

		s1, s2, s3 := res.Plan.Stages[0], res.Plan.Stages[1], res.Plan.Stages[2]
		if s1.Role != "implementer" || s1.IsReview {
			t.Fatalf("stage 1 invalid: %+v", s1)
		}
		if !s2.IsReview || len(s2.DependsOn) != 1 || s2.DependsOn[0] != s1.StageID {
			t.Fatalf("stage 2 invalid: %+v", s2)
		}
		if !s3.IsReview || len(s3.DependsOn) != 1 || s3.DependsOn[0] != s1.StageID {
			t.Fatalf("stage 3 invalid: %+v", s3)
		}

		// Distinct endpoints or roles for independent review
		if *s2.EndpointID == *s3.EndpointID && s2.Role == s3.Role {
			t.Fatalf("review stages 2 and 3 must not share both endpoint and role: s2=%+v, s3=%+v", s2, s3)
		}
	}
}

func TestWorkflowPlanner_ACC02_DualReviewDisambiguationWithFallback(t *testing.T) {
	// Portfolio without "verifier" role, but "reviewer" has fallback on distinct endpoint
	port := makeTestPlannerPortfolio()
	var withoutVerifier []protocol.RoleBinding
	for _, rb := range port.RoleBindings {
		if rb.Role != "verifier" {
			withoutVerifier = append(withoutVerifier, rb)
		}
	}
	port.RoleBindings = withoutVerifier

	req := workflowplanner.PlanRequest{
		Task: workflowplanner.TaskSpec{
			TaskID:             "task-fallback-01",
			WorkPackageID:      "wp-fallback-01",
			RequiresDualReview: true,
		},
		Portfolio: port,
	}

	res, err := workflowplanner.PlanWorkflow(req)
	if err != nil {
		t.Fatalf("PlanWorkflow failed: %v", err)
	}
	if res.Plan == nil {
		t.Fatalf("expected valid plan with fallback disambiguation, got nil; diags: %+v", res.Diagnostics)
	}
	s2, s3 := res.Plan.Stages[1], res.Plan.Stages[2]
	if s2.Role != "reviewer" || *s2.EndpointID != "ep-cli-01" {
		t.Fatalf("stage 2 expected ep-cli-01, got %v", s2.EndpointID)
	}
	if s3.Role != "reviewer" || *s3.EndpointID != "ep-cloud-01" {
		t.Fatalf("stage 3 expected fallback ep-cloud-01, got %v", s3.EndpointID)
	}
}

func TestWorkflowPlanner_ACC03_DeterministicOnly(t *testing.T) {
	port := makeTestPlannerPortfolio()
	req := workflowplanner.PlanRequest{
		Task: workflowplanner.TaskSpec{
			TaskID:            "task-det-01",
			WorkPackageID:     "wp-det-01",
			DeterministicOnly: true,
		},
		Portfolio: port,
	}

	res, err := workflowplanner.PlanWorkflow(req)
	if err != nil {
		t.Fatalf("PlanWorkflow failed: %v", err)
	}
	if res.Plan == nil {
		t.Fatalf("expected plan, got nil with diags: %+v", res.Diagnostics)
	}
	if res.Plan.Topology != protocol.TopologyDeterministicOnly {
		t.Fatalf("expected TopologyDeterministicOnly, got %s", res.Plan.Topology)
	}
	if len(res.Plan.Stages) != 1 {
		t.Fatalf("expected 1 stage, got %d", len(res.Plan.Stages))
	}
	stage := res.Plan.Stages[0]
	if stage.Role != "verifier" || stage.Kind != protocol.StageKindDeterministic || stage.IsReview {
		t.Fatalf("unexpected deterministic stage: %+v", stage)
	}
	if stage.DeterministicGateID == nil || *stage.DeterministicGateID != "gate-task-det-01" {
		t.Fatalf("expected gate-task-det-01, got %v", stage.DeterministicGateID)
	}
	if stage.EndpointID != nil || stage.ChannelID != nil || stage.ContextProfileID != nil {
		t.Fatalf("deterministic stage must have nil cognition pointers: %+v", stage)
	}
	if stage.BudgetPoolID != "pool-local" {
		t.Fatalf("expected pool-local compute pool, got %s", stage.BudgetPoolID)
	}
}

func TestWorkflowPlanner_ACC04_UnboundRoleRejectedByValidator(t *testing.T) {
	port := makeTestPlannerPortfolio()
	port.RoleBindings = nil // no bindings

	req := workflowplanner.PlanRequest{
		Task: workflowplanner.TaskSpec{
			TaskID:        "task-unbound-01",
			WorkPackageID: "wp-unbound-01",
		},
		Portfolio: port,
	}

	res, err := workflowplanner.PlanWorkflow(req)
	if err != nil {
		t.Fatalf("PlanWorkflow failed: %v", err)
	}
	if res.Plan != nil {
		t.Fatalf("expected nil plan when roles unbound, got: %+v", res.Plan)
	}
	if len(res.Diagnostics) == 0 {
		t.Fatalf("expected validator diagnostics, got 0")
	}
	hasRoleUnbound := false
	for _, d := range res.Diagnostics {
		if d.Code == cognition.CodeWorkflowRoleUnbound {
			hasRoleUnbound = true
			break
		}
	}
	if !hasRoleUnbound {
		t.Fatalf("expected CodeWorkflowRoleUnbound in diags: %+v", res.Diagnostics)
	}
}

func TestWorkflowPlanner_ACC05_Idempotent(t *testing.T) {
	port := makeTestPlannerPortfolio()
	req := workflowplanner.PlanRequest{
		Task: workflowplanner.TaskSpec{
			TaskID:             "task-idem-01",
			WorkPackageID:      "wp-idem-01",
			RequiresDualReview: true,
		},
		Portfolio: port,
	}

	var firstBytes []byte
	for i := 0; i < 10; i++ {
		res, err := workflowplanner.PlanWorkflow(req)
		if err != nil {
			t.Fatalf("iteration %d failed: %v", i, err)
		}
		data, err := protocol.CanonicalJSON(res.Plan)
		if err != nil {
			t.Fatalf("canonical JSON failed: %v", err)
		}
		if i == 0 {
			firstBytes = data
		} else if !bytes.Equal(firstBytes, data) {
			t.Fatalf("iteration %d output deviated from iteration 0", i)
		}
	}
}

func TestWorkflowPlanner_StopRules(t *testing.T) {
	port := makeTestPlannerPortfolio()

	// Nil portfolio
	_, err := workflowplanner.PlanWorkflow(workflowplanner.PlanRequest{
		Task: workflowplanner.TaskSpec{TaskID: "t1", WorkPackageID: "wp1"},
	})
	if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("expected InvalidArgument on nil portfolio, got %v", err)
	}

	// Empty TaskID
	_, err = workflowplanner.PlanWorkflow(workflowplanner.PlanRequest{
		Task:      workflowplanner.TaskSpec{TaskID: "   ", WorkPackageID: "wp1"},
		Portfolio: port,
	})
	if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("expected InvalidArgument on empty TaskID, got %v", err)
	}

	// Empty WorkPackageID
	_, err = workflowplanner.PlanWorkflow(workflowplanner.PlanRequest{
		Task:      workflowplanner.TaskSpec{TaskID: "t1", WorkPackageID: ""},
		Portfolio: port,
	})
	if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("expected InvalidArgument on empty WorkPackageID, got %v", err)
	}
}

func TestWorkflowPlanner_DefaultsAndPolicyCapping(t *testing.T) {
	port := makeTestPlannerPortfolio()
	port.WorkflowDefaults = &protocol.WorkflowDefaults{
		DefaultTimeoutSeconds: 5000,
		MaxRetries:            3,
	}

	// Policy caps at 3600
	policy := &cognition.WorkflowPolicy{
		MaxStages:              8,
		MaxTotalRetries:        6,
		MaxStageTimeoutSeconds: 3600,
	}

	res, err := workflowplanner.PlanWorkflow(workflowplanner.PlanRequest{
		Task: workflowplanner.TaskSpec{
			TaskID:        "task-cap-01",
			WorkPackageID: "wp-cap-01",
		},
		Portfolio: port,
		Policy:    policy,
	})
	if err != nil || res.Plan == nil {
		t.Fatalf("PlanWorkflow failed: %v, diags: %+v", err, res.Diagnostics)
	}
	stage := res.Plan.Stages[0]
	if stage.TimeoutSeconds != 3600 {
		t.Fatalf("expected timeout capped at 3600, got %d", stage.TimeoutSeconds)
	}
	if stage.RetryLimit != 3 {
		t.Fatalf("expected retry limit 3, got %d", stage.RetryLimit)
	}
}
