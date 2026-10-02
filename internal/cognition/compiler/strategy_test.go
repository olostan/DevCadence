package compiler_test

import (
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/protocol"
)

func TestContextStrategy_StatelessExact(t *testing.T) {
	strat, err := compiler.NewStrategy(protocol.ContextControlExactStateless)
	if err != nil {
		t.Fatalf("unexpected error creating strategy: %v", err)
	}

	if strat.Control() != protocol.ContextControlExactStateless {
		t.Errorf("control: got %v, want exact_stateless", strat.Control())
	}
	if !strat.CanEvictEvidence() {
		t.Error("expected exact stateless strategy to support evidence eviction")
	}
	if strat.RequiresRestartOnEviction() {
		t.Error("expected exact stateless strategy NOT to require restart on eviction")
	}

	pack := validTestPack()
	renderer := compiler.NewTaggedMarkdownRenderer()
	proj, _ := renderer.Render(pack)

	turn0, err := strat.PrepareTurnPrompt(pack, proj, 0, false)
	if err != nil {
		t.Fatalf("turn 0 error: %v", err)
	}
	if turn0.RestartRequired {
		t.Error("turn 0 should not require restart")
	}
	if turn0.SystemPrompt != proj.SystemPrompt || turn0.UserPrompt != proj.UserPrompt {
		t.Error("turn 0 should send full system and user prompts")
	}

	// Turn 1 on exact stateless still receives full fresh projection
	turn1, err := strat.PrepareTurnPrompt(pack, proj, 1, true)
	if err != nil {
		t.Fatalf("turn 1 error: %v", err)
	}
	if turn1.RestartRequired {
		t.Error("turn 1 on exact stateless should not require restart even when eviction occurred")
	}
}

func TestContextStrategy_AppendOnly(t *testing.T) {
	strat, err := compiler.NewStrategy(protocol.ContextControlAppendOnly)
	if err != nil {
		t.Fatalf("unexpected error creating strategy: %v", err)
	}

	if strat.Control() != protocol.ContextControlAppendOnly {
		t.Errorf("control: got %v, want append_only", strat.Control())
	}
	if strat.CanEvictEvidence() {
		t.Error("expected append-only strategy NOT to support eviction without restart")
	}
	if !strat.RequiresRestartOnEviction() {
		t.Error("expected append-only strategy to require restart on eviction")
	}

	pack := validTestPack()
	renderer := compiler.NewTaggedMarkdownRenderer()
	proj, _ := renderer.Render(pack)

	// Turn 0 sends full prompt
	turn0, err := strat.PrepareTurnPrompt(pack, proj, 0, false)
	if err != nil {
		t.Fatalf("turn 0 error: %v", err)
	}
	if turn0.SystemPrompt == "" || turn0.UserPrompt == "" {
		t.Error("turn 0 on append-only should initialize session with system and user prompt")
	}

	// Turn 1 without eviction sends incremental tail
	turn1, err := strat.PrepareTurnPrompt(pack, proj, 1, false)
	if err != nil {
		t.Fatalf("turn 1 error: %v", err)
	}
	if turn1.RestartRequired {
		t.Error("turn 1 without eviction should not require restart")
	}
	if turn1.SystemPrompt != "" {
		t.Error("subsequent turn on append-only should have empty system prompt")
	}
	if !strings.Contains(turn1.UserPrompt, "<ephemeral_tail>") {
		t.Error("subsequent turn on append-only should contain ephemeral tail")
	}

	// Turn 2 with eviction flags restart required
	turn2, err := strat.PrepareTurnPrompt(pack, proj, 2, true)
	if err != nil {
		t.Fatalf("turn 2 error: %v", err)
	}
	if !turn2.RestartRequired {
		t.Error("turn with eviction on append-only MUST flag RestartRequired")
	}
}

func TestContextStrategy_OpaqueSession(t *testing.T) {
	strat, err := compiler.NewStrategy(protocol.ContextControlOpaqueSession)
	if err != nil {
		t.Fatalf("unexpected error creating strategy: %v", err)
	}

	if strat.Control() != protocol.ContextControlOpaqueSession {
		t.Errorf("control: got %v, want opaque_session", strat.Control())
	}

	pack := validTestPack()
	renderer := compiler.NewTaggedMarkdownRenderer()
	proj, _ := renderer.Render(pack)

	// Turn 0
	turn0, err := strat.PrepareTurnPrompt(pack, proj, 0, false)
	if err != nil {
		t.Fatalf("turn 0 error: %v", err)
	}
	if turn0.RestartRequired {
		t.Error("turn 0 should not require restart")
	}

	// Turn 1 with eviction
	turn1, err := strat.PrepareTurnPrompt(pack, proj, 1, true)
	if err != nil {
		t.Fatalf("turn 1 error: %v", err)
	}
	if !turn1.RestartRequired {
		t.Error("turn 1 with eviction on opaque session MUST flag RestartRequired")
	}
}
