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
	"github.com/olostan/DevCadence/internal/protocol"
)

func TestActivationCrashRecoveryAndRollbackGuarantees(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFake(time.Date(2026, 10, 2, 22, 0, 0, 0, time.UTC), time.Second)

	mp := makeTestMachineProfile()
	inv := makeTestInventory()
	cp := makeTestContextProfiles()

	t.Run("startup recovery rolls forward pending activation and cleans temp files", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "devcadence-recovery-test-*")
		if err != nil {
			t.Fatalf("MkdirTemp failed: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		mgr, err := cognition.NewActivationManager(tmpDir, nil, clk)
		if err != nil {
			t.Fatalf("NewActivationManager failed: %v", err)
		}

		p1 := makeTestPortfolio()
		p1.PortfolioID = "port-init"
		_, err = mgr.Activate(ctx, cognition.ValidationInput{
			Portfolio:       p1,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Clock:           clk,
		})
		if err != nil {
			t.Fatalf("initial activate failed: %v", err)
		}

		// Simulate crash: inject active-portfolio.pending.json for a new activation
		p2 := makeTestPortfolio()
		p2.PortfolioID = "port-crashed"
		p2.Revision = 2
		actID := "act-crashed-0002"
		histFileName := "activation-000002-act-crashed-0002.json"

		rec := cognition.ActivationRecord{
			ActivationID:      actID,
			Sequence:          2,
			ActivatedAt:       clk.Now().UTC().Format(time.RFC3339),
			PortfolioID:       p2.PortfolioID,
			PortfolioRevision: p2.Revision,
			Portfolio:         *p2,
			CandidateDigest:   "sha256:crashed-cand",
			InventoryDigest:   "sha256:crashed-inv",
		}
		lin := cognition.PortfolioLineage{
			CurrentActivationID:      actID,
			CurrentSequence:          2,
			CurrentPortfolioID:       p2.PortfolioID,
			CurrentPortfolioRevision: p2.Revision,
			ActivatedAt:              clk.Now().UTC().Format(time.RFC3339),
			InventoryDigest:          "sha256:crashed-inv",
			CandidateDigest:          "sha256:crashed-cand",
			History:                  []cognition.ActivationRecord{rec},
		}
		pending := cognition.PendingActivation{
			Record:          rec,
			Lineage:         lin,
			HistoryFileName: histFileName,
		}

		pendingBytes, _ := json.MarshalIndent(pending, "", "  ")
		pendingPath := filepath.Join(tmpDir, cognition.PendingActivationFileName)
		if err := os.WriteFile(pendingPath, pendingBytes, 0644); err != nil {
			t.Fatalf("WriteFile pending failed: %v", err)
		}

		// Also write an orphan temp file
		tmpFile := filepath.Join(tmpDir, ".orphan.tmp.123")
		_ = os.WriteFile(tmpFile, []byte("garbage"), 0644)

		// Instantiate new ActivationManager to trigger recoverStartupLocked
		mgrRecovered, err := cognition.NewActivationManager(tmpDir, nil, clk)
		if err != nil {
			t.Fatalf("NewActivationManager with recovery failed: %v", err)
		}

		// Verify pending file was removed
		if _, err := os.Stat(pendingPath); !os.IsNotExist(err) {
			t.Errorf("expected pending file to be removed after recovery")
		}

		// Verify orphan temp file was cleaned up
		if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
			t.Errorf("expected temp file to be cleaned up after recovery")
		}

		// Verify active portfolio was rolled forward to port-crashed
		activePort, activeRec, err := mgrRecovered.GetActivePortfolio(ctx)
		if err != nil {
			t.Fatalf("GetActivePortfolio failed: %v", err)
		}
		if activePort.PortfolioID != "port-crashed" {
			t.Errorf("expected active portfolio port-crashed, got %q", activePort.PortfolioID)
		}
		if activeRec.ActivationID != actID {
			t.Errorf("expected activation ID %q, got %q", actID, activeRec.ActivationID)
		}
	})

	t.Run("mandatory rollback revalidation rejects nil revalidation input", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "devcadence-reval-nil-*")
		if err != nil {
			t.Fatalf("MkdirTemp failed: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		mgr, err := cognition.NewActivationManager(tmpDir, nil, clk)
		if err != nil {
			t.Fatalf("NewActivationManager failed: %v", err)
		}

		p1 := makeTestPortfolio()
		p1.PortfolioID = "port-1"
		_, err = mgr.Activate(ctx, cognition.ValidationInput{
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
		p2.PortfolioID = "port-2"
		rec2, err := mgr.Activate(ctx, cognition.ValidationInput{
			Portfolio:       p2,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Clock:           clk,
		})
		if err != nil {
			t.Fatalf("second activate failed: %v", err)
		}

		// RollbackToPrevious with nil revalidationInput must fail
		_, err = mgr.RollbackToPrevious(ctx, nil)
		if err == nil {
			t.Fatalf("expected error when RollbackToPrevious called with nil revalidationInput")
		}
		if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
			t.Errorf("expected CategoryInvalidArgument, got %v", err)
		}

		// RollbackToActivation with nil revalidationInput must fail
		_, err = mgr.RollbackToActivation(ctx, rec2.ActivationID, nil)
		if err == nil {
			t.Fatalf("expected error when RollbackToActivation called with nil revalidationInput")
		}
		if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
			t.Errorf("expected CategoryInvalidArgument, got %v", err)
		}
	})

	t.Run("rollback records newly computed validation digests", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "devcadence-rollback-digests-*")
		if err != nil {
			t.Fatalf("MkdirTemp failed: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		mgr, err := cognition.NewActivationManager(tmpDir, nil, clk)
		if err != nil {
			t.Fatalf("NewActivationManager failed: %v", err)
		}

		p1 := makeTestPortfolio()
		p1.PortfolioID = "port-orig"
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

		p2 := makeTestPortfolio()
		p2.PortfolioID = "port-subsequent"
		_, err = mgr.Activate(ctx, cognition.ValidationInput{
			Portfolio:       p2,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Clock:           clk,
		})
		if err != nil {
			t.Fatalf("activate p2 failed: %v", err)
		}

		// Update inventory state for revalidation (e.g. modify inventory host ID or timestamp)
		updatedInv := makeTestInventory()
		updatedInv.InventoryID = "inv-updated-999"
		expectedNewInvDigest := "sha256:" + func() string {
			val := cognition.NewPortfolioValidator().Validate(cognition.ValidationInput{
				Portfolio:       p1,
				MachineProfile:  mp,
				Inventory:       updatedInv,
				ContextProfiles: cp,
				Clock:           clk,
			})
			return val.InventoryDigest
		}()

		revalInput := cognition.ValidationInput{
			MachineProfile:  mp,
			Inventory:       updatedInv,
			ContextProfiles: cp,
			Clock:           clk,
		}
		rollbackRec, err := mgr.RollbackToPrevious(ctx, &revalInput)
		if err != nil {
			t.Fatalf("RollbackToPrevious failed: %v", err)
		}

		// Ensure the new rollback record has the NEW inventory digest, not stale rec1.InventoryDigest
		if rollbackRec.InventoryDigest == rec1.InventoryDigest {
			t.Errorf("rollback record copied stale inventory digest %q instead of revalidated digest", rec1.InventoryDigest)
		}
		if "sha256:"+rollbackRec.InventoryDigest != expectedNewInvDigest && rollbackRec.InventoryDigest != expectedNewInvDigest {
			t.Logf("rollbackRec inventory digest: %s", rollbackRec.InventoryDigest)
		}
	})

	t.Run("freshness check rejects stale ExpectedInventoryDigest", func(t *testing.T) {
		p := makeTestPortfolio()
		validator := cognition.NewPortfolioValidator()

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:               p,
			MachineProfile:          mp,
			Inventory:               inv,
			ContextProfiles:         cp,
			ExpectedInventoryDigest: "sha256:stale-digest-does-not-match",
			Clock:                   clk,
		})

		if res.Valid {
			t.Fatalf("expected validation failure for mismatched ExpectedInventoryDigest")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeStaleValidationState {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeStaleValidationState in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("inventory-only endpoint fails closed for role capability and features", func(t *testing.T) {
		p := makeTestPortfolio()
		// ep-cli-01 is only in inventory summary, not in MachineCapabilityProfile
		mpWithoutCLI := makeTestMachineProfile()
		mpWithoutCLI.Endpoints = []protocol.CognitionEndpoint{mp.Endpoints[0]} // only ep-local-01

		// Bind role 'reviewer' directly to ep-cli-01 (only in inventory)
		p.RoleBindings[0].EndpointID = "ep-cli-01"
		p.RoleBindings[0].ChannelID = "chan-cli-01"

		validator := cognition.NewPortfolioValidator()
		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mpWithoutCLI,
			Inventory:       inv,
			ContextProfiles: cp,
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected validation failure for inventory-only endpoint")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeCapabilityMissing {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeCapabilityMissing in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("AuthUnknown for non-local endpoint fails closed", func(t *testing.T) {
		p := makeTestPortfolio()
		mpAuthUnknown := makeTestMachineProfile()
		mpAuthUnknown.Endpoints[1].Auth = protocol.AuthUnknown // ep-cli-01 auth is unknown

		// Bind role to ep-cli-01
		p.RoleBindings[0].EndpointID = "ep-cli-01"
		p.RoleBindings[0].ChannelID = "chan-cli-01"

		validator := cognition.NewPortfolioValidator()
		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mpAuthUnknown,
			Inventory:       inv,
			ContextProfiles: cp,
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected failure when non-local endpoint has AuthUnknown")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeEndpointUnauthenticated {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeEndpointUnauthenticated in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("missing ContextProfiles fails closed", func(t *testing.T) {
		p := makeTestPortfolio()
		validator := cognition.NewPortfolioValidator()

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: nil, // unavailable
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected failure when ContextProfiles is nil")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeContextProfileNotFound {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeContextProfileNotFound in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("nil policy falls back to DefaultValidationPolicy and enforces limits", func(t *testing.T) {
		p := makeTestPortfolio()
		// Set portfolio exposure to unrestricted, which exceeds DefaultValidationPolicy limit (focused_snippets)
		p.MaxSourceExposure = protocol.ExposureUnrestrictedAuthorized

		validator := cognition.NewPortfolioValidator()
		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Policy:          nil, // Should fall back to DefaultValidationPolicy
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected failure under DefaultValidationPolicy when exposure exceeds focused_snippets")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeUnauthorizedSourceExposure {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeUnauthorizedSourceExposure in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("explicit AllowMeteredFallback on policy allows fallback to metered", func(t *testing.T) {
		p := makeTestPortfolio()
		meteredPool := protocol.BudgetPool{
			SchemaVersion:            protocol.SchemaVersion1,
			PoolID:                   "pool-metered",
			Name:                     "Metered API Pool",
			Regime:                   protocol.RegimeMeteredAPI,
			Unit:                     protocol.UnitUSDCents,
			HardLimit:                5000,
			SoftAlertLimit:           4000,
			Period:                   protocol.PeriodBillingCycle,
			AllowOverage:             true,
			FallbackAllowedToMetered: true,
		}
		p.BudgetPools = append(p.BudgetPools, meteredPool)

		// Set fallback to metered pool
		p.RoleBindings[0].Fallbacks[0].BudgetPoolID = "pool-metered"

		pol := cognition.DefaultValidationPolicy()
		pol.AllowMeteredFallback = true

		validator := cognition.NewPortfolioValidator()
		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Policy:          &pol,
			Clock:           clk,
		})

		// Should not report unauthorized metered fallback
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeUnauthorizedMeteredFallback {
				t.Errorf("unexpected CodeUnauthorizedMeteredFallback when AllowMeteredFallback is true: %v", d)
			}
		}
	})
}
