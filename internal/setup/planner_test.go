package setup

import (
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/protocol"
)

func TestPlannerGeneratesValidPlan(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	planner, err := NewPlanner(PlannerOptions{
		Clock: clk,
		IDs:   seq,
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	profile := protocol.ProfileLocalHeavy
	report := &protocol.DoctorReport{
		SchemaVersion:      protocol.SchemaVersion1,
		ReportID:           "doc_000000000000000000000001",
		MachineFingerprint: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ObservedAt:         protocol.NewTimestamp(clk.Now()),
		Readiness:          protocol.ReadinessActionRequired,
		EvaluationScope: protocol.ReadinessEvaluationScope{
			TargetProfile:  &profile,
			RequiredRoles:  []string{"implementation"},
			EvidenceStatus: "live",
		},
		Findings: []protocol.DiagnosticFinding{
			{
				Category: "state",
				Severity: SeverityWarning,
				Code:     FindingCodeStateDirsMissing,
				Title:    "Managed state subdirectories missing",
				Detail:   "Missing directories under home",
			},
			{
				Category: "environment",
				Severity: SeverityError,
				Code:     FindingCodeGitNotFound,
				Title:    "Git not found",
				Detail:   "Git is missing",
			},
		},
		RecommendedProfile: &protocol.ProfileRecommendation{
			SelectedProfile: &profile,
			Rationale:       []string{"Hardware meets requirements"},
			Alternatives: []protocol.ProfileAlternative{
				{
					Profile:  protocol.ProfileLocalHeavy,
					Eligible: true,
					Reasons:  []string{"Requirements met"},
				},
			},
		},
		DiscoveredEndpoints: []protocol.CognitionEndpointSummary{
			{
				ID:                     "ollama_local",
				Kind:                   protocol.EndpointLocalRuntime,
				Locality:               protocol.LocalityLocal,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthNotApplicable,
				CostClass:              protocol.CostLocalCompute,
				RequiredSourceExposure: protocol.ExposureLocalOnly,
				AccelerationVerified:   true,
			},
		},
	}

	plan, err := planner.Plan(report, protocol.TargetAll, protocol.ProfileLocalHeavy)
	if err != nil {
		t.Fatalf("planner.Plan: %v", err)
	}

	if err := plan.Validate(); err != nil {
		t.Fatalf("plan.Validate: %v", err)
	}

	// Verify mutual exclusion and intrinsic policy for all actions
	for _, act := range plan.Actions {
		if act.Authority == protocol.AuthorityHighImpactManual {
			if act.ManualInstructions == nil || act.Operation != nil {
				t.Errorf("action %s: high impact manual action must have ManualInstructions and no Operation", act.ActionID)
			}
		} else {
			if act.Operation == nil || act.ManualInstructions != nil {
				t.Errorf("action %s: automated action must have Operation and no ManualInstructions", act.ActionID)
			}
			effects, minAuth := protocol.IntrinsicPolicy(*act.Operation)
			if act.Authority.Rank() < minAuth.Rank() {
				t.Errorf("action %s: authority %v less than intrinsic %v", act.ActionID, act.Authority, minAuth)
			}
			for _, eff := range effects {
				found := false
				for _, actEff := range act.Effects {
					if actEff == eff {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("action %s: missing intrinsic effect %v", act.ActionID, eff)
				}
			}
		}
	}

	// Verify plan digest is computed and tamper-resistant
	computed, err := plan.ComputePlanDigest()
	if err != nil {
		t.Fatalf("ComputePlanDigest: %v", err)
	}
	if plan.PlanDigest != computed {
		t.Fatalf("plan digest mismatch: %s != %s", plan.PlanDigest, computed)
	}

	// Verify Ollama pull action has CondKindExecutableVerified
	foundPullAction := false
	for _, act := range plan.Actions {
		if act.RecipeID == "recipe.ollama.pull_model" {
			foundPullAction = true
			hasExecVerified := false
			for _, cond := range act.Preconditions {
				if cond.Kind == protocol.CondKindExecutableVerified {
					hasExecVerified = true
					if cond.ExecutableVerified.CanonicalPath == "" || cond.ExecutableVerified.ExpectedVersion == "" {
						t.Errorf("executable_verified condition must specify canonical path and version")
					}
				}
			}
			if !hasExecVerified {
				t.Errorf("recipe.ollama.pull_model missing CondKindExecutableVerified in preconditions")
			}
		}
	}
	if !foundPullAction {
		t.Errorf("expected recipe.ollama.pull_model in plan")
	}
}

func TestPlannerDoesNotTreatMLXAsOllama(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	planner, err := NewPlanner(PlannerOptions{
		Clock: clk,
		IDs:   seq,
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	profile := protocol.ProfileLocalHeavy
	report := &protocol.DoctorReport{
		SchemaVersion:      protocol.SchemaVersion1,
		ReportID:           "doc_000000000000000000000002",
		MachineFingerprint: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ObservedAt:         protocol.NewTimestamp(clk.Now()),
		Readiness:          protocol.ReadinessReady,
		EvaluationScope: protocol.ReadinessEvaluationScope{
			TargetProfile:  &profile,
			RequiredRoles:  []string{"implementation"},
			EvidenceStatus: "live",
		},
		DiscoveredEndpoints: []protocol.CognitionEndpointSummary{
			{
				ID:                     "mlx_local",
				Kind:                   protocol.EndpointLocalRuntime,
				Locality:               protocol.LocalityLocal,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthNotApplicable,
				CostClass:              protocol.CostLocalCompute,
				RequiredSourceExposure: protocol.ExposureLocalOnly,
				AccelerationVerified:   true,
			},
		},
	}

	plan, err := planner.Plan(report, protocol.TargetAll, protocol.ProfileLocalHeavy)
	if err != nil {
		t.Fatalf("planner.Plan: %v", err)
	}

	for _, act := range plan.Actions {
		if act.RecipeID == "recipe.ollama.pull_model" {
			t.Fatalf("MLX endpoint must NOT trigger recipe.ollama.pull_model")
		}
	}
}
