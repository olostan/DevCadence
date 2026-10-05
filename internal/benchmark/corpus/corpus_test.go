package corpus_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/olostan/DevCadence/internal/benchmark"
	"github.com/olostan/DevCadence/internal/benchmark/corpus"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// ACC-01: Default corpus loader returns registry with 10 tasks spanning 4 workloads with valid contracts and digests.
func TestACC01_LoadDefaultCorpus(t *testing.T) {
	reg, err := corpus.LoadDefaultCorpus()
	if err != nil {
		t.Fatalf("LoadDefaultCorpus failed: %v", err)
	}

	if count := reg.Count(); count != 10 {
		t.Fatalf("expected exactly 10 tasks, got %d", count)
	}

	tasks := reg.ListTasks()
	if len(tasks) != 10 {
		t.Fatalf("expected ListTasks to return 10 tasks, got %d", len(tasks))
	}

	for _, task := range tasks {
		if strings.TrimSpace(task.TaskID) == "" {
			t.Errorf("task has empty TaskID: %+v", task)
		}
		if strings.TrimSpace(task.Contract) == "" {
			t.Errorf("task %q has empty Contract", task.TaskID)
		}
		if !task.WorkloadKind.Valid() {
			t.Errorf("task %q has invalid WorkloadKind: %q", task.TaskID, task.WorkloadKind)
		}
		digest := task.Digest()
		if !strings.HasPrefix(digest, "sha256:") || len(digest) != 71 {
			t.Errorf("task %q has invalid digest format: %q", task.TaskID, digest)
		}
	}
}

// ACC-02: Tasks queried by workload return expected counts (2, 4, 2, 2) and are sorted by TaskID.
func TestACC02_TasksByWorkload(t *testing.T) {
	reg, err := corpus.LoadDefaultCorpus()
	if err != nil {
		t.Fatalf("LoadDefaultCorpus failed: %v", err)
	}

	expectations := map[protocol.WorkloadKind]int{
		protocol.WorkloadNavigation:     2,
		protocol.WorkloadImplementation: 4,
		protocol.WorkloadReview:         2,
		protocol.WorkloadArchitecture:   2,
	}

	for workload, expectedCount := range expectations {
		tasks := reg.TasksByWorkload(workload)
		if len(tasks) != expectedCount {
			t.Errorf("workload %q: expected %d tasks, got %d", workload, expectedCount, len(tasks))
		}

		// Verify sorting determinism (INV-03)
		for i := 1; i < len(tasks); i++ {
			if tasks[i-1].TaskID >= tasks[i].TaskID {
				t.Errorf("workload %q: tasks not sorted by TaskID: %q >= %q", workload, tasks[i-1].TaskID, tasks[i].TaskID)
			}
		}
	}
}

// ACC-03: Calibrated defects queried for tasks span all 3 defect categories.
func TestACC03_GetDefectsForTask(t *testing.T) {
	reg, err := corpus.LoadDefaultCorpus()
	if err != nil {
		t.Fatalf("LoadDefaultCorpus failed: %v", err)
	}

	categoriesObserved := make(map[benchmark.DefectCategory]int)
	tasks := reg.ListTasks()

	for _, task := range tasks {
		defects := reg.GetDefectsForTask(task.TaskID)
		for _, d := range defects {
			categoriesObserved[d.Category.Canonical()]++
		}
	}

	requiredCategories := []benchmark.DefectCategory{
		benchmark.DefectInvariantViolation,
		benchmark.DefectAPIMutation,
		benchmark.DefectBoundaryViolation,
	}

	for _, cat := range requiredCategories {
		if count := categoriesObserved[cat]; count == 0 {
			t.Errorf("missing required defect category %q across task defects", cat)
		}
	}

	// Unknown task returns empty slice
	emptyDefects := reg.GetDefectsForTask("non-existent-task")
	if emptyDefects == nil || len(emptyDefects) != 0 {
		t.Errorf("expected empty slice for non-existent task, got %#v", emptyDefects)
	}
}

