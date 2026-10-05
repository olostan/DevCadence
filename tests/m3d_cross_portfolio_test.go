package tests

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/cognition/planner"
	"github.com/olostan/DevCadence/internal/cognition/plannerdriver"
	"github.com/olostan/DevCadence/internal/cognition/workflowplanner"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/schema"
)

// --- Environment and Matrix Test Helpers ---

type testEnvConfig struct {
	name            string
	inventory       *protocol.ResourceInventory
	machineProfile  *protocol.MachineCapabilityProfile
	contextProfiles map[string]*protocol.ContextProfile
	budgetStates    map[string]*protocol.BudgetState
	validationPol   cognition.ValidationPolicy
}

type m3dDriverResolver struct {
	drivers map[string]drivers.SessionDriver
	models  map[string]string
	errs    map[string]error
}

func (r *m3dDriverResolver) ResolveDriver(_ context.Context, endpointID string) (drivers.SessionDriver, string, error) {
	if err, ok := r.errs[endpointID]; ok && err != nil {
		return nil, "", err
	}
	return r.drivers[endpointID], r.models[endpointID], nil
}

type m3dMockDriver struct {
	id      string
	caps    drivers.DriverCapabilities
	handler func(ctx context.Context, input drivers.TurnInput) (drivers.TurnResult, error)
}

func (d *m3dMockDriver) ID() string                               { return d.id }
func (d *m3dMockDriver) Capabilities() drivers.DriverCapabilities { return d.caps }
func (d *m3dMockDriver) StartSession(ctx context.Context, cfg drivers.SessionConfig) (drivers.Session, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &m3dMockSession{driver: d, cfg: cfg}, nil
}
func (d *m3dMockDriver) ResumeSession(ctx context.Context, sessionID string, cfg drivers.SessionConfig) (drivers.Session, error) {
	return &m3dMockSession{driver: d, cfg: cfg}, nil
}

type m3dMockSession struct {
	driver *m3dMockDriver
	cfg    drivers.SessionConfig
}

func (s *m3dMockSession) ID() string                      { return s.cfg.SessionID }
func (s *m3dMockSession) DriverID() string                { return s.driver.id }
func (s *m3dMockSession) Config() drivers.SessionConfig   { return s.cfg }
func (s *m3dMockSession) Status() drivers.SessionStatus   { return drivers.SessionStatusActive }
func (s *m3dMockSession) Close(ctx context.Context) error { return nil }
func (s *m3dMockSession) ExecuteTurn(ctx context.Context, input drivers.TurnInput) (drivers.TurnResult, error) {
	if s.driver.handler != nil {
		return s.driver.handler(ctx, input)
	}
	return drivers.TurnResult{TurnID: input.TurnID, Content: "{}"}, nil
}
func (s *m3dMockSession) StreamTurn(ctx context.Context, input drivers.TurnInput) (drivers.EventStream, error) {
	return nil, errs.New(errs.CategoryUnsupported, "streaming not implemented in test mock")
}

type m3dWorkflowMockInvoker struct {
	invokeFn func(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error)
}

func (m *m3dWorkflowMockInvoker) Invoke(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error) {
	if m.invokeFn != nil {
		return m.invokeFn(ctx, inv)
	}
	return planner.InvocationResult{Content: "{}"}, nil
}

func buildPlannerAltJSON(intent protocol.RecommendationIntent, p *protocol.CognitionPortfolio) map[string]any {
	raw, _ := json.Marshal(p)
	var portfolioMap map[string]any
	_ = json.Unmarshal(raw, &portfolioMap)
	return map[string]any{
		"intent":     string(intent),
		"portfolio":  portfolioMap,
		"rationale":  "Tailored recommendation for " + string(intent),
		"tradeoffs":  []string{"Tradeoff for " + string(intent)},
		"confidence": "high",
	}
}

func buildPlannerOutputJSON(intents []protocol.RecommendationIntent, portfolioProvider func(intent protocol.RecommendationIntent) *protocol.CognitionPortfolio) string {
	alts := make([]map[string]any, 0, len(intents))
	for _, i := range intents {
		alts = append(alts, buildPlannerAltJSON(i, portfolioProvider(i)))
	}
	b, _ := json.Marshal(map[string]any{"alternatives": alts})
	return string(b)
}

// 1. Apple Silicon MLX local-only environment
func createAppleMLXEnv() testEnvConfig {
	epID := "ep-mlx-01"
	chanID := "chan-mlx-01"
	profID := "prof-mlx-01"
	poolID := "pool-local-mlx"

	inventory := &protocol.ResourceInventory{
		SchemaVersion:      protocol.SchemaVersion1,
		InventoryID:        "inv-mlx-01",
		MachineFingerprint: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ObservedAt:         protocol.Timestamp(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)),
		Hardware: protocol.HardwareSummary{
			OSFamily:            protocol.OSDarwin,
			Arch:                "arm64",
			LogicalCores:        12,
			TotalMemoryBytes:    ptr[int64](36 * 1024 * 1024 * 1024),
			AcceleratorBackends: []protocol.BackendKind{protocol.BackendMetal},
		},
		Readiness: []protocol.ScopeReadiness{
			{Scope: protocol.ScopeCanRunLocalInference, Status: protocol.ScopeStatusReady, Reason: "Apple Metal GPU verified"},
		},
		Policy: &protocol.PolicySummary{
			MaxSourceExposure: protocol.ExposureLocalOnly,
			MaxCostClass:      protocol.CostLocalCompute,
		},
		CognitionEndpoints: []protocol.CognitionEndpointSummary{
			{
				ID:                     epID,
				Kind:                   protocol.EndpointLocalRuntime,
				Locality:               protocol.LocalityLocal,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthNotApplicable,
				CostClass:              protocol.CostLocalCompute,
				RequiredSourceExposure: protocol.ExposureLocalOnly,
				AccelerationVerified:   true,
				AccelerationBackend:    ptr(protocol.BackendMetal),
			},
		},
	}

	machineProfile := &protocol.MachineCapabilityProfile{
		SchemaVersion:      protocol.SchemaVersion1,
		ProfileID:          "mcp-mlx-01",
		MachineFingerprint: inventory.MachineFingerprint,
		ObservedAt:         inventory.ObservedAt,
		KnowledgeRevision:  "rev-mlx-1",
		ProbeDepth:         protocol.DepthInference,
		Endpoints: []protocol.CognitionEndpoint{
			{
				ID:                     epID,
				Kind:                   protocol.EndpointLocalRuntime,
				Locality:               protocol.LocalityLocal,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthNotApplicable,
				CostClass:              protocol.CostLocalCompute,
				RequiredSourceExposure: protocol.ExposureLocalOnly,
				StructuredOutput:       protocol.FeatureProbePassed,
				ToolUse:                protocol.FeatureProbePassed,
				Capabilities: []protocol.GradedCapability{
					{Dimension: protocol.CapabilityImplementation, Grade: protocol.GradeStrong, Provenance: protocol.ProvenanceMeasured},
					{Dimension: protocol.CapabilityReview, Grade: protocol.GradeStrong, Provenance: protocol.ProvenanceMeasured},
				},
				Acceleration: &protocol.AccelerationEvidence{Backend: protocol.BackendMetal, State: protocol.StateVerified},
			},
		},
	}

	contextProfiles := map[string]*protocol.ContextProfile{
		profID: {
			SchemaVersion:             protocol.SchemaVersion1,
			ProfileID:                 profID,
			EndpointID:                epID,
			ChannelID:                 chanID,
			Runtime:                   "mlx-lm",
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
				{Workload: protocol.WorkloadImplementation, EffectiveTokens: 20000, CalibrationTask: "mlx-bench", CalibrationDate: "2026-10-01", ConfidenceLevel: "high"},
			},
		},
	}

	budgetStates := map[string]*protocol.BudgetState{
		poolID: {
			SchemaVersion:    protocol.SchemaVersion1,
			PoolID:           poolID,
			Status:           protocol.BudgetStatusHealthy,
			RemainingBalance: ptr[int64](36000),
			ObservedAt:       "2026-10-04T12:00:00Z",
		},
	}

	valPol := cognition.DefaultValidationPolicy()
	valPol.MaxSourceExposure = protocol.ExposureLocalOnly
	valPol.MaxCostClass = protocol.CostLocalCompute

	return testEnvConfig{
		name:            "Apple-MLX-LocalOnly",
		inventory:       inventory,
		machineProfile:  machineProfile,
		contextProfiles: contextProfiles,
		budgetStates:    budgetStates,
		validationPol:   valPol,
	}
}

