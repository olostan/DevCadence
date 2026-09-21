package cognition_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/olostan/DevCadience/internal/cognition"
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/protocol"
)

// The five end-to-end routing scenarios M3A must support are each a test here:
// strong-local, hybrid-thin, no-local-model, offline and privacy-restricted.

// strongLocal is a machine with a verified, accelerated local model the operator
// has declared a strong implementer, plus an optional remote CLI.
func strongLocal() []protocol.CognitionEndpoint {
	local := cognition.Ready(cognition.LocalEndpoint("ollama:local-strong", "ollama", "big-coder"))
	local = cognition.WithCapability(local, protocol.CapabilityImplementation,
		protocol.GradeStrong, protocol.ProvenanceConfigured)
	local = cognition.WithCapability(local, protocol.CapabilityReview,
		protocol.GradeStrong, protocol.ProvenanceConfigured)
	local = cognition.WithVerifiedAcceleration(local, protocol.BackendMetal, at())
	local.StructuredOutput = protocol.FeatureProbePassed

	remote := cognition.Ready(cognition.CLIEndpoint("cli:codex-cli", "openai"))
	remote = cognition.WithCapability(remote, protocol.CapabilityImplementation,
		protocol.GradeStrong, protocol.ProvenanceConfigured)
	return []protocol.CognitionEndpoint{local, remote}
}

// hybridThin is the ADR-0011 thin machine: a small local model with no graded
// implementation capability, plus an authenticated remote CLI that has one.
func hybridThin() []protocol.CognitionEndpoint {
	small := cognition.Ready(cognition.LocalEndpoint("ollama:local-small", "ollama", "small"))
	small = cognition.WithCapability(small, protocol.CapabilityRepositoryReasoning,
		protocol.GradeMedium, protocol.ProvenanceConfigured)
	small.StructuredOutput = protocol.FeatureProbePassed

	remote := cognition.Ready(cognition.CLIEndpoint("cli:codex-cli", "openai"))
	remote = cognition.WithCapability(remote, protocol.CapabilityImplementation,
		protocol.GradeStrong, protocol.ProvenanceConfigured)
	remote = cognition.WithCapability(remote, protocol.CapabilityRepositoryReasoning,
		protocol.GradeStrong, protocol.ProvenanceConfigured)
	return []protocol.CognitionEndpoint{small, remote}
}

// permissivePolicy allows tool-mediated worktree access and subscription cost,
// which is what a project using an authenticated coding CLI must permit.
func permissivePolicy() cognition.Policy {
	return cognition.Policy{
		MaxSourceExposure: protocol.ExposureToolMediatedWorktree,
		MaxCostClass:      protocol.CostRemoteStrong,
	}
}

func requirement(role cognition.Role) cognition.RoleRequirement {
	return cognition.DefaultRequirements()[role]
}

func rejectionFor(decision cognition.Decision, id string) (cognition.Rejection, bool) {
	for _, rejection := range decision.Rejected {
		if rejection.EndpointID == id {
			return rejection, true
		}
	}
	return cognition.Rejection{}, false
}

func reasonsContain(reasons []string, needle string) bool {
	for _, reason := range reasons {
		if strings.Contains(reason, needle) {
			return true
		}
	}
	return false
}

// Scenario A: a strong local machine keeps implementation local, because local
// compute is cheaper than a subscription and both satisfy the requirement.
func TestStrongLocalMachineKeepsImplementationLocal(t *testing.T) {
	decision := cognition.Route(requirement(cognition.RoleImplementer), permissivePolicy(), strongLocal())
	if decision.Outcome != cognition.OutcomeSelected {
		t.Fatalf("outcome = %q, want selected: %v", decision.Outcome, decision.Reasons)
	}
	if decision.SelectedID != "ollama:local-strong" {
		t.Errorf("selected = %q, want the local endpoint", decision.SelectedID)
	}
	// The remote endpoint must be reported as eligible-but-not-chosen, not as
	// incapable: the two call for different operator action.
	rejection, found := rejectionFor(decision, "cli:codex-cli")
	if !found {
		t.Fatal("the remote endpoint was not explained at all")
	}
	if !rejection.Eligible {
		t.Error("the remote endpoint was reported as ineligible when it merely lost on cost")
	}
	if !reasonsContain(rejection.Reasons, "more expensive") {
		t.Errorf("the cost comparison was not explained: %v", rejection.Reasons)
	}
	if !reasonsContain(decision.Reasons, "implementation capability is strong (configured)") {
		t.Errorf("the selection did not cite the capability and its provenance: %v", decision.Reasons)
	}
}

