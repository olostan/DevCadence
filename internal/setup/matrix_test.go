package setup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/credentials"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/schema"
)

// matrixTestEnv creates an isolated test environment with a fake clock and IDs.
func matrixTestEnv(t *testing.T) (*Doctor, string) {
	t.Helper()
	homeDir := t.TempDir()
	stateDir := filepath.Join(homeDir, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	fixedTime := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	clk := clock.NewFake(fixedTime, 0)
	seq := ids.NewSequential()

	doc, err := NewDoctor(DoctorOptions{
		Clock:   clk,
		IDs:     seq,
		HomeDir: homeDir,
	})
	if err != nil {
		t.Fatalf("NewDoctor: %v", err)
	}
	return doc, homeDir
}

// 1. blank machine
func TestMatrix_Scenario01_BlankMachine(t *testing.T) {
	doc, _ := matrixTestEnv(t)
	facts := protocol.EnvironmentFacts{
		Host: protocol.HostFacts{Family: protocol.OSLinux, Arch: "amd64"},
	}

	scope := protocol.ReadinessEvaluationScope{
		RequiredRoles:  []string{},
		EvidenceStatus: "live",
	}

	report, err := doc.Run(context.Background(), scope, facts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// On a blank machine without git or endpoints, readiness is ACTION_REQUIRED (Git missing) or PARTIALLY_READY
	if report.Readiness != protocol.ReadinessActionRequired && report.Readiness != protocol.ReadinessPartiallyReady {
		t.Errorf("expected ACTION_REQUIRED or PARTIALLY_READY on blank machine, got %s", report.Readiness)
	}

	// Verify scope readiness
	var hasAnyViable protocol.ScopeReadinessStatus
	for _, sr := range report.ScopeReadiness {
		if sr.Scope == protocol.ScopeHasAnyViableCognitionPath {
			hasAnyViable = sr.Status
		}
	}
	if hasAnyViable != protocol.ScopeStatusUnavailable {
		t.Errorf("expected has_any_viable_cognition_path to be unavailable, got %s", hasAnyViable)
	}
}

// 2. CPU-only machine
func TestMatrix_Scenario02_CPUOnlyMachine(t *testing.T) {
	doc, _ := matrixTestEnv(t)
	cores := 4
	var mem int64 = 8 * 1024 * 1024 * 1024
	facts := protocol.EnvironmentFacts{
		Host:   protocol.HostFacts{Family: protocol.OSLinux, Arch: "amd64"},
		CPU:    protocol.CPUFacts{LogicalCores: &cores},
		Memory: protocol.MemoryFacts{TotalBytes: &mem},
	}

	inv, err := doc.BuildResourceInventory(context.Background(), facts, "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildResourceInventory: %v", err)
	}

	if len(inv.Hardware.AcceleratorBackends) != 1 || inv.Hardware.AcceleratorBackends[0] != protocol.BackendCPU {
		t.Errorf("expected CPU-only backend, got %v", inv.Hardware.AcceleratorBackends)
	}
}

// 3. local runtime installed but no model
func TestMatrix_Scenario03_LocalRuntimeInstalledButNoModel(t *testing.T) {
	endpoints := []protocol.CognitionEndpointSummary{
		{
			ID:       "ollama:base",
			Kind:     protocol.EndpointLocalRuntime,
			Locality: protocol.LocalityLocal,
			Health:   protocol.EndpointHealthNotConfigured, // runtime present, but model not present
		},
	}

	readiness := protocol.EvaluateScopeReadiness(nil, endpoints, nil)
	findScope := func(k protocol.ScopeKind) protocol.ScopeReadiness {
		for _, r := range readiness {
			if r.Scope == k {
				return r
			}
		}
		t.Fatalf("scope %q not found", k)
		return protocol.ScopeReadiness{}
	}

	rLocalInf := findScope(protocol.ScopeCanRunLocalInference)
	if rLocalInf.Status != protocol.ScopeStatusNotReady {
		t.Errorf("expected can_run_local_inference to be not_ready, got %s", rLocalInf.Status)
	}
	rModel := findScope(protocol.ScopeLocalModelAvailable)
	if rModel.Status != protocol.ScopeStatusNotReady {
		t.Errorf("expected local_model_available to be not_ready, got %s", rModel.Status)
	}
}

// 4. local runtime + verified usable model
func TestMatrix_Scenario04_LocalRuntimeAndVerifiedUsableModel(t *testing.T) {
	endpoints := []protocol.CognitionEndpointSummary{
		{
			ID:       "ollama:qwen2.5-coder",
			Kind:     protocol.EndpointLocalRuntime,
			Locality: protocol.LocalityLocal,
			Health:   protocol.EndpointHealthReady,
		},
	}

	readiness := protocol.EvaluateScopeReadiness(nil, endpoints, nil)
	findScope := func(k protocol.ScopeKind) protocol.ScopeReadiness {
		for _, r := range readiness {
			if r.Scope == k {
				return r
			}
		}
		t.Fatalf("scope %q not found", k)
		return protocol.ScopeReadiness{}
	}

	if s := findScope(protocol.ScopeCanRunLocalInference).Status; s != protocol.ScopeStatusReady {
		t.Errorf("expected can_run_local_inference ready, got %s", s)
	}
	if s := findScope(protocol.ScopeLocalModelAvailable).Status; s != protocol.ScopeStatusReady {
		t.Errorf("expected local_model_available ready, got %s", s)
	}
}

// 5. authenticated CLI with no local inference
func TestMatrix_Scenario05_AuthenticatedCLIWithNoLocalInference(t *testing.T) {
	endpoints := []protocol.CognitionEndpointSummary{
		{
			ID:                     "claude-code",
			Kind:                   protocol.EndpointAuthenticatedCLI,
			Locality:               protocol.LocalityRemote,
			Health:                 protocol.EndpointHealthReady,
			Auth:                   protocol.AuthAuthenticated,
			CostClass:              protocol.CostSubscriptionIncluded,
			RequiredSourceExposure: protocol.ExposureFocusedSnippets,
		},
	}

	readiness := protocol.EvaluateScopeReadiness(nil, endpoints, nil)
	findScope := func(k protocol.ScopeKind) protocol.ScopeReadiness {
		for _, r := range readiness {
			if r.Scope == k {
				return r
			}
		}
		t.Fatalf("scope %q not found", k)
		return protocol.ScopeReadiness{}
	}

	if s := findScope(protocol.ScopeCanUseAuthenticatedCLI).Status; s != protocol.ScopeStatusReady {
		t.Errorf("expected can_use_existing_authenticated_cli ready, got %s", s)
	}
	if s := findScope(protocol.ScopeCanRunLocalInference).Status; s != protocol.ScopeStatusUnavailable {
		t.Errorf("expected can_run_local_inference unavailable, got %s", s)
	}
	if s := findScope(protocol.ScopeHasAnyViableCognitionPath).Status; s != protocol.ScopeStatusReady {
		t.Errorf("expected has_any_viable_cognition_path ready, got %s", s)
	}
}

// 6. installed CLI but authentication unknown/expired
func TestMatrix_Scenario06_InstalledCLIButAuthUnknown(t *testing.T) {
	endpoints := []protocol.CognitionEndpointSummary{
		{
			ID:                     "claude-code",
			Kind:                   protocol.EndpointAuthenticatedCLI,
			Locality:               protocol.LocalityRemote,
			Health:                 protocol.EndpointHealthReady,
			Auth:                   protocol.AuthExpired,
			CostClass:              protocol.CostSubscriptionIncluded,
			RequiredSourceExposure: protocol.ExposureFocusedSnippets,
		},
	}

	readiness := protocol.EvaluateScopeReadiness(nil, endpoints, nil)
	for _, r := range readiness {
		if r.Scope == protocol.ScopeCanUseAuthenticatedCLI {
			if r.Status != protocol.ScopeStatusNotReady {
				t.Errorf("expected not_ready for expired CLI auth, got %s", r.Status)
			}
		}
	}
}

// 7. credential env var present but unverified
func TestMatrix_Scenario07_CredentialEnvVarPresentUnverified(t *testing.T) {
	doc, _ := matrixTestEnv(t)
	clk := clock.NewFake(time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC), 0)
	ref := protocol.CredentialRef{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-anthropic",
		Kind:          protocol.CredRefEnvVar,
		Locator:       "ANTHROPIC_API_KEY",
	}

	envReader := credentials.NewMapEnvReader(map[string]string{
		"ANTHROPIC_API_KEY": "dummy-token",
	})
	mgr, err := credentials.NewManager(credentials.Options{
		Clock: clk,
		Env:   envReader,
	})
	if err != nil {
		t.Fatal(err)
	}
	doc.credManager = mgr
	doc.credRefs = []protocol.CredentialRef{ref}

	facts := protocol.EnvironmentFacts{
		Host: protocol.HostFacts{Family: protocol.OSLinux, Arch: "amd64"},
	}
	inv, err := doc.BuildResourceInventory(context.Background(), facts, "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildResourceInventory: %v", err)
	}

	if len(inv.Credentials) != 1 {
		t.Fatalf("expected 1 credential entry, got %d", len(inv.Credentials))
	}
	// Status must be indeterminate per WP-M3B-4 invariant: env var presence is indeterminate, not authenticated
	if inv.Credentials[0].Evidence.Status != protocol.AuthStatusIndeterminate {
		t.Errorf("expected indeterminate status for env var presence, got %s", inv.Credentials[0].Evidence.Status)
	}
}

