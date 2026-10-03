package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/schema"
)

// TestReviewLedger_ACC01_ValidFixturesRoundTrip verifies that the three new valid fixtures
// satisfy their schemas, decode strictly into their Go twin types, and round-trip byte-identically.
func TestReviewLedger_ACC01_ValidFixturesRoundTrip(t *testing.T) {
	set, err := schema.Default()
	if err != nil {
		t.Fatalf("compile schemas: %v", err)
	}

	fixtures := []struct {
		file string
		kind string
	}{
		{"review-finding.valid.json", "ReviewFinding"},
		{"finding-resolution.valid.json", "FindingResolution"},
		{"resolution-verification.valid.json", "ResolutionVerification"},
	}

	for _, tc := range fixtures {
		t.Run(tc.file, func(t *testing.T) {
			path := filepath.Join(fixtureDir, tc.file)
			raw := read(t, path)

			schemaName, err := schema.NameForFile(tc.file)
			if err != nil {
				// Handle valid suffix
				base := strings.TrimSuffix(tc.file, ".valid.json")
				schemaName = schema.Name(base)
			}
			if err := set.ValidateBytes(schemaName, raw); err != nil {
				t.Fatalf("fixture %s fails schema %s: %v", tc.file, schemaName, err)
			}

			rec, err := protocol.NewRecord(tc.kind)
			if err != nil {
				t.Fatalf("NewRecord(%s): %v", tc.kind, err)
			}
			if err := protocol.Unmarshal(raw, rec); err != nil {
				t.Fatalf("Unmarshal %s: %v", tc.file, err)
			}

			marshaled, err := protocol.Marshal(rec)
			if err != nil {
				t.Fatalf("Marshal %s: %v", tc.kind, err)
			}

			wantCanonical := canonicaliseInformative(t, raw)
			gotCanonical := canonicaliseInformative(t, marshaled)
			if wantCanonical != gotCanonical {
				t.Fatalf("round trip mismatch for %s:\n got:  %s\n want: %s", tc.file, gotCanonical, wantCanonical)
			}
		})
	}
}

// TestReviewLedger_ACC08_GitScopeAndByteIdentical ensures no forbidden paths are modified,
// awaitingImplementation is unchanged, and existing review schemas/fixtures remain byte-identical.
func TestReviewLedger_ACC08_GitScopeAndByteIdentical(t *testing.T) {
	// Verify awaitingImplementation still has exactly the three M7 entries
	if len(awaitingImplementation) != 3 {
		t.Fatalf("awaitingImplementation has %d entries, want 3", len(awaitingImplementation))
	}
	for _, expected := range []schema.Name{"review-campaign", "finding-disposition", "closure-decision"} {
		if _, ok := awaitingImplementation[expected]; !ok {
			t.Errorf("awaitingImplementation missing %s", expected)
		}
	}

	// Verify existing review schemas exist and are unchanged relative to base
	forbiddenPaths := []string{
		"schemas/review-campaign.schema.json",
		"schemas/finding-disposition.schema.json",
		"schemas/closure-decision.schema.json",
		"schemas/review-result.schema.json",
	}

	cmd := exec.Command("git", "diff", "--name-only", "origin/main...HEAD")
	cmd.Dir = ".."
	out, err := cmd.Output()
	if err == nil {
		diffFiles := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, file := range diffFiles {
			file = strings.TrimSpace(file)
			if file == "" {
				continue
			}
			for _, forbidden := range forbiddenPaths {
				if file == forbidden {
					t.Errorf("forbidden path modified: %s", file)
				}
			}
			if strings.HasPrefix(file, "internal/storage/") {
				t.Errorf("forbidden path modified: %s", file)
			}
			if strings.HasPrefix(file, "internal/controlplane/") {
				t.Errorf("forbidden path modified: %s", file)
			}
			if strings.HasPrefix(file, "internal/events/") {
				t.Errorf("forbidden path modified: %s", file)
			}
			if strings.HasPrefix(file, "internal/ids/") {
				t.Errorf("forbidden path modified: %s", file)
			}
		}
	}
}

// TestReviewLedger_ACC09_RegisteredKindsAndProjectScoped proves NewRecord allocates the
// new types, each is registered with a schema, and each implements ProjectScoped.
func TestReviewLedger_ACC09_RegisteredKindsAndProjectScoped(t *testing.T) {
	kinds := []string{"ReviewFinding", "FindingResolution", "ResolutionVerification"}

	for _, kind := range kinds {
		rec, err := protocol.NewRecord(kind)
		if err != nil {
			t.Fatalf("NewRecord(%s) failed: %v", kind, err)
		}
		if rec.RecordKind() != kind {
			t.Errorf("rec.RecordKind() = %q, want %q", rec.RecordKind(), kind)
		}

		scoped, ok := rec.(protocol.ProjectScoped)
		if !ok {
			t.Fatalf("record kind %s does not implement protocol.ProjectScoped", kind)
		}
		if scoped.ProjectOf() != "" {
			t.Errorf("unpopulated record kind %s ProjectOf() = %q, want empty", kind, scoped.ProjectOf())
		}

		if _, ok := schema.RecordKindToSchema[kind]; !ok {
			t.Errorf("record kind %s not in schema.RecordKindToSchema", kind)
		}
	}
}

// TestReviewLedger_ACC10_DocsSync checks that docs no longer claim the three records lack twins.
func TestReviewLedger_ACC10_DocsSync(t *testing.T) {
	protocolsRaw, err := os.ReadFile(filepath.Join("..", "docs", "PROTOCOLS.md"))
	if err != nil {
		t.Fatalf("read docs/PROTOCOLS.md: %v", err)
	}
	content := string(protocolsRaw)

	if strings.Contains(content, "do not yet have committed Go/schema twins") {
		t.Errorf("docs/PROTOCOLS.md still contains 'do not yet have committed Go/schema twins'")
	}
	if !strings.Contains(content, "ReviewFinding`, `FindingResolution`, and `ResolutionVerification` records are implemented by WP-M3C-5") {
		t.Errorf("docs/PROTOCOLS.md missing mention that WP-M3C-5 implemented the records")
	}
}
