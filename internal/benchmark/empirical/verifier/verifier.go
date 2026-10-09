package verifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/actors"
	"github.com/olostan/DevCadence/internal/benchmark/empirical"
	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/worktrees"
)

// Verifier implements empirical.IndependentVerifier (WP-M5-R3 Part B).
type Verifier struct {
	opts Options
}

var _ empirical.IndependentVerifier = (*Verifier)(nil)

// New constructs an IndependentVerifier with injected dependencies.
func New(opts Options) (*Verifier, error) {
	if opts.Worktrees == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "verifier: worktrees manager is required")
	}
	if opts.Repositories == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "verifier: repositories provider is required")
	}
	if opts.Runner == nil {
		opts.Runner = process.NewRunner()
	}
	if opts.BuildInfo == nil {
		opts.BuildInfo = DefaultBuildInfoSource()
	}
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}
	if opts.IDs == nil {
		opts.IDs = ids.NewULIDSource()
	}
	if opts.ScratchDir == "" {
		opts.ScratchDir = filepath.Join(os.TempDir(), "devcadence-verifier")
	}
	if err := os.MkdirAll(opts.ScratchDir, 0700); err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "verifier: create scratch directory")
	}
	// The final scratch directory itself must not be a symlink: verifier
	// artifacts must not be redirected through a caller-created leaf link.
	info, err := os.Lstat(opts.ScratchDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errs.New(errs.CategoryPolicyDenied, "verifier: scratch root must be a real directory: %v", err)
	}
	return &Verifier{opts: opts}, nil
}

