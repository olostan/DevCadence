package workflowplanner

import (
	"fmt"
	"strings"

	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// TaskSpec defines the task metadata and constraints for workflow topology synthesis (WP-M3D-2B).
type TaskSpec struct {
	TaskID             string
	WorkPackageID      string
	RiskTags           []string
	RequiresDualReview bool
	DeterministicOnly  bool
}

// PlanRequest encapsulates the task specification, active portfolio, and optional bounds policy.
type PlanRequest struct {
	Task      TaskSpec
	Portfolio *protocol.CognitionPortfolio
	Policy    *cognition.WorkflowPolicy
}

// PlanResult is the deterministic verdict containing the synthesized plan or validation diagnostics.
type PlanResult struct {
	Plan        *protocol.WorkflowPlan
	Diagnostics []cognition.PortfolioDiagnostic
}

func ptr[T any](v T) *T { return &v }

// PlanWorkflow synthesizes a bounded, valid WorkflowPlan proposal based on task risk and portfolio.
func PlanWorkflow(req PlanRequest) (*PlanResult, error) {
	if req.Portfolio == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "portfolio is required")
	}
	if strings.TrimSpace(req.Task.TaskID) == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "task_id is required")
	}
	if strings.TrimSpace(req.Task.WorkPackageID) == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "work_package_id is required")
	}

	planID := fmt.Sprintf("plan-%s", req.Task.TaskID)

	effectivePolicy := cognition.DefaultWorkflowPolicy()
	if req.Policy != nil && req.Policy.MaxStageTimeoutSeconds > 0 {
		effectivePolicy = *req.Policy
	}

	timeout := 1800
	if req.Portfolio.WorkflowDefaults != nil && req.Portfolio.WorkflowDefaults.DefaultTimeoutSeconds > 0 {
		timeout = req.Portfolio.WorkflowDefaults.DefaultTimeoutSeconds
	}
	if timeout > effectivePolicy.MaxStageTimeoutSeconds {
		timeout = effectivePolicy.MaxStageTimeoutSeconds
	}

	retryLimit := 0
	if req.Portfolio.WorkflowDefaults != nil && req.Portfolio.WorkflowDefaults.MaxRetries > 0 {
		retryLimit = req.Portfolio.WorkflowDefaults.MaxRetries
	}

	topology := selectTopology(req.Task)
	stages := synthesizeStages(planID, req.Task, topology, req.Portfolio, timeout, retryLimit)

	plan := &protocol.WorkflowPlan{
		SchemaVersion: protocol.SchemaVersion1,
		PlanID:        planID,
		TaskID:        req.Task.TaskID,
		WorkPackageID: req.Task.WorkPackageID,
		Topology:      topology,
		Stages:        stages,
	}

	valResult := cognition.NewWorkflowValidator().Validate(cognition.WorkflowValidationInput{
		Plan:      plan,
		Portfolio: req.Portfolio,
		Policy:    req.Policy,
	})

	if !valResult.Valid {
		return &PlanResult{
			Plan:        nil,
			Diagnostics: valResult.Diagnostics,
		}, nil
	}

	return &PlanResult{
		Plan:        plan,
		Diagnostics: valResult.Diagnostics,
	}, nil
}

func selectTopology(task TaskSpec) protocol.WorkflowTopologyKind {
	if task.DeterministicOnly {
		return protocol.TopologyDeterministicOnly
	}
	if task.RequiresDualReview || isHighRisk(task.RiskTags) {
		return protocol.TopologyDualIndependentReview
	}
	return protocol.TopologySinglePass
}

func isHighRisk(tags []string) bool {
	for _, t := range tags {
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "security", "spending", "schema", "auth", "authority":
			return true
		}
	}
	return false
}

func findDeterministicPool(portfolio *protocol.CognitionPortfolio) string {
	for _, p := range portfolio.BudgetPools {
		if p.Regime == protocol.RegimeLocalCompute {
			return p.PoolID
		}
	}
	if len(portfolio.BudgetPools) > 0 {
		return portfolio.BudgetPools[0].PoolID
	}
	return ""
}

func findPrimaryRoleBinding(portfolio *protocol.CognitionPortfolio, role string) *protocol.RoleBinding {
	var best *protocol.RoleBinding
	for i := range portfolio.RoleBindings {
		rb := &portfolio.RoleBindings[i]
		if rb.Role == role {
			if best == nil || rb.Priority < best.Priority {
				best = rb
			}
		}
	}
	return best
}

func defaultPool(portfolio *protocol.CognitionPortfolio) string {
	if len(portfolio.BudgetPools) > 0 {
		return portfolio.BudgetPools[0].PoolID
	}
	return "pool-default"
}

