package compiler

import (
	"fmt"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// TurnPrompts represents the prompts to supply for an individual execution turn.
type TurnPrompts struct {
	SystemPrompt    string `json:"system_prompt,omitempty"`
	UserPrompt      string `json:"user_prompt"`
	RestartRequired bool   `json:"restart_required,omitempty"`
}

// ContextStrategy maps endpoint ContextControl capabilities into concrete turn layouts.
// Per PROTOCOLS §10B and ADR-0019 §1:
// - ExactStateless: rebuilds exact prefix and active leases on each turn; can evict evidence freely.
// - AppendOnly: appends turns; evidence eviction or contract mutation requires restarting or checkpointing.
// - OpaqueSession: scoped initial instructions and checkpoints; cannot remove past tokens directly.
type ContextStrategy interface {
	Control() protocol.ContextControl
	CanEvictEvidence() bool
	RequiresRestartOnEviction() bool
	PrepareTurnPrompt(pack *protocol.ContextPack, projection PromptProjection, turnIndex int, evictionOccurred bool) (TurnPrompts, error)
}

// NewStrategy creates a ContextStrategy based on the provided ContextControl capability.
func NewStrategy(control protocol.ContextControl) (ContextStrategy, error) {
	switch control {
	case protocol.ContextControlExactStateless:
		return &ExactStatelessStrategy{}, nil
	case protocol.ContextControlAppendOnly:
		return &AppendOnlyStrategy{}, nil
	case protocol.ContextControlOpaqueSession:
		return &OpaqueSessionStrategy{}, nil
	default:
		return nil, errs.New(errs.CategoryInvalidArgument, "unsupported context control %q", control)
	}
}

// ExactStatelessStrategy manages endpoints with exact stateless context control.
type ExactStatelessStrategy struct{}

func (s *ExactStatelessStrategy) Control() protocol.ContextControl {
	return protocol.ContextControlExactStateless
}

func (s *ExactStatelessStrategy) CanEvictEvidence() bool {
	return true
}

func (s *ExactStatelessStrategy) RequiresRestartOnEviction() bool {
	return false
}

func (s *ExactStatelessStrategy) PrepareTurnPrompt(pack *protocol.ContextPack, projection PromptProjection, turnIndex int, evictionOccurred bool) (TurnPrompts, error) {
	// Stateless endpoints receive the complete fresh projection on every turn
	return TurnPrompts{
		SystemPrompt:    projection.SystemPrompt,
		UserPrompt:      projection.UserPrompt,
		RestartRequired: false,
	}, nil
}

// AppendOnlyStrategy manages endpoints where past conversation cannot be rewritten or truncated.
type AppendOnlyStrategy struct{}

func (s *AppendOnlyStrategy) Control() protocol.ContextControl {
	return protocol.ContextControlAppendOnly
}

func (s *AppendOnlyStrategy) CanEvictEvidence() bool {
	return false
}

func (s *AppendOnlyStrategy) RequiresRestartOnEviction() bool {
	return true
}

func (s *AppendOnlyStrategy) PrepareTurnPrompt(pack *protocol.ContextPack, projection PromptProjection, turnIndex int, evictionOccurred bool) (TurnPrompts, error) {
	// If evidence eviction or contract re-resolution occurred on an append-only driver,
	// past context in the driver holds stale/invalid evidence, requiring session restart
	if turnIndex > 0 && evictionOccurred {
		return TurnPrompts{
			SystemPrompt:    projection.SystemPrompt,
			UserPrompt:      projection.UserPrompt,
			RestartRequired: true,
		}, nil
	}

	if turnIndex == 0 {
		return TurnPrompts{
			SystemPrompt:    projection.SystemPrompt,
			UserPrompt:      projection.UserPrompt,
			RestartRequired: false,
		}, nil
	}

	// Subsequent turns send only the incremental ephemeral tail
	var tailBuilder strings.Builder
	tailBuilder.WriteString("<ephemeral_tail>\n")
	if len(pack.EphemeralTail.RecentToolExchanges) > 0 {
		tailBuilder.WriteString("  <recent_tool_exchanges>\n")
		for _, ex := range pack.EphemeralTail.RecentToolExchanges {
			tailBuilder.WriteString(fmt.Sprintf("    - %s\n", EscapeEvidenceDelimiters(ex)))
		}
		tailBuilder.WriteString("  </recent_tool_exchanges>\n")
	}
	tailBuilder.WriteString(fmt.Sprintf("  <current_action>%s</current_action>\n",
		EscapeEvidenceDelimiters(pack.EphemeralTail.CurrentAction)))
	tailBuilder.WriteString("</ephemeral_tail>\n")

	return TurnPrompts{
		SystemPrompt:    "", // Append-only session already has initial system prompt
		UserPrompt:      tailBuilder.String(),
		RestartRequired: false,
	}, nil
}

// OpaqueSessionStrategy manages CLI wrappers or closed agent hosts.
type OpaqueSessionStrategy struct{}

func (s *OpaqueSessionStrategy) Control() protocol.ContextControl {
	return protocol.ContextControlOpaqueSession
}

func (s *OpaqueSessionStrategy) CanEvictEvidence() bool {
	return false
}

func (s *OpaqueSessionStrategy) RequiresRestartOnEviction() bool {
	return true
}

func (s *OpaqueSessionStrategy) PrepareTurnPrompt(pack *protocol.ContextPack, projection PromptProjection, turnIndex int, evictionOccurred bool) (TurnPrompts, error) {
	if turnIndex > 0 && evictionOccurred {
		return TurnPrompts{
			SystemPrompt:    projection.SystemPrompt,
			UserPrompt:      projection.UserPrompt,
			RestartRequired: true,
		}, nil
	}

	// For opaque sessions, send the full compiled prompt on turn 0, and tailored actions subsequently
	if turnIndex == 0 {
		return TurnPrompts{
			SystemPrompt:    projection.SystemPrompt,
			UserPrompt:      projection.UserPrompt,
			RestartRequired: false,
		}, nil
	}

	return TurnPrompts{
		SystemPrompt:    "",
		UserPrompt:      pack.EphemeralTail.CurrentAction,
		RestartRequired: false,
	}, nil
}
