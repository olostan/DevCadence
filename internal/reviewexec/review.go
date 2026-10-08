package reviewexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/actors"
	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/execpolicy"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/repository"
	"github.com/olostan/DevCadence/internal/storage"
	"github.com/olostan/DevCadence/internal/tasks"
	"github.com/olostan/DevCadence/internal/tools"
	"github.com/olostan/DevCadence/internal/worktrees"
)

// Review coordinates synchronous pre-checks, early fast-fails, and asynchronous
// independent review execution across requested dimensions.
func (e *Executor) Review(
	ctx context.Context,
	caller principal.CallerContext,
	meta principal.CallMeta,
	candidate principal.CandidateRef,
	dimensions []string,
) (principal.OperationRef, error) {
	// 1. Synchronous checks
	if caller.ProjectID != e.opts.ProjectID || (meta.ProjectID != "" && meta.ProjectID != e.opts.ProjectID) {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodePolicyDenied,
			false,
			[]string{"project-mismatch"},
			fmt.Sprintf("project mismatch: executor owns %q, caller=%q meta=%q", e.opts.ProjectID, caller.ProjectID, meta.ProjectID),
		)
	}

	if strings.TrimSpace(candidate.Commit) == "" ||
		strings.TrimSpace(candidate.TaskID) == "" ||
		strings.TrimSpace(candidate.AttemptID) == "" {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodeInvalidArgument,
			false,
			[]string{"missing-fields"},
			"candidate commit, task ID, and attempt ID are required",
		)
	}

	if len(dimensions) == 0 {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodeInvalidArgument,
			false,
			[]string{"review-dimensions-required"},
			"at least one review dimension is required",
		)
	}

	seenDims := make(map[protocol.ReviewDimension]bool, len(dimensions))
	sortedDims := make([]string, len(dimensions))
	copy(sortedDims, dimensions)
	sort.Strings(sortedDims)

	for _, d := range sortedDims {
		dim := protocol.ReviewDimension(d)
		if !dim.Valid() {
			return principal.OperationRef{}, principal.NewCodedError(
				principal.CodeInvalidArgument,
				false,
				[]string{"invalid-dimension"},
				fmt.Sprintf("unknown review dimension %q", d),
			)
		}
		if seenDims[dim] {
			return principal.OperationRef{}, principal.NewCodedError(
				principal.CodeInvalidArgument,
				false,
				[]string{"duplicate-dimension"},
				fmt.Sprintf("duplicate review dimension %q", d),
			)
		}
		seenDims[dim] = true
	}

	// State check: Task must exist
	taskList, err := e.opts.ControlPlane.Tasks(ctx, storage.TaskFilter{ProjectID: e.opts.ProjectID})
	if err != nil {
		return principal.OperationRef{}, err
	}
	var foundTask *tasks.Task
	for _, t := range taskList {
		if t.ID == candidate.TaskID || t.Alias == candidate.TaskID {
			foundTask = t
			break
		}
	}
	if foundTask == nil {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodeNotFound,
			false,
			[]string{"task-not-found"},
			fmt.Sprintf("task %q not found", candidate.TaskID),
		)
	}

	// Attempt must exist with status candidate_produced (or validating / reviewing)
	detail, err := e.opts.ControlPlane.TaskDetail(ctx, e.opts.ProjectID, foundTask.Alias)
	if err != nil {
		return principal.OperationRef{}, err
	}
	var foundAttempt *tasks.Attempt
	for _, a := range detail.Attempts {
		if a.ID == candidate.AttemptID {
			foundAttempt = a
			break
		}
	}
	if foundAttempt == nil {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodeNotFound,
			false,
			[]string{"attempt-not-found"},
			fmt.Sprintf("attempt %q not found", candidate.AttemptID),
		)
	}

	if foundAttempt.Status != tasks.AttemptCandidateProduced &&
		string(foundAttempt.Status) != "validating" &&
		string(foundAttempt.Status) != "reviewing" {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodePolicyDenied,
			false,
			[]string{"invalid-attempt-state"},
			fmt.Sprintf("attempt %s status is %s, want candidate_produced/validating/reviewing", candidate.AttemptID, foundAttempt.Status),
		)
	}

	if foundAttempt.CandidateCommit != candidate.Commit {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodePolicyDenied,
			false,
			[]string{"candidate-commit-mismatch"},
			fmt.Sprintf("candidate commit mismatch: attempt has %q, request has %q", foundAttempt.CandidateCommit, candidate.Commit),
		)
	}

	// Fast-fail check: check if intent already exists for any requested dimension
	for _, d := range sortedDims {
		intentID := "intent:" + candidate.AttemptID + ":" + d
		stored, err := e.opts.ControlPlane.Record(ctx, e.opts.ProjectID, "ReviewInvocationIntent", intentID, 1)
		if err == nil && stored.Document != "" {
			return principal.OperationRef{}, principal.NewCodedError(
				principal.CodePolicyDenied,
				false,
				[]string{"review-already-recorded"},
				fmt.Sprintf("review intent %s already recorded", intentID),
			)
		}
		if err != nil && errs.CategoryOf(err) != errs.CategoryNotFound {
			return principal.OperationRef{}, err
		}
	}

	// 2. Read worker provenance record <attempt_id>:implementer
	workerKey := candidate.AttemptID + ":implementer"
	storedWorker, err := e.opts.ControlPlane.Record(ctx, e.opts.ProjectID, "InvocationProvenance", workerKey, 1)
	if err != nil && errs.CategoryOf(err) == errs.CategoryNotFound {
		storedWorker, err = e.opts.ControlPlane.Record(ctx, e.opts.ProjectID, "InvocationProvenance", "prov_"+candidate.AttemptID, 1)
	}
	if err != nil {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodeModelUnavailable,
			false,
			[]string{"worker-basis-unknown"},
			fmt.Sprintf("worker provenance record not found: %v", err),
		)
	}

	var workerProv protocol.InvocationProvenance
	if err := protocol.Unmarshal([]byte(storedWorker.Document), &workerProv); err != nil {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodeModelUnavailable,
			false,
			[]string{"worker-basis-unknown"},
			fmt.Sprintf("failed to decode worker provenance: %v", err),
		)
	}

	if _, err := actors.DeriveActorID(e.opts.IndependenceBasis, workerProv.Basis); err != nil {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodeModelUnavailable,
			false,
			[]string{"worker-basis-unknown"},
			fmt.Sprintf("failed to derive worker actor ID under basis %q: %v", e.opts.IndependenceBasis, err),
		)
	}

	// 3. Execution Policy
	policy, policyDigest, err := e.opts.Policy.Current(ctx)
	if err != nil {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodeModelUnavailable,
			false,
			[]string{"execution-policy-unavailable"},
			fmt.Sprintf("execution policy unavailable: %v", err),
		)
	}

	// 4. Schedule asynchronous run
	timeout := 15*time.Minute*time.Duration(len(sortedDims)) + 60*time.Second
	opRef, err := e.opts.Registry.Start(e.opts.ProjectID, "review", timeout, func(opCtx context.Context) (string, error) {
		return e.runReview(opCtx, foundTask, candidate, sortedDims, workerProv, policy, policyDigest)
	})
	if err != nil {
		return principal.OperationRef{}, err
	}
	return opRef, nil
}

