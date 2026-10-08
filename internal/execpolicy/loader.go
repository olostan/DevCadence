package execpolicy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/olostan/DevCadence/internal/operator/receipts"
	"github.com/olostan/DevCadence/internal/principal"
)

// PolicySource provides access to the current validated execution policy and its digest.
type PolicySource interface {
	Current(ctx context.Context) (ExecutionPolicy, string, error)
}

type filePolicySource struct {
	projectID    string
	path         string
	pinnedBytes  []byte
	pinnedDigest string
	verifier     receipts.Verifier
	receiptsDir  string
}

func policyUnavailable(detail string) error {
	return principal.NewCodedError(principal.CodeModelUnavailable, false, []string{"execution-policy-unavailable"}, detail)
}

// Load strictly loads and parses the execution policy at path, pins its bytes and canonical digest,
// and verifies its activation receipt against verifier.
func Load(ctx context.Context, projectID string, path string, receiptsDir string, verifier receipts.Verifier) (PolicySource, error) {
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, policyUnavailable(fmt.Sprintf("read policy file: %v", err))
	}

	dec := json.NewDecoder(bytes.NewReader(rawBytes))
	dec.DisallowUnknownFields()
	var policy ExecutionPolicy
	if err := dec.Decode(&policy); err != nil {
		return nil, policyUnavailable(fmt.Sprintf("decode policy: %v", err))
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, policyUnavailable("unexpected trailing content in policy file")
	}

	if err := policy.Validate(); err != nil {
		return nil, policyUnavailable(fmt.Sprintf("validate policy: %v", err))
	}

	canonicalDigest, err := policy.CanonicalDigest()
	if err != nil {
		return nil, policyUnavailable(fmt.Sprintf("canonical digest: %v", err))
	}

	src := &filePolicySource{
		projectID:    projectID,
		path:         path,
		pinnedBytes:  append([]byte(nil), rawBytes...),
		pinnedDigest: canonicalDigest,
		verifier:     verifier,
		receiptsDir:  receiptsDir,
	}

	if err := src.verify(ctx, policy, rawBytes); err != nil {
		return nil, err
	}

	return src, nil
}

func (s *filePolicySource) verify(ctx context.Context, policy ExecutionPolicy, rawBytes []byte) error {
	if sha256.Sum256(rawBytes) != sha256.Sum256(s.pinnedBytes) {
		return policyUnavailable("policy byte drift detected")
	}

	if s.verifier == nil {
		return policyUnavailable("verifier is nil")
	}

	if s.receiptsDir == "" {
		return policyUnavailable("receipts directory not configured")
	}

	req := receipts.Request{
		Purpose:   receipts.PurposeExecutionPolicyActivate,
		ProjectID: s.projectID,
		Subject: receipts.Subject{
			Kind:    "ExecutionPolicy",
			ID:      policy.PolicyID,
			Version: policy.Revision,
		},
		SubjectDigest: s.pinnedDigest,
	}

	verified, err := s.verifier.Verify(ctx, req)
	if err != nil {
		return policyUnavailable(fmt.Sprintf("verify receipt: %v", err))
	}
	if verified == nil || !verified.IsValid() {
		return policyUnavailable("verified receipt is not valid")
	}

	nb, err := time.Parse(time.RFC3339, policy.NotBefore)
	if err != nil {
		return policyUnavailable(fmt.Sprintf("parse not_before: %v", err))
	}
	na, err := time.Parse(time.RFC3339, policy.NotAfter)
	if err != nil {
		return policyUnavailable(fmt.Sprintf("parse not_after: %v", err))
	}
	now := time.Now().UTC()
	if now.Before(nb) || now.After(na) {
		return policyUnavailable(fmt.Sprintf("policy outside validity window [%s, %s] at %s", policy.NotBefore, policy.NotAfter, now.Format(time.RFC3339)))
	}

	return nil
}

func (s *filePolicySource) Current(ctx context.Context) (ExecutionPolicy, string, error) {
	diskBytes, err := os.ReadFile(s.path)
	if err != nil {
		return ExecutionPolicy{}, "", policyUnavailable(fmt.Sprintf("read policy file: %v", err))
	}
	if sha256.Sum256(diskBytes) != sha256.Sum256(s.pinnedBytes) {
		return ExecutionPolicy{}, "", policyUnavailable("policy byte drift detected")
	}

	dec := json.NewDecoder(bytes.NewReader(diskBytes))
	dec.DisallowUnknownFields()
	var policy ExecutionPolicy
	if err := dec.Decode(&policy); err != nil {
		return ExecutionPolicy{}, "", policyUnavailable(fmt.Sprintf("decode policy: %v", err))
	}
	if _, err := dec.Token(); err != io.EOF {
		return ExecutionPolicy{}, "", policyUnavailable("unexpected trailing content in policy file")
	}

	if err := policy.Validate(); err != nil {
		return ExecutionPolicy{}, "", policyUnavailable(fmt.Sprintf("validate policy: %v", err))
	}

	if err := s.verify(ctx, policy, diskBytes); err != nil {
		return ExecutionPolicy{}, "", err
	}

	return policy, s.pinnedDigest, nil
}
