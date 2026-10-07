package drivers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/tools"
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
	if !snap1.CumulativeUsage.Input.Known || snap1.CumulativeUsage.Input.Value != 30 {
		t.Errorf("expected 30 known input tokens, got known=%v value=%d", snap1.CumulativeUsage.Input.Known, snap1.CumulativeUsage.Input.Value)
	}

	// Resume session
	session2, err := meteredDriver.ResumeSession(ctx, cfg.SessionID, cfg)
	if err != nil {
		t.Fatalf("ResumeSession failed: %v", err)
	}

	snap2 := session2.(*MeteredSession).Meter().Checkpoint()
	if !snap2.CumulativeUsage.Input.Known || snap2.CumulativeUsage.Input.Value != 30 {
		t.Errorf("expected resumed session to retain 30 known cumulative input tokens, got known=%v value=%d", snap2.CumulativeUsage.Input.Known, snap2.CumulativeUsage.Input.Value)
	}

	// Turn 2: another 30 chars
	_, err = session2.ExecuteTurn(ctx, TurnInput{TurnID: "t2", Prompt: "Another thirty chars of prompt"})
	if err != nil {
		t.Fatalf("turn 2 failed: %v", err)
	}

	snap3 := session2.(*MeteredSession).Meter().Checkpoint()
	if !snap3.CumulativeUsage.Input.Known || snap3.CumulativeUsage.Input.Value != 60 {
		t.Errorf("expected 60 known cumulative input tokens after resumed turn, got known=%v value=%d", snap3.CumulativeUsage.Input.Known, snap3.CumulativeUsage.Input.Value)
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

	mediator.RegisterToolDefinition(ToolDefinition{
		Name:         "write_file",
		Description:  "write file",
		MutatesFiles: true,
	})
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
			{Name: "write_file", Description: "write file", MutatesFiles: true},
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

func TestMetering_CheckpointRestorePreservesLoopDetector(t *testing.T) {
	limits := MeterLimits{
		LoopConfig: LoopDetectorConfig{
			MaxConsecutiveFailedCalls: 3,
			MaxOscillatingEdits:       2,
		},
	}
	meter := NewSilentMeter("sess-loop-restore", limits)

	// Simulate 2 consecutive failed calls for key
	call := ToolCall{
		ID:        "c1",
		Name:      "test_tool",
		Arguments: []byte(`{"arg":"same"}`),
	}
	resError := ToolResult{
		ToolCallID: "c1",
		Name:       "test_tool",
		IsError:    true,
	}

	meter.RecordToolResult(call, resError)
	meter.RecordToolResult(call, resError)

	if meter.IsPaused() {
		t.Fatalf("meter should not be paused yet after 2 failures")
	}

	// Capture snapshot
	snap := meter.Checkpoint()
	if snap.LoopSnapshot.ConsecutiveFailedCount != 2 {
		t.Fatalf("expected ConsecutiveFailedCount=2, got %d", snap.LoopSnapshot.ConsecutiveFailedCount)
	}

	// Restore into a new meter
	newMeter := NewSilentMeter("sess-loop-restore", limits)
	newMeter.RestoreFromSnapshot(snap)

	// 3rd failure on restored meter must immediately trip loop detection!
	paused, updatedSnap := newMeter.RecordToolResult(call, resError)
	if !paused {
		t.Errorf("expected 3rd failure after restore to trip loop detection")
	}
	if updatedSnap.PausedReason != PauseReasonBudgetExceeded {
		t.Errorf("expected PausedReason %q, got %q", PauseReasonBudgetExceeded, updatedSnap.PausedReason)
	}
	if updatedSnap.ExceededDimension != "semantic_loop_repeated_tool_failures" {
		t.Errorf("expected dimension semantic_loop_repeated_tool_failures, got %s", updatedSnap.ExceededDimension)
	}
}

func TestMetering_DriverInternalToolExecutionTracked(t *testing.T) {
	client := &mockDirectClient{}
	driver := MustNewDirectAPIDriver("direct-api-metered-tools", client)

	mediator := NewScopedToolMediator(nil)
	mediator.RegisterHandler("failing_tool", func(ctx context.Context, args json.RawMessage) (string, error) {
		return "", errs.New(errs.CategoryInternal, "persistent tool failure")
	})

	meteredDriver := NewMeteredDriver(driver, MeterLimits{
		LoopConfig: LoopDetectorConfig{
			MaxConsecutiveFailedCalls: 2,
		},
	})

	ctx := context.Background()
	session, err := meteredDriver.StartSession(ctx, SessionConfig{
		SessionID: "sess-internal-tool-loop",
		ModelID:   "direct-model-v1",
		Tools: []ToolDefinition{
			{Name: "failing_tool", Description: "always fails"},
		},
		Mediator: mediator,
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	// Turn 1: driver calls failing_tool internally -> 1 failure
	_, err = session.ExecuteTurn(ctx, TurnInput{
		TurnID: "t1",
		Prompt: "CALL_TOOL: failing_tool",
	})
	if err != nil {
		t.Fatalf("turn 1 failed: %v", err)
	}
	if session.Status() != SessionStatusActive {
		t.Errorf("expected active after 1 failure, got %s", session.Status())
	}

	// Turn 2: driver calls failing_tool internally again -> 2nd failure -> pauses!
	res2, err := session.ExecuteTurn(ctx, TurnInput{
		TurnID: "t2",
		Prompt: "CALL_TOOL: failing_tool",
	})
	if err != nil {
		t.Fatalf("turn 2 returned unexpected hard error: %v", err)
	}
	if res2.PausedReason != PauseReasonBudgetExceeded {
		t.Errorf("expected turn 2 to be paused with budget exceeded, got %q", res2.PausedReason)
	}
	if session.Status() != SessionStatusPausedBudgetExceeded {
		t.Errorf("expected session status to be paused, got %s", session.Status())
	}
}

func TestScopedToolMediator_StructuralContainmentAndExtraction(t *testing.T) {
	scope := &tools.Scope{
		ProjectID:    "proj-scope",
		WorktreePath: "/tmp/worktree",
	}
	mediator := NewScopedToolMediator(scope)

	// Tool with custom extractor
	mediator.RegisterToolDefinition(ToolDefinition{
		Name: "custom_extractor_tool",
		PathExtractor: func(args json.RawMessage) ([]string, error) {
			var m map[string]string
			if err := json.Unmarshal(args, &m); err != nil {
				return nil, err
			}
			if p, ok := m["custom_key"]; ok {
				return []string{p}, nil
			}
			return nil, nil
		},
	})

	// Tool with declared path parameters (nested)
	mediator.RegisterToolDefinition(ToolDefinition{
		Name:           "nested_param_tool",
		PathParameters: []string{"target.rel_file"},
	})

	mediator.RegisterHandler("custom_extractor_tool", func(ctx context.Context, args json.RawMessage) (string, error) {
		return "ok", nil
	})
	mediator.RegisterHandler("nested_param_tool", func(ctx context.Context, args json.RawMessage) (string, error) {
		return "ok", nil
	})
	mediator.RegisterHandler("recursive_tool", func(ctx context.Context, args json.RawMessage) (string, error) {
		return "ok", nil
	})

	ctx := context.Background()

	// 1. Session-scoped mediator isolation
	s1 := mediator.ForSession("session-1", []ToolDefinition{
		{Name: "custom_extractor_tool"},
	})
	s2 := mediator.ForSession("session-2", []ToolDefinition{
		{Name: "nested_param_tool"},
	})

	// s1 cannot execute s2's tool
	_, err := s1.ExecuteTool(ctx, ToolCall{
		ID:   "c1",
		Name: "nested_param_tool",
	})
	if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Errorf("expected CategoryPolicyDenied for undeclared tool on s1, got %v", err)
	}

	// 2. Custom extractor detects escaping path
	_, err = s1.ExecuteTool(ctx, ToolCall{
		ID:        "c2",
		Name:      "custom_extractor_tool",
		Arguments: []byte(`{"custom_key":"../../etc/shadow"}`),
	})
	if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Errorf("expected CategoryPolicyDenied from custom extractor escape, got %v", err)
	}

	// 3. Nested declared parameter detects escaping path
	_, err = s2.ExecuteTool(ctx, ToolCall{
		ID:        "c3",
		Name:      "nested_param_tool",
		Arguments: []byte(`{"target":{"rel_file":"../escape.txt"}}`),
	})
	if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Errorf("expected CategoryPolicyDenied from nested declared parameter escape, got %v", err)
	}

	// 4. Recursive baseline inspection detects deeply nested escaping path
	s3 := mediator.ForSession("session-3", []ToolDefinition{
		{Name: "recursive_tool"},
	})
	_, err = s3.ExecuteTool(ctx, ToolCall{
		ID:        "c4",
		Name:      "recursive_tool",
		Arguments: []byte(`{"deep":{"sub":{"items":[{"source_file":"../../outside.txt"}]}}}`),
	})
	if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Errorf("expected CategoryPolicyDenied from recursive inspection escape, got %v", err)
	}
}

