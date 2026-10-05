package mcpadapter_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/mcpadapter"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/principal/facade"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/schema"
	"github.com/olostan/DevCadence/internal/testsupport"
)

type countingPolicy struct{ calls atomic.Int32 }

func (c *countingPolicy) Check(context.Context, principal.CallerContext, principal.CallMeta, string) error {
	c.calls.Add(1)
	return nil
}

// executor is a spy for every runtime port; it never executes anything.
type executor struct {
	calls atomic.Int32
	err   error
	inst  string
}

func (e *executor) op(kind string) principal.OperationRef {
	return principal.OperationRef{ID: "op_1", InstanceID: e.inst, Kind: kind, Status: principal.StatusRunning}
}
func (e *executor) Delegate(context.Context, facade.AuthorizedTask) (principal.OperationRef, error) {
	e.calls.Add(1)
	return e.op("delegate"), e.err
}
func (e *executor) Validate(context.Context, principal.CallerContext, principal.CallMeta, principal.CandidateRef, string) (principal.OperationRef, error) {
	e.calls.Add(1)
	return e.op("validate"), e.err
}
func (e *executor) Review(context.Context, principal.CallerContext, principal.CallMeta, principal.CandidateRef, []string) (principal.OperationRef, error) {
	e.calls.Add(1)
	return e.op("review"), e.err
}
func (e *executor) Request(context.Context, principal.CallerContext, facade.SnippetRequest) (principal.OperationRef, error) {
	e.calls.Add(1)
	return e.op("snippet"), e.err
}
func (e *executor) Diff(context.Context, principal.CallerContext, facade.DiffRequest) (principal.OperationRef, error) {
	e.calls.Add(1)
	return e.op("snippet"), e.err
}

type serverRig struct {
	cs     *mcp.ClientSession
	svc    *facade.Service
	caller principal.CallerContext
	policy *countingPolicy
	exec   *executor
	ops    *facade.OperationRegistry
	rev    string
	taskID string
}

func newServerRig(t *testing.T, withExecutor bool) *serverRig {
	t.Helper()
	h := testsupport.NewHarness(t)
	ctx := context.Background()
	if _, err := h.Service.InitProject(ctx, controlplane.InitProjectInput{ProjectID: "example", MilestoneID: "M1", MilestoneTitle: "Domain core"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Service.CreateTask(ctx, controlplane.CreateTaskInput{
		ProjectID: "example", Alias: "DC-001", Title: "Bounded reads", ChangeClass: protocol.ChangeSystemic,
	}); err != nil {
		t.Fatal(err)
	}
	taskID, _ := h.Service.ResolveTaskID(ctx, "example", "DC-001")
	ops, err := facade.NewOperationRegistry("inst_server_test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ops.Close)
	policy := &countingPolicy{}
	exec := &executor{inst: ops.InstanceID()}
	opts := facade.Options{ControlPlane: h.Service, Policy: policy, Operations: ops}
	if withExecutor {
		opts.Tasks, opts.Reviews, opts.Snippets = exec, exec, exec
	}
	svc, err := facade.NewService(opts)
	if err != nil {
		t.Fatal(err)
	}
	caller := principal.CallerContext{
		PrincipalID: "principal-1", ProjectID: "example", AllowedActions: facade.ToolNames(),
		PolicyRef: "policy-1", MaxEvidenceBytes: 8192, MaxSnippetLines: 200, SourceDepth: facade.DepthSnippet,
	}
	server, err := mcpadapter.NewServer(mcpadapter.Config{Service: svc, Caller: caller, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = cs.Close(); _ = ss.Close() })
	st, _ := h.Service.ProjectState(ctx, "example")
	return &serverRig{cs: cs, svc: svc, caller: caller, policy: policy, exec: exec, ops: ops, rev: st.StateRevision, taskID: taskID}
}

func (r *serverRig) call(t *testing.T, tool string, args any) *mcp.CallToolResult {
	t.Helper()
	res, err := r.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error %v", tool, err)
	}
	return res
}

func (r *serverRig) meta() map[string]any {
	return map[string]any{"schema_version": "1.0", "project_id": "example", "correlation_id": "corr-1"}
}

// sameJSON compares two documents by value, ignoring member order.
func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(x, y)
}

func structured(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("structured content is not an object: %v (%s)", err, raw)
	}
	if len(res.Content) != 1 {
		t.Fatalf("%d content blocks", len(res.Content))
	}
	text := res.Content[0].(*mcp.TextContent).Text
	var viaText map[string]any
	if err := json.Unmarshal([]byte(text), &viaText); err != nil {
		t.Fatalf("the text result is not valid JSON: %v", err)
	}
	a, _ := json.Marshal(out)
	b, _ := json.Marshal(viaText)
	if string(a) != string(b) {
		t.Fatalf("text and structured content differ:\n%s\n%s", a, b)
	}
	return out
}

