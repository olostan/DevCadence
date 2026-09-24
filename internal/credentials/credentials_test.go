package credentials_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
	results map[string]process.Result
	errs    map[string]error
}

func (f *fakeRunner) Run(_ context.Context, spec process.Spec) (process.Result, error) {
	key := spec.Executable + " " + strings.Join(spec.Args, " ")
	if err, ok := f.errs[key]; ok {
		return process.Result{}, err
	}
	if res, ok := f.results[key]; ok {
		return res, nil
	}
	// Default to exit 0
	return process.Result{Status: process.StatusCompleted, ExitCode: 0}, nil
}

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
		RefID:   "cred-001",
		Kind:    protocol.CredRefEnvVar,
		Locator: "ANTHROPIC_API_KEY",
	}

	ev, err := mgr.CheckCredential(context.Background(), ref)
	if err != nil {
		t.Fatalf("unexpected CheckCredential error: %v", err)
	}

	if ev.Status != protocol.AuthStatusAuthenticated {
		t.Fatalf("status = %q, want %q", ev.Status, protocol.AuthStatusAuthenticated)
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
		ID:         "claude-version-probe",
		Executable: "claude",
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
		RefID:   "cred-claude",
		Kind:    protocol.CredRefCLISession,
		Locator: "claude",
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
			Executable:          "claude",
			ProbeArgs:           []string{"auth", "status"},
			UnauthenticatedMsgs: []string{"not logged in", "login required"},
		},
		&credentials.BoundedCLIAuthAdapter{
			ID:                  "codex-auth",
			Executable:          "codex",
			ProbeArgs:           []string{"auth", "status"},
			UnauthenticatedMsgs: []string{"not logged in"},
		},
		&credentials.BoundedCLIAuthAdapter{
			ID:         "gemini-auth",
			Executable: "gemini",
			ProbeArgs:  []string{"auth", "status"},
		},
		&credentials.BoundedCLIAuthAdapter{
			ID:         "missing-auth",
			Executable: "missing",
			ProbeArgs:  []string{"auth", "status"},
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
		RefID:   "cred-claude",
		Kind:    protocol.CredRefCLISession,
		Locator: "claude",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evClaude.Status != protocol.AuthStatusAuthenticated {
		t.Fatalf("claude status = %q, want authenticated", evClaude.Status)
	}

	// 2. Failure -> unauthenticated
	evCodex, err := mgr.CheckCredential(context.Background(), protocol.CredentialRef{
		RefID:   "cred-codex",
		Kind:    protocol.CredRefCLISession,
		Locator: "codex",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evCodex.Status != protocol.AuthStatusUnauthenticated {
		t.Fatalf("codex status = %q, want unauthenticated", evCodex.Status)
	}

	// 3. Timeout -> indeterminate
	evGemini, err := mgr.CheckCredential(context.Background(), protocol.CredentialRef{
		RefID:   "cred-gemini",
		Kind:    protocol.CredRefCLISession,
		Locator: "gemini",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evGemini.Status != protocol.AuthStatusIndeterminate {
		t.Fatalf("gemini status = %q, want indeterminate", evGemini.Status)
	}

	// 4. Missing / Error -> indeterminate/unavailable
	evMissing, err := mgr.CheckCredential(context.Background(), protocol.CredentialRef{
		RefID:   "cred-missing",
		Kind:    protocol.CredRefCLISession,
		Locator: "missing",
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
		ID:         "claude-auth",
		Executable: "claude",
		ProbeArgs:  []string{"auth", "status"},
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
		RefID:   "cred-claude",
		Kind:    protocol.CredRefCLISession,
		Locator: "claude",
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

	// Existing item -> authenticated
	evPresent, err := mgr.CheckCredential(context.Background(), protocol.CredentialRef{
		RefID:   "cred-kc-1",
		Kind:    protocol.CredRefKeychainRef,
		Locator: "devcadence/existing/key",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evPresent.Status != protocol.AuthStatusAuthenticated {
		t.Fatalf("status = %q, want authenticated", evPresent.Status)
	}

	// Missing item -> unauthenticated
	evMissing, err := mgr.CheckCredential(context.Background(), protocol.CredentialRef{
		RefID:   "cred-kc-2",
		Kind:    protocol.CredRefKeychainRef,
		Locator: "devcadence/missing/key",
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
		RefID:   "cred-enterprise",
		Kind:    protocol.CredRefCLISession,
		Locator: "enterprise-tool:profile-a",
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
		RefID:   "cred-openai",
		Kind:    protocol.CredRefEnvVar,
		Locator: "OPENAI_API_KEY",
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

