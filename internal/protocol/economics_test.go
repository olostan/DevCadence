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

	t.Run("table-driven regime constraints across all 6 regimes", func(t *testing.T) {
		regimes := []struct {
			regime                 protocol.EconomicRegime
			allowFallbackToMetered bool
			allowOverage           bool
		}{
			{protocol.RegimeLocalCompute, false, false},
			{protocol.RegimeSubscriptionQuota, false, false},
			{protocol.RegimeMeteredAPI, true, true},
			{protocol.RegimePrepaidCredits, false, true},
			{protocol.RegimeEnterpriseAllocation, false, true},
			{protocol.RegimeUnknownCustom, false, false},
		}

		for _, tc := range regimes {
			t.Run(string(tc.regime), func(t *testing.T) {
				// Test fallback allowed to metered
				bp := validBudgetPool()
				bp.Regime = tc.regime
				bp.FallbackAllowedToMetered = true
				err := bp.Validate()
				if tc.allowFallbackToMetered && err != nil {
					t.Errorf("regime %s should permit fallback_allowed_to_metered, got %v", tc.regime, err)
				}
				if !tc.allowFallbackToMetered && err == nil {
					t.Errorf("regime %s MUST forbid fallback_allowed_to_metered per ADR-0018 §9", tc.regime)
				}

				// Test allow overage
				bp = validBudgetPool()
				bp.Regime = tc.regime
				bp.AllowOverage = true
				err = bp.Validate()
				if tc.allowOverage && err != nil {
					t.Errorf("regime %s should permit allow_overage, got %v", tc.regime, err)
				}
				if !tc.allowOverage && err == nil {
					t.Errorf("regime %s MUST forbid allow_overage per ADR-0018 §9", tc.regime)
				}
			})
		}
	})
}

