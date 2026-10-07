package drivers

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestFakeDriverContract(t *testing.T) {
	RunDriverContractTestSuite(t, func(t *testing.T) (SessionDriver, func()) {
		driver := NewFakeDriver("fake-contract-driver")
		return driver, func() {}
	})
}

func TestFakeDriver_CancellationWithDelay(t *testing.T) {
	driver := NewFakeDriver("fake-delay-driver", FakeDriverOptions{
		Delay: 200 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	session, err := driver.StartSession(context.Background(), SessionConfig{
		SessionID: "sess-delay-cancel",
		ModelID:   "test-model",
	})
	if err != nil {
		t.Fatalf("failed to start session: %v", err)
	}

	start := time.Now()
	_, err = session.ExecuteTurn(ctx, TurnInput{
		TurnID: "turn-timeout",
		Prompt: "wait",
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Errorf("expected timeout/cancellation error, got nil")
	}
	if elapsed >= 180*time.Millisecond {
		t.Errorf("cancellation did not halt execution promptly, elapsed: %v", elapsed)
	}
}

func TestFakeDriver_CustomTurnHandler(t *testing.T) {
	driver := NewFakeDriver("fake-custom-driver")
	driver.SetTurnHandler("scripted-turn", func(ctx context.Context, input TurnInput) (TurnResult, error) {
		return TurnResult{
			TurnID:  input.TurnID,
			Content: "scripted-turn-success",
			Usage:   KnownUsage(10, 0, 20),
		}, nil
	})

	session, err := driver.StartSession(context.Background(), SessionConfig{
		SessionID: "sess-custom",
		ModelID:   "test-model",
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	res, err := session.ExecuteTurn(context.Background(), TurnInput{
		TurnID: "scripted-turn",
		Prompt: "anything",
	})
	if err != nil {
		t.Fatalf("ExecuteTurn failed: %v", err)
	}
	if res.Content != "scripted-turn-success" {
		t.Errorf("expected content 'scripted-turn-success', got %q", res.Content)
	}
}

func TestFakeDriver_StreamTurnCancellation(t *testing.T) {
	driver := NewFakeDriver("fake-stream-cancel", FakeDriverOptions{
		Delay: 200 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	session, err := driver.StartSession(context.Background(), SessionConfig{
		SessionID: "sess-stream-timeout",
		ModelID:   "test-model",
	})
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	stream, err := session.StreamTurn(ctx, TurnInput{
		TurnID: "stream-turn-timeout",
		Prompt: "stream delay",
	})
	if err != nil {
		t.Fatalf("StreamTurn failed: %v", err)
	}
	defer stream.Close()

	_, err = stream.Recv()
	if err == nil {
		t.Errorf("expected error on cancelled stream, got nil")
	}
	if !strings.Contains(err.Error(), "context") {
		t.Errorf("expected context error, got %v", err)
	}
}