// Scenario B: the hybrid-thin machine routes cheap work locally and
// implementation to the remote CLI.
func TestHybridThinRoutesImplementationRemotelyAndScoutingLocally(t *testing.T) {
	endpoints := hybridThin()
	policy := permissivePolicy()

	implementer := cognition.Route(requirement(cognition.RoleImplementer), policy, endpoints)
	if implementer.SelectedID != "cli:codex-cli" {
		t.Fatalf("implementer = %q, want the remote cli: %v", implementer.SelectedID, implementer.Reasons)
	}
	rejection, _ := rejectionFor(implementer, "ollama:local-small")
	if rejection.Eligible {
		t.Error("the small local model was treated as eligible for implementation")
	}
	if !reasonsContain(rejection.Reasons, "implementation capability is unknown") {
		t.Errorf("the rejection did not say the capability is unknown: %v", rejection.Reasons)
	}

	// Scouting has no capability floor, so the cheapest ready endpoint wins —
	// which is the local one, keeping high-volume work off the paid endpoint.
	scout := cognition.Route(requirement(cognition.RoleScout), policy, endpoints)
	if scout.SelectedID != "ollama:local-small" {
		t.Errorf("scout = %q, want the local endpoint: %v", scout.SelectedID, scout.Reasons)
	}
	classifier := cognition.Route(requirement(cognition.RoleClassifier), policy, endpoints)
	if classifier.SelectedID != "ollama:local-small" {
		t.Errorf("classifier = %q, want the local endpoint (structured output probed)", classifier.SelectedID)
	}
}

// Scenario C: no local model at all, remote implementation available.
func TestNoLocalModelStillRoutesImplementation(t *testing.T) {
	remote := cognition.Ready(cognition.CLIEndpoint("cli:codex-cli", "openai"))
	remote = cognition.WithCapability(remote, protocol.CapabilityImplementation,
		protocol.GradeStrong, protocol.ProvenanceConfigured)
	decision := cognition.Route(requirement(cognition.RoleImplementer), permissivePolicy(),
		[]protocol.CognitionEndpoint{remote})
	if decision.Outcome != cognition.OutcomeSelected || decision.SelectedID != "cli:codex-cli" {
		t.Fatalf("a no-local-model machine failed to route implementation: %+v", decision)
	}
	if err := decision.Err(); err != nil {
		t.Errorf("a successful decision reported an error: %v", err)
	}
}

// Scenario D: fully offline. Model-dependent roles are explicitly unavailable
// and the result is typed, not a crash.
func TestOfflineMachineReportsModelRolesUnavailable(t *testing.T) {
	decisions := cognition.RouteAll(cognition.DefaultRequirements(), cognition.DefaultPolicy(), nil)
	if len(decisions) != len(cognition.DefaultRequirements()) {
		t.Fatalf("expected a decision per role, got %d", len(decisions))
	}
	for _, decision := range decisions {
		if decision.Outcome != cognition.OutcomeNoEligibleEndpoint {
			t.Errorf("role %s selected %q with no endpoints at all", decision.Role, decision.SelectedID)
		}
		if !reasonsContain(decision.Reasons, "no cognition endpoint was discovered") {
			t.Errorf("role %s did not explain the absence: %v", decision.Role, decision.Reasons)
		}
		err := decision.Err()
		if err == nil {
			t.Errorf("role %s produced no typed error", decision.Role)
			continue
		}
		// A policy or capability outcome must be routable, not mistaken for a
		// defect (DCI-104).
		if !errors.Is(err, errs.ErrNoEligibleEndpoint) {
			t.Errorf("role %s error category = %q, want no_eligible_endpoint",
				decision.Role, errs.CategoryOf(err))
		}
	}
}

