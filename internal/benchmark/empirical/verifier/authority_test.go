package verifier_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/benchmark/empirical"
	"github.com/olostan/DevCadence/internal/benchmark/empirical/verifier"
	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/operator/receipts"
	"github.com/olostan/DevCadence/internal/protocol"
)

func setupTestAuthority(t *testing.T, now time.Time) (ed25519.PrivateKey, *receipts.InMemoryVerifier, receipts.TrustAnchor) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	anchor := receipts.TrustAnchor{
		AnchorID:     "anchor-op-01",
		HumanActorID: "actor-human-01",
		PublicKey:    pub,
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(30 * 24 * time.Hour),
		Purposes: []receipts.Purpose{
			receipts.PurposeEmpiricalCampaignAuthorize,
		},
	}

	inMemVerifier, err := receipts.NewVerifier([]receipts.TrustAnchor{anchor}, receipts.WithClock(clock.NewFake(now, 0)))
	if err != nil {
		t.Fatalf("NewVerifier failed: %v", err)
	}

	return priv, inMemVerifier, anchor
}

func signReceipt(
	t *testing.T,
	priv ed25519.PrivateKey,
	anchor receipts.TrustAnchor,
	planDigest, authDigest, projectID string,
	purpose receipts.Purpose,
	issuedAt, notAfter time.Time,
) receipts.Receipt {
	t.Helper()
	text := "Authorize empirical campaign"
	stmt := receipts.Statement{
		Version:   "1.0",
		ReceiptID: "rcpt_01j7abc1234567890abcdef123",
		Purpose:   purpose,
		Use:       receipts.UseGrant,
		ProjectID: projectID,
		Subject: receipts.Subject{
			Kind:    "CampaignAuthorization",
			ID:      planDigest,
			Version: 1,
		},
		SubjectDigest: authDigest,
		Text:          text,
		TextDigest:    protocol.DigestBytes([]byte(text)),
		HumanActorID:  anchor.HumanActorID,
		AnchorID:      anchor.AnchorID,
		IssuedAt:      issuedAt,
		NotAfter:      notAfter,
	}

	rcpt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("stmt.Sign failed: %v", err)
	}
	return rcpt
}

