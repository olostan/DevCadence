package taskexec

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/artifacts"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/storage"
	"github.com/olostan/DevCadence/internal/tasks"
	"github.com/olostan/DevCadence/internal/validation"
	"github.com/olostan/DevCadence/internal/worktrees"
)

// ProfileSource provides validation profiles by project and profile ID.
type ProfileSource interface {
	Profile(ctx context.Context, projectID, profileID string) (validation.Profile, error)
}

var forbiddenExecutables = map[string]bool{
	"sh":         true,
	"bash":       true,
	"zsh":        true,
	"csh":        true,
	"tcsh":       true,
	"dash":       true,
	"fish":       true,
	"ksh":        true,
	"cmd":        true,
	"cmd.exe":    true,
	"powershell": true,
	"pwsh":       true,
	"curl":       true,
	"wget":       true,
	"nc":         true,
	"netcat":     true,
	"ssh":        true,
	"scp":        true,
	"sftp":       true,
	"ftp":        true,
	"telnet":     true,
	"nmap":       true,
}

func validateProfileSafety(p validation.Profile) error {
	if err := p.Validate(); err != nil {
		return principal.NewCodedError(
			principal.CodeInvalidArgument,
			false,
			[]string{"invalid-profile"},
			fmt.Sprintf("invalid profile %q: %v", p.Name, err),
		)
	}

	for _, check := range p.Checks {
		if err := validateArgvAndDir(check.Argv, check.Dir); err != nil {
			return principal.NewCodedError(
				principal.CodePolicyDenied,
				false,
				[]string{"prohibited-profile-content"},
				fmt.Sprintf("profile %q check %q rejected: %v", p.Name, check.ID, err),
			)
		}
	}

	for _, svc := range p.Services {
		if err := validateArgvAndDir(svc.Argv, svc.Dir); err != nil {
			return principal.NewCodedError(
				principal.CodePolicyDenied,
				false,
				[]string{"prohibited-profile-content"},
				fmt.Sprintf("profile %q service %q rejected: %v", p.Name, svc.ID, err),
			)
		}
	}

	return nil
}

func validateArgvAndDir(argv []string, dir string) error {
	if strings.Contains(dir, "..") {
		return fmt.Errorf("directory traversal %q is prohibited", dir)
	}
	if filepath.IsAbs(dir) {
		return fmt.Errorf("absolute directory %q is prohibited", dir)
	}
	if len(argv) == 0 {
		return errors.New("empty argv")
	}

	exe := strings.ToLower(filepath.Base(argv[0]))
	if forbiddenExecutables[exe] {
		return fmt.Errorf("executable %q is prohibited", exe)
	}

	for i, arg := range argv {
		if i > 0 {
			argBase := strings.ToLower(filepath.Base(arg))
			if forbiddenExecutables[argBase] && (arg == "sh" || arg == "bash" || arg == "zsh") {
				return fmt.Errorf("shell argument %q is prohibited", arg)
			}
		}

		if strings.ContainsAny(arg, ";;|&><$`\n\r") {
			return fmt.Errorf("shell metacharacter in argument %q is prohibited", arg)
		}

		lowerArg := strings.ToLower(arg)
		if strings.Contains(lowerArg, "://") ||
			strings.Contains(lowerArg, "localhost") ||
			strings.Contains(lowerArg, "127.0.0.1") ||
			strings.Contains(lowerArg, "0.0.0.0") ||
			strings.HasPrefix(lowerArg, "--network") ||
			strings.HasPrefix(lowerArg, "http:") ||
			strings.HasPrefix(lowerArg, "https:") {
			return fmt.Errorf("network-looking argument %q is prohibited", arg)
		}
	}

	return nil
}

