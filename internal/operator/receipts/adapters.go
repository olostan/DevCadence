package receipts

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/principalhosts"
	"github.com/olostan/DevCadence/internal/protocol"
)

// HostPlanScope binds an approved host plan to its plan digest and exact target paths.
type HostPlanScope struct {
	PlanDigest string   `json:"plan_digest"`
	Paths      []string `json:"paths"`
}

// HostPlanApprovals wraps Verifier to implement principalhosts.ApprovalVerifier.
type HostPlanApprovals struct {
	Verifier    Verifier
	ReceiptsDir string
	ReadReceipt func(ref string) (Receipt, error)
	ProjectID   string
	Clock       clock.Clock
}

var _ principalhosts.ApprovalVerifier = (*HostPlanApprovals)(nil)

// VerifyApproval verifies an approval reference for host plan application.
// It verifies that paths are canonical and sorted, that the receipt purpose is
// PurposeHostPlanApply, subject is bound to the plan, and digests match.
func (h *HostPlanApprovals) VerifyApproval(ctx context.Context, ref string, planDigest string, paths []string) error {
	if strings.TrimSpace(ref) == "" {
		return errs.New(errs.CategoryInvalidArgument, "approval ref is required")
	}
	if strings.TrimSpace(planDigest) == "" {
		return errs.New(errs.CategoryInvalidArgument, "plan digest is required")
	}

	for i, p := range paths {
		if !filepath.IsAbs(p) {
			return errs.New(errs.CategoryInvalidArgument, "path %q must be absolute", p)
		}
		if strings.Contains(p, "\x00") {
			return errs.New(errs.CategoryInvalidArgument, "path %q contains NUL", p)
		}
		if filepath.Clean(p) != p {
			return errs.New(errs.CategoryInvalidArgument, "path %q is not clean", p)
		}
		if strings.Contains(p, "..") {
			return errs.New(errs.CategoryInvalidArgument, "path %q contains ..", p)
		}
		if i > 0 {
			if paths[i-1] == p {
				return errs.New(errs.CategoryInvalidArgument, "duplicate path %q", p)
			}
			if paths[i-1] > p {
				return errs.New(errs.CategoryInvalidArgument, "paths not sorted: %q > %q", paths[i-1], p)
			}
		}
	}

	scope := HostPlanScope{
		PlanDigest: planDigest,
		Paths:      paths,
	}
	scopeBytes, err := protocol.CanonicalJSON(scope)
	if err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "failed to compute canonical scope")
	}
	expectedScopeDigest := protocol.DigestBytes(scopeBytes)

	var receipt Receipt
	if h.ReadReceipt != nil {
		r, err := h.ReadReceipt(ref)
		if err != nil {
			return errs.Wrap(errs.CategoryPolicyDenied, err, "failed to read receipt %s", ref)
		}
		receipt = r
	} else {
		r, err := readReceiptFromDisk(h.ReceiptsDir, ref)
		if err != nil {
			return err
		}
		receipt = r
	}

	stmt := receipt.Statement
	if stmt.Purpose != PurposeHostPlanApply {
		return errs.New(errs.CategoryPolicyDenied, "purpose mismatch: receipt has %q, want %q", stmt.Purpose, PurposeHostPlanApply)
	}

	kindValid := stmt.Subject.Kind == "host_plan" || stmt.Subject.Kind == "HostPlan"
	idValid := stmt.Subject.ID == ref || stmt.Subject.ID == planDigest
	versionValid := stmt.Subject.Version == 1
	if !kindValid || !idValid || !versionValid {
		return errs.New(errs.CategoryPolicyDenied, "subject mismatch: receipt has %+v, want Kind=host_plan, ID=%s, Version=1", stmt.Subject, ref)
	}

	if stmt.SubjectDigest != expectedScopeDigest && stmt.SubjectDigest != planDigest {
		return errs.New(errs.CategoryPolicyDenied, "subject digest mismatch: receipt has %s, want %s or %s", stmt.SubjectDigest, expectedScopeDigest, planDigest)
	}

	if h.Verifier == nil {
		return errs.New(errs.CategoryPolicyDenied, "no verifier configured for host plan approval")
	}

	checkTime := time.Now().UTC()
	if h.Clock != nil {
		checkTime = h.Clock.Now()
	}
	projectID := stmt.ProjectID
	if h.ProjectID != "" {
		projectID = h.ProjectID
	}

	req := Request{
		Receipt:     receipt,
		ReceiptID:   ref,
		ReceiptsDir: h.ReceiptsDir,
		Purpose:     PurposeHostPlanApply,
		ProjectID:   projectID,
		Subject:     stmt.Subject,
		InputDigest: stmt.InputDigest,
		Text:        stmt.Text,
		Time:        checkTime,
	}

	verified, err := h.Verifier.Verify(ctx, req)
	if err != nil {
		return err
	}
	if !verified.IsValid() {
		return errs.New(errs.CategoryPolicyDenied, "host plan receipt is not valid")
	}
	return nil
}

// ReceiptSubject identifies the subject of a human discovery receipt.
type ReceiptSubject struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version int    `json:"version"`
}

// HumanReceipt represents the verified human ingress receipt returned to discovery consumers.
type HumanReceipt struct {
	HumanActorID string         `json:"human_actor_id"`
	SourceRef    string         `json:"source_ref"`
	Purpose      string         `json:"purpose"`
	InputDigest  string         `json:"input_digest"`
	Subject      ReceiptSubject `json:"subject"`
}

// HumanReceiptVerifier is the consumer interface for human discovery receipt verification.
type HumanReceiptVerifier interface {
	Verify(ctx context.Context, caller principal.CallerContext,
		receiptRef, purpose, inputDigest string, subject ReceiptSubject) (HumanReceipt, error)
}

