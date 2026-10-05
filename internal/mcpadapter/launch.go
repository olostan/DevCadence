package mcpadapter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/principal/facade"
	"github.com/olostan/DevCadence/internal/repository"
	"github.com/olostan/DevCadence/internal/storage"
)

// Environment variable names of the no-argument launch.
const (
	EnvProjectID = "DEVCADENCE_PROJECT_ID"
	EnvHome      = "DEVCADENCE_HOME"
	EnvBinding   = "DEVCADENCE_PRINCIPAL_BINDING"
)

// Exit codes of Launch.
const (
	ExitOK     = 0
	ExitFailed = 1
	ExitUsage  = 2
)

// Launch runs the no-argument stdio server. Unexpected arguments exit 2 before
// anything is opened. Configuration, the database and the protected binding
// come from the environment and DEVCADENCE_HOME, never from the working
// directory, so the source repository need not be the process cwd (I5).
// Nothing is written to stdout except protocol frames: diagnostics go to stderr.
func Launch(ctx context.Context, args []string, getenv func(string) string, transport mcp.Transport, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "usage: devcadence-mcp (takes no arguments; set DEVCADENCE_PROJECT_ID and DEVCADENCE_HOME)")
		return ExitUsage
	}
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(ctx, getenv, transport, logger); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(stderr, "devcadence-mcp:", safeMessage(err))
		return ExitFailed
	}
	return ExitOK
}

// safeMessage keeps launch diagnostics free of environment values: it prints
// the error category and the message the code itself authored.
func safeMessage(err error) string {
	var typed *errs.Error
	if errors.As(err, &typed) {
		return string(typed.Category) + ": " + typed.Message
	}
	return "launch failed"
}

func run(ctx context.Context, getenv func(string) string, transport mcp.Transport, logger *slog.Logger) error {
	project := getenv(EnvProjectID)
	if project == "" {
		return errs.New(errs.CategoryInvalidArgument, "%s is required", EnvProjectID)
	}
	home := getenv(EnvHome)
	if home == "" || !filepath.IsAbs(home) {
		return errs.New(errs.CategoryInvalidArgument, "%s must be an absolute path", EnvHome)
	}
	bindingPath := getenv(EnvBinding)
	if bindingPath == "" {
		bindingPath = filepath.Join(home, "config", "principal-binding.json")
	} else if !filepath.IsAbs(bindingPath) {
		return errs.New(errs.CategoryInvalidArgument, "%s must be an absolute path", EnvBinding)
	}
	binding, err := LoadBinding(bindingPath)
	if err != nil {
		return err
	}
	if binding.Caller.ProjectID != project {
		return errs.New(errs.CategoryPolicyDenied, "the binding is for a different project than %s", EnvProjectID)
	}

	dbPath := filepath.Join(home, "state", "control-plane.db")
	if _, err := os.Stat(dbPath); err != nil {
		return errs.New(errs.CategoryNotFound, "the control-plane database does not exist under %s", EnvHome)
	}
	store, err := storage.Open(ctx, storage.Config{Path: dbPath, Clock: clock.System()})
	if err != nil {
		return err
	}
	defer store.Close()
	cp, err := controlplane.New(controlplane.Options{Store: store, Logger: logger})
	if err != nil {
		return err
	}
	observer := repositoryObserver(ctx, cp, project, logger)

	ops, err := facade.NewOperationRegistry("")
	if err != nil {
		return err
	}
	defer ops.Close()
	svc, err := facade.NewService(facade.Options{
		ControlPlane: cp, Policy: NewBindingPolicy(binding), Operations: ops, Repository: observer, Logger: logger,
	})
	if err != nil {
		return err
	}
	server, err := NewServer(Config{Service: svc, Caller: binding.Caller, Logger: logger})
	if err != nil {
		return err
	}
	logger.Info("devcadence-mcp ready", slog.String("instance_id", ops.InstanceID()))
	return server.Run(ctx, transport)
}

// repositoryObserver registers the project's repository for drift detection.
// A project with none (Day-0) has no observer. A registered repository that can
// no longer be opened yields an observer that refuses every check, so staleness
// can never be silently skipped while connectivity still works.
func repositoryObserver(ctx context.Context, cp *controlplane.Service, project string, logger *slog.Logger) facade.RepositoryObserver {
	list, err := cp.Events(ctx, storage.EventQuery{
		ProjectID: project, Types: []events.Type{events.TypeProjectInitialized}, Limit: 1,
	})
	if err != nil {
		// Unknown whether a repository is registered: never skip drift detection.
		logger.Warn("project repository registration cannot be read; repository-dependent calls will be refused")
		return refusingObserver{err: err}
	}
	if len(list) == 0 {
		return nil
	}
	init, ok := list[0].Payload.(*events.ProjectInitialized)
	if !ok || init.RepositoryPath == "" {
		return nil
	}
	repo, err := repository.Register(ctx, project, init.RepositoryPath, repository.Options{})
	if err != nil {
		logger.Warn("registered repository cannot be opened; repository-dependent calls will be refused")
		return refusingObserver{err: err}
	}
	observer, err := facade.NewGitObserver(repo)
	if err != nil {
		return refusingObserver{err: err}
	}
	return observer
}

type refusingObserver struct{ err error }

func (r refusingObserver) Drift(context.Context, string, []string) (facade.Drift, error) {
	return facade.Drift{}, r.err
}
