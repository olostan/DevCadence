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

// TestNoPackageDependsOnAModelRuntime is the M1 exit condition that no LLM or
// runtime dependency is needed to build or test the control plane.
func TestNoPackageDependsOnAModelRuntime(t *testing.T) {
	forbidden := []string{
		"ollama", "mlx", "openai", "anthropic", "google.golang.org/genai",
		"langchain", "huggingface", "modelcontextprotocol",
	}
	for _, dep := range dependenciesOf(t, "./...") {
		lowered := strings.ToLower(dep)
		for _, needle := range forbidden {
			if strings.Contains(lowered, needle) {
				t.Errorf("the module depends on %s, which no milestone before M3 may require", dep)
			}
		}
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
