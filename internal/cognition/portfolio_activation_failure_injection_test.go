package cognition_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition"
)

func TestActivationManager_PostRenameFailureRollback_SynchronouslyRestoresPreviousState(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFake(time.Date(2026, 10, 2, 22, 0, 0, 0, time.UTC), time.Second)
	validator := cognition.NewPortfolioValidator()

	mp := makeTestMachineProfile()
	inv := makeTestInventory()
	cp := makeTestContextProfiles()

	tmpDir, err := os.MkdirTemp("", "devcadence-post-rename-rollback-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr, err := cognition.NewActivationManager(tmpDir, validator, clk)
	if err != nil {
		t.Fatalf("NewActivationManager failed: %v", err)
	}

	// 1. Activate port-1
	p1 := makeTestPortfolio()
	p1.PortfolioID = "port-1"
	p1.Revision = 1
	rec1, err := mgr.Activate(ctx, cognition.ValidationInput{
		Portfolio:       p1,
		MachineProfile:  mp,
		Inventory:       inv,
		ContextProfiles: cp,
		Clock:           clk,
	})
	if err != nil {
		t.Fatalf("activate p1 failed: %v", err)
	}

	// 2. Prepare candidate port-2
	p2 := makeTestPortfolio()
	p2.PortfolioID = "port-2"
	p2.Revision = 2

	// 3. Inject post-rename failure specifically on the first attempt to write LineageFileName
	var failedOnce bool
	mgr.SetPostRenameHookForTesting(func(targetPath string) error {
		if strings.HasSuffix(targetPath, cognition.LineageFileName) && !failedOnce {
			failedOnce = true
			return errors.New("simulated failure after lineage rename")
		}
		return nil
	})

	_, err = mgr.Activate(ctx, cognition.ValidationInput{
		Portfolio:       p2,
		MachineProfile:  mp,
		Inventory:       inv,
		ContextProfiles: cp,
		Clock:           clk,
	})
	if err == nil {
		t.Fatalf("expected Activate to fail on injected post-rename failure")
	}
	if !failedOnce {
		t.Fatalf("expected post-rename hook to be triggered")
	}

	// 4. Verify synchronous rollback restored port-1 and its lineage
	active, _, err := mgr.GetActivePortfolio(ctx)
	if err != nil {
		t.Fatalf("GetActivePortfolio failed: %v", err)
	}
	if active.PortfolioID != "port-1" {
		t.Errorf("expected active portfolio to be 'port-1', got %q", active.PortfolioID)
	}

	lineage, err := mgr.GetLineage(ctx)
	if err != nil {
		t.Fatalf("GetLineage failed: %v", err)
	}
	if lineage.CurrentPortfolioID != "port-1" || lineage.CurrentSequence != 1 {
		t.Errorf("expected lineage current portfolio 'port-1' at sequence 1, got %q seq %d",
			lineage.CurrentPortfolioID, lineage.CurrentSequence)
	}
	if len(lineage.History) != 1 || lineage.History[0].ActivationID != rec1.ActivationID {
		t.Errorf("expected lineage history to hold only rec1, got %+v", lineage.History)
	}

	// 5. Verify pending journal was cleanly removed and seq 2 history does not exist
	pendingPath := filepath.Join(tmpDir, cognition.PendingActivationFileName)
	if _, statErr := os.Stat(pendingPath); !os.IsNotExist(statErr) {
		t.Errorf("expected pending journal to be removed after successful rollback, got statErr: %v", statErr)
	}

	seq2Hist := filepath.Join(tmpDir, cognition.HistoryDirName, "activation-000002-*.json")
	matches, _ := filepath.Glob(seq2Hist)
	if len(matches) > 0 {
		t.Errorf("expected sequence 2 history file to be cleaned up, found: %v", matches)
	}
}

func TestActivationManager_PostRenameFailureWithRollbackFailure_PreservesPendingJournalAndRecovers(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFake(time.Date(2026, 10, 2, 22, 0, 0, 0, time.UTC), time.Second)
	validator := cognition.NewPortfolioValidator()

	mp := makeTestMachineProfile()
	inv := makeTestInventory()
	cp := makeTestContextProfiles()

	tmpDir, err := os.MkdirTemp("", "devcadence-post-rename-preserve-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr, err := cognition.NewActivationManager(tmpDir, validator, clk)
	if err != nil {
		t.Fatalf("NewActivationManager failed: %v", err)
	}

	// 1. Activate port-1
	p1 := makeTestPortfolio()
	p1.PortfolioID = "port-1"
	p1.Revision = 1
	_, err = mgr.Activate(ctx, cognition.ValidationInput{
		Portfolio:       p1,
		MachineProfile:  mp,
		Inventory:       inv,
		ContextProfiles: cp,
		Clock:           clk,
	})
	if err != nil {
		t.Fatalf("activate p1 failed: %v", err)
	}

	// 2. Candidate port-2
	p2 := makeTestPortfolio()
	p2.PortfolioID = "port-2"
	p2.Revision = 2

	// 3. Inject persistent failure on lineage write so that both activation and rollback fail
	mgr.SetPostRenameHookForTesting(func(targetPath string) error {
		if strings.HasSuffix(targetPath, cognition.LineageFileName) {
			return errors.New("simulated persistent failure on lineage write")
		}
		return nil
	})

	_, err = mgr.Activate(ctx, cognition.ValidationInput{
		Portfolio:       p2,
		MachineProfile:  mp,
		Inventory:       inv,
		ContextProfiles: cp,
		Clock:           clk,
	})
	if err == nil {
		t.Fatalf("expected Activate to fail")
	}

	// 4. Because rollback also failed, pending journal MUST be preserved on disk
	pendingPath := filepath.Join(tmpDir, cognition.PendingActivationFileName)
	pendingBytes, statErr := os.ReadFile(pendingPath)
	if statErr != nil {
		t.Fatalf("expected pending journal to be preserved on disk when rollback fails, got: %v", statErr)
	}
	var pending cognition.PendingActivation
	if err := json.Unmarshal(pendingBytes, &pending); err != nil {
		t.Fatalf("corrupted pending activation: %v", err)
	}
	if pending.Record.PortfolioID != "port-2" {
		t.Errorf("expected pending journal for 'port-2', got %q", pending.Record.PortfolioID)
	}

	// 5. Simulate process restart: create a new ActivationManager without test hooks.
	// Startup recovery must roll forward the pending activation cleanly.
	recoveredMgr, err := cognition.NewActivationManager(tmpDir, validator, clk)
	if err != nil {
		t.Fatalf("startup recovery failed: %v", err)
	}

	// 6. Verify pending journal was removed and port-2 is now active
	if _, statErr := os.Stat(pendingPath); !os.IsNotExist(statErr) {
		t.Errorf("expected pending journal to be removed after startup recovery, got statErr: %v", statErr)
	}

	active, _, err := recoveredMgr.GetActivePortfolio(ctx)
	if err != nil {
		t.Fatalf("GetActivePortfolio failed: %v", err)
	}
	if active.PortfolioID != "port-2" {
		t.Errorf("expected active portfolio to be 'port-2', got %q", active.PortfolioID)
	}

	lineage, err := recoveredMgr.GetLineage(ctx)
	if err != nil {
		t.Fatalf("GetLineage failed: %v", err)
	}
	if lineage.CurrentPortfolioID != "port-2" || lineage.CurrentSequence != 2 {
		t.Errorf("expected lineage current portfolio 'port-2' at sequence 2, got %q seq %d",
			lineage.CurrentPortfolioID, lineage.CurrentSequence)
	}
	if len(lineage.History) != 2 {
		t.Errorf("expected 2 history entries, got %d", len(lineage.History))
	}
}

func TestActivationManager_SyncDirFailureRollback(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFake(time.Date(2026, 10, 2, 22, 0, 0, 0, time.UTC), time.Second)
	validator := cognition.NewPortfolioValidator()

	mp := makeTestMachineProfile()
	inv := makeTestInventory()
	cp := makeTestContextProfiles()

	tmpDir, err := os.MkdirTemp("", "devcadence-syncdir-rollback-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr, err := cognition.NewActivationManager(tmpDir, validator, clk)
	if err != nil {
		t.Fatalf("NewActivationManager failed: %v", err)
	}

	// 1. Activate port-1
	p1 := makeTestPortfolio()
	p1.PortfolioID = "port-1"
	p1.Revision = 1
	_, err = mgr.Activate(ctx, cognition.ValidationInput{
		Portfolio:       p1,
		MachineProfile:  mp,
		Inventory:       inv,
		ContextProfiles: cp,
		Clock:           clk,
	})
	if err != nil {
		t.Fatalf("activate p1 failed: %v", err)
	}

	// 2. Candidate port-2 with syncDirHook failing once
	p2 := makeTestPortfolio()
	p2.PortfolioID = "port-2"
	p2.Revision = 2

	var syncFailedOnce bool
	mgr.SetSyncDirHookForTesting(func(dirPath string) error {
		if !syncFailedOnce {
			syncFailedOnce = true
			return errors.New("simulated fsync failure on directory")
		}
		return nil
	})

	_, err = mgr.Activate(ctx, cognition.ValidationInput{
		Portfolio:       p2,
		MachineProfile:  mp,
		Inventory:       inv,
		ContextProfiles: cp,
		Clock:           clk,
	})
	if err == nil {
		t.Fatalf("expected Activate to fail on simulated fsync failure")
	}
	if !syncFailedOnce {
		t.Fatalf("expected syncDir hook to have been triggered")
	}

	// 3. Verify clean rollback to port-1
	active, _, err := mgr.GetActivePortfolio(ctx)
	if err != nil {
		t.Fatalf("GetActivePortfolio failed: %v", err)
	}
	if active.PortfolioID != "port-1" {
		t.Errorf("expected active portfolio to remain 'port-1', got %q", active.PortfolioID)
	}

	lineage, err := mgr.GetLineage(ctx)
	if err != nil {
		t.Fatalf("GetLineage failed: %v", err)
	}
	if lineage.CurrentPortfolioID != "port-1" || lineage.CurrentSequence != 1 {
		t.Errorf("expected lineage current portfolio 'port-1' at sequence 1, got %q seq %d",
			lineage.CurrentPortfolioID, lineage.CurrentSequence)
	}
}
