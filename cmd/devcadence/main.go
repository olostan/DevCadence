// Command devcadence is the DevCadence control-plane CLI.
//
// In M1 it is an inspection and debugging surface over the application
// services: it initialises a project, creates tasks, appends typed
// engineering events, and shows canonical state, journal history and task
// detail. It contains no domain logic — docs/ARCHITECTURE.md §3 keeps
// orchestration out of adapters — and it requires no model runtime.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/olostan/DevCadence/internal/errs"
)

// version is the build identity. It is overridden at release time with
// -ldflags "-X main.version=..."; "dev" is the honest answer otherwise.
var version = "dev"

// ExitCoder is implemented by errors that specify an explicit process exit status.
type ExitCoder interface {
	ExitCode() int
}

// Standard exit codes distinguishing operational outcomes per WP-M3B-7.
const (
	ExitCodeSuccess         = 0
	ExitCodeExecutionError  = 1
	ExitCodeInvalidArgument = 2
	ExitCodeNotFound        = 3
	ExitCodeDrift           = 4
	ExitCodeIntegrity       = 5
	ExitCodePlanGenerated   = 6
)

func main() {
	// Cancelling on SIGINT/SIGTERM propagates through every context-aware
	// operation, which is what ENGINEERING_STANDARDS.md §7 asks of anything
	// that can block.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		// Help was printed by the flag package; there is nothing to report.
		if errors.Is(err, errFlagHelp) {
			return
		}
		code := exitCode(err)
		if code != ExitCodePlanGenerated {
			fmt.Fprintln(os.Stderr, "devcadence: "+err.Error())
		}
		os.Exit(code)
	}
}

// exitCode maps an error category or ExitCoder to a process exit status so that scripts
// can react without parsing messages.
func exitCode(err error) int {
	if errors.Is(err, errFlagHelp) {
		return ExitCodeSuccess
	}
	var coder ExitCoder
	if errors.As(err, &coder) {
		return coder.ExitCode()
	}
	switch errs.CategoryOf(err) {
	case errs.CategoryInvalidArgument:
		return ExitCodeInvalidArgument
	case errs.CategoryNotFound:
		return ExitCodeNotFound
	case errs.CategoryInvalidTransition, errs.CategoryConflict:
		return ExitCodeDrift
	case errs.CategoryIntegrity, errs.CategorySchemaVersionUnsupported:
		return ExitCodeIntegrity
	default:
		return ExitCodeExecutionError
	}
}
