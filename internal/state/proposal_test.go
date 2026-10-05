package state_test

import (
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/state"
	"github.com/olostan/DevCadence/internal/tasks"
	"github.com/olostan/DevCadence/internal/testsupport"
)

const (
	proposalTask = "tsk_00000000000000000000000001"
	proposalWP   = "wp_000000000000000000000001"
)

func proposalPayload(version int) *events.WorkPackageProposed {
	return &events.WorkPackageProposed{
		TaskID: proposalTask, WorkPackageID: proposalWP, Version: version,
		ProjectStateRevision: "ps_000000004", BaseCommit: "91acd8273f1", RecordDigest: "sha256:" + strings.Repeat("a", 64),
	}
}

// designing builds a stream whose single task is designing.
func designing(t *testing.T) *testsupport.ScenarioBuilder {
	t.Helper()
	return testsupport.NewScenario(t, "example").
		Add(&events.ProjectInitialized{Name: "Example", MilestoneID: "M1", MilestoneTitle: "Core"}).
		Add(&events.TaskCreated{TaskID: proposalTask, Alias: "DC-001", Title: "t", MilestoneID: "M1", ChangeClass: protocol.ChangeSystemic}).
		Add(&events.TaskDesignStarted{TaskID: proposalTask, Reason: "initial design"})
}

func TestWorkPackageProposalHasNoLifecycleEffect(t *testing.T) {
	stream := designing(t).Add(proposalPayload(1)).Add(proposalPayload(2)).Stream()
	projection, err := state.Reduce(stream)
	if err != nil {
		t.Fatal(err)
	}
	task, _ := projection.TaskByAlias("DC-001")
	if task.State != tasks.StateDesigning || task.WorkPackageID != "" || task.WorkPackageVersion != 0 {
		t.Fatalf("a proposal changed the task: %+v", task)
	}
	rendered, err := projection.ProjectState()
	if err != nil {
		t.Fatal(err)
	}
	if len(rendered.Tasks.Ready) != 0 {
		t.Fatalf("a proposal made work ready: %v", rendered.Tasks.Ready)
	}
	// Rebuilding is deterministic: the private proposal index is a pure function of the journal.
	again, err := state.Reduce(stream)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := projection.ProjectState()
	b, _ := again.ProjectState()
	if a.StateRevision != b.StateRevision {
		t.Fatal("rebuild differs")
	}
}

func TestWorkPackageProposalRules(t *testing.T) {
	for name, tc := range map[string]struct {
		stream func() []events.Event
		want   errs.Category
	}{
		"duplicate version": {func() []events.Event { return designing(t).Add(proposalPayload(1)).Add(proposalPayload(1)).Stream() }, errs.CategoryConflict},
		"lower version":     {func() []events.Event { return designing(t).Add(proposalPayload(2)).Add(proposalPayload(1)).Stream() }, errs.CategoryConflict},
		"not designing": {func() []events.Event {
			return testsupport.NewScenario(t, "example").
				Add(&events.ProjectInitialized{Name: "Example", MilestoneID: "M1", MilestoneTitle: "Core"}).
				Add(&events.TaskCreated{TaskID: proposalTask, Alias: "DC-001", Title: "t", MilestoneID: "M1", ChangeClass: protocol.ChangeSystemic}).
				Add(proposalPayload(1)).Stream()
		}, errs.CategoryInvalidTransition},
		"unknown task": {func() []events.Event {
			p := proposalPayload(1)
			p.TaskID = "tsk_missing"
			return designing(t).Add(p).Stream()
		}, errs.CategoryNotFound},
		"not above the approved version": {func() []events.Event {
			return designing(t).Add(&events.WorkPackageApproved{
				TaskID: proposalTask, WorkPackageID: proposalWP, WorkPackageVersion: 2, RecordDigest: "sha256:" + strings.Repeat("b", 64),
				ProjectStateRevision: "ps_000000004", BaseCommit: "91acd8273f1", ChangeClass: protocol.ChangeSystemic,
			}).Add(&events.TaskDesignStarted{TaskID: proposalTask, Reason: "revise"}).Stream()
		}, errs.CategoryInvalidTransition},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := state.Reduce(tc.stream())
			if err == nil {
				t.Fatal("an illegal proposal history reduced")
			}
			if tc.want != errs.CategoryInvalidTransition && errs.CategoryOf(err) != tc.want {
				t.Fatalf("category %s, want %s (%v)", errs.CategoryOf(err), tc.want, err)
			}
		})
	}
}

// A proposal must not be accepted at or below an approved version of the same plan.
func TestProposalMustSupersedeTheApprovedVersion(t *testing.T) {
	approved := &events.WorkPackageApproved{
		TaskID: proposalTask, WorkPackageID: proposalWP, WorkPackageVersion: 2, RecordDigest: "sha256:" + strings.Repeat("b", 64),
		ProjectStateRevision: "ps_000000004", BaseCommit: "91acd8273f1", ChangeClass: protocol.ChangeSystemic,
	}
	// Blocked work returns to designing through an explicit design act.
	base := designing(t).Add(approved).
		Add(&events.TaskDelegated{TaskID: proposalTask, WorkPackageID: proposalWP, WorkerRole: "implementer"}).
		Add(&events.EscalationRaised{TaskID: proposalTask, EscalationID: "esc_1", Reason: tasks.BlockedReason{
			Trigger: "contradicted_assumption", Statement: "s", Authority: protocol.AuthorityPrincipal, EvidenceRefs: []string{"e"}, BlockedFrom: tasks.StateRunning,
		}}).
		Add(&events.TaskDesignStarted{TaskID: proposalTask, Reason: "revise"})
	if _, err := state.Reduce(base.Add(proposalPayload(2)).Stream()); err == nil {
		t.Fatal("a proposal at the approved version reduced")
	}
	base = designing(t).Add(approved).
		Add(&events.TaskDelegated{TaskID: proposalTask, WorkPackageID: proposalWP, WorkerRole: "implementer"}).
		Add(&events.EscalationRaised{TaskID: proposalTask, EscalationID: "esc_1", Reason: tasks.BlockedReason{
			Trigger: "contradicted_assumption", Statement: "s", Authority: protocol.AuthorityPrincipal, EvidenceRefs: []string{"e"}, BlockedFrom: tasks.StateRunning,
		}}).
		Add(&events.TaskDesignStarted{TaskID: proposalTask, Reason: "revise"})
	if _, err := state.Reduce(base.Add(proposalPayload(3)).Stream()); err != nil {
		t.Fatalf("a superseding proposal was refused: %v", err)
	}
}