func (e *Executor) runReview(
	ctx context.Context,
	task *tasks.Task,
	candidate principal.CandidateRef,
	dimensions []string,
	workerProv protocol.InvocationProvenance,
	policy execpolicy.ExecutionPolicy,
	policyDigest string,
) (string, error) {
	repo, err := e.opts.Repositories.Repository(ctx, e.opts.ProjectID)
	if err != nil {
		return "", principal.NewCodedError(
			principal.CodeModelUnavailable,
			false,
			[]string{"repo-unavailable", "review-partial", "completed:0", fmt.Sprintf("requested:%d", len(dimensions))},
			fmt.Sprintf("failed to get repository: %v", err),
		)
	}

	nCompleted := 0
	for _, d := range dimensions {
		dim := protocol.ReviewDimension(d)
		err := e.reviewDimension(ctx, repo, task, candidate, dim, workerProv, policy, policyDigest)
		if err != nil {
			var coded *principal.CodedError
			code := principal.CodeModelUnavailable
			var existingRefs []string
			if errors.As(err, &coded) {
				code = coded.Code()
				existingRefs = coded.EvidenceRefs()
			}
			refs := []string{"review-partial", fmt.Sprintf("completed:%d", nCompleted), fmt.Sprintf("requested:%d", len(dimensions))}
			for _, r := range existingRefs {
				if r != "review-partial" && !strings.HasPrefix(r, "completed:") && !strings.HasPrefix(r, "requested:") {
					refs = append(refs, r)
				}
			}
			return "", principal.NewCodedError(code, false, refs, err.Error())
		}
		nCompleted++
	}

	return fmt.Sprintf("reviewed:%d", nCompleted), nil
}

