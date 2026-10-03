package cognition_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/errs"
)

func TestActivationManager(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFake(time.Date(2026, 10, 2, 21, 0, 0, 0, time.UTC), time.Second)

	t.Run("valid portfolio activates atomically and writes lineage", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "devcadence-activation-test-*")
		if err != nil {
			t.Fatalf("MkdirTemp failed: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		mgr, err := cognition.NewActivationManager(tmpDir, nil, clk)
		if err != nil {
			t.Fatalf("NewActivationManager failed: %v", err)
		}

		p := makeTestPortfolio()
		mp := makeTestMachineProfile()
		inv := makeTestInventory()
		cp := makeTestContextProfiles()
		policy := cognition.DefaultValidationPolicy()

		input := cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Policy:          &policy,
			Clock:           clk,
		}

		rec, err := mgr.Activate(ctx, input)
		if err != nil {
			t.Fatalf("Activate failed: %v", err)
		}

		if rec.Sequence != 1 {
			t.Errorf("expected sequence 1, got %d", rec.Sequence)
		}
		if rec.PortfolioID != p.PortfolioID {
			t.Errorf("expected portfolio_id %q, got %q", p.PortfolioID, rec.PortfolioID)
		}
		if rec.PreviousActivationID != "" {
			t.Errorf("expected empty previous activation on initial activation, got %q", rec.PreviousActivationID)
		}

		// Verify active-portfolio.json exists and is valid CognitionPortfolio
		activePath := filepath.Join(tmpDir, cognition.ActivePortfolioFileName)
		if _, err := os.Stat(activePath); os.IsNotExist(err) {
			t.Fatalf("active-portfolio.json was not created")
		}

		activePort, currentRec, err := mgr.GetActivePortfolio(ctx)
		if err != nil {
			t.Fatalf("GetActivePortfolio failed: %v", err)
		}
		if activePort.PortfolioID != p.PortfolioID {
			t.Errorf("expected active portfolio ID %q, got %q", p.PortfolioID, activePort.PortfolioID)
		}
		if currentRec == nil || currentRec.ActivationID != rec.ActivationID {
			t.Errorf("expected current activation record %v, got %v", rec.ActivationID, currentRec)
		}

		// Verify history
		hist, err := mgr.GetActivationHistory(ctx)
		if err != nil {
			t.Fatalf("GetActivationHistory failed: %v", err)
		}
		if len(hist) != 1 {
			t.Fatalf("expected 1 history entry, got %d", len(hist))
		}
		if hist[0].ActivationID != rec.ActivationID {
			t.Errorf("expected history activation %q, got %q", rec.ActivationID, hist[0].ActivationID)
		}
	})

	t.Run("invalid portfolio cannot activate and leaves no partial state", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "devcadence-activation-invalid-*")
		if err != nil {
			t.Fatalf("MkdirTemp failed: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		mgr, err := cognition.NewActivationManager(tmpDir, nil, clk)
		if err != nil {
			t.Fatalf("NewActivationManager failed: %v", err)
		}

		p := makeTestPortfolio()
		// Corrupt portfolio: endpoint nonexistent
		p.RoleBindings[0].EndpointID = "ep-nonexistent"

		input := cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  makeTestMachineProfile(),
			Inventory:       makeTestInventory(),
			ContextProfiles: makeTestContextProfiles(),
			Clock:           clk,
		}

		rec, err := mgr.Activate(ctx, input)
		if err == nil {
			t.Fatalf("expected error activating invalid portfolio, got rec: %v", rec)
		}

		// Verify active-portfolio.json was NOT created
		activePath := filepath.Join(tmpDir, cognition.ActivePortfolioFileName)
		if _, err := os.Stat(activePath); !os.IsNotExist(err) {
			t.Fatalf("active-portfolio.json should not exist after failed activation")
		}

		activePort, _, err := mgr.GetActivePortfolio(ctx)
		if errs.CategoryOf(err) != errs.CategoryNotFound {
			t.Errorf("expected CategoryNotFound, got %v (activePort: %v)", err, activePort)
		}
	})

	t.Run("sequential activations and rollback to previous", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "devcadence-activation-rollback-*")
		if err != nil {
			t.Fatalf("MkdirTemp failed: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		mgr, err := cognition.NewActivationManager(tmpDir, nil, clk)
		if err != nil {
			t.Fatalf("NewActivationManager failed: %v", err)
		}

		mp := makeTestMachineProfile()
		inv := makeTestInventory()
		cp := makeTestContextProfiles()
		policy := cognition.DefaultValidationPolicy()

		// 1. First activation: port-v1
		p1 := makeTestPortfolio()
		p1.PortfolioID = "port-v1"
		p1.Revision = 1
		rec1, err := mgr.Activate(ctx, cognition.ValidationInput{
			Portfolio:       p1,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Policy:          &policy,
			Clock:           clk,
		})
		if err != nil {
			t.Fatalf("initial activation failed: %v", err)
		}

		// 2. Second activation: port-v2
		p2 := makeTestPortfolio()
		p2.PortfolioID = "port-v2"
		p2.Revision = 2
		rec2, err := mgr.Activate(ctx, cognition.ValidationInput{
			Portfolio:       p2,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Policy:          &policy,
			Clock:           clk,
		})
		if err != nil {
			t.Fatalf("second activation failed: %v", err)
		}

		if rec2.Sequence != 2 {
			t.Errorf("expected sequence 2, got %d", rec2.Sequence)
		}
		if rec2.PreviousActivationID != rec1.ActivationID {
			t.Errorf("expected previous activation ID %q, got %q", rec1.ActivationID, rec2.PreviousActivationID)
		}
		if rec2.PreviousPortfolioID != p1.PortfolioID {
			t.Errorf("expected previous portfolio ID %q, got %q", p1.PortfolioID, rec2.PreviousPortfolioID)
		}

		// 3. Rollback to previous
		revalInput := cognition.ValidationInput{
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Policy:          &policy,
			Clock:           clk,
		}
		rollbackRec, err := mgr.RollbackToPrevious(ctx, &revalInput)
		if err != nil {
			t.Fatalf("RollbackToPrevious failed: %v", err)
		}

		if !rollbackRec.IsRollback {
			t.Errorf("expected IsRollback: true")
		}
		if rollbackRec.RollbackTargetActivationID != rec1.ActivationID {
			t.Errorf("expected RollbackTargetActivationID %q, got %q", rec1.ActivationID, rollbackRec.RollbackTargetActivationID)
		}
		if rollbackRec.PortfolioID != p1.PortfolioID {
			t.Errorf("expected restored portfolio ID %q, got %q", p1.PortfolioID, rollbackRec.PortfolioID)
		}
		if rollbackRec.Sequence != 3 {
			t.Errorf("expected sequence 3 for rollback, got %d", rollbackRec.Sequence)
		}

		// Active portfolio should now be port-v1
		activePort, _, err := mgr.GetActivePortfolio(ctx)
		if err != nil {
			t.Fatalf("GetActivePortfolio failed: %v", err)
		}
		if activePort.PortfolioID != p1.PortfolioID {
			t.Errorf("expected active portfolio ID %q after rollback, got %q", p1.PortfolioID, activePort.PortfolioID)
		}

		// History should now contain 3 records
		hist, err := mgr.GetActivationHistory(ctx)
		if err != nil {
			t.Fatalf("GetActivationHistory failed: %v", err)
		}
		if len(hist) != 3 {
			t.Fatalf("expected 3 history entries, got %d", len(hist))
		}
	})

	t.Run("rollback fails when no previous activation exists", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "devcadence-activation-norollback-*")
		if err != nil {
			t.Fatalf("MkdirTemp failed: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		mgr, err := cognition.NewActivationManager(tmpDir, nil, clk)
		if err != nil {
			t.Fatalf("NewActivationManager failed: %v", err)
		}

		// Activate only once
		p := makeTestPortfolio()
		_, err = mgr.Activate(ctx, cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  makeTestMachineProfile(),
			Inventory:       makeTestInventory(),
			ContextProfiles: makeTestContextProfiles(),
			Clock:           clk,
		})
		if err != nil {
			t.Fatalf("Activate failed: %v", err)
		}

		// Attempt rollback
		_, err = mgr.RollbackToPrevious(ctx, nil)
		if err == nil {
			t.Fatalf("expected error rolling back with only 1 activation")
		}
		if errs.CategoryOf(err) != errs.CategoryNotFound {
			t.Errorf("expected CategoryNotFound, got: %v", err)
		}
	})

	t.Run("rollback to specific historical activation", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "devcadence-activation-rollback-specific-*")
		if err != nil {
			t.Fatalf("MkdirTemp failed: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		mgr, err := cognition.NewActivationManager(tmpDir, nil, clk)
		if err != nil {
			t.Fatalf("NewActivationManager failed: %v", err)
		}

		mp := makeTestMachineProfile()
		inv := makeTestInventory()
		cp := makeTestContextProfiles()
		policy := cognition.DefaultValidationPolicy()

		p1 := makeTestPortfolio()
		p1.PortfolioID = "port-A"
		rec1, err := mgr.Activate(ctx, cognition.ValidationInput{
			Portfolio:       p1,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Policy:          &policy,
			Clock:           clk,
		})
		if err != nil {
			t.Fatalf("activate 1 failed: %v", err)
		}

		p2 := makeTestPortfolio()
		p2.PortfolioID = "port-B"
		_, err = mgr.Activate(ctx, cognition.ValidationInput{
			Portfolio:       p2,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Policy:          &policy,
			Clock:           clk,
		})
		if err != nil {
			t.Fatalf("activate 2 failed: %v", err)
		}

		p3 := makeTestPortfolio()
		p3.PortfolioID = "port-C"
		_, err = mgr.Activate(ctx, cognition.ValidationInput{
			Portfolio:       p3,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Policy:          &policy,
			Clock:           clk,
		})
		if err != nil {
			t.Fatalf("activate 3 failed: %v", err)
		}

		// Rollback specifically to port-A (rec1.ActivationID)
		revalInput := cognition.ValidationInput{
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Policy:          &policy,
			Clock:           clk,
		}
		recRollback, err := mgr.RollbackToActivation(ctx, rec1.ActivationID, &revalInput)
		if err != nil {
			t.Fatalf("RollbackToActivation failed: %v", err)
		}

		if recRollback.PortfolioID != "port-A" {
			t.Errorf("expected rolled back portfolio to be port-A, got %q", recRollback.PortfolioID)
		}
		if recRollback.RollbackTargetActivationID != rec1.ActivationID {
			t.Errorf("expected rollback target %q, got %q", rec1.ActivationID, recRollback.RollbackTargetActivationID)
		}

		activePort, _, err := mgr.GetActivePortfolio(ctx)
		if err != nil {
			t.Fatalf("GetActivePortfolio failed: %v", err)
		}
		if activePort.PortfolioID != "port-A" {
			t.Errorf("expected active portfolio port-A, got %q", activePort.PortfolioID)
		}
	})

	t.Run("reads target from disk history when lineage history is truncated and verifies Dir", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "devcadence-activation-disk-hist-*")
		if err != nil {
			t.Fatalf("MkdirTemp failed: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		mgr, err := cognition.NewActivationManager(tmpDir, nil, clk)
		if err != nil {
			t.Fatalf("NewActivationManager failed: %v", err)
		}

		if mgr.Dir() != tmpDir {
			t.Errorf("expected Dir() %q, got %q", tmpDir, mgr.Dir())
		}

		mp := makeTestMachineProfile()
		inv := makeTestInventory()
		cp := makeTestContextProfiles()

		p1 := makeTestPortfolio()
		p1.PortfolioID = "port-disk-1"
		rec1, err := mgr.Activate(ctx, cognition.ValidationInput{
			Portfolio:       p1,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Clock:           clk,
		})
		if err != nil {
			t.Fatalf("first activate failed: %v", err)
		}

		p2 := makeTestPortfolio()
		p2.PortfolioID = "port-disk-2"
		_, err = mgr.Activate(ctx, cognition.ValidationInput{
			Portfolio:       p2,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Clock:           clk,
		})
		if err != nil {
			t.Fatalf("second activate failed: %v", err)
		}

		// Now clear lineage.json's History slice on disk so target must be read from portfolio-history/
		lineagePath := filepath.Join(tmpDir, cognition.LineageFileName)
		data, err := os.ReadFile(lineagePath)
		if err != nil {
			t.Fatalf("ReadFile lineage failed: %v", err)
		}
		var lin cognition.PortfolioLineage
		if err := json.Unmarshal(data, &lin); err != nil {
			t.Fatalf("Unmarshal lineage failed: %v", err)
		}
		lin.History = nil
		newData, err := json.MarshalIndent(lin, "", "  ")
		if err != nil {
			t.Fatalf("Marshal lineage failed: %v", err)
		}
		if err := os.WriteFile(lineagePath, newData, 0o644); err != nil {
			t.Fatalf("WriteFile lineage failed: %v", err)
		}

		// Rollback to rec1.ActivationID; it should be loaded from disk history
		revalInput := cognition.ValidationInput{
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Clock:           clk,
		}
		recRollback, err := mgr.RollbackToActivation(ctx, rec1.ActivationID, &revalInput)
		if err != nil {
			t.Fatalf("RollbackToActivation from disk history failed: %v", err)
		}
		if recRollback.PortfolioID != "port-disk-1" {
			t.Errorf("expected rolled back portfolio port-disk-1, got %q", recRollback.PortfolioID)
		}

		// Nonexistent activation ID should fail
		_, err = mgr.RollbackToActivation(ctx, "act-nonexistent", &revalInput)
		if err == nil {
			t.Fatalf("expected error for nonexistent activation ID")
		}
	})
}
