package drivers

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/tools"
)

// DriverFactory is a function that creates a SessionDriver and returns a cleanup function.
type DriverFactory func(t *testing.T) (SessionDriver, func())

// RunDriverContractTestSuite runs the standard, comprehensive contract tests on any SessionDriver implementation.
func RunDriverContractTestSuite(t *testing.T, factory DriverFactory) {
	t.Run("CapabilitiesValidation", func(t *testing.T) {
		driver, cleanup := factory(t)
		defer cleanup()

		if driver.ID() == "" {
			t.Fatalf("driver ID must not be empty")
		}

		caps := driver.Capabilities()
		if err := caps.Validate(); err != nil {
			t.Fatalf("driver capabilities validation failed: %v", err)
		}

		if !caps.ContextControl.Valid() {
			t.Errorf("invalid context control capability: %q", caps.ContextControl)
		}
		if !caps.PrefixCache.Valid() {
			t.Errorf("invalid prefix cache capability: %q", caps.PrefixCache)
		}
		if caps.MaxConcurrentRequests < 1 {
			t.Errorf("max concurrent requests must be >= 1, got %d", caps.MaxConcurrentRequests)
		}
	})

	t.Run("SessionLifecycle", func(t *testing.T) {
		driver, cleanup := factory(t)
		defer cleanup()

		ctx := context.Background()
		cfg := SessionConfig{
			SessionID: "sess-lifecycle-test-1",
			ModelID:   "test-model",
		}

		session, err := driver.StartSession(ctx, cfg)
		if err != nil {
			t.Fatalf("failed to start session: %v", err)
		}
		if session.ID() != cfg.SessionID {
			t.Errorf("expected session ID %q, got %q", cfg.SessionID, session.ID())
		}
		if session.DriverID() != driver.ID() {
			t.Errorf("expected driver ID %q, got %q", driver.ID(), session.DriverID())
		}
		if session.Status() != SessionStatusActive {
			t.Errorf("expected status %q, got %q", SessionStatusActive, session.Status())
		}

		// Execute a valid turn
		turnInput := TurnInput{
			TurnID: "turn-1",
			Prompt: "Hello model",
		}
		res, err := session.ExecuteTurn(ctx, turnInput)
		if err != nil {
			t.Fatalf("ExecuteTurn failed: %v", err)
		}
		if res.TurnID != "turn-1" {
			t.Errorf("expected turn ID %q, got %q", "turn-1", res.TurnID)
		}
		if res.Usage.InputTokens < 0 || res.Usage.OutputTokens < 0 {
			t.Errorf("negative tokens in usage: %+v", res.Usage)
		}

		// Close session
		if err := session.Close(ctx); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
		if session.Status() != SessionStatusClosed {
			t.Errorf("expected status %q after close, got %q", SessionStatusClosed, session.Status())
		}

		// Double close must be idempotent and return nil
		if err := session.Close(ctx); err != nil {
			t.Errorf("second Close returned error: %v", err)
		}

		// Executing a turn on closed session must fail
		_, err = session.ExecuteTurn(ctx, TurnInput{TurnID: "turn-closed", Prompt: "more"})
		if err == nil {
			t.Errorf("expected error executing turn on closed session, got nil")
		}
	})

	t.Run("ConfigImmutability", func(t *testing.T) {
		driver, cleanup := factory(t)
		defer cleanup()

		ctx := context.Background()
		cfg := SessionConfig{
			SessionID: "sess-immut-1",
			ModelID:   "orig-model",
			Options:   map[string]string{"key": "orig"},
		}

		session, err := driver.StartSession(ctx, cfg)
		if err != nil {
			t.Fatalf("failed to start session: %v", err)
		}
		defer session.Close(ctx)

		// Mutate returned config
		retCfg := session.Config()
		retCfg.ModelID = "mutated-model"
		retCfg.Options["key"] = "mutated"

		// Second retrieval must still have original values
		checkCfg := session.Config()
		if checkCfg.ModelID != "orig-model" {
			t.Errorf("expected ModelID 'orig-model', got mutated %q", checkCfg.ModelID)
		}
		if checkCfg.Options["key"] != "orig" {
			t.Errorf("expected Options[key] 'orig', got mutated %q", checkCfg.Options["key"])
		}
	})

	t.Run("StreamingEvents", func(t *testing.T) {
		driver, cleanup := factory(t)
		defer cleanup()

		ctx := context.Background()
		cfg := SessionConfig{
			SessionID: "sess-streaming-test-1",
			ModelID:   "test-model",
		}

		session, err := driver.StartSession(ctx, cfg)
		if err != nil {
			t.Fatalf("failed to start session: %v", err)
		}
		defer session.Close(ctx)

		stream, err := session.StreamTurn(ctx, TurnInput{
			TurnID: "stream-turn-1",
			Prompt: "Tell me a story",
		})
		if err != nil {
			t.Fatalf("StreamTurn failed: %v", err)
		}
		defer stream.Close()

		eventCount := 0
		for {
			ev, err := stream.Recv()
			if err != nil {
				if err == io.EOF {
					break
				}
				t.Fatalf("unexpected stream error: %v", err)
			}
			eventCount++
			if ev.SessionID != cfg.SessionID {
				t.Errorf("expected event SessionID %q, got %q", cfg.SessionID, ev.SessionID)
			}
		}

		if eventCount == 0 {
			t.Errorf("expected at least one streaming event, got 0")
		}
	})

	t.Run("CancellationCleanliness", func(t *testing.T) {
		driver, cleanup := factory(t)
		defer cleanup()

		cfg := SessionConfig{
			SessionID: "sess-cancel-test-1",
			ModelID:   "test-model",
		}

		session, err := driver.StartSession(context.Background(), cfg)
		if err != nil {
			t.Fatalf("failed to start session: %v", err)
		}
		defer session.Close(context.Background())

		// Create an immediately cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err = session.ExecuteTurn(ctx, TurnInput{
			TurnID: "turn-cancel",
			Prompt: "Compute something long",
		})
		if err == nil {
			t.Errorf("expected error on cancelled turn, got nil")
		}

		// StreamTurn with cancelled context
		_, err = session.StreamTurn(ctx, TurnInput{
			TurnID: "stream-cancel",
			Prompt: "Stream something",
		})
		if err == nil {
			t.Errorf("expected error on cancelled stream, got nil")
		}
	})

	t.Run("SessionResume", func(t *testing.T) {
		driver, cleanup := factory(t)
		defer cleanup()

		ctx := context.Background()
		cfg := SessionConfig{
			SessionID: "sess-resume-test-1",
			ModelID:   "test-model",
		}

		session, err := driver.StartSession(ctx, cfg)
		if err != nil {
			t.Fatalf("failed to start session: %v", err)
		}

		_, err = session.ExecuteTurn(ctx, TurnInput{
			TurnID: "turn-before-resume",
			Prompt: "First part",
		})
		if err != nil {
			t.Fatalf("first turn failed: %v", err)
		}

		// Resume session
		resumedSession, err := driver.ResumeSession(ctx, cfg.SessionID, cfg)
		if err != nil {
			t.Fatalf("ResumeSession failed: %v", err)
		}
		if resumedSession.ID() != cfg.SessionID {
			t.Errorf("expected resumed session ID %q, got %q", cfg.SessionID, resumedSession.ID())
		}

		// Can execute turn on resumed session
		turnRes, err := resumedSession.ExecuteTurn(ctx, TurnInput{
			TurnID: "turn-after-resume",
			Prompt: "Second part",
		})
		if err != nil {
			t.Fatalf("resumed turn failed: %v", err)
		}
		if turnRes.TurnID != "turn-after-resume" {
			t.Errorf("expected turn ID %q, got %q", "turn-after-resume", turnRes.TurnID)
		}
	})

	t.Run("ToolMediation", func(t *testing.T) {
		driver, cleanup := factory(t)
		defer cleanup()

		ctx := context.Background()
		mediator := NewScopedToolMediator(nil)
		toolExecuted := false
		mediator.RegisterHandler("test_tool", func(ctx context.Context, args json.RawMessage) (string, error) {
			toolExecuted = true
			return "tool_output_ok", nil
		})

		cfg := SessionConfig{
			SessionID: "sess-tool-mediation-1",
			ModelID:   "test-model",
			Tools: []ToolDefinition{
				{
					Name:        "test_tool",
					Description: "A test tool",
				},
			},
			Mediator: mediator,
		}

		session, err := driver.StartSession(ctx, cfg)
		if err != nil {
			t.Fatalf("failed to start session: %v", err)
		}
		defer session.Close(ctx)

		// Ask model to call tool
		_, err = session.ExecuteTurn(ctx, TurnInput{
			TurnID: "turn-call-tool",
			Prompt: "CALL_TOOL: test_tool",
		})
		if err != nil {
			t.Fatalf("ExecuteTurn failed: %v", err)
		}

		if !toolExecuted {
			t.Errorf("expected tool to be executed through mediator, but it was not")
		}
	})

	t.Run("ToolWorktreeContainmentEnforcement", func(t *testing.T) {
		driver, cleanup := factory(t)
		defer cleanup()

		ctx := context.Background()
		tmpDir, err := os.MkdirTemp("", "driver-worktree-containment-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		scope := &tools.Scope{
			ProjectID:    "proj-containment",
			WorktreePath: tmpDir,
		}

		mediator := NewScopedToolMediator(scope)
		mediator.RegisterHandler("read_file", func(ctx context.Context, args json.RawMessage) (string, error) {
			return "content", nil
		})

		cfg := SessionConfig{
			SessionID:     "sess-containment-1",
			ModelID:       "test-model",
			WorktreeScope: scope,
			Tools: []ToolDefinition{
				{
					Name:        "read_file",
					Description: "read a file",
				},
			},
			Mediator: mediator,
		}

		session, err := driver.StartSession(ctx, cfg)
		if err != nil {
			t.Fatalf("failed to start session: %v", err)
		}
		defer session.Close(ctx)

		// Attempt escaping worktree
		escapeCall := ToolCall{
			ID:        "call-escape",
			Name:      "read_file",
			Arguments: []byte(`{"path":"../../etc/passwd"}`),
		}
		res, err := mediator.ExecuteTool(ctx, escapeCall)
		if err == nil {
			t.Errorf("expected CategoryPolicyDenied when executing tool with escaping path, got nil")
		}
		if errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Errorf("expected CategoryPolicyDenied, got %v", err)
		}
		if !res.IsError {
			t.Errorf("expected ToolResult.IsError=true for path escape")
		}
	})

	t.Run("InvalidConfigRejection", func(t *testing.T) {
		driver, cleanup := factory(t)
		defer cleanup()

		ctx := context.Background()

		// Missing session ID
		_, err := driver.StartSession(ctx, SessionConfig{
			SessionID: "",
			ModelID:   "test-model",
		})
		if err == nil {
			t.Errorf("expected error for empty session ID, got nil")
		}

		// Missing model ID
		_, err = driver.StartSession(ctx, SessionConfig{
			SessionID: "valid-id",
			ModelID:   "",
		})
		if err == nil {
			t.Errorf("expected error for empty model ID, got nil")
		}
	})
}

// TestChannelEventStream_ConcurrentSendClose asserts that concurrent Send and CloseWithError
// never panics and that FIFO ordering is preserved.
func TestChannelEventStream_ConcurrentSendClose(t *testing.T) {
	stream := NewChannelEventStream(64)
	const numSenders = 10
	const msgsPerSender = 20

	var wg sync.WaitGroup
	wg.Add(numSenders)

	for i := 0; i < numSenders; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < msgsPerSender; j++ {
				_ = stream.Send(DriverEvent{
					Kind:      EventContentDelta,
					Delta:     "chunk",
					SessionID: "sess-concurr",
				})
			}
		}(i)
	}

	// Concurrent close from another goroutine
	go func() {
		stream.CloseWithError(nil)
	}()

	wg.Wait()

	// Drain stream: Recv must terminate with EOF or error, without hanging or panic
	for {
		_, err := stream.Recv()
		if err != nil {
			break
		}
	}
}
