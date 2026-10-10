package taskexec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/olostan/DevCadence/internal/actors"
	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/execpolicy"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/principal/facade"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/storage"
	"github.com/olostan/DevCadence/internal/tasks"
	"github.com/olostan/DevCadence/internal/tools"
	"github.com/olostan/DevCadence/internal/worktrees"
)

// Delegate coordinates synchronous pre-checks, compiler admission, pre-effect batch commit,
// and asynchronous worker attempt execution.
func (e *Executor) Delegate(ctx context.Context, task facade.AuthorizedTask) (principal.OperationRef, error) {
	// 1. Sync checks
	if task.Caller.ProjectID != e.opts.ProjectID || task.Meta.ProjectID != e.opts.ProjectID {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodePolicyDenied,
			false,
			[]string{"project-mismatch"},
			fmt.Sprintf("project mismatch: executor owns %q, request has %q", e.opts.ProjectID, task.Meta.ProjectID),
		)
	}

	taskList, err := e.opts.ControlPlane.Tasks(ctx, storage.TaskFilter{ProjectID: e.opts.ProjectID})
	if err != nil {
		return principal.OperationRef{}, err
	}

	var foundTask *tasks.Task
	for _, t := range taskList {
		if t.ID == task.TaskID {
			foundTask = t
			break
		}
	}
	if foundTask == nil {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodeNotFound,
			false,
			[]string{"task-not-found"},
			fmt.Sprintf("task %q not found", task.TaskID),
		)
	}

	if foundTask.State != tasks.StateReady && foundTask.State != tasks.StateRunning {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodePolicyDenied,
			false,
			[]string{"invalid-task-state"},
			fmt.Sprintf("task %s in state %s cannot be delegated", foundTask.Alias, foundTask.State),
		)
	}

	detail, err := e.opts.ControlPlane.TaskDetail(ctx, e.opts.ProjectID, foundTask.Alias)
	if err != nil {
		return principal.OperationRef{}, err
	}

	if foundTask.State == tasks.StateRunning {
		for _, att := range detail.Attempts {
			if att.Status == tasks.AttemptRunning {
				return principal.OperationRef{}, principal.NewCodedError(
					principal.CodePolicyDenied,
					false,
					[]string{"attempt-already-running"},
					fmt.Sprintf("task %s already has a running attempt %s", foundTask.Alias, att.ID),
				)
			}
		}
	}

	if foundTask.WorkPackageID != task.WorkPackage.ID || foundTask.WorkPackageVersion != task.WorkPackage.Version {
		return principal.OperationRef{}, controlplane.ErrStaleWorkPackage
	}

	stored, err := e.opts.ControlPlane.Record(ctx, e.opts.ProjectID, "EngineeringWorkPackage", task.WorkPackage.ID, task.WorkPackage.Version)
	if err != nil {
		return principal.OperationRef{}, err
	}
	if stored.Digest != task.WorkPackage.Digest {
		return principal.OperationRef{}, controlplane.ErrStaleWorkPackage
	}

	var wp protocol.EngineeringWorkPackage
	if err := protocol.Unmarshal([]byte(stored.Document), &wp); err != nil {
		return principal.OperationRef{}, errs.Wrap(errs.CategoryIntegrity, err, "failed to decode stored work package")
	}
	if wp.BaseCommit != task.WorkPackage.BaseCommit {
		return principal.OperationRef{}, controlplane.ErrStaleWorkPackage
	}

	ps, err := e.opts.ControlPlane.ProjectState(ctx, e.opts.ProjectID)
	if err != nil {
		return principal.OperationRef{}, err
	}
	P := ps.StateRevision
	if task.Meta.ExpectedStateRevision != "" && task.Meta.ExpectedStateRevision != P {
		return principal.OperationRef{}, controlplane.ErrStaleProjectState
	}

	// 2. Policy
	policy, policyDigest, err := e.opts.Policy.Current(ctx)
	if err != nil {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodeModelUnavailable,
			false,
			[]string{"execution-policy-unavailable"},
			fmt.Sprintf("execution policy unavailable: %v", err),
		)
	}

	// 2b. A prior attempt whose effects are uncertain (the model or worktree may
	// have acted in a way DevCadence could not observe) must not be silently
	// retried with a second model call.
	for _, att := range detail.Attempts {
		if hasUncertainEffects(att.FailureSummary) {
			return principal.OperationRef{}, principal.NewCodedError(
				principal.CodePolicyDenied,
				false,
				[]string{"uncertain_prior_attempt"},
				fmt.Sprintf("task %s attempt %s ended with uncertain effects (%q); not retrying automatically. "+
					"Inspect the attempt and its worktree manually; to continue, create a new task or work package (or re-delegate under a new task id)", foundTask.Alias, att.ID, att.FailureSummary),
			)
		}
	}

	// 3. Attempt budget from policy
	if len(detail.Attempts) >= policy.MaxAttemptsPerTask {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodePolicyDenied,
			false,
			[]string{"attempt-budget-exhausted"},
			fmt.Sprintf("task %s attempt budget exhausted (%d >= %d)", foundTask.Alias, len(detail.Attempts), policy.MaxAttemptsPerTask),
		)
	}

	// 3b. Post-check profile: loaded and vetted before any model call.
	postCheck, err := e.preparePostCheck(ctx)
	if err != nil {
		return principal.OperationRef{}, err
	}

	// 4. Resolve endpoint and probe driver
	req := execpolicy.EndpointRequest{
		ProjectID:   e.opts.ProjectID,
		TaskID:      task.TaskID,
		Role:        "implementer",
		ContextNeed: protocol.ExposureFocusedSnippets,
	}
	ep, err := e.opts.Resolver.Resolve(ctx, req, policy, policyDigest)
	if err != nil {
		return principal.OperationRef{}, err
	}

	if e.opts.Drivers == nil {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodeModelUnavailable,
			false,
			[]string{"endpoint-unavailable"},
			"no driver factory configured",
		)
	}

	opened, err := e.opts.Drivers.Open(ctx, ep)
	if err != nil {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodeModelUnavailable,
			false,
			[]string{"endpoint-unavailable"},
			fmt.Sprintf("failed to open endpoint: %v", err),
		)
	}
	if opened.Driver == nil {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodeModelUnavailable,
			false,
			[]string{"endpoint-unavailable"},
			"opened driver is nil",
		)
	}

	if err := execpolicy.AssertDriverID(opened.Driver.ID(), opened.Observed); err != nil {
		return principal.OperationRef{}, err
	}

	ep, err = execpolicy.Bind(ep, opened.Observed)
	if err != nil {
		return principal.OperationRef{}, mapBindError(err)
	}

	// 5. Compiler admission
	contractText := wp.Objective
	if strings.TrimSpace(contractText) == "" {
		contractText = stored.Document
	}
	writeScope := wp.Scope.InScope
	readEnvelope := wp.Scope.InScope
	if len(readEnvelope) == 0 {
		readEnvelope = []string{"**"}
	}
	toolNames, declaredTools := workerToolDeclarations(e.opts.ExecutionMode)
	compileReq := compiler.CompileRequest{
		TaskID:               task.TaskID,
		WorkPackageID:        task.WorkPackage.ID,
		WorkPackageRevision:  task.WorkPackage.Version,
		WorkPackageDigest:    task.WorkPackage.Digest,
		Role:                 "implementer",
		BaseCommit:           wp.BaseCommit,
		SourceRevision:       wp.BaseCommit,
		WriteScope:           writeScope,
		ReadEnvelope:         readEnvelope,
		ExecutionContract:    contractText,
		ContextProfile:       &ep.ContextProfile,
		AccessChannel:        &ep.Channel,
		Tools:                toolNames,
		DeclaredTools:        declaredTools,
		BudgetPoolID:         "pool-default",
		ProjectStateRevision: P,
		Action:               "Implement task " + task.TaskID,
	}

	compiled, err := e.opts.Compiler.CompileInvocation(ctx, compileReq)
	if err != nil {
		return principal.OperationRef{}, err
	}

	// 6. Attempt ID & Provenance
	attemptID := e.opts.IDs.New("att")
	actorID, err := actors.DeriveActorID("endpoint_model", ep.ActorBasis())
	if err != nil {
		return principal.OperationRef{}, err
	}

	promptDigest := compiled.InvocationDigest
	if promptDigest == "" {
		promptDigest = compiled.Pack.PackDigest
	}

	manifestBytes, err := protocol.CanonicalJSON(compiled.Manifest)
	if err != nil {
		return principal.OperationRef{}, err
	}
	manifestDigest := protocol.DigestBytes(manifestBytes)

	prov := &protocol.InvocationProvenance{
		SchemaVersion: protocol.SchemaVersion1,
		ProvenanceID:  fmt.Sprintf("prov_%s", attemptID),
		ProjectID:     e.opts.ProjectID,
		TaskID:        task.TaskID,
		AttemptID:     attemptID,
		WorkPackageID: task.WorkPackage.ID,
		Role:          protocol.ProvenanceRoleImplementer,
		Actor: protocol.ActorProvenance{
			ActorID:      actorID,
			InvocationID: attemptID,
			Role:         protocol.ProvenanceRoleImplementer,
		},
		Basis:                 ep.ActorBasis(),
		IndependenceBasis:     "endpoint_model",
		EndpointBindingDigest: ep.BindingDigest,
		ContextManifestDigest: manifestDigest,
		PromptDigest:          promptDigest,
		StartedAt:             e.opts.Clock.Now(),
	}

	// 7. Check prefix before batch (prefix is not refreshed)
	psNow, err := e.opts.ControlPlane.ProjectState(ctx, e.opts.ProjectID)
	if err != nil || psNow.StateRevision != P {
		return principal.OperationRef{}, controlplane.ErrStaleProjectState
	}

	attemptCmd := controlplane.Command{
		ProjectID: e.opts.ProjectID,
		Actor:     protocol.Actor{Kind: protocol.ActorControlPlane, ID: "taskexec"},
		Payload: &events.AttemptStarted{
			TaskID:               task.TaskID,
			AttemptID:            attemptID,
			WorkPackageID:        task.WorkPackage.ID,
			WorkPackageVersion:   task.WorkPackage.Version,
			ProjectStateRevision: P,
			BaseCommit:           wp.BaseCommit,
			WorkerRole:           "implementer",
			ModelIdentity:        fmt.Sprintf("%s/%s@%s", ep.EndpointID, ep.ModelID, ep.ModelRevision),
			WorktreeID:           attemptID,
		},
	}

	batchCmd := controlplane.BatchCommand{
		ProjectID:             e.opts.ProjectID,
		Actor:                 protocol.Actor{Kind: protocol.ActorControlPlane, ID: "taskexec"},
		ExpectedStateRevision: P,
	}

	if foundTask.State == tasks.StateReady {
		batchCmd.WorkPackage = &controlplane.WorkPackageGuard{
			TaskID:        task.TaskID,
			WorkPackageID: task.WorkPackage.ID,
			Version:       task.WorkPackage.Version,
			Digest:        task.WorkPackage.Digest,
			BaseCommit:    wp.BaseCommit,
		}
		delegatedCmd := controlplane.Command{
			ProjectID: e.opts.ProjectID,
			Actor:     protocol.Actor{Kind: protocol.ActorControlPlane, ID: "taskexec"},
			Payload: &events.TaskDelegated{
				TaskID:        task.TaskID,
				WorkPackageID: task.WorkPackage.ID,
				WorkerRole:    "implementer",
				MaxAttempts:   policy.MaxAttemptsPerTask,
			},
		}
		batchCmd.Commands = []controlplane.Command{delegatedCmd, attemptCmd}
	} else {
		batchCmd.Commands = []controlplane.Command{attemptCmd}
	}

	if _, err := e.opts.ControlPlane.ApplyBatch(ctx, batchCmd); err != nil {
		return principal.OperationRef{}, err
	}

	// 8. Start asynchronous run
	deadline := time.Duration(ep.Limits.MaxDurationSeconds)*time.Second + 60*time.Second
	opRef, err := e.opts.Registry.Start(e.opts.ProjectID, "delegate", deadline, func(opCtx context.Context) (string, error) {
		return e.runDelegate(opCtx, task.TaskID, foundTask.Alias, attemptID, wp, ep, opened, compiled, prov, postCheck)
	})
	if err != nil {
		// Operation registry failed to schedule
		freshCtx := context.Background()
		if psFresh, psErr := e.opts.ControlPlane.ProjectState(freshCtx, e.opts.ProjectID); psErr == nil {
			_, _ = e.opts.ControlPlane.ApplyBatch(freshCtx, controlplane.BatchCommand{
				ProjectID:             e.opts.ProjectID,
				Actor:                 protocol.Actor{Kind: protocol.ActorControlPlane, ID: "taskexec"},
				ExpectedStateRevision: psFresh.StateRevision,
				Commands: []controlplane.Command{
					{
						ProjectID: e.opts.ProjectID,
						Actor:     protocol.Actor{Kind: protocol.ActorControlPlane, ID: "taskexec"},
						Payload: &events.AttemptFailed{
							TaskID:    task.TaskID,
							AttemptID: attemptID,
							Summary:   "reason=operation_registry_full effects=none",
							Cancelled: false,
						},
					},
				},
			})
		}
		return principal.OperationRef{}, err
	}

	return opRef, nil
}