// 2. NVIDIA CUDA local-only environment
func createNVIDIACUDAEnv() testEnvConfig {
	epID := "ep-cuda-01"
	chanID := "chan-cuda-01"
	profID := "prof-cuda-01"
	poolID := "pool-local-cuda"

	inventory := &protocol.ResourceInventory{
		SchemaVersion:      protocol.SchemaVersion1,
		InventoryID:        "inv-cuda-01",
		MachineFingerprint: "cuda123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ObservedAt:         protocol.Timestamp(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)),
		Hardware: protocol.HardwareSummary{
			OSFamily:            protocol.OSLinux,
			Arch:                "amd64",
			LogicalCores:        16,
			TotalMemoryBytes:    ptr[int64](64 * 1024 * 1024 * 1024),
			AcceleratorBackends: []protocol.BackendKind{protocol.BackendCUDA},
		},
		Readiness: []protocol.ScopeReadiness{
			{Scope: protocol.ScopeCanRunLocalInference, Status: protocol.ScopeStatusReady, Reason: "NVIDIA CUDA GPU verified"},
		},
		Policy: &protocol.PolicySummary{
			MaxSourceExposure: protocol.ExposureLocalOnly,
			MaxCostClass:      protocol.CostLocalCompute,
		},
		CognitionEndpoints: []protocol.CognitionEndpointSummary{
			{
				ID:                     epID,
				Kind:                   protocol.EndpointLocalRuntime,
				Locality:               protocol.LocalityLocal,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthNotApplicable,
				CostClass:              protocol.CostLocalCompute,
				RequiredSourceExposure: protocol.ExposureLocalOnly,
				AccelerationVerified:   true,
				AccelerationBackend:    ptr(protocol.BackendCUDA),
			},
		},
	}

	machineProfile := &protocol.MachineCapabilityProfile{
		SchemaVersion:      protocol.SchemaVersion1,
		ProfileID:          "mcp-cuda-01",
		MachineFingerprint: inventory.MachineFingerprint,
		ObservedAt:         inventory.ObservedAt,
		KnowledgeRevision:  "rev-cuda-1",
		ProbeDepth:         protocol.DepthInference,
		Endpoints: []protocol.CognitionEndpoint{
			{
				ID:                     epID,
				Kind:                   protocol.EndpointLocalRuntime,
				Locality:               protocol.LocalityLocal,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthNotApplicable,
				CostClass:              protocol.CostLocalCompute,
				RequiredSourceExposure: protocol.ExposureLocalOnly,
				StructuredOutput:       protocol.FeatureProbePassed,
				ToolUse:                protocol.FeatureProbePassed,
				Capabilities: []protocol.GradedCapability{
					{Dimension: protocol.CapabilityImplementation, Grade: protocol.GradeStrong, Provenance: protocol.ProvenanceMeasured},
					{Dimension: protocol.CapabilityReview, Grade: protocol.GradeStrong, Provenance: protocol.ProvenanceMeasured},
				},
				Acceleration: &protocol.AccelerationEvidence{Backend: protocol.BackendCUDA, State: protocol.StateVerified},
			},
		},
	}

	contextProfiles := map[string]*protocol.ContextProfile{
		profID: {
			SchemaVersion:             protocol.SchemaVersion1,
			ProfileID:                 profID,
			EndpointID:                epID,
			ChannelID:                 chanID,
			Runtime:                   "vllm",
			ModelRef:                  "deepseek-coder-v2",
			Revision:                  1,
			DeclaredWindowTokens:      65536,
			RuntimeWindowTokens:       65536,
			TargetResidentTokens:      48000,
			HardResidentCeilingTokens: 60000,
			ProtectedCoreLimitTokens:  8000,
			ContractLimitTokens:       20000,
			MaxSingleLeaseTokens:      8000,
			OutputReserveTokens:       4000,
			ToolTailReserveTokens:     2000,
			AccountingMethod:          protocol.AccountingExactBPE,
			ObservedContextControl:    protocol.ContextControlExactStateless,
			ObservedPrefixCache:       protocol.PrefixCacheSessionKV,
			WorkloadEnvelopes: []protocol.WorkloadEnvelope{
				{Workload: protocol.WorkloadImplementation, EffectiveTokens: 40000, CalibrationTask: "cuda-bench", CalibrationDate: "2026-10-01", ConfidenceLevel: "high"},
			},
		},
	}

	budgetStates := map[string]*protocol.BudgetState{
		poolID: {
			SchemaVersion:    protocol.SchemaVersion1,
			PoolID:           poolID,
			Status:           protocol.BudgetStatusHealthy,
			RemainingBalance: ptr[int64](36000),
			ObservedAt:       "2026-10-04T12:00:00Z",
		},
	}

	valPol := cognition.DefaultValidationPolicy()
	valPol.MaxSourceExposure = protocol.ExposureLocalOnly
	valPol.MaxCostClass = protocol.CostLocalCompute

	return testEnvConfig{
		name:            "NVIDIA-CUDA-LocalOnly",
		inventory:       inventory,
		machineProfile:  machineProfile,
		contextProfiles: contextProfiles,
		budgetStates:    budgetStates,
		validationPol:   valPol,
	}
}

// 3. Single subscription environment (OpenAI Codex CLI)
func createSingleSubscriptionEnv() testEnvConfig {
	epID := "ep-codex-cli"
	chanID := "chan-codex-cli"
	profID := "prof-codex-cli"
	poolID := "pool-openai-sub"

	inventory := &protocol.ResourceInventory{
		SchemaVersion:      protocol.SchemaVersion1,
		InventoryID:        "inv-sub-01",
		MachineFingerprint: "sub123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ObservedAt:         protocol.Timestamp(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)),
		Hardware: protocol.HardwareSummary{
			OSFamily:         protocol.OSLinux,
			Arch:             "amd64",
			LogicalCores:     8,
			TotalMemoryBytes: ptr[int64](16 * 1024 * 1024 * 1024),
		},
		Policy: &protocol.PolicySummary{
			MaxSourceExposure: protocol.ExposureFocusedSnippets,
			MaxCostClass:      protocol.CostSubscriptionIncluded,
		},
		CognitionEndpoints: []protocol.CognitionEndpointSummary{
			{
				ID:                     epID,
				Kind:                   protocol.EndpointAuthenticatedCLI,
				Locality:               protocol.LocalityRemoteInferenceLocalTools,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthAuthenticated,
				CostClass:              protocol.CostSubscriptionIncluded,
				RequiredSourceExposure: protocol.ExposureFocusedSnippets,
			},
		},
	}

	machineProfile := &protocol.MachineCapabilityProfile{
		SchemaVersion:      protocol.SchemaVersion1,
		ProfileID:          "mcp-sub-01",
		MachineFingerprint: inventory.MachineFingerprint,
		ObservedAt:         inventory.ObservedAt,
		KnowledgeRevision:  "rev-sub-1",
		ProbeDepth:         protocol.DepthInference,
		Endpoints: []protocol.CognitionEndpoint{
			{
				ID:                     epID,
				Kind:                   protocol.EndpointAuthenticatedCLI,
				Provider:               "openai",
				ModelID:                "gpt-4o",
				Locality:               protocol.LocalityRemoteInferenceLocalTools,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthAuthenticated,
				CostClass:              protocol.CostSubscriptionIncluded,
				RequiredSourceExposure: protocol.ExposureFocusedSnippets,
				StructuredOutput:       protocol.FeatureProbePassed,
				ToolUse:                protocol.FeatureProbePassed,
				Capabilities: []protocol.GradedCapability{
					{Dimension: protocol.CapabilityImplementation, Grade: protocol.GradeStrong, Provenance: protocol.ProvenanceMeasured},
					{Dimension: protocol.CapabilityReview, Grade: protocol.GradeStrong, Provenance: protocol.ProvenanceMeasured},
				},
			},
		},
	}

	contextProfiles := map[string]*protocol.ContextProfile{
		profID: {
			SchemaVersion:             protocol.SchemaVersion1,
			ProfileID:                 profID,
			EndpointID:                epID,
			ChannelID:                 chanID,
			Runtime:                   "openai-cli",
			ModelRef:                  "gpt-4o",
			Revision:                  1,
			DeclaredWindowTokens:      128000,
			RuntimeWindowTokens:       128000,
			TargetResidentTokens:      48000,
			HardResidentCeilingTokens: 96000,
			ProtectedCoreLimitTokens:  6000,
			ContractLimitTokens:       15000,
			MaxSingleLeaseTokens:      6000,
			OutputReserveTokens:       4000,
			ToolTailReserveTokens:     1000,
			AccountingMethod:          protocol.AccountingProviderAPI,
			ObservedContextControl:    protocol.ContextControlAppendOnly,
			ObservedPrefixCache:       protocol.PrefixCacheImplicit,
			WorkloadEnvelopes: []protocol.WorkloadEnvelope{
				{Workload: protocol.WorkloadImplementation, EffectiveTokens: 40000, CalibrationTask: "sub-bench", CalibrationDate: "2026-10-01", ConfidenceLevel: "high"},
			},
		},
	}

	budgetStates := map[string]*protocol.BudgetState{
		poolID: {
			SchemaVersion:    protocol.SchemaVersion1,
			PoolID:           poolID,
			Status:           protocol.BudgetStatusHealthy,
			RemainingBalance: ptr[int64](500),
			ObservedAt:       "2026-10-04T12:00:00Z",
		},
	}

	valPol := cognition.DefaultValidationPolicy()
	valPol.MaxSourceExposure = protocol.ExposureFocusedSnippets
	valPol.MaxCostClass = protocol.CostSubscriptionIncluded

	return testEnvConfig{
		name:            "Single-Subscription",
		inventory:       inventory,
		machineProfile:  machineProfile,
		contextProfiles: contextProfiles,
		budgetStates:    budgetStates,
		validationPol:   valPol,
	}
}

