package compaction

import (
	"context"
	"math"

	"github.com/olostan/DevCadence/internal/artifacts"
	"github.com/olostan/DevCadence/internal/errs"
)

// TokenBudget configures endpoint token limits for admission (ADR-0016).
type TokenBudget struct {
	// MaxRequestTokens (C) is the endpoint's verified/configured request token ceiling.
	// Arbitrary guessing or defaulting is forbidden.
	MaxRequestTokens int
	MaxOutputTokens  int
	SafetyMarginRate float64 // Default 0.10 (10%)
}

// ReserveTokens calculates T_reserve = MaxOutputTokens + safety margin.
func (b TokenBudget) ReserveTokens() int {
	margin := b.SafetyMarginRate
	if margin <= 0 {
		margin = 0.10
	}
	reserve := float64(b.MaxOutputTokens) * (1.0 + margin)
	return int(math.Ceil(reserve))
}

// AdmissionState tracks state across turns to enforce rearming rules.
type AdmissionState struct {
	TurnIndex      int
	Tier1Exhausted bool
}

// TokenCounter calculates token counts for strings and sessions.
type TokenCounter interface {
	CountString(text string) int
	CountSession(session *ExecutionSession) int
}

// SimpleTokenCounter estimates tokens using roughly 4 characters per token.
type SimpleTokenCounter struct{}

func (s SimpleTokenCounter) CountString(text string) int {
	if len(text) == 0 {
		return 0
	}
	return (len(text) + 3) / 4
}

func (s SimpleTokenCounter) CountSession(session *ExecutionSession) int {
	if session == nil {
		return 0
	}
	tokens := s.CountString(session.SystemPrompt)
	tokens += s.CountString(session.Assignment.Title + session.Assignment.Objective)
	for _, inv := range session.Assignment.Invariants {
		tokens += s.CountString(inv)
	}
	for _, c := range session.Assignment.Constraints {
		tokens += s.CountString(c)
	}
	for _, am := range session.Assignment.Amendments {
		tokens += s.CountString(am.Directive)
	}
	for _, op := range session.ActiveOperationIDs {
		tokens += s.CountString(op)
	}
	tokens += s.CountString(session.Checkpoint.CurrentTreeSHA + session.Checkpoint.BaseCommit + session.Checkpoint.HeadCommit)
	for _, f := range session.Checkpoint.StagedFiles {
		tokens += s.CountString(f)
	}
	for _, f := range session.Checkpoint.UnstagedFiles {
		tokens += s.CountString(f)
	}
	for _, f := range session.Checkpoint.UntrackedFiles {
		tokens += s.CountString(f)
	}
	for _, te := range session.Checkpoint.TestEvidence {
		tokens += s.CountString(te.CheckID + te.TreeSHA)
	}
	for _, msg := range session.Messages {
		tokens += s.CountString(string(msg.Role)) + s.CountString(msg.Content)
		for _, tc := range msg.ToolCalls {
			tokens += s.CountString(tc.Name) + s.CountString(tc.Arguments)
		}
	}
	return tokens
}

// AdmitOrCompact executes the multi-tier context compaction state machine (ADR-0016).
func AdmitOrCompact(
	ctx context.Context,
	session *ExecutionSession,
	budget TokenBudget,
	state *AdmissionState,
	store *artifacts.Store,
	summarizer Summarizer,
	counter TokenCounter,
) error {
	if counter == nil {
		counter = SimpleTokenCounter{}
	}
	if state == nil {
		state = &AdmissionState{}
	}

	C := budget.MaxRequestTokens
	if C <= 0 {
		return errs.New(errs.CategoryUnsupported,
			"admission: endpoint context limit (C) is unknown or unprovided; arbitrary defaults forbidden (ADR-0016)")
	}

	calcTotal := func() int {
		tin := counter.CountSession(session)
		return tin + budget.ReserveTokens()
	}

	Ttotal := calcTotal()

	// Soft watermark: 75% of C
	softWatermark := int(0.75 * float64(C))
	// Reset watermark: 65% of C
	resetWatermark := int(0.65 * float64(C))
	// Hard watermark: 85% of C
	hardWatermark := int(0.85 * float64(C))

	if Ttotal <= resetWatermark {
		state.Tier1Exhausted = false
	} else if Ttotal > softWatermark {
		// Tier 1: Deterministic Tool Pruning
		if !state.Tier1Exhausted {
			prevTotal := Ttotal
			_, _ = PruneToolResults(ctx, session, store)
			Ttotal = calcTotal()

			reduction := 0.0
			if prevTotal > 0 {
				reduction = float64(prevTotal-Ttotal) / float64(prevTotal)
			}
			if reduction < 0.05 {
				state.Tier1Exhausted = true
			}
		}

		if Ttotal <= resetWatermark {
			// Below reset watermark: rearm Tier 1
			state.Tier1Exhausted = false
		} else if Ttotal > hardWatermark || Ttotal > C {
			// Tier 2: Episodic Trajectory Summarization
			if summarizer != nil {
				applyTier2Summarization(ctx, session, summarizer, store, C)
				Ttotal = calcTotal()
			}
		}
	}

	// Universal Final Admission Check (mandatory on EVERY path)
	if Ttotal > C {
		return errs.New(errs.CategoryPolicyDenied,
			"context_budget_exceeded: total serialized load %d tokens exceeds endpoint limit %d (ADR-0016)",
			Ttotal, C)
	}

	return nil
}

