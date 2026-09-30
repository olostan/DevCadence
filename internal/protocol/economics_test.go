package protocol_test

import (
	"testing"

	"github.com/olostan/DevCadence/internal/protocol"
)

func validBudgetPool() *protocol.BudgetPool {
	return &protocol.BudgetPool{
		SchemaVersion:            protocol.SchemaVersion1,
		PoolID:                   "pool_1",
		Name:                     "Claude Pro Subscription",
		Regime:                   protocol.RegimeSubscriptionQuota,
		Unit:                     protocol.UnitRequests,
		HardLimit:                500,
		SoftAlertLimit:           400,
		Period:                   protocol.PeriodBillingCycle,
		AllowOverage:             false,
		FallbackAllowedToMetered: false,
	}
}

func TestBudgetPoolValidation(t *testing.T) {
	t.Run("valid budget pool passes", func(t *testing.T) {
		bp := validBudgetPool()
		if err := bp.Validate(); err != nil {
			t.Fatalf("expected valid, got: %v", err)
		}
		if bp.RecordKind() != "BudgetPool" {
			t.Errorf("record kind: got %q, want BudgetPool", bp.RecordKind())
		}
	})

	t.Run("negative hard limit rejected", func(t *testing.T) {
		bp := validBudgetPool()
		bp.HardLimit = -10
		if err := bp.Validate(); err == nil {
			t.Fatal("expected error on negative hard limit, got nil")
		}
	})

	t.Run("soft alert limit exceeding hard limit rejected", func(t *testing.T) {
		bp := validBudgetPool()
		bp.SoftAlertLimit = 600
		if err := bp.Validate(); err == nil {
			t.Fatal("expected error when soft limit > hard limit, got nil")
		}
	})

	t.Run("MUST: silent fallback to metered billing rejected for subscription_quota (ADR-0018 §9, DCI-104)", func(t *testing.T) {
		bp := validBudgetPool()
		bp.Regime = protocol.RegimeSubscriptionQuota
		bp.FallbackAllowedToMetered = true
		if err := bp.Validate(); err == nil {
			t.Fatal("expected error: subscription_quota must forbid fallback_allowed_to_metered")
		}
	})

	t.Run("MUST: silent fallback to metered billing rejected for local_compute (ADR-0018 §9, DCI-104)", func(t *testing.T) {
		bp := validBudgetPool()
		bp.Regime = protocol.RegimeLocalCompute
		bp.FallbackAllowedToMetered = true
		if err := bp.Validate(); err == nil {
			t.Fatal("expected error: local_compute must forbid fallback_allowed_to_metered")
		}
	})

	t.Run("fallback_allowed_to_metered allowed for metered_api regime itself", func(t *testing.T) {
		bp := validBudgetPool()
		bp.Regime = protocol.RegimeMeteredAPI
		bp.FallbackAllowedToMetered = true
		if err := bp.Validate(); err != nil {
			t.Fatalf("metered_api should permit fallback flag, got: %v", err)
		}
	})
}

func TestBudgetStateValidation(t *testing.T) {
	bs := protocol.BudgetState{
		PoolID:           "pool_1",
		CurrentUsage:     100,
		RemainingBalance: 400,
		PeriodStart:      "2026-09-01T00:00:00Z",
		PeriodEnd:        "2026-10-01T00:00:00Z",
		Status:           protocol.BudgetStatusHealthy,
	}
	if err := bs.Validate(); err != nil {
		t.Fatalf("expected valid BudgetState, got: %v", err)
	}

	bs.CurrentUsage = -1
	if err := bs.Validate(); err == nil {
		t.Fatal("expected error on negative current usage, got nil")
	}
}

func TestResourceStateValidation(t *testing.T) {
	rs := protocol.ResourceState{
		HostID:                  "host_1",
		Timestamp:               "2026-09-30T00:00:00Z",
		AvailableGPUMemoryBytes: 16000000000,
		AvailableRAMBytes:       64000000000,
		MaxConcurrentSlots:      4,
		ActiveSlots:             1,
	}
	if err := rs.Validate(); err != nil {
		t.Fatalf("expected valid ResourceState, got: %v", err)
	}

	rs.ActiveSlots = 5
	if err := rs.Validate(); err == nil {
		t.Fatal("expected error when active slots > max slots, got nil")
	}
}
