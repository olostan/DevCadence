package verifier_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/actors"
	"github.com/olostan/DevCadence/internal/benchmark/empirical"
	"github.com/olostan/DevCadence/internal/benchmark/empirical/verifier"
	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/execrt"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/repository"
	"github.com/olostan/DevCadence/internal/worktrees"
)

type mockBuildInfoSource struct {
	rev      string
	modified bool
	ok       bool
}

func (m mockBuildInfoSource) VCSRevision() (string, bool, bool) {
	return m.rev, m.modified, m.ok
}

type testArtifactResolver map[string][]byte

func (r testArtifactResolver) ReadVerified(_ context.Context, ref, _ string) ([]byte, error) {
	b, ok := r[ref]
	if !ok {
		return nil, errors.New("artifact not found: " + ref)
	}
	return b, nil
}

func setupGitRepo(t *testing.T) (*repository.Repository, string, string, string) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "verifier-repo-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })

	runGit := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = tempDir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v, out: %s", args, err, string(out))
		}
		return string(out)
	}

	runGit("init")
	runGit("config", "user.name", "Test")
	runGit("config", "user.email", "test@example.com")
	runGit("checkout", "-b", "main")

	// Base commit
	if err := os.MkdirAll(filepath.Join(tempDir, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "src", "base.txt"), []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit("add", ".")
	runGit("commit", "-m", "base commit")
	baseCommit := strings.TrimSpace(runGit("rev-parse", "HEAD"))

	// Good candidate commit (touches src/foo.txt inside write_scope ["src/"])
	if err := os.WriteFile(filepath.Join(tempDir, "src", "foo.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit("add", ".")
	runGit("commit", "-m", "cand good commit")
	candGoodCommit := strings.TrimSpace(runGit("rev-parse", "HEAD"))

	// Bad candidate commit (touches outside/bad.txt outside write_scope ["src/"])
	runGit("checkout", "-b", "bad-branch", baseCommit)
	if err := os.MkdirAll(filepath.Join(tempDir, "outside"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "outside", "bad.txt"), []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit("add", ".")
	runGit("commit", "-m", "cand bad commit")
	candBadCommit := strings.TrimSpace(runGit("rev-parse", "HEAD"))

	runGit("checkout", "main")

	repo, err := repository.Register(context.Background(), "proj-01", tempDir, repository.Options{})
	if err != nil {
		t.Fatalf("repository.Register failed: %v", err)
	}

	return repo, baseCommit, candGoodCommit, candBadCommit
}

func setupBaseTestDependencies(t *testing.T) (
	*repository.Repository,
	string,
	string,
	string,
	*worktrees.Manager,
	execrt.RepositoryProvider,
	string,
) {
	t.Helper()
	repo, baseCommit, candGoodCommit, candBadCommit := setupGitRepo(t)

	wtRoot, err := os.MkdirTemp("", "verifier-wt-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(wtRoot) })

	wtManager, err := worktrees.NewManager(wtRoot, process.NewRunner())
	if err != nil {
		t.Fatal(err)
	}

	repoProvider := execrt.NewSingleRepositoryProvider(repo)

	scratchDir, err := os.MkdirTemp("", "verifier-scratch-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(scratchDir) })

	return repo, baseCommit, candGoodCommit, candBadCommit, wtManager, repoProvider, scratchDir
}

func helperMakeValidFixtures(
	t *testing.T,
	now time.Time,
	baseCommit, candCommit, sourceCommit string,
	profile verifier.VerificationProfile,
	class string,
	qualityVerdict string,
	candContent []byte,
) (
	empirical.CampaignPlan,
	empirical.PlannedRun,
	empirical.RunEvidence,
	testArtifactResolver,
) {
	t.Helper()
	profileDigest, err := profile.Digest()
	if err != nil {
		t.Fatal(err)
	}

	plan := empirical.CampaignPlan{
		CampaignID:                "proj-01",
		VerifierSourceCommit:      sourceCommit,
		VerificationProfileDigest: profileDigest,
	}

	ep := empirical.EndpointBinding{
		EndpointID:            "ep-worker",
		DriverID:              "drv-worker",
		ModelID:               "mod-worker",
		ModelRevision:         "rev-worker",
		ChannelID:             "chan-worker",
		CapabilityClass:       "local_small",
		RuntimeVersion:        "1.0",
		ContextProfileDigest:  "sha256:3333333333333333333333333333333333333333333333333333333333333333",
		PolicyDigest:          "sha256:4444444444444444444444444444444444444444444444444444444444444444",
		SubscriptionQuotaUnit: "unknown",
	}
	epDigest, _ := protocol.Digest(ep)

	run := empirical.PlannedRun{
		RunID:      "run-01",
		TaskID:     profile.Tasks[0].TaskID,
		TaskDigest: profile.Tasks[0].TaskDigest,
		Seed:       "seed-1",
		Strategy:   "full_history",
		Repetition: 1,
		Endpoint:   ep,
		EWP:        empirical.ClosedEWPBinding{ID: "ewp-01", BaseCommit: baseCommit},
	}

	workerBasis := protocol.ActorBasis{
		EndpointID:    ep.EndpointID,
		ModelID:       ep.ModelID,
		ModelRevision: ep.ModelRevision,
	}
	workerActorID, err := actors.DeriveActorID("endpoint_model", workerBasis)
	if err != nil {
		t.Fatal(err)
	}

	role := protocol.ProvenanceRoleImplementer
	dim := protocol.ReviewDimension("")
	if class == "review" {
		role = protocol.ProvenanceRoleReviewer
		dim = protocol.DimensionCorrectness
	}

	workerProv := protocol.ActorProvenance{
		ActorID:      workerActorID,
		InvocationID: "inv-worker-01",
		Role:         role,
	}

	invProv := protocol.InvocationProvenance{
		SchemaVersion:         "1.0",
		ProvenanceID:          "prov-01",
		ProjectID:             "proj-01",
		TaskID:                run.TaskID,
		AttemptID:             run.RunID,
		WorkPackageID:         "ewp-01",
		Role:                  role,
		Actor:                 workerProv,
		Basis:                 workerBasis,
		IndependenceBasis:     "endpoint_model",
		EndpointBindingDigest: epDigest,
		ContextManifestDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PromptDigest:          "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		CandidateCommit:       candCommit,
		Dimension:             dim,
		StartedAt:             now.Add(-time.Hour),
	}
	invProvBytes, err := protocol.CanonicalJSON(invProv)
	if err != nil {
		t.Fatal(err)
	}
	invProvDigest := protocol.DigestBytes(invProvBytes)

	candBytes := candContent
	candDigest := protocol.DigestBytes(candBytes)

	zeroUSD := 0.0
	zeroSec := 0.0
	sessUsage := map[string]empirical.Measurement{
		"cumulative_input_tokens": {Known: false, Unit: "token", Provenance: empirical.ProvenanceUnknown, EvidenceRef: "meter"},
		"cached_input_tokens":     {Known: false, Unit: "token", Provenance: empirical.ProvenanceUnknown, EvidenceRef: "meter"},
		"output_tokens":           {Known: false, Unit: "token", Provenance: empirical.ProvenanceUnknown, EvidenceRef: "meter"},
		"resident_peak_tokens":    {Known: false, Unit: "token", Provenance: empirical.ProvenanceUnknown, EvidenceRef: "meter"},
		"api_spend_usd":           {Known: true, Value: &zeroUSD, Unit: "USD", Provenance: empirical.ProvenanceMeasured, EvidenceRef: "meter"},
		"subscription_quota":      {Known: false, Unit: "provider_unit", Provenance: empirical.ProvenanceUnknown, EvidenceRef: "meter"},
		"local_compute_seconds":   {Known: true, Value: &zeroSec, Unit: "second", Provenance: empirical.ProvenanceMeasured, EvidenceRef: "meter"},
		"wall_seconds":            {Known: true, Value: &zeroSec, Unit: "second", Provenance: empirical.ProvenanceMeasured, EvidenceRef: "meter"},
		"repair_rounds":           {Known: false, Unit: "count", Provenance: empirical.ProvenanceUnknown, EvidenceRef: "meter"},
		"principal_reentries":     {Known: false, Unit: "count", Provenance: empirical.ProvenanceUnknown, EvidenceRef: "meter"},
	}

	sess := empirical.SessionEvidence{
		SchemaVersion:              "1.0",
		RunID:                      run.RunID,
		CampaignID:                 plan.CampaignID,
		AttemptID:                  run.RunID,
		TaskDigest:                 run.TaskDigest,
		Endpoint:                   ep,
		PromptDigest:               invProv.PromptDigest,
		ContextManifestDigest:      invProv.ContextManifestDigest,
		InvocationProvenanceDigest: invProvDigest,
		ExecutionPolicyDigest:      ep.PolicyDigest,
		AuthorizationDigest:        "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		StartedAt:                  now.Add(-30 * time.Minute).UTC(),
		FinishedAt:                 now.Add(-10 * time.Minute).UTC(),
		DriverOutcome:              empirical.StatusCompleted,
		Turns:                      1,
		Usage:                      sessUsage,
	}
	sessDigest, err := sess.Digest()
	if err != nil {
		t.Fatal(err)
	}
	sessBytes, err := protocol.CanonicalJSON(sess)
	if err != nil {
		t.Fatal(err)
	}

	verifierBasis := protocol.ActorBasis{
		EndpointID:    "devcadence-verifier",
		ModelID:       sourceCommit,
		ModelRevision: profileDigest,
	}
	verifierActorID, _ := actors.DeriveActorID("endpoint_model", verifierBasis)

	var cmdArgvs [][]string
	var cmdExitCodes []int
	var passedAcceptance []string
	defectsCaught := 0

	if class == "implementation" {
		for _, c := range profile.Tasks[0].Checks {
			cmdArgvs = append(cmdArgvs, c.Argv)
			cmdExitCodes = append(cmdExitCodes, 0)
		}
		passedAcceptance = profile.Tasks[0].AcceptanceCheckIDs
		defectsCaught = len(profile.Tasks[0].Defects)
	} else if class == "review" {
		defectsCaught = len(profile.Tasks[0].Defects)
	}

	receipt := verifier.VerifierReceipt{
		ReceiptVersion:            "1.0",
		CampaignID:                plan.CampaignID,
		RunID:                     run.RunID,
		TaskDigest:                run.TaskDigest,
		Seed:                      run.Seed,
		Strategy:                  run.Strategy,
		EndpointBindingDigest:     epDigest,
		PromptDigest:              invProv.PromptDigest,
		SessionEvidenceDigest:     sessDigest,
		CandidateCommit:           candCommit,
		CandidateArtifactDigest:   candDigest,
		SnapshotDigest:            "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		VerifierProducerID:        verifierActorID,
		VerifierSourceCommit:      sourceCommit,
		VerificationProfileDigest: profileDigest,
		CommandArgvArrays:         cmdArgvs,
		CommandExitCodes:          cmdExitCodes,
		PassedAcceptanceIDs:       passedAcceptance,
		SeededDefectTotal:         len(profile.Tasks[0].Defects),
		SeededDefectsCaught:       defectsCaught,
		QualityVerdict:            qualityVerdict,
	}
	rcptBytes, err := protocol.CanonicalJSON(receipt)
	if err != nil {
		t.Fatal(err)
	}
	rcptDigest := protocol.DigestBytes(rcptBytes)

	evidence := empirical.RunEvidence{
		RunID:                   run.RunID,
		TaskID:                  run.TaskID,
		TaskDigest:              run.TaskDigest,
		Seed:                    run.Seed,
		Strategy:                run.Strategy,
		Repetition:              run.Repetition,
		Endpoint:                run.Endpoint,
		Status:                  empirical.StatusCompleted,
		SnapshotRef:             "snap-ref",
		SnapshotDigest:          receipt.SnapshotDigest,
		SessionEvidenceRef:      "sess-ref",
		SessionEvidenceDigest:   sessDigest,
		PromptDigest:            invProv.PromptDigest,
		CandidateCommit:         candCommit,
		CandidateArtifactRef:    "cand-ref",
		CandidateArtifactDigest: candDigest,
		VerifierReceiptRef:      "rcpt-ref",
		VerifierReceiptDigest:   rcptDigest,
		InvocationProducerID:    workerActorID,
		VerifierProducerID:      verifierActorID,
		Accepted:                (qualityVerdict == empirical.QualityAccepted),
	}

	resolver := testArtifactResolver{
		"sess-ref":    sessBytes,
		"rcpt-ref":    rcptBytes,
		"cand-ref":    candBytes,
		invProvDigest: invProvBytes,
	}

	return plan, run, evidence, resolver
}

func TestVerifier_BuildInfoChecks(t *testing.T) {
	ctx := context.Background()
	_, baseCommit, candGoodCommit, _, wtManager, repoProvider, scratchDir := setupBaseTestDependencies(t)

	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	sourceCommit := "commit-verifier-v1"

	profile := verifier.VerificationProfile{
		Version:     "1.0",
		ProfileID:   "prof-buildinfo",
		Executables: []string{"git"},
		Tasks: []verifier.TaskVerification{
			{
				TaskID:             "task-01",
				TaskDigest:         "sha256:1111111111111111111111111111111111111111111111111111111111111111",
				Class:              "implementation",
				BaseCommit:         baseCommit,
				WriteScope:         []string{"src/"},
				Checks:             []verifier.CheckSpec{{CheckID: "chk-1", Argv: []string{"git", "status"}, TimeoutSeconds: 10}},
				AcceptanceCheckIDs: []string{"chk-1"},
			},
		},
	}

	plan, run, evidence, resolver := helperMakeValidFixtures(
		t, now, baseCommit, candGoodCommit, sourceCommit, profile, "implementation", empirical.QualityAccepted, []byte("cand"),
	)

	cases := []struct {
		name     string
		bSource  verifier.BuildInfoSource
		expError string
	}{
		{
			name:     "build info unknown (ok=false)",
			bSource:  mockBuildInfoSource{ok: false},
			expError: "build info unknown",
		},
		{
			name:     "modified source tree",
			bSource:  mockBuildInfoSource{rev: sourceCommit, modified: true, ok: true},
			expError: "source tree modified",
		},
		{
			name:     "source commit mismatch",
			bSource:  mockBuildInfoSource{rev: "different-commit", modified: false, ok: true},
			expError: "does not match plan",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := verifier.New(verifier.Options{
				Worktrees:    wtManager,
				Repositories: repoProvider,
				BuildInfo:    tc.bSource,
				ScratchDir:   scratchDir,
				Profile:      &profile,
			})
			if err != nil {
				t.Fatalf("New failed: %v", err)
			}

			_, err = v.Verify(ctx, plan, run, evidence, resolver)
			if err == nil || !strings.Contains(err.Error(), tc.expError) {
				t.Fatalf("expected error containing %q, got %v", tc.expError, err)
			}
		})
	}
}

func TestVerifier_ActorIndependenceCheck(t *testing.T) {
	ctx := context.Background()
	_, baseCommit, candGoodCommit, _, wtManager, repoProvider, scratchDir := setupBaseTestDependencies(t)

	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	sourceCommit := "commit-verifier-v1"

	profile := verifier.VerificationProfile{
		Version:     "1.0",
		ProfileID:   "prof-indep",
		Executables: []string{"git"},
		Tasks: []verifier.TaskVerification{
			{
				TaskID:             "task-01",
				TaskDigest:         "sha256:1111111111111111111111111111111111111111111111111111111111111111",
				Class:              "implementation",
				BaseCommit:         baseCommit,
				WriteScope:         []string{"src/"},
				Checks:             []verifier.CheckSpec{{CheckID: "chk-1", Argv: []string{"git", "status"}, TimeoutSeconds: 10}},
				AcceptanceCheckIDs: []string{"chk-1"},
			},
		},
	}

	bSource := mockBuildInfoSource{rev: sourceCommit, modified: false, ok: true}

	t.Run("worker actor ID equals verifier actor ID fails independence", func(t *testing.T) {
		plan, run, evidence, resolver := helperMakeValidFixtures(
			t, now, baseCommit, candGoodCommit, sourceCommit, profile, "implementation", empirical.QualityAccepted, []byte("cand"),
		)

		profileDigest, _ := profile.Digest()
		verifierBasis := protocol.ActorBasis{
			EndpointID:    "devcadence-verifier",
			ModelID:       sourceCommit,
			ModelRevision: profileDigest,
		}
		verifierActorID, _ := actors.DeriveActorID("endpoint_model", verifierBasis)

		// Set worker basis to be identical to verifier basis
		ep := run.Endpoint
		ep.EndpointID = verifierBasis.EndpointID
		ep.ModelID = verifierBasis.ModelID
		ep.ModelRevision = verifierBasis.ModelRevision
		epDigest, _ := protocol.Digest(ep)
		run.Endpoint = ep
		evidence.Endpoint = ep

		workerProv := protocol.ActorProvenance{
			ActorID:      verifierActorID,
			InvocationID: "inv-same-actor",
			Role:         protocol.ProvenanceRoleImplementer,
		}

		invProv := protocol.InvocationProvenance{
			SchemaVersion:         "1.0",
			ProvenanceID:          "prov-same",
			ProjectID:             "proj-01",
			TaskID:                run.TaskID,
			AttemptID:             run.RunID,
			WorkPackageID:         "ewp-01",
			Role:                  protocol.ProvenanceRoleImplementer,
			Actor:                 workerProv,
			Basis:                 verifierBasis,
			IndependenceBasis:     "endpoint_model",
			EndpointBindingDigest: epDigest,
			ContextManifestDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			PromptDigest:          evidence.PromptDigest,
			StartedAt:             now.Add(-time.Hour),
		}
		invProvBytes, _ := protocol.CanonicalJSON(invProv)
		invProvDigest := protocol.DigestBytes(invProvBytes)

		sessBytes := resolver["sess-ref"]
		var sess empirical.SessionEvidence
		_ = json.Unmarshal(sessBytes, &sess)
		sess.Endpoint = ep
		sess.InvocationProvenanceDigest = invProvDigest
		sessDigest, _ := sess.Digest()
		sessBytes, _ = protocol.CanonicalJSON(sess)

		resolver["sess-ref"] = sessBytes
		resolver[invProvDigest] = invProvBytes
		evidence.SessionEvidenceDigest = sessDigest

		rcptBytes := resolver["rcpt-ref"]
		var rcpt verifier.VerifierReceipt
		_ = json.Unmarshal(rcptBytes, &rcpt)
		rcpt.EndpointBindingDigest = epDigest
		rcpt.SessionEvidenceDigest = sessDigest
		rcptBytes, _ = protocol.CanonicalJSON(rcpt)
		resolver["rcpt-ref"] = rcptBytes
		evidence.VerifierReceiptDigest = protocol.DigestBytes(rcptBytes)

		v, err := verifier.New(verifier.Options{
			Worktrees:    wtManager,
			Repositories: repoProvider,
			BuildInfo:    bSource,
			ScratchDir:   scratchDir,
			Profile:      &profile,
		})
		if err != nil {
			t.Fatal(err)
		}

		_, err = v.Verify(ctx, plan, run, evidence, resolver)
		if err == nil || !strings.Contains(err.Error(), "VERIFIER_NOT_INDEPENDENT") {
			t.Fatalf("expected VERIFIER_NOT_INDEPENDENT error, got %v", err)
		}
	})

	t.Run("worker stored actor ID mismatch with re-derivation fails", func(t *testing.T) {
		plan, run, evidence, resolver := helperMakeValidFixtures(
			t, now, baseCommit, candGoodCommit, sourceCommit, profile, "implementation", empirical.QualityAccepted, []byte("cand"),
		)

		sessBytes := resolver["sess-ref"]
		var sess empirical.SessionEvidence
		_ = json.Unmarshal(sessBytes, &sess)

		invProvBytes := resolver[sess.InvocationProvenanceDigest]
		var invProv protocol.InvocationProvenance
		_ = json.Unmarshal(invProvBytes, &invProv)

		// Tamper with stored actor ID
		invProv.Actor.ActorID = "actor:tampered1234567890123"
		invProvBytes, _ = protocol.CanonicalJSON(invProv)
		invProvDigest := protocol.DigestBytes(invProvBytes)
		sess.InvocationProvenanceDigest = invProvDigest
		sessDigest, _ := sess.Digest()
		sessBytes, _ = protocol.CanonicalJSON(sess)

		resolver["sess-ref"] = sessBytes
		resolver[invProvDigest] = invProvBytes
		evidence.SessionEvidenceDigest = sessDigest

		rcptBytes := resolver["rcpt-ref"]
		var rcpt verifier.VerifierReceipt
		_ = json.Unmarshal(rcptBytes, &rcpt)
		rcpt.SessionEvidenceDigest = sessDigest
		rcptBytes, _ = protocol.CanonicalJSON(rcpt)
		resolver["rcpt-ref"] = rcptBytes
		evidence.VerifierReceiptDigest = protocol.DigestBytes(rcptBytes)

		v, err := verifier.New(verifier.Options{
			Worktrees:    wtManager,
			Repositories: repoProvider,
			BuildInfo:    bSource,
			ScratchDir:   scratchDir,
			Profile:      &profile,
		})
		if err != nil {
			t.Fatal(err)
		}

		_, err = v.Verify(ctx, plan, run, evidence, resolver)
		if err == nil || !strings.Contains(err.Error(), "differs from re-derived actor_id") {
			t.Fatalf("expected re-derivation mismatch error, got %v", err)
		}
	})
}

func TestVerifier_ReviewTaskExecution(t *testing.T) {
	ctx := context.Background()
	_, baseCommit, _, _, wtManager, repoProvider, scratchDir := setupBaseTestDependencies(t)

	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	clk := clock.NewFake(now, 0)
	idGen := ids.NewSequential()

	profile := verifier.VerificationProfile{
		Version:     "1.0",
		ProfileID:   "prof-review",
		Executables: []string{"git"},
		Tasks: []verifier.TaskVerification{
			{
				TaskID:     "task-rev-01",
				TaskDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
				Class:      "review",
				BaseCommit: baseCommit,
				Defects: []verifier.DefectProbe{
					{
						DefectID: "defect-rev-01",
						Anchor: &verifier.ReviewAnchor{
							Path:      "internal/auth/checker.go",
							StartLine: 10,
							EndLine:   25,
						},
					},
				},
			},
		},
	}

	sourceCommit := "commit-verifier-v1"
	bSource := mockBuildInfoSource{rev: sourceCommit, modified: false, ok: true}

	t.Run("finding cites defect anchor -> defect caught, quality accepted", func(t *testing.T) {
		reviewResult := protocol.ReviewResult{
			SchemaVersion: "1.0",
			ReviewID:      "rev-cand-01",
			ProjectID:     "proj-01",
			AttemptID:     "run-01",
			WorkPackageID: "ewp-01",
			Dimension:     protocol.DimensionCorrectness,
			Verdict:       protocol.VerdictPass,
			Findings: []protocol.Finding{
				{
					Severity:     protocol.SeverityHigh,
					Statement:    "Defect found in auth checker",
					EvidenceRefs: []string{"internal/auth/checker.go:15"},
				},
			},
			MustCompliance: []protocol.GuidanceCompliance{
				{GuidanceID: "g-01", Status: protocol.ComplianceSatisfied},
			},
		}
		candBytes, _ := protocol.CanonicalJSON(reviewResult)

		plan, run, evidence, resolver := helperMakeValidFixtures(
			t, now, baseCommit, baseCommit, sourceCommit, profile, "review", empirical.QualityAccepted, candBytes,
		)

		v, err := verifier.New(verifier.Options{
			Worktrees:    wtManager,
			Repositories: repoProvider,
			BuildInfo:    bSource,
			ScratchDir:   scratchDir,
			Profile:      &profile,
			Clock:        clk,
			IDs:          idGen,
		})
		if err != nil {
			t.Fatal(err)
		}

		outcome, err := v.Verify(ctx, plan, run, evidence, resolver)
		if err != nil {
			t.Fatalf("Verify failed: %v", err)
		}

		if outcome.QualityVerdict != empirical.QualityAccepted {
			t.Errorf("expected QualityAccepted, got %s", outcome.QualityVerdict)
		}
		if outcome.SeededDefectTotal != 1 || outcome.SeededDefectsCaught != 1 {
			t.Errorf("expected 1/1 defects caught, got %d/%d", outcome.SeededDefectsCaught, outcome.SeededDefectTotal)
		}
		if len(outcome.VerifiedCommandArtifactRefs) == 0 {
			t.Errorf("expected non-empty VerifiedCommandArtifactRefs")
		}
	})

	t.Run("finding misses defect anchor -> defect not caught", func(t *testing.T) {
		reviewResult := protocol.ReviewResult{
			SchemaVersion: "1.0",
			ReviewID:      "rev-cand-01",
			ProjectID:     "proj-01",
			AttemptID:     "run-01",
			WorkPackageID: "ewp-01",
			Dimension:     protocol.DimensionCorrectness,
			Verdict:       protocol.VerdictPass,
			Findings: []protocol.Finding{
				{
					Severity:     protocol.SeverityHigh,
					Statement:    "Finding at line 50",
					EvidenceRefs: []string{"internal/auth/checker.go:50"}, // line 50 outside [10, 25]
				},
			},
			MustCompliance: []protocol.GuidanceCompliance{
				{GuidanceID: "g-01", Status: protocol.ComplianceSatisfied},
			},
		}
		candBytes, _ := protocol.CanonicalJSON(reviewResult)

		plan, run, evidence, resolver := helperMakeValidFixtures(
			t, now, baseCommit, baseCommit, sourceCommit, profile, "review", empirical.QualityAccepted, candBytes,
		)

		// Adjust receipt SeededDefectsCaught to 0
		var rcpt verifier.VerifierReceipt
		_ = json.Unmarshal(resolver["rcpt-ref"], &rcpt)
		rcpt.SeededDefectsCaught = 0
		rcptBytes, _ := protocol.CanonicalJSON(rcpt)
		resolver["rcpt-ref"] = rcptBytes
		evidence.VerifierReceiptDigest = protocol.DigestBytes(rcptBytes)

		v, err := verifier.New(verifier.Options{
			Worktrees:    wtManager,
			Repositories: repoProvider,
			BuildInfo:    bSource,
			ScratchDir:   scratchDir,
			Profile:      &profile,
			Clock:        clk,
			IDs:          idGen,
		})
		if err != nil {
			t.Fatal(err)
		}

		outcome, err := v.Verify(ctx, plan, run, evidence, resolver)
		if err != nil {
			t.Fatalf("Verify failed: %v", err)
		}

		if outcome.QualityVerdict != empirical.QualityAccepted {
			t.Errorf("expected QualityAccepted, got %s", outcome.QualityVerdict)
		}
		if outcome.SeededDefectTotal != 1 || outcome.SeededDefectsCaught != 0 {
			t.Errorf("expected 0/1 defects caught, got %d/%d", outcome.SeededDefectsCaught, outcome.SeededDefectTotal)
		}
	})

	t.Run("invalid review candidate artifact yields QualityRejected", func(t *testing.T) {
		// Invalid JSON
		candBytes := []byte("invalid-json")

		plan, run, evidence, resolver := helperMakeValidFixtures(
			t, now, baseCommit, baseCommit, sourceCommit, profile, "review", empirical.QualityRejected, candBytes,
		)

		// Adjust receipt SeededDefectsCaught to 0 and QualityVerdict to rejected
		var rcpt verifier.VerifierReceipt
		_ = json.Unmarshal(resolver["rcpt-ref"], &rcpt)
		rcpt.SeededDefectsCaught = 0
		rcpt.QualityVerdict = empirical.QualityRejected
		rcptBytes, _ := protocol.CanonicalJSON(rcpt)
		resolver["rcpt-ref"] = rcptBytes
		evidence.VerifierReceiptDigest = protocol.DigestBytes(rcptBytes)
		evidence.Accepted = false

		v, err := verifier.New(verifier.Options{
			Worktrees:    wtManager,
			Repositories: repoProvider,
			BuildInfo:    bSource,
			ScratchDir:   scratchDir,
			Profile:      &profile,
			Clock:        clk,
			IDs:          idGen,
		})
		if err != nil {
			t.Fatal(err)
		}

		outcome, err := v.Verify(ctx, plan, run, evidence, resolver)
		if err != nil {
			t.Fatalf("Verify failed: %v", err)
		}

		if outcome.QualityVerdict != empirical.QualityRejected {
			t.Errorf("expected QualityRejected for invalid review candidate, got %s", outcome.QualityVerdict)
		}
	})
}

func TestVerifier_ImplementationTaskExecution(t *testing.T) {
	ctx := context.Background()
	_, baseCommit, candGoodCommit, candBadCommit, wtManager, repoProvider, scratchDir := setupBaseTestDependencies(t)

	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	clk := clock.NewFake(now, 0)
	idGen := ids.NewSequential()

	profile := verifier.VerificationProfile{
		Version:     "1.0",
		ProfileID:   "prof-impl",
		Executables: []string{"git"},
		Tasks: []verifier.TaskVerification{
			{
				TaskID:     "task-impl-01",
				TaskDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
				Class:      "implementation",
				BaseCommit: baseCommit,
				WriteScope: []string{"src/"},
				Checks: []verifier.CheckSpec{
					{
						CheckID:        "chk-01",
						Argv:           []string{"git", "status"},
						Dir:            ".",
						TimeoutSeconds: 10,
						ExpectExitCode: 0,
					},
					{
						CheckID:        "chk-probe-01",
						Argv:           []string{"git", "rev-parse", "HEAD"},
						Dir:            "",
						TimeoutSeconds: 10,
						ExpectExitCode: 0,
					},
				},
				AcceptanceCheckIDs: []string{"chk-01"},
				Defects: []verifier.DefectProbe{
					{
						DefectID:      "defect-probe-01",
						CatchCheckIDs: []string{"chk-probe-01"},
					},
				},
			},
		},
	}

	sourceCommit := "commit-verifier-v1"
	bSource := mockBuildInfoSource{rev: sourceCommit, modified: false, ok: true}

	t.Run("valid execution with good candidate commit", func(t *testing.T) {
		candBytes := []byte("candidate-content")
		plan, run, evidence, resolver := helperMakeValidFixtures(
			t, now, baseCommit, candGoodCommit, sourceCommit, profile, "implementation", empirical.QualityAccepted, candBytes,
		)

		v, err := verifier.New(verifier.Options{
			Worktrees:    wtManager,
			Repositories: repoProvider,
			BuildInfo:    bSource,
			ScratchDir:   scratchDir,
			Profile:      &profile,
			Clock:        clk,
			IDs:          idGen,
		})
		if err != nil {
			t.Fatal(err)
		}

		outcome, err := v.Verify(ctx, plan, run, evidence, resolver)
		if err != nil {
			t.Fatalf("Verify failed: %v", err)
		}

		if outcome.QualityVerdict != empirical.QualityAccepted {
			t.Errorf("expected quality accepted, got %s", outcome.QualityVerdict)
		}
		if outcome.SeededDefectsCaught != 1 {
			t.Errorf("expected 1 defect caught, got %d", outcome.SeededDefectsCaught)
		}
		if len(outcome.VerifiedCommandArtifactRefs) != 2 {
			t.Errorf("expected 2 command artifacts, got %d", len(outcome.VerifiedCommandArtifactRefs))
		}
	})

	t.Run("write scope violation yields quality rejected", func(t *testing.T) {
		candBytes := []byte("candidate-content")
		plan, run, evidence, resolver := helperMakeValidFixtures(
			t, now, baseCommit, candBadCommit, sourceCommit, profile, "implementation", empirical.QualityRejected, candBytes,
		)

		v, err := verifier.New(verifier.Options{
			Worktrees:    wtManager,
			Repositories: repoProvider,
			BuildInfo:    bSource,
			ScratchDir:   scratchDir,
			Profile:      &profile,
			Clock:        clk,
			IDs:          idGen,
		})
		if err != nil {
			t.Fatal(err)
		}

		outcome, err := v.Verify(ctx, plan, run, evidence, resolver)
		if err != nil {
			t.Fatalf("Verify failed: %v", err)
		}
		if outcome.QualityVerdict != empirical.QualityRejected {
			t.Errorf("expected QualityRejected due to write scope violation, got %s", outcome.QualityVerdict)
		}
	})

	t.Run("inconsistent verification between claimed receipt and rerun fails", func(t *testing.T) {
		candBytes := []byte("candidate-content")
		// Claimed receipt says QualityAccepted, but candidate violates write scope -> rerun will be QualityRejected!
		plan, run, evidence, resolver := helperMakeValidFixtures(
			t, now, baseCommit, candBadCommit, sourceCommit, profile, "implementation", empirical.QualityAccepted, candBytes,
		)

		v, err := verifier.New(verifier.Options{
			Worktrees:    wtManager,
			Repositories: repoProvider,
			BuildInfo:    bSource,
			ScratchDir:   scratchDir,
			Profile:      &profile,
			Clock:        clk,
			IDs:          idGen,
		})
		if err != nil {
			t.Fatal(err)
		}

		_, err = v.Verify(ctx, plan, run, evidence, resolver)
		if err == nil || !strings.Contains(err.Error(), "INCONSISTENT_VERIFICATION") {
			t.Fatalf("expected INCONSISTENT_VERIFICATION error, got %v", err)
		}
	})
}

func TestVerifier_ConstructorOptions(t *testing.T) {
	_, _, _, _, wtManager, repoProvider, scratchDir := setupBaseTestDependencies(t)

	// Worktrees nil
	if _, err := verifier.New(verifier.Options{Repositories: repoProvider}); err == nil {
		t.Errorf("expected error when Worktrees is nil")
	}

	// Repositories nil
	if _, err := verifier.New(verifier.Options{Worktrees: wtManager}); err == nil {
		t.Errorf("expected error when Repositories is nil")
	}

	// Defaults populated
	v, err := verifier.New(verifier.Options{
		Worktrees:    wtManager,
		Repositories: repoProvider,
		ScratchDir:   "",
	})
	if err != nil {
		t.Fatalf("unexpected error with default options: %v", err)
	}
	if v == nil {
		t.Fatal("expected non-nil verifier")
	}

	// ScratchDir creation failure
	filePath := filepath.Join(scratchDir, "existing-file")
	if err := os.WriteFile(filePath, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	badScratch := filepath.Join(filePath, "child")
	if _, err := verifier.New(verifier.Options{
		Worktrees:    wtManager,
		Repositories: repoProvider,
		ScratchDir:   badScratch,
	}); err == nil {
		t.Errorf("expected error when ScratchDir cannot be created")
	}
}

func TestDefaultBuildInfoSource(t *testing.T) {
	b := verifier.DefaultBuildInfoSource()
	if b == nil {
		t.Fatal("expected non-nil default build info source")
	}
	_, _, _ = b.VCSRevision()
}

func TestVerifier_PreconditionErrors(t *testing.T) {
	ctx := context.Background()
	_, baseCommit, candGoodCommit, _, wtManager, repoProvider, scratchDir := setupBaseTestDependencies(t)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	sourceCommit := "commit-verifier-v1"

	profile := verifier.VerificationProfile{
		Version:     "1.0",
		ProfileID:   "prof-precond",
		Executables: []string{"git"},
		Tasks: []verifier.TaskVerification{
			{
				TaskID:             "task-01",
				TaskDigest:         "sha256:1111111111111111111111111111111111111111111111111111111111111111",
				Class:              "implementation",
				BaseCommit:         baseCommit,
				WriteScope:         []string{"src/"},
				Checks:             []verifier.CheckSpec{{CheckID: "chk-1", Argv: []string{"git", "status"}, TimeoutSeconds: 10}},
				AcceptanceCheckIDs: []string{"chk-1"},
			},
		},
	}
	bSource := mockBuildInfoSource{rev: sourceCommit, modified: false, ok: true}
	v, err := verifier.New(verifier.Options{
		Worktrees:    wtManager,
		Repositories: repoProvider,
		BuildInfo:    bSource,
		ScratchDir:   scratchDir,
		Profile:      &profile,
	})
	if err != nil {
		t.Fatal(err)
	}

	plan, run, evidence, resolver := helperMakeValidFixtures(
		t, now, baseCommit, candGoodCommit, sourceCommit, profile, "implementation", empirical.QualityAccepted, []byte("cand"),
	)

	// Context cancelled
	cancCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := v.Verify(cancCtx, plan, run, evidence, resolver); err == nil {
		t.Errorf("expected error for cancelled context")
	}

	// Status not completed
	evStatus := evidence
	evStatus.Status = empirical.StatusFailed
	if _, err := v.Verify(ctx, plan, run, evStatus, resolver); err == nil {
		t.Errorf("expected error for non-completed evidence status")
	}

	// Run ID mismatch
	evRunID := evidence
	evRunID.RunID = "different-run"
	if _, err := v.Verify(ctx, plan, run, evRunID, resolver); err == nil {
		t.Errorf("expected error for run ID mismatch")
	}
}

func TestVerifier_ArtifactResolutionAndValidationErrors(t *testing.T) {
	ctx := context.Background()
	_, baseCommit, candGoodCommit, _, wtManager, repoProvider, scratchDir := setupBaseTestDependencies(t)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	sourceCommit := "commit-verifier-v1"

	profile := verifier.VerificationProfile{
		Version:     "1.0",
		ProfileID:   "prof-artifacts",
		Executables: []string{"git"},
		Tasks: []verifier.TaskVerification{
			{
				TaskID:             "task-01",
				TaskDigest:         "sha256:1111111111111111111111111111111111111111111111111111111111111111",
				Class:              "implementation",
				BaseCommit:         baseCommit,
				WriteScope:         []string{"src/"},
				Checks:             []verifier.CheckSpec{{CheckID: "chk-1", Argv: []string{"git", "status"}, TimeoutSeconds: 10}},
				AcceptanceCheckIDs: []string{"chk-1"},
			},
		},
	}
	bSource := mockBuildInfoSource{rev: sourceCommit, modified: false, ok: true}

	plan, run, evidence, resolver := helperMakeValidFixtures(
		t, now, baseCommit, candGoodCommit, sourceCommit, profile, "implementation", empirical.QualityAccepted, []byte("cand"),
	)

	// Resolver nil and v.opts.Resolver nil
	vNoRes, _ := verifier.New(verifier.Options{
		Worktrees:    wtManager,
		Repositories: repoProvider,
		BuildInfo:    bSource,
		ScratchDir:   scratchDir,
		Profile:      &profile,
	})
	if _, err := vNoRes.Verify(ctx, plan, run, evidence, nil); err == nil {
		t.Errorf("expected error when no resolver provided")
	}

	// Resolver fallback to v.opts.Resolver
	vWithRes, _ := verifier.New(verifier.Options{
		Worktrees:    wtManager,
		Repositories: repoProvider,
		BuildInfo:    bSource,
		ScratchDir:   scratchDir,
		Profile:      &profile,
		Resolver:     resolver,
	})
	if _, err := vWithRes.Verify(ctx, plan, run, evidence, nil); err != nil {
		t.Errorf("unexpected error with opts.Resolver fallback: %v", err)
	}

	v, _ := verifier.New(verifier.Options{
		Worktrees:    wtManager,
		Repositories: repoProvider,
		BuildInfo:    bSource,
		ScratchDir:   scratchDir,
		Profile:      &profile,
	})

	// Missing receipt
	badRes1 := make(testArtifactResolver)
	for k, v := range resolver {
		badRes1[k] = v
	}
	delete(badRes1, evidence.VerifierReceiptRef)
	if _, err := v.Verify(ctx, plan, run, evidence, badRes1); err == nil {
		t.Errorf("expected error when receipt is missing")
	}

	// Invalid receipt JSON
	badRes2 := make(testArtifactResolver)
	for k, v := range resolver {
		badRes2[k] = v
	}
	badRes2[evidence.VerifierReceiptRef] = []byte("bad json")
	evidence2 := evidence
	evidence2.VerifierReceiptDigest = protocol.DigestBytes([]byte("bad json"))
	if _, err := v.Verify(ctx, plan, run, evidence2, badRes2); err == nil {
		t.Errorf("expected error for invalid receipt json")
	}

	// Trailing tokens in receipt
	badRes3 := make(testArtifactResolver)
	for k, v := range resolver {
		badRes3[k] = v
	}
	withTrailing := append(resolver[evidence.VerifierReceiptRef], []byte(" trailing")...)
	badRes3[evidence.VerifierReceiptRef] = withTrailing
	evidence3 := evidence
	evidence3.VerifierReceiptDigest = protocol.DigestBytes(withTrailing)
	if _, err := v.Verify(ctx, plan, run, evidence3, badRes3); err == nil {
		t.Errorf("expected error for trailing tokens in receipt")
	}

	// Missing session evidence
	badRes4 := make(testArtifactResolver)
	for k, v := range resolver {
		badRes4[k] = v
	}
	delete(badRes4, evidence.SessionEvidenceRef)
	if _, err := v.Verify(ctx, plan, run, evidence, badRes4); err == nil {
		t.Errorf("expected error when session evidence is missing")
	}

	// Invalid session evidence JSON
	badRes5 := make(testArtifactResolver)
	for k, v := range resolver {
		badRes5[k] = v
	}
	badRes5[evidence.SessionEvidenceRef] = []byte("bad json")
	evidence5 := evidence
	evidence5.SessionEvidenceDigest = protocol.DigestBytes([]byte("bad json"))
	if _, err := v.Verify(ctx, plan, run, evidence5, badRes5); err == nil {
		t.Errorf("expected error for invalid session evidence json")
	}

	// Session evidence digest mismatch
	badRes6 := make(testArtifactResolver)
	for k, v := range resolver {
		badRes6[k] = v
	}
	evidence6 := evidence
	evidence6.SessionEvidenceDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := v.Verify(ctx, plan, run, evidence6, badRes6); err == nil {
		t.Errorf("expected error for session evidence digest mismatch")
	}

	// Missing invocation provenance
	badRes7 := make(testArtifactResolver)
	for k, v := range resolver {
		badRes7[k] = v
	}
	var sess empirical.SessionEvidence
	_ = json.Unmarshal(resolver[evidence.SessionEvidenceRef], &sess)
	delete(badRes7, sess.InvocationProvenanceDigest)
	if _, err := v.Verify(ctx, plan, run, evidence, badRes7); err == nil {
		t.Errorf("expected error when invocation provenance is missing")
	}

	// Invalid invocation provenance JSON
	badRes8 := make(testArtifactResolver)
	for k, v := range resolver {
		badRes8[k] = v
	}
	badRes8[sess.InvocationProvenanceDigest] = []byte("bad json")
	if _, err := v.Verify(ctx, plan, run, evidence, badRes8); err == nil {
		t.Errorf("expected error for invalid invocation provenance json")
	}

	// Missing candidate artifact
	badRes9 := make(testArtifactResolver)
	for k, v := range resolver {
		badRes9[k] = v
	}
	delete(badRes9, evidence.CandidateArtifactRef)
	if _, err := v.Verify(ctx, plan, run, evidence, badRes9); err == nil {
		t.Errorf("expected error when candidate artifact is missing")
	}

	// Empty artifact ref
	evEmptyRef := evidence
	evEmptyRef.VerifierReceiptRef = "  "
	if _, err := v.Verify(ctx, plan, run, evEmptyRef, resolver); err == nil {
		t.Errorf("expected error for empty artifact ref")
	}

	// Empty artifact digest
	evEmptyDig := evidence
	evEmptyDig.VerifierReceiptDigest = "  "
	if _, err := v.Verify(ctx, plan, run, evEmptyDig, resolver); err == nil {
		t.Errorf("expected error for empty artifact digest")
	}

	// Candidate artifact digest mismatch
	evCandMismatch := evidence
	evCandMismatch.CandidateArtifactDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := v.Verify(ctx, plan, run, evCandMismatch, resolver); err == nil {
		t.Errorf("expected error for candidate artifact digest mismatch")
	}
}

func TestVerifier_ReceiptAndSessionCrossCheckErrors(t *testing.T) {
	ctx := context.Background()
	_, baseCommit, candGoodCommit, _, wtManager, repoProvider, scratchDir := setupBaseTestDependencies(t)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	sourceCommit := "commit-verifier-v1"

	profile := verifier.VerificationProfile{
		Version:     "1.0",
		ProfileID:   "prof-crosschecks",
		Executables: []string{"git"},
		Tasks: []verifier.TaskVerification{
			{
				TaskID:             "task-01",
				TaskDigest:         "sha256:1111111111111111111111111111111111111111111111111111111111111111",
				Class:              "implementation",
				BaseCommit:         baseCommit,
				WriteScope:         []string{"src/"},
				Checks:             []verifier.CheckSpec{{CheckID: "chk-1", Argv: []string{"git", "status"}, TimeoutSeconds: 10}},
				AcceptanceCheckIDs: []string{"chk-1"},
			},
		},
	}
	bSource := mockBuildInfoSource{rev: sourceCommit, modified: false, ok: true}
	v, _ := verifier.New(verifier.Options{
		Worktrees:    wtManager,
		Repositories: repoProvider,
		BuildInfo:    bSource,
		ScratchDir:   scratchDir,
		Profile:      &profile,
	})

	mutateReceipt := func(mutate func(r *verifier.VerifierReceipt)) (empirical.CampaignPlan, empirical.PlannedRun, empirical.RunEvidence, testArtifactResolver) {
		plan, run, evidence, res := helperMakeValidFixtures(
			t, now, baseCommit, candGoodCommit, sourceCommit, profile, "implementation", empirical.QualityAccepted, []byte("cand"),
		)
		var rcpt verifier.VerifierReceipt
		_ = json.Unmarshal(res[evidence.VerifierReceiptRef], &rcpt)
		mutate(&rcpt)
		rcptBytes, _ := protocol.CanonicalJSON(rcpt)
		res[evidence.VerifierReceiptRef] = rcptBytes
		evidence.VerifierReceiptDigest = protocol.DigestBytes(rcptBytes)
		return plan, run, evidence, res
	}

	// 1. Receipt version != 1.0
	{
		plan, run, evidence, res := mutateReceipt(func(r *verifier.VerifierReceipt) { r.ReceiptVersion = "2.0" })
		if _, err := v.Verify(ctx, plan, run, evidence, res); err == nil {
			t.Errorf("expected error for receipt_version != 1.0")
		}
	}

	// 2. Receipt campaign ID mismatch
	{
		plan, run, evidence, res := mutateReceipt(func(r *verifier.VerifierReceipt) { r.CampaignID = "diff-camp" })
		if _, err := v.Verify(ctx, plan, run, evidence, res); err == nil {
			t.Errorf("expected error for receipt campaign ID mismatch")
		}
	}

	// 3. Receipt prompt digest mismatch
	{
		plan, run, evidence, res := mutateReceipt(func(r *verifier.VerifierReceipt) { r.PromptDigest = "sha256:diff" })
		if _, err := v.Verify(ctx, plan, run, evidence, res); err == nil {
			t.Errorf("expected error for receipt prompt digest mismatch")
		}
	}

	// 4. Receipt candidate commit mismatch
	{
		plan, run, evidence, res := mutateReceipt(func(r *verifier.VerifierReceipt) { r.CandidateCommit = "diff-commit" })
		if _, err := v.Verify(ctx, plan, run, evidence, res); err == nil {
			t.Errorf("expected error for receipt candidate commit mismatch")
		}
	}

	// 5. Receipt snapshot digest mismatch
	{
		plan, run, evidence, res := mutateReceipt(func(r *verifier.VerifierReceipt) { r.SnapshotDigest = "sha256:diff-snap" })
		if _, err := v.Verify(ctx, plan, run, evidence, res); err == nil {
			t.Errorf("expected error for receipt snapshot digest mismatch")
		}
	}

	// 6. Receipt verifier producer id mismatch
	{
		plan, run, evidence, res := mutateReceipt(func(r *verifier.VerifierReceipt) { r.VerifierProducerID = "actor:diff" })
		if _, err := v.Verify(ctx, plan, run, evidence, res); err == nil {
			t.Errorf("expected error for verifier producer id mismatch")
		}
	}

	// 7. Receipt verifier source commit mismatch
	{
		plan, run, evidence, res := mutateReceipt(func(r *verifier.VerifierReceipt) { r.VerifierSourceCommit = "diff-src" })
		if _, err := v.Verify(ctx, plan, run, evidence, res); err == nil {
			t.Errorf("expected error for verifier source commit mismatch")
		}
	}

	// 8. Receipt verification profile digest mismatch
	{
		plan, run, evidence, res := mutateReceipt(func(r *verifier.VerifierReceipt) { r.VerificationProfileDigest = "sha256:diff-prof" })
		if _, err := v.Verify(ctx, plan, run, evidence, res); err == nil {
			t.Errorf("expected error for verification profile digest mismatch")
		}
	}

	// 9. Receipt endpoint binding digest mismatch
	{
		plan, run, evidence, res := mutateReceipt(func(r *verifier.VerifierReceipt) { r.EndpointBindingDigest = "sha256:diff-ep" })
		if _, err := v.Verify(ctx, plan, run, evidence, res); err == nil {
			t.Errorf("expected error for endpoint binding digest mismatch")
		}
	}

	// 10. Receipt session evidence digest mismatch
	{
		plan, run, evidence, res := mutateReceipt(func(r *verifier.VerifierReceipt) { r.SessionEvidenceDigest = "sha256:diff-sess" })
		if _, err := v.Verify(ctx, plan, run, evidence, res); err == nil {
			t.Errorf("expected error for session evidence digest mismatch")
		}
	}

	// 11. Session RunID mismatch with planned run
	{
		plan, run, evidence, res := helperMakeValidFixtures(
			t, now, baseCommit, candGoodCommit, sourceCommit, profile, "implementation", empirical.QualityAccepted, []byte("cand"),
		)
		var sess empirical.SessionEvidence
		_ = json.Unmarshal(res[evidence.SessionEvidenceRef], &sess)
		sess.RunID = "diff-run-id"
		sessBytes, _ := protocol.CanonicalJSON(sess)
		sessDigest := protocol.DigestBytes(sessBytes)
		res[evidence.SessionEvidenceRef] = sessBytes
		evidence.SessionEvidenceDigest = sessDigest

		var rcpt verifier.VerifierReceipt
		_ = json.Unmarshal(res[evidence.VerifierReceiptRef], &rcpt)
		rcpt.SessionEvidenceDigest = sessDigest
		rcptBytes, _ := protocol.CanonicalJSON(rcpt)
		res[evidence.VerifierReceiptRef] = rcptBytes
		evidence.VerifierReceiptDigest = protocol.DigestBytes(rcptBytes)

		if _, err := v.Verify(ctx, plan, run, evidence, res); err == nil {
			t.Errorf("expected error for session runID mismatch")
		}
	}
}

func TestVerifier_ProfileResolutionFromResolver(t *testing.T) {
	ctx := context.Background()
	_, baseCommit, candGoodCommit, _, wtManager, repoProvider, scratchDir := setupBaseTestDependencies(t)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	sourceCommit := "commit-verifier-v1"

	profile := verifier.VerificationProfile{
		Version:     "1.0",
		ProfileID:   "prof-res-check",
		Executables: []string{"git"},
		Tasks: []verifier.TaskVerification{
			{
				TaskID:             "task-01",
				TaskDigest:         "sha256:1111111111111111111111111111111111111111111111111111111111111111",
				Class:              "implementation",
				BaseCommit:         baseCommit,
				WriteScope:         []string{"src/"},
				Checks:             []verifier.CheckSpec{{CheckID: "chk-1", Argv: []string{"git", "status"}, TimeoutSeconds: 10}},
				AcceptanceCheckIDs: []string{"chk-1"},
			},
		},
	}
	bSource := mockBuildInfoSource{rev: sourceCommit, modified: false, ok: true}

	plan, run, evidence, resolver := helperMakeValidFixtures(
		t, now, baseCommit, candGoodCommit, sourceCommit, profile, "implementation", empirical.QualityAccepted, []byte("cand"),
	)

	profBytes, err := protocol.CanonicalJSON(profile)
	if err != nil {
		t.Fatal(err)
	}
	resolver[plan.VerificationProfileDigest] = profBytes

	v, err := verifier.New(verifier.Options{
		Worktrees:    wtManager,
		Repositories: repoProvider,
		BuildInfo:    bSource,
		ScratchDir:   scratchDir,
		Profile:      nil, // nil profile triggers artifact fetch
	})
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := v.Verify(ctx, plan, run, evidence, resolver)
	if err != nil {
		t.Fatalf("unexpected error resolving profile from resolver: %v", err)
	}
	if outcome.QualityVerdict != empirical.QualityAccepted {
		t.Errorf("expected quality accepted, got %s", outcome.QualityVerdict)
	}

	// Profile missing from resolver
	delete(resolver, plan.VerificationProfileDigest)
	if _, err := v.Verify(ctx, plan, run, evidence, resolver); err == nil {
		t.Errorf("expected error when profile is missing from resolver")
	}
}

func TestVerifier_WorkerProvenanceAndTaskErrors(t *testing.T) {
	ctx := context.Background()
	_, baseCommit, candGoodCommit, _, wtManager, repoProvider, scratchDir := setupBaseTestDependencies(t)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	sourceCommit := "commit-verifier-v1"

	profile := verifier.VerificationProfile{
		Version:     "1.0",
		ProfileID:   "prof-prov-task",
		Executables: []string{"git"},
		Tasks: []verifier.TaskVerification{
			{
				TaskID:             "task-01",
				TaskDigest:         "sha256:1111111111111111111111111111111111111111111111111111111111111111",
				Class:              "implementation",
				BaseCommit:         baseCommit,
				WriteScope:         []string{"src/"},
				Checks:             []verifier.CheckSpec{{CheckID: "chk-1", Argv: []string{"git", "status"}, TimeoutSeconds: 10}},
				AcceptanceCheckIDs: []string{"chk-1"},
			},
		},
	}
	bSource := mockBuildInfoSource{rev: sourceCommit, modified: false, ok: true}
	v, _ := verifier.New(verifier.Options{
		Worktrees:    wtManager,
		Repositories: repoProvider,
		BuildInfo:    bSource,
		ScratchDir:   scratchDir,
		Profile:      &profile,
	})

	mutateProv := func(mutate func(p *protocol.InvocationProvenance)) (empirical.CampaignPlan, empirical.PlannedRun, empirical.RunEvidence, testArtifactResolver) {
		plan, run, evidence, res := helperMakeValidFixtures(
			t, now, baseCommit, candGoodCommit, sourceCommit, profile, "implementation", empirical.QualityAccepted, []byte("cand"),
		)
		var sess empirical.SessionEvidence
		_ = json.Unmarshal(res[evidence.SessionEvidenceRef], &sess)
		var prov protocol.InvocationProvenance
		_ = json.Unmarshal(res[sess.InvocationProvenanceDigest], &prov)
		mutate(&prov)
		provBytes, _ := protocol.CanonicalJSON(prov)
		provDigest := protocol.DigestBytes(provBytes)
		res[provDigest] = provBytes
		sess.InvocationProvenanceDigest = provDigest
		sessDigest, _ := sess.Digest()
		sessBytes, _ := protocol.CanonicalJSON(sess)
		res[evidence.SessionEvidenceRef] = sessBytes
		evidence.SessionEvidenceDigest = sessDigest

		var rcpt verifier.VerifierReceipt
		_ = json.Unmarshal(res[evidence.VerifierReceiptRef], &rcpt)
		rcpt.SessionEvidenceDigest = sessDigest
		rcptBytes, _ := protocol.CanonicalJSON(rcpt)
		res[evidence.VerifierReceiptRef] = rcptBytes
		evidence.VerifierReceiptDigest = protocol.DigestBytes(rcptBytes)
		return plan, run, evidence, res
	}

	// 1. Worker provenance role is not implementer or reviewer
	{
		plan, run, evidence, res := mutateProv(func(p *protocol.InvocationProvenance) {
			p.Role = protocol.ProvenanceRole("observer")
		})
		if _, err := v.Verify(ctx, plan, run, evidence, res); err == nil {
			t.Errorf("expected error for invalid worker provenance role")
		}
	}

	// 2. Worker provenance task_id mismatch
	{
		plan, run, evidence, res := mutateProv(func(p *protocol.InvocationProvenance) {
			p.TaskID = "diff-task"
		})
		if _, err := v.Verify(ctx, plan, run, evidence, res); err == nil {
			t.Errorf("expected error for worker provenance task_id mismatch")
		}
	}

	// 3. Worker provenance attempt_id mismatch
	{
		plan, run, evidence, res := mutateProv(func(p *protocol.InvocationProvenance) {
			p.AttemptID = "diff-attempt"
		})
		if _, err := v.Verify(ctx, plan, run, evidence, res); err == nil {
			t.Errorf("expected error for worker provenance attempt_id mismatch")
		}
	}

	// 3b. Worker provenance basis mismatch with session evidence endpoint
	{
		plan, run, evidence, res := mutateProv(func(p *protocol.InvocationProvenance) {
			p.Basis.EndpointID = "different-endpoint"
		})
		if _, err := v.Verify(ctx, plan, run, evidence, res); err == nil {
			t.Errorf("expected error for worker provenance basis mismatch")
		}
	}

	// 4. Task not found in profile
	{
		plan, run, evidence, res := helperMakeValidFixtures(
			t, now, baseCommit, candGoodCommit, sourceCommit, profile, "implementation", empirical.QualityAccepted, []byte("cand"),
		)
		emptyProfile := profile
		emptyProfile.Tasks = nil
		vEmpty, _ := verifier.New(verifier.Options{
			Worktrees:    wtManager,
			Repositories: repoProvider,
			BuildInfo:    bSource,
			ScratchDir:   scratchDir,
			Profile:      &emptyProfile,
		})
		if _, err := vEmpty.Verify(ctx, plan, run, evidence, res); err == nil {
			t.Errorf("expected error when task not found in profile")
		}
	}
}

func TestVerifier_ExecutionAndCheckFailures(t *testing.T) {
	ctx := context.Background()
	repo, baseCommit, candGoodCommit, _, wtManager, repoProvider, scratchDir := setupBaseTestDependencies(t)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	sourceCommit := "commit-verifier-v1"
	bSource := mockBuildInfoSource{rev: sourceCommit, modified: false, ok: true}

	// 1. Check with non-root Dir ("src")
	{
		profile := verifier.VerificationProfile{
			Version:     "1.0",
			ProfileID:   "prof-dir",
			Executables: []string{"git"},
			Tasks: []verifier.TaskVerification{
				{
					TaskID:             "task-01",
					TaskDigest:         "sha256:1111111111111111111111111111111111111111111111111111111111111111",
					Class:              "implementation",
					BaseCommit:         baseCommit,
					WriteScope:         []string{"src/"},
					Checks:             []verifier.CheckSpec{{CheckID: "chk-1", Argv: []string{"git", "status"}, Dir: "src", TimeoutSeconds: 10}},
					AcceptanceCheckIDs: []string{"chk-1"},
				},
			},
		}
		v, _ := verifier.New(verifier.Options{
			Worktrees:    wtManager,
			Repositories: repoProvider,
			BuildInfo:    bSource,
			ScratchDir:   scratchDir,
			Profile:      &profile,
		})
		plan, run, evidence, resolver := helperMakeValidFixtures(
			t, now, baseCommit, candGoodCommit, sourceCommit, profile, "implementation", empirical.QualityAccepted, []byte("cand"),
		)
		outcome, err := v.Verify(ctx, plan, run, evidence, resolver)
		if err != nil {
			t.Fatalf("unexpected error with check Dir: %v", err)
		}
		if outcome.QualityVerdict != empirical.QualityAccepted {
			t.Errorf("expected quality accepted, got %s", outcome.QualityVerdict)
		}
	}

	// 2. Acceptance check failure -> QualityRejected
	{
		profile := verifier.VerificationProfile{
			Version:     "1.0",
			ProfileID:   "prof-fail",
			Executables: []string{"git"},
			Tasks: []verifier.TaskVerification{
				{
					TaskID:     "task-01",
					TaskDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
					Class:      "implementation",
					BaseCommit: baseCommit,
					WriteScope: []string{"src/"},
					Checks: []verifier.CheckSpec{
						{CheckID: "chk-1", Argv: []string{"git", "checkout", "non-existent-branch"}, TimeoutSeconds: 10, ExpectExitCode: 0},
					},
					AcceptanceCheckIDs: []string{"chk-1"},
				},
			},
		}
		v, _ := verifier.New(verifier.Options{
			Worktrees:    wtManager,
			Repositories: repoProvider,
			BuildInfo:    bSource,
			ScratchDir:   scratchDir,
			Profile:      &profile,
		})
		plan, run, evidence, resolver := helperMakeValidFixtures(
			t, now, baseCommit, candGoodCommit, sourceCommit, profile, "implementation", empirical.QualityRejected, []byte("cand"),
		)
		var rcpt verifier.VerifierReceipt
		_ = json.Unmarshal(resolver[evidence.VerifierReceiptRef], &rcpt)
		rcpt.CommandExitCodes = []int{1}
		rcpt.PassedAcceptanceIDs = []string{}
		rcpt.QualityVerdict = empirical.QualityRejected
		rcptBytes, _ := protocol.CanonicalJSON(rcpt)
		resolver[evidence.VerifierReceiptRef] = rcptBytes
		evidence.VerifierReceiptDigest = protocol.DigestBytes(rcptBytes)
		evidence.Accepted = false

		outcome, err := v.Verify(ctx, plan, run, evidence, resolver)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome.QualityVerdict != empirical.QualityRejected {
			t.Errorf("expected quality rejected, got %s", outcome.QualityVerdict)
		}
	}

	// 3. Command timeout
	{
		profile := verifier.VerificationProfile{
			Version:     "1.0",
			ProfileID:   "prof-timeout",
			Executables: []string{"sleep"},
			Tasks: []verifier.TaskVerification{
				{
					TaskID:             "task-01",
					TaskDigest:         "sha256:1111111111111111111111111111111111111111111111111111111111111111",
					Class:              "implementation",
					BaseCommit:         baseCommit,
					WriteScope:         []string{"src/"},
					Checks:             []verifier.CheckSpec{{CheckID: "chk-1", Argv: []string{"sleep", "5"}, TimeoutSeconds: 1}},
					AcceptanceCheckIDs: []string{"chk-1"},
				},
			},
		}
		v, _ := verifier.New(verifier.Options{
			Worktrees:    wtManager,
			Repositories: repoProvider,
			BuildInfo:    bSource,
			ScratchDir:   scratchDir,
			Profile:      &profile,
		})
		plan, run, evidence, resolver := helperMakeValidFixtures(
			t, now, baseCommit, candGoodCommit, sourceCommit, profile, "implementation", empirical.QualityAccepted, []byte("cand"),
		)
		_, err := v.Verify(ctx, plan, run, evidence, resolver)
		if err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("expected timeout error, got %v", err)
		}
	}

	// 4. Command cannot start (executable does not exist on PATH)
	{
		profile := verifier.VerificationProfile{
			Version:     "1.0",
			ProfileID:   "prof-nostart",
			Executables: []string{"nonexistentbinary123"},
			Tasks: []verifier.TaskVerification{
				{
					TaskID:             "task-01",
					TaskDigest:         "sha256:1111111111111111111111111111111111111111111111111111111111111111",
					Class:              "implementation",
					BaseCommit:         baseCommit,
					WriteScope:         []string{"src/"},
					Checks:             []verifier.CheckSpec{{CheckID: "chk-1", Argv: []string{"nonexistentbinary123"}, TimeoutSeconds: 10}},
					AcceptanceCheckIDs: []string{"chk-1"},
				},
			},
		}
		v, _ := verifier.New(verifier.Options{
			Worktrees:    wtManager,
			Repositories: repoProvider,
			BuildInfo:    bSource,
			ScratchDir:   scratchDir,
			Profile:      &profile,
		})
		plan, run, evidence, resolver := helperMakeValidFixtures(
			t, now, baseCommit, candGoodCommit, sourceCommit, profile, "implementation", empirical.QualityAccepted, []byte("cand"),
		)
		_, err := v.Verify(ctx, plan, run, evidence, resolver)
		if err == nil || !strings.Contains(err.Error(), "cannot start executable") {
			t.Fatalf("expected cannot start error, got %v", err)
		}
	}

	// 5. Candidate commit not found in repository
	{
		profile := verifier.VerificationProfile{
			Version:     "1.0",
			ProfileID:   "prof-nocommit",
			Executables: []string{"git"},
			Tasks: []verifier.TaskVerification{
				{
					TaskID:             "task-01",
					TaskDigest:         "sha256:1111111111111111111111111111111111111111111111111111111111111111",
					Class:              "implementation",
					BaseCommit:         baseCommit,
					WriteScope:         []string{"src/"},
					Checks:             []verifier.CheckSpec{{CheckID: "chk-1", Argv: []string{"git", "status"}, TimeoutSeconds: 10}},
					AcceptanceCheckIDs: []string{"chk-1"},
				},
			},
		}
		v, _ := verifier.New(verifier.Options{
			Worktrees:    wtManager,
			Repositories: repoProvider,
			BuildInfo:    bSource,
			ScratchDir:   scratchDir,
			Profile:      &profile,
		})
		plan, run, evidence, resolver := helperMakeValidFixtures(
			t, now, baseCommit, "0123456789abcdef0123456789abcdef01234567", sourceCommit, profile, "implementation", empirical.QualityAccepted, []byte("cand"),
		)
		_, err := v.Verify(ctx, plan, run, evidence, resolver)
		if err == nil || !strings.Contains(err.Error(), "candidate commit") {
			t.Fatalf("expected candidate commit not found error, got %v", err)
		}
	}

	// 6. Project repository not found
	{
		profile := verifier.VerificationProfile{
			Version:     "1.0",
			ProfileID:   "prof-norepo",
			Executables: []string{"git"},
			Tasks: []verifier.TaskVerification{
				{
					TaskID:             "task-01",
					TaskDigest:         "sha256:1111111111111111111111111111111111111111111111111111111111111111",
					Class:              "implementation",
					BaseCommit:         baseCommit,
					WriteScope:         []string{"src/"},
					Checks:             []verifier.CheckSpec{{CheckID: "chk-1", Argv: []string{"git", "status"}, TimeoutSeconds: 10}},
					AcceptanceCheckIDs: []string{"chk-1"},
				},
			},
		}
		emptyRepoProvider := execrt.NewSingleRepositoryProvider(repo)
		vNoRepo, _ := verifier.New(verifier.Options{
			Worktrees:    wtManager,
			Repositories: emptyRepoProvider,
			BuildInfo:    bSource,
			ScratchDir:   scratchDir,
			Profile:      &profile,
			ProjectID:    "nonexistent-project",
		})
		plan, run, evidence, resolver := helperMakeValidFixtures(
			t, now, baseCommit, candGoodCommit, sourceCommit, profile, "implementation", empirical.QualityAccepted, []byte("cand"),
		)
		_, err := vNoRepo.Verify(ctx, plan, run, evidence, resolver)
		if err == nil || !strings.Contains(err.Error(), "repository unavailable") {
			t.Fatalf("expected repository unavailable error, got %v", err)
		}
	}
}

func TestVerifier_DeterministicRerunMismatches(t *testing.T) {
	ctx := context.Background()
	_, baseCommit, candGoodCommit, _, wtManager, repoProvider, scratchDir := setupBaseTestDependencies(t)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	sourceCommit := "commit-verifier-v1"

	profile := verifier.VerificationProfile{
		Version:     "1.0",
		ProfileID:   "prof-rerun",
		Executables: []string{"git"},
		Tasks: []verifier.TaskVerification{
			{
				TaskID:             "task-01",
				TaskDigest:         "sha256:1111111111111111111111111111111111111111111111111111111111111111",
				Class:              "implementation",
				BaseCommit:         baseCommit,
				WriteScope:         []string{"src/"},
				Checks:             []verifier.CheckSpec{{CheckID: "chk-1", Argv: []string{"git", "status"}, TimeoutSeconds: 10}},
				AcceptanceCheckIDs: []string{"chk-1"},
				Defects: []verifier.DefectProbe{
					{DefectID: "def-1", CatchCheckIDs: []string{"chk-1"}},
				},
			},
		},
	}
	bSource := mockBuildInfoSource{rev: sourceCommit, modified: false, ok: true}
	v, _ := verifier.New(verifier.Options{
		Worktrees:    wtManager,
		Repositories: repoProvider,
		BuildInfo:    bSource,
		ScratchDir:   scratchDir,
		Profile:      &profile,
	})

	mutateRcpt := func(mutate func(r *verifier.VerifierReceipt)) (empirical.CampaignPlan, empirical.PlannedRun, empirical.RunEvidence, testArtifactResolver) {
		plan, run, evidence, res := helperMakeValidFixtures(
			t, now, baseCommit, candGoodCommit, sourceCommit, profile, "implementation", empirical.QualityAccepted, []byte("cand"),
		)
		var rcpt verifier.VerifierReceipt
		_ = json.Unmarshal(res[evidence.VerifierReceiptRef], &rcpt)
		mutate(&rcpt)
		rcptBytes, _ := protocol.CanonicalJSON(rcpt)
		res[evidence.VerifierReceiptRef] = rcptBytes
		evidence.VerifierReceiptDigest = protocol.DigestBytes(rcptBytes)
		return plan, run, evidence, res
	}

	// 1. passed acceptance IDs count mismatch
	{
		plan, run, evidence, res := mutateRcpt(func(r *verifier.VerifierReceipt) {
			r.PassedAcceptanceIDs = []string{} // rerun has 1
		})
		if _, err := v.Verify(ctx, plan, run, evidence, res); err == nil || !strings.Contains(err.Error(), "passed acceptance IDs") {
			t.Fatalf("expected passed acceptance IDs mismatch error, got %v", err)
		}
	}

	// 1b. passed acceptance IDs element mismatch
	{
		plan, run, evidence, res := mutateRcpt(func(r *verifier.VerifierReceipt) {
			r.PassedAcceptanceIDs = []string{"other-check-id"}
		})
		if _, err := v.Verify(ctx, plan, run, evidence, res); err == nil || !strings.Contains(err.Error(), "passed acceptance IDs mismatch") {
			t.Fatalf("expected passed acceptance IDs element mismatch error, got %v", err)
		}
	}

	// 2. command exit codes count mismatch
	{
		plan, run, evidence, res := mutateRcpt(func(r *verifier.VerifierReceipt) {
			r.CommandExitCodes = []int{} // rerun has 1
		})
		if _, err := v.Verify(ctx, plan, run, evidence, res); err == nil || !strings.Contains(err.Error(), "command exit codes") {
			t.Fatalf("expected command exit codes mismatch error, got %v", err)
		}
	}

	// 3. command exit code value mismatch
	{
		plan, run, evidence, res := mutateRcpt(func(r *verifier.VerifierReceipt) {
			r.CommandExitCodes = []int{99} // rerun has 0
		})
		if _, err := v.Verify(ctx, plan, run, evidence, res); err == nil || !strings.Contains(err.Error(), "command exit codes mismatch") {
			t.Fatalf("expected command exit codes value mismatch error, got %v", err)
		}
	}

	// 4. seeded defects caught mismatch
	{
		plan, run, evidence, res := mutateRcpt(func(r *verifier.VerifierReceipt) {
			r.SeededDefectsCaught = 0 // rerun has 1
		})
		if _, err := v.Verify(ctx, plan, run, evidence, res); err == nil || !strings.Contains(err.Error(), "seeded defects caught mismatch") {
			t.Fatalf("expected seeded defects caught mismatch error, got %v", err)
		}
	}

	// 5. seeded defect total mismatch
	{
		plan, run, evidence, res := mutateRcpt(func(r *verifier.VerifierReceipt) {
			r.SeededDefectTotal = 99 // rerun has 1
		})
		if _, err := v.Verify(ctx, plan, run, evidence, res); err == nil || !strings.Contains(err.Error(), "seeded defect total mismatch") {
			t.Fatalf("expected seeded defect total mismatch error, got %v", err)
		}
	}
}

func TestVerifier_WriteScopeMatching(t *testing.T) {
	ctx := context.Background()
	repo, baseCommit, _, _, wtManager, repoProvider, scratchDir := setupBaseTestDependencies(t)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	sourceCommit := "commit-verifier-v1"
	bSource := mockBuildInfoSource{rev: sourceCommit, modified: false, ok: true}

	// Create commit that touches exact file "pkg/file.go"
	cmd := exec.Command("git", "checkout", "-b", "exact-file-branch", baseCommit)
	cmd.Dir = repo.Path
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo.Path, "pkg"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo.Path, "pkg", "file.go"), []byte("package pkg"), 0600); err != nil {
		t.Fatal(err)
	}
	cmdAdd := exec.Command("git", "add", "pkg/file.go")
	cmdAdd.Dir = repo.Path
	if err := cmdAdd.Run(); err != nil {
		t.Fatal(err)
	}
	cmdCommit := exec.Command("git", "commit", "-m", "exact file commit")
	cmdCommit.Dir = repo.Path
	cmdCommit.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if err := cmdCommit.Run(); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", repo.Path, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	exactCommit := strings.TrimSpace(string(out))

	profile := verifier.VerificationProfile{
		Version:     "1.0",
		ProfileID:   "prof-exact-scope",
		Executables: []string{"git"},
		Tasks: []verifier.TaskVerification{
			{
				TaskID:             "task-01",
				TaskDigest:         "sha256:1111111111111111111111111111111111111111111111111111111111111111",
				Class:              "implementation",
				BaseCommit:         baseCommit,
				WriteScope:         []string{"pkg/file.go"},
				Checks:             []verifier.CheckSpec{{CheckID: "chk-1", Argv: []string{"git", "status"}, TimeoutSeconds: 10}},
				AcceptanceCheckIDs: []string{"chk-1"},
			},
		},
	}

	v, _ := verifier.New(verifier.Options{
		Worktrees:    wtManager,
		Repositories: repoProvider,
		BuildInfo:    bSource,
		ScratchDir:   scratchDir,
		Profile:      &profile,
	})

	plan, run, evidence, resolver := helperMakeValidFixtures(
		t, now, baseCommit, exactCommit, sourceCommit, profile, "implementation", empirical.QualityAccepted, []byte("cand"),
	)

	outcome, err := v.Verify(ctx, plan, run, evidence, resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.QualityVerdict != empirical.QualityAccepted {
		t.Errorf("expected quality accepted for exact write scope match, got %s", outcome.QualityVerdict)
	}
}