func (e *Executor) reviewDimension(
	ctx context.Context,
	repo *repository.Repository,
	task *tasks.Task,
	candidate principal.CandidateRef,
	dim protocol.ReviewDimension,
	workerProv protocol.InvocationProvenance,
	policy execpolicy.ExecutionPolicy,
	policyDigest string,
) error {
	intentID := "intent:" + candidate.AttemptID + ":" + string(dim)

	// 1. Resolve reviewer endpoint
	req := execpolicy.EndpointRequest{
		ProjectID:         e.opts.ProjectID,
		TaskID:            task.ID,
		Role:              "reviewer",
		ContextNeed:       protocol.ExposureToolMediatedWorktree,
		IndependenceBasis: e.opts.IndependenceBasis,
		ExcludeBases:      []protocol.ActorBasis{workerProv.Basis},
	}
	ep, err := e.opts.Resolver.Resolve(ctx, req, policy, policyDigest)
	if err != nil {
		return principal.NewCodedError(
			principal.CodeModelUnavailable,
			false,
			[]string{"no-independent-reviewer"},
			fmt.Sprintf("no independent reviewer available for %s: %v", dim, err),
		)
	}

	// 2. Probe driver
	opened, err := e.opts.Drivers.Open(ctx, ep)
	if err != nil {
		return principal.NewCodedError(
			principal.CodeModelUnavailable,
			false,
			[]string{"endpoint-unavailable"},
			fmt.Sprintf("driver open failed for %s: %v", dim, err),
		)
	}
	if opened.Driver == nil {
		return principal.NewCodedError(
			principal.CodeModelUnavailable,
			false,
			[]string{"endpoint-unavailable"},
			"opened driver is nil",
		)
	}

	// 3. Assert driver ID
	if err := execpolicy.AssertDriverID(opened.Driver.ID(), opened.Observed); err != nil {
		return principal.NewCodedError(
			principal.CodeModelUnavailable,
			false,
			[]string{"driver-id-mismatch"},
			fmt.Sprintf("driver ID assertion failed: %v", err),
		)
	}

	// 4. Bind endpoint
	ep, err = execpolicy.Bind(ep, opened.Observed)
	if err != nil {
		return principal.NewCodedError(
			principal.CodeModelUnavailable,
			false,
			[]string{"endpoint-unavailable"},
			fmt.Sprintf("bind endpoint failed: %v", err),
		)
	}

	// 5. Derive reviewer & worker actor IDs
	reviewerActorID, err := actors.DeriveActorID(e.opts.IndependenceBasis, ep.ActorBasis())
	if err != nil {
		return principal.NewCodedError(
			principal.CodeModelUnavailable,
			false,
			[]string{"reviewer-basis-unknown"},
			fmt.Sprintf("failed to derive reviewer actor ID: %v", err),
		)
	}
	workerActorID, err := actors.DeriveActorID(e.opts.IndependenceBasis, workerProv.Basis)
	if err != nil {
		return principal.NewCodedError(
			principal.CodeModelUnavailable,
			false,
			[]string{"worker-basis-unknown"},
			fmt.Sprintf("failed to derive worker actor ID: %v", err),
		)
	}
	if reviewerActorID == workerActorID {
		return principal.NewCodedError(
			principal.CodePolicyDenied,
			false,
			[]string{"independent-actor-collision"},
			fmt.Sprintf("reviewer actor %q collides with worker actor %q", reviewerActorID, workerActorID),
		)
	}

	// 6. Generate IDs
	reviewID := e.opts.IDs.New("rev")
	invocationID := e.opts.IDs.New("inv")

	// 7. Compile invocation (clean context pack)
	contractText := ""
	wpDigest := ""
	workPackageID := task.WorkPackageID
	if workPackageID == "" {
		workPackageID = candidate.WorkPackage.ID
	}
	workPackageVersion := task.WorkPackageVersion
	if workPackageVersion == 0 {
		workPackageVersion = candidate.WorkPackage.Version
	}
	storedWP, err := e.opts.ControlPlane.Record(ctx, e.opts.ProjectID, "EngineeringWorkPackage", workPackageID, workPackageVersion)
	if err != nil {
		return principal.NewCodedError(
			principal.CodeInternal,
			false,
			nil,
			fmt.Sprintf("failed to get engineering work package %s v%d: %v", workPackageID, workPackageVersion, err),
		)
	}
	wpDigest = storedWP.Digest
	var wp protocol.EngineeringWorkPackage
	if protocol.Unmarshal([]byte(storedWP.Document), &wp) == nil {
		contractText = wp.Objective
	}
	if contractText == "" {
		contractText = storedWP.Document
	}

	ps, err := e.opts.ControlPlane.ProjectState(ctx, e.opts.ProjectID)
	if err != nil {
		return principal.NewCodedError(
			principal.CodeInternal,
			false,
			nil,
			fmt.Sprintf("failed to get project state: %v", err),
		)
	}

	lensPrompt := loadReviewerPromptTemplate(e.opts.StateDir)
	compileReq := compiler.CompileRequest{
		TaskID:              task.ID,
		WorkPackageID:       workPackageID,
		WorkPackageRevision: workPackageVersion,
		WorkPackageDigest:   wpDigest,
		Role:                "reviewer",
		RoleCore:            lensPrompt,
		BaseCommit:          candidate.Commit,
		SourceRevision:      candidate.Commit,
		CandidateCommit:     &candidate.Commit,
		WriteScope:          nil,
		ReadEnvelope:        []string{"**"},
		ExecutionContract:   contractText,
		ContextProfile:      &ep.ContextProfile,
		AccessChannel:       &ep.Channel,
		Tools:               []string{"read_file", "grep", "symbols"},
		DeclaredTools: []compiler.ToolCapabilityInfo{
			{Name: "read_file", ReadOnly: true},
			{Name: "grep", ReadOnly: true},
			{Name: "symbols", ReadOnly: true},
		},
		BudgetPoolID:         "pool-default",
		ProjectStateRevision: ps.StateRevision,
		Action:               fmt.Sprintf("Review dimension %s for candidate %s", dim, candidate.Commit),
	}

	compiled, err := e.opts.Compiler.CompileInvocation(ctx, compileReq)
	if err != nil {
		return principal.NewCodedError(
			principal.CodeContextUnfit,
			false,
			[]string{"compile-failed"},
			fmt.Sprintf("failed to compile invocation: %v", err),
		)
	}

	promptDigest := compiled.InvocationDigest
	if promptDigest == "" && compiled.Pack != nil {
		promptDigest = compiled.Pack.PackDigest
	}
	if promptDigest == "" {
		promptDigest = protocol.DigestBytes([]byte("prompt:" + invocationID))
	}
	var manifestDigest string
	if compiled.Manifest != nil {
		mb, _ := protocol.CanonicalJSON(compiled.Manifest)
		manifestDigest = protocol.DigestBytes(mb)
	} else {
		manifestDigest = protocol.DigestBytes([]byte("manifest:" + invocationID))
	}

	// 8. Create review worktree rv-<invocationID>
	wt, err := e.opts.Worktrees.Create(ctx, repo, worktrees.Spec{
		ProjectID:  e.opts.ProjectID,
		TaskID:     task.ID,
		AttemptID:  "rv-" + invocationID,
		BaseCommit: candidate.Commit,
	})
	if err != nil {
		return principal.NewCodedError(
			principal.CodeModelUnavailable,
			false,
			[]string{"worktree-unavailable"},
			fmt.Sprintf("failed to create review worktree: %v", err),
		)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = e.opts.Worktrees.Cleanup(cleanupCtx, repo, e.opts.ProjectID, wt.ID, worktrees.CleanupOptions{Force: true})
	}()

	// 9. PRE-MODEL INTENT COMMIT
	startedAt := e.opts.Clock.Now().UTC()
	intent := &protocol.ReviewInvocationIntent{
		SchemaVersion:         protocol.SchemaVersion1,
		ReviewID:              reviewID,
		ProjectID:             e.opts.ProjectID,
		TaskID:                task.ID,
		AttemptID:             candidate.AttemptID,
		WorkPackageID:         workPackageID,
		Dimension:             dim,
		InvocationID:          invocationID,
		CandidateCommit:       candidate.Commit,
		InvocationNumber:      1,
		ReviewerBasis:         ep.ActorBasis(),
		IndependenceBasis:     e.opts.IndependenceBasis,
		EndpointBindingDigest: ep.BindingDigest,
		StartedAt:             startedAt,
	}
	if err := intent.Validate(); err != nil {
		return err
	}
	intentBytes, err := protocol.CanonicalJSON(intent)
	if err != nil {
		return err
	}
	intentDigest := protocol.DigestBytes(intentBytes)

	startedEvent := &events.ReviewInvocationStarted{
		TaskID:          task.ID,
		AttemptID:       candidate.AttemptID,
		WorkPackageID:   workPackageID,
		ReviewID:        reviewID,
		InvocationID:    invocationID,
		Dimension:       dim,
		CandidateCommit: candidate.Commit,
		RecordDigest:    intentDigest,
	}
	if err := startedEvent.Validate(); err != nil {
		return err
	}

	ps, err = e.opts.ControlPlane.ProjectState(ctx, e.opts.ProjectID)
	if err != nil {
		return err
	}

	preBatch := controlplane.BatchCommand{
		ProjectID:             e.opts.ProjectID,
		Actor:                 protocol.Actor{Kind: protocol.ActorControlPlane, ID: "reviewexec"},
		ExpectedStateRevision: ps.StateRevision,
		Preconditions: []controlplane.BatchGuard{
			intentAbsentGuard{
				taskID:          task.ID,
				attemptID:       candidate.AttemptID,
				candidateCommit: candidate.Commit,
				intentID:        intentID,
			},
		},
		Commands: []controlplane.Command{
			{
				ProjectID: e.opts.ProjectID,
				Actor:     protocol.Actor{Kind: protocol.ActorControlPlane, ID: "reviewexec"},
				Payload:   startedEvent,
				Records: []controlplane.RecordToStore{
					{Version: 1, Record: intent},
				},
			},
		},
	}

	if _, err := e.opts.ControlPlane.ApplyBatch(ctx, preBatch); err != nil {
		return principal.NewCodedError(
			principal.CodePolicyDenied,
			false,
			[]string{"review-intent-unrecorded"},
			fmt.Sprintf("failed committing review intent batch: %v", err),
		)
	}

	// 10. Run review session
	outcome, reviewResult := e.runReviewSession(ctx, task, candidate, dim, reviewID, invocationID, ep, opened, compiled, wt)

	// 11. TERMINAL PERSISTENCE
	endedAt := e.opts.Clock.Now().UTC()
	resBytes, err := protocol.CanonicalJSON(reviewResult)
	if err != nil {
		return err
	}
	reviewResultDigest := protocol.DigestBytes(resBytes)

	modelRef := fmt.Sprintf("%s/%s@%s", ep.EndpointID, ep.ModelID, ep.ModelRevision)
	provRecord := &protocol.InvocationProvenance{
		SchemaVersion: protocol.SchemaVersion1,
		ProvenanceID:  reviewID,
		ProjectID:     e.opts.ProjectID,
		TaskID:        task.ID,
		AttemptID:     candidate.AttemptID,
		WorkPackageID: workPackageID,
		Role:          protocol.ProvenanceRoleReviewer,
		Actor: protocol.ActorProvenance{
			ActorID:      reviewerActorID,
			InvocationID: invocationID,
			Role:         protocol.ProvenanceRoleReviewer,
			EndpointRef:  &ep.EndpointID,
			ModelRef:     &modelRef,
		},
		Basis:                 ep.ActorBasis(),
		IndependenceBasis:     e.opts.IndependenceBasis,
		EndpointBindingDigest: ep.BindingDigest,
		ContextManifestDigest: manifestDigest,
		PromptDigest:          promptDigest,
		CandidateCommit:       candidate.Commit,
		Dimension:             dim,
		StartedAt:             startedAt,
	}
	if err := provRecord.Validate(); err != nil {
		return err
	}

	invRecord := &protocol.ReviewInvocation{
		SchemaVersion:    protocol.SchemaVersion1,
		ReviewID:         reviewID,
		ProjectID:        e.opts.ProjectID,
		TaskID:           task.ID,
		AttemptID:        candidate.AttemptID,
		WorkPackageID:    workPackageID,
		Dimension:        dim,
		InvocationID:     invocationID,
		ProvenanceID:     reviewID,
		CandidateCommit:  candidate.Commit,
		InvocationNumber: 1,
		Outcome:          outcome,
		StartedAt:        startedAt,
		EndedAt:          endedAt,
	}
	if err := invRecord.Validate(); err != nil {
		return err
	}

	completedEvent := &events.ReviewCompleted{
		TaskID:                         task.ID,
		AttemptID:                      candidate.AttemptID,
		ReviewID:                       reviewID,
		WorkPackageID:                  workPackageID,
		Dimension:                      dim,
		Verdict:                        reviewResult.Verdict,
		RecordDigest:                   reviewResultDigest,
		PrincipalEscalationRecommended: reviewResult.PrincipalEscalationRecommended,
	}
	if err := completedEvent.Validate(); err != nil {
		return err
	}

	termCtx := ctx
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		termCtx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
	}

	psFresh, err := e.opts.ControlPlane.ProjectState(termCtx, e.opts.ProjectID)
	if err != nil {
		return principal.NewCodedError(
			principal.CodePolicyDenied,
			false,
			[]string{"review-outcome-unrecorded"},
			fmt.Sprintf("failed retrieving fresh state: %v", err),
		)
	}

	termBatch := controlplane.BatchCommand{
		ProjectID:             e.opts.ProjectID,
		Actor:                 protocol.Actor{Kind: protocol.ActorControlPlane, ID: "reviewexec"},
		ExpectedStateRevision: psFresh.StateRevision,
		Preconditions: []controlplane.BatchGuard{
			terminalPersistenceGuard{
				intentID:       intentID,
				expectedIntent: *intent,
				intentDigest:   intentDigest,
				reviewID:       reviewID,
			},
		},
		Commands: []controlplane.Command{
			{
				ProjectID: e.opts.ProjectID,
				Actor:     protocol.Actor{Kind: protocol.ActorControlPlane, ID: "reviewexec"},
				Payload:   completedEvent,
				Records: []controlplane.RecordToStore{
					{Version: 1, Record: reviewResult},
					{Version: 1, Record: provRecord},
					{Version: 1, Record: invRecord},
				},
			},
		},
	}

	if _, err := e.opts.ControlPlane.ApplyBatch(termCtx, termBatch); err != nil {
		return principal.NewCodedError(
			principal.CodePolicyDenied,
			false,
			[]string{"review-outcome-unrecorded"},
			fmt.Sprintf("failed persisting terminal review batch: %v", err),
		)
	}

	if outcome != protocol.OutcomeCompleted {
		var code string
		switch outcome {
		case protocol.OutcomeOutputInvalid:
			code = principal.CodeInternal
		case protocol.OutcomeCancelled:
			code = principal.CodeCancelled
		default:
			code = principal.CodeModelUnavailable
		}
		return principal.NewCodedError(
			code,
			false,
			nil,
			fmt.Sprintf("review for dimension %s ended with non-completed outcome: %s", dim, outcome),
		)
	}

	return nil
}

