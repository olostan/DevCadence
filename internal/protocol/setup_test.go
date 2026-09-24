package protocol_test

import (
	"os"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/protocol"
)

func validTestTimestamp() protocol.Timestamp {
	return protocol.NewTimestamp(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC))
}

func validExecutableAction() protocol.SetupAction {
	return protocol.SetupAction{
		ActionID:      "act-001",
		RecipeID:      "recipe-ollama-pull",
		RecipeVersion: "1.0",
		Title:         "Pull diagnostic model",
		Description:   "Pull smollm:135m for empirical probe",
		Authority:     protocol.AuthorityUserConfirmation,
		Effects: []protocol.EffectCategory{
			protocol.EffectNetworkAccess,
			protocol.EffectModelDownload,
			protocol.EffectFilesystemWrite,
		},
		Preconditions: []protocol.Condition{
			{
				Kind: protocol.CondKindCommandAvailable,
				CommandAvailable: &protocol.CommandAvailableOperand{
					CommandName: "ollama",
				},
			},
		},
		Postconditions: []protocol.Condition{
			{
				Kind: protocol.CondKindModelPresent,
				ModelPresent: &protocol.ModelPresentOperand{
					Runtime:           "ollama",
					ModelRef:          "smollm:135m",
					ResolvedRevision:  "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					ExpectedSizeBytes: 145000000,
				},
			},
		},
		ExpectedMutations: []protocol.ExpectedMutation{
			{
				Kind:   protocol.MutationModelPulled,
				Target: "smollm:135m",
				Detail: "Diagnostic model pulled",
			},
		},
		IdempotencyKey: "pull-smollm:135m",
		Operation: &protocol.TypedOperation{
			Kind: protocol.OpKindEnsureLocalModel,
			EnsureLocalModel: &protocol.EnsureLocalModelParams{
				Runtime:           "ollama",
				ModelRef:          "smollm:135m",
				ResolvedRevision:  "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				ExpectedSizeBytes: 145000000,
				AllowedSource:     "registry.ollama.ai",
				LicenseReference:  "apache-2.0",
			},
		},
	}
}

func validManualAction() protocol.SetupAction {
	return protocol.SetupAction{
		ActionID:      "act-002",
		RecipeID:      "recipe-install-cuda",
		RecipeVersion: "1.0",
		Title:         "Install NVIDIA CUDA Drivers",
		Description:   "Manual installation of system GPU drivers",
		Authority:     protocol.AuthorityHighImpactManual,
		Effects: []protocol.EffectCategory{
			protocol.EffectPrivilegeElevation,
			protocol.EffectDevicePermissionChange,
		},
		Preconditions: []protocol.Condition{
			{
				Kind: protocol.CondKindManagedDirExists,
				ManagedDirExists: &protocol.ManagedDirOperand{
					Location:    protocol.LocationState,
					FileModeOct: "0700",
				},
			},
		},
		Postconditions: []protocol.Condition{
			{
				Kind: protocol.CondKindCommandAvailable,
				CommandAvailable: &protocol.CommandAvailableOperand{
					CommandName: "nvidia-smi",
				},
			},
		},
		ExpectedMutations: []protocol.ExpectedMutation{
			{
				Kind:   protocol.MutationConfigKeySet,
				Target: "driver",
				Detail: "NVIDIA driver installed",
			},
		},
		IdempotencyKey: "install-cuda-drivers",
		ManualInstructions: &protocol.ManualGuide{
			Summary: "Install NVIDIA proprietary drivers from official vendor packages",
			Steps: []string{
				"Download driver from NVIDIA official repository",
				"Install driver package via system package manager",
				"Reboot system",
			},
			VerificationCheck: []protocol.Condition{
				{
					Kind: protocol.CondKindCommandAvailable,
					CommandAvailable: &protocol.CommandAvailableOperand{
						CommandName: "nvidia-smi",
					},
				},
			},
		},
	}
}

func TestSetupActionMutualExclusion(t *testing.T) {
	// Manual action with executable operation must fail
	act := validManualAction()
	act.Operation = &protocol.TypedOperation{
		Kind: protocol.OpKindCreateDirectory,
		CreateDirectory: &protocol.CreateDirectoryParams{
			Location:    protocol.LocationState,
			FileModeOct: "0700",
		},
	}
	if err := act.Validate(); err == nil {
		t.Fatal("manual action with non-nil operation was accepted; expected error")
	}

	// Executable action with manual instructions must fail
	actExec := validExecutableAction()
	actExec.ManualInstructions = &protocol.ManualGuide{
		Summary:           "test",
		Steps:             []string{"step"},
		VerificationCheck: []protocol.Condition{{Kind: protocol.CondKindCommandAvailable, CommandAvailable: &protocol.CommandAvailableOperand{CommandName: "git"}}},
	}
	if err := actExec.Validate(); err == nil {
		t.Fatal("executable action with non-nil manual_instructions was accepted; expected error")
	}
}

