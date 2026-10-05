package telemetry

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// ACC-01: Snapshots recorded across strategies, verifying aggregation, higher cache efficiency, lower residency.
func TestAggregate_ACC01_MultiStrategy(t *testing.T) {
	snapshots := []RunTelemetrySnapshot{
		{
			RunID:                 "run-1",
			TaskID:                "task-1",
			Strategy:              "full_history",
			Capability:            "code_edit",
			InitialTokens:         5000,
			PeakResidentTokens:    12000,
			CachedTokens:          1000,
			OutputTokens:          500,
			CumulativeInputTokens: 20000,
			Duration:              10 * time.Second,
			Turns:                 3,
			DefectSeeded:          true,
			DefectStatus:          "detected",
			Accepted:              true,
			ReviewFindingsCount:   1,
			AccountingUncertain:   false,
			LayerBreakdown: &LayerBreakdownMetrics{
				ProtectedCoreTokens:      2000,
				StateCapsuleTokens:       1000,
				EvidenceWorkingSetTokens: 1500,
				EphemeralTailTokens:      500,
				TotalTokens:              5000,
			},
		},
		{
			RunID:                 "run-2",
			TaskID:                "task-1",
			Strategy:              "hybrid_4layer",
			Capability:            "code_edit",
			InitialTokens:         3000,
			PeakResidentTokens:    4500,
			CachedTokens:          12000,
			OutputTokens:          400,
			CumulativeInputTokens: 15000,
			Duration:              8 * time.Second,
			Turns:                 2,
			DefectSeeded:          true,
			DefectStatus:          "prevented",
			Accepted:              true,
			ReviewFindingsCount:   0,
			AccountingUncertain:   false,
			LayerBreakdown: &LayerBreakdownMetrics{
				ProtectedCoreTokens:      1500,
				StateCapsuleTokens:       500,
				EvidenceWorkingSetTokens: 800,
				EphemeralTailTokens:      200,
				TotalTokens:              3000,
			},
		},
	}

	report, err := Aggregate(snapshots)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.TotalSnapshots != 2 {
		t.Errorf("expected 2 snapshots, got %d", report.TotalSnapshots)
	}
	if len(report.Groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(report.Groups))
	}

	// Order should be full_history, then hybrid_4layer
	gFull := report.Groups[0]
	gHybrid := report.Groups[1]

	if gFull.Strategy != "full_history" {
		t.Errorf("expected group 0 strategy full_history, got %s", gFull.Strategy)
	}
	if gHybrid.Strategy != "hybrid_4layer" {
		t.Errorf("expected group 1 strategy hybrid_4layer, got %s", gHybrid.Strategy)
	}

	// Hybrid strategy should show higher cache hit ratio and lower peak resident tokens
	if gHybrid.CacheHitRatio <= gFull.CacheHitRatio {
		t.Errorf("expected hybrid cache hit ratio (%f) > full_history (%f)", gHybrid.CacheHitRatio, gFull.CacheHitRatio)
	}
	if gHybrid.AvgPeakResidentTokens >= gFull.AvgPeakResidentTokens {
		t.Errorf("expected hybrid avg peak resident tokens (%f) < full_history (%f)", gHybrid.AvgPeakResidentTokens, gFull.AvgPeakResidentTokens)
	}

	if gHybrid.FirstPassAcceptanceRate != 1.0 {
		t.Errorf("expected 1.0 acceptance rate, got %f", gHybrid.FirstPassAcceptanceRate)
	}
	if !gHybrid.DefectCatchRateApplicable || gHybrid.DefectCatchRate != 1.0 {
		t.Errorf("expected applicable defect catch rate of 1.0, got %f (applicable: %v)", gHybrid.DefectCatchRate, gHybrid.DefectCatchRateApplicable)
	}
}