func TestMetering_ResumeDoesNotDuplicateListeners(t *testing.T) {
	fakeDriver := NewFakeDriver("fake-resume-listeners")
	mediator := NewScopedToolMediator(nil)

	mediator.RegisterToolDefinition(ToolDefinition{
		Name:        "failing_tool",
		Description: "always fails",
	})
	mediator.RegisterHandler("failing_tool", func(ctx context.Context, args json.RawMessage) (string, error) {
		return "", errs.New(errs.CategoryInternal, "persistent tool failure")
	})

	meteredDriver := NewMeteredDriver(fakeDriver, MeterLimits{
		LoopConfig: LoopDetectorConfig{
			MaxConsecutiveFailedCalls: 5,
		},
	})

	ctx := context.Background()
	cfg := SessionConfig{
		SessionID: "sess-resume-listener-test",
		ModelID:   "test-model",
		Tools: []ToolDefinition{
			{Name: "failing_tool", Description: "always fails"},
		},
		Mediator: mediator,
	}

	session, err := meteredDriver.StartSession(ctx, cfg)
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	// Resume the same session 3 times
	for i := 0; i < 3; i++ {
		resumed, resumeErr := meteredDriver.ResumeSession(ctx, cfg.SessionID, cfg)
		if resumeErr != nil {
			t.Fatalf("ResumeSession[%d] failed: %v", i, resumeErr)
		}
		session = resumed
	}

	// Execute 1 failing mediated tool call
	ms, ok := session.(*MeteredSession)
	if !ok {
		t.Fatalf("expected session to be *MeteredSession")
	}

	// Execute the tool call via the session's mediator
	sessionMediator := session.Config().Mediator
	if sessionMediator == nil {
		t.Fatalf("expected non-nil session mediator")
	}
	_, _ = sessionMediator.ExecuteTool(ctx, ToolCall{
		ID:        "c1",
		Name:      "failing_tool",
		Arguments: []byte(`{}`),
	})

	// Assert consecutiveFailedCount in SemanticLoopDetector advances by exactly 1, not 3 or 4
	snap := ms.Meter().Checkpoint()
	if snap.LoopSnapshot.ConsecutiveFailedCount != 1 {
		t.Fatalf("expected ConsecutiveFailedCount to be exactly 1, got %d", snap.LoopSnapshot.ConsecutiveFailedCount)
	}
}

