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
	run  func(t *testing.T, v string) (string, error)
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
	cog := func(set func(*protocol.WorkflowStage, string), get func(*protocol.WorkflowStage) string) func(*testing.T, string) (string, error) {
		return func(_ *testing.T, v string) (string, error) {
			s := trimmedFieldsStage(protocol.StageKindCognition)
			set(&s, v)
			err := s.Validate()
			return get(&s), err
		}
	}
	return []trimmedCase{
		{"FallbackBinding.endpoint_id", "endpoint_id", func(_ *testing.T, v string) (string, error) {
			f := fb()
			f.EndpointID = v
			err := f.Validate()
			return f.EndpointID, err
		}},
		{"FallbackBinding.channel_id", "channel_id", func(_ *testing.T, v string) (string, error) {
			f := fb()
			f.ChannelID = v
			err := f.Validate()
			return f.ChannelID, err
		}},
		{"FallbackBinding.budget_pool_id", "budget_pool_id", func(_ *testing.T, v string) (string, error) {
			f := fb()
			f.BudgetPoolID = v
			err := f.Validate()
			return f.BudgetPoolID, err
		}},
		{"FallbackBinding.context_profile_id", "context_profile_id", func(_ *testing.T, v string) (string, error) {
			f := fb()
			f.ContextProfileID = v
			err := f.Validate()
			return f.ContextProfileID, err
		}},
		{"RoleBinding.role", "role", func(_ *testing.T, v string) (string, error) {
			r := rb()
			r.Role = v
			err := r.Validate()
			return r.Role, err
		}},
		{"RoleBinding.endpoint_id", "endpoint_id", func(_ *testing.T, v string) (string, error) {
			r := rb()
			r.EndpointID = v
			err := r.Validate()
			return r.EndpointID, err
		}},
		{"RoleBinding.channel_id", "channel_id", func(_ *testing.T, v string) (string, error) {
			r := rb()
			r.ChannelID = v
			err := r.Validate()
			return r.ChannelID, err
		}},
		{"RoleBinding.budget_pool_id", "budget_pool_id", func(_ *testing.T, v string) (string, error) {
			r := rb()
			r.BudgetPoolID = v
			err := r.Validate()
			return r.BudgetPoolID, err
		}},
		{"RoleBinding.context_profile_id", "context_profile_id", func(_ *testing.T, v string) (string, error) {
			r := rb()
			r.ContextProfileID = v
			err := r.Validate()
			return r.ContextProfileID, err
		}},
		{"EscalationRule.from_role", "from_role", func(_ *testing.T, v string) (string, error) {
			e := er()
			e.FromRole = v
			err := e.Validate()
			return e.FromRole, err
		}},
		{"EscalationRule.to_role", "to_role", func(_ *testing.T, v string) (string, error) {
			e := er()
			e.ToRole = v
			err := e.Validate()
			return e.ToRole, err
		}},
		{"EscalationRule.trigger_condition", "trigger_condition", func(_ *testing.T, v string) (string, error) {
			e := er()
			e.TriggerCondition = v
			err := e.Validate()
			return e.TriggerCondition, err
		}},
		{"CognitionPortfolio.portfolio_id", "portfolio_id", func(_ *testing.T, v string) (string, error) {
			p := validPortfolio()
			p.PortfolioID = v
			err := p.Validate()
			return p.PortfolioID, err
		}},
		{"CognitionPortfolio.created_at", "created_at", func(_ *testing.T, v string) (string, error) {
			p := validPortfolio()
			p.CreatedAt = v
			err := p.Validate()
			return p.CreatedAt, err
		}},
		{"PortfolioRecommendation.recommendation_id", "recommendation_id", func(t *testing.T, v string) (string, error) {
			r := rec(t)
			r.RecommendationID = v
			err := r.Validate()
			return r.RecommendationID, err
		}},
		{"PortfolioRecommendation.inventory_digest", "inventory_digest", func(t *testing.T, v string) (string, error) {
			r := rec(t)
			r.InventoryDigest = v
			err := r.Validate()
			return r.InventoryDigest, err
		}},
		{"PortfolioRecommendation.synthesized_at", "synthesized_at", func(t *testing.T, v string) (string, error) {
			r := rec(t)
			r.SynthesizedAt = v
			err := r.Validate()
			return r.SynthesizedAt, err
		}},
		{"PortfolioRecommendation.rationale", "rationale", func(t *testing.T, v string) (string, error) {
			r := rec(t)
			r.Rationale = v
			err := r.Validate()
			return r.Rationale, err
		}},
		{"WorkflowStage.stage_id", "stage_id", cog(func(s *protocol.WorkflowStage, v string) { s.StageID = v }, func(s *protocol.WorkflowStage) string { return s.StageID })},
		{"WorkflowStage.role", "role", cog(func(s *protocol.WorkflowStage, v string) { s.Role = v }, func(s *protocol.WorkflowStage) string { return s.Role })},
		{"WorkflowStage.budget_pool_id", "budget_pool_id", cog(func(s *protocol.WorkflowStage, v string) { s.BudgetPoolID = v }, func(s *protocol.WorkflowStage) string { return s.BudgetPoolID })},
		{"WorkflowStage.deterministic_gate_id", "deterministic_gate_id", func(_ *testing.T, v string) (string, error) {
			s := trimmedFieldsStage(protocol.StageKindDeterministic)
			s.DeterministicGateID = &v
			err := s.Validate()
			return *s.DeterministicGateID, err
		}},
		{"WorkflowStage.endpoint_id", "endpoint_id", cog(func(s *protocol.WorkflowStage, v string) { s.EndpointID = &v }, func(s *protocol.WorkflowStage) string { return *s.EndpointID })},
		{"WorkflowStage.channel_id", "channel_id", cog(func(s *protocol.WorkflowStage, v string) { s.ChannelID = &v }, func(s *protocol.WorkflowStage) string { return *s.ChannelID })},
		{"WorkflowStage.context_profile_id", "context_profile_id", cog(func(s *protocol.WorkflowStage, v string) { s.ContextProfileID = &v }, func(s *protocol.WorkflowStage) string { return *s.ContextProfileID })},
		{"WorkflowStage.escalation_target", "escalation_target", cog(func(s *protocol.WorkflowStage, v string) { s.EscalationTarget = &v }, func(s *protocol.WorkflowStage) string { return *s.EscalationTarget })},
		{"WorkflowPlan.plan_id", "plan_id", func(t *testing.T, v string) (string, error) {
			p := trimmedFieldsPlan(t)
			p.PlanID = v
			err := p.Validate()
			return p.PlanID, err
		}},
		{"WorkflowPlan.task_id", "task_id", func(t *testing.T, v string) (string, error) {
			p := trimmedFieldsPlan(t)
			p.TaskID = v
			err := p.Validate()
			return p.TaskID, err
		}},
		{"WorkflowPlan.work_package_id", "work_package_id", func(t *testing.T, v string) (string, error) {
			p := trimmedFieldsPlan(t)
			p.WorkPackageID = v
			err := p.Validate()
			return p.WorkPackageID, err
		}},
	}
}

