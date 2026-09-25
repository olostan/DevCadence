package setup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/credentials"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
)

func TestEvaluateConditionCommandAvailable(t *testing.T) {
	deps := EvaluatorDeps{}
	passed, _, err := EvaluateCondition(context.Background(), deps, protocol.Condition{
		Kind:             protocol.CondKindCommandAvailable,
		CommandAvailable: &protocol.CommandAvailableOperand{CommandName: "definitely-not-a-real-command-xyz"},
	})
	if err != nil {
		t.Fatalf("EvaluateCondition: %v", err)
	}
	if passed {
		t.Error("passed = true for a nonexistent command, want false")
	}
}

func TestEvaluateConditionManagedDirExists(t *testing.T) {
	home := t.TempDir()
	deps := EvaluatorDeps{Home: home}
	cond := protocol.Condition{
		Kind: protocol.CondKindManagedDirExists,
		ManagedDirExists: &protocol.ManagedDirOperand{
			Location:    protocol.LocationTmp,
			FileModeOct: "0700",
		},
	}

	passed, _, err := EvaluateCondition(context.Background(), deps, cond)
	if err != nil {
		t.Fatalf("EvaluateCondition (before create): %v", err)
	}
	if passed {
		t.Error("passed = true before the directory exists, want false")
	}

	if err := EnsureLayout(home); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	passed, _, err = EvaluateCondition(context.Background(), deps, cond)
	if err != nil {
		t.Fatalf("EvaluateCondition (after create): %v", err)
	}
	if !passed {
		t.Error("passed = false after the directory exists with the right mode, want true")
	}
}

func TestEvaluateConditionPortListening(t *testing.T) {
	deps := EvaluatorDeps{}
	passed, _, err := EvaluateCondition(context.Background(), deps, protocol.Condition{
		Kind: protocol.CondKindPortListening,
		PortListening: &protocol.PortOperand{
			Host: "127.0.0.1",
			Port: 1, // reserved, essentially guaranteed not to be listening
		},
	})
	if err != nil {
		t.Fatalf("EvaluateCondition: %v", err)
	}
	if passed {
		t.Error("passed = true for a port essentially guaranteed not to be listening, want false")
	}
}

func TestEvaluateConditionExecutableVerifiedDigestMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-tool")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho hi\n"), 0755); err != nil {
		t.Fatalf("write fake executable: %v", err)
	}

	deps := EvaluatorDeps{}
	passed, _, err := EvaluateCondition(context.Background(), deps, protocol.Condition{
		Kind: protocol.CondKindExecutableVerified,
		ExecutableVerified: &protocol.ExecutableVerifiedOperand{
			CanonicalPath:   path,
			ExpectedVersion: "1.0",
			ExpectedDigest:  "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		},
	})
	if err != nil {
		t.Fatalf("EvaluateCondition: %v", err)
	}
	if passed {
		t.Error("passed = true despite a digest mismatch, want false")
	}
}

func TestEvaluateConditionExecutableVerifiedDigestMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-tool")
	content := []byte("#!/bin/sh\necho hi\n")
	if err := os.WriteFile(path, content, 0755); err != nil {
		t.Fatalf("write fake executable: %v", err)
	}
	digest, err := digestFile(path)
	if err != nil {
		t.Fatalf("digestFile: %v", err)
	}

	deps := EvaluatorDeps{}
	passed, _, err := EvaluateCondition(context.Background(), deps, protocol.Condition{
		Kind: protocol.CondKindExecutableVerified,
		ExecutableVerified: &protocol.ExecutableVerifiedOperand{
			CanonicalPath:  path,
			ExpectedDigest: digest,
		},
	})
	if err != nil {
		t.Fatalf("EvaluateCondition: %v", err)
	}
	if !passed {
		t.Error("passed = false despite a matching digest, want true")
	}
}

