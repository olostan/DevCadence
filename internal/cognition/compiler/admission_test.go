package compiler_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

const testBaseCommit = "d99e40c2107965b5af53a8c429aa6286f430ff8f"

func setupTestRegistryUnfrozen(t *testing.T) *compiler.RuleRegistry {
	t.Helper()
	reg := compiler.NewRuleRegistry()

	// 1. Always rule
	err := reg.Register(compiler.Rule{
		ID:             "DCI-018",
		SourceKind:     compiler.AuthoritySourceKindSystem,
		AdmissionClass: compiler.AdmissionClassAlways,
		SourceDoc:      "docs/INVARIANTS.md",
		Revision:       "v1.0",
		Content:        "Authority does not imply residency: do not preload the entire normative corpus.",
	})
	if err != nil {
		t.Fatalf("failed to register DCI-018: %v", err)
	}

	// 2. Capability default rule
	err = reg.Register(compiler.Rule{
		ID:             "DCI-010",
		SourceKind:     compiler.AuthoritySourceKindSystem,
		AdmissionClass: compiler.AdmissionClassCapabilityDefault,
		SourceDoc:      "docs/INVARIANTS.md",
		Revision:       "v1.0",
		Content:        "Repository mutations require strict containment to authorized worktree boundaries.",
		Capability:     "write",
	})
	if err != nil {
		t.Fatalf("failed to register DCI-010: %v", err)
	}

	// 3. Mapped rule (by domain)
	err = reg.Register(compiler.Rule{
		ID:             "DCI-131",
		SourceKind:     compiler.AuthoritySourceKindSystem,
		AdmissionClass: compiler.AdmissionClassMapped,
		SourceDoc:      "docs/INVARIANTS.md",
		Revision:       "v1.0",
		Content:        "Control-plane complexity does not imply prompt complexity.",
		Domains:        []string{"cognition", "compiler"},
		DependsOn:      []string{"DCI-132"},
	})
	if err != nil {
		t.Fatalf("failed to register DCI-131: %v", err)
	}

	// 4. Mapped rule (depended upon by DCI-131)
	err = reg.Register(compiler.Rule{
		ID:             "DCI-132",
		SourceKind:     compiler.AuthoritySourceKindSystem,
		AdmissionClass: compiler.AdmissionClassMapped,
		SourceDoc:      "docs/INVARIANTS.md",
		Revision:       "v1.0",
		Content:        "Mandatory applicability is never similarity-ranked away.",
		Domains:        []string{"cognition"},
	})
	if err != nil {
		t.Fatalf("failed to register DCI-132: %v", err)
	}

	// 5. Mapped rule by path
	err = reg.Register(compiler.Rule{
		ID:             "RULE-SETUP-DOCTOR",
		SourceKind:     compiler.AuthoritySourceKindSystem,
		AdmissionClass: compiler.AdmissionClassMapped,
		SourceDoc:      "docs/PROTOCOLS.md",
		Revision:       "v1.0",
		Content:        "Doctor diagnostics must be idempotent.",
		PathPatterns:   []string{"internal/setup/*"},
	})
	if err != nil {
		t.Fatalf("failed to register RULE-SETUP-DOCTOR: %v", err)
	}

	return reg
}

func setupTestRegistry(t *testing.T) *compiler.RuleRegistry {
	t.Helper()
	reg := setupTestRegistryUnfrozen(t)

	if err := reg.SetCatalogMeta("test_catalog", "v1.0", testBaseCommit, "v1.0"); err != nil {
		t.Fatalf("failed to set test catalog meta: %v", err)
	}
	if err := reg.Freeze(); err != nil {
		t.Fatalf("failed to freeze test registry: %v", err)
	}

	return reg
}

func mustNewCompiler(t *testing.T, registry *compiler.RuleRegistry, leaseMgr *compiler.EvidenceLeaseManager, capsuleMgr *compiler.CapsuleManager) *compiler.Compiler {
	t.Helper()
	c, err := compiler.NewCompiler(registry, leaseMgr, capsuleMgr)
	if err != nil {
		t.Fatalf("NewCompiler failed: %v", err)
	}
	return c
}

