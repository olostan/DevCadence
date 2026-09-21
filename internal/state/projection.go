// Package state reduces the engineering event journal into the canonical
// ProjectState of docs/PROJECT_STATE.md.
//
// The reducer is a pure function of the event prefix it is given: it reads no
// clock, generates no identifiers and performs no I/O. That is what makes
// DCI-053 (state is reconstructable) testable rather than aspirational — a
// projection rebuilt from the journal is byte-identical to the original.
//
// Illegal histories are rejected loudly. A reducer that repaired inconsistent
// input would hide exactly the integrity failures docs/PROJECT_STATE.md §15
// asks the system to detect.
package state

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/events"
	"github.com/olostan/DevCadience/internal/protocol"
	"github.com/olostan/DevCadience/internal/tasks"
)

// Projection is the materialised reduction of an event prefix.
//
// It holds more than ProjectState renders: attempts and per-task detail are
// needed to answer "task show" and to enforce transitions, while ProjectState
// stays compact for the principal (DCI-010).
type Projection struct {
	ProjectID   string
	Name        string
	initialised bool

	// HighWatermark is the sequence of the highest applied event and is the
	// projection's identity: see StateRevision.
	HighWatermark int64
	// LastOccurredAt is the timestamp of the highest applied event. It is
	// used as ProjectState.generated_at so that rendering needs no clock.
	LastOccurredAt protocol.Timestamp

	AcceptedCommit string
	Branch         string
	RepositoryPath string
	VisionRef      string
	CurrentOutcome string

	Milestone protocol.MilestoneState

	ActiveInvariants []string
	ActiveDecisions  []string

	Validation protocol.ValidationState
	Health     protocol.HealthState

	// Ordered id slices accompany the maps so that rendering is deterministic
	// in insertion order where insertion order is meaningful, and sorted
	// where it is not.
	components   map[string]protocol.ComponentState
	componentIDs []string

	risks   map[string]protocol.Risk
	riskIDs []string

	decisionsRequired   map[string]protocol.DecisionRequired
	decisionRequiredIDs []string

	tasks   map[string]*tasks.Task
	taskIDs []string

	attempts       map[string]*tasks.Attempt
	attemptsByTask map[string][]string

	// semanticChanges is append-only and rendered newest-first, bounded by
	// RecentSemanticChangeLimit so that ProjectState stays compact (§12).
	semanticChanges []protocol.SemanticChange

	// aliases guards against two tasks claiming the same human-readable
	// handle, which would make CLI and principal references ambiguous.
	aliases map[string]string

	// discovery is the Day-0 projection. It stays nil in ProjectState until
	// a discovery fact has been recorded.
	discovery *discoveryState
}

// RecentSemanticChangeLimit bounds how many semantic deltas ProjectState
// carries. Older changes remain in the journal; docs/PROJECT_STATE.md §12
// requires current state to stay bounded while history grows.
const RecentSemanticChangeLimit = 10

// New returns an empty projection.
func New() *Projection {
	return &Projection{
		components:        map[string]protocol.ComponentState{},
		risks:             map[string]protocol.Risk{},
		decisionsRequired: map[string]protocol.DecisionRequired{},
		tasks:             map[string]*tasks.Task{},
		attempts:          map[string]*tasks.Attempt{},
		attemptsByTask:    map[string][]string{},
		aliases:           map[string]string{},
		discovery:         newDiscoveryState(),
		Validation:        protocol.ValidationState{Status: protocol.ValidationUnknown},
		Health:            protocol.HealthState{Status: protocol.HealthUnknown},
	}
}