// 8. unsupported keychain backend
func TestMatrix_Scenario08_UnsupportedKeychainBackend(t *testing.T) {
	doc, _ := matrixTestEnv(t)
	clk := clock.NewFake(time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC), 0)
	ref := protocol.CredentialRef{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-keychain",
		Kind:          protocol.CredRefKeychainRef,
		Locator:       "service/account",
	}
	mgr, err := credentials.NewManager(credentials.Options{
		Clock:    clk,
		Keychain: credentials.UnsupportedKeychainChecker{},
	})
	if err != nil {
		t.Fatal(err)
	}
	doc.credManager = mgr
	doc.credRefs = []protocol.CredentialRef{ref}

	facts := protocol.EnvironmentFacts{
		Host: protocol.HostFacts{Family: protocol.OSLinux, Arch: "amd64"},
	}
	inv, err := doc.BuildResourceInventory(context.Background(), facts, "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildResourceInventory: %v", err)
	}

	if len(inv.Credentials) != 1 {
		t.Fatalf("expected 1 credential entry, got %d", len(inv.Credentials))
	}
	if inv.Credentials[0].Evidence.Status != protocol.AuthStatusUnavailable {
		t.Errorf("expected unavailable status for unsupported keychain, got %s", inv.Credentials[0].Evidence.Status)
	}
}

