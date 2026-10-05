package tests

import (
	"os/exec"
	"strings"
	"testing"
)

// coreDomainPackages are the packages that define the model-independent
// semantic language of the system.
var coreDomainPackages = []string{
	"github.com/olostan/DevCadence/internal/protocol",
	"github.com/olostan/DevCadence/internal/events",
	"github.com/olostan/DevCadence/internal/tasks",
	"github.com/olostan/DevCadence/internal/state",
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
				firstSegment, _, _ := strings.Cut(dep, "/")
				if !strings.Contains(firstSegment, ".") {
					continue // standard library paths have no dotted domain in the first path segment
				}
				if strings.HasPrefix(dep, "github.com/olostan/DevCadence/") {
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
// The invariant itself is unchanged and is if anything stronger now: DevCadence
// drives Ollama over its documented HTTP API and MLX-LM through the controlled
// process runner, so `go test ./...` still needs no model SDK, no Python and no
// GPU. A future adapter that reached for a vendor SDK would fail here.
func TestNoPackageDependsOnAModelProviderSDK(t *testing.T) {
	forbidden := []string{
		"ollama", "mlx", "openai", "anthropic", "google.golang.org/genai",
		"langchain", "huggingface", "tiktoken",
	}
	for _, dep := range dependenciesOf(t, "./...") {
		firstSegment, _, _ := strings.Cut(dep, "/")
		if !strings.Contains(firstSegment, ".") {
			continue // standard library
		}
		if strings.HasPrefix(dep, "github.com/olostan/DevCadence/") {
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

// TestMCPSDKIsConfinedToTheAdapter keeps the official Model Context Protocol Go
// SDK (WP-M5-2) out of the core, the facade and persistence: only the thin
// transport adapter and its launcher binary may import it, so semantics stay
// host-neutral (I1, DCI-054/055). The adapter dependency check fails rather than
// skipping, so it cannot pass vacuously.
func TestMCPSDKIsConfinedToTheAdapter(t *testing.T) {
	const (
		module = "github.com/olostan/DevCadence/"
		sdk    = "github.com/modelcontextprotocol/go-sdk"
	)
	hasSDK := func(deps []string) bool {
		for _, dep := range deps {
			if strings.HasPrefix(dep, sdk+"/") || dep == sdk {
				return true
			}
		}
		return false
	}
	if !hasSDK(dependenciesOf(t, module+"internal/mcpadapter")) {
		t.Fatal("internal/mcpadapter does not depend on the MCP SDK; the confinement check would be vacuous")
	}
	for _, pkg := range append([]string{
		module + "internal/principal", module + "internal/principal/facade",
		module + "internal/controlplane", module + "internal/storage",
	}, coreDomainPackages...) {
		if hasSDK(dependenciesOf(t, pkg)) {
			t.Errorf("%s depends on the MCP SDK; only internal/mcpadapter and cmd/devcadence-mcp may", pkg)
		}
	}
	cmd := exec.Command("go", "list", "-f", "{{.ImportPath}} {{join .Imports \" \"}}", "./...")
	cmd.Dir = ".."
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("go list unavailable in this environment: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		pkg := fields[0]
		if pkg == module+"internal/mcpadapter" || pkg == module+"cmd/devcadence-mcp" {
			continue
		}
		if hasSDK(fields[1:]) {
			t.Errorf("%s imports the MCP SDK directly; only the adapter and its launcher may", pkg)
		}
	}
}

// adapterPackages are the packages that know about a specific runtime, CLI or
// provider.
var adapterPackages = []string{
	"github.com/olostan/DevCadence/internal/cognition/ollama",
	"github.com/olostan/DevCadence/internal/cognition/mlx",
	"github.com/olostan/DevCadence/internal/cognition/codingcli",
	"github.com/olostan/DevCadence/internal/cognition/remoteapi",
	"github.com/olostan/DevCadence/internal/cognition/plannerdriver",
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
		"github.com/olostan/DevCadence/internal/cognition",
		"github.com/olostan/DevCadence/internal/cognition/planner",
		"github.com/olostan/DevCadence/internal/environment",
		"github.com/olostan/DevCadence/internal/principalhosts",
		"github.com/olostan/DevCadence/internal/controlplane",
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
					"github.com/olostan/DevCadence/internal/storage",
					"github.com/olostan/DevCadence/internal/controlplane",
					"github.com/olostan/DevCadence/internal/state",
					"github.com/olostan/DevCadence/internal/events",
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
	for _, dep := range dependenciesOf(t, "github.com/olostan/DevCadence/internal/protocol") {
		for _, forbidden := range []string{
			"github.com/olostan/DevCadence/internal/storage",
			"github.com/olostan/DevCadence/internal/controlplane",
			"github.com/olostan/DevCadence/internal/schema",
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

// TestPortfolioPlanner_ACC18_DependenciesExcludeDriversAdaptersAndPersistence keeps the
// planner service free of driver, adapter and persistence coupling (WP-M3D-1B
// REQ-12, DCI-054/055). It fails rather than skipping when the dependency list
// cannot be resolved, so it can never pass vacuously.
func TestPortfolioPlanner_ACC18_DependenciesExcludeDriversAdaptersAndPersistence(t *testing.T) {
	const pkg = "github.com/olostan/DevCadence/internal/cognition/planner"
	deps := dependenciesOf(t, pkg)
	present := false
	for _, dep := range deps {
		if dep == "github.com/olostan/DevCadence/internal/protocol" {
			present = true
		}
	}
	if !present {
		t.Fatalf("dependency list of %s does not contain internal/protocol; the check would be vacuous (got %d deps)", pkg, len(deps))
	}
	forbidden := append([]string{
		"github.com/olostan/DevCadence/internal/cognition/drivers",
		"github.com/olostan/DevCadence/internal/storage",
		"github.com/olostan/DevCadence/internal/controlplane",
		"github.com/olostan/DevCadence/internal/state",
		"github.com/olostan/DevCadence/internal/events",
	}, adapterPackages...)
	for _, dep := range deps {
		for _, f := range forbidden {
			if dep == f {
				t.Errorf("planner depends on forbidden package %s", dep)
			}
		}
	}
}

// TestPlannerDriverInvoker_ACC13_BoundaryIsNonVacuous (WP-M3D-1C1) checks that
// the cognition core stays free of drivers and that the driver-backed planner
// adapter really sits between planner and drivers. It fails rather than
// passing vacuously when the adapter's dependency list is incomplete.
func TestPlannerDriverInvoker_ACC13_BoundaryIsNonVacuous(t *testing.T) {
	const (
		drv = "github.com/olostan/DevCadence/internal/cognition/drivers"
		pln = "github.com/olostan/DevCadence/internal/cognition/planner"
	)
	for _, dep := range dependenciesOf(t, "github.com/olostan/DevCadence/internal/cognition") {
		if dep == drv {
			t.Errorf("internal/cognition depends on %s", dep)
		}
	}
	deps := dependenciesOf(t, "github.com/olostan/DevCadence/internal/cognition/plannerdriver")
	var hasPlanner, hasDrivers bool
	for _, dep := range deps {
		hasPlanner = hasPlanner || dep == pln
		hasDrivers = hasDrivers || dep == drv
	}
	if !hasPlanner || !hasDrivers {
		t.Fatalf("plannerdriver dependency list lacks planner=%v drivers=%v (got %d deps); the check would be vacuous", hasPlanner, hasDrivers, len(deps))
	}
}

// TestPrincipalContractIsPureAndCoreDoesNotDependOnIt keeps the host-neutral
// principal vocabulary (WP-M5-1) free of persistence, control-plane, state,
// event and adapter dependencies, and keeps the core and persistence layers
// from depending on a wire vocabulary. Only the application service may
// import it. The check fails rather than skipping when the dependency list is
// empty, so it cannot pass vacuously.
func TestPrincipalContractIsPureAndCoreDoesNotDependOnIt(t *testing.T) {
	const (
		module = "github.com/olostan/DevCadence/internal/"
		pkg    = module + "principal"
	)
	deps := dependenciesOf(t, pkg)
	if len(deps) < 2 {
		t.Fatalf("dependency list of %s is implausibly small (%d); the check would be vacuous", pkg, len(deps))
	}
	allowed := map[string]bool{pkg: true, module + "errs": true}
	for _, dep := range deps {
		firstSegment, _, _ := strings.Cut(dep, "/")
		if !strings.Contains(firstSegment, ".") {
			continue // standard library
		}
		if !allowed[dep] {
			t.Errorf("internal/principal depends on %s; the contract may import only internal/errs", dep)
		}
	}
	for _, dependant := range []string{
		module + "protocol", module + "events", module + "tasks", module + "state", module + "storage",
		module + "schema",
	} {
		for _, dep := range dependenciesOf(t, dependant) {
			if dep == pkg {
				t.Errorf("%s depends on internal/principal; the wire vocabulary sits above the core", dependant)
			}
		}
	}
	for _, adapter := range adapterPackages {
		for _, dep := range dependenciesOf(t, adapter) {
			if dep == pkg {
				t.Errorf("provider adapter %s depends on internal/principal", adapter)
			}
		}
	}
}

// TestPrincipalHostsStayOutsideTheSemanticLayers keeps the host adapter on its
// declared dependency edge (WP-M5-4): it must not import the MCP SDK, the MCP
// adapter or the facade (it speaks the same wire bytes), and no core package
// may import it, so host-specific types never reach core schemas.
func TestPrincipalHostsStayOutsideTheSemanticLayers(t *testing.T) {
	const mod = "github.com/olostan/DevCadence/"
	deps := dependenciesOf(t, mod+"internal/principalhosts")
	for _, dep := range deps {
		for _, banned := range []string{"github.com/modelcontextprotocol", mod + "internal/mcpadapter", mod + "internal/principal", mod + "internal/controlplane", mod + "internal/storage"} {
			if dep == banned || strings.HasPrefix(dep, banned+"/") {
				t.Errorf("internal/principalhosts depends on %s", dep)
			}
		}
	}
	for _, pkg := range append([]string{mod + "internal/principal", mod + "internal/principal/facade", mod + "internal/mcpadapter", mod + "internal/controlplane"}, coreDomainPackages...) {
		for _, dep := range dependenciesOf(t, pkg) {
			if dep == mod+"internal/principalhosts" {
				t.Errorf("%s depends on internal/principalhosts", pkg)
			}
		}
	}
}

// TestEmpiricalAdmissionIsPureAndOffline keeps the WP-M5-5 admission contract a
// pure data transformation: its own (non-test) imports exclude network, process,
// filesystem, providers, credentials and persistence, so it cannot spend or call
// endpoints. Transitive effects of the existing gate packages are unchanged.
func TestEmpiricalAdmissionIsPureAndOffline(t *testing.T) {
	const mod = "github.com/olostan/DevCadence/"
	cmd := exec.Command("go", "list", "-f", "{{join .Imports \"\\n\"}}", mod+"internal/benchmark/empirical")
	cmd.Dir = ".."
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list failed: %v", err)
	}
	imports := strings.Fields(string(out))
	if len(imports) == 0 {
		t.Fatal("no imports resolved; test would pass vacuously")
	}
	banned := []string{"net", "os", "os/exec", "syscall", mod + "internal/cognition", mod + "internal/credentials",
		mod + "internal/controlplane", mod + "internal/storage", mod + "internal/mcpadapter", mod + "internal/principal"}
	for _, imp := range imports {
		for _, b := range banned {
			if imp == b || strings.HasPrefix(imp, b+"/") {
				t.Errorf("internal/benchmark/empirical imports %s", imp)
			}
		}
	}
	for _, pkg := range append([]string{mod + "internal/principal", mod + "internal/mcpadapter", mod + "internal/controlplane"}, coreDomainPackages...) {
		for _, dep := range dependenciesOf(t, pkg) {
			if dep == mod+"internal/benchmark/empirical" {
				t.Errorf("%s depends on internal/benchmark/empirical", pkg)
			}
		}
	}
}
