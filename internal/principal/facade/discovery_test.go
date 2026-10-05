package facade_test

import (
	"context"
	"testing"

	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/principal/facade"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/testsupport"
)

func TestProjectStateDiscoveryFocusCarriesReadOnlyAnalysis(t *testing.T) {
	r := newRig(t, nil)
	r.initProject("", "")
	ctx := context.Background()

	empty, _ := r.svc.ProjectState(ctx, r.caller, facade.ProjectStateRequest{Meta: r.meta(""), Focus: "discovery"})
	requireOK(t, empty.Envelope)
	requireSchema(t, "principal-project-state-response", empty)
	if a := empty.Result.DiscoveryAnalysis; a == nil || a.PositiveReadiness || len(a.Blockers) == 0 {
		t.Fatalf("empty project must not look ready: %+v", empty.Result.DiscoveryAnalysis)
	}

	m := &protocol.ProblemModel{SchemaVersion: protocol.SchemaVersion1, ProblemModelID: "pm_1", ProjectID: project,
		Revision: 1, ProblemStatement: "s", DesiredOutcomes: []string{"o"}}
	r.append(&events.ProblemModelRevised{ProblemModelID: "pm_1", Revision: 1, Summary: "s",
		RecordDigest: testsupport.Digest(t, m)}, []controlplane.RecordToStore{{Version: 1, Record: m}})
	r.append(&events.AmbiguityOpened{AmbiguityID: "AQ-1", AmbiguityLedgerID: "al_1", Question: "q",
		ResolutionAuthority: protocol.ResolveByHuman, ArchitecturalImpact: protocol.ImpactHigh,
		CostOfWrongAssumption: protocol.ImpactHigh, WhyItMatters: "w"}, nil)

	resp, _ := r.svc.ProjectState(ctx, r.caller, facade.ProjectStateRequest{Meta: r.meta(""), Focus: "discovery"})
	requireOK(t, resp.Envelope)
	requireSchema(t, "principal-project-state-response", resp)
	a := resp.Result.DiscoveryAnalysis
	if a == nil || len(a.OpenQuestions) != 1 || a.OpenQuestions[0].ID != "AQ-1" || a.PositiveReadiness {
		t.Fatalf("unexpected %+v", a)
	}

	// Historical revisions and other focuses carry no analysis.
	hist, _ := r.svc.ProjectState(ctx, r.caller, facade.ProjectStateRequest{
		Meta: r.meta(""), Focus: "discovery", AtRevision: resp.Result.SourceRevision})
	requireOK(t, hist.Envelope)
	if hist.Result.DiscoveryAnalysis != nil {
		t.Fatal("analysis served for a historical revision")
	}
	proj, _ := r.svc.ProjectState(ctx, r.caller, facade.ProjectStateRequest{Meta: r.meta("")})
	if proj.Result.DiscoveryAnalysis != nil {
		t.Fatal("analysis served outside focus discovery")
	}
}

// Human-confirming and persisting discovery paths are denied with zero effect,
// whatever the grants, and model output cannot change that.
func TestDiscoveryWritesAreDenied(t *testing.T) {
	r := newRig(t, nil)
	r.initProject("", "")
	caller := r.caller
	caller.AllowedActions = append(facade.ToolNames(), facade.DiscoveryToolNames()...)
	want := map[string]string{
		"initialize_project":             principal.CodeNeedsPrincipal,
		"record_problem_model":           principal.CodeNeedsPrincipal,
		"record_ambiguities":             principal.CodeNeedsPrincipal,
		"record_product_decision":        principal.CodeNeedsHuman,
		"record_requirements":            principal.CodeNeedsHuman,
		"record_discovery_experiment":    principal.CodeNeedsPrincipal,
		"review_specification":           principal.CodeModelUnavailable,
		"record_specification_readiness": principal.CodeNeedsPrincipal,
	}
	before, rev := r.eventCount(), r.revision()
	for tool, code := range want {
		env := r.svc.DiscoveryWrite(context.Background(), caller, r.meta(rev), tool)
		requireCode(t, env, code)
		if len(env.Error.EvidenceRefs) != 1 || env.Error.EvidenceRefs[0] != facade.DiscoveryWriteUnavailableRef {
			t.Fatalf("%s: refs %v", tool, env.Error.EvidenceRefs)
		}
	}
	if r.eventCount() != before || r.revision() != rev {
		t.Fatal("a denied discovery write changed the journal")
	}

	// No grant: POLICY_DENIED. Reads and unknown tools are not write paths.
	requireCode(t, r.svc.DiscoveryWrite(context.Background(), r.caller, r.meta(rev), "record_product_decision"), principal.CodePolicyDenied)
	requireCode(t, r.svc.DiscoveryWrite(context.Background(), caller, r.meta(rev), "discovery_state"), principal.CodeInvalidArgument)
	requireCode(t, r.svc.DiscoveryWrite(context.Background(), caller, r.meta(rev), "accept"), principal.CodeInvalidArgument)

	// A policy resolver denial wins over a grant.
	denied := newRig(t, nil)
	denied.initProject("", "")
	svc, err := facade.NewService(facade.Options{ControlPlane: denied.h.Service, Policy: denyAll{}, Operations: denied.ops})
	if err != nil {
		t.Fatal(err)
	}
	requireCode(t, svc.DiscoveryWrite(context.Background(), caller, denied.meta(""), "record_requirements"), principal.CodePolicyDenied)
}
