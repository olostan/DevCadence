package cognition_test

import (
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/protocol"
)

func ptr[T any](v T) *T { return &v }

func makeTestPortfolio() *protocol.CognitionPortfolio {
	return &protocol.CognitionPortfolio{
		SchemaVersion:     protocol.SchemaVersion1,
		PortfolioID:       "port-test-01",
		Revision:          1,
		CreatedAt:         "2026-10-02T20:00:00Z",
		MaxSourceExposure: protocol.ExposureFocusedSnippets,
		Channels: []protocol.AccessChannel{
			{
				SchemaVersion:         protocol.SchemaVersion1,
				ChannelID:             "chan-local-01",
				EndpointID:            "ep-local-01",
				Kind:                  protocol.ChannelLocalDaemonSocket,
				SessionMode:           protocol.SessionStatelessPerCall,
				ContextControl:        protocol.ContextControlExactStateless,
				PrefixCache:           protocol.PrefixCacheSessionKV,
				SupportsStreaming:     true,
				SupportsTools:         true,
				NativeWorktreeAccess:  false,
				MaxConcurrentRequests: 2,
			},
			{
				SchemaVersion:         protocol.SchemaVersion1,
				ChannelID:             "chan-cli-01",
				EndpointID:            "ep-cli-01",
				Kind:                  protocol.ChannelCLISubprocess,
				SessionMode:           protocol.SessionResumableHandle,
				ContextControl:        protocol.ContextControlAppendOnly,
				PrefixCache:           protocol.PrefixCacheImplicit,
				SupportsStreaming:     true,
				SupportsTools:         true,
				NativeWorktreeAccess:  true,
				CredentialRefID:       ptr("cred-cli-01"),
				MaxConcurrentRequests: 1,
			},
		},
		RoleBindings: []protocol.RoleBinding{
			{
				Role:             "implementer",
				EndpointID:       "ep-local-01",
				ChannelID:        "chan-local-01",
				BudgetPoolID:     "pool-local",
				ContextProfileID: "prof-local-01",
				Priority:         1,
				Fallbacks: []protocol.FallbackBinding{
					{
						EndpointID:       "ep-cli-01",
						ChannelID:        "chan-cli-01",
						BudgetPoolID:     "pool-sub",
						ContextProfileID: "prof-cli-01",
					},
				},
			},
			{
				Role:             "scout",
				EndpointID:       "ep-local-01",
				ChannelID:        "chan-local-01",
				BudgetPoolID:     "pool-local",
				ContextProfileID: "prof-local-01",
				Priority:         1,
			},
		},
		BudgetPools: []protocol.BudgetPool{
			{
				SchemaVersion:            protocol.SchemaVersion1,
				PoolID:                   "pool-local",
				Name:                     "Local Workstation Compute",
				Regime:                   protocol.RegimeLocalCompute,
				Unit:                     protocol.UnitSeconds,
				HardLimit:                36000,
				SoftAlertLimit:           28800,
				Period:                   protocol.PeriodRollingDay,
				AllowOverage:             false,
				FallbackAllowedToMetered: false,
			},
			{
				SchemaVersion:            protocol.SchemaVersion1,
				PoolID:                   "pool-sub",
				Name:                     "Team Subscription Quota",
				Regime:                   protocol.RegimeSubscriptionQuota,
				Unit:                     protocol.UnitRequests,
				HardLimit:                500,
				SoftAlertLimit:           400,
				Period:                   protocol.PeriodBillingCycle,
				AllowOverage:             false,
				FallbackAllowedToMetered: false,
			},
		},
	}
}

