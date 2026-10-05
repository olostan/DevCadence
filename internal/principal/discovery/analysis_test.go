package discovery_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/principal/discovery"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/storage"
	"github.com/olostan/DevCadence/internal/testsupport"
)

const project = "example"

func sp(s string) *string { return &s }

func entry(id string, auth protocol.ResolutionAuthority, impact protocol.ImpactLevel, st protocol.AmbiguityStatus) protocol.AmbiguityEntry {
	return protocol.AmbiguityEntry{
		ID: id, Question: "q " + id, Origin: "design", Category: "c",
		ResolutionAuthority: auth, ArchitecturalImpact: impact,
		CostOfWrongAssumption: protocol.ImpactHigh, WhyItMatters: "matters", Status: st,
	}
}

func model(rev int) *protocol.ProblemModel {
	return &protocol.ProblemModel{
		SchemaVersion: protocol.SchemaVersion1, ProblemModelID: "pm_1", ProjectID: project, Revision: rev,
		ProblemStatement: "s", DesiredOutcomes: []string{"o"}, AmbiguityLedgerID: sp("al_1"),
	}
}

func ledger(entries ...protocol.AmbiguityEntry) *protocol.AmbiguityLedger {
	return &protocol.AmbiguityLedger{
		SchemaVersion: protocol.SchemaVersion1, AmbiguityLedgerID: "al_1", ProjectID: project, Revision: 1, Entries: entries,
	}
}

type env struct {
	t *testing.T
	h *testsupport.Harness
}

func newEnv(t *testing.T, path string) *env {
	h := testsupport.NewFileHarness(t, path)
	e := &env{t: t, h: h}
	if _, err := h.Service.InitProject(context.Background(), controlplane.InitProjectInput{
		ProjectID: project, MilestoneID: "M1", MilestoneTitle: "m",
	}); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) append(p events.Payload, recs ...controlplane.RecordToStore) {
	e.t.Helper()
	if _, err := e.h.Service.AppendTypedEvent(context.Background(), controlplane.AppendTypedEventInput{
		ProjectID: project, Payload: p, Records: recs,
	}); err != nil {
		e.t.Fatalf("append %s: %v", p.Type(), err)
	}
}

func (e *env) revise(rev int) {
	m := model(rev)
	e.append(&events.ProblemModelRevised{
		ProblemModelID: "pm_1", Revision: rev, RecordDigest: testsupport.Digest(e.t, m), Summary: "rev",
		AmbiguityLedgerID: "al_1",
	}, controlplane.RecordToStore{Version: rev, Record: m})
}

func (e *env) open(a protocol.AmbiguityEntry, recs ...controlplane.RecordToStore) {
	e.append(&events.AmbiguityOpened{
		AmbiguityID: a.ID, AmbiguityLedgerID: "al_1", Question: a.Question, ResolutionAuthority: a.ResolutionAuthority,
		ArchitecturalImpact: a.ArchitecturalImpact, CostOfWrongAssumption: a.CostOfWrongAssumption, WhyItMatters: a.WhyItMatters,
	}, recs...)
}

func (e *env) analyze() *discovery.Analysis {
	e.t.Helper()
	st, err := e.h.Service.ProjectState(context.Background(), project)
	if err != nil {
		e.t.Fatal(err)
	}
	a, err := discovery.Analyze(context.Background(), e.h.Service, st)
	if err != nil {
		e.t.Fatal(err)
	}
	return a
}

func codes(a *discovery.Analysis) []string {
	out := []string{}
	for _, v := range a.Violations {
		out = append(out, v.Code+":"+v.Subject)
	}
	return out
}

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestEmptyProjectHasNoFalseReadiness(t *testing.T) {
	e := newEnv(t, filepath.Join(t.TempDir(), "a.db"))
	a := e.analyze()
	if a.PositiveReadiness || len(a.OpenQuestions) != 0 || !has(a.Blockers, discovery.BlockerNoProblemModel) ||
		!has(a.Blockers, discovery.BlockerNoLedger) || !has(a.Blockers, discovery.BlockerNoRecordedReadiness) {
		t.Fatalf("unexpected %+v", a)
	}
}

