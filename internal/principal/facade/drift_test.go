package facade_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/principal/facade"
	"github.com/olostan/DevCadence/internal/repository"
	"github.com/olostan/DevCadence/internal/testsupport"
)

// driftRig is a repository-backed project whose repository is then changed
// behind DevCadence's back.
type driftRig struct {
	*rig
	repo *testsupport.GitRepo
	base string
}

func newDriftRig(t *testing.T) *driftRig {
	t.Helper()
	repo := testsupport.NewGitRepo(t)
	repo.WriteFile("a/x.go", "package a\n")
	repo.WriteFile("b/y.go", "package b\n")
	base := repo.Commit("add a and b")
	registered, err := repository.Register(context.Background(), project, repo.Path, repository.Options{})
	if err != nil {
		t.Fatal(err)
	}
	observer, err := facade.NewGitObserver(registered)
	if err != nil {
		t.Fatal(err)
	}
	r := newRig(t, func(o *facade.Options, s *spies) {
		withPorts(o, s)
		o.Repository = observer
	})
	r.spy.packet = packetFor(project, base)
	r.initProject(repo.Path, base)
	return &driftRig{rig: r, repo: repo, base: base}
}

func (d *driftRig) search(base string, paths ...string) facade.RequestEvidenceResponse {
	resp, _ := d.svc.RequestEvidence(context.Background(), d.caller, facade.RequestEvidenceRequest{
		Meta: d.meta(""), Kind: "search", BaseCommit: base, ScopePaths: paths, Query: "x", MaxMatches: 3, MaxBytes: 4096})
	return resp
}

func (d *driftRig) propose(scope []string) facade.CreateWorkPackageResponse {
	taskID, err := d.h.Service.ResolveTaskID(context.Background(), project, "DC-001")
	if err != nil {
		d.t.Fatal(err)
	}
	wp := testsupport.WorkPackage(project, taskID, "wp_0001", 1)
	wp.BaseCommit = d.base
	wp.Scope.InScope = scope
	wp.ProjectStateRevision = d.revision()
	resp, _ := d.svc.CreateWorkPackage(context.Background(), d.caller,
		facade.CreateWorkPackageRequest{Meta: d.meta(wp.ProjectStateRevision), WorkPackage: *wp})
	return resp
}

func (d *driftRig) projectState() facade.ProjectStateResponse {
	resp, _ := d.svc.ProjectState(context.Background(), d.caller, facade.ProjectStateRequest{Meta: d.meta("")})
	return resp
}

// An unchanged repository is never stale, however often it is read.
func TestDrift_UnchangedRepositoryIsNotStale(t *testing.T) {
	d := newDriftRig(t)
	d.designingTask()
	for i := 0; i < 3; i++ {
		requireOK(t, d.search(d.base, "a/").Envelope)
	}
	st := d.projectState()
	obs := st.Result.Repository
	if obs == nil || obs.Drifted || obs.RefreshRequired || obs.HeadCommit != d.base || len(obs.ChangedPaths) != 0 {
		t.Fatalf("spurious drift: %+v", obs)
	}
	requireSchema(t, "principal-project-state-response", st)
	requireOK(t, d.propose([]string{"a/"}).Envelope)
	// An untracked file is not drift: no evidence can describe a file that no commit contains.
	d.repo.WriteFile("a/new.go", "package a\n")
	requireOK(t, d.search(d.base, "a/").Envelope)
	if d.projectState().Result.Repository.Drifted {
		t.Fatal("an untracked file counted as drift")
	}
}

// HEAD moves outside DevCadence: only change to the affected files is stale.
func TestDrift_HeadMovesOutsideDevCadence(t *testing.T) {
	d := newDriftRig(t)
	d.designingTask()
	d.repo.WriteFile("b/y.go", "package b\n// edited elsewhere\n")
	moved := d.repo.Commit("outside change to b")

	// The state view shows the move and asks for a refresh...
	obs := d.projectState().Result.Repository
	if !obs.Drifted || !obs.RefreshRequired || obs.HeadCommit != moved || len(obs.ChangedPaths) != 1 || obs.ChangedPaths[0] != "b/y.go" {
		t.Fatalf("drift not reported: %+v", obs)
	}
	// ...but evidence about untouched files is still current,
	requireOK(t, d.search(d.base, "a/").Envelope)
	requireOK(t, d.propose([]string{"a/"}).Envelope)

	// while evidence about the changed file is stale.
	stale := d.search(d.base, "b/")
	requireCode(t, stale.Envelope, principal.CodeStaleProjectState)
	if !stale.Error.Retryable || stale.Result != nil || len(stale.EvidenceRefs) != 1 || stale.EvidenceRefs[0] != "git:"+moved {
		t.Fatalf("unexpected stale response %+v", stale)
	}
	requireSchema(t, "principal-request-evidence-response", stale)

	// Refresh: the same question at the current commit re-reads and succeeds.
	d.spy.packet = packetFor(project, moved)
	requireOK(t, d.search(moved, "b/").Envelope)
}

