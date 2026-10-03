package cognition_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/protocol"
)

func TestActivationManager_RejectsMachineFingerprintMismatchWithoutPriorLineage(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFake(time.Date(2026, 10, 3, 16, 0, 0, 0, time.UTC), time.Second)
	mgr, err := cognition.NewActivationManager(t.TempDir(), cognition.NewPortfolioValidator(), clk)
	if err != nil {
		t.Fatalf("NewActivationManager failed: %v", err)
	}

	p := makeTestPortfolio()
	mp := makeTestMachineProfile()
	inv := makeTestInventory()
	mp.MachineFingerprint = strings.Repeat("b", 64)

	_, err = mgr.Activate(ctx, cognition.ValidationInput{
		Portfolio:       p,
		MachineProfile:  mp,
		Inventory:       inv,
		ContextProfiles: makeTestContextProfiles(),
		Clock:           clk,
	})
	if err == nil {
		t.Fatalf("expected activation to reject mismatched machine fingerprints")
	}
}

func TestActivationManager_GetActivePortfolioPropagatesLineageIntegrityError(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFake(time.Date(2026, 10, 3, 16, 0, 0, 0, time.UTC), time.Second)
	dir := t.TempDir()
	mgr, err := cognition.NewActivationManager(dir, cognition.NewPortfolioValidator(), clk)
	if err != nil {
		t.Fatalf("NewActivationManager failed: %v", err)
	}

	if _, err := mgr.Activate(ctx, cognition.ValidationInput{
		Portfolio:       makeTestPortfolio(),
		MachineProfile:  makeTestMachineProfile(),
		Inventory:       makeTestInventory(),
		ContextProfiles: makeTestContextProfiles(),
		Clock:           clk,
	}); err != nil {
		t.Fatalf("Activate failed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, cognition.LineageFileName), []byte("{not-json"), 0644); err != nil {
		t.Fatalf("corrupt lineage: %v", err)
	}

	if _, _, err := mgr.GetActivePortfolio(ctx); err == nil {
		t.Fatalf("expected lineage integrity error to be propagated")
	}
}

func TestActivationManager_RollbackToActivationRequiresExactActivationID(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFake(time.Date(2026, 10, 3, 16, 0, 0, 0, time.UTC), time.Second)
	mgr, err := cognition.NewActivationManager(t.TempDir(), cognition.NewPortfolioValidator(), clk)
	if err != nil {
		t.Fatalf("NewActivationManager failed: %v", err)
	}

	input := cognition.ValidationInput{
		Portfolio:       makeTestPortfolio(),
		MachineProfile:  makeTestMachineProfile(),
		Inventory:       makeTestInventory(),
		ContextProfiles: makeTestContextProfiles(),
		Clock:           clk,
	}
	rec1, err := mgr.Activate(ctx, input)
	if err != nil {
		t.Fatalf("first Activate failed: %v", err)
	}

	p2 := makeTestPortfolio()
	p2.PortfolioID = "portfolio-second"
	p2.Revision = 2
	input.Portfolio = p2
	if _, err := mgr.Activate(ctx, input); err != nil {
		t.Fatalf("second Activate failed: %v", err)
	}

	partial := strings.TrimSuffix(rec1.ActivationID, "01")
	if partial == rec1.ActivationID {
		t.Fatalf("test setup failed to create partial activation id")
	}
	if _, err := mgr.RollbackToActivation(ctx, partial, &input); err == nil {
		t.Fatalf("expected partial activation ID %q to be rejected", partial)
	}
}

func TestPortfolioValidator_ResourceDiagnosticsAreDeterministic(t *testing.T) {
	validator := cognition.NewPortfolioValidator()
	clk := clock.NewFake(time.Date(2026, 10, 3, 16, 0, 0, 0, time.UTC), time.Second)

	slot := func(v int) *int { return &v }
	states := map[string]*protocol.ResourceState{
		"host-z": {
			SchemaVersion:     protocol.SchemaVersion1,
			HostID:            "host-z",
			Timestamp:         "2026-10-03T16:00:00Z",
			MaxConcurrentSlots: slot(1),
			ActiveSlots:        slot(1),
		},
		"host-a": {
			SchemaVersion:     protocol.SchemaVersion1,
			HostID:            "host-a",
			Timestamp:         "2026-10-03T16:00:00Z",
			MaxConcurrentSlots: slot(1),
			ActiveSlots:        slot(1),
		},
	}

	res := validator.Validate(cognition.ValidationInput{
		Portfolio:       makeTestPortfolio(),
		MachineProfile:  makeTestMachineProfile(),
		Inventory:       makeTestInventory(),
		ContextProfiles: makeTestContextProfiles(),
		ResourceStates:  states,
		Clock:           clk,
	})

	var targets []string
	for _, d := range res.Diagnostics {
		if d.Code == cognition.CodeResourceCapacityExceeded {
			targets = append(targets, d.Target)
		}
	}
	if len(targets) != 2 {
		t.Fatalf("expected two resource capacity diagnostics, got %v", targets)
	}
	if targets[0] != "resource_states[host-a]" || targets[1] != "resource_states[host-z]" {
		t.Fatalf("resource diagnostics are not deterministic: %v", targets)
	}
}
