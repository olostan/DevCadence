package facade

import (
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"regexp"
	"strings"

	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/observability"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/storage"
	"github.com/olostan/DevCadence/internal/tasks"
)

// Options composes the facade. NewService is application composition, not a
// remote configuration API. Production composition MUST NOT install fake
// executors or verifiers: a nil Tasks, Reviews, Snippets or Investigations
// port denies with MODEL_UNAVAILABLE and a nil Evidence or Summaries port
// disables that operation.
type Options struct {
	ControlPlane   *controlplane.Service
	Evidence       EvidenceReader
	Summaries      SummaryReader
	Investigations Investigator
	Snippets       SnippetWorker
	Tasks          TaskExecutor
	Reviews        ReviewExecutor
	Policy         PolicyResolver
	Operations     *OperationRegistry
	// Repository re-reads the registered repository to detect change made
	// outside DevCadence. Nil disables drift detection.
	Repository RepositoryObserver
	// Logger receives diagnostics, never protocol output. Nil discards.
	Logger *slog.Logger
}

// Service is the shared application facade. Every method has the shape
// (context, CallerContext, Request) (Response, error). Ordinary semantic
// refusals are typed error responses, identical for Go and MCP callers; the Go
// error is reserved for failures that cannot be expressed as a response.
type Service struct {
	opts Options
	log  *slog.Logger
}

// NewService builds the facade. It refuses a nil ControlPlane, Policy or
// Operations.
func NewService(opts Options) (*Service, error) {
	if opts.ControlPlane == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "facade: control plane is required")
	}
	if opts.Policy == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "facade: policy resolver is required")
	}
	if opts.Operations == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "facade: operation registry is required")
	}
	logger := opts.Logger
	if logger == nil {
		logger = observability.NewLogger(observability.Options{})
	}
	return &Service{opts: opts, log: logger}, nil
}

// nilPort reports a nil interface or an interface holding a nil pointer, so a
// typed-nil adapter cannot masquerade as an installed runtime.
func nilPort(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Interface, reflect.Chan:
		return rv.IsNil()
	}
	return false
}

func missingRuntime(what string) error {
	return coded(principal.CodeModelUnavailable, false, nil, what+" runtime is not installed")
}

// admit is the common pre-effect gate: bound project, exact grant, current
// policy. It runs before any store read or port call.
func (s *Service) admit(ctx context.Context, caller CallerContext, meta CallMeta, tool string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := meta.Validate(); err != nil {
		return err
	}
	if err := caller.Authorize(meta, tool); err != nil {
		return coded(principal.CodePolicyDenied, false, nil, err.Error())
	}
	if err := s.opts.Policy.Check(ctx, caller, meta, tool); err != nil {
		return coded(principal.CodePolicyDenied, false, nil, "policy resolver denied "+tool)
	}
	return nil
}

