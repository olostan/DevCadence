package main

// STUB-BASED PLUMBING VALIDATION (SH1-4D). This is NOT evidence that any real
// model can do the work. It drives the real devcadence-mcp composition root and
// the real native executor (worktrees, apply_patch, run_command, validation
// profile, control plane) through the MCP TRANSPORT only, against a disposable
// git fixture and a scripted httptest Ollama-compatible server. It proves
// Antigravity-style Principal -> devcadence-mcp -> executor -> edit tools ->
// validation/repair -> candidate -> manual handoff wiring and its refusals.
// Real Ollama acceptance is deliberately deferred to the owner's machine.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/mcpadapter"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/principal/facade"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/storage"
	"github.com/olostan/DevCadence/internal/testsupport"
)

const (
	rhProject = "rehearsal"
	rhModel   = "stub-coder:7b"
	rhDigest  = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

const rhBuggyCalc = "package calc\n\n// Add returns the sum of a and b.\nfunc Add(a, b int) int { return a - b }\n"

const rhCalcTest = `package calc

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Fatalf("Add(2, 3) = %d, want 5", got)
	}
}
`

// gofmt -e only fails on syntax errors (gofmt -l exits 0 on unformatted input),
// so the profile pairs it with a targeted go test of the package under change.
const rhValidationYAML = `profiles:
  default:
    checks:
      - id: gofmt-check
        kind: lint
        argv: ["gofmt", "-e", "-l", "internal/calc"]
        timeout: 1m
      - id: go-test
        kind: test
        argv: ["go", "test", "./internal/calc/..."]
        timeout: 5m
`

type rhReply func() map[string]any

func rhTools(calls ...map[string]any) rhReply {
	return func() map[string]any {
		cs := make([]any, len(calls))
		for i, c := range calls {
			cs[i] = c
		}
		return map[string]any{"role": "assistant", "tool_calls": cs}
	}
}

func rhDone(text string) rhReply {
	return func() map[string]any { return map[string]any{"role": "assistant", "content": text} }
}

func rhCall(name string, args map[string]any) map[string]any {
	return map[string]any{"function": map[string]any{"name": name, "arguments": args}}
}

func rhPatch(path, oldText, newText string) map[string]any {
	return rhCall("apply_patch", map[string]any{"path": path, "edits": []map[string]string{{"old_text": oldText, "new_text": newText}}})
}

// scriptedOllama impersonates the Ollama endpoints DevCadence uses and replies
// to the n-th /api/chat with replies[n-1] (then "done"). Usage counts are
// omitted on purpose: usage stays unknown, never invented.
type scriptedOllama struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []map[string]any
}

func newScriptedOllama(t *testing.T, replies ...rhReply) *scriptedOllama {
	t.Helper()
	s := &scriptedOllama{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "0.0.0-stub"})
	})
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": rhModel, "model": rhModel, "digest": rhDigest}}})
	})
	mux.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		s.mu.Lock()
		s.reqs = append(s.reqs, req)
		n := len(s.reqs)
		s.mu.Unlock()
		reply := rhDone("done")
		if n <= len(replies) {
			reply = replies[n-1]
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"model": rhModel, "done": true, "message": reply()})
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

func (s *scriptedOllama) chatCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.reqs)
}

// lastUser returns the last user-role message content of the n-th request (1-based).
func (s *scriptedOllama) requestText(n int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, _ := json.Marshal(s.reqs[n-1]["messages"])
	return string(raw)
}

type rhOptions struct {
	selfhostConfig string // body of selfhost.json; "" = absent
	replies        []rhReply
}

type rehearsal struct {
	t      *testing.T
	repo   *testsupport.GitRepo
	base   string
	branch string
	home   string
	stub   *scriptedOllama
	cs     *mcp.ClientSession
	stderr *bytes.Buffer
	cancel context.CancelFunc
	done   chan int
	cp     *controlplane.Service
	rev    string
}