// ACC-04: RegisterTask rejects empty TaskID, empty Contract, or invalid WorkloadKind with CategoryInvalidArgument.
func TestACC04_RegisterTask_Validation(t *testing.T) {
	reg := corpus.NewCorpusRegistry()

	// Empty TaskID
	err := reg.RegisterTask(benchmark.BenchmarkTask{
		TaskID:       "",
		Contract:     "Some contract",
		WorkloadKind: protocol.WorkloadImplementation,
	})
	if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for empty TaskID, got %v", err)
	}
	if !errors.Is(err, corpus.ErrCorpusValidation) {
		t.Errorf("expected ErrCorpusValidation for empty TaskID, got %v", err)
	}

	// Empty Contract
	err = reg.RegisterTask(benchmark.BenchmarkTask{
		TaskID:       "task-01",
		Contract:     "   ",
		WorkloadKind: protocol.WorkloadImplementation,
	})
	if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for empty Contract, got %v", err)
	}
	if !errors.Is(err, corpus.ErrCorpusValidation) {
		t.Errorf("expected ErrCorpusValidation for empty Contract, got %v", err)
	}

	// Invalid WorkloadKind
	err = reg.RegisterTask(benchmark.BenchmarkTask{
		TaskID:       "task-01",
		Contract:     "Some contract",
		WorkloadKind: protocol.WorkloadKind("bogus_workload"),
	})
	if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for invalid WorkloadKind, got %v", err)
	}
	if !errors.Is(err, corpus.ErrCorpusValidation) {
		t.Errorf("expected ErrCorpusValidation for invalid WorkloadKind, got %v", err)
	}
}

// ACC-05: Modify task returned by GetTask() -> registry copy remains unmodified (INV-04).
func TestACC05_GetTask_DefensiveCopy(t *testing.T) {
	reg, err := corpus.LoadDefaultCorpus()
	if err != nil {
		t.Fatalf("LoadDefaultCorpus failed: %v", err)
	}

	task1, err := reg.GetTask("task-impl-01")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}

	originalContract := task1.Contract
	originalReadFilesCount := len(task1.ReadFiles)

	// Mutate fields and slice
	task1.Contract = "Mutated contract that should not persist"
	task1.ReadFiles = append(task1.ReadFiles, "extra_mutated_file.go")

	// Fetch again
	task2, err := reg.GetTask("task-impl-01")
	if err != nil {
		t.Fatalf("GetTask failed on second fetch: %v", err)
	}

	if task2.Contract != originalContract {
		t.Errorf("registry task contract was mutated: got %q, expected %q", task2.Contract, originalContract)
	}
	if len(task2.ReadFiles) != originalReadFilesCount {
		t.Errorf("registry task ReadFiles was mutated: got len %d, expected len %d", len(task2.ReadFiles), originalReadFilesCount)
	}
}

