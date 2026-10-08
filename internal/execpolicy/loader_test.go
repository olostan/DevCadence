package execpolicy_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/execpolicy"
	"github.com/olostan/DevCadence/internal/operator/receipts"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/protocol"
)

func testSetup(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey, *receipts.InMemoryVerifier, string, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	anchor := receipts.TrustAnchor{
		AnchorID:     "anchor-test",
		HumanActorID: "actor-test",
		PublicKey:    pub,
		NotBefore:    time.Now().UTC().Add(-1 * time.Hour),
		NotAfter:     time.Now().UTC().Add(24 * time.Hour),
		Purposes:     []receipts.Purpose{receipts.PurposeExecutionPolicyActivate},
	}

	tempDir := t.TempDir()
	receiptsDir := filepath.Join(tempDir, "receipts")
	if err := os.MkdirAll(receiptsDir, 0755); err != nil {
		t.Fatalf("MkdirAll receiptsDir: %v", err)
	}

	v, err := receipts.NewVerifier([]receipts.TrustAnchor{anchor}, receipts.WithReceiptsDir(receiptsDir))
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	return pub, priv, v, tempDir, receiptsDir
}

func createTestPolicy(now time.Time) execpolicy.ExecutionPolicy {
	return execpolicy.ExecutionPolicy{
		Version:            "1.0",
		PolicyID:           "pol-test-1",
		Revision:           1,
		NotBefore:          now.Add(-1 * time.Hour).Format(time.RFC3339),
		NotAfter:           now.Add(24 * time.Hour).Format(time.RFC3339),
		MaxAttemptsPerTask: 3,
		Grants: []execpolicy.EndpointGrant{
			{
				EndpointID:     "ep-local",
				ModelID:        "model-local",
				Roles:          []string{"implementer"},
				Locality:       protocol.LocalityLocal,
				SourceExposure: protocol.ExposureLocalOnly,
				ChannelKind:    protocol.ChannelCLISubprocess,
				Limits: execpolicy.ExecutionLimits{
					MaxTurns:               10,
					MaxToolCalls:           20,
					MaxTotalTokens:         100000,
					MaxDurationSeconds:     300,
					MaxOutputTokensPerCall: 4096,
					MaxRequestBytes:        65536,
					AllowUnknownUsage:      true,
					MaxAPISpendMicroUSD:    0,
				},
			},
			{
				EndpointID:            "ep-remote",
				ModelID:               "model-remote",
				Roles:                 []string{"reviewer"},
				Locality:              protocol.LocalityRemote,
				SourceExposure:        protocol.ExposureFocusedSnippets,
				AllowedNetworkDomains: []string{"api.example.com"},
				ChannelKind:           protocol.ChannelDirectHTTPAPI,
				Limits: execpolicy.ExecutionLimits{
					MaxTurns:               5,
					MaxToolCalls:           10,
					MaxTotalTokens:         50000,
					MaxDurationSeconds:     120,
					MaxOutputTokensPerCall: 2048,
					MaxRequestBytes:        32768,
					AllowUnknownUsage:      false,
					MaxAPISpendMicroUSD:    50000,
				},
			},
		},
	}
}

func signReceipt(t *testing.T, priv ed25519.PrivateKey, policy execpolicy.ExecutionPolicy, receiptsDir string, receiptID string, issuedAt, notAfter time.Time) receipts.Receipt {
	t.Helper()
	canonDigest, err := policy.CanonicalDigest()
	if err != nil {
		t.Fatalf("CanonicalDigest: %v", err)
	}

	text := "Activate policy " + policy.PolicyID
	stmt := receipts.Statement{
		Version:       "1.0",
		ReceiptID:     receiptID,
		Purpose:       receipts.PurposeExecutionPolicyActivate,
		Use:           receipts.UseGrant,
		ProjectID:     "proj-test",
		Subject:       receipts.Subject{Kind: "ExecutionPolicy", ID: policy.PolicyID, Version: policy.Revision},
		SubjectDigest: canonDigest,
		InputDigest:   "",
		Text:          text,
		TextDigest:    protocol.DigestBytes([]byte(text)),
		HumanActorID:  "actor-test",
		AnchorID:      "anchor-test",
		IssuedAt:      issuedAt,
		NotAfter:      notAfter,
	}

	rcpt, err := stmt.Sign(priv)
	if err != nil {
		t.Fatalf("stmt.Sign: %v", err)
	}

	data, err := json.Marshal(rcpt)
	if err != nil {
		t.Fatalf("json.Marshal rcpt: %v", err)
	}

	if err := os.WriteFile(filepath.Join(receiptsDir, receiptID+".json"), data, 0644); err != nil {
		t.Fatalf("WriteFile receipt: %v", err)
	}

	return rcpt
}

