package setup

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/olostan/DevCadence/internal/environment"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
)

// mlxDownloadTimeout mirrors ollamaPullTimeout: model downloads are large,
// so this is a plan action actually running, not a bounded
// evidence-gathering probe.
const mlxDownloadTimeout = ollamaPullTimeout

// MLXAdapter implements LocalModelRuntimeAdapter for the MLX-LM local
// runtime. It downloads models via the Hugging Face Hub CLI (mlx-lm's own
// model distribution path) but verifies presence and enforces the
// approved supply-chain fields (ExpectedSizeBytes, AllowedSource) by
// reading the local Hugging Face Hub cache directly from disk — not by
// re-invoking the CLI — so verification never depends on ambient PATH
// resolution or on a CLI flag whose existence this package cannot pin
// (see ModelPresent's doc comment). It is registered as an equal peer of
// OllamaAdapter — ModelRuntimeRegistry treats both symmetrically, and
// neither this package nor internal/protocol privileges one runtime's
// name over the other (INVARIANTS.md DCI-055).
//
// CLI note: this adapter invokes the "hf" CLI, not the older
// "huggingface-cli" name — huggingface_hub v1.0 removed "huggingface-cli"
// (https://huggingface.co/docs/huggingface_hub/concepts/migration).
// "hf download" has no documented "--local-files-only" flag
// (https://huggingface.co/docs/huggingface_hub/package_reference/cli), so
// presence/size verification does not attempt to rely on one — it reads
// the resulting on-disk cache instead, which is both more robust to CLI
// surface changes and (per ModelPresent's doc comment) more trustworthy.
type MLXAdapter struct {
	// CacheDir overrides the Hugging Face Hub cache root this adapter
	// reads from AND passes to `hf download --cache-dir` for the actual
	// download — the same resolved value binds both, so the two can never
	// disagree about where the cache is (see EnsureModel's doc comment).
	// Empty means the real default resolved by
	// environment.HuggingFaceCacheDir (HF_HUB_CACHE, then HF_HOME/hub,
	// then XDG_CACHE_HOME/huggingface/hub, then
	// ~/.cache/huggingface/hub) — the same resolver
	// internal/cognition/mlx's discovery Adapter uses, so setup and
	// cognition can never disagree about where "the" cache is. Tests set
	// this to a t.TempDir().
	CacheDir string
}

// Runtime implements LocalModelRuntimeAdapter.
func (MLXAdapter) Runtime() string { return "mlx" }

// cacheDir resolves the Hugging Face Hub cache root this adapter reads
// from and downloads into — see environment.HuggingFaceCacheDir's doc
// comment for the precedence and why it is shared with
// internal/cognition/mlx rather than duplicated here.
func (a MLXAdapter) cacheDir() (string, error) {
	if a.CacheDir != "" {
		return a.CacheDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errs.Wrap(errs.CategoryInternal, err, "MLXAdapter: resolve user home directory for the default Hugging Face cache")
	}
	return environment.HuggingFaceCacheDir("", os.Getenv, home), nil
}

// hfRepoCacheName converts a Hugging Face repo id ("org/repo") into the
// Hub cache's directory-naming convention ("models--org--repo").
func hfRepoCacheName(modelRef string) string {
	return "models--" + strings.ReplaceAll(modelRef, "/", "--")
}

// hfSnapshotDir returns the path the Hugging Face Hub cache stores a
// resolved revision's snapshot under, given cacheDir (from (MLXAdapter).cacheDir).
func hfSnapshotDir(cacheDir, modelRef, resolvedRevision string) string {
	return filepath.Join(cacheDir, hfRepoCacheName(modelRef), "snapshots", resolvedRevision)
}