// Scenario E: privacy policy denies remote cognition, and the correct answer is
// an explainable "no eligible endpoint" rather than a silent fallback.
func TestPrivacyPolicyRejectsRemoteWithoutSilentFallback(t *testing.T) {
	policy := cognition.Policy{
		MaxSourceExposure: protocol.ExposureLocalOnly,
		MaxCostClass:      protocol.CostRemoteStrong,
	}
	decision := cognition.Route(requirement(cognition.RoleImplementer), policy, hybridThin())
	if decision.Outcome != cognition.OutcomeNoEligibleEndpoint {
		t.Fatalf("remote implementation was selected despite a local_only policy: %+v", decision)
	}
	rejection, found := rejectionFor(decision, "cli:codex-cli")
	if !found {
		t.Fatal("the remote endpoint was not explained")
	}
	if rejection.Eligible {
		t.Error("a policy-excluded endpoint was marked eligible")
	}
	if !reasonsContain(rejection.Reasons, "exceeds the project policy of local_only") {
		t.Errorf("the policy rejection was not explained: %v", rejection.Reasons)
	}
	if !reasonsContain(decision.Reasons, "source exposure") {
		t.Errorf("the overall reason did not name the privacy constraint: %v", decision.Reasons)
	}
	// The local endpoint is allowed by policy and still cannot implement; both
	// facts must be visible.
	local, found := rejectionFor(decision, "ollama:local-small")
	if !found || local.Eligible {
		t.Error("the local endpoint's capability shortfall was not reported")
	}
}

// TestCostPolicyPrefersTheEconomicalEligibleEndpoint covers the cost-routing
// requirement, including that one provider may expose several cost classes.
func TestCostPolicyPrefersTheEconomicalEligibleEndpoint(t *testing.T) {
	economy := cognition.Ready(cognition.RemoteEndpoint("api:economy", "acme", protocol.CostRemoteEconomy))
	economy = cognition.WithCapability(economy, protocol.CapabilityImplementation,
		protocol.GradeStrong, protocol.ProvenanceConfigured)
	frontier := cognition.Ready(cognition.RemoteEndpoint("api:frontier", "acme", protocol.CostFrontierExpensive))
	frontier = cognition.WithCapability(frontier, protocol.CapabilityImplementation,
		protocol.GradeStrong, protocol.ProvenanceEvaluated)

	policy := cognition.Policy{
		MaxSourceExposure: protocol.ExposureFocusedSnippets,
		MaxCostClass:      protocol.CostFrontierExpensive,
	}
	decision := cognition.Route(requirement(cognition.RoleImplementer), policy,
		[]protocol.CognitionEndpoint{frontier, economy})
	if decision.SelectedID != "api:economy" {
		t.Fatalf("selected = %q, want the economical endpoint", decision.SelectedID)
	}
	rejection, _ := rejectionFor(decision, "api:frontier")
	if !rejection.Eligible {
		t.Error("the frontier endpoint should be eligible but not chosen")
	}
	if !reasonsContain(rejection.Reasons, "more expensive") {
		t.Errorf("the cost comparison was not explained: %v", rejection.Reasons)
	}

	// Tightening the cost policy excludes it outright, with a different reason.
	policy.MaxCostClass = protocol.CostRemoteEconomy
	tightened := cognition.Route(requirement(cognition.RoleImplementer), policy,
		[]protocol.CognitionEndpoint{frontier, economy})
	excluded, _ := rejectionFor(tightened, "api:frontier")
	if excluded.Eligible {
		t.Error("a cost-excluded endpoint was marked eligible")
	}
	if !reasonsContain(excluded.Reasons, "exceeds the project policy") {
		t.Errorf("the cost exclusion was not explained: %v", excluded.Reasons)
	}
}

