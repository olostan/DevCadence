package receipts

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/protocol"
)

func TestDarwinLsACLParser(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		wantPass  bool
		wantError bool
	}{
		{
			name:      "clean single-line",
			output:    "-rw-r--r--  1 root wheel 100 Jan 1 00:00 file\n",
			wantPass:  true,
			wantError: false,
		},
		{
			name:      "clean directory single-line",
			output:    "drwxr-xr-x  2 root wheel 100 Jan 1 00:00 dir\n",
			wantPass:  true,
			wantError: false,
		},
		{
			name:      "extended attribute clean",
			output:    "-rw-r--r--@ 1 root wheel 100 Jan 1 00:00 file\n",
			wantPass:  true,
			wantError: false,
		},
		{
			name:      "directory extended attribute clean",
			output:    "drwxr-xr-x@ 2 root wheel 100 Jan 1 00:00 dir\n",
			wantPass:  true,
			wantError: false,
		},
		{
			name:      "ACL plus indicator",
			output:    "-rw-r--r--+ 1 root wheel 100 Jan 1 00:00 file\n",
			wantPass:  false,
			wantError: true,
		},
		{
			name:      "directory ACL plus indicator",
			output:    "drwxr-xr-x+ 2 root wheel 100 Jan 1 00:00 dir\n",
			wantPass:  false,
			wantError: true,
		},
		{
			name:      "ACE lines present",
			output:    "-rw-r--r--@ 1 root wheel 100 Jan 1 00:00 file\n 0: group:everyone deny delete\n",
			wantPass:  false,
			wantError: true,
		},
		{
			name:      "malformed - empty output",
			output:    "",
			wantPass:  false,
			wantError: true,
		},
		{
			name:      "malformed - missing trailing newline",
			output:    "-rw-r--r--  1 root wheel 100 Jan 1 00:00 file",
			wantPass:  false,
			wantError: true,
		},
		{
			name:      "malformed - carriage return",
			output:    "-rw-r--r--  1 root wheel 100 Jan 1 00:00 file\r\n",
			wantPass:  false,
			wantError: true,
		},
		{
			name:      "malformed - non mode line",
			output:    "total 100\n",
			wantPass:  false,
			wantError: true,
		},
		{
			name:      "malformed - garbage",
			output:    "ls: file: No such file or directory\n",
			wantPass:  false,
			wantError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			absent, err := parseDarwinLsACL([]byte(tc.output))
			if tc.wantError {
				if err == nil {
					t.Fatalf("expected error, got nil (absent=%v)", absent)
				}
				if absent {
					t.Errorf("expected absent=false on failure, got true")
				}
				if errs.CategoryOf(err) != errs.CategoryPolicyDenied {
					t.Errorf("expected CategoryPolicyDenied error, got %v", err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if absent != tc.wantPass {
					t.Errorf("absent = %v, want %v", absent, tc.wantPass)
				}
			}
		})
	}
}

func TestCheckPathProtection_EUIDZero(t *testing.T) {
	orig := getEUID
	defer func() { getEUID = orig }()
	getEUID = func() int { return 0 }

	err := CheckPathProtection("/any/path", ProtectionOptions{OperatorUID: 1000})
	if err == nil {
		t.Fatal("expected failure when EUID == 0")
	}
	if errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Errorf("expected CategoryPolicyDenied, got %v", err)
	}
}