// 4. Multi-subscription environment (Anthropic Claude + OpenAI Codex)
func createMultiSubscriptionEnv() testEnvConfig {
	epClaude := "ep-claude-cli"
	chanClaude := "chan-claude-cli"
	profClaude := "prof-claude-cli"
	poolClaude := "pool-anthropic-sub"

	epCodex := "ep-codex-cli"
	chanCodex := "chan-codex-cli"
	profCodex := "prof-codex-cli"
	poolCodex := "pool-openai-sub"

	inventory := &protocol.ResourceInventory{
		SchemaVersion:      protocol.SchemaVersion1,
		InventoryID:        "inv-multisub-01",
		MachineFingerprint: "multisub123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		ObservedAt:         protocol.Timestamp(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)),
		Hardware: protocol.HardwareSummary{
			OSFamily:         protocol.OSDarwin,
			Arch:             "arm64",
			LogicalCores:     10,
			TotalMemoryBytes: ptr[int64](32 * 1024 * 1024 * 1024),
		},
		Policy: &protocol.PolicySummary{
			MaxSourceExposure: protocol.ExposureFocusedSnippets,
			MaxCostClass:      protocol.CostSubscriptionIncluded,
		},
		CognitionEndpoints: []protocol.CognitionEndpointSummary{
			{
				ID:                     epClaude,
				Kind:                   protocol.EndpointAuthenticatedCLI,
				Locality:               protocol.LocalityRemoteInferenceLocalTools,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthAuthenticated,
				CostClass:              protocol.CostSubscriptionIncluded,
				RequiredSourceExposure: protocol.ExposureFocusedSnippets,
			},
			{
				ID:                     epCodex,
				Kind:                   protocol.EndpointAuthenticatedCLI,
				Locality:               protocol.LocalityRemoteInferenceLocalTools,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthAuthenticated,
				CostClass:              protocol.CostSubscriptionIncluded,
				RequiredSourceExposure: protocol.ExposureFocusedSnippets,
			},
		},
	}

	machineProfile := &protocol.MachineCapabilityProfile{
		SchemaVersion:      protocol.SchemaVersion1,
		ProfileID:          "mcp-multisub-01",
		MachineFingerprint: inventory.MachineFingerprint,
		ObservedAt:         inventory.ObservedAt,
		KnowledgeRevision:  "rev-multisub-1",
		ProbeDepth:         protocol.DepthInference,
		Endpoints: []protocol.CognitionEndpoint{
			{
				ID:                     epClaude,
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
					{Dimension: protocol.CapabilityArchitecture, Grade: protocol.GradeStrong, Provenance: protocol.ProvenanceMeasured},
					{Dimension: protocol.CapabilityImplementation, Grade: protocol.GradeStrong, Provenance: protocol.ProvenanceMeasured},
					{Dimension: protocol.CapabilityReview, Grade: protocol.GradeStrong, Provenance: protocol.ProvenanceMeasured},
				},
			},
			{
				ID:                     epCodex,
				Kind:                   protocol.EndpointAuthenticatedCLI,
				Provider:               "openai",
				ModelID:                "gpt-4o",
				Locality:               protocol.LocalityRemoteInferenceLocalTools,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthAuthenticated,
				CostClass:              protocol.CostSubscriptionIncluded,
				RequiredSourceExposure: protocol.ExposureFocusedSnippets,
				StructuredOutput:       protocol.FeatureProbePassed,
				ToolUse:                protocol.FeatureProbePassed,
				Capabilities: []protocol.GradedCapability{
					{Dimension: protocol.CapabilityImplementation, Grade: protocol.GradeStrong, Provenance: protocol.ProvenanceMeasured},
					{Dimension: protocol.CapabilityReview, Grade: protocol.GradeStrong, Provenance: protocol.ProvenanceMeasured},
				},
			},
		},
	}

	contextProfiles := map[string]*protocol.ContextProfile{
		profClaude: {
			SchemaVersion:             protocol.SchemaVersion1,
			ProfileID:                 profClaude,
			EndpointID:                epClaude,
			ChannelID:                 chanClaude,
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
				{Workload: protocol.WorkloadImplementation, EffectiveTokens: 50000, CalibrationTask: "claude-bench", CalibrationDate: "2026-10-01", ConfidenceLevel: "high"},
			},
		},
		profCodex: {
			SchemaVersion:             protocol.SchemaVersion1,
			ProfileID:                 profCodex,
			EndpointID:                epCodex,
			ChannelID:                 chanCodex,
			Runtime:                   "openai-cli",
			ModelRef:                  "gpt-4o",
			Revision:                  1,
			DeclaredWindowTokens:      128000,
			RuntimeWindowTokens:       128000,
			TargetResidentTokens:      48000,
			HardResidentCeilingTokens: 96000,
			ProtectedCoreLimitTokens:  6000,
			ContractLimitTokens:       15000,
			MaxSingleLeaseTokens:      6000,
			OutputReserveTokens:       4000,
			ToolTailReserveTokens:     1000,
			AccountingMethod:          protocol.AccountingProviderAPI,
			ObservedContextControl:    protocol.ContextControlAppendOnly,
			ObservedPrefixCache:       protocol.PrefixCacheImplicit,
			WorkloadEnvelopes: []protocol.WorkloadEnvelope{
				{Workload: protocol.WorkloadImplementation, EffectiveTokens: 40000, CalibrationTask: "codex-bench", CalibrationDate: "2026-10-01", ConfidenceLevel: "high"},
			},
		},
	}

	budgetStates := map[string]*protocol.BudgetState{
		poolClaude: {
			SchemaVersion:    protocol.SchemaVersion1,
			PoolID:           poolClaude,
			Status:           protocol.BudgetStatusHealthy,
			RemainingBalance: ptr[int64](400),
			ObservedAt:       "2026-10-04T12:00:00Z",
		},
		poolCodex: {
			SchemaVersion:    protocol.SchemaVersion1,
			PoolID:           poolCodex,
			Status:           protocol.BudgetStatusHealthy,
			RemainingBalance: ptr[int64](500),
			ObservedAt:       "2026-10-04T12:00:00Z",
		},
	}

	valPol := cognition.DefaultValidationPolicy()
	valPol.MaxSourceExposure = protocol.ExposureFocusedSnippets
	valPol.MaxCostClass = protocol.CostSubscriptionIncluded

	return testEnvConfig{
		name:            "Multi-Subscription",
		inventory:       inventory,
		machineProfile:  machineProfile,
		contextProfiles: contextProfiles,
		budgetStates:    budgetStates,
		validationPol:   valPol,
	}
}

// 5. Mixed Environment (Apple MLX local + Anthropic subscription + OpenAI remote API)
func createMixedEnv(allowMetered bool) testEnvConfig {
	epMLX := "ep-mlx-01"
	chanMLX := "chan-mlx-01"
	profMLX := "prof-mlx-01"
	poolMLX := "pool-local-mlx"

	epClaude := "ep-claude-cli"
	chanClaude := "chan-claude-cli"
	profClaude := "prof-claude-cli"
	poolClaude := "pool-anthropic-sub"

	epMetered := "ep-openai-api"
	chanMetered := "chan-openai-api"
	profMetered := "prof-openai-api"
	poolMetered := "pool-metered-api"

	maxExposure := protocol.ExposureFocusedSnippets
	maxCost := protocol.CostSubscriptionIncluded
	if allowMetered {
		maxCost = protocol.CostRemoteEconomy
	}

	inventory := &protocol.ResourceInventory{
		SchemaVersion:      protocol.SchemaVersion1,
		InventoryID:        "inv-mixed-01",
		MachineFingerprint: "mixed123456789abcdef0123456789abcdef0123456789abcdef0123456789ab",
		ObservedAt:         protocol.Timestamp(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)),
		Hardware: protocol.HardwareSummary{
			OSFamily:            protocol.OSDarwin,
			Arch:                "arm64",
			LogicalCores:        12,
			TotalMemoryBytes:    ptr[int64](36 * 1024 * 1024 * 1024),
			AcceleratorBackends: []protocol.BackendKind{protocol.BackendMetal},
		},
		Readiness: []protocol.ScopeReadiness{
			{Scope: protocol.ScopeCanRunLocalInference, Status: protocol.ScopeStatusReady, Reason: "Metal ready"},
		},
		Policy: &protocol.PolicySummary{
			MaxSourceExposure: maxExposure,
			MaxCostClass:      maxCost,
		},
		CognitionEndpoints: []protocol.CognitionEndpointSummary{
			{
				ID:                     epMLX,
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
				ID:                     epClaude,
				Kind:                   protocol.EndpointAuthenticatedCLI,
				Locality:               protocol.LocalityRemoteInferenceLocalTools,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthAuthenticated,
				CostClass:              protocol.CostSubscriptionIncluded,
				RequiredSourceExposure: protocol.ExposureFocusedSnippets,
			},
			{
				ID:                     epMetered,
				Kind:                   protocol.EndpointRemoteAPI,
				Locality:               protocol.LocalityRemote,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthAuthenticated,
				CostClass:              protocol.CostRemoteEconomy,
				RequiredSourceExposure: protocol.ExposureFocusedSnippets,
			},
		},
	}

	machineProfile := &protocol.MachineCapabilityProfile{
		SchemaVersion:      protocol.SchemaVersion1,
		ProfileID:          "mcp-mixed-01",
		MachineFingerprint: inventory.MachineFingerprint,
		ObservedAt:         inventory.ObservedAt,
		KnowledgeRevision:  "rev-mixed-1",
		ProbeDepth:         protocol.DepthInference,
		Endpoints: []protocol.CognitionEndpoint{
			{
				ID:                     epMLX,
				Kind:                   protocol.EndpointLocalRuntime,
				Locality:               protocol.LocalityLocal,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthNotApplicable,
				CostClass:              protocol.CostLocalCompute,
				RequiredSourceExposure: protocol.ExposureLocalOnly,
				StructuredOutput:       protocol.FeatureProbePassed,
				ToolUse:                protocol.FeatureProbePassed,
				Capabilities: []protocol.GradedCapability{
					{Dimension: protocol.CapabilityImplementation, Grade: protocol.GradeStrong, Provenance: protocol.ProvenanceMeasured},
					{Dimension: protocol.CapabilityReview, Grade: protocol.GradeStrong, Provenance: protocol.ProvenanceMeasured},
				},
				Acceleration: &protocol.AccelerationEvidence{Backend: protocol.BackendMetal, State: protocol.StateVerified},
			},
			{
				ID:                     epClaude,
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
					{Dimension: protocol.CapabilityArchitecture, Grade: protocol.GradeStrong, Provenance: protocol.ProvenanceMeasured},
					{Dimension: protocol.CapabilityImplementation, Grade: protocol.GradeStrong, Provenance: protocol.ProvenanceMeasured},
					{Dimension: protocol.CapabilityReview, Grade: protocol.GradeStrong, Provenance: protocol.ProvenanceMeasured},
				},
			},
			{
				ID:                     epMetered,
				Kind:                   protocol.EndpointRemoteAPI,
				Provider:               "openai",
				ModelID:                "o3-mini",
				Locality:               protocol.LocalityRemote,
				Health:                 protocol.EndpointHealthReady,
				Auth:                   protocol.AuthAuthenticated,
				CostClass:              protocol.CostRemoteEconomy,
				RequiredSourceExposure: protocol.ExposureFocusedSnippets,
				StructuredOutput:       protocol.FeatureProbePassed,
				ToolUse:                protocol.FeatureProbePassed,
				Capabilities: []protocol.GradedCapability{
					{Dimension: protocol.CapabilityImplementation, Grade: protocol.GradeStrong, Provenance: protocol.ProvenanceMeasured},
					{Dimension: protocol.CapabilityReview, Grade: protocol.GradeStrong, Provenance: protocol.ProvenanceMeasured},
				},
			},
		},
	}

	contextProfiles := map[string]*protocol.ContextProfile{
		profMLX: {
			SchemaVersion:             protocol.SchemaVersion1,
			ProfileID:                 profMLX,
			EndpointID:                epMLX,
			ChannelID:                 chanMLX,
			Runtime:                   "mlx-lm",
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
				{Workload: protocol.WorkloadImplementation, EffectiveTokens: 20000, CalibrationTask: "mlx-bench", CalibrationDate: "2026-10-01", ConfidenceLevel: "high"},
			},
		},
		profClaude: {
			SchemaVersion:             protocol.SchemaVersion1,
			ProfileID:                 profClaude,
			EndpointID:                epClaude,
			ChannelID:                 chanClaude,
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
				{Workload: protocol.WorkloadImplementation, EffectiveTokens: 50000, CalibrationTask: "claude-bench", CalibrationDate: "2026-10-01", ConfidenceLevel: "high"},
			},
		},
		profMetered: {
			SchemaVersion:             protocol.SchemaVersion1,
			ProfileID:                 profMetered,
			EndpointID:                epMetered,
			ChannelID:                 chanMetered,
			Runtime:                   "openai-api",
			ModelRef:                  "o3-mini",
			Revision:                  1,
			DeclaredWindowTokens:      128000,
			RuntimeWindowTokens:       128000,
			TargetResidentTokens:      48000,
			HardResidentCeilingTokens: 96000,
			ProtectedCoreLimitTokens:  6000,
			ContractLimitTokens:       15000,
			MaxSingleLeaseTokens:      6000,
			OutputReserveTokens:       4000,
			ToolTailReserveTokens:     1000,
			AccountingMethod:          protocol.AccountingProviderAPI,
			ObservedContextControl:    protocol.ContextControlExactStateless,
			ObservedPrefixCache:       protocol.PrefixCacheSessionKV,
			WorkloadEnvelopes: []protocol.WorkloadEnvelope{
				{Workload: protocol.WorkloadImplementation, EffectiveTokens: 40000, CalibrationTask: "api-bench", CalibrationDate: "2026-10-01", ConfidenceLevel: "high"},
			},
		},
	}

	budgetStates := map[string]*protocol.BudgetState{
		poolMLX: {
			SchemaVersion:    protocol.SchemaVersion1,
			PoolID:           poolMLX,
			Status:           protocol.BudgetStatusHealthy,
			RemainingBalance: ptr[int64](36000),
			ObservedAt:       "2026-10-04T12:00:00Z",
		},
		poolClaude: {
			SchemaVersion:    protocol.SchemaVersion1,
			PoolID:           poolClaude,
			Status:           protocol.BudgetStatusHealthy,
			RemainingBalance: ptr[int64](400),
			ObservedAt:       "2026-10-04T12:00:00Z",
		},
		poolMetered: {
			SchemaVersion:    protocol.SchemaVersion1,
			PoolID:           poolMetered,
			Status:           protocol.BudgetStatusHealthy,
			RemainingBalance: ptr[int64](1000),
			ObservedAt:       "2026-10-04T12:00:00Z",
		},
	}

	name := "Mixed-PaidAPIAllowed"
	if !allowMetered {
		name = "Mixed-PaidAPIForbidden"
	}

	valPol := cognition.DefaultValidationPolicy()
	valPol.MaxSourceExposure = maxExposure
	valPol.MaxCostClass = maxCost

	return testEnvConfig{
		name:            name,
		inventory:       inventory,
		machineProfile:  machineProfile,
		contextProfiles: contextProfiles,
		budgetStates:    budgetStates,
		validationPol:   valPol,
	}
}

