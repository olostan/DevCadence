package compiler

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// DefaultProvisionalProfile synthesizes a valid provisional ContextProfile adhering to
// all protocol constraints in internal/protocol/context.go and PROTOCOLS §10B.
// It reflects conservative default reserves and token limits before M4 empirical calibration.
// Epistemic Honesty (Finding 6, PROTOCOLS §10B):
//   - Requires explicit positive runtimeWindow; does NOT manufacture unobserved window capacity.
//   - CalibrationDate defaults to current UTC timestamp if not explicitly provided.
//   - Observed capabilities default to protocol.ContextControlUnknown and protocol.PrefixCacheUnknown.
//   - TargetResidentTokens provides provisional soft packing guidance (ADR-0020 §2); hard ceiling is fail-closed.
func DefaultProvisionalProfile(endpointID, channelID, modelRef string, runtimeWindow int) (*protocol.ContextProfile, error) {
	return DefaultProvisionalProfileWithCapabilities(
		endpointID, channelID, modelRef, runtimeWindow, 0,
		protocol.ContextControlUnknown, protocol.PrefixCacheUnknown, "",
	)
}

// MustDefaultProvisionalProfile is a convenience helper for tests and callers with statically known valid parameters.
// It panics if runtimeWindow <= 0.
func MustDefaultProvisionalProfile(endpointID, channelID, modelRef string, runtimeWindow int) *protocol.ContextProfile {
	prof, err := DefaultProvisionalProfile(endpointID, channelID, modelRef, runtimeWindow)
	if err != nil {
		panic(err)
	}
	return prof
}

// DefaultProvisionalProfileWithCapabilities creates a provisional profile with explicit observed capabilities.
func DefaultProvisionalProfileWithCapabilities(
	endpointID, channelID, modelRef string,
	runtimeWindow int,
	targetResident int,
	ctrl protocol.ContextControl,
	cache protocol.PrefixCache,
	calibrationDate string,
) (*protocol.ContextProfile, error) {
	if runtimeWindow <= 0 {
		return nil, errs.New(errs.CategoryInvalidArgument, "DefaultProvisionalProfile: runtimeWindow must be > 0, got %d", runtimeWindow)
	}
	if strings.TrimSpace(calibrationDate) == "" {
		calibrationDate = time.Now().UTC().Format(time.RFC3339)
	}

	outputReserve := 4096
	toolTailReserve := 2048
	if runtimeWindow < (outputReserve + toolTailReserve + 4096) {
		outputReserve = runtimeWindow / 6
		toolTailReserve = runtimeWindow / 10
	}
	hardCeiling := runtimeWindow - outputReserve - toolTailReserve
	// Target residency is soft packing and eviction guidance, not a universal fixed percentage.
	// Target residency must come from explicit policy configuration; if unconfigured/0, leave it as 0/unconfigured (ADR-0020 §2).
	if targetResident < 0 {
		targetResident = 0
	} else if targetResident > hardCeiling {
		targetResident = hardCeiling
	}

	if !ctrl.Valid() {
		ctrl = protocol.ContextControlUnknown
	}
	if !cache.Valid() {
		cache = protocol.PrefixCacheUnknown
	}

	effectiveTokens := targetResident
	if effectiveTokens < 1 {
		effectiveTokens = hardCeiling
	}

	return &protocol.ContextProfile{
		SchemaVersion:        protocol.SchemaVersion1,
		ProfileID:            fmt.Sprintf("prof-prov-%s", endpointID),
		EndpointID:           endpointID,
		ChannelID:            channelID,
		Runtime:              "provisional",
		ModelRef:             modelRef,
		Revision:             1,
		DeclaredWindowTokens: runtimeWindow,
		RuntimeWindowTokens:  runtimeWindow,
		WorkloadEnvelopes: []protocol.WorkloadEnvelope{
			{
				Workload:        protocol.WorkloadImplementation,
				EffectiveTokens: effectiveTokens,
				CalibrationTask: "provisional_conservative_allocation",
				CalibrationDate: calibrationDate,
				ConfidenceLevel: "provisional",
			},
		},
		TargetResidentTokens:      targetResident,
		HardResidentCeilingTokens: hardCeiling,
		ProtectedCoreLimitTokens:  hardCeiling / 4,
		ContractLimitTokens:       hardCeiling / 3,
		MaxSingleLeaseTokens:      hardCeiling / 3,
		OutputReserveTokens:       outputReserve,
		ToolTailReserveTokens:     toolTailReserve,
		AccountingMethod:          protocol.AccountingApproximateEstimate,
		EstimateUncertaintyRatio:  0.05,
		ObservedContextControl:    ctrl,
		ObservedPrefixCache:       cache,
	}, nil
}

