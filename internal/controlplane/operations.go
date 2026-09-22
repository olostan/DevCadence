package controlplane

import (
	"context"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/storage"
)

// InitProjectInput registers a project.
//
// No repository is required. M1 is repository-independent by design: Git
// inspection and worktrees are M2, and forcing a repository here would make
// the domain core depend on machinery that does not exist yet.
type InitProjectInput struct {
	ProjectID      string
	Name           string
	MilestoneID    string
	MilestoneTitle string
	VisionRef      string
	CurrentOutcome string
	// RepositoryPath is recorded for provenance only. It is validated as an
	// absolute path when supplied, because docs/SECURITY.md §6 requires
	// canonical paths and a relative path would mean different things to
	// different processes.
	RepositoryPath   string
	AcceptedCommit   string
	Branch           string
	ActiveInvariants []string
	Actor            protocol.Actor
}

// InitProject creates a project by appending its first event.
func (s *Service) InitProject(ctx context.Context, in InitProjectInput) (Result, error) {
	if err := validateProjectID(in.ProjectID); err != nil {
		return Result{}, err
	}
	if in.RepositoryPath != "" && !strings.HasPrefix(in.RepositoryPath, "/") {
		return Result{}, errs.New(errs.CategoryInvalidArgument,
			"repository path %q must be absolute", in.RepositoryPath)
	}
	name := in.Name
	if name == "" {
		name = in.ProjectID
	}
	return s.Apply(ctx, Command{
		ProjectID:   in.ProjectID,
		Actor:       defaultActor(in.Actor, protocol.ActorHuman, "operator"),
		Correlation: events.Correlation{MilestoneID: in.MilestoneID},
		Payload: &events.ProjectInitialized{
			Name:             name,
			AcceptedCommit:   in.AcceptedCommit,
			Branch:           in.Branch,
			RepositoryPath:   in.RepositoryPath,
			VisionRef:        in.VisionRef,
			CurrentOutcome:   in.CurrentOutcome,
			MilestoneID:      in.MilestoneID,
			MilestoneTitle:   in.MilestoneTitle,
			ActiveInvariants: in.ActiveInvariants,
		},
	})
}

// CreateTaskInput describes a new task.
type CreateTaskInput struct {
	ProjectID   string
	Alias       string
	Title       string
	MilestoneID string
	ChangeClass protocol.ChangeClass
	DependsOn   []string
	Actor       protocol.Actor
}

// CreateTask appends a TaskCreated event, allocating the task identifier.
func (s *Service) CreateTask(ctx context.Context, in CreateTaskInput) (Result, error) {
	if in.Alias == "" {
		return Result{}, errs.New(errs.CategoryInvalidArgument, "task alias is required")
	}
	if in.Title == "" {
		return Result{}, errs.New(errs.CategoryInvalidArgument, "task title is required")
	}
	changeClass := in.ChangeClass
	if changeClass == "" {
		// An unclassified change would let systemic work take the fast path,
		// so the default is the cautious one (docs/LIFECYCLE.md §7).
		changeClass = protocol.ChangeSystemic
	}
	taskID := s.newID("tsk", s.clock.Now())
	return s.Apply(ctx, Command{
		ProjectID:   in.ProjectID,
		Actor:       defaultActor(in.Actor, protocol.ActorPrincipal, "principal"),
		Correlation: events.Correlation{TaskID: taskID, MilestoneID: in.MilestoneID},
		Payload: &events.TaskCreated{
			TaskID:      taskID,
			Alias:       in.Alias,
			Title:       in.Title,
			MilestoneID: in.MilestoneID,
			ChangeClass: changeClass,
			DependsOn:   in.DependsOn,
		},
	})
}

// ApproveWorkPackageInput approves a blueprint for a task.
type ApproveWorkPackageInput struct {
	ProjectID   string
	TaskAlias   string
	WorkPackage *protocol.EngineeringWorkPackage
	Actor       protocol.Actor
}

