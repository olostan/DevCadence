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

	t.Run("post-intent failure during initial activation triggers rollback and cleans visible state", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "devcadence-activation-fail-init-*")
		if err != nil {
			t.Fatalf("MkdirTemp failed: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		mgr, err := cognition.NewActivationManager(tmpDir, nil, clk)
		if err != nil {
			t.Fatalf("NewActivationManager failed: %v", err)
		}

		// Inject failure during step 4 (write LineageFileName): make LineageFileName a directory
		lineageBlocker := filepath.Join(tmpDir, cognition.LineageFileName)
		if err := os.Mkdir(lineageBlocker, 0755); err != nil {
			t.Fatalf("Mkdir blocker failed: %v", err)
		}

		p1 := makeTestPortfolio()
		_, err = mgr.Activate(ctx, cognition.ValidationInput{
			Portfolio:       p1,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Clock:           clk,
		})
		if err == nil {
			t.Fatalf("expected Activate to fail when lineage write fails")
		}

		// Verify pending activation journal was cleaned up by rollback
		pendingPath := filepath.Join(tmpDir, cognition.PendingActivationFileName)
		if _, statErr := os.Stat(pendingPath); !os.IsNotExist(statErr) {
			t.Errorf("expected pending journal to be removed by rollbackOnFailure")
		}

		// Verify active-portfolio.json was cleaned up (no partial state)
		activePath := filepath.Join(tmpDir, cognition.ActivePortfolioFileName)
		if _, statErr := os.Stat(activePath); !os.IsNotExist(statErr) {
			t.Errorf("expected active-portfolio.json to be cleaned up on initial activation rollback")
		}
	})

	t.Run("post-intent failure during subsequent activation restores previous active portfolio", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "devcadence-activation-fail-subsequent-*")
		if err != nil {
			t.Fatalf("MkdirTemp failed: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		mgr, err := cognition.NewActivationManager(tmpDir, nil, clk)
		if err != nil {
			t.Fatalf("NewActivationManager failed: %v", err)
		}

		p1 := makeTestPortfolio()
		p1.PortfolioID = "port-sub-1"
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

		// Inject failure during step 4 of subsequent activation: make LineageFileName a directory
		lineagePath := filepath.Join(tmpDir, cognition.LineageFileName)
		_ = os.Remove(lineagePath)
		if err := os.Mkdir(lineagePath, 0755); err != nil {
			t.Fatalf("Mkdir blocker failed: %v", err)
		}

		p2 := makeTestPortfolio()
		p2.PortfolioID = "port-sub-2"
		p2.Revision = 2
		_, err = mgr.Activate(ctx, cognition.ValidationInput{
			Portfolio:       p2,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Clock:           clk,
		})
		if err == nil {
			t.Fatalf("expected subsequent Activate to fail when lineage write fails")
		}

		// Verify pending journal removed
		pendingPath := filepath.Join(tmpDir, cognition.PendingActivationFileName)
		if _, statErr := os.Stat(pendingPath); !os.IsNotExist(statErr) {
			t.Errorf("expected pending journal to be removed by rollbackOnFailure")
		}

		// Verify active-portfolio.json was restored to p1
		activeBytes, err := os.ReadFile(filepath.Join(tmpDir, cognition.ActivePortfolioFileName))
		if err != nil {
			t.Fatalf("failed to read active portfolio: %v", err)
		}
		var restoredPort protocol.CognitionPortfolio
		if err := json.Unmarshal(activeBytes, &restoredPort); err != nil {
			t.Fatalf("unmarshal active portfolio failed: %v", err)
		}
		if restoredPort.PortfolioID != "port-sub-1" {
			t.Errorf("expected active portfolio to remain port-sub-1, got %q", restoredPort.PortfolioID)
		}
	})

	t.Run("recovery active portfolio write failure propagates error and preserves pending journal", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "devcadence-recovery-active-fail-*")
		if err != nil {
			t.Fatalf("MkdirTemp failed: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		p := makeTestPortfolio()
		rec := cognition.ActivationRecord{
			ActivationID:      "act-rec-active-01",
			Sequence:          1,
			ActivatedAt:       "2026-10-02T20:00:00Z",
			PortfolioID:       p.PortfolioID,
			PortfolioRevision: p.Revision,
			Portfolio:         *p,
		}
		pending := cognition.PendingActivation{
			Record:          rec,
			Lineage:         cognition.PortfolioLineage{CurrentActivationID: rec.ActivationID, History: []cognition.ActivationRecord{rec}},
			HistoryFileName: "activation-000001-act-rec-active-01.json",
		}
		pendingBytes, _ := json.MarshalIndent(pending, "", "  ")
		pendingPath := filepath.Join(tmpDir, cognition.PendingActivationFileName)
		if err := os.WriteFile(pendingPath, pendingBytes, 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		// Inject failure for active portfolio write: make active-portfolio.json a directory
		if err := os.Mkdir(filepath.Join(tmpDir, cognition.ActivePortfolioFileName), 0755); err != nil {
			t.Fatalf("Mkdir blocker failed: %v", err)
		}

		_, err = cognition.NewActivationManager(tmpDir, nil, clk)
		if err == nil {
			t.Fatalf("expected NewActivationManager to fail when active portfolio write fails")
		}
		if _, statErr := os.Stat(pendingPath); statErr != nil {
			t.Errorf("expected pending journal to be preserved, got statErr: %v", statErr)
		}
	})

	t.Run("corrupted pending activation journal fails closed with CategoryIntegrity and preserves journal", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "devcadence-activation-corrupt-journal-*")
		if err != nil {
			t.Fatalf("MkdirTemp failed: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		pendingPath := filepath.Join(tmpDir, cognition.PendingActivationFileName)
		corruptJSON := []byte(`{"record": { invalid-json-not-well-formed`)
		if err := os.WriteFile(pendingPath, corruptJSON, 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		_, err = cognition.NewActivationManager(tmpDir, nil, clk)
		if err == nil {
			t.Fatalf("expected NewActivationManager to fail on corrupted pending journal")
		}
		if errs.CategoryOf(err) != errs.CategoryIntegrity {
			t.Errorf("expected CategoryIntegrity, got: %v", err)
		}

		// Verify journal was NOT silently deleted
		if _, statErr := os.Stat(pendingPath); statErr != nil {
			t.Errorf("expected corrupted pending journal to be preserved for inspection, got statErr: %v", statErr)
		}
	})

	t.Run("recovery write failure propagates error and preserves pending journal file", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "devcadence-activation-fail-write-*")
		if err != nil {
			t.Fatalf("MkdirTemp failed: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		p := makeTestPortfolio()
		rec := cognition.ActivationRecord{
			ActivationID:      "act-fail-write-01",
			Sequence:          1,
			ActivatedAt:       "2026-10-02T20:00:00Z",
			PortfolioID:       p.PortfolioID,
			PortfolioRevision: p.Revision,
			Portfolio:         *p,
			CandidateDigest:   "sha256:cand-01",
			InventoryDigest:   "sha256:inv-01",
			PolicyDigest:      "sha256:pol-01",
		}
		lineage := cognition.PortfolioLineage{
			CurrentActivationID:      rec.ActivationID,
			CurrentSequence:          rec.Sequence,
			CurrentPortfolioID:       rec.PortfolioID,
			CurrentPortfolioRevision: rec.PortfolioRevision,
			ActivatedAt:              rec.ActivatedAt,
			InventoryDigest:          rec.InventoryDigest,
			CandidateDigest:          rec.CandidateDigest,
			PolicyDigest:             rec.PolicyDigest,
			History:                  []cognition.ActivationRecord{rec},
		}

		pending := cognition.PendingActivation{
			Record:          rec,
			Lineage:         lineage,
			HistoryFileName: "activation-000001-act-fail-write-01.json",
		}
		pendingBytes, _ := json.MarshalIndent(pending, "", "  ")
		pendingPath := filepath.Join(tmpDir, cognition.PendingActivationFileName)
		if err := os.WriteFile(pendingPath, pendingBytes, 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		// Inject failure: block history directory creation/write by creating portfolio-history as a non-directory file
		histDir := filepath.Join(tmpDir, cognition.HistoryDirName)
		if err := os.WriteFile(histDir, []byte("blocker-file"), 0444); err != nil {
			t.Fatalf("WriteFile for histDir blocker failed: %v", err)
		}

		_, err = cognition.NewActivationManager(tmpDir, nil, clk)
		if err == nil {
			t.Fatalf("expected NewActivationManager to fail when recovery write fails")
		}

		// Verify pending journal was NOT deleted
		if _, statErr := os.Stat(pendingPath); statErr != nil {
			t.Errorf("expected pending journal to be preserved on recovery write failure, got statErr: %v", statErr)
		}
	})
}