// ACC-02: Snapshots with 0 accepted runs calculates safely without panic/NaN, flags IsUndefined = true.
func TestAggregate_ACC02_ZeroAcceptedRuns(t *testing.T) {
	snapshots := []RunTelemetrySnapshot{
		{
			RunID:                 "run-fail-1",
			TaskID:                "task-fail",
			Strategy:              "compacted",
			Capability:            "refactor",
			InitialTokens:         4000,
			PeakResidentTokens:    8000,
			CachedTokens:          2000,
			OutputTokens:          100,
			CumulativeInputTokens: 10000,
			Duration:              5 * time.Second,
			Turns:                 1,
			DefectSeeded:          false,
			DefectStatus:          "not_applicable",
			Accepted:              false,
			AccountingUncertain:   false,
		},
	}

	report, err := Aggregate(snapshots)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(report.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(report.Groups))
	}

	g := report.Groups[0]
	if g.AcceptedCount != 0 {
		t.Errorf("expected 0 accepted count, got %d", g.AcceptedCount)
	}
	if !g.ResourcePerAcceptedResult.IsUndefined {
		t.Errorf("expected ResourcePerAcceptedResult.IsUndefined to be true")
	}
	if g.ResourcePerAcceptedResult.Value != 0 {
		t.Errorf("expected ResourcePerAcceptedResult.Value to be 0, got %f", g.ResourcePerAcceptedResult.Value)
	}

	// Verify JSON marshaling does not emit NaN/Inf
	jsonBytes, err := GenerateJSONReport(report)
	if err != nil {
		t.Fatalf("failed to generate JSON report: %v", err)
	}
	if strings.Contains(string(jsonBytes), "NaN") || strings.Contains(string(jsonBytes), "Inf") {
		t.Errorf("JSON report contains NaN/Inf: %s", string(jsonBytes))
	}
}

// ACC-03: Uncertainty flag propagation when any snapshot has AccountingUncertain: true.
func TestAggregate_ACC03_UncertaintyPropagation(t *testing.T) {
	snapshots := []RunTelemetrySnapshot{
		{
			RunID:                 "run-cert",
			TaskID:                "task-1",
			Strategy:              "hybrid_4layer",
			Capability:            "code_edit",
			InitialTokens:         1000,
			PeakResidentTokens:    2000,
			CachedTokens:          500,
			CumulativeInputTokens: 2500,
			Accepted:              true,
			AccountingUncertain:   false,
		},
		{
			RunID:                 "run-uncert",
			TaskID:                "task-1",
			Strategy:              "hybrid_4layer",
			Capability:            "code_edit",
			InitialTokens:         1200,
			PeakResidentTokens:    2400,
			CachedTokens:          600,
			CumulativeInputTokens: 3000,
			Accepted:              true,
			AccountingUncertain:   true,
		},
	}

	report, err := Aggregate(snapshots)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(report.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(report.Groups))
	}
	if !report.Groups[0].ContainsUncertainAccounting {
		t.Errorf("expected ContainsUncertainAccounting to be true")
	}

	// Also verify Markdown output notes uncertainty
	md, err := GenerateMarkdownReport(report)
	if err != nil {
		t.Fatalf("unexpected error generating markdown: %v", err)
	}
	if !strings.Contains(md, "**YES (provisional)**") {
		t.Errorf("expected markdown report to flag provisional uncertainty, got:\n%s", md)
	}
}

// ACC-04: Markdown report formatting for undefined resource efficiency.
func TestReporter_ACC04_MarkdownFormatting(t *testing.T) {
	report := &AggregatedReport{
		GeneratedAt:    time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC),
		TotalSnapshots: 1,
		Groups: []AggregatedTelemetry{
			{
				Strategy:                  "snippet_pool",
				Capability:                "test_fix",
				RunCount:                  1,
				AcceptedCount:             0,
				FirstPassAcceptanceRate:   0.0,
				DefectCatchRate:           1.0,
				DefectCatchRateApplicable: false,
				CacheHitRatio:             0.25,
				AvgPeakResidentTokens:     5000,
				AvgCumulativeInputTokens:  8000,
				ResourcePerAcceptedResult: ResourceEfficiency{
					Value:       0,
					IsUndefined: true,
				},
				ContainsUncertainAccounting: false,
			},
		},
	}

	md, err := GenerateMarkdownReport(report)
	if err != nil {
		t.Fatalf("unexpected error generating markdown: %v", err)
	}

	if !strings.Contains(md, "N/A (no accepted runs)") {
		t.Errorf("expected markdown to contain 'N/A (no accepted runs)', got:\n%s", md)
	}
	if !strings.Contains(md, "N/A (no defects)") {
		t.Errorf("expected markdown to contain 'N/A (no defects)', got:\n%s", md)
	}
}

