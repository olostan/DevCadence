package taskexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/olostan/DevCadence/internal/artifacts"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/validation"
)

// Post-check: after the model claims it is done, and before a candidate commit
// is materialized, the project's validation profile runs in the candidate
// worktree. A failure is fed back to the same model session (bounded) so it
// can repair; the attempt never produces a candidate while the check fails.
const (
	// HardMaxRepairRounds is the upper bound for Options.MaxRepairRounds.
	HardMaxRepairRounds = 5

	validationReportSchema = "devcadence.validation_report/v1"
	reviewStatusSchema     = "devcadence.review_status/v1"
	feedbackOutputTail     = 4 * 1024
	postCheckCaptureBytes  = 4 << 20
	validationReportKind   = "validation-report"
	reviewStatusKind       = "review-status"

	// ReviewUnavailable labels candidates that no independent reviewer examined.
	ReviewUnavailable       = "review_unavailable"
	reviewUnavailableReason = "no independent reviewer configured; owner manual acceptance required"
)

// ErrProfileNotConfigured is returned by a ProfileSource when the project has no
// post-check profile at all; the executor then runs without a post-check.
var ErrProfileNotConfigured = errors.New("taskexec: no post-check profile configured")

var failingTestRE = regexp.MustCompile(`(?m)^\s*--- FAIL: (\S+)`)

