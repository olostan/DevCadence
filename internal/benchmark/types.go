package benchmark

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/protocol"
)

// ContextStrategyKind identifies the context construction strategy (REQ-01).
type ContextStrategyKind string

const (
	StrategyFullHistory  ContextStrategyKind = "full_history"
	StrategyCompacted    ContextStrategyKind = "compacted"
	StrategySnippetPool  ContextStrategyKind = "snippet_pool"
	StrategyHybrid4Layer ContextStrategyKind = "hybrid_4layer"
)

// Valid reports whether the strategy kind is known (REQ-01).
func (k ContextStrategyKind) Valid() bool {
	switch k {
	case StrategyFullHistory, StrategyCompacted, StrategySnippetPool, StrategyHybrid4Layer:
		return true
	default:
		return false
	}
}

// BenchmarkTask defines an engineering task executed by the benchmark harness (REQ-02, INV-01).
type BenchmarkTask struct {
	TaskID            string   `json:"task_id"`
	Name              string   `json:"name"`
	WorkPackageID     string   `json:"work_package_id"`
	Contract          string   `json:"contract"`
	ReadFiles         []string `json:"read_files"`
	TargetFiles       []string `json:"target_files"`
	ExpectedMutations []string `json:"expected_mutations"`
}

// benchmarkTaskDigestView provides deterministic canonical JSON representation for Digest.
type benchmarkTaskDigestView struct {
	Contract          string   `json:"contract"`
	ExpectedMutations []string `json:"expected_mutations"`
	Name              string   `json:"name"`
	ReadFiles         []string `json:"read_files"`
	TargetFiles       []string `json:"target_files"`
	TaskID            string   `json:"task_id"`
	WorkPackageID     string   `json:"work_package_id"`
}

// Digest computes a deterministic sha256 hex digest of all task fields (REQ-02, INV-01).
func (t BenchmarkTask) Digest() string {
	view := benchmarkTaskDigestView{
		TaskID:            t.TaskID,
		Name:              t.Name,
		WorkPackageID:     t.WorkPackageID,
		Contract:          t.Contract,
		ReadFiles:         t.ReadFiles,
		TargetFiles:       t.TargetFiles,
		ExpectedMutations: t.ExpectedMutations,
	}
	if view.ReadFiles == nil {
		view.ReadFiles = []string{}
	}
	if view.TargetFiles == nil {
		view.TargetFiles = []string{}
	}
	if view.ExpectedMutations == nil {
		view.ExpectedMutations = []string{}
	}

	canonical, err := protocol.CanonicalJSON(&view)
	if err != nil {
		hasher := sha256.New()
		hasher.Write([]byte(t.TaskID + ":" + t.Name + ":" + t.WorkPackageID + ":" + t.Contract))
		return "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	}
	return protocol.DigestBytes(canonical)
}

// DefectCategory classifies a seeded benchmark defect (REQ-03).
type DefectCategory string

const (
	DefectInvariantViolation DefectCategory = "invariant_violation"
	DefectAPIMutation        DefectCategory = "api_mutation"
	DefectBoundaryViolation  DefectCategory = "boundary_violation"
)

// Valid reports whether the defect category is known (REQ-03).
func (c DefectCategory) Valid() bool {
	switch c {
	case DefectInvariantViolation, DefectAPIMutation, DefectBoundaryViolation:
		return true
	default:
		return false
	}
}

// SeededDefect represents a known defect injected into the benchmark test environment (REQ-03, INV-01).
type SeededDefect struct {
	DefectID          string         `json:"defect_id"`
	Category          DefectCategory `json:"category"`
	Description       string         `json:"description"`
	FileTarget        string         `json:"file_target"`
	PatchContent      string         `json:"patch_content"`
	ViolatedInvariant string         `json:"violated_invariant"`
}

// BenchmarkTurnRecord captures prompt and model execution outcome for a single turn (REQ-04).
type BenchmarkTurnRecord struct {
	Turn   int                `json:"turn"`
	Prompt string             `json:"prompt"`
	Result drivers.TurnResult `json:"result"`
}

// BenchmarkSession encapsulates state, driver, and transcript for a benchmark execution (REQ-04).
type BenchmarkSession struct {
	SessionID    string                `json:"session_id"`
	Task         BenchmarkTask         `json:"task"`
	Strategy     ContextStrategyKind   `json:"strategy"`
	Driver       drivers.Session       `json:"-"`
	Defect       *SeededDefect         `json:"defect,omitempty"`
	Turns        []BenchmarkTurnRecord `json:"turns"`
	WorktreeDir  string                `json:"worktree_dir"`
	CompiledPack *protocol.ContextPack `json:"compiled_pack,omitempty"`
}

// StrategyContextBuilder constructs turn prompts tailored to a specific strategy (REQ-05).
type StrategyContextBuilder interface {
	BuildTurnPrompt(ctx context.Context, session *BenchmarkSession, turn int, lastResult *drivers.TurnResult) (string, error)
}

// CompactedStrategyConfig configures Strategy 2 (Compacted) (REQ-05).
type CompactedStrategyConfig struct {
	MaxHistoryTurns int `json:"max_history_turns"`
}

// SnippetPoolStrategyConfig configures Strategy 3 (Snippet Pool) (REQ-05).
type SnippetPoolStrategyConfig struct {
	MaxSnippetTokens int `json:"max_snippet_tokens"`
}

// RunStatus tracks the execution status of a benchmark run (REQ-10).
type RunStatus string

const (
	RunStatusCompleted    RunStatus = "completed"
	RunStatusFailed       RunStatus = "failed"
	RunStatusContextUnfit RunStatus = "context_unfit"
)

// Valid reports whether the run status is known (REQ-10).
func (s RunStatus) Valid() bool {
	switch s {
	case RunStatusCompleted, RunStatusFailed, RunStatusContextUnfit:
		return true
	default:
		return false
	}
}

// DefectStatus records the deterministic outcome of defect detection/prevention (REQ-11).
type DefectStatus string

const (
	DefectDetected      DefectStatus = "detected"
	DefectPrevented     DefectStatus = "prevented"
	DefectMissed        DefectStatus = "missed"
	DefectIntroduced    DefectStatus = "introduced"
	DefectNotApplicable DefectStatus = "not_applicable"
)

// Valid reports whether the defect status is known (REQ-11).
func (s DefectStatus) Valid() bool {
	switch s {
	case DefectDetected, DefectPrevented, DefectMissed, DefectIntroduced, DefectNotApplicable:
		return true
	default:
		return false
	}
}

// BenchmarkRunResult captures the complete metrics, wall time, and defect outcome of a run (REQ-10).
type BenchmarkRunResult struct {
	RunID                 string              `json:"run_id"`
	TaskID                string              `json:"task_id"`
	Strategy              ContextStrategyKind `json:"strategy"`
	Status                RunStatus           `json:"status"`
	Passed                bool                `json:"passed"`
	TurnsExecuted         int                 `json:"turns_executed"`
	DefectStatus          DefectStatus        `json:"defect_status"`
	TokenUsage            drivers.TokenUsage  `json:"token_usage"`
	InitialTokens         int64               `json:"initial_tokens"`
	PeakResidentTokens    int64               `json:"peak_resident_tokens"`
	CumulativeInputTokens int64               `json:"cumulative_input_tokens"`
	Duration              time.Duration       `json:"duration"`
	AccountingUncertain   bool                `json:"accounting_uncertain"`
	Err                   error               `json:"err,omitempty"`
}