func TestEvaluateConditionExecutableVerifiedVersionViaRunner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-tool")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho hi\n"), 0755); err != nil {
		t.Fatalf("write fake executable: %v", err)
	}

	runner := &fakeCommandRunner{results: map[string]process.Result{
		path: {Status: process.StatusCompleted, ExitCode: 0, Stdout: []byte("fake-tool version 9.9.9")},
	}}
	deps := EvaluatorDeps{Runner: runner, Home: dir}

	passed, _, err := EvaluateCondition(context.Background(), deps, protocol.Condition{
		Kind: protocol.CondKindExecutableVerified,
		ExecutableVerified: &protocol.ExecutableVerifiedOperand{
			CanonicalPath:   path,
			ExpectedVersion: "9.9.9",
		},
	})
	if err != nil {
		t.Fatalf("EvaluateCondition: %v", err)
	}
	if !passed {
		t.Error("passed = false despite matching --version output, want true")
	}
	if len(runner.calls) != 1 || runner.calls[0].Executable != path {
		t.Errorf("runner.calls = %+v, want exactly one call to %s", runner.calls, path)
	}
}

func TestEvaluateConditionEndpointHealthyFailsClosedWithoutChecker(t *testing.T) {
	deps := EvaluatorDeps{} // no EndpointHealth configured
	_, _, err := EvaluateCondition(context.Background(), deps, protocol.Condition{
		Kind:            protocol.CondKindEndpointHealthy,
		EndpointHealthy: &protocol.EndpointOperand{EndpointID: "ep-001"},
	})
	if err == nil {
		t.Fatal("EvaluateCondition succeeded for endpoint_healthy with no EndpointHealthChecker configured; expected a fail-closed error")
	}
}

type stubEndpointHealthChecker struct {
	healthy bool
	detail  string
	err     error
}

func (s stubEndpointHealthChecker) CheckEndpointHealthy(ctx context.Context, endpointID string) (bool, string, error) {
	return s.healthy, s.detail, s.err
}

func TestEvaluateConditionEndpointHealthyUsesConfiguredChecker(t *testing.T) {
	deps := EvaluatorDeps{EndpointHealth: stubEndpointHealthChecker{healthy: true, detail: "ok"}}
	passed, detail, err := EvaluateCondition(context.Background(), deps, protocol.Condition{
		Kind:            protocol.CondKindEndpointHealthy,
		EndpointHealthy: &protocol.EndpointOperand{EndpointID: "ep-001"},
	})
	if err != nil {
		t.Fatalf("EvaluateCondition: %v", err)
	}
	if !passed || detail != "ok" {
		t.Errorf("passed=%v detail=%q, want true/\"ok\"", passed, detail)
	}
}

func TestEvaluateConditionEndpointAuthenticatedFailsClosedWithoutChecker(t *testing.T) {
	deps := EvaluatorDeps{} // no EndpointAuth configured
	_, _, err := EvaluateCondition(context.Background(), deps, protocol.Condition{
		Kind:                  protocol.CondKindEndpointAuthenticated,
		EndpointAuthenticated: &protocol.EndpointOperand{EndpointID: "ep-001"},
	})
	if err == nil {
		t.Fatal("EvaluateCondition succeeded for endpoint_authenticated with no EndpointAuthChecker configured; expected a fail-closed error")
	}
}

type stubEndpointAuthChecker struct {
	authenticated bool
	detail        string
	err           error
}

func (s stubEndpointAuthChecker) CheckEndpointAuthenticated(ctx context.Context, endpointID, credentialRefID string) (bool, string, error) {
	return s.authenticated, s.detail, s.err
}