// Reconstruction needs only durable records: a second store handle on the same
// file, with no transcript or in-memory state, yields the identical analysis.
func TestRestartReconstructsFromDurableRecordsAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	e := newEnv(t, path)
	human := entry("AQ-001", protocol.ResolveByHuman, protocol.ImpactHigh, protocol.AmbiguityAwaitingHuman)
	low := entry("AQ-002", protocol.ResolveByPrincipal, protocol.ImpactLow, protocol.AmbiguityOpen)
	e.revise(1)
	e.open(human, controlplane.RecordToStore{Version: 1, Record: ledger(human, low)})
	e.open(low)
	before := e.analyze()

	fresh := &env{t: t, h: testsupport.NewFileHarness(t, path)}
	after := fresh.analyze()
	b1, _ := json.Marshal(before)
	b2, _ := json.Marshal(after)
	if string(b1) != string(b2) {
		t.Fatalf("restart changed the analysis:\n%s\n%s", b1, b2)
	}
	if after.ProblemModelRevision != 1 || after.LedgerRevision != 1 || len(after.OpenQuestions) != 2 ||
		len(after.Violations) != 0 || after.PositiveReadiness {
		t.Fatalf("unexpected %+v", after)
	}
	for _, want := range []string{discovery.BlockerOpenMaterialAmbiguities, discovery.BlockerAwaitingHuman,
		discovery.BlockerReflectionUnverifiable, discovery.BlockerReviewUnverifiable} {
		if !has(after.Blockers, want) {
			t.Fatalf("missing blocker %s in %v", want, after.Blockers)
		}
	}
	if !reflect.DeepEqual(after.OpenQuestions[0], discovery.OpenQuestion{
		ID: "AQ-001", ResolutionAuthority: protocol.ResolveByHuman, ArchitecturalImpact: protocol.ImpactHigh}) {
		t.Fatalf("question %+v", after.OpenQuestions[0])
	}
}

func TestLedgerConsistencyViolationsAreDetected(t *testing.T) {
	e := newEnv(t, filepath.Join(t.TempDir(), "v.db"))
	opened := entry("AQ-001", protocol.ResolveByHuman, protocol.ImpactHigh, protocol.AmbiguityOpen)
	e.revise(1)
	// Journal opens AQ-001 (human/high), AQ-002 (principal/low) and AQ-004.
	// The ledger disagrees on authority for AQ-001, impact for AQ-002, lists an
	// unopened live AQ-003 and omits AQ-004.
	skew1 := entry("AQ-001", protocol.ResolveByPrincipal, protocol.ImpactHigh, protocol.AmbiguityOpen)
	skew2 := entry("AQ-002", protocol.ResolveByPrincipal, protocol.ImpactCritical, protocol.AmbiguityOpen)
	ghost := entry("AQ-003", protocol.ResolveByPrincipal, protocol.ImpactLow, protocol.AmbiguityOpen)
	blankDefer := entry("AQ-005", protocol.ResolveByPrincipal, protocol.ImpactLow, protocol.AmbiguityExplicitlyDeferred)
	blankDefer.Resolution, blankDefer.SafeDeferralBoundary = sp("  "), sp("  ")
	humanNoDecision := entry("AQ-006", protocol.ResolveByHuman, protocol.ImpactLow, protocol.AmbiguityResolved)
	humanNoDecision.Resolution, humanNoDecision.ProductDecisionRef = sp("answer"), sp("pd_missing")
	noEvidence := entry("AQ-007", protocol.ResolveByRepositoryTool, protocol.ImpactLow, protocol.AmbiguityResolved)
	noEvidence.Resolution = sp("answer")
	e.open(opened, controlplane.RecordToStore{Version: 1, Record: ledger(skew1, skew2, ghost, blankDefer, humanNoDecision, noEvidence)})
	e.open(entry("AQ-002", protocol.ResolveByPrincipal, protocol.ImpactLow, protocol.AmbiguityOpen))
	e.open(entry("AQ-004", protocol.ResolveByPrincipal, protocol.ImpactLow, protocol.AmbiguityOpen))

	a := e.analyze()
	got := codes(a)
	for _, want := range []string{
		discovery.ViolationAuthorityMismatch + ":AQ-001",
		discovery.ViolationImpactMismatch + ":AQ-002",
		discovery.ViolationLedgerNotOpenInJrnl + ":AQ-003",
		discovery.ViolationJournalOpenNotLive + ":AQ-004",
		discovery.ViolationBlankResolution + ":AQ-005",
		discovery.ViolationBlankBoundary + ":AQ-005",
		discovery.ViolationHumanNoDecision + ":AQ-006",
		discovery.ViolationNonHumanNoEvidence + ":AQ-007",
	} {
		if !has(got, want) {
			t.Errorf("missing violation %s in %v", want, got)
		}
	}
	if !has(a.Blockers, discovery.BlockerConsistencyViolations) || a.PositiveReadiness {
		t.Fatalf("violations did not block: %+v", a.Blockers)
	}
}

