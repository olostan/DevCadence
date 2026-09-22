package tests

import (
	"os/exec"
	"strings"
	"testing"
)

// coreDomainPackages are the packages that define the model-independent
// semantic language of the system.
var coreDomainPackages = []string{
	"github.com/olostan/DevCadience/internal/protocol",
	"github.com/olostan/DevCadience/internal/events",
	"github.com/olostan/DevCadience/internal/tasks",
	"github.com/olostan/DevCadience/internal/state",
}

// TestCoreDomainHasNoExternalDependencies enforces DCI-054 and DCI-055
// structurally rather than by review.
//
// The core contracts must not depend semantically on one provider, and
// provider adapters must remain replaceable integrations. The strongest
// available check is that the core packages import nothing outside the
// standard library and this module — no SQLite driver, no schema compiler and
// certainly no model SDK. A future change that binds a protocol type to a
// provider fails here.
func TestCoreDomainHasNoExternalDependencies(t *testing.T) {
	for _, pkg := range coreDomainPackages {
		t.Run(pkg, func(t *testing.T) {
			for _, dep := range dependenciesOf(t, pkg) {
				if !strings.Contains(dep, ".") {
					continue // standard library paths have no dotted domain
				}
				if strings.HasPrefix(dep, "github.com/olostan/DevCadience/") {
					continue
				}
				t.Errorf("core package %s depends on %s", pkg, dep)
			}
		})
	}
}

// TestNoPackageDependsOnAModelProviderSDK keeps model integrations out of the
// module's dependency graph (DCI-055, ENGINEERING_STANDARDS.md §3).
//
// The check is on *third-party* dependencies. It was originally phrased as "no
// package whose path mentions a runtime", which was an adequate proxy while no
// such package existed; M3A introduced first-party adapter packages named after
// the runtimes they adapt (internal/cognition/ollama, .../mlx), and matching on
// their paths would have flagged the very code that keeps the SDKs out.
//
// The invariant itself is unchanged and is if anything stronger now: DevCadience
// drives Ollama over its documented HTTP API and MLX-LM through the controlled
// process runner, so `go test ./...` still needs no model SDK, no Python and no
// GPU. A future adapter that reached for a vendor SDK would fail here.
func TestNoPackageDependsOnAModelProviderSDK(t *testing.T) {
	forbidden := []string{
		"ollama", "mlx", "openai", "anthropic", "google.golang.org/genai",
		"langchain", "huggingface", "modelcontextprotocol", "tiktoken",
	}
	for _, dep := range dependenciesOf(t, "./...") {
		if !strings.Contains(dep, ".") {
			continue // standard library
		}
		if strings.HasPrefix(dep, "github.com/olostan/DevCadience/") {
			continue // first-party; covered by the adapter-isolation check below
		}
		lowered := strings.ToLower(dep)
		for _, needle := range forbidden {
			if strings.Contains(lowered, needle) {
				t.Errorf("the module depends on the third-party package %s; "+
					"model integrations must stay behind adapters that speak protocol types", dep)
			}
		}
	}
}

// adapterPackages are the packages that know about a specific runtime, CLI or
// provider.
var adapterPackages = []string{
	"github.com/olostan/DevCadience/internal/cognition/ollama",
	"github.com/olostan/DevCadience/internal/cognition/mlx",
	"github.com/olostan/DevCadience/internal/cognition/codingcli",
	"github.com/olostan/DevCadience/internal/cognition/remoteapi",
}

// TestProviderAdaptersDoNotLeakIntoTheCore is the M3A half of DCI-055.
//
// "Adapters are replaceable" is only true if nothing depends on a particular
// one. The core domain, the environment layer and the cognition core must
// therefore be buildable without any adapter: an adapter is selected and wired
// by the CLI (or a future daemon), which is the one place a provider choice
// belongs.
//
// Without this check, a convenience import — the cognition core reaching into
// the Ollama package for a constant, say — would quietly make the runtime a core
// dependency while every other test still passed.
func TestProviderAdaptersDoNotLeakIntoTheCore(t *testing.T) {
	core := append([]string{
		"github.com/olostan/DevCadience/internal/cognition",
		"github.com/olostan/DevCadience/internal/environment",
		"github.com/olostan/DevCadience/internal/principalhosts",
		"github.com/olostan/DevCadience/internal/controlplane",
	}, coreDomainPackages...)
	for _, pkg := range core {
		t.Run(pkg, func(t *testing.T) {
			for _, dep := range dependenciesOf(t, pkg) {
				for _, adapter := range adapterPackages {
					if dep == adapter {
						t.Errorf("%s depends on the provider adapter %s; adapters must be "+
							"selected at the edge, not baked into the core", pkg, adapter)
					}
				}
			}
		})
	}
}

// TestAdaptersDependOnlyOnTheContractTheyImplement keeps an adapter from
// reaching around the boundary into persistence or the control plane.
func TestAdaptersDependOnlyOnTheContractTheyImplement(t *testing.T) {
	for _, pkg := range adapterPackages {
		t.Run(pkg, func(t *testing.T) {
			for _, dep := range dependenciesOf(t, pkg) {
				for _, forbidden := range []string{
					"github.com/olostan/DevCadience/internal/storage",
					"github.com/olostan/DevCadience/internal/controlplane",
					"github.com/olostan/DevCadience/internal/state",
					"github.com/olostan/DevCadience/internal/events",
				} {
					if dep == forbidden {
						t.Errorf("adapter %s depends on %s", pkg, forbidden)
					}
				}
			}
		})
	}
}

// TestProtocolDoesNotDependOnStorage keeps the twin of the JSON Schemas free
// of persistence concerns, so that the contract can be reused by an adapter
// that stores nothing.
func TestProtocolDoesNotDependOnStorage(t *testing.T) {
	for _, dep := range dependenciesOf(t, "github.com/olostan/DevCadience/internal/protocol") {
		for _, forbidden := range []string{
			"github.com/olostan/DevCadience/internal/storage",
			"github.com/olostan/DevCadience/internal/controlplane",
			"github.com/olostan/DevCadience/internal/schema",
		} {
			if dep == forbidden {
				t.Errorf("internal/protocol depends on %s", dep)
			}
		}
	}
}

// dependenciesOf returns the transitive import list of a package pattern.
func dependenciesOf(t *testing.T, pattern string) []string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", pattern)
	cmd.Dir = ".."
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("go list unavailable in this environment: %v", err)
	}
	return strings.Fields(string(out))
}