// synthesizePortfolioForEnv generates a valid CognitionPortfolio matching an environment and intent.
func synthesizePortfolioForEnv(env testEnvConfig, intent protocol.RecommendationIntent) *protocol.CognitionPortfolio {
	p := &protocol.CognitionPortfolio{
		SchemaVersion:     protocol.SchemaVersion1,
		PortfolioID:       "port-" + env.name + "-" + string(intent),
		Revision:          1,
		CreatedAt:         "2026-10-04T12:00:00Z",
		MaxSourceExposure: env.inventory.Policy.MaxSourceExposure,
	}

	// Add channels and budget pools corresponding to endpoints in inventory
	for _, ep := range env.inventory.CognitionEndpoints {
		var chanKind protocol.ChannelKind
		var sessMode protocol.SessionMode
		var ctxCtrl protocol.ContextControl
		var prefixCache protocol.PrefixCache

		switch ep.Kind {
		case protocol.EndpointLocalRuntime:
			chanKind = protocol.ChannelLocalDaemonSocket
			sessMode = protocol.SessionStatelessPerCall
			ctxCtrl = protocol.ContextControlExactStateless
			prefixCache = protocol.PrefixCacheSessionKV
		case protocol.EndpointAuthenticatedCLI:
			chanKind = protocol.ChannelCLISubprocess
			sessMode = protocol.SessionResumableHandle
			ctxCtrl = protocol.ContextControlAppendOnly
			prefixCache = protocol.PrefixCacheImplicit
		default:
			chanKind = protocol.ChannelDirectHTTPAPI
			sessMode = protocol.SessionStatelessPerCall
			ctxCtrl = protocol.ContextControlExactStateless
			prefixCache = protocol.PrefixCacheSessionKV
		}

		chanID := "chan-" + ep.ID
		poolID := "pool-" + ep.ID

		p.Channels = append(p.Channels, protocol.AccessChannel{
			SchemaVersion:         protocol.SchemaVersion1,
			ChannelID:             chanID,
			EndpointID:            ep.ID,
			Kind:                  chanKind,
			SessionMode:           sessMode,
			ContextControl:        ctxCtrl,
			PrefixCache:           prefixCache,
			SupportsStreaming:     true,
			SupportsTools:         true,
			MaxConcurrentRequests: 2,
		})

		var reg protocol.EconomicRegime
		var unit protocol.BudgetUnit
		switch ep.CostClass {
		case protocol.CostLocalCompute:
			reg = protocol.RegimeLocalCompute
			unit = protocol.UnitSeconds
		case protocol.CostSubscriptionIncluded:
			reg = protocol.RegimeSubscriptionQuota
			unit = protocol.UnitRequests
		case protocol.CostRemoteEconomy, protocol.CostRemoteStrong, protocol.CostFrontierExpensive:
			reg = protocol.RegimeMeteredAPI
			unit = protocol.UnitUSDCents
		default:
			reg = protocol.RegimeLocalCompute
			unit = protocol.UnitSeconds
		}

		p.BudgetPools = append(p.BudgetPools, protocol.BudgetPool{
			SchemaVersion:  protocol.SchemaVersion1,
			PoolID:         poolID,
			Name:           "Pool for " + ep.ID,
			Regime:         reg,
			HardLimit:      1000,
			SoftAlertLimit: 800,
			Unit:           unit,
			Period:         protocol.PeriodRollingDay,
		})
	}

	// Build role bindings
	primaryEP := env.inventory.CognitionEndpoints[0]
	profID := "prof-" + primaryEP.ID
	p.RoleBindings = []protocol.RoleBinding{
		{
			Role:             "implementer",
			EndpointID:       primaryEP.ID,
			ChannelID:        "chan-" + primaryEP.ID,
			BudgetPoolID:     "pool-" + primaryEP.ID,
			ContextProfileID: profID,
			Priority:         1,
		},
		{
			Role:             "reviewer",
			EndpointID:       primaryEP.ID,
			ChannelID:        "chan-" + primaryEP.ID,
			BudgetPoolID:     "pool-" + primaryEP.ID,
			ContextProfileID: profID,
			Priority:         1,
		},
	}

	// If multiple endpoints exist, configure fallback on reviewer for dual review independence
	if len(env.inventory.CognitionEndpoints) > 1 {
		secondEP := env.inventory.CognitionEndpoints[1]
		p.RoleBindings[1].Fallbacks = []protocol.FallbackBinding{
			{
				EndpointID:       secondEP.ID,
				ChannelID:        "chan-" + secondEP.ID,
				BudgetPoolID:     "pool-" + secondEP.ID,
				ContextProfileID: "prof-" + secondEP.ID,
			},
		}
	}

	// Update context profiles map in env to include exact matching channel/endpoint IDs
	for _, ch := range p.Channels {
		profKey := "prof-" + ch.EndpointID
		if _, exists := env.contextProfiles[profKey]; !exists {
			env.contextProfiles[profKey] = &protocol.ContextProfile{
				SchemaVersion:             protocol.SchemaVersion1,
				ProfileID:                 profKey,
				EndpointID:                ch.EndpointID,
				ChannelID:                 ch.ChannelID,
				Runtime:                   "generic",
				ModelRef:                  "test-model",
				Revision:                  1,
				DeclaredWindowTokens:      32768,
				RuntimeWindowTokens:       32768,
				TargetResidentTokens:      20000,
				HardResidentCeilingTokens: 30000,
				ProtectedCoreLimitTokens:  4000,
				ContractLimitTokens:       10000,
				MaxSingleLeaseTokens:      4000,
				OutputReserveTokens:       2000,
				ToolTailReserveTokens:     1000,
				AccountingMethod:          protocol.AccountingExactBPE,
				ObservedContextControl:    ch.ContextControl,
				ObservedPrefixCache:       ch.PrefixCache,
				WorkloadEnvelopes: []protocol.WorkloadEnvelope{
					{Workload: protocol.WorkloadImplementation, EffectiveTokens: 20000, CalibrationTask: "t", CalibrationDate: "2026-10-01", ConfidenceLevel: "high"},
				},
			}
		} else {
			env.contextProfiles[profKey].EndpointID = ch.EndpointID
			env.contextProfiles[profKey].ChannelID = ch.ChannelID
		}
		// ensure budget state exists
		poolKey := "pool-" + ch.EndpointID
		if _, exists := env.budgetStates[poolKey]; !exists {
			env.budgetStates[poolKey] = &protocol.BudgetState{
				SchemaVersion:    protocol.SchemaVersion1,
				PoolID:           poolKey,
				Status:           protocol.BudgetStatusHealthy,
				RemainingBalance: ptr[int64](500),
				ObservedAt:       "2026-10-04T12:00:00Z",
			}
		}
	}

	return p
}

// --- Milestone M3D Exit Test Suites ---