func TestCheckPathProtection_AncestorWalk(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("skipping POSIX protection tests on non-POSIX")
	}

	tempRoot := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(tempRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	baseDir := filepath.Join(canonicalRoot, "trusted_base")
	if err := os.Mkdir(baseDir, 0o755); err != nil {
		t.Fatalf("Mkdir baseDir: %v", err)
	}

	operatorDir := filepath.Join(baseDir, "operator")
	if err := os.Mkdir(operatorDir, 0o700); err != nil {
		t.Fatalf("Mkdir operatorDir: %v", err)
	}

	targetFile := filepath.Join(operatorDir, "anchors.json")
	if err := os.WriteFile(targetFile, []byte(`{"version":"1.0"}`), 0o600); err != nil {
		t.Fatalf("WriteFile targetFile: %v", err)
	}

	myUID := uint32(os.Getuid())
	opts := ProtectionOptions{
		OperatorUID:      myUID,
		TrustedOwnerUIDs: []uint32{0, myUID},
		RootDir:          baseDir,
		CheckACL:         func(p string) error { return nil }, // mock ACL pass
	}

	t.Run("success", func(t *testing.T) {
		if err := CheckPathProtection(targetFile, opts); err != nil {
			t.Fatalf("unexpected failure: %v", err)
		}
	})

	t.Run("relative path rejected", func(t *testing.T) {
		err := CheckPathProtection("operator/anchors.json", opts)
		if err == nil {
			t.Fatal("expected error for relative path")
		}
		if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
			t.Errorf("expected CategoryInvalidArgument, got %v", err)
		}
	})

	t.Run("unclean path rejected", func(t *testing.T) {
		err := CheckPathProtection(targetFile+"/.", opts)
		if err == nil {
			t.Fatal("expected error for unclean path")
		}
		if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
			t.Errorf("expected CategoryInvalidArgument, got %v", err)
		}
	})

	t.Run("group writable ancestor directory rejected", func(t *testing.T) {
		if err := os.Chmod(baseDir, 0o775); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Chmod(baseDir, 0o755) }()

		err := CheckPathProtection(targetFile, opts)
		if err == nil {
			t.Fatal("expected failure on group-writable ancestor directory")
		}
		if errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Errorf("expected CategoryPolicyDenied, got %v", err)
		}
	})

	t.Run("world writable directory rejected", func(t *testing.T) {
		if err := os.Chmod(operatorDir, 0o777); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Chmod(operatorDir, 0o700) }()

		err := CheckPathProtection(targetFile, opts)
		if err == nil {
			t.Fatal("expected failure on world-writable directory")
		}
		if errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Errorf("expected CategoryPolicyDenied, got %v", err)
		}
	})

	t.Run("symlink component rejected", func(t *testing.T) {
		symlinkDir := filepath.Join(baseDir, "sym_operator")
		if err := os.Symlink(operatorDir, symlinkDir); err != nil {
			t.Fatal(err)
		}
		symlinkTarget := filepath.Join(symlinkDir, "anchors.json")

		err := CheckPathProtection(symlinkTarget, opts)
		if err == nil {
			t.Fatal("expected failure on path containing symlink")
		}
		if errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Errorf("expected CategoryPolicyDenied, got %v", err)
		}
	})

	t.Run("wrong owner rejected", func(t *testing.T) {
		wrongUID := myUID + 54321
		wrongOpts := ProtectionOptions{
			OperatorUID:      wrongUID,
			TrustedOwnerUIDs: []uint32{0, wrongUID},
			RootDir:          baseDir,
			CheckACL:         func(p string) error { return nil },
		}
		err := CheckPathProtection(targetFile, wrongOpts)
		if err == nil {
			t.Fatal("expected failure for wrong OperatorUID")
		}
		if errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Errorf("expected CategoryPolicyDenied, got %v", err)
		}
	})

	t.Run("untrusted owner in ancestor rejected", func(t *testing.T) {
		restrictedOpts := ProtectionOptions{
			OperatorUID:      myUID,
			TrustedOwnerUIDs: []uint32{0}, // myUID is not in trusted owner UIDs
			RootDir:          baseDir,
			CheckACL:         func(p string) error { return nil },
		}
		err := CheckPathProtection(targetFile, restrictedOpts)
		if err == nil {
			t.Fatal("expected failure when ancestor owned by untrusted UID")
		}
		if errs.CategoryOf(err) != errs.CategoryPolicyDenied {
			t.Errorf("expected CategoryPolicyDenied, got %v", err)
		}
	})

	t.Run("sibling prefix root escape rejected", func(t *testing.T) {
		siblingDir := baseDir + "_evil"
		if err := os.Mkdir(siblingDir, 0o700); err != nil {
			t.Fatalf("Mkdir siblingDir: %v", err)
		}
		siblingFile := filepath.Join(siblingDir, "anchors.json")
		if err := os.WriteFile(siblingFile, []byte(`{"version":"1.0"}`), 0o600); err != nil {
			t.Fatalf("WriteFile siblingFile: %v", err)
		}
		err := CheckPathProtection(siblingFile, opts)
		if err == nil {
			t.Fatal("expected failure on sibling directory escape")
		}
		if errs.CategoryOf(err) != errs.CategoryInvalidArgument {
			t.Errorf("expected CategoryInvalidArgument, got %v", err)
		}
	})
}