func TestJournalFoldAnomalyIsReportedNotFatal(t *testing.T) {
	e := newEnv(t, filepath.Join(t.TempDir(), "f.db"))
	e.revise(1)
	// A resolution of a question that was never opened is rejected by the
	// control plane, so feed the fold directly through a fake source.
	src := fakeSource{inner: e.h.Service, extra: []events.Event{{Payload: &events.AmbiguityResolved{
		AmbiguityID: "AQ-X", Outcome: events.AmbiguityOutcomeResolved, Resolution: "r", ResolvedBy: protocol.ResolveByHuman}}}}
	st, _ := e.h.Service.ProjectState(context.Background(), project)
	a, err := discovery.Analyze(context.Background(), src, st)
	if err != nil {
		t.Fatal(err)
	}
	if !has(codes(a), discovery.ViolationJournalFold+":AQ-X") {
		t.Fatalf("anomaly not reported: %v", codes(a))
	}
}

func TestRecordedPositiveReadinessIsNeverTrustedAndGoesStale(t *testing.T) {
	e := newEnv(t, filepath.Join(t.TempDir(), "r.db"))
	e.revise(1)
	done := protocol.ReadinessCheck{Status: protocol.ReadinessComplete, EvidenceRefs: []string{"e"}}
	r := &protocol.SpecificationReadiness{
		SchemaVersion: protocol.SchemaVersion1, ReadinessID: "rd_1", ProjectID: project, ProblemModelID: "pm_1",
		ProblemModelRevision: 1, Verdict: protocol.ReadyForArchitecture,
		Checks: protocol.ReadinessChecks{ProblemOutcome: done, PrimaryWorkflows: done, ScopeBoundaries: done,
			ArchitectureSensitiveQuestions: done, SecurityPrivacy: done, MaterialExternalFacts: done,
			FeasibilityAssumptions: done, Contradictions: done, IndependentReviews: done, HumanReflection: done},
	}
	e.append(&events.SpecificationReadinessRecorded{
		ReadinessID: "rd_1", ProblemModelID: "pm_1", ProblemModelRevision: 1,
		Verdict: protocol.ReadyForArchitecture, RecordDigest: testsupport.Digest(t, r),
	}, controlplane.RecordToStore{Version: 1, Record: r})
	a := e.analyze()
	if a.PositiveReadiness || a.Readiness == nil || !a.Readiness.Current || a.Readiness.Verdict != protocol.ReadyForArchitecture {
		t.Fatalf("a model-asserted verdict became positive: %+v", a)
	}
	if has(a.Blockers, discovery.BlockerNoRecordedReadiness) || !has(a.Blockers, discovery.BlockerReflectionUnverifiable) {
		t.Fatalf("blockers %v", a.Blockers)
	}
	e.revise(2)
	a = e.analyze()
	if a.Readiness == nil || a.Readiness.Current || a.Readiness.Verdict != "" || !has(a.Blockers, discovery.BlockerStaleRecordedReadiness) {
		t.Fatalf("stale readiness %+v blockers %v", a.Readiness, a.Blockers)
	}
}

