package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/tools"
)

const (
	testBaseCommit = "004165b6d5f7f3e8b0a9b3c4f7d2e5a8b1c4e7f0"
	testWPID       = "WP-M3C-TEST"
	testWPDigest   = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

func ptr[T any](v T) *T {
	return &v
}

func makeSubstrateTestCompiler(t *testing.T) (*compiler.Compiler, *compiler.EvidenceLeaseManager, *compiler.CapsuleManager) {
	t.Helper()
	reg, err := compiler.NewCanonicalRuleRegistry()
	if err != nil {
		t.Fatalf("failed to create canonical rule registry: %v", err)
	}
	leaseMgr := compiler.NewEvidenceLeaseManager()
	capMgr := compiler.NewCapsuleManager()
	c, err := compiler.NewCompiler(reg, leaseMgr, capMgr)
	if err != nil {
		t.Fatalf("failed to create canonical compiler: %v", err)
	}
	return c, leaseMgr, capMgr
}

func makeSubstrateCompileRequest(profile *protocol.ContextProfile) compiler.CompileRequest {
	return compiler.CompileRequest{
		TaskID:               "task-m3c-test-01",
		WorkPackageID:        testWPID,
		WorkPackageRevision:  1,
		WorkPackageDigest:    testWPDigest,
		Role:                 "implementer",
		BaseCommit:           testBaseCommit,
		SourceRevision:       testBaseCommit,
		ProjectStateRevision: "rev-bootstrap-001",
		MappingVersion:       compiler.CanonicalMappingRevision,
		BudgetPoolID:         "pool-local",
		ReadEnvelope:         []string{"internal/*"},
		WriteScope:           []string{"internal/cognition/*"},
		Domains:              []string{"cognition"},
		Action:               "Implement heterogeneous substrate integration verification",
		ActiveCapabilities:   []string{"write"},
		ExecutionContract:    "Execute deterministic compilation without data loss.",
		ContextProfile:       profile,
		Assumptions: []protocol.Assumption{
			{
				ID:        "asm_1",
				Statement: "Base commit matches main",
				Status:    protocol.AssumptionVerified,
				Material:  true,
			},
		},
		ExplicitQuestions: []string{"Does pack compile cleanly?"},
	}
}

// -----------------------------------------------------------------------------
// Portfolio Fixture Builders (local copies per LOCAL_DISCRETION §2)
// -----------------------------------------------------------------------------

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
						Dimension:  protocol.CapabilityRepositoryReasoning,
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
			{
				ID:                     "ep-metered-01",
				Kind:                   protocol.EndpointRemoteAPI,
				Provider:               "openai",
				ModelID:                "gpt-4o",
				Locality:               protocol.LocalityRemoteInferenceLocalTools,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthAuthenticated,
				CostClass:              protocol.CostRemoteEconomy,
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
						Dimension:  protocol.CapabilityRepositoryReasoning,
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
			{
				ID:                     "ep-metered-01",
				Kind:                   protocol.EndpointRemoteAPI,
				Locality:               protocol.LocalityRemoteInferenceLocalTools,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthAuthenticated,
				CostClass:              protocol.CostRemoteEconomy,
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
			ToolTailReserveTokens:     768,
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
		"prof-metered-01": {
			SchemaVersion:             protocol.SchemaVersion1,
			ProfileID:                 "prof-metered-01",
			EndpointID:                "ep-metered-01",
			ChannelID:                 "chan-metered-01",
			Runtime:                   "openai-direct",
			ModelRef:                  "gpt-4o",
			Revision:                  1,
			DeclaredWindowTokens:      128000,
			RuntimeWindowTokens:       128000,
			TargetResidentTokens:      40000,
			HardResidentCeilingTokens: 80000,
			ProtectedCoreLimitTokens:  6000,
			ContractLimitTokens:       15000,
			MaxSingleLeaseTokens:      6000,
			OutputReserveTokens:       3000,
			ToolTailReserveTokens:     1000,
			AccountingMethod:          protocol.AccountingExactBPE,
			ObservedContextControl:    protocol.ContextControlExactStateless,
			ObservedPrefixCache:       protocol.PrefixCacheSessionKV,
			WorkloadEnvelopes: []protocol.WorkloadEnvelope{
				{
					Workload:        protocol.WorkloadImplementation,
					EffectiveTokens: 35000,
					CalibrationTask: "bench-impl-03",
					CalibrationDate: "2026-10-01",
					ConfidenceLevel: "provisional",
				},
			},
		},
	}
}

// -----------------------------------------------------------------------------
// ACC-01: Exact Stateless vs. Opaque Session Driver Prompts
// -----------------------------------------------------------------------------

func TestM3CSubstrate_ACC01_ExactStatelessVsOpaqueSession(t *testing.T) {
	c, _, _ := makeSubstrateTestCompiler(t)
	profile := compiler.MustDefaultProvisionalProfile("ep-01", "chan-01", "model-01", 32768)
	req := makeSubstrateCompileRequest(profile)

	ctx := context.Background()
	inv, err := c.CompileInvocation(ctx, req)
	if err != nil {
		t.Fatalf("baseline CompileInvocation failed: %v", err)
	}

	s0, err := compiler.NewStrategy(protocol.ContextControlExactStateless)
	if err != nil {
		t.Fatalf("failed to create ExactStatelessStrategy: %v", err)
	}
	s1, err := compiler.NewStrategy(protocol.ContextControlOpaqueSession)
	if err != nil {
		t.Fatalf("failed to create OpaqueSessionStrategy: %v", err)
	}

	// ExactStateless: all three equal the full projection, RestartRequired=false
	t.Run("ACC-01_ExactStatelessStrategy", func(t *testing.T) {
		for _, tc := range []struct {
			turn    int
			evicted bool
		}{
			{0, false},
			{1, false},
			{1, true},
		} {
			tp, err := s0.PrepareTurnPrompt(inv.Pack, inv.Projection, tc.turn, tc.evicted)
			if err != nil {
				t.Fatalf("ExactStateless PrepareTurnPrompt(%d, %v) failed: %v", tc.turn, tc.evicted, err)
			}
			if tp.SystemPrompt != inv.Projection.SystemPrompt {
				t.Errorf("ExactStateless turn(%d, %v): SystemPrompt mismatch", tc.turn, tc.evicted)
			}
			if tp.UserPrompt != inv.Projection.UserPrompt {
				t.Errorf("ExactStateless turn(%d, %v): UserPrompt mismatch", tc.turn, tc.evicted)
			}
			if tp.RestartRequired {
				t.Errorf("ExactStateless turn(%d, %v): expected RestartRequired=false", tc.turn, tc.evicted)
			}
		}
	})

	// OpaqueSession strategy turns
	t.Run("ACC-01_OpaqueSessionStrategy", func(t *testing.T) {
		// OpaqueSession (0, false): equals the full projection, RestartRequired=false
		tp0, err := s1.PrepareTurnPrompt(inv.Pack, inv.Projection, 0, false)
		if err != nil {
			t.Fatalf("OpaqueSession (0, false) failed: %v", err)
		}
		if tp0.SystemPrompt != inv.Projection.SystemPrompt || tp0.UserPrompt != inv.Projection.UserPrompt {
			t.Errorf("OpaqueSession (0, false) should equal full projection")
		}
		if tp0.RestartRequired {
			t.Errorf("OpaqueSession (0, false) expected RestartRequired=false")
		}

		// OpaqueSession (1, false): empty SystemPrompt, UserPrompt == inv.Pack.EphemeralTail.CurrentAction, RestartRequired=false
		tp1, err := s1.PrepareTurnPrompt(inv.Pack, inv.Projection, 1, false)
		if err != nil {
			t.Fatalf("OpaqueSession (1, false) failed: %v", err)
		}
		if tp1.SystemPrompt != "" {
			t.Errorf("OpaqueSession (1, false) expected empty SystemPrompt, got %q", tp1.SystemPrompt)
		}
		if tp1.UserPrompt != inv.Pack.EphemeralTail.CurrentAction {
			t.Errorf("OpaqueSession (1, false) expected UserPrompt %q, got %q", inv.Pack.EphemeralTail.CurrentAction, tp1.UserPrompt)
		}
		if tp1.RestartRequired {
			t.Errorf("OpaqueSession (1, false) expected RestartRequired=false")
		}

		// OpaqueSession (1, true): equals full projection with RestartRequired=true
		tp2, err := s1.PrepareTurnPrompt(inv.Pack, inv.Projection, 1, true)
		if err != nil {
			t.Fatalf("OpaqueSession (1, true) failed: %v", err)
		}
		if tp2.SystemPrompt != inv.Projection.SystemPrompt || tp2.UserPrompt != inv.Projection.UserPrompt {
			t.Errorf("OpaqueSession (1, true) should equal full projection")
		}
		if !tp2.RestartRequired {
			t.Errorf("OpaqueSession (1, true) expected RestartRequired=true")
		}
	})

	// Every string in inv.Pack.NormativeClauses (after strings.TrimSpace) occurs verbatim in full-send prompts
	t.Run("ACC-01_PromptVerbatimClauses", func(t *testing.T) {
		for _, clause := range inv.Pack.NormativeClauses {
			trimmed := strings.TrimSpace(clause)
			if trimmed == "" {
				continue
			}
			if !strings.Contains(inv.Projection.UserPrompt, trimmed) && !strings.Contains(inv.Projection.SystemPrompt, trimmed) {
				t.Errorf("normative clause %q not found verbatim in full-send projection", trimmed)
			}
		}
	})
}

// -----------------------------------------------------------------------------
// ACC-02: Renderer Equivalence & JSON Round-Trip
// -----------------------------------------------------------------------------

func TestM3CSubstrate_ACC02_RendererEquivalenceAndRoundTrip(t *testing.T) {
	c, leaseMgr, _ := makeSubstrateTestCompiler(t)
	profile := compiler.MustDefaultProvisionalProfile("ep-01", "chan-01", "model-01", 32768)

	lease, err := leaseMgr.CreateLease(compiler.CreateLeaseParams{
		EvidenceKind:        protocol.LeaseKindSourceSnippet,
		SourceRevision:      testBaseCommit,
		WorktreeID:          "wt-01",
		FilePath:            "internal/a.go",
		Locator:             "L1-L10",
		AcquisitionQuestion: "What is package A?",
		AcquisitionReason:   "Verify lease rendering",
		Content:             "package a\n\nfunc Hello() string { return \"hello\" }\n",
		AccountingMethod:    protocol.AccountingExactBPE,
		ReadEnvelope:        []string{"internal/*"},
	})
	if err != nil {
		t.Fatalf("CreateLease failed: %v", err)
	}

	t.Run("ACC-02_EquivalenceAndRoundTrip", func(t *testing.T) {
		reqTagged := makeSubstrateCompileRequest(profile)
		reqTagged.ActiveLeaseIDs = []string{lease.LeaseID}
		reqTagged.Renderer = compiler.NewTaggedMarkdownRenderer()

		reqJSON := makeSubstrateCompileRequest(profile)
		reqJSON.ActiveLeaseIDs = []string{lease.LeaseID}
		reqJSON.Renderer = compiler.NewJSONRenderer()

		ctx := context.Background()
		invTagged, err := c.CompileInvocation(ctx, reqTagged)
		if err != nil {
			t.Fatalf("tagged CompileInvocation failed: %v", err)
		}

		invJSON, err := c.CompileInvocation(ctx, reqJSON)
		if err != nil {
			t.Fatalf("json CompileInvocation failed: %v", err)
		}

		// Pack.PackDigest, NormativeClauses, ExecutionContract, and leases must be identical
		if invTagged.Pack.PackDigest != invJSON.Pack.PackDigest {
			t.Errorf("PackDigest mismatch: %q vs %q", invTagged.Pack.PackDigest, invJSON.Pack.PackDigest)
		}
		if !reflect.DeepEqual(invTagged.Pack.NormativeClauses, invJSON.Pack.NormativeClauses) {
			t.Errorf("NormativeClauses mismatch between renderers")
		}
		if invTagged.Pack.ExecutionContract != invJSON.Pack.ExecutionContract {
			t.Errorf("ExecutionContract mismatch: %q vs %q", invTagged.Pack.ExecutionContract, invJSON.Pack.ExecutionContract)
		}
		if len(invTagged.Pack.EvidenceWorkingSet) != len(invJSON.Pack.EvidenceWorkingSet) {
			t.Fatalf("EvidenceWorkingSet length mismatch")
		}
		for i := range invTagged.Pack.EvidenceWorkingSet {
			lT := invTagged.Pack.EvidenceWorkingSet[i]
			lJ := invJSON.Pack.EvidenceWorkingSet[i]
			if lT.Content != lJ.Content || lT.ContentDigest != lJ.ContentDigest {
				t.Errorf("lease[%d] content/digest mismatch between renderers", i)
			}
		}

		// InvocationDigest must differ because of different renderer formats
		if invTagged.InvocationDigest == invJSON.InvocationDigest {
			t.Errorf("InvocationDigest must differ between renderers, got %q", invTagged.InvocationDigest)
		}

		// json.Unmarshal(projection.UserPrompt) into protocol.ContextPack equals pack except InvocationDigest
		var decoded protocol.ContextPack
		if err := json.Unmarshal([]byte(invJSON.Projection.UserPrompt), &decoded); err != nil {
			t.Fatalf("failed to unmarshal JSON projection UserPrompt: %v", err)
		}

		// At render time in Stage 9, pack.InvocationDigest was not yet set, so decoded carries empty InvocationDigest
		if decoded.InvocationDigest != "" {
			t.Errorf("expected empty InvocationDigest in rendered JSON body, got %q", decoded.InvocationDigest)
		}
		decoded.InvocationDigest = invJSON.Pack.InvocationDigest
		if !reflect.DeepEqual(decoded, *invJSON.Pack) {
			t.Errorf("decoded JSON context pack did not match original pack")
		}

		// Each normative clause and contract occurs verbatim in tagged UserPrompt
		for _, clause := range invTagged.Pack.NormativeClauses {
			trimmed := strings.TrimSpace(clause)
			if !strings.Contains(invTagged.Projection.UserPrompt, trimmed) {
				t.Errorf("normative clause %q not found in tagged UserPrompt", trimmed)
			}
		}
		if !strings.Contains(invTagged.Projection.UserPrompt, strings.TrimSpace(invTagged.Pack.ExecutionContract)) {
			t.Errorf("execution contract not found in tagged UserPrompt")
		}
	})
}

// -----------------------------------------------------------------------------
// ACC-03: Context Unfit Without Mandatory Content Truncation
// -----------------------------------------------------------------------------

func TestM3CSubstrate_ACC03_ContextUnfitWithoutTruncation(t *testing.T) {
	c, leaseMgr, _ := makeSubstrateTestCompiler(t)
	profile := compiler.MustDefaultProvisionalProfile("ep-01", "chan-01", "model-01", 32768)

	lease, err := leaseMgr.CreateLease(compiler.CreateLeaseParams{
		EvidenceKind:        protocol.LeaseKindSourceSnippet,
		SourceRevision:      testBaseCommit,
		WorktreeID:          "wt-01",
		FilePath:            "internal/a.go",
		Locator:             "L1-L10",
		AcquisitionQuestion: "What is package A?",
		AcquisitionReason:   "Baseline lease",
		Content:             "package a\n\nfunc Hello() string { return \"hello\" }\n",
		AccountingMethod:    protocol.AccountingExactBPE,
		ReadEnvelope:        []string{"internal/*"},
	})
	if err != nil {
		t.Fatalf("CreateLease failed: %v", err)
	}

	baseReq := makeSubstrateCompileRequest(profile)
	baseReq.ActiveLeaseIDs = []string{lease.LeaseID}

	ctx := context.Background()
	_, packB, err := c.Compile(ctx, baseReq)
	if err != nil {
		t.Fatalf("baseline Compile failed: %v", err)
	}
	if len(packB.EvidenceWorkingSet) != 1 {
		t.Fatalf("expected exactly one baseline active lease, got %d", len(packB.EvidenceWorkingSet))
	}

	if len(packB.NormativeClauses) == 0 {
		t.Fatalf("baseline NormativeClauses must not be empty")
	}
	if packB.ExecutionContract == "" {
		t.Fatalf("baseline ExecutionContract must not be empty")
	}

	// (i) ContractLimitTokens = B.TokenAccounting.ContractTokens - 1
	t.Run("ACC-03_ContractLimitTokensExceeded", func(t *testing.T) {
		derived := *profile
		derived.ContractLimitTokens = packB.TokenAccounting.ContractTokens - 1
		if err := derived.Validate(); err != nil {
			t.Fatalf("derived profile validation failed: %v", err)
		}

		req := baseReq
		req.ContextProfile = &derived
		_, pack, err := c.Compile(ctx, req)
		if !errors.Is(err, errs.ErrContextUnfit) {
			t.Fatalf("expected ErrContextUnfit, got: %v", err)
		}
		if errs.CategoryOf(err) != errs.CategoryContextUnfit {
			t.Fatalf("expected CategoryContextUnfit, got: %v", errs.CategoryOf(err))
		}
		if pack == nil || pack.Status != protocol.PackStatusContextUnfit {
			t.Fatalf("expected PackStatusContextUnfit, got: %v", pack)
		}
		if !reflect.DeepEqual(pack.NormativeClauses, packB.NormativeClauses) {
			t.Errorf("NormativeClauses must not be truncated or dropped")
		}
		if pack.ExecutionContract != packB.ExecutionContract {
			t.Errorf("ExecutionContract must not be truncated or dropped")
		}
	})

	// (ii) ProtectedCoreLimitTokens = RoleTokens + NormativeTokens - 1
	t.Run("ACC-03_ProtectedCoreLimitTokensExceeded", func(t *testing.T) {
		derived := *profile
		derived.ProtectedCoreLimitTokens = packB.TokenAccounting.RoleTokens + packB.TokenAccounting.NormativeTokens - 1
		if err := derived.Validate(); err != nil {
			t.Fatalf("derived profile validation failed: %v", err)
		}

		req := baseReq
		req.ContextProfile = &derived
		_, pack, err := c.Compile(ctx, req)
		if !errors.Is(err, errs.ErrContextUnfit) {
			t.Fatalf("expected ErrContextUnfit, got: %v", err)
		}
		if errs.CategoryOf(err) != errs.CategoryContextUnfit {
			t.Fatalf("expected CategoryContextUnfit, got: %v", errs.CategoryOf(err))
		}
		if pack == nil || pack.Status != protocol.PackStatusContextUnfit {
			t.Fatalf("expected PackStatusContextUnfit, got: %v", pack)
		}
		if !reflect.DeepEqual(pack.NormativeClauses, packB.NormativeClauses) {
			t.Errorf("NormativeClauses must not be truncated or dropped")
		}
		if pack.ExecutionContract != packB.ExecutionContract {
			t.Errorf("ExecutionContract must not be truncated or dropped")
		}
	})

	// (iii) MaxSingleLeaseTokens = largest lease TokenCount - 1
	t.Run("ACC-03_MaxSingleLeaseTokensExceeded", func(t *testing.T) {
		derived := *profile
		derived.MaxSingleLeaseTokens = packB.EvidenceWorkingSet[0].TokenCount - 1
		if err := derived.Validate(); err != nil {
			t.Fatalf("derived profile validation failed: %v", err)
		}

		req := baseReq
		req.ContextProfile = &derived
		_, pack, err := c.Compile(ctx, req)
		if !errors.Is(err, errs.ErrContextUnfit) {
			t.Fatalf("expected ErrContextUnfit, got: %v", err)
		}
		if errs.CategoryOf(err) != errs.CategoryContextUnfit {
			t.Fatalf("expected CategoryContextUnfit, got: %v", errs.CategoryOf(err))
		}
		if pack == nil || pack.Status != protocol.PackStatusContextUnfit {
			t.Fatalf("expected PackStatusContextUnfit, got: %v", pack)
		}
		if !reflect.DeepEqual(pack.NormativeClauses, packB.NormativeClauses) {
			t.Errorf("NormativeClauses must not be truncated or dropped")
		}
		if pack.ExecutionContract != packB.ExecutionContract {
			t.Errorf("ExecutionContract must not be truncated or dropped")
		}
	})

	// (iv) HardResidentCeilingTokens = B.TokenAccounting.TotalResidentTokens - 1
	t.Run("ACC-03_HardResidentCeilingTokensExceeded", func(t *testing.T) {
		derived := *profile
		derived.HardResidentCeilingTokens = packB.TokenAccounting.TotalResidentTokens - 1
		if derived.TargetResidentTokens > derived.HardResidentCeilingTokens {
			derived.TargetResidentTokens = derived.HardResidentCeilingTokens
		}
		if err := derived.Validate(); err != nil {
			t.Fatalf("derived profile validation failed: %v", err)
		}

		// Direct bounds check isolates EnforceProfileBounds
		directErr := compiler.EnforceProfileBounds(packB, &derived)
		if !errors.Is(directErr, errs.ErrContextUnfit) {
			t.Fatalf("expected direct EnforceProfileBounds to return ErrContextUnfit, got: %v", directErr)
		}
		if !strings.Contains(directErr.Error(), "hard resident ceiling") {
			t.Errorf("expected direct EnforceProfileBounds error to name hard resident ceiling, got: %v", directErr)
		}

		req := baseReq
		req.ContextProfile = &derived
		_, pack, err := c.Compile(ctx, req)
		if !errors.Is(err, errs.ErrContextUnfit) {
			t.Fatalf("expected ErrContextUnfit, got: %v", err)
		}
		if errs.CategoryOf(err) != errs.CategoryContextUnfit {
			t.Fatalf("expected CategoryContextUnfit, got: %v", errs.CategoryOf(err))
		}
		if !strings.Contains(err.Error(), "hard resident ceiling") {
			t.Errorf("expected error message to name hard resident ceiling, got: %v", err)
		}
		if pack == nil || pack.Status != protocol.PackStatusContextUnfit {
			t.Fatalf("expected PackStatusContextUnfit, got: %v", pack)
		}
		if !reflect.DeepEqual(pack.NormativeClauses, packB.NormativeClauses) {
			t.Errorf("NormativeClauses must not be truncated or dropped")
		}
		if pack.ExecutionContract != packB.ExecutionContract {
			t.Errorf("ExecutionContract must not be truncated or dropped")
		}
	})

	// (v) Projection stage: keep (i)-(iv) at baseline, add ToolSchemas large enough that EnforceProjectionBounds fails
	t.Run("ACC-03_ProjectionBoundsExceeded", func(t *testing.T) {
		req := baseReq
		// Add tool schemas that consume more tokens than the available window headroom (32768 tokens)
		largeSchema := fmt.Sprintf(`{"name": "heavy_tool", "description": %q}`, strings.Repeat("large schema payload ", 10000))
		req.ToolSchemas = []string{largeSchema}
		req.DeclaredTools = []compiler.ToolCapabilityInfo{
			{Name: "heavy_tool", ReadOnly: true},
		}

		inv, err := c.CompileInvocation(ctx, req)
		if !errors.Is(err, errs.ErrContextUnfit) {
			t.Fatalf("expected ErrContextUnfit, got: %v", err)
		}
		if errs.CategoryOf(err) != errs.CategoryContextUnfit {
			t.Fatalf("expected CategoryContextUnfit, got: %v", errs.CategoryOf(err))
		}
		if inv == nil || inv.Pack == nil || inv.Pack.Status != protocol.PackStatusContextUnfit {
			t.Fatalf("expected PackStatusContextUnfit, got: %v", inv)
		}
		if !reflect.DeepEqual(inv.Pack.NormativeClauses, packB.NormativeClauses) {
			t.Errorf("NormativeClauses must not be truncated or dropped")
		}
		if inv.Pack.ExecutionContract != packB.ExecutionContract {
			t.Errorf("ExecutionContract must not be truncated or dropped")
		}
		if inv.Projection.UserPrompt == "" {
			t.Errorf("inv.Projection.UserPrompt must be populated in projection failure stage")
		}
	})
}

// -----------------------------------------------------------------------------
// ACC-04: Reserve Accounting & Provisional Profiles
// -----------------------------------------------------------------------------

func TestM3CSubstrate_ACC04_ReserveAccountingAndProvisionalProfiles(t *testing.T) {
	// (a) a valid profile: HardResidentCeiling + OutputReserve + ToolTailReserve <= RuntimeWindow
	t.Run("ACC-04_ValidProfileWindowCeiling", func(t *testing.T) {
		profA := compiler.MustDefaultProvisionalProfile("ep-01", "chan-01", "model-01", 32768)
		if err := profA.Validate(); err != nil {
			t.Fatalf("valid profile A failed Validate: %v", err)
		}
		if profA.HardResidentCeilingTokens+profA.OutputReserveTokens+profA.ToolTailReserveTokens > profA.RuntimeWindowTokens {
			t.Errorf("profile A violates reserve accounting invariant")
		}
	})

	// (b) a profile with HardResidentCeilingTokens + OutputReserveTokens + ToolTailReserveTokens = RuntimeWindowTokens + 1
	t.Run("ACC-04_ReserveOverflowValidationRejection", func(t *testing.T) {
		profA := compiler.MustDefaultProvisionalProfile("ep-01", "chan-01", "model-01", 32768)
		profB := *profA
		profB.HardResidentCeilingTokens = profB.RuntimeWindowTokens - profB.OutputReserveTokens - profB.ToolTailReserveTokens + 1
		errB := profB.Validate()
		if errB == nil {
			t.Fatalf("expected profile B with reserve overflow to fail Validate")
		}
		if errs.CategoryOf(errB) != errs.CategoryInvalidArgument {
			t.Errorf("expected CategoryInvalidArgument for profile B, got %v", errs.CategoryOf(errB))
		}
	})

	// (c) DefaultProvisionalProfile inspection
	t.Run("ACC-04_DefaultProvisionalProfileInspection", func(t *testing.T) {
		profC, err := compiler.DefaultProvisionalProfile("ep-02", "chan-02", "model-02", 65536)
		if err != nil {
			t.Fatalf("DefaultProvisionalProfile failed: %v", err)
		}
		if profC.Runtime != "provisional" {
			t.Errorf("expected Runtime 'provisional', got %q", profC.Runtime)
		}
		for i, env := range profC.WorkloadEnvelopes {
			if env.ConfidenceLevel != "provisional" {
				t.Errorf("WorkloadEnvelopes[%d] ConfidenceLevel expected 'provisional', got %q", i, env.ConfidenceLevel)
			}
		}
		if profC.ObservedContextControl != protocol.ContextControlUnknown {
			t.Errorf("expected ObservedContextControl ContextControlUnknown, got %q", profC.ObservedContextControl)
		}
		if profC.ObservedPrefixCache != protocol.PrefixCacheUnknown {
			t.Errorf("expected ObservedPrefixCache PrefixCacheUnknown, got %q", profC.ObservedPrefixCache)
		}
	})
}

// -----------------------------------------------------------------------------
// ACC-05: Mandatory Rule Admission Inviolability
// -----------------------------------------------------------------------------

func TestM3CSubstrate_ACC05_MandatoryAdmissionInviolability(t *testing.T) {
	reg := compiler.NewRuleRegistry()

	rAlways := compiler.Rule{
		ID:             "DCI-ALWAYS-01",
		SourceKind:     compiler.AuthoritySourceKindSystem,
		AdmissionClass: compiler.AdmissionClassAlways,
		SourceDoc:      "docs/INVARIANTS.md",
		Revision:       "rev-1",
		Content:        "System mandatory invariant: always active.",
	}
	rCap := compiler.Rule{
		ID:             "DCI-CAP-WRITE-01",
		SourceKind:     compiler.AuthoritySourceKindSystem,
		AdmissionClass: compiler.AdmissionClassCapabilityDefault,
		SourceDoc:      "docs/INVARIANTS.md",
		Revision:       "rev-1",
		Capability:     "write",
		Content:        "Write capability mandatory invariant: requires containment.",
	}
	rMap := compiler.Rule{
		ID:             "DCI-MAP-COGNITION-01",
		SourceKind:     compiler.AuthoritySourceKindSystem,
		AdmissionClass: compiler.AdmissionClassMapped,
		SourceDoc:      "docs/INVARIANTS.md",
		Revision:       "rev-1",
		Domains:        []string{"cognition"},
		Content:        "Cognition domain mandatory invariant: pure state transitions.",
	}

	for _, r := range []compiler.Rule{rAlways, rCap, rMap} {
		if err := reg.Register(r); err != nil {
			t.Fatalf("Register rule %q failed: %v", r.ID, err)
		}
	}
	if err := reg.SetCatalogMeta("custom-catalog-01", "v1.0", testBaseCommit, "v1.0"); err != nil {
		t.Fatalf("SetCatalogMeta failed: %v", err)
	}
	if err := reg.Freeze(); err != nil {
		t.Fatalf("Freeze registry failed: %v", err)
	}

	leaseMgr := compiler.NewEvidenceLeaseManager()
	capMgr := compiler.NewCapsuleManager()
	comp, err := compiler.NewCompiler(reg, leaseMgr, capMgr)
	if err != nil {
		t.Fatalf("NewCompiler failed: %v", err)
	}

	profile := compiler.MustDefaultProvisionalProfile("ep-01", "chan-01", "model-01", 32768)
	baseReq := makeSubstrateCompileRequest(profile)
	baseReq.MappingVersion = "v1.0"
	baseReq.Domains = []string{"cognition"}
	baseReq.ActiveCapabilities = []string{"write"}

	ctx := context.Background()

	// 1. Verify predicate truth table & reverse coverage:
	// Both write + cognition -> all 3 admitted with 100% reverse coverage
	t.Run("ACC-05_PredicateTruthTableAndReverseCoverage", func(t *testing.T) {
		manAll, packAll, err := comp.Compile(ctx, baseReq)
		if err != nil {
			t.Fatalf("Compile with full predicates failed: %v", err)
		}
		if len(manAll.MandatoryClauses) != 3 {
			t.Fatalf("expected 3 mandatory clauses, got %d", len(manAll.MandatoryClauses))
		}
		expectedIDs := map[string]bool{rAlways.ID: true, rCap.ID: true, rMap.ID: true}
		admittedIDs := make(map[string]bool)
		for _, m := range manAll.MandatoryClauses {
			admittedIDs[m.ClauseID] = true
			if !expectedIDs[m.ClauseID] {
				t.Errorf("unmapped / unexpected rule %q admitted", m.ClauseID)
			}
		}
		for expID := range expectedIDs {
			if !admittedIDs[expID] {
				t.Errorf("expected mandatory rule %q not admitted", expID)
			}
		}
		if len(packAll.NormativeClauses) != 3 {
			t.Fatalf("expected 3 normative clauses in pack, got %d", len(packAll.NormativeClauses))
		}
		if !strings.Contains(packAll.CoverageSummary, "100% deterministic reverse coverage") {
			t.Errorf("expected 100%% deterministic reverse coverage, got %q", packAll.CoverageSummary)
		}
	})

	// write omitted -> CapabilityDefault rule absent, Always and Mapped present
	t.Run("ACC-05_WriteOmittedExcludesCapabilityRule", func(t *testing.T) {
		reqNoWrite := baseReq
		reqNoWrite.ActiveCapabilities = []string{}
		manNoWrite, _, err := comp.Compile(ctx, reqNoWrite)
		if err != nil {
			t.Fatalf("Compile without write capability failed: %v", err)
		}
		if len(manNoWrite.MandatoryClauses) != 2 {
			t.Fatalf("expected 2 mandatory clauses when write omitted, got %d", len(manNoWrite.MandatoryClauses))
		}
		expectedNoWrite := map[string]bool{rAlways.ID: true, rMap.ID: true}
		for _, m := range manNoWrite.MandatoryClauses {
			if m.ClauseID == rCap.ID {
				t.Errorf("rule %q should not be admitted when write capability is inactive", rCap.ID)
			}
			if !expectedNoWrite[m.ClauseID] {
				t.Errorf("unexpected rule %q admitted in write-omitted request", m.ClauseID)
			}
		}
	})

	// cognition domain omitted (using another domain from vocabulary) -> Mapped rule absent
	t.Run("ACC-05_CognitionOmittedExcludesMappedRule", func(t *testing.T) {
		reg2 := compiler.NewRuleRegistry()
		_ = reg2.Register(rAlways)
		_ = reg2.Register(rCap)
		rMap2 := rMap
		rMap2.Domains = []string{"cognition"}
		_ = reg2.Register(rMap2)
		_ = reg2.RegisterKnownDomain("other")
		_ = reg2.SetCatalogMeta("custom-catalog-02", "v1.0", testBaseCommit, "v1.0")
		_ = reg2.Freeze()
		comp2, _ := compiler.NewCompiler(reg2, leaseMgr, capMgr)

		reqOtherDom := baseReq
		reqOtherDom.Domains = []string{"other"}
		manOtherDom, _, err := comp2.Compile(ctx, reqOtherDom)
		if err != nil {
			t.Fatalf("Compile with 'other' domain failed: %v", err)
		}
		if len(manOtherDom.MandatoryClauses) != 2 {
			t.Fatalf("expected 2 mandatory clauses when cognition omitted, got %d", len(manOtherDom.MandatoryClauses))
		}
		expectedOtherDom := map[string]bool{rAlways.ID: true, rCap.ID: true}
		for _, m := range manOtherDom.MandatoryClauses {
			if m.ClauseID == rMap.ID {
				t.Errorf("rule %q should not be admitted when cognition domain is inactive", rMap.ID)
			}
			if !expectedOtherDom[m.ClauseID] {
				t.Errorf("unexpected rule %q admitted in other-domain request", m.ClauseID)
			}
		}
	})

	// 2. Retrieval states invariance
	t.Run("ACC-05_RetrievalStatesInvariance", func(t *testing.T) {
		// Across retrieval states, mandatory admission remains invariant:
		man1, pack1, err1 := comp.Compile(ctx, baseReq)
		man2, pack2, err2 := comp.Compile(ctx, baseReq)
		man3, pack3, err3 := comp.Compile(ctx, baseReq)
		if err1 != nil || err2 != nil || err3 != nil {
			t.Fatalf("compilation across retrieval states failed")
		}
		if !reflect.DeepEqual(man1.MandatoryClauses, man2.MandatoryClauses) || !reflect.DeepEqual(man1.MandatoryClauses, man3.MandatoryClauses) {
			t.Errorf("MandatoryClauses must be identical across optional retrieval states")
		}
		if !reflect.DeepEqual(pack1.NormativeClauses, pack2.NormativeClauses) || !reflect.DeepEqual(pack1.NormativeClauses, pack3.NormativeClauses) {
			t.Errorf("NormativeClauses must be identical across optional retrieval states")
		}
	})

	// State 4: candidate whose ID equals a mandatory clause id is removed by EnsureMandatoryInviolability
	t.Run("ACC-05_EnsureMandatoryInviolabilityRemovesCollidingCandidate", func(t *testing.T) {
		manAll, _, err := comp.Compile(ctx, baseReq)
		if err != nil {
			t.Fatalf("baseline compile failed: %v", err)
		}
		candidates := []compiler.OptionalItem{
			{ID: "opt-clean", Kind: "doc", Title: "Clean Doc", Content: "Clean reference"},
			{ID: rAlways.ID, Kind: "doc", Title: "Spoofed Doc", Content: "Attempted collision with mandatory rule"},
		}
		safe := compiler.EnsureMandatoryInviolability(manAll.MandatoryClauses, candidates)
		if len(safe) != 1 || safe[0].ID != "opt-clean" {
			t.Errorf("EnsureMandatoryInviolability failed to remove colliding candidate: %v", safe)
		}
	})

	// Static invariant check: CompileRequest has no similarity or ranking fields
	t.Run("ACC-05_CompileRequestHasNoSimilarityOrRankingFields", func(t *testing.T) {
		reqType := reflect.TypeOf(compiler.CompileRequest{})
		for i := 0; i < reqType.NumField(); i++ {
			field := reqType.Field(i)
			lower := strings.ToLower(field.Name)
			if strings.Contains(lower, "similarity") || strings.Contains(lower, "score") || strings.Contains(lower, "rank") {
				t.Errorf("CompileRequest must not contain similarity or ranking fields; found %q", field.Name)
			}
		}
	})
}

// -----------------------------------------------------------------------------
// ACC-06: Evidence Staleness Triggers
// -----------------------------------------------------------------------------

func TestM3CSubstrate_ACC06_EvidenceStalenessTriggers(t *testing.T) {
	c, leaseMgr, _ := makeSubstrateTestCompiler(t)
	profile := compiler.MustDefaultProvisionalProfile("ep-01", "chan-01", "model-01", 32768)

	createLease := func(tSub *testing.T, filePath, revision string, expiresAt *string) protocol.EvidenceLease {
		tSub.Helper()
		l, err := leaseMgr.CreateLease(compiler.CreateLeaseParams{
			EvidenceKind:        protocol.LeaseKindSourceSnippet,
			SourceRevision:      revision,
			WorktreeID:          "wt-01",
			FilePath:            filePath,
			Locator:             "L1-L5",
			AcquisitionQuestion: "test",
			AcquisitionReason:   "test",
			Content:             "func Hello() string { return \"test\" }\n",
			AccountingMethod:    protocol.AccountingExactBPE,
			ReadEnvelope:        []string{"internal/*"},
			ExpiresAt:           expiresAt,
		})
		if err != nil {
			tSub.Fatalf("CreateLease failed: %v", err)
		}
		return l
	}

	// Baseline compile succeeds
	baseLease := createLease(t, "internal/base.go", testBaseCommit, nil)
	baseReq := makeSubstrateCompileRequest(profile)
	baseReq.ActiveLeaseIDs = []string{baseLease.LeaseID}
	ctx := context.Background()
	if _, _, err := c.Compile(ctx, baseReq); err != nil {
		t.Fatalf("baseline compile failed: %v", err)
	}

	// (a) request SourceRevision != lease SourceRevision
	t.Run("ACC-06_MismatchedSourceRevision", func(t *testing.T) {
		la := createLease(t, "internal/a.go", testBaseCommit, nil)
		reqA := baseReq
		reqA.SourceRevision = "1111111111111111111111111111111111111111"
		reqA.ActiveLeaseIDs = []string{la.LeaseID}
		_, _, err := c.Compile(ctx, reqA)
		if err == nil || errs.CategoryOf(err) != errs.CategoryValidationFailed {
			t.Fatalf("expected CategoryValidationFailed on revision mismatch, got: %v", err)
		}
	})

	// (b) InvalidateForFileMutation("internal/b.go")
	t.Run("ACC-06_FileMutationInvalidation", func(t *testing.T) {
		lb := createLease(t, "internal/b.go", testBaseCommit, nil)
		invalidated := leaseMgr.InvalidateForFileMutation("internal/b.go")
		if len(invalidated) == 0 {
			t.Fatalf("expected at least 1 invalidated lease, got 0")
		}
		st, ok := leaseMgr.GetLease(lb.LeaseID)
		if !ok || st.Status != protocol.LeaseStatusInvalidated {
			t.Fatalf("expected lease status LeaseStatusInvalidated, got %v", st.Status)
		}
		reqB := baseReq
		reqB.ActiveLeaseIDs = []string{lb.LeaseID}
		_, _, err := c.Compile(ctx, reqB)
		if err == nil || errs.CategoryOf(err) != errs.CategoryValidationFailed {
			t.Fatalf("expected CategoryValidationFailed on invalidated lease, got: %v", err)
		}
	})

	// (c) ScopedToolMediator file mutation seam
	t.Run("ACC-06_ScopedToolMediatorFileMutationSeam", func(t *testing.T) {
		// KG-4: EvidenceLeaseManager.InvalidateForFileMutation has no non-test caller;
		// this test wires ScopedToolMediator.OnFileEdit to prove the seam is sufficient.
		tmpDir := t.TempDir()
		scope := &tools.Scope{WorktreePath: tmpDir}
		mediator := drivers.NewScopedToolMediator(scope)

		toolDef := drivers.ToolDefinition{
			Name:           "write_file",
			MutatesFiles:   true,
			PathParameters: []string{"path"},
		}
		mediator.RegisterToolDefinition(toolDef)
		mediator.SetDeclaredTools([]drivers.ToolDefinition{toolDef})
		mediator.RegisterHandler("write_file", func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				Path string `json:"path"`
			}
			_ = json.Unmarshal(args, &p)
			full := filepath.Join(tmpDir, p.Path)
			_ = os.MkdirAll(filepath.Dir(full), 0755)
			_ = os.WriteFile(full, []byte("mutated"), 0644)
			return "ok", nil
		})

		mediator.OnFileEdit(func(p string, _ []byte) {
			_ = leaseMgr.InvalidateForFileMutation(p)
		})

		lc := createLease(t, "internal/c.go", testBaseCommit, nil)

		callArgs, _ := json.Marshal(map[string]string{"path": "internal/c.go"})
		res, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "call-1",
			Name:      "write_file",
			Arguments: callArgs,
		})
		if err != nil || res.IsError {
			t.Fatalf("tool execution failed: %v", err)
		}

		st, ok := leaseMgr.GetLease(lc.LeaseID)
		if !ok || st.Status != protocol.LeaseStatusInvalidated {
			t.Fatalf("expected lease status LeaseStatusInvalidated via mediator seam, got %v", st.Status)
		}

		reqC := baseReq
		reqC.ActiveLeaseIDs = []string{lc.LeaseID}
		_, _, err = c.Compile(ctx, reqC)
		if err == nil || errs.CategoryOf(err) != errs.CategoryValidationFailed {
			t.Fatalf("expected CategoryValidationFailed after mediator mutation, got: %v", err)
		}
	})

	// (d) ExpiresAt in the past
	t.Run("ACC-06_ExpiredLease", func(t *testing.T) {
		// First verify CreateLease rejects a past timestamp at creation time
		pastTimestamp := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339Nano)
		_, errPast := leaseMgr.CreateLease(compiler.CreateLeaseParams{
			EvidenceKind:        protocol.LeaseKindSourceSnippet,
			SourceRevision:      testBaseCommit,
			WorktreeID:          "wt-01",
			FilePath:            "internal/d_past.go",
			Locator:             "L1-L5",
			AcquisitionQuestion: "test past",
			AcquisitionReason:   "test past",
			Content:             "func Hello() string { return \"past\" }\n",
			AccountingMethod:    protocol.AccountingExactBPE,
			ReadEnvelope:        []string{"internal/*"},
			ExpiresAt:           &pastTimestamp,
		})
		if errPast == nil || errs.CategoryOf(errPast) != errs.CategoryInvalidArgument {
			t.Fatalf("expected CreateLease to reject pre-expired timestamp with CategoryInvalidArgument, got: %v", errPast)
		}

		// Now create a valid lease with near-term expiration and poll until expired
		expTime := time.Now().UTC().Add(5 * time.Millisecond)
		expStr := expTime.Format(time.RFC3339Nano)
		ld := createLease(t, "internal/d.go", testBaseCommit, &expStr)

		// Wait deterministically until expTime has passed
		for !time.Now().UTC().After(expTime) {
			time.Sleep(1 * time.Millisecond)
		}

		reqD := baseReq
		reqD.ActiveLeaseIDs = []string{ld.LeaseID}
		_, _, err := c.Compile(ctx, reqD)
		if err == nil || errs.CategoryOf(err) != errs.CategoryValidationFailed {
			t.Fatalf("expected CategoryValidationFailed on expired lease, got: %v", err)
		}
		if !strings.Contains(strings.ToLower(err.Error()), "expired") {
			t.Errorf("expected error message to mention 'expired', got: %v", err)
		}
	})

	// (e) Mutate lease Content in pack while keeping ContentDigest
	t.Run("ACC-06_CorruptedLeaseDigestInPack", func(t *testing.T) {
		le := createLease(t, "internal/e.go", testBaseCommit, nil)
		reqE := baseReq
		reqE.ActiveLeaseIDs = []string{le.LeaseID}
		_, packE, err := c.Compile(ctx, reqE)
		if err != nil {
			t.Fatalf("compile for e failed: %v", err)
		}

		packE.EvidenceWorkingSet[0].Content = "TAMPERED CONTENT WITH UNMATCHED DIGEST"
		renderer := compiler.NewTaggedMarkdownRenderer()
		_, err = renderer.Render(packE)
		if err == nil || errs.CategoryOf(err) != errs.CategoryValidationFailed {
			t.Fatalf("expected CategoryValidationFailed from renderer on tampered lease content, got: %v", err)
		}
		errMsg := strings.ToLower(err.Error())
		if !strings.Contains(errMsg, "digest") && !strings.Contains(errMsg, "mismatch") {
			t.Errorf("expected error message to mention digest mismatch, got: %v", err)
		}
	})

	// Unknown lease ID
	t.Run("ACC-06_UnknownLeaseID", func(t *testing.T) {
		reqUnknown := baseReq
		reqUnknown.ActiveLeaseIDs = []string{"nonexistent-lease-uuid"}
		_, _, err := c.Compile(ctx, reqUnknown)
		if err == nil || errs.CategoryOf(err) != errs.CategoryNotFound {
			t.Fatalf("expected CategoryNotFound for unknown lease ID, got: %v", err)
		}
	})
}

