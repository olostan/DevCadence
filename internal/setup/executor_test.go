package setup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/artifacts"
	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
)

// fakeCommandRunner returns canned results per executable name, and records
// every spec it was asked to run — tests never depend on a real ollama/
// python3/git binary being present.
type fakeCommandRunner struct {
	results map[string]process.Result
	err     error
	calls   []process.Spec
}

func (f *fakeCommandRunner) Run(ctx context.Context, spec process.Spec) (process.Result, error) {
	f.calls = append(f.calls, spec)
	if f.err != nil {
		return process.Result{}, f.err
	}
	if result, ok := f.results[spec.Executable]; ok {
		return result, nil
	}
	return process.Result{Status: process.StatusCompleted, ExitCode: 0}, nil
}

func executorTestFixture(t *testing.T) (*Executor, string, clock.Clock) {
	t.Helper()
	home := t.TempDir()
	if err := EnsureLayout(home); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}
	ledger, err := OpenLedger(filepath.Join(home, "state", "setup-ledger.jsonl"))
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	store, err := artifacts.NewStore(filepath.Join(home, "artifacts", "setup"), ids.NewSequential())
	if err != nil {
		t.Fatalf("artifacts.NewStore: %v", err)
	}
	cache, err := NewCacheManager(filepath.Join(home, "state"), clock.NewFake(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC), 0), 0)
	if err != nil {
		t.Fatalf("NewCacheManager: %v", err)
	}
	clk := clock.NewFake(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC), time.Second)
	runner := &fakeCommandRunner{results: map[string]process.Result{}}

	exec, err := NewExecutor(ExecutorOptions{
		Runner:    runner,
		Home:      home,
		Ledger:    ledger,
		Cache:     cache,
		Artifacts: store,
		Clock:     clk,
		IDs:       ids.NewSequential(),
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	return exec, home, clk
}

// buildActionPlan wraps one or more already-constructed actions into a
// valid, digest-correct SetupPlan.
func buildActionPlan(t *testing.T, target protocol.SetupTarget, actions ...protocol.SetupAction) *protocol.SetupPlan {
	t.Helper()
	maxAuth := protocol.AuthorityReadOnly
	effectSet := map[protocol.EffectCategory]bool{}
	for _, a := range actions {
		if a.Authority.Rank() > maxAuth.Rank() {
			maxAuth = a.Authority
		}
		for _, e := range a.Effects {
			effectSet[e] = true
		}
	}
	var totalEffects []protocol.EffectCategory
	for e := range effectSet {
		totalEffects = append(totalEffects, e)
	}

	plan := &protocol.SetupPlan{
		SchemaVersion:      protocol.SchemaVersion1,
		PlanID:             "plan_test_0000000000000001",
		RecipeSetVersion:   "1.0",
		MachineFingerprint: testMachineFingerprint,
		CreatedAt:          protocol.NewTimestamp(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)),
		Target:             target,
		Actions:            actions,
		RequiredAuthority:  maxAuth,
		TotalEffects:       totalEffects,
	}
	digest := testPlanDigest(t, plan)
	plan.PlanDigest = digest
	if err := plan.Validate(); err != nil {
		t.Fatalf("test plan failed validation: %v", err)
	}
	return plan
}

func createDirAction(id string, deps ...string) protocol.SetupAction {
	op := protocol.TypedOperation{
		Kind: protocol.OpKindCreateDirectory,
		CreateDirectory: &protocol.CreateDirectoryParams{
			Location:    protocol.LocationTmp,
			FileModeOct: "0700",
		},
	}
	effects, auth := protocol.IntrinsicPolicy(op)
	return protocol.SetupAction{
		ActionID:      id,
		RecipeID:      "recipe.mkdir.tmp",
		RecipeVersion: "1.0",
		Title:         "Create tmp dir",
		Description:   "Creates the managed tmp directory",
		Authority:     auth,
		Effects:       effects,
		Operation:     &op,
		Postconditions: []protocol.Condition{{
			Kind: protocol.CondKindManagedDirExists,
			ManagedDirExists: &protocol.ManagedDirOperand{
				Location:    protocol.LocationTmp,
				FileModeOct: "0700",
			},
		}},
		IdempotencyKey: "create_dir_tmp_" + id,
		DependsOn:      deps,
	}
}

