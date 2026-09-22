package state

import (
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/protocol"
)

// discoveryState is the reducer's working set for the Day-0 discovery
// projection of docs/PROJECT_STATE.md §17.
//
// It holds the open questions and current statuses rather than the documents
// themselves: FR-D-012 asks that a new principal session reconstruct current
// product intent from durable artifacts, and ProjectState is the compact
// index into them, not a copy of them (DCI-010).
type discoveryState struct {
	problemModelID       string
	problemModelRevision int
	ambiguityLedgerID    string
	// materialAssumptionsUnverified comes from the latest ProblemModel
	// revision; it is what DCI-016 weighs before architecture starts.
	materialAssumptionsUnverified int

	// openAmbiguities holds only unresolved questions, keyed by id. Resolved
	// ones leave current state and remain in the journal.
	openAmbiguities map[string]openAmbiguity
	ambiguityOrder  []string

	// requirementStatus is the current epistemic status per requirement
	// (DCI-015). A requirement promoted from proposed to confirmed keeps one
	// identity.
	requirementStatus map[string]protocol.RequirementStatus
	// productDecisionStatus tracks confirmed/superseded/withdrawn so that
	// "active" means confirmed, not merely "ever recorded".
	productDecisionStatus map[string]protocol.ProductDecisionStatus

	readinessVerdict protocol.ReadinessVerdict
	readinessRef     string
	// readinessProblemModelRevision is the revision the verdict judged. A
	// later revision invalidates the verdict rather than inheriting it.
	readinessProblemModelRevision int
}

// openAmbiguity is the part of a ledger entry ProjectState needs.
type openAmbiguity struct {
	authority protocol.ResolutionAuthority
	// architecturalImpact is what makes an ambiguity material.
	architecturalImpact protocol.ImpactLevel
}

// material reports whether an open ambiguity is material.
//
// docs/DISCOVERY_AND_SPECIFICATION.md §3 defines materiality as the potential
// to alter architecture, and architectural_impact is the field that grades
// exactly that. The cut is medium or higher: a low-impact question is by
// definition unlikely to alter architecture, which is the condition the
// document gives for proceeding.
func (a openAmbiguity) material() bool {
	switch a.architecturalImpact {
	case protocol.ImpactMedium, protocol.ImpactHigh, protocol.ImpactCritical:
		return true
	}
	return false
}

// awaitingHuman reports whether only the human can settle the question.
//
// An open ambiguity whose resolution authority is the human is by definition
// waiting on them, so no separate status event is needed to count it.
func (a openAmbiguity) awaitingHuman() bool {
	return a.authority == protocol.ResolveByHuman
}

func newDiscoveryState() *discoveryState {
	return &discoveryState{
		openAmbiguities:       map[string]openAmbiguity{},
		requirementStatus:     map[string]protocol.RequirementStatus{},
		productDecisionStatus: map[string]protocol.ProductDecisionStatus{},
	}
}

// active reports whether any discovery fact has been recorded. ProjectState
// omits the projection entirely until then, so a project that never ran
// discovery does not carry a block of zeroes.
func (d *discoveryState) active() bool {
	return d.problemModelID != "" || d.ambiguityLedgerID != "" ||
		len(d.openAmbiguities) > 0 || len(d.requirementStatus) > 0 ||
		len(d.productDecisionStatus) > 0 || d.readinessRef != ""
}

func (d *discoveryState) applyProblemModelRevised(p *events.ProblemModelRevised) error {
	// Revisions move forward. A lower revision arriving later would mean the
	// journal is describing a rollback as if it were progress.
	if p.ProblemModelID == d.problemModelID && p.Revision <= d.problemModelRevision {
		return errs.New(errs.CategoryConflict,
			"problem model %s revision %d does not supersede revision %d",
			p.ProblemModelID, p.Revision, d.problemModelRevision)
	}
	d.problemModelID = p.ProblemModelID
	d.problemModelRevision = p.Revision
	d.materialAssumptionsUnverified = p.MaterialAssumptionsUnverified
	if p.AmbiguityLedgerID != "" {
		d.ambiguityLedgerID = p.AmbiguityLedgerID
	}
	return nil
}

func (d *discoveryState) applyAmbiguityOpened(p *events.AmbiguityOpened) error {
	if _, exists := d.openAmbiguities[p.AmbiguityID]; exists {
		return errs.New(errs.CategoryConflict, "ambiguity %s is already open", p.AmbiguityID)
	}
	d.ambiguityLedgerID = p.AmbiguityLedgerID
	d.openAmbiguities[p.AmbiguityID] = openAmbiguity{
		authority:           p.ResolutionAuthority,
		architecturalImpact: p.ArchitecturalImpact,
	}
	d.ambiguityOrder = append(d.ambiguityOrder, p.AmbiguityID)
	return nil
}

func (d *discoveryState) applyAmbiguityResolved(p *events.AmbiguityResolved) error {
	if _, open := d.openAmbiguities[p.AmbiguityID]; !open {
		return errs.New(errs.CategoryIntegrity,
			"ambiguity %s is not open and cannot be resolved", p.AmbiguityID)
	}
	// Both outcomes leave current state: an answered question is settled, and
	// an explicitly deferred one is bounded by its safe boundary, which the
	// event required. Either way it is no longer an open question for the
	// principal, and the journal retains which of the two it was.
	delete(d.openAmbiguities, p.AmbiguityID)
	d.ambiguityOrder = removeString(d.ambiguityOrder, p.AmbiguityID)
	return nil
}