// 1. Recommendation Synthesis Matrix across 4 intents and varying inventory environments.
func TestM3DCrossPortfolio_RecommendationSynthesisMatrix(t *testing.T) {
	schemas, err := schema.Default()
	if err != nil {
		t.Fatalf("schema.Default: %v", err)
	}

	allIntents := []protocol.RecommendationIntent{
		protocol.IntentMinimumSpend,
		protocol.IntentBalanced,
		protocol.IntentMaximumQualityWithinPolicy,
		protocol.IntentPrivacyFirst,
	}

	environments := []testEnvConfig{
		createAppleMLXEnv(),
		createNVIDIACUDAEnv(),
		createSingleSubscriptionEnv(),
		createMultiSubscriptionEnv(),
		createMixedEnv(true),  // Paid API allowed
		createMixedEnv(false), // Paid API forbidden
	}

	for _, env := range environments {
		t.Run("Env_"+env.name, func(t *testing.T) {
			clk := clock.NewFake(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC), time.Second)

			// Setup mock driver resolver
			outputJSON := buildPlannerOutputJSON(allIntents, func(intent protocol.RecommendationIntent) *protocol.CognitionPortfolio {
				return synthesizePortfolioForEnv(env, intent)
			})

			driver := &m3dMockDriver{
				id:   "drv-" + env.inventory.CognitionEndpoints[0].ID,
				caps: drivers.DriverCapabilities{Kind: protocol.ChannelLocalDaemonSocket, SessionMode: protocol.SessionStatelessPerCall, ContextControl: protocol.ContextControlExactStateless, PrefixCache: protocol.PrefixCacheSessionKV, MaxConcurrentRequests: 2, NativeWorktreeAccess: false, SupportsStreaming: true},
				handler: func(ctx context.Context, input drivers.TurnInput) (drivers.TurnResult, error) {
					return drivers.TurnResult{TurnID: input.TurnID, Content: outputJSON}, nil
				},
			}

			resolver := &m3dDriverResolver{
				drivers: map[string]drivers.SessionDriver{env.inventory.CognitionEndpoints[0].ID: driver},
				models:  map[string]string{env.inventory.CognitionEndpoints[0].ID: "test-model"},
			}

			req := planner.Request{
				Inventory:       env.inventory,
				MachineProfile:  env.machineProfile,
				ContextProfiles: env.contextProfiles,
				BudgetStates:    env.budgetStates,
				Policy:          &env.validationPol,
				Project: planner.ProjectCharacteristics{
					Languages: []string{"go", "typescript"},
					RiskTags:  []string{"security-sensitive"},
				},
				Intents: allIntents,
				Clock:   clk,
			}

			cfg := plannerdriver.ExecutionConfig{
				Timeout:          5 * time.Second,
				IncludeErrorText: false,
			}

			// Execute planning
			res, err := plannerdriver.ExecutePlanning(context.Background(), req, resolver, cfg)
			if err != nil {
				t.Fatalf("ExecutePlanning failed: %v", err)
			}
			if res.Outcome != planner.OutcomeRecommended {
				t.Fatalf("expected OutcomeRecommended, got %s (detail: %s, rejected: %+v)", res.Outcome, res.Detail, res.Rejected)
			}

			if len(res.Accepted) != len(allIntents) {
				t.Fatalf("expected %d accepted recommendations, got %d", len(allIntents), len(res.Accepted))
			}

			// Validate each recommendation against schema and domain invariants
			for idx, rec := range res.Accepted {
				if rec.Intent != allIntents[idx] {
					t.Errorf("expected intent %s at index %d, got %s", allIntents[idx], idx, rec.Intent)
				}
				if rec.SetID == "" {
					t.Errorf("expected non-empty SetID")
				}
				if rec.Confidence != protocol.RecommendationConfidenceHigh {
					t.Errorf("expected high confidence, got %s", rec.Confidence)
				}
				if rec.Planner == nil {
					t.Fatalf("expected non-nil Planner provenance")
				}
				if rec.Planner.EndpointID != env.inventory.CognitionEndpoints[0].ID {
					t.Errorf("expected planner endpoint %s, got %s", env.inventory.CognitionEndpoints[0].ID, rec.Planner.EndpointID)
				}

				// Schema Validation: PortfolioRecommendation
				recBytes, err := json.Marshal(rec)
				if err != nil {
					t.Fatalf("marshal recommendation: %v", err)
				}
				if err := schemas.ValidateBytes(schema.Name("portfolio-recommendation"), recBytes); err != nil {
					t.Errorf("recommendation schema validation failed: %v", err)
				}

				// Schema Validation: Embedded CognitionPortfolio
				portBytes, err := json.Marshal(rec.RecommendedPortfolio)
				if err != nil {
					t.Fatalf("marshal portfolio: %v", err)
				}
				if err := schemas.ValidateBytes(schema.Name("cognition-portfolio"), portBytes); err != nil {
					t.Errorf("portfolio schema validation failed: %v", err)
				}
			}

			// INV-01: Pure deterministic validation gates (AI proposes, deterministic machinery authorizes).
			// If model proposes an alternative referencing a non-existent endpoint, it MUST be rejected by PortfolioValidator.
			t.Run("DeterministicValidatorGating_RejectsInvalidProposal", func(t *testing.T) {
				invalidPortfolio := synthesizePortfolioForEnv(env, protocol.IntentBalanced)
				invalidPortfolio.RoleBindings[0].EndpointID = "ep-nonexistent-endpoint"

				invalidOutput := buildPlannerOutputJSON([]protocol.RecommendationIntent{protocol.IntentBalanced}, func(i protocol.RecommendationIntent) *protocol.CognitionPortfolio {
					return invalidPortfolio
				})

				invDriver := &m3dMockDriver{
					id:   "drv-invalid",
					caps: drivers.DriverCapabilities{Kind: protocol.ChannelLocalDaemonSocket, SessionMode: protocol.SessionStatelessPerCall, ContextControl: protocol.ContextControlExactStateless, PrefixCache: protocol.PrefixCacheSessionKV, MaxConcurrentRequests: 2, NativeWorktreeAccess: false},
					handler: func(ctx context.Context, input drivers.TurnInput) (drivers.TurnResult, error) {
						return drivers.TurnResult{TurnID: input.TurnID, Content: invalidOutput}, nil
					},
				}
				invResolver := &m3dDriverResolver{
					drivers: map[string]drivers.SessionDriver{env.inventory.CognitionEndpoints[0].ID: invDriver},
					models:  map[string]string{env.inventory.CognitionEndpoints[0].ID: "test-model"},
				}

				invalidReq := req
				invalidReq.Intents = []protocol.RecommendationIntent{protocol.IntentBalanced}
				invalidRes, err := plannerdriver.ExecutePlanning(context.Background(), invalidReq, invResolver, cfg)
				if err != nil {
					t.Fatalf("ExecutePlanning error: %v", err)
				}
				if invalidRes.Outcome != planner.OutcomeAllRejected {
					t.Errorf("expected OutcomeAllRejected for invalid role binding, got %s", invalidRes.Outcome)
				}
				if len(invalidRes.Rejected) == 0 {
					t.Errorf("expected structured rejection records in result")
				}
			})

			// INV-02: Graceful degradation / fallback on model or planner failure (DCI-104)
			t.Run("GracefulDegradation_UnavailablePlanner", func(t *testing.T) {
				emptyResolver := &m3dDriverResolver{drivers: map[string]drivers.SessionDriver{}}
				fallbackRes, err := plannerdriver.ExecutePlanning(context.Background(), req, emptyResolver, cfg)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if fallbackRes.Outcome != planner.OutcomeNoPlanner {
					t.Errorf("expected OutcomeNoPlanner when no planner available, got %s", fallbackRes.Outcome)
				}
			})
		})
	}
}