// EnforceProfileBounds enforces that a ContextPack strictly fits the provided ContextProfile.
// If any hard limit is exceeded, it returns errs.CategoryContextUnfit (ErrContextUnfit).
// Crucially, it MUST NEVER silently truncate or drop mandatory obligations (DCI-019).
func EnforceProfileBounds(pack *protocol.ContextPack, profile *protocol.ContextProfile) error {
	const kind = "ContextProfileEnforcement"
	if pack == nil {
		return errs.New(errs.CategoryInvalidArgument, "%s: pack cannot be nil", kind)
	}
	if profile == nil {
		return errs.New(errs.CategoryInvalidArgument, "%s: profile cannot be nil", kind)
	}
	if err := profile.Validate(); err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "%s: invalid profile", kind)
	}

	breakdown := pack.TokenAccounting
	totalResident := breakdown.TotalResidentTokens

	// 1. Hard runtime window constraint: resident tokens + output reserve + tool tail reserve
	totalRequired := totalResident + profile.OutputReserveTokens + profile.ToolTailReserveTokens
	if totalRequired > profile.RuntimeWindowTokens {
		return errs.New(errs.CategoryContextUnfit,
			"%s: required window tokens (%d = resident %d + output %d + tool tail %d) exceeds runtime window (%d)",
			kind, totalRequired, totalResident, profile.OutputReserveTokens, profile.ToolTailReserveTokens, profile.RuntimeWindowTokens)
	}

	// 2. Hard resident ceiling constraint
	if totalResident > profile.HardResidentCeilingTokens {
		return errs.New(errs.CategoryContextUnfit,
			"%s: total resident tokens (%d) exceeds hard resident ceiling (%d)",
			kind, totalResident, profile.HardResidentCeilingTokens)
	}

	// 3. Contract limit constraint
	if profile.ContractLimitTokens > 0 && breakdown.ContractTokens > profile.ContractLimitTokens {
		return errs.New(errs.CategoryContextUnfit,
			"%s: contract tokens (%d) exceeds profile contract limit (%d)",
			kind, breakdown.ContractTokens, profile.ContractLimitTokens)
	}

	// 4. Protected core limit constraint (Role tokens + Normative mandatory tokens)
	protectedCoreTokens := breakdown.RoleTokens + breakdown.NormativeTokens
	if profile.ProtectedCoreLimitTokens > 0 && protectedCoreTokens > profile.ProtectedCoreLimitTokens {
		return errs.New(errs.CategoryContextUnfit,
			"%s: protected core tokens (%d = role %d + normative %d) exceeds profile protected core limit (%d)",
			kind, protectedCoreTokens, breakdown.RoleTokens, breakdown.NormativeTokens, profile.ProtectedCoreLimitTokens)
	}

	// 5. Max single evidence lease constraint
	if profile.MaxSingleLeaseTokens > 0 {
		for i, lease := range pack.EvidenceWorkingSet {
			if lease.TokenCount > profile.MaxSingleLeaseTokens {
				return errs.New(errs.CategoryContextUnfit,
					"%s: evidence lease %q (index %d) token count (%d) exceeds max single lease limit (%d)",
					kind, lease.LeaseID, i, lease.TokenCount, profile.MaxSingleLeaseTokens)
			}
		}
	}

	return nil
}

