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
			t.Errorf("expected SelectionRationale on mandatory clause %s in manifest", mc.ClauseID)
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

	// True Invocation Identity:
	// Modifying Action changes the pack digest
	reqModifiedAction := req
	reqModifiedAction.Action = "Different action"
	_, packAction, errAction := c.Compile(context.Background(), reqModifiedAction)
	if errAction != nil {
		t.Fatalf("compilation with modified action failed: %v", errAction)
	}
	if pack.PackDigest == packAction.PackDigest {
		t.Errorf("PackDigest failed to change when CurrentAction changed")
	}

	// Modifying CognitiveState changes the pack digest
	capsuleMgr.AddHypothesis("H_new: Thread safe check")
	_, packCapsule, errCapsule := c.Compile(context.Background(), req)
	if errCapsule != nil {
		t.Fatalf("compilation with modified capsule failed: %v", errCapsule)
	}
	if pack.PackDigest == packCapsule.PackDigest {
		t.Errorf("PackDigest failed to change when CognitiveState changed")
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
		t.Fatal("expected Compile to fail closed on invalidated lease, got nil")
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
		t.Fatal("expected Compile to fail closed on expired lease, got nil")
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
		t.Fatal("expected Compile to fail closed on mismatched source revision, got nil")
	}
	if !errors.Is(compileErr, errs.ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed for mismatched revision, got %v", compileErr)
	}
}

func TestCompiler_FailClosedOnMissingProvenance(t *testing.T) {
	reg := setupTestRegistry(t)
	c := mustNewCompiler(t, reg, nil, nil)
	profile := compiler.MustDefaultProvisionalProfile("ep_1", "chan_1", "model", 32768)

	// Missing SourceRevision
	reqNoRev := validCompileRequest(profile)
	reqNoRev.SourceRevision = ""
	if _, _, err := c.Compile(context.Background(), reqNoRev); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for empty source_revision, got %v", err)
	}

	// Mismatched MappingVersion
	reqBadMap := validCompileRequest(profile)
	reqBadMap.MappingVersion = "invalid_mapping_v9.9"
	if _, _, err := c.Compile(context.Background(), reqBadMap); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for mismatched mapping_version, got %v", err)
	}

	// Empty WorkPackageDigest
	reqNoWPDigest := validCompileRequest(profile)
	reqNoWPDigest.WorkPackageDigest = ""
	if _, _, err := c.Compile(context.Background(), reqNoWPDigest); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for empty work_package_digest, got %v", err)
	}

	// Invalid WorkPackageDigest format
	reqBadWPDigest := validCompileRequest(profile)
	reqBadWPDigest.WorkPackageDigest = "md5:1234"
	if _, _, err := c.Compile(context.Background(), reqBadWPDigest); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for non-sha256 work_package_digest, got %v", err)
	}

	// Missing ExecutionContract
	reqNoContract := validCompileRequest(profile)
	reqNoContract.ExecutionContract = ""
	if _, _, err := c.Compile(context.Background(), reqNoContract); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for empty execution_contract, got %v", err)
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

func TestCompiler_InvalidWorkPackageRevisionFailsClosed(t *testing.T) {
	reg := setupTestRegistry(t)
	c := mustNewCompiler(t, reg, nil, nil)
	profile := compiler.MustDefaultProvisionalProfile("ep_1", "chan_1", "model", 32768)

	req := validCompileRequest(profile)
	req.WorkPackageRevision = 0 // Invalid revision (< 1)

	_, _, err := c.Compile(context.Background(), req)
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for work_package_revision < 1, got %v", err)
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
