package taskexec_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/taskexec"
	"github.com/olostan/DevCadence/internal/tasks"
	"github.com/olostan/DevCadence/internal/validation"
	"github.com/olostan/DevCadence/internal/worktrees"
)

type mockProfileSource struct {
	profiles map[string]validation.Profile
}

func (m *mockProfileSource) Profile(ctx context.Context, projectID, profileID string) (validation.Profile, error) {
	if p, ok := m.profiles[profileID]; ok {
		return p, nil
	}
	return validation.Profile{}, errs.New(errs.CategoryNotFound, "profile %q not found", profileID)
}

func driveTaskToValidating(t *testing.T, f *testFixture, taskAlias, wpID, attemptID string) (string, string) {
	t.Helper()
	ctx := context.Background()
	taskID, _, _ := initProjectAndApproveWP(t, f, taskAlias, wpID, []string{"base.txt"})

	f.gitRepo.WriteFile("base.txt", "candidate code content\n")
	f.gitRepo.Commit("candidate commit")
	candidateCommit := f.gitRepo.Head()

	ps, err := f.harness.Service.ProjectState(ctx, f.projectID)
	if err != nil {
		t.Fatalf("project state: %v", err)
	}

	_, err = f.harness.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: f.projectID,
		Payload: &events.TaskDelegated{
			TaskID:        taskID,
			WorkPackageID: wpID,
			WorkerRole:    "implementer",
		},
	})
	if err != nil {
		t.Fatalf("append TaskDelegated: %v", err)
	}

	_, err = f.harness.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: f.projectID,
		Payload: &events.AttemptStarted{
			TaskID:               taskID,
			AttemptID:            attemptID,
			WorkPackageID:        wpID,
			WorkPackageVersion:   1,
			ProjectStateRevision: ps.StateRevision,
			WorkerRole:           "implementer",
			WorktreeID:           attemptID,
		},
	})
	if err != nil {
		t.Fatalf("append AttemptStarted: %v", err)
	}

	_, err = f.harness.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: f.projectID,
		Payload: &events.CandidateProduced{
			TaskID:          taskID,
			AttemptID:       attemptID,
			CandidateCommit: candidateCommit,
			Summary:         "candidate produced",
		},
	})
	if err != nil {
		t.Fatalf("append CandidateProduced: %v", err)
	}

	return taskID, candidateCommit
}

