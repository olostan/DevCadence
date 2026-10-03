package compiler_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

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

func TestRuleRegistry_ValidationAndDuplicate(t *testing.T) {
	reg := compiler.NewRuleRegistry()

	// Missing or invalid SourceKind
	err := reg.Register(compiler.Rule{
		ID:             "R_NO_KIND",
		AdmissionClass: compiler.AdmissionClassAlways,
		SourceDoc:      "doc",
		Revision:       "v1",
		Content:        "content",
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for missing SourceKind, got %v", err)
	}

	// Missing ID
	err = reg.Register(compiler.Rule{
		SourceKind:     compiler.AuthoritySourceKindSystem,
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
		SourceKind:     compiler.AuthoritySourceKindSystem,
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
		SourceKind:     compiler.AuthoritySourceKindSystem,
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
		SourceKind:     compiler.AuthoritySourceKindSystem,
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
		SourceKind:     compiler.AuthoritySourceKindSystem,
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

func TestRuleRegistry_DeepCopyImmutability(t *testing.T) {
	reg := compiler.NewRuleRegistry()

	domains := []string{"cognition"}
	roles := []string{"implementer"}
	dependsOn := []string{"DEP-1"}

	err := reg.Register(compiler.Rule{
		ID:             "RULE-IMMUTABLE",
		SourceKind:     compiler.AuthoritySourceKindSystem,
		AdmissionClass: compiler.AdmissionClassMapped,
		SourceDoc:      "docs/INVARIANTS.md",
		Revision:       "v1.0",
		Content:        "Rule testing slice immutability.",
		Domains:        domains,
		Roles:          roles,
		DependsOn:      dependsOn,
	})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	// Mutate caller slices
	domains[0] = "mutated_domain"
	roles[0] = "mutated_role"
	dependsOn[0] = "mutated_dep"

	r, ok := reg.Get("RULE-IMMUTABLE")
	if !ok {
		t.Fatal("rule not found")
	}

	if r.Domains[0] != "cognition" {
		t.Errorf("Domains slice was mutated in backing storage! got %q, want cognition", r.Domains[0])
	}
	if r.Roles[0] != "implementer" {
		t.Errorf("Roles slice was mutated in backing storage! got %q, want implementer", r.Roles[0])
	}
	if r.DependsOn[0] != "DEP-1" {
		t.Errorf("DependsOn slice was mutated in backing storage! got %q, want DEP-1", r.DependsOn[0])
	}

	// Mutate returned slice
	r.Domains[0] = "another_mutation"
	r2, _ := reg.Get("RULE-IMMUTABLE")
	if r2.Domains[0] != "cognition" {
		t.Errorf("Get() exposed internal slice to mutation! got %q, want cognition", r2.Domains[0])
	}
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
				if r.SelectionRationale == "" {
					t.Errorf("expected SelectionRationale to be populated for %s", r.ID)
				}
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
				if !strings.Contains(r.SelectionRationale, "write") {
					t.Errorf("expected SelectionRationale to mention write, got %q", r.SelectionRationale)
				}
				break
			}
		}
		if !foundWrite {
			t.Fatalf("expected DCI-010 to be admitted for write capability")
		}
	})

	t.Run("excludes capability_default when explicitly excluded with typed decision", func(t *testing.T) {
		admitted, err := reg.ResolveAdmittedRules(compiler.AdmissionParams{
			Role:               "implementer",
			ActiveCapabilities: nil, // NOT active
			CapabilityExclusions: []compiler.CapabilityExclusion{
				{
					Capability: "write",
					DecisionID: "DEC-NO-WRITE",
					Revision:   "rev-1",
					Rationale:  "Read-only query invocation",
				},
			},
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

	t.Run("contradictory active and excluded capability fails closed (Finding 1)", func(t *testing.T) {
		_, err := reg.ResolveAdmittedRules(compiler.AdmissionParams{
			Role:               "implementer",
			ActiveCapabilities: []string{"write"},
			CapabilityExclusions: []compiler.CapabilityExclusion{
				{
					Capability: "write",
					DecisionID: "DEC-WRITE-002",
					Revision:   "rev-1",
					Rationale:  "Contradictory exclusion",
				},
			},
		})
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for contradictory active and excluded capability, got %v", err)
		}
	})

	t.Run("unprovenanced string exclusion fails closed (Finding 1)", func(t *testing.T) {
		_, err := reg.ResolveAdmittedRules(compiler.AdmissionParams{
			Role:                 "implementer",
			ExcludedCapabilities: []string{"write"},
		})
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for unprovenanced ExcludedCapabilities, got %v", err)
		}
	})

	t.Run("unknown required domain fails closed (Finding 2)", func(t *testing.T) {
		_, err := reg.ResolveAdmittedRules(compiler.AdmissionParams{
			Role:    "implementer",
			Domains: []string{"unmapped_alien_domain"},
		})
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Fatalf("expected ErrInvalidArgument for unmapped required domain, got %v", err)
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

func TestCompiler_CompileContextPackAndManifest(t *testing.T) {
	reg := setupTestRegistry(t)
	leaseMgr := compiler.NewEvidenceLeaseManager()
	capsuleMgr := compiler.NewCapsuleManager()
	c := mustNewCompiler(t, reg, leaseMgr, capsuleMgr)

	profile := compiler.MustDefaultProvisionalProfile("ep_1", "chan_1", "qwen2.5-coder", 32768)
	req := validCompileRequest(profile)

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
	if len(manifest.MandatoryClauses) == 0 {
		t.Fatal("expected mandatory clause references in manifest")
	}
	for _, mc := range manifest.MandatoryClauses {
		if mc.SelectionRationale == "" {
			t.Errorf("expected SelectionRationale on mandatory clause %s in manifest (Finding 3)", mc.ClauseID)
		}
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

	// True Invocation Identity (Finding 10):
	// Modifying Action changes the pack digest
	reqModifiedAction := req
	reqModifiedAction.Action = "Different action"
	_, packAction, errAction := c.Compile(context.Background(), reqModifiedAction)
	if errAction != nil {
		t.Fatalf("compilation with modified action failed: %v", errAction)
	}
	if pack.PackDigest == packAction.PackDigest {
		t.Errorf("PackDigest failed to change when CurrentAction changed (Finding 10)")
	}

	// Modifying CognitiveState changes the pack digest
	capsuleMgr.AddHypothesis("H_new: Thread safe check")
	_, packCapsule, errCapsule := c.Compile(context.Background(), req)
	if errCapsule != nil {
		t.Fatalf("compilation with modified capsule failed: %v", errCapsule)
	}
	if pack.PackDigest == packCapsule.PackDigest {
		t.Errorf("PackDigest failed to change when CognitiveState changed (Finding 10)")
	}
}

func TestCompiler_PolicyDeniedOnOutOfScopeLease(t *testing.T) {
	reg := setupTestRegistry(t)
	leaseMgr := compiler.NewEvidenceLeaseManager()
	capsuleMgr := compiler.NewCapsuleManager()
	c := mustNewCompiler(t, reg, leaseMgr, capsuleMgr)

	lease, err := leaseMgr.CreateLease(compiler.CreateLeaseParams{
		EvidenceKind:        protocol.LeaseKindSourceSnippet,
		SourceRevision:      testBaseCommit,
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

	profile := compiler.MustDefaultProvisionalProfile("ep_1", "chan_1", "qwen", 32768)
	req := validCompileRequest(profile)
	req.ReadEnvelope = []string{"internal/*"} // secrets/ is NOT in read envelope!
	req.ActiveLeaseIDs = []string{lease.LeaseID}

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
	c := mustNewCompiler(t, reg, leaseMgr, capsuleMgr)

	tinyProfile := compiler.MustDefaultProvisionalProfile("ep_tiny", "chan_tiny", "tiny-model", 500)
	req := validCompileRequest(tinyProfile)
	req.ContextProfile = tinyProfile
	req.ExecutionContract = strings.Repeat("Long contract requirement text exceeding tiny budget. ", 100)

	manifest, pack, err := c.Compile(context.Background(), req)
	if err == nil {
		t.Fatal("expected ContextUnfit error, got nil")
	}
	if !errors.Is(err, errs.ErrContextUnfit) {
		t.Fatalf("expected ErrContextUnfit, got %v", err)
	}

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

func TestCompiler_RejectStaleOrInvalidatedLease(t *testing.T) {
	reg := setupTestRegistry(t)
	leaseMgr := compiler.NewEvidenceLeaseManager()
	capsuleMgr := compiler.NewCapsuleManager()
	c := mustNewCompiler(t, reg, leaseMgr, capsuleMgr)

	profile := compiler.MustDefaultProvisionalProfile("ep_1", "chan_1", "qwen", 32768)

	// Create and then invalidate lease
	lease, err := leaseMgr.CreateLease(compiler.CreateLeaseParams{
		EvidenceKind:        protocol.LeaseKindSourceSnippet,
		SourceRevision:      testBaseCommit,
		WorktreeID:          "wt_1",
		FilePath:            "internal/setup/doctor.go",
		Locator:             "L1-L10",
		Content:             "func Test() {}",
		AcquisitionQuestion: "test",
		AcquisitionReason:   "test",
		ReadEnvelope:        []string{"internal/*"},
	})
	if err != nil {
		t.Fatalf("failed to create lease: %v", err)
	}

	leaseMgr.InvalidateForFileMutation("internal/setup/doctor.go")

	req := validCompileRequest(profile)
	req.ActiveLeaseIDs = []string{lease.LeaseID}

	_, _, compileErr := c.Compile(context.Background(), req)
	if compileErr == nil {
		t.Fatal("expected Compile to fail closed on invalidated lease (Finding 7), got nil")
	}
	if !errors.Is(compileErr, errs.ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed for invalidated lease, got %v", compileErr)
	}
}

func TestCompiler_RejectExpiredLease(t *testing.T) {
	reg := setupTestRegistry(t)
	leaseMgr := compiler.NewEvidenceLeaseManager()
	capsuleMgr := compiler.NewCapsuleManager()
	c := mustNewCompiler(t, reg, leaseMgr, capsuleMgr)

	profile := compiler.MustDefaultProvisionalProfile("ep_1", "chan_1", "qwen", 32768)

	// Create lease with imminent expiry timestamp and sleep past it
	exp := time.Now().UTC().Add(10 * time.Millisecond).Format(time.RFC3339Nano)
	lease, err := leaseMgr.CreateLease(compiler.CreateLeaseParams{
		EvidenceKind:        protocol.LeaseKindSourceSnippet,
		SourceRevision:      testBaseCommit,
		WorktreeID:          "wt_1",
		FilePath:            "internal/setup/doctor.go",
		Locator:             "L1-L10",
		Content:             "func Test() {}",
		AcquisitionQuestion: "test",
		AcquisitionReason:   "test",
		ReadEnvelope:        []string{"internal/*"},
		ExpiresAt:           &exp,
	})
	if err != nil {
		t.Fatalf("failed to create lease: %v", err)
	}

	time.Sleep(25 * time.Millisecond)

	req := validCompileRequest(profile)
	req.ActiveLeaseIDs = []string{lease.LeaseID}

	_, _, compileErr := c.Compile(context.Background(), req)
	if compileErr == nil {
		t.Fatal("expected Compile to fail closed on expired lease (Finding 7), got nil")
	}
	if !errors.Is(compileErr, errs.ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed for expired lease, got %v", compileErr)
	}
}

func TestCompiler_RejectMismatchedSourceRevision(t *testing.T) {
	reg := setupTestRegistry(t)
	leaseMgr := compiler.NewEvidenceLeaseManager()
	capsuleMgr := compiler.NewCapsuleManager()
	c := mustNewCompiler(t, reg, leaseMgr, capsuleMgr)

	profile := compiler.MustDefaultProvisionalProfile("ep_1", "chan_1", "qwen", 32768)

	lease, err := leaseMgr.CreateLease(compiler.CreateLeaseParams{
		EvidenceKind:        protocol.LeaseKindSourceSnippet,
		SourceRevision:      "different_revision_12345678",
		WorktreeID:          "wt_1",
		FilePath:            "internal/setup/doctor.go",
		Locator:             "L1-L10",
		Content:             "func Test() {}",
		AcquisitionQuestion: "test",
		AcquisitionReason:   "test",
		ReadEnvelope:        []string{"internal/*"},
	})
	if err != nil {
		t.Fatalf("failed to create lease: %v", err)
	}

	req := validCompileRequest(profile)
	req.ActiveLeaseIDs = []string{lease.LeaseID}

	_, _, compileErr := c.Compile(context.Background(), req)
	if compileErr == nil {
		t.Fatal("expected Compile to fail closed on mismatched source revision (Finding 7), got nil")
	}
	if !errors.Is(compileErr, errs.ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed for mismatched revision, got %v", compileErr)
	}
}

func TestCompiler_FailClosedOnMissingProvenance(t *testing.T) {
	reg := setupTestRegistry(t)
	leaseMgr := compiler.NewEvidenceLeaseManager()
	capsuleMgr := compiler.NewCapsuleManager()
	c := mustNewCompiler(t, reg, leaseMgr, capsuleMgr)

	profile := compiler.MustDefaultProvisionalProfile("ep_1", "chan_1", "qwen", 32768)

	// Missing WorkPackageDigest
	reqNoDigest := validCompileRequest(profile)
	reqNoDigest.WorkPackageDigest = ""
	if _, _, err := c.Compile(context.Background(), reqNoDigest); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for missing WorkPackageDigest, got %v", err)
	}

	// Missing BudgetPoolID
	reqNoPool := validCompileRequest(profile)
	reqNoPool.BudgetPoolID = ""
	if _, _, err := c.Compile(context.Background(), reqNoPool); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for missing BudgetPoolID, got %v", err)
	}

	// Mismatched MappingVersion fails closed; omitted MappingVersion derives from registry (Pass 4 Finding 3)
	reqMismatchedMap := validCompileRequest(profile)
	reqMismatchedMap.MappingVersion = "v999.0"
	if _, _, err := c.Compile(context.Background(), reqMismatchedMap); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for mismatched MappingVersion, got %v", err)
	}

	reqOmittedMap := validCompileRequest(profile)
	reqOmittedMap.MappingVersion = ""
	if m, _, err := c.Compile(context.Background(), reqOmittedMap); err != nil {
		t.Errorf("expected Compile to succeed with omitted MappingVersion, got %v", err)
	} else if m.MappingVersion != reg.MappingRevision() {
		t.Errorf("expected derived MappingVersion %q, got %q", reg.MappingRevision(), m.MappingVersion)
	}

	// Missing SourceRevision
	reqNoRev := validCompileRequest(profile)
	reqNoRev.SourceRevision = ""
	if _, _, err := c.Compile(context.Background(), reqNoRev); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for missing SourceRevision, got %v", err)
	}

	// Missing ProjectStateRevision
	reqNoState := validCompileRequest(profile)
	reqNoState.ProjectStateRevision = ""
	if _, _, err := c.Compile(context.Background(), reqNoState); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for missing ProjectStateRevision, got %v", err)
	}
}

func TestCompiler_EnforceProjectionBounds(t *testing.T) {
	reg := setupTestRegistry(t)
	leaseMgr := compiler.NewEvidenceLeaseManager()
	capsuleMgr := compiler.NewCapsuleManager()
	c := mustNewCompiler(t, reg, leaseMgr, capsuleMgr)

	// Profile with tight ceiling
	tightProfile := compiler.MustDefaultProvisionalProfile("ep_tight", "chan_tight", "model", 2000)

	req := validCompileRequest(tightProfile)
	req.ContextProfile = tightProfile
	// Add huge tool schemas to exceed projection fit (Finding 4)
	req.ToolSchemas = []string{
		fmt.Sprintf(`{"name": "large_tool", "description": %q}`, strings.Repeat("tool schema declaration description parameters ", 200)),
	}
	req.DeclaredTools = []compiler.ToolCapabilityInfo{
		{Name: "large_tool", ReadOnly: true},
	}

	_, pack, err := c.Compile(context.Background(), req)
	if err == nil {
		t.Fatal("expected Compile to fail closed when tool schemas exceed projection bounds, got nil")
	}
	if !errors.Is(err, errs.ErrContextUnfit) {
		t.Fatalf("expected ErrContextUnfit for projection overflow, got %v", err)
	}
	if pack != nil && pack.Status != protocol.PackStatusContextUnfit {
		t.Errorf("pack status: got %q, want context_unfit", pack.Status)
	}
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

func TestCompiler_InvalidWorkPackageRevisionFailsClosed(t *testing.T) {
	reg := setupTestRegistry(t)
	c := mustNewCompiler(t, reg, nil, nil)
	profile := compiler.MustDefaultProvisionalProfile("ep_1", "chan_1", "model", 32768)

	req0 := validCompileRequest(profile)
	req0.WorkPackageRevision = 0
	if _, _, err := c.Compile(context.Background(), req0); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for WorkPackageRevision=0, got %v", err)
	}

	reqNeg := validCompileRequest(profile)
	reqNeg.WorkPackageRevision = -2
	if _, _, err := c.Compile(context.Background(), reqNeg); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for WorkPackageRevision=-2, got %v", err)
	}
}

func TestCompiler_CognitiveStateExpiredDependencyRejection(t *testing.T) {
	reg := setupTestRegistry(t)
	leaseMgr := compiler.NewEvidenceLeaseManager()
	capsuleMgr := compiler.NewCapsuleManager()
	c := mustNewCompiler(t, reg, leaseMgr, capsuleMgr)
	profile := compiler.MustDefaultProvisionalProfile("ep_1", "chan_1", "model", 32768)

	// Create a lease that expires in 10ms
	exp := time.Now().UTC().Add(10 * time.Millisecond).Format(time.RFC3339Nano)
	lease, err := leaseMgr.CreateLease(compiler.CreateLeaseParams{
		EvidenceKind:        protocol.LeaseKindSourceSnippet,
		SourceRevision:      testBaseCommit,
		WorktreeID:          "wt_1",
		FilePath:            "internal/setup/doctor.go",
		Locator:             "L1-L10",
		AcquisitionQuestion: "Check?",
		AcquisitionReason:   "Safety",
		Content:             "func check() {}",
		AccountingMethod:    protocol.AccountingApproximateEstimate,
		ExpiresAt:           &exp,
	})
	if err != nil {
		t.Fatalf("failed to create lease: %v", err)
	}

	// Add lease as dependency in cognitive state
	capsuleMgr.TrackEvidenceDependency(lease.LeaseID)

	// Sleep 25ms to ensure lease is expired
	time.Sleep(25 * time.Millisecond)

	req := validCompileRequest(profile)
	// ActiveLeaseIDs does NOT include the lease, but cognitive state does!
	req.ActiveLeaseIDs = nil

	_, _, err = c.Compile(context.Background(), req)
	if err == nil {
		t.Fatal("expected failure on expired cognitive state dependency, got nil")
	}
	if errs.CategoryOf(err) != errs.CategoryValidationFailed {
		t.Errorf("expected CategoryValidationFailed, got %v", err)
	}
}

func TestCompiler_TypedCapabilitiesDerivation(t *testing.T) {
	// 1. Tool declaration with empty capabilities and ReadOnly == false MUST fail closed (Pass 4 Finding 1A)
	toolsNoMeta := []compiler.ToolCapabilityInfo{
		{Name: "bash"},
		{Name: "edit_file"},
	}
	_, err := compiler.DeriveActiveCapabilities(nil, toolsNoMeta, nil)
	if err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for tools with empty capabilities and ReadOnly == false, got %v", err)
	}

	// ReadOnly tools without privileged capabilities succeed and yield 'read_only'
	toolsReadOnly := []compiler.ToolCapabilityInfo{
		{Name: "fetch_info", ReadOnly: true},
	}
	capsRO, err := compiler.DeriveActiveCapabilities(nil, toolsReadOnly, nil)
	if err != nil {
		t.Fatalf("unexpected error deriving capabilities for read-only tool: %v", err)
	}
	if len(capsRO) != 1 || capsRO[0] != "read_only" {
		t.Errorf("expected ['read_only'], got %v", capsRO)
	}

	// 2. Typed annotations grant exact capabilities and normalize aliases
	toolsTyped := []compiler.ToolCapabilityInfo{
		{Name: "bash", RequiredCapabilities: []compiler.CapabilityClass{compiler.CapabilityClassExec}},
		{Name: "editor", MutatesFiles: true},
	}
	caps, err := compiler.DeriveActiveCapabilities(nil, toolsTyped, []string{"network"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := map[string]bool{"exec": true, "write": true, "filesystem": true, "network": true}
	for _, c := range caps {
		if !expected[c] {
			t.Errorf("unexpected capability derived: %q", c)
		}
	}
	if len(caps) != 4 {
		t.Errorf("expected 4 capabilities (exec, write, filesystem, network), got %v", caps)
	}

	// 3. Unrecognized capability string fails closed
	_, err = compiler.DeriveActiveCapabilities(nil, toolsTyped, []string{"unrecognized_magic_capability"})
	if err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for unrecognized capability string, got %v", err)
	}
}

func TestCompiler_InvocationDigestVsPackDigest(t *testing.T) {
	reg := setupTestRegistry(t)
	c := mustNewCompiler(t, reg, nil, nil)
	profile := compiler.MustDefaultProvisionalProfile("ep_1", "chan_1", "model", 32768)

	req1 := validCompileRequest(profile)
	req1.Renderer = compiler.NewTaggedMarkdownRenderer()

	req2 := validCompileRequest(profile)
	req2.Renderer = compiler.NewJSONRenderer()

	_, pack1, err1 := c.Compile(context.Background(), req1)
	if err1 != nil {
		t.Fatalf("compile 1 failed: %v", err1)
	}

	_, pack2, err2 := c.Compile(context.Background(), req2)
	if err2 != nil {
		t.Fatalf("compile 2 failed: %v", err2)
	}

	// Semantic PackDigest must be identical (same semantic content)
	if pack1.PackDigest != pack2.PackDigest {
		t.Errorf("expected identical PackDigest across renderers, got %q vs %q", pack1.PackDigest, pack2.PackDigest)
	}

	// True InvocationDigest must differ (different endpoint projection)
	if pack1.InvocationDigest == pack2.InvocationDigest {
		t.Errorf("expected different InvocationDigest across renderers, got identical %q", pack1.InvocationDigest)
	}

	// Changing tool schemas must alter InvocationDigest while preserving PackDigest
	req3 := validCompileRequest(profile)
	req3.Renderer = compiler.NewTaggedMarkdownRenderer()
	req3.ToolSchemas = []string{`{"type": "function", "name": "do_task"}`}
	req3.DeclaredTools = []compiler.ToolCapabilityInfo{{Name: "do_task", ReadOnly: true}}

	_, pack3, err3 := c.Compile(context.Background(), req3)
	if err3 != nil {
		t.Fatalf("compile 3 failed: %v", err3)
	}

	if pack1.PackDigest != pack3.PackDigest {
		t.Errorf("expected identical PackDigest when only tool schemas differ, got %q vs %q", pack1.PackDigest, pack3.PackDigest)
	}
	if pack1.InvocationDigest == pack3.InvocationDigest {
		t.Errorf("expected different InvocationDigest when tool schemas differ, got identical %q", pack1.InvocationDigest)
	}
}

func TestNewCompiler_RequiresFrozenAndIdentifiedRegistry(t *testing.T) {
	// 1. Nil registry
	if _, err := compiler.NewCompiler(nil, nil, nil); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for nil registry, got %v", err)
	}

	// 2. Mutable (unfrozen) registry
	unfrozenReg := compiler.NewRuleRegistry()
	if _, err := compiler.NewCompiler(unfrozenReg, nil, nil); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for unfrozen registry, got %v", err)
	}

	// 3. Missing catalog ID
	regNoID := compiler.NewRuleRegistry()
	_ = regNoID.SetCatalogMeta("", "1.0", "rev", "map")
	_ = regNoID.Freeze()
	if _, err := compiler.NewCompiler(regNoID, nil, nil); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for empty catalog ID, got %v", err)
	}

	// 4. Missing catalog revision
	regNoRev := compiler.NewRuleRegistry()
	_ = regNoRev.SetCatalogMeta("id", "1.0", "", "map")
	_ = regNoRev.Freeze()
	if _, err := compiler.NewCompiler(regNoRev, nil, nil); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for empty catalog revision, got %v", err)
	}
}