// 9. multiple cognition endpoints
func TestMatrix_Scenario09_MultipleCognitionEndpoints(t *testing.T) {
	endpoints := []protocol.CognitionEndpointSummary{
		{
			ID:       "ollama:qwen",
			Kind:     protocol.EndpointLocalRuntime,
			Locality: protocol.LocalityLocal,
			Health:   protocol.EndpointHealthReady,
		},
		{
			ID:       "claude-code",
			Kind:     protocol.EndpointAuthenticatedCLI,
			Locality: protocol.LocalityRemote,
			Health:   protocol.EndpointHealthReady,
			Auth:     protocol.AuthAuthenticated,
		},
	}

	readiness := protocol.EvaluateScopeReadiness(nil, endpoints, nil)
	for _, r := range readiness {
		if r.Scope == protocol.ScopeHasAnyViableCognitionPath {
			if r.Status != protocol.ScopeStatusReady {
				t.Errorf("expected ready cognition path, got %s", r.Status)
			}
		}
	}
}

// 10. no cognition endpoint
func TestMatrix_Scenario10_NoCognitionEndpoint(t *testing.T) {
	readiness := protocol.EvaluateScopeReadiness(nil, nil, nil)
	for _, r := range readiness {
		if r.Scope == protocol.ScopeHasAnyViableCognitionPath {
			if r.Status != protocol.ScopeStatusUnavailable {
				t.Errorf("expected unavailable when no endpoints, got %s", r.Status)
			}
		}
	}
}

// 11. principal host present / absent
func TestMatrix_Scenario11_PrincipalHostPresentVsAbsent(t *testing.T) {
	hostsPresent := []protocol.PrincipalHostSummary{
		{HostID: "vscode", Installed: true},
	}
	r1 := protocol.EvaluateScopeReadiness(nil, nil, hostsPresent)
	for _, r := range r1 {
		if r.Scope == protocol.ScopePrincipalHostAvailable && r.Status != protocol.ScopeStatusReady {
			t.Errorf("expected ready host, got %s", r.Status)
		}
	}

	hostsAbsent := []protocol.PrincipalHostSummary{
		{HostID: "cursor", Installed: false},
	}
	r2 := protocol.EvaluateScopeReadiness(nil, nil, hostsAbsent)
	for _, r := range r2 {
		if r.Scope == protocol.ScopePrincipalHostAvailable && r.Status != protocol.ScopeStatusUnavailable {
			t.Errorf("expected unavailable host, got %s", r.Status)
		}
	}
}

// 12. runtime present but acceleration unavailable
func TestMatrix_Scenario12_RuntimePresentAccelerationUnavailable(t *testing.T) {
	endpoints := []protocol.CognitionEndpointSummary{
		{
			ID:                   "ollama:qwen",
			Kind:                 protocol.EndpointLocalRuntime,
			Locality:             protocol.LocalityLocal,
			Health:               protocol.EndpointHealthReady,
			AccelerationVerified: false,
		},
	}

	readiness := protocol.EvaluateScopeReadiness(nil, endpoints, nil)
	for _, r := range readiness {
		if r.Scope == protocol.ScopeCanRunLocalInference {
			if r.Status != protocol.ScopeStatusReady {
				t.Errorf("expected can_run_local_inference ready even if unaccelerated, got %s", r.Status)
			}
		}
	}
}

