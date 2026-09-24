package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
)

// evaluatorNetworkTimeout bounds every live network/subprocess probe this
// file performs (port dials, HTTP GETs, --version invocations). These are
// read-only evidence-gathering calls, not plan actions, so they get a short
// fixed budget rather than a caller-supplied timeout.
const evaluatorNetworkTimeout = 5 * time.Second

// ollamaLocalPort is the default local Ollama API port, matching the port
// internal/setup/planner.go already hardcodes for its own Ollama
// preconditions (see WP-M3B-3 EWP §3 for the verified cross-reference).
const ollamaLocalPort = 11434

// defaultOllamaBaseURL is the production Ollama API base; EvaluatorDeps/
// applierDeps.OllamaBaseURL overrides it, which is what lets tests point
// model_digest_present / ollama_pull_model's supply-chain verification at
// an httptest.Server instead of a real local Ollama daemon.
var defaultOllamaBaseURL = fmt.Sprintf("http://127.0.0.1:%d", ollamaLocalPort)

// EndpointHealthChecker evaluates cognition endpoint health. It is not
// implemented by this package: endpoint_healthy conditions need the M3A
// cognition endpoint registry/probing machinery, which internal/setup does
// not and should not duplicate. With none configured, EvaluateCondition
// fails closed for that condition kind rather than assuming true.
type EndpointHealthChecker interface {
	CheckEndpointHealthy(ctx context.Context, endpointID string) (healthy bool, detail string, err error)
}

// EvaluatorDeps supplies EvaluateCondition's live dependencies.
type EvaluatorDeps struct {
	Runner CommandRunner
	// Home is the resolved $DEVCADENCE_HOME, needed to check
	// managed_dir_exists conditions.
	Home string
	// EndpointHealth is optional; nil means endpoint_healthy conditions
	// fail closed.
	EndpointHealth EndpointHealthChecker
	// OllamaBaseURL overrides the default local Ollama API base
	// ("http://127.0.0.1:11434") — empty means use the default. Tests set
	// this to an httptest.Server URL.
	OllamaBaseURL string
	// ModelRuntimes dispatches model_present conditions to the adapter
	// named by the condition's Runtime field — see modelruntime.go. nil
	// means model_present conditions fail closed with an error, the same
	// as any other unconfigured dependency in this struct.
	ModelRuntimes *ModelRuntimeRegistry
}

func (d EvaluatorDeps) ollamaBaseURL() string {
	if d.OllamaBaseURL != "" {
		return d.OllamaBaseURL
	}
	return defaultOllamaBaseURL
}

// EvaluateCondition checks whether cond currently holds against the live
// system. It is the sole live-evaluation path for preconditions and
// postconditions in this package, and (partially applied over a fixed
// EvaluatorDeps) satisfies WP-M3B-2's PostconditionChecker interface.
func EvaluateCondition(ctx context.Context, deps EvaluatorDeps, cond protocol.Condition) (bool, string, error) {
	switch cond.Kind {
	case protocol.CondKindCommandAvailable:
		return evaluateCommandAvailable(cond.CommandAvailable)
	case protocol.CondKindExecutableVerified:
		return evaluateExecutableVerified(ctx, deps, cond.ExecutableVerified)
	case protocol.CondKindManagedDirExists:
		return evaluateManagedDirExists(deps, cond.ManagedDirExists)
	case protocol.CondKindPortListening:
		return evaluatePortListening(cond.PortListening)
	case protocol.CondKindModelPresent:
		return evaluateModelPresent(ctx, deps, cond.ModelPresent)
	case protocol.CondKindEndpointHealthy:
		return evaluateEndpointHealthy(ctx, deps, cond.EndpointHealthy)
	default:
		return false, "", errs.New(errs.CategoryInvalidArgument, "EvaluateCondition: unhandled condition kind %q", cond.Kind)
	}
}

func evaluateCommandAvailable(op *protocol.CommandAvailableOperand) (bool, string, error) {
	path, err := exec.LookPath(op.CommandName)
	if err != nil {
		return false, fmt.Sprintf("command %q not found on PATH", op.CommandName), nil
	}
	return true, fmt.Sprintf("command %q resolved to %s", op.CommandName, path), nil
}

