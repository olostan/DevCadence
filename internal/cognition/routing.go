package cognition

import (
	"sort"
	"strings"

	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/protocol"
)

// Capability routing: which endpoint, if any, may play a role.
//
// The routing algorithm is deliberately boring, and boring is the requirement.
// There is no score, no weighting and no ranking function whose output nobody
// can explain. Routing is:
//
//  1. apply hard constraints, recording a reason for every rejection;
//  2. order the survivors by an explicit, documented comparison;
//  3. take the first, and explain why the others lost.
//
// A decision therefore answers both halves of the question a user actually asks
// — "why was X selected" and "why was Y rejected" — from data, not prose. That
// is what makes routing auditable when it later sends implementation work to a
// paid remote endpoint.

// Role names a cognition role. Roles are semantic, never model names (DCI-054).
type Role string

const (
	// RoleScout gathers repository evidence. It is cheap, high volume and
	// usually satisfiable by a small local model.
	RoleScout Role = "scout"
	// RoleClassifier does small structured extraction and ranking.
	RoleClassifier Role = "classifier"
	// RoleImplementer writes code.
	RoleImplementer Role = "implementer"
	// RoleCorrectnessReviewer reviews a candidate for correctness.
	RoleCorrectnessReviewer Role = "correctness_reviewer"
	// RoleArchitectureReviewer reviews architectural compliance.
	RoleArchitectureReviewer Role = "architecture_reviewer"
)

// RoleRequirement is what a role needs from an endpoint.
//
// Every field is a hard constraint. Preferences live in Policy, so that reading
// a requirement tells you what would make an endpoint ineligible and nothing
// else.
type RoleRequirement struct {
	Role Role
	// Dimension is the capability the role depends on.
	Dimension protocol.CapabilityDimension
	// MinGrade is the minimum acceptable grade. GradeUnknown means the role
	// makes no capability demand — appropriate for work where any responsive
	// endpoint will do.
	MinGrade protocol.CapabilityGrade
	// RequireStructuredOutput demands a passed structured-output probe. It is
	// set for roles that must produce protocol documents.
	RequireStructuredOutput bool
	// RequireToolUse demands tool capability.
	RequireToolUse bool
	// RequireVerifiedAcceleration demands a verified non-CPU local backend. It
	// exists for the operator who would rather not wait for CPU inference, and
	// is satisfied only by real evidence (DCI-106).
	RequireVerifiedAcceleration bool
	// MinContextTokens demands a known operating context at least this large.
	MinContextTokens int
}

// DefaultRequirements are the role requirements this build ships.
//
// The implementation and review roles demand a graded capability; the scout and
// classifier roles do not. That asymmetry is the point: routine extraction can
// go to any responsive endpoint, while writing code cannot go to an endpoint
// nobody has any evidence about.
func DefaultRequirements() map[Role]RoleRequirement {
	return map[Role]RoleRequirement{
		RoleScout: {
			Role: RoleScout, Dimension: protocol.CapabilityRepositoryReasoning,
			MinGrade: protocol.GradeUnknown,
		},
		RoleClassifier: {
			Role: RoleClassifier, Dimension: protocol.CapabilityRepositoryReasoning,
			MinGrade: protocol.GradeUnknown, RequireStructuredOutput: true,
		},
		RoleImplementer: {
			Role: RoleImplementer, Dimension: protocol.CapabilityImplementation,
			MinGrade: protocol.GradeStrong,
		},
		RoleCorrectnessReviewer: {
			Role: RoleCorrectnessReviewer, Dimension: protocol.CapabilityReview,
			MinGrade: protocol.GradeMedium,
		},
		RoleArchitectureReviewer: {
			Role: RoleArchitectureReviewer, Dimension: protocol.CapabilityArchitecture,
			MinGrade: protocol.GradeStrong,
		},
	}
}