// 13. partially stale/incomplete evidence
func TestMatrix_Scenario13_PartiallyStaleEvidence(t *testing.T) {
	doc, _ := matrixTestEnv(t)
	scope := protocol.ReadinessEvaluationScope{
		RequiredRoles:  []string{},
		EvidenceStatus: "stale_inference_retained",
	}
	endpoints := []protocol.CognitionEndpointSummary{
		{
			ID:       "claude-code",
			Kind:     protocol.EndpointAuthenticatedCLI,
			Locality: protocol.LocalityRemote,
			Health:   protocol.EndpointHealthReady,
			Auth:     protocol.AuthAuthenticated,
		},
	}

	status := doc.evaluateReadiness(scope, nil, endpoints, nil, nil)
	if status != protocol.ReadinessReadyWithReducedCap {
		t.Errorf("expected READY_WITH_REDUCED_CAPABILITY on stale evidence, got %s", status)
	}
}

// 14. one capability unavailable without invalidating unrelated capabilities
func TestMatrix_Scenario14_OneCapabilityUnavailableWithoutInvalidatingOthers(t *testing.T) {
	endpoints := []protocol.CognitionEndpointSummary{
		{
			ID:       "claude-code",
			Kind:     protocol.EndpointAuthenticatedCLI,
			Locality: protocol.LocalityRemote,
			Health:   protocol.EndpointHealthReady,
			Auth:     protocol.AuthAuthenticated,
		},
	}

	// No local runtime, no principal host
	readiness := protocol.EvaluateScopeReadiness(nil, endpoints, nil)
	findScope := func(k protocol.ScopeKind) protocol.ScopeReadiness {
		for _, r := range readiness {
			if r.Scope == k {
				return r
			}
		}
		t.Fatalf("scope %q not found", k)
		return protocol.ScopeReadiness{}
	}

	if s := findScope(protocol.ScopeCanRunLocalInference).Status; s != protocol.ScopeStatusUnavailable {
		t.Errorf("expected local inference unavailable, got %s", s)
	}
	if s := findScope(protocol.ScopeCanUseAuthenticatedCLI).Status; s != protocol.ScopeStatusReady {
		t.Errorf("expected authenticated CLI ready, got %s", s)
	}
	if s := findScope(protocol.ScopeHasAnyViableCognitionPath).Status; s != protocol.ScopeStatusReady {
		t.Errorf("expected viable cognition path ready, got %s", s)
	}
}

// 15. deterministic ordering/stable serialization of ScopeReadiness
// specifically (not the full ResourceInventory — see
// TestMatrix_Scenario19b_ResourceInventoryOrderIndependence for that,
// added per the independent-review follow-up on WP-M3B-5's matrix note).
func TestMatrix_Scenario15_DeterministicOrdering(t *testing.T) {
	endpoints := []protocol.CognitionEndpointSummary{
		{ID: "ollama:m", Kind: protocol.EndpointLocalRuntime, Health: protocol.EndpointHealthReady},
	}
	readiness1 := protocol.EvaluateScopeReadiness(nil, endpoints, nil)
	readiness2 := protocol.EvaluateScopeReadiness(nil, endpoints, nil)

	b1, err := json.Marshal(readiness1)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := json.Marshal(readiness2)
	if err != nil {
		t.Fatal(err)
	}
	if string(b1) != string(b2) {
		t.Errorf("serialization is not stable: %s vs %s", b1, b2)
	}

	for i, scope := range protocol.CanonicalScopes {
		if readiness1[i].Scope != scope {
			t.Errorf("expected scope %d to be %s, got %s", i, scope, readiness1[i].Scope)
		}
	}
}

// 16. no secret material in serialized inventory/report
func TestMatrix_Scenario16_NoSecretMaterial(t *testing.T) {
	doc, _ := matrixTestEnv(t)
	clk := clock.NewFake(time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC), 0)
	ref := protocol.CredentialRef{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-api",
		Kind:          protocol.CredRefEnvVar,
		Locator:       "SAFE_VAR_NAME",
	}
	envReader := credentials.NewMapEnvReader(map[string]string{
		"SAFE_VAR_NAME": "secret_key_12345",
	})
	mgr, err := credentials.NewManager(credentials.Options{Clock: clk, Env: envReader})
	if err != nil {
		t.Fatal(err)
	}
	doc.credManager = mgr
	doc.credRefs = []protocol.CredentialRef{ref}

	facts := protocol.EnvironmentFacts{
		Host: protocol.HostFacts{Family: protocol.OSLinux, Arch: "amd64"},
	}
	inv, err := doc.BuildResourceInventory(context.Background(), facts, "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	b, err := json.Marshal(inv)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secret_key_12345") {
		t.Fatal("secret value leaked in ResourceInventory JSON!")
	}
}

