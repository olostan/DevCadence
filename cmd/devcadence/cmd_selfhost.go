package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/selfhost"
)

// runSelfhost implements `devcadence selfhost check`: a read-only probe of the
// configured loopback Ollama. It never pulls a model and sends no prompt.
func runSelfhost(ctx context.Context, e *env, args []string) error {
	if len(args) == 0 || args[0] != "check" {
		fmt.Fprintln(e.stderr, "usage: devcadence selfhost check [-json]")
		return errs.New(errs.CategoryInvalidArgument, "selfhost: subcommand \"check\" is required")
	}
	fs := flag.NewFlagSet("selfhost check", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the observed identity as JSON")
	if err := parseFlags(fs, e, args[1:]); err != nil {
		return err
	}
	home := e.homeDir()
	cfg, err := selfhost.LoadConfig(home, os.Getenv)
	if err != nil {
		return err
	}
	if cfg == nil {
		return errs.New(errs.CategoryInvalidArgument,
			"selfhost is not configured: set %s (and optionally %s) or create %s",
			selfhost.EnvOllamaModel, selfhost.EnvOllamaURL, selfhost.ConfigPath(home))
	}
	id, err := selfhost.Probe(ctx, *cfg)
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(e.stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(id)
	}
	fmt.Fprintf(e.stdout, "ollama   %s (version %s)\n", id.BaseURL, id.Version)
	fmt.Fprintf(e.stdout, "endpoint %s\n", cfg.EndpointID)
	fmt.Fprintf(e.stdout, "model    %s\n", id.Model)
	fmt.Fprintf(e.stdout, "digest   %s\n", id.Digest)
	fmt.Fprintf(e.stdout, "policy   %s (no activation receipt; owner-local development only)\n", selfhost.PolicyProvenance)
	fmt.Fprintln(e.stdout, "installed models:")
	for _, m := range id.Installed {
		fmt.Fprintf(e.stdout, "  %s  %s\n", m.Name, m.Digest)
	}
	return nil
}
