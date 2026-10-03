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

	t.Run("GetActivePortfolio and GetActivationHistory edge cases", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "devcadence-activation-edge-*")
		if err != nil {
			t.Fatalf("MkdirTemp failed: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		mgr, err := cognition.NewActivationManager(tmpDir, nil, clk)
		if err != nil {
			t.Fatalf("NewActivationManager failed: %v", err)
		}

		// Empty dir: GetActivePortfolio -> CategoryNotFound
		_, _, err = mgr.GetActivePortfolio(ctx)
		if err == nil || errs.CategoryOf(err) != errs.CategoryNotFound {
			t.Errorf("expected CategoryNotFound on empty dir, got: %v", err)
		}

		// Empty dir: GetActivationHistory -> nil, nil
		hist, err := mgr.GetActivationHistory(ctx)
		if err != nil || hist != nil {
			t.Errorf("expected nil history on empty dir, got: %v, %v", hist, err)
		}

		// Malformed active-portfolio.json -> CategoryIntegrity
		activePath := filepath.Join(tmpDir, cognition.ActivePortfolioFileName)
		_ = os.WriteFile(activePath, []byte("bad-json"), 0644)
		_, _, err = mgr.GetActivePortfolio(ctx)
		if err == nil || errs.CategoryOf(err) != errs.CategoryIntegrity {
			t.Errorf("expected CategoryIntegrity on malformed active portfolio, got: %v", err)
		}

		// Valid active-portfolio.json but missing lineage.json -> returns portfolio with nil record
		validP := makeTestPortfolio()
		data, err := json.MarshalIndent(validP, "", "  ")
		if err != nil {
			t.Fatalf("MarshalIndent failed: %v", err)
		}
		_ = os.WriteFile(activePath, data, 0644)
		p, rec, err := mgr.GetActivePortfolio(ctx)
		if err != nil || p == nil || rec != nil {
			t.Errorf("expected portfolio with nil record when lineage missing, got: %v, %v, %v", p, rec, err)
		}

		// Corrupted lineage.json -> GetActivationHistory returns CategoryIntegrity
		lineagePath := filepath.Join(tmpDir, cognition.LineageFileName)
		_ = os.WriteFile(lineagePath, []byte("bad-json"), 0644)
		_, err = mgr.GetActivationHistory(ctx)
		if err == nil || errs.CategoryOf(err) != errs.CategoryIntegrity {
			t.Errorf("expected CategoryIntegrity on corrupted lineage, got: %v", err)
		}

		// RollbackToActivation with empty ID -> CategoryInvalidArgument
		_, err = mgr.RollbackToActivation(ctx, "", nil)
		if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
			t.Errorf("expected CategoryInvalidArgument for empty activation ID, got: %v", err)
		}

		// NewActivationManager with empty dir -> CategoryInvalidArgument
		_, err = cognition.NewActivationManager("", nil, clk)
		if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
			t.Errorf("expected CategoryInvalidArgument for empty dir, got: %v", err)
		}
	})
}