func (e *Executor) runDelegate(
	ctx context.Context,
	taskID, taskAlias, attemptID string,
	wp protocol.EngineeringWorkPackage,
	ep execpolicy.ResolvedEndpoint,
	opened execpolicy.OpenedEndpoint,
	compiled *compiler.CompiledInvocation,
	prov *protocol.InvocationProvenance,
	postCheck *postCheck,
) (string, error) {
	if postCheck != nil {
		e.postChecks.Store(attemptID, postCheck)
		defer e.postChecks.Delete(attemptID)
	}
	// 1. Create Worktree
	repo, err := e.opts.Repositories.Repository(ctx, e.opts.ProjectID)
	if err != nil {
		return e.failAttempt(taskID, attemptID, "reason=worktree_unavailable effects=none", false, 0, nil, principal.CodeModelUnavailable, err)
	}

	wt, err := e.opts.Worktrees.Create(ctx, repo, worktrees.Spec{
		ProjectID:  e.opts.ProjectID,
		TaskID:     taskID,
		AttemptID:  attemptID,
		BaseCommit: wp.BaseCommit,
	})
	if err != nil {
		return e.failAttempt(taskID, attemptID, "reason=worktree_unavailable effects=none", false, 0, nil, principal.CodeModelUnavailable, err)
	}

	// 2. Validate Limits
	if ep.Limits.MaxTurns <= 0 ||
		ep.Limits.MaxToolCalls <= 0 ||
		ep.Limits.MaxTotalTokens <= 0 ||
		ep.Limits.MaxDurationSeconds <= 0 ||
		ep.Limits.MaxOutputTokensPerCall <= 0 ||
		ep.Limits.MaxRequestBytes <= 0 {
		return e.failAttempt(taskID, attemptID, "reason=endpoint_unavailable effects=none", false, 0, nil, principal.CodeModelUnavailable, errors.New("zero required execution limit"))
	}

	// 3. MeteredDriver & Mediation
	meterLimits := drivers.MeterLimits{
		MaxCumulativeTotalTokens: ep.Limits.MaxTotalTokens,
		MaxCumulativeToolCalls:   ep.Limits.MaxToolCalls,
		MaxCumulativeDuration:    time.Duration(ep.Limits.MaxDurationSeconds) * time.Second,
		AllowUnknownUsage:        ep.Limits.AllowUnknownUsage,
		MaxDurationPerOp:         time.Duration(ep.Limits.MaxDurationSeconds) * time.Second,
	}
	md := drivers.NewMeteredDriver(opened.Driver, meterLimits)
	scope := &tools.Scope{
		ProjectID:    e.opts.ProjectID,
		WorktreeID:   attemptID,
		WorktreePath: wt.Path,
	}
	mediator := drivers.NewScopedToolMediator(scope)
	audit := newCommandAudit(e.opts.ExecutionMode.normalized())
	e.audits.Store(attemptID, audit)
	defer e.audits.Delete(attemptID)
	toolDefs := setupWorkerToolsWith(mediator, scope, e.opts.Runner, wp.Scope.InScope, workerToolConfig{
		Mode: e.opts.ExecutionMode, Audit: audit, Logger: e.opts.Logger,
		ScratchHome: filepath.Join(e.opts.StateDir, "attempts", attemptID, "home"),
	})
	if e.opts.ExecutionMode.normalized() == ExecutionUnsafeUnconfinedLocal {
		e.opts.Logger.Warn("attempt runs with unsafe_unconfined_local execution: run_command executes unconfined as the local user",
			slog.String("task_id", taskID), slog.String("attempt_id", attemptID), slog.String("marker", unsafeUnconfinedMarker))
	}

	// Drivers that were given the mediator execute the model's tool calls inside
	// ExecuteTurn; count those executions so the loop below never runs a call
	// twice (a duplicate run_command or apply_patch is not harmless).
	var execMu sync.Mutex
	driverExecuted := map[string]int{} // tool call ID -> executions this turn
	mediator.OnToolExecution(func(c drivers.ToolCall, _ drivers.ToolResult) {
		execMu.Lock()
		driverExecuted[c.ID]++
		execMu.Unlock()
	})

	// 4. Session Start
	sessionConfig := drivers.SessionConfig{
		SessionID:              attemptID + "-s1",
		ModelID:                ep.ModelID,
		SystemPrompt:           compiled.Projection.SystemPrompt,
		Tools:                  toolDefs,
		WorktreeScope:          scope,
		Mediator:               mediator,
		MaxOutputTokensPerCall: ep.Limits.MaxOutputTokensPerCall,
	}

	session, err := md.StartSession(ctx, sessionConfig)
	if err != nil {
		return e.failAttempt(taskID, attemptID, "reason=driver_error effects=uncertain", false, 0, nil, principal.CodeModelUnavailable, err)
	}
	defer func() {
		_ = session.Close(context.Background())
	}()

	// 5. Turn Loop
	turnInput := drivers.TurnInput{
		TurnID: "turn-1",
		Prompt: compiled.Projection.UserPrompt,
	}

	stoppedNormally := false
	for turn := 1; turn <= ep.Limits.MaxTurns; turn++ {
		if ctx.Err() != nil {
			return e.handleCancel(taskID, attemptID, session, ctx.Err())
		}

		inputBytes := len(turnInput.Prompt)
		for _, tr := range turnInput.ToolResults {
			inputBytes += len(tr.Content)
		}
		if inputBytes > ep.Limits.MaxRequestBytes {
			return e.failAttempt(taskID, attemptID, "reason=limit_reached effects=none", false, 0, nil, principal.CodePolicyDenied, fmt.Errorf("request bytes %d exceeds max_request_bytes %d", inputBytes, ep.Limits.MaxRequestBytes))
		}

		turnRes, err := session.ExecuteTurn(ctx, turnInput)
		if err != nil {
			if ctx.Err() != nil {
				return e.handleCancel(taskID, attemptID, session, ctx.Err())
			}
			if errors.Is(err, drivers.ErrBudgetExceeded) {
				return e.failAttempt(taskID, attemptID, "reason=limit_reached effects=none", false, 0, nil, principal.CodePolicyDenied, err)
			}
			return e.failAttempt(taskID, attemptID, "reason=driver_error effects=uncertain", false, 0, nil, principal.CodeModelUnavailable, err)
		}

		if turnRes.PausedReason != "" || md.GetMeter(sessionConfig.SessionID).IsPaused() {
			return e.failAttempt(taskID, attemptID, "reason=limit_reached effects=none", false, 0, nil, principal.CodePolicyDenied, errors.New("meter paused budget exceeded"))
		}

		// Unknown usage checks
		if !turnRes.Usage.Input.Known || !turnRes.Usage.Output.Known {
			if !ep.Limits.AllowUnknownUsage || ep.Locality != protocol.LocalityLocal {
				return e.failAttempt(taskID, attemptID, "reason=limit_reached effects=none", false, 0, nil, principal.CodePolicyDenied, errors.New("unknown usage not allowed"))
			}
		}

		if len(turnRes.ToolCalls) == 0 {
			if postCheck == nil {
				stoppedNormally = true
				break
			}
			// The model claims it is done: run the project's validation in the
			// candidate worktree before any candidate commit exists.
			passed, feedback, pcStarted, pcErr := e.runPostCheck(ctx, postCheck, wt.Path, attemptID)
			if pcErr != nil {
				if ctx.Err() != nil {
					return e.handleCancel(taskID, attemptID, session, ctx.Err())
				}
				effects := "none"
				if pcStarted {
					effects = "uncertain" // validation commands may have run unconfined
				}
				return e.failAttempt(taskID, attemptID, "reason=validation_error effects="+effects, false, max(0, len(postCheck.rounds)-1), nil, principal.CodeInternal, pcErr)
			}
			if ctx.Err() != nil {
				return e.handleCancel(taskID, attemptID, session, ctx.Err())
			}
			if passed {
				stoppedNormally = true
				break
			}
			if repairs := len(postCheck.rounds) - 1; repairs >= postCheck.maxRepair {
				return e.failAttempt(taskID, attemptID, "reason=validation_failed effects=none", false, repairs, nil, principal.CodePolicyDenied,
					fmt.Errorf("post-check still failing after %d repair round(s); no candidate produced", repairs))
			}
			turnInput = drivers.TurnInput{TurnID: fmt.Sprintf("turn-%d", turn+1), Prompt: feedback}
			continue
		}

		if ctx.Err() != nil {
			return e.handleCancel(taskID, attemptID, session, ctx.Err())
		}
		execMu.Lock()
		already := driverExecuted
		driverExecuted = map[string]int{}
		execMu.Unlock()

		var toolResults []drivers.ToolResult
		for _, tc := range turnRes.ToolCalls {
			if already[tc.ID] > 0 {
				// Executed (and recorded in the transcript) by the driver; never run twice.
				already[tc.ID]--
				continue
			}
			tr, trErr := mediator.ExecuteTool(ctx, tc)
			if trErr != nil && ctx.Err() != nil {
				return e.handleCancel(taskID, attemptID, session, ctx.Err())
			}
			toolResults = append(toolResults, tr)
		}

		turnInput = drivers.TurnInput{
			TurnID:      fmt.Sprintf("turn-%d", turn+1),
			ToolResults: toolResults,
		}
	}

	if !stoppedNormally {
		return e.failAttempt(taskID, attemptID, "reason=limit_reached effects=none", false, 0, nil, principal.CodePolicyDenied, errors.New("maximum turns reached"))
	}

	// 5b. Remove any leftover apply_patch temp files, then persist the command
	// trace. Failing to persist it while commands ran fails the attempt closed.
	sweepPatchTemps(wt.Path)
	traceRefs, traceErr := e.commandTraceRefs(ctx, audit)
	if traceErr != nil {
		return e.failAttempt(taskID, attemptID, "reason=audit_unavailable effects=uncertain", false, 0, nil, principal.CodeInternal, traceErr)
	}

	// 6. Materialize Candidate Commit
	headCommit, changedPaths, err := e.materializeCandidate(ctx, wt.Path, wp.BaseCommit, wp.Scope.InScope, taskID, attemptID)
	if err != nil {
		return "", err
	}

	// 7. Store Artifacts
	diffRes, err := e.runGit(ctx, wt.Path, "diff", wp.BaseCommit+"..HEAD")
	if err != nil {
		return e.failAttempt(taskID, attemptID, "reason=driver_error effects=uncertain", false, 0, nil, principal.CodeModelUnavailable, err)
	}
	diffRef, err := e.opts.Artifacts.Put(ctx, e.opts.ProjectID, "diff", "text/x-diff", diffRes.Stdout)
	if err != nil {
		return e.failAttempt(taskID, attemptID, "reason=driver_error effects=uncertain", false, 0, nil, principal.CodeModelUnavailable, err)
	}

	meterSnap := md.GetMeter(sessionConfig.SessionID).Checkpoint()
	snapBytes, err := json.Marshal(meterSnap)
	if err != nil {
		return e.failAttempt(taskID, attemptID, "reason=driver_error effects=uncertain", false, 0, nil, principal.CodeModelUnavailable, err)
	}
	usageRef, err := e.opts.Artifacts.Put(ctx, e.opts.ProjectID, "usage", "application/json", snapBytes)
	if err != nil {
		return e.failAttempt(taskID, attemptID, "reason=driver_error effects=uncertain", false, 0, nil, principal.CodeModelUnavailable, err)
	}

	candidateArtifacts := []protocol.ArtifactRef{diffRef, usageRef}
	candidateArtifacts = append(candidateArtifacts, traceRefs...)
	reportRefs, err := e.postCheckRefs(ctx, attemptID)
	if err != nil {
		return e.failAttempt(taskID, attemptID, "reason=audit_unavailable effects=uncertain", false, 0, nil, principal.CodeInternal, err)
	}
	candidateArtifacts = append(candidateArtifacts, reportRefs...)
	reviewRef, err := e.reviewStatusRef(ctx)
	if err != nil {
		return e.failAttempt(taskID, attemptID, "reason=audit_unavailable effects=uncertain", false, 0, nil, principal.CodeInternal, err)
	}
	candidateArtifacts = append(candidateArtifacts, reviewRef)

	// 8. Commit CandidateProduced
	if prov != nil {
		prov.CandidateCommit = headCommit
	}
	freshPS, err := e.opts.ControlPlane.ProjectState(ctx, e.opts.ProjectID)
	if err != nil {
		return "", principal.NewCodedError(principal.CodeInternal, true, []string{"attempt-outcome-unrecorded"}, fmt.Sprintf("failed to get project state: %v", err))
	}

	candCmd := controlplane.Command{
		ProjectID: e.opts.ProjectID,
		Actor:     protocol.Actor{Kind: protocol.ActorControlPlane, ID: "taskexec"},
		Payload: &events.CandidateProduced{
			TaskID:          taskID,
			AttemptID:       attemptID,
			CandidateCommit: headCommit,
			Summary:         fmt.Sprintf("candidate produced: %d files", len(changedPaths)),
			Artifacts:       candidateArtifacts,
		},
	}
	if prov != nil {
		candCmd.Records = []controlplane.RecordToStore{
			{Version: 1, Record: prov},
		}
	}

	batchCmd := controlplane.BatchCommand{
		ProjectID:             e.opts.ProjectID,
		Actor:                 protocol.Actor{Kind: protocol.ActorControlPlane, ID: "taskexec"},
		ExpectedStateRevision: freshPS.StateRevision,
		Preconditions: []controlplane.BatchGuard{
			attemptStillRunningGuard{taskAlias: taskAlias},
		},
		Commands: []controlplane.Command{candCmd},
	}

	if _, err := e.opts.ControlPlane.ApplyBatch(ctx, batchCmd); err != nil {
		return "", principal.NewCodedError(principal.CodeInternal, true, []string{"attempt-outcome-unrecorded"}, fmt.Sprintf("failed applying CandidateProduced: %v", err))
	}

	return fmt.Sprintf("candidate:%s", attemptID), nil
}

