package testsupport

import (
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/tasks"
)

// ScenarioBuilder assembles an event stream with sequences and timestamps
// assigned deterministically.
//
// It writes events directly rather than going through the store, so reducer
// tests can construct histories that the control plane would refuse to
// create — inconsistent ones included, which is how "inconsistent histories
// fail clearly" is tested at all.
type ScenarioBuilder struct {
	t         *testing.T
	projectID string
	seq       int64
	at        time.Time
	stream    []events.Event
}

// NewScenario starts a builder for the project.
func NewScenario(t *testing.T, projectID string) *ScenarioBuilder {
	t.Helper()
	return &ScenarioBuilder{t: t, projectID: projectID, at: Epoch}
}

// Add appends an event with the next sequence and timestamp.
func (b *ScenarioBuilder) Add(payload events.Payload) *ScenarioBuilder {
	b.t.Helper()
	return b.AddAs(protocol.Actor{Kind: protocol.ActorControlPlane, ID: "devcadence"}, payload)
}

// AddAs appends an event attributed to a specific actor.
func (b *ScenarioBuilder) AddAs(actor protocol.Actor, payload events.Payload) *ScenarioBuilder {
	b.t.Helper()
	b.seq++
	b.at = b.at.Add(Step)
	event := events.Event{
		Seq:           b.seq,
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       eventID(b.seq),
		ProjectID:     b.projectID,
		EventType:     payload.Type(),
		OccurredAt:    protocol.NewTimestamp(b.at),
		Actor:         actor,
		Correlation:   events.CorrelationFor(payload),
		Payload:       payload,
	}
	b.stream = append(b.stream, event)
	return b
}

// Stream returns the assembled events.
func (b *ScenarioBuilder) Stream() []events.Event {
	return append([]events.Event(nil), b.stream...)
}

// eventID renders a deterministic, correctly shaped event identifier.
func eventID(seq int64) string {
	const width = 26
	digits := []byte("0000000000000000000000000")
	body := make([]byte, width)
	copy(body, digits)
	for i := width - 1; i >= 0 && seq > 0; i-- {
		body[i] = byte('0' + seq%10)
		seq /= 10
	}
	for i := 0; i < width; i++ {
		if body[i] == 0 {
			body[i] = '0'
		}
	}
	return "evt_" + string(body)
}

// HappyPathScenario builds a project whose single task travels the whole
// lifecycle from PROPOSED to DONE.
//
// It is the shared baseline for reducer, storage and rebuild tests, so that
// those tests exercise one realistic history rather than three different
// partial ones.
func HappyPathScenario(t *testing.T, projectID string) *ScenarioBuilder {
	t.Helper()
	const (
		taskID     = "tsk_00000000000000000000000001"
		attemptID  = "att_00000000000000000000000001"
		wpID       = "wp_000000000000000000000001"
		candidate  = "cafebabe1234567"
		integrated = "deadbeef7654321"
	)
	return NewScenario(t, projectID).
		AddAs(protocol.Actor{Kind: protocol.ActorHuman, ID: "operator"}, &events.ProjectInitialized{
			Name:             "Example project",
			VisionRef:        "docs/VISION.md",
			MilestoneID:      "M1",
			MilestoneTitle:   "Domain core and canonical state",
			ActiveInvariants: []string{"DCI-053", "DCI-020"},
		}).
		AddAs(protocol.Actor{Kind: protocol.ActorPrincipal, ID: "principal"}, &events.TaskCreated{
			TaskID: taskID, Alias: "DC-001", Title: "Bounded journal reads",
			MilestoneID: "M1", ChangeClass: protocol.ChangeSystemic,
		}).
		Add(&events.TaskScoutingStarted{TaskID: taskID, InvestigationID: "inv_0001"}).
		AddAs(protocol.Actor{Kind: protocol.ActorPrincipal, ID: "principal"}, &events.TaskDesignStarted{
			TaskID: taskID, Reason: "initial design",
		}).
		AddAs(protocol.Actor{Kind: protocol.ActorPrincipal, ID: "principal"}, &events.WorkPackageApproved{
			TaskID: taskID, WorkPackageID: wpID, WorkPackageVersion: 1,
			RecordDigest: "sha256:" + zeros(64), ProjectStateRevision: "ps_000000004",
			BaseCommit: "91acd8273f1", ChangeClass: protocol.ChangeSystemic,
		}).
		Add(&events.TaskDelegated{
			TaskID: taskID, WorkPackageID: wpID, WorkerRole: "implementer",
			WorkerProfile: "local-strong-coder", MaxAttempts: 3,
		}).
		Add(&events.AttemptStarted{
			TaskID: taskID, AttemptID: attemptID, WorkPackageID: wpID, WorkPackageVersion: 1,
			ProjectStateRevision: "ps_000000004", BaseCommit: "91acd8273f1",
			WorkerRole: "implementer", WorkerProfile: "local-strong-coder",
		}).
		AddAs(protocol.Actor{Kind: protocol.ActorLocalAgent, ID: "implementer"}, &events.CandidateProduced{
			TaskID: taskID, AttemptID: attemptID, CandidateCommit: candidate,
			Summary: "Added an inclusive upper bound to EventQuery.", RepairIterations: 2,
		}).
		AddAs(protocol.Actor{Kind: protocol.ActorTool, ID: "validator"}, &events.ValidationCompleted{
			TaskID: taskID, AttemptID: attemptID, ValidationID: "val_0001",
			Scope: events.ScopeAttempt, Status: protocol.ValidationPass,
			Commit: candidate, RecordDigest: "sha256:" + zeros(64),
		}).
		AddAs(protocol.Actor{Kind: protocol.ActorLocalAgent, ID: "reviewer"}, &events.ReviewCompleted{
			TaskID: taskID, AttemptID: attemptID, ReviewID: "rev_0001", WorkPackageID: wpID,
			Dimension: protocol.DimensionCorrectness, Verdict: protocol.VerdictPass,
			RecordDigest: "sha256:" + zeros(64),
		}).
		AddAs(protocol.Actor{Kind: protocol.ActorPrincipal, ID: "principal"}, &events.ChangeAccepted{
			TaskID: taskID, AttemptID: attemptID, WorkPackageID: wpID, CandidateCommit: candidate,
			SemanticSummary: "Journal reads accept an inclusive upper bound.",
			ValidationIDs:   []string{"val_0001"}, ReviewIDs: []string{"rev_0001"},
			DecidedBy: protocol.AuthorityPrincipal,
		}).
		Add(&events.IntegrationStarted{TaskID: taskID, IntegrationID: "int_0001", BaseCommit: "91acd8273f1"}).
		Add(&events.IntegrationValidationStarted{
			TaskID: taskID, IntegrationID: "int_0001", IntegratedCommit: integrated,
		}).
		AddAs(protocol.Actor{Kind: protocol.ActorTool, ID: "validator"}, &events.ValidationCompleted{
			TaskID: taskID, ValidationID: "val_0002", Scope: events.ScopeIntegration,
			Status: protocol.ValidationPass, Commit: integrated, RecordDigest: "sha256:" + zeros(64),
		})
}