// HumanReceipts implements HumanReceiptVerifier.
type HumanReceipts struct {
	Verifier    Verifier
	ReceiptsDir string
	ReadReceipt func(ref string) (Receipt, error)
	Clock       clock.Clock
}

var _ HumanReceiptVerifier = (*HumanReceipts)(nil)

// Verify verifies a human receipt for discovery mutations.
func (h *HumanReceipts) Verify(ctx context.Context, caller principal.CallerContext,
	receiptRef, purpose, inputDigest string, subject ReceiptSubject) (HumanReceipt, error) {
	if err := caller.Validate(); err != nil {
		return HumanReceipt{}, err
	}
	if !strings.HasPrefix(receiptRef, "rcpt_") {
		return HumanReceipt{}, errs.New(errs.CategoryInvalidArgument, "invalid receipt reference %q", receiptRef)
	}

	p := Purpose(purpose)
	switch p {
	case PurposeDiscoveryProductDecision,
		PurposeDiscoveryRequirementConfirm,
		PurposeDiscoveryLedgerResolution,
		PurposeDiscoveryReflection,
		PurposeDiscoveryAcceptedRisk:
	default:
		return HumanReceipt{}, errs.New(errs.CategoryPolicyDenied, "purpose %q is not an authorized human discovery purpose", purpose)
	}

	var receipt Receipt
	if h.ReadReceipt != nil {
		r, err := h.ReadReceipt(receiptRef)
		if err != nil {
			return HumanReceipt{}, errs.Wrap(errs.CategoryPolicyDenied, err, "failed to read receipt %s", receiptRef)
		}
		receipt = r
	} else {
		r, err := readReceiptFromDisk(h.ReceiptsDir, receiptRef)
		if err != nil {
			return HumanReceipt{}, err
		}
		receipt = r
	}

	stmt := receipt.Statement
	if stmt.ReceiptID != receiptRef {
		return HumanReceipt{}, errs.New(errs.CategoryPolicyDenied, "receipt_id mismatch: receipt has %s, request has %s", stmt.ReceiptID, receiptRef)
	}
	if stmt.ProjectID != caller.ProjectID {
		return HumanReceipt{}, errs.New(errs.CategoryPolicyDenied, "project_id mismatch: receipt has %q, caller has %q", stmt.ProjectID, caller.ProjectID)
	}
	if stmt.Purpose != p {
		return HumanReceipt{}, errs.New(errs.CategoryPolicyDenied, "purpose mismatch: receipt has %q, request has %q", stmt.Purpose, p)
	}
	if stmt.Subject.Kind != subject.Kind || stmt.Subject.ID != subject.ID || stmt.Subject.Version != subject.Version {
		return HumanReceipt{}, errs.New(errs.CategoryPolicyDenied, "subject mismatch: receipt has %+v, request has %+v", stmt.Subject, subject)
	}
	if inputDigest != "" && stmt.InputDigest != inputDigest {
		return HumanReceipt{}, errs.New(errs.CategoryPolicyDenied, "input_digest mismatch: receipt has %q, request has %q", stmt.InputDigest, inputDigest)
	}

	if h.Verifier == nil {
		return HumanReceipt{}, errs.New(errs.CategoryPolicyDenied, "no verifier configured for human receipts")
	}

	checkTime := time.Now().UTC()
	if h.Clock != nil {
		checkTime = h.Clock.Now()
	}

	req := Request{
		Receipt:     receipt,
		ReceiptID:   receiptRef,
		ReceiptsDir: h.ReceiptsDir,
		Purpose:     p,
		ProjectID:   caller.ProjectID,
		Subject:     Subject{Kind: subject.Kind, ID: subject.ID, Version: subject.Version},
		InputDigest: inputDigest,
		Text:        stmt.Text,
		Time:        checkTime,
	}

	verified, err := h.Verifier.Verify(ctx, req)
	if err != nil {
		return HumanReceipt{}, err
	}
	if !verified.IsValid() {
		return HumanReceipt{}, errs.New(errs.CategoryPolicyDenied, "human receipt is not valid")
	}

	return HumanReceipt{
		HumanActorID: verified.Statement().HumanActorID,
		SourceRef:    receiptRef,
		Purpose:      purpose,
		InputDigest:  inputDigest,
		Subject:      subject,
	}, nil
}

func readReceiptFromDisk(dir, ref string) (Receipt, error) {
	if !strings.HasPrefix(ref, "rcpt_") {
		return Receipt{}, errs.New(errs.CategoryInvalidArgument, "invalid receipt reference %q", ref)
	}
	if dir == "" {
		home := os.Getenv("DEVCADENCE_HOME")
		if home != "" {
			dir = filepath.Join(home, "receipts")
		}
	}
	if dir == "" {
		return Receipt{}, errs.New(errs.CategoryNotFound, "receipts directory not configured and DEVCADENCE_HOME not set")
	}
	path := filepath.Join(dir, ref+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			path = filepath.Join(dir, ref)
			data, err = os.ReadFile(path)
		}
		if err != nil {
			return Receipt{}, errs.Wrap(errs.CategoryNotFound, err, "receipt %s not found", ref)
		}
	}
	if len(data) > 64*1024 {
		return Receipt{}, errs.New(errs.CategoryPolicyDenied, "receipt %s exceeds 64 KiB size limit", ref)
	}
	var r Receipt
	if err := json.Unmarshal(data, &r); err != nil {
		return Receipt{}, errs.Wrap(errs.CategoryInvalidArgument, err, "invalid receipt json: %s", ref)
	}
	return r, nil
}
