package drivers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// mockDirectClient implements DirectAPIClient for testing.
type mockDirectClient struct {
	delay        time.Duration
	lastMessages []DirectMessage
	lastReq      DirectAPIRequest
}

func (m *mockDirectClient) Complete(ctx context.Context, req DirectAPIRequest) (DirectAPIResponse, error) {
	if err := ctx.Err(); err != nil {
		return DirectAPIResponse{}, err
	}

	m.lastReq = req
	m.lastMessages = req.Messages

	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return DirectAPIResponse{}, ctx.Err()
		case <-time.After(m.delay):
		}
	}

	var toolCalls []ToolCall
	if strings.Contains(req.Prompt, "CALL_TOOL:") {
		parts := strings.Split(req.Prompt, "CALL_TOOL:")
		toolName := strings.TrimSpace(parts[1])
		toolCalls = append(toolCalls, ToolCall{
			ID:        "tc-direct-1",
			Name:      toolName,
			Arguments: []byte(`{"param":"direct_val"}`),
		})
	}

	return DirectAPIResponse{
		Content:   fmt.Sprintf("Direct response to: %s", req.Prompt),
		ToolCalls: toolCalls,
		Usage:     KnownUsage(int64(len(req.Prompt)), 10, 25),
	}, nil
}

func (m *mockDirectClient) Stream(ctx context.Context, req DirectAPIRequest) (EventStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.lastReq = req

	stream := NewChannelEventStream(8)
	go func() {
		if m.delay > 0 {
			select {
			case <-ctx.Done():
				stream.CloseWithError(ctx.Err())
				return
			case <-time.After(m.delay):
			}
		}

		words := strings.Fields(fmt.Sprintf("Direct stream response to: %s", req.Prompt))
		for _, w := range words {
			if !stream.Send(DriverEvent{
				Kind:      EventContentDelta,
				Delta:     w + " ",
				Timestamp: time.Now(),
			}) {
				return
			}
		}

		usage := KnownUsage(int64(len(req.Prompt)), 0, int64(len(words)*3))
		stream.Send(DriverEvent{
			Kind:      EventTurnCompleted,
			Usage:     &usage,
			Timestamp: time.Now(),
		})
		stream.CloseWithError(nil)
	}()

	return stream, nil
}

func TestDirectAPIDriverContract(t *testing.T) {
	RunDriverContractTestSuite(t, func(t *testing.T) (SessionDriver, func()) {
		client := &mockDirectClient{}
		driver := MustNewDirectAPIDriver("direct-api-test-driver", client)
		return driver, func() {}
	})
}

func TestDirectAPIDriver_ConstructorValidation(t *testing.T) {
	// Test client == nil
	_, err := NewDirectAPIDriver("nil-client", nil)
	if err == nil {
		t.Fatalf("expected error for nil client, got nil")
	}
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("expected CategoryInvalidArgument, got %v", err)
	}

	// Test invalid capabilities
	client := &mockDirectClient{}
	invalidCaps := DriverCapabilities{
		Kind:                  "invalid-kind",
		SessionMode:           protocol.SessionStatelessPerCall,
		ContextControl:        protocol.ContextControlExactStateless,
		PrefixCache:           protocol.PrefixCacheExplicit,
		MaxConcurrentRequests: 1,
	}
	_, err = NewDirectAPIDriver("invalid-caps", client, DirectAPIOptions{Capabilities: &invalidCaps})
	if err == nil {
		t.Fatalf("expected error for invalid capabilities, got nil")
	}
}