// Mutation Catalog Tests
func TestMutationCatalog_Mutants(t *testing.T) {
	reg := corpus.NewCorpusRegistry()

	validTask := benchmark.BenchmarkTask{
		TaskID:       "task-test-01",
		Name:         "Test Task",
		Contract:     "Pass all checks",
		WorkloadKind: protocol.WorkloadImplementation,
	}

	if err := reg.RegisterTask(validTask); err != nil {
		t.Fatalf("RegisterTask failed: %v", err)
	}

	// Mutant 1: Duplicate task ID overwrites existing task silently
	err := reg.RegisterTask(validTask)
	if err == nil || errs.CategoryOf(err) != errs.CategoryConflict {
		t.Errorf("Mutant 1 killed: expected CategoryConflict for duplicate task, got %v", err)
	}

	// Mutant 2: TasksByWorkload returns tasks unsorted
	anotherTask := benchmark.BenchmarkTask{
		TaskID:       "task-test-00",
		Name:         "Zero Task",
		Contract:     "Pass all checks",
		WorkloadKind: protocol.WorkloadImplementation,
	}
	if err := reg.RegisterTask(anotherTask); err != nil {
		t.Fatalf("RegisterTask failed: %v", err)
	}

	tasks := reg.TasksByWorkload(protocol.WorkloadImplementation)
	if len(tasks) != 2 || tasks[0].TaskID != "task-test-00" || tasks[1].TaskID != "task-test-01" {
		t.Errorf("Mutant 2 killed: expected sorted tasks, got [%s, %s]", tasks[0].TaskID, tasks[1].TaskID)
	}

	// Mutant 4: Defect associated to non-existent task accepted silently
	err = reg.AssociateDefect("non-existent-task-id", benchmark.SeededDefect{
		DefectID: "DEFECT-TEST-01",
		Category: benchmark.DefectInvariantViolation,
	})
	if err == nil || errs.CategoryOf(err) != errs.CategoryNotFound {
		t.Errorf("Mutant 4 killed: expected CategoryNotFound, got %v", err)
	}

	// Mutant 5: RegisterTask accepts invalid WorkloadKind
	err = reg.RegisterTask(benchmark.BenchmarkTask{
		TaskID:       "task-invalid-workload",
		Contract:     "Contract",
		WorkloadKind: protocol.WorkloadKind("unknown"),
	})
	if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("Mutant 5 killed: expected CategoryInvalidArgument, got %v", err)
	}

	// Mutant 6: GetTask non-existent task returns CategoryNotFound
	_, err = reg.GetTask("does-not-exist")
	if err == nil || errs.CategoryOf(err) != errs.CategoryNotFound {
		t.Errorf("Mutant 6 killed: expected CategoryNotFound, got %v", err)
	}
}

// Digest stability: repeated calls yield identical digests.
func TestINV01_DigestStability(t *testing.T) {
	reg1, err := corpus.LoadDefaultCorpus()
	if err != nil {
		t.Fatalf("LoadDefaultCorpus failed: %v", err)
	}

	d1 := reg1.Digest()
	d2 := reg1.Digest()
	if d1 != d2 {
		t.Fatalf("digest unstable across repeated calls on same registry: %q != %q", d1, d2)
	}

	reg2, err := corpus.LoadDefaultCorpus()
	if err != nil {
		t.Fatalf("LoadDefaultCorpus second instance failed: %v", err)
	}
	d3 := reg2.Digest()
	if d1 != d3 {
		t.Fatalf("digest differs across separate LoadDefaultCorpus calls: %q != %q", d1, d3)
	}

	// Repeatability of LoadCorpusManifest
	m1, err := corpus.LoadCorpusManifest()
	if err != nil {
		t.Fatalf("LoadCorpusManifest first instance failed: %v", err)
	}
	m2, err := corpus.LoadCorpusManifest()
	if err != nil {
		t.Fatalf("LoadCorpusManifest second instance failed: %v", err)
	}
	if m1.Digest != m2.Digest {
		t.Fatalf("LoadCorpusManifest digest unstable across calls: %q != %q", m1.Digest, m2.Digest)
	}
	if m1.Digest != d1 {
		t.Fatalf("CorpusManifest digest does not match registry digest: %q != %q", m1.Digest, d1)
	}
}