func TestValidate_SynchronousRejections(t *testing.T) {
	f := setupFixture(t, "proj-val-sync")
	profiles := &mockProfileSource{
		profiles: map[string]validation.Profile{
			"fast": {
				Name: "fast",
				Checks: []validation.CheckSpec{
					{ID: "ok", Argv: []string{"true"}, Timeout: 5 * time.Second},
				},
			},
		},
	}
	f.opts.Profiles = profiles

	exec, err := taskexec.New(f.opts)
	if err != nil {
		t.Fatalf("taskexec.New: %v", err)
	}

	taskID, commit := driveTaskToValidating(t, f, "T-01", "wp-01", "att-01")
	ctx := context.Background()
	caller := principal.CallerContext{PrincipalID: "princ_1", ProjectID: f.projectID}
	meta := principal.CallMeta{ProjectID: f.projectID}

	t.Run("project mismatch caller", func(t *testing.T) {
		badCaller := principal.CallerContext{PrincipalID: "princ_1", ProjectID: "other-project"}
		cand := principal.CandidateRef{TaskID: taskID, AttemptID: "att-01", Commit: commit}
		_, err := exec.Validate(ctx, badCaller, meta, cand, "fast")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var coded *principal.CodedError
		if !errors.As(err, &coded) || coded.Code() != principal.CodePolicyDenied {
			t.Fatalf("expected CodePolicyDenied, got %v", err)
		}
	})

	t.Run("project mismatch meta", func(t *testing.T) {
		badMeta := principal.CallMeta{ProjectID: "other-project"}
		cand := principal.CandidateRef{TaskID: taskID, AttemptID: "att-01", Commit: commit}
		_, err := exec.Validate(ctx, caller, badMeta, cand, "fast")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var coded *principal.CodedError
		if !errors.As(err, &coded) || coded.Code() != principal.CodePolicyDenied {
			t.Fatalf("expected CodePolicyDenied, got %v", err)
		}
	})

	t.Run("missing fields", func(t *testing.T) {
		cases := []struct {
			name string
			cand principal.CandidateRef
			prof string
		}{
			{"empty commit", principal.CandidateRef{TaskID: taskID, AttemptID: "att-01", Commit: ""}, "fast"},
			{"empty task", principal.CandidateRef{TaskID: "", AttemptID: "att-01", Commit: commit}, "fast"},
			{"empty attempt", principal.CandidateRef{TaskID: taskID, AttemptID: "", Commit: commit}, "fast"},
			{"empty profile", principal.CandidateRef{TaskID: taskID, AttemptID: "att-01", Commit: commit}, ""},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := exec.Validate(ctx, caller, meta, tc.cand, tc.prof)
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				var coded *principal.CodedError
				if !errors.As(err, &coded) || coded.Code() != principal.CodeInvalidArgument {
					t.Fatalf("expected CodeInvalidArgument, got %v", err)
				}
			})
		}
	})

	t.Run("task not found", func(t *testing.T) {
		cand := principal.CandidateRef{TaskID: "tsk_nonexistent", AttemptID: "att-01", Commit: commit}
		_, err := exec.Validate(ctx, caller, meta, cand, "fast")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var coded *principal.CodedError
		if !errors.As(err, &coded) || coded.Code() != principal.CodeNotFound {
			t.Fatalf("expected CodeNotFound, got %v", err)
		}
	})

	t.Run("attempt not found", func(t *testing.T) {
		cand := principal.CandidateRef{TaskID: taskID, AttemptID: "att_nonexistent", Commit: commit}
		_, err := exec.Validate(ctx, caller, meta, cand, "fast")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var coded *principal.CodedError
		if !errors.As(err, &coded) || coded.Code() != principal.CodeNotFound {
			t.Fatalf("expected CodeNotFound, got %v", err)
		}
	})

	t.Run("candidate commit mismatch", func(t *testing.T) {
		cand := principal.CandidateRef{TaskID: taskID, AttemptID: "att-01", Commit: "wrong_commit_sha"}
		_, err := exec.Validate(ctx, caller, meta, cand, "fast")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var coded *principal.CodedError
		if !errors.As(err, &coded) || coded.Code() != principal.CodePolicyDenied {
			t.Fatalf("expected CodePolicyDenied, got %v", err)
		}
	})
}

func TestValidate_AttemptNotInCandidateProduced(t *testing.T) {
	f := setupFixture(t, "proj-val-attempt-state")
	profiles := &mockProfileSource{
		profiles: map[string]validation.Profile{
			"fast": {
				Name: "fast",
				Checks: []validation.CheckSpec{
					{ID: "ok", Argv: []string{"true"}, Timeout: 5 * time.Second},
				},
			},
		},
	}
	f.opts.Profiles = profiles

	exec, err := taskexec.New(f.opts)
	if err != nil {
		t.Fatalf("taskexec.New: %v", err)
	}

	ctx := context.Background()
	taskID, _, _ := initProjectAndApproveWP(t, f, "T-State", "wp-state", []string{"base.txt"})

	f.gitRepo.WriteFile("base.txt", "state check\n")
	f.gitRepo.Commit("state commit")
	commit := f.gitRepo.Head()

	ps, err := f.harness.Service.ProjectState(ctx, f.projectID)
	if err != nil {
		t.Fatalf("project state: %v", err)
	}

	_, err = f.harness.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: f.projectID,
		Payload: &events.TaskDelegated{
			TaskID:        taskID,
			WorkPackageID: "wp-state",
			WorkerRole:    "implementer",
		},
	})
	if err != nil {
		t.Fatalf("append TaskDelegated: %v", err)
	}

	// Attempt is running, NOT candidate_produced
	_, err = f.harness.Service.AppendTypedEvent(ctx, controlplane.AppendTypedEventInput{
		ProjectID: f.projectID,
		Payload: &events.AttemptStarted{
			TaskID:               taskID,
			AttemptID:            "att-running",
			WorkPackageID:        "wp-state",
			WorkPackageVersion:   1,
			ProjectStateRevision: ps.StateRevision,
			WorkerRole:           "implementer",
			WorktreeID:           "att-running",
		},
	})
	if err != nil {
		t.Fatalf("append AttemptStarted: %v", err)
	}

	caller := principal.CallerContext{PrincipalID: "princ_1", ProjectID: f.projectID}
	meta := principal.CallMeta{ProjectID: f.projectID}
	cand := principal.CandidateRef{TaskID: taskID, AttemptID: "att-running", Commit: commit}

	_, err = exec.Validate(ctx, caller, meta, cand, "fast")
	if err == nil {
		t.Fatal("expected error for attempt in running state, got nil")
	}

	var coded *principal.CodedError
	if !errors.As(err, &coded) || coded.Code() != principal.CodePolicyDenied {
		t.Fatalf("expected CodePolicyDenied, got %v", err)
	}
}

