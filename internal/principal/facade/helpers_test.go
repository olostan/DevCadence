package facade_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/principal/facade"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/schema"
	"github.com/olostan/DevCadence/internal/storage"
	"github.com/olostan/DevCadence/internal/testsupport"
)

const project = "example"

// allowAll is a policy resolver that counts its checks.
type allowAll struct{ calls atomic.Int32 }

func (a *allowAll) Check(context.Context, principal.CallerContext, principal.CallMeta, string) error {
	a.calls.Add(1)
	return nil
}

// flipPolicy allows the first n checks and denies the rest, to prove the source
// policy is checked again before serialisation.
type flipPolicy struct {
	allowed int32
	calls   atomic.Int32
}

func (f *flipPolicy) Check(context.Context, principal.CallerContext, principal.CallMeta, string) error {
	if f.calls.Add(1) > f.allowed {
		return errors.New("policy no longer allows this")
	}
	return nil
}

type denyAll struct{}

func (denyAll) Check(context.Context, principal.CallerContext, principal.CallMeta, string) error {
	return errors.New("denied")
}

// spies record every port invocation.
type spies struct {
	tasks, reviews, evidence, summaries, invest, snippets atomic.Int32
	lastSnippet                                           facade.SnippetRequest
	lastDiff                                              facade.DiffRequest
	lastQuery                                             facade.EvidenceQuery
	lastSummary                                           facade.SummaryRequest
	lastInvest                                            facade.InvestigationRequest
	lastTask                                              facade.AuthorizedTask
	packet                                                protocol.EvidencePacket
	err                                                   error
	instance                                              string
}

func (s *spies) op(kind string) facade.OperationRef {
	return principal.OperationRef{ID: "op_spy", InstanceID: s.instance, Kind: kind, Status: principal.StatusRunning}
}

func (s *spies) Delegate(_ context.Context, t facade.AuthorizedTask) (facade.OperationRef, error) {
	s.tasks.Add(1)
	s.lastTask = t
	return s.op(principal.KindDelegate), s.err
}

func (s *spies) Validate(context.Context, principal.CallerContext, principal.CallMeta, principal.CandidateRef, string) (facade.OperationRef, error) {
	s.tasks.Add(1)
	return s.op(principal.KindValidate), s.err
}

func (s *spies) Review(context.Context, principal.CallerContext, principal.CallMeta, principal.CandidateRef, []string) (facade.OperationRef, error) {
	s.reviews.Add(1)
	return s.op(principal.KindReview), s.err
}

func (s *spies) Request(_ context.Context, _ principal.CallerContext, r facade.SnippetRequest) (facade.OperationRef, error) {
	s.snippets.Add(1)
	s.lastSnippet = r
	return s.op(principal.KindSnippet), s.err
}

func (s *spies) Diff(_ context.Context, _ principal.CallerContext, r facade.DiffRequest) (facade.OperationRef, error) {
	s.snippets.Add(1)
	s.lastDiff = r
	return s.op(principal.KindSnippet), s.err
}

func (s *spies) ReadSummary(_ context.Context, _ principal.CallerContext, r facade.SummaryRequest) (protocol.EvidencePacket, error) {
	s.summaries.Add(1)
	s.lastSummary = r
	return s.packet, s.err
}

func (s *spies) Investigate(_ context.Context, _ principal.CallerContext, r facade.InvestigationRequest) (protocol.EvidencePacket, error) {
	s.invest.Add(1)
	s.lastInvest = r
	return s.packet, s.err
}

func (s *spies) Read(_ context.Context, _ principal.CallerContext, q facade.EvidenceQuery) (protocol.EvidencePacket, error) {
	s.evidence.Add(1)
	s.lastQuery = q
	return s.packet, s.err
}

func (s *spies) total() int32 {
	return s.tasks.Load() + s.reviews.Load() + s.evidence.Load() + s.summaries.Load() + s.invest.Load() + s.snippets.Load()
}

type rig struct {
	t      *testing.T
	h      *testsupport.Harness
	svc    *facade.Service
	caller principal.CallerContext
	policy *allowAll
	ops    *facade.OperationRegistry
	spy    *spies
}

