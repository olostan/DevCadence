package receipts

import (
	"context"
	"errors"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/protocol"
)

type captureVerifier struct {
	req Request
}

func (v *captureVerifier) Verify(_ context.Context, req Request) (Verified, error) {
	v.req = req
	return verifiedReceipt{stmt: Statement{HumanActorID: "operator-alice"}, valid: true}, nil
}

type denyingVerifier struct{}

func (*denyingVerifier) Verify(context.Context, Request) (Verified, error) {
	return nil, errors.New("verify denied")
}

type invalidVerifier struct{}

func (*invalidVerifier) Verify(context.Context, Request) (Verified, error) {
	return verifiedReceipt{}, nil
}

func TestHostPlanApprovalsDerivesCanonicalRequest(t *testing.T) {
	ref := "rcpt_01j7abc1234567890abcdef123"
	planDigest := "sha256:1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff"
	paths := []string{"/workspace/a.txt", "/workspace/b.txt"}
	v := &captureVerifier{}
	h := &HostPlanApprovals{Verifier: v, ProjectID: "project-1"}

	if err := h.VerifyApproval(context.Background(), ref, planDigest, paths); err != nil {
		t.Fatalf("VerifyApproval: %v", err)
	}

	scopeBytes, err := protocol.CanonicalJSON(HostPlanScope{PlanDigest: planDigest, Paths: paths})
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	want := Request{
		ReceiptID:     ref,
		Purpose:       PurposeHostPlanApply,
		ProjectID:     "project-1",
		Subject:       Subject{Kind: "HostPlan", ID: planDigest, Version: 1},
		SubjectDigest: protocol.DigestBytes(scopeBytes),
	}
	if v.req != want {
		t.Fatalf("request = %+v, want %+v", v.req, want)
	}
}

func TestHostPlanApprovalsRejectsAmbiguousOrInvalidInputs(t *testing.T) {
	ref := "rcpt_01j7abc1234567890abcdef123"
	plan := "sha256:1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff"
	v := &captureVerifier{}

	if err := (&HostPlanApprovals{Verifier: v}).VerifyApproval(context.Background(), ref, plan, []string{"/a"}); err == nil {
		t.Fatal("expected missing project error")
	}
	if err := (&HostPlanApprovals{Verifier: v, ProjectID: "p"}).VerifyApproval(context.Background(), "bad", plan, []string{"/a"}); err == nil {
		t.Fatal("expected receipt id error")
	}
	if err := (&HostPlanApprovals{Verifier: v, ProjectID: "p"}).VerifyApproval(context.Background(), ref, plan, []string{"/b", "/a"}); err == nil {
		t.Fatal("expected unsorted path error")
	}
	if err := (&HostPlanApprovals{Verifier: v, ProjectID: "p"}).VerifyApproval(context.Background(), ref, plan, []string{"/a/../b"}); err == nil {
		t.Fatal("expected non-canonical path error")
	}
	if err := (&HostPlanApprovals{Verifier: &invalidVerifier{}, ProjectID: "p"}).VerifyApproval(context.Background(), ref, plan, []string{"/a"}); err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Fatalf("invalid verifier result error = %v", err)
	}
}

func TestHostPlanApprovalsPropagatesVerifierFailure(t *testing.T) {
	ref := "rcpt_01j7abc1234567890abcdef123"
	plan := "sha256:1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff"
	err := (&HostPlanApprovals{Verifier: &denyingVerifier{}, ProjectID: "p"}).VerifyApproval(context.Background(), ref, plan, []string{"/a"})
	if err == nil || err.Error() != "verify denied" {
		t.Fatalf("error = %v", err)
	}
}

func TestHumanReceiptsUsesConsumerDigestOnly(t *testing.T) {
	ref := "rcpt_01j7abc1234567890abcdef123"
	digest := "sha256:1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff"
	input := "sha256:222233334444555566667777888899990000aaaabbbbccccddddeeeeffff0000"
	subject := ReceiptSubject{
		Kind:          "ProblemModelReflection",
		ID:            "pm-1",
		Version:       7,
		SubjectDigest: digest,
	}
	v := &captureVerifier{}
	h := &HumanReceipts{Verifier: v}
	caller := principal.CallerContext{PrincipalID: "principal-1", ProjectID: "project-1"}

	got, err := h.Verify(context.Background(), caller, ref, string(PurposeDiscoveryReflection), input, subject)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	want := Request{
		ReceiptID:     ref,
		Purpose:       PurposeDiscoveryReflection,
		ProjectID:     "project-1",
		Subject:       Subject{Kind: subject.Kind, ID: subject.ID, Version: subject.Version},
		SubjectDigest: digest,
		InputDigest:   input,
	}
	if v.req != want {
		t.Fatalf("request = %+v, want %+v", v.req, want)
	}
	if got.HumanActorID != "operator-alice" {
		t.Fatalf("human actor = %q", got.HumanActorID)
	}
}

func TestHumanReceiptsFailsClosedWithoutIndependentBindings(t *testing.T) {
	ref := "rcpt_01j7abc1234567890abcdef123"
	caller := principal.CallerContext{PrincipalID: "principal-1", ProjectID: "project-1"}
	h := &HumanReceipts{Verifier: &captureVerifier{}}

	withoutDigest := ReceiptSubject{Kind: "ProblemModelReflection", ID: "pm-1", Version: 1}
	if _, err := h.Verify(context.Background(), caller, ref, string(PurposeDiscoveryReflection), "sha256:input", withoutDigest); err == nil {
		t.Fatal("expected missing subject digest error")
	}

	withDigest := ReceiptSubject{
		Kind:          "ProblemModelReflection",
		ID:            "pm-1",
		Version:       1,
		SubjectDigest: "sha256:1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff",
	}
	if _, err := h.Verify(context.Background(), caller, ref, string(PurposeDiscoveryReflection), "", withDigest); err == nil {
		t.Fatal("expected missing input digest error")
	}
	if _, err := h.Verify(context.Background(), caller, ref, string(PurposeHostPlanApply), "sha256:input", withDigest); err == nil {
		t.Fatal("expected unauthorized purpose error")
	}
	if _, err := (&HumanReceipts{}).Verify(context.Background(), caller, ref, string(PurposeDiscoveryLedgerResolution), "", withDigest); err == nil {
		t.Fatal("expected nil verifier error")
	}
}
