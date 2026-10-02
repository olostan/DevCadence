package compiler_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

const testBaseCommit = "d99e40c2107965b5af53a8c429aa6286f430ff8f"

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

func TestRuleRegistry_DeepCopyImmutability(t *testing.T) {
	reg := compiler.NewRuleRegistry()

	domains := []string{"cognition"}
	roles := []string{"implementer"}
	dependsOn := []string{"DEP-1"}

	err := reg.Register(compiler.Rule{
		ID:             "RULE-IMMUTABLE",
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
	c := compiler.NewCompiler(reg, leaseMgr, capsuleMgr)

	profile := compiler.DefaultProvisionalProfile("ep_1", "chan_1", "qwen2.5-coder", 32768)
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
	c := compiler.NewCompiler(reg, leaseMgr, capsuleMgr)

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

	profile := compiler.DefaultProvisionalProfile("ep_1", "chan_1", "qwen", 32768)
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
	c := compiler.NewCompiler(reg, leaseMgr, capsuleMgr)

	tinyProfile := compiler.DefaultProvisionalProfile("ep_tiny", "chan_tiny", "tiny-model", 500)
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
	c := compiler.NewCompiler(reg, leaseMgr, capsuleMgr)

	profile := compiler.DefaultProvisionalProfile("ep_1", "chan_1", "qwen", 32768)

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
	c := compiler.NewCompiler(reg, leaseMgr, capsuleMgr)

	profile := compiler.DefaultProvisionalProfile("ep_1", "chan_1", "qwen", 32768)

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
	c := compiler.NewCompiler(reg, leaseMgr, capsuleMgr)

	profile := compiler.DefaultProvisionalProfile("ep_1", "chan_1", "qwen", 32768)

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
	c := compiler.NewCompiler(reg, leaseMgr, capsuleMgr)

	profile := compiler.DefaultProvisionalProfile("ep_1", "chan_1", "qwen", 32768)

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

	// Missing MappingVersion
	reqNoMap := validCompileRequest(profile)
	reqNoMap.MappingVersion = ""
	if _, _, err := c.Compile(context.Background(), reqNoMap); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for missing MappingVersion, got %v", err)
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
	c := compiler.NewCompiler(reg, leaseMgr, capsuleMgr)

	// Profile with tight ceiling
	tightProfile := compiler.DefaultProvisionalProfile("ep_tight", "chan_tight", "model", 2000)

	req := validCompileRequest(tightProfile)
	req.ContextProfile = tightProfile
	// Add huge tool schemas to exceed projection fit (Finding 4)
	req.ToolSchemas = []string{
		strings.Repeat("tool schema declaration description parameters ", 200),
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
