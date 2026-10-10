package selfhost_test

// STUB-BASED. Every test here drives the REAL selfhost composition and the REAL
// native executor (worktrees, apply_patch, run_command, validation profile,
// control plane) against a scripted httptest Ollama. They prove the
// model -> edit tools -> validation -> feedback/repair -> candidate path and its
// failure semantics. They are NOT evidence that any real model can repair code.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/selfhost"
	"github.com/olostan/DevCadence/internal/storage"
	"github.com/olostan/DevCadence/internal/tasks"
)

const validationYAML = `profiles:
  default:
    checks:
      - id: go-test
        kind: test
        argv: ["go", "test", "./..."]
        timeout: 5m
`

const coverageYAML = `profiles:
  default:
    checks:
      - id: go-test
        kind: test
        argv: ["go", "test", "-coverprofile=cover.out", "./..."]
        timeout: 5m
`

// noisyCalcTest fails while Add is wrong and prints a long log, so the test can
// prove the model only receives a bounded tail.
const noisyCalcTest = `package calc

import "testing"

func TestAdd(t *testing.T) {
	for i := 0; i < 800; i++ {
		t.Logf("noise-%04d padding padding padding", i)
	}
	if got := Add(2, 3); got != 5 {
		t.Fatalf("Add(2, 3) = %d, want 5", got)
	}
}
`

const wrongCalc = "package calc\n\n// Add returns the sum of a and b.\nfunc Add(a, b int) int { return a * b }\n"

func postCheckFiles(yaml string) map[string]string {
	return map[string]string{".devcadence/validation.yaml": yaml}
}

type reply func() map[string]any

func toolsReply(calls ...map[string]any) reply {
	return func() map[string]any {
		cs := make([]any, len(calls))
		for i, c := range calls {
			cs[i] = c
		}
		return map[string]any{"role": "assistant", "tool_calls": cs}
	}
}

func doneReply(text string) reply {
	return func() map[string]any { return map[string]any{"role": "assistant", "content": text} }
}

// script replies n-th with replies[n-1]; beyond the end it says "done".
// Usage counts are deliberately omitted: usage must stay unknown, not invented.
func script(replies ...reply) func(int, map[string]any) (int, any) {
	return func(n int, _ map[string]any) (int, any) {
		r := doneReply("done")
		if n <= len(replies) {
			r = replies[n-1]
		}
		return http.StatusOK, map[string]any{"model": testModel, "done": true, "message": r()}
	}
}

func writeCalcTest() map[string]any {
	return rawToolCall("write_file", map[string]any{"path": "calc_test.go", "content": noisyCalcTest})
}

func patchAdd(oldText, newText string) map[string]any {
	return rawToolCall("apply_patch", map[string]any{"path": "calc.go", "edits": []map[string]string{{"old_text": oldText, "new_text": newText}}})
}

func lastUserMessage(t *testing.T, req map[string]any) string {
	t.Helper()
	msgs, _ := req["messages"].([]any)
	for i := len(msgs) - 1; i >= 0; i-- {
		m, _ := msgs[i].(map[string]any)
		if m["role"] == "user" {
			s, _ := m["content"].(string)
			return s
		}
	}
	t.Fatal("no user message in request")
	return ""
}

type reportDoc struct {
	Schema           string `json:"schema"`
	ProfileID        string `json:"profile_id"`
	ProfileDigest    string `json:"profile_digest"`
	ExecutionMode    string `json:"execution_mode"`
	UnsafeUnconfined bool   `json:"unsafe_unconfined"`
	RoundsUsed       int    `json:"rounds_used"`
	Passed           bool   `json:"passed"`
	Rounds           []struct {
		Round      int  `json:"round"`
		Passed     bool `json:"passed"`
		DurationMS int64
		Checks     []struct {
			ID       string   `json:"id"`
			Argv     []string `json:"argv"`
			Status   string   `json:"status"`
			ExitCode *int     `json:"exit_code"`
		} `json:"checks"`
	} `json:"rounds"`
}

func findArtifact(t *testing.T, arts []protocol.ArtifactRef, kind string, into any) bool {
	t.Helper()
	for _, a := range arts {
		if a.Kind == kind {
			if into != nil {
				if err := json.Unmarshal(readFile(t, a.Locator), into); err != nil {
					t.Fatalf("decode %s: %v", kind, err)
				}
			}
			return true
		}
	}
	return false
}

