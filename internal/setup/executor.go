package setup

import (
	"context"
	"path/filepath"
	"slices"

	"github.com/olostan/DevCadence/internal/artifacts"
	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/protocol"
)

// ExecutorOptions configures an Executor. Only Runner is a test-facing
// seam (it must be fake in tests, never a real subprocess runner); Home is
// the one thing every other dependency (ledger, execution lock, cache,
// artifact store) is derived from internally, so the executor's own
// machine-global operational state can never be rooted somewhere other
// than $DEVCADENCE_HOME (ADR-0014 §4) by construction, not by caller
// discipline.
type ExecutorOptions struct {
	Runner         CommandRunner
	Home           string
	Clock          clock.Clock
	IDs            ids.Source
	EndpointHealth EndpointHealthChecker // optional; see conditions.go
	// ModelRuntimes overrides the local model runtime adapter set; nil
	// means DefaultModelRuntimeAdapters() (Ollama and MLX as equal
	// peers). Tests set this to register a fake adapter, or to configure
	// a real adapter's own settings (e.g. OllamaAdapter{BaseURL: ...}) —
	// this is the only knob the executor exposes for that: it has no
	// runtime-specific fields of its own (see modelruntime.go).
	ModelRuntimes *ModelRuntimeRegistry
}

// Executor implements ADR-0014 §2's two-step approval workflow and §1/§7's
// executor-owned operation dispatch. It is a service-level API — no CLI in
// this WP.
type Executor struct {
	runner         CommandRunner
	home           string
	cache          *CacheManager
	artifacts      *artifacts.Store
	clock          clock.Clock
	ids            ids.Source
	endpointHealth EndpointHealthChecker
	modelRuntimes  *ModelRuntimeRegistry
}

// NewExecutor returns an Executor rooted at an absolute Home. Cache and the
// artifact store are constructed here, from Home, rather than accepted as
// injected dependencies — the previous shape let a caller root them
// somewhere unrelated to Home, silently breaking ADR-0014 §4's
// machine-global-state confinement.
func NewExecutor(opts ExecutorOptions) (*Executor, error) {
	if opts.Runner == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "NewExecutor: Runner is required")
	}
	if opts.Home == "" || !filepath.IsAbs(opts.Home) {
		return nil, errs.New(errs.CategoryInvalidArgument, "NewExecutor: Home must be an absolute path, got %q", opts.Home)
	}
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}
	if opts.IDs == nil {
		opts.IDs = ids.NewULIDSource()
	}
	if opts.ModelRuntimes == nil {
		opts.ModelRuntimes = NewModelRuntimeRegistry(DefaultModelRuntimeAdapters()...)
	}

	cache, err := NewCacheManager(filepath.Join(opts.Home, "state"), opts.Clock, 0)
	if err != nil {
		return nil, err
	}
	store, err := artifacts.NewStore(filepath.Join(opts.Home, "artifacts", "setup"), opts.IDs)
	if err != nil {
		return nil, err
	}

	return &Executor{
		runner:         opts.Runner,
		home:           opts.Home,
		cache:          cache,
		artifacts:      store,
		clock:          opts.Clock,
		ids:            opts.IDs,
		endpointHealth: opts.EndpointHealth,
		modelRuntimes:  opts.ModelRuntimes,
	}, nil
}

// CheckPostconditions implements WP-M3B-2's PostconditionChecker interface
// over this Executor's own dependencies, so Ledger.ReconcileInterrupted can
// use the same live condition evaluation Apply itself uses. It does not
// touch the ledger or the execution lock — it is pure condition evaluation.
func (e *Executor) CheckPostconditions(ctx context.Context, conditions []protocol.Condition) (bool, string, error) {
	deps := e.evaluatorDeps()
	for _, cond := range conditions {
		passed, detail, err := EvaluateCondition(ctx, deps, cond)
		if err != nil {
			return false, "", err
		}
		if !passed {
			return false, detail, nil
		}
	}
	return true, "all postconditions hold", nil
}

func (e *Executor) evaluatorDeps() EvaluatorDeps {
	return EvaluatorDeps{Runner: e.runner, Home: e.home, EndpointHealth: e.endpointHealth, ModelRuntimes: e.modelRuntimes}
}

func (e *Executor) applierDeps() applierDeps {
	return applierDeps{runner: e.runner, home: e.home, cache: e.cache, artifacts: e.artifacts, modelRuntimes: e.modelRuntimes}
}

