// Package errs defines the machine-classifiable error taxonomy required by
// ENGINEERING_STANDARDS.md §6.
//
// The control plane routes on error category: a contradicted assumption
// escalates to the principal, a policy denial does not, and an unsupported
// schema version must never be papered over. Human-readable messages
// supplement the category; they never replace it.
package errs

import (
	"errors"
	"fmt"
)

// Category classifies an error for control-plane routing. Values are stable
// strings because they are written to structured logs and, in later
// milestones, to durable escalation records.
type Category string

const (
	// CategoryContradictedAssumption reports that a Work Package assumption
	// was found false by evidence. It escalates to the principal.
	CategoryContradictedAssumption Category = "contradicted_assumption"
	// CategoryValidationFailed reports a deterministic check failure.
	CategoryValidationFailed Category = "validation_failed"
	// CategoryPolicyDenied reports that policy forbids the requested action.
	CategoryPolicyDenied Category = "policy_denied"
	// CategoryNeedsPrincipal reports that the action requires principal
	// authority the caller does not have.
	CategoryNeedsPrincipal Category = "needs_principal"
	// CategoryConsultantUnavailable reports that a frontier consultant could
	// not be reached. Reserved for M6.
	CategoryConsultantUnavailable Category = "consultant_unavailable"
	// CategoryModelUnavailable reports that a local model runtime could not be
	// reached. Reserved for M3.
	CategoryModelUnavailable Category = "model_unavailable"
	// CategoryWorktreeConflict reports a Git worktree or base-divergence
	// conflict. Reserved for M2.
	CategoryWorktreeConflict Category = "worktree_conflict"
	// CategoryUnsupported reports that a discovered integration exists but at
	// a version or in a configuration this build cannot use. It is distinct
	// from CategoryNotFound ("absent"): absence may be remediated by
	// installing something, an unsupported version may not.
	CategoryUnsupported Category = "unsupported"
	// CategoryUnauthenticated reports that an endpoint exists and is healthy
	// but has no usable authenticated session. It is deliberately not
	// CategoryPolicyDenied: nothing forbade the action, the caller simply has
	// no credentials for it, and the two lead to different remediation.
	CategoryUnauthenticated Category = "unauthenticated"
	// CategoryProbeFailed reports that a capability probe ran and did not
	// establish its fact. It is not an internal error: a runtime that answers
	// incorrectly, or malformed output from an external tool, is an observed
	// fact about the environment (DCI-104).
	CategoryProbeFailed Category = "probe_failed"
	// CategoryProbeTimeout reports that a probe exceeded its bound. It is
	// separate from CategoryProbeFailed because a timeout says nothing about
	// whether the capability works — only that it did not answer in time.
	CategoryProbeTimeout Category = "probe_timeout"
	// CategoryNoEligibleEndpoint reports that capability routing found no
	// endpoint satisfying the role's hard constraints. This is a normal,
	// explainable outcome — for example when project privacy policy forbids
	// every remote endpoint — and must not be reported as an internal failure
	// (DCI-104).
	CategoryNoEligibleEndpoint Category = "no_eligible_endpoint"
	// CategorySchemaVersionUnsupported reports a durable record whose
	// schema_version this build cannot interpret (DCI-092, DCI-093).
	CategorySchemaVersionUnsupported Category = "schema_version_unsupported"
	// CategoryInvalidTransition reports an illegal state-machine transition.
	CategoryInvalidTransition Category = "invalid_transition"
	// CategoryInvalidArgument reports malformed or contradictory input.
	CategoryInvalidArgument Category = "invalid_argument"
	// CategoryNotFound reports a durable record that does not exist.
	CategoryNotFound Category = "not_found"
	// CategoryConflict reports a uniqueness or optimistic-concurrency
	// conflict, such as appending an event whose ID already exists.
	CategoryConflict Category = "conflict"
	// CategoryIntegrity reports durable data that violates an invariant the
	// control plane relies on, such as a projection that disagrees with the
	// event journal.
	CategoryIntegrity Category = "integrity"
	// CategoryInternal reports a defect in DevCadience itself.
	CategoryInternal Category = "internal"
)

