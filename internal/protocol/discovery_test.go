package protocol_test

import (
	"testing"

	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/protocol"
)

// TestConfirmedRequirementMustTraceToAHuman is the discovery subsystem's
// central guard (ADR-0001, DCI-005): the principal must not be able to
// confirm its own inference as something the human asked for.
func TestConfirmedRequirementMustTraceToAHuman(t *testing.T) {
	base := func(source protocol.SourceType, status protocol.RequirementStatus) *protocol.Requirement {
		return &protocol.Requirement{
			SchemaVersion: protocol.SchemaVersion1,
			RequirementID: "req_1",
			ProjectID:     "example",
			Kind:          protocol.RequirementFunctional,
			Statement:     "The system MUST explain an acceptance.",
			Strength:      protocol.RequirementMust,
			Status:        status,
			Source:        protocol.RequirementSource{Type: source},
		}
	}
	for _, source := range []protocol.SourceType{
		protocol.SourceProductDecision, protocol.SourceHumanStatement,
	} {
		if err := base(source, protocol.RequirementConfirmed).Validate(); err != nil {
			t.Errorf("confirmed requirement from %s rejected: %v", source, err)
		}
	}
	for _, source := range []protocol.SourceType{
		protocol.SourcePrincipalInference, protocol.SourceExternalFact,
		protocol.SourceRepositoryFact, protocol.SourceExperiment, protocol.SourcePolicy,
	} {
		if err := base(source, protocol.RequirementConfirmed).Validate(); err == nil {
			t.Errorf("a requirement sourced from %s was accepted as confirmed", source)
		}
		// The same source is fine for a non-confirmed status: the point is the
		// label, not the provenance.
		if err := base(source, protocol.RequirementProposed).Validate(); err != nil {
			t.Errorf("proposed requirement from %s rejected: %v", source, err)
		}
	}
}

// TestProductDecisionRequiresHumanAuthority checks that the schema's const is
// enforced by the Go type too. A product decision is by definition an answer
// only a human can give.
func TestProductDecisionRequiresHumanAuthority(t *testing.T) {
	decision := &protocol.ProductDecision{
		SchemaVersion: protocol.SchemaVersion1,
		DecisionID:    "pd_1",
		ProjectID:     "example",
		Question:      "Offline only?",
		Answer:        "Yes.",
		Authority:     protocol.ProductDecisionAuthorityHuman,
		Status:        protocol.ProductDecisionConfirmed,
	}
	if err := decision.Validate(); err != nil {
		t.Fatalf("valid product decision rejected: %v", err)
	}
	for _, authority := range []string{"principal", "policy", "consultant", ""} {
		decision.Authority = authority
		if err := decision.Validate(); err == nil {
			t.Errorf("a product decision with authority %q was accepted", authority)
		}
	}
}

// TestResolvedAmbiguityMustRecordItsResolution stops a question being closed
// without anyone saying what the answer was.
func TestResolvedAmbiguityMustRecordItsResolution(t *testing.T) {
	entry := protocol.AmbiguityEntry{
		ID: "AQ-001", Question: "q", Origin: "o", Category: "c",
		ResolutionAuthority:   protocol.ResolveByHuman,
		ArchitecturalImpact:   protocol.ImpactHigh,
		CostOfWrongAssumption: protocol.ImpactHigh,
		WhyItMatters:          "It decides the storage layout.",
		Status:                protocol.AmbiguityResolved,
	}
	ledger := &protocol.AmbiguityLedger{
		SchemaVersion: protocol.SchemaVersion1, AmbiguityLedgerID: "al_1",
		ProjectID: "example", Revision: 1, Entries: []protocol.AmbiguityEntry{entry},
	}
	if err := ledger.Validate(); err == nil {
		t.Fatal("a resolved ambiguity with no resolution was accepted")
	}
	resolution := "Offline only; confirmed by the project owner."
	ledger.Entries[0].Resolution = &resolution
	if err := ledger.Validate(); err != nil {
		t.Fatalf("valid ledger rejected: %v", err)
	}
	// Duplicate entry ids would make requirement and decision references
	// ambiguous.
	ledger.Entries = append(ledger.Entries, ledger.Entries[0])
	if err := ledger.Validate(); err == nil {
		t.Fatal("duplicate ambiguity ids were accepted")
	} else if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category = %s", errs.CategoryOf(err))
	}
}

