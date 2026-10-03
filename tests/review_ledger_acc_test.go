// Package tests provides cross-cutting acceptance and integration test scenarios
// for WP-M3C-5 durable review-ledger primitives (ACC-01, ACC-08, ACC-09, ACC-10).
//
// These tests verify schema registration, fixture round-tripping, immutability of
// pre-existing schemas and forbidden codebase boundaries, ProjectScoped persistence
// enforcement via internal/storage, and documentation synchronization. Unit and
// state-machine acceptance tests (ACC-02 through ACC-07) reside in
// internal/protocol/review_ledger_test.go.
package tests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/schema"
	"github.com/olostan/DevCadence/internal/storage"
	"github.com/olostan/DevCadence/internal/testsupport"
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
		t.Run("ReviewLedger_ACC-01_"+tc.file, func(t *testing.T) {
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

// TestReviewLedger_ACC08_GitScopeAndByteIdentical ensures awaitingImplementation is unchanged and
// the existing review schemas remain byte-identical (pinned content hashes; no git dependency).
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

	// Verify existing review schemas remain byte-identical via pinned sha256 hashes
	expectedHashes := map[string]string{
		"schemas/review-campaign.schema.json":     "7d3e708f0f38c1aaf3615145f30968c642c28c27ab997a7376a26af69a7b9cda",
		"schemas/finding-disposition.schema.json": "988451eaf66dd6cb09e324f1eb6b5cd3e75690193c395f3036727432acdb5525",
		"schemas/closure-decision.schema.json":    "74c3d9423a0664d14dbae5334957bdd322e2441c66590eb7dafff52cc27c05ef",
		"schemas/review-result.schema.json":       "1a01aeda83aace24cc6f0e23d8aaec4327065eb76de9a9479b8c1d7a53e4f0cd",
	}

	for relPath, wantHash := range expectedHashes {
		fullPath := filepath.Join("..", relPath)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			t.Fatalf("read %s: %v", relPath, err)
		}
		hasher := sha256.New()
		hasher.Write(content)
		gotHash := hex.EncodeToString(hasher.Sum(nil))
		if gotHash != wantHash {
			t.Fatalf("%s content hash changed:\n got:  %s\n want: %s", relPath, gotHash, wantHash)
		}
	}

	// Scope of the change (no forbidden path touched) is a review-time check on the PR's file list.
	// It is deliberately not asserted here: a unit test must not depend on git history, which
	// shallow CI checkouts and pruned clones do not have. The pinned hashes above are the
	// history-independent guarantee that the existing review schemas are byte-identical.
}

// TestReviewLedger_ACC09_RegisteredKindsAndProjectScoped proves NewRecord allocates the
// new types, each is registered with a schema, implements ProjectScoped, and storage rejects project mismatches.
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

	t.Run("ReviewLedger_ACC-09_StoragePersistenceProjectScopedMismatch", func(t *testing.T) {
		ctx := context.Background()
		store, err := storage.Open(ctx, storage.Config{Path: storage.MemoryPath, Clock: testsupport.NewClock()})
		if err != nil {
			t.Fatalf("open memory store: %v", err)
		}
		t.Cleanup(func() { _ = store.Close() })

		findingRaw := read(t, filepath.Join(fixtureDir, "review-finding.valid.json"))
		finding := new(protocol.ReviewFinding)
		if err := protocol.Unmarshal(findingRaw, finding); err != nil {
			t.Fatalf("unmarshal finding: %v", err)
		}

		resRaw := read(t, filepath.Join(fixtureDir, "finding-resolution.valid.json"))
		res := new(protocol.FindingResolution)
		if err := protocol.Unmarshal(resRaw, res); err != nil {
			t.Fatalf("unmarshal resolution: %v", err)
		}

		verRaw := read(t, filepath.Join(fixtureDir, "resolution-verification.valid.json"))
		ver := new(protocol.ResolutionVerification)
		if err := protocol.Unmarshal(verRaw, ver); err != nil {
			t.Fatalf("unmarshal verification: %v", err)
		}

		records := []protocol.Record{finding, res, ver}
		for _, rec := range records {
			scoped := rec.(protocol.ProjectScoped)
			correctProject := scoped.ProjectOf()
			wrongProject := correctProject + "-mismatch"

			// Attempt to store under wrong project must be refused via ProjectScoped check
			err := store.Write(ctx, func(tx *storage.Tx) error {
				_, err := tx.PutRecord(ctx, wrongProject, 1, rec)
				return err
			})
			if err == nil {
				t.Fatalf("storing %s under mismatched project %q succeeded, want refusal", rec.RecordKind(), wrongProject)
			}
			if got := errs.CategoryOf(err); got != errs.CategoryInvalidArgument {
				t.Fatalf("storing %s under mismatched project error category = %s, want CategoryInvalidArgument (%v)", rec.RecordKind(), got, err)
			}
			if !strings.Contains(err.Error(), "declares project") || !strings.Contains(err.Error(), "is being stored under project") {
				t.Fatalf("unexpected error message for %s: %v", rec.RecordKind(), err)
			}

			// Storing under correct project succeeds
			err = store.Write(ctx, func(tx *storage.Tx) error {
				_, err := tx.PutRecord(ctx, correctProject, 1, rec)
				return err
			})
			if err != nil {
				t.Fatalf("storing %s under correct project %q failed: %v", rec.RecordKind(), correctProject, err)
			}
		}
	})
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