func yolo(stub *stubOllama, rounds int) selfhost.Config {
	cfg := yoloConfig(stub)
	cfg.MaxRepairRounds = &rounds
	return cfg
}

func TestStub_PostCheckRepairLoopProducesCandidate(t *testing.T) {
	stub := newStubOllama(t, script(
		toolsReply(writeCalcTest(), patchAdd("func Add(a, b int) int { return a - b }\n", "func Add(a, b int) int { return a * b }\n")),
		doneReply("Implemented Add."),
		// request 3 carries the validation feedback
		toolsReply(patchAdd("return a * b", "return a + b")),
		toolsReply(rawToolCall("run_command", map[string]any{"argv": []string{"go", "test", "./..."}})),
		doneReply("Fixed Add to return a+b."),
	))
	f := newFixture(t, postCheckFiles(validationYAML))
	f.build(t, yolo(stub, 2))

	ref := f.delegate(t, 5*time.Minute)
	st, _ := f.registry.Lookup(testProject, ref)
	if ref.Status != principal.StatusCompleted {
		t.Fatalf("operation %s: %+v", ref.Status, st.Error)
	}
	if n := stub.chatCalls(); n != 5 {
		t.Fatalf("chat calls = %d, want 5", n)
	}

	// The next request after the failed check carries concise feedback, not the log.
	fb := lastUserMessage(t, stub.chatReqs[2])
	for _, want := range []string{"validation FAILED", "failing check: go-test", "command: go test ./...", "exit code: 1", "first failing test: TestAdd"} {
		if !strings.Contains(fb, want) {
			t.Errorf("feedback lacks %q:\n%s", want, fb)
		}
	}
	if strings.Contains(fb, "noise-0001") || len(fb) > 6*1024 {
		t.Errorf("feedback is not bounded (len %d) or contains early log lines", len(fb))
	}
	if !strings.Contains(fb, "noise-0799") {
		t.Errorf("feedback lacks the log tail")
	}

	att := f.attempt(t)
	if att.Status != tasks.AttemptCandidateProduced || att.CandidateCommit == "" {
		t.Fatalf("attempt = %+v", att)
	}
	if want := "ollama-local/" + testModel + "@" + testDigest; att.ModelIdentity != want {
		t.Errorf("model identity = %q, want %q", att.ModelIdentity, want)
	}
	changed := strings.Fields(f.git.Git("diff", "--name-only", f.wp.BaseCommit, att.CandidateCommit))
	sort.Strings(changed)
	if strings.Join(changed, ",") != "calc.go,calc_test.go" {
		t.Errorf("candidate manifest = %v", changed)
	}
	if got := f.git.Git("show", att.CandidateCommit+":calc.go"); !strings.Contains(got, "return a + b") {
		t.Errorf("candidate calc.go not repaired:\n%s", got)
	}
	if head := f.git.Head(); head != f.wp.BaseCommit {
		t.Errorf("primary checkout HEAD moved to %s", head)
	}

	arts := f.candidateEvent(t).Artifacts
	var rep reportDoc
	if !findArtifact(t, arts, "validation-report", &rep) {
		t.Fatalf("no validation-report among %v", arts)
	}
	if rep.Schema != "devcadence.validation_report/v1" || rep.ProfileID != "default" || !strings.HasPrefix(rep.ProfileDigest, "sha256:") ||
		!rep.UnsafeUnconfined || rep.ExecutionMode != "unsafe_unconfined_local" || !rep.Passed || rep.RoundsUsed != 2 || len(rep.Rounds) != 2 {
		t.Fatalf("report = %+v", rep)
	}
	r1, r2 := rep.Rounds[0], rep.Rounds[1]
	if r1.Passed || r1.Checks[0].ExitCode == nil || *r1.Checks[0].ExitCode != 1 || r1.Checks[0].Status != "fail" ||
		!r2.Passed || r2.Checks[0].ExitCode == nil || *r2.Checks[0].ExitCode != 0 {
		t.Errorf("rounds = %+v", rep.Rounds)
	}
	if strings.Join(r1.Checks[0].Argv, " ") != "go test ./..." {
		t.Errorf("argv = %v", r1.Checks[0].Argv)
	}

	var trace struct {
		Commands []struct {
			Argv     []string `json:"argv"`
			ExitCode int      `json:"exit_code"`
		} `json:"commands"`
	}
	if !findArtifact(t, arts, "command-trace", &trace) || len(trace.Commands) != 1 || trace.Commands[0].ExitCode != 0 {
		t.Errorf("command trace missing/wrong: %+v", trace)
	}

	var review struct {
		Review      string `json:"review"`
		Reason      string `json:"reason"`
		Independent bool   `json:"independent"`
	}
	if !findArtifact(t, arts, "review-status", &review) || review.Review != "review_unavailable" || review.Independent ||
		!strings.Contains(review.Reason, "owner manual acceptance required") {
		t.Errorf("review status = %+v", review)
	}

	// Usage is unknown (the stub omits eval counts) and must not be invented.
	var usage struct {
		UsageKnown bool `json:"usage_known"`
	}
	if !findArtifact(t, arts, "usage", &usage) || usage.UsageKnown {
		t.Errorf("usage = %+v, want usage_known=false", usage)
	}
}

