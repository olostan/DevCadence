package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/olostan/DevCadience/internal/errs"
)

func runState(ctx context.Context, e *env, args []string) error {
	if len(args) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "usage: devcadience state <show|rebuild>")
	}
	switch args[0] {
	case "show":
		return runStateShow(ctx, e, args[1:])
	case "rebuild":
		return runStateRebuild(ctx, e, args[1:])
	default:
		return errs.New(errs.CategoryInvalidArgument, "unknown state subcommand %q", args[0])
	}
}

func runStateShow(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("state show", flag.ContinueOnError)
	projectID := fs.String("project", "", "project identifier")
	atSeq := fs.Int64("at", 0, "reconstruct state as of this journal sequence (0 means current)")
	asJSON := fs.Bool("json", true, "emit the canonical ProjectState document as JSON")
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	service, store, err := e.openService(ctx, true)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	// A historical revision is reduced from the journal prefix rather than
	// read from a snapshot table, so any past revision is available without
	// materialising all of them.
	projectState, err := service.ProjectState(ctx, *projectID)
	if *atSeq > 0 {
		projectState, err = service.ProjectStateAt(ctx, *projectID, *atSeq)
	}
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(e.stdout, projectState)
	}
	fmt.Fprintf(e.stdout, "project   %s\n", projectState.ProjectID)
	fmt.Fprintf(e.stdout, "revision  %s (watermark %s)\n", projectState.StateRevision, deref(projectState.EventHighWatermark))
	fmt.Fprintf(e.stdout, "milestone %s %s (%d/%d)\n", projectState.Milestone.ID, projectState.Milestone.Title,
		projectState.Milestone.CompletedTasks, projectState.Milestone.TotalTasks)
	fmt.Fprintf(e.stdout, "ready     %v\nrunning   %v\nblocked   %v\nprincipal %v\n",
		projectState.Tasks.Ready, projectState.Tasks.Running,
		projectState.Tasks.Blocked, projectState.Tasks.AwaitingPrincipal)
	fmt.Fprintf(e.stdout, "validation %s\n", projectState.Validation.Status)
	return nil
}

func runStateRebuild(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("state rebuild", flag.ContinueOnError)
	projectID := fs.String("project", "", "project identifier")
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	service, store, err := e.openService(ctx, false)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	projectState, err := service.RebuildProjection(ctx, *projectID)
	if err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "rebuilt %s from the event journal; revision %s\n",
		projectState.ProjectID, projectState.StateRevision)
	return nil
}

func deref(s *string) string {
	if s == nil {
		return "none"
	}
	return *s
}