func (s *Service) refuse(tool string, meta CallMeta, revision string, err error) Envelope {
	env := envelopeError(revision, err)
	attrs := []any{
		slog.String("tool", tool), slog.String("correlation_id", meta.CorrelationID), slog.String("code", env.Error.Code),
	}
	if env.Error.Code == principal.CodeInternal {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	s.log.Info("principal call refused", attrs...)
	return env
}

func effectiveBytes(caller CallerContext, requested int) int {
	limit := MaxEvidenceBytes
	if caller.MaxEvidenceBytes > 0 && caller.MaxEvidenceBytes < limit {
		limit = caller.MaxEvidenceBytes
	}
	if requested > 0 && requested < limit {
		limit = requested
	}
	return limit
}

func depthRank(depth string) int {
	switch depth {
	case DepthSnippet:
		return 3
	case DepthSymbol:
		return 2
	}
	return 1
}

// requireDepth checks the binding's source depth: summary reaches summaries;
// symbol adds search, symbol lookup and investigation; snippet adds mediated
// snippets and diffs.
func requireDepth(caller CallerContext, needed string) error {
	if depthRank(caller.SourceDepth) < depthRank(needed) {
		return coded(principal.CodePolicyDenied, false, nil, "source depth "+caller.SourceDepth+" is below "+needed)
	}
	return nil
}

// --- shared reads ---

func (s *Service) currentRevision(ctx context.Context, project string) (string, *protocol.ProjectState, error) {
	st, err := s.opts.ControlPlane.ProjectState(ctx, project)
	if err != nil {
		return "", nil, err
	}
	return st.StateRevision, st, nil
}

// requirePrefix compares the caller's expected prefix with the current one
// before dispatching to a port. The accepted runtime repeats the check
// transactionally; this refusal merely avoids an obviously stale dispatch.
func (s *Service) requirePrefix(ctx context.Context, meta CallMeta) (string, error) {
	revision, _, err := s.currentRevision(ctx, meta.ProjectID)
	if err != nil {
		return "", err
	}
	if meta.ExpectedStateRevision != "" && meta.ExpectedStateRevision != revision {
		return revision, controlplane.ErrStaleProjectState
	}
	return revision, nil
}

func (s *Service) loadTask(ctx context.Context, project, ref string) (*controlplane.TaskDetail, error) {
	list, err := s.opts.ControlPlane.Tasks(ctx, storage.TaskFilter{ProjectID: project})
	if err != nil {
		return nil, err
	}
	for _, t := range list {
		if t.ID == ref || t.Alias == ref {
			return s.opts.ControlPlane.TaskDetail(ctx, project, t.Alias)
		}
	}
	return nil, errs.New(errs.CategoryNotFound, "task %s does not exist", ref)
}

// approvedRef reconstructs the exact approved tuple from the latest
// WorkPackageApproved event of the task.
func approvedRef(d *controlplane.TaskDetail) (principal.WorkPackageRef, bool) {
	for i := len(d.History) - 1; i >= 0; i-- {
		if p, ok := d.History[i].Payload.(*events.WorkPackageApproved); ok {
			return principal.WorkPackageRef{
				ID: p.WorkPackageID, Version: p.WorkPackageVersion, Digest: p.RecordDigest, BaseCommit: p.BaseCommit,
			}, true
		}
	}
	return principal.WorkPackageRef{}, false
}

func taskStatusOf(d *controlplane.TaskDetail) *TaskStatus {
	t := d.Task
	out := &TaskStatus{TaskID: t.ID, Alias: t.Alias, State: string(t.State), AttemptIDs: []string{}}
	if t.Blocked != nil {
		out.BlockReason = t.Blocked.Trigger
	}
	ref, hasRef := approvedRef(d)
	if hasRef && t.WorkPackageID == ref.ID && t.WorkPackageVersion == ref.Version {
		r := ref
		out.WorkPackage = &r
	}
	for i := len(d.Attempts) - 1; i >= 0; i-- {
		if len(out.AttemptIDs) == MaxTaskAttemptList {
			out.HasMoreAttempts = true
			break
		}
		out.AttemptIDs = append(out.AttemptIDs, d.Attempts[i].ID)
	}
	if out.WorkPackage != nil {
		for _, a := range d.Attempts {
			if a.ID == t.CurrentAttemptID && a.CandidateCommit != "" {
				out.Candidate = &principal.CandidateRef{
					TaskID: t.ID, AttemptID: a.ID, WorkPackage: *out.WorkPackage, Commit: a.CandidateCommit,
				}
			}
		}
	}
	return out
}

// checkLineage verifies that a candidate reference names a real attempt of the
// task with exactly that commit, and that the task's approved tuple is the
// candidate's Work Package.
func (s *Service) checkLineage(ctx context.Context, project string, c principal.CandidateRef) (*controlplane.TaskDetail, error) {
	d, err := s.loadTask(ctx, project, c.TaskID)
	if err != nil {
		return nil, err
	}
	if d.Task.ID != c.TaskID {
		return nil, invalid("candidate names a task alias, not the task id")
	}
	if d.Task.WorkPackageID != c.WorkPackage.ID || d.Task.WorkPackageVersion != c.WorkPackage.Version {
		return nil, controlplane.ErrStaleWorkPackage
	}
	for _, a := range d.Attempts {
		if a.ID == c.AttemptID {
			if a.CandidateCommit != c.Commit {
				return nil, invalid("candidate commit does not match the attempt")
			}
			return d, nil
		}
	}
	return nil, errs.New(errs.CategoryNotFound, "attempt %s does not exist", c.AttemptID)
}

// --- drift detection ---

var pathLike = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

// scopePrefixes extracts repository path prefixes from a Work Package's
// declared scope and anchors. Entries that are prose are ignored. An empty
// result means "any tracked change counts", the conservative reading.
func scopePrefixes(wp *protocol.EngineeringWorkPackage) []string {
	var out []string
	seen := map[string]bool{}
	add := func(entry string) {
		entry = strings.TrimSpace(entry)
		for _, suffix := range []string{"/**", "/*", "/...", "*"} {
			entry = strings.TrimSuffix(entry, suffix)
		}
		if entry == "" || !pathLike.MatchString(entry) || strings.HasPrefix(entry, "/") || strings.Contains(entry, "..") {
			return
		}
		if !strings.ContainsAny(entry, "/.") {
			return
		}
		if !seen[entry] {
			seen[entry] = true
			out = append(out, entry)
		}
	}
	for _, e := range wp.Scope.InScope {
		add(e)
	}
	for _, e := range wp.RepositoryAnchors {
		add(e)
	}
	return out
}

// checkDrift re-reads the repository and refuses when any affected file
// changed since base. It never caches: each call observes the live tree, so a
// refresh (project_state, then a new request) always sees current content.
func (s *Service) checkDrift(ctx context.Context, base string, prefixes []string) error {
	if nilPort(s.opts.Repository) {
		return nil
	}
	d, err := s.opts.Repository.Drift(ctx, base, prefixes)
	if err != nil {
		return err
	}
	if len(d.ChangedPaths) > 0 {
		return staleRepository(d.HeadCommit, len(d.ChangedPaths), "repository changed outside DevCadence since the base")
	}
	return nil
}

// checkWorkPackageFresh loads the exact stored Work Package of a reference,
// verifies its digest and base, and checks its scope for outside change.
func (s *Service) checkWorkPackageFresh(ctx context.Context, project string, ref principal.WorkPackageRef) error {
	stored, err := s.opts.ControlPlane.Record(ctx, project, "EngineeringWorkPackage", ref.ID, ref.Version)
	if err != nil {
		return err
	}
	if stored.Digest != ref.Digest {
		return controlplane.ErrStaleWorkPackage
	}
	var wp protocol.EngineeringWorkPackage
	if err := protocol.Unmarshal([]byte(stored.Document), &wp); err != nil {
		return errs.Wrap(errs.CategoryIntegrity, err, "stored work package cannot be decoded")
	}
	if wp.BaseCommit != ref.BaseCommit {
		return controlplane.ErrStaleWorkPackage
	}
	return s.checkDrift(ctx, wp.BaseCommit, scopePrefixes(&wp))
}

// --- project_state ---

// ProjectState returns canonical state or a focused projection, plus a live
// observation of the repository so parallel outside change is visible.
func (s *Service) ProjectState(ctx context.Context, caller CallerContext, req ProjectStateRequest) (ProjectStateResponse, error) {
	if err := s.admit(ctx, caller, req.Meta, ToolProjectState); err != nil {
		return ProjectStateResponse{Envelope: s.refuse(ToolProjectState, req.Meta, "", err)}, nil
	}
	if err := req.Validate(); err != nil {
		return ProjectStateResponse{Envelope: s.refuse(ToolProjectState, req.Meta, "", err)}, nil
	}
	var st *protocol.ProjectState
	var err error
	if req.AtRevision != "" {
		n, _ := principal.ParseStateRevision(req.AtRevision)
		st, err = s.opts.ControlPlane.ProjectStateAt(ctx, req.Meta.ProjectID, n)
		if err == nil && st.StateRevision != req.AtRevision {
			err = errs.New(errs.CategoryNotFound, "revision %s does not exist", req.AtRevision)
		}
	} else {
		st, err = s.opts.ControlPlane.ProjectState(ctx, req.Meta.ProjectID)
	}
	if err != nil {
		return ProjectStateResponse{Envelope: s.refuse(ToolProjectState, req.Meta, "", err)}, nil
	}
	focus := req.Focus
	if focus == "" {
		focus = FocusProject
	}
	result := &ProjectStateResult{Focus: focus, SourceRevision: st.StateRevision}
	refs := []string{}
	switch focus {
	case FocusProject:
		result.ProjectState = st
	case FocusDiscovery:
		discovery := st.Discovery
		if discovery == nil {
			discovery = &protocol.DiscoveryState{}
		}
		copied := *discovery
		result.Discovery = &copied
	case FocusTask:
		d, err := s.loadTask(ctx, req.Meta.ProjectID, req.TaskID)
		if err != nil {
			return ProjectStateResponse{Envelope: s.refuse(ToolProjectState, req.Meta, st.StateRevision, err)}, nil
		}
		result.Task = taskStatusOf(d)
	}
	if req.AtRevision == "" && st.Git.AcceptedCommit != nil && !nilPort(s.opts.Repository) {
		accepted := *st.Git.AcceptedCommit
		d, derr := s.opts.Repository.Drift(ctx, accepted, nil)
		if derr != nil {
			refs = append(refs, "repository:unobserved")
		} else {
			changed := append([]string{}, d.ChangedPaths...)
			if len(changed) > 32 {
				changed = changed[:32]
			}
			result.Repository = &RepositoryObservation{
				HeadCommit: d.HeadCommit, AcceptedCommit: accepted, Dirty: d.Dirty,
				Drifted: len(d.ChangedPaths) > 0, ChangedPaths: changed, RefreshRequired: len(d.ChangedPaths) > 0,
			}
		}
	}
	return ProjectStateResponse{Envelope: envelopeOK(st.StateRevision, refs), Result: result}, nil
}

// --- investigate / request_evidence ---

func (s *Service) packetFit(caller CallerContext, meta CallMeta, base string, requested int, p protocol.EvidencePacket) (protocol.EvidencePacket, error) {
	if err := p.Validate(); err != nil {
		return p, errs.Wrap(errs.CategoryIntegrity, err, "evidence packet is not valid")
	}
	if p.ProjectID != meta.ProjectID || (base != "" && p.BaseCommit != base) {
		return p, errs.New(errs.CategoryIntegrity, "evidence packet is bound to another project or base")
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return p, errs.Wrap(errs.CategoryInternal, err, "marshal evidence packet")
	}
	if len(raw) > effectiveBytes(caller, requested) {
		return p, coded(principal.CodeContextUnfit, true, nil, "evidence exceeds the byte cap; narrow the request")
	}
	return p, nil
}

// Investigate hands the question, verbatim and as untrusted data, to the scout
// runtime. A nil Investigator denies rather than guessing a search query.
func (s *Service) Investigate(ctx context.Context, caller CallerContext, req InvestigateRequest) (InvestigateResponse, error) {
	fail := func(revision string, err error) (InvestigateResponse, error) {
		return InvestigateResponse{Envelope: s.refuse(ToolInvestigate, req.Meta, revision, err)}, nil
	}
	if err := s.admit(ctx, caller, req.Meta, ToolInvestigate); err != nil {
		return fail("", err)
	}
	if err := req.Validate(); err != nil {
		return fail("", err)
	}
	if err := requireDepth(caller, DepthSymbol); err != nil {
		return fail("", err)
	}
	if req.MaxBytes > effectiveBytes(caller, 0) {
		return fail("", coded(principal.CodeContextUnfit, false, nil, "max_bytes exceeds the binding cap"))
	}
	if nilPort(s.opts.Investigations) {
		return fail("", missingRuntime("investigation"))
	}
	if err := s.checkDrift(ctx, req.BaseCommit, req.ScopePaths); err != nil {
		return fail("", err)
	}
	packet, err := s.opts.Investigations.Investigate(ctx, caller, InvestigationRequest{
		Meta: req.Meta, Question: req.Question, BaseCommit: req.BaseCommit,
		ScopePaths: append([]string(nil), req.ScopePaths...), MaxBytes: req.MaxBytes,
	})
	if err != nil {
		return fail("", err)
	}
	if err := s.opts.Policy.Check(ctx, caller, req.Meta, ToolInvestigate); err != nil {
		return fail("", coded(principal.CodePolicyDenied, false, nil, "policy changed before release"))
	}
	packet, err = s.packetFit(caller, req.Meta, req.BaseCommit, req.MaxBytes, packet)
	if err != nil {
		return fail("", err)
	}
	return InvestigateResponse{Envelope: envelopeOK("", nil), Result: &InvestigateResult{EvidencePacket: packet}}, nil
}

// RequestEvidence dispatches the closed evidence union to exactly one port.
func (s *Service) RequestEvidence(ctx context.Context, caller CallerContext, req RequestEvidenceRequest) (RequestEvidenceResponse, error) {
	fail := func(err error) (RequestEvidenceResponse, error) {
		return RequestEvidenceResponse{Envelope: s.refuse(ToolRequestEvidence, req.Meta, "", err)}, nil
	}
	if err := s.admit(ctx, caller, req.Meta, ToolRequestEvidence); err != nil {
		return fail(err)
	}
	if err := req.Validate(); err != nil {
		return fail(err)
	}
	needed := DepthSymbol
	switch req.Kind {
	case EvidenceSummary:
		needed = DepthSummary
	case EvidenceSnippet, EvidenceDiff:
		needed = DepthSnippet
	}
	if err := requireDepth(caller, needed); err != nil {
		return fail(err)
	}
	if req.MaxBytes > effectiveBytes(caller, 0) {
		return fail(coded(principal.CodeContextUnfit, false, nil, "max_bytes exceeds the binding cap"))
	}
	if (req.Kind == EvidenceSnippet || req.Kind == EvidenceDiff) && caller.MaxSnippetLines > 0 &&
		req.EndLine-req.StartLine+1 > caller.MaxSnippetLines {
		return fail(coded(principal.CodeContextUnfit, false, nil, "line span exceeds the binding cap"))
	}
	result := &EvidenceResult{Kind: req.Kind}
	switch req.Kind {
	case EvidenceSummary:
		if nilPort(s.opts.Summaries) {
			return fail(coded(principal.CodeModelUnavailable, false, nil, "summary reader is not installed"))
		}
		packet, err := s.opts.Summaries.ReadSummary(ctx, caller, SummaryRequest{
			Meta:     req.Meta,
			Record:   EvidenceRecordRef{Kind: req.RecordKind, ID: req.RecordID, Digest: req.Digest, Version: req.RecordVersion},
			MaxBytes: req.MaxBytes,
		})
		if err != nil {
			return fail(err)
		}
		if err := s.opts.Policy.Check(ctx, caller, req.Meta, ToolRequestEvidence); err != nil {
			return fail(coded(principal.CodePolicyDenied, false, nil, "policy changed before release"))
		}
		if packet, err = s.packetFit(caller, req.Meta, "", req.MaxBytes, packet); err != nil {
			return fail(err)
		}
		result.EvidencePacket = &packet
	case EvidenceSearch, EvidenceSymbol:
		if nilPort(s.opts.Evidence) {
			return fail(coded(principal.CodeModelUnavailable, false, nil, "evidence reader is not installed"))
		}
		prefixes := req.ScopePaths
		if req.Kind == EvidenceSymbol {
			prefixes = []string{req.Path}
		}
		if err := s.checkDrift(ctx, req.BaseCommit, prefixes); err != nil {
			return fail(err)
		}
		packet, err := s.opts.Evidence.Read(ctx, caller, EvidenceQuery{
			Meta: req.Meta, Kind: req.Kind, BaseCommit: req.BaseCommit, Path: req.Path, Query: req.Query,
			Symbol: req.Symbol, ScopePaths: append([]string(nil), req.ScopePaths...),
			MaxMatches: req.MaxMatches, MaxBytes: req.MaxBytes,
		})
		if err != nil {
			return fail(err)
		}
		if err := s.opts.Policy.Check(ctx, caller, req.Meta, ToolRequestEvidence); err != nil {
			return fail(coded(principal.CodePolicyDenied, false, nil, "policy changed before release"))
		}
		if packet, err = s.packetFit(caller, req.Meta, req.BaseCommit, req.MaxBytes, packet); err != nil {
			return fail(err)
		}
		result.EvidencePacket = &packet
	case EvidenceSnippet:
		if nilPort(s.opts.Snippets) {
			return fail(coded(principal.CodeModelUnavailable, false, nil, "snippet worker is not installed"))
		}
		if err := s.checkDrift(ctx, req.BaseCommit, []string{req.Path}); err != nil {
			return fail(err)
		}
		ref, err := s.opts.Snippets.Request(ctx, caller, SnippetRequest{
			Meta: req.Meta, BaseCommit: req.BaseCommit, Path: req.Path, Reason: req.Reason, HitRef: req.HitRef,
			StartLine: req.StartLine, EndLine: req.EndLine, MaxBytes: effectiveBytes(caller, req.MaxBytes),
		})
		if err != nil {
			return fail(err)
		}
		if err := s.checkOperation(ref, principal.KindSnippet); err != nil {
			return fail(err)
		}
		result.Operation = &ref
	case EvidenceDiff:
		if nilPort(s.opts.Snippets) {
			return fail(coded(principal.CodeModelUnavailable, false, nil, "snippet worker is not installed"))
		}
		if _, err := s.checkLineage(ctx, req.Meta.ProjectID, *req.Candidate); err != nil {
			return fail(err)
		}
		ref, err := s.opts.Snippets.Diff(ctx, caller, DiffRequest{
			Meta: req.Meta, Candidate: *req.Candidate, Path: req.Path, Reason: req.Reason,
			StartLine: req.StartLine, EndLine: req.EndLine, MaxBytes: effectiveBytes(caller, req.MaxBytes),
		})
		if err != nil {
			return fail(err)
		}
		if err := s.checkOperation(ref, principal.KindSnippet); err != nil {
			return fail(err)
		}
		result.Operation = &ref
	}
	return RequestEvidenceResponse{Envelope: envelopeOK("", nil), Result: result}, nil
}

// checkOperation accepts only a well-formed handle of this process instance and
// the expected kind, so a port cannot return a handle the registry could never
// resolve.
func (s *Service) checkOperation(ref OperationRef, kind string) error {
	if err := ref.Validate(); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "port returned an invalid operation handle")
	}
	if ref.InstanceID != s.opts.Operations.InstanceID() || ref.Kind != kind {
		return errs.New(errs.CategoryInternal, "port returned a handle of another instance or kind")
	}
	return nil
}

