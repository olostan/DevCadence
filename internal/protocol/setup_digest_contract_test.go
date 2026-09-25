package protocol

import (
	"reflect"
	"testing"
	"time"
)

// TestPlanDigestViewFieldParity makes the PlanDigest field-coverage contract
// structural rather than incidental. ComputePlanDigest hashes a manually
// mirrored setupPlanDigestView, not SetupPlan itself; a future SetupPlan
// field added without a matching setupPlanDigestView field would silently
// fall outside the approval digest while every other test could still pass.
// This test fails the moment the two struct's json field sets diverge by
// anything other than plan_digest itself.
func TestPlanDigestViewFieldParity(t *testing.T) {
	planFields := jsonFieldNames(t, reflect.TypeOf(SetupPlan{}))
	viewFields := jsonFieldNames(t, reflect.TypeOf(setupPlanDigestView{}))

	delete(planFields, "plan_digest")

	for name := range planFields {
		if !viewFields[name] {
			t.Errorf("SetupPlan.%s has no matching field in setupPlanDigestView; "+
				"it will be silently excluded from the approval digest", name)
		}
	}
	for name := range viewFields {
		if !planFields[name] {
			t.Errorf("setupPlanDigestView.%s has no matching field in SetupPlan "+
				"(other than plan_digest); it is hashing something SetupPlan does not carry", name)
		}
	}
}

func jsonFieldNames(t *testing.T, typ reflect.Type) map[string]bool {
	t.Helper()
	out := make(map[string]bool, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			t.Fatalf("%s.%s has no json tag; the field-parity check cannot see it", typ.Name(), typ.Field(i).Name)
		}
		name := tag
		if idx := indexComma(tag); idx >= 0 {
			name = tag[:idx]
		}
		out[name] = true
	}
	return out
}

func indexComma(s string) int {
	for i, r := range s {
		if r == ',' {
			return i
		}
	}
	return -1
}

// digestFixturePlan builds a minimal, valid two-action SetupPlan (one
// executable, one manual) covering every top-level SetupPlan field and a
// representative nested SetupAction field, so TestPlanDigestChangesForEveryField
// can mutate each one from a single known-good baseline.
func digestFixturePlan() SetupPlan {
	execAction := SetupAction{
		ActionID:      "act-001",
		RecipeID:      "recipe-ollama-pull",
		RecipeVersion: "1.0",
		Title:         "Pull diagnostic model",
		Description:   "Pull smollm:135m for empirical probe",
		Authority:     AuthorityUserConfirmation,
		Effects:       []EffectCategory{EffectNetworkAccess, EffectModelDownload, EffectFilesystemWrite},
		Preconditions: []Condition{{
			Kind:             CondKindCommandAvailable,
			CommandAvailable: &CommandAvailableOperand{CommandName: "ollama"},
		}},
		Postconditions: []Condition{{
			Kind: CondKindModelPresent,
			ModelPresent: &ModelPresentOperand{
				Runtime: "ollama", ModelRef: "smollm:135m",
				ResolvedRevision:  "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				ExpectedSizeBytes: 145000000,
			},
		}},
		ExpectedMutations: []ExpectedMutation{{Kind: MutationModelPulled, Target: "smollm:135m", Detail: "Diagnostic model pulled"}},
		IdempotencyKey:    "pull-smollm:135m",
		Operation: &TypedOperation{
			Kind: OpKindEnsureLocalModel,
			EnsureLocalModel: &EnsureLocalModelParams{
				Runtime:           "ollama",
				ModelRef:          "smollm:135m",
				ResolvedRevision:  "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				ExpectedSizeBytes: 145000000,
				AllowedSource:     "registry.ollama.ai",
				LicenseReference:  "apache-2.0",
			},
		},
	}
	manualAction := SetupAction{
		ActionID:      "act-002",
		RecipeID:      "recipe-install-cuda",
		RecipeVersion: "1.0",
		Title:         "Install NVIDIA CUDA Drivers",
		Description:   "Manual installation of system GPU drivers",
		Authority:     AuthorityHighImpactManual,
		Effects:       []EffectCategory{EffectPrivilegeElevation, EffectDevicePermissionChange},
		Preconditions: []Condition{{
			Kind:             CondKindManagedDirExists,
			ManagedDirExists: &ManagedDirOperand{Location: LocationState, FileModeOct: "0700"},
		}},
		Postconditions: []Condition{{
			Kind:             CondKindCommandAvailable,
			CommandAvailable: &CommandAvailableOperand{CommandName: "nvidia-smi"},
		}},
		ExpectedMutations: []ExpectedMutation{{Kind: MutationConfigKeySet, Target: "driver", Detail: "NVIDIA driver installed"}},
		IdempotencyKey:    "install-cuda-drivers",
		DependsOn:         []string{"act-001"},
		ManualInstructions: &ManualGuide{
			Summary: "Install NVIDIA proprietary drivers from official vendor packages",
			Steps:   []string{"Download driver from NVIDIA official repository", "Install driver package via system package manager", "Reboot system"},
			VerificationCheck: []Condition{{
				Kind:             CondKindCommandAvailable,
				CommandAvailable: &CommandAvailableOperand{CommandName: "nvidia-smi"},
			}},
		},
	}

	return SetupPlan{
		SchemaVersion:      SchemaVersion1,
		PlanID:             "plan_000000000000000000000009",
		RecipeSetVersion:   "1.0",
		MachineFingerprint: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		CreatedAt:          NewTimestamp(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)),
		Target:             TargetAll,
		Actions:            []SetupAction{execAction, manualAction},
		RequiredAuthority:  AuthorityHighImpactManual,
		TotalEffects: []EffectCategory{
			EffectDevicePermissionChange, EffectFilesystemWrite, EffectModelDownload,
			EffectNetworkAccess, EffectPrivilegeElevation,
		},
	}
}

