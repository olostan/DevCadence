// Package discovery is the read side of WP-M5-3: it reconstructs the Day-0
// discovery picture from durable records alone (the event journal and the
// digest-verified record store), checks the Ambiguity Ledger against the
// journal, and derives what still blocks Specification Readiness.
//
// Nothing here writes, confirms human intent, issues or verifies receipts, or
// treats a model-authored claim as authority. Human-confirming and persisting
// operations remain denied by the facade until the protected operator ingress
// and human-receipt design (M5-R4) exists.
package discovery

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/storage"
)

// Source is the read-only slice of the control plane the analysis needs.
// *controlplane.Service satisfies it.
type Source interface {
	Events(ctx context.Context, query storage.EventQuery) ([]events.Event, error)
	LatestRecord(ctx context.Context, projectID, kind, id string) (*storage.StoredRecord, error)
}

// Blocker codes explain why positive readiness cannot be derived.
const (
	BlockerNoProblemModel          = "no_problem_model"
	BlockerProblemModelUnbound     = "problem_model_record_missing"
	BlockerNoLedger                = "no_ambiguity_ledger"
	BlockerLedgerUnbound           = "ledger_record_missing"
	BlockerOpenMaterialAmbiguities = "open_material_ambiguities"
	BlockerAwaitingHuman           = "awaiting_human_questions"
	BlockerUnverifiedAssumptions   = "unverified_material_assumptions"
	BlockerConsistencyViolations   = "consistency_violations"
	BlockerReflectionUnverifiable  = "human_reflection_unverifiable"
	BlockerReviewUnverifiable      = "independent_review_unverifiable"
	BlockerNoRecordedReadiness     = "no_recorded_readiness"
	BlockerStaleRecordedReadiness  = "recorded_readiness_stale"
)

// Violation codes of the consistency check.
const (
	ViolationLedgerIdentity        = "ledger_identity_mismatch"
	ViolationLedgerNotOpenInJrnl   = "ledger_live_entry_not_open_in_journal"
	ViolationJournalOpenNotLive    = "journal_open_question_not_live_in_ledger"
	ViolationAuthorityMismatch     = "question_authority_mismatch"
	ViolationImpactMismatch        = "question_impact_mismatch"
	ViolationBlankResolution       = "terminal_entry_blank_resolution"
	ViolationBlankBoundary         = "deferred_entry_blank_boundary"
	ViolationHumanNoDecision       = "human_resolution_without_active_decision"
	ViolationNonHumanNoEvidence    = "non_human_resolution_without_evidence"
	ViolationJournalFold           = "journal_fold_anomaly"
	ViolationProjectionDisagree    = "journal_projection_disagreement"
	ViolationProblemRevisionBehind = "problem_model_record_revision_mismatch"
)

// OpenQuestion is one unresolved question as the journal reconstructs it.
type OpenQuestion struct {
	ID                  string                       `json:"id"`
	ResolutionAuthority protocol.ResolutionAuthority `json:"resolution_authority"`
	ArchitecturalImpact protocol.ImpactLevel         `json:"architectural_impact"`
}

// DecisionStatus is the journal-folded status of one product decision.
type DecisionStatus struct {
	DecisionID string                         `json:"decision_id"`
	Status     protocol.ProductDecisionStatus `json:"status"`
}

// Violation is one detected inconsistency between durable records.
type Violation struct {
	Code    string `json:"code"`
	Subject string `json:"subject"`
}

// RecordedReadiness is a readiness verdict that was recorded in the journal.
// It is reported, never trusted: Current only says it judged the current
// ProblemModel revision. A superseded assessment carries no verdict.
type RecordedReadiness struct {
	Ref     string                    `json:"ref"`
	Verdict protocol.ReadinessVerdict `json:"verdict,omitempty"`
	Current bool                      `json:"current"`
}