// 2. Workflow Planning with Model-Assisted vs Deterministic Fallback Topologies.
func TestM3DCrossPortfolio_WorkflowPlanning_ModelAndFallback(t *testing.T) {
	schemas, err := schema.Default()
	if err != nil {
		t.Fatalf("schema.Default: %v", err)
	}

	env := createMultiSubscriptionEnv()
	portfolio := synthesizePortfolioForEnv(env, protocol.IntentBalanced)

	t.Run("ModelAssisted_ValidTopology_Accepted", func(t *testing.T) {
		invoker := &m3dWorkflowMockInvoker{
			invokeFn: func(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error) {
				jsonOutput := `{
					"topology": "iterative_escalation",
					"stages": [
						{
							"stage_id": "stage-impl",
							"role": "implementer",
							"order": 1,
							"budget_pool_id": "pool-ep-claude-cli",
							"endpoint_id": "ep-claude-cli",
							"channel_id": "chan-ep-claude-cli",
							"context_profile_id": "prof-ep-claude-cli",
							"timeout_seconds": 600
						},
						{
							"stage_id": "stage-rev",
							"role": "reviewer",
							"is_review": true,
							"order": 2,
							"depends_on": ["stage-impl"],
							"budget_pool_id": "pool-ep-claude-cli",
							"endpoint_id": "ep-claude-cli",
							"channel_id": "chan-ep-claude-cli",
							"context_profile_id": "prof-ep-claude-cli",
							"timeout_seconds": 300
						}
					]
				}`
				return planner.InvocationResult{Content: jsonOutput}, nil
			},
		}

		req := workflowplanner.ModelPlanRequest{
			Task: workflowplanner.TaskSpec{
				TaskID:        "task-m3d-model-01",
				WorkPackageID: "WP-M3D-2C",
			},
			Portfolio:    portfolio,
			BudgetStates: env.budgetStates,
			Invoker:      invoker,
		}

		res, err := workflowplanner.PlanWorkflowWithModel(context.Background(), req)
		if err != nil {
			t.Fatalf("PlanWorkflowWithModel failed: %v", err)
		}
		if !res.UsedModel {
			t.Fatalf("expected UsedModel: true, fallback reason: %s", res.FallbackReason)
		}
		if res.Plan == nil {
			t.Fatalf("expected non-nil WorkflowPlan")
		}
		if len(res.Plan.Stages) != 2 {
			t.Errorf("expected 2 stages from model, got %d", len(res.Plan.Stages))
		}

		// Validate against JSON schema
		planBytes, err := json.Marshal(res.Plan)
		if err != nil {
			t.Fatalf("marshal plan: %v", err)
		}
		if err := schemas.ValidateBytes(schema.Name("workflow-plan"), planBytes); err != nil {
			t.Errorf("workflow plan schema validation failed: %v", err)
		}
	})

	t.Run("ModelAssisted_Failures_GracefulDegradation", func(t *testing.T) {
		baseTask := workflowplanner.TaskSpec{
			TaskID:        "task-m3d-degrade-01",
			WorkPackageID: "WP-M3D-2C",
		}

		cases := []struct {
			name               string
			invoker            planner.Invoker
			budgetStates       map[string]*protocol.BudgetState
			wantFallbackReason string
		}{
			{
				name:               "NilInvoker",
				invoker:            nil,
				budgetStates:       env.budgetStates,
				wantFallbackReason: "no_invoker_or_deterministic_only",
			},
			{
				name: "InvokerError",
				invoker: &m3dWorkflowMockInvoker{
					invokeFn: func(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error) {
						return planner.InvocationResult{}, errors.New("timeout connecting to model driver")
					},
				},
				budgetStates:       env.budgetStates,
				wantFallbackReason: "invoker_error",
			},
			{
				name: "MalformedJSON",
				invoker: &m3dWorkflowMockInvoker{
					invokeFn: func(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error) {
						return planner.InvocationResult{Content: "{ broken json string ..."}, nil
					},
				},
				budgetStates:       env.budgetStates,
				wantFallbackReason: "malformed_model_output",
			},
			{
				name: "StageValidationError_UnboundRole",
				invoker: &m3dWorkflowMockInvoker{
					invokeFn: func(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error) {
						badOutput := `{
							"stages": [
								{
									"stage_id": "s1",
									"role": "unbound_astronaut_role",
									"order": 1,
									"budget_pool_id": "pool-ep-claude-cli",
									"endpoint_id": "ep-claude-cli",
									"channel_id": "chan-ep-claude-cli",
									"context_profile_id": "prof-ep-claude-cli"
								}
							]
						}`
						return planner.InvocationResult{Content: badOutput}, nil
					},
				},
				budgetStates:       env.budgetStates,
				wantFallbackReason: "validator_error",
			},
			{
				name: "BudgetAuthorization_ExhaustedPool",
				invoker: &m3dWorkflowMockInvoker{
					invokeFn: func(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error) {
						// Propose using the second pool (pool-ep-codex-cli) which is exhausted,
						// while primary pool (pool-ep-claude-cli) is healthy.
						output := `{
							"stages": [
								{
									"stage_id": "s1",
									"role": "reviewer",
									"is_review": true,
									"order": 1,
									"budget_pool_id": "pool-ep-codex-cli",
									"endpoint_id": "ep-codex-cli",
									"channel_id": "chan-ep-codex-cli",
									"context_profile_id": "prof-ep-codex-cli"
								}
							]
						}`
						return planner.InvocationResult{Content: output}, nil
					},
				},
				budgetStates: map[string]*protocol.BudgetState{
					"pool-ep-claude-cli": {
						SchemaVersion:    protocol.SchemaVersion1,
						PoolID:           "pool-ep-claude-cli",
						Status:           protocol.BudgetStatusHealthy,
						RemainingBalance: ptr[int64](500),
						ObservedAt:       "2026-10-04T12:00:00Z",
					},
					"pool-ep-codex-cli": {
						SchemaVersion:    protocol.SchemaVersion1,
						PoolID:           "pool-ep-codex-cli",
						Status:           protocol.BudgetStatusExhausted, // exhausted
						RemainingBalance: ptr[int64](0),
						ObservedAt:       "2026-10-04T12:00:00Z",
					},
				},
				wantFallbackReason: "budget_authorization_failed",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				req := workflowplanner.ModelPlanRequest{
					Task:         baseTask,
					Portfolio:    portfolio,
					BudgetStates: tc.budgetStates,
					Invoker:      tc.invoker,
				}

				res, err := workflowplanner.PlanWorkflowWithModel(context.Background(), req)
				if err != nil {
					t.Fatalf("PlanWorkflowWithModel failed: %v", err)
				}
				if res.UsedModel {
					t.Errorf("expected UsedModel: false, got true")
				}
				if res.Plan == nil {
					t.Fatalf("expected deterministic baseline plan to be returned upon degradation (DCI-104)")
				}
				if tc.wantFallbackReason != "" && len(res.FallbackReason) == 0 {
					t.Errorf("expected non-empty FallbackReason")
				}

				// Verify baseline plan validates against schema
				planBytes, err := json.Marshal(res.Plan)
				if err != nil {
					t.Fatalf("marshal plan: %v", err)
				}
				if err := schemas.ValidateBytes(schema.Name("workflow-plan"), planBytes); err != nil {
					t.Errorf("baseline workflow plan schema validation failed: %v", err)
				}
			})
		}
	})

	t.Run("DeterministicBaseline_Topologies", func(t *testing.T) {
		// Low risk -> SinglePass
		resLow, err := workflowplanner.PlanWorkflow(workflowplanner.PlanRequest{
			Task: workflowplanner.TaskSpec{
				TaskID:             "task-low-risk",
				WorkPackageID:      "WP-LOW",
				RiskTags:           []string{"trivial"},
				RequiresDualReview: false,
			},
			Portfolio: portfolio,
		})
		if err != nil {
			t.Fatalf("PlanWorkflow low risk error: %v", err)
		}
		if resLow.Plan.Topology != protocol.TopologySinglePass {
			t.Errorf("expected TopologySinglePass, got %s", resLow.Plan.Topology)
		}

		// High risk -> TopologyDualIndependentReview
		resHigh, err := workflowplanner.PlanWorkflow(workflowplanner.PlanRequest{
			Task: workflowplanner.TaskSpec{
				TaskID:             "task-high-risk",
				WorkPackageID:      "WP-HIGH",
				RiskTags:           []string{"security"},
				RequiresDualReview: true,
			},
			Portfolio: portfolio,
		})
		if err != nil {
			t.Fatalf("PlanWorkflow high risk error: %v", err)
		}
		if resHigh.Plan.Topology != protocol.TopologyDualIndependentReview {
			t.Errorf("expected TopologyDualIndependentReview, got %s", resHigh.Plan.Topology)
		}

		// Deterministic only -> TopologyDeterministicOnly
		resDet, err := workflowplanner.PlanWorkflow(workflowplanner.PlanRequest{
			Task: workflowplanner.TaskSpec{
				TaskID:            "task-det-only",
				WorkPackageID:     "WP-DET",
				DeterministicOnly: true,
			},
			Portfolio: portfolio,
		})
		if err != nil {
			t.Fatalf("PlanWorkflow deterministic-only error: %v", err)
		}
		if resDet.Plan.Topology != protocol.TopologyDeterministicOnly {
			t.Errorf("expected TopologyDeterministicOnly, got %s", resDet.Plan.Topology)
		}
		for _, st := range resDet.Plan.Stages {
			if st.EndpointID != nil || st.ChannelID != nil {
				t.Errorf("deterministic stage must not have endpoint or channel bindings")
			}
			if st.DeterministicGateID == nil {
				t.Errorf("deterministic stage must have DeterministicGateID")
			}
		}
	})
}

// 3. Dynamic Portfolio Adaptation, Semantic Diffing, and Atomic Lineage Activation.
func TestM3DCrossPortfolio_AdaptationDiffingAndActivationLineage(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFake(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC), time.Second)

	env1 := createAppleMLXEnv()
	p1 := synthesizePortfolioForEnv(env1, protocol.IntentMinimumSpend)

	t.Run("SemanticDiffing_BootstrapAndModifications", func(t *testing.T) {
		// Zero-base diff (initial bootstrap)
		diffBootstrap, err := cognition.DiffPortfolios(nil, p1)
		if err != nil {
			t.Fatalf("DiffPortfolios bootstrap failed: %v", err)
		}
		if !diffBootstrap.HasChanges {
			t.Errorf("expected bootstrap diff to have changes")
		}
		if len(diffBootstrap.ChannelDiffs) != len(p1.Channels) {
			t.Errorf("expected %d channel additions, got %d", len(p1.Channels), len(diffBootstrap.ChannelDiffs))
		}
		for _, d := range diffBootstrap.ChannelDiffs {
			if d.Delta != cognition.DeltaAdded {
				t.Errorf("expected DeltaAdded for bootstrap channel, got %s", d.Delta)
			}
		}

		// Incremental diff: modify concurrency and add new role binding
		p2 := *p1
		p2.PortfolioID = "port-apple-mlx-v2"
		p2.Revision = 2
		p2.Channels = make([]protocol.AccessChannel, len(p1.Channels))
		copy(p2.Channels, p1.Channels)
		p2.Channels[0].MaxConcurrentRequests = 8 // modified
		p2.RoleBindings = append(p2.RoleBindings, protocol.RoleBinding{
			Role:             "scout",
			EndpointID:       p1.RoleBindings[0].EndpointID,
			ChannelID:        p1.RoleBindings[0].ChannelID,
			BudgetPoolID:     p1.RoleBindings[0].BudgetPoolID,
			ContextProfileID: p1.RoleBindings[0].ContextProfileID,
			Priority:         2,
		})

		diffMod, err := cognition.DiffPortfolios(p1, &p2)
		if err != nil {
			t.Fatalf("DiffPortfolios mod failed: %v", err)
		}
		if !diffMod.HasChanges {
			t.Errorf("expected changes in modified diff")
		}
		if len(diffMod.ChannelDiffs) != 1 || diffMod.ChannelDiffs[0].Delta != cognition.DeltaModified {
			t.Errorf("expected 1 modified channel, got %+v", diffMod.ChannelDiffs)
		}
		if len(diffMod.RoleBindingDiffs) != 1 || diffMod.RoleBindingDiffs[0].Delta != cognition.DeltaAdded {
			t.Errorf("expected 1 added role binding, got %+v", diffMod.RoleBindingDiffs)
		}

		// Idempotent diff: identical portfolios produce HasChanges: false
		diffSame, err := cognition.DiffPortfolios(p1, p1)
		if err != nil {
			t.Fatalf("DiffPortfolios identical failed: %v", err)
		}
		if diffSame.HasChanges {
			t.Errorf("expected identical portfolios to have HasChanges: false")
		}
	})

	t.Run("ProposalFormulation_Constraints", func(t *testing.T) {
		now := clk.Now()

		// Blank rationale rejected
		_, err := cognition.CreateChangeProposal(cognition.TriggerResourceChange, "  ", nil, *p1, now)
		if err == nil {
			t.Fatalf("expected error for blank rationale")
		}
		if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
			t.Errorf("expected CategoryInvalidArgument, got %v", err)
		}

		// Invalid candidate portfolio rejected
		invalidP := *p1
		invalidP.PortfolioID = ""
		_, err = cognition.CreateChangeProposal(cognition.TriggerResourceChange, "valid rationale", nil, invalidP, now)
		if err == nil {
			t.Fatalf("expected error for invalid candidate portfolio")
		}

		// Valid proposals across triggers
		validTriggers := []cognition.ChangeTrigger{
			cognition.TriggerResourceChange,
			cognition.TriggerManualProposal,
			cognition.TriggerPolicyUpdate,
		}
		for _, tr := range validTriggers {
			prop, err := cognition.CreateChangeProposal(tr, "Switching configuration", nil, *p1, now)
			if err != nil {
				t.Errorf("CreateChangeProposal failed for trigger %s: %v", tr, err)
			}
			if prop.Trigger != tr {
				t.Errorf("expected trigger %s, got %s", tr, prop.Trigger)
			}
		}
	})

	t.Run("ActivationLineage_AtomicityAndCrashConsistency", func(t *testing.T) {
		stateDir := t.TempDir()
		mgr, err := cognition.NewActivationManager(stateDir, nil, clk)
		if err != nil {
			t.Fatalf("NewActivationManager: %v", err)
		}
		service := cognition.NewAdaptationService(mgr)

		valInput := cognition.ValidationInput{
			MachineProfile:  env1.machineProfile,
			Inventory:       env1.inventory,
			ContextProfiles: env1.contextProfiles,
			BudgetStates:    env1.budgetStates,
			Policy:          ptr(env1.validationPol),
			Clock:           clk,
		}

		// 1. Initial activation via proposal
		prop1, err := cognition.CreateChangeProposal(cognition.TriggerManualProposal, "Initial bootstrap", nil, *p1, clk.Now())
		if err != nil {
			t.Fatalf("CreateChangeProposal: %v", err)
		}
		rec1, err := service.ProposeAndActivate(ctx, prop1, valInput)
		if err != nil {
			t.Fatalf("ProposeAndActivate v1: %v", err)
		}
		if rec1.Sequence != 1 {
			t.Errorf("expected sequence 1, got %d", rec1.Sequence)
		}
		if rec1.PreviousActivationID != "" {
			t.Errorf("expected empty PreviousActivationID for initial activation")
		}

		// Verify files written to disk atomically
		activePath := filepath.Join(stateDir, cognition.ActivePortfolioFileName)
		lineagePath := filepath.Join(stateDir, cognition.LineageFileName)
		if _, err := os.Stat(activePath); err != nil {
			t.Errorf("active portfolio file not found on disk: %v", err)
		}
		if _, err := os.Stat(lineagePath); err != nil {
			t.Errorf("lineage file not found on disk: %v", err)
		}

		// 2. Second activation
		p2 := *p1
		p2.PortfolioID = "port-apple-mlx-v2"
		p2.Revision = 2
		prop2, err := cognition.CreateChangeProposal(cognition.TriggerResourceChange, "Update to v2", p1, p2, clk.Now())
		if err != nil {
			t.Fatalf("CreateChangeProposal v2: %v", err)
		}
		rec2, err := service.ProposeAndActivate(ctx, prop2, valInput)
		if err != nil {
			t.Fatalf("ProposeAndActivate v2: %v", err)
		}
		if rec2.Sequence != 2 {
			t.Errorf("expected sequence 2, got %d", rec2.Sequence)
		}
		if rec2.PreviousActivationID != rec1.ActivationID {
			t.Errorf("expected previous activation %s, got %s", rec1.ActivationID, rec2.PreviousActivationID)
		}

		// 3. Stale base conflict rejection (INV-04)
		p3 := *p1
		p3.PortfolioID = "port-apple-mlx-v3"
		p3.Revision = 3
		staleBase := *p1 // base is still p1, but active portfolio is now p2!
		propConflict, err := cognition.CreateChangeProposal(cognition.TriggerResourceChange, "Stale proposal", &staleBase, p3, clk.Now())
		if err != nil {
			t.Fatalf("CreateChangeProposal conflict: %v", err)
		}
		_, err = service.ProposeAndActivate(ctx, propConflict, valInput)
		if err == nil {
			t.Fatalf("expected conflict error when proposal base does not match active portfolio")
		}
		if errs.CategoryOf(err) != errs.CategoryConflict {
			t.Errorf("expected CategoryConflict, got %v", err)
		}

		// Verify active portfolio untouched
		activeP, _, err := mgr.GetActivePortfolio(ctx)
		if err != nil {
			t.Fatalf("GetActivePortfolio: %v", err)
		}
		if activeP.PortfolioID != p2.PortfolioID {
			t.Errorf("expected active portfolio to remain %s, got %s", p2.PortfolioID, activeP.PortfolioID)
		}
	})
}

