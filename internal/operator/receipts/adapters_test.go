package receipts

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/protocol"
)

type captureVerifier struct {
	req    Request
	result Verified
	err    error
	calls  int
}

func (v *captureVerifier) Verify(_ context.Context, req Request) (Verified, error) {
	v.calls++
	v.req = req
	if v.err != nil {
		return nil, v.err
	}
	if v.result != nil {
		return v.result, nil
	}
	return verifiedReceipt{stmt: Statement{HumanActorID: "operator-alice"}, valid: true}, nil
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
	scopeBytes, _ := protocol.CanonicalJSON(HostPlanScope{PlanDigest: planDigest, Paths: paths})
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
	if v.calls != 1 {
		t.Fatalf("Verify calls = %d, want 1", v.calls)
	}
}

func TestHostPlanApprovalsRejectsInvalidConsumerInputsBeforeVerify(t *testing.T) {
	validRef := "rcpt_01j7abc1234567890abcdef123"
	planDigest := "sha256:1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff"
	tests := []struct {
		name  string
		h     HostPlanApprovals
		ref   string
		plan  string
		paths []string
	}{
		{"invalid receipt id", HostPlanApprovals{Verifier: &captureVerifier{}, ProjectID: "p"}, "bad", planDigest, []string{"/a"}},
		{"missing plan digest", HostPlanApprovals{Verifier: &captureVerifier{}, ProjectID: "p"}, validRef, "", []string{"/a"}},
		{"missing project", HostPlanApprovals{Verifier: &captureVerifier{}}, validRef, planDigest, []string{"/a"}},
		{"relative path", HostPlanApprovals{Verifier: &captureVerifier{}, ProjectID: "p"}, validRef, planDigest, []string{"a"}},
		{"unclean path", HostPlanApprovals{Verifier: &captureVerifier{}, ProjectID: "p"}, validRef, planDigest, []string{"/a/../b"}},
		{"duplicate paths", HostPlanApprovals{Verifier: &captureVerifier{}, ProjectID: "p"}, validRef, planDigest, []string{"/a", "/a"}},
		{"unsorted paths", HostPlanApprovals{Verifier: &captureVerifier{}, ProjectID: "p"}, validRef, planDigest, []string{"/b", "/a"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.h.VerifyApproval(context.Background(), tc.ref, tc.plan, tc.paths)
			if err == nil {
				t.Fatal("expected error")
			}
			if v, ok := tc.h.Verifier.(*captureVerifier); ok && v.calls != 0 {
				t.Fatalf("verifier called %d times before consumer input validation", v.calls)
			}
		})
	}
}

func TestHostPlanApprovalsPropagatesVerifierFailureAndInvalidResult(t *testing.T) {
	ref := "rcpt_01j7abc1234567890abcdef123"
	plan := "sha256:1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff"
	sentinel := errors.New("verify denied")
	v := &captureVerifier{err: sentinel}
	h := &HostPlanApprovals{Verifier: v, ProjectID: "p"}
	if err := h.VerifyApproval(context.Background(), ref, plan, []string{"/a"}); !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want sentinel", err)
	}

	v = &captureVerifier{result: verifiedReceipt{}}
	h.Verifier = v
	err := h.VerifyApproval(context.Background(), ref, plan, []string{"/a"})
	if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Fatalf("error = %v, category = %v", err, errs.CategoryOf(err))
	}
}

