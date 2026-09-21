package state

import (
	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/events"
	"github.com/olostan/DevCadience/internal/protocol"
	"github.com/olostan/DevCadience/internal/tasks"
)

// Apply folds one event into the projection.
//
// This switch is the single place where an event is turned into a state
// change. Keeping it here rather than on the payload types leaves the events
// package a pure vocabulary of facts, and makes the whole state machine
// readable in one file.
//
// Apply is all-or-nothing: it validates every precondition before mutating
// anything, so a rejected event leaves the projection exactly as it was.
// ENGINEERING_STANDARDS.md §12 requires that of the persistent layer, and the
// persistent layer can only honour it if the in-memory reducer does too.
func (p *Projection) Apply(e *events.Event) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if e.Seq <= p.HighWatermark {
		return errs.New(errs.CategoryIntegrity,
			"event %s has sequence %d at or below the current high-watermark %d; the journal is not in order",
			e.EventID, e.Seq, p.HighWatermark)
	}
	if p.initialised {
		if e.ProjectID != p.ProjectID {
			return errs.New(errs.CategoryIntegrity,
				"event %s belongs to project %s but this projection is for %s", e.EventID, e.ProjectID, p.ProjectID)
		}
		if e.EventType == events.TypeProjectInitialized {
			return errs.New(errs.CategoryIntegrity,
				"event %s initialises project %s a second time", e.EventID, e.ProjectID)
		}
	} else if e.EventType != events.TypeProjectInitialized {
		return errs.New(errs.CategoryIntegrity,
			"event %s of type %s precedes ProjectInitialized", e.EventID, e.EventType)
	}

	if err := p.applyPayload(e); err != nil {
		return err
	}
	p.HighWatermark = e.Seq
	p.LastOccurredAt = e.OccurredAt
	return nil
}

func (p *Projection) applyPayload(e *events.Event) error {
	switch payload := e.Payload.(type) {
	case *events.ProjectInitialized:
		return p.applyProjectInitialized(e, payload)
	case *events.MilestoneStarted:
		p.Milestone = protocol.MilestoneState{ID: payload.MilestoneID, Title: payload.Title, Status: payload.Status}
		return nil
	case *events.RequirementRecorded:
		return p.discovery.applyRequirementRecorded(payload)
	case *events.ComponentDeclared:
		return p.applyComponentDeclared(payload)
	case *events.DesignCandidateCreated:
		return nil
	case *events.DecisionRequired:
		return p.applyDecisionRequired(payload)
	case *events.DecisionRecorded:
		return p.applyDecisionRecorded(payload)
	case *events.RiskRecorded:
		return p.applyRiskRecorded(payload)
	case *events.RiskResolved:
		return p.applyRiskResolved(payload)
	case *events.HealthReportRecorded:
		id := payload.ReportID
		p.Health = protocol.HealthState{
			Status:       payload.Status,
			LastReportID: &id,
			KnownDebt:    append([]string(nil), payload.KnownDebt...),
		}
		return nil

	case *events.TaskCreated:
		return p.applyTaskCreated(e, payload)
	case *events.TaskScoutingStarted:
		return p.transitionTask(e, payload.TaskID, tasks.StateScouting, nil)
	case *events.TaskDesignStarted:
		return p.transitionTask(e, payload.TaskID, tasks.StateDesigning, func(t *tasks.Task) error {
			// Leaving BLOCKED clears the block: the reason stays in the
			// journal, but current state must not claim a resolved block.
			t.Blocked = nil
			return nil
		})
	case *events.WorkPackageApproved:
		return p.applyWorkPackageApproved(e, payload)
	case *events.TaskDelegated:
		return p.applyTaskDelegated(e, payload)
	case *events.AttemptStarted:
		return p.applyAttemptStarted(e, payload)
	case *events.CandidateProduced:
		return p.applyCandidateProduced(e, payload)
	case *events.AttemptBlocked:
		return p.applyAttemptBlocked(e, payload)
	case *events.AttemptFailed:
		return p.applyAttemptFailed(e, payload)
	case *events.ValidationCompleted:
		return p.applyValidationCompleted(e, payload)
	case *events.ReviewCompleted:
		return p.applyReviewCompleted(payload)
	case *events.ChangeAccepted:
		return p.applyChangeAccepted(e, payload)
	case *events.ChangeRejected:
		return p.applyChangeRejected(e, payload)
	case *events.EscalationRaised:
		return p.applyEscalationRaised(e, payload)
	case *events.IntegrationStarted:
		return p.transitionTask(e, payload.TaskID, tasks.StateIntegrating, nil)
	case *events.IntegrationValidationStarted:
		return p.transitionTask(e, payload.TaskID, tasks.StateIntegrationValidating, nil)

	case *events.ProblemModelRevised:
		return p.discovery.applyProblemModelRevised(payload)
	case *events.AmbiguityOpened:
		return p.discovery.applyAmbiguityOpened(payload)
	case *events.AmbiguityResolved:
		return p.discovery.applyAmbiguityResolved(payload)
	case *events.ProductDecisionRecorded:
		return p.discovery.applyProductDecisionRecorded(payload)
	case *events.SpecificationReadinessRecorded:
		return p.discovery.applyReadinessRecorded(payload)
	case *events.DiscoveryExperimentStarted, *events.DiscoveryExperimentCompleted,
		*events.SpecificationReviewCompleted:
		// Recorded as durable facts. The discovery projection in
		// schemas/project-state.schema.json carries no experiment or review
		// counts, so nothing is derived from them; the records remain
		// retrievable by id.
		return nil

	case *events.LessonCandidateCreated, *events.LessonPromoted,
		*events.RefactoringEpochStarted, *events.ArchitectureReconciled:
		// Recorded in M1, acted on in M7/M8.
		return nil
	}
	// Unreachable for registered types, but a new payload added without a
	// case here must fail rather than be silently ignored.
	return errs.New(errs.CategoryInternal,
		"event %s of type %s has no reducer case", e.EventID, e.EventType)
}