// Analysis is the read-only discovery snapshot.
type Analysis struct {
	ProblemModelID       string `json:"problem_model_id,omitempty"`
	ProblemModelRevision int    `json:"problem_model_revision,omitempty"`
	LedgerID             string `json:"ambiguity_ledger_id,omitempty"`
	// LedgerRevision is zero when no ledger record is stored (journal-only).
	LedgerRevision int `json:"ambiguity_ledger_revision,omitempty"`

	OpenQuestions    []OpenQuestion     `json:"open_questions"`
	DecisionStatuses []DecisionStatus   `json:"decision_statuses"`
	Readiness        *RecordedReadiness `json:"recorded_readiness,omitempty"`

	Violations []Violation `json:"violations"`
	Blockers   []string    `json:"blockers"`
	// PositiveReadiness is true only when nothing blocks it. In this slice
	// human reflection and independent review cannot be verified, so it is
	// always false; the field exists so a caller never infers readiness from
	// an absent blocker list.
	PositiveReadiness bool `json:"positive_readiness"`
}

var journalTypes = []events.Type{
	events.TypeProblemModelRevised, events.TypeAmbiguityOpened, events.TypeAmbiguityResolved,
	events.TypeProductDecisionRecorded,
}

// Analyze reconstructs the discovery picture from durable records only, for
// the journal prefix st describes (st is the journal-derived ProjectState the
// caller already read). It holds no state between calls, so a fresh process or
// store handle yields the same result.
func Analyze(ctx context.Context, src Source, st *protocol.ProjectState) (*Analysis, error) {
	projectID := st.ProjectID
	var watermark int64 // zero reads the whole (empty) journal
	if st.EventHighWatermark != nil {
		w, perr := strconv.ParseInt(*st.EventHighWatermark, 10, 64)
		if perr != nil {
			return nil, errs.New(errs.CategoryIntegrity, "project state has no usable event high watermark")
		}
		watermark = w
	}
	evs, err := src.Events(ctx, storage.EventQuery{ProjectID: projectID, Types: journalTypes, UpToSeq: watermark})
	if err != nil {
		return nil, err
	}
	a := &Analysis{OpenQuestions: []OpenQuestion{}, DecisionStatuses: []DecisionStatus{},
		Violations: []Violation{}, Blockers: []string{}}
	open, decisions := a.foldJournal(evs)

	d := st.Discovery
	if d == nil {
		d = &protocol.DiscoveryState{}
	}
	if d.ProblemModelID != nil {
		a.ProblemModelID = *d.ProblemModelID
	}
	if d.ProblemModelRevision != nil {
		a.ProblemModelRevision = *d.ProblemModelRevision
	}
	if d.AmbiguityLedgerID != nil {
		a.LedgerID = *d.AmbiguityLedgerID
	}
	a.crossCheckProjection(d, open)
	if d.SpecificationReadinessRef != nil && d.SpecificationReadinessVerdict != nil {
		a.Readiness = &RecordedReadiness{Ref: *d.SpecificationReadinessRef, Verdict: *d.SpecificationReadinessVerdict, Current: true}
	} else if d.SpecificationReadinessRef != nil {
		// The reducer withholds the verdict when a later model revision
		// superseded the assessment.
		a.Readiness = &RecordedReadiness{Ref: *d.SpecificationReadinessRef}
	}

	problemBound, err := a.checkProblemRecord(ctx, src, projectID)
	if err != nil {
		return nil, err
	}
	ledgerBound, err := a.checkLedger(ctx, src, projectID, open, decisions)
	if err != nil {
		return nil, err
	}
	a.deriveBlockers(d, problemBound, ledgerBound)
	return a, nil
}

