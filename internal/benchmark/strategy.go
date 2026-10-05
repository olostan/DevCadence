package benchmark

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// FullHistoryStrategy implements Strategy 1: Traditional monolithic conversational history (REQ-07).
type FullHistoryStrategy struct{}

// NewFullHistoryStrategy constructs a Strategy 1 builder.
func NewFullHistoryStrategy() *FullHistoryStrategy {
	return &FullHistoryStrategy{}
}

// BuildTurnPrompt monotonically appends user prompts and assistant responses verbatim (REQ-07).
func (s *FullHistoryStrategy) BuildTurnPrompt(_ context.Context, session *BenchmarkSession, turn int, _ *drivers.TurnResult) (string, error) {
	if session == nil {
		return "", errs.New(errs.CategoryInvalidArgument, "session cannot be nil")
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== System & Task Contract ===\nTask: %s\nID: %s\nContract:\n%s\n\n",
		session.Task.Name, session.Task.TaskID, session.Task.Contract))

	// Monotonically append all historical turns verbatim without truncation
	for _, rec := range session.Turns {
		b.WriteString(fmt.Sprintf("--- User Turn %d ---\n%s\n\n", rec.Turn, rec.Prompt))
		b.WriteString(fmt.Sprintf("--- Assistant Turn %d ---\n%s\n\n", rec.Turn, rec.Result.Content))
	}

	b.WriteString(fmt.Sprintf("--- Current Turn %d Request ---\nPlease proceed with task execution according to contract.", turn))
	return b.String(), nil
}

// CompactedStrategy implements Strategy 2: Multi-Tier sliding-window history compaction (REQ-08).
type CompactedStrategy struct {
	config CompactedStrategyConfig
}

// NewCompactedStrategy constructs a Strategy 2 builder.
func NewCompactedStrategy(cfg CompactedStrategyConfig) *CompactedStrategy {
	if cfg.MaxHistoryTurns <= 0 {
		cfg.MaxHistoryTurns = 3
	}
	return &CompactedStrategy{config: cfg}
}

// BuildTurnPrompt applies sliding-window turn compaction when history exceeds MaxHistoryTurns,
// always strictly preserving the role/contract core prefix (REQ-08).
func (s *CompactedStrategy) BuildTurnPrompt(_ context.Context, session *BenchmarkSession, turn int, _ *drivers.TurnResult) (string, error) {
	if session == nil {
		return "", errs.New(errs.CategoryInvalidArgument, "session cannot be nil")
	}

	var b strings.Builder
	// Core role & contract prefix is IMMUTABLY preserved regardless of window compaction
	b.WriteString(fmt.Sprintf("=== Core Role & Execution Contract Prefix ===\nRole: implementer\nTask: %s\nID: %s\nContract:\n%s\n\n",
		session.Task.Name, session.Task.TaskID, session.Task.Contract))

	historyTurns := session.Turns
	if len(historyTurns) > s.config.MaxHistoryTurns {
		// Sliding-window compaction: keep only the most recent MaxHistoryTurns
		startIdx := len(historyTurns) - s.config.MaxHistoryTurns
		b.WriteString(fmt.Sprintf("[Context Compaction Applied: %d earlier turns compacted into summary]\n\n", startIdx))
		historyTurns = historyTurns[startIdx:]
	}

	for _, rec := range historyTurns {
		b.WriteString(fmt.Sprintf("--- User Turn %d ---\n%s\n\n", rec.Turn, rec.Prompt))
		b.WriteString(fmt.Sprintf("--- Assistant Turn %d ---\n%s\n\n", rec.Turn, rec.Result.Content))
	}

	b.WriteString(fmt.Sprintf("--- Current Turn %d Request ---\nPlease continue execution within the declared contract.", turn))
	return b.String(), nil
}

// SnippetPoolStrategy implements Strategy 3: Static prefix + active snippet pool (REQ-09).
type SnippetPoolStrategy struct {
	config SnippetPoolStrategyConfig
}