// openLockedLedger acquires the whole-home execution lock and opens the
// ledger only after the lock is held, so the ledger's chain tip can never
// be read stale relative to another process's concurrent writes (a ledger
// opened before the lock is acquired could miss an append that happened in
// the gap). Callers must release the returned lock (defer lock.Release())
// once done with the ledger.
func (e *Executor) openLockedLedger() (*ExecutionLock, *Ledger, error) {
	lock, err := AcquireExecutionLock(e.home)
	if err != nil {
		return nil, nil, err
	}
	ledger, err := OpenLedger(ledgerPath(e.home))
	if err != nil {
		_ = lock.Release()
		return nil, nil, err
	}
	return lock, ledger, nil
}

// Apply implements ADR-0014 §2's two-step approval workflow: plan is only
// ever executed if approvedDigest matches plan's own recomputed digest.
// yesScope is the "--yes"-equivalent authorization: it authorizes only
// user_confirmation-level plans outright and is rejected for anything
// requiring more.
//
// Apply refuses to start a new execution while any interrupted action from
// a prior run remains unreconciled in the ledger — call Recover first.
// This is enforced here, under the same execution lock, rather than left
// as a convention a caller could skip.
//
// plan.Actions is walked in its existing slice order, which
// SetupPlan.Validate() already guarantees is a valid topological order
// (DependsOn may only reference strictly earlier actions) — no separate
// sort is needed.
func (e *Executor) Apply(ctx context.Context, plan *protocol.SetupPlan, approvedDigest string, yesScope bool) (*protocol.SetupExecutionReport, error) {
	if plan == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "Executor.Apply: plan is required")
	}
	if err := plan.Validate(); err != nil {
		return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "Executor.Apply: plan failed validation")
	}
	if approvedDigest != plan.PlanDigest {
		return nil, errs.New(errs.CategoryInvalidArgument,
			"Executor.Apply: approved digest %q does not match plan digest %q", approvedDigest, plan.PlanDigest)
	}
	if yesScope && plan.RequiredAuthority.Rank() > protocol.AuthorityUserConfirmation.Rank() {
		return nil, errs.New(errs.CategoryPolicyDenied,
			"Executor.Apply: --yes-equivalent scope rejected: plan requires authority %q, which exceeds user_confirmation",
			plan.RequiredAuthority)
	}

	lock, ledger, err := e.openLockedLedger()
	if err != nil {
		return nil, err
	}
	defer lock.Release()

	existingEvents, err := ledger.Events()
	if err != nil {
		return nil, err
	}
	if interrupted := FindInterrupted(existingEvents); len(interrupted) > 0 {
		return nil, errs.New(errs.CategoryConflict,
			"Executor.Apply: %d interrupted action(s) from a prior run must be reconciled via Recover before a new execution can start", len(interrupted))
	}

	executionID := e.ids.New("exec")
	now := protocol.NewTimestamp(e.clock.Now())

	if _, err := ledger.Append(&protocol.SetupLedgerEvent{
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       e.ids.New("evt"),
		ExecutionID:   executionID,
		PlanID:        plan.PlanID,
		PlanDigest:    plan.PlanDigest,
		Timestamp:     now,
		Type:          protocol.EventExecutionCreated,
		Payload: protocol.EventPayload{
			ExecutionCreated: &protocol.ExecutionCreatedPayload{
				InitiatedBy: "executor",
				Target:      plan.Target,
			},
		},
	}); err != nil {
		return nil, err
	}

	if _, err := ledger.Append(&protocol.SetupLedgerEvent{
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       e.ids.New("evt"),
		ExecutionID:   executionID,
		PlanID:        plan.PlanID,
		PlanDigest:    plan.PlanDigest,
		Timestamp:     protocol.NewTimestamp(e.clock.Now()),
		Type:          protocol.EventPlanApproved,
		Payload: protocol.EventPayload{
			PlanApproved: &protocol.PlanApprovedPayload{
				ApprovedAuthority: plan.RequiredAuthority,
				ApprovedBy:        "executor",
			},
		},
	}); err != nil {
		return nil, err
	}

	applyErr := e.walk(ctx, ledger, plan, executionID)

	finishStatus := protocol.ExecutionStatusSucceeded
	if applyErr != nil {
		finishStatus = protocol.ExecutionStatusFailed
	}
	if _, err := ledger.Append(&protocol.SetupLedgerEvent{
		SchemaVersion: protocol.SchemaVersion1,
		EventID:       e.ids.New("evt"),
		ExecutionID:   executionID,
		PlanID:        plan.PlanID,
		PlanDigest:    plan.PlanDigest,
		Timestamp:     protocol.NewTimestamp(e.clock.Now()),
		Type:          protocol.EventExecutionFinished,
		Payload: protocol.EventPayload{
			ExecutionFinished: &protocol.ExecutionFinishedPayload{Status: finishStatus},
		},
	}); err != nil {
		return nil, err
	}

	events, err := ledger.Events()
	if err != nil {
		return nil, err
	}
	report, reportErr := ProjectExecutionReport(events, executionID, plan)
	if reportErr != nil {
		return nil, reportErr
	}
	return report, applyErr
}

