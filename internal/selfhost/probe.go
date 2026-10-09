package selfhost

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/cognition/sessionclients"
	"github.com/olostan/DevCadence/internal/errs"
)

const probeTimeout = 15 * time.Second

// InstalledModel is one model reported by Ollama's /api/tags.
type InstalledModel struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
}

// Identity is what the preflight probe observed. Nothing is inferred: the
// version and digest are exactly what Ollama reported.
type Identity struct {
	BaseURL   string           `json:"base_url"`
	Version   string           `json:"version"`
	Model     string           `json:"model"`
	Digest    string           `json:"digest"`
	Installed []InstalledModel `json:"installed"`
}

// Probe contacts the configured loopback Ollama and verifies that the
// configured model is installed with a usable sha256 digest. It never pulls
// models and never contacts a non-loopback address.
func Probe(ctx context.Context, cfg Config) (Identity, error) {
	if err := cfg.normalize(); err != nil {
		return Identity{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	client := sessionclients.NewLoopbackHTTPClient()

	var ver struct {
		Version string `json:"version"`
	}
	if err := getJSON(ctx, client, cfg.OllamaURL+"/api/version", &ver); err != nil {
		return Identity{}, errs.Wrap(errs.CategoryModelUnavailable, err,
			"Ollama is not reachable at %s: start it with `ollama serve`, or set %s to the correct loopback URL",
			cfg.OllamaURL, EnvOllamaURL)
	}
	var tags struct {
		Models []struct {
			Name   string `json:"name"`
			Model  string `json:"model"`
			Digest string `json:"digest"`
		} `json:"models"`
	}
	if err := getJSON(ctx, client, cfg.OllamaURL+"/api/tags", &tags); err != nil {
		return Identity{}, errs.Wrap(errs.CategoryModelUnavailable, err,
			"cannot list installed models from Ollama at %s", cfg.OllamaURL)
	}
	id := Identity{BaseURL: cfg.OllamaURL, Version: strings.TrimSpace(ver.Version), Model: cfg.Model}
	var found *InstalledModel
	for _, m := range tags.Models {
		im := InstalledModel{Name: m.Name, Digest: m.Digest}
		if im.Name == "" {
			im.Name = m.Model
		}
		id.Installed = append(id.Installed, im)
		if (m.Name == cfg.Model || m.Model == cfg.Model) && found == nil {
			cp := im
			found = &cp
		}
	}
	if id.Version == "" {
		return Identity{}, errs.New(errs.CategoryModelUnavailable,
			"Ollama at %s did not report a version; refusing to bind an unidentified runtime", cfg.OllamaURL)
	}
	if found == nil {
		names := make([]string, 0, len(id.Installed))
		for _, m := range id.Installed {
			names = append(names, m.Name)
		}
		return Identity{}, errs.New(errs.CategoryNotFound,
			"model %q is not installed in Ollama at %s: run `ollama pull %s` (installed: %s)",
			cfg.Model, cfg.OllamaURL, cfg.Model, listOrNone(names))
	}
	hex := strings.TrimPrefix(strings.TrimSpace(found.Digest), "sha256:")
	if len(hex) != 64 || !isHex(hex) {
		return Identity{}, errs.New(errs.CategoryModelUnavailable,
			"Ollama reports no valid sha256 digest for model %q; re-run `ollama pull %s`", cfg.Model, cfg.Model)
	}
	id.Digest = "sha256:" + hex
	return id, nil
}

func listOrNone(s []string) string {
	if len(s) == 0 {
		return "none"
	}
	return strings.Join(s, ", ")
}

func isHex(s string) bool {
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func getJSON(ctx context.Context, client *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned status %d", req.URL.Path, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, sessionclients.MaxResponseBytes))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