// ApproveWorkPackage stores the blueprint and moves the task to READY.
//
// The document and the event that references it are written in one
// transaction, so a WorkPackageApproved event can never point at a record
// that was not stored.
func (s *Service) ApproveWorkPackage(ctx context.Context, in ApproveWorkPackageInput) (Result, error) {
	if in.WorkPackage == nil {
		return Result{}, errs.New(errs.CategoryInvalidArgument, "work package is required")
	}
	task, err := s.resolveTask(ctx, in.ProjectID, in.TaskAlias)
	if err != nil {
		return Result{}, err
	}
	workPackage := *in.WorkPackage
	workPackage.SchemaVersion = protocol.SchemaVersion1
	workPackage.ProjectID = in.ProjectID
	workPackage.TaskID = task.ID
	if err := workPackage.Validate(); err != nil {
		return Result{}, err
	}
	digest, err := protocol.Digest(&workPackage)
	if err != nil {
		return Result{}, err
	}
	return s.Apply(ctx, Command{
		ProjectID: in.ProjectID,
		Actor:     defaultActor(in.Actor, protocol.ActorPrincipal, "principal"),
		Correlation: events.Correlation{
			TaskID:        task.ID,
			WorkPackageID: workPackage.WorkPackageID,
		},
		Records: []RecordToStore{{Version: workPackage.Version, Record: &workPackage}},
		Payload: &events.WorkPackageApproved{
			TaskID:               task.ID,
			WorkPackageID:        workPackage.WorkPackageID,
			WorkPackageVersion:   workPackage.Version,
			RecordDigest:         digest,
			ProjectStateRevision: workPackage.ProjectStateRevision,
			BaseCommit:           workPackage.BaseCommit,
			ChangeClass:          workPackage.ChangeClass,
		},
	})
}

// AppendTypedEventInput appends an already-built typed payload, optionally
// with the durable records it references.
//
// It exists so that the CLI can drive a synthetic project through its whole
// lifecycle without the control plane needing a bespoke method for every
// transition. The payload is still typed, registered and validated: this is
// not an escape hatch around the protocol.
//
// Records matters: a payload claiming a durable record is verified against
// the store, so a caller using this path must either supply that record here
// or reference one already stored. The helper stays useful for bootstrap and
// tests without becoming the one way to append an unbacked evidence claim.
type AppendTypedEventInput struct {
	ProjectID   string
	Payload     events.Payload
	Records     []RecordToStore
	Correlation events.Correlation
	Actor       protocol.Actor
}

// AppendTypedEvent appends one typed event together with any records it
// references.
func (s *Service) AppendTypedEvent(ctx context.Context, in AppendTypedEventInput) (Result, error) {
	return s.Apply(ctx, Command{
		ProjectID:   in.ProjectID,
		Actor:       defaultActor(in.Actor, protocol.ActorControlPlane, "devcadence"),
		Correlation: in.Correlation,
		Payload:     in.Payload,
		Records:     in.Records,
	})
}

// resolveTask maps a human-readable alias to the stored task.
func (s *Service) resolveTask(ctx context.Context, projectID, alias string) (taskRef, error) {
	var ref taskRef
	err := s.store.Read(ctx, func(tx *storage.Tx) error {
		task, err := tx.TaskByAlias(ctx, projectID, alias)
		if err != nil {
			return err
		}
		ref = taskRef{ID: task.ID, Alias: task.Alias}
		return nil
	})
	return ref, err
}

type taskRef struct {
	ID    string
	Alias string
}

// ResolveTaskID maps an alias to the opaque task identifier.
func (s *Service) ResolveTaskID(ctx context.Context, projectID, alias string) (string, error) {
	ref, err := s.resolveTask(ctx, projectID, alias)
	return ref.ID, err
}

// defaultActor fills in an actor when the caller did not name one.
func defaultActor(actor protocol.Actor, kind protocol.ActorKind, id string) protocol.Actor {
	if actor.Kind == "" {
		actor.Kind = kind
	}
	if actor.ID == "" {
		actor.ID = id
	}
	return actor
}

// validateProjectID keeps project identifiers usable as path and log
// components: lowercase letters, digits, hyphen and underscore only.
//
// The restriction is a security measure as much as a convenience one. Project
// identifiers end up in file names and log fields, and accepting separators
// or traversal sequences here would push the problem to every consumer.
func validateProjectID(id string) error {
	if id == "" {
		return errs.New(errs.CategoryInvalidArgument, "project id is required")
	}
	if len(id) > 64 {
		return errs.New(errs.CategoryInvalidArgument, "project id must be at most 64 characters")
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return errs.New(errs.CategoryInvalidArgument,
				"project id %q may contain only lowercase letters, digits, '-' and '_'", id)
		}
	}
	return nil
}