// workerToolDeclarations lists the tools the compiler admits for the mode.
// run_command is declared only in the unsafe-unconfined mode.
func workerToolDeclarations(mode ExecutionMode) ([]string, []compiler.ToolCapabilityInfo) {
	names := []string{"read_file", "grep", "symbols", "write_file", applyPatchToolName}
	declared := []compiler.ToolCapabilityInfo{
		{Name: "read_file", ReadOnly: true},
		{Name: "grep", ReadOnly: true},
		{Name: "symbols", ReadOnly: true},
		{Name: "write_file", MutatesFiles: true},
		{Name: applyPatchToolName, MutatesFiles: true},
	}
	if mode.normalized() == ExecutionUnsafeUnconfinedLocal {
		names = append(names, runCommandToolName)
		declared = append(declared, compiler.ToolCapabilityInfo{
			Name: runCommandToolName, RequiredCapabilities: []compiler.CapabilityClass{compiler.CapabilityClassExec},
		})
	}
	return names, declared
}

// commandTraceRefs stores the attempt's run_command audit trace through the
// artifact sink and returns its reference (empty when no command was attempted).
func (e *Executor) commandTraceRefs(ctx context.Context, audit *commandAudit) ([]protocol.ArtifactRef, error) {
	b, err := audit.trace()
	if err != nil || b == nil {
		return nil, err
	}
	ref, err := e.opts.Artifacts.Put(ctx, e.opts.ProjectID, "command-trace", "application/json", b)
	if err != nil {
		return nil, err
	}
	return []protocol.ArtifactRef{ref}, nil
}

