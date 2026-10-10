package selfhost_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/cognition/sessionclients"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/selfhost"
	"github.com/olostan/DevCadence/internal/tasks"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func writeCfg(t *testing.T, home, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(selfhost.ConfigPath(home), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfig(t *testing.T) {
	t.Run("no file is nil, even with env set (env cannot enable)", func(t *testing.T) {
		home := t.TempDir()
		for _, e := range []map[string]string{
			nil,
			{selfhost.EnvOllamaModel: "qwen2.5-coder"},
			{selfhost.EnvOllamaModel: "m:1", selfhost.EnvOllamaURL: "http://127.0.0.1:11434"},
			{selfhost.EnvOllamaURL: "http://127.0.0.1:11434"},
		} {
			cfg, err := selfhost.LoadConfig(home, env(e))
			if err != nil || cfg != nil {
				t.Fatalf("env %v: cfg=%v err=%v, want nil,nil", e, cfg, err)
			}
		}
	})
	t.Run("file with defaults applied", func(t *testing.T) {
		home := t.TempDir()
		writeCfg(t, home, `{"model":"qwen2.5-coder"}`)
		cfg, err := selfhost.LoadConfig(home, env(nil))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Model != "qwen2.5-coder:latest" || cfg.OllamaURL != "http://127.0.0.1:11434" ||
			cfg.EndpointID != selfhost.DefaultEndpointID || cfg.ContextTokens != selfhost.DefaultContextTokens {
			t.Fatalf("unexpected defaults: %+v", cfg)
		}
	})
	t.Run("file without model is rejected", func(t *testing.T) {
		home := t.TempDir()
		writeCfg(t, home, `{}`)
		if _, err := selfhost.LoadConfig(home, env(nil)); !errors.Is(err, errs.ErrInvalidArgument) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("non-loopback url is rejected", func(t *testing.T) {
		home := t.TempDir()
		writeCfg(t, home, `{"model":"m:1"}`)
		_, err := selfhost.LoadConfig(home, env(map[string]string{selfhost.EnvOllamaURL: "http://example.com:11434"}))
		if !errors.Is(err, errs.ErrPolicyDenied) {
			t.Fatalf("err = %v, want policy denied", err)
		}
	})
	t.Run("env overrides file fields; unknown keys rejected", func(t *testing.T) {
		h := t.TempDir()
		writeCfg(t, h, `{"model":"a:1","endpoint_id":"ep-x"}`)
		cfg, err := selfhost.LoadConfig(h, env(map[string]string{selfhost.EnvOllamaModel: "b:2"}))
		if err != nil || cfg.Model != "b:2" || cfg.EndpointID != "ep-x" {
			t.Fatalf("cfg=%+v err=%v", cfg, err)
		}
		writeCfg(t, h, `{"model":"a:1","api_key":"x"}`)
		if _, err := selfhost.LoadConfig(h, env(nil)); !errors.Is(err, errs.ErrInvalidArgument) {
			t.Fatalf("unknown key err = %v", err)
		}
	})
}

func TestLoopbackURLMustBeBareOrigin(t *testing.T) {
	for _, u := range []string{"http://127.0.0.1:11434", "http://127.0.0.1:11434/", "http://localhost:1"} {
		if err := sessionclients.ValidateLoopbackURL(u); err != nil {
			t.Errorf("%s rejected: %v", u, err)
		}
	}
	for _, u := range []string{"http://127.0.0.1:11434/v1", "http://127.0.0.1:1/?x=1", "http://127.0.0.1:1/#f", "http://127.0.0.1:1?x=1"} {
		if err := sessionclients.ValidateLoopbackURL(u); err == nil {
			t.Errorf("%s accepted", u)
		}
	}
}

func TestProbe_RefusesRedirect(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			hits++
			return
		}
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer srv.Close()
	_, err := selfhost.Probe(context.Background(), selfhost.Config{OllamaURL: srv.URL, Model: testModel})
	if !errors.Is(err, errs.ErrModelUnavailable) || hits != 0 {
		t.Fatalf("err = %v, redirect followed %d times", err, hits)
	}
}

func TestProbe(t *testing.T) {
	stub := newStubOllama(t, scriptedFix(false))
	id, err := selfhost.Probe(context.Background(), stub.config())
	if err != nil {
		t.Fatal(err)
	}
	if id.Version != "0.9.9-stub" || id.Digest != testDigest || id.Model != testModel || len(id.Installed) != 1 {
		t.Fatalf("identity = %+v", id)
	}
}

// Negative: Ollama down -> actionable, categorized error.
func TestBuild_OllamaDown(t *testing.T) {
	stub := newStubOllama(t, scriptedFix(false))
	cfg := stub.config()
	stub.srv.Close()
	f := newFixture(t)
	_, err := selfhost.Build(context.Background(), cfg, f.deps)
	if !errors.Is(err, errs.ErrModelUnavailable) {
		t.Fatalf("err = %v, want model_unavailable", err)
	}
	if !strings.Contains(err.Error(), "ollama serve") {
		t.Fatalf("error is not actionable: %v", err)
	}
}

// Negative: model not installed -> error naming the model and `ollama pull`.
func TestBuild_ModelNotInstalled(t *testing.T) {
	stub := newStubOllama(t, scriptedFix(false))
	cfg := stub.config()
	cfg.Model = "other-model:1b"
	f := newFixture(t)
	_, err := selfhost.Build(context.Background(), cfg, f.deps)
	if !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("err = %v, want not_found", err)
	}
	if !strings.Contains(err.Error(), "ollama pull other-model:1b") || !strings.Contains(err.Error(), testModel) {
		t.Fatalf("error does not name the model and the pull command: %v", err)
	}
}

// STUB-BASED deterministic integration test. A scripted httptest server
// impersonates Ollama; the REAL sessionclients.Composition and the REAL
// taskexec.Executor.Delegate run against it. It does NOT prove that a real model
// can complete the task (see TestLiveOllama for that).
func TestStubOllama_DelegateProducesCandidate(t *testing.T) {
	for _, tc := range []struct {
		name        string
		withCounts  bool
		wantKnown   bool
		wantInTotal int64
	}{
		{"usage unknown when Ollama omits eval counts", false, false, 0},
		{"usage known when Ollama reports eval counts", true, true, 120 + 150},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := newStubOllama(t, scriptedFix(tc.withCounts))
			f := newFixture(t)
			f.build(t, stub.config())

			ref := f.delegate(t, 30*time.Second)
			if ref.Status != principal.StatusCompleted {
				st, _ := f.registry.Lookup(testProject, ref)
				t.Fatalf("operation status %s: %+v", ref.Status, st.Error)
			}
			att := f.attempt(t)
			if att.Status != tasks.AttemptCandidateProduced || att.CandidateCommit == "" {
				t.Fatalf("attempt = %+v", att)
			}
			// Identity recorded from the stub's probe, not from config alone.
			if want := "ollama-local/" + testModel + "@" + testDigest; att.ModelIdentity != want {
				t.Errorf("model identity = %q, want %q", att.ModelIdentity, want)
			}
			// Candidate is a commit on the base, with exactly the expected manifest.
			base := f.wp.BaseCommit
			if parent := f.git.Git("rev-parse", att.CandidateCommit+"^"); parent != base {
				t.Errorf("candidate parent = %s, want %s", parent, base)
			}
			changed := strings.Fields(f.git.Git("diff", "--name-only", base, att.CandidateCommit))
			sort.Strings(changed)
			if strings.Join(changed, ",") != "calc.go,calc_test.go" {
				t.Errorf("changed files = %v", changed)
			}
			if got := f.git.Git("show", att.CandidateCommit+":calc.go"); !strings.Contains(got, "a + b") {
				t.Errorf("candidate calc.go not fixed:\n%s", got)
			}
			// The model never touched the primary checkout.
			if head := f.git.Head(); head != base {
				t.Errorf("main checkout HEAD moved to %s", head)
			}

			// Exactly two scripted model calls; second carries the tool results.
			if n := stub.chatCalls(); n != 2 {
				t.Fatalf("chat calls = %d, want 2", n)
			}
			first := stub.chatReqs[0]
			if first["model"] != testModel || first["stream"] != false {
				t.Errorf("first request = model %v stream %v", first["model"], first["stream"])
			}
			if tools, _ := first["tools"].([]any); len(tools) == 0 {
				t.Error("no tools were offered to the model")
			}

			// Usage artifact reflects only what the stub reported.
			ev := f.candidateEvent(t)
			var usage struct {
				UsageKnown      bool `json:"usage_known"`
				CumulativeUsage struct {
					Input  *int64 `json:"input_tokens"`
					Cached *int64 `json:"cached_tokens"`
					Output *int64 `json:"output_tokens"`
				} `json:"cumulative_usage"`
			}
			var found bool
			for _, a := range ev.Artifacts {
				if a.Kind == "usage" {
					found = true
					if err := json.Unmarshal(readFile(t, a.Locator), &usage); err != nil {
						t.Fatal(err)
					}
				}
			}
			if !found {
				t.Fatal("no usage artifact")
			}
			cu := usage.CumulativeUsage
			if usage.UsageKnown != tc.wantKnown || (cu.Input != nil) != tc.wantKnown || (cu.Output != nil) != tc.wantKnown {
				t.Errorf("usage = %+v, want known=%v", usage, tc.wantKnown)
			}
			if cu.Cached != nil {
				t.Errorf("cached tokens were fabricated: %d", *cu.Cached)
			}
			if tc.wantKnown && (cu.Input == nil || *cu.Input != tc.wantInTotal || cu.Output == nil || *cu.Output != 40) {
				t.Errorf("tokens = %+v, want input %d output 40", cu, tc.wantInTotal)
			}
		})
	}
}

