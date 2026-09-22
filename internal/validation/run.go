package validation

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/olostan/DevCadence/internal/artifacts"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/process"
	"github.com/olostan/DevCadence/internal/protocol"
)

// RunOptions configures one profile execution.
type RunOptions struct {
	// Dir is the working directory every check runs in — normally an
	// attempt's worktree, or the registered repository path for a baseline
	// run. It is required and is never defaulted to the daemon's own
	// directory (docs/SECURITY.md §6).
	Dir string
	// Env is the base environment each check's Env overrides are layered
	// onto. process.BaseEnv() is used when nil.
	Env []string
	// Runner executes each check. process.NewRunner() is used when nil.
	Runner *process.Runner
	// Artifacts stores stdout/stderr. Required: validation evidence must be
	// retrievable (DCI-011), not merely summarised.
	Artifacts *artifacts.Store
	// ProjectID scopes stored artifacts.
	ProjectID string
	// MaxOutputBytes bounds each check's captured stdout/stderr. Zero
	// selects process.DefaultMaxOutputBytes.
	MaxOutputBytes int64
	// RunID identifies this validation run, used for isolated service directories.
	RunID string
	// Modules provides the project's authoritative module catalog for resolving CheckSpec.ModuleID.
	Modules []protocol.ModuleDefinition
}

