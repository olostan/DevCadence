package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

func ledgerTestTime(offsetSeconds int) protocol.Timestamp {
	return protocol.NewTimestamp(time.Date(2026, 9, 24, 12, 0, offsetSeconds, 0, time.UTC))
}

const testPlanDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func executionCreatedEvent(eventID, executionID, planID string) *protocol.SetupLedgerEvent {
	return &protocol.SetupLedgerEvent{
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       eventID,
		ExecutionID:   executionID,
		PlanID:        planID,
		PlanDigest:    testPlanDigest,
		Timestamp:     ledgerTestTime(0),
		Type:          protocol.EventExecutionCreated,
		Payload: protocol.EventPayload{
			ExecutionCreated: &protocol.ExecutionCreatedPayload{
				InitiatedBy: "operator",
				Target:      protocol.TargetAll,
			},
		},
	}
}

func planApprovedEvent(eventID, executionID, planID string) *protocol.SetupLedgerEvent {
	return &protocol.SetupLedgerEvent{
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       eventID,
		ExecutionID:   executionID,
		PlanID:        planID,
		PlanDigest:    testPlanDigest,
		Timestamp:     ledgerTestTime(1),
		Type:          protocol.EventPlanApproved,
		Payload: protocol.EventPayload{
			PlanApproved: &protocol.PlanApprovedPayload{
				ApprovedAuthority: protocol.AuthorityUserConfirmation,
				ApprovedBy:        "operator",
			},
		},
	}
}

func actionStartingEvent(eventID, executionID, planID, actionID string) *protocol.SetupLedgerEvent {
	opKind := protocol.OpKindCreateDirectory
	return &protocol.SetupLedgerEvent{
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       eventID,
		ExecutionID:   executionID,
		PlanID:        planID,
		PlanDigest:    testPlanDigest,
		ActionID:      actionID,
		Timestamp:     ledgerTestTime(2),
		Type:          protocol.EventActionStarting,
		Payload: protocol.EventPayload{
			ActionStarting: &protocol.ActionStartingPayload{
				ActionID:       actionID,
				RecipeID:       "recipe-create-dir",
				RecipeVersion:  "1.0",
				OperationKind:  &opKind,
				IdempotencyKey: "create-dir-" + actionID,
			},
		},
	}
}

func actionTerminatedEvent(eventID, executionID, planID, actionID string, status protocol.ActionStatus) *protocol.SetupLedgerEvent {
	return &protocol.SetupLedgerEvent{
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       eventID,
		ExecutionID:   executionID,
		PlanID:        planID,
		PlanDigest:    testPlanDigest,
		ActionID:      actionID,
		Timestamp:     ledgerTestTime(3),
		Type:          protocol.EventActionTerminated,
		Payload: protocol.EventPayload{
			ActionTerminated: &protocol.ActionTerminatedPayload{
				ActionID: actionID,
				Status:   status,
			},
		},
	}
}

func executionFinishedEvent(eventID, executionID, planID string, status protocol.ExecutionStatus) *protocol.SetupLedgerEvent {
	return &protocol.SetupLedgerEvent{
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       eventID,
		ExecutionID:   executionID,
		PlanID:        planID,
		PlanDigest:    testPlanDigest,
		Timestamp:     ledgerTestTime(4),
		Type:          protocol.EventExecutionFinished,
		Payload: protocol.EventPayload{
			ExecutionFinished: &protocol.ExecutionFinishedPayload{
				Status: status,
			},
		},
	}
}

func TestLedgerAppendAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup-ledger.jsonl")

	l, err := OpenLedger(path)
	if err != nil {
		t.Fatalf("OpenLedger (new): %v", err)
	}
	if got := l.Events(); len(got) != 0 {
		t.Fatalf("new ledger has %d events, want 0", len(got))
	}

	events := []*protocol.SetupLedgerEvent{
		executionCreatedEvent("evt-001", "exec-001", "plan-001"),
		planApprovedEvent("evt-002", "exec-001", "plan-001"),
		actionStartingEvent("evt-003", "exec-001", "plan-001", "act-001"),
		actionTerminatedEvent("evt-004", "exec-001", "plan-001", "act-001", protocol.ActionStatusSucceeded),
		executionFinishedEvent("evt-005", "exec-001", "plan-001", protocol.ExecutionStatusSucceeded),
	}
	for i, e := range events {
		appended, err := l.Append(e)
		if err != nil {
			t.Fatalf("Append event %d: %v", i, err)
		}
		if appended.Sequence != uint64(i+1) {
			t.Fatalf("event %d: sequence = %d, want %d", i, appended.Sequence, i+1)
		}
	}

	reopened, err := OpenLedger(path)
	if err != nil {
		t.Fatalf("OpenLedger (reload): %v", err)
	}
	got := reopened.Events()
	if len(got) != len(events) {
		t.Fatalf("reloaded ledger has %d events, want %d", len(got), len(events))
	}
	for i, e := range got {
		if e.EventID != events[i].EventID {
			t.Errorf("event %d: EventID = %q, want %q", i, e.EventID, events[i].EventID)
		}
		if i > 0 && e.PreviousEventDigest != got[i-1].EventDigest {
			t.Errorf("event %d: PreviousEventDigest does not chain to event %d's EventDigest", i, i-1)
		}
	}

	// Appending further from the reloaded ledger must continue the chain,
	// not restart it.
	next, err := reopened.Append(executionCreatedEvent("evt-006", "exec-002", "plan-002"))
	if err != nil {
		t.Fatalf("Append after reload: %v", err)
	}
	if next.Sequence != uint64(len(events)+1) {
		t.Fatalf("Append after reload: sequence = %d, want %d", next.Sequence, len(events)+1)
	}
	if next.PreviousEventDigest != got[len(got)-1].EventDigest {
		t.Fatalf("Append after reload: previous_event_digest does not chain to prior tip")
	}
}

func TestLedgerFailsClosedOnNonFinalCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup-ledger.jsonl")

	l, err := OpenLedger(path)
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	for _, e := range []*protocol.SetupLedgerEvent{
		executionCreatedEvent("evt-001", "exec-001", "plan-001"),
		planApprovedEvent("evt-002", "exec-001", "plan-001"),
		actionStartingEvent("evt-003", "exec-001", "plan-001", "act-001"),
	} {
		if _, err := l.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	// Corrupt the first (non-final) line in place, preserving line count.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ledger file: %v", err)
	}
	lines := splitLines(t, raw)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	lines[0] = []byte(`{"not":"a valid setup ledger event"}`)
	if err := os.WriteFile(path, joinLines(lines), 0600); err != nil {
		t.Fatalf("rewrite corrupted ledger: %v", err)
	}

	_, err = OpenLedger(path)
	if err == nil {
		t.Fatal("OpenLedger accepted a ledger with a corrupt non-final line; expected an error")
	}
	var appErr *errs.Error
	if !errors.As(err, &appErr) || appErr.Category != errs.CategoryIntegrity {
		t.Fatalf("OpenLedger error = %v, want a CategoryIntegrity error", err)
	}
}