// NewSnippetPoolStrategy constructs a Strategy 3 builder.
func NewSnippetPoolStrategy(cfg SnippetPoolStrategyConfig) *SnippetPoolStrategy {
	if cfg.MaxSnippetTokens <= 0 {
		cfg.MaxSnippetTokens = 500
	}
	return &SnippetPoolStrategy{config: cfg}
}

// BuildTurnPrompt retains static system prefix plus dynamically selected file/symbol snippets
// bounded by token budget (REQ-09).
func (s *SnippetPoolStrategy) BuildTurnPrompt(_ context.Context, session *BenchmarkSession, turn int, _ *drivers.TurnResult) (string, error) {
	if session == nil {
		return "", errs.New(errs.CategoryInvalidArgument, "session cannot be nil")
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== Static System Prefix ===\nRole: implementer\nTask: %s\nContract:\n%s\n\n",
		session.Task.Name, session.Task.Contract))

	// Active snippet pool bounded by MaxSnippetTokens
	b.WriteString("=== Active Snippet Pool ===\n")
	budget := s.config.MaxSnippetTokens
	usedTokens := 0

	candidateFiles := append([]string{}, session.Task.ReadFiles...)
	candidateFiles = append(candidateFiles, session.Task.TargetFiles...)

	for _, file := range candidateFiles {
		var content string
		if session.WorktreeDir != "" {
			fullPath := filepath.Join(session.WorktreeDir, file)
			if data, err := os.ReadFile(fullPath); err == nil {
				content = string(data)
			}
		}
		if content == "" {
			content = fmt.Sprintf("// Target file declaration: %s", file)
		}

		// Approximate token accounting: ~4 characters per token
		snippetTokens := len(content)/4 + 1
		if usedTokens+snippetTokens > budget {
			b.WriteString(fmt.Sprintf("[Snippet Pool Capacity Reached: omitted %s]\n", file))
			break
		}

		b.WriteString(fmt.Sprintf("--- Snippet [%s] ---\n%s\n\n", file, content))
		usedTokens += snippetTokens
	}

	b.WriteString(fmt.Sprintf("=== Turn %d Execution ===\nPlease continue execution within the declared contract.", turn))
	return b.String(), nil
}

// Hybrid4LayerStrategyConfig configures Strategy 4 (Hybrid 4-Layer).
type Hybrid4LayerStrategyConfig struct {
	Compiler     *compiler.Compiler
	Profile      *protocol.ContextProfile
	BudgetPoolID string
	Renderer     compiler.PromptRenderer
	StateCapsule *protocol.CognitiveStateCapsule
	ActiveLeases []protocol.EvidenceLease
}

// Hybrid4LayerStrategy implements Strategy 4: Protected Core, State Capsule, Evidence Working Set,
// Ephemeral Tail using the Cognitive Invocation Compiler (REQ-06, INV-03).
type Hybrid4LayerStrategy struct {
	config Hybrid4LayerStrategyConfig
}

// NewHybrid4LayerStrategy constructs a Strategy 4 builder.
func NewHybrid4LayerStrategy(cfg Hybrid4LayerStrategyConfig) (*Hybrid4LayerStrategy, error) {
	if cfg.Compiler == nil {
		reg, err := compiler.NewCanonicalRuleRegistry()
		if err != nil {
			return nil, errs.Wrap(errs.CategoryInternal, err, "failed to initialize canonical rule registry")
		}
		comp, err := compiler.NewCompiler(reg, nil, nil)
		if err != nil {
			return nil, errs.Wrap(errs.CategoryInternal, err, "failed to initialize compiler")
		}
		cfg.Compiler = comp
	}

	if cfg.Profile == nil {
		prof, err := compiler.DefaultProvisionalProfile("bench-endpoint", "bench-channel", "bench-model", 65536)
		if err != nil {
			return nil, errs.Wrap(errs.CategoryInternal, err, "failed to initialize default profile")
		}
		cfg.Profile = prof
	}

	if cfg.Renderer == nil {
		cfg.Renderer = compiler.NewTaggedMarkdownRenderer()
	}

	if cfg.BudgetPoolID == "" {
		cfg.BudgetPoolID = "benchmark-default-pool"
	}

	return &Hybrid4LayerStrategy{config: cfg}, nil
}

