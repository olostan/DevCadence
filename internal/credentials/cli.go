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
// specs are built from their own fixed configuration (Handle/ExecutablePath/Args),
// never from the caller-supplied ref, so this should never actually fire
// in production — it exists as defense in depth against a future adapter
// (or a misconfigured one) that builds a spec from untrusted input.
func runGuarded(ctx context.Context, runner CommandRunner, spec process.Spec) (process.Result, error) {
	if err := ValidateProcessSpecNoSecrets(spec); err != nil {
		return process.Result{}, err
	}
	return runner.Run(ctx, spec)
}

// resolveExecSpec picks what actually gets executed and with what
// environment, separating the *logical* CLI/session identity (Handle —
// what CredentialRef.Locator names, and what Handles() matches; it must
// stay within cli_session's opaque-handle contract, which forbids `/`)
// from the *executable* identity used to actually start a process
// (ExecutablePath — a bare name resolved via the given Env's PATH, or a
// discovered/verified absolute path; independent-review follow-up on
// WP-M3B-4, finding 1).
//
// When ExecutablePath is empty, the bare Handle is used and must resolve
// via PATH. When Env is nil, process.BaseEnv() supplies a minimal PATH so
// a bare executable name actually resolves instead of failing closed with
// "no PATH in env" the moment a real process.Runner is used instead of a
// test fake.
func resolveExecSpec(handle, executablePath string, env []string) (string, []string) {
	executable := executablePath
	if executable == "" {
		executable = handle
	}
	if env == nil {
		env = process.BaseEnv()
	}
	return executable, env
}

// versionOrHelpOnlyArgs reports whether args is exactly one argument that
// conventionally means "print version or help text and exit" across common
// CLI conventions. It exists so BoundedCLIAuthAdapter can refuse to ever
// report AuthStatusAuthenticated from such an invocation, regardless of how
// the adapter's ProbeArgs happened to be configured (independent-review
// follow-up on WP-M3B-4, finding 3): the "this command is an authoritative
// auth check" property must hold at the moment evidence is produced, not
// merely be assumed of whoever constructed the adapter.
func versionOrHelpOnlyArgs(args []string) bool {
	if len(args) != 1 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "--version", "-version", "-v", "version",
		"--help", "-help", "-h", "help":
		return true
	}
	return false
}

// VersionOnlyAdapter handles CLIs where only installation/version checking is known.
// Per ADR-0014 §6, running --version can NEVER produce status "authenticated".
type VersionOnlyAdapter struct {
	ID string
	// Handle is the logical CLI identifier matched against
	// CredentialRef.Locator by Handles(). It is an opaque handle, never a
	// filesystem path (cli_session locators forbid `/`).
	Handle string
	// ExecutablePath is what is actually started: a bare name resolved via
	// Env's PATH, or a discovered/verified absolute path. Defaults to
	// Handle when empty.
	ExecutablePath string
	Arg            string
	// Env is the process environment used for execution. Defaults to
	// process.BaseEnv() when nil, so a bare ExecutablePath/Handle resolves
	// via PATH against the real process.Runner instead of failing closed.
	Env []string
}

func (a *VersionOnlyAdapter) AdapterID() string { return a.ID }

func (a *VersionOnlyAdapter) Handles(locator string) bool {
	return locator == a.Handle || strings.HasPrefix(locator, a.Handle+":")
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
		ProbeTarget:   a.Handle,
		AdapterID:     a.ID,
	}

	if runner == nil {
		evidence.Status = protocol.AuthStatusUnavailable
		evidence.Detail = "command runner not available"
		return evidence
	}

	executable, env := resolveExecSpec(a.Handle, a.ExecutablePath, a.Env)
	spec := process.Spec{
		Executable: executable,
		Args:       []string{arg},
		Dir:        "/", // Safe probe directory
		Env:        env,
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
	ID string
	// Handle is the logical CLI identifier matched against
	// CredentialRef.Locator by Handles(). See VersionOnlyAdapter.Handle.
	Handle string
	// ExecutablePath is what is actually started. See
	// VersionOnlyAdapter.ExecutablePath.
	ExecutablePath      string
	ProbeArgs           []string
	UnauthenticatedMsgs []string
	// Env is the process environment used for execution. See
	// VersionOnlyAdapter.Env.
	Env []string
}

func (a *BoundedCLIAuthAdapter) AdapterID() string { return a.ID }

func (a *BoundedCLIAuthAdapter) Handles(locator string) bool {
	return locator == a.Handle || strings.HasPrefix(locator, a.Handle+":")
}

func (a *BoundedCLIAuthAdapter) ProbeAuth(ctx context.Context, runner CommandRunner, clk clock.Clock, ref protocol.CredentialRef) protocol.AuthEvidence {
	now := protocol.NewTimestamp(clk.Now())

	// A ProbeArgs shape that is itself a version/help invocation can never
	// be an authoritative auth check, no matter what exit code it returns.
	// This is enforced here, at the moment evidence would be produced,
	// rather than trusted of whoever configured ProbeArgs (finding 3).
	versionOnly := versionOrHelpOnlyArgs(a.ProbeArgs)
	probeKind := protocol.AuthProbeCLIAuthCall
	if versionOnly {
		probeKind = protocol.AuthProbeCLIVersionOnly
	}

	evidence := protocol.AuthEvidence{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         ref.RefID,
		Kind:          ref.Kind,
		ProbeKind:     probeKind,
		ObservedAt:    now,
		ProbeTarget:   a.Handle,
		AdapterID:     a.ID,
	}

	if runner == nil {
		evidence.Status = protocol.AuthStatusUnavailable
		evidence.Detail = "command runner not available"
		return evidence
	}

	executable, env := resolveExecSpec(a.Handle, a.ExecutablePath, a.Env)
	spec := process.Spec{
		Executable: executable,
		Args:       a.ProbeArgs,
		Dir:        "/",
		Env:        env,
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

	if versionOnly {
		// ProbeArgs resolved to a version/help invocation: exit 0 proves
		// installation, never authentication (ADR-0014 §6), the same
		// invariant VersionOnlyAdapter enforces structurally.
		evidence.Status = protocol.AuthStatusIndeterminate
		evidence.Detail = "probe_args resolve to a version/help invocation; software is installed, but version output cannot establish authentication"
		return evidence
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