func TestHumanReceiptsUsesConsumerDigestOnly(t *testing.T) {
	ref := "rcpt_01j7abc1234567890abcdef123"
	digest := "sha256:1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff"
	input := "sha256:222233334444555566667777888899990000aaaabbbbccccddddeeeeffff0000"
	subject := ReceiptSubject{Kind: "ProblemModelReflection", ID: "pm-1", Version: 7, SubjectDigest: digest}
	v := &captureVerifier{result: verifiedReceipt{stmt: Statement{HumanActorID: "operator-alice"}, valid: true}}
	h := &HumanReceipts{Verifier: v}
	caller := principal.CallerContext{PrincipalID: "principal-1", ProjectID: "project-1"}

	got, err := h.Verify(context.Background(), caller, ref, string(PurposeDiscoveryReflection), input, subject)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	wantReq := Request{
		ReceiptID: ref, Purpose: PurposeDiscoveryReflection, ProjectID: "project-1",
		Subject: Subject{Kind: subject.Kind, ID: subject.ID, Version: subject.Version},
		SubjectDigest: digest, InputDigest: input,
	}
	if v.req != wantReq {
		t.Fatalf("request = %+v, want %+v", v.req, wantReq)
	}
	if got.HumanActorID != "operator-alice" || got.Subject.SubjectDigest != digest {
		t.Fatalf("result = %+v", got)
	}
}

func TestHumanReceiptsFailsClosedWithoutConsumerDigest(t *testing.T) {
	ref := "rcpt_01j7abc1234567890abcdef123"
	v := &captureVerifier{}
	h := &HumanReceipts{Verifier: v}
	caller := principal.CallerContext{PrincipalID: "principal-1", ProjectID: "project-1"}
	subject := ReceiptSubject{Kind: "ProblemModelReflection", ID: "pm-1", Version: 1}

	_, err := h.Verify(context.Background(), caller, ref, string(PurposeDiscoveryReflection), "sha256:input", subject)
	if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("error = %v, category = %v", err, errs.CategoryOf(err))
	}
	if v.calls != 0 {
		t.Fatalf("verifier called %d times", v.calls)
	}
}

func TestHumanReceiptsRejectsUnauthorizedPurposeAndMissingRequiredInput(t *testing.T) {
	ref := "rcpt_01j7abc1234567890abcdef123"
	subject := ReceiptSubject{
		Kind: "ProblemModelReflection", ID: "pm-1", Version: 1,
		SubjectDigest: "sha256:1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff",
	}
	caller := principal.CallerContext{PrincipalID: "principal-1", ProjectID: "project-1"}
	v := &captureVerifier{}
	h := &HumanReceipts{Verifier: v}

	_, err := h.Verify(context.Background(), caller, ref, string(PurposeHostPlanApply), "sha256:input", subject)
	if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Fatalf("unauthorized purpose error = %v", err)
	}
	_, err = h.Verify(context.Background(), caller, ref, string(PurposeDiscoveryReflection), "", subject)
	if err == nil || errs.CategoryOf(err) != errs.CategoryInvalidArgument {
		t.Fatalf("missing input error = %v", err)
	}
	if v.calls != 0 {
		t.Fatalf("verifier called %d times", v.calls)
	}
}

func TestHumanReceiptsVerifierFailureAndNilVerifier(t *testing.T) {
	ref := "rcpt_01j7abc1234567890abcdef123"
	subject := ReceiptSubject{
		Kind: "AmbiguityResolution", ID: "a-1", Version: 1,
		SubjectDigest: "sha256:1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff",
	}
	caller := principal.CallerContext{PrincipalID: "principal-1", ProjectID: "project-1"}

	_, err := (&HumanReceipts{}).Verify(context.Background(), caller, ref, string(PurposeDiscoveryLedgerResolution), "", subject)
	if err == nil || errs.CategoryOf(err) != errs.CategoryPolicyDenied {
		t.Fatalf("nil verifier error = %v", err)
	}

	sentinel := errors.New("verify denied")
	v := &captureVerifier{err: sentinel}
	_, err = (&HumanReceipts{Verifier: v}).Verify(context.Background(), caller, ref, string(PurposeDiscoveryLedgerResolution), "", subject)
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want sentinel", err)
	}
}

func TestHostPlanPathErrorMessagesRemainSpecific(t *testing.T) {
	ref := "rcpt_01j7abc1234567890abcdef123"
	h := &HostPlanApprovals{Verifier: &captureVerifier{}, ProjectID: "p"}
	err := h.VerifyApproval(context.Background(), ref, "sha256:x", []string{"/b", "/a"})
	if err == nil || !strings.Contains(err.Error(), "not sorted") {
		t.Fatalf("error = %v", err)
	}
}
