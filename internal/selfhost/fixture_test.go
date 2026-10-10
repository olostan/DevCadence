package selfhost_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/observability"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/principal/facade"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/selfhost"
	"github.com/olostan/DevCadence/internal/storage"
	"github.com/olostan/DevCadence/internal/tasks"
	"github.com/olostan/DevCadence/internal/testsupport"
)

const (
	testProject = "proj-selfhost"
	testModel   = "stub-coder:7b"
	testDigest  = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

const buggyCalc = "package calc\n\n// Add returns the sum of a and b.\nfunc Add(a, b int) int { return a - b }\n"

const fixedCalc = "package calc\n\n// Add returns the sum of a and b.\nfunc Add(a, b int) int { return a + b }\n"

const calcTest = `package calc

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Fatalf("Add(2, 3) = %d, want 5", got)
	}
}
`

// stubOllama impersonates the three Ollama endpoints DevCadence uses. It is a
// deterministic STUB: tests built on it prove composition and state handling,
// not that any real model can do the work.
type stubOllama struct {
	srv     *httptest.Server
	version string
	models  []map[string]string
	// chat decides the n-th (1-based) /api/chat reply.
	chat func(n int, req map[string]any) (status int, body any)

	mu       sync.Mutex
	chatReqs []map[string]any
}

func newStubOllama(t *testing.T, chat func(n int, req map[string]any) (int, any)) *stubOllama {
	t.Helper()
	s := &stubOllama{
		version: "0.9.9-stub",
		models:  []map[string]string{{"name": testModel, "model": testModel, "digest": testDigest}},
		chat:    chat,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"version": s.version})
	})
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"models": s.models})
	})
	mux.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		s.mu.Lock()
		s.chatReqs = append(s.chatReqs, req)
		n := len(s.chatReqs)
		s.mu.Unlock()
		status, body := s.chat(n, req)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stubOllama) chatCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.chatReqs)
}

func (s *stubOllama) config() selfhost.Config {
	return selfhost.Config{OllamaURL: s.srv.URL, Model: testModel}
}

func toolCall(name string, args map[string]string) map[string]any {
	return map[string]any{"function": map[string]any{"name": name, "arguments": args}}
}

// scriptedFix replies with two write_file calls, then a final message. When
// withCounts is true the stub reports Ollama eval counts; otherwise it omits them.
func scriptedFix(withCounts bool) func(int, map[string]any) (int, any) {
	return func(n int, _ map[string]any) (int, any) {
		resp := map[string]any{"model": testModel, "done": true}
		if n == 1 {
			resp["message"] = map[string]any{
				"role": "assistant", "content": "Fixing Add and adding a test.",
				"tool_calls": []any{
					toolCall("write_file", map[string]string{"path": "calc.go", "content": fixedCalc}),
					toolCall("write_file", map[string]string{"path": "calc_test.go", "content": calcTest}),
				},
			}
			if withCounts {
				resp["prompt_eval_count"], resp["eval_count"] = 120, 30
			}
			return http.StatusOK, resp
		}
		resp["message"] = map[string]any{"role": "assistant", "content": "Done: Add now returns a+b."}
		if withCounts {
			resp["prompt_eval_count"], resp["eval_count"] = 150, 10
		}
		return http.StatusOK, resp
	}
}

// fixture is a disposable git repo, a control plane with an approved work
// package, and the real executor built by selfhost.Build.
type fixture struct {
	harness  *testsupport.Harness
	git      *testsupport.GitRepo
	registry *facade.OperationRegistry
	deps     selfhost.Deps
	built    *selfhost.Built
	taskID   string
	wp       *protocol.EngineeringWorkPackage
	wpDigest string
}

func newFixture(t *testing.T, extra ...map[string]string) *fixture {
	t.Helper()
	h := testsupport.NewHarness(t)
	g := testsupport.NewGitRepo(t)
	g.WriteFile("go.mod", "module example.com/calc\n\ngo 1.21\n")
	g.WriteFile("calc.go", buggyCalc)
	for _, files := range extra {
		for path, content := range files {
			g.WriteFile(path, content)
		}
	}
	g.Commit("add calc with a bug")

	registry, err := facade.NewOperationRegistry("selfhost-test")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	t.Cleanup(registry.Close)
	stateDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{harness: h, git: g, registry: registry}
	f.deps = selfhost.Deps{
		ProjectID: testProject, RepoPath: g.Path, StateDir: stateDir,
		ControlPlane: h.Service, Registry: registry, Clock: h.Clock, IDs: h.IDs,
		Logger: observability.NewLogger(observability.Options{}),
	}
	f.approveWorkPackage(t)
	return f
}

