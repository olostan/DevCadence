package selfhost_test

import (
	"os"
	"sort"
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
	cfg, err := selfhost.LoadConfig(t.TempDir(), os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		t.Skip("live Ollama test skipped: DEVCADENCE_OLLAMA_MODEL is not set")
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
