package flightrec_test

import (
	"path/filepath"
	"testing"

	"github.com/olostan/DevCadence/internal/flightrec"
	"github.com/olostan/DevCadence/internal/setup"
)

// TestResolveParityWithSetupHome asserts that the env_home and user_home steps
// agree with setup.ResolveHome()+"/traces" (WP-TRACE-1 §8.5). The process
// environment is pinned with t.Setenv so no host state is consulted.
func TestResolveParityWithSetupHome(t *testing.T) {
	t.Setenv("DEVCADENCE_TRACE_DIR", "")
	t.Setenv("XDG_STATE_HOME", "")

	t.Setenv("DEVCADENCE_HOME", filepath.Join(t.TempDir(), "dc-home"))
	home, err := setup.ResolveHome()
	if err != nil {
		t.Fatal(err)
	}
	res, err := flightrec.Resolve(flightrec.ResolveInput{})
	if err != nil || res.Source != flightrec.SourceEnvHome || res.Root != filepath.Join(home, "traces") {
		t.Fatalf("env_home: %+v %v (want %s)", res, err, filepath.Join(home, "traces"))
	}

	t.Setenv("DEVCADENCE_HOME", "")
	t.Setenv("HOME", t.TempDir())
	home, err = setup.ResolveHome()
	if err != nil {
		t.Fatal(err)
	}
	res, err = flightrec.Resolve(flightrec.ResolveInput{})
	if err != nil || res.Source != flightrec.SourceUserHome || res.Root != filepath.Join(home, "traces") {
		t.Fatalf("user_home: %+v %v (want %s)", res, err, filepath.Join(home, "traces"))
	}
}