// EnforceProjectionBounds checks that a rendered PromptProjection (including system prompt,
// user prompt, and any additional overhead like tool schemas and host framing) strictly fits
// within the hard limits of the ContextProfile (Finding 4, PROTOCOLS §10B).
func EnforceProjectionBounds(proj PromptProjection, profile *protocol.ContextProfile, additionalTokens int) error {
	const kind = "PromptProjectionBoundsEnforcement"
	if profile == nil {
		return errs.New(errs.CategoryInvalidArgument, "%s: profile cannot be nil", kind)
	}
	if err := profile.Validate(); err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "%s: invalid profile", kind)
	}

	method := profile.AccountingMethod
	uncertainty := profile.EstimateUncertaintyRatio

	sysTokens, err := EstimateTokens(proj.SystemPrompt, method, uncertainty)
	if err != nil {
		return err
	}
	userTokens, err := EstimateTokens(proj.UserPrompt, method, uncertainty)
	if err != nil {
		return err
	}
	residentTokens := sysTokens + userTokens + additionalTokens

	totalRequired := residentTokens + profile.OutputReserveTokens + profile.ToolTailReserveTokens
	if totalRequired > profile.RuntimeWindowTokens {
		return errs.New(errs.CategoryContextUnfit,
			"%s: required projection window tokens (%d = resident %d + output %d + tool tail %d) exceeds runtime window (%d)",
			kind, totalRequired, residentTokens, profile.OutputReserveTokens, profile.ToolTailReserveTokens, profile.RuntimeWindowTokens)
	}

	if residentTokens > profile.HardResidentCeilingTokens {
		return errs.New(errs.CategoryContextUnfit,
			"%s: total projection resident tokens (%d) exceeds hard resident ceiling (%d)",
			kind, residentTokens, profile.HardResidentCeilingTokens)
	}

	return nil
}

// EstimateTokens provides a deterministic token count estimate for text based on the
// accounting method and conservative uncertainty margin.
// Epistemic Honesty (Finding 3, PROTOCOLS §10B):
// Reject specifying AccountingExactBPE or AccountingProviderAPI when using heuristic character ratios.
// If a real model tokenizer or provider API counter is not attached, the method must strictly be
// AccountingApproximateEstimate.
func EstimateTokens(text string, method protocol.TokenizerAccountingMethod, uncertainty float64) (int, error) {
	if text == "" {
		return 0, nil
	}
	if method == protocol.AccountingExactBPE || method == protocol.AccountingProviderAPI {
		return 0, errs.New(errs.CategoryInvalidArgument,
			"heuristic token estimation cannot claim %q without a verified tokenizer or provider API counter; use %q",
			method, protocol.AccountingApproximateEstimate)
	}
	if method != protocol.AccountingApproximateEstimate {
		return 0, errs.New(errs.CategoryInvalidArgument, "unsupported token accounting method %q", method)
	}
	if uncertainty < 0.0 {
		uncertainty = 0.0
	}

	// Approximate estimate: 4 chars/token
	baseTokens := float64(len(text)) / 4.0

	// Apply conservative uncertainty margin (PROTOCOLS §10B)
	withMargin := baseTokens * (1.0 + uncertainty)
	estimated := int(math.Ceil(withMargin))
	if estimated < 1 {
		estimated = 1
	}
	return estimated, nil
}

// EstimateTokensApprox provides token estimation strictly using AccountingApproximateEstimate.
func EstimateTokensApprox(text string, uncertainty float64) int {
	count, _ := EstimateTokens(text, protocol.AccountingApproximateEstimate, uncertainty)
	return count
}

func countWords(s string) int {
	words := 0
	inWord := false
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			inWord = false
		} else if !inWord {
			inWord = true
			words++
		}
	}
	if words == 0 && strings.TrimSpace(s) != "" {
		return 1
	}
	return words
}
