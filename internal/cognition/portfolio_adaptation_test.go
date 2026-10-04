package cognition_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/errs"
)

func TestPortfolioAdaptation_ACC07_CreateChangeProposalValidation(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	cand := *makeTestPortfolio()

	// 1. Blank rationale fails
	_, err := cognition.CreateChangeProposal(cognition.TriggerResourceChange, "   ", nil, cand, now)
	if err == nil {
		t.Fatalf("expected error for blank rationale")
	}
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument, got %v", err)
	}

	// 2. Invalid trigger fails
	_, err = cognition.CreateChangeProposal("unknown_trigger", "valid rationale", nil, cand, now)
	if err == nil {
		t.Fatalf("expected error for invalid trigger")
	}

	// 3. Invalid candidate portfolio fails
	invalidCand := cand
	invalidCand.PortfolioID = "" // invalid empty ID
	_, err = cognition.CreateChangeProposal(cognition.TriggerResourceChange, "valid rationale", nil, invalidCand, now)
	if err == nil {
		t.Fatalf("expected error for invalid candidate portfolio")
	}

	// 4. Valid proposal succeeds
	prop, err := cognition.CreateChangeProposal(cognition.TriggerResourceChange, "Upgrade local endpoint", nil, cand, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prop.ProposalID == "" {
		t.Errorf("expected non-empty ProposalID")
	}
	if prop.Trigger != cognition.TriggerResourceChange {
		t.Errorf("expected trigger %s, got %s", cognition.TriggerResourceChange, prop.Trigger)
	}
	if prop.Rationale != "Upgrade local endpoint" {
		t.Errorf("expected rationale 'Upgrade local endpoint', got %s", prop.Rationale)
	}
	if !prop.Diff.HasChanges {
		t.Errorf("expected bootstrap diff to have changes")
	}
}

func TestPortfolioAdaptation_ACC03_ProposeAndActivate(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFake(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC), time.Second)

	tmpDir, err := os.MkdirTemp("", "devcadence-adaptation-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr, err := cognition.NewActivationManager(tmpDir, nil, clk)
	if err != nil {
		t.Fatalf("NewActivationManager failed: %v", err)
	}

	service := cognition.NewAdaptationService(mgr)

	p1 := makeTestPortfolio()
	mp := makeTestMachineProfile()
	inv := makeTestInventory()
	cp := makeTestContextProfiles()
	policy := cognition.DefaultValidationPolicy()

	valInput := cognition.ValidationInput{
		MachineProfile:  mp,
		Inventory:       inv,
		ContextProfiles: cp,
		Policy:          &policy,
		Clock:           clk,
	}

	// 1. Initial activation via proposal
	prop1, err := cognition.CreateChangeProposal(cognition.TriggerManualProposal, "Initial bootstrap", nil, *p1, clk.Now())
	if err != nil {
		t.Fatalf("CreateChangeProposal failed: %v", err)
	}

	rec1, err := service.ProposeAndActivate(ctx, prop1, valInput)
	if err != nil {
		t.Fatalf("ProposeAndActivate failed: %v", err)
	}
	if rec1.Sequence != 1 {
		t.Errorf("expected sequence 1, got %d", rec1.Sequence)
	}
	if rec1.PortfolioID != p1.PortfolioID {
		t.Errorf("expected PortfolioID %s, got %s", p1.PortfolioID, rec1.PortfolioID)
	}

	// 2. Second activation updating portfolio
	p2 := makeTestPortfolio()
	p2.PortfolioID = "port-test-02"
	p2.Revision = 2

	prop2, err := cognition.CreateChangeProposal(cognition.TriggerResourceChange, "Update to revision 2", p1, *p2, clk.Now())
	if err != nil {
		t.Fatalf("CreateChangeProposal failed: %v", err)
	}

	rec2, err := service.ProposeAndActivate(ctx, prop2, valInput)
	if err != nil {
		t.Fatalf("ProposeAndActivate for p2 failed: %v", err)
	}
	if rec2.Sequence != 2 {
		t.Errorf("expected sequence 2, got %d", rec2.Sequence)
	}
	if rec2.PortfolioID != p2.PortfolioID {
		t.Errorf("expected PortfolioID %s, got %s", p2.PortfolioID, rec2.PortfolioID)
	}
	if rec2.PreviousActivationID != rec1.ActivationID {
		t.Errorf("expected previous activation %s, got %s", rec1.ActivationID, rec2.PreviousActivationID)
	}
}