func fullCaller() principal.CallerContext {
	return principal.CallerContext{
		PrincipalID: "principal-1", ProjectID: project, AllowedActions: facade.ToolNames(),
		PolicyRef: "policy-1", MaxEvidenceBytes: 8192, MaxSnippetLines: 200, SourceDepth: facade.DepthSnippet,
	}
}

// newRig builds a facade over an in-memory control plane. tweak may replace
// ports before construction; by default every runtime port is nil.
func newRig(t *testing.T, tweak func(*facade.Options, *spies)) *rig {
	t.Helper()
	h := testsupport.NewHarness(t)
	ops, err := facade.NewOperationRegistry("inst_test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ops.Close)
	policy := &allowAll{}
	spy := &spies{instance: ops.InstanceID()}
	opts := facade.Options{ControlPlane: h.Service, Policy: policy, Operations: ops}
	if tweak != nil {
		tweak(&opts, spy)
	}
	svc, err := facade.NewService(opts)
	if err != nil {
		t.Fatal(err)
	}
	return &rig{t: t, h: h, svc: svc, caller: fullCaller(), policy: policy, ops: ops, spy: spy}
}

// withPorts installs every spy port.
func withPorts(opts *facade.Options, s *spies) {
	opts.Evidence, opts.Summaries, opts.Investigations, opts.Snippets, opts.Tasks, opts.Reviews = s, s, s, s, s, s
}

func (r *rig) meta(revision string) principal.CallMeta {
	return principal.CallMeta{
		SchemaVersion: principal.SchemaVersion, ProjectID: project, ExpectedStateRevision: revision, CorrelationID: "corr-1",
	}
}

func (r *rig) revision() string {
	r.t.Helper()
	st, err := r.h.Service.ProjectState(context.Background(), project)
	if err != nil {
		r.t.Fatal(err)
	}
	return st.StateRevision
}

func (r *rig) eventCount() int {
	r.t.Helper()
	stream, err := r.h.Service.Events(context.Background(), storage.EventQuery{ProjectID: project})
	if err != nil {
		r.t.Fatal(err)
	}
	return len(stream)
}

func (r *rig) lastEvent() events.Event {
	r.t.Helper()
	stream, err := r.h.Service.Events(context.Background(), storage.EventQuery{ProjectID: project})
	if err != nil || len(stream) == 0 {
		r.t.Fatalf("events: %v", err)
	}
	return stream[len(stream)-1]
}

// initProject initialises the project, optionally repository-backed.
func (r *rig) initProject(repoPath, accepted string) {
	r.t.Helper()
	if _, err := r.h.Service.InitProject(context.Background(), controlplane.InitProjectInput{
		ProjectID: project, MilestoneID: "M1", MilestoneTitle: "Domain core",
		RepositoryPath: repoPath, AcceptedCommit: accepted,
	}); err != nil {
		r.t.Fatalf("init project: %v", err)
	}
}

// designingTask creates DC-001 in the designing state and returns its id.
func (r *rig) designingTask() string {
	r.t.Helper()
	ctx := context.Background()
	if _, err := r.h.Service.CreateTask(ctx, controlplane.CreateTaskInput{
		ProjectID: project, Alias: "DC-001", Title: "Bounded reads", ChangeClass: protocol.ChangeSystemic,
	}); err != nil {
		r.t.Fatal(err)
	}
	id, err := r.h.Service.ResolveTaskID(ctx, project, "DC-001")
	if err != nil {
		r.t.Fatal(err)
	}
	r.append(&events.TaskDesignStarted{TaskID: id, Reason: "initial design"}, nil)
	return id
}

func (r *rig) append(payload events.Payload, records []controlplane.RecordToStore) {
	r.t.Helper()
	if _, err := r.h.Service.AppendTypedEvent(context.Background(), controlplane.AppendTypedEventInput{
		ProjectID: project, Payload: payload, Records: records,
	}); err != nil {
		r.t.Fatalf("append %s: %v", payload.Type(), err)
	}
}

// reviewingTask drives DC-001 to reviewing against a Work Package whose base
// and scope are given, and returns the exact candidate reference.
func (r *rig) reviewingTask(base string, scope []string) principal.CandidateRef {
	r.t.Helper()
	taskID := r.designingTask()
	const (
		wpID      = "wp_0001"
		attemptID = "att_0001"
		candidate = "cafebabe1234567"
	)
	wp := testsupport.WorkPackage(project, taskID, wpID, 1)
	wp.BaseCommit = base
	wp.Scope.InScope = scope
	wpDigest := testsupport.Digest(r.t, wp)
	validation := testsupport.AttemptValidation(project, "val_0001", taskID, attemptID, candidate, protocol.ValidationPass)
	r.append(&events.WorkPackageApproved{
		TaskID: taskID, WorkPackageID: wpID, WorkPackageVersion: 1, RecordDigest: wpDigest,
		ProjectStateRevision: wp.ProjectStateRevision, BaseCommit: base, ChangeClass: protocol.ChangeSystemic,
	}, []controlplane.RecordToStore{{Version: 1, Record: wp}})
	r.append(&events.TaskDelegated{TaskID: taskID, WorkPackageID: wpID, WorkerRole: "implementer"}, nil)
	r.append(&events.AttemptStarted{
		TaskID: taskID, AttemptID: attemptID, WorkPackageID: wpID, WorkPackageVersion: 1,
		ProjectStateRevision: "ps_000000003", BaseCommit: base, WorkerRole: "implementer",
	}, nil)
	r.append(&events.CandidateProduced{TaskID: taskID, AttemptID: attemptID, CandidateCommit: candidate, Summary: "done"}, nil)
	r.append(&events.ValidationCompleted{
		TaskID: taskID, AttemptID: attemptID, ValidationID: "val_0001", Scope: events.ScopeAttempt,
		Status: protocol.ValidationPass, Commit: candidate, RecordDigest: testsupport.Digest(r.t, validation),
	}, []controlplane.RecordToStore{{Version: 1, Record: validation}})
	return principal.CandidateRef{
		TaskID: taskID, AttemptID: attemptID, Commit: candidate,
		WorkPackage: principal.WorkPackageRef{ID: wpID, Version: 1, Digest: wpDigest, BaseCommit: base},
	}
}

func (r *rig) completeReview(c principal.CandidateRef) {
	r.t.Helper()
	review := testsupport.Review(project, "rev_0001", c.AttemptID, c.WorkPackage.ID, protocol.DimensionCorrectness, protocol.VerdictPass)
	r.append(&events.ReviewCompleted{
		TaskID: c.TaskID, AttemptID: c.AttemptID, ReviewID: "rev_0001", WorkPackageID: c.WorkPackage.ID,
		Dimension: protocol.DimensionCorrectness, Verdict: protocol.VerdictPass, RecordDigest: testsupport.Digest(r.t, review),
	}, []controlplane.RecordToStore{{Version: 1, Record: review}})
}

// validatePacket returns a minimal valid packet bound to a project and base.
func packetFor(projectID, base string) protocol.EvidencePacket {
	return protocol.EvidencePacket{
		SchemaVersion: protocol.SchemaVersion1, EvidencePacketID: "ep_0001", ProjectID: projectID,
		BaseCommit: base, Question: "q", Claims: []protocol.Claim{}, Counterevidence: []string{},
		Disagreements: []protocol.Disagreement{}, Uncertainties: []string{"unverified"},
		RawEvidenceRefs: []protocol.EvidenceRef{},
	}
}

func requireCode(t *testing.T, env facade.Envelope, code string) {
	t.Helper()
	if env.Error == nil {
		t.Fatalf("want error %s, got success", code)
	}
	if env.Error.Code != code {
		t.Fatalf("code = %s, want %s", env.Error.Code, code)
	}
	if err := env.Error.Validate(); err != nil {
		t.Fatalf("error is not a valid SemanticError: %v", err)
	}
}

func requireOK(t *testing.T, env facade.Envelope) {
	t.Helper()
	if env.Error != nil {
		t.Fatalf("unexpected error %s", env.Error.Code)
	}
}

// requireSchema checks a response against its published schema.
func requireSchema(t *testing.T, name schema.Name, v any) {
	t.Helper()
	set, err := schema.Default()
	if err != nil {
		t.Fatal(err)
	}
	if err := set.ValidateValue(name, v); err != nil {
		raw, _ := json.Marshal(v)
		t.Fatalf("value does not satisfy %s: %v\n%s", name, err, raw)
	}
}

func isNotFound(err error) bool { return errs.CategoryOf(err) == errs.CategoryNotFound }