func TestCompiler_JSONRenderer_InvocationDigestSelfConsistency(t *testing.T) {
	// Regression test for Finding 4:
	// Compile with JSON renderer -> re-render returned pack -> assert actual invocation projection == projection measured and hashed
	reg := setupTestRegistry(t)
	c := mustNewCompiler(t, reg, nil, nil)
	profile := compiler.MustDefaultProvisionalProfile("ep_1", "chan_1", "model", 32768)

	req := validCompileRequest(profile)
	jsonRenderer := compiler.NewJSONRenderer()
	req.Renderer = jsonRenderer

	inv, err := c.CompileInvocation(context.Background(), req)
	if err != nil {
		t.Fatalf("compile invocation failed: %v", err)
	}

	measuredProj := inv.Projection

	// Now re-render the returned pack (which has pack.InvocationDigest set)
	reRenderedProj, err := jsonRenderer.Render(inv.Pack)
	if err != nil {
		t.Fatalf("re-render failed: %v", err)
	}

	if reRenderedProj.UserPrompt != measuredProj.UserPrompt {
		t.Errorf("re-rendered UserPrompt differs from measured prompt projection!\nMeasured:\n%s\nRe-rendered:\n%s",
			measuredProj.UserPrompt, reRenderedProj.UserPrompt)
	}
	if reRenderedProj.Digest != measuredProj.Digest {
		t.Errorf("re-rendered Digest %q differs from measured Digest %q",
			reRenderedProj.Digest, measuredProj.Digest)
	}
}