// BlockedScenario builds a project whose task is blocked on a contradicted
// assumption and then resumed through a revised design.
func BlockedScenario(t *testing.T, projectID string) *ScenarioBuilder {
	t.Helper()
	const (
		taskID    = "tsk_00000000000000000000000001"
		attemptID = "att_00000000000000000000000001"
		wpID      = "wp_000000000000000000000001"
	)
	reason := tasks.BlockedReason{
		Trigger:      "contradicted_assumption",
		Statement:    "Assumption A2 is false: three callers construct EventQuery positionally.",
		Authority:    protocol.AuthorityPrincipal,
		EvidenceRefs: []string{"ev_callers"},
	}
	return NewScenario(t, projectID).
		AddAs(protocol.Actor{Kind: protocol.ActorHuman, ID: "operator"}, &events.ProjectInitialized{
			Name: "Example project", MilestoneID: "M1", MilestoneTitle: "Domain core and canonical state",
		}).
		AddAs(protocol.Actor{Kind: protocol.ActorPrincipal, ID: "principal"}, &events.TaskCreated{
			TaskID: taskID, Alias: "DC-001", Title: "Bounded journal reads",
			MilestoneID: "M1", ChangeClass: protocol.ChangeSystemic,
		}).
		AddAs(protocol.Actor{Kind: protocol.ActorPrincipal, ID: "principal"}, &events.TaskDesignStarted{
			TaskID: taskID, Reason: "initial design",
		}).
		AddAs(protocol.Actor{Kind: protocol.ActorPrincipal, ID: "principal"}, &events.WorkPackageApproved{
			TaskID: taskID, WorkPackageID: wpID, WorkPackageVersion: 1,
			RecordDigest: "sha256:" + zeros(64), ProjectStateRevision: "ps_000000003",
			BaseCommit: "91acd8273f1", ChangeClass: protocol.ChangeSystemic,
		}).
		Add(&events.TaskDelegated{TaskID: taskID, WorkPackageID: wpID, WorkerRole: "implementer"}).
		Add(&events.AttemptStarted{
			TaskID: taskID, AttemptID: attemptID, WorkPackageID: wpID, WorkPackageVersion: 1,
			ProjectStateRevision: "ps_000000003", WorkerRole: "implementer",
		}).
		AddAs(protocol.Actor{Kind: protocol.ActorLocalAgent, ID: "implementer"}, &events.AttemptBlocked{
			TaskID: taskID, AttemptID: attemptID, Reason: reason,
		}).
		Add(&events.EscalationRaised{
			TaskID: taskID, EscalationID: "esc_0001", Reason: reason,
			Options: []string{"Revise assumption A2", "Split the change"},
			Tried:   []string{"Searched for positional constructions"},
		})
}

func zeros(n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = '0'
	}
	return string(out)
}
