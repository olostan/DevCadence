// Package tasks implements the delivery task state machine and the immutable
// attempt model.
//
// The canonical lifecycle is the one drawn in docs/LIFECYCLE.md §12. The
// shorter list in ENGINEERING_STANDARDS.md §12 is explicitly illustrative;
// where the two differ, docs/adr/0004-canonical-task-state-machine.md records
// the resolution.
//
// This package is pure: it knows nothing about SQLite, events or the CLI, so
// that transition legality can be reasoned about and tested in isolation.
package tasks

import (
	"sort"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// State is a delivery task state.
type State string

const (
	// StateProposed is a task that exists but has not been triaged.
	StateProposed State = "proposed"
	// StateScouting is local repository reconnaissance for the task.
	StateScouting State = "scouting"
	// StateDesigning is principal design work producing a Work Package.
	StateDesigning State = "designing"
	// StateReady has an approved Work Package and awaits delegation.
	StateReady State = "ready"
	// StateRunning has a delegated worker implementing the Work Package.
	StateRunning State = "running"
	// StateValidating is running deterministic checks over a candidate.
	StateValidating State = "validating"
	// StateReviewing is gathering independent review evidence.
	StateReviewing State = "reviewing"
	// StateAccepted has an acceptance decision but is not yet integrated.
	StateAccepted State = "accepted"
	// StateIntegrating is combining the accepted change with the baseline.
	StateIntegrating State = "integrating"
	// StateIntegrationValidating is validating the integrated result.
	StateIntegrationValidating State = "integration_validating"
	// StateDone is terminal and successful.
	StateDone State = "done"
	// StateBlocked holds a task that cannot progress without a decision. The
	// reason is always preserved; see BlockedReason.
	StateBlocked State = "blocked"
)

// AllStates returns every state in lifecycle order. The order is fixed so
// that CLI output and test tables are stable.
func AllStates() []State {
	return []State{
		StateProposed, StateScouting, StateDesigning, StateReady, StateRunning,
		StateValidating, StateReviewing, StateAccepted, StateIntegrating,
		StateIntegrationValidating, StateDone, StateBlocked,
	}
}

// Valid reports whether s is a known state.
func (s State) Valid() bool {
	for _, known := range AllStates() {
		if s == known {
			return true
		}
	}
	return false
}

// Terminal reports whether no further transition is possible.
//
// Only StateDone is terminal. StateBlocked is deliberately not terminal: a
// blocked task is waiting for a decision, and DCI-045 requires escalation
// rather than silent abandonment.
func (s State) Terminal() bool { return s == StateDone }

// Active reports whether the task is in a state that work can be performed
// in. Active states are the ones that may be blocked.
func (s State) Active() bool { return s.Valid() && s != StateDone && s != StateBlocked }

// transitions is the canonical adjacency list of the lifecycle. It is the
// single source of truth for legality; nothing else in the codebase may
// hard-code an edge.
//
// StateBlocked is reachable from every active state and is therefore not
// listed here; see Allowed.
var transitions = map[State][]State{
	StateProposed:  {StateScouting, StateDesigning},
	StateScouting:  {StateDesigning},
	StateDesigning: {StateReady},
	StateReady:     {StateRunning},
	StateRunning:   {StateValidating},
	// Validating proceeds to Reviewing when deterministic checks pass, or
	// returns to Running when they fail. The repair runs as a *new* attempt:
	// producing a candidate terminates the attempt that produced it, and a
	// terminated attempt is historical fact. Attempt.RepairIterations counts
	// the worker's internal fix-and-recheck cycles *before* it produced that
	// candidate, which is a different thing from a retry across attempts.
	StateValidating: {StateRunning, StateReviewing},
	// Reviewing returns to Running when a local fix is requested, which
	// starts a new attempt rather than erasing the previous one.
	StateReviewing:             {StateRunning, StateAccepted},
	StateAccepted:              {StateIntegrating},
	StateIntegrating:           {StateIntegrationValidating},
	StateIntegrationValidating: {StateDone},
	// A blocked task always resumes through Designing so that the revision
	// that unblocks it is an explicit, recorded design act (DCI-024).
	StateBlocked: {StateDesigning},
	StateDone:    nil,
}

// Allowed returns the states reachable from s, in a stable order.
func Allowed(s State) []State {
	next := append([]State(nil), transitions[s]...)
	if s.Active() {
		next = append(next, StateBlocked)
	}
	sort.Slice(next, func(i, j int) bool { return next[i] < next[j] })
	return next
}

// CanTransition reports whether from -> to is a legal edge.
func CanTransition(from, to State) bool {
	if !from.Valid() || !to.Valid() {
		return false
	}
	if to == StateBlocked {
		return from.Active()
	}
	for _, candidate := range transitions[from] {
		if candidate == to {
			return true
		}
	}
	return false
}

// CheckTransition returns a categorised error when from -> to is illegal.
//
// Callers must call this before mutating any durable state:
// ENGINEERING_STANDARDS.md §12 requires that an illegal transition leave
// persistence untouched.
func CheckTransition(taskID string, from, to State) error {
	if !from.Valid() {
		return errs.New(errs.CategoryIntegrity, "task %s: stored state %q is not a known task state", taskID, string(from))
	}
	if !to.Valid() {
		return errs.New(errs.CategoryInvalidArgument, "task %s: %q is not a known task state", taskID, string(to))
	}
	if CanTransition(from, to) {
		return nil
	}
	return errs.New(errs.CategoryInvalidTransition,
		"task %s: transition %s -> %s is not permitted (allowed: %v)", taskID, from, to, Allowed(from))
}

// Bucket is the ProjectState task grouping a state belongs to.
type Bucket string

const (
	// BucketNone means the state is not surfaced in ProjectState.tasks.
	BucketNone Bucket = ""
	// BucketReady holds tasks with an approved Work Package awaiting a worker.
	BucketReady Bucket = "ready"
	// BucketRunning holds tasks with local work in flight.
	BucketRunning Bucket = "running"
	// BucketBlocked holds tasks waiting for a decision.
	BucketBlocked Bucket = "blocked"
	// BucketAwaitingPrincipal holds tasks whose next act belongs to the
	// principal.
	BucketAwaitingPrincipal Bucket = "awaiting_principal"
)

// Buckets returns the ProjectState buckets a state contributes to.
//
// A task blocked on a principal decision appears in both "blocked" and
// "awaiting_principal": the first answers "what is stuck", the second answers
// "what is mine to unstick", and collapsing them would lose one of those
// answers. The mapping lives here so that every consumer of ProjectState
// agrees; see docs/adr/0004-canonical-task-state-machine.md.
//
// StateProposed, StateAccepted and StateDone map to no bucket: an untriaged
// task is not yet work, and accepted/done tasks are reported through
// milestone progress and recent semantic changes instead.
func Buckets(s State, blockedAuthority protocol.DecisionAuthority) []Bucket {
	switch s {
	case StateReady:
		return []Bucket{BucketReady}
	case StateScouting, StateRunning, StateValidating, StateReviewing,
		StateIntegrating, StateIntegrationValidating:
		return []Bucket{BucketRunning}
	case StateDesigning:
		return []Bucket{BucketAwaitingPrincipal}
	case StateBlocked:
		if blockedAuthority == protocol.AuthorityPrincipal {
			return []Bucket{BucketBlocked, BucketAwaitingPrincipal}
		}
		return []Bucket{BucketBlocked}
	default:
		return nil
	}
}
