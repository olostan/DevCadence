package setup

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/artifacts"
	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
)

func applierTestDeps(t *testing.T) (ApplierDeps, string) {
	t.Helper()
	home := t.TempDir()
	if err := EnsureLayout(home); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}
	cache, err := NewCacheManager(filepath.Join(home, "state"), clock.NewFake(time.Now(), 0), 0)
	if err != nil {
		t.Fatalf("NewCacheManager: %v", err)
	}
	store, err := artifacts.NewStore(filepath.Join(home, "artifacts", "setup"), ids.NewSequential())
	if err != nil {
		t.Fatalf("artifacts.NewStore: %v", err)
	}
	return ApplierDeps{
		Runner:    &fakeCommandRunner{results: map[string]process.Result{}},
		Home:      home,
		Cache:     cache,
		Artifacts: store,
	}, home
}

func TestApplyOperationCreateDirectory(t *testing.T) {
	deps, home := applierTestDeps(t)
	op := protocol.TypedOperation{
		Kind: protocol.OpKindCreateDirectory,
		CreateDirectory: &protocol.CreateDirectoryParams{
			Location:    protocol.LocationTmp,
			FileModeOct: "0700",
		},
	}
	mutated, _, procResult, artifact, err := ApplyOperation(context.Background(), deps, op, true)
	if err != nil {
		t.Fatalf("ApplyOperation: %v", err)
	}
	if !mutated {
		t.Error("mutated = false, want true")
	}
	if procResult != nil {
		t.Errorf("procResult = %+v, want nil (create_directory never runs a subprocess)", procResult)
	}
	if artifact != nil {
		t.Errorf("artifact = %+v, want nil", artifact)
	}

	passed, _, err := EvaluateCondition(context.Background(), EvaluatorDeps{Home: home}, protocol.Condition{
		Kind:             protocol.CondKindManagedDirExists,
		ManagedDirExists: &protocol.ManagedDirOperand{Location: protocol.LocationTmp, FileModeOct: "0700"},
	})
	if err != nil {
		t.Fatalf("EvaluateCondition: %v", err)
	}
	if !passed {
		t.Error("managed_dir_exists did not pass after create_directory applied")
	}
}

func TestApplyOperationWriteManagedConfig(t *testing.T) {
	deps, home := applierTestDeps(t)
	op := protocol.TypedOperation{
		Kind: protocol.OpKindWriteManagedConfig,
		WriteManagedConfig: &protocol.WriteManagedConfigParams{
			Key:   protocol.ConfigKeyLogVerbosity,
			Value: "debug",
		},
	}
	mutated, detail, procResult, artifact, err := ApplyOperation(context.Background(), deps, op, true)
	if err != nil {
		t.Fatalf("ApplyOperation: %v", err)
	}
	if !mutated {
		t.Errorf("mutated = false, want true (detail: %s)", detail)
	}
	if procResult != nil || artifact != nil {
		t.Errorf("procResult=%+v artifact=%+v, want both nil", procResult, artifact)
	}

	cfg, err := ReadManagedConfig(home)
	if err != nil {
		t.Fatalf("ReadManagedConfig: %v", err)
	}
	if cfg[protocol.ConfigKeyLogVerbosity] != "debug" {
		t.Errorf("cfg[log_verbosity] = %q, want \"debug\"", cfg[protocol.ConfigKeyLogVerbosity])
	}
}

func TestApplyOperationWriteManagedConfigRejectsInvalidValue(t *testing.T) {
	deps, _ := applierTestDeps(t)
	op := protocol.TypedOperation{
		Kind: protocol.OpKindWriteManagedConfig,
		WriteManagedConfig: &protocol.WriteManagedConfigParams{
			Key:   protocol.ConfigKeyLogVerbosity,
			Value: "not-a-real-level",
		},
	}
	if _, _, _, _, err := ApplyOperation(context.Background(), deps, op, true); err == nil {
		t.Fatal("ApplyOperation accepted an invalid managed-config value; expected an error")
	}
}