func TestSetupActionIntrinsicPolicyEnforcement(t *testing.T) {
	// Action declaring weaker authority than intrinsic policy must fail
	act := validExecutableAction()
	act.Authority = protocol.AuthorityReadOnly
	if err := act.Validate(); err == nil {
		t.Fatal("action with authority weaker than intrinsic policy was accepted; expected error")
	}

	// Action omitting required intrinsic effect must fail
	act2 := validExecutableAction()
	act2.Effects = []protocol.EffectCategory{protocol.EffectFilesystemWrite} // Missing NetworkAccess and ModelDownload
	if err := act2.Validate(); err == nil {
		t.Fatal("action with missing intrinsic effects was accepted; expected error")
	}
}

func TestSetupActionEnsureLocalModelRequiresMatchingPostcondition(t *testing.T) {
	// A valid action's operation and postcondition already agree (baseline).
	act := validExecutableAction()
	if err := act.Validate(); err != nil {
		t.Fatalf("baseline action failed validation: %v", err)
	}

	// Mismatched model_ref between the operation and its postcondition must
	// be rejected — a plan must not be able to approve pulling model A
	// while declaring success against model B's presence.
	mismatched := validExecutableAction()
	mismatched.Postconditions = []protocol.Condition{
		{
			Kind: protocol.CondKindModelPresent,
			ModelPresent: &protocol.ModelPresentOperand{
				Runtime:          "ollama",
				ModelRef:         "a-completely-different-model:latest",
				ResolvedRevision: mismatched.Operation.EnsureLocalModel.ResolvedRevision,
			},
		},
	}
	if err := mismatched.Validate(); err == nil {
		t.Fatal("action with mismatched ensure_local_model/model_present identity was accepted; expected error")
	}

	// No model_present postcondition at all must also be rejected.
	missing := validExecutableAction()
	missing.Postconditions = nil
	if err := missing.Validate(); err == nil {
		t.Fatal("ensure_local_model action with no model_present postcondition was accepted; expected error")
	}

	// Mismatched expected_size_bytes must also be rejected — a plan must
	// not be able to approve one size while asking the postcondition (and
	// therefore crash recovery) to accept a different one.
	sizeMismatch := validExecutableAction()
	sizeMismatch.Postconditions = []protocol.Condition{
		{
			Kind: protocol.CondKindModelPresent,
			ModelPresent: &protocol.ModelPresentOperand{
				Runtime:           sizeMismatch.Operation.EnsureLocalModel.Runtime,
				ModelRef:          sizeMismatch.Operation.EnsureLocalModel.ModelRef,
				ResolvedRevision:  sizeMismatch.Operation.EnsureLocalModel.ResolvedRevision,
				ExpectedSizeBytes: sizeMismatch.Operation.EnsureLocalModel.ExpectedSizeBytes + 1,
			},
		},
	}
	if err := sizeMismatch.Validate(); err == nil {
		t.Fatal("action with mismatched ensure_local_model/model_present expected_size_bytes was accepted; expected error")
	}
}

