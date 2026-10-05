package corpus

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"

	"github.com/olostan/DevCadence/internal/benchmark"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// ErrCorpusValidation indicates a validation failure for a corpus task or defect.
var ErrCorpusValidation = errs.New(errs.CategoryInvalidArgument, "corpus validation failed")

// CorpusRegistry maintains the canonical corpus of benchmark tasks and associated defects (REQ-05, REQ-06).
type CorpusRegistry struct {
	mu      sync.RWMutex
	tasks   map[string]benchmark.BenchmarkTask
	defects map[string][]benchmark.SeededDefect // keyed by taskID
}

// NewCorpusRegistry instantiates an empty thread-safe corpus registry (REQ-05).
func NewCorpusRegistry() *CorpusRegistry {
	return &CorpusRegistry{
		tasks:   make(map[string]benchmark.BenchmarkTask),
		defects: make(map[string][]benchmark.SeededDefect),
	}
}

// ValidateTask verifies that a task conforms to all structural and semantic requirements (REQ-03, REQ-08).
func ValidateTask(task benchmark.BenchmarkTask) error {
	const kind = "ValidateTask"
	if strings.TrimSpace(task.TaskID) == "" {
		return errs.Wrap(errs.CategoryInvalidArgument, ErrCorpusValidation, "%s: task_id cannot be empty", kind)
	}
	if strings.TrimSpace(task.Contract) == "" {
		return errs.Wrap(errs.CategoryInvalidArgument, ErrCorpusValidation, "%s: contract cannot be empty for task %q", kind, task.TaskID)
	}
	if !task.WorkloadKind.Valid() {
		return errs.Wrap(errs.CategoryInvalidArgument, ErrCorpusValidation, "%s: invalid workload kind %q for task %q", kind, task.WorkloadKind, task.TaskID)
	}
	return nil
}

// ValidateDefect verifies that a seeded defect conforms to structural and semantic requirements.
func ValidateDefect(defect benchmark.SeededDefect) error {
	const kind = "ValidateDefect"
	if strings.TrimSpace(defect.DefectID) == "" {
		return errs.Wrap(errs.CategoryInvalidArgument, ErrCorpusValidation, "%s: defect_id cannot be empty", kind)
	}
	if !defect.Category.Valid() {
		return errs.Wrap(errs.CategoryInvalidArgument, ErrCorpusValidation, "%s: invalid defect category %q for defect %q", kind, defect.Category, defect.DefectID)
	}
	if strings.TrimSpace(defect.FileTarget) == "" && defect.PatchContent != "" {
		return errs.Wrap(errs.CategoryInvalidArgument, ErrCorpusValidation, "%s: file_target cannot be empty when patch_content is present for defect %q", kind, defect.DefectID)
	}
	if strings.TrimSpace(defect.FileTarget) != "" && strings.TrimSpace(defect.PatchContent) == "" {
		return errs.Wrap(errs.CategoryInvalidArgument, ErrCorpusValidation, "%s: patch_content cannot be empty when file_target is specified for defect %q", kind, defect.DefectID)
	}
	return nil
}

// RegisterTask registers a task in the registry, failing closed on validation errors or collisions (REQ-05, REQ-08).
func (r *CorpusRegistry) RegisterTask(task benchmark.BenchmarkTask) error {
	const kind = "CorpusRegistry.RegisterTask"
	if err := ValidateTask(task); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tasks[task.TaskID]; exists {
		return errs.New(errs.CategoryConflict, "%s: task %q already registered", kind, task.TaskID)
	}

	// Defensive copy of slice fields (INV-04)
	tCopy := task
	tCopy.ReadFiles = append([]string(nil), task.ReadFiles...)
	tCopy.TargetFiles = append([]string(nil), task.TargetFiles...)
	tCopy.ExpectedMutations = append([]string(nil), task.ExpectedMutations...)

	r.tasks[task.TaskID] = tCopy
	return nil
}

// GetTask retrieves a defensive copy of the task by its ID, returning CategoryNotFound if absent (REQ-05, INV-04).
func (r *CorpusRegistry) GetTask(taskID string) (*benchmark.BenchmarkTask, error) {
	const kind = "CorpusRegistry.GetTask"
	r.mu.RLock()
	defer r.mu.RUnlock()

	task, exists := r.tasks[taskID]
	if !exists {
		return nil, errs.New(errs.CategoryNotFound, "%s: task %q not found", kind, taskID)
	}

	// Defensive copy to prevent caller mutation from altering registry state (INV-04)
	copy := task
	copy.ReadFiles = append([]string(nil), task.ReadFiles...)
	copy.TargetFiles = append([]string(nil), task.TargetFiles...)
	copy.ExpectedMutations = append([]string(nil), task.ExpectedMutations...)
	return &copy, nil
}

// TaskByID is an alias for GetTask.
func (r *CorpusRegistry) TaskByID(taskID string) (*benchmark.BenchmarkTask, error) {
	return r.GetTask(taskID)
}