func (p *Projection) applyProjectInitialized(e *events.Event, payload *events.ProjectInitialized) error {
	p.initialised = true
	p.ProjectID = e.ProjectID
	p.Name = payload.Name
	p.AcceptedCommit = payload.AcceptedCommit
	p.Branch = payload.Branch
	p.RepositoryPath = payload.RepositoryPath
	p.VisionRef = payload.VisionRef
	p.CurrentOutcome = payload.CurrentOutcome
	p.Milestone = protocol.MilestoneState{ID: payload.MilestoneID, Title: payload.MilestoneTitle}
	p.ActiveInvariants = sortedCopy(payload.ActiveInvariants)
	return nil
}

func (p *Projection) applyComponentDeclared(payload *events.ComponentDeclared) error {
	component := protocol.ComponentState{
		ID:            payload.ComponentID,
		Status:        payload.Status,
		ContractState: payload.ContractState,
		EvidenceRefs:  append([]string(nil), payload.EvidenceRefs...),
	}
	if payload.Responsibility != "" {
		responsibility := payload.Responsibility
		component.Responsibility = &responsibility
	}
	if _, exists := p.components[payload.ComponentID]; !exists {
		p.componentIDs = append(p.componentIDs, payload.ComponentID)
	}
	p.components[payload.ComponentID] = component
	return nil
}

func (p *Projection) applyDecisionRequired(payload *events.DecisionRequired) error {
	if _, exists := p.decisionsRequired[payload.DecisionRequiredID]; exists {
		return errs.New(errs.CategoryConflict,
			"decision required %s is already open", payload.DecisionRequiredID)
	}
	p.decisionsRequired[payload.DecisionRequiredID] = protocol.DecisionRequired{
		ID:           payload.DecisionRequiredID,
		Question:     payload.Question,
		Authority:    payload.Authority,
		EvidenceRefs: append([]string(nil), payload.EvidenceRefs...),
	}
	p.decisionRequiredIDs = append(p.decisionRequiredIDs, payload.DecisionRequiredID)
	return nil
}

