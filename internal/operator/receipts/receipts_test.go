package receipts

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

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

func validStatement(pub ed25519.PublicKey, anchorID, humanActorID string) Statement {
	now := time.Now().UTC()
	text := "Approve host execution plan plan-123"
	return Statement{
		Version:       "1.0",
		ReceiptID:     "rcpt_01j7abc1234567890abcdef",
		Purpose:       PurposeHostPlanApply,
		Use:           UseOnce,
		ProjectID:     "test-project",
		Subject:       Subject{Kind: "HostPlan", ID: "sha256:1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff", Version: 1},
		SubjectDigest: "sha256:1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff",
		InputDigest:   "none",
		Text:          text,
		TextDigest:    protocol.DigestBytes([]byte(text)),
		HumanActorID:  humanActorID,
		AnchorID:      anchorID,
		IssuedAt:      now.Add(-10 * time.Minute),
		NotAfter:      now.Add(50 * time.Minute),
	}
}

func TestVerify_Success(t *testing.T) {
	pub, priv := testKeypair(t)
	anchorID := "anchor_01"
	actorID := "operator-alice"

	anchor := TrustAnchor{
		AnchorID:     anchorID,
		HumanActorID: actorID,
		PublicKey:    pub,
		NotAfter:     time.Now().UTC().Add(24 * time.Hour),
	}
	v, err := NewVerifier([]TrustAnchor{anchor})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	stmt := validStatement(pub, anchorID, actorID)
	receipt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	req := Request{
		Receipt:     receipt,
		Purpose:     stmt.Purpose,
		ProjectID:   stmt.ProjectID,
		Subject:     stmt.Subject,
		InputDigest: stmt.InputDigest,
		Text:        stmt.Text,
		Time:        time.Now().UTC(),
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

	anchor := TrustAnchor{
		AnchorID:     anchorID,
		HumanActorID: actorID,
		PublicKey:    pub,
		NotAfter:     time.Now().UTC().Add(24 * time.Hour),
	}
	v, err := NewVerifier([]TrustAnchor{anchor})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	stmt := validStatement(pub, anchorID, actorID)

	t.Run("signed with different key", func(t *testing.T) {
		forgedReceipt, err := stmt.Sign(otherPriv)
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		req := Request{
			Receipt:     forgedReceipt,
			Purpose:     stmt.Purpose,
			ProjectID:   stmt.ProjectID,
			Subject:     stmt.Subject,
			InputDigest: stmt.InputDigest,
			Text:        stmt.Text,
			Time:        time.Now().UTC(),
		}
		_, err = v.Verify(context.Background(), req)
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
		req := Request{
			Receipt:     corruptedReceipt,
			Purpose:     stmt.Purpose,
			ProjectID:   stmt.ProjectID,
			Subject:     stmt.Subject,
			InputDigest: stmt.InputDigest,
			Text:        stmt.Text,
			Time:        time.Now().UTC(),
		}
		_, err = v.Verify(context.Background(), req)
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

	anchor := TrustAnchor{
		AnchorID:     anchorID,
		HumanActorID: actorID,
		PublicKey:    pub,
		NotAfter:     time.Now().UTC().Add(24 * time.Hour),
	}
	v, err := NewVerifier([]TrustAnchor{anchor})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	stmt := validStatement(pub, anchorID, actorID)
	// Expired 5 minutes ago
	stmt.IssuedAt = time.Now().UTC().Add(-60 * time.Minute)
	stmt.NotAfter = time.Now().UTC().Add(-5 * time.Minute)

	receipt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	req := Request{
		Receipt:     receipt,
		Purpose:     stmt.Purpose,
		ProjectID:   stmt.ProjectID,
		Subject:     stmt.Subject,
		InputDigest: stmt.InputDigest,
		Text:        stmt.Text,
		Time:        time.Now().UTC(),
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

	// Anchor expired 1 hour ago
	anchor := TrustAnchor{
		AnchorID:     anchorID,
		HumanActorID: actorID,
		PublicKey:    pub,
		NotAfter:     time.Now().UTC().Add(-1 * time.Hour),
	}
	v, err := NewVerifier([]TrustAnchor{anchor})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	stmt := validStatement(pub, anchorID, actorID)
	// Receipt itself is inside its validity window
	stmt.IssuedAt = time.Now().UTC().Add(-10 * time.Minute)
	stmt.NotAfter = time.Now().UTC().Add(50 * time.Minute)

	receipt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	req := Request{
		Receipt:     receipt,
		Purpose:     stmt.Purpose,
		ProjectID:   stmt.ProjectID,
		Subject:     stmt.Subject,
		InputDigest: stmt.InputDigest,
		Text:        stmt.Text,
		Time:        time.Now().UTC(),
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

	anchor := TrustAnchor{
		AnchorID:     anchorID,
		HumanActorID: actorID,
		PublicKey:    pub,
		NotAfter:     time.Now().UTC().Add(24 * time.Hour),
	}

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

	req := Request{
		Receipt:     receipt,
		Purpose:     stmt.Purpose,
		ProjectID:   stmt.ProjectID,
		Subject:     stmt.Subject,
		InputDigest: stmt.InputDigest,
		Text:        stmt.Text,
		Time:        time.Now().UTC(),
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

	anchor := TrustAnchor{
		AnchorID:     anchorID,
		HumanActorID: actorID,
		PublicKey:    pub,
		NotAfter:     time.Now().UTC().Add(24 * time.Hour),
	}
	v, err := NewVerifier([]TrustAnchor{anchor})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

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
			name: "input digest mismatch",
			mut: func(r *Request) {
				r.InputDigest = "different_digest"
			},
		},
		{
			name: "text mismatch",
			mut: func(r *Request) {
				r.Text = "different text"
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := Request{
				Receipt:     receipt,
				Purpose:     stmt.Purpose,
				ProjectID:   stmt.ProjectID,
				Subject:     stmt.Subject,
				InputDigest: stmt.InputDigest,
				Text:        stmt.Text,
				Time:        time.Now().UTC(),
			}
			tc.mut(&req)
			_, err := v.Verify(context.Background(), req)
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
	anchor := TrustAnchor{
		AnchorID:     anchorID,
		HumanActorID: "operator-alice",
		PublicKey:    pub,
		NotAfter:     time.Now().UTC().Add(24 * time.Hour),
	}
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

	req := Request{
		Receipt:     receipt,
		Purpose:     stmt.Purpose,
		ProjectID:   stmt.ProjectID,
		Subject:     stmt.Subject,
		InputDigest: stmt.InputDigest,
		Text:        stmt.Text,
		Time:        time.Now().UTC(),
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

	anchor := TrustAnchor{
		AnchorID:     anchorID,
		HumanActorID: actorID,
		PublicKey:    pub,
		NotAfter:     time.Now().UTC().Add(24 * time.Hour),
	}
	v, err := NewVerifier([]TrustAnchor{anchor})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	stmt := validStatement(pub, anchorID, actorID)
	stmt.Use = UseGrant // grant receipt
	receipt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	req := Request{
		Receipt:     receipt,
		Purpose:     stmt.Purpose,
		ProjectID:   stmt.ProjectID,
		Subject:     stmt.Subject,
		InputDigest: stmt.InputDigest,
		Text:        stmt.Text,
		Time:        time.Now().UTC(),
	}

	verified, err := v.Verify(context.Background(), req)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	_, _, err = ConsumeOnce(verified, "HostPlan", "plan-123")
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

	anchor := TrustAnchor{
		AnchorID:     anchorID,
		HumanActorID: actorID,
		PublicKey:    pub,
		NotAfter:     time.Now().UTC().Add(24 * time.Hour),
	}
	v, err := NewVerifier([]TrustAnchor{anchor})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	stmt := validStatement(pub, anchorID, actorID)
	receipt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	req := Request{
		Receipt:     receipt,
		Purpose:     stmt.Purpose,
		ProjectID:   stmt.ProjectID,
		Subject:     stmt.Subject,
		InputDigest: stmt.InputDigest,
		Text:        stmt.Text,
		Time:        time.Now().UTC(),
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

	anchor := TrustAnchor{
		AnchorID:     anchorID,
		HumanActorID: actorID,
		PublicKey:    pub,
		NotAfter:     time.Now().UTC().Add(24 * time.Hour),
	}
	v, err := NewVerifier([]TrustAnchor{anchor})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	stmt := validStatement(pub, anchorID, actorID)
	stmt.Use = UseGrant
	receipt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	req := Request{
		Receipt:     receipt,
		Purpose:     stmt.Purpose,
		ProjectID:   stmt.ProjectID,
		Subject:     stmt.Subject,
		InputDigest: stmt.InputDigest,
		Text:        stmt.Text,
		Time:        time.Now().UTC(),
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

	// Try AuditGrantUse on a UseOnce receipt
	onceStmt := validStatement(pub, anchorID, actorID)
	onceReceipt, err := onceStmt.Sign(priv)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	onceReq := Request{
		Receipt:     onceReceipt,
		Purpose:     onceStmt.Purpose,
		ProjectID:   onceStmt.ProjectID,
		Subject:     onceStmt.Subject,
		InputDigest: onceStmt.InputDigest,
		Text:        onceStmt.Text,
		Time:        time.Now().UTC(),
	}
	onceVerified, err := v.Verify(context.Background(), onceReq)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	_, err = AuditGrantUse(onceVerified, "ExecutionPolicy", "policy-apply-2")
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