// hfSnapshotSize walks a resolved snapshot directory and sums the real
// (symlink-resolved) size of every file in it. The Hugging Face Hub cache
// stores a snapshot as a tree of symlinks into a shared blob store, so a
// plain directory-size walk without following symlinks would undercount;
// this follows exactly one level of symlink (blob files are never
// symlinks themselves) via os.Stat rather than os.Lstat.
func hfSnapshotSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, statErr := os.Stat(path) // follows symlinks, unlike d.Info()
		if statErr != nil {
			return statErr
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}

// ModelPresent implements LocalModelRuntimeAdapter as a read-only,
// filesystem-only check: op.ResolvedRevision names an immutable commit
// (see internal/setup/planner.go's isImmutableHFRevision, enforced before
// any automated action referencing this adapter can be built), so the
// Hugging Face Hub cache's snapshot directory for that exact revision
// either exists or it does not — there is no ambiguity to resolve by
// asking a CLI. This deliberately never spawns a subprocess: an earlier
// revision of this adapter shelled out to a bare "huggingface-cli"/"hf"
// resolved from ambient PATH for this check, which meant a different
// binary placed on PATH could supply the evidence Executor.Recover uses
// to decide whether an interrupted mutating action actually succeeded —
// the same PATH-substitution class of problem ADR-0014 §1 already
// requires EnsureModel's own mutation to avoid. Reading a fixed,
// well-known cache path from disk has no such trust dependency.
// A snapshot directory existing is not, by itself, proof that every file
// in it actually landed — an interrupted download can leave a partial
// snapshot behind. Because this condition is also what Executor.Recover
// uses to decide whether an interrupted ensure_local_model action actually
// succeeded, presence alone is not strong enough evidence for that use:
// when op.ExpectedSizeBytes is non-zero (the plan approved a specific
// size — see SetupAction.Validate()'s structural binding, which requires
// this to match the operation's own ExpectedSizeBytes), this also measures
// the snapshot's real total size and rejects a mismatch, the same way
// EnsureModel's own post-download check already does.
func (a MLXAdapter) ModelPresent(_ context.Context, deps EvaluatorDeps, op *protocol.ModelPresentOperand) (bool, string, error) {
	dir, err := a.cacheDir()
	if err != nil {
		return false, "", err
	}
	snapshot := hfSnapshotDir(dir, op.ModelRef, op.ResolvedRevision)
	info, statErr := os.Stat(snapshot)
	if statErr != nil {
		return false, fmt.Sprintf("%s@%s not present in the local Hugging Face cache (%s)", op.ModelRef, op.ResolvedRevision, snapshot), nil
	}
	if !info.IsDir() {
		return false, fmt.Sprintf("%s exists but is not a directory", snapshot), nil
	}
	if op.ExpectedSizeBytes > 0 {
		size, sizeErr := hfSnapshotSize(snapshot)
		if sizeErr != nil {
			return false, "", sizeErr
		}
		if size != op.ExpectedSizeBytes {
			return false, fmt.Sprintf("%s@%s present but incomplete: measured %d bytes, expected %d", op.ModelRef, op.ResolvedRevision, size, op.ExpectedSizeBytes), nil
		}
	}
	return true, fmt.Sprintf("%s@%s present in the local Hugging Face cache", op.ModelRef, op.ResolvedRevision), nil
}

