package workflowplanner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/cognition/planner"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// ModelPlanRequest encapsulates the inputs for model-assisted workflow planning (WP-M3D-2C).
type ModelPlanRequest struct {
	Task                     TaskSpec
	Portfolio                *protocol.CognitionPortfolio
	Policy                   *cognition.WorkflowPolicy
	BudgetStates             map[string]*protocol.BudgetState
	AllowUnknownLocalCompute bool
	Invoker                  planner.Invoker
}

// ModelPlanResult is the verdict of model-assisted planning, including degradation information.
type ModelPlanResult struct {
	Plan           *protocol.WorkflowPlan
	Diagnostics    []cognition.PortfolioDiagnostic
	UsedModel      bool
	FallbackReason string
}

type modelStagePayload struct {
	StageID             string   `json:"stage_id"`
	Role                string   `json:"role"`
	Kind                string   `json:"kind,omitempty"`
	IsReview            bool     `json:"is_review,omitempty"`
	Order               int      `json:"order"`
	DependsOn           []string `json:"depends_on,omitempty"`
	BudgetPoolID        string   `json:"budget_pool_id,omitempty"`
	TimeoutSeconds      int      `json:"timeout_seconds,omitempty"`
	EndpointID          *string  `json:"endpoint_id,omitempty"`
	ChannelID           *string  `json:"channel_id,omitempty"`
	ContextProfileID    *string  `json:"context_profile_id,omitempty"`
	RetryLimit          int      `json:"retry_limit,omitempty"`
	EscalationTarget    *string  `json:"escalation_target,omitempty"`
	DeterministicGateID *string  `json:"deterministic_gate_id,omitempty"`
}

type modelPlanEnvelope struct {
	Topology string              `json:"topology,omitempty"`
	Stages   []modelStagePayload `json:"stages"`
	Plan     *struct {
		Topology string              `json:"topology,omitempty"`
		Stages   []modelStagePayload `json:"stages"`
	} `json:"plan,omitempty"`
}

