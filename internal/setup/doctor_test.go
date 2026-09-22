package setup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/protocol"
)

func int64Ptr(v int64) *int64 { return &v }

func TestDoctorStateRootCheck(t *testing.T) {
	ctx := context.Background()
	tmpHome := t.TempDir()
	clk := clock.NewFake(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	doc, err := NewDoctor(DoctorOptions{
		Clock:   clk,
		IDs:     seq,
		HomeDir: tmpHome,
	})
	if err != nil {
		t.Fatalf("NewDoctor: %v", err)
	}

	profile := protocol.ProfileCustom
	scope := protocol.ReadinessEvaluationScope{
		TargetProfile:  &profile,
		RequiredRoles:  []string{"implementation"},
		EvidenceStatus: "live",
	}
	facts := protocol.EnvironmentFacts{
		Host: protocol.HostFacts{
			Family: protocol.OSDarwin,
			Arch:   "arm64",
		},
		CPU: protocol.CPUFacts{},
		Memory: protocol.MemoryFacts{
			TotalBytes: int64Ptr(32 * 1024 * 1024 * 1024),
		},
		Virtualization: protocol.VirtualizationFacts{
			Container: protocol.ContainerNone,
		},
		Software: []protocol.SoftwarePresence{
			{ID: "git", Category: protocol.SoftwareEngineering, Installed: true, Version: "2.45.0"},
		},
	}

	// First run: HomeDir exists, but subdirs (state, artifacts_setup, tmp) are missing
	report, err := doc.Run(ctx, scope, facts)
	if err != nil {
		t.Fatalf("doc.Run: %v", err)
	}

	hasMissingDirs := false
	for _, f := range report.Findings {
		if f.Code == FindingCodeStateDirsMissing {
			hasMissingDirs = true
			break
		}
	}
	if !hasMissingDirs {
		t.Fatalf("expected finding %s for missing subdirs", FindingCodeStateDirsMissing)
	}

	// Create subdirectories (canonical: state, artifacts/setup, tmp)
	for _, sub := range []string{"state", filepath.Join("artifacts", "setup"), "tmp"} {
		_ = os.MkdirAll(filepath.Join(tmpHome, sub), 0700)
	}

	report2, err := doc.Run(ctx, scope, facts)
	if err != nil {
		t.Fatalf("doc.Run (second): %v", err)
	}

	hasStateReady := false
	for _, f := range report2.Findings {
		if f.Code == FindingCodeStateRootReady {
			hasStateReady = true
			break
		}
	}
	if !hasStateReady {
		t.Fatalf("expected finding %s after creating subdirs", FindingCodeStateRootReady)
	}
}

func TestDoctorGitMissing(t *testing.T) {
	ctx := context.Background()
	tmpHome := t.TempDir()
	clk := clock.NewFake(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	doc, err := NewDoctor(DoctorOptions{
		Clock:   clk,
		IDs:     seq,
		HomeDir: tmpHome,
	})
	if err != nil {
		t.Fatalf("NewDoctor: %v", err)
	}

	profile := protocol.ProfileCustom
	scope := protocol.ReadinessEvaluationScope{
		TargetProfile:  &profile,
		RequiredRoles:  []string{"implementation"},
		EvidenceStatus: "live",
	}
	facts := protocol.EnvironmentFacts{
		Host: protocol.HostFacts{
			Family: protocol.OSDarwin,
			Arch:   "arm64",
		},
		CPU: protocol.CPUFacts{},
		Memory: protocol.MemoryFacts{
			TotalBytes: int64Ptr(32 * 1024 * 1024 * 1024),
		},
		Virtualization: protocol.VirtualizationFacts{
			Container: protocol.ContainerNone,
		},
		Software: []protocol.SoftwarePresence{
			{ID: "git", Category: protocol.SoftwareEngineering, Installed: false},
		},
	}

	report, err := doc.Run(ctx, scope, facts)
	if err != nil {
		t.Fatalf("doc.Run: %v", err)
	}

	if err := report.Validate(); err != nil {
		t.Fatalf("report validation: %v", err)
	}

	if report.Readiness != protocol.ReadinessActionRequired {
		t.Fatalf("expected readiness ACTION_REQUIRED when git is missing, got %s", report.Readiness)
	}
}

func TestDoctorReadinessScopedToCloudCognitionWithOnlyLocalMLX(t *testing.T) {
	doc, err := NewDoctor(DoctorOptions{
		Clock: clock.NewFake(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), 0),
		IDs:   ids.NewSequential(),
	})
	if err != nil {
		t.Fatalf("NewDoctor: %v", err)
	}

	cloudProfile := protocol.ProfileCloudCognition
	scope := protocol.ReadinessEvaluationScope{
		TargetProfile:  &cloudProfile,
		RequiredRoles:  []string{"implementation"},
		EvidenceStatus: "live",
	}

	// Only local MLX endpoint discovered; no remote endpoints
	endpoints := []protocol.CognitionEndpointSummary{
		{
			ID:                   "mlx_local",
			Kind:                 protocol.EndpointLocalRuntime,
			Locality:             protocol.LocalityLocal,
			Health:               protocol.EndpointHealthReady,
			AccelerationVerified: true,
		},
	}

	readiness := doc.evaluateReadiness(scope, nil, endpoints, nil, &cloudProfile)
	if readiness == protocol.ReadinessReady {
		t.Fatalf("cloud-cognition target with only local MLX must NOT be READY; got %s", readiness)
	}
	if readiness != protocol.ReadinessPartiallyReady {
		t.Fatalf("expected PARTIALLY_READY, got %s", readiness)
	}
}

func TestDoctorReadinessLocalHeavyUnverifiedAcceleration(t *testing.T) {
	doc, err := NewDoctor(DoctorOptions{
		Clock: clock.NewFake(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), 0),
		IDs:   ids.NewSequential(),
	})
	if err != nil {
		t.Fatalf("NewDoctor: %v", err)
	}

	heavyProfile := protocol.ProfileLocalHeavy
	scope := protocol.ReadinessEvaluationScope{
		TargetProfile:  &heavyProfile,
		RequiredRoles:  []string{"scout"},
		EvidenceStatus: "live",
	}

	// Local runtime present, but acceleration is unverified
	endpoints := []protocol.CognitionEndpointSummary{
		{
			ID:                   "ollama_local",
			Kind:                 protocol.EndpointLocalRuntime,
			Locality:             protocol.LocalityLocal,
			Health:               protocol.EndpointHealthReady,
			AccelerationVerified: false,
		},
	}

	readiness := doc.evaluateReadiness(scope, nil, endpoints, nil, &heavyProfile)
	if readiness == protocol.ReadinessReady {
		t.Fatalf("local-heavy target with unverified acceleration must NOT be READY; got %s", readiness)
	}
	if readiness != protocol.ReadinessPartiallyReady {
		t.Fatalf("expected PARTIALLY_READY, got %s", readiness)
	}
}