// Negative: Ollama fails mid-call. The attempt must fail with effects=uncertain
// and Recover on a rebuilt executor must not re-invoke the model.
func TestStubOllama_MidCallErrorIsUncertainAndNotRetried(t *testing.T) {
	stub := newStubOllama(t, func(n int, _ map[string]any) (int, any) {
		return http.StatusInternalServerError, map[string]string{"error": "model runner crashed"}
	})
	f := newFixture(t)
	f.build(t, stub.config())

	ref := f.delegate(t, 30*time.Second)
	if ref.Status != principal.StatusFailed {
		t.Fatalf("operation status = %s, want failed", ref.Status)
	}
	att := f.attempt(t)
	if att.Status != tasks.AttemptFailed || att.FailureSummary != "reason=driver_error effects=uncertain" {
		t.Fatalf("attempt = %s %q", att.Status, att.FailureSummary)
	}
	if att.CandidateCommit != "" {
		t.Errorf("failed attempt has candidate %s", att.CandidateCommit)
	}
	if n := stub.chatCalls(); n != 1 {
		t.Fatalf("chat calls = %d, want 1", n)
	}

	// Simulate a restart: release the lock, rebuild (taskexec.New runs Recover).
	if err := f.built.Close(); err != nil {
		t.Fatal(err)
	}
	f.build(t, stub.config())
	if n := stub.chatCalls(); n != 1 {
		t.Errorf("Recover re-invoked the model: chat calls = %d, want 1", n)
	}
	att = f.attempt(t)
	if att.Status != tasks.AttemptFailed || att.FailureSummary != "reason=driver_error effects=uncertain" {
		t.Errorf("after recover: %s %q", att.Status, att.FailureSummary)
	}
}