func TestJournalOnlyLedgerIsUnboundAndProblemRevisionMismatchIsFlagged(t *testing.T) {
	e := newEnv(t, filepath.Join(t.TempDir(), "p.db"))
	e.revise(1)
	// A legacy journal opens a question without ever storing its ledger.
	e.open(entry("AQ-001", protocol.ResolveByPrincipal, protocol.ImpactLow, protocol.AmbiguityOpen))
	a := e.analyze()
	if !has(a.Blockers, discovery.BlockerLedgerUnbound) || a.LedgerRevision != 0 || len(a.Violations) != 0 {
		t.Fatalf("unexpected %+v", a)
	}
	src := fakeSource{inner: e.h.Service, bumpModel: true}
	st, _ := e.h.Service.ProjectState(context.Background(), project)
	a, err := discovery.Analyze(context.Background(), src, st)
	if err != nil {
		t.Fatal(err)
	}
	if !has(codes(a), discovery.ViolationProblemRevisionBehind+":pm_1") {
		t.Fatalf("revision mismatch not flagged: %v", codes(a))
	}
}

// fakeSource injects journal or record anomalies the control plane itself
// refuses to write.
type fakeSource struct {
	inner     *controlplane.Service
	extra     []events.Event
	bumpModel bool
	// dropModel hides the stored ProblemModel record.
	dropModel bool
	// mutateLedger rewrites the stored AmbiguityLedger document.
	mutateLedger func(*protocol.AmbiguityLedger)
}

func (f fakeSource) Events(ctx context.Context, q storage.EventQuery) ([]events.Event, error) {
	evs, err := f.inner.Events(ctx, q)
	return append(evs, f.extra...), err
}

func (f fakeSource) LatestRecord(ctx context.Context, p, kind, id string) (*storage.StoredRecord, error) {
	rec, err := f.inner.LatestRecord(ctx, p, kind, id)
	if err == nil && rec != nil && f.dropModel && kind == "ProblemModel" {
		return nil, nil
	}
	if err == nil && rec != nil && f.mutateLedger != nil && kind == "AmbiguityLedger" {
		var l protocol.AmbiguityLedger
		if jerr := json.Unmarshal([]byte(rec.Document), &l); jerr != nil {
			return nil, jerr
		}
		f.mutateLedger(&l)
		b, jerr := json.Marshal(&l)
		if jerr != nil {
			return nil, jerr
		}
		cp := *rec
		cp.Document = string(b)
		rec = &cp
	}
	if err == nil && rec != nil && f.bumpModel && kind == "ProblemModel" {
		rec.Version++
	}
	return rec, err
}

func (e *env) state() *protocol.ProjectState {
	e.t.Helper()
	st, err := e.h.Service.ProjectState(context.Background(), project)
	if err != nil {
		e.t.Fatal(err)
	}
	return st
}

func (e *env) analyzeWith(src discovery.Source, st *protocol.ProjectState) *discovery.Analysis {
	e.t.Helper()
	a, err := discovery.Analyze(context.Background(), src, st)
	if err != nil {
		e.t.Fatal(err)
	}
	return a
}

// The journal prefix is bounded by the supplied state's watermark: events
// appended after that state was read must not leak into the analysis.
func TestAnalyzeIsBoundedByTheStateWatermark(t *testing.T) {
	e := newEnv(t, filepath.Join(t.TempDir(), "w.db"))
	e.revise(1)
	human := entry("AQ-001", protocol.ResolveByHuman, protocol.ImpactHigh, protocol.AmbiguityAwaitingHuman)
	e.open(human, controlplane.RecordToStore{Version: 1, Record: ledger(human)})
	old := e.state()

	// Later discovery events: a new open question, then a resolution of the first.
	late := entry("AQ-002", protocol.ResolveByPrincipal, protocol.ImpactLow, protocol.AmbiguityOpen)
	e.open(late)
	e.append(&events.AmbiguityResolved{AmbiguityID: "AQ-001", Outcome: events.AmbiguityOutcomeResolved,
		Resolution: "r", ResolvedBy: protocol.ResolveByHuman})

	a := e.analyzeWith(e.h.Service, old)
	if len(a.OpenQuestions) != 1 || a.OpenQuestions[0].ID != "AQ-001" {
		t.Fatalf("later events leaked past the watermark: %+v", a.OpenQuestions)
	}
	if has(codes(a), discovery.ViolationProjectionDisagree+":open_material_ambiguities") {
		t.Fatalf("old state disagreed with its own prefix: %v", codes(a))
	}

	fresh := e.analyze()
	if len(fresh.OpenQuestions) != 1 || fresh.OpenQuestions[0].ID != "AQ-002" {
		t.Fatalf("fresh state must include later events: %+v", fresh.OpenQuestions)
	}
}

