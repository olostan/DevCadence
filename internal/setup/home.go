package setup

import (
	"os"
	"path/filepath"

	"github.com/olostan/DevCadence/internal/errs"
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

// layoutDirs are the directories EnsureLayout creates under $DEVCADENCE_HOME,
// matching the names internal/setup/doctor.go's checkStateRoot already
// checks for.
var layoutDirs = []string{
	"state",
	filepath.Join("artifacts", "setup"),
	"tmp",
}

// EnsureLayout idempotently creates the $DEVCADENCE_HOME directory layout
// (state/, artifacts/setup/, tmp/) with mode 0700, per ADR-0014 §4.
func EnsureLayout(home string) error {
	if !filepath.IsAbs(home) {
		return errs.New(errs.CategoryInvalidArgument, "EnsureLayout: home must be an absolute path, got %q", home)
	}
	for _, dir := range layoutDirs {
		path := filepath.Join(home, dir)
		if err := os.MkdirAll(path, 0700); err != nil {
			return errs.Wrap(errs.CategoryInternal, err, "create %s", path)
		}
	}
	return nil
}
