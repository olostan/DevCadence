package setup

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// Ledger is a crash-safe, hash-chained, append-only JSONL event log at a
// single file path ($DEVCADENCE_HOME/state/setup-ledger.jsonl). It is not
// itself lock-holding: callers serialize concurrent setup runs with
// AcquireExecutionLock before constructing or using a Ledger for a given
// $DEVCADENCE_HOME (ADR-0014 §3-4).
type Ledger struct {
	mu   sync.Mutex
	path string
	tip  *protocol.SetupLedgerEvent // last valid event, or nil if the ledger is empty
}

// OpenLedger loads and validates path, recovering a torn final write and
// failing closed on any earlier corruption, and returns a Ledger ready to
// Append further events. A nonexistent file is treated as an empty ledger.
func OpenLedger(path string) (*Ledger, error) {
	events, err := loadLedgerFile(path)
	if err != nil {
		return nil, err
	}
	l := &Ledger{path: path}
	if len(events) > 0 {
		l.tip = events[len(events)-1]
	}
	return l, nil
}

// ledgerLine is one newline-delimited chunk read from a ledger file, with
// enough position/termination information to tell a genuinely torn final
// write apart from a complete-but-corrupt record.
type ledgerLine struct {
	offsetBefore int64
	content      []byte
	// terminated is true only when this chunk was followed by its own '\n'
	// in the file — i.e. the write that produced it is known to have
	// completed. An untermined chunk can only be the last chunk in the
	// file (whatever a reader reaches EOF without a trailing newline).
	terminated bool
}

// readLedgerLines splits path's contents into newline-delimited chunks,
// tracking each chunk's start offset and whether it was terminated by its
// own '\n'. It distinguishes a genuine I/O error from ordinary EOF.
func readLedgerLines(path string) ([]ledgerLine, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errs.Wrap(errs.CategoryInternal, err, "open setup ledger %s", path)
	}
	defer f.Close()

	var lines []ledgerLine
	reader := bufio.NewReader(f)
	var offset int64
	for {
		start := offset
		raw, readErr := reader.ReadBytes('\n')
		offset += int64(len(raw))

		if readErr != nil && readErr != io.EOF {
			return nil, errs.Wrap(errs.CategoryInternal, readErr, "read setup ledger %s", path)
		}

		terminated := len(raw) > 0 && raw[len(raw)-1] == '\n'
		content := raw
		if terminated {
			content = raw[:len(raw)-1]
		}
		if len(content) > 0 || terminated {
			lines = append(lines, ledgerLine{offsetBefore: start, content: content, terminated: terminated})
		}
		if readErr == io.EOF {
			break
		}
	}
	return lines, nil
}

// loadLedgerFile reads and validates every event in path.
//
// Only an unterminated final chunk (the file does not end in '\n' — the
// unambiguous signature of a write that was cut off before completing) is
// ever recovered as a torn write: it is dropped and the file truncated
// back to its start, regardless of whether its bytes happen to parse as a
// complete, otherwise-valid event. Every newline-terminated chunk — final
// or not — that fails to parse, fails self-validation, or breaks the hash
// chain fails the whole load closed (ADR-0014 §3: "only an incomplete
// final append may be recovered as a torn write").
func loadLedgerFile(path string) ([]*protocol.SetupLedgerEvent, error) {
	lines, err := readLedgerLines(path)
	if err != nil {
		return nil, err
	}

	var events []*protocol.SetupLedgerEvent
	prevDigest := ""
	for i, line := range lines {
		if !line.terminated {
			// Only possible for the last chunk (readLedgerLines only emits
			// an unterminated chunk at EOF). A crash/short write that left
			// a complete-looking but unflushed record here is exactly the
			// case this drops, per ADR-0014: it was never durably
			// committed, so it is never accepted, complete-JSON or not.
			if truncErr := os.Truncate(path, line.offsetBefore); truncErr != nil {
				return nil, errs.Wrap(errs.CategoryInternal, truncErr, "truncate torn write in %s", path)
			}
			break
		}

		event := &protocol.SetupLedgerEvent{}
		validErr := protocol.Unmarshal(line.content, event)
		chainOK := validErr == nil &&
			event.Sequence == uint64(i+1) &&
			event.PreviousEventDigest == prevDigest

		if !chainOK {
			return nil, errs.New(errs.CategoryIntegrity,
				"setup ledger %s: corrupt or chain-broken event at sequence %d (line %d): %v",
				path, i+1, i+1, validErr)
		}

		events = append(events, event)
		prevDigest = event.EventDigest
	}

	return events, nil
}

