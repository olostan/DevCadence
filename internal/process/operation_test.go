package process

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestOperationCompletesWithinThreshold(t *testing.T) {
	mgr := NewOperationManager(nil, nil)
	dir := testDir(t)

	snap, err := mgr.StartOperation(context.Background(), Spec{
		Executable: "sh",
		Args:       []string{"-c", "echo op-instant"},
		Dir:        dir,
		Env:        BaseEnv(),
		Timeout:    2 * time.Second,
	}, 1*time.Second)
	if err != nil {
		t.Fatalf("StartOperation: %v", err)
	}

	if snap.Status != OperationStatusCompleted {
		t.Errorf("Expected status completed, got %v", snap.Status)
	}
	if snap.Result == nil || snap.Result.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %+v", snap.Result)
	}
	if !strings.Contains(string(snap.Result.Stdout), "op-instant") {
		t.Errorf("Stdout mismatch: %s", string(snap.Result.Stdout))
	}
}

func TestOperationYieldsAfterThreshold(t *testing.T) {
	mgr := NewOperationManager(nil, nil)
	dir := testDir(t)

	// Command sleeps for 500ms, but yield threshold is 50ms
	start := time.Now()
	snap, err := mgr.StartOperation(context.Background(), Spec{
		Executable: "sh",
		Args:       []string{"-c", "sleep 0.3; echo done-bg"},
		Dir:        dir,
		Env:        BaseEnv(),
		Timeout:    2 * time.Second,
	}, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("StartOperation: %v", err)
	}

	yieldDuration := time.Since(start)
	if yieldDuration > 200*time.Millisecond {
		t.Errorf("StartOperation took %v to yield, expected ~50ms", yieldDuration)
	}

	if snap.Status != OperationStatusRunning {
		t.Fatalf("Expected status running on yield, got %v", snap.Status)
	}
	if snap.YieldRecommendation == "" {
		t.Errorf("Expected yield recommendation to be present")
	}

	// Now wait for completion via WaitOperation
	finalSnap, err := mgr.WaitOperation(context.Background(), snap.ID)
	if err != nil {
		t.Fatalf("WaitOperation: %v", err)
	}
	if finalSnap.Status != OperationStatusCompleted {
		t.Errorf("Expected final status completed, got %v", finalSnap.Status)
	}
	if finalSnap.Result == nil || !strings.Contains(string(finalSnap.Result.Stdout), "done-bg") {
		t.Errorf("Expected 'done-bg' in stdout, got %+v", finalSnap.Result)
	}
}

func TestOperationCompletionBeforeSubscription(t *testing.T) {
	mgr := NewOperationManager(nil, nil)
	dir := testDir(t)

	snap, err := mgr.StartOperation(context.Background(), Spec{
		Executable: "sh",
		Args:       []string{"-c", "sleep 0.1; echo subscribed-late"},
		Dir:        dir,
		Env:        BaseEnv(),
		Timeout:    2 * time.Second,
	}, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("StartOperation: %v", err)
	}
	if snap.Status != OperationStatusRunning {
		t.Fatalf("Expected running, got %v", snap.Status)
	}

	// Wait 200ms so it completes in background before we subscribe
	time.Sleep(200 * time.Millisecond)

	// Subscribe late: must return terminal state immediately
	subSnap, err := mgr.WaitOperation(context.Background(), snap.ID)
	if err != nil {
		t.Fatalf("WaitOperation: %v", err)
	}
	if subSnap.Status != OperationStatusCompleted {
		t.Errorf("Expected completed, got %v", subSnap.Status)
	}
	if subSnap.Result == nil || !strings.Contains(string(subSnap.Result.Stdout), "subscribed-late") {
		t.Errorf("Expected 'subscribed-late' in stdout, got %+v", subSnap.Result)
	}
}

func TestOperationIdempotentCancellation(t *testing.T) {
	mgr := NewOperationManager(nil, nil)
	dir := testDir(t)

	snap, err := mgr.StartOperation(context.Background(), Spec{
		Executable: "sh",
		Args:       []string{"-c", "sleep 5"},
		Dir:        dir,
		Env:        BaseEnv(),
		Timeout:    10 * time.Second,
	}, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("StartOperation: %v", err)
	}

	// First cancellation
	cancelSnap, delivered, err := mgr.CancelOperation(snap.ID)
	if err != nil {
		t.Fatalf("CancelOperation: %v", err)
	}
	if !delivered {
		t.Errorf("Expected cancellation to be delivered on first call")
	}
	if cancelSnap.Status != OperationStatusCancelled {
		t.Errorf("Expected status cancelled, got %v", cancelSnap.Status)
	}

	// Second cancellation: must be idempotent and report delivered = false
	secondSnap, secondDelivered, err := mgr.CancelOperation(snap.ID)
	if err != nil {
		t.Fatalf("Second CancelOperation: %v", err)
	}
	if secondDelivered {
		t.Errorf("Expected second cancellation delivered to be false")
	}
	if secondSnap.Status != OperationStatusCancelled {
		t.Errorf("Expected status cancelled, got %v", secondSnap.Status)
	}
}

func TestOperationTimeout(t *testing.T) {
	mgr := NewOperationManager(nil, nil)
	dir := testDir(t)

	snap, err := mgr.StartOperation(context.Background(), Spec{
		Executable: "sh",
		Args:       []string{"-c", "sleep 5"},
		Dir:        dir,
		Env:        BaseEnv(),
		Timeout:    150 * time.Millisecond,
	}, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("StartOperation: %v", err)
	}

	finalSnap, err := mgr.WaitOperation(context.Background(), snap.ID)
	if err != nil {
		t.Fatalf("WaitOperation: %v", err)
	}
	if finalSnap.Status != OperationStatusTimeout {
		t.Errorf("Expected status timeout, got %v", finalSnap.Status)
	}
}
