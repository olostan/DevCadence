package credentials

import (
	"context"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
)

// CommandRunner matches the process execution boundary (process.Runner).
type CommandRunner interface {
	Run(ctx context.Context, spec process.Spec) (process.Result, error)
}

// CLISessionAuthAdapter is the provider-neutral extension point for CLI authentication probing.
type CLISessionAuthAdapter interface {
	AdapterID() string
	Handles(locator string) bool
	ProbeAuth(ctx context.Context, runner CommandRunner, clk clock.Clock, ref protocol.CredentialRef) protocol.AuthEvidence
}

// runGuarded is the sole path either adapter in this file uses to execute a
// process.Spec: it enforces ValidateProcessSpecNoSecrets before the
// runner ever sees the spec, so "no secret in argv/env" is a real
// execution-time gate for every WP4 credential/auth probe this package
// runs, not an opt-in helper a caller could forget to call. Both adapters'
// specs are built from their own fixed configuration (Executable/Args),
// never from the caller-supplied ref, so this should never actually fire
// in production — it exists as defense in depth against a future adapter
// (or a misconfigured one) that builds a spec from untrusted input.
func runGuarded(ctx context.Context, runner CommandRunner, spec process.Spec) (process.Result, error) {
	if err := ValidateProcessSpecNoSecrets(spec); err != nil {
		return process.Result{}, err
	}
	return runner.Run(ctx, spec)
}

// VersionOnlyAdapter handles CLIs where only installation/version checking is known.
// Per ADR-0014 §6, running --version can NEVER produce status "authenticated".
type VersionOnlyAdapter struct {
	ID         string
	Executable string
	Arg        string
}

func (a *VersionOnlyAdapter) AdapterID() string { return a.ID }

func (a *VersionOnlyAdapter) Handles(locator string) bool {
	return locator == a.Executable || strings.HasPrefix(locator, a.Executable+":")
}

func (a *VersionOnlyAdapter) ProbeAuth(ctx context.Context, runner CommandRunner, clk clock.Clock, ref protocol.CredentialRef) protocol.AuthEvidence {
	now := protocol.NewTimestamp(clk.Now())
	arg := a.Arg
	if arg == "" {
		arg = "--version"
	}

	evidence := protocol.AuthEvidence{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         ref.RefID,
		Kind:          ref.Kind,
		ProbeKind:     protocol.AuthProbeCLIVersionOnly,
		ObservedAt:    now,
		ProbeTarget:   a.Executable,
		AdapterID:     a.ID,
	}

	if runner == nil {
		evidence.Status = protocol.AuthStatusUnavailable
		evidence.Detail = "command runner not available"
		return evidence
	}

	spec := process.Spec{
		Executable: a.Executable,
		Args:       []string{arg},
		Dir:        "/", // Safe probe directory
		Timeout:    5 * time.Second,
	}

	res, err := runGuarded(ctx, runner, spec)
	if err != nil || res.Status == process.StatusTimeout || res.Status == process.StatusCancelled {
		evidence.Status = protocol.AuthStatusUnavailable
		evidence.Detail = "CLI executable could not be executed"
		return evidence
	}

	if !res.Success() {
		evidence.Status = protocol.AuthStatusUnavailable
		evidence.Detail = "CLI version check failed"
		return evidence
	}

	// Succeeded: software is installed, but version alone CANNOT establish authentication!
	evidence.Status = protocol.AuthStatusIndeterminate
	evidence.Detail = "software is installed; version output proves installation only, not authentication"
	return evidence
}

// BoundedCLIAuthAdapter probes CLI auth using a non-inference command.
// Raw stdout and stderr are NEVER retained in durable records or error messages.
type BoundedCLIAuthAdapter struct {
	ID                  string
	Executable          string
	ProbeArgs           []string
	UnauthenticatedMsgs []string
}

func (a *BoundedCLIAuthAdapter) AdapterID() string { return a.ID }

func (a *BoundedCLIAuthAdapter) Handles(locator string) bool {
	return locator == a.Executable || strings.HasPrefix(locator, a.Executable+":")
}

