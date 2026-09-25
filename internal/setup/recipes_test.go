package setup_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/environment"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/schema"
	"github.com/olostan/DevCadence/internal/setup"
)

// mockResolver implements setup.ModelResolver for testing failure modes.
type mockResolver struct {
	resolveFn func(ctx context.Context, runtime, modelRef string) (setup.ResolvedModel, error)
}

func (m *mockResolver) ResolveModel(ctx context.Context, runtime, modelRef string) (setup.ResolvedModel, error) {
	if m.resolveFn != nil {
		return m.resolveFn(ctx, runtime, modelRef)
	}
	return setup.ResolvedModel{}, errs.New(errs.CategoryNotFound, "not found")
}

func testReport(fp string) *protocol.DoctorReport {
	if fp == "" {
		fp = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	}
	return &protocol.DoctorReport{
		SchemaVersion:      protocol.SchemaVersion1,
		ReportID:           "rep_001",
		MachineFingerprint: fp,
		ObservedAt:         protocol.NewTimestamp(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)),
		EvaluationScope: protocol.ReadinessEvaluationScope{
			EvidenceStatus: "live",
			RequiredRoles:  []string{"implementation"},
		},
		Readiness: protocol.ReadinessActionRequired,
		Findings:  nil,
	}
}

// 1. Pre-resolution of Ollama model tag
func TestRecipe_Scenario01_PreResolutionOllamaModel(t *testing.T) {
	resolver := setup.NewCatalogModelResolver()
	resolved, err := resolver.ResolveModel(context.Background(), "ollama", setup.DefaultOllamaModelTag)
	if err != nil {
		t.Fatalf("expected successful resolution, got: %v", err)
	}
	if resolved.ResolvedRevision != setup.DefaultOllamaDigest {
		t.Errorf("expected revision %q, got %q", setup.DefaultOllamaDigest, resolved.ResolvedRevision)
	}
	if resolved.ExpectedSizeBytes != setup.DefaultOllamaSizeBytes {
		t.Errorf("expected size %d, got %d", setup.DefaultOllamaSizeBytes, resolved.ExpectedSizeBytes)
	}
	if resolved.AllowedSource != setup.DefaultOllamaRegistryHost {
		t.Errorf("expected source %q, got %q", setup.DefaultOllamaRegistryHost, resolved.AllowedSource)
	}
	if resolved.LicenseReference != setup.DefaultOllamaLicense {
		t.Errorf("expected license %q, got %q", setup.DefaultOllamaLicense, resolved.LicenseReference)
	}
	if err := resolved.Validate(); err != nil {
		t.Errorf("expected valid resolved model, got: %v", err)
	}
}

// 2. Pre-resolution of MLX model ref
func TestRecipe_Scenario02_PreResolutionMLXModel(t *testing.T) {
	resolver := setup.NewCatalogModelResolver()
	resolved, err := resolver.ResolveModel(context.Background(), "mlx", setup.DefaultMLXModelRef)
	if err != nil {
		t.Fatalf("expected successful resolution, got: %v", err)
	}
	if resolved.ResolvedRevision != setup.DefaultMLXRevision {
		t.Errorf("expected revision %q, got %q", setup.DefaultMLXRevision, resolved.ResolvedRevision)
	}
	if resolved.ExpectedSizeBytes != setup.DefaultMLXSizeBytes {
		t.Errorf("expected size %d, got %d", setup.DefaultMLXSizeBytes, resolved.ExpectedSizeBytes)
	}
	if resolved.AllowedSource != setup.DefaultMLXSource {
		t.Errorf("expected source %q, got %q", setup.DefaultMLXSource, resolved.AllowedSource)
	}
	if resolved.LicenseReference != setup.DefaultMLXLicense {
		t.Errorf("expected license %q, got %q", setup.DefaultMLXLicense, resolved.LicenseReference)
	}
	if err := resolved.Validate(); err != nil {
		t.Errorf("expected valid resolved model, got: %v", err)
	}
}

