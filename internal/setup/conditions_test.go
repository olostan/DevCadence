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

func (s stubEndpointAuthChecker) CheckEndpointAuthenticated(ctx context.Context, endpointID string) (bool, string, error) {
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
		EndpointAuthenticated: &protocol.EndpointOperand{EndpointID: "ep-001"},
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
		EndpointAuthenticated: &protocol.EndpointOperand{EndpointID: "ep-001"},
	})
	if err != nil {
		t.Fatalf("EvaluateCondition: %v", err)
	}
	if !passed || detail != "logged in" {
		t.Errorf("passed=%v detail=%q, want true/\"logged in\"", passed, detail)
	}
}

func TestCredentialsEndpointAuthChecker_EvaluatesAuthViaCredentialManager(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFake(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC), 0)
	adapterClaude := &credentials.StaticCLIAuthAdapter{
		ID:     "adapter-claude",
		Target: "claude",
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
		CLIAdapters: []credentials.CLISessionAuthAdapter{adapterClaude, adapterCodex},
	})
	if err != nil {
		t.Fatalf("credentials.NewManager: %v", err)
	}

	checker := NewCredentialsEndpointAuthChecker(mgr)

	t.Run("authenticated endpoint with cli: prefix", func(t *testing.T) {
		authed, detail, err := checker.CheckEndpointAuthenticated(ctx, "cli:claude")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !authed || detail != "claude session active" {
			t.Errorf("authed=%v detail=%q, want true/\"claude session active\"", authed, detail)
		}
	})

	t.Run("authenticated endpoint bare handle", func(t *testing.T) {
		authed, detail, err := checker.CheckEndpointAuthenticated(ctx, "claude")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !authed || detail != "claude session active" {
			t.Errorf("authed=%v detail=%q, want true/\"claude session active\"", authed, detail)
		}
	})

	t.Run("unauthenticated endpoint", func(t *testing.T) {
		authed, detail, err := checker.CheckEndpointAuthenticated(ctx, "cli:codex")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if authed || detail != "codex session unauthenticated" {
			t.Errorf("authed=%v detail=%q, want false/\"codex session unauthenticated\"", authed, detail)
		}
	})

	t.Run("unrecognized endpoint", func(t *testing.T) {
		authed, _, err := checker.CheckEndpointAuthenticated(ctx, "cli:unknown-cli")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if authed {
			t.Errorf("authed=%v, want false for unknown endpoint", authed)
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
		// EndpointAuth is intentionally nil to prove NewExecutor wires it via CredentialManager
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	conds := []protocol.Condition{
		{
			Kind:                  protocol.CondKindEndpointAuthenticated,
			EndpointAuthenticated: &protocol.EndpointOperand{EndpointID: "cli:claude"},
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