// TestEndpointHealthyAndEndpointAuthenticatedAreIndependent is the
// independent-review follow-up on WP-M3B-5, finding 6's required
// regression: an endpoint reporting healthy=true must not make
// endpoint_authenticated pass too — health and authentication are checked
// through entirely separate dependencies, so a re-authenticate action's
// postcondition (which now uses endpoint_authenticated, not
// endpoint_healthy) cannot be satisfied by mere health.
func TestEndpointHealthyAndEndpointAuthenticatedAreIndependent(t *testing.T) {
	deps := EvaluatorDeps{
		EndpointHealth: stubEndpointHealthChecker{healthy: true, detail: "process is running"},
		EndpointAuth:   stubEndpointAuthChecker{authenticated: false, detail: "session expired"},
	}

	healthyPassed, _, err := EvaluateCondition(context.Background(), deps, protocol.Condition{
		Kind:            protocol.CondKindEndpointHealthy,
		EndpointHealthy: &protocol.EndpointOperand{EndpointID: "ep-001"},
	})
	if err != nil {
		t.Fatalf("EvaluateCondition(endpoint_healthy): %v", err)
	}
	if !healthyPassed {
		t.Fatal("expected endpoint_healthy to pass (the endpoint is healthy in this scenario)")
	}

	authPassed, detail, err := EvaluateCondition(context.Background(), deps, protocol.Condition{
		Kind:                  protocol.CondKindEndpointAuthenticated,
		EndpointAuthenticated: &protocol.EndpointOperand{EndpointID: "ep-001", CredentialRefID: "claude-cli-ref"},
	})
	if err != nil {
		t.Fatalf("EvaluateCondition(endpoint_authenticated): %v", err)
	}
	if authPassed {
		t.Fatalf("SECURITY-ADJACENT BUG: endpoint_authenticated passed (detail=%q) for an endpoint that is healthy but not authenticated — a re-authenticate action's postcondition must not be satisfiable by mere health", detail)
	}
}

func TestEvaluateConditionEndpointAuthenticatedUsesConfiguredChecker(t *testing.T) {
	deps := EvaluatorDeps{EndpointAuth: stubEndpointAuthChecker{authenticated: true, detail: "logged in"}}
	passed, detail, err := EvaluateCondition(context.Background(), deps, protocol.Condition{
		Kind:                  protocol.CondKindEndpointAuthenticated,
		EndpointAuthenticated: &protocol.EndpointOperand{EndpointID: "ep-001", CredentialRefID: "claude-cli-ref"},
	})
	if err != nil {
		t.Fatalf("EvaluateCondition: %v", err)
	}
	if !passed || detail != "logged in" {
		t.Errorf("passed=%v detail=%q, want true/\"logged in\"", passed, detail)
	}
}