// EnsureModel implements LocalModelRuntimeAdapter by downloading the model
// via the "hf" CLI, then verifying the result directly from the on-disk
// cache (never by re-invoking the CLI — see this type's doc comment):
// the resolved-revision snapshot directory must exist (which, since
// ResolvedRevision is enforced to be an immutable commit hash, is itself
// an identity check — see ModelPresent), its total size must match
// ExpectedSizeBytes when one was approved, and AllowedSource must name
// the only source this adapter ever pulls from. This is the same
// bounded-supply-chain shape as OllamaAdapter.EnsureModel (ADR-0014 §7),
// adapted to what this runtime can actually verify: the Hugging Face Hub
// CLI has no documented per-file content-digest surface equivalent to
// Ollama's registry digest, so identity is bound by (immutable revision +
// measured total size) instead.
func (a MLXAdapter) EnsureModel(ctx context.Context, deps applierDeps, p *protocol.EnsureLocalModelParams, captureOutput bool, verifiedExecutablePath string) (bool, string, *process.Result, *protocol.ArtifactRef, error) {
	if deps.runner == nil {
		return false, "", nil, nil, errs.New(errs.CategoryInvalidArgument, "MLXAdapter.EnsureModel: requires a CommandRunner")
	}
	// The exact binary run here must be the one an executable_verified
	// precondition already checked — never a bare name re-resolved from
	// PATH, the same PATH-substitution defense OllamaAdapter.EnsureModel
	// applies (ADR-0014 §1).
	if verifiedExecutablePath == "" {
		return false, "", nil, nil, errs.New(errs.CategoryPolicyDenied,
			"MLXAdapter.EnsureModel: requires an executable_verified precondition binding the exact hf CLI binary to run")
	}
	if p.AllowedSource != "huggingface.co" {
		return false, "", nil, nil, errs.New(errs.CategoryPolicyDenied,
			"MLXAdapter.EnsureModel: allowed_source %q is not a source this adapter can pull from (only \"huggingface.co\")", p.AllowedSource)
	}

	// The cache directory is resolved exactly once and passed explicitly
	// to the subprocess via --cache-dir, rather than relying on hf itself
	// to derive the same path this adapter's own verification reads from.
	// process.BaseEnv() deliberately passes only PATH/HOME/locale — not
	// HF_HOME/HF_HUB_CACHE — so without --cache-dir the subprocess could
	// resolve a different cache than a.cacheDir() (e.g. this adapter
	// configured with an explicit CacheDir, or a daemon environment with
	// HF_HOME set but the subprocess env not inheriting it), letting a
	// real download succeed while verification looks in the wrong place
	// and reports the model missing.
	cacheDir, cacheErr := a.cacheDir()
	if cacheErr != nil {
		return false, "", nil, nil, cacheErr
	}

	spec := process.Spec{
		Executable: verifiedExecutablePath,
		Args:       []string{"download", p.ModelRef, "--revision", p.ResolvedRevision, "--cache-dir", cacheDir},
		Dir:        homeOrTemp(deps.home),
		Env:        process.BaseEnv(),
		Timeout:    mlxDownloadTimeout,
	}
	result, err := deps.runner.Run(ctx, spec)
	if err != nil {
		return false, "", nil, nil, err
	}
	artifact, artErr := maybeCaptureOutput(ctx, deps, "ensure_local_model_mlx", result, captureOutput)
	if artErr != nil {
		return false, "", nil, nil, artErr
	}
	if !result.Success() {
		return false, fmt.Sprintf("hf download %s failed (exit %d)", p.ModelRef, result.ExitCode), &result, artifact, nil
	}

	snapshot := hfSnapshotDir(cacheDir, p.ModelRef, p.ResolvedRevision)
	if info, statErr := os.Stat(snapshot); statErr != nil || !info.IsDir() {
		return false, fmt.Sprintf("hf download %s reported success but revision %s is not present in the local cache afterward (%s)", p.ModelRef, p.ResolvedRevision, snapshot), &result, artifact, nil
	}
	if p.ExpectedSizeBytes > 0 {
		size, sizeErr := hfSnapshotSize(snapshot)
		if sizeErr != nil {
			return false, "", &result, artifact, errs.Wrap(errs.CategoryConflict, sizeErr, "hf download %s reported success but the resulting cache size could not be measured", p.ModelRef)
		}
		if size != p.ExpectedSizeBytes {
			return false, fmt.Sprintf("hf download %s produced %d bytes, which does not match the approved expected_size_bytes %d", p.ModelRef, size, p.ExpectedSizeBytes), &result, artifact, nil
		}
	}

	return true, fmt.Sprintf("downloaded %s@%s via hf, presence and size verified in the local cache", p.ModelRef, p.ResolvedRevision), &result, artifact, nil
}