// Mutant M-04: Altering a seeded defect's PatchContent MUST change CorpusRegistry.Digest() and CorpusManifest.Digest.
func TestCorpusRegistry_Digest_PatchContentSensitivity(t *testing.T) {
	task := benchmark.BenchmarkTask{
		TaskID:       "task-patch-sensitivity",
		Name:         "Patch Sensitivity Test",
		Contract:     "Deterministic Digest Requirement",
		WorkloadKind: protocol.WorkloadImplementation,
	}

	defect1 := benchmark.SeededDefect{
		DefectID:          "DEF-PATCH-01",
		Category:          benchmark.DefectInvariantViolation,
		FileTarget:        "internal/sample/file.go",
		PatchContent:      "original patch content",
		ViolatedInvariant: "INV-01",
	}

	defect2 := defect1
	defect2.PatchContent = "modified patch content that alters defect semantics"

	reg1 := corpus.NewCorpusRegistry()
	if err := reg1.RegisterTask(task); err != nil {
		t.Fatalf("RegisterTask failed: %v", err)
	}
	if err := reg1.AssociateDefect(task.TaskID, defect1); err != nil {
		t.Fatalf("AssociateDefect failed: %v", err)
	}

	reg2 := corpus.NewCorpusRegistry()
	if err := reg2.RegisterTask(task); err != nil {
		t.Fatalf("RegisterTask failed: %v", err)
	}
	if err := reg2.AssociateDefect(task.TaskID, defect2); err != nil {
		t.Fatalf("AssociateDefect failed: %v", err)
	}

	d1 := reg1.Digest()
	d2 := reg2.Digest()
	if d1 == d2 {
		t.Fatalf("CorpusRegistry.Digest() did not change when PatchContent changed: %q == %q", d1, d2)
	}

	// Verify CorpusManifest.Digest also reflects PatchContent difference
	manifest1 := &corpus.CorpusManifest{
		Digest:      reg1.Digest(),
		TaskCount:   reg1.Count(),
		DefectCount: reg1.DefectCount(),
		Tasks:       reg1.ListTasks(),
		Defects:     reg1.ListDefects(),
	}
	manifest2 := &corpus.CorpusManifest{
		Digest:      reg2.Digest(),
		TaskCount:   reg2.Count(),
		DefectCount: reg2.DefectCount(),
		Tasks:       reg2.ListTasks(),
		Defects:     reg2.ListDefects(),
	}
	if manifest1.Digest == manifest2.Digest {
		t.Fatalf("CorpusManifest.Digest did not change when PatchContent changed: %q == %q", manifest1.Digest, manifest2.Digest)
	}
}

// Query methods: TasksByComplexity, DefectByID, DefectsByCategory, DefectsBySeverity.
func TestCorpusRegistry_QueryMethods(t *testing.T) {
	reg, err := corpus.LoadDefaultCorpus()
	if err != nil {
		t.Fatalf("LoadDefaultCorpus failed: %v", err)
	}

	// Complexity queries
	microTasks := reg.TasksByComplexity("micro_refactor")
	if len(microTasks) != 4 {
		t.Errorf("expected 4 micro_refactor tasks, got %d", len(microTasks))
	}
	greenfieldTasks := reg.TasksByComplexity("greenfield_component")
	if len(greenfieldTasks) != 2 {
		t.Errorf("expected 2 greenfield_component tasks, got %d", len(greenfieldTasks))
	}
	complexTasks := reg.TasksByComplexity("complex_evolution")
	if len(complexTasks) != 4 {
		t.Errorf("expected 4 complex_evolution tasks, got %d", len(complexTasks))
	}

	// DefectByID
	d, err := reg.DefectByID("DEFECT-INV-01")
	if err != nil || d.DefectID != "DEFECT-INV-01" {
		t.Errorf("DefectByID failed: %v", err)
	}
	_, err = reg.DefectByID("NON-EXISTENT")
	if err == nil || errs.CategoryOf(err) != errs.CategoryNotFound {
		t.Errorf("expected CategoryNotFound for missing defect, got %v", err)
	}

	// DefectsByCategory
	invDefects := reg.DefectsByCategory(benchmark.DefectInvariantViolation)
	if len(invDefects) < 4 {
		t.Errorf("expected at least 4 invariant_violation defects, got %d", len(invDefects))
	}
	apiDefects := reg.DefectsByCategory(benchmark.DefectAPIMutation)
	if len(apiDefects) < 4 {
		t.Errorf("expected at least 4 api_mutation defects, got %d", len(apiDefects))
	}
	boundDefects := reg.DefectsByCategory(benchmark.DefectBoundaryViolation)
	if len(boundDefects) < 4 {
		t.Errorf("expected at least 4 boundary_violation defects, got %d", len(boundDefects))
	}

	// Extended category queries
	archDefects := reg.DefectsByCategory("architectural_boundary")
	if len(archDefects) == 0 {
		t.Errorf("expected architectural_boundary defects, got %d", len(archDefects))
	}
	stateDefects := reg.DefectsByCategory("state_corruption")
	if len(stateDefects) == 0 {
		t.Errorf("expected state_corruption defects, got %d", len(stateDefects))
	}

	// DefectsBySeverity
	blockingDefects := reg.DefectsBySeverity("blocking")
	if len(blockingDefects) == 0 {
		t.Errorf("expected blocking defects, got %d", len(blockingDefects))
	}
	criticalDefects := reg.DefectsBySeverity("critical")
	if len(criticalDefects) == 0 {
		t.Errorf("expected critical defects, got %d", len(criticalDefects))
	}

	// DefectCount and ListDefects
	if reg.DefectCount() != 12 {
		t.Errorf("expected 12 unique defects, got %d", reg.DefectCount())
	}
	allDefects := reg.ListDefects()
	if len(allDefects) != 12 {
		t.Errorf("expected 12 defects in ListDefects, got %d", len(allDefects))
	}
}

