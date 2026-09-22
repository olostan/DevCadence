package setup

import (
	"fmt"

	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/environment"
	"github.com/olostan/DevCadence/internal/protocol"
)

const (
	gibibyte = 1024 * 1024 * 1024
	mem64GiB = 64 * gibibyte
	mem24GiB = 24 * gibibyte
	mem16GiB = 16 * gibibyte
	mem8GiB  = 8 * gibibyte
)

// UserPreferences represents operator preferences for profile selection.
type UserPreferences struct {
	PreferredProfile *protocol.DeploymentProfile `json:"preferred_profile,omitempty"`
}

// RecommendationInput provides all contextual inputs for profile recommendation.
type RecommendationInput struct {
	Facts            protocol.EnvironmentFacts
	Endpoints        []protocol.CognitionEndpointSummary
	CognitionProfile *protocol.MachineCapabilityProfile // optional full profile
	Policy           *cognition.Policy                  // optional routing policy
	UserPreferences  *UserPreferences                   // optional user preferences
}

// ProfileRecommender assesses hardware facts and available endpoints to recommend a deployment profile.
type ProfileRecommender struct{}

// NewProfileRecommender returns a ProfileRecommender.
func NewProfileRecommender() *ProfileRecommender {
	return &ProfileRecommender{}
}

// summariesToEndpoints synthesizes CognitionEndpoints from CognitionEndpointSummaries.
func summariesToEndpoints(summaries []protocol.CognitionEndpointSummary) []protocol.CognitionEndpoint {
	var endpoints []protocol.CognitionEndpoint
	for _, s := range summaries {
		var acc *protocol.AccelerationEvidence
		if s.AccelerationBackend != nil {
			state := protocol.StateUnverified
			if s.AccelerationVerified {
				state = protocol.StateVerified
			}
			acc = &protocol.AccelerationEvidence{
				Backend: *s.AccelerationBackend,
				State:   state,
			}
		}

		var caps []protocol.GradedCapability
		if s.Kind == protocol.EndpointAuthenticatedCLI || s.Kind == protocol.EndpointRemoteAPI {
			if s.Health == protocol.EndpointHealthReady && s.Auth == protocol.AuthAuthenticated {
				caps = append(caps,
					protocol.GradedCapability{
						Dimension:  protocol.CapabilityImplementation,
						Grade:      protocol.GradeStrong,
						Provenance: protocol.ProvenanceConfigured,
					},
					protocol.GradedCapability{
						Dimension:  protocol.CapabilityArchitecture,
						Grade:      protocol.GradeStrong,
						Provenance: protocol.ProvenanceConfigured,
					},
					protocol.GradedCapability{
						Dimension:  protocol.CapabilityReview,
						Grade:      protocol.GradeStrong,
						Provenance: protocol.ProvenanceConfigured,
					},
					protocol.GradedCapability{
						Dimension:  protocol.CapabilityRepositoryReasoning,
						Grade:      protocol.GradeStrong,
						Provenance: protocol.ProvenanceConfigured,
					},
				)
			}
		} else if s.Kind == protocol.EndpointLocalRuntime {
			if s.Health == protocol.EndpointHealthReady {
				caps = append(caps,
					protocol.GradedCapability{
						Dimension:  protocol.CapabilityRepositoryReasoning,
						Grade:      protocol.GradeMedium,
						Provenance: protocol.ProvenanceEvaluated,
					},
				)
				if s.AccelerationVerified {
					caps = append(caps,
						protocol.GradedCapability{
							Dimension:  protocol.CapabilityImplementation,
							Grade:      protocol.GradeStrong,
							Provenance: protocol.ProvenanceEvaluated,
						},
						protocol.GradedCapability{
							Dimension:  protocol.CapabilityReview,
							Grade:      protocol.GradeMedium,
							Provenance: protocol.ProvenanceEvaluated,
						},
					)
				}
			}
		}

		so := protocol.FeatureUnsupported
		tu := protocol.FeatureUnsupported
		if s.Health == protocol.EndpointHealthReady {
			so = protocol.FeatureProbePassed
			tu = protocol.FeatureProbePassed
		}

		costClass := s.CostClass
		sourceExposure := s.RequiredSourceExposure
		locality := s.Locality
		auth := s.Auth

		if s.Kind == protocol.EndpointLocalRuntime {
			if costClass == "" {
				costClass = protocol.CostLocalCompute
			}
			if sourceExposure == "" {
				sourceExposure = protocol.ExposureLocalOnly
			}
			if locality == "" {
				locality = protocol.LocalityLocal
			}
			if auth == "" {
				auth = protocol.AuthNotApplicable
			}
		} else {
			if costClass == "" {
				costClass = protocol.CostRemoteEconomy
			}
			if sourceExposure == "" {
				sourceExposure = protocol.ExposureFocusedSnippets
			}
			if locality == "" {
				locality = protocol.LocalityRemote
			}
			if auth == "" && s.Health == protocol.EndpointHealthReady {
				auth = protocol.AuthAuthenticated
			}
		}

		ep := protocol.CognitionEndpoint{
			ID:                     s.ID,
			Kind:                   s.Kind,
			Locality:               locality,
			Health:                 s.Health,
			Auth:                   auth,
			CostClass:              costClass,
			RequiredSourceExposure: sourceExposure,
			Acceleration:           acc,
			Capabilities:           caps,
			StructuredOutput:       so,
			ToolUse:                tu,
		}
		endpoints = append(endpoints, ep)
	}
	return endpoints
}