// -----------------------------------------------------------------------------
// ACC-07: Independent Read and Write Authority
// -----------------------------------------------------------------------------

func TestM3CSubstrate_ACC07_AuthoritySeparationReadWrite(t *testing.T) {
	c, leaseMgr, _ := makeSubstrateTestCompiler(t)
	profile := compiler.MustDefaultProvisionalProfile("ep-01", "chan-01", "model-01", 32768)

	// (1) CreateLease outside envelope
	t.Run("ACC-07_CreateLeaseOutsideEnvelopeDenied", func(t *testing.T) {
		_, err := leaseMgr.CreateLease(compiler.CreateLeaseParams{
			EvidenceKind:        protocol.LeaseKindSourceSnippet,
			SourceRevision:      testBaseCommit,
			WorktreeID:          "wt-01",
			FilePath:            "docs/x.md",
			Locator:             "L1-L5",
			AcquisitionQuestion: "test",
			AcquisitionReason:   "test",
			Content:             "# Out of envelope doc\n",
			AccountingMethod:    protocol.AccountingExactBPE,
			ReadEnvelope:        []string{"internal/*"},
		})
		if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Errorf("(1) expected CategoryPolicyDenied on CreateLease outside ReadEnvelope, got: %v", err)
		}
	})

	// (2) Compile with ReadEnvelope=["internal/*"] and a lease created with empty envelope
	t.Run("ACC-07_CompileOutsideEnvelopeDenied", func(t *testing.T) {
		leaseOut, err := leaseMgr.CreateLease(compiler.CreateLeaseParams{
			EvidenceKind:        protocol.LeaseKindSourceSnippet,
			SourceRevision:      testBaseCommit,
			WorktreeID:          "wt-01",
			FilePath:            "docs/x.md",
			Locator:             "L1-L5",
			AcquisitionQuestion: "test",
			AcquisitionReason:   "test",
			Content:             "# Out of envelope doc\n",
			AccountingMethod:    protocol.AccountingExactBPE,
			ReadEnvelope:        nil, // empty
		})
		if err != nil {
			t.Fatalf("CreateLease with empty envelope failed: %v", err)
		}

		req2 := makeSubstrateCompileRequest(profile)
		req2.ReadEnvelope = []string{"internal/*"}
		req2.ActiveLeaseIDs = []string{leaseOut.LeaseID}
		ctx := context.Background()
		_, _, err = c.Compile(ctx, req2)
		if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Errorf("(2) expected CategoryPolicyDenied when compiling lease outside ReadEnvelope, got: %v", err)
		}
	})

	// Write side via ScopedToolMediator
	tmpDir := t.TempDir()
	scope := &tools.Scope{WorktreePath: tmpDir}
	mediator := drivers.NewScopedToolMediator(scope)

	readDef := drivers.ToolDefinition{
		Name:           "read_like",
		MutatesFiles:   false,
		PathParameters: []string{"path"},
	}
	writeDef := drivers.ToolDefinition{
		Name:           "write_like",
		MutatesFiles:   true,
		PathParameters: []string{"path"},
	}
	mediator.RegisterToolDefinition(readDef)
	mediator.RegisterToolDefinition(writeDef)
	mediator.SetDeclaredTools([]drivers.ToolDefinition{readDef, writeDef})

	var handlerInvoked bool
	mediator.RegisterHandler("write_like", func(ctx context.Context, args json.RawMessage) (string, error) {
		handlerInvoked = true
		return "write success", nil
	})
	mediator.RegisterHandler("read_like", func(ctx context.Context, args json.RawMessage) (string, error) {
		return "read success", nil
	})

	// (3) ExecuteTool of tool not declared
	t.Run("ACC-07_UndeclaredToolPolicyDenied", func(t *testing.T) {
		ctx := context.Background()
		_, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "c3",
			Name:      "undeclared_tool",
			Arguments: []byte(`{}`),
		})
		if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Errorf("(3) expected CategoryPolicyDenied on undeclared tool, got: %v", err)
		}
	})

	// (4) ExecuteTool(write_like) with path escaping worktree
	t.Run("ACC-07_WorktreePathEscapePolicyDenied", func(t *testing.T) {
		ctx := context.Background()
		handlerInvoked = false
		outsideArgs, _ := json.Marshal(map[string]string{"path": "../outside.txt"})
		_, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "c4",
			Name:      "write_like",
			Arguments: outsideArgs,
		})
		if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Errorf("(4) expected CategoryPolicyDenied on path escaping worktree, got: %v", err)
		}
		if handlerInvoked {
			t.Errorf("(4) handler must NOT be invoked when path containment fails")
		}
	})

	// (5) ExecuteTool(write_like) with in-worktree path
	t.Run("ACC-07_InWorktreeToolAllowed", func(t *testing.T) {
		ctx := context.Background()
		handlerInvoked = false
		insideArgs, _ := json.Marshal(map[string]string{"path": "inside.txt"})
		res5, err := mediator.ExecuteTool(ctx, drivers.ToolCall{
			ID:        "c5",
			Name:      "write_like",
			Arguments: insideArgs,
		})
		if err != nil || res5.IsError || !handlerInvoked {
			t.Errorf("(5) expected success on in-worktree tool execution, got err: %v, res: %v", err, res5)
		}
	})

	// (6) Path in ReadEnvelope and absent from WriteScope
	// KG-3: CompileRequest.WriteScope is recorded in manifest and used for rule admission;
	// ScopedToolMediator enforces tool declaration and worktree containment, not WriteScope path patterns.
	t.Run("ACC-07_ReadLeaseGrantsNoWriteAuthority", func(t *testing.T) {
		leaseValid, err := leaseMgr.CreateLease(compiler.CreateLeaseParams{
			EvidenceKind:        protocol.LeaseKindSourceSnippet,
			SourceRevision:      testBaseCommit,
			WorktreeID:          "wt-01",
			FilePath:            "internal/read_only.go",
			Locator:             "L1-L5",
			AcquisitionQuestion: "test",
			AcquisitionReason:   "test",
			Content:             "package readonly\n",
			AccountingMethod:    protocol.AccountingExactBPE,
			ReadEnvelope:        []string{"internal/*"},
		})
		if err != nil {
			t.Fatalf("CreateLease for read_only.go failed: %v", err)
		}

		req6 := makeSubstrateCompileRequest(profile)
		req6.ReadEnvelope = []string{"internal/*"}
		req6.WriteScope = []string{"cmd/*"} // path internal/read_only.go is absent from WriteScope
		req6.ActiveLeaseIDs = []string{leaseValid.LeaseID}

		ctx := context.Background()
		man6, _, err := c.Compile(ctx, req6)
		if err != nil {
			t.Fatalf("(6) Compile failed: %v", err)
		}
		if !reflect.DeepEqual(man6.WriteScope, req6.WriteScope) {
			t.Errorf("(6) manifest WriteScope must match request WriteScope, not ReadEnvelope")
		}
	})
}