func alwaysWrong() []reply {
	var rs []reply
	for i := 0; i < 6; i++ {
		rs = append(rs,
			toolsReply(writeCalcTest(), rawToolCall("write_file", map[string]any{"path": "calc.go", "content": wrongCalc})),
			doneReply("done"))
	}
	return rs
}

func TestStub_PostCheckFailureNoCandidateAndRoundsCapped(t *testing.T) {
	for _, rounds := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("max_repair_rounds=%d", rounds), func(t *testing.T) {
			stub := newStubOllama(t, script(alwaysWrong()...))
			f := newFixture(t, postCheckFiles(validationYAML))
			f.build(t, yolo(stub, rounds))

			ref := f.delegate(t, 5*time.Minute)
			if ref.Status != principal.StatusFailed {
				t.Fatalf("operation %s, want failed", ref.Status)
			}
			if want := 2 * (rounds + 1); stub.chatCalls() != want {
				t.Fatalf("chat calls = %d, want %d", stub.chatCalls(), want)
			}
			att := f.attempt(t)
			if att.Status != tasks.AttemptFailed || att.FailureSummary != "reason=validation_failed effects=none" || att.CandidateCommit != "" {
				t.Fatalf("attempt = %s %q candidate=%q", att.Status, att.FailureSummary, att.CandidateCommit)
			}
			var rep reportDoc
			if !findArtifact(t, att.Artifacts, "validation-report", &rep) {
				t.Fatalf("failure lacks validation-report: %+v", att.Artifacts)
			}
			if rep.Passed || rep.RoundsUsed != rounds+1 || len(rep.Rounds) != rounds+1 {
				t.Errorf("report = %+v", rep)
			}
			for _, a := range att.Artifacts {
				if a.Kind == "review-status" {
					t.Error("failed attempt must not carry a review label as if it were a candidate")
				}
			}
			if head := f.git.Head(); head != f.wp.BaseCommit {
				t.Errorf("primary HEAD moved to %s", head)
			}
			// No CandidateProduced event exists.
			for _, ev := range allEvents(t, f) {
				if _, ok := ev.Payload.(*events.CandidateProduced); ok {
					t.Fatal("CandidateProduced persisted for a failing attempt")
				}
			}
		})
	}
}

func TestStub_StrictModeRefusesPostCheckBeforeAnySubprocess(t *testing.T) {
	stub := newStubOllama(t, script(doneReply("done")))
	f := newFixture(t, postCheckFiles(validationYAML))
	f.build(t, stub.config()) // strict

	_, err := f.built.Executor.Delegate(t.Context(), f.authorizedTask())
	if err == nil || !strings.Contains(err.Error(), "yolo") {
		t.Fatalf("Delegate err = %v, want actionable refusal naming yolo", err)
	}
	var coded *principal.CodedError
	if !errors.As(err, &coded) || len(coded.EvidenceRefs()) == 0 || coded.EvidenceRefs()[0] != "postcheck_requires_yolo" {
		t.Errorf("error = %#v, want ref postcheck_requires_yolo", err)
	}
	if stub.chatCalls() != 0 {
		t.Errorf("model was called %d times", stub.chatCalls())
	}
	if _, err := os.Stat(filepath.Join(f.deps.StateDir, "attempts")); !os.IsNotExist(err) {
		t.Errorf("an attempt scratch dir exists (a subprocess may have run): %v", err)
	}
	d, derr := f.harness.Service.TaskDetail(t.Context(), testProject, "SH-1")
	if derr != nil || len(d.Attempts) != 0 {
		t.Errorf("attempts = %v err %v, want none", d, derr)
	}
}