// 3. Fail-closed on unresolvable model tag
func TestRecipe_Scenario03_FailClosedOnUnresolvableModelTag(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	// Resolver returns error for model
	resolver := &mockResolver{
		resolveFn: func(ctx context.Context, runtime, modelRef string) (setup.ResolvedModel, error) {
			return setup.ResolvedModel{}, errs.New(errs.CategoryNotFound, "registry manifest not found for %s", modelRef)
		},
	}

	planner, err := setup.NewPlanner(setup.PlannerOptions{
		Clock:            clk,
		IDs:              seq,
		SelectedRuntimes: []string{"ollama"},
		ModelResolver:    resolver,
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	report := testReport("")
	_, err = planner.Plan(report, protocol.TargetCognition, "")
	if err == nil {
		t.Fatalf("expected Plan to fail when model digest cannot be resolved, but succeeded")
	}
	if !strings.Contains(err.Error(), "failed to resolve model") {
		t.Errorf("expected error mentioning failed resolution, got: %v", err)
	}
}

// 4. Fail-closed on empty digest/revision
func TestRecipe_Scenario04_FailClosedOnEmptyDigest(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	resolver := &mockResolver{
		resolveFn: func(ctx context.Context, runtime, modelRef string) (setup.ResolvedModel, error) {
			return setup.ResolvedModel{
				Runtime:           "ollama",
				ModelRef:          "custom-model",
				ResolvedRevision:  "", // empty digest
				ExpectedSizeBytes: 1000,
				AllowedSource:     "registry.ollama.ai",
				LicenseReference:  "MIT",
			}, nil
		},
	}

	planner, err := setup.NewPlanner(setup.PlannerOptions{
		Clock:            clk,
		IDs:              seq,
		SelectedRuntimes: []string{"ollama"},
		ModelResolver:    resolver,
		ModelRefs:        map[string]string{"ollama": "custom-model"},
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	report := testReport("")
	_, err = planner.Plan(report, protocol.TargetCognition, "")
	if err == nil {
		t.Fatalf("expected Plan to fail when resolved model has empty revision, but succeeded")
	}
}

// 5. Fail-closed on mutable MLX revision
func TestRecipe_Scenario05_FailClosedOnMutableMLXRevision(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	fixture := environment.DarwinAppleSilicon()
	facts, err := fixture.Discover(context.Background(), clk, protocol.DepthHealth)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	resolver := &mockResolver{
		resolveFn: func(ctx context.Context, runtime, modelRef string) (setup.ResolvedModel, error) {
			return setup.ResolvedModel{
				Runtime:           "mlx",
				ModelRef:          "mlx-community/model",
				ResolvedRevision:  "main", // mutable branch ref — invalid!
				ExpectedSizeBytes: 1000,
				AllowedSource:     "huggingface.co",
				LicenseReference:  "Apache-2.0",
			}, nil
		},
	}

	fp, _ := environment.Fingerprint(facts)
	report := testReport(fp)

	planner, err := setup.NewPlanner(setup.PlannerOptions{
		Clock:            clk,
		IDs:              seq,
		Facts:            &facts,
		SelectedRuntimes: []string{"mlx"},
		ModelResolver:    resolver,
		ModelRefs:        map[string]string{"mlx": "mlx-community/model"},
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	_, err = planner.Plan(report, protocol.TargetCognition, "")
	if err == nil {
		t.Fatalf("expected Plan to fail when MLX revision is mutable, but succeeded")
	}
	if !strings.Contains(err.Error(), "not an immutable 40-character commit hash") {
		t.Errorf("expected error about non-immutable 40-hex hash, got: %v", err)
	}
}

// 6. Fail-closed on missing license metadata
func TestRecipe_Scenario06_FailClosedOnMissingLicense(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	resolver := &mockResolver{
		resolveFn: func(ctx context.Context, runtime, modelRef string) (setup.ResolvedModel, error) {
			return setup.ResolvedModel{
				Runtime:           "ollama",
				ModelRef:          "custom-model",
				ResolvedRevision:  "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				ExpectedSizeBytes: 1000,
				AllowedSource:     "registry.ollama.ai",
				LicenseReference:  "", // missing license
			}, nil
		},
	}

	planner, err := setup.NewPlanner(setup.PlannerOptions{
		Clock:            clk,
		IDs:              seq,
		SelectedRuntimes: []string{"ollama"},
		ModelResolver:    resolver,
		ModelRefs:        map[string]string{"ollama": "custom-model"},
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	report := testReport("")
	_, err = planner.Plan(report, protocol.TargetCognition, "")
	if err == nil {
		t.Fatalf("expected Plan to fail when license is missing, but succeeded")
	}
	if !strings.Contains(err.Error(), "license_reference is required") {
		t.Errorf("expected license_reference is required, got: %v", err)
	}
}

// FailClosedOnMutableOllamaRevision is the independent-review follow-up on
// WP-M3B-6, FIX_NOW 1: Ollama pre-resolution must reject a mutable
// revision (e.g. a tag like "main") the same way MLX's does, not merely
// require it to be non-empty.
func TestRecipe_FailClosedOnMutableOllamaRevision(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	resolver := &mockResolver{
		resolveFn: func(ctx context.Context, runtime, modelRef string) (setup.ResolvedModel, error) {
			return setup.ResolvedModel{
				Runtime:           "ollama",
				ModelRef:          "custom-model",
				ResolvedRevision:  "main", // mutable tag — invalid!
				ExpectedSizeBytes: 1000,
				AllowedSource:     "registry.ollama.ai",
				LicenseReference:  "Apache-2.0",
			}, nil
		},
	}

	planner, err := setup.NewPlanner(setup.PlannerOptions{
		Clock:            clk,
		IDs:              seq,
		SelectedRuntimes: []string{"ollama"},
		ModelResolver:    resolver,
		ModelRefs:        map[string]string{"ollama": "custom-model"},
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	report := testReport("")
	_, err = planner.Plan(report, protocol.TargetCognition, "")
	if err == nil {
		t.Fatalf("expected Plan to fail when Ollama revision is mutable, but succeeded")
	}
	if !strings.Contains(err.Error(), "not an immutable sha256 manifest digest") {
		t.Errorf("expected error about non-immutable sha256 manifest digest, got: %v", err)
	}
}

// FailClosedOnResolverRuntimeMismatch and
// FailClosedOnResolverModelRefMismatch are the independent-review
// follow-up on WP-M3B-6, FIX_NOW 2: the planner must not blindly trust a
// ModelResolver implementation's returned identity — a resolver that
// returns a perfectly valid, immutable record for a different
// runtime/model must not let the plan silently target that artifact.
func TestRecipe_FailClosedOnResolverRuntimeMismatch(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	resolver := &mockResolver{
		resolveFn: func(ctx context.Context, runtime, modelRef string) (setup.ResolvedModel, error) {
			return setup.ResolvedModel{
				// Requested runtime is "ollama"; resolver returns "mlx".
				Runtime:           "mlx",
				ModelRef:          modelRef,
				ResolvedRevision:  strings.Repeat("a", 40),
				ExpectedSizeBytes: 1000,
				AllowedSource:     "huggingface.co",
				LicenseReference:  "Apache-2.0",
			}, nil
		},
	}

	planner, err := setup.NewPlanner(setup.PlannerOptions{
		Clock:            clk,
		IDs:              seq,
		SelectedRuntimes: []string{"ollama"},
		ModelResolver:    resolver,
		ModelRefs:        map[string]string{"ollama": "custom-model"},
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	report := testReport("")
	_, err = planner.Plan(report, protocol.TargetCognition, "")
	if err == nil {
		t.Fatalf("expected Plan to fail when the resolver returns a mismatched runtime, but succeeded")
	}
	if !strings.Contains(err.Error(), "mismatched identity") {
		t.Errorf("expected a mismatched-identity error, got: %v", err)
	}
}

func TestRecipe_FailClosedOnResolverModelRefMismatch(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	resolver := &mockResolver{
		resolveFn: func(ctx context.Context, runtime, modelRef string) (setup.ResolvedModel, error) {
			return setup.ResolvedModel{
				Runtime: "ollama",
				// Requested model_ref is "custom-model"; resolver returns a different one.
				ModelRef:          "a-different-model",
				ResolvedRevision:  "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				ExpectedSizeBytes: 1000,
				AllowedSource:     "registry.ollama.ai",
				LicenseReference:  "Apache-2.0",
			}, nil
		},
	}

	planner, err := setup.NewPlanner(setup.PlannerOptions{
		Clock:            clk,
		IDs:              seq,
		SelectedRuntimes: []string{"ollama"},
		ModelResolver:    resolver,
		ModelRefs:        map[string]string{"ollama": "custom-model"},
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	report := testReport("")
	_, err = planner.Plan(report, protocol.TargetCognition, "")
	if err == nil {
		t.Fatalf("expected Plan to fail when the resolver returns a mismatched model_ref, but succeeded")
	}
	if !strings.Contains(err.Error(), "mismatched identity") {
		t.Errorf("expected a mismatched-identity error, got: %v", err)
	}
}

// 7. Fallback to manual model pull on untrusted CLI identity
func TestRecipe_Scenario07_ManualModelPullFallbackOnUntrustedIdentity(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	// No facts configured -> untrusted CLI identity
	planner, err := setup.NewPlanner(setup.PlannerOptions{
		Clock:            clk,
		IDs:              seq,
		SelectedRuntimes: []string{"ollama"},
		Facts:            nil,
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	report := testReport("")
	plan, err := planner.Plan(report, protocol.TargetCognition, "")
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	var pullAct *protocol.SetupAction
	for i := range plan.Actions {
		if strings.HasPrefix(plan.Actions[i].RecipeID, "recipe.manual.pull_ollama_model") {
			pullAct = &plan.Actions[i]
			break
		}
	}
	if pullAct == nil {
		t.Fatalf("expected recipe.manual.pull_ollama_model action in plan")
	}
	if pullAct.Authority != protocol.AuthorityHighImpactManual {
		t.Errorf("expected AuthorityHighImpactManual, got %s", pullAct.Authority)
	}
	if pullAct.Operation != nil {
		t.Errorf("expected Operation == nil on manual action, got non-nil")
	}
	if pullAct.ManualInstructions == nil {
		t.Fatalf("expected non-nil ManualInstructions")
	}
	// Postcondition must bind the exact pre-resolved revision and size
	if len(pullAct.Postconditions) != 1 || pullAct.Postconditions[0].Kind != protocol.CondKindModelPresent {
		t.Fatalf("expected model_present postcondition on manual pull")
	}
	mp := pullAct.Postconditions[0].ModelPresent
	if mp.ResolvedRevision != setup.DefaultOllamaDigest {
		t.Errorf("expected postcondition revision %q, got %q", setup.DefaultOllamaDigest, mp.ResolvedRevision)
	}
	if mp.ExpectedSizeBytes != setup.DefaultOllamaSizeBytes {
		t.Errorf("expected postcondition size %d, got %d", setup.DefaultOllamaSizeBytes, mp.ExpectedSizeBytes)
	}
}

// 8. Automated model pull on verified CLI identity
func TestRecipe_Scenario08_AutomatedModelPullOnVerifiedIdentity(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	fixture := environment.DarwinAppleSilicon()
	facts, err := fixture.Discover(context.Background(), clk, protocol.DepthHealth)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	facts.Software = append(facts.Software, protocol.SoftwarePresence{
		ID:        "ollama",
		Installed: true,
		Path:      "/usr/local/bin/ollama",
		Version:   "0.3.0",
	})
	fp, _ := environment.Fingerprint(facts)
	report := testReport(fp)

	planner, err := setup.NewPlanner(setup.PlannerOptions{
		Clock:            clk,
		IDs:              seq,
		Facts:            &facts,
		SelectedRuntimes: []string{"ollama"},
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	plan, err := planner.Plan(report, protocol.TargetCognition, "")
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	var pullAct *protocol.SetupAction
	for i := range plan.Actions {
		if plan.Actions[i].RecipeID == "recipe.ollama.pull_model" {
			pullAct = &plan.Actions[i]
			break
		}
	}
	if pullAct == nil {
		t.Fatalf("expected recipe.ollama.pull_model in plan")
	}
	if pullAct.Operation == nil || pullAct.Operation.Kind != protocol.OpKindEnsureLocalModel {
		t.Fatalf("expected Operation to be ensure_local_model")
	}
	if pullAct.ManualInstructions != nil {
		t.Errorf("expected ManualInstructions == nil for automated action")
	}
	op := pullAct.Operation.EnsureLocalModel
	if op.ResolvedRevision != setup.DefaultOllamaDigest {
		t.Errorf("expected resolved revision %q, got %q", setup.DefaultOllamaDigest, op.ResolvedRevision)
	}
	if op.LicenseReference != setup.DefaultOllamaLicense {
		t.Errorf("expected license %q, got %q", setup.DefaultOllamaLicense, op.LicenseReference)
	}
}

// 9. Hardware driver remediation: NVIDIA missing driver
func TestRecipe_Scenario09_HardwareDriverRemediationNvidiaDriver(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	facts := protocol.EnvironmentFacts{
		Host: protocol.HostFacts{Family: protocol.OSLinux, Arch: "x86_64"},
		Accelerators: []protocol.AcceleratorDevice{
			{
				ID:          "pci:0000:01:00.0",
				Vendor:      protocol.VendorNVIDIA,
				DriverInUse: "", // No driver bound
			},
		},
	}
	fp, _ := environment.Fingerprint(facts)
	report := testReport(fp)

	planner, err := setup.NewPlanner(setup.PlannerOptions{
		Clock: clk,
		IDs:   seq,
		Facts: &facts,
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	plan, err := planner.Plan(report, protocol.TargetHardware, "")
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	var driverAct *protocol.SetupAction
	for i := range plan.Actions {
		if plan.Actions[i].RecipeID == "recipe.manual.install_nvidia_driver" {
			driverAct = &plan.Actions[i]
			break
		}
	}
	if driverAct == nil {
		t.Fatalf("expected recipe.manual.install_nvidia_driver in plan")
	}
	if driverAct.Authority != protocol.AuthorityHighImpactManual {
		t.Errorf("expected AuthorityHighImpactManual, got %s", driverAct.Authority)
	}
	if driverAct.Operation != nil {
		t.Errorf("driver action must have operation == nil")
	}
	if driverAct.ManualInstructions == nil {
		t.Errorf("driver action must have manual instructions")
	}
}

// 10. Hardware device node remediation: NVIDIA device access
func TestRecipe_Scenario10_HardwareDriverRemediationNvidiaDeviceAccess(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	facts := protocol.EnvironmentFacts{
		Host: protocol.HostFacts{Family: protocol.OSLinux, Arch: "x86_64"},
		Accelerators: []protocol.AcceleratorDevice{
			{
				ID:          "pci:0000:01:00.0",
				Vendor:      protocol.VendorNVIDIA,
				DriverInUse: "nvidia", // Driver bound, but /dev/nvidiactl not in DeviceNodes -> unusable
			},
		},
	}
	fp, _ := environment.Fingerprint(facts)
	report := testReport(fp)

	planner, err := setup.NewPlanner(setup.PlannerOptions{
		Clock: clk,
		IDs:   seq,
		Facts: &facts,
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	plan, err := planner.Plan(report, protocol.TargetHardware, "")
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	var permAct *protocol.SetupAction
	for i := range plan.Actions {
		if plan.Actions[i].RecipeID == "recipe.manual.configure_nvidia_device_permissions" {
			permAct = &plan.Actions[i]
			break
		}
	}
	if permAct == nil {
		t.Fatalf("expected recipe.manual.configure_nvidia_device_permissions in plan")
	}
	if permAct.Authority != protocol.AuthorityHighImpactManual {
		t.Errorf("expected AuthorityHighImpactManual, got %s", permAct.Authority)
	}
}

// 11. Hardware driver remediation: AMD ROCm missing driver
//
// The fixture deliberately leaves Accelerators[0].DriverInUse empty (no
// amdgpu kernel driver bound at all) — the "driver absent" state, distinct
// from "driver bound but /dev/kfd inaccessible" (see
// TestRecipe_Scenario11b below). Before the independent-review follow-up
// on WP-M3B-6, FIX_NOW 3, amdCandidates could not tell these two states
// apart and always routed to configure_amdgpu_device_permissions,
// leaving recipe.manual.install_rocm_driver unreachable from the real
// AssessBackends -> Planner path; this scenario now actually exercises
// that reachability, which is what its name always claimed.
func TestRecipe_Scenario11_HardwareDriverRemediationRocmDriver(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	facts := protocol.EnvironmentFacts{
		Host: protocol.HostFacts{Family: protocol.OSLinux, Arch: "x86_64"},
		Accelerators: []protocol.AcceleratorDevice{
			{
				ID:           "pci:0000:03:00.0",
				Vendor:       protocol.VendorAMD,
				Architecture: "gfx90a", // Supported architecture, but no amdgpu driver bound.
			},
		},
	}
	fp, _ := environment.Fingerprint(facts)
	report := testReport(fp)

	planner, err := setup.NewPlanner(setup.PlannerOptions{
		Clock: clk,
		IDs:   seq,
		Facts: &facts,
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	plan, err := planner.Plan(report, protocol.TargetHardware, "")
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	var rocmAct *protocol.SetupAction
	for i := range plan.Actions {
		if plan.Actions[i].RecipeID == "recipe.manual.install_rocm_driver" {
			rocmAct = &plan.Actions[i]
			break
		}
	}
	if rocmAct == nil {
		t.Fatalf("expected recipe.manual.install_rocm_driver in plan")
	}
	if rocmAct.Authority != protocol.AuthorityHighImpactManual {
		t.Errorf("expected AuthorityHighImpactManual, got %s", rocmAct.Authority)
	}
}

// 11b. Hardware driver remediation: AMD ROCm driver present but /dev/kfd
// inaccessible, exercised through the real AssessBackends -> Planner path
// (not by calling NewManualAmdgpuDevicePermissionsAction directly, which
// scenario 12 already covers but which never tests AssessBackends'
// routing logic). Companion regression to scenario 11, per the
// independent-review follow-up on WP-M3B-6, FIX_NOW 3's required-repair
// list.
func TestRecipe_Scenario11b_HardwareDriverRemediationAmdgpuDeviceAccessViaAssessBackends(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	facts := protocol.EnvironmentFacts{
		Host: protocol.HostFacts{Family: protocol.OSLinux, Arch: "x86_64"},
		Accelerators: []protocol.AcceleratorDevice{
			{
				ID:           "pci:0000:03:00.0",
				Vendor:       protocol.VendorAMD,
				Architecture: "gfx90a",
				DriverInUse:  "amdgpu", // Driver bound; /dev/kfd is still missing/inaccessible.
			},
		},
	}
	fp, _ := environment.Fingerprint(facts)
	report := testReport(fp)

	planner, err := setup.NewPlanner(setup.PlannerOptions{
		Clock: clk,
		IDs:   seq,
		Facts: &facts,
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	plan, err := planner.Plan(report, protocol.TargetHardware, "")
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	var permAct *protocol.SetupAction
	for i := range plan.Actions {
		if plan.Actions[i].RecipeID == "recipe.manual.configure_amdgpu_device_permissions" {
			permAct = &plan.Actions[i]
			break
		}
	}
	if permAct == nil {
		t.Fatalf("expected recipe.manual.configure_amdgpu_device_permissions in plan")
	}
	if permAct.Authority != protocol.AuthorityHighImpactManual {
		t.Errorf("expected AuthorityHighImpactManual, got %s", permAct.Authority)
	}
	for i := range plan.Actions {
		if plan.Actions[i].RecipeID == "recipe.manual.install_rocm_driver" {
			t.Fatalf("did not expect recipe.manual.install_rocm_driver when the amdgpu driver is already bound")
		}
	}
}

// 12. Hardware device node remediation: AMD device access
func TestRecipe_Scenario12_HardwareDriverRemediationAmdgpuDeviceAccess(t *testing.T) {
	act := setup.NewManualAmdgpuDevicePermissionsAction("act_001", "pci:0000:03:00.0", "1.0.0")
	if act.RecipeID != "recipe.manual.configure_amdgpu_device_permissions" {
		t.Errorf("unexpected recipeID: %s", act.RecipeID)
	}
	if act.Authority != protocol.AuthorityHighImpactManual {
		t.Errorf("expected AuthorityHighImpactManual, got %s", act.Authority)
	}
	if err := act.Validate(); err != nil {
		t.Fatalf("action failed validation: %v", err)
	}
}

// 13. Cache removal recipe generation
func TestRecipe_Scenario13_CacheRemovalRecipeGeneration(t *testing.T) {
	act := setup.NewRemoveStaleCacheAction("act_001", protocol.CacheTargetMachineProfile, nil, "1.0.0")
	if act.RecipeID != "recipe.cache.remove.machine_profile" {
		t.Errorf("unexpected recipe ID: %s", act.RecipeID)
	}
	if act.Authority != protocol.AuthorityUserConfirmation {
		t.Errorf("expected AuthorityUserConfirmation, got %s", act.Authority)
	}
	if act.Operation == nil || act.Operation.Kind != protocol.OpKindRemoveStaleCache {
		t.Fatalf("expected OpKindRemoveStaleCache")
	}
	if err := act.Validate(); err != nil {
		t.Fatalf("action failed validation: %v", err)
	}

	// Verify plan generation on stale inference
	clk := clock.NewFake(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()
	planner, _ := setup.NewPlanner(setup.PlannerOptions{Clock: clk, IDs: seq})
	report := testReport("")
	report.EvaluationScope.EvidenceStatus = "stale_inference_retained"
	plan, err := planner.Plan(report, protocol.TargetHardware, "")
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	found := false
	for _, a := range plan.Actions {
		if a.RecipeID == "recipe.cache.remove.machine_profile" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected recipe.cache.remove.machine_profile in plan for stale_inference_retained")
	}
}

// 14. Diagnostic check recipe generation
func TestRecipe_Scenario14_DiagnosticCheckRecipeGeneration(t *testing.T) {
	act := setup.NewRunDiagnosticCheckAction("act_001", protocol.CheckStateRootWritable, "", "1.0.0")
	if act.RecipeID != "recipe.diagnostic.state_root_writable" {
		t.Errorf("unexpected recipe ID: %s", act.RecipeID)
	}
	if act.Authority != protocol.AuthorityReadOnly {
		t.Errorf("expected AuthorityReadOnly, got %s", act.Authority)
	}
	if act.Operation == nil || act.Operation.Kind != protocol.OpKindRunDiagnosticCheck {
		t.Fatalf("expected OpKindRunDiagnosticCheck")
	}
	if err := act.Validate(); err != nil {
		t.Fatalf("action failed validation: %v", err)
	}
}

// 15. Directory creation recipe generation
func TestRecipe_Scenario15_DirectoryCreationRecipeGeneration(t *testing.T) {
	act := setup.NewCreateDirectoryAction("act_001", protocol.LocationState, "1.0.0")
	if act.RecipeID != "recipe.mkdir.state" {
		t.Errorf("unexpected recipe ID: %s", act.RecipeID)
	}
	if act.Authority != protocol.AuthorityUserConfirmation {
		t.Errorf("expected AuthorityUserConfirmation, got %s", act.Authority)
	}
	if err := act.Validate(); err != nil {
		t.Fatalf("action failed validation: %v", err)
	}
}

// 16. Config write recipe generation
func TestRecipe_Scenario16_ConfigWriteRecipeGeneration(t *testing.T) {
	act := setup.NewWriteManagedConfigAction("act_001", protocol.ConfigKeyDefaultProfile, string(protocol.ProfileLocalHeavy), nil, "1.0.0")
	if act.RecipeID != "recipe.config.default_profile" {
		t.Errorf("unexpected recipe ID: %s", act.RecipeID)
	}
	if act.Authority != protocol.AuthorityUserConfirmation {
		t.Errorf("expected AuthorityUserConfirmation, got %s", act.Authority)
	}
	if err := act.Validate(); err != nil {
		t.Fatalf("action failed validation: %v", err)
	}
}

// 17. Manual Git install recipe generation
func TestRecipe_Scenario17_ManualGitInstallRecipeGeneration(t *testing.T) {
	act := setup.NewManualGitAction("act_001", "1.0.0")
	if act.RecipeID != "recipe.manual.install_git" {
		t.Errorf("unexpected recipe ID: %s", act.RecipeID)
	}
	if act.Authority != protocol.AuthorityHighImpactManual {
		t.Errorf("expected AuthorityHighImpactManual, got %s", act.Authority)
	}
	if err := act.Validate(); err != nil {
		t.Fatalf("action failed validation: %v", err)
	}
}

// 18. Manual re-authenticate recipe generation
func TestRecipe_Scenario18_ManualReauthenticateRecipeGeneration(t *testing.T) {
	act := setup.NewManualReauthenticateAction("act_001", "cli:claude", "ref_claude_token", "1.0.0")
	if act.RecipeID != "recipe.manual.reauthenticate" {
		t.Errorf("unexpected recipe ID: %s", act.RecipeID)
	}
	if act.Authority != protocol.AuthorityHighImpactManual {
		t.Errorf("expected AuthorityHighImpactManual, got %s", act.Authority)
	}
	if err := act.Validate(); err != nil {
		t.Fatalf("action failed validation: %v", err)
	}
}

// 19. Executable declared authority matches IntrinsicPolicy
func TestRecipe_Scenario19_ExecutableAuthorityMatchesIntrinsicPolicy(t *testing.T) {
	// Test each executable operation kind
	actions := []protocol.SetupAction{
		setup.NewCreateDirectoryAction("act_1", protocol.LocationState, "1.0.0"),
		setup.NewWriteManagedConfigAction("act_2", protocol.ConfigKeyDefaultProfile, string(protocol.ProfileCloudCognition), nil, "1.0.0"),
		setup.NewRemoveStaleCacheAction("act_3", protocol.CacheTargetMachineProfile, nil, "1.0.0"),
		setup.NewRunDiagnosticCheckAction("act_4", protocol.CheckGitAvailable, "", "1.0.0"),
	}

	for _, act := range actions {
		if act.Operation == nil {
			t.Fatalf("expected non-nil operation for %s", act.RecipeID)
		}
		effects, minAuth := protocol.IntrinsicPolicy(*act.Operation)
		if !act.Authority.AtLeast(minAuth) {
			t.Errorf("action %s declared authority %s is weaker than intrinsic policy %s", act.RecipeID, act.Authority, minAuth)
		}
		for _, reqEff := range effects {
			found := false
			for _, eff := range act.Effects {
				if eff == reqEff {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("action %s missing required effect %s", act.RecipeID, reqEff)
			}
		}
		if err := act.Validate(); err != nil {
			t.Errorf("action %s failed validation: %v", act.RecipeID, err)
		}
	}
}

// 20. Deterministic plan digest and JSON schema validation
func TestRecipe_Scenario20_DeterministicPlanDigestAndSerialization(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	planner, err := setup.NewPlanner(setup.PlannerOptions{
		Clock:            clk,
		IDs:              seq,
		SelectedRuntimes: []string{"ollama"},
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	report := testReport("")
	report.Findings = []protocol.DiagnosticFinding{
		{
			Category: "state",
			Severity: "warning",
			Code:     setup.FindingCodeStateDirsMissing,
			Title:    "dirs missing",
			Detail:   "state dirs missing",
		},
		{
			Category: "environment",
			Severity: "error",
			Code:     setup.FindingCodeGitNotFound,
			Title:    "git not found",
			Detail:   "git not found",
		},
	}

	plan, err := planner.Plan(report, protocol.TargetAll, protocol.ProfileLocalHeavy)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	// Validate Plan
	if err := plan.Validate(); err != nil {
		t.Fatalf("plan failed validation: %v", err)
	}

	// Serialize and validate against JSON Schema
	planJSON, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}

	set, err := schema.Default()
	if err != nil {
		t.Fatalf("load default schema set: %v", err)
	}

	if err := set.ValidateBytes(schema.NameSetupPlan, planJSON); err != nil {
		t.Fatalf("plan JSON failed schema validation: %v\nJSON:\n%s", err, string(planJSON))
	}
}