func (e *Executor) runReviewSession(
	ctx context.Context,
	task *tasks.Task,
	candidate principal.CandidateRef,
	dim protocol.ReviewDimension,
	reviewID, invocationID string,
	ep execpolicy.ResolvedEndpoint,
	opened execpolicy.OpenedEndpoint,
	compiled *compiler.CompiledInvocation,
	wt *worktrees.Worktree,
) (protocol.ReviewInvocationOutcome, *protocol.ReviewResult) {
	workPackageID := task.WorkPackageID
	if workPackageID == "" {
		workPackageID = candidate.WorkPackage.ID
	}

	if ep.Limits.MaxTurns <= 0 ||
		ep.Limits.MaxToolCalls <= 0 ||
		ep.Limits.MaxTotalTokens <= 0 ||
		ep.Limits.MaxDurationSeconds <= 0 ||
		ep.Limits.MaxOutputTokensPerCall <= 0 ||
		ep.Limits.MaxRequestBytes <= 0 {
		return protocol.OutcomeDriverError, authorUnableToVerifyResult(e.opts.ProjectID, candidate.AttemptID, workPackageID, dim, reviewID, ep)
	}

	maxDurationSec := ep.Limits.MaxDurationSeconds
	meterLimits := drivers.MeterLimits{
		MaxCumulativeTotalTokens: ep.Limits.MaxTotalTokens,
		MaxCumulativeToolCalls:   ep.Limits.MaxToolCalls,
		MaxCumulativeDuration:    time.Duration(maxDurationSec) * time.Second,
		AllowUnknownUsage:        ep.Limits.AllowUnknownUsage,
		MaxDurationPerOp:         time.Duration(maxDurationSec) * time.Second,
	}

	md := drivers.NewMeteredDriver(opened.Driver, meterLimits)
	scope := &tools.Scope{
		ProjectID:    e.opts.ProjectID,
		WorktreeID:   wt.ID,
		WorktreePath: wt.Path,
	}
	mediator := drivers.NewScopedToolMediator(scope)
	toolDefs := setupReviewerTools(mediator, scope, e.opts.Runner)

	sessionConfig := drivers.SessionConfig{
		SessionID:              invocationID + "-s1",
		ModelID:                ep.ModelID,
		SystemPrompt:           compiled.Projection.SystemPrompt,
		Tools:                  toolDefs,
		WorktreeScope:          scope,
		Mediator:               mediator,
		MaxOutputTokensPerCall: ep.Limits.MaxOutputTokensPerCall,
	}

	session, err := md.StartSession(ctx, sessionConfig)
	if err != nil {
		return protocol.OutcomeDriverError, authorUnableToVerifyResult(e.opts.ProjectID, candidate.AttemptID, workPackageID, dim, reviewID, ep)
	}
	defer func() {
		_ = session.Close(context.Background())
	}()

	maxTurns := ep.Limits.MaxTurns
	userPrompt := compiled.Projection.UserPrompt
	if userPrompt == "" {
		userPrompt = fmt.Sprintf("Please review candidate commit %s for dimension %s.", candidate.Commit, dim)
	}
	turnInput := drivers.TurnInput{
		TurnID: "turn-1",
		Prompt: userPrompt,
	}

	var finalContent string
	stoppedNormally := false

	for turn := 1; turn <= maxTurns; turn++ {
		if ctx.Err() != nil {
			return protocol.OutcomeCancelled, authorUnableToVerifyResult(e.opts.ProjectID, candidate.AttemptID, workPackageID, dim, reviewID, ep)
		}

		inputBytes := len(turnInput.Prompt)
		for _, tr := range turnInput.ToolResults {
			inputBytes += len(tr.Content)
		}
		if inputBytes > ep.Limits.MaxRequestBytes {
			return protocol.OutcomeLimitReached, authorUnableToVerifyResult(e.opts.ProjectID, candidate.AttemptID, workPackageID, dim, reviewID, ep)
		}

		turnRes, err := session.ExecuteTurn(ctx, turnInput)
		if err != nil {
			if ctx.Err() != nil {
				return protocol.OutcomeCancelled, authorUnableToVerifyResult(e.opts.ProjectID, candidate.AttemptID, workPackageID, dim, reviewID, ep)
			}
			if errors.Is(err, drivers.ErrBudgetExceeded) {
				return protocol.OutcomeLimitReached, authorUnableToVerifyResult(e.opts.ProjectID, candidate.AttemptID, workPackageID, dim, reviewID, ep)
			}
			return protocol.OutcomeDriverError, authorUnableToVerifyResult(e.opts.ProjectID, candidate.AttemptID, workPackageID, dim, reviewID, ep)
		}

		if turnRes.PausedReason != "" || md.GetMeter(sessionConfig.SessionID).IsPaused() {
			return protocol.OutcomeLimitReached, authorUnableToVerifyResult(e.opts.ProjectID, candidate.AttemptID, workPackageID, dim, reviewID, ep)
		}

		if !turnRes.Usage.Input.Known || !turnRes.Usage.Output.Known {
			if !ep.Limits.AllowUnknownUsage || ep.Locality != protocol.LocalityLocal {
				return protocol.OutcomeLimitReached, authorUnableToVerifyResult(e.opts.ProjectID, candidate.AttemptID, workPackageID, dim, reviewID, ep)
			}
		}

		if len(turnRes.ToolCalls) == 0 {
			finalContent = turnRes.Content
			stoppedNormally = true
			break
		}

		var toolResults []drivers.ToolResult
		for _, tc := range turnRes.ToolCalls {
			tr, trErr := mediator.ExecuteTool(ctx, tc)
			if trErr != nil && ctx.Err() != nil {
				return protocol.OutcomeCancelled, authorUnableToVerifyResult(e.opts.ProjectID, candidate.AttemptID, workPackageID, dim, reviewID, ep)
			}
			toolResults = append(toolResults, tr)
		}

		turnInput = drivers.TurnInput{
			TurnID:      fmt.Sprintf("turn-%d", turn+1),
			ToolResults: toolResults,
		}
	}

	if !stoppedNormally {
		return protocol.OutcomeLimitReached, authorUnableToVerifyResult(e.opts.ProjectID, candidate.AttemptID, workPackageID, dim, reviewID, ep)
	}

	stripped, ok := stripFence(strings.TrimSpace(finalContent))
	if !ok {
		return protocol.OutcomeOutputInvalid, authorUnableToVerifyResult(e.opts.ProjectID, candidate.AttemptID, workPackageID, dim, reviewID, ep)
	}

	var parsed protocol.ReviewResult
	dec := json.NewDecoder(strings.NewReader(stripped))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&parsed); err != nil {
		return protocol.OutcomeOutputInvalid, authorUnableToVerifyResult(e.opts.ProjectID, candidate.AttemptID, workPackageID, dim, reviewID, ep)
	}
	if dec.More() {
		return protocol.OutcomeOutputInvalid, authorUnableToVerifyResult(e.opts.ProjectID, candidate.AttemptID, workPackageID, dim, reviewID, ep)
	}

	parsed.SchemaVersion = protocol.SchemaVersion1
	parsed.ReviewID = reviewID
	parsed.ProjectID = e.opts.ProjectID
	parsed.AttemptID = candidate.AttemptID
	parsed.WorkPackageID = workPackageID
	parsed.Dimension = dim
	revProfile := ep.EndpointID
	parsed.ReviewerProfile = &revProfile
	modelIdent := fmt.Sprintf("%s/%s@%s", ep.EndpointID, ep.ModelID, ep.ModelRevision)
	parsed.ModelIdentity = &modelIdent

	if err := parsed.Validate(); err != nil {
		return protocol.OutcomeOutputInvalid, authorUnableToVerifyResult(e.opts.ProjectID, candidate.AttemptID, workPackageID, dim, reviewID, ep)
	}

	if len(parsed.Findings) > 64 {
		return protocol.OutcomeOutputInvalid, authorUnableToVerifyResult(e.opts.ProjectID, candidate.AttemptID, workPackageID, dim, reviewID, ep)
	}
	for _, f := range parsed.Findings {
		if len(f.Statement) > 2048 {
			return protocol.OutcomeOutputInvalid, authorUnableToVerifyResult(e.opts.ProjectID, candidate.AttemptID, workPackageID, dim, reviewID, ep)
		}
	}

	return protocol.OutcomeCompleted, &parsed
}