func TestCompiler_UnknownToolAuthority_FailsClosed(t *testing.T) {
	reg := setupTestRegistry(t)
	c := mustNewCompiler(t, reg, nil, nil)
	profile := compiler.MustDefaultProvisionalProfile("ep_1", "chan_1", "model", 32768)

	// 1. Tool schemas provided without declared tools fails closed (Finding 5)
	reqNoDeclared := validCompileRequest(profile)
	reqNoDeclared.ToolSchemas = []string{`{"name": "fetch"}`}
	reqNoDeclared.DeclaredTools = nil
	if _, _, err := c.Compile(context.Background(), reqNoDeclared); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument when tool schemas are provided without DeclaredTools, got %v", err)
	}

	// 2. Legacy req.Tools with undeclared tool fails closed
	reqUndeclaredLegacy := validCompileRequest(profile)
	reqUndeclaredLegacy.Tools = []string{"legacy_untyped_tool"}
	reqUndeclaredLegacy.DeclaredTools = []compiler.ToolCapabilityInfo{{Name: "other_tool", ReadOnly: true}}
	if _, _, err := c.Compile(context.Background(), reqUndeclaredLegacy); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for undeclared legacy tool, got %v", err)
	}

	// 3. Tool schema with unrelated declaration fails closed (Pass 4 Finding 1B)
	reqUnrelated := validCompileRequest(profile)
	reqUnrelated.ToolSchemas = []string{`{"name": "powerful_tool"}`}
	reqUnrelated.DeclaredTools = []compiler.ToolCapabilityInfo{{Name: "unrelated_tool", ReadOnly: true}}
	if _, _, err := c.Compile(context.Background(), reqUnrelated); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for tool schema with unrelated declaration, got %v", err)
	}

	// 4. Declared tool without matching tool schema fails closed (Pass 4 Finding 1B)
	reqExtraDecl := validCompileRequest(profile)
	reqExtraDecl.ToolSchemas = []string{`{"name": "tool_a"}`}
	reqExtraDecl.DeclaredTools = []compiler.ToolCapabilityInfo{
		{Name: "tool_a", ReadOnly: true},
		{Name: "tool_b", ReadOnly: true},
	}
	if _, _, err := c.Compile(context.Background(), reqExtraDecl); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for extra tool declaration without schema, got %v", err)
	}

	// 5. Mixed modern ToolSchemas and legacy Tools fails closed
	reqMixed := validCompileRequest(profile)
	reqMixed.ToolSchemas = []string{`{"name": "safe_tool"}`}
	reqMixed.Tools = []string{"untyped_legacy_tool"}
	reqMixed.DeclaredTools = []compiler.ToolCapabilityInfo{
		{Name: "safe_tool", ReadOnly: true},
	}
	if _, _, err := c.Compile(context.Background(), reqMixed); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for mixed ToolSchemas and legacy Tools, got %v", err)
	}
}

