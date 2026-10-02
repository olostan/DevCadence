package drivers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMetering_TokenBudgets(t *testing.T) {
	fakeDriver := NewFakeDriver("fake-metering-tokens")

	// Limit cumulative input to 50 tokens
	meteredDriver := NewMeteredDriver(fakeDriver, MeterLimits{
		MaxCumulativeInputTokens: 50,
	})

	ctx := context.Background()
	session, err := meteredDriver.StartSession(ctx, SessionConfig{
		SessionID: "sess-meter-tokens",
		ModelID:   "test-model",
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	// Turn 1: 30 chars prompt (~30 input tokens in fake driver) -> OK
	prompt1 := "Short prompt thirty characters"
	res1, err := session.ExecuteTurn(ctx, TurnInput{TurnID: "t1", Prompt: prompt1})
	if err != nil {
		t.Fatalf("turn 1 failed: %v", err)
	}
	if res1.PausedReason != "" {
		t.Errorf("unexpected paused reason on turn 1: %s", res1.PausedReason)
	}
	if session.Status() != SessionStatusActive {
		t.Errorf("expected active status, got %s", session.Status())
	}

	// Turn 2: 30 chars prompt -> cumulative input = 60 > 50 -> PAUSED_BUDGET_EXCEEDED
	prompt2 := "Another prompt thirty chars ok"
	res2, err := session.ExecuteTurn(ctx, TurnInput{TurnID: "t2", Prompt: prompt2})
	if err != nil {
		t.Fatalf("turn 2 returned unexpected hard error: %v", err)
	}
	if res2.PausedReason != PauseReasonBudgetExceeded {
		t.Errorf("expected PausedReason %q, got %q", PauseReasonBudgetExceeded, res2.PausedReason)
	}
	if session.Status() != SessionStatusPausedBudgetExceeded {
		t.Errorf("expected status %q, got %q", SessionStatusPausedBudgetExceeded, session.Status())
	}

	// Turn 3: after pause, starting a turn must be rejected
	_, err = session.ExecuteTurn(ctx, TurnInput{TurnID: "t3", Prompt: "more"})
	if err == nil {
		t.Errorf("expected error executing turn while paused, got nil")
	}

	meteredSess := session.(*MeteredSession)
	snap := meteredSess.Meter().Checkpoint()
	if snap.PausedReason != PauseReasonBudgetExceeded {
		t.Errorf("expected checkpoint paused reason %q, got %q", PauseReasonBudgetExceeded, snap.PausedReason)
	}
	if snap.ExceededDimension != "max_cumulative_input_tokens" {
		t.Errorf("expected exceeded dimension 'max_cumulative_input_tokens', got %q", snap.ExceededDimension)
	}
	if !snap.EscalationRequired {
		t.Errorf("expected EscalationRequired=true")
	}
}

func TestMetering_ResumePreservesMeterState(t *testing.T) {
	fakeDriver := NewFakeDriver("fake-meter-resume")

	meteredDriver := NewMeteredDriver(fakeDriver, MeterLimits{
		MaxCumulativeInputTokens: 100,
	})

	ctx := context.Background()
	cfg := SessionConfig{
		SessionID: "sess-continuity-1",
		ModelID:   "test-model",
	}

	session1, err := meteredDriver.StartSession(ctx, cfg)
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	// Turn 1: 30 chars input
	_, err = session1.ExecuteTurn(ctx, TurnInput{TurnID: "t1", Prompt: "Thirty characters long prompt."})
	if err != nil {
		t.Fatalf("turn 1 failed: %v", err)
	}

	snap1 := session1.(*MeteredSession).Meter().Checkpoint()
	if snap1.CumulativeUsage.InputTokens != 30 {
		t.Errorf("expected 30 input tokens, got %d", snap1.CumulativeUsage.InputTokens)
	}

	// Resume session
	session2, err := meteredDriver.ResumeSession(ctx, cfg.SessionID, cfg)
	if err != nil {
		t.Fatalf("ResumeSession failed: %v", err)
	}

	snap2 := session2.(*MeteredSession).Meter().Checkpoint()
	if snap2.CumulativeUsage.InputTokens != 30 {
		t.Errorf("expected resumed session to retain 30 cumulative input tokens, got %d", snap2.CumulativeUsage.InputTokens)
	}

	// Turn 2: another 30 chars
	_, err = session2.ExecuteTurn(ctx, TurnInput{TurnID: "t2", Prompt: "Another thirty chars of prompt"})
	if err != nil {
		t.Fatalf("turn 2 failed: %v", err)
	}

	snap3 := session2.(*MeteredSession).Meter().Checkpoint()
	if snap3.CumulativeUsage.InputTokens != 60 {
		t.Errorf("expected 60 cumulative input tokens after resumed turn, got %d", snap3.CumulativeUsage.InputTokens)
	}
}

func TestMetering_ActiveMaxDurationPerOpTimeout(t *testing.T) {
	fakeDriver := NewFakeDriver("fake-timeout-driver", FakeDriverOptions{
		Delay: 200 * time.Millisecond,
	})

	// Set MaxDurationPerOp to 40ms, so execution must actively time out
	meteredDriver := NewMeteredDriver(fakeDriver, MeterLimits{
		MaxDurationPerOp: 40 * time.Millisecond,
	})

	ctx := context.Background()
	session, err := meteredDriver.StartSession(ctx, SessionConfig{
		SessionID: "sess-op-timeout",
		ModelID:   "test-model",
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}
	defer session.Close(ctx)

	start := time.Now()
	_, err = session.ExecuteTurn(ctx, TurnInput{TurnID: "t-timeout", Prompt: "slow op"})
	elapsed := time.Since(start)

	if err == nil {
		t.Errorf("expected timeout error when turn exceeds MaxDurationPerOp, got nil")
	}
	if elapsed >= 180*time.Millisecond {
		t.Errorf("operation was not cancelled actively by deadline, took %v", elapsed)
	}

	snap := session.(*MeteredSession).Meter().Checkpoint()
	if snap.Status != SessionStatusPausedBudgetExceeded {
		t.Errorf("expected paused status after timeout, got %s", snap.Status)
	}
	if snap.ExceededDimension != "max_duration_per_op" {
		t.Errorf("expected exceeded dimension 'max_duration_per_op', got %s", snap.ExceededDimension)
	}
}

func TestMetering_OscillatingEditsViaMediator(t *testing.T) {
	fakeDriver := NewFakeDriver("fake-meter-oscillate")
	mediator := NewScopedToolMediator(nil)

	mediator.RegisterHandler("write_file", func(ctx context.Context, args json.RawMessage) (string, error) {
		return "ok", nil
	})

	meteredDriver := NewMeteredDriver(fakeDriver, MeterLimits{
		LoopConfig: LoopDetectorConfig{
			MaxOscillatingEdits: 2,
		},
	})

	cfg := SessionConfig{
		SessionID: "sess-oscillate-test",
		ModelID:   "test-model",
		Tools: []ToolDefinition{
			{Name: "write_file", Description: "write file"},
		},
		Mediator: mediator,
	}

	session, err := meteredDriver.StartSession(context.Background(), cfg)
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	// Turn 1: edit A
	_, _ = session.ExecuteTurn(context.Background(), TurnInput{
		TurnID: "t1",
		Prompt: "write A",
	})
	_, _ = mediator.ExecuteTool(context.Background(), ToolCall{
		ID:        "c1",
		Name:      "write_file",
		Arguments: []byte(`{"path":"main.go","content":"code A"}`),
	})

	// Turn 2: edit B
	_, _ = mediator.ExecuteTool(context.Background(), ToolCall{
		ID:        "c2",
		Name:      "write_file",
		Arguments: []byte(`{"path":"main.go","content":"code B"}`),
	})

	// Turn 3: edit A (1st oscillation)
	_, _ = mediator.ExecuteTool(context.Background(), ToolCall{
		ID:        "c3",
		Name:      "write_file",
		Arguments: []byte(`{"path":"main.go","content":"code A"}`),
	})

	// Turn 4: edit B (2nd oscillation -> trigger loop!)
	_, _ = mediator.ExecuteTool(context.Background(), ToolCall{
		ID:        "c4",
		Name:      "write_file",
		Arguments: []byte(`{"path":"main.go","content":"code B"}`),
	})

	if session.Status() != SessionStatusPausedBudgetExceeded {
		t.Errorf("expected session paused on oscillating edits, got %s", session.Status())
	}
	snap := session.(*MeteredSession).Meter().Checkpoint()
	if snap.ExceededDimension != "semantic_loop_oscillating_edits" {
		t.Errorf("expected 'semantic_loop_oscillating_edits', got %q", snap.ExceededDimension)
	}
}

func TestMetering_ClosedSessionStatus(t *testing.T) {
	fakeDriver := NewFakeDriver("fake-meter-close")
	meteredDriver := NewMeteredDriver(fakeDriver, MeterLimits{
		MaxCumulativeInputTokens: 10,
	})

	session, err := meteredDriver.StartSession(context.Background(), SessionConfig{
		SessionID: "sess-close-check",
		ModelID:   "test-model",
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	// Exceed limit so it enters paused state
	_, _ = session.ExecuteTurn(context.Background(), TurnInput{
		TurnID: "t1",
		Prompt: "Prompt exceeding ten chars easily",
	})
	if session.Status() != SessionStatusPausedBudgetExceeded {
		t.Fatalf("expected paused status, got %s", session.Status())
	}

	// Now close the session
	_ = session.Close(context.Background())

	// Status must report closed, not stuck in paused
	if session.Status() != SessionStatusClosed {
		t.Errorf("expected status 'closed' after Close(), got %s", session.Status())
	}
}

func TestMetering_ToolCallCeiling(t *testing.T) {
	fakeDriver := NewFakeDriver("fake-metering-tools")

	meteredDriver := NewMeteredDriver(fakeDriver, MeterLimits{
		MaxCumulativeToolCalls: 2,
	})

	ctx := context.Background()
	session, err := meteredDriver.StartSession(ctx, SessionConfig{
		SessionID: "sess-meter-tool-calls",
		ModelID:   "test-model",
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	// Turn with 1 tool call
	_, _ = session.ExecuteTurn(ctx, TurnInput{TurnID: "t1", Prompt: "CALL_TOOL: tool_a"})
	if session.Status() != SessionStatusActive {
		t.Errorf("expected active, got %s", session.Status())
	}

	// Turn with 2nd tool call -> cumulative = 2 <= 2 -> active
	_, _ = session.ExecuteTurn(ctx, TurnInput{TurnID: "t2", Prompt: "CALL_TOOL: tool_b"})
	if session.Status() != SessionStatusActive {
		t.Errorf("expected active, got %s", session.Status())
	}

	// Turn with 3rd tool call -> cumulative = 3 > 2 -> paused
	res, _ := session.ExecuteTurn(ctx, TurnInput{TurnID: "t3", Prompt: "CALL_TOOL: tool_c"})
	if res.PausedReason != PauseReasonBudgetExceeded {
		t.Errorf("expected paused reason %q, got %q", PauseReasonBudgetExceeded, res.PausedReason)
	}
	if session.Status() != SessionStatusPausedBudgetExceeded {
		t.Errorf("expected status %q, got %q", SessionStatusPausedBudgetExceeded, session.Status())
	}
}

func TestMetering_LoopDetectionSuspension(t *testing.T) {
	fakeDriver := NewFakeDriver("fake-metering-loop")

	meteredDriver := NewMeteredDriver(fakeDriver, MeterLimits{
		LoopConfig: LoopDetectorConfig{
			MaxConsecutiveFailedCalls: 2,
		},
	})

	ctx := context.Background()
	session, err := meteredDriver.StartSession(ctx, SessionConfig{
		SessionID: "sess-meter-loop",
		ModelID:   "test-model",
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	// Turn 1 with a failed tool result
	res1, _ := session.ExecuteTurn(ctx, TurnInput{
		TurnID: "t1",
		Prompt: "CALL_TOOL: failed_tool",
		ToolResults: []ToolResult{
			{
				ToolCallID: "call-1",
				Name:       "failed_tool",
				Content:    "error syntax",
				IsError:    true,
			},
		},
	})
	if res1.PausedReason != "" {
		t.Errorf("turn 1 should not be paused")
	}

	// Turn 2 with identical failed tool result -> threshold reached
	res2, _ := session.ExecuteTurn(ctx, TurnInput{
		TurnID: "t2",
		Prompt: "CALL_TOOL: failed_tool",
		ToolResults: []ToolResult{
			{
				ToolCallID: "call-1",
				Name:       "failed_tool",
				Content:    "error syntax",
				IsError:    true,
			},
		},
	})
	if res2.PausedReason != PauseReasonBudgetExceeded {
		t.Errorf("expected PausedReason %q, got %q", PauseReasonBudgetExceeded, res2.PausedReason)
	}
	if session.Status() != SessionStatusPausedBudgetExceeded {
		t.Errorf("expected status %q, got %q", SessionStatusPausedBudgetExceeded, session.Status())
	}

	meteredSess := session.(*MeteredSession)
	snap := meteredSess.Meter().Checkpoint()
	if snap.ExceededDimension != "semantic_loop_repeated_tool_failures" {
		t.Errorf("expected exceeded dimension 'semantic_loop_repeated_tool_failures', got %q", snap.ExceededDimension)
	}
}

func TestMetering_NoPromptCountdownInjection(t *testing.T) {
	// ADR-0019 §2: Prompts must NEVER inject turn countdowns (e.g. "you have 5 turns left").
	var receivedPrompt string
	fakeDriver := NewFakeDriver("fake-no-countdown")
	fakeDriver.SetTurnHandler("t1", func(ctx context.Context, input TurnInput) (TurnResult, error) {
		receivedPrompt = input.Prompt
		return TurnResult{
			TurnID:  input.TurnID,
			Content: "ok",
		}, nil
	})

	meteredDriver := NewMeteredDriver(fakeDriver, MeterLimits{
		MaxCumulativeTotalTokens: 1000,
	})

	ctx := context.Background()
	session, err := meteredDriver.StartSession(ctx, SessionConfig{
		SessionID: "sess-no-countdown",
		ModelID:   "test-model",
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	originalPrompt := "Implement function F according to contract."
	_, err = session.ExecuteTurn(ctx, TurnInput{
		TurnID: "t1",
		Prompt: originalPrompt,
	})
	if err != nil {
		t.Fatalf("ExecuteTurn failed: %v", err)
	}

	// Prompt must match verbatim: no countdowns, no injected warnings
	if receivedPrompt != originalPrompt {
		t.Errorf("expected prompt %q, got altered prompt %q", originalPrompt, receivedPrompt)
	}
	if strings.Contains(strings.ToLower(receivedPrompt), "turns left") ||
		strings.Contains(strings.ToLower(receivedPrompt), "countdown") ||
		strings.Contains(strings.ToLower(receivedPrompt), "turn limit") {
		t.Errorf("prompt contains forbidden turn countdown text: %q", receivedPrompt)
	}
}