// Reduce applies every event in order and returns the resulting projection.
func Reduce(stream []events.Event) (*Projection, error) {
	p := New()
	for i := range stream {
		if err := p.Apply(&stream[i]); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// Initialised reports whether a ProjectInitialized event has been applied.
func (p *Projection) Initialised() bool { return p.initialised }

// Task returns the projected task, or a not-found error.
func (p *Projection) Task(id string) (*tasks.Task, error) {
	t, ok := p.tasks[id]
	if !ok {
		return nil, errs.New(errs.CategoryNotFound, "task %s does not exist", id)
	}
	return t, nil
}

// TaskByAlias resolves a human-readable task handle.
func (p *Projection) TaskByAlias(alias string) (*tasks.Task, error) {
	id, ok := p.aliases[alias]
	if !ok {
		return nil, errs.New(errs.CategoryNotFound, "no task with alias %s", alias)
	}
	return p.Task(id)
}

// Tasks returns every task in creation order.
func (p *Projection) Tasks() []*tasks.Task {
	out := make([]*tasks.Task, 0, len(p.taskIDs))
	for _, id := range p.taskIDs {
		out = append(out, p.tasks[id])
	}
	return out
}

// Attempt returns the projected attempt, or a not-found error.
func (p *Projection) Attempt(id string) (*tasks.Attempt, error) {
	a, ok := p.attempts[id]
	if !ok {
		return nil, errs.New(errs.CategoryNotFound, "attempt %s does not exist", id)
	}
	return a, nil
}

// AttemptsForTask returns the task's attempts in start order. Previous
// attempts are always present: a retry adds, it never replaces.
func (p *Projection) AttemptsForTask(taskID string) []*tasks.Attempt {
	ids := p.attemptsByTask[taskID]
	out := make([]*tasks.Attempt, 0, len(ids))
	for _, id := range ids {
		out = append(out, p.attempts[id])
	}
	return out
}

// StateRevision derives the revision identity from the high-watermark.
//
// Making the revision a function of the journal position, rather than a
// separately allocated counter, means the same history always names the same
// revision, and the consistency check "state revision matches journal
// high-water mark" (docs/PROJECT_STATE.md §15) holds structurally. See
// docs/adr/0005-deterministic-project-state-identity.md.
func StateRevision(highWatermark int64) string {
	return fmt.Sprintf("ps_%09d", highWatermark)
}

// ProjectState renders the compact principal-facing snapshot.
//
// It takes no clock and no identifier source: everything it needs is already
// in the projection, so the same event prefix always renders the same bytes.
func (p *Projection) ProjectState() (*protocol.ProjectState, error) {
	if !p.initialised {
		return nil, errs.New(errs.CategoryNotFound, "project state requested before ProjectInitialized")
	}
	watermark := strconv.FormatInt(p.HighWatermark, 10)

	buckets := protocol.TaskBuckets{
		Ready:             []string{},
		Running:           []string{},
		Blocked:           []string{},
		AwaitingPrincipal: []string{},
	}
	completed := 0
	total := 0
	for _, id := range p.taskIDs {
		t := p.tasks[id]
		if t.MilestoneID == p.Milestone.ID {
			total++
			if t.State == tasks.StateDone {
				completed++
			}
		}
		// Tasks are reported by alias: the principal reasons about "DC-012",
		// not about an opaque ULID.
		for _, bucket := range tasks.Buckets(t.State, t.BlockedAuthority()) {
			switch bucket {
			case tasks.BucketReady:
				buckets.Ready = append(buckets.Ready, t.Alias)
			case tasks.BucketRunning:
				buckets.Running = append(buckets.Running, t.Alias)
			case tasks.BucketBlocked:
				buckets.Blocked = append(buckets.Blocked, t.Alias)
			case tasks.BucketAwaitingPrincipal:
				buckets.AwaitingPrincipal = append(buckets.AwaitingPrincipal, t.Alias)
			}
		}
	}

	milestone := p.Milestone
	milestone.CompletedTasks = completed
	milestone.TotalTasks = total

	state := &protocol.ProjectState{
		SchemaVersion:      protocol.SchemaVersion1,
		ProjectID:          p.ProjectID,
		StateRevision:      StateRevision(p.HighWatermark),
		GeneratedAt:        p.LastOccurredAt,
		EventHighWatermark: &watermark,
		Git:                protocol.GitState{},
		Milestone:          milestone,
		Tasks:              buckets,
		ActiveInvariants:   append([]string(nil), p.ActiveInvariants...),
		ActiveDecisions:    append([]string(nil), p.ActiveDecisions...),
		Validation:         p.Validation,
		Risks:              []protocol.Risk{},
		DecisionsRequired:  []protocol.DecisionRequired{},
	}
	if p.AcceptedCommit != "" {
		commit := p.AcceptedCommit
		state.Git.AcceptedCommit = &commit
	}
	if p.Branch != "" {
		branch := p.Branch
		state.Git.Branch = &branch
	}
	if p.VisionRef != "" || p.CurrentOutcome != "" {
		product := &protocol.ProductState{}
		if p.VisionRef != "" {
			ref := p.VisionRef
			product.VisionRef = &ref
		}
		if p.CurrentOutcome != "" {
			outcome := p.CurrentOutcome
			product.CurrentOutcome = &outcome
		}
		state.Product = product
	}
	for _, id := range p.componentIDs {
		state.Components = append(state.Components, p.components[id])
	}
	for _, id := range p.riskIDs {
		state.Risks = append(state.Risks, p.risks[id])
	}
	for _, id := range p.decisionRequiredIDs {
		state.DecisionsRequired = append(state.DecisionsRequired, p.decisionsRequired[id])
	}
	state.Discovery = p.discovery.render()
	if p.Health.Status != "" && p.Health.Status != protocol.HealthUnknown {
		health := p.Health
		health.KnownDebt = append([]string(nil), p.Health.KnownDebt...)
		state.Health = &health
	}
	// Newest first: the principal reads the most recent delta at the top.
	for i := len(p.semanticChanges) - 1; i >= 0 && len(state.RecentSemanticChanges) < RecentSemanticChangeLimit; i-- {
		state.RecentSemanticChanges = append(state.RecentSemanticChanges, p.semanticChanges[i])
	}
	if err := state.Validate(); err != nil {
		return nil, err
	}
	return state, nil
}

// sortedCopy returns a sorted copy of ids, used where rendering order carries
// no meaning and stability is all that matters.
func sortedCopy(ids []string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}