func TestToolCapabilityInfo_FailClosedValidation(t *testing.T) {
	// Empty capabilities and ReadOnly == false fails closed (Pass 4 Finding 1A)
	t1 := compiler.ToolCapabilityInfo{Name: "tool1"}
	if err := t1.Validate(); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for empty capabilities with ReadOnly == false, got %v", err)
	}

	// ReadOnly: true without capabilities is valid
	t2 := compiler.ToolCapabilityInfo{Name: "tool2", ReadOnly: true}
	if err := t2.Validate(); err != nil {
		t.Errorf("expected valid for ReadOnly: true tool, got %v", err)
	}

	// ReadOnly: true with MutatesFiles: true fails closed
	t3 := compiler.ToolCapabilityInfo{Name: "tool3", ReadOnly: true, MutatesFiles: true}
	if err := t3.Validate(); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for ReadOnly: true + MutatesFiles: true, got %v", err)
	}

	// ReadOnly: true with privileged capability fails closed
	t4 := compiler.ToolCapabilityInfo{
		Name:                 "tool4",
		ReadOnly:             true,
		RequiredCapabilities: []compiler.CapabilityClass{compiler.CapabilityClassWrite},
	}
	if err := t4.Validate(); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for ReadOnly: true + Write capability, got %v", err)
	}
}

