package compiler

import (
	"fmt"
	"math"
	"strings"
	"unicode"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// DefaultProvisionalProfile synthesizes a valid provisional ContextProfile adhering to
// all protocol constraints in internal/protocol/context.go and PROTOCOLS §10B.
// It reflects conservative default reserves and token limits before M4 empirical calibration.
func DefaultProvisionalProfile(endpointID, channelID, modelRef string, runtimeWindow int) *protocol.ContextProfile {
	if runtimeWindow <= 0 {
		runtimeWindow = 32768
	}
	outputReserve := 4096
	toolTailReserve := 2048
	if runtimeWindow < (outputReserve + toolTailReserve + 4096) {
		outputReserve = runtimeWindow / 6
		toolTailReserve = runtimeWindow / 10
	}
	hardCeiling := runtimeWindow - outputReserve - toolTailReserve
	targetResident := int(float64(hardCeiling) * 0.75)
	if targetResident < 1 {
		targetResident = 1
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
				EffectiveTokens: targetResident,
				CalibrationTask: "provisional_conservative_allocation",
				CalibrationDate: "2026-10-02T00:00:00Z",
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
		AccountingMethod:          protocol.AccountingExactBPE,
		EstimateUncertaintyRatio:  0.05,
		ObservedContextControl:    protocol.ContextControlExactStateless,
		ObservedPrefixCache:       protocol.PrefixCacheSessionKV,
	}
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

// EstimateTokens provides a deterministic token count estimate for text based on the
// accounting method and conservative uncertainty margin.
func EstimateTokens(text string, method protocol.TokenizerAccountingMethod, uncertainty float64) int {
	if text == "" {
		return 0
	}
	if uncertainty < 0.0 {
		uncertainty = 0.0
	}

	var baseTokens float64
	switch method {
	case protocol.AccountingExactBPE:
		// Exact BPE approximation: blend of character ratio and word tokenization.
		// Standard English/code averages ~3.6 - 4.0 characters per token.
		charCount := len(text)
		words := countWords(text)
		// Code and punctuation often create more tokens than simple whitespace word splitting.
		// Formula blends (charCount / 3.7) with (wordCount * 1.3)
		byChars := float64(charCount) / 3.7
		byWords := float64(words) * 1.3
		baseTokens = math.Max(byChars, byWords)
	case protocol.AccountingProviderAPI:
		// Provider API estimate: slightly more conservative
		baseTokens = float64(len(text)) / 3.5
	case protocol.AccountingApproximateEstimate:
		fallthrough
	default:
		// Approximate estimate: 4 chars/token
		baseTokens = float64(len(text)) / 4.0
	}

	// Apply conservative uncertainty margin (PROTOCOLS §10B)
	withMargin := baseTokens * (1.0 + uncertainty)
	estimated := int(math.Ceil(withMargin))
	if estimated < 1 {
		estimated = 1
	}
	return estimated
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
