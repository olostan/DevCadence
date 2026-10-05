package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/observability"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/state"
	"github.com/olostan/DevCadence/internal/storage"
	"github.com/olostan/DevCadence/internal/tasks"
)

// Batch bounds (WP-M5-1).
const (
	// MaxBatchCommands bounds the members of one batch.
	MaxBatchCommands = 256
	// MaxBatchGuards bounds preconditions and postconditions each.
	MaxBatchGuards = 8
	// MaxBatchBytes bounds the aggregate serialized records and payloads.
	MaxBatchBytes = 1 << 20
)

// conflictSentinel is an exported error identity that is also a
// CategoryConflict. errors.Is matches the sentinel by identity, so a stale
// project state, a stale Work Package and a busy store are distinguishable by
// a facade even though all three are conflicts; none of them reinterprets any
// other conflict.
type conflictSentinel struct{ message string }

func (e *conflictSentinel) Error() string { return "conflict: " + e.message }
func (e *conflictSentinel) Unwrap() error { return errs.ErrConflict }

var (
	// ErrStaleProjectState reports that the caller's expected state revision
	// is not the current one. Nothing was written.
	ErrStaleProjectState error = &conflictSentinel{"project state revision is stale"}
	// ErrStaleWorkPackage reports that the guarded Work Package is not the
	// current approved plan for its task. Nothing was written.
	ErrStaleWorkPackage error = &conflictSentinel{"work package is stale"}
	// ErrStorageBusy reports that the store's write lock stayed contended for
	// the whole contention budget. Nothing was written, and it is not a
	// staleness verdict.
	ErrStorageBusy error = &conflictSentinel{"storage write lock is busy"}
)

// WorkPackageGuard is the start-execution guard: it names the exact approved
// tuple a batch intends to start work against. It is not for accept, reject,
// validation or review, which check current candidate lineage instead.
type WorkPackageGuard struct {
	TaskID        string
	WorkPackageID string
	Version       int
	Digest        string
	BaseCommit    string
}

// BatchReadView is the read-only, project-bound window a guard sees. It cannot
// write, run tools, consult a network or mint authority, and it is retired
// when Check returns: later use fails with an invalid-transition error (or nil
// ProjectState).
type BatchReadView interface {
	// ProjectState renders a fresh copy of the project state the guard is
	// checking: the pre-batch state for preconditions, the final working state
	// for postconditions. It returns nil once the view is retired.
	ProjectState() *protocol.ProjectState
	// Record returns a digest-verified copy of one stored record of the batch
	// project, including records written earlier in this same batch.
	Record(ctx context.Context, kind, id string, version int) (storage.StoredRecord, error)
}

// BatchGuard is a trusted in-process application callback. It is never a wire
// field. It must have no external effects; a panic rolls the batch back and
// surfaces as an internal failure.
type BatchGuard interface {
	Check(ctx context.Context, view BatchReadView) error
}

// BatchCommand is an expected-prefix, all-or-nothing group of commands.
type BatchCommand struct {
	ProjectID string
	// Actor is the server binding identity, not request data.
	Actor       protocol.Actor
	Correlation events.Correlation
	// ExpectedStateRevision is the full canonical prefix identity the caller
	// observed. An uninitialised project's prefix is principal.EmptyStateRevision.
	ExpectedStateRevision string
	WorkPackage           *WorkPackageGuard
	Preconditions         []BatchGuard
	Postconditions        []BatchGuard
	Commands              []Command
}

// BatchResult reports a committed batch.
type BatchResult struct {
	// Results are in event order. Each member's ProjectState is the state
	// after that member within the transaction; only the final one, which is
	// also ProjectState, was ever committed as a projection.
	Results      []Result
	ProjectState *protocol.ProjectState
}

