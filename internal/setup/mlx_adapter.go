package setup

import (
	"context"
	"fmt"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
)

// mlxDownloadTimeout mirrors ollamaPullTimeout: model downloads are large,
// so this is a plan action actually running, not a bounded
// evidence-gathering probe.
const mlxDownloadTimeout = ollamaPullTimeout

// mlxPresenceProbeTimeout bounds the read-only --local-files-only presence
// check, which touches only the local Hugging Face cache and never the
// network, so it can be much shorter than a real download.
const mlxPresenceProbeTimeout = diagnosticProbeTimeout

// MLXAdapter implements LocalModelRuntimeAdapter for the MLX-LM local
// runtime, using huggingface-cli (mlx-lm's own model distribution path) to
// download and verify model presence. It is registered as an equal peer of
// OllamaAdapter — ModelRuntimeRegistry treats both symmetrically, and
// neither this package nor internal/protocol privileges one runtime's name
// over the other (INVARIANTS.md DCI-055).
type MLXAdapter struct{}

// Runtime implements LocalModelRuntimeAdapter.
func (MLXAdapter) Runtime() string { return "mlx" }

// ModelPresent implements LocalModelRuntimeAdapter as a read-only check of
// the local Hugging Face cache: `huggingface-cli download --local-files-only`
// exits 0 only when every file for the given repo/revision is already
// cached locally, touching no network. This is a lower-stakes evidence
// probe (like OllamaAdapter's tags fetch), so it resolves the command by
// bare name from PATH rather than requiring a pre-verified executable path
// — the same tier applyRunDiagnosticCheck's mlx_importable check already
// operates at.
func (MLXAdapter) ModelPresent(ctx context.Context, deps EvaluatorDeps, op *protocol.ModelPresentOperand) (bool, string, error) {
	if deps.Runner == nil {
		return false, "", errs.New(errs.CategoryInvalidArgument, "MLXAdapter.ModelPresent: requires a CommandRunner")
	}
	spec := process.Spec{
		Executable: "huggingface-cli",
		Args:       mlxDownloadArgs(op.ModelRef, op.ResolvedRevision, true),
		Dir:        homeOrTemp(deps.Home),
		Env:        process.BaseEnv(),
		Timeout:    mlxPresenceProbeTimeout,
	}
	result, err := deps.Runner.Run(ctx, spec)
	if err != nil {
		// Unreachable/missing huggingface-cli means the condition does not
		// currently hold, not that evaluation itself failed — consistent
		// with OllamaAdapter.ModelPresent treating an unreachable API the
		// same way.
		return false, err.Error(), nil
	}
	if !result.Success() {
		return false, fmt.Sprintf("model %s@%s not present in local Hugging Face cache", op.ModelRef, op.ResolvedRevision), nil
	}
	return true, fmt.Sprintf("model %s@%s present in local Hugging Face cache", op.ModelRef, op.ResolvedRevision), nil
}

// mlxDownloadArgs builds the huggingface-cli argument list shared by the
// mutating download (EnsureModel) and the read-only presence probe
// (ModelPresent), which differ only in --local-files-only.
func mlxDownloadArgs(modelRef, revision string, localFilesOnly bool) []string {
	args := []string{"download", modelRef, "--revision", revision}
	if localFilesOnly {
		args = append(args, "--local-files-only")
	}
	return args
}

// EnsureModel implements LocalModelRuntimeAdapter by downloading the model
// via huggingface-cli, then re-running the same read-only presence check
// EnsureModel's own postcondition would use, so a download that reports
// success but did not actually land the expected revision is caught here
// rather than only (after the fact, if at all) by a separate postcondition
// the plan happens to also declare — the same bounded-supply-chain shape as
// OllamaAdapter.EnsureModel (ADR-0014 §7).
func (MLXAdapter) EnsureModel(ctx context.Context, deps applierDeps, p *protocol.EnsureLocalModelParams, captureOutput bool, verifiedExecutablePath string) (bool, string, *process.Result, *protocol.ArtifactRef, error) {
	if deps.runner == nil {
		return false, "", nil, nil, errs.New(errs.CategoryInvalidArgument, "MLXAdapter.EnsureModel: requires a CommandRunner")
	}
	// The exact binary run here must be the one an executable_verified
	// precondition already checked — never a bare name re-resolved from
	// PATH, the same PATH-substitution defense OllamaAdapter.EnsureModel
	// applies (ADR-0014 §1).
	if verifiedExecutablePath == "" {
		return false, "", nil, nil, errs.New(errs.CategoryPolicyDenied,
			"MLXAdapter.EnsureModel: requires an executable_verified precondition binding the exact huggingface-cli binary to run")
	}

	spec := process.Spec{
		Executable: verifiedExecutablePath,
		Args:       mlxDownloadArgs(p.ModelRef, p.ResolvedRevision, false),
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
		return false, fmt.Sprintf("huggingface-cli download %s failed (exit %d)", p.ModelRef, result.ExitCode), &result, artifact, nil
	}

	verifySpec := process.Spec{
		Executable: verifiedExecutablePath,
		Args:       mlxDownloadArgs(p.ModelRef, p.ResolvedRevision, true),
		Dir:        homeOrTemp(deps.home),
		Env:        process.BaseEnv(),
		Timeout:    mlxPresenceProbeTimeout,
	}
	verifyResult, verifyErr := deps.runner.Run(ctx, verifySpec)
	if verifyErr != nil {
		return false, "", &result, artifact, errs.Wrap(errs.CategoryConflict, verifyErr, "huggingface-cli download %s reported success but the resulting model could not be verified", p.ModelRef)
	}
	if !verifyResult.Success() {
		return false, fmt.Sprintf("huggingface-cli download %s reported success but revision %s is not present in the local cache afterward", p.ModelRef, p.ResolvedRevision), &result, artifact, nil
	}

	return true, fmt.Sprintf("downloaded %s@%s via huggingface-cli, presence verified in the local cache", p.ModelRef, p.ResolvedRevision), &result, artifact, nil
}
