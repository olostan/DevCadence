package principalhosts_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/controlplane"
	ph "github.com/olostan/DevCadence/internal/principalhosts"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/storage"
)

var epoch = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

func envFacts(entries ...protocol.SoftwarePresence) func(context.Context) (protocol.EnvironmentFacts, error) {
	return func(context.Context) (protocol.EnvironmentFacts, error) {
		f := protocol.EnvironmentFacts{ObservedAt: protocol.NewTimestamp(epoch), Software: entries}
		f.Host.Family = "linux"
		return f, nil
	}
}

func host(id string, installed bool, path, version string, vs protocol.VersionStatus) protocol.SoftwarePresence {
	return protocol.SoftwarePresence{ID: id, Category: protocol.SoftwarePrincipalHost, Installed: installed, Path: path, Version: version, VersionStatus: vs}
}

func states(t *testing.T, svc *ph.Service) map[string]string {
	t.Helper()
	obs, err := svc.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, o := range obs {
		out[o.HostID] = o.State
	}
	return out
}

// ACC-01, ACC-02: absence is normal; ambiguity and incompatibility are separate.
func TestDetectReportsEveryStateSeparately(t *testing.T) {
	none := states(t, ph.New(ph.Options{Environment: envFacts()}))
	for _, h := range []string{"antigravity", "cursor", "vscode"} {
		if none[h] != ph.StateAbsent {
			t.Fatalf("%s = %q, want absent", h, none[h])
		}
	}
	got := states(t, ph.New(ph.Options{Environment: envFacts(
		host("antigravity", true, "/usr/bin/ag", "1.0", protocol.VersionUnknown),
		host("cursor", true, "/usr/bin/cursor", "9", protocol.VersionIncompatible),
		host("vscode", true, "/a/code", "1", protocol.VersionUnknown),
		host("vscode", true, "/b/code", "2", protocol.VersionUnknown),
	)}))
	if got["antigravity"] != ph.StateDetected || got["cursor"] != ph.StateIncompatible || got["vscode"] != ph.StateAmbiguous {
		t.Fatalf("states = %v", got)
	}
	if st := states(t, ph.New(ph.Options{Environment: envFacts(host("cursor", true, "", "", ""))})); st["cursor"] != ph.StateUnknown {
		t.Fatalf("pathless install = %q, want unknown", st["cursor"])
	}
	if _, err := ph.New(ph.Options{}).Detect(context.Background()); err == nil {
		t.Fatal("detect without an environment source must fail")
	}
}

type rig struct {
	t             *testing.T
	svc           *ph.Service
	ws, home, exe string
	source        string
	approvals     *fakeApprovals
	prober        *fakeProber
	last          ph.IntegrationPlan
	req           ph.IntegrationRequest
}

type fakeApprovals struct {
	err   error
	calls int
	paths []string
}

func (f *fakeApprovals) VerifyApproval(_ context.Context, _, _ string, paths []string) error {
	f.calls++
	f.paths = paths
	return f.err
}

func newRig(t *testing.T, hostID, mode string) *rig {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, ws: filepath.Join(root, "ws"), home: filepath.Join(root, "home"), exe: filepath.Join(root, "devcadence-mcp"),
		source: filepath.Join(root, "source"), approvals: &fakeApprovals{}, prober: newProber()}
	for _, d := range []string{r.ws, r.home, r.source} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(r.exe, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	r.svc = ph.New(ph.Options{Environment: envFacts(host(hostID, true, "/usr/bin/"+hostID, "1.0", protocol.VersionUnknown)),
		SourceRoots: []string{r.source}, Approvals: r.approvals, Prober: r.prober, Clock: clock.NewFake(epoch, time.Second)})
	r.req = ph.IntegrationRequest{HostID: hostID, ProjectID: "proj", PrincipalWorkspace: r.ws, MCPExecutable: r.exe, RuntimeHome: r.home, Mode: mode}
	return r
}

func (r *rig) plan() ph.IntegrationPlan {
	r.t.Helper()
	obs, err := r.svc.Detect(context.Background())
	if err != nil {
		r.t.Fatal(err)
	}
	for _, o := range obs {
		if o.HostID == r.req.HostID {
			p, err := r.svc.Plan(context.Background(), o, r.req)
			if err != nil {
				r.t.Fatal(err)
			}
			return p
		}
	}
	r.t.Fatal("host not observed")
	return ph.IntegrationPlan{}
}

