package main

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/olostan/DevCadience/internal/controlplane"
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/events"
	"github.com/olostan/DevCadience/internal/protocol"
	"github.com/olostan/DevCadience/internal/storage"
	"github.com/olostan/DevCadience/internal/tasks"
)

func runTask(ctx context.Context, e *env, args []string) error {
	if len(args) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "usage: devcadience task <create|list|show|states>")
	}
	switch args[0] {
	case "create":
		return runTaskCreate(ctx, e, args[1:])
	case "list":
		return runTaskList(ctx, e, args[1:])
	case "show":
		return runTaskShow(ctx, e, args[1:])
	case "states":
		return runTaskStates(ctx, e, args[1:])
	default:
		return errs.New(errs.CategoryInvalidArgument, "unknown task subcommand %q", args[0])
	}
}

func runTaskCreate(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("task create", flag.ContinueOnError)
	in := controlplane.CreateTaskInput{}
	var changeClass, dependsOn, actorKind, actorID string
	fs.StringVar(&in.ProjectID, "project", "", "project identifier")
	fs.StringVar(&in.Alias, "alias", "", "human-readable task handle, e.g. DC-012")
	fs.StringVar(&in.Title, "title", "", "task title")
	fs.StringVar(&in.MilestoneID, "milestone", "", "milestone identifier (defaults to the active milestone)")
	fs.StringVar(&changeClass, "class", "", "change class: local, systemic or architectural")
	fs.StringVar(&dependsOn, "depends-on", "", "comma-separated task aliases this task depends on")
	fs.StringVar(&actorKind, "actor-kind", "principal", "actor kind recorded on the event")
	fs.StringVar(&actorID, "actor-id", "principal", "actor identifier recorded on the event")
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	in.ChangeClass = protocol.ChangeClass(changeClass)
	in.DependsOn = splitList(dependsOn)
	in.Actor = actorFromFlags(actorKind, actorID)

	service, store, err := e.openService(ctx, false)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	result, err := service.CreateTask(ctx, in)
	if err != nil {
		return err
	}
	created, ok := result.Event.Payload.(*events.TaskCreated)
	if !ok {
		// The payload is always TaskCreated here; the assertion exists so a
		// future refactor that changes it fails loudly rather than printing
		// the wrong identifier.
		return errs.New(errs.CategoryInternal, "task create produced a %s event", result.Event.EventType)
	}
	fmt.Fprintf(e.stdout, "created task %s (%s) in state %s\n", created.Alias, created.TaskID, tasks.StateProposed)
	return nil
}

func runTaskList(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("task list", flag.ContinueOnError)
	projectID := fs.String("project", "", "project identifier")
	stateFilter := fs.String("state", "", "comma-separated task states to include")
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	filter := storage.TaskFilter{ProjectID: *projectID}
	for _, name := range splitList(*stateFilter) {
		s := tasks.State(strings.ToLower(name))
		if !s.Valid() {
			return errs.New(errs.CategoryInvalidArgument, "%q is not a task state", name)
		}
		filter.States = append(filter.States, s)
	}

	service, store, err := e.openService(ctx, true)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	list, err := service.Tasks(ctx, filter)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(e.stdout, list)
	}
	if len(list) == 0 {
		fmt.Fprintln(e.stdout, "no tasks")
		return nil
	}
	for _, task := range list {
		line := fmt.Sprintf("%-10s %-24s %-12s %s", task.Alias, task.State, task.ChangeClass, task.Title)
		if task.Blocked != nil {
			line += fmt.Sprintf("  [blocked: %s -> %s]", task.Blocked.Trigger, task.Blocked.Authority)
		}
		fmt.Fprintln(e.stdout, line)
	}
	return nil
}

func runTaskShow(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("task show", flag.ContinueOnError)
	projectID := fs.String("project", "", "project identifier")
	alias := fs.String("task", "", "task alias")
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	if *alias == "" {
		return errs.New(errs.CategoryInvalidArgument, "task show requires -task")
	}
	service, store, err := e.openService(ctx, true)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	detail, err := service.TaskDetail(ctx, *projectID, *alias)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(e.stdout, detail)
	}
	task := detail.Task
	fmt.Fprintf(e.stdout, "task      %s (%s)\n", task.Alias, task.ID)
	fmt.Fprintf(e.stdout, "title     %s\n", task.Title)
	fmt.Fprintf(e.stdout, "state     %s\n", task.State)
	fmt.Fprintf(e.stdout, "class     %s\n", task.ChangeClass)
	fmt.Fprintf(e.stdout, "milestone %s\n", task.MilestoneID)
	if task.WorkPackageID != "" {
		fmt.Fprintf(e.stdout, "package   %s v%d\n", task.WorkPackageID, task.WorkPackageVersion)
	}
	if task.AcceptedCommit != "" {
		fmt.Fprintf(e.stdout, "accepted  %s\n", task.AcceptedCommit)
	}
	if task.Blocked != nil {
		fmt.Fprintf(e.stdout, "blocked   %s (from %s, authority %s)\n            %s\n",
			task.Blocked.Trigger, task.Blocked.BlockedFrom, task.Blocked.Authority, task.Blocked.Statement)
	}
	fmt.Fprintf(e.stdout, "allowed   %v\n", tasks.Allowed(task.State))

	fmt.Fprintf(e.stdout, "\nattempts (%d)\n", len(detail.Attempts))
	for _, attempt := range detail.Attempts {
		fmt.Fprintf(e.stdout, "  #%d %-18s %-14s role=%s", attempt.Ordinal, attempt.ID, attempt.Status, attempt.WorkerRole)
		if attempt.CandidateCommit != "" {
			fmt.Fprintf(e.stdout, " candidate=%s", attempt.CandidateCommit)
		}
		if attempt.BlockReason != nil {
			fmt.Fprintf(e.stdout, " block=%s", attempt.BlockReason.Trigger)
		}
		fmt.Fprintln(e.stdout)
	}

	fmt.Fprintf(e.stdout, "\nhistory (%d events)\n", len(detail.History))
	for _, event := range detail.History {
		fmt.Fprintf(e.stdout, "  %6d %-32s %s\n", event.Seq, event.EventType, event.OccurredAt)
	}
	return nil
}

// runTaskStates prints the canonical state machine. It makes the lifecycle
// inspectable without reading source, which is useful when driving a
// synthetic project by hand.
func runTaskStates(_ context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("task states", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	if *asJSON {
		graph := make(map[string][]string, len(tasks.AllStates()))
		for _, s := range tasks.AllStates() {
			allowed := tasks.Allowed(s)
			names := make([]string, 0, len(allowed))
			for _, next := range allowed {
				names = append(names, string(next))
			}
			graph[string(s)] = names
		}
		return writeJSON(e.stdout, graph)
	}
	for _, s := range tasks.AllStates() {
		fmt.Fprintf(e.stdout, "%-24s -> %v\n", s, tasks.Allowed(s))
	}
	return nil
}
