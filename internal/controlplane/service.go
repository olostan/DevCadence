// Package controlplane holds the application services the CLI (and, from M4,
// the MCP adapter) call.
//
// docs/ARCHITECTURE.md §3 makes adapters thin: CLI and MCP must not contain
// orchestration policy or durable business logic. Everything that decides
// what a durable operation means lives here, so that a second adapter cannot
// implement it differently.
package controlplane

import (
	"context"
	"log/slog"
	"time"

	"github.com/olostan/DevCadience/internal/clock"
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/events"
	"github.com/olostan/DevCadience/internal/ids"
	"github.com/olostan/DevCadience/internal/observability"
	"github.com/olostan/DevCadience/internal/protocol"
	"github.com/olostan/DevCadience/internal/state"
	"github.com/olostan/DevCadience/internal/storage"
	"github.com/olostan/DevCadience/internal/tasks"
)

// Service is the control-plane application service.
type Service struct {
	store  *storage.Store
	clock  clock.Clock
	ids    ids.Source
	logger *slog.Logger
}

// Options configures a Service. Clock and IDs are injected so that tests can
// make every durable value deterministic (ENGINEERING_STANDARDS.md §17).
type Options struct {
	Store  *storage.Store
	Clock  clock.Clock
	IDs    ids.Source
	Logger *slog.Logger
}

// New builds a Service.
func New(opts Options) (*Service, error) {
	if opts.Store == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "controlplane: store is required")
	}
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}
	if opts.IDs == nil {
		opts.IDs = ids.NewULIDSource()
	}
	if opts.Logger == nil {
		opts.Logger = observability.NewLogger(observability.Options{})
	}
	return &Service{store: opts.Store, clock: opts.Clock, ids: opts.IDs, logger: opts.Logger}, nil
}

// Command describes one durable control-plane operation.
//
// Every state change goes through Apply as a Command, so there is exactly one
// place where an event is appended and the projection is updated, and exactly
// one transaction boundary protecting the pair.
type Command struct {
	ProjectID   string
	Actor       protocol.Actor
	Correlation events.Correlation
	Payload     events.Payload
	// Records are durable protocol documents to store in the same
	// transaction as the event that references them, so a reference can never
	// outlive its target.
	Records []RecordToStore
}

// RecordToStore is a protocol document written alongside an event.
type RecordToStore struct {
	Version int
	Record  protocol.Record
}

// Result reports what an applied command produced.
type Result struct {
	Event        events.Event
	ProjectState *protocol.ProjectState
	// RecordDigests maps each stored record's ID to its content digest.
	RecordDigests map[string]string
}