// ApplyBatch commits every command of the batch, or none.
//
// The transaction takes the SQLite write lock before reading anything, then
// compares the expected prefix, optionally the Work Package guard, runs the
// preconditions, applies each member in order against one working projection,
// runs the postconditions and persists the final projection once. Any failure
// rolls everything back. A refused call changes no record, event or projection.
func (s *Service) ApplyBatch(ctx context.Context, cmd BatchCommand) (BatchResult, error) {
	if err := validateBatch(ctx, cmd); err != nil {
		return BatchResult{}, err
	}

	var out BatchResult
	err := s.store.WriteSerialized(ctx, func(tx *storage.Tx) error {
		// Every attempt starts clean: a retried transaction re-reads everything.
		out = BatchResult{}
		projection, err := s.loadProjection(ctx, tx, cmd.ProjectID)
		if err != nil {
			return err
		}
		if current := state.StateRevision(projection.HighWatermark); current != cmd.ExpectedStateRevision {
			return fmt.Errorf("%w: expected %s, current %s",
				ErrStaleProjectState, cmd.ExpectedStateRevision, current)
		}
		if cmd.WorkPackage != nil {
			if err := checkWorkPackageGuard(ctx, tx, cmd.ProjectID, projection, *cmd.WorkPackage); err != nil {
				return err
			}
		}
		if err := runGuards(ctx, tx, cmd.ProjectID, projection, cmd.Preconditions, "precondition"); err != nil {
			return err
		}
		results := make([]Result, 0, len(cmd.Commands))
		for _, member := range cmd.Commands {
			applied, err := s.applyMember(ctx, tx, projection, member, false)
			if err != nil {
				return err
			}
			results = append(results, applied)
		}
		if err := runGuards(ctx, tx, cmd.ProjectID, projection, cmd.Postconditions, "postcondition"); err != nil {
			return err
		}
		if err := tx.SaveProjection(ctx, projection); err != nil {
			return err
		}
		finalState, err := projection.ProjectState()
		if err != nil {
			return err
		}
		out = BatchResult{Results: results, ProjectState: finalState}
		return nil
	})
	if err != nil {
		if errors.Is(err, storage.ErrBusy) {
			return BatchResult{}, fmt.Errorf("%w: %w", ErrStorageBusy, err)
		}
		return BatchResult{}, err
	}

	for _, result := range out.Results {
		stored := result.Event.Correlation
		observability.Correlation{
			ProjectID:     cmd.ProjectID,
			TaskID:        stored.TaskID,
			WorkPackageID: stored.WorkPackageID,
			AttemptID:     stored.AttemptID,
		}.With(s.logger).InfoContext(ctx, "event appended",
			slog.String("event_type", string(result.Event.EventType)),
			slog.String("event_id", result.Event.EventID),
			slog.Int64("seq", result.Event.Seq),
			slog.String("state_revision", out.ProjectState.StateRevision),
		)
	}
	return out, nil
}