func TestUnhealthyAndUnauthenticatedEndpointsAreRejected(t *testing.T) {
	base := cognition.WithCapability(cognition.CLIEndpoint("cli:a", "acme"),
		protocol.CapabilityImplementation, protocol.GradeStrong, protocol.ProvenanceConfigured)

	unhealthy := base
	unhealthy.ID = "cli:unhealthy"
	unhealthy.Health = protocol.EndpointHealthUnhealthy

	installedOnly := base
	installedOnly.ID = "cli:installed-only"
	installedOnly.Health = protocol.EndpointHealthInstalled

	unverified := base
	unverified.ID = "cli:unverified"
	unverified.Health = protocol.EndpointHealthUnverified

	unauthenticated := cognition.Ready(base)
	unauthenticated.ID = "cli:unauthenticated"
	unauthenticated.Auth = protocol.AuthUnauthenticated

	expired := cognition.Ready(base)
	expired.ID = "cli:expired"
	expired.Auth = protocol.AuthExpired

	decision := cognition.Route(requirement(cognition.RoleImplementer), permissivePolicy(),
		[]protocol.CognitionEndpoint{unhealthy, installedOnly, unverified, unauthenticated, expired})
	if decision.Outcome != cognition.OutcomeNoEligibleEndpoint {
		t.Fatalf("an unusable endpoint was selected: %+v", decision)
	}
	for _, id := range []string{"cli:unhealthy", "cli:installed-only", "cli:unverified"} {
		rejection, found := rejectionFor(decision, id)
		if !found {
			t.Fatalf("%s was not explained", id)
		}
		if !reasonsContain(rejection.Reasons, "only a probed-ready endpoint may be routed to") {
			t.Errorf("%s rejection = %v", id, rejection.Reasons)
		}
	}
	for _, id := range []string{"cli:unauthenticated", "cli:expired"} {
		rejection, _ := rejectionFor(decision, id)
		if !reasonsContain(rejection.Reasons, "authentication state is") {
			t.Errorf("%s rejection = %v", id, rejection.Reasons)
		}
	}
}

// TestUnknownAuthenticationIsNotARejection reflects the honest default: no
// supported CLI publishes a safe way to ask, and a probe that answered is
// stronger evidence than a status command.
func TestUnknownAuthenticationIsNotARejection(t *testing.T) {
	endpoint := cognition.Ready(cognition.CLIEndpoint("cli:codex-cli", "openai"))
	endpoint = cognition.WithCapability(endpoint, protocol.CapabilityImplementation,
		protocol.GradeStrong, protocol.ProvenanceConfigured)
	if endpoint.Auth != protocol.AuthUnknown {
		t.Fatalf("fixture auth = %q, want unknown", endpoint.Auth)
	}
	decision := cognition.Route(requirement(cognition.RoleImplementer), permissivePolicy(),
		[]protocol.CognitionEndpoint{endpoint})
	if decision.Outcome != cognition.OutcomeSelected {
		t.Fatalf("unknown authentication blocked a healthy endpoint: %+v", decision)
	}
	if !reasonsContain(decision.Reasons, "authentication state is unknown") {
		t.Errorf("the decision did not record that authentication is unconfirmed: %v", decision.Reasons)
	}
}

// TestUngradedCapabilityIsNotEvidence is the anti-folklore rule: an endpoint
// nobody measured cannot satisfy a graded requirement.
func TestUngradedCapabilityIsNotEvidence(t *testing.T) {
	endpoint := cognition.Ready(cognition.LocalEndpoint("ollama:mystery", "ollama", "coder-70b-instruct"))
	decision := cognition.Route(requirement(cognition.RoleImplementer), permissivePolicy(),
		[]protocol.CognitionEndpoint{endpoint})
	if decision.Outcome != cognition.OutcomeNoEligibleEndpoint {
		t.Fatalf("a model named 'coder' was routed implementation work on its name alone: %+v", decision)
	}
	if !reasonsContain(decision.Reasons, "an ungraded endpoint is not evidence of capability") {
		t.Errorf("the reason did not state the rule: %v", decision.Reasons)
	}

	// An insufficient grade is a different, separately reported condition.
	weak := cognition.WithCapability(endpoint, protocol.CapabilityImplementation,
		protocol.GradeLow, protocol.ProvenanceEvaluated)
	weakDecision := cognition.Route(requirement(cognition.RoleImplementer), permissivePolicy(),
		[]protocol.CognitionEndpoint{weak})
	rejection, _ := rejectionFor(weakDecision, "ollama:mystery")
	if !reasonsContain(rejection.Reasons, "capability is low, below the required strong") {
		t.Errorf("an insufficient grade was not distinguished from an unknown one: %v", rejection.Reasons)
	}
}