func (e *Executor) materializeCandidate(
	ctx context.Context,
	worktreePath string,
	baseCommit string,
	writeScope []string,
	taskID, attemptID string,
) (headCommit string, changedPaths []string, retErr error) {
	// Status check
	statusRes, err := e.runGit(ctx, worktreePath, "status", "--porcelain=v2", "-z")
	if err != nil {
		_, err := e.failAttempt(taskID, attemptID, "reason=driver_error effects=uncertain", false, 0, nil, principal.CodeModelUnavailable, err)
		return "", nil, err
	}

	raw := statusRes.Stdout
	if len(raw) == 0 {
		_, err := e.failAttempt(taskID, attemptID, "reason=no_change effects=none", false, 0, nil, principal.CodePolicyDenied, errors.New("no files changed"))
		return "", nil, err
	}

	// Parse porcelain v2 -z tokens
	tokens := bytes.Split(raw, []byte{0})
	var paths []string
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if len(t) == 0 {
			continue
		}
		s := string(t)
		if strings.HasPrefix(s, "1 ") {
			fields := strings.SplitN(s, " ", 9)
			if len(fields) >= 9 {
				paths = append(paths, fields[8])
				if fields[3] == "120000" || fields[4] == "120000" || fields[5] == "120000" {
					_, err := e.failAttempt(taskID, attemptID, "reason=scope_violation class=symlink effects=none", false, 0, nil, principal.CodePolicyDenied, errors.New("symlink detected"))
					return "", nil, err
				}
				if fields[3] == "160000" || fields[4] == "160000" || fields[5] == "160000" || !strings.HasPrefix(fields[2], "N") {
					_, err := e.failAttempt(taskID, attemptID, "reason=scope_violation class=submodule effects=none", false, 0, nil, principal.CodePolicyDenied, errors.New("submodule detected"))
					return "", nil, err
				}
			}
		} else if strings.HasPrefix(s, "2 ") {
			fields := strings.SplitN(s, " ", 10)
			if len(fields) >= 10 {
				paths = append(paths, fields[9])
			}
			i++ // skip origPath token
		} else if strings.HasPrefix(s, "u ") {
			fields := strings.SplitN(s, " ", 11)
			if len(fields) >= 11 {
				paths = append(paths, fields[10])
			}
		} else if strings.HasPrefix(s, "? ") {
			paths = append(paths, strings.TrimPrefix(s, "? "))
		}
	}

	if len(paths) == 0 {
		_, err := e.failAttempt(taskID, attemptID, "reason=no_change effects=none", false, 0, nil, principal.CodePolicyDenied, errors.New("no files changed"))
		return "", nil, err
	}

	if len(paths) > 64 {
		_, err := e.failAttempt(taskID, attemptID, "reason=candidate_too_large class=count effects=none", false, 0, nil, principal.CodePolicyDenied, errors.New("too many files changed"))
		return "", nil, err
	}

	// Verify path boundaries and scope
	for _, p := range paths {
		clean := filepath.Clean(p)
		if clean == ".git" || strings.HasPrefix(clean, ".git/") || strings.Contains(clean, "/.git/") || strings.HasSuffix(clean, "/.git") {
			_, err := e.failAttempt(taskID, attemptID, "reason=scope_violation class=git_dir effects=none", false, 0, nil, principal.CodePolicyDenied, errors.New("git directory modification"))
			return "", nil, err
		}

		fullPath := filepath.Join(worktreePath, clean)
		if fi, err := os.Lstat(fullPath); err == nil && (fi.Mode()&os.ModeSymlink != 0) {
			_, err := e.failAttempt(taskID, attemptID, "reason=scope_violation class=symlink effects=none", false, 0, nil, principal.CodePolicyDenied, errors.New("symlink file detected"))
			return "", nil, err
		}

		if !compiler.IsPathAuthorized(clean, writeScope) {
			_, err := e.failAttempt(taskID, attemptID, "reason=scope_violation class=scope effects=none", false, 0, nil, principal.CodePolicyDenied, fmt.Errorf("path %q outside write scope", clean))
			return "", nil, err
		}
	}

	// Diff size check (<= 2 MiB)
	diffRes, err := e.runGit(ctx, worktreePath, "diff", baseCommit)
	if err != nil {
		_, err := e.failAttempt(taskID, attemptID, "reason=driver_error effects=uncertain", false, 0, nil, principal.CodeModelUnavailable, err)
		return "", nil, err
	}
	if len(diffRes.Stdout) > 2*1024*1024 {
		_, err := e.failAttempt(taskID, attemptID, "reason=candidate_too_large class=size effects=none", false, 0, nil, principal.CodePolicyDenied, errors.New("patch size exceeds 2 MiB"))
		return "", nil, err
	}

	// Add files
	addArgs := append([]string{"add", "--"}, paths...)
	if _, err := e.runGit(ctx, worktreePath, addArgs...); err != nil {
		_, err := e.failAttempt(taskID, attemptID, "reason=driver_error effects=uncertain", false, 0, nil, principal.CodeModelUnavailable, err)
		return "", nil, err
	}

	// Commit
	commitMsg := fmt.Sprintf("devcadence attempt %s", attemptID)
	if _, err := e.runGit(ctx, worktreePath, "commit", "-m", commitMsg); err != nil {
		_, err := e.failAttempt(taskID, attemptID, "reason=driver_error effects=uncertain", false, 0, nil, principal.CodeModelUnavailable, err)
		return "", nil, err
	}

	// Head commit hash
	revRes, err := e.runGit(ctx, worktreePath, "rev-parse", "HEAD")
	if err != nil {
		_, err := e.failAttempt(taskID, attemptID, "reason=driver_error effects=uncertain", false, 0, nil, principal.CodeModelUnavailable, err)
		return "", nil, err
	}
	headCommit = strings.TrimSpace(string(revRes.Stdout))

	// Verify sole parent matches base commit
	parentsRes, err := e.runGit(ctx, worktreePath, "rev-list", "--parents", "-n", "1", "HEAD")
	if err != nil {
		_, err := e.failAttempt(taskID, attemptID, "reason=driver_error effects=uncertain", false, 0, nil, principal.CodeModelUnavailable, err)
		return "", nil, err
	}
	parentTokens := strings.Fields(string(parentsRes.Stdout))
	if len(parentTokens) != 2 || parentTokens[1] != baseCommit {
		_, err := e.failAttempt(taskID, attemptID, "reason=scope_violation class=scope effects=none", false, 0, nil, principal.CodePolicyDenied, fmt.Errorf("commit sole parent mismatch: expected %q, got %v", baseCommit, parentTokens))
		return "", nil, err
	}

	// Pin the candidate commit durably in the primary repository (shared ref
	// namespace; moves no branch and not HEAD). Fail closed: a candidate that
	// cannot be reached from the primary repository is not handed off.
	if err := e.pinCandidateRef(ctx, worktreePath, taskID, attemptID, headCommit); err != nil {
		_, ferr := e.failAttempt(taskID, attemptID, "reason=candidate_ref_unavailable effects=none", false, 0, nil, principal.CodeInternal, err)
		return "", nil, ferr
	}

	// Final scope check on diff --name-status base..HEAD
	nameStatusRes, err := e.runGit(ctx, worktreePath, "diff", "--name-status", baseCommit+"..HEAD")
	if err != nil {
		_, err := e.failAttempt(taskID, attemptID, "reason=driver_error effects=uncertain", false, 0, nil, principal.CodeModelUnavailable, err)
		return "", nil, err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(nameStatusRes.Stdout)), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			p := fields[len(fields)-1]
			if !compiler.IsPathAuthorized(p, writeScope) {
				_, err := e.failAttempt(taskID, attemptID, "reason=scope_violation class=scope effects=none", false, 0, nil, principal.CodePolicyDenied, fmt.Errorf("diff path %q outside write scope", p))
				return "", nil, err
			}
		}
	}

	return headCommit, paths, nil
}

