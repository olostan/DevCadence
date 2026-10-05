package facade

import (
	"context"
	"errors"

	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/principal"
)

// codedError carries a semantic code chosen by the facade itself. Its text is
// diagnostic only and is never sent to a caller: SemanticError messages are
// fixed per code.
type codedError struct {
	code      string
	retryable bool
	refs      []string
	detail    string
}

func (e *codedError) Error() string { return e.code + ": " + e.detail }

func coded(code string, retryable bool, refs []string, detail string) error {
	return &codedError{code: code, retryable: retryable, refs: refs, detail: detail}
}

// staleRepository reports that the repository changed outside DevCadence
// since the evidence, Work Package or state was captured. The caller must
// refresh (project_state) and re-request.
func staleRepository(head string, changed int, detail string) error {
	refs := []string{}
	if head != "" && len(head) <= principal.MaxIDBytes-4 {
		refs = append(refs, "git:"+head)
	}
	_ = changed
	return coded(principal.CodeStaleProjectState, true, refs, detail)
}

// semanticFor maps any error to a safe SemanticError. The raw error is never
// copied: only its code is, so provider, SQL, shell and path text cannot leak.
func semanticFor(err error) principal.SemanticError {
	var ce *codedError
	switch {
	case errors.As(err, &ce):
		return principal.NewSemanticError(ce.code, ce.refs, ce.retryable)
	case errors.Is(err, controlplane.ErrStaleProjectState):
		return principal.NewSemanticError(principal.CodeStaleProjectState, nil, true)
	case errors.Is(err, controlplane.ErrStaleWorkPackage):
		return principal.NewSemanticError(principal.CodeStaleWorkPackage, nil, false)
	case errors.Is(err, controlplane.ErrStorageBusy):
		return principal.NewSemanticError(principal.CodeInternal, nil, true)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return principal.NewSemanticError(principal.CodeCancelled, nil, true)
	}
	switch errs.CategoryOf(err) {
	case errs.CategoryInvalidArgument, errs.CategoryInvalidTransition, errs.CategoryConflict:
		return principal.NewSemanticError(principal.CodeInvalidArgument, nil, false)
	case errs.CategorySchemaVersionUnsupported:
		return principal.NewSemanticError(principal.CodeUnsupportedSchemaVersion, nil, false)
	case errs.CategoryNotFound:
		return principal.NewSemanticError(principal.CodeNotFound, nil, false)
	case errs.CategoryIntegrity:
		return principal.NewSemanticError(principal.CodeIntegrity, nil, false)
	case errs.CategoryPolicyDenied, errs.CategoryUnauthenticated:
		return principal.NewSemanticError(principal.CodePolicyDenied, nil, false)
	case errs.CategoryNeedsPrincipal:
		return principal.NewSemanticError(principal.CodeNeedsPrincipal, nil, false)
	case errs.CategoryModelUnavailable, errs.CategoryNoEligibleEndpoint:
		return principal.NewSemanticError(principal.CodeModelUnavailable, nil, false)
	case errs.CategoryContextUnfit:
		return principal.NewSemanticError(principal.CodeContextUnfit, nil, false)
	case errs.CategoryContradictedAssumption:
		return principal.NewSemanticError(principal.CodeContradictedAssumption, nil, false)
	case errs.CategoryValidationFailed:
		return principal.NewSemanticError(principal.CodeValidationFailed, nil, false)
	}
	return principal.NewSemanticError(principal.CodeInternal, nil, false)
}

// envelopeError builds the response envelope of a refusal.
func envelopeError(revision string, err error) Envelope {
	se := semanticFor(err)
	refs := se.EvidenceRefs
	if refs == nil {
		refs = []string{}
	}
	return Envelope{SchemaVersion: principal.SchemaVersion, StateRevision: revision, EvidenceRefs: refs, Error: &se}
}

func envelopeOK(revision string, refs []string) Envelope {
	if refs == nil {
		refs = []string{}
	}
	return Envelope{SchemaVersion: principal.SchemaVersion, StateRevision: revision, EvidenceRefs: refs}
}

// ResponseError returns the semantic error of a response envelope, or nil.
func (e Envelope) ResponseError() *principal.SemanticError { return e.Error }

// ErrorEnvelope builds the response envelope of a refusal that happened before
// the facade was reached (decode failure, oversize frame, recovered panic).
// The error text is never copied: only its semantic code is.
func ErrorEnvelope(err error) Envelope { return envelopeError("", err) }

// ContextUnfitEnvelope is the complete typed refusal for a response that would
// exceed the transport cap.
func ContextUnfitEnvelope() Envelope {
	return envelopeError("", coded(principal.CodeContextUnfit, true, nil, "response exceeds the transport cap"))
}
