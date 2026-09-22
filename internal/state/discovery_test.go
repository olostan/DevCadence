package state_test

import (
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/state"
	"github.com/olostan/DevCadence/internal/testsupport"
)

const testDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

// discoveryScenario builds a project that has run a round of discovery.
func discoveryScenario(t *testing.T) *testsupport.ScenarioBuilder {
	t.Helper()
	return testsupport.NewScenario(t, "example").
		Add(&events.ProjectInitialized{
			Name: "Example", MilestoneID: "M0", MilestoneTitle: "Discovery",
		}).
		Add(&events.ProblemModelRevised{
			ProblemModelID: "pm_1", Revision: 1, RecordDigest: testDigest,
			Summary:                       "Initial problem model.",
			MaterialAssumptionsUnverified: 2,
			AmbiguityLedgerID:             "al_1",
		}).
		// Material and awaiting the human.
		Add(&events.AmbiguityOpened{
			AmbiguityID: "AQ-001", AmbiguityLedgerID: "al_1",
			Question:              "Must this work fully offline?",
			ResolutionAuthority:   protocol.ResolveByHuman,
			ArchitecturalImpact:   protocol.ImpactHigh,
			CostOfWrongAssumption: protocol.ImpactCritical,
			WhyItMatters:          "It decides whether a network path may exist at all.",
		}).
		// Material, but a repository tool can settle it.
		Add(&events.AmbiguityOpened{
			AmbiguityID: "AQ-002", AmbiguityLedgerID: "al_1",
			Question:              "Does any caller construct the query positionally?",
			ResolutionAuthority:   protocol.ResolveByRepositoryTool,
			ArchitecturalImpact:   protocol.ImpactMedium,
			CostOfWrongAssumption: protocol.ImpactMedium,
			WhyItMatters:          "It decides whether the change is additive.",
		}).
		// Not material: low architectural impact.
		Add(&events.AmbiguityOpened{
			AmbiguityID: "AQ-003", AmbiguityLedgerID: "al_1",
			Question:              "Text or a rendered timeline?",
			ResolutionAuthority:   protocol.ResolveByHuman,
			ArchitecturalImpact:   protocol.ImpactLow,
			CostOfWrongAssumption: protocol.ImpactLow,
			WhyItMatters:          "Presentation only.",
		})
}

func renderDiscovery(t *testing.T, builder *testsupport.ScenarioBuilder) *protocol.DiscoveryState {
	t.Helper()
	projection, err := state.Reduce(builder.Stream())
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	projectState, err := projection.ProjectState()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return projectState.Discovery
}

// TestDiscoveryProjectionCountsOpenQuestions is the core of FR-D-012: a new
// principal session must be able to see where discovery stands from durable
// records alone.
func TestDiscoveryProjectionCountsOpenQuestions(t *testing.T) {
	discovery := renderDiscovery(t, discoveryScenario(t))
	if discovery == nil {
		t.Fatal("no discovery projection was rendered")
	}
	if discovery.ProblemModelID == nil || *discovery.ProblemModelID != "pm_1" {
		t.Fatalf("problem_model_id = %v", discovery.ProblemModelID)
	}
	if discovery.ProblemModelRevision == nil || *discovery.ProblemModelRevision != 1 {
		t.Fatalf("problem_model_revision = %v", discovery.ProblemModelRevision)
	}
	if discovery.MaterialAssumptionsUnverified != 2 {
		t.Fatalf("material_assumptions_unverified = %d, want 2", discovery.MaterialAssumptionsUnverified)
	}
	// AQ-001 (high) and AQ-002 (medium) are material; AQ-003 (low) is not.
	if discovery.OpenMaterialAmbiguities != 2 {
		t.Fatalf("open_material_ambiguities = %d, want 2", discovery.OpenMaterialAmbiguities)
	}
	// AQ-001 and AQ-003 are the human's to answer; AQ-002 is a tool's.
	if discovery.AwaitingHumanAmbiguities != 2 {
		t.Fatalf("awaiting_human_ambiguities = %d, want 2", discovery.AwaitingHumanAmbiguities)
	}
	want := []string{"AQ-001", "AQ-003"}
	if len(discovery.CurrentQuestionRefs) != len(want) {
		t.Fatalf("current_question_refs = %v, want %v", discovery.CurrentQuestionRefs, want)
	}
	for i := range want {
		if discovery.CurrentQuestionRefs[i] != want[i] {
			t.Fatalf("current_question_refs = %v, want %v", discovery.CurrentQuestionRefs, want)
		}
	}
}

