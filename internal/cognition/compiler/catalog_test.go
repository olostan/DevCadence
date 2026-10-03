package compiler_test

import (
	"errors"
	"strings"
	"testing"

	devcadence "github.com/olostan/DevCadence"
	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/errs"
)

func TestParseInvariantsFromDoc_AllCanonicalInvariants(t *testing.T) {
	rules, err := compiler.ParseInvariantsFromDoc(devcadence.InvariantsDoc, compiler.CanonicalCatalogRevision)
	if err != nil {
		t.Fatalf("unexpected error parsing invariants from INVARIANTS.md: %v", err)
	}

	if len(rules) != len(compiler.CanonicalInvariantMappings) {
		t.Fatalf("parsed %d DCI invariants, want exactly the %d mapped canonical invariants", len(rules), len(compiler.CanonicalInvariantMappings))
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
	if reg.SourceRevision() != compiler.CanonicalSourceRevision {
		t.Errorf("source revision = %q, want %q", reg.SourceRevision(), compiler.CanonicalSourceRevision)
	}
	if reg.MappingRevision() != compiler.CanonicalMappingRevision {
		t.Errorf("mapping revision = %q, want %q", reg.MappingRevision(), compiler.CanonicalMappingRevision)
	}
	if reg.CatalogRevision() != compiler.CanonicalCatalogRevision {
		t.Errorf("catalog revision = %q, want %q", reg.CatalogRevision(), compiler.CanonicalCatalogRevision)
	}
	if !strings.HasPrefix(reg.NormativeSourceDigest(), "sha256:") || len(reg.NormativeSourceDigest()) != 71 {
		t.Errorf("normative source digest invalid: %q", reg.NormativeSourceDigest())
	}
	if !strings.HasPrefix(reg.AuthorityProjectionDigest(), "sha256:") || len(reg.AuthorityProjectionDigest()) != 71 {
		t.Errorf("authority projection digest invalid: %q", reg.AuthorityProjectionDigest())
	}
	if !strings.HasPrefix(reg.CatalogDigest(), "sha256:") || len(reg.CatalogDigest()) != 71 {
		t.Errorf("catalog digest invalid: %q", reg.CatalogDigest())
	}

	// Verify 100% reverse coverage
	if err := reg.VerifyReverseCoverage(); err != nil {
		t.Errorf("expected 100%% reverse coverage across the canonical invariant mapping, got error: %v", err)
	}

	// Verify registry is frozen (cannot register new rule or modify metadata)
	err = reg.Register(compiler.Rule{
		ID:             "DCI-999",
		SourceKind:     compiler.AuthoritySourceKindSystem,
		AdmissionClass: compiler.AdmissionClassAlways,
		SourceDoc:      "test.md",
		Revision:       "v1.0",
		Content:        "Dummy content",
	})
	if err == nil || !errors.Is(err, errs.ErrConflict) {
		t.Errorf("expected ErrConflict registering on frozen registry, got: %v", err)
	}

	err = reg.SetCatalogMeta("new-id", "v2.0", "new-rev", "new-map-rev")
	if err == nil || !errors.Is(err, errs.ErrConflict) {
		t.Errorf("expected ErrConflict setting catalog meta on frozen registry, got: %v", err)
	}
}

func TestCatalogDigest_AuthenticatesMappingSemantics(t *testing.T) {
	// Baseline rule
	r1 := compiler.Rule{
		ID:             "DCI-030",
		SourceKind:     compiler.AuthoritySourceKindSystem,
		AdmissionClass: compiler.AdmissionClassCapabilityDefault,
		Capability:     "write",
		SourceDoc:      "INVARIANTS.md",
		Revision:       "v1.0",
		Content:        "Autonomous work is isolated in dedicated branches or worktrees.",
	}

	buildRegistry := func(r compiler.Rule) *compiler.RuleRegistry {
		reg := compiler.NewRuleRegistry()
		_ = reg.RegisterKnownDomain("test")
		if err := reg.Register(r); err != nil {
			t.Fatalf("register failed: %v", err)
		}
		_ = reg.SetCatalogMeta("test-cat", "1.0", "rev-1", "map-1")
		if err := reg.Freeze(); err != nil {
			t.Fatalf("freeze failed: %v", err)
		}
		return reg
	}

	baseReg := buildRegistry(r1)
	baseSrcDigest := baseReg.NormativeSourceDigest()
	baseProjDigest := baseReg.AuthorityProjectionDigest()
	baseCatDigest := baseReg.CatalogDigest()

	// 1. Changing AdmissionClass MUST change AuthorityProjectionDigest and CatalogDigest,
	// but NormativeSourceDigest MUST remain unchanged.
	rChangedClass := r1
	rChangedClass.AdmissionClass = compiler.AdmissionClassMapped
	rChangedClass.Domains = []string{"test"}
	regClass := buildRegistry(rChangedClass)

	if regClass.NormativeSourceDigest() != baseSrcDigest {
		t.Errorf("expected NormativeSourceDigest to be unchanged, got %s vs %s",
			regClass.NormativeSourceDigest(), baseSrcDigest)
	}
	if regClass.AuthorityProjectionDigest() == baseProjDigest {
		t.Errorf("expected AuthorityProjectionDigest to change when AdmissionClass changes")
	}
	if regClass.CatalogDigest() == baseCatDigest {
		t.Errorf("expected CatalogDigest to change when AdmissionClass changes")
	}

	// 2. Changing Capability MUST change AuthorityProjectionDigest and CatalogDigest.
	rChangedCap := r1
	rChangedCap.Capability = "credentials"
	regCap := buildRegistry(rChangedCap)

	if regCap.NormativeSourceDigest() != baseSrcDigest {
		t.Errorf("expected NormativeSourceDigest to be unchanged")
	}
	if regCap.AuthorityProjectionDigest() == baseProjDigest {
		t.Errorf("expected AuthorityProjectionDigest to change when Capability changes")
	}
	if regCap.CatalogDigest() == baseCatDigest {
		t.Errorf("expected CatalogDigest to change when Capability changes")
	}

	// 3. Adding Domains MUST change AuthorityProjectionDigest and CatalogDigest.
	rChangedDomain := r1
	rChangedDomain.Domains = []string{"test"}
	regDomain := buildRegistry(rChangedDomain)

	if regDomain.AuthorityProjectionDigest() == baseProjDigest {
		t.Errorf("expected AuthorityProjectionDigest to change when Domains change")
	}
	if regDomain.CatalogDigest() == baseCatDigest {
		t.Errorf("expected CatalogDigest to change when Domains change")
	}

	// 4. Changing rule content MUST change NormativeSourceDigest and CatalogDigest.
	rChangedContent := r1
	rChangedContent.Content = "Autonomous work is isolated in dedicated branches or worktrees with strict sandbox."
	rChangedContent.ContentDigest = "" // Will be recomputed
	regContent := buildRegistry(rChangedContent)

	if regContent.NormativeSourceDigest() == baseSrcDigest {
		t.Errorf("expected NormativeSourceDigest to change when Content changes")
	}
	if regContent.CatalogDigest() == baseCatDigest {
		t.Errorf("expected CatalogDigest to change when Content changes")
	}

	// 5. Changing MappingRevision MUST change AuthorityProjectionDigest and CatalogDigest,
	// while NormativeSourceDigest MUST remain unchanged (Pass 4 Finding 2).
	buildRegistryWithMappingRev := func(r compiler.Rule, mapRev string) *compiler.RuleRegistry {
		reg := compiler.NewRuleRegistry()
		_ = reg.RegisterKnownDomain("test")
		if err := reg.Register(r); err != nil {
			t.Fatalf("register failed: %v", err)
		}
		_ = reg.SetCatalogMeta("test-cat", "1.0", "rev-1", mapRev)
		if err := reg.Freeze(); err != nil {
			t.Fatalf("freeze failed: %v", err)
		}
		return reg
	}
	regMapRev := buildRegistryWithMappingRev(r1, "map-2")
	if regMapRev.NormativeSourceDigest() != baseSrcDigest {
		t.Errorf("expected NormativeSourceDigest to be unchanged when only MappingRevision changes")
	}
	if regMapRev.AuthorityProjectionDigest() == baseProjDigest {
		t.Errorf("expected AuthorityProjectionDigest to change when MappingRevision changes")
	}
	if regMapRev.CatalogDigest() == baseCatDigest {
		t.Errorf("expected CatalogDigest to change when MappingRevision changes")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
