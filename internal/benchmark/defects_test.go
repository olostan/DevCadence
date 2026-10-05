package benchmark

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
)

func TestEnums_Validity(t *testing.T) {
	// Strategy kinds
	strategies := []ContextStrategyKind{
		StrategyFullHistory, StrategyCompacted, StrategySnippetPool, StrategyHybrid4Layer,
	}
	for _, s := range strategies {
		if !s.Valid() {
			t.Errorf("expected strategy %q to be valid", s)
		}
	}
	if ContextStrategyKind("unknown").Valid() {
		t.Error("expected unknown strategy to be invalid")
	}

	// Defect categories
	categories := []DefectCategory{
		DefectInvariantViolation, DefectAPIMutation, DefectBoundaryViolation,
	}
	for _, c := range categories {
		if !c.Valid() {
			t.Errorf("expected category %q to be valid", c)
		}
	}
	if DefectCategory("unknown").Valid() {
		t.Error("expected unknown defect category to be invalid")
	}

	// Run statuses
	statuses := []RunStatus{
		RunStatusCompleted, RunStatusFailed, RunStatusContextUnfit,
	}
	for _, st := range statuses {
		if !st.Valid() {
			t.Errorf("expected status %q to be valid", st)
		}
	}
	if RunStatus("unknown").Valid() {
		t.Error("expected unknown run status to be invalid")
	}

	// Defect statuses
	defectStatuses := []DefectStatus{
		DefectDetected, DefectPrevented, DefectMissed, DefectIntroduced, DefectNotApplicable,
	}
	for _, ds := range defectStatuses {
		if !ds.Valid() {
			t.Errorf("expected defect status %q to be valid", ds)
		}
	}
	if DefectStatus("unknown").Valid() {
		t.Error("expected unknown defect status to be invalid")
	}
}

func TestBenchmarkTask_DigestDeterminism(t *testing.T) {
	task1 := BenchmarkTask{
		TaskID:            "task-001",
		Name:              "Add Feature",
		WorkPackageID:     "WP-M4-1",
		Contract:          "Must pass all tests without regressions.",
		ReadFiles:         []string{"pkg/a.go", "pkg/b.go"},
		TargetFiles:       []string{"pkg/c.go"},
		ExpectedMutations: []string{"func Feature()"},
	}

	task2 := BenchmarkTask{
		TaskID:            "task-001",
		Name:              "Add Feature",
		WorkPackageID:     "WP-M4-1",
		Contract:          "Must pass all tests without regressions.",
		ReadFiles:         []string{"pkg/a.go", "pkg/b.go"},
		TargetFiles:       []string{"pkg/c.go"},
		ExpectedMutations: []string{"func Feature()"},
	}

	d1 := task1.Digest()
	d2 := task2.Digest()

	if d1 != d2 {
		t.Fatalf("expected deterministic identical digests, got %q != %q", d1, d2)
	}

	if !strings.HasPrefix(d1, "sha256:") || len(d1) != 71 {
		t.Fatalf("expected digest to be sha256:<64 hex chars>, got %q", d1)
	}

	// Mutation changes digest
	task3 := task1
	task3.Contract = "Different contract"
	d3 := task3.Digest()
	if d1 == d3 {
		t.Fatal("expected different contract to change digest")
	}
}

func TestEvaluateDefectOutcome(t *testing.T) {
	defect := FixtureAuthBypass()

	// 1. Defect not applicable when defect == nil
	if res := EvaluateDefectOutcome(nil, false, false); res != DefectNotApplicable {
		t.Errorf("expected DefectNotApplicable for nil defect, got %s", res)
	}

	// 2. Defect prevented when compiler rejected upfront
	if res := EvaluateDefectOutcome(defect, true, false); res != DefectPrevented {
		t.Errorf("expected DefectPrevented when compiler rejected, got %s", res)
	}

	// 3. Defect detected when test failed
	if res := EvaluateDefectOutcome(defect, false, true); res != DefectDetected {
		t.Errorf("expected DefectDetected when test failed, got %s", res)
	}

	// 4. Defect missed when verification passed despite defect
	if res := EvaluateDefectOutcome(defect, false, false); res != DefectMissed {
		t.Errorf("expected DefectMissed when tests passed despite defect, got %s", res)
	}
}

func TestPatchApplier_BoundaryAndApplication(t *testing.T) {
	applier := &DefaultPatchApplier{}
	tmpDir := t.TempDir()

	// 1. Nil defect returns nil
	if err := applier.Apply(tmpDir, nil); err != nil {
		t.Fatalf("expected nil error for nil defect, got %v", err)
	}

	// 2. Empty file target with non-empty patch content fails closed
	badDefect := &SeededDefect{
		FileTarget:   "",
		PatchContent: "content",
	}
	if err := applier.Apply(tmpDir, badDefect); err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("expected CategoryInvalidArgument for empty target, got %v", err)
	}

	// 3. Empty worktree dir fails closed
	validDefect := FixtureAPIMutation()
	if err := applier.Apply("", validDefect); err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("expected CategoryInvalidArgument for empty worktreeDir, got %v", err)
	}

	// 4. Boundary escape path traversal fails closed with CategoryPolicyDenied
	escapeDefect := FixtureBoundaryViolation()
	if err := applier.Apply(tmpDir, escapeDefect); err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Fatalf("expected CategoryPolicyDenied for path traversal defect, got %v", err)
	}

	// 5. Valid patch writes file cleanly
	if err := applier.Apply(tmpDir, validDefect); err != nil {
		t.Fatalf("expected clean patch application, got %v", err)
	}

	targetPath := filepath.Join(tmpDir, validDefect.FileTarget)
	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("failed to read patched file: %v", err)
	}
	if string(content) != validDefect.PatchContent {
		t.Fatalf("patched content mismatch: expected %q, got %q", validDefect.PatchContent, string(content))
	}
}
