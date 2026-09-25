package setup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/errs"
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

// TestWriteProfile_SameIDIdempotent_DifferentContentConflict is the
// independent-review follow-up on WP-M3B-5 finding 1c: ProfileID must be a
// stable content identity, so a second write for the same ProfileID must be
// a no-op when the content is identical and must fail closed (not silently
// overwrite) when it differs.
func TestWriteProfile_SameIDIdempotent_DifferentContentConflict(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	now := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	clk := clock.NewFake(now, 0)
	cm, err := NewCacheManager(tmpDir, clk, 24*time.Hour)
	if err != nil {
		t.Fatalf("NewCacheManager: %v", err)
	}

	facts := protocol.EnvironmentFacts{
		Host:           protocol.HostFacts{Family: protocol.OSLinux, Arch: "amd64"},
		Virtualization: protocol.VirtualizationFacts{Container: protocol.ContainerNone},
	}
	base := protocol.MachineCapabilityProfile{
		SchemaVersion:         protocol.SchemaVersion1,
		ProfileID:             "mcp-fixed-id",
		MachineFingerprint:    "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ObservedAt:            protocol.NewTimestamp(now),
		KnowledgeRevision:     "2026-09-22",
		ProbeDepth:            protocol.DepthHealth,
		Assessment:            protocol.AssessmentReady,
		Environment:           facts,
		AcceleratorCandidates: []protocol.AcceleratorCandidate{},
	}

	if err := WriteProfile(ctx, cm, base, 24*time.Hour); err != nil {
		t.Fatalf("first WriteProfile: %v", err)
	}

	// Identical content, same ProfileID: must be a silent idempotent no-op.
	if err := WriteProfile(ctx, cm, base, 24*time.Hour); err != nil {
		t.Fatalf("idempotent re-write of identical content should succeed, got: %v", err)
	}

	// Different content, same ProfileID: must fail closed with a conflict.
	mutated := base
	mutated.Assessment = protocol.AssessmentPartiallyReady
	err = WriteProfile(ctx, cm, mutated, 24*time.Hour)
	if err == nil {
		t.Fatalf("expected conflict error writing different content under the same ProfileID, got nil")
	}
	if errs.CategoryOf(err) != errs.CategoryConflict {
		t.Errorf("expected CategoryConflict, got: %v", err)
	}

	// The originally archived content must be unchanged.
	resolved, found, err := ReadProfileByID(ctx, cm, base.ProfileID)
	if err != nil || !found {
		t.Fatalf("expected original profile still readable: found=%v, err=%v", found, err)
	}
	if resolved.Assessment != protocol.AssessmentReady {
		t.Errorf("archived profile was overwritten: Assessment = %q, want %q (unchanged)", resolved.Assessment, protocol.AssessmentReady)
	}
}

// TestReadProfileByID_RejectsContentIDMismatch is the independent-review
// follow-up on WP-M3B-5 finding 1c's resolver-validation ask: a resolver
// must not merely trust the filename it looked up by, but must cross-check
// the loaded content's own ProfileID.
func TestReadProfileByID_RejectsContentIDMismatch(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	now := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	clk := clock.NewFake(now, 0)
	cm, err := NewCacheManager(tmpDir, clk, 24*time.Hour)
	if err != nil {
		t.Fatalf("NewCacheManager: %v", err)
	}

	facts := protocol.EnvironmentFacts{
		Host:           protocol.HostFacts{Family: protocol.OSLinux, Arch: "amd64"},
		Virtualization: protocol.VirtualizationFacts{Container: protocol.ContainerNone},
	}
	profile := protocol.MachineCapabilityProfile{
		SchemaVersion:         protocol.SchemaVersion1,
		ProfileID:             "mcp-real-id",
		MachineFingerprint:    "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ObservedAt:            protocol.NewTimestamp(now),
		KnowledgeRevision:     "2026-09-22",
		ProbeDepth:            protocol.DepthHealth,
		Assessment:            protocol.AssessmentReady,
		Environment:           facts,
		AcceleratorCandidates: []protocol.AcceleratorCandidate{},
	}
	if err := WriteProfile(ctx, cm, profile, 24*time.Hour); err != nil {
		t.Fatalf("WriteProfile: %v", err)
	}

	// Corrupt the on-disk mapping: rename the archived file to a different
	// ProfileID's path, simulating a mismatch between the lookup key and the
	// stored content (e.g. a bad rename or a manually placed file).
	root := filepath.Join(tmpDir, "profiles")
	if err := os.Rename(filepath.Join(root, "mcp-real-id.json"), filepath.Join(root, "mcp-spoofed-id.json")); err != nil {
		t.Fatalf("simulate ID mismatch: %v", err)
	}

	_, found, err := ReadProfileByID(ctx, cm, "mcp-spoofed-id")
	if err != nil {
		t.Fatalf("ReadProfileByID: %v", err)
	}
	if found {
		t.Errorf("expected ReadProfileByID to reject content whose own ProfileID (%q) does not match the requested ID (%q)", profile.ProfileID, "mcp-spoofed-id")
	}
}