// --- create_work_package ---

// CreateWorkPackage persists an immutable proposal. It is distinct from
// approval: no task state changes and nothing is delegated.
func (s *Service) CreateWorkPackage(ctx context.Context, caller CallerContext, req CreateWorkPackageRequest) (CreateWorkPackageResponse, error) {
	fail := func(revision string, err error) (CreateWorkPackageResponse, error) {
		return CreateWorkPackageResponse{Envelope: s.refuse(ToolCreateWorkPackage, req.Meta, revision, err)}, nil
	}
	if err := s.admit(ctx, caller, req.Meta, ToolCreateWorkPackage); err != nil {
		return fail("", err)
	}
	if err := req.Validate(); err != nil {
		return fail("", err)
	}
	wp := req.WorkPackage
	if wp.ProjectID != req.Meta.ProjectID {
		return fail("", invalid("work package belongs to another project"))
	}
	if wp.ProjectStateRevision != req.Meta.ExpectedStateRevision {
		return fail("", invalid("work package planning revision differs from the expected state revision"))
	}
	revision, st, err := s.currentRevision(ctx, req.Meta.ProjectID)
	if err != nil {
		return fail("", err)
	}
	if revision != req.Meta.ExpectedStateRevision {
		return fail(revision, controlplane.ErrStaleProjectState)
	}
	d, err := s.loadTask(ctx, req.Meta.ProjectID, wp.TaskID)
	if err != nil {
		return fail(revision, err)
	}
	if d.Task.ID != wp.TaskID {
		return fail(revision, invalid("work package names a task alias, not the task id"))
	}
	if d.Task.State != tasks.StateDesigning {
		return fail(revision, invalid("task is not designing"))
	}
	if st.Git.AcceptedCommit != nil && *st.Git.AcceptedCommit != wp.BaseCommit {
		return fail(revision, coded(principal.CodeStaleProjectState, true, nil, "base is not the accepted base"))
	}
	if err := s.checkDrift(ctx, wp.BaseCommit, scopePrefixes(&wp)); err != nil {
		return fail(revision, err)
	}
	digest, err := protocol.Digest(&wp)
	if err != nil {
		return fail(revision, err)
	}
	payload := &events.WorkPackageProposed{
		TaskID: wp.TaskID, WorkPackageID: wp.WorkPackageID, Version: wp.Version,
		ProjectStateRevision: wp.ProjectStateRevision, BaseCommit: wp.BaseCommit, RecordDigest: digest,
	}
	actor := protocol.Actor{Kind: protocol.ActorPrincipal, ID: caller.PrincipalID}
	corr := events.CorrelationFor(payload)
	out, err := s.opts.ControlPlane.ApplyBatch(ctx, controlplane.BatchCommand{
		ProjectID: req.Meta.ProjectID, Actor: actor, Correlation: corr,
		ExpectedStateRevision: req.Meta.ExpectedStateRevision,
		Commands: []controlplane.Command{{
			ProjectID: req.Meta.ProjectID, Actor: actor, Correlation: corr, Payload: payload,
			Records: []controlplane.RecordToStore{{Version: wp.Version, Record: &wp}},
		}},
	})
	if err != nil {
		return fail("", err)
	}
	return CreateWorkPackageResponse{
		Envelope: envelopeOK(out.ProjectState.StateRevision, nil),
		Result: &CreateWorkPackageResult{
			TaskID: wp.TaskID, Status: "proposed",
			WorkPackage: principal.WorkPackageRef{
				ID: wp.WorkPackageID, Version: wp.Version, Digest: digest, BaseCommit: wp.BaseCommit,
			},
		},
	}, nil
}