// validateBatch applies every structural check that needs no store.
func validateBatch(ctx context.Context, cmd BatchCommand) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if cmd.ProjectID == "" {
		return errs.New(errs.CategoryInvalidArgument, "batch: project_id is required")
	}
	if err := cmd.Actor.Validate(); err != nil {
		return err
	}
	if cmd.ExpectedStateRevision == "" {
		return errs.New(errs.CategoryInvalidArgument, "batch: expected_state_revision is required")
	}
	if _, err := principal.ParseStateRevision(cmd.ExpectedStateRevision); err != nil {
		return err
	}
	if n := len(cmd.Commands); n < 1 || n > MaxBatchCommands {
		return errs.New(errs.CategoryInvalidArgument,
			"batch: %d commands; a batch holds 1 to %d", n, MaxBatchCommands)
	}
	if len(cmd.Preconditions) > MaxBatchGuards || len(cmd.Postconditions) > MaxBatchGuards {
		return errs.New(errs.CategoryInvalidArgument,
			"batch: at most %d preconditions and %d postconditions", MaxBatchGuards, MaxBatchGuards)
	}
	for _, guards := range [][]BatchGuard{cmd.Preconditions, cmd.Postconditions} {
		for i, guard := range guards {
			if guard == nil {
				return errs.New(errs.CategoryInvalidArgument, "batch: guard %d is nil", i)
			}
		}
	}
	if cmd.WorkPackage != nil {
		g := cmd.WorkPackage
		if g.TaskID == "" || g.WorkPackageID == "" || g.Version < 1 || g.Digest == "" || g.BaseCommit == "" {
			return errs.New(errs.CategoryInvalidArgument,
				"batch: work package guard needs task, work package, version >= 1, digest and base commit")
		}
	}

	type recordIdentity struct {
		kind, id string
		version  int
	}
	seen := map[recordIdentity]bool{}
	total := 0
	for i, member := range cmd.Commands {
		if member.Payload == nil {
			return errs.New(errs.CategoryInvalidArgument, "batch: command %d has no payload", i)
		}
		if member.ProjectID != cmd.ProjectID {
			return errs.New(errs.CategoryInvalidArgument,
				"batch: command %d is for project %q, not %q", i, member.ProjectID, cmd.ProjectID)
		}
		if member.Actor != cmd.Actor {
			return errs.New(errs.CategoryInvalidArgument, "batch: command %d names a different actor", i)
		}
		if member.Correlation != cmd.Correlation {
			return errs.New(errs.CategoryInvalidArgument, "batch: command %d names a different correlation", i)
		}
		payload, err := json.Marshal(member.Payload)
		if err != nil {
			return errs.Wrap(errs.CategoryInvalidArgument, err, "batch: command %d payload", i)
		}
		total += len(payload)
		for _, record := range member.Records {
			if record.Record == nil || record.Version < 1 {
				return errs.New(errs.CategoryInvalidArgument,
					"batch: command %d has a nil record or a version below 1", i)
			}
			identity := recordIdentity{record.Record.RecordKind(), record.Record.RecordID(), record.Version}
			if seen[identity] {
				return errs.New(errs.CategoryInvalidArgument,
					"batch: record %s %s version %d appears twice", identity.kind, identity.id, identity.version)
			}
			seen[identity] = true
			canonical, err := protocol.CanonicalJSON(record.Record)
			if err != nil {
				return errs.Wrap(errs.CategoryInvalidArgument, err, "batch: command %d record", i)
			}
			total += len(canonical)
		}
	}
	if total > MaxBatchBytes {
		return errs.New(errs.CategoryInvalidArgument,
			"batch: %d serialized bytes exceed the %d byte limit", total, MaxBatchBytes)
	}
	return nil
}