// TestPlanDigestChangesForEveryField proves the scope card's acceptance
// criterion directly ("PlanDigest ... changes for any field change other
// than plan_digest itself"), not just for the one field
// (TestSetupPlanValidationAndDigest only tampers Target) the prior test
// happened to cover.
func TestPlanDigestChangesForEveryField(t *testing.T) {
	base := digestFixturePlan()
	baseline, err := ComputePlanDigest(&base)
	if err != nil {
		t.Fatalf("compute baseline digest: %v", err)
	}

	mutate := func(mutator func(*SetupPlan)) string {
		p := digestFixturePlan()
		mutator(&p)
		digest, err := ComputePlanDigest(&p)
		if err != nil {
			t.Fatalf("compute mutated digest: %v", err)
		}
		return digest
	}

	cases := map[string]func(*SetupPlan){
		"schema_version":                func(p *SetupPlan) { p.SchemaVersion = "9.9" },
		"plan_id":                       func(p *SetupPlan) { p.PlanID = "plan_different" },
		"recipe_set_version":            func(p *SetupPlan) { p.RecipeSetVersion = "2.0" },
		"machine_fingerprint":           func(p *SetupPlan) { p.MachineFingerprint = "sha256:" + "1" + p.MachineFingerprint[8:] },
		"created_at":                    func(p *SetupPlan) { p.CreatedAt = NewTimestamp(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)) },
		"target":                        func(p *SetupPlan) { p.Target = TargetInference },
		"required_authority":            func(p *SetupPlan) { p.RequiredAuthority = AuthorityPrivilegedConfirmation },
		"total_effects":                 func(p *SetupPlan) { p.TotalEffects = append(p.TotalEffects, EffectAuthentication) },
		"actions_length":                func(p *SetupPlan) { p.Actions = p.Actions[:1] },
		"nested_action_title":           func(p *SetupPlan) { p.Actions[0].Title = "A different title" },
		"nested_action_idempotency_key": func(p *SetupPlan) { p.Actions[1].IdempotencyKey = "a-different-key" },
	}

	for name, mutator := range cases {
		t.Run(name, func(t *testing.T) {
			got := mutate(mutator)
			if got == baseline {
				t.Fatalf("changing %s did not change the plan digest (both %s)", name, got)
			}
		})
	}
}

// TestPlanDigestUnaffectedByPlanDigestField proves the digest is stable
// under exactly the one field ADR-0014 says it must ignore: the current
// value of PlanDigest itself never feeds back into ComputePlanDigest.
func TestPlanDigestUnaffectedByPlanDigestField(t *testing.T) {
	p := digestFixturePlan()

	first, err := ComputePlanDigest(&p)
	if err != nil {
		t.Fatalf("compute first digest: %v", err)
	}

	p.PlanDigest = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	second, err := ComputePlanDigest(&p)
	if err != nil {
		t.Fatalf("compute second digest: %v", err)
	}

	if first != second {
		t.Fatalf("setting plan_digest changed ComputePlanDigest's output: %s != %s", first, second)
	}

	p.PlanDigest = ""
	third, err := ComputePlanDigest(&p)
	if err != nil {
		t.Fatalf("compute third digest: %v", err)
	}
	if first != third {
		t.Fatalf("clearing plan_digest changed ComputePlanDigest's output: %s != %s", first, third)
	}
}