func TestExecutorRejectsWrongDigest(t *testing.T) {
	exec, home, _ := executorTestFixture(t)
	plan := buildActionPlan(t, protocol.TargetAll, createDirAction("act-001"))

	_, err := exec.Apply(context.Background(), plan, "sha256:"+"0"+plan.PlanDigest[8:], false)
	if err == nil {
		t.Fatal("Apply accepted a plan with the wrong approved digest; expected an error")
	}

	if _, statErr := os.Stat(filepath.Join(home, "state", "setup-ledger.jsonl")); !os.IsNotExist(statErr) {
		t.Fatalf("ledger file exists after a digest-mismatched Apply; expected no ledger writes at all")
	}
}

func TestExecutorHaltsOnPreconditionDrift(t *testing.T) {
	exec, _, _ := executorTestFixture(t)

	first := createDirAction("act-001")

	op := protocol.TypedOperation{
		Kind: protocol.OpKindRunDiagnosticCheck,
		RunDiagnosticCheck: &protocol.RunDiagnosticCheckParams{
			CheckName: protocol.CheckStateRootWritable,
		},
	}
	effects, auth := protocol.IntrinsicPolicy(op)
	second := protocol.SetupAction{
		ActionID:      "act-002",
		RecipeID:      "recipe.diagnostic.state_root",
		RecipeVersion: "1.0",
		Title:         "Check state root writable",
		Description:   "Diagnostic check gated by a precondition that will never hold",
		Authority:     auth,
		Effects:       effects,
		Operation:     &op,
		Preconditions: []protocol.Condition{{
			Kind: protocol.CondKindCommandAvailable,
			CommandAvailable: &protocol.CommandAvailableOperand{
				CommandName: "definitely-not-a-real-command-xyz-devcadence-test",
			},
		}},
		IdempotencyKey: "check_state_root",
		DependsOn:      []string{"act-001"},
	}

	plan := buildActionPlan(t, protocol.TargetAll, first, second)

	report, err := exec.Apply(context.Background(), plan, plan.PlanDigest, false)
	if err == nil {
		t.Fatal("Apply succeeded despite a drifted precondition on the second action; expected an error")
	}
	if report == nil {
		t.Fatal("Apply returned a nil report alongside the drift error; expected a partial report")
	}

	foundFirst, foundSecond := false, false
	for _, r := range report.Results {
		if r.ActionID == "act-001" {
			foundFirst = true
			if r.Status != protocol.ActionStatusSucceeded {
				t.Errorf("act-001 status = %q, want succeeded", r.Status)
			}
		}
		if r.ActionID == "act-002" {
			foundSecond = true
		}
	}
	if !foundFirst {
		t.Error("report does not include act-001, which should have executed before the halt")
	}
	if foundSecond {
		t.Error("report includes act-002, which should never have started (precondition drifted before ActionStarting)")
	}
}