func errorCode(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if !res.IsError {
		t.Fatalf("expected isError, got %v", structured(t, res))
	}
	e, _ := structured(t, res)["error"].(map[string]any)
	if e == nil {
		t.Fatal("no error object")
	}
	raw, _ := json.Marshal(e)
	se, err := principal.DecodeSemanticError(raw)
	if err != nil {
		t.Fatalf("the error is not a valid SemanticError: %v", err)
	}
	return se.Code
}

// A5/R6: exactly the eleven base tools; no raw backdoor primitive exists.
func TestA5_ClosedToolAllowlistAndNoBackdoorPrimitives(t *testing.T) {
	r := newServerRig(t, false)
	ctx := context.Background()
	list, err := r.cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tool := range list.Tools {
		got = append(got, tool.Name)
		schemaMap, ok := tool.InputSchema.(map[string]any)
		if !ok || schemaMap["type"] != "object" {
			t.Errorf("%s: input schema is not an object schema", tool.Name)
		}
		raw, _ := json.Marshal(tool.InputSchema)
		if strings.Contains(string(raw), "devcadence:///") || strings.Contains(string(raw), "$ref\":\"principal") {
			t.Errorf("%s: schema holds an unresolved cross-schema reference", tool.Name)
		}
		if tool.Description == "" {
			t.Errorf("%s: no description", tool.Name)
		}
	}
	want := facade.ToolNames()
	if len(got) != len(want) {
		t.Fatalf("tools %v, want %v", got, want)
	}
	have := map[string]bool{}
	for _, n := range got {
		have[n] = true
	}
	for _, n := range want {
		if !have[n] {
			t.Errorf("tool %s is missing", n)
		}
	}
	for _, raw := range []string{"read_file", "run_shell", "sql", "fetch", "list_directory", "grep", "pager", "initialize_project", "discovery_state", "record_requirements"} {
		if have[raw] {
			t.Errorf("raw or unlisted tool %s is registered", raw)
		}
		if res, err := r.cs.CallTool(ctx, &mcp.CallToolParams{Name: raw, Arguments: map[string]any{}}); err == nil && !res.IsError {
			t.Errorf("calling %s succeeded", raw)
		}
	}
	if _, err := r.cs.ListResources(ctx, nil); err == nil {
		t.Error("resources are listable")
	}
	if _, err := r.cs.ListPrompts(ctx, nil); err == nil {
		t.Error("prompts are listable")
	}
	if _, err := r.cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "file:///etc/passwd"}); err == nil {
		t.Error("a resource was readable")
	}
	caps := r.cs.InitializeResult().Capabilities
	if caps == nil || caps.Tools == nil || caps.Resources != nil || caps.Prompts != nil || caps.Logging != nil || caps.Completions != nil {
		t.Fatalf("unexpected capabilities %+v", caps)
	}
	if r.cs.InitializeResult().ProtocolVersion == "" {
		t.Fatal("no negotiated protocol version")
	}
	for _, tool := range list.Tools {
		if tool.Name == "accept" && !strings.Contains(tool.Description, "DISABLED") {
			t.Error("accept does not disclose that it is disabled")
		}
		if tool.Name == "delegate" && !strings.Contains(tool.Description, "MODEL_UNAVAILABLE") {
			t.Error("delegate does not disclose that its runtime may be unavailable")
		}
	}
}

// A14/R1: the same call through Go and MCP gives the same result.
func TestA14_GoAndMCPResultsAreIdentical(t *testing.T) {
	r := newServerRig(t, false)
	req := facade.ProjectStateRequest{Meta: principal.CallMeta{SchemaVersion: "1.0", ProjectID: "example", CorrelationID: "corr-1"}}
	direct, err := r.svc.ProjectState(context.Background(), r.caller, req)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(direct)
	res := r.call(t, "project_state", map[string]any{"meta": r.meta()})
	if res.IsError {
		t.Fatalf("error: %v", structured(t, res))
	}
	structured(t, res)
	got, _ := json.Marshal(res.StructuredContent)
	if !sameJSON(t, got, want) {
		t.Fatalf("MCP and Go differ:\n%s\n%s", got, want)
	}
	set, _ := schema.Default()
	if err := set.ValidateBytes("principal-project-state-response", got); err != nil {
		t.Fatalf("the MCP result violates its published schema: %v", err)
	}

	// A refusal is identical too.
	bad := facade.ProjectStateRequest{Meta: req.Meta, Focus: "task", TaskID: "ghost"}
	directBad, _ := r.svc.ProjectState(context.Background(), r.caller, bad)
	wantBad, _ := json.Marshal(directBad)
	resBad := r.call(t, "project_state", map[string]any{"meta": r.meta(), "focus": "task", "task_id": "ghost"})
	gotBad, _ := json.Marshal(resBad.StructuredContent)
	if !sameJSON(t, gotBad, wantBad) || errorCode(t, resBad) != principal.CodeNotFound {
		t.Fatalf("refusals differ:\n%s\n%s", gotBad, wantBad)
	}
}