// Standalone DefectRegistry and CorpusManifest tests.
func TestStandaloneDefectRegistryAndManifest(t *testing.T) {
	// TaskByID check
	reg, err := corpus.LoadDefaultCorpus()
	if err != nil {
		t.Fatalf("LoadDefaultCorpus failed: %v", err)
	}
	tByID, err := reg.TaskByID("task-impl-01")
	if err != nil || tByID.TaskID != "task-impl-01" {
		t.Errorf("TaskByID failed: %v", err)
	}

	dr, err := corpus.LoadDefaultDefectRegistry()
	if err != nil {
		t.Fatalf("LoadDefaultDefectRegistry failed: %v", err)
	}

	if dr.Count() != 12 {
		t.Errorf("expected 12 defects in DefectRegistry, got %d", dr.Count())
	}

	d, err := dr.DefectByID("DEFECT-API-01")
	if err != nil || d.DefectID != "DEFECT-API-01" {
		t.Errorf("dr.DefectByID failed: %v", err)
	}

	// Missing defect in DefectRegistry
	_, err = dr.DefectByID("MISSING-ID")
	if err == nil || errs.CategoryOf(err) != errs.CategoryNotFound {
		t.Errorf("expected CategoryNotFound for missing defect, got %v", err)
	}

	// Duplicate defect registration in DefectRegistry
	err = dr.RegisterDefect(*d)
	if err == nil || errs.CategoryOf(err) != errs.CategoryConflict {
		t.Errorf("expected CategoryConflict for duplicate defect, got %v", err)
	}

	// dr.DefectsByCategory
	catDefects := dr.DefectsByCategory(benchmark.DefectInvariantViolation)
	if len(catDefects) == 0 {
		t.Errorf("expected invariant defects in dr, got 0")
	}

	// dr.DefectsBySeverity
	critDefects := dr.DefectsBySeverity("critical")
	if len(critDefects) == 0 {
		t.Errorf("expected critical defects in dr, got 0")
	}

	// dr.ListDefects
	allDefects := dr.ListDefects()
	if len(allDefects) != 12 {
		t.Errorf("expected 12 defects in dr.ListDefects, got %d", len(allDefects))
	}

	// dr.ValidateDefect
	if err := dr.ValidateDefect(*d); err != nil {
		t.Errorf("dr.ValidateDefect failed: %v", err)
	}

	// Manifest
	manifest, err := corpus.LoadCorpusManifest()
	if err != nil {
		t.Fatalf("LoadCorpusManifest failed: %v", err)
	}
	if manifest.TaskCount != 10 {
		t.Errorf("expected 10 tasks in manifest, got %d", manifest.TaskCount)
	}
	if manifest.DefectCount != 12 {
		t.Errorf("expected 12 defects in manifest, got %d", manifest.DefectCount)
	}
	if !strings.HasPrefix(manifest.Digest, "sha256:") {
		t.Errorf("invalid manifest digest: %s", manifest.Digest)
	}
	if !strings.Contains(manifest.String(), "CorpusManifest") {
		t.Errorf("unexpected manifest String(): %s", manifest.String())
	}
}

