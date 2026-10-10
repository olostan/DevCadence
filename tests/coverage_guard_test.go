package tests

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCoverageGuardReportsRegressedBlocks runs scripts/health/coverage-guard.sh
// against a tiny throwaway module whose candidate drops coverage of 45 blocks.
// It pins the diagnostic contract: the verdict stays a regression, stderr lists
// at most 40 blocks, and the full list plus the base profile are exported.
func TestCoverageGuardReportsRegressedBlocks(t *testing.T) {
	for _, tool := range []string{"git", "go", "sh"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not available", tool)
		}
	}
	script := filepath.Join(repositoryRoot(t), "scripts", "health", "coverage-guard.sh")
	const funcs = 45

	var source, calls strings.Builder
	source.WriteString("package fixture\n\n")
	calls.WriteString("package fixture\n\nimport \"testing\"\n\nfunc TestAll(t *testing.T) {\n")
	for i := 0; i < funcs; i++ {
		fmt.Fprintf(&source, "func F%d() int { return %d }\n", i, i)
		fmt.Fprintf(&calls, "\tF%d()\n", i)
	}
	calls.WriteString("}\n")
	const noCalls = "package fixture\n\nimport \"testing\"\n\nfunc TestNone(t *testing.T) {}\n"

	write := func(dir, name, content string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	populate := func(dir, testSource string) {
		write(dir, "go.mod", "module example.test/fixture\n\ngo 1.21\n")
		write(dir, "fixture.go", source.String())
		write(dir, "fixture_test.go", testSource)
	}

	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	candidate := filepath.Join(root, "candidate")
	export := filepath.Join(root, "export")
	populate(repo, calls.String())
	populate(candidate, noCalls)

	env := []string{}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") && !strings.HasPrefix(kv, "COVERAGE_") {
			env = append(env, kv)
		}
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.test"}, args...)...)
		cmd.Dir, cmd.Env = repo, env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-q", "-m", "base")

	cmd := exec.Command("sh", script, "HEAD", candidate)
	cmd.Dir = repo
	cmd.Env = append(env, "COVERAGE_EXPORT_DIR="+export)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		t.Fatalf("guard accepted a candidate that lost coverage\n%s", stderr.String())
	}
	diag := stderr.String()
	if !strings.Contains(diag, "coverage regression:") {
		t.Fatalf("regression verdict missing from stderr:\n%s", diag)
	}
	if want := fmt.Sprintf("showing 40 of %d", funcs); !strings.Contains(diag, want) {
		t.Fatalf("stderr lacks %q:\n%s", want, diag)
	}
	if got := strings.Count(diag, "  example.test/fixture/fixture.go:"); got != 40 {
		t.Fatalf("stderr listed %d blocks, want exactly 40:\n%s", got, diag)
	}

	full, err := os.ReadFile(filepath.Join(export, "regressed-blocks.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(full), "\n"); lines != funcs {
		t.Fatalf("regressed-blocks.txt has %d lines, want %d", lines, funcs)
	}
	if !strings.Contains(string(full), "example.test/fixture/fixture.go:3.") {
		t.Fatalf("regressed-blocks.txt lacks an expected block:\n%s", full)
	}
	baseOut, err := os.ReadFile(filepath.Join(export, "base.out"))
	if err != nil || !strings.HasPrefix(string(baseOut), "mode: atomic") {
		t.Fatalf("base.out not exported correctly: %v", err)
	}
}