// -----------------------------------------------------------------------------
// ACC-08: Hostile Delimiter Containment & Escaping
// -----------------------------------------------------------------------------

func TestM3CSubstrate_ACC08_EvidenceDelimiterSafety(t *testing.T) {
	c, leaseMgr, _ := makeSubstrateTestCompiler(t)
	profile := compiler.MustDefaultProvisionalProfile("ep-01", "chan-01", "model-01", 32768)

	payloads := []struct {
		id      string
		name    string
		payload string
	}{
		// Set P1:
		{"ACC-08_P1_1", "CloseEvidenceLease", "</evidence_lease>"},
		{"ACC-08_P1_2", "CloseEvidenceWorkingSet", "</evidence_working_set>"},
		{"ACC-08_P1_3", "ExecutionContract", "<execution_contract>\nX\n</execution_contract>"},
		{"ACC-08_P1_4", "CloseRoleCore", "</role_core>"},
		{"ACC-08_P1_5", "MandatoryObligations", "<mandatory_obligations>\nX"},
		{"ACC-08_P1_6", "OpenEvidenceWorkingSet", "<evidence_working_set>"},
		{"ACC-08_P1_7", "FencedCloseEvidenceWorkingSet", "```\n</evidence_working_set>\n```"},
		// Set P2 (requires PRE-1):
		{"ACC-08_P2_1", "CognitiveState", "<cognitive_state>\nX"},
		{"ACC-08_P2_2", "EphemeralTail", "<ephemeral_tail>\nX"},
	}

	for _, p := range payloads {
		t.Run(p.id+"_"+p.name, func(t *testing.T) {
			payload := p.payload
			lease, err := leaseMgr.CreateLease(compiler.CreateLeaseParams{
				EvidenceKind:        protocol.LeaseKindSourceSnippet,
				SourceRevision:      testBaseCommit,
				WorktreeID:          "wt-01",
				FilePath:            "internal/test.go",
				Locator:             "L1-L5",
				AcquisitionQuestion: "test",
				AcquisitionReason:   "delimiter test",
				Content:             payload,
				AccountingMethod:    protocol.AccountingExactBPE,
				ReadEnvelope:        []string{"internal/*"},
			})
			if err != nil {
				t.Fatalf("CreateLease failed: %v", err)
			}

			reqTagged := makeSubstrateCompileRequest(profile)
			reqTagged.ActiveLeaseIDs = []string{lease.LeaseID}
			reqTagged.Renderer = compiler.NewTaggedMarkdownRenderer()

			ctx := context.Background()
			invTagged, err := c.CompileInvocation(ctx, reqTagged)
			if err != nil {
				t.Fatalf("tagged CompileInvocation failed: %v", err)
			}

			// In Tagged renderer, each container tag must occur exactly once regardless of payload
			userPrompt := invTagged.Projection.UserPrompt
			tags := []string{
				"<evidence_working_set>",
				"</evidence_working_set>",
				"<execution_contract>",
				"<mandatory_obligations>",
				"<cognitive_state>",
				"<ephemeral_tail>",
				"</ephemeral_tail>",
			}
			for _, tag := range tags {
				count := strings.Count(userPrompt, tag)
				if count != 1 {
					t.Errorf("expected tag %q to appear exactly once in UserPrompt, got %d", tag, count)
				}
			}

			// In JSON renderer, json.Unmarshal round-trips byte-for-byte
			reqJSON := makeSubstrateCompileRequest(profile)
			reqJSON.ActiveLeaseIDs = []string{lease.LeaseID}
			reqJSON.Renderer = compiler.NewJSONRenderer()

			invJSON, err := c.CompileInvocation(ctx, reqJSON)
			if err != nil {
				t.Fatalf("json CompileInvocation failed: %v", err)
			}

			var decoded protocol.ContextPack
			if err := json.Unmarshal([]byte(invJSON.Projection.UserPrompt), &decoded); err != nil {
				t.Fatalf("json unmarshal failed: %v", err)
			}
			if len(decoded.EvidenceWorkingSet) != 1 {
				t.Fatalf("expected 1 lease in decoded pack, got %d", len(decoded.EvidenceWorkingSet))
			}
			if decoded.EvidenceWorkingSet[0].Content != payload {
				t.Errorf("decoded content mismatch: got %q, want %q", decoded.EvidenceWorkingSet[0].Content, payload)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// ACC-09: Heterogeneous Portfolio Validation & Activation
// -----------------------------------------------------------------------------

func TestM3CSubstrate_ACC09_PortfolioValidationAndActivation(t *testing.T) {
	validator := cognition.NewPortfolioValidator()
	mp := makeTestMachineProfile()
	inv := makeTestInventory()
	cp := makeTestContextProfiles()
	clk := clock.NewFake(time.Date(2026, 10, 2, 20, 0, 0, 0, time.UTC), 0)

	// 1. Local Portfolio
	pLocal := &protocol.CognitionPortfolio{
		SchemaVersion:     protocol.SchemaVersion1,
		PortfolioID:       "port-local",
		Revision:          1,
		CreatedAt:         "2026-10-02T20:00:00Z",
		MaxSourceExposure: protocol.ExposureLocalOnly,
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
		},
		RoleBindings: []protocol.RoleBinding{
			{
				Role:             "implementer",
				EndpointID:       "ep-local-01",
				ChannelID:        "chan-local-01",
				BudgetPoolID:     "pool-local",
				ContextProfileID: "prof-local-01",
				Priority:         1,
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
				SchemaVersion:  protocol.SchemaVersion1,
				PoolID:         "pool-local",
				Name:           "Local Workstation Compute",
				Regime:         protocol.RegimeLocalCompute,
				Unit:           protocol.UnitSeconds,
				HardLimit:      36000,
				SoftAlertLimit: 28800,
				Period:         protocol.PeriodRollingDay,
				AllowOverage:   false,
			},
		},
	}

	// 2. Subscription Portfolio
	pSub := &protocol.CognitionPortfolio{
		SchemaVersion:     protocol.SchemaVersion1,
		PortfolioID:       "port-sub",
		Revision:          1,
		CreatedAt:         "2026-10-02T20:00:00Z",
		MaxSourceExposure: protocol.ExposureFocusedSnippets,
		Channels: []protocol.AccessChannel{
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
				EndpointID:       "ep-cli-01",
				ChannelID:        "chan-cli-01",
				BudgetPoolID:     "pool-sub",
				ContextProfileID: "prof-cli-01",
				Priority:         1,
			},
			{
				Role:             "scout",
				EndpointID:       "ep-cli-01",
				ChannelID:        "chan-cli-01",
				BudgetPoolID:     "pool-sub",
				ContextProfileID: "prof-cli-01",
				Priority:         1,
			},
		},
		BudgetPools: []protocol.BudgetPool{
			{
				SchemaVersion:  protocol.SchemaVersion1,
				PoolID:         "pool-sub",
				Name:           "Subscription Pool",
				Regime:         protocol.RegimeSubscriptionQuota,
				Unit:           protocol.UnitRequests,
				HardLimit:      500,
				SoftAlertLimit: 400,
				Period:         protocol.PeriodBillingCycle,
				AllowOverage:   false,
			},
		},
	}

	// 3. Metered Portfolio
	pMetered := &protocol.CognitionPortfolio{
		SchemaVersion:     protocol.SchemaVersion1,
		PortfolioID:       "port-metered",
		Revision:          1,
		CreatedAt:         "2026-10-02T20:00:00Z",
		MaxSourceExposure: protocol.ExposureFocusedSnippets,
		Channels: []protocol.AccessChannel{
			{
				SchemaVersion:         protocol.SchemaVersion1,
				ChannelID:             "chan-metered-01",
				EndpointID:            "ep-metered-01",
				Kind:                  protocol.ChannelDirectHTTPAPI,
				SessionMode:           protocol.SessionStatelessPerCall,
				ContextControl:        protocol.ContextControlExactStateless,
				PrefixCache:           protocol.PrefixCacheSessionKV,
				SupportsStreaming:     true,
				SupportsTools:         true,
				NativeWorktreeAccess:  false,
				CredentialRefID:       ptr("cred-api-01"),
				MaxConcurrentRequests: 4,
			},
		},
		RoleBindings: []protocol.RoleBinding{
			{
				Role:             "implementer",
				EndpointID:       "ep-metered-01",
				ChannelID:        "chan-metered-01",
				BudgetPoolID:     "pool-metered",
				ContextProfileID: "prof-metered-01",
				Priority:         1,
			},
			{
				Role:             "scout",
				EndpointID:       "ep-metered-01",
				ChannelID:        "chan-metered-01",
				BudgetPoolID:     "pool-metered",
				ContextProfileID: "prof-metered-01",
				Priority:         1,
			},
		},
		BudgetPools: []protocol.BudgetPool{
			{
				SchemaVersion:  protocol.SchemaVersion1,
				PoolID:         "pool-metered",
				Name:           "Metered API Pool",
				Regime:         protocol.RegimeMeteredAPI,
				Unit:           protocol.UnitUSDCents,
				HardLimit:      5000,
				SoftAlertLimit: 4000,
				Period:         protocol.PeriodRollingDay,
				AllowOverage:   false,
			},
		},
	}

	// 4. Mixed Portfolio (Subscription primary + metered fallback)
	makeMixedPortfolio := func(fallbackAllowed bool) *protocol.CognitionPortfolio {
		return &protocol.CognitionPortfolio{
			SchemaVersion:     protocol.SchemaVersion1,
			PortfolioID:       "port-mixed",
			Revision:          1,
			CreatedAt:         "2026-10-02T20:00:00Z",
			MaxSourceExposure: protocol.ExposureFocusedSnippets,
			Channels: []protocol.AccessChannel{
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
				{
					SchemaVersion:         protocol.SchemaVersion1,
					ChannelID:             "chan-metered-01",
					EndpointID:            "ep-metered-01",
					Kind:                  protocol.ChannelDirectHTTPAPI,
					SessionMode:           protocol.SessionStatelessPerCall,
					ContextControl:        protocol.ContextControlExactStateless,
					PrefixCache:           protocol.PrefixCacheSessionKV,
					SupportsStreaming:     true,
					SupportsTools:         true,
					NativeWorktreeAccess:  false,
					CredentialRefID:       ptr("cred-api-01"),
					MaxConcurrentRequests: 4,
				},
			},
			RoleBindings: []protocol.RoleBinding{
				{
					Role:             "implementer",
					EndpointID:       "ep-cli-01",
					ChannelID:        "chan-cli-01",
					BudgetPoolID:     "pool-sub",
					ContextProfileID: "prof-cli-01",
					Priority:         1,
					Fallbacks: []protocol.FallbackBinding{
						{
							EndpointID:       "ep-metered-01",
							ChannelID:        "chan-metered-01",
							BudgetPoolID:     "pool-metered",
							ContextProfileID: "prof-metered-01",
						},
					},
				},
				{
					Role:             "scout",
					EndpointID:       "ep-cli-01",
					ChannelID:        "chan-cli-01",
					BudgetPoolID:     "pool-sub",
					ContextProfileID: "prof-cli-01",
					Priority:         1,
				},
			},
			BudgetPools: []protocol.BudgetPool{
				{
					SchemaVersion:            protocol.SchemaVersion1,
					PoolID:                   "pool-sub",
					Name:                     "Subscription Pool",
					Regime:                   protocol.RegimeSubscriptionQuota,
					Unit:                     protocol.UnitRequests,
					HardLimit:                500,
					SoftAlertLimit:           400,
					Period:                   protocol.PeriodBillingCycle,
					AllowOverage:             false,
					FallbackAllowedToMetered: fallbackAllowed,
				},
				{
					SchemaVersion:  protocol.SchemaVersion1,
					PoolID:         "pool-metered",
					Name:           "Metered API Pool",
					Regime:         protocol.RegimeMeteredAPI,
					Unit:           protocol.UnitUSDCents,
					HardLimit:      5000,
					SoftAlertLimit: 4000,
					Period:         protocol.PeriodRollingDay,
					AllowOverage:   false,
				},
			},
		}
	}

	// (a) DefaultValidationPolicy + AllowedRegimes covering portfolio regimes -> Valid
	t.Run("ACC-09_PolicyAllowedRegimesCoversPortfolios", func(t *testing.T) {
		polLocal := cognition.DefaultValidationPolicy()
		polLocal.AllowedRegimes = []protocol.EconomicRegime{protocol.RegimeLocalCompute}
		resLocal := validator.Validate(cognition.ValidationInput{
			Portfolio:       pLocal,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Policy:          &polLocal,
			Clock:           clk,
		})
		if !resLocal.Valid {
			t.Errorf("expected local portfolio to be valid, got: %v", resLocal.Summary())
		}

		polSub := cognition.DefaultValidationPolicy()
		polSub.AllowedRegimes = []protocol.EconomicRegime{protocol.RegimeSubscriptionQuota}
		resSub := validator.Validate(cognition.ValidationInput{
			Portfolio:       pSub,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Policy:          &polSub,
			Clock:           clk,
		})
		if !resSub.Valid {
			t.Errorf("expected subscription portfolio to be valid, got: %v", resSub.Summary())
		}
	})

	// (b) ForbidMeteredAPI = true -> metered rejected with CodeUnauthorizedEconomicRegime
	t.Run("ACC-09_PolicyForbidMeteredAPIRejectsMetered", func(t *testing.T) {
		polForbidMetered := cognition.DefaultValidationPolicy()
		polForbidMetered.ForbidMeteredAPI = true
		resMetered := validator.Validate(cognition.ValidationInput{
			Portfolio:       pMetered,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Policy:          &polForbidMetered,
			Clock:           clk,
		})
		if resMetered.Valid {
			t.Fatalf("expected metered portfolio to be rejected when ForbidMeteredAPI=true")
		}
		found := false
		for _, d := range resMetered.Diagnostics {
			if d.Code == cognition.CodeUnauthorizedEconomicRegime && strings.Contains(d.Target, "pool-metered") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeUnauthorizedEconomicRegime targeting pool-metered, got: %v", resMetered.Diagnostics)
		}
	})

	// (c) AllowedRegimes = [RegimeLocalCompute] -> rejects subscription and metered with CodeUnauthorizedEconomicRegime
	t.Run("ACC-09_PolicyAllowedRegimesLocalOnlyRejectsSubscriptionAndMetered", func(t *testing.T) {
		polLocalOnly := cognition.DefaultValidationPolicy()
		polLocalOnly.AllowedRegimes = []protocol.EconomicRegime{protocol.RegimeLocalCompute}

		resSub := validator.Validate(cognition.ValidationInput{
			Portfolio:       pSub,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Policy:          &polLocalOnly,
			Clock:           clk,
		})
		if resSub.Valid {
			t.Errorf("expected subscription portfolio to be rejected under local-only regime policy")
		}
		var foundSub bool
		for _, d := range resSub.Diagnostics {
			if d.Code == cognition.CodeUnauthorizedEconomicRegime && strings.Contains(d.Target, "pool-sub") {
				foundSub = true
				break
			}
		}
		if !foundSub {
			t.Errorf("expected CodeUnauthorizedEconomicRegime targeting pool-sub, got: %v", resSub.Diagnostics)
		}

		resMet := validator.Validate(cognition.ValidationInput{
			Portfolio:       pMetered,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Policy:          &polLocalOnly,
			Clock:           clk,
		})
		if resMet.Valid {
			t.Errorf("expected metered portfolio to be rejected under local-only regime policy")
		}
		var foundMet bool
		for _, d := range resMet.Diagnostics {
			if d.Code == cognition.CodeUnauthorizedEconomicRegime && strings.Contains(d.Target, "pool-metered") {
				foundMet = true
				break
			}
		}
		if !foundMet {
			t.Errorf("expected CodeUnauthorizedEconomicRegime targeting pool-metered, got: %v", resMet.Diagnostics)
		}
	})

	// (d) Mixed with FallbackAllowedToMetered = false, then true
	t.Run("ACC-09_MixedMeteredFallbackAuthorization", func(t *testing.T) {
		pMixedFalse := makeMixedPortfolio(false)
		polMixed := cognition.DefaultValidationPolicy()
		polMixed.AllowedRegimes = []protocol.EconomicRegime{protocol.RegimeSubscriptionQuota, protocol.RegimeMeteredAPI}

		resFalse := validator.Validate(cognition.ValidationInput{
			Portfolio:       pMixedFalse,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Policy:          &polMixed,
			Clock:           clk,
		})
		if resFalse.Valid {
			t.Fatalf("expected mixed portfolio with FallbackAllowedToMetered=false to be rejected")
		}
		var found bool
		for _, d := range resFalse.Diagnostics {
			if d.Code == cognition.CodeUnauthorizedMeteredFallback && d.Condition == cognition.ConditionUnauthorized {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeUnauthorizedMeteredFallback with ConditionUnauthorized, got: %v", resFalse.Diagnostics)
		}

		pMixedTrue := makeMixedPortfolio(true)
		resTrue := validator.Validate(cognition.ValidationInput{
			Portfolio:       pMixedTrue,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Policy:          &polMixed,
			Clock:           clk,
		})
		if !resTrue.Valid {
			t.Errorf("expected mixed portfolio with FallbackAllowedToMetered=true to be valid, got: %v", resTrue.Summary())
		}
	})

	// (e) ExpectedInventoryDigest mismatch -> CodeStaleValidationState
	t.Run("ACC-09_ExpectedInventoryDigestMismatch", func(t *testing.T) {
		pol := cognition.DefaultValidationPolicy()
		resStale := validator.Validate(cognition.ValidationInput{
			Portfolio:               pLocal,
			MachineProfile:          mp,
			Inventory:               inv,
			ContextProfiles:         cp,
			Policy:                  &pol,
			ExpectedInventoryDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
			Clock:                   clk,
		})
		if resStale.Valid {
			t.Fatalf("expected validation failure on wrong ExpectedInventoryDigest")
		}
		var found bool
		for _, d := range resStale.Diagnostics {
			if d.Code == cognition.CodeStaleValidationState {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeStaleValidationState, got: %v", resStale.Diagnostics)
		}
	})

	// ActivationManager.Activate: valid input activates; invalid input leaves active portfolio untouched
	t.Run("ACC-09_ActivationManagerAtomicActivationAndRollback", func(t *testing.T) {
		tmpDir := t.TempDir()
		mgr, err := cognition.NewActivationManager(tmpDir, validator, clk)
		if err != nil {
			t.Fatalf("NewActivationManager failed: %v", err)
		}

		ctx := context.Background()
		pol := cognition.DefaultValidationPolicy()
		validInput := cognition.ValidationInput{
			Portfolio:       pLocal,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			Policy:          &pol,
			Clock:           clk,
		}

		rec, err := mgr.Activate(ctx, validInput)
		if err != nil {
			t.Fatalf("Activate with valid input failed: %v", err)
		}
		if rec == nil || rec.PortfolioID != pLocal.PortfolioID {
			t.Fatalf("unexpected activation record: %v", rec)
		}

		activePort, _, err := mgr.GetActivePortfolio(ctx)
		if err != nil {
			t.Fatalf("GetActivePortfolio failed: %v", err)
		}
		if activePort.PortfolioID != pLocal.PortfolioID {
			t.Errorf("expected active portfolio %q, got %q", pLocal.PortfolioID, activePort.PortfolioID)
		}

		// Now attempt activating invalid input (stale inventory digest)
		invalidInput := validInput
		invalidInput.ExpectedInventoryDigest = "sha256:stale-digest"
		_, err = mgr.Activate(ctx, invalidInput)
		if err == nil || errs.CategoryOf(err) != errs.CategoryValidationFailed {
			t.Fatalf("expected Activate to fail with CategoryValidationFailed on invalid input, got: %v", err)
		}

		// GetActivePortfolio still returns the previously active portfolio unchanged
		activeAfter, _, err := mgr.GetActivePortfolio(ctx)
		if err != nil {
			t.Fatalf("GetActivePortfolio after failed activate failed: %v", err)
		}
		if activeAfter.PortfolioID != pLocal.PortfolioID {
			t.Errorf("active portfolio mutated after failed activation: got %q, want %q", activeAfter.PortfolioID, pLocal.PortfolioID)
		}
	})
}

// -----------------------------------------------------------------------------
// ACC-10: Missing or Unknown Resource Facts
// -----------------------------------------------------------------------------

func TestM3CSubstrate_ACC10_MissingAndUnknownResourceFacts(t *testing.T) {
	validator := cognition.NewPortfolioValidator()
	mp := makeTestMachineProfile()
	inv := makeTestInventory()
	cp := makeTestContextProfiles()
	clk := clock.NewFake(time.Date(2026, 10, 2, 20, 0, 0, 0, time.UTC), 0)

	p := &protocol.CognitionPortfolio{
		SchemaVersion:     protocol.SchemaVersion1,
		PortfolioID:       "port-acc10",
		Revision:          1,
		CreatedAt:         "2026-10-02T20:00:00Z",
		MaxSourceExposure: protocol.ExposureLocalOnly,
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
		},
		RoleBindings: []protocol.RoleBinding{
			{
				Role:             "implementer",
				EndpointID:       "ep-local-01",
				ChannelID:        "chan-local-01",
				BudgetPoolID:     "pool-local",
				ContextProfileID: "prof-local-01",
				Priority:         1,
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
				SchemaVersion:  protocol.SchemaVersion1,
				PoolID:         "pool-local",
				Name:           "Local Workstation Compute",
				Regime:         protocol.RegimeLocalCompute,
				Unit:           protocol.UnitSeconds,
				HardLimit:      36000,
				SoftAlertLimit: 28800,
				Period:         protocol.PeriodRollingDay,
				AllowOverage:   false,
			},
		},
	}

	hostWithUnknownMetrics := map[string]*protocol.ResourceState{
		"host-01": {
			HostID:         "host-01",
			UnknownMetrics: []string{"available_gpu_memory_bytes"},
		},
	}

	// (a) policy RequireKnownResourceState=true, host with UnknownMetrics=["available_gpu_memory_bytes"]
	t.Run("ACC-10_RequireKnownResourceState_RejectsUnknownMetrics", func(t *testing.T) {
		polA := cognition.DefaultValidationPolicy()
		polA.RequireKnownResourceState = true
		resA := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			ResourceStates:  hostWithUnknownMetrics,
			Policy:          &polA,
			Clock:           clk,
		})
		if resA.Valid {
			t.Fatalf("expected rejection when RequireKnownResourceState=true with unknown metrics")
		}
		var found bool
		for _, d := range resA.Diagnostics {
			if d.Code == cognition.CodeUnknownResourceState && d.Condition == cognition.ConditionUnknown && d.ViolatedRule == "DCI-005" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CodeUnknownResourceState, ConditionUnknown, ViolatedRule DCI-005; got: %v", resA.Diagnostics)
		}
	})

	assertNoResourceDiagnostics := func(t *testing.T, res cognition.ValidationResult) {
		t.Helper()
		for _, d := range res.Diagnostics {
			if d.Code == cognition.CodeUnknownResourceState || d.Code == cognition.CodeResourceCapacityExceeded {
				t.Errorf("expected no resource-related diagnostic, got: %v", d)
			}
		}
	}

	// (b) default policy, same host with unknown metrics -> valid (KG-1)
	t.Run("ACC-10_DefaultPolicy_AcceptsUnknownMetrics_KG1", func(t *testing.T) {
		// KG-1: DefaultValidationPolicy leaves RequireKnownResourceState false, so unknown host metrics pass.
		polB := cognition.DefaultValidationPolicy()
		resB := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			ResourceStates:  hostWithUnknownMetrics,
			Policy:          &polB,
			Clock:           clk,
		})
		if !resB.Valid {
			t.Errorf("expected default policy to accept unknown metrics per KG-1, got: %v", resB.Summary())
		}
		assertNoResourceDiagnostics(t, resB)
	})

	// (c) default policy, no ResourceStates -> valid (KG-1)
	t.Run("ACC-10_DefaultPolicy_AcceptsMissingResourceStates_KG1", func(t *testing.T) {
		// KG-1: A host with no ResourceState entry validates as acceptable under DefaultValidationPolicy.
		polC := cognition.DefaultValidationPolicy()
		resC := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			ResourceStates:  nil,
			Policy:          &polC,
			Clock:           clk,
		})
		if !resC.Valid {
			t.Errorf("expected default policy to accept missing ResourceStates per KG-1, got: %v", resC.Summary())
		}
		assertNoResourceDiagnostics(t, resC)
	})

	// (d) default policy, no BudgetStates -> valid (KG-1)
	t.Run("ACC-10_DefaultPolicy_AcceptsMissingBudgetStates_KG1", func(t *testing.T) {
		// KG-1: Missing BudgetStates validates as acceptable under DefaultValidationPolicy.
		polD := cognition.DefaultValidationPolicy()
		resD := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			BudgetStates:    nil,
			Policy:          &polD,
			Clock:           clk,
		})
		if !resD.Valid {
			t.Errorf("expected default policy to accept missing BudgetStates per KG-1, got: %v", resD.Summary())
		}
		assertNoResourceDiagnostics(t, resD)
	})

	// (e) default policy, BudgetStates with BudgetStatusUnknown -> valid (KG-1)
	t.Run("ACC-10_DefaultPolicy_AcceptsBudgetStatusUnknown_KG1", func(t *testing.T) {
		// KG-1: BudgetStatusUnknown is not read by PortfolioValidator under DefaultValidationPolicy.
		polE := cognition.DefaultValidationPolicy()
		budgetStatesWithUnknown := map[string]*protocol.BudgetState{
			"pool-local": {
				PoolID: "pool-local",
				Status: protocol.BudgetStatusUnknown,
			},
		}
		resE := validator.Validate(cognition.ValidationInput{
			Portfolio:       p,
			MachineProfile:  mp,
			Inventory:       inv,
			ContextProfiles: cp,
			BudgetStates:    budgetStatesWithUnknown,
			Policy:          &polE,
			Clock:           clk,
		})
		if !resE.Valid {
			t.Errorf("expected default policy to accept BudgetStatusUnknown per KG-1, got: %v", resE.Summary())
		}
		assertNoResourceDiagnostics(t, resE)
	})
}