// Validation helper tests
func TestValidateDefect_Rejections(t *testing.T) {
	// Empty DefectID
	err := corpus.ValidateDefect(benchmark.SeededDefect{
		DefectID: "",
		Category: benchmark.DefectInvariantViolation,
	})
	if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for empty DefectID, got %v", err)
	}
	if !errors.Is(err, corpus.ErrCorpusValidation) {
		t.Errorf("expected ErrCorpusValidation for empty DefectID, got %v", err)
	}

	// Invalid Category
	err = corpus.ValidateDefect(benchmark.SeededDefect{
		DefectID: "DEF-01",
		Category: benchmark.DefectCategory("invalid_category"),
	})
	if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for invalid Category, got %v", err)
	}
	if !errors.Is(err, corpus.ErrCorpusValidation) {
		t.Errorf("expected ErrCorpusValidation for invalid Category, got %v", err)
	}

	// Patch content without file target
	err = corpus.ValidateDefect(benchmark.SeededDefect{
		DefectID:     "DEF-01",
		Category:     benchmark.DefectInvariantViolation,
		PatchContent: "some content",
		FileTarget:   "",
	})
	if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for patch with empty file target, got %v", err)
	}
	if !errors.Is(err, corpus.ErrCorpusValidation) {
		t.Errorf("expected ErrCorpusValidation for patch with empty file target, got %v", err)
	}

	// Missing patch content when file target is specified
	err = corpus.ValidateDefect(benchmark.SeededDefect{
		DefectID:     "DEF-01",
		Category:     benchmark.DefectInvariantViolation,
		FileTarget:   "internal/foo/bar.go",
		PatchContent: "   ",
	})
	if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument for empty patch content with file target, got %v", err)
	}
	if !errors.Is(err, corpus.ErrCorpusValidation) {
		t.Errorf("expected ErrCorpusValidation for empty patch content with file target, got %v", err)
	}
}

// Patch containment verification (INV-02).
func TestINV02_PatchContainment(t *testing.T) {
	reg, err := corpus.LoadDefaultCorpus()
	if err != nil {
		t.Fatalf("LoadDefaultCorpus failed: %v", err)
	}

	tmpDir := t.TempDir()
	applier := &benchmark.DefaultPatchApplier{}

	defects := reg.ListDefects()
	for _, defect := range defects {
		// If defect is boundary traversal, applier MUST reject it fail-closed
		isTraversal := strings.HasPrefix(defect.FileTarget, "..") || filepath.IsAbs(defect.FileTarget)
		err := applier.Apply(tmpDir, &defect)
		if isTraversal {
			if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
				t.Errorf("expected CategoryPolicyDenied for traversal defect %q (target: %q), got %v", defect.DefectID, defect.FileTarget, err)
			}
		} else {
			if err != nil {
				t.Errorf("expected successful apply for valid defect %q (target: %q), got %v", defect.DefectID, defect.FileTarget, err)
			}
			// Verify file was written inside worktree
			fullPath := filepath.Join(tmpDir, defect.FileTarget)
			if _, statErr := os.Stat(fullPath); statErr != nil {
				t.Errorf("expected target file %q to exist after patch apply", fullPath)
			}
		}
	}
}