func synthesizeStages(planID string, task TaskSpec, topology protocol.WorkflowTopologyKind, portfolio *protocol.CognitionPortfolio, timeout, retryLimit int) []protocol.WorkflowStage {
	switch topology {
	case protocol.TopologyDeterministicOnly:
		return []protocol.WorkflowStage{
			{
				StageID:             fmt.Sprintf("%s-stage-1", planID),
				Role:                "verifier",
				Kind:                protocol.StageKindDeterministic,
				IsReview:            false,
				Order:               1,
				BudgetPoolID:        findDeterministicPool(portfolio),
				TimeoutSeconds:      timeout,
				RetryLimit:          retryLimit,
				DeterministicGateID: ptr(fmt.Sprintf("gate-%s", task.TaskID)),
			},
		}

	case protocol.TopologyDualIndependentReview:
		stage1ID := fmt.Sprintf("%s-stage-1", planID)
		stage2ID := fmt.Sprintf("%s-stage-2", planID)
		stage3ID := fmt.Sprintf("%s-stage-3", planID)

		implBinding := findPrimaryRoleBinding(portfolio, "implementer")
		stage1 := protocol.WorkflowStage{
			StageID:        stage1ID,
			Role:           "implementer",
			Kind:           protocol.StageKindCognition,
			IsReview:       false,
			Order:          1,
			TimeoutSeconds: timeout,
			RetryLimit:     retryLimit,
		}
		if implBinding != nil {
			stage1.EndpointID = ptr(implBinding.EndpointID)
			stage1.ChannelID = ptr(implBinding.ChannelID)
			stage1.ContextProfileID = ptr(implBinding.ContextProfileID)
			stage1.BudgetPoolID = implBinding.BudgetPoolID
		} else {
			stage1.BudgetPoolID = defaultPool(portfolio)
		}

		revBinding := findPrimaryRoleBinding(portfolio, "reviewer")
		stage2 := protocol.WorkflowStage{
			StageID:        stage2ID,
			Role:           "reviewer",
			Kind:           protocol.StageKindCognition,
			IsReview:       true,
			Order:          2,
			DependsOn:      []string{stage1ID},
			TimeoutSeconds: timeout,
			RetryLimit:     retryLimit,
		}
		if revBinding != nil {
			stage2.EndpointID = ptr(revBinding.EndpointID)
			stage2.ChannelID = ptr(revBinding.ChannelID)
			stage2.ContextProfileID = ptr(revBinding.ContextProfileID)
			stage2.BudgetPoolID = revBinding.BudgetPoolID
		} else {
			stage2.BudgetPoolID = defaultPool(portfolio)
		}

		var stage2Endpoint *string
		if stage2.EndpointID != nil {
			stage2Endpoint = stage2.EndpointID
		}

		stage3 := protocol.WorkflowStage{
			StageID:        stage3ID,
			Kind:           protocol.StageKindCognition,
			IsReview:       true,
			Order:          3,
			DependsOn:      []string{stage1ID},
			TimeoutSeconds: timeout,
			RetryLimit:     retryLimit,
		}

		// Disambiguation algorithm (REQ-06):
		// 1. Check if role "verifier" exists with endpoint distinct from stage2Endpoint
		verBinding := findPrimaryRoleBinding(portfolio, "verifier")
		if verBinding != nil && (stage2Endpoint == nil || verBinding.EndpointID != *stage2Endpoint) {
			stage3.Role = "verifier"
			stage3.EndpointID = ptr(verBinding.EndpointID)
			stage3.ChannelID = ptr(verBinding.ChannelID)
			stage3.ContextProfileID = ptr(verBinding.ContextProfileID)
			stage3.BudgetPoolID = verBinding.BudgetPoolID
		} else if revBinding != nil && len(revBinding.Fallbacks) > 0 {
			// 2. Else check reviewer.Fallbacks for first fallback tuple distinct from stage2Endpoint
			var matchedFallback *protocol.FallbackBinding
			for i := range revBinding.Fallbacks {
				fb := &revBinding.Fallbacks[i]
				if stage2Endpoint == nil || fb.EndpointID != *stage2Endpoint {
					matchedFallback = fb
					break
				}
			}
			if matchedFallback != nil {
				stage3.Role = "reviewer"
				stage3.EndpointID = ptr(matchedFallback.EndpointID)
				stage3.ChannelID = ptr(matchedFallback.ChannelID)
				stage3.ContextProfileID = ptr(matchedFallback.ContextProfileID)
				stage3.BudgetPoolID = matchedFallback.BudgetPoolID
			} else if verBinding != nil {
				// 3. Else bind verifier primary (allows validator to catch duplicate endpoint)
				stage3.Role = "verifier"
				stage3.EndpointID = ptr(verBinding.EndpointID)
				stage3.ChannelID = ptr(verBinding.ChannelID)
				stage3.ContextProfileID = ptr(verBinding.ContextProfileID)
				stage3.BudgetPoolID = verBinding.BudgetPoolID
			} else {
				stage3.Role = "verifier"
				stage3.BudgetPoolID = defaultPool(portfolio)
			}
		} else if verBinding != nil {
			stage3.Role = "verifier"
			stage3.EndpointID = ptr(verBinding.EndpointID)
			stage3.ChannelID = ptr(verBinding.ChannelID)
			stage3.ContextProfileID = ptr(verBinding.ContextProfileID)
			stage3.BudgetPoolID = verBinding.BudgetPoolID
		} else {
			stage3.Role = "verifier"
			stage3.BudgetPoolID = defaultPool(portfolio)
		}

		return []protocol.WorkflowStage{stage1, stage2, stage3}

	default: // protocol.TopologySinglePass
		implBinding := findPrimaryRoleBinding(portfolio, "implementer")
		stage1 := protocol.WorkflowStage{
			StageID:        fmt.Sprintf("%s-stage-1", planID),
			Role:           "implementer",
			Kind:           protocol.StageKindCognition,
			IsReview:       false,
			Order:          1,
			TimeoutSeconds: timeout,
			RetryLimit:     retryLimit,
		}
		if implBinding != nil {
			stage1.EndpointID = ptr(implBinding.EndpointID)
			stage1.ChannelID = ptr(implBinding.ChannelID)
			stage1.ContextProfileID = ptr(implBinding.ContextProfileID)
			stage1.BudgetPoolID = implBinding.BudgetPoolID
		} else {
			stage1.BudgetPoolID = defaultPool(portfolio)
		}
		return []protocol.WorkflowStage{stage1}
	}
}
