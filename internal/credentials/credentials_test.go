package credentials_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/credentials"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
)

const SentinelSecret = "sk-ant-test-sentinel-vault-token-xyz-12345"

type fakeRunner struct {
	results     map[string]process.Result
	errs        map[string]error
	invocations []string
}

func (f *fakeRunner) Run(_ context.Context, spec process.Spec) (process.Result, error) {
	key := spec.Executable + " " + strings.Join(spec.Args, " ")
	f.invocations = append(f.invocations, key)
	if err, ok := f.errs[key]; ok {
		return process.Result{}, err
	}
	if res, ok := f.results[key]; ok {
		return res, nil
	}
	// Default to exit 0
	return process.Result{Status: process.StatusCompleted, ExitCode: 0}, nil
}

func (f *fakeRunner) calls() []string { return f.invocations }

func TestEnvVarPresenceOnly(t *testing.T) {
	reader := credentials.NewMapEnvReader(map[string]string{
		"ANTHROPIC_API_KEY": SentinelSecret,
		"EMPTY_KEY":         "",
	})

	if !reader.IsPresent("ANTHROPIC_API_KEY") {
		t.Fatal("expected ANTHROPIC_API_KEY to be present")
	}
	if reader.IsPresent("EMPTY_KEY") {
		t.Fatal("expected EMPTY_KEY (empty string) to be not present")
	}
	if reader.IsPresent("NONEXISTENT_KEY") {
		t.Fatal("expected NONEXISTENT_KEY to be not present")
	}

	// Verify using OsEnvReader with real environment
	os.Setenv("TEST_CADENCE_SENTINEL", SentinelSecret)
	defer os.Unsetenv("TEST_CADENCE_SENTINEL")

	osReader := credentials.OsEnvReader{}
	if !osReader.IsPresent("TEST_CADENCE_SENTINEL") {
		t.Fatal("expected TEST_CADENCE_SENTINEL to be present in OS environment")
	}
}

func TestSentinelSecretNeverLeaksFromEnvResolution(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), 0)
	env := credentials.NewMapEnvReader(map[string]string{
		"ANTHROPIC_API_KEY": SentinelSecret,
	})

	mgr, err := credentials.NewManager(credentials.Options{
		Clock: clk,
		Env:   env,
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ref := protocol.CredentialRef{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-001",
		Kind:          protocol.CredRefEnvVar,
		Locator:       "ANTHROPIC_API_KEY",
	}

	ev, err := mgr.CheckCredential(context.Background(), ref)
	if err != nil {
		t.Fatalf("unexpected CheckCredential error: %v", err)
	}

	// Presence of an env var is not proof of authentication (finding 1):
	// only an authoritative probe that actually exercises the credential
	// can produce "authenticated".
	if ev.Status != protocol.AuthStatusIndeterminate {
		t.Fatalf("status = %q, want %q", ev.Status, protocol.AuthStatusIndeterminate)
	}

	// Assert sentinel secret never appears in any string representation or serialization
	serialized, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("failed to marshal evidence: %v", err)
	}

	targets := []string{
		ev.RefID,
		string(ev.Kind),
		string(ev.Status),
		string(ev.ProbeKind),
		ev.ProbeTarget,
		ev.AdapterID,
		ev.Detail,
		fmt.Sprintf("%v", ev),
		fmt.Sprintf("%+v", ev),
		fmt.Sprintf("%#v", ev),
		string(serialized),
	}

	for _, target := range targets {
		if strings.Contains(target, SentinelSecret) {
			t.Fatalf("SECURITY VIOLATION: sentinel secret leaked in output: %q", target)
		}
	}
}