func TestDirectAPIDriver_ResumeAfterClosePreservesHistory(t *testing.T) {
	client := &mockDirectClient{}
	driver := MustNewDirectAPIDriver("direct-api-history", client)
	ctx := context.Background()

	session, err := driver.StartSession(ctx, SessionConfig{
		SessionID:              "sess-history-preserve",
		ModelID:                "direct-model-v1",
		MaxOutputTokensPerCall: 4096,
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	_, err = session.ExecuteTurn(ctx, TurnInput{
		TurnID: "turn-1",
		Prompt: "First turn prompt",
	})
	if err != nil {
		t.Fatalf("turn 1 failed: %v", err)
	}

	// Close session
	if err := session.Close(ctx); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if session.Status() != SessionStatusClosed {
		t.Errorf("expected closed status, got %s", session.Status())
	}

	// Resume session
	resumed, err := driver.ResumeSession(ctx, "sess-history-preserve", SessionConfig{
		SessionID:              "sess-history-preserve",
		ModelID:                "direct-model-v1",
		MaxOutputTokensPerCall: 4096,
	})
	if err != nil {
		t.Fatalf("ResumeSession failed: %v", err)
	}
	if resumed.Status() != SessionStatusActive {
		t.Errorf("expected active status on resumed session, got %s", resumed.Status())
	}

	// Execute turn 2 on resumed session
	_, err = resumed.ExecuteTurn(ctx, TurnInput{
		TurnID: "turn-2",
		Prompt: "Second turn prompt",
	})
	if err != nil {
		t.Fatalf("turn 2 failed: %v", err)
	}

	// Verify client received both turn 1 and turn 2 messages in conversation history
	if len(client.lastMessages) < 2 {
		t.Fatalf("expected at least 2 messages in history, got %d", len(client.lastMessages))
	}
	foundTurn1 := false
	for _, m := range client.lastMessages {
		if strings.Contains(m.Content, "First turn prompt") {
			foundTurn1 = true
			break
		}
	}
	if !foundTurn1 {
		t.Errorf("expected history to preserve First turn prompt, but was not found in: %+v", client.lastMessages)
	}
}

func TestDirectAPIDriver_Cancellation(t *testing.T) {
	client := &mockDirectClient{delay: 200 * time.Millisecond}
	driver := MustNewDirectAPIDriver("direct-api-cancel", client)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	session, err := driver.StartSession(context.Background(), SessionConfig{
		SessionID:              "sess-api-cancel",
		ModelID:                "direct-model-v1",
		MaxOutputTokensPerCall: 4096,
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	_, err = session.ExecuteTurn(ctx, TurnInput{
		TurnID: "turn-cancel-api",
		Prompt: "slow direct query",
	})
	if err == nil {
		t.Errorf("expected cancellation error, got nil")
	}
}

func TestDirectAPIDriver_ToolMediationExecution(t *testing.T) {
	client := &mockDirectClient{}
	driver := MustNewDirectAPIDriver("direct-api-tool", client)

	mediator := NewScopedToolMediator(nil)
	toolRan := false
	mediator.RegisterHandler("fetch_data", func(ctx context.Context, args json.RawMessage) (string, error) {
		toolRan = true
		return `{"status":"ok"}`, nil
	})

	session, err := driver.StartSession(context.Background(), SessionConfig{
		SessionID:              "sess-api-tool-exec",
		ModelID:                "direct-model-v1",
		MaxOutputTokensPerCall: 4096,
		Tools: []ToolDefinition{
			{Name: "fetch_data", Description: "fetches data"},
		},
		Mediator: mediator,
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	res, err := session.ExecuteTurn(context.Background(), TurnInput{
		TurnID: "turn-with-tool",
		Prompt: "CALL_TOOL: fetch_data",
	})
	if err != nil {
		t.Fatalf("ExecuteTurn failed: %v", err)
	}

	if !toolRan {
		t.Errorf("expected tool fetch_data to be executed through mediator")
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Name != "fetch_data" {
		t.Errorf("expected 1 tool call 'fetch_data', got %+v", res.ToolCalls)
	}
}

func TestDirectAPIDriver_StreamingEvents(t *testing.T) {
	client := &mockDirectClient{}
	driver := MustNewDirectAPIDriver("direct-api-stream", client)

	session, err := driver.StartSession(context.Background(), SessionConfig{
		SessionID:              "sess-api-stream",
		ModelID:                "direct-model-v1",
		MaxOutputTokensPerCall: 4096,
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	stream, err := session.StreamTurn(context.Background(), TurnInput{
		TurnID: "turn-stream",
		Prompt: "hello stream",
	})
	if err != nil {
		t.Fatalf("StreamTurn failed: %v", err)
	}
	defer stream.Close()

	var deltas []string
	for {
		ev, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("unexpected stream error: %v", err)
		}
		if ev.Delta != "" {
			deltas = append(deltas, ev.Delta)
		}
	}

	if len(deltas) == 0 {
		t.Errorf("expected content deltas in stream, got 0")
	}
}

type trackingStreamClient struct {
	streamClosed    bool
	contextCanceled bool
	mu              sync.Mutex
}

func (c *trackingStreamClient) Complete(ctx context.Context, req DirectAPIRequest) (DirectAPIResponse, error) {
	return DirectAPIResponse{}, nil
}

func (c *trackingStreamClient) Stream(ctx context.Context, req DirectAPIRequest) (EventStream, error) {
	stream := NewChannelEventStream(8)
	stream.SetOnClose(func() {
		c.mu.Lock()
		c.streamClosed = true
		c.mu.Unlock()
	})

	go func() {
		defer stream.Close()
		// Send initial event
		stream.Send(DriverEvent{
			Kind:  EventContentDelta,
			Delta: "first word",
		})

		// Block waiting for context cancellation or stream close
		<-ctx.Done()
		c.mu.Lock()
		c.contextCanceled = true
		c.mu.Unlock()
	}()

	return stream, nil
}

func TestDirectAPIDriver_StreamEarlyCloseCancelsUnderlyingStream(t *testing.T) {
	client := &trackingStreamClient{}
	driver := MustNewDirectAPIDriver("direct-api-cancel-stream", client)

	ctx := context.Background()
	session, err := driver.StartSession(ctx, SessionConfig{
		SessionID:              "sess-api-cancel-stream",
		ModelID:                "direct-model-v1",
		MaxOutputTokensPerCall: 4096,
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	stream, err := session.StreamTurn(ctx, TurnInput{
		TurnID: "turn-cancel",
		Prompt: "hello",
	})
	if err != nil {
		t.Fatalf("StreamTurn failed: %v", err)
	}

	// Read 1 event
	ev, err := stream.Recv()
	if err != nil {
		t.Fatalf("expected first event, got err: %v", err)
	}
	if ev.Delta != "first word" {
		t.Errorf("expected 'first word', got %q", ev.Delta)
	}

	// Close stream early
	if err := stream.Close(); err != nil {
		t.Fatalf("stream.Close failed: %v", err)
	}

	// Wait briefly for goroutine to receive cancellation / close hook
	time.Sleep(50 * time.Millisecond)

	client.mu.Lock()
	closed := client.streamClosed
	canceled := client.contextCanceled
	client.mu.Unlock()

	if !closed {
		t.Errorf("expected underlying stream.Close() to be called on consumer early close")
	}
	if !canceled {
		t.Errorf("expected stream context to be canceled on consumer early close")
	}
}

func TestDirectAPIDriver_MaxOutputTokensPropagated(t *testing.T) {
	client := &mockDirectClient{}
	driver := MustNewDirectAPIDriver("direct-api-tokens", client)

	ctx := context.Background()
	const maxTokens = int64(2048)
	session, err := driver.StartSession(ctx, SessionConfig{
		SessionID:              "sess-tokens-check",
		ModelID:                "direct-model-v1",
		MaxOutputTokensPerCall: maxTokens,
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	// 1. Complete turn receives MaxOutputTokens
	_, err = session.ExecuteTurn(ctx, TurnInput{
		TurnID: "turn-tokens-complete",
		Prompt: "test tokens complete",
	})
	if err != nil {
		t.Fatalf("ExecuteTurn failed: %v", err)
	}
	if client.lastReq.MaxOutputTokens != maxTokens {
		t.Errorf("ExecuteTurn: expected MaxOutputTokens=%d, got %d", maxTokens, client.lastReq.MaxOutputTokens)
	}

	// 2. Stream turn receives MaxOutputTokens
	stream, err := session.StreamTurn(ctx, TurnInput{
		TurnID: "turn-tokens-stream",
		Prompt: "test tokens stream",
	})
	if err != nil {
		t.Fatalf("StreamTurn failed: %v", err)
	}
	defer stream.Close()
	if client.lastReq.MaxOutputTokens != maxTokens {
		t.Errorf("StreamTurn: expected MaxOutputTokens=%d, got %d", maxTokens, client.lastReq.MaxOutputTokens)
	}
}