// TestCredentialsEndpointAuthChecker_EvaluatesAuthViaCredentialManager is the
// independent-review follow-up on WP-M3B-5, round-3 finding 2: the checker
// must resolve a condition's explicit CredentialRefID against the actual
// configured CredentialRef index, never guess a cli_session locator from the
// endpoint ID. Regressions specifically cover an endpoint ID that differs
// from its credential's locator, and a non-CLI (env_var) CredentialRef kind
// for a remote API endpoint.
func TestCredentialsEndpointAuthChecker_EvaluatesAuthViaCredentialManager(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFake(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC), 0)
	adapterClaude := &credentials.StaticCLIAuthAdapter{
		ID:     "adapter-claude",
		Target: "claude-handle",
		Status: protocol.AuthStatusAuthenticated,
		Detail: "claude session active",
	}
	adapterCodex := &credentials.StaticCLIAuthAdapter{
		ID:     "adapter-codex",
		Target: "codex",
		Status: protocol.AuthStatusUnauthenticated,
		Detail: "codex session unauthenticated",
	}
	mgr, err := credentials.NewManager(credentials.Options{
		Clock:       clk,
		Env:         credentials.NewMapEnvReader(map[string]string{"REMOTE_API_KEY": "present"}),
		CLIAdapters: []credentials.CLISessionAuthAdapter{adapterClaude, adapterCodex},
	})
	if err != nil {
		t.Fatalf("credentials.NewManager: %v", err)
	}

	refs := []protocol.CredentialRef{
		{
			SchemaVersion: protocol.SchemaVersion1,
			// RefID deliberately differs from both the endpoint ID below
			// and the CLI adapter's own Locator/Target, proving the
			// checker resolves by the explicit binding rather than
			// assuming any of those strings are interchangeable.
			RefID:   "claude-cli-ref",
			Kind:    protocol.CredRefCLISession,
			Locator: "claude-handle",
		},
		{
			SchemaVersion: protocol.SchemaVersion1,
			RefID:         "codex-cli-ref",
			Kind:          protocol.CredRefCLISession,
			Locator:       "codex",
		},
		{
			SchemaVersion: protocol.SchemaVersion1,
			RefID:         "remote-api-ref",
			Kind:          protocol.CredRefEnvVar,
			Locator:       "REMOTE_API_KEY",
		},
	}

	checker, err := NewCredentialsEndpointAuthChecker(mgr, refs)
	if err != nil {
		t.Fatalf("NewCredentialsEndpointAuthChecker: %v", err)
	}

	t.Run("endpoint ID differs from credential locator", func(t *testing.T) {
		authed, detail, err := checker.CheckEndpointAuthenticated(ctx, "cli:claude-code", "claude-cli-ref")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !authed || detail != "claude session active" {
			t.Errorf("authed=%v detail=%q, want true/\"claude session active\"", authed, detail)
		}
	})

	t.Run("unauthenticated CLI endpoint", func(t *testing.T) {
		authed, detail, err := checker.CheckEndpointAuthenticated(ctx, "cli:codex", "codex-cli-ref")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if authed || detail != "codex session unauthenticated" {
			t.Errorf("authed=%v detail=%q, want false/\"codex session unauthenticated\"", authed, detail)
		}
	})

	t.Run("expired remote API endpoint bound to a non-CLI env_var CredentialRef", func(t *testing.T) {
		// env_var presence is only ever indeterminate evidence (never
		// AuthStatusAuthenticated — Manager.CheckCredential's own
		// documented rule), so this proves the checker supports a
		// CredentialRefKind other than cli_session at all, rather than
		// forcing CLI semantics onto a remote API endpoint.
		authed, _, err := checker.CheckEndpointAuthenticated(ctx, "remote:some-provider", "remote-api-ref")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if authed {
			t.Errorf("authed=%v, want false: env_var presence is indeterminate, never authenticated", authed)
		}
	})

	t.Run("empty CredentialRefID fails closed without guessing", func(t *testing.T) {
		authed, detail, err := checker.CheckEndpointAuthenticated(ctx, "cli:claude-code", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if authed {
			t.Errorf("authed=%v, want false for an endpoint with no configured credential binding", authed)
		}
		if detail == "" {
			t.Errorf("expected a non-empty detail explaining no binding is configured")
		}
	})

	t.Run("unresolved CredentialRefID fails closed without guessing", func(t *testing.T) {
		authed, _, err := checker.CheckEndpointAuthenticated(ctx, "cli:claude-code", "no-such-ref")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if authed {
			t.Errorf("authed=%v, want false for a CredentialRefID absent from the configured index", authed)
		}
	})
}

