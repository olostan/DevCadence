// Command devcadience is the DevCadience control-plane CLI.
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

	"github.com/olostan/DevCadience/internal/errs"
)

// version is the build identity. It is overridden at release time with
// -ldflags "-X main.version=..."; "dev" is the honest answer otherwise.
var version = "dev"

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
		fmt.Fprintln(os.Stderr, "devcadience: "+err.Error())
		os.Exit(exitCode(err))
	}
}

// exitCode maps an error category to a process exit status so that scripts
// can react without parsing messages.
func exitCode(err error) int {
	if errors.Is(err, errFlagHelp) {
		return 0
	}
	switch errs.CategoryOf(err) {
	case errs.CategoryInvalidArgument:
		return 2
	case errs.CategoryNotFound:
		return 3
	case errs.CategoryInvalidTransition, errs.CategoryConflict:
		return 4
	case errs.CategoryIntegrity, errs.CategorySchemaVersionUnsupported:
		return 5
	default:
		return 1
	}
}