func TestCLIVersionOutputAloneCannotEstablishAuthenticatedState(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), 0)
	runner := &fakeRunner{
		results: map[string]process.Result{
			"claude --version": {
				Status:   process.StatusCompleted,
				ExitCode: 0,
				Stdout:   []byte("claude-code version 1.2.3\n"),
			},
		},
	}

	versionAdapter := &credentials.VersionOnlyAdapter{
		ID:     "claude-version-probe",
		Handle: "claude",
	}

	mgr, err := credentials.NewManager(credentials.Options{
		Clock:       clk,
		Runner:      runner,
		CLIAdapters: []credentials.CLISessionAuthAdapter{versionAdapter},
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ref := protocol.CredentialRef{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-claude",
		Kind:          protocol.CredRefCLISession,
		Locator:       "claude",
	}

	ev, err := mgr.CheckCredential(context.Background(), ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ev.Status == protocol.AuthStatusAuthenticated {
		t.Fatalf("SECURITY VIOLATION: version probe established authenticated state! Got: %q", ev.Status)
	}
	if ev.Status != protocol.AuthStatusIndeterminate {
		t.Fatalf("status = %q, want indeterminate", ev.Status)
	}
	if ev.ProbeKind != protocol.AuthProbeCLIVersionOnly {
		t.Fatalf("probe_kind = %q, want cli_version_only", ev.ProbeKind)
	}
}

func TestCLIAuthProbeSuccessAndFailure(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), 0)
	runner := &fakeRunner{
		results: map[string]process.Result{
			"claude auth status": {
				Status:   process.StatusCompleted,
				ExitCode: 0,
				Stdout:   []byte("Logged in as user@example.com\n"),
			},
			"codex auth status": {
				Status:   process.StatusCompleted,
				ExitCode: 1,
				Stdout:   []byte("Error: Not logged in. Please run codex login.\n"),
			},
			"gemini auth status": {
				Status: process.StatusTimeout,
			},
		},
		errs: map[string]error{
			"missing auth status": errs.New(errs.CategoryNotFound, "executable not found"),
		},
	}

	adapters := []credentials.CLISessionAuthAdapter{
		&credentials.BoundedCLIAuthAdapter{
			ID:                  "claude-auth",
			Handle:              "claude",
			Probe:               credentials.MustAuthProbeDefinition([]string{"auth", "status"}),
			UnauthenticatedMsgs: []string{"not logged in", "login required"},
		},
		&credentials.BoundedCLIAuthAdapter{
			ID:                  "codex-auth",
			Handle:              "codex",
			Probe:               credentials.MustAuthProbeDefinition([]string{"auth", "status"}),
			UnauthenticatedMsgs: []string{"not logged in"},
		},
		&credentials.BoundedCLIAuthAdapter{
			ID:     "gemini-auth",
			Handle: "gemini",
			Probe:  credentials.MustAuthProbeDefinition([]string{"auth", "status"}),
		},
		&credentials.BoundedCLIAuthAdapter{
			ID:     "missing-auth",
			Handle: "missing",
			Probe:  credentials.MustAuthProbeDefinition([]string{"auth", "status"}),
		},
	}

	mgr, err := credentials.NewManager(credentials.Options{
		Clock:       clk,
		Runner:      runner,
		CLIAdapters: adapters,
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// 1. Success -> authenticated
	evClaude, err := mgr.CheckCredential(context.Background(), protocol.CredentialRef{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-claude",
		Kind:          protocol.CredRefCLISession,
		Locator:       "claude",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evClaude.Status != protocol.AuthStatusAuthenticated {
		t.Fatalf("claude status = %q, want authenticated", evClaude.Status)
	}

	// 2. Failure -> unauthenticated
	evCodex, err := mgr.CheckCredential(context.Background(), protocol.CredentialRef{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-codex",
		Kind:          protocol.CredRefCLISession,
		Locator:       "codex",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evCodex.Status != protocol.AuthStatusUnauthenticated {
		t.Fatalf("codex status = %q, want unauthenticated", evCodex.Status)
	}

	// 3. Timeout -> indeterminate
	evGemini, err := mgr.CheckCredential(context.Background(), protocol.CredentialRef{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-gemini",
		Kind:          protocol.CredRefCLISession,
		Locator:       "gemini",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evGemini.Status != protocol.AuthStatusIndeterminate {
		t.Fatalf("gemini status = %q, want indeterminate", evGemini.Status)
	}

	// 4. Missing / Error -> indeterminate/unavailable
	evMissing, err := mgr.CheckCredential(context.Background(), protocol.CredentialRef{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-missing",
		Kind:          protocol.CredRefCLISession,
		Locator:       "missing",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evMissing.Status != protocol.AuthStatusIndeterminate {
		t.Fatalf("missing status = %q, want indeterminate", evMissing.Status)
	}
}

func TestHostileAuthProbeOutputNeverLeaks(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), 0)
	hostileToken := "sk-live-hostile-token-which-must-not-appear-in-state-12345"

	runner := &fakeRunner{
		results: map[string]process.Result{
			"claude auth status": {
				Status:   process.StatusCompleted,
				ExitCode: 0,
				Stdout:   []byte("Authentication successful! Token: " + hostileToken + "\n"),
				Stderr:   []byte("DEBUG: session key " + hostileToken + "\n"),
			},
		},
	}

	adapter := &credentials.BoundedCLIAuthAdapter{
		ID:     "claude-auth",
		Handle: "claude",
		Probe:  credentials.MustAuthProbeDefinition([]string{"auth", "status"}),
	}

	mgr, err := credentials.NewManager(credentials.Options{
		Clock:       clk,
		Runner:      runner,
		CLIAdapters: []credentials.CLISessionAuthAdapter{adapter},
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ev, err := mgr.CheckCredential(context.Background(), protocol.CredentialRef{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-claude",
		Kind:          protocol.CredRefCLISession,
		Locator:       "claude",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify hostileToken never appears in Detail or anywhere in ev
	if strings.Contains(ev.Detail, hostileToken) {
		t.Fatalf("SECURITY VIOLATION: hostile token leaked into Detail: %q", ev.Detail)
	}

	evData, _ := json.Marshal(ev)
	if strings.Contains(string(evData), hostileToken) {
		t.Fatalf("SECURITY VIOLATION: hostile token leaked into serialized AuthEvidence: %s", string(evData))
	}
}

func TestValidateProcessSpecNoSecrets(t *testing.T) {
	// 1. Valid clean spec
	cleanSpec := process.Spec{
		Executable: "git",
		Args:       []string{"status", "--short"},
		Dir:        "/tmp",
		Env:        []string{"PATH=/usr/bin:/bin", "HOME=/home/user"},
	}
	if err := credentials.ValidateProcessSpecNoSecrets(cleanSpec); err != nil {
		t.Fatalf("expected clean spec to pass validation, got: %v", err)
	}

	// 2. Secret in Args
	secretArgSpec := cleanSpec
	secretArgSpec.Args = []string{"login", "--token", SentinelSecret}
	if err := credentials.ValidateProcessSpecNoSecrets(secretArgSpec); err == nil {
		t.Fatal("expected secret in Args to be rejected, but passed")
	}

	// 3. Secret in Env
	secretEnvSpec := cleanSpec
	secretEnvSpec.Env = []string{"PATH=/bin", "AUTH_KEY=" + SentinelSecret}
	if err := credentials.ValidateProcessSpecNoSecrets(secretEnvSpec); err == nil {
		t.Fatal("expected secret in Env to be rejected, but passed")
	}

	// 4. Token prefix in Env
	tokenEnvSpec := cleanSpec
	tokenEnvSpec.Env = []string{"BEARER=bearer xyz123"}
	if err := credentials.ValidateProcessSpecNoSecrets(tokenEnvSpec); err == nil {
		t.Fatal("expected bearer token in Env to be rejected, but passed")
	}

	// 5. A long but ordinary PATH is not a secret merely for being long
	// (independent-review follow-up on WP-M3B-4, finding 2): an earlier
	// revision of LooksLikeSecret rejected any value over 128 bytes
	// unconditionally, which would have made a perfectly normal
	// multi-directory PATH untestable via the real process.Runner.
	longPathSpec := cleanSpec
	longDirs := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		longDirs = append(longDirs, fmt.Sprintf("/opt/tools/bin-%02d", i))
	}
	longPath := "PATH=" + strings.Join(longDirs, ":")
	if len(longPath) <= 128 {
		t.Fatalf("test setup error: longPath is only %d bytes, want > 128", len(longPath))
	}
	longPathSpec.Env = []string{longPath, "HOME=/home/user"}
	if err := credentials.ValidateProcessSpecNoSecrets(longPathSpec); err != nil {
		t.Fatalf("expected a long but ordinary PATH to pass validation, got: %v", err)
	}

	// 6. A genuinely secret-shaped long value is still rejected: removing
	// the length-based heuristic must not weaken the prefix/keyword
	// detection that actually identifies secret shapes.
	longSecretSpec := cleanSpec
	longSecretSpec.Env = []string{"PATH=/usr/bin", "TOKEN=" + SentinelSecret + strings.Repeat("x", 100)}
	if err := credentials.ValidateProcessSpecNoSecrets(longSecretSpec); err == nil {
		t.Fatal("expected a long secret-shaped env value to still be rejected, but passed")
	}
}

func TestKeychainPresenceResolution(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), 0)
	checker := credentials.NewMapKeychainChecker(map[string]bool{
		"devcadence/existing/key": true,
	})

	mgr, err := credentials.NewManager(credentials.Options{
		Clock:    clk,
		Keychain: checker,
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Existing item -> indeterminate (presence only; finding 1 — presence
	// must never be promoted to authenticated).
	evPresent, err := mgr.CheckCredential(context.Background(), protocol.CredentialRef{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-kc-1",
		Kind:          protocol.CredRefKeychainRef,
		Locator:       "devcadence/existing/key",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evPresent.Status != protocol.AuthStatusIndeterminate {
		t.Fatalf("status = %q, want indeterminate", evPresent.Status)
	}

	// Missing item -> unauthenticated
	evMissing, err := mgr.CheckCredential(context.Background(), protocol.CredentialRef{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-kc-2",
		Kind:          protocol.CredRefKeychainRef,
		Locator:       "devcadence/missing/key",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evMissing.Status != protocol.AuthStatusUnauthenticated {
		t.Fatalf("status = %q, want unauthenticated", evMissing.Status)
	}
}

func TestProviderNeutralAdapterContract(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), 0)
	// Add a custom extension adapter without changing any core protocol types
	customAdapter := &credentials.StaticCLIAuthAdapter{
		ID:        "custom-enterprise-cli",
		Target:    "enterprise-tool",
		Status:    protocol.AuthStatusAuthenticated,
		ProbeKind: protocol.AuthProbeCLIAuthCall,
		Detail:    "enterprise SSO session confirmed",
	}

	mgr, err := credentials.NewManager(credentials.Options{
		Clock:       clk,
		CLIAdapters: []credentials.CLISessionAuthAdapter{customAdapter},
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ev, err := mgr.CheckCredential(context.Background(), protocol.CredentialRef{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-enterprise",
		Kind:          protocol.CredRefCLISession,
		Locator:       "enterprise-tool:profile-a",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Status != protocol.AuthStatusAuthenticated {
		t.Fatalf("status = %q, want authenticated", ev.Status)
	}
	if ev.AdapterID != "custom-enterprise-cli" {
		t.Fatalf("adapter_id = %q, want custom-enterprise-cli", ev.AdapterID)
	}
}

func TestSerializationContainsNoSentinelSecretMaterial(t *testing.T) {
	sentinel := "sk-ant-sentinel-vault-token-xyz-12345"
	clk := clock.NewFake(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), 0)
	env := credentials.NewMapEnvReader(map[string]string{
		"OPENAI_API_KEY": sentinel,
	})

	mgr, err := credentials.NewManager(credentials.Options{
		Clock: clk,
		Env:   env,
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ref := protocol.CredentialRef{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-openai",
		Kind:          protocol.CredRefEnvVar,
		Locator:       "OPENAI_API_KEY",
	}

	ev, err := mgr.CheckCredential(context.Background(), ref)
	if err != nil {
		t.Fatalf("CheckCredential failed: %v", err)
	}

	// 1. Serialize CredentialRef
	refBytes, err := json.Marshal(ref)
	if err != nil {
		t.Fatalf("json.Marshal(ref) failed: %v", err)
	}
	if strings.Contains(string(refBytes), sentinel) {
		t.Fatal("sentinel secret leaked into serialized CredentialRef")
	}

	// 2. Serialize AuthEvidence
	evBytes, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("json.Marshal(ev) failed: %v", err)
	}
	if strings.Contains(string(evBytes), sentinel) {
		t.Fatal("sentinel secret leaked into serialized AuthEvidence")
	}

	// 3. Serialize SetupLedgerEvent recording this action
	ledgerEvent := protocol.SetupLedgerEvent{
		SchemaVersion: protocol.SchemaVersion1,
		Sequence:      1,
		EventID:       "ev-001",
		ExecutionID:   "exec-001",
		PlanID:        "plan-001",
		PlanDigest:    "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Timestamp:     protocol.NewTimestamp(clk.Now()),
		Type:          protocol.EventActionStarting,
		ActionID:      "action-001",
		Payload: protocol.EventPayload{
			ActionStarting: &protocol.ActionStartingPayload{
				ActionID: "action-001",
				RecipeID: "check-credentials",
			},
		},
	}
	ledgerBytes, err := json.Marshal(ledgerEvent)
	if err != nil {
		t.Fatalf("json.Marshal(ledgerEvent) failed: %v", err)
	}
	if strings.Contains(string(ledgerBytes), sentinel) {
		t.Fatal("sentinel secret leaked into serialized SetupLedgerEvent")
	}
}

// TestUnsupportedKeychainBackendIsUnavailableNotAbsent proves an
// unsupported keychain backend reports unavailable, not unauthenticated —
// an unsupported backend never actually looked for the item, so it must
// not be read as evidence the item is absent (finding 1's related bug).
func TestUnsupportedKeychainBackendIsUnavailableNotAbsent(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), 0)
	mgr, err := credentials.NewManager(credentials.Options{
		Clock:    clk,
		Keychain: credentials.UnsupportedKeychainChecker{},
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ev, err := mgr.CheckCredential(context.Background(), protocol.CredentialRef{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-kc-unsupported",
		Kind:          protocol.CredRefKeychainRef,
		Locator:       "devcadence/some/key",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Status != protocol.AuthStatusUnavailable {
		t.Fatalf("status = %q, want unavailable (an unsupported backend never checked, so it must not report unauthenticated)", ev.Status)
	}
}

// TestBoundedCLIAuthAdapterUnrecognizedFailureIsIndeterminate proves an
// unmatched nonzero exit code (no UnauthenticatedMsgs entry matched) is
// reported as indeterminate, not unauthenticated (finding 6) — a nonzero
// exit can mean executable failure, an incompatible CLI version, local
// misconfiguration, a provider outage, or a permission failure, none of
// which is evidence the user is unauthenticated.
func TestBoundedCLIAuthAdapterUnrecognizedFailureIsIndeterminate(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), 0)
	runner := &fakeRunner{
		results: map[string]process.Result{
			"flaky-cli auth status": {
				Status:   process.StatusCompleted,
				ExitCode: 127,
				Stderr:   []byte("flaky-cli: command not found in this shell config\n"),
			},
		},
	}
	adapter := &credentials.BoundedCLIAuthAdapter{
		ID:                  "flaky-auth",
		Handle:              "flaky-cli",
		Probe:               credentials.MustAuthProbeDefinition([]string{"auth", "status"}),
		UnauthenticatedMsgs: []string{"not logged in", "login required"},
	}
	mgr, err := credentials.NewManager(credentials.Options{
		Clock:       clk,
		Runner:      runner,
		CLIAdapters: []credentials.CLISessionAuthAdapter{adapter},
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ev, err := mgr.CheckCredential(context.Background(), protocol.CredentialRef{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-flaky",
		Kind:          protocol.CredRefCLISession,
		Locator:       "flaky-cli",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Status != protocol.AuthStatusIndeterminate {
		t.Fatalf("status = %q, want indeterminate (an unrecognized failure exit code is not evidence of being unauthenticated)", ev.Status)
	}
}

// TestNewAuthProbeDefinitionRejectsVersionAndHelpShapes proves the
// authoritative-probe declaration itself refuses a version/help-shaped
// argv — the exact `--version` smuggling example a review found — at
// construction time, before any BoundedCLIAuthAdapter could ever be built
// from it (independent-review follow-up on WP-M3B-4, finding 1, third
// round).
func TestNewAuthProbeDefinitionRejectsVersionAndHelpShapes(t *testing.T) {
	for _, probeArgs := range [][]string{
		{"--version"}, {"-version"}, {"-v"}, {"version"},
		{"--help"}, {"-help"}, {"-h"}, {"help"},
		{"--VERSION"}, {" --version "}, // case/whitespace insensitive
	} {
		t.Run(strings.Join(probeArgs, "|"), func(t *testing.T) {
			if _, err := credentials.NewAuthProbeDefinition(probeArgs); err == nil {
				t.Fatalf("SECURITY VIOLATION: NewAuthProbeDefinition accepted a version/help-shaped argv %v", probeArgs)
			}
		})
	}
}

// TestNewAuthProbeDefinitionRejectsEmpty proves an empty argv cannot be
// declared authoritative either.
func TestNewAuthProbeDefinitionRejectsEmpty(t *testing.T) {
	if _, err := credentials.NewAuthProbeDefinition(nil); err == nil {
		t.Fatal("expected NewAuthProbeDefinition(nil) to be rejected")
	}
	if _, err := credentials.NewAuthProbeDefinition([]string{}); err == nil {
		t.Fatal("expected NewAuthProbeDefinition([]string{}) to be rejected")
	}
}

// TestBoundedCLIAuthAdapterWithoutDeclaredProbeCannotAuthenticate proves the
// structural half of the finding-1 (third round) fix: AuthProbeDefinition's
// only field is unexported, so a BoundedCLIAuthAdapter built from a bare
// struct literal — the exact "arbitrary command reaches cli_auth_call
// authority" shape the review is concerned about — has no way to populate
// Probe at all from outside this package, leaving it at its zero value.
// That zero value must never run a command or authenticate; combined with
// TestNewAuthProbeDefinitionRejectsVersionAndHelpShapes (the only
// constructor refuses version/help), authority can only ever be granted by
// a deliberate, validated NewAuthProbeDefinition call.
func TestBoundedCLIAuthAdapterWithoutDeclaredProbeCannotAuthenticate(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), 0)
	runner := &fakeRunner{results: map[string]process.Result{}}

	// Deliberately constructed the way an attacker/careless caller would:
	// a bare struct literal with every exported field set except Probe,
	// which cannot be set this way.
	adapter := &credentials.BoundedCLIAuthAdapter{
		ID:                  "undeclared-auth",
		Handle:              "undeclared-cli",
		UnauthenticatedMsgs: []string{"not logged in"},
	}

	mgr, err := credentials.NewManager(credentials.Options{
		Clock:       clk,
		Runner:      runner,
		CLIAdapters: []credentials.CLISessionAuthAdapter{adapter},
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ev, err := mgr.CheckCredential(context.Background(), protocol.CredentialRef{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-undeclared",
		Kind:          protocol.CredRefCLISession,
		Locator:       "undeclared-cli",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Status == protocol.AuthStatusAuthenticated {
		t.Fatal("SECURITY VIOLATION: an adapter with no declared AuthProbeDefinition reported authenticated")
	}
	if ev.Status != protocol.AuthStatusUnavailable {
		t.Fatalf("status = %q, want unavailable (no authoritative probe was ever declared)", ev.Status)
	}
	if len(runner.calls()) != 0 {
		t.Fatalf("runner should never have been invoked with no declared probe, got calls: %v", runner.calls())
	}
}

// TestBoundedCLIAuthAdapterAuthoritativeProbeStillAuthenticates is the
// control for the adjacent smuggling test: a genuine auth-status command
// (not a version/help invocation) must still be able to report
// authenticated, so the finding-3 fix does not overcorrect into rejecting
// legitimate probes.
func TestBoundedCLIAuthAdapterAuthoritativeProbeStillAuthenticates(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), 0)
	runner := &fakeRunner{
		results: map[string]process.Result{
			"real-cli auth status": {
				Status:   process.StatusCompleted,
				ExitCode: 0,
				Stdout:   []byte("Logged in as user@example.com\n"),
			},
		},
	}
	adapter := &credentials.BoundedCLIAuthAdapter{
		ID:     "real-auth",
		Handle: "real-cli",
		Probe:  credentials.MustAuthProbeDefinition([]string{"auth", "status"}),
	}
	mgr, err := credentials.NewManager(credentials.Options{
		Clock:       clk,
		Runner:      runner,
		CLIAdapters: []credentials.CLISessionAuthAdapter{adapter},
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	ev, err := mgr.CheckCredential(context.Background(), protocol.CredentialRef{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-real",
		Kind:          protocol.CredRefCLISession,
		Locator:       "real-cli",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Status != protocol.AuthStatusAuthenticated {
		t.Fatalf("status = %q, want authenticated for a genuine auth-status probe", ev.Status)
	}
	if ev.ProbeKind != protocol.AuthProbeCLIAuthCall {
		t.Fatalf("probe_kind = %q, want cli_auth_call", ev.ProbeKind)
	}
}

// TestCLIAdaptersEnforceProcessSpecSecretGuard proves the secret guard is
// on the actual execution path both adapters use, not merely available as
// an opt-in helper (finding 2): an adapter configured (however that came
// to be — misconfiguration, a future adapter building args from untrusted
// input) with a secret-looking arg must never reach the runner.
func TestCLIAdaptersEnforceProcessSpecSecretGuard(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), 0)
	runner := &fakeRunner{results: map[string]process.Result{}}

	versionAdapter := &credentials.VersionOnlyAdapter{
		ID:     "leaky-version",
		Handle: "leaky-cli",
		Arg:    SentinelSecret,
	}
	authAdapter := &credentials.BoundedCLIAuthAdapter{
		ID:     "leaky-auth",
		Handle: "leaky-cli",
		Probe:  credentials.MustAuthProbeDefinition([]string{SentinelSecret}),
	}

	for _, tc := range []struct {
		name    string
		adapter credentials.CLISessionAuthAdapter
	}{
		{"VersionOnlyAdapter", versionAdapter},
		{"BoundedCLIAuthAdapter", authAdapter},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr, err := credentials.NewManager(credentials.Options{
				Clock:       clk,
				Runner:      runner,
				CLIAdapters: []credentials.CLISessionAuthAdapter{tc.adapter},
			})
			if err != nil {
				t.Fatalf("failed to create manager: %v", err)
			}

			ev, err := mgr.CheckCredential(context.Background(), protocol.CredentialRef{
				SchemaVersion: protocol.SchemaVersion1,
				RefID:         "cred-leaky",
				Kind:          protocol.CredRefCLISession,
				Locator:       "leaky-cli",
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ev.Status == protocol.AuthStatusAuthenticated {
				t.Fatalf("SECURITY VIOLATION: adapter with a secret-looking arg reached the runner and reported authenticated")
			}
			for _, call := range runner.calls() {
				if strings.Contains(call, SentinelSecret) {
					t.Fatalf("SECURITY VIOLATION: runner was invoked with a secret-looking argument: %q", call)
				}
			}
		})
	}
}

// TestAdaptersExecuteViaRealProcessRunner proves a WP4 CLI auth/version
// probe actually starts and produces bounded evidence when run through the
// real process.Runner instead of only the in-package fakeRunner test
// double (independent-review follow-up on WP-M3B-4, finding 1). fakeRunner
// never exercised process.Runner's controlled-resolution rule: a bare
// Executable is resolved only via Spec.Env's PATH, and fails closed with no
// PATH at all — so a canonical adapter built with no Env could pass every
// fakeRunner-based test while failing before the real process even starts.
//
// It also proves Handle (the opaque identifier matched against
// CredentialRef.Locator, which the cli_session locator contract forbids
// from containing "/") is independent of ExecutablePath (what actually
// gets started, which may be a bare name resolved via PATH or a
// discovered/verified absolute path) — the two cannot be collapsed into
// one field the way the pre-fix Executable field did.
func TestAdaptersExecuteViaRealProcessRunner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake executable is a POSIX shell script; not applicable on windows")
	}

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "fake-cli")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"auth\" ] && [ \"$2\" = \"status\" ]; then\n" +
		"  echo 'Logged in as test@example.com'\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo 'fake-cli version 1.0.0'\n" +
		"exit 0\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake executable: %v", err)
	}

	clk := clock.NewFake(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC), 0)
	runner := process.NewRunner()

	t.Run("VersionOnlyAdapter bare name resolved via Env PATH", func(t *testing.T) {
		adapter := &credentials.VersionOnlyAdapter{
			ID:             "fake-version",
			Handle:         "fake-cli", // logical handle: no "/" allowed by the cli_session locator contract
			ExecutablePath: "fake-cli", // bare name, resolved via Env's PATH below
			Env:            []string{"PATH=" + dir},
		}
		mgr, err := credentials.NewManager(credentials.Options{
			Clock:       clk,
			Runner:      runner,
			CLIAdapters: []credentials.CLISessionAuthAdapter{adapter},
		})
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}
		ev, err := mgr.CheckCredential(context.Background(), protocol.CredentialRef{
			SchemaVersion: protocol.SchemaVersion1,
			RefID:         "cred-fake",
			Kind:          protocol.CredRefCLISession,
			Locator:       "fake-cli",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ev.Status != protocol.AuthStatusIndeterminate {
			t.Fatalf("status = %q, want indeterminate (version probe succeeded but cannot authenticate)", ev.Status)
		}
	})

	t.Run("BoundedCLIAuthAdapter absolute ExecutablePath distinct from Handle", func(t *testing.T) {
		adapter := &credentials.BoundedCLIAuthAdapter{
			ID:             "fake-auth",
			Handle:         "fake-cli", // opaque handle used for CredentialRef matching only
			ExecutablePath: scriptPath, // discovered/verified absolute path, distinct from Handle
			Probe:          credentials.MustAuthProbeDefinition([]string{"auth", "status"}),
			Env:            process.BaseEnv(),
		}
		mgr, err := credentials.NewManager(credentials.Options{
			Clock:       clk,
			Runner:      runner,
			CLIAdapters: []credentials.CLISessionAuthAdapter{adapter},
		})
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}
		ev, err := mgr.CheckCredential(context.Background(), protocol.CredentialRef{
			SchemaVersion: protocol.SchemaVersion1,
			RefID:         "cred-fake-auth",
			Kind:          protocol.CredRefCLISession,
			Locator:       "fake-cli",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ev.Status != protocol.AuthStatusAuthenticated {
			t.Fatalf("status = %q, want authenticated", ev.Status)
		}
	})

	t.Run("bare ExecutablePath with no PATH fails closed", func(t *testing.T) {
		// A test double could never catch this: fakeRunner never resolves
		// Executable against Env at all. This is exactly the failure mode
		// the follow-up review flagged as hidden by fakeRunner-only
		// coverage.
		adapter := &credentials.VersionOnlyAdapter{
			ID:             "fake-version-no-path",
			Handle:         "fake-cli",
			ExecutablePath: "fake-cli",
			Env:            []string{}, // explicit empty Env (not nil), so the BaseEnv default does not apply
		}
		mgr, err := credentials.NewManager(credentials.Options{
			Clock:       clk,
			Runner:      runner,
			CLIAdapters: []credentials.CLISessionAuthAdapter{adapter},
		})
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}
		ev, err := mgr.CheckCredential(context.Background(), protocol.CredentialRef{
			SchemaVersion: protocol.SchemaVersion1,
			RefID:         "cred-fake-nopath",
			Kind:          protocol.CredRefCLISession,
			Locator:       "fake-cli",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ev.Status != protocol.AuthStatusUnavailable {
			t.Fatalf("status = %q, want unavailable (no PATH in Env means resolveExecutable fails closed)", ev.Status)
		}
	})

	t.Run("default Env falls back to process.BaseEnv and resolves a real PATH entry", func(t *testing.T) {
		// Not the fake-cli fixture (BaseEnv uses the real host PATH, which
		// does not contain our temp dir): a widely-available real
		// executable proves the nil-Env default itself works end to end
		// through the real runner.
		realExecutable := "true"
		if _, err := os.Stat("/usr/bin/true"); err != nil {
			t.Skip("no /usr/bin/true available on this host to probe")
		}
		adapter := &credentials.VersionOnlyAdapter{
			ID:             "real-true",
			Handle:         "posix-true",
			ExecutablePath: realExecutable,
			Arg:            "", // VersionOnlyAdapter defaults to --version; `true` ignores unknown args and exits 0 regardless
			Env:            nil,
		}
		mgr, err := credentials.NewManager(credentials.Options{
			Clock:       clk,
			Runner:      runner,
			CLIAdapters: []credentials.CLISessionAuthAdapter{adapter},
		})
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}
		ev, err := mgr.CheckCredential(context.Background(), protocol.CredentialRef{
			SchemaVersion: protocol.SchemaVersion1,
			RefID:         "cred-real-true",
			Kind:          protocol.CredRefCLISession,
			Locator:       "posix-true",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ev.Status != protocol.AuthStatusIndeterminate {
			t.Fatalf("status = %q, want indeterminate", ev.Status)
		}
	})
}