// Verify implements empirical.IndependentVerifier.Verify following the exact 9-step algorithm.
func (v *Verifier) Verify(
	ctx context.Context,
	plan empirical.CampaignPlan,
	run empirical.PlannedRun,
	evidence empirical.RunEvidence,
	resolver empirical.ArtifactResolver,
) (empirical.VerifiedOutcome, error) {
	if err := ctx.Err(); err != nil {
		return empirical.VerifiedOutcome{}, err
	}

	// A separate worktree and sanitized environment do not confine arbitrary
	// candidate process code. Refuse before any profile check is executed unless
	// the embedding caller explicitly opts into trusted host process execution.
	// The production replay CLI never opts in.
	if !v.opts.PermitUnconfinedHostChecks {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryPolicyDenied,
			"VERIFIER_UNCONFINED_HOST_PROCESS: no OS-isolated check runner is configured; refusing candidate check execution")
	}

	// 1. Preconditions.
	if evidence.Status != empirical.StatusCompleted {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument,
			"run %q is not completed (status: %q)", run.RunID, evidence.Status)
	}
	if run.RunID != evidence.RunID || run.TaskID != evidence.TaskID || run.TaskDigest != evidence.TaskDigest ||
		run.Seed != evidence.Seed || run.Strategy != evidence.Strategy || run.Repetition != evidence.Repetition ||
		run.Endpoint != evidence.Endpoint {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument,
			"run %q identity or endpoint binding mismatch between plan and evidence", run.RunID)
	}

	// Audit and sanitize run.RunID (I4).
	if strings.Contains(run.RunID, "..") || strings.Contains(run.RunID, "/") || strings.Contains(run.RunID, "\\") {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument,
			"invalid run_id %q: path traversal characters rejected", run.RunID)
	}
	cleanScratchRoot := filepath.Clean(v.opts.ScratchDir)
	cleanRunScratch := filepath.Clean(filepath.Join(cleanScratchRoot, run.RunID))
	relScratch, err := filepath.Rel(cleanScratchRoot, cleanRunScratch)
	if err != nil || relScratch == "." || relScratch == ".." || strings.HasPrefix(relScratch, ".."+string(filepath.Separator)) {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument,
			"invalid run_id %q: path traversal beyond scratch dir", run.RunID)
	}

	// 2. Resolve and digest-check artifacts.
	res := resolver
	if res == nil {
		res = v.opts.Resolver
	}
	if res == nil {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument, "no artifact resolver provided")
	}

	rcptBytes, err := fetchArtifact(ctx, res, evidence.VerifierReceiptRef, evidence.VerifierReceiptDigest)
	if err != nil {
		return empirical.VerifiedOutcome{}, errs.Wrap(errs.CategoryInternal, err, "receipt unavailable")
	}
	var receipt VerifierReceipt
	decR := json.NewDecoder(bytes.NewReader(rcptBytes))
	decR.DisallowUnknownFields()
	if err := decR.Decode(&receipt); err != nil {
		return empirical.VerifiedOutcome{}, errs.Wrap(errs.CategoryInvalidArgument, err, "invalid receipt json")
	}
	if _, err := decR.Token(); err != io.EOF {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument, "trailing tokens in receipt")
	}

	sessBytes, err := fetchArtifact(ctx, res, evidence.SessionEvidenceRef, evidence.SessionEvidenceDigest)
	if err != nil {
		return empirical.VerifiedOutcome{}, errs.Wrap(errs.CategoryInternal, err, "session evidence unavailable")
	}
	var sess empirical.SessionEvidence
	decS := json.NewDecoder(bytes.NewReader(sessBytes))
	decS.DisallowUnknownFields()
	if err := decS.Decode(&sess); err != nil {
		return empirical.VerifiedOutcome{}, errs.Wrap(errs.CategoryInvalidArgument, err, "invalid session evidence json")
	}
	if _, err := decS.Token(); err != io.EOF {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument, "trailing tokens in session evidence")
	}
	if err := sess.Validate(); err != nil {
		return empirical.VerifiedOutcome{}, errs.Wrap(errs.CategoryInvalidArgument, err, "invalid session evidence")
	}
	sessDigest, err := sess.Digest()
	if err != nil || sessDigest != evidence.SessionEvidenceDigest {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument, "session evidence digest mismatch")
	}

	provBytes, err := fetchArtifact(ctx, res, sess.InvocationProvenanceDigest, sess.InvocationProvenanceDigest)
	if err != nil {
		return empirical.VerifiedOutcome{}, errs.Wrap(errs.CategoryInternal, err, "invocation provenance unavailable")
	}
	var prov protocol.InvocationProvenance
	decP := json.NewDecoder(bytes.NewReader(provBytes))
	decP.DisallowUnknownFields()
	if err := decP.Decode(&prov); err != nil {
		return empirical.VerifiedOutcome{}, errs.Wrap(errs.CategoryInvalidArgument, err, "invalid invocation provenance json")
	}
	if _, err := decP.Token(); err != io.EOF {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument, "trailing tokens in invocation provenance")
	}
	if err := prov.Validate(); err != nil {
		return empirical.VerifiedOutcome{}, errs.Wrap(errs.CategoryInvalidArgument, err, "invalid invocation provenance")
	}

	candidateBytes, err := fetchArtifact(ctx, res, evidence.CandidateArtifactRef, evidence.CandidateArtifactDigest)
	if err != nil {
		return empirical.VerifiedOutcome{}, errs.Wrap(errs.CategoryInternal, err, "candidate artifact unavailable")
	}

	// Cross-check IDs, digests, and identity.
	if receipt.ReceiptVersion != "1.0" {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument, "receipt_version must be 1.0, got %q", receipt.ReceiptVersion)
	}
	if receipt.CampaignID != plan.CampaignID || receipt.RunID != run.RunID || receipt.TaskDigest != run.TaskDigest ||
		receipt.Seed != run.Seed || receipt.Strategy != run.Strategy {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument, "receipt identity fields mismatch with plan/run")
	}

	epDigest, err := protocol.Digest(run.Endpoint)
	if err != nil {
		return empirical.VerifiedOutcome{}, err
	}
	if receipt.EndpointBindingDigest != epDigest || prov.EndpointBindingDigest != epDigest || sess.Endpoint != run.Endpoint {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument, "endpoint binding digest mismatch")
	}
	if receipt.PromptDigest != evidence.PromptDigest || sess.PromptDigest != evidence.PromptDigest {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument, "prompt digest mismatch")
	}
	if receipt.SessionEvidenceDigest != evidence.SessionEvidenceDigest {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument, "session evidence digest mismatch in receipt")
	}
	if receipt.CandidateCommit != evidence.CandidateCommit || receipt.CandidateArtifactDigest != evidence.CandidateArtifactDigest {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument, "candidate commit/artifact digest mismatch in receipt")
	}
	if receipt.SnapshotDigest != evidence.SnapshotDigest {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument, "snapshot digest mismatch in receipt")
	}
	if receipt.VerifierProducerID != evidence.VerifierProducerID {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument, "verifier producer id mismatch in receipt")
	}
	if receipt.VerifierSourceCommit != plan.VerifierSourceCommit {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument, "verifier source commit mismatch with plan")
	}
	if receipt.VerificationProfileDigest != plan.VerificationProfileDigest {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument, "verification profile digest mismatch with plan")
	}
	if sess.RunID != run.RunID || sess.CampaignID != plan.CampaignID || sess.TaskDigest != run.TaskDigest {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument, "session evidence identity mismatch with plan/run")
	}

	// 3. Determine own identity and profile.
	rev, modified, ok := v.opts.BuildInfo.VCSRevision()
	if !ok {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryPolicyDenied, "verifier build info unknown (ok=false)")
	}
	if modified {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryPolicyDenied, "verifier source tree modified (dirty build)")
	}
	if rev != plan.VerifierSourceCommit {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryPolicyDenied,
			"verifier source commit %q does not match plan %q", rev, plan.VerifierSourceCommit)
	}

	var profile *VerificationProfile
	if v.opts.Profile != nil {
		profile = v.opts.Profile
	} else {
		profBytes, err := fetchArtifact(ctx, res, plan.VerificationProfileDigest, plan.VerificationProfileDigest)
		if err != nil {
			return empirical.VerifiedOutcome{}, errs.Wrap(errs.CategoryInternal, err, "verification profile artifact unavailable")
		}
		profile, err = LoadProfile(profBytes)
		if err != nil {
			return empirical.VerifiedOutcome{}, errs.Wrap(errs.CategoryInvalidArgument, err, "failed to load verification profile")
		}
	}
	profDigest, err := profile.Digest()
	if err != nil || profDigest != plan.VerificationProfileDigest {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryPolicyDenied,
			"verification profile digest mismatch with plan (got %q, plan has %q)", profDigest, plan.VerificationProfileDigest)
	}

	// 4. Build Verifier ActorProvenance and Worker ActorProvenance; evaluate independence.
	verifierBasis := protocol.ActorBasis{
		EndpointID:    "devcadence-verifier",
		ModelID:       plan.VerifierSourceCommit,
		ModelRevision: plan.VerificationProfileDigest,
	}
	verifierActorID, err := actors.DeriveActorID("endpoint_model", verifierBasis)
	if err != nil {
		return empirical.VerifiedOutcome{}, errs.Wrap(errs.CategoryInternal, err, "failed to derive verifier actor id")
	}
	verifierProv := protocol.ActorProvenance{
		ActorID:      verifierActorID,
		InvocationID: v.opts.IDs.New("inv"),
		Role:         protocol.ProvenanceRoleVerifier,
	}
	if err := verifierProv.Validate("verifier", protocol.ProvenanceRoleVerifier); err != nil {
		return empirical.VerifiedOutcome{}, errs.Wrap(errs.CategoryInternal, err, "verifier provenance invalid")
	}

	// 5. Look up task in verification profile and verify candidate binding (B3).
	var task *TaskVerification
	for i := range profile.Tasks {
		if profile.Tasks[i].TaskID == run.TaskID {
			task = &profile.Tasks[i]
			break
		}
	}
	if task == nil {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInternal,
			"VERIFIER_INFRASTRUCTURE: task %q not found in verification profile", run.TaskID)
	}
	if task.TaskDigest != run.TaskDigest {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryIntegrity,
			"profile task digest %q does not match planned run task digest %q", task.TaskDigest, run.TaskDigest)
	}
	if task.BaseCommit != run.EWP.BaseCommit {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryIntegrity,
			"profile task base commit %q does not match planned run base commit %q", task.BaseCommit, run.EWP.BaseCommit)
	}

	// Original verifier receipts are claims. Bind the exact command vector to the
	// pinned profile before executing any check: matching exit codes alone does
	// not prove that the original producer ran the approved commands.
	if len(receipt.CommandArgvArrays) != len(task.Checks) {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryIntegrity,
			"INCONSISTENT_VERIFICATION: receipt command argv count %d differs from pinned profile %d",
			len(receipt.CommandArgvArrays), len(task.Checks))
	}
	for i, check := range task.Checks {
		claimed := receipt.CommandArgvArrays[i]
		if len(claimed) != len(check.Argv) {
			return empirical.VerifiedOutcome{}, errs.New(errs.CategoryIntegrity,
				"INCONSISTENT_VERIFICATION: receipt command argv length differs from pinned check %q", check.CheckID)
		}
		for j, arg := range check.Argv {
			if claimed[j] != arg {
				return empirical.VerifiedOutcome{}, errs.New(errs.CategoryIntegrity,
					"INCONSISTENT_VERIFICATION: receipt command argv differs from pinned check %q at argument %d", check.CheckID, j)
			}
		}
	}

	expectedRole := protocol.ProvenanceRoleImplementer
	if task.Class == "review" {
		expectedRole = protocol.ProvenanceRoleReviewer
	}
	if prov.Role != expectedRole {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument,
			"worker provenance role %q must be %s", prov.Role, expectedRole)
	}
	if prov.TaskID != run.TaskID {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument,
			"worker provenance task_id %q does not match run task %q", prov.TaskID, run.TaskID)
	}
	if run.EWP.ID != "" && prov.WorkPackageID != run.EWP.ID {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryIntegrity,
			"worker provenance work_package_id mismatch: %s != %s", prov.WorkPackageID, run.EWP.ID)
	}
	if prov.AttemptID != sess.AttemptID {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument,
			"worker provenance attempt_id %q mismatch with session attempt_id %q", prov.AttemptID, sess.AttemptID)
	}
	if prov.Basis.EndpointID != sess.Endpoint.EndpointID || prov.Basis.ModelID != sess.Endpoint.ModelID ||
		prov.Basis.ModelRevision != sess.Endpoint.ModelRevision {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument,
			"worker provenance basis does not match session evidence endpoint")
	}

	reDerivedWorkerActorID, err := actors.DeriveActorID("endpoint_model", prov.Basis)
	if err != nil {
		return empirical.VerifiedOutcome{}, errs.Wrap(errs.CategoryPolicyDenied, err, "failed to re-derive worker actor id")
	}
	if reDerivedWorkerActorID != prov.Actor.ActorID {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryPolicyDenied,
			"worker provenance stored actor_id %q differs from re-derived actor_id %q",
			prov.Actor.ActorID, reDerivedWorkerActorID)
	}

	workerProv := prov.Actor
	workerProv.ActorID = reDerivedWorkerActorID

	if !protocol.ActorsIndependent(workerProv, verifierProv) {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryPolicyDenied,
			"VERIFIER_NOT_INDEPENDENT: verifier actor %q is not independent of worker actor %q",
			verifierProv.ActorID, workerProv.ActorID)
	}

	qualityVerdict := empirical.QualityAccepted
	var worktreeDir string
	var revResult *protocol.ReviewResult

	// Use a fresh, privately created directory for each verification.
	// A predictable reused run_id path could contain attacker-planted log symlinks.
	runScratch, err := os.MkdirTemp(v.opts.ScratchDir, run.RunID+"-verify-")
	if err != nil {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInternal, "VERIFIER_INFRASTRUCTURE: create scratch directory: %v", err)
	}

	if task.Class == "review" {
		worktreeDir = runScratch
		decRev := json.NewDecoder(bytes.NewReader(candidateBytes))
		decRev.DisallowUnknownFields()
		var rr protocol.ReviewResult
		if err := decRev.Decode(&rr); err != nil {
			qualityVerdict = empirical.QualityRejected
		} else if _, err := decRev.Token(); err != io.EOF {
			qualityVerdict = empirical.QualityRejected
		} else if err := rr.Validate(); err != nil {
			qualityVerdict = empirical.QualityRejected
		} else {
			revResult = &rr
		}
	} else if task.Class == "implementation" {
		projectID := v.opts.ProjectID
		if projectID == "" {
			projectID = plan.CampaignID
		}
		repo, err := v.opts.Repositories.Repository(ctx, projectID)
		if err != nil && projectID != "devcadence" {
			if r2, err2 := v.opts.Repositories.Repository(ctx, "devcadence"); err2 == nil {
				repo = r2
				err = nil
			}
		}
		if err != nil || repo == nil {
			return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInternal,
				"VERIFIER_INFRASTRUCTURE: repository unavailable for project %q: %v", projectID, err)
		}

		candCommit, err := repo.ResolveCommit(ctx, evidence.CandidateCommit)
		if err != nil {
			return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInternal,
				"VERIFIER_INFRASTRUCTURE: candidate commit %q not found in repository: %v", evidence.CandidateCommit, err)
		}
		baseCommit, err := repo.ResolveCommit(ctx, task.BaseCommit)
		if err != nil {
			return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInternal,
				"VERIFIER_INFRASTRUCTURE: task base commit %q not found in repository: %v", task.BaseCommit, err)
		}

		// Verify sole parent equals task's BaseCommit.
		resParents, err := v.opts.Runner.Run(ctx, process.Spec{
			Executable: "git",
			Args:       []string{"-C", repo.Path, "rev-list", "--parents", "-n", "1", candCommit},
			Dir:        repo.Path,
			Env:        process.BaseEnv(),
			Timeout:    30 * time.Second,
		})
		if err != nil || !resParents.Success() {
			return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInternal,
				"VERIFIER_INFRASTRUCTURE: failed to inspect commit parents: %v", err)
		}
		tokens := strings.Fields(string(resParents.Stdout))
		if len(tokens) != 2 || tokens[1] != baseCommit {
			qualityVerdict = empirical.QualityRejected
		}

		// Verify every changed path lies inside task.WriteScope.
		resDiff, err := v.opts.Runner.Run(ctx, process.Spec{
			Executable: "git",
			Args:       []string{"-C", repo.Path, "diff", "--name-status", baseCommit + ".." + candCommit},
			Dir:        repo.Path,
			Env:        process.BaseEnv(),
			Timeout:    30 * time.Second,
		})
		if err != nil || !resDiff.Success() {
			return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInternal,
				"VERIFIER_INFRASTRUCTURE: git diff failed: %v", err)
		}
		diffLines := strings.Split(strings.TrimSpace(string(resDiff.Stdout)), "\n")
		for _, line := range diffLines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.Split(line, "\t")
			if len(parts) < 2 {
				continue
			}
			for _, p := range parts[1:] {
				if strings.HasPrefix(p, ".git") || strings.Contains(p, "..") {
					qualityVerdict = empirical.QualityRejected
					break
				}
				if !matchesWriteScope(p, task.WriteScope) {
					qualityVerdict = empirical.QualityRejected
					break
				}
			}
		}

		// Verify no symlinks or submodules.
		resTree, err := v.opts.Runner.Run(ctx, process.Spec{
			Executable: "git",
			Args:       []string{"-C", repo.Path, "diff-tree", "-r", "--no-commit-id", candCommit},
			Dir:        repo.Path,
			Env:        process.BaseEnv(),
			Timeout:    30 * time.Second,
		})
		if err != nil || !resTree.Success() {
			msg := fmt.Sprintf("%v", err)
			if err == nil {
				msg = strings.TrimSpace(string(resTree.Stderr))
				if msg == "" {
					msg = fmt.Sprintf("exit code %d", resTree.ExitCode)
				}
			}
			return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInternal,
				"VERIFIER_INFRASTRUCTURE: git diff-tree failed: %s", msg)
		}
		for _, line := range strings.Split(string(resTree.Stdout), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				mode := fields[1]
				if mode == "120000" || mode == "160000" {
					qualityVerdict = empirical.QualityRejected
				}
			}
		}

		// Create isolated worktree.
		wtAttemptID := run.RunID + "-verify"
		wtSpec := worktrees.Spec{
			ProjectID:  repo.ProjectID,
			TaskID:     task.TaskID,
			AttemptID:  wtAttemptID,
			BaseCommit: candCommit,
		}
		wt, err := v.opts.Worktrees.Create(ctx, repo, wtSpec)
		if err != nil {
			return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInternal,
				"VERIFIER_INFRASTRUCTURE: failed to create worktree: %v", err)
		}
		defer func() {
			_ = v.opts.Worktrees.Cleanup(context.Background(), repo, repo.ProjectID, wt.ID, worktrees.CleanupOptions{Force: true})
		}()
		worktreeDir = wt.Path
	}

	// 6. Execute CheckSpecs through process.Runner.
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		pathEnv = "/usr/local/bin:/usr/bin:/bin"
	}
	cleanEnv := []string{
		"PATH=" + pathEnv,
		"HOME=" + runScratch,
		"TMPDIR=" + runScratch,
		"GOFLAGS=-mod=readonly",
		"GOPROXY=off",
		"GOTOOLCHAIN=local",
		"HTTP_PROXY=",
		"HTTPS_PROXY=",
		"ALL_PROXY=",
		"NO_PROXY=",
		"http_proxy=",
		"https_proxy=",
		"all_proxy=",
		"no_proxy=",
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
	}

	checkCleanGitStatus := func(phase string) error {
		if task.Class != "implementation" || worktreeDir == "" {
			return nil
		}
		statusRes, err := v.opts.Runner.Run(ctx, process.Spec{
			Executable: "git",
			Args:       []string{"-C", worktreeDir, "status", "--porcelain"},
			Dir:        worktreeDir,
			Env:        cleanEnv,
			Timeout:    30 * time.Second,
		})
		if err != nil || !statusRes.Success() {
			return errs.New(errs.CategoryInternal,
				"VERIFIER_INFRASTRUCTURE: git status check failed (%s): %v", phase, err)
		}
		if strings.TrimSpace(string(statusRes.Stdout)) != "" {
			return errs.New(errs.CategoryPolicyDenied,
				"CHECKOUT_MODIFICATION: candidate checkout modified (%s): %s", phase, strings.TrimSpace(string(statusRes.Stdout)))
		}
		return nil
	}

	if err := checkCleanGitStatus("initial worktree"); err != nil {
		if errs.CategoryOf(err) == errs.CategoryPolicyDenied {
			qualityVerdict = empirical.QualityRejected
		} else {
			return empirical.VerifiedOutcome{}, err
		}
	}

	var verifiedArtifactRefs []string
	checkPassed := make(map[string]bool, len(task.Checks))
	var orderedExitCodes []int

	for _, check := range task.Checks {
		// Audit and sanitize check.CheckID (I4).
		if strings.Contains(check.CheckID, "..") || strings.Contains(check.CheckID, "/") || strings.Contains(check.CheckID, "\\") {
			return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument,
				"invalid check_id %q: path traversal characters rejected", check.CheckID)
		}
		logPath := filepath.Clean(filepath.Join(runScratch, fmt.Sprintf("check-%s.log", check.CheckID)))
		relLog, err := filepath.Rel(runScratch, logPath)
		if err != nil || relLog == "." || relLog == ".." || strings.HasPrefix(relLog, ".."+string(filepath.Separator)) {
			return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInvalidArgument,
				"invalid check_id %q: log path traversal beyond run scratch dir", check.CheckID)
		}

		checkDir := worktreeDir
		if check.Dir != "" && check.Dir != "." {
			checkDir = filepath.Join(worktreeDir, check.Dir)
		}

		if err := checkCleanGitStatus(fmt.Sprintf("before check %s", check.CheckID)); err != nil {
			if errs.CategoryOf(err) == errs.CategoryPolicyDenied {
				qualityVerdict = empirical.QualityRejected
			} else {
				return empirical.VerifiedOutcome{}, err
			}
		}

		runSpec := process.Spec{
			Executable: check.Argv[0],
			Args:       check.Argv[1:],
			Dir:        checkDir,
			Env:        cleanEnv,
			Timeout:    time.Duration(check.TimeoutSeconds) * time.Second,
		}
		res, err := v.opts.Runner.Run(ctx, runSpec)
		if err != nil {
			return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInternal,
				"VERIFIER_INFRASTRUCTURE: cannot start executable %q for check %q: %v",
				check.Argv[0], check.CheckID, err)
		}
		if res.Status == process.StatusTimeout {
			return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInternal,
				"VERIFIER_INFRASTRUCTURE: check %q timed out", check.CheckID)
		}
		if res.Status == process.StatusCancelled {
			return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInternal,
				"VERIFIER_INFRASTRUCTURE: check %q was cancelled", check.CheckID)
		}
		if res.Signal != "" {
			return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInternal,
				"VERIFIER_INFRASTRUCTURE: check %q killed by signal %s", check.CheckID, res.Signal)
		}
		if res.StdoutTruncated || res.StderrTruncated {
			return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInternal,
				"VERIFIER_INFRASTRUCTURE: check %q output cap exceeded", check.CheckID)
		}

		passed := (res.ExitCode == check.ExpectExitCode)
		if err := checkCleanGitStatus(fmt.Sprintf("after check %s", check.CheckID)); err != nil {
			if errs.CategoryOf(err) == errs.CategoryPolicyDenied {
				qualityVerdict = empirical.QualityRejected
				passed = false
			} else {
				return empirical.VerifiedOutcome{}, err
			}
		}

		var logBuf bytes.Buffer
		logBuf.WriteString(fmt.Sprintf("=== check: %s ===\nexit: %d\n--- stdout ---\n", check.CheckID, res.ExitCode))
		logBuf.Write(res.Stdout)
		logBuf.WriteString("\n--- stderr ---\n")
		logBuf.Write(res.Stderr)
		if err := writeVerifierArtifact(logPath, logBuf.Bytes()); err != nil {
			return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInternal,
				"VERIFIER_INFRASTRUCTURE: cannot write check %q artifact: %v", check.CheckID, err)
		}
		verifiedArtifactRefs = append(verifiedArtifactRefs, logPath)

		checkPassed[check.CheckID] = passed
		orderedExitCodes = append(orderedExitCodes, res.ExitCode)
	}

	if task.Class == "review" {
		revLogPath := filepath.Join(runScratch, "review-verification.log")
		if err := writeVerifierArtifact(revLogPath, []byte(fmt.Sprintf("review validation verdict: %s\n", qualityVerdict))); err != nil {
			return empirical.VerifiedOutcome{}, errs.New(errs.CategoryInternal,
				"VERIFIER_INFRASTRUCTURE: cannot write review artifact: %v", err)
		}
		verifiedArtifactRefs = append(verifiedArtifactRefs, revLogPath)
	}

	// 7. Compute results and quality verdict.
	var passedAcceptanceIDs []string
	for _, aid := range task.AcceptanceCheckIDs {
		if checkPassed[aid] {
			passedAcceptanceIDs = append(passedAcceptanceIDs, aid)
		}
	}

	seededDefectsCaught := 0
	reviewAnchorRe := regexp.MustCompile(`^([^:\s]+):([1-9][0-9]*)$`)

	for _, def := range task.Defects {
		if task.Class == "implementation" {
			caught := true
			for _, cid := range def.CatchCheckIDs {
				if !checkPassed[cid] {
					caught = false
					break
				}
			}
			if caught {
				seededDefectsCaught++
			}
		} else if task.Class == "review" {
			caught := false
			if def.Anchor != nil && revResult != nil {
				for _, f := range revResult.Findings {
					for _, rRef := range f.EvidenceRefs {
						matches := reviewAnchorRe.FindStringSubmatch(rRef)
						if len(matches) == 3 {
							refPath := path.Clean(filepath.ToSlash(matches[1]))
							if !strings.HasPrefix(refPath, "/") && !strings.Contains(refPath, "..") && refPath == def.Anchor.Path {
								lineNum, _ := strconv.Atoi(matches[2])
								if lineNum >= def.Anchor.StartLine && lineNum <= def.Anchor.EndLine {
									caught = true
									break
								}
							}
						}
					}
					if caught {
						break
					}
				}
			}
			if caught {
				seededDefectsCaught++
			}
		}
	}

	if task.Class == "implementation" {
		allPassed := true
		for _, aid := range task.AcceptanceCheckIDs {
			if !checkPassed[aid] {
				allPassed = false
				break
			}
		}
		if qualityVerdict == empirical.QualityAccepted && !allPassed {
			qualityVerdict = empirical.QualityRejected
		}
	}

	// 8. Deterministic re-run consistency check against original claimed receipt.
	if len(passedAcceptanceIDs) != len(receipt.PassedAcceptanceIDs) {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryIntegrity,
			"INCONSISTENT_VERIFICATION: passed acceptance IDs count mismatch: rerun=%d, claimed=%d",
			len(passedAcceptanceIDs), len(receipt.PassedAcceptanceIDs))
	}
	for i := range passedAcceptanceIDs {
		if passedAcceptanceIDs[i] != receipt.PassedAcceptanceIDs[i] {
			return empirical.VerifiedOutcome{}, errs.New(errs.CategoryIntegrity,
				"INCONSISTENT_VERIFICATION: passed acceptance IDs mismatch: rerun=%v, claimed=%v",
				passedAcceptanceIDs, receipt.PassedAcceptanceIDs)
		}
	}

	if len(orderedExitCodes) != len(receipt.CommandExitCodes) {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryIntegrity,
			"INCONSISTENT_VERIFICATION: command exit codes count mismatch: rerun=%d, claimed=%d",
			len(orderedExitCodes), len(receipt.CommandExitCodes))
	}
	for i := range orderedExitCodes {
		if orderedExitCodes[i] != receipt.CommandExitCodes[i] {
			return empirical.VerifiedOutcome{}, errs.New(errs.CategoryIntegrity,
				"INCONSISTENT_VERIFICATION: command exit codes mismatch: rerun=%v, claimed=%v",
				orderedExitCodes, receipt.CommandExitCodes)
		}
	}

	if seededDefectsCaught != receipt.SeededDefectsCaught {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryIntegrity,
			"INCONSISTENT_VERIFICATION: seeded defects caught mismatch: rerun=%d, claimed=%d",
			seededDefectsCaught, receipt.SeededDefectsCaught)
	}
	if len(task.Defects) != receipt.SeededDefectTotal {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryIntegrity,
			"INCONSISTENT_VERIFICATION: seeded defect total mismatch: rerun=%d, claimed=%d",
			len(task.Defects), receipt.SeededDefectTotal)
	}

	if qualityVerdict != receipt.QualityVerdict {
		return empirical.VerifiedOutcome{}, errs.New(errs.CategoryIntegrity,
			"INCONSISTENT_VERIFICATION: quality verdict mismatch: rerun=%s, claimed=%s",
			qualityVerdict, receipt.QualityVerdict)
	}

	// 9. Construct and return VerifiedOutcome.
	planDigest, err := protocol.Digest(plan)
	if err != nil {
		return empirical.VerifiedOutcome{}, err
	}

	return empirical.VerifiedOutcome{
		RunID:                       run.RunID,
		PlanDigest:                  planDigest,
		SessionDigest:               evidence.SessionEvidenceDigest,
		CandidateDigest:             evidence.CandidateArtifactDigest,
		SnapshotDigest:              evidence.SnapshotDigest,
		ReceiptDigest:               evidence.VerifierReceiptDigest,
		VerifierSourceCommit:        plan.VerifierSourceCommit,
		VerificationProfileDigest:   plan.VerificationProfileDigest,
		Worker:                      workerProv,
		Verifier:                    verifierProv,
		QualityVerdict:              qualityVerdict,
		SeededDefectTotal:           len(task.Defects),
		SeededDefectsCaught:         seededDefectsCaught,
		VerifiedCommandArtifactRefs: verifiedArtifactRefs,
	}, nil
}

