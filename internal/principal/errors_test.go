package principal_test

import (
	"testing"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/principal"
)

func TestCodedError(t *testing.T) {
	t.Run("basic properties and formatting", func(t *testing.T) {
		refs := []string{"ref-1", "ref-2"}
		err := principal.NewCodedError(principal.CodeModelUnavailable, true, refs, "connection timeout")

		if err.Error() != principal.CodeModelUnavailable+": connection timeout" {
			t.Errorf("Error() = %q, want %q", err.Error(), principal.CodeModelUnavailable+": connection timeout")
		}
		if err.Code() != principal.CodeModelUnavailable {
			t.Errorf("Code() = %q, want %q", err.Code(), principal.CodeModelUnavailable)
		}
		if !err.Retryable() {
			t.Errorf("Retryable() = false, want true")
		}
		if err.Detail() != "connection timeout" {
			t.Errorf("Detail() = %q, want %q", err.Detail(), "connection timeout")
		}

		gotRefs := err.EvidenceRefs()
		if len(gotRefs) != 2 || gotRefs[0] != "ref-1" || gotRefs[1] != "ref-2" {
			t.Errorf("EvidenceRefs() = %v, want %v", gotRefs, refs)
		}

		// Mutating returned refs should not affect internal state
		gotRefs[0] = "mutated"
		freshRefs := err.EvidenceRefs()
		if freshRefs[0] != "ref-1" {
			t.Errorf("internal refs mutated via returned slice")
		}

		// Mutating initial slice should not affect internal state
		refs[0] = "mutated-initial"
		freshRefs2 := err.EvidenceRefs()
		if freshRefs2[0] != "ref-1" {
			t.Errorf("internal refs mutated via constructor slice")
		}
	})

	t.Run("nil refs handling", func(t *testing.T) {
		err := principal.NewCodedError(principal.CodeNotFound, false, nil, "item missing")
		if err.EvidenceRefs() != nil {
			t.Errorf("EvidenceRefs() with nil refs = %v, want nil", err.EvidenceRefs())
		}
		if err.Retryable() {
			t.Errorf("Retryable() = true, want false")
		}
	})

	t.Run("unwrap category mappings", func(t *testing.T) {
		codes := []struct {
			code string
			want errs.Category
		}{
			{principal.CodeConflict, errs.CategoryConflict},
			{principal.CodePolicyDenied, errs.CategoryPolicyDenied},
			{principal.CodeInvalidArgument, errs.CategoryInvalidArgument},
			{principal.CodeNotFound, errs.CategoryNotFound},
			{principal.CodeIntegrity, errs.CategoryIntegrity},
			{principal.CodeValidationFailed, errs.CategoryValidationFailed},
			{principal.CodeContradictedAssumption, errs.CategoryContradictedAssumption},
			{principal.CodeNeedsPrincipal, errs.CategoryNeedsPrincipal},
			{principal.CodeModelUnavailable, errs.CategoryModelUnavailable},
			{principal.CodeContextUnfit, errs.CategoryContextUnfit},
			{"UNKNOWN_CODE", errs.CategoryInternal},
		}
		for _, tc := range codes {
			err := principal.NewCodedError(tc.code, false, nil, "some detail")
			unwrapped := err.Unwrap()
			if got := errs.CategoryOf(unwrapped); got != tc.want {
				t.Errorf("code %s: got category %v, want %v", tc.code, got, tc.want)
			}
		}
	})
}