func makeTestMachineProfile() *protocol.MachineCapabilityProfile {
	return &protocol.MachineCapabilityProfile{
		SchemaVersion:      protocol.SchemaVersion1,
		ProfileID:          "prof-mcp-01",
		MachineFingerprint: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ObservedAt:         protocol.Timestamp(time.Date(2026, 10, 2, 20, 0, 0, 0, time.UTC)),
		KnowledgeRevision:  "rev-1",
		ProbeDepth:         protocol.DepthInference,
		Endpoints: []protocol.CognitionEndpoint{
			{
				ID:                     "ep-local-01",
				Kind:                   protocol.EndpointLocalRuntime,
				Locality:               protocol.LocalityLocal,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthNotApplicable,
				CostClass:              protocol.CostLocalCompute,
				RequiredSourceExposure: protocol.ExposureLocalOnly,
				StructuredOutput:       protocol.FeatureProbePassed,
				ToolUse:                protocol.FeatureProbePassed,
				Capabilities: []protocol.GradedCapability{
					{
						Dimension:  protocol.CapabilityImplementation,
						Grade:      protocol.GradeStrong,
						Provenance: protocol.ProvenanceMeasured,
					},
					{
						Dimension:  protocol.CapabilityRepositoryReasoning,
						Grade:      protocol.GradeMedium,
						Provenance: protocol.ProvenanceMeasured,
					},
					{
						Dimension:  protocol.CapabilityReview,
						Grade:      protocol.GradeStrong,
						Provenance: protocol.ProvenanceMeasured,
					},
				},
				Acceleration: &protocol.AccelerationEvidence{
					Backend: protocol.BackendMetal,
					State:   protocol.StateVerified,
				},
			},
			{
				ID:                     "ep-cli-01",
				Kind:                   protocol.EndpointAuthenticatedCLI,
				Provider:               "anthropic",
				ModelID:                "claude-3-7-sonnet",
				Locality:               protocol.LocalityRemoteInferenceLocalTools,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthAuthenticated,
				CostClass:              protocol.CostSubscriptionIncluded,
				RequiredSourceExposure: protocol.ExposureFocusedSnippets,
				StructuredOutput:       protocol.FeatureProbePassed,
				ToolUse:                protocol.FeatureProbePassed,
				Capabilities: []protocol.GradedCapability{
					{
						Dimension:  protocol.CapabilityImplementation,
						Grade:      protocol.GradeStrong,
						Provenance: protocol.ProvenanceMeasured,
					},
					{
						Dimension:  protocol.CapabilityReview,
						Grade:      protocol.GradeStrong,
						Provenance: protocol.ProvenanceMeasured,
					},
				},
			},
		},
	}
}

func makeTestInventory() *protocol.ResourceInventory {
	return &protocol.ResourceInventory{
		SchemaVersion:      protocol.SchemaVersion1,
		InventoryID:        "inv-01",
		MachineFingerprint: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ObservedAt:         protocol.Timestamp(time.Date(2026, 10, 2, 20, 0, 0, 0, time.UTC)),
		Hardware: protocol.HardwareSummary{
			OSFamily:            protocol.OSDarwin,
			Arch:                "arm64",
			LogicalCores:        12,
			TotalMemoryBytes:    ptr(int64(36 * 1024 * 1024 * 1024)),
			AcceleratorBackends: []protocol.BackendKind{protocol.BackendMetal},
		},
		CognitionEndpoints: []protocol.CognitionEndpointSummary{
			{
				ID:                     "ep-local-01",
				Kind:                   protocol.EndpointLocalRuntime,
				Locality:               protocol.LocalityLocal,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthNotApplicable,
				CostClass:              protocol.CostLocalCompute,
				RequiredSourceExposure: protocol.ExposureLocalOnly,
				AccelerationVerified:   true,
				AccelerationBackend:    ptr(protocol.BackendMetal),
			},
			{
				ID:                     "ep-cli-01",
				Kind:                   protocol.EndpointAuthenticatedCLI,
				Locality:               protocol.LocalityRemoteInferenceLocalTools,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthAuthenticated,
				CostClass:              protocol.CostSubscriptionIncluded,
				RequiredSourceExposure: protocol.ExposureFocusedSnippets,
			},
		},
	}
}