func TestExecutorRejectsYesScopeOnPrivilegedPlan(t *testing.T) {
	exec, home, _ := executorTestFixture(t)

	manual := protocol.SetupAction{
		ActionID:      "act-001",
		RecipeID:      "recipe.manual.install_something",
		RecipeVersion: "1.0",
		Title:         "Install something manually",
		Description:   "Requires a human",
		Authority:     protocol.AuthorityHighImpactManual,
		Effects:       []protocol.EffectCategory{protocol.EffectPrivilegeElevation},
		ManualInstructions: &protocol.ManualGuide{
			Summary: "Do the thing",
			Steps:   []string{"Step one"},
			VerificationCheck: []protocol.Condition{{
				Kind:             protocol.CondKindCommandAvailable,
				CommandAvailable: &protocol.CommandAvailableOperand{CommandName: "git"},
			}},
		},
		IdempotencyKey: "manual_install_something",
	}
	plan := buildActionPlan(t, protocol.TargetAll, manual)

	_, err := exec.Apply(context.Background(), plan, plan.PlanDigest, true)
	if err == nil {
		t.Fatal("Apply accepted --yes-equivalent scope on a plan requiring high_impact_manual authority; expected rejection")
	}

	if _, statErr := os.Stat(filepath.Join(home, "state", "setup-ledger.jsonl")); !os.IsNotExist(statErr) {
		t.Fatalf("ledger file exists after a yesScope-rejected Apply; expected no ledger writes at all")
	}
}

func TestExecutorYesScopeAllowsUserConfirmationPlan(t *testing.T) {
	exec, _, _ := executorTestFixture(t)
	plan := buildActionPlan(t, protocol.TargetAll, createDirAction("act-001"))

	report, err := exec.Apply(context.Background(), plan, plan.PlanDigest, true)
	if err != nil {
		t.Fatalf("Apply with yesScope on a user_confirmation-only plan failed: %v", err)
	}
	if report.Status != protocol.ExecutionStatusSucceeded {
		t.Fatalf("report.Status = %q, want succeeded", report.Status)
	}
}

func mlxAction(id string, effects []protocol.EffectCategory) protocol.SetupAction {
	op := protocol.TypedOperation{
		Kind: protocol.OpKindRunDiagnosticCheck,
		RunDiagnosticCheck: &protocol.RunDiagnosticCheckParams{
			CheckName: protocol.CheckMLXImportable,
		},
	}
	_, auth := protocol.IntrinsicPolicy(op)
	return protocol.SetupAction{
		ActionID:       id,
		RecipeID:       "recipe.diagnostic.mlx",
		RecipeVersion:  "1.0",
		Title:          "Check mlx importable",
		Description:    "Runs python3 -c \"import mlx\"",
		Authority:      auth,
		Effects:        effects,
		Operation:      &op,
		IdempotencyKey: "check_mlx_" + id,
	}
}

func TestExecutorBoundsOutputArtifacts(t *testing.T) {
	exec, _, _ := executorTestFixture(t)

	huge := make([]byte, process.DefaultMaxOutputBytes+1024)
	for i := range huge {
		huge[i] = 'x'
	}
	exec.runner.(*fakeCommandRunner).results["python3"] = process.Result{
		Status: process.StatusCompleted, ExitCode: 0, Stdout: huge,
	}

	action := mlxAction("act-001", []protocol.EffectCategory{protocol.EffectNetworkAccess})
	plan := buildActionPlan(t, protocol.TargetAll, action)

	report, err := exec.Apply(context.Background(), plan, plan.PlanDigest, false)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(report.Results) != 1 {
		t.Fatalf("report.Results = %+v, want exactly one result", report.Results)
	}
	ref := report.Results[0].ArtifactRef
	if ref == nil {
		t.Fatal("report.Results[0].ArtifactRef is nil, want a captured (truncated) artifact")
	}
	if !ref.Truncated {
		t.Error("ArtifactRef.Truncated = false, want true for output exceeding the byte cap")
	}
	if ref.SizeBytes > process.DefaultMaxOutputBytes {
		t.Errorf("ArtifactRef.SizeBytes = %d, want <= %d (process.DefaultMaxOutputBytes)", ref.SizeBytes, process.DefaultMaxOutputBytes)
	}
}