func TestBudgetStateValidation(t *testing.T) {
	currentUsage := int64(100)
	remainingBalance := int64(400)
	periodStart := "2026-09-01T00:00:00Z"
	periodEnd := "2026-10-01T00:00:00Z"

	t.Run("valid full BudgetState", func(t *testing.T) {
		bs := &protocol.BudgetState{
			SchemaVersion:    protocol.SchemaVersion1,
			PoolID:           "pool_1",
			CurrentUsage:     &currentUsage,
			RemainingBalance: &remainingBalance,
			PeriodStart:      &periodStart,
			PeriodEnd:        &periodEnd,
			Status:           protocol.BudgetStatusHealthy,
			ObservedAt:       "2026-09-30T00:00:00Z",
		}
		if err := bs.Validate(); err != nil {
			t.Fatalf("expected valid BudgetState, got: %v", err)
		}
		if bs.RecordKind() != "BudgetState" {
			t.Errorf("expected RecordKind BudgetState, got %q", bs.RecordKind())
		}
	})

	t.Run("valid BudgetState with honest unknown nil fields", func(t *testing.T) {
		bs := &protocol.BudgetState{
			SchemaVersion: protocol.SchemaVersion1,
			PoolID:        "pool_unmetered_subscription",
			Status:        protocol.BudgetStatusHealthy,
			ObservedAt:    "2026-09-30T00:00:00Z",
			UnknownFields: []string{"current_usage", "remaining_balance"},
		}
		if err := bs.Validate(); err != nil {
			t.Fatalf("expected valid BudgetState with nil pointers, got: %v", err)
		}
	})

	t.Run("negative usage rejected", func(t *testing.T) {
		neg := int64(-1)
		bs := &protocol.BudgetState{
			SchemaVersion: protocol.SchemaVersion1,
			PoolID:        "pool_1",
			CurrentUsage:  &neg,
			Status:        protocol.BudgetStatusHealthy,
			ObservedAt:    "2026-09-30T00:00:00Z",
		}
		if err := bs.Validate(); err == nil {
			t.Fatal("expected error on negative current usage, got nil")
		}
	})
	t.Run("exhausted status with positive balance rejected", func(t *testing.T) {
		pos := int64(10)
		bs := &protocol.BudgetState{
			SchemaVersion:    protocol.SchemaVersion1,
			PoolID:           "pool_1",
			RemainingBalance: &pos,
			Status:           protocol.BudgetStatusExhausted,
			ObservedAt:       "2026-09-30T00:00:00Z",
		}
		if err := bs.Validate(); err == nil {
			t.Fatal("expected error when status exhausted with remaining balance > 0, got nil")
		}
	})

	t.Run("exhausted status with zero balance accepted", func(t *testing.T) {
		zero := int64(0)
		bs := &protocol.BudgetState{
			SchemaVersion:    protocol.SchemaVersion1,
			PoolID:           "pool_1",
			RemainingBalance: &zero,
			Status:           protocol.BudgetStatusExhausted,
			ObservedAt:       "2026-09-30T00:00:00Z",
		}
		if err := bs.Validate(); err != nil {
			t.Fatalf("expected valid BudgetState when exhausted with 0 balance, got: %v", err)
		}
	})

	t.Run("period_end before period_start rejected", func(t *testing.T) {
		start := "2026-10-01T00:00:00Z"
		end := "2026-09-01T00:00:00Z"
		bs := &protocol.BudgetState{
			SchemaVersion: protocol.SchemaVersion1,
			PoolID:        "pool_1",
			PeriodStart:   &start,
			PeriodEnd:     &end,
			Status:        protocol.BudgetStatusHealthy,
			ObservedAt:    "2026-09-30T00:00:00Z",
		}
		if err := bs.Validate(); err == nil {
			t.Fatal("expected error when period_end before period_start, got nil")
		}
	})

	t.Run("unknown_fields with invalid field name rejected", func(t *testing.T) {
		bs := &protocol.BudgetState{
			SchemaVersion: protocol.SchemaVersion1,
			PoolID:        "pool_1",
			Status:        protocol.BudgetStatusHealthy,
			ObservedAt:    "2026-09-30T00:00:00Z",
			UnknownFields: []string{"nonexistent_metric"},
		}
		if err := bs.Validate(); err == nil {
			t.Fatal("expected error for invalid unknown_fields entry, got nil")
		}
	})

	t.Run("unknown_fields with duplicate field rejected", func(t *testing.T) {
		bs := &protocol.BudgetState{
			SchemaVersion: protocol.SchemaVersion1,
			PoolID:        "pool_1",
			Status:        protocol.BudgetStatusHealthy,
			ObservedAt:    "2026-09-30T00:00:00Z",
			UnknownFields: []string{"current_usage", "current_usage"},
		}
		if err := bs.Validate(); err == nil {
			t.Fatal("expected error for duplicate in unknown_fields, got nil")
		}
	})

	t.Run("populated field also in unknown_fields rejected", func(t *testing.T) {
		usage := int64(50)
		bs := &protocol.BudgetState{
			SchemaVersion: protocol.SchemaVersion1,
			PoolID:        "pool_1",
			CurrentUsage:  &usage,
			Status:        protocol.BudgetStatusHealthy,
			ObservedAt:    "2026-09-30T00:00:00Z",
			UnknownFields: []string{"current_usage"},
		}
		if err := bs.Validate(); err == nil {
			t.Fatal("expected error when populated field is in unknown_fields, got nil")
		}
	})

	t.Run("status unknown accepted and can be listed in unknown_fields", func(t *testing.T) {
		bs := &protocol.BudgetState{
			SchemaVersion: protocol.SchemaVersion1,
			PoolID:        "pool_unmetered_subscription",
			Status:        protocol.BudgetStatusUnknown,
			ObservedAt:    "2026-09-30T00:00:00Z",
			UnknownFields: []string{"status"},
		}
		if err := bs.Validate(); err != nil {
			t.Fatalf("expected valid BudgetState when status is unknown, got: %v", err)
		}
	})

	t.Run("status healthy listed in unknown_fields rejected", func(t *testing.T) {
		bs := &protocol.BudgetState{
			SchemaVersion: protocol.SchemaVersion1,
			PoolID:        "pool_1",
			Status:        protocol.BudgetStatusHealthy,
			ObservedAt:    "2026-09-30T00:00:00Z",
			UnknownFields: []string{"status"},
		}
		if err := bs.Validate(); err == nil {
			t.Fatal("expected error when healthy status is listed in unknown_fields, got nil")
		}
	})
}

