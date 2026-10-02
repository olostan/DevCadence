package compiler_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

func setupTestRegistry(t *testing.T) *compiler.RuleRegistry {
	t.Helper()
	reg := compiler.NewRuleRegistry()

	// 1. Always rule
	err := reg.Register(compiler.Rule{
		ID:             "DCI-018",
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

func TestRuleRegistry_ValidationAndDuplicate(t *testing.T) {
	reg := compiler.NewRuleRegistry()

	// Missing ID
	err := reg.Register(compiler.Rule{
		AdmissionClass: compiler.AdmissionClassAlways,
		SourceDoc:      "doc",
		Revision:       "v1",
		Content:        "content",
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for missing ID, got %v", err)
	}

	// Invalid admission class
	err = reg.Register(compiler.Rule{
		ID:             "R1",
		AdmissionClass: "invalid_class",
		SourceDoc:      "doc",
		Revision:       "v1",
		Content:        "content",
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for invalid class, got %v", err)
	}

	// Capability default without capability
	err = reg.Register(compiler.Rule{
		ID:             "R2",
		AdmissionClass: compiler.AdmissionClassCapabilityDefault,
		SourceDoc:      "doc",
		Revision:       "v1",
		Content:        "content",
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for missing capability, got %v", err)
	}

	// Valid registration
	err = reg.Register(compiler.Rule{
		ID:             "R3",
		AdmissionClass: compiler.AdmissionClassAlways,
		SourceDoc:      "doc",
		Revision:       "v1",
		Content:        "content",
	})
	if err != nil {
		t.Fatalf("expected valid registration, got %v", err)
	}

	// Duplicate registration conflict
	err = reg.Register(compiler.Rule{
		ID:             "R3",
		AdmissionClass: compiler.AdmissionClassAlways,
		SourceDoc:      "doc",
		Revision:       "v1",
		Content:        "content",
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("expected ErrConflict for duplicate registration, got %v", err)
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
		reg := setupTestRegistry(t)
		// Register an orphan mapped rule with no triggers and no inbound dependencies
		err := reg.Register(compiler.Rule{
			ID:             "ORPHAN-001",
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
			Role: "implementer",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		foundAlways := false
		for _, r := range admitted {
			if r.ID == "DCI-018" {
				foundAlways = true
				break
			}
		}
		if !foundAlways {
			t.Fatalf("expected always rule DCI-018 to be admitted")
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
		foundWrite := false
		for _, r := range admitted {
			if r.ID == "DCI-010" {
				foundWrite = true
				break
			}
		}
		if !foundWrite {
			t.Fatalf("expected DCI-010 to be admitted for write capability")
		}
	})

	t.Run("excludes capability_default when explicitly excluded", func(t *testing.T) {
		admitted, err := reg.ResolveAdmittedRules(compiler.AdmissionParams{
			Role:                 "implementer",
			ActiveCapabilities:   []string{"write"},
			ExcludedCapabilities: []string{"write"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, r := range admitted {
			if r.ID == "DCI-010" {
				t.Fatalf("expected DCI-010 to be excluded when write capability is excluded")
			}
		}
	})

	t.Run("admits mapped rule and computes transitive dependency closure", func(t *testing.T) {
		// DCI-131 matches domain 'compiler' and depends on DCI-132.
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

func TestCompiler_CompileContextPackAndManifest(t *testing.T) {
	reg := setupTestRegistry(t)
	leaseMgr := compiler.NewEvidenceLeaseManager()
	capsuleMgr := compiler.NewCapsuleManager()
	c := compiler.NewCompiler(reg, leaseMgr, capsuleMgr)

	profile := compiler.DefaultProvisionalProfile("ep_1", "chan_1", "qwen2.5-coder", 32768)

	req := compiler.CompileRequest{
		TaskID:              "task-test-001",
		WorkPackageID:       "WP-M3C-TEST",
		WorkPackageRevision: 1,
		WorkPackageDigest:   "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		Role:                "implementer",
		BaseCommit:          "d99e40c79ebf3747b4d32a934446b3f9408e001c",
		ReadEnvelope:        []string{"internal/*"},
		WriteScope:          []string{"internal/cognition/*"},
		Domains:             []string{"cognition"},
		Action:              "Implement test package",
		ActiveCapabilities:  []string{"write"},
		ExecutionContract:   "Execute deterministic compilation without data loss.",
		ContextProfile:      profile,
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

	manifest, pack, err := c.Compile(context.Background(), req)
	if err != nil {
		t.Fatalf("expected successful compilation, got %v", err)
	}

	// Verify manifest
	if manifest == nil {
		t.Fatal("manifest is nil")
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("manifest failed protocol validation: %v", err)
	}
	if manifest.ManifestID != "manifest-task-test-001-rev1" {
		t.Errorf("manifest ID: got %q, want manifest-task-test-001-rev1", manifest.ManifestID)
	}

	// Verify pack
	if pack == nil {
		t.Fatal("pack is nil")
	}
	if err := pack.Validate(); err != nil {
		t.Fatalf("pack failed protocol validation: %v", err)
	}
	if pack.Status != protocol.PackStatusReady {
		t.Errorf("pack status: got %q, want ready", pack.Status)
	}
	if len(pack.NormativeClauses) == 0 {
		t.Fatal("expected admitted normative clauses, got 0")
	}

	// Reproducibility test: re-running with same inputs yields identical pack digest
	_, pack2, err2 := c.Compile(context.Background(), req)
	if err2 != nil {
		t.Fatalf("second compilation failed: %v", err2)
	}
	if pack.PackDigest != pack2.PackDigest {
		t.Fatalf("compilation not reproducible: pack1 digest %q != pack2 digest %q", pack.PackDigest, pack2.PackDigest)
	}
}

func TestCompiler_PolicyDeniedOnOutOfScopeLease(t *testing.T) {
	reg := setupTestRegistry(t)
	leaseMgr := compiler.NewEvidenceLeaseManager()
	capsuleMgr := compiler.NewCapsuleManager()
	c := compiler.NewCompiler(reg, leaseMgr, capsuleMgr)

	// Create lease for a file in "secrets/"
	lease, err := leaseMgr.CreateLease(compiler.CreateLeaseParams{
		EvidenceKind:        protocol.LeaseKindSourceSnippet,
		SourceRevision:      "d99e40c79ebf3747b4d32a934446b3f9408e001c",
		WorktreeID:          "wt_1",
		FilePath:            "secrets/keys.json",
		Locator:             "L1-L10",
		Content:             `{"secret": "val"}`,
		AcquisitionQuestion: "Get secrets?",
		AcquisitionReason:   "Checking keys",
	})
	if err != nil {
		t.Fatalf("failed to create lease: %v", err)
	}

	profile := compiler.DefaultProvisionalProfile("ep_1", "chan_1", "qwen", 32768)

	req := compiler.CompileRequest{
		TaskID:              "task-policy-test",
		WorkPackageID:       "WP-M3C-TEST",
		WorkPackageRevision: 1,
		Role:                "implementer",
		BaseCommit:          "d99e40c79ebf3747b4d32a934446b3f9408e001c",
		ReadEnvelope:        []string{"internal/*"}, // secrets/ is NOT in read envelope!
		WriteScope:          []string{"internal/*"},
		Domains:             []string{"cognition"},
		ExecutionContract:   "Check policy enforcement",
		ContextProfile:      profile,
		ActiveLeaseIDs:      []string{lease.LeaseID},
	}

	_, _, err = c.Compile(context.Background(), req)
	if err == nil {
		t.Fatal("expected policy denial for out-of-scope lease, got nil")
	}
	if !errors.Is(err, errs.ErrPolicyDenied) {
		t.Fatalf("expected ErrPolicyDenied, got %v", err)
	}
}

func TestCompiler_ContextUnfitOnBudgetExceeded(t *testing.T) {
	reg := setupTestRegistry(t)
	leaseMgr := compiler.NewEvidenceLeaseManager()
	capsuleMgr := compiler.NewCapsuleManager()
	c := compiler.NewCompiler(reg, leaseMgr, capsuleMgr)

	// Extremely tiny profile window
	tinyProfile := compiler.DefaultProvisionalProfile("ep_tiny", "chan_tiny", "tiny-model", 500)

	req := compiler.CompileRequest{
		TaskID:              "task-overflow-test",
		WorkPackageID:       "WP-M3C-TEST",
		WorkPackageRevision: 1,
		Role:                "implementer",
		BaseCommit:          "d99e40c79ebf3747b4d32a934446b3f9408e001c",
		ReadEnvelope:        []string{"internal/*"},
		WriteScope:          []string{"internal/*"},
		Domains:             []string{"cognition"},
		// Long contract that will exceed tiny profile
		ExecutionContract: strings.Repeat("Long contract requirement text exceeding tiny budget. ", 100),
		ContextProfile:    tinyProfile,
	}

	manifest, pack, err := c.Compile(context.Background(), req)
	if err == nil {
		t.Fatal("expected ContextUnfit error, got nil")
	}
	if !errors.Is(err, errs.ErrContextUnfit) {
		t.Fatalf("expected ErrContextUnfit, got %v", err)
	}

	// Invariant DCI-019 check:
	// Contract MUST NOT be truncated or dropped!
	if pack == nil {
		t.Fatal("pack was not returned with ContextUnfit")
	}
	if pack.Status != protocol.PackStatusContextUnfit {
		t.Errorf("pack status: got %q, want context_unfit", pack.Status)
	}
	if pack.ExecutionContract != req.ExecutionContract {
		t.Fatal("DCI-019 violation: ExecutionContract was truncated or modified to fit budget!")
	}
	if manifest == nil {
		t.Fatal("manifest was not returned with ContextUnfit")
	}
}