func TestVerifiedAccelerationRequirementNeedsRealEvidence(t *testing.T) {
	requirement := cognition.RoleRequirement{
		Role: cognition.RoleScout, Dimension: protocol.CapabilityRepositoryReasoning,
		RequireVerifiedAcceleration: true,
	}
	unverified := cognition.Ready(cognition.LocalEndpoint("ollama:cpu", "ollama", "small"))
	decision := cognition.Route(requirement, cognition.DefaultPolicy(),
		[]protocol.CognitionEndpoint{unverified})
	if decision.Outcome != cognition.OutcomeNoEligibleEndpoint {
		t.Fatal("an unverified endpoint satisfied a verified-acceleration requirement")
	}

	verified := cognition.WithVerifiedAcceleration(
		cognition.Ready(cognition.LocalEndpoint("ollama:gpu", "ollama", "small")),
		protocol.BackendVulkan, at())
	ok := cognition.Route(requirement, cognition.DefaultPolicy(),
		[]protocol.CognitionEndpoint{verified})
	if ok.SelectedID != "ollama:gpu" {
		t.Fatalf("a verified endpoint was not selected: %+v", ok)
	}
	if !reasonsContain(ok.Reasons, "acceleration is verified on backend vulkan") {
		t.Errorf("the acceleration evidence was not cited: %v", ok.Reasons)
	}

	// A verified CPU backend is not acceleration, and must not satisfy it.
	cpu := cognition.WithVerifiedAcceleration(
		cognition.Ready(cognition.LocalEndpoint("ollama:cpu-verified", "ollama", "small")),
		protocol.BackendCPU, at())
	cpuDecision := cognition.Route(requirement, cognition.DefaultPolicy(),
		[]protocol.CognitionEndpoint{cpu})
	if cpuDecision.Outcome != cognition.OutcomeNoEligibleEndpoint {
		t.Error("a verified cpu backend satisfied a non-cpu acceleration requirement")
	}
}

// TestOperatorPreferenceOrdersButCannotOverridePolicy keeps preference from
// becoming a privacy bypass.
func TestOperatorPreferenceOrdersButCannotOverridePolicy(t *testing.T) {
	endpoints := strongLocal()
	policy := permissivePolicy()
	policy.Preferred = []cognition.Role2Endpoints{
		{Role: cognition.RoleImplementer, Endpoints: []string{"cli:codex-cli", "ollama:local-strong"}},
	}
	decision := cognition.Route(requirement(cognition.RoleImplementer), policy, endpoints)
	if decision.SelectedID != "cli:codex-cli" {
		t.Fatalf("operator preference was ignored: %+v", decision)
	}
	rejection, _ := rejectionFor(decision, "ollama:local-strong")
	if !reasonsContain(rejection.Reasons, "operator's preference order") {
		t.Errorf("the preference was not cited as the reason: %v", rejection.Reasons)
	}

	// The same preference must not survive a policy that forbids the endpoint.
	policy.MaxSourceExposure = protocol.ExposureLocalOnly
	restricted := cognition.Route(requirement(cognition.RoleImplementer), policy, endpoints)
	if restricted.SelectedID != "ollama:local-strong" {
		t.Fatalf("preference overrode the privacy policy: %+v", restricted)
	}
}

