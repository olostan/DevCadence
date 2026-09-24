package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
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
	case protocol.CondKindModelDigestPresent:
		return evaluateModelDigestPresent(ctx, deps.ollamaBaseURL(), cond.ModelDigestPresent)
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

// ollamaModelEntry mirrors just the fields this file needs from one entry of
// Ollama's GET /api/tags response.
type ollamaModelEntry struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

// ollamaTagsResponse mirrors just the fields this file needs from Ollama's
// GET /api/tags response — a minimal, independent parse rather than a
// dependency on internal/cognition/ollama's richer adapter, which is built
// around discovery/probing, not a single tag/digest existence check.
type ollamaTagsResponse struct {
	Models []ollamaModelEntry `json:"models"`
}

// fetchOllamaTags queries the local Ollama API's model list. Shared by
// evaluateModelDigestPresent (a live Condition check) and
// applyOllamaPullModel (post-pull supply-chain verification), so the two
// can never silently disagree about what "present" means.
func fetchOllamaTags(ctx context.Context, baseURL string) (ollamaTagsResponse, error) {
	client := &http.Client{Timeout: evaluatorNetworkTimeout}
	url := baseURL + "/api/tags"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ollamaTagsResponse{}, errs.Wrap(errs.CategoryInternal, err, "build ollama tags request")
	}
	resp, err := client.Do(req)
	if err != nil {
		return ollamaTagsResponse{}, errs.Wrap(errs.CategoryConflict, err, "ollama not reachable at %s", url)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ollamaTagsResponse{}, errs.New(errs.CategoryConflict, "ollama tags request returned status %d", resp.StatusCode)
	}
	var tags ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return ollamaTagsResponse{}, errs.Wrap(errs.CategoryInternal, err, "decode ollama tags response")
	}
	return tags, nil
}

func findOllamaModel(tags ollamaTagsResponse, modelTag string) (ollamaModelEntry, bool) {
	for _, m := range tags.Models {
		if m.Name == modelTag {
			return m, true
		}
	}
	return ollamaModelEntry{}, false
}

// normalizeDigest strips an optional "sha256:" prefix so two digests can be
// compared as bare hex.
func normalizeDigest(d string) string { return strings.TrimPrefix(d, "sha256:") }

func evaluateModelDigestPresent(ctx context.Context, ollamaBaseURL string, op *protocol.ModelDigestOperand) (bool, string, error) {
	if op.Runtime != "ollama" {
		return false, "", errs.New(errs.CategoryInvalidArgument,
			"evaluateModelDigestPresent: unsupported runtime %q (only \"ollama\" is implemented)", op.Runtime)
	}

	tags, err := fetchOllamaTags(ctx, ollamaBaseURL)
	if err != nil {
		// Unreachable/unhealthy Ollama means the condition does not
		// currently hold, not that evaluation itself failed — the caller
		// (an executor precondition/postcondition check) should see "not
		// satisfied," not error out on a transient connectivity gap.
		return false, err.Error(), nil
	}

	entry, found := findOllamaModel(tags, op.ModelTag)
	if !found {
		return false, fmt.Sprintf("model %s not present", op.ModelTag), nil
	}
	// Exact normalized-digest equality only — a prefix match would accept
	// any digest sharing a prefix with the expected one, which is not
	// verification of the immutable digest the protocol field represents.
	if normalizeDigest(entry.Digest) != normalizeDigest(op.Digest) {
		return false, fmt.Sprintf("model %s present but digest %s does not match expected %s", op.ModelTag, entry.Digest, op.Digest), nil
	}
	return true, fmt.Sprintf("model %s present with matching digest", op.ModelTag), nil
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