func TestDoctorReadinessOfflineRejectsRemoteEndpoints(t *testing.T) {
	doc, err := NewDoctor(DoctorOptions{
		Clock: clock.NewFake(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), 0),
		IDs:   ids.NewSequential(),
	})
	if err != nil {
		t.Fatalf("NewDoctor: %v", err)
	}

	offlineProfile := protocol.ProfileOffline
	scope := protocol.ReadinessEvaluationScope{
		TargetProfile:  &offlineProfile,
		RequiredRoles:  []string{"implementation"},
		EvidenceStatus: "live",
	}

	// Remote endpoints only, no local endpoints
	endpoints := []protocol.CognitionEndpointSummary{
		{
			ID:        "remote_api",
			Kind:      protocol.EndpointRemoteAPI,
			Locality:  protocol.LocalityRemote,
			Health:    protocol.EndpointHealthReady,
			Auth:      protocol.AuthAuthenticated,
			CostClass: protocol.CostRemoteEconomy,
		},
	}

	readiness := doc.evaluateReadiness(scope, nil, endpoints, nil, &offlineProfile)
	if readiness == protocol.ReadinessReady {
		t.Fatalf("offline target with only remote endpoints must NOT be READY; got %s", readiness)
	}
	if readiness != protocol.ReadinessPartiallyReady {
		t.Fatalf("expected PARTIALLY_READY, got %s", readiness)
	}
}