func TestSetupPlanValidationAndDigest(t *testing.T) {
	act1 := validExecutableAction()
	act2 := validManualAction()
	act2.DependsOn = []string{act1.ActionID}

	plan := protocol.SetupPlan{
		SchemaVersion:      protocol.SchemaVersion1,
		PlanID:             "plan-001",
		RecipeSetVersion:   "1.0",
		MachineFingerprint: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		CreatedAt:          validTestTimestamp(),
		Target:             protocol.TargetAll,
		Actions:            []protocol.SetupAction{act1, act2},
		RequiredAuthority:  protocol.AuthorityHighImpactManual,
		TotalEffects: []protocol.EffectCategory{
			protocol.EffectDevicePermissionChange,
			protocol.EffectFilesystemWrite,
			protocol.EffectModelDownload,
			protocol.EffectNetworkAccess,
			protocol.EffectPrivilegeElevation,
		},
	}

	digest, err := protocol.ComputePlanDigest(&plan)
	if err != nil {
		t.Fatalf("compute plan digest: %v", err)
	}
	plan.PlanDigest = digest

	if err := plan.Validate(); err != nil {
		t.Fatalf("valid plan failed validation: %v", err)
	}

	// Tampering any field must cause digest mismatch
	tampered := plan
	tampered.Target = protocol.TargetInference
	if err := tampered.Validate(); err == nil {
		t.Fatal("tampered plan was accepted; expected digest mismatch")
	}

	// Empty PlanDigest must fail
	emptyDigest := plan
	emptyDigest.PlanDigest = ""
	if err := emptyDigest.Validate(); err == nil {
		t.Fatal("plan with empty PlanDigest was accepted; expected error")
	}

	// Duplicate action ID must fail
	dup := plan
	dup.Actions = []protocol.SetupAction{act1, act1}
	dup.PlanDigest, _ = protocol.ComputePlanDigest(&dup)
	if err := dup.Validate(); err == nil {
		t.Fatal("plan with duplicate action ID was accepted; expected error")
	}

	// Duplicate idempotency key must fail
	dupKey := plan
	act2DupKey := act2
	act2DupKey.IdempotencyKey = act1.IdempotencyKey
	dupKey.Actions = []protocol.SetupAction{act1, act2DupKey}
	dupKey.PlanDigest, _ = protocol.ComputePlanDigest(&dupKey)
	if err := dupKey.Validate(); err == nil {
		t.Fatal("plan with duplicate idempotency key was accepted; expected error")
	}

	// Dependency cycle / forward dependency must fail
	forwardDep := plan
	act1Fwd := act1
	act1Fwd.DependsOn = []string{act2.ActionID} // act1 depends on act2 which appears later
	forwardDep.Actions = []protocol.SetupAction{act1Fwd, act2}
	forwardDep.PlanDigest, _ = protocol.ComputePlanDigest(&forwardDep)
	if err := forwardDep.Validate(); err == nil {
		t.Fatal("plan with forward dependency was accepted; expected error")
	}

	// Missing dependency must fail
	missingDep := plan
	act1Miss := act1
	act1Miss.DependsOn = []string{"act-999"}
	missingDep.Actions = []protocol.SetupAction{act1Miss}
	missingDep.PlanDigest, _ = protocol.ComputePlanDigest(&missingDep)
	if err := missingDep.Validate(); err == nil {
		t.Fatal("plan with missing dependency was accepted; expected error")
	}
}

func TestLoopbackOnlyPortCondition(t *testing.T) {
	cond := protocol.Condition{
		Kind: protocol.CondKindPortListening,
		PortListening: &protocol.PortOperand{
			Host: "192.168.1.50", // Non-loopback
			Port: 11434,
		},
	}
	if err := cond.Validate(); err == nil {
		t.Fatal("non-loopback host was accepted; expected error")
	}

	condLoopback := protocol.Condition{
		Kind: protocol.CondKindPortListening,
		PortListening: &protocol.PortOperand{
			Host: "127.0.0.1",
			Port: 11434,
		},
	}
	if err := condLoopback.Validate(); err != nil {
		t.Fatalf("loopback host failed validation: %v", err)
	}
}

func TestCredentialRefValidation(t *testing.T) {
	// Valid env_var
	credEnv := protocol.CredentialRef{
		RefID:    "cred-001",
		Kind:     protocol.CredRefEnvVar,
		Provider: "anthropic",
		Locator:  "ANTHROPIC_API_KEY",
	}
	if err := credEnv.Validate(); err != nil {
		t.Fatalf("valid env_var cred failed: %v", err)
	}

	// Invalid env_var with lowercase or special chars
	credEnvInvalid := credEnv
	credEnvInvalid.Locator = "my-secret-key"
	if err := credEnvInvalid.Validate(); err == nil {
		t.Fatal("invalid env identifier was accepted; expected error")
	}

	// Valid cli_session
	credCLI := protocol.CredentialRef{
		RefID:    "cred-002",
		Kind:     protocol.CredRefCLISession,
		Provider: "anthropic",
		Locator:  "claude-code:session-1",
	}
	if err := credCLI.Validate(); err != nil {
		t.Fatalf("valid cli_session cred failed: %v", err)
	}
}

