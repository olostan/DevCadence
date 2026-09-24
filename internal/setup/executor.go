package setup

import (
	"context"
	"slices"

	"github.com/olostan/DevCadence/internal/artifacts"
	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/protocol"
)

// ExecutorOptions configures an Executor.
type ExecutorOptions struct {
	Runner         CommandRunner
	Home           string
	Ledger         *Ledger
	Cache          *CacheManager
	Artifacts      *artifacts.Store
	Clock          clock.Clock
	IDs            ids.Source
	EndpointHealth EndpointHealthChecker // optional; see conditions.go
}

// Executor implements ADR-0014 §2's two-step approval workflow and §1/§7's
// executor-owned operation dispatch. It is a service-level API — no CLI in
// this WP.
type Executor struct {
	runner         CommandRunner
	home           string
	ledger         *Ledger
	cache          *CacheManager
	artifacts      *artifacts.Store
	clock          clock.Clock
	ids            ids.Source
	endpointHealth EndpointHealthChecker
}

// NewExecutor returns an Executor. Runner, Home, and Ledger are required;
// Cache/Artifacts/EndpointHealth are required only if the plans this
// Executor will Apply actually need them (a plan with no remove_stale_cache
// action never needs Cache, for example) — Apply surfaces a clear error
// from the relevant applier/evaluator if a plan needs a dependency that
// was not configured, rather than NewExecutor guessing what a caller will
// or won't need.
func NewExecutor(opts ExecutorOptions) (*Executor, error) {
	if opts.Runner == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "NewExecutor: Runner is required")
	}
	if opts.Home == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "NewExecutor: Home is required")
	}
	if opts.Ledger == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "NewExecutor: Ledger is required")
	}
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}
	if opts.IDs == nil {
		opts.IDs = ids.NewULIDSource()
	}
	return &Executor{
		runner:         opts.Runner,
		home:           opts.Home,
		ledger:         opts.Ledger,
		cache:          opts.Cache,
		artifacts:      opts.Artifacts,
		clock:          opts.Clock,
		ids:            opts.IDs,
		endpointHealth: opts.EndpointHealth,
	}, nil
}

// CheckPostconditions implements WP-M3B-2's PostconditionChecker interface
// over this Executor's own dependencies, so Ledger.ReconcileInterrupted can
// use the same live condition evaluation Apply itself uses.
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
	return EvaluatorDeps{Runner: e.runner, Home: e.home, EndpointHealth: e.endpointHealth}
}

func (e *Executor) applierDeps() ApplierDeps {
	return ApplierDeps{Runner: e.runner, Home: e.home, Cache: e.cache, Artifacts: e.artifacts}
}

// Apply implements ADR-0014 §2's two-step approval workflow: plan is only
// ever executed if approvedDigest matches plan's own recomputed digest.
// yesScope is the "--yes"-equivalent authorization: it authorizes only
// user_confirmation-level plans outright and is rejected for anything
// requiring more.
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

	executionID := e.ids.New("exec")
	now := protocol.NewTimestamp(e.clock.Now())

	if _, err := e.ledger.Append(&protocol.SetupLedgerEvent{
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

	if _, err := e.ledger.Append(&protocol.SetupLedgerEvent{
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

	applyErr := e.walk(ctx, plan, executionID)

	finishStatus := protocol.ExecutionStatusSucceeded
	if applyErr != nil {
		finishStatus = protocol.ExecutionStatusFailed
	}
	if _, err := e.ledger.Append(&protocol.SetupLedgerEvent{
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

	events, err := e.ledger.Events()
	if err != nil {
		return nil, err
	}
	report, reportErr := ProjectExecutionReport(events, executionID, plan)
	if reportErr != nil {
		return nil, reportErr
	}
	return report, applyErr
}

// walk executes plan's actions in order, halting (returning a non-nil
// error) the moment a precondition has drifted or an action fails —
// neither continues to any later action.
func (e *Executor) walk(ctx context.Context, plan *protocol.SetupPlan, executionID string) error {
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
		if _, err := e.ledger.Append(&protocol.SetupLedgerEvent{
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
			if _, err := e.ledger.Append(&protocol.SetupLedgerEvent{
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
		_, _, procResult, artifact, opErr := ApplyOperation(ctx, e.applierDeps(), *action.Operation, captureOutput)

		if procResult != nil {
			outputTruncated := procResult.StdoutTruncated || procResult.StderrTruncated
			if artifact != nil {
				outputTruncated = outputTruncated || artifact.Truncated
			}
			if _, err := e.ledger.Append(&protocol.SetupLedgerEvent{
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
		for _, cond := range action.Postconditions {
			passed, pcDetail, pcErr := EvaluateCondition(ctx, deps, cond)
			if pcErr != nil {
				return pcErr
			}
			if _, err := e.ledger.Append(&protocol.SetupLedgerEvent{
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
		}

		status := protocol.ActionStatusSucceeded
		if !postconditionsPassed {
			status = protocol.ActionStatusFailed
		}
		if _, err := e.ledger.Append(&protocol.SetupLedgerEvent{
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

		if !postconditionsPassed {
			if opErr != nil {
				return opErr
			}
			return errs.New(errs.CategoryConflict, "Executor.Apply: action %q postconditions failed", action.ActionID)
		}
	}

	return nil
}
