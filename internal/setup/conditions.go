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

	"github.com/olostan/DevCadence/internal/credentials"
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

// defaultOllamaBaseURL is the production Ollama API base that
// OllamaAdapter.baseURL() falls back to when its own BaseURL field is
// unset — a runtime-owned default, not a generic executor dependency (see
// OllamaAdapter's doc comment in ollama_adapter.go).
var defaultOllamaBaseURL = fmt.Sprintf("http://127.0.0.1:%d", ollamaLocalPort)

// EndpointHealthChecker evaluates cognition endpoint health. It is not
// implemented by this package: endpoint_healthy conditions need the M3A
// cognition endpoint registry/probing machinery, which internal/setup does
// not and should not duplicate. With none configured, EvaluateCondition
// fails closed for that condition kind rather than assuming true.
type EndpointHealthChecker interface {
	CheckEndpointHealthy(ctx context.Context, endpointID string) (healthy bool, detail string, err error)
}

// EndpointAuthChecker evaluates whether an endpoint has been verified
// authenticated — never merely healthy (WP-M3B-4's "healthy != authenticated
// != usable" invariant). Like EndpointHealthChecker, it is not implemented
// by this package: it needs the WP-M3B-4 credential/auth evidence boundary
// (internal/credentials.Manager or an M3A endpoint registry), which
// internal/setup does not and should not duplicate. With none configured,
// EvaluateCondition fails closed for endpoint_authenticated rather than
// assuming true (independent-review follow-up on WP-M3B-5, finding 6).
//
// credentialRefID is the explicit protocol.EndpointOperand.CredentialRefID
// binding the condition itself carries — never derived by guessing a
// credential locator from endpointID. CognitionEndpoint != CredentialRef is
// a WP-M3B-4 identity boundary a checker must not collapse (independent-
// review follow-up on WP-M3B-5, round-3 finding 2): an endpoint ID is not a
// credential locator, and an endpoint's credential can be any configured
// CredentialRefKind (cli_session, env_var, keychain_ref), not only a CLI
// session whose locator happens to equal the endpoint ID.
type EndpointAuthChecker interface {
	CheckEndpointAuthenticated(ctx context.Context, endpointID, credentialRefID string) (authenticated bool, detail string, err error)
}

// CredentialsEndpointAuthChecker verifies endpoint authentication by
// resolving a condition's explicit CredentialRefID against an index of the
// actual configured protocol.CredentialRef values (keyed by RefID) and
// delegating to a credentials.Manager (the WP-M3B-4 credential and
// auth-evidence authority). It never fabricates a CredentialRef from an
// endpoint ID: a credentialRefID with no entry in the index is treated as
// "no configured binding for this endpoint" and fails closed, rather than
// guessing.
type CredentialsEndpointAuthChecker struct {
	manager  *credentials.Manager
	refsByID map[string]protocol.CredentialRef
}

// buildCredentialRefIndex validates every ref structurally and indexes it by
// RefID, rejecting a duplicate RefID outright: RefID is meant to be an
// identity (the whole endpoint->CredentialRef binding model depends on
// "CredentialRef.RefID -> exactly one configured CredentialRef"), not a
// multimap key a later "last write wins" map build could silently resolve
// differently depending on iteration order. Both NewDoctor and
// NewCredentialsEndpointAuthChecker call this one helper so they can never
// drift on what counts as a valid, unambiguous configured set
// (independent-review follow-up on WP-M3B-5, round-6 finding).
func buildCredentialRefIndex(refs []protocol.CredentialRef) (map[string]protocol.CredentialRef, error) {
	byID := make(map[string]protocol.CredentialRef, len(refs))
	for _, ref := range refs {
		if err := ref.Validate(); err != nil {
			return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "configured CredentialRefs contains an invalid entry")
		}
		if _, dup := byID[ref.RefID]; dup {
			return nil, errs.New(errs.CategoryInvalidArgument,
				"configured CredentialRefs contains duplicate ref_id %q; RefID must be a unique identity, not a multimap key", ref.RefID)
		}
		byID[ref.RefID] = ref
	}
	return byID, nil
}

