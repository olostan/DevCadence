package compiler_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

func TestEvidenceLeaseManager_Lifecycle(t *testing.T) {
	mgr := compiler.NewEvidenceLeaseManager()

	// 1. Successful lease creation
	lease, err := mgr.CreateLease(compiler.CreateLeaseParams{
		EvidenceKind:        protocol.LeaseKindSourceSnippet,
		SourceRevision:      "d99e40c2107965b5af53a8c429aa6286f430ff8f",
		WorktreeID:          "wt_main",
		FilePath:            "internal/setup/doctor.go",
		Locator:             "L100-L120",
		AcquisitionQuestion: "What doctor checks exist?",
		AcquisitionReason:   "Ensuring environment safety",
		Content:             "func runDoctor() error {\n  return nil\n}",
		AccountingMethod:    protocol.AccountingApproximateEstimate,
		ReadEnvelope:        []string{"internal/*"},
	})
	if err != nil {
		t.Fatalf("failed to create valid lease: %v", err)
	}

	if lease.Status != protocol.LeaseStatusActive {
		t.Errorf("status: got %q, want active", lease.Status)
	}
	if !strings.HasPrefix(lease.ContentDigest, "sha256:") {
		t.Errorf("digest missing sha256 prefix: %q", lease.ContentDigest)
	}

	// 2. Read envelope authorization failure (DCI-014, DCI-018)
	_, err = mgr.CreateLease(compiler.CreateLeaseParams{
		EvidenceKind:        protocol.LeaseKindSourceSnippet,
		SourceRevision:      "d99e40c2107965b5af53a8c429aa6286f430ff8f",
		WorktreeID:          "wt_main",
		FilePath:            "config/credentials.json",
		Locator:             "L1-L5",
		AcquisitionQuestion: "Read credentials?",
		AcquisitionReason:   "Access tokens",
		Content:             "secret_tokens",
		ReadEnvelope:        []string{"internal/*"}, // config/ is NOT in read envelope!
	})
	if err == nil {
		t.Fatal("expected ErrPolicyDenied for path outside ReadEnvelope, got nil")
	}
	if !errors.Is(err, errs.ErrPolicyDenied) {
		t.Fatalf("expected ErrPolicyDenied, got %v", err)
	}

	// 3. Invalidation on file mutation (freshness tracking)
	invalidated := mgr.InvalidateForFileMutation("internal/setup/doctor.go")
	if len(invalidated) != 1 || invalidated[0] != lease.LeaseID {
		t.Fatalf("expected lease %q to be invalidated, got %v", lease.LeaseID, invalidated)
	}

	updatedLease, ok := mgr.GetLease(lease.LeaseID)
	if !ok || updatedLease.Status != protocol.LeaseStatusInvalidated {
		t.Fatalf("expected lease status invalidated, got %v", updatedLease.Status)
	}

	// 4. Invalidation on revision change
	lease2, err := mgr.CreateLease(compiler.CreateLeaseParams{
		EvidenceKind:        protocol.LeaseKindSourceSnippet,
		SourceRevision:      "commit_abc",
		WorktreeID:          "wt_main",
		FilePath:            "internal/protocol/context.go",
		Locator:             "L50-L60",
		AcquisitionQuestion: "Context verification",
		AcquisitionReason:   "Checking types",
		Content:             "type ContextPack struct {}",
		ReadEnvelope:        []string{"internal/*"},
	})
	if err != nil {
		t.Fatalf("failed to create lease2: %v", err)
	}

	invalidated = mgr.InvalidateForRevision("commit_new")
	if len(invalidated) != 1 || invalidated[0] != lease2.LeaseID {
		t.Fatalf("expected lease2 %q to be invalidated on revision change, got %v", lease2.LeaseID, invalidated)
	}

	// 5. Eviction to fit budget
	leaseA, _ := mgr.CreateLease(compiler.CreateLeaseParams{
		EvidenceKind:        protocol.LeaseKindSourceSnippet,
		SourceRevision:      "commit_current",
		WorktreeID:          "wt_main",
		FilePath:            "internal/a.go",
		Locator:             "L1",
		AcquisitionQuestion: "Q",
		AcquisitionReason:   "R",
		Content:             strings.Repeat("a ", 50),
		ReadEnvelope:        []string{"internal/*"},
	})
	leaseB, _ := mgr.CreateLease(compiler.CreateLeaseParams{
		EvidenceKind:        protocol.LeaseKindSourceSnippet,
		SourceRevision:      "commit_current",
		WorktreeID:          "wt_main",
		FilePath:            "internal/b.go",
		Locator:             "L1",
		AcquisitionQuestion: "Q",
		AcquisitionReason:   "R",
		Content:             strings.Repeat("b ", 50),
		ReadEnvelope:        []string{"internal/*"},
	})

	totalTokens := leaseA.TokenCount + leaseB.TokenCount
	targetTokens := leaseA.TokenCount // Need to evict one lease to reach target
	evicted, err := mgr.EvictToFit(targetTokens, totalTokens)
	if err != nil {
		t.Fatalf("evict error: %v", err)
	}
	if len(evicted) == 0 {
		t.Fatalf("expected at least one lease evicted to fit target, got %v", evicted)
	}
}

func TestIsPathAuthorized(t *testing.T) {
	tests := []struct {
		path     string
		patterns []string
		want     bool
	}{
		{"internal/compiler/admission.go", []string{"internal/*"}, true},
		{"internal/compiler/admission.go", []string{"internal/..."}, true},
		{"internal/compiler/admission.go", []string{"internal/compiler/*"}, true},
		{"docs/INVARIANTS.md", []string{"docs/INVARIANTS.md"}, true},
		{"secrets/keys.json", []string{"internal/*", "docs/*"}, false},
		{"internal/compiler/admission.go", []string{"*"}, true},
	}

	for _, tc := range tests {
		got := compiler.IsPathAuthorized(tc.path, tc.patterns)
		if got != tc.want {
			t.Errorf("IsPathAuthorized(%q, %v): got %v, want %v", tc.path, tc.patterns, got, tc.want)
		}
	}
}
