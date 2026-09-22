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
