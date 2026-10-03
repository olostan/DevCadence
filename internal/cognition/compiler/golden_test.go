package compiler_test

import (
	"encoding/json"
	"errors"
	"flag"
	"io/fs"
	"os"
	"testing"
)

// goldenFile pins the digests of the embedded normative source, the catalog
// derived from it, and the compiled invocation for a fixed request. It is
// regenerated with `make update-goldens`, never edited by hand.
const goldenFile = "testdata/golden_digests.json"

var updateGoldens = flag.Bool("update-goldens", false,
	"rewrite "+goldenFile+" from the current compiler output; use `make update-goldens`, which guards against masking unintended behavior changes")

// Golden names. They are the JSON keys in goldenFile.
const (
	goldenNormativeSource     = "normative_source"
	goldenAuthorityProjection = "authority_projection"
	goldenCatalog             = "catalog"
	goldenPack                = "pack"
	goldenInvocationMarkdown  = "invocation_markdown"
	goldenInvocationJSON      = "invocation_json"
)

func readGoldens(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(goldenFile)
	if errors.Is(err, fs.ErrNotExist) && *updateGoldens {
		return map[string]string{}
	}
	if err != nil {
		t.Fatalf("read %s: %v", goldenFile, err)
	}
	set := map[string]string{}
	if err := json.Unmarshal(raw, &set); err != nil {
		t.Fatalf("parse %s: %v", goldenFile, err)
	}
	return set
}

func writeGoldens(t *testing.T, set map[string]string) {
	t.Helper()
	raw, err := json.MarshalIndent(set, "", "  ")
	if err != nil {
		t.Fatalf("encode goldens: %v", err)
	}
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatalf("create testdata: %v", err)
	}
	if err := os.WriteFile(goldenFile, append(raw, '\n'), 0o644); err != nil {
		t.Fatalf("write %s: %v", goldenFile, err)
	}
}

// checkGolden compares got with the pinned digest. With -update-goldens it
// records got instead.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	set := readGoldens(t)
	if *updateGoldens {
		if set[name] != got {
			t.Logf("updated golden %s: %q -> %q", name, set[name], got)
			set[name] = got
			writeGoldens(t, set)
		}
		return
	}
	want, ok := set[name]
	if !ok {
		t.Fatalf("golden %q missing from %s; run `make update-goldens`", name, goldenFile)
	}
	if got != want {
		t.Errorf("%s digest mismatch:\ngot:  %s\nwant: %s\n"+
			"If INVARIANTS.md or the invariant catalog changed intentionally, run `make update-goldens` and commit %s with that change. "+
			"Otherwise compiler output changed unintentionally.",
			name, got, want, goldenFile)
	}
}