// RunProfile executes every check in profile, in order, against opts.Dir.
//
// Every check runs even after an earlier one fails: partial evidence (only
// the first failure) would be weaker than DCI-040 asks for, and the check
// list is normally small and already bounded by the profile's own timeouts,
// so running the rest costs little. A future profile format may add
// stop-on-failure if a concrete need appears (docs/IMPLEMENTATION_PLAN.md M2
// §14 asks for it only "if clearly needed").
//
// The returned checks and outcome are the raw material for a
// protocol.ValidationResult; RunProfile does not build or persist one itself
// so that callers validating a baseline, an attempt or an integration can
// supply the right Subject.
func RunProfile(ctx context.Context, profile Profile, opts RunOptions) (checks []protocol.CheckResult, outcome protocol.ValidationOutcome, err error) {
	if opts.Dir == "" {
		return nil, "", errs.New(errs.CategoryInvalidArgument, "validation: dir is required")
	}
	if opts.Artifacts == nil {
		return nil, "", errs.New(errs.CategoryInvalidArgument, "validation: artifact store is required")
	}
	if opts.ProjectID == "" {
		return nil, "", errs.New(errs.CategoryInvalidArgument, "validation: project id is required")
	}
	if err := profile.Validate(); err != nil {
		return nil, "", err
	}
	runner := opts.Runner
	if runner == nil {
		runner = process.NewRunner()
	}
	baseEnv := opts.Env
	if baseEnv == nil {
		baseEnv = process.BaseEnv()
	}
	maxOutput := opts.MaxOutputBytes
	if maxOutput == 0 {
		maxOutput = process.DefaultMaxOutputBytes
	}

	findModule := func(modID string) *protocol.ModuleDefinition {
		for i := range opts.Modules {
			if opts.Modules[i].ID == modID {
				return &opts.Modules[i]
			}
		}
		return nil
	}

	if profile.ModuleID != "" {
		if findModule(profile.ModuleID) == nil {
			return nil, protocol.ValidationError, errs.New(errs.CategoryInvalidArgument, "validation: profile %q specifies unknown module %q", profile.Name, profile.ModuleID)
		}
	}

	effectiveServices := make([]ServiceSpec, len(profile.Services))
	for i, spec := range profile.Services {
		effectiveModID := spec.ModuleID
		if effectiveModID == "" {
			effectiveModID = profile.ModuleID
		}
		if effectiveModID != "" && findModule(effectiveModID) == nil {
			return nil, protocol.ValidationError, errs.New(errs.CategoryInvalidArgument, "validation: service %q specifies unknown module %q", spec.ID, effectiveModID)
		}
		effectiveServices[i] = spec
		effectiveServices[i].ModuleID = effectiveModID
	}

	for _, spec := range profile.Checks {
		effectiveModID := spec.ModuleID
		if effectiveModID == "" {
			effectiveModID = profile.ModuleID
		}
		if effectiveModID != "" && findModule(effectiveModID) == nil {
			return nil, protocol.ValidationError, errs.New(errs.CategoryInvalidArgument, "validation: check %q specifies unknown module %q", spec.ID, effectiveModID)
		}
	}

	checks = make([]protocol.CheckResult, 0, len(profile.Checks))
	sawFail, sawError, sawCancel := false, false, false

	var activeServices *ActiveServices
	if len(profile.Services) > 0 {
		runID := opts.RunID
		if runID == "" {
			runID = fmt.Sprintf("val_%d", time.Now().UnixNano())
		}
		active, startErr := StartServices(ctx, effectiveServices, opts.Dir, baseEnv, runID, opts.Modules)
		if startErr != nil {
			return nil, protocol.ValidationError, startErr
		}
		activeServices = active
		defer func() {
			if tErr := activeServices.Teardown(context.Background()); tErr != nil {
				if err == nil {
					err = tErr
				} else {
					err = fmt.Errorf("%w; teardown error: %v", err, tErr)
				}
				outcome = protocol.ValidationError
			}
		}()
		baseEnv = activeServices.Env()
	}

	for _, spec := range profile.Checks {
		if spec.ID == "" {
			// Same default (and the same uniqueness guarantee, enforced by
			// profile.Validate above) LoadProfiles' buildProfiles applies,
			// so a programmatically constructed Profile that skips
			// LoadProfiles still gets unambiguous per-check IDs.
			spec.ID = defaultCheckID(spec)
		}
		if spec.Kind == "" {
			spec.Kind = spec.ID
		}
		if ctx.Err() != nil {
			// The caller already cancelled: report every remaining check as
			// cancelled rather than starting and immediately killing each
			// process in turn, which would be wasteful and would make
			// "cancelled" and "ran for a moment then got killed" look the
			// same in the evidence.
			now := time.Now().UTC()
			checks = append(checks, protocol.CheckResult{
				ID: spec.ID, Kind: spec.Kind, Command: spec.Argv, WorkingDirectory: &opts.Dir,
				Status: protocol.CheckCancelled, StartedAt: protocol.NewTimestamp(now), FinishedAt: protocol.NewTimestamp(now),
			})
			sawCancel = true
			continue
		}

		if activeServices != nil {
			if err := activeServices.CheckHealth(); err != nil {
				now := time.Now().UTC()
				msg := err.Error()
				checks = append(checks, protocol.CheckResult{
					ID:               spec.ID,
					Kind:             spec.Kind,
					Command:          spec.Argv,
					WorkingDirectory: &opts.Dir,
					Status:           protocol.CheckError,
					Summary:          &msg,
					StartedAt:        protocol.NewTimestamp(now),
					FinishedAt:       protocol.NewTimestamp(now),
				})
				sawError = true
				break
			}
		}

		env := baseEnv
		if len(spec.Env) > 0 {
			env = process.MergeEnv(baseEnv, spec.Env)
		}

		effectiveModID := spec.ModuleID
		if effectiveModID == "" {
			effectiveModID = profile.ModuleID
		}

		workingDir := opts.Dir
		if effectiveModID != "" {
			foundMod := findModule(effectiveModID)
			if foundMod != nil {
				moduleBaseDir, err := ValidateDirContainment(opts.Dir, foundMod.Path)
				if err != nil {
					now := time.Now().UTC()
					msg := err.Error()
					checks = append(checks, protocol.CheckResult{
						ID:               spec.ID,
						Kind:             spec.Kind,
						Command:          spec.Argv,
						WorkingDirectory: &opts.Dir,
						Status:           protocol.CheckError,
						Summary:          &msg,
						StartedAt:        protocol.NewTimestamp(now),
						FinishedAt:       protocol.NewTimestamp(now),
					})
					sawError = true
					continue
				}
				workingDir = moduleBaseDir
			}
		}

		if spec.Dir != "" {
			containedDir, err := ValidateDirContainment(workingDir, spec.Dir)
			if err != nil {
				now := time.Now().UTC()
				msg := err.Error()
				checks = append(checks, protocol.CheckResult{
					ID:               spec.ID,
					Kind:             spec.Kind,
					Command:          spec.Argv,
					WorkingDirectory: &opts.Dir,
					Status:           protocol.CheckError,
					Summary:          &msg,
					StartedAt:        protocol.NewTimestamp(now),
					FinishedAt:       protocol.NewTimestamp(now),
				})
				sawError = true
				continue
			}
			workingDir = containedDir
		}

		checkCtx, cancel := context.WithCancelCause(ctx)
		var stopMonitor func()
		if activeServices != nil {
			stopMonitor = activeServices.Monitor(checkCtx, cancel)
		}

		started := time.Now().UTC()
		res, runErr := runner.Run(checkCtx, process.Spec{
			Executable:     spec.Argv[0],
			Args:           spec.Argv[1:],
			Dir:            workingDir,
			Env:            env,
			Timeout:        spec.Timeout,
			MaxStdoutBytes: maxOutput,
			MaxStderrBytes: maxOutput,
		})
		finished := time.Now().UTC()
		if stopMonitor != nil {
			stopMonitor()
		}
		cancel(nil)

		// If execution aborted due to a background service crash, record immediately
		if cause := context.Cause(checkCtx); cause != nil && cause != context.Canceled {
			check := protocol.CheckResult{
				ID:               spec.ID,
				Kind:             spec.Kind,
				Command:          spec.Argv,
				WorkingDirectory: &workingDir,
				StartedAt:        protocol.NewTimestamp(started),
				FinishedAt:       protocol.NewTimestamp(finished),
				Status:           protocol.CheckError,
			}
			summary := fmt.Sprintf("service failure during check execution: %v", cause)
			check.Summary = &summary
			sawError = true
			checks = append(checks, check)
			break
		}

		check := protocol.CheckResult{
			ID:               spec.ID,
			Kind:             spec.Kind,
			Command:          spec.Argv,
			WorkingDirectory: &workingDir,
			StartedAt:        protocol.NewTimestamp(started),
			FinishedAt:       protocol.NewTimestamp(finished),
		}
		if v, err := gitVersion(ctx, runner, spec.Argv, env, workingDir); err == nil && v != "" {
			check.ToolVersion = &v
		}

		if runErr != nil {
			// The runner never started the command (missing executable,
			// invalid spec, internal failure). This is a check that errored,
			// not one that failed: it establishes no fact about the code
			// under test (DCI-041).
			check.Status = protocol.CheckError
			summary := runErr.Error()
			check.Summary = &summary
			sawError = true
			checks = append(checks, check)
			continue
		}

		check.ExitCode = intPtr(res.ExitCode)
		stdoutRef, stderrRef := storeCheckOutput(ctx, opts.Artifacts, opts.ProjectID, spec.ID, res)
		if stdoutRef != nil {
			check.StdoutArtifact = &stdoutRef.ID
		}
		if stderrRef != nil {
			check.StderrArtifact = &stderrRef.ID
		}
		check.OutputTruncated = res.StdoutTruncated || res.StderrTruncated

		switch res.Status {
		case process.StatusTimeout:
			check.Status = protocol.CheckError
			summary := "check exceeded its timeout"
			check.Summary = &summary
			sawError = true
		case process.StatusCancelled:
			check.Status = protocol.CheckCancelled
			sawCancel = true
		default:
			if res.ExitCode == 0 {
				check.Status = protocol.CheckPass
			} else {
				check.Status = protocol.CheckFail
				sawFail = true
			}
		}
		checks = append(checks, check)
	}

	// Verify services remained healthy after the final check
	if activeServices != nil && !sawError {
		if err := activeServices.CheckHealth(); err != nil {
			sawError = true
		}
	}

	outcome = protocol.ValidationPass
	switch {
	case sawCancel:
		outcome = protocol.ValidationCancelled
	case sawError:
		outcome = protocol.ValidationError
	case sawFail:
		outcome = protocol.ValidationFail
	}
	return checks, outcome, nil
}