// Policy is the project's routing policy.
//
// Source exposure and cost are project decisions, not machine facts, which is
// why they live here and not on the endpoint or in the machine profile: the same
// machine serves a project that may send focused snippets and one that may not.
type Policy struct {
	// MaxSourceExposure is the most revealing exposure class this project
	// permits. An endpoint needing more is rejected outright — remote
	// inference is never a silent fallback when policy forbids it
	// (docs/MODEL_RUNTIME.md §18).
	MaxSourceExposure protocol.SourceExposure
	// MaxCostClass is the most expensive class this project permits.
	MaxCostClass protocol.CostClass
	// Preferred lists endpoint ids the operator prefers, most preferred
	// first. It orders eligible endpoints; it never makes an ineligible one
	// eligible, so an operator cannot preference their way past a privacy
	// constraint.
	Preferred []Role2Endpoints
}

// Role2Endpoints binds a role to an operator preference order.
type Role2Endpoints struct {
	Role      Role
	Endpoints []string
}

// DefaultPolicy is the conservative policy used when a project declares none.
//
// It permits focused snippets and economical remote cognition: enough for the
// hybrid-thin profile to work, and short of shipping whole files or paying for
// frontier cognition without being asked.
func DefaultPolicy() Policy {
	return Policy{
		MaxSourceExposure: protocol.ExposureFocusedSnippets,
		MaxCostClass:      protocol.CostRemoteEconomy,
	}
}

// Outcome is what routing concluded.
type Outcome string

const (
	OutcomeSelected Outcome = "selected"
	// OutcomeNoEligibleEndpoint means every endpoint failed a hard constraint.
	// It is a correct, explainable answer — notably for a privacy-restricted
	// project whose local capability is insufficient — and not a failure of the
	// router (DCI-104).
	OutcomeNoEligibleEndpoint Outcome = "no_eligible_endpoint"
)

// Rejection is one endpoint and why it was not selected.
type Rejection struct {
	EndpointID string
	// Eligible reports whether the endpoint satisfied every hard constraint
	// and lost only on preference. The distinction matters: "could not" and
	// "was not chosen" call for different operator action.
	Eligible bool
	Reasons  []string
}

// Decision is the routing result.
type Decision struct {
	Role       Role
	Outcome    Outcome
	SelectedID string
	// Reasons explains the selection, or the absence of one.
	Reasons  []string
	Rejected []Rejection
}

// Err returns a typed error when no endpoint was eligible.
//
// The error carries CategoryNoEligibleEndpoint so a caller can route on it —
// a policy-driven "no endpoint" must not be mistaken for an internal defect.
func (d Decision) Err() error {
	if d.Outcome == OutcomeSelected {
		return nil
	}
	return errs.New(errs.CategoryNoEligibleEndpoint,
		"no cognition endpoint is eligible for role %s: %s", d.Role, strings.Join(d.Reasons, "; "))
}