// Apply appends one event and updates the materialised projection atomically.
//
// The projection is recomputed by reducing the project's journal rather than
// patched in place. Two implementations of "what this event means" would
// eventually disagree, and the reducer is the one that must be right, since
// it is also what a rebuild uses. The cost is a full replay per write, which
// is acceptable for a single-user local control plane and is the first thing
// to revisit if it ever shows up in a measurement.
func (s *Service) Apply(ctx context.Context, cmd Command) (Result, error) {
	if cmd.ProjectID == "" {
		return Result{}, errs.New(errs.CategoryInvalidArgument, "command: project_id is required")
	}
	if cmd.Payload == nil {
		return Result{}, errs.New(errs.CategoryInvalidArgument, "command: payload is required")
	}
	if err := cmd.Actor.Validate(); err != nil {
		return Result{}, err
	}

	var result Result
	err := s.store.Write(ctx, func(tx *storage.Tx) error {
		projection, err := s.loadProjection(ctx, tx, cmd.ProjectID)
		if err != nil {
			return err
		}
		// The first event of a project must be ProjectInitialized, and only
		// the first: the reducer enforces both, here we only surface it early
		// enough that nothing has been written.
		if !projection.Initialised() && cmd.Payload.Type() != events.TypeProjectInitialized {
			return errs.New(errs.CategoryNotFound,
				"project %s has not been initialised", cmd.ProjectID)
		}

		result.RecordDigests = make(map[string]string, len(cmd.Records))
		for _, record := range cmd.Records {
			// High-value product-authority references are checked before the
			// record becomes durable. This is deliberately not a general
			// referential engine over every protocol relation; it covers the
			// one relation where a dangling reference would let the system
			// claim human authority it does not have (DCI-009, DCI-015).
			if err := checkProductAuthorityRefs(ctx, tx, cmd.ProjectID, record.Record); err != nil {
				return err
			}
			digest, err := tx.PutRecord(ctx, cmd.ProjectID, record.Version, record.Record)
			if err != nil {
				return err
			}
			result.RecordDigests[record.Record.RecordID()] = digest
		}

		// Correlation is derived from the typed payload here rather than
		// taken from the caller. Task history and observability read the
		// indexed correlation columns, so an adapter that omitted or
		// mis-set them could append a valid state change that never appears
		// in its task's history.
		correlation, err := canonicalCorrelation(cmd.Payload, cmd.Correlation)
		if err != nil {
			return err
		}

		now := s.clock.Now()
		event := events.Event{
			SchemaVersion: protocol.SchemaVersion1,
			EventID:       s.newID("evt", now),
			ProjectID:     cmd.ProjectID,
			EventType:     cmd.Payload.Type(),
			OccurredAt:    protocol.NewTimestamp(now),
			Actor:         cmd.Actor,
			Correlation:   correlation,
			Payload:       cmd.Payload,
		}
		appended, err := tx.AppendEvent(ctx, event)
		if err != nil {
			return err
		}
		// Applying after the append is what makes an illegal transition roll
		// the append back: the two are one unit of work.
		if err := projection.Apply(&appended); err != nil {
			return err
		}
		if err := tx.SaveProjection(ctx, projection); err != nil {
			return err
		}
		projectState, err := projection.ProjectState()
		if err != nil {
			return err
		}
		result.Event = appended
		result.ProjectState = projectState
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	// The log reports the correlation that was *stored*, not the one the
	// caller supplied: the two can differ, because the control plane derives
	// correlation from the typed payload. Logging the caller's version would
	// make the operational record disagree with the journal about the same
	// event (docs/OBSERVABILITY.md §9).
	stored := result.Event.Correlation
	correlation := observability.Correlation{
		ProjectID:     cmd.ProjectID,
		TaskID:        stored.TaskID,
		WorkPackageID: stored.WorkPackageID,
		AttemptID:     stored.AttemptID,
	}
	correlation.With(s.logger).InfoContext(ctx, "event appended",
		slog.String("event_type", string(result.Event.EventType)),
		slog.String("event_id", result.Event.EventID),
		slog.Int64("seq", result.Event.Seq),
		slog.String("state_revision", result.ProjectState.StateRevision),
	)
	return result, nil
}

// newID returns an identifier stamped with the supplied instant where the
// source supports it, so that an identifier and the record it names never
// disagree about when they were created.
func (s *Service) newID(prefix string, at time.Time) string {
	if stamped, ok := s.ids.(interface {
		NewAt(string, time.Time) string
	}); ok {
		return stamped.NewAt(prefix, at)
	}
	return s.ids.New(prefix)
}

// loadProjection reduces the project's whole journal.
func (s *Service) loadProjection(ctx context.Context, tx *storage.Tx, projectID string) (*state.Projection, error) {
	stream, err := tx.ReadEvents(ctx, storage.EventQuery{ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	return state.Reduce(stream)
}

// ProjectState returns the materialised canonical state.
func (s *Service) ProjectState(ctx context.Context, projectID string) (*protocol.ProjectState, error) {
	var out *protocol.ProjectState
	err := s.store.Read(ctx, func(tx *storage.Tx) error {
		projectState, err := tx.MaterialisedProjectState(ctx, projectID)
		if err != nil {
			return err
		}
		out = projectState
		return nil
	})
	return out, err
}

// ProjectStateAt reconstructs the canonical state as of a journal sequence.
//
// It reduces the prefix rather than reading a stored snapshot, which is what
// makes any historical revision retrievable without materialising every one
// of them.
func (s *Service) ProjectStateAt(ctx context.Context, projectID string, seq int64) (*protocol.ProjectState, error) {
	var out *protocol.ProjectState
	err := s.store.Read(ctx, func(tx *storage.Tx) error {
		stream, err := tx.ReadEvents(ctx, storage.EventQuery{ProjectID: projectID, UpToSeq: seq})
		if err != nil {
			return err
		}
		projection, err := state.Reduce(stream)
		if err != nil {
			return err
		}
		projectState, err := projection.ProjectState()
		if err != nil {
			return err
		}
		out = projectState
		return nil
	})
	return out, err
}

// RebuildProjection destroys and reconstructs the derived view from the
// journal alone.
//
// This is the operator procedure of docs/PROJECT_STATE.md §16 and the proof
// of DCI-053. It is also what tests use to show that the materialised state
// carries no information the journal does not.
func (s *Service) RebuildProjection(ctx context.Context, projectID string) (*protocol.ProjectState, error) {
	var out *protocol.ProjectState
	err := s.store.Write(ctx, func(tx *storage.Tx) error {
		projection, err := s.loadProjection(ctx, tx, projectID)
		if err != nil {
			return err
		}
		if !projection.Initialised() {
			return errs.New(errs.CategoryNotFound, "project %s has no events to rebuild from", projectID)
		}
		if err := tx.DropProjection(ctx, projectID); err != nil {
			return err
		}
		if err := tx.SaveProjection(ctx, projection); err != nil {
			return err
		}
		projectState, err := projection.ProjectState()
		if err != nil {
			return err
		}
		out = projectState
		return nil
	})
	return out, err
}

// Events returns journal entries matching the query.
func (s *Service) Events(ctx context.Context, query storage.EventQuery) ([]events.Event, error) {
	var out []events.Event
	err := s.store.Read(ctx, func(tx *storage.Tx) error {
		stream, err := tx.ReadEvents(ctx, query)
		if err != nil {
			return err
		}
		out = stream
		return nil
	})
	return out, err
}

// Projects lists materialised projects.
func (s *Service) Projects(ctx context.Context) ([]storage.ProjectSummary, error) {
	var out []storage.ProjectSummary
	err := s.store.Read(ctx, func(tx *storage.Tx) error {
		summaries, err := tx.ProjectSummaries(ctx)
		if err != nil {
			return err
		}
		out = summaries
		return nil
	})
	return out, err
}

// Tasks lists materialised tasks.
func (s *Service) Tasks(ctx context.Context, filter storage.TaskFilter) ([]*tasks.Task, error) {
	var out []*tasks.Task
	err := s.store.Read(ctx, func(tx *storage.Tx) error {
		list, err := tx.Tasks(ctx, filter)
		if err != nil {
			return err
		}
		out = list
		return nil
	})
	return out, err
}

// TaskDetail is everything `task show` needs about one task.
type TaskDetail struct {
	Task     *tasks.Task
	Attempts []*tasks.Attempt
	// History is the task's correlated events, oldest first, so that an
	// operator can see how the task reached its current state.
	History []events.Event
}

// TaskDetail returns one task with its attempts and history.
func (s *Service) TaskDetail(ctx context.Context, projectID, alias string) (*TaskDetail, error) {
	var out *TaskDetail
	err := s.store.Read(ctx, func(tx *storage.Tx) error {
		task, err := tx.TaskByAlias(ctx, projectID, alias)
		if err != nil {
			return err
		}
		attempts, err := tx.Attempts(ctx, task.ID)
		if err != nil {
			return err
		}
		history, err := tx.ReadEvents(ctx, storage.EventQuery{ProjectID: projectID, TaskID: task.ID})
		if err != nil {
			return err
		}
		out = &TaskDetail{Task: task, Attempts: attempts, History: history}
		return nil
	})
	return out, err
}

// Record returns a stored durable protocol document.
//
// A record is identified within its project: semantic identifiers such as
// "PD-001" or "wp_1" are chosen per project and collide across them, so the
// project is part of the lookup rather than a filter applied afterwards.
func (s *Service) Record(ctx context.Context, projectID, kind, id string, version int) (storage.StoredRecord, error) {
	var out storage.StoredRecord
	err := s.store.Read(ctx, func(tx *storage.Tx) error {
		record, err := tx.Record(ctx, projectID, kind, id, version)
		if err != nil {
			return err
		}
		out = record
		return nil
	})
	return out, err
}
