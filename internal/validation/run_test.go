package validation_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/artifacts"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/validation"
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

// TestRunProfileDefaultsMatchJoinedArgvAndRejectCollisions proves RunProfile
// applies the same joined-argv default ID (and the same duplicate-id
// rejection) as LoadProfiles' buildProfiles, for a Profile constructed
// programmatically rather than loaded from YAML. Two checks that would
// otherwise both default to the executable name alone ("true") must not
// silently collapse onto one ambiguous CheckResult ID.
func TestRunProfileDefaultsMatchJoinedArgvAndRejectCollisions(t *testing.T) {
	dir := t.TempDir()
	profile := validation.Profile{
		Name: "dup",
		Checks: []validation.CheckSpec{
			{Argv: []string{"true", "a"}, Timeout: 5000000000},
			{Argv: []string{"true", "b"}, Timeout: 5000000000},
		},
	}
	_, _, err := validation.RunProfile(context.Background(), profile, validation.RunOptions{
		Dir: dir, ProjectID: "proj-a", Artifacts: newArtifactStore(t),
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	collidingProfile := validation.Profile{
		Name: "dup-collide",
		Checks: []validation.CheckSpec{
			{Argv: []string{"true"}, Timeout: 5000000000},
			{Argv: []string{"true"}, Timeout: 5000000000},
		},
	}
	_, _, err = validation.RunProfile(context.Background(), collidingProfile, validation.RunOptions{
		Dir: dir, ProjectID: "proj-a", Artifacts: newArtifactStore(t),
	})
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category = %v, want a refusal for two checks defaulting to the same id (err=%v)", errs.CategoryOf(err), err)
	}
}

func TestRunProfileMissingExecutableIsCheckError(t *testing.T) {
	dir := t.TempDir()
	profile := validation.Profile{
		Name:   "bad-exe",
		Checks: []validation.CheckSpec{{ID: "missing", Argv: []string{"devcadence-no-such-tool-xyz"}, Timeout: 5000000000}},
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

func TestRunProfileCheckDirExecution(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "submodule")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	profile := validation.Profile{
		Name: "sub",
		Checks: []validation.CheckSpec{
			{ID: "check-pwd", Argv: []string{"pwd"}, Timeout: 5000000000, Dir: "submodule"},
		},
	}
	checks, outcome, err := validation.RunProfile(context.Background(), profile, validation.RunOptions{
		Dir: dir, ProjectID: "proj-a", Artifacts: newArtifactStore(t),
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if outcome != protocol.ValidationPass {
		t.Fatalf("outcome = %s, want pass", outcome)
	}
	resolvedSubDir, err := filepath.EvalSymlinks(subDir)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	if checks[0].WorkingDirectory == nil || *checks[0].WorkingDirectory != resolvedSubDir {
		t.Errorf("working dir = %v, want %s", *checks[0].WorkingDirectory, resolvedSubDir)
	}
}

func TestRunProfileCheckDirEscapeRejected(t *testing.T) {
	dir := t.TempDir()
	profile := validation.Profile{
		Name: "escape",
		Checks: []validation.CheckSpec{
			{ID: "bad-dir", Argv: []string{"true"}, Timeout: 5000000000, Dir: "../outside"},
		},
	}
	checks, outcome, err := validation.RunProfile(context.Background(), profile, validation.RunOptions{
		Dir: dir, ProjectID: "proj-a", Artifacts: newArtifactStore(t),
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if outcome != protocol.ValidationError {
		t.Fatalf("outcome = %s, want error", outcome)
	}
	if checks[0].Status != protocol.CheckError {
		t.Fatalf("status = %s, want CheckError", checks[0].Status)
	}
}

func TestRunProfileModuleResolution(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "packages", "core")
	if err := os.MkdirAll(modDir, 0755); err != nil {
		t.Fatalf("mkdir core: %v", err)
	}

	modules := []protocol.ModuleDefinition{
		{
			ID:       "pkg-core",
			Path:     "packages/core",
			Language: "go",
		},
	}

	profile := validation.Profile{
		Name: "mod-test",
		Checks: []validation.CheckSpec{
			{ID: "check-mod", ModuleID: "pkg-core", Argv: []string{"pwd"}, Timeout: 5000000000},
		},
	}

	checks, outcome, err := validation.RunProfile(context.Background(), profile, validation.RunOptions{
		Dir:       dir,
		ProjectID: "proj-a",
		Artifacts: newArtifactStore(t),
		Modules:   modules,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if outcome != protocol.ValidationPass {
		t.Fatalf("outcome = %s, want pass", outcome)
	}

	resolvedModDir, err := filepath.EvalSymlinks(modDir)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	if checks[0].WorkingDirectory == nil || *checks[0].WorkingDirectory != resolvedModDir {
		t.Errorf("working dir = %v, want %s", *checks[0].WorkingDirectory, resolvedModDir)
	}
}

func TestRunProfileUnknownModuleRejected(t *testing.T) {
	dir := t.TempDir()
	profile := validation.Profile{
		Name: "mod-reject",
		Checks: []validation.CheckSpec{
			{ID: "check-mod", ModuleID: "nonexistent-module", Argv: []string{"pwd"}, Timeout: 5000000000},
		},
	}

	checks, outcome, err := validation.RunProfile(context.Background(), profile, validation.RunOptions{
		Dir:       dir,
		ProjectID: "proj-a",
		Artifacts: newArtifactStore(t),
		Modules: []protocol.ModuleDefinition{
			{ID: "pkg-core", Path: "packages/core", Language: "go"},
		},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if outcome != protocol.ValidationError {
		t.Fatalf("outcome = %s, want ValidationError", outcome)
	}
	if len(checks) == 0 || checks[0].Status != protocol.CheckError {
		t.Fatalf("expected CheckError for unknown module, got %+v", checks)
	}
	if checks[0].Summary == nil || !strings.Contains(*checks[0].Summary, "not found in project modules catalog") {
		t.Errorf("expected module not found message, got %v", checks[0].Summary)
	}
}