// Route selects an endpoint for a role.
//
// It is pure: the same endpoints, requirement and policy always produce the same
// decision, including the ordering of reasons and rejections.
func Route(requirement RoleRequirement, policy Policy, endpoints []protocol.CognitionEndpoint) Decision {
	requirement, policy = normalise(requirement, policy)
	decision := Decision{Role: requirement.Role, Outcome: OutcomeNoEligibleEndpoint}
	preference := preferenceIndex(policy, requirement.Role)

	type candidate struct {
		endpoint   protocol.CognitionEndpoint
		reasons    []string
		preference int
	}
	var eligible []candidate

	// Hard constraints, evaluated in a fixed order so that an endpoint failing
	// several is reported against all of them. Reporting only the first would
	// send an operator to fix one problem and rediscover the next.
	for _, endpoint := range sortedEndpoints(endpoints) {
		var blocking, satisfied []string

		if !endpoint.Health.Usable() {
			blocking = append(blocking, "endpoint health is "+string(endpoint.Health)+
				", and only a probed-ready endpoint may be routed to")
		} else {
			satisfied = append(satisfied, "endpoint is probed ready")
		}

		switch endpoint.Auth {
		case protocol.AuthAuthenticated, protocol.AuthNotApplicable:
			satisfied = append(satisfied, "authentication state is "+string(endpoint.Auth))
		case protocol.AuthUnauthenticated, protocol.AuthExpired, protocol.AuthError:
			blocking = append(blocking, "authentication state is "+string(endpoint.Auth))
		default:
			// Unknown authentication is not a rejection for a healthy
			// endpoint: a probe that answered demonstrates a usable session
			// more directly than any status command would, and several CLIs
			// publish no safe way to ask. It is recorded so the decision is
			// not read as a confirmed account.
			satisfied = append(satisfied, "authentication state is unknown; the health probe answered regardless")
		}

		if endpoint.RequiredSourceExposure.ExposureRank() > policy.MaxSourceExposure.ExposureRank() {
			blocking = append(blocking, "requires source exposure "+
				string(endpoint.RequiredSourceExposure)+", which exceeds the project policy of "+
				string(policy.MaxSourceExposure))
		} else {
			satisfied = append(satisfied, "source exposure "+string(endpoint.RequiredSourceExposure)+" is allowed")
		}

		if endpoint.CostClass.CostRank() > policy.MaxCostClass.CostRank() {
			blocking = append(blocking, "cost class "+string(endpoint.CostClass)+
				" exceeds the project policy of "+string(policy.MaxCostClass))
		} else {
			satisfied = append(satisfied, "cost class "+string(endpoint.CostClass)+" is acceptable")
		}

		graded := endpoint.Capability(requirement.Dimension)
		if requirement.MinGrade != protocol.GradeUnknown {
			switch {
			case graded.Grade == protocol.GradeUnknown:
				blocking = append(blocking, string(requirement.Dimension)+
					" capability is unknown, and this role requires at least "+string(requirement.MinGrade))
			case graded.Grade.GradeRank() < requirement.MinGrade.GradeRank():
				blocking = append(blocking, string(requirement.Dimension)+" capability is "+
					string(graded.Grade)+", below the required "+string(requirement.MinGrade))
			default:
				satisfied = append(satisfied, string(requirement.Dimension)+" capability is "+
					string(graded.Grade)+" ("+string(graded.Provenance)+")")
			}
		}

		if requirement.RequireStructuredOutput {
			if endpoint.StructuredOutput != protocol.FeatureProbePassed {
				blocking = append(blocking, "structured output is "+string(endpoint.StructuredOutput)+
					", and this role requires a passed structured-output probe")
			} else {
				satisfied = append(satisfied, "structured-output probe passed")
			}
		}
		if requirement.RequireToolUse {
			switch endpoint.ToolUse {
			case protocol.FeatureProbePassed, protocol.FeatureDeclared:
				satisfied = append(satisfied, "tool use is "+string(endpoint.ToolUse))
			default:
				blocking = append(blocking, "tool use is "+string(endpoint.ToolUse))
			}
		}
		if requirement.RequireVerifiedAcceleration {
			if endpoint.AccelerationVerified() {
				satisfied = append(satisfied, "acceleration is verified on backend "+
					string(endpoint.Acceleration.Backend))
			} else {
				blocking = append(blocking, "verified non-cpu acceleration is required but "+
					Describe(endpoint.Acceleration))
			}
		}
		if requirement.MinContextTokens > 0 {
			switch {
			case endpoint.ContextTokens == nil:
				blocking = append(blocking, "operating context size is unknown")
			case *endpoint.ContextTokens < requirement.MinContextTokens:
				blocking = append(blocking, "operating context is smaller than the role requires")
			default:
				satisfied = append(satisfied, "operating context is sufficient")
			}
		}

		if len(blocking) > 0 {
			sort.Strings(blocking)
			decision.Rejected = append(decision.Rejected,
				Rejection{EndpointID: endpoint.ID, Eligible: false, Reasons: blocking})
			continue
		}
		sort.Strings(satisfied)
		eligible = append(eligible, candidate{
			endpoint: endpoint, reasons: satisfied, preference: rankOf(preference, endpoint.ID),
		})
	}

	if len(eligible) == 0 {
		decision.Reasons = []string{noEligibleReason(endpoints, requirement, policy)}
		sortRejections(decision.Rejected)
		return decision
	}

	// Ordering. Operator preference first, because an operator who named an
	// order knows something the router does not. Then cheapest, then most
	// private, then best-graded, then id — cost before capability grade because
	// the routing principle is "the least expensive, most private endpoint that
	// *satisfies* the requirement", and everything still in the running already
	// satisfies it (docs/MODEL_RUNTIME.md §5).
	sort.SliceStable(eligible, func(i, j int) bool {
		a, b := eligible[i], eligible[j]
		if a.preference != b.preference {
			return a.preference < b.preference
		}
		if ra, rb := a.endpoint.CostClass.CostRank(), b.endpoint.CostClass.CostRank(); ra != rb {
			return ra < rb
		}
		ea := a.endpoint.RequiredSourceExposure.ExposureRank()
		eb := b.endpoint.RequiredSourceExposure.ExposureRank()
		if ea != eb {
			return ea < eb
		}
		ga := a.endpoint.Capability(requirement.Dimension).Grade.GradeRank()
		gb := b.endpoint.Capability(requirement.Dimension).Grade.GradeRank()
		if ga != gb {
			return ga > gb
		}
		// Identifier last, so ties are broken deterministically rather than by
		// discovery order.
		return a.endpoint.ID < b.endpoint.ID
	})

	selected := eligible[0]
	decision.Outcome = OutcomeSelected
	decision.SelectedID = selected.endpoint.ID
	decision.Reasons = selected.reasons

	for _, loser := range eligible[1:] {
		reasons := []string{"satisfies every hard constraint for this role"}
		switch {
		case loser.preference > selected.preference:
			reasons = append(reasons, "the operator's preference order ranks "+
				selected.endpoint.ID+" ahead of it")
		case loser.endpoint.CostClass.CostRank() > selected.endpoint.CostClass.CostRank():
			reasons = append(reasons, "cost class "+string(loser.endpoint.CostClass)+
				" is more expensive than the selected endpoint's "+string(selected.endpoint.CostClass))
		case loser.endpoint.RequiredSourceExposure.ExposureRank() >
			selected.endpoint.RequiredSourceExposure.ExposureRank():
			reasons = append(reasons, "requires more source exposure ("+
				string(loser.endpoint.RequiredSourceExposure)+") than the selected endpoint")
		default:
			reasons = append(reasons, "ranked below "+selected.endpoint.ID+
				" on capability grade, then on identifier")
		}
		decision.Rejected = append(decision.Rejected,
			Rejection{EndpointID: loser.endpoint.ID, Eligible: true, Reasons: reasons})
	}
	sortRejections(decision.Rejected)
	return decision
}