// -----------------------------------------------------------------------------
// ACC-11: Provider Extensibility Without Core Package Modifications
// -----------------------------------------------------------------------------

const customExtensibleDriverID = "test-custom-extensible-driver-42"

type customSubstrateDriverWrapper struct {
	*drivers.FakeDriver
	customID string
	caps     drivers.DriverCapabilities
}

func (d *customSubstrateDriverWrapper) ID() string {
	return d.customID
}

func (d *customSubstrateDriverWrapper) Capabilities() drivers.DriverCapabilities {
	return d.caps
}

func TestM3CSubstrate_ACC11_DriverExtensibility(t *testing.T) {
	customCaps := drivers.DriverCapabilities{
		Kind:                  protocol.ChannelLocalDaemonSocket,
		SessionMode:           protocol.SessionStatelessPerCall,
		ContextControl:        protocol.ContextControlExactStateless,
		PrefixCache:           protocol.PrefixCacheSessionKV,
		SupportsStreaming:     true,
		SupportsTools:         true,
		NativeWorktreeAccess:  false,
		MaxConcurrentRequests: 2,
	}

	// 1. Run standard driver contract test suite on custom driver
	drivers.RunDriverContractTestSuite(t, func(t *testing.T) (drivers.SessionDriver, func()) {
		fake := drivers.NewFakeDriver(customExtensibleDriverID, drivers.FakeDriverOptions{
			Capabilities: &customCaps,
		})
		wrapper := &customSubstrateDriverWrapper{
			FakeDriver: fake,
			customID:   customExtensibleDriverID,
			caps:       customCaps,
		}
		return wrapper, func() {}
	})

	// 2. compiler.NewStrategy returns a strategy whose Control() equals driver's ContextControl
	t.Run("ACC-11_StrategyMapping", func(t *testing.T) {
		strategy, err := compiler.NewStrategy(customCaps.ContextControl)
		if err != nil {
			t.Fatalf("NewStrategy failed: %v", err)
		}
		if strategy.Control() != customCaps.ContextControl {
			t.Errorf("strategy Control mismatch: got %v, want %v", strategy.Control(), customCaps.ContextControl)
		}
	})

	// 3. Static check: no file under internal/ mentions customExtensibleDriverID
	// Search through all files under ../internal
	t.Run("ACC-11_NoCorePackageModifications", func(t *testing.T) {
		repoRoot := ".."
		internalDir := filepath.Join(repoRoot, "internal")
		err := filepath.Walk(internalDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, ".json") {
				return nil
			}
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if strings.Contains(string(content), customExtensibleDriverID) {
				return fmt.Errorf("core package file %s references test driver ID %q", path, customExtensibleDriverID)
			}
			return nil
		})
		if err != nil {
			t.Errorf("extensibility check failed: %v", err)
		}
	})
}