func (e *Executor) runGit(ctx context.Context, dir string, args ...string) (process.Result, error) {
	env := process.MergeEnv(process.BaseEnv(), map[string]string{
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_SYSTEM":   os.DevNull,
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME":     "DevCadence executor",
		"GIT_AUTHOR_EMAIL":    "executor@devcadence.invalid",
		"GIT_COMMITTER_NAME":  "DevCadence executor",
		"GIT_COMMITTER_EMAIL": "executor@devcadence.invalid",
	})
	fullArgs := append([]string{"-c", "core.hooksPath=", "-c", "commit.gpgsign=false"}, args...)
	spec := process.Spec{
		Executable: "git",
		Args:       fullArgs,
		Dir:        dir,
		Env:        env,
		Timeout:    30 * time.Second,
	}
	res, err := e.opts.Runner.Run(ctx, spec)
	if err != nil {
		return res, err
	}
	if res.ExitCode != 0 {
		return res, fmt.Errorf("git %v failed with exit code %d: %s", args, res.ExitCode, string(res.Stderr))
	}
	return res, nil
}

func (e *Executor) failAttempt(
	taskID, attemptID string,
	summary string,
	cancelled bool,
	repairIterations int,
	artifacts []protocol.ArtifactRef,
	code string,
	cause error,
) (string, error) {
	freshCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attach the command trace (content-addressed, so idempotent) on every
	// failure path when commands were attempted. A store failure here is
	// logged: the attempt is already failing.
	if v, ok := e.audits.Load(attemptID); ok {
		hasTrace := false
		for _, a := range artifacts {
			hasTrace = hasTrace || a.Kind == "command-trace"
		}
		if !hasTrace {
			if refs, terr := e.commandTraceRefs(freshCtx, v.(*commandAudit)); terr != nil {
				e.opts.Logger.Error("command trace not persisted on failed attempt", slog.String("attempt_id", attemptID), slog.String("error", terr.Error()))
			} else {
				artifacts = append(append([]protocol.ArtifactRef(nil), artifacts...), refs...)
			}
		}
	}

	// Attach the validation report when any post-check round ran.
	if refs, rerr := e.postCheckRefs(freshCtx, attemptID); rerr != nil {
		e.opts.Logger.Error("validation report not persisted on failed attempt", slog.String("attempt_id", attemptID), slog.String("error", rerr.Error()))
	} else if len(refs) > 0 {
		artifacts = append(append([]protocol.ArtifactRef(nil), artifacts...), refs...)
	}

	psFresh, err := e.opts.ControlPlane.ProjectState(freshCtx, e.opts.ProjectID)
	if err != nil {
		return "", principal.NewCodedError(principal.CodeInternal, true, []string{"attempt-outcome-unrecorded"}, fmt.Sprintf("failed to read project state: %v", err))
	}

	batchCmd := controlplane.BatchCommand{
		ProjectID:             e.opts.ProjectID,
		Actor:                 protocol.Actor{Kind: protocol.ActorControlPlane, ID: "taskexec"},
		ExpectedStateRevision: psFresh.StateRevision,
		Commands: []controlplane.Command{
			{
				ProjectID: e.opts.ProjectID,
				Actor:     protocol.Actor{Kind: protocol.ActorControlPlane, ID: "taskexec"},
				Payload: &events.AttemptFailed{
					TaskID:           taskID,
					AttemptID:        attemptID,
					Summary:          summary,
					Cancelled:        cancelled,
					RepairIterations: repairIterations,
					Artifacts:        artifacts,
				},
			},
		},
	}

	if _, err := e.opts.ControlPlane.ApplyBatch(freshCtx, batchCmd); err != nil {
		return "", principal.NewCodedError(principal.CodeInternal, true, []string{"attempt-outcome-unrecorded"}, fmt.Sprintf("failed to record attempt failure: %v", err))
	}

	if cancelled {
		return "", cause
	}
	return "", principal.NewCodedError(code, false, []string{"attempt-failed"}, fmt.Sprintf("attempt failed: %s (%v)", summary, cause))
}

