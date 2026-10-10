package selfhost_test

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/selfhost"
	"github.com/olostan/DevCadence/internal/tasks"
)

func rawToolCall(name string, args map[string]any) map[string]any {
	return map[string]any{"function": map[string]any{"name": name, "arguments": args}}
}

// scriptedPatchAndRun: turn 1 patches calc.go (multi-line replacement) and
// writes the test; turn 2 runs `go test ./...`; turn 3 is the final message.
func scriptedPatchAndRun() func(int, map[string]any) (int, any) {
	return func(n int, _ map[string]any) (int, any) {
		resp := map[string]any{"model": testModel, "done": true, "prompt_eval_count": 100, "eval_count": 20}
		var msg map[string]any
		switch n {
		case 1:
			msg = map[string]any{"role": "assistant", "tool_calls": []any{
				rawToolCall("apply_patch", map[string]any{"path": "calc.go", "edits": []map[string]string{{
					"old_text": "func Add(a, b int) int { return a - b }\n",
					"new_text": "func Add(a, b int) int {\n\t// fixed: add, do not subtract\n\treturn a + b\n}\n",
				}}}),
				rawToolCall("write_file", map[string]any{"path": "calc_test.go", "content": calcTest}),
			}}
		case 2:
			msg = map[string]any{"role": "assistant", "tool_calls": []any{
				rawToolCall("run_command", map[string]any{"argv": []string{"go", "test", "./..."}, "timeout_seconds": 300}),
			}}
		default:
			msg = map[string]any{"role": "assistant", "content": "Done: Add fixed and tested."}
		}
		resp["message"] = msg
		return http.StatusOK, resp
	}
}

func offeredTools(req map[string]any) []string {
	var names []string
	ts, _ := req["tools"].([]any)
	for _, t := range ts {
		fn, _ := t.(map[string]any)["function"].(map[string]any)
		names = append(names, fn["name"].(string))
	}
	sort.Strings(names)
	return names
}

// STUB-BASED: proves tool wiring, mode gating and audit persistence with a
// scripted model. It is NOT evidence that any real model can use these tools.
func TestStubOllama_ApplyPatchAndRunCommandYolo(t *testing.T) {
	stub := newStubOllama(t, scriptedPatchAndRun())
	f := newFixture(t)
	cfg := stub.config()
	cfg.ExecutionMode = selfhost.ExecutionModeYolo
	f.build(t, cfg)

	ref := f.delegate(t, 3*time.Minute)
	if ref.Status != principal.StatusCompleted {
		st, _ := f.registry.Lookup(testProject, ref)
		t.Fatalf("operation status %s: %+v", ref.Status, st.Error)
	}
	att := f.attempt(t)
	if att.Status != tasks.AttemptCandidateProduced || att.CandidateCommit == "" {
		t.Fatalf("attempt = %+v", att)
	}
	base := f.wp.BaseCommit
	changed := strings.Fields(f.git.Git("diff", "--name-only", base, att.CandidateCommit))
	sort.Strings(changed)
	if strings.Join(changed, ",") != "calc.go,calc_test.go" {
		t.Errorf("changed files = %v", changed)
	}
	if got := f.git.Git("show", att.CandidateCommit+":calc.go"); !strings.Contains(got, "\treturn a + b\n}") {
		t.Errorf("calc.go not patched:\n%s", got)
	}

	if n := stub.chatCalls(); n != 3 {
		t.Fatalf("chat calls = %d, want 3", n)
	}
	if got := strings.Join(offeredTools(stub.chatReqs[0]), ","); got != "apply_patch,grep,read_file,run_command,symbols,write_file" {
		t.Errorf("offered tools = %s", got)
	}
	// The model received the run_command result (exit 0, marker) on its next turn.
	last, _ := json.Marshal(stub.chatReqs[2]["messages"])
	for _, want := range []string{`\"exit_code\":0`, `unsafe_unconfined`, `hunks_applied`} {
		if !strings.Contains(string(last), want) {
			t.Errorf("third request lacks %q", want)
		}
	}

	// Audit: the command trace artifact is attached to the candidate event.
	var trace struct {
		Schema           string `json:"schema"`
		UnsafeUnconfined bool   `json:"unsafe_unconfined"`
		Commands         []struct {
			Argv             []string `json:"argv"`
			ExitCode         int      `json:"exit_code"`
			OutputSHA256     string   `json:"output_sha256"`
			UnsafeUnconfined bool     `json:"unsafe_unconfined"`
		} `json:"commands"`
	}
	var found bool
	for _, a := range f.candidateEvent(t).Artifacts {
		if a.Kind == "command-trace" {
			found = true
			if err := json.Unmarshal(readFile(t, a.Locator), &trace); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !found || !trace.UnsafeUnconfined || len(trace.Commands) != 1 {
		t.Fatalf("command trace missing or wrong: found=%v %+v", found, trace)
	}
	c := trace.Commands[0]
	if strings.Join(c.Argv, " ") != "go test ./..." || c.ExitCode != 0 || len(c.OutputSHA256) != 64 || !c.UnsafeUnconfined {
		t.Errorf("command record = %+v", c)
	}
}

// STUB-BASED: in the default strict mode run_command is not offered to the
// model, and a scripted model that calls it anyway fails the attempt at the
// driver's tool-list integrity check, before any command can run.
func TestStubOllama_StrictModeNeverOffersRunCommand(t *testing.T) {
	stub := newStubOllama(t, scriptedPatchAndRun())
	f := newFixture(t)
	f.build(t, stub.config()) // ExecutionMode unset => strict

	ref := f.delegate(t, 3*time.Minute)
	if ref.Status != principal.StatusFailed {
		t.Fatalf("operation status %s, want failed", ref.Status)
	}
	if got := strings.Join(offeredTools(stub.chatReqs[0]), ","); got != "apply_patch,grep,read_file,symbols,write_file" {
		t.Errorf("offered tools = %s", got)
	}
	att := f.attempt(t)
	if att.Status != tasks.AttemptFailed || att.CandidateCommit != "" || len(att.Artifacts) != 0 {
		t.Errorf("attempt = %+v", att)
	}
}
