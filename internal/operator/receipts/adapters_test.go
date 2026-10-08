package receipts

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/protocol"
)

func TestHostPlanApprovals_VerifyApproval(t *testing.T) {
	pub, priv := testKeypair(t)
	anchorID := "anchor_adapt_01"
	actorID := "operator-alice"
	projectID := "test-project"

	anchor := testAnchor(pub, anchorID, actorID)
	t0 := time.Now().UTC()
	anchor.NotBefore = t0.Add(-24 * time.Hour)
	anchor.NotAfter = t0.Add(24 * time.Hour)

	verifier, err := NewVerifier([]TrustAnchor{anchor})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	planDigest := "sha256:1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff"
	ref := "rcpt_01j7abc1234567890abcdef123"
	paths := []string{"/workspace/a.txt", "/workspace/b.txt"}

	scope := HostPlanScope{
		PlanDigest: planDigest,
		Paths:      paths,
	}
	scopeBytes, err := protocol.CanonicalJSON(scope)
	if err != nil {
		t.Fatalf("CanonicalJSON scope: %v", err)
	}
	expectedScopeDigest := protocol.DigestBytes(scopeBytes)

	makeReceipt := func(modify func(s *Statement)) Receipt {
		stmt := validStatement(pub, anchorID, actorID)
		stmt.ReceiptID = ref
		stmt.Purpose = PurposeHostPlanApply
		stmt.Use = UseOnce
		stmt.ProjectID = projectID
		stmt.Subject = Subject{Kind: "host_plan", ID: ref, Version: 1}
		stmt.SubjectDigest = expectedScopeDigest
		stmt.InputDigest = ""
		stmt.IssuedAt = t0.Add(-5 * time.Minute)
		stmt.NotAfter = t0.Add(30 * time.Minute)
		if modify != nil {
			modify(&stmt)
		}
		rcpt, err := stmt.Sign(priv)
		if err != nil {
			t.Fatalf("Sign statement: %v", err)
		}
		return rcpt
	}

	defaultRcpt := makeReceipt(nil)

	newVerifier := func() *InMemoryVerifier {
		v, err := NewVerifier([]TrustAnchor{anchor})
		if err != nil {
			t.Fatalf("NewVerifier: %v", err)
		}
		return v
	}

	t.Run("valid approval with scope digest", func(t *testing.T) {
		h := &HostPlanApprovals{
			Verifier: newVerifier(),
			ReadReceipt: func(r string) (Receipt, error) {
				return defaultRcpt, nil
			},
			ProjectID: projectID,
		}
		if err := h.VerifyApproval(context.Background(), ref, planDigest, paths); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid approval with plan digest and HostPlan uppercase", func(t *testing.T) {
		planRcpt := makeReceipt(func(s *Statement) {
			s.Subject.Kind = "HostPlan"
			s.Subject.ID = planDigest
			s.SubjectDigest = planDigest
		})
		h := &HostPlanApprovals{
			Verifier: newVerifier(),
			ReadReceipt: func(r string) (Receipt, error) {
				return planRcpt, nil
			},
		}
		if err := h.VerifyApproval(context.Background(), ref, planDigest, paths); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("missing ref", func(t *testing.T) {
		h := &HostPlanApprovals{Verifier: verifier}
		err := h.VerifyApproval(context.Background(), "   ", planDigest, paths)
		if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
			t.Fatalf("category = %v, want CategoryInvalidArgument", errs.CategoryOf(err))
		}
	})

	t.Run("missing plan digest", func(t *testing.T) {
		h := &HostPlanApprovals{Verifier: verifier}
		err := h.VerifyApproval(context.Background(), ref, "", paths)
		if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
			t.Fatalf("category = %v, want CategoryInvalidArgument", errs.CategoryOf(err))
		}
	})

	pathTests := []struct {
		name  string
		paths []string
		match string
	}{
		{
			name:  "relative path",
			paths: []string{"workspace/a.txt"},
			match: "must be absolute",
		},
		{
			name:  "NUL byte in path",
			paths: []string{"/workspace/\x00a.txt"},
			match: "contains NUL",
		},
		{
			name:  "uncleaned path",
			paths: []string{"/workspace//a.txt"},
			match: "is not clean",
		},
		{
			name:  "path contains ..",
			paths: []string{"/workspace/sub/../a.txt"},
			match: "is not clean", // filepath.Clean would change it, or .. check
		},
		{
			name:  "duplicate paths",
			paths: []string{"/workspace/a.txt", "/workspace/a.txt"},
			match: "duplicate path",
		},
		{
			name:  "unsorted paths",
			paths: []string{"/workspace/b.txt", "/workspace/a.txt"},
			match: "paths not sorted",
		},
	}

	for _, pt := range pathTests {
		t.Run("path validation: "+pt.name, func(t *testing.T) {
			h := &HostPlanApprovals{
				Verifier: verifier,
				ReadReceipt: func(r string) (Receipt, error) {
					return defaultRcpt, nil
				},
			}
			err := h.VerifyApproval(context.Background(), ref, planDigest, pt.paths)
			if err == nil {
				t.Fatalf("expected error for path %v, got nil", pt.paths)
			}
			if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
				t.Errorf("category = %v, want CategoryInvalidArgument", errs.CategoryOf(err))
			}
			if !strings.Contains(err.Error(), pt.match) {
				t.Errorf("error %q does not contain %q", err.Error(), pt.match)
			}
		})
	}

	t.Run("failed to read receipt", func(t *testing.T) {
		h := &HostPlanApprovals{
			Verifier: verifier,
			ReadReceipt: func(r string) (Receipt, error) {
				return Receipt{}, errs.New(errs.CategoryNotFound, "not found")
			},
		}
		err := h.VerifyApproval(context.Background(), ref, planDigest, paths)
		if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Fatalf("category = %v, want CategoryPolicyDenied", errs.CategoryOf(err))
		}
	})

	t.Run("purpose mismatch", func(t *testing.T) {
		wrongPurpose := makeReceipt(func(s *Statement) {
			s.Purpose = PurposeDiscoveryReflection
			s.InputDigest = "sha256:abcd"
		})
		h := &HostPlanApprovals{
			Verifier: verifier,
			ReadReceipt: func(r string) (Receipt, error) {
				return wrongPurpose, nil
			},
		}
		err := h.VerifyApproval(context.Background(), ref, planDigest, paths)
		if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Fatalf("category = %v, want CategoryPolicyDenied", errs.CategoryOf(err))
		}
	})

	t.Run("subject kind mismatch", func(t *testing.T) {
		wrongKind := makeReceipt(func(s *Statement) {
			s.Subject.Kind = "invalid_kind"
		})
		h := &HostPlanApprovals{
			Verifier: verifier,
			ReadReceipt: func(r string) (Receipt, error) {
				return wrongKind, nil
			},
		}
		err := h.VerifyApproval(context.Background(), ref, planDigest, paths)
		if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Fatalf("category = %v, want CategoryPolicyDenied", errs.CategoryOf(err))
		}
	})

	t.Run("subject id mismatch", func(t *testing.T) {
		wrongID := makeReceipt(func(s *Statement) {
			s.Subject.ID = "wrong_id"
		})
		h := &HostPlanApprovals{
			Verifier: verifier,
			ReadReceipt: func(r string) (Receipt, error) {
				return wrongID, nil
			},
		}
		err := h.VerifyApproval(context.Background(), ref, planDigest, paths)
		if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Fatalf("category = %v, want CategoryPolicyDenied", errs.CategoryOf(err))
		}
	})

	t.Run("subject version mismatch", func(t *testing.T) {
		wrongVer := makeReceipt(func(s *Statement) {
			s.Subject.Version = 2
		})
		h := &HostPlanApprovals{
			Verifier: verifier,
			ReadReceipt: func(r string) (Receipt, error) {
				return wrongVer, nil
			},
		}
		err := h.VerifyApproval(context.Background(), ref, planDigest, paths)
		if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Fatalf("category = %v, want CategoryPolicyDenied", errs.CategoryOf(err))
		}
	})

	t.Run("subject digest mismatch", func(t *testing.T) {
		wrongDigest := makeReceipt(func(s *Statement) {
			s.SubjectDigest = "sha256:9999999999999999999999999999999999999999999999999999999999999999"
		})
		h := &HostPlanApprovals{
			Verifier: verifier,
			ReadReceipt: func(r string) (Receipt, error) {
				return wrongDigest, nil
			},
		}
		err := h.VerifyApproval(context.Background(), ref, planDigest, paths)
		if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Fatalf("category = %v, want CategoryPolicyDenied", errs.CategoryOf(err))
		}
	})

	t.Run("nil verifier", func(t *testing.T) {
		h := &HostPlanApprovals{
			Verifier: nil,
			ReadReceipt: func(r string) (Receipt, error) {
				return defaultRcpt, nil
			},
		}
		err := h.VerifyApproval(context.Background(), ref, planDigest, paths)
		if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Fatalf("category = %v, want CategoryPolicyDenied", errs.CategoryOf(err))
		}
	})
}

