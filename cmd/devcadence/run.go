package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/observability"
	"github.com/olostan/DevCadence/internal/storage"
)

// errFlagHelp signals that help was requested and nothing went wrong.
var errFlagHelp = errors.New("help requested")

// globals are the options every subcommand shares.
type globals struct {
	dbPath   string
	logLevel string
	logJSON  bool
}

// env is the environment a subcommand runs in. Passing writers and readers explicitly
// rather than using os.Stdout/os.Stdin directly is what makes the CLI testable.
type env struct {
	globals    globals
	stdout     io.Writer
	stderr     io.Writer
	stdin      io.Reader
	isTerminal func() bool
}

// homeDir resolves the root DevCadence directory for state, caches, and artifacts.
func (e *env) homeDir() string {
	if h := os.Getenv("DEVCADENCE_HOME"); h != "" {
		return h
	}
	if e.globals.dbPath != "" && e.globals.dbPath != storage.MemoryPath {
		dir := filepath.Dir(e.globals.dbPath)
		if filepath.Base(dir) == "state" {
			return filepath.Dir(dir)
		}
		return dir
	}
	userHome, err := os.UserHomeDir()
	if err == nil {
		return filepath.Join(userHome, ".devcadence")
	}
	return "."
}

// command is one CLI verb.
type command struct {
	name    string
	summary string
	run     func(ctx context.Context, e *env, args []string) error
}

func commands() []command {
	return []command{
		{"version", "print the build version", runVersion},
		{"project", "initialise and list projects", runProject},
		{"task", "create, list and inspect tasks", runTask},
		{"event", "append a typed engineering event", runEvent},
		{"events", "list journal entries", runEvents},
		{"state", "show or rebuild canonical ProjectState", runState},
		{"schema", "validate JSON documents against the published schemas", runSchema},
		{"migrate", "report control-plane schema migrations", runMigrate},
		{"repo", "register and inspect a Git repository", runRepo},
		{"worktree", "create, list, cleanup and recover isolated worktrees", runWorktree},
		{"run", "run one controlled command in a working directory", runRun},
		{"validate", "execute a validation profile and record its ValidationResult", runValidate},
		{"candidate", "show deterministic candidate/diff/integration metadata", runCandidate},
		{"environment", "inspect observed hardware, software and backend candidates", runEnvironment},
		{"cognition", "list, probe and route discovered cognition endpoints", runCognition},
		{"doctor", "diagnose environment readiness, inspect inventory, and plan remediation", runDoctor},
		{"setup", "plan and apply bounded remediation recipes", runSetup},
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return runWithEnv(ctx, args, stdout, stderr, os.Stdin, nil)
}

func runWithEnv(ctx context.Context, args []string, stdout, stderr io.Writer, stdin io.Reader, isTerminal func() bool) error {
	fs := flag.NewFlagSet("devcadence", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var g globals
	fs.StringVar(&g.dbPath, "db", "", "control-plane database path (default $DEVCADENCE_HOME/state/control-plane.db)")
	fs.StringVar(&g.logLevel, "log-level", "warn", "log level: debug, info, warn, error")
	fs.BoolVar(&g.logJSON, "log-json", false, "emit structured logs as JSON")
	fs.Usage = func() { usage(stderr, fs) }
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return errFlagHelp
		}
		return errs.Wrap(errs.CategoryInvalidArgument, err, "parse global flags")
	}
	rest := fs.Args()
	if len(rest) == 0 {
		usage(stderr, fs)
		return errs.New(errs.CategoryInvalidArgument, "a subcommand is required")
	}
	if g.dbPath == "" {
		resolved, err := defaultDatabasePath()
		if err != nil {
			return err
		}
		g.dbPath = resolved
	}

	if stdin == nil {
		stdin = os.Stdin
	}
	if isTerminal == nil {
		isTerminal = func() bool {
			if f, ok := stdin.(*os.File); ok {
				stat, err := f.Stat()
				if err == nil {
					return (stat.Mode() & os.ModeCharDevice) != 0
				}
			}
			return false
		}
	}

	e := &env{
		globals:    g,
		stdout:     stdout,
		stderr:     stderr,
		stdin:      stdin,
		isTerminal: isTerminal,
	}
	for _, c := range commands() {
		if c.name == rest[0] {
			return c.run(ctx, e, rest[1:])
		}
	}
	usage(stderr, fs)
	return errs.New(errs.CategoryInvalidArgument, "unknown subcommand %q", rest[0])
}

func usage(w io.Writer, fs *flag.FlagSet) {
	fmt.Fprintln(w, "usage: devcadence [global flags] <command> [arguments]")
	fmt.Fprintln(w, "\nCommands:")
	for _, c := range commands() {
		fmt.Fprintf(w, "  %-10s %s\n", c.name, c.summary)
	}
	fmt.Fprintln(w, "\nGlobal flags:")
	fs.PrintDefaults()
}

// defaultDatabasePath resolves $DEVCADENCE_HOME/state/control-plane.db.
//
// docs/SETUP.md §8 keeps runtime state out of target project source trees by
// default, so the database lives under the user's DevCadence home rather
// than the working directory.
func defaultDatabasePath() (string, error) {
	home := os.Getenv("DEVCADENCE_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", errs.Wrap(errs.CategoryInvalidArgument, err,
				"cannot determine home directory; pass -db explicitly")
		}
		home = filepath.Join(userHome, ".devcadence")
	}
	if !filepath.IsAbs(home) {
		return "", errs.New(errs.CategoryInvalidArgument,
			"DEVCADENCE_HOME must be an absolute path, got %q", home)
	}
	return filepath.Join(home, "state", "control-plane.db"), nil
}

// openService opens the store and builds the application service.
//
// readOnly commands refuse to create or migrate a database, so that a typo in
// -db reports a missing project instead of silently creating an empty one.
func (e *env) openService(ctx context.Context, readOnly bool) (*controlplane.Service, *storage.Store, error) {
	if !readOnly && e.globals.dbPath != storage.MemoryPath {
		if err := os.MkdirAll(filepath.Dir(e.globals.dbPath), 0o700); err != nil {
			return nil, nil, errs.Wrap(errs.CategoryInvalidArgument, err,
				"create state directory for %s", e.globals.dbPath)
		}
	}
	store, err := storage.Open(ctx, storage.Config{
		Path:     e.globals.dbPath,
		Clock:    clock.System(),
		ReadOnly: readOnly,
	})
	if err != nil {
		return nil, nil, err
	}
	format := observability.FormatText
	if e.globals.logJSON {
		format = observability.FormatJSON
	}
	logger := observability.NewLogger(observability.Options{
		Level:  observability.ParseLevel(e.globals.logLevel),
		Format: format,
		Writer: e.stderr,
	})
	service, err := controlplane.New(controlplane.Options{
		Store:  store,
		Clock:  clock.System(),
		IDs:    ids.NewULIDSource(),
		Logger: logger,
	})
	if err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	return service, store, nil
}

// parseFlags parses a subcommand's flag set with consistent help handling.
func parseFlags(fs *flag.FlagSet, e *env, args []string) error {
	fs.SetOutput(e.stderr)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return errFlagHelp
		}
		return errs.Wrap(errs.CategoryInvalidArgument, err, "parse flags for %s", fs.Name())
	}
	return nil
}

// splitList parses a comma-separated flag value into a trimmed list.
func splitList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func runVersion(_ context.Context, e *env, _ []string) error {
	fmt.Fprintf(e.stdout, "devcadence %s\n", version)
	return nil
}