func TestCompiler_CapabilityAliasNormalization_AdmitsRules(t *testing.T) {
	reg, err := compiler.NewCanonicalRuleRegistry()
	if err != nil {
		t.Fatalf("failed to build canonical rule registry: %v", err)
	}
	c := mustNewCompiler(t, reg, nil, nil)
	profile := compiler.MustDefaultProvisionalProfile("ep_1", "chan_1", "model", 65536)

	// 1. repository_mutation normalizes to write and admits DCI-030, DCI-031, DCI-034
	reqWrite := validCompileRequest(profile)
	reqWrite.SourceRevision = compiler.CanonicalSourceRevision
	reqWrite.MappingVersion = compiler.CanonicalMappingRevision
	reqWrite.ToolSchemas = []string{`{"name": "git_mutator"}`}
	reqWrite.DeclaredTools = []compiler.ToolCapabilityInfo{
		{
			Name:                 "git_mutator",
			RequiredCapabilities: []compiler.CapabilityClass{compiler.CapabilityClassRepositoryMutation},
		},
	}
	_, packWrite, err := c.Compile(context.Background(), reqWrite)
	if err != nil {
		t.Fatalf("compile with repository_mutation failed: %v", err)
	}
	writeRules := map[string]bool{"DCI-030": false, "DCI-031": false, "DCI-034": false}
	for id := range packWrite.AdmittedObjectDigests {
		if _, ok := writeRules[id]; ok {
			writeRules[id] = true
		}
	}
	for id, found := range writeRules {
		if !found {
			t.Errorf("expected %s to be admitted by repository_mutation (normalized to write)", id)
		}
	}

	// 2. process_execution normalizes to exec and admits DCI-033, DCI-083
	reqExec := validCompileRequest(profile)
	reqExec.SourceRevision = compiler.CanonicalSourceRevision
	reqExec.MappingVersion = compiler.CanonicalMappingRevision
	reqExec.ToolSchemas = []string{`{"name": "proc_runner"}`}
	reqExec.DeclaredTools = []compiler.ToolCapabilityInfo{
		{
			Name:                 "proc_runner",
			RequiredCapabilities: []compiler.CapabilityClass{compiler.CapabilityClassProcessExecution},
		},
	}
	_, packExec, err := c.Compile(context.Background(), reqExec)
	if err != nil {
		t.Fatalf("compile with process_execution failed: %v", err)
	}
	execRules := map[string]bool{"DCI-033": false, "DCI-083": false}
	for id := range packExec.AdmittedObjectDigests {
		if _, ok := execRules[id]; ok {
			execRules[id] = true
		}
	}
	for id, found := range execRules {
		if !found {
			t.Errorf("expected %s to be admitted by process_execution (normalized to exec)", id)
		}
	}
}