// applyByHand plays the operator applying the reviewed bytes.
func (r *rig) applyByHand(p ph.IntegrationPlan) {
	r.t.Helper()
	for _, f := range p.Files {
		if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(f.Path, f.Content, 0o644); err != nil {
			r.t.Fatal(err)
		}
	}
}

func digestOfTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			b, _ := os.ReadFile(p)
			out[p] = string(b)
		}
		return nil
	})
	return out
}

func sameTree(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func findFile(t *testing.T, p ph.IntegrationPlan, suffix string) ph.PlannedFile {
	t.Helper()
	for _, f := range p.Files {
		if strings.HasSuffix(f.Path, suffix) {
			return f
		}
	}
	t.Fatalf("no planned file %s", suffix)
	return ph.PlannedFile{}
}

// ACC-02: a non-detected or mismatched observation never plans; VS Code is guidance only.
func TestPlanRequiresAnExplicitDetectedHost(t *testing.T) {
	r := newRig(t, "antigravity", ph.ModeAssisted)
	ctx := context.Background()
	for _, state := range []string{ph.StateAbsent, ph.StateAmbiguous, ph.StateUnknown, ph.StateIncompatible} {
		_, err := r.svc.Plan(ctx, ph.HostObservation{HostID: "antigravity", State: state}, r.req)
		if !errors.Is(err, ph.ErrHostNotSelected) {
			t.Errorf("%s: %v", state, err)
		}
	}
	if _, err := r.svc.Plan(ctx, ph.HostObservation{HostID: "cursor", State: ph.StateDetected}, r.req); !errors.Is(err, ph.ErrHostNotSelected) {
		t.Errorf("mismatched host: %v", err)
	}
	req := r.req
	req.HostID = "vscode"
	if _, err := r.svc.Plan(ctx, ph.HostObservation{HostID: "vscode", State: ph.StateDetected}, req); !errors.Is(err, ph.ErrGuidanceOnly) {
		t.Errorf("vscode: %v", err)
	}
	if _, err := r.svc.Plan(ctx, ph.HostObservation{HostID: "antigravity", State: ph.StateDetected}, ph.IntegrationRequest{HostID: "antigravity"}); !errors.Is(err, ph.ErrInvalidRequest) {
		t.Errorf("empty request: %v", err)
	}
}

var golden = map[string]struct{ host, planned, fixture string }{
	"ag-config": {"antigravity", ".agents/mcp_config.json", "antigravity.mcp_config.json"},
	"ag-rule":   {"antigravity", ".agents/rules/devcadence-principal.md", "antigravity.rule.md"},
	"ag-skill":  {"antigravity", ".agents/skills/devcadence-principal/SKILL.md", "antigravity.SKILL.md"},
	"cu-config": {"cursor", ".cursor/mcp.json", "cursor.mcp.json"},
	"cu-rule":   {"cursor", ".cursor/rules/devcadence-principal.mdc", "cursor.rule.mdc"},
}

// ACC-03: exact native paths, type, frontmatter and env, against committed fixtures.
func TestRenderedNativePlansMatchGoldenFixtures(t *testing.T) {
	for name, g := range golden {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, g.host, ph.ModeAssisted)
			r.req.PrincipalBindingPath = ""
			plan := r.plan()
			f := findFile(t, plan, g.planned)
			content := strings.ReplaceAll(string(f.Content), r.ws, "/WORKSPACE")
			content = strings.ReplaceAll(content, r.exe, "/EXECUTABLE")
			content = strings.ReplaceAll(content, r.home, "/HOME")
			path := filepath.Join("..", "..", "fixtures", "principalhosts", g.fixture)
			if os.Getenv("UPDATE_PRINCIPALHOSTS_GOLDENS") == "1" {
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if content != string(want) {
				t.Fatalf("rendered %s differs from fixture:\n%s", g.planned, content)
			}
		})
	}
}