func TestPolicyLoadAndCurrent_Success(t *testing.T) {
	_, priv, v, tempDir, receiptsDir := testSetup(t)
	now := time.Now().UTC()

	policy := createTestPolicy(now)
	signReceipt(t, priv, policy, receiptsDir, "rcpt_01j7valid1234567890abcdef1", now.Add(-1*time.Hour), now.Add(24*time.Hour))

	policyPath := filepath.Join(tempDir, "policy.json")
	policyBytes, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent policy: %v", err)
	}
	if err := os.WriteFile(policyPath, policyBytes, 0644); err != nil {
		t.Fatalf("WriteFile policy: %v", err)
	}

	ctx := context.Background()
	src, err := execpolicy.Load(ctx, "proj-test", policyPath, receiptsDir, v)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	curPolicy, curDigest, err := src.Current(ctx)
	if err != nil {
		t.Fatalf("Current failed: %v", err)
	}

	if curPolicy.PolicyID != policy.PolicyID {
		t.Errorf("PolicyID = %q, want %q", curPolicy.PolicyID, policy.PolicyID)
	}
	if curPolicy.Revision != policy.Revision {
		t.Errorf("Revision = %d, want %d", curPolicy.Revision, policy.Revision)
	}
	if len(curPolicy.Grants) != 2 {
		t.Errorf("len(Grants) = %d, want 2", len(curPolicy.Grants))
	}

	expectedDigest, err := policy.CanonicalDigest()
	if err != nil {
		t.Fatalf("CanonicalDigest: %v", err)
	}
	if curDigest != expectedDigest {
		t.Errorf("Digest = %q, want %q", curDigest, expectedDigest)
	}
}

func TestPolicyLoad_ByteDriftFailsClosed(t *testing.T) {
	_, priv, v, tempDir, receiptsDir := testSetup(t)
	now := time.Now().UTC()

	policy := createTestPolicy(now)
	signReceipt(t, priv, policy, receiptsDir, "rcpt_01j7drift1234567890abcdef1", now.Add(-1*time.Hour), now.Add(24*time.Hour))

	policyPath := filepath.Join(tempDir, "policy.json")
	policyBytes, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent policy: %v", err)
	}
	if err := os.WriteFile(policyPath, policyBytes, 0644); err != nil {
		t.Fatalf("WriteFile policy: %v", err)
	}

	ctx := context.Background()
	src, err := execpolicy.Load(ctx, "proj-test", policyPath, receiptsDir, v)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Tamper with the policy file on disk
	tamperedBytes := append(policyBytes, []byte("\n ")...)
	if err := os.WriteFile(policyPath, tamperedBytes, 0644); err != nil {
		t.Fatalf("tampering WriteFile: %v", err)
	}

	_, _, err = src.Current(ctx)
	if err == nil {
		t.Fatalf("Current succeeded unexpectedly after byte drift")
	}

	var ce *principal.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *principal.CodedError, got %T: %v", err, err)
	}
	if ce.Code() != principal.CodeModelUnavailable {
		t.Errorf("Code = %q, want %q", ce.Code(), principal.CodeModelUnavailable)
	}
	if len(ce.EvidenceRefs()) != 1 || ce.EvidenceRefs()[0] != "execution-policy-unavailable" {
		t.Errorf("EvidenceRefs = %v, want ['execution-policy-unavailable']", ce.EvidenceRefs())
	}
}

