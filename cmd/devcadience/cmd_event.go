package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/olostan/DevCadience/internal/controlplane"
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/events"
	"github.com/olostan/DevCadience/internal/storage"
)

// maxPayloadBytes bounds how much JSON the CLI will read for one event.
//
// Payloads are compact transition facts. A bound turns a mistaken `-payload`
// argument pointing at a huge file into a clear error rather than an
// out-of-memory condition (NFR-007: resource use is bounded by policy).
const maxPayloadBytes = 1 << 20

func runEvent(ctx context.Context, e *env, args []string) error {
	if len(args) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "usage: devcadience event <append|types>")
	}
	switch args[0] {
	case "append":
		return runEventAppend(ctx, e, args[1:])
	case "types":
		return runEventTypes(ctx, e, args[1:])
	default:
		return errs.New(errs.CategoryInvalidArgument, "unknown event subcommand %q", args[0])
	}
}

func runEventAppend(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("event append", flag.ContinueOnError)
	projectID := fs.String("project", "", "project identifier")
	eventType := fs.String("type", "", "event type, e.g. TaskDelegated (see `devcadience event types`)")
	payloadArg := fs.String("payload", "-", "payload JSON: inline, @file, or - for stdin")
	taskAlias := fs.String("task", "", "task alias; fills task_id in the payload when it is not set")
	actorKind := fs.String("actor-kind", "control_plane", "actor kind recorded on the event")
	actorID := fs.String("actor-id", "devcadience", "actor identifier recorded on the event")
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	if *eventType == "" {
		return errs.New(errs.CategoryInvalidArgument, "event append requires -type")
	}
	raw, err := readPayload(*payloadArg, os.Stdin)
	if err != nil {
		return err
	}

	service, store, err := e.openService(ctx, false)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	if *taskAlias != "" {
		taskID, err := service.ResolveTaskID(ctx, *projectID, *taskAlias)
		if err != nil {
			return err
		}
		raw, err = withTaskID(raw, taskID)
		if err != nil {
			return err
		}
	}

	// DecodePayload rejects unknown event types and unknown payload fields,
	// so an operator typo cannot append a record the reducer would have to
	// guess about.
	payload, err := events.DecodePayload(events.Type(*eventType), json.RawMessage(raw))
	if err != nil {
		return err
	}
	result, err := service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID:   *projectID,
		Payload:     payload,
		Correlation: events.CorrelationFor(payload),
		Actor:       actorFromFlags(*actorKind, *actorID),
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "appended %s as %s (seq %d); state revision %s\n",
		result.Event.EventType, result.Event.EventID, result.Event.Seq, result.ProjectState.StateRevision)
	return nil
}

func runEventTypes(_ context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("event types", flag.ContinueOnError)
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	for _, t := range events.RegisteredTypes() {
		fmt.Fprintln(e.stdout, t)
	}
	return nil
}

func runEvents(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("events list", flag.ContinueOnError)
	projectID := fs.String("project", "", "project identifier")
	typeFilter := fs.String("type", "", "comma-separated event types to include")
	taskAlias := fs.String("task", "", "restrict to one task's correlated events")
	limit := fs.Int("limit", 0, "maximum number of events (0 means all)")
	newest := fs.Bool("newest", false, "list newest first")
	asJSON := fs.Bool("json", false, "emit JSON")
	if len(args) > 0 && args[0] == "list" {
		args = args[1:]
	}
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}

	service, store, err := e.openService(ctx, true)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	query := storage.EventQuery{ProjectID: *projectID, Limit: *limit, Newest: *newest}
	for _, name := range splitList(*typeFilter) {
		query.Types = append(query.Types, events.Type(name))
	}
	if *taskAlias != "" {
		taskID, err := service.ResolveTaskID(ctx, *projectID, *taskAlias)
		if err != nil {
			return err
		}
		query.TaskID = taskID
	}
	stream, err := service.Events(ctx, query)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(e.stdout, stream)
	}
	for _, event := range stream {
		fmt.Fprintf(e.stdout, "%6d %-24s %-32s %s/%s\n",
			event.Seq, event.OccurredAt, event.EventType, event.Actor.Kind, event.Actor.ID)
	}
	return nil
}

// readPayload resolves the -payload argument.
//
// Only three forms are accepted and none of them involves a shell: inline
// JSON, an explicit @file, or stdin. docs/SECURITY.md §5 rules out shell
// interpolation as an input mechanism.
func readPayload(arg string, stdin io.Reader) ([]byte, error) {
	switch {
	case arg == "-":
		data, err := io.ReadAll(io.LimitReader(stdin, maxPayloadBytes+1))
		if err != nil {
			return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "read payload from stdin")
		}
		return checkPayloadSize(data)
	case strings.HasPrefix(arg, "@"):
		data, err := os.ReadFile(arg[1:])
		if err != nil {
			return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "read payload file %s", arg[1:])
		}
		return checkPayloadSize(data)
	default:
		return checkPayloadSize([]byte(arg))
	}
}

func checkPayloadSize(data []byte) ([]byte, error) {
	if len(data) > maxPayloadBytes {
		return nil, errs.New(errs.CategoryInvalidArgument,
			"payload exceeds the %d byte limit; events carry transition facts, not artifacts", maxPayloadBytes)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, errs.New(errs.CategoryInvalidArgument, "payload is empty")
	}
	return data, nil
}

// withTaskID fills task_id into payload JSON when the operator supplied an
// alias instead.
//
// This transformation acts on command-line input before it becomes a typed
// payload, never on a durable record. The result still goes through strict
// typed decoding, so nothing untyped reaches the journal.
func withTaskID(raw []byte, taskID string) ([]byte, error) {
	var generic map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&generic); err != nil {
		return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "payload must be a JSON object")
	}
	if existing, ok := generic["task_id"]; ok && existing != "" {
		return raw, nil
	}
	generic["task_id"] = taskID
	out, err := json.Marshal(generic)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "rewrite payload with task id")
	}
	return out, nil
}
