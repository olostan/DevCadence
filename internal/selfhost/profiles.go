package selfhost

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/taskexec"
	"github.com/olostan/DevCadence/internal/validation"
)

// Project post-check convention: the registered repository may define
// ValidationProfileRelPath, a document in validation.LoadProfiles format, with a
// profile named PostCheckProfileID. Its commands are repository-controlled and
// therefore run only in execution_mode=yolo (see taskexec.preparePostCheck).
const (
	ValidationProfileRelPath = ".devcadence/validation.yaml"
	PostCheckProfileID       = "default"
	maxProfileBytes          = 256 * 1024
)

// DefaultMaxRepairRounds and HardMaxRepairRounds bound the repair loop.
const (
	DefaultMaxRepairRounds = 2
	HardMaxRepairRounds    = taskexec.HardMaxRepairRounds
)

// repoProfiles implements taskexec.ProfileSource over the registered
// repository's checked-out validation file. It is read when a delegation starts
// (before any model call), never from the model-edited candidate worktree.
type repoProfiles struct{ repoPath string }

// NewRepoProfileSource returns the ProfileSource for the repository at repoPath.
func NewRepoProfileSource(repoPath string) taskexec.ProfileSource {
	return repoProfiles{repoPath: repoPath}
}

func (p repoProfiles) Profile(_ context.Context, _ string, profileID string) (validation.Profile, error) {
	path := filepath.Join(p.repoPath, filepath.FromSlash(ValidationProfileRelPath))
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return validation.Profile{}, taskexec.ErrProfileNotConfigured
	}
	if err != nil {
		return validation.Profile{}, errs.Wrap(errs.CategoryInvalidArgument, err, "cannot stat %s", ValidationProfileRelPath)
	}
	if !fi.Mode().IsRegular() || fi.Size() > maxProfileBytes {
		return validation.Profile{}, errs.New(errs.CategoryInvalidArgument, "%s must be a regular file of at most %d bytes", ValidationProfileRelPath, maxProfileBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return validation.Profile{}, errs.Wrap(errs.CategoryInvalidArgument, err, "cannot read %s", ValidationProfileRelPath)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxProfileBytes+1))
	if err != nil {
		return validation.Profile{}, errs.Wrap(errs.CategoryInvalidArgument, err, "cannot read %s", ValidationProfileRelPath)
	}
	profiles, err := validation.LoadProfiles(raw)
	if err != nil {
		return validation.Profile{}, err
	}
	prof, ok := profiles[profileID]
	if !ok {
		return validation.Profile{}, errs.New(errs.CategoryNotFound, "%s has no profile %q", ValidationProfileRelPath, profileID)
	}
	return prof, nil
}