func evaluateExecutableVerified(ctx context.Context, deps EvaluatorDeps, op *protocol.ExecutableVerifiedOperand) (bool, string, error) {
	// The canonical_path is checked directly, never PATH-searched — this is
	// what prevents PATH substitution attacks (ADR-0014 §1).
	info, err := os.Stat(op.CanonicalPath)
	if err != nil {
		return false, fmt.Sprintf("%s: not found", op.CanonicalPath), nil
	}
	if info.IsDir() || info.Mode().Perm()&0111 == 0 {
		return false, fmt.Sprintf("%s: not an executable file", op.CanonicalPath), nil
	}

	if op.ExpectedDigest != "" {
		digest, err := digestFile(op.CanonicalPath)
		if err != nil {
			return false, "", err
		}
		if digest != op.ExpectedDigest {
			return false, fmt.Sprintf("%s: digest %s does not match expected %s", op.CanonicalPath, digest, op.ExpectedDigest), nil
		}
	}

	if op.ExpectedVersion != "" {
		if deps.Runner == nil {
			return false, "", errs.New(errs.CategoryInvalidArgument, "evaluateExecutableVerified: version check requires a CommandRunner")
		}
		dir := deps.Home
		if dir == "" {
			dir = os.TempDir()
		}
		result, err := deps.Runner.Run(ctx, processSpecFor(op.CanonicalPath, []string{"--version"}, dir))
		if err != nil {
			return false, "", err
		}
		output := string(result.Stdout) + string(result.Stderr)
		if !result.Success() || !strings.Contains(output, op.ExpectedVersion) {
			return false, fmt.Sprintf("%s --version did not report expected version %q", op.CanonicalPath, op.ExpectedVersion), nil
		}
	}

	return true, fmt.Sprintf("%s verified", op.CanonicalPath), nil
}

func digestFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", errs.Wrap(errs.CategoryInternal, err, "open %s for digest", path)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", errs.Wrap(errs.CategoryInternal, err, "hash %s", path)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func evaluateManagedDirExists(deps EvaluatorDeps, op *protocol.ManagedDirOperand) (bool, string, error) {
	path, err := LocationPath(deps.Home, op.Location)
	if err != nil {
		return false, "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, fmt.Sprintf("%s: does not exist", path), nil
	}
	if !info.IsDir() {
		return false, fmt.Sprintf("%s: exists but is not a directory", path), nil
	}
	wantMode, err := parseFileModeOct(op.FileModeOct)
	if err != nil {
		return false, "", err
	}
	if info.Mode().Perm() != wantMode {
		return false, fmt.Sprintf("%s: mode %o does not match expected %s", path, info.Mode().Perm(), op.FileModeOct), nil
	}
	return true, fmt.Sprintf("%s exists with mode %s", path, op.FileModeOct), nil
}

func parseFileModeOct(s string) (os.FileMode, error) {
	var mode uint32
	if _, err := fmt.Sscanf(s, "%o", &mode); err != nil {
		return 0, errs.Wrap(errs.CategoryInvalidArgument, err, "invalid file_mode_oct %q", s)
	}
	return os.FileMode(mode), nil
}

func evaluatePortListening(op *protocol.PortOperand) (bool, string, error) {
	addr := net.JoinHostPort(op.Host, fmt.Sprintf("%d", op.Port))
	conn, err := net.DialTimeout("tcp", addr, evaluatorNetworkTimeout)
	if err != nil {
		return false, fmt.Sprintf("%s: not listening", addr), nil
	}
	_ = conn.Close()
	return true, fmt.Sprintf("%s is listening", addr), nil
}

// evaluateModelPresent dispatches to the adapter registered for op.Runtime.
// There is no default runtime: an unregistered one is always an error,
// symmetric across every runtime including "ollama" (see modelruntime.go).
func evaluateModelPresent(ctx context.Context, deps EvaluatorDeps, op *protocol.ModelPresentOperand) (bool, string, error) {
	adapter, ok := deps.ModelRuntimes.For(op.Runtime)
	if !ok {
		return false, "", unsupportedRuntimeError("evaluateModelPresent", op.Runtime)
	}
	return adapter.ModelPresent(ctx, deps, op)
}

func evaluateEndpointHealthy(ctx context.Context, deps EvaluatorDeps, op *protocol.EndpointOperand) (bool, string, error) {
	if deps.EndpointHealth == nil {
		return false, "", errs.New(errs.CategoryInvalidArgument,
			"evaluateEndpointHealthy: no EndpointHealthChecker configured for endpoint %q", op.EndpointID)
	}
	return deps.EndpointHealth.CheckEndpointHealthy(ctx, op.EndpointID)
}

// processSpecFor builds a minimal, bounded process.Spec for a live
// evaluation probe (e.g. `<path> --version`) — never for a plan action's
// own execution, which the operation appliers build separately.
func processSpecFor(executable string, args []string, dir string) process.Spec {
	return process.Spec{
		Executable: executable,
		Args:       args,
		Dir:        dir,
		Env:        process.BaseEnv(),
		Timeout:    evaluatorNetworkTimeout,
	}
}