func TestPolicyLoad_MissingReceiptFailsClosed(t *testing.T) {
	_, _, v, tempDir, receiptsDir := testSetup(t)
	now := time.Now().UTC()

	policy := createTestPolicy(now)
	// Do not create any receipt in receiptsDir

	policyPath := filepath.Join(tempDir, "policy.json")
	policyBytes, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("Marshal policy: %v", err)
	}
	if err := os.WriteFile(policyPath, policyBytes, 0644); err != nil {
		t.Fatalf("WriteFile policy: %v", err)
	}

	ctx := context.Background()
	_, err = execpolicy.Load(ctx, "proj-test", policyPath, receiptsDir, v)
	if err == nil {
		t.Fatalf("Load succeeded unexpectedly with missing receipt")
	}

	var ce *principal.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *principal.CodedError, got %T: %v", err, err)
	}
	if ce.Code() != principal.CodeModelUnavailable {
		t.Errorf("Code = %q, want %q", ce.Code(), principal.CodeModelUnavailable)
	}
	if len(ce.EvidenceRefs()) != 1 || ce.EvidenceRefs()[0] != "execution-policy-unavailable" {
		t.Errorf("EvidenceRefs = %v, want ['execution-policy-unavailable']", ce.EvidenceRefs())
	}
}

func TestPolicyLoad_RevokedAndExpiredReceiptFailsClosed(t *testing.T) {
	_, priv, v, tempDir, receiptsDir := testSetup(t)
	now := time.Now().UTC()

	receiptID := "rcpt_01j7revoked1234567890abcde"
	policy := createTestPolicy(now)
	signReceipt(t, priv, policy, receiptsDir, receiptID, now.Add(-1*time.Hour), now.Add(24*time.Hour))

	policyPath := filepath.Join(tempDir, "policy.json")
	policyBytes, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("Marshal policy: %v", err)
	}
	if err := os.WriteFile(policyPath, policyBytes, 0644); err != nil {
		t.Fatalf("WriteFile policy: %v", err)
	}

	ctx := context.Background()
	src, err := execpolicy.Load(ctx, "proj-test", policyPath, receiptsDir, v)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Revoke the receipt
	v.SetRevocationList(&receipts.RevocationList{
		RevokedIDs: map[string]time.Time{receiptID: now},
		RevokedAt:  now,
	})

	_, _, err = src.Current(ctx)
	if err == nil {
		t.Fatalf("Current succeeded unexpectedly after receipt revocation")
	}

	var ce *principal.CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *principal.CodedError, got %T: %v", err, err)
	}
	if ce.Code() != principal.CodeModelUnavailable {
		t.Errorf("Code = %q, want %q", ce.Code(), principal.CodeModelUnavailable)
	}
	if len(ce.EvidenceRefs()) != 1 || ce.EvidenceRefs()[0] != "execution-policy-unavailable" {
		t.Errorf("EvidenceRefs = %v, want ['execution-policy-unavailable']", ce.EvidenceRefs())
	}

	// Test expired receipt at load time
	t.Run("expired receipt rejected at Load", func(t *testing.T) {
		_, privExp, vExp, tempDirExp, receiptsDirExp := testSetup(t)
		expPolicy := createTestPolicy(now)
		expReceiptID := "rcpt_01j7expired1234567890abcde"
		// Receipt expired 5 minutes ago
		signReceipt(t, privExp, expPolicy, receiptsDirExp, expReceiptID, now.Add(-2*time.Hour), now.Add(-5*time.Minute))

		expPolicyPath := filepath.Join(tempDirExp, "policy.json")
		expPolicyBytes, _ := json.Marshal(expPolicy)
		if err := os.WriteFile(expPolicyPath, expPolicyBytes, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		_, err := execpolicy.Load(ctx, "proj-test", expPolicyPath, receiptsDirExp, vExp)
		if err == nil {
			t.Fatalf("Load succeeded unexpectedly with expired receipt")
		}

		var ceExp *principal.CodedError
		if !errors.As(err, &ceExp) {
			t.Fatalf("expected *principal.CodedError, got %T: %v", err, err)
		}
		if ceExp.Code() != principal.CodeModelUnavailable {
			t.Errorf("Code = %q, want %q", ceExp.Code(), principal.CodeModelUnavailable)
		}
	})
}

