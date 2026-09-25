package credentials

import (
	"context"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// Options configures the credential Manager.
type Options struct {
	Clock       clock.Clock
	Runner      CommandRunner
	Env         EnvReader
	Keychain    KeychainChecker
	CLIAdapters []CLISessionAuthAdapter
}

// Manager coordinates opaque credential reference inspection and authentication evidence generation.
// Invariant: DevCadence holds no secret custody (ADR-0014 §6, DCI-081).
type Manager struct {
	clock       clock.Clock
	runner      CommandRunner
	env         EnvReader
	keychain    KeychainChecker
	cliAdapters []CLISessionAuthAdapter
}

// NewManager creates a credential Manager.
func NewManager(opts Options) (*Manager, error) {
	clk := opts.Clock
	if clk == nil {
		clk = clock.System()
	}
	env := opts.Env
	if env == nil {
		env = OsEnvReader{}
	}
	keychain := opts.Keychain
	if keychain == nil {
		keychain = UnsupportedKeychainChecker{}
	}

	return &Manager{
		clock:       clk,
		runner:      opts.Runner,
		env:         env,
		keychain:    keychain,
		cliAdapters: append([]CLISessionAuthAdapter(nil), opts.CLIAdapters...),
	}, nil
}

// CheckCredential inspects the authorization source referenced by ref and produces
// structured AuthEvidence. It NEVER reads, logs, or stores raw secrets.
//
// env_presence and keychain_presence probes can only ever prove that a
// value exists somewhere — never that it is valid, accepted by the
// provider, unexpired, or bound to a usable session. Presence therefore
// maps to AuthStatusIndeterminate, never AuthStatusAuthenticated (the same
// "installed is not authenticated" distinction AuthProbeCLIVersionOnly
// already draws, now enforced structurally by AuthEvidence.Validate too).
// Absence maps to AuthStatusUnauthenticated: there is no credential for
// the reference to resolve to at all, which is itself a real (if weak)
// negative signal, unlike mere presence being promoted to a positive one.
func (m *Manager) CheckCredential(ctx context.Context, ref protocol.CredentialRef) (protocol.AuthEvidence, error) {
	if err := ref.Validate(); err != nil {
		return protocol.AuthEvidence{}, err
	}

	now := protocol.NewTimestamp(m.clock.Now())
	var evidence protocol.AuthEvidence

	switch ref.Kind {
	case protocol.CredRefEnvVar:
		present := m.env.IsPresent(ref.Locator)
		status := protocol.AuthStatusUnauthenticated
		detail := "environment variable is unset or empty"
		if present {
			status = protocol.AuthStatusIndeterminate
			detail = "environment variable is present; presence alone does not verify the credential is valid or accepted by the provider"
		}
		evidence = protocol.AuthEvidence{
			SchemaVersion: protocol.SchemaVersion1,
			RefID:         ref.RefID,
			Kind:          ref.Kind,
			Status:        status,
			ProbeKind:     protocol.AuthProbeEnvPresence,
			ObservedAt:    now,
			ProbeTarget:   ref.Locator,
			Detail:        detail,
		}

	case protocol.CredRefCLISession:
		var matched CLISessionAuthAdapter
		for _, adapter := range m.cliAdapters {
			if adapter.Handles(ref.Locator) {
				matched = adapter
				break
			}
		}

		if matched == nil {
			evidence = protocol.AuthEvidence{
				SchemaVersion: protocol.SchemaVersion1,
				RefID:         ref.RefID,
				Kind:          ref.Kind,
				Status:        protocol.AuthStatusIndeterminate,
				ProbeKind:     protocol.AuthProbeCLIAuthCall,
				ObservedAt:    now,
				ProbeTarget:   ref.Locator,
				Detail:        "no auth probe adapter available for CLI locator",
			}
		} else {
			evidence = matched.ProbeAuth(ctx, m.runner, m.clock, ref)
		}

	case protocol.CredRefKeychainRef:
		// A KeychainChecker reports its outcome as (present, err): err
		// means the check itself could not be completed — including an
		// unsupported/unavailable backend (see UnsupportedKeychainChecker) —
		// which is evidence the check is inconclusive, not evidence the
		// referenced item is absent. Only a completed check (err == nil)
		// distinguishes present from absent.
		present, err := m.keychain.CheckPresence(ctx, ref.Locator)
		if err != nil {
			evidence = protocol.AuthEvidence{
				SchemaVersion: protocol.SchemaVersion1,
				RefID:         ref.RefID,
				Kind:          ref.Kind,
				Status:        protocol.AuthStatusUnavailable,
				ProbeKind:     protocol.AuthProbeKeychainPresence,
				ObservedAt:    now,
				ProbeTarget:   ref.Locator,
				Detail:        "keychain presence check could not be completed",
			}
		} else if present {
			evidence = protocol.AuthEvidence{
				SchemaVersion: protocol.SchemaVersion1,
				RefID:         ref.RefID,
				Kind:          ref.Kind,
				Status:        protocol.AuthStatusIndeterminate,
				ProbeKind:     protocol.AuthProbeKeychainPresence,
				ObservedAt:    now,
				ProbeTarget:   ref.Locator,
				Detail:        "keychain item is present; presence alone does not verify the credential is valid or accepted by the provider",
			}
		} else {
			evidence = protocol.AuthEvidence{
				SchemaVersion: protocol.SchemaVersion1,
				RefID:         ref.RefID,
				Kind:          ref.Kind,
				Status:        protocol.AuthStatusUnauthenticated,
				ProbeKind:     protocol.AuthProbeKeychainPresence,
				ObservedAt:    now,
				ProbeTarget:   ref.Locator,
				Detail:        "keychain item is not found",
			}
		}

	default:
		return protocol.AuthEvidence{}, errs.New(errs.CategoryInvalidArgument,
			"credentials: unknown credential reference kind %q", ref.Kind)
	}

	if err := evidence.Validate(); err != nil {
		return protocol.AuthEvidence{}, errs.Wrap(errs.CategoryInternal, err,
			"credentials: generated auth evidence violated schema")
	}

	return evidence, nil
}
