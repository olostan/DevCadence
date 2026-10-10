package taskexec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/tools"
)

func (e *toolEnv) run(args map[string]any) (string, bool) { return e.call("run_command", args) }

func decodeCommand(t *testing.T, out string) commandResult {
	t.Helper()
	var r commandResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	return r
}

func TestRunCommand_StrictRefusesBeforeAnySubprocess(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	args := map[string]any{"argv": []string{"touch", marker}}

	// Declared tool list: run_command absent, so the mediator refuses it.
	e := newToolEnv(t, []string{"src/*"}, ExecutionStrict)
	for _, n := range e.toolNames() {
		if n == "run_command" {
			t.Fatal("run_command declared in strict mode")
		}
	}
	if out, isErr := e.run(args); !isErr || !strings.Contains(out, "not declared") {
		t.Fatalf("undeclared call: isErr=%v out=%q", isErr, out)
	}

	// Even with no declaration filter the handler itself refuses in strict mode.
	scope := &tools.Scope{ProjectID: "p", WorktreePath: e.root}
	med := drivers.NewScopedToolMediator(scope)
	audit := newCommandAudit(ExecutionStrict)
	if _, ok := registerRunCommand(med, scope, process.NewRunner(), workerToolConfig{Mode: ExecutionStrict, Audit: audit}); ok {
		t.Fatal("strict mode must not return a definition to declare")
	}
	raw, _ := json.Marshal(args)
	res, _ := med.ExecuteTool(t.Context(), drivers.ToolCall{ID: "c", Name: "run_command", Arguments: raw})
	if !res.IsError || !strings.Contains(res.Content, "execution_disabled") || !strings.Contains(res.Content, "yolo") {
		t.Fatalf("handler result = %+v", res)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("subprocess ran in strict mode")
	}
	if b, _ := audit.trace(); b != nil {
		t.Errorf("strict mode recorded a trace: %s", b)
	}
}

func TestRunCommand_YoloRunsArgvAndAudits(t *testing.T) {
	e := newToolEnv(t, []string{"src/*"}, ExecutionUnsafeUnconfinedLocal)
	if !containsStr(e.toolNames(), "run_command") {
		t.Fatalf("run_command not declared in yolo mode: %v", e.toolNames())
	}

	out, isErr := e.run(map[string]any{"argv": []string{"go", "version"}})
	if isErr {
		t.Fatal(out)
	}
	r := decodeCommand(t, out)
	if r.ExitCode != 0 || r.TimedOut || r.Truncated || !r.UnsafeUnconfined || !strings.Contains(r.Output, "go version") {
		t.Fatalf("result = %+v", r)
	}

	// No shell: metacharacters are plain arguments.
	out, _ = e.run(map[string]any{"argv": []string{"printf", "%s", "hello; echo pwned | cat"}})
	if r = decodeCommand(t, out); r.Output != "hello; echo pwned | cat" {
		t.Fatalf("output = %q", r.Output)
	}

	// Non-zero exit is a result, not an error.
	out, isErr = e.run(map[string]any{"argv": []string{"go", "nosuchsubcommand"}})
	if isErr || decodeCommand(t, out).ExitCode == 0 {
		t.Fatalf("isErr=%v out=%q", isErr, out)
	}

	raw, err := e.audit.trace()
	if err != nil || raw == nil {
		t.Fatalf("trace = %s err=%v", raw, err)
	}
	var tr commandTrace
	if err := json.Unmarshal(raw, &tr); err != nil {
		t.Fatal(err)
	}
	if tr.Schema != commandTraceSchema || !tr.UnsafeUnconfined || tr.Mode != ExecutionUnsafeUnconfinedLocal || len(tr.Commands) != 3 {
		t.Fatalf("trace = %+v", tr)
	}
	sum := sha256.Sum256([]byte("hello; echo pwned | cat"))
	c := tr.Commands[1]
	if c.Seq != 2 || strings.Join(c.Argv, " ") != "printf %s hello; echo pwned | cat" || c.ExitCode != 0 ||
		c.OutputSHA256 != hex.EncodeToString(sum[:]) || !c.UnsafeUnconfined || c.Refused != "" {
		t.Errorf("record = %+v", c)
	}
	if tr.Commands[2].ExitCode == 0 {
		t.Errorf("failing command recorded exit 0: %+v", tr.Commands[2])
	}
}