func TestApplyOperationRemoveStaleCache(t *testing.T) {
	deps, home := applierTestDeps(t)
	ctx := context.Background()

	// Write a cache entry, then confirm remove_stale_cache actually evicts it.
	if err := Write(ctx, deps.Cache, protocol.CacheTargetEndpointProbes, testMachineFingerprint, map[string]string{"a": "b"}, time.Hour); err != nil {
		t.Fatalf("Write cache entry: %v", err)
	}
	if _, found, err := Read[map[string]string](ctx, deps.Cache, protocol.CacheTargetEndpointProbes, testMachineFingerprint); err != nil || !found {
		t.Fatalf("Read cache entry before removal: found=%v err=%v", found, err)
	}

	op := protocol.TypedOperation{
		Kind:             protocol.OpKindRemoveStaleCache,
		RemoveStaleCache: &protocol.RemoveStaleCacheParams{Target: protocol.CacheTargetEndpointProbes},
	}
	mutated, _, _, _, err := ApplyOperation(ctx, deps, op, true)
	if err != nil {
		t.Fatalf("ApplyOperation: %v", err)
	}
	if !mutated {
		t.Error("mutated = false, want true")
	}

	if _, found, err := Read[map[string]string](ctx, deps.Cache, protocol.CacheTargetEndpointProbes, testMachineFingerprint); err != nil || found {
		t.Fatalf("cache entry still present after remove_stale_cache: found=%v err=%v", found, err)
	}
	_ = home
}

func TestApplyOperationRunDiagnosticCheckGitAvailable(t *testing.T) {
	deps, _ := applierTestDeps(t)
	op := protocol.TypedOperation{
		Kind:               protocol.OpKindRunDiagnosticCheck,
		RunDiagnosticCheck: &protocol.RunDiagnosticCheckParams{CheckName: protocol.CheckGitAvailable},
	}
	mutated, detail, procResult, artifact, err := ApplyOperation(context.Background(), deps, op, true)
	if err != nil {
		t.Fatalf("ApplyOperation: %v", err)
	}
	if mutated {
		t.Error("mutated = true, want false (diagnostic checks never mutate)")
	}
	if procResult != nil || artifact != nil {
		t.Errorf("procResult=%+v artifact=%+v, want both nil for git_available (LookPath, no subprocess)", procResult, artifact)
	}
	if detail == "" {
		t.Error("detail is empty, want a description of the check result")
	}
}

func TestApplyOperationOllamaPullModel(t *testing.T) {
	deps, _ := applierTestDeps(t)
	deps.Runner.(*fakeCommandRunner).results["ollama"] = process.Result{
		Status: process.StatusCompleted, ExitCode: 0, Stdout: []byte("pulling manifest\nsuccess\n"),
	}
	op := protocol.TypedOperation{
		Kind: protocol.OpKindOllamaPullModel,
		OllamaPullModel: &protocol.OllamaPullModelParams{
			ModelTag:            "smollm:135m",
			ResolvedDigest:      "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			ExpectedSizeBytes:   145000000,
			AllowedRegistryHost: "registry.ollama.ai",
			LicenseReference:    "apache-2.0",
		},
	}
	mutated, _, procResult, artifact, err := ApplyOperation(context.Background(), deps, op, true)
	if err != nil {
		t.Fatalf("ApplyOperation: %v", err)
	}
	if !mutated {
		t.Error("mutated = false, want true for a successful pull")
	}
	if procResult == nil {
		t.Fatal("procResult is nil, want the subprocess result for ollama_pull_model")
	}
	if artifact == nil {
		t.Error("artifact is nil, want captured output")
	}

	runner := deps.Runner.(*fakeCommandRunner)
	if len(runner.calls) != 1 || runner.calls[0].Executable != "ollama" || len(runner.calls[0].Args) != 2 || runner.calls[0].Args[0] != "pull" {
		t.Errorf("runner.calls = %+v, want one `ollama pull <tag>` call", runner.calls)
	}
}

func TestApplyOperationRejectsUnhandledKind(t *testing.T) {
	deps, _ := applierTestDeps(t)
	if _, _, _, _, err := ApplyOperation(context.Background(), deps, protocol.TypedOperation{Kind: protocol.OperationKind("bogus")}, true); err == nil {
		t.Fatal("ApplyOperation accepted an unhandled operation kind; expected an error")
	}
}
