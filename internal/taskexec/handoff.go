package taskexec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/events"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/principal/facade"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/storage"
	"github.com/olostan/DevCadence/internal/tasks"
)

// Candidate handoff (SH1-4C). The candidate commit is made durable and
// reachable from the primary repository by a ref under candidateRefPrefix,
// created with `git update-ref`: no branch of the primary repository moves, its
// HEAD and working tree are untouched, and nothing is pushed or merged. The
// handoff packet is assembled on demand from durable records (attempt,
// artifacts, events) and the repository, so it is reproducible after restart.
const (
	candidateRefPrefix = "refs/devcadence/candidates/"
	maxHandoffArtifact = 1 << 20
	maxManifestEntries = 256

	acceptanceNote = "Acceptance is a manual owner action: DevCadence has not accepted, merged or pushed this candidate. " +
		"No independent reviewer examined it (review_unavailable). Inspect the diff with the commands below, then integrate it yourself."
)

var _ facade.CandidateInspector = (*Executor)(nil)

// CandidateRefName is the durable ref pinning an attempt's candidate commit.
func CandidateRefName(taskID, attemptID string) string {
	return candidateRefPrefix + taskID + "-" + attemptID
}

// pinCandidateRef creates the durable candidate ref. It never overwrites an
// existing ref that points elsewhere (attempt ids are unique).
func (e *Executor) pinCandidateRef(ctx context.Context, dir, taskID, attemptID, commit string) error {
	// An all-zero old value makes update-ref create-only.
	_, err := e.runGit(ctx, dir, "update-ref", CandidateRefName(taskID, attemptID), commit, strings.Repeat("0", len(commit)))
	return err
}

// InspectCandidate implements facade.CandidateInspector. The facade has already
// verified lineage; the executor re-verifies that the attempt names the commit.
func (e *Executor) InspectCandidate(ctx context.Context, c principal.CandidateRef) (*facade.CandidateHandoff, error) {
	taskList, err := e.opts.ControlPlane.Tasks(ctx, storage.TaskFilter{ProjectID: e.opts.ProjectID})
	if err != nil {
		return nil, err
	}
	alias := ""
	for _, t := range taskList {
		if t.ID == c.TaskID {
			alias = t.Alias
		}
	}
	if alias == "" {
		return nil, errs.New(errs.CategoryNotFound, "task %q not found", c.TaskID)
	}
	detail, err := e.opts.ControlPlane.TaskDetail(ctx, e.opts.ProjectID, alias)
	if err != nil {
		return nil, err
	}
	var att *tasks.Attempt
	for _, a := range detail.Attempts {
		if a.ID == c.AttemptID {
			att = a
		}
	}
	if att == nil || att.CandidateCommit == "" || att.CandidateCommit != c.Commit {
		return nil, errs.New(errs.CategoryNotFound, "attempt %s has no candidate %s", c.AttemptID, c.Commit)
	}
	repo, err := e.opts.Repositories.Repository(ctx, e.opts.ProjectID)
	if err != nil {
		return nil, err
	}
	base := att.BaseCommit
	if base == "" {
		base = c.WorkPackage.BaseCommit
	}
	refName := CandidateRefName(detail.Task.ID, att.ID)
	h := &facade.CandidateHandoff{
		TaskID: detail.Task.ID, AttemptID: att.ID, BaseCommit: base, CandidateCommit: att.CandidateCommit,
		Ref: refName, Branch: fmt.Sprintf("devcadence/%s/%s", detail.Task.ID, att.ID),
		ExecutionMode: string(e.opts.ExecutionMode.normalized()),
		Review:        facade.HandoffReview{Status: ReviewUnavailable, Independent: false, Reason: reviewUnavailableReason},
		Validations:   []facade.HandoffValidation{}, ChangedFiles: []facade.ChangedFile{},
		Acceptance: acceptanceNote,
	}
	h.Model = parseModelIdentity(att.ModelIdentity)
	if wt, werr := e.opts.Worktrees.Get(e.opts.ProjectID, detail.Task.ID+"/"+att.ID); werr == nil && wt != nil {
		h.WorktreePath = wt.Path
		h.Branch = wt.Branch
	}

	// The ref must exist and name the candidate commit; otherwise the packet
	// would promise a durable handle that is not there.
	got, gerr := e.runGit(ctx, repo.Path, "rev-parse", "--verify", "--quiet", refName+"^{commit}")
	if gerr != nil || strings.TrimSpace(string(got.Stdout)) != att.CandidateCommit {
		return nil, errs.New(errs.CategoryIntegrity, "candidate ref %s is missing or does not name %s", refName, att.CandidateCommit)
	}

	names, err := e.runGit(ctx, repo.Path, "diff", "--name-status", "-z", base+".."+att.CandidateCommit)
	if err != nil {
		return nil, err
	}
	h.ChangedFiles = parseNameStatusZ(names.Stdout)

	for _, a := range att.Artifacts {
		switch a.Kind {
		case validationReportKind:
			var rep validationReport
			if e.readArtifactJSON(a, &rep) == nil {
				h.PostCheck = summarizeReport(rep)
				h.ExecutionMode = string(rep.ExecutionMode)
			}
		case "command-trace":
			var tr commandTrace
			if e.readArtifactJSON(a, &tr) == nil {
				s := &facade.HandoffCommands{Commands: len(tr.Commands), UnsafeUnconfined: tr.UnsafeUnconfined}
				for _, r := range tr.Commands {
					if r.Refused != "" {
						s.Refused++
					}
				}
				h.CommandTrace = s
			}
		case reviewStatusKind:
			var rs struct {
				Review      string `json:"review"`
				Independent bool   `json:"independent"`
				Reason      string `json:"reason"`
			}
			if e.readArtifactJSON(a, &rs) == nil && rs.Review != "" {
				h.Review = facade.HandoffReview{Status: rs.Review, Independent: rs.Independent, Reason: rs.Reason}
			}
		}
	}

	evs, err := e.opts.ControlPlane.Events(ctx, storage.EventQuery{
		ProjectID: e.opts.ProjectID, TaskID: detail.Task.ID, Types: []events.Type{events.TypeValidationCompleted},
	})
	if err != nil {
		return nil, err
	}
	for _, ev := range evs {
		if vc, ok := ev.Payload.(*events.ValidationCompleted); ok && vc.AttemptID == att.ID {
			h.Validations = append(h.Validations, facade.HandoffValidation{ValidationID: vc.ValidationID, Outcome: string(vc.Status)})
		}
	}

	q := shellQuote(repo.Path)
	h.Inspect = facade.HandoffInspect{
		Diff:       fmt.Sprintf("git -C %s diff %s..%s", q, base, att.CandidateCommit),
		Log:        fmt.Sprintf("git -C %s log --stat %s..%s", q, base, att.CandidateCommit),
		Merge:      fmt.Sprintf("git -C %s merge --no-ff %s", q, att.CandidateCommit),
		CherryPick: fmt.Sprintf("git -C %s cherry-pick %s", q, att.CandidateCommit),
	}
	return h, nil
}