func TestRunCommand_Cwd(t *testing.T) {
	e := newToolEnv(t, []string{"src/*"}, ExecutionUnsafeUnconfinedLocal)
	e.write("src/file.txt", "x", 0o644)
	for name, target := range map[string]string{"linkout": e.outside, "linkin": filepath.Join(e.root, "src")} {
		if err := os.Symlink(target, filepath.Join(e.root, name)); err != nil {
			t.Fatal(err)
		}
	}

	out, isErr := e.run(map[string]any{"argv": []string{"pwd"}, "cwd": "src/sub"})
	if isErr || strings.TrimSpace(decodeCommand(t, out).Output) != filepath.Join(e.root, "src", "sub") {
		t.Fatalf("cwd run: isErr=%v out=%q", isErr, out)
	}
	for _, cwd := range []string{"..", "../outside", "src/../../outside", e.outside, "linkout", "linkin", ".git", "src/missing", "src/file.txt"} {
		if out, isErr := e.run(map[string]any{"argv": []string{"pwd"}, "cwd": cwd}); !isErr {
			t.Errorf("cwd %q accepted: %s", cwd, out)
		}
	}
}

func TestRunCommand_DeniedAndInvalid(t *testing.T) {
	e := newToolEnv(t, []string{"src/*"}, ExecutionUnsafeUnconfinedLocal)
	marker := filepath.Join(e.root, "pwned")
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"git", map[string]any{"argv": []string{"git", "status"}}, "denied_executable"},
		{"absolute git", map[string]any{"argv": []string{"/usr/bin/git", "push"}}, "denied_executable"},
		{"gh", map[string]any{"argv": []string{"gh", "pr", "list"}}, "denied_executable"},
		{"ssh", map[string]any{"argv": []string{"ssh", "host"}}, "denied_executable"},
		{"scp", map[string]any{"argv": []string{"scp", "a", "b"}}, "denied_executable"},
		{"curl upper-case", map[string]any{"argv": []string{"CURL", "http://x"}}, "denied_executable"},
		{"wget", map[string]any{"argv": []string{"wget", "http://x"}}, "denied_executable"},
		{"sh", map[string]any{"argv": []string{"sh", "-c", "touch " + marker}}, "denied_executable"},
		{"bash", map[string]any{"argv": []string{"bash", "-c", "touch " + marker}}, "denied_executable"},
		{"zsh", map[string]any{"argv": []string{"zsh", "-c", "x"}}, "denied_executable"},
		{"fish", map[string]any{"argv": []string{"fish", "-c", "x"}}, "denied_executable"},
		{"cmd.exe", map[string]any{"argv": []string{"cmd.exe", "/c", "x"}}, "denied_executable"},
		{"powershell", map[string]any{"argv": []string{"powershell", "x"}}, "denied_executable"},
		{"env wrapper", map[string]any{"argv": []string{"env", "sh"}}, "denied_executable"},
		{"relative path", map[string]any{"argv": []string{"./tool"}}, "denied_executable"},
		{"relative subdir path", map[string]any{"argv": []string{"bin/tool"}}, "denied_executable"},
		{"empty argv", map[string]any{"argv": []string{}}, "invalid_argv"},
		{"blank program", map[string]any{"argv": []string{" "}}, "invalid_argv"},
		{"timeout too large", map[string]any{"argv": []string{"pwd"}, "timeout_seconds": 601}, "invalid_timeout"},
		{"timeout negative", map[string]any{"argv": []string{"pwd"}, "timeout_seconds": -1}, "invalid_timeout"},
		{"unknown program", map[string]any{"argv": []string{"definitely-not-a-program-xyz"}}, "not_started"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, isErr := e.run(tc.args)
			if !isErr || !strings.Contains(out, tc.want) {
				t.Fatalf("isErr=%v out=%q, want %q", isErr, out, tc.want)
			}
		})
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("denied command executed")
	}
	raw, _ := e.audit.trace()
	var tr commandTrace
	if err := json.Unmarshal(raw, &tr); err != nil || len(tr.Commands) != len(tests) {
		t.Fatalf("refusals not audited: %d records, err=%v", len(tr.Commands), err)
	}
	for _, c := range tr.Commands {
		if c.Refused == "" || !c.UnsafeUnconfined {
			t.Errorf("record = %+v", c)
		}
	}
}