func TestHostPlanApprovals_ReadFromDisk(t *testing.T) {
	pub, priv := testKeypair(t)
	anchorID := "anchor_disk_01"
	actorID := "operator-alice"
	projectID := "test-project"

	anchor := testAnchor(pub, anchorID, actorID)
	t0 := time.Now().UTC()
	anchor.NotBefore = t0.Add(-24 * time.Hour)
	anchor.NotAfter = t0.Add(24 * time.Hour)

	verifier, err := NewVerifier([]TrustAnchor{anchor})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	tempDir := t.TempDir()
	ref := "rcpt_01j7abc1234567890abcdef123"
	planDigest := "sha256:1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff"
	paths := []string{"/workspace/a.txt"}

	scope := HostPlanScope{PlanDigest: planDigest, Paths: paths}
	scopeBytes, _ := protocol.CanonicalJSON(scope)
	expectedScopeDigest := protocol.DigestBytes(scopeBytes)

	stmt := validStatement(pub, anchorID, actorID)
	stmt.ReceiptID = ref
	stmt.Purpose = PurposeHostPlanApply
	stmt.Use = UseOnce
	stmt.ProjectID = projectID
	stmt.Subject = Subject{Kind: "host_plan", ID: ref, Version: 1}
	stmt.SubjectDigest = expectedScopeDigest
	stmt.InputDigest = ""
	stmt.IssuedAt = t0.Add(-5 * time.Minute)
	stmt.NotAfter = t0.Add(30 * time.Minute)
	rcpt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	rcptBytes, err := json.Marshal(rcpt)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// 1. Invalid receipt ref format
	h := &HostPlanApprovals{Verifier: verifier, ReceiptsDir: tempDir}
	err = h.VerifyApproval(context.Background(), "invalid_ref", planDigest, paths)
	if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("category = %v, want CategoryInvalidArgument", errs.CategoryOf(err))
	}

	// 2. Receipt file not found
	err = h.VerifyApproval(context.Background(), ref, planDigest, paths)
	if err == nil || errs.CategoryOf(err) != errs.CategoryNotFound {
		t.Errorf("category = %v, want CategoryNotFound", errs.CategoryOf(err))
	}

	// 3. Write valid receipt to disk and verify
	filePath := filepath.Join(tempDir, ref+".json")
	if err := os.WriteFile(filePath, rcptBytes, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := h.VerifyApproval(context.Background(), ref, planDigest, paths); err != nil {
		t.Fatalf("VerifyApproval disk read failed: %v", err)
	}

	// 4. Oversized receipt file (> 64 KiB)
	oversizedPath := filepath.Join(tempDir, "rcpt_01j7abc1234567890abcdef999.json")
	bigData := make([]byte, 65*1024)
	if err := os.WriteFile(oversizedPath, bigData, 0644); err != nil {
		t.Fatalf("WriteFile big: %v", err)
	}
	err = h.VerifyApproval(context.Background(), "rcpt_01j7abc1234567890abcdef999", planDigest, paths)
	if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Errorf("category = %v, want CategoryPolicyDenied for oversized receipt", errs.CategoryOf(err))
	}

	// 5. Invalid JSON file
	badJSONPath := filepath.Join(tempDir, "rcpt_01j7abc1234567890abcdef888.json")
	if err := os.WriteFile(badJSONPath, []byte("invalid json content"), 0644); err != nil {
		t.Fatalf("WriteFile bad json: %v", err)
	}
	err = h.VerifyApproval(context.Background(), "rcpt_01j7abc1234567890abcdef888", planDigest, paths)
	if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Errorf("category = %v, want CategoryInvalidArgument for invalid json", errs.CategoryOf(err))
	}

	// 6. DEVCADENCE_HOME fallback test
	hNoDir := &HostPlanApprovals{Verifier: verifier, ReceiptsDir: ""}
	err = hNoDir.VerifyApproval(context.Background(), ref, planDigest, paths)
	if err == nil || errs.CategoryOf(err) != errs.CategoryNotFound {
		t.Errorf("category = %v, want CategoryNotFound when dir unconfigured", errs.CategoryOf(err))
	}
}

