package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stubOllamaForCheck(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"version":"0.9.9-stub"}`) })
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"models":[{"name":"stub:7b","model":"stub:7b","digest":"sha256:`+strings.Repeat("c", 64)+`"}]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestSelfhostCheck(t *testing.T) {
	srv := stubOllamaForCheck(t)
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config", "selfhost.json"), []byte(`{"model":"stub:7b"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVCADENCE_HOME", home)
	t.Setenv("DEVCADENCE_OLLAMA_URL", srv.URL)
	t.Setenv("DEVCADENCE_OLLAMA_MODEL", "")
	c := newCLI(t)

	out := c.mustRun("selfhost", "check")
	for _, want := range []string{"0.9.9-stub", "stub:7b", "sha256:" + strings.Repeat("c", 64), "owner_local_unsigned"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	var id struct{ Version, Digest string }
	if err := json.Unmarshal([]byte(c.mustRun("selfhost", "check", "-json")), &id); err != nil || id.Version != "0.9.9-stub" {
		t.Errorf("json output: %+v %v", id, err)
	}

	t.Setenv("DEVCADENCE_OLLAMA_MODEL", "missing:1b")
	if _, _, err := c.run("selfhost", "check"); err == nil || !strings.Contains(err.Error(), "ollama pull missing:1b") {
		t.Errorf("missing model error = %v", err)
	}
	srv.Close()
	t.Setenv("DEVCADENCE_OLLAMA_MODEL", "stub:7b")
	if _, _, err := c.run("selfhost", "check"); err == nil || !strings.Contains(err.Error(), "ollama serve") {
		t.Errorf("down error = %v", err)
	}
}

func TestSelfhostCheckUnconfigured(t *testing.T) {
	t.Setenv("DEVCADENCE_HOME", t.TempDir())
	// Env alone must not enable self-host.
	t.Setenv("DEVCADENCE_OLLAMA_URL", "http://127.0.0.1:11434")
	t.Setenv("DEVCADENCE_OLLAMA_MODEL", "m:1")
	if _, _, err := newCLI(t).run("selfhost", "check"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("err = %v", err)
	}
}