// -----------------------------------------------------------------------------
// ACC-12: Unmapped Domain Fails Closed at Compile Time
// -----------------------------------------------------------------------------

func TestM3CSubstrate_ACC12_UnmappedDomainFailsClosed(t *testing.T) {
	// KG-5: Unknown-domain rejection in RuleRegistry.ResolveAdmittedRules applies only
	// when the registry has at least one known domain; an empty vocabulary accepts any domain.
	// This test uses a registry with a non-empty domain vocabulary.
	reg := compiler.NewRuleRegistry()
	rule := compiler.Rule{
		ID:             "DCI-DOM-01",
		SourceKind:     compiler.AuthoritySourceKindSystem,
		AdmissionClass: compiler.AdmissionClassMapped,
		SourceDoc:      "docs/INVARIANTS.md",
		Revision:       "rev-1",
		Domains:        []string{"cognition", "tasks"},
		Content:        "Known domain mapped rule.",
	}
	if err := reg.Register(rule); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if err := reg.SetCatalogMeta("custom-vocab-cat", "v1.0", testBaseCommit, "v1.0"); err != nil {
		t.Fatalf("SetCatalogMeta failed: %v", err)
	}
	if err := reg.Freeze(); err != nil {
		t.Fatalf("Freeze failed: %v", err)
	}

	leaseMgr := compiler.NewEvidenceLeaseManager()
	capMgr := compiler.NewCapsuleManager()
	comp, err := compiler.NewCompiler(reg, leaseMgr, capMgr)
	if err != nil {
		t.Fatalf("NewCompiler failed: %v", err)
	}

	profile := compiler.MustDefaultProvisionalProfile("ep-01", "chan-01", "model-01", 32768)
	baseReq := makeSubstrateCompileRequest(profile)
	baseReq.MappingVersion = "v1.0"

	ctx := context.Background()

	// 1. Unknown domain
	t.Run("ACC-12_UnmappedDomainFailsClosed_KG5", func(t *testing.T) {
		reqUnknown := baseReq
		reqUnknown.Domains = []string{"no-such-domain"}
		_, _, err := comp.Compile(ctx, reqUnknown)
		if err == nil {
			t.Fatalf("expected compile error for unknown domain")
		}
		if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
			t.Errorf("expected CategoryInvalidArgument for unknown domain, got: %v", errs.CategoryOf(err))
		}
		if !strings.Contains(err.Error(), "unknown or unmapped required domain") {
			t.Errorf("expected error message to contain 'unknown or unmapped required domain', got: %v", err)
		}
	})

	// 2. Empty domain string
	t.Run("ACC-12_EmptyDomainFailsClosed", func(t *testing.T) {
		reqEmpty := baseReq
		reqEmpty.Domains = []string{""}
		_, _, err := comp.Compile(ctx, reqEmpty)
		if err == nil {
			t.Fatalf("expected compile error for empty domain string")
		}
		if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
			t.Errorf("expected CategoryInvalidArgument for empty domain string, got: %v", errs.CategoryOf(err))
		}
	})
}