// Events returns a defensive copy of the events currently recorded in this
// Ledger's file, re-read from disk so Events() and the on-disk file never
// disagree. It returns an error rather than an empty slice on a read or
// integrity failure — a corrupt ledger must never be indistinguishable
// from an empty one to a caller doing crash recovery.
func (l *Ledger) Events() ([]*protocol.SetupLedgerEvent, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	events, err := loadLedgerFile(l.path)
	if err != nil {
		return nil, err
	}
	out := make([]*protocol.SetupLedgerEvent, len(events))
	copy(out, events)
	return out, nil
}

// Append assigns Sequence, PreviousEventDigest and EventDigest from the
// current chain tip (overwriting whatever the caller set), appends one
// canonical-JSON line, fsyncs, and returns the finalized event. The
// caller populates EventID, ExecutionID, PlanID, PlanDigest, ActionID,
// Timestamp, Type and Payload before calling Append.
func (l *Ledger) Append(event *protocol.SetupLedgerEvent) (*protocol.SetupLedgerEvent, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	seq := uint64(1)
	prevDigest := ""
	if l.tip != nil {
		seq = l.tip.Sequence + 1
		prevDigest = l.tip.EventDigest
	}
	event.Sequence = seq
	event.PreviousEventDigest = prevDigest

	digest, err := protocol.ComputeLedgerEventDigest(event)
	if err != nil {
		return nil, err
	}
	event.EventDigest = digest

	data, err := protocol.Marshal(event)
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(l.path)
	if err := ensureDirMode(dir, 0700); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "open setup ledger %s", l.path)
	}
	defer f.Close()
	if err := ensureFileMode(l.path, 0600); err != nil {
		return nil, err
	}

	if _, err := f.Write(append(data, '\n')); err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "write setup ledger %s", l.path)
	}
	if err := f.Sync(); err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "fsync setup ledger %s", l.path)
	}

	l.tip = event
	return event, nil
}

// PostconditionChecker evaluates whether a set of conditions currently
// hold against the live system. WP-M3B-3's executor supplies the real
// implementation; this package depends only on the interface, keeping
// ledger recovery independent of process execution (ADR-0014 §3).
type PostconditionChecker interface {
	CheckPostconditions(ctx context.Context, conditions []protocol.Condition) (passed bool, detail string, err error)
}

// InterruptedAction describes one action a prior process left mid-flight:
// an ActionStarting event with no subsequent terminal event for it.
type InterruptedAction struct {
	ExecutionID string
	ActionID    string
	RecipeID    string
	PlanID      string
	PlanDigest  string
}

// FindInterrupted scans loaded ledger events and returns every action that
// reached ActionStarting with no later ActionTerminated for its action_id.
//
// This does not stop looking once an ExecutionFinished event is seen for
// that action's execution: an execution that finished while still leaving
// one of its own actions non-terminal is itself an inconsistency to
// surface, not a reason to hide the interrupted action.
func FindInterrupted(events []*protocol.SetupLedgerEvent) []InterruptedAction {
	type key struct{ executionID, actionID string }

	starting := make(map[key]InterruptedAction)
	terminated := make(map[key]bool)

	for _, e := range events {
		switch e.Type {
		case protocol.EventActionStarting:
			k := key{e.ExecutionID, e.ActionID}
			starting[k] = InterruptedAction{
				ExecutionID: e.ExecutionID,
				ActionID:    e.ActionID,
				RecipeID:    e.Payload.ActionStarting.RecipeID,
				PlanID:      e.PlanID,
				PlanDigest:  e.PlanDigest,
			}
		case protocol.EventActionTerminated:
			terminated[key{e.ExecutionID, e.ActionID}] = true
		}
	}

	var out []InterruptedAction
	for k, action := range starting {
		if terminated[k] {
			continue
		}
		out = append(out, action)
	}
	return out
}