// ACC-05: Empty snapshot slice returns valid empty report with TotalSnapshots: 0 without error.
func TestAggregate_ACC05_EmptySnapshots(t *testing.T) {
	report, err := Aggregate(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report == nil {
		t.Fatalf("expected non-nil report")
	}
	if report.TotalSnapshots != 0 {
		t.Errorf("expected TotalSnapshots: 0, got %d", report.TotalSnapshots)
	}
	if report.Groups != nil {
		t.Errorf("expected Groups: nil, got %v", report.Groups)
	}

	report2, err := Aggregate([]RunTelemetrySnapshot{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report2.TotalSnapshots != 0 || report2.Groups != nil {
		t.Errorf("expected TotalSnapshots: 0 and nil groups for empty slice, got %+v", report2)
	}

	md, err := GenerateMarkdownReport(report)
	if err != nil {
		t.Fatalf("unexpected error generating markdown for empty report: %v", err)
	}
	if !strings.Contains(md, "No telemetry groups recorded.") {
		t.Errorf("expected 'No telemetry groups recorded.' in markdown, got:\n%s", md)
	}
}

// Mutation test: Division by zero when count(DefectSeeded) == 0.
func TestAggregate_Mutant_ZeroDefectsSeeded(t *testing.T) {
	snapshots := []RunTelemetrySnapshot{
		{
			RunID:                 "run-1",
			TaskID:                "task-1",
			Strategy:              "hybrid_4layer",
			Capability:            "code_edit",
			InitialTokens:         1000,
			PeakResidentTokens:    2000,
			CumulativeInputTokens: 3000,
			Accepted:              true,
			DefectSeeded:          false,
			DefectStatus:          "not_applicable",
		},
	}

	report, err := Aggregate(snapshots)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	g := report.Groups[0]
	if g.DefectCatchRateApplicable {
		t.Errorf("expected DefectCatchRateApplicable to be false when 0 defects seeded")
	}
	if g.DefectCatchRate != 1.0 {
		t.Errorf("expected DefectCatchRate to be 1.0 when 0 defects seeded, got %f", g.DefectCatchRate)
	}
}

// Mutation test: Layer breakdown sum validation (TotalTokens == sum of parts).
func TestLayerBreakdown_Integrity(t *testing.T) {
	lb := LayerBreakdownMetrics{
		ProtectedCoreTokens:      1000,
		StateCapsuleTokens:       500,
		EvidenceWorkingSetTokens: 1200,
		EphemeralTailTokens:      300,
		TotalTokens:              3000,
	}

	sum := lb.ProtectedCoreTokens + lb.StateCapsuleTokens + lb.EvidenceWorkingSetTokens + lb.EphemeralTailTokens
	if lb.TotalTokens != sum {
		t.Fatalf("layer breakdown integrity violation: total %d != sum of parts %d", lb.TotalTokens, sum)
	}
}

// Mutation test: Group ordering determinism (Strategy ascending, Capability ascending).
func TestAggregate_GroupOrderingDeterminism(t *testing.T) {
	snapshots := []RunTelemetrySnapshot{
		{Strategy: "snippet_pool", Capability: "zebra", CumulativeInputTokens: 100},
		{Strategy: "full_history", Capability: "beta", CumulativeInputTokens: 100},
		{Strategy: "snippet_pool", Capability: "alpha", CumulativeInputTokens: 100},
		{Strategy: "full_history", Capability: "alpha", CumulativeInputTokens: 100},
		{Strategy: "compacted", Capability: "omega", CumulativeInputTokens: 100},
	}

	for i := 0; i < 5; i++ {
		report, err := Aggregate(snapshots)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedOrder := [][2]string{
			{"compacted", "omega"},
			{"full_history", "alpha"},
			{"full_history", "beta"},
			{"snippet_pool", "alpha"},
			{"snippet_pool", "zebra"},
		}

		if len(report.Groups) != len(expectedOrder) {
			t.Fatalf("expected %d groups, got %d", len(expectedOrder), len(report.Groups))
		}

		for idx, exp := range expectedOrder {
			if report.Groups[idx].Strategy != exp[0] || report.Groups[idx].Capability != exp[1] {
				t.Fatalf("iteration %d group %d mismatch: got (%s, %s), expected (%s, %s)",
					i, idx, report.Groups[idx].Strategy, report.Groups[idx].Capability, exp[0], exp[1])
			}
		}
	}
}

// TelemetryCollector thread-safety and functionality test.
func TestTelemetryCollector_ConcurrentAccess(t *testing.T) {
	collector := NewTelemetryCollector()

	const numGoroutines = 20
	const numSnapshotsPerRoutine = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	// Writers
	for i := 0; i < numGoroutines; i++ {
		go func(routineID int) {
			defer wg.Done()
			for j := 0; j < numSnapshotsPerRoutine; j++ {
				runID := fmt.Sprintf("run-%d-%d", routineID, j)
				collector.RecordSnapshot(RunTelemetrySnapshot{
					RunID:                 runID,
					TaskID:                "task-concurrent",
					Strategy:              "hybrid_4layer",
					Capability:            "code_edit",
					InitialTokens:         int64(j * 100),
					CumulativeInputTokens: int64(j * 200),
				})

				_ = collector.RecordLayerBreakdown(runID, LayerBreakdownMetrics{
					ProtectedCoreTokens:      int64(j * 50),
					StateCapsuleTokens:       int64(j * 25),
					EvidenceWorkingSetTokens: int64(j * 15),
					EphemeralTailTokens:      int64(j * 10),
					TotalTokens:              int64(j * 100),
				})
			}
		}(i)
	}

	// Readers
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numSnapshotsPerRoutine; j++ {
				_ = collector.GetSnapshots()
			}
		}()
	}

	wg.Wait()

	all := collector.GetSnapshots()
	expectedTotal := numGoroutines * numSnapshotsPerRoutine
	if len(all) != expectedTotal {
		t.Fatalf("expected %d snapshots recorded, got %d", expectedTotal, len(all))
	}

	// Verify updating unknown run ID returns error
	err := collector.RecordLayerBreakdown("non-existent-run", LayerBreakdownMetrics{})
	if err == nil {
		t.Fatalf("expected error when recording layer breakdown for unknown run ID, got nil")
	}

	// Verify updating existing snapshot with RecordSnapshot
	collector.RecordSnapshot(RunTelemetrySnapshot{
		RunID:      "run-0-0",
		TaskID:     "updated-task",
		Strategy:   "hybrid_4layer",
		Capability: "code_edit",
	})
	updated := collector.GetSnapshots()
	found := false
	for _, s := range updated {
		if s.RunID == "run-0-0" && s.TaskID == "updated-task" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected updated snapshot for run-0-0")
	}

	// Verify defensive copy
	all[0].TaskID = "modified-externally"
	fresh := collector.GetSnapshots()
	if fresh[0].TaskID == "modified-externally" {
		t.Fatalf("GetSnapshots() did not return an isolated snapshot slice")
	}
}

// Test nil error handling in reporter
func TestReporter_NilReport(t *testing.T) {
	_, errJSON := GenerateJSONReport(nil)
	if errJSON == nil {
		t.Fatalf("expected error for nil report in GenerateJSONReport")
	}

	_, errMD := GenerateMarkdownReport(nil)
	if errMD == nil {
		t.Fatalf("expected error for nil report in GenerateMarkdownReport")
	}
}

// Test ResourceEfficiency JSON formatting
func TestResourceEfficiency_MarshalJSON(t *testing.T) {
	effUndefined := ResourceEfficiency{Value: 0, IsUndefined: true}
	data, err := json.Marshal(effUndefined)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if !strings.Contains(string(data), `"is_undefined":true`) {
		t.Errorf("expected is_undefined:true in %s", string(data))
	}

	effDefined := ResourceEfficiency{Value: 1234.5, IsUndefined: false}
	data2, err := json.Marshal(effDefined)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if !strings.Contains(string(data2), `"is_undefined":false`) || !strings.Contains(string(data2), `1234.5`) {
		t.Errorf("expected is_undefined:false and 1234.5 in %s", string(data2))
	}
}
