package setup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
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

	readiness := doc.evaluateReadiness(scope, nil, endpoints, &cloudProfile)
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

	readiness := doc.evaluateReadiness(scope, nil, endpoints, &heavyProfile)
	if readiness == protocol.ReadinessReady {
		t.Fatalf("local-heavy target with unverified acceleration must NOT be READY; got %s", readiness)
	}
	if readiness != protocol.ReadinessReadyWithReducedCap {
		t.Fatalf("expected READY_WITH_REDUCED_CAPABILITY, got %s", readiness)
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

	readiness := doc.evaluateReadiness(scope, nil, endpoints, &offlineProfile)
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

	readiness := doc.evaluateReadiness(scope, nil, endpoints, &hybridProfile)
	if readiness != protocol.ReadinessPartiallyReady {
		t.Fatalf("expected PARTIALLY_READY when required role cannot be satisfied, got %s", readiness)
	}
}