func (p *Projection) applyDecisionRecorded(payload *events.DecisionRecorded) error {
	// Both preconditions are checked before either mutation, so a duplicate
	// decision cannot close an open question on its way to being rejected.
	if payload.ResolvesDecisionRequired != "" {
		if _, open := p.decisionsRequired[payload.ResolvesDecisionRequired]; !open {
			return errs.New(errs.CategoryIntegrity,
				"decision %s resolves %s, which is not an open question",
				payload.DecisionID, payload.ResolvesDecisionRequired)
		}
	}
	for _, existing := range p.ActiveDecisions {
		if existing == payload.DecisionID {
			return errs.New(errs.CategoryConflict, "decision %s has already been recorded", payload.DecisionID)
		}
	}
	if payload.ResolvesDecisionRequired != "" {
		delete(p.decisionsRequired, payload.ResolvesDecisionRequired)
		p.decisionRequiredIDs = removeString(p.decisionRequiredIDs, payload.ResolvesDecisionRequired)
	}
	p.ActiveDecisions = append(p.ActiveDecisions, payload.DecisionID)
	return nil
}

func (p *Projection) applyRiskRecorded(payload *events.RiskRecorded) error {
	if _, exists := p.risks[payload.RiskID]; exists {
		return errs.New(errs.CategoryConflict, "risk %s is already open", payload.RiskID)
	}
	p.risks[payload.RiskID] = protocol.Risk{
		ID:           payload.RiskID,
		Severity:     payload.Severity,
		Statement:    payload.Statement,
		EvidenceRefs: append([]string(nil), payload.EvidenceRefs...),
	}
	p.riskIDs = append(p.riskIDs, payload.RiskID)
	return nil
}

func (p *Projection) applyRiskResolved(payload *events.RiskResolved) error {
	if _, exists := p.risks[payload.RiskID]; !exists {
		return errs.New(errs.CategoryIntegrity, "risk %s is not open and cannot be resolved", payload.RiskID)
	}
	delete(p.risks, payload.RiskID)
	p.riskIDs = removeString(p.riskIDs, payload.RiskID)
	return nil
}

func (p *Projection) applyTaskCreated(e *events.Event, payload *events.TaskCreated) error {
	if _, exists := p.tasks[payload.TaskID]; exists {
		return errs.New(errs.CategoryConflict, "task %s already exists", payload.TaskID)
	}
	if owner, taken := p.aliases[payload.Alias]; taken {
		return errs.New(errs.CategoryConflict,
			"task alias %s is already used by task %s", payload.Alias, owner)
	}
	milestoneID := payload.MilestoneID
	if milestoneID == "" {
		milestoneID = p.Milestone.ID
	}
	task := &tasks.Task{
		ID:          payload.TaskID,
		ProjectID:   e.ProjectID,
		Alias:       payload.Alias,
		Title:       payload.Title,
		MilestoneID: milestoneID,
		ChangeClass: payload.ChangeClass,
		State:       tasks.StateProposed,
		CreatedSeq:  e.Seq,
		UpdatedSeq:  e.Seq,
	}
	if err := task.Validate(); err != nil {
		return err
	}
	p.tasks[task.ID] = task
	p.taskIDs = append(p.taskIDs, task.ID)
	p.aliases[task.Alias] = task.ID
	return nil
}

// transitionTask validates and performs a task state change.
//
// mutate runs after the transition is known to be legal and may adjust
// further task fields; if it fails, the task is left untouched because the
// state assignment happens only once mutate has succeeded.
func (p *Projection) transitionTask(e *events.Event, taskID string, to tasks.State, mutate func(*tasks.Task) error) error {
	task, err := p.Task(taskID)
	if err != nil {
		return err
	}
	if err := tasks.CheckTransition(task.Alias, task.State, to); err != nil {
		return err
	}
	staged := *task
	staged.State = to
	if mutate != nil {
		if err := mutate(&staged); err != nil {
			return err
		}
	}
	staged.UpdatedSeq = e.Seq
	if err := staged.Validate(); err != nil {
		return err
	}
	*task = staged
	return nil
}

