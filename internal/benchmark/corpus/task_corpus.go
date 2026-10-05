package corpus

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	benchmarkfixtures "github.com/olostan/DevCadence/fixtures/benchmark"
	"github.com/olostan/DevCadence/internal/benchmark"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// DefaultTaskDefectMapping defines the canonical association between tasks and seeded defects.
var DefaultTaskDefectMapping = map[string][]string{
	"task-nav-01":  {"DEFECT-INV-03"},
	"task-nav-02":  {"DEFECT-INV-01"},
	"task-impl-01": {"DEFECT-INV-02"},
	"task-impl-02": {"DEFECT-API-03"},
	"task-impl-03": {"DEFECT-INV-04"},
	"task-impl-04": {"DEFECT-API-01", "DEFECT-API-02"},
	"task-rev-01":  {"DEFECT-API-04"},
	"task-rev-02":  {"DEFECT-BOUND-01", "DEFECT-BOUND-02"},
	"task-arch-01": {"DEFECT-BOUND-03"},
	"task-arch-02": {"DEFECT-BOUND-04"},
}

// LoadDefaultCorpus loads the canonical benchmark task corpus and associated defects from embedded fixtures (REQ-07).
func LoadDefaultCorpus() (*CorpusRegistry, error) {
	const kind = "LoadDefaultCorpus"
	registry := NewCorpusRegistry()

	// 1. Load tasks from embedded tasks/*.json
	taskFiles, err := fs.Glob(benchmarkfixtures.FS, "tasks/*.json")
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "%s: failed to glob embedded task fixtures", kind)
	}
	if len(taskFiles) == 0 {
		return nil, errs.New(errs.CategoryNotFound, "%s: no embedded task fixtures found", kind)
	}
	sort.Strings(taskFiles)

	for _, path := range taskFiles {
		data, err := benchmarkfixtures.FS.ReadFile(path)
		if err != nil {
			return nil, errs.Wrap(errs.CategoryInternal, err, "%s: failed to read task fixture %q", kind, path)
		}
		var task benchmark.BenchmarkTask
		if err := json.Unmarshal(data, &task); err != nil {
			return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "%s: malformed task JSON in %q", kind, path)
		}
		if err := registry.RegisterTask(task); err != nil {
			return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "%s: failed to register task %q", kind, task.TaskID)
		}
	}

	// 2. Load defects from embedded seed_defects/*.json
	defectFiles, err := fs.Glob(benchmarkfixtures.FS, "seed_defects/*.json")
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "%s: failed to glob embedded defect fixtures", kind)
	}
	sort.Strings(defectFiles)

	defectMap := make(map[string]benchmark.SeededDefect)
	for _, path := range defectFiles {
		data, err := benchmarkfixtures.FS.ReadFile(path)
		if err != nil {
			return nil, errs.Wrap(errs.CategoryInternal, err, "%s: failed to read defect fixture %q", kind, path)
		}
		var defect benchmark.SeededDefect
		if err := json.Unmarshal(data, &defect); err != nil {
			return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "%s: malformed defect JSON in %q", kind, path)
		}
		defectMap[defect.DefectID] = defect
	}

	// 3. Associate defects to tasks according to canonical mapping
	for taskID, defectIDs := range DefaultTaskDefectMapping {
		for _, defectID := range defectIDs {
			defect, ok := defectMap[defectID]
			if !ok {
				return nil, errs.New(errs.CategoryNotFound, "%s: defect %q required for task %q not found in fixtures", kind, defectID, taskID)
			}
			if err := registry.AssociateDefect(taskID, defect); err != nil {
				return nil, errs.Wrap(errs.CategoryInternal, err, "%s: failed to associate defect %q to task %q", kind, defectID, taskID)
			}
		}
	}

	// 4. Verify all tasks have valid sha256 digests and non-empty contracts (INV-01, REQ-03)
	for _, task := range registry.ListTasks() {
		digest := task.Digest()
		if !strings.HasPrefix(digest, "sha256:") || len(digest) != 71 {
			return nil, errs.New(errs.CategoryIntegrity, "%s: task %q has invalid digest %q", kind, task.TaskID, digest)
		}
		if strings.TrimSpace(task.Contract) == "" {
			return nil, errs.New(errs.CategoryIntegrity, "%s: task %q has empty contract", kind, task.TaskID)
		}
	}

	// 5. Verify all four required workloads are represented (ACC-01, REQ-02)
	workloads := []protocol.WorkloadKind{
		protocol.WorkloadNavigation,
		protocol.WorkloadImplementation,
		protocol.WorkloadReview,
		protocol.WorkloadArchitecture,
	}
	for _, w := range workloads {
		tasks := registry.TasksByWorkload(w)
		if len(tasks) == 0 {
			return nil, errs.New(errs.CategoryIntegrity, "%s: missing required workload %q in default corpus", kind, w)
		}
	}

	return registry, nil
}

// LoadDefaultDefectRegistry loads all embedded defects into a standalone DefectRegistry.
func LoadDefaultDefectRegistry() (*DefectRegistry, error) {
	const kind = "LoadDefaultDefectRegistry"
	dr := NewDefectRegistry()

	defectFiles, err := fs.Glob(benchmarkfixtures.FS, "seed_defects/*.json")
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "%s: failed to glob embedded defect fixtures", kind)
	}
	sort.Strings(defectFiles)

	for _, path := range defectFiles {
		data, err := benchmarkfixtures.FS.ReadFile(path)
		if err != nil {
			return nil, errs.Wrap(errs.CategoryInternal, err, "%s: failed to read defect fixture %q", kind, path)
		}
		var defect benchmark.SeededDefect
		if err := json.Unmarshal(data, &defect); err != nil {
			return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "%s: malformed defect JSON in %q", kind, path)
		}
		if err := dr.RegisterDefect(defect); err != nil {
			return nil, errs.Wrap(errs.CategoryInternal, err, "%s: failed to register defect %q", kind, defect.DefectID)
		}
	}
	return dr, nil
}

// CorpusManifest represents the complete inventory and deterministic digest of the benchmark corpus.
type CorpusManifest struct {
	Digest      string                    `json:"digest"`
	TaskCount   int                       `json:"task_count"`
	DefectCount int                       `json:"defect_count"`
	Tasks       []benchmark.BenchmarkTask `json:"tasks"`
	Defects     []benchmark.SeededDefect  `json:"defects"`
}

// LoadCorpusManifest loads the default corpus and constructs a verified manifest.
func LoadCorpusManifest() (*CorpusManifest, error) {
	reg, err := LoadDefaultCorpus()
	if err != nil {
		return nil, err
	}
	tasks := reg.ListTasks()
	defects := reg.ListDefects()
	return &CorpusManifest{
		Digest:      reg.Digest(),
		TaskCount:   len(tasks),
		DefectCount: len(defects),
		Tasks:       tasks,
		Defects:     defects,
	}, nil
}

// String returns a human-readable summary of the corpus manifest.
func (m *CorpusManifest) String() string {
	return fmt.Sprintf("CorpusManifest[digest=%s, tasks=%d, defects=%d]", m.Digest, m.TaskCount, m.DefectCount)
}