func TestLedgerRecoversTornFinalWrite(t *testing.T) {
	cases := []struct {
		name    string
		corrupt func(lastLine []byte) []byte
	}{
		{
			name: "truncated mid-write",
			corrupt: func(lastLine []byte) []byte {
				return lastLine[:len(lastLine)/2]
			},
		},
		{
			name: "complete but invalid JSON",
			corrupt: func(lastLine []byte) []byte {
				return []byte(`{"this_is": "not a setup ledger event at all"}`)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "setup-ledger.jsonl")

			l, err := OpenLedger(path)
			if err != nil {
				t.Fatalf("OpenLedger: %v", err)
			}
			good := []*protocol.SetupLedgerEvent{
				executionCreatedEvent("evt-001", "exec-001", "plan-001"),
				planApprovedEvent("evt-002", "exec-001", "plan-001"),
			}
			for _, e := range good {
				if _, err := l.Append(e); err != nil {
					t.Fatalf("Append: %v", err)
				}
			}

			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read ledger file: %v", err)
			}
			lines := splitLines(t, raw)
			if len(lines) != 2 {
				t.Fatalf("expected 2 lines, got %d", len(lines))
			}

			// Simulate a crash mid-write of a third event: append a torn
			// final line directly to the file, bypassing Append.
			third := actionStartingEvent("evt-003", "exec-001", "plan-001", "act-001")
			third.Sequence = 3
			third.PreviousEventDigest = mustDigest(t, lines[1])
			digest, err := protocol.ComputeLedgerEventDigest(third)
			if err != nil {
				t.Fatalf("ComputeLedgerEventDigest: %v", err)
			}
			third.EventDigest = digest
			thirdBytes, err := protocol.Marshal(third)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			torn := tc.corrupt(append(thirdBytes, '\n'))

			f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
			if err != nil {
				t.Fatalf("open for append: %v", err)
			}
			if _, err := f.Write(torn); err != nil {
				t.Fatalf("write torn line: %v", err)
			}
			f.Close()

			reopened, err := OpenLedger(path)
			if err != nil {
				t.Fatalf("OpenLedger recovering torn write: %v", err)
			}
			recovered := reopened.Events()
			if len(recovered) != 2 {
				t.Fatalf("recovered %d events, want 2 (torn write dropped)", len(recovered))
			}

			// The next Append must continue the chain at sequence 3, proving
			// the file was actually truncated, not just skipped in memory.
			next, err := reopened.Append(actionStartingEvent("evt-003b", "exec-001", "plan-001", "act-001"))
			if err != nil {
				t.Fatalf("Append after recovery: %v", err)
			}
			if next.Sequence != 3 {
				t.Fatalf("Append after recovery: sequence = %d, want 3", next.Sequence)
			}

			rereopened, err := OpenLedger(path)
			if err != nil {
				t.Fatalf("OpenLedger after recovery append: %v", err)
			}
			if got := len(rereopened.Events()); got != 3 {
				t.Fatalf("final ledger has %d events, want 3", got)
			}
		})
	}
}

// stubPostconditionChecker returns a fixed result for CheckPostconditions.
type stubPostconditionChecker struct {
	passed bool
	detail string
	err    error
}

func (s stubPostconditionChecker) CheckPostconditions(ctx context.Context, conditions []protocol.Condition) (bool, string, error) {
	return s.passed, s.detail, s.err
}

func TestLedgerRecoversInterruptedAction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup-ledger.jsonl")

	l, err := OpenLedger(path)
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	// Simulate a crash: ActionStarting was durably written, but the process
	// died before any terminal event for it.
	for _, e := range []*protocol.SetupLedgerEvent{
		executionCreatedEvent("evt-001", "exec-001", "plan-001"),
		planApprovedEvent("evt-002", "exec-001", "plan-001"),
		actionStartingEvent("evt-003", "exec-001", "plan-001", "act-001"),
	} {
		if _, err := l.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	reopened, err := OpenLedger(path)
	if err != nil {
		t.Fatalf("OpenLedger (restart): %v", err)
	}

	interrupted := FindInterrupted(reopened.Events())
	if len(interrupted) != 1 {
		t.Fatalf("FindInterrupted returned %d actions, want 1", len(interrupted))
	}
	if interrupted[0].ActionID != "act-001" || interrupted[0].ExecutionID != "exec-001" {
		t.Fatalf("FindInterrupted returned %+v, want exec-001/act-001", interrupted[0])
	}

	ctx := context.Background()
	postconditions := []protocol.Condition{{
		Kind:             protocol.CondKindCommandAvailable,
		CommandAvailable: &protocol.CommandAvailableOperand{CommandName: "git"},
	}}

	status, _, err := ResolveInterrupted(ctx, stubPostconditionChecker{passed: true, detail: "postcondition holds"}, postconditions)
	if err != nil {
		t.Fatalf("ResolveInterrupted (passed): %v", err)
	}
	if status != protocol.ActionStatusSucceeded {
		t.Fatalf("ResolveInterrupted (passed) = %q, want succeeded", status)
	}

	status, _, err = ResolveInterrupted(ctx, stubPostconditionChecker{passed: false, detail: "postcondition still fails"}, postconditions)
	if err != nil {
		t.Fatalf("ResolveInterrupted (failed): %v", err)
	}
	if status != protocol.ActionStatusBlocked {
		t.Fatalf("ResolveInterrupted (failed) = %q, want blocked", status)
	}

	// Once resolved, appending the terminal event must remove it from the
	// interrupted set — reconciliation never leaves a resolved action
	// looking interrupted forever.
	if _, err := reopened.Append(actionTerminatedEvent("evt-004", "exec-001", "plan-001", "act-001", protocol.ActionStatusBlocked)); err != nil {
		t.Fatalf("Append terminal event: %v", err)
	}
	if got := FindInterrupted(reopened.Events()); len(got) != 0 {
		t.Fatalf("FindInterrupted after resolution returned %d actions, want 0", len(got))
	}
}