func (p *Projection) applyWorkPackageApproved(e *events.Event, payload *events.WorkPackageApproved) error {
	return p.transitionTask(e, payload.TaskID, tasks.StateReady, func(t *tasks.Task) error {
		// A new blueprint version supersedes the previous one; an equal or
		// lower version would silently reopen a superseded plan.
		if t.WorkPackageID == payload.WorkPackageID && payload.WorkPackageVersion <= t.WorkPackageVersion {
			return errs.New(errs.CategoryConflict,
				"work package %s version %d does not supersede the approved version %d",
				payload.WorkPackageID, payload.WorkPackageVersion, t.WorkPackageVersion)
		}
		t.WorkPackageID = payload.WorkPackageID
		t.WorkPackageVersion = payload.WorkPackageVersion
		if payload.ChangeClass != "" {
			t.ChangeClass = payload.ChangeClass
		}
		return nil
	})
}

func (p *Projection) applyTaskDelegated(e *events.Event, payload *events.TaskDelegated) error {
	return p.transitionTask(e, payload.TaskID, tasks.StateRunning, func(t *tasks.Task) error {
		if t.WorkPackageID != payload.WorkPackageID {
			return errs.New(errs.CategoryIntegrity,
				"task %s is delegated with work package %s but %s was approved",
				t.Alias, payload.WorkPackageID, t.WorkPackageID)
		}
		return nil
	})
}

func (p *Projection) applyAttemptStarted(e *events.Event, payload *events.AttemptStarted) error {
	task, err := p.Task(payload.TaskID)
	if err != nil {
		return err
	}
	if task.State != tasks.StateRunning {
		return errs.New(errs.CategoryInvalidTransition,
			"attempt cannot start for task %s in state %s", task.Alias, task.State)
	}
	// An attempt is lineage: it records what blueprint the work was executed
	// against. Starting one against a Work Package the task never approved
	// would make that lineage a fiction, and acceptance later checks the two
	// agree, so the disagreement is caught here where it originates.
	if payload.WorkPackageID != task.WorkPackageID {
		return errs.New(errs.CategoryIntegrity,
			"attempt for task %s names work package %s, but %s is the approved one",
			task.Alias, payload.WorkPackageID, task.WorkPackageID)
	}
	if payload.WorkPackageVersion != task.WorkPackageVersion {
		return errs.New(errs.CategoryIntegrity,
			"attempt for task %s names work package %s v%d, but v%d is the approved version",
			task.Alias, payload.WorkPackageID, payload.WorkPackageVersion, task.WorkPackageVersion)
	}
	if _, exists := p.attempts[payload.AttemptID]; exists {
		return errs.New(errs.CategoryConflict, "attempt %s already exists", payload.AttemptID)
	}
	// An attempt that starts while another is still running would make
	// "which attempt produced this candidate" ambiguous.
	for _, id := range p.attemptsByTask[payload.TaskID] {
		if p.attempts[id].Status == tasks.AttemptRunning {
			return errs.New(errs.CategoryConflict,
				"task %s already has a running attempt (%s)", task.Alias, id)
		}
	}
	attempt := &tasks.Attempt{
		ID:                   payload.AttemptID,
		ProjectID:            e.ProjectID,
		TaskID:               payload.TaskID,
		Ordinal:              len(p.attemptsByTask[payload.TaskID]) + 1,
		WorkPackageID:        payload.WorkPackageID,
		WorkPackageVersion:   payload.WorkPackageVersion,
		ProjectStateRevision: payload.ProjectStateRevision,
		BaseCommit:           payload.BaseCommit,
		WorkerRole:           payload.WorkerRole,
		WorkerProfile:        payload.WorkerProfile,
		ModelIdentity:        payload.ModelIdentity,
		WorktreeID:           payload.WorktreeID,
		Status:               tasks.AttemptRunning,
		StartedAt:            e.OccurredAt,
		CreatedSeq:           e.Seq,
		UpdatedSeq:           e.Seq,
	}
	if err := attempt.Validate(); err != nil {
		return err
	}
	p.attempts[attempt.ID] = attempt
	p.attemptsByTask[payload.TaskID] = append(p.attemptsByTask[payload.TaskID], attempt.ID)
	task.CurrentAttemptID = attempt.ID
	task.UpdatedSeq = e.Seq
	return nil
}

