package verifier

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/olostan/DevCadence/internal/benchmark/empirical"
	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/operator/receipts"
)

// CampaignAuthority adapts receipts.Verifier to empirical.OperatorAuthority (WP-M5-R3 Part B).
type CampaignAuthority struct {
	Verifier  receipts.Verifier
	ProjectID string
	Clock     clock.Clock
}

var _ empirical.OperatorAuthority = (*CampaignAuthority)(nil)

// NewCampaignAuthority constructs a CampaignAuthority wrapping an operator receipt verifier.
func NewCampaignAuthority(v receipts.Verifier, projectID string, clk clock.Clock) (*CampaignAuthority, error) {
	if v == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "campaign authority: verifier is required")
	}
	if clk == nil {
		clk = clock.System()
	}
	if projectID == "" {
		projectID = "devcadence"
	}
	return &CampaignAuthority{
		Verifier:  v,
		ProjectID: projectID,
		Clock:     clk,
	}, nil
}

// VerifyAuthorization verifies that authorization was issued through an authentic operator receipt
// bound to the plan digest, subject digest, and project.
func (ca *CampaignAuthority) VerifyAuthorization(ctx context.Context, planDigest, authorizationDigest string, authorization []byte) (empirical.AuthorityWindow, error) {
	if ca.Verifier == nil {
		return empirical.AuthorityWindow{}, errs.New(errs.CategoryPolicyDenied, "campaign authority: no receipt verifier configured")
	}
	if len(authorization) == 0 {
		return empirical.AuthorityWindow{}, errs.New(errs.CategoryInvalidArgument, "campaign authority: authorization bytes are empty")
	}

	// 1. Strictly decode authorization into empirical.CampaignAuthorization.
	dec := json.NewDecoder(bytes.NewReader(authorization))
	dec.DisallowUnknownFields()
	var authz empirical.CampaignAuthorization
	if err := dec.Decode(&authz); err != nil {
		return empirical.AuthorityWindow{}, errs.Wrap(errs.CategoryInvalidArgument, err, "campaign authority: invalid authorization json")
	}
	if _, err := dec.Token(); err != io.EOF {
		return empirical.AuthorityWindow{}, errs.New(errs.CategoryInvalidArgument, "campaign authority: trailing tokens in authorization")
	}

	if authz.PlanDigest != planDigest {
		return empirical.AuthorityWindow{}, errs.New(errs.CategoryPolicyDenied,
			"campaign authority: plan digest mismatch: authorization has %q, expected %q",
			authz.PlanDigest, planDigest)
	}

	clk := ca.Clock
	if clk == nil {
		clk = clock.System()
	}
	now := clk.Now().UTC()

	expiry, err := time.Parse(time.RFC3339Nano, authz.Expiry)
	if err != nil {
		expiry, err = time.Parse(time.RFC3339, authz.Expiry)
		if err != nil {
			return empirical.AuthorityWindow{}, errs.Wrap(errs.CategoryInvalidArgument, err,
				"campaign authority: invalid expiry timestamp %q", authz.Expiry)
		}
	}
	expiry = expiry.UTC()
	if !expiry.After(now) {
		return empirical.AuthorityWindow{}, errs.New(errs.CategoryPolicyDenied,
			"campaign authority: authorization expired at %s (current time: %s)",
			expiry.Format(time.RFC3339), now.Format(time.RFC3339))
	}

	// 2. Discover and verify receipt by subject lookup.
	projectID := ca.ProjectID
	if projectID == "" {
		projectID = "devcadence"
	}

	req := receipts.Request{
		ReceiptID: "",
		Purpose:   receipts.PurposeEmpiricalCampaignAuthorize,
		ProjectID: projectID,
		Subject: receipts.Subject{
			Kind:    "CampaignAuthorization",
			ID:      planDigest,
			Version: 1,
		},
		SubjectDigest: authorizationDigest,
	}

	verified, err := ca.Verifier.Verify(ctx, req)
	if err != nil {
		return empirical.AuthorityWindow{}, errs.Wrap(errs.CategoryPolicyDenied, err,
			"campaign authority: operator receipt verification failed")
	}
	if !verified.IsValid() {
		return empirical.AuthorityWindow{}, errs.New(errs.CategoryPolicyDenied,
			"campaign authority: operator receipt is not valid")
	}

	stmt := verified.Statement()
	if stmt.Subject.Kind != "CampaignAuthorization" || stmt.Subject.ID != planDigest || stmt.Subject.Version != 1 {
		return empirical.AuthorityWindow{}, errs.New(errs.CategoryPolicyDenied,
			"campaign authority: receipt subject mismatch: got %+v", stmt.Subject)
	}
	if stmt.SubjectDigest != authorizationDigest {
		return empirical.AuthorityWindow{}, errs.New(errs.CategoryPolicyDenied,
			"campaign authority: receipt subject_digest mismatch: got %q, expected %q",
			stmt.SubjectDigest, authorizationDigest)
	}
	if stmt.Purpose != receipts.PurposeEmpiricalCampaignAuthorize {
		return empirical.AuthorityWindow{}, errs.New(errs.CategoryPolicyDenied,
			"campaign authority: receipt purpose mismatch: got %q", stmt.Purpose)
	}

	// 3. Require Expiry <= receipt NotAfter.
	receiptNotAfter := stmt.NotAfter.UTC()
	if expiry.After(receiptNotAfter) {
		return empirical.AuthorityWindow{}, errs.New(errs.CategoryPolicyDenied,
			"campaign authority: authorization expiry (%s) exceeds receipt not_after (%s)",
			expiry.Format(time.RFC3339), receiptNotAfter.Format(time.RFC3339))
	}

	notAfter := expiry
	if receiptNotAfter.Before(notAfter) {
		notAfter = receiptNotAfter
	}

	return empirical.AuthorityWindow{
		IssuedAt: stmt.IssuedAt.UTC(),
		NotAfter: notAfter,
	}, nil
}