func TestHumanReceipts_Verify(t *testing.T) {
	pub, priv := testKeypair(t)
	anchorID := "anchor_human_01"
	actorID := "operator-alice"
	projectID := "test-project"

	anchor := testAnchor(pub, anchorID, actorID)
	t0 := time.Now().UTC()
	anchor.NotBefore = t0.Add(-24 * time.Hour)
	anchor.NotAfter = t0.Add(24 * time.Hour)

	verifier, err := NewVerifier([]TrustAnchor{anchor})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	ref := "rcpt_01j7abc1234567890abcdef123"
	subject := ReceiptSubject{Kind: "DiscoveryQuestion", ID: "q-123", Version: 1}
	inputDigest := "sha256:222233334444555566667777888899990000aaaabbbbccccddddeeeeffff0000"

	makeReceipt := func(modify func(s *Statement)) Receipt {
		stmt := validStatement(pub, anchorID, actorID)
		stmt.ReceiptID = ref
		stmt.Purpose = PurposeDiscoveryReflection
		stmt.Use = UseOnce
		stmt.ProjectID = projectID
		stmt.Subject = Subject{Kind: subject.Kind, ID: subject.ID, Version: subject.Version}
		stmt.SubjectDigest = "sha256:1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff"
		stmt.InputDigest = inputDigest
		stmt.IssuedAt = t0.Add(-5 * time.Minute)
		stmt.NotAfter = t0.Add(30 * time.Minute)
		if modify != nil {
			modify(&stmt)
		}
		rcpt, err := stmt.Sign(priv)
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		return rcpt
	}

	defaultRcpt := makeReceipt(nil)
	caller := principal.CallerContext{
		PrincipalID: "principal-alice",
		ProjectID:   projectID,
	}

	hr := &HumanReceipts{
		Verifier: verifier,
		ReadReceipt: func(r string) (Receipt, error) {
			return defaultRcpt, nil
		},
	}

	t.Run("valid human receipt", func(t *testing.T) {
		res, err := hr.Verify(context.Background(), caller, ref, string(PurposeDiscoveryReflection), inputDigest, subject)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.HumanActorID != actorID {
			t.Errorf("HumanActorID = %q, want %q", res.HumanActorID, actorID)
		}
		if res.SourceRef != ref {
			t.Errorf("SourceRef = %q, want %q", res.SourceRef, ref)
		}
	})

	t.Run("invalid caller", func(t *testing.T) {
		_, err := hr.Verify(context.Background(), principal.CallerContext{}, ref, string(PurposeDiscoveryReflection), inputDigest, subject)
		if err == nil {
			t.Fatal("expected caller error")
		}
	})

	t.Run("invalid receipt ref prefix", func(t *testing.T) {
		_, err := hr.Verify(context.Background(), caller, "bad_ref", string(PurposeDiscoveryReflection), inputDigest, subject)
		if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
			t.Fatalf("category = %v, want CategoryInvalidArgument", errs.CategoryOf(err))
		}
	})

	t.Run("unauthorized purpose", func(t *testing.T) {
		_, err := hr.Verify(context.Background(), caller, ref, string(PurposeHostPlanApply), inputDigest, subject)
		if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Fatalf("category = %v, want CategoryPolicyDenied", errs.CategoryOf(err))
		}
	})

	t.Run("receipt ID mismatch", func(t *testing.T) {
		_, err := hr.Verify(context.Background(), caller, "rcpt_01j7abc1234567890abcdef999", string(PurposeDiscoveryReflection), inputDigest, subject)
		if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Fatalf("category = %v, want CategoryPolicyDenied", errs.CategoryOf(err))
		}
	})

	t.Run("project ID mismatch", func(t *testing.T) {
		diffProjectCaller := caller
		diffProjectCaller.ProjectID = "other-project"
		_, err := hr.Verify(context.Background(), diffProjectCaller, ref, string(PurposeDiscoveryReflection), inputDigest, subject)
		if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Fatalf("category = %v, want CategoryPolicyDenied", errs.CategoryOf(err))
		}
	})

	t.Run("purpose mismatch in statement", func(t *testing.T) {
		diffPurposeRcpt := makeReceipt(func(s *Statement) {
			s.Purpose = PurposeDiscoveryAcceptedRisk
		})
		hrDiff := &HumanReceipts{
			Verifier: verifier,
			ReadReceipt: func(r string) (Receipt, error) {
				return diffPurposeRcpt, nil
			},
		}
		_, err := hrDiff.Verify(context.Background(), caller, ref, string(PurposeDiscoveryReflection), inputDigest, subject)
		if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Fatalf("category = %v, want CategoryPolicyDenied", errs.CategoryOf(err))
		}
	})

	t.Run("subject mismatch", func(t *testing.T) {
		diffSubj := subject
		diffSubj.ID = "other-q"
		_, err := hr.Verify(context.Background(), caller, ref, string(PurposeDiscoveryReflection), inputDigest, diffSubj)
		if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Fatalf("category = %v, want CategoryPolicyDenied", errs.CategoryOf(err))
		}
	})

	t.Run("input digest mismatch", func(t *testing.T) {
		_, err := hr.Verify(context.Background(), caller, ref, string(PurposeDiscoveryReflection), "sha256:diff", subject)
		if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Fatalf("category = %v, want CategoryPolicyDenied", errs.CategoryOf(err))
		}
	})

	t.Run("nil verifier", func(t *testing.T) {
		hrNil := &HumanReceipts{
			Verifier: nil,
			ReadReceipt: func(r string) (Receipt, error) {
				return defaultRcpt, nil
			},
		}
		_, err := hrNil.Verify(context.Background(), caller, ref, string(PurposeDiscoveryReflection), inputDigest, subject)
		if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Fatalf("category = %v, want CategoryPolicyDenied", errs.CategoryOf(err))
		}
	})
}