// finishAttempt validates and applies a terminal attempt status.
func (p *Projection) finishAttempt(e *events.Event, taskID, attemptID string, to tasks.AttemptStatus, mutate func(*tasks.Attempt)) (*tasks.Attempt, error) {
	attempt, err := p.Attempt(attemptID)
	if err != nil {
		return nil, err
	}
	if attempt.TaskID != taskID {
		return nil, errs.New(errs.CategoryIntegrity,
			"attempt %s belongs to task %s, not %s", attemptID, attempt.TaskID, taskID)
	}
	if err := tasks.CheckAttemptTransition(attemptID, attempt.Status, to); err != nil {
		return nil, err
	}
	staged := *attempt
	staged.Status = to
	finished := e.OccurredAt
	staged.FinishedAt = &finished
	staged.UpdatedSeq = e.Seq
	if mutate != nil {
		mutate(&staged)
	}
	if err := staged.Validate(); err != nil {
		return nil, err
	}
	*attempt = staged
	return attempt, nil
}

func (p *Projection) applyCandidateProduced(e *events.Event, payload *events.CandidateProduced) error {
	// The task transition is checked first so that a candidate produced for a
	// task that cannot move to VALIDATING does not leave a finished attempt
	// behind. Apply is all-or-nothing, and an attempt that terminated for a
	// transition that never happened would be a half-applied event.
	task, err := p.Task(payload.TaskID)
	if err != nil {
		return err
	}
	if err := tasks.CheckTransition(task.Alias, task.State, tasks.StateValidating); err != nil {
		return err
	}
	if _, err := p.finishAttempt(e, payload.TaskID, payload.AttemptID, tasks.AttemptCandidateProduced,
		func(a *tasks.Attempt) {
			a.CandidateCommit = payload.CandidateCommit
			a.RepairIterations = payload.RepairIterations
			a.Artifacts = append(a.Artifacts, payload.Artifacts...)
		}); err != nil {
		return err
	}
	return p.transitionTask(e, payload.TaskID, tasks.StateValidating, nil)
}

func (p *Projection) applyAttemptBlocked(e *events.Event, payload *events.AttemptBlocked) error {
	reason := payload.Reason
	reason.AttemptID = payload.AttemptID
	_, err := p.finishAttempt(e, payload.TaskID, payload.AttemptID, tasks.AttemptBlocked,
		func(a *tasks.Attempt) { a.BlockReason = &reason })
	// The task is deliberately left in its current state: the control plane
	// verifies the contradiction before escalating (docs/LIFECYCLE.md §13).
	return err
}

func (p *Projection) applyAttemptFailed(e *events.Event, payload *events.AttemptFailed) error {
	status := tasks.AttemptFailed
	if payload.Cancelled {
		status = tasks.AttemptCancelled
	}
	_, err := p.finishAttempt(e, payload.TaskID, payload.AttemptID, status, func(a *tasks.Attempt) {
		a.FailureSummary = payload.Summary
		a.RepairIterations = payload.RepairIterations
		a.Artifacts = append(a.Artifacts, payload.Artifacts...)
	})
	return err
}

func (p *Projection) applyValidationCompleted(e *events.Event, payload *events.ValidationCompleted) error {
	switch payload.Scope {
	case events.ScopeBaseline:
		return p.applyBaselineValidation(payload)
	case events.ScopeAttempt:
		return p.applyAttemptValidation(e, payload)
	case events.ScopeIntegration:
		return p.applyIntegrationValidation(e, payload)
	}
	return errs.New(errs.CategoryInternal, "unhandled validation scope %s", payload.Scope)
}

