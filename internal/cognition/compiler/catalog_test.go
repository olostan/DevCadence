package compiler_test

import (
	"errors"
	"strings"
	"testing"

	devcadence "github.com/olostan/DevCadence"
	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/errs"
)

func TestParseInvariantsFromDoc_All94Invariants(t *testing.T) {
	rules, err := compiler.ParseInvariantsFromDoc(devcadence.InvariantsDoc, compiler.CanonicalCatalogRevision)
	if err != nil {
		t.Fatalf("unexpected error parsing invariants from INVARIANTS.md: %v", err)
	}

	if len(rules) != 94 {
		t.Fatalf("expected exactly 94 DCI invariants extracted, got %d", len(rules))
	}

	// Verify every invariant has non-empty fields and valid SHA-256 digest
	seenIDs := make(map[string]bool)
	for _, r := range rules {
		if !strings.HasPrefix(r.ID, "DCI-") {
			t.Errorf("rule ID %q does not start with DCI-", r.ID)
		}
		if seenIDs[r.ID] {
			t.Errorf("duplicate rule ID %q in parsed rules", r.ID)
		}
		seenIDs[r.ID] = true

		if !strings.HasPrefix(r.Content, "### "+r.ID+" — ") {
			t.Errorf("rule %s content does not start with exact heading: %q", r.ID, r.Content[:min(40, len(r.Content))])
		}
		if !strings.HasPrefix(r.ContentDigest, "sha256:") || len(r.ContentDigest) != 71 {
			t.Errorf("rule %s content digest invalid: %q", r.ID, r.ContentDigest)
		}
		if r.Revision != compiler.CanonicalCatalogRevision {
			t.Errorf("rule %s revision %q, want %q", r.ID, r.Revision, compiler.CanonicalCatalogRevision)
		}
		if r.SourceDoc != "INVARIANTS.md" {
			t.Errorf("rule %s source doc %q, want INVARIANTS.md", r.ID, r.SourceDoc)
		}
	}
}

func TestCanonicalRuleRegistry_FullCoverageAndFrozen(t *testing.T) {
	reg, err := compiler.NewCanonicalRuleRegistry()
	if err != nil {
		t.Fatalf("failed to create canonical rule registry: %v", err)
	}

	if reg.CatalogID() != compiler.CanonicalCatalogID {
		t.Errorf("catalog ID = %q, want %q", reg.CatalogID(), compiler.CanonicalCatalogID)
	}
	if reg.CatalogVersion() != compiler.CanonicalCatalogVersion {
		t.Errorf("catalog version = %q, want %q", reg.CatalogVersion(), compiler.CanonicalCatalogVersion)
	}
	if reg.CatalogRevision() != compiler.CanonicalCatalogRevision {
		t.Errorf("catalog revision = %q, want %q", reg.CatalogRevision(), compiler.CanonicalCatalogRevision)
	}
	if !strings.HasPrefix(reg.CatalogDigest(), "sha256:") || len(reg.CatalogDigest()) != 71 {
		t.Errorf("catalog digest invalid: %q", reg.CatalogDigest())
	}

	// Verify 100% reverse coverage
	if err := reg.VerifyReverseCoverage(); err != nil {
		t.Errorf("expected 100%% reverse coverage across all 94 invariants, got error: %v", err)
	}

	// Verify registry is frozen (cannot register new rule or modify metadata)
	err = reg.Register(compiler.Rule{
		ID:             "DCI-999",
		AdmissionClass: compiler.AdmissionClassAlways,
		SourceDoc:      "test.md",
		Revision:       "v1.0",
		Content:        "Dummy content",
	})
	if err == nil || !errors.Is(err, errs.ErrConflict) {
		t.Errorf("expected ErrConflict registering on frozen registry, got: %v", err)
	}

	err = reg.SetCatalogMeta("new-id", "v2.0", "new-rev", "new-digest")
	if err == nil || !errors.Is(err, errs.ErrConflict) {
		t.Errorf("expected ErrConflict setting catalog meta on frozen registry, got: %v", err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
