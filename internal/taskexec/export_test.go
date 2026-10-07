package taskexec

import (
	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/tools"
)

// SetupWorkerToolsForTesting exports setupWorkerTools for unit testing.
func SetupWorkerToolsForTesting(mediator *drivers.ScopedToolMediator, scope *tools.Scope, runner *process.Runner, writeScope []string) []drivers.ToolDefinition {
	return setupWorkerTools(mediator, scope, runner, writeScope)
}
