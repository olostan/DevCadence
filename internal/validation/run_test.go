package validation_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/olostan/DevCadience/internal/artifacts"
	"github.com/olostan/DevCadience/internal/ids"
	"github.com/olostan/DevCadience/internal/protocol"
	"github.com/olostan/DevCadience/internal/validation"
)

func newArtifactStore(t *testing.T) *artifacts.Store {
	t.Helper()
	s, err := artifacts.NewStore(filepath.Join(t.TempDir(), "artifacts"), ids.NewSequential())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return s
}

func TestRunProfilePassAndFail(t *testing.T) {
	dir := t.TempDir()
	profile := validation.Profile{
		Name: "mixed",
		Checks: []validation.CheckSpec{
			{ID: "ok", Argv: []string{"true"}, Timeout: 5000000000},
			{ID: "bad", Argv: []string{"false"}, Timeout: 5000000000},
		},
	}
	checks, outcome, err := validation.RunProfile(context.Background(), profile, validation.RunOptions{
		Dir: dir, ProjectID: "proj-a", Artifacts: newArtifactStore(t),
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if outcome != protocol.ValidationFail {
		t.Fatalf("outcome = %s", outcome)
	}
	if len(checks) != 2 || checks[0].Status != protocol.CheckPass || checks[1].Status != protocol.CheckFail {
		t.Fatalf("checks = %+v", checks)
	}
}

func TestRunProfileMissingExecutableIsCheckError(t *testing.T) {
	dir := t.TempDir()
	profile := validation.Profile{
		Name:   "bad-exe",
		Checks: []validation.CheckSpec{{ID: "missing", Argv: []string{"devcadience-no-such-tool-xyz"}, Timeout: 5000000000}},
	}
	checks, outcome, err := validation.RunProfile(context.Background(), profile, validation.RunOptions{
		Dir: dir, ProjectID: "proj-a", Artifacts: newArtifactStore(t),
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if outcome != protocol.ValidationError {
		t.Fatalf("outcome = %s", outcome)
	}
	if checks[0].Status != protocol.CheckError {
		t.Fatalf("status = %s", checks[0].Status)
	}
}

func TestRunProfileTimeout(t *testing.T) {
	dir := t.TempDir()
	profile := validation.Profile{
		Name:   "slow",
		Checks: []validation.CheckSpec{{ID: "sleep", Argv: []string{"sleep", "5"}, Timeout: 100000000}},
	}
	checks, outcome, err := validation.RunProfile(context.Background(), profile, validation.RunOptions{
		Dir: dir, ProjectID: "proj-a", Artifacts: newArtifactStore(t),
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if outcome != protocol.ValidationError {
		t.Fatalf("outcome = %s", outcome)
	}
	if checks[0].Status != protocol.CheckError {
		t.Fatalf("status = %s", checks[0].Status)
	}
}

func TestRunProfileOutputCaptured(t *testing.T) {
	dir := t.TempDir()
	store := newArtifactStore(t)
	profile := validation.Profile{
		Name:   "echo",
		Checks: []validation.CheckSpec{{ID: "echo", Argv: []string{"sh", "-c", "echo out; echo err 1>&2"}, Timeout: 5000000000}},
	}
	checks, outcome, err := validation.RunProfile(context.Background(), profile, validation.RunOptions{
		Dir: dir, ProjectID: "proj-a", Artifacts: store,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if outcome != protocol.ValidationPass {
		t.Fatalf("outcome = %s", outcome)
	}
	if checks[0].StdoutArtifact == nil || checks[0].StderrArtifact == nil {
		t.Fatalf("artifacts not recorded: %+v", checks[0])
	}
}