// Sentinels for errors.Is checks. Each carries only its category so that
// callers can classify without string matching.
var (
	ErrContradictedAssumption   = &Error{Category: CategoryContradictedAssumption, Message: "contradicted assumption"}
	ErrValidationFailed         = &Error{Category: CategoryValidationFailed, Message: "validation failed"}
	ErrPolicyDenied             = &Error{Category: CategoryPolicyDenied, Message: "policy denied"}
	ErrNeedsPrincipal           = &Error{Category: CategoryNeedsPrincipal, Message: "principal decision required"}
	ErrConsultantUnavailable    = &Error{Category: CategoryConsultantUnavailable, Message: "consultant unavailable"}
	ErrModelUnavailable         = &Error{Category: CategoryModelUnavailable, Message: "model unavailable"}
	ErrWorktreeConflict         = &Error{Category: CategoryWorktreeConflict, Message: "worktree conflict"}
	ErrUnsupported              = &Error{Category: CategoryUnsupported, Message: "unsupported"}
	ErrUnauthenticated          = &Error{Category: CategoryUnauthenticated, Message: "unauthenticated"}
	ErrProbeFailed              = &Error{Category: CategoryProbeFailed, Message: "probe failed"}
	ErrProbeTimeout             = &Error{Category: CategoryProbeTimeout, Message: "probe timed out"}
	ErrNoEligibleEndpoint       = &Error{Category: CategoryNoEligibleEndpoint, Message: "no eligible cognition endpoint"}
	ErrSchemaVersionUnsupported = &Error{Category: CategorySchemaVersionUnsupported, Message: "schema version unsupported"}
	ErrInvalidTransition        = &Error{Category: CategoryInvalidTransition, Message: "invalid transition"}
	ErrInvalidArgument          = &Error{Category: CategoryInvalidArgument, Message: "invalid argument"}
	ErrNotFound                 = &Error{Category: CategoryNotFound, Message: "not found"}
	ErrConflict                 = &Error{Category: CategoryConflict, Message: "conflict"}
	ErrIntegrity                = &Error{Category: CategoryIntegrity, Message: "integrity violation"}
	ErrInternal                 = &Error{Category: CategoryInternal, Message: "internal error"}
)

// Error is a categorised control-plane error.
type Error struct {
	Category Category
	// Message is the human-readable explanation. It supplements Category.
	Message string
	// Cause is the wrapped error, if any.
	Cause error
}

func (e *Error) Error() string {
	if e.Cause == nil {
		return fmt.Sprintf("%s: %s", e.Category, e.Message)
	}
	return fmt.Sprintf("%s: %s: %v", e.Category, e.Message, e.Cause)
}

// Unwrap exposes the cause to errors.Is/errors.As.
func (e *Error) Unwrap() error { return e.Cause }

// Is reports equality by category, so errors.Is(err, ErrNotFound) matches any
// not-found error regardless of message or cause.
func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	return ok && other.Category == e.Category
}

// New returns a categorised error with a formatted message.
func New(category Category, format string, args ...any) *Error {
	return &Error{Category: category, Message: fmt.Sprintf(format, args...)}
}

// Wrap returns a categorised error wrapping cause. It returns nil when cause
// is nil so that it can be used directly on a returned error value.
func Wrap(category Category, cause error, format string, args ...any) error {
	if cause == nil {
		return nil
	}
	return &Error{Category: category, Message: fmt.Sprintf(format, args...), Cause: cause}
}

// CategoryOf reports the category of err, walking the wrap chain. Errors that
// do not originate here are reported as CategoryInternal, because an
// unclassified failure is a defect in classification rather than a routable
// engineering condition.
func CategoryOf(err error) Category {
	if err == nil {
		return ""
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Category
	}
	return CategoryInternal
}
