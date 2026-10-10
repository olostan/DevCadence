package selfhost_test

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/selfhost"
	"github.com/olostan/DevCadence/internal/tasks"
)

// TestLiveOllama runs the same scenario as the stub test against a REAL local
// Ollama and a REAL model. It is skipped unless DEVCADENCE_LIVE_OLLAMA=1 and
// DEVCADENCE_OLLAMA_MODEL name an installed model (optionally
// DEVCADENCE_OLLAMA_URL). It never pulls a model. Only this test can show that
// a real model drives the executor; the stub test cannot.
func TestLiveOllama(t *testing.T) {
	if os.Getenv("DEVCADENCE_LIVE_OLLAMA") != "1" {
		t.Skip("live Ollama test skipped: set DEVCADENCE_LIVE_OLLAMA=1 and DEVCADENCE_OLLAMA_MODEL=<installed model> to run it")
	}
	model := os.Getenv(selfhost.EnvOllamaModel)
	if model == "" {
		t.Skip("live Ollama test skipped: DEVCADENCE_OLLAMA_MODEL is not set")
	}
	home := t.TempDir()
	writeCfg(t, home, `{"model":`+strconv.Quote(model)+`}`) // env URL (if any) overrides
	cfg, err := selfhost.LoadConfig(home, os.Getenv)
	if err != nil || cfg == nil {
		t.Fatalf("config: %v %v", cfg, err)
	}
	f := newFixture(t)
	f.build(t, *cfg)
	t.Logf("live identity: ollama %s model %s digest %s", f.built.Identity.Version, f.built.Identity.Model, f.built.Identity.Digest)

	ref := f.delegate(t, 20*time.Minute)
	att := f.attempt(t)
	t.Logf("operation=%s attempt=%s summary=%q", ref.Status, att.Status, att.FailureSummary)
	if ref.Status != principal.StatusCompleted || att.Status != tasks.AttemptCandidateProduced {
		t.Fatalf("real model did not produce a candidate: operation %s, attempt %s %q", ref.Status, att.Status, att.FailureSummary)
	}
	changed := strings.Fields(f.git.Git("diff", "--name-only", f.wp.BaseCommit, att.CandidateCommit))
	sort.Strings(changed)
	t.Logf("candidate %s changed files: %v", att.CandidateCommit, changed)
	for _, p := range changed {
		if p != "calc.go" && p != "calc_test.go" {
			t.Errorf("out-of-scope change %s", p)
		}
	}
	if len(changed) == 0 {
		t.Error("candidate has no changes")
	}
}

// TestLiveOllamaRepair is the SH1-3 live acceptance: a REAL local model, the REAL
// executor in yolo mode, a disposable Go project with a real defect and a failing
// committed test, and the project's validation profile as post-check. It logs a
// compact report. Skipped unless DEVCADENCE_LIVE_OLLAMA=1 and
// DEVCADENCE_OLLAMA_MODEL are set; never pulls a model. Run it through
// scripts/selfhost-live.sh. A PASS shows one real model repaired one tiny defect
// once; it says nothing about reliability.
func TestLiveOllamaRepair(t *testing.T) {
	if os.Getenv("DEVCADENCE_LIVE_OLLAMA") != "1" {
		t.Skip("live Ollama test skipped: set DEVCADENCE_LIVE_OLLAMA=1 and DEVCADENCE_OLLAMA_MODEL=<installed model> (see scripts/selfhost-live.sh)")
	}
	model := os.Getenv(selfhost.EnvOllamaModel)
	if model == "" {
		t.Skip("live Ollama test skipped: DEVCADENCE_OLLAMA_MODEL is not set")
	}
	home := t.TempDir()
	writeCfg(t, home, `{"model":`+strconv.Quote(model)+`,"execution_mode":"yolo","max_repair_rounds":2}`)
	cfg, err := selfhost.LoadConfig(home, func(k string) string {
		if k == selfhost.EnvExecutionMode {
			return "" // never downgrade here: the test requires yolo
		}
		return os.Getenv(k)
	})
	if err != nil || cfg == nil {
		t.Fatalf("config: %v %v", cfg, err)
	}
	f := newFixture(t, map[string]string{"calc_test.go": calcTest, ".devcadence/validation.yaml": validationYAML})
	f.build(t, *cfg)
	id := f.built.Identity

	ref := f.delegate(t, 30*time.Minute)
	att := f.attempt(t)
	var b strings.Builder
	fmt.Fprintf(&b, "\n=== SH1-3 live repair report (real model; yolo = unconfined, NOT a sandbox) ===\n")
	fmt.Fprintf(&b, "model: %s digest=%s ollama=%s\nmodel identity (provenance): %s\n", id.Model, id.Digest, id.Version, att.ModelIdentity)
	fmt.Fprintf(&b, "operation=%s attempt=%s %q\n", ref.Status, att.Status, att.FailureSummary)
	arts := att.Artifacts
	if att.Status == tasks.AttemptCandidateProduced {
		arts = f.candidateEvent(t).Artifacts
	}
	var trace struct {
		Commands []struct {
			Argv     []string `json:"argv"`
			ExitCode int      `json:"exit_code"`
			Refused  string   `json:"refused"`
		} `json:"commands"`
	}
	if findArtifact(t, arts, "command-trace", &trace) {
		for _, c := range trace.Commands {
			fmt.Fprintf(&b, "model command: %s -> exit %d %s\n", strings.Join(c.Argv, " "), c.ExitCode, c.Refused)
		}
	}
	var rep reportDoc
	if findArtifact(t, arts, "validation-report", &rep) {
		fmt.Fprintf(&b, "validation: profile=%s digest=%s rounds=%d passed=%v\n", rep.ProfileID, rep.ProfileDigest, rep.RoundsUsed, rep.Passed)
		for _, r := range rep.Rounds {
			fmt.Fprintf(&b, "  round %d passed=%v checks=%d\n", r.Round, r.Passed, len(r.Checks))
		}
	}
	var review struct {
		Review string `json:"review"`
	}
	if findArtifact(t, arts, "review-status", &review) {
		fmt.Fprintf(&b, "review: %s\n", review.Review)
	}
	if att.CandidateCommit != "" {
		fmt.Fprintf(&b, "candidate commit: %s\nmanifest:\n%s", att.CandidateCommit,
			f.git.Git("diff", "--stat", f.wp.BaseCommit, att.CandidateCommit))
	}
	t.Log(b.String())

	if ref.Status != principal.StatusCompleted || att.Status != tasks.AttemptCandidateProduced {
		t.Fatalf("no validated candidate: operation %s, attempt %s %q", ref.Status, att.Status, att.FailureSummary)
	}
	if !rep.Passed {
		t.Errorf("candidate without a passing validation report")
	}
	changed := strings.Fields(f.git.Git("diff", "--name-only", f.wp.BaseCommit, att.CandidateCommit))
	sort.Strings(changed)
	for _, p := range changed {
		if p != "calc.go" && p != "calc_test.go" {
			t.Errorf("out-of-scope change %s", p)
		}
	}
}
