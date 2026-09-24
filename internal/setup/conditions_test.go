package setup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