// A2: a request can never mint authority and a refusal makes zero callbacks.
func TestA2_RequestsCannotMintPermission(t *testing.T) {
	r := newServerRig(t, true)
	cand := map[string]any{"task_id": r.taskID, "attempt_id": "a", "commit": "abcdef0", "work_package": map[string]any{
		"id": "wp", "version": 1, "digest": "sha256:" + strings.Repeat("a", 64), "base_commit": "abcdef0"}}
	other := r.meta()
	other["project_id"] = "elsewhere"
	forgedMeta := r.meta()
	forgedMeta["allowed_actions"] = []string{"accept", "delegate"}
	actorMeta := r.meta()
	actorMeta["actor"] = map[string]any{"kind": "human", "id": "operator"}
	policyMeta := r.meta()
	policyMeta["policy_ref"] = "weaker"
	before := r.policy.calls.Load()
	for name, args := range map[string]map[string]any{
		"wrong project":      {"meta": other, "candidate": cand, "profile_id": "p"},
		"grants in meta":     {"meta": forgedMeta, "candidate": cand, "profile_id": "p"},
		"actor in meta":      {"meta": actorMeta, "candidate": cand, "profile_id": "p"},
		"policy in meta":     {"meta": policyMeta, "candidate": cand, "profile_id": "p"},
		"unknown top key":    {"meta": r.meta(), "candidate": cand, "profile_id": "p", "caller": "root"},
		"top-level grants":   {"meta": r.meta(), "candidate": cand, "profile_id": "p", "allowed_actions": []string{"validate"}},
		"missing candidate":  {"meta": r.meta(), "profile_id": "p"},
		"null where omitted": {"meta": r.meta(), "candidate": cand, "profile_id": nil},
	} {
		res := r.call(t, "validate", args)
		code := errorCode(t, res)
		if code != principal.CodeInvalidArgument && code != principal.CodePolicyDenied {
			t.Errorf("%s: code %s", name, code)
		}
	}
	if r.exec.calls.Load() != 0 {
		t.Fatalf("%d executor callbacks for refused requests", r.exec.calls.Load())
	}
	// Decode failures never even reach the policy resolver; only a bound,
	// well-formed request for another project does, and it is still denied.
	if got := r.policy.calls.Load() - before; got != 0 {
		t.Fatalf("%d policy callbacks for refused requests", got)
	}
}

func TestA13_RawProviderErrorsNeverReachTheClient(t *testing.T) {
	r := newServerRig(t, true)
	r.exec.err = errors.New("provider failure sk-live-SECRET at /home/user/.ssh/id_rsa: SELECT * FROM events")
	cand := map[string]any{"task_id": r.taskID, "attempt_id": "a", "commit": "abcdef0", "work_package": map[string]any{
		"id": "wp", "version": 1, "digest": "sha256:" + strings.Repeat("a", 64), "base_commit": "abcdef0"}}
	for _, call := range []struct {
		tool string
		args map[string]any
	}{
		{"validate", map[string]any{"meta": r.meta(), "candidate": cand, "profile_id": "p"}},
		{"review", map[string]any{"meta": r.meta(), "candidate": cand, "dimensions": []string{"security"}}},
		{"request_evidence", map[string]any{"meta": r.meta(), "kind": "snippet", "base_commit": "abcdef0", "path": "a.go",
			"start_line": 1, "end_line": 2, "reason": "r", "max_bytes": 100}},
	} {
		res := r.call(t, call.tool, call.args)
		raw, _ := json.Marshal(res)
		for _, leak := range []string{"SECRET", "ssh", "SELECT", "provider failure"} {
			if strings.Contains(string(raw), leak) {
				t.Fatalf("%s leaked %q: %s", call.tool, leak, raw)
			}
		}
		if !res.IsError {
			t.Fatalf("%s did not fail", call.tool)
		}
	}
}