func TestPortfolioAdaptation_ACC04_StaleBaseConflict(t *testing.T) {
	// Mutant kill: verify base portfolio ID mismatch in ProposeAndActivate
	ctx := context.Background()
	clk := clock.NewFake(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC), time.Second)

	tmpDir, err := os.MkdirTemp("", "devcadence-adaptation-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr, err := cognition.NewActivationManager(tmpDir, nil, clk)
	if err != nil {
		t.Fatalf("NewActivationManager failed: %v", err)
	}

	service := cognition.NewAdaptationService(mgr)

	p1 := makeTestPortfolio()
	valInput := cognition.ValidationInput{
		MachineProfile:  makeTestMachineProfile(),
		Inventory:       makeTestInventory(),
		ContextProfiles: makeTestContextProfiles(),
		Policy:          ptr(cognition.DefaultValidationPolicy()),
		Clock:           clk,
	}

	prop1, err := cognition.CreateChangeProposal(cognition.TriggerManualProposal, "Initial bootstrap", nil, *p1, clk.Now())
	if err != nil {
		t.Fatalf("CreateChangeProposal failed: %v", err)
	}
	_, err = service.ProposeAndActivate(ctx, prop1, valInput)
	if err != nil {
		t.Fatalf("initial activation failed: %v", err)
	}

	// Create candidate proposal with obsolete base ID
	p2 := makeTestPortfolio()
	p2.PortfolioID = "port-test-02"
	obsoleteBase := makeTestPortfolio()
	obsoleteBase.PortfolioID = "port-obsolete-999"

	propConflict, err := cognition.CreateChangeProposal(cognition.TriggerResourceChange, "Stale update", obsoleteBase, *p2, clk.Now())
	if err != nil {
		t.Fatalf("CreateChangeProposal failed: %v", err)
	}

	_, err = service.ProposeAndActivate(ctx, propConflict, valInput)
	if err == nil {
		t.Fatalf("expected conflict error when proposal base does not match active portfolio")
	}
	if errs.CategoryOf(err) != errs.CategoryConflict {
		t.Errorf("expected CategoryConflict, got %v", err)
	}
}

func TestPortfolioAdaptation_ACC05_RollbackPrevious(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFake(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC), time.Second)

	tmpDir, err := os.MkdirTemp("", "devcadence-adaptation-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr, err := cognition.NewActivationManager(tmpDir, nil, clk)
	if err != nil {
		t.Fatalf("NewActivationManager failed: %v", err)
	}

	service := cognition.NewAdaptationService(mgr)

	valInput := cognition.ValidationInput{
		MachineProfile:  makeTestMachineProfile(),
		Inventory:       makeTestInventory(),
		ContextProfiles: makeTestContextProfiles(),
		Policy:          ptr(cognition.DefaultValidationPolicy()),
		Clock:           clk,
	}

	// 1. Activate p1
	p1 := makeTestPortfolio()
	prop1, _ := cognition.CreateChangeProposal(cognition.TriggerManualProposal, "v1", nil, *p1, clk.Now())
	rec1, err := service.ProposeAndActivate(ctx, prop1, valInput)
	if err != nil {
		t.Fatalf("activate p1 failed: %v", err)
	}

	// 2. Activate p2
	p2 := makeTestPortfolio()
	p2.PortfolioID = "port-test-02"
	prop2, _ := cognition.CreateChangeProposal(cognition.TriggerResourceChange, "v2", p1, *p2, clk.Now())
	_, err = service.ProposeAndActivate(ctx, prop2, valInput)
	if err != nil {
		t.Fatalf("activate p2 failed: %v", err)
	}

	// 3. Rollback with empty targetActivationID (restores previous)
	recRollback, err := service.Rollback(ctx, "", &valInput)
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	if !recRollback.IsRollback {
		t.Errorf("expected IsRollback: true")
	}
	if recRollback.RollbackTargetActivationID != rec1.ActivationID {
		t.Errorf("expected target activation %s, got %s", rec1.ActivationID, recRollback.RollbackTargetActivationID)
	}
	if recRollback.PortfolioID != p1.PortfolioID {
		t.Errorf("expected rolled back portfolio ID %s, got %s", p1.PortfolioID, recRollback.PortfolioID)
	}
}