func TestRunCommand_Timeout(t *testing.T) {
	e := newToolEnv(t, []string{"src/*"}, ExecutionUnsafeUnconfinedLocal)
	out, isErr := e.run(map[string]any{"argv": []string{"sleep", "30"}, "timeout_seconds": 1})
	if isErr {
		t.Fatal(out)
	}
	r := decodeCommand(t, out)
	if !r.TimedOut || r.ExitCode == 0 || r.DurationMS >= 20000 {
		t.Fatalf("result = %+v", r)
	}
	raw, _ := e.audit.trace()
	if !strings.Contains(string(raw), `"timed_out":true`) {
		t.Errorf("trace lacks timed_out: %s", raw)
	}
}

func TestRunCommand_OutputTruncatedHeadAndTail(t *testing.T) {
	e := newToolEnv(t, []string{"src/*"}, ExecutionUnsafeUnconfinedLocal)
	out, isErr := e.run(map[string]any{"argv": []string{"seq", "1", "20000"}})
	if isErr {
		t.Fatal(out)
	}
	r := decodeCommand(t, out)
	if !r.Truncated || r.OutputBytes < 100000 || len(r.Output) > commandOutputHead+commandOutputTail+200 {
		t.Fatalf("truncated=%v bytes=%d len=%d", r.Truncated, r.OutputBytes, len(r.Output))
	}
	if !strings.HasPrefix(r.Output, "1\n2\n") || !strings.HasSuffix(r.Output, "19999\n20000\n") || !strings.Contains(r.Output, "output truncated") {
		t.Errorf("head/tail/marker missing: %q ... %q", r.Output[:20], r.Output[len(r.Output)-20:])
	}
}

func TestRunCommand_EnvironmentHasNoHostSecrets(t *testing.T) {
	secrets := map[string]string{
		"DEVCADENCE_FAKE_SECRET": "s3cr3t-value", "GITHUB_TOKEN": "ghp_faketoken", "SSH_AUTH_SOCK": "/tmp/fake-agent.sock",
		"ANTHROPIC_API_KEY": "sk-fake", "AWS_SECRET_ACCESS_KEY": "aws-fake", "GOFLAGS": "-mod=mod",
	}
	for k, v := range secrets {
		t.Setenv(k, v)
	}
	e := newToolEnv(t, []string{"src/*"}, ExecutionUnsafeUnconfinedLocal)
	out, isErr := e.run(map[string]any{"argv": []string{"printenv"}})
	if isErr {
		t.Fatal(out)
	}
	got := decodeCommand(t, out).Output
	for k, v := range secrets {
		if strings.Contains(got, k+"=") || strings.Contains(got, v) {
			t.Errorf("host variable %s leaked into command environment:\n%s", k, got)
		}
	}
	if !strings.Contains(got, "PATH=") {
		t.Errorf("PATH missing from minimal environment:\n%s", got)
	}
}