func makeTestContextProfiles() map[string]*protocol.ContextProfile {
	return map[string]*protocol.ContextProfile{
		"prof-local-01": {
			SchemaVersion:             protocol.SchemaVersion1,
			ProfileID:                 "prof-local-01",
			EndpointID:                "ep-local-01",
			ChannelID:                 "chan-local-01",
			Runtime:                   "ollama",
			ModelRef:                  "qwen-2.5-coder-32b",
			Revision:                  1,
			DeclaredWindowTokens:      32768,
			RuntimeWindowTokens:       32768,
			TargetResidentTokens:      24000,
			HardResidentCeilingTokens: 30000,
			ProtectedCoreLimitTokens:  4000,
			ContractLimitTokens:       10000,
			MaxSingleLeaseTokens:      4000,
			OutputReserveTokens:       2000,
			ToolTailReserveTokens:     1000,
			AccountingMethod:          protocol.AccountingExactBPE,
			ObservedContextControl:    protocol.ContextControlExactStateless,
			ObservedPrefixCache:       protocol.PrefixCacheSessionKV,
			WorkloadEnvelopes: []protocol.WorkloadEnvelope{
				{
					Workload:        protocol.WorkloadImplementation,
					EffectiveTokens: 20000,
					CalibrationTask: "bench-impl-01",
					CalibrationDate: "2026-10-01",
					ConfidenceLevel: "provisional",
				},
			},
		},
		"prof-cli-01": {
			SchemaVersion:             protocol.SchemaVersion1,
			ProfileID:                 "prof-cli-01",
			EndpointID:                "ep-cli-01",
			ChannelID:                 "chan-cli-01",
			Runtime:                   "claude-cli",
			ModelRef:                  "claude-3-7-sonnet",
			Revision:                  1,
			DeclaredWindowTokens:      200000,
			RuntimeWindowTokens:       200000,
			TargetResidentTokens:      64000,
			HardResidentCeilingTokens: 128000,
			ProtectedCoreLimitTokens:  8000,
			ContractLimitTokens:       20000,
			MaxSingleLeaseTokens:      8000,
			OutputReserveTokens:       4000,
			ToolTailReserveTokens:     2000,
			AccountingMethod:          protocol.AccountingProviderAPI,
			ObservedContextControl:    protocol.ContextControlAppendOnly,
			ObservedPrefixCache:       protocol.PrefixCacheImplicit,
			WorkloadEnvelopes: []protocol.WorkloadEnvelope{
				{
					Workload:        protocol.WorkloadImplementation,
					EffectiveTokens: 50000,
					CalibrationTask: "bench-impl-02",
					CalibrationDate: "2026-10-01",
					ConfidenceLevel: "provisional",
				},
			},
		},
	}
}

func TestPortfolioValidator_ValidPortfolios(t *testing.T) {
	validator := cognition.NewPortfolioValidator()
	clk := clock.NewFake(time.Date(2026, 10, 2, 20, 0, 0, 0, time.UTC), 0)

	t.Run("valid local and subscription mixed portfolio", func(t *testing.T) {
		p := makeTestPortfolio()
		mp := makeTestMachineProfile()
		inv := makeTestInventory()
		cp := makeTestContextProfiles()
		policy := cognition.DefaultValidationPolicy()

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Policy:          &policy,
			Clock:           clk,
		})

		if !res.Valid {
			t.Fatalf("expected valid portfolio, got errors: %v", res.Summary())
		}
		if res.Err() != nil {
			t.Fatalf("expected nil error, got: %v", res.Err())
		}
		if res.CandidateDigest == "" {
			t.Errorf("expected non-empty candidate digest")
		}
		if res.InventoryDigest == "" {
			t.Errorf("expected non-empty inventory digest")
		}
	})

	t.Run("valid metered portfolio within authorized budget and policy", func(t *testing.T) {
		p := makeTestPortfolio()
		mp := makeTestMachineProfile()
		inv := makeTestInventory()
		cp := makeTestContextProfiles()

		// Add metered pool
		p.BudgetPools = append(p.BudgetPools, protocol.BudgetPool{
			SchemaVersion:            protocol.SchemaVersion1,
			PoolID:                   "pool-metered-api",
			Name:                     "API Metered Pool",
			Regime:                   protocol.RegimeMeteredAPI,
			Unit:                     protocol.UnitUSDCents,
			HardLimit:                5000, // $50
			SoftAlertLimit:           4000,
			Period:                   protocol.PeriodRollingDay,
			AllowOverage:             false,
			FallbackAllowedToMetered: true,
		})

		policy := cognition.ValidationPolicy{
			MaxSourceExposure: protocol.ExposureFocusedSnippets,
			MaxCostClass:      protocol.CostFrontierExpensive,
			ForbidMeteredAPI:  false,
		}

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Policy:          &policy,
			Clock:           clk,
		})

		if !res.Valid {
			t.Fatalf("expected valid metered portfolio, got: %v", res.Summary())
		}
	})
}