func TestStub_ProfileValidationRejectsShells(t *testing.T) {
	stub := newStubOllama(t, script(doneReply("done")))
	f := newFixture(t, postCheckFiles("profiles:\n  default:\n    checks:\n      - id: x\n        argv: [\"sh\", \"-c\", \"id\"]\n        timeout: 1m\n"))
	f.build(t, yolo(stub, 2))
	if _, err := f.built.Executor.Delegate(t.Context(), f.authorizedTask()); err == nil || stub.chatCalls() != 0 {
		t.Fatalf("shell profile accepted: err=%v chat=%d", err, stub.chatCalls())
	}
}

func TestStub_OutOfScopeEditRefusedAndExcludedFromCandidate(t *testing.T) {
	stub := newStubOllama(t, script(
		toolsReply(
			rawToolCall("write_file", map[string]any{"path": "go.mod", "content": "module evil\n"}),
			writeCalcTest(),
			patchAdd("func Add(a, b int) int { return a - b }\n", "func Add(a, b int) int { return a + b }\n")),
		doneReply("done"),
	))
	f := newFixture(t, postCheckFiles(validationYAML))
	f.build(t, yolo(stub, 2))
	if ref := f.delegate(t, 5*time.Minute); ref.Status != principal.StatusCompleted {
		t.Fatalf("operation %s", ref.Status)
	}
	if !strings.Contains(strings.ToLower(lastToolResults(stub.chatReqs[1])), "scope") {
		t.Errorf("model was not told the go.mod write was refused: %s", lastToolResults(stub.chatReqs[1]))
	}
	att := f.attempt(t)
	changed := strings.Fields(f.git.Git("diff", "--name-only", f.wp.BaseCommit, att.CandidateCommit))
	sort.Strings(changed)
	if strings.Join(changed, ",") != "calc.go,calc_test.go" {
		t.Errorf("candidate manifest = %v", changed)
	}
}

func lastToolResults(req map[string]any) string {
	b, _ := json.Marshal(req["messages"])
	return string(b)
}

// A passing post-check that leaves an out-of-scope artifact (coverage profile)
// in the worktree still cannot become a candidate.
func TestStub_PostCheckPassButOutOfScopeFileFailsScope(t *testing.T) {
	stub := newStubOllama(t, script(
		toolsReply(
			writeCalcTest(),
			patchAdd("func Add(a, b int) int { return a - b }\n", "func Add(a, b int) int { return a + b }\n")),
		doneReply("done"),
	))
	f := newFixture(t, postCheckFiles(coverageYAML))
	f.build(t, yolo(stub, 2))
	if ref := f.delegate(t, 5*time.Minute); ref.Status != principal.StatusFailed {
		t.Fatalf("operation %s, want failed", ref.Status)
	}
	att := f.attempt(t)
	if att.Status != tasks.AttemptFailed || !strings.Contains(att.FailureSummary, "scope_violation") || att.CandidateCommit != "" {
		t.Fatalf("attempt = %s %q", att.Status, att.FailureSummary)
	}
	var rep reportDoc
	if !findArtifact(t, att.Artifacts, "validation-report", &rep) || !rep.Passed {
		t.Errorf("expected passed validation-report on scope failure, got %+v", att.Artifacts)
	}
}

