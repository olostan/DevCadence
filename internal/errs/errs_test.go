package errs_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/olostan/DevCadience/internal/errs"
)

func TestErrorsMatchByCategoryNotByMessage(t *testing.T) {
	err := errs.New(errs.CategoryNotFound, "task %s does not exist", "DC-001")
	if !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("a not-found error did not match the sentinel")
	}
	if errors.Is(err, errs.ErrConflict) {
		t.Fatal("a not-found error matched the conflict sentinel")
	}
}

func TestWrapPreservesTheCauseAndClassifies(t *testing.T) {
	cause := errors.New("disk on fire")
	wrapped := errs.Wrap(errs.CategoryIntegrity, cause, "read projection for %s", "example")
	if !errors.Is(wrapped, cause) {
		t.Fatal("the cause was lost")
	}
	if got := errs.CategoryOf(wrapped); got != errs.CategoryIntegrity {
		t.Fatalf("category = %s, want integrity", got)
	}
}

func TestWrapOfNilIsNil(t *testing.T) {
	if err := errs.Wrap(errs.CategoryInternal, nil, "no failure here"); err != nil {
		t.Fatalf("wrapping nil produced %v", err)
	}
}

// TestUnclassifiedErrorsAreInternal makes a missing classification visible
// rather than letting it masquerade as a routable engineering condition.
func TestUnclassifiedErrorsAreInternal(t *testing.T) {
	if got := errs.CategoryOf(errors.New("plain")); got != errs.CategoryInternal {
		t.Fatalf("category = %s, want internal", got)
	}
	if got := errs.CategoryOf(nil); got != "" {
		t.Fatalf("category of nil = %q, want empty", got)
	}
}

func TestCategorySurvivesFurtherWrapping(t *testing.T) {
	err := errs.New(errs.CategoryPolicyDenied, "denied")
	outer := fmt.Errorf("while delegating: %w", err)
	if got := errs.CategoryOf(outer); got != errs.CategoryPolicyDenied {
		t.Fatalf("category = %s, want policy_denied", got)
	}
}

// TestEveryTaxonomySentinelHasADistinctCategory keeps the routing taxonomy of
// ENGINEERING_STANDARDS.md §6 unambiguous.
func TestEveryTaxonomySentinelHasADistinctCategory(t *testing.T) {
	sentinels := []error{
		errs.ErrContradictedAssumption, errs.ErrValidationFailed, errs.ErrPolicyDenied,
		errs.ErrNeedsPrincipal, errs.ErrConsultantUnavailable, errs.ErrModelUnavailable,
		errs.ErrWorktreeConflict, errs.ErrSchemaVersionUnsupported, errs.ErrInvalidTransition,
		errs.ErrInvalidArgument, errs.ErrNotFound, errs.ErrConflict, errs.ErrIntegrity,
		errs.ErrInternal,
	}
	seen := make(map[errs.Category]bool, len(sentinels))
	for _, sentinel := range sentinels {
		category := errs.CategoryOf(sentinel)
		if category == "" {
			t.Fatalf("sentinel %v has no category", sentinel)
		}
		if seen[category] {
			t.Fatalf("category %s is claimed by two sentinels", category)
		}
		seen[category] = true
	}
}