// --- delegate / validate / review ---

// Delegate dispatches an approved Work Package to the accepted runtime. With no
// runtime installed it denies before any read or write.
func (s *Service) Delegate(ctx context.Context, caller CallerContext, req DelegateRequest) (DelegateResponse, error) {
	fail := func(revision string, err error) (DelegateResponse, error) {
		return DelegateResponse{Envelope: s.refuse(ToolDelegate, req.Meta, revision, err)}, nil
	}
	if err := s.admit(ctx, caller, req.Meta, ToolDelegate); err != nil {
		return fail("", err)
	}
	if err := req.Validate(); err != nil {
		return fail("", err)
	}
	if nilPort(s.opts.Tasks) {
		return fail("", missingRuntime("task execution"))
	}
	revision, err := s.requirePrefix(ctx, req.Meta)
	if err != nil {
		return fail(revision, err)
	}
	d, err := s.loadTask(ctx, req.Meta.ProjectID, req.TaskID)
	if err != nil {
		return fail(revision, err)
	}
	approved, ok := approvedRef(d)
	if !ok || d.Task.WorkPackageID != req.WorkPackage.ID || d.Task.WorkPackageVersion != req.WorkPackage.Version ||
		approved != req.WorkPackage {
		return fail(revision, controlplane.ErrStaleWorkPackage)
	}
	if err := s.checkWorkPackageFresh(ctx, req.Meta.ProjectID, req.WorkPackage); err != nil {
		return fail(revision, err)
	}
	ref, err := s.opts.Tasks.Delegate(ctx, AuthorizedTask{
		Caller: caller, Meta: req.Meta, TaskID: d.Task.ID, WorkPackage: req.WorkPackage,
	})
	if err != nil {
		return fail(revision, err)
	}
	if err := s.checkOperation(ref, principal.KindDelegate); err != nil {
		return fail(revision, err)
	}
	return DelegateResponse{Envelope: envelopeOK(revision, nil), Result: &OperationResult{Operation: ref}}, nil
}

