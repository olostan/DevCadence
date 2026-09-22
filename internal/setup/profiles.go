package setup

import (
	"fmt"

	"github.com/olostan/DevCadence/internal/protocol"
)

const (
	gibibyte = 1024 * 1024 * 1024
	mem64GiB = 64 * gibibyte
	mem24GiB = 24 * gibibyte
	mem16GiB = 16 * gibibyte
	mem8GiB  = 8 * gibibyte
)

// ProfileRecommender assesses hardware facts and available endpoints to recommend a deployment profile.
type ProfileRecommender struct{}

// NewProfileRecommender returns a ProfileRecommender.
func NewProfileRecommender() *ProfileRecommender {
	return &ProfileRecommender{}
}

// Recommend evaluates environment facts and endpoints against ADR-0011 rules.
func (r *ProfileRecommender) Recommend(facts protocol.EnvironmentFacts, endpoints []protocol.CognitionEndpointSummary) protocol.ProfileRecommendation {
	hasLocalRuntime := false
	localRuntimeAccelerated := false
	hasRemoteEndpoint := false
	hasCodingCLI := false

	for _, ep := range endpoints {
		switch ep.Kind {
		case protocol.EndpointLocalRuntime:
			if ep.Health == protocol.EndpointHealthReady || ep.Health == protocol.EndpointHealthInstalled || ep.Health == protocol.EndpointHealthUnverified {
				hasLocalRuntime = true
				if ep.AccelerationVerified {
					localRuntimeAccelerated = true
				}
			}
		case protocol.EndpointRemoteAPI:
			if ep.Health == protocol.EndpointHealthReady && ep.Auth == protocol.AuthAuthenticated {
				hasRemoteEndpoint = true
			}
		case protocol.EndpointAuthenticatedCLI:
			if ep.Health == protocol.EndpointHealthReady && ep.Auth == protocol.AuthAuthenticated {
				hasCodingCLI = true
			}
		}
	}

	var totalMem uint64
	if facts.Memory.TotalBytes != nil && *facts.Memory.TotalBytes > 0 {
		totalMem = uint64(*facts.Memory.TotalBytes)
	}

	isUnified := false
	var maxVRAM uint64
	for _, acc := range facts.Accelerators {
		if acc.Class == protocol.AcceleratorUnified {
			isUnified = true
		}
		if acc.MemoryBytes != nil && *acc.MemoryBytes > 0 && uint64(*acc.MemoryBytes) > maxVRAM {
			maxVRAM = uint64(*acc.MemoryBytes)
		}
	}

	meetsHeavyMem := (isUnified && totalMem >= mem64GiB) || (maxVRAM >= mem24GiB)
	meetsThinMem := (isUnified && totalMem >= mem16GiB) || (maxVRAM >= mem8GiB)

	// Evaluate each alternative
	// 1. local-heavy
	heavyEligible := meetsHeavyMem && hasLocalRuntime && localRuntimeAccelerated
	var heavyReasons []string
	var heavyMissing []string
	if meetsHeavyMem {
		heavyReasons = append(heavyReasons, fmt.Sprintf("Memory capacity meets local-heavy threshold (unified=%v, total=%d GiB, vram=%d GiB)", isUnified, totalMem/gibibyte, maxVRAM/gibibyte))
	} else {
		heavyMissing = append(heavyMissing, "Requires unified memory >= 64GB or dedicated VRAM >= 24GB")
	}
	if hasLocalRuntime && localRuntimeAccelerated {
		heavyReasons = append(heavyReasons, "Local runtime with verified hardware acceleration is available")
	} else if hasLocalRuntime {
		heavyMissing = append(heavyMissing, "Local runtime lacks verified hardware acceleration")
	} else {
		heavyMissing = append(heavyMissing, "No healthy local runtime endpoint discovered")
	}
	if len(heavyReasons) == 0 {
		heavyReasons = append(heavyReasons, "Machine memory and local runtime do not satisfy local-heavy requirements")
	}

	// 2. hybrid-thin
	thinEligible := meetsThinMem && hasLocalRuntime && (hasRemoteEndpoint || hasCodingCLI)
	var thinReasons []string
	var thinMissing []string
	if meetsThinMem {
		thinReasons = append(thinReasons, fmt.Sprintf("Memory capacity meets hybrid-thin threshold (unified=%v, total=%d GiB, vram=%d GiB)", isUnified, totalMem/gibibyte, maxVRAM/gibibyte))
	} else {
		thinMissing = append(thinMissing, "Requires unified memory >= 16GB or dedicated VRAM >= 8GB")
	}
	if hasLocalRuntime {
		thinReasons = append(thinReasons, "Local runtime available for scout and review roles")
	} else {
		thinMissing = append(thinMissing, "No local runtime available for local roles")
	}
	if hasRemoteEndpoint || hasCodingCLI {
		thinReasons = append(thinReasons, "Remote API or authenticated coding CLI available for complex roles")
	} else {
		thinMissing = append(thinMissing, "No authenticated remote API or coding CLI found for complex roles")
	}
	if len(thinReasons) == 0 {
		thinReasons = append(thinReasons, "Machine memory, local runtime, or remote endpoints do not satisfy hybrid-thin requirements")
	}

	// 3. cloud-cognition
	cloudEligible := hasRemoteEndpoint || hasCodingCLI
	var cloudReasons []string
	var cloudMissing []string
	if cloudEligible {
		cloudReasons = append(cloudReasons, "Authenticated remote endpoint or coding CLI is available")
	} else {
		cloudReasons = append(cloudReasons, "No authenticated remote endpoints or coding CLIs detected")
		cloudMissing = append(cloudMissing, "No authenticated remote API or coding CLI found")
	}

	// 4. offline
	offlineEligible := meetsThinMem && hasLocalRuntime
	var offlineReasons []string
	var offlineMissing []string
	if offlineEligible {
		offlineReasons = append(offlineReasons, "Local compute and local runtime available without external network requirements")
	} else {
		offlineReasons = append(offlineReasons, "Local compute or local runtime do not satisfy offline execution requirements")
		offlineMissing = append(offlineMissing, "Requires local compute (>= 16GB) and healthy local runtime")
	}

	// 5. custom
	customEligible := true
	customReasons := []string{"Manual configuration profile always eligible"}

	alternatives := []protocol.ProfileAlternative{
		{
			Profile:  protocol.ProfileLocalHeavy,
			Eligible: heavyEligible,
			Reasons:  heavyReasons,
			Missing:  heavyMissing,
		},
		{
			Profile:  protocol.ProfileHybridThin,
			Eligible: thinEligible,
			Reasons:  thinReasons,
			Missing:  thinMissing,
		},
		{
			Profile:  protocol.ProfileCloudCognition,
			Eligible: cloudEligible,
			Reasons:  cloudReasons,
			Missing:  cloudMissing,
		},
		{
			Profile:  protocol.ProfileOffline,
			Eligible: offlineEligible,
			Reasons:  offlineReasons,
			Missing:  offlineMissing,
		},
		{
			Profile:  protocol.ProfileCustom,
			Eligible: customEligible,
			Reasons:  customReasons,
		},
	}

	var selected protocol.DeploymentProfile
	var rationale []string
	var limitations []string
	var unknowns []string

	if heavyEligible {
		selected = protocol.ProfileLocalHeavy
		rationale = append(rationale, "Hardware has sufficient memory and verified local acceleration for autonomous local-heavy execution")
	} else if thinEligible {
		selected = protocol.ProfileHybridThin
		rationale = append(rationale, "System meets hybrid-thin profile: local runtime provides low-cost execution while remote endpoints handle frontier tasks")
		if !localRuntimeAccelerated {
			limitations = append(limitations, "Local runtime acceleration is not verified; local execution may use CPU fallback")
		}
	} else if cloudEligible {
		selected = protocol.ProfileCloudCognition
		rationale = append(rationale, "Authenticated cloud cognition endpoints are available; local execution hardware is insufficient or unconfigured")
		limitations = append(limitations, "All model cognition requires network access and adheres to remote source exposure policy")
	} else if offlineEligible {
		selected = protocol.ProfileOffline
		rationale = append(rationale, "Local hardware and runtime are present for offline local execution")
		limitations = append(limitations, "No frontier cloud models available; capabilities limited to local model capacity")
	} else {
		selected = protocol.ProfileCloudCognition
		rationale = append(rationale, "No eligible profile found; defaulting to cloud-cognition recommendation pending credential configuration")
		limitations = append(limitations, "Machine cannot run autonomously until an endpoint is authenticated or installed")
		unknowns = append(unknowns, "Authentication status for remote endpoints is unknown or missing")
	}

	return protocol.ProfileRecommendation{
		SelectedProfile: &selected,
		Rationale:       rationale,
		Alternatives:    alternatives,
		Limitations:     limitations,
		Unknowns:        unknowns,
	}
}