var allTools = []string{"project_state", "investigate", "create_work_package", "delegate", "task_status",
	"validate", "review", "request_evidence", "accept", "reject", "record_decision"}

func yoloConfigJSON(url string) string {
	return `{"model":"` + rhModel + `","ollama_url":"` + url + `","execution_mode":"yolo","max_repair_rounds":2}`
}

func strictConfigJSON(url string) string {
	return `{"model":"` + rhModel + `","ollama_url":"` + url + `"}`
}

func startRehearsal(t *testing.T, mkConfig func(url string) string, replies ...rhReply) *rehearsal {
	t.Helper()
	r := &rehearsal{t: t, stub: newScriptedOllama(t, replies...), stderr: &bytes.Buffer{}}

	// Disposable repository resembling DevCadence (internal/<pkg> layout).
	r.repo = testsupport.NewGitRepo(t)
	r.repo.WriteFile("go.mod", "module example.com/calc\n\ngo 1.21\n")
	r.repo.WriteFile("internal/calc/calc.go", rhBuggyCalc)
	r.repo.WriteFile("internal/other/other.go", "package other\n\n// Name is out of the work package's write scope.\nconst Name = \"other\"\n")
	r.repo.WriteFile(".devcadence/validation.yaml", rhValidationYAML)
	r.base = r.repo.Commit("add calc with a bug")
	r.branch = r.repo.Branch()

	// DEVCADENCE_HOME with a protected principal binding and, optionally, selfhost.json.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r.home = filepath.Join(root, "home")
	for _, dir := range []string{root, r.home, filepath.Join(r.home, "config"), filepath.Join(r.home, "state")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	actions, _ := json.Marshal(allTools)
	binding := `{"binding_version":"1.0","project_id":"` + rhProject + `","principal_id":"antigravity-principal","allowed_actions":` +
		string(actions) + `,"policy_ref":"policy-1","max_evidence_bytes":4096,"max_snippet_lines":100,"source_depth":"summary"}`
	writePrivate(t, filepath.Join(r.home, "config", "principal-binding.json"), binding)
	if mkConfig != nil {
		writePrivate(t, filepath.Join(r.home, "config", "selfhost.json"), mkConfig(r.stub.srv.URL))
	}

	// Owner-side project setup (what `devcadence project init` / `task create` do).
	ctx := context.Background()
	store, err := storage.Open(ctx, storage.Config{Path: filepath.Join(r.home, "state", "control-plane.db"), Clock: clock.System()})
	if err != nil {
		t.Fatal(err)
	}
	r.cp, err = controlplane.New(controlplane.Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := r.cp.InitProject(ctx, controlplane.InitProjectInput{
		ProjectID: rhProject, Name: "Rehearsal", MilestoneID: "M1", MilestoneTitle: "Milestone 1",
		RepositoryPath: r.repo.Path, AcceptedCommit: r.base,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.cp.CreateTask(ctx, controlplane.CreateTaskInput{
		ProjectID: rhProject, Alias: "RH-1", Title: "Fix Add", ChangeClass: protocol.ChangeLocal,
	}); err != nil {
		t.Fatal(err)
	}
	taskID, err := r.cp.ResolveTaskID(ctx, rhProject, "RH-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.cp.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: rhProject, Payload: &events.TaskDesignStarted{TaskID: taskID, Reason: "design"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	// The server below opens its own handle; reopen a second one for owner-side approval later.
	store2, err := storage.Open(ctx, storage.Config{Path: filepath.Join(r.home, "state", "control-plane.db"), Clock: clock.System()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store2.Close() })
	r.cp, err = controlplane.New(controlplane.Options{Store: store2})
	if err != nil {
		t.Fatal(err)
	}

	// Launch devcadence-mcp exactly as the binary does (same composition root),
	// over an in-memory MCP transport.
	env := map[string]string{mcpadapter.EnvProjectID: rhProject, mcpadapter.EnvHome: r.home}
	lctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	r.cancel = cancel
	r.done = make(chan int, 1)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	go func() {
		r.done <- mcpadapter.LaunchWith(lctx, nil, func(k string) string { return env[k] }, serverTransport, r.stderr, selfhostTasks)
	}()
	client := mcp.NewClient(&mcp.Implementation{Name: "antigravity-stand-in", Version: "1"}, nil)
	r.cs, err = client.Connect(lctx, clientTransport, nil)
	if err != nil {
		cancel()
		t.Fatalf("connect: %v (stderr %s)", err, r.stderr.String())
	}
	t.Cleanup(func() {
		cancel()
		_ = r.cs.Close()
		select {
		case <-r.done:
		case <-time.After(30 * time.Second):
		}
	})
	return r
}

func writePrivate(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (r *rehearsal) meta(rev string) principal.CallMeta {
	return principal.CallMeta{SchemaVersion: principal.SchemaVersion, ProjectID: rhProject, ExpectedStateRevision: rev, CorrelationID: "rehearsal"}
}

// call is the ONLY way the rehearsal touches DevCadence after launch: an MCP tool call.
func (r *rehearsal) call(tool string, req any, out any) *mcp.CallToolResult {
	r.t.Helper()
	raw, err := json.Marshal(req)
	if err != nil {
		r.t.Fatal(err)
	}
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		r.t.Fatal(err)
	}
	res, err := r.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		r.t.Fatalf("%s: %v", tool, err)
	}
	if out != nil {
		b, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(b, out); err != nil {
			r.t.Fatalf("%s: decode %s: %v", tool, b, err)
		}
	}
	return res
}

func (r *rehearsal) projectState(focus, taskID string) facade.ProjectStateResponse {
	r.t.Helper()
	var resp facade.ProjectStateResponse
	if res := r.call("project_state", facade.ProjectStateRequest{Meta: r.meta(""), Focus: focus, TaskID: taskID}, &resp); res.IsError {
		r.t.Fatalf("project_state: %+v", resp.Error)
	}
	r.rev = resp.StateRevision
	return resp
}

// poll follows an operation handle with task_status until it leaves "running".
func (r *rehearsal) poll(op principal.OperationRef) *facade.OperationStatus {
	r.t.Helper()
	deadline := time.Now().Add(5 * time.Minute)
	polls := 0
	for time.Now().Before(deadline) {
		var resp facade.TaskStatusResponse
		r.call("task_status", facade.TaskStatusRequest{Meta: r.meta(""), Operation: &op}, &resp)
		if resp.Result == nil || resp.Result.Operation == nil {
			r.t.Fatalf("task_status(operation): %+v", resp.Error)
		}
		polls++
		if st := resp.Result.Operation; st.Operation.Status != principal.StatusRunning {
			return st
		}
		time.Sleep(100 * time.Millisecond)
	}
	r.t.Fatalf("operation %s still running after 5 minutes", op.ID)
	return nil
}

func (r *rehearsal) workPackage(taskID string) protocol.EngineeringWorkPackage {
	return protocol.EngineeringWorkPackage{
		SchemaVersion: protocol.SchemaVersion1, WorkPackageID: "wp-rh1", TaskID: taskID, Version: 1,
		ProjectID: rhProject, ProjectStateRevision: r.rev, BaseCommit: r.base, ChangeClass: protocol.ChangeLocal,
		Objective:           "Fix Add in internal/calc/calc.go so it returns a+b, and add internal/calc/calc_test.go covering it.",
		Rationale:           "Add subtracts instead of adding.",
		ArchitecturalIntent: "Minimal fix inside the calc package.",
		Scope:               protocol.Scope{InScope: []string{"internal/calc/..."}, OutOfScope: []string{"go.mod", "internal/other/..."}},
		Guidance:            []protocol.Guidance{{ID: "G1", Strength: protocol.GuidanceMust, Statement: "Modify only files under internal/calc"}},
		AcceptanceCriteria:  []string{"Add(2,3) == 5"}, ValidationRequirements: []string{"go test ./internal/calc/..."},
		EscalationConditions: []string{"Uncertainty"},
	}
}

// approve is the OWNER action between proposal and delegation (there is no MCP
// approval tool): it is `devcadence event append -type WorkPackageApproved`.
func (r *rehearsal) approve(ref principal.WorkPackageRef, wp protocol.EngineeringWorkPackage) {
	r.t.Helper()
	if _, err := r.cp.AppendTypedEvent(context.Background(), controlplane.AppendTypedEventInput{
		ProjectID: rhProject,
		Payload: &events.WorkPackageApproved{
			TaskID: wp.TaskID, WorkPackageID: ref.ID, WorkPackageVersion: ref.Version, RecordDigest: ref.Digest,
			ProjectStateRevision: wp.ProjectStateRevision, BaseCommit: wp.BaseCommit, ChangeClass: wp.ChangeClass,
		},
	}); err != nil {
		r.t.Fatalf("owner approval: %v", err)
	}
}

// proposeAndApprove runs project_state -> create_work_package -> owner approval and
// returns the task id and the approved work package reference.
func (r *rehearsal) proposeAndApprove() (string, principal.WorkPackageRef) {
	r.t.Helper()
	r.projectState("", "")
	task := r.projectState(facade.FocusTask, "RH-1")
	if task.Result == nil || task.Result.Task == nil || task.Result.Task.State != "designing" {
		r.t.Fatalf("task state: %+v", task.Result)
	}
	taskID := task.Result.Task.TaskID
	wp := r.workPackage(taskID)
	var created facade.CreateWorkPackageResponse
	if res := r.call("create_work_package", facade.CreateWorkPackageRequest{Meta: r.meta(r.rev), WorkPackage: wp}, &created); res.IsError {
		r.t.Fatalf("create_work_package: %+v", created.Error)
	}
	if created.Result.Status != "proposed" {
		r.t.Fatalf("status %q", created.Result.Status)
	}
	ref := created.Result.WorkPackage
	r.approve(ref, wp)
	r.projectState("", "")
	return taskID, ref
}

func (r *rehearsal) delegate(taskID string, ref principal.WorkPackageRef) (facade.DelegateResponse, *mcp.CallToolResult) {
	r.t.Helper()
	var resp facade.DelegateResponse
	res := r.call("delegate", facade.DelegateRequest{Meta: r.meta(r.rev), TaskID: taskID, WorkPackage: ref}, &resp)
	return resp, res
}

func (r *rehearsal) git(dir string, args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *rehearsal) shell(command string) string {
	r.t.Helper()
	out, err := exec.Command("sh", "-c", command).CombinedOutput()
	if err != nil {
		r.t.Fatalf("%s: %v\n%s", command, err, out)
	}
	return string(out)
}

func (r *rehearsal) requireError(res *mcp.CallToolResult, code string, refs ...string) {
	r.t.Helper()
	if !res.IsError {
		r.t.Fatalf("expected %s, got success", code)
	}
	var env facade.Envelope
	b, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(b, &env); err != nil || env.Error == nil {
		r.t.Fatalf("not an error envelope: %s", b)
	}
	if env.Error.Code != code {
		r.t.Fatalf("code = %s, want %s (%s)", env.Error.Code, code, b)
	}
	for _, want := range refs {
		found := false
		for _, got := range env.EvidenceRefs {
			found = found || got == want
		}
		if !found {
			r.t.Fatalf("evidence_refs %v lack %q", env.EvidenceRefs, want)
		}
	}
}

func TestRehearsal_StubMCPDelegationRepairAndHandoff(t *testing.T) {
	r := startRehearsal(t, yoloConfigJSON, // yolo is explicit user-level config, never repo config
		// request 1: a WRONG fix (a*b) plus the test, written with DevCadence's own edit tools
		rhTools(rhPatch("internal/calc/calc.go", "return a - b", "return a * b"),
			rhCall("write_file", map[string]any{"path": "internal/calc/calc_test.go", "content": rhCalcTest})),
		// request 2: the model claims it is done -> post-check go test fails -> feedback
		rhDone("Implemented Add."),
		// request 3 carries the validation feedback: repair and run vet via run_command
		rhTools(rhPatch("internal/calc/calc.go", "return a * b", "return a + b"),
			rhCall("run_command", map[string]any{"argv": []string{"go", "vet", "./internal/calc/..."}})),
		// request 4: done -> post-check passes -> candidate
		rhDone("Fixed Add to return a+b."),
	)

	// 1. The advertised MCP tool surface.
	listed, err := r.cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range listed.Tools {
		names = append(names, tl.Name)
	}
	sort.Strings(names)
	want := append([]string(nil), allTools...)
	sort.Strings(want)
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("tools = %v", names)
	}

	// 2. project_state -> create_work_package -> (owner approval) -> project_state.
	taskID, wpRef := r.proposeAndApprove()

	// 3. delegate reaches the REAL runtime and returns an operation handle.
	dele, res := r.delegate(taskID, wpRef)
	if res.IsError || dele.Result == nil || dele.Result.Operation.Kind != principal.KindDelegate {
		t.Fatalf("delegate: %+v %+v", dele.Error, res)
	}
	st := r.poll(dele.Result.Operation)
	if st.Operation.Status != principal.StatusCompleted || !strings.HasPrefix(st.ResultHandle, "candidate:") {
		t.Fatalf("delegate operation: %+v (stderr %s)", st, r.stderr.String())
	}
	if n := r.stub.chatCalls(); n != 4 {
		t.Fatalf("chat calls = %d, want 4", n)
	}

	// The primary repository was not touched by anything but a ref: HEAD, branch and tree.
	if r.repo.Head() != r.base || r.repo.Branch() != r.branch || r.git(r.repo.Path, "status", "--porcelain") != "" {
		t.Fatal("the primary repository HEAD, branch or working tree moved")
	}
	// The validation feedback reached the model on request 3 (concise, from the failing check).
	if fb := r.stub.requestText(3); !strings.Contains(fb, "validation FAILED") || !strings.Contains(fb, "go-test") {
		t.Errorf("request 3 lacks validation feedback: %s", fb)
	}

	// 4. task_status(task): candidate + handoff packet.
	var ts facade.TaskStatusResponse
	r.call("task_status", facade.TaskStatusRequest{Meta: r.meta(""), TaskID: taskID}, &ts)
	if ts.Result == nil || ts.Result.Task == nil || ts.Result.Task.Candidate == nil || ts.Result.CandidateHandoff == nil {
		t.Fatalf("task_status: %+v", ts)
	}
	cand := *ts.Result.Task.Candidate
	h := ts.Result.CandidateHandoff
	if ts.Result.Task.State != "validating" {
		t.Errorf("task state = %s", ts.Result.Task.State)
	}
	fullSHA := regexp.MustCompile(`^[0-9a-f]{40}$`)
	if h.BaseCommit != r.base || h.CandidateCommit != cand.Commit || !fullSHA.MatchString(h.CandidateCommit) || h.CandidateCommit == h.BaseCommit {
		t.Fatalf("commits: %+v", h)
	}
	refName := "refs/devcadence/candidates/" + h.TaskID + "-" + h.AttemptID
	if h.Ref != refName {
		t.Fatalf("ref = %s", h.Ref)
	}
	if got := r.git(r.repo.Path, "rev-parse", refName); got != h.CandidateCommit {
		t.Fatalf("ref in the primary repository names %s, want %s", got, h.CandidateCommit)
	}
	if r.git(r.repo.Path, "rev-parse", "HEAD") != r.base {
		t.Fatal("HEAD moved")
	}
	// Edits were made by DevCadence's tools in the attempt worktree, not by this test.
	if h.WorktreePath == "" || h.Branch != "devcadence/"+h.TaskID+"/"+h.AttemptID {
		t.Errorf("worktree/branch: %q %q", h.WorktreePath, h.Branch)
	}
	if got := r.git(r.repo.Path, "show", h.CandidateCommit+":internal/calc/calc.go"); !strings.Contains(got, "return a + b") {
		t.Errorf("candidate calc.go:\n%s", got)
	}
	files := map[string]string{}
	for _, f := range h.ChangedFiles {
		files[f.Path] = f.Status
	}
	if len(files) != 2 || files["internal/calc/calc.go"] != "M" || files["internal/calc/calc_test.go"] != "A" {
		t.Errorf("manifest = %v", h.ChangedFiles)
	}
	if h.Model.EndpointID != "ollama-local" || h.Model.Model != rhModel || h.Model.Digest != rhDigest {
		t.Errorf("model = %+v", h.Model)
	}
	if h.ExecutionMode != "unsafe_unconfined_local" {
		t.Errorf("mode = %q", h.ExecutionMode)
	}
	if h.Review.Status != "review_unavailable" || h.Review.Independent {
		t.Errorf("review = %+v", h.Review)
	}
	pc := h.PostCheck
	if pc == nil || !pc.Passed || pc.RoundsUsed != 2 || pc.RepairRounds != 1 || len(pc.Rounds) != 2 {
		t.Fatalf("post-check = %+v", pc)
	}
	if pc.Rounds[0].Passed || !pc.Rounds[1].Passed {
		t.Errorf("repair history = %+v", pc.Rounds)
	}
	statusOf := func(round facade.HandoffRound, id string) string {
		for _, c := range round.Checks {
			if c.ID == id {
				return c.Status
			}
		}
		return "<missing>"
	}
	if statusOf(pc.Rounds[0], "gofmt-check") != "pass" || statusOf(pc.Rounds[0], "go-test") == "pass" || statusOf(pc.Rounds[1], "go-test") != "pass" {
		t.Errorf("round checks: %+v", pc.Rounds)
	}
	if h.CommandTrace == nil || h.CommandTrace.Commands != 1 || h.CommandTrace.Refused != 0 || !h.CommandTrace.UnsafeUnconfined {
		t.Errorf("command trace = %+v", h.CommandTrace)
	}
	if !strings.Contains(h.Acceptance, "manual owner action") {
		t.Errorf("acceptance note = %q", h.Acceptance)
	}

	// The printed inspection commands really work against the primary repository.
	diff := r.shell(h.Inspect.Diff)
	if !strings.Contains(diff, "+func Add(a, b int) int { return a + b }") || !strings.Contains(diff, "internal/calc/calc_test.go") {
		t.Errorf("printed git diff does not show the change:\n%s", diff)
	}
	if lg := r.shell(h.Inspect.Log); !strings.Contains(lg, h.CandidateCommit) || !strings.Contains(lg, "internal/calc/calc.go") {
		t.Errorf("printed git log:\n%s", lg)
	}
	// ...and the printed cherry-pick works (on a throwaway clone, with the command's
	// repo path swapped for the clone) without touching the primary repository.
	clone := filepath.Join(t.TempDir(), "clone")
	r.git(filepath.Dir(clone), "clone", "-q", r.repo.Path, clone)
	r.git(clone, "fetch", "-q", r.repo.Path, refName+":"+refName)
	r.git(clone, "config", "user.email", "owner@example.invalid")
	r.git(clone, "config", "user.name", "owner")
	pick := strings.Replace(h.Inspect.CherryPick, "'"+r.repo.Path+"'", "'"+clone+"'", 1)
	if pick == h.Inspect.CherryPick {
		t.Fatalf("cherry-pick command does not name the repository: %s", pick)
	}
	r.shell(pick)
	if got := r.git(clone, "show", "HEAD:internal/calc/calc.go"); !strings.Contains(got, "return a + b") {
		t.Errorf("cherry-picked clone:\n%s", got)
	}

	// 5. validate (independent, deterministic, in a clean worktree) through MCP.
	r.projectState("", "")
	var val facade.ValidateResponse
	if res := r.call("validate", facade.ValidateRequest{Meta: r.meta(r.rev), Candidate: cand, ProfileID: "default"}, &val); res.IsError {
		t.Fatalf("validate: %+v", val.Error)
	}
	vst := r.poll(val.Result.Operation)
	if vst.Operation.Status != principal.StatusCompleted || vst.ResultHandle == "" {
		t.Fatalf("validate operation: %+v (stderr %s)", vst, r.stderr.String())
	}

	// 6. review is honestly unavailable (reviewexec is not wired).
	r.projectState("", "")
	res = r.call("review", facade.ReviewRequest{Meta: r.meta(r.rev), Candidate: cand, Dimensions: []string{"correctness"}}, nil)
	r.requireError(res, principal.CodeModelUnavailable, facade.MissingRuntimeRef("review"))

	// 7. inspect again: the independent validation is now part of the handoff.
	r.call("task_status", facade.TaskStatusRequest{Meta: r.meta(""), TaskID: taskID}, &ts)
	if h2 := ts.Result.CandidateHandoff; h2 == nil || len(h2.Validations) != 1 || h2.Validations[0].ValidationID != vst.ResultHandle || h2.Validations[0].Outcome != "pass" {
		t.Fatalf("validations in handoff: %+v", ts.Result.CandidateHandoff)
	}

	// 8. accept: refused (manual owner action) but carries the handoff packet.
	var acc facade.AcceptResponse
	res = r.call("accept", facade.AcceptRequest{
		Meta: r.meta(r.rev), Candidate: cand, ValidationIDs: []string{vst.ResultHandle},
		ReviewIDs: []string{"review_unavailable"}, Reason: "owner will integrate manually",
	}, &acc)
	r.requireError(res, principal.CodeNeedsPrincipal, facade.AcceptanceUnavailableRef)
	if acc.Result == nil || acc.Result.Handoff == nil {
		t.Fatalf("accept carries no handoff: %+v", acc)
	}
	ah := acc.Result.Handoff
	if ah.CandidateCommit != h.CandidateCommit || ah.Ref != refName || ah.Review.Status != "review_unavailable" || ah.Review.Independent ||
		!strings.Contains(ah.Acceptance, "manual owner action") || !strings.Contains(ah.Acceptance, "review_unavailable") {
		t.Errorf("accept handoff = %+v", ah)
	}
	// Nothing was accepted or merged by DevCadence.
	d, err := r.cp.TaskDetail(context.Background(), rhProject, "RH-1")
	if err != nil {
		t.Fatal(err)
	}
	if d.Task.AcceptedCommit != "" || d.Task.State != "reviewing" {
		t.Errorf("task = %s accepted=%q", d.Task.State, d.Task.AcceptedCommit)
	}
	if r.repo.Head() != r.base || r.repo.Branch() != r.branch || r.git(r.repo.Path, "status", "--porcelain") != "" {
		t.Fatal("the primary repository moved during validate/review/accept")
	}
	// The branch DevCadence itself created for the worktree is the only branch beyond the original.
	for _, b := range strings.Split(r.git(r.repo.Path, "for-each-ref", "--format=%(refname)", "refs/heads"), "\n") {
		if b != "refs/heads/"+r.branch && !strings.HasPrefix(b, "refs/heads/devcadence/") {
			t.Errorf("unexpected branch %s", b)
		}
	}
}

func TestRehearsal_StrictModeRefusesBeforeAnySubprocess(t *testing.T) {
	r := startRehearsal(t, strictConfigJSON, rhDone("must never be asked"))
	taskID, wpRef := r.proposeAndApprove()
	dele, res := r.delegate(taskID, wpRef)
	// The repository defines .devcadence/validation.yaml whose commands run unconfined,
	// so strict mode refuses synchronously and names the remedy as an evidence handle.
	r.requireError(res, principal.CodePolicyDenied, "postcheck_requires_yolo")
	if dele.Result != nil {
		t.Fatalf("delegate returned an operation: %+v", dele.Result)
	}
	if n := r.stub.chatCalls(); n != 0 {
		t.Fatalf("model was called %d times before the refusal", n)
	}
	if r.git(r.repo.Path, "for-each-ref", "refs/devcadence", "refs/heads/devcadence") != "" {
		t.Error("a ref or worktree branch was created by a refused delegation")
	}
	var ts facade.TaskStatusResponse
	r.call("task_status", facade.TaskStatusRequest{Meta: r.meta(""), TaskID: taskID}, &ts)
	if ts.Result.Task.State != "ready" || len(ts.Result.Task.AttemptIDs) != 0 || ts.Result.CandidateHandoff != nil {
		t.Errorf("task after refusal: %+v", ts.Result.Task)
	}
}

func TestRehearsal_ScopeViolationYieldsNoCandidate(t *testing.T) {
	r := startRehearsal(t, yoloConfigJSON,
		// The model tries to write outside the work package's write scope.
		rhTools(rhCall("write_file", map[string]any{"path": "internal/other/evil.go", "content": "package other\n"})),
		rhDone("done"),
	)
	taskID, wpRef := r.proposeAndApprove()
	dele, res := r.delegate(taskID, wpRef)
	if res.IsError {
		t.Fatalf("delegate: %+v", dele.Error)
	}
	st := r.poll(dele.Result.Operation)
	if st.Operation.Status != principal.StatusFailed {
		t.Fatalf("operation = %+v", st)
	}
	if !strings.Contains(r.stub.requestText(2), "outside declared write scope") {
		t.Errorf("the scope refusal did not reach the model: %s", r.stub.requestText(2))
	}
	if _, err := os.Stat(filepath.Join(r.repo.Path, "internal/other/evil.go")); err == nil {
		t.Error("an out-of-scope file reached the primary repository")
	}
	if r.git(r.repo.Path, "for-each-ref", "refs/devcadence") != "" {
		t.Error("a candidate ref exists for a failed attempt")
	}
	var ts facade.TaskStatusResponse
	r.call("task_status", facade.TaskStatusRequest{Meta: r.meta(""), TaskID: taskID}, &ts)
	if ts.Result.Task.Candidate != nil || ts.Result.CandidateHandoff != nil {
		t.Errorf("failed attempt exposes a candidate: %+v", ts.Result)
	}
	if r.repo.Head() != r.base {
		t.Fatal("HEAD moved")
	}
}

func TestRehearsal_AbsentSelfhostConfigRefusesDelegate(t *testing.T) {
	r := startRehearsal(t, nil)
	taskID, wpRef := r.proposeAndApprove()
	_, res := r.delegate(taskID, wpRef)
	r.requireError(res, principal.CodeModelUnavailable, facade.MissingRuntimeRef("task execution"))
	if !strings.Contains(r.stderr.String(), "selfhost.json") {
		t.Errorf("launch log gives no remedy: %s", r.stderr.String())
	}
	// validate refuses the same way; project_state and create_work_package still work.
	cand := principal.CandidateRef{TaskID: taskID, AttemptID: "att-x", WorkPackage: wpRef, Commit: r.base}
	res = r.call("validate", facade.ValidateRequest{Meta: r.meta(r.rev), Candidate: cand, ProfileID: "default"}, nil)
	r.requireError(res, principal.CodeModelUnavailable, facade.MissingRuntimeRef("validation"))
	if n := r.stub.chatCalls(); n != 0 {
		t.Fatalf("model called %d times", n)
	}
}
