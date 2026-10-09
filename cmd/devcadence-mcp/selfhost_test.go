package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/mcpadapter"
	"github.com/olostan/DevCadence/internal/observability"
	"github.com/olostan/DevCadence/internal/principal/facade"
	"github.com/olostan/DevCadence/internal/testsupport"
)

func stubOllama(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"version":"0.0.0-stub"}`) })
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"models":[{"name":"stub:1b","model":"stub:1b","digest":"sha256:`+strings.Repeat("b", 64)+`"}]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestSelfhostTasks(t *testing.T) {
	h := testsupport.NewHarness(t)
	repo := testsupport.NewGitRepo(t)
	registry, err := facade.NewOperationRegistry("t")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(registry.Close)
	home := t.TempDir()
	srv := stubOllama(t)
	env := map[string]string{}
	writeSelfhostFile := func(body string) {
		if err := os.MkdirAll(filepath.Join(home, "config"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, "config", "selfhost.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	in := mcpadapter.TaskPortInput{
		ProjectID: "example", Home: home, RepoPath: repo.Path, ControlPlane: h.Service, Operations: registry,
		Logger: observability.NewLogger(observability.Options{}),
		Getenv: func(k string) string { return env[k] },
	}

	// Not configured: no executor, no error (launch behavior unchanged).
	if exec, closeFn, err := selfhostTasks(context.Background(), in); exec != nil || closeFn != nil || err != nil {
		t.Fatalf("unconfigured: %v %v %v", exec, closeFn != nil, err)
	}

	// Environment variables alone never enable the executor.
	env["DEVCADENCE_OLLAMA_MODEL"], env["DEVCADENCE_OLLAMA_URL"] = "stub:1b", srv.URL
	if exec, closeFn, err := selfhostTasks(context.Background(), in); exec != nil || closeFn != nil || err != nil {
		t.Fatalf("env-only: %v %v %v", exec, closeFn != nil, err)
	}
	writeSelfhostFile(`{"model":"stub:1b"}`)
	exec, closeFn, err := selfhostTasks(context.Background(), in)
	if err != nil || exec == nil || closeFn == nil {
		t.Fatalf("configured: exec=%v err=%v", exec, err)
	}
	if err := closeFn(); err != nil {
		t.Fatal(err)
	}

	// Configured but no registered repository: fail closed.
	noRepo := in
	noRepo.RepoPath = ""
	if _, _, err := selfhostTasks(context.Background(), noRepo); err == nil || !strings.Contains(err.Error(), "no registered repository") {
		t.Errorf("no repo err = %v", err)
	}

	// Configured but Ollama down: actionable launch error.
	srv.Close()
	if _, _, err := selfhostTasks(context.Background(), in); err == nil || !strings.Contains(err.Error(), "ollama serve") {
		t.Errorf("down err = %v", err)
	}
}