func makeAuthorization(planDigest string, expiry time.Time) ([]byte, string, error) {
	authz := empirical.CampaignAuthorization{
		Version:                 "1.0",
		PlanDigest:              planDigest,
		AuthorizedBy:            "Operator Alice",
		Expiry:                  expiry.Format(time.RFC3339),
		MaxTotalRuns:            10,
		MaxCallsPerRun:          4,
		MaxTotalCalls:           40,
		MaxRunSeconds:           600,
		MaxCampaignSeconds:      3600,
		MaxAPISpendMicroUSD:     5000000,
		MaxSubscriptionCalls:    0,
		MaxLocalComputeSeconds:  120.0,
		AllowedSourceClasses:    []string{"standard"},
		AllowedNetworkDomains:   []string{},
		AllowedEndpointBindings: []empirical.EndpointBinding{},
		CredentialRefs:          []string{},
		IssuedPolicyDigest:      "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	raw, err := json.Marshal(authz)
	if err != nil {
		return nil, "", err
	}
	digest := protocol.DigestBytes(raw)
	return raw, digest, nil
}

func TestCampaignAuthority_ValidGrant(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	priv, _, anchor := setupTestAuthority(t, now)

	planDigest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	authExpiry := now.Add(4 * time.Hour)
	authBytes, authDigest, err := makeAuthorization(planDigest, authExpiry)
	if err != nil {
		t.Fatalf("makeAuthorization failed: %v", err)
	}

	rcptNotAfter := now.Add(24 * time.Hour)
	rcpt := signReceipt(t, priv, anchor, planDigest, authDigest, "devcadence",
		receipts.PurposeEmpiricalCampaignAuthorize, now, rcptNotAfter)

	inMemWithRcpt, err := receipts.NewVerifierWithReceipts(
		[]receipts.TrustAnchor{anchor},
		[]receipts.Receipt{rcpt},
		receipts.WithClock(clock.NewFake(now, 0)),
	)
	if err != nil {
		t.Fatalf("NewVerifierWithReceipts failed: %v", err)
	}

	ca, err := verifier.NewCampaignAuthority(inMemWithRcpt, "devcadence", clock.NewFake(now, 0))
	if err != nil {
		t.Fatalf("NewCampaignAuthority failed: %v", err)
	}

	window, err := ca.VerifyAuthorization(ctx, planDigest, authDigest, authBytes)
	if err != nil {
		t.Fatalf("VerifyAuthorization failed: %v", err)
	}

	if !window.IssuedAt.Equal(now) {
		t.Errorf("expected IssuedAt %v, got %v", now, window.IssuedAt)
	}
	if !window.NotAfter.Equal(authExpiry) {
		t.Errorf("expected NotAfter %v, got %v", authExpiry, window.NotAfter)
	}
}

func TestCampaignAuthority_MismatchedPlanDigest(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	priv, _, anchor := setupTestAuthority(t, now)

	planDigest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	authExpiry := now.Add(4 * time.Hour)
	authBytes, authDigest, err := makeAuthorization(planDigest, authExpiry)
	if err != nil {
		t.Fatalf("makeAuthorization failed: %v", err)
	}

	rcpt := signReceipt(t, priv, anchor, planDigest, authDigest, "devcadence",
		receipts.PurposeEmpiricalCampaignAuthorize, now, now.Add(24*time.Hour))
	inMemWithRcpt, _ := receipts.NewVerifierWithReceipts(
		[]receipts.TrustAnchor{anchor}, []receipts.Receipt{rcpt}, receipts.WithClock(clock.NewFake(now, 0)))

	ca, _ := verifier.NewCampaignAuthority(inMemWithRcpt, "devcadence", clock.NewFake(now, 0))

	differentPlan := "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	if _, err := ca.VerifyAuthorization(ctx, differentPlan, authDigest, authBytes); err == nil {
		t.Fatalf("expected error for mismatched plan digest, got nil")
	}
}

func TestCampaignAuthority_MismatchedSubjectDigest(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	priv, _, anchor := setupTestAuthority(t, now)

	planDigest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	authExpiry := now.Add(4 * time.Hour)
	authBytes, authDigest, _ := makeAuthorization(planDigest, authExpiry)

	rcpt := signReceipt(t, priv, anchor, planDigest, authDigest, "devcadence",
		receipts.PurposeEmpiricalCampaignAuthorize, now, now.Add(24*time.Hour))
	inMemWithRcpt, _ := receipts.NewVerifierWithReceipts(
		[]receipts.TrustAnchor{anchor}, []receipts.Receipt{rcpt}, receipts.WithClock(clock.NewFake(now, 0)))

	ca, _ := verifier.NewCampaignAuthority(inMemWithRcpt, "devcadence", clock.NewFake(now, 0))

	diffAuthDigest := "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	if _, err := ca.VerifyAuthorization(ctx, planDigest, diffAuthDigest, authBytes); err == nil {
		t.Fatalf("expected error for mismatched authorization digest, got nil")
	}
}

func TestCampaignAuthority_ExpiredAuthorization(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	priv, _, anchor := setupTestAuthority(t, now)

	planDigest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	pastExpiry := now.Add(-10 * time.Minute)
	authBytes, authDigest, _ := makeAuthorization(planDigest, pastExpiry)

	rcpt := signReceipt(t, priv, anchor, planDigest, authDigest, "devcadence",
		receipts.PurposeEmpiricalCampaignAuthorize, now.Add(-time.Hour), now.Add(24*time.Hour))
	inMemWithRcpt, _ := receipts.NewVerifierWithReceipts(
		[]receipts.TrustAnchor{anchor}, []receipts.Receipt{rcpt}, receipts.WithClock(clock.NewFake(now, 0)))

	ca, _ := verifier.NewCampaignAuthority(inMemWithRcpt, "devcadence", clock.NewFake(now, 0))

	if _, err := ca.VerifyAuthorization(ctx, planDigest, authDigest, authBytes); err == nil {
		t.Fatalf("expected error for expired authorization, got nil")
	}
}