func TestScopedToolMediator_ReadOnlyToolsDoNotTriggerEditListeners(t *testing.T) {
	scope := &tools.Scope{
		ProjectID:    "proj-readonly",
		WorktreePath: t.TempDir(),
	}
	mediator := NewScopedToolMediator(scope)

	mediator.RegisterToolDefinition(ToolDefinition{
		Name:           "read_file",
		Description:    "reads a file without mutating",
		PathParameters: []string{"path"},
		MutatesFiles:   false,
	})
	mediator.RegisterHandler("read_file", func(ctx context.Context, args json.RawMessage) (string, error) {
		return "file contents", nil
	})

	var editCount int
	mediator.OnFileEdit(func(path string, content []byte) {
		editCount++
	})

	ctx := context.Background()
	sessMediator := mediator.ForSession("session-ro", []ToolDefinition{
		{Name: "read_file"},
	})
	sessMediator.OnFileEdit(func(path string, content []byte) {
		editCount++
	})

	// Execute read_file multiple times with alternating paths
	for i := 0; i < 5; i++ {
		path := "a.txt"
		if i%2 == 1 {
			path = "b.txt"
		}
		_, err := sessMediator.ExecuteTool(ctx, ToolCall{
			ID:        fmt.Sprintf("read-%d", i),
			Name:      "read_file",
			Arguments: []byte(fmt.Sprintf(`{"path":%q}`, path)),
		})
		if err != nil {
			t.Fatalf("unexpected error executing read_file: %v", err)
		}
	}

	if editCount != 0 {
		t.Errorf("expected 0 file edit notifications for read-only tool, got %d", editCount)
	}
}

