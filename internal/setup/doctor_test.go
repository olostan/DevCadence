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

	// Create subdirectories
	for _, sub := range []string{"state", "artifacts_setup", "tmp"} {
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
}