// Validate dispatches deterministic validation of a candidate.
func (s *Service) Validate(ctx context.Context, caller CallerContext, req ValidateRequest) (ValidateResponse, error) {
	fail := func(revision string, err error) (ValidateResponse, error) {
		return ValidateResponse{Envelope: s.refuse(ToolValidate, req.Meta, revision, err)}, nil
	}
	if err := s.admit(ctx, caller, req.Meta, ToolValidate); err != nil {
		return fail("", err)
	}
	if err := req.Validate(); err != nil {
		return fail("", err)
	}
	if nilPort(s.opts.Tasks) {
		return fail("", missingRuntime("validation"))
	}
	revision, err := s.requirePrefix(ctx, req.Meta)
	if err != nil {
		return fail(revision, err)
	}
	if _, err := s.checkLineage(ctx, req.Meta.ProjectID, req.Candidate); err != nil {
		return fail(revision, err)
	}
	if err := s.checkWorkPackageFresh(ctx, req.Meta.ProjectID, req.Candidate.WorkPackage); err != nil {
		return fail(revision, err)
	}
	ref, err := s.opts.Tasks.Validate(ctx, caller, req.Meta, req.Candidate, req.ProfileID)
	if err != nil {
		return fail(revision, err)
	}
	if err := s.checkOperation(ref, principal.KindValidate); err != nil {
		return fail(revision, err)
	}
	return ValidateResponse{Envelope: envelopeOK(revision, nil), Result: &OperationResult{Operation: ref}}, nil
}

