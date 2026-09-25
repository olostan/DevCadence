package protocol_test

import (
	"encoding/json"
	"testing"

	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/schema"
)

func validHardwareSummary() protocol.HardwareSummary {
	mem := int64(17179869184)
	return protocol.HardwareSummary{
		OSFamily:            protocol.OSLinux,
		Arch:                "amd64",
		LogicalCores:        8,
		TotalMemoryBytes:    &mem,
		AcceleratorBackends: []protocol.BackendKind{protocol.BackendCPU},
	}
}

func validResourceInventory() protocol.ResourceInventory {
	return protocol.ResourceInventory{
		SchemaVersion:      protocol.SchemaVersion1,
		InventoryID:        "inv-0001",
		MachineFingerprint: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ObservedAt:         validTestTimestamp(),
		Hardware:           validHardwareSummary(),
	}
}

func TestResourceInventoryValidation(t *testing.T) {
	inv := validResourceInventory()
	if err := inv.Validate(); err != nil {
		t.Fatalf("expected valid inventory, got: %v", err)
	}
	if inv.RecordKind() != "ResourceInventory" {
		t.Fatalf("RecordKind() = %q, want ResourceInventory", inv.RecordKind())
	}
	if inv.RecordID() != inv.InventoryID {
		t.Fatalf("RecordID() = %q, want %q", inv.RecordID(), inv.InventoryID)
	}
	if inv.SchemaVer() != protocol.SchemaVersion1 {
		t.Fatalf("SchemaVer() = %q, want %q", inv.SchemaVer(), protocol.SchemaVersion1)
	}
}