func TestValidate_ProhibitedProfiles(t *testing.T) {
	cases := []struct {
		name    string
		profile validation.Profile
	}{
		{
			name: "shell executable sh",
			profile: validation.Profile{
				Name: "sh-exe",
				Checks: []validation.CheckSpec{
					{ID: "c1", Argv: []string{"sh", "-c", "true"}, Timeout: 5 * time.Second},
				},
			},
		},
		{
			name: "shell executable bash path",
			profile: validation.Profile{
				Name: "bash-path",
				Checks: []validation.CheckSpec{
					{ID: "c1", Argv: []string{"/bin/bash", "script.sh"}, Timeout: 5 * time.Second},
				},
			},
		},
		{
			name: "shell executable curl",
			profile: validation.Profile{
				Name: "curl-exe",
				Checks: []validation.CheckSpec{
					{ID: "c1", Argv: []string{"curl", "https://example.com"}, Timeout: 5 * time.Second},
				},
			},
		},
		{
			name: "metacharacter semicolon",
			profile: validation.Profile{
				Name: "semi",
				Checks: []validation.CheckSpec{
					{ID: "c1", Argv: []string{"go", "test; rm -rf /"}, Timeout: 5 * time.Second},
				},
			},
		},
		{
			name: "metacharacter pipe",
			profile: validation.Profile{
				Name: "pipe",
				Checks: []validation.CheckSpec{
					{ID: "c1", Argv: []string{"go", "test", "|", "cat"}, Timeout: 5 * time.Second},
				},
			},
		},
		{
			name: "metacharacter ampersand",
			profile: validation.Profile{
				Name: "amp",
				Checks: []validation.CheckSpec{
					{ID: "c1", Argv: []string{"go", "test", "&&", "echo", "done"}, Timeout: 5 * time.Second},
				},
			},
		},
		{
			name: "metacharacter redirection",
			profile: validation.Profile{
				Name: "redir",
				Checks: []validation.CheckSpec{
					{ID: "c1", Argv: []string{"go", "test", ">", "out.txt"}, Timeout: 5 * time.Second},
				},
			},
		},
		{
			name: "metacharacter dollar",
			profile: validation.Profile{
				Name: "dollar",
				Checks: []validation.CheckSpec{
					{ID: "c1", Argv: []string{"go", "test", "$(whoami)"}, Timeout: 5 * time.Second},
				},
			},
		},
		{
			name: "metacharacter backtick",
			profile: validation.Profile{
				Name: "backtick",
				Checks: []validation.CheckSpec{
					{ID: "c1", Argv: []string{"go", "test", "`id`"}, Timeout: 5 * time.Second},
				},
			},
		},
		{
			name: "network url argument",
			profile: validation.Profile{
				Name: "net-url",
				Checks: []validation.CheckSpec{
					{ID: "c1", Argv: []string{"mytool", "http://example.com/api"}, Timeout: 5 * time.Second},
				},
			},
		},
		{
			name: "network localhost argument",
			profile: validation.Profile{
				Name: "net-local",
				Checks: []validation.CheckSpec{
					{ID: "c1", Argv: []string{"mytool", "localhost:8080"}, Timeout: 5 * time.Second},
				},
			},
		},
		{
			name: "network ip argument",
			profile: validation.Profile{
				Name: "net-ip",
				Checks: []validation.CheckSpec{
					{ID: "c1", Argv: []string{"mytool", "127.0.0.1:9090"}, Timeout: 5 * time.Second},
				},
			},
		},
		{
			name: "network all interfaces argument",
			profile: validation.Profile{
				Name: "net-zero-ip",
				Checks: []validation.CheckSpec{
					{ID: "c1", Argv: []string{"mytool", "0.0.0.0:80"}, Timeout: 5 * time.Second},
				},
			},
		},
		{
			name: "directory traversal",
			profile: validation.Profile{
				Name: "dir-traversal",
				Checks: []validation.CheckSpec{
					{ID: "c1", Dir: "../other", Argv: []string{"true"}, Timeout: 5 * time.Second},
				},
			},
		},
		{
			name: "absolute directory",
			profile: validation.Profile{
				Name: "dir-abs",
				Checks: []validation.CheckSpec{
					{ID: "c1", Dir: "/tmp", Argv: []string{"true"}, Timeout: 5 * time.Second},
				},
			},
		},
		{
			name: "service with prohibited argv",
			profile: validation.Profile{
				Name: "svc-prohibited",
				Checks: []validation.CheckSpec{
					{ID: "c1", Argv: []string{"true"}, Timeout: 5 * time.Second},
				},
				Services: []validation.ServiceSpec{
					{
						ID:             "s1",
						Argv:           []string{"bash", "-c", "sleep 1"},
						StartupTimeout: 5 * time.Second, ShutdownTimeout: 5 * time.Second,
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := setupFixture(t, "proj-val-prohib")
			f.opts.Profiles = &mockProfileSource{
				profiles: map[string]validation.Profile{
					"bad": tc.profile,
				},
			}
			exec, err := taskexec.New(f.opts)
			if err != nil {
				t.Fatalf("taskexec.New: %v", err)
			}

			taskID, commit := driveTaskToValidating(t, f, "T-Prohib", "wp-prohib", "att-prohib")
			ctx := context.Background()
			caller := principal.CallerContext{PrincipalID: "princ_1", ProjectID: f.projectID}
			meta := principal.CallMeta{ProjectID: f.projectID}
			cand := principal.CandidateRef{TaskID: taskID, AttemptID: "att-prohib", Commit: commit}

			_, err = exec.Validate(ctx, caller, meta, cand, "bad")
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			var coded *principal.CodedError
			if !errors.As(err, &coded) || coded.Code() != principal.CodePolicyDenied {
				t.Fatalf("expected CodePolicyDenied for %s, got %v", tc.name, err)
			}
		})
	}
}

func TestValidate_AsynchronousExecution_Success(t *testing.T) {
	f := setupFixture(t, "proj-val-async-ok")
	profiles := &mockProfileSource{
		profiles: map[string]validation.Profile{
			"fast-pass": {
				Name: "fast-pass",
				Checks: []validation.CheckSpec{
					{
						ID:      "check-true",
						Kind:    "test",
						Argv:    []string{"true"},
						Timeout: 2 * time.Second,
					},
				},
			},
		},
	}
	f.opts.Profiles = profiles

	exec, err := taskexec.New(f.opts)
	if err != nil {
		t.Fatalf("taskexec.New: %v", err)
	}

	taskID, commit := driveTaskToValidating(t, f, "T-Async", "wp-async", "att-async")
	ctx := context.Background()
	caller := principal.CallerContext{PrincipalID: "princ_1", ProjectID: f.projectID}
	meta := principal.CallMeta{ProjectID: f.projectID}
	cand := principal.CandidateRef{TaskID: taskID, AttemptID: "att-async", Commit: commit}

	opRef, err := exec.Validate(ctx, caller, meta, cand, "fast-pass")
	if err != nil {
		t.Fatalf("exec.Validate: %v", err)
	}
	if opRef.Kind != "validate" {
		t.Fatalf("opRef.Kind = %q, want validate", opRef.Kind)
	}
	if opRef.Status != principal.StatusRunning {
		t.Fatalf("opRef.Status = %q, want running", opRef.Status)
	}

	// Wait for async validation to complete
	completedRef := f.registry.Wait(ctx, opRef, 10*time.Second)
	if completedRef.Status != principal.StatusCompleted {
		t.Fatalf("completedRef.Status = %q, want completed", completedRef.Status)
	}

	status, err := f.registry.Lookup(f.projectID, opRef)
	if err != nil {
		t.Fatalf("registry.Lookup: %v", err)
	}
	if status.Operation.Status != principal.StatusCompleted {
		t.Fatalf("status.Operation.Status = %q, want completed", status.Operation.Status)
	}
	valID := status.ResultHandle
	if valID == "" {
		t.Fatal("expected non-empty ResultHandle (ValidationID), got empty")
	}

	// Verify durable ValidationResult record in control plane
	stored, err := f.harness.Service.Record(ctx, f.projectID, "ValidationResult", valID, 1)
	if err != nil {
		t.Fatalf("Record ValidationResult: %v", err)
	}
	var valResult protocol.ValidationResult
	if err := protocol.Unmarshal([]byte(stored.Document), &valResult); err != nil {
		t.Fatalf("unmarshal ValidationResult: %v", err)
	}
	if valResult.Status != protocol.ValidationPass {
		t.Fatalf("valResult.Status = %s, want pass", valResult.Status)
	}
	if valResult.Commit != commit {
		t.Fatalf("valResult.Commit = %s, want %s", valResult.Commit, commit)
	}

	// Verify task transitioned to reviewing
	detail, err := f.harness.Service.TaskDetail(ctx, f.projectID, "T-Async")
	if err != nil {
		t.Fatalf("TaskDetail: %v", err)
	}
	if detail.Task.State != tasks.StateReviewing {
		t.Fatalf("detail.Task.State = %s, want reviewing", detail.Task.State)
	}

	// Verify ValidationCompleted event in history
	var foundEvent bool
	for _, ev := range detail.History {
		if ev.EventType == events.TypeValidationCompleted {
			if vc, ok := ev.Payload.(*events.ValidationCompleted); ok {
				if vc.ValidationID == valID && vc.Status == protocol.ValidationPass {
					foundEvent = true
					break
				}
			}
		}
	}
	if !foundEvent {
		t.Fatal("ValidationCompleted event not found in task history")
	}

	// Verify ephemeral worktree was cleaned up
	wts, err := f.worktrees.List(f.projectID)
	if err != nil {
		t.Fatalf("worktrees.List: %v", err)
	}
	for _, wt := range wts {
		if wt.AttemptID == "att-async-v1" && wt.Status == worktrees.StatusActive {
			t.Fatalf("expected worktree %s to be cleaned up, but found active", wt.ID)
		}
	}
}

func TestValidate_AsynchronousExecution_CheckFailure(t *testing.T) {
	f := setupFixture(t, "proj-val-async-fail")
	profiles := &mockProfileSource{
		profiles: map[string]validation.Profile{
			"fast-fail": {
				Name: "fast-fail",
				Checks: []validation.CheckSpec{
					{
						ID:      "check-false",
						Kind:    "test",
						Argv:    []string{"false"},
						Timeout: 2 * time.Second,
					},
				},
			},
		},
	}
	f.opts.Profiles = profiles

	exec, err := taskexec.New(f.opts)
	if err != nil {
		t.Fatalf("taskexec.New: %v", err)
	}

	taskID, commit := driveTaskToValidating(t, f, "T-Fail", "wp-fail", "att-fail")
	ctx := context.Background()
	caller := principal.CallerContext{PrincipalID: "princ_1", ProjectID: f.projectID}
	meta := principal.CallMeta{ProjectID: f.projectID}
	cand := principal.CandidateRef{TaskID: taskID, AttemptID: "att-fail", Commit: commit}

	opRef, err := exec.Validate(ctx, caller, meta, cand, "fast-fail")
	if err != nil {
		t.Fatalf("exec.Validate: %v", err)
	}

	completedRef := f.registry.Wait(ctx, opRef, 10*time.Second)
	if completedRef.Status != principal.StatusCompleted {
		t.Fatalf("completedRef.Status = %q, want completed", completedRef.Status)
	}

	status, err := f.registry.Lookup(f.projectID, opRef)
	if err != nil {
		t.Fatalf("registry.Lookup: %v", err)
	}
	valID := status.ResultHandle
	if valID == "" {
		t.Fatal("expected non-empty ResultHandle (ValidationID), got empty")
	}

	// Verify durable ValidationResult recorded fail outcome
	stored, err := f.harness.Service.Record(ctx, f.projectID, "ValidationResult", valID, 1)
	if err != nil {
		t.Fatalf("Record ValidationResult: %v", err)
	}
	var valResult protocol.ValidationResult
	if err := protocol.Unmarshal([]byte(stored.Document), &valResult); err != nil {
		t.Fatalf("unmarshal ValidationResult: %v", err)
	}
	if valResult.Status != protocol.ValidationFail {
		t.Fatalf("valResult.Status = %s, want fail", valResult.Status)
	}

	// Verify task transitioned back to running on failed validation
	detail, err := f.harness.Service.TaskDetail(ctx, f.projectID, "T-Fail")
	if err != nil {
		t.Fatalf("TaskDetail: %v", err)
	}
	if detail.Task.State != tasks.StateRunning {
		t.Fatalf("detail.Task.State = %s, want running", detail.Task.State)
	}
}

func TestValidate_ConcurrentValidations(t *testing.T) {
	f := setupFixture(t, "proj-val-concurrent")
	profiles := &mockProfileSource{
		profiles: map[string]validation.Profile{
			"fast": {
				Name: "fast",
				Checks: []validation.CheckSpec{
					{
						ID:      "check-true",
						Kind:    "test",
						Argv:    []string{"true"},
						Timeout: 2 * time.Second,
					},
				},
			},
		},
	}
	f.opts.Profiles = profiles

	exec, err := taskexec.New(f.opts)
	if err != nil {
		t.Fatalf("taskexec.New: %v", err)
	}

	taskID, commit := driveTaskToValidating(t, f, "T-Conc", "wp-conc", "att-conc")
	ctx := context.Background()
	caller := principal.CallerContext{PrincipalID: "princ_1", ProjectID: f.projectID}
	meta := principal.CallMeta{ProjectID: f.projectID}
	cand := principal.CandidateRef{TaskID: taskID, AttemptID: "att-conc", Commit: commit}

	opRef1, err1 := exec.Validate(ctx, caller, meta, cand, "fast")
	if err1 != nil {
		t.Fatalf("exec.Validate 1: %v", err1)
	}

	opRef2, err2 := exec.Validate(ctx, caller, meta, cand, "fast")
	if err2 != nil {
		t.Fatalf("exec.Validate 2: %v", err2)
	}

	if opRef1.ID == opRef2.ID {
		t.Fatalf("expected distinct operation IDs, got both %s", opRef1.ID)
	}

	// Wait for both operations
	f.registry.Wait(ctx, opRef1, 10*time.Second)
	f.registry.Wait(ctx, opRef2, 10*time.Second)

	st1, err := f.registry.Lookup(f.projectID, opRef1)
	if err != nil {
		t.Fatalf("Lookup op1: %v", err)
	}
	st2, err := f.registry.Lookup(f.projectID, opRef2)
	if err != nil {
		t.Fatalf("Lookup op2: %v", err)
	}

	// Both operations must be tracked in the registry with terminal status
	if st1.Operation.Status != principal.StatusCompleted && st1.Operation.Status != principal.StatusFailed {
		t.Fatalf("op1 status = %s, want completed or failed", st1.Operation.Status)
	}
	if st2.Operation.Status != principal.StatusCompleted && st2.Operation.Status != principal.StatusFailed {
		t.Fatalf("op2 status = %s, want completed or failed", st2.Operation.Status)
	}

	// At least one operation completed successfully
	if st1.Operation.Status != principal.StatusCompleted && st2.Operation.Status != principal.StatusCompleted {
		t.Fatalf("expected at least one operation to complete, got st1=%s st2=%s", st1.Operation.Status, st2.Operation.Status)
	}

	// Ensure no worktrees were leaked
	wts, err := f.worktrees.List(f.projectID)
	if err != nil {
		t.Fatalf("worktrees.List: %v", err)
	}
	for _, wt := range wts {
		if wt.Status == worktrees.StatusActive && (wt.AttemptID == "att-conc-v1" || wt.AttemptID == "att-conc-v2") {
			t.Fatalf("expected validation worktree %s to be cleaned up, but found active", wt.ID)
		}
	}
}
