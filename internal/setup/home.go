package setup

import (
	"os"
	"path/filepath"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// homeEnvVar names the environment variable that overrides the default
// $DEVCADENCE_HOME location, matching cmd/devcadence's own resolution.
const homeEnvVar = "DEVCADENCE_HOME"

// ResolveHome returns the root of DevCadence's machine-global operational
// state ($DEVCADENCE_HOME), reading the DEVCADENCE_HOME environment
// variable and falling back to $HOME/.devcadence. It mirrors
// cmd/devcadence/cmd_repo.go's defaultUnderHome exactly, so the CLI and
// service layers never disagree about where state lives.
func ResolveHome() (string, error) {
	home := os.Getenv(homeEnvVar)
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", errs.Wrap(errs.CategoryInvalidArgument, err, "cannot determine home directory")
		}
		home = filepath.Join(userHome, ".devcadence")
	}
	if !filepath.IsAbs(home) {
		return "", errs.New(errs.CategoryInvalidArgument, "%s must be an absolute path, got %q", homeEnvVar, home)
	}
	return home, nil
}

// managedDirLocations lists every ManagedDirectoryLocation LocationPath
// knows how to resolve, in the order EnsureLayout creates them. Kept as an
// explicit list (rather than iterating protocol constants) so a new
// location added to the protocol package fails LocationPath loudly instead
// of silently being skipped by EnsureLayout.
var managedDirLocations = []protocol.ManagedDirectoryLocation{
	protocol.LocationState,
	protocol.LocationArtifactsSetup,
	protocol.LocationTmp,
}

// LocationPath resolves an allowlisted ManagedDirectoryLocation to its
// absolute path under home. This is the single mapping every caller that
// needs a location's real path uses — EnsureLayout, the condition
// evaluator's managed_dir_exists check, and the create_directory operation
// applier — so the three can never silently disagree about where a
// location actually is.
func LocationPath(home string, loc protocol.ManagedDirectoryLocation) (string, error) {
	if !filepath.IsAbs(home) {
		return "", errs.New(errs.CategoryInvalidArgument, "LocationPath: home must be an absolute path, got %q", home)
	}
	switch loc {
	case protocol.LocationState:
		return filepath.Join(home, "state"), nil
	case protocol.LocationArtifactsSetup:
		return filepath.Join(home, "artifacts", "setup"), nil
	case protocol.LocationTmp:
		return filepath.Join(home, "tmp"), nil
	default:
		return "", errs.New(errs.CategoryInvalidArgument, "LocationPath: unhandled location %q", loc)
	}
}

// EnsureLayout idempotently creates the $DEVCADENCE_HOME directory layout
// (state/, artifacts/setup/, tmp/) with mode 0700, per ADR-0014 §4. A
// directory that already existed with a looser mode is tightened, not left
// as-is: ADR-0014's owner-only requirement is a property of the path, not
// just of paths this call happens to create.
func EnsureLayout(home string) error {
	for _, loc := range managedDirLocations {
		path, err := LocationPath(home, loc)
		if err != nil {
			return err
		}
		if err := ensureDirMode(path, 0700); err != nil {
			return err
		}
	}
	return nil
}

// ensureDirMode creates path (and parents) if absent, then enforces mode on
// it whether it was just created or already existed.
func ensureDirMode(path string, mode os.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "create %s", path)
	}
	if err := os.Chmod(path, mode); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "chmod %s", path)
	}
	return nil
}

// ensureFileMode enforces mode on an already-open/created file path,
// covering the case where OpenFile's perm argument was ignored because the
// file already existed with a looser mode.
func ensureFileMode(path string, mode os.FileMode) error {
	if err := os.Chmod(path, mode); err != nil {
		return errs.Wrap(errs.CategoryInternal, err, "chmod %s", path)
	}
	return nil
}