func TestCampaignAuthority_AuthorizationExpiryExceedsReceiptNotAfter(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	priv, _, anchor := setupTestAuthority(t, now)

	planDigest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	authExpiry := now.Add(10 * time.Hour)
	authBytes, authDigest, _ := makeAuthorization(planDigest, authExpiry)

	rcptNotAfter := now.Add(2 * time.Hour) // receipt expires before authorization
	rcpt := signReceipt(t, priv, anchor, planDigest, authDigest, "devcadence",
		receipts.PurposeEmpiricalCampaignAuthorize, now, rcptNotAfter)
	inMemWithRcpt, _ := receipts.NewVerifierWithReceipts(
		[]receipts.TrustAnchor{anchor}, []receipts.Receipt{rcpt}, receipts.WithClock(clock.NewFake(now, 0)))

	ca, _ := verifier.NewCampaignAuthority(inMemWithRcpt, "devcadence", clock.NewFake(now, 0))

	if _, err := ca.VerifyAuthorization(ctx, planDigest, authDigest, authBytes); err == nil {
		t.Fatalf("expected error when authorization expiry exceeds receipt NotAfter, got nil")
	}
}

func TestCampaignAuthority_RevokedReceipt(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	priv, _, anchor := setupTestAuthority(t, now)

	planDigest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	authExpiry := now.Add(4 * time.Hour)
	authBytes, authDigest, _ := makeAuthorization(planDigest, authExpiry)

	rcpt := signReceipt(t, priv, anchor, planDigest, authDigest, "devcadence",
		receipts.PurposeEmpiricalCampaignAuthorize, now, now.Add(24*time.Hour))

	revList := &receipts.RevocationList{
		RevokedIDs: map[string]time.Time{
			rcpt.Statement.ReceiptID: now,
		},
		RevokedAt: now,
	}

	inMemWithRcpt, _ := receipts.NewVerifierWithReceipts(
		[]receipts.TrustAnchor{anchor},
		[]receipts.Receipt{rcpt},
		receipts.WithClock(clock.NewFake(now, 0)),
		receipts.WithRevocationList(revList),
	)

	ca, _ := verifier.NewCampaignAuthority(inMemWithRcpt, "devcadence", clock.NewFake(now, 0))

	if _, err := ca.VerifyAuthorization(ctx, planDigest, authDigest, authBytes); err == nil {
		t.Fatalf("expected error for revoked receipt, got nil")
	}
}