func authorUnableToVerifyResult(
	projectID, attemptID, workPackageID string,
	dim protocol.ReviewDimension,
	reviewID string,
	ep execpolicy.ResolvedEndpoint,
) *protocol.ReviewResult {
	revProfile := ep.EndpointID
	modelIdent := fmt.Sprintf("%s/%s@%s", ep.EndpointID, ep.ModelID, ep.ModelRevision)
	return &protocol.ReviewResult{
		SchemaVersion:                  protocol.SchemaVersion1,
		ReviewID:                       reviewID,
		ProjectID:                      projectID,
		AttemptID:                      attemptID,
		WorkPackageID:                  workPackageID,
		Dimension:                      dim,
		ReviewerProfile:                &revProfile,
		ModelIdentity:                  &modelIdent,
		Verdict:                        protocol.VerdictUnableToVerify,
		Findings:                       []protocol.Finding{},
		MustCompliance:                 []protocol.GuidanceCompliance{},
		PrincipalEscalationRecommended: false,
	}
}

func stripFence(text string) (string, bool) {
	if !strings.HasPrefix(text, "```") {
		return text, true
	}
	nl := strings.IndexByte(text, '\n')
	if nl < 0 {
		return "", false
	}
	label := strings.TrimSpace(strings.TrimRight(text[3:nl], "\r"))
	if label != "" && !strings.EqualFold(label, "json") {
		return "", false
	}
	rest := text[nl+1:]
	body, last := "", rest
	if lastNL := strings.LastIndexByte(rest, '\n'); lastNL >= 0 {
		body, last = rest[:lastNL], rest[lastNL+1:]
	}
	if strings.TrimSpace(strings.TrimRight(last, "\r")) != "```" {
		return "", false
	}
	return body, true
}

func loadReviewerPromptTemplate(stateDir string) string {
	for _, candidate := range []string{
		filepath.Join(stateDir, "prompts", "reviewer.md"),
		"prompts/reviewer.md",
		"../prompts/reviewer.md",
		"../../prompts/reviewer.md",
	} {
		data, err := os.ReadFile(candidate)
		if err == nil && len(data) > 0 {
			return string(data)
		}
	}
	return "Role: Reviewer. Independent review and verification."
}
