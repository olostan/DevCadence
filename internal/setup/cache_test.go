package setup

import (
	"context"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/protocol"
)

type sampleData struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestCacheManagerReadWriteRoundTrip(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	clk := clock.NewFake(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), 0)

	cm, err := NewCacheManager(tmpDir, clk, 1*time.Hour)
	if err != nil {
		t.Fatalf("NewCacheManager: %v", err)
	}

	fingerprint := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	data := sampleData{Name: "test-profile", Count: 42}

	if err := Write(ctx, cm, protocol.CacheTargetMachineProfile, fingerprint, data, 1*time.Hour); err != nil {
		t.Fatalf("Write: %v", err)
	}

	readData, found, err := Read[sampleData](ctx, cm, protocol.CacheTargetMachineProfile, fingerprint)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !found {
		t.Fatalf("expected cache hit, got not found")
	}
	if readData != data {
		t.Fatalf("expected %+v, got %+v", data, readData)
	}
}

func TestCacheManagerExpires(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	clk := clock.NewFake(now, 0)

	cm, err := NewCacheManager(tmpDir, clk, 10*time.Minute)
	if err != nil {
		t.Fatalf("NewCacheManager: %v", err)
	}

	fingerprint := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	data := sampleData{Name: "temp", Count: 1}

	if err := Write(ctx, cm, protocol.CacheTargetMachineProfile, fingerprint, data, 10*time.Minute); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Advance clock past expiration
	laterClk := clock.NewFake(now.Add(15*time.Minute), 0)
	laterCM, _ := NewCacheManager(tmpDir, laterClk, 10*time.Minute)

	_, found, err := Read[sampleData](ctx, laterCM, protocol.CacheTargetMachineProfile, fingerprint)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if found {
		t.Fatalf("expected expired cache to be dropped, but was found")
	}
}

func TestCacheManagerFingerprintMismatch(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	clk := clock.NewFake(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), 0)

	cm, err := NewCacheManager(tmpDir, clk, 1*time.Hour)
	if err != nil {
		t.Fatalf("NewCacheManager: %v", err)
	}

	fp1 := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	fp2 := "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	data := sampleData{Name: "data", Count: 10}

	if err := Write(ctx, cm, protocol.CacheTargetMachineProfile, fp1, data, 1*time.Hour); err != nil {
		t.Fatalf("Write: %v", err)
	}

	_, found, err := Read[sampleData](ctx, cm, protocol.CacheTargetMachineProfile, fp2)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if found {
		t.Fatalf("expected cache to be invalidated on machine fingerprint mismatch")
	}
}

func TestCacheManagerRemove(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	clk := clock.NewFake(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC), 0)

	cm, err := NewCacheManager(tmpDir, clk, 1*time.Hour)
	if err != nil {
		t.Fatalf("NewCacheManager: %v", err)
	}

	fp := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	data := sampleData{Name: "data", Count: 10}

	if err := Write(ctx, cm, protocol.CacheTargetEndpointProbes, fp, data, 1*time.Hour); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := cm.Remove(ctx, protocol.CacheTargetEndpointProbes); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	_, found, err := Read[sampleData](ctx, cm, protocol.CacheTargetEndpointProbes, fp)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if found {
		t.Fatalf("expected cache to be removed")
	}
}

func TestCacheManagerInvalidTarget(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	cm, _ := NewCacheManager(tmpDir, clock.System(), 1*time.Hour)

	err := Write(ctx, cm, protocol.CacheTarget("invalid_target"), "fp", sampleData{}, 1*time.Hour)
	if err == nil {
		t.Fatalf("expected error for invalid target")
	}

	_, _, err = Read[sampleData](ctx, cm, protocol.CacheTarget("invalid_target"), "fp")
	if err == nil {
		t.Fatalf("expected error for invalid target")
	}
}

