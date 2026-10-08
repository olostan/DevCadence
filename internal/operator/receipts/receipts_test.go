package receipts

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
	"github.com/olostan/DevCadence/internal/storage"
)

type mockBatchReadView struct {
	records map[string]storage.StoredRecord
	err     error
}

func (m *mockBatchReadView) ProjectState() *protocol.ProjectState { return nil }
func (m *mockBatchReadView) Record(ctx context.Context, kind, id string, version int) (storage.StoredRecord, error) {
	if m.err != nil {
		return storage.StoredRecord{}, m.err
	}
	key := kind + ":" + id
	if rec, ok := m.records[key]; ok {
		return rec, nil
	}
	return storage.StoredRecord{}, errs.New(errs.CategoryNotFound, "record %s %s not found", kind, id)
}

func testKeypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return pub, priv
}

func testAnchor(pub ed25519.PublicKey, anchorID, humanActorID string) TrustAnchor {
	now := time.Now().UTC()
	return TrustAnchor{
		AnchorID:     anchorID,
		HumanActorID: humanActorID,
		PublicKey:    pub,
		NotBefore:    now.Add(-24 * time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		Purposes: []Purpose{
			PurposeDiscoveryProductDecision,
			PurposeDiscoveryRequirementConfirm,
			PurposeDiscoveryLedgerResolution,
			PurposeDiscoveryReflection,
			PurposeDiscoveryAcceptedRisk,
			PurposeHostPlanApply,
			PurposeAcceptancePolicyActivate,
			PurposeExecutionPolicyActivate,
			PurposeEmpiricalCampaignAuthorize,
		},
	}
}

func validStatement(pub ed25519.PublicKey, anchorID, humanActorID string) Statement {
	now := time.Now().UTC()
	text := "Approve host execution plan plan-123"
	return Statement{
		Version:       "1.0",
		ReceiptID:     "rcpt_01j7abc1234567890abcdef123",
		Purpose:       PurposeHostPlanApply,
		Use:           UseOnce,
		ProjectID:     "test-project",
		Subject:       Subject{Kind: "HostPlan", ID: "sha256:1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff", Version: 1},
		SubjectDigest: "sha256:1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff",
		InputDigest:   "",
		Text:          text,
		TextDigest:    protocol.DigestBytes([]byte(text)),
		HumanActorID:  humanActorID,
		AnchorID:      anchorID,
		IssuedAt:      now.Add(-10 * time.Minute),
		NotAfter:      now.Add(50 * time.Minute),
	}
}

func setupTestFileVerifier(t *testing.T, pub ed25519.PublicKey, anchorID, humanActorID string, purposes []Purpose) (*FileVerifier, string, string) {
	t.Helper()
	origEUID := getEUID
	myUID := uint32(os.Getuid())
	mockVerifierEUID := myUID + 100
	getEUID = func() int { return int(mockVerifierEUID) }
	t.Cleanup(func() { getEUID = origEUID })

	tempDir := t.TempDir()
	canonicalDir, err := filepath.EvalSymlinks(tempDir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	operatorDir := filepath.Join(canonicalDir, "operator")
	if err := os.Mkdir(operatorDir, 0o700); err != nil {
		t.Fatalf("Mkdir operatorDir: %v", err)
	}

	receiptsDir := filepath.Join(canonicalDir, "receipts")
	if err := os.Mkdir(receiptsDir, 0o700); err != nil {
		t.Fatalf("Mkdir receiptsDir: %v", err)
	}

	now := time.Now().UTC()
	anchor := TrustAnchor{
		AnchorID:     anchorID,
		HumanActorID: humanActorID,
		PublicKey:    pub,
		NotBefore:    now.Add(-24 * time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		Purposes:     purposes,
	}

	anchorsDoc := struct {
		Version string        `json:"version"`
		Anchors []TrustAnchor `json:"anchors"`
	}{
		Version: "1.0",
		Anchors: []TrustAnchor{anchor},
	}
	anchorsBytes, err := json.Marshal(anchorsDoc)
	if err != nil {
		t.Fatalf("Marshal anchorsDoc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(operatorDir, "anchors.json"), anchorsBytes, 0o600); err != nil {
		t.Fatalf("WriteFile anchors.json: %v", err)
	}

	revokedDoc := struct {
		Version    string   `json:"version"`
		ReceiptIDs []string `json:"receipt_ids"`
		AnchorIDs  []string `json:"anchor_ids"`
	}{
		Version:    "1.0",
		ReceiptIDs: []string{},
		AnchorIDs:  []string{},
	}
	revokedBytes, err := json.Marshal(revokedDoc)
	if err != nil {
		t.Fatalf("Marshal revokedDoc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(operatorDir, "revoked.json"), revokedBytes, 0o600); err != nil {
		t.Fatalf("WriteFile revoked.json: %v", err)
	}

	fv, err := NewFileVerifier(FileOptions{
		OperatorDir:      operatorDir,
		ReceiptsDir:      receiptsDir,
		OperatorUID:      myUID,
		TrustedOwnerUIDs: []uint32{0, myUID},
		Clock:            clock.System(),
		CheckACL:         func(p string) error { return nil },
		RootDir:          operatorDir,
	})
	if err != nil {
		t.Fatalf("NewFileVerifier: %v", err)
	}

	return fv, operatorDir, receiptsDir
}

func TestVerify_Success(t *testing.T) {
	pub, priv := testKeypair(t)
	anchorID := "anchor_01"
	actorID := "operator-alice"

	anchor := testAnchor(pub, anchorID, actorID)
	v, err := NewVerifier([]TrustAnchor{anchor})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	stmt := validStatement(pub, anchorID, actorID)
	receipt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	v.receipts = append(v.receipts, receipt)

	req := Request{
		ReceiptID:     stmt.ReceiptID,
		Purpose:       stmt.Purpose,
		ProjectID:     stmt.ProjectID,
		Subject:       stmt.Subject,
		SubjectDigest: stmt.SubjectDigest,
		InputDigest:   stmt.InputDigest,
	}

	verified, err := v.Verify(context.Background(), req)
	if err != nil {
		t.Fatalf("Verify failed unexpectedly: %v", err)
	}
	if !verified.IsValid() {
		t.Errorf("verified.IsValid() = false, want true")
	}
	if verified.Statement().ReceiptID != stmt.ReceiptID {
		t.Errorf("verified.Statement().ReceiptID = %q, want %q", verified.Statement().ReceiptID, stmt.ReceiptID)
	}
}

func TestVerify_SignatureForgeryRejection(t *testing.T) {
	pub, priv := testKeypair(t)
	_, otherPriv := testKeypair(t)
	anchorID := "anchor_01"
	actorID := "operator-alice"

	anchor := testAnchor(pub, anchorID, actorID)

	stmt := validStatement(pub, anchorID, actorID)

	t.Run("signed with different key", func(t *testing.T) {
		forgedReceipt, err := stmt.Sign(otherPriv)
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		vf, err := NewVerifierWithReceipts([]TrustAnchor{anchor}, []Receipt{forgedReceipt})
		if err != nil {
			t.Fatalf("NewVerifierWithReceipts: %v", err)
		}
		req := Request{
			ReceiptID:     stmt.ReceiptID,
			Purpose:       stmt.Purpose,
			ProjectID:     stmt.ProjectID,
			Subject:       stmt.Subject,
			SubjectDigest: stmt.SubjectDigest,
			InputDigest:   stmt.InputDigest,
		}
		_, err = vf.Verify(context.Background(), req)
		if err == nil {
			t.Fatal("expected failure on forged signature, got nil")
		}
		if cat := errs.CategoryOf(err); cat != errs.CategoryPolicyDenied {
			t.Errorf("category = %v, want CategoryPolicyDenied", cat)
		}
	})

	t.Run("corrupted signature bytes", func(t *testing.T) {
		validReceipt, err := stmt.Sign(priv)
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		rawSig, _ := base64.StdEncoding.DecodeString(validReceipt.Signature)
		rawSig[0] ^= 0xff
		corruptedReceipt := Receipt{
			Statement: stmt,
			Signature: base64.StdEncoding.EncodeToString(rawSig),
		}
		vf, err := NewVerifierWithReceipts([]TrustAnchor{anchor}, []Receipt{corruptedReceipt})
		if err != nil {
			t.Fatalf("NewVerifierWithReceipts: %v", err)
		}
		req := Request{
			ReceiptID:     stmt.ReceiptID,
			Purpose:       stmt.Purpose,
			ProjectID:     stmt.ProjectID,
			Subject:       stmt.Subject,
			SubjectDigest: stmt.SubjectDigest,
			InputDigest:   stmt.InputDigest,
		}
		_, err = vf.Verify(context.Background(), req)
		if err == nil {
			t.Fatal("expected failure on corrupted signature, got nil")
		}
		if cat := errs.CategoryOf(err); cat != errs.CategoryPolicyDenied {
			t.Errorf("category = %v, want CategoryPolicyDenied", cat)
		}
	})
}

func TestVerify_ExpiredReceiptRejection(t *testing.T) {
	pub, priv := testKeypair(t)
	anchorID := "anchor_01"
	actorID := "operator-alice"

	anchor := testAnchor(pub, anchorID, actorID)
	v, err := NewVerifier([]TrustAnchor{anchor})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	stmt := validStatement(pub, anchorID, actorID)
	now := time.Now().UTC()
	stmt.IssuedAt = now.Add(-60 * time.Minute)
	stmt.NotAfter = now.Add(-5 * time.Minute)

	receipt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	v.receipts = append(v.receipts, receipt)

	req := Request{
		ReceiptID:     stmt.ReceiptID,
		Purpose:       stmt.Purpose,
		ProjectID:     stmt.ProjectID,
		Subject:       stmt.Subject,
		SubjectDigest: stmt.SubjectDigest,
		InputDigest:   stmt.InputDigest,
	}

	_, err = v.Verify(context.Background(), req)
	if err == nil {
		t.Fatal("expected failure on expired receipt, got nil")
	}
	if cat := errs.CategoryOf(err); cat != errs.CategoryPolicyDenied {
		t.Errorf("category = %v, want CategoryPolicyDenied", cat)
	}
}

func TestVerify_ExpiredAnchorRejection(t *testing.T) {
	pub, priv := testKeypair(t)
	anchorID := "anchor_01"
	actorID := "operator-alice"

	anchor := testAnchor(pub, anchorID, actorID)
	anchor.NotBefore = time.Now().UTC().Add(-2 * time.Hour)
	anchor.NotAfter = time.Now().UTC().Add(-1 * time.Hour)

	v, err := NewVerifier([]TrustAnchor{anchor})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	stmt := validStatement(pub, anchorID, actorID)
	now := time.Now().UTC()
	stmt.IssuedAt = now.Add(-10 * time.Minute)
	stmt.NotAfter = now.Add(45 * time.Minute)

	receipt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	v.receipts = append(v.receipts, receipt)

	req := Request{
		ReceiptID:     stmt.ReceiptID,
		Purpose:       stmt.Purpose,
		ProjectID:     stmt.ProjectID,
		Subject:       stmt.Subject,
		SubjectDigest: stmt.SubjectDigest,
		InputDigest:   stmt.InputDigest,
	}

	_, err = v.Verify(context.Background(), req)
	if err == nil {
		t.Fatal("expected failure on expired anchor, got nil")
	}
	if cat := errs.CategoryOf(err); cat != errs.CategoryPolicyDenied {
		t.Errorf("category = %v, want CategoryPolicyDenied", cat)
	}
}

func TestVerify_RevokedReceiptRejection(t *testing.T) {
	pub, priv := testKeypair(t)
	anchorID := "anchor_01"
	actorID := "operator-alice"

	anchor := testAnchor(pub, anchorID, actorID)

	stmt := validStatement(pub, anchorID, actorID)
	receipt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	revocations := &RevocationList{
		RevokedIDs: map[string]time.Time{
			stmt.ReceiptID: time.Now().UTC(),
		},
		RevokedAt: time.Now().UTC(),
	}

	v, err := NewVerifier([]TrustAnchor{anchor}, WithRevocationList(revocations))
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	v.receipts = append(v.receipts, receipt)

	req := Request{
		ReceiptID:     stmt.ReceiptID,
		Purpose:       stmt.Purpose,
		ProjectID:     stmt.ProjectID,
		Subject:       stmt.Subject,
		SubjectDigest: stmt.SubjectDigest,
		InputDigest:   stmt.InputDigest,
	}

	_, err = v.Verify(context.Background(), req)
	if err == nil {
		t.Fatal("expected failure on revoked receipt, got nil")
	}
	if cat := errs.CategoryOf(err); cat != errs.CategoryPolicyDenied {
		t.Errorf("category = %v, want CategoryPolicyDenied", cat)
	}
}

func TestVerify_PurposeSubjectProjectMismatchRejection(t *testing.T) {
	pub, priv := testKeypair(t)
	anchorID := "anchor_01"
	actorID := "operator-alice"

	anchor := testAnchor(pub, anchorID, actorID)

	stmt := validStatement(pub, anchorID, actorID)
	receipt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	cases := []struct {
		name string
		mut  func(r *Request)
	}{
		{
			name: "purpose mismatch",
			mut: func(r *Request) {
				r.Purpose = PurposeDiscoveryProductDecision
			},
		},
		{
			name: "project id mismatch",
			mut: func(r *Request) {
				r.ProjectID = "other-project"
			},
		},
		{
			name: "subject kind mismatch",
			mut: func(r *Request) {
				r.Subject.Kind = "OtherKind"
			},
		},
		{
			name: "subject id mismatch",
			mut: func(r *Request) {
				r.Subject.ID = "other-id"
			},
		},
		{
			name: "subject version mismatch",
			mut: func(r *Request) {
				r.Subject.Version = 2
			},
		},
		{
			name: "subject digest mismatch",
			mut: func(r *Request) {
				r.SubjectDigest = "sha256:different_subject_digest"
			},
		},
		{
			name: "input digest mismatch",
			mut: func(r *Request) {
				r.InputDigest = "different_digest"
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vf, err := NewVerifierWithReceipts([]TrustAnchor{anchor}, []Receipt{receipt})
			if err != nil {
				t.Fatalf("NewVerifierWithReceipts: %v", err)
			}
			req := Request{
				ReceiptID:     stmt.ReceiptID,
				Purpose:       stmt.Purpose,
				ProjectID:     stmt.ProjectID,
				Subject:       stmt.Subject,
				SubjectDigest: stmt.SubjectDigest,
				InputDigest:   stmt.InputDigest,
			}
			tc.mut(&req)
			_, err = vf.Verify(context.Background(), req)
			if err == nil {
				t.Fatalf("expected verification failure for %s, got nil", tc.name)
			}
			if cat := errs.CategoryOf(err); cat != errs.CategoryPolicyDenied {
				t.Errorf("category = %v, want CategoryPolicyDenied", cat)
			}
		})
	}
}

func TestVerify_AnchorHumanActorIDMismatchRejection(t *testing.T) {
	pub, priv := testKeypair(t)
	anchorID := "anchor_01"

	// Anchor belongs to alice
	anchor := testAnchor(pub, anchorID, "operator-alice")
	v, err := NewVerifier([]TrustAnchor{anchor})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	// Statement claims bob
	stmt := validStatement(pub, anchorID, "operator-bob")
	receipt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	v.receipts = append(v.receipts, receipt)

	req := Request{
		ReceiptID:     stmt.ReceiptID,
		Purpose:       stmt.Purpose,
		ProjectID:     stmt.ProjectID,
		Subject:       stmt.Subject,
		SubjectDigest: stmt.SubjectDigest,
		InputDigest:   stmt.InputDigest,
	}

	_, err = v.Verify(context.Background(), req)
	if err == nil {
		t.Fatal("expected failure on human actor id mismatch, got nil")
	}
	if cat := errs.CategoryOf(err); cat != errs.CategoryPolicyDenied {
		t.Errorf("category = %v, want CategoryPolicyDenied", cat)
	}
}

func TestConsumeOnce_GrantReceiptRejection(t *testing.T) {
	pub, priv := testKeypair(t)
	anchorID := "anchor_01"
	actorID := "operator-alice"

	anchor := testAnchor(pub, anchorID, actorID)
	v, err := NewVerifier([]TrustAnchor{anchor})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	stmt := validStatement(pub, anchorID, actorID)
	stmt.Purpose = PurposeExecutionPolicyActivate
	stmt.Use = UseGrant // grant receipt
	receipt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	v.receipts = append(v.receipts, receipt)

	req := Request{
		ReceiptID:     stmt.ReceiptID,
		Purpose:       stmt.Purpose,
		ProjectID:     stmt.ProjectID,
		Subject:       stmt.Subject,
		SubjectDigest: stmt.SubjectDigest,
		InputDigest:   stmt.InputDigest,
	}

	verified, err := v.Verify(context.Background(), req)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	_, _, err = ConsumeOnce(verified, "ExecutionPolicy", "pol-123")
	if err == nil {
		t.Fatal("ConsumeOnce must reject UseGrant receipt, got nil")
	}
	if cat := errs.CategoryOf(err); cat != errs.CategoryPolicyDenied {
		t.Errorf("category = %v, want CategoryPolicyDenied", cat)
	}
}

func TestConsumeOnce_BatchGuardReplayProtection(t *testing.T) {
	pub, priv := testKeypair(t)
	anchorID := "anchor_01"
	actorID := "operator-alice"

	anchor := testAnchor(pub, anchorID, actorID)
	v, err := NewVerifier([]TrustAnchor{anchor})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	stmt := validStatement(pub, anchorID, actorID)
	receipt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	v.receipts = append(v.receipts, receipt)

	req := Request{
		ReceiptID:     stmt.ReceiptID,
		Purpose:       stmt.Purpose,
		ProjectID:     stmt.ProjectID,
		Subject:       stmt.Subject,
		SubjectDigest: stmt.SubjectDigest,
		InputDigest:   stmt.InputDigest,
	}

	verified, err := v.Verify(context.Background(), req)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	guard, recordToStore, err := ConsumeOnce(verified, "HostPlan", "plan-123")
	if err != nil {
		t.Fatalf("ConsumeOnce: %v", err)
	}

	if recordToStore.Version != 1 {
		t.Errorf("recordToStore.Version = %d, want 1", recordToStore.Version)
	}
	rec, ok := recordToStore.Record.(*Consumption)
	if !ok {
		t.Fatalf("recordToStore.Record is not *Consumption, got %T", recordToStore.Record)
	}
	if rec.ReceiptID != stmt.ReceiptID {
		t.Errorf("rec.ReceiptID = %q, want %q", rec.ReceiptID, stmt.ReceiptID)
	}
	if rec.SubjectDigest != stmt.SubjectDigest {
		t.Errorf("rec.SubjectDigest = %q, want %q", rec.SubjectDigest, stmt.SubjectDigest)
	}
	if rec.AnchorID != stmt.AnchorID {
		t.Errorf("rec.AnchorID = %q, want %q", rec.AnchorID, stmt.AnchorID)
	}

	// First check: record does not exist -> pass
	view := &mockBatchReadView{
		records: make(map[string]storage.StoredRecord),
	}
	if err := guard.Check(context.Background(), view); err != nil {
		t.Fatalf("first check failed: %v", err)
	}

	// Simulate commit by adding stored record
	view.records["ReceiptConsumption:"+stmt.ReceiptID] = storage.StoredRecord{
		Kind:    "ReceiptConsumption",
		ID:      stmt.ReceiptID,
		Version: 1,
	}

	// Second check: record exists -> fails with receipt-replayed and CategoryConflict
	err = guard.Check(context.Background(), view)
	if err == nil {
		t.Fatal("second check succeeded, want receipt-replayed conflict error")
	}
	if cat := errs.CategoryOf(err); cat != errs.CategoryConflict {
		t.Errorf("category = %v, want CategoryConflict", cat)
	}
	if !containsStr(err.Error(), "receipt-replayed") {
		t.Errorf("error %q does not contain receipt-replayed", err.Error())
	}
}

func TestAuditGrantUse_SuccessAndRejection(t *testing.T) {
	pub, priv := testKeypair(t)
	anchorID := "anchor_01"
	actorID := "operator-alice"

	anchor := testAnchor(pub, anchorID, actorID)
	v, err := NewVerifier([]TrustAnchor{anchor})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	stmt := validStatement(pub, anchorID, actorID)
	stmt.Purpose = PurposeExecutionPolicyActivate
	stmt.Use = UseGrant
	receipt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	v.receipts = append(v.receipts, receipt)

	req := Request{
		ReceiptID:     stmt.ReceiptID,
		Purpose:       stmt.Purpose,
		ProjectID:     stmt.ProjectID,
		Subject:       stmt.Subject,
		SubjectDigest: stmt.SubjectDigest,
		InputDigest:   stmt.InputDigest,
	}

	verified, err := v.Verify(context.Background(), req)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	recToStore, err := AuditGrantUse(verified, "ExecutionPolicy", "policy-apply-1")
	if err != nil {
		t.Fatalf("AuditGrantUse failed: %v", err)
	}
	if recToStore.Version != 1 {
		t.Errorf("Version = %d, want 1", recToStore.Version)
	}
	rec, ok := recToStore.Record.(*Consumption)
	if !ok {
		t.Fatalf("Record is %T, want *Consumption", recToStore.Record)
	}
	expectedID := stmt.ReceiptID + ":policy-apply-1"
	if rec.ReceiptID != expectedID {
		t.Errorf("rec.ReceiptID = %q, want %q", rec.ReceiptID, expectedID)
	}
	if rec.SubjectDigest != stmt.SubjectDigest {
		t.Errorf("rec.SubjectDigest = %q, want %q", rec.SubjectDigest, stmt.SubjectDigest)
	}
	if rec.AnchorID != stmt.AnchorID {
		t.Errorf("rec.AnchorID = %q, want %q", rec.AnchorID, stmt.AnchorID)
	}

	// Try AuditGrantUse on a UseOnce receipt
	onceStmt := validStatement(pub, anchorID, actorID)
	onceStmt.ReceiptID = "rcpt_01once1234567890abcdef1234"
	onceStmt.Purpose = PurposeHostPlanApply
	onceStmt.Use = UseOnce
	onceReceipt, err := onceStmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	v.receipts = append(v.receipts, onceReceipt)
	onceReq := Request{
		ReceiptID:     onceStmt.ReceiptID,
		Purpose:       onceStmt.Purpose,
		ProjectID:     onceStmt.ProjectID,
		Subject:       onceStmt.Subject,
		SubjectDigest: onceStmt.SubjectDigest,
		InputDigest:   onceStmt.InputDigest,
	}
	onceVerified, err := v.Verify(context.Background(), onceReq)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	_, err = AuditGrantUse(onceVerified, "HostPlan", "policy-apply-2")
	if err == nil {
		t.Fatal("AuditGrantUse must reject UseOnce, got nil")
	}
	if cat := errs.CategoryOf(err); cat != errs.CategoryPolicyDenied {
		t.Errorf("category = %v, want CategoryPolicyDenied", cat)
	}
}

func TestZeroValueVerified_Rejection(t *testing.T) {
	var nilVerified Verified
	zeroVerified := verifiedReceipt{}

	if zeroVerified.IsValid() {
		t.Error("zeroVerified.IsValid() = true, want false")
	}
	if zeroVerified.Statement().ReceiptID != "" {
		t.Error("zeroVerified.Statement() returned non-empty statement")
	}

	// Test ConsumeOnce with nil Verified
	_, _, err := ConsumeOnce(nilVerified, "HostPlan", "123")
	if err == nil {
		t.Fatal("ConsumeOnce(nil) must reject, got nil")
	}
	if cat := errs.CategoryOf(err); cat != errs.CategoryPolicyDenied {
		t.Errorf("category = %v, want CategoryPolicyDenied", cat)
	}

	// Test ConsumeOnce with zero-value Verified
	_, _, err = ConsumeOnce(zeroVerified, "HostPlan", "123")
	if err == nil {
		t.Fatal("ConsumeOnce(zero) must reject, got nil")
	}
	if cat := errs.CategoryOf(err); cat != errs.CategoryPolicyDenied {
		t.Errorf("category = %v, want CategoryPolicyDenied", cat)
	}

	// Test AuditGrantUse with nil Verified
	_, err = AuditGrantUse(nilVerified, "HostPlan", "123")
	if err == nil {
		t.Fatal("AuditGrantUse(nil) must reject, got nil")
	}
	if cat := errs.CategoryOf(err); cat != errs.CategoryPolicyDenied {
		t.Errorf("category = %v, want CategoryPolicyDenied", cat)
	}

	// Test AuditGrantUse with zero-value Verified
	_, err = AuditGrantUse(zeroVerified, "HostPlan", "123")
	if err == nil {
		t.Fatal("AuditGrantUse(zero) must reject, got nil")
	}
	if cat := errs.CategoryOf(err); cat != errs.CategoryPolicyDenied {
		t.Errorf("category = %v, want CategoryPolicyDenied", cat)
	}
}

func TestStatementValidation(t *testing.T) {
	pub, _ := testKeypair(t)
	stmt := validStatement(pub, "anchor_01", "operator-alice")

	if err := stmt.Validate(); err != nil {
		t.Fatalf("valid statement failed validation: %v", err)
	}

	t.Run("invalid version", func(t *testing.T) {
		s := stmt
		s.Version = "2.0"
		if err := s.Validate(); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("missing prefix rcpt_", func(t *testing.T) {
		s := stmt
		s.ReceiptID = "invalid_id"
		if err := s.Validate(); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("not_after before issued_at", func(t *testing.T) {
		s := stmt
		s.NotAfter = s.IssuedAt.Add(-1 * time.Minute)
		if err := s.Validate(); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("text digest mismatch", func(t *testing.T) {
		s := stmt
		s.TextDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
		if err := s.Validate(); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestStatementValidation_PurposeTable(t *testing.T) {
	pub, _ := testKeypair(t)
	now := time.Now().UTC()

	t.Run("discovery.product_decision requires input digest", func(t *testing.T) {
		s := Statement{
			Version:       "1.0",
			ReceiptID:     "rcpt_01j7abc1234567890abcdef123",
			Purpose:       PurposeDiscoveryProductDecision,
			Use:           UseOnce,
			ProjectID:     "p1",
			Subject:       Subject{Kind: "ProductDecision", ID: "d1", Version: 1},
			SubjectDigest: "sha256:aaa",
			InputDigest:   "",
			Text:          "Decision text",
			TextDigest:    protocol.DigestBytes([]byte("Decision text")),
			HumanActorID:  "alice",
			AnchorID:      "a1",
			IssuedAt:      now.Add(-10 * time.Minute),
			NotAfter:      now.Add(30 * time.Minute),
		}
		if err := s.Validate(); err == nil {
			t.Fatal("expected error when InputDigest is empty for product_decision")
		}
		s.InputDigest = "sha256:input1"
		if err := s.Validate(); err != nil {
			t.Fatalf("unexpected error when InputDigest is present: %v", err)
		}
	})

	t.Run("host.plan_apply forbids input digest", func(t *testing.T) {
		s := validStatement(pub, "a1", "alice")
		s.InputDigest = "sha256:unexpected"
		if err := s.Validate(); err == nil {
			t.Fatal("expected error when InputDigest is set for host.plan_apply")
		}
		s.InputDigest = ""
		if err := s.Validate(); err != nil {
			t.Fatalf("unexpected error with empty InputDigest: %v", err)
		}
	})

	t.Run("discovery.ledger_resolution forbids input digest", func(t *testing.T) {
		s := Statement{
			Version:       "1.0",
			ReceiptID:     "rcpt_01j7abc1234567890abcdef123",
			Purpose:       PurposeDiscoveryLedgerResolution,
			Use:           UseOnce,
			ProjectID:     "p1",
			Subject:       Subject{Kind: "AmbiguityResolution", ID: "l1", Version: 1},
			SubjectDigest: "sha256:aaa",
			InputDigest:   "sha256:bad",
			Text:          "Resolve ambiguity",
			TextDigest:    protocol.DigestBytes([]byte("Resolve ambiguity")),
			HumanActorID:  "alice",
			AnchorID:      "a1",
			IssuedAt:      now.Add(-10 * time.Minute),
			NotAfter:      now.Add(30 * time.Minute),
		}
		if err := s.Validate(); err == nil {
			t.Fatal("expected error when InputDigest is set for ledger_resolution")
		}
		s.InputDigest = ""
		if err := s.Validate(); err != nil {
			t.Fatalf("unexpected error with empty InputDigest: %v", err)
		}
	})

	t.Run("use mismatch rejected", func(t *testing.T) {
		s := validStatement(pub, "a1", "alice")
		s.Use = UseGrant // host.plan_apply requires UseOnce
		if err := s.Validate(); err == nil {
			t.Fatal("expected error for UseGrant on host.plan_apply")
		}

		grantStmt := Statement{
			Version:       "1.0",
			ReceiptID:     "rcpt_01j7abc1234567890abcdef123",
			Purpose:       PurposeAcceptancePolicyActivate,
			Use:           UseOnce, // acceptance policy requires UseGrant
			ProjectID:     "p1",
			Subject:       Subject{Kind: "AcceptancePolicy", ID: "pol-1", Version: 1},
			SubjectDigest: "sha256:aaa",
			InputDigest:   "",
			Text:          "Activate policy",
			TextDigest:    protocol.DigestBytes([]byte("Activate policy")),
			HumanActorID:  "alice",
			AnchorID:      "a1",
			IssuedAt:      now.Add(-10 * time.Minute),
			NotAfter:      now.Add(24 * time.Hour),
		}
		if err := grantStmt.Validate(); err == nil {
			t.Fatal("expected error for UseOnce on acceptance.policy_activate")
		}
	})

	t.Run("validity duration limits", func(t *testing.T) {
		s := validStatement(pub, "a1", "alice")
		s.IssuedAt = now.Add(-10 * time.Minute)
		s.NotAfter = s.IssuedAt.Add(2 * time.Hour) // exceeds 1 hour max
		if err := s.Validate(); err == nil {
			t.Fatal("expected error when validity exceeds 1 hour for host.plan_apply")
		}

		grantStmt := Statement{
			Version:       "1.0",
			ReceiptID:     "rcpt_01j7abc1234567890abcdef123",
			Purpose:       PurposeAcceptancePolicyActivate,
			Use:           UseGrant,
			ProjectID:     "p1",
			Subject:       Subject{Kind: "AcceptancePolicy", ID: "pol-1", Version: 1},
			SubjectDigest: "sha256:aaa",
			InputDigest:   "",
			Text:          "Activate policy",
			TextDigest:    protocol.DigestBytes([]byte("Activate policy")),
			HumanActorID:  "alice",
			AnchorID:      "a1",
			IssuedAt:      now.Add(-1 * time.Hour),
			NotAfter:      now.Add(31 * 24 * time.Hour), // exceeds 30 days max
		}
		if err := grantStmt.Validate(); err == nil {
			t.Fatal("expected error when validity exceeds 30 days for acceptance.policy_activate")
		}
	})

	t.Run("future dated issued_at rejected", func(t *testing.T) {
		s := validStatement(pub, "a1", "alice")
		s.IssuedAt = now.Add(10 * time.Minute) // > 5 minutes in future
		s.NotAfter = s.IssuedAt.Add(30 * time.Minute)
		if err := s.Validate(); err == nil {
			t.Fatal("expected error when IssuedAt is more than 5 minutes in future")
		}
	})
}

func TestStatementValidation_TextBounds(t *testing.T) {
	pub, _ := testKeypair(t)

	t.Run("text over 4096 bytes rejected", func(t *testing.T) {
		s := validStatement(pub, "a1", "alice")
		s.Text = strings.Repeat("A", 4097)
		s.TextDigest = protocol.DigestBytes([]byte(s.Text))
		if err := s.Validate(); err == nil {
			t.Fatal("expected error for text > 4096 bytes")
		}
	})

	t.Run("text with carriage return rejected", func(t *testing.T) {
		s := validStatement(pub, "a1", "alice")
		s.Text = "Approve\rplan"
		s.TextDigest = protocol.DigestBytes([]byte(s.Text))
		if err := s.Validate(); err == nil {
			t.Fatal("expected error for text containing carriage return")
		}
	})

	t.Run("text with null byte rejected", func(t *testing.T) {
		s := validStatement(pub, "a1", "alice")
		s.Text = "Approve\x00plan"
		s.TextDigest = protocol.DigestBytes([]byte(s.Text))
		if err := s.Validate(); err == nil {
			t.Fatal("expected error for text containing null byte")
		}
	})

	t.Run("text with newline and tab accepted", func(t *testing.T) {
		s := validStatement(pub, "a1", "alice")
		s.Text = "Approve plan\n\tline 2"
		s.TextDigest = protocol.DigestBytes([]byte(s.Text))
		if err := s.Validate(); err != nil {
			t.Fatalf("unexpected error for text with newline and tab: %v", err)
		}
	})

	t.Run("text with invalid UTF-8 rejected", func(t *testing.T) {
		s := validStatement(pub, "a1", "alice")
		s.Text = "Invalid \xff\xfe bytes"
		s.TextDigest = protocol.DigestBytes([]byte(s.Text))
		if err := s.Validate(); err == nil {
			t.Fatal("expected error for non-UTF8 text")
		}
	})
}

func TestFileVerifier_RevokedReceiptAndAnchorRejection(t *testing.T) {
	pub, priv := testKeypair(t)
	anchorID := "anchor_fv_01"
	actorID := "operator-alice"

	fv, operatorDir, receiptsDir := setupTestFileVerifier(t, pub, anchorID, actorID, []Purpose{PurposeHostPlanApply})

	stmt := validStatement(pub, anchorID, actorID)
	receipt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	receiptBytes, err := json.Marshal(receipt)
	if err != nil {
		t.Fatalf("Marshal receipt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(receiptsDir, stmt.ReceiptID+".json"), receiptBytes, 0o600); err != nil {
		t.Fatalf("WriteFile receipt: %v", err)
	}

	req := Request{
		ReceiptID:     stmt.ReceiptID,
		Purpose:       stmt.Purpose,
		ProjectID:     stmt.ProjectID,
		Subject:       stmt.Subject,
		SubjectDigest: stmt.SubjectDigest,
	}

	// 1. Initially valid
	verified, err := fv.Verify(context.Background(), req)
	if err != nil {
		t.Fatalf("initial Verify failed: %v", err)
	}
	if !verified.IsValid() {
		t.Fatal("expected verified.IsValid() = true")
	}

	// 2. Revoke receipt ID in revoked.json
	revokedDoc := struct {
		Version    string   `json:"version"`
		ReceiptIDs []string `json:"receipt_ids"`
		AnchorIDs  []string `json:"anchor_ids"`
	}{
		Version:    "1.0",
		ReceiptIDs: []string{stmt.ReceiptID},
		AnchorIDs:  []string{},
	}
	revBytes, _ := json.Marshal(revokedDoc)
	if err := os.WriteFile(filepath.Join(operatorDir, "revoked.json"), revBytes, 0o600); err != nil {
		t.Fatalf("WriteFile revoked.json: %v", err)
	}

	_, err = fv.Verify(context.Background(), req)
	if err == nil {
		t.Fatal("expected failure after receipt revoked")
	}
	if cat := errs.CategoryOf(err); cat != errs.CategoryPolicyDenied {
		t.Errorf("category = %v, want CategoryPolicyDenied", cat)
	}

	// 3. Clear receipt ID revocation and revoke anchor ID
	revokedDoc.ReceiptIDs = []string{}
	revokedDoc.AnchorIDs = []string{anchorID}
	revBytes, _ = json.Marshal(revokedDoc)
	if err := os.WriteFile(filepath.Join(operatorDir, "revoked.json"), revBytes, 0o600); err != nil {
		t.Fatalf("WriteFile revoked.json: %v", err)
	}

	_, err = fv.Verify(context.Background(), req)
	if err == nil {
		t.Fatal("expected failure after anchor revoked")
	}
	if cat := errs.CategoryOf(err); cat != errs.CategoryPolicyDenied {
		t.Errorf("category = %v, want CategoryPolicyDenied", cat)
	}
}

func TestFileVerifier_PinnedAnchorsChangedRejection(t *testing.T) {
	pub, priv := testKeypair(t)
	anchorID := "anchor_fv_02"
	actorID := "operator-alice"

	fv, operatorDir, receiptsDir := setupTestFileVerifier(t, pub, anchorID, actorID, []Purpose{PurposeHostPlanApply})

	stmt := validStatement(pub, anchorID, actorID)
	receipt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	receiptBytes, err := json.Marshal(receipt)
	if err != nil {
		t.Fatalf("Marshal receipt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(receiptsDir, stmt.ReceiptID+".json"), receiptBytes, 0o600); err != nil {
		t.Fatalf("WriteFile receipt: %v", err)
	}

	req := Request{
		ReceiptID:     stmt.ReceiptID,
		Purpose:       stmt.Purpose,
		ProjectID:     stmt.ProjectID,
		Subject:       stmt.Subject,
		SubjectDigest: stmt.SubjectDigest,
	}

	// Succeeded initially
	_, err = fv.Verify(context.Background(), req)
	if err != nil {
		t.Fatalf("initial Verify failed: %v", err)
	}

	// Modify anchors.json on disk
	modifiedAnchorsDoc := struct {
		Version string        `json:"version"`
		Anchors []TrustAnchor `json:"anchors"`
	}{
		Version: "1.0",
		Anchors: []TrustAnchor{
			testAnchor(pub, anchorID, actorID),
		},
	}
	modifiedBytes, _ := json.MarshalIndent(modifiedAnchorsDoc, "", "  ")
	// Add comment/whitespace difference to alter digest
	modifiedBytes = append(modifiedBytes, []byte("\n")...)
	if err := os.WriteFile(filepath.Join(operatorDir, "anchors.json"), modifiedBytes, 0o600); err != nil {
		t.Fatalf("WriteFile modified anchors: %v", err)
	}

	_, err = fv.Verify(context.Background(), req)
	if err == nil {
		t.Fatal("expected failure when anchors.json modified on disk")
	}
	if cat := errs.CategoryOf(err); cat != errs.CategoryPolicyDenied {
		t.Errorf("category = %v, want CategoryPolicyDenied", cat)
	}
	if !containsStr(err.Error(), "anchors-changed") {
		t.Errorf("error %q does not contain anchors-changed", err.Error())
	}
}

func TestFileVerifier_MalformedJSONRejection(t *testing.T) {
	pub, priv := testKeypair(t)
	anchorID := "anchor_fv_03"
	actorID := "operator-alice"

	fv, operatorDir, receiptsDir := setupTestFileVerifier(t, pub, anchorID, actorID, []Purpose{PurposeHostPlanApply})

	stmt := validStatement(pub, anchorID, actorID)
	receipt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	t.Run("malformed receipt json", func(t *testing.T) {
		badID := "rcpt_01broken1234567890abcdef"
		badPath := filepath.Join(receiptsDir, badID+".json")
		if err := os.WriteFile(badPath, []byte("NOT_JSON{"), 0o600); err != nil {
			t.Fatal(err)
		}
		req := Request{
			ReceiptID:     badID,
			Purpose:       stmt.Purpose,
			ProjectID:     stmt.ProjectID,
			Subject:       stmt.Subject,
			SubjectDigest: stmt.SubjectDigest,
		}
		_, err := fv.Verify(context.Background(), req)
		if err == nil {
			t.Fatal("expected error on broken receipt JSON")
		}
	})

	t.Run("unknown fields in receipt json rejected via strict decode", func(t *testing.T) {
		unknownID := "rcpt_01unknown1234567890abcde"
		stmtUnknown := stmt
		stmtUnknown.ReceiptID = unknownID
		m := map[string]any{
			"statement": stmtUnknown,
			"signature": receipt.Signature,
			"injected":  "evil_extra_field",
		}
		bytesData, _ := json.Marshal(m)
		unknownPath := filepath.Join(receiptsDir, unknownID+".json")
		if err := os.WriteFile(unknownPath, bytesData, 0o600); err != nil {
			t.Fatal(err)
		}
		req := Request{
			ReceiptID:     unknownID,
			Purpose:       stmt.Purpose,
			ProjectID:     stmt.ProjectID,
			Subject:       stmt.Subject,
			SubjectDigest: stmt.SubjectDigest,
		}
		_, err := fv.Verify(context.Background(), req)
		if err == nil {
			t.Fatal("expected error on receipt JSON with unknown fields")
		}
	})

	t.Run("trailing content in receipt json rejected", func(t *testing.T) {
		trailingID := "rcpt_01trailing1234567890abcdef"
		stmtTrailing := stmt
		stmtTrailing.ReceiptID = trailingID
		rcptTrailing, err := stmtTrailing.Sign(priv)
		if err != nil {
			t.Fatal(err)
		}
		rcptBytes, _ := json.Marshal(rcptTrailing)
		rcptBytes = append(rcptBytes, []byte(" trailing_garbage")...)
		trailingPath := filepath.Join(receiptsDir, trailingID+".json")
		if err := os.WriteFile(trailingPath, rcptBytes, 0o600); err != nil {
			t.Fatal(err)
		}
		req := Request{
			ReceiptID:     trailingID,
			Purpose:       stmt.Purpose,
			ProjectID:     stmt.ProjectID,
			Subject:       stmt.Subject,
			SubjectDigest: stmt.SubjectDigest,
		}
		_, err = fv.Verify(context.Background(), req)
		if err == nil {
			t.Fatal("expected error on receipt JSON with trailing content")
		}
	})

	t.Run("unknown field in revoked.json rejected", func(t *testing.T) {
		m := map[string]any{
			"version":     "1.0",
			"receipt_ids": []string{},
			"anchor_ids":  []string{},
			"unknown":     "bad",
		}
		bytesData, _ := json.Marshal(m)
		if err := os.WriteFile(filepath.Join(operatorDir, "revoked.json"), bytesData, 0o600); err != nil {
			t.Fatal(err)
		}
		req := Request{
			ReceiptID:     stmt.ReceiptID,
			Purpose:       stmt.Purpose,
			ProjectID:     stmt.ProjectID,
			Subject:       stmt.Subject,
			SubjectDigest: stmt.SubjectDigest,
		}
		_, err := fv.Verify(context.Background(), req)
		if err == nil {
			t.Fatal("expected failure on revoked.json with unknown field")
		}
		if !containsStr(err.Error(), "revocation-unavailable") {
			t.Errorf("error %q does not contain revocation-unavailable", err.Error())
		}
	})
}

func TestFileVerifier_SubjectDiscovery(t *testing.T) {
	pub, priv := testKeypair(t)
	anchorID := "anchor_fv_04"
	actorID := "operator-alice"

	fv, _, receiptsDir := setupTestFileVerifier(t, pub, anchorID, actorID, []Purpose{PurposeHostPlanApply})

	now := time.Now().UTC()

	// Create older receipt (IssuedAt: now - 15m)
	olderStmt := validStatement(pub, anchorID, actorID)
	olderStmt.ReceiptID = "rcpt_01older1234567890abcdef123"
	olderStmt.IssuedAt = now.Add(-15 * time.Minute)
	olderStmt.NotAfter = now.Add(40 * time.Minute)
	olderRcpt, err := olderStmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign older: %v", err)
	}
	olderBytes, _ := json.Marshal(olderRcpt)
	if err := os.WriteFile(filepath.Join(receiptsDir, olderStmt.ReceiptID+".json"), olderBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	// Create newer receipt (IssuedAt: now - 2m)
	newerStmt := validStatement(pub, anchorID, actorID)
	newerStmt.ReceiptID = "rcpt_02newer1234567890abcdef123"
	newerStmt.IssuedAt = now.Add(-2 * time.Minute)
	newerStmt.NotAfter = now.Add(55 * time.Minute)
	newerRcpt, err := newerStmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign newer: %v", err)
	}
	newerBytes, _ := json.Marshal(newerRcpt)
	if err := os.WriteFile(filepath.Join(receiptsDir, newerStmt.ReceiptID+".json"), newerBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	// Subject discovery request (ReceiptID is empty)
	req := Request{
		ReceiptID:     "",
		Purpose:       olderStmt.Purpose,
		ProjectID:     olderStmt.ProjectID,
		Subject:       olderStmt.Subject,
		SubjectDigest: olderStmt.SubjectDigest,
	}

	verified, err := fv.Verify(context.Background(), req)
	if err != nil {
		t.Fatalf("discovery Verify failed: %v", err)
	}
	if verified.Statement().ReceiptID != newerStmt.ReceiptID {
		t.Errorf("discovered ReceiptID = %q, want newer receipt %q", verified.Statement().ReceiptID, newerStmt.ReceiptID)
	}
}

func TestNewFileVerifier_EmptyTrustedOwnerUIDsRejected(t *testing.T) {
	_, err := NewFileVerifier(FileOptions{
		OperatorDir:      t.TempDir(),
		ReceiptsDir:      t.TempDir(),
		OperatorUID:      2000,
		TrustedOwnerUIDs: []uint32{},
	})
	if err == nil {
		t.Fatal("expected error for empty TrustedOwnerUIDs, got nil")
	}
	if cat := errs.CategoryOf(err); cat != errs.CategoryInvalidArgument {
		t.Errorf("category = %v, want CategoryInvalidArgument", cat)
	}
}

func TestVerify_VerifierClockDeterminesValidity(t *testing.T) {
	pub, priv := testKeypair(t)
	anchorID := "anchor_clock_01"
	actorID := "operator-alice"

	anchor := testAnchor(pub, anchorID, actorID)
	// Base time T0
	t0 := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	mockClock := clock.NewFake(t0, 0)

	v, err := NewVerifier([]TrustAnchor{anchor}, WithClock(mockClock))
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	stmt := validStatement(pub, anchorID, actorID)
	stmt.ReceiptID = "rcpt_01clock1234567890abcdef123"
	stmt.IssuedAt = t0.Add(-10 * time.Minute)
	stmt.NotAfter = t0.Add(20 * time.Minute)
	receipt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	v.receipts = append(v.receipts, receipt)

	req := Request{
		ReceiptID:     stmt.ReceiptID,
		Purpose:       stmt.Purpose,
		ProjectID:     stmt.ProjectID,
		Subject:       stmt.Subject,
		SubjectDigest: stmt.SubjectDigest,
	}

	// 1. Valid at T0
	verified, err := v.Verify(context.Background(), req)
	if err != nil {
		t.Fatalf("expected valid at t0: %v", err)
	}
	if !verified.IsValid() {
		t.Fatal("expected verified.IsValid() = true")
	}

	// 2. Advance verifier clock past NotAfter (T0 + 25m) -> Expired, regardless of caller
	mockClock.Advance(25 * time.Minute)
	_, err = v.Verify(context.Background(), req)
	if err == nil {
		t.Fatal("expected expiry rejection when verifier clock advances past NotAfter, got nil")
	}
	if cat := errs.CategoryOf(err); cat != errs.CategoryPolicyDenied {
		t.Errorf("category = %v, want CategoryPolicyDenied", cat)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(sub) > 0 && searchSub(s, sub)))
}

func searchSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