func fetchArtifact(ctx context.Context, r empirical.ArtifactResolver, ref, digest string) ([]byte, error) {
	if strings.TrimSpace(ref) == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "artifact ref is empty")
	}
	if strings.TrimSpace(digest) == "" {
		return nil, errs.New(errs.CategoryInvalidArgument, "artifact digest is empty")
	}
	b, err := r.ReadVerified(ctx, ref, digest)
	if err != nil {
		return nil, err
	}
	if protocol.DigestBytes(b) != digest {
		return nil, errs.New(errs.CategoryIntegrity, "artifact %q does not match digest %q", ref, digest)
	}
	return b, nil
}

func matchesWriteScope(p string, scopes []string) bool {
	cleanP := path.Clean(p)
	for _, s := range scopes {
		if strings.HasSuffix(s, "/") {
			prefix := strings.TrimSuffix(s, "/") + "/"
			if strings.HasPrefix(cleanP, prefix) || cleanP == strings.TrimSuffix(s, "/") {
				return true
			}
		} else {
			if cleanP == path.Clean(s) {
				return true
			}
		}
	}
	return false
}

// writeVerifierArtifact fails closed on pre-existing files, including symlinks
// planted by a subprocess using the verifier's scratch directory.
func writeVerifierArtifact(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}