func TestExecutor_ProductionWiredEndpointAuthEvaluatesCondition(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFake(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()
	tmpHome := t.TempDir()

	adapterClaude := &credentials.StaticCLIAuthAdapter{
		ID:     "adapter-claude",
		Target: "claude",
		Status: protocol.AuthStatusAuthenticated,
		Detail: "session ok",
	}
	mgr, err := credentials.NewManager(credentials.Options{
		Clock:       clk,
		CLIAdapters: []credentials.CLISessionAuthAdapter{adapterClaude},
	})
	if err != nil {
		t.Fatalf("credentials.NewManager: %v", err)
	}

	exec, err := NewExecutor(ExecutorOptions{
		Runner:            &fakeCommandRunner{},
		Home:              tmpHome,
		Clock:             clk,
		IDs:               seq,
		CredentialManager: mgr,
		CredentialRefs: []protocol.CredentialRef{
			{SchemaVersion: protocol.SchemaVersion1, RefID: "claude-cli-ref", Kind: protocol.CredRefCLISession, Locator: "claude"},
		},
		// EndpointAuth is intentionally nil to prove NewExecutor wires it via CredentialManager
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	conds := []protocol.Condition{
		{
			Kind:                  protocol.CondKindEndpointAuthenticated,
			EndpointAuthenticated: &protocol.EndpointOperand{EndpointID: "cli:claude", CredentialRefID: "claude-cli-ref"},
		},
	}
	passed, detail, err := exec.CheckPostconditions(ctx, conds)
	if err != nil {
		t.Fatalf("CheckPostconditions: %v", err)
	}
	if !passed {
		t.Fatalf("expected postcondition to pass via production wired auth checker, got detail: %s", detail)
	}
}

func TestEvaluateConditionModelPresentMLX(t *testing.T) {
	cacheDir := t.TempDir()
	modelRef := "mlx-community/Qwen2.5-Coder-7B-Instruct-4bit"
	revision := "019cc73c45c770444708a6dd8690c66243cc5c80"
	writeFakeHFSnapshot(t, cacheDir, modelRef, revision, 1024)

	// MLXAdapter.ModelPresent is filesystem-only — it must not touch a
	// CommandRunner at all, unlike the earlier subprocess-based design
	// (see mlx_adapter.go's doc comment for why: a postcondition/recovery
	// check must not depend on ambient PATH resolution).
	deps := EvaluatorDeps{ModelRuntimes: NewModelRuntimeRegistry(OllamaAdapter{}, MLXAdapter{CacheDir: cacheDir})}
	passed, _, err := EvaluateCondition(context.Background(), deps, protocol.Condition{
		Kind: protocol.CondKindModelPresent,
		ModelPresent: &protocol.ModelPresentOperand{
			Runtime:          "mlx",
			ModelRef:         modelRef,
			ResolvedRevision: revision,
		},
	})
	if err != nil {
		t.Fatalf("EvaluateCondition: %v", err)
	}
	if !passed {
		t.Error("passed = false, want true when the resolved-revision snapshot directory exists in the cache")
	}
}

// TestEvaluateConditionModelPresentMLXHonorsHFHubCacheEnvVar proves
// MLXAdapter resolves its cache location through the same
// environment.HuggingFaceCacheDir precedence internal/cognition/mlx's
// discovery adapter uses (verified from that package's own test) —
// without an explicit CacheDir, an HF_HUB_CACHE override in the process
// environment must be honored, not silently ignored in favor of the
// ~/.cache/huggingface/hub default.
func TestEvaluateConditionModelPresentMLXHonorsHFHubCacheEnvVar(t *testing.T) {
	overrideCache := t.TempDir()
	t.Setenv("HF_HUB_CACHE", overrideCache)
	modelRef := "mlx-community/Qwen2.5-Coder-7B-Instruct-4bit"
	revision := "019cc73c45c770444708a6dd8690c66243cc5c80"
	writeFakeHFSnapshot(t, overrideCache, modelRef, revision, 1024)

	deps := EvaluatorDeps{ModelRuntimes: NewModelRuntimeRegistry(OllamaAdapter{}, MLXAdapter{})} // no explicit CacheDir
	passed, detail, err := EvaluateCondition(context.Background(), deps, protocol.Condition{
		Kind: protocol.CondKindModelPresent,
		ModelPresent: &protocol.ModelPresentOperand{
			Runtime:          "mlx",
			ModelRef:         modelRef,
			ResolvedRevision: revision,
		},
	})
	if err != nil {
		t.Fatalf("EvaluateCondition: %v", err)
	}
	if !passed {
		t.Errorf("passed = false (detail: %s), want true — MLXAdapter must resolve HF_HUB_CACHE from the environment", detail)
	}
}

func TestEvaluateConditionModelPresentMLXAbsent(t *testing.T) {
	deps := EvaluatorDeps{ModelRuntimes: NewModelRuntimeRegistry(OllamaAdapter{}, MLXAdapter{CacheDir: t.TempDir()})}
	passed, _, err := EvaluateCondition(context.Background(), deps, protocol.Condition{
		Kind: protocol.CondKindModelPresent,
		ModelPresent: &protocol.ModelPresentOperand{
			Runtime:          "mlx",
			ModelRef:         "mlx-community/Qwen2.5-Coder-7B-Instruct-4bit",
			ResolvedRevision: "019cc73c45c770444708a6dd8690c66243cc5c80",
		},
	})
	if err != nil {
		t.Fatalf("EvaluateCondition: %v", err)
	}
	if passed {
		t.Error("passed = true for an empty cache directory, want false")
	}
}

func TestEvaluateConditionModelPresentRejectsUnregisteredRuntime(t *testing.T) {
	deps := EvaluatorDeps{ModelRuntimes: NewModelRuntimeRegistry(DefaultModelRuntimeAdapters()...)}
	_, _, err := EvaluateCondition(context.Background(), deps, protocol.Condition{
		Kind: protocol.CondKindModelPresent,
		ModelPresent: &protocol.ModelPresentOperand{
			Runtime:          "some-other-runtime",
			ModelRef:         "model:latest",
			ResolvedRevision: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		},
	})
	if err == nil {
		t.Fatal("EvaluateCondition succeeded for an unregistered runtime; expected a fail-closed error")
	}
}

// TestEvaluateDeviceNodeAccessible is the independent-review follow-up on
// WP-M3B-6, FIX_NOW 4: hardware driver/device-permission manual recipes
// must verify the actual remediation state (device-node existence and, when
// required, current-user read/write accessibility) rather than a proxy
// like command_available. These regressions prove the condition
// distinguishes "does not exist" from "exists but inaccessible" from
// TestEvaluateDeviceNodeAccessible tests the closed, read-only probe for
// device special files. It verifies that regular files are strictly rejected,
// nonexistent paths fail, and genuine device special files are inspected
// for existence and current-user read/write accessibility.
func TestEvaluateDeviceNodeAccessible(t *testing.T) {
	t.Run("missing node fails existence and accessibility checks", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "does-not-exist")
		passed, _, err := EvaluateCondition(context.Background(), EvaluatorDeps{}, protocol.Condition{
			Kind:                 protocol.CondKindDeviceNodeAccessible,
			DeviceNodeAccessible: &protocol.DeviceNodeOperand{Path: missing},
		})
		if err != nil {
			t.Fatalf("EvaluateCondition: %v", err)
		}
		if passed {
			t.Error("passed = true for a nonexistent device node, want false")
		}
	})

	t.Run("regular file is strictly rejected even if writable", func(t *testing.T) {
		dir := t.TempDir()
		regular := filepath.Join(dir, "not-a-device")
		if err := os.WriteFile(regular, []byte("fake"), 0666); err != nil {
			t.Fatalf("create regular file: %v", err)
		}
		// Existence-only check must reject regular file
		passed, detail, err := EvaluateCondition(context.Background(), EvaluatorDeps{}, protocol.Condition{
			Kind:                 protocol.CondKindDeviceNodeAccessible,
			DeviceNodeAccessible: &protocol.DeviceNodeOperand{Path: regular, RequireAccessible: false},
		})
		if err != nil {
			t.Fatalf("EvaluateCondition: %v", err)
		}
		if passed {
			t.Errorf("passed = true for regular file with RequireAccessible=false; want false (must reject non-device files), detail=%s", detail)
		}

		// Accessibility check must also reject regular file
		passed, detail, err = EvaluateCondition(context.Background(), EvaluatorDeps{}, protocol.Condition{
			Kind:                 protocol.CondKindDeviceNodeAccessible,
			DeviceNodeAccessible: &protocol.DeviceNodeOperand{Path: regular, RequireAccessible: true},
		})
		if err != nil {
			t.Fatalf("EvaluateCondition: %v", err)
		}
		if passed {
			t.Errorf("passed = true for regular file with RequireAccessible=true; want false (must reject non-device files), detail=%s", detail)
		}
	})

	t.Run("real character device satisfies existence and accessibility checks", func(t *testing.T) {
		const devNull = "/dev/null"
		info, err := os.Stat(devNull)
		if err != nil || info.Mode()&os.ModeDevice == 0 {
			t.Skip("/dev/null not available as a device special file on this platform")
		}

		// Existence-only on real device special file
		passed, detail, err := EvaluateCondition(context.Background(), EvaluatorDeps{}, protocol.Condition{
			Kind:                 protocol.CondKindDeviceNodeAccessible,
			DeviceNodeAccessible: &protocol.DeviceNodeOperand{Path: devNull, RequireAccessible: false},
		})
		if err != nil {
			t.Fatalf("EvaluateCondition: %v", err)
		}
		if !passed {
			t.Errorf("passed = false for %s with RequireAccessible=false, detail=%s", devNull, detail)
		}

		// Read/write accessibility on /dev/null
		passed, detail, err = EvaluateCondition(context.Background(), EvaluatorDeps{}, protocol.Condition{
			Kind:                 protocol.CondKindDeviceNodeAccessible,
			DeviceNodeAccessible: &protocol.DeviceNodeOperand{Path: devNull, RequireAccessible: true},
		})
		if err != nil {
			t.Fatalf("EvaluateCondition: %v", err)
		}
		if !passed {
			t.Errorf("passed = false for %s with RequireAccessible=true, detail=%s", devNull, detail)
		}
	})

	t.Run("inaccessible device special file fails RequireAccessible when permission denied", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("running as root: root has ambient access to restricted device nodes")
		}
		// Look for an existing device node in /dev that non-root cannot open for read/write
		candidates := []string{"/dev/mem", "/dev/kmem", "/dev/port", "/dev/nvram", "/dev/kmsg", "/dev/tty0"}
		var restrictedNode string
		for _, c := range candidates {
			info, err := os.Stat(c)
			if err == nil && info.Mode()&os.ModeDevice != 0 {
				if f, openErr := os.OpenFile(c, os.O_RDWR, 0); openErr != nil {
					restrictedNode = c
					break
				} else {
					_ = f.Close()
				}
			}
		}
		if restrictedNode == "" {
			t.Skip("no restricted device node found in test environment to test permission denial")
		}

		// Existence should pass because it IS a device special file
		passed, detail, err := EvaluateCondition(context.Background(), EvaluatorDeps{}, protocol.Condition{
			Kind:                 protocol.CondKindDeviceNodeAccessible,
			DeviceNodeAccessible: &protocol.DeviceNodeOperand{Path: restrictedNode, RequireAccessible: false},
		})
		if err != nil {
			t.Fatalf("EvaluateCondition: %v", err)
		}
		if !passed {
			t.Errorf("passed = false for existing device node %s with RequireAccessible=false, detail=%s", restrictedNode, detail)
		}

		// But RequireAccessible must fail because the current user cannot open it
		passed, detail, err = EvaluateCondition(context.Background(), EvaluatorDeps{}, protocol.Condition{
			Kind:                 protocol.CondKindDeviceNodeAccessible,
			DeviceNodeAccessible: &protocol.DeviceNodeOperand{Path: restrictedNode, RequireAccessible: true},
		})
		if err != nil {
			t.Fatalf("EvaluateCondition: %v", err)
		}
		if passed {
			t.Errorf("passed = true for restricted device node %s with RequireAccessible=true; want false, detail=%s", restrictedNode, detail)
		}
	})
}