// 4. Revalidation-Gated Rollback Under Changing Machine Conditions (INV-03, DCI-124).
func TestM3DCrossPortfolio_RevalidationGatedRollback(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFake(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC), time.Second)

	env := createMultiSubscriptionEnv()
	p1 := synthesizePortfolioForEnv(env, protocol.IntentMinimumSpend)
	p1.PortfolioID = "port-multisub-v1"

	p2 := synthesizePortfolioForEnv(env, protocol.IntentBalanced)
	p2.PortfolioID = "port-multisub-v2"
	p2.Revision = 2

	t.Run("Rollback_HappyPath", func(t *testing.T) {
		stateDir := t.TempDir()
		mgr, err := cognition.NewActivationManager(stateDir, nil, clk)
		if err != nil {
			t.Fatalf("NewActivationManager: %v", err)
		}
		service := cognition.NewAdaptationService(mgr)

		valInput := cognition.ValidationInput{
			MachineProfile:  env.machineProfile,
			Inventory:       env.inventory,
			ContextProfiles: env.contextProfiles,
			BudgetStates:    env.budgetStates,
			Policy:          ptr(env.validationPol),
			Clock:           clk,
		}

		// Activate p1 then p2
		prop1, _ := cognition.CreateChangeProposal(cognition.TriggerManualProposal, "v1", nil, *p1, clk.Now())
		rec1, err := service.ProposeAndActivate(ctx, prop1, valInput)
		if err != nil {
			t.Fatalf("activate p1: %v", err)
		}

		prop2, _ := cognition.CreateChangeProposal(cognition.TriggerResourceChange, "v2", p1, *p2, clk.Now())
		_, err = service.ProposeAndActivate(ctx, prop2, valInput)
		if err != nil {
			t.Fatalf("activate p2: %v", err)
		}

		// Rollback to v1 (empty string target restores previous activation)
		recRollback, err := service.Rollback(ctx, "", &valInput)
		if err != nil {
			t.Fatalf("Rollback failed: %v", err)
		}
		if !recRollback.IsRollback {
			t.Errorf("expected IsRollback: true")
		}
		if recRollback.RollbackTargetActivationID != rec1.ActivationID {
			t.Errorf("expected target activation %s, got %s", rec1.ActivationID, recRollback.RollbackTargetActivationID)
		}
		if recRollback.Sequence != 3 {
			t.Errorf("expected sequence 3, got %d", recRollback.Sequence)
		}
		if recRollback.PortfolioID != p1.PortfolioID {
			t.Errorf("expected active portfolio to be restored to %s, got %s", p1.PortfolioID, recRollback.PortfolioID)
		}
	})

	t.Run("Rollback_FailsClosed_HardwareOrEndpointLost", func(t *testing.T) {
		stateDir := t.TempDir()
		mgr, err := cognition.NewActivationManager(stateDir, nil, clk)
		if err != nil {
			t.Fatalf("NewActivationManager: %v", err)
		}
		service := cognition.NewAdaptationService(mgr)

		valInput := cognition.ValidationInput{
			MachineProfile:  env.machineProfile,
			Inventory:       env.inventory,
			ContextProfiles: env.contextProfiles,
			BudgetStates:    env.budgetStates,
			Policy:          ptr(env.validationPol),
			Clock:           clk,
		}

		prop1, _ := cognition.CreateChangeProposal(cognition.TriggerManualProposal, "v1", nil, *p1, clk.Now())
		_, err = service.ProposeAndActivate(ctx, prop1, valInput)
		if err != nil {
			t.Fatalf("activate p1: %v", err)
		}

		prop2, _ := cognition.CreateChangeProposal(cognition.TriggerResourceChange, "v2", p1, *p2, clk.Now())
		_, err = service.ProposeAndActivate(ctx, prop2, valInput)
		if err != nil {
			t.Fatalf("activate p2: %v", err)
		}

		// Simulate machine hardware change / endpoint loss:
		// Required endpoint "ep-claude-cli" is removed from inventory and machine profile!
		brokenInventory := *env.inventory
		brokenInventory.CognitionEndpoints = nil
		brokenProfile := *env.machineProfile
		brokenProfile.Endpoints = nil

		revalInput := cognition.ValidationInput{
			MachineProfile:  &brokenProfile,
			Inventory:       &brokenInventory,
			ContextProfiles: env.contextProfiles,
			BudgetStates:    env.budgetStates,
			Policy:          ptr(env.validationPol),
			Clock:           clk,
		}

		// Rollback MUST fail closed because target portfolio references an endpoint that no longer exists
		_, err = service.Rollback(ctx, "", &revalInput)
		if err == nil {
			t.Fatalf("expected rollback to fail when target portfolio endpoints are missing")
		}
		if errs.CategoryOf(err) != errs.CategoryValidationFailed {
			t.Errorf("expected CategoryValidationFailed, got %v", err)
		}

		// Ensure active portfolio was NOT mutated (INV-03)
		activeP, _, err := mgr.GetActivePortfolio(ctx)
		if err != nil {
			t.Fatalf("GetActivePortfolio: %v", err)
		}
		if activeP.PortfolioID != p2.PortfolioID {
			t.Errorf("expected active portfolio to remain untouched at %s, got %s", p2.PortfolioID, activeP.PortfolioID)
		}
	})

	t.Run("Rollback_FailsClosed_BudgetExhaustedOrUnknown", func(t *testing.T) {
		stateDir := t.TempDir()
		mgr, err := cognition.NewActivationManager(stateDir, nil, clk)
		if err != nil {
			t.Fatalf("NewActivationManager: %v", err)
		}
		service := cognition.NewAdaptationService(mgr)

		valInput := cognition.ValidationInput{
			MachineProfile:  env.machineProfile,
			Inventory:       env.inventory,
			ContextProfiles: env.contextProfiles,
			BudgetStates:    env.budgetStates,
			Policy:          ptr(env.validationPol),
			Clock:           clk,
		}

		prop1, _ := cognition.CreateChangeProposal(cognition.TriggerManualProposal, "v1", nil, *p1, clk.Now())
		_, err = service.ProposeAndActivate(ctx, prop1, valInput)
		if err != nil {
			t.Fatalf("activate p1: %v", err)
		}

		prop2, _ := cognition.CreateChangeProposal(cognition.TriggerResourceChange, "v2", p1, *p2, clk.Now())
		_, err = service.ProposeAndActivate(ctx, prop2, valInput)
		if err != nil {
			t.Fatalf("activate p2: %v", err)
		}

		// Corrupt budget state: budget pool required by p1 has status: unknown
		brokenBudgetStates := map[string]*protocol.BudgetState{
			p1.BudgetPools[0].PoolID: {
				SchemaVersion: protocol.SchemaVersion1,
				PoolID:        p1.BudgetPools[0].PoolID,
				Status:        protocol.BudgetStatusUnknown, // unknown
				ObservedAt:    "2026-10-04T12:00:00Z",
			},
		}

		revalInput := cognition.ValidationInput{
			MachineProfile:  env.machineProfile,
			Inventory:       env.inventory,
			ContextProfiles: env.contextProfiles,
			BudgetStates:    brokenBudgetStates,
			Policy:          ptr(env.validationPol),
			Clock:           clk,
		}

		_, err = service.Rollback(ctx, "", &revalInput)
		if err == nil {
			t.Fatalf("expected rollback to fail when budget state is unknown")
		}
		if errs.CategoryOf(err) != errs.CategoryValidationFailed {
			t.Errorf("expected CategoryValidationFailed, got %v", err)
		}

		// Ensure active portfolio untouched
		activeP, _, err := mgr.GetActivePortfolio(ctx)
		if err != nil {
			t.Fatalf("GetActivePortfolio: %v", err)
		}
		if activeP.PortfolioID != p2.PortfolioID {
			t.Errorf("expected active portfolio to remain untouched at %s, got %s", p2.PortfolioID, activeP.PortfolioID)
		}
	})
}