func TestResourceStateValidation(t *testing.T) {
	gpuBytes := int64(16000000000)
	ramBytes := int64(64000000000)
	maxSlots := 4
	activeSlots := 1

	t.Run("valid full ResourceState", func(t *testing.T) {
		rs := &protocol.ResourceState{
			SchemaVersion:           protocol.SchemaVersion1,
			HostID:                  "host_1",
			Timestamp:               "2026-09-30T00:00:00Z",
			AvailableGPUMemoryBytes: &gpuBytes,
			AvailableRAMBytes:       &ramBytes,
			MaxConcurrentSlots:      &maxSlots,
			ActiveSlots:             &activeSlots,
		}
		if err := rs.Validate(); err != nil {
			t.Fatalf("expected valid ResourceState, got: %v", err)
		}
		if rs.RecordKind() != "ResourceState" {
			t.Errorf("expected RecordKind ResourceState, got %q", rs.RecordKind())
		}
	})

	t.Run("valid ResourceState with honest unknown nil metrics", func(t *testing.T) {
		rs := &protocol.ResourceState{
			SchemaVersion:  protocol.SchemaVersion1,
			HostID:         "host_headless_ci",
			Timestamp:      "2026-09-30T00:00:00Z",
			UnknownMetrics: []string{"available_gpu_memory_bytes"},
		}
		if err := rs.Validate(); err != nil {
			t.Fatalf("expected valid ResourceState with nil metrics, got: %v", err)
		}
	})

	t.Run("active slots exceeding max slots rejected", func(t *testing.T) {
		tooMany := 5
		rs := &protocol.ResourceState{
			SchemaVersion:      protocol.SchemaVersion1,
			HostID:             "host_1",
			Timestamp:          "2026-09-30T00:00:00Z",
			MaxConcurrentSlots: &maxSlots,
			ActiveSlots:        &tooMany,
		}
		if err := rs.Validate(); err == nil {
			t.Fatal("expected error when active slots > max slots, got nil")
		}
	})

	t.Run("unknown_metrics with invalid metric name rejected", func(t *testing.T) {
		rs := &protocol.ResourceState{
			SchemaVersion:  protocol.SchemaVersion1,
			HostID:         "host_1",
			Timestamp:      "2026-09-30T00:00:00Z",
			UnknownMetrics: []string{"unsupported_metric"},
		}
		if err := rs.Validate(); err == nil {
			t.Fatal("expected error for invalid unknown_metrics entry, got nil")
		}
	})

	t.Run("unknown_metrics with duplicate metric rejected", func(t *testing.T) {
		rs := &protocol.ResourceState{
			SchemaVersion:  protocol.SchemaVersion1,
			HostID:         "host_1",
			Timestamp:      "2026-09-30T00:00:00Z",
			UnknownMetrics: []string{"available_ram_bytes", "available_ram_bytes"},
		}
		if err := rs.Validate(); err == nil {
			t.Fatal("expected error for duplicate in unknown_metrics, got nil")
		}
	})

	t.Run("populated metric also in unknown_metrics rejected", func(t *testing.T) {
		rs := &protocol.ResourceState{
			SchemaVersion:     protocol.SchemaVersion1,
			HostID:            "host_1",
			Timestamp:         "2026-09-30T00:00:00Z",
			AvailableRAMBytes: &ramBytes,
			UnknownMetrics:    []string{"available_ram_bytes"},
		}
		if err := rs.Validate(); err == nil {
			t.Fatal("expected error when populated metric is in unknown_metrics, got nil")
		}
	})
}