func validCompileRequest(profile *protocol.ContextProfile) compiler.CompileRequest {
	return compiler.CompileRequest{
		TaskID:               "task-test-001",
		WorkPackageID:        "WP-M3C-TEST",
		WorkPackageRevision:  1,
		WorkPackageDigest:    "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		Role:                 "implementer",
		BaseCommit:           testBaseCommit,
		SourceRevision:       testBaseCommit,
		ProjectStateRevision: "rev-bootstrap-001",
		MappingVersion:       "v1.0",
		BudgetPoolID:         "default_pool",
		ReadEnvelope:         []string{"internal/*"},
		WriteScope:           []string{"internal/cognition/*"},
		Domains:              []string{"cognition"},
		Action:               "Implement test package",
		ActiveCapabilities:   []string{"write"},
		ExecutionContract:    "Execute deterministic compilation without data loss.",
		ContextProfile:       profile,
		Assumptions: []protocol.Assumption{
			{
				ID:        "asm_1",
				Statement: "Base commit matches main",
				Status:    protocol.AssumptionVerified,
				Material:  true,
			},
		},
		ExplicitQuestions: []string{"Does pack compile cleanly?"},
	}
}

func TestRuleRegistry_ReverseCoverageValidation(t *testing.T) {
	t.Run("valid registry passes reverse coverage", func(t *testing.T) {
		reg := setupTestRegistry(t)
		if err := reg.ValidateReverseCoverage(); err != nil {
			t.Fatalf("expected valid registry to pass reverse coverage, got %v", err)
		}
	})

	t.Run("orphaned rule fails reverse coverage", func(t *testing.T) {
		reg := setupTestRegistryUnfrozen(t)
		err := reg.Register(compiler.Rule{
			ID:             "ORPHAN-001",
			SourceKind:     compiler.AuthoritySourceKindSystem,
			AdmissionClass: compiler.AdmissionClassMapped,
			SourceDoc:      "docs/INVARIANTS.md",
			Revision:       "v1.0",
			Content:        "An orphaned rule that no role, path, domain, or dependency ever admits.",
		})
		if err != nil {
			t.Fatalf("unexpected register error: %v", err)
		}

		covErr := reg.ValidateReverseCoverage()
		if covErr == nil {
			t.Fatalf("expected reverse-coverage validation to fail on orphan rule, got nil")
		}
		if !errors.Is(covErr, errs.ErrValidationFailed) {
			t.Fatalf("expected ErrValidationFailed, got %v", covErr)
		}
		if !strings.Contains(covErr.Error(), "ORPHAN-001") {
			t.Fatalf("expected error message to mention ORPHAN-001, got %v", covErr)
		}
	})
}