// 5. Full End-to-End Portfolio Lifecycle Scenario.
func TestM3DCrossPortfolio_EndToEndLifecycle(t *testing.T) {
	schemas, err := schema.Default()
	if err != nil {
		t.Fatalf("schema.Default: %v", err)
	}

	ctx := context.Background()
	clk := clock.NewFake(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC), time.Second)
	stateDir := t.TempDir()

	env := createMixedEnv(true)

	// Step 1: Synthesize recommendations across all 4 intents
	allIntents := []protocol.RecommendationIntent{
		protocol.IntentMinimumSpend,
		protocol.IntentBalanced,
		protocol.IntentMaximumQualityWithinPolicy,
		protocol.IntentPrivacyFirst,
	}

	outputJSON := buildPlannerOutputJSON(allIntents, func(intent protocol.RecommendationIntent) *protocol.CognitionPortfolio {
		return synthesizePortfolioForEnv(env, intent)
	})

	driver := &m3dMockDriver{
		id:   "drv-" + env.inventory.CognitionEndpoints[0].ID,
		caps: drivers.DriverCapabilities{Kind: protocol.ChannelLocalDaemonSocket, SessionMode: protocol.SessionStatelessPerCall, ContextControl: protocol.ContextControlExactStateless, PrefixCache: protocol.PrefixCacheSessionKV, MaxConcurrentRequests: 2, NativeWorktreeAccess: false, SupportsStreaming: true},
		handler: func(ctx context.Context, input drivers.TurnInput) (drivers.TurnResult, error) {
			return drivers.TurnResult{TurnID: input.TurnID, Content: outputJSON}, nil
		},
	}

	resolver := &m3dDriverResolver{
		drivers: map[string]drivers.SessionDriver{env.inventory.CognitionEndpoints[0].ID: driver},
		models:  map[string]string{env.inventory.CognitionEndpoints[0].ID: "test-model"},
	}

	req := planner.Request{
		Inventory:       env.inventory,
		MachineProfile:  env.machineProfile,
		ContextProfiles: env.contextProfiles,
		BudgetStates:    env.budgetStates,
		Policy:          &env.validationPol,
		Project: planner.ProjectCharacteristics{
			Languages: []string{"go"},
			RiskTags:  []string{"high-risk"},
		},
		Intents: allIntents,
		Clock:   clk,
	}

	planResult, err := plannerdriver.ExecutePlanning(ctx, req, resolver, plannerdriver.ExecutionConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("step 1 ExecutePlanning failed: %v", err)
	}
	if planResult.Outcome != planner.OutcomeRecommended || len(planResult.Accepted) != 4 {
		t.Fatalf("step 1 expected 4 recommendations, got outcome %s, count %d", planResult.Outcome, len(planResult.Accepted))
	}

	// Step 2: User selects Balanced candidate portfolio and activates it via AdaptationService
	selectedRec := planResult.Accepted[1] // Balanced
	targetPortfolio := selectedRec.RecommendedPortfolio

	mgr, err := cognition.NewActivationManager(stateDir, nil, clk)
	if err != nil {
		t.Fatalf("NewActivationManager: %v", err)
	}
	service := cognition.NewAdaptationService(mgr)

	valInput := cognition.ValidationInput{
		MachineProfile:  env.machineProfile,
		Inventory:       env.inventory,
		ContextProfiles: env.contextProfiles,
		BudgetStates:    env.budgetStates,
		Policy:          ptr(env.validationPol),
		Clock:           clk,
	}

	prop1, err := cognition.CreateChangeProposal(cognition.TriggerManualProposal, selectedRec.Rationale, nil, targetPortfolio, clk.Now())
	if err != nil {
		t.Fatalf("step 2 CreateChangeProposal: %v", err)
	}

	rec1, err := service.ProposeAndActivate(ctx, prop1, valInput)
	if err != nil {
		t.Fatalf("step 2 ProposeAndActivate: %v", err)
	}
	if rec1.Sequence != 1 {
		t.Errorf("step 2 expected sequence 1, got %d", rec1.Sequence)
	}

	// Step 3: High-risk task arrives -> model-assisted workflow planning
	highRiskTask := workflowplanner.TaskSpec{
		TaskID:             "task-m3d-e2e-01",
		WorkPackageID:      "WP-M3D-5",
		RiskTags:           []string{"security"},
		RequiresDualReview: true,
	}

	mockWorkflowInvoker := &m3dWorkflowMockInvoker{
		invokeFn: func(ctx context.Context, inv planner.Invocation) (planner.InvocationResult, error) {
			output := `{
				"topology": "dual_independent_review",
				"stages": [
					{
						"stage_id": "s-impl",
						"role": "implementer",
						"order": 1,
						"budget_pool_id": "pool-ep-mlx-01",
						"endpoint_id": "ep-mlx-01",
						"channel_id": "chan-ep-mlx-01",
						"context_profile_id": "prof-ep-mlx-01",
						"timeout_seconds": 600
					},
					{
						"stage_id": "s-rev1",
						"role": "reviewer",
						"is_review": true,
						"order": 2,
						"depends_on": ["s-impl"],
						"budget_pool_id": "pool-ep-mlx-01",
						"endpoint_id": "ep-mlx-01",
						"channel_id": "chan-ep-mlx-01",
						"context_profile_id": "prof-ep-mlx-01",
						"timeout_seconds": 300
					},
					{
						"stage_id": "s-rev2",
						"role": "reviewer",
						"is_review": true,
						"order": 3,
						"depends_on": ["s-impl"],
						"budget_pool_id": "pool-ep-claude-cli",
						"endpoint_id": "ep-claude-cli",
						"channel_id": "chan-ep-claude-cli",
						"context_profile_id": "prof-ep-claude-cli",
						"timeout_seconds": 300
					}
				]
			}`
			return planner.InvocationResult{Content: output}, nil
		},
	}

	modelPlanRes, err := workflowplanner.PlanWorkflowWithModel(ctx, workflowplanner.ModelPlanRequest{
		Task:         highRiskTask,
		Portfolio:    &targetPortfolio,
		BudgetStates: env.budgetStates,
		Invoker:      mockWorkflowInvoker,
	})
	if err != nil {
		t.Fatalf("step 3 PlanWorkflowWithModel: %v", err)
	}
	if !modelPlanRes.UsedModel {
		t.Errorf("step 3 expected UsedModel: true, fallback: %s", modelPlanRes.FallbackReason)
	}

	// Validate synthesized plan with schema
	planBytes, err := json.Marshal(modelPlanRes.Plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	if err := schemas.ValidateBytes(schema.Name("workflow-plan"), planBytes); err != nil {
		t.Errorf("step 3 workflow plan schema validation failed: %v", err)
	}

	// Step 4: Machine resources update -> upgrade portfolio to v2
	portfolioV2 := targetPortfolio
	portfolioV2.PortfolioID = "port-mixed-v2"
	portfolioV2.Revision = 2
	portfolioV2.Channels[0].MaxConcurrentRequests = 4

	prop2, err := cognition.CreateChangeProposal(cognition.TriggerResourceChange, "Upgrade concurrency limits", &targetPortfolio, portfolioV2, clk.Now())
	if err != nil {
		t.Fatalf("step 4 CreateChangeProposal: %v", err)
	}
	rec2, err := service.ProposeAndActivate(ctx, prop2, valInput)
	if err != nil {
		t.Fatalf("step 4 ProposeAndActivate: %v", err)
	}
	if rec2.Sequence != 2 {
		t.Errorf("step 4 expected sequence 2, got %d", rec2.Sequence)
	}

	// Step 5: Deterministic-only task arrives -> pure deterministic workflow plan
	detTask := workflowplanner.TaskSpec{
		TaskID:            "task-m3d-e2e-det",
		WorkPackageID:     "WP-M3D-5",
		DeterministicOnly: true,
	}
	detPlanRes, err := workflowplanner.PlanWorkflow(workflowplanner.PlanRequest{
		Task:      detTask,
		Portfolio: &portfolioV2,
	})
	if err != nil {
		t.Fatalf("step 5 PlanWorkflow deterministic: %v", err)
	}
	if detPlanRes.Plan.Topology != protocol.TopologyDeterministicOnly {
		t.Errorf("step 5 expected DeterministicOnly, got %s", detPlanRes.Plan.Topology)
	}

	// Step 6: Rollback v2 -> v1 with revalidation gate
	recRollback, err := service.Rollback(ctx, rec1.ActivationID, &valInput)
	if err != nil {
		t.Fatalf("step 6 Rollback failed: %v", err)
	}
	if !recRollback.IsRollback || recRollback.Sequence != 3 {
		t.Errorf("step 6 expected rollback sequence 3, got %+v", recRollback)
	}
	if recRollback.PortfolioID != targetPortfolio.PortfolioID {
		t.Errorf("step 6 expected restored portfolio ID %s, got %s", targetPortfolio.PortfolioID, recRollback.PortfolioID)
	}

	// Step 7: Verify all protocol schemas validate and invariants INV-01 through INV-04 held
	activePortfolio, currentRec, err := mgr.GetActivePortfolio(ctx)
	if err != nil {
		t.Fatalf("step 7 GetActivePortfolio: %v", err)
	}
	if activePortfolio.PortfolioID != targetPortfolio.PortfolioID {
		t.Errorf("step 7 active portfolio mismatch: got %s, want %s", activePortfolio.PortfolioID, targetPortfolio.PortfolioID)
	}
	if currentRec == nil || !currentRec.IsRollback {
		t.Errorf("step 7 expected active record to be rollback: %+v", currentRec)
	}

	history, err := mgr.GetActivationHistory(ctx)
	if err != nil {
		t.Fatalf("step 7 GetActivationHistory: %v", err)
	}
	if len(history) != 3 {
		t.Errorf("step 7 expected 3 activation history records, got %d", len(history))
	}

	activeBytes, err := json.Marshal(activePortfolio)
	if err != nil {
		t.Fatalf("marshal active portfolio: %v", err)
	}
	if err := schemas.ValidateBytes(schema.Name("cognition-portfolio"), activeBytes); err != nil {
		t.Errorf("step 7 cognition-portfolio schema validation failed: %v", err)
	}
}