func TestSilentMeter_SnapshotAndRestorePreservesToolCallState(t *testing.T) {
	meter := NewSilentMeter("sess-snap-test", MeterLimits{})
	call := ToolCall{
		ID:        "tc-101",
		Name:      "inspect_tool",
		Arguments: []byte(`{"arg":"val"}`),
	}
	res := ToolResult{
		ToolCallID: "tc-101",
		Name:       "inspect_tool",
		Content:    "result 101",
	}
	meter.RecordToolResult(call, res)

	snap := meter.Checkpoint()
	if len(snap.ProcessedToolCallIDs) != 1 || snap.ProcessedToolCallIDs[0] != "tc-101" {
		t.Errorf("expected ProcessedToolCallIDs to contain 'tc-101', got %v", snap.ProcessedToolCallIDs)
	}
	if len(snap.RecentToolCalls) != 1 || snap.RecentToolCalls[0].ID != "tc-101" {
		t.Errorf("expected RecentToolCalls to contain 'tc-101', got %v", snap.RecentToolCalls)
	}

	// Restore into brand-new meter
	restoredMeter := NewSilentMeter("sess-snap-test", MeterLimits{})
	restoredMeter.RestoreFromSnapshot(snap)

	restoredSnap := restoredMeter.Checkpoint()
	if len(restoredSnap.ProcessedToolCallIDs) != 1 || restoredSnap.ProcessedToolCallIDs[0] != "tc-101" {
		t.Errorf("restored: expected ProcessedToolCallIDs to contain 'tc-101', got %v", restoredSnap.ProcessedToolCallIDs)
	}
	if len(restoredSnap.RecentToolCalls) != 1 || restoredSnap.RecentToolCalls[0].ID != "tc-101" {
		t.Errorf("restored: expected RecentToolCalls to contain 'tc-101', got %v", restoredSnap.RecentToolCalls)
	}
}

func TestScopedToolMediator_MultiPathMutatingTools(t *testing.T) {
	scope := &tools.Scope{
		ProjectID:    "proj-multipath",
		WorktreePath: t.TempDir(),
	}
	mediator := NewScopedToolMediator(scope)

	mediator.RegisterToolDefinition(ToolDefinition{
		Name:         "multi_edit",
		Description:  "modifies multiple files",
		MutatesFiles: true,
	})
	mediator.RegisterHandler("multi_edit", func(ctx context.Context, args json.RawMessage) (string, error) {
		return "ok", nil
	})

	var editedPaths []string
	mediator.OnFileEdit(func(path string, content []byte) {
		editedPaths = append(editedPaths, path)
	})

	ctx := context.Background()
	_, err := mediator.ExecuteTool(ctx, ToolCall{
		ID:        "c-multi",
		Name:      "multi_edit",
		Arguments: []byte(`{"files":["f1.go","f2.go","f3.go"]}`),
	})
	if err != nil {
		t.Fatalf("ExecuteTool failed: %v", err)
	}

	if len(editedPaths) != 3 {
		t.Fatalf("expected 3 edited paths notified, got %d: %v", len(editedPaths), editedPaths)
	}
	expected := map[string]bool{"f1.go": true, "f2.go": true, "f3.go": true}
	for _, p := range editedPaths {
		if !expected[p] {
			t.Errorf("unexpected path notified: %q", p)
		}
	}
}

