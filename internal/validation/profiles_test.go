package validation_test

import (
	"os"
	"testing"

	"github.com/olostan/DevCadience/internal/errs"
	"github.com/olostan/DevCadience/internal/validation"
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
	if len(fast.Checks) != 1 || fast.Checks[0].ID != "go" {
		t.Fatalf("fast checks = %+v", fast.Checks)
	}
	if fast.Checks[0].Timeout.String() != "10m0s" {
		t.Fatalf("timeout = %v", fast.Checks[0].Timeout)
	}
	full := profiles["full"]
	if len(full.Checks) != 2 {
		t.Fatalf("full checks = %+v", full.Checks)
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
	if len(fast.Checks) != 1 || fast.Checks[0].ID != "go" {
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