func TestDoctorReadinessMissingRequiredRoles(t *testing.T) {
	doc, err := NewDoctor(DoctorOptions{
		Clock: clock.NewFake(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), 0),
		IDs:   ids.NewSequential(),
	})
	if err != nil {
		t.Fatalf("NewDoctor: %v", err)
	}

	hybridProfile := protocol.ProfileHybridThin
	scope := protocol.ReadinessEvaluationScope{
		TargetProfile:  &hybridProfile,
		RequiredRoles:  []string{"principal"}, // requires remote endpoint in hybrid-thin
		EvidenceStatus: "live",
	}

	// Only local endpoint is ready, no remote endpoint available to fulfill principal
	endpoints := []protocol.CognitionEndpointSummary{
		{
			ID:                   "ollama_local",
			Kind:                 protocol.EndpointLocalRuntime,
			Locality:             protocol.LocalityLocal,
			Health:               protocol.EndpointHealthReady,
			AccelerationVerified: true,
		},
	}

	readiness := doc.evaluateReadiness(scope, nil, endpoints, nil, &hybridProfile)
	if readiness != protocol.ReadinessPartiallyReady {
		t.Fatalf("expected PARTIALLY_READY when required role cannot be satisfied, got %s", readiness)
	}
}

func TestDoctorReadinessUnknownRoleFails(t *testing.T) {
	doc, err := NewDoctor(DoctorOptions{
		Clock: clock.NewFake(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), 0),
		IDs:   ids.NewSequential(),
	})
	if err != nil {
		t.Fatalf("NewDoctor: %v", err)
	}

	cloudProfile := protocol.ProfileCloudCognition
	scope := protocol.ReadinessEvaluationScope{
		TargetProfile:  &cloudProfile,
		RequiredRoles:  []string{"unknown_custom_role"},
		EvidenceStatus: "live",
	}

	endpoints := []protocol.CognitionEndpointSummary{
		{
			ID:        "remote_api",
			Kind:      protocol.EndpointRemoteAPI,
			Locality:  protocol.LocalityRemote,
			Health:    protocol.EndpointHealthReady,
			Auth:      protocol.AuthAuthenticated,
			CostClass: protocol.CostRemoteEconomy,
		},
	}

	readiness := doc.evaluateReadiness(scope, nil, endpoints, nil, &cloudProfile)
	if readiness != protocol.ReadinessPartiallyReady {
		t.Fatalf("expected PARTIALLY_READY for unknown role, got %s", readiness)
	}
}

func TestDoctorReadinessRemoteWithoutImplementationGradeFails(t *testing.T) {
	doc, err := NewDoctor(DoctorOptions{
		Clock: clock.NewFake(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), 0),
		IDs:   ids.NewSequential(),
	})
	if err != nil {
		t.Fatalf("NewDoctor: %v", err)
	}

	cloudProfile := protocol.ProfileCloudCognition
	scope := protocol.ReadinessEvaluationScope{
		TargetProfile:  &cloudProfile,
		RequiredRoles:  []string{"implementer"},
		EvidenceStatus: "live",
	}

	cognProfile := &protocol.MachineCapabilityProfile{
		Endpoints: []protocol.CognitionEndpoint{
			{
				ID:                     "remote_api",
				Kind:                   protocol.EndpointRemoteAPI,
				Locality:               protocol.LocalityRemote,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthAuthenticated,
				CostClass:              protocol.CostRemoteEconomy,
				RequiredSourceExposure: protocol.ExposureFocusedSnippets,
				// Capabilities is intentionally empty: no implementation grade
			},
		},
	}

	readiness := doc.evaluateReadiness(scope, nil, nil, cognProfile, &cloudProfile)
	if readiness != protocol.ReadinessPartiallyReady {
		t.Fatalf("expected PARTIALLY_READY when remote endpoint lacks implementation grade, got %s", readiness)
	}
}

func TestDoctorReadinessRemotePolicyExposureViolationFails(t *testing.T) {
	strictPolicy := cognition.Policy{
		MaxSourceExposure: protocol.ExposureLocalOnly,
		MaxCostClass:      protocol.CostRemoteEconomy,
	}
	doc, err := NewDoctor(DoctorOptions{
		Clock:  clock.NewFake(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), 0),
		IDs:    ids.NewSequential(),
		Policy: &strictPolicy,
	})
	if err != nil {
		t.Fatalf("NewDoctor: %v", err)
	}

	cloudProfile := protocol.ProfileCloudCognition
	scope := protocol.ReadinessEvaluationScope{
		TargetProfile:  &cloudProfile,
		RequiredRoles:  []string{"implementer"},
		EvidenceStatus: "live",
	}

	cognProfile := &protocol.MachineCapabilityProfile{
		Endpoints: []protocol.CognitionEndpoint{
			{
				ID:                     "remote_api",
				Kind:                   protocol.EndpointRemoteAPI,
				Locality:               protocol.LocalityRemote,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthAuthenticated,
				CostClass:              protocol.CostRemoteEconomy,
				RequiredSourceExposure: protocol.ExposureFocusedSnippets, // exceeds ExposureLocalOnly
				Capabilities: []protocol.GradedCapability{
					{
						Dimension:  protocol.CapabilityImplementation,
						Grade:      protocol.GradeStrong,
						Provenance: protocol.ProvenanceConfigured,
					},
				},
			},
		},
	}

	readiness := doc.evaluateReadiness(scope, nil, nil, cognProfile, &cloudProfile)
	if readiness != protocol.ReadinessPartiallyReady {
		t.Fatalf("expected PARTIALLY_READY when endpoint violates source exposure policy, got %s", readiness)
	}
}