func TestSilentMeter_DeterministicSnapshotOrdering(t *testing.T) {
	meter := NewSilentMeter("sess-order-test", MeterLimits{})

	// Add out of order IDs
	calls := []ToolCall{
		{ID: "z-call", Name: "z_tool"},
		{ID: "a-call", Name: "a_tool"},
		{ID: "m-call", Name: "m_tool"},
	}
	for _, c := range calls {
		meter.RecordToolResult(c, ToolResult{ToolCallID: c.ID, Name: c.Name})
	}

	snap := meter.Checkpoint()

	// Check ProcessedToolCallIDs is sorted
	if len(snap.ProcessedToolCallIDs) != 3 ||
		snap.ProcessedToolCallIDs[0] != "a-call" ||
		snap.ProcessedToolCallIDs[1] != "m-call" ||
		snap.ProcessedToolCallIDs[2] != "z-call" {
		t.Errorf("expected ProcessedToolCallIDs to be sorted [a-call, m-call, z-call], got %v", snap.ProcessedToolCallIDs)
	}

	// Check RecentToolCalls is sorted by ID
	if len(snap.RecentToolCalls) != 3 ||
		snap.RecentToolCalls[0].ID != "a-call" ||
		snap.RecentToolCalls[1].ID != "m-call" ||
		snap.RecentToolCalls[2].ID != "z-call" {
		t.Errorf("expected RecentToolCalls to be sorted by ID [a-call, m-call, z-call], got %v", snap.RecentToolCalls)
	}
}

func TestMetering_AllowUnknownUsage_False_SuspendsOnUnknown(t *testing.T) {
	// P0-6: configured cached limit with cached unknown and AllowUnknownUsage=false
	// -> suspends PAUSED_BUDGET_EXCEEDED, pause reason usage_unknown.
	meter := NewSilentMeter("sess-unknown-usage", MeterLimits{
		MaxCumulativeCachedTokens: 50,
		AllowUnknownUsage:         false,
	})

	// Operation reports known input and output, but unknown cached
	usage := TokenUsage{
		Input:  KnownMeasurement(10),
		Cached: TokenMeasurement{}, // unknown
		Output: KnownMeasurement(5),
	}

	paused, snap := meter.RecordOperationEnd(time.Now(), usage, nil, nil)
	if !paused {
		t.Fatalf("expected meter to pause when configured limit metric is unknown and AllowUnknownUsage=false")
	}
	if snap.Status != SessionStatusPausedBudgetExceeded {
		t.Errorf("expected status %q, got %q", SessionStatusPausedBudgetExceeded, snap.Status)
	}
	if snap.PausedReason != PauseReasonBudgetExceeded {
		t.Errorf("expected PausedReason %q, got %q", PauseReasonBudgetExceeded, snap.PausedReason)
	}
	if snap.ExceededDimension != "usage_unknown" {
		t.Errorf("expected ExceededDimension %q, got %q", "usage_unknown", snap.ExceededDimension)
	}

	// P0-6 continued: total-only configured limit with known input/output is still evaluated
	// and does NOT trip on unknown cached.
	meterTotal := NewSilentMeter("sess-total-only", MeterLimits{
		MaxCumulativeTotalTokens: 100,
		AllowUnknownUsage:        false,
	})
	pausedTotal, snapTotal := meterTotal.RecordOperationEnd(time.Now(), usage, nil, nil)
	if pausedTotal {
		t.Errorf("total-only limit with known input/output should evaluate without pausing, got paused")
	}
	if !snapTotal.UsageKnown {
		t.Errorf("expected UsageKnown=true when input and output are known")
	}

	// If total limit is configured and output is unknown -> pauses with usage_unknown
	meterTotalUnknown := NewSilentMeter("sess-total-unknown", MeterLimits{
		MaxCumulativeTotalTokens: 100,
		AllowUnknownUsage:        false,
	})
	usageOutUnknown := TokenUsage{
		Input:  KnownMeasurement(10),
		Cached: KnownMeasurement(5),
		Output: TokenMeasurement{}, // unknown
	}
	pausedOutUnknown, snapOutUnknown := meterTotalUnknown.RecordOperationEnd(time.Now(), usageOutUnknown, nil, nil)
	if !pausedOutUnknown {
		t.Fatalf("expected total-only limit to pause when output is unknown and AllowUnknownUsage=false")
	}
	if snapOutUnknown.ExceededDimension != "usage_unknown" {
		t.Errorf("expected ExceededDimension 'usage_unknown', got %q", snapOutUnknown.ExceededDimension)
	}
}