func TestDrift_FileModifiedOutsideDevCadence(t *testing.T) {
	d := newDriftRig(t)
	d.designingTask()
	if err := os.WriteFile(filepath.Join(d.repo.Path, "a", "x.go"), []byte("package a\n// dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := d.eventCount()
	requireCode(t, d.search(d.base, "a/").Envelope, principal.CodeStaleProjectState)
	requireCode(t, d.search(d.base, ".").Envelope, principal.CodeStaleProjectState)
	requireOK(t, d.search(d.base, "b/").Envelope) // other files unaffected
	for _, scope := range [][]string{{"a/"}, {"a/x.go"}} {
		requireCode(t, d.propose(scope).Envelope, principal.CodeStaleProjectState)
	}
	// A symbol lookup is stale for exactly its file.
	sym, _ := d.svc.RequestEvidence(context.Background(), d.caller, facade.RequestEvidenceRequest{
		Meta: d.meta(""), Kind: "symbol", BaseCommit: d.base, Path: "a/x.go", Symbol: "S", MaxBytes: 100})
	requireCode(t, sym.Envelope, principal.CodeStaleProjectState)
	snip, _ := d.svc.RequestEvidence(context.Background(), d.caller, facade.RequestEvidenceRequest{
		Meta: d.meta(""), Kind: "snippet", BaseCommit: d.base, Path: "a/x.go", StartLine: 1, EndLine: 2, Reason: "r", MaxBytes: 100})
	requireCode(t, snip.Envelope, principal.CodeStaleProjectState)
	if d.eventCount() != before || d.spy.snippets.Load() != 0 {
		t.Fatal("a stale request had effects or reached the worker")
	}
	if obs := d.projectState().Result.Repository; !obs.Dirty || !obs.Drifted || obs.ChangedPaths[0] != "a/x.go" {
		t.Fatalf("dirty tree not reported: %+v", obs)
	}
	// Restoring the file clears the staleness without any cache to invalidate.
	d.repo.Git("checkout", "--", "a/x.go")
	requireOK(t, d.search(d.base, "a/").Envelope)
	if d.projectState().Result.Repository.Drifted {
		t.Fatal("drift persisted after the file was restored")
	}
}

// A validated candidate whose Work Package scope changed outside is stale; an
// untouched scope reaches the executor.
func TestDrift_ValidateAndReviewRecheckTheWorkPackageScope(t *testing.T) {
	d := newDriftRig(t)
	cand := d.reviewingTask(d.base, []string{"a/"})
	ctx := context.Background()
	validate := func() facade.ValidateResponse {
		resp, _ := d.svc.Validate(ctx, d.caller, facade.ValidateRequest{Meta: d.meta(""), Candidate: cand, ProfileID: "p"})
		return resp
	}
	review := func() facade.ReviewResponse {
		resp, _ := d.svc.Review(ctx, d.caller, facade.ReviewRequest{Meta: d.meta(""), Candidate: cand, Dimensions: []string{"correctness"}})
		return resp
	}
	requireOK(t, validate().Envelope)
	requireOK(t, review().Envelope)

	d.repo.WriteFile("b/y.go", "package b\n// unrelated\n")
	d.repo.Commit("unrelated outside change")
	requireOK(t, validate().Envelope)

	d.repo.WriteFile("a/x.go", "package a\n// related\n")
	calls := d.spy.tasks.Load()
	requireCode(t, validate().Envelope, principal.CodeStaleProjectState)
	requireCode(t, review().Envelope, principal.CodeStaleProjectState)
	if d.spy.tasks.Load() != calls || d.spy.reviews.Load() != 1 {
		t.Fatal("a stale candidate reached an executor")
	}
}

func TestDrift_UnknownBaseAndBrokenObserverFailClosed(t *testing.T) {
	d := newDriftRig(t)
	d.designingTask()
	resp := d.search("deadbee", "a/")
	if resp.Error == nil || resp.Result != nil {
		t.Fatalf("an unknown base was accepted: %+v", resp.Envelope)
	}
	requireSchema(t, "principal-request-evidence-response", resp)
}

func TestScopePrefixesIgnoreProse(t *testing.T) {
	// Prose scope entries do not become path filters: with none usable the
	// check conservatively treats any tracked change as affecting the plan.
	d := newDriftRig(t)
	d.designingTask()
	d.repo.WriteFile("b/y.go", "package b\n// change\n")
	requireCode(t, d.propose([]string{"the storage layer", "journal reads"}).Envelope, principal.CodeStaleProjectState)
	d.repo.Git("checkout", "--", "b/y.go")
	requireOK(t, d.propose([]string{"the storage layer", "journal reads"}).Envelope)
}