func TestInvocationDigest_IncludesMappingRevision(t *testing.T) {
	packDigest := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	profile := compiler.MustDefaultProvisionalProfile("ep_1", "chan_1", "model", 32768)

	d1, err := compiler.ComputeInvocationDigest(
		packDigest, "json", "sys", "user", []string{`{"name":"tool"}`}, "framing",
		"cat-1", "rev-1", "map-rev-1", "sha256:catdigest1", profile,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	d2, err := compiler.ComputeInvocationDigest(
		packDigest, "json", "sys", "user", []string{`{"name":"tool"}`}, "framing",
		"cat-1", "rev-1", "map-rev-2", "sha256:catdigest1", profile,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if d1 == d2 {
		t.Errorf("expected InvocationDigest to change when MappingRevision changes, got identical %q", d1)
	}
}

func TestEvidenceLease_StrictRFC3339Validation(t *testing.T) {
	lease := protocol.EvidenceLease{
		SchemaVersion:       protocol.SchemaVersion1,
		LeaseID:             "lease-1",
		EvidenceKind:        protocol.LeaseKindSourceSnippet,
		SourceRevision:      testBaseCommit,
		WorktreeID:          "wt_1",
		FilePath:            "internal/test.go",
		Locator:             "L1-L5",
		Content:             "test content",
		TokenCount:          5,
		AccountingMethod:    protocol.AccountingApproximateEstimate,
		Status:              protocol.LeaseStatusActive,
		AcquisitionQuestion: "Q",
		AcquisitionReason:   "R",
		AcquiredAt:          "not-a-valid-timestamp",
	}
	hasher := sha256.New()
	hasher.Write([]byte(lease.Content))
	lease.ContentDigest = "sha256:" + hex.EncodeToString(hasher.Sum(nil))

	if err := lease.Validate(); err == nil {
		t.Error("expected validation failure for non-RFC3339 AcquiredAt, got nil")
	}

	lease.AcquiredAt = "2026-10-02T00:00:00Z"
	badExp := "invalid-exp"
	lease.ExpiresAt = &badExp

	if err := lease.Validate(); err == nil {
		t.Error("expected validation failure for non-RFC3339 ExpiresAt, got nil")
	}
}