func TestLimitsAndPolicyValidation(t *testing.T) {
	t.Run("invalid limits: zero per-call output tokens", func(t *testing.T) {
		limits := execpolicy.ExecutionLimits{
			MaxTurns:               10,
			MaxToolCalls:           20,
			MaxTotalTokens:         100000,
			MaxDurationSeconds:     300,
			MaxOutputTokensPerCall: 0,
			MaxRequestBytes:        1000,
		}
		if err := limits.Validate(protocol.LocalityLocal); err == nil {
			t.Errorf("expected error for MaxOutputTokensPerCall = 0")
		}
	})

	t.Run("invalid limits: zero request bytes", func(t *testing.T) {
		limits := execpolicy.ExecutionLimits{
			MaxTurns:               10,
			MaxToolCalls:           20,
			MaxTotalTokens:         100000,
			MaxDurationSeconds:     300,
			MaxOutputTokensPerCall: 1000,
			MaxRequestBytes:        0,
		}
		if err := limits.Validate(protocol.LocalityLocal); err == nil {
			t.Errorf("expected error for MaxRequestBytes = 0")
		}
	})

	t.Run("invalid limits: zero max turns", func(t *testing.T) {
		limits := execpolicy.ExecutionLimits{
			MaxTurns:               0,
			MaxToolCalls:           20,
			MaxTotalTokens:         100000,
			MaxDurationSeconds:     300,
			MaxOutputTokensPerCall: 1000,
			MaxRequestBytes:        1000,
		}
		if err := limits.Validate(protocol.LocalityLocal); err == nil {
			t.Errorf("expected error for MaxTurns = 0")
		}
	})

	t.Run("invalid limits: zero max tool calls", func(t *testing.T) {
		limits := execpolicy.ExecutionLimits{
			MaxTurns:               10,
			MaxToolCalls:           0,
			MaxTotalTokens:         100000,
			MaxDurationSeconds:     300,
			MaxOutputTokensPerCall: 1000,
			MaxRequestBytes:        1000,
		}
		if err := limits.Validate(protocol.LocalityLocal); err == nil {
			t.Errorf("expected error for MaxToolCalls = 0")
		}
	})

	t.Run("invalid limits: zero max total tokens", func(t *testing.T) {
		limits := execpolicy.ExecutionLimits{
			MaxTurns:               10,
			MaxToolCalls:           20,
			MaxTotalTokens:         0,
			MaxDurationSeconds:     300,
			MaxOutputTokensPerCall: 1000,
			MaxRequestBytes:        1000,
		}
		if err := limits.Validate(protocol.LocalityLocal); err == nil {
			t.Errorf("expected error for MaxTotalTokens = 0")
		}
	})

	t.Run("invalid limits: zero max duration seconds", func(t *testing.T) {
		limits := execpolicy.ExecutionLimits{
			MaxTurns:               10,
			MaxToolCalls:           20,
			MaxTotalTokens:         100000,
			MaxDurationSeconds:     0,
			MaxOutputTokensPerCall: 1000,
			MaxRequestBytes:        1000,
		}
		if err := limits.Validate(protocol.LocalityLocal); err == nil {
			t.Errorf("expected error for MaxDurationSeconds = 0")
		}
	})

	t.Run("invalid limits: allow unknown usage on non-local grant", func(t *testing.T) {
		limits := execpolicy.ExecutionLimits{
			MaxTurns:               10,
			MaxToolCalls:           20,
			MaxTotalTokens:         100000,
			MaxDurationSeconds:     300,
			MaxOutputTokensPerCall: 1000,
			MaxRequestBytes:        1000,
			AllowUnknownUsage:      true,
		}
		if err := limits.Validate(protocol.LocalityRemote); err == nil {
			t.Errorf("expected error for AllowUnknownUsage on LocalityRemote")
		}
		if err := limits.Validate(protocol.LocalityRemoteInferenceLocalTools); err == nil {
			t.Errorf("expected error for AllowUnknownUsage on LocalityRemoteInferenceLocalTools")
		}
		if err := limits.Validate(protocol.LocalityLocal); err != nil {
			t.Errorf("unexpected error for AllowUnknownUsage on LocalityLocal: %v", err)
		}
	})

	t.Run("invalid limits: allow unknown usage with api spend micro usd > 0", func(t *testing.T) {
		limits := execpolicy.ExecutionLimits{
			MaxTurns:               10,
			MaxToolCalls:           20,
			MaxTotalTokens:         100000,
			MaxDurationSeconds:     300,
			MaxOutputTokensPerCall: 1000,
			MaxRequestBytes:        1000,
			AllowUnknownUsage:      true,
			MaxAPISpendMicroUSD:    500,
		}
		if err := limits.Validate(protocol.LocalityLocal); err == nil {
			t.Errorf("expected error for AllowUnknownUsage with MaxAPISpendMicroUSD > 0")
		}
	})

	t.Run("grant: local locality cannot have allowed network domains", func(t *testing.T) {
		grant := execpolicy.EndpointGrant{
			EndpointID:            "ep-1",
			ModelID:               "m-1",
			Roles:                 []string{"implementer"},
			Locality:              protocol.LocalityLocal,
			SourceExposure:        protocol.ExposureLocalOnly,
			AllowedNetworkDomains: []string{"example.com"},
			ChannelKind:           protocol.ChannelLocalDaemonSocket,
			Limits: execpolicy.ExecutionLimits{
				MaxTurns:               10,
				MaxToolCalls:           20,
				MaxTotalTokens:         100000,
				MaxDurationSeconds:     300,
				MaxOutputTokensPerCall: 100,
				MaxRequestBytes:        100,
			},
		}
		if err := grant.Validate(); err == nil {
			t.Errorf("expected error for local locality with allowed network domains")
		}
	})

	t.Run("policy: grants must be sorted", func(t *testing.T) {
		now := time.Now().UTC()
		policy := createTestPolicy(now)
		// Swap grants so they are unsorted
		policy.Grants[0], policy.Grants[1] = policy.Grants[1], policy.Grants[0]
		if err := policy.Validate(); err == nil {
			t.Errorf("expected error for unsorted grants")
		}
	})

	t.Run("policy: duplicate grants rejected", func(t *testing.T) {
		now := time.Now().UTC()
		policy := createTestPolicy(now)
		// Set duplicate grant
		policy.Grants[1].EndpointID = policy.Grants[0].EndpointID
		policy.Grants[1].ModelID = policy.Grants[0].ModelID
		if err := policy.Validate(); err == nil {
			t.Errorf("expected error for duplicate grants")
		}
	})

	t.Run("policy: invalid max attempts per task", func(t *testing.T) {
		now := time.Now().UTC()
		policy := createTestPolicy(now)
		policy.MaxAttemptsPerTask = 0
		if err := policy.Validate(); err == nil {
			t.Errorf("expected error for MaxAttemptsPerTask = 0")
		}
		policy.MaxAttemptsPerTask = 6
		if err := policy.Validate(); err == nil {
			t.Errorf("expected error for MaxAttemptsPerTask = 6")
		}
	})

	t.Run("policy: validity window > 30 days rejected", func(t *testing.T) {
		now := time.Now().UTC()
		policy := createTestPolicy(now)
		policy.NotAfter = now.Add(31 * 24 * time.Hour).Format(time.RFC3339)
		if err := policy.Validate(); err == nil {
			t.Errorf("expected error for validity duration > 30 days")
		}
	})

	t.Run("policy: non-UTC timestamp rejected", func(t *testing.T) {
		now := time.Now().UTC()
		policy := createTestPolicy(now)
		policy.NotBefore = "2026-10-07T12:00:00+02:00"
		if err := policy.Validate(); err == nil {
			t.Errorf("expected error for non-UTC NotBefore timestamp")
		}
	})
}
