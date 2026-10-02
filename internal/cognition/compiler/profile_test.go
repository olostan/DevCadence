package compiler_test

import (
	"errors"
	"testing"

	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

func TestDefaultProvisionalProfile(t *testing.T) {
	prof := compiler.DefaultProvisionalProfile("test-ep", "test-chan", "qwen2.5-coder", 32768)
	if err := prof.Validate(); err != nil {
		t.Fatalf("DefaultProvisionalProfile failed protocol validation: %v", err)
	}
	if prof.HardResidentCeilingTokens <= 0 {
		t.Errorf("expected positive HardResidentCeilingTokens, got %d", prof.HardResidentCeilingTokens)
	}
	if prof.TargetResidentTokens > prof.HardResidentCeilingTokens {
		t.Errorf("TargetResidentTokens (%d) cannot exceed HardResidentCeilingTokens (%d)",
			prof.TargetResidentTokens, prof.HardResidentCeilingTokens)
	}
}

func TestEnforceProfileBounds(t *testing.T) {
	prof := compiler.DefaultProvisionalProfile("test-ep", "test-chan", "qwen2.5-coder", 32768)

	t.Run("within bounds succeeds", func(t *testing.T) {
		pack := validTestPack()
		if err := compiler.EnforceProfileBounds(pack, prof); err != nil {
			t.Fatalf("expected pack to be within bounds, got: %v", err)
		}
	})

	t.Run("exceeds runtime window fails with ContextUnfit", func(t *testing.T) {
		pack := validTestPack()
		// Artificially inflate total resident tokens to exceed runtime window
		pack.TokenAccounting.RoleTokens = 30000
		pack.TokenAccounting.TotalResidentTokens = 30000 + pack.TokenAccounting.ContractTokens +
			pack.TokenAccounting.NormativeTokens + pack.TokenAccounting.StateTokens +
			pack.TokenAccounting.EvidenceTokens + pack.TokenAccounting.TailTokens

		err := compiler.EnforceProfileBounds(pack, prof)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, errs.ErrContextUnfit) {
			t.Fatalf("expected ErrContextUnfit, got %v", err)
		}
	})

	t.Run("exceeds contract limit fails with ContextUnfit", func(t *testing.T) {
		pack := validTestPack()
		profWithSmallContract := *prof
		profWithSmallContract.ContractLimitTokens = 20 // pack has 30 contract tokens

		err := compiler.EnforceProfileBounds(pack, &profWithSmallContract)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, errs.ErrContextUnfit) {
			t.Fatalf("expected ErrContextUnfit, got %v", err)
		}
	})

	t.Run("exceeds protected core limit fails with ContextUnfit", func(t *testing.T) {
		pack := validTestPack()
		profWithSmallCore := *prof
		// Pack has 20 role + 35 normative = 55 protected core tokens
		profWithSmallCore.ProtectedCoreLimitTokens = 40

		err := compiler.EnforceProfileBounds(pack, &profWithSmallCore)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, errs.ErrContextUnfit) {
			t.Fatalf("expected ErrContextUnfit, got %v", err)
		}
	})

	t.Run("exceeds max single lease fails with ContextUnfit", func(t *testing.T) {
		pack := validTestPack()
		profWithSmallLease := *prof
		// Pack has lease with 15 tokens
		profWithSmallLease.MaxSingleLeaseTokens = 10

		err := compiler.EnforceProfileBounds(pack, &profWithSmallLease)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, errs.ErrContextUnfit) {
			t.Fatalf("expected ErrContextUnfit, got %v", err)
		}
	})
}

func TestEstimateTokens(t *testing.T) {
	text := "func RunEngine(ctx context.Context) error { return nil }"
	tokensBPE := compiler.EstimateTokens(text, protocol.AccountingExactBPE, 0.05)
	if tokensBPE <= 0 {
		t.Fatalf("expected positive token estimate, got %d", tokensBPE)
	}

	tokensAPI := compiler.EstimateTokens(text, protocol.AccountingProviderAPI, 0.05)
	if tokensAPI <= 0 {
		t.Fatalf("expected positive token estimate, got %d", tokensAPI)
	}

	tokensApprox := compiler.EstimateTokens(text, protocol.AccountingApproximateEstimate, 0.05)
	if tokensApprox <= 0 {
		t.Fatalf("expected positive token estimate, got %d", tokensApprox)
	}

	// Empty text returns 0
	if zero := compiler.EstimateTokens("", protocol.AccountingExactBPE, 0.05); zero != 0 {
		t.Fatalf("expected 0 for empty text, got %d", zero)
	}
}
