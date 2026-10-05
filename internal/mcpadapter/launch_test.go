package mcpadapter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/mcpadapter"
	"github.com/olostan/DevCadence/internal/storage"
	"github.com/olostan/DevCadence/internal/testsupport"
)

// launchHome builds DEVCADENCE_HOME with a project database and a protected
// binding. repo, when set, registers a Git repository for drift detection.
func launchHome(t *testing.T, binding string, repo *testsupport.GitRepo) (home string, getenv func(string) string) {
	t.Helper()
	home, cfg := protectedHome(t)
	writeBinding(t, cfg, binding)
	ctx := context.Background()
	store, err := storage.Open(ctx, storage.Config{Path: filepath.Join(home, "state", "control-plane.db"), Clock: clock.System()})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := controlplane.New(controlplane.Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	init := controlplane.InitProjectInput{ProjectID: "example", MilestoneID: "M1", MilestoneTitle: "Domain core"}
	if repo != nil {
		init.RepositoryPath, init.AcceptedCommit = repo.Path, repo.Head()
	}
	if _, err := svc.InitProject(ctx, init); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{mcpadapter.EnvProjectID: "example", mcpadapter.EnvHome: home}
	return home, func(k string) string { return env[k] }
}

type launched struct {
	cs     *mcp.ClientSession
	done   chan int
	stderr *bytes.Buffer
	cancel context.CancelFunc
}

func (l *launched) stop(t *testing.T) int {
	t.Helper()
	l.cancel()
	select {
	case code := <-l.done:
		return code
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not stop")
		return -1
	}
}

func startLaunch(t *testing.T, getenv func(string) string) *launched {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	l := &launched{done: make(chan int, 1), stderr: &bytes.Buffer{}, cancel: cancel}
	go func() { l.done <- mcpadapter.Launch(ctx, nil, getenv, serverTransport, l.stderr) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "1"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		cancel()
		t.Fatalf("connect: %v (stderr %s)", err, l.stderr.String())
	}
	l.cs = cs
	t.Cleanup(func() { cancel(); _ = cs.Close() })
	return l
}

func projectState(t *testing.T, cs *mcp.ClientSession) (*mcp.CallToolResult, map[string]any) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "project_state", Arguments: map[string]any{
		"meta": map[string]any{"schema_version": "1.0", "project_id": "example", "correlation_id": "c"}}})
	if err != nil {
		t.Fatal(err)
	}
	return res, structured(t, res)
}

// syncBuffer is a bytes.Buffer safe for the exec copier goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestLaunchRejectsArgumentsAndBadEnvironment(t *testing.T) {
	var stderr bytes.Buffer
	if code := mcpadapter.Launch(context.Background(), []string{"--http"}, func(string) string { return "" }, &mcp.StdioTransport{}, &stderr); code != mcpadapter.ExitUsage {
		t.Fatalf("exit %d for an argument", code)
	}
	if !strings.Contains(stderr.String(), "no arguments") {
		t.Fatalf("stderr %q", stderr.String())
	}
	_, getenv := launchHome(t, goodBinding, nil)
	for name, env := range map[string]map[string]string{
		"no project":         {mcpadapter.EnvHome: getenv(mcpadapter.EnvHome)},
		"relative home":      {mcpadapter.EnvProjectID: "example", mcpadapter.EnvHome: "relative"},
		"no home":            {mcpadapter.EnvProjectID: "example"},
		"relative binding":   {mcpadapter.EnvProjectID: "example", mcpadapter.EnvHome: getenv(mcpadapter.EnvHome), mcpadapter.EnvBinding: "rel.json"},
		"other project":      {mcpadapter.EnvProjectID: "elsewhere", mcpadapter.EnvHome: getenv(mcpadapter.EnvHome)},
		"binding is missing": {mcpadapter.EnvProjectID: "example", mcpadapter.EnvHome: getenv(mcpadapter.EnvHome), mcpadapter.EnvBinding: filepath.Join(getenv(mcpadapter.EnvHome), "nope.json")},
	} {
		var out bytes.Buffer
		serverTransport, _ := mcp.NewInMemoryTransports()
		code := mcpadapter.Launch(context.Background(), nil, func(k string) string { return env[k] }, serverTransport, &out)
		if code != mcpadapter.ExitFailed {
			t.Errorf("%s: exit %d", name, code)
		}
		if strings.Contains(out.String(), getenv(mcpadapter.EnvHome)) {
			t.Errorf("%s: the home path leaked into stderr: %s", name, out.String())
		}
	}
	// A database that does not exist is never created or discovered.
	home, cfg := protectedHome(t)
	writeBinding(t, cfg, goodBinding)
	serverTransport, _ := mcp.NewInMemoryTransports()
	code := mcpadapter.Launch(context.Background(), nil, func(k string) string {
		return map[string]string{mcpadapter.EnvProjectID: "example", mcpadapter.EnvHome: home}[k]
	}, serverTransport, &stderr)
	if code != mcpadapter.ExitFailed {
		t.Fatalf("exit %d without a database", code)
	}
	if _, err := os.Stat(filepath.Join(home, "state", "control-plane.db")); err == nil {
		t.Fatal("launch created a database")
	}
}