// readArtifactJSON reads a bounded artifact written by this executor's sink and
// verifies its digest. The locator must stay inside the state directory.
func (e *Executor) readArtifactJSON(ref protocol.ArtifactRef, into any) error {
	root, err := filepath.Abs(filepath.Join(e.opts.StateDir, "artifacts"))
	if err != nil {
		return err
	}
	loc, err := filepath.Abs(ref.Locator)
	if err != nil {
		return err
	}
	if rel, rerr := filepath.Rel(root, loc); rerr != nil || strings.HasPrefix(rel, "..") {
		return errs.New(errs.CategoryPolicyDenied, "artifact locator is outside the artifact store")
	}
	fi, err := os.Lstat(loc)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxHandoffArtifact {
		return errs.New(errs.CategoryNotFound, "artifact %s is unavailable", ref.ID)
	}
	b, err := os.ReadFile(loc)
	if err != nil {
		return err
	}
	if ref.Digest != "" && protocol.DigestBytes(b) != ref.Digest {
		return errs.New(errs.CategoryIntegrity, "artifact %s does not match its digest", ref.ID)
	}
	return json.Unmarshal(b, into)
}

func summarizeReport(rep validationReport) *facade.HandoffChecks {
	out := &facade.HandoffChecks{
		ProfileID: rep.ProfileID, Passed: rep.Passed, RoundsUsed: rep.RoundsUsed,
		RepairRounds: max(0, rep.RoundsUsed-1), MaxRepairRounds: rep.MaxRepairRounds, Rounds: []facade.HandoffRound{},
	}
	for _, r := range rep.Rounds {
		hr := facade.HandoffRound{Round: r.Round, Passed: r.Passed, Checks: []facade.HandoffCheck{}}
		for _, c := range r.Checks {
			hr.Checks = append(hr.Checks, facade.HandoffCheck{ID: c.ID, Argv: c.Argv, Status: c.Status, ExitCode: c.ExitCode})
		}
		out.Rounds = append(out.Rounds, hr)
	}
	return out
}

// parseModelIdentity splits "endpoint/model@digest" as recorded by Delegate.
func parseModelIdentity(id string) facade.HandoffModel {
	var m facade.HandoffModel
	rest := id
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		m.Digest, rest = rest[i+1:], rest[:i]
	}
	if i := strings.Index(rest, "/"); i >= 0 {
		m.EndpointID, m.Model = rest[:i], rest[i+1:]
	} else {
		m.Model = rest
	}
	return m
}

// parseNameStatusZ parses `git diff --name-status -z` output (bounded).
func parseNameStatusZ(raw []byte) []facade.ChangedFile {
	out := []facade.ChangedFile{}
	toks := strings.Split(string(raw), "\x00")
	for i := 0; i < len(toks) && len(out) < maxManifestEntries; i++ {
		st := toks[i]
		if st == "" {
			continue
		}
		n := 1
		if st[0] == 'R' || st[0] == 'C' {
			n = 2
		}
		if i+n >= len(toks) || toks[i+n] == "" {
			break
		}
		out = append(out, facade.ChangedFile{Status: st, Path: toks[i+n]})
		i += n
	}
	return out
}

// shellQuote single-quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