// TestRoutingIsDeterministicIncludingTies is what makes decisions reproducible
// and diffable.
func TestRoutingIsDeterministicIncludingTies(t *testing.T) {
	first := cognition.WithCapability(
		cognition.Ready(cognition.RemoteEndpoint("api:bbb", "acme", protocol.CostRemoteEconomy)),
		protocol.CapabilityImplementation, protocol.GradeStrong, protocol.ProvenanceConfigured)
	second := cognition.WithCapability(
		cognition.Ready(cognition.RemoteEndpoint("api:aaa", "acme", protocol.CostRemoteEconomy)),
		protocol.CapabilityImplementation, protocol.GradeStrong, protocol.ProvenanceConfigured)

	forward := cognition.Route(requirement(cognition.RoleImplementer), permissivePolicy(),
		[]protocol.CognitionEndpoint{first, second})
	reversed := cognition.Route(requirement(cognition.RoleImplementer), permissivePolicy(),
		[]protocol.CognitionEndpoint{second, first})
	if forward.SelectedID != reversed.SelectedID {
		t.Fatalf("input order changed the selection: %q vs %q", forward.SelectedID, reversed.SelectedID)
	}
	// Ties break on identifier, so the answer is predictable rather than
	// dependent on discovery order.
	if forward.SelectedID != "api:aaa" {
		t.Errorf("tie broken to %q, want the lexicographically first id", forward.SelectedID)
	}
	if forward.Explain() != reversed.Explain() {
		t.Error("the rendered explanation depends on input order")
	}
}

// TestExplainAnswersBothQuestions checks the explainability requirement at the
// level an operator actually reads.
func TestExplainAnswersBothQuestions(t *testing.T) {
	decision := cognition.Route(requirement(cognition.RoleImplementer), permissivePolicy(), hybridThin())
	rendered := decision.Explain()
	for _, expected := range []string{
		"implementer:", "selected: cli:codex-cli", "reasons:", "rejected:", "ollama:local-small:",
	} {
		if !strings.Contains(rendered, expected) {
			t.Errorf("the explanation does not contain %q:\n%s", expected, rendered)
		}
	}
}

// TestAnUnsetPolicyFailsClosed is the security property of the zero value: a
// caller that forgot to configure a policy must not thereby authorise remote
// cognition.
func TestAnUnsetPolicyFailsClosed(t *testing.T) {
	decision := cognition.Route(requirement(cognition.RoleImplementer), cognition.Policy{}, hybridThin())
	if decision.SelectedID != "" {
		t.Fatalf("an empty policy selected %q", decision.SelectedID)
	}
	rejection, _ := rejectionFor(decision, "cli:codex-cli")
	if !reasonsContain(rejection.Reasons, "local_only") {
		t.Errorf("an empty policy did not default to local_only: %v", rejection.Reasons)
	}

	// A local endpoint is still usable under the closed default, so failing
	// closed reduces capability rather than disabling the machine.
	local := cognition.WithCapability(
		cognition.Ready(cognition.LocalEndpoint("ollama:strong", "ollama", "big")),
		protocol.CapabilityImplementation, protocol.GradeStrong, protocol.ProvenanceConfigured)
	allowed := cognition.Route(requirement(cognition.RoleImplementer), cognition.Policy{},
		[]protocol.CognitionEndpoint{local})
	if allowed.SelectedID != "ollama:strong" {
		t.Errorf("the closed default also excluded a local endpoint: %+v", allowed)
	}
}

// TestNoScoreAppearsInAnyExplanation guards against a numeric ranking creeping
// back in.
func TestNoScoreAppearsInAnyExplanation(t *testing.T) {
	for _, decision := range cognition.RouteAll(cognition.DefaultRequirements(),
		permissivePolicy(), append(strongLocal(), hybridThin()...)) {
		rendered := strings.ToLower(decision.Explain())
		for _, forbidden := range []string{"score", "weight", "confidence", "rank ="} {
			if strings.Contains(rendered, forbidden) {
				t.Errorf("role %s explanation contains %q, which is not an auditable reason:\n%s",
					decision.Role, forbidden, rendered)
			}
		}
	}
}