func TestCacheManagerReadEntryRetainsExpired(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	clk := clock.NewFake(now, 0)

	cm, err := NewCacheManager(tmpDir, clk, 10*time.Minute)
	if err != nil {
		t.Fatalf("NewCacheManager: %v", err)
	}

	fingerprint := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	data := sampleData{Name: "data", Count: 42}

	if err := Write(ctx, cm, protocol.CacheTargetMachineProfile, fingerprint, data, 10*time.Minute); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Advance clock past expiration
	laterClk := clock.NewFake(now.Add(15*time.Minute), 0)
	laterCM, _ := NewCacheManager(tmpDir, laterClk, 10*time.Minute)

	// ReadEntry must return found=true and expired=true, without removing the file
	readData, found, expired, err := ReadEntry[sampleData](ctx, laterCM, protocol.CacheTargetMachineProfile, fingerprint)
	if err != nil {
		t.Fatalf("ReadEntry: %v", err)
	}
	if !found {
		t.Fatalf("expected ReadEntry to find expired entry")
	}
	if !expired {
		t.Fatalf("expected ReadEntry to report expired=true")
	}
	if readData.Count != 42 {
		t.Fatalf("expected data count 42, got %d", readData.Count)
	}

	// Calling ReadEntry a second time must still find the file (not deleted)
	_, found2, expired2, err2 := ReadEntry[sampleData](ctx, laterCM, protocol.CacheTargetMachineProfile, fingerprint)
	if err2 != nil || !found2 || !expired2 {
		t.Fatalf("expected file to be retained on disk, but found=%v, expired=%v, err=%v", found2, expired2, err2)
	}
}

func TestCacheManagerWriteDefaultTTLExpired(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	clk := clock.NewFake(now, 0)

	cm, err := NewCacheManager(tmpDir, clk, 1*time.Hour)
	if err != nil {
		t.Fatalf("NewCacheManager: %v", err)
	}

	fingerprint := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	data := sampleData{Name: "default_ttl", Count: 100}

	// Call Write with ttl <= 0 (0) so defaultTTL (1 hour) is used
	if err := Write(ctx, cm, protocol.CacheTargetMachineProfile, fingerprint, data, 0); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Advance clock beyond 1 hour (75 minutes)
	laterClk := clock.NewFake(now.Add(75*time.Minute), 0)
	laterCM, err := NewCacheManager(tmpDir, laterClk, 1*time.Hour)
	if err != nil {
		t.Fatalf("NewCacheManager: %v", err)
	}

	// Read should return found=false because it filters expired entries
	_, found, err := Read[sampleData](ctx, laterCM, protocol.CacheTargetMachineProfile, fingerprint)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if found {
		t.Fatalf("expected entry written with ttl=0 to have expired after 75 minutes with 1h default TTL")
	}

	// ReadEntry should return found=true and expired=true
	_, foundEntry, expired, err := ReadEntry[sampleData](ctx, laterCM, protocol.CacheTargetMachineProfile, fingerprint)
	if err != nil {
		t.Fatalf("ReadEntry: %v", err)
	}
	if !foundEntry || !expired {
		t.Fatalf("expected found=true and expired=true for expired entry, got found=%v, expired=%v", foundEntry, expired)
	}
}