func TestPortfolioValidator_Rejections(t *testing.T) {
	validator := cognition.NewPortfolioValidator()
	clk := clock.NewFake(time.Date(2026, 10, 2, 20, 0, 0, 0, time.UTC), 0)

	t.Run("endpoint not found", func(t *testing.T) {
		p := makeTestPortfolio()
		p.RoleBindings[0].EndpointID = "ep-nonexistent"
		p.RoleBindings[0].ChannelID = "chan-local-01"

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  makeTestMachineProfile(),
			Inventory:       makeTestInventory(),
			ContextProfiles: makeTestContextProfiles(),
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected validation failure for nonexistent endpoint")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeEndpointNotFound {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeEndpointNotFound in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("unauthorized source exposure exceeds policy limit", func(t *testing.T) {
		p := makeTestPortfolio()
		// Portfolio allows focused snippets, but policy restricts to local only
		policy := cognition.ValidationPolicy{
			MaxSourceExposure: protocol.ExposureLocalOnly,
			MaxCostClass:      protocol.CostFrontierExpensive,
		}

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  makeTestMachineProfile(),
			Inventory:       makeTestInventory(),
			ContextProfiles: makeTestContextProfiles(),
			Policy:          &policy,
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected rejection for source exposure exceeding policy")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeUnauthorizedSourceExposure {
				found = true
				if d.Condition != cognition.ConditionUnauthorized {
					t.Errorf("expected ConditionUnauthorized, got %s", d.Condition)
				}
				break
			}
		}
		if !found {
			t.Errorf("expected CodeUnauthorizedSourceExposure in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("attempted silent metered fallback without authorization", func(t *testing.T) {
		p := makeTestPortfolio()
		// Add metered pool
		p.BudgetPools = append(p.BudgetPools, protocol.BudgetPool{
			SchemaVersion:            protocol.SchemaVersion1,
			PoolID:                   "pool-metered",
			Name:                     "Metered Pool",
			Regime:                   protocol.RegimeMeteredAPI,
			Unit:                     protocol.UnitUSDCents,
			HardLimit:                1000,
			SoftAlertLimit:           800,
			Period:                   protocol.PeriodRollingDay,
			FallbackAllowedToMetered: true,
		})
		// Primary pool is local compute without FallbackAllowedToMetered
		p.RoleBindings[0].BudgetPoolID = "pool-local"
		p.RoleBindings[0].Fallbacks[0].BudgetPoolID = "pool-metered"

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  makeTestMachineProfile(),
			Inventory:       makeTestInventory(),
			ContextProfiles: makeTestContextProfiles(),
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected rejection for unauthorized metered fallback")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeUnauthorizedMeteredFallback {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeUnauthorizedMeteredFallback in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("budget pool exhausted in BudgetState", func(t *testing.T) {
		p := makeTestPortfolio()
		budgetStates := map[string]*protocol.BudgetState{
			"pool-local": {
				SchemaVersion:    protocol.SchemaVersion1,
				PoolID:           "pool-local",
				Status:           protocol.BudgetStatusExhausted,
				ObservedAt:       "2026-10-02T20:00:00Z",
				RemainingBalance: ptr(int64(0)),
			},
		}

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  makeTestMachineProfile(),
			Inventory:       makeTestInventory(),
			ContextProfiles: makeTestContextProfiles(),
			BudgetStates:    budgetStates,
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected rejection for exhausted budget pool")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeBudgetPoolExhausted {
				found = true
				if d.Condition != cognition.ConditionOverBudget {
					t.Errorf("expected ConditionOverBudget, got %s", d.Condition)
				}
				break
			}
		}
		if !found {
			t.Errorf("expected CodeBudgetPoolExhausted in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("context control mismatch between channel and context profile", func(t *testing.T) {
		p := makeTestPortfolio()
		cp := makeTestContextProfiles()
		// Force channel context_control to opaque_session while profile observed exact_stateless
		p.Channels[0].ContextControl = protocol.ContextControlOpaqueSession

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  makeTestMachineProfile(),
			Inventory:       makeTestInventory(),
			ContextProfiles: cp,
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected rejection for context control mismatch")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeContextControlMismatch {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeContextControlMismatch in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("hardware accelerator unavailable on machine", func(t *testing.T) {
		p := makeTestPortfolio()
		mp := makeTestMachineProfile()
		inv := makeTestInventory()
		cp := makeTestContextProfiles()

		// Endpoint claims CUDA backend, but machine inventory only has Metal
		mp.Endpoints[0].Acceleration = &protocol.AccelerationEvidence{
			Backend: protocol.BackendCUDA,
			State:   protocol.StateVerified,
		}

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected rejection for missing CUDA accelerator")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeHardwareBackendUnavailable {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeHardwareBackendUnavailable in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("resource capacity exceeded active slots", func(t *testing.T) {
		p := makeTestPortfolio()
		resStates := map[string]*protocol.ResourceState{
			"host-01": {
				SchemaVersion:      protocol.SchemaVersion1,
				HostID:             "host-01",
				Timestamp:          "2026-10-02T20:00:00Z",
				MaxConcurrentSlots: ptr(4),
				ActiveSlots:        ptr(4), // saturated
			},
		}

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  makeTestMachineProfile(),
			Inventory:       makeTestInventory(),
			ContextProfiles: makeTestContextProfiles(),
			ResourceStates:  resStates,
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected rejection for saturated slots")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeResourceCapacityExceeded {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeResourceCapacityExceeded in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("diversity requirements violated when review roles share endpoint", func(t *testing.T) {
		p := makeTestPortfolio()
		// Add two review roles bound to the same endpoint
		p.RoleBindings = append(p.RoleBindings,
			protocol.RoleBinding{
				Role:             "correctness_reviewer",
				EndpointID:       "ep-local-01",
				ChannelID:        "chan-local-01",
				BudgetPoolID:     "pool-local",
				ContextProfileID: "prof-local-01",
				Priority:         1,
			},
			protocol.RoleBinding{
				Role:             "architecture_reviewer",
				EndpointID:       "ep-local-01", // duplicate endpoint!
				ChannelID:        "chan-local-01",
				BudgetPoolID:     "pool-local",
				ContextProfileID: "prof-local-01",
				Priority:         1,
			},
		)
		p.DiversityRequirements = &protocol.DiversityPolicy{
			RequireDistinctEndpointsForReview: true,
		}

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  makeTestMachineProfile(),
			Inventory:       makeTestInventory(),
			ContextProfiles: makeTestContextProfiles(),
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected rejection for review diversity violation")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeDiversityViolation {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeDiversityViolation in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("capability grade insufficient", func(t *testing.T) {
		p := makeTestPortfolio()
		mp := makeTestMachineProfile()
		// Set implementer capability to low (requires strong)
		mp.Endpoints[0].Capabilities[0].Grade = protocol.GradeLow

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       makeTestInventory(),
			ContextProfiles: makeTestContextProfiles(),
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected rejection for insufficient capability grade")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeCapabilityGradeInsufficient {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeCapabilityGradeInsufficient in diagnostics: %v", res.Diagnostics)
		}
	})

	t.Run("capability provenance invalid when measured required", func(t *testing.T) {
		p := makeTestPortfolio()
		mp := makeTestMachineProfile()
		// Set provenance to configured
		mp.Endpoints[0].Capabilities[0].Provenance = protocol.ProvenanceConfigured

		policy := cognition.ValidationPolicy{
			MaxSourceExposure:         protocol.ExposureFocusedSnippets,
			MaxCostClass:              protocol.CostFrontierExpensive,
			RequireMeasuredProvenance: true,
		}

		res := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       makeTestInventory(),
			ContextProfiles: makeTestContextProfiles(),
			Policy:          &policy,
			Clock:           clk,
		})

		if res.Valid {
			t.Fatalf("expected rejection for configured provenance when measured required")
		}
		found := false
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeCapabilityProvenanceInvalid {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeCapabilityProvenanceInvalid in diagnostics: %v", res.Diagnostics)
		}
	})
}
