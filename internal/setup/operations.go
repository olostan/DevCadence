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

// applierDeps supplies applyOperation's live dependencies. Unexported along
// with applyOperation itself: the executor's approval/precondition/ledger
// gating is the only intended path to mutating the system (ADR-0014 §1/§7
// MUST), so nothing outside this file's own dispatcher and Executor's own
// call into it should be able to construct a bypass.
type applierDeps struct {
	runner    CommandRunner
	home      string
	cache     *CacheManager
	artifacts *artifacts.Store
	// modelRuntimes dispatches ensure_local_model operations to the
	// adapter named by the operation's Runtime field — see
	// modelruntime.go. nil means ensure_local_model operations fail
	// closed with an error, the same as any other unconfigured dependency
	// in this struct. Each adapter carries its own runtime-specific
	// configuration (e.g. OllamaAdapter.BaseURL) rather than this struct
	// — see modelruntime.go/ollama_adapter.go.
	modelRuntimes *ModelRuntimeRegistry
}

// applyOperation dispatches op to its applier — the sole path through
// which any TypedOperation kind actually mutates the system. captureOutput
// controls whether a subprocess-backed applier persists its output as an
// artifact; the caller (Executor) sets it false for any action whose
// Effects include EffectAuthentication. verifiedExecutablePath, when
// non-empty, is the exact canonical path an executable_verified
// precondition on this same action already checked (digest/version) —
// appliers that run a specific binary (ollama_pull_model) must run that
// exact path, never re-resolve the bare command name from PATH, or the
// verified identity and the executed identity could silently diverge
// (ADR-0014 §1's PATH-substitution defense). procResult is non-nil only
// when this operation actually ran a subprocess.
func applyOperation(ctx context.Context, deps applierDeps, op protocol.TypedOperation, captureOutput bool, verifiedExecutablePath string) (mutated bool, detail string, procResult *process.Result, artifact *protocol.ArtifactRef, err error) {
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
	case protocol.OpKindEnsureLocalModel:
		return applyEnsureLocalModel(ctx, deps, op.EnsureLocalModel, captureOutput, verifiedExecutablePath)
	default:
		return false, "", nil, nil, errs.New(errs.CategoryInvalidArgument, "applyOperation: unhandled operation kind %q", op.Kind)
	}
}

