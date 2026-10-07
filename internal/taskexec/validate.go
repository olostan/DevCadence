package taskexec

import (
	"context"

	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/validation"
)

// ProfileSource provides validation profiles by project and profile ID.
type ProfileSource interface {
	Profile(ctx context.Context, projectID, profileID string) (validation.Profile, error)
}

// Validate dispatches deterministic validation of a candidate commit (reserved for WP-9 / Part C).
func (e *Executor) Validate(ctx context.Context, caller principal.CallerContext, meta principal.CallMeta, candidate principal.CandidateRef, profileID string) (principal.OperationRef, error) {
	if e.opts.Profiles == nil {
		return principal.OperationRef{}, principal.NewCodedError(
			principal.CodeModelUnavailable,
			false,
			[]string{"validate-not-implemented"},
			"validation runtime not available in this window",
		)
	}
	return principal.OperationRef{}, principal.NewCodedError(
		principal.CodeModelUnavailable,
		false,
		[]string{"validate-not-implemented"},
		"validation implementation reserved for WP-9",
	)
}