func TestResourceInventoryValidation_MissingFields(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*protocol.ResourceInventory)
	}{
		{"missing schema_version", func(i *protocol.ResourceInventory) { i.SchemaVersion = "" }},
		{"missing inventory_id", func(i *protocol.ResourceInventory) { i.InventoryID = "" }},
		{"malformed machine_fingerprint", func(i *protocol.ResourceInventory) { i.MachineFingerprint = "not-a-fingerprint" }},
		{"zero observed_at", func(i *protocol.ResourceInventory) { i.ObservedAt = protocol.Timestamp{} }},
		{"invalid hardware os_family", func(i *protocol.ResourceInventory) { i.Hardware.OSFamily = "plan9" }},
		{"invalid hardware arch", func(i *protocol.ResourceInventory) { i.Hardware.Arch = "" }},
		{"negative logical_cores", func(i *protocol.ResourceInventory) { i.Hardware.LogicalCores = -1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv := validResourceInventory()
			tc.mutate(&inv)
			if err := inv.Validate(); err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}

func TestResourceInventoryValidation_CognitionEndpoints(t *testing.T) {
	inv := validResourceInventory()
	inv.CognitionEndpoints = []protocol.CognitionEndpointSummary{
		{ID: "", Kind: protocol.EndpointLocalRuntime},
	}
	if err := inv.Validate(); err == nil {
		t.Fatal("expected error for endpoint with empty id")
	}

	inv.CognitionEndpoints = []protocol.CognitionEndpointSummary{
		{
			ID:                     "ollama:small",
			Kind:                   protocol.EndpointLocalRuntime,
			Locality:               protocol.LocalityLocal,
			Health:                 protocol.EndpointHealthReady,
			Auth:                   protocol.AuthNotApplicable,
			CostClass:              protocol.CostLocalCompute,
			RequiredSourceExposure: protocol.ExposureLocalOnly,
		},
	}
	// Profile reference is required when cognition endpoints are present
	if err := inv.Validate(); err == nil {
		t.Fatal("expected error when cognition endpoints are present but profile is nil")
	}

	inv.Profile = &protocol.MachineProfileRef{
		ProfileID:          "mcp-001",
		MachineFingerprint: inv.MachineFingerprint,
		ObservedAt:         validTestTimestamp(),
		ProbeDepth:         protocol.DepthHealth,
	}
	if err := inv.Validate(); err != nil {
		t.Fatalf("expected valid endpoint and profile to pass, got: %v", err)
	}

	inv.CognitionEndpoints = []protocol.CognitionEndpointSummary{
		{ID: "ep-1", Kind: "not-a-real-kind"},
	}
	if err := inv.Validate(); err == nil {
		t.Fatal("expected error for endpoint with invalid kind")
	}
}

func TestResourceInventoryValidation_PrincipalHosts(t *testing.T) {
	inv := validResourceInventory()
	inv.PrincipalHosts = []protocol.PrincipalHostSummary{{HostID: "not-a-real-host"}}
	if err := inv.Validate(); err == nil {
		t.Fatal("expected error for invalid principal host id")
	}
	inv.PrincipalHosts = []protocol.PrincipalHostSummary{{HostID: "vscode", Installed: true}}
	if err := inv.Validate(); err != nil {
		t.Fatalf("expected valid principal host to pass, got: %v", err)
	}
}

func TestResourceInventoryValidation_Policy(t *testing.T) {
	inv := validResourceInventory()
	bad := protocol.PolicySummary{MaxSourceExposure: "not-real", MaxCostClass: protocol.CostLocalCompute}
	inv.Policy = &bad
	if err := inv.Validate(); err == nil {
		t.Fatal("expected error for invalid policy max_source_exposure")
	}

	good := protocol.PolicySummary{MaxSourceExposure: protocol.ExposureFocusedSnippets, MaxCostClass: protocol.CostRemoteEconomy}
	inv.Policy = &good
	if err := inv.Validate(); err != nil {
		t.Fatalf("expected valid policy to pass, got: %v", err)
	}
}

// TestResourceInventoryValidation_CredentialEntryBinding proves a
// CredentialInventoryEntry cannot pair a reference with evidence observed
// for a different credential (mismatched ref_id or kind), the same
// integrity guarantee WP-M3B-4 gives AuthEvidence/CredentialRef
// individually, now enforced across the pairing too.
func TestResourceInventoryValidation_CredentialEntryBinding(t *testing.T) {
	ref := protocol.CredentialRef{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-claude",
		Kind:          protocol.CredRefCLISession,
		Locator:       "claude",
	}
	validEvidence := protocol.AuthEvidence{
		SchemaVersion: protocol.SchemaVersion1,
		RefID:         "cred-claude",
		Kind:          protocol.CredRefCLISession,
		Status:        protocol.AuthStatusIndeterminate,
		ProbeKind:     protocol.AuthProbeCLIVersionOnly,
		ObservedAt:    validTestTimestamp(),
	}

	inv := validResourceInventory()
	inv.Credentials = []protocol.CredentialInventoryEntry{{Ref: ref, Evidence: validEvidence}}
	if err := inv.Validate(); err != nil {
		t.Fatalf("expected matching ref/evidence pair to pass, got: %v", err)
	}

	mismatchedRefID := validEvidence
	mismatchedRefID.RefID = "cred-someone-else"
	inv.Credentials = []protocol.CredentialInventoryEntry{{Ref: ref, Evidence: mismatchedRefID}}
	if err := inv.Validate(); err == nil {
		t.Fatal("expected error for mismatched ref_id between ref and evidence")
	}

	mismatchedKind := validEvidence
	mismatchedKind.Kind = protocol.CredRefEnvVar
	mismatchedKind.ProbeKind = protocol.AuthProbeEnvPresence
	inv.Credentials = []protocol.CredentialInventoryEntry{{Ref: ref, Evidence: mismatchedKind}}
	if err := inv.Validate(); err == nil {
		t.Fatal("expected error for mismatched kind between ref and evidence")
	}
}

// TestResourceInventorySchemaParity is the Go/JSON-Schema parity check for
// ResourceInventory, the same discipline WP-M3B-4 established for
// CredentialRef/AuthEvidence.
func TestResourceInventorySchemaParity(t *testing.T) {
	schemas, err := schema.Default()
	if err != nil {
		t.Fatalf("failed to compile schemas: %v", err)
	}

	cases := []struct {
		name string
		inv  protocol.ResourceInventory
	}{
		{"minimal valid", validResourceInventory()},
		{"with endpoints/hosts/policy", func() protocol.ResourceInventory {
			i := validResourceInventory()
			i.Profile = &protocol.MachineProfileRef{
				ProfileID:          "mcp-001",
				MachineFingerprint: i.MachineFingerprint,
				ObservedAt:         validTestTimestamp(),
				ProbeDepth:         protocol.DepthHealth,
			}
			i.CognitionEndpoints = []protocol.CognitionEndpointSummary{
				{ID: "ollama:small", Kind: protocol.EndpointLocalRuntime, Locality: protocol.LocalityLocal,
					Health: protocol.EndpointHealthReady, Auth: protocol.AuthNotApplicable,
					CostClass: protocol.CostLocalCompute, RequiredSourceExposure: protocol.ExposureLocalOnly},
			}
			i.PrincipalHosts = []protocol.PrincipalHostSummary{{HostID: "vscode", Installed: true}}
			policy := protocol.PolicySummary{MaxSourceExposure: protocol.ExposureFocusedSnippets, MaxCostClass: protocol.CostRemoteEconomy}
			i.Policy = &policy
			return i
		}()},
		{"invalid hardware os_family", func() protocol.ResourceInventory {
			i := validResourceInventory()
			i.Hardware.OSFamily = "plan9"
			return i
		}()},
		{"negative memory", func() protocol.ResourceInventory {
			i := validResourceInventory()
			neg := int64(-1)
			i.Hardware.TotalMemoryBytes = &neg
			return i
		}()},
		{"secret-shaped credential ref_id", func() protocol.ResourceInventory {
			i := validResourceInventory()
			i.Credentials = []protocol.CredentialInventoryEntry{{
				Ref: protocol.CredentialRef{SchemaVersion: protocol.SchemaVersion1, RefID: "sk-ant-api03-abcdefghijklmnop", Kind: protocol.CredRefCLISession, Locator: "claude"},
				Evidence: protocol.AuthEvidence{SchemaVersion: protocol.SchemaVersion1, RefID: "sk-ant-api03-abcdefghijklmnop", Kind: protocol.CredRefCLISession,
					Status: protocol.AuthStatusIndeterminate, ProbeKind: protocol.AuthProbeCLIVersionOnly, ObservedAt: validTestTimestamp()},
			}}
			return i
		}()},
		// The following two cases are the independent-review follow-up on
		// WP-M3B-5, finding 4a's exact examples: before switching to a
		// cross-schema $ref onto the canonical credential-ref.schema.json/
		// auth-evidence.schema.json, resource-inventory.schema.json forked
		// weaker local copies that didn't enforce these two WP-M3B-4
		// structural rules, so Go rejected these while the (then-separate)
		// nested schema accepted them — a real parity gap this proves is
		// now closed.
		{"nested credential_ref: lowercase env_var locator (WP4 rule)", func() protocol.ResourceInventory {
			i := validResourceInventory()
			i.Credentials = []protocol.CredentialInventoryEntry{{
				Ref: protocol.CredentialRef{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-1", Kind: protocol.CredRefEnvVar, Locator: "lowercase_key"},
				Evidence: protocol.AuthEvidence{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-1", Kind: protocol.CredRefEnvVar,
					Status: protocol.AuthStatusIndeterminate, ProbeKind: protocol.AuthProbeEnvPresence, ObservedAt: validTestTimestamp()},
			}}
			return i
		}()},
		{"nested auth_evidence: kind/probe_kind mismatch (WP4 structural rule)", func() protocol.ResourceInventory {
			i := validResourceInventory()
			i.Credentials = []protocol.CredentialInventoryEntry{{
				Ref: protocol.CredentialRef{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-1", Kind: protocol.CredRefEnvVar, Locator: "SOME_KEY"},
				Evidence: protocol.AuthEvidence{SchemaVersion: protocol.SchemaVersion1, RefID: "cred-1", Kind: protocol.CredRefEnvVar,
					Status: protocol.AuthStatusIndeterminate, ProbeKind: protocol.AuthProbeCLIAuthCall, ObservedAt: validTestTimestamp()},
			}}
			return i
		}()},
		{"remote endpoint claiming verified acceleration", func() protocol.ResourceInventory {
			i := validResourceInventory()
			i.Profile = &protocol.MachineProfileRef{
				ProfileID:          "mcp-001",
				MachineFingerprint: i.MachineFingerprint,
				ObservedAt:         validTestTimestamp(),
				ProbeDepth:         protocol.DepthHealth,
			}
			backend := protocol.BackendCUDA
			i.CognitionEndpoints = []protocol.CognitionEndpointSummary{
				{
					ID:                     "remote-ep",
					Kind:                   protocol.EndpointRemoteAPI,
					Locality:               protocol.LocalityRemote,
					Health:                 protocol.EndpointHealthReady,
					Auth:                   protocol.AuthAuthenticated,
					CostClass:              protocol.CostRemoteEconomy,
					RequiredSourceExposure: protocol.ExposureLocalOnly,
					AccelerationVerified:   true,
					AccelerationBackend:    &backend,
				},
			}
			return i
		}()},
		{"local runtime with remote locality", func() protocol.ResourceInventory {
			i := validResourceInventory()
			i.Profile = &protocol.MachineProfileRef{
				ProfileID:          "mcp-001",
				MachineFingerprint: i.MachineFingerprint,
				ObservedAt:         validTestTimestamp(),
				ProbeDepth:         protocol.DepthHealth,
			}
			i.CognitionEndpoints = []protocol.CognitionEndpointSummary{
				{
					ID:                     "ollama-remote",
					Kind:                   protocol.EndpointLocalRuntime,
					Locality:               protocol.LocalityRemote,
					Health:                 protocol.EndpointHealthReady,
					Auth:                   protocol.AuthNotApplicable,
					CostClass:              protocol.CostLocalCompute,
					RequiredSourceExposure: protocol.ExposureLocalOnly,
				},
			}
			return i
		}()},
		{"duplicate scope readiness", func() protocol.ResourceInventory {
			i := validResourceInventory()
			i.Readiness = []protocol.ScopeReadiness{
				{Scope: protocol.ScopeCanRunLocalInference, Status: protocol.ScopeStatusReady, Reason: "ready"},
				{Scope: protocol.ScopeCanRunLocalInference, Status: protocol.ScopeStatusNotReady, Reason: "duplicate"},
			}
			return i
		}()},
		{"duplicate endpoint summaries", func() protocol.ResourceInventory {
			i := validResourceInventory()
			i.Profile = &protocol.MachineProfileRef{
				ProfileID:          "mcp-001",
				MachineFingerprint: i.MachineFingerprint,
				ObservedAt:         validTestTimestamp(),
				ProbeDepth:         protocol.DepthHealth,
			}
			ep := protocol.CognitionEndpointSummary{
				ID:                     "ollama:small",
				Kind:                   protocol.EndpointLocalRuntime,
				Locality:               protocol.LocalityLocal,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthNotApplicable,
				CostClass:              protocol.CostLocalCompute,
				RequiredSourceExposure: protocol.ExposureLocalOnly,
			}
			i.CognitionEndpoints = []protocol.CognitionEndpointSummary{ep, ep}
			return i
		}()},
		{"duplicate host summaries", func() protocol.ResourceInventory {
			i := validResourceInventory()
			h := protocol.PrincipalHostSummary{HostID: "vscode", Installed: true}
			i.PrincipalHosts = []protocol.PrincipalHostSummary{h, h}
			return i
		}()},
		{"missing profile when cognition endpoints present", func() protocol.ResourceInventory {
			i := validResourceInventory()
			i.Profile = nil
			i.CognitionEndpoints = []protocol.CognitionEndpointSummary{
				{
					ID:                     "ollama:small",
					Kind:                   protocol.EndpointLocalRuntime,
					Locality:               protocol.LocalityLocal,
					Health:                 protocol.EndpointHealthReady,
					Auth:                   protocol.AuthNotApplicable,
					CostClass:              protocol.CostLocalCompute,
					RequiredSourceExposure: protocol.ExposureLocalOnly,
				},
			}
			return i
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			goErr := tc.inv.Validate()
			data, err := json.Marshal(tc.inv)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			schemaErr := schemas.ValidateBytes(schema.NameResourceInventory, data)
			if (goErr == nil) != (schemaErr == nil) {
				t.Fatalf("parity mismatch: go accepted=%v (err=%v), schema accepted=%v (err=%v)",
					goErr == nil, goErr, schemaErr == nil, schemaErr)
			}
		})
	}
}

// TestEvaluateScopeReadinessRespectsAuthentication is the required
// regression set from the independent-review follow-up on WP-M3B-5,
// finding 5: has_any_viable_cognition_path and
// can_use_existing_authenticated_cli must respect the WP-M3B-4 invariant
// "healthy != authenticated != usable", not just endpoint health.
func TestEvaluateScopeReadinessRespectsAuthentication(t *testing.T) {
	localReady := protocol.CognitionEndpointSummary{
		ID: "local", Kind: protocol.EndpointLocalRuntime, Locality: protocol.LocalityLocal,
		Health: protocol.EndpointHealthReady, Auth: protocol.AuthNotApplicable,
	}
	cliExpired := protocol.CognitionEndpointSummary{
		ID: "cli-expired", Kind: protocol.EndpointAuthenticatedCLI, Locality: protocol.LocalityRemote,
		Health: protocol.EndpointHealthReady, Auth: protocol.AuthExpired,
	}
	cliUnknown := protocol.CognitionEndpointSummary{
		ID: "cli-unknown", Kind: protocol.EndpointAuthenticatedCLI, Locality: protocol.LocalityRemote,
		Health: protocol.EndpointHealthReady, Auth: protocol.AuthUnknown,
	}
	cliAuthenticated := protocol.CognitionEndpointSummary{
		ID: "cli-authenticated", Kind: protocol.EndpointAuthenticatedCLI, Locality: protocol.LocalityRemote,
		Health: protocol.EndpointHealthReady, Auth: protocol.AuthAuthenticated,
	}

	t.Run("ready CLI with expired auth has no viable cognition path", func(t *testing.T) {
		got := protocol.EvaluateScopeReadiness(nil, []protocol.CognitionEndpointSummary{cliExpired}, nil)
		if s := scopeStatus(got, protocol.ScopeHasAnyViableCognitionPath); s != protocol.ScopeStatusNotReady {
			t.Errorf("has_any_viable_cognition_path = %q, want not_ready (ready health must not imply viable when auth is expired)", s)
		}
		if s := scopeStatus(got, protocol.ScopeCanUseAuthenticatedCLI); s != protocol.ScopeStatusNotReady {
			t.Errorf("can_use_existing_authenticated_cli = %q, want not_ready", s)
		}
	})

	t.Run("ready CLI with unknown auth is unknown, not silently not_ready or ready", func(t *testing.T) {
		got := protocol.EvaluateScopeReadiness(nil, []protocol.CognitionEndpointSummary{cliUnknown}, nil)
		if s := scopeStatus(got, protocol.ScopeHasAnyViableCognitionPath); s != protocol.ScopeStatusUnknown {
			t.Errorf("has_any_viable_cognition_path = %q, want unknown", s)
		}
		if s := scopeStatus(got, protocol.ScopeCanUseAuthenticatedCLI); s != protocol.ScopeStatusUnknown {
			t.Errorf("can_use_existing_authenticated_cli = %q, want unknown", s)
		}
	})

	t.Run("ready local runtime is viable without any remote auth", func(t *testing.T) {
		got := protocol.EvaluateScopeReadiness(nil, []protocol.CognitionEndpointSummary{localReady}, nil)
		if s := scopeStatus(got, protocol.ScopeHasAnyViableCognitionPath); s != protocol.ScopeStatusReady {
			t.Errorf("has_any_viable_cognition_path = %q, want ready (local runtime needs no auth)", s)
		}
	})

	t.Run("one unusable remote endpoint plus one ready local endpoint is still viable", func(t *testing.T) {
		got := protocol.EvaluateScopeReadiness(nil, []protocol.CognitionEndpointSummary{cliExpired, localReady}, nil)
		if s := scopeStatus(got, protocol.ScopeHasAnyViableCognitionPath); s != protocol.ScopeStatusReady {
			t.Errorf("has_any_viable_cognition_path = %q, want ready (the local endpoint alone makes a path viable)", s)
		}
	})

	t.Run("a genuinely authenticated CLI is ready", func(t *testing.T) {
		got := protocol.EvaluateScopeReadiness(nil, []protocol.CognitionEndpointSummary{cliAuthenticated}, nil)
		if s := scopeStatus(got, protocol.ScopeHasAnyViableCognitionPath); s != protocol.ScopeStatusReady {
			t.Errorf("has_any_viable_cognition_path = %q, want ready", s)
		}
		if s := scopeStatus(got, protocol.ScopeCanUseAuthenticatedCLI); s != protocol.ScopeStatusReady {
			t.Errorf("can_use_existing_authenticated_cli = %q, want ready", s)
		}
	})
}

func scopeStatus(readiness []protocol.ScopeReadiness, scope protocol.ScopeKind) protocol.ScopeReadinessStatus {
	for _, r := range readiness {
		if r.Scope == scope {
			return r.Status
		}
	}
	return ""
}

func TestResourceInventoryValidation_DuplicateIDs(t *testing.T) {
	t.Run("duplicate endpoint ID with different fields", func(t *testing.T) {
		inv := validResourceInventory()
		inv.Profile = &protocol.MachineProfileRef{
			ProfileID:          "mcp-001",
			MachineFingerprint: inv.MachineFingerprint,
			ObservedAt:         validTestTimestamp(),
			ProbeDepth:         protocol.DepthHealth,
		}
		inv.CognitionEndpoints = []protocol.CognitionEndpointSummary{
			{
				ID:                     "ep-1",
				Kind:                   protocol.EndpointLocalRuntime,
				Locality:               protocol.LocalityLocal,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthNotApplicable,
				CostClass:              protocol.CostLocalCompute,
				RequiredSourceExposure: protocol.ExposureLocalOnly,
			},
			{
				ID:                     "ep-1",
				Kind:                   protocol.EndpointLocalRuntime,
				Locality:               protocol.LocalityLocal,
				Health:                 protocol.EndpointHealthUnhealthy,
				Auth:                   protocol.AuthNotApplicable,
				CostClass:              protocol.CostLocalCompute,
				RequiredSourceExposure: protocol.ExposureLocalOnly,
			},
		}
		if err := inv.Validate(); err == nil {
			t.Fatal("expected error for duplicate endpoint ID in CognitionEndpoints")
		}
	})

	t.Run("duplicate principal host ID with different fields", func(t *testing.T) {
		inv := validResourceInventory()
		inv.PrincipalHosts = []protocol.PrincipalHostSummary{
			{HostID: "vscode", Installed: true, Path: "/usr/bin/code"},
			{HostID: "vscode", Installed: false, Path: "/usr/local/bin/code"},
		}
		if err := inv.Validate(); err == nil {
			t.Fatal("expected error for duplicate host ID in PrincipalHosts")
		}
	})

	t.Run("duplicate credential ref_id with different evidence", func(t *testing.T) {
		inv := validResourceInventory()
		ref1 := protocol.CredentialRef{
			SchemaVersion: protocol.SchemaVersion1,
			RefID:         "cred-claude",
			Kind:          protocol.CredRefCLISession,
			Locator:       "claude",
		}
		ev1 := protocol.AuthEvidence{
			SchemaVersion: protocol.SchemaVersion1,
			RefID:         "cred-claude",
			Kind:          protocol.CredRefCLISession,
			Status:        protocol.AuthStatusAuthenticated,
			ProbeKind:     protocol.AuthProbeCLIAuthCall,
			ObservedAt:    validTestTimestamp(),
			ProbeTarget:   "claude",
		}
		ev2 := protocol.AuthEvidence{
			SchemaVersion: protocol.SchemaVersion1,
			RefID:         "cred-claude",
			Kind:          protocol.CredRefCLISession,
			Status:        protocol.AuthStatusUnauthenticated,
			ProbeKind:     protocol.AuthProbeCLIAuthCall,
			ObservedAt:    validTestTimestamp(),
			ProbeTarget:   "claude",
		}
		inv.Credentials = []protocol.CredentialInventoryEntry{
			{Ref: ref1, Evidence: ev1},
			{Ref: ref1, Evidence: ev2},
		}
		if err := inv.Validate(); err == nil {
			t.Fatal("expected error for duplicate credential ref_id in Credentials")
		}
	})
}

func TestDoctorReportValidation_DuplicatesAndInventoryParity(t *testing.T) {
	validReport := func() protocol.DoctorReport {
		inv := validResourceInventory()
		inv.Profile = &protocol.MachineProfileRef{
			ProfileID:          "mcp-001",
			MachineFingerprint: inv.MachineFingerprint,
			ObservedAt:         validTestTimestamp(),
			ProbeDepth:         protocol.DepthHealth,
		}
		ep := protocol.CognitionEndpointSummary{
			ID:                     "ollama:small",
			Kind:                   protocol.EndpointLocalRuntime,
			Locality:               protocol.LocalityLocal,
			Health:                 protocol.EndpointHealthReady,
			Auth:                   protocol.AuthNotApplicable,
			CostClass:              protocol.CostLocalCompute,
			RequiredSourceExposure: protocol.ExposureLocalOnly,
		}
		host := protocol.PrincipalHostSummary{HostID: "vscode", Installed: true}
		inv.CognitionEndpoints = []protocol.CognitionEndpointSummary{ep}
		inv.PrincipalHosts = []protocol.PrincipalHostSummary{host}
		inv.Readiness = []protocol.ScopeReadiness{
			{Scope: protocol.ScopeCanRunLocalInference, Status: protocol.ScopeStatusReady, Reason: "ok"},
		}

		targetProfile := protocol.ProfileCustom
		return protocol.DoctorReport{
			SchemaVersion:       protocol.SchemaVersion1,
			ReportID:            "doc-001",
			ObservedAt:          validTestTimestamp(),
			MachineFingerprint:  inv.MachineFingerprint,
			EvaluationScope:     protocol.ReadinessEvaluationScope{TargetProfile: &targetProfile, EvidenceStatus: "live"},
			Readiness:           protocol.ReadinessReady,
			DiscoveredEndpoints: []protocol.CognitionEndpointSummary{ep},
			PrincipalHosts:      []protocol.PrincipalHostSummary{host},
			ScopeReadiness: []protocol.ScopeReadiness{
				{Scope: protocol.ScopeCanRunLocalInference, Status: protocol.ScopeStatusReady, Reason: "ok"},
			},
			ResourceInventory: &inv,
		}
	}

	t.Run("valid report passes", func(t *testing.T) {
		r := validReport()
		if err := r.Validate(); err != nil {
			t.Fatalf("expected valid report to pass: %v", err)
		}
	})

	t.Run("duplicate endpoint in DiscoveredEndpoints rejected", func(t *testing.T) {
		r := validReport()
		ep2 := r.DiscoveredEndpoints[0]
		ep2.Health = protocol.EndpointHealthUnhealthy
		r.DiscoveredEndpoints = append(r.DiscoveredEndpoints, ep2)
		if err := r.Validate(); err == nil {
			t.Fatal("expected error for duplicate endpoint in DiscoveredEndpoints")
		}
	})

	t.Run("duplicate host in PrincipalHosts rejected", func(t *testing.T) {
		r := validReport()
		h2 := r.PrincipalHosts[0]
		h2.Installed = false
		r.PrincipalHosts = append(r.PrincipalHosts, h2)
		if err := r.Validate(); err == nil {
			t.Fatal("expected error for duplicate host in PrincipalHosts")
		}
	})

	t.Run("contradictory endpoints between report and inventory rejected", func(t *testing.T) {
		r := validReport()
		ep2 := r.DiscoveredEndpoints[0]
		ep2.Health = protocol.EndpointHealthUnhealthy
		r.ResourceInventory.CognitionEndpoints = []protocol.CognitionEndpointSummary{ep2}
		if err := r.Validate(); err == nil {
			t.Fatal("expected error when DiscoveredEndpoints contradicts ResourceInventory.CognitionEndpoints")
		}
	})

	t.Run("contradictory hosts between report and inventory rejected", func(t *testing.T) {
		r := validReport()
		h2 := r.PrincipalHosts[0]
		h2.Installed = false
		r.ResourceInventory.PrincipalHosts = []protocol.PrincipalHostSummary{h2}
		if err := r.Validate(); err == nil {
			t.Fatal("expected error when PrincipalHosts contradicts ResourceInventory.PrincipalHosts")
		}
	})
}

func TestDoctorReportSchemaParity(t *testing.T) {
	schemas, err := schema.Default()
	if err != nil {
		t.Fatalf("failed to compile schemas: %v", err)
	}

	validReport := func() protocol.DoctorReport {
		inv := validResourceInventory()
		inv.Profile = &protocol.MachineProfileRef{
			ProfileID:          "mcp-001",
			MachineFingerprint: inv.MachineFingerprint,
			ObservedAt:         validTestTimestamp(),
			ProbeDepth:         protocol.DepthHealth,
		}
		ep := protocol.CognitionEndpointSummary{
			ID:                     "ollama:small",
			Kind:                   protocol.EndpointLocalRuntime,
			Locality:               protocol.LocalityLocal,
			Health:                 protocol.EndpointHealthReady,
			Auth:                   protocol.AuthNotApplicable,
			CostClass:              protocol.CostLocalCompute,
			RequiredSourceExposure: protocol.ExposureLocalOnly,
		}
		host := protocol.PrincipalHostSummary{HostID: "vscode", Installed: true}
		inv.CognitionEndpoints = []protocol.CognitionEndpointSummary{ep}
		inv.PrincipalHosts = []protocol.PrincipalHostSummary{host}
		inv.Readiness = []protocol.ScopeReadiness{
			{Scope: protocol.ScopeCanRunLocalInference, Status: protocol.ScopeStatusReady, Reason: "ok"},
		}

		targetProfile := protocol.ProfileCustom
		return protocol.DoctorReport{
			SchemaVersion:       protocol.SchemaVersion1,
			ReportID:            "doc-001",
			ObservedAt:          validTestTimestamp(),
			MachineFingerprint:  inv.MachineFingerprint,
			EvaluationScope:     protocol.ReadinessEvaluationScope{TargetProfile: &targetProfile, RequiredRoles: []string{}, EvidenceStatus: "live"},
			Readiness:           protocol.ReadinessReady,
			Findings:            []protocol.DiagnosticFinding{},
			DiscoveredEndpoints: []protocol.CognitionEndpointSummary{ep},
			PrincipalHosts:      []protocol.PrincipalHostSummary{host},
			ScopeReadiness: []protocol.ScopeReadiness{
				{Scope: protocol.ScopeCanRunLocalInference, Status: protocol.ScopeStatusReady, Reason: "ok"},
			},
			ResourceInventory: &inv,
		}
	}

	cases := []struct {
		name   string
		report func() protocol.DoctorReport
	}{
		{"valid report", validReport},
		{"duplicate scope readiness", func() protocol.DoctorReport {
			r := validReport()
			r.ScopeReadiness = append(r.ScopeReadiness, protocol.ScopeReadiness{
				Scope: protocol.ScopeCanRunLocalInference, Status: protocol.ScopeStatusNotReady, Reason: "dup",
			})
			return r
		}},
		{"remote endpoint claiming verified acceleration", func() protocol.DoctorReport {
			r := validReport()
			backend := protocol.BackendCUDA
			r.DiscoveredEndpoints = []protocol.CognitionEndpointSummary{
				{
					ID:                     "remote-ep",
					Kind:                   protocol.EndpointRemoteAPI,
					Locality:               protocol.LocalityRemote,
					Health:                 protocol.EndpointHealthReady,
					Auth:                   protocol.AuthAuthenticated,
					CostClass:              protocol.CostRemoteEconomy,
					RequiredSourceExposure: protocol.ExposureLocalOnly,
					AccelerationVerified:   true,
					AccelerationBackend:    &backend,
				},
			}
			return r
		}},
		{"local runtime with remote locality", func() protocol.DoctorReport {
			r := validReport()
			r.DiscoveredEndpoints = []protocol.CognitionEndpointSummary{
				{
					ID:                     "ollama-remote",
					Kind:                   protocol.EndpointLocalRuntime,
					Locality:               protocol.LocalityRemote,
					Health:                 protocol.EndpointHealthReady,
					Auth:                   protocol.AuthNotApplicable,
					CostClass:              protocol.CostLocalCompute,
					RequiredSourceExposure: protocol.ExposureLocalOnly,
				},
			}
			return r
		}},
		{"duplicate endpoint items", func() protocol.DoctorReport {
			r := validReport()
			r.DiscoveredEndpoints = append(r.DiscoveredEndpoints, r.DiscoveredEndpoints[0])
			return r
		}},
		{"duplicate host items", func() protocol.DoctorReport {
			r := validReport()
			r.PrincipalHosts = append(r.PrincipalHosts, r.PrincipalHosts[0])
			return r
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep := tc.report()
			goErr := rep.Validate()
			data, err := json.Marshal(rep)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			schemaErr := schemas.ValidateBytes(schema.NameDoctorReport, data)
			if (goErr == nil) != (schemaErr == nil) {
				t.Fatalf("parity mismatch: go accepted=%v (err=%v), schema accepted=%v (err=%v)",
					goErr == nil, goErr, schemaErr == nil, schemaErr)
			}
		})
	}
}