// applyAttemptValidation records deterministic evidence about one attempt's
// candidate and moves the task on.
//
// Every precondition is checked before anything is mutated, so a validation
// that cites the wrong attempt or the wrong commit changes nothing.
func (p *Projection) applyAttemptValidation(e *events.Event, payload *events.ValidationCompleted) error {
	const what = "attempt validation"
	// The task must be VALIDATING for either outcome. Checking only that the
	// transition is legal would not be enough: RUNNING is also reachable from
	// REVIEWING, so a failing validation could otherwise pull a task out of
	// review without the rejection decision that is supposed to do it.
	task, err := p.requireTaskInState(payload.TaskID, tasks.StateValidating, what)
	if err != nil {
		return err
	}
	attempt, err := p.requireAttemptOf(task.ID, payload.AttemptID, what)
	if err != nil {
		return err
	}
	if err := requireCandidate(attempt, what); err != nil {
		return err
	}
	// The commit pins the evidence to a specific candidate, so a validation
	// cannot be read as covering a later or superseded one.
	if err := requireCandidateCommit(attempt, payload.Commit, what); err != nil {
		return err
	}
	if err := p.recordEvidence(p.validations, "validation", payload.ValidationID, evidenceRef{
		scope:     string(scopeAttemptEvidence),
		taskID:    task.ID,
		attemptID: attempt.ID,
		commit:    attempt.CandidateCommit,
	}); err != nil {
		return err
	}
	if payload.Status == protocol.ValidationPass {
		return p.transitionTask(e, payload.TaskID, tasks.StateReviewing, nil)
	}
	// A failed candidate returns the task to the worker. The repair runs as a
	// new attempt: the attempt that produced the rejected candidate has
	// already terminated, and rewriting it would erase history.
	return p.transitionTask(e, payload.TaskID, tasks.StateRunning, nil)
}

func (p *Projection) applyIntegrationValidation(e *events.Event, payload *events.ValidationCompleted) error {
	const what = "integration validation"
	task, err := p.requireTaskInState(payload.TaskID, tasks.StateIntegrationValidating, what)
	if err != nil {
		return err
	}
	if err := p.recordEvidence(p.validations, "validation", payload.ValidationID, evidenceRef{
		scope:  string(events.ScopeIntegration),
		taskID: task.ID,
		commit: payload.Commit,
	}); err != nil {
		return err
	}
	if payload.Status != protocol.ValidationPass {
		// Integration failure does not itself block: an escalation must name
		// the decision owner, so it is a separate recorded act.
		return nil
	}
	if err := p.transitionTask(e, payload.TaskID, tasks.StateDone, nil); err != nil {
		return err
	}
	// Only an integrated, validated change becomes the project baseline.
	// Acceptance alone is not integration (docs/ARCHITECTURE.md §11).
	if payload.Commit != "" {
		p.AcceptedCommit = payload.Commit
	} else if task.AcceptedCommit != "" {
		p.AcceptedCommit = task.AcceptedCommit
	}
	return nil
}

func (p *Projection) applyBaselineValidation(payload *events.ValidationCompleted) error {
	id := payload.ValidationID
	status := protocol.ValidationRed
	switch payload.Status {
	case protocol.ValidationPass:
		status = protocol.ValidationGreen
	case protocol.ValidationCancelled, protocol.ValidationError:
		// A run that did not complete says nothing about the baseline, and
		// reporting it as red would be as misleading as reporting it green.
		status = protocol.ValidationUnknown
	}
	p.Validation = protocol.ValidationState{
		Status: status,
		// The baseline is only evidence about the accepted commit if it ran
		// on it (DCI-012).
		AcceptedCommitVerified: payload.Status == protocol.ValidationPass &&
			payload.Commit != "" && payload.Commit == p.AcceptedCommit,
		LastFullValidationID: &id,
	}
	return nil
}

func (p *Projection) applyReviewCompleted(payload *events.ReviewCompleted) error {
	const what = "review"
	task, err := p.requireTaskInState(payload.TaskID, tasks.StateReviewing, what)
	if err != nil {
		return err
	}
	attempt, err := p.requireAttemptOf(task.ID, payload.AttemptID, what)
	if err != nil {
		return err
	}
	if err := requireCandidate(attempt, what); err != nil {
		return err
	}
	// Reviews are evidence, not a transition: the task leaves REVIEWING only
	// through an explicit acceptance or rejection decision (DCI-044).
	return p.recordEvidence(p.reviews, "review", payload.ReviewID, evidenceRef{
		scope:     string(scopeAttemptEvidence),
		taskID:    task.ID,
		attemptID: attempt.ID,
		commit:    attempt.CandidateCommit,
	})
}