// normalise fills the zero values, and fills them conservatively.
//
// An unset MinGrade means "this role makes no capability demand", which is the
// same as GradeUnknown — without this, an empty string would be compared against
// GradeUnknown, come out unequal, and silently impose a requirement nothing
// could satisfy.
//
// An unset policy fails *closed*. A zero Policy is most plausibly a caller that
// forgot to configure one, and defaulting it to permissive would let remote
// cognition be selected by omission — exactly the silent fallback
// docs/MODEL_RUNTIME.md §18 forbids. So an unset exposure limit means local_only
// and an unset cost limit means local_compute: an unconfigured project can use
// what is already on the machine and nothing else.
func normalise(requirement RoleRequirement, policy Policy) (RoleRequirement, Policy) {
	if requirement.MinGrade == "" {
		requirement.MinGrade = protocol.GradeUnknown
	}
	if policy.MaxSourceExposure == "" {
		policy.MaxSourceExposure = protocol.ExposureLocalOnly
	}
	if policy.MaxCostClass == "" {
		policy.MaxCostClass = protocol.CostLocalCompute
	}
	return requirement, policy
}

// RouteAll routes every requirement, returning decisions in role order.
func RouteAll(requirements map[Role]RoleRequirement, policy Policy, endpoints []protocol.CognitionEndpoint) []Decision {
	roles := make([]Role, 0, len(requirements))
	for role := range requirements {
		roles = append(roles, role)
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i] < roles[j] })
	out := make([]Decision, 0, len(roles))
	for _, role := range roles {
		out = append(out, Route(requirements[role], policy, endpoints))
	}
	return out
}

