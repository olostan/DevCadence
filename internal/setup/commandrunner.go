package setup

import (
	"context"

	"github.com/olostan/DevCadence/internal/process"
)

// CommandRunner is the narrow surface through which every operation
// applier and condition evaluator in this package invokes an external
// process (ADR-0014 §7: "subprocess execution routes exclusively through
// internal/process.Runner"). *process.Runner already satisfies this
// interface exactly, so no adapter type is needed — the interface exists
// only to make the dependency swappable in tests.
type CommandRunner interface {
	Run(ctx context.Context, spec process.Spec) (process.Result, error)
}