func TestRuleRegistry_ResolveAdmittedRules(t *testing.T) {
	reg := setupTestRegistry(t)

	t.Run("admits always rules unconditionally", func(t *testing.T) {
		admitted, err := reg.ResolveAdmittedRules(compiler.AdmissionParams{
			Role: "minimal_role",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		found := false
		for _, r := range admitted {
			if r.ID == "DCI-018" {
				found = true
				if !strings.Contains(r.SelectionRationale, "always authority floor") {
					t.Errorf("expected DCI-018 rationale to mention always authority floor, got %q", r.SelectionRationale)
				}
				break
			}
		}
		if !found {
			t.Fatalf("expected DCI-018 to be admitted unconditionally as always class")
		}
	})

	t.Run("admits capability_default when capability is active", func(t *testing.T) {
		admitted, err := reg.ResolveAdmittedRules(compiler.AdmissionParams{
			Role:               "implementer",
			ActiveCapabilities: []string{"write"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		found := false
		for _, r := range admitted {
			if r.ID == "DCI-010" {
				found = true
				if !strings.Contains(r.SelectionRationale, "active capability") {
					t.Errorf("expected DCI-010 rationale to mention active capability, got %q", r.SelectionRationale)
				}
				break
			}
		}
		if !found {
			t.Fatalf("expected DCI-010 to be admitted for active write capability")
		}
	})

	t.Run("excludes capability_default when explicitly excluded with typed decision", func(t *testing.T) {
		admitted, err := reg.ResolveAdmittedRules(compiler.AdmissionParams{
			Role: "implementer",
			CapabilityExclusions: []compiler.CapabilityExclusion{
				{
					Capability: "write",
					DecisionID: "DEC-TEST-001",
					Revision:   "v1.0",
					Rationale:  "Explicit read-only test run",
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, r := range admitted {
			if r.ID == "DCI-010" {
				t.Fatalf("expected DCI-010 to be excluded via CapabilityExclusion")
			}
		}
	})

	t.Run("contradictory active and excluded capability fails closed", func(t *testing.T) {
		_, err := reg.ResolveAdmittedRules(compiler.AdmissionParams{
			Role:               "implementer",
			ActiveCapabilities: []string{"write"},
			CapabilityExclusions: []compiler.CapabilityExclusion{
				{
					Capability: "write",
					DecisionID: "DEC-TEST-001",
					Revision:   "v1.0",
					Rationale:  "Contradictory exclusion",
				},
			},
		})
		if err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for contradictory active+excluded, got %v", err)
		}
	})

	t.Run("unprovenanced string exclusion fails closed", func(t *testing.T) {
		_, err := reg.ResolveAdmittedRules(compiler.AdmissionParams{
			Role:                 "implementer",
			ExcludedCapabilities: []string{"write"},
		})
		if err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for unprovenanced ExcludedCapabilities string, got %v", err)
		}
	})

	t.Run("unknown required domain fails closed", func(t *testing.T) {
		_, err := reg.ResolveAdmittedRules(compiler.AdmissionParams{
			Role:    "implementer",
			Domains: []string{"unknown_domain_xyz"},
		})
		if err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for unknown domain, got %v", err)
		}
	})

	t.Run("admits mapped rule and computes transitive dependency closure", func(t *testing.T) {
		admitted, err := reg.ResolveAdmittedRules(compiler.AdmissionParams{
			Role:    "implementer",
			Domains: []string{"compiler"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		has131 := false
		has132 := false
		for _, r := range admitted {
			if r.ID == "DCI-131" {
				has131 = true
			}
			if r.ID == "DCI-132" {
				has132 = true
				if !strings.Contains(r.SelectionRationale, "transitive dependency") {
					t.Errorf("expected DCI-132 rationale to mention transitive dependency, got %q", r.SelectionRationale)
				}
			}
		}
		if !has131 {
			t.Fatalf("expected DCI-131 to be admitted for domain 'compiler'")
		}
		if !has132 {
			t.Fatalf("expected DCI-132 to be transitively admitted via dependency closure")
		}
	})

	t.Run("admits rule by path pattern", func(t *testing.T) {
		admitted, err := reg.ResolveAdmittedRules(compiler.AdmissionParams{
			Role:  "implementer",
			Paths: []string{"internal/setup/doctor.go"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		hasDoctorRule := false
		for _, r := range admitted {
			if r.ID == "RULE-SETUP-DOCTOR" {
				hasDoctorRule = true
				break
			}
		}
		if !hasDoctorRule {
			t.Fatalf("expected RULE-SETUP-DOCTOR to be admitted for path internal/setup/doctor.go")
		}
	})

	t.Run("fails closed on missing explicit rule ID", func(t *testing.T) {
		_, err := reg.ResolveAdmittedRules(compiler.AdmissionParams{
			Role:            "implementer",
			ExplicitRuleIDs: []string{"NON_EXISTENT_RULE"},
		})
		if !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("expected ErrNotFound for non-existent explicit rule ID, got %v", err)
		}
	})
}

func TestCanonicalRuleRegistry(t *testing.T) {
	reg, err := compiler.NewCanonicalRuleRegistry()
	if err != nil {
		t.Fatalf("failed to create canonical rule registry: %v", err)
	}

	if !reg.IsFrozen() {
		t.Error("expected canonical rule registry to be frozen")
	}

	// Verify reverse-coverage passed
	if err := reg.ValidateReverseCoverage(); err != nil {
		t.Fatalf("canonical registry failed reverse coverage: %v", err)
	}

	// Verify core rules are present
	expectedRules := []string{"DCI-018", "DCI-019", "DCI-030", "DCI-031", "DCI-033", "DCI-131", "DCI-132", "DCI-133"}
	for _, id := range expectedRules {
		if _, ok := reg.Get(id); !ok {
			t.Errorf("canonical registry missing expected rule %s", id)
		}
	}

	// Test resolving canonical rules for a principal engineer on compiler domain
	admitted, err := reg.ResolveAdmittedRules(compiler.AdmissionParams{
		Role:               "principal_engineer",
		Domains:            []string{"compiler", "discovery_and_specification"},
		ActiveCapabilities: []string{"write"},
	})
	if err != nil {
		t.Fatalf("failed to resolve canonical rules: %v", err)
	}

	foundWrite := false
	foundPrincipal := false
	for _, r := range admitted {
		if r.ID == "DCI-030" {
			foundWrite = true
		}
		if r.ID == "DCI-004" {
			foundPrincipal = true
		}
		if r.SelectionRationale == "" {
			t.Errorf("missing SelectionRationale on %s", r.ID)
		}
	}
	if !foundWrite {
		t.Error("expected DCI-030 admitted for active write capability")
	}
	if !foundPrincipal {
		t.Error("expected DCI-004 admitted for principal_engineer role")
	}
}