// A1/R5: a source-free working directory is enough.
func TestA1_LaunchWorksFromASourceFreeDirectory(t *testing.T) {
	_, getenv := launchHome(t, goodBinding, nil)
	t.Chdir(t.TempDir())
	l := startLaunch(t, getenv)
	res, state := projectState(t, l.cs)
	if res.IsError || state["state_revision"] == "" {
		t.Fatalf("project_state failed: %v", state)
	}
	list, err := l.cs.ListTools(context.Background(), nil)
	if err != nil || len(list.Tools) != 11 {
		t.Fatalf("tools: %v %v", len(list.Tools), err)
	}
	if code := l.stop(t); code != mcpadapter.ExitOK {
		t.Fatalf("exit %d (stderr %s)", code, l.stderr.String())
	}
}

// A17: editing the binding after launch grants nothing and stops further actions.
func TestA17_LaunchedProcessIgnoresBindingEdits(t *testing.T) {
	home, getenv := launchHome(t, strings.Replace(goodBinding, `"accept"`, `"task_status"`, 1), nil)
	l := startLaunch(t, getenv)
	if res, _ := projectState(t, l.cs); res.IsError {
		t.Fatal("the bound call was refused")
	}
	path := filepath.Join(home, "config", "principal-binding.json")
	widened := strings.Replace(goodBinding, `"accept"`, `"task_status", "delegate"`, 1)
	if err := os.WriteFile(path, []byte(widened), 0o600); err != nil {
		t.Fatal(err)
	}
	res, state := projectState(t, l.cs)
	if !res.IsError {
		t.Fatalf("a drifted binding still served a call: %v", state)
	}
	if e := state["error"].(map[string]any); e["code"] != "POLICY_DENIED" {
		t.Fatalf("code %v", e["code"])
	}
	del, err := l.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "delegate", Arguments: map[string]any{}})
	if err != nil || !del.IsError {
		t.Fatal("a newly written grant was usable")
	}
	l.stop(t)
}

// Drift detection through the real composition: the project's repository is
// registered from its journal and re-read on every call.
func TestLaunchDetectsRepositoryChangedOutsideDevCadence(t *testing.T) {
	repo := testsupport.NewGitRepo(t)
	_, getenv := launchHome(t, goodBinding, repo)
	l := startLaunch(t, getenv)
	observation := func() map[string]any {
		_, state := projectState(t, l.cs)
		result := state["result"].(map[string]any)
		obs, _ := result["repository"].(map[string]any)
		if obs == nil {
			t.Fatalf("no repository observation: %v", state)
		}
		return obs
	}
	if obs := observation(); obs["drifted"] != false || obs["refresh_required"] != false {
		t.Fatalf("spurious drift: %v", obs)
	}
	repo.WriteFile("README.md", "# changed behind DevCadence's back\n")
	if obs := observation(); obs["drifted"] != true || obs["refresh_required"] != true {
		t.Fatalf("drift was not detected: %v", obs)
	}
	repo.Commit("outside commit")
	obs := observation()
	if obs["drifted"] != true || obs["head_commit"] != repo.Head() {
		t.Fatalf("a moved HEAD was not detected: %v", obs)
	}
	l.stop(t)
}

func TestLaunchWithUnopenableRepositoryFailsClosedNotConnectivity(t *testing.T) {
	repo := testsupport.NewGitRepo(t)
	_, getenv := launchHome(t, goodBinding, repo)
	if err := os.RemoveAll(filepath.Join(repo.Path, ".git")); err != nil {
		t.Fatal(err)
	}
	l := startLaunch(t, getenv)
	res, state := projectState(t, l.cs)
	if res.IsError {
		t.Fatalf("connectivity must survive a broken repository: %v", state)
	}
	if state["result"].(map[string]any)["repository"] != nil {
		t.Fatal("an unobservable repository was reported as observed")
	}
	if refs, _ := state["evidence_refs"].([]any); len(refs) != 1 || refs[0] != "repository:unobserved" {
		t.Fatalf("the unobserved repository was not disclosed: %v", state["evidence_refs"])
	}
	l.stop(t)
}

// The real binary over real stdio: no arguments, a source-free cwd, only
// protocol frames on stdout, and exit code 2 for arguments.
func TestA12_BinaryOverStdio(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := filepath.Join(t.TempDir(), "devcadence-mcp")
	build := exec.Command("go", "build", "-o", bin, "./cmd/devcadence-mcp")
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("cannot build the binary here: %v\n%s", err, out)
	}
	if err := exec.Command(bin, "--stdio").Run(); err == nil {
		t.Fatal("an argument was accepted")
	} else if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 2 {
		t.Fatalf("exit %v", err)
	}

	home, _ := launchHome(t, goodBinding, nil)
	cmd := exec.Command(bin)
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), mcpadapter.EnvProjectID+"=example", mcpadapter.EnvHome+"="+home)
	stderr := &syncBuffer{}
	cmd.Stderr = stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "1"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect: %v\n%s", err, stderr.String())
	}
	defer cs.Close()
	version := cs.InitializeResult().ProtocolVersion
	list, err := cs.ListTools(ctx, nil)
	if err != nil || len(list.Tools) != 11 {
		t.Fatalf("tools: %v %v", len(list.Tools), err)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "project_state", Arguments: map[string]any{
		"meta": map[string]any{"schema_version": "1.0", "project_id": "example", "correlation_id": "c"}}})
	if err != nil || res.IsError {
		t.Fatalf("project_state: %v %+v", err, res)
	}
	var doc map[string]any
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &doc); err != nil || doc["state_revision"] == nil {
		t.Fatalf("unexpected result %s", raw)
	}
	t.Logf("negotiated protocol version %s; tools %d; stderr bytes %d", version, len(list.Tools), len(stderr.String()))
	// Closing stdin (EOF) shuts the server down cleanly.
	if err := cs.Close(); err != nil {
		t.Logf("close: %v", err)
	}
}
