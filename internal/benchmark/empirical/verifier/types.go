package verifier

import (
	"context"

	"github.com/olostan/DevCadence/internal/benchmark/empirical"
	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/execrt"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/worktrees"
)

// CheckSpec describes one deterministic check command executed inside an isolated worktree.
type CheckSpec struct {
	CheckID        string   `json:"check_id"`
	Argv           []string `json:"argv"`            // Argv[0] is in the profile allow-list, never a shell
	Dir            string   `json:"dir"`             // relative to worktree root, no ".."
	TimeoutSeconds int      `json:"timeout_seconds"` // 1..600
	ExpectExitCode int      `json:"expect_exit_code"`
}

// ReviewAnchor defines an exact source file location for review-task defect anchoring.
type ReviewAnchor struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

// DefectProbe specifies how an injected seeded defect is detected by verification.
type DefectProbe struct {
	DefectID      string        `json:"defect_id"`
	CatchCheckIDs []string      `json:"catch_check_ids,omitempty"` // implementation tasks: caught iff all pass
	Anchor        *ReviewAnchor `json:"anchor,omitempty"`          // review tasks: caught iff a finding cites path:line in anchor
}

// TaskVerification specifies verification constraints and checks for one benchmark task.
type TaskVerification struct {
	TaskID             string        `json:"task_id"`
	TaskDigest         string        `json:"task_digest"`
	Class              string        `json:"class"` // "implementation" | "review"
	BaseCommit         string        `json:"base_commit"`
	WriteScope         []string      `json:"write_scope,omitempty"` // repo-relative paths or dir prefixes ending "/"
	Checks             []CheckSpec   `json:"checks"`
	AcceptanceCheckIDs []string      `json:"acceptance_check_ids,omitempty"`
	Defects            []DefectProbe `json:"defects,omitempty"`
}

// VerificationProfile is the immutable document pinning verification rules for all tasks.
type VerificationProfile struct {
	Version     string             `json:"version"` // "1.0"
	ProfileID   string             `json:"profile_id"`
	Executables []string           `json:"executables"` // allow-list of executable names
	Tasks       []TaskVerification `json:"tasks"`       // sorted, unique TaskID
}

// BuildInfoSource abstracts reading build VCS identity from the running binary.
type BuildInfoSource interface {
	VCSRevision() (revision string, modified bool, ok bool)
}

// VerifierReceipt represents the strict schema of an original verifier receipt artifact.
type VerifierReceipt struct {
	ReceiptVersion            string     `json:"receipt_version"`
	CampaignID                string     `json:"campaign_id"`
	RunID                     string     `json:"run_id"`
	TaskDigest                string     `json:"task_digest"`
	Seed                      string     `json:"seed"`
	Strategy                  string     `json:"strategy"`
	EndpointBindingDigest     string     `json:"endpoint_binding_digest"`
	PromptDigest              string     `json:"prompt_digest"`
	SessionEvidenceDigest     string     `json:"session_evidence_digest"`
	CandidateCommit           string     `json:"candidate_commit"`
	CandidateArtifactDigest   string     `json:"candidate_artifact_digest"`
	SnapshotDigest            string     `json:"snapshot_digest"`
	VerifierProducerID        string     `json:"verifier_producer_id"`
	VerifierSourceCommit      string     `json:"verifier_source_commit"`
	VerificationProfileDigest string     `json:"verification_profile_digest"`
	CommandArgvArrays         [][]string `json:"command_argv_arrays"`
	CommandExitCodes          []int      `json:"command_exit_codes"`
	OutputArtifactRefs        []string   `json:"output_artifact_refs,omitempty"`
	OutputArtifactDigests     []string   `json:"output_artifact_digests,omitempty"`
	PassedAcceptanceIDs       []string   `json:"passed_acceptance_ids"`
	SeededDefectTotal         int        `json:"seeded_defect_total"`
	SeededDefectsCaught       int        `json:"seeded_defects_caught"`
	QualityVerdict            string     `json:"quality_verdict"`
	AccountingEvidenceRefs    []string   `json:"accounting_evidence_refs,omitempty"`
	AccountingEvidenceDigests []string   `json:"accounting_evidence_digests,omitempty"`
}

// CommandRunner abstracts execution of subprocesses, matching process.Runner.
type CommandRunner interface {
	Run(ctx context.Context, spec process.Spec) (process.Result, error)
}

// Options configures the IndependentVerifier.
type Options struct {
	// PermitUnconfinedHostChecks is only for explicitly trusted/test-owned
	// candidates. The default denies execution because process.Runner is not
	// an OS-level filesystem/network sandbox.
	PermitUnconfinedHostChecks bool
	Resolver     empirical.ArtifactResolver
	Worktrees    *worktrees.Manager
	Repositories execrt.RepositoryProvider
	Runner       CommandRunner
	BuildInfo    BuildInfoSource
	Clock        clock.Clock
	IDs          ids.Source
	ScratchDir   string
	Profile      *VerificationProfile
	ProjectID    string
}

// VerifierOptions is an alias for Options for ergonomics.
type VerifierOptions = Options

// RunVerification is an alias for empirical.VerifiedOutcome for ergonomics.
type RunVerification = empirical.VerifiedOutcome

// LimitationUnconfinedHostProcess declares that independent verifier checks execute as host processes without an OS sandbox boundary.
const LimitationUnconfinedHostProcess = "unconfined_host_process"