// A non-loopback URL is refused by validation before any connection is made.
func TestProbe_NonLoopbackRefusedBeforeDialing(t *testing.T) {
	cfg := selfhost.Config{OllamaURL: "http://example.com:11434", Model: testModel}
	if _, err := selfhost.Probe(context.Background(), cfg); !errors.Is(err, errs.ErrPolicyDenied) {
		t.Fatalf("err = %v, want policy denied", err)
	}
}

func TestLoadConfig_ExecutionMode(t *testing.T) {
	load := func(t *testing.T, body string, e map[string]string) *selfhost.Config {
		t.Helper()
		home := t.TempDir()
		writeCfg(t, home, body)
		cfg, err := selfhost.LoadConfig(home, env(e))
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	envMode := func(v string) map[string]string { return map[string]string{selfhost.EnvExecutionMode: v} }

	if got := load(t, `{"model":"m:1"}`, nil).ExecutionMode; got != selfhost.ExecutionModeStrict {
		t.Errorf("default = %q, want strict", got)
	}
	if got := load(t, `{"model":"m:1","execution_mode":"yolo"}`, nil).ExecutionMode; got != selfhost.ExecutionModeYolo {
		t.Errorf("file yolo = %q", got)
	}
	// The environment can never enable yolo, whatever it says.
	for _, v := range []string{"yolo", "YOLO", "unsafe_unconfined_local", "1", "true"} {
		if got := load(t, `{"model":"m:1"}`, envMode(v)).ExecutionMode; got != selfhost.ExecutionModeStrict {
			t.Errorf("env %q enabled %q", v, got)
		}
	}
	// ...but it can downgrade a file-enabled yolo.
	if got := load(t, `{"model":"m:1","execution_mode":"yolo"}`, envMode("strict")).ExecutionMode; got != selfhost.ExecutionModeStrict {
		t.Errorf("env strict did not downgrade: %q", got)
	}
	if got := load(t, `{"model":"m:1","execution_mode":"yolo"}`, envMode("yolo")).ExecutionMode; got != selfhost.ExecutionModeYolo {
		t.Errorf("unrelated env value must not change file yolo: %q", got)
	}
	// Env alone (no file) still does not enable self-host at all.
	if cfg, err := selfhost.LoadConfig(t.TempDir(), env(envMode("yolo"))); cfg != nil || err != nil {
		t.Errorf("env without file: cfg=%v err=%v", cfg, err)
	}
	home := t.TempDir()
	writeCfg(t, home, `{"model":"m:1","execution_mode":"permissive"}`)
	if _, err := selfhost.LoadConfig(home, env(nil)); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("invalid mode err = %v", err)
	}
}

func TestLoadConfig_MaxRepairRounds(t *testing.T) {
	load := func(body string) (*selfhost.Config, error) {
		home := t.TempDir()
		writeCfg(t, home, body)
		return selfhost.LoadConfig(home, env(nil))
	}
	cfg, err := load(`{"model":"m:1"}`)
	if err != nil || *cfg.MaxRepairRounds != selfhost.DefaultMaxRepairRounds {
		t.Fatalf("default = %v err %v, want %d", cfg, err, selfhost.DefaultMaxRepairRounds)
	}
	if cfg, err = load(`{"model":"m:1","max_repair_rounds":0}`); err != nil || *cfg.MaxRepairRounds != 0 {
		t.Fatalf("explicit 0 = %v err %v", cfg, err)
	}
	if cfg, err = load(`{"model":"m:1","max_repair_rounds":5}`); err != nil || *cfg.MaxRepairRounds != 5 {
		t.Fatalf("5 = %v err %v", cfg, err)
	}
	for _, bad := range []string{`{"model":"m:1","max_repair_rounds":6}`, `{"model":"m:1","max_repair_rounds":-1}`} {
		if _, err := load(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}