// TestEffectivePolicyAndTrimmedFieldsACC01And02 pins every required (ACC-01)
// and optional-pointer (ACC-02) field: a whitespace-only value is rejected
// with InvalidArgument naming the JSON key, and a plain value is accepted.
func TestEffectivePolicyAndTrimmedFieldsACC01And02(t *testing.T) {
	for _, tc := range trimmedFieldCases() {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.run(t, "valid_value"); err != nil {
				t.Fatalf("control: non-whitespace value rejected: %v", err)
			}
			// Padded non-blank values are accepted and stored unchanged.
			if stored, err := tc.run(t, "  x  "); err != nil || stored != "  x  " {
				t.Fatalf("padded control: err=%v stored=%q, want accepted and unchanged", err, stored)
			}
			for _, ws := range []string{"   ", " \t\n "} {
				_, err := tc.run(t, ws)
				requireInvalidArgument(t, err)
				if !strings.Contains(err.Error(), tc.key) {
					t.Fatalf("error %q does not name key %q", err, tc.key)
				}
			}
		})
	}
}

// TestEffectivePolicyAndTrimmedFieldsACC02GateOnCognitionStage pins the
// forbidden branch: any non-empty deterministic_gate_id (including a
// whitespace-only one) is rejected on a cognition stage. A non-nil empty
// string is accepted there today (known nuance, behavior unchanged).
func TestEffectivePolicyAndTrimmedFieldsACC02GateOnCognitionStage(t *testing.T) {
	for _, v := range []string{"  ", " \t ", "gate_1"} {
		s := trimmedFieldsStage(protocol.StageKindCognition)
		s.DeterministicGateID = &v
		err := s.Validate()
		requireInvalidArgument(t, err)
		if !strings.Contains(err.Error(), "deterministic_gate_id") {
			t.Fatalf("error %q does not name deterministic_gate_id", err)
		}
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