func TestFindInterruptedIgnoresFinishedExecutions(t *testing.T) {
	events := []*protocol.SetupLedgerEvent{
		executionCreatedEvent("evt-001", "exec-001", "plan-001"),
		actionStartingEvent("evt-002", "exec-001", "plan-001", "act-001"),
		executionFinishedEvent("evt-003", "exec-001", "plan-001", protocol.ExecutionStatusFailed),
	}
	if got := FindInterrupted(events); len(got) != 0 {
		t.Fatalf("FindInterrupted returned %d actions for a finished execution, want 0", len(got))
	}
}

func TestProjectExecutionReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup-ledger.jsonl")
	l, err := OpenLedger(path)
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	for _, e := range []*protocol.SetupLedgerEvent{
		executionCreatedEvent("evt-001", "exec-001", "plan-001"),
		planApprovedEvent("evt-002", "exec-001", "plan-001"),
		actionStartingEvent("evt-003", "exec-001", "plan-001", "act-001"),
		actionTerminatedEvent("evt-004", "exec-001", "plan-001", "act-001", protocol.ActionStatusSucceeded),
		executionFinishedEvent("evt-005", "exec-001", "plan-001", protocol.ExecutionStatusSucceeded),
	} {
		if _, err := l.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	report, err := ProjectExecutionReport(l.Events(), "exec-001", testPlanDigest)
	if err != nil {
		t.Fatalf("ProjectExecutionReport: %v", err)
	}
	if report.PlanID != "plan-001" {
		t.Errorf("PlanID = %q, want plan-001", report.PlanID)
	}
	if report.MachineFingerprint != testPlanDigest {
		t.Errorf("MachineFingerprint = %q, want %q", report.MachineFingerprint, testPlanDigest)
	}
	if report.Status != protocol.ExecutionStatusSucceeded {
		t.Errorf("Status = %q, want succeeded", report.Status)
	}
	if len(report.Results) != 1 || report.Results[0].Status != protocol.ActionStatusSucceeded {
		t.Fatalf("Results = %+v, want one succeeded act-001 result", report.Results)
	}
	if report.CompletedAt == nil {
		t.Error("CompletedAt is nil, want set from execution_finished event")
	}

	if _, err := ProjectExecutionReport(l.Events(), "exec-nonexistent", testPlanDigest); err == nil {
		t.Fatal("ProjectExecutionReport for an unknown execution_id succeeded; expected an error")
	}
}

// splitLines splits raw ledger file bytes into individual lines (each still
// terminated conceptually, but returned without the trailing newline).
func splitLines(t *testing.T, raw []byte) [][]byte {
	t.Helper()
	var lines [][]byte
	for _, line := range bytesSplit(raw, '\n') {
		if len(line) > 0 {
			lines = append(lines, line)
		}
	}
	return lines
}

func joinLines(lines [][]byte) []byte {
	var out []byte
	for _, line := range lines {
		out = append(out, line...)
		out = append(out, '\n')
	}
	return out
}

func bytesSplit(raw []byte, sep byte) [][]byte {
	var out [][]byte
	start := 0
	for i, b := range raw {
		if b == sep {
			out = append(out, raw[start:i])
			start = i + 1
		}
	}
	if start < len(raw) {
		out = append(out, raw[start:])
	}
	return out
}

func mustDigest(t *testing.T, line []byte) string {
	t.Helper()
	event := &protocol.SetupLedgerEvent{}
	if err := protocol.Unmarshal(line, event); err != nil {
		t.Fatalf("decode line to compute digest: %v", err)
	}
	return event.EventDigest
}