// foldJournal replays the discovery events strictly by sequence. Anomalies are
// reported as violations rather than aborting, so a damaged journal is still
// described.
func (a *Analysis) foldJournal(evs []events.Event) (map[string]OpenQuestion, map[string]protocol.ProductDecisionStatus) {
	open := map[string]OpenQuestion{}
	decisions := map[string]protocol.ProductDecisionStatus{}
	anomaly := func(subject string) {
		a.Violations = append(a.Violations, Violation{Code: ViolationJournalFold, Subject: subject})
	}
	for _, e := range evs {
		switch p := e.Payload.(type) {
		case *events.AmbiguityOpened:
			if _, dup := open[p.AmbiguityID]; dup {
				anomaly(p.AmbiguityID)
			}
			open[p.AmbiguityID] = OpenQuestion{ID: p.AmbiguityID, ResolutionAuthority: p.ResolutionAuthority,
				ArchitecturalImpact: p.ArchitecturalImpact}
		case *events.AmbiguityResolved:
			if _, ok := open[p.AmbiguityID]; !ok {
				anomaly(p.AmbiguityID)
			}
			delete(open, p.AmbiguityID)
		case *events.ProductDecisionRecorded:
			if p.ResolvesAmbiguity != "" {
				if _, ok := open[p.ResolvesAmbiguity]; !ok {
					anomaly(p.ResolvesAmbiguity)
				}
				delete(open, p.ResolvesAmbiguity)
			}
			if p.Supersedes != "" {
				decisions[p.Supersedes] = protocol.ProductDecisionSuperseded
			}
			decisions[p.ProductDecisionID] = p.Status
		}
	}
	for _, q := range open {
		a.OpenQuestions = append(a.OpenQuestions, q)
	}
	sort.Slice(a.OpenQuestions, func(i, j int) bool { return a.OpenQuestions[i].ID < a.OpenQuestions[j].ID })
	for id, s := range decisions {
		a.DecisionStatuses = append(a.DecisionStatuses, DecisionStatus{DecisionID: id, Status: s})
	}
	sort.Slice(a.DecisionStatuses, func(i, j int) bool { return a.DecisionStatuses[i].DecisionID < a.DecisionStatuses[j].DecisionID })
	return open, decisions
}

// crossCheckProjection compares the independent journal fold with the
// reducer's compact counts.
func (a *Analysis) crossCheckProjection(d *protocol.DiscoveryState, open map[string]OpenQuestion) {
	material, awaiting := 0, 0
	for _, q := range open {
		switch q.ArchitecturalImpact {
		case protocol.ImpactMedium, protocol.ImpactHigh, protocol.ImpactCritical:
			material++
		}
		if q.ResolutionAuthority == protocol.ResolveByHuman {
			awaiting++
		}
	}
	if material != d.OpenMaterialAmbiguities {
		a.Violations = append(a.Violations, Violation{Code: ViolationProjectionDisagree, Subject: "open_material_ambiguities"})
	}
	if awaiting != d.AwaitingHumanAmbiguities {
		a.Violations = append(a.Violations, Violation{Code: ViolationProjectionDisagree, Subject: "awaiting_human_ambiguities"})
	}
}

func (a *Analysis) checkProblemRecord(ctx context.Context, src Source, projectID string) (bool, error) {
	if a.ProblemModelID == "" {
		return false, nil
	}
	rec, err := src.LatestRecord(ctx, projectID, "ProblemModel", a.ProblemModelID)
	if err != nil {
		return false, err
	}
	if rec == nil {
		return false, nil
	}
	if rec.Version != a.ProblemModelRevision {
		a.Violations = append(a.Violations, Violation{Code: ViolationProblemRevisionBehind, Subject: a.ProblemModelID})
	}
	return true, nil
}

func blank(s *string) bool { return s == nil || strings.TrimSpace(*s) == "" }

