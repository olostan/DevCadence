package receipts

import (
	"context"
	"path/filepath"
	"strings"

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

// HostPlanApprovals implements principalhosts.ApprovalVerifier without reading receipts.
// Receipt lookup and strict decoding belong exclusively to the configured Verifier.
type HostPlanApprovals struct {
	Verifier  Verifier
	ProjectID string
}

var _ principalhosts.ApprovalVerifier = (*HostPlanApprovals)(nil)

// VerifyApproval recomputes the exact host-plan authority binding from consumer inputs
// and asks the verifier to validate the named receipt against that binding.
func (h *HostPlanApprovals) VerifyApproval(ctx context.Context, ref string, planDigest string, paths []string) error {
	if !IsValidReceiptID(ref) {
		return errs.New(errs.CategoryInvalidArgument, "invalid approval receipt id %q", ref)
	}
	if strings.TrimSpace(planDigest) == "" {
		return errs.New(errs.CategoryInvalidArgument, "plan digest is required")
	}
	if strings.TrimSpace(h.ProjectID) == "" {
		return errs.New(errs.CategoryInvalidArgument, "project id is required")
	}
	if h.Verifier == nil {
		return errs.New(errs.CategoryPolicyDenied, "no verifier configured for host plan approval")
	}

	for i, p := range paths {
		if !filepath.IsAbs(p) {
			return errs.New(errs.CategoryInvalidArgument, "path %q must be absolute", p)
		}
		if strings.ContainsRune(p, '\x00') {
			return errs.New(errs.CategoryInvalidArgument, "path %q contains NUL", p)
		}
		if filepath.Clean(p) != p {
			return errs.New(errs.CategoryInvalidArgument, "path %q is not clean", p)
		}
		for _, part := range strings.Split(filepath.ToSlash(p), "/") {
			if part == ".." {
				return errs.New(errs.CategoryInvalidArgument, "path %q contains ..", p)
			}
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

	scopeBytes, err := protocol.CanonicalJSON(HostPlanScope{PlanDigest: planDigest, Paths: paths})
	if err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "failed to compute canonical host plan scope")
	}

	req := Request{
		ReceiptID:     ref,
		Purpose:       PurposeHostPlanApply,
		ProjectID:     h.ProjectID,
		Subject:       Subject{Kind: "HostPlan", ID: planDigest, Version: 1},
		SubjectDigest: protocol.DigestBytes(scopeBytes),
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

// ReceiptSubject identifies the subject of a human discovery receipt and carries
// the consumer-recomputed digest of the authority-bearing artifact.
type ReceiptSubject struct {
	Kind          string `json:"kind"`
	ID            string `json:"id"`
	Version       int    `json:"version"`
	SubjectDigest string `json:"subject_digest"`
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

// HumanReceipts implements HumanReceiptVerifier without reading receipts.
// The consumer must supply the recomputed SubjectDigest in ReceiptSubject.
type HumanReceipts struct {
	Verifier Verifier
}

var _ HumanReceiptVerifier = (*HumanReceipts)(nil)

// Verify asks the verifier to validate the named receipt against only caller/consumer-derived values.
func (h *HumanReceipts) Verify(ctx context.Context, caller principal.CallerContext,
	receiptRef, purpose, inputDigest string, subject ReceiptSubject) (HumanReceipt, error) {
	if err := caller.Validate(); err != nil {
		return HumanReceipt{}, err
	}
	if !IsValidReceiptID(receiptRef) {
		return HumanReceipt{}, errs.New(errs.CategoryInvalidArgument, "invalid receipt reference %q", receiptRef)
	}
	if strings.TrimSpace(subject.SubjectDigest) == "" {
		return HumanReceipt{}, errs.New(errs.CategoryInvalidArgument, "subject digest is required")
	}
	if h.Verifier == nil {
		return HumanReceipt{}, errs.New(errs.CategoryPolicyDenied, "no verifier configured for human receipts")
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
	if RequiresInputDigest(p) && strings.TrimSpace(inputDigest) == "" {
		return HumanReceipt{}, errs.New(errs.CategoryInvalidArgument, "input digest is required for purpose %q", p)
	}

	req := Request{
		ReceiptID:     receiptRef,
		Purpose:       p,
		ProjectID:     caller.ProjectID,
		Subject:       Subject{Kind: subject.Kind, ID: subject.ID, Version: subject.Version},
		SubjectDigest: subject.SubjectDigest,
		InputDigest:   inputDigest,
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