// Validate dispatches deterministic validation of a candidate commit.
func (e *Executor) Validate(
	ctx context.Context,
	caller principal.CallerContext,
	meta principal.CallMeta,
	candidate principal.CandidateRef,
	profileID string,
) (principal.OperationRef, error) {
	if e.opts.Profiles == nil {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodeModelUnavailable,
			false,
			[]string{"validate-not-implemented"},
			"validation runtime not available in this window",
		)
	}

	// Profile commands come from the (untrusted) repository and the candidate
	// contains model-written code: they run unconfined, so only in the explicit
	// owner-local mode (same gate as the post-check). Refuse before anything runs.
	if e.opts.ExecutionMode.normalized() != ExecutionUnsafeUnconfinedLocal {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodePolicyDenied, false, []string{"validate_requires_yolo"},
			"validation runs repository-defined commands and model-written code unconfined; no command was run. "+
				"Enable \"execution_mode\":\"yolo\" in $DEVCADENCE_HOME/config/selfhost.json (owner-local, NOT a sandbox) to allow it",
		)
	}

	// Synchronous checks:
	// 1. Caller and project mismatch checks
	if caller.ProjectID != e.opts.ProjectID || (meta.ProjectID != "" && meta.ProjectID != e.opts.ProjectID) {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodePolicyDenied,
			false,
			[]string{"project-mismatch"},
			fmt.Sprintf("project mismatch: executor owns %q, caller has %q", e.opts.ProjectID, caller.ProjectID),
		)
	}

	// 2. Validate non-empty required fields
	if strings.TrimSpace(candidate.Commit) == "" ||
		strings.TrimSpace(candidate.TaskID) == "" ||
		strings.TrimSpace(candidate.AttemptID) == "" ||
		strings.TrimSpace(profileID) == "" {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodeInvalidArgument,
			false,
			[]string{"missing-fields"},
			"candidate commit, task ID, attempt ID, and profile ID are required",
		)
	}

	// 3. Check state: Task must exist in ControlPlane
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

	// 4. Check Attempt: must exist and have status candidate_produced (or validating)
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

	if foundAttempt.Status != tasks.AttemptCandidateProduced && string(foundAttempt.Status) != "validating" {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodePolicyDenied,
			false,
			[]string{"invalid-attempt-state"},
			fmt.Sprintf("attempt %s status is %s, want candidate_produced", candidate.AttemptID, foundAttempt.Status),
		)
	}

	// 5. Candidate commit must equal candidate.Commit
	if foundAttempt.CandidateCommit != candidate.Commit {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodePolicyDenied,
			false,
			[]string{"candidate-commit-mismatch"},
			fmt.Sprintf("candidate commit mismatch: attempt has %q, request has %q", foundAttempt.CandidateCommit, candidate.Commit),
		)
	}

	// 6. Query profile
	profile, err := e.opts.Profiles.Profile(ctx, e.opts.ProjectID, profileID)
	if err != nil {
		return principal.OperationRef{}, err
	}

	// 7. Profile safety validation
	if err := validateProfileSafety(profile); err != nil {
		return principal.OperationRef{}, err
	}

	// Asynchronous run scheduled via e.opts.Registry.Start:
	// Calculate timeout: sum of check timeouts in profile + 60s buffer (minimum 120s)
	var sumTimeouts time.Duration
	for _, c := range profile.Checks {
		sumTimeouts += c.Timeout
	}
	for _, s := range profile.Services {
		sumTimeouts += s.StartupTimeout + s.ShutdownTimeout
	}
	timeout := sumTimeouts + 60*time.Second
	if timeout < 120*time.Second {
		timeout = 120 * time.Second
	}

	candidateNorm := candidate
	candidateNorm.TaskID = foundTask.ID

	opRef, err := e.opts.Registry.Start(e.opts.ProjectID, "validate", timeout, func(opCtx context.Context) (string, error) {
		return e.runValidate(opCtx, candidateNorm, profile)
	})
	if err != nil {
		return principal.OperationRef{}, err
	}

	return opRef, nil
}

