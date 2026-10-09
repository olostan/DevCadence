package main

import (
	"context"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/mcpadapter"
	"github.com/olostan/DevCadence/internal/principal/facade"
	"github.com/olostan/DevCadence/internal/selfhost"
	"path/filepath"
)

// selfhostTasks is the composition root for SH1-1: when the user-level
// self-host configuration (DEVCADENCE_HOME config file / DEVCADENCE_OLLAMA_*)
// is present it composes the native task executor with the loopback Ollama
// provider. With no configuration it returns a nil executor and nothing changes.
func selfhostTasks(ctx context.Context, in mcpadapter.TaskPortInput) (facade.TaskExecutor, func() error, error) {
	cfg, err := selfhost.LoadConfig(in.Home, in.Getenv)
	if err != nil {
		return nil, nil, err
	}
	if cfg == nil {
		return nil, nil, nil
	}
	if in.RepoPath == "" {
		return nil, nil, errs.New(errs.CategoryNotFound, "selfhost is configured but the project has no registered repository")
	}
	built, err := selfhost.Build(ctx, *cfg, selfhost.Deps{
		ProjectID: in.ProjectID, RepoPath: in.RepoPath, StateDir: filepath.Join(in.Home, "state", "selfhost"),
		ControlPlane: in.ControlPlane, Registry: in.Operations, Logger: in.Logger,
	})
	if err != nil {
		return nil, nil, err
	}
	return built.Executor, built.Close, nil
}