// PlanWorkflowWithModel executes model-assisted workflow planning with graceful deterministic fallback.
func PlanWorkflowWithModel(ctx context.Context, req ModelPlanRequest) (*ModelPlanResult, error) {
	if req.Portfolio == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "portfolio is required")
	}
	if strings.TrimSpace(req.Task.TaskID) == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "task_id is required")
	}
	if strings.TrimSpace(req.Task.WorkPackageID) == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "work_package_id is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if req.Invoker == nil || req.Task.DeterministicOnly {
		return fallbackToBaseline(req, "no_invoker_or_deterministic_only")
	}

	prompt, promptDigest := BuildWorkflowPrompt(req.Task, req.Portfolio, req.Policy)

	invRes, err := req.Invoker.Invoke(ctx, planner.Invocation{
		Prompt:       prompt,
		PromptDigest: promptDigest,
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err != nil {
		return fallbackToBaseline(req, "invoker_error: "+err.Error())
	}

	stages, proposedTopology, err := decodeModelStages(invRes.Content, req.Policy)
	if err != nil {
		return fallbackToBaseline(req, "malformed_model_output: "+err.Error())
	}

	candidatePlan := assemblePlan(req.Task, stages, proposedTopology, req.Portfolio, req.Policy)

	// Validate with pure WorkflowValidator (2A1)
	valResult := cognition.NewWorkflowValidator().Validate(cognition.WorkflowValidationInput{
		Plan:      candidatePlan,
		Portfolio: req.Portfolio,
		Policy:    req.Policy,
	})
	if !valResult.Valid {
		// Log diagnostics in debug mode or fallback
		return fallbackToBaselineWithDiags(req, "validator_rejected_model_plan", valResult.Diagnostics)
	}

	// Authorize with live budget authorizer (2A2)
	budgetResult := cognition.AuthorizeWorkflowBudget(cognition.WorkflowBudgetInput{
		Plan:                     candidatePlan,
		Portfolio:                req.Portfolio,
		BudgetStates:             req.BudgetStates,
		AllowUnknownLocalCompute: req.AllowUnknownLocalCompute,
	})
	if !budgetResult.Authorized {
		return fallbackToBaseline(req, "budget_unauthorized_model_plan")
	}

	return &ModelPlanResult{
		Plan:        candidatePlan,
		Diagnostics: nil,
		UsedModel:   true,
	}, nil
}

func fallbackToBaseline(req ModelPlanRequest, reason string) (*ModelPlanResult, error) {
	return fallbackToBaselineWithDiags(req, reason, nil)
}

func fallbackToBaselineWithDiags(req ModelPlanRequest, reason string, modelDiags []cognition.PortfolioDiagnostic) (*ModelPlanResult, error) {
	baseRes, err := PlanWorkflow(PlanRequest{
		Task:      req.Task,
		Portfolio: req.Portfolio,
		Policy:    req.Policy,
	})
	if err != nil {
		return nil, err
	}
	if baseRes.Plan == nil {
		diags := baseRes.Diagnostics
		if len(diags) == 0 {
			diags = modelDiags
		}
		return &ModelPlanResult{
			Plan:           nil,
			Diagnostics:    diags,
			UsedModel:      false,
			FallbackReason: reason,
		}, nil
	}

	budgetResult := cognition.AuthorizeWorkflowBudget(cognition.WorkflowBudgetInput{
		Plan:                     baseRes.Plan,
		Portfolio:                req.Portfolio,
		BudgetStates:             req.BudgetStates,
		AllowUnknownLocalCompute: req.AllowUnknownLocalCompute,
	})
	if !budgetResult.Authorized {
		return &ModelPlanResult{
			Plan:           nil,
			Diagnostics:    budgetResult.Diagnostics,
			UsedModel:      false,
			FallbackReason: reason,
		}, nil
	}

	return &ModelPlanResult{
		Plan:           baseRes.Plan,
		Diagnostics:    nil,
		UsedModel:      false,
		FallbackReason: reason,
	}, nil
}

// BuildWorkflowPrompt constructs a deterministic prompt for model stage synthesis.
func BuildWorkflowPrompt(task TaskSpec, portfolio *protocol.CognitionPortfolio, policy *cognition.WorkflowPolicy) (string, string) {
	var sb strings.Builder
	sb.WriteString("devcadence-workflow-planner-prompt/1\n")
	sb.WriteString(fmt.Sprintf("Task ID: %s\n", task.TaskID))
	sb.WriteString(fmt.Sprintf("Work Package ID: %s\n", task.WorkPackageID))
	sb.WriteString(fmt.Sprintf("Risk Tags: %s\n", strings.Join(task.RiskTags, ", ")))
	sb.WriteString(fmt.Sprintf("Requires Dual Review: %t\n", task.RequiresDualReview))

	sb.WriteString("\nAvailable Role Bindings:\n")
	for _, rb := range portfolio.RoleBindings {
		sb.WriteString(fmt.Sprintf("- Role: %s, EndpointID: %s, ChannelID: %s, BudgetPoolID: %s, ContextProfileID: %s\n",
			rb.Role, rb.EndpointID, rb.ChannelID, rb.BudgetPoolID, rb.ContextProfileID))
		for _, fb := range rb.Fallbacks {
			sb.WriteString(fmt.Sprintf("  Fallback: EndpointID: %s, ChannelID: %s, BudgetPoolID: %s\n",
				fb.EndpointID, fb.ChannelID, fb.BudgetPoolID))
		}
	}

	sb.WriteString("\nAvailable Budget Pools:\n")
	for _, bp := range portfolio.BudgetPools {
		sb.WriteString(fmt.Sprintf("- PoolID: %s, Regime: %s, HardLimit: %d\n", bp.PoolID, bp.Regime, bp.HardLimit))
	}

	effectivePolicy := cognition.DefaultWorkflowPolicy()
	if policy != nil && policy.MaxStages > 0 {
		effectivePolicy = *policy
	}
	sb.WriteString("\nPolicy Limits:\n")
	sb.WriteString(fmt.Sprintf("- Max Stages: %d\n", effectivePolicy.MaxStages))
	sb.WriteString(fmt.Sprintf("- Max Total Retries: %d\n", effectivePolicy.MaxTotalRetries))
	sb.WriteString(fmt.Sprintf("- Max Stage Timeout Seconds: %d\n", effectivePolicy.MaxStageTimeoutSeconds))

	sb.WriteString("\nInstructions:\n")
	sb.WriteString("Propose a workflow stage topology in JSON format: {\"stages\": [...]}.\n")
	sb.WriteString("Each stage must specify role, budget_pool_id, order, timeout_seconds, and optional depends_on.\n")
	sb.WriteString("Your output is advisory. Deterministic validation decides whether the plan is usable.\n")

	prompt := sb.String()
	hash := sha256.Sum256([]byte(prompt))
	return prompt, "sha256:" + hex.EncodeToString(hash[:])
}

func stripMarkdownFences(s string) string {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) < 2 {
		return trimmed
	}
	start := 1
	end := len(lines) - 1
	if strings.HasPrefix(lines[end], "```") {
		return strings.Join(lines[start:end], "\n")
	}
	return strings.Join(lines[start:], "\n")
}

func decodeModelStages(content string, policy *cognition.WorkflowPolicy) ([]modelStagePayload, string, error) {
	cleaned := stripMarkdownFences(content)
	if cleaned == "" {
		return nil, "", fmt.Errorf("empty model output")
	}

	maxStages := 8
	if policy != nil && policy.MaxStages > 0 {
		maxStages = policy.MaxStages
	}

	var env modelPlanEnvelope
	dec := json.NewDecoder(strings.NewReader(cleaned))
	if err := dec.Decode(&env); err != nil {
		return nil, "", fmt.Errorf("failed to parse json: %w", err)
	}

	var rawStages []modelStagePayload
	proposedTopology := env.Topology
	if len(env.Stages) > 0 {
		rawStages = env.Stages
	} else if env.Plan != nil && len(env.Plan.Stages) > 0 {
		rawStages = env.Plan.Stages
		if env.Plan.Topology != "" {
			proposedTopology = env.Plan.Topology
		}
	}

	if len(rawStages) == 0 {
		return nil, "", fmt.Errorf("no stages found in model response")
	}
	if len(rawStages) > maxStages {
		return nil, "", fmt.Errorf("model proposed %d stages exceeding max %d", len(rawStages), maxStages)
	}

	return rawStages, proposedTopology, nil
}

