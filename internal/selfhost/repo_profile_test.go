package selfhost_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/selfhost"
)

// TestRepoValidationProfileLoadsThroughProductionPath keeps DevCadence's own
// .devcadence/validation.yaml loadable and safe so it cannot rot. It loads the
// real file through the production ProfileSource, then drives the real
// Executor in strict mode: Delegate runs validateProfileSafety before the
// execution-mode gate, so a profile that is unsafe fails with
// prohibited-profile-content and a safe one is refused only with
// postcheck_requires_yolo. No subprocess runs and no model is called.
func TestRepoValidationProfileLoadsThroughProductionPath(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(selfhost.ValidationProfileRelPath)))
	if err != nil {
		t.Fatalf("repository validation profile missing: %v", err)
	}

	profile, err := selfhost.NewRepoProfileSource(repoRoot).Profile(t.Context(), "devcadence", selfhost.PostCheckProfileID)
	if err != nil {
		t.Fatalf("repository profile does not load: %v", err)
	}
	if len(profile.Checks) == 0 {
		t.Fatal("repository profile has no checks")
	}
	for _, c := range profile.Checks {
		if c.Timeout <= 0 {
			t.Errorf("check %q has no timeout", c.ID)
		}
		// The format check must be able to fail: bare `gofmt -l` exits 0 on unformatted files.
		if len(c.Argv) > 0 && c.Argv[0] == "gofmt" {
			t.Errorf("check %q uses gofmt, which exits 0 on unformatted input; use scripts/health/fmtcheck", c.ID)
		}
	}

	stub := newStubOllama(t, script(doneReply("done")))
	f := newFixture(t, postCheckFiles(string(raw)))
	f.build(t, stub.config()) // strict
	_, err = f.built.Executor.Delegate(t.Context(), f.authorizedTask())
	var coded *principal.CodedError
	if !errors.As(err, &coded) || len(coded.EvidenceRefs()) == 0 || coded.EvidenceRefs()[0] != "postcheck_requires_yolo" {
		t.Fatalf("repository profile failed the safety gate or was not refused for yolo: %v", err)
	}
	if !strings.Contains(err.Error(), "yolo") || stub.chatCalls() != 0 {
		t.Errorf("err=%v chat calls=%d", err, stub.chatCalls())
	}
}
