package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/schema"
	"github.com/olostan/DevCadence/internal/storage"
)

func runSchema(ctx context.Context, e *env, args []string) error {
	if len(args) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "usage: devcadence schema <list|validate>")
	}
	switch args[0] {
	case "list":
		return runSchemaList(ctx, e, args[1:])
	case "validate":
		return runSchemaValidate(ctx, e, args[1:])
	default:
		return errs.New(errs.CategoryInvalidArgument, "unknown schema subcommand %q", args[0])
	}
}

func runSchemaList(_ context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("schema list", flag.ContinueOnError)
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	set, err := schema.Default()
	if err != nil {
		return err
	}
	for _, name := range set.Names() {
		fmt.Fprintln(e.stdout, name)
	}
	return nil
}

// runSchemaValidate checks documents against a published schema.
//
// It is the operator-facing half of the twin-representation rule: the same
// check runs in tests over the fixtures, and here over any document an
// integration produces.
func runSchemaValidate(_ context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("schema validate", flag.ContinueOnError)
	name := fs.String("schema", "", "schema name, e.g. project-state (inferred from the file name when omitted)")
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	files := fs.Args()
	if len(files) == 0 {
		return errs.New(errs.CategoryInvalidArgument, "schema validate requires at least one file")
	}
	set, err := schema.Default()
	if err != nil {
		return err
	}
	failures := 0
	for _, file := range files {
		schemaName := schema.Name(*name)
		if schemaName == "" {
			inferred, err := inferSchemaName(file)
			if err != nil {
				return err
			}
			schemaName = inferred
		}
		document, err := os.ReadFile(file)
		if err != nil {
			return errs.Wrap(errs.CategoryInvalidArgument, err, "read %s", file)
		}
		if err := set.ValidateBytes(schemaName, document); err != nil {
			failures++
			fmt.Fprintf(e.stdout, "FAIL %s (%s)\n     %v\n", file, schemaName, err)
			continue
		}
		fmt.Fprintf(e.stdout, "ok   %s (%s)\n", file, schemaName)
	}
	if failures > 0 {
		return errs.New(errs.CategoryInvalidArgument, "%d document(s) failed schema validation", failures)
	}
	return nil
}

// inferSchemaName derives the schema from a fixture path of the form
// `<schema-name>.<anything>.json`, which is how fixtures/ is organised.
func inferSchemaName(file string) (schema.Name, error) {
	base := filepath.Base(file)
	// AllNames is ordered longest first, so a fixture named
	// "product-decision.valid.json" cannot be mistaken for a shorter name
	// that happens to be a prefix.
	for _, candidate := range schema.AllNames() {
		if strings.HasPrefix(base, string(candidate)+".") {
			return candidate, nil
		}
	}
	return "", errs.New(errs.CategoryInvalidArgument,
		"cannot infer a schema from %q; pass -schema", base)
}

func runMigrate(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("migrate status", flag.ContinueOnError)
	apply := fs.Bool("apply", false, "open the database read-write so that pending migrations are applied")
	if len(args) > 0 && args[0] == "status" {
		args = args[1:]
	}
	if err := parseFlags(fs, e, args); err != nil {
		return err
	}
	_, store, err := e.openService(ctx, !*apply)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	applied, err := store.AppliedMigrations(ctx)
	if err != nil {
		return err
	}
	available, err := storage.LoadMigrations()
	if err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "database %s\n", store.Path())
	for _, m := range available {
		status := "pending"
		for _, a := range applied {
			if a.Version == m.Version {
				status = "applied " + a.AppliedAt
			}
		}
		fmt.Fprintf(e.stdout, "  %04d %-24s %s\n", m.Version, m.Name, status)
	}
	return nil
}
