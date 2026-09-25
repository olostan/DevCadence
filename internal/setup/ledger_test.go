package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

func ledgerTestTime(offsetSeconds int) protocol.Timestamp {
	return protocol.NewTimestamp(time.Date(2026, 9, 24, 12, 0, offsetSeconds, 0, time.UTC))
}

const testMachineFingerprint = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// testPlan returns a minimal SetupPlan whose digest can be computed
// (ComputePlanDigest only needs the fields it hashes, not a fully
// Validate()-passing plan) for use as the "approved plan" ledger events
// and ProjectExecutionReport calls are anchored to.
func testPlan(planID string) *protocol.SetupPlan {
	return &protocol.SetupPlan{
		SchemaVersion:      protocol.SchemaVersion1,
		PlanID:             planID,
		RecipeSetVersion:   "1.0",
		MachineFingerprint: testMachineFingerprint,
		CreatedAt:          ledgerTestTime(0),
		Target:             protocol.TargetAll,
	}
}

func testPlanDigest(t *testing.T, plan *protocol.SetupPlan) string {
	t.Helper()
	digest, err := protocol.ComputePlanDigest(plan)
	if err != nil {
		t.Fatalf("ComputePlanDigest: %v", err)
	}
	return digest
}

func executionCreatedEvent(eventID, executionID, planID, planDigest string) *protocol.SetupLedgerEvent {
	return &protocol.SetupLedgerEvent{
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       eventID,
		ExecutionID:   executionID,
		PlanID:        planID,
		PlanDigest:    planDigest,
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

func planApprovedEvent(eventID, executionID, planID, planDigest string) *protocol.SetupLedgerEvent {
	return &protocol.SetupLedgerEvent{
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       eventID,
		ExecutionID:   executionID,
		PlanID:        planID,
		PlanDigest:    planDigest,
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

func actionStartingEvent(eventID, executionID, planID, planDigest, actionID string) *protocol.SetupLedgerEvent {
	opKind := protocol.OpKindCreateDirectory
	return &protocol.SetupLedgerEvent{
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       eventID,
		ExecutionID:   executionID,
		PlanID:        planID,
		PlanDigest:    planDigest,
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

func actionProcessCompletedEvent(eventID, executionID, planID, planDigest, actionID string, exitCode int) *protocol.SetupLedgerEvent {
	return &protocol.SetupLedgerEvent{
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       eventID,
		ExecutionID:   executionID,
		PlanID:        planID,
		PlanDigest:    planDigest,
		ActionID:      actionID,
		Timestamp:     ledgerTestTime(3),
		Type:          protocol.EventActionProcessCompleted,
		Payload: protocol.EventPayload{
			ActionProcessCompleted: &protocol.ActionProcessCompletedPayload{
				ActionID: actionID,
				ExitCode: exitCode,
			},
		},
	}
}

func actionTerminatedEvent(eventID, executionID, planID, planDigest, actionID string, status protocol.ActionStatus) *protocol.SetupLedgerEvent {
	return &protocol.SetupLedgerEvent{
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       eventID,
		ExecutionID:   executionID,
		PlanID:        planID,
		PlanDigest:    planDigest,
		ActionID:      actionID,
		Timestamp:     ledgerTestTime(4),
		Type:          protocol.EventActionTerminated,
		Payload: protocol.EventPayload{
			ActionTerminated: &protocol.ActionTerminatedPayload{
				ActionID: actionID,
				Status:   status,
			},
		},
	}
}

func executionFinishedEvent(eventID, executionID, planID, planDigest string, status protocol.ExecutionStatus) *protocol.SetupLedgerEvent {
	return &protocol.SetupLedgerEvent{
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       eventID,
		ExecutionID:   executionID,
		PlanID:        planID,
		PlanDigest:    planDigest,
		Timestamp:     ledgerTestTime(5),
		Type:          protocol.EventExecutionFinished,
		Payload: protocol.EventPayload{
			ExecutionFinished: &protocol.ExecutionFinishedPayload{
				Status: status,
			},
		},
	}
}

func mustEvents(t *testing.T, l *Ledger) []*protocol.SetupLedgerEvent {
	t.Helper()
	events, err := l.Events()
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	return events
}

func TestLedgerAppendAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup-ledger.jsonl")
	plan := testPlan("plan-001")
	digest := testPlanDigest(t, plan)

	l, err := OpenLedger(path)
	if err != nil {
		t.Fatalf("OpenLedger (new): %v", err)
	}
	if got := mustEvents(t, l); len(got) != 0 {
		t.Fatalf("new ledger has %d events, want 0", len(got))
	}

	events := []*protocol.SetupLedgerEvent{
		executionCreatedEvent("evt-001", "exec-001", "plan-001", digest),
		planApprovedEvent("evt-002", "exec-001", "plan-001", digest),
		actionStartingEvent("evt-003", "exec-001", "plan-001", digest, "act-001"),
		actionTerminatedEvent("evt-004", "exec-001", "plan-001", digest, "act-001", protocol.ActionStatusSucceeded),
		executionFinishedEvent("evt-005", "exec-001", "plan-001", digest, protocol.ExecutionStatusSucceeded),
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
	got := mustEvents(t, reopened)
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
	next, err := reopened.Append(executionCreatedEvent("evt-006", "exec-002", "plan-002", digest))
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
	plan := testPlan("plan-001")
	digest := testPlanDigest(t, plan)

	l, err := OpenLedger(path)
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	for _, e := range []*protocol.SetupLedgerEvent{
		executionCreatedEvent("evt-001", "exec-001", "plan-001", digest),
		planApprovedEvent("evt-002", "exec-001", "plan-001", digest),
		actionStartingEvent("evt-003", "exec-001", "plan-001", digest, "act-001"),
	} {
		if _, err := l.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	// Corrupt the first (non-final) line in place, preserving line count
	// and keeping every line newline-terminated.
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
	requireIntegrityError(t, err, "OpenLedger accepted a ledger with a corrupt non-final line")
}

func TestLedgerFailsClosedOnCorruptNewlineTerminatedFinalLine(t *testing.T) {
	// A complete, newline-terminated final record that is invalid must
	// fail closed, not be silently truncated away — only an unterminated
	// (genuinely torn) final write is ever recoverable.
	path := filepath.Join(t.TempDir(), "setup-ledger.jsonl")
	plan := testPlan("plan-001")
	digest := testPlanDigest(t, plan)

	l, err := OpenLedger(path)
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	if _, err := l.Append(executionCreatedEvent("evt-001", "exec-001", "plan-001", digest)); err != nil {
		t.Fatalf("Append: %v", err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	if _, err := f.Write([]byte(`{"this_is": "a complete, newline-terminated, but invalid record"}` + "\n")); err != nil {
		t.Fatalf("write corrupt terminated line: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	_, err = OpenLedger(path)
	requireIntegrityError(t, err, "OpenLedger accepted a ledger whose newline-terminated final line is corrupt")
}

func TestLedgerRecoversTornFinalWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup-ledger.jsonl")
	plan := testPlan("plan-001")
	digest := testPlanDigest(t, plan)

	l, err := OpenLedger(path)
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	good := []*protocol.SetupLedgerEvent{
		executionCreatedEvent("evt-001", "exec-001", "plan-001", digest),
		planApprovedEvent("evt-002", "exec-001", "plan-001", digest),
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

	// Build what a complete third event's line would have been, then write
	// only a truncated, NOT newline-terminated prefix of it — the
	// unambiguous signature of a write cut off mid-append.
	third := actionStartingEvent("evt-003", "exec-001", "plan-001", digest, "act-001")
	third.Sequence = 3
	third.PreviousEventDigest = mustDigest(t, lines[1])
	digest3, err := protocol.ComputeLedgerEventDigest(third)
	if err != nil {
		t.Fatalf("ComputeLedgerEventDigest: %v", err)
	}
	third.EventDigest = digest3
	thirdBytes, err := protocol.Marshal(third)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	torn := thirdBytes[:len(thirdBytes)/2] // no trailing '\n': unterminated

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
	recovered := mustEvents(t, reopened)
	if len(recovered) != 2 {
		t.Fatalf("recovered %d events, want 2 (torn write dropped)", len(recovered))
	}

	// The next Append must continue the chain at sequence 3, proving the
	// file was actually truncated, not just skipped in memory.
	next, err := reopened.Append(actionStartingEvent("evt-003b", "exec-001", "plan-001", digest, "act-001"))
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
	if got := len(mustEvents(t, rereopened)); got != 3 {
		t.Fatalf("final ledger has %d events, want 3", got)
	}
}

func TestLedgerAcceptsCompleteUnterminatedFinalRecordAsTorn(t *testing.T) {
	// A complete, valid-looking JSON record with NO trailing newline is
	// still treated as torn (never accepted as-is), because there is no
	// way to distinguish "the write finished but the newline is still
	// pending" from "this happens to be valid JSON but the write was cut
	// off before whatever was meant to follow." Termination, not
	// parseability, is what proves a write committed.
	path := filepath.Join(t.TempDir(), "setup-ledger.jsonl")
	plan := testPlan("plan-001")
	digest := testPlanDigest(t, plan)

	l, err := OpenLedger(path)
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	if _, err := l.Append(executionCreatedEvent("evt-001", "exec-001", "plan-001", digest)); err != nil {
		t.Fatalf("Append: %v", err)
	}

	second := planApprovedEvent("evt-002", "exec-001", "plan-001", digest)
	second.Sequence = 2
	firstEvents := mustEvents(t, l)
	second.PreviousEventDigest = firstEvents[0].EventDigest
	digest2, err := protocol.ComputeLedgerEventDigest(second)
	if err != nil {
		t.Fatalf("ComputeLedgerEventDigest: %v", err)
	}
	second.EventDigest = digest2
	secondBytes, err := protocol.Marshal(second)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	if _, err := f.Write(secondBytes); err != nil { // deliberately no trailing '\n'
		t.Fatalf("write unterminated complete record: %v", err)
	}
	f.Close()

	reopened, err := OpenLedger(path)
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	recovered := mustEvents(t, reopened)
	if len(recovered) != 1 {
		t.Fatalf("recovered %d events, want 1 (unterminated complete record dropped)", len(recovered))
	}

	next, err := reopened.Append(planApprovedEvent("evt-002b", "exec-001", "plan-001", digest))
	if err != nil {
		t.Fatalf("Append after recovery: %v", err)
	}
	if next.Sequence != 2 {
		t.Fatalf("Append after recovery: sequence = %d, want 2", next.Sequence)
	}
}

func requireIntegrityError(t *testing.T, err error, msgIfNil string) {
	t.Helper()
	if err == nil {
		t.Fatal(msgIfNil + "; expected an error")
	}
	var appErr *errs.Error
	if !errors.As(err, &appErr) || appErr.Category != errs.CategoryIntegrity {
		t.Fatalf("error = %v, want a CategoryIntegrity error", err)
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

func TestLedgerReconcilesInterruptedActionDurably(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup-ledger.jsonl")
	plan := testPlan("plan-001")
	digest := testPlanDigest(t, plan)

	l, err := OpenLedger(path)
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	// Simulate a crash: ActionStarting was durably written, but the process
	// died before any terminal event for it.
	for _, e := range []*protocol.SetupLedgerEvent{
		executionCreatedEvent("evt-001", "exec-001", "plan-001", digest),
		planApprovedEvent("evt-002", "exec-001", "plan-001", digest),
		actionStartingEvent("evt-003", "exec-001", "plan-001", digest, "act-001"),
	} {
		if _, err := l.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	reopened, err := OpenLedger(path)
	if err != nil {
		t.Fatalf("OpenLedger (restart): %v", err)
	}

	events := mustEvents(t, reopened)
	interrupted := FindInterrupted(events)
	if len(interrupted) != 1 {
		t.Fatalf("FindInterrupted returned %d actions, want 1", len(interrupted))
	}
	action := interrupted[0]
	if action.ActionID != "act-001" || action.ExecutionID != "exec-001" {
		t.Fatalf("FindInterrupted returned %+v, want exec-001/act-001", action)
	}
	if action.PlanID != "plan-001" || action.PlanDigest != digest {
		t.Fatalf("FindInterrupted did not carry plan_id/plan_digest through: %+v", action)
	}

	// Before reconciliation, the projected report must show the action (and
	// the execution as a whole) interrupted, not "running" — a crash is not
	// still in progress.
	report, err := ProjectExecutionReport(events, "exec-001", plan)
	if err != nil {
		t.Fatalf("ProjectExecutionReport (pre-reconcile): %v", err)
	}
	if report.Status != protocol.ExecutionStatusInterrupted {
		t.Errorf("pre-reconcile report.Status = %q, want interrupted", report.Status)
	}
	if len(report.Results) != 1 || report.Results[0].Status != protocol.ActionStatusInterrupted {
		t.Fatalf("pre-reconcile report.Results = %+v, want one interrupted act-001", report.Results)
	}

	ctx := context.Background()
	postconditions := []protocol.Condition{{
		Kind:             protocol.CondKindCommandAvailable,
		CommandAvailable: &protocol.CommandAvailableOperand{CommandName: "git"},
	}}

	status, err := reopened.ReconcileInterrupted(ctx, stubPostconditionChecker{passed: true, detail: "postcondition holds"},
		action, postconditions, "evt-004", "evt-005", ledgerTestTime(10))
	if err != nil {
		t.Fatalf("ReconcileInterrupted: %v", err)
	}
	if status != protocol.ActionStatusSucceeded {
		t.Fatalf("ReconcileInterrupted status = %q, want succeeded", status)
	}

	// The reconciliation must be durable: read the ledger back (a fresh
	// Open, not the in-memory Ledger) and confirm the resolution actually
	// landed on disk, with no manual/hand-written terminal-event append.
	rereopened, err := OpenLedger(path)
	if err != nil {
		t.Fatalf("OpenLedger after reconciliation: %v", err)
	}
	finalEvents := mustEvents(t, rereopened)
	if got := FindInterrupted(finalEvents); len(got) != 0 {
		t.Fatalf("FindInterrupted after durable reconciliation returned %d actions, want 0", len(got))
	}

	finalReport, err := ProjectExecutionReport(finalEvents, "exec-001", plan)
	if err != nil {
		t.Fatalf("ProjectExecutionReport (post-reconcile): %v", err)
	}
	if len(finalReport.Results) != 1 || finalReport.Results[0].Status != protocol.ActionStatusSucceeded {
		t.Fatalf("post-reconcile report.Results = %+v, want one succeeded act-001", finalReport.Results)
	}
}

func TestLedgerReconcilesInterruptedActionAsBlockedWhenPostconditionsFail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup-ledger.jsonl")
	plan := testPlan("plan-001")
	digest := testPlanDigest(t, plan)

	l, err := OpenLedger(path)
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	for _, e := range []*protocol.SetupLedgerEvent{
		executionCreatedEvent("evt-001", "exec-001", "plan-001", digest),
		actionStartingEvent("evt-002", "exec-001", "plan-001", digest, "act-001"),
	} {
		if _, err := l.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	action := FindInterrupted(mustEvents(t, l))[0]
	ctx := context.Background()
	postconditions := []protocol.Condition{{
		Kind:             protocol.CondKindCommandAvailable,
		CommandAvailable: &protocol.CommandAvailableOperand{CommandName: "git"},
	}}
	status, err := l.ReconcileInterrupted(ctx, stubPostconditionChecker{passed: false, detail: "still missing"},
		action, postconditions, "evt-003", "evt-004", ledgerTestTime(10))
	if err != nil {
		t.Fatalf("ReconcileInterrupted: %v", err)
	}
	if status != protocol.ActionStatusBlocked {
		t.Fatalf("ReconcileInterrupted status = %q, want blocked", status)
	}
}

func TestFindInterruptedSurfacesInconsistencyEvenAfterExecutionFinished(t *testing.T) {
	// An execution that finished while one of its own actions never
	// reached a terminal event is an inconsistency to surface, not a
	// reason to hide the interrupted action.
	digest := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	events := []*protocol.SetupLedgerEvent{
		executionCreatedEvent("evt-001", "exec-001", "plan-001", digest),
		actionStartingEvent("evt-002", "exec-001", "plan-001", digest, "act-001"),
		executionFinishedEvent("evt-003", "exec-001", "plan-001", digest, protocol.ExecutionStatusFailed),
	}
	got := FindInterrupted(events)
	if len(got) != 1 || got[0].ActionID != "act-001" {
		t.Fatalf("FindInterrupted = %+v, want one interrupted act-001 despite ExecutionFinished", got)
	}
}

func TestProjectExecutionReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup-ledger.jsonl")
	plan := testPlan("plan-001")
	digest := testPlanDigest(t, plan)

	l, err := OpenLedger(path)
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	for _, e := range []*protocol.SetupLedgerEvent{
		executionCreatedEvent("evt-001", "exec-001", "plan-001", digest),
		planApprovedEvent("evt-002", "exec-001", "plan-001", digest),
		actionStartingEvent("evt-003", "exec-001", "plan-001", digest, "act-001"),
		actionProcessCompletedEvent("evt-004", "exec-001", "plan-001", digest, "act-001", 0),
		actionTerminatedEvent("evt-005", "exec-001", "plan-001", digest, "act-001", protocol.ActionStatusSucceeded),
		executionFinishedEvent("evt-006", "exec-001", "plan-001", digest, protocol.ExecutionStatusSucceeded),
	} {
		if _, err := l.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	events := mustEvents(t, l)

	report, err := ProjectExecutionReport(events, "exec-001", plan)
	if err != nil {
		t.Fatalf("ProjectExecutionReport: %v", err)
	}
	if report.PlanID != "plan-001" {
		t.Errorf("PlanID = %q, want plan-001", report.PlanID)
	}
	if report.MachineFingerprint != testMachineFingerprint {
		t.Errorf("MachineFingerprint = %q, want %q", report.MachineFingerprint, testMachineFingerprint)
	}
	if report.Status != protocol.ExecutionStatusSucceeded {
		t.Errorf("Status = %q, want succeeded", report.Status)
	}
	if len(report.Results) != 1 || report.Results[0].Status != protocol.ActionStatusSucceeded {
		t.Fatalf("Results = %+v, want one succeeded act-001 result", report.Results)
	}
	if report.Results[0].ExitCode == nil || *report.Results[0].ExitCode != 0 {
		t.Errorf("Results[0].ExitCode = %v, want a pointer to 0", report.Results[0].ExitCode)
	}
	if report.CompletedAt == nil {
		t.Error("CompletedAt is nil, want set from execution_finished event")
	}

	if _, err := ProjectExecutionReport(events, "exec-nonexistent", plan); err == nil {
		t.Fatal("ProjectExecutionReport for an unknown execution_id succeeded; expected an error")
	}
}

func TestProjectExecutionReportRejectsPlanNotMatchingTheLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup-ledger.jsonl")
	plan := testPlan("plan-001")
	digest := testPlanDigest(t, plan)

	l, err := OpenLedger(path)
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	if _, err := l.Append(executionCreatedEvent("evt-001", "exec-001", "plan-001", digest)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	events := mustEvents(t, l)

	t.Run("wrong plan_id", func(t *testing.T) {
		wrongPlan := testPlan("plan-999")
		if _, err := ProjectExecutionReport(events, "exec-001", wrongPlan); err == nil {
			t.Fatal("ProjectExecutionReport accepted a plan whose plan_id does not match the ledger; expected an error")
		}
	})

	t.Run("wrong content, same plan_id (digest mismatch)", func(t *testing.T) {
		tamperedPlan := testPlan("plan-001")
		tamperedPlan.RecipeSetVersion = "2.0" // changes the computed digest without changing plan_id
		if _, err := ProjectExecutionReport(events, "exec-001", tamperedPlan); err == nil {
			t.Fatal("ProjectExecutionReport accepted a plan whose digest does not match the ledger; expected an error")
		}
	})

	t.Run("nil plan", func(t *testing.T) {
		if _, err := ProjectExecutionReport(events, "exec-001", nil); err == nil {
			t.Fatal("ProjectExecutionReport accepted a nil plan; expected an error")
		}
	})
}

func TestEventsFailsClosedOnCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup-ledger.jsonl")
	plan := testPlan("plan-001")
	digest := testPlanDigest(t, plan)

	l, err := OpenLedger(path)
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	for _, e := range []*protocol.SetupLedgerEvent{
		executionCreatedEvent("evt-001", "exec-001", "plan-001", digest),
		planApprovedEvent("evt-002", "exec-001", "plan-001", digest),
	} {
		if _, err := l.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ledger file: %v", err)
	}
	lines := splitLines(t, raw)
	lines[0] = []byte(`{"not":"a valid setup ledger event"}`)
	if err := os.WriteFile(path, joinLines(lines), 0600); err != nil {
		t.Fatalf("rewrite corrupted ledger: %v", err)
	}

	// Events() must surface the integrity error, not silently return an
	// empty (or otherwise misleadingly small) slice — a corrupt ledger must
	// never look indistinguishable from an empty one to a recovery caller.
	_, err = l.Events()
	requireIntegrityError(t, err, "Events() accepted a corrupted ledger file")
}

func TestLedgerFileModeIsHardenedOnExistingPermissiveFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file mode semantics do not apply on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "setup-ledger.jsonl")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatalf("pre-create permissive ledger file: %v", err)
	}

	l, err := OpenLedger(path)
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	plan := testPlan("plan-001")
	digest := testPlanDigest(t, plan)
	if _, err := l.Append(executionCreatedEvent("evt-001", "exec-001", "plan-001", digest)); err != nil {
		t.Fatalf("Append: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("ledger file mode = %o, want 0600 (pre-existing 0644 must be tightened)", perm)
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
