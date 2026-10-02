package drivers

import (
	"strings"
	"testing"
)

func TestLoopDetector_RepeatingIdenticalFailedToolCalls(t *testing.T) {
	detector := NewSemanticLoopDetector(LoopDetectorConfig{
		MaxConsecutiveFailedCalls: 3,
		MaxOscillatingEdits:       2,
	})

	call := ToolCall{
		ID:        "c1",
		Name:      "run_test",
		Arguments: []byte(`{"suite":"unit"}`),
	}

	// 1st failure
	detected, _ := detector.RecordToolCall(call, true)
	if detected {
		t.Fatalf("unexpected loop detected on 1st failure")
	}

	// 2nd failure
	detected, _ = detector.RecordToolCall(call, true)
	if detected {
		t.Fatalf("unexpected loop detected on 2nd failure")
	}

	// 3rd failure: threshold reached
	detected, reason := detector.RecordToolCall(call, true)
	if !detected {
		t.Fatalf("expected loop detected on 3rd identical failure")
	}
	if !strings.Contains(reason, "run_test") || !strings.Contains(reason, "DCI-045") {
		t.Errorf("reason should mention tool and DCI-045, got %q", reason)
	}

	// Resetting
	detector.Reset()
	detected, _ = detector.RecordToolCall(call, true)
	if detected {
		t.Fatalf("after reset, 1st failure should not detect loop")
	}
}

func TestLoopDetector_SuccessResetsCounter(t *testing.T) {
	detector := NewSemanticLoopDetector(LoopDetectorConfig{
		MaxConsecutiveFailedCalls: 3,
	})

	call := ToolCall{
		ID:        "c1",
		Name:      "grep",
		Arguments: []byte(`{"query":"pattern"}`),
	}

	// 2 failures
	detector.RecordToolCall(call, true)
	detector.RecordToolCall(call, true)

	// 1 success
	detector.RecordToolCall(call, false)

	// 1 failure again -> consecutive count should be 1, not 3
	detected, _ := detector.RecordToolCall(call, true)
	if detected {
		t.Errorf("expected count reset after success, but loop was detected")
	}
}

func TestLoopDetector_OscillatingEdits(t *testing.T) {
	detector := NewSemanticLoopDetector(LoopDetectorConfig{
		MaxOscillatingEdits: 2,
	})

	path := "internal/logic.go"
	hashA := HashContent([]byte("state A"))
	hashB := HashContent([]byte("state B"))

	// Edit 1: state A
	detected, _ := detector.RecordFileEdit(path, hashA)
	if detected {
		t.Fatalf("unexpected loop on edit 1")
	}

	// Edit 2: state B
	detected, _ = detector.RecordFileEdit(path, hashB)
	if detected {
		t.Fatalf("unexpected loop on edit 2")
	}

	// Edit 3: state A (1st oscillation: A -> B -> A)
	detected, _ = detector.RecordFileEdit(path, hashA)
	if detected {
		t.Fatalf("unexpected loop on 1st oscillation (threshold is 2)")
	}

	// Edit 4: state B (2nd oscillation: B -> A -> B)
	detected, reason := detector.RecordFileEdit(path, hashB)
	if !detected {
		t.Fatalf("expected loop detected on 2nd oscillation")
	}
	if !strings.Contains(reason, "oscillated") || !strings.Contains(reason, "DCI-045") {
		t.Errorf("reason should mention oscillation and DCI-045, got %q", reason)
	}
}