// Review dispatches independent review of a candidate.
func (s *Service) Review(ctx context.Context, caller CallerContext, req ReviewRequest) (ReviewResponse, error) {
	fail := func(revision string, err error) (ReviewResponse, error) {
		return ReviewResponse{Envelope: s.refuse(ToolReview, req.Meta, revision, err)}, nil
	}
	if err := s.admit(ctx, caller, req.Meta, ToolReview); err != nil {
		return fail("", err)
	}
	if err := req.Validate(); err != nil {
		return fail("", err)
	}
	if nilPort(s.opts.Reviews) {
		return fail("", missingRuntime("review"))
	}
	revision, err := s.requirePrefix(ctx, req.Meta)
	if err != nil {
		return fail(revision, err)
	}
	if _, err := s.checkLineage(ctx, req.Meta.ProjectID, req.Candidate); err != nil {
		return fail(revision, err)
	}
	if err := s.checkWorkPackageFresh(ctx, req.Meta.ProjectID, req.Candidate.WorkPackage); err != nil {
		return fail(revision, err)
	}
	ref, err := s.opts.Reviews.Review(ctx, caller, req.Meta, req.Candidate, append([]string(nil), req.Dimensions...))
	if err != nil {
		return fail(revision, err)
	}
	if err := s.checkOperation(ref, principal.KindReview); err != nil {
		return fail(revision, err)
	}
	return ReviewResponse{Envelope: envelopeOK(revision, nil), Result: &OperationResult{Operation: ref}}, nil
}

