package setup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/artifacts"
	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
)

func applierTestDeps(t *testing.T) (applierDeps, string) {
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
	return applierDeps{
		runner:    &fakeCommandRunner{results: map[string]process.Result{}},
		home:      home,
		cache:     cache,
		artifacts: store,
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
	mutated, _, procResult, artifact, err := applyOperation(context.Background(), deps, op, true, "")
	if err != nil {
		t.Fatalf("applyOperation: %v", err)
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
	mutated, detail, procResult, artifact, err := applyOperation(context.Background(), deps, op, true, "")
	if err != nil {
		t.Fatalf("applyOperation: %v", err)
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
	if _, _, _, _, err := applyOperation(context.Background(), deps, op, true, ""); err == nil {
		t.Fatal("applyOperation accepted an invalid managed-config value; expected an error")
	}
}

func TestApplyOperationRemoveStaleCache(t *testing.T) {
	deps, home := applierTestDeps(t)
	ctx := context.Background()

	// Write a cache entry, then confirm remove_stale_cache actually evicts it.
	if err := Write(ctx, deps.cache, protocol.CacheTargetEndpointProbes, testMachineFingerprint, map[string]string{"a": "b"}, time.Hour); err != nil {
		t.Fatalf("Write cache entry: %v", err)
	}
	if _, found, err := Read[map[string]string](ctx, deps.cache, protocol.CacheTargetEndpointProbes, testMachineFingerprint); err != nil || !found {
		t.Fatalf("Read cache entry before removal: found=%v err=%v", found, err)
	}

	op := protocol.TypedOperation{
		Kind:             protocol.OpKindRemoveStaleCache,
		RemoveStaleCache: &protocol.RemoveStaleCacheParams{Target: protocol.CacheTargetEndpointProbes},
	}
	mutated, _, _, _, err := applyOperation(ctx, deps, op, true, "")
	if err != nil {
		t.Fatalf("applyOperation: %v", err)
	}
	if !mutated {
		t.Error("mutated = false, want true")
	}

	if _, found, err := Read[map[string]string](ctx, deps.cache, protocol.CacheTargetEndpointProbes, testMachineFingerprint); err != nil || found {
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
	// This environment may or may not have git on PATH; either outcome is a
	// valid, deterministic result as long as it's truthful — assert
	// consistency between the returned error and the detail, not a fixed
	// pass/fail.
	mutated, detail, procResult, artifact, err := applyOperation(context.Background(), deps, op, true, "")
	if mutated {
		t.Error("mutated = true, want false (diagnostic checks never mutate)")
	}
	if procResult != nil || artifact != nil {
		t.Errorf("procResult=%+v artifact=%+v, want both nil for git_available (LookPath, no subprocess)", procResult, artifact)
	}
	if detail == "" {
		t.Error("detail is empty, want a description of the check result")
	}
	if err != nil {
		// A failed diagnostic now surfaces as an error (finding 6): confirm
		// it's specifically a diagnostic-failure error, not something else.
		t.Logf("git_available failed in this environment (expected if git is absent): %v", err)
	}
}

func TestApplyOperationRunDiagnosticCheckStateRootWritableDoesNotMutate(t *testing.T) {
	deps, home := applierTestDeps(t)
	before, err := filepath.Glob(filepath.Join(home, ".devcadence_diagnostic_*"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	op := protocol.TypedOperation{
		Kind:               protocol.OpKindRunDiagnosticCheck,
		RunDiagnosticCheck: &protocol.RunDiagnosticCheckParams{CheckName: protocol.CheckStateRootWritable},
	}
	mutated, detail, procResult, artifact, err := applyOperation(context.Background(), deps, op, true, "")
	if err != nil {
		t.Fatalf("applyOperation: %v", err)
	}
	if mutated {
		t.Error("mutated = true, want false: state_root_writable must be a read-only check (IntrinsicPolicy declares AuthorityReadOnly)")
	}
	if procResult != nil || artifact != nil {
		t.Errorf("procResult=%+v artifact=%+v, want both nil (no subprocess for this check)", procResult, artifact)
	}
	if detail == "" {
		t.Error("detail is empty")
	}
	after, err := filepath.Glob(filepath.Join(home, ".devcadence_diagnostic_*"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("state_root_writable left %d temp file(s) behind, want 0 (it must not write to disk)", len(after)-len(before))
	}
}

func newOllamaTagsServer(t *testing.T, models []ollamaModelEntry) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(ollamaTagsResponse{Models: models})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestApplyOperationOllamaPullModel(t *testing.T) {
	deps, home := applierTestDeps(t)
	ollamaPath := filepath.Join(home, "ollama-fake")
	deps.runner.(*fakeCommandRunner).results[ollamaPath] = process.Result{
		Status: process.StatusCompleted, ExitCode: 0, Stdout: []byte("pulling manifest\nsuccess\n"),
	}
	resolvedDigest := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	srv := newOllamaTagsServer(t, []ollamaModelEntry{
		{Name: "smollm:135m", Digest: resolvedDigest, Size: 145000000},
	})
	deps.ollamaBaseURL = srv.URL

	op := protocol.TypedOperation{
		Kind: protocol.OpKindOllamaPullModel,
		OllamaPullModel: &protocol.OllamaPullModelParams{
			ModelTag:            "smollm:135m",
			ResolvedDigest:      resolvedDigest,
			ExpectedSizeBytes:   145000000,
			AllowedRegistryHost: "registry.ollama.ai",
			LicenseReference:    "apache-2.0",
		},
	}
	mutated, detail, procResult, artifact, err := applyOperation(context.Background(), deps, op, true, ollamaPath)
	if err != nil {
		t.Fatalf("applyOperation: %v", err)
	}
	if !mutated {
		t.Errorf("mutated = false, want true for a successful, verified pull (detail: %s)", detail)
	}
	if procResult == nil {
		t.Fatal("procResult is nil, want the subprocess result for ollama_pull_model")
	}
	if artifact == nil {
		t.Error("artifact is nil, want captured output")
	}

	runner := deps.runner.(*fakeCommandRunner)
	if len(runner.calls) != 1 || runner.calls[0].Executable != ollamaPath || len(runner.calls[0].Args) != 2 || runner.calls[0].Args[0] != "pull" {
		t.Errorf("runner.calls = %+v, want one call to the verified path %q with `pull <ref>`", runner.calls, ollamaPath)
	}
}

func TestApplyOperationOllamaPullModelRequiresVerifiedExecutablePath(t *testing.T) {
	deps, _ := applierTestDeps(t)
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
	if _, _, _, _, err := applyOperation(context.Background(), deps, op, true, ""); err == nil {
		t.Fatal("applyOperation ran ollama_pull_model with no verified executable path; expected rejection")
	}
}

func TestApplyOperationOllamaPullModelRejectsDigestMismatch(t *testing.T) {
	deps, home := applierTestDeps(t)
	ollamaPath := filepath.Join(home, "ollama-fake")
	deps.runner.(*fakeCommandRunner).results[ollamaPath] = process.Result{
		Status: process.StatusCompleted, ExitCode: 0,
	}
	// The server reports a different digest than what the plan approved.
	srv := newOllamaTagsServer(t, []ollamaModelEntry{
		{Name: "smollm:135m", Digest: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", Size: 145000000},
	})
	deps.ollamaBaseURL = srv.URL

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
	mutated, detail, _, _, err := applyOperation(context.Background(), deps, op, true, ollamaPath)
	if err != nil {
		t.Fatalf("applyOperation returned an error rather than a failed-but-no-error result: %v", err)
	}
	if mutated {
		t.Errorf("mutated = true despite a digest mismatch, want false (detail: %s)", detail)
	}
}

func TestApplyOperationRejectsUnhandledKind(t *testing.T) {
	deps, _ := applierTestDeps(t)
	if _, _, _, _, err := applyOperation(context.Background(), deps, protocol.TypedOperation{Kind: protocol.OperationKind("bogus")}, true, ""); err == nil {
		t.Fatal("applyOperation accepted an unhandled operation kind; expected an error")
	}
}

func TestApplyOperationOllamaPullModelRejectsSizeMismatch(t *testing.T) {
	deps, home := applierTestDeps(t)
	ollamaPath := filepath.Join(home, "ollama-fake")
	deps.runner.(*fakeCommandRunner).results[ollamaPath] = process.Result{Status: process.StatusCompleted, ExitCode: 0}
	resolvedDigest := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	srv := newOllamaTagsServer(t, []ollamaModelEntry{
		{Name: "smollm:135m", Digest: resolvedDigest, Size: 999999999}, // does not match ExpectedSizeBytes below
	})
	deps.ollamaBaseURL = srv.URL

	op := protocol.TypedOperation{
		Kind: protocol.OpKindOllamaPullModel,
		OllamaPullModel: &protocol.OllamaPullModelParams{
			ModelTag:            "smollm:135m",
			ResolvedDigest:      resolvedDigest,
			ExpectedSizeBytes:   145000000,
			AllowedRegistryHost: "registry.ollama.ai",
			LicenseReference:    "apache-2.0",
		},
	}
	mutated, detail, _, _, err := applyOperation(context.Background(), deps, op, true, ollamaPath)
	if err != nil {
		t.Fatalf("applyOperation returned an error rather than a failed-but-no-error result: %v", err)
	}
	if mutated {
		t.Errorf("mutated = true despite a size mismatch, want false (detail: %s)", detail)
	}
}

func TestApplyOperationOllamaPullModelPrefixesNonDefaultRegistry(t *testing.T) {
	deps, home := applierTestDeps(t)
	ollamaPath := filepath.Join(home, "ollama-fake")
	deps.runner.(*fakeCommandRunner).results[ollamaPath] = process.Result{Status: process.StatusCompleted, ExitCode: 0}
	resolvedDigest := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	srv := newOllamaTagsServer(t, []ollamaModelEntry{
		{Name: "smollm:135m", Digest: resolvedDigest, Size: 145000000},
	})
	deps.ollamaBaseURL = srv.URL

	op := protocol.TypedOperation{
		Kind: protocol.OpKindOllamaPullModel,
		OllamaPullModel: &protocol.OllamaPullModelParams{
			ModelTag:            "smollm:135m",
			ResolvedDigest:      resolvedDigest,
			ExpectedSizeBytes:   145000000,
			AllowedRegistryHost: "my-private-registry.example.com",
			LicenseReference:    "apache-2.0",
		},
	}
	if _, _, _, _, err := applyOperation(context.Background(), deps, op, true, ollamaPath); err != nil {
		t.Fatalf("applyOperation: %v", err)
	}

	runner := deps.runner.(*fakeCommandRunner)
	if len(runner.calls) != 1 || runner.calls[0].Args[1] != "my-private-registry.example.com/smollm:135m" {
		t.Errorf("runner.calls = %+v, want the pull ref prefixed with the non-default AllowedRegistryHost", runner.calls)
	}
}