// AssociateDefect standalone happy path test.
func TestCorpusRegistry_AssociateDefect_HappyPath(t *testing.T) {
	reg := corpus.NewCorpusRegistry()
	task := benchmark.BenchmarkTask{
		TaskID:       "task-assoc-happy",
		Name:         "Happy Path Task",
		Contract:     "Must accept associated defects",
		WorkloadKind: protocol.WorkloadNavigation,
	}
	if err := reg.RegisterTask(task); err != nil {
		t.Fatalf("RegisterTask failed: %v", err)
	}

	defect := benchmark.SeededDefect{
		DefectID:          "DEF-HAPPY-01",
		Category:          benchmark.DefectInvariantViolation,
		Description:       "Happy path defect test",
		FileTarget:        "internal/happy/path.go",
		PatchContent:      "package happy\n",
		ViolatedInvariant: "INV-HAPPY-01",
	}

	if err := reg.AssociateDefect(task.TaskID, defect); err != nil {
		t.Fatalf("AssociateDefect failed on happy path: %v", err)
	}

	defects := reg.GetDefectsForTask(task.TaskID)
	if len(defects) != 1 {
		t.Fatalf("expected 1 defect for task %q, got %d", task.TaskID, len(defects))
	}
	if defects[0].DefectID != defect.DefectID {
		t.Errorf("defect ID mismatch: got %q, expected %q", defects[0].DefectID, defect.DefectID)
	}

	retrieved, err := reg.DefectByID(defect.DefectID)
	if err != nil {
		t.Fatalf("DefectByID failed: %v", err)
	}
	if retrieved.DefectID != defect.DefectID || retrieved.PatchContent != defect.PatchContent {
		t.Errorf("retrieved defect mismatch: got %+v, expected %+v", retrieved, defect)
	}
}

// Concurrency test: registry is safe under parallel reads and writes under -race.
func TestCorpusRegistry_ConcurrentAccess(t *testing.T) {
	reg, err := corpus.LoadDefaultCorpus()
	if err != nil {
		t.Fatalf("LoadDefaultCorpus failed: %v", err)
	}

	var wg sync.WaitGroup
	readWorkers := 20
	writeWorkers := 5
	iterations := 50

	// Concurrent readers
	for i := 0; i < readWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = reg.ListTasks()
				_ = reg.TasksByWorkload(protocol.WorkloadImplementation)
				_ = reg.TasksByComplexity("micro_refactor")
				_, _ = reg.GetTask("task-impl-01")
				_ = reg.GetDefectsForTask("task-impl-01")
				_ = reg.ListDefects()
				_ = reg.DefectsByCategory(benchmark.DefectInvariantViolation)
				_ = reg.Digest()
			}
		}()
	}

	// Concurrent writers (RegisterTask and AssociateDefect)
	for i := 0; i < writeWorkers; i++ {
		wg.Add(1)
		workerID := i
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				taskID := fmt.Sprintf("task-concurrent-%d-%d", workerID, j)
				newTask := benchmark.BenchmarkTask{
					TaskID:       taskID,
					Name:         fmt.Sprintf("Concurrent Task %d-%d", workerID, j),
					Contract:     "Concurrency Contract",
					WorkloadKind: protocol.WorkloadImplementation,
				}
				_ = reg.RegisterTask(newTask)

				newDefect := benchmark.SeededDefect{
					DefectID:          fmt.Sprintf("DEF-CONC-%d-%d", workerID, j),
					Category:          benchmark.DefectAPIMutation,
					FileTarget:        "internal/concurrent/file.go",
					PatchContent:      "package concurrent\n",
					ViolatedInvariant: "INV-CONC-01",
				}
				// AssociateDefect may target the newly added task or existing tasks
				_ = reg.AssociateDefect(taskID, newDefect)
			}
		}()
	}

	wg.Wait()
}