func TestWorkerToolDeclarations(t *testing.T) {
	names, declared := workerToolDeclarations(ExecutionStrict)
	if containsStr(names, "run_command") || len(names) != len(declared) || !containsStr(names, "apply_patch") {
		t.Fatalf("strict: %v", names)
	}
	names, declared = workerToolDeclarations(ExecutionUnsafeUnconfinedLocal)
	if !containsStr(names, "run_command") || len(names) != len(declared) {
		t.Fatalf("yolo: %v", names)
	}
	if ExecutionMode("").normalized() != ExecutionStrict {
		t.Error("zero ExecutionMode must be strict")
	}
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func hostGoEnv(t *testing.T) map[string]string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOCACHE", "GOMODCACHE", "GOPATH").Output()
	if err != nil {
		t.Skipf("go env: %v", err)
	}
	l := strings.Split(strings.TrimSpace(string(out)), "\n")
	return map[string]string{"GOCACHE": l[0], "GOMODCACHE": l[1], "GOPATH": l[2]}
}

func TestRunCommand_ScratchHomeHidesHostHomeAndGoStillWorks(t *testing.T) {
	for k, v := range hostGoEnv(t) { // keep the real caches when HOME is faked below
		t.Setenv(k, v)
	}
	realHome := t.TempDir()
	cred := filepath.Join(realHome, ".aws", "credentials")
	if err := os.MkdirAll(filepath.Dir(cred), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cred, []byte("aws_secret_access_key=fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", realHome)

	e := newToolEnv(t, []string{"src/*"}, ExecutionUnsafeUnconfinedLocal)
	out, isErr := e.run(map[string]any{"argv": []string{"printenv"}})
	if isErr {
		t.Fatal(out)
	}
	env := decodeCommand(t, out).Output
	if strings.Contains(env, realHome) || !strings.Contains(env, "HOME="+filepath.Join(filepath.Dir(e.root), "scratch-home")) {
		t.Fatalf("HOME not scratch:\n%s", env)
	}
	for _, want := range []string{"GOTOOLCHAIN=local", "GIT_CONFIG_NOSYSTEM=1", "GOCACHE="} {
		if !strings.Contains(env, want) {
			t.Errorf("env lacks %s:\n%s", want, env)
		}
	}
	if strings.Contains(env, "GOPROXY=off") {
		t.Error("GOPROXY must not be forced off")
	}
	// The fake credential file is not discoverable through $HOME.
	out, _ = e.run(map[string]any{"argv": []string{"ls", "-A", filepath.Join(filepath.Dir(e.root), "scratch-home")}})
	if strings.Contains(out, ".aws") {
		t.Errorf("credential dir visible via HOME: %s", out)
	}

	// go test still works with the scratch HOME.
	e.write("go.mod", "module example.com/m\n\ngo 1.21\n", 0o644)
	e.write("m_test.go", "package m\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n", 0o644)
	out, isErr = e.run(map[string]any{"argv": []string{"go", "test", "./..."}, "timeout_seconds": 300})
	if r := decodeCommand(t, out); isErr || r.ExitCode != 0 {
		t.Fatalf("go test failed: %s", out)
	}
}

func TestRunCommand_ExecutableResolution(t *testing.T) {
	e := newToolEnv(t, []string{"src/*"}, ExecutionUnsafeUnconfinedLocal)
	script := e.write("src/tool.sh", "#!/bin/true\n", 0o755)
	if err := os.Symlink(script, filepath.Join(e.root, "src", "alias")); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	cases := map[string][]string{
		"script written in worktree":      {script},
		"symlink in worktree to worktree": {filepath.Join(e.root, "src", "alias")},
		"relative path":                   {"src/tool.sh"},
	}
	if git, err := exec.LookPath("git"); err == nil {
		link := filepath.Join(tmp, "innocent")
		if err := os.Symlink(git, link); err != nil {
			t.Fatal(err)
		}
		cases["symlink named innocent to git"] = []string{link, "--version"}
		inWT := filepath.Join(e.root, "src", "g")
		if err := os.Symlink(git, inWT); err != nil {
			t.Fatal(err)
		}
		cases["symlink in worktree to git"] = []string{inWT, "--version"}
	}
	for name, argv := range cases {
		t.Run(name, func(t *testing.T) {
			out, isErr := e.run(map[string]any{"argv": argv})
			if !isErr || !strings.Contains(out, "denied_executable") {
				t.Fatalf("isErr=%v out=%q", isErr, out)
			}
		})
	}
}