func TestMissingRuntimeThroughMCP(t *testing.T) {
	r := newServerRig(t, false)
	cand := map[string]any{"task_id": r.taskID, "attempt_id": "a", "commit": "abcdef0", "work_package": map[string]any{
		"id": "wp", "version": 1, "digest": "sha256:" + strings.Repeat("a", 64), "base_commit": "abcdef0"}}
	res := r.call(t, "delegate", map[string]any{
		"meta":    map[string]any{"schema_version": "1.0", "project_id": "example", "correlation_id": "c", "expected_state_revision": r.rev},
		"task_id": r.taskID, "work_package": cand["work_package"]})
	if errorCode(t, res) != principal.CodeModelUnavailable {
		t.Fatal("delegate without a runtime must be MODEL_UNAVAILABLE")
	}
	acc := r.call(t, "accept", map[string]any{"meta": r.meta(), "candidate": cand, "validation_ids": []string{"v"}, "review_ids": []string{"r"}, "reason": "x"})
	if errorCode(t, acc) != principal.CodeNeedsPrincipal {
		t.Fatal("accept must be NEEDS_PRINCIPAL")
	}
}

func TestTaskStatusOperationHandleThroughMCP(t *testing.T) {
	r := newServerRig(t, false)
	ref, err := r.ops.Start("example", principal.KindValidate, time.Minute, func(context.Context) (string, error) { return "res-1", nil })
	if err != nil {
		t.Fatal(err)
	}
	r.ops.Wait(context.Background(), ref, 5*time.Second)
	res := r.call(t, "task_status", map[string]any{"meta": r.meta(), "operation": map[string]any{
		"id": ref.ID, "instance_id": ref.InstanceID, "kind": ref.Kind, "status": "running"}})
	if res.IsError {
		t.Fatalf("%v", structured(t, res))
	}
	lost := r.call(t, "task_status", map[string]any{"meta": r.meta(), "operation": map[string]any{
		"id": ref.ID, "instance_id": "inst_before_restart", "kind": ref.Kind, "status": "running"}})
	if errorCode(t, lost) != principal.CodeOperationLost {
		t.Fatal("a handle of another instance must be lost")
	}
}

func TestOversizeRequestIsRefusedAsCompleteTypedError(t *testing.T) {
	r := newServerRig(t, false)
	res := r.call(t, "project_state", map[string]any{"meta": r.meta(), "focus": strings.Repeat("x", facade.MaxRequestBytes)})
	if errorCode(t, res) != principal.CodeInvalidArgument {
		t.Fatal("an oversize request must be refused")
	}
}

func TestToolResultsNeverTruncate(t *testing.T) {
	huge := map[string]any{"data": strings.Repeat("x", facade.MaxResponseBytes+10)}
	res := mcpadapter.ToolResultForTest(huge, false)
	if !res.IsError {
		t.Fatal("an oversize response was not an error")
	}
	var env facade.Envelope
	text := res.Content[0].(*mcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatalf("the oversize refusal is not complete JSON: %v", err)
	}
	if env.Error == nil || env.Error.Code != principal.CodeContextUnfit || !env.Error.Retryable || len(text) > facade.MaxResponseBytes {
		t.Fatalf("unexpected %+v (%d bytes)", env.Error, len(text))
	}
	if env.Error.Validate() != nil {
		t.Fatal("not a valid SemanticError")
	}
	unmarshalable := mcpadapter.ToolResultForTest(func() {}, false)
	if !unmarshalable.IsError {
		t.Fatal("an unserialisable response was not an error")
	}
	var ok map[string]any
	if err := json.Unmarshal([]byte(unmarshalable.Content[0].(*mcp.TextContent).Text), &ok); err != nil {
		t.Fatal("the fallback is not valid JSON")
	}
}

func TestPanicsBecomeInternalErrors(t *testing.T) {
	res := mcpadapter.DispatchForTest(func(context.Context, principal.CallerContext, []byte) (any, *principal.SemanticError) {
		panic("boom with secret")
	})
	if !res.IsError {
		t.Fatal("a panic was not an error")
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if strings.Contains(text, "secret") || !strings.Contains(text, "INTERNAL") {
		t.Fatalf("unexpected %s", text)
	}
}

func TestNewServerRefusesBadComposition(t *testing.T) {
	if _, err := mcpadapter.NewServer(mcpadapter.Config{}); err == nil {
		t.Fatal("a server without a facade was built")
	}
	r := newServerRig(t, false)
	if _, err := mcpadapter.NewServer(mcpadapter.Config{Service: r.svc, Caller: principal.CallerContext{}}); err == nil {
		t.Fatal("a server without a valid caller was built")
	}
}