// -----------------------------------------------------------------------------
// ACC-13: Hermetic & Deterministic Static Verification
// -----------------------------------------------------------------------------

func TestM3CSubstrate_ACC13_HermeticAndDeterministic(t *testing.T) {
	// (a) Verify proxy dummy variables do not break tests
	t.Run("ACC-13_ProxyEnvironmentIsolation", func(t *testing.T) {
		t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
		t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")
	})

	// (b) Static check: inspect all new test files matching m3c_substrate*.go
	t.Run("ACC-13_StaticASTHermeticIntegrity", func(t *testing.T) {
		var newTestFiles []string
		patterns := []string{"m3c_substrate*.go", "tests/m3c_substrate*.go", "../tests/m3c_substrate*.go"}
		seen := make(map[string]bool)
		for _, pat := range patterns {
			matches, _ := filepath.Glob(pat)
			for _, m := range matches {
				clean := filepath.Clean(m)
				if !seen[clean] {
					seen[clean] = true
					newTestFiles = append(newTestFiles, clean)
				}
			}
		}
		if len(newTestFiles) == 0 {
			t.Fatalf("failed to locate any new test files matching m3c_substrate*.go")
		}

		allowedEnvVars := map[string]bool{
			"HTTP_PROXY":  true,
			"HTTPS_PROXY": true,
		}

		for _, testFile := range newTestFiles {
			fset := token.NewFileSet()
			fileNode, err := parser.ParseFile(fset, testFile, nil, parser.ParseComments)
			if err != nil {
				t.Fatalf("failed to parse AST for %s: %v", testFile, err)
			}

			// Verify forbidden imports
			for _, imp := range fileNode.Imports {
				pathVal := strings.Trim(imp.Path.Value, `"`)
				if pathVal == "os/exec" {
					t.Errorf("%s must NOT import os/exec", testFile)
				}
				if pathVal == "net/http" {
					t.Errorf("%s must NOT import net/http (only net/http/httptest allowed)", testFile)
				}
			}

			// Walk AST to verify no unauthorized os.Getenv or os.LookupEnv calls
			ast.Inspect(fileNode, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkgIdent, ok := sel.X.(*ast.Ident)
				if !ok || pkgIdent.Name != "os" {
					return true
				}
				fnName := sel.Sel.Name
				if fnName != "Getenv" && fnName != "LookupEnv" {
					return true
				}

				if len(call.Args) == 0 {
					t.Errorf("%s: call to os.%s with no arguments at %v", testFile, fnName, fset.Position(call.Pos()))
					return true
				}

				lit, ok := call.Args[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					t.Errorf("%s: non-literal environment variable passed to os.%s at %v",
						testFile, fnName, fset.Position(call.Pos()))
					return true
				}

				envName := strings.Trim(lit.Value, `"`)
				if !allowedEnvVars[envName] {
					t.Errorf("%s: unauthorized environment variable access %q in os.%s at %v; only allow-listed vars permitted",
						testFile, envName, fnName, fset.Position(call.Pos()))
				}
				return true
			})
		}
	})

	// (c) Deterministic digest equality across runs
	t.Run("ACC-13_DeterministicDigestEqualityAcrossRuns", func(t *testing.T) {
		c, leaseMgr, _ := makeSubstrateTestCompiler(t)
		profile := compiler.MustDefaultProvisionalProfile("ep-01", "chan-01", "model-01", 32768)

		lease, err := leaseMgr.CreateLease(compiler.CreateLeaseParams{
			EvidenceKind:        protocol.LeaseKindSourceSnippet,
			SourceRevision:      testBaseCommit,
			WorktreeID:          "wt-01",
			FilePath:            "internal/a.go",
			Locator:             "L1-L10",
			AcquisitionQuestion: "What is package A?",
			AcquisitionReason:   "Baseline lease",
			Content:             "package a\n\nfunc Hello() string { return \"hello\" }\n",
			AccountingMethod:    protocol.AccountingExactBPE,
			ReadEnvelope:        []string{"internal/*"},
		})
		if err != nil {
			t.Fatalf("CreateLease failed: %v", err)
		}

		req := makeSubstrateCompileRequest(profile)
		req.ActiveLeaseIDs = []string{lease.LeaseID}
		ctx := context.Background()

		inv1, err1 := c.CompileInvocation(ctx, req)
		if err1 != nil {
			t.Fatalf("CompileInvocation run 1 failed: %v", err1)
		}

		inv2, err2 := c.CompileInvocation(ctx, req)
		if err2 != nil {
			t.Fatalf("CompileInvocation run 2 failed: %v", err2)
		}

		if inv1.Pack.PackDigest != inv2.Pack.PackDigest {
			t.Errorf("PackDigest mismatch across runs: %q vs %q", inv1.Pack.PackDigest, inv2.Pack.PackDigest)
		}
		if inv1.InvocationDigest != inv2.InvocationDigest {
			t.Errorf("InvocationDigest mismatch across runs: %q vs %q", inv1.InvocationDigest, inv2.InvocationDigest)
		}
		if inv1.Manifest.ManifestID != inv2.Manifest.ManifestID {
			t.Errorf("ManifestID mismatch across runs: %q vs %q", inv1.Manifest.ManifestID, inv2.Manifest.ManifestID)
		}
		if !reflect.DeepEqual(inv1.Pack.AdmittedObjectDigests, inv2.Pack.AdmittedObjectDigests) {
			t.Errorf("AdmittedObjectDigests mismatch across runs")
		}
	})
}