func TestMetering_AllowUnknownUsage_True_UnprovableLimits(t *testing.T) {
	// P0-7: AllowUnknownUsage=true, cached limit configured, cached unknown
	// -> execution continues, UnprovableLimits == ["max_cumulative_cached_tokens"],
	// turn/wall-time/tool-call bounds still trip.
	meter := NewSilentMeter("sess-allow-unknown", MeterLimits{
		MaxCumulativeCachedTokens: 50,
		MaxCumulativeInputTokens:  100,
		MaxCumulativeToolCalls:    2,
		AllowUnknownUsage:         true,
	})

	usage := TokenUsage{
		Input:  KnownMeasurement(20),
		Cached: TokenMeasurement{}, // unknown
		Output: KnownMeasurement(10),
	}

	// First op: within tool calls, cached unknown
	paused, snap := meter.RecordOperationEnd(time.Now(), usage, []ToolCall{{ID: "tc-1"}}, nil)
	if paused {
		t.Fatalf("meter should not pause when AllowUnknownUsage=true and unknown metric is unprovable")
	}
	if snap.Status != SessionStatusActive {
		t.Errorf("expected status active, got %s", snap.Status)
	}
	if len(snap.UnprovableLimits) != 1 || snap.UnprovableLimits[0] != "max_cumulative_cached_tokens" {
		t.Errorf("expected UnprovableLimits == [max_cumulative_cached_tokens], got %v", snap.UnprovableLimits)
	}
	if !snap.UsageKnown {
		t.Errorf("expected UsageKnown=true since input and output are known")
	}

	// Second op exceeds tool call bound -> trips max_cumulative_tool_calls
	paused2, snap2 := meter.RecordOperationEnd(time.Now(), usage, []ToolCall{{ID: "tc-2"}, {ID: "tc-3"}}, nil)
	if !paused2 {
		t.Fatalf("expected tool call ceiling to still trip when AllowUnknownUsage=true")
	}
	if snap2.ExceededDimension != "max_cumulative_tool_calls" {
		t.Errorf("expected ExceededDimension max_cumulative_tool_calls, got %q", snap2.ExceededDimension)
	}
	// UnprovableLimits should still record the unprovable limit
	if len(snap2.UnprovableLimits) != 1 || snap2.UnprovableLimits[0] != "max_cumulative_cached_tokens" {
		t.Errorf("expected UnprovableLimits to still contain max_cumulative_cached_tokens, got %v", snap2.UnprovableLimits)
	}
}

