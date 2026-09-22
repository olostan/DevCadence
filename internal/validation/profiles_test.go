package validation_test

import (
	"os"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/validation"
)

const exampleDoc = `
validation:
  fast:
    - argv: ["go", "test", "./internal/..."]
      timeout: 10m
  full:
    - argv: ["go", "test", "./..."]
      timeout: 30m
    - argv: ["go", "vet", "./..."]
      timeout: 10m
`

func TestLoadProfiles(t *testing.T) {
	profiles, err := validation.LoadProfiles([]byte(exampleDoc))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	fast, ok := profiles["fast"]
	if !ok {
		t.Fatal("missing fast profile")
	}
	if len(fast.Checks) != 1 || fast.Checks[0].ID != "go-test-./internal/..." {
		t.Fatalf("fast checks = %+v", fast.Checks)
	}
	if fast.Checks[0].Timeout.String() != "10m0s" {
		t.Fatalf("timeout = %v", fast.Checks[0].Timeout)
	}
	full := profiles["full"]
	if len(full.Checks) != 2 {
		t.Fatalf("full checks = %+v", full.Checks)
	}
	// The default ID is the check's full joined argv, not just its
	// executable name: "go test ..." and "go vet ..." in the same profile
	// must not collapse onto the same ambiguous "go" ID (docs regression:
	// this is exactly the shape of the shipped fast/full profiles below).
	if full.Checks[0].ID == full.Checks[1].ID {
		t.Fatalf("full's two checks defaulted to the same id %q", full.Checks[0].ID)
	}
	if full.Checks[0].ID != "go-test-./..." || full.Checks[1].ID != "go-vet-./..." {
		t.Fatalf("full checks ids = %q, %q", full.Checks[0].ID, full.Checks[1].ID)
	}
}

// TestLoadProfilesNestedProfilesKey proves the `validation.profiles` shape
// documented and used by config/project.example.yaml (and README.md's
// walkthrough) actually loads, rather than only the flatter `validation:
// {name: [...]}` shape used in the tests above.
func TestLoadProfilesNestedProfilesKey(t *testing.T) {
	doc := `
validation:
  profiles:
    fast:
      - argv: ["go", "test", "./internal/..."]
        timeout: 10m
    full:
      - argv: ["go", "test", "./..."]
        timeout: 30m
`
	profiles, err := validation.LoadProfiles([]byte(doc))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	fast, ok := profiles["fast"]
	if !ok {
		t.Fatal("missing fast profile")
	}
	if len(fast.Checks) != 1 || fast.Checks[0].ID != "go-test-./internal/..." {
		t.Fatalf("fast checks = %+v", fast.Checks)
	}
	if _, ok := profiles["full"]; !ok {
		t.Fatal("missing full profile")
	}
}

// TestLoadProfilesActualExampleFile is a regression test using the real
// config/project.example.yaml shipped in the repository, so a future change
// to either the loader or the example file cannot silently drift apart
// again the way this test's underlying bug let them.
func TestLoadProfilesActualExampleFile(t *testing.T) {
	data, err := os.ReadFile("../../config/project.example.yaml")
	if err != nil {
		t.Fatalf("read example config: %v", err)
	}
	profiles, err := validation.LoadProfiles(data)
	if err != nil {
		t.Fatalf("load example config: %v", err)
	}
	if _, ok := profiles["fast"]; !ok {
		t.Fatalf("missing fast profile: %+v", profiles)
	}
	if _, ok := profiles["full"]; !ok {
		t.Fatalf("missing full profile: %+v", profiles)
	}
}

func TestLoadProfilesDirectMap(t *testing.T) {
	doc := `
fast:
  - argv: ["echo", "ok"]
    timeout: 1m
`
	profiles, err := validation.LoadProfiles([]byte(doc))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := profiles["fast"]; !ok {
		t.Fatal("missing fast profile")
	}
}

func TestLoadProfilesRejectsEmptyArgv(t *testing.T) {
	doc := `
validation:
  bad:
    - argv: []
      timeout: 1m
`
	_, err := validation.LoadProfiles([]byte(doc))
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category = %v", errs.CategoryOf(err))
	}
}

func TestLoadProfilesRejectsMissingTimeout(t *testing.T) {
	doc := `
validation:
  bad:
    - argv: ["echo", "hi"]
`
	_, err := validation.LoadProfiles([]byte(doc))
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category = %v", errs.CategoryOf(err))
	}
}

// TestLoadProfilesRejectsDuplicateDefaultCheckIDs proves a profile whose
// checks would still collide even under the joined-argv default (here, two
// checks with literally identical argv and no explicit id) is refused
// clearly at load time rather than silently producing a ValidationResult
// with two ambiguous CheckResult entries sharing one ID.
func TestLoadProfilesRejectsDuplicateDefaultCheckIDs(t *testing.T) {
	doc := `
validation:
  bad:
    - argv: ["go", "test", "./..."]
      timeout: 1m
    - argv: ["go", "test", "./..."]
      timeout: 2m
`
	_, err := validation.LoadProfiles([]byte(doc))
	if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("category = %v (err=%v)", errs.CategoryOf(err), err)
	}
}

// TestLoadProfilesExampleFastProfileChecksHaveDistinctIDs is a focused
// regression on the actual shipped config/project.example.yaml `fast`
// profile, which (docs/README.md's own example) pairs `go test ...` with
// `go vet ...` in the same profile: under the old executable-name-only
// default, both checks silently collapsed onto the ambiguous ID "go", so a
// failure in either could not be told apart in FailedChecks or the stored
// CheckResults. The joined-argv default must keep them distinct.
func TestLoadProfilesExampleFastProfileChecksHaveDistinctIDs(t *testing.T) {
	data, err := os.ReadFile("../../config/project.example.yaml")
	if err != nil {
		t.Fatalf("read example config: %v", err)
	}
	profiles, err := validation.LoadProfiles(data)
	if err != nil {
		t.Fatalf("load example config: %v", err)
	}
	fast, ok := profiles["fast"]
	if !ok {
		t.Fatal("missing fast profile")
	}
	seen := map[string]bool{}
	for _, c := range fast.Checks {
		if seen[c.ID] {
			t.Fatalf("fast profile has two checks defaulting to the same id %q: %+v", c.ID, fast.Checks)
		}
		seen[c.ID] = true
	}
	if len(seen) < 2 {
		t.Fatalf("expected the shipped fast profile to have more than one check, got %+v", fast.Checks)
	}
}