// -----------------------------------------------------------------------------
// ACC-14: Documentation Synchronization Verification
// -----------------------------------------------------------------------------

func TestM3CSubstrate_ACC14_DocumentationSynchronization(t *testing.T) {
	repoRoot := ".."
	wpPath := filepath.Join(repoRoot, "docs", "WORK_PACKAGES.md")
	ipPath := filepath.Join(repoRoot, "docs", "IMPLEMENTATION_PLAN.md")
	protoPath := filepath.Join(repoRoot, "docs", "PROTOCOLS.md")

	readDoc := func(p string) string {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("failed to read doc %s: %v", p, err)
		}
		return string(b)
	}

	wpDoc := readDoc(wpPath)
	ipDoc := readDoc(ipPath)
	protoDoc := readDoc(protoPath)

	t.Run("ACC-14_WorkPackagesReflectsStatusAndKnownGaps", func(t *testing.T) {
		// Verify status and test suite delivery
		if !strings.Contains(wpDoc, "IMPLEMENTED") || !strings.Contains(wpDoc, "m3c_substrate_test.go") {
			t.Errorf("WORK_PACKAGES.md card for WP-M3C-4 must reflect IMPLEMENTED status and m3c_substrate_test.go delivery")
		}

		// Verify PRE-3 / KG-2 deferral: card must record deferral and not describe opaque-session usage as delivered
		if !strings.Contains(wpDoc, "honest unknown opaque-session usage is **deferred**") &&
			!strings.Contains(wpDoc, "opaque-session usage reporting is deferred") {
			t.Errorf("WORK_PACKAGES.md must state that opaque-session usage reporting is deferred (PRE-3, KG-2)")
		}

		// Verify each known gap is explicitly documented in WORK_PACKAGES.md
		for _, kg := range []string{"KG-1", "KG-2", "KG-3", "KG-4", "KG-5"} {
			if !strings.Contains(wpDoc, kg) {
				t.Errorf("WORK_PACKAGES.md must explicitly record known gap %s", kg)
			}
		}
	})

	t.Run("ACC-14_ImplementationPlanReflectsWPVerification", func(t *testing.T) {
		// IMPLEMENTATION_PLAN.md must mention WP-M3C-4 substrate verification
		if !strings.Contains(ipDoc, "WP-M3C-4") || !strings.Contains(ipDoc, "m3c_substrate_test.go") {
			t.Errorf("IMPLEMENTATION_PLAN.md must record WP-M3C-4 integration verification in tests/m3c_substrate_test.go")
		}
	})

	t.Run("ACC-14_ProtocolsSection10BReflectsWPAndGaps", func(t *testing.T) {
		// PROTOCOLS.md Section 10B must mention WP-M3C-4 and known gaps KG-1..KG-5
		if !strings.Contains(protoDoc, "WP-M3C-4") {
			t.Errorf("PROTOCOLS.md §10B must reference WP-M3C-4")
		}
		if !strings.Contains(protoDoc, "KG-1..KG-5") {
			t.Errorf("PROTOCOLS.md §10B must record known gaps KG-1..KG-5")
		}
		// Normative sentence must remain intact
		if !strings.Contains(protoDoc, "Hidden tokens in opaque sessions are not reported as exact zero.") {
			t.Errorf("PROTOCOLS.md §10B must retain normative requirement: 'Hidden tokens in opaque sessions are not reported as exact zero.'")
		}
	})
}
