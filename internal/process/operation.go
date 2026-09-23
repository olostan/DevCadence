package process

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/ids"
)

// DefaultYieldThreshold is the standard latency threshold (10 seconds)
// after which a controlled command yields execution asynchronously (ADR-0016).
const DefaultYieldThreshold = 10 * time.Second

// OperationStatus represents the lifecycle state of an execution Operation.
type OperationStatus string

const (
	OperationStatusRunning   OperationStatus = "running"
	OperationStatusCompleted OperationStatus = "completed"
	OperationStatusTimeout   OperationStatus = "timeout"
	OperationStatusCancelled OperationStatus = "cancelled"
	OperationStatusFailed    OperationStatus = "failed"
)

// OperationSnapshot represents an immutable point-in-time view of an Operation.
type OperationSnapshot struct {
	ID                  string          `json:"id"`
	Status              OperationStatus `json:"status"`
	Command             []string        `json:"command"`
	Dir                 string          `json:"dir"`
	StartedAt           time.Time       `json:"started_at"`
	FinishedAt          time.Time       `json:"finished_at,omitempty"`
	Duration            time.Duration   `json:"duration,omitempty"`
	Result              *Result         `json:"result,omitempty"`
	YieldRecommendation string          `json:"yield_recommendation,omitempty"`
	Error               string          `json:"error,omitempty"`
}

type operationState struct {
	mu         sync.RWMutex
	id         string
	spec       Spec
	status     OperationStatus
	cmd        *exec.Cmd
	cancelFn   context.CancelFunc
	startedAt  time.Time
	finishedAt time.Time
	result     *Result
	err        error
	doneCh     chan struct{}
	closeOnce  sync.Once
}

func (s *operationState) snapshot() OperationSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snap := OperationSnapshot{
		ID:         s.id,
		Status:     s.status,
		Command:    append([]string{s.spec.Executable}, s.spec.Args...),
		Dir:        s.spec.Dir,
		StartedAt:  s.startedAt,
		FinishedAt: s.finishedAt,
		Result:     s.result,
	}
	if s.err != nil {
		snap.Error = s.err.Error()
	}
	if !s.finishedAt.IsZero() {
		snap.Duration = s.finishedAt.Sub(s.startedAt)
	} else {
		snap.Duration = time.Since(s.startedAt)
	}
	if s.status == OperationStatusRunning {
		snap.YieldRecommendation = "Operation is executing in the background. Do not busy-poll; wait for event notification or query status."
	}
	return snap
}

// OperationManager supervises asynchronous controlled operations.
type OperationManager struct {
	mu         sync.RWMutex
	runner     *Runner
	ids        ids.Source
	operations map[string]*operationState
}

// NewOperationManager returns a new OperationManager.
func NewOperationManager(runner *Runner, idSource ids.Source) *OperationManager {
	if runner == nil {
		runner = NewRunner()
	}
	if idSource == nil {
		idSource = ids.NewULIDSource()
	}
	return &OperationManager{
		runner:     runner,
		ids:        idSource,
		operations: make(map[string]*operationState),
	}
}