// Recommend evaluates environment facts, endpoints, policy, and preferences against ADR-0011/ADR-0014 rules.
func (r *ProfileRecommender) Recommend(input RecommendationInput) protocol.ProfileRecommendation {
	facts := input.Facts
	policy := input.Policy

	effPolicy := cognition.DefaultPolicy()
	if policy != nil {
		effPolicy = *policy
	}

	var fullEndpoints []protocol.CognitionEndpoint
	if input.CognitionProfile != nil && len(input.CognitionProfile.Endpoints) > 0 {
		fullEndpoints = input.CognitionProfile.Endpoints
	} else {
		fullEndpoints = summariesToEndpoints(input.Endpoints)
	}

	var localEndpoints []protocol.CognitionEndpoint
	var remoteEndpoints []protocol.CognitionEndpoint
	localRuntimeAccelerated := false
	var policyRejections []string

	for _, ep := range fullEndpoints {
		if ep.Health != protocol.EndpointHealthReady {
			// Unverified, installed, or unhealthy endpoints are not functional
			continue
		}
		if ep.Locality == protocol.LocalityLocal || ep.Kind == protocol.EndpointLocalRuntime {
			localEndpoints = append(localEndpoints, ep)
			if ep.AccelerationVerified() {
				localRuntimeAccelerated = true
			}
		} else {
			if ep.RequiredSourceExposure.ExposureRank() > effPolicy.MaxSourceExposure.ExposureRank() {
				policyRejections = append(policyRejections, fmt.Sprintf("Endpoint %s requires source exposure %s exceeding policy %s", ep.ID, ep.RequiredSourceExposure, effPolicy.MaxSourceExposure))
				continue
			}
			if ep.CostClass.CostRank() > effPolicy.MaxCostClass.CostRank() {
				policyRejections = append(policyRejections, fmt.Sprintf("Endpoint %s cost class %s exceeds policy %s", ep.ID, ep.CostClass, effPolicy.MaxCostClass))
				continue
			}
			remoteEndpoints = append(remoteEndpoints, ep)
		}
	}

	hasLocalRuntime := len(localEndpoints) > 0
	hasRemoteEndpoint := len(remoteEndpoints) > 0

	// Assess hardware accelerator backends
	hasViableAccelerator := false
	for _, c := range environment.AssessBackends(facts) {
		if c.Support == protocol.SupportSupported && c.Backend != protocol.BackendCPU {
			hasViableAccelerator = true
			break
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

	meetsHeavyMem := hasViableAccelerator && ((isUnified && totalMem >= mem64GiB) || (maxVRAM >= mem24GiB))
	meetsThinMem := (isUnified && totalMem >= mem16GiB) || (maxVRAM >= mem8GiB) || (totalMem >= mem16GiB)

	defaultReqs := cognition.DefaultRequirements()

	// 1. local-heavy: requires unified memory >= 64GB or dedicated VRAM >= 24GB,
	// verified local hardware acceleration, and successful routing for RoleImplementer locally.
	canRouteLocalImplementer := false
	var localImplReasons []string
	if hasLocalRuntime && localRuntimeAccelerated {
		dec := cognition.Route(defaultReqs[cognition.RoleImplementer], effPolicy, localEndpoints)
		if dec.Outcome == cognition.OutcomeSelected {
			canRouteLocalImplementer = true
		} else {
			localImplReasons = dec.Reasons
		}
	}

	heavyEligible := meetsHeavyMem && hasLocalRuntime && localRuntimeAccelerated && canRouteLocalImplementer
	var heavyReasons []string
	var heavyMissing []string
	if meetsHeavyMem {
		heavyReasons = append(heavyReasons, fmt.Sprintf("Memory and accelerator backend meet local-heavy threshold (unified=%v, total=%d GiB, vram=%d GiB)", isUnified, totalMem/gibibyte, maxVRAM/gibibyte))
	} else if !hasViableAccelerator {
		heavyMissing = append(heavyMissing, "No supported hardware accelerator backend (Metal, CUDA, ROCm) available")
	} else {
		heavyMissing = append(heavyMissing, "Requires unified memory >= 64GB or dedicated VRAM >= 24GB")
	}
	if hasLocalRuntime && localRuntimeAccelerated && canRouteLocalImplementer {
		heavyReasons = append(heavyReasons, "Local runtime with verified hardware acceleration and strong implementation capability is available")
	} else if hasLocalRuntime && localRuntimeAccelerated {
		heavyMissing = append(heavyMissing, "Local runtime lacks verified implementation capability grade required for autonomous execution")
		if len(localImplReasons) > 0 {
			heavyMissing = append(heavyMissing, localImplReasons...)
		}
	} else if hasLocalRuntime {
		heavyMissing = append(heavyMissing, "Local runtime lacks verified hardware acceleration")
	} else {
		heavyMissing = append(heavyMissing, "No ready local runtime endpoint discovered")
	}
	if len(heavyReasons) == 0 {
		heavyReasons = append(heavyReasons, "Machine memory and local runtime do not satisfy local-heavy requirements")
	}

	// 2. hybrid-thin: requires memory >= 16GB, local runtime capable of RoleScout,
	// and remote endpoint capable of RoleImplementer.
	canRouteLocalScout := false
	var scoutReasons []string
	if hasLocalRuntime {
		dec := cognition.Route(defaultReqs[cognition.RoleScout], effPolicy, localEndpoints)
		if dec.Outcome == cognition.OutcomeSelected {
			canRouteLocalScout = true
		} else {
			scoutReasons = dec.Reasons
		}
	}

	canRouteRemoteImplementer := false
	var remoteImplReasons []string
	if hasRemoteEndpoint {
		dec := cognition.Route(defaultReqs[cognition.RoleImplementer], effPolicy, remoteEndpoints)
		if dec.Outcome == cognition.OutcomeSelected {
			canRouteRemoteImplementer = true
		} else {
			remoteImplReasons = dec.Reasons
		}
	}

	thinEligible := meetsThinMem && canRouteLocalScout && canRouteRemoteImplementer
	var thinReasons []string
	var thinMissing []string
	if meetsThinMem {
		thinReasons = append(thinReasons, fmt.Sprintf("Memory capacity meets hybrid-thin threshold (unified=%v, total=%d GiB, vram=%d GiB)", isUnified, totalMem/gibibyte, maxVRAM/gibibyte))
	} else {
		thinMissing = append(thinMissing, "Requires unified memory >= 16GB or dedicated VRAM >= 8GB")
	}
	if canRouteLocalScout {
		thinReasons = append(thinReasons, "Local runtime available and routed for scout and reconnaissance roles")
	} else if hasLocalRuntime {
		thinMissing = append(thinMissing, "Local runtime cannot satisfy scout role requirements")
		if len(scoutReasons) > 0 {
			thinMissing = append(thinMissing, scoutReasons...)
		}
	} else {
		thinMissing = append(thinMissing, "No ready local runtime available for local roles")
	}
	if canRouteRemoteImplementer {
		thinReasons = append(thinReasons, "Remote API or authenticated coding CLI available and routed for complex roles")
	} else if hasRemoteEndpoint {
		thinMissing = append(thinMissing, "Remote endpoints lack strong implementation capability or fail routing policy")
		if len(remoteImplReasons) > 0 {
			thinMissing = append(thinMissing, remoteImplReasons...)
		}
	} else {
		thinMissing = append(thinMissing, "No authenticated remote API or coding CLI found satisfying routing policy")
	}
	if len(thinReasons) == 0 {
		thinReasons = append(thinReasons, "Machine memory, local runtime, or remote endpoints do not satisfy hybrid-thin requirements")
	}

	// 3. cloud-cognition: requires remote endpoint capable of RoleImplementer
	cloudEligible := canRouteRemoteImplementer
	var cloudReasons []string
	var cloudMissing []string
	if cloudEligible {
		cloudReasons = append(cloudReasons, "Authenticated remote endpoint or coding CLI is available, routed for implementation, and satisfies routing policy")
	} else if hasRemoteEndpoint {
		cloudReasons = append(cloudReasons, "Remote endpoints detected but ineligible for implementation role under policy")
		cloudMissing = append(cloudMissing, "Remote endpoints lack strong implementation capability or fail routing policy")
		if len(remoteImplReasons) > 0 {
			cloudMissing = append(cloudMissing, remoteImplReasons...)
		}
	} else {
		cloudReasons = append(cloudReasons, "No authenticated remote endpoints or coding CLIs detected satisfying routing policy")
		cloudMissing = append(cloudMissing, "No authenticated remote API or coding CLI found satisfying routing policy")
	}

	// 4. offline: requires memory >= 16GB and local runtime capable of RoleScout
	offlineEligible := meetsThinMem && canRouteLocalScout
	var offlineReasons []string
	var offlineMissing []string
	if offlineEligible {
		offlineReasons = append(offlineReasons, "Local compute and ready local runtime available without external network requirements")
	} else {
		offlineReasons = append(offlineReasons, "Local compute or local runtime do not satisfy offline execution requirements")
		offlineMissing = append(offlineMissing, "Requires local compute (>= 16GB) and ready local runtime capable of repository reasoning")
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

	var selected *protocol.DeploymentProfile
	var rationale []string
	var limitations []string
	var unknowns []string

	if len(policyRejections) > 0 {
		limitations = append(limitations, policyRejections...)
	}

	// Honor user preferences if eligible
	if input.UserPreferences != nil && input.UserPreferences.PreferredProfile != nil {
		pref := *input.UserPreferences.PreferredProfile
		switch pref {
		case protocol.ProfileLocalHeavy:
			if heavyEligible {
				selected = &pref
				rationale = append(rationale, "Selected local-heavy profile per operator preference")
			}
		case protocol.ProfileHybridThin:
			if thinEligible {
				selected = &pref
				rationale = append(rationale, "Selected hybrid-thin profile per operator preference")
			}
		case protocol.ProfileCloudCognition:
			if cloudEligible {
				selected = &pref
				rationale = append(rationale, "Selected cloud-cognition profile per operator preference")
			}
		case protocol.ProfileOffline:
			if offlineEligible {
				selected = &pref
				rationale = append(rationale, "Selected offline profile per operator preference")
			}
		case protocol.ProfileCustom:
			selected = &pref
			rationale = append(rationale, "Selected custom profile per operator preference")
		}
	}

	if selected == nil {
		if heavyEligible {
			p := protocol.ProfileLocalHeavy
			selected = &p
			rationale = append(rationale, "Hardware has sufficient memory and verified local acceleration for autonomous local-heavy execution")
		} else if thinEligible {
			p := protocol.ProfileHybridThin
			selected = &p
			rationale = append(rationale, "System meets hybrid-thin profile: local runtime provides low-cost execution while remote endpoints handle frontier tasks")
			if !localRuntimeAccelerated {
				limitations = append(limitations, "Local runtime acceleration is not verified; local execution may use CPU fallback")
			}
		} else if cloudEligible {
			p := protocol.ProfileCloudCognition
			selected = &p
			rationale = append(rationale, "Authenticated cloud cognition endpoints are available; local execution hardware is insufficient or unconfigured")
			limitations = append(limitations, "All model cognition requires network access and adheres to remote source exposure policy")
		} else if offlineEligible {
			p := protocol.ProfileOffline
			selected = &p
			rationale = append(rationale, "Local hardware and runtime are present for offline local execution")
			limitations = append(limitations, "No frontier cloud models available; capabilities limited to local model capacity")
		} else {
			// ADR-0014: If no profile satisfies capability and policy, SelectedProfile is nil
			rationale = append(rationale, "No deployment profile satisfies both detected capability and active routing policy")
			limitations = append(limitations, "Machine cannot run autonomously until an eligible endpoint is configured or authenticated")
			unknowns = append(unknowns, "Missing required cognition endpoints or authentication credentials")
		}
	}

	return protocol.ProfileRecommendation{
		SelectedProfile: selected,
		Rationale:       rationale,
		Alternatives:    alternatives,
		Limitations:     limitations,
		Unknowns:        unknowns,
	}
}

// RecommendEndpoints provides a backward-compatible entrypoint.
func (r *ProfileRecommender) RecommendEndpoints(facts protocol.EnvironmentFacts, endpoints []protocol.CognitionEndpointSummary) protocol.ProfileRecommendation {
	return r.Recommend(RecommendationInput{
		Facts:     facts,
		Endpoints: endpoints,
	})
}