func applyTier2Summarization(ctx context.Context, session *ExecutionSession, summarizer Summarizer, store *artifacts.Store, C int) {
	if session == nil || len(session.Messages) < 4 {
		return
	}

	targetCutoff := len(session.Messages) - 2
	cutoff := findSafeCutoff(session.Messages, targetCutoff)
	if cutoff <= 0 {
		return
	}

	// Build tool group membership and propagate Protected status across entire groups.
	// Assistant message with ToolCalls and all corresponding RoleTool messages belong to the same group.
	toolCallToGroup := make(map[string]int)
	groupProtected := make(map[int]bool)
	msgGroup := make([]int, len(session.Messages))
	for i := range msgGroup {
		msgGroup[i] = -1
	}

	nextGroupID := 0
	for i, msg := range session.Messages {
		if msg.Role == RoleAssistant && len(msg.ToolCalls) > 0 {
			gid := nextGroupID
			nextGroupID++
			msgGroup[i] = gid
			for _, tc := range msg.ToolCalls {
				toolCallToGroup[tc.ID] = gid
			}
			if msg.Protected {
				groupProtected[gid] = true
			}
		} else if msg.Role == RoleTool && msg.ToolCallID != "" {
			if gid, ok := toolCallToGroup[msg.ToolCallID]; ok {
				msgGroup[i] = gid
				if msg.Protected {
					groupProtected[gid] = true
				}
			}
		}
	}

	// Propagate protection: if any member of a group is protected, all members are protected
	messages := make([]Message, len(session.Messages))
	copy(messages, session.Messages)
	for i := range messages {
		gid := msgGroup[i]
		if gid >= 0 && groupProtected[gid] {
			messages[i].Protected = true
		}
	}

	// If any member of a tool group is retained in remaining (past cutoff or protected),
	// the entire group must stay in remaining to avoid splitting.
	groupInRemaining := make(map[int]bool)
	for i, msg := range messages {
		if msg.IsDigest {
			continue
		}
		gid := msgGroup[i]
		if gid >= 0 && (i >= cutoff || msg.Protected) {
			groupInRemaining[gid] = true
		}
	}

	// Collect older non-protected messages to summarize
	var toSummarize []Message
	var remaining []Message

	for i, msg := range messages {
		if msg.IsDigest {
			// Replace earlier generated digests instead of duplicating
			continue
		}
		gid := msgGroup[i]
		if gid >= 0 && groupInRemaining[gid] {
			remaining = append(remaining, msg)
			continue
		}
		if i < cutoff {
			if !msg.Protected {
				toSummarize = append(toSummarize, msg)
			} else {
				remaining = append(remaining, msg)
			}
		} else {
			remaining = append(remaining, msg)
		}
	}

	if len(toSummarize) == 0 {
		return
	}

	if err := ValidateAtomicToolGroups(remaining); err != nil {
		// If splitting would orphan tool calls/results, abort Tier 2
		return
	}

	input := DigestInput{
		Session:             session,
		MessagesToSummarize: toSummarize,
	}

	digest, err := summarizer.Summarize(ctx, session.Policy, input, C/4)
	if err != nil {
		// If summarizer fails or is denied by policy, leave session intact and let final guard escalate
		return
	}

	// Validate evidence references and decision authority against authoritative store
	var knownDecisions map[string]bool
	if session.KnownDecisionIDs != nil {
		knownDecisions = session.KnownDecisionIDs
	}
	digest.ValidateEvidenceAndAuthority(store, knownDecisions)

	session.Digest = &digest
	digestMsg := digest.FormatAsUserMessage()

	// Reconstruct messages: single canonical digest first, then remaining
	newMessages := make([]Message, 0, len(remaining)+1)
	newMessages = append(newMessages, digestMsg)
	newMessages = append(newMessages, remaining...)

	if err := ValidateAtomicToolGroups(newMessages); err != nil {
		return
	}

	// Verify that every completed tool result present before compaction is still present
	// for any retained assistant tool call
	originalResults := make(map[string]bool)
	for _, m := range session.Messages {
		if m.Role == RoleTool && m.ToolCallID != "" {
			originalResults[m.ToolCallID] = true
		}
	}
	newCalls := make(map[string]bool)
	newResults := make(map[string]bool)
	for _, m := range newMessages {
		if m.Role == RoleAssistant {
			for _, tc := range m.ToolCalls {
				newCalls[tc.ID] = true
			}
		} else if m.Role == RoleTool && m.ToolCallID != "" {
			newResults[m.ToolCallID] = true
		}
	}
	for callID := range newCalls {
		if originalResults[callID] && !newResults[callID] {
			return
		}
	}

	session.Messages = newMessages
}

func findSafeCutoff(messages []Message, target int) int {
	if target <= 0 {
		return 0
	}
	if target >= len(messages) {
		return len(messages)
	}

	toolOwner := make(map[string]int)
	groupEnd := make(map[int]int)

	for i, msg := range messages {
		if msg.Role == RoleAssistant && len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				toolOwner[tc.ID] = i
			}
			groupEnd[i] = i
		} else if msg.Role == RoleTool && msg.ToolCallID != "" {
			if ownerIdx, ok := toolOwner[msg.ToolCallID]; ok {
				if i > groupEnd[ownerIdx] {
					groupEnd[ownerIdx] = i
				}
			}
		}
	}

	// If target falls strictly inside [ownerIdx, groupEnd[ownerIdx]],
	// shift target to ownerIdx so the entire group moves together.
	cutoff := target
	for ownerIdx, endIdx := range groupEnd {
		if cutoff > ownerIdx && cutoff <= endIdx {
			cutoff = ownerIdx
		}
	}

	return cutoff
}