func TestPortfolioAdaptation_ACC06_RollbackFailsClosedOnMissingInventory(t *testing.T) {
	// Mutant kill: skip revalidation during Rollback
	ctx := context.Background()
	clk := clock.NewFake(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC), time.Second)

	tmpDir, err := os.MkdirTemp("", "devcadence-adaptation-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr, err := cognition.NewActivationManager(tmpDir, nil, clk)
	if err != nil {
		t.Fatalf("NewActivationManager failed: %v", err)
	}

	service := cognition.NewAdaptationService(mgr)

	valInput := cognition.ValidationInput{
		MachineProfile:  makeTestMachineProfile(),
		Inventory:       makeTestInventory(),
		ContextProfiles: makeTestContextProfiles(),
		Policy:          ptr(cognition.DefaultValidationPolicy()),
		Clock:           clk,
	}

	p1 := makeTestPortfolio()
	prop1, _ := cognition.CreateChangeProposal(cognition.TriggerManualProposal, "v1", nil, *p1, clk.Now())
	_, err = service.ProposeAndActivate(ctx, prop1, valInput)
	if err != nil {
		t.Fatalf("activate p1 failed: %v", err)
	}

	p2 := makeTestPortfolio()
	p2.PortfolioID = "port-test-02"
	prop2, _ := cognition.CreateChangeProposal(cognition.TriggerResourceChange, "v2", p1, *p2, clk.Now())
	_, err = service.ProposeAndActivate(ctx, prop2, valInput)
	if err != nil {
		t.Fatalf("activate p2 failed: %v", err)
	}

	// Revalidation input where required endpoint ep-local-01 is missing from inventory and machine profile!
	brokenInventory := makeTestInventory()
	brokenInventory.CognitionEndpoints = nil // remove all endpoints
	brokenProfile := makeTestMachineProfile()
	brokenProfile.Endpoints = nil

	revalInput := cognition.ValidationInput{
		MachineProfile:  brokenProfile,
		Inventory:       brokenInventory,
		ContextProfiles: makeTestContextProfiles(),
		Policy:          ptr(cognition.DefaultValidationPolicy()),
		Clock:           clk,
	}

	_, err = service.Rollback(ctx, "", &revalInput)
	if err == nil {
		t.Fatalf("expected rollback to fail when inventory no longer supports portfolio")
	}
	if errs.CategoryOf(err) != errs.CategoryValidationFailed {
		t.Errorf("expected CategoryValidationFailed, got %v", err)
	}

	// Verify current active portfolio was NOT modified
	activeP, _, err := mgr.GetActivePortfolio(ctx)
	if err != nil {
		t.Fatalf("GetActivePortfolio failed: %v", err)
	}
	if activeP.PortfolioID != p2.PortfolioID {
		t.Errorf("expected active portfolio to remain %s, got %s", p2.PortfolioID, activeP.PortfolioID)
	}
}

func TestPortfolioAdaptation_StopRules(t *testing.T) {
	service := cognition.NewAdaptationService(nil)
	ctx := context.Background()

	// 1. ProposeAndActivate with nil proposal
	_, err := service.ProposeAndActivate(ctx, nil, cognition.ValidationInput{})
	if err == nil {
		t.Errorf("expected error for nil proposal")
	}

	// 2. Rollback with nil revalInput
	_, err = service.Rollback(ctx, "target-1", nil)
	if err == nil {
		t.Errorf("expected error for nil revalInput")
	}
}