type stubCognitionAdapter struct {
	endpoints []protocol.CognitionEndpoint
}

func (s *stubCognitionAdapter) ID() string { return "stub" }
func (s *stubCognitionAdapter) Discover(ctx context.Context, in cognition.DiscoveryInput) ([]protocol.CognitionEndpoint, error) {
	return s.endpoints, nil
}
func (s *stubCognitionAdapter) Probe(ctx context.Context, endpoint protocol.CognitionEndpoint, req cognition.ProbeRequest) (cognition.ProbeResult, error) {
	return cognition.ProbeResult{}, nil
}

func TestDoctorCachedInferenceEvidenceMergedAndPreserved(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	clk := clock.NewFake(now, 0)
	idSrc := ids.NewSequential()

	cm, err := NewCacheManager(tmpDir, clk, 1*time.Hour)
	if err != nil {
		t.Fatalf("NewCacheManager: %v", err)
	}

	fingerprint := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	facts := protocol.EnvironmentFacts{
		Host: protocol.HostFacts{
			Family: protocol.OSDarwin,
			Arch:   "arm64",
		},
		Virtualization: protocol.VirtualizationFacts{
			Container: protocol.ContainerNone,
		},
	}

	// Prime the cache with verified acceleration and evaluated implementation capability
	cachedVerifiedTime := protocol.NewTimestamp(now.Add(-10 * time.Minute))
	cachedProfile := protocol.MachineCapabilityProfile{
		SchemaVersion:         protocol.SchemaVersion1,
		ProfileID:             "mcp_cached",
		MachineFingerprint:    fingerprint,
		KnowledgeRevision:     "2026-09-22",
		ProbeDepth:            protocol.DepthInference,
		Assessment:            protocol.AssessmentReady,
		ObservedAt:            protocol.NewTimestamp(now.Add(-10 * time.Minute)),
		Environment:           facts,
		AcceleratorCandidates: []protocol.AcceleratorCandidate{},
		Endpoints: []protocol.CognitionEndpoint{
			{
				ID:                     "ollama_local",
				Kind:                   protocol.EndpointLocalRuntime,
				Locality:               protocol.LocalityLocal,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthNotApplicable,
				CostClass:              protocol.CostLocalCompute,
				RequiredSourceExposure: protocol.ExposureLocalOnly,
				ObservedAt:             protocol.NewTimestamp(now.Add(-10 * time.Minute)),
				StructuredOutput:       protocol.FeatureDeclared,
				ToolUse:                protocol.FeatureDeclared,
				Acceleration: &protocol.AccelerationEvidence{
					Backend:    protocol.BackendMetal,
					State:      protocol.StateVerified,
					VerifiedAt: &cachedVerifiedTime,
					Signals: []protocol.AccelerationSignal{
						{
							Source:    "ollama:/api/ps",
							Trust:     protocol.TrustAuthoritative,
							Backend:   protocol.BackendMetal,
							Offloaded: true,
						},
					},
				},
				Capabilities: []protocol.GradedCapability{
					{
						Dimension:  protocol.CapabilityImplementation,
						Grade:      protocol.GradeStrong,
						Provenance: protocol.ProvenanceEvaluated,
					},
				},
			},
		},
	}
	if err := cachedProfile.Validate(); err != nil {
		t.Fatalf("cachedProfile must be valid: %v", err)
	}
	if err := Write(ctx, cm, protocol.CacheTargetMachineProfile, fingerprint, cachedProfile, 1*time.Hour); err != nil {
		t.Fatalf("Write cache: %v", err)
	}

	// The live adapter returns a shallow health-only probe (Acceleration unverified, no implementation capability)
	stub := &stubCognitionAdapter{
		endpoints: []protocol.CognitionEndpoint{
			{
				ID:                     "ollama_local",
				Kind:                   protocol.EndpointLocalRuntime,
				Locality:               protocol.LocalityLocal,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthNotApplicable,
				CostClass:              protocol.CostLocalCompute,
				RequiredSourceExposure: protocol.ExposureLocalOnly,
				StructuredOutput:       protocol.FeatureUnsupported,
				ToolUse:                protocol.FeatureUnsupported,
				ObservedAt:             protocol.NewTimestamp(now),
				Acceleration: &protocol.AccelerationEvidence{
					Backend: protocol.BackendMetal,
					State:   protocol.StateUnverified, // shallow probe
				},
			},
		},
	}

	service, err := cognition.NewService(cognition.Options{
		Adapters: []cognition.Adapter{stub},
		Clock:    clk,
		IDs:      idSrc,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	doc, err := NewDoctor(DoctorOptions{
		Clock:            clk,
		IDs:              idSrc,
		Cache:            cm,
		CognitionService: service,
	})
	if err != nil {
		t.Fatalf("NewDoctor: %v", err)
	}

	summaries, _, profile, evidenceStatus, err := doc.discoverEndpoints(ctx, facts, fingerprint)
	if err != nil {
		t.Fatalf("discoverEndpoints: %v", err)
	}

	if evidenceStatus != "refreshed_health" {
		t.Fatalf("expected evidenceStatus refreshed_health, got %q", evidenceStatus)
	}
	if len(summaries) != 1 || !summaries[0].AccelerationVerified {
		t.Fatalf("expected merged endpoint summary to retain verified acceleration, got: %+v", summaries)
	}
	if profile == nil || len(profile.Endpoints) != 1 {
		t.Fatalf("expected 1 endpoint in merged profile")
	}
	mergedEp := profile.Endpoints[0]
	if !mergedEp.AccelerationVerified() {
		t.Fatalf("expected merged endpoint to retain verified acceleration")
	}
	if mergedEp.Acceleration.VerifiedAt == nil || *mergedEp.Acceleration.VerifiedAt != cachedVerifiedTime {
		t.Fatalf("expected verified_at timestamp to be preserved")
	}
	if mergedEp.Capability(protocol.CapabilityImplementation).Grade != protocol.GradeStrong {
		t.Fatalf("expected implementation capability GradeStrong to be preserved")
	}

	// Verify disk cache was updated with the merged profile (so verified acceleration persists)
	diskProfile, found, err := Read[protocol.MachineCapabilityProfile](ctx, cm, protocol.CacheTargetMachineProfile, fingerprint)
	if err != nil || !found {
		t.Fatalf("expected cached profile to exist on disk: %v", err)
	}
	if !diskProfile.Endpoints[0].AccelerationVerified() {
		t.Fatalf("expected disk cache to preserve verified acceleration")
	}

	// Now advance clock past cache TTL (default 24h).
	// A subsequent health probe should still retain verified acceleration, but report "stale_inference_retained"
	expiredClk := clock.NewFake(now.Add(25*time.Hour), 0)
	expiredCM, _ := NewCacheManager(tmpDir, expiredClk, 1*time.Hour)
	docExpired, err := NewDoctor(DoctorOptions{
		Clock:            expiredClk,
		IDs:              idSrc,
		Cache:            expiredCM,
		CognitionService: service,
	})
	if err != nil {
		t.Fatalf("NewDoctor with expired cache: %v", err)
	}

	_, _, _, statusExpired, err := docExpired.discoverEndpoints(ctx, facts, fingerprint)
	if err != nil {
		t.Fatalf("discoverEndpoints with expired cache: %v", err)
	}
	if statusExpired != "stale_inference_retained" {
		t.Fatalf("expected evidenceStatus stale_inference_retained, got %q", statusExpired)
	}

	// Calling discoverEndpoints a second time with the expired cache must also return "stale_inference_retained"
	// (proves that the first health probe did not refresh the stale inference evidence in cache)
	_, _, _, statusExpired2, err := docExpired.discoverEndpoints(ctx, facts, fingerprint)
	if err != nil {
		t.Fatalf("discoverEndpoints second call with expired cache: %v", err)
	}
	if statusExpired2 != "stale_inference_retained" {
		t.Fatalf("expected evidenceStatus stale_inference_retained on second run, got %q", statusExpired2)
	}
}