// TestProductDecisionClosesItsAmbiguity is DCI-009 in the projection: the
// human's answer is what removes the question, and the decision becomes
// active.
func TestProductDecisionClosesItsAmbiguity(t *testing.T) {
	discovery := renderDiscovery(t, discoveryScenario(t).
		Add(&events.ProductDecisionRecorded{
			ProductDecisionID: "pd_1",
			Question:          "Must this work fully offline?",
			Answer:            "Yes, with no network access during normal operation.",
			RecordDigest:      testDigest,
			Status:            protocol.ProductDecisionConfirmed,
			AuthorityActor:    "project_owner",
			ResolvesAmbiguity: "AQ-001",
		}))
	if discovery.ActiveProductDecisions != 1 {
		t.Fatalf("active_product_decisions = %d, want 1", discovery.ActiveProductDecisions)
	}
	if discovery.OpenMaterialAmbiguities != 1 {
		t.Fatalf("open_material_ambiguities = %d, want 1 after AQ-001 was answered",
			discovery.OpenMaterialAmbiguities)
	}
	if len(discovery.CurrentQuestionRefs) != 1 || discovery.CurrentQuestionRefs[0] != "AQ-003" {
		t.Fatalf("current_question_refs = %v, want [AQ-003]", discovery.CurrentQuestionRefs)
	}
}

func TestSupersededProductDecisionIsNoLongerActive(t *testing.T) {
	discovery := renderDiscovery(t, discoveryScenario(t).
		Add(&events.ProductDecisionRecorded{
			ProductDecisionID: "pd_1", Question: "Offline?", Answer: "Yes.",
			RecordDigest: testDigest, Status: protocol.ProductDecisionConfirmed,
		}).
		Add(&events.ProductDecisionRecorded{
			ProductDecisionID: "pd_2", Question: "Offline?",
			Answer:       "Yes, except for an explicitly approved consultant call.",
			RecordDigest: testDigest, Status: protocol.ProductDecisionConfirmed,
			Supersedes: "pd_1",
		}))
	// Two decisions were recorded; only the superseding one is active.
	if discovery.ActiveProductDecisions != 1 {
		t.Fatalf("active_product_decisions = %d, want 1", discovery.ActiveProductDecisions)
	}
}

// TestRequirementsAreCountedByEpistemicStatus is DCI-015: an inference must
// not be counted as human-confirmed intent.
func TestRequirementsAreCountedByEpistemicStatus(t *testing.T) {
	discovery := renderDiscovery(t, discoveryScenario(t).
		Add(&events.ProductDecisionRecorded{
			ProductDecisionID: "pd_1", Question: "Offline?", Answer: "Yes.",
			RecordDigest: testDigest, Status: protocol.ProductDecisionConfirmed,
		}).
		Add(&events.RequirementRecorded{
			RequirementID: "req_1", Statement: "MUST explain an acceptance.",
			Kind: protocol.RequirementFunctional, Strength: protocol.RequirementMust,
			Status: protocol.RequirementConfirmed, SourceType: protocol.SourceProductDecision,
			SourceRef: "pd_1",
		}).
		Add(&events.RequirementRecorded{
			RequirementID: "req_2", Statement: "SHOULD read well in a terminal.",
			Kind: protocol.RequirementNonFunctional, Strength: protocol.RequirementShould,
			Status: protocol.RequirementProposed, SourceType: protocol.SourcePrincipalInference,
		}).
		Add(&events.RequirementRecorded{
			RequirementID: "req_3", Statement: "MAY export an audit report.",
			Status: protocol.RequirementDeferred, SourceType: protocol.SourcePrincipalInference,
		}))
	if discovery.ConfirmedRequirements != 1 {
		t.Fatalf("confirmed_requirements = %d, want 1", discovery.ConfirmedRequirements)
	}
	if discovery.ProposedRequirements != 1 {
		t.Fatalf("proposed_requirements = %d, want 1", discovery.ProposedRequirements)
	}
}