// TestCacheManager_WriteProfileAndReadByIDRegression proves the Item 1 regression requirement:
// create two different profiles for the same machine fingerprint and prove an older inventory
// still resolves to the exact profile observation that created it after the newer profile has been written.
func TestCacheManager_WriteProfileAndReadByIDRegression(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	now := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	clk := clock.NewFake(now, 0)

	cm, err := NewCacheManager(tmpDir, clk, 24*time.Hour)
	if err != nil {
		t.Fatalf("NewCacheManager: %v", err)
	}

	fp := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	facts := protocol.EnvironmentFacts{
		Host:           protocol.HostFacts{Family: protocol.OSLinux, Arch: "amd64"},
		Virtualization: protocol.VirtualizationFacts{Container: protocol.ContainerNone},
	}

	profile1 := protocol.MachineCapabilityProfile{
		SchemaVersion:         protocol.SchemaVersion1,
		ProfileID:             "mcp-obs-1",
		MachineFingerprint:    fp,
		ObservedAt:            protocol.NewTimestamp(now),
		KnowledgeRevision:     "2026-09-22",
		ProbeDepth:            protocol.DepthHealth,
		Assessment:            protocol.AssessmentReady,
		Environment:           facts,
		AcceleratorCandidates: []protocol.AcceleratorCandidate{},
		Endpoints: []protocol.CognitionEndpoint{
			{
				ID:                     "ollama-local",
				Kind:                   protocol.EndpointLocalRuntime,
				Locality:               protocol.LocalityLocal,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthNotApplicable,
				CostClass:              protocol.CostLocalCompute,
				RequiredSourceExposure: protocol.ExposureLocalOnly,
				ObservedAt:             protocol.NewTimestamp(now),
				StructuredOutput:       protocol.FeatureDeclared,
				ToolUse:                protocol.FeatureDeclared,
			},
		},
	}

	if err := WriteProfile(ctx, cm, profile1, 24*time.Hour); err != nil {
		t.Fatalf("WriteProfile 1: %v", err)
	}

	// Inventory 1 references profile1
	inv1 := protocol.ResourceInventory{
		SchemaVersion:      protocol.SchemaVersion1,
		InventoryID:        "inv-001",
		MachineFingerprint: fp,
		ObservedAt:         protocol.NewTimestamp(now),
		Hardware: protocol.HardwareSummary{
			OSFamily:     protocol.OSLinux,
			Arch:         "amd64",
			LogicalCores: 4,
		},
		Profile: &protocol.MachineProfileRef{
			ProfileID:          profile1.ProfileID,
			MachineFingerprint: fp,
			ObservedAt:         profile1.ObservedAt,
			ProbeDepth:         profile1.ProbeDepth,
		},
		CognitionEndpoints: []protocol.CognitionEndpointSummary{
			{
				ID:                     "ollama-local",
				Kind:                   protocol.EndpointLocalRuntime,
				Locality:               protocol.LocalityLocal,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthNotApplicable,
				CostClass:              protocol.CostLocalCompute,
				RequiredSourceExposure: protocol.ExposureLocalOnly,
			},
		},
	}
	if err := inv1.Validate(); err != nil {
		t.Fatalf("inv1.Validate: %v", err)
	}

	// Advance clock slightly and create profile2 for the SAME fingerprint with deeper probe
	laterNow := now.Add(15 * time.Minute)
	clkLater := clock.NewFake(laterNow, 0)
	cmLater, _ := NewCacheManager(tmpDir, clkLater, 24*time.Hour)

	profile2 := protocol.MachineCapabilityProfile{
		SchemaVersion:         protocol.SchemaVersion1,
		ProfileID:             "mcp-obs-2",
		MachineFingerprint:    fp,
		ObservedAt:            protocol.NewTimestamp(laterNow),
		KnowledgeRevision:     "2026-09-22",
		ProbeDepth:            protocol.DepthInference,
		Assessment:            protocol.AssessmentReady,
		Environment:           facts,
		AcceleratorCandidates: []protocol.AcceleratorCandidate{},
		Endpoints: []protocol.CognitionEndpoint{
			{
				ID:                     "ollama-local",
				Kind:                   protocol.EndpointLocalRuntime,
				Locality:               protocol.LocalityLocal,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthNotApplicable,
				CostClass:              protocol.CostLocalCompute,
				RequiredSourceExposure: protocol.ExposureLocalOnly,
				ObservedAt:             protocol.NewTimestamp(laterNow),
				StructuredOutput:       protocol.FeatureProbePassed,
				ToolUse:                protocol.FeatureProbePassed,
			},
		},
	}

	// Writing profile2 updates the latest target machine-profile.json
	if err := WriteProfile(ctx, cmLater, profile2, 24*time.Hour); err != nil {
		t.Fatalf("WriteProfile 2: %v", err)
	}

	// Inventory 2 references profile2
	inv2 := protocol.ResourceInventory{
		SchemaVersion:      protocol.SchemaVersion1,
		InventoryID:        "inv-002",
		MachineFingerprint: fp,
		ObservedAt:         protocol.NewTimestamp(laterNow),
		Hardware: protocol.HardwareSummary{
			OSFamily:     protocol.OSLinux,
			Arch:         "amd64",
			LogicalCores: 4,
		},
		Profile: &protocol.MachineProfileRef{
			ProfileID:          profile2.ProfileID,
			MachineFingerprint: fp,
			ObservedAt:         profile2.ObservedAt,
			ProbeDepth:         profile2.ProbeDepth,
		},
		CognitionEndpoints: []protocol.CognitionEndpointSummary{
			{
				ID:                     "ollama-local",
				Kind:                   protocol.EndpointLocalRuntime,
				Locality:               protocol.LocalityLocal,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthNotApplicable,
				CostClass:              protocol.CostLocalCompute,
				RequiredSourceExposure: protocol.ExposureLocalOnly,
			},
		},
	}
	if err := inv2.Validate(); err != nil {
		t.Fatalf("inv2.Validate: %v", err)
	}

	// 1. Verify older inventory 1 resolves to exact profile 1 via its ProfileRef
	resolvedProfile1, found1, err := ReadProfileByID(ctx, cmLater, inv1.Profile.ProfileID)
	if err != nil {
		t.Fatalf("ReadProfileByID inv1: %v", err)
	}
	if !found1 {
		t.Fatalf("expected profile1 to be found by ProfileID %q", inv1.Profile.ProfileID)
	}
	if resolvedProfile1.ProfileID != "mcp-obs-1" {
		t.Errorf("resolved profile1 ProfileID = %q, want mcp-obs-1", resolvedProfile1.ProfileID)
	}
	if resolvedProfile1.ProbeDepth != protocol.DepthHealth {
		t.Errorf("resolved profile1 ProbeDepth = %q, want health", resolvedProfile1.ProbeDepth)
	}
	if resolvedProfile1.Endpoints[0].StructuredOutput != protocol.FeatureDeclared {
		t.Errorf("resolved profile1 StructuredOutput = %q, want declared", resolvedProfile1.Endpoints[0].StructuredOutput)
	}

	// 2. Verify newer inventory 2 resolves to exact profile 2 via its ProfileRef
	resolvedProfile2, found2, err := ReadProfileByID(ctx, cmLater, inv2.Profile.ProfileID)
	if err != nil {
		t.Fatalf("ReadProfileByID inv2: %v", err)
	}
	if !found2 {
		t.Fatalf("expected profile2 to be found by ProfileID %q", inv2.Profile.ProfileID)
	}
	if resolvedProfile2.ProfileID != "mcp-obs-2" {
		t.Errorf("resolved profile2 ProfileID = %q, want mcp-obs-2", resolvedProfile2.ProfileID)
	}
	if resolvedProfile2.ProbeDepth != protocol.DepthInference {
		t.Errorf("resolved profile2 ProbeDepth = %q, want inference", resolvedProfile2.ProbeDepth)
	}
	if resolvedProfile2.Endpoints[0].StructuredOutput != protocol.FeatureProbePassed {
		t.Errorf("resolved profile2 StructuredOutput = %q, want probe_passed", resolvedProfile2.Endpoints[0].StructuredOutput)
	}

	// 3. Verify latest target cache resolves to profile 2 (the latest one)
	latestProfile, foundLatest, err := Read[protocol.MachineCapabilityProfile](ctx, cmLater, protocol.CacheTargetMachineProfile, fp)
	if err != nil || !foundLatest {
		t.Fatalf("expected latest profile to be readable: found=%v, err=%v", foundLatest, err)
	}
	if latestProfile.ProfileID != "mcp-obs-2" {
		t.Errorf("latest cached profile ID = %q, want mcp-obs-2", latestProfile.ProfileID)
	}
}
