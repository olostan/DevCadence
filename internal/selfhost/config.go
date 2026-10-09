// Package selfhost composes the existing native task executor with a real
// loopback Ollama provider for the Self-Host Alpha (SH1-1).
//
// Configuration is user-level only: a JSON file under the DevCadence home
// directory and/or environment variables. It is never read from the project
// repository, so a repository cannot grant itself an execution endpoint.
package selfhost

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/olostan/DevCadence/internal/cognition/sessionclients"
	"github.com/olostan/DevCadence/internal/errs"
)

// Environment variables that configure (and may override) the config file.
const (
	EnvOllamaURL   = "DEVCADENCE_OLLAMA_URL"
	EnvOllamaModel = "DEVCADENCE_OLLAMA_MODEL"
)

// Defaults.
const (
	DefaultEndpointID    = "ollama-local"
	DefaultContextTokens = 32768
	configFileName       = "selfhost.json"
)

// Config is the user-level self-host configuration for ONE loopback Ollama
// endpoint and model.
type Config struct {
	// OllamaURL is the loopback base URL; defaults to Ollama's standard address.
	OllamaURL string `json:"ollama_url,omitempty"`
	// Model is the installed Ollama model name (for example "qwen2.5-coder:7b").
	Model string `json:"model"`
	// EndpointID is the stable id recorded in provenance; default "ollama-local".
	EndpointID string `json:"endpoint_id,omitempty"`
	// ContextTokens is the operating context target used for prompt budgeting.
	ContextTokens int `json:"context_tokens,omitempty"`
}

// ConfigPath returns the config file location under the DevCadence home.
func ConfigPath(home string) string {
	return filepath.Join(home, "config", configFileName)
}

// LoadConfig reads <home>/config/selfhost.json (if present) and applies the
// environment overrides. It returns (nil, nil) when self-host is not
// configured at all (no file and no DEVCADENCE_OLLAMA_MODEL), so callers keep
// their existing behavior. Any partial or invalid configuration is an error.
func LoadConfig(home string, getenv func(string) string) (*Config, error) {
	if home == "" || !filepath.IsAbs(home) {
		return nil, errs.New(errs.CategoryInvalidArgument, "selfhost: DevCadence home must be an absolute path")
	}
	var cfg Config
	present := false
	path := ConfigPath(home)
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		present = true
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&cfg); err != nil {
			return nil, errs.Wrap(errs.CategoryInvalidArgument, err,
				"selfhost: %s is not valid (expected keys ollama_url, model, endpoint_id, context_tokens)", path)
		}
		if _, err := dec.Token(); !errors.Is(err, io.EOF) {
			return nil, errs.New(errs.CategoryInvalidArgument, "selfhost: %s has trailing content", path)
		}
	case errors.Is(err, fs.ErrNotExist):
		// Not configured by file.
	default:
		return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "selfhost: cannot read %s", path)
	}
	if v := strings.TrimSpace(getenv(EnvOllamaModel)); v != "" {
		cfg.Model = v
		present = true
	}
	if v := strings.TrimSpace(getenv(EnvOllamaURL)); v != "" {
		cfg.OllamaURL = v
		if !present {
			return nil, errs.New(errs.CategoryInvalidArgument,
				"selfhost: %s is set but no model is configured; set %s (or \"model\" in %s)", EnvOllamaURL, EnvOllamaModel, path)
		}
	}
	if !present {
		return nil, nil
	}
	if err := cfg.normalize(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// normalize applies defaults and validates the configuration.
func (c *Config) normalize() error {
	c.Model = strings.TrimSpace(c.Model)
	if c.Model == "" {
		return errs.New(errs.CategoryInvalidArgument,
			"selfhost: model is required; set %s or \"model\" in the selfhost config (then run `ollama pull <model>`)", EnvOllamaModel)
	}
	if strings.ContainsAny(c.Model, " \t\r\n") {
		return errs.New(errs.CategoryInvalidArgument, "selfhost: model name %q contains whitespace", c.Model)
	}
	if !strings.Contains(c.Model, ":") {
		c.Model += ":latest" // Ollama's implicit tag; /api/tags lists it explicitly.
	}
	c.OllamaURL = strings.TrimSpace(c.OllamaURL)
	if c.OllamaURL == "" {
		c.OllamaURL = sessionclients.DefaultLoopbackBaseURL
	}
	if err := sessionclients.ValidateLoopbackURL(c.OllamaURL); err != nil {
		return err
	}
	c.OllamaURL = strings.TrimSuffix(c.OllamaURL, "/")
	if strings.TrimSpace(c.EndpointID) == "" {
		c.EndpointID = DefaultEndpointID
	}
	if strings.ContainsAny(c.EndpointID, " \t\r\n/\\") {
		return errs.New(errs.CategoryInvalidArgument, "selfhost: endpoint_id %q must not contain whitespace or slashes", c.EndpointID)
	}
	if c.ContextTokens == 0 {
		c.ContextTokens = DefaultContextTokens
	}
	if c.ContextTokens < 8192 {
		return errs.New(errs.CategoryInvalidArgument, "selfhost: context_tokens must be >= 8192, got %d", c.ContextTokens)
	}
	return nil
}
