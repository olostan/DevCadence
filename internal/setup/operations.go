package setup

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/olostan/DevCadence/internal/artifacts"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
)

// artifactsNamespace is the fixed internal/artifacts.Store "project id"
// this package writes setup output artifacts under. It is not a real
// project — $DEVCADENCE_HOME/artifacts/setup is machine-global operational
// state, never project/Git state (ADR-0014 §4) — the namespace only exists
// because Store's API is shaped around one.
const artifactsNamespace = "setup"

// ollamaPullTimeout is generous because model downloads are large; this is
// a plan action actually running, not a bounded evidence-gathering probe
// (see evaluatorNetworkTimeout in conditions.go for that distinction).
const ollamaPullTimeout = 30 * time.Minute

const diagnosticProbeTimeout = 10 * time.Second

// ApplierDeps supplies ApplyOperation's live dependencies.
type ApplierDeps struct {
	Runner    CommandRunner
	Home      string
	Cache     *CacheManager
	Artifacts *artifacts.Store
}

// ApplyOperation dispatches op to its applier — the sole path through
// which any TypedOperation kind actually mutates the system (ADR-0014 §1/
// §7: no ad hoc shell-out or filesystem mutation for a plan action lives
// anywhere else in internal/setup). captureOutput controls whether a
// subprocess-backed applier persists its output as an artifact; the caller
// (Executor) sets it false for any action whose Effects include
// EffectAuthentication. procResult is non-nil only when this operation
// actually ran a subprocess — create_directory/write_managed_config/
// remove_stale_cache and three of the four diagnostic checks never do, and
// the Executor only emits an ActionProcessCompleted ledger event when one
// ran.
func ApplyOperation(ctx context.Context, deps ApplierDeps, op protocol.TypedOperation, captureOutput bool) (mutated bool, detail string, procResult *process.Result, artifact *protocol.ArtifactRef, err error) {
	switch op.Kind {
	case protocol.OpKindCreateDirectory:
		mutated, detail, err = applyCreateDirectory(deps, op.CreateDirectory)
		return mutated, detail, nil, nil, err
	case protocol.OpKindWriteManagedConfig:
		mutated, detail, err = applyWriteManagedConfig(deps, op.WriteManagedConfig)
		return mutated, detail, nil, nil, err
	case protocol.OpKindRemoveStaleCache:
		mutated, detail, err = applyRemoveStaleCache(ctx, deps, op.RemoveStaleCache)
		return mutated, detail, nil, nil, err
	case protocol.OpKindRunDiagnosticCheck:
		return applyRunDiagnosticCheck(ctx, deps, op.RunDiagnosticCheck, captureOutput)
	case protocol.OpKindOllamaPullModel:
		return applyOllamaPullModel(ctx, deps, op.OllamaPullModel, captureOutput)
	default:
		return false, "", nil, nil, errs.New(errs.CategoryInvalidArgument, "ApplyOperation: unhandled operation kind %q", op.Kind)
	}
}

func applyCreateDirectory(deps ApplierDeps, p *protocol.CreateDirectoryParams) (bool, string, error) {
	path, err := LocationPath(deps.Home, p.Location)
	if err != nil {
		return false, "", err
	}
	mode, err := parseFileModeOct(p.FileModeOct)
	if err != nil {
		return false, "", err
	}
	if err := ensureDirMode(path, mode); err != nil {
		return false, "", err
	}
	return true, fmt.Sprintf("created %s with mode %s", path, p.FileModeOct), nil
}

func applyWriteManagedConfig(deps ApplierDeps, p *protocol.WriteManagedConfigParams) (bool, string, error) {
	if err := WriteManagedConfigKey(deps.Home, p.Key, p.Value); err != nil {
		return false, "", err
	}
	return true, fmt.Sprintf("set %s = %s", p.Key, p.Value), nil
}

func applyRemoveStaleCache(ctx context.Context, deps ApplierDeps, p *protocol.RemoveStaleCacheParams) (bool, string, error) {
	if deps.Cache == nil {
		return false, "", errs.New(errs.CategoryInvalidArgument, "applyRemoveStaleCache: no CacheManager configured")
	}
	if err := deps.Cache.Remove(ctx, p.Target); err != nil {
		return false, "", err
	}
	return true, fmt.Sprintf("removed cache target %s", p.Target), nil
}