// Recover durably resolves every interrupted action in the ledger that
// belongs to plan (matched by plan_id and plan_digest, so a stale or wrong
// plan can never be used to resolve another plan's interrupted actions):
// it looks up each interrupted action's Postconditions from plan itself
// and calls Ledger.ReconcileInterrupted, which checks them live (never
// re-executing the action) and durably appends the resolution. This is the
// service-level crash-recovery entry point WP-M3B-2 defined the
// primitives for; Apply refuses to start a new execution while any
// interrupted action remains unreconciled, so a caller resuming after a
// crash must call Recover before its next Apply.
func (e *Executor) Recover(ctx context.Context, plan *protocol.SetupPlan) ([]protocol.ActionStatus, error) {
	if plan == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "Executor.Recover: plan is required")
	}
	if err := plan.Validate(); err != nil {
		return nil, errs.Wrap(errs.CategoryInvalidArgument, err, "Executor.Recover: plan failed validation")
	}

	lock, ledger, err := e.openLockedLedger()
	if err != nil {
		return nil, err
	}
	defer lock.Release()

	events, err := ledger.Events()
	if err != nil {
		return nil, err
	}

	var statuses []protocol.ActionStatus
	for _, interrupted := range FindInterrupted(events) {
		if interrupted.PlanID != plan.PlanID || interrupted.PlanDigest != plan.PlanDigest {
			continue
		}
		action := findActionByID(plan, interrupted.ActionID)
		if action == nil {
			return statuses, errs.New(errs.CategoryNotFound,
				"Executor.Recover: plan %q has no action %q, but the ledger records it as interrupted", plan.PlanID, interrupted.ActionID)
		}
		now := protocol.NewTimestamp(e.clock.Now())
		status, err := ledger.ReconcileInterrupted(ctx, e, interrupted, action.Postconditions, e.ids.New("evt"), e.ids.New("evt"), now)
		if err != nil {
			return statuses, err
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func findActionByID(plan *protocol.SetupPlan, actionID string) *protocol.SetupAction {
	for i := range plan.Actions {
		if plan.Actions[i].ActionID == actionID {
			return &plan.Actions[i]
		}
	}
	return nil
}

// verifiedExecutablePath returns the canonical_path of the first
// executable_verified precondition among preconditions, or "" if none —
// see applyOperation's doc comment for why an applier that runs a specific
// binary must be given this rather than re-resolving a bare name.
func verifiedExecutablePath(preconditions []protocol.Condition) string {
	for _, cond := range preconditions {
		if cond.Kind == protocol.CondKindExecutableVerified && cond.ExecutableVerified != nil {
			return cond.ExecutableVerified.CanonicalPath
		}
	}
	return ""
}

// walk executes plan's actions in order, halting (returning a non-nil
// error) the moment a precondition has drifted or an action fails —
// neither continues to any later action. Every action reaches a terminal
// ActionTerminated event before walk returns, including one halted by a
// postcondition *evaluator* error (not just a failed postcondition) — an
// execution never finishes with an action still sitting in ActionStarting.
func (e *Executor) walk(ctx context.Context, ledger *Ledger, plan *protocol.SetupPlan, executionID string) error {
	deps := e.evaluatorDeps()

	for i := range plan.Actions {
		action := &plan.Actions[i]

		for _, cond := range action.Preconditions {
			passed, detail, err := EvaluateCondition(ctx, deps, cond)
			if err != nil {
				return err
			}
			if !passed {
				return errs.New(errs.CategoryConflict,
					"Executor.Apply: precondition drift on action %q: %s", action.ActionID, detail)
			}
		}

		var operationKind *protocol.OperationKind
		if action.Operation != nil {
			k := action.Operation.Kind
			operationKind = &k
		}
		if _, err := ledger.Append(&protocol.SetupLedgerEvent{
			SchemaVersion: protocol.SchemaVersion1,
			EventID:       e.ids.New("evt"),
			ExecutionID:   executionID,
			PlanID:        plan.PlanID,
			PlanDigest:    plan.PlanDigest,
			ActionID:      action.ActionID,
			Timestamp:     protocol.NewTimestamp(e.clock.Now()),
			Type:          protocol.EventActionStarting,
			Payload: protocol.EventPayload{
				ActionStarting: &protocol.ActionStartingPayload{
					ActionID:       action.ActionID,
					RecipeID:       action.RecipeID,
					RecipeVersion:  action.RecipeVersion,
					OperationKind:  operationKind,
					IdempotencyKey: action.IdempotencyKey,
				},
			},
		}); err != nil {
			return err
		}

		if action.ManualInstructions != nil {
			if _, err := ledger.Append(&protocol.SetupLedgerEvent{
				SchemaVersion: protocol.SchemaVersion1,
				EventID:       e.ids.New("evt"),
				ExecutionID:   executionID,
				PlanID:        plan.PlanID,
				PlanDigest:    plan.PlanDigest,
				ActionID:      action.ActionID,
				Timestamp:     protocol.NewTimestamp(e.clock.Now()),
				Type:          protocol.EventActionTerminated,
				Payload: protocol.EventPayload{
					ActionTerminated: &protocol.ActionTerminatedPayload{
						ActionID: action.ActionID,
						Status:   protocol.ActionStatusManualRequired,
					},
				},
			}); err != nil {
				return err
			}
			// No automated path exists past a manual step: stop the whole
			// walk rather than assume any later action is safe to run.
			return errs.New(errs.CategoryPolicyDenied,
				"Executor.Apply: action %q requires manual completion; halting", action.ActionID)
		}

		captureOutput := !slices.Contains(action.Effects, protocol.EffectAuthentication)
		verifiedPath := verifiedExecutablePath(action.Preconditions)
		_, _, procResult, artifact, opErr := applyOperation(ctx, e.applierDeps(), *action.Operation, captureOutput, verifiedPath)

		if procResult != nil {
			outputTruncated := procResult.StdoutTruncated || procResult.StderrTruncated
			if artifact != nil {
				outputTruncated = outputTruncated || artifact.Truncated
			}
			if _, err := ledger.Append(&protocol.SetupLedgerEvent{
				SchemaVersion: protocol.SchemaVersion1,
				EventID:       e.ids.New("evt"),
				ExecutionID:   executionID,
				PlanID:        plan.PlanID,
				PlanDigest:    plan.PlanDigest,
				ActionID:      action.ActionID,
				Timestamp:     protocol.NewTimestamp(e.clock.Now()),
				Type:          protocol.EventActionProcessCompleted,
				Payload: protocol.EventPayload{
					ActionProcessCompleted: &protocol.ActionProcessCompletedPayload{
						ActionID:        action.ActionID,
						ExitCode:        procResult.ExitCode,
						Signal:          procResult.Signal,
						OutputTruncated: outputTruncated,
						ArtifactRef:     artifact,
					},
				},
			}); err != nil {
				return err
			}
		}

		postconditionsPassed := opErr == nil
		var pcEvalErr error
		for _, cond := range action.Postconditions {
			passed, pcDetail, pcErr := EvaluateCondition(ctx, deps, cond)
			if pcErr != nil {
				pcEvalErr = pcErr
				passed = false
				pcDetail = pcErr.Error()
			}
			if _, err := ledger.Append(&protocol.SetupLedgerEvent{
				SchemaVersion: protocol.SchemaVersion1,
				EventID:       e.ids.New("evt"),
				ExecutionID:   executionID,
				PlanID:        plan.PlanID,
				PlanDigest:    plan.PlanDigest,
				ActionID:      action.ActionID,
				Timestamp:     protocol.NewTimestamp(e.clock.Now()),
				Type:          protocol.EventPostconditionVerified,
				Payload: protocol.EventPayload{
					PostconditionVerified: &protocol.PostconditionVerifiedPayload{
						ActionID: action.ActionID,
						Passed:   passed,
						Detail:   pcDetail,
					},
				},
			}); err != nil {
				return err
			}
			if !passed {
				postconditionsPassed = false
			}
			if pcEvalErr != nil {
				// An evaluator failure (as opposed to a condition simply
				// not holding) means later postconditions can't be
				// meaningfully checked either; stop checking, but the
				// action still gets terminalized below before we return.
				break
			}
		}

		status := protocol.ActionStatusSucceeded
		if !postconditionsPassed {
			status = protocol.ActionStatusFailed
		}
		if _, err := ledger.Append(&protocol.SetupLedgerEvent{
			SchemaVersion: protocol.SchemaVersion1,
			EventID:       e.ids.New("evt"),
			ExecutionID:   executionID,
			PlanID:        plan.PlanID,
			PlanDigest:    plan.PlanDigest,
			ActionID:      action.ActionID,
			Timestamp:     protocol.NewTimestamp(e.clock.Now()),
			Type:          protocol.EventActionTerminated,
			Payload: protocol.EventPayload{
				ActionTerminated: &protocol.ActionTerminatedPayload{
					ActionID: action.ActionID,
					Status:   status,
				},
			},
		}); err != nil {
			return err
		}

		if pcEvalErr != nil {
			return pcEvalErr
		}
		if !postconditionsPassed {
			if opErr != nil {
				return opErr
			}
			return errs.New(errs.CategoryConflict, "Executor.Apply: action %q postconditions failed", action.ActionID)
		}
	}

	return nil
}