func (a *Analysis) checkLedger(ctx context.Context, src Source, projectID string,
	open map[string]OpenQuestion, decisions map[string]protocol.ProductDecisionStatus) (bool, error) {
	if a.LedgerID == "" {
		return false, nil
	}
	rec, err := src.LatestRecord(ctx, projectID, "AmbiguityLedger", a.LedgerID)
	if err != nil {
		return false, err
	}
	if rec == nil {
		return false, nil
	}
	var ledger protocol.AmbiguityLedger
	if err := json.Unmarshal([]byte(rec.Document), &ledger); err != nil {
		return false, err
	}
	a.LedgerRevision = ledger.Revision
	violate := func(code, subject string) {
		a.Violations = append(a.Violations, Violation{Code: code, Subject: subject})
	}
	if ledger.ProjectID != projectID || ledger.AmbiguityLedgerID != a.LedgerID {
		violate(ViolationLedgerIdentity, a.LedgerID)
	}
	live := map[string]bool{}
	for _, e := range ledger.Entries {
		terminal := e.Status == protocol.AmbiguityResolved || e.Status == protocol.AmbiguityExplicitlyDeferred
		if !terminal {
			live[e.ID] = true
			q, ok := open[e.ID]
			switch {
			case !ok:
				violate(ViolationLedgerNotOpenInJrnl, e.ID)
			case q.ResolutionAuthority != e.ResolutionAuthority:
				violate(ViolationAuthorityMismatch, e.ID)
			case q.ArchitecturalImpact != e.ArchitecturalImpact:
				violate(ViolationImpactMismatch, e.ID)
			}
			continue
		}
		if blank(e.Resolution) {
			violate(ViolationBlankResolution, e.ID)
		}
		if e.Status == protocol.AmbiguityExplicitlyDeferred && blank(e.SafeDeferralBoundary) {
			violate(ViolationBlankBoundary, e.ID)
		}
		if e.Status != protocol.AmbiguityResolved {
			continue
		}
		if e.ResolutionAuthority == protocol.ResolveByHuman {
			s, known := decisions[derefString(e.ProductDecisionRef)]
			if blank(e.ProductDecisionRef) || !known || s == protocol.ProductDecisionWithdrawn {
				violate(ViolationHumanNoDecision, e.ID)
			}
		} else if len(e.EvidenceRefs) == 0 {
			violate(ViolationNonHumanNoEvidence, e.ID)
		}
	}
	for id := range open {
		if !live[id] {
			violate(ViolationJournalOpenNotLive, id)
		}
	}
	sort.Slice(a.Violations, func(i, j int) bool {
		if a.Violations[i].Code != a.Violations[j].Code {
			return a.Violations[i].Code < a.Violations[j].Code
		}
		return a.Violations[i].Subject < a.Violations[j].Subject
	})
	return true, nil
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// deriveBlockers lists, in a fixed order, everything that prevents positive
// readiness. Reflection and independent review depend on protected human
// receipts and an execution runtime that do not exist yet, so they always
// block: the model-authored reflection revision in the journal is a claim,
// not evidence.
func (a *Analysis) deriveBlockers(d *protocol.DiscoveryState, problemBound, ledgerBound bool) {
	add := func(b string) { a.Blockers = append(a.Blockers, b) }
	switch {
	case a.ProblemModelID == "":
		add(BlockerNoProblemModel)
	case !problemBound:
		add(BlockerProblemModelUnbound)
	}
	switch {
	case a.LedgerID == "":
		add(BlockerNoLedger)
	case !ledgerBound:
		add(BlockerLedgerUnbound)
	}
	if d.OpenMaterialAmbiguities > 0 {
		add(BlockerOpenMaterialAmbiguities)
	}
	if d.AwaitingHumanAmbiguities > 0 {
		add(BlockerAwaitingHuman)
	}
	if d.MaterialAssumptionsUnverified > 0 {
		add(BlockerUnverifiedAssumptions)
	}
	if len(a.Violations) > 0 {
		add(BlockerConsistencyViolations)
	}
	add(BlockerReflectionUnverifiable)
	add(BlockerReviewUnverifiable)
	switch {
	case a.Readiness == nil:
		add(BlockerNoRecordedReadiness)
	case !a.Readiness.Current:
		add(BlockerStaleRecordedReadiness)
	}
	a.PositiveReadiness = len(a.Blockers) == 0
}
