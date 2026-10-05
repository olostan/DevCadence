package facade_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/olostan/DevCadence/internal/principal/facade"
	"github.com/olostan/DevCadence/internal/testsupport"
)

// brokenObserver is a repository that cannot be observed.
type brokenObserver struct{ calls atomic.Int32 }

func (b *brokenObserver) Drift(context.Context, string, []string) (facade.Drift, error) {
	b.calls.Add(1)
	return facade.Drift{}, errors.New("repository unobservable")
}

// An unobservable repository must fail every repository-dependent call closed:
// an error envelope, no event appended and no port reached.
func TestDrift_UnobservableRepositoryFailsClosedWithoutEffects(t *testing.T) {
	obs := &brokenObserver{}
	r := newRig(t, func(o *facade.Options, s *spies) {
		withPorts(o, s)
		o.Repository = obs
	})
	r.initProject("", "")
	cand := r.reviewingTask("91acd8273f1", []string{"storage"})
	rev := r.revision()
	before, ctx := r.eventCount(), context.Background()
	calls := map[string]func() facade.Envelope{
		"delegate": func() facade.Envelope {
			resp, _ := r.svc.Delegate(ctx, r.caller, facade.DelegateRequest{Meta: r.meta(rev), TaskID: cand.TaskID, WorkPackage: cand.WorkPackage})
			return resp.Envelope
		},
		"validate": func() facade.Envelope {
			resp, _ := r.svc.Validate(ctx, r.caller, facade.ValidateRequest{Meta: r.meta(""), Candidate: cand, ProfileID: "p"})
			return resp.Envelope
		},
		"review": func() facade.Envelope {
			resp, _ := r.svc.Review(ctx, r.caller, facade.ReviewRequest{Meta: r.meta(""), Candidate: cand, Dimensions: []string{"correctness"}})
			return resp.Envelope
		},
		"search": func() facade.Envelope {
			resp, _ := r.svc.RequestEvidence(ctx, r.caller, facade.RequestEvidenceRequest{
				Meta: r.meta(""), Kind: "search", BaseCommit: cand.WorkPackage.BaseCommit, ScopePaths: []string{"storage"}, Query: "x", MaxMatches: 3, MaxBytes: 4096})
			return resp.Envelope
		},
		"snippet": func() facade.Envelope {
			resp, _ := r.svc.RequestEvidence(ctx, r.caller, facade.RequestEvidenceRequest{
				Meta: r.meta(""), Kind: "snippet", BaseCommit: cand.WorkPackage.BaseCommit, Path: "storage/a.go",
				StartLine: 1, EndLine: 2, Reason: "check", HitRef: "hit_1", MaxBytes: 4096})
			return resp.Envelope
		},
	}
	for name, call := range calls {
		seen := obs.calls.Load()
		if env := call(); env.Error == nil {
			t.Fatalf("%s succeeded against an unobservable repository", name)
		}
		if obs.calls.Load() == seen {
			t.Fatalf("%s failed before consulting the repository observer", name)
		}
	}
	requireCreateFailsClosed(t)
	if r.spy.total() != 0 {
		t.Fatal("an unobservable repository let a call reach a port")
	}
	if r.eventCount() != before {
		t.Fatal("an unobservable repository let a call append events")
	}
}

// create_work_package runs against a task still being designed.
func requireCreateFailsClosed(t *testing.T) {
	t.Helper()
	obs := &brokenObserver{}
	r := newRig(t, func(o *facade.Options, s *spies) {
		withPorts(o, s)
		o.Repository = obs
	})
	r.initProject("", "")
	r.designingTask()
	taskID, err := r.h.Service.ResolveTaskID(context.Background(), project, "DC-001")
	if err != nil {
		t.Fatal(err)
	}
	wp := testsupport.WorkPackage(project, taskID, "wp_0001", 1)
	wp.BaseCommit = "91acd8273f1"
	wp.Scope.InScope = []string{"storage"}
	wp.ProjectStateRevision = r.revision()
	before := r.eventCount()
	resp, _ := r.svc.CreateWorkPackage(context.Background(), r.caller,
		facade.CreateWorkPackageRequest{Meta: r.meta(wp.ProjectStateRevision), WorkPackage: *wp})
	if resp.Error == nil || obs.calls.Load() == 0 {
		t.Fatalf("create_work_package did not fail closed at the observer: %+v", resp.Envelope)
	}
	if r.eventCount() != before {
		t.Fatal("create_work_package appended events")
	}
}