// storeCheckOutput persists a check's stdout/stderr as artifacts. Storage
// failures are deliberately not fatal to the validation run itself: the
// check outcome (pass/fail/error) is the authoritative evidence, and a
// storage hiccup is reported by simply omitting the artifact reference
// rather than discarding a deterministic result the tool already produced.
func storeCheckOutput(ctx context.Context, store *artifacts.Store, projectID, checkID string, res process.Result) (*protocol.ArtifactRef, *protocol.ArtifactRef) {
	var stdoutRef, stderrRef *protocol.ArtifactRef
	if len(res.Stdout) > 0 || res.StdoutTruncated {
		if out, err := store.Put(ctx, artifacts.PutInput{
			ProjectID: projectID, Kind: "stdout:" + checkID, MediaType: "text/plain",
			Reader: bytes.NewReader(res.Stdout),
		}); err == nil {
			r := out.Ref
			stdoutRef = &r
		}
	}
	if len(res.Stderr) > 0 || res.StderrTruncated {
		if out, err := store.Put(ctx, artifacts.PutInput{
			ProjectID: projectID, Kind: "stderr:" + checkID, MediaType: "text/plain",
			Reader: bytes.NewReader(res.Stderr),
		}); err == nil {
			r := out.Ref
			stderrRef = &r
		}
	}
	return stdoutRef, stderrRef
}

// gitVersion is a narrow convenience: when a check invokes `git`, its
// version is worth recording (docs/PROTOCOLS.md §10: "tool version"). Other
// tools are not probed automatically — a generic "run --version for
// anything" policy would itself be an uncontrolled command, which
// docs/IMPLEMENTATION_PLAN.md M2 §10 rules out.
func gitVersion(ctx context.Context, runner *process.Runner, argv, env []string, dir string) (string, error) {
	if len(argv) == 0 || argv[0] != "git" {
		return "", nil
	}
	res, err := runner.Run(ctx, process.Spec{
		Executable: "git", Args: []string{"--version"}, Dir: dir, Env: env, Timeout: 5 * time.Second,
	})
	if err != nil || !res.Success() {
		return "", err
	}
	return trimTrailingNewline(string(res.Stdout)), nil
}

func trimTrailingNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func intPtr(v int) *int { return &v }