// ResolveInterrupted checks postconditions once via checker and reports the
// resolved terminal status for an interrupted action. It never re-executes
// the action (ADR-0014 §3: "never blindly rerun").
func ResolveInterrupted(ctx context.Context, checker PostconditionChecker, postconditions []protocol.Condition) (protocol.ActionStatus, string, error) {
	passed, detail, err := checker.CheckPostconditions(ctx, postconditions)
	if err != nil {
		return "", "", err
	}
	if passed {
		return protocol.ActionStatusSucceeded, detail, nil
	}
	return protocol.ActionStatusBlocked, detail, nil
}

// ReconcileInterrupted durably resolves one interrupted action: it checks
// postconditions via checker (never re-executing the action) and appends
// the resulting PostconditionVerified and ActionTerminated events to the
// ledger itself, so a crash immediately after this call still leaves the
// action resolved on the next restart — resolution recorded only in an
// in-memory return value, with no corresponding ledger write, is not a
// durable recovery. postconditionEventID and terminatedEventID are the
// caller-generated IDs for the two appended events; now is their shared
// timestamp.
func (l *Ledger) ReconcileInterrupted(
	ctx context.Context,
	checker PostconditionChecker,
	action InterruptedAction,
	postconditions []protocol.Condition,
	postconditionEventID, terminatedEventID string,
	now protocol.Timestamp,
) (protocol.ActionStatus, error) {
	status, detail, err := ResolveInterrupted(ctx, checker, postconditions)
	if err != nil {
		return "", err
	}

	pcEvent := &protocol.SetupLedgerEvent{
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       postconditionEventID,
		ExecutionID:   action.ExecutionID,
		PlanID:        action.PlanID,
		PlanDigest:    action.PlanDigest,
		ActionID:      action.ActionID,
		Timestamp:     now,
		Type:          protocol.EventPostconditionVerified,
		Payload: protocol.EventPayload{
			PostconditionVerified: &protocol.PostconditionVerifiedPayload{
				ActionID: action.ActionID,
				Passed:   status == protocol.ActionStatusSucceeded,
				Detail:   detail,
			},
		},
	}
	if _, err := l.Append(pcEvent); err != nil {
		return "", err
	}

	termEvent := &protocol.SetupLedgerEvent{
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       terminatedEventID,
		ExecutionID:   action.ExecutionID,
		PlanID:        action.PlanID,
		PlanDigest:    action.PlanDigest,
		ActionID:      action.ActionID,
		Timestamp:     now,
		Type:          protocol.EventActionTerminated,
		Payload: protocol.EventPayload{
			ActionTerminated: &protocol.ActionTerminatedPayload{
				ActionID: action.ActionID,
				Status:   status,
			},
		},
	}
	if _, err := l.Append(termEvent); err != nil {
		return "", err
	}

	return status, nil
}