func applyRunDiagnosticCheck(ctx context.Context, deps ApplierDeps, p *protocol.RunDiagnosticCheckParams, captureOutput bool) (bool, string, *process.Result, *protocol.ArtifactRef, error) {
	switch p.CheckName {
	case protocol.CheckGitAvailable:
		_, detail, err := evaluateCommandAvailable(&protocol.CommandAvailableOperand{CommandName: "git"})
		return false, detail, nil, nil, err
	case protocol.CheckStateRootWritable:
		path := deps.Home
		f, err := os.CreateTemp(path, ".devcadence_diagnostic_*")
		if err != nil {
			return false, fmt.Sprintf("%s: not writable: %v", path, err), nil, nil, nil
		}
		name := f.Name()
		_ = f.Close()
		_ = os.Remove(name)
		return false, fmt.Sprintf("%s is writable", path), nil, nil, nil
	case protocol.CheckOllamaResponding:
		_, detail, err := evaluatePortListening(&protocol.PortOperand{Host: "127.0.0.1", Port: ollamaLocalPort})
		return false, detail, nil, nil, err
	case protocol.CheckMLXImportable:
		if deps.Runner == nil {
			return false, "", nil, nil, errs.New(errs.CategoryInvalidArgument, "applyRunDiagnosticCheck: mlx_importable requires a CommandRunner")
		}
		spec := process.Spec{
			Executable: "python3",
			Args:       []string{"-c", "import mlx"},
			Dir:        homeOrTemp(deps.Home),
			Env:        process.BaseEnv(),
			Timeout:    diagnosticProbeTimeout,
		}
		result, err := deps.Runner.Run(ctx, spec)
		if err != nil {
			return false, "", nil, nil, err
		}
		artifact, artErr := maybeCaptureOutput(ctx, deps, "mlx_importable", result, captureOutput)
		if artErr != nil {
			return false, "", nil, nil, artErr
		}
		if !result.Success() {
			return false, "mlx not importable", &result, artifact, nil
		}
		return false, "mlx is importable", &result, artifact, nil
	default:
		return false, "", nil, nil, errs.New(errs.CategoryInvalidArgument, "applyRunDiagnosticCheck: unhandled check_name %q", p.CheckName)
	}
}

func homeOrTemp(home string) string {
	if home != "" {
		return home
	}
	return os.TempDir()
}

func applyOllamaPullModel(ctx context.Context, deps ApplierDeps, p *protocol.OllamaPullModelParams, captureOutput bool) (bool, string, *process.Result, *protocol.ArtifactRef, error) {
	if deps.Runner == nil {
		return false, "", nil, nil, errs.New(errs.CategoryInvalidArgument, "applyOllamaPullModel: requires a CommandRunner")
	}
	spec := process.Spec{
		Executable: "ollama",
		Args:       []string{"pull", p.ModelTag},
		Dir:        homeOrTemp(deps.Home),
		Env:        process.BaseEnv(),
		Timeout:    ollamaPullTimeout,
	}
	result, err := deps.Runner.Run(ctx, spec)
	if err != nil {
		return false, "", nil, nil, err
	}
	artifact, artErr := maybeCaptureOutput(ctx, deps, "ollama_pull_model", result, captureOutput)
	if artErr != nil {
		return false, "", nil, nil, artErr
	}
	if !result.Success() {
		return false, fmt.Sprintf("ollama pull %s failed (exit %d)", p.ModelTag, result.ExitCode), &result, artifact, nil
	}
	return true, fmt.Sprintf("pulled %s via ollama", p.ModelTag), &result, artifact, nil
}

// ansiEscape matches ANSI/VT100 control sequences (colour codes, cursor
// movement) that a CLI tool like `ollama pull`'s progress bar emits — these
// are terminal-rendering noise, not evidence, and ADR-0014 §7 requires
// captured output to be stripped of them before storage.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\r`)

// maybeCaptureOutput persists a subprocess result's combined, ANSI-stripped
// output as a content-addressed artifact, bounded to
// process.DefaultMaxOutputBytes. It returns (nil, nil) when captureOutput
// is false — the caller (Executor) sets that to false for any action whose
// Effects include EffectAuthentication, so no raw output ever reaches
// durable storage for those actions (ADR-0014 §7).
func maybeCaptureOutput(ctx context.Context, deps ApplierDeps, kind string, result process.Result, captureOutput bool) (*protocol.ArtifactRef, error) {
	if !captureOutput {
		return nil, nil
	}
	if deps.Artifacts == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "maybeCaptureOutput: no artifacts.Store configured")
	}
	combined := ansiEscape.ReplaceAll(append(append([]byte{}, result.Stdout...), result.Stderr...), nil)
	putResult, err := deps.Artifacts.PutBytes(ctx, artifactsNamespace, kind, "text/plain", combined, process.DefaultMaxOutputBytes)
	if err != nil {
		return nil, err
	}
	ref := putResult.Ref
	return &ref, nil
}