func TestSetupLedgerEventValidationAndChain(t *testing.T) {
	ev1 := protocol.SetupLedgerEvent{
		SchemaVersion:       protocol.SchemaVersion1,
		Sequence:            1,
		EventID:             "ev-001",
		ExecutionID:         "exec-001",
		PlanID:              "plan-001",
		PlanDigest:          "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		PreviousEventDigest: "", // Sequence 1 requires empty
		Timestamp:           validTestTimestamp(),
		Type:                protocol.EventExecutionCreated,
		Payload: protocol.EventPayload{
			ExecutionCreated: &protocol.ExecutionCreatedPayload{
				InitiatedBy: "operator",
				Target:      protocol.TargetAll,
			},
		},
	}

	d1, err := protocol.ComputeLedgerEventDigest(&ev1)
	if err != nil {
		t.Fatalf("compute event 1 digest: %v", err)
	}
	ev1.EventDigest = d1

	if err := ev1.Validate(); err != nil {
		t.Fatalf("valid event 1 failed: %v", err)
	}

	// Event 2 linking to Event 1
	opKind := protocol.OpKindEnsureLocalModel
	ev2 := protocol.SetupLedgerEvent{
		SchemaVersion:       protocol.SchemaVersion1,
		Sequence:            2,
		EventID:             "ev-002",
		ExecutionID:         "exec-001",
		PlanID:              "plan-001",
		PlanDigest:          "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ActionID:            "act-001",
		PreviousEventDigest: d1,
		Timestamp:           validTestTimestamp(),
		Type:                protocol.EventActionStarting,
		Payload: protocol.EventPayload{
			ActionStarting: &protocol.ActionStartingPayload{
				ActionID:       "act-001",
				RecipeID:       "recipe-ollama-pull",
				RecipeVersion:  "1.0",
				OperationKind:  &opKind,
				IdempotencyKey: "pull-smollm:135m",
			},
		},
	}

	d2, err := protocol.ComputeLedgerEventDigest(&ev2)
	if err != nil {
		t.Fatalf("compute event 2 digest: %v", err)
	}
	ev2.EventDigest = d2

	if err := ev2.Validate(); err != nil {
		t.Fatalf("valid event 2 failed: %v", err)
	}

	// Tampered previous event digest must fail
	ev2Tampered := ev2
	ev2Tampered.PreviousEventDigest = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if err := ev2Tampered.Validate(); err == nil {
		t.Fatal("tampered previous digest was accepted; expected error")
	}

	// Payload/Type mismatch must fail
	ev2Mismatch := ev2
	ev2Mismatch.Type = protocol.EventExecutionCreated
	ev2Mismatch.EventDigest = ""
	if err := ev2Mismatch.Validate(); err == nil {
		t.Fatal("payload mismatch was accepted; expected error")
	}
}

func TestDoctorReportValidation(t *testing.T) {
	targetProfile := protocol.ProfileLocalHeavy
	rep := protocol.DoctorReport{
		SchemaVersion:      protocol.SchemaVersion1,
		ReportID:           "doc-001",
		MachineFingerprint: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ObservedAt:         validTestTimestamp(),
		EvaluationScope: protocol.ReadinessEvaluationScope{
			TargetProfile:  &targetProfile,
			RequiredRoles:  []string{"implementation", "review"},
			EvidenceStatus: "live",
		},
		Readiness: protocol.ReadinessReady,
		Findings: []protocol.DiagnosticFinding{
			{
				Category: "environment",
				Severity: "info",
				Code:     "GIT_READY",
				Title:    "Git installed",
				Detail:   "Git 2.45.0 available on PATH",
			},
		},
		RecommendedProfile: &protocol.ProfileRecommendation{
			SelectedProfile: &targetProfile,
			Rationale:       []string{"Hardware acceleration verified", "Local coding endpoint ready"},
			Alternatives: []protocol.ProfileAlternative{
				{
					Profile:  protocol.ProfileLocalHeavy,
					Eligible: true,
					Reasons:  []string{"Hardware meets requirements"},
				},
			},
		},
	}

	if err := rep.Validate(); err != nil {
		t.Fatalf("valid doctor report failed: %v", err)
	}

	// Claiming READY without target profile must fail
	repNoProfile := rep
	repNoProfile.EvaluationScope.TargetProfile = nil
	if err := repNoProfile.Validate(); err == nil {
		t.Fatal("claiming READY without evaluated target_profile was accepted; expected error")
	}
}

func TestComputeDigestForFixtures(t *testing.T) {
	// Plan fixture
	planData, err := os.ReadFile("../../fixtures/protocol/setup-plan.valid.json")
	if err != nil {
		t.Fatalf("read plan fixture: %v", err)
	}
	var plan protocol.SetupPlan
	if err := protocol.Unmarshal(planData, &plan); err != nil {
		t.Fatalf("unmarshal plan: %v", err)
	}
	planDigest, err := protocol.ComputePlanDigest(&plan)
	if err != nil {
		t.Fatalf("compute plan digest: %v", err)
	}
	t.Logf("Computed plan digest: %s", planDigest)

	// Ledger event fixture
	eventData, err := os.ReadFile("../../fixtures/protocol/setup-ledger-event.valid.json")
	if err != nil {
		t.Fatalf("read event fixture: %v", err)
	}
	var ev protocol.SetupLedgerEvent
	if err := protocol.Unmarshal(eventData, &ev); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	evDigest, err := protocol.ComputeLedgerEventDigest(&ev)
	if err != nil {
		t.Fatalf("compute event digest: %v", err)
	}
	t.Logf("Computed event digest: %s", evDigest)
}