// 17. Go/schema round-trip and one negative parity case (an invalid scope
// name) at the top level. The nested CredentialRef/AuthEvidence structural
// parity cases (the WP-M3B-4 rules a forked local schema copy previously
// missed) are in TestResourceInventorySchemaParity
// (internal/protocol/resource_inventory_test.go), added per the
// independent-review follow-up on WP-M3B-5, finding 4a.
func TestMatrix_Scenario17_GoSchemaParity(t *testing.T) {
	validJSON, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "protocol", "resource-inventory.valid.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	// 1. Schema check
	s, err := schema.Default()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ValidateBytes(schema.NameResourceInventory, validJSON); err != nil {
		t.Fatalf("valid fixture failed schema validation: %v", err)
	}

	// 2. Go unmarshal and validate
	var inv protocol.ResourceInventory
	if err := json.Unmarshal(validJSON, &inv); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := inv.Validate(); err != nil {
		t.Fatalf("inv.Validate(): %v", err)
	}

	// 3. Negative case: invalid scope
	inv.Readiness = append(inv.Readiness, protocol.ScopeReadiness{
		Scope:  "invalid_scope_name",
		Status: protocol.ScopeStatusReady,
		Reason: "bad",
	})
	if err := inv.Validate(); err == nil {
		t.Fatal("expected Go validation to reject invalid scope name")
	}
	badJSON, _ := json.Marshal(inv)
	if err := s.ValidateBytes(schema.NameResourceInventory, badJSON); err == nil {
		t.Fatal("expected JSON schema to reject invalid scope name")
	}
}

// 18. Windows platform/path compatibility in ResourceInventory and DoctorReport
func TestMatrix_Scenario18_WindowsCompatibility(t *testing.T) {
	doc, _ := matrixTestEnv(t)
	facts := protocol.EnvironmentFacts{
		Host: protocol.HostFacts{
			Family:  protocol.OSWindows,
			Arch:    "amd64",
			Version: "10.0.19041",
		},
	}
	hosts := []protocol.PrincipalHostSummary{
		{
			HostID:    "cursor",
			Installed: true,
			Path:      `C:\Users\Developer\AppData\Local\Programs\cursor\Cursor.exe`,
			Version:   "0.40.0",
		},
	}
	endpoints := []protocol.CognitionEndpointSummary{
		{
			ID:                     "claude-code",
			Kind:                   protocol.EndpointAuthenticatedCLI,
			Locality:               protocol.LocalityRemote,
			Health:                 protocol.EndpointHealthReady,
			Auth:                   protocol.AuthAuthenticated,
			CostClass:              protocol.CostSubscriptionIncluded,
			RequiredSourceExposure: protocol.ExposureFocusedSnippets,
		},
	}

	cognProfile := &protocol.MachineCapabilityProfile{
		ProfileID:          "mcp-windows",
		MachineFingerprint: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ObservedAt:         protocol.NewTimestamp(doc.clock.Now()),
		ProbeDepth:         protocol.DepthHealth,
	}

	inv, err := doc.BuildResourceInventory(context.Background(), facts, "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", endpoints, hosts, cognProfile, nil)
	if err != nil {
		t.Fatalf("BuildResourceInventory failed on Windows facts: %v", err)
	}
	if err := inv.Validate(); err != nil {
		t.Fatalf("Windows ResourceInventory failed validation: %v", err)
	}

	scope := protocol.ReadinessEvaluationScope{
		EvidenceStatus: "live",
	}
	report, err := doc.Run(context.Background(), scope, facts)
	if err != nil {
		t.Fatalf("Doctor.Run failed on Windows facts: %v", err)
	}
	if report.MachineFingerprint == "" {
		t.Errorf("expected valid machine fingerprint on Windows facts")
	}
}