func (a *BoundedCLIAuthAdapter) ProbeAuth(ctx context.Context, runner CommandRunner, clk clock.Clock, ref protocol.CredentialRef) protocol.AuthEvidence {
	now := protocol.NewTimestamp(clk.Now())
	evidence := protocol.AuthEvidence{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         ref.RefID,
		Kind:          ref.Kind,
		ProbeKind:     protocol.AuthProbeCLIAuthCall,
		ObservedAt:    now,
		ProbeTarget:   a.Executable,
		AdapterID:     a.ID,
	}

	if runner == nil {
		evidence.Status = protocol.AuthStatusUnavailable
		evidence.Detail = "command runner not available"
		return evidence
	}

	spec := process.Spec{
		Executable: a.Executable,
		Args:       a.ProbeArgs,
		Dir:        "/",
		Timeout:    10 * time.Second,
	}

	res, err := runGuarded(ctx, runner, spec)
	if err != nil || res.Status == process.StatusTimeout || res.Status == process.StatusCancelled {
		evidence.Status = protocol.AuthStatusIndeterminate
		evidence.Detail = "auth probe timed out or failed to execute"
		return evidence
	}

	// Treat output as untrusted and potentially secret-bearing.
	// Only inspect for known unauthenticated substrings, and discard output immediately.
	output := strings.ToLower(string(res.Stdout) + " " + string(res.Stderr))

	if !res.Success() {
		// A nonzero exit code recognized (by an explicit, provider-specific
		// message match) as this CLI's own "not logged in" signal is real
		// unauthenticated evidence.
		for _, msg := range a.UnauthenticatedMsgs {
			if strings.Contains(output, strings.ToLower(msg)) {
				evidence.Status = protocol.AuthStatusUnauthenticated
				evidence.Detail = "CLI reports unauthenticated session"
				return evidence
			}
		}
		// An unrecognized nonzero exit is NOT evidence of being
		// unauthenticated — it can just as easily mean the executable
		// failed to run correctly, an incompatible CLI version, a local
		// configuration error, a provider outage, or a permission
		// failure. Only a provider-specific authoritative signal
		// (a matched UnauthenticatedMsgs entry, above) may report
		// unauthenticated; anything else is indeterminate.
		evidence.Status = protocol.AuthStatusIndeterminate
		evidence.Detail = "CLI auth check returned a failure exit code not recognized as an unauthenticated signal"
		return evidence
	}

	// Exit code 0: check if output explicitly reports unauthenticated despite exit 0
	for _, msg := range a.UnauthenticatedMsgs {
		if strings.Contains(output, strings.ToLower(msg)) {
			evidence.Status = protocol.AuthStatusUnauthenticated
			evidence.Detail = "CLI reports unauthenticated session"
			return evidence
		}
	}

	evidence.Status = protocol.AuthStatusAuthenticated
	evidence.Detail = "session verified via CLI auth probe"
	return evidence
}

// StaticCLIAuthAdapter is a deterministic test adapter.
type StaticCLIAuthAdapter struct {
	ID        string
	Target    string
	Status    protocol.AuthEvidenceStatus
	ProbeKind protocol.AuthProbeKind
	Detail    string
}

func (s *StaticCLIAuthAdapter) AdapterID() string { return s.ID }
func (s *StaticCLIAuthAdapter) Handles(locator string) bool {
	return s.Target == "" || s.Target == locator || strings.HasPrefix(locator, s.Target+":")
}
func (s *StaticCLIAuthAdapter) ProbeAuth(_ context.Context, _ CommandRunner, clk clock.Clock, ref protocol.CredentialRef) protocol.AuthEvidence {
	kind := s.ProbeKind
	if kind == "" {
		kind = protocol.AuthProbeCLIAuthCall
	}
	detail := s.Detail
	if detail == "" {
		detail = "static test probe result"
	}
	return protocol.AuthEvidence{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         ref.RefID,
		Kind:          ref.Kind,
		Status:        s.Status,
		ProbeKind:     kind,
		ObservedAt:    protocol.NewTimestamp(clk.Now()),
		ProbeTarget:   s.Target,
		AdapterID:     s.ID,
		Detail:        detail,
	}
}