// --- task_status ---

// TaskStatus reports a task or a process-scoped operation: handles, never logs.
func (s *Service) TaskStatus(ctx context.Context, caller CallerContext, req TaskStatusRequest) (TaskStatusResponse, error) {
	fail := func(revision string, err error) (TaskStatusResponse, error) {
		return TaskStatusResponse{Envelope: s.refuse(ToolTaskStatus, req.Meta, revision, err)}, nil
	}
	if err := s.admit(ctx, caller, req.Meta, ToolTaskStatus); err != nil {
		return fail("", err)
	}
	if err := req.Validate(); err != nil {
		return fail("", err)
	}
	if req.Operation != nil {
		status, err := s.opts.Operations.Lookup(req.Meta.ProjectID, *req.Operation)
		if err != nil {
			return fail("", err)
		}
		return TaskStatusResponse{Envelope: envelopeOK("", nil), Result: &TaskStatusResult{Operation: &status}}, nil
	}
	revision, _, err := s.currentRevision(ctx, req.Meta.ProjectID)
	if err != nil {
		return fail("", err)
	}
	d, err := s.loadTask(ctx, req.Meta.ProjectID, req.TaskID)
	if err != nil {
		return fail(revision, err)
	}
	return TaskStatusResponse{Envelope: envelopeOK(revision, nil), Result: &TaskStatusResult{Task: taskStatusOf(d)}}, nil
}

// --- accept / reject / record_decision ---

// AcceptanceUnavailableRef is the safe evidence handle of the hard-disabled
// acceptance operation.
const AcceptanceUnavailableRef = "acceptance-runtime-unavailable"

// Accept is hard-disabled in this slice. After structural, project and action
// checks it returns NEEDS_PRINCIPAL with zero journal, storage or Git effects,
// irrespective of grants or evidence. No option or injected gate enables it.
func (s *Service) Accept(ctx context.Context, caller CallerContext, req AcceptRequest) (AcceptResponse, error) {
	if err := s.admit(ctx, caller, req.Meta, ToolAccept); err != nil {
		return AcceptResponse{Envelope: s.refuse(ToolAccept, req.Meta, "", err)}, nil
	}
	if err := req.Validate(); err != nil {
		return AcceptResponse{Envelope: s.refuse(ToolAccept, req.Meta, "", err)}, nil
	}
	err := coded(principal.CodeNeedsPrincipal, false, []string{AcceptanceUnavailableRef}, "acceptance is disabled")
	return AcceptResponse{Envelope: s.refuse(ToolAccept, req.Meta, "", err)}, nil
}

