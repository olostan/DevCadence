package facade_test

import (
	"context"
	"testing"

	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/principal/facade"
)

// inspectingTasks is a task runtime spy that can also describe a candidate.
type inspectingTasks struct {
	*spies
	handoff *facade.CandidateHandoff
	err     error
	calls   int
}

func (i *inspectingTasks) InspectCandidate(_ context.Context, c facade.CandidateRef) (*facade.CandidateHandoff, error) {
	i.calls++
	if i.err != nil {
		return nil, i.err
	}
	h := *i.handoff
	h.TaskID, h.AttemptID, h.CandidateCommit = c.TaskID, c.AttemptID, c.Commit
	return &h, nil
}

func sampleHandoff() *facade.CandidateHandoff {
	return &facade.CandidateHandoff{
		BaseCommit: "91acd8273f1", Ref: "refs/devcadence/candidates/t-a",
		ChangedFiles: []facade.ChangedFile{{Status: "M", Path: "x.go"}},
		Model:        facade.HandoffModel{EndpointID: "e", Model: "m", Digest: "sha256:abc"},
		Review:       facade.HandoffReview{Status: "review_unavailable", Independent: false},
		Validations:  []facade.HandoffValidation{},
		Inspect:      facade.HandoffInspect{Diff: "d", Log: "l", Merge: "m", CherryPick: "c"},
		Acceptance:   "Acceptance is a manual owner action",
	}
}

// SH1-4C: an installed task runtime that can inspect candidates adds the
// handoff packet to accept (still refused) and to task_status.
func TestHandoff_AcceptAndTaskStatusCarryPacket(t *testing.T) {
	var insp *inspectingTasks
	r := newRig(t, func(o *facade.Options, s *spies) {
		withPorts(o, s)
		insp = &inspectingTasks{spies: s, handoff: sampleHandoff()}
		o.Tasks = insp
	})
	r.initProject("", "")
	cand := r.reviewingTask("91acd8273f1", []string{"storage"})
	r.completeReview(cand)
	rev, before := r.revision(), r.eventCount()

	resp, _ := r.svc.Accept(context.Background(), r.caller, facade.AcceptRequest{
		Meta: r.meta(rev), Candidate: cand, ValidationIDs: []string{"val_0001"}, ReviewIDs: []string{"review_unavailable"}, Reason: "x",
	})
	requireCode(t, resp.Envelope, principal.CodeNeedsPrincipal)
	if len(resp.EvidenceRefs) != 1 || resp.EvidenceRefs[0] != facade.AcceptanceUnavailableRef {
		t.Fatalf("refs = %v", resp.EvidenceRefs)
	}
	if resp.Result == nil || resp.Result.Handoff == nil || resp.Result.Handoff.CandidateCommit != cand.Commit {
		t.Fatalf("accept carries no handoff: %+v", resp)
	}
	requireSchema(t, "principal-accept-response", resp)
	if r.eventCount() != before || r.revision() != rev {
		t.Fatal("accept wrote to the journal")
	}

	// A candidate that is not a real attempt is refused, not described.
	bad := cand
	bad.Commit = "deadbeefdeadbeef"
	resp, _ = r.svc.Accept(context.Background(), r.caller, facade.AcceptRequest{
		Meta: r.meta(rev), Candidate: bad, ValidationIDs: []string{"v"}, ReviewIDs: []string{"r"}, Reason: "x",
	})
	requireCode(t, resp.Envelope, principal.CodeInvalidArgument)
	if resp.Result != nil {
		t.Fatalf("handoff for a candidate with no lineage: %+v", resp.Result)
	}

	// An inspector failure refuses the accept call with its semantic code.
	insp.err = principal.NewCodedError(principal.CodeIntegrity, false, nil, "ref missing")
	resp, _ = r.svc.Accept(context.Background(), r.caller, facade.AcceptRequest{
		Meta: r.meta(rev), Candidate: cand, ValidationIDs: []string{"v"}, ReviewIDs: []string{"r"}, Reason: "x",
	})
	requireCode(t, resp.Envelope, principal.CodeIntegrity)
	insp.err = nil

	ts, _ := r.svc.TaskStatus(context.Background(), r.caller, facade.TaskStatusRequest{Meta: r.meta(""), TaskID: cand.TaskID})
	if ts.Result == nil || ts.Result.CandidateHandoff == nil || ts.Result.Task.Candidate == nil {
		t.Fatalf("task_status carries no handoff: %+v", ts)
	}
	requireSchema(t, "principal-task-status-response", ts)

	// task_status stays available when the packet cannot be assembled.
	insp.err = principal.NewCodedError(principal.CodeInternal, false, nil, "boom")
	ts, _ = r.svc.TaskStatus(context.Background(), r.caller, facade.TaskStatusRequest{Meta: r.meta(""), TaskID: cand.TaskID})
	if ts.Error != nil || ts.Result == nil || ts.Result.Task == nil || ts.Result.CandidateHandoff != nil ||
		len(ts.EvidenceRefs) != 1 || ts.EvidenceRefs[0] != facade.CandidateHandoffUnavailableRef {
		t.Fatalf("task_status must degrade to no handoff: %+v", ts)
	}
}