// applyChangeAccepted is the point at which a candidate becomes the project's
// answer, so it is where lineage has to be complete.
//
// docs/OBSERVABILITY.md §9 requires an acceptance to be explainable from the
// Work Package, the candidate, the validation and the reviews. Every one of
// those references is checked here; an acceptance that cites another task's
// attempt, a commit the attempt did not produce, a blueprint the task never
// approved, or evidence that was never recorded is refused rather than
// stored as a decision that merely looks justified.
func (p *Projection) applyChangeAccepted(e *events.Event, payload *events.ChangeAccepted) error {
	const what = "acceptance"
	task, err := p.requireTaskInState(payload.TaskID, tasks.StateReviewing, what)
	if err != nil {
		return err
	}
	attempt, err := p.requireAttemptOf(task.ID, payload.AttemptID, what)
	if err != nil {
		return err
	}
	if err := requireCandidate(attempt, what); err != nil {
		return err
	}
	if err := requireCandidateCommit(attempt, payload.CandidateCommit, what); err != nil {
		return err
	}
	// The accepted change must be the one the approved blueprint asked for,
	// and the attempt must have been executed against that same blueprint
	// version (DCI-032).
	if payload.WorkPackageID != task.WorkPackageID {
		return errs.New(errs.CategoryIntegrity,
			"%s of task %s names work package %s, but %s is the approved one",
			what, task.Alias, payload.WorkPackageID, task.WorkPackageID)
	}
	if attempt.WorkPackageID != task.WorkPackageID || attempt.WorkPackageVersion != task.WorkPackageVersion {
		return errs.New(errs.CategoryIntegrity,
			"%s of task %s accepts attempt %s, which ran against work package %s v%d "+
				"rather than the approved %s v%d",
			what, task.Alias, attempt.ID,
			attempt.WorkPackageID, attempt.WorkPackageVersion,
			task.WorkPackageID, task.WorkPackageVersion)
	}
	if err := p.requireEvidenceFor(p.validations, "validation", payload.ValidationIDs, task, attempt); err != nil {
		return err
	}
	if err := p.requireEvidenceFor(p.reviews, "review", payload.ReviewIDs, task, attempt); err != nil {
		return err
	}

	if err := p.transitionTask(e, payload.TaskID, tasks.StateAccepted, func(t *tasks.Task) error {
		t.AcceptedCommit = payload.CandidateCommit
		return nil
	}); err != nil {
		return err
	}
	commit := payload.CandidateCommit
	p.semanticChanges = append(p.semanticChanges, protocol.SemanticChange{
		TaskID:  task.Alias,
		Commit:  &commit,
		Summary: payload.SemanticSummary,
	})
	return nil
}

func (p *Projection) applyEscalationRaised(e *events.Event, payload *events.EscalationRaised) error {
	task, err := p.Task(payload.TaskID)
	if err != nil {
		return err
	}
	// A block that cites an attempt must cite one of this task's attempts,
	// otherwise the escalation points the decision owner at unrelated work.
	if payload.Reason.AttemptID != "" {
		if _, err := p.requireAttemptOf(task.ID, payload.Reason.AttemptID, "escalation"); err != nil {
			return err
		}
	}
	reason := payload.Reason
	reason.BlockedFrom = task.State
	return p.transitionTask(e, payload.TaskID, tasks.StateBlocked, func(t *tasks.Task) error {
		t.Blocked = &reason
		return nil
	})
}

// removeString returns a new slice without value. It allocates rather than
// compacting in place so that slices handed out earlier are never mutated
// behind the caller's back.
func removeString(list []string, value string) []string {
	out := make([]string, 0, len(list))
	for _, item := range list {
		if item != value {
			out = append(out, item)
		}
	}
	return out
}

// applyChangeRejected sends a reviewed candidate back for repair.
//
// It carries the same lineage requirements as acceptance minus the evidence
// citations: a rejection must be about a real candidate of the task being
// rejected, or it would return some other task's work to the worker.
func (p *Projection) applyChangeRejected(e *events.Event, payload *events.ChangeRejected) error {
	const what = "rejection"
	task, err := p.requireTaskInState(payload.TaskID, tasks.StateReviewing, what)
	if err != nil {
		return err
	}
	attempt, err := p.requireAttemptOf(task.ID, payload.AttemptID, what)
	if err != nil {
		return err
	}
	if err := requireCandidate(attempt, what); err != nil {
		return err
	}
	return p.transitionTask(e, payload.TaskID, tasks.StateRunning, nil)
}