// checkWorkPackageGuard is the start-execution freshness check. It never
// compares the Work Package's planning revision with the global prefix: the
// approval itself advanced the prefix, and events of other tasks or of
// discovery do not make this plan stale. Staleness is read from the task's own
// journal since its approval.
func checkWorkPackageGuard(
	ctx context.Context, tx *storage.Tx, projectID string, projection *state.Projection, g WorkPackageGuard,
) error {
	stale := func(format string, args ...any) error {
		return fmt.Errorf("%w: "+format, append([]any{ErrStaleWorkPackage}, args...)...)
	}
	task, err := projection.Task(g.TaskID)
	if err != nil {
		return err
	}
	if task.State != tasks.StateReady {
		return stale("task %s is %s, not ready", task.Alias, task.State)
	}
	if task.WorkPackageID != g.WorkPackageID || task.WorkPackageVersion != g.Version {
		return stale("task %s is approved for %s v%d, not %s v%d",
			task.Alias, task.WorkPackageID, task.WorkPackageVersion, g.WorkPackageID, g.Version)
	}

	approvals, err := tx.ReadEvents(ctx, storage.EventQuery{
		ProjectID: projectID, TaskID: g.TaskID,
		Types: []events.Type{events.TypeWorkPackageApproved}, Newest: true, Limit: 1,
	})
	if err != nil {
		return err
	}
	if len(approvals) == 0 {
		return stale("task %s has no recorded approval", task.Alias)
	}
	approval := approvals[0]
	approved, ok := approval.Payload.(*events.WorkPackageApproved)
	if !ok {
		return errs.New(errs.CategoryIntegrity, "event %s is not a WorkPackageApproved payload", approval.EventID)
	}
	if approved.WorkPackageID != g.WorkPackageID || approved.WorkPackageVersion != g.Version ||
		approved.RecordDigest != g.Digest || approved.BaseCommit != g.BaseCommit {
		return stale("the latest approval of task %s names a different work package tuple", task.Alias)
	}

	// Any later event correlated with the task (moved, revised, blocked,
	// resumed, candidate produced, ...) supersedes the approval.
	later, err := tx.ReadEvents(ctx, storage.EventQuery{
		ProjectID: projectID, TaskID: g.TaskID, AfterSeq: approval.Seq, Limit: 1,
	})
	if err != nil {
		return err
	}
	if len(later) > 0 {
		return stale("task %s has event %s (%s) after its approval", task.Alias, later[0].EventID, later[0].EventType)
	}

	stored, err := tx.Record(ctx, projectID, "EngineeringWorkPackage", g.WorkPackageID, g.Version)
	if err != nil {
		return err
	}
	if stored.Digest != g.Digest {
		return stale("stored work package %s v%d has digest %s, not %s",
			g.WorkPackageID, g.Version, stored.Digest, g.Digest)
	}
	var wp protocol.EngineeringWorkPackage
	if err := protocol.Unmarshal([]byte(stored.Document), &wp); err != nil {
		return errs.Wrap(errs.CategoryIntegrity, err, "stored work package %s cannot be decoded", g.WorkPackageID)
	}
	if wp.WorkPackageID != g.WorkPackageID || wp.Version != g.Version || wp.TaskID != g.TaskID ||
		wp.BaseCommit != g.BaseCommit {
		return errs.New(errs.CategoryIntegrity,
			"stored work package %s v%d disagrees with its approval", g.WorkPackageID, g.Version)
	}

	// A repository-backed project must have a registered accepted base, and
	// the plan must have been written against exactly that base. A project with
	// no repository (Day-0) has no base to compare.
	if projection.RepositoryPath != "" {
		if projection.AcceptedCommit == "" {
			return stale("the registered repository has no accepted base commit")
		}
		if projection.AcceptedCommit != g.BaseCommit {
			return stale("the accepted base is %s, not the work package base %s",
				projection.AcceptedCommit, g.BaseCommit)
		}
	}
	return nil
}

// runGuards runs trusted callbacks in order against a read-only view. A panic
// or an error aborts the batch; the view is retired before returning either way.
func runGuards(
	ctx context.Context, tx *storage.Tx, projectID string, projection *state.Projection,
	guards []BatchGuard, phase string,
) error {
	if len(guards) == 0 {
		return nil
	}
	for i, guard := range guards {
		view := &batchView{tx: tx, projectID: projectID, projection: projection}
		view.live.Store(true)
		err := invokeGuard(ctx, guard, view)
		view.live.Store(false)
		if err != nil {
			return fmt.Errorf("batch %s %d refused: %w", phase, i, err)
		}
	}
	return nil
}

func invokeGuard(ctx context.Context, guard BatchGuard, view BatchReadView) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = errs.New(errs.CategoryInternal, "a batch guard panicked (%T); the batch was rolled back", recovered)
		}
	}()
	return guard.Check(ctx, view)
}

// batchView implements BatchReadView over one transaction.
type batchView struct {
	tx         *storage.Tx
	projectID  string
	projection *state.Projection
	live       atomic.Bool
}

// ProjectState implements BatchReadView.
func (v *batchView) ProjectState() *protocol.ProjectState {
	if !v.live.Load() {
		return nil
	}
	rendered, err := v.projection.ProjectState()
	if err != nil {
		return nil
	}
	return rendered
}

// Record implements BatchReadView.
func (v *batchView) Record(ctx context.Context, kind, id string, version int) (storage.StoredRecord, error) {
	if !v.live.Load() {
		return storage.StoredRecord{}, errs.New(errs.CategoryInvalidTransition,
			"the batch read view was retired when its guard returned")
	}
	if kind == "" || id == "" || version < 1 {
		return storage.StoredRecord{}, errs.New(errs.CategoryInvalidArgument,
			"record lookup needs a kind, an id and a version >= 1")
	}
	return v.tx.Record(ctx, v.projectID, kind, id, version)
}