func TestLedgerIdentityMismatchIsReported(t *testing.T) {
	for name, mut := range map[string]func(*protocol.AmbiguityLedger){
		"project": func(l *protocol.AmbiguityLedger) { l.ProjectID = "other" },
		"ledger":  func(l *protocol.AmbiguityLedger) { l.AmbiguityLedgerID = "al_other" },
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, filepath.Join(t.TempDir(), "i.db"))
			e.revise(1)
			q := entry("AQ-001", protocol.ResolveByPrincipal, protocol.ImpactLow, protocol.AmbiguityOpen)
			e.open(q, controlplane.RecordToStore{Version: 1, Record: ledger(q)})
			if v := codes(e.analyze()); len(v) != 0 {
				t.Fatalf("baseline not clean: %v", v)
			}
			a := e.analyzeWith(fakeSource{inner: e.h.Service, mutateLedger: mut}, e.state())
			if !has(codes(a), discovery.ViolationLedgerIdentity+":al_1") {
				t.Fatalf("identity mismatch not reported: %v", codes(a))
			}
		})
	}
}

func TestJournalFoldAnomaliesDuplicateOpenAndDecisionOfUnopened(t *testing.T) {
	e := newEnv(t, filepath.Join(t.TempDir(), "a.db"))
	e.revise(1)
	q := entry("AQ-001", protocol.ResolveByPrincipal, protocol.ImpactLow, protocol.AmbiguityOpen)
	e.open(q)
	src := fakeSource{inner: e.h.Service, extra: []events.Event{
		{Payload: &events.AmbiguityOpened{AmbiguityID: "AQ-001", AmbiguityLedgerID: "al_1",
			ResolutionAuthority: protocol.ResolveByPrincipal, ArchitecturalImpact: protocol.ImpactLow}},
		{Payload: &events.ProductDecisionRecorded{ProductDecisionID: "pd_1", Status: protocol.ProductDecisionConfirmed,
			ResolvesAmbiguity: "AQ-Y"}},
	}}
	a := e.analyzeWith(src, e.state())
	got := codes(a)
	for _, want := range []string{discovery.ViolationJournalFold + ":AQ-001", discovery.ViolationJournalFold + ":AQ-Y"} {
		if !has(got, want) {
			t.Errorf("missing %s in %v", want, got)
		}
	}
	if len(a.DecisionStatuses) != 1 || a.DecisionStatuses[0].DecisionID != "pd_1" {
		t.Fatalf("decision status %+v", a.DecisionStatuses)
	}
}

func TestProjectionCountDisagreementIsReported(t *testing.T) {
	e := newEnv(t, filepath.Join(t.TempDir(), "d.db"))
	e.revise(1)
	human := entry("AQ-001", protocol.ResolveByHuman, protocol.ImpactHigh, protocol.AmbiguityAwaitingHuman)
	e.open(human, controlplane.RecordToStore{Version: 1, Record: ledger(human)})
	if v := codes(e.analyze()); len(v) != 0 {
		t.Fatalf("baseline not clean: %v", v)
	}
	st := e.state()
	d := *st.Discovery
	d.OpenMaterialAmbiguities, d.AwaitingHumanAmbiguities = 0, 0
	st.Discovery = &d
	got := codes(e.analyzeWith(e.h.Service, st))
	for _, want := range []string{discovery.ViolationProjectionDisagree + ":open_material_ambiguities",
		discovery.ViolationProjectionDisagree + ":awaiting_human_ambiguities"} {
		if !has(got, want) {
			t.Errorf("missing %s in %v", want, got)
		}
	}
}

func TestMissingProblemModelRecordBlocks(t *testing.T) {
	e := newEnv(t, filepath.Join(t.TempDir(), "m.db"))
	e.revise(1)
	a := e.analyzeWith(fakeSource{inner: e.h.Service, dropModel: true}, e.state())
	if !has(a.Blockers, discovery.BlockerProblemModelUnbound) || has(a.Blockers, discovery.BlockerNoProblemModel) {
		t.Fatalf("blockers %v", a.Blockers)
	}
}