// BuildTurnPrompt invokes the Cognitive Invocation Compiler to assemble a ContextPack and PromptProjection,
// failing closed with errs.CategoryContextUnfit if profile ceilings are violated (REQ-06, INV-03).
func (s *Hybrid4LayerStrategy) BuildTurnPrompt(ctx context.Context, session *BenchmarkSession, turn int, _ *drivers.TurnResult) (string, error) {
	if session == nil {
		return "", errs.New(errs.CategoryInvalidArgument, "session cannot be nil")
	}

	wpID := session.Task.WorkPackageID
	if strings.TrimSpace(wpID) == "" {
		wpID = "WP-M4-1"
	}

	taskID := session.Task.TaskID
	if strings.TrimSpace(taskID) == "" {
		taskID = "task-bench-001"
	}

	digest := session.Task.Digest()
	if !strings.HasPrefix(digest, "sha256:") || len(digest) != 71 {
		digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	}

	contract := session.Task.Contract
	if strings.TrimSpace(contract) == "" {
		contract = "Execute benchmark task per specification."
	}

	req := compiler.CompileRequest{
		TaskID:               taskID,
		WorkPackageID:        wpID,
		WorkPackageRevision:  1,
		WorkPackageDigest:    digest,
		Role:                 "implementer",
		BaseCommit:           "a457e7d1e28f37c5055d1f51a4da45a4320749a7",
		SourceRevision:       compiler.CanonicalSourceRevision,
		ProjectStateRevision: "rev-001",
		MappingVersion:       compiler.CanonicalMappingRevision,
		BudgetPoolID:         s.config.BudgetPoolID,
		ExecutionContract:    contract,
		ReadEnvelope:         session.Task.ReadFiles,
		WriteScope:           session.Task.TargetFiles,
		Domains:              []string{"cognition"},
		Action:               fmt.Sprintf("Execute benchmark turn %d", turn),
		ContextProfile:       s.config.Profile,
		Renderer:             s.config.Renderer,
	}

	// Turn > 1: feed recent tool exchanges and history into ephemeral tail
	if len(session.Turns) > 0 {
		var toolExchanges []string
		for _, prev := range session.Turns {
			toolExchanges = append(toolExchanges, fmt.Sprintf("Turn %d: %s", prev.Turn, prev.Result.Content))
		}
		req.RecentToolExchanges = toolExchanges
	}

	// If seeded defect specifies violated invariant or rule, pass it as explicit rule to verify compiler admission
	if session.Defect != nil && session.Defect.ViolatedInvariant != "" {
		req.ExplicitRuleIDs = []string{session.Defect.ViolatedInvariant}
	}

	inv, err := s.config.Compiler.CompileInvocation(ctx, req)
	if err != nil {
		// Strict pass-through of compiler errors (CategoryContextUnfit, admission rejection, etc.)
		return "", err
	}

	if inv == nil || inv.Pack == nil {
		return "", errs.New(errs.CategoryInternal, "compiler returned nil invocation or pack")
	}

	// Attach compiled pack to session for deterministic verification
	session.CompiledPack = inv.Pack

	var rendered string
	if inv.Projection.SystemPrompt != "" && inv.Projection.UserPrompt != "" {
		rendered = inv.Projection.SystemPrompt + "\n\n" + inv.Projection.UserPrompt
	} else if inv.Projection.UserPrompt != "" {
		rendered = inv.Projection.UserPrompt
	} else {
		rendered = inv.Projection.SystemPrompt
	}
	return rendered, nil
}