// ListTasks returns all registered tasks sorted deterministically by TaskID ascending (REQ-05, INV-03).
func (r *CorpusRegistry) ListTasks() []benchmark.BenchmarkTask {
	r.mu.RLock()
	defer r.mu.RUnlock()

	list := make([]benchmark.BenchmarkTask, 0, len(r.tasks))
	for _, task := range r.tasks {
		copy := task
		copy.ReadFiles = append([]string(nil), task.ReadFiles...)
		copy.TargetFiles = append([]string(nil), task.TargetFiles...)
		copy.ExpectedMutations = append([]string(nil), task.ExpectedMutations...)
		list = append(list, copy)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].TaskID < list[j].TaskID
	})
	return list
}

// TasksByWorkload returns tasks matching the specified workload kind, sorted deterministically by TaskID (REQ-05, INV-03).
func (r *CorpusRegistry) TasksByWorkload(w protocol.WorkloadKind) []benchmark.BenchmarkTask {
	r.mu.RLock()
	defer r.mu.RUnlock()

	list := make([]benchmark.BenchmarkTask, 0)
	for _, task := range r.tasks {
		if task.WorkloadKind == w {
			copy := task
			copy.ReadFiles = append([]string(nil), task.ReadFiles...)
			copy.TargetFiles = append([]string(nil), task.TargetFiles...)
			copy.ExpectedMutations = append([]string(nil), task.ExpectedMutations...)
			list = append(list, copy)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].TaskID < list[j].TaskID
	})
	return list
}

// TasksByComplexity returns tasks matching the specified complexity string, sorted by TaskID.
func (r *CorpusRegistry) TasksByComplexity(complexity string) []benchmark.BenchmarkTask {
	r.mu.RLock()
	defer r.mu.RUnlock()

	list := make([]benchmark.BenchmarkTask, 0)
	for _, task := range r.tasks {
		if task.Complexity == complexity {
			copy := task
			copy.ReadFiles = append([]string(nil), task.ReadFiles...)
			copy.TargetFiles = append([]string(nil), task.TargetFiles...)
			copy.ExpectedMutations = append([]string(nil), task.ExpectedMutations...)
			list = append(list, copy)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].TaskID < list[j].TaskID
	})
	return list
}

// Count returns the total number of registered tasks in the registry.
func (r *CorpusRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tasks)
}

// AssociateDefect associates a calibrated seeded defect with a registered task (REQ-06).
func (r *CorpusRegistry) AssociateDefect(taskID string, defect benchmark.SeededDefect) error {
	const kind = "CorpusRegistry.AssociateDefect"
	if strings.TrimSpace(taskID) == "" {
		return errs.Wrap(errs.CategoryInvalidArgument, ErrCorpusValidation, "%s: taskID cannot be empty", kind)
	}
	if err := ValidateDefect(defect); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tasks[taskID]; !exists {
		return errs.New(errs.CategoryNotFound, "%s: task %q not found", kind, taskID)
	}

	r.defects[taskID] = append(r.defects[taskID], defect)
	return nil
}

// GetDefectsForTask returns a copy of associated defects for a given task, or an empty slice if none or unknown (REQ-06).
func (r *CorpusRegistry) GetDefectsForTask(taskID string) []benchmark.SeededDefect {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defects, exists := r.defects[taskID]
	if !exists || len(defects) == 0 {
		return []benchmark.SeededDefect{}
	}
	result := make([]benchmark.SeededDefect, len(defects))
	copy(result, defects)
	return result
}

// ListDefects returns all unique seeded defects registered across all tasks, sorted by DefectID.
func (r *CorpusRegistry) ListDefects() []benchmark.SeededDefect {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]benchmark.SeededDefect)
	for _, defects := range r.defects {
		for _, d := range defects {
			seen[d.DefectID] = d
		}
	}

	list := make([]benchmark.SeededDefect, 0, len(seen))
	for _, d := range seen {
		list = append(list, d)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].DefectID < list[j].DefectID
	})
	return list
}

// DefectByID returns a defensive copy of a defect by its ID across all associated task defects.
func (r *CorpusRegistry) DefectByID(defectID string) (*benchmark.SeededDefect, error) {
	const kind = "CorpusRegistry.DefectByID"
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, defects := range r.defects {
		for _, d := range defects {
			if d.DefectID == defectID {
				copy := d
				return &copy, nil
			}
		}
	}
	return nil, errs.New(errs.CategoryNotFound, "%s: defect %q not found", kind, defectID)
}

// DefectsByCategory returns all unique defects matching the given category (matching direct or canonical category).
func (r *CorpusRegistry) DefectsByCategory(cat benchmark.DefectCategory) []benchmark.SeededDefect {
	r.mu.RLock()
	defer r.mu.RUnlock()

	canonicalTarget := cat.Canonical()
	seen := make(map[string]benchmark.SeededDefect)
	for _, defects := range r.defects {
		for _, d := range defects {
			if d.Category == cat || d.Category.Canonical() == canonicalTarget || (cat == "state_corruption" && d.ViolatedInvariant == "INV-LEASE-INTEGRITY") {
				seen[d.DefectID] = d
			}
		}
	}

	list := make([]benchmark.SeededDefect, 0, len(seen))
	for _, d := range seen {
		list = append(list, d)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].DefectID < list[j].DefectID
	})
	return list
}

