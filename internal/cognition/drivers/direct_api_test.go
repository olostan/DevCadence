package drivers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// mockDirectClient implements DirectAPIClient for testing.
type mockDirectClient struct {
	delay time.Duration
}

func (m *mockDirectClient) Complete(ctx context.Context, req DirectAPIRequest) (DirectAPIResponse, error) {
	if err := ctx.Err(); err != nil {
		return DirectAPIResponse{}, err
	}

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
		Usage: TokenUsage{
			InputTokens:  int64(len(req.Prompt)),
			CachedTokens: 10,
			OutputTokens: 25,
		},
	}, nil
}

func (m *mockDirectClient) Stream(ctx context.Context, req DirectAPIRequest) (EventStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

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
			if err := ctx.Err(); err != nil {
				stream.CloseWithError(err)
				return
			}
			stream.Send(DriverEvent{
				Kind:      EventContentDelta,
				Delta:     w + " ",
				Timestamp: time.Now(),
			})
		}

		stream.Send(DriverEvent{
			Kind: EventTurnCompleted,
			Usage: &TokenUsage{
				InputTokens:  int64(len(req.Prompt)),
				OutputTokens: int64(len(words) * 3),
			},
			Timestamp: time.Now(),
		})
		stream.CloseWithError(nil)
	}()

	return stream, nil
}

func TestDirectAPIDriverContract(t *testing.T) {
	RunDriverContractTestSuite(t, func(t *testing.T) (SessionDriver, func()) {
		client := &mockDirectClient{}
		driver := NewDirectAPIDriver("direct-api-test-driver", client)
		return driver, func() {}
	})
}

func TestDirectAPIDriver_Cancellation(t *testing.T) {
	client := &mockDirectClient{delay: 200 * time.Millisecond}
	driver := NewDirectAPIDriver("direct-api-cancel", client)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	session, err := driver.StartSession(context.Background(), SessionConfig{
		SessionID: "sess-api-cancel",
		ModelID:   "direct-model-v1",
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
	driver := NewDirectAPIDriver("direct-api-tool", client)

	mediator := NewScopedToolMediator(nil)
	toolRan := false
	mediator.RegisterHandler("fetch_data", func(ctx context.Context, args json.RawMessage) (string, error) {
		toolRan = true
		return `{"status":"ok"}`, nil
	})

	session, err := driver.StartSession(context.Background(), SessionConfig{
		SessionID: "sess-api-tool-exec",
		ModelID:   "direct-model-v1",
		Mediator:  mediator,
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
	driver := NewDirectAPIDriver("direct-api-stream", client)

	session, err := driver.StartSession(context.Background(), SessionConfig{
		SessionID: "sess-api-stream",
		ModelID:   "direct-model-v1",
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
