package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	"github.com/olostan/DevCadience/internal/controlplane"
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/protocol"
)

func runProject(ctx context.Context, e *env, args []string) error {
	if len(args) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "usage: devcadience project <init|list>")
	}
	switch args[0] {
	case "init":
		return runProjectInit(ctx, e, args[1:])
	case "list":
		return runProjectList(ctx, e, args[1:])
	default:
		return errs.New(errs.CategoryInvalidArgument, "unknown project subcommand %q", args[0])
	}
}

func runProjectInit(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("project init", flag.ContinueOnError)
	in := controlplane.InitProjectInput{}
	var invariants string
	fs.StringVar(&in.ProjectID, "id", "", "project identifier (lowercase letters, digits, '-', '_')")
	fs.StringVar(&in.Name, "name", "", "human-readable project name (defaults to the id)")
	fs.StringVar(&in.MilestoneID, "milestone-id", "", "active milestone identifier, e.g. M1")
	fs.StringVar(&in.MilestoneTitle, "milestone-title", "", "active milestone title")
	fs.StringVar(&in.VisionRef, "vision-ref", "", "reference to the project vision document")
	fs.StringVar(&in.CurrentOutcome, "outcome", "", "current product outcome being pursued")
	fs.StringVar(&in.RepositoryPath, "repository", "", "absolute path of the target repository (recorded only)")
	fs.StringVar(&in.AcceptedCommit, "accepted-commit", "", "accepted baseline commit, if one exists")
	fs.StringVar(&in.Branch, "branch", "", "accepted branch name")
	fs.StringVar(&invariants, "invariants", "", "comma-separated invariant identifiers the project binds to")
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	in.ActiveInvariants = splitList(invariants)

	service, store, err := e.openService(ctx, false)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	result, err := service.InitProject(ctx, in)
	if err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "initialised project %s at revision %s (database %s)\n",
		in.ProjectID, result.ProjectState.StateRevision, store.Path())
	return nil
}

func runProjectList(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("project list", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	service, store, err := e.openService(ctx, true)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	summaries, err := service.Projects(ctx)
	if err != nil {
		return err
	}
	if *asJSON {
		type row struct {
			ProjectID     string `json:"project_id"`
			Name          string `json:"name"`
			StateRevision string `json:"state_revision"`
			HighWatermark int64  `json:"event_high_watermark"`
		}
		rows := make([]row, 0, len(summaries))
		for _, s := range summaries {
			rows = append(rows, row{s.ProjectID, s.Name, s.StateRevision, s.HighWatermark})
		}
		return writeJSON(e.stdout, rows)
	}
	if len(summaries) == 0 {
		fmt.Fprintln(e.stdout, "no projects")
		return nil
	}
	for _, s := range summaries {
		fmt.Fprintf(e.stdout, "%-24s %-20s %s (watermark %d)\n", s.ProjectID, s.Name, s.StateRevision, s.HighWatermark)
	}
	return nil
}

// writeJSON emits indented JSON followed by a newline. Every --json output
// path goes through it so the CLI's machine-readable form is consistent.
func writeJSON(w interface{ Write([]byte) (int, error) }, v any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	// HTML escaping would mangle identifiers and prose in state documents.
	encoder.SetEscapeHTML(false)
	return errs.Wrap(errs.CategoryInternal, encoder.Encode(v), "write json output")
}

// actorFromFlags builds the actor attribution for a CLI-issued command.
func actorFromFlags(kind, id string) protocol.Actor {
	return protocol.Actor{Kind: protocol.ActorKind(kind), ID: id}
}
