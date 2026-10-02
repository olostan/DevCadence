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
	// Epistemic honesty checks (Finding 5, 6)
	if prof.AccountingMethod != protocol.AccountingApproximateEstimate {
		t.Errorf("expected AccountingApproximateEstimate, got %s", prof.AccountingMethod)
	}
	if prof.ObservedContextControl != protocol.ContextControlUnknown {
		t.Errorf("expected ContextControlUnknown, got %s", prof.ObservedContextControl)
	}
	if prof.ObservedPrefixCache != protocol.PrefixCacheUnknown {
		t.Errorf("expected PrefixCacheUnknown, got %s", prof.ObservedPrefixCache)
	}
	if len(prof.WorkloadEnvelopes) == 0 || prof.WorkloadEnvelopes[0].ConfidenceLevel != "provisional" {
		t.Errorf("expected provisional confidence level on workload envelope")
	}

	// Test profile with explicit observed capabilities
	customProf := compiler.DefaultProvisionalProfileWithCapabilities(
		"test-ep-2", "test-chan-2", "model-2", 65536, 30000,
		protocol.ContextControlExactStateless, protocol.PrefixCacheExplicit,
	)
	if err := customProf.Validate(); err != nil {
		t.Fatalf("custom profile failed validation: %v", err)
	}
	if customProf.ObservedContextControl != protocol.ContextControlExactStateless {
		t.Errorf("expected ContextControlExactStateless, got %s", customProf.ObservedContextControl)
	}
	if customProf.ObservedPrefixCache != protocol.PrefixCacheExplicit {
		t.Errorf("expected PrefixCacheExplicit, got %s", customProf.ObservedPrefixCache)
	}
	if customProf.TargetResidentTokens != 30000 {
		t.Errorf("expected custom TargetResidentTokens 30000, got %d", customProf.TargetResidentTokens)
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

	// Exact BPE and Provider API must be rejected by heuristic estimator (Finding 3)
	if _, err := compiler.EstimateTokens(text, protocol.AccountingExactBPE, 0.05); err == nil {
		t.Fatal("expected error for exact_bpe heuristic estimate, got nil")
	}

	if _, err := compiler.EstimateTokens(text, protocol.AccountingProviderAPI, 0.05); err == nil {
		t.Fatal("expected error for provider_api heuristic estimate, got nil")
	}

	// Approximate estimate succeeds
	tokensApprox, err := compiler.EstimateTokens(text, protocol.AccountingApproximateEstimate, 0.05)
	if err != nil {
		t.Fatalf("unexpected error for approximate estimate: %v", err)
	}
	if tokensApprox <= 0 {
		t.Fatalf("expected positive token estimate, got %d", tokensApprox)
	}

	// Helper EstimateTokensApprox succeeds
	helperTokens := compiler.EstimateTokensApprox(text, 0.05)
	if helperTokens != tokensApprox {
		t.Fatalf("helper returned %d, want %d", helperTokens, tokensApprox)
	}

	// Empty text returns 0
	zero, err := compiler.EstimateTokens("", protocol.AccountingApproximateEstimate, 0.05)
	if err != nil || zero != 0 {
		t.Fatalf("expected (0, nil) for empty text, got (%d, %v)", zero, err)
	}
}