// 19. protocol.EvaluateScopeReadiness's own output is order-independent
// regardless of input endpoint order. See
// TestMatrix_Scenario19b_ResourceInventoryOrderIndependence immediately
// below for the full-ResourceInventory version of this claim
// (independent-review follow-up on WP-M3B-5: the original version of this
// scenario's name/comment claimed the fuller guarantee without testing it).
func TestMatrix_Scenario19_OrderIndependence(t *testing.T) {
	ep1 := protocol.CognitionEndpointSummary{ID: "a", Kind: protocol.EndpointLocalRuntime, Health: protocol.EndpointHealthReady}
	ep2 := protocol.CognitionEndpointSummary{ID: "b", Kind: protocol.EndpointAuthenticatedCLI, Health: protocol.EndpointHealthReady, Auth: protocol.AuthAuthenticated}

	r1 := protocol.EvaluateScopeReadiness(nil, []protocol.CognitionEndpointSummary{ep1, ep2}, nil)
	r2 := protocol.EvaluateScopeReadiness(nil, []protocol.CognitionEndpointSummary{ep2, ep1}, nil)

	b1, _ := json.Marshal(r1)
	b2, _ := json.Marshal(r2)
	if string(b1) != string(b2) {
		t.Errorf("readiness differed based on endpoint ordering: %s vs %s", b1, b2)
	}
}