// StartOperation launches a controlled process invocation. If execution completes
// within yieldThreshold, the terminal snapshot is returned immediately. If it exceeds
// yieldThreshold, it yields with status "running" while continuing in the background under spec.Timeout.
func (m *OperationManager) StartOperation(ctx context.Context, spec Spec, yieldThreshold time.Duration) (OperationSnapshot, error) {
	if yieldThreshold <= 0 {
		yieldThreshold = DefaultYieldThreshold
	}
	if err := validateSpec(spec); err != nil {
		return OperationSnapshot{}, err
	}

	resolved, err := resolveExecutable(spec.Executable, spec.Env)
	if err != nil {
		return OperationSnapshot{}, err
	}

	maxStdout := spec.MaxStdoutBytes
	if maxStdout == 0 {
		maxStdout = DefaultMaxOutputBytes
	}
	maxStderr := spec.MaxStderrBytes
	if maxStderr == 0 {
		maxStderr = DefaultMaxOutputBytes
	}

	runCtx, cancel := context.WithTimeout(context.Background(), spec.Timeout)

	cmd := exec.Cmd{
		Path: resolved,
		Args: append([]string{resolved}, spec.Args...),
		Dir:  spec.Dir,
		Env:  append([]string(nil), spec.Env...),
	}
	if spec.Stdin != nil {
		cmd.Stdin = spec.Stdin
	}

	stdout := newBoundedWriter(maxStdout)
	stderr := newBoundedWriter(maxStderr)
	if spec.StdoutSink != nil {
		cmd.Stdout = io.MultiWriter(stdout, spec.StdoutSink)
	} else {
		cmd.Stdout = stdout
	}
	if spec.StderrSink != nil {
		cmd.Stderr = io.MultiWriter(stderr, spec.StderrSink)
	} else {
		cmd.Stderr = stderr
	}

	setProcAttrs(&cmd)

	started := time.Now().UTC()
	if err := cmd.Start(); err != nil {
		cancel()
		if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) {
			return OperationSnapshot{}, errs.Wrap(errs.CategoryNotFound, err, "start %s", resolved)
		}
		return OperationSnapshot{}, errs.Wrap(errs.CategoryInternal, err, "start %s", resolved)
	}

	opID := m.ids.New("op")
	state := &operationState{
		id:        opID,
		spec:      spec,
		status:    OperationStatusRunning,
		cmd:       &cmd,
		cancelFn:  cancel,
		startedAt: started,
		doneCh:    make(chan struct{}),
	}

	m.mu.Lock()
	m.operations[opID] = state
	m.mu.Unlock()

	// Background worker that waits for process termination under runCtx
	go func() {
		defer cancel()
		waitErrCh := make(chan error, 1)
		go func() { waitErrCh <- cmd.Wait() }()

		var waitErr error
		select {
		case waitErr = <-waitErrCh:
			// Process exited on its own
		case <-runCtx.Done():
			killProcessGroup(cmd.Process)
			waitErr = <-waitErrCh
		}

		finished := time.Now().UTC()

		res := Result{
			Command:         append([]string{resolved}, spec.Args...),
			Dir:             spec.Dir,
			Env:             append([]string(nil), spec.Env...),
			Stdout:          stdout.buf.Bytes(),
			Stderr:          stderr.buf.Bytes(),
			StdoutTruncated: stdout.truncated,
			StderrTruncated: stderr.truncated,
			StartedAt:       started,
			FinishedAt:      finished,
			ExitCode:        -1,
		}

		state.mu.Lock()
		switch {
		case state.status == OperationStatusCancelled:
			res.Status = StatusCancelled
		case runCtx.Err() == context.DeadlineExceeded:
			state.status = OperationStatusTimeout
			res.Status = StatusTimeout
		case runCtx.Err() == context.Canceled:
			state.status = OperationStatusCancelled
			res.Status = StatusCancelled
		default:
			state.status = OperationStatusCompleted
			res.Status = StatusCompleted
		}

		if waitErr == nil {
			res.ExitCode = 0
		} else {
			var exitErr *exec.ExitError
			if errors.As(waitErr, &exitErr) {
				res.ExitCode = exitErr.ExitCode()
				if sig := signalName(exitErr); sig != "" {
					res.Signal = sig
					res.ExitCode = -1
				}
			} else if state.status == OperationStatusCompleted {
				state.status = OperationStatusFailed
				state.err = waitErr
			}
		}

		state.result = &res
		state.finishedAt = finished
		state.mu.Unlock()

		state.closeOnce.Do(func() {
			close(state.doneCh)
		})
	}()

	// Wait up to yieldThreshold or caller ctx
	select {
	case <-state.doneCh:
		return state.snapshot(), nil
	case <-time.After(yieldThreshold):
		return state.snapshot(), nil
	case <-ctx.Done():
		m.CancelOperation(opID)
		return state.snapshot(), ctx.Err()
	}
}

// GetOperation returns the current snapshot of the operation.
func (m *OperationManager) GetOperation(opID string) (OperationSnapshot, error) {
	m.mu.RLock()
	state, exists := m.operations[opID]
	m.mu.RUnlock()

	if !exists {
		return OperationSnapshot{}, errs.New(errs.CategoryNotFound, "operation %q not found", opID)
	}

	return state.snapshot(), nil
}

// CancelOperation terminates the operation if it is running (ADR-0016).
// It returns the snapshot and whether a cancellation signal was actually delivered.
// Repeated cancellation is idempotent and returns already_completed = false without error.
func (m *OperationManager) CancelOperation(opID string) (OperationSnapshot, bool, error) {
	m.mu.RLock()
	state, exists := m.operations[opID]
	m.mu.RUnlock()

	if !exists {
		return OperationSnapshot{}, false, errs.New(errs.CategoryNotFound, "operation %q not found", opID)
	}

	state.mu.Lock()
	if state.status != OperationStatusRunning {
		state.mu.Unlock()
		return state.snapshot(), false, nil
	}

	state.status = OperationStatusCancelled
	state.cancelFn()
	if state.cmd != nil && state.cmd.Process != nil {
		killProcessGroup(state.cmd.Process)
	}
	state.mu.Unlock()

	// Wait for cleanup
	<-state.doneCh
	return state.snapshot(), true, nil
}

// WaitOperation blocks until the operation completes or ctx is cancelled.
// If the operation already completed before this call, it returns immediately.
func (m *OperationManager) WaitOperation(ctx context.Context, opID string) (OperationSnapshot, error) {
	m.mu.RLock()
	state, exists := m.operations[opID]
	m.mu.RUnlock()

	if !exists {
		return OperationSnapshot{}, errs.New(errs.CategoryNotFound, "operation %q not found", opID)
	}

	select {
	case <-state.doneCh:
		return state.snapshot(), nil
	case <-ctx.Done():
		return state.snapshot(), ctx.Err()
	}
}