func (f *fixture) build(t *testing.T, cfg selfhost.Config) *selfhost.Built {
	t.Helper()
	b, err := selfhost.Build(context.Background(), cfg, f.deps)
	if err != nil {
		t.Fatalf("selfhost.Build: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	f.built = b
	return b
}

func (f *fixture) approveWorkPackage(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	svc := f.harness.Service
	if _, err := svc.InitProject(ctx, controlplane.InitProjectInput{
		ProjectID: testProject, Name: "Selfhost", MilestoneID: "M1", MilestoneTitle: "Milestone 1",
	}); err != nil {
		t.Fatalf("init project: %v", err)
	}
	if _, err := svc.CreateTask(ctx, controlplane.CreateTaskInput{
		ProjectID: testProject, Alias: "SH-1", Title: "Fix Add", ChangeClass: protocol.ChangeLocal,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	taskID, err := svc.ResolveTaskID(ctx, testProject, "SH-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: testProject, Payload: &events.TaskDesignStarted{TaskID: taskID, Reason: "design"},
	}); err != nil {
		t.Fatal(err)
	}
	ps, err := svc.ProjectState(ctx, testProject)
	if err != nil {
		t.Fatal(err)
	}
	base := f.git.Head()
	wp := &protocol.EngineeringWorkPackage{
		SchemaVersion: protocol.SchemaVersion1, WorkPackageID: "wp-sh1", TaskID: taskID, Version: 1,
		ProjectID: testProject, ProjectStateRevision: ps.StateRevision, BaseCommit: base,
		ChangeClass: protocol.ChangeLocal,
		Objective:   "Fix Add in calc.go so it returns a+b, and add calc_test.go covering it.",
		Rationale:   "Add subtracts instead of adding.", ArchitecturalIntent: "Minimal fix.",
		Scope:                  protocol.Scope{InScope: []string{"calc.go", "calc_test.go"}, OutOfScope: []string{"go.mod"}},
		Guidance:               []protocol.Guidance{{ID: "G1", Strength: protocol.GuidanceMust, Statement: "Modify only in-scope files"}},
		AcceptanceCriteria:     []string{"Add(2,3) == 5"},
		ValidationRequirements: []string{"go test ./..."},
		EscalationConditions:   []string{"Uncertainty"},
	}
	digest, err := protocol.Digest(wp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: testProject,
		Payload: &events.WorkPackageApproved{
			TaskID: taskID, WorkPackageID: wp.WorkPackageID, WorkPackageVersion: 1, RecordDigest: digest,
			ProjectStateRevision: ps.StateRevision, BaseCommit: base, ChangeClass: protocol.ChangeLocal,
		},
		Records: []controlplane.RecordToStore{{Version: 1, Record: wp}},
	}); err != nil {
		t.Fatalf("approve wp: %v", err)
	}
	f.taskID, f.wp, f.wpDigest = taskID, wp, digest
}

func (f *fixture) authorizedTask() facade.AuthorizedTask {
	return facade.AuthorizedTask{
		Caller: principal.CallerContext{PrincipalID: "selfhost-test", ProjectID: testProject, SourceDepth: "all"},
		Meta:   principal.CallMeta{SchemaVersion: principal.SchemaVersion, ProjectID: testProject, CorrelationID: "corr-sh1"},
		TaskID: f.taskID,
		WorkPackage: principal.WorkPackageRef{
			ID: f.wp.WorkPackageID, Version: f.wp.Version, Digest: f.wpDigest, BaseCommit: f.wp.BaseCommit,
		},
	}
}

// delegate runs the real Executor.Delegate and waits for the operation.
func (f *fixture) delegate(t *testing.T, wait time.Duration) principal.OperationRef {
	t.Helper()
	ref, err := f.built.Executor.Delegate(context.Background(), f.authorizedTask())
	if err != nil {
		t.Fatalf("Delegate: %v", err)
	}
	return f.registry.Wait(context.Background(), ref, wait)
}

func (f *fixture) attempt(t *testing.T) *tasks.Attempt {
	t.Helper()
	d, err := f.harness.Service.TaskDetail(context.Background(), testProject, "SH-1")
	if err != nil {
		t.Fatalf("task detail: %v", err)
	}
	if len(d.Attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(d.Attempts))
	}
	return d.Attempts[0]
}

// candidateEvent returns the persisted CandidateProduced event.
func (f *fixture) candidateEvent(t *testing.T) *events.CandidateProduced {
	t.Helper()
	var evs []events.Event
	err := f.harness.Store.Read(context.Background(), func(tx *storage.Tx) error {
		var err error
		evs, err = tx.ReadEvents(context.Background(), storage.EventQuery{ProjectID: testProject})
		return err
	})
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	for _, ev := range evs {
		if c, ok := ev.Payload.(*events.CandidateProduced); ok {
			return c
		}
	}
	t.Fatal("no CandidateProduced event persisted")
	return nil
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}