// NewCredentialsEndpointAuthChecker constructs an EndpointAuthChecker backed
// by the WP-M3B-4 credential manager and an explicit set of configured
// CredentialRef values (e.g. DoctorOptions.CredentialRefs) it may resolve a
// condition's CredentialRefID against. mgr must have real, authoritative
// auth adapters configured (a Manager with no adapters can never prove
// anything authenticated, and wiring one anyway would misleadingly claim
// "production auth verification" where none exists — independent-review
// follow-up on WP-M3B-5, round-3 finding 2's "Runner-only fallback" point).
func NewCredentialsEndpointAuthChecker(mgr *credentials.Manager, refs []protocol.CredentialRef) (*CredentialsEndpointAuthChecker, error) {
	byID, err := buildCredentialRefIndex(refs)
	if err != nil {
		return nil, err
	}
	return &CredentialsEndpointAuthChecker{manager: mgr, refsByID: byID}, nil
}

// CheckEndpointAuthenticated evaluates authentication for the CredentialRef
// explicitly bound to endpointID via credentialRefID. An empty or unresolved
// credentialRefID means no configured credential binding is known for this
// endpoint — this is evidence the checker cannot verify authentication, so
// it fails closed (authenticated=false with a clear detail), never a license
// to guess a locator from endpointID.
func (c *CredentialsEndpointAuthChecker) CheckEndpointAuthenticated(ctx context.Context, endpointID, credentialRefID string) (bool, string, error) {
	if c == nil || c.manager == nil {
		return false, "no credential manager configured", nil
	}
	if credentialRefID == "" {
		return false, fmt.Sprintf("no configured credential binding for endpoint %q", endpointID), nil
	}
	ref, ok := c.refsByID[credentialRefID]
	if !ok {
		return false, fmt.Sprintf("credential_ref_id %q is not among the configured CredentialRefs", credentialRefID), nil
	}
	evidence, err := c.manager.CheckCredential(ctx, ref)
	if err != nil {
		return false, "", err
	}
	return evidence.Status == protocol.AuthStatusAuthenticated, evidence.Detail, nil
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
	// EndpointAuth is optional; nil means endpoint_authenticated conditions
	// fail closed.
	EndpointAuth EndpointAuthChecker
	// ModelRuntimes dispatches model_present conditions to the adapter
	// named by the condition's Runtime field — see modelruntime.go. nil
	// means model_present conditions fail closed with an error, the same
	// as any other unconfigured dependency in this struct. Each adapter
	// carries its own runtime-specific configuration (e.g.
	// OllamaAdapter.BaseURL) rather than this struct — see
	// modelruntime.go/ollama_adapter.go.
	ModelRuntimes *ModelRuntimeRegistry
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
	case protocol.CondKindEndpointAuthenticated:
		return evaluateEndpointAuthenticated(ctx, deps, cond.EndpointAuthenticated)
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
		versionArgs := versionProbeArgs(op.VersionProbe)
		result, err := deps.Runner.Run(ctx, processSpecFor(op.CanonicalPath, versionArgs, dir))
		if err != nil {
			return false, "", err
		}
		output := string(result.Stdout) + string(result.Stderr)
		if !result.Success() || !strings.Contains(output, op.ExpectedVersion) {
			return false, fmt.Sprintf("%s %s did not report expected version %q", op.CanonicalPath, strings.Join(versionArgs, " "), op.ExpectedVersion), nil
		}
	}

	return true, fmt.Sprintf("%s verified", op.CanonicalPath), nil
}

// versionProbeArgs is the executor-owned mapping from a closed
// protocol.VersionProbeKind to the exact argv it runs — the only place
// this argv is decided. A plan can select a probe kind but can never
// supply its own argv: protocol.VersionProbeKind is a closed enum
// (Valid() rejects anything else at plan-validation time), so this
// switch's default is unreachable for a validated plan, not a silent
// fallback for attacker-controlled input.
func versionProbeArgs(kind protocol.VersionProbeKind) []string {
	switch kind {
	case protocol.VersionProbeVersionSubcommand:
		return []string{"version"}
	default:
		return []string{"--version"}
	}
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

func evaluateEndpointAuthenticated(ctx context.Context, deps EvaluatorDeps, op *protocol.EndpointOperand) (bool, string, error) {
	if deps.EndpointAuth == nil {
		return false, "", errs.New(errs.CategoryInvalidArgument,
			"evaluateEndpointAuthenticated: no EndpointAuthChecker configured for endpoint %q", op.EndpointID)
	}
	return deps.EndpointAuth.CheckEndpointAuthenticated(ctx, op.EndpointID, op.CredentialRefID)
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