// TestRequirementPromotionKeepsOneIdentity checks that re-recording a
// requirement replaces its status rather than double-counting it.
func TestRequirementPromotionKeepsOneIdentity(t *testing.T) {
	discovery := renderDiscovery(t, discoveryScenario(t).
		Add(&events.ProductDecisionRecorded{
			ProductDecisionID: "pd_1", Question: "Offline?", Answer: "Yes.",
			RecordDigest: testDigest, Status: protocol.ProductDecisionConfirmed,
		}).
		Add(&events.RequirementRecorded{
			RequirementID: "req_1", Statement: "MUST explain an acceptance.",
			Status: protocol.RequirementProposed, SourceType: protocol.SourcePrincipalInference,
		}).
		Add(&events.RequirementRecorded{
			RequirementID: "req_1", Statement: "MUST explain an acceptance.",
			Status: protocol.RequirementConfirmed, SourceType: protocol.SourceProductDecision,
			SourceRef: "pd_1",
		}))
	if discovery.ConfirmedRequirements != 1 || discovery.ProposedRequirements != 0 {
		t.Fatalf("confirmed = %d, proposed = %d, want 1 and 0",
			discovery.ConfirmedRequirements, discovery.ProposedRequirements)
	}
}

// TestConfirmedRequirementEventMustTraceToAHuman is DCI-008 enforced on the
// journal, not only on the record.
func TestConfirmedRequirementEventMustTraceToAHuman(t *testing.T) {
	payload := &events.RequirementRecorded{
		RequirementID: "req_1", Statement: "MUST do the thing.",
		Status: protocol.RequirementConfirmed, SourceType: protocol.SourcePrincipalInference,
	}
	if err := payload.Validate(); err == nil {
		t.Fatal("the principal confirmed its own inference through an event")
	}
	payload.SourceType = protocol.SourceHumanStatement
	if err := payload.Validate(); err == nil {
		t.Fatal("a confirmation claiming human origin with nothing to trace to was accepted")
	}
	payload.SourceRef = "AQ-001"
	if err := payload.Validate(); err != nil {
		t.Fatalf("a referenced human-sourced confirmation was rejected: %v", err)
	}
}

// TestReadinessVerdictIsWithheldAfterTheProblemChanges is DCI-016: a verdict
// describes the revision it judged, and a later revision must not inherit it.
func TestReadinessVerdictIsWithheldAfterTheProblemChanges(t *testing.T) {
	assessed := discoveryScenario(t).
		Add(&events.SpecificationReadinessRecorded{
			ReadinessID: "sr_1", ProblemModelID: "pm_1", ProblemModelRevision: 1,
			Verdict: protocol.ReadyWithExplicitRisks, RecordDigest: testDigest,
		})
	discovery := renderDiscovery(t, assessed)
	if discovery.SpecificationReadinessVerdict == nil ||
		*discovery.SpecificationReadinessVerdict != protocol.ReadyWithExplicitRisks {
		t.Fatalf("verdict = %v, want ready_with_explicit_risks", discovery.SpecificationReadinessVerdict)
	}
	if discovery.SpecificationReadinessRef == nil || *discovery.SpecificationReadinessRef != "sr_1" {
		t.Fatalf("readiness ref = %v", discovery.SpecificationReadinessRef)
	}

	// The problem model is revised; the old verdict no longer describes it.
	revised := assessed.Add(&events.ProblemModelRevised{
		ProblemModelID: "pm_1", Revision: 2, RecordDigest: testDigest,
		Summary: "Scope widened after the human's answer.", MaterialAssumptionsUnverified: 1,
	})
	after := renderDiscovery(t, revised)
	if after.SpecificationReadinessVerdict != nil {
		t.Fatalf("a stale verdict survived a problem-model revision: %v",
			*after.SpecificationReadinessVerdict)
	}
	// The reference is kept so the assessment stays retrievable.
	if after.SpecificationReadinessRef == nil || *after.SpecificationReadinessRef != "sr_1" {
		t.Fatalf("readiness ref was lost: %v", after.SpecificationReadinessRef)
	}
}