func TestNativeRecipeSemantics(t *testing.T) {
	for _, hostID := range []string{"antigravity", "cursor"} {
		r := newRig(t, hostID, ph.ModeAssisted)
		r.req.PrincipalBindingPath = filepath.Join(r.home, "config", "alt-binding.json")
		cfg := "/.agents/mcp_config.json"
		if hostID == "cursor" {
			cfg = "/.cursor/mcp.json"
		}
		doc := map[string]map[string]map[string]any{}
		if err := json.Unmarshal(findFile(t, r.plan(), cfg).Content, &doc); err != nil {
			t.Fatal(err)
		}
		entry := doc["mcpServers"]["devcadence"]
		env := entry["env"].(map[string]any)
		if entry["command"] != r.exe || env["DEVCADENCE_HOME"] != r.home || env["DEVCADENCE_PROJECT_ID"] != "proj" ||
			env["DEVCADENCE_PRINCIPAL_BINDING"] != r.req.PrincipalBindingPath {
			t.Errorf("%s entry %v", hostID, entry)
		}
		if _, has := entry["args"]; has {
			t.Errorf("%s: no args expected", hostID)
		}
		if _, has := entry["cwd"]; has {
			t.Errorf("%s: no source cwd expected", hostID)
		}
		if (entry["type"] == "stdio") != (hostID == "cursor") {
			t.Errorf("%s: type = %v", hostID, entry["type"])
		}
	}
	// Default binding: the env pair is omitted.
	r := newRig(t, "cursor", ph.ModeAssisted)
	if strings.Contains(string(findFile(t, r.plan(), "/.cursor/mcp.json").Content), "PRINCIPAL_BINDING") {
		t.Error("default binding must not be rendered")
	}
}