// TestEvaluateKernelDriverBound verifies the typed kernel_driver_bound condition.
// It verifies that driver binding is distinguished from device presence, and that
// unbound/wrong-driver states fail while bound states pass.
func TestEvaluateKernelDriverBound(t *testing.T) {
	sysfsRoot := t.TempDir()
	checker := &SysfsDriverChecker{SysfsDevicesDir: sysfsRoot}
	deps := EvaluatorDeps{DeviceDriver: checker}

	const deviceID = "pci:0000:01:00.0"
	slotDir := filepath.Join(sysfsRoot, "0000:01:00.0")

	t.Run("missing device in sysfs fails", func(t *testing.T) {
		passed, detail, err := EvaluateCondition(context.Background(), deps, protocol.Condition{
			Kind: protocol.CondKindKernelDriverBound,
			KernelDriverBound: &protocol.KernelDriverBoundOperand{
				DeviceID: deviceID,
				Driver:   "nvidia",
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if passed {
			t.Errorf("passed = true for nonexistent sysfs device, detail=%s", detail)
		}
	})

	t.Run("device present but no driver bound fails: pre-remediation state", func(t *testing.T) {
		if err := os.MkdirAll(slotDir, 0755); err != nil {
			t.Fatalf("mkdir slot: %v", err)
		}
		uevent := filepath.Join(slotDir, "uevent")
		if err := os.WriteFile(uevent, []byte("PCI_CLASS=30000\nPCI_ID=10DE:2204\n"), 0644); err != nil {
			t.Fatalf("write uevent: %v", err)
		}

		passed, detail, err := EvaluateCondition(context.Background(), deps, protocol.Condition{
			Kind: protocol.CondKindKernelDriverBound,
			KernelDriverBound: &protocol.KernelDriverBoundOperand{
				DeviceID: deviceID,
				Driver:   "nvidia",
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if passed {
			t.Errorf("passed = true for unbound device, want false; detail=%s", detail)
		}
	})

	t.Run("device has different driver bound fails", func(t *testing.T) {
		uevent := filepath.Join(slotDir, "uevent")
		if err := os.WriteFile(uevent, []byte("DRIVER=nouveau\nPCI_CLASS=30000\n"), 0644); err != nil {
			t.Fatalf("write uevent: %v", err)
		}

		passed, detail, err := EvaluateCondition(context.Background(), deps, protocol.Condition{
			Kind: protocol.CondKindKernelDriverBound,
			KernelDriverBound: &protocol.KernelDriverBoundOperand{
				DeviceID: deviceID,
				Driver:   "nvidia",
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if passed {
			t.Errorf("passed = true when driver is nouveau, want false for nvidia; detail=%s", detail)
		}
	})

	t.Run("device has expected driver bound succeeds: corrected remediation state", func(t *testing.T) {
		uevent := filepath.Join(slotDir, "uevent")
		if err := os.WriteFile(uevent, []byte("DRIVER=nvidia\nPCI_CLASS=30000\n"), 0644); err != nil {
			t.Fatalf("write uevent: %v", err)
		}

		passed, detail, err := EvaluateCondition(context.Background(), deps, protocol.Condition{
			Kind: protocol.CondKindKernelDriverBound,
			KernelDriverBound: &protocol.KernelDriverBoundOperand{
				DeviceID: deviceID,
				Driver:   "nvidia",
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !passed {
			t.Errorf("passed = false when driver is nvidia; detail=%s", detail)
		}

		// Also check nvidia_drm alias for nvidia
		if err := os.WriteFile(uevent, []byte("DRIVER=nvidia_drm\nPCI_CLASS=30000\n"), 0644); err != nil {
			t.Fatalf("write uevent: %v", err)
		}
		passed, detail, err = EvaluateCondition(context.Background(), deps, protocol.Condition{
			Kind: protocol.CondKindKernelDriverBound,
			KernelDriverBound: &protocol.KernelDriverBoundOperand{
				DeviceID: deviceID,
				Driver:   "nvidia",
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !passed {
			t.Errorf("passed = false when driver is nvidia_drm; detail=%s", detail)
		}
	})

	t.Run("AMD ROCm driver bound succeeds", func(t *testing.T) {
		const amdDeviceID = "pci:0000:06:00.0"
		amdSlotDir := filepath.Join(sysfsRoot, "0000:06:00.0")
		if err := os.MkdirAll(amdSlotDir, 0755); err != nil {
			t.Fatalf("mkdir amd slot: %v", err)
		}
		uevent := filepath.Join(amdSlotDir, "uevent")
		if err := os.WriteFile(uevent, []byte("DRIVER=amdgpu\nPCI_CLASS=30000\n"), 0644); err != nil {
			t.Fatalf("write uevent: %v", err)
		}

		passed, detail, err := EvaluateCondition(context.Background(), deps, protocol.Condition{
			Kind: protocol.CondKindKernelDriverBound,
			KernelDriverBound: &protocol.KernelDriverBoundOperand{
				DeviceID: amdDeviceID,
				Driver:   "amdgpu",
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !passed {
			t.Errorf("passed = false when driver is amdgpu; detail=%s", detail)
		}
	})
}
