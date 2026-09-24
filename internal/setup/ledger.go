package setup

import (
	"bufio"
	"bytes"
	"context"
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

// loadLedgerFile reads and validates every event in path, recovering a
// torn or corrupt final line by truncating it away, and failing closed if
// any non-final line is corrupt or breaks the hash chain.
func loadLedgerFile(path string) ([]*protocol.SetupLedgerEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errs.Wrap(errs.CategoryInternal, err, "open setup ledger %s", path)
	}
	defer f.Close()

	type lineRecord struct {
		offsetBefore int64
		offsetAfter  int64
		raw          []byte
	}
	var lines []lineRecord
	reader := bufio.NewReader(f)
	var offset int64
	for {
		start := offset
		raw, readErr := reader.ReadBytes('\n')
		offset += int64(len(raw))
		trimmed := bytes.TrimRight(raw, "\n")
		if len(trimmed) > 0 {
			lines = append(lines, lineRecord{offsetBefore: start, offsetAfter: offset, raw: trimmed})
		}
		if readErr != nil {
			break // EOF or read error: stop, treating whatever was read as the whole file
		}
	}

	var events []*protocol.SetupLedgerEvent
	prevDigest := ""
	for i, line := range lines {
		isLast := i == len(lines)-1

		event := &protocol.SetupLedgerEvent{}
		validErr := protocol.Unmarshal(line.raw, event)
		chainOK := validErr == nil &&
			event.Sequence == uint64(i+1) &&
			event.PreviousEventDigest == prevDigest

		if chainOK {
			events = append(events, event)
			prevDigest = event.EventDigest
			continue
		}

		if isLast {
			// Torn write or a corrupt-but-recoverable final append: drop it
			// and truncate the file back to the end of the last good event,
			// so the next Append continues cleanly.
			if truncErr := os.Truncate(path, line.offsetBefore); truncErr != nil {
				return nil, errs.Wrap(errs.CategoryInternal, truncErr, "truncate torn write in %s", path)
			}
			break
		}

		return nil, errs.New(errs.CategoryIntegrity,
			"setup ledger %s: corrupt or chain-broken event at sequence %d (line %d): %v",
			path, i+1, i+1, validErr)
	}

	return events, nil
}

// Events returns a defensive copy of the events currently known to this
// Ledger (loaded at Open time plus any appended since).
func (l *Ledger) Events() []*protocol.SetupLedgerEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	// The in-memory tip only tracks the last event; re-read from disk for a
	// full list so Events() and the on-disk file never disagree.
	events, err := loadLedgerFile(l.path)
	if err != nil {
		// A Ledger successfully opened and appended to by this process
		// should never fail to re-read its own file; surface nothing rather
		// than panicking, callers that need the error use OpenLedger again.
		return nil
	}
	out := make([]*protocol.SetupLedgerEvent, len(events))
	copy(out, events)
	return out
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

	if err := os.MkdirAll(filepath.Dir(l.path), 0700); err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "create %s", filepath.Dir(l.path))
	}

	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "open setup ledger %s", l.path)
	}
	defer f.Close()

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
}

// FindInterrupted scans loaded ledger events and returns every action that
// reached ActionStarting with no later ActionTerminated for its action_id
// and no ExecutionFinished for its execution_id.
func FindInterrupted(events []*protocol.SetupLedgerEvent) []InterruptedAction {
	type key struct{ executionID, actionID string }

	starting := make(map[key]InterruptedAction)
	terminated := make(map[key]bool)
	finishedExecutions := make(map[string]bool)

	for _, e := range events {
		switch e.Type {
		case protocol.EventActionStarting:
			k := key{e.ExecutionID, e.ActionID}
			starting[k] = InterruptedAction{
				ExecutionID: e.ExecutionID,
				ActionID:    e.ActionID,
				RecipeID:    e.Payload.ActionStarting.RecipeID,
			}
		case protocol.EventActionTerminated:
			terminated[key{e.ExecutionID, e.ActionID}] = true
		case protocol.EventExecutionFinished:
			finishedExecutions[e.ExecutionID] = true
		}
	}

	var out []InterruptedAction
	for k, action := range starting {
		if terminated[k] || finishedExecutions[k.executionID] {
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

// ProjectExecutionReport derives a SetupExecutionReport for one execution
// from ledger events plus the machine fingerprint of the plan being
// executed. SetupExecutionReport is never a second source of truth for
// anything the ledger records (ADR-0014 §3) — this is the only way one is
// produced — but no ledger event payload carries machine_fingerprint (only
// SetupPlan does, from WP-M3B-1), so the caller supplies it from the same
// approved SetupPlan it is executing. That does not make this an
// independent source of truth: the value is not decided here, only passed
// through from the one place it already lives.
func ProjectExecutionReport(events []*protocol.SetupLedgerEvent, executionID, machineFingerprint string) (*protocol.SetupExecutionReport, error) {
	report := &protocol.SetupExecutionReport{
		SchemaVersion:      protocol.SchemaVersion1,
		ExecutionID:        executionID,
		MachineFingerprint: machineFingerprint,
		Status:             protocol.ExecutionStatusRunning,
	}
	resultIndex := make(map[string]int)

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
			idx, ok := resultIndex[e.ActionID]
			if !ok {
				idx = len(report.Results)
				resultIndex[e.ActionID] = idx
				report.Results = append(report.Results, protocol.ActionResult{ActionID: e.ActionID})
			}
			started := e.Timestamp
			report.Results[idx].Status = protocol.ActionStatusRunning
			report.Results[idx].StartedAt = &started
		case protocol.EventActionTerminated:
			idx, ok := resultIndex[e.ActionID]
			if !ok {
				idx = len(report.Results)
				resultIndex[e.ActionID] = idx
				report.Results = append(report.Results, protocol.ActionResult{ActionID: e.ActionID})
			}
			finished := e.Timestamp
			report.Results[idx].Status = e.Payload.ActionTerminated.Status
			report.Results[idx].FinishedAt = &finished
			report.Results[idx].FailureDetail = e.Payload.ActionTerminated.FailureReason
		case protocol.EventExecutionFinished:
			finished := e.Timestamp
			report.CompletedAt = &finished
			report.Status = e.Payload.ExecutionFinished.Status
		}
	}

	if report.PlanID == "" {
		return nil, errs.New(errs.CategoryNotFound, "setup execution report: no execution_created event found for execution_id %q", executionID)
	}
	if err := report.Validate(); err != nil {
		return nil, err
	}
	return report, nil
}
