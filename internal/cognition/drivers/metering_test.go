package drivers

import (
	"context"
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

func TestMetering_WallClockLimits(t *testing.T) {
	fakeDriver := NewFakeDriver("fake-metering-clock")

	meteredDriver := NewMeteredDriver(fakeDriver, MeterLimits{
		MaxDurationPerOp: 50 * time.Millisecond,
	})

	ctx := context.Background()
	session, err := meteredDriver.StartSession(ctx, SessionConfig{
		SessionID: "sess-meter-clock",
		ModelID:   "test-model",
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	meteredSess := session.(*MeteredSession)
	meter := meteredSess.Meter()

	// Inject custom simulated clock
	currentTime := time.Now()
	meter.SetNowFunc(func() time.Time {
		return currentTime
	})

	// Simulate op start
	start, err := meter.RecordOperationStart(ctx)
	if err != nil {
		t.Fatalf("RecordOperationStart failed: %v", err)
	}

	// Advance clock past 50ms limit (e.g. 80ms)
	currentTime = currentTime.Add(80 * time.Millisecond)

	paused, snap := meter.RecordOperationEnd(start, TokenUsage{InputTokens: 5, OutputTokens: 5}, nil, nil)
	if !paused {
		t.Fatalf("expected pause when duration exceeds limit")
	}
	if snap.Status != SessionStatusPausedBudgetExceeded {
		t.Errorf("expected status %q, got %q", SessionStatusPausedBudgetExceeded, snap.Status)
	}
	if snap.ExceededDimension != "max_duration_per_op" {
		t.Errorf("expected exceeded dimension 'max_duration_per_op', got %q", snap.ExceededDimension)
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