func TestInconsistentDiscoveryHistoriesAreRejected(t *testing.T) {
	cases := []struct {
		name     string
		build    func(*testing.T) *testsupport.ScenarioBuilder
		category errs.Category
	}{
		{
			name: "problem model revision goes backwards",
			build: func(t *testing.T) *testsupport.ScenarioBuilder {
				return discoveryScenario(t).Add(&events.ProblemModelRevised{
					ProblemModelID: "pm_1", Revision: 1, RecordDigest: testDigest,
					Summary: "again", MaterialAssumptionsUnverified: 0,
				})
			},
			category: errs.CategoryConflict,
		},
		{
			name: "ambiguity opened twice",
			build: func(t *testing.T) *testsupport.ScenarioBuilder {
				return discoveryScenario(t).Add(&events.AmbiguityOpened{
					AmbiguityID: "AQ-001", AmbiguityLedgerID: "al_1", Question: "again",
					ResolutionAuthority:   protocol.ResolveByHuman,
					ArchitecturalImpact:   protocol.ImpactLow,
					CostOfWrongAssumption: protocol.ImpactLow,
					WhyItMatters:          "duplicate",
				})
			},
			category: errs.CategoryConflict,
		},
		{
			name: "resolving an ambiguity that is not open",
			build: func(t *testing.T) *testsupport.ScenarioBuilder {
				return discoveryScenario(t).Add(&events.AmbiguityResolved{
					AmbiguityID: "AQ-404", Outcome: events.AmbiguityOutcomeResolved,
					Resolution: "settled", ResolvedBy: protocol.ResolveByHuman,
				})
			},
			category: errs.CategoryIntegrity,
		},
		{
			name: "decision resolving a question that is not open",
			build: func(t *testing.T) *testsupport.ScenarioBuilder {
				return discoveryScenario(t).Add(&events.ProductDecisionRecorded{
					ProductDecisionID: "pd_1", Question: "q", Answer: "a",
					RecordDigest: testDigest, Status: protocol.ProductDecisionConfirmed,
					ResolvesAmbiguity: "AQ-404",
				})
			},
			category: errs.CategoryIntegrity,
		},
		{
			name: "readiness ahead of the current problem model revision",
			build: func(t *testing.T) *testsupport.ScenarioBuilder {
				return discoveryScenario(t).Add(&events.SpecificationReadinessRecorded{
					ReadinessID: "sr_1", ProblemModelID: "pm_1", ProblemModelRevision: 9,
					Verdict: protocol.ReadyForArchitecture, RecordDigest: testDigest,
				})
			},
			category: errs.CategoryIntegrity,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := state.Reduce(tc.build(t).Stream())
			if err == nil {
				t.Fatal("an inconsistent discovery history was accepted")
			}
			if got := errs.CategoryOf(err); got != tc.category {
				t.Fatalf("category = %s, want %s (%v)", got, tc.category, err)
			}
		})
	}
}

// TestDeferralRequiresABoundary is the rule from
// docs/DISCOVERY_AND_SPECIFICATION.md §5: an unbounded deferral is a silent
// architectural decision.
func TestDeferralRequiresABoundary(t *testing.T) {
	payload := &events.AmbiguityResolved{
		AmbiguityID: "AQ-001", Outcome: events.AmbiguityOutcomeDeferred,
		Resolution: "Not needed before the first campaign.", ResolvedBy: protocol.ResolveByPrincipal,
	}
	if err := payload.Validate(); err == nil {
		t.Fatal("an unbounded deferral was accepted")
	}
	payload.SafeDeferralBoundary = "Revisit before any long autonomous campaign."
	if err := payload.Validate(); err != nil {
		t.Fatalf("a bounded deferral was rejected: %v", err)
	}
}