func assemblePlan(task TaskSpec, payloads []modelStagePayload, proposedTopology string, portfolio *protocol.CognitionPortfolio, policy *cognition.WorkflowPolicy) *protocol.WorkflowPlan {
	planID := fmt.Sprintf("plan-%s", task.TaskID)

	effectivePolicy := cognition.DefaultWorkflowPolicy()
	if policy != nil && policy.MaxStageTimeoutSeconds > 0 {
		effectivePolicy = *policy
	}

	defaultTimeout := 1800
	if portfolio.WorkflowDefaults != nil && portfolio.WorkflowDefaults.DefaultTimeoutSeconds > 0 {
		defaultTimeout = portfolio.WorkflowDefaults.DefaultTimeoutSeconds
	}
	if defaultTimeout > effectivePolicy.MaxStageTimeoutSeconds {
		defaultTimeout = effectivePolicy.MaxStageTimeoutSeconds
	}

	defaultRetryLimit := 0
	if portfolio.WorkflowDefaults != nil && portfolio.WorkflowDefaults.MaxRetries > 0 {
		defaultRetryLimit = portfolio.WorkflowDefaults.MaxRetries
	}

	// 1. Build old-to-new stage ID mapping table
	oldToNewMap := make(map[string]string)
	for i, p := range payloads {
		order := p.Order
		if order <= 0 {
			order = i + 1
		}
		sanitizedID := fmt.Sprintf("%s-stage-%d", planID, order)
		if p.StageID != "" {
			oldToNewMap[p.StageID] = sanitizedID
		}
		oldToNewMap[sanitizedID] = sanitizedID
	}

	// 2. Assemble sanitized stages and remap DAG dependencies
	stages := make([]protocol.WorkflowStage, len(payloads))
	for i, p := range payloads {
		order := p.Order
		if order <= 0 {
			order = i + 1
		}
		stageID := fmt.Sprintf("%s-stage-%d", planID, order)

		kind := protocol.StageKindCognition
		if p.Kind == string(protocol.StageKindDeterministic) {
			kind = protocol.StageKindDeterministic
		}

		timeout := p.TimeoutSeconds
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		if timeout > effectivePolicy.MaxStageTimeoutSeconds {
			timeout = effectivePolicy.MaxStageTimeoutSeconds
		}

		retries := p.RetryLimit
		if retries <= 0 {
			retries = defaultRetryLimit
		}

		// Remap DependsOn
		var remappedDependsOn []string
		for _, dep := range p.DependsOn {
			if target, ok := oldToNewMap[dep]; ok {
				remappedDependsOn = append(remappedDependsOn, target)
			} else {
				remappedDependsOn = append(remappedDependsOn, dep)
			}
		}

		// Remap EscalationTarget
		var remappedEscalation *string
		if p.EscalationTarget != nil {
			if target, ok := oldToNewMap[*p.EscalationTarget]; ok {
				remappedEscalation = ptr(target)
			} else {
				remappedEscalation = p.EscalationTarget
			}
		}

		stages[i] = protocol.WorkflowStage{
			StageID:             stageID,
			Role:                p.Role,
			Kind:                kind,
			IsReview:            p.IsReview,
			Order:               order,
			DependsOn:           remappedDependsOn,
			BudgetPoolID:        p.BudgetPoolID,
			TimeoutSeconds:      timeout,
			EndpointID:          p.EndpointID,
			ChannelID:           p.ChannelID,
			ContextProfileID:    p.ContextProfileID,
			RetryLimit:          retries,
			EscalationTarget:    remappedEscalation,
			DeterministicGateID: p.DeterministicGateID,
		}
	}

	// 3. Topology defaulting and risk verification
	topology := selectTopology(task)
	candidateTopology := protocol.WorkflowTopologyKind(proposedTopology)
	if candidateTopology.Valid() {
		// Verify topology against risk constraints
		if (task.RequiresDualReview || isHighRisk(task.RiskTags)) && candidateTopology != protocol.TopologyDualIndependentReview {
			// Do not allow model to downgrade high-risk tasks
			topology = protocol.TopologyDualIndependentReview
		} else {
			topology = candidateTopology
		}
	}

	return &protocol.WorkflowPlan{
		SchemaVersion: protocol.SchemaVersion1,
		PlanID:        planID,
		TaskID:        task.TaskID,
		WorkPackageID: task.WorkPackageID,
		Topology:      topology,
		Stages:        stages,
	}
}
