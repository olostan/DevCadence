package compaction

import (
	"context"
	"fmt"
	"sort"

	"github.com/olostan/DevCadence/internal/artifacts"
)

const PrunedOutputTemplate = "[Pruned tool output. Full content preserved in artifact: %s. Use fetch_content to read.]"

// SortAmendments sorts amendments by AuthorityLevel descending, then JournalSeq ascending (ADR-0016).
func SortAmendments(amendments []Amendment) {
	sort.SliceStable(amendments, func(i, j int) bool {
		if amendments[i].AuthorityLevel != amendments[j].AuthorityLevel {
			return amendments[i].AuthorityLevel > amendments[j].AuthorityLevel
		}
		return amendments[i].JournalSeq < amendments[j].JournalSeq
	})
}

// PruneToolResults replaces older unpruned tool result messages with artifact references.
// It strictly preserves atomic tool-call/result groups and never prunes protected messages.
func PruneToolResults(ctx context.Context, session *ExecutionSession, store *artifacts.Store) (int, error) {
	if session == nil {
		return 0, nil
	}

	prunedCount := 0
	projectID := session.Policy.ProjectID
	if projectID == "" {
		projectID = "default"
	}

	// Identify the most recent turns (last 2 turns or last turn) to keep unpruned if feasible,
	// while pruning older tool results.
	msgCount := len(session.Messages)
	cutoffIndex := msgCount - 2
	if cutoffIndex < 0 {
		cutoffIndex = 0
	}

	for i := 0; i < cutoffIndex; i++ {
		msg := &session.Messages[i]
		if msg.Protected {
			continue
		}
		if msg.Role != RoleTool || msg.IsPruned {
			continue
		}
		if len(msg.Content) < 100 {
			// Don't prune trivial stubs
			continue
		}

		ref := msg.ContentRef
		if ref == "" {
			if store == nil {
				// Cannot safely prune without store; preserve original content intact
				continue
			}
			putRes, err := store.PutBytes(ctx, projectID, "pruned_tool", "text/plain", []byte(msg.Content), 0)
			if err != nil {
				// Storage failure; leave message unpruned to avoid data loss
				continue
			}
			ref = putRes.Ref.Locator
			msg.ContentRef = ref
		}

		msg.Content = fmt.Sprintf(PrunedOutputTemplate, ref)
		msg.IsPruned = true
		prunedCount++
	}

	return prunedCount, nil
}

// ValidateAtomicToolGroups verifies that an assistant message with tool calls has all
// its matching tool results present, and that no tool result is orphaned without its assistant message.
// It checks both directions:
// 1. Every tool result must have a corresponding assistant tool call.
// 2. Every assistant tool call must have its matching tool result, unless it is a legitimately
//    pending call at the end of the message sequence (no subsequent user/assistant turns).
func ValidateAtomicToolGroups(messages []Message) error {
	assistantToolCalls := make(map[string]bool)
	toolResults := make(map[string]bool)

	for _, msg := range messages {
		if msg.Role == RoleAssistant {
			for _, tc := range msg.ToolCalls {
				assistantToolCalls[tc.ID] = true
			}
		} else if msg.Role == RoleTool {
			if msg.ToolCallID != "" {
				toolResults[msg.ToolCallID] = true
			}
		}
	}

	// 1. Every tool result must have an assistant tool call
	for resID := range toolResults {
		if !assistantToolCalls[resID] {
			return fmt.Errorf("orphaned tool result %q without matching assistant tool call", resID)
		}
	}

	// 2. Every assistant tool call must have its matching result unless it is legitimately pending at the end
	var lastAssistantWithCallsIdx = -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == RoleAssistant && len(messages[i].ToolCalls) > 0 {
			lastAssistantWithCallsIdx = i
			break
		}
	}

	for i, msg := range messages {
		if msg.Role == RoleAssistant {
			for _, tc := range msg.ToolCalls {
				if !toolResults[tc.ID] {
					isPendingAtEnd := (i == lastAssistantWithCallsIdx)
					if isPendingAtEnd {
						for j := i + 1; j < len(messages); j++ {
							if messages[j].Role == RoleUser || messages[j].Role == RoleAssistant {
								isPendingAtEnd = false
								break
							}
						}
					}
					if !isPendingAtEnd {
						return fmt.Errorf("assistant tool call %q missing matching completed tool result", tc.ID)
					}
				}
			}
		}
	}

	return nil
}