// noEligibleReason states the dominant reason nothing was eligible.
//
// A single honest sentence is worth more here than a list: an operator whose
// privacy policy excluded every remote endpoint needs to be told that, not
// handed a table of individually plausible rejections.
func noEligibleReason(endpoints []protocol.CognitionEndpoint, requirement RoleRequirement, policy Policy) string {
	if len(endpoints) == 0 {
		return "no cognition endpoint was discovered on this machine"
	}
	healthy := 0
	blockedByExposure := 0
	blockedByCost := 0
	blockedByCapability := 0
	for _, endpoint := range endpoints {
		if !endpoint.Health.Usable() {
			continue
		}
		healthy++
		if endpoint.RequiredSourceExposure.ExposureRank() > policy.MaxSourceExposure.ExposureRank() {
			blockedByExposure++
			continue
		}
		if endpoint.CostClass.CostRank() > policy.MaxCostClass.CostRank() {
			blockedByCost++
			continue
		}
		if requirement.MinGrade != protocol.GradeUnknown &&
			endpoint.Capability(requirement.Dimension).Grade.GradeRank() < requirement.MinGrade.GradeRank() {
			blockedByCapability++
		}
	}
	switch {
	case healthy == 0:
		return "no discovered endpoint is probed ready"
	case blockedByExposure > 0 && blockedByExposure == healthy:
		return "every ready endpoint requires more source exposure than the project policy of " +
			string(policy.MaxSourceExposure) + " permits"
	case blockedByExposure > 0:
		return "the ready endpoints capable of this role require more source exposure than the project policy of " +
			string(policy.MaxSourceExposure) + " permits"
	case blockedByCost > 0 && blockedByCost == healthy:
		return "every ready endpoint costs more than the project policy of " +
			string(policy.MaxCostClass) + " permits"
	case blockedByCapability > 0:
		return "no ready endpoint has a " + string(requirement.Dimension) +
			" capability of at least " + string(requirement.MinGrade) +
			"; an ungraded endpoint is not evidence of capability"
	default:
		return "no ready endpoint satisfies every constraint for this role"
	}
}

// unrankedPreference is the rank given to an endpoint the operator did not
// name. It sits above every named rank so that naming an endpoint promotes it,
// and an unnamed endpoint falls back to the router's own ordering.
const unrankedPreference = 1 << 20

// preferenceIndex maps endpoint id to operator preference rank for one role.
func preferenceIndex(policy Policy, role Role) map[string]int {
	out := map[string]int{}
	for _, binding := range policy.Preferred {
		if binding.Role != role {
			continue
		}
		for i, id := range binding.Endpoints {
			if _, exists := out[id]; !exists {
				out[id] = i
			}
		}
	}
	return out
}

// rankOf reads a preference rank, defaulting to unranked.
//
// A bare map lookup would return zero for a missing key, and zero means "most
// preferred" — the opposite of the intent — so every lookup goes through here.
func rankOf(index map[string]int, id string) int {
	if rank, ok := index[id]; ok {
		return rank
	}
	return unrankedPreference
}

func sortedEndpoints(endpoints []protocol.CognitionEndpoint) []protocol.CognitionEndpoint {
	out := append([]protocol.CognitionEndpoint(nil), endpoints...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func sortRejections(rejections []Rejection) {
	sort.Slice(rejections, func(i, j int) bool { return rejections[i].EndpointID < rejections[j].EndpointID })
}

// Explain renders a decision as the operator-facing explanation.
//
// The format mirrors the one the M3A specification asks for, so that "why was X
// selected" and "why was Y rejected" are answerable from the CLI without
// reading code.
func (d Decision) Explain() string {
	var b strings.Builder
	b.WriteString(string(d.Role) + ":\n")
	if d.Outcome == OutcomeSelected {
		b.WriteString("  selected: " + d.SelectedID + "\n")
	} else {
		b.WriteString("  selected: none (" + string(d.Outcome) + ")\n")
	}
	if len(d.Reasons) > 0 {
		b.WriteString("  reasons:\n")
		for _, reason := range d.Reasons {
			b.WriteString("    - " + reason + "\n")
		}
	}
	if len(d.Rejected) > 0 {
		b.WriteString("  rejected:\n")
		for _, rejection := range d.Rejected {
			b.WriteString("    " + rejection.EndpointID + ":\n")
			for _, reason := range rejection.Reasons {
				b.WriteString("      - " + reason + "\n")
			}
		}
	}
	return b.String()
}
