package setup

import (
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/environment"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/protocol"
)

func TestPlannerGeneratesValidPlan(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	facts := protocol.EnvironmentFacts{
		Software: []protocol.SoftwarePresence{
			{
				ID:        "ollama",
				Installed: true,
				Path:      "/usr/local/bin/ollama",
				Version:   "0.5.1",
			},
		},
	}
	fp, err := environment.Fingerprint(facts)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}

	planner, err := NewPlanner(PlannerOptions{
		Clock: clk,
		IDs:   seq,
		Facts: &facts,
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	profile := protocol.ProfileLocalHeavy
	report := &protocol.DoctorReport{
		SchemaVersion:      protocol.SchemaVersion1,
		ReportID:           "doc_000000000000000000000001",
		MachineFingerprint: fp,
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

	facts := protocol.EnvironmentFacts{
		Host: protocol.HostFacts{
			Family: protocol.OSDarwin,
			Arch:   "arm64",
		},
	}
	planner, err := NewPlanner(PlannerOptions{
		Clock: clk,
		IDs:   seq,
		Facts: &facts,
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

	foundMLXGuide := false
	for _, act := range plan.Actions {
		if act.RecipeID == "recipe.ollama.pull_model" {
			t.Fatalf("MLX endpoint must NOT trigger recipe.ollama.pull_model")
		}
		if act.RecipeID == "recipe.manual.pull_mlx_model" {
			foundMLXGuide = true
		}
	}
	if !foundMLXGuide {
		t.Fatalf("expected recipe.manual.pull_mlx_model for MLX endpoint on Darwin arm64")
	}
}

func TestPlannerOllamaPullOnNotConfigured(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	facts := protocol.EnvironmentFacts{
		Software: []protocol.SoftwarePresence{
			{
				ID:        "ollama",
				Installed: true,
				Path:      "/usr/local/bin/ollama",
				Version:   "0.5.1",
			},
		},
	}
	fp, err := environment.Fingerprint(facts)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}

	planner, err := NewPlanner(PlannerOptions{
		Clock: clk,
		IDs:   seq,
		Facts: &facts,
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	profile := protocol.ProfileLocalHeavy
	report := &protocol.DoctorReport{
		SchemaVersion:      protocol.SchemaVersion1,
		ReportID:           "doc_000000000000000000000003",
		MachineFingerprint: fp,
		ObservedAt:         protocol.NewTimestamp(clk.Now()),
		Readiness:          protocol.ReadinessPartiallyReady,
		EvaluationScope: protocol.ReadinessEvaluationScope{
			TargetProfile:  &profile,
			RequiredRoles:  []string{"implementation"},
			EvidenceStatus: "live",
		},
		DiscoveredEndpoints: []protocol.CognitionEndpointSummary{
			{
				ID:                     "ollama_local",
				Kind:                   protocol.EndpointLocalRuntime,
				Locality:               protocol.LocalityLocal,
				Health:                 protocol.EndpointHealthNotConfigured, // not_configured (no models)
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

	foundPull := false
	for _, act := range plan.Actions {
		if act.RecipeID == "recipe.ollama.pull_model" {
			foundPull = true
			break
		}
	}
	if !foundPull {
		t.Fatalf("expected recipe.ollama.pull_model for Ollama with EndpointHealthNotConfigured")
	}
}

func TestPlannerOllamaPathAndVersionFromFacts(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	facts := protocol.EnvironmentFacts{
		Software: []protocol.SoftwarePresence{
			{
				ID:        "ollama",
				Installed: true,
				Path:      "/opt/homebrew/bin/ollama",
				Version:   "0.5.12",
			},
		},
	}
	fp, err := environment.Fingerprint(facts)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}

	planner, err := NewPlanner(PlannerOptions{
		Clock: clk,
		IDs:   seq,
		Facts: &facts,
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	profile := protocol.ProfileLocalHeavy
	report := &protocol.DoctorReport{
		SchemaVersion:      protocol.SchemaVersion1,
		ReportID:           "doc_000000000000000000000004",
		MachineFingerprint: fp,
		ObservedAt:         protocol.NewTimestamp(clk.Now()),
		Readiness:          protocol.ReadinessReady,
		EvaluationScope: protocol.ReadinessEvaluationScope{
			TargetProfile:  &profile,
			RequiredRoles:  []string{"implementation"},
			EvidenceStatus: "live",
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

	foundPull := false
	for _, act := range plan.Actions {
		if act.RecipeID == "recipe.ollama.pull_model" {
			foundPull = true
			for _, cond := range act.Preconditions {
				if cond.Kind == protocol.CondKindExecutableVerified {
					if cond.ExecutableVerified.CanonicalPath != "/opt/homebrew/bin/ollama" {
						t.Errorf("expected canonical path /opt/homebrew/bin/ollama, got %s", cond.ExecutableVerified.CanonicalPath)
					}
					if cond.ExecutableVerified.ExpectedVersion != "0.5.12" {
						t.Errorf("expected version 0.5.12, got %s", cond.ExecutableVerified.ExpectedVersion)
					}
				}
			}
		}
	}
	if !foundPull {
		t.Fatalf("expected recipe.ollama.pull_model")
	}
}

func TestPlannerOllamaUntrustworthyIdentityEmitsManualRemediation(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	profile := protocol.ProfileLocalHeavy
	report := &protocol.DoctorReport{
		SchemaVersion:      protocol.SchemaVersion1,
		ReportID:           "doc_000000000000000000000005",
		MachineFingerprint: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		ObservedAt:         protocol.NewTimestamp(clk.Now()),
		Readiness:          protocol.ReadinessPartiallyReady,
		EvaluationScope: protocol.ReadinessEvaluationScope{
			TargetProfile:  &profile,
			RequiredRoles:  []string{"implementation"},
			EvidenceStatus: "live",
		},
		DiscoveredEndpoints: []protocol.CognitionEndpointSummary{
			{
				ID:                     "ollama_local",
				Kind:                   protocol.EndpointLocalRuntime,
				Locality:               protocol.LocalityLocal,
				Health:                 protocol.EndpointHealthNotConfigured,
				Auth:                   protocol.AuthNotApplicable,
				CostClass:              protocol.CostLocalCompute,
				RequiredSourceExposure: protocol.ExposureLocalOnly,
				AccelerationVerified:   true,
			},
		},
	}

	// 1. Nil facts: must emit recipe.manual.pull_ollama_model
	pNil, _ := NewPlanner(PlannerOptions{Clock: clk, IDs: seq, Facts: nil})
	planNil, err := pNil.Plan(report, protocol.TargetAll, protocol.ProfileLocalHeavy)
	if err != nil {
		t.Fatalf("Plan with nil facts: %v", err)
	}
	foundManualNil := false
	for _, act := range planNil.Actions {
		if act.RecipeID == "recipe.ollama.pull_model" {
			t.Fatalf("must NOT emit automated recipe.ollama.pull_model when facts are nil")
		}
		if act.RecipeID == "recipe.manual.pull_ollama_model" {
			foundManualNil = true
			if act.ManualInstructions == nil {
				t.Errorf("manual action must provide ManualInstructions")
			}
		}
	}
	if !foundManualNil {
		t.Fatalf("expected recipe.manual.pull_ollama_model when facts are nil")
	}

	// 2. Fingerprint mismatch: must emit recipe.manual.pull_ollama_model
	facts := protocol.EnvironmentFacts{
		Software: []protocol.SoftwarePresence{
			{ID: "ollama", Installed: true, Path: "/usr/bin/ollama", Version: "0.5.0"},
		},
	}
	pMismatch, _ := NewPlanner(PlannerOptions{Clock: clk, IDs: seq, Facts: &facts})
	planMismatch, err := pMismatch.Plan(report, protocol.TargetAll, protocol.ProfileLocalHeavy)
	if err != nil {
		t.Fatalf("Plan with fingerprint mismatch: %v", err)
	}
	foundManualMismatch := false
	for _, act := range planMismatch.Actions {
		if act.RecipeID == "recipe.ollama.pull_model" {
			t.Fatalf("must NOT emit automated recipe.ollama.pull_model when fingerprint mismatches")
		}
		if act.RecipeID == "recipe.manual.pull_ollama_model" {
			foundManualMismatch = true
		}
	}
	if !foundManualMismatch {
		t.Fatalf("expected recipe.manual.pull_ollama_model when fingerprint mismatches")
	}

	// 3. Missing path/version: must emit recipe.manual.pull_ollama_model
	factsNoPath := protocol.EnvironmentFacts{
		Software: []protocol.SoftwarePresence{
			{ID: "ollama", Installed: true, Path: "", Version: ""},
		},
	}
	fpNoPath, _ := environment.Fingerprint(factsNoPath)
	reportValidFP := *report
	reportValidFP.MachineFingerprint = fpNoPath
	pNoPath, _ := NewPlanner(PlannerOptions{Clock: clk, IDs: seq, Facts: &factsNoPath})
	planNoPath, err := pNoPath.Plan(&reportValidFP, protocol.TargetAll, protocol.ProfileLocalHeavy)
	if err != nil {
		t.Fatalf("Plan with no path: %v", err)
	}
	foundManualNoPath := false
	for _, act := range planNoPath.Actions {
		if act.RecipeID == "recipe.ollama.pull_model" {
			t.Fatalf("must NOT emit automated recipe.ollama.pull_model when path is empty")
		}
		if act.RecipeID == "recipe.manual.pull_ollama_model" {
			foundManualNoPath = true
		}
	}
	if !foundManualNoPath {
		t.Fatalf("expected recipe.manual.pull_ollama_model when path is empty")
	}
}

func TestPlannerMLXAbsentOnLinuxDoesNotCreateSetupMLX(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	facts := protocol.EnvironmentFacts{
		Host: protocol.HostFacts{
			Family: protocol.OSLinux,
			Arch:   "amd64",
		},
		Software: []protocol.SoftwarePresence{
			{ID: "mlx", Installed: false},
		},
	}

	planner, err := NewPlanner(PlannerOptions{
		Clock:            clk,
		IDs:              seq,
		Facts:            &facts,
		SelectedRuntimes: []string{"mlx"}, // even if selected, Linux cannot run MLX
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	profile := protocol.ProfileLocalHeavy
	report := &protocol.DoctorReport{
		SchemaVersion:      protocol.SchemaVersion1,
		ReportID:           "doc_000000000000000000000006",
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
		if act.RecipeID == "recipe.manual.pull_mlx_model" {
			t.Fatalf("absent MLX on Linux must NOT create recipe.manual.pull_mlx_model")
		}
	}
}

func TestPlannerMLXAbsentAndNonSelectedOnDarwinDoesNotCreateSetupMLX(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	facts := protocol.EnvironmentFacts{
		Host: protocol.HostFacts{
			Family: protocol.OSDarwin,
			Arch:   "arm64",
		},
		Software: []protocol.SoftwarePresence{
			{ID: "mlx", Installed: false}, // row present with Installed: false is NOT detected
		},
	}

	planner, err := NewPlanner(PlannerOptions{
		Clock: clk,
		IDs:   seq,
		Facts: &facts,
		// SelectedRuntimes is empty
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	profile := protocol.ProfileLocalHeavy
	report := &protocol.DoctorReport{
		SchemaVersion:      protocol.SchemaVersion1,
		ReportID:           "doc_000000000000000000000007",
		MachineFingerprint: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ObservedAt:         protocol.NewTimestamp(clk.Now()),
		Readiness:          protocol.ReadinessReady,
		EvaluationScope: protocol.ReadinessEvaluationScope{
			TargetProfile:  &profile,
			RequiredRoles:  []string{"implementation"},
			EvidenceStatus: "live",
		},
		DiscoveredEndpoints: []protocol.CognitionEndpointSummary{}, // no MLX endpoint
	}

	plan, err := planner.Plan(report, protocol.TargetAll, protocol.ProfileLocalHeavy)
	if err != nil {
		t.Fatalf("planner.Plan: %v", err)
	}

	for _, act := range plan.Actions {
		if act.RecipeID == "recipe.manual.pull_mlx_model" {
			t.Fatalf("absent and non-selected MLX on Darwin must NOT create recipe.manual.pull_mlx_model")
		}
	}
}

func TestPlannerMLXSelectedOnDarwinCreatesSetupMLX(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	facts := protocol.EnvironmentFacts{
		Host: protocol.HostFacts{
			Family: protocol.OSDarwin,
			Arch:   "arm64",
		},
		Software: []protocol.SoftwarePresence{
			{ID: "mlx", Installed: false}, // absent
		},
	}

	planner, err := NewPlanner(PlannerOptions{
		Clock:            clk,
		IDs:              seq,
		Facts:            &facts,
		SelectedRuntimes: []string{"mlx"}, // explicitly selected
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	profile := protocol.ProfileLocalHeavy
	report := &protocol.DoctorReport{
		SchemaVersion:      protocol.SchemaVersion1,
		ReportID:           "doc_000000000000000000000008",
		MachineFingerprint: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ObservedAt:         protocol.NewTimestamp(clk.Now()),
		Readiness:          protocol.ReadinessReady,
		EvaluationScope: protocol.ReadinessEvaluationScope{
			TargetProfile:  &profile,
			RequiredRoles:  []string{"implementation"},
			EvidenceStatus: "live",
		},
		DiscoveredEndpoints: []protocol.CognitionEndpointSummary{}, // absent endpoint
	}

	plan, err := planner.Plan(report, protocol.TargetAll, protocol.ProfileLocalHeavy)
	if err != nil {
		t.Fatalf("planner.Plan: %v", err)
	}

	foundMLXGuide := false
	for _, act := range plan.Actions {
		if act.RecipeID == "recipe.manual.pull_mlx_model" {
			foundMLXGuide = true
			break
		}
	}
	if !foundMLXGuide {
		t.Fatalf("absent-but-selected MLX on Darwin arm64 must create recipe.manual.pull_mlx_model")
	}
}