func (e *Executor) runValidate(
	ctx context.Context,
	candidate principal.CandidateRef,
	profile validation.Profile,
) (string, error) {
	// Determine validation index n := count of existing ValidationCompleted events for attempt + 1
	evs, err := e.opts.ControlPlane.Events(ctx, storage.EventQuery{
		ProjectID: e.opts.ProjectID,
		TaskID:    candidate.TaskID,
		Types:     []events.Type{events.TypeValidationCompleted},
	})
	if err != nil {
		e.opts.Logger.ErrorContext(ctx, "failed to query validation events", "err", err)
		return "", err
	}

	var count int
	for _, ev := range evs {
		if vc, ok := ev.Payload.(*events.ValidationCompleted); ok && vc.AttemptID == candidate.AttemptID {
			count++
		}
	}

	e.valMu.Lock()
	if e.valIndices == nil {
		e.valIndices = make(map[string]int)
	}
	n := count + 1
	if last, ok := e.valIndices[candidate.AttemptID]; ok && last >= n {
		n = last + 1
	}
	e.valIndices[candidate.AttemptID] = n
	e.valMu.Unlock()

	// Check worktree manifest to avoid colliding with active or historical worktree IDs
	if wts, listErr := e.opts.Worktrees.List(e.opts.ProjectID); listErr == nil {
		for {
			candidateAttemptID := fmt.Sprintf("%s-v%d", candidate.AttemptID, n)
			collision := false
			for _, wt := range wts {
				if wt.AttemptID == candidateAttemptID {
					collision = true
					break
				}
			}
			if !collision {
				break
			}
			n++
			e.valMu.Lock()
			if e.valIndices[candidate.AttemptID] < n {
				e.valIndices[candidate.AttemptID] = n
			}
			e.valMu.Unlock()
		}
	}
	valAttemptID := fmt.Sprintf("%s-v%d", candidate.AttemptID, n)

	// Get repository
	repo, err := e.opts.Repositories.Repository(ctx, e.opts.ProjectID)
	if err != nil {
		e.opts.Logger.ErrorContext(ctx, "failed to get repository for validation", "err", err)
		return "", err
	}

	// Create clean worktree
	wt, err := e.opts.Worktrees.Create(ctx, repo, worktrees.Spec{
		ProjectID:  e.opts.ProjectID,
		TaskID:     candidate.TaskID,
		AttemptID:  valAttemptID,
		BaseCommit: candidate.Commit,
	})
	if err != nil {
		e.opts.Logger.ErrorContext(ctx, "failed to create validation worktree", "err", err)
		return "", err
	}

	// Prepare artifact store
	absStateDir, err := filepath.Abs(e.opts.StateDir)
	if err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = e.opts.Worktrees.Cleanup(cleanupCtx, repo, e.opts.ProjectID, wt.ID, worktrees.CleanupOptions{Force: false})
		return "", err
	}
	artStore, err := artifacts.NewStore(filepath.Join(absStateDir, "artifacts"), e.opts.IDs)
	if err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = e.opts.Worktrees.Cleanup(cleanupCtx, repo, e.opts.ProjectID, wt.ID, worktrees.CleanupOptions{Force: false})
		return "", err
	}

	runOpts := validation.RunOptions{
		Dir:       wt.Path,
		Runner:    e.opts.Runner,
		Artifacts: artStore,
		ProjectID: e.opts.ProjectID,
	}

	// Execute and record
	res, execErr := validation.ExecuteAndRecord(ctx, e.opts.ControlPlane, validation.ExecuteInput{
		ProjectID: e.opts.ProjectID,
		Subject: protocol.ValidationSubject{
			Kind:      protocol.ScopeAttempt,
			TaskID:    candidate.TaskID,
			AttemptID: candidate.AttemptID,
		},
		Commit:  candidate.Commit,
		Profile: profile,
		Run:     runOpts,
		Actor:   protocol.Actor{Kind: protocol.ActorControlPlane, ID: "taskexec"},
		IDs:     e.opts.IDs,
	})

	// Cleanup worktree (clean up if clean; if dirty, Cleanup with Force=false refuses and leaves it)
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cleanupCancel()
	cleanupErr := e.opts.Worktrees.Cleanup(cleanupCtx, repo, e.opts.ProjectID, wt.ID, worktrees.CleanupOptions{Force: false})
	if cleanupErr != nil {
		e.opts.Logger.WarnContext(cleanupCtx, "failed to cleanup validation worktree", "worktree_id", wt.ID, "err", cleanupErr)
	}

	if execErr != nil {
		e.opts.Logger.ErrorContext(ctx, "validation failed", "task_id", candidate.TaskID, "attempt_id", candidate.AttemptID, "err", execErr)
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", execErr
	}

	return res.ValidationResult.ValidationID, nil
}
