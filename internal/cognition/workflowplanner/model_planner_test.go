package workflowplanner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/cognition/planner"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

type mockInvoker struct {
	invokeFn func(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error)
	invoked  bool
}

func (m *mockInvoker) Invoke(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error) {
	m.invoked = true
	if m.invokeFn != nil {
		return m.invokeFn(ctx, inv)
	}
	return planner.InvocationResult{Content: "{}"}, nil
}

func validTestPortfolio() *protocol.CognitionPortfolio {
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
			{
				SchemaVersion:  protocol.SchemaVersion1,
				PoolID:         "pool-metered",
				Name:           "Metered API Pool",
				Regime:         protocol.RegimeMeteredAPI,
				HardLimit:      500,
				SoftAlertLimit: 400,
				Unit:           protocol.UnitUSDCents,
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

func TestModelPlanner_ACC01_ValidModelOutput(t *testing.T) {
	p := validTestPortfolio()
	inv := &mockInvoker{
		invokeFn: func(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error) {
			jsonOutput := `{
				"stages": [
					{
						"stage_id": "model-s1",
						"role": "implementer",
						"order": 1,
						"budget_pool_id": "pool-local",
						"endpoint_id": "ep-local-01",
						"channel_id": "chan-local-01",
						"context_profile_id": "prof-local-01"
					}
				]
			}`
			return planner.InvocationResult{Content: jsonOutput}, nil
		},
	}

	req := ModelPlanRequest{
		Task: TaskSpec{
			TaskID:        "task-acc01",
			WorkPackageID: "wp-acc01",
		},
		Portfolio:                p,
		AllowUnknownLocalCompute: true,
		Invoker:                  inv,
	}

	res, err := PlanWorkflowWithModel(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.UsedModel {
		t.Errorf("expected UsedModel: true, got false (reason: %s)", res.FallbackReason)
	}
	if res.FallbackReason != "" {
		t.Errorf("expected empty FallbackReason, got %q", res.FallbackReason)
	}
	if res.Plan == nil {
		t.Fatalf("expected non-nil plan")
	}
	if len(res.Plan.Stages) != 1 {
		t.Errorf("expected 1 stage, got %d", len(res.Plan.Stages))
	}
	if res.Plan.PlanID != "plan-task-acc01" {
		t.Errorf("expected plan-task-acc01, got %s", res.Plan.PlanID)
	}
}

func TestModelPlanner_ACC02_NoInvokerFallback(t *testing.T) {
	p := validTestPortfolio()
	req := ModelPlanRequest{
		Task: TaskSpec{
			TaskID:        "task-acc02",
			WorkPackageID: "wp-acc02",
		},
		Portfolio:                p,
		AllowUnknownLocalCompute: true,
		Invoker:                  nil,
	}

	res, err := PlanWorkflowWithModel(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.UsedModel {
		t.Errorf("expected UsedModel: false")
	}
	if res.FallbackReason != "no_invoker_or_deterministic_only" {
		t.Errorf("expected fallback reason no_invoker_or_deterministic_only, got %s", res.FallbackReason)
	}
	if res.Plan == nil {
		t.Fatalf("expected non-nil baseline plan")
	}
}

func TestModelPlanner_ACC03_InvokerErrorFallback(t *testing.T) {
	p := validTestPortfolio()
	inv := &mockInvoker{
		invokeFn: func(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error) {
			return planner.InvocationResult{}, errors.New("connection reset by peer")
		},
	}

	req := ModelPlanRequest{
		Task: TaskSpec{
			TaskID:        "task-acc03",
			WorkPackageID: "wp-acc03",
		},
		Portfolio:                p,
		AllowUnknownLocalCompute: true,
		Invoker:                  inv,
	}

	res, err := PlanWorkflowWithModel(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.UsedModel {
		t.Errorf("expected UsedModel: false")
	}
	if res.Plan == nil {
		t.Fatalf("expected non-nil baseline plan")
	}
	if !strings.Contains(res.FallbackReason, "invoker_error") {
		t.Errorf("expected invoker_error in fallback reason, got %s", res.FallbackReason)
	}
}

func TestModelPlanner_ACC04_MalformedOutputFallback(t *testing.T) {
	p := validTestPortfolio()
	inv := &mockInvoker{
		invokeFn: func(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error) {
			return planner.InvocationResult{Content: "Here is your plan: ```json not-json at all"}, nil
		},
	}

	req := ModelPlanRequest{
		Task: TaskSpec{
			TaskID:        "task-acc04",
			WorkPackageID: "wp-acc04",
		},
		Portfolio:                p,
		AllowUnknownLocalCompute: true,
		Invoker:                  inv,
	}

	res, err := PlanWorkflowWithModel(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.UsedModel {
		t.Errorf("expected UsedModel: false")
	}
	if res.Plan == nil {
		t.Fatalf("expected non-nil baseline plan")
	}
	if !strings.Contains(res.FallbackReason, "malformed_model_output") {
		t.Errorf("expected malformed_model_output in fallback reason, got %s", res.FallbackReason)
	}
}

func TestModelPlanner_ACC05_ValidatorRejectedModelPlan(t *testing.T) {
	p := validTestPortfolio()
	inv := &mockInvoker{
		invokeFn: func(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error) {
			// Proposes an unbound role "unbound_astronaut"
			jsonOutput := `{
				"stages": [
					{
						"stage_id": "model-s1",
						"role": "unbound_astronaut",
						"order": 1,
						"budget_pool_id": "pool-local"
					}
				]
			}`
			return planner.InvocationResult{Content: jsonOutput}, nil
		},
	}

	req := ModelPlanRequest{
		Task: TaskSpec{
			TaskID:        "task-acc05",
			WorkPackageID: "wp-acc05",
		},
		Portfolio:                p,
		AllowUnknownLocalCompute: true,
		Invoker:                  inv,
	}

	res, err := PlanWorkflowWithModel(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.UsedModel {
		t.Errorf("expected UsedModel: false due to validator rejection")
	}
	if res.FallbackReason != "validator_rejected_model_plan" {
		t.Errorf("expected validator_rejected_model_plan, got %s", res.FallbackReason)
	}
	if res.Plan == nil {
		t.Fatalf("expected baseline plan fallback")
	}
}

func TestModelPlanner_ACC06_BudgetUnauthorizedModelPlan(t *testing.T) {
	p := validTestPortfolio()
	inv := &mockInvoker{
		invokeFn: func(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error) {
			// Proposes using metered pool with valid binding (reviewer has fallback on ep-cloud-01, but reviewer priority 1 is ep-cli-01 on pool-sub)
			// Let's check: role reviewer has fallback ep-cloud-01 on pool-sub.
			// Let's add metered pool to a role binding or create a portfolio with a role bound to pool-metered.
			jsonOutput := `{
				"stages": [
					{
						"stage_id": "model-s1",
						"role": "metered_worker",
						"order": 1,
						"budget_pool_id": "pool-metered",
						"endpoint_id": "ep-cloud-01",
						"channel_id": "chan-cloud-01",
						"context_profile_id": "prof-cloud-01"
					}
				]
			}`
			return planner.InvocationResult{Content: jsonOutput}, nil
		},
	}

	// Add metered_worker binding to portfolio so WorkflowValidator passes R4
	p.RoleBindings = append(p.RoleBindings, protocol.RoleBinding{
		Role:             "metered_worker",
		EndpointID:       "ep-cloud-01",
		ChannelID:        "chan-cloud-01",
		BudgetPoolID:     "pool-metered",
		ContextProfileID: "prof-cloud-01",
		Priority:         1,
	})

	// Metered pool is exhausted!
	budgetStates := map[string]*protocol.BudgetState{
		"pool-metered": {
			PoolID: "pool-metered",
			Status: protocol.BudgetStatusExhausted,
		},
	}

	req := ModelPlanRequest{
		Task: TaskSpec{
			TaskID:        "task-acc06",
			WorkPackageID: "wp-acc06",
		},
		Portfolio:                p,
		BudgetStates:             budgetStates,
		AllowUnknownLocalCompute: true,
		Invoker:                  inv,
	}

	res, err := PlanWorkflowWithModel(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.UsedModel {
		t.Errorf("expected UsedModel: false due to exhausted budget")
	}
	if res.FallbackReason != "budget_unauthorized_model_plan" {
		t.Errorf("expected budget_unauthorized_model_plan, got %s", res.FallbackReason)
	}
	if res.Plan == nil {
		t.Fatalf("expected fallback baseline plan")
	}
}

func TestModelPlanner_ACC07_DeterministicOnlySkipsModel(t *testing.T) {
	p := validTestPortfolio()
	inv := &mockInvoker{
		invokeFn: func(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error) {
			t.Fatalf("invoker MUST NOT be called when DeterministicOnly: true")
			return planner.InvocationResult{}, nil
		},
	}

	req := ModelPlanRequest{
		Task: TaskSpec{
			TaskID:            "task-acc07",
			WorkPackageID:     "wp-acc07",
			DeterministicOnly: true,
		},
		Portfolio:                p,
		AllowUnknownLocalCompute: true,
		Invoker:                  inv,
	}

	res, err := PlanWorkflowWithModel(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv.invoked {
		t.Errorf("invoker was called unexpectedly")
	}
	if res.UsedModel {
		t.Errorf("expected UsedModel: false")
	}
	if res.Plan == nil || res.Plan.Topology != protocol.TopologyDeterministicOnly {
		t.Errorf("expected deterministic-only plan")
	}
}

func TestModelPlanner_ACC08_ContextCanceled(t *testing.T) {
	p := validTestPortfolio()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // canceled before call

	inv := &mockInvoker{}
	req := ModelPlanRequest{
		Task: TaskSpec{
			TaskID:        "task-acc08",
			WorkPackageID: "wp-acc08",
		},
		Portfolio: p,
		Invoker:   inv,
	}

	_, err := PlanWorkflowWithModel(ctx, req)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled error, got %v", err)
	}
}

func TestModelPlanner_ACC08_ContextCanceledDuringInvocation(t *testing.T) {
	p := validTestPortfolio()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	inv := &mockInvoker{
		invokeFn: func(innerCtx context.Context, inv planner.Invocation) (planner.InvocationResult, error) {
			cancel() // cancel parent context during execution
			return planner.InvocationResult{}, innerCtx.Err()
		},
	}

	req := ModelPlanRequest{
		Task: TaskSpec{
			TaskID:        "task-acc08-during",
			WorkPackageID: "wp-acc08-during",
		},
		Portfolio: p,
		Invoker:   inv,
	}

	_, err := PlanWorkflowWithModel(ctx, req)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled error when canceled during invocation, got %v", err)
	}
}

func TestModelPlanner_Invariant_IdentitySpoofingOverwritten(t *testing.T) {
	// Mutant 6 kill: model returns its own malicious plan_id, task_id, schema_version
	p := validTestPortfolio()
	inv := &mockInvoker{
		invokeFn: func(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error) {
			jsonOutput := `{
				"stages": [
					{
						"stage_id": "malicious-stage",
						"role": "implementer",
						"order": 1,
						"budget_pool_id": "pool-local",
						"endpoint_id": "ep-local-01",
						"channel_id": "chan-local-01",
						"context_profile_id": "prof-local-01"
					}
				]
			}`
			return planner.InvocationResult{Content: jsonOutput}, nil
		},
	}

	req := ModelPlanRequest{
		Task: TaskSpec{
			TaskID:        "canonical-task-123",
			WorkPackageID: "canonical-wp-456",
		},
		Portfolio:                p,
		AllowUnknownLocalCompute: true,
		Invoker:                  inv,
	}

	res, err := PlanWorkflowWithModel(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.UsedModel {
		t.Fatalf("expected UsedModel: true, got false (reason: %s)", res.FallbackReason)
	}
	if res.Plan.TaskID != "canonical-task-123" {
		t.Errorf("expected TaskID to be canonical-task-123, got %s", res.Plan.TaskID)
	}
	if res.Plan.WorkPackageID != "canonical-wp-456" {
		t.Errorf("expected WorkPackageID to be canonical-wp-456, got %s", res.Plan.WorkPackageID)
	}
	if res.Plan.SchemaVersion != protocol.SchemaVersion1 {
		t.Errorf("expected schema version %s, got %s", protocol.SchemaVersion1, res.Plan.SchemaVersion)
	}
	if res.Plan.PlanID != "plan-canonical-task-123" {
		t.Errorf("expected PlanID plan-canonical-task-123, got %s", res.Plan.PlanID)
	}
	if res.Plan.Stages[0].StageID != "plan-canonical-task-123-stage-1" {
		t.Errorf("expected stage ID sanitized, got %s", res.Plan.Stages[0].StageID)
	}
}

func TestModelPlanner_REQ08_FallbackBaselineBudgetRejection(t *testing.T) {
	// Baseline plan also gets rejected by budget authorizer -> returns res.Plan == nil with diagnostics
	p := validTestPortfolio()
	inv := &mockInvoker{
		invokeFn: func(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error) {
			return planner.InvocationResult{}, errors.New("provider failure")
		},
	}

	// All budget pools exhausted, including pool-local used by baseline plan!
	budgetStates := map[string]*protocol.BudgetState{
		"pool-local": {
			PoolID: "pool-local",
			Status: protocol.BudgetStatusExhausted,
		},
		"pool-sub": {
			PoolID: "pool-sub",
			Status: protocol.BudgetStatusExhausted,
		},
	}

	req := ModelPlanRequest{
		Task: TaskSpec{
			TaskID:        "task-req08",
			WorkPackageID: "wp-req08",
		},
		Portfolio:    p,
		BudgetStates: budgetStates,
		Invoker:      inv,
	}

	res, err := PlanWorkflowWithModel(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Plan != nil {
		t.Errorf("expected res.Plan == nil when fallback baseline is rejected by budget authorizer")
	}
	if len(res.Diagnostics) == 0 {
		t.Errorf("expected budget diagnostics on rejected fallback baseline")
	}
	if !strings.Contains(res.FallbackReason, "invoker_error") {
		t.Errorf("expected invoker_error fallback reason, got %s", res.FallbackReason)
	}
}

func TestModelPlanner_REQ05_HighRiskTopologyDowngradePrevented(t *testing.T) {
	// High-risk task (RequiresDualReview: true) requires DualReview.
	// Model returns a single stage (no review). Validator must reject it.
	p := validTestPortfolio()
	inv := &mockInvoker{
		invokeFn: func(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error) {
			// Single stage without review
			jsonOutput := `{
				"stages": [
					{
						"stage_id": "model-s1",
						"role": "implementer",
						"order": 1,
						"budget_pool_id": "pool-local",
						"endpoint_id": "ep-local-01",
						"channel_id": "chan-local-01",
						"context_profile_id": "prof-local-01"
					}
				]
			}`
			return planner.InvocationResult{Content: jsonOutput}, nil
		},
	}

	req := ModelPlanRequest{
		Task: TaskSpec{
			TaskID:             "task-highrisk",
			WorkPackageID:      "wp-highrisk",
			RequiresDualReview: true,
		},
		Portfolio: p,
		BudgetStates: map[string]*protocol.BudgetState{
			"pool-sub": {
				PoolID: "pool-sub",
				Status: protocol.BudgetStatusHealthy,
			},
		},
		AllowUnknownLocalCompute: true,
		Invoker:                  inv,
	}

	res, err := PlanWorkflowWithModel(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.UsedModel {
		t.Errorf("expected UsedModel: false when model downgrades high-risk dual review")
	}
	if res.FallbackReason != "validator_rejected_model_plan" {
		t.Errorf("expected validator_rejected_model_plan, got %s", res.FallbackReason)
	}
	if res.Plan == nil {
		t.Fatalf("expected non-nil fallback plan")
	}
	// Fallback plan must have dual review
	if res.Plan.Topology != protocol.TopologyDualIndependentReview {
		t.Errorf("expected fallback baseline to preserve TopologyDualIndependentReview, got %s", res.Plan.Topology)
	}
}

func TestModelPlanner_BuildWorkflowPrompt_PolicyLimits(t *testing.T) {
	p := validTestPortfolio()
	task := TaskSpec{
		TaskID:        "task-prompt",
		WorkPackageID: "wp-prompt",
	}
	policy := &cognition.WorkflowPolicy{
		MaxStages:              7,
		MaxTotalRetries:        4,
		MaxStageTimeoutSeconds: 450,
	}

	prompt, _ := BuildWorkflowPrompt(task, p, policy)
	if !strings.Contains(prompt, "Max Stages: 7") {
		t.Errorf("expected Max Stages: 7 in prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Max Total Retries: 4") {
		t.Errorf("expected Max Total Retries: 4 in prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Max Stage Timeout Seconds: 450") {
		t.Errorf("expected Max Stage Timeout Seconds: 450 in prompt, got:\n%s", prompt)
	}
}

func TestModelPlanner_ACC09_RemapsDAGDependencies(t *testing.T) {
	p := validTestPortfolio()
	inv := &mockInvoker{
		invokeFn: func(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error) {
			jsonOutput := `{
				"stages": [
					{
						"stage_id": "stage-a",
						"role": "implementer",
						"order": 1,
						"budget_pool_id": "pool-local",
						"endpoint_id": "ep-local-01",
						"channel_id": "chan-local-01",
						"context_profile_id": "prof-local-01"
					},
					{
						"stage_id": "stage-b",
						"role": "reviewer",
						"order": 2,
						"depends_on": ["stage-a"],
						"is_review": true,
						"budget_pool_id": "pool-sub",
						"endpoint_id": "ep-cli-01",
						"channel_id": "chan-cli-01",
						"context_profile_id": "prof-cli-01"
					}
				]
			}`
			return planner.InvocationResult{Content: jsonOutput}, nil
		},
	}

	req := ModelPlanRequest{
		Task: TaskSpec{
			TaskID:        "task-acc09",
			WorkPackageID: "wp-acc09",
		},
		Portfolio: p,
		BudgetStates: map[string]*protocol.BudgetState{
			"pool-sub": {
				PoolID: "pool-sub",
				Status: protocol.BudgetStatusHealthy,
			},
		},
		AllowUnknownLocalCompute: true,
		Invoker:                  inv,
	}

	res, err := PlanWorkflowWithModel(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.UsedModel {
		t.Fatalf("expected UsedModel: true, got false (reason: %s)", res.FallbackReason)
	}
	if len(res.Plan.Stages) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(res.Plan.Stages))
	}
	stage1 := res.Plan.Stages[0]
	stage2 := res.Plan.Stages[1]
	if len(stage2.DependsOn) != 1 || stage2.DependsOn[0] != stage1.StageID {
		t.Errorf("expected stage 2 depends_on remapped to %s, got %v", stage1.StageID, stage2.DependsOn)
	}
}

func TestModelPlanner_StopRules(t *testing.T) {
	req := ModelPlanRequest{}
	_, err := PlanWorkflowWithModel(context.Background(), req)
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for nil portfolio, got %v", err)
	}

	p := validTestPortfolio()
	req.Portfolio = p
	_, err = PlanWorkflowWithModel(context.Background(), req)
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for missing task_id, got %v", err)
	}

	req.Task.TaskID = "t1"
	_, err = PlanWorkflowWithModel(context.Background(), req)
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for missing work_package_id, got %v", err)
	}
}

func TestModelPlanner_EdgeCases_MarkdownFencesAndEnvelope(t *testing.T) {
	p := validTestPortfolio()

	// 1. Single line fence
	f1 := stripMarkdownFences("```json single line")
	if f1 != "```json single line" {
		t.Errorf("expected raw string, got %s", f1)
	}

	// 2. Fence without trailing closing fence
	f2 := stripMarkdownFences("```json\nline 1\nline 2")
	if f2 != "line 1\nline 2" {
		t.Errorf("expected line 1 and 2, got %s", f2)
	}

	// 3. Nested plan envelope with escalation target and custom topology
	inv := &mockInvoker{
		invokeFn: func(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error) {
			jsonOutput := `
			{
				"plan": {
					"topology": "iterative_escalation",
					"stages": [
						{
							"stage_id": "s1",
							"role": "implementer",
							"order": 1,
							"budget_pool_id": "pool-local",
							"endpoint_id": "ep-local-01",
							"channel_id": "chan-local-01",
							"context_profile_id": "prof-local-01",
							"escalation_target": "s2"
						},
						{
							"stage_id": "s2",
							"role": "verifier",
							"order": 2,
							"budget_pool_id": "pool-local",
							"endpoint_id": "ep-cloud-01",
							"channel_id": "chan-cloud-01",
							"context_profile_id": "prof-cloud-01"
						}
					]
				}
			}`
			return planner.InvocationResult{Content: jsonOutput}, nil
		},
	}

	req := ModelPlanRequest{
		Task: TaskSpec{
			TaskID:        "task-esc",
			WorkPackageID: "wp-esc",
		},
		Portfolio:                p,
		AllowUnknownLocalCompute: true,
		Invoker:                  inv,
	}

	res, err := PlanWorkflowWithModel(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.UsedModel {
		t.Fatalf("expected UsedModel: true, got false (reason: %s)", res.FallbackReason)
	}
	if res.Plan.Topology != protocol.TopologyIterativeEscalation {
		t.Errorf("expected TopologyIterativeEscalation, got %s", res.Plan.Topology)
	}
	stage1 := res.Plan.Stages[0]
	stage2 := res.Plan.Stages[1]
	if stage1.EscalationTarget == nil || *stage1.EscalationTarget != stage2.StageID {
		t.Errorf("expected escalation target remapped to %s, got %v", stage2.StageID, stage1.EscalationTarget)
	}

	// 4. Exceeding max stages triggers fallback
	invTooMany := &mockInvoker{
		invokeFn: func(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error) {
			return planner.InvocationResult{Content: `{"stages": [
				{"role": "implementer", "order": 1, "budget_pool_id": "pool-local", "endpoint_id": "ep-local-01", "channel_id": "chan-local-01", "context_profile_id": "prof-local-01"},
				{"role": "implementer", "order": 2, "budget_pool_id": "pool-local", "endpoint_id": "ep-local-01", "channel_id": "chan-local-01", "context_profile_id": "prof-local-01"},
				{"role": "implementer", "order": 3, "budget_pool_id": "pool-local", "endpoint_id": "ep-local-01", "channel_id": "chan-local-01", "context_profile_id": "prof-local-01"}
			]}`}, nil
		},
	}
	reqTooMany := ModelPlanRequest{
		Task: TaskSpec{
			TaskID:        "task-toomany",
			WorkPackageID: "wp-toomany",
		},
		Portfolio:                p,
		Policy:                   &cognition.WorkflowPolicy{MaxStages: 2},
		AllowUnknownLocalCompute: true,
		Invoker:                  invTooMany,
	}
	resTooMany, err := PlanWorkflowWithModel(context.Background(), reqTooMany)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resTooMany.UsedModel {
		t.Errorf("expected UsedModel: false when stages exceed policy")
	}

	// 5. Empty stages triggers fallback
	invEmpty := &mockInvoker{
		invokeFn: func(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error) {
			return planner.InvocationResult{Content: `{"stages": []}`}, nil
		},
	}
	reqEmpty := ModelPlanRequest{
		Task: TaskSpec{
			TaskID:        "task-empty",
			WorkPackageID: "wp-empty",
		},
		Portfolio:                p,
		AllowUnknownLocalCompute: true,
		Invoker:                  invEmpty,
	}
	resEmpty, err := PlanWorkflowWithModel(context.Background(), reqEmpty)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resEmpty.UsedModel {
		t.Errorf("expected UsedModel: false when stages empty")
	}
}