func TestCampaignAuthority_ConstructorOptions(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	_, inMemVerifier, _ := setupTestAuthority(t, now)

	// Verifier nil returns error
	if _, err := verifier.NewCampaignAuthority(nil, "proj", nil); err == nil {
		t.Errorf("expected error when verifier is nil")
	}

	// Empty project and nil clock use defaults
	ca, err := verifier.NewCampaignAuthority(inMemVerifier, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ca.ProjectID != "devcadence" {
		t.Errorf("expected default projectID devcadence, got %q", ca.ProjectID)
	}
	if ca.Clock == nil {
		t.Errorf("expected non-nil default clock")
	}
}

func TestCampaignAuthority_PreconditionErrors(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	_, inMemVerifier, _ := setupTestAuthority(t, now)

	// ca.Verifier == nil
	caNilVerifier := &verifier.CampaignAuthority{Verifier: nil}
	if _, err := caNilVerifier.VerifyAuthorization(ctx, "plan", "auth", []byte("{}")); err == nil {
		t.Errorf("expected error when ca.Verifier is nil")
	}

	ca, err := verifier.NewCampaignAuthority(inMemVerifier, "devcadence", clock.NewFake(now, 0))
	if err != nil {
		t.Fatalf("NewCampaignAuthority failed: %v", err)
	}

	// Empty authorization
	if _, err := ca.VerifyAuthorization(ctx, "plan", "auth", nil); err == nil {
		t.Errorf("expected error for empty authorization")
	}

	// Invalid json
	if _, err := ca.VerifyAuthorization(ctx, "plan", "auth", []byte("bad-json")); err == nil {
		t.Errorf("expected error for invalid json")
	}

	// Trailing tokens
	planDigest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	authBytes, authDigest, _ := makeAuthorization(planDigest, now.Add(time.Hour))
	trailing := append(authBytes, []byte(" trailing")...)
	if _, err := ca.VerifyAuthorization(ctx, planDigest, authDigest, trailing); err == nil {
		t.Errorf("expected error for trailing tokens in authorization")
	}
}

func TestCampaignAuthority_InvalidExpiry(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	_, inMemVerifier, _ := setupTestAuthority(t, now)

	ca, _ := verifier.NewCampaignAuthority(inMemVerifier, "devcadence", clock.NewFake(now, 0))

	authz := empirical.CampaignAuthorization{
		Version:      "1.0",
		PlanDigest:   "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		AuthorizedBy: "Operator Alice",
		Expiry:       "not-a-timestamp",
	}
	raw, _ := json.Marshal(authz)
	digest := protocol.DigestBytes(raw)

	if _, err := ca.VerifyAuthorization(ctx, authz.PlanDigest, digest, raw); err == nil {
		t.Errorf("expected error for invalid expiry timestamp")
	}
}

func TestCampaignAuthority_ReceiptSubjectAndPurposeMismatch(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	priv, _, anchor := setupTestAuthority(t, now)
	anchor.Purposes = append(anchor.Purposes, receipts.PurposeExecutionPolicyActivate)

	planDigest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	authExpiry := now.Add(4 * time.Hour)
	authBytes, authDigest, _ := makeAuthorization(planDigest, authExpiry)

	// Sign receipt with different purpose
	rcpt := signReceipt(t, priv, anchor, planDigest, authDigest, "devcadence",
		receipts.PurposeExecutionPolicyActivate, now, now.Add(24*time.Hour))
	inMemWithRcpt, _ := receipts.NewVerifierWithReceipts(
		[]receipts.TrustAnchor{anchor}, []receipts.Receipt{rcpt}, receipts.WithClock(clock.NewFake(now, 0)))

	ca, _ := verifier.NewCampaignAuthority(inMemWithRcpt, "devcadence", clock.NewFake(now, 0))
	if _, err := ca.VerifyAuthorization(ctx, planDigest, authDigest, authBytes); err == nil {
		t.Errorf("expected error when receipt purpose does not match")
	}
}

func TestCampaignAuthority_DefaultProjectAndClockDuringVerify(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	priv, _, anchor := setupTestAuthority(t, now)

	planDigest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	// Set expiry far in future so clock.System() (which is ~current year 2026) is before expiry
	authExpiry := time.Now().Add(24 * time.Hour)
	authBytes, authDigest, _ := makeAuthorization(planDigest, authExpiry)

	rcpt := signReceipt(t, priv, anchor, planDigest, authDigest, "devcadence",
		receipts.PurposeEmpiricalCampaignAuthorize, time.Now().Add(-time.Hour), time.Now().Add(48*time.Hour))
	inMemWithRcpt, _ := receipts.NewVerifierWithReceipts(
		[]receipts.TrustAnchor{anchor}, []receipts.Receipt{rcpt})

	// Explicitly clear ProjectID and Clock to exercise fallbacks in VerifyAuthorization
	ca := &verifier.CampaignAuthority{
		Verifier:  inMemWithRcpt,
		ProjectID: "",
		Clock:     nil,
	}

	window, err := ca.VerifyAuthorization(ctx, planDigest, authDigest, authBytes)
	if err != nil {
		t.Fatalf("unexpected error with default project and clock: %v", err)
	}
	if window.NotAfter.IsZero() {
		t.Errorf("expected non-zero NotAfter")
	}
}

type mockReceiptVerifier struct {
	verified receipts.Verified
	err      error
}

func (m mockReceiptVerifier) Verify(ctx context.Context, req receipts.Request) (receipts.Verified, error) {
	return m.verified, m.err
}

type mockVerified struct {
	stmt  receipts.Statement
	valid bool
}

func (m mockVerified) Statement() receipts.Statement { return m.stmt }
func (m mockVerified) IsValid() bool                 { return m.valid }

func TestCampaignAuthority_VerifiedReceiptBranches(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	planDigest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	authExpiry := now.Add(4 * time.Hour)
	authBytes, authDigest, _ := makeAuthorization(planDigest, authExpiry)

	validStmt := receipts.Statement{
		Purpose: receipts.PurposeEmpiricalCampaignAuthorize,
		Subject: receipts.Subject{
			Kind:    "CampaignAuthorization",
			ID:      planDigest,
			Version: 1,
		},
		SubjectDigest: authDigest,
		IssuedAt:      now,
		NotAfter:      now.Add(10 * time.Hour),
	}

	// 1. verified.IsValid() == false
	{
		ca := &verifier.CampaignAuthority{
			Verifier: mockReceiptVerifier{verified: mockVerified{valid: false}},
			Clock:    clock.NewFake(now, 0),
		}
		if _, err := ca.VerifyAuthorization(ctx, planDigest, authDigest, authBytes); err == nil {
			t.Errorf("expected error when verified.IsValid is false")
		}
	}

	// 2. stmt.Subject mismatch (Kind)
	{
		stmt := validStmt
		stmt.Subject.Kind = "OtherKind"
		ca := &verifier.CampaignAuthority{
			Verifier: mockReceiptVerifier{verified: mockVerified{valid: true, stmt: stmt}},
			Clock:    clock.NewFake(now, 0),
		}
		if _, err := ca.VerifyAuthorization(ctx, planDigest, authDigest, authBytes); err == nil {
			t.Errorf("expected error when stmt.Subject.Kind mismatches")
		}
	}

	// 3. stmt.SubjectDigest mismatch
	{
		stmt := validStmt
		stmt.SubjectDigest = "sha256:different"
		ca := &verifier.CampaignAuthority{
			Verifier: mockReceiptVerifier{verified: mockVerified{valid: true, stmt: stmt}},
			Clock:    clock.NewFake(now, 0),
		}
		if _, err := ca.VerifyAuthorization(ctx, planDigest, authDigest, authBytes); err == nil {
			t.Errorf("expected error when stmt.SubjectDigest mismatches")
		}
	}

	// 4. stmt.Purpose mismatch
	{
		stmt := validStmt
		stmt.Purpose = receipts.PurposeExecutionPolicyActivate
		ca := &verifier.CampaignAuthority{
			Verifier: mockReceiptVerifier{verified: mockVerified{valid: true, stmt: stmt}},
			Clock:    clock.NewFake(now, 0),
		}
		if _, err := ca.VerifyAuthorization(ctx, planDigest, authDigest, authBytes); err == nil {
			t.Errorf("expected error when stmt.Purpose mismatches")
		}
	}

	// 5. receiptNotAfter <= authExpiry bounds check
	{
		stmt := validStmt
		stmt.NotAfter = authExpiry // exactly equals authExpiry
		ca := &verifier.CampaignAuthority{
			Verifier: mockReceiptVerifier{verified: mockVerified{valid: true, stmt: stmt}},
			Clock:    clock.NewFake(now, 0),
		}
		win, err := ca.VerifyAuthorization(ctx, planDigest, authDigest, authBytes)
		if err != nil {
			t.Fatalf("unexpected error when notAfter matches: %v", err)
		}
		if !win.NotAfter.Equal(authExpiry) {
			t.Errorf("expected NotAfter %v, got %v", authExpiry, win.NotAfter)
		}
	}
}