func applyCreateDirectory(deps applierDeps, p *protocol.CreateDirectoryParams) (bool, string, error) {
	path, err := LocationPath(deps.home, p.Location)
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

func applyWriteManagedConfig(deps applierDeps, p *protocol.WriteManagedConfigParams) (bool, string, error) {
	if err := writeManagedConfigKey(deps.home, p.Key, p.Value); err != nil {
		return false, "", err
	}
	return true, fmt.Sprintf("set %s = %s", p.Key, p.Value), nil
}

func applyRemoveStaleCache(ctx context.Context, deps applierDeps, p *protocol.RemoveStaleCacheParams) (bool, string, error) {
	if deps.cache == nil {
		return false, "", errs.New(errs.CategoryInvalidArgument, "applyRemoveStaleCache: no CacheManager configured")
	}
	if err := deps.cache.Remove(ctx, p.Target); err != nil {
		return false, "", err
	}
	return true, fmt.Sprintf("removed cache target %s", p.Target), nil
}

// diagnosticFailed reports a diagnostic check's negative outcome as a real
// error, not a silently-successful (nil error, negative detail) result.
// Before this, an executor basing action success only on opErr==nil plus
// postconditions would mark a run_diagnostic_check action succeeded even
// when e.g. git was missing or the state root wasn't writable.
func diagnosticFailed(checkName protocol.DiagnosticCheckName, detail string) error {
	return errs.New(errs.CategoryConflict, "diagnostic check %s failed: %s", checkName, detail)
}

func applyRunDiagnosticCheck(ctx context.Context, deps applierDeps, p *protocol.RunDiagnosticCheckParams, captureOutput bool) (bool, string, *process.Result, *protocol.ArtifactRef, error) {
	switch p.CheckName {
	case protocol.CheckGitAvailable:
		passed, detail, err := evaluateCommandAvailable(&protocol.CommandAvailableOperand{CommandName: "git"})
		if err != nil {
			return false, "", nil, nil, err
		}
		if !passed {
			return false, detail, nil, nil, diagnosticFailed(p.CheckName, detail)
		}
		return false, detail, nil, nil, nil

	case protocol.CheckStateRootWritable:
		// A non-mutating check: run_diagnostic_check's IntrinsicPolicy
		// declares AuthorityReadOnly for this operation kind, so this
		// check must never itself write to disk. This inspects the
		// owner-write permission mode bit rather than proving writability
		// by writing — a real but genuinely weaker signal than "this path
		// is writable": it cannot see ACLs, read-only-mounted filesystems,
		// or quota. The result wording below says exactly that, rather
		// than asserting unqualified writability the check cannot
		// actually establish (a prior revision's wording was flagged as
		// untruthful for exactly this reason).
		path := deps.home
		info, statErr := os.Stat(path)
		if statErr != nil {
			detail := fmt.Sprintf("%s: %v", path, statErr)
			return false, detail, nil, nil, diagnosticFailed(p.CheckName, detail)
		}
		if info.Mode().Perm()&0200 == 0 {
			detail := fmt.Sprintf("%s: owner-write permission bit is not set (mode %o) — writable access not established", path, info.Mode().Perm())
			return false, detail, nil, nil, diagnosticFailed(p.CheckName, detail)
		}
		return false, fmt.Sprintf("%s: owner-write permission bit is set (mode %o) — appears writable; this mode-bit check cannot see ACLs, read-only mounts, or quota, so it is not a guarantee", path, info.Mode().Perm()), nil, nil, nil

	case protocol.CheckOllamaResponding:
		passed, detail, err := evaluatePortListening(&protocol.PortOperand{Host: "127.0.0.1", Port: ollamaLocalPort})
		if err != nil {
			return false, "", nil, nil, err
		}
		if !passed {
			return false, detail, nil, nil, diagnosticFailed(p.CheckName, detail)
		}
		return false, detail, nil, nil, nil

	case protocol.CheckMLXImportable:
		if deps.runner == nil {
			return false, "", nil, nil, errs.New(errs.CategoryInvalidArgument, "applyRunDiagnosticCheck: mlx_importable requires a CommandRunner")
		}
		spec := process.Spec{
			Executable: "python3",
			Args:       []string{"-c", "import mlx"},
			Dir:        homeOrTemp(deps.home),
			Env:        process.BaseEnv(),
			Timeout:    diagnosticProbeTimeout,
		}
		result, err := deps.runner.Run(ctx, spec)
		if err != nil {
			return false, "", nil, nil, err
		}
		artifact, artErr := maybeCaptureOutput(ctx, deps, "mlx_importable", result, captureOutput)
		if artErr != nil {
			return false, "", nil, nil, artErr
		}
		if !result.Success() {
			return false, "mlx not importable", &result, artifact, diagnosticFailed(p.CheckName, "mlx not importable")
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

// applyEnsureLocalModel dispatches to the adapter registered for
// p.Runtime — the sole path through which any ensure_local_model operation
// actually mutates the system. There is no default runtime: an
// unregistered one is always an error, symmetric across every runtime
// including "ollama" (see modelruntime.go).
func applyEnsureLocalModel(ctx context.Context, deps applierDeps, p *protocol.EnsureLocalModelParams, captureOutput bool, verifiedExecutablePath string) (bool, string, *process.Result, *protocol.ArtifactRef, error) {
	adapter, ok := deps.modelRuntimes.For(p.Runtime)
	if !ok {
		return false, "", nil, nil, unsupportedRuntimeError("applyEnsureLocalModel", p.Runtime)
	}
	return adapter.EnsureModel(ctx, deps, p, captureOutput, verifiedExecutablePath)
}

// ansiEscape matches ANSI/VT100 control sequences a CLI tool's progress
// output can contain: CSI sequences (colour, cursor movement, including
// private-mode parameters like "\x1b[?25l"), OSC sequences (terminal
// title/hyperlink payloads, terminated by BEL or ST), and the remaining
// two-byte Fe escape sequences — this is a real control-sequence
// sanitizer, not just the narrow "CSI ending in a letter" pattern that
// misses OSC and private-parameter forms.
var ansiEscape = regexp.MustCompile(
	"\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)" + // OSC ... (BEL | ST)
		"|\x1b\\[[0-9;:<=>?]*[ -/]*[@-~]" + // CSI ... final byte (incl. private/intermediate bytes)
		"|\x1b[@-Z\\\\\\]^_]" + // remaining two-byte Fe escape sequences
		"|\r",
)

// maybeCaptureOutput persists a subprocess result's combined, ANSI-stripped
// output as a content-addressed artifact, bounded to
// process.DefaultMaxOutputBytes. It returns (nil, nil) when captureOutput
// is false — the caller (Executor) sets that to false for any action whose
// Effects include EffectAuthentication, so no raw output ever reaches
// durable storage for those actions (ADR-0014 §7).
func maybeCaptureOutput(ctx context.Context, deps applierDeps, kind string, result process.Result, captureOutput bool) (*protocol.ArtifactRef, error) {
	if !captureOutput {
		return nil, nil
	}
	if deps.artifacts == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "maybeCaptureOutput: no artifacts.Store configured")
	}
	combined := ansiEscape.ReplaceAll(append(append([]byte{}, result.Stdout...), result.Stderr...), nil)
	putResult, err := deps.artifacts.PutBytes(ctx, artifactsNamespace, kind, "text/plain", combined, process.DefaultMaxOutputBytes)
	if err != nil {
		return nil, err
	}
	ref := putResult.Ref
	return &ref, nil
}