// ACC-03: unrelated JSON is preserved; replacing an unequal owned entry is explicit.
func TestExistingConfigIsMergedNotRewritten(t *testing.T) {
	r := newRig(t, "cursor", ph.ModeAssisted)
	cfg := filepath.Join(r.ws, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	old := `{"theme":{"x":1},"mcpServers":{"other":{"command":"/bin/other","args":["a"]},"devcadence":{"command":"/old"}}}`
	if err := os.WriteFile(cfg, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := r.plan()
	f := findFile(t, plan, "/.cursor/mcp.json")
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(f.Content, &doc); err != nil || !semanticallyEqual(doc["theme"], `{"x":1}`) {
		t.Fatalf("top-level key lost: %v %s", err, f.Content)
	}
	var servers map[string]json.RawMessage
	_ = json.Unmarshal(doc["mcpServers"], &servers)
	if !semanticallyEqual(servers["other"], `{"command":"/bin/other","args":["a"]}`) {
		t.Fatalf("unrelated entry changed: %s", servers["other"])
	}
	if f.BeforeSHA256 == "absent" || f.OwnedEntry != "devcadence" {
		t.Fatalf("preimage/owner: %+v", f)
	}
	if !strings.Contains(strings.Join(plan.ManualSteps, "\n"), "REPLACES") {
		t.Fatal("replacement of an unequal owned entry must be explicit")
	}
	// Malformed config is a conflict, never overwritten.
	if err := os.WriteFile(cfg, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	obs, _ := r.svc.Detect(context.Background())
	for _, o := range obs {
		if o.HostID == "cursor" {
			if _, err := r.svc.Plan(context.Background(), o, r.req); !errors.Is(err, ph.ErrConfigNotMergeable) {
				t.Fatalf("malformed config: %v", err)
			}
		}
	}
}

// ACC-20: runtime path validation.
func TestInvalidRuntimePathsAreBlocked(t *testing.T) {
	r := newRig(t, "antigravity", ph.ModeStrict)
	obs := ph.HostObservation{HostID: "antigravity", State: ph.StateDetected}
	ctx := context.Background()
	mutate := func(name string, f func(*ph.IntegrationRequest)) {
		req := r.req
		f(&req)
		if _, err := r.svc.Plan(ctx, obs, req); !errors.Is(err, ph.ErrInvalidRequest) {
			t.Errorf("%s: %v", name, err)
		}
	}
	mutate("missing home", func(q *ph.IntegrationRequest) { q.RuntimeHome = "" })
	mutate("relative home", func(q *ph.IntegrationRequest) { q.RuntimeHome = "rel/home" })
	mutate("non-canonical home", func(q *ph.IntegrationRequest) { q.RuntimeHome = r.home + "/../home" })
	mutate("relative binding", func(q *ph.IntegrationRequest) { q.PrincipalBindingPath = "b.json" })
	mutate("home inside source", func(q *ph.IntegrationRequest) { q.RuntimeHome = filepath.Join(r.source, "home") })
	mutate("binding inside source", func(q *ph.IntegrationRequest) { q.PrincipalBindingPath = filepath.Join(r.source, "b.json") })
	mutate("strict workspace in source", func(q *ph.IntegrationRequest) { q.PrincipalWorkspace = filepath.Join(r.source, "ws") })
	mutate("source inside workspace", func(q *ph.IntegrationRequest) { q.PrincipalWorkspace = filepath.Dir(r.source) })
	mutate("no project", func(q *ph.IntegrationRequest) { q.ProjectID = " " })
	mutate("bad mode", func(q *ph.IntegrationRequest) { q.Mode = "" })
	// Assisted may share the workspace with source but never the runtime home.
	req := r.req
	req.Mode, req.PrincipalWorkspace = ph.ModeAssisted, filepath.Join(r.source, "ws")
	if _, err := r.svc.Plan(ctx, obs, req); err != nil {
		t.Errorf("assisted workspace in source: %v", err)
	}
	// Strict without known source roots cannot be planned.
	noSource := ph.New(ph.Options{})
	if _, err := noSource.Plan(ctx, obs, r.req); !errors.Is(err, ph.ErrInvalidRequest) {
		t.Errorf("strict without roots: %v", err)
	}
	// An unprotected home is refused by the real check.
	loose := t.TempDir()
	if err := os.Chmod(loose, 0o755); err != nil {
		t.Fatal(err)
	}
	real := ph.New(ph.Options{SourceRoots: []string{r.source}})
	req = r.req
	req.RuntimeHome = loose
	if _, err := real.Plan(ctx, obs, req); !errors.Is(err, ph.ErrInvalidRequest) {
		t.Errorf("group/world-accessible home: %v", err)
	}
}

// ACC-04, ACC-05, ACC-16: nothing is ever written; unauthorized and stale plans are refused.
func TestApplyNeverWritesAndRefusesUnauthorizedOrStalePlans(t *testing.T) {
	r := newRig(t, "antigravity", ph.ModeAssisted)
	plan := r.plan()
	if !plan.ApprovalRequired || plan.AutomaticApplyEligible {
		t.Fatalf("plan flags: %+v", plan)
	}
	before := digestOfTree(t, filepath.Dir(r.ws))
	ctx := context.Background()
	ok := ph.ApprovedPlan{PlanDigest: plan.Digest, ApprovalRef: "appr-1"}

	for name, ap := range map[string]ph.ApprovedPlan{
		"no ref": {PlanDigest: plan.Digest}, "wrong digest": {PlanDigest: "x", ApprovalRef: "appr-1"}, "empty": {},
	} {
		if _, err := r.svc.ApplyApproved(ctx, plan, ap); !errors.Is(err, ph.ErrUnauthorized) {
			t.Errorf("%s: %v", name, err)
		}
	}
	tampered := plan
	tampered.Files = append([]ph.PlannedFile(nil), plan.Files...)
	tampered.Files[0].Content = []byte("evil")
	if _, err := r.svc.ApplyApproved(ctx, tampered, ok); !errors.Is(err, ph.ErrUnauthorized) {
		t.Errorf("tampered: %v", err)
	}
	r.approvals.err = errors.New("denied")
	if _, err := r.svc.ApplyApproved(ctx, plan, ok); !errors.Is(err, ph.ErrUnauthorized) {
		t.Errorf("verifier denial: %v", err)
	}
	r.approvals.err = nil

	// Fully approved: still manual, zero writes (no exclusive channel).
	rec, err := r.svc.ApplyApproved(ctx, plan, ok)
	if err != nil || !rec.Manual || len(rec.AppliedPaths) != 0 || r.approvals.calls == 0 || len(r.approvals.paths) != len(plan.Files) {
		t.Fatalf("receipt %+v err %v", rec, err)
	}
	// No approval authority at all: manual-required, zero writes.
	bare := ph.New(ph.Options{SourceRoots: []string{r.source}})
	if rec, err := bare.ApplyApproved(ctx, plan, ok); err != nil || !rec.Manual {
		t.Fatalf("no authority: %+v %v", rec, err)
	}
	if !sameTree(before, digestOfTree(t, filepath.Dir(r.ws))) {
		t.Fatal("ApplyApproved changed bytes")
	}

	// Stale preimage: a planned file changed after planning.
	cfg := findFile(t, plan, "/.agents/mcp_config.json").Path
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	edited := digestOfTree(t, filepath.Dir(r.ws))
	if rec, err := r.svc.ApplyApproved(ctx, plan, ok); !errors.Is(err, ph.ErrStalePreimage) || len(rec.FailedPaths) == 0 {
		t.Fatalf("stale: %+v %v", rec, err)
	}
	if !sameTree(edited, digestOfTree(t, filepath.Dir(r.ws))) {
		t.Fatal("stale apply changed bytes")
	}
}

// fakeProber derives results from a hostile simulated session, never from a
// preassigned ready flag.
type fakeProber struct {
	info        ph.SessionInfo
	sessionErr  error
	connect     string
	instruction string
	allowed     map[string]bool // route -> access succeeds
	unavailable map[string]bool
	cancel      context.CancelFunc
}

func fullRoutes() []string { return append([]string(nil), ph.RequiredRoutes...) }

func newProber() *fakeProber {
	return &fakeProber{info: ph.SessionInfo{HostVersion: "1.0", OS: "linux", PolicyIdentity: "policy-a", EnabledRoutes: fullRoutes()},
		connect: ph.ResultPass, instruction: ph.ResultPass, allowed: map[string]bool{}, unavailable: map[string]bool{}}
}

func (f *fakeProber) Session(context.Context, string) (ph.SessionInfo, error) {
	return f.info, f.sessionErr
}
func (f *fakeProber) Connect(context.Context, ph.IntegrationPlan) ph.ProbeCheck {
	if f.cancel != nil {
		f.cancel()
	}
	return ph.ProbeCheck{Result: f.connect, EvidenceRef: "c"}
}
func (f *fakeProber) Instructions(context.Context, ph.IntegrationPlan, string) ph.ProbeCheck {
	return ph.ProbeCheck{Result: f.instruction, EvidenceRef: "i"}
}
func (f *fakeProber) Access(_ context.Context, _, route string) ph.ProbeCheck {
	switch {
	case f.unavailable[route]:
		return ph.ProbeCheck{Result: "timeout", EvidenceRef: route}
	case f.allowed[route]:
		return ph.ProbeCheck{Result: ph.ResultFail, EvidenceRef: route}
	}
	return ph.ProbeCheck{Result: ph.ResultPass, EvidenceRef: route}
}

func verifyState(t *testing.T, r *rig, mutate func(*fakeProber)) ph.VerificationReport {
	t.Helper()
	r.prober.info = newProber().info
	*r.prober = *newProber()
	if mutate != nil {
		mutate(r.prober)
	}
	plan := r.plan()
	r.last = plan
	r.applyByHand(plan)
	rep, err := r.svc.Verify(context.Background(), plan, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

// ACC-10: complete fresh denial evidence yields strict_ready bound to identities.
func TestStrictReadyOnlyWithCompleteDenialEvidence(t *testing.T) {
	r := newRig(t, "antigravity", ph.ModeStrict)
	rep := verifyState(t, r, nil)
	if rep.State != ph.ReadyStrict || rep.HostVersion != "1.0" || rep.OS != "linux" || rep.SessionRef != "session-1" || rep.PlanDigest == "" {
		t.Fatalf("report %+v", rep)
	}
	for _, k := range []string{"executable", "policy", "routes", "host_version"} {
		if rep.Identities[k] == "" {
			t.Errorf("identity %s missing", k)
		}
	}
	for _, c := range rep.Checks {
		if c.ObservedAt.IsZero() || c.Result != ph.ResultPass {
			t.Errorf("check %+v", c)
		}
	}
}

// ACC-08, ACC-09, ACC-11, ACC-15: every shortfall blocks strict; assisted stays labeled.
func TestStrictIsBlockedByAnyAccessUnknownOrEscape(t *testing.T) {
	cases := map[string]func(*fakeProber){
		"native read allowed":  func(p *fakeProber) { p.allowed["native_read_absolute"] = true },
		"shell escape allowed": func(p *fakeProber) { p.allowed["shell_escape"] = true },
		"other tool allowed": func(p *fakeProber) {
			p.info.EnabledRoutes = append(fullRoutes(), "mcp_other_fs")
			p.allowed["mcp_other_fs"] = true
		},
		"timeout is not denial":  func(p *fakeProber) { p.unavailable["terminal_write"] = true },
		"policy unavailable":     func(p *fakeProber) { p.info.PolicyIdentity = "" },
		"inventory unavailable":  func(p *fakeProber) { p.info.EnabledRoutes = nil },
		"required route missing": func(p *fakeProber) { p.info.EnabledRoutes = p.info.EnabledRoutes[1:] },
	}
	for name, mutate := range cases {
		r := newRig(t, "antigravity", ph.ModeStrict)
		rep := verifyState(t, r, mutate)
		if rep.State != ph.Blocked || len(rep.Reasons) == 0 {
			t.Errorf("%s: %+v", name, rep)
		}
		// The same session verified as assisted is ready, with the shortfall recorded.
		a := newRig(t, "antigravity", ph.ModeAssisted)
		arep := verifyState(t, a, mutate)
		if arep.State != ph.ReadyAssisted || len(arep.Reasons) == 0 {
			t.Errorf("%s assisted: %+v", name, arep)
		}
	}
	// A route shown disabled by the host needs no denial test.
	r := newRig(t, "antigravity", ph.ModeStrict)
	rep := verifyState(t, r, func(p *fakeProber) {
		p.info.EnabledRoutes = p.info.EnabledRoutes[1:]
		p.info.DisabledRoutes = map[string]string{"native_read_absolute": "host-config:disabled"}
	})
	if rep.State != ph.ReadyStrict {
		t.Errorf("disabled route with evidence: %+v", rep)
	}
}

func TestConnectionInstructionsAndDriftGateReadiness(t *testing.T) {
	r := newRig(t, "cursor", ph.ModeAssisted)
	if rep := verifyState(t, r, func(p *fakeProber) { p.connect = ph.ResultUnavailable }); rep.State != ph.Unverified {
		t.Errorf("connect unavailable: %+v", rep)
	}
	if rep := verifyState(t, r, func(p *fakeProber) { p.instruction = ph.ResultFail }); rep.State != ph.Blocked {
		t.Errorf("instructions fail: %+v", rep)
	}
	if rep := verifyState(t, r, func(p *fakeProber) { p.info.HostVersion = "" }); rep.State != ph.Unverified {
		t.Errorf("no session identity: %+v", rep)
	}
	if rep := verifyState(t, r, func(p *fakeProber) { p.sessionErr = errors.New("x") }); rep.State != ph.Unverified {
		t.Errorf("session error: %+v", rep)
	}
	// Cancellation during probing is never a pass.
	ctx, cancel := context.WithCancel(context.Background())
	*r.prober = *newProber()
	r.prober.cancel = cancel
	plan := r.plan()
	r.applyByHand(plan)
	if rep, _ := r.svc.Verify(ctx, plan, "s"); rep.State == ph.ReadyAssisted || rep.State == ph.ReadyStrict {
		t.Errorf("cancelled verify: %+v", rep)
	}
	// Unapplied configuration is blocked: Verify checks real bytes, not an assertion.
	r2 := newRig(t, "cursor", ph.ModeAssisted)
	plan2 := r2.plan()
	if rep, _ := r2.svc.Verify(context.Background(), plan2, "s"); rep.State != ph.Blocked {
		t.Errorf("unapplied plan: %+v", rep)
	}
	if rep, _ := ph.New(ph.Options{}).Verify(context.Background(), plan2, "s"); rep.State != ph.Unverified {
		t.Errorf("no prober: %+v", rep)
	}
	if _, err := r2.svc.Verify(context.Background(), ph.IntegrationPlan{}, "s"); !errors.Is(err, ph.ErrUnauthorized) {
		t.Errorf("forged plan: %v", err)
	}
}

// ACC-13: a stored report is evidence; any identity change requires reverification.
func TestReuseRequiresUnchangedIdentities(t *testing.T) {
	r := newRig(t, "antigravity", ph.ModeStrict)
	rep := verifyState(t, r, nil)
	plan := r.last
	ctx := context.Background()
	if err := r.svc.Reuse(ctx, rep, plan, "session-1"); err != nil {
		t.Fatalf("unchanged identities: %v", err)
	}
	changes := map[string]func(){
		"new session":    func() {},
		"host version":   func() { r.prober.info.HostVersion = "1.1" },
		"policy":         func() { r.prober.info.PolicyIdentity = "policy-b" },
		"route":          func() { r.prober.info.EnabledRoutes = append(fullRoutes(), "extra") },
		"executable":     func() { _ = os.WriteFile(r.exe, []byte("other"), 0o700) },
		"config":         func() { _ = os.WriteFile(findFile(t, plan, "mcp_config.json").Path, []byte("{}"), 0o644) },
		"session policy": func() { r.prober.sessionErr = errors.New("introspection lost") },
	}
	for name, change := range changes {
		*r.prober = *newProber()
		_ = os.WriteFile(r.exe, []byte("binary"), 0o700)
		r.applyByHand(plan)
		change()
		ref := "session-1"
		if name == "new session" {
			ref = "session-2"
		}
		if err := r.svc.Reuse(ctx, rep, plan, ref); !errors.Is(err, ph.ErrReverification) {
			t.Errorf("%s: %v", name, err)
		}
	}
	notReady := rep
	notReady.State = ph.Blocked
	*r.prober = *newProber()
	if err := r.svc.Reuse(ctx, notReady, plan, "session-1"); !errors.Is(err, ph.ErrReverification) {
		t.Errorf("blocked report reuse: %v", err)
	}
}

// ACC-07 (real server bytes): the smoke launches devcadence-mcp from a source-free
// directory, reads project state, and rejects a wrong project binding.
func TestSmokeAgainstTheRealServerBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := filepath.Join(t.TempDir(), "devcadence-mcp")
	build := exec.Command("go", "build", "-o", bin, "./cmd/devcadence-mcp")
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("cannot build the binary here: %v\n%s", err, out)
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	home := filepath.Join(root, "home")
	for _, d := range []string{home, filepath.Join(home, "config"), filepath.Join(home, "state")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		_ = os.Chmod(d, 0o700)
	}
	binding := `{"binding_version":"1.0","project_id":"example","principal_id":"p1","allowed_actions":["project_state"],"policy_ref":"pol","max_evidence_bytes":4096,"max_snippet_lines":100,"source_depth":"symbol"}`
	if err := os.WriteFile(filepath.Join(home, "config", "principal-binding.json"), []byte(binding), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	store, err := storage.Open(ctx, storage.Config{Path: filepath.Join(home, "state", "control-plane.db"), Clock: clock.System()})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := controlplane.New(controlplane.Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InitProject(ctx, controlplane.InitProjectInput{ProjectID: "example", MilestoneID: "M1", MilestoneTitle: "Domain core"}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	req := ph.IntegrationRequest{ProjectID: "example", MCPExecutable: bin, RuntimeHome: home}
	if c := ph.SmokeProjectState(ctx, req); c.Result != ph.ResultPass {
		t.Fatalf("smoke: %+v", c)
	}
	wrong := req
	wrong.ProjectID = "someone-else"
	if c := ph.SmokeProjectState(ctx, wrong); c.Result == ph.ResultPass {
		t.Fatalf("wrong binding passed: %+v", c)
	}
	missing := req
	missing.PrincipalBindingPath = filepath.Join(home, "config", "absent.json")
	if c := ph.SmokeProjectState(ctx, missing); c.Result == ph.ResultPass {
		t.Fatalf("missing binding passed: %+v", c)
	}
	nobin := req
	nobin.MCPExecutable = filepath.Join(root, "nope")
	if c := ph.SmokeProjectState(ctx, nobin); c.Result != ph.ResultFail {
		t.Fatalf("missing executable: %+v", c)
	}
}

// ACC-14: VS Code samples parse, use the right roots and make no Windows sandbox claim.
func TestVSCodeGuidance(t *testing.T) {
	dir := filepath.Join("..", "..", "integrations", "vscode")
	for file, root := range map[string]string{"mcp.portable.json": "mcpServers", "mcp.native.json": "servers"} {
		b, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]map[string]map[string]any
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		entry := doc[root]["devcadence"]
		env, _ := entry["env"].(map[string]any)
		if entry["type"] != "stdio" || env["DEVCADENCE_HOME"] == nil || env["DEVCADENCE_PROJECT_ID"] == nil || entry["args"] != nil || len(doc) != 1 {
			t.Errorf("%s: %v", file, doc)
		}
	}
	b, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, want := range []string{"macOS and Linux only", "not supported on Windows", "Agent Host", "DEVCADENCE_PRINCIPAL_BINDING", "nothing here is first-class empirical readiness"} {
		if !strings.Contains(text, want) {
			t.Errorf("README lacks %q", want)
		}
	}
}

func semanticallyEqual(got json.RawMessage, want string) bool {
	var a, b any
	return json.Unmarshal(got, &a) == nil && json.Unmarshal([]byte(want), &b) == nil && reflect.DeepEqual(a, b)
}