func TestMetering_KnownUsage_LimitsEnforced(t *testing.T) {
	// P0-8: known usage over a configured limit -> trips exactly as before;
	// known usage under it -> no trip and UnprovableLimits empty.
	meter := NewSilentMeter("sess-known-limits", MeterLimits{
		MaxCumulativeInputTokens: 50,
		AllowUnknownUsage:        true,
	})

	usageUnder := KnownUsage(30, 10, 10)
	pausedUnder, snapUnder := meter.RecordOperationEnd(time.Now(), usageUnder, nil, nil)
	if pausedUnder {
		t.Errorf("usage under limit should not pause")
	}
	if len(snapUnder.UnprovableLimits) != 0 {
		t.Errorf("expected UnprovableLimits to be empty for known usage under limit, got %v", snapUnder.UnprovableLimits)
	}

	usageOver := KnownUsage(30, 5, 5)
	pausedOver, snapOver := meter.RecordOperationEnd(time.Now(), usageOver, nil, nil)
	if !pausedOver {
		t.Fatalf("expected known usage over limit to trip")
	}
	if snapOver.ExceededDimension != "max_cumulative_input_tokens" {
		t.Errorf("expected ExceededDimension 'max_cumulative_input_tokens', got %q", snapOver.ExceededDimension)
	}
}

type immediateErrSession struct {
	Session
	errToReturn error
}

func (s *immediateErrSession) StreamTurn(_ context.Context, _ TurnInput) (EventStream, error) {
	return nil, s.errToReturn
}