func (d *discoveryState) applyProductDecisionRecorded(p *events.ProductDecisionRecorded) error {
	if p.ResolvesAmbiguity != "" {
		if _, open := d.openAmbiguities[p.ResolvesAmbiguity]; !open {
			return errs.New(errs.CategoryIntegrity,
				"product decision %s resolves ambiguity %s, which is not open",
				p.ProductDecisionID, p.ResolvesAmbiguity)
		}
	}
	if p.Supersedes != "" {
		if _, known := d.productDecisionStatus[p.Supersedes]; !known {
			return errs.New(errs.CategoryIntegrity,
				"product decision %s supersedes %s, which was never recorded",
				p.ProductDecisionID, p.Supersedes)
		}
	}
	// Preconditions are all checked before any mutation, so a rejected event
	// leaves the projection exactly as it was.
	if p.ResolvesAmbiguity != "" {
		delete(d.openAmbiguities, p.ResolvesAmbiguity)
		d.ambiguityOrder = removeString(d.ambiguityOrder, p.ResolvesAmbiguity)
	}
	if p.Supersedes != "" {
		d.productDecisionStatus[p.Supersedes] = protocol.ProductDecisionSuperseded
	}
	d.productDecisionStatus[p.ProductDecisionID] = p.Status
	return nil
}

func (d *discoveryState) applyRequirementRecorded(p *events.RequirementRecorded) error {
	// A requirement confirmed from a product decision must name one that was
	// actually recorded. The payload already requires a reference; this is
	// what makes the reference mean something, so the journal cannot assert
	// human authority derived from a decision nobody made (DCI-008, DCI-015).
	if p.Status == protocol.RequirementConfirmed && p.SourceType == protocol.SourceProductDecision {
		status, known := d.productDecisionStatus[p.SourceRef]
		if !known {
			return errs.New(errs.CategoryIntegrity,
				"requirement %s is confirmed from product decision %s, which was never recorded",
				p.RequirementID, p.SourceRef)
		}
		// A withdrawn decision has had its authority retracted, so it cannot
		// be the thing a requirement is confirmed from. A superseded one
		// still confirmed it at the time, and history keeps that.
		if status == protocol.ProductDecisionWithdrawn {
			return errs.New(errs.CategoryIntegrity,
				"requirement %s is confirmed from product decision %s, which has been withdrawn",
				p.RequirementID, p.SourceRef)
		}
	}
	d.requirementStatus[p.RequirementID] = p.Status
	return nil
}

func (d *discoveryState) applyReadinessRecorded(p *events.SpecificationReadinessRecorded) error {
	if d.problemModelID != "" && p.ProblemModelID != d.problemModelID {
		return errs.New(errs.CategoryIntegrity,
			"readiness %s assesses problem model %s but the project's model is %s",
			p.ReadinessID, p.ProblemModelID, d.problemModelID)
	}
	if d.problemModelRevision != 0 && p.ProblemModelRevision > d.problemModelRevision {
		return errs.New(errs.CategoryIntegrity,
			"readiness %s assesses problem model revision %d, which is ahead of the current revision %d",
			p.ReadinessID, p.ProblemModelRevision, d.problemModelRevision)
	}
	d.readinessVerdict = p.Verdict
	d.readinessRef = p.ReadinessID
	d.readinessProblemModelRevision = p.ProblemModelRevision
	return nil
}

// render builds the compact ProjectState projection, or nil when no discovery
// fact has been recorded.
func (d *discoveryState) render() *protocol.DiscoveryState {
	if !d.active() {
		return nil
	}
	out := &protocol.DiscoveryState{
		MaterialAssumptionsUnverified: d.materialAssumptionsUnverified,
	}
	if d.problemModelID != "" {
		id := d.problemModelID
		revision := d.problemModelRevision
		out.ProblemModelID = &id
		out.ProblemModelRevision = &revision
	}
	if d.ambiguityLedgerID != "" {
		id := d.ambiguityLedgerID
		out.AmbiguityLedgerID = &id
	}

	// current_question_refs lists the open questions waiting on the human,
	// which is what a principal session needs in order to know what it may
	// not decide for itself (DCI-009).
	var awaiting []string
	for _, id := range d.ambiguityOrder {
		entry := d.openAmbiguities[id]
		if entry.material() {
			out.OpenMaterialAmbiguities++
		}
		if entry.awaitingHuman() {
			out.AwaitingHumanAmbiguities++
			awaiting = append(awaiting, id)
		}
	}
	out.CurrentQuestionRefs = awaiting

	for _, status := range d.requirementStatus {
		switch status {
		case protocol.RequirementConfirmed:
			out.ConfirmedRequirements++
		case protocol.RequirementProposed:
			out.ProposedRequirements++
		}
	}
	for _, status := range d.productDecisionStatus {
		if status == protocol.ProductDecisionConfirmed {
			out.ActiveProductDecisions++
		}
	}

	if d.readinessRef != "" {
		ref := d.readinessRef
		out.SpecificationReadinessRef = &ref
		verdict := d.readinessVerdict
		// A verdict assessed against an older ProblemModel revision no longer
		// describes the current one (DCI-016): the revision changed the thing
		// that was judged, so the verdict is withheld rather than inherited.
		if d.readinessProblemModelRevision == d.problemModelRevision {
			out.SpecificationReadinessVerdict = &verdict
		}
	}
	return out
}