// TestMatrix_Scenario19b_ResourceInventoryOrderIndependence is scenario 19's
// missing half: the original scenario only compared
// protocol.EvaluateScopeReadiness's own output order-independence, never
// actually built two ResourceInventory values from differently-ordered
// input and compared them (independent-review follow-up on WP-M3B-5,
// finding 3's order-independence requirement). This builds two full
// inventories — differing only in the order of CognitionEndpoints,
// PrincipalHosts and CredentialRefs given to BuildResourceInventory — and
// asserts their sorted collections (Hardware.AcceleratorBackends,
// CognitionEndpoints, PrincipalHosts, Credentials, Readiness) serialize
// identically once the two non-deterministic-by-construction fields
// (InventoryID, from a sequential ID source; ObservedAt, only different if
// the clock ticks between calls) are normalized away.
func TestMatrix_Scenario19b_ResourceInventoryOrderIndependence(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()

	env := credentials.NewMapEnvReader(map[string]string{"KEY_A": "present", "KEY_B": "present"})
	mgr, err := credentials.NewManager(credentials.Options{Clock: clk, Env: env})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	refA := protocol.CredentialRef{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-a", Kind: protocol.CredRefEnvVar, Locator: "KEY_A"}
	refB := protocol.CredentialRef{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-b", Kind: protocol.CredRefEnvVar, Locator: "KEY_B"}

	doc, err := NewDoctor(DoctorOptions{
		Clock:             clk,
		IDs:               seq,
		HomeDir:           t.TempDir(),
		CredentialManager: mgr,
		CredentialRefs:    []protocol.CredentialRef{refA, refB},
	})
	if err != nil {
		t.Fatalf("NewDoctor: %v", err)
	}

	facts := protocol.EnvironmentFacts{
		Host: protocol.HostFacts{Family: protocol.OSLinux, Arch: "amd64"},
	}
	fp := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	epA := protocol.CognitionEndpointSummary{ID: "a-runtime", Kind: protocol.EndpointLocalRuntime, Locality: protocol.LocalityLocal, Health: protocol.EndpointHealthReady, Auth: protocol.AuthNotApplicable, CostClass: protocol.CostLocalCompute, RequiredSourceExposure: protocol.ExposureLocalOnly}
	epB := protocol.CognitionEndpointSummary{ID: "b-cli", Kind: protocol.EndpointAuthenticatedCLI, Locality: protocol.LocalityRemote, Health: protocol.EndpointHealthReady, Auth: protocol.AuthAuthenticated, CostClass: protocol.CostSubscriptionIncluded, RequiredSourceExposure: protocol.ExposureFocusedSnippets}
	hostA := protocol.PrincipalHostSummary{HostID: "cursor", Installed: true}
	hostB := protocol.PrincipalHostSummary{HostID: "vscode", Installed: true}

	scopeReadiness := protocol.EvaluateScopeReadiness(nil, []protocol.CognitionEndpointSummary{epA, epB}, []protocol.PrincipalHostSummary{hostA, hostB})

	cognProfile := &protocol.MachineCapabilityProfile{
		ProfileID:          "mcp-order-test",
		MachineFingerprint: fp,
		ObservedAt:         protocol.NewTimestamp(clk.Now()),
		ProbeDepth:         protocol.DepthHealth,
	}

	inv1, err := doc.BuildResourceInventory(context.Background(), facts, fp,
		[]protocol.CognitionEndpointSummary{epA, epB},
		[]protocol.PrincipalHostSummary{hostA, hostB},
		cognProfile, scopeReadiness)
	if err != nil {
		t.Fatalf("BuildResourceInventory (order 1): %v", err)
	}

	inv2, err := doc.BuildResourceInventory(context.Background(), facts, fp,
		[]protocol.CognitionEndpointSummary{epB, epA},
		[]protocol.PrincipalHostSummary{hostB, hostA},
		cognProfile, scopeReadiness)
	if err != nil {
		t.Fatalf("BuildResourceInventory (order 2): %v", err)
	}

	// Normalize the two fields that are legitimately allowed to differ
	// between separate calls (a fresh sequential ID; observed_at would
	// differ only if the clock ticked, which this fake clock does not).
	inv1.InventoryID = ""
	inv2.InventoryID = ""

	b1, _ := json.Marshal(inv1)
	b2, _ := json.Marshal(inv2)
	if string(b1) != string(b2) {
		t.Errorf("ResourceInventory differed based on input ordering:\n  order 1: %s\n  order 2: %s", b1, b2)
	}
}

// 20. old static-profile recommendation no longer influences canonical
// readiness/output — exercised through the real Doctor.Run path (not the
// private evaluateReadiness helper directly), so the informational
// RecommendedProfile is actually the same value Run computes and could, if
// the finding-1 bug were still present, get silently promoted into
// EvaluationScope.TargetProfile and gate readiness (independent-review
// follow-up on WP-M3B-5: the prior version of this scenario called a
// private helper and never caught that the real Run path still injected
// the static profile).
func TestMatrix_Scenario20_StaticProfileNoLongerGatesReadiness(t *testing.T) {
	homeDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(homeDir, "state"), 0o700); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(homeDir, "artifacts", "setup"), 0o700); err != nil {
		t.Fatalf("mkdir artifacts/setup: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(homeDir, "tmp"), 0o700); err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	clk := clock.NewFake(time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC), 0)
	idSrc := ids.NewSequential()

	var mem int64 = 64 * 1024 * 1024 * 1024
	facts := protocol.EnvironmentFacts{
		Host:   protocol.HostFacts{Family: protocol.OSLinux, Arch: "amd64"},
		Memory: protocol.MemoryFacts{TotalBytes: &mem},
		Virtualization: protocol.VirtualizationFacts{
			Container: protocol.ContainerNone,
		},
		Software: []protocol.SoftwarePresence{
			{ID: "git", Category: protocol.SoftwareEngineering, Installed: true, Version: "2.40.0"},
		},
	}

	// An authenticated CLI (viable) plus an unaccelerated local runtime
	// (makes local-heavy ineligible for the informational recommendation,
	// but must not affect canonical readiness).
	stub := &stubCognitionAdapter{
		endpoints: []protocol.CognitionEndpoint{
			{
				ID:                     "claude-code",
				Kind:                   protocol.EndpointAuthenticatedCLI,
				Locality:               protocol.LocalityRemote,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthAuthenticated,
				CostClass:              protocol.CostSubscriptionIncluded,
				RequiredSourceExposure: protocol.ExposureFocusedSnippets,
				StructuredOutput:       protocol.FeatureDeclared,
				ToolUse:                protocol.FeatureDeclared,
				ObservedAt:             protocol.NewTimestamp(clk.Now()),
			},
			{
				ID:                     "ollama:qwen",
				Kind:                   protocol.EndpointLocalRuntime,
				Locality:               protocol.LocalityLocal,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthNotApplicable,
				CostClass:              protocol.CostLocalCompute,
				RequiredSourceExposure: protocol.ExposureLocalOnly,
				StructuredOutput:       protocol.FeatureUnsupported,
				ToolUse:                protocol.FeatureUnsupported,
				ObservedAt:             protocol.NewTimestamp(clk.Now()),
				Acceleration: &protocol.AccelerationEvidence{
					Backend: protocol.BackendCPU,
					State:   protocol.StateUnverified,
				},
			},
		},
	}
	service, err := cognition.NewService(cognition.Options{
		Adapters: []cognition.Adapter{stub},
		Clock:    clk,
		IDs:      idSrc,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	doc, err := NewDoctor(DoctorOptions{
		Clock:            clk,
		IDs:              idSrc,
		HomeDir:          homeDir,
		CognitionService: service,
	})
	if err != nil {
		t.Fatalf("NewDoctor: %v", err)
	}

	scope := protocol.ReadinessEvaluationScope{
		TargetProfile:  nil, // no profile forced by the caller
		RequiredRoles:  []string{},
		EvidenceStatus: "live",
	}

	report, err := doc.Run(context.Background(), scope, facts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The recommendation is informational: local-heavy must show
	// ineligible (unaccelerated local runtime), proving the scenario
	// actually constructs the "recommendation would disagree with
	// canonical readiness" case the finding was about.
	foundHeavy := false
	if report.RecommendedProfile != nil {
		for _, alt := range report.RecommendedProfile.Alternatives {
			if alt.Profile == protocol.ProfileLocalHeavy {
				foundHeavy = true
				if alt.Eligible {
					t.Errorf("expected local-heavy to be ineligible without verified acceleration")
				}
			}
		}
	}
	if !foundHeavy {
		t.Fatalf("expected local-heavy in alternatives")
	}

	// The real Run path must not have promoted RecommendedProfile into
	// EvaluationScope.TargetProfile.
	if report.EvaluationScope.TargetProfile != nil {
		t.Errorf("EvaluationScope.TargetProfile = %v, want nil (never auto-set from RecommendedProfile)", *report.EvaluationScope.TargetProfile)
	}

	// Canonical readiness is READY or READY_WITH_REDUCED_CAPABILITY (the
	// latter only because this facts set declares no verified hardware
	// accelerator at all, an honest warning unrelated to this scenario) —
	// never PARTIALLY_READY/ACTION_REQUIRED, which is what the bug this
	// scenario targets would have produced: local-heavy is ineligible, so
	// a static-profile-gated readiness would have downgraded to
	// PARTIALLY_READY even though an authenticated CLI provides a
	// perfectly viable cognition path.
	if report.Readiness != protocol.ReadinessReady && report.Readiness != protocol.ReadinessReadyWithReducedCap {
		t.Errorf("Readiness = %s, want READY or READY_WITH_REDUCED_CAPABILITY (authenticated CLI is a viable path; no profile gate should apply)", report.Readiness)
	}

	findScope := func(k protocol.ScopeKind) protocol.ScopeReadiness {
		for _, r := range report.ScopeReadiness {
			if r.Scope == k {
				return r
			}
		}
		t.Fatalf("scope %q not found", k)
		return protocol.ScopeReadiness{}
	}
	if s := findScope(protocol.ScopeCanUseAuthenticatedCLI).Status; s != protocol.ScopeStatusReady {
		t.Errorf("expected authenticated CLI ready, got %s", s)
	}
	if s := findScope(protocol.ScopeHasAnyViableCognitionPath).Status; s != protocol.ScopeStatusReady {
		t.Errorf("expected viable cognition path ready, got %s", s)
	}
}

// TestPlannerRecommendationLabelAloneCannotChangeSetupPlan proves a
// DoctorReport's RecommendedProfile.SelectedProfile has no effect on which
// SetupActions Planner.Plan generates: only the caller-supplied profile
// parameter and concrete facts (DiscoveredEndpoints, selected runtimes) may
// (independent-review follow-up on WP-M3B-5, finding 1's required
// regression).
func TestPlannerRecommendationLabelAloneCannotChangeSetupPlan(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC), 0)
	seq := ids.NewSequential()
	planner, err := NewPlanner(PlannerOptions{Clock: clk, IDs: seq})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}

	localHeavy := protocol.ProfileLocalHeavy
	baseReport := func(recommended *protocol.DeploymentProfile) *protocol.DoctorReport {
		r := &protocol.DoctorReport{
			SchemaVersion:      protocol.SchemaVersion1,
			ReportID:           "doc_000000000000000000000099",
			MachineFingerprint: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			ObservedAt:         protocol.NewTimestamp(clk.Now()),
			Readiness:          protocol.ReadinessReady,
			EvaluationScope: protocol.ReadinessEvaluationScope{
				RequiredRoles:  []string{},
				EvidenceStatus: "live",
			},
			// No DiscoveredEndpoints at all: nothing concrete suggests a
			// local model should be pulled.
		}
		if recommended != nil {
			r.RecommendedProfile = &protocol.ProfileRecommendation{
				SelectedProfile: recommended,
				Rationale:       []string{"test rationale"},
				Alternatives: []protocol.ProfileAlternative{
					{Profile: *recommended, Eligible: true, Reasons: []string{"test"}},
				},
			}
		}
		return r
	}

	// With no RecommendedProfile at all, and no explicit profile passed:
	planWithout, err := planner.Plan(baseReport(nil), protocol.TargetAll, "")
	if err != nil {
		t.Fatalf("Plan (no recommendation): %v", err)
	}

	// With RecommendedProfile.SelectedProfile = local-heavy, but still no
	// explicit profile passed by the caller:
	planWithLabel, err := planner.Plan(baseReport(&localHeavy), protocol.TargetAll, "")
	if err != nil {
		t.Fatalf("Plan (with recommendation label): %v", err)
	}

	if len(planWithout.Actions) != len(planWithLabel.Actions) {
		t.Fatalf("RecommendedProfile label changed the plan: %d actions without vs %d with",
			len(planWithout.Actions), len(planWithLabel.Actions))
	}
	for _, act := range planWithLabel.Actions {
		if strings.Contains(act.RecipeID, "ollama") || strings.Contains(act.RecipeID, "mlx") {
			t.Errorf("a local-model action (%s) was generated from RecommendedProfile alone, with no discovered endpoint or explicit selection", act.RecipeID)
		}
	}
}