// DefectsBySeverity returns all unique defects matching the given severity, sorted by DefectID.
func (r *CorpusRegistry) DefectsBySeverity(sev string) []benchmark.SeededDefect {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]benchmark.SeededDefect)
	for _, defects := range r.defects {
		for _, d := range defects {
			if d.Severity == sev {
				seen[d.DefectID] = d
			}
		}
	}

	list := make([]benchmark.SeededDefect, 0, len(seen))
	for _, d := range seen {
		list = append(list, d)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].DefectID < list[j].DefectID
	})
	return list
}

// DefectCount returns the count of unique seeded defects associated in the registry.
func (r *CorpusRegistry) DefectCount() int {
	return len(r.ListDefects())
}

// Digest computes a deterministic sha256 hex digest across all registered tasks and defects (INV-01).
func (r *CorpusRegistry) Digest() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tasks := make([]benchmark.BenchmarkTask, 0, len(r.tasks))
	for _, task := range r.tasks {
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].TaskID < tasks[j].TaskID
	})

	hasher := sha256.New()
	for _, t := range tasks {
		hasher.Write([]byte(t.Digest() + "\n"))
		defects := r.defects[t.TaskID]
		for _, d := range defects {
			patchHash := sha256.Sum256([]byte(d.PatchContent))
			hasher.Write([]byte(d.DefectID + ":" + string(d.Category) + ":" + d.FileTarget + ":" + d.ViolatedInvariant + ":" + hex.EncodeToString(patchHash[:]) + "\n"))
		}
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil))
}

// DefectRegistry provides standalone defect management and indexing.
type DefectRegistry struct {
	mu      sync.RWMutex
	defects map[string]benchmark.SeededDefect
}

// NewDefectRegistry initializes an empty DefectRegistry.
func NewDefectRegistry() *DefectRegistry {
	return &DefectRegistry{
		defects: make(map[string]benchmark.SeededDefect),
	}
}

// RegisterDefect registers a defect in the defect registry.
func (dr *DefectRegistry) RegisterDefect(defect benchmark.SeededDefect) error {
	const kind = "DefectRegistry.RegisterDefect"
	if err := ValidateDefect(defect); err != nil {
		return err
	}

	dr.mu.Lock()
	defer dr.mu.Unlock()

	if _, exists := dr.defects[defect.DefectID]; exists {
		return errs.New(errs.CategoryConflict, "%s: defect %q already registered", kind, defect.DefectID)
	}
	dr.defects[defect.DefectID] = defect
	return nil
}

// DefectByID retrieves a defensive copy of a defect by ID.
func (dr *DefectRegistry) DefectByID(defectID string) (*benchmark.SeededDefect, error) {
	const kind = "DefectRegistry.DefectByID"
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	d, exists := dr.defects[defectID]
	if !exists {
		return nil, errs.New(errs.CategoryNotFound, "%s: defect %q not found", kind, defectID)
	}
	copy := d
	return &copy, nil
}

// DefectsByCategory returns all defects matching the given category.
func (dr *DefectRegistry) DefectsByCategory(cat benchmark.DefectCategory) []benchmark.SeededDefect {
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	canonicalTarget := cat.Canonical()
	list := make([]benchmark.SeededDefect, 0)
	for _, d := range dr.defects {
		if d.Category == cat || d.Category.Canonical() == canonicalTarget || (cat == "state_corruption" && d.ViolatedInvariant == "INV-LEASE-INTEGRITY") {
			list = append(list, d)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].DefectID < list[j].DefectID
	})
	return list
}

// DefectsBySeverity returns all defects matching the given severity.
func (dr *DefectRegistry) DefectsBySeverity(sev string) []benchmark.SeededDefect {
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	list := make([]benchmark.SeededDefect, 0)
	for _, d := range dr.defects {
		if d.Severity == sev {
			list = append(list, d)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].DefectID < list[j].DefectID
	})
	return list
}

// ListDefects returns all defects sorted by DefectID.
func (dr *DefectRegistry) ListDefects() []benchmark.SeededDefect {
	dr.mu.RLock()
	defer dr.mu.RUnlock()

	list := make([]benchmark.SeededDefect, 0, len(dr.defects))
	for _, d := range dr.defects {
		list = append(list, d)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].DefectID < list[j].DefectID
	})
	return list
}

// Count returns the number of defects registered.
func (dr *DefectRegistry) Count() int {
	dr.mu.RLock()
	defer dr.mu.RUnlock()
	return len(dr.defects)
}

// ValidateDefect validates the given defect.
func (dr *DefectRegistry) ValidateDefect(defect benchmark.SeededDefect) error {
	return ValidateDefect(defect)
}
