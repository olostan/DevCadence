package protocol_test

import (
	"os"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/protocol"
)

func trimmedFieldsPlan(t *testing.T) *protocol.WorkflowPlan {
	t.Helper()
	data, err := os.ReadFile(recFixtureDir + "workflow-plan.valid.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	plan := &protocol.WorkflowPlan{}
	if err := protocol.Unmarshal(data, plan); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	return plan
}

func trimmedFieldsStage(kind protocol.StageKind) protocol.WorkflowStage {
	s := protocol.WorkflowStage{
		StageID: "s1", Role: "implementer", Kind: kind, Order: 1,
		BudgetPoolID: "p1", TimeoutSeconds: 60,
	}
	if kind == protocol.StageKindDeterministic {
		gate := "gate_1"
		s.DeterministicGateID = &gate
	}
	return s
}

type trimmedCase struct {
	name string
	key  string
	run  func(t *testing.T, v string) error
}

func trimmedFieldCases() []trimmedCase {
	fb := func() protocol.FallbackBinding {
		return protocol.FallbackBinding{EndpointID: "e", ChannelID: "c", BudgetPoolID: "b", ContextProfileID: "x"}
	}
	rb := func() protocol.RoleBinding {
		return protocol.RoleBinding{Role: "r", EndpointID: "e", ChannelID: "c", BudgetPoolID: "b", ContextProfileID: "x", Priority: 1}
	}
	er := func() protocol.EscalationRule {
		return protocol.EscalationRule{FromRole: "a", ToRole: "b", TriggerCondition: "t", MaxEscalations: 1}
	}
	rec := func(t *testing.T) *protocol.PortfolioRecommendation {
		return loadRecommendation(t, "portfolio-recommendation.valid.json")
	}
	cog := func(set func(*protocol.WorkflowStage, string)) func(*testing.T, string) error {
		return func(_ *testing.T, v string) error {
			s := trimmedFieldsStage(protocol.StageKindCognition)
			set(&s, v)
			return s.Validate()
		}
	}
	return []trimmedCase{
		{"FallbackBinding.endpoint_id", "endpoint_id", func(_ *testing.T, v string) error { f := fb(); f.EndpointID = v; return f.Validate() }},
		{"FallbackBinding.channel_id", "channel_id", func(_ *testing.T, v string) error { f := fb(); f.ChannelID = v; return f.Validate() }},
		{"FallbackBinding.budget_pool_id", "budget_pool_id", func(_ *testing.T, v string) error { f := fb(); f.BudgetPoolID = v; return f.Validate() }},
		{"FallbackBinding.context_profile_id", "context_profile_id", func(_ *testing.T, v string) error { f := fb(); f.ContextProfileID = v; return f.Validate() }},
		{"RoleBinding.role", "role", func(_ *testing.T, v string) error { r := rb(); r.Role = v; return r.Validate() }},
		{"RoleBinding.endpoint_id", "endpoint_id", func(_ *testing.T, v string) error { r := rb(); r.EndpointID = v; return r.Validate() }},
		{"RoleBinding.channel_id", "channel_id", func(_ *testing.T, v string) error { r := rb(); r.ChannelID = v; return r.Validate() }},
		{"RoleBinding.budget_pool_id", "budget_pool_id", func(_ *testing.T, v string) error { r := rb(); r.BudgetPoolID = v; return r.Validate() }},
		{"RoleBinding.context_profile_id", "context_profile_id", func(_ *testing.T, v string) error { r := rb(); r.ContextProfileID = v; return r.Validate() }},
		{"EscalationRule.from_role", "from_role", func(_ *testing.T, v string) error { e := er(); e.FromRole = v; return e.Validate() }},
		{"EscalationRule.to_role", "to_role", func(_ *testing.T, v string) error { e := er(); e.ToRole = v; return e.Validate() }},
		{"EscalationRule.trigger_condition", "trigger_condition", func(_ *testing.T, v string) error { e := er(); e.TriggerCondition = v; return e.Validate() }},
		{"CognitionPortfolio.portfolio_id", "portfolio_id", func(_ *testing.T, v string) error { p := validPortfolio(); p.PortfolioID = v; return p.Validate() }},
		{"CognitionPortfolio.created_at", "created_at", func(_ *testing.T, v string) error { p := validPortfolio(); p.CreatedAt = v; return p.Validate() }},
		{"PortfolioRecommendation.recommendation_id", "recommendation_id", func(t *testing.T, v string) error { r := rec(t); r.RecommendationID = v; return r.Validate() }},
		{"PortfolioRecommendation.inventory_digest", "inventory_digest", func(t *testing.T, v string) error { r := rec(t); r.InventoryDigest = v; return r.Validate() }},
		{"PortfolioRecommendation.synthesized_at", "synthesized_at", func(t *testing.T, v string) error { r := rec(t); r.SynthesizedAt = v; return r.Validate() }},
		{"PortfolioRecommendation.rationale", "rationale", func(t *testing.T, v string) error { r := rec(t); r.Rationale = v; return r.Validate() }},
		{"WorkflowStage.stage_id", "stage_id", cog(func(s *protocol.WorkflowStage, v string) { s.StageID = v })},
		{"WorkflowStage.role", "role", cog(func(s *protocol.WorkflowStage, v string) { s.Role = v })},
		{"WorkflowStage.budget_pool_id", "budget_pool_id", cog(func(s *protocol.WorkflowStage, v string) { s.BudgetPoolID = v })},
		{"WorkflowStage.deterministic_gate_id", "deterministic_gate_id", func(_ *testing.T, v string) error {
			s := trimmedFieldsStage(protocol.StageKindDeterministic)
			s.DeterministicGateID = &v
			return s.Validate()
		}},
		{"WorkflowStage.endpoint_id", "endpoint_id", cog(func(s *protocol.WorkflowStage, v string) { s.EndpointID = &v })},
		{"WorkflowStage.channel_id", "channel_id", cog(func(s *protocol.WorkflowStage, v string) { s.ChannelID = &v })},
		{"WorkflowStage.context_profile_id", "context_profile_id", cog(func(s *protocol.WorkflowStage, v string) { s.ContextProfileID = &v })},
		{"WorkflowStage.escalation_target", "escalation_target", cog(func(s *protocol.WorkflowStage, v string) { s.EscalationTarget = &v })},
		{"WorkflowPlan.plan_id", "plan_id", func(t *testing.T, v string) error { p := trimmedFieldsPlan(t); p.PlanID = v; return p.Validate() }},
		{"WorkflowPlan.task_id", "task_id", func(t *testing.T, v string) error { p := trimmedFieldsPlan(t); p.TaskID = v; return p.Validate() }},
		{"WorkflowPlan.work_package_id", "work_package_id", func(t *testing.T, v string) error {
			p := trimmedFieldsPlan(t)
			p.WorkPackageID = v
			return p.Validate()
		}},
	}
}

// TestEffectivePolicyAndTrimmedFieldsACC01And02 pins every required (ACC-01)
// and optional-pointer (ACC-02) field: a whitespace-only value is rejected
// with InvalidArgument naming the JSON key, and a plain value is accepted.
func TestEffectivePolicyAndTrimmedFieldsACC01And02(t *testing.T) {
	for _, tc := range trimmedFieldCases() {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(t, "valid_value"); err != nil {
				t.Fatalf("control: non-whitespace value rejected: %v", err)
			}
			for _, ws := range []string{"   ", " \t\n "} {
				err := tc.run(t, ws)
				requireInvalidArgument(t, err)
				if !strings.Contains(err.Error(), tc.key) {
					t.Fatalf("error %q does not name key %q", err, tc.key)
				}
			}
		})
	}
}

// TestEffectivePolicyAndTrimmedFieldsACC02NilOptionalPointersValid keeps nil
// optional pointers valid and never normalizes stored values.
func TestEffectivePolicyAndTrimmedFieldsACC02NilOptionalPointersValid(t *testing.T) {
	s := trimmedFieldsStage(protocol.StageKindCognition)
	if err := s.Validate(); err != nil {
		t.Fatalf("nil optional pointers must be valid: %v", err)
	}
	padded := "  gate_1  "
	d := trimmedFieldsStage(protocol.StageKindDeterministic)
	d.DeterministicGateID = &padded
	if err := d.Validate(); err != nil {
		t.Fatalf("padded non-blank value must remain valid: %v", err)
	}
	if *d.DeterministicGateID != "  gate_1  " {
		t.Fatalf("stored value was normalized: %q", *d.DeterministicGateID)
	}
}