func (e *Executor) handleCancel(taskID, attemptID string, session drivers.Session, ctxErr error) (string, error) {
	if session != nil {
		closeCtx, cancelClose := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelClose()
		_ = session.Close(closeCtx)
	}
	return e.failAttempt(taskID, attemptID, "reason=cancelled effects=uncertain", true, 0, nil, principal.CodeCancelled, ctxErr)
}

type attemptStillRunningGuard struct {
	taskAlias string
}

func (g attemptStillRunningGuard) Check(ctx context.Context, view controlplane.BatchReadView) error {
	ps := view.ProjectState()
	if ps == nil {
		return errs.New(errs.CategoryInternal, "nil project state in guard")
	}
	for _, alias := range ps.Tasks.Running {
		if alias == g.taskAlias {
			return nil
		}
	}
	return errs.New(errs.CategoryConflict, "task %s is no longer running", g.taskAlias)
}

func mapBindError(err error) error {
	msg := err.Error()
	ref := "endpoint-unavailable"
	if strings.Contains(msg, "model-revision-unknown") {
		ref = "model-revision-unknown"
	} else if strings.Contains(msg, "runtime-version-unknown") {
		ref = "runtime-version-unknown"
	} else if strings.Contains(msg, "driver-id-unknown") {
		ref = "driver-id-unknown"
	}
	return principal.NewCodedError(principal.CodeModelUnavailable, false, []string{ref}, msg)
}