// Reject records ChangeRejected for the current reviewing candidate. It never
// starts another attempt and never changes a budget.
func (s *Service) Reject(ctx context.Context, caller CallerContext, req RejectRequest) (RejectResponse, error) {
	fail := func(revision string, err error) (RejectResponse, error) {
		return RejectResponse{Envelope: s.refuse(ToolReject, req.Meta, revision, err)}, nil
	}
	if err := s.admit(ctx, caller, req.Meta, ToolReject); err != nil {
		return fail("", err)
	}
	if err := req.Validate(); err != nil {
		return fail("", err)
	}
	revision, err := s.requirePrefix(ctx, req.Meta)
	if err != nil {
		return fail(revision, err)
	}
	d, err := s.checkLineage(ctx, req.Meta.ProjectID, req.Candidate)
	if err != nil {
		return fail(revision, err)
	}
	if d.Task.State != tasks.StateReviewing {
		return fail(revision, coded(principal.CodeStaleProjectState, false, nil, "task is not reviewing"))
	}
	if d.Task.CurrentAttemptID != req.Candidate.AttemptID {
		return fail(revision, invalid("candidate is not the current attempt"))
	}
	payload := &events.ChangeRejected{
		TaskID: d.Task.ID, AttemptID: req.Candidate.AttemptID, Reason: req.Reason,
		RequestedRepairs: append([]string(nil), req.RequestedRepairs...), DecidedBy: protocol.AuthorityPrincipal,
	}
	actor := protocol.Actor{Kind: protocol.ActorPrincipal, ID: caller.PrincipalID}
	corr := events.CorrelationFor(payload)
	out, err := s.opts.ControlPlane.ApplyBatch(ctx, controlplane.BatchCommand{
		ProjectID: req.Meta.ProjectID, Actor: actor, Correlation: corr,
		ExpectedStateRevision: req.Meta.ExpectedStateRevision,
		Commands:              []controlplane.Command{{ProjectID: req.Meta.ProjectID, Actor: actor, Correlation: corr, Payload: payload}},
	})
	if err != nil {
		return fail("", err)
	}
	return RejectResponse{
		Envelope: envelopeOK(out.ProjectState.StateRevision, nil),
		Result:   &RejectResult{TaskID: d.Task.ID, AttemptID: req.Candidate.AttemptID, State: string(tasks.StateRunning)},
	}, nil
}

// RecordDecision stores an immutable DecisionRecord with its existing event. It
// confirms no human product decision and mutates no policy or Git state.
func (s *Service) RecordDecision(ctx context.Context, caller CallerContext, req RecordDecisionRequest) (RecordDecisionResponse, error) {
	fail := func(revision string, err error) (RecordDecisionResponse, error) {
		return RecordDecisionResponse{Envelope: s.refuse(ToolRecordDecision, req.Meta, revision, err)}, nil
	}
	if err := s.admit(ctx, caller, req.Meta, ToolRecordDecision); err != nil {
		return fail("", err)
	}
	if err := req.Validate(); err != nil {
		return fail("", err)
	}
	decision := req.Decision
	if decision.ProjectID != req.Meta.ProjectID {
		return fail("", invalid("decision belongs to another project"))
	}
	digest, err := protocol.Digest(&decision)
	if err != nil {
		return fail("", err)
	}
	payload := &events.DecisionRecorded{
		DecisionID: decision.DecisionID, RecordDigest: digest, Question: decision.Question,
		Selected: decision.Selected, InvariantChanges: append([]string(nil), decision.InvariantChanges...),
	}
	if decision.ADRRef != nil {
		payload.ADRRef = *decision.ADRRef
	}
	actor := protocol.Actor{Kind: protocol.ActorPrincipal, ID: caller.PrincipalID}
	corr := events.CorrelationFor(payload)
	out, err := s.opts.ControlPlane.ApplyBatch(ctx, controlplane.BatchCommand{
		ProjectID: req.Meta.ProjectID, Actor: actor, Correlation: corr,
		ExpectedStateRevision: req.Meta.ExpectedStateRevision,
		Commands: []controlplane.Command{{
			ProjectID: req.Meta.ProjectID, Actor: actor, Correlation: corr, Payload: payload,
			Records: []controlplane.RecordToStore{{Version: 1, Record: &decision}},
		}},
	})
	if err != nil {
		return fail("", err)
	}
	return RecordDecisionResponse{
		Envelope: envelopeOK(out.ProjectState.StateRevision, nil),
		Result:   &RecordDecisionResult{DecisionID: decision.DecisionID, Digest: digest},
	}, nil
}