// TestArchiveProfile_RefusesToOverwriteUnprovableExistingEntry is the
// independent-review follow-up on WP-M3B-5, round-4 finding 3: the
// "immutable by ProfileID" archive must fail closed rather than overwrite
// whenever it cannot prove the existing entry equals the new write —
// malformed JSON, an invalid envelope/profile, or a decoded ProfileID that
// doesn't match the path are all "cannot prove identical," never license
// to replace.
func TestArchiveProfile_RefusesToOverwriteUnprovableExistingEntry(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	clk := clock.NewFake(now, 0)

	facts := protocol.EnvironmentFacts{
		Host:           protocol.HostFacts{Family: protocol.OSLinux, Arch: "amd64"},
		Virtualization: protocol.VirtualizationFacts{Container: protocol.ContainerNone},
	}
	newProfile := protocol.MachineCapabilityProfile{
		SchemaVersion:         protocol.SchemaVersion1,
		ProfileID:             "mcp-target-id",
		MachineFingerprint:    "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ObservedAt:            protocol.NewTimestamp(now),
		KnowledgeRevision:     "2026-09-22",
		ProbeDepth:            protocol.DepthHealth,
		Assessment:            protocol.AssessmentReady,
		Environment:           facts,
		AcceleratorCandidates: []protocol.AcceleratorCandidate{},
	}

	cases := []struct {
		name          string
		writeExisting func(t *testing.T, profilesDir string)
	}{
		{"malformed JSON", func(t *testing.T, profilesDir string) {
			t.Helper()
			path := filepath.Join(profilesDir, "mcp-target-id.json")
			if err := os.WriteFile(path, []byte("{not valid json"), 0600); err != nil {
				t.Fatalf("write malformed file: %v", err)
			}
		}},
		{"valid envelope, invalid profile content", func(t *testing.T, profilesDir string) {
			t.Helper()
			env := CacheEnvelope[protocol.MachineCapabilityProfile]{
				SchemaVersion:      protocol.SchemaVersion1,
				CreatedAt:          protocol.NewTimestamp(now),
				ExpiresAt:          protocol.NewTimestamp(now.Add(time.Hour)),
				MachineFingerprint: newProfile.MachineFingerprint,
				// Missing required fields (e.g. ProfileID, KnowledgeRevision)
				// makes this fail MachineCapabilityProfile.Validate.
				Data: protocol.MachineCapabilityProfile{SchemaVersion: protocol.SchemaVersion1},
			}
			bytes, err := json.Marshal(env)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			path := filepath.Join(profilesDir, "mcp-target-id.json")
			if err := os.WriteFile(path, bytes, 0600); err != nil {
				t.Fatalf("write invalid-content file: %v", err)
			}
		}},
		{"valid profile, mismatched ProfileID", func(t *testing.T, profilesDir string) {
			t.Helper()
			other := newProfile
			other.ProfileID = "mcp-different-id"
			env := CacheEnvelope[protocol.MachineCapabilityProfile]{
				SchemaVersion:      protocol.SchemaVersion1,
				CreatedAt:          protocol.NewTimestamp(now),
				ExpiresAt:          protocol.NewTimestamp(now.Add(time.Hour)),
				MachineFingerprint: other.MachineFingerprint,
				Data:               other,
			}
			bytes, err := json.Marshal(env)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			// Filed under mcp-target-id.json even though its own decoded
			// ProfileID says mcp-different-id.
			path := filepath.Join(profilesDir, "mcp-target-id.json")
			if err := os.WriteFile(path, bytes, 0600); err != nil {
				t.Fatalf("write mismatched-id file: %v", err)
			}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			cm, err := NewCacheManager(tmpDir, clk, 24*time.Hour)
			if err != nil {
				t.Fatalf("NewCacheManager: %v", err)
			}
			profilesDir := filepath.Join(tmpDir, "profiles")
			if err := os.MkdirAll(profilesDir, 0700); err != nil {
				t.Fatalf("mkdir profiles: %v", err)
			}
			tc.writeExisting(t, profilesDir)

			before, readErr := os.ReadFile(filepath.Join(profilesDir, "mcp-target-id.json"))
			if readErr != nil {
				t.Fatalf("read existing fixture: %v", readErr)
			}

			err = WriteProfile(ctx, cm, newProfile, 24*time.Hour)
			if err == nil {
				t.Fatalf("expected archiveProfile to refuse to overwrite an unprovable existing entry")
			}
			if errs.CategoryOf(err) != errs.CategoryConflict {
				t.Errorf("expected CategoryConflict, got: %v", err)
			}

			after, readErr := os.ReadFile(filepath.Join(profilesDir, "mcp-target-id.json"))
			if readErr != nil {
				t.Fatalf("read fixture after refused write: %v", readErr)
			}
			if string(before) != string(after) {
				t.Errorf("existing archive bytes changed despite the write being refused")
			}
		})
	}
}