type checkReport struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Argv        []string `json:"argv"`
	Status      string   `json:"status"`
	ExitCode    *int     `json:"exit_code,omitempty"`
	DurationMS  int64    `json:"duration_ms"`
	OutputBytes int64    `json:"output_bytes"`
	Truncated   bool     `json:"output_truncated,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	StdoutID    string   `json:"stdout_artifact,omitempty"`
	StderrID    string   `json:"stderr_artifact,omitempty"`
}

type roundReport struct {
	Round      int           `json:"round"`
	Outcome    string        `json:"outcome"`
	Passed     bool          `json:"passed"`
	DurationMS int64         `json:"duration_ms"`
	Checks     []checkReport `json:"checks"`
}

type validationReport struct {
	Schema           string        `json:"schema"`
	ProfileID        string        `json:"profile_id"`
	ProfileDigest    string        `json:"profile_digest"`
	ExecutionMode    ExecutionMode `json:"execution_mode"`
	UnsafeUnconfined bool          `json:"unsafe_unconfined"`
	MaxRepairRounds  int           `json:"max_repair_rounds"`
	RoundsUsed       int           `json:"rounds_used"`
	Passed           bool          `json:"passed"`
	Rounds           []roundReport `json:"rounds"`
}

// postCheck is the per-attempt post-check state.
type postCheck struct {
	profileID string
	profile   validation.Profile
	digest    string
	maxRepair int

	mu     sync.Mutex
	rounds []roundReport
	passed bool
}

// preparePostCheck loads and vets the post-check profile synchronously, before
// any attempt exists or the model is called. It returns nil when no post-check
// is configured. Profile commands come from the (untrusted) repository, so they
// run only in unsafe_unconfined_local mode; strict mode refuses here, before any
// subprocess, rather than silently skipping a configured check.
func (e *Executor) preparePostCheck(ctx context.Context) (*postCheck, error) {
	if e.opts.PostCheckProfileID == "" || e.opts.Profiles == nil {
		return nil, nil
	}
	profile, err := e.opts.Profiles.Profile(ctx, e.opts.ProjectID, e.opts.PostCheckProfileID)
	if errors.Is(err, ErrProfileNotConfigured) {
		return nil, nil
	}
	if err != nil {
		return nil, principal.NewCodedError(principal.CodeInvalidArgument, false, []string{"postcheck_profile_invalid"},
			fmt.Sprintf("post-check profile %q could not be loaded: %v", e.opts.PostCheckProfileID, err))
	}
	if err := validateProfileSafety(profile); err != nil {
		return nil, err
	}
	if e.opts.ExecutionMode.normalized() != ExecutionUnsafeUnconfinedLocal {
		return nil, principal.NewCodedError(principal.CodePolicyDenied, false, []string{"postcheck_requires_yolo"},
			fmt.Sprintf("the project defines post-check profile %q whose commands come from the repository and run unconfined; "+
				"no command was run. Enable \"execution_mode\":\"yolo\" in $DEVCADENCE_HOME/config/selfhost.json (owner-local, NOT a sandbox) "+
				"or remove the project's validation profile", e.opts.PostCheckProfileID))
	}
	b, err := protocol.CanonicalJSON(profile)
	if err != nil {
		return nil, err
	}
	return &postCheck{
		profileID: e.opts.PostCheckProfileID, profile: profile,
		digest: protocol.DigestBytes(b), maxRepair: e.opts.MaxRepairRounds,
	}, nil
}

// run executes one post-check round and returns whether it passed plus, when
// it failed, the concise feedback message for the model.
func (e *Executor) runPostCheck(ctx context.Context, pc *postCheck, worktree, attemptID string) (passed bool, feedback string, started bool, err error) {
	sweepPatchTemps(worktree)
	scratch := filepath.Join(e.opts.StateDir, "attempts", attemptID, "home")
	env, err := hostCommandEnv.build(ctx, e.opts.Runner, worktree, scratch)
	if err != nil {
		return false, "", false, err
	}
	absState, err := filepath.Abs(e.opts.StateDir)
	if err != nil {
		return false, "", false, err
	}
	store, err := artifacts.NewStore(filepath.Join(absState, "artifacts"), e.opts.IDs)
	if err != nil {
		return false, "", false, err
	}

	pc.mu.Lock()
	round := len(pc.rounds) + 1
	pc.mu.Unlock()

	outputs := map[string]process.Result{}
	t0 := time.Now()
	checks, outcome, err := validation.RunProfile(ctx, pc.profile, validation.RunOptions{
		Dir: worktree, Env: env, Runner: e.opts.Runner, Artifacts: store, ProjectID: e.opts.ProjectID,
		MaxOutputBytes: postCheckCaptureBytes, RunID: fmt.Sprintf("%s-pc%d", attemptID, round),
		OnCheckOutput: func(id string, res process.Result) { outputs[id] = res },
	})
	if err != nil {
		return false, "", true, errs.Wrap(errs.CategoryInternal, err, "post-check run failed")
	}
	rr := roundReport{
		Round: round, Outcome: string(outcome), Passed: outcome == protocol.ValidationPass,
		DurationMS: time.Since(t0).Milliseconds(),
	}
	var firstBad *protocol.CheckResult
	for i := range checks {
		c := checks[i]
		cr := checkReport{
			ID: c.ID, Kind: c.Kind, Argv: c.Command, Status: string(c.Status), ExitCode: c.ExitCode,
			DurationMS: c.FinishedAt.Time().Sub(c.StartedAt.Time()).Milliseconds(), Truncated: c.OutputTruncated,
		}
		if c.Summary != nil {
			cr.Summary = *c.Summary
		}
		if c.StdoutArtifact != nil {
			cr.StdoutID = *c.StdoutArtifact
		}
		if c.StderrArtifact != nil {
			cr.StderrID = *c.StderrArtifact
		}
		if res, ok := outputs[c.ID]; ok {
			cr.OutputBytes = int64(len(res.Stdout) + len(res.Stderr))
		}
		rr.Checks = append(rr.Checks, cr)
		if c.Status != protocol.CheckPass && firstBad == nil {
			firstBad = &checks[i]
		}
	}
	pc.mu.Lock()
	pc.rounds = append(pc.rounds, rr)
	pc.passed = rr.Passed
	pc.mu.Unlock()
	if rr.Passed {
		return true, "", true, nil
	}
	return false, buildFeedback(round, pc.maxRepair, outcome, checks, firstBad, outputs), true, nil
}

// buildFeedback is the bounded failure message the model sees. Verbose logs stay
// in artifacts; only the failing check, exit code, the first failing test name
// and the last feedbackOutputTail bytes of output are sent.
func buildFeedback(round, maxRepair int, outcome protocol.ValidationOutcome, checks []protocol.CheckResult, bad *protocol.CheckResult, outputs map[string]process.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Automated validation FAILED (outcome %s; check run %d, %d repair round(s) allowed in total).\n", outcome, round, maxRepair)
	if bad == nil {
		b.WriteString("No individual check was reported as failing; validation did not complete.\n")
	} else {
		fmt.Fprintf(&b, "failing check: %s\ncommand: %s\n", bad.ID, strings.Join(bad.Command, " "))
		if bad.ExitCode != nil {
			fmt.Fprintf(&b, "exit code: %d\n", *bad.ExitCode)
		}
		if bad.Summary != nil {
			fmt.Fprintf(&b, "summary: %s\n", *bad.Summary)
		}
		res := outputs[bad.ID]
		out := strings.ToValidUTF8(string(res.Stdout)+string(res.Stderr), "?")
		if m := failingTestRE.FindStringSubmatch(out); m != nil {
			fmt.Fprintf(&b, "first failing test: %s\n", m[1])
		}
		tail := out
		if len(tail) > feedbackOutputTail {
			tail = "..." + tail[len(tail)-feedbackOutputTail:]
		}
		if strings.TrimSpace(tail) != "" {
			fmt.Fprintf(&b, "output (last %d bytes of %d):\n%s\n", min(len(out), feedbackOutputTail), len(out), tail)
		}
	}
	var others []string
	for _, c := range checks {
		if c.Status != protocol.CheckPass && (bad == nil || c.ID != bad.ID) {
			others = append(others, c.ID)
		}
	}
	if len(others) > 0 {
		fmt.Fprintf(&b, "other non-passing checks: %s\n", strings.Join(others, ", "))
	}
	b.WriteString("Fix the code with your tools (do not edit the validation profile), then reply without tool calls when finished.")
	return b.String()
}

func (pc *postCheck) reportJSON(mode ExecutionMode) ([]byte, bool, error) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	if len(pc.rounds) == 0 {
		return nil, false, nil
	}
	b, err := json.Marshal(validationReport{
		Schema: validationReportSchema, ProfileID: pc.profileID, ProfileDigest: pc.digest,
		ExecutionMode: mode, UnsafeUnconfined: true, MaxRepairRounds: pc.maxRepair,
		RoundsUsed: len(pc.rounds), Passed: pc.passed, Rounds: append([]roundReport(nil), pc.rounds...),
	})
	return b, true, err
}

// postCheckRefs stores the attempt's validation report (when any round ran).
func (e *Executor) postCheckRefs(ctx context.Context, attemptID string) ([]protocol.ArtifactRef, error) {
	v, ok := e.postChecks.Load(attemptID)
	if !ok {
		return nil, nil
	}
	b, have, err := v.(*postCheck).reportJSON(e.opts.ExecutionMode.normalized())
	if err != nil || !have {
		return nil, err
	}
	ref, err := e.opts.Artifacts.Put(ctx, e.opts.ProjectID, validationReportKind, "application/json", b)
	if err != nil {
		return nil, err
	}
	return []protocol.ArtifactRef{ref}, nil
}

// reviewStatusRef stores the honest hand-off label: nobody independent reviewed
// this candidate.
func (e *Executor) reviewStatusRef(ctx context.Context) (protocol.ArtifactRef, error) {
	b, err := json.Marshal(map[string]any{
		"schema": reviewStatusSchema, "review": ReviewUnavailable, "reason": reviewUnavailableReason,
		"independent": false, "owner_acceptance_required": true,
	})
	if err != nil {
		return protocol.ArtifactRef{}, err
	}
	return e.opts.Artifacts.Put(ctx, e.opts.ProjectID, reviewStatusKind, "application/json", b)
}

// hasUncertainEffects reports whether a failure summary carries the exact
// token effects=uncertain (token match, not a substring).
func hasUncertainEffects(summary string) bool {
	for _, tok := range strings.Fields(summary) {
		if tok == "effects=uncertain" {
			return true
		}
	}
	return false
}