func TestMetering_StreamTurn_ImmediateErrorRecordsUnknown(t *testing.T) {
	// P0-9: a session whose StreamTurn is invoked and immediately returns an error
	// with no usage evidence -> the operation records unknown usage;
	// with AllowUnknownUsage=false and a configured token limit the meter suspends with usage_unknown.
	fakeDriver := NewFakeDriver("fake-stream-err")
	rawSession, err := fakeDriver.StartSession(context.Background(), SessionConfig{
		SessionID: "sess-imm-err",
		ModelID:   "test-model",
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	errSess := &immediateErrSession{
		Session:     rawSession,
		errToReturn: errs.New(errs.CategoryInternal, "immediate stream failure"),
	}

	meter := NewSilentMeter("sess-imm-err", MeterLimits{
		MaxCumulativeInputTokens: 100,
		AllowUnknownUsage:        false,
	})
	ms := NewMeteredSessionWithMeter(errSess, meter)

	ctx := context.Background()
	_, streamErr := ms.StreamTurn(ctx, TurnInput{TurnID: "turn-err", Prompt: "test prompt"})
	if streamErr == nil {
		t.Fatalf("expected immediate error, got nil")
	}

	snap := ms.Meter().Checkpoint()
	if snap.Status != SessionStatusPausedBudgetExceeded {
		t.Errorf("expected session to pause after immediate stream error with configured token limit and AllowUnknownUsage=false, got status %q", snap.Status)
	}
	if snap.ExceededDimension != "usage_unknown" {
		t.Errorf("expected ExceededDimension 'usage_unknown', got %q", snap.ExceededDimension)
	}

	// Subsequent turn rejected due to pause
	_, nextErr := ms.ExecuteTurn(ctx, TurnInput{TurnID: "turn-next", Prompt: "retry"})
	if nextErr == nil {
		t.Errorf("expected turn to be rejected while paused")
	}
}

type noUsageEventSession struct {
	Session
}

func (s *noUsageEventSession) StreamTurn(_ context.Context, input TurnInput) (EventStream, error) {
	stream := NewChannelEventStream(4)
	go func() {
		stream.Send(DriverEvent{
			Kind:      EventContentDelta,
			SessionID: s.ID(),
			TurnID:    input.TurnID,
			Delta:     "hello world",
			Timestamp: time.Now(),
		})
		stream.CloseWithError(nil)
	}()
	return stream, nil
}

func TestMetering_StreamTurn_StreamWithoutUsageRecordsUnknown(t *testing.T) {
	// P0-10: a stream that emits events but none with Usage -> the operation reports
	// unknown (not zero) and, with AllowUnknownUsage=false and a configured limit,
	// suspends usage_unknown; a stream with usage events accumulates from KnownZeroUsage() and stays known.
	fakeDriver := NewFakeDriver("fake-stream-nousage")
	rawSession, err := fakeDriver.StartSession(context.Background(), SessionConfig{
		SessionID: "sess-no-usage-ev",
		ModelID:   "test-model",
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	noUsageSess := &noUsageEventSession{Session: rawSession}
	meter := NewSilentMeter("sess-no-usage-ev", MeterLimits{
		MaxCumulativeTotalTokens: 100,
		AllowUnknownUsage:        false,
	})
	ms := NewMeteredSessionWithMeter(noUsageSess, meter)

	ctx := context.Background()
	stream, err := ms.StreamTurn(ctx, TurnInput{TurnID: "t-no-usage", Prompt: "hello"})
	if err != nil {
		t.Fatalf("StreamTurn failed: %v", err)
	}

	// Drain stream
	for {
		_, recvErr := stream.Recv()
		if recvErr != nil {
			break
		}
	}

	// Give background defer a moment to finalize
	time.Sleep(20 * time.Millisecond)

	snap := ms.Meter().Checkpoint()
	if snap.Status != SessionStatusPausedBudgetExceeded {
		t.Errorf("expected session to pause when stream emitted no usage event and AllowUnknownUsage=false, got status %s", snap.Status)
	}
	if snap.ExceededDimension != "usage_unknown" {
		t.Errorf("expected ExceededDimension 'usage_unknown', got %q", snap.ExceededDimension)
	}
}

func TestMetering_UnprovableLimitsVocabularyAndShape(t *testing.T) {
	// P0-11: UnprovableLimits vocabulary and shape: only the four closed names,
	// only configured limits, sorted, deduplicated, never populated when AllowUnknownUsage=false
	meter := NewSilentMeter("sess-vocab", MeterLimits{
		MaxCumulativeTotalTokens:  100,
		MaxCumulativeInputTokens:  100,
		MaxCumulativeCachedTokens: 100,
		MaxCumulativeOutputTokens: 100,
		AllowUnknownUsage:         true,
	})

	// All unknown
	_, snap := meter.RecordOperationEnd(time.Now(), TokenUsage{}, nil, nil)
	expected := []string{
		"max_cumulative_cached_tokens",
		"max_cumulative_input_tokens",
		"max_cumulative_output_tokens",
		"max_cumulative_total_tokens",
	}
	if len(snap.UnprovableLimits) != 4 {
		t.Fatalf("expected 4 unprovable limits, got %v", snap.UnprovableLimits)
	}
	for i, exp := range expected {
		if snap.UnprovableLimits[i] != exp {
			t.Errorf("at index %d: expected %q, got %q", i, exp, snap.UnprovableLimits[i])
		}
	}

	// JSON serialization check: unprovable_limits present
	marshaled, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("failed to marshal MeterSnapshot: %v", err)
	}
	if !strings.Contains(string(marshaled), `"unprovable_limits"`) {
		t.Errorf("expected unprovable_limits in JSON, got %s", string(marshaled))
	}

	var roundTrip MeterSnapshot
	if err := json.Unmarshal(marshaled, &roundTrip); err != nil {
		t.Fatalf("failed to unmarshal MeterSnapshot: %v", err)
	}
	if len(roundTrip.UnprovableLimits) != 4 {
		t.Errorf("round-trip UnprovableLimits mismatch: %v", roundTrip.UnprovableLimits)
	}

	// When AllowUnknownUsage=false, UnprovableLimits is never populated
	meterFalse := NewSilentMeter("sess-vocab-false", MeterLimits{
		MaxCumulativeInputTokens: 100,
		AllowUnknownUsage:        false,
	})
	_, snapFalse := meterFalse.RecordOperationEnd(time.Now(), TokenUsage{}, nil, nil)
	if len(snapFalse.UnprovableLimits) != 0 {
		t.Errorf("expected UnprovableLimits empty when AllowUnknownUsage=false, got %v", snapFalse.UnprovableLimits)
	}

	marshaledFalse, _ := json.Marshal(snapFalse)
	if strings.Contains(string(marshaledFalse), `"unprovable_limits"`) {
		t.Errorf("unprovable_limits should be omitted when empty, got %s", string(marshaledFalse))
	}
}