func TestExecutorNeverArtifactsAuthenticationActions(t *testing.T) {
	exec, _, _ := executorTestFixture(t)
	exec.runner.(*fakeCommandRunner).results["python3"] = process.Result{
		Status: process.StatusCompleted, ExitCode: 0, Stdout: []byte("some output that would otherwise be captured"),
	}

	action := mlxAction("act-001", []protocol.EffectCategory{protocol.EffectNetworkAccess, protocol.EffectAuthentication})
	plan := buildActionPlan(t, protocol.TargetAll, action)

	report, err := exec.Apply(context.Background(), plan, plan.PlanDigest, false)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(report.Results) != 1 {
		t.Fatalf("report.Results = %+v, want exactly one result", report.Results)
	}
	if report.Results[0].ArtifactRef != nil {
		t.Errorf("ArtifactRef = %+v, want nil for an action whose Effects include EffectAuthentication", report.Results[0].ArtifactRef)
	}
}

func TestExecutorManualActionHaltsTheWalk(t *testing.T) {
	exec, _, _ := executorTestFixture(t)

	manual := protocol.SetupAction{
		ActionID:      "act-001",
		RecipeID:      "recipe.manual.install_something",
		RecipeVersion: "1.0",
		Title:         "Install something manually",
		Description:   "Requires a human",
		Authority:     protocol.AuthorityHighImpactManual,
		Effects:       []protocol.EffectCategory{protocol.EffectPrivilegeElevation},
		ManualInstructions: &protocol.ManualGuide{
			Summary: "Do the thing",
			Steps:   []string{"Step one"},
			VerificationCheck: []protocol.Condition{{
				Kind:             protocol.CondKindCommandAvailable,
				CommandAvailable: &protocol.CommandAvailableOperand{CommandName: "git"},
			}},
		},
		IdempotencyKey: "manual_install_something",
	}
	second := createDirAction("act-002", "act-001")
	// act-002 doesn't actually depend on act-001 in the graph sense that
	// matters here beyond ordering, but DependsOn keeps it strictly after
	// act-001 in the required topological order (SetupPlan.Validate()).
	plan := buildActionPlan(t, protocol.TargetAll, manual, second)

	report, err := exec.Apply(context.Background(), plan, plan.PlanDigest, false)
	if err == nil {
		t.Fatal("Apply succeeded despite a manual action; expected the walk to halt")
	}

	foundManual, foundSecond := false, false
	for _, r := range report.Results {
		if r.ActionID == "act-001" {
			foundManual = true
			if r.Status != protocol.ActionStatusManualRequired {
				t.Errorf("act-001 status = %q, want manual_required", r.Status)
			}
		}
		if r.ActionID == "act-002" {
			foundSecond = true
		}
	}
	if !foundManual {
		t.Error("report does not include act-001 (the manual action)")
	}
	if foundSecond {
		t.Error("report includes act-002, which should never have started past a manual action")
	}
}

func TestExecutorCheckPostconditionsSatisfiesPostconditionChecker(t *testing.T) {
	exec, _, _ := executorTestFixture(t)
	var _ PostconditionChecker = exec // compile-time assertion

	passed, _, err := exec.CheckPostconditions(context.Background(), []protocol.Condition{{
		Kind:             protocol.CondKindCommandAvailable,
		CommandAvailable: &protocol.CommandAvailableOperand{CommandName: "git"},
	}})
	if err != nil {
		t.Fatalf("CheckPostconditions: %v", err)
	}
	if !passed {
		t.Skip("git not available in this environment; CheckPostconditions itself is exercised regardless")
	}
}

func TestExecutorRejectsInvalidPlan(t *testing.T) {
	exec, _, _ := executorTestFixture(t)
	plan := buildActionPlan(t, protocol.TargetAll, createDirAction("act-001"))
	plan.Target = protocol.SetupTarget("not_a_real_target") // corrupt after digesting, so Validate() now fails

	if _, err := exec.Apply(context.Background(), plan, plan.PlanDigest, false); err == nil {
		t.Fatal("Apply accepted a plan that fails its own Validate(); expected an error")
	}
}