// TestReadProfileByRef_CrossChecksFullReference is the independent-review
// follow-up on WP-M3B-5, round-4 finding 3's ask that a resolver not
// silently ignore most of MachineProfileRef: it must cross-check
// MachineFingerprint, ObservedAt, and ProbeDepth, not only ProfileID.
func TestReadProfileByRef_CrossChecksFullReference(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	now := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	clk := clock.NewFake(now, 0)
	cm, err := NewCacheManager(tmpDir, clk, 24*time.Hour)
	if err != nil {
		t.Fatalf("NewCacheManager: %v", err)
	}

	facts := protocol.EnvironmentFacts{
		Host:           protocol.HostFacts{Family: protocol.OSLinux, Arch: "amd64"},
		Virtualization: protocol.VirtualizationFacts{Container: protocol.ContainerNone},
	}
	profile := protocol.MachineCapabilityProfile{
		SchemaVersion:         protocol.SchemaVersion1,
		ProfileID:             "mcp-ref-001",
		MachineFingerprint:    "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ObservedAt:            protocol.NewTimestamp(now),
		KnowledgeRevision:     "2026-09-22",
		ProbeDepth:            protocol.DepthHealth,
		Assessment:            protocol.AssessmentReady,
		Environment:           facts,
		AcceleratorCandidates: []protocol.AcceleratorCandidate{},
	}
	if err := WriteProfile(ctx, cm, profile, 24*time.Hour); err != nil {
		t.Fatalf("WriteProfile: %v", err)
	}

	validRef := protocol.MachineProfileRef{
		ProfileID:          profile.ProfileID,
		MachineFingerprint: profile.MachineFingerprint,
		ObservedAt:         profile.ObservedAt,
		ProbeDepth:         profile.ProbeDepth,
	}
	got, found, err := ReadProfileByRef(ctx, cm, validRef)
	if err != nil || !found {
		t.Fatalf("expected valid ref to resolve: found=%v, err=%v", found, err)
	}
	if got.ProfileID != profile.ProfileID {
		t.Errorf("resolved ProfileID = %q, want %q", got.ProfileID, profile.ProfileID)
	}

	t.Run("fingerprint mismatch", func(t *testing.T) {
		ref := validRef
		ref.MachineFingerprint = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
		_, found, err := ReadProfileByRef(ctx, cm, ref)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if found {
			t.Errorf("expected mismatched MachineFingerprint to fail resolution")
		}
	})

	t.Run("observed_at mismatch", func(t *testing.T) {
		ref := validRef
		ref.ObservedAt = protocol.NewTimestamp(now.Add(time.Hour))
		_, found, err := ReadProfileByRef(ctx, cm, ref)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if found {
			t.Errorf("expected mismatched ObservedAt to fail resolution")
		}
	})

	t.Run("probe_depth mismatch", func(t *testing.T) {
		ref := validRef
		ref.ProbeDepth = protocol.DepthInference
		_, found, err := ReadProfileByRef(ctx, cm, ref)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if found {
			t.Errorf("expected mismatched ProbeDepth to fail resolution")
		}
	})
}