// TestReadinessVerdictMustMatchItsChecks keeps the Design Readiness Gate from
// being passed by assertion rather than by evidence.
func TestReadinessVerdictMustMatchItsChecks(t *testing.T) {
	complete := protocol.ReadinessCheck{Status: protocol.ReadinessComplete}
	allComplete := protocol.ReadinessChecks{
		ProblemOutcome: complete, PrimaryWorkflows: complete, ScopeBoundaries: complete,
		ArchitectureSensitiveQuestions: complete, SecurityPrivacy: complete,
		MaterialExternalFacts: complete, FeasibilityAssumptions: complete,
		Contradictions: complete, IndependentReviews: complete, HumanReflection: complete,
	}
	readiness := &protocol.SpecificationReadiness{
		SchemaVersion: protocol.SchemaVersion1, ReadinessID: "sr_1", ProjectID: "example",
		ProblemModelID: "pm_1", ProblemModelRevision: 1,
		Checks: allComplete, Verdict: protocol.ReadyForArchitecture,
	}
	if err := readiness.Validate(); err != nil {
		t.Fatalf("all-complete readiness rejected: %v", err)
	}

	// An incomplete check forbids any verdict but not_ready.
	incomplete := readiness
	incomplete.Checks.SecurityPrivacy = protocol.ReadinessCheck{Status: protocol.ReadinessIncomplete}
	for _, verdict := range []protocol.ReadinessVerdict{
		protocol.ReadyForArchitecture, protocol.ReadyWithExplicitRisks,
	} {
		incomplete.Verdict = verdict
		if err := incomplete.Validate(); err == nil {
			t.Errorf("verdict %s was accepted with an incomplete check", verdict)
		}
	}
	incomplete.Verdict = protocol.NotReady
	if err := incomplete.Validate(); err != nil {
		t.Fatalf("not_ready with an incomplete check rejected: %v", err)
	}

	// An accepted risk means "ready with explicit risks", never plain ready.
	risky := &protocol.SpecificationReadiness{
		SchemaVersion: protocol.SchemaVersion1, ReadinessID: "sr_2", ProjectID: "example",
		ProblemModelID: "pm_1", ProblemModelRevision: 1,
		Checks: allComplete, Verdict: protocol.ReadyForArchitecture,
	}
	risky.Checks.ArchitectureSensitiveQuestions = protocol.ReadinessCheck{Status: protocol.ReadinessAcceptedRisk}
	if err := risky.Validate(); err == nil {
		t.Fatal("ready_for_architecture was accepted with an accepted-risk check")
	}
	risky.Verdict = protocol.ReadyWithExplicitRisks
	if err := risky.Validate(); err != nil {
		t.Fatalf("ready_with_explicit_risks rejected: %v", err)
	}

	// An unknown that is not safe to defer forbids proceeding at all.
	risky.RemainingUnknowns = []protocol.RemainingUnknown{
		{Statement: "Whether the data is regulated.", ArchitectureSafeToDefer: false},
	}
	if err := risky.Validate(); err == nil {
		t.Fatal("a verdict past not_ready was accepted with an undeferrable unknown")
	}
}

// TestCompletedExperimentMustReportAResult stops "we ran it" standing in for
// "we learned something".
func TestCompletedExperimentMustReportAResult(t *testing.T) {
	experiment := &protocol.DiscoveryExperiment{
		SchemaVersion: protocol.SchemaVersion1, ExperimentID: "exp_1", ProjectID: "example",
		Question: "q", Hypothesis: "h", Method: "m", Environment: "e",
		Status: protocol.ExperimentCompleted,
	}
	if err := experiment.Validate(); err == nil {
		t.Fatal("a completed experiment with no result was accepted")
	}
	summary := "Replay stays linear to 10,000 events."
	experiment.ResultSummary = &summary
	if err := experiment.Validate(); err != nil {
		t.Fatalf("valid experiment rejected: %v", err)
	}
	// An inconclusive experiment legitimately has no result.
	experiment.Status = protocol.ExperimentInconclusive
	experiment.ResultSummary = nil
	if err := experiment.Validate(); err != nil {
		t.Fatalf("inconclusive experiment rejected: %v", err)
	}
}

// TestProblemModelRequiresADesiredOutcome refuses a problem statement with no
// stated outcome: docs/LIFECYCLE.md §2 asks what success would look like, and
// a model that cannot answer that is not a problem model.
func TestProblemModelRequiresADesiredOutcome(t *testing.T) {
	model := &protocol.ProblemModel{
		SchemaVersion: protocol.SchemaVersion1, ProblemModelID: "pm_1",
		ProjectID: "example", Revision: 1, ProblemStatement: "Things are hard.",
	}
	if err := model.Validate(); err == nil {
		t.Fatal("a problem model with no desired outcome was accepted")
	}
	model.DesiredOutcomes = []string{"Things become less hard, measurably."}
	if err := model.Validate(); err != nil {
		t.Fatalf("valid problem model rejected: %v", err)
	}
}
