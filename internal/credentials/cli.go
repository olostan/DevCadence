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
		RefID:       ref.RefID,
		Kind:        ref.Kind,
		ProbeKind:   protocol.AuthProbeCLIVersionOnly,
		ObservedAt:  now,
		ProbeTarget: a.Executable,
		AdapterID:   a.ID,
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

	res, err := runner.Run(ctx, spec)
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
		RefID:       ref.RefID,
		Kind:        ref.Kind,
		ProbeKind:   protocol.AuthProbeCLIAuthCall,
		ObservedAt:  now,
		ProbeTarget: a.Executable,
		AdapterID:   a.ID,
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

	res, err := runner.Run(ctx, spec)
	if err != nil || res.Status == process.StatusTimeout || res.Status == process.StatusCancelled {
		evidence.Status = protocol.AuthStatusIndeterminate
		evidence.Detail = "auth probe timed out or failed to execute"
		return evidence
	}

	// Treat output as untrusted and potentially secret-bearing.
	// Only inspect for known unauthenticated substrings, and discard output immediately.
	output := strings.ToLower(string(res.Stdout) + " " + string(res.Stderr))

	if !res.Success() {
		// Non-zero exit code
		for _, msg := range a.UnauthenticatedMsgs {
			if strings.Contains(output, strings.ToLower(msg)) {
				evidence.Status = protocol.AuthStatusUnauthenticated
				evidence.Detail = "CLI reports unauthenticated session"
				return evidence
			}
		}
		// If exit code is not 0 and no specific unauthenticated message matched:
		evidence.Status = protocol.AuthStatusUnauthenticated
		evidence.Detail = "CLI auth check returned failure exit code"
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
		RefID:       ref.RefID,
		Kind:        ref.Kind,
		Status:      s.Status,
		ProbeKind:   kind,
		ObservedAt:  protocol.NewTimestamp(clk.Now()),
		ProbeTarget: s.Target,
		AdapterID:   s.ID,
		Detail:      detail,
	}
}
