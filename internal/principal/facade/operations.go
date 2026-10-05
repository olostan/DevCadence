package facade

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/principal"
)

// Operation registry bounds (R7).
const (
	MaxActiveOperations    = 64
	MaxCompletedOperations = 256
	// DefaultYield is how long a caller waits before an operation yields a
	// handle. It is a response threshold only: it is neither the operation's
	// deadline nor a durability promise.
	DefaultYield = 10 * time.Second
)

// OperationFunc is the body of a process-scoped operation. It returns an
// opaque result handle on success.
type OperationFunc func(ctx context.Context) (resultHandle string, err error)

type operation struct {
	ref     OperationRef
	project string
	cancel  context.CancelFunc
	done    chan struct{}
	handle  string
	failure *principal.SemanticError
}

// OperationRegistry tracks process-scoped operations. Handles carry the
// registry's instance identity: a handle issued by another process (or before a
// restart) is OPERATION_LOST and is never replayed. Nothing here is durable.
type OperationRegistry struct {
	mu        sync.Mutex
	instance  string
	seq       uint64
	ops       map[string]*operation
	completed []string
	active    int
	closed    bool
	wg        sync.WaitGroup
}

// NewOperationRegistry builds a registry. An empty instanceID is replaced by a
// random one.
func NewOperationRegistry(instanceID string) (*OperationRegistry, error) {
	if instanceID == "" {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, errs.Wrap(errs.CategoryInternal, err, "operation registry instance id")
		}
		instanceID = "inst_" + hex.EncodeToString(b[:])
	}
	if err := principal.ValidateID("instance_id", instanceID); err != nil {
		return nil, err
	}
	return &OperationRegistry{instance: instanceID, ops: map[string]*operation{}}, nil
}

// InstanceID returns the process instance identity stamped on every handle.
func (r *OperationRegistry) InstanceID() string { return r.instance }

// Start runs fn as a process-scoped operation. An unknown (non-positive)
// deadline denies scheduling; a full registry denies new work and never evicts
// an active operation.
func (r *OperationRegistry) Start(project, kind string, deadline time.Duration, fn OperationFunc) (OperationRef, error) {
	if deadline <= 0 {
		return OperationRef{}, coded(principal.CodePolicyDenied, false, nil, "operation deadline is unknown")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return OperationRef{}, coded(principal.CodeCancelled, true, nil, "operation registry is closed")
	}
	if r.active >= MaxActiveOperations {
		return OperationRef{}, coded(principal.CodeContextUnfit, true, nil, "too many active operations")
	}
	r.seq++
	id := fmt.Sprintf("op_%06d", r.seq)
	ref := OperationRef{ID: id, InstanceID: r.instance, Kind: kind, Status: principal.StatusRunning}
	if err := ref.Validate(); err != nil {
		return OperationRef{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	op := &operation{ref: ref, project: project, cancel: cancel, done: make(chan struct{})}
	r.ops[id] = op
	r.active++
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		handle, err := fn(ctx)
		ctxErr := ctx.Err() // read before cancel: cancel itself sets the error
		cancel()
		r.finish(op, handle, err, ctxErr)
	}()
	return ref, nil
}

func (r *OperationRegistry) finish(op *operation, handle string, err, ctxErr error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case err == nil:
		op.ref.Status = principal.StatusCompleted
		op.handle = handle
	case ctxErr != nil || r.closed:
		op.ref.Status = principal.StatusCancelled
		se := principal.NewSemanticError(principal.CodeCancelled, nil, true)
		op.failure = &se
	default:
		op.ref.Status = principal.StatusFailed
		se := semanticFor(err)
		op.failure = &se
	}
	r.active--
	r.completed = append(r.completed, op.ref.ID)
	for len(r.completed) > MaxCompletedOperations {
		delete(r.ops, r.completed[0])
		r.completed = r.completed[1:]
	}
	close(op.done)
}

// Wait blocks until the operation completes or the yield threshold passes and
// returns the current handle. A zero yield uses DefaultYield.
func (r *OperationRegistry) Wait(ctx context.Context, ref OperationRef, yield time.Duration) OperationRef {
	if yield <= 0 {
		yield = DefaultYield
	}
	r.mu.Lock()
	op := r.ops[ref.ID]
	r.mu.Unlock()
	if op == nil || ref.InstanceID != r.instance {
		return ref
	}
	timer := time.NewTimer(yield)
	defer timer.Stop()
	select {
	case <-op.done:
	case <-timer.C:
	case <-ctx.Done():
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return op.ref
}

// Lookup reports an operation's status. Another instance, an unknown or an
// expired handle is OPERATION_LOST; an operation of another project is denied.
func (r *OperationRegistry) Lookup(project string, ref OperationRef) (OperationStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	op, ok := r.ops[ref.ID]
	if !ok || ref.InstanceID != r.instance {
		return OperationStatus{}, coded(principal.CodeOperationLost, false, nil, "operation handle is not live")
	}
	if op.project != project {
		return OperationStatus{}, coded(principal.CodePolicyDenied, false, nil, "operation belongs to another project")
	}
	return OperationStatus{Operation: op.ref, ResultHandle: op.handle, Error: op.failure}, nil
}

// Close cancels every active operation (EOF or shutdown) and waits for them.
// Canonical task and attempt records are never invented or marked done.
func (r *OperationRegistry) Close() {
	r.mu.Lock()
	r.closed = true
	for _, op := range r.ops {
		if op.ref.Status == principal.StatusRunning {
			op.cancel()
		}
	}
	r.mu.Unlock()
	r.wg.Wait()
}
