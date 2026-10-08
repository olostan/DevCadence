package reviewexec

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/protocol"
)

type intentAbsentGuard struct {
	taskID          string
	attemptID       string
	candidateCommit string
	intentID        string
}

func (g intentAbsentGuard) Check(ctx context.Context, view controlplane.BatchReadView) error {
	ps := view.ProjectState()
	if ps == nil {
		return principal.NewCodedError(principal.CodeInternal, false, []string{"nil-project-state"}, "nil project state in guard")
	}
	running := false
	for _, id := range ps.Tasks.Running {
		if id == g.taskID || id == "tsk_"+g.taskID || g.taskID == "tsk_"+id {
			running = true
			break
		}
	}
	if !running {
		return principal.NewCodedError(
			principal.CodeConflict,
			false,
			[]string{"task-not-running"},
			fmt.Sprintf("task %s is not running", g.taskID),
		)
	}

	if g.attemptID == "" || g.candidateCommit == "" {
		return principal.NewCodedError(
			principal.CodeConflict,
			false,
			[]string{"missing-candidate-evidence"},
			"attemptID and candidateCommit required",
		)
	}

	storedWorker, err := view.Record(ctx, "InvocationProvenance", "prov_"+g.attemptID, 1)
	if err != nil && errs.CategoryOf(err) == errs.CategoryNotFound {
		storedWorker, err = view.Record(ctx, "InvocationProvenance", g.attemptID+":implementer", 1)
	}
	if err != nil {
		return principal.NewCodedError(
			principal.CodeConflict,
			false,
			[]string{"missing-candidate-evidence"},
			fmt.Sprintf("candidate evidence missing for attempt %s: %v", g.attemptID, err),
		)
	}
	if storedWorker.Document == "" {
		return principal.NewCodedError(
			principal.CodeConflict,
			false,
			[]string{"empty-candidate-evidence"},
			fmt.Sprintf("candidate evidence document is empty for attempt %s", g.attemptID),
		)
	}
	var workerProv protocol.InvocationProvenance
	if err := json.Unmarshal([]byte(storedWorker.Document), &workerProv); err != nil {
		return principal.NewCodedError(
			principal.CodeConflict,
			false,
			[]string{"malformed-candidate-evidence"},
			fmt.Sprintf("failed to decode candidate evidence: %v", err),
		)
	}
	if workerProv.CandidateCommit == "" {
		return principal.NewCodedError(
			principal.CodeConflict,
			false,
			[]string{"empty-candidate-commit"},
			fmt.Sprintf("candidate evidence for attempt %s has no candidate commit", g.attemptID),
		)
	}
	if workerProv.CandidateCommit != g.candidateCommit {
		return principal.NewCodedError(
			principal.CodeConflict,
			false,
			[]string{"candidate-commit-mismatch"},
			fmt.Sprintf("candidate commit mismatch: provenance has %s, request has %s", workerProv.CandidateCommit, g.candidateCommit),
		)
	}

	_, err = view.Record(ctx, "ReviewInvocationIntent", g.intentID, 1)
	if err == nil {
		return principal.NewCodedError(
			principal.CodePolicyDenied,
			false,
			[]string{"review-already-recorded"},
			fmt.Sprintf("review intent %s already exists", g.intentID),
		)
	}
	if errs.CategoryOf(err) != errs.CategoryNotFound {
		return err
	}
	return nil
}

type terminalPersistenceGuard struct {
	intentID       string
	expectedIntent protocol.ReviewInvocationIntent
	intentDigest   string
	reviewID       string
}

func (g terminalPersistenceGuard) Check(ctx context.Context, view controlplane.BatchReadView) error {
	stored, err := view.Record(ctx, "ReviewInvocationIntent", g.intentID, 1)
	if err != nil {
		return errs.Wrap(errs.CategoryConflict, err, "review intent missing")
	}
	if g.intentDigest != "" && stored.Digest != g.intentDigest {
		return errs.New(errs.CategoryConflict, "review intent digest mismatch: %s != %s", stored.Digest, g.intentDigest)
	}
	var intent protocol.ReviewInvocationIntent
	if err := protocol.Unmarshal([]byte(stored.Document), &intent); err != nil {
		return errs.Wrap(errs.CategoryIntegrity, err, "failed to unmarshal intent")
	}
	if intent.ReviewID != g.expectedIntent.ReviewID ||
		intent.InvocationID != g.expectedIntent.InvocationID ||
		intent.Dimension != g.expectedIntent.Dimension ||
		intent.CandidateCommit != g.expectedIntent.CandidateCommit ||
		intent.ReviewerBasis != g.expectedIntent.ReviewerBasis ||
		intent.IndependenceBasis != g.expectedIntent.IndependenceBasis ||
		intent.EndpointBindingDigest != g.expectedIntent.EndpointBindingDigest {
		return errs.New(errs.CategoryConflict, "review intent fields mismatch")
	}

	_, err = view.Record(ctx, "ReviewInvocation", g.reviewID, 1)
	if err == nil {
		return errs.New(errs.CategoryConflict, "review invocation %s already completed", g.reviewID)
	}
	if errs.CategoryOf(err) != errs.CategoryNotFound {
		return err
	}
	return nil
}