func TestHostPlanApprovals_Adapter(t *testing.T) {
	pub, priv := testKeypair(t)
	anchorID := "anchor-hp"
	actorID := "operator-bob"
	projectID := "project-x"
	planDigest := "sha256:aaaa1111222233334444555566667777888899990000aaaabbbbccccddddeeee"
	paths := []string{"/etc/config.json", "/var/lib/data.bin"}

	anchor := TrustAnchor{
		AnchorID:     anchorID,
		HumanActorID: actorID,
		PublicKey:    pub,
		NotBefore:    time.Now().UTC().Add(-1 * time.Hour),
		NotAfter:     time.Now().UTC().Add(24 * time.Hour),
		Purposes:     []Purpose{PurposeHostPlanApply},
	}
	scopeBytes, err := protocol.CanonicalJSON(HostPlanScope{PlanDigest: planDigest, Paths: paths})
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	now := time.Now().UTC()
	text := "Approve host plan"
	receiptID := "rcpt_01j7hostplan1234567890abcd"
	stmt := Statement{
		Version:       "1.0",
		ReceiptID:     receiptID,
		Purpose:       PurposeHostPlanApply,
		Use:           UseOnce,
		ProjectID:     projectID,
		Subject:       Subject{Kind: "HostPlan", ID: planDigest, Version: 1},
		SubjectDigest: protocol.DigestBytes(scopeBytes),
		Text:          text,
		TextDigest:    protocol.DigestBytes([]byte(text)),
		HumanActorID:  actorID,
		AnchorID:      anchorID,
		IssuedAt:      now.Add(-5 * time.Minute),
		NotAfter:      now.Add(55 * time.Minute),
	}
	receipt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	v, err := NewVerifier([]TrustAnchor{anchor}, WithReceipts(receipt))
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	approvals := &HostPlanApprovals{Verifier: v, ProjectID: projectID}
	if err := approvals.VerifyApproval(context.Background(), receiptID, planDigest, paths); err != nil {
		t.Fatalf("VerifyApproval failed: %v", err)
	}

	unsorted := []string{"/var/lib/data.bin", "/etc/config.json"}
	if err := approvals.VerifyApproval(context.Background(), receiptID, planDigest, unsorted); err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("unsorted paths error = %v", err)
	}

	if err := approvals.VerifyApproval(context.Background(), receiptID, "sha256:different-plan-digest", paths); err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Fatalf("plan digest mismatch error = %v", err)
	}
}

func TestHumanReceipts_Adapter(t *testing.T) {
	pub, priv := testKeypair(t)
	anchorID := "anchor-human"
	actorID := "operator-carol"
	projectID := "project-alpha"
	inputDigest := "sha256:input1234567890abcdef"
	subjectDigest := "sha256:subject1234567890abcdef"
	subject := Subject{Kind: "ProductDecision", ID: "dec-101", Version: 1}

	anchor := TrustAnchor{
		AnchorID:     anchorID,
		HumanActorID: actorID,
		PublicKey:    pub,
		NotBefore:    time.Now().UTC().Add(-1 * time.Hour),
		NotAfter:     time.Now().UTC().Add(24 * time.Hour),
		Purposes:     []Purpose{PurposeDiscoveryProductDecision},
	}
	now := time.Now().UTC()
	text := "Confirm product decision D-101"
	receiptID := "rcpt_01j7decision1234567890abcd"
	stmt := Statement{
		Version:       "1.0",
		ReceiptID:     receiptID,
		Purpose:       PurposeDiscoveryProductDecision,
		Use:           UseOnce,
		ProjectID:     projectID,
		Subject:       subject,
		SubjectDigest: subjectDigest,
		InputDigest:   inputDigest,
		Text:          text,
		TextDigest:    protocol.DigestBytes([]byte(text)),
		HumanActorID:  actorID,
		AnchorID:      anchorID,
		IssuedAt:      now.Add(-5 * time.Minute),
		NotAfter:      now.Add(55 * time.Minute),
	}
	receipt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	v, err := NewVerifier([]TrustAnchor{anchor}, WithReceipts(receipt))
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	caller := principal.CallerContext{
		PrincipalID:    "p-principal",
		ProjectID:      projectID,
		AllowedActions: []string{"investigate", "record_decision"},
	}
	reqSubject := ReceiptSubject{
		Kind:          subject.Kind,
		ID:            subject.ID,
		Version:       subject.Version,
		SubjectDigest: subjectDigest,
	}
	humanAdapter := &HumanReceipts{Verifier: v}
	hRec, err := humanAdapter.Verify(context.Background(), caller, receiptID, string(PurposeDiscoveryProductDecision), inputDigest, reqSubject)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if hRec.HumanActorID != actorID {
		t.Fatalf("HumanActorID = %q, want %q", hRec.HumanActorID, actorID)
	}

	wrongCaller := caller
	wrongCaller.ProjectID = "project-other"
	if _, err := humanAdapter.Verify(context.Background(), wrongCaller, receiptID, string(PurposeDiscoveryProductDecision), inputDigest, reqSubject); err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Fatalf("project mismatch error = %v", err)
	}

	badSubject := reqSubject
	badSubject.SubjectDigest = "sha256:different"
	if _, err := humanAdapter.Verify(context.Background(), caller, receiptID, string(PurposeDiscoveryProductDecision), inputDigest, badSubject); err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Fatalf("subject digest mismatch error = %v", err)
	}
}
