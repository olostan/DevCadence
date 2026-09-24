package setup

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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
	clk := clock.NewFake(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC), time.Second)
	runner := &fakeCommandRunner{results: map[string]process.Result{}}

	exec, err := NewExecutor(ExecutorOptions{
		Runner: runner,
		Home:   home,
		Clock:  clk,
		IDs:    ids.NewSequential(),
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

	if _, statErr := os.Stat(ledgerPath(home)); !os.IsNotExist(statErr) {
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

	if _, statErr := os.Stat(ledgerPath(home)); !os.IsNotExist(statErr) {
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

func TestNewExecutorRequiresAbsoluteHome(t *testing.T) {
	_, err := NewExecutor(ExecutorOptions{
		Runner: &fakeCommandRunner{},
		Home:   "relative/home",
	})
	if err == nil {
		t.Fatal("NewExecutor accepted a relative Home; expected an error")
	}
}

// blockingCommandRunner signals started the first time Run is called, then
// blocks until release is closed — used to hold an Executor.Apply call
// "in progress" (and thus holding the execution lock) long enough for a
// concurrent Apply attempt to observe it.
type blockingCommandRunner struct {
	started  chan struct{}
	release  chan struct{}
	startsOn sync.Once
}

func newBlockingCommandRunner() *blockingCommandRunner {
	return &blockingCommandRunner{started: make(chan struct{}), release: make(chan struct{})}
}

func (b *blockingCommandRunner) Run(ctx context.Context, spec process.Spec) (process.Result, error) {
	b.startsOn.Do(func() { close(b.started) })
	<-b.release
	return process.Result{Status: process.StatusCompleted, ExitCode: 0}, nil
}

func TestExecutorSerializesConcurrentApply(t *testing.T) {
	home := t.TempDir()
	if err := EnsureLayout(home); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	blocker := newBlockingCommandRunner()
	exec1, err := NewExecutor(ExecutorOptions{Runner: blocker, Home: home, IDs: ids.NewSequential()})
	if err != nil {
		t.Fatalf("NewExecutor (1): %v", err)
	}
	exec2, err := NewExecutor(ExecutorOptions{Runner: &fakeCommandRunner{results: map[string]process.Result{}}, Home: home, IDs: ids.NewSequential()})
	if err != nil {
		t.Fatalf("NewExecutor (2): %v", err)
	}

	plan1 := buildActionPlan(t, protocol.TargetAll, mlxAction("act-001", []protocol.EffectCategory{protocol.EffectNetworkAccess}))
	plan2 := buildActionPlan(t, protocol.TargetAll, createDirAction("act-001"))

	done1 := make(chan error, 1)
	go func() {
		_, err := exec1.Apply(context.Background(), plan1, plan1.PlanDigest, false)
		done1 <- err
	}()

	select {
	case <-blocker.started:
	case <-time.After(5 * time.Second):
		t.Fatal("exec1's Apply never reached the blocking subprocess call")
	}

	done2 := make(chan error, 1)
	go func() {
		_, err := exec2.Apply(context.Background(), plan2, plan2.PlanDigest, false)
		done2 <- err
	}()

	select {
	case <-done2:
		t.Fatal("exec2's Apply completed while exec1 still held the execution lock; expected it to block")
	case <-time.After(200 * time.Millisecond):
		// Expected: exec2 is still blocked on the execution lock.
	}

	close(blocker.release)

	if err := <-done1; err != nil {
		t.Fatalf("exec1's Apply: %v", err)
	}
	select {
	case err := <-done2:
		if err != nil {
			t.Fatalf("exec2's Apply: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("exec2's Apply did not complete after exec1 released the execution lock")
	}
}

// simulateCrash opens the ledger directly (bypassing Executor.Apply) and
// appends ExecutionCreated/PlanApproved/ActionStarting for action, leaving
// no terminal event — the same durable-but-unresolved state a real crash
// between ActionStarting and any terminal event would leave.
func simulateCrash(t *testing.T, home string, plan *protocol.SetupPlan, action protocol.SetupAction) {
	t.Helper()
	ledger, err := OpenLedger(ledgerPath(home))
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	idSrc := ids.NewSequential()
	now := protocol.NewTimestamp(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC))
	for _, e := range []*protocol.SetupLedgerEvent{
		{
			SchemaVersion: protocol.SchemaVersion1, EventID: idSrc.New("evt"),
			ExecutionID: "exec-crashed", PlanID: plan.PlanID, PlanDigest: plan.PlanDigest,
			Timestamp: now, Type: protocol.EventExecutionCreated,
			Payload: protocol.EventPayload{ExecutionCreated: &protocol.ExecutionCreatedPayload{InitiatedBy: "test", Target: plan.Target}},
		},
		{
			SchemaVersion: protocol.SchemaVersion1, EventID: idSrc.New("evt"),
			ExecutionID: "exec-crashed", PlanID: plan.PlanID, PlanDigest: plan.PlanDigest,
			Timestamp: now, Type: protocol.EventPlanApproved,
			Payload: protocol.EventPayload{PlanApproved: &protocol.PlanApprovedPayload{ApprovedAuthority: plan.RequiredAuthority, ApprovedBy: "test"}},
		},
		{
			SchemaVersion: protocol.SchemaVersion1, EventID: idSrc.New("evt"),
			ExecutionID: "exec-crashed", PlanID: plan.PlanID, PlanDigest: plan.PlanDigest, ActionID: action.ActionID,
			Timestamp: now, Type: protocol.EventActionStarting,
			Payload: protocol.EventPayload{ActionStarting: &protocol.ActionStartingPayload{
				ActionID: action.ActionID, RecipeID: action.RecipeID, RecipeVersion: action.RecipeVersion,
				IdempotencyKey: action.IdempotencyKey,
			}},
		},
	} {
		if _, err := ledger.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
}

func TestExecutorApplyRefusesWhileInterruptedActionsUnresolved(t *testing.T) {
	home := t.TempDir()
	if err := EnsureLayout(home); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}
	action := createDirAction("act-001")
	plan := buildActionPlan(t, protocol.TargetAll, action)
	simulateCrash(t, home, plan, action)

	exec, err := NewExecutor(ExecutorOptions{Runner: &fakeCommandRunner{results: map[string]process.Result{}}, Home: home, IDs: ids.NewSequential()})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	otherPlan := buildActionPlan(t, protocol.TargetAll, createDirAction("act-999"))
	if _, err := exec.Apply(context.Background(), otherPlan, otherPlan.PlanDigest, false); err == nil {
		t.Fatal("Apply started a new execution despite an unresolved interrupted action; expected rejection")
	}
}

func TestExecutorRecoverReconcilesInterruptedAction(t *testing.T) {
	home := t.TempDir()
	if err := EnsureLayout(home); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}
	// The action's postcondition (managed_dir_exists for tmp/) already
	// holds because EnsureLayout created it — simulating the case where the
	// actual mutation completed before the crash, so recovery should
	// resolve to succeeded without rerunning anything.
	action := createDirAction("act-001")
	plan := buildActionPlan(t, protocol.TargetAll, action)
	simulateCrash(t, home, plan, action)

	exec, err := NewExecutor(ExecutorOptions{Runner: &fakeCommandRunner{results: map[string]process.Result{}}, Home: home, IDs: ids.NewSequential()})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	statuses, err := exec.Recover(context.Background(), plan)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(statuses) != 1 || statuses[0] != protocol.ActionStatusSucceeded {
		t.Fatalf("Recover statuses = %+v, want one succeeded", statuses)
	}

	// The interrupted action must be durably resolved now — a fresh Apply
	// on a different plan must no longer be refused.
	otherPlan := buildActionPlan(t, protocol.TargetAll, createDirAction("act-999"))
	if _, err := exec.Apply(context.Background(), otherPlan, otherPlan.PlanDigest, false); err != nil {
		t.Fatalf("Apply after Recover: %v", err)
	}
}

func TestExecutorEnsureLocalModelOllamaUsesTheVerifiedExecutablePath(t *testing.T) {
	exec, home, _ := executorTestFixture(t)
	ollamaPath := filepath.Join(home, "ollama-fake")
	resolvedDigest := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	exec.runner.(*fakeCommandRunner).results[ollamaPath] = process.Result{Status: process.StatusCompleted, ExitCode: 0, Stdout: []byte("ollama version 1.0.0")}

	srv := newOllamaTagsServer(t, []ollamaModelEntry{{Name: "smollm:135m", Digest: resolvedDigest, Size: 145000000}})
	exec.ollamaBaseURL = srv.URL

	op := protocol.TypedOperation{
		Kind: protocol.OpKindEnsureLocalModel,
		EnsureLocalModel: &protocol.EnsureLocalModelParams{
			Runtime: "ollama", ModelRef: "smollm:135m", ResolvedRevision: resolvedDigest,
			ExpectedSizeBytes: 145000000, AllowedSource: "registry.ollama.ai", LicenseReference: "apache-2.0",
		},
	}
	effects, auth := protocol.IntrinsicPolicy(op)
	action := protocol.SetupAction{
		ActionID: "act-001", RecipeID: "recipe.ollama.pull_model", RecipeVersion: "1.0",
		Title: "Pull model", Description: "Pulls smollm:135m",
		Authority: auth, Effects: effects, Operation: &op,
		Preconditions: []protocol.Condition{{
			Kind: protocol.CondKindExecutableVerified,
			ExecutableVerified: &protocol.ExecutableVerifiedOperand{
				CanonicalPath: ollamaPath, ExpectedVersion: "1.0.0",
			},
		}},
		IdempotencyKey: "ollama_pull_smollm",
	}
	// Make the executable_verified precondition itself pass: create a real
	// (fake) executable file at ollamaPath.
	if err := os.WriteFile(ollamaPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write fake ollama binary: %v", err)
	}

	plan := buildActionPlan(t, protocol.TargetAll, action)
	report, err := exec.Apply(context.Background(), plan, plan.PlanDigest, false)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(report.Results) != 1 || report.Results[0].Status != protocol.ActionStatusSucceeded {
		t.Fatalf("report.Results = %+v, want one succeeded", report.Results)
	}

	// Two calls are expected: the executable_verified precondition's
	// --version check, then the actual `pull`. Both must run the exact
	// verified path, never a bare "ollama" resolved from PATH.
	runner := exec.runner.(*fakeCommandRunner)
	if len(runner.calls) != 2 {
		t.Fatalf("runner.calls = %+v, want exactly 2 calls (--version precondition check, then pull)", runner.calls)
	}
	for i, call := range runner.calls {
		if call.Executable != ollamaPath {
			t.Errorf("runner.calls[%d].Executable = %q, want the exact verified path %q, not a bare \"ollama\"", i, call.Executable, ollamaPath)
		}
	}
	if runner.calls[1].Args[0] != "pull" {
		t.Errorf("runner.calls[1].Args = %v, want the pull invocation", runner.calls[1].Args)
	}
}

func TestExecutorTerminalizesActionOnPostconditionEvaluatorError(t *testing.T) {
	exec, _, _ := executorTestFixture(t)

	op := protocol.TypedOperation{
		Kind: protocol.OpKindCreateDirectory,
		CreateDirectory: &protocol.CreateDirectoryParams{
			Location:    protocol.LocationTmp,
			FileModeOct: "0700",
		},
	}
	effects, auth := protocol.IntrinsicPolicy(op)
	action := protocol.SetupAction{
		ActionID:      "act-001",
		RecipeID:      "recipe.mkdir.tmp",
		RecipeVersion: "1.0",
		Title:         "Create tmp dir",
		Description:   "Creates the managed tmp directory",
		Authority:     auth,
		Effects:       effects,
		Operation:     &op,
		// This postcondition can never be evaluated (unsupported runtime),
		// so EvaluateCondition returns an error, not just "not satisfied".
		Postconditions: []protocol.Condition{{
			Kind: protocol.CondKindModelPresent,
			ModelPresent: &protocol.ModelPresentOperand{
				Runtime:          "some-unsupported-runtime",
				ModelRef:         "model:latest",
				ResolvedRevision: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
		}},
		IdempotencyKey: "create_dir_tmp_eval_error",
	}
	plan := buildActionPlan(t, protocol.TargetAll, action)

	report, err := exec.Apply(context.Background(), plan, plan.PlanDigest, false)
	if err == nil {
		t.Fatal("Apply succeeded despite a postcondition evaluator error; expected an error")
	}
	if report == nil {
		t.Fatal("Apply returned a nil report; expected a partial report with the action terminalized")
	}
	if len(report.Results) != 1 {
		t.Fatalf("report.Results = %+v, want exactly one result", report.Results)
	}
	// The key assertion: the action must be terminal (failed), never left
	// looking like it's still running/started, even though evaluation of
	// its postcondition itself errored rather than just failing.
	if report.Results[0].Status == protocol.ActionStatusRunning || report.Results[0].Status == protocol.ActionStatusInterrupted {
		t.Errorf("report.Results[0].Status = %q, want a terminal status (not running/interrupted)", report.Results[0].Status)
	}
	if report.Results[0].FinishedAt == nil {
		t.Error("report.Results[0].FinishedAt is nil, want set — the action must reach ActionTerminated")
	}
}

func TestAnsiEscapeStripsOSCAndPrivateModeSequences(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"plain color CSI", "\x1b[31mred\x1b[0m text", "red text"},
		{"private mode CSI (cursor hide)", "before\x1b[?25lafter", "beforeafter"},
		{"OSC terminated by BEL (title change)", "before\x1b]0;My Title\x07after", "beforeafter"},
		{"OSC terminated by ST", "before\x1b]0;My Title\x1b\\after", "beforeafter"},
		{"carriage return", "progress\rmore", "progressmore"},
		{"no escapes", "plain text", "plain text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(ansiEscape.ReplaceAll([]byte(tc.input), nil))
			if got != tc.want {
				t.Errorf("stripped %q = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