// TestProjectWithoutDiscoveryOmitsTheProjection keeps ProjectState compact:
// a project that never ran discovery carries no block of zeroes.
func TestProjectWithoutDiscoveryOmitsTheProjection(t *testing.T) {
	projection, err := state.Reduce(testsupport.HappyPathScenario(t, "example").Stream())
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}
	projectState, err := projection.ProjectState()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if projectState.Discovery != nil {
		t.Fatalf("a project with no discovery carries a projection: %+v", projectState.Discovery)
	}
}

// TestDiscoveryProjectionIsDeterministic keeps the discovery fields inside
// the guarantee of ADR-0005: same journal, same bytes.
func TestDiscoveryProjectionIsDeterministic(t *testing.T) {
	stream := discoveryScenario(t).
		Add(&events.ProductDecisionRecorded{
			ProductDecisionID: "pd_1", Question: "Offline?", Answer: "Yes.",
			RecordDigest: testDigest, Status: protocol.ProductDecisionConfirmed,
		}).
		Add(&events.RequirementRecorded{
			RequirementID: "req_1", Statement: "MUST explain an acceptance.",
			Status: protocol.RequirementConfirmed, SourceType: protocol.SourceProductDecision,
			SourceRef: "pd_1",
		}).
		Add(&events.RequirementRecorded{
			RequirementID: "req_2", Statement: "SHOULD read well.",
			Status: protocol.RequirementProposed, SourceType: protocol.SourcePrincipalInference,
		}).
		Stream()

	var previous string
	for i := 0; i < 24; i++ {
		projection, err := state.Reduce(stream)
		if err != nil {
			t.Fatalf("reduce: %v", err)
		}
		projectState, err := projection.ProjectState()
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		document, err := protocol.CanonicalJSON(projectState)
		if err != nil {
			t.Fatalf("canonicalise: %v", err)
		}
		if i > 0 && string(document) != previous {
			t.Fatalf("discovery projection is not deterministic:\n%s\n%s", document, previous)
		}
		previous = string(document)
	}
}

// TestConfirmedRequirementMustNameARecordedDecision is the journal-side half
// of the provenance rule: requiring a reference only helps if the reference
// names something. Without this the journal could assert human authority
// derived from a decision nobody ever made.
func TestConfirmedRequirementMustNameARecordedDecision(t *testing.T) {
	_, err := state.Reduce(discoveryScenario(t).
		Add(&events.RequirementRecorded{
			RequirementID: "req_1", Statement: "MUST run offline.",
			Status: protocol.RequirementConfirmed, SourceType: protocol.SourceProductDecision,
			SourceRef: "pd_never_recorded",
		}).Stream())
	if err == nil {
		t.Fatal("a requirement was confirmed from a decision that was never recorded")
	}
	if got := errs.CategoryOf(err); got != errs.CategoryIntegrity {
		t.Fatalf("category = %s, want integrity (%v)", got, err)
	}
}

// TestConfirmedRequirementCannotRestOnAWithdrawnDecision: a withdrawn
// decision has had its authority retracted, so it cannot be what a
// requirement is confirmed from.
func TestConfirmedRequirementCannotRestOnAWithdrawnDecision(t *testing.T) {
	_, err := state.Reduce(discoveryScenario(t).
		Add(&events.ProductDecisionRecorded{
			ProductDecisionID: "pd_1", Question: "Offline?", Answer: "Yes.",
			RecordDigest: testDigest, Status: protocol.ProductDecisionWithdrawn,
		}).
		Add(&events.RequirementRecorded{
			RequirementID: "req_1", Statement: "MUST run offline.",
			Status: protocol.RequirementConfirmed, SourceType: protocol.SourceProductDecision,
			SourceRef: "pd_1",
		}).Stream())
	if err == nil {
		t.Fatal("a requirement was confirmed from a withdrawn decision")
	}
	if got := errs.CategoryOf(err); got != errs.CategoryIntegrity {
		t.Fatalf("category = %s, want integrity (%v)", got, err)
	}
}
