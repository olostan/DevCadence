package taskexec

import (
	"context"
	"sync"

	"github.com/olostan/DevCadence/internal/principal/facade"
)

// Ensure Executor implements facade.TaskExecutor.
var _ facade.TaskExecutor = (*Executor)(nil)

// Executor coordinates bounded task execution and recovery.
type Executor struct {
	opts       Options
	valMu      sync.Mutex
	valIndices map[string]int
	// audits maps a running attemptID to its run_command audit so failAttempt
	// can attach the command trace on every failure path.
	audits sync.Map
	// postChecks maps a running attemptID to its post-check state so failAttempt
	// can attach the validation report on every failure path.
	postChecks sync.Map
}

// New constructs and initializes a new Executor.
// It validates required options and runs orphan recovery before returning.
func New(opts Options) (*Executor, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	e := &Executor{
		opts:       opts,
		valIndices: make(map[string]int),
	}
	if err := e.Recover(context.Background()); err != nil {
		return nil, err
	}
	return e, nil
}