// ProjectExecutionReport derives a SetupExecutionReport for one execution
// from ledger events and the immutable SetupPlan that execution is
// running. SetupExecutionReport is never a second source of truth for
// anything the ledger records (ADR-0014 §3) — this is the only way one is
// produced — but no ledger event payload carries machine_fingerprint (only
// SetupPlan does, from WP-M3B-1). Rather than accept a bare string the
// caller could set to anything, plan is verified against what the ledger
// actually recorded (its plan_id and plan_digest) before MachineFingerprint
// is taken from it, so two callers can never project the same immutable
// ledger into two different valid reports by passing different plans.
func ProjectExecutionReport(events []*protocol.SetupLedgerEvent, executionID string, plan *protocol.SetupPlan) (*protocol.SetupExecutionReport, error) {
	if plan == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "setup execution report: plan is required")
	}

	report := &protocol.SetupExecutionReport{
		SchemaVersion: protocol.SchemaVersion1,
		ExecutionID:   executionID,
		Status:        protocol.ExecutionStatusRunning,
	}
	resultIndex := make(map[string]int)
	resultStarted := make(map[string]bool)
	resultTerminated := make(map[string]bool)
	executionFinished := false

	resultFor := func(actionID string) int {
		idx, ok := resultIndex[actionID]
		if !ok {
			idx = len(report.Results)
			resultIndex[actionID] = idx
			report.Results = append(report.Results, protocol.ActionResult{ActionID: actionID})
		}
		return idx
	}

	for _, e := range events {
		if e.ExecutionID != executionID {
			continue
		}
		switch e.Type {
		case protocol.EventExecutionCreated:
			report.PlanID = e.PlanID
			report.PlanDigest = e.PlanDigest
			report.StartedAt = e.Timestamp
		case protocol.EventActionStarting:
			idx := resultFor(e.ActionID)
			started := e.Timestamp
			report.Results[idx].Status = protocol.ActionStatusRunning
			report.Results[idx].StartedAt = &started
			resultStarted[e.ActionID] = true
		case protocol.EventActionProcessCompleted:
			idx := resultFor(e.ActionID)
			if p := e.Payload.ActionProcessCompleted; p != nil {
				exitCode := p.ExitCode
				report.Results[idx].ExitCode = &exitCode
				report.Results[idx].ArtifactRef = p.ArtifactRef
			}
		case protocol.EventActionTerminated:
			idx := resultFor(e.ActionID)
			finished := e.Timestamp
			report.Results[idx].Status = e.Payload.ActionTerminated.Status
			report.Results[idx].FinishedAt = &finished
			report.Results[idx].FailureDetail = e.Payload.ActionTerminated.FailureReason
			resultTerminated[e.ActionID] = true
		case protocol.EventExecutionFinished:
			finished := e.Timestamp
			report.CompletedAt = &finished
			report.Status = e.Payload.ExecutionFinished.Status
			executionFinished = true
		}
	}

	if report.PlanID == "" {
		return nil, errs.New(errs.CategoryNotFound, "setup execution report: no execution_created event found for execution_id %q", executionID)
	}
	if plan.PlanID != report.PlanID {
		return nil, errs.New(errs.CategoryInvalidArgument,
			"setup execution report: plan %q does not match the plan_id %q recorded for execution %q",
			plan.PlanID, report.PlanID, executionID)
	}
	planDigest, err := protocol.ComputePlanDigest(plan)
	if err != nil {
		return nil, err
	}
	if planDigest != report.PlanDigest {
		return nil, errs.New(errs.CategoryInvalidArgument,
			"setup execution report: plan digest %q does not match the plan_digest %q recorded for execution %q",
			planDigest, report.PlanDigest, executionID)
	}
	report.MachineFingerprint = plan.MachineFingerprint

	// An action that started but never reached a terminal event is
	// interrupted, not still running — a crash is not "in progress." An
	// execution left with any interrupted action is itself interrupted,
	// unless a later ExecutionFinished event overrides that explicitly.
	anyInterrupted := false
	for actionID, idx := range resultIndex {
		if resultStarted[actionID] && !resultTerminated[actionID] {
			report.Results[idx].Status = protocol.ActionStatusInterrupted
			anyInterrupted = true
		}
	}
	if anyInterrupted && !executionFinished {
		report.Status = protocol.ExecutionStatusInterrupted
	}

	if err := report.Validate(); err != nil {
		return nil, err
	}
	return report, nil
}