func TestStub_OllamaErrorMidRepairIsUncertainAndNotRetried(t *testing.T) {
	stub := newStubOllama(t, func(n int, _ map[string]any) (int, any) {
		switch n {
		case 1:
			return http.StatusOK, map[string]any{"model": testModel, "done": true, "message": toolsReply(writeCalcTest(), rawToolCall("write_file", map[string]any{"path": "calc.go", "content": wrongCalc}))()}
		case 2:
			return http.StatusOK, map[string]any{"model": testModel, "done": true, "message": doneReply("done")()}
		}
		return http.StatusInternalServerError, map[string]string{"error": "model runner crashed"}
	})
	f := newFixture(t, postCheckFiles(validationYAML))
	f.build(t, yolo(stub, 2))

	if ref := f.delegate(t, 5*time.Minute); ref.Status != principal.StatusFailed {
		t.Fatalf("operation %s, want failed", ref.Status)
	}
	att := f.attempt(t)
	if att.Status != tasks.AttemptFailed || att.FailureSummary != "reason=driver_error effects=uncertain" || att.CandidateCommit != "" {
		t.Fatalf("attempt = %s %q", att.Status, att.FailureSummary)
	}
	var rep reportDoc
	if !findArtifact(t, att.Artifacts, "validation-report", &rep) || rep.Passed || rep.RoundsUsed != 1 {
		t.Errorf("evidence of the failed round missing: %+v", att.Artifacts)
	}
	calls := stub.chatCalls()

	// A new delegation must not silently make a second model call.
	_, err := f.built.Executor.Delegate(t.Context(), f.authorizedTask())
	var coded *principal.CodedError
	if err == nil || !errors.As(err, &coded) || len(coded.EvidenceRefs()) == 0 || coded.EvidenceRefs()[0] != "uncertain_prior_attempt" {
		t.Fatalf("second Delegate err = %v, want uncertain_prior_attempt", err)
	}
	if stub.chatCalls() != calls {
		t.Errorf("model re-invoked after uncertain attempt")
	}
}

func allEvents(t *testing.T, f *fixture) []events.Event {
	t.Helper()
	var evs []events.Event
	err := f.harness.Store.Read(t.Context(), func(tx *storage.Tx) error {
		var err error
		evs, err = tx.ReadEvents(t.Context(), storage.EventQuery{ProjectID: testProject})
		return err
	})
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	return evs
}

// A committed symlink used as a check `dir` must not let a check run outside the
// worktree: the check errors, nothing runs, no candidate is produced.
func TestStub_CheckDirSymlinkEscapeRefused(t *testing.T) {
	outside := t.TempDir()
	yaml := "profiles:\n  default:\n    checks:\n      - id: go-test\n        dir: esc\n        argv: [\"go\", \"version\"]\n        timeout: 1m\n"
	stub := newStubOllama(t, script(alwaysWrong()...))
	f := newFixture(t, postCheckFiles(yaml), map[string]string{"esc": "symlink:" + outside})
	f.build(t, yolo(stub, 0))
	if ref := f.delegate(t, 5*time.Minute); ref.Status != principal.StatusFailed {
		t.Fatalf("operation %s, want failed", ref.Status)
	}
	att := f.attempt(t)
	var rep reportDoc
	if att.CandidateCommit != "" || !findArtifact(t, att.Artifacts, "validation-report", &rep) ||
		rep.Passed || rep.Rounds[0].Checks[0].Status == "pass" || rep.Rounds[0].Checks[0].ExitCode != nil {
		t.Fatalf("attempt = %+v report = %+v", att, rep)
	}
}

// A profile that cannot run (unknown module) errors inside the validation run:
// the attempt fails closed as validation_error with uncertain effects.
func TestStub_PostCheckRunErrorIsUncertain(t *testing.T) {
	yaml := "profiles:\n  default:\n    module_id: nope\n    checks:\n      - id: go-test\n        argv: [\"go\", \"version\"]\n        timeout: 1m\n"
	stub := newStubOllama(t, script(
		toolsReply(writeCalcTest(), rawToolCall("write_file", map[string]any{"path": "calc.go", "content": wrongCalc})),
		doneReply("done")))
	f := newFixture(t, postCheckFiles(yaml))
	f.build(t, yolo(stub, 1))
	ref := f.delegate(t, 5*time.Minute)
	if ref.Status != principal.StatusFailed {
		t.Fatalf("operation %s, want failed", ref.Status)
	}
	att := f.attempt(t)
	if att.CandidateCommit != "" || att.FailureSummary != "reason=validation_error effects=uncertain" {
		t.Fatalf("attempt = %s %q", att.Status, att.FailureSummary)
	}
}
